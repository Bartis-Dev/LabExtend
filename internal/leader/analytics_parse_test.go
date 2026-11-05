package leader

import (
	"testing"
	"time"
)

func TestParseAccessLine(t *testing.T) {
	full := `{"ClientHost":"203.0.113.7","DownstreamStatus":200,"RequestHost":"Example.COM:443","RequestMethod":"GET","RequestPath":"/blog/post","RequestProtocol":"HTTP/2.0","Duration":1234,"StartUTC":"2026-11-05T10:15:30.123456789Z","RouterName":"site@docker","ServiceName":"site@docker","entryPointName":"websecure","request_User-Agent":"Mozilla/5.0","request_Referer":"https://news.ycombinator.com/item?id=1"}`
	tests := []struct {
		name    string
		line    string
		wantOK  bool
		wantErr bool
		check   func(t *testing.T, r accessRecord)
	}{
		{
			name: "full access record", line: full, wantOK: true,
			check: func(t *testing.T, r accessRecord) {
				if r.Host != "example.com" {
					t.Errorf("host = %q, want example.com", r.Host)
				}
				if r.Status != 200 || r.Method != "GET" || r.Path != "/blog/post" {
					t.Errorf("status/method/path = %d %q %q", r.Status, r.Method, r.Path)
				}
				if r.ClientHost != "203.0.113.7" || r.UA != "Mozilla/5.0" || r.Router != "site@docker" {
					t.Errorf("client/ua/router = %q %q %q", r.ClientHost, r.UA, r.Router)
				}
				if r.Referer != "https://news.ycombinator.com/item?id=1" {
					t.Errorf("referer = %q", r.Referer)
				}
				want := time.Date(2026, 11, 5, 10, 15, 30, 123456789, time.UTC)
				if !r.Start.Equal(want) {
					t.Errorf("start = %v, want %v", r.Start, want)
				}
			},
		},
		{
			name:   "traefik own log line is ignored, not an error",
			line:   `{"level":"info","msg":"Configuration loaded","time":"2026-11-05T10:00:00Z"}`,
			wantOK: false,
		},
		{name: "not json is a parse error", line: `time="x" level=info msg="hi"`, wantErr: true},
		{name: "truncated json is a parse error", line: `{"RequestHost":"a.com","Request...`, wantErr: true},
		{name: "empty line is a parse error", line: ``, wantErr: true},
		{
			name: "missing StartUTC leaves zero time", wantOK: true,
			line: `{"RequestHost":"a.com","RequestMethod":"GET","RequestPath":"/","DownstreamStatus":200,"RouterName":"r"}`,
			check: func(t *testing.T, r accessRecord) {
				if !r.Start.IsZero() {
					t.Errorf("start = %v, want zero", r.Start)
				}
				if r.Referer != "" {
					t.Errorf("referer = %q, want empty", r.Referer)
				}
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r, ok, err := parseAccessLine(tt.line)
			if (err != nil) != tt.wantErr {
				t.Fatalf("err = %v, wantErr %v", err, tt.wantErr)
			}
			if ok != tt.wantOK {
				t.Fatalf("ok = %v, want %v", ok, tt.wantOK)
			}
			if tt.check != nil {
				tt.check(t, r)
			}
		})
	}
}

