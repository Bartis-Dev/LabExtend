package leader

import (
	"context"
	"database/sql"
	"log/slog"
	"sync"
	"time"

	pb "github.com/Bartis-Dev/LabExtend/internal/grpc/pb"
)

// metricsStore keeps the most recent sample per node, computes deltas
// (rates) against the previous sample, and exposes the latest snapshot to
// the rest of the leader. The downsampled 1-min buckets are persisted to
// SQLite by the WriteBucket loop.
type metricsStore struct {
	db *sql.DB

	mu      sync.RWMutex
	current map[string]*MetricsSample // node_id → latest
	prev    map[string]*MetricsSample // node_id → previous (for delta calc)

	bucketMu sync.Mutex
	buckets  map[string]*minuteBucket // node_id → accumulator for current minute
}

// MetricsSample is the leader-side, computed-rate view of one heartbeat.
type MetricsSample struct {
	NodeID         string  `json:"node_id"`
	ReportedAt     int64   `json:"reported_at"`
	UptimeSeconds  int64   `json:"uptime_seconds"`
	LoadAvg1m      float64 `json:"load_avg_1m"`
	CPUPercent     float64 `json:"cpu_percent"`
	CPUCores       uint32  `json:"cpu_cores"`
	MemUsedBytes   uint64  `json:"mem_used_bytes"`
	MemTotalBytes  uint64  `json:"mem_total_bytes"`
	MemPercent     float64 `json:"mem_percent"`
	DiskUsedBytes  uint64  `json:"disk_used_bytes"`
	DiskTotalBytes uint64  `json:"disk_total_bytes"`
	DiskPercent    float64 `json:"disk_percent"`
	NetRxBytes     uint64  `json:"net_rx_bytes"`
	NetTxBytes     uint64  `json:"net_tx_bytes"`
	NetRxBps       int64   `json:"net_rx_bps"`
	NetTxBps       int64   `json:"net_tx_bps"`
	DiskReadBytes  uint64  `json:"disk_read_bytes"`
	DiskWriteBytes uint64  `json:"disk_write_bytes"`
	DiskReadBps    int64   `json:"disk_read_bps"`
	DiskWriteBps   int64   `json:"disk_write_bps"`
}

// minuteBucket aggregates samples within one minute window so we can persist
// one row to node_metric_buckets per (node, minute).
type minuteBucket struct {
	startedAt time.Time
	sum       MetricsSample
	count     int
}

func newMetricsStore(db *sql.DB) *metricsStore {
	return &metricsStore{
		db:      db,
		current: make(map[string]*MetricsSample),
		prev:    make(map[string]*MetricsSample),
		buckets: make(map[string]*minuteBucket),
	}
}

// Apply ingests one heartbeat from one node. Returns the computed sample.
func (s *metricsStore) Apply(nodeID string, hb *pb.Heartbeat) *MetricsSample {
	now := time.Now()
	sample := &MetricsSample{
		NodeID:         nodeID,
		ReportedAt:     now.Unix(),
		UptimeSeconds:  hb.UptimeSeconds,
		LoadAvg1m:      hb.LoadAvg_1M,
		CPUPercent:     hb.CpuPercent,
		CPUCores:       hb.CpuCores,
		MemUsedBytes:   hb.MemUsedBytes,
		MemTotalBytes:  hb.MemTotalBytes,
		DiskUsedBytes:  hb.DiskUsedBytes,
		DiskTotalBytes: hb.DiskTotalBytes,
		NetRxBytes:     hb.NetRxBytes,
		NetTxBytes:     hb.NetTxBytes,
		DiskReadBytes:  hb.DiskReadBytes,
		DiskWriteBytes: hb.DiskWriteBytes,
	}
	if sample.MemTotalBytes > 0 {
		sample.MemPercent = 100 * float64(sample.MemUsedBytes) / float64(sample.MemTotalBytes)
	}
	if sample.DiskTotalBytes > 0 {
		sample.DiskPercent = 100 * float64(sample.DiskUsedBytes) / float64(sample.DiskTotalBytes)
	}

	s.mu.Lock()
	prev := s.current[nodeID]
	if prev != nil {
		dt := sample.ReportedAt - prev.ReportedAt
		if dt > 0 {
			sample.NetRxBps = bytesPerSecond(sample.NetRxBytes, prev.NetRxBytes, dt)
			sample.NetTxBps = bytesPerSecond(sample.NetTxBytes, prev.NetTxBytes, dt)
			sample.DiskReadBps = bytesPerSecond(sample.DiskReadBytes, prev.DiskReadBytes, dt)
			sample.DiskWriteBps = bytesPerSecond(sample.DiskWriteBytes, prev.DiskWriteBytes, dt)
		}
	}
	s.prev[nodeID] = prev
	s.current[nodeID] = sample
	s.mu.Unlock()

	s.feedBucket(now, sample)
	return sample
}

