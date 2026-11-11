package notify

import (
	"context"
	"database/sql"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Bartis-Dev/LabExtend/internal/db"
)

func testDB(t *testing.T) *sql.DB {
	t.Helper()
	ctx := context.Background()
	d, err := db.Open(ctx, t.TempDir()+"/n.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = d.Close() })
	if err := db.Migrate(ctx, d); err != nil {
		t.Fatal(err)
	}
	return d
}

type sink struct {
	mu     sync.Mutex
	bodies []string
}

func (s *sink) all() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.bodies...)
}

func server(t *testing.T, status int) (*sink, string) {
	t.Helper()
	s := &sink{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		s.mu.Lock()
		s.bodies = append(s.bodies, string(b))
		s.mu.Unlock()
		w.WriteHeader(status)
	}))
	t.Cleanup(srv.Close)
	return s, srv.URL
}

func addWebhook(t *testing.T, d *sql.DB, id, url string, enabled bool, types string) {
	t.Helper()
	en := 0
	if enabled {
		en = 1
	}
	if _, err := d.Exec(`INSERT INTO webhook_configs (id, name, kind, url, enabled, created_at, updated_at, event_types)
		VALUES (?, ?, 'discord', ?, ?, 0, 0, ?)`, id, id, url, en, types); err != nil {
		t.Fatal(err)
	}
}

type pubRec struct {
	mu     sync.Mutex
	topics []string
}

func (p *pubRec) Publish(topic string, _ any) {
	p.mu.Lock()
	p.topics = append(p.topics, topic)
	p.mu.Unlock()
}

func TestEmitFansOutOnlyToSubscribedEnabledWebhooks(t *testing.T) {
	db := testDB(t)
	sub, subURL := server(t, http.StatusNoContent)
	other, otherURL := server(t, http.StatusNoContent)
	off, offURL := server(t, http.StatusNoContent)
	addWebhook(t, db, "sub", subURL, true, `["backup.failed","node.offline"]`)
	addWebhook(t, db, "other", otherURL, true, `["node.online"]`)
	addWebhook(t, db, "off", offURL, false, `["backup.failed"]`)

	pub := &pubRec{}
	c := New(db, pub)
	n := c.Emit(context.Background(), Event{Type: BackupFailed, Severity: SeverityCritical,
		Title: "Backup failed · Nightly", Body: "s3 endpoint: boom", Link: "/backups?run=r1",
		Details: map[string]any{"run_id": "r1"}})

	if got := sub.all(); len(got) != 1 || !strings.Contains(got[0], "Backup failed") || !strings.Contains(got[0], "s3 endpoint: boom") {
		t.Fatalf("subscribed webhook got %q", got)
	}
	if len(other.all()) != 0 {
		t.Fatal("webhook not subscribed to backup.failed was posted to")
	}
	if len(off.all()) != 0 {
		t.Fatal("disabled webhook was posted to")
	}
	if n.ID == 0 || n.DeliveryError != "" {
		t.Fatalf("notification = %+v", n)
	}
	var title, link, details string
	if err := db.QueryRow(`SELECT title, link, details_json FROM notifications WHERE id = ?`, n.ID).Scan(&title, &link, &details); err != nil {
		t.Fatal(err)
	}
	if link != "/backups?run=r1" || !strings.Contains(details, `"run_id":"r1"`) {
		t.Fatalf("stored link=%q details=%q", link, details)
	}
	if len(pub.topics) != 1 || pub.topics[0] != "notification" {
		t.Fatalf("published %v", pub.topics)
	}
}

func TestEmitRecordsDeliveryFailure(t *testing.T) {
	db := testDB(t)
	_, badURL := server(t, http.StatusInternalServerError)
	good, goodURL := server(t, http.StatusNoContent)
	addWebhook(t, db, "bad", badURL, true, `["node.offline"]`)
	addWebhook(t, db, "good", goodURL, true, `["node.offline"]`)

	pub := &pubRec{}
	n := New(db, pub).Emit(context.Background(), Event{Type: NodeOffline, Severity: SeverityCritical, Title: "Node offline · n1"})

	if len(good.all()) != 1 {
		t.Fatal("one failing webhook stopped delivery to the others")
	}
	var stored string
	if err := db.QueryRow(`SELECT delivery_error FROM notifications WHERE id = ?`, n.ID).Scan(&stored); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(stored, "bad") || !strings.Contains(stored, "500") || strings.Contains(stored, "good") {
		t.Fatalf("delivery_error = %q", stored)
	}
	if n.DeliveryError != stored {
		t.Fatalf("returned %q, stored %q", n.DeliveryError, stored)
	}
	if strings.Join(pub.topics, ",") != "notification,notification.updated" {
		t.Fatalf("published %v", pub.topics)
	}
}

func TestPruneKeepsNewestAndDropsOld(t *testing.T) {
	db := testDB(t)
	c := New(db, nil)
	old := time.Now().Add(-retentionAge - time.Hour).Unix()
	if _, err := db.Exec(`INSERT INTO notifications (ts, type, severity, title) VALUES (?, 'node.offline', 'critical', 'old')`, old); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < retentionRows+5; i++ {
		c.Emit(context.Background(), Event{Type: NodeOnline, Severity: SeverityInfo, Title: "x"})
	}
	var n, olds int
	_ = db.QueryRow(`SELECT COUNT(*) FROM notifications`).Scan(&n)
	_ = db.QueryRow(`SELECT COUNT(*) FROM notifications WHERE title = 'old'`).Scan(&olds)
	if n != retentionRows || olds != 0 {
		t.Fatalf("rows=%d old=%d, want %d and 0", n, olds, retentionRows)
	}
}

func TestMarkReadAndUnreadCount(t *testing.T) {
	db := testDB(t)
	c := New(db, nil)
	a := c.Emit(context.Background(), Event{Type: NodeOffline, Severity: SeverityCritical, Title: "a"})
	c.Emit(context.Background(), Event{Type: NodeOffline, Severity: SeverityCritical, Title: "b"})

	if err := c.MarkRead(context.Background(), a.ID); err != nil {
		t.Fatal(err)
	}
	list, unread, err := c.List(context.Background(), 50)
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 2 || unread != 1 || list[0].Title != "b" || list[1].ReadAt == nil {
		t.Fatalf("list=%+v unread=%d", list, unread)
	}
	if err := c.MarkRead(context.Background(), 0); err != nil {
		t.Fatal(err)
	}
	if _, unread, _ = c.List(context.Background(), 50); unread != 0 {
		t.Fatalf("unread after mark all = %d", unread)
	}
}
