package resilience

import (
	"errors"
	"fmt"
	"sync"
	"time"
)

var ErrCircuitOpen = errors.New("circuit breaker open")

type CircuitState uint8

const (
	CircuitClosed CircuitState = iota
	CircuitOpen
	CircuitHalfOpen
)

func (s CircuitState) String() string {
	switch s {
	case CircuitClosed:
		return "closed"
	case CircuitOpen:
		return "open"
	case CircuitHalfOpen:
		return "half-open"
	default:
		return "unknown"
	}
}

type CircuitBreakerConfig struct {
	WindowSize           int
	MinimumRequests      int
	FailureRateThreshold float64
	Cooldown             time.Duration
	HalfOpenMaxRequests  int
}

func (c CircuitBreakerConfig) normalized() CircuitBreakerConfig {
	if c.WindowSize <= 0 {
		c.WindowSize = 20
	}
	if c.MinimumRequests <= 0 {
		c.MinimumRequests = c.WindowSize
	}
	if c.MinimumRequests > c.WindowSize {
		c.MinimumRequests = c.WindowSize
	}
	if c.FailureRateThreshold <= 0 || c.FailureRateThreshold > 1 {
		c.FailureRateThreshold = 0.5
	}
	if c.Cooldown <= 0 {
		c.Cooldown = 5 * time.Second
	}
	if c.HalfOpenMaxRequests <= 0 {
		c.HalfOpenMaxRequests = 1
	}
	return c
}

type CircuitSnapshot struct {
	State             CircuitState
	WindowRequests    int
	WindowFailures    int
	FailureRate       float64
	HalfOpenInFlight  int
	HalfOpenSuccesses int
	RejectedTotal     uint64
	OpenedTotal       uint64
}

type CircuitPermit struct {
	breaker    *CircuitBreaker
	generation uint64
	state      CircuitState
	once       sync.Once
}

// Done must be called exactly once for every granted permit. success indicates
// whether the upstream attempt should be treated as healthy by the breaker.
func (p *CircuitPermit) Done(success bool) {
	if p == nil || p.breaker == nil {
		return
	}
	p.once.Do(func() { p.breaker.complete(p.generation, p.state, success) })
}

type CircuitBreaker struct {
	mu  sync.Mutex
	cfg CircuitBreakerConfig
	now func() time.Time

	state      CircuitState
	generation uint64
	openedAt   time.Time

	window   []bool // true means failure
	next     int
	count    int
	failures int

	halfOpenPermitted int
	halfOpenInFlight  int
	halfOpenSuccesses int

	rejectedTotal uint64
	openedTotal   uint64
}

func NewCircuitBreaker(cfg CircuitBreakerConfig) *CircuitBreaker {
	return newCircuitBreakerWithClock(cfg, time.Now)
}

func newCircuitBreakerWithClock(cfg CircuitBreakerConfig, now func() time.Time) *CircuitBreaker {
	cfg = cfg.normalized()
	return &CircuitBreaker{cfg: cfg, now: now, state: CircuitClosed, window: make([]bool, cfg.WindowSize)}
}

func (b *CircuitBreaker) Allow() (*CircuitPermit, error) {
	b.mu.Lock()
	defer b.mu.Unlock()

	now := b.now()
	if b.state == CircuitOpen {
		if now.Sub(b.openedAt) < b.cfg.Cooldown {
			b.rejectedTotal++
			return nil, ErrCircuitOpen
		}
		b.transitionHalfOpen()
	}

	if b.state == CircuitHalfOpen {
		if b.halfOpenPermitted >= b.cfg.HalfOpenMaxRequests {
			b.rejectedTotal++
			return nil, ErrCircuitOpen
		}
		b.halfOpenPermitted++
		b.halfOpenInFlight++
	}

	return &CircuitPermit{breaker: b, generation: b.generation, state: b.state}, nil
}

