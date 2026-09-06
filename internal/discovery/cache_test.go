package discovery

import "testing"

func TestCacheFullSyncAndRevision(t *testing.T) {
	c := NewCache()
	c.Replace(Snapshot{Revision: 10, Endpoints: []Endpoint{{Service: "echo", InstanceID: "b", Address: "b:1"}, {Service: "echo", InstanceID: "a", Address: "a:1"}}})
	if got, err := c.Resolve("echo"); err != nil || got != "a:1" {
		t.Fatalf("Resolve=%q,%v want a:1", got, err)
	}
	c.Apply(Event{Type: EventPut, Endpoint: Endpoint{Service: "echo", InstanceID: "c", Address: "c:1"}, Revision: 11})
	c.Apply(Event{Type: EventDelete, Service: "echo", InstanceID: "a", Revision: 12})
	if got, err := c.Resolve("echo"); err != nil || got != "b:1" {
		t.Fatalf("Resolve after delete=%q,%v want b:1", got, err)
	}
	c.Apply(Event{Type: EventPut, Endpoint: Endpoint{Service: "echo", InstanceID: "a", Address: "stale:1"}, Revision: 9})
	if got, _ := c.Resolve("echo"); got != "b:1" {
		t.Fatalf("stale revision applied: %q", got)
	}
	if c.Revision() != 12 {
		t.Fatalf("revision=%d want 12", c.Revision())
	}
}
