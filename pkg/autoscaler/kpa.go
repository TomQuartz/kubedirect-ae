package autoscaler

import (
	"context"
	"fmt"
	"time"

	"k8s.io/client-go/util/workqueue"
	"k8s.io/klog/v2"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/manager"

	// Kubedirect
	"github.com/tomquartz/kubedirect-bench/pkg/autoscaler/decider"
	"github.com/tomquartz/kubedirect-bench/pkg/autoscaler/scaler"
)

type KnativeAutoscaler struct {
	*autoscalerImpl
}

type KnativeAutoscalerConfig struct {
	client                   client.Client
	Async                    bool    `yaml:"async"`
	TargetConcurrency        float64 `yaml:"targetConcurrency"`
	MaxScaleUpRate           float64 `yaml:"maxScaleUpRate"`
	MaxScaleDownRate         float64 `yaml:"maxScaleDownRate"`
	StableWindowSeconds      float64 `yaml:"stableWindowSeconds"`
	PanicWindowPercentage    float64 `yaml:"panicWindowPercentage"`
	PanicThresholdPercentage float64 `yaml:"panicThresholdPercentage"`
	ScaleDownDelaySeconds    int64   `yaml:"scaleDownDelaySeconds"`
	TickIntervalSeconds      int64   `yaml:"tickIntervalSeconds"`
}

func (cfg *KnativeAutoscalerConfig) Complete(ctx context.Context, mgr manager.Manager) (*KnativeAutoscalerConfig, error) {
	cfg.client = mgr.GetClient()
	if cfg.TargetConcurrency == 0 {
		// use the default value in Dirigent
		// https://github.com/vhive-serverless/invitro/blob/40546b63cade9113a8c27e5632f39b03aa38333c/pkg/driver/deployment.go#L110
		cfg.TargetConcurrency = 100
	}
	return cfg, nil
}

func NewKnativeAutoscaler(
	ctx context.Context,
	cfg *KnativeAutoscalerConfig,
	keys ...string,
) (*KnativeAutoscaler, error) {
	logger := klog.FromContext(ctx)
	s := &KnativeAutoscaler{
		autoscalerImpl: &autoscalerImpl{
			framework:    "kpa",
			async:        cfg.Async,
			tickInterval: time.Duration(cfg.TickIntervalSeconds) * time.Second,
			client:       cfg.client,
			deciders:     make(map[string]decider.Decider),
			queue: workqueue.NewTypedRateLimitingQueueWithConfig(
				workqueue.DefaultTypedControllerRateLimiter[string](),
				workqueue.TypedRateLimitingQueueConfig[string]{Name: "kpa"},
			),
		},
	}

	// deployment-based scaler
	scaler, err := scaler.NewDeploymentScaler(ctx, cfg.client, keys...)
	if err != nil {
		// logger.Error(err, "failed to create deployment scaler")
		return nil, fmt.Errorf("failed to create deployment scaler in aquatope autoscaler: %v", err)
	}
	s.scaler = scaler

	stableWindow := time.Duration(cfg.StableWindowSeconds) * time.Second
	panicWindow := time.Duration(cfg.PanicWindowPercentage/100*cfg.StableWindowSeconds) * time.Second
	scaleDownDelay := time.Duration(cfg.ScaleDownDelaySeconds) * time.Second
	tickInterval := time.Duration(cfg.TickIntervalSeconds) * time.Second

	for _, key := range keys {
		s.deciders[key] = decider.NewKPADecider(key, cfg.TargetConcurrency, cfg.MaxScaleUpRate, cfg.MaxScaleDownRate, stableWindow, panicWindow, cfg.PanicThresholdPercentage/100, scaleDownDelay, tickInterval)
	}

	logger.Info("Knative autoscaler initialized", "concurrency", cfg.TargetConcurrency, "maxUp", cfg.MaxScaleUpRate, "maxDown", cfg.MaxScaleDownRate, "stable", cfg.StableWindowSeconds, "panicWin%", cfg.PanicWindowPercentage, "panicThresh%", cfg.PanicThresholdPercentage, "delay", cfg.ScaleDownDelaySeconds, "tick", cfg.TickIntervalSeconds)
	return s, nil
}

var _ Autoscaler = &KnativeAutoscaler{}
