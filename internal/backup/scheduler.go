// Package backup contains the leader-side scheduler and runner for backup
// plans. Plans are persisted in `backup_plans`; the scheduler reads them on
// boot, registers cron entries with robfig/cron, and re-evaluates whenever a
// plan is mutated through the API.
package backup

import (
	"context"
	"database/sql"
	"errors"
	"log/slog"
	"sync"

	"github.com/robfig/cron/v3"
)

// Scheduler owns a robfig/cron instance and the mapping from plan_id to
// cron EntryID, so we can add/remove entries on plan changes.
type Scheduler struct {
	db      *sql.DB
	cron    *cron.Cron
	runner  *Runner

	mu      sync.Mutex
	entries map[string]cron.EntryID // plan_id → cron entry id
}

// NewScheduler wires a Scheduler. Pass a Runner that knows how to execute a
// single plan (fan out to agents → S3).
func NewScheduler(db *sql.DB, runner *Runner) *Scheduler {
	return &Scheduler{
		db:      db,
		cron:    cron.New(cron.WithSeconds()), // accept 6-field expressions
		runner:  runner,
		entries: make(map[string]cron.EntryID),
	}
}

// Start launches the cron loop and loads existing plans.
// TODO(phase 9): SELECT enabled plans, AddFunc each one, log next_run_at.
func (s *Scheduler) Start(ctx context.Context) error {
	slog.Info("backup scheduler starting")
	s.cron.Start()
	go func() {
		<-ctx.Done()
		stopCtx := s.cron.Stop()
		<-stopCtx.Done()
	}()
	return errors.New("Scheduler.Start: TODO(phase 9) — load plans from DB")
}

// AddOrUpdatePlan (re)registers a plan with the cron. Idempotent: if a plan
// with the same ID already exists, remove its existing entry first.
// TODO(phase 9).
func (s *Scheduler) AddOrUpdatePlan(_ context.Context, planID, schedule string) error {
	_, _ = planID, schedule
	return errors.New("AddOrUpdatePlan: TODO(phase 9)")
}

// RemovePlan deregisters a plan's cron entry (called on plan delete or
// disable).
// TODO(phase 9).
func (s *Scheduler) RemovePlan(_ context.Context, planID string) error {
	_ = planID
	return errors.New("RemovePlan: TODO(phase 9)")
}

// Trigger fires a plan immediately (manual run), respecting the same scope
// resolution + per-node fan-out used by scheduled runs.
// TODO(phase 9).
func (s *Scheduler) Trigger(_ context.Context, planID, triggeredBy string) (runID string, err error) {
	_, _ = planID, triggeredBy
	return "", errors.New("Trigger: TODO(phase 9)")
}
