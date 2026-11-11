// Package frontend embeds the built Next.js SPA so the leader serves the UI
// from the same binary. The build pipeline must populate ./dist before
// `go build` runs (see scripts/gen-proto.sh + npm run build).
package frontend

import (
	"crypto/sha256"
	"embed"
	"encoding/base64"
	"io/fs"
	"net/http"
	"regexp"
	"strings"
)

//go:embed all:dist
var distFS embed.FS

// Handler returns an http.Handler that serves the embedded SPA.
//
// Next.js with `output: 'export'` and `trailingSlash: false` produces files
// like `dashboard.html`, `nodes.html`, `_next/static/...`. Browser requests
// to `/dashboard` must therefore map to `dashboard.html`. Truly unknown
// routes (e.g. a deep-link to a deleted page) fall back to `index.html` for
// client-side routing.
//
// Resolution order for a request to `/<p>`:
//  1. exact match  →  serve as-is        (/_next/static/foo.js → foo.js)
//  2. <p>.html exists → serve that       (/dashboard → dashboard.html)
//  3. has file extension, not found → 404
//  4. no extension → serve index.html    (true SPA fallback)
func Handler() http.Handler {
	sub, err := fs.Sub(distFS, "dist")
	if err != nil {
		return notFound{}
	}
	fileServer := http.FileServer(http.FS(sub))
	csp := pageCSPs(sub)
	pageHeaders := func(w http.ResponseWriter, p string) {
		if c, ok := csp[p]; ok {
			w.Header().Set("Content-Security-Policy", c)
		}
		setCacheHeaders(w, p)
	}

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p := strings.TrimPrefix(r.URL.Path, "/")
		if p == "" {
			pageHeaders(w, "index.html")
			fileServer.ServeHTTP(w, r)
			return
		}

		// 1. exact match
		if _, err := fs.Stat(sub, p); err == nil {
			pageHeaders(w, p)
			fileServer.ServeHTTP(w, r)
			return
		}

		// 2. <p>.html (Next.js export pattern)
		if !hasExt(p) {
			htmlPath := p + ".html"
			if _, err := fs.Stat(sub, htmlPath); err == nil {
				r2 := r.Clone(r.Context())
				r2.URL.Path = "/" + htmlPath
				pageHeaders(w, htmlPath)
				fileServer.ServeHTTP(w, r2)
				return
			}
		}

		// 3. file-with-extension not found → 404 (don't shadow with index.html;
		//    would mask missing assets behind a 200 + HTML body)
		if hasExt(p) {
			http.NotFound(w, r)
			return
		}

		// 4. SPA fallback for unknown route-shaped paths
		r2 := r.Clone(r.Context())
		r2.URL.Path = "/"
		pageHeaders(w, "index.html")
		fileServer.ServeHTTP(w, r2)
	})
}

type notFound struct{}

func (notFound) ServeHTTP(w http.ResponseWriter, _ *http.Request) {
	http.Error(w, "SPA not built yet — run `cd frontend && npm run build`", http.StatusServiceUnavailable)
}

// inlineScript matches <script> elements without a src attribute. Next's
// static export puts the RSC flight data into several of those per page.
var inlineScript = regexp.MustCompile(`(?s)<script(?:\s[^>]*)?>(.*?)</script>`)

// pageCSPs builds a Content-Security-Policy per exported HTML page that allows
// exactly that page's inline scripts by hash. Next's static export cannot use
// nonces (there is no server render to inject one), and the scripts differ per
// page and per build, so they are hashed from the embedded files at startup.
// style-src keeps 'unsafe-inline': React and the UI set style attributes, and
// injected CSS cannot run code the way an injected script can.
func pageCSPs(sub fs.FS) map[string]string {
	out := map[string]string{}
	pages, _ := fs.Glob(sub, "*.html")
	for _, p := range pages {
		b, err := fs.ReadFile(sub, p)
		if err != nil {
			continue
		}
		script := "script-src 'self'"
		for _, m := range inlineScript.FindAllSubmatch(b, -1) {
			if hasSrcAttr(m[0]) {
				continue
			}
			sum := sha256.Sum256(m[1])
			script += " 'sha256-" + base64.StdEncoding.EncodeToString(sum[:]) + "'"
		}
		out[p] = "default-src 'self'; " + script + "; style-src 'self' 'unsafe-inline'; " +
			"img-src 'self' data: blob:; font-src 'self' data:; connect-src 'self'; " +
			"object-src 'none'; base-uri 'self'; form-action 'self'; frame-ancestors 'none'"
	}
	return out
}

func hasSrcAttr(tag []byte) bool {
	end := strings.IndexByte(string(tag), '>')
	return end > 0 && strings.Contains(string(tag[:end]), " src=")
}

func hasExt(p string) bool {
	i := strings.LastIndexByte(p, '.')
	if i < 0 {
		return false
	}
	return !strings.ContainsAny(p[i:], "/")
}

func setCacheHeaders(w http.ResponseWriter, p string) {
	if strings.HasSuffix(p, ".html") || p == "" {
		w.Header().Set("Cache-Control", "no-cache")
		return
	}
	if strings.Contains(p, "/_next/static/") {
		w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
	}
}
