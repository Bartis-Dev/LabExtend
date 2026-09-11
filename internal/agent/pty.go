package agent

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"

	pb "github.com/Bartis-Dev/LabExtend/internal/grpc/pb"
)

// Interactive host shell.
//
// The agent runs in an unprivileged container, so it cannot hand out a root
// shell on the host directly. What it does have is the Docker socket — and
// with that it can start a privileged helper container that nsenters into
// PID 1's namespaces. From inside those namespaces the shell IS the host:
// host mounts, host network, host processes.
//
// That is the same power the agent already has (anyone holding this socket can
// start a privileged container), so this adds no new privilege — but it makes
// it reachable from a browser, which is why the HTTP side is admin-only and
// audit-logged.
//
// One helper container per session. It dies when the shell exits, when the
// browser disconnects, when the lifetime cap expires, or when the agent's
// stream to the leader drops.

const (
	defaultPtyMaxSeconds = 3600 // hard ceiling on one session's lifetime
	maxPtySessions       = 8    // per node, to bound a runaway UI
	ptyReadBuffer        = 32 * 1024
)

type ptySession struct {
	id          string
	containerID string
	conn        net.Conn
	cancel      context.CancelFunc

	writeMu sync.Mutex
	closed  bool
}

type ptyManager struct {
	docker *dockerClient
	image  string // BPM_SHELL_IMAGE override; empty = this agent's own image

	mu       sync.Mutex
	sessions map[string]*ptySession
}

func newPtyManager(docker *dockerClient, image string) *ptyManager {
	return &ptyManager{
		docker:   docker,
		image:    image,
		sessions: make(map[string]*ptySession),
	}
}

// Open starts a helper container, attaches to its TTY and streams its output
// back as PtyOutput events until it ends.
func (p *ptyManager) Open(ctx context.Context, req *pb.PtyOpenReq, emit eventEmitter) (*pb.PtyOpenResp, error) {
	if emit == nil {
		return nil, errors.New("terminal: agent is not connected to a leader")
	}
	p.mu.Lock()
	n := len(p.sessions)
	p.mu.Unlock()
	if n >= maxPtySessions {
		return nil, fmt.Errorf("terminal: %d sessions already open on this node", n)
	}

	image, err := p.resolveImage(ctx)
	if err != nil {
		return nil, err
	}

	cols, rows := clampGeometry(req.GetCols(), req.GetRows())
	sessionID := req.GetSessionId()
	if sessionID == "" {
		sessionID = uuid.NewString()
	}

	containerID, err := p.docker.CreateContainer(ctx, "labextend-shell-"+nameSuffix(sessionID), map[string]any{
		"Image": image,
		// nsenter itself runs in the helper container, so that binary must
		// exist there; everything after "--" is resolved in the HOST's mount
		// namespace, which is why we can ask for the host's own bash.
		"Cmd": []string{
			"nsenter", "--target", "1", "--mount", "--uts", "--ipc", "--net", "--pid", "--",
			"/bin/sh", "-c", hostShellCmd,
		},
		"Tty":          true,
		"OpenStdin":    true,
		"StdinOnce":    true,
		"AttachStdin":  true,
		"AttachStdout": true,
		"AttachStderr": true,
		"Env":          []string{"TERM=xterm-256color"},
		"Labels":       map[string]string{"com.labextend.session": "terminal"},
		"HostConfig": map[string]any{
			"Privileged":  true,
			"PidMode":     "host",
			"NetworkMode": "host",
			"IpcMode":     "host",
			"UTSMode":     "host",
			"AutoRemove":  true,
		},
	})
	if err != nil {
		return nil, err
	}

	// Attach BEFORE start so the shell's first prompt is not lost.
	conn, br, err := p.docker.AttachTTY(ctx, containerID)
	if err != nil {
		p.removeContainer(containerID)
		return nil, err
	}
	if err := p.docker.StartContainer(ctx, containerID); err != nil {
		_ = conn.Close()
		p.removeContainer(containerID)
		return nil, err
	}
	if err := p.docker.ResizeTTY(ctx, containerID, cols, rows); err != nil {
		slog.Debug("terminal: initial resize failed", "err", err, "session", sessionID)
	}

	sessCtx, cancel := context.WithCancel(context.Background())
	s := &ptySession{id: sessionID, containerID: containerID, conn: conn, cancel: cancel}

	p.mu.Lock()
	p.sessions[sessionID] = s
	p.mu.Unlock()

	maxSeconds := req.GetMaxSeconds()
	if maxSeconds == 0 || maxSeconds > defaultPtyMaxSeconds {
		maxSeconds = defaultPtyMaxSeconds
	}
	go p.pump(sessCtx, s, br, emit, time.Duration(maxSeconds)*time.Second)

	slog.Info("terminal: session opened",
		"session", sessionID, "container", shortID(containerID), "image", image,
		"cols", cols, "rows", rows, "max_seconds", maxSeconds)

	return &pb.PtyOpenResp{SessionId: sessionID, ContainerId: containerID, Image: image}, nil
}

// hostShellCmd runs inside the host's mount namespace: prefer the host's bash,
// fall back to whatever sh it has. `exec` on a missing binary would kill the
// shell outright, so bash is probed with command -v first.
const hostShellCmd = `cd /root 2>/dev/null || cd /; command -v bash >/dev/null 2>&1 && exec bash -l; exec sh -l`

