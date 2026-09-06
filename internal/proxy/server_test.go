package proxy_test

import (
	"context"
	"fmt"
	"net"
	"runtime"
	"sync"
	"testing"
	"time"

	minimeshv1 "github.com/minimesh/minimesh/api/gen/minimesh/v1"
	"github.com/minimesh/minimesh/internal/examplebackend"
	"github.com/minimesh/minimesh/internal/proxy"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/test/bufconn"
)

const bufSize = 1024 * 1024

type meshHarness struct {
	backendListener *bufconn.Listener
	backendServer   *grpc.Server
	sidecarListener *bufconn.Listener
	sidecarServer   *grpc.Server
	clientConn      *grpc.ClientConn
	client          minimeshv1.ProxyServiceClient
}

func newHarness(t *testing.T) *meshHarness {
	t.Helper()

	backendListener := bufconn.Listen(bufSize)
	backendServer := grpc.NewServer()
	minimeshv1.RegisterEchoServiceServer(backendServer, examplebackend.Server{})
	go func() { _ = backendServer.Serve(backendListener) }()

	backendDial := func(ctx context.Context, target string) (*grpc.ClientConn, error) {
		if target == "missing-backend" {
			return nil, status.Error(codes.Unavailable, "injected missing backend")
		}
		return grpc.DialContext(
			ctx,
			"passthrough:///backend",
			grpc.WithContextDialer(func(context.Context, string) (net.Conn, error) { return backendListener.Dial() }),
			grpc.WithTransportCredentials(insecure.NewCredentials()),
			grpc.WithBlock(),
		)
	}

	sidecarListener := bufconn.Listen(bufSize)
	sidecarServer := grpc.NewServer()
	minimeshv1.RegisterProxyServiceServer(sidecarServer, proxy.NewServerWithDialer(backendDial))
	go func() { _ = sidecarServer.Serve(sidecarListener) }()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	clientConn, err := grpc.DialContext(
		ctx,
		"passthrough:///sidecar",
		grpc.WithContextDialer(func(context.Context, string) (net.Conn, error) { return sidecarListener.Dial() }),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithBlock(),
	)
	if err != nil {
		t.Fatalf("dial sidecar: %v", err)
	}

	h := &meshHarness{
		backendListener: backendListener,
		backendServer:   backendServer,
		sidecarListener: sidecarListener,
		sidecarServer:   sidecarServer,
		clientConn:      clientConn,
		client:          minimeshv1.NewProxyServiceClient(clientConn),
	}
	t.Cleanup(func() {
		clientConn.Close()
		sidecarServer.Stop()
		backendServer.Stop()
		sidecarListener.Close()
		backendListener.Close()
	})
	return h
}

func invokeRequest(target, body string) *minimeshv1.ProxyRequest {
	return &minimeshv1.ProxyRequest{
		Target:     target,
		FullMethod: minimeshv1.EchoService_Echo_FullMethodName,
		Payload:    &minimeshv1.Payload{Data: []byte(body)},
	}
}

func TestProxyNormalRequestAndMetadata(t *testing.T) {
	h := newHarness(t)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	ctx = metadata.AppendToOutgoingContext(ctx, "x-request-id", "req-stage1")

	var header metadata.MD
	resp, err := h.client.Invoke(ctx, invokeRequest("backend", "hello"), grpc.Header(&header))
	if err != nil {
		t.Fatalf("Invoke() error = %v", err)
	}
	if got, want := string(resp.GetPayload().GetData()), "echo:hello"; got != want {
		t.Fatalf("payload = %q, want %q", got, want)
	}
	if got := header.Get("x-backend-request-id"); len(got) != 1 || got[0] != "req-stage1" {
		t.Fatalf("response metadata = %v, want propagated request id", got)
	}
}

func TestProxyBackendUnavailable(t *testing.T) {
	h := newHarness(t)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	_, err := h.client.Invoke(ctx, invokeRequest("missing-backend", "hello"))
	if status.Code(err) != codes.Unavailable {
		t.Fatalf("status = %v, want %v (err=%v)", status.Code(err), codes.Unavailable, err)
	}
}

func TestProxyDeadlinePropagatesToBackend(t *testing.T) {
	h := newHarness(t)
	ctx, cancel := context.WithTimeout(context.Background(), 40*time.Millisecond)
	defer cancel()

	_, err := h.client.Invoke(ctx, invokeRequest("backend", "sleep:250ms"))
	if status.Code(err) != codes.DeadlineExceeded {
		t.Fatalf("status = %v, want %v (err=%v)", status.Code(err), codes.DeadlineExceeded, err)
	}
}

func TestProxyPreservesBackendStatus(t *testing.T) {
	h := newHarness(t)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	_, err := h.client.Invoke(ctx, invokeRequest("backend", "error"))
	if status.Code(err) != codes.Internal {
		t.Fatalf("status = %v, want %v (err=%v)", status.Code(err), codes.Internal, err)
	}
}

func TestProxyRejectsInvalidRequest(t *testing.T) {
	h := newHarness(t)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	_, err := h.client.Invoke(ctx, &minimeshv1.ProxyRequest{Target: "backend", FullMethod: "Echo", Payload: &minimeshv1.Payload{}})
	if status.Code(err) != codes.InvalidArgument {
		t.Fatalf("status = %v, want %v (err=%v)", status.Code(err), codes.InvalidArgument, err)
	}
}

func TestProxy1000ConcurrentRequests(t *testing.T) {
	h := newHarness(t)
	baseline := runtime.NumGoroutine()

	const requests = 1000
	var wg sync.WaitGroup
	errCh := make(chan error, requests)
	wg.Add(requests)
	for i := 0; i < requests; i++ {
		i := i
		go func() {
			defer wg.Done()
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			body := fmt.Sprintf("req-%d", i)
			resp, err := h.client.Invoke(ctx, invokeRequest("backend", body))
			if err != nil {
				errCh <- err
				return
			}
			if got, want := string(resp.GetPayload().GetData()), "echo:"+body; got != want {
				errCh <- fmt.Errorf("payload=%q want=%q", got, want)
			}
		}()
	}
	wg.Wait()
	close(errCh)
	for err := range errCh {
		t.Errorf("concurrent request failed: %v", err)
	}

	// Stage 1 intentionally dials per request, but every connection must be
	// closed. Give grpc transport goroutines a short grace period to exit.
	runtime.GC()
	time.Sleep(300 * time.Millisecond)
	if delta := runtime.NumGoroutine() - baseline; delta > 64 {
		t.Fatalf("possible goroutine leak: baseline=%d current=%d delta=%d", baseline, runtime.NumGoroutine(), delta)
	}
}
