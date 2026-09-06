package loadbalance

import (
	"strconv"
	"sync"

	"github.com/minimesh/minimesh/internal/discovery"
)

const WeightMetadataKey = "minimesh.weight"

type weightedNode struct {
	endpoint discovery.Endpoint
	weight   int
	current  int
}

type WeightedRoundRobinPicker struct {
	mu    sync.Mutex
	nodes []weightedNode
}

func NewWeightedRoundRobin() *WeightedRoundRobinPicker { return &WeightedRoundRobinPicker{} }

func (p *WeightedRoundRobinPicker) Update(endpoints []discovery.Endpoint) {
	p.mu.Lock()
	defer p.mu.Unlock()
	nodes := make([]weightedNode, 0, len(endpoints))
	for _, ep := range endpoints {
		nodes = append(nodes, weightedNode{endpoint: ep, weight: endpointWeight(ep)})
	}
	p.nodes = nodes
}

func (p *WeightedRoundRobinPicker) Pick() (PickResult, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if len(p.nodes) == 0 {
		return PickResult{}, ErrNoEndpoint
	}
	total := 0
	best := 0
	for i := range p.nodes {
		p.nodes[i].current += p.nodes[i].weight
		total += p.nodes[i].weight
		if p.nodes[i].current > p.nodes[best].current {
			best = i
		}
	}
	p.nodes[best].current -= total
	return PickResult{Endpoint: p.nodes[best].endpoint, Done: func() {}}, nil
}

func endpointWeight(ep discovery.Endpoint) int {
	if ep.Metadata == nil {
		return 1
	}
	weight, err := strconv.Atoi(ep.Metadata[WeightMetadataKey])
	if err != nil || weight <= 0 {
		return 1
	}
	return weight
}
