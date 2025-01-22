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
var executorTimeout time.Duration

func Use(f string) {
	framework = f
}

func WithTimeout(t time.Duration) {
	executorTimeout = t
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
