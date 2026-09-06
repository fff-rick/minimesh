package resilience

import (
	"context"
	"errors"
	"sync"
	"time"
)

var ErrRetryBudgetExhausted = errors.New("retry budget exhausted")

type RetryConfig struct {
	MaxAttempts    int
	InitialBackoff time.Duration
	MaxBackoff     time.Duration
}

func (c RetryConfig) Normalized() RetryConfig {
	if c.MaxAttempts <= 0 {
		c.MaxAttempts = 1
	}
	if c.InitialBackoff < 0 {
		c.InitialBackoff = 0
	}
	if c.MaxBackoff <= 0 || c.MaxBackoff < c.InitialBackoff {
		c.MaxBackoff = c.InitialBackoff
	}
	return c
}

func Backoff(cfg RetryConfig, retryIndex int) time.Duration {
	cfg = cfg.Normalized()
	if retryIndex <= 0 || cfg.InitialBackoff <= 0 {
		return 0
	}
	d := cfg.InitialBackoff
	for i := 1; i < retryIndex; i++ {
		if d >= cfg.MaxBackoff || d > cfg.MaxBackoff/2 {
			return cfg.MaxBackoff
		}
		d *= 2
	}
	if d > cfg.MaxBackoff {
		return cfg.MaxBackoff
	}
	return d
}

func Sleep(ctx context.Context, d time.Duration) error {
	if d <= 0 {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
			return nil
		}
	}
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

// Budget is a token bucket dedicated to retries. The first attempt never
// consumes budget; every extra attempt must acquire one token.
type Budget struct {
	mu       sync.Mutex
	rate     float64
	capacity float64
	tokens   float64
	last     time.Time
	now      func() time.Time
}

type BudgetConfig struct {
	RatePerSecond float64
	Burst         int
}

func NewBudget(cfg BudgetConfig) *Budget {
	return newBudgetWithClock(cfg, time.Now)
}

func newBudgetWithClock(cfg BudgetConfig, now func() time.Time) *Budget {
	if cfg.RatePerSecond < 0 {
		cfg.RatePerSecond = 0
	}
	if cfg.Burst < 0 {
		cfg.Burst = 0
	}
	t := now()
	return &Budget{
		rate:     cfg.RatePerSecond,
		capacity: float64(cfg.Burst),
		tokens:   float64(cfg.Burst),
		last:     t,
		now:      now,
	}
}

func (b *Budget) Allow() bool {
	if b == nil {
		return true
	}
	b.mu.Lock()
	defer b.mu.Unlock()

	now := b.now()
	elapsed := now.Sub(b.last).Seconds()
	if elapsed > 0 && b.rate > 0 {
		b.tokens += elapsed * b.rate
		if b.tokens > b.capacity {
			b.tokens = b.capacity
		}
	}
	b.last = now
	if b.tokens < 1 {
		return false
	}
	b.tokens--
	return true
}

type DoConfig struct {
	Retry     RetryConfig
	Budget    *Budget
	Retryable func(error) bool
}

// Do executes fn at most MaxAttempts times. Attempt numbers start at 1.
// The caller's context is the total retry deadline: all attempts and backoff
// share it. Retry budget is consumed only for attempts after the first.
func Do[T any](ctx context.Context, cfg DoConfig, fn func(context.Context, int) (T, error)) (T, error) {
	var zero T
	cfg.Retry = cfg.Retry.Normalized()
	if cfg.Retryable == nil {
		cfg.Retryable = func(error) bool { return false }
	}

	var lastErr error
	for attempt := 1; attempt <= cfg.Retry.MaxAttempts; attempt++ {
		if attempt > 1 {
			if cfg.Budget != nil && !cfg.Budget.Allow() {
				return zero, lastErr
			}
			if err := Sleep(ctx, Backoff(cfg.Retry, attempt-1)); err != nil {
				return zero, err
			}
		}

		value, err := fn(ctx, attempt)
		if err == nil {
			return value, nil
		}
		lastErr = err
		if attempt == cfg.Retry.MaxAttempts || !cfg.Retryable(err) || ctx.Err() != nil {
			if ctx.Err() != nil {
				return zero, ctx.Err()
			}
			return zero, err
		}
	}
	return zero, lastErr
}
