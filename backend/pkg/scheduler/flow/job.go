package flow

import (
	"context"
	"errors"

	"github.com/getarcaneapp/arcane/types/v2/scheduler"

	activitylib "github.com/getarcaneapp/arcane/backend/v2/pkg/libarcane/activity"
)

// Job runs a workflow as a scheduler job. The coordinator's single flight per
// job and environment already prevents overlapping runs.
type Job struct {
	Engine   *Engine
	Workflow *Workflow
	JobName  string
	// Payload is every instance's input, such as the entity a per-entity job runs for.
	Payload any
	// Activity overrides the definition's activity template, such as with the entity's name.
	Activity    activitylib.StartRequest
	ScheduleFn  func(ctx context.Context) string
	ShouldRunFn func(ctx context.Context) bool
	// FallbackFn recovers a run whose instance cannot resume; nil fails the run so
	// a retry starts a fresh instance under a new attempt.
	FallbackFn      func(ctx context.Context, previous scheduler.Run) (scheduler.Outcome, error)
	ValidateRetryFn func(ctx context.Context, run scheduler.Run) error
}

// Name is the scheduler job name.
func (j *Job) Name() string { return j.JobName }

// Schedule is the job's current cron expression.
func (j *Job) Schedule(ctx context.Context) string { return j.ScheduleFn(ctx) }

// ShouldSchedule reports whether the job runs; a nil ShouldRunFn always does.
func (j *Job) ShouldSchedule(ctx context.Context) bool {
	return j.ShouldRunFn == nil || j.ShouldRunFn(ctx)
}

// Run skips when the job's condition no longer holds, then runs the workflow.
func (j *Job) Run(ctx context.Context) (scheduler.Outcome, error) {
	if !j.ShouldSchedule(ctx) {
		return scheduler.Outcome{Status: scheduler.Skipped}, nil
	}
	return j.Engine.Run(ctx, j.Workflow, j.Payload, j.Activity, nil)
}

// Reconcile resumes an interrupted run's instance, falling back to FallbackFn when it cannot be resumed.
// A job disabled since the run started makes no further changes.
func (j *Job) Reconcile(ctx context.Context, previous scheduler.Run) (scheduler.Outcome, error) {
	if !j.ShouldSchedule(ctx) {
		const message = "Job is disabled; the interrupted run was not resumed"
		if outcome, finished, err := j.Engine.Retire(ctx, j.Workflow, previous, message); finished {
			return outcome, err
		}
		return scheduler.Outcome{Status: scheduler.Failed, Message: message}, nil
	}
	outcome, err := j.Engine.Resume(ctx, j.Workflow, previous)
	if !errors.Is(err, ErrNotResumable) {
		return outcome, err
	}
	if j.FallbackFn != nil {
		return j.FallbackFn(ctx, previous)
	}
	// Running again here could reuse the superseded instance's ID within this attempt.
	return scheduler.Outcome{Status: scheduler.Failed, Message: "Workflow instance could not be resumed; retry the run"}, nil
}

// ValidateRetry rejects a retry that ValidateRetryFn reports unsafe.
func (j *Job) ValidateRetry(ctx context.Context, run scheduler.Run) error {
	if j.ValidateRetryFn == nil {
		return nil
	}
	return j.ValidateRetryFn(ctx, run)
}
