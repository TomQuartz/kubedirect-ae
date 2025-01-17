package backend

import (
	"context"
	"time"

	"github.com/tomquartz/kubedirect-bench/pkg/workload"
)

type fakeBackend struct{}

var _ Executor = &fakeBackend{}

func newFakeBackend() *fakeBackend {
	return &fakeBackend{}
}

func (f *fakeBackend) Start() error { return nil }

func (f *fakeBackend) Close() {}

func (f *fakeBackend) Execute(_ context.Context, req *workload.Request) *workload.Response {
	start := time.Now()
	req.GatewaySendTS = start
	<-time.After(time.Duration(req.DurationMilliSec) * time.Millisecond)
	return &workload.Response{
		Source:          req,
		Status:          workload.SUCCESS,
		GatewayRecvTS:   time.Now(),
		RuntimeMicroSec: int(time.Since(start).Microseconds()),
	}
}
