package leader

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"

	pb "github.com/Bartis-Dev/LabExtend/internal/grpc/pb"
)

// Request sends a Command to the agent and blocks until the matching
// CommandResult arrives (or ctx times out). This is THE helper every
// leader-side handler uses to talk to agents.
//
// Concurrency: the AgentConn pending-map allows many in-flight commands
// per agent. The bidi stream itself is serialized by sendMu.
func (c *AgentConn) Request(ctx context.Context, cmd *pb.Command) (*pb.CommandResult, error) {
	id := uuid.NewString()
	ch := make(chan *pb.AgentMessage, 1)

	c.pendingMu.Lock()
	c.pending[id] = ch
	c.pendingMu.Unlock()

	defer func() {
		c.pendingMu.Lock()
		delete(c.pending, id)
		c.pendingMu.Unlock()
	}()

	msg := &pb.LeaderMessage{
		RequestId: id,
		Kind:      &pb.LeaderMessage_Command{Command: cmd},
	}
	if err := c.Send(msg); err != nil {
		return nil, fmt.Errorf("send: %w", err)
	}

	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case reply := <-ch:
		res := reply.GetResult()
		if res == nil {
			return nil, errors.New("agent reply was not a CommandResult")
		}
		if !res.Ok {
			return res, fmt.Errorf("agent: %s", res.Error)
		}
		return res, nil
	}
}

// RequestWithDefault is a convenience: 30s timeout if ctx has no deadline.
func (c *AgentConn) RequestWithDefault(ctx context.Context, cmd *pb.Command) (*pb.CommandResult, error) {
	if _, ok := ctx.Deadline(); !ok {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, 30*time.Second)
		defer cancel()
	}
	return c.Request(ctx, cmd)
}

// agentByID resolves an agent connection or returns a friendly 404-ish error.
// All file/cron/backup handlers call this first.
func (r *AgentRegistry) RequireAgent(nodeID string) (*AgentConn, error) {
	conn, ok := r.Get(nodeID)
	if !ok {
		return nil, fmt.Errorf("agent %q is offline", nodeID)
	}
	return conn, nil
}
