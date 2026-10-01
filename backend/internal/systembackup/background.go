package systembackup

import (
	"context"
	"encoding/json/v2"
	"fmt"
	"log/slog"

	"emperror.dev/errors"
	"github.com/getarcaneapp/arcane/backend/v2/internal/backup"
	"github.com/getarcaneapp/arcane/backend/v2/internal/common"
	"github.com/getarcaneapp/arcane/backend/v2/internal/database"
	"github.com/getarcaneapp/arcane/backend/v2/internal/middleware"
	"github.com/getarcaneapp/arcane/backend/v2/internal/volume"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/authz"
	activitylib "github.com/getarcaneapp/arcane/backend/v2/pkg/libarcane/activity"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/scheduler/jobcontext"
	activitytypes "github.com/getarcaneapp/arcane/types/v2/activity"
	backuptypes "github.com/getarcaneapp/arcane/types/v2/backup"
	schedulertypes "github.com/getarcaneapp/arcane/types/v2/scheduler"
	"github.com/moby/moby/client"
	"go.getarcane.app/sys/crypto"
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
		lease.Release(ctx)
		return nil, err
	}
	activityID, workCtx := activitylib.StartHandlerActivity(ctx, s.activityService, "0", activitytypes.TypeResourceAction, "system_backup", "arcane", "Arcane", &user,
		"Creating system backup", "Creating Arcane system backup", database.JSON{"action": "create_system_backup", "backupId": prepared.run.ID, "destination": prepared.run.Destination, "s3DestinationId": prepared.run.S3DestinationID, "policyId": prepared.run.PolicyID}, false)
	finish := func(runErr error) {
		defer lease.Release(ctx)
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
	keyID, _ := ctx.Value(middleware.ContextKeyApiKeyID).(string)
	encryptedKey, keyErr := crypto.Encrypt(prepared.recoveryKey)
	if keyErr != nil {
		finish(keyErr)
		return nil, keyErr
	}
	payload, err := json.Marshal(manualSystemBackupInternal{Checkpoint: systemBackupRecoveryInternal{BackupID: prepared.run.ID, LocalEnabled: prepared.localEnabled, S3Enabled: prepared.s3Enabled}, EncryptedKey: encryptedKey, ActivityID: activityID, UserID: user.ID})
	if err == nil {
		err = s.engine.SubmitDurableRun(workCtx, backuptypes.DurableRunCommand{Kind: "system", RunID: prepared.run.ID, ActivityID: activityID, Payload: payload, UserID: user.ID, EnvironmentID: "0", Permission: authz.PermSystemBackupsManage, RequestedWithKey: keyID}, lease)
	}
	if err != nil {
		finish(err)
		return nil, err
	}

	return &dto, nil
}

// ReconcileInterruptedBackups marks work interrupted by the previous process.
func (s *SystemBackupService) ReconcileInterruptedBackups(ctx context.Context, protectedIDs ...string) error {
	query := s.db.WithContext(ctx).Model(&SystemBackupRun{}).Where("status = ?", SystemBackupStatusRunning)
	if len(protectedIDs) > 0 {
		query = query.Where("id NOT IN ?", protectedIDs)
	}
	return query.Updates(map[string]any{"status": SystemBackupStatusFailed, "error": "Backup interrupted by application restart"}).Error
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
		lease.Release(ctx)
		return nil, errors.New("failed to create system-managed volume backup activity")
	}
	finish := func(runErr error) {
		defer lease.Release(ctx)
		activitylib.CompleteHandlerActivity(workCtx, s.activityService, activityID, "System-managed volume backups completed", runErr)
	}
	keyID, _ := ctx.Value(middleware.ContextKeyApiKeyID).(string)
	payload, err := json.Marshal(manualSystemVolumesInternal{Policy: policy, ManualPolicy: manualPolicy, Candidates: candidates, ActivityID: activityID, UserID: user.ID})
	if err == nil {
		err = s.engine.SubmitDurableRun(workCtx, backuptypes.DurableRunCommand{Kind: "system-volumes", RunID: activityID, ActivityID: activityID, Payload: payload, UserID: user.ID, EnvironmentID: "0", Permission: authz.PermSystemBackupsManage, RequestedWithKey: keyID}, lease)
	}

	if err != nil {
		finish(err)
		return nil, err
	}
	return &backuptypes.BackupRunAccepted{ActivityID: activityID, Status: "running"}, nil
}

