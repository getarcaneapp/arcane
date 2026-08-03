package bootstrap

import (
	"context"
	"encoding/json/v2"
	"log/slog"
	"net/http"
	"slices"

	"github.com/getarcaneapp/arcane/backend/v2/internal/actors"
	"github.com/getarcaneapp/arcane/backend/v2/internal/config"
	"github.com/getarcaneapp/arcane/backend/v2/internal/models"
	"github.com/getarcaneapp/arcane/backend/v2/internal/services"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/libarcane"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/scheduler"
	schedulertypes "github.com/getarcaneapp/arcane/types/v2/scheduler"
	"go.uber.org/fx"
)

func newJobScheduler(appCtx context.Context, lc fx.Lifecycle, cfg *config.Config, runtime *actors.Runtime, _ *actors.Gate[actors.AdmissionKey], imageUpdateWatcher *scheduler.ImageUpdateWatcher, analytics *scheduler.AnalyticsJob, systemUpgrade *services.SystemUpgradeService) (schedulertypes.JobScheduler, error) {
	schedulerCtx, cancelScheduler := context.WithCancel(appCtx)
	jobScheduler, err := scheduler.NewJobScheduler(schedulerCtx, runtime, cfg.GetLocation())
	if err != nil {
		cancelScheduler()
		return nil, err
	}
	lc.Append(fx.Hook{
		OnStart: func(context.Context) error {
			slog.InfoContext(appCtx, "Starting scheduler")
			if imageUpdateWatcher != nil {
				if err := jobScheduler.RegisterBusWatcher(imageUpdateWatcher, true); err != nil {
					return err
				}
			}
			if err := jobScheduler.StartScheduler(); err != nil {
				return err
			}
			if analytics != nil {
				go analytics.Run(schedulerCtx)
			}
			if !cfg.AgentMode && systemUpgrade != nil {
				go systemUpgrade.ResumeUpdateAllOnStartup(schedulerCtx)
			}
			return nil
		},
		OnStop: func(ctx context.Context) error {
			err := jobScheduler.Stop(ctx)
			cancelScheduler()
			if err != nil {
				slog.ErrorContext(ctx, "Job scheduler exited with error", "error", err)
				return err
			}
			slog.InfoContext(ctx, "Scheduler stopped")
			return nil
		},
	})
	return jobScheduler, nil
}

