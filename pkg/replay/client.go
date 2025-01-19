package replay

import (
	"context"
	"fmt"
	"os"
	"sync"
	"time"

	"golang.design/x/chann"
	appsv1 "k8s.io/api/apps/v1"
	"k8s.io/klog/v2"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/manager"

	// Kubedirect
	"github.com/tomquartz/kubedirect-bench/pkg/gateway"
	benchutil "github.com/tomquartz/kubedirect-bench/pkg/util"
	"github.com/tomquartz/kubedirect-bench/pkg/workload"
)

var (
	sampleOutputFactor = 1
)

func SampleOutput(factor int) {
	sampleOutputFactor = factor
}

type Client struct {
	gateway    gateway.Gateway
	traces     []*workload.TraceSpec
	workers    map[string]*worker
	outputFile *os.File
	client     client.Client
	finishSend chan struct{}
	finishRecv chan struct{}
}

func NewClient(ctx context.Context, gateway gateway.Gateway, loaderConfig string, outputPath string) (*Client, error) {
	logger := klog.FromContext(ctx)

	logger.Info("Loading trace specs...", "config", loaderConfig)
	traces := workload.LoadTraceFromConfig(loaderConfig)
	logger.Info("Finished loading", "total", len(traces))

	outputFile, err := os.Create(outputPath)
	if err != nil {
		return nil, fmt.Errorf("failed to create output file %v: %v", outputPath, err)
	}

	return &Client{
		gateway:    gateway,
		traces:     traces,
		workers:    make(map[string]*worker),
		outputFile: outputFile,
		finishSend: make(chan struct{}),
		finishRecv: make(chan struct{}),
	}, nil
}

func (c *Client) SetupWithManager(ctx context.Context, mgr manager.Manager) error {
	logger := klog.FromContext(ctx)

	c.client = mgr.GetClient()

	// setup a temporary client to list services because manager hasn't started yet
	uncachedClient := benchutil.NewUncachedClientOrDie(mgr)

	// NOTE: deployments are the common basis for both knative and k8s workloads
	targets := &appsv1.DeploymentList{}
	if err := uncachedClient.List(ctx, targets, workload.CtrlListOptionsForTrace...); err != nil {
		return fmt.Errorf("error listing deployments in client: %v", err)
	}
	if len(targets.Items) > len(c.traces) {
		return fmt.Errorf("mismatched deployments and traces: expected %d, got %d", len(c.traces), len(targets.Items))
	} else if len(targets.Items) < len(c.traces) {
		logger.Info(fmt.Sprintf("Using the first %d traces out of %d", len(targets.Items), len(c.traces)))
	}

	for i := range targets.Items {
		target := &targets.Items[i]
		key := workload.KeyFromObject(target)
		wrk := newWorker(key, c.traces[i], c.gateway.RequestChan(key))
		c.workers[key] = wrk
		logger.V(1).Info(fmt.Sprintf("Registered worker %v", key), "senders", wrk.nSenders, "trace", wrk.trace.String())
	}
	logger.Info("All workers registered", "total", len(c.workers))
	return nil
}

// does not rely on ctx to stop
// it stops itself when the gateway closes the response channel
func (c *Client) recv(_ context.Context) {
	writerChan := chann.New[*workload.Response]()
	defer writerChan.Close()
	go c.write(writerChan.Out())
	// fan-in responses from all workers
	// gateway must close the response chan when shutting down
	for res := range c.gateway.ResponseChan("") {
		// logger.V(1).Info("Received response", "id", res.Source.ID, "target", res.Source.Target, "content", res.String())
		res.ClientRecvTS = time.Now()
		writerChan.In() <- res
	}
}

func (c *Client) write(responses <-chan *workload.Response) {
	var nTotal, nFailed int64
	for res := range responses {
		if res == nil {
			break
		}
		nTotal++
		if res.Status != workload.SUCCESS {
			nFailed++
		}
		if nTotal%int64(sampleOutputFactor) == 0 {
			if _, err := c.outputFile.WriteString(res.Summary()); err != nil {
				panic(fmt.Sprintf("Failed to write response: %v", err))
			}
		}
	}
	if _, err := c.outputFile.WriteString(fmt.Sprintf("Summary: total %v success %v fail %v\n", nTotal, nTotal-nFailed, nFailed)); err != nil {
		panic(fmt.Sprintf("Failed to write request summary: %v", err))
	}
	c.outputFile.Sync()
	c.outputFile.Close()
	close(c.finishRecv)
}

func (c *Client) FinishSend() <-chan struct{} {
	return c.finishSend
}

func (c *Client) FinishRecv() <-chan struct{} {
	return c.finishRecv
}

// NOTE: ctx is not used to stop the client
func (c *Client) Start(ctx context.Context) error {
	logger := klog.FromContext(ctx)

	// start workers for traces
	start := time.Now()
	var wg sync.WaitGroup
	wg.Add(len(c.workers))
	for key := range c.workers {
		worker := c.workers[key]
		go func() {
			defer wg.Done()
			worker.replay(ctx, start)
		}()
	}

	// recv stops when the gateway closes the response channel
	go c.recv(ctx)

	// wait for senders to finish, signal when done
	wg.Wait()
	logger.Info("Finished sending")
	close(c.finishSend)

	return nil
}
