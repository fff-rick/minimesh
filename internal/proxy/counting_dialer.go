package proxy

import (
	"context"
	"sync/atomic"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

type CountingDialer struct{ count atomic.Int64 }

func (d *CountingDialer) Dial(ctx context.Context, target string) (*grpc.ClientConn, error) {
	conn, err := grpc.DialContext(ctx, target, grpc.WithTransportCredentials(insecure.NewCredentials()), grpc.WithBlock())
	if err == nil {
		d.count.Add(1)
	}
	return conn, err
}
func (d *CountingDialer) Count() int64 { return d.count.Load() }
