package loadbalance

import (
	"sync"
	"sync/atomic"

	"github.com/minimesh/minimesh/internal/discovery"
)

type leastNode struct {
	endpoint discovery.Endpoint
	active   atomic.Int64
}

type LeastConnectionsPicker struct {
	mu    sync.RWMutex
	nodes []*leastNode
}

func NewLeastConnections() *LeastConnectionsPicker { return &LeastConnectionsPicker{} }

func (p *LeastConnectionsPicker) Update(endpoints []discovery.Endpoint) {
	p.mu.Lock()
	defer p.mu.Unlock()
	existing := make(map[string]*leastNode, len(p.nodes))
	for _, node := range p.nodes {
		existing[node.endpoint.InstanceID] = node
	}
	next := make([]*leastNode, 0, len(endpoints))
	for _, ep := range endpoints {
		if node := existing[ep.InstanceID]; node != nil {
			node.endpoint = ep
			next = append(next, node)
		} else {
			next = append(next, &leastNode{endpoint: ep})
		}
	}
	p.nodes = next
}

func (p *LeastConnectionsPicker) Pick() (PickResult, error) {
	p.mu.RLock()
	if len(p.nodes) == 0 {
		p.mu.RUnlock()
		return PickResult{}, ErrNoEndpoint
	}
	best := p.nodes[0]
	bestCount := best.active.Load()
	for _, node := range p.nodes[1:] {
		if count := node.active.Load(); count < bestCount {
			best, bestCount = node, count
		}
	}
	best.active.Add(1)
	ep := best.endpoint
	p.mu.RUnlock()

	var once sync.Once
	return PickResult{Endpoint: ep, Done: func() { once.Do(func() { best.active.Add(-1) }) }}, nil
}
