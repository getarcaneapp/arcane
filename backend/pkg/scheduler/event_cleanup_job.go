package scheduler

import (
	"context"
	"log/slog"
	"time"

	schedulertypes "github.com/getarcaneapp/arcane/types/v2/scheduler"

	"github.com/getarcaneapp/arcane/backend/v2/internal/activity"
	"github.com/getarcaneapp/arcane/backend/v2/internal/event"
	"github.com/getarcaneapp/arcane/backend/v2/internal/settings"
)

const EventCleanupJobName = "event-cleanup"

type EventCleanupJob struct {
	eventService    *event.EventService
	activityService *activity.ActivityService
	settingsService *settings.SettingsService
}

func NewEventCleanupJob(eventService *event.EventService, activityService *activity.ActivityService, settingsService *settings.SettingsService) *EventCleanupJob {
	return &EventCleanupJob{
		eventService:    eventService,
		activityService: activityService,
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

func (j *EventCleanupJob) Run(ctx context.Context) (schedulertypes.Outcome, error) {
	slog.InfoContext(ctx, "Running event cleanup job", "jobName", EventCleanupJobName)

	// Delete events older than 36 hours
	olderThan := 36 * time.Hour
	if err := j.eventService.DeleteOldEvents(ctx, olderThan); err != nil {
		slog.ErrorContext(ctx, "Failed to delete old events", "jobName", EventCleanupJobName, "olderThan", olderThan.String(), "error", err)
		return schedulertypes.Outcome{}, err
	}

	slog.InfoContext(ctx, "Event cleanup job completed successfully",
		"jobName", EventCleanupJobName,
		"olderThan", olderThan.String())

	if j.activityService != nil {
		retentionDays := j.settingsService.GetIntSetting(ctx, "activityHistoryRetentionDays", 30)
		maxEntries := j.settingsService.GetIntSetting(ctx, "activityHistoryMaxEntries", 1000)
		deleted, err := j.activityService.PruneHistory(ctx, retentionDays, maxEntries)
		if err != nil {
			slog.ErrorContext(ctx, "Failed to prune activity history", "jobName", EventCleanupJobName, "error", err)
			return schedulertypes.Outcome{}, err
		}

		slog.InfoContext(ctx, "Activity history cleanup completed successfully",
			"jobName", EventCleanupJobName,
			"retentionDays", retentionDays,
			"maxEntries", maxEntries,
			"deleted", deleted)
	}
	return schedulertypes.Outcome{Status: schedulertypes.Succeeded}, nil
}

func (j *EventCleanupJob) Reschedule(ctx context.Context) error {
	slog.InfoContext(ctx, "rescheduling event cleanup job in new scheduler; currently requires restart")
	return nil
}
