package volume

import (
	"context"

	"emperror.dev/errors"
	"github.com/getarcaneapp/arcane/backend/v2/internal/common"
	"github.com/getarcaneapp/arcane/backend/v2/internal/database"
	activitylib "github.com/getarcaneapp/arcane/backend/v2/pkg/libarcane/activity"
	activitytypes "github.com/getarcaneapp/arcane/types/v2/activity"
	volumetypes "github.com/getarcaneapp/arcane/types/v2/volume"
)

// StartBackup persists a running backup and submits application-owned work.
func (s *VolumeService) StartBackup(ctx context.Context, environmentID, volumeName string, user common.User, request volumetypes.CreateBackupRequest) (volumetypes.BackupEntry, error) {
	plan, err := s.resolveBackupPlanInternal(ctx, volumeName, VolumeBackupTriggerManual, request, nil)
	if err != nil {
		return volumetypes.BackupEntry{}, err
	}
	entry, lease, err := s.prepareBackupInternal(ctx, volumeName, VolumeBackupTriggerManual, request.PolicyID, plan)
	if err != nil {
		return volumetypes.BackupEntry{}, err
	}
	activityID, workCtx := activitylib.StartHandlerActivity(ctx, s.activityService, environmentID, activitytypes.TypeResourceAction,
		"volume", volumeName, volumeName, &user, "Creating backup", "Creating volume backup",
		database.JSON{"action": "create_volume_backup", "backupId": entry.ID, "destination": entry.Destination, "policyId": request.PolicyID, "s3DestinationId": entry.S3DestinationID}, false)
	if activityID == "" {
		defer lease.Release()
		return volumetypes.BackupEntry{}, s.completeBackupInternal(ctx, entry, errors.New("failed to start backup activity"))
	}
	entry.ActivityID = &activityID
	accepted := entry.ToDTO()
	complete := func(runErr error) {
		defer lease.Release()
		runErr = s.completeBackupInternal(workCtx, entry, runErr)
		activitylib.CompleteHandlerActivity(workCtx, s.activityService, activityID, "Volume backup created successfully", runErr)
	}
	if err := s.engine.SubmitRun(workCtx, entry.ID, func(workerCtx context.Context) error {
		return s.executeBackupInternal(workerCtx, entry, user, plan)
	}, complete); err != nil {
		complete(err)
		return volumetypes.BackupEntry{}, err
	}
	return accepted, nil
}

// ReconcileInterruptedBackups runs before this process can accept backup work.
func (s *VolumeService) ReconcileInterruptedBackups(ctx context.Context) error {
	return s.db.WithContext(ctx).Model(&VolumeBackup{}).Where("status = ?", VolumeBackupStatusRunning).
		Updates(map[string]any{"status": VolumeBackupStatusFailed, "error": "Backup interrupted by Arcane restart"}).Error
}
