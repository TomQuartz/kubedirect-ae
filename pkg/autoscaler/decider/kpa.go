package decider

import (
	"context"
	"fmt"
	"math"
	"sync/atomic"
	"time"

	"k8s.io/klog/v2"
	knas "knative.dev/serving/pkg/autoscaler/aggregation/max"

	// Kubedirect
	"github.com/tomquartz/kubedirect-bench/pkg/autoscaler/metric"
)

type KPADecider struct {
	*metric.Collector
	active int32
	// concurrency-based
	targetValue      float64
	maxScaleUpRate   float64
	maxScaleDownRate float64
	stableWindow     time.Duration
	panicWindow      time.Duration
	panicThreshold   float64
	delayWindow      *knas.TimeWindow
	tickInterval     time.Duration
	// variables
	panicTime    time.Time
	maxPanicPods int
	desiredScale int32
}

func NewKPADecider(
	key string,
	targetValue float64,
	maxScaleUpRate, maxScaleDownRate float64,
	stableWindow, panicWindow time.Duration,
	panicThreshold float64,
	scaleDownDelay, tickInterval time.Duration,
) *KPADecider {
	d := &KPADecider{
		Collector:        metric.NewCollector(key, stableWindow, panicWindow, 1*time.Second),
		targetValue:      targetValue,
		maxScaleUpRate:   maxScaleUpRate,
		maxScaleDownRate: maxScaleDownRate,
		stableWindow:     stableWindow,
		panicWindow:      panicWindow,
		panicThreshold:   panicThreshold,
		tickInterval:     tickInterval,
	}
	if scaleDownDelay > 0 {
		d.delayWindow = knas.NewTimeWindow(scaleDownDelay, tickInterval)
	}
	return d
}

var _ Decider = &KPADecider{}

func (k *KPADecider) Activate(ctx context.Context) bool {
	if atomic.CompareAndSwapInt32(&k.active, 0, 1) {
		logger := klog.FromContext(ctx)
		logger.V(1).Info("Starting KPA decider", "target", k.Key)
		go k.Collector.Run(ctx)
		return true
	}
	return false
}

func (k *KPADecider) Reconcile(ctx context.Context, now time.Time, currentReady int) (int, error) {
	logger := klog.FromContext(ctx).WithValues("target", k.Key)

	observedStableValue, observedPanicValue, observedInstantValue := k.StableAndPanicAndInstantConcurrency(now)

	isScalingFromZero := currentReady == 0
	// Use 1 if 0, otherwise the scale up/down rates won't work
	currentReady = int(math.Max(1, float64(currentReady)))
	upperbound, lowerbound := func() (float64, float64) {
		up := math.Ceil(k.maxScaleUpRate * float64(currentReady))
		low := math.Floor(float64(currentReady) / k.maxScaleDownRate)
		// If we're scaling from zero, we need to ensure we always have at least one pod.
		if isScalingFromZero && observedInstantValue > 0 {
			up = math.Max(up, 1)
			low = math.Max(low, 1)
		}
		return up, low
	}()
	dspc := math.Ceil(observedStableValue / k.targetValue)
	dppc := math.Ceil(observedPanicValue / k.targetValue)

	desiredStablePodCount := int(math.Min(math.Max(dspc, lowerbound), upperbound))
	desiredPanicPodCount := int(math.Min(math.Max(dppc, lowerbound), upperbound))

	isOverPanicThreshold := (dppc/float64(currentReady) >= k.panicThreshold)
	if k.panicTime.IsZero() && isOverPanicThreshold {
		// Begin panicking when we cross the threshold in the panic window.
		logger.V(2).Info("PANICKING.")
		k.panicTime = now
	} else if isOverPanicThreshold {
		// If we're still over panic threshold right now — extend the panic window.
		k.panicTime = now
	} else if !k.panicTime.IsZero() && !isOverPanicThreshold && k.panicTime.Add(k.stableWindow).Before(now) {
		// Stop panicking after the surge has made its way into the stable metric.
		logger.V(2).Info("UN-PANICKING.")
		k.panicTime = time.Time{}
		k.maxPanicPods = 0
	}

	var mode string
	desiredPodCount := desiredStablePodCount
	if !k.panicTime.IsZero() {
		// In some edgecases stable window metric might be larger
		// than panic one. And we should provision for stable as for panic,
		// so pick the larger of the two.
		if desiredPodCount < desiredPanicPodCount {
			desiredPodCount = desiredPanicPodCount
		}
		logger.V(2).Info("Operating in panic mode.")
		mode = "panic"
		// We do not scale down while in panic mode. Only increases will be applied.
		if desiredPodCount > k.maxPanicPods {
			logger.V(2).Info(fmt.Sprintf("[Panic] Update max pods in panic mode from %d to %d", k.maxPanicPods, desiredPodCount))
			k.maxPanicPods = desiredPodCount
		} else if desiredPodCount < k.maxPanicPods {
			logger.V(2).Info(fmt.Sprintf("[Panic] Cancel scale down: want %d keep %d", desiredPodCount, k.maxPanicPods))
		}
		desiredPodCount = k.maxPanicPods
	} else {
		logger.V(2).Info("Operating in stable mode.")
		mode = "stable"
	}

	// Delay scale down decisions, if a ScaleDownDelay was specified.
	// We only do this if there's a non-nil delayWindow because although a
	// one-element delay window is _almost_ the same as no delay at all, it is
	// not the same in the case where two Scale()s happen in the same time
	// interval (because the largest will be picked rather than the most recent
	// in that case).
	var delayedPodCount int
	if k.delayWindow != nil {
		k.delayWindow.Record(now, int32(desiredPodCount))
		delayedPodCount = int(k.delayWindow.Current())
		if delayedPodCount != desiredPodCount {
			logger.V(2).Info(fmt.Sprintf("Delaying scale down to %d, staying at %d", desiredPodCount, delayedPodCount))
			desiredPodCount = delayedPodCount
		}
	}

	logger.V(2).Info(fmt.Sprintf("[decider/kpa] %v"+
		" | Mode: %v"+
		" | Concurrency: stable=%0.3f panic=%0.3f target=%0.3f"+
		" | Scaling: current=%d desired=%d stable=%d(%0.0f) panic=%d(%0.0f) delay=%d range=[%0.0f, %0.0f]",
		k.Key, mode,
		observedStableValue, observedPanicValue, k.targetValue,
		currentReady, desiredPodCount, desiredStablePodCount, dspc, desiredPanicPodCount, dppc, delayedPodCount, lowerbound, upperbound))

	atomic.StoreInt32(&k.desiredScale, int32(desiredPodCount))

	return desiredPodCount, nil
}

func (k *KPADecider) Desired() int {
	return int(atomic.LoadInt32(&k.desiredScale))
}
