package dispatcher

import (
	"context"
	"fmt"
	"strings"

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
	reqChan  <-chan *workload.Request
	resChan  chan<- *workload.Response
	endpoint string
	executor backend.Executor
}

func NewKnServiceDispatcher(ctx context.Context, target string, reqChan <-chan *workload.Request, resChan chan<- *workload.Response, url string) (*KnServiceDispatcher, error) {
	kd := &KnServiceDispatcher{
		target:   target,
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
	res := kd.executor.Execute(ctx, req)
	kd.resChan <- res
}

func (kd *KnServiceDispatcher) Run(ctx context.Context) {
	logger := klog.FromContext(ctx).WithValues("src", "dispatcher/ksvc", "target", kd.target)
	logger.V(1).Info("starting Knative Service dispatcher")
	for {
		select {
		case req := <-kd.reqChan:
			go kd.Dispatch(ctx, logger, req)
		case <-ctx.Done():
			return
		}
	}
}
