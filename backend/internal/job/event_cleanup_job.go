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

func RegisterEventCleanupJob(
	ctx context.Context,
	scheduler *Scheduler,
	eventService *services.EventService,
) error {
	slog.InfoContext(ctx, "Registering event cleanup job", "jobName", EventCleanupJobName)

	taskFunc := func(jobCtx context.Context) error {
		slog.InfoContext(jobCtx, "Running event cleanup job", "jobName", EventCleanupJobName)

		// Delete events older than 36 hours
		olderThan := 36 * time.Hour
		if err := eventService.DeleteOldEvents(jobCtx, olderThan); err != nil {
			slog.ErrorContext(jobCtx, "Failed to delete old events", "jobName", EventCleanupJobName, "olderThan", olderThan.String(), "error", err)
			return err
		}

		slog.InfoContext(jobCtx, "Event cleanup job completed successfully",
			"jobName", EventCleanupJobName,
			"olderThan", olderThan.String())
		return nil
	}

	// Run every 6 hours to keep the cleanup regular but not too frequent
	jobDefinition := gocron.DurationJob(6 * time.Hour)

	err := scheduler.RegisterJob(
		ctx,
		EventCleanupJobName,
		jobDefinition,
		taskFunc,
		false, // Don't run immediately on startup
	)

	if err != nil {
		return fmt.Errorf("failed to register event cleanup job %q: %w", EventCleanupJobName, err)
	}

	slog.InfoContext(ctx, "Event cleanup job registered successfully",
		"jobName", EventCleanupJobName,
		"interval", "6h",
		"retention", "36h")
	return nil
}
