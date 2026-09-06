package connectionpool

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type fakeConn struct{ closed atomic.Int32 }

func (f *fakeConn) Close() error { f.closed.Add(1); return nil }

type fakeDialer struct {
	calls atomic.Int64
	mu    sync.Mutex
	conns []*fakeConn
}

func (d *fakeDialer) dial(context.Context, string) (Conn, error) {
	d.calls.Add(1)
	c := &fakeConn{}
	d.mu.Lock()
	d.conns = append(d.conns, c)
	d.mu.Unlock()
	return c, nil
}

func TestReuseIdleConnection(t *testing.T) {
	d := &fakeDialer{}
	p, _ := New(Config{MaxConnectionsPerTarget: 2, MaxIdleTime: time.Minute}, d.dial)
	defer p.Close()
	c1, done, _ := p.Acquire(context.Background(), "a")
	done()
	c2, done, _ := p.Acquire(context.Background(), "a")
	done()
	if c1 != c2 || d.calls.Load() != 1 {
		t.Fatalf("connection was not reused: calls=%d", d.calls.Load())
	}
	s := p.Stats()
	if s.IdleConnections != 1 || s.ActiveConnections != 0 || s.ConnectionCreated != 1 {
		t.Fatalf("stats=%+v", s)
	}
}

func TestGrowsUntilMaxThenMultiplexes(t *testing.T) {
	d := &fakeDialer{}
	p, _ := New(Config{MaxConnectionsPerTarget: 2, MaxIdleTime: time.Minute}, d.dial)
	defer p.Close()
	_, r1, _ := p.Acquire(context.Background(), "a")
	_, r2, _ := p.Acquire(context.Background(), "a")
	_, r3, _ := p.Acquire(context.Background(), "a")
	if d.calls.Load() != 2 {
		t.Fatalf("dial calls=%d want 2", d.calls.Load())
	}
	s := p.Stats()
	if s.ActiveConnections != 2 || s.ConnectionCreated != 2 {
		t.Fatalf("stats=%+v", s)
	}
	r1()
	r2()
	r3()
}

func TestIdleReaping(t *testing.T) {
	d := &fakeDialer{}
	p, _ := New(Config{MaxConnectionsPerTarget: 1, MaxIdleTime: 20 * time.Millisecond, ReapInterval: time.Hour}, d.dial)
	defer p.Close()
	_, release, _ := p.Acquire(context.Background(), "a")
	release()
	time.Sleep(25 * time.Millisecond)
	if n := p.ReapIdle(time.Now()); n != 1 {
		t.Fatalf("reaped=%d want 1", n)
	}
	if s := p.Stats(); s.IdleConnections != 0 || s.Targets != 0 {
		t.Fatalf("stats=%+v", s)
	}
	d.mu.Lock()
	closed := d.conns[0].closed.Load()
	d.mu.Unlock()
	if closed != 1 {
		t.Fatalf("close count=%d", closed)
	}
}

func TestConcurrentAcquireRespectsMax(t *testing.T) {
	d := &fakeDialer{}
	p, _ := New(Config{MaxConnectionsPerTarget: 4, MaxIdleTime: time.Minute}, d.dial)
	defer p.Close()
	const n = 1000
	var wg sync.WaitGroup
	start := make(chan struct{})
	releases := make(chan func(), n)
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			_, r, err := p.Acquire(context.Background(), "a")
			if err != nil {
				t.Errorf("Acquire: %v", err)
				return
			}
			releases <- r
		}()
	}
	close(start)
	wg.Wait()
	close(releases)
	if got := d.calls.Load(); got < 1 || got > 4 {
		t.Fatalf("dial calls=%d, want 1..4", got)
	}
	for r := range releases {
		r()
	}
	s := p.Stats()
	if s.ActiveConnections != 0 || s.IdleConnections < 1 || s.IdleConnections > 4 {
		t.Fatalf("stats=%+v", s)
	}
}

func TestGracefulCloseAndRejectNewAcquire(t *testing.T) {
	d := &fakeDialer{}
	p, _ := New(Config{MaxConnectionsPerTarget: 1, MaxIdleTime: time.Minute}, d.dial)
	_, release, _ := p.Acquire(context.Background(), "a")
	release()
	if err := p.Close(); err != nil {
		t.Fatal(err)
	}
	if _, _, err := p.Acquire(context.Background(), "a"); err != ErrClosed {
		t.Fatalf("err=%v want ErrClosed", err)
	}
}
