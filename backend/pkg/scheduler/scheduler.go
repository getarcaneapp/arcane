package scheduler

import (
	"context"
	"log/slog"

	schedulertypes "github.com/getarcaneapp/arcane/types/scheduler"
	"github.com/robfig/cron/v3"
)

type JobScheduler struct {
	cron    *cron.Cron
	jobs    []schedulertypes.Job
	context context.Context
}

func NewJobScheduler(ctx context.Context) *JobScheduler {
	return &JobScheduler{
		cron:    cron.New(cron.WithSeconds()),
		jobs:    []schedulertypes.Job{},
		context: ctx,
	}
}

func (js *JobScheduler) RegisterJob(job schedulertypes.Job) {
	js.jobs = append(js.jobs, job)
}

func (js *JobScheduler) StartScheduler() {
	for _, job := range js.jobs {
		currentJob := job
		schedule := currentJob.Schedule(js.context)

		slog.InfoContext(js.context, "Starting Job", "name", currentJob.Name(), "schedule", schedule)

		if _, err := js.cron.AddFunc(schedule, func() {
			slog.InfoContext(js.context, "Job starting", "name", currentJob.Name())
			currentJob.Run(js.context)
			slog.InfoContext(js.context, "Job finished", "name", currentJob.Name())
		}); err != nil {
			slog.ErrorContext(js.context, "Failed to schedule job", "name", currentJob.Name(), "error", err)
		}
	}
	js.cron.Start()
}

func (js *JobScheduler) Run(ctx context.Context) error {
	js.StartScheduler()
	<-ctx.Done()
	js.cron.Stop()
	return nil
}
