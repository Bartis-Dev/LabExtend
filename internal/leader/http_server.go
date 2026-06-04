package leader

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"

	"github.com/Bartis-Dev/LabExtend/internal/auth"
	"github.com/Bartis-Dev/LabExtend/internal/config"
	"github.com/Bartis-Dev/LabExtend/internal/frontend"
)

// leaderDeps bundles every shared piece an HTTP handler may need so we don't
// pass 8 args through every helper.
type leaderDeps struct {
	DB         *sql.DB
	Registry   *AgentRegistry
	Hub        *Hub
	Metrics    *metricsStore
	Containers *containerStore
	Logs       *logStore
	Alerts     *AlertEngine
}

func startHTTPServer(ctx context.Context, cfg *config.Config, deps *leaderDeps) error {
	sessions := auth.NewSessionStore(
		deps.DB,
		cfg.SessionCookieName,
		time.Duration(cfg.SessionTTLHours)*time.Hour,
		cfg.SessionSecureCookie,
	)

	// Background: purge expired sessions every 10 min.
	go func() {
		t := time.NewTicker(10 * time.Minute)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				if err := sessions.PurgeExpired(ctx); err != nil {
					slog.Warn("session purge failed", "err", err)
				}
			}
		}
	}()

	authDeps := &AuthDeps{DB: deps.DB, Sessions: sessions}
	monDeps := &MonitoringDeps{
		DB:         deps.DB,
		Registry:   deps.Registry,
		Metrics:    deps.Metrics,
		Containers: deps.Containers,
		Logs:       deps.Logs,
		Alerts:     deps.Alerts,
	}

	r := chi.NewRouter()
	r.Use(middleware.RealIP)
	r.Use(middleware.RequestID)
	r.Use(middleware.Recoverer)
	r.Use(middleware.Timeout(60 * time.Second))
	r.Use(sessionMiddleware(sessions))

	r.Get("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{"ok": true})
	})
	r.Get("/readyz", func(w http.ResponseWriter, req *http.Request) {
		if err := deps.DB.PingContext(req.Context()); err != nil {
			writeJSON(w, http.StatusServiceUnavailable, map[string]any{"ok": false, "err": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"ok": true, "agents": len(deps.Registry.List())})
	})

	r.Route("/api", func(api chi.Router) {
		// Public — no auth, no CSRF.
		api.Get("/setup/status", authDeps.SetupStatus)
		api.Post("/setup/initialize", authDeps.SetupInitialize)
		api.Post("/auth/login", authDeps.Login)
		api.Get("/ping", func(w http.ResponseWriter, _ *http.Request) {
			writeJSON(w, http.StatusOK, map[string]any{"pong": true, "ts": time.Now().Unix()})
		})

		// Authenticated routes.
		api.Group(func(authed chi.Router) {
			authed.Use(requireAuth)
			authed.Use(csrfMiddleware)

			authed.Post("/auth/logout", authDeps.Logout)
			authed.Get("/me", authDeps.Me)

			authed.Get("/events", deps.Hub.ServeHTTP)

			// Monitoring.
			authed.Get("/nodes", monDeps.ListNodes)
			authed.Get("/nodes/{id}", monDeps.GetNode)
			authed.Get("/nodes/{id}/history", monDeps.NodeHistory)

			authed.Get("/containers", monDeps.ListContainers)
			authed.Get("/containers/{node}/{id}", monDeps.GetContainer)
			authed.Get("/containers/{node}/{id}/logs", monDeps.ContainerLogTail)
			authed.Get("/containers/{node}/{id}/logs/stream", monDeps.ContainerLogStream)

			authed.Get("/alert-rules", monDeps.ListAlertRules)
			authed.Post("/alert-rules", monDeps.CreateAlertRule)
			authed.Put("/alert-rules/{id}", monDeps.UpdateAlertRule)
			authed.Delete("/alert-rules/{id}", monDeps.DeleteAlertRule)
			authed.Get("/alert-history", monDeps.ListAlertHistory)

			authed.Get("/webhooks", monDeps.ListWebhooks)
			authed.Post("/webhooks", monDeps.CreateWebhook)
			authed.Put("/webhooks/{id}", monDeps.UpdateWebhook)
			authed.Delete("/webhooks/{id}", monDeps.DeleteWebhook)
			authed.Post("/webhooks/{id}/test", monDeps.TestWebhook)
		})
	})

	// SPA fallback — everything not under /api is served by the embedded export.
	r.Handle("/*", frontend.Handler())

	srv := &http.Server{
		Addr:              cfg.HTTPAddr,
		Handler:           r,
		ReadHeaderTimeout: 10 * time.Second,
	}
	slog.Info("http: listening", "addr", cfg.HTTPAddr)

	errs := make(chan error, 1)
	go func() {
		err := srv.ListenAndServe()
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			errs <- err
		} else {
			errs <- nil
		}
	}()

	select {
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = srv.Shutdown(shutdownCtx)
		return nil
	case err := <-errs:
		return err
	}
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}
