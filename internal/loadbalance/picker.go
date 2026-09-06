package loadbalance

import (
	"errors"

	"github.com/minimesh/minimesh/internal/discovery"
)

var ErrNoEndpoint = errors.New("loadbalance: no endpoint available")

type DoneFunc func()

type PickResult struct {
	Endpoint discovery.Endpoint
	Done     DoneFunc
}

type Picker interface {
	Update([]discovery.Endpoint)
	Pick() (PickResult, error)
}

type Algorithm string

const (
	RoundRobin         Algorithm = "round_robin"
	WeightedRoundRobin Algorithm = "weighted_round_robin"
	LeastConnections   Algorithm = "least_connections"
)

func New(algorithm Algorithm) (Picker, error) {
	switch algorithm {
	case RoundRobin:
		return NewRoundRobin(), nil
	case WeightedRoundRobin:
		return NewWeightedRoundRobin(), nil
	case LeastConnections:
		return NewLeastConnections(), nil
	default:
		return nil, errors.New("loadbalance: unknown algorithm: " + string(algorithm))
	}
}
