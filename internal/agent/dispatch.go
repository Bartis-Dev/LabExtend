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

	case *pb.Command_Stat:
		r, err := h.Stat(ctx, op.Stat)
		return wrap(err, &pb.CommandResult{Payload: &pb.CommandResult_Stat{Stat: r}})

	case *pb.Command_ReadFile:
		r, err := h.ReadFile(ctx, op.ReadFile)
		return wrap(err, &pb.CommandResult{Payload: &pb.CommandResult_ReadFile{ReadFile: r}})

	case *pb.Command_WriteFile:
		r, err := h.WriteFile(ctx, op.WriteFile)
		return wrap(err, &pb.CommandResult{Payload: &pb.CommandResult_WriteFile{WriteFile: r}})

	case *pb.Command_Mkdir:
		r, err := h.Mkdir(ctx, op.Mkdir)
		return wrap(err, &pb.CommandResult{Payload: &pb.CommandResult_Mkdir{Mkdir: r}})

	case *pb.Command_Rename:
		r, err := h.Rename(ctx, op.Rename)
		return wrap(err, &pb.CommandResult{Payload: &pb.CommandResult_Rename{Rename: r}})

	case *pb.Command_Delete:
		r, err := h.Delete(ctx, op.Delete)
		return wrap(err, &pb.CommandResult{Payload: &pb.CommandResult_Delete{Delete: r}})

	case *pb.Command_Chown:
		r, err := h.Chown(ctx, op.Chown)
		return wrap(err, &pb.CommandResult{Payload: &pb.CommandResult_Chown{Chown: r}})

	case *pb.Command_LookupUser:
		r, err := h.LookupUser(ctx, op.LookupUser)
		return wrap(err, &pb.CommandResult{Payload: &pb.CommandResult_LookupUser{LookupUser: r}})

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
