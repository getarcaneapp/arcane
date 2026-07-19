package services

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"path"
	"strconv"
	"strings"

	"github.com/getarcaneapp/arcane/backend/v2/internal/models"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/projects"
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
					break
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

func (s *VolumeService) uploadVolumeBackupToS3Internal(ctx context.Context, backup *models.VolumeBackup) error {
	if s.s3Destinations == nil {
		return errors.New("S3 backup service is unavailable")
	}
	backupCfg, err := s.s3Destinations.configurationInternal(ctx, backup.S3DestinationID)
	if err != nil {
		return errors.New("the selected S3 backup destination is not configured")
	}
	filename, err := s.backupArchiveFilenameInternal(backup.ID)
	if err != nil {
		return err
	}
	dockerClient, err := s.dockerService.GetClient(ctx)
	if err != nil {
		return err
	}
	containerID, cleanup, err := s.createBackupTempContainerInternal(ctx, dockerClient, "/volume", true)
	if err != nil {
		return err
	}
	reader, size, err := s.downloadFileFromContainerInternal(ctx, dockerClient, containerID, path.Join("/volume", filename), cleanup)
	if err != nil {
		return err
	}
	defer func() { _ = reader.Close() }()
	remoteKey := path.Join(backupCfg.S3Prefix, "volumes", projects.SanitizeProjectName(backup.VolumeName), filename)
	if err := s.s3Destinations.putObjectInternal(ctx, backupCfg, reader, remoteKey, size); err != nil {
		return err
	}
	backup.RemoteKey = remoteKey
	return nil
}

func (s *VolumeService) UploadBackup(ctx context.Context, backupID string) (*models.VolumeBackup, error) {
	var backup models.VolumeBackup
	if err := s.db.WithContext(ctx).Where("id = ?", backupID).First(&backup).Error; err != nil {
		return nil, err
	}
	if backup.Status != models.VolumeBackupStatusSucceeded {
		return nil, errors.New("only successful volume backups can be uploaded")
	}
	if strings.TrimSpace(backup.RemoteKey) != "" {
		return nil, errors.New("volume backup has already been uploaded")
	}
	localAvailable, err := s.volumeBackupArchiveAvailableInternal(ctx, backup.ID)
	if err != nil {
		return nil, err
	}
	if !localAvailable {
		return nil, errors.New("the local volume backup archive is missing")
	}
	policy, err := s.loadVolumeBackupPolicyInternal(ctx, backup.VolumeName)
	if err != nil {
		return nil, err
	}
	if policy == nil || !policy.S3Enabled || strings.TrimSpace(policy.S3DestinationID) == "" {
		return nil, errors.New("select an S3 destination in the volume backup configuration first")
	}
	backup.S3DestinationID = policy.S3DestinationID
	if err := s.uploadVolumeBackupToS3Internal(ctx, &backup); err != nil {
		return nil, err
	}
	if err := s.db.WithContext(ctx).Save(&backup).Error; err != nil {
		return nil, fmt.Errorf("failed to save uploaded volume backup: %w", err)
	}
	return &backup, nil
}

func (s *VolumeService) volumeBackupArchiveAvailableInternal(ctx context.Context, backupID string) (bool, error) {
	filename, err := s.backupArchiveFilenameInternal(backupID)
	if err != nil {
		return false, err
	}
	containerID, cleanup, err := s.createBackupTempContainerInternal(ctx, nil, "/volume", true)
	if err != nil {
		return false, err
	}
	defer cleanup()
	stdout, _, err := s.execInContainerInternal(ctx, containerID, []string{"sh", "-c", "if test -f " + strconv.Quote(path.Join("/volume", filename)) + "; then printf yes; else printf no; fi"})
	if err != nil {
		return false, err
	}
	return strings.TrimSpace(stdout) == "yes", nil
}

func (s *VolumeService) deleteVolumeBackupArchiveInternal(ctx context.Context, backupID string) error {
	filename, err := s.backupArchiveFilenameInternal(backupID)
	if err != nil {
		return err
	}
	containerID, cleanup, err := s.createBackupTempContainerInternal(ctx, nil, "/volume", false)
	if err != nil {
		return err
	}
	defer cleanup()
	if _, _, err := s.execInContainerInternal(ctx, containerID, []string{"rm", "-f", path.Join("/volume", filename)}); err != nil {
		return err
	}
	return nil
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
			backup, err := s.CreateBackup(ctx, policy.VolumeName, systemUser, models.VolumeBackupTriggerScheduled)
			if errors.Is(err, ErrVolumeBackupAlreadyRunning) {
				slog.InfoContext(ctx, "Scheduled volume backup skipped; another backup is running", "volume", policy.VolumeName)
				return
			}
			if err != nil {
				slog.ErrorContext(ctx, "Scheduled volume backup failed", "volume", policy.VolumeName, "error", err)
				return
			}
			slog.InfoContext(ctx, "Scheduled volume backup completed", "volume", policy.VolumeName, "backup_id", backup.ID, "remote_key", backup.RemoteKey)
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
