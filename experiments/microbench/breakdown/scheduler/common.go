package main

import (
	"context"
	"fmt"
	"os"
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
	kdrpc "k8s.io/kubedirect/pkg/rpc"
	kdproto "k8s.io/kubedirect/pkg/rpc/proto"
	kdutil "k8s.io/kubedirect/pkg/util"
)

const (
	testClient   = "test"
	schedService = "sched"
	dialTimeout  = 5 * time.Second
	dialInterval = 1 * time.Second
)

func doSchedulerHandshake(ctx context.Context, src string, dest string, client kdproto.SchedulerClient) (string, error) {
	if src != testClient {
		panic(fmt.Sprintf("invalid source: expected %s, got %s", testClient, src))
	}
	if dest != schedService {
		panic(fmt.Sprintf("invalid destination: expected %s, got %s", schedService, dest))
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

func run(ctx context.Context, mgr manager.Manager, target string, nPods int, fallback bool) {
	uncachedClient := benchutil.NewUncachedClientOrDie(mgr)

	schedulerLister := newSchedulerLister(ctx, uncachedClient)
	kdClientHub := kdrpc.NewEventedClientHub(testClient, schedService, kdproto.NewSchedulerClient).
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
	if fallback && !kdutil.IsFallbackBinding(templatePod) {
		klog.Fatalf("Invalid template pod: missing fallback binding label")
	}

	fakeReplicaSet := &appsv1.ReplicaSet{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: templatePod.Namespace,
			Name:      target,
		},
	}

	// IMPORTANT: use blocking request
	req := kdrpc.NewPodSchedulingRequest(kdClient, fakeReplicaSet, nPods)
	req.Blocking = true

	start := time.Now()
	if _, err := kdClient.Client().SchedulePods(ctx, req); err != nil {
		klog.Error(err, "Error scheduling pods", "target", klog.KObj(fakeReplicaSet))
		os.Exit(1)
	}

	fmt.Printf("total: %v us\n", time.Since(start).Microseconds())
}
