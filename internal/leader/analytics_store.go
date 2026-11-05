package leader

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	pb "github.com/Bartis-Dev/LabExtend/internal/grpc/pb"
)

// webAnalytics turns Traefik access-log lines (which already arrive as ordinary
// container logs) into per-host page-view statistics.
//
// Ingest runs on the agent's gRPC receive goroutine, so it only touches
// in-memory maps under one mutex; SQLite is written by Flush on its own
// ticker. Privacy: no client address ever leaves Ingest. A visitor is the
// sha256 of (day salt, ClientHost, User-Agent); salts and hashes of a finished
// UTC day are deleted on the first flush of the next day, keeping only the
// count.
type webAnalytics struct {
	db            *sql.DB
	imageOf       func(nodeID, containerID string) string
	imagePrefix   string
	retentionDays int
	now           func() time.Time

	parseErrors atomic.Uint64

	// flushMu serialises Flush (ticker vs. shutdown) and guards maintainedDay.
	flushMu sync.Mutex

	mu        sync.Mutex
	saltDay   string
	salt      []byte
	saltDirty bool
	pend      *webPending

	// maintainedDay is the UTC day whose rollover work (deleting old salts and
	// hashes, capping closed days, retention) has already run.
	maintainedDay string
}

type webDayKey struct{ host, day string }
type webHourKey struct {
	host string
	hour int64
}
type webDimKey struct{ host, day, dim, value string }
type webHashKey struct {
	host, day string
	hash      [sha256.Size]byte
}
type webCounts struct{ requests, pageviews, bots int64 }

type webPending struct {
	daily  map[webDayKey]*webCounts
	hourly map[webHourKey]*webCounts
	dims   map[webDimKey]int64
	hashes map[webHashKey]int64 // → first hour seen
}

func newWebPending() *webPending {
	return &webPending{
		daily:  map[webDayKey]*webCounts{},
		hourly: map[webHourKey]*webCounts{},
		dims:   map[webDimKey]int64{},
		hashes: map[webHashKey]int64{},
	}
}

const (
	webDayFormat     = "2006-01-02"
	webClosedDimCap  = 50   // per (host, day, dim) once the day is over
	webTodayDimCap   = 1000 // safety bound for the running day
	webHourlyKeep    = 48 * time.Hour
	webMaxPathLength = 512
)

func newWebAnalytics(db *sql.DB, imageOf func(nodeID, containerID string) string, imagePrefix string, retentionDays int) *webAnalytics {
	if retentionDays <= 0 {
		retentionDays = 400
	}
	return &webAnalytics{
		db:            db,
		imageOf:       imageOf,
		imagePrefix:   imagePrefix,
		retentionDays: retentionDays,
		now:           time.Now,
		pend:          newWebPending(),
	}
}

// Load picks up today's salt after a restart so returning visitors are not
// counted twice on the same day.
func (a *webAnalytics) Load(ctx context.Context) error {
	day := a.now().UTC().Format(webDayFormat)
	var salt []byte
	err := a.db.QueryRowContext(ctx, `SELECT salt FROM web_visitor_salts WHERE day = ?`, day).Scan(&salt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("load analytics salt: %w", err)
	}
	a.mu.Lock()
	a.saltDay, a.salt, a.saltDirty = day, salt, false
	a.mu.Unlock()
	return nil
}

// Ingest is called for every LogBatch. Lines from non-Traefik containers cost
// one map lookup per container per batch.
func (a *webAnalytics) Ingest(nodeID string, batch *pb.LogBatch) {
	defer func() {
		// A panic here would take down the agent's gRPC stream handler, and
		// with it the whole leader. Statistics are not worth that.
		if r := recover(); r != nil {
			a.parseErrors.Add(1)
			slog.Error("web analytics: ingest panic", "err", r)
		}
	}()
	lastCID, lastMatch := "", false
	for _, l := range batch.Lines {
		if l.ContainerId != lastCID {
			lastCID = l.ContainerId
			lastMatch = isSourceImage(a.imageOf(nodeID, l.ContainerId), a.imagePrefix)
		}
		if lastMatch {
			a.record(l.Line)
		}
	}
}

