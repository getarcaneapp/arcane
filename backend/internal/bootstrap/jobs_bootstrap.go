package bootstrap

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/getarcaneapp/arcane/backend/internal/config"
	"github.com/getarcaneapp/arcane/backend/internal/job"
)

func initializeScheduler() (*job.Scheduler, error) {
	scheduler, err := job.NewScheduler()
	if err != nil {
		return nil, fmt.Errorf("failed to create job scheduler: %w", err)
	}
	return scheduler, nil
}

func registerJobs(appCtx context.Context, scheduler *job.Scheduler, appServices *Services, appConfig *config.Config) {
	autoUpdateJob := job.NewAutoUpdateJob(scheduler, appServices.Updater, appServices.Settings)
	if err := autoUpdateJob.Register(appCtx); err != nil {
		slog.ErrorContext(appCtx, "Failed to register auto-update job", "error", err)
	}

	imagePollingJob := job.NewImagePollingJob(scheduler, appServices.ImageUpdate, appServices.Settings, appServices.Environment)
	if err := imagePollingJob.Register(appCtx); err != nil {
		slog.ErrorContext(appCtx, "Failed to register image polling job", "error", err)
	}

	environmentHealthJob := job.NewEnvironmentHealthJob(scheduler, appServices.Environment, appServices.Settings)
	if !appConfig.AgentMode {
		if err := environmentHealthJob.Register(appCtx); err != nil {
			slog.ErrorContext(appCtx, "Failed to register environment health check job", "error", err)
		}
	}

	analyticsJob := job.NewAnalyticsJob(scheduler, appServices.Settings, nil, appConfig)
	if err := analyticsJob.Register(appCtx); err != nil {
		slog.ErrorContext(appCtx, "Failed to register analytics heartbeat job", "error", err)
	}

	eventCleanupJob := job.NewEventCleanupJob(scheduler, appServices.Event, appServices.Settings)
	if err := eventCleanupJob.Register(appCtx); err != nil {
		slog.ErrorContext(appCtx, "Failed to register event cleanup job", "error", err)
	}

	fsWatcherJob, err := job.RegisterFilesystemWatcherJob(appCtx, scheduler, appServices.Project, appServices.Template, appServices.Settings)
	if err != nil {
		slog.ErrorContext(appCtx, "Failed to register filesystem watcher job", "error", err)
	}

	gitOpsSyncJob := job.NewGitOpsSyncJob(scheduler, appServices.GitOpsSync, appServices.Settings)
	if err := gitOpsSyncJob.Register(appCtx); err != nil {
		slog.ErrorContext(appCtx, "Failed to register GitOps sync job", slog.Any("error", err))
	}

	setupJobScheduleCallbacks(appServices, appConfig, environmentHealthJob, analyticsJob, eventCleanupJob)
	setupSettingsCallbacks(appServices, appConfig, imagePollingJob, autoUpdateJob, environmentHealthJob, fsWatcherJob)
}

func setupJobScheduleCallbacks(appServices *Services, appConfig *config.Config, environmentHealthJob *job.EnvironmentHealthJob, analyticsJob *job.AnalyticsJob, eventCleanupJob *job.EventCleanupJob) {
	if appServices.JobSchedule != nil {
		appServices.JobSchedule.OnJobSchedulesChanged = func(ctx context.Context) {
			if !appConfig.AgentMode {
				if err := environmentHealthJob.Reschedule(ctx); err != nil {
					slog.WarnContext(ctx, "Failed to reschedule environment-health job", "error", err)
				}
			}
			if err := analyticsJob.Reschedule(ctx); err != nil {
				slog.WarnContext(ctx, "Failed to reschedule analytics heartbeat job", "error", err)
			}
			if err := eventCleanupJob.Reschedule(ctx); err != nil {
				slog.WarnContext(ctx, "Failed to reschedule event cleanup job", "error", err)
			}
		}
	}
}

func setupSettingsCallbacks(appServices *Services, appConfig *config.Config, imagePollingJob *job.ImagePollingJob, autoUpdateJob *job.AutoUpdateJob, environmentHealthJob *job.EnvironmentHealthJob, fsWatcherJob *job.FilesystemWatcherJob) {
	appServices.Settings.OnImagePollingSettingsChanged = func(ctx context.Context) {
		if err := imagePollingJob.Reschedule(ctx); err != nil {
			slog.WarnContext(ctx, "Failed to reschedule image-polling job", "error", err)
		}
		if err := autoUpdateJob.Reschedule(ctx); err != nil {
			slog.WarnContext(ctx, "Failed to reschedule auto-update job", "error", err)
		}
		if !appConfig.AgentMode {
			if err := environmentHealthJob.Reschedule(ctx); err != nil {
				slog.WarnContext(ctx, "Failed to reschedule environment-health job", "error", err)
			}
		}
	}
	appServices.Settings.OnAutoUpdateSettingsChanged = func(ctx context.Context) {
		if err := autoUpdateJob.Reschedule(ctx); err != nil {
			slog.WarnContext(ctx, "Failed to reschedule auto-update job", "error", err)
		}
	}
	appServices.Settings.OnProjectsDirectoryChanged = func(ctx context.Context) {
		if fsWatcherJob != nil {
			if err := fsWatcherJob.RestartProjectsWatcher(ctx); err != nil {
				slog.WarnContext(ctx, "Failed to restart projects filesystem watcher", "error", err)
			}
		}
	}
}
