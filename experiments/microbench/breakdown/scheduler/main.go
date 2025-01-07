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

// NOTE: no ReplicaSet, just a template pod
// k8s: fallback=binding + blocking rpc
// kd: blocking rpc
func main() {
	var debug bool
	var baseline string
	var target string
	var nPods int

	// NOTE: should create the deployments ahead of time
	flag.BoolVar(&debug, "debug", false, "Enable debug log")
	flag.StringVar(&baseline, "baseline", "k8s", "Baseline for the experiment. Options: k8s, kd")
	flag.StringVar(&target, "target", "", "target ReplicaSet name")
	flag.IntVar(&nPods, "n", 100, "Total number of pods to scale up")
	flag.Parse()

	benchutil.SetupLogger(debug)

	if target == "" {
		log.Fatalf("must specify target ReplicaSet\n")
	}

	ctx := ctrl.SetupSignalHandler()
	mgr := benchutil.NewManagerOrDie()

	if baseline == "k8s" {
		run(ctx, mgr, target, nPods, true)
	} else if baseline == "kd" {
		run(ctx, mgr, target, nPods, false)
	} else {
		log.Fatalf("unknown baseline %s\n", baseline)
	}
}
