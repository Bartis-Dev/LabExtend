package leader

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"github.com/Bartis-Dev/LabExtend/internal/db"
	pb "github.com/Bartis-Dev/LabExtend/internal/grpc/pb"
	"github.com/Bartis-Dev/LabExtend/internal/notify"
)

func watcherDB(t *testing.T) *sql.DB {
	t.Helper()
	ctx := context.Background()
	d, err := db.Open(ctx, t.TempDir()+"/w.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = d.Close() })
	if err := db.Migrate(ctx, d); err != nil {
		t.Fatal(err)
	}
	return d
}

func addNode(t *testing.T, d *sql.DB, id string, lastSeen time.Time) {
	t.Helper()
	if _, err := d.Exec(`INSERT INTO nodes (id, hostname, os, arch, version, labels_json, first_seen, last_seen, status)
		VALUES (?, ?, 'linux', 'amd64', 'v', '{}', 0, ?, 'online')`, id, id+"-host", lastSeen.Unix()); err != nil {
		t.Fatal(err)
	}
}

func setLastSeen(t *testing.T, d *sql.DB, id string, ts time.Time) {
	t.Helper()
	if _, err := d.Exec(`UPDATE nodes SET last_seen = ? WHERE id = ?`, ts.Unix(), id); err != nil {
		t.Fatal(err)
	}
}

func eventTypes(t *testing.T, d *sql.DB) []string {
	t.Helper()
	rows, err := d.Query(`SELECT type FROM notifications ORDER BY id`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	out := []string{}
	for rows.Next() {
		var s string
		_ = rows.Scan(&s)
		out = append(out, s)
	}
	return out
}

type clock struct{ t time.Time }

func (c *clock) now() time.Time          { return c.t }
func (c *clock) advance(d time.Duration) { c.t = c.t.Add(d) }

func newTestWatcher(d *sql.DB, reg *AgentRegistry, cnts *containerStore, c *clock) *eventWatcher {
	w := newEventWatcher(d, reg, cnts, notify.New(d, nil))
	w.now = c.now
	w.started = c.t.Add(-time.Hour)
	return w
}

func TestNodeOfflineThenOnlineWithHysteresis(t *testing.T) {
	d := watcherDB(t)
	c := &clock{t: time.Now()}
	reg := NewAgentRegistry()
	conn := &AgentConn{ID: "n1"}
	reg.Register(conn)
	addNode(t, d, "n1", c.t)
	w := newTestWatcher(d, reg, nil, c)
	ctx := context.Background()

	w.tick(ctx)
	// A short drop below the threshold is not an outage.
	reg.Unregister(conn)
	c.advance(30 * time.Second)
	w.tick(ctx)
	if got := eventTypes(t, d); len(got) != 0 {
		t.Fatalf("30s drop produced %v", got)
	}

	c.advance(40 * time.Second)
	w.tick(ctx)
	c.advance(5 * time.Second)
	w.tick(ctx)
	if got := eventTypes(t, d); len(got) != 1 || got[0] != notify.NodeOffline {
		t.Fatalf("after 70s offline: %v", got)
	}

	// Reconnecting briefly and dropping again must not flap.
	reg.Register(conn)
	setLastSeen(t, d, "n1", c.t)
	w.tick(ctx)
	c.advance(10 * time.Second)
	w.tick(ctx)
	reg.Unregister(conn)
	c.advance(90 * time.Second)
	w.tick(ctx)
	if got := eventTypes(t, d); len(got) != 1 {
		t.Fatalf("flapping produced %v", got)
	}

	// Stable for long enough: one online event.
	reg.Register(conn)
	w.tick(ctx)
	c.advance(onlineAfter)
	w.tick(ctx)
	c.advance(5 * time.Second)
	w.tick(ctx)
	if got := eventTypes(t, d); len(got) != 2 || got[1] != notify.NodeOnline {
		t.Fatalf("after recovery: %v", got)
	}
}

// After a leader restart every node's last_seen is as old as the outage; the
// leader's own downtime must not be blamed on the nodes.
func TestNodeOfflineIgnoresLeadersOwnOutage(t *testing.T) {
	d := watcherDB(t)
	c := &clock{t: time.Now()}
	addNode(t, d, "n1", c.t.Add(-10*time.Minute))
	w := newTestWatcher(d, NewAgentRegistry(), nil, c)
	w.started = c.t

	w.tick(context.Background())
	if got := eventTypes(t, d); len(got) != 0 {
		t.Fatalf("leader restart produced %v", got)
	}
	c.advance(offlineAfter)
	w.tick(context.Background())
	if got := eventTypes(t, d); len(got) != 1 {
		t.Fatalf("node still gone a minute after the restart: %v", got)
	}
}

// A node announced offline before a leader restart must not be announced again
// after it, but its recovery still must be.
func TestNodeStateSurvivesLeaderRestart(t *testing.T) {
	d := watcherDB(t)
	c := &clock{t: time.Now()}
	addNode(t, d, "n1", c.t.Add(-10*time.Minute))
	w := newTestWatcher(d, NewAgentRegistry(), nil, c)
	w.tick(context.Background())
	if got := eventTypes(t, d); len(got) != 1 {
		t.Fatalf("first leader: %v", got)
	}

	reg := NewAgentRegistry()
	w2 := newTestWatcher(d, reg, nil, c)
	c.advance(2 * time.Minute)
	w2.tick(context.Background())
	if got := eventTypes(t, d); len(got) != 1 {
		t.Fatalf("restarted leader re-announced: %v", got)
	}
	reg.Register(&AgentConn{ID: "n1"})
	w2.tick(context.Background())
	c.advance(onlineAfter)
	w2.tick(context.Background())
	if got := eventTypes(t, d); len(got) != 2 || got[1] != notify.NodeOnline {
		t.Fatalf("recovery after restart: %v", got)
	}
}

func report(crashed bool) *pb.ContainerReport {
	return &pb.ContainerReport{Containers: []*pb.ContainerSnapshot{{
		ContainerId: "c1", Name: "web", Image: "nginx", State: "running", CrashedLoop: crashed, RecentRestarts: 4,
	}}}
}

func TestCrashloopEmitsOncePerTransition(t *testing.T) {
	d := watcherDB(t)
	c := &clock{t: time.Now()}
	reg := NewAgentRegistry()
	reg.Register(&AgentConn{ID: "n1"})
	addNode(t, d, "n1", c.t)
	cnts := newContainerStore(d)
	w := newTestWatcher(d, reg, cnts, c)
	ctx := context.Background()

	cnts.Apply(ctx, "n1", report(false))
	w.tick(ctx)
	for i := 0; i < 3; i++ {
		cnts.Apply(ctx, "n1", report(true))
		w.tick(ctx)
	}
	if got := eventTypes(t, d); len(got) != 1 || got[0] != notify.ContainerCrashloop {
		t.Fatalf("three crashed reports: %v", got)
	}
	cnts.Apply(ctx, "n1", report(false))
	w.tick(ctx)
	cnts.Apply(ctx, "n1", report(true))
	w.tick(ctx)
	if got := eventTypes(t, d); len(got) != 2 {
		t.Fatalf("second crash loop: %v", got)
	}
	var link string
	_ = d.QueryRow(`SELECT link FROM notifications ORDER BY id DESC LIMIT 1`).Scan(&link)
	if link != "/container?node=n1&id=c1" {
		t.Fatalf("link = %q", link)
	}
}

// A container already crash-looping when the leader starts was announced by
// the previous leader; the restart must not announce it again.
func TestCrashloopKnownAtStartIsNotReannounced(t *testing.T) {
	d := watcherDB(t)
	c := &clock{t: time.Now()}
	reg := NewAgentRegistry()
	reg.Register(&AgentConn{ID: "n1"})
	addNode(t, d, "n1", c.t)
	cnts := newContainerStore(d)
	cnts.Apply(context.Background(), "n1", report(true))
	w := newTestWatcher(d, reg, cnts, c)

	w.tick(context.Background())
	w.tick(context.Background())
	if got := eventTypes(t, d); len(got) != 0 {
		t.Fatalf("restart re-announced crash loop: %v", got)
	}
}
