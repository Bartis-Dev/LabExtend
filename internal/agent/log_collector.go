package agent

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"io"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	pb "github.com/Bartis-Dev/LabExtend/internal/grpc/pb"
)

// logCollector keeps one goroutine per running container reading
// `docker logs -f` and shipping batched LogBatch events to the leader.
//
// Per-container rate cap: we drop lines beyond maxLinesPerSecond to keep a
// runaway-logger container from saturating the gRPC channel.
type logCollector struct {
	docker            *dockerClient
	flushInterval     time.Duration
	maxLinesPerBatch  int
	maxLinesPerSecond int
	maxLineBytes      int
	tailFromBeginning bool

	mu       sync.Mutex
	streams  map[string]*containerLogStream
	disabled map[string]bool
}

type containerLogStream struct {
	containerID string
	cancel      context.CancelFunc

	bufMu sync.Mutex
	buf   []*pb.LogLine

	rateMu     sync.Mutex
	rateWindow []time.Time

	// emittedBatches tracks how many batches we've successfully shipped.
	// First emit per stream is logged at Info; subsequent at Debug.
	emittedBatches atomic.Uint64
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

	for id, s := range lc.streams {
		c, ok := current[id]
		if !ok || c.State != "running" {
			s.cancel()
			delete(lc.streams, id)
		}
	}

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

func (lc *logCollector) stopAll() {
	lc.mu.Lock()
	defer lc.mu.Unlock()
	for _, s := range lc.streams {
		s.cancel()
	}
	lc.streams = make(map[string]*containerLogStream)
}

// tail reads the log stream until ctx is canceled. Probes the container's
// TTY mode once via Inspect; TTY=true streams are raw bytes (no demuxer),
// TTY=false streams use Docker's 8-byte frame multiplex.
func (lc *logCollector) tail(ctx context.Context, s *containerLogStream) {
	short := s.containerID
	if len(short) > 12 {
		short = short[:12]
	}

	insp, err := lc.docker.Inspect(ctx, s.containerID)
	if err != nil {
		slog.Warn("log tail: inspect failed", "container", short, "err", err)
		return
	}
	isTty := insp.Config.Tty
	name := insp.Name
	if len(name) > 0 && name[0] == '/' {
		name = name[1:]
	}
	slog.Info("log tail: started", "container", short, "name", name, "tty", isTty)

	backoff := 500 * time.Millisecond
	maxBackoff := 30 * time.Second

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
			slog.Debug("log tail: open failed", "container", short, "err", err)
			select {
			case <-ctx.Done():
				return
			case <-time.After(backoff):
			}
			backoff *= 2
			if backoff > maxBackoff {
				backoff = maxBackoff
			}
			continue
		}
		backoff = 500 * time.Millisecond

		var consErr error
		if isTty {
			consErr = lc.consumeRaw(rc, s)
		} else {
			consErr = lc.consumeFramed(rc, s)
		}
		rc.Close()

		if errors.Is(consErr, context.Canceled) || ctx.Err() != nil {
			return
		}
		if consErr != nil && !errors.Is(consErr, io.EOF) {
			slog.Warn("log tail: consume errored, reconnecting",
				"container", short, "tty", isTty, "err", consErr)
		}
		since = time.Now()
		select {
		case <-ctx.Done():
			return
		case <-time.After(backoff):
		}
	}
}

// consumeFramed reads Docker's 8-byte multiplex frame format (TTY=false).
//
//	byte 0: stream type (1=stdout, 2=stderr)
//	bytes 1-3: padding
//	bytes 4-7: BE uint32 payload length
func (lc *logCollector) consumeFramed(rc io.ReadCloser, s *containerLogStream) error {
	br := bufio.NewReader(rc)
	for {
		stream, payload, err := readLogFrame(br)
		if err != nil {
			return err
		}
		for _, raw := range bytes.Split(payload, []byte{'\n'}) {
			if len(raw) == 0 {
				continue
			}
			if !lc.rateOK(s) {
				continue
			}
			lc.appendLine(s, stream, raw)
		}
	}
}

// consumeRaw reads a TTY-mode stream where stdout+stderr are merged into
// plain bytes without frame headers. Line-oriented scan.
func (lc *logCollector) consumeRaw(rc io.ReadCloser, s *containerLogStream) error {
	sc := bufio.NewScanner(rc)
	// Scanner default buf is 64KB max-line — bump so we don't hit it on
	// containers that occasionally log long stack traces.
	sc.Buffer(make([]byte, 64*1024), 4*1024*1024)
	for sc.Scan() {
		raw := sc.Bytes()
		if len(raw) == 0 {
			continue
		}
		if !lc.rateOK(s) {
			continue
		}
		// Copy because scanner reuses the buffer between calls.
		cp := make([]byte, len(raw))
		copy(cp, raw)
		lc.appendLine(s, "stdout", cp)
	}
	return sc.Err()
}

// appendLine truncates oversized lines, parses the Docker timestamp prefix,
// and appends to the per-stream buffer (hard cap to bound memory).
func (lc *logCollector) appendLine(s *containerLogStream, stream string, raw []byte) {
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
		s.buf = s.buf[len(s.buf)-lc.maxLinesPerBatch*2:]
	}
	s.bufMu.Unlock()
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
// First successful batch per stream is logged at Info so the operator can
// see in the logs that the pipeline is alive.
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

		short := s.containerID
		if len(short) > 12 {
			short = short[:12]
		}

		msg := &pb.AgentMessage{
			Kind: &pb.AgentMessage_Event{Event: &pb.Event{
				Kind: &pb.Event_LogBatch{LogBatch: &pb.LogBatch{Lines: lines}},
			}},
		}
		if err := emit(msg); err != nil {
			slog.Warn("log flush: emit failed",
				"container", short, "lines", len(lines), "err", err)
			continue
		}
		n := s.emittedBatches.Add(1)
		if n == 1 {
			slog.Info("log tail: first batch shipped",
				"container", short, "lines", len(lines))
		} else {
			slog.Debug("log batch shipped",
				"container", short, "lines", len(lines), "batch_n", n)
		}
	}
}
