package gateway

import (
	"context"
	"fmt"
	"time"

	"github.com/tomquartz/kubedirect-bench/pkg/autoscaler"
	"golang.design/x/chann"
	"k8s.io/klog/v2"
	"sigs.k8s.io/controller-runtime/pkg/manager"

	//lint:ignore ST1001 Allow dot imports
	. "github.com/tomquartz/kubedirect-bench/pkg/workload"
)

var (
	tracingOutputPeriod = 5000
)

func StartTracing(period int) {
	tracingOutputPeriod = period
}

type Gateway interface {
	RequestChan(target string) chan<- *Request
	ResponseChan(target string) <-chan *Response
	Autoscaler() autoscaler.Autoscaler
	SetUpWithManager(ctx context.Context, mgr manager.Manager) error
	Start(ctx context.Context) error
	Close()
}

type gatewayImpl struct {
	internalInputBuffers  map[string]RequestBuffer
	internalOutputBuffers map[string]ResponseBuffer
	externalInputs        map[string]RequestBuffer
	externalOutput        ResponseBuffer // fan-in for all keys
	onReqIn               func(req *Request)
	onReqOut              func(res *Response)
}

func newGatewayImpl(onReqIn func(req *Request), onReqOut func(res *Response)) *gatewayImpl {
	return &gatewayImpl{
		externalInputs:        make(map[string]RequestBuffer),
		externalOutput:        chann.New[*Response](),
		internalInputBuffers:  make(map[string]RequestBuffer),
		internalOutputBuffers: make(map[string]ResponseBuffer),
		onReqIn:               onReqIn,
		onReqOut:              onReqOut,
	}
}

func (g *gatewayImpl) RequestChan(target string) chan<- *Request {
	return g.externalInputs[target].In()
}

func (g *gatewayImpl) ResponseChan(target string) <-chan *Response {
	return g.externalOutput.Out()
}

func (g *gatewayImpl) Close() {
	g.externalOutput.Close()
	for _, reqBuffer := range g.externalInputs {
		reqBuffer.Close()
	}
}

func (g *gatewayImpl) internalBuffers(key string) (reqChan <-chan *Request, resChan chan<- *Response) {
	return g.internalInputBuffers[key].Out(), g.internalOutputBuffers[key].In()
}

func (g *gatewayImpl) register(key string) {
	g.externalInputs[key] = chann.New[*Request]()
	g.internalInputBuffers[key] = chann.New[*Request]()
	g.internalOutputBuffers[key] = chann.New[*Response]()
}

func (g *gatewayImpl) relay(ctx context.Context, key string) {
	logger := klog.FromContext(ctx).WithValues("src", "gateway", "op", "relay", "key", key)
	logger.V(1).Info("Starting request/response relay")
	externalInput := g.externalInputs[key].Out()
	internalInput := g.internalInputBuffers[key].In()
	externalOutput := g.externalOutput.In()
	internalOutput := g.internalOutputBuffers[key].Out()
	nSend := 0
	nRecv := 0
	outstanding := 0
	for {
		select {
		case req := <-externalInput:
			if req.Target != key {
				logger.Error(fmt.Errorf("invalid target"), "Fail to relay req", "id", req.ID, "target", req.Target)
				res := &Response{
					Source: req,
					Status: INVALID_TARGET,
				}
				externalOutput <- res
				continue
			}
			outstanding++
			nSend++
			if nSend%tracingOutputPeriod == 0 {
				logger.V(1).Info("Forward request", "id", req.ID, "outstanding", outstanding, "send/recv", fmt.Sprintf("%v/%v", nSend, nRecv))
			}
			g.onReqIn(req)
			req.GatewayRecvTS = time.Now()
			internalInput <- req
		case res := <-internalOutput:
			outstanding--
			nRecv++
			if nRecv%tracingOutputPeriod == 0 {
				logger.V(1).Info("Receive response", "id", res.Source.ID, "outstanding", outstanding, "send/recv", fmt.Sprintf("%v/%v", nSend, nRecv))
			}
			g.onReqOut(res)
			externalOutput <- res
		case <-ctx.Done():
			return
		}
	}
}
