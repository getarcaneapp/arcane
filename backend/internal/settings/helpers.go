package settings

import (
	"cmp"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"uuid"

	"go.getarcane.app/kit/pkg"
	"gorm.io/gorm"

	"github.com/getarcaneapp/arcane/backend/v3/internal/config"
	"github.com/getarcaneapp/arcane/backend/v3/internal/database"
	"github.com/getarcaneapp/arcane/backend/v3/pkg/utils"
)

type Env map[string]string

// EnsureInstanceID returns this installation's persisted instance ID, creating it on first start.
func EnsureInstanceID(ctx context.Context, db *database.DB) (string, error) {
	return ensureSettingValue(ctx, db, "instanceId", "instance ID", func() (string, error) { return uuid.New().String(), nil })
}

func ResolveEncryptionKey(ctx context.Context, db *database.DB, cfg *config.Config) (string, error) {
	if !cfg.AgentMode && cfg.Environment.IsProdEnvironment() {
		return cfg.EncryptionKey, nil
	}
	var stored SettingVariable
	if err := db.WithContext(ctx).Where("key = ?", "encryptionKey").Limit(1).Find(&stored).Error; err != nil {
		return "", fmt.Errorf("failed to load encryption key: %w", err)
	}
	switch {
	case stored.Value == "" && cfg.EncryptionKey == "":
		return ensureSettingValue(ctx, db, "encryptionKey", "encryption key", func() (string, error) {
			sum := sha256.Sum256([]byte(uuid.New().String()))
			return base64.StdEncoding.EncodeToString(sum[:]), nil
		})
	case stored.Value != "" && cfg.EncryptionKey != "" && stored.Value != cfg.EncryptionKey:
		slog.WarnContext(ctx, "Ignoring ARCANE_ENCRYPTION_KEY because existing secrets are encrypted with the stored encryption key")
	}
	return cmp.Or(stored.Value, cfg.EncryptionKey), nil
}

func EnvNames() []string {
	fields, _ := settingsFields()
	names := make([]string, 0, len(fields))
	for _, field := range fields {
		if !field.isInternal {
			names = append(names, field.envName)
		}
	}
	return names
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

func EnvName(key, envTag string) string {
	return cmp.Or(envTag, "ARCANE_"+strings.ToUpper(kit.SnakeCase(key)))
}

func LoadEnv() (Env, error) {
	fields, _ := settingsFields()
	env := Env{}
	var errs []error
	for _, field := range fields {
		if field.isInternal {
			continue
		}
		raw, _, _ := utils.LookupEnvOrFile(field.envName)
		value := kit.TrimQuotes(raw)
		if value == "" {
			continue
		}
		if err := validateValue(field, value); err != nil {
			errs = append(errs, fmt.Errorf("invalid environment variable %s: %w", field.envName, err))
			continue
		}
		env[field.key] = value
	}
	if err := errors.Join(errs...); err != nil {
		return nil, err
	}
	return env, nil
}

func (e Env) Variables() map[string]string {
	_, byKey := settingsFields()
	variables := make(map[string]string, len(e))
	for key, value := range e {
		variables[byKey[key].envName] = value
	}
	return variables
}

func (e Env) ValueOrDefault(key string) string {
	_, byKey := settingsFields()
	return cmp.Or(e[key], byKey[key].defaultValue)
}
