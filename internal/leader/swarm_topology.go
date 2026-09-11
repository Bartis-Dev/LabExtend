package leader

import (
	"context"
	"log/slog"
	"strings"
	"sync"
	"time"

	pb "github.com/Bartis-Dev/LabExtend/internal/grpc/pb"
)

// swarmTopology caches the cluster's node list.
//
// GET /nodes is manager-only, so it is answered by the leader's own in-process
// agent rather than by each node about itself. That is one gRPC round trip for
// the whole cluster, and the answer changes about as often as someone runs
// `docker node update` — hence the cache, so opening the Nodes page every few
// seconds does not hit the Docker API every time.
type swarmTopology struct {
	registry     *AgentRegistry
	leaderNodeID string
	ttl          time.Duration

	mu        sync.Mutex
	byID      map[string]*pb.SwarmNode // keyed by lowercase hostname AND swarm id
	fetchedAt time.Time
	lastErr   error
}

func newSwarmTopology(reg *AgentRegistry, leaderNodeID string) *swarmTopology {
	return &swarmTopology{
		registry:     reg,
		leaderNodeID: leaderNodeID,
		ttl:          30 * time.Second,
	}
}

// lookup returns the swarm entry for an agent, matched on hostname or on the
// agent's host id. An agent's BPM_AGENT_HOST_ID defaults to its hostname but
// can be set to anything, so neither key alone is reliable.
func (t *swarmTopology) lookup(ctx context.Context, agentID, hostname string) *pb.SwarmNode {
	if t == nil {
		return nil
	}
	m := t.snapshot(ctx)
	if m == nil {
		return nil
	}
	if n, ok := m[strings.ToLower(hostname)]; ok {
		return n
	}
	return m[strings.ToLower(agentID)]
}

// snapshot returns the cached map, refreshing it when stale.
func (t *swarmTopology) snapshot(ctx context.Context) map[string]*pb.SwarmNode {
	t.mu.Lock()
	defer t.mu.Unlock()

	if t.byID != nil && time.Since(t.fetchedAt) < t.ttl {
		return t.byID
	}
	// A failed fetch is also cached for the TTL. Without that, a cluster whose
	// leader is not a manager would issue a doomed gRPC round trip on every
	// single request to /api/nodes.
	if t.byID == nil && t.lastErr != nil && time.Since(t.fetchedAt) < t.ttl {
		return nil
	}

	nodes, err := t.fetch(ctx)
	t.fetchedAt = time.Now()
	t.lastErr = err
	if err != nil {
		// Debug, not warn: on a cluster whose leader sits on a worker this is
		// the expected steady state, and it would otherwise log every 30s.
		slog.Debug("swarm topology unavailable", "err", err)
		t.byID = nil
		return nil
	}

	m := make(map[string]*pb.SwarmNode, len(nodes)*2)
	for _, n := range nodes {
		if h := strings.ToLower(n.GetHostname()); h != "" {
			m[h] = n
		}
		if id := strings.ToLower(n.GetId()); id != "" {
			m[id] = n
		}
	}
	t.byID = m
	return m
}

func (t *swarmTopology) fetch(ctx context.Context) ([]*pb.SwarmNode, error) {
	conn, err := t.registry.RequireAgent(t.leaderNodeID)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	res, err := conn.RequestWithDefault(ctx, &pb.Command{
		Op: &pb.Command_SwarmNodes{SwarmNodes: &pb.SwarmNodesReq{}},
	})
	if err != nil {
		return nil, err
	}
	return res.GetSwarmNodes().GetNodes(), nil
}
