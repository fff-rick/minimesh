package discovery

import (
	"context"
	"errors"
	"testing"
)

type fakeSource struct{ start int64 }

func (f *fakeSource) FullSync(context.Context) (Snapshot, error) {
	return Snapshot{Revision: 7, Endpoints: []Endpoint{{Service: "echo", InstanceID: "i1", Address: "one"}}}, nil
}
func (f *fakeSource) Watch(_ context.Context, start int64, emit func(Event)) error {
	f.start = start
	emit(Event{Type: EventPut, Endpoint: Endpoint{Service: "echo", InstanceID: "i2", Address: "two"}, Revision: 8})
	emit(Event{Type: EventDelete, Service: "echo", InstanceID: "i1", Revision: 9})
	return errors.New("watch closed")
}
func TestRunStartsWatchAtRevisionPlusOne(t *testing.T) {
	src := &fakeSource{}
	c := NewCache()
	if err := Run(context.Background(), src, c); err == nil {
		t.Fatal("expected watch error")
	}
	if src.start != 8 {
		t.Fatalf("start=%d want 8", src.start)
	}
	got, err := c.Resolve("echo")
	if err != nil || got != "two" {
		t.Fatalf("Resolve=%q,%v", got, err)
	}
}
