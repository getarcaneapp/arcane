package systembackup

import (
	"context"
	"fmt"
	"log/slog"

	"emperror.dev/errors"

	"github.com/getarcaneapp/arcane/backend/v2/internal/backup"
	"github.com/getarcaneapp/arcane/backend/v2/internal/common"
	"github.com/getarcaneapp/arcane/backend/v2/internal/database"
	"github.com/getarcaneapp/arcane/backend/v2/internal/volume"
	activitylib "github.com/getarcaneapp/arcane/backend/v2/pkg/libarcane/activity"
	activitytypes "github.com/getarcaneapp/arcane/types/v2/activity"
	backuptypes "github.com/getarcaneapp/arcane/types/v2/backup"
)

// StartBackup prepares a manual backup before submitting its snapshot work.
func (s *SystemBackupService) StartBackup(ctx context.Context, user common.User, request backuptypes.CreateSystemBackupRequest) (*backuptypes.SystemBackupRun, error) {
	lease, admitted, err := s.engine.TryAcquireRun(ctx, backup.SystemAdmissionScope, systemAdmissionID)
	if err != nil {
		return nil, err
	}
	if !admitted {
		return nil, ErrSystemBackupAlreadyRunning
	}
	prepared, err := s.prepareBackupInternal(ctx, SystemBackupTriggerManual, request)
	if err != nil {
		lease.Release()
		return nil, err
	}
	activityID, workCtx := activitylib.StartHandlerActivity(ctx, s.activityService, "0", activitytypes.TypeResourceAction, "system_backup", "arcane", "Arcane", &user,
		"Creating system backup", "Creating Arcane system backup", database.JSON{"action": "create_system_backup", "backupId": prepared.run.ID, "destination": prepared.run.Destination, "s3DestinationId": prepared.run.S3DestinationID, "policyId": prepared.run.PolicyID}, false)
	finish := func(runErr error) {
		defer lease.Release()
		if runErr != nil {
			if saveErr := s.db.WithContext(context.WithoutCancel(workCtx)).Model(&SystemBackupRun{}).Where("id = ?", prepared.run.ID).Updates(map[string]any{"status": SystemBackupStatusFailed, "error": runErr.Error()}).Error; saveErr != nil {
				runErr = errors.Combine(runErr, errors.WrapIf(saveErr, "save backup failure"))
			}
		}
		activitylib.CompleteHandlerActivity(workCtx, s.activityService, activityID, "Arcane system backup created successfully", runErr)
	}
	if activityID == "" {
		err = errors.New("failed to create system backup activity")
		finish(err)
		return nil, err
	}
	dto := prepared.run.ToDTO()
	dto.ActivityID = activityID
	if err = s.engine.SubmitRun(workCtx, prepared.run.ID, func(runCtx context.Context) error {
		_, runErr := s.executeBackupInternal(runCtx, prepared)
		return runErr
	}, finish); err != nil {
		finish(err)
		return nil, err
	}
	return &dto, nil
}

// ReconcileInterruptedBackups marks work interrupted by the previous process.
func (s *SystemBackupService) ReconcileInterruptedBackups(ctx context.Context) error {
	return s.db.WithContext(ctx).Model(&SystemBackupRun{}).Where("status = ?", SystemBackupStatusRunning).
		Updates(map[string]any{"status": SystemBackupStatusFailed, "error": "Backup interrupted by application restart"}).Error
}

// StartSystemVolumeBackups freezes the selected policy and volumes before returning.
func (s *SystemBackupService) StartSystemVolumeBackups(ctx context.Context, user common.User, request backuptypes.RunSystemVolumeBackupsRequest) (*backuptypes.BackupRunAccepted, error) {
	prepared, err := s.prepareSystemVolumeBackupsInternal(ctx, request)
	if err != nil {
		return nil, err
	}
	policy, manualPolicy, candidates, lease := prepared.policy, prepared.manualPolicy, prepared.candidates, prepared.lease

	names := make([]string, len(candidates))
	for i, candidate := range candidates {
		names[i] = candidate.Name
	}
	activityID, workCtx := activitylib.StartHandlerActivity(ctx, s.activityService, "0", activitytypes.TypeResourceAction, "system_backup", "volumes", "Volumes", &user,
		"Backing up volumes", "Creating system-managed volume backups", database.JSON{"action": "run_system_volume_backups", "policyId": policy.ID, "volumeNames": names, "matched": len(candidates), "succeeded": 0, "failed": 0, "skipped": 0, "failures": []backuptypes.SystemVolumeBackupFailure{}}, false)
	if activityID == "" {
		lease.Release()
		return nil, errors.New("failed to create system-managed volume backup activity")
	}
	finish := func(runErr error) {
		defer lease.Release()
		activitylib.CompleteHandlerActivity(workCtx, s.activityService, activityID, "System-managed volume backups completed", runErr)
	}
	err = s.engine.SubmitRun(workCtx, activityID, func(runCtx context.Context) error {
		result, runErr := s.executeSystemVolumeBackupsInternal(runCtx, policy, manualPolicy, candidates, volume.VolumeBackupTriggerManual, activityID)
		if runErr != nil {
			return runErr
		}
		if result.Failed > 0 {
			return fmt.Errorf("%d volume backups failed", result.Failed)
		}
		return nil
	}, finish)
	if err != nil {
		finish(err)
		return nil, err
	}
	return &backuptypes.BackupRunAccepted{ActivityID: activityID, Status: "running"}, nil
}

func (s *SystemBackupService) updateSystemVolumeProgressInternal(ctx context.Context, activityID string, policyID string, candidates []backuptypes.SystemVolumeBackupOption, result *backuptypes.SystemVolumeBackupRunResult) {
	if activityID == "" {
		return
	}
	names := make([]string, len(candidates))
	for i, candidate := range candidates {
		names[i] = candidate.Name
	}
	progress := 100
	if result.Matched > 0 {
		progress = 100 * (result.Succeeded + result.Failed + result.Skipped) / result.Matched
	}
	_, err := s.activityService.UpdateActivity(ctx, activityID, activitylib.UpdateRequest{Progress: &progress, Metadata: database.JSON{
		"action": "run_system_volume_backups", "policyId": policyID, "volumeNames": names,
		"matched": result.Matched, "succeeded": result.Succeeded, "failed": result.Failed, "skipped": result.Skipped, "failures": result.Failures,
	}})
	if err != nil {
		slog.WarnContext(ctx, "Failed to report system-managed volume backup progress", "activityId", activityID, "policyId", policyID, "error", err)
	}
}
