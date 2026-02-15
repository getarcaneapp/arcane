package bootstrap

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"

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
	// Send initial heartbeat on startup without blocking bootstrap.
	go analyticsJob.Run(appCtx)

	eventCleanupJob := pkg_scheduler.NewEventCleanupJob(appServices.Event, appServices.Settings)
	newScheduler.RegisterJob(eventCleanupJob)

	scheduledPruneJob := pkg_scheduler.NewScheduledPruneJob(appServices.System, appServices.Settings, appServices.Notification)
	newScheduler.RegisterJob(scheduledPruneJob)

	fsWatcherJob, err := pkg_scheduler.RegisterFilesystemWatcherJob(appCtx, appServices.Project, appServices.Template, appServices.Settings)
	if err != nil {
		slog.ErrorContext(appCtx, "Failed to register filesystem watcher job", "error", err)
	}

	gitOpsSyncJob := pkg_scheduler.NewGitOpsSyncJob(appServices.GitOpsSync, appServices.Settings)
	newScheduler.RegisterJob(gitOpsSyncJob)

	vulnerabilityScanJob := pkg_scheduler.NewVulnerabilityScanJob(appServices.Vulnerability, appServices.Settings)
	newScheduler.RegisterJob(vulnerabilityScanJob)

	setupJobScheduleCallbacks(
		appCtx,
		appServices,
		appConfig,
		newScheduler,
		imagePollingJob,
		autoUpdateJob,
		environmentHealthJob,
		analyticsJob,
		eventCleanupJob,
		scheduledPruneJob,
		gitOpsSyncJob,
		vulnerabilityScanJob,
	)
	setupSettingsCallbacks(appCtx, appServices, appConfig, newScheduler, imagePollingJob, autoUpdateJob, environmentHealthJob, fsWatcherJob, scheduledPruneJob, vulnerabilityScanJob)
}

func setupJobScheduleCallbacks(
	lifecycleCtx context.Context,
	appServices *Services,
	appConfig *config.Config,
	newScheduler *pkg_scheduler.JobScheduler,
	imagePollingJob *pkg_scheduler.ImagePollingJob,
	autoUpdateJob *pkg_scheduler.AutoUpdateJob,
	environmentHealthJob *pkg_scheduler.EnvironmentHealthJob,
	analyticsJob *pkg_scheduler.AnalyticsJob,
	eventCleanupJob *pkg_scheduler.EventCleanupJob,
	scheduledPruneJob *pkg_scheduler.ScheduledPruneJob,
	gitOpsSyncJob *pkg_scheduler.GitOpsSyncJob,
	vulnerabilityScanJob *pkg_scheduler.VulnerabilityScanJob,
) {
	if appServices.JobSchedule == nil {
		return
	}

	appServices.JobSchedule.OnJobSchedulesChanged = func(ctx context.Context, changedKeys []string) {
		for _, key := range changedKeys {
			slog.DebugContext(lifecycleCtx, "Processing job schedule change", "key", key, "triggerContextCanceled", ctx.Err() != nil)
			handleJobScheduleChangeInternal(
				lifecycleCtx,
				key,
				appConfig,
				newScheduler,
				imagePollingJob,
				autoUpdateJob,
				environmentHealthJob,
				analyticsJob,
				eventCleanupJob,
				scheduledPruneJob,
				gitOpsSyncJob,
				vulnerabilityScanJob,
			)
		}
	}
}

