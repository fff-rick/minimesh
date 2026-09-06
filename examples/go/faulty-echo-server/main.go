package main

import (
	"context"
	"flag"
	"log/slog"
	"net"
	"os"
	"sync/atomic"
	"time"

	minimeshv1 "github.com/minimesh/minimesh/api/gen/minimesh/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type server struct {
	minimeshv1.UnimplementedEchoServiceServer
	id             string
	mode           string
	delay          time.Duration
	failurePercent int
	requests       atomic.Uint64
}

func (s *server) Echo(ctx context.Context, in *minimeshv1.Payload) (*minimeshv1.Payload, error) {
	switch s.mode {
	case "internal":
		return nil, status.Error(codes.Internal, "injected backend 500/internal error")
	case "unavailable":
		return nil, status.Error(codes.Unavailable, "injected temporary unavailable")
	case "percentage":
		if percentageFailure(s.requests.Add(1)-1, s.failurePercent) {
			return nil, status.Errorf(codes.Internal, "injected deterministic failure %d%%", s.failurePercent)
		}
		return &minimeshv1.Payload{Data: append([]byte("echo@"+s.id+":"), in.GetData()...)}, nil
	case "timeout":
		timer := time.NewTimer(s.delay)
		defer timer.Stop()
		select {
		case <-ctx.Done():
			return nil, status.FromContextError(ctx.Err()).Err()
		case <-timer.C:
		}
		return &minimeshv1.Payload{Data: append([]byte("echo@"+s.id+":"), in.GetData()...)}, nil
	default:
		return &minimeshv1.Payload{Data: append([]byte("echo@"+s.id+":"), in.GetData()...)}, nil
	}
}

// percentageFailure distributes failures across each 100-request cycle instead
// of placing them all at its beginning. For example, 80% produces 8 failures in
// every 10 consecutive requests, which makes small fault-injection samples
// representative as well as deterministic.
func percentageFailure(request uint64, percentage int) bool {
	return int((request*uint64(percentage))%100) < percentage
}

func main() {
	listenAddr := flag.String("listen", ":19091", "gRPC listen address")
	instanceID := flag.String("id", "faulty", "instance id included in healthy responses")
	mode := flag.String("mode", "internal", "fault mode: internal, unavailable, percentage, timeout, healthy")
	delay := flag.Duration("delay", 3*time.Second, "timeout mode response delay")
	failurePercent := flag.Int("failure-percent", 80, "percentage mode deterministic failure percentage [0,100]")
	flag.Parse()
	if *failurePercent < 0 || *failurePercent > 100 {
		slog.Error("failure-percent must be between 0 and 100", "value", *failurePercent)
		os.Exit(2)
	}

	listener, err := net.Listen("tcp", *listenAddr)
	if err != nil {
		slog.Error("faulty backend listen failed", "addr", *listenAddr, "error", err)
		os.Exit(1)
	}
	grpcServer := grpc.NewServer()
	minimeshv1.RegisterEchoServiceServer(grpcServer, &server{id: *instanceID, mode: *mode, delay: *delay, failurePercent: *failurePercent})
	slog.Info("MiniMesh faulty backend listening", "addr", *listenAddr, "instance_id", *instanceID, "mode", *mode, "delay", *delay, "failure_percent", *failurePercent)
	if err := grpcServer.Serve(listener); err != nil {
		slog.Error("faulty backend stopped", "error", err)
		os.Exit(1)
	}
}
