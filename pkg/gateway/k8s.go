package gateway

import (
	"context"
	"fmt"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/klog/v2"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	// Kubedirect
	"github.com/tomquartz/kubedirect-bench/pkg/autoscaler"
	"github.com/tomquartz/kubedirect-bench/pkg/backend"
	"github.com/tomquartz/kubedirect-bench/pkg/gateway/dispatcher"
	benchutil "github.com/tomquartz/kubedirect-bench/pkg/util"
	"github.com/tomquartz/kubedirect-bench/pkg/workload"
)

type k8sGateway struct {
	*gatewayImpl
	client          client.Client
	dispatchers     map[string]*dispatcher.PodDispatcher
	autoscaler      autoscaler.Autoscaler
	newAutoscalerFn func(ctx context.Context, mgr manager.Manager, keys ...string) (autoscaler.Autoscaler, error)
}

func NewK8sGateway(asFramework string, asConfigPath string) (*k8sGateway, error) {
	g := &k8sGateway{
		dispatchers: make(map[string]*dispatcher.PodDispatcher),
	}
	g.gatewayImpl = newGatewayImpl(g.onReqIn, g.onReqOut)

	asConfig, err := autoscaler.NewAutoscalerConfigFrom(asConfigPath)
	if err != nil {
		return nil, fmt.Errorf("failed to parse autoscaler config: %v", err)
	}

	switch asFramework {
	case "kpa":
		g.newAutoscalerFn = func(ctx context.Context, mgr manager.Manager, keys ...string) (autoscaler.Autoscaler, error) {
			if kpaConfig, err := asConfig.Knative.Complete(ctx, mgr); err != nil {
				return nil, err
			} else {
				return autoscaler.NewKnativeAutoscaler(ctx, kpaConfig, keys...)
			}
		}
	case "one-time":
		g.newAutoscalerFn = func(ctx context.Context, mgr manager.Manager, keys ...string) (autoscaler.Autoscaler, error) {
			if oneTimeConfig, err := asConfig.OneTime.Complete(ctx, mgr); err != nil {
				return nil, err
			} else {
				return autoscaler.NewOneTimeAutoscaler(ctx, mgr, oneTimeConfig, keys...)
			}
		}
	}
	return g, nil
}

var _ Gateway = &k8sGateway{}

func (g *k8sGateway) onReqIn(req *workload.Request) {
	g.autoscaler.ReqIn(req)
}

func (g *k8sGateway) onReqOut(req *workload.Response) {
	g.autoscaler.ReqOut(req)
}

func (g *k8sGateway) Autoscaler() autoscaler.Autoscaler {
	return g.autoscaler
}

func (g *k8sGateway) Start(ctx context.Context) error {
	for key, dispatcher := range g.dispatchers {
		go g.relay(ctx, key)
		go dispatcher.Run(ctx)
	}
	if g.autoscaler != nil {
		go g.autoscaler.Run(ctx)
	}
	return nil
}

func (g *k8sGateway) SetUpWithManager(ctx context.Context, mgr manager.Manager) error {
	logger := klog.FromContext(ctx).WithValues("gateway", "k8s")

	g.client = mgr.GetClient()

	// setup a temporary client to list deployments because manager hasn't started yet
	uncachedClient := benchutil.NewUncachedClientOrDie(mgr)

	// NOTE: assume service names are the same as deployment names
	targets := &appsv1.DeploymentList{}
	if err := uncachedClient.List(ctx, targets, workload.CtrlListOptionsForTrace...); err != nil {
		return fmt.Errorf("error listing deployments in k8s gateway: %v", err)
	}
	keys := []string{}
	for i := range targets.Items {
		target := &targets.Items[i]
		key := workload.KeyFromObject(target)
		keys = append(keys, key)
		logger.V(1).Info(fmt.Sprintf("Registering deployment %v", key))
		// register channel
		g.register(key)
		reqBuffer, resBuffer := g.internalBuffers(key)
		// default to concurrency 1
		pd, err := dispatcher.NewPodDispatcher(ctx, key, reqBuffer, resBuffer)
		if err != nil {
			return fmt.Errorf("failed to create pod dispatcher for %v: %v", key, err)
		}
		g.dispatchers[key] = pd
	}
	logger.Info("All deployments registered", "total", len(g.dispatchers))

	if g.newAutoscalerFn != nil {
		autoscaler, err := g.newAutoscalerFn(ctx, mgr, keys...)
		if err != nil {
			return fmt.Errorf("failed to create autoscaler: %v", err)
		}
		g.autoscaler = autoscaler
		logger.Info("Autoscaler created", "framework", autoscaler.Framework())
	}

	// set up event handler
	enqueueWorkload := handler.TypedEnqueueRequestsFromMapFunc(
		func(ctx context.Context, obj client.Object) []reconcile.Request {
			workloadKey := workload.KeyFromObject(obj)
			return []reconcile.Request{{NamespacedName: workload.NamespacedNameFromKey(workloadKey)}}
		},
	)
	return ctrl.NewControllerManagedBy(mgr).
		WithOptions(controller.Options{
			MaxConcurrentReconciles: 256,
		}).
		Named("gateway_k8s").
		Watches(&corev1.Pod{}, enqueueWorkload).
		Watches(&appsv1.Deployment{}, enqueueWorkload).
		WithEventFilter(predicate.NewPredicateFuncs(g.FilterEvent)).
		Complete(g)
}

func (g *k8sGateway) FilterEvent(object client.Object) bool {
	return workload.IsTraceWorkload(object)
}

func (g *k8sGateway) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := klog.FromContext(ctx).WithValues("gateway", "k8s")

	key := req.NamespacedName.String()
	target := &appsv1.Deployment{}
	if err := g.client.Get(ctx, req.NamespacedName, target); err != nil {
		logger.Error(err, "Failed to get target deployment", "key", key)
		if apierrors.IsNotFound(err) {
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, err
	}

	// get matching pods
	pods := &corev1.PodList{}
	if err := g.client.List(ctx, pods,
		client.InNamespace(target.Namespace),
		client.MatchingLabels(target.Spec.Template.Labels),
	); err != nil {
		logger.Error(err, "Failed to list pods for target deployment", "key", key)
	}

	readyPods := make([]*corev1.Pod, 0, len(pods.Items))
	for i := range pods.Items {
		pod := &pods.Items[i]
		if backend.IsPodReady(pod) {
			readyPods = append(readyPods, pod)
		}
	}

	pd, ok := g.dispatchers[key]
	if !ok {
		logger.Info("[WARN] No dispatcher found for target, will ignore", "key", key)
		return ctrl.Result{}, nil
	}
	if err := pd.Reconcile(ctx, readyPods); err != nil {
		logger.Error(err, "Failed to reconcile pod dispatcher", "key", key)
		return ctrl.Result{}, err
	}

	return ctrl.Result{}, nil
}
