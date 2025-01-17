package main

import (
	"context"
	"fmt"
	"time"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/klog/v2"

	"google.golang.org/grpc"
	grpccodes "google.golang.org/grpc/codes"
	grpcstatus "google.golang.org/grpc/status"
	emptypb "google.golang.org/protobuf/types/known/emptypb"

	// Kubedirect
	kdctx "k8s.io/kubedirect/pkg/context"
	kdrpc "k8s.io/kubedirect/pkg/rpc"
	kdproto "k8s.io/kubedirect/pkg/rpc/proto"
	kdutil "k8s.io/kubedirect/pkg/util"
)

// impl kdrpc.Registerer
func (s *KubedirectServer) Register(sr grpc.ServiceRegistrar) {
	kdproto.RegisterKubeletServer(sr, s)
}

// impl kdproto.KubeletServer
func (s *KubedirectServer) Handshake(ctx context.Context, req *kdproto.HandshakeRequest) (*kdproto.NodeInfo, error) {
	kdLogger := kdutil.NewLogger(klog.FromContext(ctx)).WithHeader(req.Source + "->Handshake")
	kdLogger.Info(fmt.Sprintf("New epoch from %s: %s", req.Source, req.Epoch))
	holder := s.serverHub.Lock(req.Source, req.Epoch)
	defer holder.Unlock()
	msg := &kdproto.NodeInfo{
		Epoch: req.Epoch,
		Name:  s.nodeName,
		Pods:  s.inMemCache.AsPodInfosProto(),
	}
	return msg, nil
}

// impl kdproto.KubeletServer
func (s *KubedirectServer) BindPod(ctx context.Context, req *kdproto.PodBindingRequest) (*emptypb.Empty, error) {
	kdLogger := kdutil.NewLogger(klog.FromContext(ctx)).WithHeader(req.Source + "->BindPod")
	// get unnamed pod template
	template, err := kdutil.GetUnnamedTemplateFor(ctx, s.podLister, req.PodInfo.Owner.Namespace, req.PodInfo.Owner.Name, true)
	// err is probably due to:
	// 1. the template pod was deleted, which means the rs is also deleted
	// 2. the template pod is not yet added to the informer cache
	// notify the the sender to let it decide whether to retry
	if err != nil {
		return nil, grpcstatus.Errorf(grpccodes.NotFound,
			"%s: error getting template pod for %s/%s: %v",
			kdrpc.NoTemplatePodError, req.PodInfo.Owner.Namespace, req.PodInfo.Owner.Name, err,
		)
	}
	podInfo := kdctx.NewPodInfoFromBindingRequest(req, s.nodeName)
	// acquire shared lock on epoch
	holder, err := s.serverHub.RLock(req.Source, req.Epoch)
	if err != nil {
		return nil, grpcstatus.Errorf(grpccodes.InvalidArgument, "%s: %v", kdrpc.EpochMismatchError, err)
	}
	defer holder.RUnlock()
	// check if the pod already exists in in-mem cache
	if _, fresh := s.inMemCache.GetOrCreate(podInfo.Name, func() *kdctx.PodInfo { return podInfo }); !fresh {
		kdLogger.WARN("Pod already exists in in-mem cache, will ignore", "pod", podInfo)
		return &emptypb.Empty{}, nil
	}
	kdLogger.Info("Binding", "pod", podInfo)
	// NOTE: BindPod can be called multiple times for the same pod
	// the previous GetOrCreate check should avoid most duplicate deliveries
	// but they can still happen in case the in-mem cache is flushed by informer event handler and BindPod comes in again.
	// the PendingPod struct, as key of the workqueue, treats two identical in-mem pods as **different** (due to the pointer field)
	// but it is fine because the Create and UpdateStatus can only succeed once for a certain pod.
	pod := podInfo.AsPersistentPod(template)
	pending := NewPendingPodFromInMemCache(pod)
	s.queue.Add(pending)
	return &emptypb.Empty{}, nil
}

