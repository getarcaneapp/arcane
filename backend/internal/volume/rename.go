package volume

import (
	"context"
	stderrors "errors"
	"log/slog"
	"strings"

	"emperror.dev/errors"
	cerrdefs "github.com/containerd/errdefs"
	"github.com/getarcaneapp/arcane/backend/v2/internal/common"
	"github.com/getarcaneapp/arcane/backend/v2/internal/database"
	"github.com/getarcaneapp/arcane/backend/v2/internal/event"
	dockerutil "github.com/getarcaneapp/arcane/backend/v2/pkg/dockerutil"
	volumeops "github.com/getarcaneapp/arcane/backend/v2/pkg/libarcane/volumes"
	volumetypes "github.com/getarcaneapp/arcane/types/v2/volume"
	"github.com/moby/moby/client"
	"gorm.io/gorm"
)

// RenameVolume copies an unused volume to a new name and removes the source.
func (s *VolumeService) RenameVolume(ctx context.Context, oldName, newName string, user common.User) (*volumetypes.Volume, error) {
	oldName = strings.TrimSpace(oldName)
	newName = strings.TrimSpace(newName)
	if oldName == "" || newName == "" || oldName == newName {
		return nil, common.ErrVolumeRenameInvalid
	}
	if strings.EqualFold(oldName, s.backupVolumeName) || strings.EqualFold(newName, s.backupVolumeName) {
		return nil, common.ErrVolumeRenameProtected
	}

	defer s.workspaceLocks.Lock(oldName)()

	dockerClient, err := s.dockerService.GetClient(ctx)
	if err != nil {
		s.logVolumeRenameErrorInternal(ctx, oldName, newName, user, "connect", err)
		return nil, errors.WrapIf(err, "failed to connect to Docker")
	}

	source, err := dockerClient.VolumeInspect(ctx, oldName, client.VolumeInspectOptions{})
	if err != nil {
		if cerrdefs.IsNotFound(err) {
			err = common.Classify(common.ErrNotFound, err)
		}
		s.logVolumeRenameErrorInternal(ctx, oldName, newName, user, "inspect", err)
		return nil, errors.WrapIf(err, "inspect source volume")
	}
	if s.isInternalVolumeInternal(volumetypes.NewSummary(source.Volume)) {
		return nil, common.ErrVolumeRenameProtected
	}

	// Stop any read-only browse helper first; a helper mounting the volume would
	// otherwise fail the detached-source check with "in use".
	if stopErr := s.StopHelper(ctx, oldName); stopErr != nil {
		slog.WarnContext(ctx, "could not stop volume browse helper before rename", "volume", oldName, "error", stopErr.Error())
	}

	migration, err := volumeops.PlanRename(ctx, dockerClient, oldName, newName, s.toolsImageInternal())
	if err != nil {
		s.logVolumeRenameErrorInternal(ctx, oldName, newName, user, "plan", err)
		return nil, err
	}
	if err := migration.Apply(ctx); err != nil {
		if cerrdefs.IsInvalidArgument(err) {
			err = common.Classify(common.ErrBadRequest, err)
		}
		s.logVolumeRenameErrorInternal(ctx, oldName, newName, user, "apply", err)
		return nil, err
	}

	if err := s.renameVolumeMetadataInternal(ctx, oldName, newName); err != nil {
		rollbackErr := migration.Rollback(ctx)
		combinedErr := stderrors.Join(errors.WrapIf(err, "rename volume metadata"), rollbackErr)
		s.logVolumeRenameErrorInternal(ctx, oldName, newName, user, "metadata", combinedErr)
		return nil, combinedErr
	}

	if committer, ok := migration.(volumetypes.Committer); ok {
		if err := committer.Commit(ctx); err != nil {
			var cleanupErr *volumetypes.SourceCleanupError
			if errors.As(err, &cleanupErr) {
				// The copy and metadata are committed; only removing the source
				// failed, so the rename itself succeeded.
				slog.WarnContext(ctx, "volume renamed but source volume could not be removed", "oldVolume", oldName, "newVolume", newName, "error", err.Error())
			} else {
				metadataErr := s.renameVolumeMetadataInternal(ctx, newName, oldName)
				rollbackErr := migration.Rollback(ctx)
				combinedErr := stderrors.Join(err, metadataErr, rollbackErr)
				s.logVolumeRenameErrorInternal(ctx, oldName, newName, user, "commit", combinedErr)
				return nil, combinedErr
			}
		}
	}

	s.removeHelperEntry(oldName)
	dockerutil.InvalidateVolumeUsageCache(dockerClient)

	renamed, err := s.GetVolumeByName(ctx, newName)
	if err != nil {
		s.logVolumeRenameErrorInternal(ctx, oldName, newName, user, "inspect-renamed", err)
		return nil, errors.WrapIf(err, "inspect renamed volume")
	}

	metadata := database.JSON{"action": "rename", "oldName": oldName, "newName": newName}
	if logErr := s.eventService.LogVolumeEvent(ctx, event.EventTypeVolumeRename, newName, newName, user.ID, user.Username, "0", metadata); logErr != nil {
		slog.WarnContext(ctx, "could not log volume rename action", "oldVolume", oldName, "newVolume", newName, "error", logErr)
	}

	return renamed, nil
}

// Backup jobs are keyed by policy ID and re-read the policy row on every run,
// so renaming volume_name requires no job rescheduling.
func (s *VolumeService) renameVolumeMetadataInternal(ctx context.Context, oldName, newName string) error {
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

func (s *VolumeService) logVolumeRenameErrorInternal(ctx context.Context, oldName, newName string, user common.User, step string, err error) {
	s.eventService.LogErrorEvent(ctx, event.EventTypeVolumeError, "volume", oldName, oldName, user.ID, user.Username, "0", err, database.JSON{
		"action":  "rename",
		"step":    step,
		"oldName": oldName,
		"newName": newName,
	})
}
