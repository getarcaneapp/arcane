package bootstrap

import (
	"context"
	"encoding/json/v2"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"slices"
	"sync"
	"time"

	"github.com/getarcaneapp/arcane/backend/v2/internal/activity"
	"github.com/getarcaneapp/arcane/backend/v2/internal/apns"
	"github.com/getarcaneapp/arcane/backend/v2/internal/backup"
	"github.com/getarcaneapp/arcane/backend/v2/internal/config"
	"github.com/getarcaneapp/arcane/backend/v2/internal/environment"
	"github.com/getarcaneapp/arcane/backend/v2/internal/gitops"
	"github.com/getarcaneapp/arcane/backend/v2/internal/job"
	"github.com/getarcaneapp/arcane/backend/v2/internal/settings"
	"github.com/getarcaneapp/arcane/backend/v2/internal/system"
	"github.com/getarcaneapp/arcane/backend/v2/internal/systembackup"
	"github.com/getarcaneapp/arcane/backend/v2/internal/updater"
	"github.com/getarcaneapp/arcane/backend/v2/internal/volume"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/libarcane"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/scheduler"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/scheduler/runs"
	"github.com/getarcaneapp/arcane/types/v2/features"
	schedulertypes "github.com/getarcaneapp/arcane/types/v2/scheduler"
	"go.uber.org/fx"
)

func newJobScheduler(appCtx context.Context, lc fx.Lifecycle, cfg *config.Config, runtime *runs.Coordinator, _ *runs.Admission, imageUpdateWatcher *scheduler.ImageUpdateWatcher, analytics *scheduler.AnalyticsJob, systemUpgrade *system.SystemUpgradeService, jobService *job.JobService, backupEngine *backup.Engine, updaterService *updater.UpdaterService, volumes *volume.VolumeService, systemBackups *systembackup.SystemBackupService, gitopsSync *gitops.GitOpsSyncService) (schedulertypes.JobScheduler, error) {
	schedulerCtx, cancelScheduler := context.WithCancel(appCtx)
	jobScheduler, err := scheduler.NewJobScheduler(schedulerCtx, jobService.Coordinator(), cfg.GetLocation())
	if err != nil {
		cancelScheduler()
		return nil, err
	}
	jobService.SetScheduler(schedulerCtx, jobScheduler)
	var repairDone chan struct{}
	var resumeDone chan struct{}
	lc.Append(fx.Hook{
		OnStart: func(context.Context) error {
			slog.InfoContext(appCtx, "Starting scheduler")
			if err := jobService.Coordinator().Start(schedulerCtx); err != nil {
				return err
			}
			if err := reconcileCoordinatorStartupInternal(schedulerCtx, jobService, backupEngine, updaterService, volumes, systemBackups, gitopsSync); err != nil {
				return err
			}
			if imageUpdateWatcher != nil {
				imageUpdateWatcher.SetCoordinator(jobService.Coordinator())
				if err := jobScheduler.RegisterBusWatcher(imageUpdateWatcher, true); err != nil {
					return err
				}
			}
			if err := jobScheduler.StartScheduler(); err != nil {
				return err
			}
			jobService.Coordinator().Activate()
			if backupEngine != nil {
				repairDone = make(chan struct{})
				go func() {
					defer close(repairDone)
					repairBackupDispatchesInternal(schedulerCtx, backupEngine)
				}()
			}
			if analytics != nil {
				if _, err := jobService.Coordinator().Submit(schedulerCtx, schedulertypes.Request{JobID: analytics.Name(), EnvironmentID: "0", Trigger: "startup"}); err != nil {
					return err
				}
			}
			if !cfg.AgentMode && systemUpgrade != nil {
				resumeDone = make(chan struct{})
				go func() { defer close(resumeDone); systemUpgrade.ResumeUpdateAllOnStartup(schedulerCtx) }()
			}
			return nil
		},
		OnStop: func(ctx context.Context) error {
			return stopJobSchedulerInternal(ctx, cancelScheduler, []chan struct{}{repairDone, resumeDone}, jobService.Coordinator(), jobScheduler)
		},
	})
	return jobScheduler, nil
}

func stopJobSchedulerInternal(ctx context.Context, cancelScheduler context.CancelFunc, workers []chan struct{}, coordinator *runs.Coordinator, jobScheduler schedulertypes.JobScheduler) error {
	cancelScheduler()
	for _, done := range workers {
		if done != nil {
			select {
			case <-done:
			case <-ctx.Done():
				return ctx.Err()
			}
		}
	}
	err := errors.Join(coordinator.Stop(ctx), jobScheduler.Stop(ctx))
	if err != nil {
		slog.ErrorContext(ctx, "Job scheduler exited with error", "error", err)
		return err
	}
	slog.InfoContext(ctx, "Scheduler stopped")
	return nil
}