// Current returns the latest sample for one node, or nil if unknown.
func (s *metricsStore) Current(nodeID string) *MetricsSample {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.current[nodeID]
}

// All returns the latest sample for every known node.
func (s *metricsStore) All() map[string]*MetricsSample {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make(map[string]*MetricsSample, len(s.current))
	for k, v := range s.current {
		cp := *v
		out[k] = &cp
	}
	return out
}

// MarkOffline drops a node's in-memory state when it disconnects so the UI
// shows zero (instead of stale) values.
func (s *metricsStore) MarkOffline(nodeID string) {
	s.mu.Lock()
	delete(s.current, nodeID)
	delete(s.prev, nodeID)
	s.mu.Unlock()
}

// Persist writes the sample to node_metrics (latest snapshot) AND appends a
// row to node_metric_samples (short-retention history for sub-minute UI
// granularity).
func (s *metricsStore) Persist(ctx context.Context, sample *MetricsSample) error {
	if s.db == nil {
		return nil
	}
	// Raw sample for the 5m / 1h windows. Errors here are non-fatal — the
	// upsert below is the source of truth for "current state".
	_, _ = s.db.ExecContext(ctx, `
		INSERT INTO node_metric_samples
			(node_id, ts_ms, cpu_percent, mem_pct, disk_pct,
			 net_rx_bps, net_tx_bps, disk_read_bps, disk_write_bps)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, sample.NodeID, sample.ReportedAt*1000,
		sample.CPUPercent, sample.MemPercent, sample.DiskPercent,
		sample.NetRxBps, sample.NetTxBps,
		sample.DiskReadBps, sample.DiskWriteBps,
	)

	_, err := s.db.ExecContext(ctx, `
		INSERT INTO node_metrics
			(node_id, reported_at, uptime_seconds, load_avg_1m,
			 mem_used_bytes, mem_total_bytes,
			 disk_used_bytes, disk_total_bytes,
			 cpu_percent, cpu_cores,
			 net_rx_bytes, net_tx_bytes,
			 disk_read_bytes, disk_write_bytes,
			 net_rx_bps, net_tx_bps,
			 disk_read_bps, disk_write_bps)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(node_id) DO UPDATE SET
			reported_at      = excluded.reported_at,
			uptime_seconds   = excluded.uptime_seconds,
			load_avg_1m      = excluded.load_avg_1m,
			mem_used_bytes   = excluded.mem_used_bytes,
			mem_total_bytes  = excluded.mem_total_bytes,
			disk_used_bytes  = excluded.disk_used_bytes,
			disk_total_bytes = excluded.disk_total_bytes,
			cpu_percent      = excluded.cpu_percent,
			cpu_cores        = excluded.cpu_cores,
			net_rx_bytes     = excluded.net_rx_bytes,
			net_tx_bytes     = excluded.net_tx_bytes,
			disk_read_bytes  = excluded.disk_read_bytes,
			disk_write_bytes = excluded.disk_write_bytes,
			net_rx_bps       = excluded.net_rx_bps,
			net_tx_bps       = excluded.net_tx_bps,
			disk_read_bps    = excluded.disk_read_bps,
			disk_write_bps   = excluded.disk_write_bps
	`,
		sample.NodeID, sample.ReportedAt, sample.UptimeSeconds, sample.LoadAvg1m,
		sample.MemUsedBytes, sample.MemTotalBytes,
		sample.DiskUsedBytes, sample.DiskTotalBytes,
		sample.CPUPercent, sample.CPUCores,
		sample.NetRxBytes, sample.NetTxBytes,
		sample.DiskReadBytes, sample.DiskWriteBytes,
		sample.NetRxBps, sample.NetTxBps,
		sample.DiskReadBps, sample.DiskWriteBps,
	)
	return err
}

// feedBucket accumulates the sample into the current-minute bucket. When the
// minute rolls over, the previous bucket is flushed to SQLite by the
// background loop.
func (s *metricsStore) feedBucket(now time.Time, sample *MetricsSample) {
	minute := now.Truncate(time.Minute)
	s.bucketMu.Lock()
	defer s.bucketMu.Unlock()
	b, ok := s.buckets[sample.NodeID]
	if !ok || !b.startedAt.Equal(minute) {
		// Roll over: persist old bucket, start new.
		if ok {
			go s.flushBucket(sample.NodeID, b)
		}
		b = &minuteBucket{startedAt: minute}
		s.buckets[sample.NodeID] = b
	}
	b.count++
	b.sum.CPUPercent += sample.CPUPercent
	b.sum.MemPercent += sample.MemPercent
	b.sum.DiskPercent += sample.DiskPercent
	b.sum.NetRxBps += sample.NetRxBps
	b.sum.NetTxBps += sample.NetTxBps
	b.sum.DiskReadBps += sample.DiskReadBps
	b.sum.DiskWriteBps += sample.DiskWriteBps
}

// flushBucket persists the minute bucket as a single row.
func (s *metricsStore) flushBucket(nodeID string, b *minuteBucket) {
	if s.db == nil || b.count == 0 {
		return
	}
	avg := func(x float64) float64 { return x / float64(b.count) }
	avgI := func(x int64) int64 { return x / int64(b.count) }

	_, err := s.db.ExecContext(context.Background(), `
		INSERT INTO node_metric_buckets
			(node_id, bucket_minute, cpu_percent, mem_pct, disk_pct,
			 net_rx_bps, net_tx_bps, disk_read_bps, disk_write_bps, sample_count)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(node_id, bucket_minute) DO UPDATE SET
			cpu_percent    = excluded.cpu_percent,
			mem_pct        = excluded.mem_pct,
			disk_pct       = excluded.disk_pct,
			net_rx_bps     = excluded.net_rx_bps,
			net_tx_bps     = excluded.net_tx_bps,
			disk_read_bps  = excluded.disk_read_bps,
			disk_write_bps = excluded.disk_write_bps,
			sample_count   = excluded.sample_count
	`,
		nodeID, b.startedAt.Unix(),
		avg(b.sum.CPUPercent), avg(b.sum.MemPercent), avg(b.sum.DiskPercent),
		avgI(b.sum.NetRxBps), avgI(b.sum.NetTxBps),
		avgI(b.sum.DiskReadBps), avgI(b.sum.DiskWriteBps), b.count,
	)
	if err != nil {
		slog.Warn("metrics bucket persist failed", "node", nodeID, "err", err)
	}
}

// RunPruneLoop runs forever, pruning bucket rows older than bucketRetentionHours
// AND raw-sample rows older than sampleRetentionHours. Bucket retention is the
// long view (default 7d, configurable); sample retention is short (default 2h)
// because samples are only used for the live-graph 5m/1h windows.
func (s *metricsStore) RunPruneLoop(ctx context.Context, bucketRetentionHours, sampleRetentionHours int) {
	t := time.NewTicker(5 * time.Minute)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			if s.db == nil {
				continue
			}
			bucketCutoff := time.Now().Add(-time.Duration(bucketRetentionHours) * time.Hour).Unix()
			if _, err := s.db.ExecContext(ctx,
				`DELETE FROM node_metric_buckets WHERE bucket_minute < ?`, bucketCutoff); err != nil {
				slog.Warn("metrics bucket prune failed", "err", err)
			}
			sampleCutoffMs := time.Now().Add(-time.Duration(sampleRetentionHours) * time.Hour).UnixMilli()
			if _, err := s.db.ExecContext(ctx,
				`DELETE FROM node_metric_samples WHERE ts_ms < ?`, sampleCutoffMs); err != nil {
				slog.Warn("metrics sample prune failed", "err", err)
			}
		}
	}
}

// SamplesForNode returns raw per-heartbeat samples for the [sinceMs, untilMs]
// window. Used for the 5m / 1h UI views where 1-min buckets aren't fine enough.
func (s *metricsStore) SamplesForNode(ctx context.Context, nodeID string, sinceMs, untilMs int64) ([]MetricsBucket, error) {
	if s.db == nil {
		return nil, nil
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT ts_ms, cpu_percent, mem_pct, disk_pct,
		       net_rx_bps, net_tx_bps, disk_read_bps, disk_write_bps
		FROM node_metric_samples
		WHERE node_id = ? AND ts_ms BETWEEN ? AND ?
		ORDER BY ts_ms ASC
	`, nodeID, sinceMs, untilMs)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []MetricsBucket
	for rows.Next() {
		var b MetricsBucket
		var tsMs int64
		if err := rows.Scan(&tsMs, &b.CPUPercent, &b.MemPercent, &b.DiskPercent,
			&b.NetRxBps, &b.NetTxBps, &b.DiskReadBps, &b.DiskWriteBps); err != nil {
			return nil, err
		}
		// Samples table uses ms; reuse the bucket-shape with seconds for the UI.
		b.BucketMinute = tsMs / 1000
		out = append(out, b)
	}
	return out, rows.Err()
}