func handleJobScheduleChangeInternal(
	ctx context.Context,
	key string,
	appConfig *config.Config,
	newScheduler *pkg_scheduler.JobScheduler,
	imagePollingJob *pkg_scheduler.ImagePollingJob,
	autoUpdateJob *pkg_scheduler.AutoUpdateJob,
	environmentHealthJob *pkg_scheduler.EnvironmentHealthJob,
	analyticsJob *pkg_scheduler.AnalyticsJob,
	eventCleanupJob *pkg_scheduler.EventCleanupJob,
	scheduledPruneJob *pkg_scheduler.ScheduledPruneJob,
	gitOpsSyncJob *pkg_scheduler.GitOpsSyncJob,
	vulnerabilityScanJob *pkg_scheduler.VulnerabilityScanJob,
) {
	switch key {
	case "pollingInterval":
		if err := newScheduler.RescheduleJob(ctx, imagePollingJob); err != nil {
			slog.WarnContext(ctx, "Failed to reschedule image-polling job", "error", err)
		}
	case "autoUpdateInterval":
		if err := newScheduler.RescheduleJob(ctx, autoUpdateJob); err != nil {
			slog.WarnContext(ctx, "Failed to reschedule auto-update job", "error", err)
		}
	case "environmentHealthInterval":
		if appConfig.AgentMode {
			return
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
	case "scheduledPruneInterval":
		if err := newScheduler.RescheduleJob(ctx, scheduledPruneJob); err != nil {
			slog.WarnContext(ctx, "Failed to reschedule scheduled-prune job", "error", err)
		}
	case "gitopsSyncInterval":
		if err := newScheduler.RescheduleJob(ctx, gitOpsSyncJob); err != nil {
			slog.WarnContext(ctx, "Failed to reschedule gitops sync job", "error", err)
		}
	case "vulnerabilityScanInterval":
		if err := newScheduler.RescheduleJob(ctx, vulnerabilityScanJob); err != nil {
			slog.WarnContext(ctx, "Failed to reschedule vulnerability-scan job", "error", err)
		}
	}
}

func setupSettingsCallbacks(lifecycleCtx context.Context, appServices *Services, appConfig *config.Config, newScheduler *pkg_scheduler.JobScheduler, imagePollingJob *pkg_scheduler.ImagePollingJob, autoUpdateJob *pkg_scheduler.AutoUpdateJob, environmentHealthJob *pkg_scheduler.EnvironmentHealthJob, fsWatcherJob *pkg_scheduler.FilesystemWatcherJob, scheduledPruneJob *pkg_scheduler.ScheduledPruneJob, vulnerabilityScanJob *pkg_scheduler.VulnerabilityScanJob) {
	appServices.Settings.OnImagePollingSettingsChanged = func(_ context.Context) {
		if err := newScheduler.RescheduleJob(lifecycleCtx, imagePollingJob); err != nil {
			slog.WarnContext(lifecycleCtx, "Failed to reschedule image-polling job", "error", err)
		}
		if err := newScheduler.RescheduleJob(lifecycleCtx, autoUpdateJob); err != nil {
			slog.WarnContext(lifecycleCtx, "Failed to reschedule auto-update job", "error", err)
		}
		if !appConfig.AgentMode {
			if err := newScheduler.RescheduleJob(lifecycleCtx, environmentHealthJob); err != nil {
				slog.WarnContext(lifecycleCtx, "Failed to reschedule environment-health job", "error", err)
			}
		}
	}
	appServices.Settings.OnAutoUpdateSettingsChanged = func(ctx context.Context) {
		slog.DebugContext(lifecycleCtx, "AutoUpdateSettingsChanged callback triggered", "triggerContextCanceled", ctx.Err() != nil)
		if err := newScheduler.RescheduleJob(lifecycleCtx, autoUpdateJob); err != nil {
			slog.WarnContext(lifecycleCtx, "Failed to reschedule auto-update job", "error", err)
		}
	}
	appServices.Settings.OnProjectsDirectoryChanged = func(_ context.Context) {
		if fsWatcherJob != nil {
			if err := fsWatcherJob.RestartProjectsWatcher(lifecycleCtx); err != nil {
				slog.WarnContext(lifecycleCtx, "Failed to restart projects filesystem watcher", "error", err)
			}
		}
	}
	appServices.Settings.OnScheduledPruneSettingsChanged = func(_ context.Context) {
		if err := newScheduler.RescheduleJob(lifecycleCtx, scheduledPruneJob); err != nil {
			slog.WarnContext(lifecycleCtx, "Failed to reschedule scheduled-prune job", "error", err)
		}
	}
	appServices.Settings.OnVulnerabilityScanSettingsChanged = func(_ context.Context) {
		if err := newScheduler.RescheduleJob(lifecycleCtx, vulnerabilityScanJob); err != nil {
			slog.WarnContext(lifecycleCtx, "Failed to reschedule vulnerability-scan job", "error", err)
		}
	}

	// Only set up timeout sync callback on main instance (not in agent mode)
	if !appConfig.AgentMode {
		appServices.Settings.OnTimeoutSettingsChanged = func(ctx context.Context, timeoutSettings map[string]string) {
			go syncTimeoutSettingsToAgentsInternal(context.WithoutCancel(ctx), appServices, timeoutSettings)
		}
	}
}

// syncTimeoutSettingsToAgentsInternal syncs timeout settings to all connected remote environments
func syncTimeoutSettingsToAgentsInternal(ctx context.Context, appServices *Services, timeoutSettings map[string]string) {
	envs, err := appServices.Environment.ListRemoteEnvironments(ctx)
	if err != nil {
		slog.WarnContext(ctx, "Failed to list remote environments for timeout sync", "error", err)
		return
	}

	if len(envs) == 0 {
		return
	}

	// Build the settings update payload
	body, err := json.Marshal(timeoutSettings)
	if err != nil {
		slog.WarnContext(ctx, "Failed to marshal timeout settings for sync", "error", err)
		return
	}

	slog.InfoContext(ctx, "Syncing timeout settings to remote environments", "count", len(envs), "settings", timeoutSettings)

	for _, env := range envs {
		_, statusCode, err := appServices.Environment.ProxyRequest(ctx, env.ID, http.MethodPut, "/api/environments/0/settings", body)
		if err != nil {
			slog.WarnContext(ctx, "Failed to sync timeout settings to environment", "environmentID", env.ID, "environmentName", env.Name, "error", err)
			continue
		}
		if statusCode != http.StatusOK {
			slog.WarnContext(ctx, "Environment returned non-OK status for timeout sync", "environmentID", env.ID, "environmentName", env.Name, "statusCode", statusCode)
			continue
		}
		slog.DebugContext(ctx, "Successfully synced timeout settings to environment", "environmentID", env.ID, "environmentName", env.Name)
	}
}
