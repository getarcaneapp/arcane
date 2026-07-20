package services

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"

	"github.com/getarcaneapp/arcane/backend/v2/internal/models"
	schedulertypes "github.com/getarcaneapp/arcane/types/v2/scheduler"
	volumetypes "github.com/getarcaneapp/arcane/types/v2/volume"
	"github.com/robfig/cron/v3"
	"gorm.io/gorm"
)

const (
	defaultVolumeBackupSchedule = "0 0 2 * * *"
	volumeBackupJobPrefix       = "volume-backup:"
)

var ErrVolumeBackupAlreadyRunning = errors.New("a backup is already running for this volume")

func (s *VolumeService) SetScheduler(ctx context.Context, scheduler DynamicScheduler) { //nolint:contextcheck // scheduled backups must use the application lifecycle context
	if ctx == nil {
		ctx = context.Background()
	}
	s.lifecycleCtx = ctx
	s.scheduler = scheduler
}

func (s *VolumeService) backupSchedulerContextInternal(ctx context.Context) context.Context {
	if s.lifecycleCtx != nil {
		return s.lifecycleCtx
	}
	if ctx != nil {
		return context.WithoutCancel(ctx)
	}
	return context.Background()
}

func volumeBackupJobNameInternal(policyID string) string {
	return volumeBackupJobPrefix + policyID
}

func (s *VolumeService) loadVolumeBackupPolicyInternal(ctx context.Context, volumeName string) (*models.VolumeBackupPolicy, error) {
	var policy models.VolumeBackupPolicy
	err := s.db.WithContext(ctx).Where("volume_name = ?", volumeName).First(&policy).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to load volume backup policy: %w", err)
	}
	return &policy, nil
}

func (s *VolumeService) GetBackupPolicy(ctx context.Context, volumeName string) (*volumetypes.BackupPolicy, error) {
	policy, err := s.loadVolumeBackupPolicyInternal(ctx, volumeName)
	if err != nil {
		return nil, err
	}
	if policy == nil {
		policy = &models.VolumeBackupPolicy{
			VolumeName:     volumeName,
			Schedule:       defaultVolumeBackupSchedule,
			RetentionCount: 7,
			LocalEnabled:   true,
		}
	}
	var lastRun *models.VolumeBackup
	var backup models.VolumeBackup
	if err := s.db.WithContext(ctx).Where("volume_name = ?", volumeName).Order("created_at DESC").First(&backup).Error; err == nil {
		lastRun = &backup
	} else if !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, fmt.Errorf("failed to load latest volume backup: %w", err)
	}
	dto := policy.ToDTO(lastRun)
	if s.s3Destinations != nil {
		destinations, listErr := s.s3Destinations.ListAllS3Destinations(ctx)
		if listErr == nil && len(destinations) > 0 {
			dto.S3Available = true
			for _, destination := range destinations {
				if destination.ID == policy.S3DestinationID {
					dto.S3Bucket = destination.Bucket
					dto.S3DestinationName = destination.Name
				}
				if dto.LastRun != nil && dto.LastRun.S3DestinationID == destination.ID {
					dto.LastRun.S3DestinationName = destination.Name
				}
			}
		}
	}
	return &dto, nil
}

func (s *VolumeService) UpdateBackupPolicy(ctx context.Context, volumeName string, update volumetypes.UpdateBackupPolicy) (*volumetypes.BackupPolicy, error) {
	parser := cron.NewParser(cron.Second | cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow | cron.Descriptor)
	update.Schedule = strings.TrimSpace(update.Schedule)
	if update.Schedule == "" {
		update.Schedule = defaultVolumeBackupSchedule
	}
	if _, err := parser.Parse(update.Schedule); err != nil {
		return nil, fmt.Errorf("invalid volume backup schedule: %w", err)
	}
	if update.RetentionCount < 0 || update.RetentionCount > 3650 {
		return nil, errors.New("retentionCount must be between 0 and 3650")
	}
	if !update.LocalEnabled && !update.S3Enabled {
		return nil, errors.New("select at least one volume backup destination")
	}
	if update.S3Enabled {
		if s.s3Destinations == nil {
			return nil, errors.New("S3 backup destinations are unavailable")
		}
		if strings.TrimSpace(update.S3DestinationID) == "" {
			return nil, errors.New("select an S3 destination for volume backups")
		}
		if _, err := s.s3Destinations.configurationInternal(ctx, update.S3DestinationID); err != nil {
			return nil, errors.New("select a valid S3 destination for volume backups")
		}
	} else {
		update.S3DestinationID = ""
	}

	policy, err := s.loadVolumeBackupPolicyInternal(ctx, volumeName)
	if err != nil {
		return nil, err
	}
	if policy == nil {
		policy = &models.VolumeBackupPolicy{VolumeName: volumeName}
	}
	policy.Enabled = update.Enabled
	policy.Schedule = update.Schedule
	policy.RetentionCount = update.RetentionCount
	policy.StopContainers = update.StopContainers
	policy.LocalEnabled = update.LocalEnabled
	policy.S3Enabled = update.S3Enabled
	policy.S3DestinationID = update.S3DestinationID
	if err := s.db.WithContext(ctx).Save(policy).Error; err != nil {
		return nil, fmt.Errorf("failed to save volume backup policy: %w", err)
	}
	s.rescheduleVolumeBackupPolicyInternal(ctx, policy)
	return s.GetBackupPolicy(ctx, volumeName)
}

