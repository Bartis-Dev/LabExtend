// Package backup implements the leader-side cron-driven scheduler and the
// fan-out runner that talks to one or more agents per plan.
package backup

import (
	"context"
	"database/sql"
	"log/slog"
	"sync"
	"time"

	"github.com/robfig/cron/v3"
)

// Scheduler reads backup_plans every 60s, keeps a cron schedule per enabled
// plan, and fires Runner.RunPlan when each plan's next scheduled time arrives.
type Scheduler struct {
	db     *sql.DB
	runner *Runner

	cron       *cron.Cron
	planJobIDs map[string]planJob
	mu         sync.Mutex
	stopOnce   sync.Once
}

type planJob struct {
	entryID  cron.EntryID
	schedule string // remember spec so we can detect changes
}

// NewScheduler wires a Scheduler. Pass a Runner that knows how to execute a
// single plan (fan out to agents → S3).
func NewScheduler(db *sql.DB, runner *Runner) *Scheduler {
	c := cron.New(cron.WithSeconds(), cron.WithChain(cron.Recover(slogLogger{})))
	return &Scheduler{
		db:         db,
		runner:     runner,
		cron:       c,
		planJobIDs: make(map[string]planJob),
	}
}

// Run starts the scheduler and refresh loop. Blocks until ctx is canceled.
func (s *Scheduler) Run(ctx context.Context) error {
	s.cron.Start()
	defer s.stopOnce.Do(func() {
		stopCtx := s.cron.Stop()
		<-stopCtx.Done()
	})

	if err := s.Refresh(ctx); err != nil {
		slog.Warn("backup scheduler: initial refresh failed", "err", err)
	}

	t := time.NewTicker(60 * time.Second)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-t.C:
			if err := s.Refresh(ctx); err != nil {
				slog.Warn("backup scheduler: refresh failed", "err", err)
			}
		}
	}
}

// Refresh reconciles cron entries with the current `enabled` rows of
// backup_plans. Adds new ones, removes deleted/disabled, replaces those whose
// schedule changed. Safe to call from handler-write paths.
func (s *Scheduler) Refresh(ctx context.Context) error {
	type planRow struct {
		ID       string
		Name     string
		Schedule string
		Enabled  bool
	}
	rows, err := s.db.QueryContext(ctx, `SELECT id, name, schedule, enabled FROM backup_plans`)
	if err != nil {
		return err
	}
	defer rows.Close()

	current := map[string]planRow{}
	for rows.Next() {
		var p planRow
		var en int
		if err := rows.Scan(&p.ID, &p.Name, &p.Schedule, &en); err != nil {
			return err
		}
		p.Enabled = en == 1
		current[p.ID] = p
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	// Drop entries that are no longer enabled / no longer exist.
	for id, pj := range s.planJobIDs {
		p, ok := current[id]
		if !ok || !p.Enabled || p.Schedule != pj.schedule {
			s.cron.Remove(pj.entryID)
			delete(s.planJobIDs, id)
		}
	}

	// Add (back) entries we don't have yet.
	for id, p := range current {
		if !p.Enabled {
			continue
		}
		if _, exists := s.planJobIDs[id]; exists {
			continue
		}
		planID, planName := p.ID, p.Name
		eid, err := s.cron.AddFunc(p.Schedule, func() {
			s.runner.RunPlan(context.Background(), planID, planName, "schedule")
		})
		if err != nil {
			slog.Warn("backup scheduler: invalid schedule", "plan", id, "spec", p.Schedule, "err", err)
			continue
		}
		s.planJobIDs[id] = planJob{entryID: eid, schedule: p.Schedule}
	}
	return nil
}

// Trigger fires one plan immediately (manual "Run now" button). Returns the
// new run_id.
func (s *Scheduler) Trigger(ctx context.Context, planID, triggeredBy string) (string, error) {
	var name string
	if err := s.db.QueryRowContext(ctx, `SELECT name FROM backup_plans WHERE id = ?`, planID).Scan(&name); err != nil {
		return "", err
	}
	return s.runner.RunPlan(ctx, planID, name, triggeredBy), nil
}

// slogLogger adapts slog to cron's logger interface so panic recovery output
// shows up in our normal JSON logs.
type slogLogger struct{}

func (slogLogger) Info(msg string, kv ...any) { slog.Info("cron: "+msg, kv...) }
func (slogLogger) Error(err error, msg string, kv ...any) {
	slog.Error("cron: "+msg, append(kv, "err", err)...)
}
