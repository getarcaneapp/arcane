package scheduler

import (
	"context"
	"log/slog"
	"time"

	"getarcane.dev/arcane/v3/internal/upload"
	"getarcane.dev/types/v3"
)

type (
	// UploadSessionsCleanupJob purges upload sessions that have been idle for
	// longer than uploadSessionMaxAge. It runs on managers and agents alike,
	// since sessions live on whichever node serves the environment.
	UploadSessionsCleanupJob struct {
		uploadService *upload.UploadService
	}
)

const (
	// UploadSessionsCleanupJobName identifies the hourly purge of idle chunked
	// upload sessions.
	UploadSessionsCleanupJobName = "upload-sessions-cleanup"
	uploadSessionMaxAge          = 24 * time.Hour
)

// NewUploadSessionsCleanupJob builds the cleanup job for the scheduler.
func NewUploadSessionsCleanupJob(uploadService *upload.UploadService) *UploadSessionsCleanupJob {
	return &UploadSessionsCleanupJob{uploadService: uploadService}
}

func (j *UploadSessionsCleanupJob) Name() string {
	return UploadSessionsCleanupJobName
}

func (j *UploadSessionsCleanupJob) Schedule(ctx context.Context) string {
	return "0 * * * *"
}

func (j *UploadSessionsCleanupJob) Run(ctx context.Context) (types.SchedulerOutcome, error) {
	removed, err := j.uploadService.PurgeExpiredSessions(ctx, uploadSessionMaxAge)
	if err != nil {
		slog.ErrorContext(ctx, "Failed to purge expired upload sessions", "jobName", UploadSessionsCleanupJobName, "removed", removed, "error", err)
		return types.SchedulerOutcome{}, err
	}
	if removed > 0 {
		slog.InfoContext(ctx, "Purged expired upload sessions", "jobName", UploadSessionsCleanupJobName, "removed", removed)
	}
	return types.SchedulerOutcome{Status: types.RunStatusSucceeded}, nil
}

func (j *UploadSessionsCleanupJob) Reschedule(ctx context.Context) error {
	return nil
}