func (s *SystemBackupService) updateSystemVolumeProgressInternal(ctx context.Context, activityID, policyID string, candidates []backuptypes.SystemVolumeBackupOption, result *backuptypes.SystemVolumeBackupRunResult) {
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

func (s *SystemBackupService) reconcileBackupInternal(ctx context.Context, previous schedulertypes.Run, policyID string, suppliedKeys ...string) (schedulertypes.Outcome, error) {
	outcome := jobcontext.ConfirmedTarget(previous, policyID)
	if outcome.Status == schedulertypes.Succeeded {
		return outcome, nil
	}
	for _, target := range previous.Outcome.Targets {
		if target.ID != policyID || len(target.RecoveryData) == 0 {
			continue
		}
		var checkpoint systemBackupRecoveryInternal
		if err := json.Unmarshal(target.RecoveryData, &checkpoint); err != nil {
			return outcome, err
		}
		var run SystemBackupRun
		if err := s.db.WithContext(ctx).Where("id = ?", checkpoint.BackupID).First(&run).Error; err != nil {
			return outcome, err
		}
		if run.Status != SystemBackupStatusSucceeded {
			lease, admitted, err := s.engine.AcquireDurableRun(ctx, previous.ID, backup.SystemAdmissionScope, systemAdmissionID)
			if err != nil {
				return outcome, err
			}
			if !admitted {
				return outcome, nil
			}
			defer lease.Release(ctx)
			key := ""
			if len(suppliedKeys) > 0 {
				key = suppliedKeys[0]
			}
			if err := s.resumeBackupInternal(ctx, previous, &run, checkpoint, key); err != nil {
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

func systemBackupStageEvidenceInternal(previous schedulertypes.Run, run *SystemBackupRun) (string, bool) {
	stageID, attempted := run.LocalSnapshotID, false
	for _, evidence := range previous.Outcome.Targets {
		if evidence.ID != run.ID+":stage" {
			continue
		}
		attempted = true
		if stageID == "" && evidence.Status == schedulertypes.Succeeded {
			stageID = evidence.Message
		}
	}
	return stageID, attempted
}

func systemBackupRemoteAttemptedInternal(previous schedulertypes.Run, runID string) bool {
	for _, evidence := range previous.Outcome.Targets {
		if evidence.ID == runID+":remote" {
			return true
		}
	}
	return false
}

func (s *SystemBackupService) observeBackupSnapshotsInternal(ctx context.Context, dockerClient *client.Client, previous schedulertypes.Run, run *SystemBackupRun, checkpoint systemBackupRecoveryInternal, key string) (backup.Snapshot, backup.Repository, backup.Repository, error) {
	var staged backup.Snapshot
	var remote backup.Repository
	local, err := s.localRepositoryInternal(ctx, dockerClient, false)
	if err != nil && checkpoint.LocalEnabled {
		return staged, local, remote, err
	}
	stageID, stageAttempted := systemBackupStageEvidenceInternal(previous, run)
	if err == nil && (stageAttempted || stageID != "") {
		snapshot, found, findErr := s.engine.FindRunSnapshot(ctx, dockerClient, local, key, run.ID, stageID)
		if findErr == nil && found {
			staged = snapshot
		}
		if checkpoint.LocalEnabled && findErr != nil {
			return staged, local, remote, findErr
		}
	}
	if !checkpoint.S3Enabled {
		return staged, local, remote, nil
	}
	remote, err = s.remoteRepositoryInternal(ctx, run.S3DestinationID)
	if err != nil {
		return staged, local, remote, err
	}
	attempted := (stageAttempted && staged.ID == "") || systemBackupRemoteAttemptedInternal(previous, run.ID)
	if attempted || run.RemoteSnapshotID != "" {
		snapshot, found, err := s.engine.FindRunSnapshot(ctx, dockerClient, remote, key, run.ID, run.RemoteSnapshotID)
		if err != nil {
			return staged, local, remote, err
		}
		if found {
			run.RemoteSnapshotID = snapshot.ID
			if run.Size == 0 {
				run.Size = snapshot.Size
			}
		}
	}
	return staged, local, remote, nil
}

func (s *SystemBackupService) finishRecoveredDestinationsInternal(ctx context.Context, dockerClient *client.Client, run *SystemBackupRun, checkpoint systemBackupRecoveryInternal, key string, staged backup.Snapshot, local, remote backup.Repository) error {
	if checkpoint.LocalEnabled && staged.ID == "" && run.RemoteSnapshotID != "" {
		var err error
		staged, err = s.engine.Replicate(ctx, dockerClient, remote, run.RemoteSnapshotID, local, key, "arcane-system-recovery", backup.RunSnapshotTag(run.ID))
		if err != nil {
			return err
		}
	}
	if staged.ID == "" && run.RemoteSnapshotID == "" {
		_, err := s.executeBackupInternal(ctx, &preparedSystemBackupInternal{run: run, recoveryKey: key, localEnabled: checkpoint.LocalEnabled, s3Enabled: checkpoint.S3Enabled})
		return err
	}
	if checkpoint.LocalEnabled {
		run.LocalSnapshotID, run.Size = staged.ID, staged.Size
	}
	if checkpoint.S3Enabled && run.RemoteSnapshotID == "" {
		if staged.ID == "" {
			return errors.New("System backup has no snapshot to replicate")
		}
		snapshot, err := s.engine.Replicate(ctx, dockerClient, local, staged.ID, remote, key, "arcane-system-recovery", backup.RunSnapshotTag(run.ID))
		if err != nil {
			return err
		}
		run.RemoteSnapshotID = snapshot.ID
	}
	if !checkpoint.LocalEnabled && staged.ID != "" {
		return s.engine.ForgetSnapshots(ctx, dockerClient, local, key, []string{staged.ID})
	}
	return nil
}

func (s *SystemBackupService) resumeBackupInternal(ctx context.Context, previous schedulertypes.Run, run *SystemBackupRun, checkpoint systemBackupRecoveryInternal, suppliedKey string) error {
	dockerClient, err := s.dockerService.GetClient(ctx)
	if err != nil {
		return err
	}
	key, err := s.recoveryKeyInternal(ctx, suppliedKey)
	if err != nil {
		return err
	}
	staged, local, remote, err := s.observeBackupSnapshotsInternal(ctx, dockerClient, previous, run, checkpoint, key)
	if err != nil {
		return err
	}
	if err := s.finishRecoveredDestinationsInternal(ctx, dockerClient, run, checkpoint, key, staged, local, remote); err != nil {
		return err
	}
	run.Status, run.Error = SystemBackupStatusSucceeded, ""
	return s.db.WithContext(ctx).Save(run).Error
}

type manualSystemBackupInternal struct {
	Checkpoint   systemBackupRecoveryInternal `json:"checkpoint"`
	EncryptedKey string                       `json:"encryptedKey"`
	ActivityID   string                       `json:"activityId"`
	UserID       string                       `json:"userId"`
}

type manualSystemVolumesInternal struct {
	Policy       backuptypes.SystemVolumeBackupPolicy   `json:"policy"`
	ManualPolicy bool                                   `json:"manualPolicy"`
	Candidates   []backuptypes.SystemVolumeBackupOption `json:"candidates"`
	ActivityID   string                                 `json:"activityId"`
	UserID       string                                 `json:"userId"`
}

func (s *SystemBackupService) executeDurableBackupInternal(ctx context.Context, runID string, payload []byte, interrupted bool) (err error) {
	var command manualSystemBackupInternal
	if err = json.Unmarshal(payload, &command); err != nil {
		return err
	}
	defer func() {
		if ctx.Err() == nil {
			if err != nil {
				saveErr := s.db.WithContext(ctx).Model(&SystemBackupRun{}).Where("id = ?", command.Checkpoint.BackupID).Updates(map[string]any{"status": SystemBackupStatusFailed, "error": err.Error()}).Error
				err = errors.Combine(err, saveErr)
			}
			activitylib.CompleteHandlerActivity(ctx, s.activityService, command.ActivityID, "Arcane system backup created successfully", err)
		}
	}()
	var run SystemBackupRun
	if err = s.db.WithContext(ctx).Where("id = ?", command.Checkpoint.BackupID).First(&run).Error; err != nil {
		return err
	}
	if run.Status == SystemBackupStatusSucceeded {
		return nil
	}
	key, err := crypto.Decrypt(command.EncryptedKey)
	if err != nil {
		return err
	}
	if interrupted {
		previous, _ := jobcontext.Run(ctx)
		outcome, recoveryErr := s.reconcileBackupInternal(ctx, previous, runID, key)
		if recoveryErr != nil {
			return recoveryErr
		}
		if outcome.Status != schedulertypes.Succeeded {
			return errors.New(outcome.Message)
		}
		return nil
	}
	lease, admitted, err := s.engine.AcquireDurableRun(ctx, runID, backup.SystemAdmissionScope, systemAdmissionID)
	if err != nil {
		return err
	}
	if !admitted {
		return ErrSystemBackupAlreadyRunning
	}
	defer lease.Release(ctx)
	checkpoint, err := json.Marshal(command.Checkpoint)
	if err != nil {
		return err
	}
	if err = jobcontext.Progress(ctx, schedulertypes.TargetOutcome{ResourceType: "system_backup", ID: runID, Status: schedulertypes.Running, RecoveryData: checkpoint, ActivityID: command.ActivityID}); err != nil {
		return err
	}
	_, err = s.executeBackupInternal(ctx, &preparedSystemBackupInternal{run: &run, recoveryKey: key, localEnabled: command.Checkpoint.LocalEnabled, s3Enabled: command.Checkpoint.S3Enabled})
	return err
}

func (s *SystemBackupService) executeDurableVolumeBackupsInternal(ctx context.Context, runID string, payload []byte, interrupted bool) (err error) {
	var command manualSystemVolumesInternal
	if err = json.Unmarshal(payload, &command); err != nil {
		return err
	}
	defer func() {
		if ctx.Err() == nil {
			activitylib.CompleteHandlerActivity(ctx, s.activityService, command.ActivityID, "System-managed volume backups completed", err)
		}
	}()
	lease, admitted, err := s.engine.AcquireDurableRun(ctx, runID, backup.SystemAdmissionScope, systemAdmissionID)
	if err != nil {
		return err
	}
	if !admitted {
		return ErrSystemBackupAlreadyRunning
	}
	defer lease.Release(ctx)
	if interrupted {
		ctx, err = s.reconcileVolumeCandidatesInternal(ctx, command.Candidates)
		if err != nil {
			return err
		}
	}
	result, err := s.executeSystemVolumeBackupsInternal(ctx, command.Policy, command.ManualPolicy, command.Candidates, volume.VolumeBackupTriggerManual, command.ActivityID)
	if err != nil {
		return err
	}
	if result.Failed > 0 {
		return fmt.Errorf("%d volume backups failed", result.Failed)
	}
	return nil
}

func (s *SystemBackupService) reconcileVolumeCandidatesInternal(ctx context.Context, candidates []backuptypes.SystemVolumeBackupOption) (context.Context, error) {
	previous, _ := jobcontext.Run(ctx)
	for _, candidate := range candidates {
		pending := false
		for _, target := range previous.Outcome.Targets {
			if target.ID == candidate.Name && target.Status != schedulertypes.Succeeded && target.Status != schedulertypes.Skipped {
				pending = true
				break
			}
		}
		if !pending {
			continue
		}
		outcome, recoveryErr := s.volumeService.ReconcileBackup(ctx, previous, candidate.Name)
		if recoveryErr != nil {
			return ctx, recoveryErr
		}
		if outcome.Status != schedulertypes.Succeeded {
			return ctx, errors.New(outcome.Message)
		}
		for i := range previous.Outcome.Targets {
			if previous.Outcome.Targets[i].ID == candidate.Name {
				previous.Outcome.Targets[i].Status = schedulertypes.Succeeded
			}
		}
	}
	progressCtx := ctx
	ctx = jobcontext.WithExecution(ctx, previous, func(target schedulertypes.TargetOutcome) error { return jobcontext.Progress(progressCtx, target) })
	return ctx, nil
}

func (s *SystemBackupService) failDurableBackupInternal(ctx context.Context, _ string, payload []byte, runErr error) error {
	var command manualSystemBackupInternal
	if err := json.Unmarshal(payload, &command); err != nil {
		return err
	}
	err := s.db.WithContext(ctx).Model(&SystemBackupRun{}).Where("id = ?", command.Checkpoint.BackupID).Updates(map[string]any{"status": SystemBackupStatusFailed, "error": runErr.Error()}).Error
	activitylib.CompleteHandlerActivity(ctx, s.activityService, command.ActivityID, "Arcane system backup created successfully", runErr)
	return err
}

func (s *SystemBackupService) failDurableVolumeBackupsInternal(ctx context.Context, _ string, payload []byte, runErr error) error {
	var command manualSystemVolumesInternal
	if err := json.Unmarshal(payload, &command); err != nil {
		return err
	}
	activitylib.CompleteHandlerActivity(ctx, s.activityService, command.ActivityID, "System-managed volume backups completed", runErr)
	return nil
}
