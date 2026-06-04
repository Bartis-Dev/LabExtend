package leader

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"sync"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"

	"github.com/Bartis-Dev/LabExtend/internal/config"
	pb "github.com/Bartis-Dev/LabExtend/internal/grpc/pb"
)

// AgentConn is the leader-side handle to a connected agent.
type AgentConn struct {
	ID          string
	Hostname    string
	Version     string
	OS          string
	Arch        string
	Labels      map[string]string
	ConnectedAt time.Time
	LastSeen    time.Time

	// stream is the bidi gRPC stream used to push commands to the agent.
	stream pb.ManagerAgent_ChannelServer

	// sendMu serializes Sends because gRPC streams are not safe for concurrent
	// writers.
	sendMu sync.Mutex

	// pending request_id → channel waiting for the matching CommandResult.
	pendingMu sync.Mutex
	pending   map[string]chan *pb.AgentMessage
}

// Send pushes a LeaderMessage to the agent.
func (c *AgentConn) Send(msg *pb.LeaderMessage) error {
	c.sendMu.Lock()
	defer c.sendMu.Unlock()
	return c.stream.Send(msg)
}

// AgentRegistry tracks all connected agents and routes commands to them.
type AgentRegistry struct {
	mu         sync.RWMutex
	agents     map[string]*AgentConn // keyed by agent ID
	hub        *Hub
	metrics    *metricsStore
	containers *containerStore
}

// NewAgentRegistry returns an empty registry.
func NewAgentRegistry() *AgentRegistry {
	return &AgentRegistry{agents: make(map[string]*AgentConn)}
}

// SetHub attaches the SSE hub so registry events are broadcast to UI clients.
func (r *AgentRegistry) SetHub(h *Hub) { r.hub = h }

// SetMetrics attaches the metrics store so disconnects clear live samples.
func (r *AgentRegistry) SetMetrics(m *metricsStore) { r.metrics = m }

// SetContainers attaches the container store so disconnects drop live containers.
func (r *AgentRegistry) SetContainers(c *containerStore) { r.containers = c }

// Register adds an agent connection. If the same agent ID was already
// connected, returns the previous entry so the caller can close its stream.
func (r *AgentRegistry) Register(c *AgentConn) (previous *AgentConn) {
	r.mu.Lock()
	defer r.mu.Unlock()
	prev := r.agents[c.ID]
	r.agents[c.ID] = c
	if r.hub != nil {
		r.hub.Publish("node.connected", map[string]any{
			"id": c.ID, "hostname": c.Hostname, "version": c.Version,
		})
	}
	return prev
}

// Unregister removes an agent by ID if it matches the given conn. Also
// drops the node's live metrics + containers so the UI shows it as offline
// immediately (DB rows survive for the "last-seen" view).
func (r *AgentRegistry) Unregister(c *AgentConn) {
	r.mu.Lock()
	cur, ok := r.agents[c.ID]
	if ok && cur == c {
		delete(r.agents, c.ID)
	}
	metrics := r.metrics
	containers := r.containers
	hub := r.hub
	r.mu.Unlock()
	if !ok || cur != c {
		return
	}
	if metrics != nil {
		metrics.MarkOffline(c.ID)
	}
	if containers != nil {
		containers.MarkNodeOffline(c.ID)
	}
	if hub != nil {
		hub.Publish("node.disconnected", map[string]any{"id": c.ID})
	}
}

// Get returns the live connection for an agent ID, if connected.
func (r *AgentRegistry) Get(id string) (*AgentConn, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	c, ok := r.agents[id]
	return c, ok
}

// List returns a snapshot of all connected agents.
func (r *AgentRegistry) List() []*AgentConn {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]*AgentConn, 0, len(r.agents))
	for _, c := range r.agents {
		out = append(out, c)
	}
	return out
}

// ManagerAgentServer is the leader-side implementation of the gRPC service.
type ManagerAgentServer struct {
	pb.UnimplementedManagerAgentServer

	registry   *AgentRegistry
	db         *sql.DB
	cfg        *config.Config
	metrics    *metricsStore
	containers *containerStore
	logs       *logStore
}

