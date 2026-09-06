package controlstream

import (
	"context"
	"net"
	"testing"
	"time"

	minimeshv1 "github.com/minimesh/minimesh/api/gen/minimesh/v1"
	"github.com/minimesh/minimesh/internal/discovery"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/test/bufconn"
)

type snapshotSource struct {
	snapshot     discovery.Snapshot
	watchStarted chan int64
}

func (s snapshotSource) FullSync(context.Context) (discovery.Snapshot, error) { return s.snapshot, nil }
func (s snapshotSource) Watch(ctx context.Context, startRevision int64, _ func(discovery.Event)) error {
	if s.watchStarted != nil {
		select {
		case s.watchStarted <- startRevision:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	<-ctx.Done()
	return ctx.Err()
}

func TestStreamSendsFullSnapshotAfterRegister(t *testing.T) {
	listener := bufconn.Listen(1024 * 1024)
	server := grpc.NewServer()
	source := snapshotSource{
		snapshot:     discovery.Snapshot{Revision: 7, Endpoints: []discovery.Endpoint{{Service: "echo", InstanceID: "one", Address: "127.0.0.1:19090", Metadata: map[string]string{"zone": "a"}}}},
		watchStarted: make(chan int64, 1),
	}
	minimeshv1.RegisterControlServiceServer(server, NewServer(source))
	go func() { _ = server.Serve(listener) }()
	defer server.Stop()

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	conn, err := grpc.DialContext(ctx, "bufnet", grpc.WithContextDialer(func(context.Context, string) (net.Conn, error) { return listener.Dial() }), grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	stream, err := minimeshv1.NewControlServiceClient(conn).Stream(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err := stream.Send(&minimeshv1.ControlMessage{Payload: &minimeshv1.ControlMessage_Register{Register: &minimeshv1.SidecarRegister{SidecarId: "test-sidecar"}}}); err != nil {
		t.Fatal(err)
	}
	message, err := stream.Recv()
	if err != nil {
		t.Fatal(err)
	}
	update := message.GetEndpointUpdate()
	if message.GetVersion() != 7 || update == nil || !update.GetFullSync() || len(update.GetEndpoints()) != 1 || update.GetEndpoints()[0].GetAddress() != "127.0.0.1:19090" {
		t.Fatalf("unexpected update: %#v", message)
	}
	if err := stream.Send(&minimeshv1.ControlMessage{Payload: &minimeshv1.ControlMessage_Ack{Ack: &minimeshv1.Ack{Version: 7}}}); err != nil {
		t.Fatal(err)
	}
	select {
	case got := <-source.watchStarted:
		if got != 8 {
			t.Fatalf("watch start revision=%d, want 8", got)
		}
	case <-ctx.Done():
		t.Fatal("control stream did not start etcd watch")
	}
}

func TestProtoSnapshotPreservesMetadata(t *testing.T) {
	snapshot := protoSnapshot(8, &minimeshv1.EndpointUpdate{Endpoints: []*minimeshv1.Endpoint{{Service: "echo", InstanceId: "one", Address: "one:1", Metadata: map[string]string{"zone": "a"}}}, FullSync: true})
	if snapshot.Revision != 8 || len(snapshot.Endpoints) != 1 || snapshot.Endpoints[0].Metadata["zone"] != "a" {
		t.Fatalf("unexpected snapshot: %#v", snapshot)
	}
}
