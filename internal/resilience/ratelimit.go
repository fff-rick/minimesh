package resilience

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// RateLimitRule configures a local token bucket. RatePerSecond <= 0 disables the rule.
type RateLimitRule struct {
	RatePerSecond float64
	Burst         int
}

type RateLimitStats struct {
	Allowed  uint64
	Rejected uint64
}

type tokenBucket struct {
	mu       sync.Mutex
	rule     RateLimitRule
	tokens   float64
	last     time.Time
	now      func() time.Time
	allowed  atomic.Uint64
	rejected atomic.Uint64
}

func newTokenBucket(rule RateLimitRule, now func() time.Time) (*tokenBucket, error) {
	if rule.RatePerSecond <= 0 {
		return nil, fmt.Errorf("rate must be > 0")
	}
	if rule.Burst <= 0 {
		return nil, fmt.Errorf("burst must be > 0")
	}
	if now == nil {
		now = time.Now
	}
	t := now()
	return &tokenBucket{rule: rule, tokens: float64(rule.Burst), last: t, now: now}, nil
}

func (b *tokenBucket) allow() bool {
	b.mu.Lock()
	now := b.now()
	elapsed := now.Sub(b.last).Seconds()
	if elapsed > 0 {
		b.tokens += elapsed * b.rule.RatePerSecond
		if max := float64(b.rule.Burst); b.tokens > max {
			b.tokens = max
		}
		b.last = now
	}
	ok := b.tokens >= 1
	if ok {
		b.tokens--
	}
	b.mu.Unlock()

	if ok {
		b.allowed.Add(1)
	} else {
		b.rejected.Add(1)
	}
	return ok
}

func (b *tokenBucket) stats() RateLimitStats {
	return RateLimitStats{Allowed: b.allowed.Load(), Rejected: b.rejected.Load()}
}

// LocalRateLimiter applies route rules before service rules, then an optional default rule.
type LocalRateLimiter struct {
	defaultBucket *tokenBucket
	service       map[string]*tokenBucket
	route         map[string]*tokenBucket
}

func NewLocalRateLimiter(defaultRule *RateLimitRule, serviceRules map[string]RateLimitRule, routeRules map[string]RateLimitRule) (*LocalRateLimiter, error) {
	return newLocalRateLimiter(defaultRule, serviceRules, routeRules, time.Now)
}

func newLocalRateLimiter(defaultRule *RateLimitRule, serviceRules map[string]RateLimitRule, routeRules map[string]RateLimitRule, now func() time.Time) (*LocalRateLimiter, error) {
	l := &LocalRateLimiter{service: make(map[string]*tokenBucket), route: make(map[string]*tokenBucket)}
	var err error
	if defaultRule != nil {
		l.defaultBucket, err = newTokenBucket(*defaultRule, now)
		if err != nil {
			return nil, fmt.Errorf("default rate limit: %w", err)
		}
	}
	for key, rule := range serviceRules {
		if key == "" {
			return nil, fmt.Errorf("service rate limit: empty service")
		}
		bucket, e := newTokenBucket(rule, now)
		if e != nil {
			return nil, fmt.Errorf("service rate limit %q: %w", key, e)
		}
		l.service[key] = bucket
	}
	for key, rule := range routeRules {
		if key == "" {
			return nil, fmt.Errorf("route rate limit: empty route")
		}
		bucket, e := newTokenBucket(rule, now)
		if e != nil {
			return nil, fmt.Errorf("route rate limit %q: %w", key, e)
		}
		l.route[key] = bucket
	}
	return l, nil
}

func RouteKey(service, fullMethod string) string { return service + "|" + fullMethod }

func (l *LocalRateLimiter) Allow(service, fullMethod string) bool {
	if l == nil {
		return true
	}
	if bucket := l.route[RouteKey(service, fullMethod)]; bucket != nil {
		return bucket.allow()
	}
	if bucket := l.service[service]; bucket != nil {
		return bucket.allow()
	}
	if l.defaultBucket != nil {
		return l.defaultBucket.allow()
	}
	return true
}

