package dispatcher

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/go-logr/logr"
	"golang.design/x/chann"
	corev1 "k8s.io/api/core/v1"
	utilerrors "k8s.io/apimachinery/pkg/util/errors"
	"k8s.io/klog/v2"

	// Kubedirect
	"github.com/tomquartz/kubedirect-bench/pkg/backend"
	"github.com/tomquartz/kubedirect-bench/pkg/workload"
	"github.com/tomquartz/kubedirect-bench/pkg/workload/handler"
	kdutil "k8s.io/kubedirect/pkg/util"
)

const (
	podServiceConcurrency     = 1
	podServiceDispatchTimeout = 300 * time.Second
)

// NOTE: we index by both pod name and ip:port to handle pod restarts and/or ip reuse for different pods
var podEndpointKeyFunc = func(pod *corev1.Pod) (key string, ep string) {
	ep = pod.Status.PodIP + handler.WorkloadServicePort
	key = fmt.Sprintf("%s@%s", pod.Name, ep)
	return
}

// Directly dispatch request to a pod
type PodDispatcher struct {
	target    string
	endpoints *kdutil.SharedMap[backend.Executor]
	tokens    *chann.Chann[string]
	reqChan   <-chan *workload.Request
	resChan   chan<- *workload.Response
	logger    logr.Logger
}

func NewPodDispatcher(ctx context.Context, target string, reqChan <-chan *workload.Request, resChan chan<- *workload.Response) (*PodDispatcher, error) {
	pd := &PodDispatcher{
		target:    target,
		endpoints: kdutil.NewSharedMap[backend.Executor](),
		tokens:    chann.New[string](),
		reqChan:   reqChan,
		resChan:   resChan,
	}
	return pd, nil
}

func (pd *PodDispatcher) dispatch(ctx context.Context) (string, backend.Executor) {
	dispatchCtx, cancel := context.WithTimeout(ctx, podServiceDispatchTimeout)
	defer cancel()
	for {
		select {
		case <-dispatchCtx.Done():
			return "", nil
		case key := <-pd.tokens.Out():
			// Discard tokens of removed pods
			executor, ok := pd.endpoints.Get(key)
			if !ok {
				continue
			}
			// pd.logger.V(1).Info("Dispatching to pod", "req", req.ID, "endpoint", key)
			return key, executor
		}
	}
}

func (pd *PodDispatcher) Dispatch(ctx context.Context, logger logr.Logger, req *workload.Request) {
	key, executor := pd.dispatch(ctx)
	if executor == nil {
		logger.V(1).Info("[WARN] Timeout dispatching request", "req", req.ID)
		res := &workload.Response{
			Source: req,
			Status: workload.FAIL_TIMEOUT,
		}
		pd.resChan <- res
		return
	}
	// pd.logger.V(1).Info("Dispatching to pod", "req", req.ID, "endpoint", key)
	res := executor.Execute(ctx, req)
	pd.tokens.In() <- key
	pd.resChan <- res
}

func (pd *PodDispatcher) Reconcile(ctx context.Context, readyPods []*corev1.Pod) error {
	logger := pd.logger

	endpoints := make(map[string]string)
	for _, pod := range readyPods {
		key, ep := podEndpointKeyFunc(pod)
		endpoints[key] = ep
	}

	// reconcile with existing endpoins
	// NOTE: there is actually no need to acquire read lock, because we are the only writer to endpoins
	add, del := func() (add, del []string) {
		pd.endpoints.RLock()
		defer pd.endpoints.RUnlock()
		existing := pd.endpoints.Inner()
		for key := range endpoints {
			if _, ok := existing[key]; !ok {
				add = append(add, key)
			}
		}
		for key := range existing {
			if _, ok := endpoints[key]; !ok {
				del = append(del, key)
			}
		}
		return add, del
	}()
	if len(add) > 0 || len(del) > 0 {
		logger.V(1).Info("Reconciling", "ready", len(endpoints), "add", len(add), "del", len(del))
	}

	// add new endpoints in parallel
	var wg sync.WaitGroup
	wg.Add(len(add))
	errs := make(chan error, len(endpoints))
	for _, key := range add {
		go func(key string) {
			defer wg.Done()
			ep := endpoints[key]
			executor, err := backend.NewBackend(ep)
			if err != nil {
				errs <- fmt.Errorf("failed to start backend: %v", err)
				return
			}
			pd.endpoints.Set(key, executor)
			for i := 0; i < podServiceConcurrency; i++ {
				pd.tokens.In() <- key
			}
		}(key)
	}

	// remove stale endpoints
	for _, key := range del {
		if executor, _ := pd.endpoints.Del(key); executor != nil {
			go executor.Close()
		}
	}

	// wait for all adds to finish
	wg.Wait()
	close(errs)
	errList := []error{}
	for err := range errs {
		errList = append(errList, err)
	}
	return utilerrors.NewAggregate(errList)
}

func (pd *PodDispatcher) Run(ctx context.Context) {
	logger := klog.FromContext(ctx).WithValues("target", pd.target)
	logger.V(1).Info("Starting pod dispatcher")
	pd.logger = logger
	for {
		select {
		case req := <-pd.reqChan:
			go pd.Dispatch(ctx, logger, req)
		case <-ctx.Done():
			return
		}
	}
}
