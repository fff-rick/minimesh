// Package registration keeps a service endpoint registered through the
// control-plane HTTP API. It is used by a sidecar whose lifecycle is coupled
// to the business container in the same Kubernetes Pod.
package registration

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/minimesh/minimesh/internal/discovery"
)

type Config struct {
	RegistryURL  string
	Endpoint     discovery.Endpoint
	TTL          time.Duration
	RetryDelay   time.Duration
	HTTPClient   *http.Client
	OnRegistered func(leaseID int64)
}

type registerRequest struct {
	Endpoint discovery.Endpoint `json:"endpoint"`
	TTL      int64              `json:"ttl_seconds"`
}

type registerResponse struct {
	LeaseID int64 `json:"lease_id"`
}

// Run blocks until ctx is cancelled. Failed registration or heartbeat calls
// are retried, so a control-plane restart does not require restarting the Pod.
func Run(ctx context.Context, cfg Config) error {
	if err := validate(cfg); err != nil {
		return err
	}
	client := cfg.HTTPClient
	if client == nil {
		client = &http.Client{Timeout: 3 * time.Second}
	}
	retryDelay := cfg.RetryDelay
	if retryDelay <= 0 {
		retryDelay = 2 * time.Second
	}

	for {
		leaseID, err := register(ctx, client, cfg)
		if err == nil {
			if cfg.OnRegistered != nil {
				cfg.OnRegistered(leaseID)
			}
			err = keepAlive(ctx, client, cfg, leaseID)
			if ctx.Err() != nil {
				bestEffortDeregister(client, cfg, leaseID)
				return nil
			}
		}

		timer := time.NewTimer(retryDelay)
		select {
		case <-ctx.Done():
			timer.Stop()
			return nil
		case <-timer.C:
		}
	}
}

func validate(cfg Config) error {
	if strings.TrimSpace(cfg.RegistryURL) == "" {
		return fmt.Errorf("registry URL is required")
	}
	if strings.TrimSpace(cfg.Endpoint.Service) == "" || strings.TrimSpace(cfg.Endpoint.InstanceID) == "" || strings.TrimSpace(cfg.Endpoint.Address) == "" {
		return fmt.Errorf("service, instance ID, and address are required")
	}
	if cfg.TTL < time.Second {
		return fmt.Errorf("registration TTL must be at least one second")
	}
	return nil
}

func register(ctx context.Context, client *http.Client, cfg Config) (int64, error) {
	body, err := json.Marshal(registerRequest{Endpoint: cfg.Endpoint, TTL: int64(cfg.TTL / time.Second)})
	if err != nil {
		return 0, err
	}
	var response registerResponse
	if err := postJSON(ctx, client, cfg.RegistryURL+"/v1/registry/register", body, http.StatusCreated, &response); err != nil {
		return 0, err
	}
	if response.LeaseID == 0 {
		return 0, fmt.Errorf("registry returned an empty lease ID")
	}
	return response.LeaseID, nil
}

func keepAlive(ctx context.Context, client *http.Client, cfg Config, leaseID int64) error {
	interval := cfg.TTL / 3
	if interval < time.Second {
		interval = time.Second
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			body, _ := json.Marshal(map[string]int64{"lease_id": leaseID})
			if err := postJSON(ctx, client, cfg.RegistryURL+"/v1/registry/heartbeat", body, http.StatusNoContent, nil); err != nil {
				return err
			}
		}
	}
}

func bestEffortDeregister(client *http.Client, cfg Config, leaseID int64) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	body, _ := json.Marshal(map[string]any{
		"lease_id":    leaseID,
		"service":     cfg.Endpoint.Service,
		"instance_id": cfg.Endpoint.InstanceID,
	})
	_ = postJSON(ctx, client, cfg.RegistryURL+"/v1/registry/deregister", body, http.StatusNoContent, nil)
}

func postJSON(ctx context.Context, client *http.Client, url string, body []byte, expected int, response any) error {
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(url, "/"), bytes.NewReader(body))
	if err != nil {
		return err
	}
	request.Header.Set("Content-Type", "application/json")
	result, err := client.Do(request)
	if err != nil {
		return err
	}
	defer result.Body.Close()
	if result.StatusCode != expected {
		message, _ := io.ReadAll(io.LimitReader(result.Body, 1024))
		return fmt.Errorf("registry returned %s: %s", result.Status, strings.TrimSpace(string(message)))
	}
	if response != nil {
		return json.NewDecoder(result.Body).Decode(response)
	}
	return nil
}
