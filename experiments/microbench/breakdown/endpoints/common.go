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
	epService    = "ep"
	dialTimeout  = 5 * time.Second
	dialInterval = 1 * time.Second
)

func doEndpointsHandshake(ctx context.Context, src string, dest string, client kdproto.EndpointsListerClient) (string, error) {
	if src != testClient {
		panic(fmt.Sprintf("invalid source: expected %s, got %s", testClient, src))
	}
	if dest != epService {
		panic(fmt.Sprintf("invalid destination: expected %s, got %s", epService, dest))
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

func newEndpointsServiceLister(ctx context.Context, uncachedClient client.Client) func(ctx context.Context) (addrs []string, err error) {
	logger := klog.FromContext(ctx)
	kdLogger := kdutil.NewLogger(logger).WithHeader(fmt.Sprintf("Lister/%s", epService))

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
			addrs = append(addrs, destIP+kdrpc.EndpointsServicePort)
		}
		return
	}
}

func newEndpointsWatchRequest(client kdrpc.ClientInterface[kdproto.EndpointsListerClient], service *corev1.Service) *kdproto.EndpointsWatchRequest {
	return &kdproto.EndpointsWatchRequest{
		Source: client.ID(),
		Epoch:  client.Epoch(),
		Service: &kdproto.NamespacedName{
			Namespace: service.Namespace,
			Name:      service.Name,
		},
	}
}

func checkMetadata(obj metav1.Object, fallback bool) {
	if obj.GetLabels()["app"] != obj.GetName() {
		klog.Fatalf("ReplicaSet \"app\" label must match name: expected %s, got %s", obj.GetName(), obj.GetLabels()["app"])
	}
	if fallback != !kdutil.IsManaged(obj) {
		klog.Fatal("ReplicaSet should set fallback label if and only if in fallback mode")
	}
}

func run(ctx context.Context, mgr manager.Manager, selector string, nPods int, fallback bool) {
	uncachedClient := benchutil.NewUncachedClientOrDie(mgr)

	services := &corev1.ServiceList{}
	listOpts := append(
		[]client.ListOption{client.MatchingLabels{"workload": selector}},
		workload.CtrlListOptions...,
	)
	if err := uncachedClient.List(ctx, services, listOpts...); err != nil {
		klog.Fatalf("Error listing Services: %v", err)
	}
	if len(services.Items) == 0 {
		klog.Fatalf("No Service selected")
	}
	replicaSets := make([]*appsv1.ReplicaSet, 0, len(services.Items))
	for i := range services.Items {
		svc := &services.Items[i]
		checkMetadata(svc, fallback)
		rs := &appsv1.ReplicaSet{}
		// NOTE: rs name is the same as the svc
		if err := uncachedClient.Get(ctx, client.ObjectKeyFromObject(svc), rs); err != nil {
			klog.Fatalf("Error getting matching ReplicaSet for Service %v: %v", klog.KObj(svc), err)
		}
		checkMetadata(rs, fallback)
		if *rs.Spec.Replicas != 0 {
			klog.Fatalf("ReplicaSet %s/%s has non-zero initial replicas", rs.Namespace, rs.Name)
		}
		replicaSets = append(replicaSets, rs)
	}

	nPodsPerTarget := nPods / len(services.Items)
	if nPodsPerTarget == 0 {
		klog.Warning("The number of pods scaled per target is 0, resetting to 1")
		nPodsPerTarget = 1
	}

	// scale up replicas
	klog.Infof("Scaling up %d targets, %d pods each", len(replicaSets), nPodsPerTarget)
	for _, rs := range replicaSets {
		desiredScale := &autoscalingv1.Scale{Spec: autoscalingv1.ScaleSpec{Replicas: int32(nPodsPerTarget)}}
		if err := uncachedClient.SubResource("scale").Update(ctx, rs, client.WithSubResourceBody(desiredScale)); err != nil {
			klog.Fatalf("Error scaling up %v: %v", klog.KObj(rs), err)
		}
	}

	// wait for pods
	waitForReadyPods := func(ctx context.Context) (bool, error) {
		rsList := &appsv1.ReplicaSetList{}
		if err := uncachedClient.List(ctx, rsList, listOpts...); err != nil {
			klog.Fatalf("Error listing ReplicaSets: %v", err)
		}
		for i := range rsList.Items {
			rs := &rsList.Items[i]
			if rs.Status.ReadyReplicas != int32(nPodsPerTarget) {
				return false, nil
			}
		}
		return true, nil
	}
	if err := wait.PollUntilContextCancel(ctx, 5*time.Second, false, waitForReadyPods); err != nil {
		klog.Fatalf("Error waiting for ready pods: %v", err)
	}

	klog.Info("Starting KD client")
	epServiceLister := newEndpointsServiceLister(ctx, uncachedClient)
	kdClientHub := kdrpc.NewEventedClientHub(testClient, epService, kdproto.NewEndpointsListerClient).
		WithHandshake(doEndpointsHandshake).
		WithDialOptions(dialTimeout, dialInterval).
		WithAddrLister(epServiceLister)
	kdClientHub.Start(ctx)
	defer kdClientHub.Stop()

	var kdClient kdrpc.ClientInterface[kdproto.EndpointsListerClient]
	wait.PollUntilContextCancel(ctx, 1*time.Second, true, func(ctx context.Context) (bool, error) {
		kdClient = kdClientHub.Unwrap()
		if kdClient == nil {
			return false, nil
		}
		return true, nil
	})

	klog.Infof("Watching Endpoints of %d Services, expecting %d pods each", len(services.Items), nPodsPerTarget)
	watchGroup := &sync.WaitGroup{}
	watchGroup.Add(len(services.Items))
	nFinished := int32(0)
	for i := range services.Items {
		service := &services.Items[i]
		go func() {
			defer watchGroup.Done()
			if _, err := kdClient.Client().Watch(ctx, newEndpointsWatchRequest(kdClient, service)); err != nil {
				klog.ErrorS(err, "Error watching Service", "target", klog.KObj(service))
			} else {
				atomic.AddInt32(&nFinished, 1)
			}
		}()
	}

	// must wait till all watch callbacks are installed
	time.Sleep(30 * time.Second)

	klog.Infof("Populating Endpoints for %d Services, %d pods each", len(services.Items), nPodsPerTarget)
	updateGroup := &sync.WaitGroup{}
	updateGroup.Add(len(services.Items))
	nUpdated := int32(0)
	start := time.Now()
	for i := range services.Items {
		service := &services.Items[i]
		go func() {
			defer updateGroup.Done()
			service.Spec.Selector = map[string]string{
				"app":      service.Name,
				"workload": selector,
			}
			if err := uncachedClient.Update(ctx, service); err != nil {
				klog.ErrorS(err, "Error updating Serive spec.selector", "target", klog.KObj(service))
			} else {
				atomic.AddInt32(&nUpdated, 1)
			}
		}()
	}

	// wait for populating process
	updateGroup.Wait()
	select {
	case <-ctx.Done():
		klog.Info("Context cancelled")
		return
	default:
	}
	fmt.Printf("Targets scaled %d/%d in %v\n", atomic.LoadInt32(&nUpdated), len(services.Items), time.Since(start))

	// wait for watchers
	watchGroup.Wait()
	select {
	case <-ctx.Done():
		klog.Info("Context cancelled")
		return
	default:
	}
	fmt.Printf("RPC returned %d/%d in %v\n", atomic.LoadInt32(&nFinished), len(services.Items), time.Since(start))

	fmt.Printf("total: %v us\n", time.Since(start).Microseconds())
}
