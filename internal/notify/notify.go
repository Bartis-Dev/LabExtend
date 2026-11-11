// Package notify keeps the events the leader emits (failed backups, nodes going
// offline, crash-looping containers) as notifications for the panel and sends
// each one to every enabled webhook subscribed to its type.
package notify

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/Bartis-Dev/LabExtend/internal/discord"
)

// Event types. Webhooks subscribe to these by name, so they are part of the
// stored configuration: renaming one silently unsubscribes every webhook.
const (
	BackupFailed          = "backup.failed"
	BackupRetentionFailed = "backup.retention_failed"
	NodeOffline           = "node.offline"
	NodeOnline            = "node.online"
	ContainerCrashloop    = "container.crashloop"
)

// Types lists every event type in the order the UI shows them.
var Types = []string{BackupFailed, BackupRetentionFailed, NodeOffline, NodeOnline, ContainerCrashloop}

// ValidType reports whether t is one of Types.
func ValidType(t string) bool {
	for _, v := range Types {
		if v == t {
			return true
		}
	}
	return false
}

const (
	SeverityCritical = "critical"
	SeverityWarning  = "warning"
	SeverityInfo     = "info"
)

const (
	retentionAge  = 90 * 24 * time.Hour
	retentionRows = 500
)

// Event is what an emitter hands to Emit.
type Event struct {
	Type     string
	Severity string
	Title    string
	Body     string
	Link     string // panel path of the related page, e.g. /node?id=x
	Details  map[string]any
}

// Notification is a stored event, as the API returns it.
type Notification struct {
	ID            int64           `json:"id"`
	TS            int64           `json:"ts"`
	Type          string          `json:"type"`
	Severity      string          `json:"severity"`
	Title         string          `json:"title"`
	Body          string          `json:"body"`
	Link          string          `json:"link,omitempty"`
	Details       json.RawMessage `json:"details"`
	ReadAt        *int64          `json:"read_at"`
	DeliveryError string          `json:"delivery_error,omitempty"`
}

// Publisher pushes notifications to SSE subscribers (the leader's Hub).
type Publisher interface {
	Publish(topic string, data any)
}

// Center stores and delivers notifications. A nil *Center drops every event,
// so code paths without one (tests, the retention report) need no guard.
type Center struct {
	db  *sql.DB
	pub Publisher
}

func New(db *sql.DB, pub Publisher) *Center {
	return &Center{db: db, pub: pub}
}

// Emit stores ev, publishes it on SSE and posts it to the subscribed webhooks.
// It blocks until every webhook answered; a failed post is logged and kept on
// the notification as delivery_error.
func (c *Center) Emit(ctx context.Context, ev Event) Notification {
	if c == nil || c.db == nil {
		return Notification{}
	}
	// Emitters call this at the end of runs whose context may already be dead
	// (a backup that hit its 4h deadline), which is exactly an event to keep.
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), time.Minute)
	defer cancel()

	details := []byte("{}")
	if len(ev.Details) > 0 {
		if b, err := json.Marshal(ev.Details); err == nil {
			details = b
		}
	}
	n := Notification{
		TS: time.Now().Unix(), Type: ev.Type, Severity: ev.Severity,
		Title: ev.Title, Body: ev.Body, Link: ev.Link, Details: details,
	}
	res, err := c.db.ExecContext(ctx, `
		INSERT INTO notifications (ts, type, severity, title, body, link, details_json)
		VALUES (?, ?, ?, ?, ?, ?, ?)`,
		n.TS, n.Type, n.Severity, n.Title, n.Body, n.Link, string(details))
	if err != nil {
		slog.Error("notify: storing notification failed", "type", ev.Type, "err", err)
	} else {
		n.ID, _ = res.LastInsertId()
		c.prune(ctx, time.Now())
	}
	slog.Info("notify: event", "type", n.Type, "severity", n.Severity, "title", n.Title)
	c.publish("notification", n)

	if failed := c.deliver(ctx, n); failed != "" {
		n.DeliveryError = failed
		if n.ID != 0 {
			if _, err := c.db.ExecContext(ctx,
				`UPDATE notifications SET delivery_error = ? WHERE id = ?`, failed, n.ID); err != nil {
				slog.Warn("notify: recording delivery error failed", "id", n.ID, "err", err)
			}
		}
		c.publish("notification.updated", n)
	}
	return n
}

