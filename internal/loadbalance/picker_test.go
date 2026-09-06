package loadbalance

import (
	"fmt"
	"math"
	"sync"
	"testing"

	"github.com/minimesh/minimesh/internal/discovery"
)

func endpoints(n int) []discovery.Endpoint {
	out := make([]discovery.Endpoint, 0, n)
	for i := 0; i < n; i++ {
		out = append(out, discovery.Endpoint{Service: "echo", InstanceID: fmt.Sprintf("echo-%d", i+1), Address: fmt.Sprintf("127.0.0.1:%d", 19090+i)})
	}
	return out
}

func TestRoundRobinDistribution10000(t *testing.T) {
	p := NewRoundRobin()
	p.Update(endpoints(3))
	counts := map[string]int{}
	for i := 0; i < 10000; i++ {
		r, err := p.Pick()
		if err != nil {
			t.Fatal(err)
		}
		counts[r.Endpoint.InstanceID]++
	}
	for id, got := range counts {
		if math.Abs(float64(got)-10000.0/3.0) > 1 {
			t.Fatalf("%s got=%d, want near 3333", id, got)
		}
	}
}

func TestWeightedRoundRobinDistribution10000(t *testing.T) {
	eps := endpoints(3)
	eps[0].Metadata = map[string]string{WeightMetadataKey: "1"}
	eps[1].Metadata = map[string]string{WeightMetadataKey: "2"}
	eps[2].Metadata = map[string]string{WeightMetadataKey: "7"}
	p := NewWeightedRoundRobin()
	p.Update(eps)
	counts := map[string]int{}
	for i := 0; i < 10000; i++ {
		r, err := p.Pick()
		if err != nil {
			t.Fatal(err)
		}
		counts[r.Endpoint.InstanceID]++
	}
	want := map[string]int{"echo-1": 1000, "echo-2": 2000, "echo-3": 7000}
	for id, expected := range want {
		if got := counts[id]; got != expected {
			t.Fatalf("%s got=%d want=%d", id, got, expected)
		}
	}
}

func TestLeastConnectionsTracksInflight(t *testing.T) {
	p := NewLeastConnections()
	p.Update(endpoints(3))
	a, _ := p.Pick()
	b, _ := p.Pick()
	c, _ := p.Pick()
	if a.Endpoint.InstanceID != "echo-1" || b.Endpoint.InstanceID != "echo-2" || c.Endpoint.InstanceID != "echo-3" {
		t.Fatalf("unexpected first picks: %s %s %s", a.Endpoint.InstanceID, b.Endpoint.InstanceID, c.Endpoint.InstanceID)
	}
	d, _ := p.Pick()
	if d.Endpoint.InstanceID != "echo-1" {
		t.Fatalf("tie should select stable first endpoint, got %s", d.Endpoint.InstanceID)
	}
	a.Done()
	d.Done()
	e, _ := p.Pick()
	if e.Endpoint.InstanceID != "echo-1" {
		t.Fatalf("released endpoint should be least loaded, got %s", e.Endpoint.InstanceID)
	}
	b.Done()
	c.Done()
	e.Done()
}

func TestEndpointDynamicUpdate(t *testing.T) {
	p := NewRoundRobin()
	p.Update(endpoints(3))
	p.Update(endpoints(2))
	for i := 0; i < 100; i++ {
		r, _ := p.Pick()
		if r.Endpoint.InstanceID == "echo-3" {
			t.Fatal("removed endpoint was selected")
		}
	}
	eps := endpoints(4)
	p.Update(eps)
	seen := map[string]bool{}
	for i := 0; i < 8; i++ {
		r, _ := p.Pick()
		seen[r.Endpoint.InstanceID] = true
	}
	if !seen["echo-4"] {
		t.Fatal("new endpoint was not selected")
	}
}

func TestRoundRobinConcurrentPickAndUpdate(t *testing.T) {
	p := NewRoundRobin()
	p.Update(endpoints(3))
	var wg sync.WaitGroup
	for g := 0; g < 16; g++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < 2000; i++ {
				if _, err := p.Pick(); err != nil {
					t.Errorf("pick: %v", err)
					return
				}
			}
		}()
	}
	for i := 0; i < 100; i++ {
		if i%2 == 0 {
			p.Update(endpoints(2))
		} else {
			p.Update(endpoints(3))
		}
	}
	wg.Wait()
}
