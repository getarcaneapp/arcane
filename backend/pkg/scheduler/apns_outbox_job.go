package scheduler

import (
	"context"

	"github.com/getarcaneapp/arcane/backend/v2/internal/apns"
	"github.com/getarcaneapp/arcane/backend/v2/internal/config"
)

const ApnsOutboxJobName = "apns-outbox"

type ApnsOutboxJob struct {
	service *apns.ApnsService
	config  *config.Config
}

func NewApnsOutboxJob(service *apns.ApnsService, cfg *config.Config) *ApnsOutboxJob {
	return &ApnsOutboxJob{service: service, config: cfg}
}

func (j *ApnsOutboxJob) Name() string {
	return ApnsOutboxJobName
}

func (j *ApnsOutboxJob) Schedule(_ context.Context) string {
	return "0 */5 * * * *"
}

func (j *ApnsOutboxJob) ShouldSchedule(ctx context.Context) bool {
	return (j.config == nil || !j.config.AgentMode) && j.service.Enabled(ctx)
}

func (j *ApnsOutboxJob) Run(ctx context.Context) {
	j.service.DrainOutbox(ctx)
}
