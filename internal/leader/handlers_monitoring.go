package leader

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/Bartis-Dev/LabExtend/internal/discord"
	"github.com/Bartis-Dev/LabExtend/internal/notify"
)

// MonitoringDeps groups what every monitoring/webhook handler needs.
type MonitoringDeps struct {
	DB         *sql.DB
	Registry   *AgentRegistry
	Metrics    *metricsStore
	Containers *containerStore
	Logs       *logStore
	Swarm      *swarmTopology
	Audit      *AuditLogger
}

// ─── nodes ──────────────────────────────────────────────────────────────────

// NodeView is the JSON shape returned by /api/nodes.
type NodeView struct {
	ID       string `json:"id"`
	Hostname string `json:"hostname"`
	OS       string `json:"os"`
	Arch     string `json:"arch"`
	Version  string `json:"version"`
	Status   string `json:"status"` // online | offline

	// Labels comes from BPM_AGENT_LABELS in the agent's own environment.
	// SwarmLabels comes from the Docker API (Spec.Labels) and is what
	// placement constraints actually match — the two are unrelated, so they
	// are reported separately rather than merged.
	Labels      map[string]string `json:"labels"`
	SwarmLabels map[string]string `json:"swarm_labels,omitempty"`
	SwarmRole   string            `json:"swarm_role,omitempty"`
	SwarmAvail  string            `json:"swarm_availability,omitempty"`

	FirstSeen int64          `json:"first_seen"`
	LastSeen  int64          `json:"last_seen"`
	Metrics   *MetricsSample `json:"metrics,omitempty"`
}

// ListNodes returns every known node (live + historical) joined with current
// metrics.
func (d *MonitoringDeps) ListNodes(w http.ResponseWriter, r *http.Request) {
	views, err := loadNodeViews(r.Context(), d.DB, d.Registry, d.Metrics, d.Swarm)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"nodes": views})
}

func (d *MonitoringDeps) GetNode(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	views, err := loadNodeViews(r.Context(), d.DB, d.Registry, d.Metrics, d.Swarm)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	for _, v := range views {
		if v.ID == id {
			writeJSON(w, http.StatusOK, v)
			return
		}
	}
	writeErr(w, http.StatusNotFound, errors.New("node not found"))
}

