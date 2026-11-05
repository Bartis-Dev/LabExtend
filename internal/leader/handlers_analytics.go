package leader

import (
	"errors"
	"log/slog"
	"net/http"
)

// GET /api/analytics/hosts
func analyticsHosts(a *webAnalytics) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		hosts, err := a.Hosts(r.Context())
		if err != nil {
			slog.Warn("analytics hosts failed", "err", err)
			writeErr(w, http.StatusInternalServerError, nil)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"hosts":        hosts,
			"parse_errors": a.parseErrors.Load(),
		})
	}
}

// GET /api/analytics/report?host=example.com&range=7d
func analyticsReport(a *webAnalytics) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		host := normalizeHost(r.URL.Query().Get("host"))
		if host == "" || len(host) > 253 {
			writeErr(w, http.StatusBadRequest, errors.New("host is required"))
			return
		}
		rng := r.URL.Query().Get("range")
		if rng == "" {
			rng = "7d"
		}
		rep, err := a.Report(r.Context(), host, rng)
		if errors.Is(err, errUnknownRange) {
			writeErr(w, http.StatusBadRequest, errors.New("range must be one of 24h, 7d, 30d, 90d, 365d"))
			return
		}
		if err != nil {
			slog.Warn("analytics report failed", "host", host, "err", err)
			writeErr(w, http.StatusInternalServerError, nil)
			return
		}
		writeJSON(w, http.StatusOK, rep)
	}
}
