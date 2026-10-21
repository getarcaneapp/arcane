package bootstrap

import (
	"context"
	"encoding/json/v2"
	"errors"
	"fmt"
	"log/slog"
	"maps"
	"net/http"
	"slices"
	"sync"

	"github.com/getarcaneapp/arcane/types/v3"
	"go.uber.org/fx"

	"github.com/getarcaneapp/arcane/backend/v2/internal/activity"
	"github.com/getarcaneapp/arcane/backend/v2/internal/apns"
	"github.com/getarcaneapp/arcane/backend/v2/internal/config"
	"github.com/getarcaneapp/arcane/backend/v2/internal/environment"
	"github.com/getarcaneapp/arcane/backend/v2/internal/gitops"
	"github.com/getarcaneapp/arcane/backend/v2/internal/job"
	"github.com/getarcaneapp/arcane/backend/v2/internal/settings"
	"github.com/getarcaneapp/arcane/backend/v2/internal/system"
	"github.com/getarcaneapp/arcane/backend/v2/internal/volume"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/libarcane"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/scheduler"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/scheduler/flow"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/scheduler/runs"
)

func newJobScheduler(
	appCtx context.Context,
	lc fx.Lifecycle,
	cfg *config.Config,
	runtime *runs.Coordinator,
	_ *runs.Admission,
	imageUpdateWatcher *scheduler.ImageUpdateWatcher,
	analytics *scheduler.AnalyticsJob,
	systemService *system.SystemService,
	jobService *job.JobService,
	workflows *flow.Engine,
	volumes *volume.VolumeService,
	gitopsSync *gitops.GitOpsSyncService,
) (
	types.JobScheduler,
	error,
) {
	schedulerCtx, cancelScheduler := context.WithCancel(appCtx)
	jobScheduler, err := scheduler.NewJobScheduler(schedulerCtx, jobService.Coordinator(), cfg.GetLocation())
	if err != nil {
		cancelScheduler()
		return nil, err
	}
	jobService.SetScheduler(schedulerCtx, jobScheduler)
	var resumeDone chan struct{}
	lc.Append(fx.Hook{
		OnStart: func(ctx context.Context) error {
			started := false
			defer func() {
				if !started {
					cancelScheduler()
				}
			}()
			slog.InfoContext(appCtx, "Starting scheduler")
			if startErr := jobService.Coordinator().Start(ctx, schedulerCtx); startErr != nil {
				return fmt.Errorf("initialize scheduler: %w", startErr)
			}
			if reconcileCoordinatorStartupErr := reconcileCoordinatorStartup(ctx, jobService, workflows, volumes, systemService, gitopsSync); reconcileCoordinatorStartupErr != nil {
				return fmt.Errorf("reconcile scheduler startup: %w", reconcileCoordinatorStartupErr)
			}
			if gitopsSync != nil {
				gitopsSync.RegisterAutoSyncJobsOnStartup(ctx)
				gitopsSync.SubscribeProjectFileChanges(schedulerCtx)
			}
			if imageUpdateWatcher != nil {
				imageUpdateWatcher.SetCoordinator(jobService.Coordinator())
				if registerBusWatcherErr := jobScheduler.RegisterBusWatcher(imageUpdateWatcher, true); registerBusWatcherErr != nil {
					return registerBusWatcherErr
				}
			}
			if startSchedulerErr := jobScheduler.StartScheduler(ctx); startSchedulerErr != nil {
				return fmt.Errorf("start scheduler: %w", startSchedulerErr)
			}
			jobService.Coordinator().Activate()
			if analytics != nil {
				if _, submitErr := jobService.Coordinator().Submit(ctx, types.SchedulerRequest{JobID: analytics.Name(), EnvironmentID: "0", Trigger: "startup"}); submitErr != nil {
					return fmt.Errorf("submit startup job %q: %w", analytics.Name(), submitErr)
				}
			}
			// Standalone tasks wait until update-all recovery has read its job, so a resumed step's new checkpoint
			// is never mistaken for one the previous process left.
			if !cfg.AgentMode && systemService != nil {
				resumeDone = make(chan struct{})
				go func() {
					defer close(resumeDone)
					systemService.ResumeUpdateAllOnStartup(schedulerCtx)
					workflows.Ready()
				}()
			} else {
				workflows.Ready()
			}
			if startupCancellationErr := ctx.Err(); startupCancellationErr != nil {
				return fmt.Errorf("complete scheduler startup: %w", startupCancellationErr)
			}
			started = true
			return nil
		},
		OnStop: func(ctx context.Context) error {
			cancelScheduler()
			resumeErr := func() error {
				if resumeDone == nil {
					return nil
				}
				select {
				case <-resumeDone:
					return nil
				case <-ctx.Done():
					return ctx.Err()
				}
			}()
			if stopErr := errors.Join(resumeErr, jobService.Coordinator().Stop(ctx), jobScheduler.Stop(ctx)); stopErr != nil {
				slog.ErrorContext(ctx, "Job scheduler exited with error", "error", stopErr)
				return stopErr
			}
			slog.InfoContext(ctx, "Scheduler stopped")
			return nil
		},
	})
	return jobScheduler, nil
}

