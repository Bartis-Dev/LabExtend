package agent

import (
	"context"

	pb "github.com/Bartis-Dev/LabExtend/internal/grpc/pb"
)

// SampleHeartbeat collects current host metrics for a heartbeat message.
func (h *Handler) SampleHeartbeat() *pb.Heartbeat {
	return h.host.Sample()
}

// HandleCommand dispatches a single LeaderMessage.Command to the right
// handler method and returns the matching CommandResult.
func (h *Handler) HandleCommand(ctx context.Context, cmd *pb.Command) *pb.CommandResult {
	switch op := cmd.Op.(type) {

	// ── filesystem ────────────────────────────────────────────────────────
	case *pb.Command_ListPath:
		r, err := h.ListPath(ctx, op.ListPath)
		return wrap(err, &pb.CommandResult{Payload: &pb.CommandResult_ListPath{ListPath: r}})

	// ── cron ──────────────────────────────────────────────────────────────
	case *pb.Command_ListCron:
		r, err := h.ListCron(ctx)
		return wrap(err, &pb.CommandResult{Payload: &pb.CommandResult_ListCron{ListCron: r}})

	case *pb.Command_ApplyCron:
		r, err := h.ApplyCron(ctx, op.ApplyCron)
		return wrap(err, &pb.CommandResult{Payload: &pb.CommandResult_ApplyCron{ApplyCron: r}})

	// ── backup ────────────────────────────────────────────────────────────
	case *pb.Command_RunBackup:
		r, err := h.RunBackup(ctx, op.RunBackup)
		return wrap(err, &pb.CommandResult{Payload: &pb.CommandResult_RunBackup{RunBackup: r}})

	case *pb.Command_CancelBackup:
		r, err := h.CancelBackup(ctx, op.CancelBackup)
		return wrap(err, &pb.CommandResult{Payload: &pb.CommandResult_CancelBackup{CancelBackup: r}})

	// ── swarm service control ───────────────────────────────────────────────
	case *pb.Command_ServiceUpdate:
		r, err := h.ServiceUpdate(ctx, op.ServiceUpdate)
		return wrap(err, &pb.CommandResult{Payload: &pb.CommandResult_ServiceUpdate{ServiceUpdate: r}})

	case *pb.Command_SwarmNodes:
		r, err := h.SwarmNodes(ctx, op.SwarmNodes)
		return wrap(err, &pb.CommandResult{Payload: &pb.CommandResult_SwarmNodes{SwarmNodes: r}})

	// ── monitoring control ────────────────────────────────────────────────
	case *pb.Command_EnableLogs:
		h.monitor.SetLogsEnabled(op.EnableLogs.GetContainerId(), true)
		return &pb.CommandResult{Ok: true, Payload: &pb.CommandResult_EnableLogs{EnableLogs: &pb.EnableLogsResp{}}}

	case *pb.Command_DisableLogs:
		h.monitor.SetLogsEnabled(op.DisableLogs.GetContainerId(), false)
		return &pb.CommandResult{Ok: true, Payload: &pb.CommandResult_DisableLogs{DisableLogs: &pb.DisableLogsResp{}}}

	case *pb.Command_Exec:
		return execNotEnabled()

	default:
		return &pb.CommandResult{Ok: false, Error: "unknown command op"}
	}
}

// wrap mutates the prebuilt CommandResult to set Ok/Error from a Go error.
func wrap(err error, r *pb.CommandResult) *pb.CommandResult {
	if err != nil {
		r.Ok = false
		r.Error = err.Error()
	} else {
		r.Ok = true
	}
	return r
}

func execNotEnabled() *pb.CommandResult {
	return &pb.CommandResult{
		Ok:    false,
		Error: "Exec disabled (set BPM_ALLOW_EXEC=true to enable)",
	}
}
