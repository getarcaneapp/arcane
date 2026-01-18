package scheduler

import (
	"context"
	"fmt"
	"log/slog"
	"strconv"

	"github.com/getarcaneapp/arcane/backend/internal/services"
)

type ImagePollingJob struct {
	imageUpdateService *services.ImageUpdateService
	settingsService    *services.SettingsService
	environmentService *services.EnvironmentService
}

func NewImagePollingJob(imageUpdateService *services.ImageUpdateService, settingsService *services.SettingsService, environmentService *services.EnvironmentService) *ImagePollingJob {
	return &ImagePollingJob{
		imageUpdateService: imageUpdateService,
		settingsService:    settingsService,
		environmentService: environmentService,
	}
}

func (j *ImagePollingJob) Name() string {
	return "image-polling"
}

func (j *ImagePollingJob) Schedule(ctx context.Context) string {
	s := j.settingsService.GetStringSetting(ctx, "pollingInterval", "0 0 * * * *")
	if s == "" {
		return "0 0 * * * *"
	}

	// Handle legacy straight int if it somehow didn't get migrated
	if i, err := strconv.Atoi(s); err == nil {
		if i <= 0 {
			i = 60
		}
		if i%60 == 0 {
			return fmt.Sprintf("0 0 */%d * * *", i/60)
		}
		return fmt.Sprintf("0 */%d * * * *", i)
	}

	return s
}

func (j *ImagePollingJob) Run(ctx context.Context) {
	pollingEnabled := j.settingsService.GetBoolSetting(ctx, "pollingEnabled", true)
	if !pollingEnabled {
		slog.DebugContext(ctx, "polling disabled; skipping image scan")
		return
	}

	slog.InfoContext(ctx, "image scan run started")

	creds, err := j.environmentService.GetEnabledRegistryCredentials(ctx)
	if err != nil {
		slog.WarnContext(ctx, "failed to load registry credentials for polling", "error", err.Error())
		creds = nil
	}

	results, err := j.imageUpdateService.CheckAllImages(ctx, 0, creds)
	if err != nil {
		slog.ErrorContext(ctx, "image scan failed", "err", err)
		return
	}

	total := len(results)
	updates := 0
	errors := 0
	for _, r := range results {
		if r == nil {
			continue
		}
		if r.Error != "" {
			errors++
			continue
		}
		if r.HasUpdate {
			updates++
		}
	}

	slog.InfoContext(ctx, "image scan run completed", "checked", total, "updates", updates, "errors", errors)
}

func (j *ImagePollingJob) Reschedule(ctx context.Context) error {
	slog.InfoContext(ctx, "rescheduling image polling job in new scheduler; currently requires restart")
	return nil
}