func reconcileCoordinatorStartupInternal(ctx context.Context, jobs *job.JobService, engine *backup.Engine, updates *updater.UpdaterService, volumes *volume.VolumeService, systemBackups *systembackup.SystemBackupService, syncService *gitops.GitOpsSyncService) error {
	activities, backups, err := startupProtectionInternal(ctx, jobs, engine, updates)
	if err != nil {
		return err
	}
	if volumes != nil {
		if err := volumes.ReconcileInterruptedBackups(ctx, backups...); err != nil {
			return err
		}
	}
	if systemBackups != nil {
		if err := systemBackups.ReconcileInterruptedBackups(ctx, backups...); err != nil {
			return err
		}
	}
	if syncService != nil {
		if err := syncService.ReconcileInterruptedBackupsOnStartup(ctx, backups...); err != nil {
			return err
		}
	}
	return jobs.ReconcileStartupActivities(ctx, activities...)
}

func startupProtectionInternal(ctx context.Context, jobs *job.JobService, engine *backup.Engine, updates *updater.UpdaterService) ([]string, []string, error) {
	records, err := jobs.Coordinator().Records(ctx)
	if err != nil {
		return nil, nil, err
	}
	scheduled, err := scheduledBackupProtectionInternal(records)
	if err != nil {
		return nil, nil, err
	}

	activities := []string{}
	backups := make([]string, 0, len(scheduled))
	if engine != nil {
		var err error
		activities, err = engine.ActiveActivityIDs(ctx)
		if err != nil {
			return nil, nil, err
		}
		backups, err = engine.ActiveRunIDs(ctx)
		if err != nil {
			return nil, nil, err
		}
		if err := engine.ReconcileDispatches(ctx); err != nil {
			return nil, nil, err
		}
	}
	if updates != nil {
		ids, err := updates.ActiveUpdateActivityIDs(ctx)
		if err != nil {
			return nil, nil, err
		}
		activities = append(activities, ids...)
	}
	return activities, append(backups, scheduled...), nil
}

func scheduledBackupProtectionInternal(records []schedulertypes.QueueRecord) ([]string, error) {
	ids := []string{}
	for _, record := range records {
		for _, run := range record.Runs {
			if run.Status.Terminal() {
				continue
			}
			for _, target := range run.Outcome.Targets {
				if len(target.RecoveryData) == 0 {
					continue
				}
				var checkpoint struct {
					BackupID string `json:"backupId"`
				}
				if err := json.Unmarshal(target.RecoveryData, &checkpoint); err != nil {
					return nil, fmt.Errorf("decode startup recovery evidence: %w", err)
				}
				if checkpoint.BackupID != "" {
					ids = append(ids, checkpoint.BackupID)
				}
			}
		}
	}
	return ids, nil
}

func repairBackupDispatchesInternal(ctx context.Context, engine *backup.Engine) {
	timer := time.NewTicker(15 * time.Second)
	defer timer.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-timer.C:
			if err := engine.ReconcileDispatches(ctx); err != nil && ctx.Err() == nil {
				slog.ErrorContext(ctx, "Backup dispatch reconciliation failed", "error", err)
			}
		}
	}
}

type registerJobsParams struct {
	fx.In

	Lifecycle fx.Lifecycle
	AppCtx    context.Context
	Config    *config.Config
	Scheduler schedulertypes.JobScheduler

	Activity     *activity.ActivityService
	GitOpsSync   *gitops.GitOpsSyncService
	Environment  *environment.EnvironmentService
	JobSchedule  *job.JobService
	Settings     *settings.SettingsService
	Volume       *volume.VolumeService
	SystemBackup *systembackup.SystemBackupService
	Admission    *runs.Admission
	Apns         *apns.ApnsService

	AutoUpdate             *scheduler.AutoUpdateJob
	ImageUpdateWatcher     *scheduler.ImageUpdateWatcher
	DockerClientRefresh    *scheduler.DockerClientRefreshJob
	Analytics              *scheduler.AnalyticsJob
	UpgradeLogCleanup      schedulertypes.Job `name:"upgrade-log-cleanup"`
	EventCleanup           *scheduler.EventCleanupJob
	PruningVolumeHelper    *scheduler.PruningVolumeHelperJob
	ExpiredSessionsCleanup *scheduler.ExpiredSessionsCleanupJob
	ScheduledPrune         *scheduler.ScheduledPruneJob
	FilesystemWatcher      *scheduler.FilesystemWatcherJob
	VulnerabilityScan      *scheduler.VulnerabilityScanJob
	VulnerabilityRisk      *scheduler.VulnerabilityRiskJob
	AutoPatch              *scheduler.AutoPatchJob
	AutoHeal               *scheduler.AutoHealJob
	ActivitySweep          *scheduler.ActivitySweepJob
	UploadSessionsCleanup  *scheduler.UploadSessionsCleanupJob
	GitCloneCleanup        *scheduler.GitCloneCleanupJob
	ApnsOutbox             *scheduler.ApnsOutboxJob
}

