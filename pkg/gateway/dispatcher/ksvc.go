package dispatcher

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/go-logr/logr"
	"k8s.io/klog/v2"

	// Kubedirect
	"github.com/tomquartz/kubedirect-bench/pkg/backend"
	"github.com/tomquartz/kubedirect-bench/pkg/workload"
)

const (
	kourierGatewayServicePort = ":80"
)

type KnServiceDispatcher struct {
	target   string
	timeout  time.Duration
	reqChan  <-chan *workload.Request
	resChan  chan<- *workload.Response
	endpoint string
	executor backend.Executor
}

func NewKnServiceDispatcher(ctx context.Context, target string, timeout time.Duration, reqChan <-chan *workload.Request, resChan chan<- *workload.Response, url string) (*KnServiceDispatcher, error) {
	kd := &KnServiceDispatcher{
		target:   target,
		timeout:  timeout,
		reqChan:  reqChan,
		resChan:  resChan,
		endpoint: strings.TrimPrefix(url, "http://") + kourierGatewayServicePort,
	}
	executor, err := backend.NewBackend(kd.endpoint)
	if err != nil {
		return nil, fmt.Errorf("failed to start backend: %v", err)
	}
	kd.executor = executor
	return kd, nil
}

func (kd *KnServiceDispatcher) Dispatch(ctx context.Context, _ logr.Logger, req *workload.Request) {
	// kn dispatcher is integrated with gateway service, so add the timeout
	ctx, cancel := context.WithTimeout(ctx, kd.timeout+backend.Timeout(req))
	defer cancel()
	res := kd.executor.Execute(ctx, req)
	kd.resChan <- res
}

func (kd *KnServiceDispatcher) Run(ctx context.Context) {
	logger := klog.FromContext(ctx)
	logger.V(1).Info("starting knative service dispatcher", "target", kd.target)
	for {
		select {
		case req := <-kd.reqChan:
			go kd.Dispatch(ctx, logger, req)
		case <-ctx.Done():
			return
		}
	}
}