// BucketsForNode returns minute averages for one node within [since, until].
// Used by /api/nodes/:id/history endpoint.
func (s *metricsStore) BucketsForNode(ctx context.Context, nodeID string, sinceUnix, untilUnix int64) ([]MetricsBucket, error) {
	if s.db == nil {
		return nil, nil
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT bucket_minute, cpu_percent, mem_pct, disk_pct,
		       net_rx_bps, net_tx_bps, disk_read_bps, disk_write_bps
		FROM node_metric_buckets
		WHERE node_id = ? AND bucket_minute BETWEEN ? AND ?
		ORDER BY bucket_minute ASC
	`, nodeID, sinceUnix, untilUnix)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []MetricsBucket
	for rows.Next() {
		var b MetricsBucket
		if err := rows.Scan(&b.BucketMinute, &b.CPUPercent, &b.MemPercent, &b.DiskPercent,
			&b.NetRxBps, &b.NetTxBps, &b.DiskReadBps, &b.DiskWriteBps); err != nil {
			return nil, err
		}
		out = append(out, b)
	}
	return out, rows.Err()
}

// MetricsBucket is the JSON shape returned to the UI.
type MetricsBucket struct {
	BucketMinute int64   `json:"bucket_minute"`
	CPUPercent   float64 `json:"cpu_percent"`
	MemPercent   float64 `json:"mem_percent"`
	DiskPercent  float64 `json:"disk_percent"`
	NetRxBps     int64   `json:"net_rx_bps"`
	NetTxBps     int64   `json:"net_tx_bps"`
	DiskReadBps  int64   `json:"disk_read_bps"`
	DiskWriteBps int64   `json:"disk_write_bps"`
}

func bytesPerSecond(now, prev uint64, dtSec int64) int64 {
	if now < prev || dtSec <= 0 {
		return 0
	}
	return int64((now - prev) / uint64(dtSec))
}
