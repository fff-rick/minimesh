package registry

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/minimesh/minimesh/internal/discovery"
	"github.com/minimesh/minimesh/internal/etcdhttp"
)

type Store interface {
	Register(context.Context, discovery.Endpoint, int64) (int64, error)
	Heartbeat(context.Context, int64) error
	Deregister(context.Context, discovery.Endpoint, int64) error
	List(context.Context, string) ([]discovery.Endpoint, int64, error)
}

type EtcdStore struct{ client *etcdhttp.Client }

func NewEtcdStore(endpoint string) *EtcdStore { return &EtcdStore{client: etcdhttp.New(endpoint)} }
func key(ep discovery.Endpoint) string {
	return "/minimesh/services/" + ep.Service + "/" + ep.InstanceID
}
func (s *EtcdStore) Register(ctx context.Context, ep discovery.Endpoint, ttl int64) (int64, error) {
	lease, err := s.client.Grant(ctx, ttl)
	if err != nil {
		return 0, err
	}
	b, _ := json.Marshal(ep)
	if err := s.client.Put(ctx, key(ep), b, lease); err != nil {
		_ = s.client.Revoke(ctx, lease)
		return 0, err
	}
	return lease, nil
}
func (s *EtcdStore) Heartbeat(ctx context.Context, lease int64) error {
	return s.client.KeepAliveOnce(ctx, lease)
}
func (s *EtcdStore) Deregister(ctx context.Context, ep discovery.Endpoint, lease int64) error {
	if err := s.client.Delete(ctx, key(ep)); err != nil {
		return err
	}
	if lease != 0 {
		return s.client.Revoke(ctx, lease)
	}
	return nil
}
func (s *EtcdStore) List(ctx context.Context, service string) ([]discovery.Endpoint, int64, error) {
	kvs, rev, err := s.client.RangePrefix(ctx, "/minimesh/services/"+service+"/")
	if err != nil {
		return nil, 0, err
	}
	out := make([]discovery.Endpoint, 0, len(kvs))
	for _, kv := range kvs {
		var ep discovery.Endpoint
		if err := json.Unmarshal(kv.Value, &ep); err != nil {
			return nil, 0, err
		}
		out = append(out, ep)
	}
	return out, rev, nil
}

type API struct {
	store      Store
	defaultTTL int64
}

func NewAPI(store Store, defaultTTL int64) *API { return &API{store: store, defaultTTL: defaultTTL} }

type registerRequest struct {
	Endpoint discovery.Endpoint `json:"endpoint"`
	TTL      int64              `json:"ttl_seconds"`
}
type leaseRequest struct {
	LeaseID    int64  `json:"lease_id"`
	Service    string `json:"service"`
	InstanceID string `json:"instance_id"`
}

func (a *API) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /v1/registry/register", a.register)
	mux.HandleFunc("POST /v1/registry/heartbeat", a.heartbeat)
	mux.HandleFunc("POST /v1/registry/deregister", a.deregister)
	mux.HandleFunc("GET /v1/registry/services/", a.list)
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok\n"))
	})
	return mux
}
func (a *API) register(w http.ResponseWriter, r *http.Request) {
	var req registerRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, 400, err)
		return
	}
	if strings.TrimSpace(req.Endpoint.Service) == "" || strings.TrimSpace(req.Endpoint.InstanceID) == "" || strings.TrimSpace(req.Endpoint.Address) == "" {
		writeErr(w, 400, fmt.Errorf("service, instance_id and address are required"))
		return
	}
	if req.TTL <= 0 {
		req.TTL = a.defaultTTL
	}
	lease, err := a.store.Register(r.Context(), req.Endpoint, req.TTL)
	if err != nil {
		writeErr(w, 502, err)
		return
	}
	writeJSON(w, 201, map[string]any{"lease_id": lease, "ttl_seconds": req.TTL})
}
func (a *API) heartbeat(w http.ResponseWriter, r *http.Request) {
	var req leaseRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, 400, err)
		return
	}
	if req.LeaseID == 0 {
		writeErr(w, 400, fmt.Errorf("lease_id is required"))
		return
	}
	if err := a.store.Heartbeat(r.Context(), req.LeaseID); err != nil {
		writeErr(w, 502, err)
		return
	}
	w.WriteHeader(204)
}
func (a *API) deregister(w http.ResponseWriter, r *http.Request) {
	var req leaseRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, 400, err)
		return
	}
	if req.Service == "" || req.InstanceID == "" {
		writeErr(w, 400, fmt.Errorf("service and instance_id are required"))
		return
	}
	if err := a.store.Deregister(r.Context(), discovery.Endpoint{Service: req.Service, InstanceID: req.InstanceID}, req.LeaseID); err != nil {
		writeErr(w, 502, err)
		return
	}
	w.WriteHeader(204)
}
func (a *API) list(w http.ResponseWriter, r *http.Request) {
	service := strings.TrimPrefix(r.URL.Path, "/v1/registry/services/")
	eps, rev, err := a.store.List(r.Context(), service)
	if err != nil {
		writeErr(w, 502, err)
		return
	}
	w.Header().Set("X-MiniMesh-Revision", strconv.FormatInt(rev, 10))
	writeJSON(w, 200, eps)
}
func writeErr(w http.ResponseWriter, code int, err error) {
	writeJSON(w, code, map[string]string{"error": err.Error()})
}
func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}
