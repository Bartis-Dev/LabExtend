// Command manager is the single-binary entry point for labextend.
// It chooses between leader and agent role at boot, based on the LEADER env
// var. Both roles share the same binary, build flags, and config loader.
package main

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/Bartis-Dev/LabExtend/internal/agent"
	"github.com/Bartis-Dev/LabExtend/internal/config"
	"github.com/Bartis-Dev/LabExtend/internal/leader"
)

// version is set at build time via -ldflags "-X main.version=...".
var (
	version = "dev"
	commit  = "unknown"
)

func main() {
	logger := newLogger()
	slog.SetDefault(logger)

	cfg, err := config.Load()
	if err != nil {
		slog.Error("config load failed", "err", err)
		os.Exit(2)
	}

	logger.Info("labextend starting",
		"version", version,
		"commit", commit,
		"role", cfg.Role(),
		"http_addr", cfg.HTTPAddr,
		"grpc_addr", cfg.GRPCAddr,
	)

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	var runErr error
	switch cfg.Role() {
	case config.RoleLeader:
		// Leader also spawns an in-process agent that connects to itself so
		// the leader's own host + containers get monitored too.
		go func() {
			agentCfg := *cfg
			agentCfg.LeaderMode = false
			agentCfg.LeaderAddr = "127.0.0.1" + cfg.GRPCAddr
			// Brief delay so leader's gRPC listener is up.
			select {
			case <-ctx.Done():
				return
			case <-time.After(1500 * time.Millisecond):
			}
			if err := agent.Run(ctx, &agentCfg); err != nil && !errors.Is(err, context.Canceled) {
				slog.Warn("local agent loop ended", "err", err)
			}
		}()
		runErr = leader.Run(ctx, cfg)
	case config.RoleAgent:
		runErr = agent.Run(ctx, cfg)
	default:
		runErr = errors.New("invalid role; set LEADER=true OR LEADER_ADDR=host:port")
	}

	if runErr != nil && !errors.Is(runErr, context.Canceled) {
		slog.Error("shutdown with error", "err", runErr)
		os.Exit(1)
	}
	slog.Info("clean shutdown")
}

func newLogger() *slog.Logger {
	level := slog.LevelInfo
	switch os.Getenv("BPM_LOG_LEVEL") {
	case "debug":
		level = slog.LevelDebug
	case "warn":
		level = slog.LevelWarn
	case "error":
		level = slog.LevelError
	}
	return slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: level}))
}
