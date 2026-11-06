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

	summaryMap := make(map[string]containerSummary, len(summaries))
	built := make([]*pb.ContainerSnapshot, len(summaries))
	// Docker answers stats?stream=0 only after two samples, about a second
	// each. One after another, the budget above covered three or four
	// containers and every later one went unreported, so they run in parallel.
	// ponytail: fixed width, a host with hundreds of containers can still run
	// out of budget for stats (it is still reported, without fresh stats).
	sem := make(chan struct{}, 16)
	var wg sync.WaitGroup
	for i, s := range summaries {
		summaryMap[s.ID] = s
		wg.Add(1)
		go func(i int, s containerSummary) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			built[i] = c.buildSnapshot(ctx, sampleCtx, s)
		}(i, s)
	}
	wg.Wait()
	snaps := make([]*pb.ContainerSnapshot, 0, len(built))
	for _, snap := range built {
		if snap != nil {
			snaps = append(snaps, snap)
		}
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
// The inspect gets its own short deadline rather than the sample's, so a
// container is never dropped only because the stats of others ate the budget.
func (c *containerCollector) buildSnapshot(ctx, statsCtx context.Context, s containerSummary) *pb.ContainerSnapshot {
	ictx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	insp, err := c.docker.Inspect(ictx, s.ID)
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

	recent := c.recordRestarts(s.ID, insp.RestartCount, time.Now())

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
		if st, err := c.docker.Stats(statsCtx, s.ID); err == nil {
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

// recordRestarts turns Docker's lifetime restart counter into restarts within
// the last minute. The first sighting only sets the baseline: the counter
// covers the container's whole life, and stamping all of it "now" made every
// agent start report long-lived containers as crash-looping — a false
// container_crashed alert on every rollout.
func (c *containerCollector) recordRestarts(id string, count int, now time.Time) int {
	c.mu.Lock()
	defer c.mu.Unlock()
	prev, seen := c.lastCount[id]
	if seen && count > prev {
		for i := 0; i < count-prev; i++ {
			c.restartLog[id] = append(c.restartLog[id], now)
		}
	}
	c.lastCount[id] = count
	return countRecent(c.restartLog[id], now, 60*time.Second)
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
