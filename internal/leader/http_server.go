package leader

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"

	"github.com/Bartis-Dev/LabExtend/internal/auth"
	"github.com/Bartis-Dev/LabExtend/internal/backup"
	"github.com/Bartis-Dev/LabExtend/internal/config"
	"github.com/Bartis-Dev/LabExtend/internal/frontend"
	"github.com/Bartis-Dev/LabExtend/internal/notify"
)

// leaderDeps bundles every shared piece an HTTP handler may need.
type leaderDeps struct {
	DB         *sql.DB
	Registry   *AgentRegistry
	Hub        *Hub
	Metrics    *metricsStore
	Containers *containerStore
	Logs       *logStore
	Analytics  *webAnalytics
	Events     *notify.Center
	Audit      *AuditLogger
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

	authDeps := &AuthDeps{
		DB: deps.DB, Sessions: sessions, Audit: deps.Audit,
		ProviderName: cfg.OIDCName,
		ProviderURL:  cfg.OIDCIssuer,
		UserGroup:    cfg.OIDCUserGroup,
		AdminGroup:   cfg.OIDCAdminGroup,
		FileBrowser:  cfg.FileBrowser,
	}
	monDeps := &MonitoringDeps{
		DB: deps.DB, Registry: deps.Registry, Metrics: deps.Metrics,
		Containers: deps.Containers, Logs: deps.Logs,
		// Swarm labels come from the manager socket, which only the leader's
		// own in-process agent can reach.
		Swarm: newSwarmTopology(deps.Registry, cfg.AgentHostID),
	}
	filesDeps := &FilesDeps{DB: deps.DB, Registry: deps.Registry, Audit: deps.Audit}
	cronDeps := &CronDeps{DB: deps.DB, Registry: deps.Registry, Audit: deps.Audit}
	s3Deps := &S3Deps{DB: deps.DB, SecretsKey: deps.SecretsKey, Audit: deps.Audit}
	backupDeps := &BackupDeps{DB: deps.DB, Scheduler: deps.Scheduler, Audit: deps.Audit}
	serviceDeps := &ServiceDeps{
		Registry:     deps.Registry,
		Audit:        deps.Audit,
		LeaderNodeID: cfg.AgentHostID,
		ServiceName:  cfg.PortainerService,
	}
	oidcDeps := newOIDCDeps(ctx, cfg, authDeps, deps.Audit)
	auditDeps := &AuditDeps{DB: deps.DB}
	settingsDeps := &SettingsDeps{Audit: deps.Audit}

	r := chi.NewRouter()
	r.Use(realIP(cfg.TrustedProxies))
	r.Use(middleware.RequestID)
	r.Use(middleware.Recoverer)
	r.Use(timeoutExceptStreams(120 * time.Second))
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
		api.Get("/auth/oidc/config", oidcDeps.Config)
		api.Get("/auth/oidc/login", oidcDeps.Login)
		api.Get("/auth/oidc/callback", oidcDeps.Callback)
		api.Get("/ping", func(w http.ResponseWriter, _ *http.Request) {
			writeJSON(w, http.StatusOK, map[string]any{"pong": true, "ts": time.Now().Unix()})
		})

		// ── authenticated ─────────────────────────────────────────────────
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

			// Read-only, aggregated and IP-free; same audience as the
			// container logs it is computed from.
			authed.Get("/analytics/hosts", analyticsHosts(deps.Analytics))
			authed.Get("/analytics/report", analyticsReport(deps.Analytics))

			authed.Get("/notifications", listNotifications(deps.Events))
			authed.Post("/notifications/read-all", markNotificationsRead(deps.Events))
			authed.Post("/notifications/{id}/read", markNotificationsRead(deps.Events))

			authed.Get("/webhooks", monDeps.ListWebhooks)
			authed.Post("/webhooks", monDeps.CreateWebhook)
			authed.Put("/webhooks/{id}", monDeps.UpdateWebhook)
			authed.Delete("/webhooks/{id}", monDeps.DeleteWebhook)
			authed.Post("/webhooks/{id}/test", monDeps.TestWebhook)

			// ── admin only ────────────────────────────────────
			// Files, cron, swarm services, S3 and backups all reach the host
			// filesystem or crontab of every node, so they are root-equivalent
			// and must never be reachable by a non-admin account.
			authed.Group(func(admin chi.Router) {
				admin.Use(requireAdmin(deps.DB))

				admin.Get("/audit", auditDeps.List)
				admin.Get("/settings", settingsDeps.Get)
				admin.Put("/settings", settingsDeps.Update)

				// ── files (per-node), only with BPM_FILEBROWSER ──────────
				admin.Group(func(files chi.Router) {
					files.Use(requireFileBrowser(cfg.FileBrowser))
					files.Get("/nodes/{id}/paths", filesDeps.ListPaths)
					files.Post("/nodes/{id}/paths", filesDeps.CreatePath)
					files.Delete("/nodes/{id}/paths/{pid}", filesDeps.DeletePath)
					files.Get("/nodes/{id}/files", filesDeps.ListFiles)
				})

				// ── cron ──────────────────────────────────────────────────
				admin.Get("/cronjobs", cronDeps.List)
				admin.Get("/nodes/{id}/cronjobs", cronDeps.List)
				admin.Post("/cronjobs", cronDeps.Create)
				admin.Put("/cronjobs/{id}", cronDeps.Update)
				admin.Delete("/cronjobs/{id}", cronDeps.Delete)
				admin.Post("/nodes/{id}/cronjobs/apply", cronDeps.Apply)

				// ── swarm service control ─────────────────────────────────
				admin.Post("/services/portainer-agent/restart", serviceDeps.RestartPortainerAgent)

				// ── S3 ────────────────────────────────────────────────────
				admin.Get("/s3/endpoints", s3Deps.List)
				admin.Post("/s3/endpoints", s3Deps.Create)
				admin.Put("/s3/endpoints/{id}", s3Deps.Update)
				admin.Delete("/s3/endpoints/{id}", s3Deps.Delete)
				admin.Post("/s3/endpoints/{id}/test", s3Deps.Test)
				admin.Get("/s3/endpoints/{id}/buckets", s3Deps.Buckets)
				admin.Get("/s3/endpoints/{id}/buckets/{bucket}/objects", s3Deps.Objects)

				// ── backup ────────────────────────────────────────────────
				admin.Get("/backups/plans", backupDeps.ListPlans)
				admin.Post("/backups/plans", backupDeps.CreatePlan)
				admin.Put("/backups/plans/{id}", backupDeps.UpdatePlan)
				admin.Delete("/backups/plans/{id}", backupDeps.DeletePlan)
				admin.Post("/backups/plans/{id}/trigger", backupDeps.TriggerPlan)
				admin.Get("/backups/runs", backupDeps.ListRuns)
				admin.Delete("/backups/runs/{id}", backupDeps.DeleteRun)
				admin.Post("/backups/runs/cleanup-failed", backupDeps.CleanupFailedRuns)
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

// timeoutExceptStreams puts a deadline on ordinary API calls but leaves the
// long-lived streaming endpoints alone. chi's middleware.Timeout cancels the
// request context, so under it an SSE tail is torn down after two minutes no
// matter how healthy it is.
func timeoutExceptStreams(d time.Duration) func(http.Handler) http.Handler {
	timeout := middleware.Timeout(d)
	return func(next http.Handler) http.Handler {
		timed := timeout(next)
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if isStreamingPath(r.URL.Path) {
				next.ServeHTTP(w, r)
				return
			}
			timed.ServeHTTP(w, r)
		})
	}
}

func isStreamingPath(p string) bool {
	return p == "/api/events" ||
		strings.HasSuffix(p, "/logs/stream")
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}
