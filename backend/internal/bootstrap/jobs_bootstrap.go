package bootstrap

import (
	"context"
	json "encoding/json/v2"
	"log/slog"
	"net/http"

	"github.com/getarcaneapp/arcane/backend/v2/internal/config"
	"github.com/getarcaneapp/arcane/backend/v2/internal/services"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/libarcane"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/scheduler"
	"go.uber.org/fx"
)

func newJobScheduler(appCtx context.Context, lc fx.Lifecycle, cfg *config.Config, imageUpdateWatcher *scheduler.ImageUpdateWatcher, analytics *scheduler.AnalyticsJob, systemUpgrade *services.SystemUpgradeService) *scheduler.JobScheduler {
	schedulerCtx, cancelScheduler := context.WithCancel(appCtx)
	jobScheduler := scheduler.NewJobScheduler(schedulerCtx, cfg.GetLocation())
	lc.Append(fx.Hook{
		OnStart: func(context.Context) error {
			slog.InfoContext(appCtx, "Starting scheduler")
			jobScheduler.RegisterBusWatcher(imageUpdateWatcher, true)
			jobScheduler.StartScheduler()
			if analytics != nil {
				go analytics.Run(schedulerCtx)
			}
			if !cfg.AgentMode && systemUpgrade != nil {
				go systemUpgrade.ResumeUpdateAllOnStartup(schedulerCtx)
			}
			return nil
		},
		OnStop: func(ctx context.Context) error {
			cancelScheduler()
			err := jobScheduler.Stop(ctx)
			if err != nil {
				slog.ErrorContext(ctx, "Job scheduler exited with error", "error", err)
				return err
			}
			slog.InfoContext(ctx, "Scheduler stopped")
			return nil
		},
	})
	return jobScheduler
}

type registerJobsParams struct {
	fx.In

	AppCtx    context.Context
	Config    *config.Config
	Scheduler *scheduler.JobScheduler

	Activity    *services.ActivityService
	GitOpsSync  *services.GitOpsSyncService
	Environment *services.EnvironmentService
	JobSchedule *services.JobService
	Settings    *services.SettingsService

	AutoUpdate             *scheduler.AutoUpdateJob
	ImageUpdateWatcher     *scheduler.ImageUpdateWatcher
	DockerClientRefresh    *scheduler.DockerClientRefreshJob
	Analytics              *scheduler.AnalyticsJob
	EventCleanup           *scheduler.EventCleanupJob
	PruningVolumeHelper    *scheduler.PruningVolumeHelperJob
	ExpiredSessionsCleanup *scheduler.ExpiredSessionsCleanupJob
	ScheduledPrune         *scheduler.ScheduledPruneJob
	FilesystemWatcher      *scheduler.FilesystemWatcherJob
	VulnerabilityScan      *scheduler.VulnerabilityScanJob
	AutoHeal               *scheduler.AutoHealJob
	ActivitySweep          *scheduler.ActivitySweepJob
}

