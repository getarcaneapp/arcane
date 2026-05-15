package scheduler

import (
	"context"
	"log/slog"
	"time"

	"github.com/getarcaneapp/arcane/backend/internal/services"
)

const ExpiredSessionsCleanupJobName = "expired-sessions-cleanup"

type ExpiredSessionsCleanupJob struct {
	sessionService  *services.SessionService
	settingsService *services.SettingsService
}

func NewExpiredSessionsCleanupJob(sessionService *services.SessionService, settingsService *services.SettingsService) *ExpiredSessionsCleanupJob {
	return &ExpiredSessionsCleanupJob{
		sessionService:  sessionService,
		settingsService: settingsService,
	}
}

func (j *ExpiredSessionsCleanupJob) Name() string {
	return ExpiredSessionsCleanupJobName
}

func (j *ExpiredSessionsCleanupJob) Schedule(ctx context.Context) string {
	s := j.settingsService.GetStringSetting(ctx, "expiredSessionsCleanupInterval", "0 0 0 * * *")
	if s == "" {
		return "0 0 0 * * *"
	}
	return s
}

func (j *ExpiredSessionsCleanupJob) Run(ctx context.Context) {
	slog.InfoContext(ctx, "Running expired sessions cleanup job", "jobName", ExpiredSessionsCleanupJobName)

	revokedRetention := 7 * 24 * time.Hour
	deleted, err := j.sessionService.DeleteExpiredSessions(ctx, revokedRetention)
	if err != nil {
		slog.ErrorContext(ctx, "Failed to delete expired user sessions", "jobName", ExpiredSessionsCleanupJobName, "revokedRetention", revokedRetention.String(), "error", err)
		return
	}

	slog.InfoContext(ctx, "Expired sessions cleanup job completed successfully",
		"jobName", ExpiredSessionsCleanupJobName,
		"revokedRetention", revokedRetention.String(),
		"deleted", deleted)
}

func (j *ExpiredSessionsCleanupJob) Reschedule(ctx context.Context) error {
	slog.InfoContext(ctx, "rescheduling expired sessions cleanup job in new scheduler; currently requires restart")
	return nil
}
