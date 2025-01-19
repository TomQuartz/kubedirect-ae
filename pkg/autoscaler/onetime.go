package autoscaler

import (
	"context"
	"fmt"
	"sync"

	"k8s.io/klog/v2"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/manager"

	// Kubedirect
	"github.com/tomquartz/kubedirect-bench/pkg/autoscaler/scaler"
	"github.com/tomquartz/kubedirect-bench/pkg/workload"
)

const onetimeInitialScale = 1

type OneTimeAutoscalerConfig struct {
	client       client.Client
	InitialScale int `yaml:"initialScale"`
}

func (cfg *OneTimeAutoscalerConfig) Complete(ctx context.Context, mgr manager.Manager) (*OneTimeAutoscalerConfig, error) {
	if cfg == nil {
		cfg = &OneTimeAutoscalerConfig{InitialScale: onetimeInitialScale}
	}
	cfg.client = mgr.GetClient()
	return cfg, nil
}

type OneTimeAutoscaler struct {
	runCtx       context.Context
	mu           sync.Mutex
	seen         map[string]bool
	scaler       scaler.Scaler
	initialScale int
}

func NewOneTimeAutoscaler(
	ctx context.Context,
	mgr manager.Manager,
	cfg *OneTimeAutoscalerConfig,
	keys ...string,
) (*OneTimeAutoscaler, error) {
	logger := klog.FromContext(ctx)
	s := &OneTimeAutoscaler{
		seen:         make(map[string]bool),
		initialScale: cfg.InitialScale,
	}
	// pre-populate deciders; the map layout is fixed thereafter
	for _, key := range keys {
		s.seen[key] = false
	}
	// deployment-based scaler
	scaler, err := scaler.NewDeploymentScaler(ctx, cfg.client, keys...)
	if err != nil {
		// logger.Error(err, "failed to create deployment scaler")
		return nil, fmt.Errorf("failed to create deployment scaler in one-time autoscaler: %v", err)
	}
	s.scaler = scaler
	logger.Info("One-time autoscaler initialized", "initialScale", s.initialScale)
	return s, nil
}

var _ Autoscaler = &OneTimeAutoscaler{}

func (s *OneTimeAutoscaler) Framework() string {
	return "one-time"
}

// Override autoscalerImpl.Run
func (s *OneTimeAutoscaler) Run(ctx context.Context) {
	s.runCtx = ctx
}

func (s *OneTimeAutoscaler) ReqIn(req *workload.Request) {
	key := req.Target
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.seen[key] {
		s.seen[key] = true
		go func() {
			if _, err := s.scaler.Scale(s.runCtx, key, s.initialScale); err != nil {
				klog.FromContext(s.runCtx).Error(err, "failed to scale")
			}
		}()
	}
}

func (s *OneTimeAutoscaler) ReqOut(req *workload.Response) {}
