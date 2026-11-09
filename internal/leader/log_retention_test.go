package leader

import (
	"context"
	"database/sql"
	"fmt"
	"testing"
	"time"

	pb "github.com/Bartis-Dev/LabExtend/internal/grpc/pb"
)

func countLogs(t *testing.T, d *sql.DB, cid string) int {
	t.Helper()
	var n int
	if err := d.QueryRow(`SELECT COUNT(*) FROM container_log_lines WHERE container_id = ?`, cid).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func addLogs(t *testing.T, ls *logStore, node, cid string, n int, at time.Time) {
	t.Helper()
	b := &pb.LogBatch{}
	for i := 0; i < n; i++ {
		b.Lines = append(b.Lines, &pb.LogLine{ContainerId: cid, Stream: "stdout", TsMs: at.UnixMilli(), Line: fmt.Sprint(i)})
	}
	ls.Apply(context.Background(), node, b)
}

func TestLogRetention(t *testing.T) {
	ctx := context.Background()
	d := analyticsDB(t)
	now := time.Now()
	if _, err := d.Exec(`INSERT INTO nodes (id, hostname, first_seen, last_seen) VALUES ('n1', 'n1', 0, 0)`); err != nil {
		t.Fatal(err)
	}

	cs := newContainerStore(d)
	cs.Apply(ctx, "n1", &pb.ContainerReport{Containers: []*pb.ContainerSnapshot{
		{ContainerId: "live", State: "running"},
		{ContainerId: "fresh-stop", State: "exited", FinishedAtMs: now.Add(-time.Hour).UnixMilli()},
		{ContainerId: "old-stop", State: "exited", FinishedAtMs: now.Add(-49 * time.Hour).UnixMilli()},
	}})

	ls := newLogStore(d, 5000)
	old := now.Add(-2 * time.Hour)
	for _, cid := range []string{"live", "fresh-stop", "old-stop", "removed"} {
		addLogs(t, ls, "n1", cid, 10, old)
	}
	addLogs(t, ls, "n1", "brand-new", 10, now) // logs before its first report

	ls.pruneOrphans(ctx, now)

	want := map[string]int{"live": 10, "fresh-stop": 10, "old-stop": 0, "removed": 0, "brand-new": 10}
	for cid, n := range want {
		if got := countLogs(t, d, cid); got != n {
			t.Errorf("%s: %d lines, want %d", cid, got, n)
		}
	}

	// Per-container cap: never pruned below 5000, never above 5000+batch.
	for i := 0; i < 7; i++ {
		addLogs(t, ls, "n1", "live", 1000, now)
	}
	time.Sleep(200 * time.Millisecond) // prune runs in a goroutine
	if got := countLogs(t, d, "live"); got < 5000 || got > 6000 {
		t.Errorf("live after 7010 lines: %d, want 5000..6000", got)
	}
}
