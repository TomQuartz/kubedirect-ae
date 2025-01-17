package decider

import (
	"context"
	"time"

	"github.com/tomquartz/kubedirect-bench/pkg/workload"
)

type Decider interface {
	// returns instant concurrency
	ReqIn(req *workload.Request) float64
	ReqOut(res *workload.Response) float64
	Activate(ctx context.Context) bool
	Reconcile(ctx context.Context, now time.Time, currentReady int) (int, error)
	Desired() int
}
