package scheduler

import "context"

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
