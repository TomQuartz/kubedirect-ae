package main

import (
	"context"
	"fmt"
	"os"
	"sync"
	"sync/atomic"
	"time"

	appsv1 "k8s.io/api/apps/v1"
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
	wg      *sync.WaitGroup
	done    int32
	desired int
}

func NewExpectation(wg *sync.WaitGroup, desired int) *Expectation {
	return &Expectation{
		wg:      wg,
		desired: desired,
	}
}

func (s *Expectation) Desired() int {
	return s.desired
}

func (s *Expectation) Done() bool {
	if atomic.CompareAndSwapInt32(&s.done, 0, 1) {
		s.wg.Done()
		return true
	}
	return false
}

type ReplicaSetMonitor struct {
	selector     string
	expectations *kdutil.SharedMap[*Expectation]
}

func NewReplicaSetMonitor(selector string) *ReplicaSetMonitor {
	return &ReplicaSetMonitor{
		selector:     selector,
		expectations: kdutil.NewSharedMap[*Expectation](),
	}
}

func (m *ReplicaSetMonitor) Watch(wg *sync.WaitGroup, key string, desired int) {
	m.expectations.Set(key, NewExpectation(wg, desired))
}

func (m *ReplicaSetMonitor) SetupWithManager(ctx context.Context, mgr ctrl.Manager) error {
	logger := klog.FromContext(ctx)
	kdLogger := kdutil.NewLogger(logger).WithHeader("Monitor").WithHeader("ReplicaSet")

	return ctrl.NewControllerManagedBy(mgr).
		// WithOptions(controller.Options{
		// 	MaxConcurrentReconciles: 256,
		// }).
		Named("breakdown_replicaset").
		WithEventFilter(predicate.NewPredicateFuncs(m.FilterEvent)).
		Watches(&appsv1.ReplicaSet{}, handler.Funcs{
			CreateFunc: func(_ context.Context, ev event.CreateEvent, q CtrlWorkQueue) {
				rs := ev.Object.(*appsv1.ReplicaSet)
				m.OnReplicaSetCreated(kdLogger, rs)
			},
			UpdateFunc: func(_ context.Context, ev event.UpdateEvent, q CtrlWorkQueue) {
				old := ev.ObjectOld.(*appsv1.ReplicaSet)
				new := ev.ObjectNew.(*appsv1.ReplicaSet)
				m.OnReplicaSetUpdated(kdLogger, old, new)
			},
			DeleteFunc: func(_ context.Context, ev event.DeleteEvent, q CtrlWorkQueue) {
				rs := ev.Object.(*appsv1.ReplicaSet)
				m.OnReplicaSetDeleted(kdLogger, rs)
			},
			GenericFunc: func(_ context.Context, ev event.GenericEvent, q CtrlWorkQueue) {
				kdLogger.WARN("Generic event", "event", ev)
			},
		}).
		Complete(m)
}

func (m *ReplicaSetMonitor) FilterEvent(object client.Object) bool {
	return workload.IsWorkload(object) && object.GetLabels()["workload"] == m.selector
}

func (m *ReplicaSetMonitor) OnReplicaSetCreated(kdLogger *kdutil.Logger, rs *appsv1.ReplicaSet) {
	key := workload.KeyFromObject(rs)
	kdLogger.Info("Created", "key", key)
}

func (m *ReplicaSetMonitor) OnReplicaSetDeleted(kdLogger *kdutil.Logger, rs *appsv1.ReplicaSet) {
	key := workload.KeyFromObject(rs)
	kdLogger.Info("Deleted", "key", key)
	if exp, _ := m.expectations.Del(key); exp != nil {
		if exp.Done() {
			kdLogger.Info("Force done on deletion", "key", key)
		}
	}
}

func (m *ReplicaSetMonitor) OnReplicaSetUpdated(kdLogger *kdutil.Logger, old, new *appsv1.ReplicaSet) {
	key := workload.KeyFromObject(new)
	exp, _ := m.expectations.Get(key)
	if exp == nil {
		kdLogger.V(1).DEBUG("No expectation, skipping", "key", key)
		return
	}
	if new.Status.Replicas == *new.Spec.Replicas && *new.Spec.Replicas == int32(exp.Desired()) {
		if exp, _ := m.expectations.Del(key); exp != nil {
			if exp.Done() {
				kdLogger.Info("Expectation met", "key", key)
			}
		}
	}
}

func (m *ReplicaSetMonitor) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	return ctrl.Result{}, nil
}

func runK8s(ctx context.Context, mgr manager.Manager, selector string, nPods int) {
	monitor := NewReplicaSetMonitor(selector)
	if err := monitor.SetupWithManager(ctx, mgr); err != nil {
		klog.Fatalf("Error creating monitor: %v", err)
	}

	klog.Info("Starting manager")
	go func() {
		if err := mgr.Start(ctx); err != nil {
			klog.Fatalf("Error running manager: %v", err)
		}
	}()

	if !mgr.GetCache().WaitForCacheSync(ctx) {
		klog.Fatalf("Cannot syncing manager cache")
	}
	mgrClient := mgr.GetClient()

	targets := &appsv1.ReplicaSetList{}
	listOpts := append(
		[]client.ListOption{client.MatchingLabels{"workload": selector}},
		workload.CtrlListOptions...,
	)
	if err := mgrClient.List(ctx, targets, listOpts...); err != nil {
		klog.Fatalf("Error listing scaling targets: %v", err)
	}
	if len(targets.Items) == 0 {
		klog.Fatalf("No scaling targets")
	}

	nPodsPerTarget := nPods / len(targets.Items)
	if nPodsPerTarget == 0 {
		klog.Warning("The number of pods scaled per target is 0, resetting to 1")
		nPodsPerTarget = 1
	}

	wg := &sync.WaitGroup{}
	wg.Add(len(targets.Items))
	for i := range targets.Items {
		target := &targets.Items[i]
		monitor.Watch(wg, workload.KeyFromObject(target), nPodsPerTarget)
	}

	klog.Infof("Scaling up %d targets, %d pods each", len(targets.Items), nPodsPerTarget)
	start := time.Now()
	for i := range targets.Items {
		target := &targets.Items[i]
		*target.Spec.Replicas = int32(nPodsPerTarget)
		go func() {
			if err := mgrClient.Update(ctx, target); err != nil {
				klog.Error(err, "Error scaling up", "target", klog.KObj(target))
				os.Exit(1)
			}
		}()
	}
	wg.Wait()
	klog.Info("Done")

	fmt.Printf("total: %v us\n", time.Since(start).Microseconds())
}
