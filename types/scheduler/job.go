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
	Run(ctx context.Context) (Outcome, error)
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

// StoppableBusWatcher lets a continuous watcher perform explicit shutdown work
// before its actor runner is joined.
type StoppableBusWatcher interface {
	BusWatcher
	Stop(ctx context.Context) error
}

// DynamicScheduler owns jobs registered and removed while the application is running.
type DynamicScheduler interface {
	Submit(ctx context.Context, request Request) (Run, error)
	AddJob(ctx context.Context, job Job) error
	RemoveJob(ctx context.Context, name string)
	HasJob(name string) bool
}

// JobController exposes scheduler operations used by job-management services.
type JobController interface {
	GetJob(jobID string) (Job, bool)
	GetJobRuntimeState(jobID string) (JobRuntimeState, bool)
	RescheduleJob(ctx context.Context, job Job) error
	RunBusWatcherNow(ctx context.Context, watcherID string) error
}

// WorkerController exposes continuous-worker health and recovery operations.
type WorkerController interface {
	RestartWatcher(ctx context.Context, watcherID string) error
	WatcherHealth(watcherID string) (WorkerHealth, bool)
}

// JobScheduler is the shared public contract for the actor-owned scheduler.
// Concrete cron and actor state remains private to the backend implementation.
type JobScheduler interface {
	SetDispatcher(dispatcher Dispatcher)
	ListRegisteredJobs() []Job
	WorkerController
	DynamicScheduler
	JobController

	RegisterJob(job Job) error
	RegisterBusWatcher(watcher BusWatcher, canRunManually bool) error
	StartScheduler() error
	GetLocation() *time.Location
	Stop(ctx context.Context) error
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
	RunFn       func(ctx context.Context) (Outcome, error)
	ReconcileFn func(ctx context.Context, previous Run) (Outcome, error)
	ShouldRunFn func(ctx context.Context) bool
}

func (g *GenericJob) Name() string { return g.JobName }

func (g *GenericJob) Schedule(ctx context.Context) string { return g.ScheduleFn(ctx) }

func (g *GenericJob) Run(ctx context.Context) (Outcome, error) { return g.RunFn(ctx) }

// ShouldSchedule satisfies ConditionalJob. A GenericJob without a ShouldRunFn is
// always scheduled; the scheduler treats a ConditionalJob returning false as "do
// not schedule", so nil must map to true rather than a nil-func panic.
func (g *GenericJob) ShouldSchedule(ctx context.Context) bool {
	if g.ShouldRunFn == nil {
		return true
	}
	return g.ShouldRunFn(ctx)
}

func (g *GenericJob) Reconcile(ctx context.Context, previous Run) (Outcome, error) {
	if g.ReconcileFn != nil {
		return g.ReconcileFn(ctx, previous)
	}
	return Outcome{Status: NeedsAttention, Message: "The interrupted operation has no confirmed result; review its domain records before retrying", Targets: previous.Outcome.Targets}, nil
}
