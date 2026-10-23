// Package sweeps selects the activity rows that periodic and startup repair
// sweeps finalize.
package sweeps

import (
	"context"
	"slices"
	"time"

	"github.com/getarcaneapp/arcane/types/v3"
	"gorm.io/gorm"

	"github.com/getarcaneapp/arcane/backend/v3/internal/database"
)

const (
	staleImageUpdateCheckAge = 6 * time.Hour
	// abandonedActivityGrace is how old an untracked queued/running activity
	// must be before the periodic sweep fails it. It covers the window between
	// StartActivity's row creation and the worker's Track call.
	abandonedActivityGrace = 2 * time.Minute

	// abandonedActivitySweepLimit bounds one sweep; later sweeps take the rest.
	abandonedActivitySweepLimit = 500
)

var backupActions = []string{
	"create_volume_backup",
	"create_system_backup",
	"run_system_volume_backups",
	"scheduled_volume_backup",
	"scheduled_system_backup",
}

// Candidate is the part of an activity row a sweep decides on.
type Candidate struct {
	ID        string
	StartedAt time.Time
	Metadata  database.JSON `gorm:"type:text"`
}

// Service runs the candidate queries behind the periodic activity sweeps.
type Service struct {
	db *database.DB
}

// NewService returns a sweeps query service over db.
func NewService(db *database.DB) *Service {
	return &Service{db: db}
}

func (s *Service) activities(ctx context.Context) *gorm.DB {
	return s.db.WithContext(ctx).Table("activities")
}

// StaleImageUpdateChecks returns image update checks left running across a
// prior process lifetime.
func (s *Service) StaleImageUpdateChecks(ctx context.Context) ([]Candidate, error) {
	var rows []Candidate
	err := s.activities(ctx).
		Where("type = ? AND status = ? AND started_at < ?", types.ActivityTypeImageUpdateCheck, types.ActivityStatusRunning, time.Now().Add(-staleImageUpdateCheckAge)).
		Find(&rows).Error
	return rows, err
}

// Abandoned returns active non-job activities past the grace period that are
// not in trackedIDs, oldest first.
func (s *Service) Abandoned(ctx context.Context, trackedIDs []string) ([]Candidate, error) {
	query := s.activities(ctx).
		Select("id", "started_at").
		Where("status IN ? AND started_at < ?", []types.ActivityStatus{types.ActivityStatusQueued, types.ActivityStatusRunning}, time.Now().Add(-abandonedActivityGrace)).
		Where("type <> ?", types.ActivityTypeJobRun)
	if len(trackedIDs) > 0 {
		query = query.Where("id NOT IN ?", trackedIDs)
	}
	var rows []Candidate
	err := query.Order("started_at").Limit(abandonedActivitySweepLimit).Find(&rows).Error
	return rows, err
}

// OrphanedQueued returns queued non-job activities outside protectedIDs.
func (s *Service) OrphanedQueued(ctx context.Context, protectedIDs []string) ([]Candidate, error) {
	var rows []Candidate
	err := s.activities(ctx).
		Where("status = ?", types.ActivityStatusQueued).
		Where("type <> ?", types.ActivityTypeJobRun).
		Where("id NOT IN ?", append(protectedIDs, "")).
		Find(&rows).Error
	return rows, err
}

// StaleAutoUpdates returns running auto-update activities outside protectedIDs.
func (s *Service) StaleAutoUpdates(ctx context.Context, protectedIDs []string) ([]Candidate, error) {
	var rows []Candidate
	err := s.activities(ctx).
		Where("type = ? AND status = ?", types.ActivityTypeAutoUpdate, types.ActivityStatusRunning).
		Where("id NOT IN ?", append(protectedIDs, "")).
		Find(&rows).Error
	return rows, err
}

// InterruptedBackups returns active backup activities outside protectedIDs.
func (s *Service) InterruptedBackups(ctx context.Context, protectedIDs []string) ([]Candidate, error) {
	var rows []Candidate
	if err := s.activities(ctx).
		Where("status IN ?", []types.ActivityStatus{types.ActivityStatusQueued, types.ActivityStatusRunning}).
		Where("id NOT IN ?", append(protectedIDs, "")).
		Find(&rows).Error; err != nil {
		return nil, err
	}
	return slices.DeleteFunc(rows, func(row Candidate) bool {
		action, _ := row.Metadata["action"].(string)
		return !slices.Contains(backupActions, action)
	}), nil
}
