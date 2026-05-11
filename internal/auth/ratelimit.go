package auth

import (
	"sync"
	"time"
)

// Limiter is a simple in-memory sliding-window rate limiter keyed by an
// arbitrary string (typically client IP or "global").
type Limiter struct {
	mu     sync.Mutex
	hits   map[string][]time.Time
	max    int
	window time.Duration
}

// NewLimiter creates a Limiter that allows up to max events per window.
func NewLimiter(max int, window time.Duration) *Limiter {
	return &Limiter{hits: map[string][]time.Time{}, max: max, window: window}
}

// Allow records an attempt for key now and reports whether the caller is
// within the limit. Events older than window are pruned on each call.
func (l *Limiter) Allow(key string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := time.Now()
	cutoff := now.Add(-l.window)
	hits := l.hits[key]
	kept := hits[:0]
	for _, t := range hits {
		if t.After(cutoff) {
			kept = append(kept, t)
		}
	}
	if len(kept) >= l.max {
		l.hits[key] = kept
		return false
	}
	l.hits[key] = append(kept, now)
	return true
}
