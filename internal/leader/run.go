// Package leader hosts the WebUI HTTP server, the gRPC server that agents
// connect to, the SSE hub, the alert engine, and the backup scheduler.
// Everything orchestrating across the cluster lives here.
package leader

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strconv"

	"github.com/Bartis-Dev/LabExtend/internal/auth"
	"github.com/Bartis-Dev/LabExtend/internal/backup"
	"github.com/Bartis-Dev/LabExtend/internal/config"
	"github.com/Bartis-Dev/LabExtend/internal/db"
)

// Run boots the leader: opens the DB, starts the gRPC server (for agents),
// starts the HTTP server (for the WebUI + SSE), starts background loops for
// metrics persistence + alert evaluation + backup scheduling. Blocks until
// ctx is done.
func Run(ctx context.Context, cfg *config.Config) error {
	slog.Info("leader: starting", "data_dir", cfg.DataDir)

	if err := ensureDataDir(cfg.DataDir); err != nil {
		return fmt.Errorf("ensure data dir: %w", err)
	}

	dbPath := filepath.Join(cfg.DataDir, "labextend.db")
	database, err := db.Open(ctx, dbPath)
	if err != nil {
		return fmt.Errorf("open db: %w", err)
	}
	defer func() { _ = database.Close() }()

	if err := db.Migrate(ctx, database); err != nil {
		return fmt.Errorf("migrate: %w", err)
	}
	if v, err := db.Version(ctx, database); err == nil {
		slog.Info("leader: db ready", "path", dbPath, "schema_version", v)
	}

	hub := NewHub()
	go hub.Run(ctx)

	registry := NewAgentRegistry()
	metrics := newMetricsStore(database)
	containers := newContainerStore(database)
	maxLogLines := intEnv("BPM_LOG_MAX_LINES", 5000)
	logRetention := intEnv("BPM_LOG_RETENTION_HOURS", 0)
	bucketRetention := intEnv("BPM_METRIC_RETENTION_HOURS", 24)
	sampleRetention := intEnv("BPM_SAMPLE_RETENTION_HOURS", 2)
	logs := newLogStore(database, maxLogLines)
	alerts := newAlertEngine(database, metrics, containers, registry, hub)
	audit := newAuditLogger(database, cfg.DisableAudit)

	totpMgr, err := auth.NewTOTPManager(database, cfg.TOTPIssuer, cfg.TOTPKey)
	if err != nil {
		return fmt.Errorf("totp manager: %w", err)
	}

	backupRunner := backup.NewRunner(
		database,
		newRegistryAdapter(registry),
		newHubPublisher(hub),
		cfg.SecretsKey,
	)
	backupScheduler := backup.NewScheduler(database, backupRunner)

	// Background loops.
	go metrics.RunPruneLoop(ctx, bucketRetention, sampleRetention)
	go logs.RunRetentionLoop(ctx, logRetention)
	go alerts.Run(ctx)
	go func() {
		if err := backupScheduler.Run(ctx); err != nil {
			slog.Warn("backup scheduler exited", "err", err)
		}
	}()

	errs := make(chan error, 2)

	go func() {
		if err := startGRPCServer(ctx, cfg, registry, hub, database, metrics, containers, logs); err != nil {
			errs <- fmt.Errorf("grpc: %w", err)
		}
	}()

	go func() {
		deps := &leaderDeps{
			DB:         database,
			Registry:   registry,
			Hub:        hub,
			Metrics:    metrics,
			Containers: containers,
			Logs:       logs,
			Alerts:     alerts,
			Audit:      audit,
			TOTP:       totpMgr,
			Scheduler:  backupScheduler,
			SecretsKey: cfg.SecretsKey,
		}
		if err := startHTTPServer(ctx, cfg, deps); err != nil {
			errs <- fmt.Errorf("http: %w", err)
		}
	}()

	select {
	case <-ctx.Done():
		return ctx.Err()
	case err := <-errs:
		if err != nil && !errors.Is(err, context.Canceled) {
			return err
		}
		return nil
	}
}

// ensureDataDir creates the data directory if missing and verifies it's
// writable. Uses 0700 so .env.generated isn't world-readable.
func ensureDataDir(dir string) error {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("mkdir %s: %w", dir, err)
	}
	probe := filepath.Join(dir, ".writable")
	f, err := os.Create(probe)
	if err != nil {
		return fmt.Errorf("data dir not writable: %w", err)
	}
	_ = f.Close()
	_ = os.Remove(probe)
	return nil
}

func intEnv(k string, def int) int {
	v := os.Getenv(k)
	if v == "" {
		return def
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return def
	}
	return n
}
