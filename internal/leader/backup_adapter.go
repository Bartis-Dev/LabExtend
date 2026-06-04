package leader

import (
	"context"

	"github.com/Bartis-Dev/LabExtend/internal/backup"
	pb "github.com/Bartis-Dev/LabExtend/internal/grpc/pb"
)

// registryAdapter implements backup.AgentRequester using AgentRegistry. We
// keep this in the leader package so the backup package never imports leader
// (which would be cyclic via Hub + AgentConn).
type registryAdapter struct {
	reg *AgentRegistry
}

func newRegistryAdapter(reg *AgentRegistry) backup.AgentRequester {
	return &registryAdapter{reg: reg}
}

func (a *registryAdapter) ListAgents() []backup.AgentInfo {
	all := a.reg.List()
	out := make([]backup.AgentInfo, 0, len(all))
	for _, c := range all {
		out = append(out, backup.AgentInfo{ID: c.ID, Hostname: c.Hostname, Labels: c.Labels})
	}
	return out
}

func (a *registryAdapter) Request(ctx context.Context, agentID string, cmd *pb.Command) (*pb.CommandResult, error) {
	conn, err := a.reg.RequireAgent(agentID)
	if err != nil {
		return nil, err
	}
	return conn.RequestWithDefault(ctx, cmd)
}

// hubPublisher implements backup.Publisher via the SSE hub.
type hubPublisher struct{ h *Hub }

func newHubPublisher(h *Hub) backup.Publisher { return &hubPublisher{h: h} }
func (p *hubPublisher) Publish(topic string, data any) {
	if p.h != nil {
		p.h.Publish(topic, data)
	}
}
