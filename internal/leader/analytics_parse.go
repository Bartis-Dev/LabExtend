package leader

import (
	"encoding/json"
	"errors"
	"net"
	"net/url"
	"path"
	"strings"
	"time"
)

// Pure helpers for Web Analytics: turning one Traefik JSON access-log line
// into a record and classifying it. No state, no I/O.

// accessRecord is the subset of a Traefik access-log line analytics uses.
type accessRecord struct {
	Host       string // lower-cased, port stripped
	Method     string
	Path       string
	Status     int
	ClientHost string
	UA         string
	Referer    string
	Router     string
	Start      time.Time // zero when absent or unparseable
}

type traefikAccessLine struct {
	ClientHost       string `json:"ClientHost"`
	DownstreamStatus int    `json:"DownstreamStatus"`
	RequestHost      string `json:"RequestHost"`
	RequestMethod    string `json:"RequestMethod"`
	RequestPath      string `json:"RequestPath"`
	RouterName       string `json:"RouterName"`
	StartUTC         string `json:"StartUTC"`
	UserAgent        string `json:"request_User-Agent"`
	Referer          string `json:"request_Referer"`
}

var errNotJSON = errors.New("not a json object")

// parseAccessLine returns ok=false with a nil error for a well-formed line that
// is not an access record (Traefik's own level/msg logs), and an error for
// anything that is not JSON at all.
func parseAccessLine(line string) (accessRecord, bool, error) {
	line = strings.TrimSpace(line)
	if !strings.HasPrefix(line, "{") {
		return accessRecord{}, false, errNotJSON
	}
	var l traefikAccessLine
	if err := json.Unmarshal([]byte(line), &l); err != nil {
		return accessRecord{}, false, err
	}
	if l.RequestHost == "" {
		return accessRecord{}, false, nil
	}
	r := accessRecord{
		Host:       normalizeHost(l.RequestHost),
		Method:     strings.ToUpper(l.RequestMethod),
		Path:       l.RequestPath,
		Status:     l.DownstreamStatus,
		ClientHost: l.ClientHost,
		UA:         l.UserAgent,
		Referer:    l.Referer,
		Router:     l.RouterName,
	}
	if t, err := time.Parse(time.RFC3339Nano, l.StartUTC); err == nil {
		r.Start = t.UTC()
	}
	return r, true, nil
}

func normalizeHost(h string) string {
	h = strings.ToLower(strings.TrimSpace(h))
	if h == "" {
		return ""
	}
	if strings.HasPrefix(h, "[") {
		if host, _, err := net.SplitHostPort(h); err == nil {
			return host
		}
		return strings.Trim(h, "[]")
	}
	if i := strings.LastIndexByte(h, ':'); i >= 0 && strings.Count(h, ":") == 1 {
		h = h[:i]
	}
	return strings.TrimSuffix(h, ".")
}

// Prefixes that are assets, APIs or crawler files rather than pages a person
// reads. Matched with HasPrefix, so "/sitemap" also covers "/sitemap-0.xml".
var nonPagePrefixes = []string{
	"/_next", "/api", "/static", "/assets", "/favicon", "/robots.txt",
	"/sitemap", "/.well-known", "/icon", "/apple-icon", "/opengraph-image",
}

func isPageView(method string, status int, p string) bool {
	if method != "GET" && method != "HEAD" {
		return false
	}
	if status < 200 || status > 399 {
		return false
	}
	if i := strings.IndexAny(p, "?#"); i >= 0 {
		p = p[:i]
	}
	if p == "" || p[0] != '/' {
		return false
	}
	for _, pre := range nonPagePrefixes {
		if strings.HasPrefix(p, pre) {
			return false
		}
	}
	switch strings.ToLower(path.Ext(p)) {
	case "", ".html", ".htm":
		return true
	}
	return false
}

// botMarkers are lower-case substrings. "bot" alone already covers Googlebot,
// bingbot, AhrefsBot, Discordbot, TelegramBot and friends.
var botMarkers = []string{
	"bot", "crawl", "spider", "slurp", "curl", "wget", "python", "go-http",
	"java/", "okhttp", "headless", "lighthouse", "monitor", "uptime", "preview",
	"facebookexternalhit", "telegram", "whatsapp", "scrapy", "httpclient",
	"axios", "node-fetch", "libwww", "pingdom", "phantomjs", "feedfetcher",
}

func isBot(ua string) bool {
	ua = strings.ToLower(strings.TrimSpace(ua))
	if ua == "" {
		return true
	}
	for _, m := range botMarkers {
		if strings.Contains(ua, m) {
			return true
		}
	}
	return false
}

// referrerDomain returns "direct" for no referrer, "" when the referrer must
// not be counted (the site itself, or nothing parseable), else its domain.
func referrerDomain(ref, host string) string {
	if strings.TrimSpace(ref) == "" {
		return "direct"
	}
	u, err := url.Parse(ref)
	if err != nil {
		return ""
	}
	d := strings.TrimPrefix(normalizeHost(u.Host), "www.")
	if d == "" || d == strings.TrimPrefix(normalizeHost(host), "www.") {
		return ""
	}
	return d
}

type uaRule struct {
	name    string
	markers []string
}

// Order matters: Edge, Opera and Samsung all also say "Chrome", Chrome also
// says "Safari", and every iPhone says "like Mac OS X".
var browserRules = []uaRule{
	{"Edge", []string{"edg/", "edga/", "edgios/"}},
	{"Opera", []string{"opr/", "opera"}},
	{"Samsung Internet", []string{"samsungbrowser"}},
	{"Firefox", []string{"firefox/", "fxios/"}},
	{"Chrome", []string{"chrome/", "crios/", "chromium/"}},
	{"Safari", []string{"safari/"}},
}

var osRules = []uaRule{
	{"Windows", []string{"windows"}},
	{"Android", []string{"android"}},
	{"iOS", []string{"iphone", "ipad", "ipod"}},
	{"ChromeOS", []string{"cros"}},
	{"macOS", []string{"macintosh", "mac os x"}},
	{"Linux", []string{"linux"}},
}

func matchUA(ua string, rules []uaRule) string {
	for _, r := range rules {
		for _, m := range r.markers {
			if strings.Contains(ua, m) {
				return r.name
			}
		}
	}
	return "Other"
}

func parseUA(ua string) (browser, os string) {
	ua = strings.ToLower(ua)
	return matchUA(ua, browserRules), matchUA(ua, osRules)
}

func statusClass(code int) string {
	switch {
	case code >= 200 && code < 300:
		return "2xx"
	case code >= 300 && code < 400:
		return "3xx"
	case code >= 400 && code < 500:
		return "4xx"
	case code >= 500 && code < 600:
		return "5xx"
	}
	return ""
}

// isSourceImage matches the image's own name (after the last "/"), so both
// "traefik:v3" and "docker.io/library/traefik:v3" count, but an image that
// merely lives under a "traefik" registry path does not.
func isSourceImage(image, prefix string) bool {
	if prefix == "" || image == "" {
		return false
	}
	name := image
	if i := strings.LastIndexByte(name, '/'); i >= 0 {
		name = name[i+1:]
	}
	return strings.HasPrefix(strings.ToLower(name), strings.ToLower(prefix))
}
