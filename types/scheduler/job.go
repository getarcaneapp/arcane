package scheduler

import (
	"context"
	"time"
)

// JobRuntimeState describes the schedule currently installed in a scheduler.
// It intentionally exposes only read-only state needed by job-management APIs.
type JobRuntimeState struct {
	Schedule  string
	NextRun   *time.Time
	Scheduled bool
}

type Job interface {
	Name() string
	Schedule(ctx context.Context) string
	Run(ctx context.Context)
}

// ConditionalJob allows a job to opt out of cron registration when it is disabled.
// Jobs that do not implement this interface are always scheduled.
type ConditionalJob interface {
	ShouldSchedule(ctx context.Context) bool
}

// GenericJob is a reusable Job built from closures. It lets a service register a
// per-entity dynamic job (e.g. one per GitOps sync or one per environment) without
// importing the scheduler package: the service constructs a GenericJob and hands it
// to the scheduler through the types/scheduler.Job interface.
//
// JobName must be unique per logical job; per-entity jobs use a "<subsystem>:<entityID>"
// scheme (e.g. "gitops-sync:abc123"). ShouldRunFn is optional — when nil the job is
// always scheduled, matching the behavior of a Job that does not implement
// ConditionalJob.
type GenericJob struct {
	JobName     string
	ScheduleFn  func(ctx context.Context) string
	RunFn       func(ctx context.Context)
	ShouldRunFn func(ctx context.Context) bool
}

func (g *GenericJob) Name() string { return g.JobName }

func (g *GenericJob) Schedule(ctx context.Context) string { return g.ScheduleFn(ctx) }

func (g *GenericJob) Run(ctx context.Context) { g.RunFn(ctx) }

// ShouldSchedule satisfies ConditionalJob. A GenericJob without a ShouldRunFn is
// always scheduled; the scheduler treats a ConditionalJob returning false as "do
// not schedule", so nil must map to true rather than a nil-func panic.
func (g *GenericJob) ShouldSchedule(ctx context.Context) bool {
	if g.ShouldRunFn == nil {
		return true
	}
	return g.ShouldRunFn(ctx)
}
