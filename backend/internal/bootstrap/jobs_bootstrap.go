package bootstrap

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"

	"github.com/getarcaneapp/arcane/backend/internal/config"
	"github.com/getarcaneapp/arcane/backend/internal/di"
	"github.com/getarcaneapp/arcane/backend/pkg/libarcane"
	pkg_scheduler "github.com/getarcaneapp/arcane/backend/pkg/scheduler"
)

func registerJobs(appCtx context.Context, newScheduler *pkg_scheduler.JobScheduler, appServices *di.Services, appConfig *config.Config) {
	// wire constructs every job from the built services; bootstrap owns registration,
	// the agent-mode gating, the startup heartbeat, and the settings callbacks.
	jobs := di.InitializeJobs(appCtx, appConfig, appServices)

	if appServices.Activity != nil {
		failed, err := appServices.Activity.FailStaleImageUpdateChecks(appCtx)
		if err != nil {
			slog.WarnContext(appCtx, "Failed to mark stale image update checks as failed", "count", failed, "error", err)
		} else if failed > 0 {
			slog.InfoContext(appCtx, "Marked stale image update checks as failed", "count", failed)
		}
	}

	newScheduler.RegisterJob(jobs.AutoUpdate)
	newScheduler.RegisterJob(jobs.ImagePolling)
	newScheduler.RegisterJob(jobs.DockerClientRefresh)
	newScheduler.RegisterJob(jobs.Analytics)
	// Send initial heartbeat on startup without blocking bootstrap.
	go jobs.Analytics.Run(appCtx)
	newScheduler.RegisterJob(jobs.EventCleanup)
	newScheduler.RegisterJob(jobs.PruningVolumeHelper)
	newScheduler.RegisterJob(jobs.ExpiredSessionsCleanup)
	newScheduler.RegisterJob(jobs.ScheduledPrune)
	// FilesystemWatcher is intentionally not scheduler-registered; it watches inline
	// and is only rebound on settings changes below.
	newScheduler.RegisterJob(jobs.VulnerabilityScan)
	newScheduler.RegisterJob(jobs.AutoHeal)

	// GitOps sync and environment health are no longer single global jobs; each
	// entity registers its own dynamic job.
	registerDynamicJobs(appCtx, newScheduler, appServices, appConfig)

	setupSettingsCallbacks(appCtx, appServices, appConfig, newScheduler, jobs)
}

// registerDynamicJobs injects the scheduler into the services that own per-entity
// jobs and registers the jobs for already-existing entities at startup. AddJob is
// an idempotent upsert, so these run safely before the scheduler is started.
func registerDynamicJobs(appCtx context.Context, newScheduler *pkg_scheduler.JobScheduler, appServices *di.Services, appConfig *config.Config) {
	// GitOps: one job per auto-sync-enabled sync (runs on manager and agents).
	if appServices.GitOpsSync != nil {
		appServices.GitOpsSync.SetScheduler(appCtx, newScheduler)
		appServices.GitOpsSync.RegisterAutoSyncJobsOnStartup(appCtx)
	}

	// Environment health: one job per enabled environment (manager only). The Jobs
	// UI still addresses "environment-health" by ID, so bridge its reschedule and
	// run-now back to EnvironmentService.
	if !appConfig.AgentMode && appServices.Environment != nil {
		appServices.Environment.SetScheduler(appCtx, newScheduler)
		appServices.JobSchedule.OnEnvironmentHealthReschedule = func(ctx context.Context) {
			appServices.Environment.RescheduleHealthJobs(ctx)
		}
		appServices.JobSchedule.RunEnvironmentHealthNow = func(ctx context.Context) error {
			return appServices.Environment.RunHealthChecksNow(ctx)
		}
		appServices.Environment.RegisterHealthJobsOnStartup(appCtx)
	}
}

func setupSettingsCallbacks(lifecycleCtx context.Context, appServices *di.Services, appConfig *config.Config, newScheduler *pkg_scheduler.JobScheduler, jobs *di.Jobs) {
	appServices.Settings.OnImagePollingSettingsChanged = func(_ context.Context) {
		if err := newScheduler.RescheduleJob(lifecycleCtx, jobs.ImagePolling); err != nil {
			slog.WarnContext(lifecycleCtx, "Failed to reschedule image-polling job", "error", err)
		}
		if err := newScheduler.RescheduleJob(lifecycleCtx, jobs.AutoUpdate); err != nil {
			slog.WarnContext(lifecycleCtx, "Failed to reschedule auto-update job", "error", err)
		}
	}
	appServices.Settings.OnAutoUpdateSettingsChanged = func(ctx context.Context) {
		slog.DebugContext(lifecycleCtx, "AutoUpdateSettingsChanged callback triggered", "triggerContextCanceled", ctx.Err() != nil)
		if err := newScheduler.RescheduleJob(lifecycleCtx, jobs.AutoUpdate); err != nil {
			slog.WarnContext(lifecycleCtx, "Failed to reschedule auto-update job", "error", err)
		}
	}
	appServices.Settings.OnProjectsDirectoryChanged = func(_ context.Context) {
		if jobs.FilesystemWatcher != nil {
			if err := jobs.FilesystemWatcher.RestartProjectsWatcher(lifecycleCtx); err != nil {
				slog.WarnContext(lifecycleCtx, "Failed to restart projects filesystem watcher", "error", err)
			}
		}
	}
	appServices.Settings.OnTemplatesDirectoryChanged = func(_ context.Context) {
		if jobs.FilesystemWatcher != nil {
			if err := jobs.FilesystemWatcher.RestartTemplatesWatcher(lifecycleCtx); err != nil {
				slog.WarnContext(lifecycleCtx, "Failed to restart templates filesystem watcher", "error", err)
			}
		}
	}
	appServices.Settings.OnScheduledPruneSettingsChanged = func(_ context.Context) {
		if err := newScheduler.RescheduleJob(lifecycleCtx, jobs.ScheduledPrune); err != nil {
			slog.WarnContext(lifecycleCtx, "Failed to reschedule scheduled-prune job", "error", err)
		}
	}
	appServices.Settings.OnVulnerabilityScanSettingsChanged = func(_ context.Context) {
		if err := newScheduler.RescheduleJob(lifecycleCtx, jobs.VulnerabilityScan); err != nil {
			slog.WarnContext(lifecycleCtx, "Failed to reschedule vulnerability-scan job", "error", err)
		}
	}
	appServices.Settings.OnAutoHealSettingsChanged = func(ctx context.Context) {
		if err := newScheduler.RescheduleJob(ctx, jobs.AutoHeal); err != nil {
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
func syncTimeoutSettingsToAgentsInternal(ctx context.Context, appServices *di.Services, timeoutSettings []libarcane.SettingUpdate) {
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
