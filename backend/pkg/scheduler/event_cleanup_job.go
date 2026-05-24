package scheduler

import (
	"context"
	"log/slog"
	"time"

	"github.com/getarcaneapp/arcane/backend/internal/services"
)

const EventCleanupJobName = "event-cleanup"

type EventCleanupJob struct {
	eventService    *services.EventService
	settingsService *services.SettingsService
}

func NewEventCleanupJob(eventService *services.EventService, settingsService *services.SettingsService) *EventCleanupJob {
	return &EventCleanupJob{
		eventService:    eventService,
		settingsService: settingsService,
	}
}

func (j *EventCleanupJob) Name() string {
	return EventCleanupJobName
}

func (j *EventCleanupJob) Schedule(ctx context.Context) string {
	s := j.settingsService.GetStringSetting(ctx, "eventCleanupInterval", "0 0 */6 * * *")
	if s == "" {
		return "0 0 */6 * * *"
	}
	return s
}

func (j *EventCleanupJob) Run(ctx context.Context) {
	slog.InfoContext(ctx, "Running event cleanup job", "jobName", EventCleanupJobName)

	// Delete events older than 36 hours
	olderThan := 36 * time.Hour
	if err := j.eventService.DeleteOldEvents(ctx, olderThan); err != nil {
		slog.ErrorContext(ctx, "Failed to delete old events", "jobName", EventCleanupJobName, "olderThan", olderThan.String(), "error", err)
		return
	}

	slog.InfoContext(ctx, "Event cleanup job completed successfully",
		"jobName", EventCleanupJobName,
		"olderThan", olderThan.String())
}

func (j *EventCleanupJob) Reschedule(ctx context.Context) error {
	slog.InfoContext(ctx, "rescheduling event cleanup job in new scheduler; currently requires restart")
	return nil
}
