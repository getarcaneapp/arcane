package services

import (
	"context"
	"fmt"
	"strconv"

	"github.com/getarcaneapp/arcane/backend/internal/config"
	"github.com/getarcaneapp/arcane/backend/internal/database"
	"github.com/getarcaneapp/arcane/backend/internal/models"
	"github.com/getarcaneapp/arcane/types/jobschedule"
	"gorm.io/gorm"
)

// JobService manages configuration for background job schedules.
//
// Intervals are persisted in the existing settings table as individual keys.
// After updates, the SettingsService cache is reloaded and a callback can be
// triggered so the running scheduler can reschedule active jobs.
//
// NOTE: This is intentionally separate from SettingsService to keep the API
// surface job-focused and to centralize schedule validation/rescheduling.
type JobService struct {
	db       *database.DB
	settings *SettingsService
	cfg      *config.Config

	OnJobSchedulesChanged func(ctx context.Context)
}

func NewJobService(db *database.DB, settings *SettingsService, cfg *config.Config) *JobService {
	return &JobService{db: db, settings: settings, cfg: cfg}
}

func (s *JobService) GetJobSchedules(ctx context.Context) jobschedule.Config {
	// Use SettingsService cache for fast reads.
	return jobschedule.Config{
		EnvironmentHealthInterval:  s.settings.GetIntSetting(ctx, "environmentHealthInterval", 2),
		EventCleanupInterval:       s.settings.GetIntSetting(ctx, "eventCleanupInterval", 360),
		AnalyticsHeartbeatInterval: s.settings.GetIntSetting(ctx, "analyticsHeartbeatInterval", 1440),
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

	// Validate inputs (minutes)
	validate := func(name string, v *int, min, max int) error {
		if v == nil {
			return nil
		}
		if *v < min || *v > max {
			return fmt.Errorf("%s must be between %d and %d minutes", name, min, max)
		}
		return nil
	}

	// Keep bounds aligned with job-level guardrails.
	if err := validate("environmentHealthInterval", updates.EnvironmentHealthInterval, 1, 60); err != nil {
		return jobschedule.Config{}, err
	}
	if err := validate("eventCleanupInterval", updates.EventCleanupInterval, 5, 10080); err != nil {
		return jobschedule.Config{}, err
	}
	if err := validate("analyticsHeartbeatInterval", updates.AnalyticsHeartbeatInterval, 60, 43200); err != nil {
		return jobschedule.Config{}, err
	}

	changed := false
	upsert := func(tx *gorm.DB, key string, v *int, currentVal int) error {
		if v == nil {
			return nil
		}
		if *v == currentVal {
			return nil
		}
		changed = true
		return tx.Save(&models.SettingVariable{Key: key, Value: strconv.Itoa(*v)}).Error
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
			s.OnJobSchedulesChanged(ctx)
		}
	}

	return s.GetJobSchedules(ctx), nil
}
