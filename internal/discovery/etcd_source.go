package discovery

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/minimesh/minimesh/internal/etcdhttp"
)

const keyPrefix = "/minimesh/services/"

type EtcdSource struct{ client *etcdhttp.Client }

func NewEtcdSource(endpoint string) *EtcdSource { return &EtcdSource{client: etcdhttp.New(endpoint)} }
func (s *EtcdSource) FullSync(ctx context.Context) (Snapshot, error) {
	kvs, rev, err := s.client.RangePrefix(ctx, keyPrefix)
	if err != nil {
		return Snapshot{}, err
	}
	out := Snapshot{Revision: rev}
	for _, kv := range kvs {
		var ep Endpoint
		if err := json.Unmarshal(kv.Value, &ep); err != nil {
			return Snapshot{}, fmt.Errorf("decode %s: %w", string(kv.Key), err)
		}
		out.Endpoints = append(out.Endpoints, ep)
	}
	return out, nil
}
func (s *EtcdSource) Watch(ctx context.Context, startRevision int64, emit func(Event)) error {
	return s.client.WatchPrefix(ctx, keyPrefix, startRevision, func(ev etcdhttp.WatchEvent) error {
		if ev.Type == "DELETE" {
			service, id := parseKey(string(ev.KV.Key))
			if service == "" {
				service, id = parseKey(string(ev.PrevKV.Key))
			}
			emit(Event{Type: EventDelete, Service: service, InstanceID: id, Revision: ev.Revision})
			return nil
		}
		var ep Endpoint
		if err := json.Unmarshal(ev.KV.Value, &ep); err != nil {
			return err
		}
		emit(Event{Type: EventPut, Endpoint: ep, Revision: ev.Revision})
		return nil
	})
}
func parseKey(k string) (string, string) {
	rest := strings.TrimPrefix(k, keyPrefix)
	parts := strings.Split(rest, "/")
	if len(parts) != 2 {
		return "", ""
	}
	return parts[0], parts[1]
}

type Source interface {
	FullSync(context.Context) (Snapshot, error)
	Watch(context.Context, int64, func(Event)) error
}

func Run(ctx context.Context, src Source, cache *Cache) error {
	snap, err := src.FullSync(ctx)
	if err != nil {
		return err
	}
	cache.Replace(snap)
	return src.Watch(ctx, snap.Revision+1, cache.Apply)
}
