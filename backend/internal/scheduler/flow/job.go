package flow

import (
	"context"
	"errors"

	activitylib "getarcane.dev/arcane/v3/pkg/activity"
	"getarcane.dev/types/v3"
)

type (
	Job struct {
		Payload         any
		Engine          *Engine
		Workflow        *Workflow
		ScheduleFn      func(ctx context.Context) string
		ShouldRunFn     func(ctx context.Context) bool
		FallbackFn      func(ctx context.Context, previous types.SchedulerRun) (types.SchedulerOutcome, error)
		ValidateRetryFn func(ctx context.Context, run types.SchedulerRun) error
		JobName         string
		Activity        activitylib.StartRequest
	}
)

func (j *Job) Name() string { return j.JobName }

func (j *Job) Schedule(ctx context.Context) string { return j.ScheduleFn(ctx) }

func (j *Job) ShouldSchedule(ctx context.Context) bool {
	return j.ShouldRunFn == nil || j.ShouldRunFn(ctx)
}

func (j *Job) Run(ctx context.Context) (types.SchedulerOutcome, error) {
	if !j.ShouldSchedule(ctx) {
		return types.SchedulerOutcome{Status: types.RunStatusSkipped}, nil
	}
	return j.Engine.Run(ctx, j.Workflow, j.Payload, j.Activity, nil)
}

// A job disabled since the run started makes no further changes.
func (j *Job) Reconcile(ctx context.Context, previous types.SchedulerRun) (types.SchedulerOutcome, error) {
	if !j.ShouldSchedule(ctx) {
		const message = "Job is disabled; the interrupted run was not resumed"
		if outcome, finished, err := j.Engine.Retire(ctx, j.Workflow, previous, message); finished {
			return outcome, err
		}
		return types.SchedulerOutcome{Status: types.RunStatusFailed, Message: message}, nil
	}
	outcome, err := j.Engine.Resume(ctx, j.Workflow, previous)
	if !errors.Is(err, ErrNotResumable) {
		return outcome, err
	}
	if j.FallbackFn != nil {
		return j.FallbackFn(ctx, previous)
	}
	// Running again here could reuse the superseded instance's ID within this attempt.
	return types.SchedulerOutcome{Status: types.RunStatusFailed, Message: "Workflow instance could not be resumed; retry the run"}, nil
}

func (j *Job) ValidateRetry(ctx context.Context, run types.SchedulerRun) error {
	if j.ValidateRetryFn == nil {
		return nil
	}
	return j.ValidateRetryFn(ctx, run)
}
