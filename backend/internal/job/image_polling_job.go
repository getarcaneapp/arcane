package job

import (
	"context"
	"log/slog"
	"time"

	"github.com/getarcaneapp/arcane/backend/internal/services"
	"github.com/getarcaneapp/arcane/types/containerregistry"
	"github.com/go-co-op/gocron/v2"
)

type ImagePollingJob struct {
	imageUpdateService *services.ImageUpdateService
	settingsService    *services.SettingsService
	environmentService *services.EnvironmentService
	scheduler          *Scheduler
}

func NewImagePollingJob(scheduler *Scheduler, imageUpdateService *services.ImageUpdateService, settingsService *services.SettingsService, environmentService *services.EnvironmentService) *ImagePollingJob {
	return &ImagePollingJob{
		imageUpdateService: imageUpdateService,
		settingsService:    settingsService,
		environmentService: environmentService,
		scheduler:          scheduler,
	}
}

func (j *ImagePollingJob) Register(ctx context.Context) error {
	pollingEnabled := j.settingsService.GetBoolSetting(ctx, "pollingEnabled", true)
	pollingInterval := j.settingsService.GetIntSetting(ctx, "pollingInterval", 60)

	if !pollingEnabled {
		slog.InfoContext(ctx, "polling disabled; job not registered")
		return nil
	}

	interval := time.Duration(pollingInterval) * time.Minute
	if interval < 5*time.Minute {
		slog.WarnContext(ctx, "polling interval too low; using default", "requested_minutes", pollingInterval, "effective_interval", "60m")
		interval = 60 * time.Minute
	}

	slog.InfoContext(ctx, "registering image polling job", "interval", interval.String())

	j.scheduler.RemoveJobByName("image-polling")

	jobDefinition := gocron.DurationJob(interval)
	return j.scheduler.RegisterJob(
		ctx,
		"image-polling",
		jobDefinition,
		j.Execute,
		false,
	)
}

func (j *ImagePollingJob) Execute(ctx context.Context) error {
	slog.InfoContext(ctx, "image scan run started")

	creds, err := j.loadRegistryCredentials(ctx)
	if err != nil {
		slog.WarnContext(ctx, "failed to load registry credentials for polling", "error", err.Error())
		creds = nil
	}

	results, err := j.imageUpdateService.CheckAllImages(ctx, 0, creds)
	if err != nil {
		slog.ErrorContext(ctx, "image scan failed", "err", err)
		return err
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

	return nil
}

func (j *ImagePollingJob) loadRegistryCredentials(ctx context.Context) ([]containerregistry.Credential, error) {
	return j.environmentService.GetEnabledRegistryCredentials(ctx)
}

func (j *ImagePollingJob) Reschedule(ctx context.Context) error {
	pollingEnabled := j.settingsService.GetBoolSetting(ctx, "pollingEnabled", true)
	pollingInterval := j.settingsService.GetIntSetting(ctx, "pollingInterval", 60)

	if !pollingEnabled {
		j.scheduler.RemoveJobByName("image-polling")
		slog.InfoContext(ctx, "polling disabled; removed image-polling job if present")
		return nil
	}

	interval := time.Duration(pollingInterval) * time.Minute
	if interval < 5*time.Minute {
		interval = 60 * time.Minute
	}
	slog.InfoContext(ctx, "polling settings changed; rescheduling", "interval", interval.String())

	return j.scheduler.RescheduleDurationJobByName(ctx, "image-polling", interval, j.Execute, false)
}
