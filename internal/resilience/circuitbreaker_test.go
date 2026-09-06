package resilience

import (
	"errors"
	"sync"
	"testing"
	"time"
)

func testBreaker(now *time.Time, cfg CircuitBreakerConfig) *CircuitBreaker {
	return newCircuitBreakerWithClock(cfg, func() time.Time { return *now })
}

func attempt(t *testing.T, b *CircuitBreaker, success bool) error {
	t.Helper()
	p, err := b.Allow()
	if err != nil {
		return err
	}
	p.Done(success)
	return nil
}

func TestCircuitBreakerOpensAtFailureRateThreshold(t *testing.T) {
	now := time.Unix(100, 0)
	b := testBreaker(&now, CircuitBreakerConfig{WindowSize: 10, MinimumRequests: 5, FailureRateThreshold: .8, Cooldown: time.Second})
	results := []bool{false, false, false, false, true} // 80% failures
	for _, ok := range results {
		if err := attempt(t, b, ok); err != nil {
			t.Fatalf("attempt: %v", err)
		}
	}
	snap := b.Snapshot()
	if snap.State != CircuitOpen || snap.WindowRequests != 5 || snap.WindowFailures != 4 {
		t.Fatalf("snapshot=%+v", snap)
	}
	if _, err := b.Allow(); !errors.Is(err, ErrCircuitOpen) {
		t.Fatalf("Allow() err=%v want ErrCircuitOpen", err)
	}
}

func TestCircuitBreakerSlidingWindowEvictsOldFailures(t *testing.T) {
	now := time.Unix(100, 0)
	b := testBreaker(&now, CircuitBreakerConfig{WindowSize: 4, MinimumRequests: 4, FailureRateThreshold: .75})
	// Keep failure rate below threshold while proving old failures are evicted.
	for _, ok := range []bool{false, false, true, true, true, true} {
		if err := attempt(t, b, ok); err != nil {
			t.Fatalf("attempt: %v", err)
		}
	}
	snap := b.Snapshot()
	if snap.State != CircuitClosed || snap.WindowRequests != 4 || snap.WindowFailures != 0 {
		t.Fatalf("snapshot=%+v", snap)
	}
}

func TestCircuitBreakerCooldownHalfOpenAndRecovery(t *testing.T) {
	now := time.Unix(100, 0)
	b := testBreaker(&now, CircuitBreakerConfig{WindowSize: 2, MinimumRequests: 2, FailureRateThreshold: .5, Cooldown: time.Second, HalfOpenMaxRequests: 2})
	_ = attempt(t, b, false)
	_ = attempt(t, b, true) // 50% => open
	if b.Snapshot().State != CircuitOpen {
		t.Fatalf("state=%s", b.Snapshot().State)
	}

	now = now.Add(time.Second)
	p1, err := b.Allow()
	if err != nil {
		t.Fatal(err)
	}
	p2, err := b.Allow()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := b.Allow(); !errors.Is(err, ErrCircuitOpen) {
		t.Fatalf("third probe err=%v", err)
	}
	if b.Snapshot().State != CircuitHalfOpen {
		t.Fatalf("state=%s", b.Snapshot().State)
	}
	p1.Done(true)
	if b.Snapshot().State != CircuitHalfOpen {
		t.Fatalf("closed before all probes succeeded")
	}
	p2.Done(true)
	if b.Snapshot().State != CircuitClosed {
		t.Fatalf("state=%s", b.Snapshot().State)
	}
}

func TestCircuitBreakerHalfOpenFailureReopens(t *testing.T) {
	now := time.Unix(100, 0)
	b := testBreaker(&now, CircuitBreakerConfig{WindowSize: 1, MinimumRequests: 1, FailureRateThreshold: 1, Cooldown: time.Second, HalfOpenMaxRequests: 2})
	_ = attempt(t, b, false)
	now = now.Add(time.Second)
	p1, _ := b.Allow()
	p2, _ := b.Allow()
	p1.Done(false)
	if b.Snapshot().State != CircuitOpen {
		t.Fatalf("state=%s", b.Snapshot().State)
	}
	// Stale successful completion must not close a freshly reopened breaker.
	p2.Done(true)
	if b.Snapshot().State != CircuitOpen {
		t.Fatalf("stale probe changed state=%s", b.Snapshot().State)
	}
}

func TestCircuitBreakerConcurrentHalfOpenProbeLimit(t *testing.T) {
	now := time.Unix(100, 0)
	b := testBreaker(&now, CircuitBreakerConfig{WindowSize: 1, MinimumRequests: 1, FailureRateThreshold: 1, Cooldown: time.Second, HalfOpenMaxRequests: 3})
	_ = attempt(t, b, false)
	now = now.Add(time.Second)

	const workers = 100
	var wg sync.WaitGroup
	start := make(chan struct{})
	granted := make(chan *CircuitPermit, workers)
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			p, err := b.Allow()
			if err == nil {
				granted <- p
			}
		}()
	}
	close(start)
	wg.Wait()
	close(granted)
	permits := make([]*CircuitPermit, 0, workers)
	for p := range granted {
		permits = append(permits, p)
	}
	if len(permits) != 3 {
		t.Fatalf("granted=%d want=3", len(permits))
	}
	for _, p := range permits {
		p.Done(true)
	}
	if b.Snapshot().State != CircuitClosed {
		t.Fatalf("state=%s", b.Snapshot().State)
	}
}

func TestCircuitBreakerSetIsolatesEndpoints(t *testing.T) {
	set := NewCircuitBreakerSet(CircuitBreakerConfig{WindowSize: 1, MinimumRequests: 1, FailureRateThreshold: 1})
	p, err := set.Allow("a")
	if err != nil {
		t.Fatal(err)
	}
	p.Done(false)
	if _, err := set.Allow("a"); !errors.Is(err, ErrCircuitOpen) {
		t.Fatalf("a err=%v", err)
	}
	p, err = set.Allow("b")
	if err != nil {
		t.Fatalf("healthy endpoint b blocked: %v", err)
	}
	p.Done(true)
}
