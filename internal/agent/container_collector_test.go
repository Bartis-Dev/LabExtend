package agent

import (
	"testing"
	"time"

	pb "github.com/Bartis-Dev/LabExtend/internal/grpc/pb"
)

// Docker's RestartCount covers the container's whole life. Seeing it for the
// first time — every agent start — must not look like that many restarts in
// the last minute, or each rollout raises a false container.crashloop notification.
func TestRecordRestartsSeedsBaselineOnFirstSight(t *testing.T) {
	c := newContainerCollector(nil, time.Second)
	now := time.Now()

	if got := c.recordRestarts("a", 7, now); got != 0 {
		t.Fatalf("first sight of 7 lifetime restarts: recent=%d, want 0", got)
	}
	if got := c.recordRestarts("a", 7, now.Add(time.Second)); got != 0 {
		t.Fatalf("unchanged counter: recent=%d, want 0", got)
	}
	// A real loop after the baseline is still caught (CrashedLoop is >= 3).
	if got := c.recordRestarts("a", 10, now.Add(2*time.Second)); got != 3 {
		t.Fatalf("3 new restarts: recent=%d, want 3", got)
	}
	if got := c.recordRestarts("a", 10, now.Add(2*time.Minute)); got != 0 {
		t.Fatalf("restarts older than the 60s window: recent=%d, want 0", got)
	}
}

// Swarm replaces a failed task with a new container whose RestartCount is 0,
// so the per-container counter never sees a Swarm service crash-looping.
func TestSwarmServiceCrashLoopMarksCurrentContainer(t *testing.T) {
	now := time.Unix(10_000, 0)
	svc := map[string]string{swarmServiceLabel: "svc1"}
	summaries := []containerSummary{
		{ID: "old1", State: "exited", Labels: svc, Created: now.Add(-50 * time.Second).Unix()},
		{ID: "old2", State: "exited", Labels: svc, Created: now.Add(-30 * time.Second).Unix()},
		{ID: "old3", State: "exited", Labels: svc, Created: now.Add(-15 * time.Second).Unix()},
		{ID: "cur", State: "running", Labels: svc, Created: now.Add(-2 * time.Second).Unix()},
		// A long-lived task of another service is left alone.
		{ID: "other", State: "running", Labels: map[string]string{swarmServiceLabel: "svc2"}, Created: now.Add(-time.Hour).Unix()},
		// Old exits (a rollout last week) do not count.
		{ID: "stale", State: "exited", Labels: map[string]string{swarmServiceLabel: "svc2"}, Created: now.Add(-time.Hour).Unix()},
	}
	snaps := make([]*pb.ContainerSnapshot, len(summaries))
	for i, s := range summaries {
		snaps[i] = &pb.ContainerSnapshot{ContainerId: s.ID}
	}
	markSwarmLoops(summaries, snaps, now)
	for i, s := range snaps {
		want := summaries[i].ID == "cur"
		if s.CrashedLoop != want {
			t.Errorf("%s crashed_loop=%v, want %v", s.ContainerId, s.CrashedLoop, want)
		}
	}
	if snaps[3].RecentRestarts != 3 {
		t.Errorf("recent_restarts = %d, want 3", snaps[3].RecentRestarts)
	}
}

// lastCount and restartLog are keyed by container id and grew forever.
func TestForgetGoneDropsRemovedContainers(t *testing.T) {
	c := newContainerCollector(nil, time.Second)
	now := time.Now()
	c.recordRestarts("keep", 1, now)
	c.recordRestarts("gone", 1, now)
	c.recordRestarts("gone", 5, now)
	c.forgetGone(map[string]containerSummary{"keep": {}})
	if _, ok := c.lastCount["gone"]; ok {
		t.Error("lastCount kept a removed container")
	}
	if _, ok := c.restartLog["gone"]; ok {
		t.Error("restartLog kept a removed container")
	}
	if _, ok := c.lastCount["keep"]; !ok {
		t.Error("lastCount lost a present container")
	}
}
