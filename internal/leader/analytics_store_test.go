package leader

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/Bartis-Dev/LabExtend/internal/db"
	pb "github.com/Bartis-Dev/LabExtend/internal/grpc/pb"
)

func analyticsDB(t *testing.T) *sql.DB {
	t.Helper()
	ctx := context.Background()
	d, err := db.Open(ctx, t.TempDir()+"/t.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = d.Close() })
	if err := db.Migrate(ctx, d); err != nil {
		t.Fatal(err)
	}
	return d
}

const (
	testUA    = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/130.0.0.0 Safari/537.36"
	testBotUA = "Mozilla/5.0 (compatible; Googlebot/2.1)"
)

type hit struct {
	ip, host, method, path, ua, ref, router string
	// mode and lang default to a real browser navigation; "-" omits the
	// header, the way Traefik drops headers the client did not send.
	mode, lang string
	status     int
	at         time.Time
}

func (h hit) line() string {
	if h.method == "" {
		h.method = "GET"
	}
	if h.status == 0 {
		h.status = 200
	}
	switch h.router {
	case "":
		h.router = "site@docker"
	case "-": // explicitly no router
		h.router = ""
	}
	ref := ""
	if h.ref != "" {
		ref = fmt.Sprintf(`,"request_Referer":%q`, h.ref)
	}
	if h.mode == "" {
		h.mode = "navigate"
	}
	if h.mode != "-" {
		ref += fmt.Sprintf(`,"request_Sec-Fetch-Mode":%q`, h.mode)
	}
	if h.lang == "" {
		h.lang = "de-DE,de;q=0.9,en;q=0.8"
	}
	if h.lang != "-" {
		ref += fmt.Sprintf(`,"request_Accept-Language":%q`, h.lang)
	}
	return fmt.Sprintf(`{"ClientHost":%q,"DownstreamStatus":%d,"RequestHost":%q,"RequestMethod":%q,"RequestPath":%q,"StartUTC":%q,"RouterName":%q,"request_User-Agent":%q%s}`,
		h.ip, h.status, h.host, h.method, h.path, h.at.UTC().Format(time.RFC3339Nano), h.router, h.ua, ref)
}

// testAnalytics wires a store whose clock the test controls and where
// container "tr" on node "n1" is Traefik and "app" is not.
func testAnalytics(t *testing.T, d *sql.DB, now *time.Time) *webAnalytics {
	t.Helper()
	images := func(node, cid string) string {
		if node == "n1" && cid == "tr" {
			return "traefik:v3.7"
		}
		return "nginx:1.27"
	}
	a := newWebAnalytics(d, images, "traefik", 400, true)
	a.now = func() time.Time { return *now }
	if err := a.Load(context.Background()); err != nil {
		t.Fatal(err)
	}
	return a
}

func ingest(a *webAnalytics, cid string, lines ...string) {
	b := &pb.LogBatch{}
	for _, l := range lines {
		b.Lines = append(b.Lines, &pb.LogLine{ContainerId: cid, Stream: "stdout", Line: l})
	}
	a.Ingest("n1", b)
}

func mustFlush(t *testing.T, a *webAnalytics) {
	t.Helper()
	if err := a.Flush(context.Background()); err != nil {
		t.Fatal(err)
	}
}

type dailyRow struct{ requests, pageviews, bots, visitors int64 }

func readDaily(t *testing.T, d *sql.DB, host, day string) dailyRow {
	t.Helper()
	var r dailyRow
	err := d.QueryRow(`SELECT requests, pageviews, bot_hits, visitors FROM web_daily WHERE host=? AND day=?`, host, day).
		Scan(&r.requests, &r.pageviews, &r.bots, &r.visitors)
	if err != nil && err != sql.ErrNoRows {
		t.Fatal(err)
	}
	return r
}

func countRows(t *testing.T, d *sql.DB, q string, args ...any) int {
	t.Helper()
	var n int
	if err := d.QueryRow(q, args...).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func dimCount(t *testing.T, d *sql.DB, host, day, dim, value string) int64 {
	t.Helper()
	var n int64
	err := d.QueryRow(`SELECT count FROM web_dims WHERE host=? AND day=? AND dim=? AND value=?`, host, day, dim, value).Scan(&n)
	if err != nil && err != sql.ErrNoRows {
		t.Fatal(err)
	}
	return n
}

func TestWebAnalyticsFlushAggregates(t *testing.T) {
	d := analyticsDB(t)
	now := time.Date(2026, 11, 5, 10, 30, 0, 0, time.UTC)
	a := testAnalytics(t, d, &now)
	at := now.Add(-time.Minute)
	day := "2026-11-05"

	ingest(a, "tr",
		hit{ip: "203.0.113.7", host: "Example.com:443", path: "/", ua: testUA, at: at}.line(),
		hit{ip: "203.0.113.7", host: "example.com", path: "/blog", ua: testUA, ref: "https://www.google.com/search", at: at}.line(),
		hit{ip: "203.0.113.7", host: "example.com", path: "/blog/x", ua: testUA, ref: "https://example.com/blog", at: at}.line(), // internal nav
		hit{ip: "198.51.100.2", host: "example.com", path: "/", ua: testUA, at: at}.line(),
		hit{ip: "198.51.100.2", host: "example.com", path: "/_next/static/a.js", ua: testUA, at: at}.line(), // asset
		hit{ip: "198.51.100.2", host: "example.com", path: "/missing", ua: testUA, status: 404, at: at}.line(),
		hit{ip: "192.0.2.9", host: "example.com", path: "/", ua: testBotUA, at: at}.line(), // bot
		hit{ip: "192.0.2.9", host: "example.com", path: "/", ua: testUA, router: "web-to-websecure@internal", status: 301, at: at}.line(),
		hit{ip: "192.0.2.9", host: "example.com", path: "/", ua: testUA, router: "-", status: 404, at: at}.line(), // no router: scanner
		`{"level":"info","msg":"Configuration loaded"}`,
		`not json at all`,
	)
	// The same access line from a non-Traefik container must be ignored.
	ingest(a, "app", hit{ip: "203.0.113.7", host: "example.com", path: "/", ua: testUA, at: at}.line())
	mustFlush(t, a)

	got := readDaily(t, d, "example.com", day)
	want := dailyRow{requests: 7, pageviews: 4, bots: 1, visitors: 2}
	if got != want {
		t.Fatalf("daily = %+v, want %+v", got, want)
	}
	if n := a.parseErrors.Load(); n != 1 {
		t.Errorf("parse errors = %d, want 1", n)
	}
	checks := []struct {
		dim, value string
		want       int64
	}{
		{"path", "/", 2}, {"path", "/blog", 1}, {"path", "/blog/x", 1}, {"path", "/_next/static/a.js", 0},
		{"referrer", "direct", 2}, {"referrer", "google.com", 1}, {"referrer", "example.com", 0},
		{"browser", "Chrome", 4}, {"os", "Windows", 4},
		{"status", "2xx", 6}, {"status", "4xx", 1},
	}
	for _, c := range checks {
		if n := dimCount(t, d, "example.com", day, c.dim, c.value); n != c.want {
			t.Errorf("dim %s=%q count = %d, want %d", c.dim, c.value, n, c.want)
		}
	}
	hour := now.Truncate(time.Hour).Unix()
	var hpv, hvis int64
	if err := d.QueryRow(`SELECT pageviews, visitors FROM web_hourly WHERE host='example.com' AND hour=?`, hour).Scan(&hpv, &hvis); err != nil {
		t.Fatal(err)
	}
	if hpv != 4 || hvis != 2 {
		t.Errorf("hourly pv/visitors = %d/%d, want 4/2", hpv, hvis)
	}

	// A second flush adds to the counters; a returning visitor is not new.
	ingest(a, "tr",
		hit{ip: "203.0.113.7", host: "example.com", path: "/", ua: testUA, at: at}.line(),
		hit{ip: "203.0.113.50", host: "example.com", path: "/", ua: testUA, at: at}.line(),
	)
	mustFlush(t, a)
	got = readDaily(t, d, "example.com", day)
	want = dailyRow{requests: 9, pageviews: 6, bots: 1, visitors: 3}
	if got != want {
		t.Fatalf("after second flush daily = %+v, want %+v", got, want)
	}
	// An empty flush must not change anything.
	mustFlush(t, a)
	if got2 := readDaily(t, d, "example.com", day); got2 != want {
		t.Fatalf("empty flush changed daily to %+v", got2)
	}
}

func TestWebAnalyticsDailySaltRotation(t *testing.T) {
	d := analyticsDB(t)
	now := time.Date(2026, 11, 5, 23, 50, 0, 0, time.UTC)
	a := testAnalytics(t, d, &now)
	day1, day2 := "2026-11-05", "2026-11-06"

	lines := []string{
		hit{ip: "203.0.113.7", host: "example.com", path: "/", ua: testUA, at: now}.line(),
		hit{ip: "198.51.100.2", host: "example.com", path: "/", ua: testUA, at: now}.line(),
	}
	for i := 0; i < 60; i++ { // more distinct paths than the closed-day cap
		lines = append(lines, hit{ip: "203.0.113.7", host: "example.com", path: fmt.Sprintf("/p%d", i), ua: testUA, at: now}.line())
	}
	ingest(a, "tr", lines...)
	mustFlush(t, a)

	if n := countRows(t, d, `SELECT COUNT(*) FROM web_visitor_hashes WHERE day=?`, day1); n != 2 {
		t.Fatalf("day1 hashes = %d, want 2", n)
	}
	var salt1 []byte
	if err := d.QueryRow(`SELECT salt FROM web_visitor_salts WHERE day=?`, day1).Scan(&salt1); err != nil {
		t.Fatalf("day1 salt not stored: %v", err)
	}
	var hash1 []byte
	if err := d.QueryRow(`SELECT hash FROM web_visitor_hashes WHERE day=? ORDER BY hash LIMIT 1`, day1).Scan(&hash1); err != nil {
		t.Fatal(err)
	}

	// A restart on the same day keeps the salt, so a returning visitor is
	// still recognised rather than counted twice.
	b := testAnalytics(t, d, &now)
	ingest(b, "tr", hit{ip: "203.0.113.7", host: "example.com", path: "/", ua: testUA, at: now}.line())
	mustFlush(t, b)
	if got := readDaily(t, d, "example.com", day1).visitors; got != 2 {
		t.Fatalf("visitors after restart = %d, want 2", got)
	}

	// Next UTC day: the same visitor comes back.
	now = time.Date(2026, 11, 6, 0, 5, 0, 0, time.UTC)
	ingest(b, "tr", hit{ip: "203.0.113.7", host: "example.com", path: "/", ua: testUA, at: now}.line())
	mustFlush(t, b)

	if n := countRows(t, d, `SELECT COUNT(*) FROM web_visitor_hashes WHERE day=?`, day1); n != 0 {
		t.Errorf("day1 hashes after rollover = %d, want 0", n)
	}
	if n := countRows(t, d, `SELECT COUNT(*) FROM web_visitor_salts WHERE day<>?`, day2); n != 0 {
		t.Errorf("salts other than today's = %d, want 0", n)
	}
	if got := readDaily(t, d, "example.com", day1).visitors; got != 2 {
		t.Errorf("day1 visitor count must survive the hash deletion: got %d, want 2", got)
	}
	if got := readDaily(t, d, "example.com", day2).visitors; got != 1 {
		t.Errorf("day2 visitors = %d, want 1", got)
	}
	var salt2, hash2 []byte
	if err := d.QueryRow(`SELECT salt FROM web_visitor_salts WHERE day=?`, day2).Scan(&salt2); err != nil {
		t.Fatal(err)
	}
	if string(salt1) == string(salt2) {
		t.Error("day2 reused day1's salt")
	}
	if err := d.QueryRow(`SELECT hash FROM web_visitor_hashes WHERE day=?`, day2).Scan(&hash2); err != nil {
		t.Fatal(err)
	}
	if string(hash2) == string(hash1) {
		t.Error("the same visitor hashed identically on two days")
	}
	if n := countRows(t, d, `SELECT COUNT(*) FROM web_dims WHERE host='example.com' AND day=? AND dim='path'`, day1); n != 50 {
		t.Errorf("closed-day path rows = %d, want 50", n)
	}

	// No raw client address in any table, anywhere.
	assertNoIPStored(t, d, "203.0.113.7", "198.51.100.2")
}

func assertNoIPStored(t *testing.T, d *sql.DB, ips ...string) {
	t.Helper()
	for _, tbl := range []string{"web_visitor_salts", "web_visitor_hashes", "web_daily", "web_hourly", "web_dims"} {
		rows, err := d.Query(`SELECT * FROM ` + tbl)
		if err != nil {
			t.Fatal(err)
		}
		cols, _ := rows.Columns()
		for rows.Next() {
			vals := make([]any, len(cols))
			ptrs := make([]any, len(cols))
			for i := range vals {
				ptrs[i] = &vals[i]
			}
			if err := rows.Scan(ptrs...); err != nil {
				t.Fatal(err)
			}
			for _, v := range vals {
				s := fmt.Sprint(v)
				if b, ok := v.([]byte); ok {
					s = string(b)
				}
				for _, ip := range ips {
					if strings.Contains(s, ip) {
						t.Errorf("table %s stores client IP %s", tbl, ip)
					}
				}
			}
		}
		_ = rows.Close()
	}
}

func TestWebAnalyticsReport(t *testing.T) {
	d := analyticsDB(t)
	now := time.Date(2026, 11, 5, 10, 30, 0, 0, time.UTC)
	a := testAnalytics(t, d, &now)
	yesterday := now.Add(-24 * time.Hour)

	// Yesterday's data is written while "now" is still yesterday, the way it
	// happens live.
	real := now
	now = yesterday
	ingest(a, "tr",
		hit{ip: "203.0.113.7", host: "example.com", path: "/", ua: testUA, at: yesterday}.line(),
		hit{ip: "203.0.113.7", host: "other.org", path: "/", ua: testUA, at: yesterday}.line(),
	)
	mustFlush(t, a)
	now = real
	ingest(a, "tr",
		hit{ip: "203.0.113.7", host: "example.com", path: "/", ua: testUA, at: now}.line(),
		hit{ip: "198.51.100.2", host: "example.com", path: "/about", ua: testUA, ref: "https://news.ycombinator.com/", at: now}.line(),
		hit{ip: "192.0.2.9", host: "example.com", path: "/", ua: testBotUA, at: now}.line(),
	)
	mustFlush(t, a)

	hosts, err := a.Hosts(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(hosts) != 2 || hosts[0].Host != "example.com" {
		t.Fatalf("hosts = %+v, want example.com first of 2", hosts)
	}

	tests := []struct {
		rng                     string
		points                  int
		requests, pv, vis, bots int64
		topPath                 string
	}{
		{"7d", 7, 4, 3, 3, 1, "/"},
		{"24h", 24, 3, 2, 2, 1, "/"},
	}
	for _, tt := range tests {
		t.Run(tt.rng, func(t *testing.T) {
			r, err := a.Report(context.Background(), "example.com", tt.rng)
			if err != nil {
				t.Fatal(err)
			}
			if len(r.Series) != tt.points {
				t.Errorf("series points = %d, want %d", len(r.Series), tt.points)
			}
			tot := r.Totals
			if tot.Requests != tt.requests || tot.PageViews != tt.pv || tot.Visitors != tt.vis || tot.BotHits != tt.bots {
				t.Errorf("totals = %+v, want req=%d pv=%d vis=%d bots=%d", tot, tt.requests, tt.pv, tt.vis, tt.bots)
			}
			if len(r.Paths) == 0 || r.Paths[0].Value != tt.topPath {
				t.Errorf("paths = %+v, want %q first", r.Paths, tt.topPath)
			}
			var sum int64
			for _, p := range r.Series {
				sum += p.PageViews
			}
			if sum != tt.pv {
				t.Errorf("series page views sum = %d, want %d", sum, tt.pv)
			}
		})
	}
	if _, err := a.Report(context.Background(), "example.com", "2y"); err == nil {
		t.Error("unknown range accepted")
	}
}

// Ingest runs on gRPC goroutines while the ticker flushes; no hit may be lost
// or counted twice across the pending-map swap.
func TestWebAnalyticsConcurrentIngestAndFlush(t *testing.T) {
	d := analyticsDB(t)
	now := time.Date(2026, 11, 5, 10, 30, 0, 0, time.UTC)
	a := testAnalytics(t, d, &now)
	line := hit{ip: "203.0.113.7", host: "example.com", path: "/", ua: testUA, at: now}.line()

	const workers, perWorker = 4, 250
	done := make(chan struct{})
	for w := 0; w < workers; w++ {
		go func() {
			for i := 0; i < perWorker; i++ {
				ingest(a, "tr", line)
			}
			done <- struct{}{}
		}()
	}
	for finished := 0; finished < workers; {
		select {
		case <-done:
			finished++
		default:
			mustFlush(t, a)
		}
	}
	mustFlush(t, a)
	got := readDaily(t, d, "example.com", "2026-11-05")
	if want := (dailyRow{requests: workers * perWorker, pageviews: workers * perWorker, visitors: 1}); got != want {
		t.Fatalf("daily = %+v, want %+v", got, want)
	}
}

// Scanners send browser User-Agents but no Sec-Fetch-Mode / Accept-Language;
// they must land in bot hits, never in page views or visitors.
func TestWebAnalyticsRequireBrowserHeaders(t *testing.T) {
	now := time.Date(2026, 11, 5, 10, 30, 0, 0, time.UTC)
	at := now.Add(-time.Minute)
	lines := []string{
		hit{ip: "203.0.113.7", host: "example.com", path: "/", ua: testUA, at: at}.line(),
		hit{ip: "203.0.113.7", host: "example.com", path: "/b", ua: testUA, mode: "NAVIGATE", at: at}.line(),          // case-insensitive
		hit{ip: "198.51.100.2", host: "example.com", path: "/admin", ua: testUA, mode: "-", lang: "-", at: at}.line(), // scanner
		hit{ip: "198.51.100.3", host: "example.com", path: "/", ua: testUA, lang: "-", at: at}.line(),                 // no language
		hit{ip: "198.51.100.4", host: "example.com", path: "/", ua: testUA, mode: "cors", at: at}.line(),              // fetch(), not a navigation
		hit{ip: "198.51.100.5", host: "example.com", path: "/", ua: testUA, lang: "  ", at: at}.line(),                // blank counts as absent
		hit{ip: "198.51.100.6", host: "example.com", method: "HEAD", path: "/", ua: testUA, at: at}.line(),            // browsers navigate with GET
		hit{ip: "198.51.100.7", host: "example.com", path: "/a.js", ua: testUA, mode: "-", at: at}.line(),             // asset: not a candidate
	}
	tests := []struct {
		name    string
		require bool
		want    dailyRow
	}{
		{"required", true, dailyRow{requests: 8, pageviews: 2, bots: 5, visitors: 1}},
		{"off keeps old behaviour", false, dailyRow{requests: 8, pageviews: 7, bots: 0, visitors: 6}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			d := analyticsDB(t)
			n := now
			a := testAnalytics(t, d, &n)
			a.requireBrowserHeaders = tt.require
			ingest(a, "tr", lines...)
			mustFlush(t, a)
			if got := readDaily(t, d, "example.com", "2026-11-05"); got != tt.want {
				t.Fatalf("daily = %+v, want %+v", got, tt.want)
			}
			if tt.require {
				if c := dimCount(t, d, "example.com", "2026-11-05", "path", "/admin"); c != 0 {
					t.Errorf("scanner path counted %d times", c)
				}
			}
			// Accept-Language is only checked for presence, never kept.
			assertNoIPStored(t, d, "de-DE")
		})
	}
}

func TestWebAnalyticsMissingHeaderWarning(t *testing.T) {
	d := analyticsDB(t)
	now := time.Date(2026, 11, 5, 10, 30, 0, 0, time.UTC)
	a := testAnalytics(t, d, &now)
	var lines []string
	for i := 0; i < webHeaderWarnMin; i++ {
		lines = append(lines, hit{ip: "203.0.113.7", host: "example.com", path: "/", ua: testUA, mode: "-", lang: "-", at: now}.line())
	}

	ingest(a, "tr", lines...)
	mustFlush(t, a)
	if a.lastHeaderWarn != now {
		t.Fatalf("no warning after %d candidates without Sec-Fetch-Mode", webHeaderWarnMin)
	}
	// Rate-limited: the next window within the hour stays quiet.
	now = now.Add(30 * time.Minute)
	ingest(a, "tr", lines...)
	mustFlush(t, a)
	if a.lastHeaderWarn == now {
		t.Fatal("warned again within the hour")
	}
	now = now.Add(31 * time.Minute)
	ingest(a, "tr", lines...)
	mustFlush(t, a)
	if a.lastHeaderWarn != now {
		t.Fatal("no warning after the hour passed")
	}

	// One request carrying the header proves Traefik keeps it: no warning.
	b := testAnalytics(t, analyticsDB(t), &now)
	ingest(b, "tr", append(lines, hit{ip: "203.0.113.8", host: "example.com", path: "/", ua: testUA, at: now}.line())...)
	mustFlush(t, b)
	if !b.lastHeaderWarn.IsZero() {
		t.Fatal("warned although Sec-Fetch-Mode was present")
	}
}
