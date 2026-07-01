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
	"github.com/Bartis-Dev/LabExtend/internal/backup"
	"github.com/Bartis-Dev/LabExtend/internal/config"
	"github.com/Bartis-Dev/LabExtend/internal/frontend"
)

// leaderDeps bundles every shared piece an HTTP handler may need.
type leaderDeps struct {
	DB         *sql.DB
	Registry   *AgentRegistry
	Hub        *Hub
	Metrics    *metricsStore
	Containers *containerStore
	Logs       *logStore
	Alerts     *AlertEngine
	Audit      *AuditLogger
	TOTP       *auth.TOTPManager
	Scheduler  *backup.Scheduler
	SecretsKey string
}

func startHTTPServer(ctx context.Context, cfg *config.Config, deps *leaderDeps) error {
	sessions := auth.NewSessionStore(
		deps.DB,
		cfg.SessionCookieName,
		time.Duration(cfg.SessionTTLHours)*time.Hour,
		cfg.SessionSecureCookie,
	)

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

	authDeps := &AuthDeps{DB: deps.DB, Sessions: sessions, TOTP: deps.TOTP}
	monDeps := &MonitoringDeps{
		DB: deps.DB, Registry: deps.Registry, Metrics: deps.Metrics,
		Containers: deps.Containers, Logs: deps.Logs, Alerts: deps.Alerts,
	}
	filesDeps := &FilesDeps{DB: deps.DB, Registry: deps.Registry, Audit: deps.Audit}
	cronDeps := &CronDeps{DB: deps.DB, Registry: deps.Registry, Audit: deps.Audit}
	s3Deps := &S3Deps{DB: deps.DB, SecretsKey: deps.SecretsKey, Audit: deps.Audit}
	backupDeps := &BackupDeps{DB: deps.DB, Scheduler: deps.Scheduler, Audit: deps.Audit}
	usersDeps := &UsersDeps{DB: deps.DB, Audit: deps.Audit}
	serviceDeps := &ServiceDeps{
		Registry:     deps.Registry,
		Audit:        deps.Audit,
		LeaderNodeID: cfg.AgentHostID,
		ServiceName:  cfg.PortainerService,
	}
	accountDeps := &AccountDeps{AuthDeps: authDeps, TOTP: deps.TOTP, Audit: deps.Audit}
	auditDeps := &AuditDeps{DB: deps.DB}

	r := chi.NewRouter()
	r.Use(middleware.RealIP)
	r.Use(middleware.RequestID)
	r.Use(middleware.Recoverer)
	r.Use(middleware.Timeout(120 * time.Second))
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
		// ── public ────────────────────────────────────────────────────────
		api.Get("/setup/status", authDeps.SetupStatus)
		api.Post("/setup/initialize", authDeps.SetupInitialize)
		api.Post("/auth/login", authDeps.Login)
		api.Get("/ping", func(w http.ResponseWriter, _ *http.Request) {
			writeJSON(w, http.StatusOK, map[string]any{"pong": true, "ts": time.Now().Unix()})
		})

		// 2FA verify: needs cookie + CSRF but NOT requireAuth (because the
		// session IS 2FA-pending).
		api.Group(func(g chi.Router) {
			g.Use(csrfMiddleware)
			g.Post("/auth/2fa/verify", authDeps.Verify2FA)
		})

		// ── authenticated (session + 2FA promoted) ────────────────────────
		api.Group(func(authed chi.Router) {
			authed.Use(requireAuth)
			authed.Use(csrfMiddleware)

			authed.Post("/auth/logout", authDeps.Logout)
			authed.Get("/me", authDeps.Me)

			authed.Get("/events", deps.Hub.ServeHTTP)

			// ── monitoring ────────────────────────────────────────────
			authed.Get("/nodes", monDeps.ListNodes)
			authed.Post("/nodes/cleanup", monDeps.CleanupNodes)
			authed.Get("/nodes/{id}", monDeps.GetNode)
			authed.Get("/nodes/{id}/history", monDeps.NodeHistory)
			authed.Get("/nodes/{id}/samples", monDeps.NodeSamples)

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

			// ── files (per-node) ──────────────────────────────────────
			authed.Get("/nodes/{id}/paths", filesDeps.ListPaths)
			authed.Post("/nodes/{id}/paths", filesDeps.CreatePath)
			authed.Delete("/nodes/{id}/paths/{pid}", filesDeps.DeletePath)
			authed.Get("/nodes/{id}/files", filesDeps.ListFiles)
			authed.Get("/nodes/{id}/files/stat", filesDeps.StatFile)
			authed.Get("/nodes/{id}/files/read", filesDeps.ReadFile)
			authed.Post("/nodes/{id}/files/write", filesDeps.WriteFile)
			authed.Post("/nodes/{id}/files/mkdir", filesDeps.Mkdir)
			authed.Post("/nodes/{id}/files/rename", filesDeps.Rename)
			authed.Delete("/nodes/{id}/files", filesDeps.Delete)
			authed.Post("/nodes/{id}/files/chown", filesDeps.Chown)
			authed.Post("/nodes/{id}/files/suggest-owner", filesDeps.SuggestOwner)
			authed.Get("/nodes/{id}/files/lookup-user", filesDeps.LookupUser)

			// ── cron ──────────────────────────────────────────────────
			authed.Get("/cronjobs", cronDeps.List)
			authed.Get("/nodes/{id}/cronjobs", cronDeps.List)
			authed.Post("/cronjobs", cronDeps.Create)
			authed.Put("/cronjobs/{id}", cronDeps.Update)
			authed.Delete("/cronjobs/{id}", cronDeps.Delete)
			authed.Post("/nodes/{id}/cronjobs/apply", cronDeps.Apply)

			// ── swarm service control ─────────────────────────────────
			authed.Post("/services/portainer-agent/restart", serviceDeps.RestartPortainerAgent)

			// ── S3 ────────────────────────────────────────────────────
			authed.Get("/s3/endpoints", s3Deps.List)
			authed.Post("/s3/endpoints", s3Deps.Create)
			authed.Put("/s3/endpoints/{id}", s3Deps.Update)
			authed.Delete("/s3/endpoints/{id}", s3Deps.Delete)
			authed.Post("/s3/endpoints/{id}/test", s3Deps.Test)
			authed.Get("/s3/endpoints/{id}/buckets", s3Deps.Buckets)
			authed.Get("/s3/endpoints/{id}/buckets/{bucket}/objects", s3Deps.Objects)
			authed.Post("/s3/endpoints/{id}/buckets/{bucket}/delete", s3Deps.DeleteObjects)

			// ── backup ────────────────────────────────────────────────
			authed.Get("/backups/plans", backupDeps.ListPlans)
			authed.Post("/backups/plans", backupDeps.CreatePlan)
			authed.Put("/backups/plans/{id}", backupDeps.UpdatePlan)
			authed.Delete("/backups/plans/{id}", backupDeps.DeletePlan)
			authed.Post("/backups/plans/{id}/trigger", backupDeps.TriggerPlan)
			authed.Get("/backups/runs", backupDeps.ListRuns)
			authed.Delete("/backups/runs/{id}", backupDeps.DeleteRun)
			authed.Post("/backups/runs/cleanup-failed", backupDeps.CleanupFailedRuns)

			// ── account self-service (any logged-in user) ─────────────
			authed.Get("/account/totp", accountDeps.TOTPStatus)
			authed.Post("/account/totp/begin", accountDeps.TOTPBegin)
			authed.Post("/account/totp/confirm", accountDeps.TOTPConfirm)
			authed.Post("/account/totp/disable", accountDeps.TOTPDisable)
			authed.Post("/account/password", accountDeps.ChangePassword)

			// ── admin only: user mgmt + audit log ─────────────────────
			authed.Group(func(admin chi.Router) {
				admin.Use(requireAdmin(deps.DB))
				admin.Get("/users", usersDeps.List)
				admin.Post("/users", usersDeps.Create)
				admin.Put("/users/{id}", usersDeps.Update)
				admin.Delete("/users/{id}", usersDeps.Delete)
				admin.Get("/audit", auditDeps.List)
			})
		})
	})

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