func (a *webAnalytics) record(line string) {
	r, ok, err := parseAccessLine(line)
	if err != nil {
		a.parseErrors.Add(1)
		return
	}
	// No router means Traefik matched nothing (scanners with made-up Host
	// headers); @internal is Traefik's own redirect/dashboard/ping routers.
	if !ok || r.Host == "" || r.Router == "" || strings.HasSuffix(r.Router, "@internal") {
		return
	}
	now := a.now().UTC()
	t := r.Start
	if t.IsZero() || t.After(now.Add(5*time.Minute)) {
		t = now
	}
	day := t.Format(webDayFormat)
	hour := t.Truncate(time.Hour).Unix()

	a.mu.Lock()
	defer a.mu.Unlock()
	p := a.pend
	dc := p.daily[webDayKey{r.Host, day}]
	if dc == nil {
		dc = &webCounts{}
		p.daily[webDayKey{r.Host, day}] = dc
	}
	hc := p.hourly[webHourKey{r.Host, hour}]
	if hc == nil {
		hc = &webCounts{}
		p.hourly[webHourKey{r.Host, hour}] = hc
	}
	dc.requests++
	hc.requests++
	if sc := statusClass(r.Status); sc != "" {
		p.dims[webDimKey{r.Host, day, "status", sc}]++
	}
	if isBot(r.UA) {
		dc.bots++
		hc.bots++
		return
	}
	if !isPageView(r.Method, r.Status, r.Path) {
		return
	}
	dc.pageviews++
	hc.pageviews++

	pth := r.Path
	if i := strings.IndexAny(pth, "?#"); i >= 0 {
		pth = pth[:i]
	}
	if len(pth) > webMaxPathLength {
		pth = pth[:webMaxPathLength]
	}
	p.dims[webDimKey{r.Host, day, "path", pth}]++
	if ref := referrerDomain(r.Referer, r.Host); ref != "" {
		p.dims[webDimKey{r.Host, day, "referrer", ref}]++
	}
	browser, os := parseUA(r.UA)
	p.dims[webDimKey{r.Host, day, "browser", browser}]++
	p.dims[webDimKey{r.Host, day, "os", os}]++

	// Visitors only for today: yesterday's salt may already be gone, and
	// minting a fresh one for a past day would defeat the rotation.
	a.ensureSaltLocked(now)
	if day != a.saltDay {
		return
	}
	h := sha256.New()
	h.Write(a.salt)
	h.Write([]byte(r.ClientHost))
	h.Write([]byte{0})
	h.Write([]byte(r.UA))
	var sum [sha256.Size]byte
	copy(sum[:], h.Sum(nil))
	k := webHashKey{r.Host, day, sum}
	if first, seen := p.hashes[k]; !seen || hour < first {
		p.hashes[k] = hour
	}
}

func (a *webAnalytics) ensureSaltLocked(now time.Time) {
	day := now.Format(webDayFormat)
	if a.saltDay == day {
		return
	}
	salt := make([]byte, 32)
	if _, err := rand.Read(salt); err != nil {
		// crypto/rand does not fail on supported platforms; refuse to hash
		// with a predictable salt if it ever does.
		a.saltDay, a.salt = "", nil
		return
	}
	a.saltDay, a.salt, a.saltDirty = day, salt, true
}

// Run flushes every interval until ctx is done.
func (a *webAnalytics) Run(ctx context.Context, interval time.Duration) {
	t := time.NewTicker(interval)
	defer t.Stop()
	var lastErrs uint64
	for {
		select {
		case <-ctx.Done():
			return // the final flush is leader.Run's, before it closes the DB
		case <-t.C:
			if err := a.Flush(ctx); err != nil {
				slog.Warn("web analytics: flush failed", "err", err)
			}
			if n := a.parseErrors.Load(); n != lastErrs {
				slog.Debug("web analytics: unparseable lines", "total", n, "new", n-lastErrs)
				lastErrs = n
			}
		}
	}
}

