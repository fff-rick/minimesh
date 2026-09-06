package controlstream

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"sync"
	"time"

	minimeshv1 "github.com/minimesh/minimesh/api/gen/minimesh/v1"
	"github.com/minimesh/minimesh/internal/discovery"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

type ClientConfig struct {
	Address           string
	SidecarID         string
	HeartbeatInterval time.Duration
	InitialBackoff    time.Duration
	MaxBackoff        time.Duration
	OnSnapshot        func()
}

func (c ClientConfig) normalized() ClientConfig {
	if c.HeartbeatInterval <= 0 {
		c.HeartbeatInterval = 15 * time.Second
	}
	if c.InitialBackoff <= 0 {
		c.InitialBackoff = 100 * time.Millisecond
	}
	if c.MaxBackoff <= 0 || c.MaxBackoff < c.InitialBackoff {
		c.MaxBackoff = 2 * time.Second
	}
	return c
}

// Run reconnects until ctx is cancelled. Every new stream starts with a full
// snapshot, so local cache state remains useful while the control plane is down.
func Run(ctx context.Context, cfg ClientConfig, cache *discovery.Cache) {
	cfg = cfg.normalized()
	backoff := cfg.InitialBackoff
	for ctx.Err() == nil {
		err := runOnce(ctx, cfg, cache)
		if ctx.Err() != nil {
			return
		}
		slog.Warn("control stream disconnected; reconnecting", "error", err, "backoff", backoff)
		timer := time.NewTimer(backoff)
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
		}
		backoff *= 2
		if backoff > cfg.MaxBackoff {
			backoff = cfg.MaxBackoff
		}
	}
}

func runOnce(ctx context.Context, cfg ClientConfig, cache *discovery.Cache) error {
	conn, err := grpc.DialContext(ctx, cfg.Address, grpc.WithTransportCredentials(insecure.NewCredentials()), grpc.WithBlock())
	if err != nil {
		return err
	}
	defer conn.Close()
	stream, err := minimeshv1.NewControlServiceClient(conn).Stream(ctx)
	if err != nil {
		return err
	}
	var sendMu sync.Mutex
	send := func(message *minimeshv1.ControlMessage) error {
		sendMu.Lock()
		defer sendMu.Unlock()
		return stream.Send(message)
	}
	if err := send(&minimeshv1.ControlMessage{Payload: &minimeshv1.ControlMessage_Register{Register: &minimeshv1.SidecarRegister{SidecarId: cfg.SidecarID, LastAppliedVersion: uint64(cache.Revision())}}}); err != nil {
		return err
	}

	heartbeatDone := make(chan struct{})
	defer close(heartbeatDone)
	go func() {
		ticker := time.NewTicker(cfg.HeartbeatInterval)
		defer ticker.Stop()
		for {
			select {
			case <-heartbeatDone:
				return
			case <-ticker.C:
				if err := send(&minimeshv1.ControlMessage{Payload: &minimeshv1.ControlMessage_Heartbeat{Heartbeat: &minimeshv1.SidecarHeartbeat{SidecarId: cfg.SidecarID}}}); err != nil {
					return
				}
			}
		}
	}()

	for {
		message, err := stream.Recv()
		if err != nil {
			if err == io.EOF {
				return nil
			}
			return err
		}
		if update := message.GetEndpointUpdate(); update != nil {
			if !update.GetFullSync() {
				return fmt.Errorf("control stream received non-snapshot endpoint update")
			}
			cache.Replace(protoSnapshot(message.GetVersion(), update))
			if cfg.OnSnapshot != nil {
				cfg.OnSnapshot()
			}
			if err := send(&minimeshv1.ControlMessage{Payload: &minimeshv1.ControlMessage_Ack{Ack: &minimeshv1.Ack{Version: message.GetVersion()}}}); err != nil {
				return err
			}
		}
		if message.GetRouteUpdate() != nil {
			// RouteUpdate is intentionally acknowledged even before a route policy
			// consumer is introduced. This keeps mixed-version control planes from
			// treating a capable Stage 8 sidecar as disconnected.
			if err := send(&minimeshv1.ControlMessage{Payload: &minimeshv1.ControlMessage_Ack{Ack: &minimeshv1.Ack{Version: message.GetVersion()}}}); err != nil {
				return err
			}
		}
	}
}

func protoSnapshot(version uint64, update *minimeshv1.EndpointUpdate) discovery.Snapshot {
	endpoints := make([]discovery.Endpoint, 0, len(update.GetEndpoints()))
	for _, endpoint := range update.GetEndpoints() {
		metadata := make(map[string]string, len(endpoint.GetMetadata()))
		for k, v := range endpoint.GetMetadata() {
			metadata[k] = v
		}
		endpoints = append(endpoints, discovery.Endpoint{Service: endpoint.GetService(), InstanceID: endpoint.GetInstanceId(), Address: endpoint.GetAddress(), Metadata: metadata})
	}
	return discovery.Snapshot{Revision: int64(version), Endpoints: endpoints}
}
