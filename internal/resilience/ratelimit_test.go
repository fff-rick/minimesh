package resilience

import (
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type fakeRateClock struct {
	mu sync.Mutex
	t  time.Time
}

func (c *fakeRateClock) Now() time.Time      { c.mu.Lock(); defer c.mu.Unlock(); return c.t }
func (c *fakeRateClock) Add(d time.Duration) { c.mu.Lock(); c.t = c.t.Add(d); c.mu.Unlock() }

func TestRateLimitBurstAndRefill(t *testing.T) {
	clock := &fakeRateClock{t: time.Unix(0, 0)}
	rule := RateLimitRule{RatePerSecond: 1000, Burst: 1000}
	limiter, err := newLocalRateLimiter(&rule, nil, nil, clock.Now)
	if err != nil {
		t.Fatal(err)
	}

	allowed := 0
	for i := 0; i < 5000; i++ {
		if limiter.Allow("", "/test") {
			allowed++
		}
	}
	if allowed != 1000 {
		t.Fatalf("first burst allowed=%d want=1000", allowed)
	}
	stats := limiter.Stats()["default"]
	if stats.Allowed != 1000 || stats.Rejected != 4000 {
		t.Fatalf("stats=%+v", stats)
	}

	clock.Add(time.Second)
	allowed = 0
	for i := 0; i < 1200; i++ {
		if limiter.Allow("", "/test") {
			allowed++
		}
	}
	if allowed != 1000 {
		t.Fatalf("after refill allowed=%d want=1000", allowed)
	}
}

func TestRateLimitRulePrecedence(t *testing.T) {
	clock := &fakeRateClock{t: time.Unix(0, 0)}
	def := RateLimitRule{RatePerSecond: 10, Burst: 1}
	limiter, err := newLocalRateLimiter(&def,
		map[string]RateLimitRule{"echo": {RatePerSecond: 10, Burst: 2}},
		map[string]RateLimitRule{RouteKey("echo", "/Echo/Fast"): {RatePerSecond: 10, Burst: 3}},
		clock.Now,
	)
	if err != nil {
		t.Fatal(err)
	}

	for i := 0; i < 3; i++ {
		if !limiter.Allow("echo", "/Echo/Fast") {
			t.Fatalf("route request %d unexpectedly rejected", i)
		}
	}
	if limiter.Allow("echo", "/Echo/Fast") {
		t.Fatal("route burst should be exhausted")
	}

	for i := 0; i < 2; i++ {
		if !limiter.Allow("echo", "/Echo/Other") {
			t.Fatalf("service request %d unexpectedly rejected", i)
		}
	}
	if limiter.Allow("echo", "/Echo/Other") {
		t.Fatal("service burst should be exhausted")
	}

	if !limiter.Allow("other", "/Other/Call") {
		t.Fatal("default first request rejected")
	}
	if limiter.Allow("other", "/Other/Call") {
		t.Fatal("default burst should be exhausted")
	}
}

func TestRateLimitConcurrentBurstDoesNotOvershoot(t *testing.T) {
	clock := &fakeRateClock{t: time.Unix(0, 0)}
	rule := RateLimitRule{RatePerSecond: 1000, Burst: 100}
	limiter, err := newLocalRateLimiter(&rule, nil, nil, clock.Now)
	if err != nil {
		t.Fatal(err)
	}

	var allowed atomic.Int64
	var wg sync.WaitGroup
	for i := 0; i < 1000; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if limiter.Allow("", "/test") {
				allowed.Add(1)
			}
		}()
	}
	wg.Wait()
	if got := allowed.Load(); got != 100 {
		t.Fatalf("allowed=%d want=100", got)
	}
}

func TestRateLimitValidation(t *testing.T) {
	bad := []RateLimitRule{{RatePerSecond: 0, Burst: 1}, {RatePerSecond: 1, Burst: 0}}
	for _, rule := range bad {
		if _, err := NewLocalRateLimiter(&rule, nil, nil); err == nil {
			t.Fatalf("rule %+v should fail", rule)
		}
	}
}

func TestParseRateLimitRules(t *testing.T) {
	services, err := ParseServiceRateRules("echo=1000:2000;payment=500:500")
	if err != nil {
		t.Fatal(err)
	}
	if services["echo"].RatePerSecond != 1000 || services["echo"].Burst != 2000 {
		t.Fatalf("echo=%+v", services["echo"])
	}
	routes, err := ParseRouteRateRules("echo|/minimesh.v1.EchoService/Echo=100:200")
	if err != nil {
		t.Fatal(err)
	}
	if routes[RouteKey("echo", "/minimesh.v1.EchoService/Echo")].Burst != 200 {
		t.Fatalf("routes=%+v", routes)
	}
	if _, err := ParseRouteRateRules("bad=100:100"); err == nil {
		t.Fatal("expected invalid route rule")
	}
}

func TestRateLimitPrometheusText(t *testing.T) {
	rule := RateLimitRule{RatePerSecond: 10, Burst: 1}
	limiter, err := NewLocalRateLimiter(nil, map[string]RateLimitRule{"echo": rule}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !limiter.Allow("echo", "/Echo") {
		t.Fatal("first request rejected")
	}
	if limiter.Allow("echo", "/Echo") {
		t.Fatal("second request should be rejected")
	}
	text := limiter.PrometheusText()
	if !strings.Contains(text, `minimesh_rate_limit_allowed_total{rule="service:echo"} 1`) {
		t.Fatalf("metrics=%s", text)
	}
	if !strings.Contains(text, `minimesh_rate_limit_rejected_total{rule="service:echo"} 1`) {
		t.Fatalf("metrics=%s", text)
	}
}
