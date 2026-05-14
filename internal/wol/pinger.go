package wol

import (
	"context"
	"net"
	"strconv"
	"sync"
	"time"
)

// Pinger periodically TCP-connects to each target's ping_host:ping_port
// and surfaces a per-target up/down state. We use TCP rather than ICMP
// because ICMP needs CAP_NET_RAW or unprivileged-ping group setup, which
// the distroless nonroot Docker image doesn't provide.
//
// A Pinger holds the latest known status in memory only — there's no
// disk write per tick, so this is cheap even with dozens of targets.
type Pinger struct {
	Store    *Store
	Interval time.Duration
	Timeout  time.Duration

	statuses sync.Map // map[int64]string ("up" | "down")
}

func NewPinger(s *Store, interval, timeout time.Duration) *Pinger {
	if interval < 2*time.Second {
		interval = 10 * time.Second
	}
	if timeout <= 0 {
		timeout = 1500 * time.Millisecond
	}
	return &Pinger{Store: s, Interval: interval, Timeout: timeout}
}

func (p *Pinger) Run(ctx context.Context) {
	t := time.NewTicker(p.Interval)
	defer t.Stop()
	p.tick(ctx)
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			p.tick(ctx)
		}
	}
}

func (p *Pinger) tick(ctx context.Context) {
	targets, err := p.Store.List()
	if err != nil {
		return
	}
	// Track which IDs were probed this tick so we can prune stale entries
	// for deleted targets.
	live := map[int64]struct{}{}
	for _, t := range targets {
		if t.PingHost == "" {
			p.statuses.Delete(t.ID)
			continue
		}
		live[t.ID] = struct{}{}
		port := t.PingPort
		if port <= 0 {
			port = 22
		}
		addr := net.JoinHostPort(t.PingHost, strconv.Itoa(port))
		dctx, cancel := context.WithTimeout(ctx, p.Timeout)
		d := net.Dialer{Timeout: p.Timeout}
		conn, derr := d.DialContext(dctx, "tcp", addr)
		cancel()
		if derr != nil {
			p.statuses.Store(t.ID, "down")
			continue
		}
		_ = conn.Close()
		p.statuses.Store(t.ID, "up")
	}
	// Prune statuses for targets that are gone or had ping disabled.
	p.statuses.Range(func(k, _ any) bool {
		if _, ok := live[k.(int64)]; !ok {
			p.statuses.Delete(k)
		}
		return true
	})
}

// Statuses returns a snapshot of every known target's status. IDs not
// in the map were either never probed or had ping disabled.
func (p *Pinger) Statuses() map[int64]string {
	out := map[int64]string{}
	p.statuses.Range(func(k, v any) bool {
		out[k.(int64)] = v.(string)
		return true
	})
	return out
}
