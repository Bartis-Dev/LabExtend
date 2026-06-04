package leader

import (
	"context"
	"database/sql"
	"log/slog"
	"sync"
	"time"

	pb "github.com/Bartis-Dev/LabExtend/internal/grpc/pb"
)

// logStore persists incoming container log lines to SQLite and fans live
// lines out to subscribed WebSocket viewers.
type logStore struct {
	db              *sql.DB
	maxLinesPerCnt  int
	pruneBatchEvery int

	mu          sync.RWMutex
	subscribers map[string]map[chan *LogEntry]struct{} // subKey → sub channels
	pendingPrn  map[string]int                         // subKey → lines since last prune
}

// LogEntry is the JSON shape served via WebSocket + REST tail.
type LogEntry struct {
	ID          int64  `json:"id"`
	NodeID      string `json:"node_id"`
	ContainerID string `json:"container_id"`
	Stream      string `json:"stream"`
	TsMs        int64  `json:"ts_ms"`
	Line        string `json:"line"`
}

func newLogStore(db *sql.DB, maxLinesPerContainer int) *logStore {
	if maxLinesPerContainer <= 0 {
		maxLinesPerContainer = 5000
	}
	return &logStore{
		db:              db,
		maxLinesPerCnt:  maxLinesPerContainer,
		pruneBatchEvery: maxLinesPerContainer / 10,
		subscribers:     make(map[string]map[chan *LogEntry]struct{}),
		pendingPrn:      make(map[string]int),
	}
}

// Apply persists one LogBatch from one node, then notifies live subscribers.
func (s *logStore) Apply(ctx context.Context, nodeID string, batch *pb.LogBatch) {
	if len(batch.Lines) == 0 {
		return
	}

	type entry struct {
		view *LogEntry
		key  string
	}
	entries := make([]entry, 0, len(batch.Lines))

	if s.db != nil {
		tx, err := s.db.BeginTx(ctx, nil)
		if err != nil {
			slog.Warn("log persist: begin failed", "err", err)
			return
		}
		stmt, err := tx.PrepareContext(ctx, `
			INSERT INTO container_log_lines (node_id, container_id, ts_ms, stream, line)
			VALUES (?, ?, ?, ?, ?)
		`)
		if err != nil {
			_ = tx.Rollback()
			slog.Warn("log persist: prepare failed", "err", err)
			return
		}

		for _, line := range batch.Lines {
			res, err := stmt.ExecContext(ctx, nodeID, line.ContainerId, line.TsMs, line.Stream, line.Line)
			if err != nil {
				slog.Debug("log persist: insert failed", "err", err)
				continue
			}
			id, _ := res.LastInsertId()
			entries = append(entries, entry{
				view: &LogEntry{
					ID:          id,
					NodeID:      nodeID,
					ContainerID: line.ContainerId,
					Stream:      line.Stream,
					TsMs:        line.TsMs,
					Line:        line.Line,
				},
				key: subKey(nodeID, line.ContainerId),
			})
		}
		_ = stmt.Close()
		if err := tx.Commit(); err != nil {
			slog.Warn("log persist: commit failed", "err", err)
		}
	} else {
		// In-memory only (tests).
		for _, line := range batch.Lines {
			entries = append(entries, entry{
				view: &LogEntry{
					NodeID:      nodeID,
					ContainerID: line.ContainerId,
					Stream:      line.Stream,
					TsMs:        line.TsMs,
					Line:        line.Line,
				},
				key: subKey(nodeID, line.ContainerId),
			})
		}
	}

	// Fan out + prune accounting per key.
	pruneNeeded := make(map[string]bool)
	for _, e := range entries {
		s.fanout(e.key, e.view)
		s.mu.Lock()
		s.pendingPrn[e.key]++
		if s.pendingPrn[e.key] >= s.pruneBatchEvery {
			pruneNeeded[e.key] = true
			s.pendingPrn[e.key] = 0
		}
		s.mu.Unlock()
	}

	for k := range pruneNeeded {
		nodeID, cid := splitSubKey(k)
		go s.pruneOne(nodeID, cid)
	}
}