func TestNormalizeHost(t *testing.T) {
	tests := []struct{ in, want string }{
		{"Example.com", "example.com"},
		{"example.com:8443", "example.com"},
		{"example.com.", "example.com"},
		{"[2001:db8::1]:443", "2001:db8::1"},
		{"[2001:db8::1]", "2001:db8::1"},
		{"", ""},
		{"  a.com ", "a.com"},
	}
	for _, tt := range tests {
		if got := normalizeHost(tt.in); got != tt.want {
			t.Errorf("normalizeHost(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestIsPageView(t *testing.T) {
	tests := []struct {
		method string
		status int
		path   string
		want   bool
	}{
		{"GET", 200, "/", true},
		{"GET", 200, "/blog/hello-world", true},
		{"HEAD", 200, "/about", true},
		{"GET", 304, "/about", true},
		{"GET", 200, "/index.html", true},
		{"GET", 200, "/old/page.htm", true},
		{"GET", 200, "/docs/v1.2/intro", true}, // dot in a directory, not the file
		{"POST", 200, "/contact", false},
		{"GET", 404, "/missing", false},
		{"GET", 500, "/", false},
		{"GET", 199, "/", false},
		{"GET", 400, "/", false},
		{"GET", 200, "/style.css", false},
		{"GET", 200, "/img/logo.png", false},
		{"GET", 200, "/_next/static/chunk.js", false},
		{"GET", 200, "/_next/data/build/page", false},
		{"GET", 200, "/api/users", false},
		{"GET", 200, "/static/x", false},
		{"GET", 200, "/assets/x", false},
		{"GET", 200, "/favicon.ico", false},
		{"GET", 200, "/robots.txt", false},
		{"GET", 200, "/sitemap.xml", false},
		{"GET", 200, "/sitemap-0", false},
		{"GET", 200, "/.well-known/acme-challenge/x", false},
		{"GET", 200, "/icon", false},
		{"GET", 200, "/apple-icon", false},
		{"GET", 200, "/opengraph-image", false},
		{"GET", 200, "/blog?page=2", true}, // stray query is stripped before the check
		{"GET", 200, "", false},
	}
	for _, tt := range tests {
		if got := isPageView(tt.method, tt.status, tt.path); got != tt.want {
			t.Errorf("isPageView(%s %d %q) = %v, want %v", tt.method, tt.status, tt.path, got, tt.want)
		}
	}
}

func TestIsBot(t *testing.T) {
	tests := []struct {
		ua   string
		want bool
	}{
		{"", true},
		{"   ", true},
		{"Mozilla/5.0 (compatible; Googlebot/2.1; +http://www.google.com/bot.html)", true},
		{"Mozilla/5.0 (compatible; bingbot/2.0)", true},
		{"Mozilla/5.0 (compatible; AhrefsBot/7.0)", true},
		{"Mozilla/5.0 (compatible; Yahoo! Slurp)", true},
		{"curl/8.5.0", true},
		{"Wget/1.21", true},
		{"python-requests/2.31", true},
		{"Go-http-client/1.1", true},
		{"Java/17.0.1", true},
		{"okhttp/4.12.0", true},
		{"Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) HeadlessChrome/120.0 Safari/537.36", true},
		{"Mozilla/5.0 Chrome-Lighthouse", true},
		{"Uptime-Kuma/1.23", true},
		{"facebookexternalhit/1.1", true},
		{"Mozilla/5.0 (compatible; Discordbot/2.0)", true},
		{"TelegramBot (like TwitterBot)", true},
		{"WhatsApp/2.23", true},
		{"Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/130.0.0.0 Safari/537.36", false},
		{"Mozilla/5.0 (iPhone; CPU iPhone OS 17_0 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/17.0 Mobile/15E148 Safari/604.1", false},
	}
	for _, tt := range tests {
		if got := isBot(tt.ua); got != tt.want {
			t.Errorf("isBot(%q) = %v, want %v", tt.ua, got, tt.want)
		}
	}
}

func TestReferrerDomain(t *testing.T) {
	tests := []struct{ ref, host, want string }{
		{"", "example.com", "direct"},
		{"https://news.ycombinator.com/item?id=1", "example.com", "news.ycombinator.com"},
		{"https://www.google.com/", "example.com", "google.com"},
		{"https://GOOGLE.com:443/search", "example.com", "google.com"},
		{"https://example.com/other", "example.com", ""},     // internal navigation
		{"https://www.example.com/other", "example.com", ""}, // www variant is still the site itself
		{"https://example.com/", "www.example.com", ""},
		{"android-app://com.reddit.frontpage/", "example.com", "com.reddit.frontpage"},
		{"not a url", "example.com", ""},
		{"/relative/path", "example.com", ""},
	}
	for _, tt := range tests {
		if got := referrerDomain(tt.ref, tt.host); got != tt.want {
			t.Errorf("referrerDomain(%q, %q) = %q, want %q", tt.ref, tt.host, got, tt.want)
		}
	}
}

func TestParseUA(t *testing.T) {
	tests := []struct{ ua, browser, os string }{
		{"Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/130.0.0.0 Safari/537.36", "Chrome", "Windows"},
		{"Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/130.0.0.0 Safari/537.36 Edg/130.0.0.0", "Edge", "Windows"},
		{"Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/130.0.0.0 Safari/537.36 OPR/114.0.0.0", "Opera", "Windows"},
		{"Mozilla/5.0 (Windows NT 10.0; Win64; x64; rv:132.0) Gecko/20100101 Firefox/132.0", "Firefox", "Windows"},
		{"Mozilla/5.0 (Macintosh; Intel Mac OS X 14_7) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/18.0 Safari/605.1.15", "Safari", "macOS"},
		{"Mozilla/5.0 (iPhone; CPU iPhone OS 18_0 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/18.0 Mobile/15E148 Safari/604.1", "Safari", "iOS"},
		{"Mozilla/5.0 (iPhone; CPU iPhone OS 18_0 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) CriOS/130.0 Mobile/15E148 Safari/604.1", "Chrome", "iOS"},
		{"Mozilla/5.0 (iPad; CPU OS 17_0 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) FxiOS/132.0 Mobile/15E148 Safari/605.1.15", "Firefox", "iOS"},
		{"Mozilla/5.0 (Linux; Android 14; SM-S918B) AppleWebKit/537.36 (KHTML, like Gecko) SamsungBrowser/26.0 Chrome/122.0.0.0 Mobile Safari/537.36", "Samsung Internet", "Android"},
		{"Mozilla/5.0 (Linux; Android 14; Pixel 8) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/130.0.0.0 Mobile Safari/537.36", "Chrome", "Android"},
		{"Mozilla/5.0 (X11; Linux x86_64; rv:132.0) Gecko/20100101 Firefox/132.0", "Firefox", "Linux"},
		{"Mozilla/5.0 (X11; CrOS x86_64 14541.0.0) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/130.0.0.0 Safari/537.36", "Chrome", "ChromeOS"},
		{"SomethingElse/1.0", "Other", "Other"},
	}
	for _, tt := range tests {
		b, o := parseUA(tt.ua)
		if b != tt.browser || o != tt.os {
			t.Errorf("parseUA(%q) = (%q, %q), want (%q, %q)", tt.ua, b, o, tt.browser, tt.os)
		}
	}
}

func TestStatusClass(t *testing.T) {
	tests := []struct {
		code int
		want string
	}{{200, "2xx"}, {204, "2xx"}, {301, "3xx"}, {404, "4xx"}, {503, "5xx"}, {101, ""}, {0, ""}, {600, ""}}
	for _, tt := range tests {
		if got := statusClass(tt.code); got != tt.want {
			t.Errorf("statusClass(%d) = %q, want %q", tt.code, got, tt.want)
		}
	}
}

func TestIsSourceImage(t *testing.T) {
	tests := []struct {
		image, prefix string
		want          bool
	}{
		{"traefik:v3.7", "traefik", true},
		{"traefik:v3.7@sha256:abc", "traefik", true},
		{"docker.io/library/traefik:v3.7", "traefik", true},
		{"Traefik:latest", "traefik", true},
		{"ghcr.io/org/traefik-custom:1", "traefik", true},
		{"nginx:1.27", "traefik", false},
		{"ghcr.io/traefik/whoami:1", "traefik", false}, // registry path mentions traefik, image does not
		{"", "traefik", false},
		{"caddy:2", "", false}, // an empty prefix never matches everything
	}
	for _, tt := range tests {
		if got := isSourceImage(tt.image, tt.prefix); got != tt.want {
			t.Errorf("isSourceImage(%q, %q) = %v, want %v", tt.image, tt.prefix, got, tt.want)
		}
	}
}
