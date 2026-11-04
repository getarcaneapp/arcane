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
	"time"

	"go.uber.org/fx"
	"golang.org/x/sync/errgroup"

	"getarcane.dev/arcane/v3/internal/activity"
	"getarcane.dev/arcane/v3/internal/apns"
	"getarcane.dev/arcane/v3/internal/config"
	"getarcane.dev/arcane/v3/internal/environment"
	"getarcane.dev/arcane/v3/internal/gitops"
	"getarcane.dev/arcane/v3/internal/job"
	"getarcane.dev/arcane/v3/internal/scheduler"
	"getarcane.dev/arcane/v3/internal/scheduler/flow"
	"getarcane.dev/arcane/v3/internal/scheduler/runs"
	"getarcane.dev/arcane/v3/internal/settings"
	"getarcane.dev/arcane/v3/internal/system"
	"getarcane.dev/arcane/v3/internal/volume"
	"getarcane.dev/arcane/v3/pkg/httpx"
	"getarcane.dev/types/v3"
)

type (
	registerJobsParams struct {
		fx.In

		Lifecycle fx.Lifecycle
		AppCtx    context.Context
		Config    *config.Config
		Scheduler types.JobScheduler

		Activity    *activity.ActivityService
		Environment *environment.EnvironmentService
		JobSchedule *job.JobService
		Settings    *settings.SettingsService
		Volume      *volume.VolumeService
		System      *system.SystemService
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

	dynamicJobsParams struct {
		AppCtx      context.Context
		Config      *config.Config
		Environment *environment.EnvironmentService
		JobSchedule *job.JobService
		Volume      *volume.VolumeService
		System      *system.SystemService
	}

	startJobSchedulerParams struct {
		fx.In

		Lifecycle          fx.Lifecycle
		AppCtx             context.Context
		Config             *config.Config
		Scheduler          types.JobScheduler
		Coordinator        *runs.Coordinator
		Workflows          *flow.Engine
		ImageUpdateWatcher *scheduler.ImageUpdateWatcher
		Analytics          *scheduler.AnalyticsJob
		JobSchedule        *job.JobService
		Volume             *volume.VolumeService
		System             *system.SystemService
		GitOpsSync         *gitops.GitOpsSyncService
	}

	settingsSubscriptionsParams struct {
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

	settingsChangeSubscriber interface {
		SubscribeSettingsChanges(keys []string, callback func([]types.SettingUpdate)) func()
	}

	settingsEffectsScheduler interface {
		AddJob(ctx context.Context, job types.SchedulerJob) error
	}

	timeoutSettingsEnvironment interface {
		ListRemoteEnvironments(ctx context.Context) ([]environment.Environment, error)
		ProxyRequest(ctx context.Context, envID, method, path string, body []byte) ([]byte, int, error)
	}

	timeoutSyncWorker struct {
		ctx     context.Context
		updates map[string]string
		wake    chan struct{}
		done    chan struct{}
		cancel  context.CancelFunc
		mu      sync.Mutex
		closed  bool
	}
)

func startJobScheduler(p startJobSchedulerParams) {
	schedulerCtx, cancelScheduler := context.WithCancel(p.AppCtx)
	var resumeDone chan struct{}
	p.Lifecycle.Append(fx.Hook{
		OnStart: func(ctx context.Context) error {
			started := false
			defer func() {
				if !started {
					cancelScheduler()
				}
			}()
			slog.InfoContext(p.AppCtx, "Starting scheduler", "timezone", p.Config.TimezoneName(), "utcOffset", time.Now().In(p.Config.GetLocation()).Format("-07:00"))
			if startErr := p.Coordinator.Start(ctx, schedulerCtx); startErr != nil {
				return fmt.Errorf("initialize scheduler: %w", startErr)
			}
			if reconcileCoordinatorStartupErr := reconcileCoordinatorStartup(ctx, p); reconcileCoordinatorStartupErr != nil {
				return fmt.Errorf("reconcile scheduler startup: %w", reconcileCoordinatorStartupErr)
			}
			p.GitOpsSync.RegisterAutoSyncJobsOnStartup(ctx)
			p.GitOpsSync.SubscribeProjectFileChanges()
			if registerBusWatcherErr := p.Scheduler.RegisterBusWatcher(p.ImageUpdateWatcher, true); registerBusWatcherErr != nil {
				return registerBusWatcherErr
			}
			if startSchedulerErr := p.Scheduler.StartScheduler(ctx); startSchedulerErr != nil {
				return fmt.Errorf("start scheduler: %w", startSchedulerErr)
			}
			p.Coordinator.Activate()
			if _, submitErr := p.Coordinator.Submit(ctx, types.SchedulerRequest{JobID: p.Analytics.Name(), EnvironmentID: "0", Trigger: "startup"}); submitErr != nil {
				return fmt.Errorf("submit startup job %q: %w", p.Analytics.Name(), submitErr)
			}
			// Standalone tasks wait until upgrade job recovery has read its job, so a resumed step's new checkpoint
			// is never mistaken for one the previous process left.
			if !p.Config.AgentEnabled {
				resumeDone = make(chan struct{})
				go func() {
					defer close(resumeDone)
					p.System.ResumeUpgradeJobOnStartup(schedulerCtx)
					p.Workflows.Ready()
				}()
			} else {
				p.Workflows.Ready()
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
			if stopErr := errors.Join(resumeErr, p.Coordinator.Stop(ctx), p.Scheduler.Stop(ctx)); stopErr != nil {
				slog.ErrorContext(ctx, "Job scheduler exited with error", "error", stopErr)
				return stopErr
			}
			slog.InfoContext(ctx, "Scheduler stopped")
			return nil
		},
	})
}

// reconcileCoordinatorStartup fails backups and activities the previous process left running,
// except those that unfinished runs and standalone workflows still own.
func reconcileCoordinatorStartup(ctx context.Context, p startJobSchedulerParams) error {
	records, err := p.Coordinator.Records(ctx)
	if err != nil {
		return err
	}
	standalone, err := p.Workflows.Standalone(ctx)
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
	if reconcileInterruptedBackupsErr := p.Volume.ReconcileInterruptedBackups(ctx, backups...); reconcileInterruptedBackupsErr != nil {
		return reconcileInterruptedBackupsErr
	}
	if reconcileSystemBackupsErr := p.System.ReconcileInterruptedBackups(ctx, backups...); reconcileSystemBackupsErr != nil {
		return reconcileSystemBackupsErr
	}
	if reconcileInterruptedBackupsOnStartupErr := p.GitOpsSync.ReconcileInterruptedBackupsOnStartup(ctx, backups...); reconcileInterruptedBackupsOnStartupErr != nil {
		return reconcileInterruptedBackupsOnStartupErr
	}
	return p.JobSchedule.ReconcileStartupActivities(ctx, activities...)
}

func registerJobs(params registerJobsParams) error {
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
	registerDynamicJobs(dynamicJobsParams{
		AppCtx:      params.AppCtx,
		Config:      params.Config,
		Environment: params.Environment,
		JobSchedule: params.JobSchedule,
		Volume:      params.Volume,
		System:      params.System,
	})

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

func registerDynamicJobs(params dynamicJobsParams) {
	// Volume backups run on the environment that owns the Docker volume. This is
	// registered on managers and agents; environment proxying persists each policy
	// in the correct Arcane database.
	params.Volume.RegisterBackupJobsOnStartup(params.AppCtx)
	if !params.Config.AgentEnabled {
		params.System.RegisterBackupJobOnStartup(params.AppCtx)
	}

	// Environment health: one job per enabled environment (manager only). The Jobs
	// UI still addresses "environment-health" by ID, so bridge its reschedule and
	// run-now back to EnvironmentService.
	if !params.Config.AgentEnabled {
		params.JobSchedule.OnEnvironmentHealthReschedule = func(ctx context.Context) {
			params.Environment.RescheduleHealthJobs(ctx)
		}
		params.JobSchedule.RunEnvironmentHealthNow = func(ctx context.Context) error {
			return params.Environment.RunHealthChecksNow(ctx)
		}
		params.Environment.RescheduleHealthJobs(params.AppCtx)
	}
}

func setupSettingsSubscriptions(params settingsSubscriptionsParams) error {
	unsubscribes := make([]func(), 0, 8)
	subscribe := func(keys []string, callback func([]types.SettingUpdate)) {
		unsubscribes = append(unsubscribes, params.Settings.SubscribeSettingsChanges(keys, callback))
	}
	rescheduleOn := func(keys []string, job types.SchedulerJob) {
		subscribe(keys, func(_ []types.SettingUpdate) {
			if err := params.Scheduler.AddJob(params.LifecycleCtx, job); err != nil {
				slog.WarnContext(params.LifecycleCtx, "Failed to reschedule job", "job", job.Name(), "error", err)
			}
		})
	}
	timeoutSyncExecutor, cancelTimeoutSync := setupTimeoutSettingsSubscription(params, subscribe)

	subscribe([]string{"pollingEnabled", "pollingInterval"}, func(_ []types.SettingUpdate) {
		params.ImageUpdateWatcher.RefreshSchedule()
		params.ImageUpdateWatcher.Trigger()
		if err := params.Scheduler.AddJob(params.LifecycleCtx, params.AutoUpdate); err != nil {
			slog.WarnContext(params.LifecycleCtx, "Failed to reschedule auto-update job", "error", err)
		}
	})

	subscribe([]string{"apnsEnabled"}, func(updates []types.SettingUpdate) {
		params.Apns.ApplyEnabledUpdates(params.LifecycleCtx, updates, func() error {
			return params.Scheduler.AddJob(params.LifecycleCtx, params.ApnsOutbox)
		})
	})

	rescheduleOn([]string{"autoUpdate", "autoUpdateInterval"}, params.AutoUpdate)

	subscribe([]string{"dockerClientRefreshInterval", "environmentHealthInterval"}, func(updates []types.SettingUpdate) {
		changedKeys := make([]string, 0, len(updates))
		for _, update := range updates {
			changedKeys = append(changedKeys, update.Key)
		}
		if err := params.JobSchedule.RescheduleJobsForSettingKeys(params.LifecycleCtx, changedKeys); err != nil {
			slog.WarnContext(params.LifecycleCtx, "Failed to reschedule jobs after settings change", "keys", changedKeys, "error", err)
		}
	})

	subscribe([]string{"projectsDirectory", "followProjectSymlinks", "projectScanMaxDepth"}, func(_ []types.SettingUpdate) {
		if err := params.FilesystemWatcher.RestartProjectsWatcher(params.LifecycleCtx); err != nil {
			slog.WarnContext(params.LifecycleCtx, "Failed to restart projects filesystem watcher", "error", err)
			params.FilesystemWatcher.RequestRestart()
		}
	})

	subscribe([]string{"templatesDirectory"}, func(_ []types.SettingUpdate) {
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

func setupTimeoutSettingsSubscription(params settingsSubscriptionsParams, subscribe func([]string, func([]types.SettingUpdate))) (*timeoutSyncWorker, context.CancelFunc) {
	if params.Config.AgentEnabled {
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
	subscribe(settings.PropagatedKeys(), func(updates []types.SettingUpdate) {
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

	slog.InfoContext(ctx, "Syncing environment settings to remote environments", "count", len(envs), "keys", slices.Sorted(maps.Keys(settingsMap)))

	var group errgroup.Group
	group.SetLimit(httpx.EnvironmentConcurrency)
	for _, env := range envs {
		group.Go(func() error {
			snapshotBody, statusCode, proxyRequestErr := localEnvironment.ProxyRequest(ctx, env.ID, http.MethodGet, "/api/environments/0/settings", nil)
			if proxyRequestErr != nil {
				slog.WarnContext(ctx, "Failed to read environment settings for timeout sync", "environmentId", env.ID, "environmentName", env.Name, "error", proxyRequestErr)
				return nil
			}
			if statusCode < http.StatusOK || statusCode >= http.StatusMultipleChoices {
				slog.WarnContext(ctx, "Environment returned non-OK status for settings read", "environmentId", env.ID, "environmentName", env.Name, "statusCode", statusCode, "response", string(snapshotBody))
				return nil
			}
			var snapshot types.SettingsSnapshot
			if decodeErr := json.Unmarshal(snapshotBody, &snapshot); decodeErr != nil || snapshot.Settings == nil {
				slog.ErrorContext(ctx, "agent version mismatch: environment did not return a settings snapshot; skipping settings sync",
					"environmentId", env.ID, "environmentName", env.Name, "error", decodeErr)
				return nil
			}

			update := maps.Clone(settingsMap)
			for _, setting := range snapshot.Settings {
				if setting.Locked {
					delete(update, setting.Key)
				}
			}
			if len(update) == 0 {
				slog.DebugContext(ctx, "All synced settings are locked on environment", "environmentId", env.ID, "environmentName", env.Name)
				return nil
			}

			body, marshalErr := json.Marshal(types.SettingsUpdate(update))
			if marshalErr != nil {
				slog.WarnContext(ctx, "Failed to marshal timeout settings for sync", "environmentId", env.ID, "error", marshalErr)
				return nil
			}
			responseBody, statusCode, proxyRequestErr := localEnvironment.ProxyRequest(ctx, env.ID, http.MethodPatch, "/api/environments/0/settings", body)
			if proxyRequestErr != nil {
				slog.WarnContext(ctx, "Failed to sync timeout settings to environment", "environmentId", env.ID, "environmentName", env.Name, "error", proxyRequestErr)
				return nil
			}
			if statusCode < http.StatusOK || statusCode >= http.StatusMultipleChoices {
				slog.WarnContext(ctx, "Environment returned non-OK status for timeout sync", "environmentId", env.ID, "environmentName", env.Name, "statusCode", statusCode, "response", string(responseBody))
				return nil
			}
			slog.DebugContext(ctx, "Successfully synced timeout settings to environment", "environmentId", env.ID, "environmentName", env.Name)
			return nil
		})
	}
	_ = group.Wait()
}