// CleanupNodes deletes node rows that are currently offline (no live agent
// in the registry) AND whose last_seen is older than `olderThanHours`. Removing
// every offline node needs an explicit {"all": true}. Cascading FKs handle the
// linked tables (node_metrics, node_metric_buckets, node_metric_samples,
// node_paths, cronjobs, container_state); container_log_lines and
// backup_run_items reference the node WITHOUT ON DELETE CASCADE, so we wipe
// them explicitly first — otherwise SQLite rejects the node DELETE with
// "FOREIGN KEY constraint failed" (only the DB index is dropped; the actual
// S3 backup objects are untouched).
//
// Body: {"older_than_hours": 24}  →  delete offline nodes idle ≥24h
//
//	{"all": true}             →  delete ALL offline nodes
//
// A missing, broken or zero body used to mean "delete all", so any malformed
// request wiped every offline node's history.
func (d *MonitoringDeps) CleanupNodes(w http.ResponseWriter, r *http.Request) {
	var req struct {
		OlderThanHours int  `json:"older_than_hours"`
		All            bool `json:"all"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	if !req.All && req.OlderThanHours < 1 {
		writeErr(w, http.StatusBadRequest,
			errors.New(`older_than_hours must be at least 1; send {"all": true} to remove every offline node`))
		return
	}
	if req.All {
		req.OlderThanHours = 0
	}

	// Build the set of node_ids that are CURRENTLY OFFLINE (= not in registry)
	// and match the age cutoff. We do this in Go (not pure SQL) because the
	// "online" status is in-memory, not in the DB.
	registryOnline := map[string]bool{}
	for _, c := range d.Registry.List() {
		registryOnline[c.ID] = true
	}

	cutoff := int64(0)
	if req.OlderThanHours > 0 {
		cutoff = time.Now().Add(-time.Duration(req.OlderThanHours) * time.Hour).Unix()
	}

	rows, err := d.DB.QueryContext(r.Context(),
		`SELECT id, hostname, last_seen FROM nodes`)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	type victim struct{ id, hostname string }
	var victims []victim
	for rows.Next() {
		var id, hostname string
		var lastSeen int64
		if err := rows.Scan(&id, &hostname, &lastSeen); err != nil {
			continue
		}
		if registryOnline[id] {
			continue // online → keep
		}
		if cutoff > 0 && lastSeen > cutoff {
			continue // recently seen → keep
		}
		victims = append(victims, victim{id: id, hostname: hostname})
	}
	rows.Close()

	if len(victims) == 0 {
		d.Audit.Log(r.Context(), r, "node.cleanup", "node", "",
			map[string]any{"count": 0, "older_than_hours": req.OlderThanHours, "all": req.All})
		writeJSON(w, http.StatusOK, map[string]any{"deleted": 0, "ids": []string{}})
		return
	}

	tx, err := d.DB.BeginTx(r.Context(), nil)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	defer tx.Rollback() //nolint:errcheck

	ids := make([]string, 0, len(victims))
	for _, v := range victims {
		// container_log_lines has no FK reference → explicit cleanup.
		if _, err := tx.ExecContext(r.Context(),
			`DELETE FROM container_log_lines WHERE node_id = ?`, v.id); err != nil {
			writeErr(w, http.StatusInternalServerError, err)
			return
		}
		// backup_run_items references nodes(id) WITHOUT ON DELETE CASCADE, so
		// the node DELETE would fail with a FK error while backup history for
		// this node still exists → wipe those rows first.
		if _, err := tx.ExecContext(r.Context(),
			`DELETE FROM backup_run_items WHERE node_id = ?`, v.id); err != nil {
			writeErr(w, http.StatusInternalServerError, err)
			return
		}
		// nodes DELETE cascades to: node_metrics, node_metric_buckets,
		// node_metric_samples, node_paths, cronjobs, container_state.
		if _, err := tx.ExecContext(r.Context(),
			`DELETE FROM nodes WHERE id = ?`, v.id); err != nil {
			writeErr(w, http.StatusInternalServerError, err)
			return
		}
		ids = append(ids, v.id)
	}
	if err := tx.Commit(); err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}

	// Drop live-state for these nodes too (best-effort).
	for _, v := range victims {
		d.Metrics.MarkOffline(v.id)
		d.Containers.MarkNodeOffline(v.id)
	}

	d.Audit.Log(r.Context(), r, "node.cleanup", "node", "",
		map[string]any{"count": len(ids), "ids": ids, "older_than_hours": req.OlderThanHours, "all": req.All})
	writeJSON(w, http.StatusOK, map[string]any{
		"deleted":          len(ids),
		"ids":              ids,
		"older_than_hours": req.OlderThanHours,
	})
}

// NodeSamples returns raw per-heartbeat samples for one node. Used by the
// UI for sub-minute granularity (5m + 1h time-windows). Default last 5 min.
//
//	?since_ms=<ms>  ?until_ms=<ms>   (both inclusive)
func (d *MonitoringDeps) NodeSamples(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	now := time.Now().UnixMilli()
	since := now - 5*60*1000
	until := now
	if v := r.URL.Query().Get("since_ms"); v != "" {
		if n, err := strconv.ParseInt(v, 10, 64); err == nil {
			since = n
		}
	}
	if v := r.URL.Query().Get("until_ms"); v != "" {
		if n, err := strconv.ParseInt(v, 10, 64); err == nil {
			until = n
		}
	}
	samples, err := d.Metrics.SamplesForNode(r.Context(), id, since, until)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"samples":  samples,
		"since_ms": since,
		"until_ms": until,
	})
}

// NodeHistory returns minute-bucket averages for the requested window
// (default last 60 minutes).
func (d *MonitoringDeps) NodeHistory(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	now := time.Now().Unix()
	since := now - 3600
	until := now
	if v := r.URL.Query().Get("since"); v != "" {
		if n, err := strconv.ParseInt(v, 10, 64); err == nil {
			since = n
		}
	}
	if v := r.URL.Query().Get("until"); v != "" {
		if n, err := strconv.ParseInt(v, 10, 64); err == nil {
			until = n
		}
	}
	buckets, err := d.Metrics.BucketsForNode(r.Context(), id, since, until)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"buckets": buckets, "since": since, "until": until})
}

// loadNodeViews joins DB nodes with live registry + metrics state.
func loadNodeViews(ctx context.Context, db *sql.DB, reg *AgentRegistry, metrics *metricsStore, swarm *swarmTopology) ([]*NodeView, error) {
	if db == nil {
		return nil, nil
	}
	rows, err := db.QueryContext(ctx, `
		SELECT id, hostname, COALESCE(os,''), COALESCE(arch,''), COALESCE(version,''),
		       labels_json, first_seen, last_seen, status
		FROM nodes ORDER BY hostname
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []*NodeView{}
	for rows.Next() {
		var v NodeView
		var labelsJSON string
		if err := rows.Scan(&v.ID, &v.Hostname, &v.OS, &v.Arch, &v.Version,
			&labelsJSON, &v.FirstSeen, &v.LastSeen, &v.Status); err != nil {
			return nil, err
		}
		if labelsJSON != "" {
			_ = json.Unmarshal([]byte(labelsJSON), &v.Labels)
		}
		if _, ok := reg.Get(v.ID); ok {
			v.Status = "online"
		} else {
			v.Status = "offline"
		}
		if m := metrics.Current(v.ID); m != nil {
			v.Metrics = m
		}
		if sn := swarm.lookup(ctx, v.ID, v.Hostname); sn != nil {
			v.SwarmLabels = sn.GetLabels()
			v.SwarmRole = sn.GetRole()
			v.SwarmAvail = sn.GetAvailability()
			if sn.GetLeader() {
				v.SwarmRole = "manager (leader)"
			}
		}
		out = append(out, &v)
	}
	return out, rows.Err()
}

// ─── containers ─────────────────────────────────────────────────────────────

func (d *MonitoringDeps) ListContainers(w http.ResponseWriter, r *http.Request) {
	views, err := d.Containers.All(r.Context())
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"containers": views})
}

