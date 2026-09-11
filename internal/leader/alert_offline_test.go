package leader

import (
	"context"
	"database/sql"
	"testing"
	"time"

	_ "modernc.org/sqlite"
)

func alertDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", "file:"+t.TempDir()+"/a.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	for _, q := range []string{
		`CREATE TABLE nodes (id TEXT PRIMARY KEY, last_seen INTEGER NOT NULL, labels_json TEXT)`,
		`CREATE TABLE alert_history (id INTEGER PRIMARY KEY AUTOINCREMENT, rule_id TEXT,
			node_id TEXT, container_id TEXT, fired_at INTEGER, state TEXT, value REAL, message TEXT)`,
	} {
		if _, err := db.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	return db
}

func addNode(t *testing.T, db *sql.DB, id string, lastSeen time.Time, labels string) {
	t.Helper()
	if _, err := db.Exec(`INSERT INTO nodes (id, last_seen, labels_json) VALUES (?, ?, ?)`,
		id, lastSeen.Unix(), labels); err != nil {
		t.Fatal(err)
	}
}

func offlineRule(scope string) AlertRule {
	return AlertRule{ID: "r", Name: "offline", Kind: string(AlertNodeOffline),
		Comparator: ">", Threshold: 60, Scope: scope}
}

func triggered(e *AlertEngine, nodeID string) bool {
	st, ok := e.state["r|node:"+nodeID]
	return ok && st.triggered
}

// The rule iterated the registry, which only holds connected agents — a node
// leaves it the moment its stream ends — so it could never see an offline
// node, and a dead node produced no alert at all.
func TestNodeOfflineFiresForADisconnectedNode(t *testing.T) {
	db := alertDB(t)
	reg := NewAgentRegistry()
	reg.Register(&AgentConn{ID: "online"})

	tenMinAgo := time.Now().Add(-10 * time.Minute)
	addNode(t, db, "gone", tenMinAgo, `{}`)
	addNode(t, db, "online", tenMinAgo, `{}`) // stale row, but the agent is connected

	e := newAlertEngine(db, nil, nil, reg, nil)
	e.started = time.Now().Add(-time.Hour)
	e.evalNodeOffline(context.Background(), offlineRule("all"))

	if !triggered(e, "gone") {
		t.Fatal("node silent for 10 min did not trigger node_offline")
	}
	if triggered(e, "online") {
		t.Fatal("a connected node was reported offline because of a stale last_seen")
	}
	var n int
	if err := db.QueryRow(`SELECT COUNT(*) FROM alert_history WHERE node_id = 'gone' AND state = 'triggered'`).Scan(&n); err != nil || n != 1 {
		t.Fatalf("alert_history rows for the offline node: %d (err %v), want 1", n, err)
	}
}

// After a leader restart every node's last_seen is as old as the outage. The
// engine must not count the leader's own downtime against the nodes, or all of
// them alert at once while their agents are seconds from reconnecting.
func TestNodeOfflineDoesNotBlameNodesForTheLeadersOwnOutage(t *testing.T) {
	db := alertDB(t)
	addNode(t, db, "n1", time.Now().Add(-10*time.Minute), `{}`)

	e := newAlertEngine(db, nil, nil, NewAgentRegistry(), nil)
	e.started = time.Now()
	e.evalNodeOffline(context.Background(), offlineRule("all"))

	if triggered(e, "n1") {
		t.Fatal("node alerted for the time the leader itself was down")
	}
}

// Label scopes used to look labels up in the registry, which an offline node
// is no longer in — so a label-scoped offline rule could never match.
func TestNodeOfflineLabelScopeSeesOfflineNodes(t *testing.T) {
	db := alertDB(t)
	old := time.Now().Add(-10 * time.Minute)
	addNode(t, db, "db1", old, `{"role":"db"}`)
	addNode(t, db, "web1", old, `{"role":"web"}`)

	e := newAlertEngine(db, nil, nil, NewAgentRegistry(), nil)
	e.started = time.Now().Add(-time.Hour)
	e.evalNodeOffline(context.Background(), offlineRule("label:role=db"))

	if !triggered(e, "db1") {
		t.Fatal("offline node matching the label scope did not trigger")
	}
	if triggered(e, "web1") {
		t.Fatal("node outside the label scope triggered")
	}
}
