package transparentproxy

import (
	"context"
	"io"
	"net"
	"testing"
	"time"
)

func TestServeForwardsBothDirections(t *testing.T) {
	upstream, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer upstream.Close()
	go func() {
		connection, acceptErr := upstream.Accept()
		if acceptErr != nil {
			return
		}
		defer connection.Close()
		_, _ = io.Copy(connection, connection)
	}()

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	connected := make(chan string, 1)
	done := make(chan error, 1)
	go func() {
		done <- Serve(ctx, listener, Config{
			Resolve:      func(*net.TCPConn) (string, error) { return upstream.Addr().String(), nil },
			OnConnection: func(target string) { connected <- target },
		})
	}()

	client, err := net.DialTimeout("tcp", listener.Addr().String(), time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := client.Write([]byte("transparent")); err != nil {
		t.Fatal(err)
	}
	response := make([]byte, len("transparent"))
	if _, err := io.ReadFull(client, response); err != nil {
		t.Fatal(err)
	}
	_ = client.Close()
	if string(response) != "transparent" {
		t.Fatalf("response = %q", response)
	}
	if target := <-connected; target != upstream.Addr().String() {
		t.Fatalf("target = %q", target)
	}
	cancel()
	if err := <-done; err != nil {
		t.Fatalf("Serve() error = %v", err)
	}
}