func registerJobs(params registerJobsParams) {
	params.JobSchedule.SetScheduler(params.AppCtx, params.Scheduler)

	// Bootstrap owns registration, agent-mode gating, and settings callbacks.
	if params.Activity != nil {
		failed, err := params.Activity.FailStaleImageUpdateChecks(params.AppCtx)
		if err != nil {
			slog.WarnContext(params.AppCtx, "Failed to mark stale image update checks as failed", "count", failed, "error", err)
		} else if failed > 0 {
			slog.InfoContext(params.AppCtx, "Marked stale image update checks as failed", "count", failed)
		}

		resolved, err := params.Activity.ResolveStaleAutoUpdateActivities(params.AppCtx)
		if err != nil {
			slog.WarnContext(params.AppCtx, "Failed to resolve stale auto-update activities", "count", resolved, "error", err)
		} else if resolved > 0 {
			slog.InfoContext(params.AppCtx, "Resolved stale auto-update activities", "count", resolved)
		}

		orphaned, err := params.Activity.ResolveOrphanedQueuedActivities(params.AppCtx)
		if err != nil {
			slog.WarnContext(params.AppCtx, "Failed to resolve orphaned queued activities", "count", orphaned, "error", err)
		} else if orphaned > 0 {
			slog.InfoContext(params.AppCtx, "Resolved orphaned queued activities", "count", orphaned)
		}
	}

	params.Scheduler.RegisterJob(params.AutoUpdate)
	params.Scheduler.RegisterJob(params.DockerClientRefresh)
	params.Scheduler.RegisterJob(params.Analytics)
	params.Scheduler.RegisterJob(params.EventCleanup)
	params.Scheduler.RegisterJob(params.PruningVolumeHelper)
	params.Scheduler.RegisterJob(params.ExpiredSessionsCleanup)
	params.Scheduler.RegisterJob(params.ScheduledPrune)
	// FilesystemWatcher is intentionally not scheduler-registered; it watches inline
	// and is only rebound on settings changes below.
	params.Scheduler.RegisterJob(params.VulnerabilityScan)
	params.Scheduler.RegisterJob(params.AutoHeal)
	// Internal self-healing sweep (managers and agents alike); intentionally
	// absent from job_metadata so it stays out of the Jobs UI.
	params.Scheduler.RegisterJob(params.ActivitySweep)

	// GitOps sync and environment health are no longer single global jobs; each
	// entity registers its own dynamic job.
	registerDynamicJobs(dynamicJobsParams{
		AppCtx:      params.AppCtx,
		Config:      params.Config,
		Scheduler:   params.Scheduler,
		GitOpsSync:  params.GitOpsSync,
		Environment: params.Environment,
		JobSchedule: params.JobSchedule,
	})

	setupSettingsCallbacks(settingsCallbacksParams{
		LifecycleCtx:       params.AppCtx,
		Config:             params.Config,
		Scheduler:          params.Scheduler,
		Settings:           params.Settings,
		Environment:        params.Environment,
		AutoUpdate:         params.AutoUpdate,
		ImageUpdateWatcher: params.ImageUpdateWatcher,
		FilesystemWatcher:  params.FilesystemWatcher,
		ScheduledPrune:     params.ScheduledPrune,
		VulnerabilityScan:  params.VulnerabilityScan,
		AutoHeal:           params.AutoHeal,
	})
}

type dynamicJobsParams struct {
	AppCtx      context.Context
	Config      *config.Config
	Scheduler   *scheduler.JobScheduler
	GitOpsSync  *services.GitOpsSyncService
	Environment *services.EnvironmentService
	JobSchedule *services.JobService
}

// registerDynamicJobs injects the scheduler into the services that own per-entity
// jobs and registers the jobs for already-existing entities at startup. AddJob is
// an idempotent upsert, so these run safely before the scheduler is started.
func registerDynamicJobs(params dynamicJobsParams) {
	// GitOps: one job per auto-sync-enabled sync (runs on manager and agents).
	if params.GitOpsSync != nil {
		params.GitOpsSync.SetScheduler(params.AppCtx, params.Scheduler)
		params.GitOpsSync.RegisterAutoSyncJobsOnStartup(params.AppCtx)
	}

	// Environment health: one job per enabled environment (manager only). The Jobs
	// UI still addresses "environment-health" by ID, so bridge its reschedule and
	// run-now back to EnvironmentService.
	if !params.Config.AgentMode && params.Environment != nil {
		params.Environment.SetScheduler(params.AppCtx, params.Scheduler)
		params.JobSchedule.OnEnvironmentHealthReschedule = func(ctx context.Context) {
			params.Environment.RescheduleHealthJobs(ctx)
		}
		params.JobSchedule.RunEnvironmentHealthNow = func(ctx context.Context) error {
			return params.Environment.RunHealthChecksNow(ctx)
		}
		params.Environment.RegisterHealthJobsOnStartup(params.AppCtx)
	}
}

