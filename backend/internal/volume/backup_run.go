package volume

import (
	"context"
	"encoding/json/v2"

	"emperror.dev/errors"
	"github.com/getarcaneapp/arcane/backend/v2/internal/backup"
	"github.com/getarcaneapp/arcane/backend/v2/internal/common"
	"github.com/getarcaneapp/arcane/backend/v2/internal/database"
	"github.com/getarcaneapp/arcane/backend/v2/internal/middleware"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/authz"
	activitylib "github.com/getarcaneapp/arcane/backend/v2/pkg/libarcane/activity"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/scheduler/jobcontext"
	activitytypes "github.com/getarcaneapp/arcane/types/v2/activity"
	backuptypes "github.com/getarcaneapp/arcane/types/v2/backup"
	schedulertypes "github.com/getarcaneapp/arcane/types/v2/scheduler"
	volumetypes "github.com/getarcaneapp/arcane/types/v2/volume"
	"github.com/moby/moby/client"
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
		defer lease.Release(ctx)
		return volumetypes.BackupEntry{}, s.completeBackupInternal(ctx, entry, errors.New("failed to start backup activity"))
	}
	entry.ActivityID = &activityID
	accepted := entry.ToDTO()
	keyID, _ := ctx.Value(middleware.ContextKeyApiKeyID).(string)
	payload, err := json.Marshal(manualVolumeBackupInternal{Checkpoint: volumeBackupRecoveryInternal{BackupID: entry.ID, LocalEnabled: plan.localEnabled, S3Enabled: plan.s3Enabled, S3DestinationID: plan.s3DestinationID, Policy: plan.policy}, VolumeName: volumeName, UserID: user.ID, ActivityID: activityID})
	if err == nil {
		err = s.engine.SubmitDurableRun(workCtx, backuptypes.DurableRunCommand{Kind: "volume", RunID: entry.ID, ActivityID: activityID, Payload: payload, UserID: user.ID, EnvironmentID: environmentID, Permission: authz.PermVolumesBackup, RequestedWithKey: keyID}, lease)
	}
	if err != nil {
		lease.Release(ctx)
		err = s.completeBackupInternal(workCtx, entry, err)
		activitylib.CompleteHandlerActivity(workCtx, s.activityService, activityID, "Volume backup created successfully", err)
		return volumetypes.BackupEntry{}, err
	}

	return accepted, nil
}

// ReconcileInterruptedBackups runs before this process can accept backup work.
func (s *VolumeService) ReconcileInterruptedBackups(ctx context.Context, protectedIDs ...string) error {
	query := s.db.WithContext(ctx).Model(&VolumeBackup{}).Where("status = ?", VolumeBackupStatusRunning)
	if len(protectedIDs) > 0 {
		query = query.Where("id NOT IN ?", protectedIDs)
	}
	return query.Updates(map[string]any{"status": VolumeBackupStatusFailed, "error": "Backup interrupted by Arcane restart"}).Error
}

func (s *VolumeService) ReconcileBackup(ctx context.Context, previous schedulertypes.Run, volumeName string) (schedulertypes.Outcome, error) {
	outcome := jobcontext.ConfirmedTarget(previous, volumeName)
	if outcome.Status == schedulertypes.Succeeded {
		return outcome, nil
	}
	for _, target := range previous.Outcome.Targets {
		if target.ID != volumeName || len(target.RecoveryData) == 0 {
			continue
		}
		var checkpoint volumeBackupRecoveryInternal
		if err := json.Unmarshal(target.RecoveryData, &checkpoint); err != nil {
			return outcome, err
		}
		var entry VolumeBackup
		if err := s.db.WithContext(ctx).Where("id = ? AND volume_name = ?", checkpoint.BackupID, volumeName).First(&entry).Error; err != nil {
			return outcome, err
		}
		if entry.Status != VolumeBackupStatusSucceeded {
			if checkpoint.Policy != nil && checkpoint.Policy.StopContainers {
				return outcome, nil
			}
			lease, admitted, err := s.engine.AcquireDurableRun(ctx, previous.ID, backup.VolumeAdmissionScope, volumeName)
			if err != nil {
				return outcome, err
			}
			if !admitted {
				return outcome, nil
			}
			defer lease.Release(ctx)
			if err := s.resumeBackupInternal(ctx, previous, &entry, checkpoint); err != nil {
				return outcome, err
			}
		}
		target.Status = schedulertypes.Succeeded
		if err := jobcontext.Progress(ctx, target); err != nil {
			return outcome, err
		}
		return schedulertypes.Outcome{Status: schedulertypes.Succeeded, Targets: []schedulertypes.TargetOutcome{target}}, nil
	}
	return outcome, nil
}

