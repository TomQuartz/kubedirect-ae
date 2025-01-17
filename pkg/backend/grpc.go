package backend

import (
	"context"
	"strings"
	"time"

	"golang.design/x/chann"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"k8s.io/klog/v2"

	// Kubedirect
	"github.com/tomquartz/kubedirect-bench/pkg/workload"
	"github.com/tomquartz/kubedirect-bench/pkg/workload/handler/proto"
)

const (
	grpcExecutorConcurrency = 80
	grpcExecutorTimeout     = 15 * time.Second
)

type grpcBackend struct {
	endpoint       string
	connectionPool *chann.Chann[*grpc.ClientConn]
}

var _ Executor = &grpcBackend{}

func newGrpcBackend(endpoint string) (*grpcBackend, error) {
	g := &grpcBackend{
		endpoint:       endpoint,
		connectionPool: chann.New[*grpc.ClientConn](),
	}
	if err := g.newClient(); err != nil {
		return nil, err
	}
	return g, nil
}

func (g *grpcBackend) Close() {
	for conn := range g.connectionPool.Out() {
		conn.Close()
	}
	g.connectionPool.Close()
}

func (g *grpcBackend) Execute(ctx context.Context, req *workload.Request) *workload.Response {
	logger := klog.FromContext(ctx).WithValues("backend", "grpc", "endpoint", g.endpoint, "req", req.ID)
	res := &workload.Response{Source: req}

	conn, err := g.getOrCreateClient()
	if err != nil {
		logger.Error(err, "Error creating gRPC connection")
		res.Status = workload.FAIL_CONNECT
		return res
	}
	defer func() { g.connectionPool.In() <- conn }()
	grpcExecutor := proto.NewExecutorClient(conn)

	execContext, cancelExecution := context.WithTimeout(ctx, grpcExecutorTimeout)
	defer cancelExecution()

	req.GatewaySendTS = time.Now()
	faasResponse, err := grpcExecutor.Execute(execContext, &proto.FaasRequest{
		Message:         "request",
		RuntimeMilliSec: uint32(req.DurationMilliSec),
	})
	if err != nil {
		logger.V(1).Info("[WARN] gRPC request failed", "error", err)
		if strings.Contains(err.Error(), "overflow") {
			res.Status = workload.FAIL_SEND
		} else {
			res.Status = workload.FAIL_RECV
		}
		return res
	}

	res.GatewayRecvTS = time.Now()
	res.RuntimeMicroSec = int(faasResponse.DurationMicroSec)

	return res
}

func (g *grpcBackend) newClient(opts ...grpc.DialOption) error {
	opts = append(opts, grpc.WithTransportCredentials(insecure.NewCredentials()))
	conn, err := grpc.NewClient(g.endpoint, opts...)
	if err != nil {
		return err
	}
	for i := 0; i < grpcExecutorConcurrency; i++ {
		g.connectionPool.In() <- conn
	}
	return nil
}

func (g *grpcBackend) getOrCreateClient() (*grpc.ClientConn, error) {
	select {
	case conn := <-g.connectionPool.Out():
		return conn, nil
	default:
		if err := g.newClient(); err != nil {
			return nil, err
		}
		conn := <-g.connectionPool.Out()
		return conn, nil
	}
}