// pump copies terminal output to the leader until the shell exits, the
// lifetime cap fires, or the session is closed.
func (p *ptyManager) pump(ctx context.Context, s *ptySession, br *bufio.Reader, emit eventEmitter, maxLifetime time.Duration) {
	deadline := time.NewTimer(maxLifetime)
	defer deadline.Stop()

	go func() {
		select {
		case <-ctx.Done():
		case <-deadline.C:
			slog.Info("terminal: session hit its lifetime cap", "session", s.id)
			p.finish(s, "session expired (1h cap)", emit)
		}
	}()

	buf := make([]byte, ptyReadBuffer)
	for {
		n, err := br.Read(buf)
		if n > 0 {
			out := make([]byte, n)
			copy(out, buf[:n])
			if sendErr := emit(&pb.AgentMessage{Kind: &pb.AgentMessage_Event{Event: &pb.Event{
				Kind: &pb.Event_PtyOutput{PtyOutput: &pb.PtyOutput{SessionId: s.id, Data: out}},
			}}}); sendErr != nil {
				p.finish(s, "leader stream closed", nil)
				return
			}
		}
		if err != nil {
			reason := "shell exited"
			if !errors.Is(err, io.EOF) && ctx.Err() == nil {
				reason = err.Error()
			}
			p.finish(s, reason, emit)
			return
		}
	}
}

// Write forwards keystrokes to the shell.
func (p *ptyManager) Write(sessionID string, data []byte) error {
	s, ok := p.get(sessionID)
	if !ok {
		return fmt.Errorf("terminal: unknown session %q", sessionID)
	}
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	if s.closed {
		return fmt.Errorf("terminal: session %q already closed", sessionID)
	}
	if _, err := s.conn.Write(data); err != nil {
		return fmt.Errorf("terminal: write: %w", err)
	}
	return nil
}

// Resize applies a new terminal geometry.
func (p *ptyManager) Resize(ctx context.Context, sessionID string, cols, rows uint32) error {
	s, ok := p.get(sessionID)
	if !ok {
		return fmt.Errorf("terminal: unknown session %q", sessionID)
	}
	c, r := clampGeometry(cols, rows)
	return p.docker.ResizeTTY(ctx, s.containerID, c, r)
}

// Close ends one session on request from the leader.
func (p *ptyManager) Close(sessionID string) error {
	s, ok := p.get(sessionID)
	if !ok {
		return nil // already gone — closing twice is not an error
	}
	p.finish(s, "closed by user", nil)
	return nil
}

// CloseAll tears every session down. Called when the agent loses its stream to
// the leader: nobody can reach those shells any more, and leaving privileged
// root containers running on the host is not an acceptable failure mode.
func (p *ptyManager) CloseAll() {
	p.mu.Lock()
	all := make([]*ptySession, 0, len(p.sessions))
	for _, s := range p.sessions {
		all = append(all, s)
	}
	p.mu.Unlock()
	for _, s := range all {
		p.finish(s, "agent disconnected from leader", nil)
	}
}

// finish is the single teardown path: idempotent, removes the container, and
// emits the terminating event if the leader can still hear us.
func (p *ptyManager) finish(s *ptySession, reason string, emit eventEmitter) {
	s.writeMu.Lock()
	if s.closed {
		s.writeMu.Unlock()
		return
	}
	s.closed = true
	s.writeMu.Unlock()

	p.mu.Lock()
	delete(p.sessions, s.id)
	p.mu.Unlock()

	s.cancel()
	_ = s.conn.Close()
	p.removeContainer(s.containerID)

	slog.Info("terminal: session closed", "session", s.id, "reason", reason)
	if emit != nil {
		_ = emit(&pb.AgentMessage{Kind: &pb.AgentMessage_Event{Event: &pb.Event{
			Kind: &pb.Event_PtyOutput{PtyOutput: &pb.PtyOutput{
				SessionId: s.id, Closed: true, Reason: reason,
			}},
		}}})
	}
}

func (p *ptyManager) get(sessionID string) (*ptySession, bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	s, ok := p.sessions[sessionID]
	return s, ok
}

// removeContainer runs on its own context: teardown often happens because the
// session context was just canceled.
func (p *ptyManager) removeContainer(id string) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if err := p.docker.RemoveContainer(ctx, id); err != nil {
		slog.Warn("terminal: removing helper container failed", "container", shortID(id), "err", err)
	}
}

// resolveImage picks the helper image. Defaulting to the agent's own image
// means the shell never waits on a registry pull — that image is by definition
// already on this node — and it is the one image we know ships nsenter.
func (p *ptyManager) resolveImage(ctx context.Context) (string, error) {
	if p.image != "" {
		return p.image, nil
	}
	id, err := p.docker.SelfImageID(ctx)
	if err != nil {
		return "", fmt.Errorf("terminal: cannot determine helper image, set BPM_SHELL_IMAGE: %w", err)
	}
	return id, nil
}

// clampGeometry keeps a hostile or buggy client from asking the kernel for an
// absurd window size.
func clampGeometry(cols, rows uint32) (uint32, uint32) {
	if cols < 2 || cols > 1000 {
		cols = 80
	}
	if rows < 2 || rows > 1000 {
		rows = 24
	}
	return cols, rows
}

// nameSuffix turns a leader-supplied session id into something Docker will
// accept as part of a container name ([a-zA-Z0-9][a-zA-Z0-9_.-]*). The name is
// only there so `docker ps` shows which shell is which, so filtering beats
// validating.
func nameSuffix(sessionID string) string {
	var b strings.Builder
	for i := 0; i < len(sessionID) && b.Len() < 12; i++ {
		if c := sessionID[i]; (c >= 'a' && c <= 'z') || (c >= '0' && c <= '9') {
			b.WriteByte(c)
		}
	}
	if b.Len() == 0 {
		return "x"
	}
	return b.String()
}

func shortID(id string) string {
	if len(id) > 12 {
		return id[:12]
	}
	return id
}