type settingsCallbacksParams struct {
	LifecycleCtx context.Context
	Config       *config.Config
	Scheduler    *scheduler.JobScheduler
	Settings     *services.SettingsService
	Environment  *services.EnvironmentService

	AutoUpdate         *scheduler.AutoUpdateJob
	ImageUpdateWatcher *scheduler.ImageUpdateWatcher
	FilesystemWatcher  *scheduler.FilesystemWatcherJob
	ScheduledPrune     *scheduler.ScheduledPruneJob
	VulnerabilityScan  *scheduler.VulnerabilityScanJob
	AutoHeal           *scheduler.AutoHealJob
}

//nolint:contextcheck // callbacks intentionally use the app lifecycle context so reschedules outlive the triggering request context.
func setupSettingsCallbacks(params settingsCallbacksParams) {
	params.Settings.OnImagePollingSettingsChanged = func(_ context.Context) {
		if params.ImageUpdateWatcher != nil {
			params.ImageUpdateWatcher.RefreshSchedule()
			params.ImageUpdateWatcher.Trigger()
		}
		if err := params.Scheduler.RescheduleJob(params.LifecycleCtx, params.AutoUpdate); err != nil {
			slog.WarnContext(params.LifecycleCtx, "Failed to reschedule auto-update job", "error", err)
		}
	}
	params.Settings.OnAutoUpdateSettingsChanged = func(ctx context.Context) {
		slog.DebugContext(params.LifecycleCtx, "AutoUpdateSettingsChanged callback triggered", "triggerContextCanceled", ctx.Err() != nil)
		if err := params.Scheduler.RescheduleJob(params.LifecycleCtx, params.AutoUpdate); err != nil {
			slog.WarnContext(params.LifecycleCtx, "Failed to reschedule auto-update job", "error", err)
		}
	}
	params.Settings.OnProjectsDirectoryChanged = func(_ context.Context) {
		if params.FilesystemWatcher != nil {
			if err := params.FilesystemWatcher.RestartProjectsWatcher(params.LifecycleCtx); err != nil {
				slog.WarnContext(params.LifecycleCtx, "Failed to restart projects filesystem watcher", "error", err)
			}
		}
	}
	params.Settings.OnTemplatesDirectoryChanged = func(_ context.Context) {
		if params.FilesystemWatcher != nil {
			if err := params.FilesystemWatcher.RestartTemplatesWatcher(params.LifecycleCtx); err != nil {
				slog.WarnContext(params.LifecycleCtx, "Failed to restart templates filesystem watcher", "error", err)
			}
		}
	}
	params.Settings.OnScheduledPruneSettingsChanged = func(_ context.Context) {
		if err := params.Scheduler.RescheduleJob(params.LifecycleCtx, params.ScheduledPrune); err != nil {
			slog.WarnContext(params.LifecycleCtx, "Failed to reschedule scheduled-prune job", "error", err)
		}
	}
	params.Settings.OnVulnerabilityScanSettingsChanged = func(_ context.Context) {
		if err := params.Scheduler.RescheduleJob(params.LifecycleCtx, params.VulnerabilityScan); err != nil {
			slog.WarnContext(params.LifecycleCtx, "Failed to reschedule vulnerability-scan job", "error", err)
		}
	}
	params.Settings.OnAutoHealSettingsChanged = func(ctx context.Context) {
		if err := params.Scheduler.RescheduleJob(ctx, params.AutoHeal); err != nil {
			slog.WarnContext(ctx, "Failed to reschedule auto-heal job", "error", err)
		}
	}

	// Only set up timeout sync callback on main instance (not in agent mode)
	if !params.Config.AgentMode {
		params.Settings.OnTimeoutSettingsChanged = func(ctx context.Context, timeoutSettings []libarcane.SettingUpdate) {
			go syncTimeoutSettingsToAgentsInternal(context.WithoutCancel(ctx), params.Environment, timeoutSettings)
		}
	}
}

// syncTimeoutSettingsToAgentsInternal syncs timeout settings to all connected remote environments
func syncTimeoutSettingsToAgentsInternal(ctx context.Context, environment *services.EnvironmentService, timeoutSettings []libarcane.SettingUpdate) {
	envs, err := environment.ListRemoteEnvironments(ctx)
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
		resp, err := environment.ExecuteRemoteRequest(ctx, env.ID, http.MethodPut, "/api/environments/0/settings", body)
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