func (d *MonitoringDeps) GetContainer(w http.ResponseWriter, r *http.Request) {
	node := chi.URLParam(r, "node")
	id := chi.URLParam(r, "id")
	v := d.Containers.Get(r.Context(), node, id)
	if v == nil {
		writeErr(w, http.StatusNotFound, errors.New("container not found"))
		return
	}
	writeJSON(w, http.StatusOK, v)
}

// ContainerLogTail returns the most recent N persisted lines for one container.
func (d *MonitoringDeps) ContainerLogTail(w http.ResponseWriter, r *http.Request) {
	node := chi.URLParam(r, "node")
	id := chi.URLParam(r, "id")
	limit := 500
	if v := r.URL.Query().Get("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 && n < 10000 {
			limit = n
		}
	}
	lines, err := d.Logs.Tail(r.Context(), node, id, limit)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"lines": lines})
}

// ContainerLogStream upgrades to a one-shot streaming response (chunked HTTP
// text/event-stream) — the simplest live-tail without pulling in a WebSocket
// library. Browser side uses EventSource.
//
// Output is one SSE event per line:
//
//	event: log
//	data: {"ts_ms": …, "stream": "stdout", "line": "…"}
//
// On connect we first emit the persisted tail (so the user sees backlog
// instantly), then live lines as they arrive.
func (d *MonitoringDeps) ContainerLogStream(w http.ResponseWriter, r *http.Request) {
	node := chi.URLParam(r, "node")
	id := chi.URLParam(r, "id")

	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")

	tail, _ := d.Logs.Tail(r.Context(), node, id, 500)

	ch, cancel := d.Logs.Subscribe(node, id, 256)
	defer cancel()

	// Send persisted tail first.
	fmt.Fprintf(w, "event: ready\ndata: {\"tail\":%d}\n\n", len(tail))
	flusher.Flush()
	for _, e := range tail {
		writeLogEvent(w, e)
	}
	flusher.Flush()

	heartbeat := time.NewTicker(15 * time.Second)
	defer heartbeat.Stop()

	for {
		select {
		case <-r.Context().Done():
			return
		case e, ok := <-ch:
			if !ok {
				return
			}
			writeLogEvent(w, e)
			flusher.Flush()
		case <-heartbeat.C:
			fmt.Fprintf(w, ": keep-alive\n\n")
			flusher.Flush()
		}
	}
}

