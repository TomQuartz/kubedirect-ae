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
	kdctx "k8s.io/kubedirect/pkg/context"
	kdrpc "k8s.io/kubedirect/pkg/rpc"
	kdproto "k8s.io/kubedirect/pkg/rpc/proto"
	kdutil "k8s.io/kubedirect/pkg/util"
)

const (
	testClient   = "test"
	rsService    = "rs"
	dialTimeout  = 5 * time.Second
	dialInterval = 1 * time.Second
)

func doReplicaSetHandshake(ctx context.Context, src string, dest string, client kdproto.ReplicaSetClient) (string, error) {
	if src != testClient {
		panic(fmt.Sprintf("invalid source: expected %s, got %s", testClient, src))
	}
	if dest != rsService {
		panic(fmt.Sprintf("invalid destination: expected %s, got %s", rsService, dest))
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

func newReplicaSetServiceLister(ctx context.Context, uncachedClient client.Client) func(ctx context.Context) (addrs []string, err error) {
	logger := klog.FromContext(ctx)
	kdLogger := kdutil.NewLogger(logger).WithHeader(fmt.Sprintf("Lister/%s", rsService))

	return func(ctx context.Context) (addrs []string, err error) {
		ctrlMgrs := &corev1.PodList{}
		err = uncachedClient.List(ctx, ctrlMgrs,
			client.InNamespace(metav1.NamespaceSystem),
			client.MatchingLabels{"component": "kube-controller-manager"},
		)
		if err != nil {
			kdLogger.Error(err, "Failed to list controller managers")
			return
		}
		if len(ctrlMgrs.Items) == 0 {
			kdLogger.WARN("No controller manager found, will retry later")
			return
		}
		if len(ctrlMgrs.Items) > 1 {
			kdLogger.WARN("Multiple controller managers found, will use the first available one")
		}
		for i := range ctrlMgrs.Items {
			ctrlMgr := &ctrlMgrs.Items[i]
			if !kdutil.IsPodReady(ctrlMgr) {
				kdLogger.WARN(fmt.Sprintf("Controller manager %v is not ready", klog.KObj(ctrlMgr)))
				continue
			}
			destIP := ctrlMgr.Status.PodIP
			addrs = append(addrs, destIP+kdrpc.ReplicaSetServicePort)
		}
		return
	}
}

func run(ctx context.Context, mgr manager.Manager, selector string, nPods int, fallback bool) {
	uncachedClient := benchutil.NewUncachedClientOrDie(mgr)

	targets := &appsv1.ReplicaSetList{}
	listOpts := append(
		[]client.ListOption{client.MatchingLabels{"workload": selector}},
		workload.CtrlListOptions...,
	)
	if err := uncachedClient.List(ctx, targets, listOpts...); err != nil {
		klog.Fatalf("Error listing scaling targets: %v", err)
	}
	if len(targets.Items) == 0 {
		klog.Fatalf("No scaling targets selected")
	}
	for i := range targets.Items {
		rs := &targets.Items[i]
		if !kdutil.IsManaged(rs) {
			klog.Fatalf("ReplicaSet must be managed in this breakdown test")
		}
		if fallback != kdutil.IsFallbackScaling(rs) {
			klog.Fatal("ReplicaSet should set fallback label if and only if in fallback mode")
		}
	}

	nPodsPerTarget := nPods / len(targets.Items)
	if nPodsPerTarget == 0 {
		klog.Warning("The number of pods scaled per target is 0, resetting to 1")
		nPodsPerTarget = 1
	}

	klog.Info("Starting KD client")
	rsServiceLister := newReplicaSetServiceLister(ctx, uncachedClient)
	kdClientHub := kdrpc.NewEventedClientHub(testClient, rsService, kdproto.NewReplicaSetClient).
		WithHandshake(doReplicaSetHandshake).
		WithDialOptions(dialTimeout, dialInterval).
		WithAddrLister(rsServiceLister)
	kdClientHub.Start(ctx)
	defer kdClientHub.Stop()

	var kdClient kdrpc.ClientInterface[kdproto.ReplicaSetClient]
	wait.PollUntilContextCancel(ctx, 1*time.Second, true, func(ctx context.Context) (bool, error) {
		kdClient = kdClientHub.Unwrap()
		if kdClient == nil {
			return false, nil
		}
		return true, nil
	})

	klog.Infof("Scaling up %d targets, %d pods each", len(targets.Items), nPodsPerTarget)
	wg := &sync.WaitGroup{}
	wg.Add(len(targets.Items))
	nScaled := int32(0)
	start := time.Now()
	for i := range targets.Items {
		target := &targets.Items[i]
		*target.Spec.Replicas = int32(nPodsPerTarget)
		go func() {
			defer wg.Done()
			// IMPORTANT: use blocking request
			req := kdctx.NewReplicaSetScalingRequest(kdClient, target)
			req.Blocking = true
			if _, err := kdClient.Client().Scale(ctx, req); err != nil {
				klog.ErrorS(err, "Error scaling up", "target", klog.KObj(target))
			} else {
				atomic.AddInt32(&nScaled, 1)
			}
		}()
	}
	wg.Wait()
	select {
	case <-ctx.Done():
		klog.Info("Context cancelled")
		return
	default:
	}
	fmt.Printf("RPC returned %d/%d in %v\n", atomic.LoadInt32(&nScaled), len(targets.Items), time.Since(start))

	fmt.Printf("total: %v us\n", time.Since(start).Microseconds())
}