func (s *VolumeService) applyVolumeBackupRetentionInternal(ctx context.Context, volumeName string, retentionCount int) error {
	var expired []models.VolumeBackup
	if err := s.db.WithContext(ctx).
		Where("volume_name = ?", volumeName).
		Order("created_at DESC").
		Offset(retentionCount).
		Find(&expired).Error; err != nil {
		return err
	}
	for i := range expired {
		if err := s.DeleteBackup(ctx, expired[i].ID, nil); err != nil {
			return err
		}
	}
	return nil
}

func (s *VolumeService) buildVolumeBackupJobInternal(policyID string) *schedulertypes.GenericJob {
	return &schedulertypes.GenericJob{
		JobName: volumeBackupJobNameInternal(policyID),
		ScheduleFn: func(ctx context.Context) string {
			var policy models.VolumeBackupPolicy
			if err := s.db.WithContext(ctx).Where("id = ?", policyID).First(&policy).Error; err != nil || strings.TrimSpace(policy.Schedule) == "" {
				return defaultVolumeBackupSchedule
			}
			return policy.Schedule
		},
		ShouldRunFn: func(ctx context.Context) bool {
			var policy models.VolumeBackupPolicy
			return s.db.WithContext(ctx).Where("id = ? AND enabled = ?", policyID, true).First(&policy).Error == nil
		},
		RunFn: func(ctx context.Context) {
			var policy models.VolumeBackupPolicy
			if err := s.db.WithContext(ctx).Where("id = ? AND enabled = ?", policyID, true).First(&policy).Error; err != nil {
				return
			}
			backup, err := s.CreateBackup(ctx, policy.VolumeName, systemUser, models.VolumeBackupTriggerScheduled, "")
			if errors.Is(err, ErrVolumeBackupAlreadyRunning) {
				slog.InfoContext(ctx, "Scheduled volume backup skipped; another backup is running", "volume", policy.VolumeName)
				return
			}
			if err != nil {
				slog.ErrorContext(ctx, "Scheduled volume backup failed", "volume", policy.VolumeName, "error", err)
				return
			}
			slog.InfoContext(ctx, "Scheduled volume backup completed", "volume", policy.VolumeName, "backup_id", backup.ID, "remote_snapshot_id", backup.RemoteSnapshotID)
		},
	}
}

func (s *VolumeService) rescheduleVolumeBackupPolicyInternal(ctx context.Context, policy *models.VolumeBackupPolicy) {
	if s.scheduler == nil || policy == nil {
		return
	}
	schedulerCtx := s.backupSchedulerContextInternal(ctx)
	if !policy.Enabled {
		s.scheduler.RemoveJob(schedulerCtx, volumeBackupJobNameInternal(policy.ID))
		return
	}
	if err := s.scheduler.AddJob(schedulerCtx, s.buildVolumeBackupJobInternal(policy.ID)); err != nil {
		slog.ErrorContext(schedulerCtx, "Failed to register volume backup job", "volume", policy.VolumeName, "error", err)
	}
}

func (s *VolumeService) RegisterBackupJobsOnStartup(ctx context.Context) {
	if s.scheduler == nil {
		return
	}
	var policies []models.VolumeBackupPolicy
	if err := s.db.WithContext(ctx).Where("enabled = ?", true).Find(&policies).Error; err != nil {
		slog.ErrorContext(ctx, "Failed to load scheduled volume backups", "error", err)
		return
	}
	for i := range policies {
		s.rescheduleVolumeBackupPolicyInternal(ctx, &policies[i])
	}
	slog.InfoContext(ctx, "Registered scheduled volume backup jobs", "count", len(policies))
}

func (s *VolumeService) removeVolumeBackupPolicyInternal(ctx context.Context, volumeName string) {
	policy, err := s.loadVolumeBackupPolicyInternal(ctx, volumeName)
	if err != nil || policy == nil {
		return
	}
	if s.scheduler != nil {
		s.scheduler.RemoveJob(s.backupSchedulerContextInternal(ctx), volumeBackupJobNameInternal(policy.ID))
	}
	if err := s.db.WithContext(ctx).Delete(policy).Error; err != nil {
		slog.WarnContext(ctx, "Failed to delete volume backup policy", "volume", volumeName, "error", err)
	}
}
