package services

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sort"
	"time"

	"github.com/getarcaneapp/arcane/backend/v2/internal/config"
	"github.com/getarcaneapp/arcane/backend/v2/internal/database"
	"github.com/getarcaneapp/arcane/backend/v2/internal/models"
	"github.com/getarcaneapp/arcane/types/v2/jobschedule"
	"github.com/getarcaneapp/arcane/types/v2/meta"
	schedulertypes "github.com/getarcaneapp/arcane/types/v2/scheduler"
	"github.com/robfig/cron/v3"
	"gorm.io/gorm"
)

type JobRunner interface {
	GetJob(jobID string) (schedulertypes.Job, bool)
	GetJobRuntimeState(jobID string) (schedulertypes.JobRuntimeState, bool)
	RescheduleJob(ctx context.Context, job schedulertypes.Job) error
	RunBusWatcherNow(ctx context.Context, watcherID string) error
}

// JobService manages configuration for background job schedules.
//
// Intervals are persisted in the existing settings table as individual keys.
// After updates, the SettingsService cache is reloaded and active jobs are
// rescheduled through the configured scheduler.
//
// NOTE: This is intentionally separate from SettingsService to keep the API
// surface job-focused and to centralize schedule validation/rescheduling.
type JobService struct {
	db           *database.DB
	settings     *SettingsService
	cfg          *config.Config
	scheduler    JobRunner
	lifecycleCtx context.Context
	location     *time.Location // Timezone for cron schedule calculations

	// environment-health is no longer a single scheduler job — it fans out to one
	// dynamic job per environment owned by EnvironmentService. These bridge the Jobs
	// UI (which addresses jobs by ID) back to that service. Set during bootstrap on
	// the manager only.
	OnEnvironmentHealthReschedule func(ctx context.Context)
	RunEnvironmentHealthNow       func(ctx context.Context) error
}

func NewJobService(db *database.DB, settings *SettingsService, cfg *config.Config) *JobService {
	return &JobService{
		db:       db,
		settings: settings,
		cfg:      cfg,
		location: cfg.GetLocation(),
	}
}

func (s *JobService) SetScheduler(ctx context.Context, scheduler JobRunner) { //nolint:contextcheck // scheduler jobs must capture the app lifecycle context, not request contexts
	if ctx == nil {
		ctx = context.Background()
	}
	s.lifecycleCtx = ctx
	s.scheduler = scheduler
}

func (s *JobService) GetJobSchedules(ctx context.Context) jobschedule.Config {
	defaults := DefaultSettingsConfig()

	// Use SettingsService cache for fast reads.
	return jobschedule.Config{
		EnvironmentHealthInterval:      s.settings.GetStringSetting(ctx, "environmentHealthInterval", defaults.EnvironmentHealthInterval.Value),
		EventCleanupInterval:           s.settings.GetStringSetting(ctx, "eventCleanupInterval", defaults.EventCleanupInterval.Value),
		ExpiredSessionsCleanupInterval: s.settings.GetStringSetting(ctx, "expiredSessionsCleanupInterval", defaults.ExpiredSessionsCleanupInterval.Value),
		AutoUpdateInterval:             s.settings.GetStringSetting(ctx, "autoUpdateInterval", defaults.AutoUpdateInterval.Value),
		DockerClientRefreshInterval:    s.settings.GetStringSetting(ctx, "dockerClientRefreshInterval", defaults.DockerClientRefreshInterval.Value),
		PollingInterval:                s.settings.GetStringSetting(ctx, "pollingInterval", defaults.PollingInterval.Value),
		ScheduledPruneInterval:         s.settings.GetStringSetting(ctx, "scheduledPruneInterval", defaults.ScheduledPruneInterval.Value),
		VulnerabilityScanInterval:      s.settings.GetStringSetting(ctx, "vulnerabilityScanInterval", defaults.VulnerabilityScanInterval.Value),
		AutoHealInterval:               s.settings.GetStringSetting(ctx, "autoHealInterval", defaults.AutoHealInterval.Value),
	}
}

