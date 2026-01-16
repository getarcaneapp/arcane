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
		schedule := currentJob.Schedule()
		if _, err := js.cron.AddFunc(schedule, func() {
			ctx := js.context
			if ctx == nil {
				ctx = context.Background()
			}
			slog.InfoContext(ctx, "Job starting", "name", currentJob.Name())
			currentJob.Run(ctx)
			slog.InfoContext(ctx, "Job finished", "name", currentJob.Name())
		}); err != nil {
			ctx := js.context
			if ctx == nil {
				ctx = context.Background()
			}
			slog.ErrorContext(ctx, "Failed to schedule job", "name", currentJob.Name(), "error", err)
		}
	}
	js.cron.Start()
}
