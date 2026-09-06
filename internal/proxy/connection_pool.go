package proxy

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/minimesh/minimesh/internal/connectionpool"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/keepalive"
)

type ConnectionStats = connectionpool.Stats

type GRPCConnectionPool struct{ core *connectionpool.Pool }

type GRPCPoolConfig struct {
	MaxConnectionsPerTarget int
	MaxIdleTime             time.Duration
	ReapInterval            time.Duration
	KeepaliveTime           time.Duration
	KeepaliveTimeout        time.Duration
	CredentialsForService   func(service string) (credentials.TransportCredentials, error)
}

func NewGRPCConnectionPool(cfg GRPCPoolConfig) (*GRPCConnectionPool, error) {
	core, err := connectionpool.New(connectionpool.Config{
		MaxConnectionsPerTarget: cfg.MaxConnectionsPerTarget,
		MaxIdleTime:             cfg.MaxIdleTime,
		ReapInterval:            cfg.ReapInterval,
	}, func(ctx context.Context, key string) (connectionpool.Conn, error) {
		service, target := splitPoolKey(key)
		transportCredentials := credentials.TransportCredentials(insecure.NewCredentials())
		if cfg.CredentialsForService != nil {
			if service == "" {
				return nil, fmt.Errorf("proxy: service identity is required for secure upstream %q", target)
			}
			var credentialErr error
			transportCredentials, credentialErr = cfg.CredentialsForService(service)
			if credentialErr != nil {
				return nil, credentialErr
			}
		}
		opts := []grpc.DialOption{
			grpc.WithTransportCredentials(transportCredentials),
			grpc.WithBlock(),
		}
		if cfg.KeepaliveTime > 0 {
			timeout := cfg.KeepaliveTimeout
			if timeout <= 0 {
				timeout = 5 * time.Second
			}
			opts = append(opts, grpc.WithKeepaliveParams(keepalive.ClientParameters{
				Time:                cfg.KeepaliveTime,
				Timeout:             timeout,
				PermitWithoutStream: true,
			}))
		}
		return grpc.DialContext(ctx, target, opts...)
	})
	if err != nil {
		return nil, err
	}
	return &GRPCConnectionPool{core: core}, nil
}

func (p *GRPCConnectionPool) Acquire(ctx context.Context, target string) (*grpc.ClientConn, func(), error) {
	return p.acquire(ctx, target)
}

func (p *GRPCConnectionPool) AcquireForService(ctx context.Context, target, service string) (*grpc.ClientConn, func(), error) {
	return p.acquire(ctx, poolKey(service, target))
}

func (p *GRPCConnectionPool) acquire(ctx context.Context, key string) (*grpc.ClientConn, func(), error) {
	conn, release, err := p.core.Acquire(ctx, key)
	if err != nil {
		return nil, nil, err
	}
	grpcConn, ok := conn.(*grpc.ClientConn)
	if !ok {
		release()
		return nil, nil, connectionpool.ErrClosed // impossible for production dialer
	}
	return grpcConn, release, nil
}

const poolKeySeparator = "\x00"

func poolKey(service, target string) string { return service + poolKeySeparator + target }

func splitPoolKey(key string) (service, target string) {
	service, target, found := strings.Cut(key, poolKeySeparator)
	if !found {
		return "", key
	}
	return service, target
}

func (p *GRPCConnectionPool) Stats() ConnectionStats { return p.core.Stats() }
func (p *GRPCConnectionPool) Close() error           { return p.core.Close() }
