// Package frontend embeds the built Next.js SPA so the leader serves the UI
// from the same binary. The build pipeline must populate ./dist before
// `go build` runs (see scripts/build-web.sh).
package frontend

import (
	"embed"
	"io/fs"
	"net/http"
	"strings"
)

//go:embed all:dist
var distFS embed.FS

// Handler returns an http.Handler that serves the embedded SPA with proper
// SPA fallback (any unknown path → index.html). Static assets get a long
// cache header; index.html stays uncached.
//
// During development, if ./dist is empty (no SPA built yet), this still
// returns a working handler that 404s — the API stays usable for backend dev.
func Handler() http.Handler {
	sub, err := fs.Sub(distFS, "dist")
	if err != nil {
		return notFound{}
	}
	fileServer := http.FileServer(http.FS(sub))

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// SPA fallback: try to open the requested file; if it doesn't exist
		// and the request looks like a route (no extension), serve index.html.
		p := strings.TrimPrefix(r.URL.Path, "/")
		if p == "" {
			p = "index.html"
		}
		if _, err := fs.Stat(sub, p); err != nil && !hasExt(p) {
			r2 := r.Clone(r.Context())
			r2.URL.Path = "/"
			setCacheHeaders(w, "index.html")
			fileServer.ServeHTTP(w, r2)
			return
		}
		setCacheHeaders(w, p)
		fileServer.ServeHTTP(w, r)
	})
}

type notFound struct{}

func (notFound) ServeHTTP(w http.ResponseWriter, _ *http.Request) {
	http.Error(w, "SPA not built yet — run `cd frontend && npm run build`", http.StatusServiceUnavailable)
}

func hasExt(p string) bool {
	i := strings.LastIndexByte(p, '.')
	if i < 0 {
		return false
	}
	// path/ext (no slashes after the dot)
	return !strings.ContainsAny(p[i:], "/")
}

func setCacheHeaders(w http.ResponseWriter, p string) {
	if strings.HasSuffix(p, "index.html") || p == "" {
		w.Header().Set("Cache-Control", "no-cache")
		return
	}
	// Next.js fingerprints static assets under /_next/static/...
	if strings.Contains(p, "/_next/static/") {
		w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
	}
}
