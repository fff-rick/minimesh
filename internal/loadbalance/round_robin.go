package loadbalance

import (
	"sync/atomic"

	"github.com/minimesh/minimesh/internal/discovery"
)

type endpointSnapshot struct{ endpoints []discovery.Endpoint }

type RoundRobinPicker struct {
	snapshot atomic.Pointer[endpointSnapshot]
	next     atomic.Uint64
}

func NewRoundRobin() *RoundRobinPicker { return &RoundRobinPicker{} }

func (p *RoundRobinPicker) Update(endpoints []discovery.Endpoint) {
	cp := append([]discovery.Endpoint(nil), endpoints...)
	p.snapshot.Store(&endpointSnapshot{endpoints: cp})
}

func (p *RoundRobinPicker) Pick() (PickResult, error) {
	snap := p.snapshot.Load()
	if snap == nil || len(snap.endpoints) == 0 {
		return PickResult{}, ErrNoEndpoint
	}
	index := p.next.Add(1) - 1
	return PickResult{Endpoint: snap.endpoints[index%uint64(len(snap.endpoints))], Done: func() {}}, nil
}
