package backup

import (
	"context"

	"github.com/getarcaneapp/arcane/backend/v2/internal/database"
)

// ExpiredRunIDs returns the IDs of succeeded runs with snapshots that fall
// outside the newest keep entries for one policy, oldest last. Callers delete
// each run through their own delete path so snapshots are forgotten too.
func ExpiredRunIDs(ctx context.Context, db *database.DB, table, policyID string, keep int) ([]string, error) {
	var ids []string
	err := db.WithContext(ctx).
		Table(table).
		Where(
			"policy_id = ? AND status = ? AND (COALESCE(local_snapshot_id, '') <> '' OR COALESCE(remote_snapshot_id, '') <> '')",
			policyID,
			"succeeded",
		).
		Order("created_at DESC").
		Offset(keep).
		Pluck("id", &ids).Error
	return ids, err
}
