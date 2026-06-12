package scheduler

import (
	"context"
	"log/slog"
	"sync"
	"time"

	schedulertypes "github.com/getarcaneapp/arcane/types/v2/scheduler"
	"github.com/robfig/cron/v3"
)

type JobScheduler struct {
	// mu guards jobs, jobsByID and entryIDs. It is held across the cron
	// add/remove calls (which are themselves quick and never block on job
	// execution) but never across job.Run — Run executes on cron's own
	// goroutine, so a running job must not call back into a locking method here.
	mu       sync.Mutex
	cron     *cron.Cron
	jobs     []schedulertypes.Job
	jobsByID map[string]schedulertypes.Job
	entryIDs map[string]cron.EntryID
	context  context.Context
	location *time.Location
}

// NewJobScheduler creates a new job scheduler with the specified timezone location.
// The location is used for interpreting cron expressions.
// If location is nil, UTC is used.
func NewJobScheduler(ctx context.Context, location *time.Location) *JobScheduler {
	if location == nil {
		location = time.UTC
	}
	slog.InfoContext(ctx, "Initializing job scheduler", "timezone", location.String())
	return &JobScheduler{
		cron:     cron.New(cron.WithSeconds(), cron.WithLocation(location)),
		jobs:     []schedulertypes.Job{},
		jobsByID: make(map[string]schedulertypes.Job),
		entryIDs: make(map[string]cron.EntryID),
		context:  ctx,
		location: location,
	}
}

// RegisterJob records a static job to be scheduled when StartScheduler runs. Use
// AddJob for jobs added dynamically at runtime.
func (js *JobScheduler) RegisterJob(job schedulertypes.Job) {
	js.mu.Lock()
	defer js.mu.Unlock()
	js.jobs = append(js.jobs, job)
	js.jobsByID[job.Name()] = job
}

func (js *JobScheduler) GetJob(jobID string) (schedulertypes.Job, bool) {
	js.mu.Lock()
	defer js.mu.Unlock()
	job, ok := js.jobsByID[jobID]
	return job, ok
}

// HasJob reports whether a job with the given name is currently registered.
func (js *JobScheduler) HasJob(jobID string) bool {
	js.mu.Lock()
	defer js.mu.Unlock()
	_, ok := js.jobsByID[jobID]
	return ok
}

func (js *JobScheduler) StartScheduler() {
	js.mu.Lock()
	for _, job := range js.jobs {
		if err := js.scheduleJobInternal(js.context, job); err != nil {
			slog.ErrorContext(js.context, "Failed to schedule job", "name", job.Name(), "error", err)
		}
	}
	js.mu.Unlock()
	js.cron.Start()
}

// AddJob registers and schedules a job at runtime. It is an idempotent upsert: if
// a job with the same name is already scheduled, its existing cron entry is
// removed first (preventing a leaked, forever-firing entry when the schedule
// changes). Safe to call before or after StartScheduler.
func (js *JobScheduler) AddJob(ctx context.Context, job schedulertypes.Job) error {
	js.mu.Lock()
	defer js.mu.Unlock()
	return js.upsertJobInternal(ctx, job)
}

// RemoveJob unschedules and forgets a job by name. It is a no-op (not an error)
// when no job with that name is registered.
func (js *JobScheduler) RemoveJob(ctx context.Context, jobName string) {
	js.mu.Lock()
	defer js.mu.Unlock()

	if entryID, ok := js.entryIDs[jobName]; ok {
		js.cron.Remove(entryID)
		delete(js.entryIDs, jobName)
	}
	delete(js.jobsByID, jobName)
	for i, j := range js.jobs {
		if j.Name() == jobName {
			js.jobs = append(js.jobs[:i], js.jobs[i+1:]...)
			break
		}
	}
	slog.DebugContext(ctx, "Job removed", "name", jobName)
}

func (js *JobScheduler) RescheduleJob(ctx context.Context, job schedulertypes.Job) error {
	js.mu.Lock()
	defer js.mu.Unlock()
	return js.upsertJobInternal(ctx, job)
}

// GetLocation returns the timezone location used by the scheduler for cron expressions.
func (js *JobScheduler) GetLocation() *time.Location {
	return js.location
}

func (js *JobScheduler) Run(ctx context.Context) error {
	js.StartScheduler()
	<-ctx.Done()
	js.cron.Stop()
	return nil
}

// upsertJobInternal records the job and (re)schedules it. Callers must hold js.mu.
func (js *JobScheduler) upsertJobInternal(ctx context.Context, job schedulertypes.Job) error {
	jobName := job.Name()
	if entryID, ok := js.entryIDs[jobName]; ok {
		js.cron.Remove(entryID)
		delete(js.entryIDs, jobName)
	}
	delete(js.jobsByID, jobName)

	if err := js.scheduleJobInternal(ctx, job); err != nil {
		return err
	}

	js.jobsByID[jobName] = job

	slog.DebugContext(ctx, "Job scheduled", "name", jobName, "scheduled", js.isJobScheduledInternal(jobName), "contextCanceled", ctx.Err() != nil)
	return nil
}

// scheduleJobInternal adds the job to cron. Callers must hold js.mu. The cron
// closure runs the job with the scheduler's lifecycle context (js.context), never
// the per-call ctx — the per-call ctx may be a request context that is canceled
// once the originating handler returns, which would silently kill future fires.
func (js *JobScheduler) scheduleJobInternal(ctx context.Context, job schedulertypes.Job) error {
	if conditionalJob, ok := job.(schedulertypes.ConditionalJob); ok && !conditionalJob.ShouldSchedule(ctx) {
		slog.DebugContext(ctx, "Job disabled; not scheduling", "name", job.Name())
		return nil
	}

	schedule := job.Schedule(ctx)
	slog.InfoContext(ctx, "Starting Job", "name", job.Name(), "schedule", schedule)

	entryID, err := js.cron.AddFunc(schedule, func() {
		slog.InfoContext(js.context, "Job starting", "name", job.Name(), "schedule", schedule)
		job.Run(js.context)
		slog.InfoContext(js.context, "Job finished", "name", job.Name())
	})
	if err != nil {
		return err
	}

	js.entryIDs[job.Name()] = entryID
	return nil
}

func (js *JobScheduler) isJobScheduledInternal(jobName string) bool {
	_, ok := js.entryIDs[jobName]
	return ok
}
