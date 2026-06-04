package leader

import (
	"context"
	"database/sql"
	"encoding/json"
	"sync"
	"time"

	pb "github.com/Bartis-Dev/LabExtend/internal/grpc/pb"
)

// containerStore holds the cluster-wide current state of every container
// (one entry per (node_id, container_id)). The full state is also persisted
// to SQLite on every report so the UI can render after restart even before
// the first agent reconnects.
//
// Bytes/sec rates for net are computed leader-side from successive snapshots.
type containerStore struct {
	db *sql.DB

	mu      sync.RWMutex
	state   map[string]*ContainerView // key = node + "::" + container_id
	prevNet map[string]netSample      // for rate calculation
}

// ContainerView is the JSON-friendly shape returned by /api/containers.
type ContainerView struct {
	NodeID          string            `json:"node_id"`
	ContainerID     string            `json:"container_id"`
	Name            string            `json:"name"`
	Image           string            `json:"image"`
	State           string            `json:"state"`
	Health          string            `json:"health"`
	StartedAtMs     int64             `json:"started_at_ms"`
	FinishedAtMs    int64             `json:"finished_at_ms"`
	RestartCount    uint32            `json:"restart_count"`
	RecentRestarts  uint32            `json:"recent_restarts"`
	CrashedLoop     bool              `json:"crashed_loop"`
	ExitCode        int32             `json:"exit_code"`
	CPUPercent      float64           `json:"cpu_percent"`
	MemUsedBytes    uint64            `json:"mem_used_bytes"`
	MemLimitBytes   uint64            `json:"mem_limit_bytes"`
	MemPercent      float64           `json:"mem_percent"`
	NetRxBytes      uint64            `json:"net_rx_bytes"`
	NetTxBytes      uint64            `json:"net_tx_bytes"`
	NetRxBps        int64             `json:"net_rx_bps"`
	NetTxBps        int64             `json:"net_tx_bps"`
	BlockReadBytes  uint64            `json:"block_read_bytes"`
	BlockWriteBytes uint64            `json:"block_write_bytes"`
	Labels          map[string]string `json:"labels"`
	ReportedAt      int64             `json:"reported_at"`
}

type netSample struct {
	rx uint64
	tx uint64
	ts int64
}

func newContainerStore(db *sql.DB) *containerStore {
	return &containerStore{
		db:      db,
		state:   make(map[string]*ContainerView),
		prevNet: make(map[string]netSample),
	}
}

func key(node, container string) string { return node + "::" + container }

// Apply ingests one ContainerReport from one node. Returns the updated
// ContainerView slice for SSE broadcast.
func (s *containerStore) Apply(ctx context.Context, nodeID string, rep *pb.ContainerReport) []*ContainerView {
	now := rep.SampledAtMs / 1000
	if now == 0 {
		now = time.Now().Unix()
	}

	updated := make([]*ContainerView, 0, len(rep.Containers))
	seen := make(map[string]bool, len(rep.Containers))

	s.mu.Lock()
	for _, snap := range rep.Containers {
		k := key(nodeID, snap.ContainerId)
		seen[k] = true

		view := &ContainerView{
			NodeID:          nodeID,
			ContainerID:     snap.ContainerId,
			Name:            snap.Name,
			Image:           snap.Image,
			State:           snap.State,
			Health:          snap.Health,
			StartedAtMs:     snap.StartedAtMs,
			FinishedAtMs:    snap.FinishedAtMs,
			RestartCount:    snap.RestartCount,
			RecentRestarts:  snap.RecentRestarts,
			CrashedLoop:     snap.CrashedLoop,
			ExitCode:        snap.ExitCode,
			CPUPercent:      snap.CpuPercent,
			MemUsedBytes:    snap.MemUsedBytes,
			MemLimitBytes:   snap.MemLimitBytes,
			NetRxBytes:      snap.NetRxBytes,
			NetTxBytes:      snap.NetTxBytes,
			BlockReadBytes:  snap.BlockReadBytes,
			BlockWriteBytes: snap.BlockWriteBytes,
			Labels:          snap.Labels,
			ReportedAt:      now,
		}
		if view.MemLimitBytes > 0 {
			view.MemPercent = 100 * float64(view.MemUsedBytes) / float64(view.MemLimitBytes)
		}

		// Net rate against previous sample for this container.
		if prev, ok := s.prevNet[k]; ok && now > prev.ts {
			dt := now - prev.ts
			if view.NetRxBytes >= prev.rx {
				view.NetRxBps = int64((view.NetRxBytes - prev.rx) / uint64(dt))
			}
			if view.NetTxBytes >= prev.tx {
				view.NetTxBps = int64((view.NetTxBytes - prev.tx) / uint64(dt))
			}
		}
		s.prevNet[k] = netSample{rx: view.NetRxBytes, tx: view.NetTxBytes, ts: now}

		s.state[k] = view
		updated = append(updated, view)
	}

	// Remove containers this node didn't report (deleted on host).
	for k, v := range s.state {
		if v.NodeID != nodeID {
			continue
		}
		if !seen[k] {
			delete(s.state, k)
			delete(s.prevNet, k)
		}
	}
	s.mu.Unlock()

	if s.db != nil {
		s.persistBatch(ctx, nodeID, updated, seen)
	}
	return updated
}

