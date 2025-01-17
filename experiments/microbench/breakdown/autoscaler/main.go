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

// NOTE: use Deployment, vary # Deployments (nPods = # Deployments)
// k8s: no managed label
// kd: mark managed
func main() {
	var baseline string
	var selector string
	var nPods int

	flag.StringVar(&baseline, "baseline", "k8s", "Baseline for the experiment. Options: k8s, kd")
	flag.StringVar(&selector, "selector", "", "Select Deployments with `workload=$selector` selector")
	flag.IntVar(&nPods, "n", 0, "Total number of pods to scale up. If 0, equal to the number of selected Deployments")
	flag.Parse()

	ctx := ctrl.SetupSignalHandler()
	ctrl.SetLogger(klog.Background())

	if selector == "" {
		klog.Fatalf("must specify workload selector")
	}

	mgr := benchutil.NewManagerOrDie()

	if baseline == "k8s" {
		run(ctx, mgr, selector, nPods, true)
	} else if baseline == "kd" {
		run(ctx, mgr, selector, nPods, false)
	} else {
		klog.Fatalf("unknown baseline %s", baseline)
	}
}