func (b *CircuitBreaker) complete(generation uint64, permitState CircuitState, success bool) {
	b.mu.Lock()
	defer b.mu.Unlock()

	// Ignore completions from a previous breaker generation. This matters when
	// concurrent half-open probes finish after one probe has already reopened.
	if generation != b.generation {
		return
	}

	switch permitState {
	case CircuitClosed:
		if b.state != CircuitClosed {
			return
		}
		b.recordClosed(!success)
		if b.count >= b.cfg.MinimumRequests && float64(b.failures)/float64(b.count) >= b.cfg.FailureRateThreshold {
			b.transitionOpen()
		}
	case CircuitHalfOpen:
		if b.state != CircuitHalfOpen {
			return
		}
		if b.halfOpenInFlight > 0 {
			b.halfOpenInFlight--
		}
		if !success {
			b.transitionOpen()
			return
		}
		b.halfOpenSuccesses++
		if b.halfOpenSuccesses >= b.cfg.HalfOpenMaxRequests {
			b.transitionClosed()
		}
	}
}

func (b *CircuitBreaker) recordClosed(failure bool) {
	if b.count < len(b.window) {
		b.count++
	} else if b.window[b.next] {
		b.failures--
	}
	b.window[b.next] = failure
	if failure {
		b.failures++
	}
	b.next = (b.next + 1) % len(b.window)
}

func (b *CircuitBreaker) transitionOpen() {
	b.state = CircuitOpen
	b.generation++
	b.openedAt = b.now()
	b.halfOpenPermitted = 0
	b.halfOpenInFlight = 0
	b.halfOpenSuccesses = 0
	b.openedTotal++
}

func (b *CircuitBreaker) transitionHalfOpen() {
	b.state = CircuitHalfOpen
	b.generation++
	b.halfOpenPermitted = 0
	b.halfOpenInFlight = 0
	b.halfOpenSuccesses = 0
}

func (b *CircuitBreaker) transitionClosed() {
	b.state = CircuitClosed
	b.generation++
	b.next = 0
	b.count = 0
	b.failures = 0
	for i := range b.window {
		b.window[i] = false
	}
	b.halfOpenPermitted = 0
	b.halfOpenInFlight = 0
	b.halfOpenSuccesses = 0
}

func (b *CircuitBreaker) Snapshot() CircuitSnapshot {
	b.mu.Lock()
	defer b.mu.Unlock()
	rate := 0.0
	if b.count > 0 {
		rate = float64(b.failures) / float64(b.count)
	}
	return CircuitSnapshot{
		State: b.state, WindowRequests: b.count, WindowFailures: b.failures,
		FailureRate: rate, HalfOpenInFlight: b.halfOpenInFlight,
		HalfOpenSuccesses: b.halfOpenSuccesses, RejectedTotal: b.rejectedTotal, OpenedTotal: b.openedTotal,
	}
}

// CircuitBreakerSet provides one breaker per upstream key (normally endpoint address).
type CircuitBreakerSet struct {
	mu       sync.Mutex
	cfg      CircuitBreakerConfig
	breakers map[string]*CircuitBreaker
}

func NewCircuitBreakerSet(cfg CircuitBreakerConfig) *CircuitBreakerSet {
	return &CircuitBreakerSet{cfg: cfg.normalized(), breakers: make(map[string]*CircuitBreaker)}
}

func (s *CircuitBreakerSet) Breaker(key string) *CircuitBreaker {
	s.mu.Lock()
	defer s.mu.Unlock()
	if b := s.breakers[key]; b != nil {
		return b
	}
	b := NewCircuitBreaker(s.cfg)
	s.breakers[key] = b
	return b
}

func (s *CircuitBreakerSet) Allow(key string) (*CircuitPermit, error) {
	if s == nil {
		return &CircuitPermit{}, nil
	}
	if key == "" {
		return nil, fmt.Errorf("circuit breaker key is empty")
	}
	return s.Breaker(key).Allow()
}

func (s *CircuitBreakerSet) Snapshot(key string) CircuitSnapshot {
	return s.Breaker(key).Snapshot()
}
