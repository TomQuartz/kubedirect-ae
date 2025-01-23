package backend

import (
	"context"
	"fmt"
	"time"

	corev1 "k8s.io/api/core/v1"

	// Kubedirect
	"github.com/tomquartz/kubedirect-bench/pkg/workload"
	kdutil "k8s.io/kubedirect/pkg/util"
)

type Executor interface {
	Execute(ctx context.Context, req *workload.Request) *workload.Response
	Close()
}

var framework string
var baseTimeout = 15 * time.Second
var timeoutFactor = 5.0

func Use(f string) {
	framework = f
}

func WithSLO(factor float64) {
	timeoutFactor = factor
}

func Timeout(req *workload.Request) time.Duration {
	if slo := time.Duration(float64(req.DurationMilliSec)*timeoutFactor) * time.Millisecond; slo > baseTimeout {
		return slo
	}
	return baseTimeout
}

func NewBackend(endpoint string) (Executor, error) {
	switch framework {
	case "fake":
		return newFakeBackend(), nil
	case "grpc":
		return newGrpcBackend(endpoint)
	}
	panic(fmt.Sprintf("invalid framework: %s", framework))
}

func IsPodReady(pod *corev1.Pod) bool {
	return kdutil.IsPodReady(pod)
}
