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

// BusWatcher is a continuous event consumer owned by the application scheduler lifecycle.
type BusWatcher interface {
	Name() string
	Start(ctx context.Context) error
	RunNow(ctx context.Context) error
}