func registerJobs(params registerJobsParams) error {
	params.JobSchedule.SetScheduler(params.AppCtx, params.Scheduler)

	for _, job := range []schedulertypes.Job{
		params.AutoUpdate,
		params.DockerClientRefresh,
		params.Analytics,
		params.EventCleanup,
		params.PruningVolumeHelper,
		params.ExpiredSessionsCleanup,
		params.ScheduledPrune,
		params.VulnerabilityScan,
		params.VulnerabilityRisk,
		params.AutoPatch,
		params.AutoHeal,
		params.ActivitySweep,
		params.UploadSessionsCleanup,
		params.GitCloneCleanup,
		params.UpgradeLogCleanup,
		params.ApnsOutbox,
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
		AppCtx:       params.AppCtx,
		Config:       params.Config,
		Scheduler:    params.Scheduler,
		GitOpsSync:   params.GitOpsSync,
		Environment:  params.Environment,
		JobSchedule:  params.JobSchedule,
		Volume:       params.Volume,
		SystemBackup: params.SystemBackup,
		Admission:    params.Admission,
	}); err != nil {
		return err
	}

	if err := setupSettingsSubscriptionsInternal(settingsSubscriptionsParams{
		Lifecycle:          params.Lifecycle,
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
		VulnerabilityRisk:  params.VulnerabilityRisk,
		AutoPatch:          params.AutoPatch,
		AutoHeal:           params.AutoHeal,
		Apns:               params.Apns,
		ApnsOutbox:         params.ApnsOutbox,
	}); err != nil {
		return err
	}
	return nil
}

type dynamicJobsParams struct {
	AppCtx       context.Context
	Config       *config.Config
	Scheduler    schedulertypes.JobScheduler
	GitOpsSync   *gitops.GitOpsSyncService
	Environment  *environment.EnvironmentService
	JobSchedule  *job.JobService
	Volume       *volume.VolumeService
	SystemBackup *systembackup.SystemBackupService
	Admission    *runs.Admission
}

