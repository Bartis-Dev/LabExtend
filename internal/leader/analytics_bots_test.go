package leader

import (
	"database/sql"
	"fmt"
	"sort"
	"testing"
	"time"
)

// Visitors that send real browser headers but turn out to be bots.

const (
	edgaUA   = "Mozilla/5.0 (Linux; Android 10; K) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/156.0.0.0 Mobile Safari/537.36 EdgA/156.0.0.0"
	iphoneUA = "Mozilla/5.0 (iPhone; CPU iPhone OS 18_0 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/18.0 Mobile/15E148 Safari/604.1"
)

type nav struct {
	at        time.Time
	path, ref string
}

// feed ingests navigations in order with the clock at each one, flushing
// every few lines so a verdict also has to undo counts already in SQLite.
func feed(t *testing.T, a *webAnalytics, now *time.Time, ip, ua, host string, navs []nav) {
	t.Helper()
	for i, n := range navs {
		*now = n.at
		ingest(a, "tr", hit{ip: ip, host: host, path: n.path, ref: n.ref, ua: ua, at: n.at}.line())
		if i%7 == 6 {
			mustFlush(t, a)
		}
	}
	mustFlush(t, a)
}

// patternA: bursts of the four bartis.me pages every period from start for
// span, by `instances` interleaved schedulers with a few seconds of jitter.
func patternA(start time.Time, period, span time.Duration, instances int) []nav {
	pages := []string{"/", "/legal/contact", "/legal/imprint", "/legal/privacy"}
	var out []nav
	for k := 0; time.Duration(k)*period < span; k++ {
		base := start.Add(time.Duration(k) * period)
		for j := 0; j < instances; j++ {
			b := base.Add(time.Duration(j)*period*13/time.Duration(10*instances) + time.Duration((k*7+j*3)%11)*time.Second)
			for i, p := range pages {
				out = append(out, nav{at: b.Add(time.Duration(i*2) * time.Second), path: p})
			}
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].at.Before(out[j].at) })
	return out
}

func readHourly(t *testing.T, d *sql.DB, host string, hour time.Time) (pv, vis int64) {
	t.Helper()
	err := d.QueryRow(`SELECT pageviews, visitors FROM web_hourly WHERE host=? AND hour=?`, host, hour.Unix()).Scan(&pv, &vis)
	if err != nil && err != sql.ErrNoRows {
		t.Fatal(err)
	}
	return pv, vis
}

func TestWebAnalyticsPeriodicVisitorIsBot(t *testing.T) {
	start := time.Date(2026, 11, 5, 0, 0, 0, 0, time.UTC)
	day := "2026-11-05"
	tests := []struct {
		name      string
		period    time.Duration
		instances int
	}{
		{"36 per hour", 400 * time.Second, 1},
		{"72 per hour", 200 * time.Second, 1},
		{"108 per hour", 133 * time.Second, 1},
		{"two schedulers interleaved", 400 * time.Second, 2},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			d := analyticsDB(t)
			now := start
			a := testAnalytics(t, d, &now)
			first := patternA(start, tt.period, 2*time.Hour, tt.instances)
			feed(t, a, &now, "203.0.113.66", edgaUA, "bartis.me", first)

			// Caught within the first two hours, and everything it had
			// already been counted for is gone again.
			got := readDaily(t, d, "bartis.me", day)
			if want := (dailyRow{requests: int64(len(first)), bots: int64(len(first))}); got != want {
				t.Fatalf("daily after 2h = %+v, want %+v", got, want)
			}
			for _, h := range []time.Time{start, start.Add(time.Hour)} {
				if pv, vis := readHourly(t, d, "bartis.me", h); pv != 0 || vis != 0 {
					t.Errorf("hour %s pv/visitors = %d/%d, want 0/0", h.Format("15:04"), pv, vis)
				}
			}
			for _, c := range [][2]string{{"path", "/"}, {"path", "/legal/privacy"}, {"referrer", "direct"}, {"browser", "Edge"}, {"os", "Android"}} {
				if n := dimCount(t, d, "bartis.me", day, c[0], c[1]); n != 0 {
					t.Errorf("dim %s=%q = %d, want 0", c[0], c[1], n)
				}
			}

			// The rest of the day is bot hits only.
			more := patternA(start.Add(2*time.Hour), tt.period, time.Hour, tt.instances)
			feed(t, a, &now, "203.0.113.66", edgaUA, "bartis.me", more)
			got = readDaily(t, d, "bartis.me", day)
			if got.pageviews != 0 || got.visitors != 0 || got.bots != int64(len(first)+len(more)) {
				t.Fatalf("daily after 3h = %+v, want only bot hits", got)
			}
		})
	}
}

