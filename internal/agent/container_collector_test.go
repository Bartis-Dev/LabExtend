package agent

import (
	"testing"
	"time"
)

// Docker's RestartCount covers the container's whole life. Seeing it for the
// first time — every agent start — must not look like that many restarts in
// the last minute, or each rollout raises a false container_crashed alert.
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
