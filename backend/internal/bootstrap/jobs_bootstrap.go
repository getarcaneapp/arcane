package bootstrap

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"

	"github.com/getarcaneapp/arcane/backend/internal/config"
	"github.com/getarcaneapp/arcane/backend/pkg/libarcane"
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

	dockerClientRefreshJob := pkg_scheduler.NewDockerClientRefreshJob(appServices.Docker, appServices.Settings)
	newScheduler.RegisterJob(dockerClientRefreshJob)

	analyticsJob := pkg_scheduler.NewAnalyticsJob(appServices.Settings, appServices.KV, nil, appConfig)
	newScheduler.RegisterJob(analyticsJob)
	// Send initial heartbeat on startup without blocking bootstrap.
	go analyticsJob.Run(appCtx)

	eventCleanupJob := pkg_scheduler.NewEventCleanupJob(appServices.Event, appServices.Settings)
	newScheduler.RegisterJob(eventCleanupJob)

	pruningVolumeHelperJob := pkg_scheduler.NewPruningVolumeHelperJob(appServices.Volume, appServices.Settings)
	newScheduler.RegisterJob(pruningVolumeHelperJob)

	expiredSessionsCleanupJob := pkg_scheduler.NewExpiredSessionsCleanupJob(appServices.Session, appServices.Settings)
	newScheduler.RegisterJob(expiredSessionsCleanupJob)

	scheduledPruneJob := pkg_scheduler.NewScheduledPruneJob(appServices.System, appServices.Settings, appServices.Notification)
	newScheduler.RegisterJob(scheduledPruneJob)

	fsWatcherJob, err := pkg_scheduler.RegisterFilesystemWatcherJob(appCtx, appServices.Project, appServices.Template, appServices.Settings, appConfig.ProjectScanMaxDepth)
	if err != nil {
		slog.ErrorContext(appCtx, "Failed to register filesystem watcher job", "error", err)
	}

	gitOpsSyncJob := pkg_scheduler.NewGitOpsSyncJob(appServices.GitOpsSync, appServices.Settings)
	newScheduler.RegisterJob(gitOpsSyncJob)

	vulnerabilityScanJob := pkg_scheduler.NewVulnerabilityScanJob(appServices.Vulnerability, appServices.Settings)
	newScheduler.RegisterJob(vulnerabilityScanJob)

	autoHealJob := pkg_scheduler.NewAutoHealJob(appServices.Docker, appServices.Settings, appServices.Event, appServices.Notification)
	newScheduler.RegisterJob(autoHealJob)

	setupSettingsCallbacks(appCtx, appServices, appConfig, newScheduler, imagePollingJob, autoUpdateJob, environmentHealthJob, fsWatcherJob, scheduledPruneJob, vulnerabilityScanJob, autoHealJob)
}

func setupSettingsCallbacks(lifecycleCtx context.Context, appServices *Services, appConfig *config.Config, newScheduler *pkg_scheduler.JobScheduler, imagePollingJob *pkg_scheduler.ImagePollingJob, autoUpdateJob *pkg_scheduler.AutoUpdateJob, environmentHealthJob *pkg_scheduler.EnvironmentHealthJob, fsWatcherJob *pkg_scheduler.FilesystemWatcherJob, scheduledPruneJob *pkg_scheduler.ScheduledPruneJob, vulnerabilityScanJob *pkg_scheduler.VulnerabilityScanJob, autoHealJob *pkg_scheduler.AutoHealJob) {
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
	appServices.Settings.OnTemplatesDirectoryChanged = func(_ context.Context) {
		if fsWatcherJob != nil {
			if err := fsWatcherJob.RestartTemplatesWatcher(lifecycleCtx); err != nil {
				slog.WarnContext(lifecycleCtx, "Failed to restart templates filesystem watcher", "error", err)
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
	appServices.Settings.OnAutoHealSettingsChanged = func(ctx context.Context) {
		if err := newScheduler.RescheduleJob(ctx, autoHealJob); err != nil {
			slog.WarnContext(ctx, "Failed to reschedule auto-heal job", "error", err)
		}
	}

	// Only set up timeout sync callback on main instance (not in agent mode)
	if !appConfig.AgentMode {
		appServices.Settings.OnTimeoutSettingsChanged = func(ctx context.Context, timeoutSettings []libarcane.SettingUpdate) {
			go syncTimeoutSettingsToAgentsInternal(context.WithoutCancel(ctx), appServices, timeoutSettings)
		}
	}
}

// syncTimeoutSettingsToAgentsInternal syncs timeout settings to all connected remote environments
func syncTimeoutSettingsToAgentsInternal(ctx context.Context, appServices *Services, timeoutSettings []libarcane.SettingUpdate) {
	envs, err := appServices.Environment.ListRemoteEnvironments(ctx)
	if err != nil {
		slog.WarnContext(ctx, "Failed to list remote environments for timeout sync", "error", err)
		return
	}

	if len(envs) == 0 {
		return
	}

	// Build the settings update payload
	settingsMap := make(map[string]string, len(timeoutSettings))
	keys := make([]string, 0, len(timeoutSettings))
	for _, update := range timeoutSettings {
		settingsMap[update.Key] = update.Value
		keys = append(keys, update.Key)
	}
	body, err := json.Marshal(settingsMap)
	if err != nil {
		slog.WarnContext(ctx, "Failed to marshal timeout settings for sync", "error", err)
		return
	}

	slog.InfoContext(ctx, "Syncing environment settings to remote environments", "count", len(envs), "keys", keys)

	for _, env := range envs {
		resp, err := appServices.Environment.ExecuteRemoteRequest(ctx, env.ID, http.MethodPut, "/api/environments/0/settings", body)
		if err != nil {
			slog.WarnContext(ctx, "Failed to sync timeout settings to environment", "environmentID", env.ID, "environmentName", env.Name, "error", err)
			continue
		}
		if err := resp.RequireSuccess(); err != nil {
			slog.WarnContext(ctx, "Environment returned non-OK status for timeout sync", "environmentID", env.ID, "environmentName", env.Name, "statusCode", resp.StatusCode, "response", string(resp.Body))
			continue
		}
		slog.DebugContext(ctx, "Successfully synced timeout settings to environment", "environmentID", env.ID, "environmentName", env.Name)
	}
}