func writeLogEvent(w http.ResponseWriter, e *LogEntry) {
	b, err := json.Marshal(e)
	if err != nil {
		return
	}
	fmt.Fprintf(w, "event: log\ndata: %s\n\n", b)
}

// ─── webhooks ───────────────────────────────────────────────────────────────

type WebhookConfig struct {
	ID         string   `json:"id"`
	Name       string   `json:"name"`
	Kind       string   `json:"kind"` // "discord" for v1
	URL        string   `json:"url,omitempty"`
	Enabled    bool     `json:"enabled"`
	EventTypes []string `json:"event_types"`
	CreatedAt  int64    `json:"created_at"`
	UpdatedAt  int64    `json:"updated_at"`
}

type webhookReq struct {
	Name       string   `json:"name"`
	Kind       string   `json:"kind"`
	URL        string   `json:"url"`
	Enabled    bool     `json:"enabled"`
	EventTypes []string `json:"event_types"`
}

// eventTypesJSON validates the requested subscriptions and encodes them for
// webhook_configs.event_types.
func eventTypesJSON(types []string) (string, error) {
	out := []string{}
	for _, t := range types {
		if !notify.ValidType(t) {
			return "", fmt.Errorf("unknown event type %q", t)
		}
		out = append(out, t)
	}
	b, err := json.Marshal(out)
	if err != nil {
		return "", fmt.Errorf("encode event types: %w", err)
	}
	return string(b), nil
}

func (d *MonitoringDeps) ListWebhooks(w http.ResponseWriter, r *http.Request) {
	rows, err := d.DB.QueryContext(r.Context(), `
		SELECT id, name, kind, url, enabled, event_types, created_at, updated_at
		FROM webhook_configs ORDER BY name
	`)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	defer rows.Close()
	out := []WebhookConfig{}
	for rows.Next() {
		var c WebhookConfig
		var enabled int
		var types string
		if err := rows.Scan(&c.ID, &c.Name, &c.Kind, &c.URL, &enabled, &types,
			&c.CreatedAt, &c.UpdatedAt); err != nil {
			writeErr(w, http.StatusInternalServerError, err)
			return
		}
		c.Enabled = enabled == 1
		c.EventTypes = notify.ParseTypes(types)
		c.URL = maskURL(c.URL)
		out = append(out, c)
	}
	writeJSON(w, http.StatusOK, map[string]any{"webhooks": out})
}

func (d *MonitoringDeps) CreateWebhook(w http.ResponseWriter, r *http.Request) {
	var req webhookReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	if req.Name == "" || req.URL == "" {
		writeErr(w, http.StatusBadRequest, errors.New("name + url required"))
		return
	}
	if req.Kind == "" {
		req.Kind = "discord"
	}
	if err := discord.ValidWebhookURL(req.URL); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	types, err := eventTypesJSON(req.EventTypes)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	id := uuid.NewString()
	now := time.Now().Unix()
	_, err = d.DB.ExecContext(r.Context(), `
		INSERT INTO webhook_configs (id, name, kind, url, enabled, event_types, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)
	`, id, req.Name, req.Kind, req.URL, boolI(req.Enabled), types, now, now)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	d.Audit.Log(r.Context(), r, "webhook.create", "webhook", id,
		map[string]any{"name": req.Name, "url": maskURL(req.URL), "event_types": req.EventTypes})
	writeJSON(w, http.StatusOK, map[string]any{"id": id})
}

