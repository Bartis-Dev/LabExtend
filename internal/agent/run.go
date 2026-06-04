// Package agent runs on every swarm node. It connects out to the leader's
// gRPC endpoint, opens a persistent bidi stream, and handles filesystem,
// cron, and backup commands from the leader.
package agent

import (
	"context"
	"log/slog"

	"github.com/Bartis-Dev/LabExtend/internal/config"
)

// Run boots the agent loop: maintain a gRPC connection to the leader, accept
// commands, push heartbeats. Returns when ctx is canceled.
func Run(ctx context.Context, cfg *config.Config) error {
	slog.Info("agent: starting", "leader_addr", cfg.LeaderAddr, "host_id", cfg.AgentHostID)

	client := NewClient(cfg)
	return client.Run(ctx)
}
