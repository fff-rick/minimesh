package examplebackend

import (
	"context"
	"strings"
	"time"

	minimeshv1 "github.com/minimesh/minimesh/api/gen/minimesh/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

type Server struct {
	minimeshv1.UnimplementedEchoServiceServer
	ID string
}

func (s Server) Echo(ctx context.Context, in *minimeshv1.Payload) (*minimeshv1.Payload, error) {
	md, _ := metadata.FromIncomingContext(ctx)
	if requestIDs := md.Get("x-request-id"); len(requestIDs) > 0 {
		_ = grpc.SetHeader(ctx, metadata.Pairs("x-backend-request-id", requestIDs[0]))
	}

	text := string(in.GetData())
	if strings.HasPrefix(text, "sleep:") {
		d, err := time.ParseDuration(strings.TrimPrefix(text, "sleep:"))
		if err != nil {
			return nil, status.Error(codes.InvalidArgument, "invalid sleep duration")
		}
		timer := time.NewTimer(d)
		defer timer.Stop()
		select {
		case <-ctx.Done():
			return nil, status.FromContextError(ctx.Err()).Err()
		case <-timer.C:
		}
	}
	if text == "error" {
		return nil, status.Error(codes.Internal, "injected backend error")
	}

	prefix := "echo:"
	if id := strings.TrimSpace(s.ID); id != "" {
		prefix = "echo@" + id + ":"
	}
	return &minimeshv1.Payload{Data: append([]byte(prefix), in.GetData()...)}, nil
}
