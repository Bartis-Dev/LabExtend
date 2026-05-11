package api

import (
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
)

// Routes returns the full HTTP handler tree, with /api/* handled here and
// any unmatched path delegated to webHandler (the embedded SPA).
func (s *Server) Routes(webHandler http.Handler) http.Handler {
	r := chi.NewRouter()
	r.Use(middleware.RequestID)
	r.Use(middleware.RealIP)
	r.Use(middleware.Logger)
	r.Use(middleware.Recoverer)
	r.Use(middleware.Timeout(60 * time.Second))
	r.Use(originCheck)

	r.Route("/api", func(r chi.Router) {
		r.Get("/healthz", s.handleHealthz)
		r.Get("/bootstrap", s.handleBootstrap)
		// Auth + protected routes registered in later tasks.
	})

	// SPA fallback: serve the embedded frontend for anything not under /api.
	r.NotFound(webHandler.ServeHTTP)
	return r
}

func (s *Server) handleHealthz(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// handleBootstrap reports whether the app needs to run the setup wizard
// and includes the currently-active theme so the frontend can paint the
// correct palette before any other fetches.
func (s *Server) handleBootstrap(w http.ResponseWriter, _ *http.Request) {
	var count int
	if err := s.DB.QueryRow(`SELECT COUNT(*) FROM users`).Scan(&count); err != nil {
		writeError(w, http.StatusInternalServerError, "db error")
		return
	}

	var (
		themeID                                int64
		themeName, palette, customCSS          string
		isDefault, isActive                    int
	)
	row := s.DB.QueryRow(
		`SELECT id, name, palette_json, custom_css, is_default, is_active
		   FROM themes WHERE is_active=1 LIMIT 1`,
	)
	_ = row.Scan(&themeID, &themeName, &palette, &customCSS, &isDefault, &isActive)

	writeJSON(w, http.StatusOK, map[string]any{
		"needs_setup": count == 0,
		"active_theme": map[string]any{
			"id":           themeID,
			"name":         themeName,
			"palette_json": palette,
			"custom_css":   customCSS,
			"is_default":   isDefault == 1,
		},
	})
}
