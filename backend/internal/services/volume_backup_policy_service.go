package services

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"log/slog"
	"strings"

	"github.com/getarcaneapp/arcane/backend/v2/internal/models"
	activitylib "github.com/getarcaneapp/arcane/backend/v2/pkg/libarcane/activity"
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

func volumeBackupJobNameInternal(policyID, schedule string) string {
	sum := sha256.Sum256([]byte(schedule))
	return fmt.Sprintf("%s%s:%x", volumeBackupJobPrefix, policyID, sum[:6])
}

func normalizeVolumeBackupScheduleInternal(schedule string) (string, error) {
	parser := cron.NewParser(cron.Second | cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow | cron.Descriptor)
	fields := strings.Fields(schedule)
	if len(fields) == 0 {
		return "", errors.New("volume backup schedule is required")
	}
	if len(fields) != 6 {
		return "", fmt.Errorf("invalid volume backup schedule %q: expected six fields", strings.TrimSpace(schedule))
	}
	schedule = strings.Join(fields, " ")
	if _, err := parser.Parse(schedule); err != nil {
		return "", fmt.Errorf("invalid volume backup schedule %q: %w", schedule, err)
	}
	return schedule, nil
}

func (s *VolumeService) loadVolumeBackupPoliciesInternal(ctx context.Context, volumeName string) ([]models.VolumeBackupPolicy, error) {
	var policies []models.VolumeBackupPolicy
	if err := s.db.WithContext(ctx).Where("volume_name = ?", volumeName).Order("created_at ASC").Find(&policies).Error; err != nil {
		return nil, fmt.Errorf("failed to load volume backup policies: %w", err)
	}
	return policies, nil
}

func (s *VolumeService) loadVolumeBackupPolicyInternal(ctx context.Context, volumeName, policyID string) (*models.VolumeBackupPolicy, error) {
	if strings.TrimSpace(policyID) == "" {
		return nil, nil
	}
	var policy models.VolumeBackupPolicy
	err := s.db.WithContext(ctx).Where("id = ? AND volume_name = ?", policyID, volumeName).First(&policy).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to load volume backup policy: %w", err)
	}
	return &policy, nil
}

func (s *VolumeService) GetBackupPolicies(ctx context.Context, volumeName string) (*volumetypes.BackupPolicyCollection, error) {
	policies, err := s.loadVolumeBackupPoliciesInternal(ctx, volumeName)
	if err != nil {
		return nil, err
	}
	result := &volumetypes.BackupPolicyCollection{Policies: make([]volumetypes.BackupPolicy, 0, len(policies))}
	destinations := make(map[string]models.S3Destination)
	if s.s3Destinations != nil {
		available, listErr := s.s3Destinations.ListAllS3Destinations(ctx)
		if listErr == nil {
			result.S3Available = len(available) > 0
			for _, destination := range available {
				destinations[destination.ID] = models.S3Destination{BaseModel: models.BaseModel{ID: destination.ID}, Name: destination.Name, Bucket: destination.Bucket}
			}
		}
	}
	for i := range policies {
		var lastRun *models.VolumeBackup
		var backup models.VolumeBackup
		if runErr := s.db.WithContext(ctx).Where("policy_id = ?", policies[i].ID).Order("created_at DESC").First(&backup).Error; runErr == nil {
			if destination, ok := destinations[backup.S3DestinationID]; ok {
				backup.S3DestinationName = destination.Name
			}
			lastRun = &backup
		} else if !errors.Is(runErr, gorm.ErrRecordNotFound) {
			return nil, fmt.Errorf("failed to load latest volume backup: %w", runErr)
		}
		dto := policies[i].ToDTO(lastRun)
		dto.S3Available = result.S3Available
		if destination, ok := destinations[policies[i].S3DestinationID]; ok {
			dto.S3Bucket = destination.Bucket
			dto.S3DestinationName = destination.Name
		}
		result.Policies = append(result.Policies, dto)
	}
	return result, nil
}

