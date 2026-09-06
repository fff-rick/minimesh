// Package controlstream implements the Stage 8 control-plane stream.
package controlstream

import (
	"context"
	"io"
	"log/slog"

	minimeshv1 "github.com/minimesh/minimesh/api/gen/minimesh/v1"
	"github.com/minimesh/minimesh/internal/discovery"
)

// Server publishes complete endpoint snapshots. A snapshot, rather than a
// per-client delta queue, makes delivery idempotent and recovery deterministic.
type Server struct {
	minimeshv1.UnimplementedControlServiceServer
	source discovery.Source
}

func NewServer(source discovery.Source) *Server { return &Server{source: source} }

func (s *Server) Stream(stream minimeshv1.ControlService_StreamServer) error {
	ctx, cancel := context.WithCancel(stream.Context())
	defer cancel()

	registered := make(chan *minimeshv1.SidecarRegister, 1)
	go func() {
		defer cancel()
		for {
			msg, err := stream.Recv()
			if err != nil {
				if err != io.EOF && ctx.Err() == nil {
					slog.Debug("control stream receive ended", "error", err)
				}
				return
			}
			if register := msg.GetRegister(); register != nil {
				select {
				case registered <- register:
				default:
				}
			}
			if ack := msg.GetAck(); ack != nil {
				slog.Debug("control update acknowledged", "version", ack.GetVersion())
			}
		}
	}()

	select {
	case reg := <-registered:
		slog.Info("sidecar connected to control stream", "sidecar_id", reg.GetSidecarId(), "last_applied_version", reg.GetLastAppliedVersion())
	case <-ctx.Done():
		return nil
	}

	revision, err := s.sendSnapshot(ctx, stream)
	if err != nil {
		return err
	}
	// Start at the revision immediately after the snapshot. A watch that starts
	// at "now" can miss a registration made after FullSync returns but before
	// the watch is established, leaving the sidecar with a stale empty cache.
	return s.source.Watch(ctx, revision+1, func(event discovery.Event) {
		// Watch event revisions are not used as a delta protocol: re-read the
		// authoritative snapshot so a reconnect or an ACK loss cannot create a gap.
		if _, err := s.sendSnapshot(ctx, stream); err != nil {
			cancel()
		}
	})
}

func (s *Server) sendSnapshot(ctx context.Context, stream minimeshv1.ControlService_StreamServer) (int64, error) {
	snapshot, err := s.source.FullSync(ctx)
	if err != nil {
		return 0, err
	}
	endpoints := make([]*minimeshv1.Endpoint, 0, len(snapshot.Endpoints))
	for _, endpoint := range snapshot.Endpoints {
		metadata := make(map[string]string, len(endpoint.Metadata))
		for k, v := range endpoint.Metadata {
			metadata[k] = v
		}
		endpoints = append(endpoints, &minimeshv1.Endpoint{Service: endpoint.Service, InstanceId: endpoint.InstanceID, Address: endpoint.Address, Metadata: metadata})
	}
	if err := stream.Send(&minimeshv1.ControlMessage{Version: uint64(snapshot.Revision), Payload: &minimeshv1.ControlMessage_EndpointUpdate{EndpointUpdate: &minimeshv1.EndpointUpdate{Endpoints: endpoints, FullSync: true}}}); err != nil {
		return 0, err
	}
	return snapshot.Revision, nil
}
