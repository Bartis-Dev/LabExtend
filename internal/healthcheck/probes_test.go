package healthcheck

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestHTTPProbeUpDown(t *testing.T) {
	upServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(200)
	}))
	defer upServer.Close()
	downServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(503)
	}))
	defer downServer.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if s := HTTPProbe(ctx, upServer.URL); s != StatusUp {
		t.Errorf("up server: got %s, want up", s)
	}
	if s := HTTPProbe(ctx, downServer.URL); s != StatusDown {
		t.Errorf("down server: got %s, want down", s)
	}
	if s := HTTPProbe(ctx, "http://127.0.0.1:1"); s != StatusDown {
		t.Errorf("unreachable: got %s, want down", s)
	}
}

func TestTCPProbe(t *testing.T) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	go func() {
		for {
			c, err := l.Accept()
			if err != nil {
				return
			}
			c.Close()
		}
	}()
	host, portStr, _ := net.SplitHostPort(l.Addr().String())
	port, _ := strconv.Atoi(portStr)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if s := TCPProbe(ctx, host, port); s != StatusUp {
		t.Errorf("running listener: got %s, want up", s)
	}
	if s := TCPProbe(ctx, host, 1); s != StatusDown {
		t.Errorf("closed port: got %s, want down", s)
	}
}

func TestResolveHostPort(t *testing.T) {
	type want struct {
		host string
		port int
	}
	cases := []struct {
		in           string
		explicitPort int
		want         want
	}{
		{"https://app.lan", 0, want{"app.lan", 443}},
		{"http://app.lan:1234", 0, want{"app.lan", 1234}},
		{"app.lan:8080", 0, want{"app.lan", 8080}},
		{"app.lan", 0, want{"app.lan", 80}},
		{"app.lan", 9000, want{"app.lan", 9000}},
		{"https://app.lan:9443", 0, want{"app.lan", 9443}},
	}
	for _, c := range cases {
		host, port := resolveHostPort(c.in, c.explicitPort)
		if host != c.want.host || port != c.want.port {
			t.Errorf("resolveHostPort(%q, %d) = (%q, %d), want (%q, %d)",
				c.in, c.explicitPort, host, port, c.want.host, c.want.port)
		}
	}
}

func TestDeriveHealthcheckURL(t *testing.T) {
	port := 8080
	cases := []struct {
		host string
		port *int
		want string
	}{
		{"app.lan", nil, "http://app.lan"},
		{"app.lan", &port, "http://app.lan:8080"},
		{"https://app.lan", nil, "https://app.lan"},
		{"https://app.lan", &port, "https://app.lan:8080"},
	}
	for _, c := range cases {
		got := DeriveHealthcheckURL(c.host, c.port)
		if !strings.HasPrefix(got, c.want) {
			t.Errorf("DeriveHealthcheckURL(%q, %v) = %q, want prefix %q", c.host, c.port, got, c.want)
		}
	}
}