// Channel handles a single agent's persistent bidi stream.
// Lifecycle:
//   1. First message MUST be AgentMessage.Hello (auth via metadata already done)
//   2. Send Welcome with assigned_id
//   3. Loop: receive Heartbeat / CommandResult / Event; on heartbeat update DB
//   4. On stream EOF or error: Unregister and emit node.disconnected
func (s *ManagerAgentServer) Channel(stream pb.ManagerAgent_ChannelServer) error {
	ctx := stream.Context()
	agentID := agentIDFromContext(ctx)
	if agentID == "" {
		return status.Errorf(codes.Unauthenticated, "missing agent-id metadata")
	}

	// First message must be Hello.
	first, err := stream.Recv()
	if err != nil {
		return status.Errorf(codes.InvalidArgument, "recv hello: %v", err)
	}
	hello := first.GetHello()
	if hello == nil {
		return status.Errorf(codes.InvalidArgument, "first message must be Hello, got %T", first.Kind)
	}

	conn := &AgentConn{
		ID:          agentID,
		Hostname:    hello.Hostname,
		Version:     hello.Version,
		OS:          hello.Os,
		Arch:        hello.Arch,
		Labels:      hello.Labels,
		ConnectedAt: time.Now(),
		LastSeen:    time.Now(),
		stream:      stream,
		pending:     make(map[string]chan *pb.AgentMessage),
	}

	if prev := s.registry.Register(conn); prev != nil {
		slog.Warn("agent reconnected — superseded previous session",
			"agent_id", agentID, "hostname", hello.Hostname)
	}
	defer s.registry.Unregister(conn)
	defer slog.Info("agent disconnected", "agent_id", agentID, "hostname", hello.Hostname)

	if err := s.upsertNode(ctx, conn); err != nil {
		slog.Warn("upsert node failed", "agent_id", agentID, "err", err)
	}

	if err := conn.Send(&pb.LeaderMessage{
		RequestId: "",
		Kind: &pb.LeaderMessage_Welcome{Welcome: &pb.Welcome{
			AssignedId:   agentID,
			ServerTimeMs: time.Now().UnixMilli(),
		}},
	}); err != nil {
		return fmt.Errorf("send welcome: %w", err)
	}

	slog.Info("agent connected",
		"agent_id", agentID, "hostname", hello.Hostname,
		"version", hello.Version, "labels", hello.Labels,
	)

	// Receive loop.
	for {
		msg, err := stream.Recv()
		if err != nil {
			if errors.Is(err, context.Canceled) {
				return nil
			}
			return err // EOF or transport error → unregister
		}
		conn.LastSeen = time.Now()

		switch k := msg.Kind.(type) {
		case *pb.AgentMessage_Heartbeat:
			if err := s.applyHeartbeat(ctx, conn, k.Heartbeat); err != nil {
				slog.Warn("heartbeat persist failed", "agent_id", agentID, "err", err)
			}
		case *pb.AgentMessage_Result:
			s.dispatchResult(conn, msg.RequestId, msg)
		case *pb.AgentMessage_Event:
			s.handleEvent(conn, k.Event)
		case *pb.AgentMessage_Hello:
			// Already handled. Re-Hello inside the stream is weird; log and ignore.
			slog.Warn("unexpected mid-stream Hello", "agent_id", agentID)
		default:
			slog.Warn("unknown AgentMessage kind", "agent_id", agentID, "kind", fmt.Sprintf("%T", k))
		}
	}
}

// dispatchResult delivers a CommandResult to the goroutine waiting on it.
func (s *ManagerAgentServer) dispatchResult(c *AgentConn, requestID string, msg *pb.AgentMessage) {
	if requestID == "" {
		slog.Warn("CommandResult with empty request_id, dropping", "agent_id", c.ID)
		return
	}
	c.pendingMu.Lock()
	ch, ok := c.pending[requestID]
	if ok {
		delete(c.pending, requestID)
	}
	c.pendingMu.Unlock()
	if !ok {
		slog.Warn("no waiter for request_id", "agent_id", c.ID, "request_id", requestID)
		return
	}
	select {
	case ch <- msg:
	default:
		// Buffered channel of size 1; if already filled, drop silently.
	}
}

// handleEvent processes unsolicited events from the agent.
func (s *ManagerAgentServer) handleEvent(c *AgentConn, ev *pb.Event) {
	switch k := ev.Kind.(type) {
	case *pb.Event_ContainerReport:
		ctx := context.Background()
		if s.containers != nil {
			updated := s.containers.Apply(ctx, c.ID, k.ContainerReport)
			if s.registry != nil && s.registry.hub != nil {
				s.registry.hub.Publish("containers.update", map[string]any{
					"node_id":    c.ID,
					"containers": updated,
				})
			}
		}
	case *pb.Event_LogBatch:
		if s.logs != nil {
			s.logs.Apply(context.Background(), c.ID, k.LogBatch)
		}
	case *pb.Event_BackupProgress, *pb.Event_BackupLog, *pb.Event_CronChanged:
		// Routed in later phases.
		slog.Debug("event from agent", "agent_id", c.ID, "kind", fmt.Sprintf("%T", k))
	default:
		slog.Debug("unknown event kind", "agent_id", c.ID, "kind", fmt.Sprintf("%T", k))
	}
}