// reconcileCoordinatorStartup fails backups and activities the previous process left running,
// except those that unfinished runs and standalone workflows still own.
func reconcileCoordinatorStartup(
	ctx context.Context,
	jobs *job.JobService,
	workflows *flow.Engine,
	volumes *volume.VolumeService,
	systemService *system.SystemService,
	syncService *gitops.GitOpsSyncService,
) error {
	records, err := jobs.Coordinator().Records(ctx)
	if err != nil {
		return err
	}
	standalone, err := workflows.Standalone(ctx)
	if err != nil {
		return err
	}
	activities, backups := []string{}, []string{}
	for _, record := range append(records, types.SchedulerQueueRecord{Runs: standalone}) {
		for _, run := range record.Runs {
			if run.Status.Terminal() {
				continue
			}
			for _, target := range run.Outcome.Targets {
				var checkpoint struct {
					BackupID string `json:"backupId"`
				}
				if len(target.RecoveryData) == 0 {
					continue
				}
				if decodeErr := json.Unmarshal(target.RecoveryData, &checkpoint); decodeErr != nil {
					return fmt.Errorf("decode startup recovery evidence: %w", decodeErr)
				}
				if checkpoint.BackupID != "" {
					backups = append(backups, checkpoint.BackupID)
				}
			}
		}
	}
	for _, run := range standalone {
		if run.ActivityID != "" {
			activities = append(activities, run.ActivityID)
		}
	}
	if volumes != nil {
		if reconcileInterruptedBackupsErr := volumes.ReconcileInterruptedBackups(ctx, backups...); reconcileInterruptedBackupsErr != nil {
			return reconcileInterruptedBackupsErr
		}
	}
	if systemService != nil {
		if reconcileSystemBackupsErr := systemService.ReconcileInterruptedBackups(ctx, backups...); reconcileSystemBackupsErr != nil {
			return reconcileSystemBackupsErr
		}
	}
	if syncService != nil {
		if reconcileInterruptedBackupsOnStartupErr := syncService.ReconcileInterruptedBackupsOnStartup(ctx, backups...); reconcileInterruptedBackupsOnStartupErr != nil {
			return reconcileInterruptedBackupsOnStartupErr
		}
	}
	return jobs.ReconcileStartupActivities(ctx, activities...)
}

