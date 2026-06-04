package agent

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"math/rand"
	"runtime"
	"sync"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"

	"github.com/Bartis-Dev/LabExtend/internal/config"
	pb "github.com/Bartis-Dev/LabExtend/internal/grpc/pb"
)

// version is set at build time (mirrored from cmd/manager).
var version = "dev"

// Client maintains a persistent gRPC connection to the leader with
// exponential-with-jitter reconnect.
type Client struct {
	cfg     *config.Config
	handler *Handler

	backoffInitial time.Duration
	backoffMax     time.Duration
}

// NewClient returns a configured (but not yet connected) Client.
func NewClient(cfg *config.Config) *Client {
	return &Client{
		cfg:            cfg,
		handler:        NewHandler(cfg),
		backoffInitial: 500 * time.Millisecond,
		backoffMax:     60 * time.Second,
	}
}

// Run is the agent main loop. It connects to the leader, opens the bidi
// Channel stream, runs the read/write loops until either side errors, then
// backs off and reconnects. Returns only when ctx is canceled.
func (c *Client) Run(ctx context.Context) error {
	backoff := c.backoffInitial
	for {
		if err := ctx.Err(); err != nil {
			return err
		}

		err := c.connectAndServe(ctx)
		if errors.Is(err, context.Canceled) {
			return nil
		}
		if err != nil {
			slog.Warn("agent: stream ended", "err", err, "backoff", backoff)
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(jitter(backoff)):
			}
			backoff *= 2
			if backoff > c.backoffMax {
				backoff = c.backoffMax
			}
			continue
		}

		// Clean stream end → reset backoff and reconnect quickly.
		backoff = c.backoffInitial
	}
}

// connectAndServe opens one gRPC stream and serves it until it errors or ctx
// is canceled.
func (c *Client) connectAndServe(parentCtx context.Context) error {
	slog.Info("agent: connecting", "addr", c.cfg.LeaderAddr, "agent_id", c.cfg.AgentHostID)

	conn, err := grpc.NewClient(
		c.cfg.LeaderAddr,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithDefaultServiceConfig(`{"loadBalancingPolicy":"round_robin"}`),
	)
	if err != nil {
		return fmt.Errorf("grpc dial: %w", err)
	}
	defer conn.Close()

	client := pb.NewManagerAgentClient(conn)

	// Per-stream context cancels when either side errors so both goroutines exit.
	ctx, cancel := context.WithCancel(parentCtx)
	defer cancel()

	// Metadata: agent-id + shared-secret token. Set on outgoing context.
	md := metadata.Pairs(
		"agent-id", c.cfg.AgentHostID,
		"x-agent-token", c.cfg.AgentToken,
		"agent-version", version,
	)
	streamCtx := metadata.NewOutgoingContext(ctx, md)

	stream, err := client.Channel(streamCtx)
	if err != nil {
		return fmt.Errorf("open channel: %w", err)
	}

	// 1. Send Hello immediately.
	hello := &pb.AgentMessage{
		RequestId: "",
		Kind: &pb.AgentMessage_Hello{Hello: &pb.Hello{
			Hostname:     c.cfg.AgentHostID,
			Version:      version,
			Os:           runtime.GOOS,
			Arch:         runtime.GOARCH,
			Labels:       c.cfg.AgentLabels,
			Capabilities: []string{"fs", "cron", "backup"},
		}},
	}
	if err := stream.Send(hello); err != nil {
		return fmt.Errorf("send hello: %w", err)
	}

	// 2. Wait for Welcome.
	welcomeMsg, err := stream.Recv()
	if err != nil {
		return fmt.Errorf("recv welcome: %w", err)
	}
	welcome := welcomeMsg.GetWelcome()
	if welcome == nil {
		return fmt.Errorf("first reply was not Welcome (got %T)", welcomeMsg.Kind)
	}
	slog.Info("agent: connected", "assigned_id", welcome.AssignedId,
		"server_time_ms", welcome.ServerTimeMs)

	// sendMu serializes stream.Send calls (heartbeat + command-response).
	var sendMu sync.Mutex
	safeSend := func(m *pb.AgentMessage) error {
		sendMu.Lock()
		defer sendMu.Unlock()
		return stream.Send(m)
	}

	// 3. Spawn goroutines: receive loop + heartbeat loop + monitor.
	c.handler.monitor.Start(ctx, safeSend)
	defer c.handler.monitor.Stop()

	errCh := make(chan error, 2)

	go func() { errCh <- c.recvLoop(ctx, stream, safeSend) }()
	go func() { errCh <- c.heartbeatLoop(ctx, safeSend) }()

	// Return when first goroutine errors (or ctx cancels).
	select {
	case err := <-errCh:
		cancel()
		// Wait briefly for the other goroutine to also exit.
		select {
		case <-errCh:
		case <-time.After(2 * time.Second):
		}
		return err
	case <-ctx.Done():
		cancel()
		return ctx.Err()
	}
}

// recvLoop dispatches inbound LeaderMessages to handlers and sends results back.
func (c *Client) recvLoop(
	ctx context.Context,
	stream pb.ManagerAgent_ChannelClient,
	send func(*pb.AgentMessage) error,
) error {
	for {
		msg, err := stream.Recv()
		if err != nil {
			if errors.Is(err, io.EOF) {
				return errors.New("leader closed stream")
			}
			return fmt.Errorf("recv: %w", err)
		}

		switch k := msg.Kind.(type) {
		case *pb.LeaderMessage_Ping:
			// Respond with a heartbeat-like ack via Heartbeat (cheap).
			_ = send(&pb.AgentMessage{
				RequestId: msg.RequestId,
				Kind: &pb.AgentMessage_Heartbeat{
					Heartbeat: c.handler.SampleHeartbeat(),
				},
			})

		case *pb.LeaderMessage_Command:
			result := c.handler.HandleCommand(ctx, k.Command)
			if err := send(&pb.AgentMessage{
				RequestId: msg.RequestId,
				Kind:      &pb.AgentMessage_Result{Result: result},
			}); err != nil {
				return fmt.Errorf("send result: %w", err)
			}

		case *pb.LeaderMessage_Welcome:
			// Stray re-Welcome inside the stream — log + ignore.
			slog.Warn("agent: unexpected mid-stream Welcome")

		default:
			slog.Warn("agent: unknown LeaderMessage kind",
				"kind", fmt.Sprintf("%T", msg.Kind))
		}
	}
}

// heartbeatLoop sends Heartbeat every HeartbeatInterval until ctx is done.
func (c *Client) heartbeatLoop(
	ctx context.Context,
	send func(*pb.AgentMessage) error,
) error {
	t := time.NewTicker(c.cfg.HeartbeatInterval)
	defer t.Stop()

	// Send one heartbeat immediately so the leader gets metrics fast.
	if err := send(&pb.AgentMessage{
		Kind: &pb.AgentMessage_Heartbeat{Heartbeat: c.handler.SampleHeartbeat()},
	}); err != nil {
		return fmt.Errorf("heartbeat send: %w", err)
	}

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-t.C:
			if err := send(&pb.AgentMessage{
				Kind: &pb.AgentMessage_Heartbeat{Heartbeat: c.handler.SampleHeartbeat()},
			}); err != nil {
				return fmt.Errorf("heartbeat send: %w", err)
			}
		}
	}
}

// jitter returns a duration in [d/2, d*1.5).
func jitter(d time.Duration) time.Duration {
	if d <= 0 {
		return 0
	}
	half := d / 2
	return half + time.Duration(rand.Int63n(int64(d)))
}
