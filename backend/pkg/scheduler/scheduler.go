package scheduler

import (
	"context"
	"log/slog"
	"time"

	schedulertypes "github.com/getarcaneapp/arcane/types/scheduler"
	"github.com/robfig/cron/v3"
)

type JobScheduler struct {
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

func (js *JobScheduler) RegisterJob(job schedulertypes.Job) {
	js.jobs = append(js.jobs, job)
	js.jobsByID[job.Name()] = job
}

func (js *JobScheduler) GetJob(jobID string) (schedulertypes.Job, bool) {
	job, ok := js.jobsByID[jobID]
	return job, ok
}

func (js *JobScheduler) StartScheduler() {
	for _, job := range js.jobs {
		if err := js.scheduleJobInternal(js.context, job); err != nil {
			slog.ErrorContext(js.context, "Failed to schedule job", "name", job.Name(), "error", err)
		}
	}
	js.cron.Start()
}

func (js *JobScheduler) RescheduleJob(ctx context.Context, job schedulertypes.Job) error {
	if entryID, ok := js.entryIDs[job.Name()]; ok {
		js.cron.Remove(entryID)
		delete(js.entryIDs, job.Name())
	}

	if err := js.scheduleJobInternal(ctx, job); err != nil {
		return err
	}

	slog.DebugContext(ctx, "Job rescheduled", "name", job.Name(), "scheduled", js.isJobScheduledInternal(job.Name()), "contextCanceled", ctx.Err() != nil)
	return nil
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

func (js *JobScheduler) scheduleJobInternal(ctx context.Context, job schedulertypes.Job) error {
	if conditionalJob, ok := job.(schedulertypes.ConditionalJob); ok && !conditionalJob.ShouldSchedule(ctx) {
		slog.DebugContext(ctx, "Job disabled; not scheduling", "name", job.Name())
		return nil
	}

	schedule := job.Schedule(ctx)
	slog.InfoContext(ctx, "Starting Job", "name", job.Name(), "schedule", schedule)

	entryID, err := js.cron.AddFunc(schedule, func() {
		slog.InfoContext(ctx, "Job starting", "name", job.Name(), "schedule", schedule)
		job.Run(ctx)
		slog.InfoContext(ctx, "Job finished", "name", job.Name())
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
