package agent

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"io"
	"log/slog"
	"sync"
	"time"

	pb "github.com/Bartis-Dev/LabExtend/internal/grpc/pb"
)

// logCollector keeps one goroutine per running container reading
// `docker logs -f` and shipping batched LogBatch events to the leader.
//
// Tail starts from time.Now() (so reconnects don't replay) unless this is
// the very first time we see the container, in which case we backfill the
// last N lines via the agent-side ringbuffer.
//
// Per-container rate cap: we drop lines beyond maxLinesPerSecond to keep a
// runaway-logger container from saturating the gRPC channel.
type logCollector struct {
	docker             *dockerClient
	flushInterval      time.Duration
	maxLinesPerBatch   int
	maxLinesPerSecond  int
	maxLineBytes       int
	tailFromBeginning  bool

	mu       sync.Mutex
	streams  map[string]*containerLogStream
	disabled map[string]bool
}

type containerLogStream struct {
	containerID string
	cancel      context.CancelFunc

	bufMu sync.Mutex
	buf   []*pb.LogLine

	rateMu      sync.Mutex
	rateWindow  []time.Time
}

func newLogCollector(d *dockerClient) *logCollector {
	return &logCollector{
		docker:            d,
		flushInterval:     1500 * time.Millisecond,
		maxLinesPerBatch:  500,
		maxLinesPerSecond: 200,
		maxLineBytes:      4 * 1024,
		streams:           make(map[string]*containerLogStream),
		disabled:          make(map[string]bool),
	}
}

// Run drives the flusher: every flushInterval, gathers buffered lines from
// every container stream and emits one LogBatch event per stream.
// container streams are started/stopped via Sync().
func (lc *logCollector) Run(ctx context.Context, emit func(*pb.AgentMessage) error) {
	t := time.NewTicker(lc.flushInterval)
	defer t.Stop()

	for {
		select {
		case <-ctx.Done():
			lc.stopAll()
			return
		case <-t.C:
			lc.flush(emit)
		}
	}
}

// Sync reconciles the set of active streams with the set of currently-running
// containers. Called by the container collector after each list.
func (lc *logCollector) Sync(ctx context.Context, current map[string]containerSummary) {
	lc.mu.Lock()
	defer lc.mu.Unlock()

	// Stop streams whose container disappeared or stopped.
	for id, s := range lc.streams {
		c, ok := current[id]
		if !ok || c.State != "running" {
			s.cancel()
			delete(lc.streams, id)
		}
	}

	// Start streams for running containers we don't have yet.
	for id, c := range current {
		if c.State != "running" {
			continue
		}
		if lc.disabled[id] {
			continue
		}
		if _, ok := lc.streams[id]; ok {
			continue
		}
		streamCtx, cancel := context.WithCancel(ctx)
		s := &containerLogStream{containerID: id, cancel: cancel}
		lc.streams[id] = s
		go lc.tail(streamCtx, s)
	}
}

// SetDisabled toggles whether logs for one container should be shipped. The
// leader's EnableLogs/DisableLogs commands call this.
func (lc *logCollector) SetDisabled(containerID string, disabled bool) {
	lc.mu.Lock()
	defer lc.mu.Unlock()
	lc.disabled[containerID] = disabled
	if disabled {
		if s, ok := lc.streams[containerID]; ok {
			s.cancel()
			delete(lc.streams, containerID)
		}
	}
}

// stopAll cancels every running tail goroutine (called on Run exit).
func (lc *logCollector) stopAll() {
	lc.mu.Lock()
	defer lc.mu.Unlock()
	for _, s := range lc.streams {
		s.cancel()
	}
	lc.streams = make(map[string]*containerLogStream)
}

