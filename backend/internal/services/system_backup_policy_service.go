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
	backuptypes "github.com/getarcaneapp/arcane/types/v2/backup"
	schedulertypes "github.com/getarcaneapp/arcane/types/v2/scheduler"
	"go.getarcane.app/sys/crypto"
	"gorm.io/gorm"
)

const (
	systemBackupJobPrefix  = "system-recovery-backup:"
	systemRecoveryConfigID = "system-recovery"
)

func systemBackupJobNameInternal(policyID, schedule string) string {
	sum := sha256.Sum256([]byte(schedule))
	return fmt.Sprintf("%s%s:%x", systemBackupJobPrefix, policyID, sum[:6])
}

func (s *SystemBackupService) loadPoliciesInternal(ctx context.Context) ([]models.SystemBackupPolicy, error) {
	var policies []models.SystemBackupPolicy
	if err := s.db.WithContext(ctx).Order("created_at ASC").Find(&policies).Error; err != nil {
		return nil, fmt.Errorf("failed to load system backup policies: %w", err)
	}
	return policies, nil
}

func (s *SystemBackupService) loadPolicyInternal(ctx context.Context, policyID string) (*models.SystemBackupPolicy, error) {
	if strings.TrimSpace(policyID) == "" {
		return nil, nil
	}
	var policy models.SystemBackupPolicy
	err := s.db.WithContext(ctx).Where("id = ?", policyID).First(&policy).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to load system backup policy: %w", err)
	}
	return &policy, nil
}

func (s *SystemBackupService) recoveryKeyConfiguredInternal(ctx context.Context) (bool, error) {
	var count int64
	if err := s.db.WithContext(ctx).Model(&models.SystemBackupRecoveryConfig{}).
		Where("id = ? AND encrypted_recovery_key <> ''", systemRecoveryConfigID).Count(&count).Error; err != nil {
		return false, fmt.Errorf("failed to load recovery key status: %w", err)
	}
	return count > 0, nil
}

func (s *SystemBackupService) SetRecoveryKey(ctx context.Context, recoveryKey string) (*backuptypes.SystemBackupRecoveryKeyStatus, error) {
	if err := validateRecoveryKeyInternal(recoveryKey); err != nil {
		return nil, err
	}
	encrypted, err := crypto.Encrypt(recoveryKey)
	if err != nil {
		return nil, fmt.Errorf("failed to encrypt recovery key: %w", err)
	}
	config := models.SystemBackupRecoveryConfig{EncryptedRecoveryKey: encrypted}
	config.ID = systemRecoveryConfigID
	if err := s.db.WithContext(ctx).Save(&config).Error; err != nil {
		return nil, fmt.Errorf("failed to save recovery key: %w", err)
	}
	return &backuptypes.SystemBackupRecoveryKeyStatus{Configured: true}, nil
}

func (s *SystemBackupService) GetPolicies(ctx context.Context) (*backuptypes.SystemBackupPolicyCollection, error) {
	policies, err := s.loadPoliciesInternal(ctx)
	if err != nil {
		return nil, err
	}
	configured, err := s.recoveryKeyConfiguredInternal(ctx)
	if err != nil {
		return nil, err
	}
	result := &backuptypes.SystemBackupPolicyCollection{
		Policies: make([]backuptypes.SystemBackupPolicy, 0, len(policies)), RecoveryKeyStored: configured,
	}
	destinations := make(map[string]string)
	if s.s3Destinations != nil {
		if available, listErr := s.s3Destinations.ListAllS3Destinations(ctx); listErr == nil {
			for _, destination := range available {
				destinations[destination.ID] = destination.Name
			}
		}
	}
	for i := range policies {
		var lastRun *models.SystemBackupRun
		var run models.SystemBackupRun
		if runErr := s.db.WithContext(ctx).Where("policy_id = ?", policies[i].ID).Order("created_at DESC").First(&run).Error; runErr == nil {
			run.S3DestinationName = destinations[run.S3DestinationID]
			lastRun = &run
		} else if !errors.Is(runErr, gorm.ErrRecordNotFound) {
			return nil, fmt.Errorf("failed to load latest system backup: %w", runErr)
		}
		dto := policies[i].ToDTO(lastRun)
		dto.S3DestinationName = destinations[policies[i].S3DestinationID]
		result.Policies = append(result.Policies, dto)
	}
	return result, nil
}