// deliver posts n to every enabled webhook subscribed to its type and returns
// the failures as "name: error" lines, or "" when all succeeded.
func (c *Center) deliver(ctx context.Context, n Notification) string {
	rows, err := c.db.QueryContext(ctx,
		`SELECT name, url, event_types FROM webhook_configs WHERE enabled = 1 ORDER BY name`)
	if err != nil {
		slog.Warn("notify: loading webhooks failed", "err", err)
		return "loading webhooks: " + err.Error()
	}
	type target struct{ name, url string }
	var targets []target
	for rows.Next() {
		var t target
		var types string
		if err := rows.Scan(&t.name, &t.url, &types); err != nil {
			continue
		}
		if subscribed(types, n.Type) {
			targets = append(targets, t)
		}
	}
	_ = rows.Close()

	var failed []string
	payload := Payload(n)
	for _, t := range targets {
		if err := discord.NewClient(t.url).Post(ctx, payload); err != nil {
			slog.Warn("notify: webhook post failed", "webhook", t.name, "type", n.Type, "err", err)
			failed = append(failed, t.name+": "+err.Error())
		}
	}
	return strings.Join(failed, "\n")
}

// Payload renders n as a Discord message.
func Payload(n Notification) discord.Payload {
	color := discord.ColorBlue
	switch n.Severity {
	case SeverityCritical:
		color = discord.ColorRed
	case SeverityWarning:
		color = discord.ColorYellow
	}
	if n.Type == NodeOnline {
		color = discord.ColorGreen
	}
	body := n.Body
	// Discord rejects descriptions over 4096 characters outright.
	if len(body) > 4000 {
		body = body[:4000] + "… (truncated)"
	}
	return discord.Payload{
		Username: "LabExtend",
		Embeds: []discord.Embed{{
			Title:       n.Title,
			Description: body,
			Color:       color,
			Timestamp:   time.Unix(n.TS, 0).UTC().Format(time.RFC3339),
			Footer:      &discord.Footer{Text: "LabExtend · " + n.Type},
		}},
	}
}

// ParseTypes decodes a webhook's event_types column, dropping unknown names.
func ParseTypes(s string) []string {
	var raw []string
	_ = json.Unmarshal([]byte(s), &raw)
	out := []string{}
	for _, t := range raw {
		if ValidType(t) {
			out = append(out, t)
		}
	}
	return out
}

// subscribed reports whether the event_types column s includes typ.
func subscribed(s, typ string) bool {
	for _, t := range ParseTypes(s) {
		if t == typ {
			return true
		}
	}
	return false
}

func (c *Center) prune(ctx context.Context, now time.Time) {
	if _, err := c.db.ExecContext(ctx, `
		DELETE FROM notifications
		 WHERE ts < ?
		    OR id NOT IN (SELECT id FROM notifications ORDER BY id DESC LIMIT ?)`,
		now.Add(-retentionAge).Unix(), retentionRows); err != nil {
		slog.Warn("notify: pruning notifications failed", "err", err)
	}
}

// List returns the newest notifications and the number of unread ones.
func (c *Center) List(ctx context.Context, limit int) ([]Notification, int, error) {
	rows, err := c.db.QueryContext(ctx, `
		SELECT id, ts, type, severity, title, body, link, details_json, read_at, delivery_error
		  FROM notifications ORDER BY id DESC LIMIT ?`, limit)
	if err != nil {
		return nil, 0, fmt.Errorf("list notifications: %w", err)
	}
	defer rows.Close()
	out := []Notification{}
	for rows.Next() {
		var n Notification
		var details string
		var readAt sql.NullInt64
		if err := rows.Scan(&n.ID, &n.TS, &n.Type, &n.Severity, &n.Title, &n.Body,
			&n.Link, &details, &readAt, &n.DeliveryError); err != nil {
			return nil, 0, fmt.Errorf("scan notification: %w", err)
		}
		n.Details = json.RawMessage(details)
		if readAt.Valid {
			v := readAt.Int64
			n.ReadAt = &v
		}
		out = append(out, n)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, fmt.Errorf("list notifications: %w", err)
	}
	var unread int
	if err := c.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM notifications WHERE read_at IS NULL`).Scan(&unread); err != nil {
		return nil, 0, fmt.Errorf("count unread: %w", err)
	}
	return out, unread, nil
}

// MarkRead marks one notification read, or all of them when id is 0.
func (c *Center) MarkRead(ctx context.Context, id int64) error {
	q, args := `UPDATE notifications SET read_at = ? WHERE read_at IS NULL`, []any{time.Now().Unix()}
	if id != 0 {
		q += ` AND id = ?`
		args = append(args, id)
	}
	if _, err := c.db.ExecContext(ctx, q, args...); err != nil {
		return fmt.Errorf("mark read: %w", err)
	}
	c.publish("notification.read", map[string]any{"id": id})
	return nil
}

func (c *Center) publish(topic string, data any) {
	if c.pub != nil {
		c.pub.Publish(topic, data)
	}
}