// persistBatch writes (or deletes) container_state rows in one transaction.
func (s *containerStore) persistBatch(ctx context.Context, nodeID string, views []*ContainerView, seen map[string]bool) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return
	}
	defer tx.Rollback() //nolint:errcheck

	for _, v := range views {
		labelsJSON := mapToJSON(v.Labels)
		crashed := 0
		if v.CrashedLoop {
			crashed = 1
		}
		_, _ = tx.ExecContext(ctx, `
			INSERT INTO container_state
				(node_id, container_id, name, image, state, health,
				 started_at_ms, finished_at_ms, restart_count, recent_restarts,
				 crashed_loop, exit_code, cpu_percent, mem_used_bytes, mem_limit_bytes,
				 net_rx_bytes, net_tx_bytes, net_rx_bps, net_tx_bps,
				 block_read_bytes, block_write_bytes, labels_json, reported_at)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
			ON CONFLICT(node_id, container_id) DO UPDATE SET
				name              = excluded.name,
				image             = excluded.image,
				state             = excluded.state,
				health            = excluded.health,
				started_at_ms     = excluded.started_at_ms,
				finished_at_ms    = excluded.finished_at_ms,
				restart_count     = excluded.restart_count,
				recent_restarts   = excluded.recent_restarts,
				crashed_loop      = excluded.crashed_loop,
				exit_code         = excluded.exit_code,
				cpu_percent       = excluded.cpu_percent,
				mem_used_bytes    = excluded.mem_used_bytes,
				mem_limit_bytes   = excluded.mem_limit_bytes,
				net_rx_bytes      = excluded.net_rx_bytes,
				net_tx_bytes      = excluded.net_tx_bytes,
				net_rx_bps        = excluded.net_rx_bps,
				net_tx_bps        = excluded.net_tx_bps,
				block_read_bytes  = excluded.block_read_bytes,
				block_write_bytes = excluded.block_write_bytes,
				labels_json       = excluded.labels_json,
				reported_at       = excluded.reported_at
		`,
			v.NodeID, v.ContainerID, v.Name, v.Image, v.State, v.Health,
			v.StartedAtMs, v.FinishedAtMs, v.RestartCount, v.RecentRestarts,
			crashed, v.ExitCode, v.CPUPercent, v.MemUsedBytes, v.MemLimitBytes,
			v.NetRxBytes, v.NetTxBytes, v.NetRxBps, v.NetTxBps,
			v.BlockReadBytes, v.BlockWriteBytes, labelsJSON, v.ReportedAt,
		)
	}

	// Drop rows for this node not in the latest report.
	if len(seen) > 0 {
		rows, err := tx.QueryContext(ctx, `SELECT container_id FROM container_state WHERE node_id = ?`, nodeID)
		if err == nil {
			var toDelete []string
			for rows.Next() {
				var cid string
				if err := rows.Scan(&cid); err == nil && !seen[key(nodeID, cid)] {
					toDelete = append(toDelete, cid)
				}
			}
			rows.Close()
			for _, cid := range toDelete {
				_, _ = tx.ExecContext(ctx, `DELETE FROM container_state WHERE node_id = ? AND container_id = ?`, nodeID, cid)
			}
		}
	}

	_ = tx.Commit()
}

// MarkNodeOffline drops all containers for a node when the agent disconnects.
// We KEEP the DB rows (last-seen survives restart) and only remove the
// in-memory live state.
func (s *containerStore) MarkNodeOffline(nodeID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for k, v := range s.state {
		if v.NodeID == nodeID {
			delete(s.state, k)
			delete(s.prevNet, k)
		}
	}
}

