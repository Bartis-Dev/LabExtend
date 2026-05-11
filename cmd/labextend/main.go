package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/Bartis-Dev/LabExtend/internal/api"
	"github.com/Bartis-Dev/LabExtend/internal/config"
	"github.com/Bartis-Dev/LabExtend/internal/db"
	"github.com/Bartis-Dev/LabExtend/internal/healthcheck"
	"github.com/Bartis-Dev/LabExtend/internal/settings"
	web "github.com/Bartis-Dev/LabExtend/web"
)

func main() {
	cfg := config.Load()

	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
		Level: parseLogLevel(cfg.LogLevel),
	}))
	slog.SetDefault(logger)

	database, err := db.Open(cfg.DataDir)
	if err != nil {
		slog.Error("db open", "err", err)
		os.Exit(1)
	}
	defer database.Close()

	if err := db.Migrate(database); err != nil {
		slog.Error("migrate", "err", err)
		os.Exit(1)
	}

	st := settings.New(database)

	if cfg.PasswordReset {
		if _, err := database.Exec(`DELETE FROM users`); err != nil {
			slog.Error("password reset: delete users", "err", err)
			os.Exit(1)
		}
		slog.Warn("LABEXTEND_PASSWORD_RESET=true: users deleted; setup wizard will appear on next login")
	}

	jwtSecret := cfg.JWTSecret
	if jwtSecret == "" {
		jwtSecret, err = st.GetOrCreateJWTSecret()
		if err != nil {
			slog.Error("jwt secret bootstrap", "err", err)
			os.Exit(1)
		}
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	srv := api.New(database, cfg, st, []byte(jwtSecret))

	// Healthcheck worker + hub. The interval can be overridden at runtime
	// via PUT /api/settings; we honour the env-supplied default here.
	hub := healthcheck.NewHub()
	worker := &healthcheck.Worker{DB: database, Hub: hub, Interval: cfg.HealthcheckInterval}
	if v, _ := st.Get(settings.KeyHealthcheckInterval); v != "" {
		if d, err := config.ParseDuration(v); err == nil {
			worker.SetInterval(d)
		}
	}
	srv.Hub = hub
	srv.Worker = worker
	go worker.Run(ctx)

	webHandler := spaHandler(http.FS(web.FS()))
	handler := srv.Routes(webHandler)

	httpSrv := &http.Server{
		Addr:              cfg.Listen,
		Handler:           handler,
		ReadHeaderTimeout: 10 * time.Second,
	}

	go func() {
		slog.Info("server listening",
			"addr", cfg.Listen,
			"data_dir", cfg.DataDir,
			"session_timeout", cfg.SessionTimeout,
		)
		if err := httpSrv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			slog.Error("server error", "err", err)
			os.Exit(1)
		}
	}()

	<-ctx.Done()
	slog.Info("shutting down")
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_ = httpSrv.Shutdown(shutdownCtx)
}

func spaHandler(fsys http.FileSystem) http.Handler {
	fileServer := http.FileServer(fsys)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := r.URL.Path
		if path == "" {
			path = "/"
		}
		f, err := fsys.Open(path)
		if err != nil {
			r.URL.Path = "/"
		} else {
			_ = f.Close()
		}
		fileServer.ServeHTTP(w, r)
	})
}

func parseLogLevel(s string) slog.Level {
	switch strings.ToLower(s) {
	case "debug":
		return slog.LevelDebug
	case "warn", "warning":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}
