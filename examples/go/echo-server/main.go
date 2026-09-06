package main

import (
	"flag"
	"log/slog"
	"net"
	"os"

	minimeshv1 "github.com/minimesh/minimesh/api/gen/minimesh/v1"
	"github.com/minimesh/minimesh/internal/examplebackend"
	"google.golang.org/grpc"
)

func main() {
	listenAddr := flag.String("listen", ":19090", "gRPC listen address")
	instanceID := flag.String("id", "", "optional instance id included in demo responses")
	flag.Parse()

	listener, err := net.Listen("tcp", *listenAddr)
	if err != nil {
		slog.Error("backend listen failed", "addr", *listenAddr, "error", err)
		os.Exit(1)
	}

	server := grpc.NewServer()
	minimeshv1.RegisterEchoServiceServer(server, examplebackend.Server{ID: *instanceID})
	slog.Info("MiniMesh echo backend listening", "addr", *listenAddr, "instance_id", *instanceID)
	if err := server.Serve(listener); err != nil {
		slog.Error("backend stopped", "error", err)
		os.Exit(1)
	}
}