func (s *JobService) UpdateJobSchedules(ctx context.Context, updates jobschedule.Update) (jobschedule.Config, error) {
	if s == nil || s.db == nil || s.settings == nil {
		return jobschedule.Config{}, errors.New("job service not initialized")
	}
	if s.cfg != nil && s.cfg.UIConfigurationDisabled {
		return jobschedule.Config{}, errors.New("job schedule updates are disabled")
	}

	current := s.GetJobSchedules(ctx)

	fields := []struct {
		key     string
		current string
		update  *string
	}{
		{key: "environmentHealthInterval", current: current.EnvironmentHealthInterval, update: updates.EnvironmentHealthInterval},
		{key: "eventCleanupInterval", current: current.EventCleanupInterval, update: updates.EventCleanupInterval},
		{key: "expiredSessionsCleanupInterval", current: current.ExpiredSessionsCleanupInterval, update: updates.ExpiredSessionsCleanupInterval},
		{key: "autoUpdateInterval", current: current.AutoUpdateInterval, update: updates.AutoUpdateInterval},
		{key: "dockerClientRefreshInterval", current: current.DockerClientRefreshInterval, update: updates.DockerClientRefreshInterval},
		{key: "pollingInterval", current: current.PollingInterval, update: updates.PollingInterval},
		{key: "scheduledPruneInterval", current: current.ScheduledPruneInterval, update: updates.ScheduledPruneInterval},
		{key: "vulnerabilityScanInterval", current: current.VulnerabilityScanInterval, update: updates.VulnerabilityScanInterval},
		{key: "autoHealInterval", current: current.AutoHealInterval, update: updates.AutoHealInterval},
	}

	// Validate inputs (cron expressions)
	parser := cron.NewParser(cron.Second | cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow)
	for _, field := range fields {
		if field.update == nil || *field.update == "" {
			continue
		}
		if _, err := parser.Parse(*field.update); err != nil {
			return jobschedule.Config{}, fmt.Errorf("invalid cron expression for %s: %w", field.key, err)
		}
	}

	changed := false
	changedKeys := make([]string, 0, len(fields))
	previousValues := make(map[string]string, len(fields))
	upsert := func(tx *gorm.DB, key string, v *string, currentVal string) error {
		if v == nil {
			return nil
		}
		if *v == currentVal {
			return nil
		}
		changed = true
		changedKeys = append(changedKeys, key)
		previousValues[key] = currentVal
		return tx.Save(&models.SettingVariable{Key: key, Value: *v}).Error
	}

	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		for _, field := range fields {
			if err := upsert(tx, field.key, field.update, field.current); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return jobschedule.Config{}, fmt.Errorf("failed to update job schedules: %w", err)
	}

	// Refresh settings cache so jobs reading from SettingsService see new values.
	if changed {
		if err := s.settings.LoadDatabaseSettings(ctx); err != nil {
			restoreErr := s.restoreJobSchedulesInternal(ctx, previousValues, changedKeys)
			return jobschedule.Config{}, errors.Join(
				fmt.Errorf("failed to reload settings after job schedule update: %w", err),
				restoreErr,
			)
		}

		if err := s.RescheduleJobsForSettingKeys(ctx, changedKeys); err != nil {
			restoreErr := s.restoreJobSchedulesInternal(ctx, previousValues, changedKeys)
			return jobschedule.Config{}, errors.Join(
				fmt.Errorf("failed to apply job schedule update: %w", err),
				restoreErr,
			)
		}
	}

	return s.GetJobSchedules(ctx), nil
}

