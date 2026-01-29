package bootstrap

import (
	"context"
	"log/slog"

	"github.com/getarcaneapp/arcane/backend/internal/config"
	pkg_scheduler "github.com/getarcaneapp/arcane/backend/pkg/scheduler"
)

func registerJobs(appCtx context.Context, newScheduler *pkg_scheduler.JobScheduler, appServices *Services, appConfig *config.Config) {
	autoUpdateJob := pkg_scheduler.NewAutoUpdateJob(appServices.Updater, appServices.Settings)
	newScheduler.RegisterJob(autoUpdateJob)

	imagePollingJob := pkg_scheduler.NewImagePollingJob(appServices.ImageUpdate, appServices.Settings, appServices.Environment)
	newScheduler.RegisterJob(imagePollingJob)

	environmentHealthJob := pkg_scheduler.NewEnvironmentHealthJob(appServices.Environment, appServices.Settings)
	if !appConfig.AgentMode {
		newScheduler.RegisterJob(environmentHealthJob)
	}

	analyticsJob := pkg_scheduler.NewAnalyticsJob(appServices.Settings, nil, appConfig)
	newScheduler.RegisterJob(analyticsJob)

	eventCleanupJob := pkg_scheduler.NewEventCleanupJob(appServices.Event, appServices.Settings)
	newScheduler.RegisterJob(eventCleanupJob)

	scheduledPruneJob := pkg_scheduler.NewScheduledPruneJob(appServices.System, appServices.Settings)
	newScheduler.RegisterJob(scheduledPruneJob)

	fsWatcherJob, err := pkg_scheduler.RegisterFilesystemWatcherJob(appCtx, appServices.Project, appServices.Template, appServices.Settings)
	if err != nil {
		slog.ErrorContext(appCtx, "Failed to register filesystem watcher job", "error", err)
	}

	gitOpsSyncJob := pkg_scheduler.NewGitOpsSyncJob(appServices.GitOpsSync, appServices.Settings)
	newScheduler.RegisterJob(gitOpsSyncJob)

	setupJobScheduleCallbacks(appServices, appConfig, newScheduler, environmentHealthJob, analyticsJob, eventCleanupJob)
	setupSettingsCallbacks(appServices, appConfig, newScheduler, imagePollingJob, autoUpdateJob, environmentHealthJob, fsWatcherJob, scheduledPruneJob)
}

func setupJobScheduleCallbacks(appServices *Services, appConfig *config.Config, newScheduler *pkg_scheduler.JobScheduler, environmentHealthJob *pkg_scheduler.EnvironmentHealthJob, analyticsJob *pkg_scheduler.AnalyticsJob, eventCleanupJob *pkg_scheduler.EventCleanupJob) {
	if appServices.JobSchedule != nil {
		appServices.JobSchedule.OnJobSchedulesChanged = func(ctx context.Context, changedKeys []string) {
			for _, key := range changedKeys {
				switch key {
				case "environmentHealthInterval":
					if appConfig.AgentMode {
						continue
					}
					if err := newScheduler.RescheduleJob(ctx, environmentHealthJob); err != nil {
						slog.WarnContext(ctx, "Failed to reschedule environment-health job", "error", err)
					}
				case "analyticsHeartbeatInterval":
					if err := newScheduler.RescheduleJob(ctx, analyticsJob); err != nil {
						slog.WarnContext(ctx, "Failed to reschedule analytics heartbeat job", "error", err)
					}
				case "eventCleanupInterval":
					if err := newScheduler.RescheduleJob(ctx, eventCleanupJob); err != nil {
						slog.WarnContext(ctx, "Failed to reschedule event cleanup job", "error", err)
					}
				}
			}
		}
	}
}

func setupSettingsCallbacks(appServices *Services, appConfig *config.Config, newScheduler *pkg_scheduler.JobScheduler, imagePollingJob *pkg_scheduler.ImagePollingJob, autoUpdateJob *pkg_scheduler.AutoUpdateJob, environmentHealthJob *pkg_scheduler.EnvironmentHealthJob, fsWatcherJob *pkg_scheduler.FilesystemWatcherJob, scheduledPruneJob *pkg_scheduler.ScheduledPruneJob) {
	appServices.Settings.OnImagePollingSettingsChanged = func(ctx context.Context) {
		if err := newScheduler.RescheduleJob(ctx, imagePollingJob); err != nil {
			slog.WarnContext(ctx, "Failed to reschedule image-polling job", "error", err)
		}
		if err := newScheduler.RescheduleJob(ctx, autoUpdateJob); err != nil {
			slog.WarnContext(ctx, "Failed to reschedule auto-update job", "error", err)
		}
		if !appConfig.AgentMode {
			if err := newScheduler.RescheduleJob(ctx, environmentHealthJob); err != nil {
				slog.WarnContext(ctx, "Failed to reschedule environment-health job", "error", err)
			}
		}
	}
	appServices.Settings.OnAutoUpdateSettingsChanged = func(ctx context.Context) {
		slog.DebugContext(ctx, "AutoUpdateSettingsChanged callback triggered")
		if err := newScheduler.RescheduleJob(ctx, autoUpdateJob); err != nil {
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
	appServices.Settings.OnScheduledPruneSettingsChanged = func(ctx context.Context) {
		if err := newScheduler.RescheduleJob(ctx, scheduledPruneJob); err != nil {
			slog.WarnContext(ctx, "Failed to reschedule scheduled-prune job", "error", err)
		}
	}
}
