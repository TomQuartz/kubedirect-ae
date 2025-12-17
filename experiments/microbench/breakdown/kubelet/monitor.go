package main

import (
	"context"
	"fmt"
	"sort"
	"sync"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/client-go/util/workqueue"
	"k8s.io/klog/v2"

	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	// Kubedirect

	kdctx "k8s.io/kubedirect/pkg/context"
	kdutil "k8s.io/kubedirect/pkg/util"
)

type CtrlWorkQueue = workqueue.TypedRateLimitingInterface[reconcile.Request]

type Expectation struct {
	wg      *sync.WaitGroup
	mu      sync.Mutex
	desired map[string]time.Time
}

func NewExpectation() *Expectation {
	return &Expectation{
		desired: make(map[string]time.Time),
	}
}

func (s *Expectation) Watch(wg *sync.WaitGroup, podInfos []*kdctx.PodInfo) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.wg = wg
	for _, podInfo := range podInfos {
		key := fmt.Sprintf("%s/%s", podInfo.Namespace, podInfo.Name)
		s.desired[key] = time.Time{}
	}
}

func (s *Expectation) Done(pod *corev1.Pod) bool {
	key := fmt.Sprintf("%s/%s", pod.Namespace, pod.Name)
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.wg == nil {
		return false
	}
	if t, ok := s.desired[key]; ok && t.IsZero() {
		s.desired[key] = time.Now()
		s.wg.Done()
		return true
	}
	return false
}

type PodMonitor struct {
	ownerName   string
	expectation *Expectation
}

func NewPodMonitor(ownerName string) *PodMonitor {
	return &PodMonitor{
		ownerName:   ownerName,
		expectation: NewExpectation(),
	}
}


func (m *PodMonitor) Since(start time.Time) time.Duration {
	// gather all seen times from expectations
	seenTimes := []time.Time{}
	m.expectation.mu.Lock()
	defer m.expectation.mu.Unlock()
	for _, t := range m.expectation.desired {
		seenTimes = append(seenTimes, t)
	}
	if len(seenTimes) == 0 {
		klog.Infof("No seen times recorded")
		return 0
	}
	sort.Slice(seenTimes, func(i, j int) bool { return seenTimes[i].Before(seenTimes[j]) })
	idx := (90*len(seenTimes)) / 100
	percentile := seenTimes[idx]
	return percentile.Sub(start)
}

func (m *PodMonitor) Watch(wg *sync.WaitGroup, podInfos []*kdctx.PodInfo) {
	m.expectation.Watch(wg, podInfos)
}

func (m *PodMonitor) SetupWithManager(ctx context.Context, mgr ctrl.Manager) error {
	logger := klog.FromContext(ctx)
	kdLogger := kdutil.NewLogger(logger).WithHeader("Monitor").WithHeader("Kubelet")

	return ctrl.NewControllerManagedBy(mgr).
		// WithOptions(controller.Options{
		// 	MaxConcurrentReconciles: 256,
		// }).
		Named("breakdown_kubelet").
		WithEventFilter(predicate.NewPredicateFuncs(m.FilterEvent)).
		Watches(&corev1.Pod{}, handler.Funcs{
			CreateFunc: func(_ context.Context, ev event.CreateEvent, q CtrlWorkQueue) {
				pod := ev.Object.(*corev1.Pod)
				m.HandlePodEvent(kdLogger, nil, pod)
			},
			UpdateFunc: func(_ context.Context, ev event.UpdateEvent, q CtrlWorkQueue) {
				old := ev.ObjectOld.(*corev1.Pod)
				new := ev.ObjectNew.(*corev1.Pod)
				m.HandlePodEvent(kdLogger, old, new)
			},
			DeleteFunc: func(_ context.Context, ev event.DeleteEvent, q CtrlWorkQueue) {
				pod := ev.Object.(*corev1.Pod)
				m.HandlePodEvent(kdLogger, pod, nil)
			},
			GenericFunc: func(_ context.Context, ev event.GenericEvent, q CtrlWorkQueue) {
				kdLogger.WARN("Generic event", "event", ev)
			},
		}).
		Complete(m)
}

func (m *PodMonitor) FilterEvent(object client.Object) bool {
	return kdutil.IsManaged(object) && object.GetLabels()[kdutil.OwnerNameLabel] == m.ownerName
}

func (m *PodMonitor) HandlePodEvent(kdLogger *kdutil.Logger, old, new *corev1.Pod) {
	// this is deletion
	if new == nil {
		if m.expectation.Done(old) {
			kdLogger.Info("Pod deletion", "pod", klog.KObj(old))
		}
		return
	}
	// create or update
	if kdutil.IsPodReady(new) {
		if m.expectation.Done(new) {
			kdLogger.Info("Pod ready", "pod", klog.KObj(old))
		}
	}
}

func (m *PodMonitor) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	return ctrl.Result{}, nil
}
