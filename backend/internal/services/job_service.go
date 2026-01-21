package services

import (
	"context"
	"fmt"
	"time"

	"github.com/getarcaneapp/arcane/backend/internal/config"
	"github.com/getarcaneapp/arcane/backend/internal/database"
	"github.com/getarcaneapp/arcane/backend/internal/models"
	"github.com/getarcaneapp/arcane/types/jobschedule"
	"github.com/getarcaneapp/arcane/types/meta"
	schedulertypes "github.com/getarcaneapp/arcane/types/scheduler"
	"github.com/robfig/cron/v3"
	"gorm.io/gorm"
)

type JobRunner interface {
	GetJob(jobID string) (schedulertypes.Job, bool)
}

// JobService manages configuration for background job schedules.
//
// Intervals are persisted in the existing settings table as individual keys.
// After updates, the SettingsService cache is reloaded and a callback can be
// triggered so the running scheduler can reschedule active jobs.
//
// NOTE: This is intentionally separate from SettingsService to keep the API
// surface job-focused and to centralize schedule validation/rescheduling.
type JobService struct {
	db        *database.DB
	settings  *SettingsService
	cfg       *config.Config
	scheduler JobRunner

	OnJobSchedulesChanged func(ctx context.Context, changedKeys []string)
}

func NewJobService(db *database.DB, settings *SettingsService, cfg *config.Config) *JobService {
	return &JobService{db: db, settings: settings, cfg: cfg}
}

func (s *JobService) SetScheduler(scheduler JobRunner) {
	s.scheduler = scheduler
}

func (s *JobService) GetJobSchedules(ctx context.Context) jobschedule.Config {
	// Use SettingsService cache for fast reads.
	return jobschedule.Config{
		EnvironmentHealthInterval:  s.settings.GetStringSetting(ctx, "environmentHealthInterval", "0 */2 * * * *"),
		EventCleanupInterval:       s.settings.GetStringSetting(ctx, "eventCleanupInterval", "0 0 */6 * * *"),
		AnalyticsHeartbeatInterval: s.settings.GetStringSetting(ctx, "analyticsHeartbeatInterval", "0 0 0 * * *"),
	}
}

func (s *JobService) UpdateJobSchedules(ctx context.Context, updates jobschedule.Update) (jobschedule.Config, error) {
	if s == nil || s.db == nil || s.settings == nil {
		return jobschedule.Config{}, fmt.Errorf("job service not initialized")
	}
	if s.cfg != nil && (s.cfg.UIConfigurationDisabled || s.cfg.AgentMode) {
		return jobschedule.Config{}, fmt.Errorf("job schedule updates are disabled")
	}

	current := s.GetJobSchedules(ctx)

	// Validate inputs (cron expressions)
	validate := func(name string, v *string) error {
		if v == nil || *v == "" {
			return nil
		}
		if _, err := cron.NewParser(cron.Second | cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow).Parse(*v); err != nil {
			return fmt.Errorf("invalid cron expression for %s: %w", name, err)
		}
		return nil
	}

	if err := validate("environmentHealthInterval", updates.EnvironmentHealthInterval); err != nil {
		return jobschedule.Config{}, err
	}
	if err := validate("eventCleanupInterval", updates.EventCleanupInterval); err != nil {
		return jobschedule.Config{}, err
	}
	if err := validate("analyticsHeartbeatInterval", updates.AnalyticsHeartbeatInterval); err != nil {
		return jobschedule.Config{}, err
	}

	changed := false
	changedKeys := make([]string, 0, 3)
	upsert := func(tx *gorm.DB, key string, v *string, currentVal string) error {
		if v == nil {
			return nil
		}
		if *v == currentVal {
			return nil
		}
		changed = true
		changedKeys = append(changedKeys, key)
		return tx.Save(&models.SettingVariable{Key: key, Value: *v}).Error
	}

	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := upsert(tx, "environmentHealthInterval", updates.EnvironmentHealthInterval, current.EnvironmentHealthInterval); err != nil {
			return err
		}
		if err := upsert(tx, "eventCleanupInterval", updates.EventCleanupInterval, current.EventCleanupInterval); err != nil {
			return err
		}
		if err := upsert(tx, "analyticsHeartbeatInterval", updates.AnalyticsHeartbeatInterval, current.AnalyticsHeartbeatInterval); err != nil {
			return err
		}
		return nil
	})
	if err != nil {
		return jobschedule.Config{}, fmt.Errorf("failed to update job schedules: %w", err)
	}

	// Refresh settings cache so jobs reading from SettingsService see new values.
	if changed {
		if err := s.settings.LoadDatabaseSettings(ctx); err != nil {
			return jobschedule.Config{}, fmt.Errorf("failed to reload settings after job schedule update: %w", err)
		}

		if s.OnJobSchedulesChanged != nil {
			s.OnJobSchedulesChanged(ctx, changedKeys)
		}
	}

	return s.GetJobSchedules(ctx), nil
}

func (s *JobService) ListJobs(ctx context.Context) (*jobschedule.JobListResponse, error) {
	if s == nil || s.settings == nil {
		return nil, fmt.Errorf("job service not initialized")
	}

	allMetadata := meta.GetAllJobMetadata()
	jobs := make([]jobschedule.JobStatus, 0, len(allMetadata))

	for _, meta := range allMetadata {
		schedule := s.getJobScheduleInternal(ctx, meta)
		nextRun := s.calculateNextRunInternal(schedule)
		enabled := s.isJobEnabledInternal(ctx, meta)
		prerequisites := s.evaluatePrerequisitesInternal(ctx, meta)

		jobStatus := meta.ToJobStatus(schedule, nextRun, enabled, prerequisites)
		jobs = append(jobs, jobStatus)
	}

	isAgent := s.cfg != nil && s.cfg.AgentMode

	return &jobschedule.JobListResponse{
		Jobs:    jobs,
		IsAgent: isAgent,
	}, nil
}

func (s *JobService) RunJobNowInline(ctx context.Context, jobID string) error {
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
		return nil, fmt.Errorf("job service or scheduler not initialized")
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
	if meta.IsContinuous {
		return "continuous"
	}

	if meta.SettingsKey == "" {
		return ""
	}

	defaultSchedules := map[string]string{
		"environmentHealthInterval":  "0 */2 * * * *",
		"eventCleanupInterval":       "0 0 */6 * * *",
		"analyticsHeartbeatInterval": "0 0 0 * * *",
		"autoUpdateInterval":         "0 0 0 * * *",
		"pollingInterval":            "0 */15 * * * *",
		"scheduledPruneInterval":     "0 0 0 * * *",
		"gitopsSyncInterval":         "0 */5 * * * *",
	}

	defaultSchedule := defaultSchedules[meta.SettingsKey]
	if defaultSchedule == "" {
		defaultSchedule = "0 0 0 * * *"
	}

	return s.settings.GetStringSetting(ctx, meta.SettingsKey, defaultSchedule)
}

func (s *JobService) isJobEnabledInternal(ctx context.Context, meta meta.JobMetadata) bool {
	if meta.IsContinuous {
		return true
	}

	if meta.EnabledKey != "" {
		return s.settings.GetBoolSetting(ctx, meta.EnabledKey, false)
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

	parser := cron.NewParser(cron.Second | cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow)
	sched, err := parser.Parse(schedule)
	if err != nil {
		return nil
	}

	nextRun := sched.Next(time.Now())
	return &nextRun
}
