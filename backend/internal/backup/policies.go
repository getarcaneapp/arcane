package backup

import (
	"context"
	"fmt"
	"strings"

	"emperror.dev/errors"

	"github.com/getarcaneapp/arcane/backend/v2/internal/database"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/utils/schedule"
	backuptypes "github.com/getarcaneapp/arcane/types/v2/backup"
	"gorm.io/gorm"
)

// PolicyReconciliation reconciles a full set of policy updates against the
// existing rows of one backup domain: shared validation, create/update/delete
// in a single transaction, and the scheduled-job swap. Volume and system
// backups both run through it so the flow cannot drift between them.
type PolicyReconciliation[P any] struct {
	// Domain names the backup flavor in validation errors ("volume", "system").
	Domain string
	DB     *database.DB
	// Existing is every policy currently persisted for the reconciled scope.
	Existing []P
	ID       func(*P) string
	// New returns the blank policy for created entries, pre-scoped by the
	// caller (e.g. with the volume name set).
	New   func() P
	Apply func(*P, backuptypes.UpdateBackupPolicy)
	// ValidateUpdate adds per-domain checks on top of the shared ones; nil skips.
	ValidateUpdate func(backuptypes.UpdateBackupPolicy) error
	// S3Configured verifies the destination exists and is usable; its error is
	// returned to the caller verbatim.
	S3Configured func(ctx context.Context, destinationID string) error
	Unregister   func(ctx context.Context, policyID string)
	Reschedule   func(ctx context.Context, policy *P)
}

func (r PolicyReconciliation[P]) Run(ctx context.Context, updates []backuptypes.UpdateBackupPolicy) error {
	if err := r.validateInternal(ctx, updates); err != nil {
		return err
	}
	policies, kept, err := r.buildInternal(updates)
	if err != nil {
		return err
	}
	err = r.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		for i := range policies {
			if saveErr := tx.Save(&policies[i]).Error; saveErr != nil {
				return saveErr
			}
		}
		for i := range r.Existing {
			if _, ok := kept[r.ID(&r.Existing[i])]; !ok {
				if deleteErr := tx.Delete(&r.Existing[i]).Error; deleteErr != nil {
					return deleteErr
				}
			}
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("failed to save %s backup policies: %w", r.Domain, err)
	}
	for i := range r.Existing {
		if _, ok := kept[r.ID(&r.Existing[i])]; !ok {
			r.Unregister(ctx, r.ID(&r.Existing[i]))
		}
	}
	for i := range policies {
		r.Reschedule(ctx, &policies[i])
	}
	return nil
}

// validateInternal checks the shared policy rules and normalizes the updates
// in place (canonical schedule, cleared destination when S3 is off).
func (r PolicyReconciliation[P]) validateInternal(ctx context.Context, updates []backuptypes.UpdateBackupPolicy) error {
	for i := range updates {
		normalized, err := schedule.NormalizeSixField(updates[i].Schedule, r.Domain+" backup")
		if err != nil {
			return err
		}
		updates[i].Schedule = normalized
		if updates[i].RetentionCount < 0 || updates[i].RetentionCount > 3650 {
			return errors.New("retentionCount must be between 0 and 3650")
		}
		if !updates[i].LocalEnabled && !updates[i].S3Enabled {
			return fmt.Errorf("select at least one %s backup destination", r.Domain)
		}
		if updates[i].S3Enabled {
			if strings.TrimSpace(updates[i].S3DestinationID) == "" {
				return fmt.Errorf("select an S3 destination for %s backups", r.Domain)
			}
			if err := r.S3Configured(ctx, updates[i].S3DestinationID); err != nil {
				return err
			}
		} else {
			updates[i].S3DestinationID = ""
		}
		if r.ValidateUpdate != nil {
			if err := r.ValidateUpdate(updates[i]); err != nil {
				return err
			}
		}
	}
	return nil
}

// buildInternal maps the updates onto existing or new policy rows and reports
// which existing IDs survive the reconciliation.
func (r PolicyReconciliation[P]) buildInternal(updates []backuptypes.UpdateBackupPolicy) ([]P, map[string]struct{}, error) {
	byID := make(map[string]P, len(r.Existing))
	for i := range r.Existing {
		byID[r.ID(&r.Existing[i])] = r.Existing[i]
	}
	policies := make([]P, 0, len(updates))
	kept := make(map[string]struct{}, len(updates))
	for _, update := range updates {
		policy := r.New()
		if update.ID != "" {
			var ok bool
			policy, ok = byID[update.ID]
			if !ok {
				return nil, nil, fmt.Errorf("%s backup policy not found", r.Domain)
			}
			if _, duplicate := kept[update.ID]; duplicate {
				return nil, nil, fmt.Errorf("duplicate %s backup policy", r.Domain)
			}
			kept[update.ID] = struct{}{}
		}
		r.Apply(&policy, update)
		policies = append(policies, policy)
	}
	return policies, kept, nil
}