// applyHeartbeat ingests the heartbeat into the metrics store, persists it,
// and broadcasts the computed sample over SSE.
func (s *ManagerAgentServer) applyHeartbeat(ctx context.Context, c *AgentConn, h *pb.Heartbeat) error {
	if s.metrics == nil {
		return nil // unit tests without store
	}
	sample := s.metrics.Apply(c.ID, h)
	if s.registry != nil && s.registry.hub != nil {
		s.registry.hub.Publish("node.metrics", sample)
	}
	return s.metrics.Persist(ctx, sample)
}

// upsertNode writes the nodes row for a freshly connected agent.
func (s *ManagerAgentServer) upsertNode(ctx context.Context, c *AgentConn) error {
	if s.db == nil {
		return nil
	}
	now := time.Now().Unix()
	labelsJSON, _ := marshalStringMap(c.Labels)
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO nodes (id, hostname, os, arch, version, labels_json, first_seen, last_seen, status)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, 'online')
		ON CONFLICT(id) DO UPDATE SET
			hostname    = excluded.hostname,
			os          = excluded.os,
			arch        = excluded.arch,
			version     = excluded.version,
			labels_json = excluded.labels_json,
			last_seen   = excluded.last_seen,
			status      = 'online'
	`,
		c.ID, c.Hostname, c.OS, c.Arch, c.Version, labelsJSON, now, now,
	)
	return err
}

// startGRPCServer binds and serves the gRPC listener until ctx is canceled.
func startGRPCServer(ctx context.Context, cfg *config.Config, reg *AgentRegistry, hub *Hub, database *sql.DB, metrics *metricsStore, cnts *containerStore, logs *logStore) error {
	reg.SetHub(hub)
	reg.SetMetrics(metrics)
	reg.SetContainers(cnts)

	lis, err := net.Listen("tcp", cfg.GRPCAddr)
	if err != nil {
		return fmt.Errorf("listen %s: %w", cfg.GRPCAddr, err)
	}
	slog.Info("grpc: listening", "addr", cfg.GRPCAddr)

	authInt := newAuthInterceptor(cfg.AgentToken)
	opts := []grpc.ServerOption{
		grpc.UnaryInterceptor(authInt.unary),
		grpc.StreamInterceptor(authInt.stream),
	}
	if cfg.GRPCTLSCert != "" && cfg.GRPCTLSKey != "" {
		tlsCreds, err := loadServerTLS(cfg.GRPCTLSCert, cfg.GRPCTLSKey, cfg.GRPCTLSClientCA)
		if err != nil {
			return fmt.Errorf("load tls: %w", err)
		}
		opts = append(opts, grpc.Creds(tlsCreds))
		slog.Info("grpc: mTLS enabled", "client_ca", cfg.GRPCTLSClientCA != "")
	}
	srv := grpc.NewServer(opts...)
	pb.RegisterManagerAgentServer(srv, &ManagerAgentServer{
		registry:   reg,
		db:         database,
		cfg:        cfg,
		metrics:    metrics,
		containers: cnts,
		logs:       logs,
	})

	go func() {
		<-ctx.Done()
		slog.Info("grpc: shutting down")
		srv.GracefulStop()
	}()

	if err := srv.Serve(lis); err != nil && !errors.Is(err, grpc.ErrServerStopped) {
		return fmt.Errorf("grpc serve: %w", err)
	}
	return nil
}

// agentIDFromContext extracts the agent-id metadata set by the agent's gRPC
// client outgoing context.
func agentIDFromContext(ctx context.Context) string {
	md, ok := metadata.FromIncomingContext(ctx)
	if !ok {
		return ""
	}
	v := md.Get("agent-id")
	if len(v) == 0 {
		return ""
	}
	return v[0]
}

// marshalStringMap JSON-encodes a label map for the DB column.
func marshalStringMap(m map[string]string) (string, error) {
	if len(m) == 0 {
		return "{}", nil
	}
	b, err := json.Marshal(m)
	return string(b), err
}
