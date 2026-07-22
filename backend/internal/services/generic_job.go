package services

import "context"

// GenericJob is a closure-backed job used for per-entity schedules owned by services.
// JobName must be unique per logical job. ShouldRunFn is optional; when nil the
// job is always eligible for scheduling.
type GenericJob struct {
	JobName     string
	ScheduleFn  func(ctx context.Context) string
	RunFn       func(ctx context.Context)
	ShouldRunFn func(ctx context.Context) bool
}

func (g *GenericJob) Name() string { return g.JobName }

func (g *GenericJob) Schedule(ctx context.Context) string { return g.ScheduleFn(ctx) }

func (g *GenericJob) Run(ctx context.Context) { g.RunFn(ctx) }

// ShouldSchedule lets a job opt out of cron registration. A missing predicate
// means the job is always scheduled.
func (g *GenericJob) ShouldSchedule(ctx context.Context) bool {
	if g.ShouldRunFn == nil {
		return true
	}
	return g.ShouldRunFn(ctx)
}