func (l *LocalRateLimiter) Stats() map[string]RateLimitStats {
	result := make(map[string]RateLimitStats)
	if l == nil {
		return result
	}
	if l.defaultBucket != nil {
		result["default"] = l.defaultBucket.stats()
	}
	for key, bucket := range l.service {
		result["service:"+key] = bucket.stats()
	}
	for key, bucket := range l.route {
		result["route:"+key] = bucket.stats()
	}
	return result
}

// ParseServiceRateRules parses "service=rate:burst;service2=rate:burst".
func ParseServiceRateRules(text string) (map[string]RateLimitRule, error) {
	return parseNamedRateRules(text, false)
}

// ParseRouteRateRules parses "service|/package.Service/Method=rate:burst;...".
func ParseRouteRateRules(text string) (map[string]RateLimitRule, error) {
	return parseNamedRateRules(text, true)
}

func parseNamedRateRules(text string, route bool) (map[string]RateLimitRule, error) {
	result := make(map[string]RateLimitRule)
	text = strings.TrimSpace(text)
	if text == "" {
		return result, nil
	}
	for _, raw := range strings.Split(text, ";") {
		raw = strings.TrimSpace(raw)
		if raw == "" {
			continue
		}
		parts := strings.SplitN(raw, "=", 2)
		if len(parts) != 2 || strings.TrimSpace(parts[0]) == "" {
			return nil, fmt.Errorf("invalid rate limit rule %q", raw)
		}
		key := strings.TrimSpace(parts[0])
		if route && !strings.Contains(key, "|") {
			return nil, fmt.Errorf("route rule %q must use service|/method", raw)
		}
		rb := strings.SplitN(strings.TrimSpace(parts[1]), ":", 2)
		if len(rb) != 2 {
			return nil, fmt.Errorf("rate limit rule %q must use rate:burst", raw)
		}
		rate, err := strconv.ParseFloat(strings.TrimSpace(rb[0]), 64)
		if err != nil || rate <= 0 {
			return nil, fmt.Errorf("invalid rate in rule %q", raw)
		}
		burst, err := strconv.Atoi(strings.TrimSpace(rb[1]))
		if err != nil || burst <= 0 {
			return nil, fmt.Errorf("invalid burst in rule %q", raw)
		}
		result[key] = RateLimitRule{RatePerSecond: rate, Burst: burst}
	}
	return result, nil
}

// PrometheusText returns the Stage 7 rate-limit metrics in Prometheus text exposition format.
// Stage 10 will replace this narrow endpoint with the full observability stack.
func (l *LocalRateLimiter) PrometheusText() string {
	stats := l.Stats()
	keys := make([]string, 0, len(stats))
	for key := range stats {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	var b strings.Builder
	b.WriteString("# HELP minimesh_rate_limit_allowed_total Requests admitted by the local Sidecar rate limiter.\n")
	b.WriteString("# TYPE minimesh_rate_limit_allowed_total counter\n")
	for _, key := range keys {
		fmt.Fprintf(&b, "minimesh_rate_limit_allowed_total{rule=%q} %d\n", escapePromLabel(key), stats[key].Allowed)
	}
	b.WriteString("# HELP minimesh_rate_limit_rejected_total Requests rejected by the local Sidecar rate limiter.\n")
	b.WriteString("# TYPE minimesh_rate_limit_rejected_total counter\n")
	for _, key := range keys {
		fmt.Fprintf(&b, "minimesh_rate_limit_rejected_total{rule=%q} %d\n", escapePromLabel(key), stats[key].Rejected)
	}
	return b.String()
}

func escapePromLabel(v string) string {
	v = strings.ReplaceAll(v, "\\", "\\\\")
	v = strings.ReplaceAll(v, "\n", "\\n")
	return strings.ReplaceAll(v, "\"", "\\\"")
}
