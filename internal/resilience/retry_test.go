package resilience

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestBackoffExponentialAndCapped(t *testing.T) {
	cfg := RetryConfig{MaxAttempts: 5, InitialBackoff: 10 * time.Millisecond, MaxBackoff: 25 * time.Millisecond}
	wants := []time.Duration{0, 10 * time.Millisecond, 20 * time.Millisecond, 25 * time.Millisecond, 25 * time.Millisecond}
	for i, want := range wants {
		if got := Backoff(cfg, i); got != want {
			t.Fatalf("Backoff(%d)=%s want=%s", i, got, want)
		}
	}
}

func TestSleepStopsOnContextDeadline(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	started := time.Now()
	err := Sleep(ctx, time.Second)
	if err != context.DeadlineExceeded {
		t.Fatalf("Sleep() error=%v want DeadlineExceeded", err)
	}
	if elapsed := time.Since(started); elapsed > 200*time.Millisecond {
		t.Fatalf("Sleep ignored context deadline: %s", elapsed)
	}
}

func TestBudgetBurstAndRefill(t *testing.T) {
	now := time.Unix(100, 0)
	budget := newBudgetWithClock(BudgetConfig{RatePerSecond: 2, Burst: 2}, func() time.Time { return now })
	if !budget.Allow() || !budget.Allow() {
		t.Fatal("initial burst tokens should be available")
	}
	if budget.Allow() {
		t.Fatal("third immediate retry should be rejected")
	}
	now = now.Add(500 * time.Millisecond)
	if !budget.Allow() {
		t.Fatal("one token should refill after 500ms at 2 tokens/s")
	}
	if budget.Allow() {
		t.Fatal("only one token should have refilled")
	}
}

func TestDisabledBudgetRejectsRetries(t *testing.T) {
	budget := newBudgetWithClock(BudgetConfig{RatePerSecond: 0, Burst: 0}, time.Now)
	if budget.Allow() {
		t.Fatal("zero burst/rate budget should reject retries")
	}
}

func TestDoRetriesTransientFailureThenSucceeds(t *testing.T) {
	attempts := 0
	value, err := Do(context.Background(), DoConfig{
		Retry:     RetryConfig{MaxAttempts: 3},
		Retryable: func(err error) bool { return err != nil },
	}, func(context.Context, int) (string, error) {
		attempts++
		if attempts < 3 {
			return "", errors.New("temporary")
		}
		return "ok", nil
	})
	if err != nil || value != "ok" || attempts != 3 {
		t.Fatalf("value=%q err=%v attempts=%d", value, err, attempts)
	}
}

func TestDoDoesNotRetryNonRetryableFailure(t *testing.T) {
	attempts := 0
	want := errors.New("permanent")
	_, err := Do(context.Background(), DoConfig{
		Retry:     RetryConfig{MaxAttempts: 5},
		Retryable: func(error) bool { return false },
	}, func(context.Context, int) (struct{}, error) {
		attempts++
		return struct{}{}, want
	})
	if err != want || attempts != 1 {
		t.Fatalf("err=%v attempts=%d want err=%v attempts=1", err, attempts, want)
	}
}

func TestDoStopsWhenBudgetIsExhausted(t *testing.T) {
	now := time.Unix(100, 0)
	budget := newBudgetWithClock(BudgetConfig{RatePerSecond: 0, Burst: 1}, func() time.Time { return now })
	attempts := 0
	_, err := Do(context.Background(), DoConfig{
		Retry:     RetryConfig{MaxAttempts: 5},
		Budget:    budget,
		Retryable: func(error) bool { return true },
	}, func(context.Context, int) (struct{}, error) {
		attempts++
		return struct{}{}, errors.New("temporary")
	})
	if err == nil || attempts != 2 {
		t.Fatalf("err=%v attempts=%d want non-nil err and 2 attempts", err, attempts)
	}
}

func TestDoTotalDeadlineIncludesBackoff(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Millisecond)
	defer cancel()
	attempts := 0
	_, err := Do(ctx, DoConfig{
		Retry:     RetryConfig{MaxAttempts: 5, InitialBackoff: 100 * time.Millisecond, MaxBackoff: 100 * time.Millisecond},
		Retryable: func(error) bool { return true },
	}, func(context.Context, int) (struct{}, error) {
		attempts++
		return struct{}{}, errors.New("temporary")
	})
	if err != context.DeadlineExceeded {
		t.Fatalf("err=%v want DeadlineExceeded", err)
	}
	if attempts != 1 {
		t.Fatalf("attempts=%d want 1; deadline should expire during backoff", attempts)
	}
}

func TestBudgetConcurrentBurstDoesNotOversubscribe(t *testing.T) {
	budget := newBudgetWithClock(BudgetConfig{RatePerSecond: 0, Burst: 50}, time.Now)
	const workers = 1000
	var wg sync.WaitGroup
	var allowed atomic.Int64
	wg.Add(workers)
	for i := 0; i < workers; i++ {
		go func() {
			defer wg.Done()
			if budget.Allow() {
				allowed.Add(1)
			}
		}()
	}
	wg.Wait()
	if got := allowed.Load(); got != 50 {
		t.Fatalf("allowed=%d want=50", got)
	}
}
