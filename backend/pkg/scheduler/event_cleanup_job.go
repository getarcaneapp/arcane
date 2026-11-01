package scheduler

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/getarcaneapp/arcane/types/v3"
	"go.uber.org/fx"

	"github.com/getarcaneapp/arcane/backend/v3/internal/activity"
	"github.com/getarcaneapp/arcane/backend/v3/internal/build"
	"github.com/getarcaneapp/arcane/backend/v3/internal/event"
	"github.com/getarcaneapp/arcane/backend/v3/internal/settings"
	"github.com/getarcaneapp/arcane/backend/v3/internal/updater"
	"github.com/getarcaneapp/arcane/backend/v3/pkg/scheduler/schedule"
)

type (
	EventCleanupJob struct {
		eventService    *event.EventService
		activityService *activity.ActivityService
		buildService    *build.BuildService
		settingsService *settings.SettingsService
		updaterService  *updater.UpdaterService
	}

	EventCleanupJobParams struct {
		fx.In

		EventService    *event.EventService
		ActivityService *activity.ActivityService
		BuildService    *build.BuildService
		SettingsService *settings.SettingsService
		UpdaterService  *updater.UpdaterService
	}
)

const (
	EventCleanupJobName = "event-cleanup"
)

func NewEventCleanupJob(p EventCleanupJobParams) *EventCleanupJob {
	return &EventCleanupJob{
		eventService:    p.EventService,
		activityService: p.ActivityService,
		buildService:    p.BuildService,
		settingsService: p.SettingsService,
		updaterService:  p.UpdaterService,
	}
}

func (j *EventCleanupJob) Name() string {
	return EventCleanupJobName
}

func (j *EventCleanupJob) Schedule(ctx context.Context) string {
	return schedule.Or(ctx, j.settingsService.GetStringSetting(ctx, "eventCleanupInterval"), "0 */6 * * *", EventCleanupJobName)
}

func (j *EventCleanupJob) Run(ctx context.Context) (types.SchedulerOutcome, error) {
	slog.InfoContext(ctx, "Running event cleanup job", "jobName", EventCleanupJobName)

	var cleanupErr error
	olderThan := 36 * time.Hour
	if err := j.eventService.DeleteOldEvents(ctx, olderThan); err != nil {
		cleanupErr = fmt.Errorf("delete old events: %w", err)
	} else {
		slog.InfoContext(ctx, "Event cleanup completed successfully",
			"jobName", EventCleanupJobName,
			"olderThan", olderThan.String())
	}

	retentionDays := j.settingsService.GetIntSetting(ctx, "activityHistoryRetentionDays")
	maxEntries := j.settingsService.GetIntSetting(ctx, "activityHistoryMaxEntries")
	if deleted, err := j.activityService.PruneHistory(ctx, retentionDays, maxEntries); err != nil {
		cleanupErr = errors.Join(cleanupErr, fmt.Errorf("prune activity history: %w", err))
	} else {
		slog.InfoContext(ctx, "Activity history cleanup completed successfully",
			"jobName", EventCleanupJobName,
			"retentionDays", retentionDays,
			"maxEntries", maxEntries,
			"deleted", deleted)
	}

	buildRetentionDays := j.settingsService.GetIntSetting(ctx, "buildHistoryRetentionDays")
	if deletedBuilds, err := j.buildService.PruneHistory(ctx, buildRetentionDays); err != nil {
		cleanupErr = errors.Join(cleanupErr, fmt.Errorf("prune image build history: %w", err))
	} else {
		slog.InfoContext(ctx, "Image build history cleanup completed successfully",
			"jobName", EventCleanupJobName,
			"retentionDays", buildRetentionDays,
			"deleted", deletedBuilds)
	}

	updateRetentionDays := j.settingsService.GetIntSetting(ctx, "activityHistoryRetentionDays")
	if deletedUpdates, err := j.updaterService.PruneHistory(ctx, updateRetentionDays); err != nil {
		cleanupErr = errors.Join(cleanupErr, fmt.Errorf("prune auto-update history: %w", err))
	} else {
		slog.InfoContext(ctx, "Auto-update history cleanup completed successfully",
			"jobName", EventCleanupJobName,
			"retentionDays", updateRetentionDays,
			"deleted", deletedUpdates)
	}

	if cleanupErr != nil {
		slog.ErrorContext(ctx, "Event cleanup job failed", "jobName", EventCleanupJobName, "error", cleanupErr)
		return types.SchedulerOutcome{}, cleanupErr
	}
	return types.SchedulerOutcome{Status: types.RunStatusSucceeded}, nil
}

func (j *EventCleanupJob) Reschedule(ctx context.Context) error {
	slog.InfoContext(ctx, "rescheduling event cleanup job in new scheduler; currently requires restart")
	return nil
}
