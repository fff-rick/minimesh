package loadbalance

import (
	"fmt"
	"sort"
	"strings"
	"sync"

	"github.com/minimesh/minimesh/internal/discovery"
)

type EndpointLister interface {
	List(service string) []discovery.Endpoint
}

type servicePicker struct {
	picker      Picker
	fingerprint string
}

type Resolver struct {
	source    EndpointLister
	algorithm Algorithm
	mu        sync.Mutex
	services  map[string]*servicePicker
}

func NewResolver(source EndpointLister, algorithm Algorithm) (*Resolver, error) {
	if source == nil {
		return nil, fmt.Errorf("loadbalance: nil endpoint source")
	}
	if _, err := New(algorithm); err != nil {
		return nil, err
	}
	return &Resolver{source: source, algorithm: algorithm, services: make(map[string]*servicePicker)}, nil
}

func (r *Resolver) Pick(service string) (string, func(), error) {
	endpoints := r.source.List(service)
	fp := endpointFingerprint(endpoints)

	r.mu.Lock()
	state := r.services[service]
	if state == nil {
		picker, _ := New(r.algorithm)
		state = &servicePicker{picker: picker}
		r.services[service] = state
	}
	if state.fingerprint != fp {
		state.picker.Update(endpoints)
		state.fingerprint = fp
	}
	picker := state.picker
	r.mu.Unlock()

	result, err := picker.Pick()
	if err != nil {
		return "", nil, err
	}
	done := result.Done
	if done == nil {
		done = func() {}
	}
	return result.Endpoint.Address, done, nil
}

func endpointFingerprint(endpoints []discovery.Endpoint) string {
	parts := make([]string, 0, len(endpoints))
	for _, ep := range endpoints {
		parts = append(parts, ep.InstanceID+"="+ep.Address+"@"+fmt.Sprint(endpointWeight(ep)))
	}
	sort.Strings(parts)
	return strings.Join(parts, "|")
}

// Resolve preserves the Stage 2 resolver contract. Stage 3 proxy code detects
// Pick and keeps Done scoped to the complete upstream request.
func (r *Resolver) Resolve(service string) (string, error) {
	address, done, err := r.Pick(service)
	if done != nil {
		done()
	}
	return address, err
}
