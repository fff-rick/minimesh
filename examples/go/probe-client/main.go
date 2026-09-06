package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"sort"
	"strings"
	"sync"
	"time"

	minimeshv1 "github.com/minimesh/minimesh/api/gen/minimesh/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"
)

type summary struct {
	Requests  int            `json:"requests"`
	Success   int            `json:"success"`
	Errors    int            `json:"errors"`
	Codes     map[string]int `json:"codes"`
	Responses map[string]int `json:"responses"`
	ElapsedMS int64          `json:"elapsed_ms"`
}

func validateSummary(result summary, minSuccess, minErrors int, expectedCode string) error {
	if result.Success < minSuccess {
		return fmt.Errorf("success=%d, want at least %d", result.Success, minSuccess)
	}
	if result.Errors < minErrors {
		return fmt.Errorf("errors=%d, want at least %d", result.Errors, minErrors)
	}
	if expectedCode != "" && result.Codes[expectedCode] == 0 {
		return fmt.Errorf("expected at least one %s error, got %v", expectedCode, result.Codes)
	}
	return nil
}

func main() {
	sidecar := flag.String("sidecar", "127.0.0.1:18080", "sidecar gRPC address")
	service := flag.String("service", "echo", "service name resolved by the sidecar")
	requests := flag.Int("requests", 1, "number of requests")
	concurrency := flag.Int("concurrency", 1, "number of concurrent workers")
	timeout := flag.Duration("timeout", 2*time.Second, "per-request deadline")
	minSuccess := flag.Int("min-success", 0, "minimum successful calls required")
	minErrors := flag.Int("min-errors", 0, "minimum failed calls required")
	expectedCode := flag.String("expect-code", "", "require at least one gRPC status code")
	flag.Parse()
	if *requests <= 0 || *concurrency <= 0 {
		fmt.Fprintln(os.Stderr, "requests and concurrency must be positive")
		os.Exit(2)
	}

	conn, err := grpc.Dial(*sidecar, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
	defer conn.Close()
	client := minimeshv1.NewProxyServiceClient(conn)
	result := summary{Requests: *requests, Codes: map[string]int{}, Responses: map[string]int{}}
	jobs := make(chan int)
	var mu sync.Mutex
	var wg sync.WaitGroup
	started := time.Now()
	workers := *concurrency
	if workers > *requests {
		workers = *requests
	}
	for worker := 0; worker < workers; worker++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for id := range jobs {
				ctx, cancel := context.WithTimeout(context.Background(), *timeout)
				response, callErr := client.Invoke(ctx, &minimeshv1.ProxyRequest{
					Target:     "service://" + *service,
					FullMethod: minimeshv1.EchoService_Echo_FullMethodName,
					Payload:    &minimeshv1.Payload{Data: []byte(fmt.Sprintf("stage16-%d", id))},
				})
				cancel()
				mu.Lock()
				if callErr != nil {
					result.Errors++
					result.Codes[status.Code(callErr).String()]++
				} else {
					result.Success++
					body := string(response.GetPayload().GetData())
					backend := body
					if before, _, ok := strings.Cut(body, ":"); ok {
						backend = before
					}
					result.Responses[backend]++
				}
				mu.Unlock()
			}
		}()
	}
	for id := 0; id < *requests; id++ {
		jobs <- id
	}
	close(jobs)
	wg.Wait()
	result.ElapsedMS = time.Since(started).Milliseconds()

	// Stable key order makes shell-captured evidence and report diffs readable.
	result.Codes = sortedMap(result.Codes)
	result.Responses = sortedMap(result.Responses)
	encoded, _ := json.Marshal(result)
	fmt.Println(string(encoded))
	if err := validateSummary(result, *minSuccess, *minErrors, *expectedCode); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func sortedMap(input map[string]int) map[string]int {
	keys := make([]string, 0, len(input))
	for key := range input {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	result := make(map[string]int, len(input))
	for _, key := range keys {
		result[key] = input[key]
	}
	return result
}
