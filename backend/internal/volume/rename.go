package volume

import (
	"context"
	stderrors "errors"
	"log/slog"
	"strings"

	"emperror.dev/errors"
	"github.com/getarcaneapp/arcane/backend/v2/internal/common"
	"github.com/getarcaneapp/arcane/backend/v2/internal/database"
	"github.com/getarcaneapp/arcane/backend/v2/internal/event"
	dockerutil "github.com/getarcaneapp/arcane/backend/v2/pkg/dockerutil"
	volumeops "github.com/getarcaneapp/arcane/backend/v2/pkg/libarcane/volumes"
	volumetypes "github.com/getarcaneapp/arcane/types/v2/volume"
	"gorm.io/gorm"
)

var (
	ErrVolumeRenameInvalid   = errors.New("source and target volume names must be non-empty and different")
	ErrVolumeRenameProtected = errors.New("Arcane's backup volume cannot be renamed")
)

// RenameVolume copies an unused volume to a new name and removes the source.
func (s *VolumeService) RenameVolume(ctx context.Context, oldName, newName string, user common.User) (*volumetypes.Volume, error) {
	oldName = strings.TrimSpace(oldName)
	newName = strings.TrimSpace(newName)
	if oldName == "" || newName == "" || oldName == newName {
		return nil, ErrVolumeRenameInvalid
	}
	if strings.EqualFold(oldName, s.backupVolumeName) || strings.EqualFold(newName, s.backupVolumeName) {
		return nil, ErrVolumeRenameProtected
	}

	defer s.workspaceLocks.Lock(oldName)()

	dockerClient, err := s.dockerService.GetClient(ctx)
	if err != nil {
		s.logVolumeRenameErrorInternal(ctx, oldName, newName, user, err)
		return nil, errors.WrapIf(err, "failed to connect to Docker")
	}
	if err := s.StopHelper(ctx, oldName); err != nil {
		s.logVolumeRenameErrorInternal(ctx, oldName, newName, user, err)
		return nil, errors.WrapIf(err, "failed to stop volume browse helper")
	}

	migration, err := volumeops.PlanRename(ctx, dockerClient, oldName, newName)
	if err != nil {
		s.logVolumeRenameErrorInternal(ctx, oldName, newName, user, err)
		return nil, err
	}
	if err := migration.Apply(ctx); err != nil {
		s.logVolumeRenameErrorInternal(ctx, oldName, newName, user, err)
		return nil, err
	}

	policies, err := s.renameVolumeMetadataInternal(ctx, oldName, newName)
	if err != nil {
		rollbackErr := migration.Rollback(ctx)
		combinedErr := stderrors.Join(errors.WrapIf(err, "rename volume metadata"), rollbackErr)
		s.logVolumeRenameErrorInternal(ctx, oldName, newName, user, combinedErr)
		return nil, combinedErr
	}

	committer, ok := migration.(volumeops.Committer)
	if !ok {
		rollbackErr := migration.Rollback(ctx)
		metadataErr := s.renameVolumeMetadataWithoutPoliciesInternal(ctx, newName, oldName)
		combinedErr := stderrors.Join(errors.New("volume rename migration cannot be committed"), metadataErr, rollbackErr)
		s.logVolumeRenameErrorInternal(ctx, oldName, newName, user, combinedErr)
		return nil, combinedErr
	}
	if err := committer.Commit(ctx); err != nil {
		oldExists, inspectErr := volumeops.VolumeExists(ctx, dockerClient, oldName)
		if inspectErr != nil || oldExists {
			metadataErr := s.renameVolumeMetadataWithoutPoliciesInternal(ctx, newName, oldName)
			rollbackErr := migration.Rollback(ctx)
			combinedErr := stderrors.Join(err, inspectErr, metadataErr, rollbackErr)
			s.logVolumeRenameErrorInternal(ctx, oldName, newName, user, combinedErr)
			return nil, combinedErr
		}
	}

	for i := range policies {
		s.rescheduleVolumeBackupPolicyInternal(ctx, &policies[i])
	}
	s.removeHelperEntry(oldName)
	dockerutil.InvalidateVolumeUsageCache(dockerClient)

	renamed, err := s.GetVolumeByName(ctx, newName)
	if err != nil {
		s.logVolumeRenameErrorInternal(ctx, oldName, newName, user, err)
		return nil, errors.WrapIf(err, "inspect renamed volume")
	}

	metadata := database.JSON{"action": "rename", "oldName": oldName, "newName": newName}
	if logErr := s.eventService.LogVolumeEvent(ctx, event.EventTypeVolumeRename, newName, newName, user.ID, user.Username, "0", metadata); logErr != nil {
		slog.WarnContext(ctx, "could not log volume rename action", "oldVolume", oldName, "newVolume", newName, "error", logErr)
	}

	return renamed, nil
}

func (s *VolumeService) renameVolumeMetadataInternal(ctx context.Context, oldName, newName string) ([]VolumeBackupPolicy, error) {
	if s.db == nil {
		return nil, nil
	}

	var policies []VolumeBackupPolicy
	err := s.db.WithContext(ctx).Where("volume_name = ?", oldName).Find(&policies).Error
	if err != nil {
		return nil, errors.WrapIf(err, "load volume backup policies")
	}
	if err := s.renameVolumeMetadataWithoutPoliciesInternal(ctx, oldName, newName); err != nil {
		return nil, err
	}
	for i := range policies {
		policies[i].VolumeName = newName
	}
	return policies, nil
}

func (s *VolumeService) renameVolumeMetadataWithoutPoliciesInternal(ctx context.Context, oldName, newName string) error {
	if s.db == nil {
		return nil
	}

	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Model(&VolumeBackup{}).Where("volume_name = ?", oldName).Update("volume_name", newName).Error; err != nil {
			return errors.WrapIf(err, "rename volume backup history")
		}
		if err := tx.Model(&VolumeBackupPolicy{}).Where("volume_name = ?", oldName).Update("volume_name", newName).Error; err != nil {
			return errors.WrapIf(err, "rename volume backup policies")
		}
		return nil
	})
}

func (s *VolumeService) logVolumeRenameErrorInternal(ctx context.Context, oldName, newName string, user common.User, err error) {
	s.eventService.LogErrorEvent(ctx, event.EventTypeVolumeError, "volume", oldName, oldName, user.ID, user.Username, "0", err, database.JSON{
		"action":  "rename",
		"oldName": oldName,
		"newName": newName,
	})
}
