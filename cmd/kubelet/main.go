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
	"os"
	"time"

	"k8s.io/klog/v2"
	ctrl "sigs.k8s.io/controller-runtime"

	// Kubedirect
	benchutil "github.com/tomquartz/kubedirect-bench/pkg/util"
)

func init() {
	klog.InitFlags(nil)
}

func main() {
	var node string
	var simulate bool
	var patch bool
	var readyDelayMilliseconds int

	flag.StringVar(&node, "node", "", "Node name this kubelet binds to. Default to hostname if not set")
	flag.BoolVar(&simulate, "simulate", false, "If true, report pod readiness without binding to real containers")
	flag.BoolVar(&patch, "patch", true, "If true, use patch instead of update to mark pod ready")
	flag.IntVar(&readyDelayMilliseconds, "ready-after", 100, "Delay in ms before kubelet reports pod ready")
	flag.Parse()

	if node == "" {
		hostName, err := os.Hostname()
		if err != nil {
			klog.Fatalf("Failed to get hostname: %v", err)
		}
		node = hostName
	}

	ctx := ctrl.SetupSignalHandler()
	ctrl.SetLogger(klog.Background())
	kubeClient := benchutil.NewClientsetOrDie()

	kdServer := NewKubedirectServer(kubeClient, node).
		WithReadyDelay(time.Duration(readyDelayMilliseconds) * time.Millisecond)
	if simulate {
		kdServer.Simulate()
	}
	if patch {
		kdServer.UsePatch()
	}

	klog.InfoS("Starting custom kubelet server", "node", node, "simulate", simulate, "ready-after", readyDelayMilliseconds, "patch", patch)
	if err := kdServer.ListenAndServe(ctx); err != nil {
		klog.Fatalf("Failed to listen & serve: %v", err)
	}
	klog.Info("Server stopped")
}
