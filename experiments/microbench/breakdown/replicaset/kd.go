package main

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/klog/v2"

	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/manager"

	// Kubedirect
	benchutil "github.com/tomquartz/kubedirect-bench/pkg/util"
	"github.com/tomquartz/kubedirect-bench/pkg/workload"
	kdrpc "k8s.io/kubedirect/pkg/rpc"
	kdproto "k8s.io/kubedirect/pkg/rpc/proto"
	kdutil "k8s.io/kubedirect/pkg/util"
)

const (
	testClient           = "test"
	schedService         = "sched"
	SchedulerServicePort = ":24120"
	dialTimeout          = 5 * time.Second
	dialInterval         = 1 * time.Second
)

func doSchedulerHandshake(ctx context.Context, src string, dest string, client kdproto.SchedulerClient) (string, error) {
	if src != testClient {
		panic(fmt.Sprintf("invalid source: expected %s, got %s", testClient, src))
	}
	if dest != schedService {
		panic(fmt.Sprintf("invalid destination: expected %s, got %s", schedService, dest))
	}
	msg := kdrpc.NewHandshakeRequest(src, dest)
	epoch := msg.Epoch
	rsInfos, err := client.Handshake(ctx, msg)
	if err != nil {
		return "", err
	}
	if epoch != rsInfos.Epoch {
		return "", fmt.Errorf("epoch mismatch: expected %s, got %s", epoch, rsInfos.Epoch)
	}
	logger := klog.FromContext(ctx)
	kdLogger := kdutil.NewLogger(logger).WithHeader(fmt.Sprintf("Handshake->%v", dest))
	kdLogger.Info("Handshake done", "epoch", epoch)
	return epoch, nil
}

func newSchedulerLister(ctx context.Context, uncachedClient client.Client) func(ctx context.Context) (addrs []string, err error) {
	logger := klog.FromContext(ctx)
	kdLogger := kdutil.NewLogger(logger).WithHeader(fmt.Sprintf("Lister/%s", schedService))

	return func(ctx context.Context) (addrs []string, err error) {
		schedulers := &corev1.PodList{}
		err = uncachedClient.List(ctx, schedulers,
			client.InNamespace(metav1.NamespaceSystem),
			client.MatchingLabels{"component": "kube-scheduler"},
		)
		if err != nil {
			kdLogger.Error(err, "Failed to list schedulers")
			return
		}
		if len(schedulers.Items) == 0 {
			kdLogger.WARN("No schedulers found, will retry later")
			return
		}
		if len(schedulers.Items) > 1 {
			kdLogger.WARN("Multiple schedulers found, will use the first available one")
		}
		for i := range schedulers.Items {
			sched := &schedulers.Items[i]
			if !kdutil.IsPodReady(sched) {
				kdLogger.WARN("Scheduler is not ready", "scheduler", klog.KObj(sched))
				continue
			}
			destIP := sched.Status.PodIP
			addrs = append(addrs, destIP+kdrpc.SchedulerServicePort)
		}
		return
	}
}

func runKd(ctx context.Context, mgr manager.Manager, selector string, nPods int) {
	uncachedClient := benchutil.NewUncachedClientOrDie(mgr)

	klog.Info("Starting KD client")
	schedulerLister := newSchedulerLister(ctx, uncachedClient)
	kdClientHub := kdrpc.NewEventedClientHub("test", "sched", kdproto.NewSchedulerClient).
		WithHandshake(doSchedulerHandshake).
		WithDialOptions(dialTimeout, dialInterval).
		WithAddrLister(schedulerLister)
	kdClientHub.Start(ctx)

	var kdClient kdrpc.ClientInterface[kdproto.SchedulerClient]
	wait.PollUntilContextCancel(ctx, 1*time.Second, true, func(ctx context.Context) (bool, error) {
		kdClient = kdClientHub.Unwrap()
		if kdClient == nil {
			return false, nil
		}
		return true, nil
	})

	targets := &appsv1.ReplicaSetList{}
	listOpts := append(
		[]client.ListOption{client.MatchingLabels{"workload": selector}},
		workload.CtrlListOptions...,
	)
	if err := uncachedClient.List(ctx, targets, listOpts...); err != nil {
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

	klog.Infof("Scaling up %d targets, %d pods each", len(targets.Items), nPodsPerTarget)
	var wg sync.WaitGroup
	wg.Add(len(targets.Items))
	start := time.Now()
	errs := int32(0)
	for i := range targets.Items {
		target := &targets.Items[i]
		go func() {
			defer wg.Done()
			req := kdrpc.NewPodSchedulingRequest(kdClient, target, nPodsPerTarget)
			if _, err := kdClient.Client().SchedulePods(ctx, req); err != nil {
				klog.Error(err, "Error scaling up", "target", klog.KObj(target))
				atomic.AddInt32(&errs, 1)
				// os.Exit(1)
			}
		}()
	}
	wg.Wait()
	klog.Info("Done")

	nErrs := int(atomic.LoadInt32(&errs))
	fmt.Printf("total: %v us (%d/%d)\n", time.Since(start).Microseconds(), len(targets.Items)-nErrs, len(targets.Items))
}
