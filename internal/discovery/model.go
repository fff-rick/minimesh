package discovery

import (
	"errors"
	"sort"
	"sync"
)

var ErrNoEndpoint = errors.New("discovery: no endpoint available")

type Endpoint struct {
	Service    string            `json:"service"`
	InstanceID string            `json:"instance_id"`
	Address    string            `json:"address"`
	Metadata   map[string]string `json:"metadata,omitempty"`
}

type EventType int

const (
	EventPut EventType = iota + 1
	EventDelete
)

type Event struct {
	Type       EventType
	Endpoint   Endpoint
	Service    string
	InstanceID string
	Revision   int64
}

type Snapshot struct {
	Endpoints []Endpoint
	Revision  int64
}

type Cache struct {
	mu       sync.RWMutex
	services map[string]map[string]Endpoint
	revision int64
}

func NewCache() *Cache { return &Cache{services: make(map[string]map[string]Endpoint)} }

func (c *Cache) Replace(s Snapshot) {
	next := make(map[string]map[string]Endpoint)
	for _, ep := range s.Endpoints {
		if next[ep.Service] == nil {
			next[ep.Service] = make(map[string]Endpoint)
		}
		next[ep.Service][ep.InstanceID] = cloneEndpoint(ep)
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if s.Revision < c.revision {
		return
	}
	c.services, c.revision = next, s.Revision
}

func (c *Cache) Apply(e Event) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if e.Revision <= c.revision {
		return
	}
	switch e.Type {
	case EventPut:
		ep := cloneEndpoint(e.Endpoint)
		if c.services[ep.Service] == nil {
			c.services[ep.Service] = make(map[string]Endpoint)
		}
		c.services[ep.Service][ep.InstanceID] = ep
	case EventDelete:
		if instances := c.services[e.Service]; instances != nil {
			delete(instances, e.InstanceID)
			if len(instances) == 0 {
				delete(c.services, e.Service)
			}
		}
	}
	c.revision = e.Revision
}

func (c *Cache) List(service string) []Endpoint {
	c.mu.RLock()
	defer c.mu.RUnlock()
	instances := c.services[service]
	out := make([]Endpoint, 0, len(instances))
	for _, ep := range instances {
		out = append(out, cloneEndpoint(ep))
	}
	sort.Slice(out, func(i, j int) bool { return out[i].InstanceID < out[j].InstanceID })
	return out
}

// Resolve intentionally selects the first stable endpoint. Load-balancing is Stage 3.
func (c *Cache) Resolve(service string) (string, error) {
	eps := c.List(service)
	if len(eps) == 0 {
		return "", ErrNoEndpoint
	}
	return eps[0].Address, nil
}

func (c *Cache) Revision() int64 { c.mu.RLock(); defer c.mu.RUnlock(); return c.revision }

func cloneEndpoint(ep Endpoint) Endpoint {
	cp := ep
	if ep.Metadata != nil {
		cp.Metadata = make(map[string]string, len(ep.Metadata))
		for k, v := range ep.Metadata {
			cp.Metadata[k] = v
		}
	}
	return cp
}
