package leader

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/Bartis-Dev/LabExtend/internal/discord"
)

// AlertKind is the metric this rule watches.
type AlertKind string

const (
	AlertCPUPercent       AlertKind = "cpu_percent"
	AlertMemPercent       AlertKind = "mem_percent"
	AlertDiskPercent      AlertKind = "disk_percent"
	AlertDiskFreeGB       AlertKind = "disk_free_gb"
	AlertLoadAvg1m        AlertKind = "load_avg_1m"
	AlertNodeOffline      AlertKind = "node_offline" // duration_sec since last heartbeat
	AlertContainerCrashed AlertKind = "container_crashed"
)

// AlertRule is the DB row + parsed fields.
type AlertRule struct {
	ID           string  `json:"id"`
	Name         string  `json:"name"`
	Kind         string  `json:"kind"`
	Comparator   string  `json:"comparator"` // > >= < <=
	Threshold    float64 `json:"threshold"`
	DurationSec  int     `json:"duration_sec"`
	Scope        string  `json:"scope"`        // "all" | "node:<host>" | "label:<k=v>"
	WebhookID    *string `json:"webhook_id,omitempty"`
	CooldownSec  int     `json:"cooldown_sec"`
	Enabled      bool    `json:"enabled"`
	CreatedAt    int64   `json:"created_at"`
	UpdatedAt    int64   `json:"updated_at"`
}

// AlertHistoryRow is one fire/recover event.
type AlertHistoryRow struct {
	ID          int64   `json:"id"`
	RuleID      string  `json:"rule_id"`
	RuleName    string  `json:"rule_name,omitempty"`
	NodeID      string  `json:"node_id,omitempty"`
	ContainerID string  `json:"container_id,omitempty"`
	FiredAt     int64   `json:"fired_at"`
	State       string  `json:"state"`
	Value       float64 `json:"value"`
	Message     string  `json:"message"`
}

// ruleState tracks evaluator state per (rule, target). target is "node:<id>"
// or "container:<node>::<cid>".
type ruleState struct {
	overSince time.Time // when the condition first went above threshold
	triggered bool      // currently in "triggered" state (so we can post "recover")
	lastFired time.Time // last time a webhook was POSTed (for cooldown)
}

// AlertEngine evaluates rules against metrics samples + container reports.
type AlertEngine struct {
	db      *sql.DB
	metrics *metricsStore
	cnts    *containerStore
	reg     *AgentRegistry
	hub     *Hub

	mu       sync.RWMutex
	state    map[string]*ruleState // key = ruleID + "|" + targetKey
	webhooks map[string]string     // id → decoded URL (cached)
}

func newAlertEngine(db *sql.DB, m *metricsStore, c *containerStore, reg *AgentRegistry, hub *Hub) *AlertEngine {
	return &AlertEngine{
		db:       db,
		metrics:  m,
		cnts:     c,
		reg:      reg,
		hub:      hub,
		state:    make(map[string]*ruleState),
		webhooks: make(map[string]string),
	}
}

// Run loops every 5s evaluating all enabled rules.
func (e *AlertEngine) Run(ctx context.Context) {
	t := time.NewTicker(5 * time.Second)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			rules, err := e.loadEnabledRules(ctx)
			if err != nil {
				slog.Warn("alert: load rules failed", "err", err)
				continue
			}
			for _, r := range rules {
				e.evaluate(ctx, r)
			}
		}
	}
}

