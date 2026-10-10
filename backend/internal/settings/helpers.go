package settings

import (
	"context"
	"fmt"
	"strings"
	"uuid"

	"go.getarcane.app/kit/pkg"
	"gorm.io/gorm"

	"github.com/getarcaneapp/arcane/backend/v2/internal/database"
)

// EnsureInstanceID returns this installation's persisted instance ID, creating it on first start.
func EnsureInstanceID(ctx context.Context, db *database.DB) (string, error) {
	return ensureSettingValue(ctx, db, "instanceId", "instance ID", func() (string, error) { return uuid.New().String(), nil })
}

func ensureSettingValue(ctx context.Context, db *database.DB, key, label string, generate func() (string, error)) (string, error) {
	var value string
	err := db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var setting SettingVariable
		if err := tx.Where("key = ?", key).Limit(1).Find(&setting).Error; err != nil {
			return fmt.Errorf("failed to load %s: %w", label, err)
		}
		if setting.Value != "" {
			value = setting.Value
			return nil
		}

		generated, err := generate()
		if err != nil {
			return err
		}
		if saveErr := tx.Save(&SettingVariable{Key: key, Value: generated}).Error; saveErr != nil {
			return fmt.Errorf("failed to persist %s: %w", label, saveErr)
		}
		value = generated
		return nil
	})
	return value, err
}

func settingEnvName(key string) string {
	return strings.ToUpper(kit.SnakeCase(key))
}
