package util

import (
	"log"

	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/manager"
)

const (
	defaultQPS   = 10000
	defaultBurst = 20000
)

// Setup a temporary client before manager starts
func NewUncachedClientOrDie(mgr manager.Manager) client.Client {
	c, err := client.New(mgr.GetConfig(), client.Options{
		Scheme: mgr.GetScheme(),
		Mapper: mgr.GetRESTMapper(),
	})
	if err != nil {
		log.Fatalf("Error creating uncached client: %v", err)
	}
	return c
}

func NewManagerOrDie() manager.Manager {
	kubeConfig := ctrl.GetConfigOrDie()
	kubeConfig.QPS = defaultQPS
	kubeConfig.Burst = defaultBurst

	ctrlOptions := ctrl.Options{
		HealthProbeBindAddress: "0",
		LeaderElection:         false,
	}
	ctrlOptions.Metrics.BindAddress = "0"

	mgr, err := ctrl.NewManager(kubeConfig, ctrlOptions)
	if err != nil {
		log.Fatalf("Error creating manager: %v", err)
	}
	return mgr
}
