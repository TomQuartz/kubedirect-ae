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
	"context"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"time"

	"k8s.io/klog/v2"
	ctrl "sigs.k8s.io/controller-runtime"

	// Kubedirect
	"github.com/tomquartz/kubedirect-bench/pkg/backend"
	"github.com/tomquartz/kubedirect-bench/pkg/gateway"
	"github.com/tomquartz/kubedirect-bench/pkg/replay"
	benchutil "github.com/tomquartz/kubedirect-bench/pkg/util"
)

var baseDir string

func init() {
	klog.InitFlags(nil)
	_, thisFile, _, _ := runtime.Caller(0)
	baseDir = filepath.Dir(thisFile)
}

var backendFramework string
var gatewayFramework string
var autoscalerFramework string
var autoscalerConfig string
var traceLoaderConfig string
var outputPath string
var requestTimeoutSeconds int

// var dispatchTimeoutSeconds int

func validateFlags() {
	if traceLoaderConfig == "" {
		panic("must provide workload config")
	}
	switch gatewayFramework {
	case "knative":
		if autoscalerFramework != "" || autoscalerConfig != "" {
			klog.Info("[WARN] Ignoring autoscaler options for knative gateway")
			autoscalerFramework = ""
			autoscalerConfig = ""
		}
		if backendFramework == "" {
			klog.Info("Defaulting to grpc backend for knative gateway")
			backendFramework = "grpc"
		} else if backendFramework != "grpc" {
			klog.Fatalf("Only grpc backend is supported for knative gateway, got %v", backendFramework)
		}
	case "k8s":
		if autoscalerFramework != "one-time" && autoscalerConfig == "" {
			klog.Fatalf("Must provide config for %v autoscaler", autoscalerFramework)
		}
		if backendFramework == "" {
			klog.Info("Defaulting to fake backend for k8s gateway")
			backendFramework = "fake"
		} else if backendFramework != "grpc" && backendFramework != "fake" {
			klog.Fatalf("Only fake/grpc backend is supported for k8s gateway")
		}
	default:
		klog.Fatalf("Unknown gateway framework %v", gatewayFramework)
	}
}

func main() {
	// must move to baseDir to read config files
	if err := os.Chdir(baseDir); err != nil {
		klog.Fatalf("Cannot enter %v: %v", baseDir, err)
	}
	if dirInfo, err := os.Stat(filepath.Join(baseDir, "data")); err != nil || !dirInfo.IsDir() {
		klog.Fatalf("%v contains no data dir, consider running download.sh first", baseDir)
	}

	flag.StringVar(&gatewayFramework, "gateway", "k8s", "The gateway to use. Options: k8s, knative")
	flag.StringVar(&backendFramework, "backend", "fake", "The backend to use. Options: fake, grpc")
	flag.StringVar(&autoscalerFramework, "autoscaler", "one-time", "The autoscaler framework to use, only applicable to k8s gateway. Options: kpa, one-time")
	flag.StringVar(&autoscalerConfig, "autoscaler-config", "", "The path to the autoscaler config file, only applicable to k8s gateway")
	flag.StringVar(&traceLoaderConfig, "loader-config", "config/loader.json", "The path to the trace loader configuration file")
	flag.StringVar(&outputPath, "output", "trace.log", "The path to the output file")
	flag.IntVar(&requestTimeoutSeconds, "timeout", 15, "The timeout in seconds for a request to be cancelled in execution stage")
	// flag.IntVar(&dispatchTimeoutSeconds, "timeout", 15, "The timeout in seconds for a request to be cancelled in dispatch stage")
	flag.Parse()

	validateFlags()
	backend.Use(backendFramework)
	backend.WithTimeout(time.Duration(requestTimeoutSeconds) * time.Second)
	klog.InfoS("Running trace with options", "backend", backendFramework, "timeout", requestTimeoutSeconds, "gateway", gatewayFramework, "autoscaler", autoscalerFramework, "autoscaler-config", autoscalerConfig, "loader-config", traceLoaderConfig, "output", outputPath, "dir", baseDir)

	ctx := ctrl.SetupSignalHandler()
	ctx, cancel := context.WithCancel(ctx)

	ctrl.SetLogger(klog.Background())
	mgr := benchutil.NewManagerOrDie()

	klog.Infof("Creating %v gateway", gatewayFramework)
	gatewayImpl, err := func() (gateway.Gateway, error) {
		switch gatewayFramework {
		case "knative":
			return gateway.NewKnativeGateway()
		case "k8s":
			return gateway.NewK8sGateway(autoscalerFramework, autoscalerConfig)
		default:
			panic(fmt.Sprintf("unknown gateway framework %v", gatewayFramework))
		}
	}()
	if err != nil {
		klog.Fatalf("Unable to create %v gateway: %v", gatewayFramework, err)
	}
	if err := gatewayImpl.SetUpWithManager(ctx, mgr); err != nil {
		klog.Fatalf("Unable to setup %v gateway with manager: %v", gatewayFramework, err)
	}

	klog.Info("Creating client")
	client, err := replay.NewClient(ctx, gatewayImpl, traceLoaderConfig, outputPath)
	if err != nil {
		klog.Fatalf("Unable to create client: %v", err)
	}
	if err := client.SetupWithManager(ctx, mgr); err != nil {
		klog.Fatalf("Unable to setup client with manager: %v", err)
	}

	klog.Info("Starting manager")
	// mgr.Start blocks, must run it in another goroutine
	go func() {
		if err := mgr.Start(ctx); err != nil {
			klog.Fatalf("Unable to run manager: %v", err)
		}
	}()
	// wait for cache sync
	if !mgr.GetCache().WaitForCacheSync(ctx) {
		klog.Fatalf("Unable to sync manager cache")
	}

	<-time.After(5 * time.Second)
	klog.Infof("Starting %v gateway", gatewayFramework)
	go gatewayImpl.Start(ctx)

	<-time.After(5 * time.Second)
	klog.Info("Starting client")
	go client.Start(ctx)

	select {
	case <-ctx.Done():
		klog.Info("Received signal")
	case <-client.FinishSend():
		klog.Info("Client finished")
	}
	// cancel context to stop everything
	cancel()

	<-time.After(5 * time.Second)
	gatewayImpl.Close()
	<-client.FinishRecv()

	klog.Info("Finished trace")
}
