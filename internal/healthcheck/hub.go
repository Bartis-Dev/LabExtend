package healthcheck

import (
	"sync"
)

// Hub fans out StatusMap updates to subscribers. Producers call Publish on
// each new snapshot; consumers receive the same value via Subscribe.
type Hub struct {
	mu       sync.RWMutex
	last     StatusMap
	subs     map[chan StatusMap]struct{}
	bufSize  int
}

func NewHub() *Hub {
	return &Hub{
		last:    StatusMap{},
		subs:    map[chan StatusMap]struct{}{},
		bufSize: 8,
	}
}

// Snapshot returns a copy of the most recent published map.
func (h *Hub) Snapshot() StatusMap {
	h.mu.RLock()
	defer h.mu.RUnlock()
	out := make(StatusMap, len(h.last))
	for k, v := range h.last {
		out[k] = v
	}
	return out
}

// Publish stores the snapshot and broadcasts to subscribers. Subscribers
// whose channel buffer is full are skipped (drop policy) so a slow client
// never stalls probes.
func (h *Hub) Publish(s StatusMap) {
	h.mu.Lock()
	h.last = s
	subs := make([]chan StatusMap, 0, len(h.subs))
	for c := range h.subs {
		subs = append(subs, c)
	}
	h.mu.Unlock()
	for _, c := range subs {
		select {
		case c <- s:
		default:
			// Drop.
		}
	}
}

// Subscribe returns a buffered channel that receives every future Publish.
// Callers must call the returned cancel function to release resources.
func (h *Hub) Subscribe() (<-chan StatusMap, func()) {
	c := make(chan StatusMap, h.bufSize)
	h.mu.Lock()
	h.subs[c] = struct{}{}
	h.mu.Unlock()
	cancel := func() {
		h.mu.Lock()
		delete(h.subs, c)
		h.mu.Unlock()
		close(c)
	}
	return c, cancel
}
