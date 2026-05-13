package main

import (
	"context"
	"crypto/tls"
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
	"github.com/Bartis-Dev/LabExtend/internal/ddns"
	"github.com/Bartis-Dev/LabExtend/internal/docs"
	"github.com/Bartis-Dev/LabExtend/internal/modules"
	"github.com/Bartis-Dev/LabExtend/internal/notes"
	"github.com/Bartis-Dev/LabExtend/internal/servercrypto"
	"github.com/Bartis-Dev/LabExtend/internal/settings"
	"github.com/Bartis-Dev/LabExtend/internal/stats"
	"github.com/Bartis-Dev/LabExtend/internal/tlsstore"
	"github.com/Bartis-Dev/LabExtend/internal/vault"
	"github.com/Bartis-Dev/LabExtend/internal/wol"
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
	mods := modules.New(database)
	vlt := vault.New(database)

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

	tokenCipher, err := servercrypto.New([]byte(jwtSecret), "ddns/provider-token")
	if err != nil {
		slog.Error("ddns token cipher", "err", err)
		os.Exit(1)
	}
	ddnsStore := ddns.New(database, tokenCipher)
	wolStore := wol.New(database)
	docsStore := docs.New(database)
	notesStore := notes.New(database)
	statsStore := stats.New(database)

	tlsStore := tlsstore.New(cfg.DataDir, cfg.TLSCertFile, cfg.TLSKeyFile)
	if err := tlsStore.LoadOrCreate(); err != nil {
		slog.Error("tls load/create", "err", err)
		os.Exit(1)
	}
	info := tlsStore.CurrentInfo()
	slog.Info("tls certificate ready",
		"source", info.Source, "subject", info.Subject,
		"not_after", info.NotAfter.Format(time.RFC3339),
	)

	srv := api.New(database, cfg, st, mods, vlt, ddnsStore, wolStore, docsStore, notesStore, statsStore, tlsStore, []byte(jwtSecret))

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

	// DDNS worker honours the ddns_check_interval setting (default 5min).
	ddnsInterval := 5 * time.Minute
	if v, _ := st.Get(settings.KeyDDNSCheckInterval); v != "" {
		if d, err := config.ParseDuration(v); err == nil {
			ddnsInterval = d
		}
	}
	ddnsWorker := ddns.NewWorker(ddnsStore, ddnsInterval)
	srv.DDNSWorker = ddnsWorker
	go ddnsWorker.Run(ctx)

	webHandler := spaHandler(http.FS(web.FS()))
	handler := srv.Routes(webHandler)

	// LabExtend serves HTTPS only. The cert is read fresh on every
	// handshake via tlsStore.GetCertificate, so uploads/regenerates from
	// the UI take effect on the next connection without restart.
	httpsSrv := &http.Server{
		Addr:              cfg.Listen,
		Handler:           handler,
		ReadHeaderTimeout: 10 * time.Second,
		TLSConfig: &tls.Config{
			MinVersion:     tls.VersionTLS12,
			GetCertificate: tlsStore.GetCertificate,
		},
	}
	srv.HTTPSStarted = true
	go func() {
		slog.Info("https listening",
			"addr", cfg.Listen,
			"data_dir", cfg.DataDir,
			"session_timeout", cfg.SessionTimeout,
		)
		if err := httpsSrv.ListenAndServeTLS("", ""); err != nil && !errors.Is(err, http.ErrServerClosed) {
			slog.Error("https server error", "err", err)
			os.Exit(1)
		}
	}()

	<-ctx.Done()
	slog.Info("shutting down")
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_ = httpsSrv.Shutdown(shutdownCtx)
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
