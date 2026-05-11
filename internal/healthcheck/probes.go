package healthcheck

import (
	"context"
	"crypto/tls"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// httpClient is shared across probes. TLS verification is disabled because
// homelab services often use self-signed certificates; this is the
// dashboard's job — it never relays the response body.
var httpClient = &http.Client{
	Timeout: 5 * time.Second,
	Transport: &http.Transport{
		TLSClientConfig:       &tls.Config{InsecureSkipVerify: true}, // homelab default
		DisableKeepAlives:     true,
		ResponseHeaderTimeout: 4 * time.Second,
		ExpectContinueTimeout: 1 * time.Second,
	},
}

// HTTPProbe returns up when target responds 2xx/3xx within timeout.
// target may be a full URL or a host[:port] string; bare hosts get an
// http:// scheme prefix.
func HTTPProbe(ctx context.Context, target string) HostStatus {
	if target == "" {
		return StatusDown
	}
	u := normaliseURL(target)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return StatusDown
	}
	req.Header.Set("User-Agent", "labextend-healthcheck/0.1")
	resp, err := httpClient.Do(req)
	if err != nil {
		return StatusDown
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 200 && resp.StatusCode < 400 {
		return StatusUp
	}
	return StatusDown
}

// TCPProbe attempts a TCP connect to host:port within 3s.
// If port == 0 it is inferred from the URL scheme (https=443, http=80, else 80).
func TCPProbe(ctx context.Context, host string, port int) HostStatus {
	host, port = resolveHostPort(host, port)
	if host == "" {
		return StatusDown
	}
	d := net.Dialer{Timeout: 3 * time.Second}
	conn, err := d.DialContext(ctx, "tcp", net.JoinHostPort(host, strconv.Itoa(port)))
	if err != nil {
		return StatusDown
	}
	_ = conn.Close()
	return StatusUp
}

// resolveHostPort extracts a (host, port) pair from the input. If the input
// already contains a host:port pair, that wins. Otherwise, scheme-default
// ports apply (https=443, http=80, fallback=80).
func resolveHostPort(input string, explicitPort int) (string, int) {
	input = strings.TrimSpace(input)
	if input == "" {
		return "", 0
	}
	if explicitPort > 0 {
		host := stripScheme(input)
		host, _ = trimHostPort(host)
		return host, explicitPort
	}
	if u, err := url.Parse(input); err == nil && (u.Scheme == "http" || u.Scheme == "https") {
		host := u.Hostname()
		if u.Port() != "" {
			p, _ := strconv.Atoi(u.Port())
			return host, p
		}
		if u.Scheme == "https" {
			return host, 443
		}
		return host, 80
	}
	host, portStr := trimHostPort(input)
	if portStr != "" {
		p, _ := strconv.Atoi(portStr)
		return host, p
	}
	return host, 80
}

func trimHostPort(s string) (host, port string) {
	// Handle bracketed IPv6 first.
	if strings.HasPrefix(s, "[") {
		if end := strings.LastIndex(s, "]"); end != -1 {
			host = s[1:end]
			if len(s) > end+1 && s[end+1] == ':' {
				port = s[end+2:]
			}
			return host, port
		}
	}
	idx := strings.LastIndex(s, ":")
	if idx == -1 {
		return s, ""
	}
	// Reject when it looks like an IPv6 without brackets ("a:b:c").
	if strings.Count(s, ":") > 1 {
		return s, ""
	}
	return s[:idx], s[idx+1:]
}

func stripScheme(s string) string {
	if u, err := url.Parse(s); err == nil && u.Scheme != "" {
		if u.Host != "" {
			return u.Host
		}
	}
	return s
}

// normaliseURL ensures a valid http(s) URL for the HTTP probe. Bare hosts
// become http://host.
func normaliseURL(s string) string {
	if strings.HasPrefix(s, "http://") || strings.HasPrefix(s, "https://") {
		return s
	}
	return "http://" + s
}

// DeriveHealthcheckURL picks the URL the HTTP probe should hit when the
// user enabled HC for this host but did not supply a custom URL.
func DeriveHealthcheckURL(host string, port *int) string {
	if host == "" {
		return ""
	}
	if strings.HasPrefix(host, "http://") || strings.HasPrefix(host, "https://") {
		u, err := url.Parse(host)
		if err != nil {
			return host
		}
		if port != nil && u.Port() == "" {
			u.Host = fmt.Sprintf("%s:%d", u.Hostname(), *port)
		}
		return u.String()
	}
	if port != nil {
		return fmt.Sprintf("http://%s:%d", host, *port)
	}
	return "http://" + host
}

