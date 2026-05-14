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

		// Public auth + setup.
		r.Post("/setup", s.handleSetup)
		r.Post("/auth/login", s.handleLogin)
		r.Post("/auth/logout", s.handleLogout)

		// Icons are served publicly so <img> tags don't need cookie round-trips
		// and CDN/reverse-proxy caches stay effective. UUID filenames mean a
		// caller cannot enumerate the directory.
		r.Get("/icons/{filename}", s.serveIcon)

		// Stats ingest: token-authed by URL, not by JWT. Sits in the public
		// group on purpose so external scripts (cron jobs, smart-home
		// dashboards, etc.) can push metrics without holding a session cookie.
		r.Post("/stats/ingest/{token}", s.ingestStatsPoint)

		// Protected.
		r.Group(func(r chi.Router) {
			r.Use(s.requireAuth)
			r.Get("/auth/me", s.handleMe)
			r.Put("/auth/password", s.handleChangePassword)

			r.Get("/services", s.listServices)
			r.Post("/services", s.createService)
			r.Get("/services/{uuid}", s.getService)
			r.Put("/services/{uuid}", s.updateService)
			r.Delete("/services/{uuid}", s.deleteService)
			r.Post("/services/{uuid}/icon", s.uploadIcon)
			r.Put("/services/{uuid}/icon-url", s.setIconURL)
			r.Delete("/services/{uuid}/icon", s.deleteIcon)

			r.Get("/categories", s.listCategories)
			r.Post("/categories", s.createCategory)
			r.Put("/categories/{id}", s.updateCategory)
			r.Delete("/categories/{id}", s.deleteCategory)

			r.Put("/layout", s.handleLayoutBulk)

			r.Get("/themes", s.listThemes)
			r.Post("/themes", s.createTheme)
			r.Put("/themes/{id}", s.updateTheme)
			r.Delete("/themes/{id}", s.deleteTheme)
			r.Post("/themes/{id}/activate", s.activateTheme)

			r.Get("/healthcheck/status", s.handleHCStatus)

			r.Get("/settings", s.handleGetSettings)
			r.Put("/settings", s.handlePutSettings)

			r.Get("/modules", s.listModules)
			r.Post("/modules", s.createModule)
			r.Put("/modules/{id}", s.updateModule)
			r.Delete("/modules/{id}", s.deleteModule)

			r.Get("/vault/state", s.handleVaultState)
			r.Post("/vault/setup", s.handleVaultSetup)
			r.Get("/vault/entries", s.handleVaultList)
			r.Post("/vault/entries", s.handleVaultCreate)
			r.Put("/vault/entries/{id}", s.handleVaultUpdate)
			r.Delete("/vault/entries/{id}", s.handleVaultDelete)

			r.Get("/ddns/providers", s.listDDNSProviders)
			r.Post("/ddns/providers", s.createDDNSProvider)
			r.Put("/ddns/providers/{id}", s.updateDDNSProvider)
			r.Delete("/ddns/providers/{id}", s.deleteDDNSProvider)
			r.Get("/ddns/providers/{id}/zones", s.listDDNSZones)

			r.Get("/ddns/cards", s.listDDNSCards)
			r.Post("/ddns/cards", s.createDDNSCard)
			r.Put("/ddns/cards/{id}", s.updateDDNSCard)
			r.Delete("/ddns/cards/{id}", s.deleteDDNSCard)
			r.Get("/ddns/cards/{id}/records", s.listCardRecords)
			r.Post("/ddns/cards/{id}/records", s.createCardRecord)
			r.Put("/ddns/cards/{id}/records/{record_id}", s.updateCardRecord)
			r.Delete("/ddns/cards/{id}/records/{record_id}", s.deleteCardRecord)
			r.Post("/ddns/cards/{id}/auto-update", s.toggleAutoUpdate)

			r.Get("/ddns/auto-update", s.listAutoUpdates)

			r.Get("/wol", s.listWoL)
			r.Get("/wol/status", s.listWoLStatus)
			r.Post("/wol", s.createWoL)
			r.Put("/wol/{id}", s.updateWoL)
			r.Delete("/wol/{id}", s.deleteWoL)
			r.Post("/wol/{id}/wake", s.wakeWoL)

			r.Get("/docs", s.listDocs)
			r.Post("/docs", s.createDoc)
			r.Get("/docs/{id}", s.getDoc)
			r.Put("/docs/{id}", s.updateDoc)
			r.Delete("/docs/{id}", s.deleteDoc)

			r.Get("/notes", s.listNotes)
			r.Post("/notes/cards", s.createNotesCard)
			r.Put("/notes/cards/{id}", s.updateNotesCard)
			r.Patch("/notes/cards/{id}/layout", s.patchNotesCardLayout)
			r.Delete("/notes/cards/{id}", s.deleteNotesCard)
			r.Post("/notes/cards/{id}/items", s.createNotesItem)
			r.Put("/notes/items/{id}", s.updateNotesItem)
			r.Patch("/notes/items/{id}/move", s.moveNotesItem)
			r.Delete("/notes/items/{id}", s.deleteNotesItem)
			r.Post("/notes/boards", s.createNotesBoard)
			r.Put("/notes/boards/{id}", s.updateNotesBoard)
			r.Patch("/notes/boards/{id}/position", s.patchNotesBoardPosition)
			r.Post("/notes/boards/{id}/append-card", s.appendNotesBoardCard)
			r.Delete("/notes/boards/{id}", s.deleteNotesBoard)
			r.Post("/notes/cards/swap-slots", s.swapNotesCardSlots)

			r.Get("/stats/sources", s.listStatsSources)
			r.Post("/stats/sources", s.createStatsSource)
			r.Put("/stats/sources/{id}", s.updateStatsSource)
			r.Post("/stats/sources/{id}/rotate-token", s.rotateStatsSourceToken)
			r.Delete("/stats/sources/{id}", s.deleteStatsSource)
			r.Get("/stats/sources/{id}/points", s.queryStatsPoints)
			r.Get("/stats/widgets", s.listStatsWidgets)
			r.Post("/stats/widgets", s.createStatsWidget)
			r.Put("/stats/widgets/{id}", s.updateStatsWidget)
			r.Delete("/stats/widgets/{id}", s.deleteStatsWidget)

			r.Get("/tls/state", s.handleTLSState)
			r.Post("/tls/cert", s.handleTLSUpload)
			r.Post("/tls/self-signed", s.handleTLSSelfSign)
			r.Post("/tls/reset", s.handleTLSReset)
		})

		// WebSocket: own auth path that runs before websocket.Accept owns the writer.
		r.Get("/ws", s.wsAuth(s.handleWS))
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
