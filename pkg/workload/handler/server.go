/*
 * MIT License
 *
 * Copyright (c) 2023 EASL and the vHive community
 *
 * Permission is hereby granted, free of charge, to any person obtaining a copy
 * of this software and associated documentation files (the "Software"), to deal
 * in the Software without restriction, including without limitation the rights
 * to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
 * copies of the Software, and to permit persons to whom the Software is
 * furnished to do so, subject to the following conditions:
 *
 * The above copyright notice and this permission notice shall be included in all
 * copies or substantial portions of the Software.
 *
 * THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
 * IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
 * FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
 * AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
 * LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
 * OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
 * SOFTWARE.
 */

package handler

import (
	"context"
	"net"
	"os/signal"
	"syscall"
	"time"

	log "github.com/sirupsen/logrus"
	"google.golang.org/grpc"

	"github.com/tomquartz/kubedirect-bench/pkg/workload/handler/proto"
)

const WorkloadServicePort = ":80"

type funcServer struct {
	mode FunctionType
	proto.UnimplementedExecutorServer
}

func newFuncServer(mode FunctionType) *funcServer {
	return &funcServer{
		mode: mode,
	}
}

func (s *funcServer) Execute(_ context.Context, req *proto.FaasRequest) (*proto.FaasReply, error) {
	start := time.Now()

	var msg string
	if s.mode == TraceFunction {
		msg = TraceFunctionExecution(start, req.RuntimeMilliSec)
	} else {
		msg = EmptyFunctionExecution(start, req.RuntimeMilliSec)
	}

	return &proto.FaasReply{
		Message:          msg,
		DurationMicroSec: uint32(time.Since(start).Microseconds()),
	}, nil
}

func StartGRPCServer() {
	readEnvironmentalVariables()

	listener, err := net.Listen("tcp", WorkloadServicePort)
	if err != nil {
		log.Fatalf("failed to listen: %v", err)
	}

	grpcServer := grpc.NewServer()

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	go func() {
		<-ctx.Done()
		log.Info("Received SIGTERM, shutting down gracefully...")
		grpcServer.GracefulStop()
	}()

	proto.RegisterExecutorServer(grpcServer, newFuncServer(funcType))
	if err := grpcServer.Serve(listener); err != nil {
		log.Fatalf("Failed to serve: %v", err)
	}
}
