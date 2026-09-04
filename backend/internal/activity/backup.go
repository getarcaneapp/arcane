package activity

import (
	"context"

	"emperror.dev/errors"
	activitytypes "github.com/getarcaneapp/arcane/types/v2/activity"
)

// FailInterruptedBackups finalizes backup activities before startup admits work.
func (s *ActivityService) FailInterruptedBackups(ctx context.Context) error {
	var pending []Activity
	if err := s.db.WithContext(ctx).Where("status IN ?", []activitytypes.Status{activitytypes.StatusQueued, activitytypes.StatusRunning}).Find(&pending).Error; err != nil {
		return errors.WrapIf(err, "find interrupted backup activities")
	}
	const message = "Backup interrupted by Arcane restart"
	var result error
	for _, entry := range pending {
		if s.isTrackedInternal(entry.ID) {
			continue
		}
		switch entry.Metadata["action"] {
		case "create_volume_backup", "create_system_backup", "run_system_volume_backups", "scheduled_volume_backup", "scheduled_system_backup":
			errMessage := message
			_, err := s.CompleteActivity(ctx, entry.ID, activitytypes.StatusFailed, message, &errMessage)
			result = errors.Combine(result, err)
		}
	}
	return result
}
