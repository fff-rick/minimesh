package registry

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/minimesh/minimesh/internal/discovery"
)

type fakeStore struct {
	ep         discovery.Endpoint
	lease      int64
	heartbeats int
	removed    bool
}

func (f *fakeStore) Register(_ context.Context, ep discovery.Endpoint, _ int64) (int64, error) {
	f.ep = ep
	f.lease = 42
	return 42, nil
}
func (f *fakeStore) Heartbeat(_ context.Context, lease int64) error {
	if lease == 42 {
		f.heartbeats++
	}
	return nil
}
func (f *fakeStore) Deregister(_ context.Context, _ discovery.Endpoint, _ int64) error {
	f.removed = true
	return nil
}
func (f *fakeStore) List(_ context.Context, _ string) ([]discovery.Endpoint, int64, error) {
	return []discovery.Endpoint{f.ep}, 99, nil
}
func doJSON(t *testing.T, h http.Handler, method, path string, v any) *httptest.ResponseRecorder {
	t.Helper()
	b, _ := json.Marshal(v)
	r := httptest.NewRequest(method, path, bytes.NewReader(b))
	r.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}
func TestRegistryLifecycle(t *testing.T) {
	s := &fakeStore{}
	h := NewAPI(s, 15).Handler()
	w := doJSON(t, h, "POST", "/v1/registry/register", map[string]any{"endpoint": map[string]any{"service": "echo", "instance_id": "i1", "address": "127.0.0.1:19090"}})
	if w.Code != 201 {
		t.Fatalf("register=%d body=%s", w.Code, w.Body.String())
	}
	w = doJSON(t, h, "POST", "/v1/registry/heartbeat", map[string]any{"lease_id": 42})
	if w.Code != 204 || s.heartbeats != 1 {
		t.Fatalf("heartbeat=%d count=%d", w.Code, s.heartbeats)
	}
	w = httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("GET", "/v1/registry/services/echo", nil))
	if w.Code != 200 || w.Header().Get("X-MiniMesh-Revision") != "99" {
		t.Fatalf("list code=%d rev=%s", w.Code, w.Header().Get("X-MiniMesh-Revision"))
	}
	w = doJSON(t, h, "POST", "/v1/registry/deregister", map[string]any{"lease_id": 42, "service": "echo", "instance_id": "i1"})
	if w.Code != 204 || !s.removed {
		t.Fatalf("deregister=%d removed=%v", w.Code, s.removed)
	}
}