func backupDestinationAttemptedInternal(previous schedulertypes.Run, backupID, destination string) bool {
	for _, evidence := range previous.Outcome.Targets {
		if evidence.ID == backupID+":"+destination {
			return true
		}
	}
	return false
}

func (s *VolumeService) recoverSnapshotInternal(ctx context.Context, dockerClient *client.Client, repository backup.Repository, entry *VolumeBackup, snapshotID string) (backup.Snapshot, bool, error) {
	password, err := s.volumeBackupPasswordInternal(ctx, dockerClient, repository)
	if err != nil {
		return backup.Snapshot{}, false, err
	}
	return s.engine.FindRunSnapshot(ctx, dockerClient, repository, password, entry.ID, snapshotID)
}

func (s *VolumeService) observeBackupDestinationsInternal(ctx context.Context, dockerClient *client.Client, previous schedulertypes.Run, entry *VolumeBackup, checkpoint volumeBackupRecoveryInternal) (backup.Repository, backup.Repository, error) {
	var local, remote backup.Repository
	var err error
	if checkpoint.LocalEnabled {
		local, err = s.localRusticRepositoryInternal(ctx, dockerClient, false)
		if err != nil {
			return local, remote, err
		}
		if entry.LocalSnapshotID != "" || backupDestinationAttemptedInternal(previous, entry.ID, "local") {
			snapshot, found, err := s.recoverSnapshotInternal(ctx, dockerClient, local, entry, entry.LocalSnapshotID)
			if err != nil {
				return local, remote, err
			}
			if found {
				entry.LocalSnapshotID, entry.Size = snapshot.ID, snapshot.Size
			}
		}
	}
	if checkpoint.S3Enabled {
		remote, err = s.remoteRusticRepositoryInternal(ctx, checkpoint.S3DestinationID)
		if err != nil {
			return local, remote, err
		}
		if entry.RemoteSnapshotID != "" || backupDestinationAttemptedInternal(previous, entry.ID, "remote") {
			snapshot, found, err := s.recoverSnapshotInternal(ctx, dockerClient, remote, entry, entry.RemoteSnapshotID)
			if err != nil {
				return local, remote, err
			}
			if found {
				entry.RemoteSnapshotID = snapshot.ID
				if entry.Size == 0 {
					entry.Size = snapshot.Size
				}
			}
		}
	}
	return local, remote, nil
}

func (s *VolumeService) resumeBackupInternal(ctx context.Context, previous schedulertypes.Run, entry *VolumeBackup, checkpoint volumeBackupRecoveryInternal) error {
	dockerClient, err := s.dockerService.GetClient(ctx)
	if err != nil {
		return err
	}
	local, remote, err := s.observeBackupDestinationsInternal(ctx, dockerClient, previous, entry, checkpoint)
	if err != nil {
		return err
	}
	if checkpoint.LocalEnabled && entry.LocalSnapshotID == "" && entry.RemoteSnapshotID != "" {
		password, err := s.volumeBackupPasswordInternal(ctx, dockerClient, remote)
		if err != nil {
			return err
		}
		snapshot, err := s.engine.Replicate(ctx, dockerClient, remote, entry.RemoteSnapshotID, local, password, entry.VolumeName, backup.RunSnapshotTag(entry.ID))
		if err != nil {
			return err
		}
		entry.LocalSnapshotID = snapshot.ID
	}
	if err := s.db.WithContext(ctx).Save(entry).Error; err != nil {
		return err
	}
	plan := backupPlanInternal{localEnabled: checkpoint.LocalEnabled, s3Enabled: checkpoint.S3Enabled, s3DestinationID: checkpoint.S3DestinationID, policy: checkpoint.Policy, destination: entry.Destination}
	err = s.executeBackupInternal(ctx, entry, common.SystemUser, plan)
	return s.completeBackupInternal(ctx, entry, err)
}