func (s *VolumeService) UpdateBackupPolicies(ctx context.Context, volumeName string, updates []volumetypes.UpdateBackupPolicy) (*volumetypes.BackupPolicyCollection, error) {
	for i := range updates {
		schedule, err := normalizeVolumeBackupScheduleInternal(updates[i].Schedule)
		if err != nil {
			return nil, err
		}
		updates[i].Schedule = schedule
		if updates[i].RetentionCount < 0 || updates[i].RetentionCount > 3650 {
			return nil, errors.New("retentionCount must be between 0 and 3650")
		}
		if !updates[i].LocalEnabled && !updates[i].S3Enabled {
			return nil, errors.New("select at least one volume backup destination")
		}
		if updates[i].S3Enabled {
			if s.s3Destinations == nil {
				return nil, errors.New("S3 backup destinations are unavailable")
			}
			if strings.TrimSpace(updates[i].S3DestinationID) == "" {
				return nil, errors.New("select an S3 destination for volume backups")
			}
			if _, err := s.s3Destinations.configurationInternal(ctx, updates[i].S3DestinationID); err != nil {
				return nil, errors.New("select a valid S3 destination for volume backups")
			}
		} else {
			updates[i].S3DestinationID = ""
		}
	}
	existing, err := s.loadVolumeBackupPoliciesInternal(ctx, volumeName)
	if err != nil {
		return nil, err
	}
	byID := make(map[string]models.VolumeBackupPolicy, len(existing))
	for i := range existing {
		byID[existing[i].ID] = existing[i]
	}
	policies := make([]models.VolumeBackupPolicy, 0, len(updates))
	kept := make(map[string]struct{}, len(updates))
	for _, update := range updates {
		policy := models.VolumeBackupPolicy{VolumeName: volumeName}
		if update.ID != "" {
			var ok bool
			policy, ok = byID[update.ID]
			if !ok {
				return nil, errors.New("volume backup policy not found")
			}
			if _, duplicate := kept[update.ID]; duplicate {
				return nil, errors.New("duplicate volume backup policy")
			}
			kept[update.ID] = struct{}{}
		}
		policy.Enabled, policy.Schedule, policy.RetentionCount = update.Enabled, update.Schedule, update.RetentionCount
		policy.StopContainers, policy.LocalEnabled, policy.S3Enabled = update.StopContainers, update.LocalEnabled, update.S3Enabled
		policy.S3DestinationID = update.S3DestinationID
		policies = append(policies, policy)
	}
	err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		for i := range policies {
			if saveErr := tx.Save(&policies[i]).Error; saveErr != nil {
				return saveErr
			}
		}
		for i := range existing {
			if _, ok := kept[existing[i].ID]; !ok {
				if deleteErr := tx.Delete(&existing[i]).Error; deleteErr != nil {
					return deleteErr
				}
			}
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("failed to save volume backup policies: %w", err)
	}
	for i := range existing {
		s.removeVolumeBackupPolicyJobsInternal(ctx, existing[i].ID, existing[i].Schedule)
	}
	for i := range policies {
		s.rescheduleVolumeBackupPolicyInternal(ctx, &policies[i])
	}
	return s.GetBackupPolicies(ctx, volumeName)
}

