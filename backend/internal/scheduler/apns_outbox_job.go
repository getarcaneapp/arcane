package scheduler

import (
	"context"

	"getarcane.dev/arcane/v3/internal/apns"
	"getarcane.dev/arcane/v3/internal/config"
	"getarcane.dev/types/v3"
)

type (
	ApnsOutboxJob struct {
		service *apns.ApnsService
		config  *config.Config
	}
)

const (
	ApnsOutboxJobName = "apns-outbox"
)

func NewApnsOutboxJob(service *apns.ApnsService, cfg *config.Config) *ApnsOutboxJob {
	return &ApnsOutboxJob{service: service, config: cfg}
}

func (j *ApnsOutboxJob) Name() string {
	return ApnsOutboxJobName
}

func (j *ApnsOutboxJob) Schedule(_ context.Context) string {
	return "*/5 * * * *"
}

func (j *ApnsOutboxJob) ShouldSchedule(ctx context.Context) bool {
	return (j.config == nil || !j.config.AgentEnabled) && j.service.Enabled(ctx)
}

func (j *ApnsOutboxJob) Run(ctx context.Context) (types.SchedulerOutcome, error) {
	if err := j.service.DrainOutbox(ctx); err != nil {
		return types.SchedulerOutcome{}, err
	}
	return types.SchedulerOutcome{Status: types.RunStatusSucceeded}, nil
}
