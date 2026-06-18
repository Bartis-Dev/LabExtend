package leader

import (
	"net/http"

	pb "github.com/Bartis-Dev/LabExtend/internal/grpc/pb"
)

// ServiceDeps groups what the swarm-service handlers need. The action always
// targets the leader's own in-process agent: `docker service update` is a
// swarm-manager operation and the leader node is the pinned manager, so we do
// not let the operator pick an arbitrary (possibly worker) node.
type ServiceDeps struct {
	Registry     *AgentRegistry
	Audit        *AuditLogger
	LeaderNodeID string // cfg.AgentHostID, i.e. the local agent's registry key
	ServiceName  string // service to force-update (default portainer_agent)
}

// RestartPortainerAgent force-redeploys the configured Portainer agent service
// across the swarm, equivalent to `docker service update --force portainer_agent`.
// One click fixes Portainer's stale "no agent on environment" state.
func (d *ServiceDeps) RestartPortainerAgent(w http.ResponseWriter, r *http.Request) {
	conn, err := d.Registry.RequireAgent(d.LeaderNodeID)
	if err != nil {
		// Leader's own agent loop isn't connected yet (or never started) — there
		// is no manager socket we can reach.
		writeErr(w, http.StatusServiceUnavailable, err)
		return
	}

	res, err := conn.RequestWithDefault(r.Context(), &pb.Command{
		Op: &pb.Command_ServiceUpdate{ServiceUpdate: &pb.ServiceUpdateReq{
			Service: d.ServiceName,
		}},
	})
	if err != nil {
		d.Audit.Log(r.Context(), r, "service.force_update.failed", "service", d.ServiceName,
			map[string]any{"error": err.Error()})
		writeErr(w, http.StatusBadGateway, err)
		return
	}

	out := res.GetServiceUpdate()
	d.Audit.Log(r.Context(), r, "service.force_update", "service", d.ServiceName,
		map[string]any{"service_id": out.GetServiceId(), "force_counter": out.GetForceCounter()})
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":            true,
		"service":       d.ServiceName,
		"service_id":    out.GetServiceId(),
		"force_counter": out.GetForceCounter(),
	})
}