func TestWebAnalyticsBurstVisitorIsBot(t *testing.T) {
	d := analyticsDB(t)
	start := time.Date(2026, 11, 5, 14, 2, 0, 0, time.UTC)
	now := start
	a := testAnalytics(t, d, &now)
	var navs []nav
	at := start
	for i := 0; i < 67; i++ { // a scraper: 67 pages in under an hour, then gone
		navs = append(navs, nav{at: at, path: fmt.Sprintf("/docs/page-%d", i)})
		at = at.Add(time.Duration(40+i%3*10) * time.Second)
	}
	feed(t, a, &now, "198.51.100.77", testUA, "dylaris.com", navs)
	got := readDaily(t, d, "dylaris.com", "2026-11-05")
	if want := (dailyRow{requests: 67, bots: 67}); got != want {
		t.Fatalf("daily = %+v, want %+v", got, want)
	}
	if n := dimCount(t, d, "dylaris.com", "2026-11-05", "path", "/docs/page-0"); n != 0 {
		t.Errorf("scraper path still counted %d times", n)
	}
}

func TestWebAnalyticsHumansStayHuman(t *testing.T) {
	d := analyticsDB(t)
	day0 := time.Date(2026, 11, 5, 0, 0, 0, 0, time.UTC)
	now := day0
	a := testAnalytics(t, d, &now)
	pv := 0

	// One long reading session: 20 pages with uneven reading times.
	gaps := []int{5, 40, 75, 20, 130, 65, 10, 200, 90, 30, 15, 180, 60, 45, 100, 25, 70, 150, 35}
	at := day0.Add(9*time.Hour + 12*time.Minute)
	reader := []nav{{at: at, path: "/", ref: "https://www.google.com/"}}
	for i, g := range gaps {
		at = at.Add(time.Duration(g) * time.Second)
		reader = append(reader, nav{at: at, path: fmt.Sprintf("/blog/%d", i)})
	}
	feed(t, a, &now, "203.0.113.10", testUA, "bartis.me", reader)
	pv += len(reader)

	// Comes back three times a day, each visit straddling a full hour.
	var returning []nav
	for _, v := range []time.Duration{8*time.Hour + 58*time.Minute, 13*time.Hour + 59*time.Minute, 20*time.Hour + 57*time.Minute} {
		for i := 0; i < 5; i++ {
			returning = append(returning, nav{at: day0.Add(v + time.Duration(i*45)*time.Second), path: []string{"/", "/legal/contact"}[i%2]})
		}
	}
	feed(t, a, &now, "203.0.113.11", iphoneUA, "bartis.me", returning)
	pv += len(returning)

	// Two people behind one NAT address, different browsers, same pages.
	for _, ua := range []string{testUA, edgaUA} {
		var navs []nav
		for i := 0; i < 12; i++ {
			navs = append(navs, nav{at: day0.Add(10*time.Hour + time.Duration(i*50)*time.Second), path: "/"})
		}
		feed(t, a, &now, "198.51.100.200", ua, "bartis.me", navs)
		pv += len(navs)
	}

	got := readDaily(t, d, "bartis.me", "2026-11-05")
	if want := (dailyRow{requests: int64(pv), pageviews: int64(pv), visitors: 4}); got != want {
		t.Fatalf("daily = %+v, want %+v", got, want)
	}
}