// Flush writes the pending aggregates in one transaction, then runs the
// once-a-day rollover. A failed write drops that interval's counts.
// ponytail: dropped rather than merged back; merge if SQLite errors ever become routine.
func (a *webAnalytics) Flush(ctx context.Context) error {
	a.flushMu.Lock()
	defer a.flushMu.Unlock()
	now := a.now().UTC()
	today := now.Format(webDayFormat)

	a.mu.Lock()
	p := a.pend
	a.pend = newWebPending()
	saltDay, salt, saltDirty := a.saltDay, a.salt, a.saltDirty
	a.saltDirty = false
	a.mu.Unlock()

	if err := a.writePending(ctx, p, today, saltDay, salt, saltDirty); err != nil {
		return err
	}
	if a.maintainedDay != today {
		if err := a.rollover(ctx, now); err != nil {
			return err
		}
		a.maintainedDay = today
	}
	return nil
}

func (a *webAnalytics) writePending(ctx context.Context, p *webPending, today, saltDay string, salt []byte, saltDirty bool) error {
	empty := len(p.daily) == 0 && len(p.hashes) == 0 && !saltDirty
	tx, err := a.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("analytics begin: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck

	if saltDirty && saltDay != "" {
		if _, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO web_visitor_salts (day, salt) VALUES (?, ?)`, saltDay, salt); err != nil {
			return fmt.Errorf("analytics salt: %w", err)
		}
	}
	for k, c := range p.daily {
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO web_daily (host, day, requests, pageviews, bot_hits) VALUES (?, ?, ?, ?, ?)
			ON CONFLICT(host, day) DO UPDATE SET
				requests  = requests  + excluded.requests,
				pageviews = pageviews + excluded.pageviews,
				bot_hits  = bot_hits  + excluded.bot_hits`,
			k.host, k.day, c.requests, c.pageviews, c.bots); err != nil {
			return fmt.Errorf("analytics daily: %w", err)
		}
	}
	for k, c := range p.hourly {
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO web_hourly (host, hour, requests, pageviews, bot_hits) VALUES (?, ?, ?, ?, ?)
			ON CONFLICT(host, hour) DO UPDATE SET
				requests  = requests  + excluded.requests,
				pageviews = pageviews + excluded.pageviews,
				bot_hits  = bot_hits  + excluded.bot_hits`,
			k.host, k.hour, c.requests, c.pageviews, c.bots); err != nil {
			return fmt.Errorf("analytics hourly: %w", err)
		}
	}
	for k, n := range p.dims {
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO web_dims (host, day, dim, value, count) VALUES (?, ?, ?, ?, ?)
			ON CONFLICT(host, day, dim, value) DO UPDATE SET count = count + excluded.count`,
			k.host, k.day, k.dim, k.value, n); err != nil {
			return fmt.Errorf("analytics dims: %w", err)
		}
	}
	days := map[webDayKey]bool{}
	hours := map[webHourKey]bool{}
	for k, hour := range p.hashes {
		if _, err := tx.ExecContext(ctx,
			`INSERT OR IGNORE INTO web_visitor_hashes (host, day, hash, hour) VALUES (?, ?, ?, ?)`,
			k.host, k.day, k.hash[:], hour); err != nil {
			return fmt.Errorf("analytics hashes: %w", err)
		}
		days[webDayKey{k.host, k.day}] = true
		hours[webHourKey{k.host, hour}] = true
	}
	for k := range days {
		if _, err := tx.ExecContext(ctx, `
			UPDATE web_daily SET visitors = (SELECT COUNT(*) FROM web_visitor_hashes WHERE host = ? AND day = ?)
			WHERE host = ? AND day = ?`, k.host, k.day, k.host, k.day); err != nil {
			return fmt.Errorf("analytics daily visitors: %w", err)
		}
	}
	for k := range hours {
		if _, err := tx.ExecContext(ctx, `
			UPDATE web_hourly SET visitors = (SELECT COUNT(*) FROM web_visitor_hashes WHERE host = ? AND hour = ?)
			WHERE host = ? AND hour = ?`, k.host, k.hour, k.host, k.hour); err != nil {
			return fmt.Errorf("analytics hourly visitors: %w", err)
		}
	}
	if len(p.dims) > 0 {
		if err := capDims(ctx, tx, `day = ?`, today, webTodayDimCap); err != nil {
			return err
		}
	}
	if !empty {
		cutoff := a.now().Add(-webHourlyKeep).Truncate(time.Hour).Unix()
		if _, err := tx.ExecContext(ctx, `DELETE FROM web_hourly WHERE hour < ?`, cutoff); err != nil {
			return fmt.Errorf("analytics hourly retention: %w", err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("analytics commit: %w", err)
	}
	return nil
}

// rollover runs once per UTC day: forget every salt and hash of finished days,
// cap their breakdowns, apply retention.
func (a *webAnalytics) rollover(ctx context.Context, now time.Time) error {
	today := now.Format(webDayFormat)
	cutoff := now.AddDate(0, 0, -a.retentionDays).Format(webDayFormat)
	tx, err := a.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("analytics rollover begin: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck
	for _, q := range []struct {
		sql string
		arg string
	}{
		{`DELETE FROM web_visitor_hashes WHERE day < ?`, today},
		{`DELETE FROM web_visitor_salts WHERE day < ?`, today},
		{`DELETE FROM web_daily WHERE day < ?`, cutoff},
		{`DELETE FROM web_dims WHERE day < ?`, cutoff},
	} {
		if _, err := tx.ExecContext(ctx, q.sql, q.arg); err != nil {
			return fmt.Errorf("analytics rollover: %w", err)
		}
	}
	if err := capDims(ctx, tx, `day < ?`, today, webClosedDimCap); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("analytics rollover commit: %w", err)
	}
	return nil
}

// capDims keeps the top `limit` values per (host, day, dim) among the rows
// matching where. where is a constant from this file, never user input.
func capDims(ctx context.Context, tx *sql.Tx, where, arg string, limit int) error {
	_, err := tx.ExecContext(ctx, `
		DELETE FROM web_dims WHERE rowid IN (
			SELECT rowid FROM (
				SELECT rowid, ROW_NUMBER() OVER (
					PARTITION BY host, day, dim ORDER BY count DESC, value
				) AS rn
				FROM web_dims WHERE `+where+`
			) WHERE rn > ?
		)`, arg, limit)
	if err != nil {
		return fmt.Errorf("analytics cap dims: %w", err)
	}
	return nil
}

// ─── queries ────────────────────────────────────────────────────────────────

type webHost struct {
	Host         string `json:"host"`
	LastDay      string `json:"last_day"`
	PageViews30d int64  `json:"pageviews_30d"`
}

// Hosts lists every host with data, busiest (last 30 days) first.
func (a *webAnalytics) Hosts(ctx context.Context) ([]webHost, error) {
	since := a.now().UTC().AddDate(0, 0, -29).Format(webDayFormat)
	rows, err := a.db.QueryContext(ctx, `
		SELECT host, MAX(day), COALESCE(SUM(CASE WHEN day >= ? THEN pageviews END), 0) AS pv
		FROM web_daily GROUP BY host ORDER BY pv DESC, host`, since)
	if err != nil {
		return nil, fmt.Errorf("analytics hosts: %w", err)
	}
	defer rows.Close()
	out := []webHost{}
	for rows.Next() {
		var h webHost
		if err := rows.Scan(&h.Host, &h.LastDay, &h.PageViews30d); err != nil {
			return nil, err
		}
		out = append(out, h)
	}
	return out, rows.Err()
}

type webTotals struct {
	Requests  int64 `json:"requests"`
	PageViews int64 `json:"pageviews"`
	Visitors  int64 `json:"visitors"`
	BotHits   int64 `json:"bot_hits"`
}

type webPoint struct {
	TS        int64 `json:"ts"` // unix seconds, start of the hour/day
	PageViews int64 `json:"pageviews"`
	Visitors  int64 `json:"visitors"`
}

type webCount struct {
	Value string `json:"value"`
	Count int64  `json:"count"`
}

type webReport struct {
	Host      string     `json:"host"`
	Range     string     `json:"range"`
	Bucket    string     `json:"bucket"` // "hour" | "day"
	Totals    webTotals  `json:"totals"`
	Series    []webPoint `json:"series"`
	Paths     []webCount `json:"paths"`
	Referrers []webCount `json:"referrers"`
	Browsers  []webCount `json:"browsers"`
	OS        []webCount `json:"os"`
	Status    []webCount `json:"status"`
}

var webRanges = map[string]int{"24h": 0, "7d": 7, "30d": 30, "90d": 90, "365d": 365}

var errUnknownRange = errors.New("unknown range")

// Report builds the summary for one host. "24h" reads the hourly table;
// breakdowns are per day, so for "24h" they cover today and yesterday (UTC).
// Visitors over several days are the sum of the daily counts: a visitor cannot
// be recognised across days, by design.
func (a *webAnalytics) Report(ctx context.Context, host, rng string) (*webReport, error) {
	days, ok := webRanges[rng]
	if !ok {
		return nil, errUnknownRange
	}
	now := a.now().UTC()
	r := &webReport{Host: host, Range: rng}

	var firstDay string
	if days == 0 {
		r.Bucket = "hour"
		firstDay = now.AddDate(0, 0, -1).Format(webDayFormat)
		end := now.Truncate(time.Hour)
		start := end.Add(-23 * time.Hour)
		byHour := map[int64]webPoint{}
		rows, err := a.db.QueryContext(ctx, `
			SELECT hour, requests, pageviews, bot_hits, visitors FROM web_hourly
			WHERE host = ? AND hour >= ? AND hour <= ?`, host, start.Unix(), end.Unix())
		if err != nil {
			return nil, fmt.Errorf("analytics report hourly: %w", err)
		}
		for rows.Next() {
			var p webPoint
			var req, bots int64
			if err := rows.Scan(&p.TS, &req, &p.PageViews, &bots, &p.Visitors); err != nil {
				rows.Close()
				return nil, err
			}
			byHour[p.TS] = p
			r.Totals.Requests += req
			r.Totals.BotHits += bots
			r.Totals.PageViews += p.PageViews
			r.Totals.Visitors += p.Visitors
		}
		rows.Close()
		for t := start; !t.After(end); t = t.Add(time.Hour) {
			p := byHour[t.Unix()]
			p.TS = t.Unix()
			r.Series = append(r.Series, p)
		}
	} else {
		r.Bucket = "day"
		today := now.Truncate(24 * time.Hour)
		start := today.AddDate(0, 0, -(days - 1))
		firstDay = start.Format(webDayFormat)
		byDay := map[string]webPoint{}
		rows, err := a.db.QueryContext(ctx, `
			SELECT day, requests, pageviews, bot_hits, visitors FROM web_daily
			WHERE host = ? AND day >= ?`, host, firstDay)
		if err != nil {
			return nil, fmt.Errorf("analytics report daily: %w", err)
		}
		for rows.Next() {
			var day string
			var p webPoint
			var req, bots int64
			if err := rows.Scan(&day, &req, &p.PageViews, &bots, &p.Visitors); err != nil {
				rows.Close()
				return nil, err
			}
			byDay[day] = p
			r.Totals.Requests += req
			r.Totals.BotHits += bots
			r.Totals.PageViews += p.PageViews
			r.Totals.Visitors += p.Visitors
		}
		rows.Close()
		for t := start; !t.After(today); t = t.AddDate(0, 0, 1) {
			p := byDay[t.Format(webDayFormat)]
			p.TS = t.Unix()
			r.Series = append(r.Series, p)
		}
	}

	dims := map[string]*[]webCount{
		"path": &r.Paths, "referrer": &r.Referrers, "browser": &r.Browsers, "os": &r.OS, "status": &r.Status,
	}
	for dim, dst := range dims {
		rows, err := a.db.QueryContext(ctx, `
			SELECT value, SUM(count) AS n FROM web_dims
			WHERE host = ? AND dim = ? AND day >= ?
			GROUP BY value ORDER BY n DESC, value LIMIT ?`, host, dim, firstDay, webClosedDimCap)
		if err != nil {
			return nil, fmt.Errorf("analytics report %s: %w", dim, err)
		}
		out := []webCount{}
		for rows.Next() {
			var c webCount
			if err := rows.Scan(&c.Value, &c.Count); err != nil {
				rows.Close()
				return nil, err
			}
			out = append(out, c)
		}
		rows.Close()
		*dst = out
	}
	// Status classes read best in their natural order, not by size.
	sort.Slice(r.Status, func(i, j int) bool { return r.Status[i].Value < r.Status[j].Value })
	return r, nil
}
