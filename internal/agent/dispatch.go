package agent

import (
	"context"

	pb "github.com/Bartis-Dev/LabExtend/internal/grpc/pb"
)

// SampleHeartbeat collects current host metrics for a heartbeat message.
// OS-specific collector state (CPU-delta etc) is held in h.host so that
// rate-of-change fields work correctly across calls.
func (h *Handler) SampleHeartbeat() *pb.Heartbeat {
	return h.host.Sample()
}

// HandleCommand dispatches a single LeaderMessage.Command to the right
// handler method and returns the matching CommandResult.
//
// Phase 3: returns Unimplemented for everything except the trivial cases.
// Phases 6/7/9 fill in fs/cron/backup branches.
func (h *Handler) HandleCommand(ctx context.Context, cmd *pb.Command) *pb.CommandResult {
	_ = ctx

	switch cmd.Op.(type) {

	case *pb.Command_ListPath:
		return unimplemented("ListPath (phase 6)")

	case *pb.Command_Stat:
		return unimplemented("Stat (phase 6)")

	case *pb.Command_ReadFile:
		return unimplemented("ReadFile (phase 6)")

	case *pb.Command_WriteFile:
		return unimplemented("WriteFile (phase 6)")

	case *pb.Command_Mkdir:
		return unimplemented("Mkdir (phase 6)")

	case *pb.Command_Rename:
		return unimplemented("Rename (phase 6)")

	case *pb.Command_Delete:
		return unimplemented("Delete (phase 6)")

	case *pb.Command_Chown:
		return unimplemented("Chown (phase 6)")

	case *pb.Command_LookupUser:
		return unimplemented("LookupUser (phase 6)")

	case *pb.Command_ListCron:
		return unimplemented("ListCron (phase 7)")

	case *pb.Command_ApplyCron:
		return unimplemented("ApplyCron (phase 7)")

	case *pb.Command_RunBackup:
		return unimplemented("RunBackup (phase 12)")

	case *pb.Command_CancelBackup:
		return unimplemented("CancelBackup (phase 12)")

	case *pb.Command_EnableLogs:
		req := cmd.GetEnableLogs()
		h.monitor.SetLogsEnabled(req.GetContainerId(), true)
		return &pb.CommandResult{
			Ok:      true,
			Payload: &pb.CommandResult_EnableLogs{EnableLogs: &pb.EnableLogsResp{}},
		}

	case *pb.Command_DisableLogs:
		req := cmd.GetDisableLogs()
		h.monitor.SetLogsEnabled(req.GetContainerId(), false)
		return &pb.CommandResult{
			Ok:      true,
			Payload: &pb.CommandResult_DisableLogs{DisableLogs: &pb.DisableLogsResp{}},
		}

	case *pb.Command_Exec:
		return execNotEnabled()

	default:
		return &pb.CommandResult{
			Ok:    false,
			Error: "unknown command op",
		}
	}
}

func unimplemented(name string) *pb.CommandResult {
	return &pb.CommandResult{
		Ok:    false,
		Error: name + ": not implemented yet",
	}
}

func execNotEnabled() *pb.CommandResult {
	return &pb.CommandResult{
		Ok:    false,
		Error: "Exec disabled (set BPM_ALLOW_EXEC=true and rebuild handler dispatch in phase 9)",
	}
}
