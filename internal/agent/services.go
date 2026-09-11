package agent

import (
	"context"
	"errors"

	pb "github.com/Bartis-Dev/LabExtend/internal/grpc/pb"
)

// ServiceUpdate force-redeploys a swarm service (the Engine-API equivalent of
// `docker service update --force <service>`). Only meaningful when this agent
// runs on a swarm manager; on a worker the engine returns "not a swarm manager"
// and that error is propagated back to the leader unchanged.
func (h *Handler) ServiceUpdate(ctx context.Context, req *pb.ServiceUpdateReq) (*pb.ServiceUpdateResp, error) {
	if req.GetService() == "" {
		return nil, errors.New("service name is required")
	}
	id, force, err := h.monitor.docker.ServiceForceUpdate(ctx, req.GetService())
	if err != nil {
		return nil, err
	}
	return &pb.ServiceUpdateResp{ServiceId: id, ForceCounter: force}, nil
}

// SwarmNodes returns the cluster's node list with the labels placement
// constraints match. Manager-only, same as ServiceUpdate above.
func (h *Handler) SwarmNodes(ctx context.Context, _ *pb.SwarmNodesReq) (*pb.SwarmNodesResp, error) {
	nodes, err := h.monitor.docker.ListSwarmNodes(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]*pb.SwarmNode, 0, len(nodes))
	for _, n := range nodes {
		sn := &pb.SwarmNode{
			Id:           n.ID,
			Hostname:     n.Description.Hostname,
			Role:         n.Spec.Role,
			Availability: n.Spec.Availability,
			State:        n.Status.State,
			Labels:       n.Spec.Labels,
		}
		if n.ManagerStatus != nil {
			sn.Leader = n.ManagerStatus.Leader
		}
		out = append(out, sn)
	}
	return &pb.SwarmNodesResp{Nodes: out}, nil
}
