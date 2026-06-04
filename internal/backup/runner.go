package backup

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

// Runner orchestrates one plan execution: resolve scope → fan out gRPC
// commands to each in-scope agent → collect per-node results → prune
// retention → fire webhook.
type Runner struct {
	db *sql.DB
	// TODO(phase 9): hold:
	//   - *leader.AgentRegistry (so we can address the right agents)
	//   - *leader.Hub (publish backup.* SSE events)
	//   - notifier.Notifier (Discord et al.)
}

// NewRunner returns a Runner bound to the leader DB.
func NewRunner(db *sql.DB) *Runner {
	return &Runner{db: db}
}

// RunPlan executes one full backup run for the given plan.
// TODO(phase 9): full implementation as described in architecture §12.
func (r *Runner) RunPlan(_ context.Context, planID, runID, triggeredBy string) (*RunResult, error) {
	_, _, _ = planID, runID, triggeredBy
	return nil, errors.New("RunPlan: TODO(phase 9)")
}

// RunResult summarizes one finished run for the Discord embed / API response.
type RunResult struct {
	RunID         string
	PlanID        string
	Status        string // success|partial|failed|cancelled
	Duration      time.Duration
	NodeResults   []NodeResult
	BytesTotal    uint64
	FailedSummary string
}

// NodeResult is one row of the per-node outcome for a run.
type NodeResult struct {
	NodeID        string
	Status        string
	S3Key         string
	BytesUploaded uint64
	FileCount     uint32
	SHA256        string
	Error         string
}

// PruneRetention deletes the oldest objects in S3 to leave only `keep`
// newest matching the plan's key prefix per node.
// TODO(phase 9).
func (r *Runner) PruneRetention(_ context.Context, planID string, keep int) (deleted int, err error) {
	_, _ = planID, keep
	return 0, errors.New("PruneRetention: TODO(phase 9)")
}
