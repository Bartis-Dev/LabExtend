package leader

import (
	"sync"
	"time"
)

// attemptLimiter is a fixed-window failure counter for credential endpoints.
//
// There was none at all: /api/auth/login and /api/auth/2fa/verify accepted
// unlimited guesses, and neither records a failure anywhere, so an online
// password or 6-digit-TOTP brute force was both feasible and invisible. In
// process and per leader is the right scope here — there is exactly one leader
// and its session state is local anyway.
type attemptLimiter struct {
	mu     sync.Mutex
	max    int
	window time.Duration
	seen   map[string]*attemptWindow
}

type attemptWindow struct {
	start time.Time
	n     int
}

func newAttemptLimiter(max int, window time.Duration) *attemptLimiter {
	return &attemptLimiter{max: max, window: window, seen: make(map[string]*attemptWindow)}
}

// allow reports whether another attempt may be made for key, and counts it.
func (l *attemptLimiter) allow(key string) bool {
	now := time.Now()
	l.mu.Lock()
	defer l.mu.Unlock()

	// Opportunistic sweep so a stream of one-shot keys (rotating source IPs)
	// cannot grow the map without bound.
	if len(l.seen) > 4096 {
		for k, w := range l.seen {
			if now.Sub(w.start) > l.window {
				delete(l.seen, k)
			}
		}
	}

	w, ok := l.seen[key]
	if !ok || now.Sub(w.start) > l.window {
		l.seen[key] = &attemptWindow{start: now, n: 1}
		return true
	}
	w.n++
	return w.n <= l.max
}

// reset clears the counter for key. Called after a successful authentication so
// a legitimate user is never locked out by their own earlier typos.
func (l *attemptLimiter) reset(key string) {
	l.mu.Lock()
	delete(l.seen, key)
	l.mu.Unlock()
}
