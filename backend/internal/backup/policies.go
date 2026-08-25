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
// existing policies of one backup domain, then swaps the scheduled jobs.
type PolicyReconciliation[P, U any] struct {
	// Domain names the backup flavor in validation errors ("volume", "system").
	Domain string
	DB     *database.DB
	// Existing is every policy currently persisted for the reconciled scope.
	Existing []P
	ID       func(*P) string
	UpdateID func(U) string
	// New returns the blank policy for created entries, pre-scoped by the
	// caller (e.g. with the volume name set).
	New func() P
	// Build validates an update and applies it to an existing or new policy.
	Build func(ctx context.Context, policy *P, update U) error
	// Persist overrides the default GORM transaction for settings-backed policies.
	Persist    func(ctx context.Context, policies []P) error
	Unregister func(ctx context.Context, policyID string)
	Reschedule func(ctx context.Context, policy *P)
}

func (r PolicyReconciliation[P, U]) Run(ctx context.Context, updates []U) error {
	policies, kept, err := r.buildInternal(ctx, updates)
	if err != nil {
		return err
	}
	if err := r.persistInternal(ctx, policies, kept); err != nil {
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

func (r PolicyReconciliation[P, U]) persistInternal(ctx context.Context, policies []P, kept map[string]struct{}) error {
	if r.Persist != nil {
		return r.Persist(ctx, policies)
	}
	if r.DB == nil {
		return errors.New("backup policy database is unavailable")
	}
	return r.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
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
}

// ValidatePolicyUpdate applies the shared cron, retention, and destination rules used by backup policies.
func ValidatePolicyUpdate(ctx context.Context, domain string, update backuptypes.UpdateBackupPolicy, s3Configured func(context.Context, string) error) (backuptypes.UpdateBackupPolicy, error) {
	normalized, err := schedule.NormalizeSixField(update.Schedule, domain+" backup")
	if err != nil {
		return update, err
	}
	update.Schedule = normalized
	if update.RetentionCount < 0 || update.RetentionCount > 3650 {
		return update, errors.New("retentionCount must be between 0 and 3650")
	}
	if !update.LocalEnabled && !update.S3Enabled {
		return update, fmt.Errorf("select at least one %s backup destination", domain)
	}
	if update.S3Enabled {
		if strings.TrimSpace(update.S3DestinationID) == "" {
			return update, fmt.Errorf("select an S3 destination for %s backups", domain)
		}
		if s3Configured == nil {
			return update, errors.New("S3 backup destinations are unavailable")
		}
		if err := s3Configured(ctx, update.S3DestinationID); err != nil {
			return update, err
		}
	} else {
		update.S3DestinationID = ""
	}
	return update, nil
}

// buildInternal maps the updates onto existing or new policy rows and reports
// which existing IDs survive the reconciliation.
func (r PolicyReconciliation[P, U]) buildInternal(ctx context.Context, updates []U) ([]P, map[string]struct{}, error) {
	byID := make(map[string]P, len(r.Existing))
	for i := range r.Existing {
		byID[r.ID(&r.Existing[i])] = r.Existing[i]
	}
	policies := make([]P, 0, len(updates))
	kept := make(map[string]struct{}, len(updates))
	for _, update := range updates {
		policy := r.New()
		updateID := r.UpdateID(update)
		if updateID != "" {
			var ok bool
			policy, ok = byID[updateID]
			if !ok {
				return nil, nil, fmt.Errorf("%s backup policy not found", r.Domain)
			}
			if _, duplicate := kept[updateID]; duplicate {
				return nil, nil, fmt.Errorf("duplicate %s backup policy", r.Domain)
			}
		}
		if err := r.Build(ctx, &policy, update); err != nil {
			return nil, nil, err
		}
		if policyID := r.ID(&policy); policyID != "" {
			kept[policyID] = struct{}{}
		}
		policies = append(policies, policy)
	}
	return policies, kept, nil
}
