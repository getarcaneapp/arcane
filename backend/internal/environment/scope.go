package environment

import (
	"fmt"

	"gorm.io/gorm"
)

type (
	ScopeRows struct {
		Table          string
		OwnerColumn    string
		OwnerID        string
		EnvironmentIDs []string
	}
)

func IDs(environments []Environment) []string {
	ids := make([]string, 0, len(environments))
	for _, env := range environments {
		ids = append(ids, env.ID)
	}
	return ids
}

func ReplaceScopeRows(tx *gorm.DB, scope ScopeRows) error {
	if err := tx.Table(scope.Table).Where(scope.OwnerColumn+" = ?", scope.OwnerID).Delete(map[string]any{}).Error; err != nil {
		return fmt.Errorf("failed to clear %s scope: %w", scope.Table, err)
	}
	if len(scope.EnvironmentIDs) == 0 {
		return nil
	}
	rows := make([]map[string]any, 0, len(scope.EnvironmentIDs))
	for _, envID := range scope.EnvironmentIDs {
		rows = append(rows, map[string]any{
			scope.OwnerColumn: scope.OwnerID,
			"environment_id":  envID,
		})
	}
	if err := tx.Table(scope.Table).Create(rows).Error; err != nil {
		return fmt.Errorf("failed to set %s scope: %w", scope.Table, err)
	}
	return nil
}
