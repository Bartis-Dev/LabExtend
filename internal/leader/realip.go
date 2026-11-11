package leader

import (
	"log/slog"
	"net"
	"net/http"
	"net/netip"
	"strings"
)

// realIP replaces chi's middleware.RealIP.
//
// That one rewrites RemoteAddr from X-Forwarded-For / X-Real-IP sent by *any*
// peer (CVE GO-2026-5775 / GO-2026-5777 against chi < 5.3.0), and the audit
// logger and session store each read the header a second time on their own. So
// every client could choose the source IP recorded against its file writes and
// cron changes, and — now that credential endpoints are rate limited per IP —
// could reset its own limit by rotating the header.
//
// Here the header is believed only when the direct peer is a configured proxy.
// Otherwise it is stripped, so nothing downstream can be fooled by it either.
func realIP(trusted []string) func(http.Handler) http.Handler {
	prefixes := parsePrefixes(trusted)

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if !peerIsTrusted(r.RemoteAddr, prefixes) {
				r.Header.Del("X-Forwarded-For")
				r.Header.Del("X-Real-IP")
				r.Header.Del("X-Forwarded-Proto")
				next.ServeHTTP(w, r)
				return
			}
			if ip := forwardedClient(r, prefixes); ip != "" {
				r.RemoteAddr = net.JoinHostPort(ip, "0")
			}
			next.ServeHTTP(w, r)
		})
	}
}

// parsePrefixes turns the configured entries into prefixes. A bare address
// becomes a single-host prefix. Unparseable entries are dropped loudly rather
// than silently widening or narrowing trust.
func parsePrefixes(entries []string) []netip.Prefix {
	out := make([]netip.Prefix, 0, len(entries))
	for _, e := range entries {
		e = strings.TrimSpace(e)
		if e == "" {
			continue
		}
		if p, err := netip.ParsePrefix(e); err == nil {
			out = append(out, p)
			continue
		}
		if a, err := netip.ParseAddr(e); err == nil {
			out = append(out, netip.PrefixFrom(a, a.BitLen()))
			continue
		}
		slog.Warn("ignoring unparseable BPM_TRUSTED_PROXIES entry", "entry", e)
	}
	return out
}

func peerIsTrusted(remoteAddr string, prefixes []netip.Prefix) bool {
	if len(prefixes) == 0 {
		return false
	}
	host := remoteAddr
	if h, _, err := net.SplitHostPort(remoteAddr); err == nil {
		host = h
	}
	return addrInPrefixes(host, prefixes)
}

func addrInPrefixes(host string, prefixes []netip.Prefix) bool {
	a, err := netip.ParseAddr(strings.Trim(host, "[]"))
	if err != nil {
		return false
	}
	a = a.Unmap()
	for _, p := range prefixes {
		if p.Contains(a) {
			return true
		}
	}
	return false
}

// forwardedClient walks X-Forwarded-For from the right and returns the first
// entry that is not itself a trusted proxy — the address the outermost proxy we
// trust actually observed. Reading the leftmost entry instead would take the
// value the client itself prepended, which is the whole spoofing problem.
func forwardedClient(r *http.Request, prefixes []netip.Prefix) string {
	xff := r.Header.Get("X-Forwarded-For")
	if xff == "" {
		return strings.TrimSpace(r.Header.Get("X-Real-IP"))
	}
	parts := strings.Split(xff, ",")
	for i := len(parts) - 1; i >= 0; i-- {
		hop := strings.TrimSpace(parts[i])
		if hop == "" {
			continue
		}
		if addrInPrefixes(hop, prefixes) {
			continue // another proxy of ours; keep walking left
		}
		return hop
	}
	return ""
}
