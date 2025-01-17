package gateway

import (
	"context"
	"fmt"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/klog/v2"
	knclient "knative.dev/serving/pkg/client/clientset/versioned"
	"sigs.k8s.io/controller-runtime/pkg/manager"

	// Kubedirect
	"github.com/tomquartz/kubedirect-bench/pkg/autoscaler"
	"github.com/tomquartz/kubedirect-bench/pkg/gateway/dispatcher"
	"github.com/tomquartz/kubedirect-bench/pkg/workload"
)

type knativeGateway struct {
	*gatewayImpl
	*knclient.Clientset
	dispatchers map[string]*dispatcher.KnServiceDispatcher
}

func NewKnativeGateway() (*knativeGateway, error) {
	g := &knativeGateway{
		dispatchers: make(map[string]*dispatcher.KnServiceDispatcher),
	}
	g.gatewayImpl = newGatewayImpl(g.onReqIn, g.onReqOut)
	return g, nil
}

var _ Gateway = &knativeGateway{}

func (g *knativeGateway) onReqIn(req *workload.Request) {}

func (g *knativeGateway) onReqOut(req *workload.Response) {}

func (g *knativeGateway) Start(ctx context.Context) error {
	for key, dispatcher := range g.dispatchers {
		go g.relay(ctx, key)
		go dispatcher.Run(ctx)
	}
	return nil
}

func (g *knativeGateway) Autoscaler() autoscaler.Autoscaler {
	return nil
}

func (g *knativeGateway) SetUpWithManager(ctx context.Context, mgr manager.Manager) error {
	logger := klog.FromContext(ctx).WithValues("gateway", "knative")

	g.Clientset = knclient.NewForConfigOrDie(mgr.GetConfig())

	// assume deployment and ksvc has the same "app" label
	knServices, err := g.ServingV1().Services(metav1.NamespaceAll).List(ctx, workload.MetaV1ListOptionsForTrace)
	if err != nil {
		return fmt.Errorf("error listing kn services in knative gateway: %v", err)
	}
	keys := []string{}
	for i := range knServices.Items {
		service := &knServices.Items[i]
		key := workload.KeyFromObject(service)
		keys = append(keys, key)
		logger.V(1).Info("Registering knative service", "key", key)
		// register channel
		g.register(key)
		reqBuffer, resBuffer := g.internalBuffers(key)
		// create dispatcher
		url := service.Status.URL.String()
		kd, err := dispatcher.NewKnServiceDispatcher(ctx, key, reqBuffer, resBuffer, url)
		if err != nil {
			return fmt.Errorf("Failed to create knative service dispatcher for %v (%v): %v", klog.KObj(service), url, err)
		}
		g.dispatchers[key] = kd
	}
	logger.Info("All knative services registered", "total", len(g.dispatchers))
	return nil
}
