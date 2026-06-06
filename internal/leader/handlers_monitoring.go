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
	"sync"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/Bartis-Dev/LabExtend/internal/discord"
)

// MonitoringDeps groups what every monitoring/alerts handler needs.
type MonitoringDeps struct {
	DB         *sql.DB
	Registry   *AgentRegistry
	Metrics    *metricsStore
	Containers *containerStore
	Logs       *logStore
	Alerts     *AlertEngine
}

// ─── nodes ──────────────────────────────────────────────────────────────────

// NodeView is the JSON shape returned by /api/nodes.
type NodeView struct {
	ID           string            `json:"id"`
	Hostname     string            `json:"hostname"`
	OS           string            `json:"os"`
	Arch         string            `json:"arch"`
	Version      string            `json:"version"`
	Status       string            `json:"status"` // online | offline
	Labels       map[string]string `json:"labels"`
	FirstSeen    int64             `json:"first_seen"`
	LastSeen     int64             `json:"last_seen"`
	Metrics      *MetricsSample    `json:"metrics,omitempty"`
}

// ListNodes returns every known node (live + historical) joined with current
// metrics.
func (d *MonitoringDeps) ListNodes(w http.ResponseWriter, r *http.Request) {
	views, err := loadNodeViews(r.Context(), d.DB, d.Registry, d.Metrics)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"nodes": views})
}

