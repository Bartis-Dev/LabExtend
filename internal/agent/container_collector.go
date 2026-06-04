package agent

import (
	"context"
	"log/slog"
	"strings"
	"sync"
	"time"

	pb "github.com/Bartis-Dev/LabExtend/internal/grpc/pb"
)

// containerCollector samples Docker every sampleInterval and emits a
// ContainerReport event with the snapshot. Tracks per-container restart
// history to flag restart-loops and recent_restarts.
type containerCollector struct {
	docker         *dockerClient
	sampleInterval time.Duration

	mu         sync.Mutex
	restartLog map[string][]time.Time // container_id → ordered restart timestamps
	lastCount  map[string]int         // container_id → last seen restart_count
}

func newContainerCollector(d *dockerClient, sampleInterval time.Duration) *containerCollector {
	return &containerCollector{
		docker:         d,
		sampleInterval: sampleInterval,
		restartLog:     make(map[string][]time.Time),
		lastCount:      make(map[string]int),
	}
}

// Run loops until ctx is canceled, calling emit() with each snapshot event.
// onSnapshot is called with a map of {container_id → snapshot} after each
// successful sampling so the log collector can react to discoveries.
func (c *containerCollector) Run(
	ctx context.Context,
	emit func(*pb.AgentMessage) error,
	onSnapshot func(map[string]containerSummary),
) {
	t := time.NewTicker(c.sampleInterval)
	defer t.Stop()

	// First sample immediately so dashboards aren't blank for 5s on connect.
	c.sampleOnce(ctx, emit, onSnapshot)

	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			c.sampleOnce(ctx, emit, onSnapshot)
		}
	}
}

func (c *containerCollector) sampleOnce(
	ctx context.Context,
	emit func(*pb.AgentMessage) error,
	onSnapshot func(map[string]containerSummary),
) {
	sampleCtx, cancel := context.WithTimeout(ctx, c.sampleInterval-200*time.Millisecond)
	defer cancel()

	summaries, err := c.docker.ListContainers(sampleCtx)
	if err != nil {
		slog.Debug("container collector: list failed", "err", err)
		return
	}

	snaps := make([]*pb.ContainerSnapshot, 0, len(summaries))
	summaryMap := make(map[string]containerSummary, len(summaries))

	for _, s := range summaries {
		summaryMap[s.ID] = s
		snap := c.buildSnapshot(sampleCtx, s)
		if snap == nil {
			continue
		}
		snaps = append(snaps, snap)
	}

	if onSnapshot != nil {
		onSnapshot(summaryMap)
	}

	if emit == nil {
		return
	}
	msg := &pb.AgentMessage{
		Kind: &pb.AgentMessage_Event{Event: &pb.Event{
			Kind: &pb.Event_ContainerReport{ContainerReport: &pb.ContainerReport{
				SampledAtMs: time.Now().UnixMilli(),
				Containers:  snaps,
			}},
		}},
	}
	if err := emit(msg); err != nil {
		slog.Debug("container collector: emit failed", "err", err)
	}

	c.pruneRestartLog()
}

// buildSnapshot inspects one container and grabs a stats snapshot. Returns
// nil if the container disappeared between list and inspect (race).
func (c *containerCollector) buildSnapshot(ctx context.Context, s containerSummary) *pb.ContainerSnapshot {
	insp, err := c.docker.Inspect(ctx, s.ID)
	if err != nil {
		return nil
	}

	name := s.ID
	if len(s.Names) > 0 {
		name = strings.TrimPrefix(s.Names[0], "/")
	} else if insp.Name != "" {
		name = strings.TrimPrefix(insp.Name, "/")
	}

	startedMs := parseDockerTime(insp.State.StartedAt)
	finishedMs := parseDockerTime(insp.State.FinishedAt)

	health := ""
	if insp.State.Health != nil {
		health = insp.State.Health.Status
	}

	// Track restart count deltas so we can compute recent_restarts.
	c.mu.Lock()
	prev := c.lastCount[s.ID]
	if insp.RestartCount > prev {
		bumps := insp.RestartCount - prev
		now := time.Now()
		for i := 0; i < bumps; i++ {
			c.restartLog[s.ID] = append(c.restartLog[s.ID], now)
		}
	}
	c.lastCount[s.ID] = insp.RestartCount
	recent := countRecent(c.restartLog[s.ID], time.Now(), 60*time.Second)
	c.mu.Unlock()

	snap := &pb.ContainerSnapshot{
		ContainerId:    s.ID,
		Name:           name,
		Image:          s.Image,
		State:          insp.State.Status,
		Health:         health,
		StartedAtMs:    startedMs,
		FinishedAtMs:   finishedMs,
		RestartCount:   uint32(insp.RestartCount),
		RecentRestarts: uint32(recent),
		CrashedLoop:    recent >= 3,
		ExitCode:       int32(insp.State.ExitCode),
		MemLimitBytes:  uint64(insp.HostConfig.Memory),
		Labels:         insp.Config.Labels,
	}

	// Stats only for running containers (others return errors / zeros).
	if insp.State.Status == "running" {
		if st, err := c.docker.Stats(ctx, s.ID); err == nil {
			snap.CpuPercent = st.CPUPercent()
			snap.MemUsedBytes = st.MemoryStats.Usage
			if snap.MemLimitBytes == 0 {
				snap.MemLimitBytes = st.MemoryStats.Limit
			}
			rx, tx := st.NetTotals()
			snap.NetRxBytes = rx
			snap.NetTxBytes = tx
			rd, wr := st.BlockTotals()
			snap.BlockReadBytes = rd
			snap.BlockWriteBytes = wr
		}
	}

	return snap
}

// pruneRestartLog drops timestamps older than 5 min so the map doesn't grow
// unbounded for long-running churny containers.
func (c *containerCollector) pruneRestartLog() {
	cutoff := time.Now().Add(-5 * time.Minute)
	c.mu.Lock()
	defer c.mu.Unlock()
	for id, ts := range c.restartLog {
		kept := ts[:0]
		for _, t := range ts {
			if t.After(cutoff) {
				kept = append(kept, t)
			}
		}
		if len(kept) == 0 {
			delete(c.restartLog, id)
		} else {
			c.restartLog[id] = kept
		}
	}
}

// countRecent returns how many timestamps in ts fall within [now-window, now].
func countRecent(ts []time.Time, now time.Time, window time.Duration) int {
	cutoff := now.Add(-window)
	n := 0
	for _, t := range ts {
		if t.After(cutoff) {
			n++
		}
	}
	return n
}

// parseDockerTime turns Docker's "2026-06-04T12:34:56.123456789Z" into unix
// millis. Returns 0 for "0001-01-01T00:00:00Z" (Docker's zero-value).
func parseDockerTime(s string) int64 {
	if s == "" || strings.HasPrefix(s, "0001-01-01") {
		return 0
	}
	t, err := time.Parse(time.RFC3339Nano, s)
	if err != nil {
		return 0
	}
	return t.UnixMilli()
}
