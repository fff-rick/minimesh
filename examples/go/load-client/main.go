package main

import (
	"context"
	"flag"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	minimeshv1 "github.com/minimesh/minimesh/api/gen/minimesh/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"
)

func main() {
	sidecar := flag.String("sidecar", "127.0.0.1:18080", "sidecar address")
	backend := flag.String("backend", "", "explicit backend address; bypasses service discovery when set")
	service := flag.String("service", "echo", "service name")
	requests := flag.Int("requests", 5000, "total requests")
	concurrency := flag.Int("concurrency", 200, "concurrent workers")
	timeout := flag.Duration("timeout", 3*time.Second, "per-request timeout")
	flag.Parse()

	conn, err := grpc.Dial(*sidecar, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		panic(err)
	}
	defer conn.Close()
	client := minimeshv1.NewProxyServiceClient(conn)
	target := "service://" + *service
	if *backend != "" {
		target = *backend
	}

	jobs := make(chan int)
	var ok, limited, other atomic.Int64
	start := time.Now()
	var wg sync.WaitGroup
	for i := 0; i < *concurrency; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for n := range jobs {
				ctx, cancel := context.WithTimeout(context.Background(), *timeout)
				_, err := client.Invoke(ctx, &minimeshv1.ProxyRequest{
					Target:     target,
					FullMethod: minimeshv1.EchoService_Echo_FullMethodName,
					Payload:    &minimeshv1.Payload{Data: []byte(fmt.Sprintf("load-%d", n))},
				})
				cancel()
				if err == nil {
					ok.Add(1)
					continue
				}
				if status.Code(err) == codes.ResourceExhausted {
					limited.Add(1)
				} else {
					other.Add(1)
				}
			}
		}()
	}
	for i := 0; i < *requests; i++ {
		jobs <- i
	}
	close(jobs)
	wg.Wait()
	elapsed := time.Since(start)
	fmt.Printf("requests=%d success=%d rate_limited=%d other_errors=%d elapsed=%s input_qps=%.2f\n",
		*requests, ok.Load(), limited.Load(), other.Load(), elapsed, float64(*requests)/elapsed.Seconds())
	if other.Load() != 0 {
		panic("unexpected non-rate-limit errors")
	}
}