// A visitor over the daily cap is removed from everything it was counted in,
// also from rows flushed hours ago, and stays a bot across a restart.
func TestWebAnalyticsDailyCapRemovesVisitor(t *testing.T) {
	d := analyticsDB(t)
	day0 := time.Date(2026, 11, 5, 0, 0, 0, 0, time.UTC)
	day := "2026-11-05"
	now := day0
	a := testAnalytics(t, d, &now)
	if a.maxViews != 100 {
		t.Fatalf("maxViews = %d, want 100", a.maxViews)
	}

	human := []nav{{at: day0.Add(30 * time.Minute), path: "/", ref: "https://www.google.com/"}, {at: day0.Add(31 * time.Minute), path: "/a"}}
	feed(t, a, &now, "203.0.113.10", testUA, "bartis.me", human)

	// Five visits, three hours apart, 21 pages each: no rhythm rule fires,
	// only the cap does, on view 101.
	var heavy []nav
	for v := 0; v < 5; v++ {
		start := day0.Add(time.Duration(1+3*v) * time.Hour)
		for i := 0; i < 21; i++ {
			n := nav{at: start.Add(time.Duration(i*20) * time.Second), path: []string{"/a", "/b", "/c"}[i%3]}
			if i == 0 {
				n.ref = "https://news.ycombinator.com/"
			}
			heavy = append(heavy, n)
		}
	}
	feed(t, a, &now, "198.51.100.9", edgaUA, "bartis.me", heavy[:100])
	if got := readDaily(t, d, "bartis.me", day); got.pageviews != 102 || got.visitors != 2 || got.bots != 0 {
		t.Fatalf("at the cap: daily = %+v, want 102 page views, 2 visitors", got)
	}
	for _, v := range a.visitors {
		if len(v.views) > a.maxViews {
			t.Errorf("visitor keeps %d views, more than the cap %d", len(v.views), a.maxViews)
		}
	}

	feed(t, a, &now, "198.51.100.9", edgaUA, "bartis.me", heavy[100:])
	got := readDaily(t, d, "bartis.me", day)
	if want := (dailyRow{requests: 107, pageviews: 2, bots: 105, visitors: 1}); got != want {
		t.Fatalf("over the cap: daily = %+v, want %+v", got, want)
	}
	for _, c := range []struct {
		dim, value string
		want       int64
	}{
		{"path", "/", 1}, {"path", "/a", 1}, {"path", "/b", 0}, {"path", "/c", 0},
		{"referrer", "google.com", 1}, {"referrer", "news.ycombinator.com", 0},
		{"browser", "Chrome", 2}, {"browser", "Edge", 0}, {"os", "Windows", 2}, {"os", "Android", 0},
	} {
		if n := dimCount(t, d, "bartis.me", day, c.dim, c.value); n != c.want {
			t.Errorf("dim %s=%q = %d, want %d", c.dim, c.value, n, c.want)
		}
	}
	if n := countRows(t, d, `SELECT COUNT(*) FROM web_dims WHERE count <= 0`); n != 0 {
		t.Errorf("%d breakdown rows at zero or below", n)
	}
	if pv, vis := readHourly(t, d, "bartis.me", day0); pv != 2 || vis != 1 {
		t.Errorf("hour 00 pv/visitors = %d/%d, want 2/1", pv, vis)
	}
	for v := 0; v < 5; v++ {
		if pv, vis := readHourly(t, d, "bartis.me", day0.Add(time.Duration(1+3*v)*time.Hour)); pv != 0 || vis != 0 {
			t.Errorf("hour of visit %d pv/visitors = %d/%d, want 0/0", v, pv, vis)
		}
	}
	for _, v := range a.visitors {
		if v.bot && v.views != nil {
			t.Error("a bot visitor still holds its page views")
		}
	}
	if n := countRows(t, d, `SELECT COUNT(*) FROM web_visitor_hashes WHERE day=? AND bot=1`, day); n != 1 {
		t.Fatalf("bot hashes = %d, want 1", n)
	}

	// After a restart the verdict holds: one more page is a bot hit.
	now = day0.Add(14 * time.Hour)
	b := testAnalytics(t, d, &now)
	feed(t, b, &now, "198.51.100.9", edgaUA, "bartis.me", []nav{{at: now, path: "/"}})
	if got := readDaily(t, d, "bartis.me", day); got.pageviews != 2 || got.visitors != 1 || got.bots != 106 {
		t.Fatalf("after restart: daily = %+v, want 2 page views, 1 visitor, 106 bot hits", got)
	}

	// Next day the hashes, bot verdicts included, are gone as before.
	now = day0.Add(24*time.Hour + 5*time.Minute)
	ingest(b, "tr", hit{ip: "198.51.100.9", host: "bartis.me", path: "/", ua: edgaUA, at: now}.line())
	mustFlush(t, b)
	if n := countRows(t, d, `SELECT COUNT(*) FROM web_visitor_hashes WHERE day=?`, day); n != 0 {
		t.Errorf("day hashes after rollover = %d, want 0", n)
	}
	if got := readDaily(t, d, "bartis.me", "2026-11-06"); got.pageviews != 1 || got.visitors != 1 {
		t.Errorf("next day the visitor starts fresh: daily = %+v", got)
	}
	assertNoIPStored(t, d, "198.51.100.9", "203.0.113.10")
}

func TestWebAnalyticsTrackedVisitorBound(t *testing.T) {
	d := analyticsDB(t)
	now := time.Date(2026, 11, 5, 10, 0, 0, 0, time.UTC)
	a := testAnalytics(t, d, &now)
	n := webMaxTrackedVisitors + 10
	lines := make([]string, 0, n)
	for i := 0; i < n; i++ {
		lines = append(lines, hit{ip: fmt.Sprintf("10.%d.%d.%d", i>>16&255, i>>8&255, i&255), host: "example.com", path: "/", ua: testUA, at: now}.line())
	}
	ingest(a, "tr", lines...)
	mustFlush(t, a)
	if len(a.visitors) != webMaxTrackedVisitors {
		t.Errorf("tracked visitors = %d, want the bound %d", len(a.visitors), webMaxTrackedVisitors)
	}
	// Visitors past the bound are still counted, just not checked.
	if got := readDaily(t, d, "example.com", "2026-11-05"); got.visitors != int64(n) || got.pageviews != int64(n) {
		t.Errorf("daily = %+v, want %d page views and visitors", got, n)
	}
	now = now.Add(24 * time.Hour)
	ingest(a, "tr", hit{ip: "10.0.0.1", host: "example.com", path: "/", ua: testUA, at: now}.line())
	if len(a.visitors) != 1 {
		t.Errorf("tracked visitors on the next day = %d, want 1", len(a.visitors))
	}
}
