package main

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	autoscalingv1 "k8s.io/api/autoscaling/v1"
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
	testClient   = "test"
	dpService    = "dp"
	dialTimeout  = 5 * time.Second
	dialInterval = 1 * time.Second
)

func doDeploymentHandshake(ctx context.Context, src string, dest string, client kdproto.DeploymentClient) (string, error) {
	if src != testClient {
		panic(fmt.Sprintf("invalid source: expected %s, got %s", testClient, src))
	}
	if dest != dpService {
		panic(fmt.Sprintf("invalid destination: expected %s, got %s", dpService, dest))
	}
	msg := kdrpc.NewHandshakeRequest(src, dest)
	epoch := msg.Epoch
	resp, err := client.Handshake(ctx, msg)
	if err != nil {
		return "", err
	}
	if epoch != resp.Epoch {
		return "", fmt.Errorf("epoch mismatch: expected %s, got %s", epoch, resp.Epoch)
	}
	logger := klog.FromContext(ctx)
	kdLogger := kdutil.NewLogger(logger).WithHeader(fmt.Sprintf("Handshake->%v", dest))
	kdLogger.Info("Handshake done", "epoch", epoch)
	return epoch, nil
}

func newDeploymentServiceLister(ctx context.Context, uncachedClient client.Client) func(ctx context.Context) (addrs []string, err error) {
	logger := klog.FromContext(ctx)
	kdLogger := kdutil.NewLogger(logger).WithHeader(fmt.Sprintf("Lister/%s", dpService))

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
			addrs = append(addrs, destIP+kdrpc.DeploymentServicePort)
		}
		return
	}
}

func newDeploymentWatchRequest(client kdrpc.ClientInterface[kdproto.DeploymentClient], dp *appsv1.Deployment, replicas int) *kdproto.DeploymentWatchRequest {
	return &kdproto.DeploymentWatchRequest{
		Source: client.ID(),
		Epoch:  client.Epoch(),
		Target: &kdproto.NamespacedName{
			Namespace: dp.Namespace,
			Name:      dp.Name,
		},
		Replicas: int32(replicas),
	}
}

func run(ctx context.Context, mgr manager.Manager, selector string, nPods int, fallback bool) {
	uncachedClient := benchutil.NewUncachedClientOrDie(mgr)

	targets := &appsv1.DeploymentList{}
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
		dp := &targets.Items[i]
		if fallback != !kdutil.IsManaged(dp) {
			klog.Fatal("Deployment must not be managed in fallback mode and vice versa")
		}
	}

	waitForReplicaSets := func(ctx context.Context) (bool, error) {
		rsList := &appsv1.ReplicaSetList{}
		if err := uncachedClient.List(ctx, rsList, listOpts...); err != nil {
			klog.Fatalf("Error listing ReplicaSets: %v", err)
		}
		for i := range rsList.Items {
			rs := &rsList.Items[i]
			if metav1.GetControllerOfNoCopy(rs) == nil {
				klog.Fatalf("ReplicaSet %s/%s has no owner", rs.Namespace, rs.Name)
			}
		}
		return len(rsList.Items) == len(targets.Items), nil
	}
	if err := wait.PollUntilContextCancel(ctx, 5*time.Second, false, waitForReplicaSets); err != nil {
		klog.Fatalf("Error waiting for ReplicaSets: %v", err)
	}

	// wait for rate limiter
	<-time.After(15 * time.Second)

	nPodsPerTarget := nPods / len(targets.Items)
	if nPodsPerTarget == 0 {
		klog.Warning("The number of pods scaled per target is 0, resetting to 1")
		nPodsPerTarget = 1
	}

	klog.Info("Starting KD client")
	dpServiceLister := newDeploymentServiceLister(ctx, uncachedClient)
	kdClientHub := kdrpc.NewEventedClientHub(testClient, dpService, kdproto.NewDeploymentClient).
		WithHandshake(doDeploymentHandshake).
		WithDialOptions(dialTimeout, dialInterval).
		WithAddrLister(dpServiceLister)
	kdClientHub.Start(ctx)
	defer kdClientHub.Stop()

	var kdClient kdrpc.ClientInterface[kdproto.DeploymentClient]
	wait.PollUntilContextCancel(ctx, 1*time.Second, true, func(ctx context.Context) (bool, error) {
		kdClient = kdClientHub.Unwrap()
		if kdClient == nil {
			return false, nil
		}
		return true, nil
	})

	klog.Infof("Watching %d Deployments, expecting %d pods each", len(targets.Items), nPodsPerTarget)
	watchGroup := &sync.WaitGroup{}
	watchGroup.Add(len(targets.Items))
	nFinished := int32(0)
	for i := range targets.Items {
		dp := &targets.Items[i]
		go func() {
			defer watchGroup.Done()
			if _, err := kdClient.Client().Watch(ctx, newDeploymentWatchRequest(kdClient, dp, nPodsPerTarget)); err != nil {
				klog.ErrorS(err, "Error watching Deployment", "target", klog.KObj(dp))
			} else {
				atomic.AddInt32(&nFinished, 1)
			}
		}()
	}

	// must wait till all watch callbacks are installed
	time.Sleep(30 * time.Second)

	klog.Infof("Scaling up %d targets, %d pods each", len(targets.Items), nPodsPerTarget)
	scaleGroup := &sync.WaitGroup{}
	scaleGroup.Add(len(targets.Items))
	nScaled := int32(0)
	start := time.Now()
	for i := range targets.Items {
		target := &targets.Items[i]
		go func() {
			defer scaleGroup.Done()
			desiredScale := &autoscalingv1.Scale{Spec: autoscalingv1.ScaleSpec{Replicas: int32(nPodsPerTarget)}}
			if err := uncachedClient.SubResource("scale").Update(ctx, target, client.WithSubResourceBody(desiredScale)); err != nil {
				klog.ErrorS(err, "Error scaling up", "target", klog.KObj(target))
			} else {
				atomic.AddInt32(&nScaled, 1)
			}
		}()
	}

	// wait for scaling process
	scaleGroup.Wait()
	select {
	case <-ctx.Done():
		klog.Info("Context cancelled")
		return
	default:
	}
	fmt.Printf("Targets scaled %d/%d in %v\n", atomic.LoadInt32(&nScaled), len(targets.Items), time.Since(start))

	// wait for watchers
	watchGroup.Wait()
	select {
	case <-ctx.Done():
		klog.Info("Context cancelled")
		return
	default:
	}
	fmt.Printf("RPC returned %d/%d in %v\n", atomic.LoadInt32(&nFinished), len(targets.Items), time.Since(start))

	fmt.Printf("total: %v us\n", time.Since(start).Microseconds())
}
