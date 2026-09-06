package connectionpool

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"
)

var ErrClosed = errors.New("connectionpool: pool is closed")

type Conn interface{ Close() error }
type DialFunc func(context.Context, string) (Conn, error)

type Config struct {
	MaxConnectionsPerTarget int
	MaxIdleTime             time.Duration
	ReapInterval            time.Duration
}

type Stats struct {
	ActiveConnections int64
	IdleConnections   int64
	ConnectionCreated int64
	Targets           int
}

type entry struct {
	conn     Conn
	active   int
	lastUsed time.Time
	closing  bool
}

type targetPool struct {
	entries  []*entry
	creating int
}

type Pool struct {
	mu      sync.Mutex
	cfg     Config
	dial    DialFunc
	targets map[string]*targetPool
	created int64
	closed  bool
	stop    chan struct{}
	done    chan struct{}
	cond    *sync.Cond
}

func New(cfg Config, dial DialFunc) (*Pool, error) {
	if dial == nil {
		return nil, fmt.Errorf("connectionpool: nil dialer")
	}
	if cfg.MaxConnectionsPerTarget <= 0 {
		cfg.MaxConnectionsPerTarget = 1
	}
	if cfg.MaxIdleTime <= 0 {
		cfg.MaxIdleTime = 30 * time.Second
	}
	if cfg.ReapInterval <= 0 {
		cfg.ReapInterval = minDuration(cfg.MaxIdleTime/2, 5*time.Second)
	}
	if cfg.ReapInterval <= 0 {
		cfg.ReapInterval = time.Second
	}
	p := &Pool{cfg: cfg, dial: dial, targets: make(map[string]*targetPool), stop: make(chan struct{}), done: make(chan struct{})}
	p.cond = sync.NewCond(&p.mu)
	go p.reaper()
	return p, nil
}

func (p *Pool) Acquire(ctx context.Context, target string) (Conn, func(), error) {
	if target == "" {
		return nil, nil, fmt.Errorf("connectionpool: empty target")
	}

	for {
		p.mu.Lock()
		if p.closed {
			p.mu.Unlock()
			return nil, nil, ErrClosed
		}
		tp := p.targets[target]
		if tp == nil {
			tp = &targetPool{}
			p.targets[target] = tp
		}

		if e := leastActive(tp.entries, true); e != nil {
			e.active++
			p.mu.Unlock()
			return e.conn, p.releaseFunc(e), nil
		}

		// Count in-flight dials toward the target cap. This prevents a burst
		// of callers from creating many temporary connections beyond Max.
		if len(tp.entries)+tp.creating < p.cfg.MaxConnectionsPerTarget {
			tp.creating++
			p.mu.Unlock()

			conn, err := p.dial(ctx, target)

			p.mu.Lock()
			current := p.targets[target]
			if current == nil {
				current = tp
				p.targets[target] = current
			}
			if current.creating > 0 {
				current.creating--
			}
			if p.closed {
				p.cond.Broadcast()
				p.mu.Unlock()
				if conn != nil {
					_ = conn.Close()
				}
				return nil, nil, ErrClosed
			}
			if err != nil {
				p.cond.Broadcast()
				p.mu.Unlock()
				return nil, nil, err
			}
			e := &entry{conn: conn, active: 1, lastUsed: time.Now()}
			current.entries = append(current.entries, e)
			p.created++
			p.cond.Broadcast()
			p.mu.Unlock()
			return conn, p.releaseFunc(e), nil
		}

		if e := leastActive(tp.entries, false); e != nil {
			e.active++
			p.mu.Unlock()
			return e.conn, p.releaseFunc(e), nil
		}

		// The target is at capacity solely because all slots are currently
		// dialing. Wait until at least one dial finishes.
		if err := ctx.Err(); err != nil {
			p.mu.Unlock()
			return nil, nil, err
		}
		p.cond.Wait()
		p.mu.Unlock()
	}
}

func (p *Pool) releaseFunc(e *entry) func() {
	var once sync.Once
	return func() {
		once.Do(func() {
			p.mu.Lock()
			defer p.mu.Unlock()
			if e.active > 0 {
				e.active--
			}
			if e.active == 0 {
				e.lastUsed = time.Now()
			}
		})
	}
}

func leastActive(entries []*entry, idleOnly bool) *entry {
	var best *entry
	for _, e := range entries {
		if e == nil || e.closing {
			continue
		}
		if idleOnly && e.active != 0 {
			continue
		}
		if best == nil || e.active < best.active {
			best = e
		}
	}
	return best
}

func (p *Pool) Stats() Stats {
	p.mu.Lock()
	defer p.mu.Unlock()
	s := Stats{ConnectionCreated: p.created, Targets: len(p.targets)}
	for _, tp := range p.targets {
		for _, e := range tp.entries {
			if e.closing {
				continue
			}
			if e.active > 0 {
				s.ActiveConnections++
			} else {
				s.IdleConnections++
			}
		}
	}
	return s
}

func (p *Pool) ReapIdle(now time.Time) int {
	p.mu.Lock()
	var closing []Conn
	reaped := 0
	for target, tp := range p.targets {
		kept := tp.entries[:0]
		for _, e := range tp.entries {
			if e.active == 0 && !e.closing && now.Sub(e.lastUsed) >= p.cfg.MaxIdleTime {
				e.closing = true
				closing = append(closing, e.conn)
				reaped++
				continue
			}
			kept = append(kept, e)
		}
		tp.entries = kept
		if len(tp.entries) == 0 && tp.creating == 0 {
			delete(p.targets, target)
		}
	}
	p.mu.Unlock()
	for _, c := range closing {
		_ = c.Close()
	}
	return reaped
}

func (p *Pool) Close() error {
	p.mu.Lock()
	if p.closed {
		p.mu.Unlock()
		<-p.done
		return nil
	}
	p.closed = true
	close(p.stop)
	p.cond.Broadcast()
	var conns []Conn
	for _, tp := range p.targets {
		for _, e := range tp.entries {
			if !e.closing {
				e.closing = true
				conns = append(conns, e.conn)
			}
		}
	}
	p.targets = make(map[string]*targetPool)
	p.mu.Unlock()
	<-p.done
	var first error
	for _, c := range conns {
		if err := c.Close(); err != nil && first == nil {
			first = err
		}
	}
	return first
}

func (p *Pool) reaper() {
	ticker := time.NewTicker(p.cfg.ReapInterval)
	defer func() { ticker.Stop(); close(p.done) }()
	for {
		select {
		case now := <-ticker.C:
			p.ReapIdle(now)
		case <-p.stop:
			return
		}
	}
}

func minDuration(a, b time.Duration) time.Duration {
	if a < b {
		return a
	}
	return b
}
