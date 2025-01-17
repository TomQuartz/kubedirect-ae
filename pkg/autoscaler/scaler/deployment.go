package scaler

import (
	"context"
	"fmt"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	autoscalingv1 "k8s.io/api/autoscaling/v1"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/klog/v2"
	"sigs.k8s.io/controller-runtime/pkg/client"

	// Kubedirect
	"github.com/tomquartz/kubedirect-bench/pkg/workload"
)

type DeploymentScaler struct {
	client client.Client
}

func NewDeploymentScaler(ctx context.Context, client client.Client, keys ...string) (*DeploymentScaler, error) {
	// logger := klog.FromContext(ctx).WithValues("src", "autoscaler/scaler/deployment", "op", "init")
	s := &DeploymentScaler{
		client: client,
	}
	return s, nil
}

var _ Scaler = &DeploymentScaler{}

func (s *DeploymentScaler) Scale(ctx context.Context, key string, desired int) error {
	logger := klog.FromContext(ctx).WithValues("src", "autoscaler/scaler/deployment", "op", "scale", "key", key)
	deployment := &appsv1.Deployment{}
	if err := s.client.Get(ctx, workload.NamespacedNameFromKey(key), deployment); err != nil {
		return fmt.Errorf("failed to get deployment %v: %v", key, err)
	}
	if deployment.DeletionTimestamp != nil {
		return fmt.Errorf("deployment %v is being deleted", key)
	}
	if deployment.Spec.Replicas != nil && *deployment.Spec.Replicas == int32(desired) {
		return nil
	}
	logger.V(1).Info(fmt.Sprintf("[scaler/deployment] Scaling %v %v -> %v", key, *deployment.Spec.Replicas, desired))
	scale := &autoscalingv1.Scale{Spec: autoscalingv1.ScaleSpec{Replicas: int32(desired)}}
	return wait.PollUntilContextTimeout(ctx, time.Millisecond*50, time.Second*1, true, func(retryContext context.Context) (bool, error) {
		if err := s.client.SubResource("scale").Update(ctx, deployment, client.WithSubResourceBody(scale)); err != nil {
			return false, err
		}
		return true, nil
	})
}
