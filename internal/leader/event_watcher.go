package leader

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"net/url"
	"time"

	"github.com/Bartis-Dev/LabExtend/internal/notify"
)

const (
	// offlineAfter is how long a node may go without a heartbeat before it is
	// announced offline. Agent reconnects after a leader or network blip take
	// a few seconds and must stay quiet.
	offlineAfter = 60 * time.Second
	// onlineAfter is how long an offline node must stay connected before it
	// is announced back, so a node that keeps dropping is reported once.
	onlineAfter = 30 * time.Second
)

// eventWatcher turns node presence and container reports into node.offline,
// node.online and container.crashloop events. It polls rather than hooking
// the registry: a node that never reconnects produces no callback at all.
type eventWatcher struct {
	db     *sql.DB
	reg    *AgentRegistry
	cnts   *containerStore
	events *notify.Center
	now    func() time.Time

	// started bounds how long a node counts as gone: after a leader restart
	// every node's last_seen is as old as the outage, and each would be
	// announced although its agent reconnects seconds later.
	started time.Time

	seeded  bool
	nodes   map[string]*nodePresence
	crashed map[string]bool // container key -> crashed_loop at the last tick
}

type nodePresence struct {
	down        bool
	downSince   time.Time
	onlineSince time.Time // zero while disconnected
}

func newEventWatcher(db *sql.DB, reg *AgentRegistry, cnts *containerStore, events *notify.Center) *eventWatcher {
	return &eventWatcher{
		db: db, reg: reg, cnts: cnts, events: events, now: time.Now,
		nodes: map[string]*nodePresence{}, crashed: map[string]bool{},
	}
}

func (w *eventWatcher) Run(ctx context.Context) {
	w.started = w.now()
	t := time.NewTicker(5 * time.Second)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			w.tick(ctx)
		}
	}
}

func (w *eventWatcher) tick(ctx context.Context) {
	hosts, err := w.checkNodes(ctx)
	if err != nil {
		slog.Warn("events: checking nodes failed", "err", err)
	}
	w.checkContainers(ctx, hosts)
	w.seeded = true
}

type knownNode struct {
	id, hostname string
	lastSeen     int64
}

// checkNodes returns id -> hostname for every known node.
func (w *eventWatcher) checkNodes(ctx context.Context) (map[string]string, error) {
	rows, err := w.db.QueryContext(ctx, `SELECT id, hostname, last_seen FROM nodes`)
	if err != nil {
		return nil, fmt.Errorf("load nodes: %w", err)
	}
	var nodes []knownNode
	for rows.Next() {
		var n knownNode
		if err := rows.Scan(&n.id, &n.hostname, &n.lastSeen); err != nil {
			_ = rows.Close()
			return nil, fmt.Errorf("scan node: %w", err)
		}
		nodes = append(nodes, n)
	}
	_ = rows.Close()

	if !w.seeded {
		w.seedNodes(ctx)
	}
	now := w.now()
	hosts := make(map[string]string, len(nodes))
	for _, n := range nodes {
		hosts[n.id] = n.hostname
		st := w.nodes[n.id]
		if st == nil {
			st = &nodePresence{}
			w.nodes[n.id] = st
		}
		// The registry, not nodes.status: the row stays 'online' when the
		// leader itself dies, and only the registry knows who is connected.
		if _, online := w.reg.Get(n.id); online {
			if st.onlineSince.IsZero() {
				st.onlineSince = now
			}
			if st.down && now.Sub(st.onlineSince) >= onlineAfter {
				st.down = false
				w.emitOnline(ctx, n, now.Sub(st.downSince))
			}
			continue
		}
		st.onlineSince = time.Time{}
		if st.down {
			continue
		}
		seen := time.Unix(n.lastSeen, 0)
		if seen.Before(w.started) {
			seen = w.started
		}
		if gone := now.Sub(seen); gone >= offlineAfter {
			st.down, st.downSince = true, seen
			w.emitOffline(ctx, n, gone)
		}
	}
	for id := range w.nodes {
		if _, ok := hosts[id]; !ok {
			delete(w.nodes, id) // removed via node cleanup
		}
	}
	return hosts, nil
}

