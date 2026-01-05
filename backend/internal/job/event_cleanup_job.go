package job

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/getarcaneapp/arcane/backend/internal/services"
	"github.com/go-co-op/gocron/v2"
)

const EventCleanupJobName = "EventCleanup"

type EventCleanupJob struct {
	scheduler       *Scheduler
	eventService    *services.EventService
	settingsService *services.SettingsService
}

func NewEventCleanupJob(scheduler *Scheduler, eventService *services.EventService, settingsService *services.SettingsService) *EventCleanupJob {
	return &EventCleanupJob{
		scheduler:       scheduler,
		eventService:    eventService,
		settingsService: settingsService,
	}
}

func (j *EventCleanupJob) Register(ctx context.Context) error {
	interval := j.getInterval(ctx)

	slog.InfoContext(ctx, "Registering event cleanup job", "jobName", EventCleanupJobName, "interval", interval.String(), "retention", "36h")

	// ensure single instance
	j.scheduler.RemoveJobByName(EventCleanupJobName)

	jobDefinition := gocron.DurationJob(interval)
	err := j.scheduler.RegisterJob(
		ctx,
		EventCleanupJobName,
		jobDefinition,
		j.Execute,
		false, // Don't run immediately on startup
	)
	if err != nil {
		return fmt.Errorf("failed to register event cleanup job %q: %w", EventCleanupJobName, err)
	}

	return nil
}

func (j *EventCleanupJob) Reschedule(ctx context.Context) error {
	interval := j.getInterval(ctx)
	slog.InfoContext(ctx, "event cleanup settings changed; rescheduling", "jobName", EventCleanupJobName, "interval", interval.String())
	return j.scheduler.RescheduleDurationJobByName(ctx, EventCleanupJobName, interval, j.Execute, false)
}

func (j *EventCleanupJob) getInterval(ctx context.Context) time.Duration {
	minutes := j.settingsService.GetIntSetting(ctx, "eventCleanupInterval", 360)
	interval := time.Duration(minutes) * time.Minute
	if interval < 5*time.Minute {
		interval = 6 * time.Hour
	}
	return interval
}

func (j *EventCleanupJob) Execute(ctx context.Context) error {
	slog.InfoContext(ctx, "Running event cleanup job", "jobName", EventCleanupJobName)

	// Delete events older than 36 hours
	olderThan := 36 * time.Hour
	if err := j.eventService.DeleteOldEvents(ctx, olderThan); err != nil {
		slog.ErrorContext(ctx, "Failed to delete old events", "jobName", EventCleanupJobName, "olderThan", olderThan.String(), "error", err)
		return err
	}

	slog.InfoContext(ctx, "Event cleanup job completed successfully",
		"jobName", EventCleanupJobName,
		"olderThan", olderThan.String())
	return nil
}
