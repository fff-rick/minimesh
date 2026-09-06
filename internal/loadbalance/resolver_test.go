package loadbalance

import (
	"testing"

	"github.com/minimesh/minimesh/internal/discovery"
)

type fakeLister struct{ eps []discovery.Endpoint }

func (f *fakeLister) List(string) []discovery.Endpoint {
	return append([]discovery.Endpoint(nil), f.eps...)
}

func TestResolverObservesEndpointChanges(t *testing.T) {
	source := &fakeLister{eps: endpoints(2)}
	r, err := NewResolver(source, RoundRobin)
	if err != nil {
		t.Fatal(err)
	}
	got1, done, err := r.Pick("echo")
	if err != nil {
		t.Fatal(err)
	}
	done()
	got2, done, err := r.Pick("echo")
	if err != nil {
		t.Fatal(err)
	}
	done()
	if got1 == got2 {
		t.Fatalf("round robin did not rotate: %s %s", got1, got2)
	}
	source.eps = []discovery.Endpoint{{Service: "echo", InstanceID: "echo-9", Address: "127.0.0.1:19999"}}
	for i := 0; i < 10; i++ {
		got, done, err := r.Pick("echo")
		if err != nil {
			t.Fatal(err)
		}
		done()
		if got != "127.0.0.1:19999" {
			t.Fatalf("stale endpoint selected: %s", got)
		}
	}
}
