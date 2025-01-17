package replay

import (
	"context"
	"fmt"
	"math"
	"sync"
	"time"

	"k8s.io/klog/v2"

	// Kubedirect
	"github.com/tomquartz/kubedirect-bench/pkg/workload"
)

const maxInvocationsPerSecondPerSender = 1000.

type worker struct {
	target            string
	trace             *workload.TraceSpec
	toGateway         chan<- *workload.Request
	clientStartTime   time.Time
	nSenders          int
	senderInvocations [][]*workload.InvocationSpec
}

func newWorker(target string, trace *workload.TraceSpec, send chan<- *workload.Request) *worker {
	// shard invocations to senders in a round-robin fashion
	nSenders := math.Ceil(float64(len(trace.Invocations)) / 60 / maxInvocationsPerSecondPerSender)
	senderInvocations := make([][]*workload.InvocationSpec, int(nSenders))
	for i, invocation := range trace.Invocations {
		senderBin := i % int(nSenders)
		senderInvocations[senderBin] = append(senderInvocations[senderBin], invocation)
	}
	return &worker{
		target:            target,
		trace:             trace,
		toGateway:         send,
		nSenders:          int(nSenders),
		senderInvocations: senderInvocations,
	}
}

func (w *worker) next(nextRequestTime float64) <-chan time.Time {
	nextSendTS := w.clientStartTime.Add(time.Duration(nextRequestTime * float64(time.Second)))
	return time.After(time.Until(nextSendTS))
}

func (w *worker) send(senderID int) {
	for reqID, spec := range w.senderInvocations[senderID] {
		<-w.next(spec.ArrivalTimeSec)
		now := time.Now()
		req := &workload.Request{
			ID:               fmt.Sprintf("%s-%d/%d", w.target, senderID, reqID),
			Target:           w.target,
			DurationMilliSec: spec.RuntimeMilliSec,
			ClientSendTS:     now,
			ClientRelTime:    now.Sub(w.clientStartTime),
			TraceRelTime:     time.Duration(spec.ArrivalTimeSec * float64(time.Second)),
		}
		// logger.V(1).Info("sending request", "time", t, "id", req.ID)
		w.toGateway <- req
	}
}

// NOTE: ctx is not used to stop senders
func (w *worker) replay(ctx context.Context, start time.Time) {
	logger := klog.FromContext(ctx).WithValues("src", "replay/worker", "target", w.target)
	logger.Info("Starting trace replay")
	w.clientStartTime = start
	var wg sync.WaitGroup
	wg.Add(w.nSenders)
	for i := 0; i < w.nSenders; i++ {
		go func(i int) {
			defer wg.Done()
			w.send(i)
		}(i)
	}
	wg.Wait()
	logger.Info("Trace replay finished")
}