func (d *MonitoringDeps) GetNode(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	views, err := loadNodeViews(r.Context(), d.DB, d.Registry, d.Metrics)
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
// in the registry) AND whose last_seen is older than `olderThanHours`. With
// olderThanHours=0 every offline node is removed. Cascading FKs handle the
// linked tables (node_metrics, node_metric_buckets, node_paths, cronjobs,
// container_state); container_log_lines has no FK so we wipe it explicitly.
//
// Body: {"older_than_hours": 24}  →  delete offline nodes idle ≥24h
//       {"older_than_hours": 0}   →  delete ALL offline nodes
func (d *MonitoringDeps) CleanupNodes(w http.ResponseWriter, r *http.Request) {
	var req struct {
		OlderThanHours int `json:"older_than_hours"`
	}
	_ = json.NewDecoder(r.Body).Decode(&req) // empty body OK → treated as 0

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
		// nodes DELETE cascades to: node_metrics, node_metric_buckets,
		// node_paths, cronjobs, container_state.
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

	// Audit log via the alert engine's registry — but we don't have an audit
	// logger here. The handler is admin-only via the router; the action is
	// non-destructive for live nodes (only offline-and-stale rows go).
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
func loadNodeViews(ctx context.Context, db *sql.DB, reg *AgentRegistry, metrics *metricsStore) ([]*NodeView, error) {
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

// ─── alert rules ────────────────────────────────────────────────────────────

type alertRuleReq struct {
	Name        string  `json:"name"`
	Kind        string  `json:"kind"`
	Comparator  string  `json:"comparator"`
	Threshold   float64 `json:"threshold"`
	DurationSec int     `json:"duration_sec"`
	Scope       string  `json:"scope"`
	WebhookID   string  `json:"webhook_id"`
	CooldownSec int     `json:"cooldown_sec"`
	Enabled     bool    `json:"enabled"`
}

func (d *MonitoringDeps) ListAlertRules(w http.ResponseWriter, r *http.Request) {
	rows, err := d.DB.QueryContext(r.Context(), `
		SELECT id, name, kind, comparator, threshold, duration_sec, scope,
		       COALESCE(webhook_id,''), cooldown_sec, enabled, created_at, updated_at
		FROM alert_rules ORDER BY created_at DESC
	`)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	defer rows.Close()
	out := []AlertRule{}
	for rows.Next() {
		var ar AlertRule
		var wid string
		var enabled int
		if err := rows.Scan(&ar.ID, &ar.Name, &ar.Kind, &ar.Comparator, &ar.Threshold,
			&ar.DurationSec, &ar.Scope, &wid, &ar.CooldownSec, &enabled,
			&ar.CreatedAt, &ar.UpdatedAt); err != nil {
			writeErr(w, http.StatusInternalServerError, err)
			return
		}
		ar.Enabled = enabled == 1
		if wid != "" {
			ar.WebhookID = &wid
		}
		out = append(out, ar)
	}
	writeJSON(w, http.StatusOK, map[string]any{"rules": out})
}

func (d *MonitoringDeps) CreateAlertRule(w http.ResponseWriter, r *http.Request) {
	var req alertRuleReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	if req.Name == "" || req.Kind == "" {
		writeErr(w, http.StatusBadRequest, errors.New("name + kind required"))
		return
	}
	if req.Comparator == "" {
		req.Comparator = ">"
	}
	if req.Scope == "" {
		req.Scope = "all"
	}
	if req.CooldownSec == 0 {
		req.CooldownSec = 300
	}
	id := uuid.NewString()
	now := time.Now().Unix()
	var widArg any
	if req.WebhookID != "" {
		widArg = req.WebhookID
	}
	_, err := d.DB.ExecContext(r.Context(), `
		INSERT INTO alert_rules
			(id, name, kind, comparator, threshold, duration_sec, scope,
			 webhook_id, cooldown_sec, enabled, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, id, req.Name, req.Kind, req.Comparator, req.Threshold, req.DurationSec, req.Scope,
		widArg, req.CooldownSec, boolI(req.Enabled), now, now)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"id": id})
}

func (d *MonitoringDeps) UpdateAlertRule(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	var req alertRuleReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	var widArg any
	if req.WebhookID != "" {
		widArg = req.WebhookID
	}
	res, err := d.DB.ExecContext(r.Context(), `
		UPDATE alert_rules SET
			name = ?, kind = ?, comparator = ?, threshold = ?, duration_sec = ?,
			scope = ?, webhook_id = ?, cooldown_sec = ?, enabled = ?, updated_at = ?
		WHERE id = ?
	`, req.Name, req.Kind, req.Comparator, req.Threshold, req.DurationSec,
		req.Scope, widArg, req.CooldownSec, boolI(req.Enabled), time.Now().Unix(), id)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		writeErr(w, http.StatusNotFound, errors.New("rule not found"))
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"updated": n})
}

func (d *MonitoringDeps) DeleteAlertRule(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	res, err := d.DB.ExecContext(r.Context(), `DELETE FROM alert_rules WHERE id = ?`, id)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		writeErr(w, http.StatusNotFound, errors.New("rule not found"))
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"deleted": n})
}

func (d *MonitoringDeps) ListAlertHistory(w http.ResponseWriter, r *http.Request) {
	limit := 100
	if v := r.URL.Query().Get("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 && n < 1000 {
			limit = n
		}
	}
	rows, err := d.DB.QueryContext(r.Context(), `
		SELECT h.id, h.rule_id, COALESCE(r.name,''), COALESCE(h.node_id,''),
		       COALESCE(h.container_id,''), h.fired_at, h.state,
		       COALESCE(h.value,0), h.message
		FROM alert_history h LEFT JOIN alert_rules r ON r.id = h.rule_id
		ORDER BY h.fired_at DESC LIMIT ?
	`, limit)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	defer rows.Close()
	out := []AlertHistoryRow{}
	for rows.Next() {
		var h AlertHistoryRow
		if err := rows.Scan(&h.ID, &h.RuleID, &h.RuleName, &h.NodeID, &h.ContainerID,
			&h.FiredAt, &h.State, &h.Value, &h.Message); err != nil {
			writeErr(w, http.StatusInternalServerError, err)
			return
		}
		out = append(out, h)
	}
	writeJSON(w, http.StatusOK, map[string]any{"history": out})
}

// ─── webhooks ───────────────────────────────────────────────────────────────

type WebhookConfig struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Kind      string `json:"kind"` // "discord" for v1
	URL       string `json:"url,omitempty"`
	Enabled   bool   `json:"enabled"`
	CreatedAt int64  `json:"created_at"`
	UpdatedAt int64  `json:"updated_at"`
}

type webhookReq struct {
	Name    string `json:"name"`
	Kind    string `json:"kind"`
	URL     string `json:"url"`
	Enabled bool   `json:"enabled"`
}

func (d *MonitoringDeps) ListWebhooks(w http.ResponseWriter, r *http.Request) {
	rows, err := d.DB.QueryContext(r.Context(), `
		SELECT id, name, kind, url, enabled, created_at, updated_at
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
		if err := rows.Scan(&c.ID, &c.Name, &c.Kind, &c.URL, &enabled,
			&c.CreatedAt, &c.UpdatedAt); err != nil {
			writeErr(w, http.StatusInternalServerError, err)
			return
		}
		c.Enabled = enabled == 1
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
	id := uuid.NewString()
	now := time.Now().Unix()
	_, err := d.DB.ExecContext(r.Context(), `
		INSERT INTO webhook_configs (id, name, kind, url, enabled, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?)
	`, id, req.Name, req.Kind, req.URL, boolI(req.Enabled), now, now)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"id": id})
}

func (d *MonitoringDeps) UpdateWebhook(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	var req webhookReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	// If URL is empty or masked-shape, keep the existing one.
	var res sql.Result
	var err error
	if req.URL == "" || isMaskedURL(req.URL) {
		res, err = d.DB.ExecContext(r.Context(), `
			UPDATE webhook_configs SET name = ?, kind = ?, enabled = ?, updated_at = ?
			WHERE id = ?
		`, req.Name, req.Kind, boolI(req.Enabled), time.Now().Unix(), id)
	} else {
		res, err = d.DB.ExecContext(r.Context(), `
			UPDATE webhook_configs SET name = ?, kind = ?, url = ?, enabled = ?, updated_at = ?
			WHERE id = ?
		`, req.Name, req.Kind, req.URL, boolI(req.Enabled), time.Now().Unix(), id)
	}
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	if d.Alerts != nil {
		d.Alerts.InvalidateWebhook(id)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		writeErr(w, http.StatusNotFound, errors.New("webhook not found"))
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"updated": n})
}

func (d *MonitoringDeps) DeleteWebhook(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	res, err := d.DB.ExecContext(r.Context(), `DELETE FROM webhook_configs WHERE id = ?`, id)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	if d.Alerts != nil {
		d.Alerts.InvalidateWebhook(id)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		writeErr(w, http.StatusNotFound, errors.New("webhook not found"))
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"deleted": n})
}

// TestWebhook posts a one-shot probe payload so the user can verify the URL
// from the UI.
func (d *MonitoringDeps) TestWebhook(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	var url string
	if err := d.DB.QueryRowContext(r.Context(),
		`SELECT url FROM webhook_configs WHERE id = ?`, id).Scan(&url); err != nil {
		writeErr(w, http.StatusNotFound, err)
		return
	}
	go func(u string) {
		c := discord.NewClient(u)
		p := discord.Payload{
			Username: "labextend",
			Embeds: []discord.Embed{{
				Title:     "Webhook test · success",
				Color:     discord.ColorBlue,
				Timestamp: time.Now().UTC().Format(time.RFC3339),
				Footer:    &discord.Footer{Text: "labextend"},
			}},
		}
		if err := c.Post(context.Background(), p); err != nil {
			slog.Warn("webhook test failed", "id", id, "err", err)
		}
	}(url)
	writeJSON(w, http.StatusOK, map[string]any{"queued": true})
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

// _ = sync.Once // keep import for future use
var _ = sync.Once{}
