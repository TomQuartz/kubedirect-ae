package main

import (
	"context"
	"fmt"
	"sync"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/klog/v2"

	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/manager"

	// Kubedirect
	benchutil "github.com/tomquartz/kubedirect-bench/pkg/util"
	kdctx "k8s.io/kubedirect/pkg/context"
	kdrpc "k8s.io/kubedirect/pkg/rpc"
	kdproto "k8s.io/kubedirect/pkg/rpc/proto"
	kdutil "k8s.io/kubedirect/pkg/util"
)

const (
	testClient   = "test"
	dialTimeout  = 5 * time.Second
	dialInterval = 1 * time.Second
)

func doKubeletHandshake(ctx context.Context, src string, dest string, client kdproto.KubeletClient) (string, error) {
	if src != testClient {
		panic(fmt.Sprintf("invalid source: expected %s, got %s", testClient, src))
	}
	msg := kdrpc.NewHandshakeRequest(testClient)
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

func newKubeletLister(_ context.Context, uncachedClient client.Client, nodeName string, requireAddrAnnotation bool) func(ctx context.Context) (addrs []string, err error) {
	// logger := klog.FromContext(ctx)
	// kdLogger := kdutil.NewLogger(logger).WithHeader(fmt.Sprintf("Lister/%s", nodeName))

	return func(ctx context.Context) ([]string, error) {
		node := &corev1.Node{}
		if err := uncachedClient.Get(ctx, client.ObjectKey{Name: nodeName}, node); err != nil {
			return nil, err
		}
		if addr, ok := node.Annotations[kdrpc.KubeletServiceAddrAnnotation]; ok && addr != "" {
			return []string{addr}, nil
		}
		if requireAddrAnnotation {
			return nil, fmt.Errorf("missing Kubelet service address annotation")
		}
		nodeIPs := []string{}
		for _, addr := range node.Status.Addresses {
			if addr.Type == corev1.NodeInternalIP {
				nodeIPs = append(nodeIPs, addr.Address)
			}
		}
		port := kdrpc.KubeletServicePort
		for i := range nodeIPs {
			nodeIPs[i] += port
		}
		return nodeIPs, nil
	}
}

func newPodInfos(ownerNamespace, ownerName string, nodeName string, nPods int) []*kdctx.PodInfo {
	podInfos := make([]*kdctx.PodInfo, nPods)
	creationTimestamp := metav1.Now()
	for i := 0; i < nPods; i++ {
		podInfos[i] = &kdctx.PodInfo{
			Namespace:         ownerNamespace,
			Name:              fmt.Sprintf("%s-%d-%d", ownerName, creationTimestamp.UnixNano(), i),
			OwnerName:         ownerName,
			NodeName:          nodeName,
			CreationTimestamp: creationTimestamp,
		}
	}
	return podInfos
}

func newBindingRequests(kdClient kdrpc.ClientInterface[kdproto.KubeletClient], podInfos []*kdctx.PodInfo) []*kdproto.PodBindingRequest {
	reqs := make([]*kdproto.PodBindingRequest, len(podInfos))
	for i, podInfo := range podInfos {
		reqs[i] = podInfo.RequestForBinding(kdClient)
	}
	return reqs
}

func run(ctx context.Context, mgr manager.Manager, nodeName string, target string, nPods int, useDefaultKubelet bool) {
	uncachedClient := benchutil.NewUncachedClientOrDie(mgr)

	kubeletLister := newKubeletLister(ctx, uncachedClient, nodeName, !useDefaultKubelet)
	kdClientHub := kdrpc.NewEventedClientHub(testClient, nodeName, kdproto.NewKubeletClient).
		WithHandshake(doKubeletHandshake).
		WithDialOptions(dialTimeout, dialInterval).
		WithAddrLister(kubeletLister)
	kdClientHub.Start(ctx)

	var kdClient kdrpc.ClientInterface[kdproto.KubeletClient]
	wait.PollUntilContextCancel(ctx, 1*time.Second, true, func(ctx context.Context) (bool, error) {
		kdClient = kdClientHub.Unwrap()
		if kdClient == nil {
			return false, nil
		}
		return true, nil
	})

	templatePod := &corev1.Pod{}
	templatePodKey := client.ObjectKey{
		Namespace: metav1.NamespaceDefault,
		Name:      target + "-template",
	}
	if err := uncachedClient.Get(ctx, templatePodKey, templatePod); err != nil {
		klog.Fatalf("Error getting template pod: %v", err)
	}

	if !kdutil.IsTemplatePod(templatePod) {
		klog.Fatalf("Invalid template pod: missing template pod label")
	}
	if owner := templatePod.Labels[kdutil.OwnerNameLabel]; owner != target {
		klog.Fatalf("Invalid owner label, expected %s, got %s", target, owner)
	}
	if useDefaultKubelet != kdutil.IsKubeletResponsibleFor(templatePod) {
		klog.Fatalf("Invalid template pod: pod-lifecycle label does not match kubelet implementation")
	}

	podInfos := newPodInfos(templatePod.Namespace, target, nodeName, nPods)
	reqs := newBindingRequests(kdClient, podInfos)

	// setup pod monitor
	monitor := NewPodMonitor(target)
	if err := monitor.SetupWithManager(ctx, mgr); err != nil {
		klog.Fatalf("Error creating monitor: %v", err)
	}

	wg := &sync.WaitGroup{}
	wg.Add(len(reqs))
	monitor.Watch(wg, podInfos)

	start := time.Now()
	for i := range reqs {
		go func(i int) {
			if _, err := kdClient.Client().BindPod(ctx, reqs[i]); err != nil {
				klog.Error(err, "Error binding pod", "pod", podInfos[i])
			}
		}(i)
	}
	wg.Wait()

	fmt.Printf("total: %v us\n", time.Since(start).Microseconds())
}