type registerJobsParams struct {
	fx.In

	Lifecycle    fx.Lifecycle
	AppCtx       context.Context
	Config       *config.Config
	Scheduler    schedulertypes.JobScheduler
	ActorRuntime *actors.Runtime

	Activity    *services.ActivityService
	GitOpsSync  *services.GitOpsSyncService
	Snippet     *services.SnippetService
	Environment *services.EnvironmentService
	JobSchedule *services.JobService
	Settings    *services.SettingsService
	Admission   *actors.Gate[actors.AdmissionKey]

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

func registerJobs(params registerJobsParams) error {
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

	for _, job := range []schedulertypes.Job{
		params.AutoUpdate,
		params.DockerClientRefresh,
		params.Analytics,
		params.EventCleanup,
		params.PruningVolumeHelper,
		params.ExpiredSessionsCleanup,
		params.ScheduledPrune,
		params.VulnerabilityScan,
		params.AutoHeal,
		params.ActivitySweep,
	} {
		if err := params.Scheduler.RegisterJob(job); err != nil {
			return err
		}
	}
	// FilesystemWatcher is intentionally not scheduler-registered; it watches inline
	// and is only rebound on settings changes below.
	// Internal self-healing sweep (managers and agents alike); intentionally
	// absent from job_metadata so it stays out of the Jobs UI.

	// GitOps sync and environment health are no longer single global jobs; each
	// entity registers its own dynamic job.
	if err := registerDynamicJobs(dynamicJobsParams{
		AppCtx:      params.AppCtx,
		Config:      params.Config,
		Scheduler:   params.Scheduler,
		GitOpsSync:  params.GitOpsSync,
		Snippet:     params.Snippet,
		Environment: params.Environment,
		JobSchedule: params.JobSchedule,
		Admission:   params.Admission,
	}); err != nil {
		return err
	}

	if err := setupSettingsSubscriptionsInternal(settingsSubscriptionsParams{
		Lifecycle:          params.Lifecycle,
		LifecycleCtx:       params.AppCtx,
		Config:             params.Config,
		Scheduler:          params.Scheduler,
		ActorRuntime:       params.ActorRuntime,
		Settings:           params.Settings,
		Environment:        params.Environment,
		AutoUpdate:         params.AutoUpdate,
		ImageUpdateWatcher: params.ImageUpdateWatcher,
		FilesystemWatcher:  params.FilesystemWatcher,
		ScheduledPrune:     params.ScheduledPrune,
		VulnerabilityScan:  params.VulnerabilityScan,
		AutoHeal:           params.AutoHeal,
	}); err != nil {
		return err
	}
	return nil
}

type dynamicJobsParams struct {
	AppCtx      context.Context
	Config      *config.Config
	Scheduler   schedulertypes.JobScheduler
	GitOpsSync  *services.GitOpsSyncService
	Snippet     *services.SnippetService
	Environment *services.EnvironmentService
	JobSchedule *services.JobService
	Admission   *actors.Gate[actors.AdmissionKey]
}

// registerDynamicJobs injects the scheduler into the services that own per-entity
// jobs and registers the jobs for already-existing entities at startup. AddJob is
// an idempotent upsert, so these run safely before the scheduler is started.
func registerDynamicJobs(params dynamicJobsParams) error {
	// GitOps: one job per auto-sync-enabled sync (runs on manager and agents).
	if params.GitOpsSync != nil {
		if err := params.GitOpsSync.SetScheduler(params.AppCtx, params.Scheduler, params.Admission); err != nil {
			return err
		}
		params.GitOpsSync.RegisterAutoSyncJobsOnStartup(params.AppCtx)
	}

	// Snippets: one job per schedule-enabled snippet. Unconditional (not gated
	// on !AgentMode) — a snippet is defined on and executed by the environment
	// that owns it, so an agent must schedule its own snippet rows on its own
	// scheduler too, which is also correct when the tunnel to the manager is
	// down.
	if params.Snippet != nil {
		params.Snippet.SetScheduler(params.AppCtx, params.Scheduler)
		params.Snippet.RegisterScheduledSnippetsOnStartup(params.AppCtx)
	}

	// Environment health: one job per enabled environment (manager only). The Jobs
	// UI still addresses "environment-health" by ID, so bridge its reschedule and
	// run-now back to EnvironmentService.
	if !params.Config.AgentMode && params.Environment != nil {
		if err := params.Environment.SetScheduler(params.AppCtx, params.Scheduler, params.Admission); err != nil {
			return err
		}
		params.JobSchedule.OnEnvironmentHealthReschedule = func(ctx context.Context) {
			params.Environment.RescheduleHealthJobs(ctx)
		}
		params.JobSchedule.RunEnvironmentHealthNow = func(ctx context.Context) error {
			return params.Environment.RunHealthChecksNow(ctx)
		}
		params.Environment.RegisterHealthJobsOnStartup(params.AppCtx)
	}
	return nil
}

type settingsSubscriptionsParams struct {
	Lifecycle    fx.Lifecycle
	LifecycleCtx context.Context
	Config       *config.Config
	Scheduler    settingsEffectsSchedulerInternal
	ActorRuntime *actors.Runtime
	Settings     settingsChangeSubscriberInternal
	Environment  timeoutSettingsEnvironmentInternal

	AutoUpdate         *scheduler.AutoUpdateJob
	ImageUpdateWatcher *scheduler.ImageUpdateWatcher
	FilesystemWatcher  *scheduler.FilesystemWatcherJob
	ScheduledPrune     *scheduler.ScheduledPruneJob
	VulnerabilityScan  *scheduler.VulnerabilityScanJob
	AutoHeal           *scheduler.AutoHealJob
}

type settingsChangeSubscriberInternal interface {
	SubscribeSettingsChanges(keys []string, callback func([]libarcane.SettingUpdate)) func()
}

type settingsEffectsSchedulerInternal interface {
	RescheduleJob(ctx context.Context, job schedulertypes.Job) error
}

type timeoutSettingsEnvironmentInternal interface {
	ListRemoteEnvironments(ctx context.Context) ([]models.Environment, error)
	ProxyRequest(ctx context.Context, envID string, method string, path string, body []byte) ([]byte, int, error)
}

func setupSettingsSubscriptionsInternal(params settingsSubscriptionsParams) error {
	unsubscribes := make([]func(), 0, 8)
	subscribe := func(keys []string, callback func([]libarcane.SettingUpdate)) {
		unsubscribes = append(unsubscribes, params.Settings.SubscribeSettingsChanges(keys, callback))
	}
	timeoutSyncExecutor, cancelTimeoutSync, err := setupTimeoutSettingsSubscriptionInternal(params, subscribe)
	if err != nil {
		return err
	}

	subscribe([]string{"pollingEnabled", "pollingInterval"}, func(_ []libarcane.SettingUpdate) {
		if params.ImageUpdateWatcher != nil {
			params.ImageUpdateWatcher.RefreshSchedule()
			params.ImageUpdateWatcher.Trigger()
		}
		if err := params.Scheduler.RescheduleJob(params.LifecycleCtx, params.AutoUpdate); err != nil {
			slog.WarnContext(params.LifecycleCtx, "Failed to reschedule auto-update job", "error", err)
		}
	})

	subscribe([]string{"autoUpdate", "autoUpdateInterval"}, func(_ []libarcane.SettingUpdate) {
		if err := params.Scheduler.RescheduleJob(params.LifecycleCtx, params.AutoUpdate); err != nil {
			slog.WarnContext(params.LifecycleCtx, "Failed to reschedule auto-update job", "error", err)
		}
	})

	subscribe([]string{"projectsDirectory", "followProjectSymlinks"}, func(_ []libarcane.SettingUpdate) {
		if params.FilesystemWatcher != nil {
			if err := params.FilesystemWatcher.RestartProjectsWatcher(params.LifecycleCtx); err != nil {
				slog.WarnContext(params.LifecycleCtx, "Failed to restart projects filesystem watcher", "error", err)
			}
		}
	})

	subscribe([]string{"templatesDirectory"}, func(_ []libarcane.SettingUpdate) {
		if params.FilesystemWatcher != nil {
			if err := params.FilesystemWatcher.RestartTemplatesWatcher(params.LifecycleCtx); err != nil {
				slog.WarnContext(params.LifecycleCtx, "Failed to restart templates filesystem watcher", "error", err)
			}
		}
	})

	subscribe([]string{"scheduledPruneEnabled", "scheduledPruneInterval"}, func(_ []libarcane.SettingUpdate) {
		if err := params.Scheduler.RescheduleJob(params.LifecycleCtx, params.ScheduledPrune); err != nil {
			slog.WarnContext(params.LifecycleCtx, "Failed to reschedule scheduled-prune job", "error", err)
		}
	})

	subscribe(
		[]string{
			"vulnerabilityScanEnabled", "vulnerabilityScanInterval", "trivyNetwork", "trivySecurityOpts", "trivyPrivileged",
			"trivyResourceLimitsEnabled", "trivyCpuLimit", "trivyMemoryLimitMb", "trivyConcurrentScanContainers",
		},
		func(_ []libarcane.SettingUpdate) {
			if err := params.Scheduler.RescheduleJob(params.LifecycleCtx, params.VulnerabilityScan); err != nil {
				slog.WarnContext(params.LifecycleCtx, "Failed to reschedule vulnerability-scan job", "error", err)
			}
		},
	)

	subscribe(
		[]string{"autoHealEnabled", "autoHealInterval", "autoHealExcludedContainers", "autoHealMaxRestarts", "autoHealRestartWindow"},
		func(_ []libarcane.SettingUpdate) {
			if err := params.Scheduler.RescheduleJob(params.LifecycleCtx, params.AutoHeal); err != nil {
				slog.WarnContext(params.LifecycleCtx, "Failed to reschedule auto-heal job", "error", err)
			}
		},
	)

	params.Lifecycle.Append(fx.Hook{
		OnStop: func(ctx context.Context) error {
			for _, unsubscribe := range slices.Backward(unsubscribes) {
				unsubscribe()
			}
			if cancelTimeoutSync != nil {
				cancelTimeoutSync()
			}
			return timeoutSyncExecutor.Stop(ctx)
		},
	})
	return nil
}

func setupTimeoutSettingsSubscriptionInternal(params settingsSubscriptionsParams, subscribe func([]string, func([]libarcane.SettingUpdate))) (*actors.Executor, context.CancelFunc, error) {
	if params.Config.AgentMode {
		return nil, nil, nil
	}

	timeoutSyncExecutor, err := actors.NewExecutor(params.LifecycleCtx, params.ActorRuntime, "services", "settings-timeout-sync", 3)
	if err != nil {
		return nil, nil, err
	}
	timeoutSyncContext, cancelTimeoutSync := context.WithCancel(params.LifecycleCtx)
	subscribe(libarcane.TimeoutSettingKeys(), func(updates []libarcane.SettingUpdate) {
		_, err := actors.Submit(timeoutSyncContext, timeoutSyncExecutor, "sync timeout settings to remote environments", func(ctx context.Context) (actors.NoPayload, error) {
			syncTimeoutSettingsToAgentsInternal(ctx, params.Environment, updates)
			return actors.NoPayload{}, nil
		}, nil)
		if err != nil && timeoutSyncContext.Err() == nil {
			slog.ErrorContext(timeoutSyncContext, "Failed to queue timeout settings sync", "error", err)
		}
	})
	return timeoutSyncExecutor, cancelTimeoutSync, nil
}

// syncTimeoutSettingsToAgentsInternal syncs timeout settings to all connected remote environments
func syncTimeoutSettingsToAgentsInternal(ctx context.Context, environment timeoutSettingsEnvironmentInternal, timeoutSettings []libarcane.SettingUpdate) {
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
		responseBody, statusCode, err := environment.ProxyRequest(ctx, env.ID, http.MethodPut, "/api/environments/0/settings", body)
		if err != nil {
			slog.WarnContext(ctx, "Failed to sync timeout settings to environment", "environmentID", env.ID, "environmentName", env.Name, "error", err)
			continue
		}
		if statusCode < http.StatusOK || statusCode >= http.StatusMultipleChoices {
			slog.WarnContext(ctx, "Environment returned non-OK status for timeout sync", "environmentID", env.ID, "environmentName", env.Name, "statusCode", statusCode, "response", string(responseBody))
			continue
		}
		slog.DebugContext(ctx, "Successfully synced timeout settings to environment", "environmentID", env.ID, "environmentName", env.Name)
	}
}
