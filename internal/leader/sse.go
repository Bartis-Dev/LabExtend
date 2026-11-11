package leader

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"
)

// Event is a typed envelope for SSE messages. Topic maps to the EventSource
// `event:` field, Data is JSON-encoded into `data:`.
type Event struct {
	Topic string
	Data  any
}

// subscriber is one connected SSE client.
type subscriber struct {
	id      string
	userID  int64 // 0 == not-yet-authenticated (filtered out before sending)
	isAdmin bool
	ch      chan Event
}

// visible reports whether s may receive ev. The stream must not show anyone
// more than the REST API would: backup plans and runs are admin-only there.
func (s *subscriber) visible(ev Event) bool {
	if s.userID == 0 {
		return false
	}
	if strings.HasPrefix(ev.Topic, "backup.") {
		return s.isAdmin
	}
	return true
}

// Hub fans events out to all subscribed SSE clients. One goroutine owns the
// subscriber map to avoid locks on the hot send path.
type Hub struct {
	register   chan *subscriber
	unregister chan *subscriber
	broadcast  chan Event

	// Track for debugging/metrics only.
	mu       sync.RWMutex
	subCount int
}

// NewHub returns a ready-to-run Hub.
func NewHub() *Hub {
	return &Hub{
		register:   make(chan *subscriber, 16),
		unregister: make(chan *subscriber, 16),
		broadcast:  make(chan Event, 256),
	}
}

// Publish sends an event to all subscribers (filtered by their visibility
// rules in the send loop). Non-blocking — drops if the bus is full.
func (h *Hub) Publish(topic string, data any) {
	select {
	case h.broadcast <- Event{Topic: topic, Data: data}:
	default:
		slog.Warn("sse: broadcast bus full; dropping", "topic", topic)
	}
}

// Run drives the hub until ctx is canceled.
func (h *Hub) Run(ctx context.Context) {
	subs := make(map[*subscriber]struct{})
	heartbeat := time.NewTicker(20 * time.Second)
	defer heartbeat.Stop()

	for {
		select {
		case <-ctx.Done():
			for s := range subs {
				close(s.ch)
			}
			return

		case s := <-h.register:
			subs[s] = struct{}{}
			h.mu.Lock()
			h.subCount = len(subs)
			h.mu.Unlock()

		case s := <-h.unregister:
			if _, ok := subs[s]; ok {
				delete(subs, s)
				close(s.ch)
			}
			h.mu.Lock()
			h.subCount = len(subs)
			h.mu.Unlock()

		case ev := <-h.broadcast:
			for s := range subs {
				if !s.visible(ev) {
					continue
				}
				select {
				case s.ch <- ev:
				default:
					// Slow client; drop this event for them. 16-deep buffer.
				}
			}

		case <-heartbeat.C:
			ping := Event{Topic: "ping", Data: map[string]any{"ts": time.Now().Unix()}}
			for s := range subs {
				select {
				case s.ch <- ping:
				default:
				}
			}
		}
	}
}

// serve upgrades the connection to an SSE stream for an authenticated user.
// The role is taken once at connect; a demoted admin's open stream keeps it
// until the browser reconnects.
func (h *Hub) serve(w http.ResponseWriter, r *http.Request, userID int64, isAdmin bool) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no") // disable nginx buffering

	sub := &subscriber{
		id:      r.RemoteAddr,
		userID:  userID,
		isAdmin: isAdmin,
		ch:      make(chan Event, 16),
	}
	h.register <- sub
	defer func() { h.unregister <- sub }()

	// Send an initial "ready" event so the EventSource onopen fires reliably.
	fmt.Fprintf(w, "event: ready\ndata: {}\n\n")
	flusher.Flush()

	for {
		select {
		case <-r.Context().Done():
			return
		case ev, ok := <-sub.ch:
			if !ok {
				return
			}
			b, err := json.Marshal(ev.Data)
			if err != nil {
				continue
			}
			fmt.Fprintf(w, "event: %s\ndata: %s\n\n", ev.Topic, b)
			flusher.Flush()
		}
	}
}
