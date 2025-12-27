package job

import (
	"context"
	"log/slog"
	"time"

	"github.com/getarcaneapp/arcane/backend/internal/services"
	"github.com/go-co-op/gocron/v2"
)

type GlobalVariablesSyncJob struct {
	templateService    *services.TemplateService
	environmentService *services.EnvironmentService
	settingsService    *services.SettingsService
	scheduler          *Scheduler
}

func NewGlobalVariablesSyncJob(scheduler *Scheduler, templateService *services.TemplateService, environmentService *services.EnvironmentService, settingsService *services.SettingsService) *GlobalVariablesSyncJob {
	return &GlobalVariablesSyncJob{
		templateService:    templateService,
		environmentService: environmentService,
		settingsService:    settingsService,
		scheduler:          scheduler,
	}
}

func (j *GlobalVariablesSyncJob) Register(ctx context.Context) error {
	syncInterval := j.settingsService.GetIntSetting(ctx, "globalVariablesSyncInterval", 5)
	interval := time.Duration(syncInterval) * time.Minute

	// Ensure minimum interval of 1 minute
	if interval < 1*time.Minute {
		slog.WarnContext(ctx, "global variables sync interval too low; using minimum", "requested_minutes", syncInterval, "effective_interval", "1m")
		interval = 1 * time.Minute
	}

	slog.InfoContext(ctx, "registering global variables sync job", "interval", interval.String())

	j.scheduler.RemoveJobByName("global-variables-sync")

	jobDefinition := gocron.DurationJob(interval)
	return j.scheduler.RegisterJob(
		ctx,
		"global-variables-sync",
		jobDefinition,
		j.Execute,
		true, // Run immediately on startup
	)
}

func (j *GlobalVariablesSyncJob) Reschedule(ctx context.Context) error {
	syncInterval := j.settingsService.GetIntSetting(ctx, "globalVariablesSyncInterval", 5)
	interval := time.Duration(syncInterval) * time.Minute

	if interval < 1*time.Minute {
		interval = 1 * time.Minute
	}

	slog.InfoContext(ctx, "global variables sync settings changed; rescheduling", "interval", interval.String())

	return j.scheduler.RescheduleDurationJobByName(ctx, "global-variables-sync", interval, j.Execute, false)
}

func (j *GlobalVariablesSyncJob) Execute(ctx context.Context) error {
	slog.InfoContext(ctx, "global variables sync started")

	vars, err := j.templateService.GetGlobalVariables(ctx)
	if err != nil {
		slog.ErrorContext(ctx, "failed to get global variables for sync", "error", err)
		return err
	}

	if err := j.environmentService.SyncGlobalVariablesToAllAgents(ctx, vars); err != nil {
		slog.ErrorContext(ctx, "failed to sync global variables to agents", "error", err)
		return err
	}

	slog.InfoContext(ctx, "global variables sync completed")
	return nil
}