func (s *SystemBackupService) UpdatePolicies(ctx context.Context, updates []backuptypes.UpdateSystemBackupPolicy) (*backuptypes.SystemBackupPolicyCollection, error) {
	configured, err := s.recoveryKeyConfiguredInternal(ctx)
	if err != nil {
		return nil, err
	}
	for i := range updates {
		schedule, scheduleErr := normalizeVolumeBackupScheduleInternal(updates[i].Schedule)
		if scheduleErr != nil {
			return nil, errors.New(strings.ReplaceAll(scheduleErr.Error(), "volume backup", "system backup"))
		}
		updates[i].Schedule = schedule
		if updates[i].RetentionCount < 0 || updates[i].RetentionCount > 3650 {
			return nil, errors.New("retentionCount must be between 0 and 3650")
		}
		if !updates[i].LocalEnabled && !updates[i].S3Enabled {
			return nil, errors.New("select at least one system backup destination")
		}
		if updates[i].Enabled && !configured {
			return nil, errors.New("configure a recovery key before enabling scheduled system backups")
		}
		if updates[i].S3Enabled {
			if strings.TrimSpace(updates[i].S3DestinationID) == "" {
				return nil, errors.New("select an S3 destination for system backups")
			}
			if _, destinationErr := s.s3Destinations.configurationInternal(ctx, updates[i].S3DestinationID); destinationErr != nil {
				return nil, errors.New("select a valid S3 destination for system backups")
			}
		} else {
			updates[i].S3DestinationID = ""
		}
	}
	existing, err := s.loadPoliciesInternal(ctx)
	if err != nil {
		return nil, err
	}
	byID := make(map[string]models.SystemBackupPolicy, len(existing))
	for i := range existing {
		byID[existing[i].ID] = existing[i]
	}
	policies := make([]models.SystemBackupPolicy, 0, len(updates))
	kept := make(map[string]struct{}, len(updates))
	for _, update := range updates {
		policy := models.SystemBackupPolicy{}
		if update.ID != "" {
			var ok bool
			policy, ok = byID[update.ID]
			if !ok {
				return nil, errors.New("system backup policy not found")
			}
			if _, duplicate := kept[update.ID]; duplicate {
				return nil, errors.New("duplicate system backup policy")
			}
			kept[update.ID] = struct{}{}
		}
		policy.Enabled, policy.Schedule, policy.RetentionCount = update.Enabled, update.Schedule, update.RetentionCount
		policy.LocalEnabled, policy.S3Enabled, policy.S3DestinationID = update.LocalEnabled, update.S3Enabled, update.S3DestinationID
		policies = append(policies, policy)
	}
	if err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
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
	}); err != nil {
		return nil, fmt.Errorf("failed to save system backup policies: %w", err)
	}
	for i := range existing {
		s.removeSystemBackupPolicyJobInternal(ctx, existing[i].ID, existing[i].Schedule)
	}
	for i := range policies {
		s.rescheduleSystemBackupPolicyInternal(ctx, &policies[i])
	}
	return s.GetPolicies(ctx)
}

func (s *SystemBackupService) SetScheduler(ctx context.Context, scheduler DynamicScheduler) { //nolint:contextcheck
	if ctx == nil {
		ctx = context.Background()
	}
	s.lifecycleCtx, s.scheduler = ctx, scheduler
}

func (s *SystemBackupService) schedulerContextInternal(ctx context.Context) context.Context {
	if s.lifecycleCtx != nil {
		return s.lifecycleCtx
	}
	return context.WithoutCancel(ctx)
}