func (d *MonitoringDeps) UpdateWebhook(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	var req webhookReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	keepURL := req.URL == "" || isMaskedURL(req.URL)
	if !keepURL {
		if err := discord.ValidWebhookURL(req.URL); err != nil {
			writeErr(w, http.StatusBadRequest, err)
			return
		}
	}
	types, err := eventTypesJSON(req.EventTypes)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	// If URL is empty or masked-shape, keep the existing one.
	var res sql.Result
	if keepURL {
		res, err = d.DB.ExecContext(r.Context(), `
			UPDATE webhook_configs SET name = ?, kind = ?, enabled = ?, event_types = ?, updated_at = ?
			WHERE id = ?
		`, req.Name, req.Kind, boolI(req.Enabled), types, time.Now().Unix(), id)
	} else {
		res, err = d.DB.ExecContext(r.Context(), `
			UPDATE webhook_configs SET name = ?, kind = ?, url = ?, enabled = ?, event_types = ?, updated_at = ?
			WHERE id = ?
		`, req.Name, req.Kind, req.URL, boolI(req.Enabled), types, time.Now().Unix(), id)
	}
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		writeErr(w, http.StatusNotFound, errors.New("webhook not found"))
		return
	}
	d.Audit.Log(r.Context(), r, "webhook.update", "webhook", id,
		map[string]any{"name": req.Name, "url_changed": !keepURL, "enabled": req.Enabled, "event_types": req.EventTypes})
	writeJSON(w, http.StatusOK, map[string]any{"updated": n})
}

func (d *MonitoringDeps) DeleteWebhook(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	res, err := d.DB.ExecContext(r.Context(), `DELETE FROM webhook_configs WHERE id = ?`, id)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		writeErr(w, http.StatusNotFound, errors.New("webhook not found"))
		return
	}
	d.Audit.Log(r.Context(), r, "webhook.delete", "webhook", id, nil)
	writeJSON(w, http.StatusOK, map[string]any{"deleted": n})
}

// TestWebhook posts a one-shot probe payload and reports Discord's answer, so
// the user can verify the URL from the UI.
func (d *MonitoringDeps) TestWebhook(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	var url string
	if err := d.DB.QueryRowContext(r.Context(),
		`SELECT url FROM webhook_configs WHERE id = ?`, id).Scan(&url); err != nil {
		writeErr(w, http.StatusNotFound, errors.New("webhook not found"))
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	p := discord.Payload{
		Username: "LabExtend",
		Embeds: []discord.Embed{{
			Title:     "Webhook test · success",
			Color:     discord.ColorBlue,
			Timestamp: time.Now().UTC().Format(time.RFC3339),
			Footer:    &discord.Footer{Text: "LabExtend"},
		}},
	}
	err := discord.NewClient(url).Post(ctx, p)
	d.Audit.Log(r.Context(), r, "webhook.test", "webhook", id, map[string]any{"ok": err == nil})
	if err != nil {
		slog.Warn("webhook test failed", "id", id, "err", err)
		writeErr(w, http.StatusBadGateway, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"sent": true})
}

// ─── helpers ────────────────────────────────────────────────────────────────

func boolI(b bool) int {
	if b {
		return 1
	}
	return 0
}

// maskURL returns "discord://…/<lastN>" — enough to identify but not leak.
func maskURL(u string) string {
	if u == "" {
		return ""
	}
	if len(u) < 16 {
		return "•••"
	}
	return u[:14] + "…" + u[len(u)-8:]
}

func isMaskedURL(u string) bool {
	for _, r := range u {
		if r == 0x2026 { // … character
			return true
		}
	}
	return false
}
