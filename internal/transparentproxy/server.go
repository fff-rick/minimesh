// Package transparentproxy implements a small TCP proxy for connections that
// a Pod-local iptables REDIRECT rule sends to the MiniMesh sidecar.
package transparentproxy

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"sync"
	"time"
)

type DestinationResolver func(*net.TCPConn) (string, error)

type Config struct {
	Resolve      DestinationResolver
	DialTimeout  time.Duration
	OnConnection func(target string)
	OnError      func(error)
}

// Serve accepts redirected TCP connections until ctx is cancelled. Protocol
// bytes are not decoded; gRPC/HTTP2 is forwarded unchanged.
func Serve(ctx context.Context, listener net.Listener, cfg Config) error {
	resolver := cfg.Resolve
	if resolver == nil {
		resolver = OriginalDestination
	}
	dialTimeout := cfg.DialTimeout
	if dialTimeout <= 0 {
		dialTimeout = 3 * time.Second
	}
	go func() {
		<-ctx.Done()
		_ = listener.Close()
	}()

	for {
		connection, err := listener.Accept()
		if err != nil {
			if ctx.Err() != nil || errors.Is(err, net.ErrClosed) {
				return nil
			}
			return fmt.Errorf("accept transparent connection: %w", err)
		}
		tcpConnection, ok := connection.(*net.TCPConn)
		if !ok {
			_ = connection.Close()
			continue
		}
		go handle(ctx, tcpConnection, resolver, dialTimeout, cfg)
	}
}

func handle(ctx context.Context, downstream *net.TCPConn, resolver DestinationResolver, dialTimeout time.Duration, cfg Config) {
	defer downstream.Close()
	target, err := resolver(downstream)
	if err != nil {
		report(cfg, fmt.Errorf("resolve original destination: %w", err))
		return
	}
	dialer := net.Dialer{Timeout: dialTimeout}
	connection, err := dialer.DialContext(ctx, "tcp", target)
	if err != nil {
		report(cfg, fmt.Errorf("dial original destination %s: %w", target, err))
		return
	}
	upstream, ok := connection.(*net.TCPConn)
	if !ok {
		_ = connection.Close()
		report(cfg, fmt.Errorf("dial original destination %s returned a non-TCP connection", target))
		return
	}
	defer upstream.Close()
	if cfg.OnConnection != nil {
		cfg.OnConnection(target)
	}

	var wg sync.WaitGroup
	wg.Add(2)
	go copyHalf(&wg, upstream, downstream, cfg)
	go copyHalf(&wg, downstream, upstream, cfg)
	wg.Wait()
}

func copyHalf(wg *sync.WaitGroup, destination, source *net.TCPConn, cfg Config) {
	defer wg.Done()
	if _, err := io.Copy(destination, source); err != nil && !errors.Is(err, net.ErrClosed) {
		report(cfg, err)
	}
	_ = destination.CloseWrite()
}

func report(cfg Config, err error) {
	if cfg.OnError != nil {
		cfg.OnError(err)
	}
}