type registerJobsParams struct {
	fx.In

	Lifecycle fx.Lifecycle
	AppCtx    context.Context
	Config    *config.Config
	Scheduler types.JobScheduler

	Activity    *activity.ActivityService
	GitOpsSync  *gitops.GitOpsSyncService
	Environment *environment.EnvironmentService
	JobSchedule *job.JobService
	Settings    *settings.SettingsService
	Volume      *volume.VolumeService
	System      *system.SystemService
	Admission   *runs.Admission
	Apns        *apns.ApnsService

	AutoUpdate             *flow.Job `name:"auto-update"`
	ImageUpdateWatcher     *scheduler.ImageUpdateWatcher
	DockerClientRefresh    *scheduler.DockerClientRefreshJob
	Analytics              *scheduler.AnalyticsJob
	UpgradeLogCleanup      types.SchedulerJob `name:"upgrade-log-cleanup"`
	EventCleanup           *scheduler.EventCleanupJob
	PruningVolumeHelper    *scheduler.PruningVolumeHelperJob
	ExpiredSessionsCleanup *scheduler.ExpiredSessionsCleanupJob
	ScheduledPrune         *scheduler.ScheduledPruneJob
	FilesystemWatcher      *scheduler.FilesystemWatcherJob
	VulnerabilityScan      *flow.Job `name:"vulnerability-scan"`
	VulnerabilityRisk      *scheduler.VulnerabilityRiskJob
	AutoPatch              *flow.Job `name:"auto-patch"`
	AutoHeal               *scheduler.AutoHealJob
	ActivitySweep          *scheduler.ActivitySweepJob
	UploadSessionsCleanup  *scheduler.UploadSessionsCleanupJob
	GitCloneCleanup        *scheduler.GitCloneCleanupJob
	BackupRepositoryPrune  *scheduler.BackupRepositoryPruneJob
	ApnsOutbox             *scheduler.ApnsOutboxJob
}

