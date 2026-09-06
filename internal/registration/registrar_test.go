package registration

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/minimesh/minimesh/internal/discovery"
)

func TestRunRegistersHeartbeatsAndDeregisters(t *testing.T) {
	var registered, heartbeats, deregistered atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/registry/register":
			registered.Add(1)
			w.WriteHeader(http.StatusCreated)
			_ = json.NewEncoder(w).Encode(map[string]int64{"lease_id": 42})
		case "/v1/registry/heartbeat":
			heartbeats.Add(1)
			w.WriteHeader(http.StatusNoContent)
		case "/v1/registry/deregister":
			deregistered.Add(1)
			w.WriteHeader(http.StatusNoContent)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- Run(ctx, Config{
			RegistryURL: server.URL,
			Endpoint:    discovery.Endpoint{Service: "inventory", InstanceID: "pod-1", Address: "10.0.0.2:19091"},
			TTL:         3 * time.Second,
			RetryDelay:  10 * time.Millisecond,
		})
	}()

	deadline := time.Now().Add(2 * time.Second)
	for heartbeats.Load() == 0 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if heartbeats.Load() == 0 {
		t.Fatal("registrar did not heartbeat")
	}
	cancel()
	if err := <-done; err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if registered.Load() != 1 || deregistered.Load() != 1 {
		t.Fatalf("registered=%d deregistered=%d", registered.Load(), deregistered.Load())
	}
}

func TestValidateRequiresCompleteEndpoint(t *testing.T) {
	err := validate(Config{RegistryURL: "http://registry", TTL: 15 * time.Second})
	if err == nil {
		t.Fatal("validate() expected an error")
	}
}