// registerDynamicJobs injects the scheduler into the services that own per-entity
// jobs and registers the jobs for already-existing entities at startup. AddJob is
// an idempotent upsert, so these run safely before the scheduler is started.
func registerDynamicJobs(params dynamicJobsParams) error {
	// Volume backups run on the environment that owns the Docker volume. This is
	// registered on managers and agents; environment proxying persists each policy
	// in the correct Arcane database.
	if params.Volume != nil {
		if err := params.Volume.SetScheduler(params.AppCtx, params.Scheduler, params.Admission); err != nil {
			return err
		}
		params.Volume.RegisterBackupJobsOnStartup(params.AppCtx)
	}
	if !params.Config.AgentMode && params.SystemBackup != nil {
		if err := params.SystemBackup.SetScheduler(params.AppCtx, params.Scheduler, params.Admission); err != nil {
			return err
		}
		params.SystemBackup.RegisterBackupJobOnStartup(params.AppCtx)
	}
	// GitOps: one job per auto-sync-enabled sync (runs on manager and agents).
	if params.GitOpsSync != nil {
		if err := params.GitOpsSync.SetScheduler(params.AppCtx, params.Scheduler, params.Admission); err != nil {
			return err
		}
		params.GitOpsSync.RegisterAutoSyncJobsOnStartup(params.AppCtx)
		params.GitOpsSync.SubscribeProjectFileChanges(params.AppCtx)
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
	Settings     settingsChangeSubscriberInternal
	Environment  timeoutSettingsEnvironmentInternal

	AutoUpdate         *scheduler.AutoUpdateJob
	ImageUpdateWatcher *scheduler.ImageUpdateWatcher
	FilesystemWatcher  *scheduler.FilesystemWatcherJob
	ScheduledPrune     *scheduler.ScheduledPruneJob
	VulnerabilityScan  *scheduler.VulnerabilityScanJob
	VulnerabilityRisk  *scheduler.VulnerabilityRiskJob
	AutoPatch          *scheduler.AutoPatchJob
	AutoHeal           *scheduler.AutoHealJob
	Apns               *apns.ApnsService
	ApnsOutbox         *scheduler.ApnsOutboxJob
}

type settingsChangeSubscriberInternal interface {
	SubscribeSettingsChanges(keys []string, callback func([]libarcane.SettingUpdate)) func()
}

type settingsEffectsSchedulerInternal interface {
	RescheduleJob(ctx context.Context, job schedulertypes.Job) error
}

type timeoutSettingsEnvironmentInternal interface {
	ListRemoteEnvironments(ctx context.Context) ([]environment.Environment, error)
	ProxyRequest(ctx context.Context, envID, method, path string, body []byte) ([]byte, int, error)
}

func setupSettingsSubscriptionsInternal(params settingsSubscriptionsParams) error {
	unsubscribes := make([]func(), 0, 8)
	subscribe := func(keys []string, callback func([]libarcane.SettingUpdate)) {
		unsubscribes = append(unsubscribes, params.Settings.SubscribeSettingsChanges(keys, callback))
	}
	rescheduleOn := func(keys []string, job schedulertypes.Job) {
		subscribe(keys, func(_ []libarcane.SettingUpdate) {
			if err := params.Scheduler.RescheduleJob(params.LifecycleCtx, job); err != nil {
				slog.WarnContext(params.LifecycleCtx, "Failed to reschedule job", "job", job.Name(), "error", err)
			}
		})
	}
	timeoutSyncExecutor, cancelTimeoutSync := setupTimeoutSettingsSubscriptionInternal(params, subscribe)

	subscribe([]string{"pollingEnabled", "pollingInterval"}, func(_ []libarcane.SettingUpdate) {
		if params.ImageUpdateWatcher != nil {
			params.ImageUpdateWatcher.RefreshSchedule()
			params.ImageUpdateWatcher.Trigger()
		}
		if err := params.Scheduler.RescheduleJob(params.LifecycleCtx, params.AutoUpdate); err != nil {
			slog.WarnContext(params.LifecycleCtx, "Failed to reschedule auto-update job", "error", err)
		}
	})

	subscribe([]string{"apnsEnabled"}, func(updates []libarcane.SettingUpdate) {
		params.Apns.ApplyEnabledUpdates(params.LifecycleCtx, updates, func() error {
			return params.Scheduler.RescheduleJob(params.LifecycleCtx, params.ApnsOutbox)
		})
	})

	rescheduleOn([]string{"autoUpdate", "autoUpdateInterval"}, params.AutoUpdate)

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

	rescheduleOn([]string{"scheduledPruneEnabled", "scheduledPruneInterval"}, params.ScheduledPrune)
	rescheduleOn([]string{
		features.VulnerabilityManagementSettingKey, "vulnerabilityScanEnabled", "vulnerabilityScanInterval", "trivyNetwork", "trivySecurityOpts", "trivyPrivileged",
		"trivyResourceLimitsEnabled", "trivyCpuLimit", "trivyMemoryLimitMb", "trivyConcurrentScanContainers",
	}, params.VulnerabilityScan)
	rescheduleOn([]string{features.VulnerabilityManagementSettingKey}, params.VulnerabilityRisk)
	rescheduleOn([]string{features.VulnerabilityManagementSettingKey, "imageAutoPatchEnabled", "imageAutoPatchInterval"}, params.AutoPatch)
	rescheduleOn([]string{"autoHealEnabled", "autoHealInterval", "autoHealExcludedContainers", "autoHealMaxRestarts", "autoHealRestartWindow"}, params.AutoHeal)

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

type timeoutSyncWorkerInternal struct {
	mu      sync.Mutex
	updates [][]libarcane.SettingUpdate
	wake    chan struct{}
	done    chan struct{}
	ctx     context.Context
	cancel  context.CancelFunc
	closed  bool
}

func (w *timeoutSyncWorkerInternal) Stop(ctx context.Context) error {
	if w == nil {
		return nil
	}
	w.mu.Lock()
	w.closed = true
	w.cancel()
	w.mu.Unlock()
	select {
	case <-w.done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func setupTimeoutSettingsSubscriptionInternal(params settingsSubscriptionsParams, subscribe func([]string, func([]libarcane.SettingUpdate))) (*timeoutSyncWorkerInternal, context.CancelFunc) {
	if params.Config.AgentMode {
		return nil, nil
	}
	ctx, cancel := context.WithCancel(params.LifecycleCtx)
	worker := &timeoutSyncWorkerInternal{wake: make(chan struct{}, 1), done: make(chan struct{}), ctx: ctx, cancel: cancel}
	go func() {
		defer close(worker.done)
		for {
			select {
			case <-ctx.Done():
				return
			case <-worker.wake:
			}
			for {
				worker.mu.Lock()
				if len(worker.updates) == 0 {
					worker.mu.Unlock()
					break
				}
				updates := worker.updates[0]
				worker.updates = worker.updates[1:]
				worker.mu.Unlock()
				syncTimeoutSettingsToAgentsInternal(ctx, params.Environment, updates)
			}
		}
	}()
	subscribe(libarcane.TimeoutSettingKeys(), func(updates []libarcane.SettingUpdate) {
		worker.mu.Lock()
		defer worker.mu.Unlock()
		if worker.closed {
			return
		}
		worker.updates = append(worker.updates, slices.Clone(updates))
		select {
		case worker.wake <- struct{}{}:
		default:
		}
	})
	return worker, cancel
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
