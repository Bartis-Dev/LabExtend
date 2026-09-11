package leader

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

// The audit log's source_ip, the session row's ip and the per-IP login rate
// limit all read RemoteAddr, so whoever controls RemoteAddr controls all three.
func TestRealIP(t *testing.T) {
	tests := []struct {
		name        string
		trusted     []string
		remoteAddr  string
		xff         string
		realIPHdr   string
		wantAddr    string
		wantXFFGone bool
	}{
		{
			name:        "no proxies configured: header ignored and stripped",
			trusted:     nil,
			remoteAddr:  "203.0.113.9:51000",
			xff:         "10.9.9.9",
			wantAddr:    "203.0.113.9:51000",
			wantXFFGone: true,
		},
		{
			name:        "untrusted peer cannot spoof",
			trusted:     []string{"10.0.0.0/8"},
			remoteAddr:  "203.0.113.9:51000",
			xff:         "10.0.0.5",
			wantAddr:    "203.0.113.9:51000",
			wantXFFGone: true,
		},
		{
			name:       "trusted proxy is believed",
			trusted:    []string{"10.0.0.0/8"},
			remoteAddr: "10.0.0.2:40000",
			xff:        "198.51.100.7",
			wantAddr:   "198.51.100.7:0",
		},
		{
			// Traefik appends, so the leftmost entry is whatever the client
			// sent. Walking from the right past our own proxies yields the
			// address the outermost proxy we trust actually saw.
			name:       "client-prepended entry is not taken",
			trusted:    []string{"10.0.0.0/8"},
			remoteAddr: "10.0.0.2:40000",
			xff:        "1.2.3.4, 198.51.100.7, 10.0.0.2",
			wantAddr:   "198.51.100.7:0",
		},
		{
			name:       "single trusted host entry, X-Real-IP fallback",
			trusted:    []string{"10.0.0.2"},
			remoteAddr: "10.0.0.2:40000",
			realIPHdr:  "198.51.100.7",
			wantAddr:   "198.51.100.7:0",
		},
		{
			name:       "all hops trusted: keep the peer",
			trusted:    []string{"10.0.0.0/8"},
			remoteAddr: "10.0.0.2:40000",
			xff:        "10.0.0.3, 10.0.0.2",
			wantAddr:   "10.0.0.2:40000",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var gotAddr, gotXFF string
			h := realIP(tc.trusted)(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
				gotAddr = r.RemoteAddr
				gotXFF = r.Header.Get("X-Forwarded-For")
			}))

			req := httptest.NewRequest(http.MethodGet, "/", nil)
			req.RemoteAddr = tc.remoteAddr
			if tc.xff != "" {
				req.Header.Set("X-Forwarded-For", tc.xff)
			}
			if tc.realIPHdr != "" {
				req.Header.Set("X-Real-IP", tc.realIPHdr)
			}
			h.ServeHTTP(httptest.NewRecorder(), req)

			if gotAddr != tc.wantAddr {
				t.Errorf("RemoteAddr = %q, want %q", gotAddr, tc.wantAddr)
			}
			if tc.wantXFFGone && gotXFF != "" {
				t.Errorf("X-Forwarded-For survived as %q; downstream code could still read it", gotXFF)
			}
		})
	}
}

func TestClientIPFromRequestStripsPort(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.RemoteAddr = "198.51.100.7:12345"
	req.Header.Set("X-Forwarded-For", "1.2.3.4")
	if got := clientIPFromRequest(req); got != "198.51.100.7" {
		t.Errorf("got %q, want %q (the header must not win)", got, "198.51.100.7")
	}
}
