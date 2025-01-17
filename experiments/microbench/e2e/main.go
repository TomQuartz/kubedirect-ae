/*
Copyright 2024.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package main

import (
	"flag"

	"k8s.io/klog/v2"
	ctrl "sigs.k8s.io/controller-runtime"

	// Kubedirect
	benchutil "github.com/tomquartz/kubedirect-bench/pkg/util"
)

func init() {
	klog.InitFlags(nil)
}

// NOTE: use Deployment
// k8s: no managed label
// k8s+: no managed label + pod-lifecycle=custom label(in the pod template) + custom kubelet
// kd: managed label
// kd+: managed label + pod-lifecycle=custom label(in the pod template) + custom kubelet

// custom kubelet:
// 1. daemonset for the actual workload pods
// 2. run the custom kubelets (override kubelet service annotation)
func main() {
	var baseline string
	var selector string
	var nPods int

	// NOTE: should create the deployments ahead of time
	flag.StringVar(&baseline, "baseline", "k8s", "Baseline for the experiment. Options: k8s, k8s+, kd, kd+")
	flag.StringVar(&selector, "selector", "test", "Select Deployments with `workload=$selector` selector")
	flag.IntVar(&nPods, "n", 100, "Total number of pods to scale up")
	flag.Parse()

	ctx := ctrl.SetupSignalHandler()
	ctrl.SetLogger(klog.Background())

	if selector == "" {
		klog.Fatalf("must specify workload selector")
	}

	mgr := benchutil.NewManagerOrDie()

	switch baseline {
	case "k8s", "k8s+", "kd", "kd+":
	default:
		klog.Fatalf("unknown baseline %s", baseline)
	}

	// We do not check on the various specs as per the NOTEs because it's too complicated to do so in code
	run(ctx, mgr, selector, nPods)
}
