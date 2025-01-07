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
	"log"

	ctrl "sigs.k8s.io/controller-runtime"

	// Kubedirect
	benchutil "github.com/tomquartz/kubedirect-bench/pkg/util"
)

// NOTE: use ReplicaSet
// k8s: no managed label
// kd: mark managed
func main() {
	var debug bool
	var baseline string
	var selector string
	var nPods int

	flag.BoolVar(&debug, "debug", false, "Enable debug log")
	flag.StringVar(&baseline, "baseline", "k8s", "Baseline for the experiment. Options: k8s, kd")
	flag.StringVar(&selector, "selector", "", "Select ReplicaSets with `workload=$selector` selector")
	flag.IntVar(&nPods, "n", 100, "Total number of pods to scale up")
	flag.Parse()

	benchutil.SetupLogger(debug)

	if selector == "" {
		log.Fatalf("must specify workload selector\n")
	}

	ctx := ctrl.SetupSignalHandler()
	mgr := benchutil.NewManagerOrDie()

	if baseline == "k8s" {
		runK8s(ctx, mgr, selector, nPods)
	} else if baseline == "kd" {
		runKd(ctx, mgr, selector, nPods)
	} else {
		log.Fatalf("unknown baseline %s\n", baseline)
	}
}