// tail reads the log stream until ctx is canceled or the stream errors.
// On error, sleeps a bit and reconnects.
func (lc *logCollector) tail(ctx context.Context, s *containerLogStream) {
	backoff := 500 * time.Millisecond
	max := 30 * time.Second

	since := time.Time{}
	if !lc.tailFromBeginning {
		since = time.Now()
	}

	for {
		if ctx.Err() != nil {
			return
		}
		rc, err := lc.docker.Logs(ctx, s.containerID, since)
		if err != nil {
			slog.Debug("log tail open failed", "container", s.containerID, "err", err)
			select {
			case <-ctx.Done():
				return
			case <-time.After(backoff):
			}
			backoff *= 2
			if backoff > max {
				backoff = max
			}
			continue
		}
		backoff = 500 * time.Millisecond
		// Detect TTY via Inspect on first connect; for now assume non-TTY
		// (the demuxer copes with malformed headers by erroring out).
		err = lc.consume(rc, s)
		rc.Close()
		if errors.Is(err, context.Canceled) || ctx.Err() != nil {
			return
		}
		since = time.Now()
		select {
		case <-ctx.Done():
			return
		case <-time.After(backoff):
		}
	}
}

// consume reads frames until EOF or an unrecoverable error. Each demuxed
// payload may contain multiple newline-terminated lines.
func (lc *logCollector) consume(rc io.ReadCloser, s *containerLogStream) error {
	br := bufio.NewReader(rc)
	for {
		stream, payload, err := readLogFrame(br)
		if err != nil {
			return err
		}
		// Split payload on \n. Each line gets its own LogLine entry, with the
		// timestamp Docker stamped on the line (we requested timestamps=1).
		for _, raw := range bytes.Split(payload, []byte{'\n'}) {
			if len(raw) == 0 {
				continue
			}
			if !lc.rateOK(s) {
				continue
			}
			if len(raw) > lc.maxLineBytes {
				raw = append(raw[:lc.maxLineBytes-3:lc.maxLineBytes-3], '.', '.', '.')
			}
			ts, msg := parseLogLine(raw)
			line := &pb.LogLine{
				ContainerId: s.containerID,
				Stream:      stream,
				TsMs:        ts.UnixMilli(),
				Line:        msg,
			}
			s.bufMu.Lock()
			s.buf = append(s.buf, line)
			if len(s.buf) > lc.maxLinesPerBatch*4 {
				// Hard cap on per-stream buffer to bound memory if leader is slow.
				s.buf = s.buf[len(s.buf)-lc.maxLinesPerBatch*2:]
			}
			s.bufMu.Unlock()
		}
	}
}

// rateOK returns true if this line is within the per-second budget.
func (lc *logCollector) rateOK(s *containerLogStream) bool {
	now := time.Now()
	cutoff := now.Add(-1 * time.Second)
	s.rateMu.Lock()
	defer s.rateMu.Unlock()
	kept := s.rateWindow[:0]
	for _, t := range s.rateWindow {
		if t.After(cutoff) {
			kept = append(kept, t)
		}
	}
	s.rateWindow = kept
	if len(s.rateWindow) >= lc.maxLinesPerSecond {
		return false
	}
	s.rateWindow = append(s.rateWindow, now)
	return true
}

// flush ships one LogBatch event per stream that has buffered lines.
func (lc *logCollector) flush(emit func(*pb.AgentMessage) error) {
	lc.mu.Lock()
	streams := make([]*containerLogStream, 0, len(lc.streams))
	for _, s := range lc.streams {
		streams = append(streams, s)
	}
	lc.mu.Unlock()

	for _, s := range streams {
		s.bufMu.Lock()
		if len(s.buf) == 0 {
			s.bufMu.Unlock()
			continue
		}
		lines := s.buf
		s.buf = nil
		s.bufMu.Unlock()

		msg := &pb.AgentMessage{
			Kind: &pb.AgentMessage_Event{Event: &pb.Event{
				Kind: &pb.Event_LogBatch{LogBatch: &pb.LogBatch{Lines: lines}},
			}},
		}
		if err := emit(msg); err != nil {
			slog.Debug("log flush emit failed", "container", s.containerID, "err", err)
		}
	}
}