type manualVolumeBackupInternal struct {
	Checkpoint volumeBackupRecoveryInternal `json:"checkpoint"`
	VolumeName string                       `json:"volumeName"`
	UserID     string                       `json:"userId"`
	ActivityID string                       `json:"activityId"`
}

func (s *VolumeService) executeDurableBackupInternal(ctx context.Context, runID string, payload []byte, interrupted bool) (err error) {
	var command manualVolumeBackupInternal
	if err = json.Unmarshal(payload, &command); err != nil {
		return err
	}
	defer func() {
		if ctx.Err() == nil {
			if err != nil {
				saveErr := s.db.WithContext(ctx).Model(&VolumeBackup{}).Where("id = ?", command.Checkpoint.BackupID).Updates(map[string]any{"status": VolumeBackupStatusFailed, "error": err.Error()}).Error
				err = errors.Combine(err, saveErr)
			}
			activitylib.CompleteHandlerActivity(ctx, s.activityService, command.ActivityID, "Volume backup created successfully", err)
		}
	}()
	var entry VolumeBackup
	if err = s.db.WithContext(ctx).Where("id = ?", command.Checkpoint.BackupID).First(&entry).Error; err != nil {
		return err
	}

	if entry.Status == VolumeBackupStatusSucceeded {
		return nil
	}
	if interrupted {
		previous, _ := jobcontext.Run(ctx)
		outcome, recoveryErr := s.ReconcileBackup(ctx, previous, command.VolumeName)
		if recoveryErr != nil {
			return recoveryErr
		}
		if outcome.Status != schedulertypes.Succeeded {
			return errors.New(outcome.Message)
		}
		return nil
	}
	lease, admitted, err := s.engine.AcquireDurableRun(ctx, runID, backup.VolumeAdmissionScope, command.VolumeName)
	if err != nil {
		return err
	}
	if !admitted {
		return ErrVolumeBackupAlreadyRunning
	}
	defer lease.Release(ctx)
	user := common.SystemUser
	if command.UserID != "" && command.UserID != "agent" {
		if err = s.db.WithContext(ctx).Where("id = ?", command.UserID).First(&user).Error; err != nil {
			return err
		}
	}
	checkpoint, err := json.Marshal(command.Checkpoint)
	if err != nil {
		return err
	}
	if err = jobcontext.Progress(ctx, schedulertypes.TargetOutcome{ResourceType: "volume_backup", ID: command.VolumeName, Status: schedulertypes.Running, RecoveryData: checkpoint, ActivityID: command.ActivityID}); err != nil {
		return err
	}
	plan := backupPlanInternal{localEnabled: command.Checkpoint.LocalEnabled, s3Enabled: command.Checkpoint.S3Enabled, s3DestinationID: command.Checkpoint.S3DestinationID, policy: command.Checkpoint.Policy, destination: entry.Destination}
	err = s.executeBackupInternal(ctx, &entry, user, plan)
	if ctx.Err() != nil {
		return ctx.Err()
	}
	return s.completeBackupInternal(ctx, &entry, err)
}

func (s *VolumeService) failDurableBackupInternal(ctx context.Context, _ string, payload []byte, runErr error) error {
	var command manualVolumeBackupInternal
	if err := json.Unmarshal(payload, &command); err != nil {
		return err
	}
	var entry VolumeBackup
	if err := s.db.WithContext(ctx).Where("id = ?", command.Checkpoint.BackupID).First(&entry).Error; err != nil {
		return err
	}
	err := s.db.WithContext(ctx).Model(&VolumeBackup{}).Where("id = ?", entry.ID).Updates(map[string]any{"status": VolumeBackupStatusFailed, "error": runErr.Error()}).Error
	activitylib.CompleteHandlerActivity(ctx, s.activityService, command.ActivityID, "Volume backup created successfully", runErr)
	return err
}
