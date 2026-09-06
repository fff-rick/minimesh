package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"time"

	minimeshv1 "github.com/minimesh/minimesh/api/gen/minimesh/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
)

func main() {
	sidecar := flag.String("sidecar", "127.0.0.1:18080", "sidecar address")
	backend := flag.String("backend", "127.0.0.1:19090", "backend address; ignored when --service is set")
	service := flag.String("service", "", "service name resolved by the sidecar")
	message := flag.String("message", "hello", "payload")
	timeout := flag.Duration("timeout", 2*time.Second, "client-side total deadline")
	flag.Parse()

	conn, err := grpc.Dial(*sidecar, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		log.Fatal(err)
	}
	defer conn.Close()

	ctx, cancel := context.WithTimeout(context.Background(), *timeout)
	defer cancel()
	ctx = metadata.AppendToOutgoingContext(ctx, "x-request-id", "stage2-demo")

	target := *backend
	if *service != "" {
		target = "service://" + *service
	}

	client := minimeshv1.NewProxyServiceClient(conn)
	var header metadata.MD
	resp, err := client.Invoke(ctx, &minimeshv1.ProxyRequest{
		Target:     target,
		FullMethod: minimeshv1.EchoService_Echo_FullMethodName,
		Payload:    &minimeshv1.Payload{Data: []byte(*message)},
	}, grpc.Header(&header))
	if err != nil {
		log.Fatal(err)
	}

	fmt.Printf("response=%q backend-request-id=%q\n", string(resp.GetPayload().GetData()), header.Get("x-backend-request-id"))
}
