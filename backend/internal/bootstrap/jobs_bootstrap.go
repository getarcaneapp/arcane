package bootstrap

import (
	"context"
	"encoding/json/v2"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"time"

	"go.uber.org/fx"

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
				slog.WarnContext(ctx, "Failed to submit startup job", "job", p.Analytics.Name(), "error", submitErr)
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
	params.Volume.RegisterBackupJobsOnStartup(params.AppCtx)
	if !params.Config.AgentEnabled {
		params.System.RegisterBackupJobOnStartup(params.AppCtx)
	}

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

	subscribe([]string{"projectsDirectory", "projectsDirectories", "followProjectSymlinks", "projectScanMaxDepth"}, func(_ []types.SettingUpdate) {
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
		OnStop: func(context.Context) error {
			for _, unsubscribe := range slices.Backward(unsubscribes) {
				unsubscribe()
			}
			return nil
		},
	})
	return nil
}