func (s *JobService) RescheduleJobsForSettingKeys(ctx context.Context, changedKeys []string) error {
	if len(changedKeys) == 0 {
		return nil
	}
	if s == nil || s.scheduler == nil {
		return errors.New("job scheduler not initialized")
	}

	changed := make(map[string]struct{}, len(changedKeys))
	for _, key := range changedKeys {
		changed[key] = struct{}{}
	}

	var rescheduleErrors []error
	for jobID, jobMeta := range meta.GetAllJobMetadata() {
		if !jobMetadataAffectedBySettingInternal(jobMeta, changed) {
			continue
		}
		if s.cfg != nil && s.cfg.AgentMode && jobMeta.ManagerOnly {
			slog.DebugContext(ctx, "Skipping manager-only job reschedule in agent mode", "job", jobID)
			continue
		}

		// environment-health fans out to per-environment dynamic jobs; delegate the
		// reschedule to EnvironmentService instead of looking up a single job.
		if jobID == "environment-health" {
			if s.OnEnvironmentHealthReschedule != nil {
				reschedCtx := ctx //nolint:contextcheck // lifecycle context preferred so jobs outlive the request
				if s.lifecycleCtx != nil {
					reschedCtx = s.lifecycleCtx
				}
				s.OnEnvironmentHealthReschedule(reschedCtx)
			} else {
				rescheduleErrors = append(rescheduleErrors, errors.New("environment-health rescheduler not initialized"))
			}
			continue
		}

		// Continuous bus watchers have no cron entry to reschedule; notify the
		// watcher so it re-reads its poll schedule instead.
		if jobMeta.IsContinuous {
			if jobID == "image-polling" && s.settings != nil && s.settings.OnImagePollingSettingsChanged != nil {
				notifyCtx := ctx //nolint:contextcheck // lifecycle context preferred so the watcher outlives the request
				if s.lifecycleCtx != nil {
					notifyCtx = s.lifecycleCtx
				}
				s.settings.OnImagePollingSettingsChanged(notifyCtx)
			}
			continue
		}

		job, ok := s.scheduler.GetJob(jobID)
		if !ok {
			rescheduleErrors = append(rescheduleErrors, fmt.Errorf("job %s not found in scheduler", jobID))
			continue
		}

		slog.DebugContext(ctx, "Processing job setting change", "job", jobID, "settingsKey", jobMeta.SettingsKey, "enabledKey", jobMeta.EnabledKey)
		rescheduleCtx := ctx //nolint:contextcheck // fallback only; lifecycle context is preferred so cron jobs outlive HTTP requests
		if s.lifecycleCtx != nil {
			rescheduleCtx = s.lifecycleCtx
		}
		if err := s.scheduler.RescheduleJob(rescheduleCtx, job); err != nil {
			rescheduleErrors = append(rescheduleErrors, fmt.Errorf("reschedule job %s: %w", jobID, err))
			continue
		}

		runtimeState, ok := s.scheduler.GetJobRuntimeState(jobID)
		if !ok {
			rescheduleErrors = append(rescheduleErrors, fmt.Errorf("job %s has no runtime scheduler state", jobID))
			continue
		}

		expectedSchedule := job.Schedule(rescheduleCtx)
		if runtimeState.Schedule != expectedSchedule {
			rescheduleErrors = append(rescheduleErrors, fmt.Errorf("job %s runtime schedule %q does not match requested schedule %q", jobID, runtimeState.Schedule, expectedSchedule))
		}
	}

	return errors.Join(rescheduleErrors...)
}

func (s *JobService) restoreJobSchedulesInternal(ctx context.Context, previousValues map[string]string, changedKeys []string) error {
	if len(previousValues) == 0 {
		return nil
	}

	if err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		for key, value := range previousValues {
			if err := tx.Save(&models.SettingVariable{Key: key, Value: value}).Error; err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		return fmt.Errorf("failed to restore previous job schedules: %w", err)
	}

	if err := s.settings.LoadDatabaseSettings(ctx); err != nil {
		return fmt.Errorf("failed to reload restored job schedules: %w", err)
	}

	if err := s.RescheduleJobsForSettingKeys(ctx, changedKeys); err != nil {
		return fmt.Errorf("failed to restore runtime job schedules: %w", err)
	}

	return nil
}

func jobMetadataAffectedBySettingInternal(jobMeta meta.JobMetadata, changed map[string]struct{}) bool {
	if jobMeta.SettingsKey != "" {
		if _, ok := changed[jobMeta.SettingsKey]; ok {
			return true
		}
	}
	if jobMeta.EnabledKey != "" {
		if _, ok := changed[jobMeta.EnabledKey]; ok {
			return true
		}
	}
	return false
}

func (s *JobService) ListJobs(ctx context.Context) (*jobschedule.JobListResponse, error) {
	if s == nil || s.settings == nil {
		return nil, errors.New("job service not initialized")
	}

	allMetadata := meta.GetAllJobMetadata()
	jobs := make([]jobschedule.JobStatus, 0, len(allMetadata))

	for _, jobMeta := range allMetadata {
		schedule := s.getJobScheduleInternal(ctx, jobMeta)
		nextRun := s.calculateNextRunInternal(schedule)
		enabled := s.isJobEnabledInternal(ctx, jobMeta)
		prerequisites := s.evaluatePrerequisitesInternal(ctx, jobMeta)

		if s.scheduler != nil && jobMeta.SettingsKey != "" && jobMeta.ID != "environment-health" {
			if runtimeState, ok := s.scheduler.GetJobRuntimeState(jobMeta.ID); ok && runtimeState.Schedule != "" {
				schedule = runtimeState.Schedule
				nextRun = runtimeState.NextRun
			}
		}

		jobStatus := jobMeta.ToJobStatus(schedule, nextRun, enabled, prerequisites)
		jobs = append(jobs, jobStatus)
	}

	// Sort jobs by ID to ensure stable UI order
	sort.Slice(jobs, func(i, j int) bool {
		return jobs[i].ID < jobs[j].ID
	})

	isAgent := s.cfg != nil && s.cfg.AgentMode

	return &jobschedule.JobListResponse{
		Jobs:    jobs,
		IsAgent: isAgent,
	}, nil
}

