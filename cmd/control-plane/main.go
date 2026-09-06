package main

import (
	"context"
	"flag"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	minimeshv1 "github.com/minimesh/minimesh/api/gen/minimesh/v1"
	"github.com/minimesh/minimesh/internal/controlstream"
	"github.com/minimesh/minimesh/internal/discovery"
	"github.com/minimesh/minimesh/internal/registry"
	"google.golang.org/grpc"
)

func main() {
	listen := flag.String("listen", ":17070", "registry HTTP listen address")
	controlListen := flag.String("control-listen", ":17071", "control-stream gRPC listen address")
	etcd := flag.String("etcd", "http://127.0.0.1:2379", "etcd v3 gateway endpoint")
	ttl := flag.Int64("lease-ttl", 15, "default endpoint lease TTL in seconds")
	flag.Parse()

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	api := registry.NewAPI(registry.NewEtcdStore(*etcd), *ttl)
	srv := &http.Server{Addr: *listen, Handler: api.Handler(), ReadHeaderTimeout: 5 * time.Second}
	grpcListener, err := net.Listen("tcp", *controlListen)
	if err != nil {
		slog.Error("control stream listen failed", "addr", *controlListen, "error", err)
		os.Exit(1)
	}
	grpcServer := grpc.NewServer()
	minimeshv1.RegisterControlServiceServer(grpcServer, controlstream.NewServer(discovery.NewEtcdSource(*etcd)))
	slog.Info("MiniMesh Stage 8 control-plane listening", "registry_addr", *listen, "control_addr", *controlListen, "etcd", *etcd)
	go func() {
		if serveErr := grpcServer.Serve(grpcListener); serveErr != nil && ctx.Err() == nil {
			slog.Error("control stream stopped", "error", serveErr)
			cancel()
		}
	}()
	go func() {
		<-ctx.Done()
		shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer shutdownCancel()
		_ = srv.Shutdown(shutdownCtx)

		// ControlService.Stream is long-lived, so GracefulStop can wait forever
		// for a connected sidecar. Give active streams a short drain window, then
		// force them to reconnect to the replacement control plane.
		grpcStopped := make(chan struct{})
		go func() {
			grpcServer.GracefulStop()
			close(grpcStopped)
		}()
		select {
		case <-grpcStopped:
		case <-time.After(time.Second):
			grpcServer.Stop()
		}
	}()
	if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed && ctx.Err() == nil {
		slog.Error("control-plane stopped", "error", err)
		os.Exit(1)
	}
}
