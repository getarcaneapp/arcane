package environment

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"

	"gorm.io/gorm"

	"kit.getarcane.app"

	"getarcane.dev/arcane/v3/internal/common"
	"getarcane.dev/arcane/v3/internal/database"
)

type (
	ScopeTable struct {
		Table       string
		OwnerColumn string
	}

	ScopeChange struct {
		Submitted       map[string]bool
		Current         []Environment
		EnvironmentIDs  []string
		AllEnvironments bool
		CurrentAll      bool
	}
)

func IDs(environments []Environment) []string {
	ids := make([]string, 0, len(environments))
	for _, env := range environments {
		ids = append(ids, env.ID)
	}
	return ids
}

func (t ScopeTable) Replace(tx *gorm.DB, ownerID string, envIDs []string) error {
	if err := tx.Table(t.Table).Where(t.OwnerColumn+" = ?", ownerID).Delete(map[string]any{}).Error; err != nil {
		return fmt.Errorf("failed to clear %s scope: %w", t.Table, err)
	}
	if len(envIDs) == 0 {
		return nil
	}
	rows := make([]map[string]any, 0, len(envIDs))
	for _, envID := range envIDs {
		rows = append(rows, map[string]any{
			t.OwnerColumn:    ownerID,
			"environment_id": envID,
		})
	}
	if err := tx.Table(t.Table).Create(rows).Error; err != nil {
		return fmt.Errorf("failed to set %s scope: %w", t.Table, err)
	}
	return nil
}

func NormalizeScope(ctx context.Context, db *database.DB, allEnvironments bool, envIDs []string) ([]string, error) {
	if allEnvironments {
		return nil, nil
	}

	unique := make([]string, 0, len(envIDs))
	for _, id := range envIDs {
		id = strings.TrimSpace(id)
		if id != "" && !slices.Contains(unique, id) {
			unique = append(unique, id)
		}
	}
	if len(unique) == 0 {
		return nil, common.Classify(
			common.ErrGlobalVariableScopeRequired,
			errors.New(
				"at least one environment is required when a variable is not scoped to all environments",
			),
		)
	}

	var count int64
	if err := db.WithContext(ctx).Model(&Environment{}).Where("id IN ?", unique).Count(&count).Error; err != nil {
		return nil, fmt.Errorf("failed to update global variables: %w", err)
	}
	if count != int64(len(unique)) {
		return nil, common.Classify(
			common.ErrGlobalVariableUnknownEnvironment,
			errors.New("failed to update global variables: one or more environments do not exist"),
		)
	}
	return unique, nil
}

func ResolveScope(ctx context.Context, db *database.DB, change ScopeChange) ([]string, bool, error) {
	envIDs := IDs(change.Current)
	if !change.Submitted["allEnvironments"] && !change.Submitted["environmentIds"] {
		return envIDs, change.CurrentAll, nil
	}

	normalized, err := NormalizeScope(
		ctx,
		db,
		kit.Ternary(change.Submitted["allEnvironments"], change.AllEnvironments, change.CurrentAll),
		kit.Ternary(change.Submitted["environmentIds"], change.EnvironmentIDs, envIDs),
	)
	if err != nil {
		return nil, false, err
	}
	return normalized, len(normalized) == 0, nil
}