func (e *AlertEngine) loadEnabledRules(ctx context.Context) ([]AlertRule, error) {
	if e.db == nil {
		return nil, nil
	}
	rows, err := e.db.QueryContext(ctx, `
		SELECT id, name, kind, comparator, threshold, duration_sec, scope,
		       webhook_id, cooldown_sec, enabled, created_at, updated_at
		FROM alert_rules WHERE enabled = 1
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []AlertRule
	for rows.Next() {
		var r AlertRule
		var wid sql.NullString
		var enabled int
		if err := rows.Scan(&r.ID, &r.Name, &r.Kind, &r.Comparator, &r.Threshold,
			&r.DurationSec, &r.Scope, &wid, &r.CooldownSec, &enabled,
			&r.CreatedAt, &r.UpdatedAt); err != nil {
			return nil, err
		}
		r.Enabled = enabled == 1
		if wid.Valid && wid.String != "" {
			s := wid.String
			r.WebhookID = &s
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// evaluate runs a single rule. Container/node-offline rules iterate matching
// targets; metric-threshold rules use each node's current sample.
func (e *AlertEngine) evaluate(ctx context.Context, r AlertRule) {
	switch AlertKind(r.Kind) {
	case AlertContainerCrashed:
		e.evalContainerCrashed(ctx, r)
	case AlertNodeOffline:
		e.evalNodeOffline(ctx, r)
	default:
		e.evalNodeMetric(ctx, r)
	}
}

func (e *AlertEngine) evalNodeMetric(ctx context.Context, r AlertRule) {
	for nodeID, sample := range e.metrics.All() {
		if !ruleAppliesToNode(r.Scope, nodeID, e.reg) {
			continue
		}
		v := extractMetricValue(AlertKind(r.Kind), sample)
		breach := compare(v, r.Threshold, r.Comparator)
		e.processBreach(ctx, r, "node:"+nodeID, nodeID, "", v, breach,
			fmt.Sprintf("%s on %s = %.2f (threshold %s %.2f)", r.Kind, nodeID, v, r.Comparator, r.Threshold))
	}
}

func (e *AlertEngine) evalNodeOffline(ctx context.Context, r AlertRule) {
	// Iterate all known agents; for each, last_seen drives the breach.
	for _, c := range e.reg.List() {
		since := time.Since(c.LastSeen).Seconds()
		breach := compare(since, r.Threshold, r.Comparator)
		if !ruleAppliesToNode(r.Scope, c.ID, e.reg) {
			continue
		}
		e.processBreach(ctx, r, "node:"+c.ID, c.ID, "", since, breach,
			fmt.Sprintf("node %s last seen %.0fs ago", c.ID, since))
	}
}

func (e *AlertEngine) evalContainerCrashed(ctx context.Context, r AlertRule) {
	views, _ := e.cnts.All(ctx)
	for _, v := range views {
		if !ruleAppliesToNode(r.Scope, v.NodeID, e.reg) {
			continue
		}
		val := float64(v.RecentRestarts)
		breach := v.CrashedLoop // for this rule comparator is implicit
		e.processBreach(ctx, r,
			"container:"+v.NodeID+"::"+v.ContainerID, v.NodeID, v.ContainerID,
			val, breach,
			fmt.Sprintf("container %s on %s in restart-loop (%d restarts/60s)",
				v.Name, v.NodeID, v.RecentRestarts))
	}
}

// processBreach is the hysteresis core: only fire if breach holds for
// >= duration_sec; only fire once per cooldown; post "recovered" when breach
// clears after a prior trigger.
func (e *AlertEngine) processBreach(ctx context.Context, r AlertRule,
	targetKey, nodeID, containerID string,
	value float64, breach bool, message string) {

	stateKey := r.ID + "|" + targetKey
	now := time.Now()

	e.mu.Lock()
	st, ok := e.state[stateKey]
	if !ok {
		st = &ruleState{}
		e.state[stateKey] = st
	}
	e.mu.Unlock()

	if breach {
		if st.overSince.IsZero() {
			st.overSince = now
		}
		held := now.Sub(st.overSince).Seconds()
		if held < float64(r.DurationSec) {
			return // not yet
		}
		if st.triggered {
			return // already fired
		}
		if !st.lastFired.IsZero() && now.Sub(st.lastFired).Seconds() < float64(r.CooldownSec) {
			return // cooldown
		}
		st.triggered = true
		st.lastFired = now
		e.fire(ctx, r, nodeID, containerID, "triggered", value, message)
	} else {
		if st.triggered {
			st.triggered = false
			e.fire(ctx, r, nodeID, containerID, "recovered", value, message)
		}
		st.overSince = time.Time{}
	}
}

// fire writes an alert_history row, broadcasts on SSE, posts to webhook.
func (e *AlertEngine) fire(ctx context.Context, r AlertRule, nodeID, containerID, state string, value float64, message string) {
	now := time.Now()
	if e.db != nil {
		var nodeArg, cidArg any
		if nodeID != "" {
			nodeArg = nodeID
		}
		if containerID != "" {
			cidArg = containerID
		}
		_, _ = e.db.ExecContext(ctx, `
			INSERT INTO alert_history (rule_id, node_id, container_id, fired_at, state, value, message)
			VALUES (?, ?, ?, ?, ?, ?, ?)
		`, r.ID, nodeArg, cidArg, now.Unix(), state, value, message)
	}

	if e.hub != nil {
		e.hub.Publish("alert."+state, map[string]any{
			"rule_id":      r.ID,
			"rule_name":    r.Name,
			"state":        state,
			"node_id":      nodeID,
			"container_id": containerID,
			"value":        value,
			"message":      message,
			"ts":           now.Unix(),
		})
	}

	slog.Info("alert fired", "rule", r.Name, "state", state, "node", nodeID,
		"container", containerID, "value", value)

	if r.WebhookID == nil || *r.WebhookID == "" {
		return
	}
	url, err := e.webhookURL(ctx, *r.WebhookID)
	if err != nil || url == "" {
		slog.Warn("webhook url lookup failed", "err", err)
		return
	}
	scope := nodeID
	if containerID != "" {
		scope = nodeID + " / " + containerID
	}
	go func() {
		c := discord.NewClient(url)
		if err := c.Post(context.Background(), discord.AlertEmbed(state, r.Name, scope, message, value, now)); err != nil {
			slog.Warn("webhook post failed", "err", err)
		}
	}()
}

// webhookURL looks up the URL by ID (cached). NOT decrypted yet — webhook_configs.url
// is currently plain text in v1 (we store the URL, not credentials).
func (e *AlertEngine) webhookURL(ctx context.Context, id string) (string, error) {
	e.mu.RLock()
	if v, ok := e.webhooks[id]; ok {
		e.mu.RUnlock()
		return v, nil
	}
	e.mu.RUnlock()
	if e.db == nil {
		return "", errors.New("no db")
	}
	var url string
	if err := e.db.QueryRowContext(ctx,
		`SELECT url FROM webhook_configs WHERE id = ? AND enabled = 1`, id).Scan(&url); err != nil {
		return "", err
	}
	e.mu.Lock()
	e.webhooks[id] = url
	e.mu.Unlock()
	return url, nil
}

// InvalidateWebhook drops the cached URL (called by webhook CRUD handlers).
func (e *AlertEngine) InvalidateWebhook(id string) {
	e.mu.Lock()
	delete(e.webhooks, id)
	e.mu.Unlock()
}

// ─── helpers ────────────────────────────────────────────────────────────────

func extractMetricValue(k AlertKind, s *MetricsSample) float64 {
	if s == nil {
		return 0
	}
	switch k {
	case AlertCPUPercent:
		return s.CPUPercent
	case AlertMemPercent:
		return s.MemPercent
	case AlertDiskPercent:
		return s.DiskPercent
	case AlertDiskFreeGB:
		if s.DiskTotalBytes == 0 {
			return 0
		}
		freeBytes := s.DiskTotalBytes - s.DiskUsedBytes
		return float64(freeBytes) / (1024 * 1024 * 1024)
	case AlertLoadAvg1m:
		return s.LoadAvg1m
	}
	return 0
}

func compare(value, threshold float64, op string) bool {
	switch op {
	case ">":
		return value > threshold
	case ">=":
		return value >= threshold
	case "<":
		return value < threshold
	case "<=":
		return value <= threshold
	}
	return false
}

// ruleAppliesToNode evaluates a scope expression against a node ID.
// Supported forms:
//
//	"all"             — every node
//	"node:<hostname>" — exact match
//	"label:k=v"       — node labels (looked up via the AgentRegistry)
func ruleAppliesToNode(scope, nodeID string, reg *AgentRegistry) bool {
	scope = strings.TrimSpace(scope)
	if scope == "" || scope == "all" {
		return true
	}
	if strings.HasPrefix(scope, "node:") {
		return strings.TrimPrefix(scope, "node:") == nodeID
	}
	if strings.HasPrefix(scope, "label:") {
		want := strings.TrimPrefix(scope, "label:")
		k, v, ok := strings.Cut(want, "=")
		if !ok {
			return false
		}
		conn, found := reg.Get(nodeID)
		if !found {
			return false
		}
		return conn.Labels[k] == v
	}
	return false
}