// seedNodes restores which nodes were last announced offline, so a leader
// restart neither repeats an offline event nor swallows the recovery.
func (w *eventWatcher) seedNodes(ctx context.Context) {
	rows, err := w.db.QueryContext(ctx, `
		SELECT json_extract(details_json, '$.node_id'), type, ts FROM notifications
		 WHERE type IN (?, ?) ORDER BY id`, notify.NodeOffline, notify.NodeOnline)
	if err != nil {
		slog.Warn("events: restoring node state failed", "err", err)
		return
	}
	defer rows.Close()
	for rows.Next() {
		var id sql.NullString
		var typ string
		var ts int64
		if rows.Scan(&id, &typ, &ts) != nil || !id.Valid {
			continue
		}
		w.nodes[id.String] = &nodePresence{down: typ == notify.NodeOffline, downSince: time.Unix(ts, 0)}
	}
}

func (w *eventWatcher) emitOffline(ctx context.Context, n knownNode, gone time.Duration) {
	w.events.Emit(ctx, notify.Event{
		Type: notify.NodeOffline, Severity: notify.SeverityCritical,
		Title: "Node offline · " + n.hostname,
		Body: fmt.Sprintf("No heartbeat from %s for %s (last seen %s).",
			n.hostname, gone.Round(time.Second), time.Unix(n.lastSeen, 0).UTC().Format(time.RFC3339)),
		Link:    "/node?id=" + url.QueryEscape(n.id),
		Details: map[string]any{"node_id": n.id, "hostname": n.hostname, "last_seen": n.lastSeen},
	})
}

func (w *eventWatcher) emitOnline(ctx context.Context, n knownNode, down time.Duration) {
	w.events.Emit(ctx, notify.Event{
		Type: notify.NodeOnline, Severity: notify.SeverityInfo,
		Title:   "Node back online · " + n.hostname,
		Body:    fmt.Sprintf("%s is connected again after %s offline.", n.hostname, down.Round(time.Second)),
		Link:    "/node?id=" + url.QueryEscape(n.id),
		Details: map[string]any{"node_id": n.id, "hostname": n.hostname, "offline_sec": int64(down.Seconds())},
	})
}

// checkContainers emits once when a container's crashed_loop flips to true.
// The first tick only records the current state: those loops were announced
// by the leader that ran before this one.
func (w *eventWatcher) checkContainers(ctx context.Context, hosts map[string]string) {
	if w.cnts == nil {
		return
	}
	views, err := w.cnts.All(ctx)
	if err != nil {
		slog.Warn("events: loading containers failed", "err", err)
		return
	}
	seen := make(map[string]bool, len(views))
	for _, v := range views {
		k := key(v.NodeID, v.ContainerID)
		seen[k] = true
		was := w.crashed[k]
		w.crashed[k] = v.CrashedLoop
		if !v.CrashedLoop || was || !w.seeded {
			continue
		}
		host := hosts[v.NodeID]
		if host == "" {
			host = v.NodeID
		}
		w.events.Emit(ctx, notify.Event{
			Type: notify.ContainerCrashloop, Severity: notify.SeverityCritical,
			Title: "Container crash-looping · " + v.Name,
			Body: fmt.Sprintf("%s (%s) on %s restarted %d times in the last minute.",
				v.Name, v.Image, host, v.RecentRestarts),
			Link: "/container?node=" + url.QueryEscape(v.NodeID) + "&id=" + url.QueryEscape(v.ContainerID),
			Details: map[string]any{
				"node_id": v.NodeID, "hostname": host, "container_id": v.ContainerID,
				"name": v.Name, "image": v.Image, "recent_restarts": v.RecentRestarts,
			},
		})
	}
	for k := range w.crashed {
		if !seen[k] {
			delete(w.crashed, k)
		}
	}
}