// All returns a snapshot of every container view (live + persisted).
// Live state takes precedence over DB rows.
func (s *containerStore) All(ctx context.Context) ([]*ContainerView, error) {
	s.mu.RLock()
	live := make(map[string]*ContainerView, len(s.state))
	for k, v := range s.state {
		cp := *v
		live[k] = &cp
	}
	s.mu.RUnlock()

	// Pull persisted rows for offline nodes / restart-restored data.
	if s.db != nil {
		rows, err := s.db.QueryContext(ctx, `
			SELECT node_id, container_id, name, image, state, health,
			       started_at_ms, finished_at_ms, restart_count, recent_restarts,
			       crashed_loop, exit_code, cpu_percent, mem_used_bytes, mem_limit_bytes,
			       net_rx_bytes, net_tx_bytes, net_rx_bps, net_tx_bps,
			       block_read_bytes, block_write_bytes, labels_json, reported_at
			FROM container_state
		`)
		if err == nil {
			defer rows.Close()
			for rows.Next() {
				var v ContainerView
				var crashed int
				var labelsJSON string
				if err := rows.Scan(&v.NodeID, &v.ContainerID, &v.Name, &v.Image, &v.State, &v.Health,
					&v.StartedAtMs, &v.FinishedAtMs, &v.RestartCount, &v.RecentRestarts,
					&crashed, &v.ExitCode, &v.CPUPercent, &v.MemUsedBytes, &v.MemLimitBytes,
					&v.NetRxBytes, &v.NetTxBytes, &v.NetRxBps, &v.NetTxBps,
					&v.BlockReadBytes, &v.BlockWriteBytes, &labelsJSON, &v.ReportedAt); err != nil {
					continue
				}
				v.CrashedLoop = crashed == 1
				if v.MemLimitBytes > 0 {
					v.MemPercent = 100 * float64(v.MemUsedBytes) / float64(v.MemLimitBytes)
				}
				if labelsJSON != "" {
					_ = json.Unmarshal([]byte(labelsJSON), &v.Labels)
				}
				k := key(v.NodeID, v.ContainerID)
				if _, ok := live[k]; !ok {
					live[k] = &v
				}
			}
		}
	}

	out := make([]*ContainerView, 0, len(live))
	for _, v := range live {
		out = append(out, v)
	}
	return out, nil
}

// Get returns the view for one specific container.
func (s *containerStore) Get(ctx context.Context, nodeID, containerID string) *ContainerView {
	s.mu.RLock()
	v, ok := s.state[key(nodeID, containerID)]
	s.mu.RUnlock()
	if ok {
		cp := *v
		return &cp
	}
	if s.db == nil {
		return nil
	}
	var view ContainerView
	var crashed int
	var labelsJSON string
	err := s.db.QueryRowContext(ctx, `
		SELECT node_id, container_id, name, image, state, health,
		       started_at_ms, finished_at_ms, restart_count, recent_restarts,
		       crashed_loop, exit_code, cpu_percent, mem_used_bytes, mem_limit_bytes,
		       net_rx_bytes, net_tx_bytes, net_rx_bps, net_tx_bps,
		       block_read_bytes, block_write_bytes, labels_json, reported_at
		FROM container_state WHERE node_id = ? AND container_id = ?
	`, nodeID, containerID).Scan(
		&view.NodeID, &view.ContainerID, &view.Name, &view.Image, &view.State, &view.Health,
		&view.StartedAtMs, &view.FinishedAtMs, &view.RestartCount, &view.RecentRestarts,
		&crashed, &view.ExitCode, &view.CPUPercent, &view.MemUsedBytes, &view.MemLimitBytes,
		&view.NetRxBytes, &view.NetTxBytes, &view.NetRxBps, &view.NetTxBps,
		&view.BlockReadBytes, &view.BlockWriteBytes, &labelsJSON, &view.ReportedAt,
	)
	if err != nil {
		return nil
	}
	view.CrashedLoop = crashed == 1
	if view.MemLimitBytes > 0 {
		view.MemPercent = 100 * float64(view.MemUsedBytes) / float64(view.MemLimitBytes)
	}
	if labelsJSON != "" {
		_ = json.Unmarshal([]byte(labelsJSON), &view.Labels)
	}
	return &view
}

func mapToJSON(m map[string]string) string {
	if len(m) == 0 {
		return "{}"
	}
	b, err := json.Marshal(m)
	if err != nil {
		return "{}"
	}
	return string(b)
}