func (s *SystemBackupService) buildJobInternal(policyID, schedule string) *schedulertypes.GenericJob {
	return &schedulertypes.GenericJob{
		JobName: systemBackupJobNameInternal(policyID, schedule),
		ScheduleFn: func(ctx context.Context) string {
			policy, err := s.loadPolicyInternal(ctx, policyID)
			if err != nil || policy == nil {
				return defaultSystemBackupSchedule
			}
			return policy.Schedule
		},
		ShouldRunFn: func(ctx context.Context) bool {
			policy, err := s.loadPolicyInternal(ctx, policyID)
			return err == nil && policy != nil && policy.Enabled && policy.Schedule == schedule
		},
		RunFn: func(ctx context.Context) {
			policy, loadErr := s.loadPolicyInternal(ctx, policyID)
			if loadErr != nil || policy == nil || !policy.Enabled || policy.Schedule != schedule {
				return
			}
			var run *models.SystemBackupRun
			_, runErr := activitylib.RunHandlerActivity(ctx, s.activityService, activitylib.HandlerOptions{
				EnvironmentID: "0", Type: models.ActivityTypeResourceAction, ResourceType: "system_backup",
				ResourceID: policy.ID, ResourceName: "Arcane", User: &systemUser,
				Step: "Creating scheduled system backup", Message: "Creating scheduled Arcane system backup",
				SuccessMessage: "Scheduled Arcane system backup created successfully",
				Metadata: models.JSON{"action": "scheduled_system_backup", "policyId": policy.ID, "schedule": policy.Schedule,
					"retentionCount": policy.RetentionCount, "localEnabled": policy.LocalEnabled,
					"s3Enabled": policy.S3Enabled, "s3DestinationId": policy.S3DestinationID},
			}, func(activityCtx context.Context) error {
				var backupErr error
				run, backupErr = s.CreateBackup(activityCtx, systemUser, models.VolumeBackupTriggerScheduled,
					backuptypes.CreateSystemBackupRequest{PolicyID: policy.ID})
				return backupErr
			})
			if errors.Is(runErr, ErrSystemBackupAlreadyRunning) {
				slog.InfoContext(ctx, "Scheduled Arcane system backup skipped; another backup is running", "policyId", policy.ID)
				return
			}
			if runErr != nil {
				slog.ErrorContext(ctx, "Scheduled Arcane system backup failed", "policyId", policy.ID, "error", runErr)
				return
			}
			if policy.RetentionCount > 0 {
				if retentionErr := s.applyRetentionInternal(ctx, policy.ID, policy.RetentionCount); retentionErr != nil {
					slog.ErrorContext(ctx, "System backup retention failed", "policyId", policy.ID, "error", retentionErr)
				}
			}
			slog.InfoContext(ctx, "Scheduled Arcane system backup completed", "backupId", run.ID, "policyId", policy.ID)
		},
	}
}

func (s *SystemBackupService) removeSystemBackupPolicyJobInternal(ctx context.Context, policyID, schedule string) {
	if s.scheduler != nil {
		s.scheduler.RemoveJob(s.schedulerContextInternal(ctx), systemBackupJobNameInternal(policyID, schedule))
	}
}

func (s *SystemBackupService) rescheduleSystemBackupPolicyInternal(ctx context.Context, policy *models.SystemBackupPolicy) {
	if s.scheduler == nil || policy == nil {
		return
	}
	if !policy.Enabled {
		s.removeSystemBackupPolicyJobInternal(ctx, policy.ID, policy.Schedule)
		return
	}
	if err := s.scheduler.AddJob(s.schedulerContextInternal(ctx), s.buildJobInternal(policy.ID, policy.Schedule)); err != nil {
		slog.ErrorContext(ctx, "Failed to schedule Arcane system backup", "policyId", policy.ID, "error", err)
	}
}

func (s *SystemBackupService) RegisterBackupJobOnStartup(ctx context.Context) {
	policies, err := s.loadPoliciesInternal(ctx)
	if err != nil {
		slog.ErrorContext(ctx, "Failed to load Arcane system backup policies", "error", err)
		return
	}
	for i := range policies {
		s.rescheduleSystemBackupPolicyInternal(ctx, &policies[i])
	}
	slog.InfoContext(ctx, "Registered scheduled Arcane system backup jobs", "count", len(policies))
}

func (s *SystemBackupService) applyRetentionInternal(ctx context.Context, policyID string, keep int) error {
	var expired []models.SystemBackupRun
	if err := s.db.WithContext(ctx).
		Where(
			"policy_id = ? AND status = ? AND (COALESCE(local_snapshot_id, '') <> '' OR COALESCE(remote_snapshot_id, '') <> '')",
			policyID,
			models.VolumeBackupStatusSucceeded,
		).
		Order("created_at DESC").
		Offset(keep).
		Find(&expired).Error; err != nil {
		return err
	}
	for i := range expired {
		if err := s.DeleteBackup(ctx, expired[i].ID, ""); err != nil {
			return err
		}
	}
	return nil
}
