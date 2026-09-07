package backup

import (
	"context"
	"fmt"
	"time"

	"emperror.dev/errors"
	"github.com/getarcaneapp/arcane/backend/v2/internal/database"
	s3domain "github.com/getarcaneapp/arcane/backend/v2/internal/s3"
	s3util "github.com/getarcaneapp/arcane/backend/v2/pkg/utils/s3"
	backuptypes "github.com/getarcaneapp/arcane/types/v2/backup"
)

var ErrRemoteRepositoryMissing = errors.New("S3 backup storage is missing")

const RemoteDisabledMessage = "S3 backup storage is missing. Remote backups were disabled; edit the policy to resume."

// RemoteSnapshotChecker memoizes targeted checks within one response budget.
// A nil result means the remote copy could not be verified.
func RemoteSnapshotChecker(ctx context.Context, destinations *s3domain.S3DestinationService, root string) func(string, string) *bool {
	checked := make(map[string]*backuptypes.RepositoryObservation)
	deadline := time.Now().Add(10 * time.Second)
	return func(destinationID, snapshotID string) *bool {
		if destinations == nil || root == "" || destinationID == "" || snapshotID == "" {
			return nil
		}
		key := destinationID + ":" + snapshotID
		observation, seen := checked[key]
		if !seen {
			checked[key] = nil
			checkCtx, cancel := context.WithDeadline(ctx, deadline)
			defer cancel()
			configuration, err := destinations.Configuration(checkCtx, destinationID)
			if err != nil {
				return nil
			}
			result, err := s3util.CheckRepository(checkCtx, configuration, root, snapshotID)
			if err != nil {
				return nil
			}
			observation = &result
			checked[key] = observation
		}
		if observation == nil {
			return nil
		}
		return new(observation.Available && observation.SnapshotAvailable)
	}
}

// CheckScheduledRemote permits first-use initialization, but detects lost repositories.
func CheckScheduledRemote(ctx context.Context, db *database.DB, destinations *s3domain.S3DestinationService, table, destinationID, root string) error {
	if destinations == nil {
		return errors.New("S3 destinations are unavailable")
	}
	configuration, err := destinations.Configuration(ctx, destinationID)
	if err != nil {
		return err
	}
	checkCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	result, err := s3util.CheckRepository(checkCtx, configuration, root, "")
	if err != nil || result.Available {
		return err
	}
	if result.Reason == s3util.RepositoryReasonMissingRepository {
		var retained int64
		if err := db.WithContext(ctx).Table(table).Where("s3_destination_id = ? AND COALESCE(remote_snapshot_id, '') <> ''", destinationID).Count(&retained).Error; err != nil {
			return err
		}
		if retained == 0 {
			return nil
		}
	}
	return fmt.Errorf("%w: %s", ErrRemoteRepositoryMissing, result.Reason)
}
