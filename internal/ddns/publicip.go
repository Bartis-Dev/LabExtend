package ddns

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"time"
)

// Two services are queried in parallel for the user's public IP; the
// first valid response wins. If both fail we return an error so the
// worker can record it and try again on the next tick.
//
// We need separate calls for v4 and v6 since most providers default to
// "whatever the connection used" — useful for AAAA records which need
// the IPv6 address even if the request would have happened over v4.

type ipService struct {
	URL     string // %s replaced with "" for v4, "6" for v6 marker (or fixed URL)
	IsIPv6  bool
	Resolve func(ctx context.Context, network, addr string) (net.Conn, error)
}

var publicIPServices = []struct{ Name, V4URL, V6URL string }{
	{Name: "ipify", V4URL: "https://api.ipify.org", V6URL: "https://api64.ipify.org"},
	{Name: "ifconfig.me", V4URL: "https://ifconfig.me/ip", V6URL: "https://ifconfig.me/ip"},
}

// PublicIPv4 returns the host's IPv4 public address. Falls back to a
// secondary service if the first fails or returns garbage.
func PublicIPv4(ctx context.Context) (string, error) {
	return publicIP(ctx, false)
}

func PublicIPv6(ctx context.Context) (string, error) {
	return publicIP(ctx, true)
}

func publicIP(ctx context.Context, v6 bool) (string, error) {
	type result struct {
		ip  string
		err error
	}
	results := make(chan result, len(publicIPServices))

	for _, svc := range publicIPServices {
		svc := svc
		url := svc.V4URL
		if v6 {
			url = svc.V6URL
		}
		go func() {
			ip, err := fetchIP(ctx, url, v6)
			results <- result{ip, err}
		}()
	}

	var lastErr error
	for i := 0; i < len(publicIPServices); i++ {
		r := <-results
		if r.err == nil && r.ip != "" {
			return r.ip, nil
		}
		if r.err != nil {
			lastErr = r.err
		}
	}
	if lastErr == nil {
		lastErr = errors.New("all public IP services returned empty responses")
	}
	return "", fmt.Errorf("public ip lookup: %w", lastErr)
}

func fetchIP(ctx context.Context, url string, wantV6 bool) (string, error) {
	network := "tcp4"
	if wantV6 {
		network = "tcp6"
	}
	// Force the dialer onto the right family so ipify-style "whatever you
	// connected with" services return the address kind we asked for.
	dialer := &net.Dialer{Timeout: 5 * time.Second}
	transport := &http.Transport{
		DialContext: func(ctx context.Context, _ string, addr string) (net.Conn, error) {
			return dialer.DialContext(ctx, network, addr)
		},
	}
	client := &http.Client{Timeout: 6 * time.Second, Transport: transport}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", err
	}
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		return "", fmt.Errorf("%s: status %d", url, resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 64))
	if err != nil {
		return "", err
	}
	ip := strings.TrimSpace(string(body))
	parsed := net.ParseIP(ip)
	if parsed == nil {
		return "", fmt.Errorf("%s: invalid IP %q", url, ip)
	}
	if wantV6 && parsed.To4() != nil {
		return "", fmt.Errorf("%s: got IPv4 when IPv6 was requested", url)
	}
	if !wantV6 && parsed.To4() == nil {
		return "", fmt.Errorf("%s: got IPv6 when IPv4 was requested", url)
	}
	return ip, nil
}
