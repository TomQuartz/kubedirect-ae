package main

import (
	"context"
	"fmt"
	"log"
	"sync"
	"sync/atomic"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
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
	kdLogger := kdutil.NewLogger(logger).WithHeader("Monitor").WithHeader("Scaling")

	return ctrl.NewControllerManagedBy(mgr).
		// WithOptions(controller.Options{
		// 	MaxConcurrentReconciles: 256,
		// }).
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
	labels := object.GetLabels()
	return labels["app"] != "" && labels["workload"] == m.selector
}

func (m *ReplicaSetMonitor) OnReplicaSetCreated(kdLogger *kdutil.Logger, rs *appsv1.ReplicaSet) {
	key := workload.KeyFromObject(rs)
	kdLogger.Info("Created", "key", key)
}

func (m *ReplicaSetMonitor) OnReplicaSetDeleted(kdLogger *kdutil.Logger, rs *appsv1.ReplicaSet) {
	key := workload.KeyFromObject(rs)
	kdLogger.Info("Deleted", "key", key)
	m.expectations.Del(key, func(exp *Expectation) {
		if exp.Done() {
			kdLogger.Info("Force done on deletion", "key", key)
		}
	})
}

func (m *ReplicaSetMonitor) OnReplicaSetUpdated(kdLogger *kdutil.Logger, old, new *appsv1.ReplicaSet) {
	key := workload.KeyFromObject(new)
	exp, _ := m.expectations.Get(key)
	if exp == nil {
		kdLogger.V(1).DEBUG("No expectation, skipping", "key", key)
		return
	}
	if *new.Spec.Replicas == int32(exp.Desired()) {
		m.expectations.Del(key, func(exp *Expectation) {
			if exp.Done() {
				kdLogger.Info("Expectation met", "key", key)
			}
		})
	}
}

func (m *ReplicaSetMonitor) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	return ctrl.Result{}, nil
}

func run(ctx context.Context, mgr manager.Manager, selector string, nPods int, fallback bool) {
	monitor := NewReplicaSetMonitor(selector)
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

	monitorTargets := &appsv1.ReplicaSetList{}
	mgrClient := mgr.GetClient()
	listOpts := append(
		[]client.ListOption{client.MatchingLabels{"workload": selector}},
		workload.CtrlListOptions...,
	)
	if err := mgrClient.List(ctx, monitorTargets, listOpts...); err != nil {
		log.Fatalf("Error listing ReplicaSet: %v\n", err)
	}
	if len(monitorTargets.Items) == 0 {
		log.Fatalf("No ReplicaSet selected\n")
	}

	targetKeys := sets.New[string]()
	targets := []*appsv1.Deployment{}
	for i := range monitorTargets.Items {
		rs := &monitorTargets.Items[i]
		if fallback && kdutil.IsManaged(rs) {
			log.Fatalf("ReplicaSet %s/%s must not be managed in fallback mode\n", rs.Namespace, rs.Name)
		}
		key := workload.KeyFromObject(rs)
		ownerRef := metav1.GetControllerOfNoCopy(rs)
		if ownerRef == nil {
			log.Fatalf("ReplicaSet %s/%s has no owner\n", rs.Namespace, rs.Name)
		}
		owner := &appsv1.Deployment{}
		if err := mgrClient.Get(ctx, workload.NamespacedNameFromKey(key), owner); err != nil {
			log.Fatalf("Error getting Deployment target %s from ReplicaSet %s: %v\n", key, klog.KObj(rs), err)
		}
		if targetKeys.Has(key) {
			log.Fatalf("Duplicate Deployment target %s from ReplicaSet %s/%s\n", key, rs.Namespace, rs.Name)
		}
		targetKeys.Insert(key)
		targets = append(targets, owner)
	}

	nPodsPerTarget := nPods / len(targets)
	if nPodsPerTarget == 0 {
		log.Println("[WARN] The number of pods scaled per target is 0, resetting to 1")
		nPodsPerTarget = 1
	}

	wg := &sync.WaitGroup{}
	wg.Add(len(targets))
	for i := range targets {
		target := targets[i]
		monitor.Watch(wg, workload.KeyFromObject(target), nPodsPerTarget)
	}

	logger := klog.FromContext(ctx)
	start := time.Now()
	for i := range targets {
		target := targets[i]
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