func (s *KubedirectServer) exposeManagedPod(ctx context.Context, kdLogger *kdutil.Logger, pod *corev1.Pod) {
	kdLogger = kdLogger.WithHeader("Expose")
	if pod.ResourceVersion != "" {
		kdLogger.WARN("Pod with resource version should not be exposed again")
		return
	}
	start := time.Now()
	tryCreate := func(ctx context.Context) (bool, error) {
		_, err := s.client.CoreV1().Pods(pod.Namespace).Create(ctx, pod, metav1.CreateOptions{})
		if err == nil {
			kdLogger.V(1).DEBUG("Successfully exposed pod", "elapsed", time.Since(start))
			return true, nil
		} else if apierrors.IsAlreadyExists(err) {
			kdLogger.V(2).WARN("Pod already exposed")
			return true, nil
		}
		kdLogger.WARN(fmt.Sprintf("Failed to expose pod: %v", err))
		return false, nil
	}
	wait.PollUntilContextCancel(ctx, time.Second, true, tryCreate)
}

func (s *KubedirectServer) getRefPodStatus(pod *corev1.Pod) (*corev1.PodStatus, error) {
	// find the reference pod with matching workload label from workload pool
	workloadSelector := labels.Set{
		WorkloadPoolLabel: pod.Labels["workload"],
	}
	workloadPool, err := s.podLister.Pods(s.nodeName).List(workloadSelector.AsSelectorPreValidated())
	if err != nil {
		return nil, fmt.Errorf("failed to list pods from workload pool: %v", err)
	}
	var matchingPod *corev1.Pod
	for _, pod := range workloadPool {
		if pod.Spec.NodeName == s.nodeName && kdutil.IsPodReady(pod) {
			matchingPod = pod
			break
		}
	}
	if matchingPod == nil {
		return nil, fmt.Errorf("no ready pod matches the workload")
	}

	refStatus := matchingPod.Status.DeepCopy()
	tweakRefPodStatus(refStatus)
	return refStatus, nil
}

func (s *KubedirectServer) simulateRefPodStatus(_ *corev1.Pod) *corev1.PodStatus {
	// simulate the reference pod status
	refStatus := &corev1.PodStatus{
		Phase: corev1.PodRunning,
		Conditions: []corev1.PodCondition{
			{
				Type:   corev1.PodReady,
				Status: corev1.ConditionTrue,
			},
			{
				Type:   corev1.ContainersReady,
				Status: corev1.ConditionTrue,
			},
			{
				Type:   corev1.PodScheduled,
				Status: corev1.ConditionTrue,
			},
		},
		HostIP: "127.0.0.1",
		PodIP:  "127.0.0.1",
	}
	tweakRefPodStatus(refStatus)
	return refStatus
}

// tweak timestamp fields in status
func tweakRefPodStatus(refStatus *corev1.PodStatus) {
	now := metav1.Now()
	refStatus.StartTime = &now
	for i := range refStatus.Conditions {
		cond := &refStatus.Conditions[i]
		cond.LastTransitionTime = now
	}
	for i := range refStatus.ContainerStatuses {
		status := &refStatus.ContainerStatuses[i]
		if running := status.State.Running; running != nil {
			running.StartedAt = now
		}
	}
	for i := range refStatus.InitContainerStatuses {
		status := &refStatus.InitContainerStatuses[i]
		if running := status.State.Running; running != nil {
			running.StartedAt = now
		}
	}
}

// assume from is mutable (i.e. deepcopied from cache)
func (s *KubedirectServer) markPodReady(ctx context.Context, pod *corev1.Pod, refStatus *corev1.PodStatus) error {
	pod.Status = *refStatus.DeepCopy()
	// update transition times
	now := metav1.Now()
	for i := range refStatus.Conditions {
		fromCond := &refStatus.Conditions[i]
		toCond := &pod.Status.Conditions[i]
		if !fromCond.LastProbeTime.IsZero() {
			toCond.LastProbeTime = now
		}
		if !fromCond.LastTransitionTime.IsZero() {
			toCond.LastTransitionTime = now
		}
	}
	_, err := s.client.CoreV1().Pods(pod.Namespace).UpdateStatus(ctx, pod, metav1.UpdateOptions{})
	return err
}