func (s *logStore) fanout(k string, e *LogEntry) {
	s.mu.RLock()
	subs := s.subscribers[k]
	chs := make([]chan *LogEntry, 0, len(subs))
	for ch := range subs {
		chs = append(chs, ch)
	}
	s.mu.RUnlock()
	for _, ch := range chs {
		select {
		case ch <- e:
		default:
			// Slow subscriber → drop, don't block the ingest path.
		}
	}
}

// pruneOne deletes oldest rows so the per-container cap holds.
func (s *logStore) pruneOne(nodeID, containerID string) {
	if s.db == nil {
		return
	}
	_, err := s.db.ExecContext(context.Background(), `
		DELETE FROM container_log_lines
		WHERE node_id = ? AND container_id = ?
		  AND id < (
		    SELECT id FROM container_log_lines
		    WHERE node_id = ? AND container_id = ?
		    ORDER BY id DESC LIMIT 1 OFFSET ?
		  )
	`, nodeID, containerID, nodeID, containerID, s.maxLinesPerCnt)
	if err != nil {
		slog.Debug("log prune failed", "err", err, "container", containerID)
	}
}

// Tail returns the most recent N lines for one container, oldest first.
func (s *logStore) Tail(ctx context.Context, nodeID, containerID string, n int) ([]*LogEntry, error) {
	if n <= 0 {
		n = 500
	}
	if s.db == nil {
		return nil, nil
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, ts_ms, stream, line FROM container_log_lines
		WHERE node_id = ? AND container_id = ?
		ORDER BY id DESC LIMIT ?
	`, nodeID, containerID, n)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []*LogEntry
	for rows.Next() {
		e := &LogEntry{NodeID: nodeID, ContainerID: containerID}
		if err := rows.Scan(&e.ID, &e.TsMs, &e.Stream, &e.Line); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	// Reverse so caller gets chronological order.
	for i, j := 0, len(out)-1; i < j; i, j = i+1, j-1 {
		out[i], out[j] = out[j], out[i]
	}
	return out, nil
}

// Subscribe registers a live-log channel for one container. Caller must call
// the returned cancel func to unregister.
func (s *logStore) Subscribe(nodeID, containerID string, buffer int) (<-chan *LogEntry, func()) {
	if buffer < 8 {
		buffer = 8
	}
	ch := make(chan *LogEntry, buffer)
	k := subKey(nodeID, containerID)
	s.mu.Lock()
	if s.subscribers[k] == nil {
		s.subscribers[k] = make(map[chan *LogEntry]struct{})
	}
	s.subscribers[k][ch] = struct{}{}
	s.mu.Unlock()
	cancel := func() {
		s.mu.Lock()
		if subs, ok := s.subscribers[k]; ok {
			delete(subs, ch)
			if len(subs) == 0 {
				delete(s.subscribers, k)
			}
		}
		s.mu.Unlock()
		// Drain after unregister to unblock any fanout in flight.
		go func() {
			for range ch {
			}
		}()
		close(ch)
	}
	return ch, cancel
}

// RunRetentionLoop deletes log lines older than retentionHours.
func (s *logStore) RunRetentionLoop(ctx context.Context, retentionHours int) {
	if retentionHours <= 0 {
		return
	}
	t := time.NewTicker(15 * time.Minute)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			if s.db == nil {
				continue
			}
			cutoff := time.Now().Add(-time.Duration(retentionHours)*time.Hour).UnixMilli()
			if _, err := s.db.ExecContext(ctx,
				`DELETE FROM container_log_lines WHERE ts_ms < ?`, cutoff); err != nil {
				slog.Warn("log retention prune failed", "err", err)
			}
		}
	}
}

func subKey(nodeID, containerID string) string { return nodeID + "::" + containerID }

func splitSubKey(k string) (string, string) {
	for i := 0; i < len(k)-1; i++ {
		if k[i] == ':' && k[i+1] == ':' {
			return k[:i], k[i+2:]
		}
	}
	return k, ""
}
