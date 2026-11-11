package leader

import (
	"context"
	"database/sql"
	"encoding/json"
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

	// Each only after it succeeded: a failed first container listing used to
	// count as seeded, and the next tick announced every existing crash loop.
	nodesSeeded      bool
	containersSeeded bool
	nodes            map[string]*nodePresence
	crashed          map[string]bool // crash key -> crashed_loop at the last tick
}

// downStateKey holds the nodes announced offline (id -> down since, unix) in
// system_state. The notification prune may delete the offline event of a node
// that is still down, so notifications alone cannot restore that state.
const downStateKey = "events.down_nodes"

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
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("load nodes: %w", err)
	}

	hosts := make(map[string]string, len(nodes))
	for _, n := range nodes {
		hosts[n.id] = n.hostname
	}
	if !w.nodesSeeded {
		// Judging presence without the previous leader's state would repeat
		// its offline events or swallow recoveries; retry on the next tick.
		if err := w.seedNodes(ctx); err != nil {
			return hosts, fmt.Errorf("restore node state: %w", err)
		}
		w.nodesSeeded = true
	}
	now := w.now()
	changed := false
	defer func() {
		if changed {
			w.saveDown(ctx)
		}
	}()
	for _, n := range nodes {
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
				changed = true
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
			changed = true
			w.emitOffline(ctx, n, gone)
		}
	}
	for id := range w.nodes {
		if _, ok := hosts[id]; !ok {
			delete(w.nodes, id) // removed via node cleanup
			changed = true
		}
	}
	return hosts, nil
}

// seedNodes restores which nodes were last announced offline, so a leader
// restart neither repeats an offline event nor swallows the recovery. The
// notifications are only the fallback for a leader that never saved the
// state (the first start after the upgrade).
func (w *eventWatcher) seedNodes(ctx context.Context) error {
	var raw string
	err := w.db.QueryRowContext(ctx, `SELECT value FROM system_state WHERE key = ?`, downStateKey).Scan(&raw)
	if err == nil {
		var down map[string]int64
		if err := json.Unmarshal([]byte(raw), &down); err != nil {
			return fmt.Errorf("decode %s: %w", downStateKey, err)
		}
		for id, since := range down {
			w.nodes[id] = &nodePresence{down: true, downSince: time.Unix(since, 0)}
		}
		return nil
	}
	if err != sql.ErrNoRows {
		return err
	}
	rows, err := w.db.QueryContext(ctx, `
		SELECT json_extract(details_json, '$.node_id'), type, ts FROM notifications
		 WHERE type IN (?, ?) ORDER BY id`, notify.NodeOffline, notify.NodeOnline)
	if err != nil {
		return err
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
	return rows.Err()
}

// saveDown persists the nodes currently announced offline.
func (w *eventWatcher) saveDown(ctx context.Context) {
	down := map[string]int64{}
	for id, st := range w.nodes {
		if st.down {
			down[id] = st.downSince.Unix()
		}
	}
	b, _ := json.Marshal(down)
	if _, err := w.db.ExecContext(ctx, `
		INSERT INTO system_state (key, value) VALUES (?, ?)
		ON CONFLICT(key) DO UPDATE SET value = excluded.value`, downStateKey, string(b)); err != nil {
		slog.Warn("events: saving node state failed", "err", err)
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

// crashKey groups the task containers of one Swarm service on one node:
// Swarm replaces a crashing task with a new container every few seconds, and
// each new id would otherwise be a crash loop of its own.
func crashKey(v *ContainerView) string {
	if svc := v.Labels["com.docker.swarm.service.id"]; svc != "" {
		return key(v.NodeID, "service:"+svc)
	}
	return key(v.NodeID, v.ContainerID)
}

// checkContainers emits once when a container's (or Swarm service's)
// crashed_loop flips to true. The first successful listing only records the
// current state: those loops were announced by the leader that ran before
// this one.
func (w *eventWatcher) checkContainers(ctx context.Context, hosts map[string]string) {
	if w.cnts == nil {
		return
	}
	views, err := w.cnts.All(ctx)
	if err != nil {
		slog.Warn("events: loading containers failed", "err", err)
		return
	}
	seeded := w.containersSeeded
	w.containersSeeded = true
	seen := make(map[string]bool, len(views))
	crashedNow := map[string]*ContainerView{}
	for _, v := range views {
		k := crashKey(v)
		seen[k] = true
		if v.CrashedLoop && crashedNow[k] == nil {
			crashedNow[k] = v
		}
	}
	for k := range seen {
		v := crashedNow[k]
		was := w.crashed[k]
		w.crashed[k] = v != nil
		if v == nil || was || !seeded {
			continue
		}
		host := hosts[v.NodeID]
		if host == "" {
			host = v.NodeID
		}
		w.events.Emit(ctx, notify.Event{
			Type: notify.ContainerCrashloop, Severity: notify.SeverityCritical,
			Title: "Container crash-looping · " + v.Name,
			Body: fmt.Sprintf("%s (%s) on %s restarted %d times in the last few minutes.",
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