func (s *VolumeService) applyVolumeBackupRetentionInternal(ctx context.Context, policyID string, retentionCount int) error {
	var expired []models.VolumeBackup
	if err := s.db.WithContext(ctx).
		Where("policy_id = ?", policyID).
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

func (s *VolumeService) buildVolumeBackupJobInternal(policyID, schedule string) *schedulertypes.GenericJob {
	return &schedulertypes.GenericJob{
		JobName: volumeBackupJobNameInternal(policyID, schedule),
		ScheduleFn: func(ctx context.Context) string {
			var policy models.VolumeBackupPolicy
			if err := s.db.WithContext(ctx).Where("id = ?", policyID).First(&policy).Error; err != nil {
				return defaultVolumeBackupSchedule
			}
			return policy.Schedule
		},
		ShouldRunFn: func(ctx context.Context) bool {
			var policy models.VolumeBackupPolicy
			if s.db.WithContext(ctx).Where("id = ? AND enabled = ?", policyID, true).First(&policy).Error != nil {
				return false
			}
			return policy.Schedule == schedule
		},
		RunFn: func(ctx context.Context) {
			var policy models.VolumeBackupPolicy
			if err := s.db.WithContext(ctx).Where("id = ? AND enabled = ?", policyID, true).First(&policy).Error; err != nil {
				return
			}
			var backup *models.VolumeBackup
			_, err := activitylib.RunHandlerActivity(ctx, s.activityService, activitylib.HandlerOptions{
				EnvironmentID:  "0",
				Type:           models.ActivityTypeResourceAction,
				ResourceType:   "volume_backup",
				ResourceID:     policy.VolumeName,
				ResourceName:   policy.VolumeName,
				User:           &systemUser,
				Step:           "Creating scheduled backup",
				Message:        "Creating scheduled volume backup",
				SuccessMessage: "Scheduled volume backup created successfully",
				Metadata: models.JSON{
					"action":          "scheduled_volume_backup",
					"policyId":        policy.ID,
					"schedule":        schedule,
					"volumeName":      policy.VolumeName,
					"retentionCount":  policy.RetentionCount,
					"stopContainers":  policy.StopContainers,
					"localEnabled":    policy.LocalEnabled,
					"s3Enabled":       policy.S3Enabled,
					"s3DestinationId": policy.S3DestinationID,
				},
			}, func(activityCtx context.Context) error {
				var backupErr error
				backup, backupErr = s.CreateBackup(activityCtx, policy.VolumeName, systemUser, models.VolumeBackupTriggerScheduled, volumetypes.CreateBackupRequest{PolicyID: policy.ID})
				return backupErr
			})
			if errors.Is(err, ErrVolumeBackupAlreadyRunning) {
				slog.InfoContext(ctx, "Scheduled volume backup skipped; another backup is running", "volume", policy.VolumeName, "schedule", schedule)
				return
			}
			if err != nil {
				slog.ErrorContext(ctx, "Scheduled volume backup failed", "volume", policy.VolumeName, "schedule", schedule, "error", err)
				return
			}
			slog.InfoContext(ctx, "Scheduled volume backup completed", "volume", policy.VolumeName, "backup_id", backup.ID, "remote_snapshot_id", backup.RemoteSnapshotID)
		},
	}
}

func (s *VolumeService) removeVolumeBackupPolicyJobsInternal(ctx context.Context, policyID, schedule string) {
	if s.scheduler == nil {
		return
	}
	schedulerCtx := s.backupSchedulerContextInternal(ctx)
	s.scheduler.RemoveJob(schedulerCtx, volumeBackupJobNameInternal(policyID, schedule))
}

func (s *VolumeService) rescheduleVolumeBackupPolicyInternal(ctx context.Context, policy *models.VolumeBackupPolicy) {
	if s.scheduler == nil || policy == nil {
		return
	}
	schedulerCtx := s.backupSchedulerContextInternal(ctx)
	if !policy.Enabled {
		s.removeVolumeBackupPolicyJobsInternal(schedulerCtx, policy.ID, policy.Schedule)
		return
	}
	if err := s.scheduler.AddJob(schedulerCtx, s.buildVolumeBackupJobInternal(policy.ID, policy.Schedule)); err != nil {
		slog.ErrorContext(schedulerCtx, "Failed to register volume backup job", "volume", policy.VolumeName, "schedule", policy.Schedule, "error", err)
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
	policies, err := s.loadVolumeBackupPoliciesInternal(ctx, volumeName)
	if err != nil {
		return
	}
	for i := range policies {
		s.removeVolumeBackupPolicyJobsInternal(ctx, policies[i].ID, policies[i].Schedule)
	}
	if err := s.db.WithContext(ctx).Where("volume_name = ?", volumeName).Delete(&models.VolumeBackupPolicy{}).Error; err != nil {
		slog.WarnContext(ctx, "Failed to delete volume backup policy", "volume", volumeName, "error", err)
	}
}