func (s *JobService) RunJobNowInline(ctx context.Context, jobID string) error {
	// environment-health runs as per-environment dynamic jobs; "run now" fans out
	// through EnvironmentService rather than a single registered job.
	if jobID == "environment-health" && s.RunEnvironmentHealthNow != nil {
		return s.RunEnvironmentHealthNow(context.WithoutCancel(ctx))
	}
	jobMeta, ok := meta.GetJobMetadata(jobID)
	if ok && jobMeta.IsContinuous && jobMeta.CanRunManually {
		if s == nil || s.scheduler == nil {
			return errors.New("job service or scheduler not initialized")
		}
		return s.scheduler.RunBusWatcherNow(context.WithoutCancel(ctx), jobID)
	}

	job, err := s.getRunnableJobInternal(jobID)
	if err != nil {
		return err
	}

	runCtx := context.WithoutCancel(ctx)
	job.Run(runCtx)

	return nil
}

func (s *JobService) getRunnableJobInternal(jobID string) (schedulertypes.Job, error) {
	if s == nil || s.scheduler == nil {
		return nil, errors.New("job service or scheduler not initialized")
	}

	meta, ok := meta.GetJobMetadata(jobID)
	if !ok {
		return nil, fmt.Errorf("unknown job: %s", jobID)
	}

	if !meta.CanRunManually {
		return nil, fmt.Errorf("job %s cannot be run manually", jobID)
	}

	if s.cfg != nil && s.cfg.AgentMode && meta.ManagerOnly {
		return nil, fmt.Errorf("job %s is manager-only and cannot run in agent mode", jobID)
	}

	job, ok := s.scheduler.GetJob(jobID)
	if !ok {
		return nil, fmt.Errorf("job %s not found in scheduler", jobID)
	}

	return job, nil
}

func (s *JobService) getJobScheduleInternal(ctx context.Context, meta meta.JobMetadata) string {
	// Continuous jobs with a settings key (image-polling) are event-driven but
	// also poll on that cron schedule, so surface the real expression.
	if meta.IsContinuous && meta.SettingsKey == "" {
		return "continuous"
	}

	if meta.ID == "analytics-heartbeat" {
		return "automatic (checked hourly; sent once per 24h)"
	}

	if meta.SettingsKey == "" {
		return ""
	}

	defaultSchedule, _, _, err := DefaultSettingsConfig().FieldByKey(meta.SettingsKey)
	if err != nil || defaultSchedule == "" {
		defaultSchedule = "0 0 0 * * *"
	}

	return s.settings.GetStringSetting(ctx, meta.SettingsKey, defaultSchedule)
}

func (s *JobService) isJobEnabledInternal(ctx context.Context, meta meta.JobMetadata) bool {
	if meta.EnabledKey != "" {
		return s.settings.GetBoolSetting(ctx, meta.EnabledKey, false)
	}
	if meta.IsContinuous {
		return true
	}

	return true
}

func (s *JobService) evaluatePrerequisitesInternal(ctx context.Context, meta meta.JobMetadata) []jobschedule.JobPrerequisite {
	prerequisites := make([]jobschedule.JobPrerequisite, 0, len(meta.Prerequisites))

	for _, prereq := range meta.Prerequisites {
		isMet := s.settings.GetBoolSetting(ctx, prereq.SettingKey, false)

		prerequisites = append(prerequisites, jobschedule.JobPrerequisite{
			SettingKey:  prereq.SettingKey,
			Label:       prereq.Label,
			IsMet:       isMet,
			SettingsURL: prereq.SettingsURL,
		})
	}

	return prerequisites
}

func (s *JobService) calculateNextRunInternal(schedule string) *time.Time {
	if schedule == "" || schedule == "continuous" {
		return nil
	}

	// Parse schedule and force it to use the same timezone as the scheduler.
	parser := cron.NewParser(cron.Second | cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow)
	sched, err := parser.Parse(schedule)
	if err != nil {
		return nil
	}

	location := time.UTC
	if s != nil && s.location != nil {
		location = s.location
	}

	if specSchedule, ok := sched.(*cron.SpecSchedule); ok {
		specSchedule.Location = location
	}

	// Calculate next run using the configured timezone.
	now := time.Now().In(location)
	return new(sched.Next(now))
}