func registerJobs(params registerJobsParams) error {
	params.JobSchedule.SetScheduler(params.AppCtx, params.Scheduler)

	for _, job := range []types.SchedulerJob{
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
		params.BackupRepositoryPrune,
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
		AppCtx:      params.AppCtx,
		Config:      params.Config,
		Scheduler:   params.Scheduler,
		GitOpsSync:  params.GitOpsSync,
		Environment: params.Environment,
		JobSchedule: params.JobSchedule,
		Volume:      params.Volume,
		System:      params.System,
		Admission:   params.Admission,
	}); err != nil {
		return err
	}

	if err := setupSettingsSubscriptions(settingsSubscriptionsParams{
		Lifecycle:          params.Lifecycle,
		LifecycleCtx:       params.AppCtx,
		Config:             params.Config,
		Scheduler:          params.Scheduler,
		Settings:           params.Settings,
		Environment:        params.Environment,
		JobSchedule:        params.JobSchedule,
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
	AppCtx      context.Context
	Config      *config.Config
	Scheduler   types.JobScheduler
	GitOpsSync  *gitops.GitOpsSyncService
	Environment *environment.EnvironmentService
	JobSchedule *job.JobService
	Volume      *volume.VolumeService
	System      *system.SystemService
	Admission   *runs.Admission
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
	if !params.Config.AgentMode && params.System != nil {
		if err := params.System.SetBackupScheduler(params.AppCtx, params.Scheduler, params.Admission); err != nil {
			return err
		}
		params.System.RegisterBackupJobOnStartup(params.AppCtx)
	}
	// GitOps startup submissions wait for Francis in the scheduler start hook.
	if params.GitOpsSync != nil {
		if err := params.GitOpsSync.SetScheduler(params.AppCtx, params.Scheduler, params.Admission); err != nil {
			return err
		}
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
		params.Environment.RescheduleHealthJobs(params.AppCtx)
	}
	return nil
}

type settingsSubscriptionsParams struct {
	Lifecycle    fx.Lifecycle
	LifecycleCtx context.Context
	Config       *config.Config
	Scheduler    settingsEffectsScheduler
	Settings     settingsChangeSubscriber
	Environment  timeoutSettingsEnvironment
	JobSchedule  *job.JobService

	AutoUpdate         *flow.Job
	ImageUpdateWatcher *scheduler.ImageUpdateWatcher
	FilesystemWatcher  *scheduler.FilesystemWatcherJob
	ScheduledPrune     *scheduler.ScheduledPruneJob
	VulnerabilityScan  *flow.Job
	VulnerabilityRisk  *scheduler.VulnerabilityRiskJob
	AutoPatch          *flow.Job
	AutoHeal           *scheduler.AutoHealJob
	Apns               *apns.ApnsService
	ApnsOutbox         *scheduler.ApnsOutboxJob
}

type settingsChangeSubscriber interface {
	SubscribeSettingsChanges(keys []string, callback func([]libarcane.SettingUpdate)) func()
}

type settingsEffectsScheduler interface {
	RescheduleJob(ctx context.Context, job types.SchedulerJob) error
}

type timeoutSettingsEnvironment interface {
	ListRemoteEnvironments(ctx context.Context) ([]environment.Environment, error)
	ProxyRequest(ctx context.Context, envID, method, path string, body []byte) ([]byte, int, error)
}

func setupSettingsSubscriptions(params settingsSubscriptionsParams) error {
	unsubscribes := make([]func(), 0, 8)
	subscribe := func(keys []string, callback func([]libarcane.SettingUpdate)) {
		unsubscribes = append(unsubscribes, params.Settings.SubscribeSettingsChanges(keys, callback))
	}
	rescheduleOn := func(keys []string, job types.SchedulerJob) {
		subscribe(keys, func(_ []libarcane.SettingUpdate) {
			if err := params.Scheduler.RescheduleJob(params.LifecycleCtx, job); err != nil {
				slog.WarnContext(params.LifecycleCtx, "Failed to reschedule job", "job", job.Name(), "error", err)
			}
		})
	}
	timeoutSyncExecutor, cancelTimeoutSync := setupTimeoutSettingsSubscription(params, subscribe)

	subscribe([]string{"pollingEnabled", "pollingInterval"}, func(_ []libarcane.SettingUpdate) {
		params.ImageUpdateWatcher.RefreshSchedule()
		params.ImageUpdateWatcher.Trigger()
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

	subscribe([]string{"dockerClientRefreshInterval", "environmentHealthInterval"}, func(updates []libarcane.SettingUpdate) {
		changedKeys := make([]string, 0, len(updates))
		for _, update := range updates {
			changedKeys = append(changedKeys, update.Key)
		}
		if err := params.JobSchedule.RescheduleJobsForSettingKeys(params.LifecycleCtx, changedKeys); err != nil {
			slog.WarnContext(params.LifecycleCtx, "Failed to reschedule jobs after settings change", "keys", changedKeys, "error", err)
		}
	})

	subscribe([]string{"projectsDirectory", "followProjectSymlinks"}, func(_ []libarcane.SettingUpdate) {
		if err := params.FilesystemWatcher.RestartProjectsWatcher(params.LifecycleCtx); err != nil {
			slog.WarnContext(params.LifecycleCtx, "Failed to restart projects filesystem watcher", "error", err)
			params.FilesystemWatcher.RequestRestart()
		}
	})

	subscribe([]string{"templatesDirectory"}, func(_ []libarcane.SettingUpdate) {
		if err := params.FilesystemWatcher.RestartTemplatesWatcher(params.LifecycleCtx); err != nil {
			slog.WarnContext(params.LifecycleCtx, "Failed to restart templates filesystem watcher", "error", err)
			params.FilesystemWatcher.RequestRestart()
		}
	})

	rescheduleOn([]string{"scheduledPruneEnabled", "scheduledPruneInterval"}, params.ScheduledPrune)
	rescheduleOn([]string{
		types.FeatureVulnerabilityManagementSettingKey, "vulnerabilityScanEnabled", "vulnerabilityScanInterval", "trivyNetwork", "trivySecurityOpts", "trivyPrivileged",
		"trivyResourceLimitsEnabled", "trivyCpuLimit", "trivyMemoryLimitMb", "trivyConcurrentScanContainers",
	}, params.VulnerabilityScan)
	rescheduleOn([]string{types.FeatureVulnerabilityManagementSettingKey}, params.VulnerabilityRisk)
	rescheduleOn([]string{types.FeatureVulnerabilityManagementSettingKey, "imageAutoPatchEnabled", "imageAutoPatchInterval"}, params.AutoPatch)
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

type timeoutSyncWorker struct {
	mu      sync.Mutex
	updates map[string]string
	wake    chan struct{}
	done    chan struct{}
	ctx     context.Context
	cancel  context.CancelFunc
	closed  bool
}

func (w *timeoutSyncWorker) Stop(ctx context.Context) error {
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

func setupTimeoutSettingsSubscription(params settingsSubscriptionsParams, subscribe func([]string, func([]libarcane.SettingUpdate))) (*timeoutSyncWorker, context.CancelFunc) {
	if params.Config.AgentMode {
		return nil, nil
	}
	ctx, cancel := context.WithCancel(params.LifecycleCtx)
	worker := &timeoutSyncWorker{wake: make(chan struct{}, 1), done: make(chan struct{}), ctx: ctx, cancel: cancel}
	go func() {
		defer close(worker.done)
		for {
			select {
			case <-ctx.Done():
				return
			case <-worker.wake:
			}
			worker.mu.Lock()
			updates := worker.updates
			worker.updates = nil
			worker.mu.Unlock()
			if len(updates) > 0 {
				syncTimeoutSettingsToAgents(ctx, params.Environment, updates)
			}
		}
	}()
	subscribe(libarcane.TimeoutSettingKeys(), func(updates []libarcane.SettingUpdate) {
		worker.mu.Lock()
		defer worker.mu.Unlock()
		if worker.closed {
			return
		}
		if worker.updates == nil {
			worker.updates = make(map[string]string, len(updates))
		}
		for _, update := range updates {
			worker.updates[update.Key] = update.Value
		}
		select {
		case worker.wake <- struct{}{}:
		default:
		}
	})
	return worker, cancel
}

// syncTimeoutSettingsToAgents syncs timeout settings to all connected remote environments
func syncTimeoutSettingsToAgents(ctx context.Context, localEnvironment timeoutSettingsEnvironment, settingsMap map[string]string) {
	envs, err := localEnvironment.ListRemoteEnvironments(ctx)
	if err != nil {
		slog.WarnContext(ctx, "Failed to list remote environments for timeout sync", "error", err)
		return
	}

	if len(envs) == 0 {
		return
	}

	body, err := json.Marshal(settingsMap)
	if err != nil {
		slog.WarnContext(ctx, "Failed to marshal timeout settings for sync", "error", err)
		return
	}

	slog.InfoContext(ctx, "Syncing environment settings to remote environments", "count", len(envs), "keys", slices.Sorted(maps.Keys(settingsMap)))

	for _, env := range envs {
		responseBody, statusCode, proxyRequestErr := localEnvironment.ProxyRequest(ctx, env.ID, http.MethodPut, "/api/environments/0/settings", body)
		if proxyRequestErr != nil {
			slog.WarnContext(ctx, "Failed to sync timeout settings to environment", "environmentId", env.ID, "environmentName", env.Name, "error", proxyRequestErr)
			continue
		}
		if statusCode < http.StatusOK || statusCode >= http.StatusMultipleChoices {
			slog.WarnContext(ctx, "Environment returned non-OK status for timeout sync", "environmentId", env.ID, "environmentName", env.Name, "statusCode", statusCode, "response", string(responseBody))
			continue
		}
		slog.DebugContext(ctx, "Successfully synced timeout settings to environment", "environmentId", env.ID, "environmentName", env.Name)
	}
}
