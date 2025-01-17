package autoscaler

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/go-logr/logr"
	"gopkg.in/yaml.v2"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/client-go/util/workqueue"
	"k8s.io/klog/v2"
	"sigs.k8s.io/controller-runtime/pkg/client"

	// Kubedirect
	"github.com/tomquartz/kubedirect-bench/pkg/autoscaler/decider"
	"github.com/tomquartz/kubedirect-bench/pkg/autoscaler/scaler"
	"github.com/tomquartz/kubedirect-bench/pkg/backend"
	"github.com/tomquartz/kubedirect-bench/pkg/workload"
)

const (
	maxConcurrentScalers = 16
)

type AutoscalerConfig struct {
	Knative *KnativeAutoscalerConfig `yaml:"kpa"`
	OneTime *OneTimeAutoscalerConfig `yaml:"oneTime"`
}

func NewAutoscalerConfigFrom(configPath string) (*AutoscalerConfig, error) {
	if configPath == "" {
		return nil, nil
	}
	configYaml, err := os.ReadFile(configPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read gateway YAML config: %v", err)
	}
	config := &AutoscalerConfig{}
	if err := yaml.Unmarshal(configYaml, config); err != nil {
		return nil, fmt.Errorf("failed to unmarshal YAML config: %v", err)
	}
	return config, nil
}

type Autoscaler interface {
	Framework() string
	ReqIn(req *workload.Request)
	ReqOut(res *workload.Response)
	Run(ctx context.Context)
}

type autoscalerImpl struct {
	framework    string
	async        bool
	tickInterval time.Duration
	client       client.Client
	deciders     map[string]decider.Decider
	scaler       scaler.Scaler
	// We need a queue because ticking is periodic yet scaling is blocking
	// the queue would merge multiple requests for the same key
	queue  workqueue.TypedRateLimitingInterface[string]
	runCtx context.Context
	logger logr.Logger
}

func (s *autoscalerImpl) Framework() string {
	return s.framework
}

func (s *autoscalerImpl) scale(ctx context.Context, key string) error {
	logger := klog.FromContext(ctx).WithValues("autoscaler", s.framework, "op", "scale", "key", key)
	if s.deciders[key] == nil {
		panic(fmt.Sprintf("Scaling error: no decider for key %v", key))
	}
	deployment := &appsv1.Deployment{}
	if err := s.client.Get(ctx, workload.NamespacedNameFromKey(key), deployment); err != nil {
		return fmt.Errorf("failed to get deployment %v: %v", key, err)
	}

	var nReady int
	pods := corev1.PodList{}
	if err := s.client.List(ctx, &pods,
		client.InNamespace(deployment.Namespace),
		client.MatchingLabels(deployment.Spec.Template.Labels),
	); err != nil {
		return fmt.Errorf("failed to list pods for key %v: %v", key, err)
	}
	for i := range pods.Items {
		pod := &pods.Items[i]
		if backend.IsPodReady(pod) {
			nReady++
		}
	}
	desired, err := s.deciders[key].Reconcile(ctx, time.Now(), nReady)
	if err != nil {
		return fmt.Errorf("failed to get desired scale for key %v: %v", key, err)
	}
	logger.V(1).Info(fmt.Sprintf("scaling %v -> %v", nReady, desired))
	return s.scaler.Scale(ctx, key, desired)
}

func (s *autoscalerImpl) Run(ctx context.Context) {
	logger := klog.FromContext(ctx).WithValues("src", "autoscaler/"+s.framework)
	logger.Info("starting autoscaler")
	defer utilruntime.HandleCrashWithContext(ctx)
	defer s.queue.ShutDown()

	s.runCtx = ctx
	s.logger = logger
	for i := 0; i < maxConcurrentScalers; i++ {
		go s.workerLoop(ctx)
	}
	<-ctx.Done()
}

func (s *autoscalerImpl) processNextItem(ctx context.Context) bool {
	key, shutdown := s.queue.Get()
	if shutdown {
		return false
	}
	defer s.queue.Done(key)
	// we do not requeue in any cases
	defer s.queue.Forget(key)

	start := time.Now()
	s.logger.V(1).Info(fmt.Sprintf("Start scaling %v", key))
	if err := s.scale(ctx, key); err != nil {
		s.logger.Error(err, fmt.Sprintf("Failed to scale %v", key))
		// etcd error
		if strings.Contains(err.Error(), "mvcc") {
			panic(err.Error())
		}
	} else {
		s.logger.V(1).Info(fmt.Sprintf("Finish scaling %v after %v ms", key, time.Since(start).Milliseconds()))
	}
	return true
}

func (s *autoscalerImpl) workerLoop(ctx context.Context) {
	// Exit when s.queue is shut down
	for s.processNextItem(ctx) {
	}
}

func (s *autoscalerImpl) tickAutoScaler(key string) {
	ticker := time.NewTicker(s.tickInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			s.queue.Add(key)
		case <-s.runCtx.Done():
			return
		}
	}
}

func (s *autoscalerImpl) ReqIn(req *workload.Request) {
	if s.runCtx == nil {
		panic("autoscaler not started")
	}
	key := req.Target
	if s.deciders[key] == nil {
		panic(fmt.Sprintf("Req in id %v: no decider for key %v", req.ID, key))
	}
	// s.logger.V(1).Info("request in", "id", req.ID, "target", req.Target)
	s.deciders[key].ReqIn(req)
	if s.deciders[key].Activate(s.runCtx) {
		go s.tickAutoScaler(key)
	}
	if !s.async && s.deciders[key].Desired() == 0 {
		s.queue.Add(key)
	}
}

func (s *autoscalerImpl) ReqOut(res *workload.Response) {
	if s.runCtx == nil {
		panic("autoscaler not started")
	}
	key := res.Source.Target
	// s.logger.V(1).Info("request out", "id", res.Source.ID, "target", key)
	if s.deciders[key] == nil {
		panic(fmt.Sprintf("Req out id %v: no decider for key %v", res.Source.ID, key))
	}
	s.deciders[key].ReqOut(res)
}
