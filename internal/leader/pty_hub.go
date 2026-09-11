package leader

import (
	"sync"

	pb "github.com/Bartis-Dev/LabExtend/internal/grpc/pb"
)

// ptyHub routes terminal output from the agents' shared gRPC channel to the
// WebSocket handler that owns each session.
//
// Unlike log lines, terminal bytes cannot be dropped: a hole in the stream
// leaves the renderer in a state nobody can reconstruct — half an escape
// sequence, a prompt that never arrives. So a subscriber that cannot keep up
// is killed rather than fed a corrupted stream.
type ptyHub struct {
	mu   sync.Mutex
	subs map[string]*ptySub
}

type ptySub struct {
	out  chan *pb.PtyOutput
	dead chan struct{} // closed when output had to be dropped
	once sync.Once
}

func newPtyHub() *ptyHub {
	return &ptyHub{subs: make(map[string]*ptySub)}
}

// subscribe registers a session. The returned func must be called when the
// handler is done with it.
func (h *ptyHub) subscribe(sessionID string) (*ptySub, func()) {
	s := &ptySub{
		// Deep enough to absorb a burst from `find /` without stalling the
		// agent channel; past that the client is simply too slow.
		out:  make(chan *pb.PtyOutput, 1024),
		dead: make(chan struct{}),
	}
	h.mu.Lock()
	h.subs[sessionID] = s
	h.mu.Unlock()

	return s, func() {
		h.mu.Lock()
		if cur, ok := h.subs[sessionID]; ok && cur == s {
			delete(h.subs, sessionID)
		}
		h.mu.Unlock()
		// out is deliberately never closed: deliver may be mid-send on the
		// gRPC receive goroutine, and closing under it would panic that
		// goroutine and take every agent connection down with it.
	}
}

// deliver hands one output message to its session, if anyone is still there.
// Called from the agent's gRPC receive loop, so it must never block.
func (h *ptyHub) deliver(out *pb.PtyOutput) {
	h.mu.Lock()
	s, ok := h.subs[out.GetSessionId()]
	h.mu.Unlock()
	if !ok {
		return // session already torn down; the agent will notice on close
	}
	select {
	case s.out <- out:
	default:
		s.once.Do(func() { close(s.dead) })
	}
}
