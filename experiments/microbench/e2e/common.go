package main

import (
	"context"
	"fmt"
	"log"
	"sync"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/client-go/util/workqueue"
	"k8s.io/klog/v2"

	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	// Kubedirect
	"github.com/tomquartz/kubedirect-bench/pkg/workload"
	kdutil "k8s.io/kubedirect/pkg/util"
)

type CtrlWorkQueue = workqueue.TypedRateLimitingInterface[reconcile.Request]

type Expectation struct {
	wg   *sync.WaitGroup
	mu   sync.Mutex
	seen sets.Set[string]
}

func NewExpectation(wg *sync.WaitGroup) *Expectation {
	return &Expectation{
		wg:   wg,
		seen: sets.New[string](),
	}
}

func (s *Expectation) Done(pod *corev1.Pod) bool {
	key := fmt.Sprintf("%s/%s", pod.Namespace, pod.Name)
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.seen.Has(key) {
		return false
	}
	s.seen.Insert(key)
	s.wg.Done()
	return true
}

type PodMonitor struct {
	selector     string
	expectations *kdutil.SharedMap[*Expectation]
}

func NewPodMonitor(selector string) *PodMonitor {
	return &PodMonitor{
		selector:     selector,
		expectations: kdutil.NewSharedMap[*Expectation](),
	}
}

func (m *PodMonitor) Watch(wg *sync.WaitGroup, key string) {
	m.expectations.Set(key, NewExpectation(wg))
}

func (m *PodMonitor) SetupWithManager(ctx context.Context, mgr ctrl.Manager) error {
	logger := klog.FromContext(ctx)
	kdLogger := kdutil.NewLogger(logger).WithHeader("Monitor").WithHeader("Pod")

	return ctrl.NewControllerManagedBy(mgr).
		// WithOptions(controller.Options{
		// 	MaxConcurrentReconciles: 256,
		// }).
		WithEventFilter(predicate.NewPredicateFuncs(m.FilterEvent)).
		Watches(&corev1.Pod{}, handler.Funcs{
			CreateFunc: func(_ context.Context, ev event.CreateEvent, q CtrlWorkQueue) {
				pod := ev.Object.(*corev1.Pod)
				m.HandlePodEvent(kdLogger, nil, pod)
			},
			UpdateFunc: func(_ context.Context, ev event.UpdateEvent, q CtrlWorkQueue) {
				old := ev.ObjectOld.(*corev1.Pod)
				new := ev.ObjectNew.(*corev1.Pod)
				m.HandlePodEvent(kdLogger, old, new)
			},
			DeleteFunc: func(_ context.Context, ev event.DeleteEvent, q CtrlWorkQueue) {
				pod := ev.Object.(*corev1.Pod)
				m.HandlePodEvent(kdLogger, pod, nil)
			},
			GenericFunc: func(_ context.Context, ev event.GenericEvent, q CtrlWorkQueue) {
				kdLogger.WARN("Generic event", "event", ev)
			},
		}).
		Complete(m)
}

func (m *PodMonitor) FilterEvent(object client.Object) bool {
	return kdutil.IsManaged(object) && object.GetLabels()["workload"] == m.selector
}

func (m *PodMonitor) HandlePodEvent(kdLogger *kdutil.Logger, old, new *corev1.Pod) {
	// this is deletion
	if new == nil {
		kdLogger.Info("Pod deletion", "pod", klog.KObj(old))
		return
	}
	// create or update
	if kdutil.IsPodReady(new) {
		key := workload.KeyFromObject(new)
		if exp, ok := m.expectations.Get(key); ok {
			if exp.Done(new) {
				kdLogger.Info("Pod ready", "pod", klog.KObj(old))
			}
		}
	}
}

func (m *PodMonitor) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	return ctrl.Result{}, nil
}

func run(ctx context.Context, mgr manager.Manager, selector string, nPods int) {
	monitor := NewPodMonitor(selector)
	if err := monitor.SetupWithManager(ctx, mgr); err != nil {
		log.Fatalf("Error creating monitor: %v\n", err)
	}

	log.Println("Starting manager")
	go func() {
		if err := mgr.Start(ctx); err != nil {
			log.Fatalf("Error running manager: %v\n", err)
		}
	}()
	if !mgr.GetCache().WaitForCacheSync(ctx) {
		log.Fatalf("Cannot syncing manager cache\n")
	}

	targets := &appsv1.DeploymentList{}
	mgrClient := mgr.GetClient()
	listOpts := append(
		[]client.ListOption{client.MatchingLabels{"workload": selector}},
		workload.CtrlListOptions...,
	)
	if err := mgrClient.List(ctx, targets, listOpts...); err != nil {
		log.Fatalf("Error listing Deployments: %v\n", err)
	}
	if len(targets.Items) == 0 {
		log.Fatal("No Deployment selected\n")
	}

	nPodsPerTarget := nPods / len(targets.Items)
	if nPodsPerTarget == 0 {
		log.Println("[WARN] The number of pods scaled per target is 0, resetting to 1")
		nPodsPerTarget = 1
	}
	nPods = nPodsPerTarget * len(targets.Items)
	log.Printf("Scaling up %d pods over %d deployments\n", nPods, len(targets.Items))

	wg := &sync.WaitGroup{}
	wg.Add(nPods)
	for i := range targets.Items {
		target := &targets.Items[i]
		monitor.Watch(wg, workload.KeyFromObject(target))
	}

	logger := klog.FromContext(ctx)
	start := time.Now()
	for i := range targets.Items {
		target := &targets.Items[i]
		*target.Spec.Replicas = int32(nPodsPerTarget)
		go func() {
			if err := mgrClient.Update(ctx, target); err != nil {
				logger.Error(err, "Error scaling up", "target", klog.KObj(target))
			}
		}()
	}
	wg.Wait()

	fmt.Printf("total: %v us\n", time.Since(start).Microseconds())
}
