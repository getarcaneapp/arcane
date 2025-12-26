package services

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"reflect"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"github.com/hashicorp/go-uuid"
	"gorm.io/gorm"

	"github.com/getarcaneapp/arcane/backend/internal/config"
	"github.com/getarcaneapp/arcane/backend/internal/database"
	"github.com/getarcaneapp/arcane/backend/internal/models"
	"github.com/getarcaneapp/arcane/backend/internal/utils"
	"github.com/getarcaneapp/arcane/types/settings"
)

type SettingsService struct {
	db     *database.DB
	config atomic.Pointer[models.Settings]

	OnImagePollingSettingsChanged func(ctx context.Context)
	OnAutoUpdateSettingsChanged   func(ctx context.Context)
}

func NewSettingsService(ctx context.Context, db *database.DB) (*SettingsService, error) {
	svc := &SettingsService{
		db: db,
	}

	err := svc.LoadDatabaseSettings(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to load settings: %w", err)
	}

	err = svc.setupInstanceID(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to setup instance ID: %w", err)
	}

	if err = svc.LoadDatabaseSettings(ctx); err != nil {
		return nil, fmt.Errorf("failed to reload settings after instance ID setup: %w", err)
	}

	return svc, nil
}

func (s *SettingsService) GetSettingsConfig() *models.Settings {
	v := s.config.Load()
	if v == nil {
		panic("GetSettingsConfig called before Settings has been loaded")
	}

	return v
}

func (s *SettingsService) LoadDatabaseSettings(ctx context.Context) (err error) {
	dst, err := s.loadDatabaseSettingsInternal(ctx, s.db)
	if err != nil {
		return err
	}

	s.config.Store(dst)

	return nil
}

func (s *SettingsService) getDefaultSettings() *models.Settings {
	return &models.Settings{
		ProjectsDirectory:  models.SettingVariable{Value: "data/projects"},
		DiskUsagePath:      models.SettingVariable{Value: "data/projects"},
		AutoUpdate:         models.SettingVariable{Value: "false"},
		AutoUpdateInterval: models.SettingVariable{Value: "1440"},
		PollingEnabled:     models.SettingVariable{Value: "true"},
		PollingInterval:    models.SettingVariable{Value: "60"},
		AutoInjectEnv:      models.SettingVariable{Value: "false"},
		PruneMode:          models.SettingVariable{Value: "dangling"},
		BaseServerURL:      models.SettingVariable{Value: "http://localhost"},
		EnableGravatar:     models.SettingVariable{Value: "true"},
		DefaultShell:       models.SettingVariable{Value: "/bin/sh"},
		DockerHost:         models.SettingVariable{Value: "unix:///var/run/docker.sock"},
		AuthLocalEnabled:   models.SettingVariable{Value: "true"},
		AuthSessionTimeout: models.SettingVariable{Value: "1440"},
		AuthPasswordPolicy: models.SettingVariable{Value: "strong"},
		// AuthOidcConfig DEPRECATED will be removed in a future release
		AuthOidcConfig:             models.SettingVariable{Value: "{}"},
		OidcEnabled:                models.SettingVariable{Value: "false"},
		OidcClientId:               models.SettingVariable{Value: ""},
		OidcClientSecret:           models.SettingVariable{Value: ""},
		OidcIssuerUrl:              models.SettingVariable{Value: ""},
		OidcScopes:                 models.SettingVariable{Value: "openid email profile"},
		OidcAdminClaim:             models.SettingVariable{Value: ""},
		OidcAdminValue:             models.SettingVariable{Value: ""},
		OidcMergeAccounts:          models.SettingVariable{Value: "false"},
		MobileNavigationMode:       models.SettingVariable{Value: "floating"},
		MobileNavigationShowLabels: models.SettingVariable{Value: "true"},
		SidebarHoverExpansion:      models.SettingVariable{Value: "true"},
		GlassEffectEnabled:         models.SettingVariable{Value: "false"},
		AccentColor:                models.SettingVariable{Value: "oklch(0.606 0.25 292.717)"},
		MaxImageUploadSize:         models.SettingVariable{Value: "500"},
		EnvironmentHealthInterval:  models.SettingVariable{Value: "2"},

		InstanceID: models.SettingVariable{Value: ""},
	}
}

func (s *SettingsService) loadDatabaseSettingsInternal(ctx context.Context, db *database.DB) (*models.Settings, error) {
	if config.Load().UIConfigurationDisabled || config.Load().AgentMode {
		slog.DebugContext(ctx, "loadDatabaseSettingsInternal: using env path", "UIConfigurationDisabled", config.Load().UIConfigurationDisabled, "AgentMode", config.Load().AgentMode, "Environment", config.Load().Environment)
		return s.loadDatabaseConfigFromEnv(ctx, db)
	}

	dest := s.getDefaultSettings()

	var loaded []models.SettingVariable
	queryCtx, queryCancel := context.WithTimeout(ctx, 10*time.Second)
	defer queryCancel()
	err := db.
		WithContext(queryCtx).
		Find(&loaded).Error
	if err != nil {
		return nil, fmt.Errorf("failed to load configuration from the database: %w", err)
	}

	for _, v := range loaded {
		err = dest.UpdateField(v.Key, v.Value, false)

		if err != nil && !errors.Is(err, models.SettingKeyNotFoundError{}) {
			return nil, fmt.Errorf("failed to process settings for key '%s': %w", v.Key, err)
		}
	}

	// Apply environment variable overrides for fields tagged with "envOverride"
	s.applyEnvOverrides(ctx, dest)

	return dest, nil
}

func (s *SettingsService) loadDatabaseConfigFromEnv(ctx context.Context, db *database.DB) (*models.Settings, error) {
	dest := s.getDefaultSettings()

	// Fetch all settings once to avoid N+1 queries for internal keys
	var allSettings []models.SettingVariable
	if err := db.WithContext(ctx).Find(&allSettings).Error; err != nil {
		return nil, fmt.Errorf("failed to load settings for env config: %w", err)
	}
	settingsMap := make(map[string]string, len(allSettings))
	for _, s := range allSettings {
		settingsMap[s.Key] = s.Value
	}

	rt := reflect.ValueOf(dest).Elem().Type()
	rv := reflect.ValueOf(dest).Elem()
	for i := range rt.NumField() {
		field := rt.Field(i)

		tagParts := strings.Split(field.Tag.Get("key"), ",")
		key := tagParts[0]
		isInternal := false
		for _, attr := range tagParts[1:] {
			if attr == "internal" {
				isInternal = true
				break
			}
		}

		if isInternal {
			if val, ok := settingsMap[key]; ok {
				rv.Field(i).FieldByName("Value").SetString(val)
			}
			continue
		}

		envVarName := utils.CamelCaseToScreamingSnakeCase(key)

		// debug: log each env name checked and whether a value exists
		if val, ok := os.LookupEnv(envVarName); ok {
			mask := "<empty>"
			if len(val) > 0 {
				mask = fmt.Sprintf("%d chars", len(val))
			}
			slog.DebugContext(ctx, "loadDatabaseConfigFromEnv: env override found", "key", key, "env", envVarName, "valueMasked", mask)
			rv.Field(i).FieldByName("Value").SetString(val)
			continue
		} else if val, ok := settingsMap[key]; ok {
			// Fallback to database if environment variable is not set
			slog.DebugContext(ctx, "loadDatabaseConfigFromEnv: using database fallback", "key", key)
			rv.Field(i).FieldByName("Value").SetString(val)
			continue
		} else {
			slog.DebugContext(ctx, "loadDatabaseConfigFromEnv: env not set and no database value", "key", key, "env", envVarName)
		}
	}

	// debug: final snapshot (only show which fields are non-empty)
	count := 0
	for i := range rt.NumField() {
		v := rv.Field(i).FieldByName("Value").String()
		if v != "" {
			count++
		}
	}
	slog.DebugContext(ctx, "loadDatabaseConfigFromEnv: completed env load", "loadedFields", count)

	return dest, nil
}

func (s *SettingsService) applyEnvOverrides(ctx context.Context, dest *models.Settings) {
	rt := reflect.ValueOf(dest).Elem().Type()
	rv := reflect.ValueOf(dest).Elem()

	for i := range rt.NumField() {
		field := rt.Field(i)
		tagValue := field.Tag.Get("key")
		if tagValue == "" {
			continue
		}

		// Parse tag attributes (e.g., "dockerHost,public,envOverride")
		parts := strings.Split(tagValue, ",")
		key := parts[0]
		hasEnvOverride := false
		for _, attr := range parts[1:] {
			if attr == "envOverride" {
				hasEnvOverride = true
				break
			}
		}

		if !hasEnvOverride {
			continue
		}

		// Check if environment variable is set
		envVarName := utils.CamelCaseToScreamingSnakeCase(key)
		if val, ok := os.LookupEnv(envVarName); ok && val != "" {
			slog.DebugContext(ctx, "applyEnvOverrides: applying env override", "key", key, "env", envVarName)
			rv.Field(i).FieldByName("Value").SetString(val)
		}
	}
}

func (s *SettingsService) GetSettings(ctx context.Context) (*models.Settings, error) {
	var settingVars []models.SettingVariable
	err := s.db.WithContext(ctx).Find(&settingVars).Error
	if err != nil {
		return nil, err
	}

	settings := &models.Settings{}

	for _, sv := range settingVars {
		if err := settings.UpdateField(sv.Key, sv.Value, false); err != nil {
			var notFoundErr models.SettingKeyNotFoundError
			if !errors.As(err, &notFoundErr) {
				return nil, fmt.Errorf("failed to load setting %s: %w", sv.Key, err)
			}
		}
	}

	// Apply environment variable overrides for fields tagged with "envOverride".
	// This keeps behavior consistent with the cached settings path (LoadDatabaseSettingsInternal)
	// and allows env vars like OIDC_MERGE_ACCOUNTS to affect runtime behavior.
	s.applyEnvOverrides(ctx, settings)

	return settings, nil
}

// MigrateOidcConfigToFields migrates the legacy JSON authOidcConfig to individual fields,
// and renames legacy auth* keys to their new oidc* names.
// This should be called during bootstrap to ensure existing configurations are preserved.
func (s *SettingsService) MigrateOidcConfigToFields(ctx context.Context) error {
	currentSettings, err := s.GetSettings(ctx)
	if err != nil {
		return fmt.Errorf("failed to get settings for OIDC migration: %w", err)
	}

	// Migrate legacy key names (authOidcEnabled -> oidcEnabled, authOidcMergeAccounts -> oidcMergeAccounts)
	if err := s.migrateOidcKeyNames(ctx); err != nil {
		slog.WarnContext(ctx, "Failed to migrate OIDC key names", "error", err)
		// Continue with JSON migration even if key rename fails
	}

	// Check if migration is needed: if we have authOidcConfig but no oidcClientId
	if currentSettings.AuthOidcConfig.Value == "" || currentSettings.AuthOidcConfig.Value == "{}" {
		slog.DebugContext(ctx, "No OIDC config to migrate")
		return nil
	}

	// If individual fields are already populated, skip migration
	if currentSettings.OidcClientId.Value != "" {
		slog.DebugContext(ctx, "OIDC fields already populated, skipping migration")
		return nil
	}

	var oidcConfig models.OidcConfig
	if err := json.Unmarshal([]byte(currentSettings.AuthOidcConfig.Value), &oidcConfig); err != nil {
		slog.WarnContext(ctx, "Failed to parse legacy OIDC config for migration", "error", err)
		return nil
	}

	// Only migrate if there's actual data
	if oidcConfig.ClientID == "" && oidcConfig.IssuerURL == "" {
		slog.DebugContext(ctx, "Legacy OIDC config is empty, skipping migration")
		return nil
	}

	slog.InfoContext(ctx, "Migrating legacy OIDC config to individual fields")

	scopes := oidcConfig.Scopes
	if scopes == "" {
		scopes = "openid email profile"
	}

	_, err = s.UpdateSettings(ctx, settings.Update{
		OidcClientId:     &oidcConfig.ClientID,
		OidcClientSecret: &oidcConfig.ClientSecret,
		OidcIssuerUrl:    &oidcConfig.IssuerURL,
		OidcScopes:       &scopes,
		OidcAdminClaim:   &oidcConfig.AdminClaim,
		OidcAdminValue:   &oidcConfig.AdminValue,
	})
	if err != nil {
		return fmt.Errorf("failed to migrate OIDC config: %w", err)
	}

	slog.InfoContext(ctx, "Successfully migrated OIDC config to individual fields")
	return nil
}

// migrateOidcKeyNames renames legacy authOidc* keys to new oidc* keys in the database.
func (s *SettingsService) migrateOidcKeyNames(ctx context.Context) error {
	keyMappings := map[string]string{
		"authOidcEnabled":       "oidcEnabled",
		"authOidcMergeAccounts": "oidcMergeAccounts",
		"authOidcClientId":      "oidcClientId",
		"authOidcClientSecret":  "oidcClientSecret",
		"authOidcIssuerUrl":     "oidcIssuerUrl",
		"authOidcScopes":        "oidcScopes",
		"authOidcAdminClaim":    "oidcAdminClaim",
		"authOidcAdminValue":    "oidcAdminValue",
	}

	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		for oldKey, newKey := range keyMappings {
			// Check if old key exists
			var oldSetting models.SettingVariable
			if err := tx.Where("key = ?", oldKey).First(&oldSetting).Error; err != nil {
				if errors.Is(err, gorm.ErrRecordNotFound) {
					continue // Old key doesn't exist, nothing to migrate
				}
				return fmt.Errorf("failed to check old key %s: %w", oldKey, err)
			}

			// Check if new key already exists
			var newSetting models.SettingVariable
			if err := tx.Where("key = ?", newKey).First(&newSetting).Error; err == nil {
				// New key already exists, delete the old one
				if err := tx.Delete(&oldSetting).Error; err != nil {
					return fmt.Errorf("failed to delete old key %s: %w", oldKey, err)
				}
				slog.DebugContext(ctx, "Deleted duplicate legacy key", "oldKey", oldKey, "newKey", newKey)
				continue
			}

			// Rename: update key from old to new
			if err := tx.Model(&oldSetting).Update("key", newKey).Error; err != nil {
				return fmt.Errorf("failed to rename key %s to %s: %w", oldKey, newKey, err)
			}
			slog.InfoContext(ctx, "Migrated OIDC setting key", "oldKey", oldKey, "newKey", newKey)
		}
		return nil
	})
}

func (s *SettingsService) UpdateSetting(ctx context.Context, key, value string) error {
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		settingVar := &models.SettingVariable{
			Key:   key,
			Value: value,
		}
		return tx.Save(settingVar).Error
	})
}

func (s *SettingsService) UpdateSettings(ctx context.Context, updates settings.Update) ([]models.SettingVariable, error) {
	defaultCfg := s.getDefaultSettings()
	cfg, err := s.GetSettings(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to load current settings: %w", err)
	}

	valuesToUpdate, changedPolling, changedAutoUpdate, err := s.prepareUpdateValues(updates, cfg, defaultCfg)
	if err != nil {
		return nil, err
	}

	if err := s.persistSettings(ctx, valuesToUpdate); err != nil {
		return nil, err
	}

	if err := s.handleOidcConfigUpdate(ctx, updates); err != nil {
		return nil, err
	}

	// Reload and store settings BEFORE calling callbacks so they read updated values
	settings, err := s.GetSettings(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to retrieve updated settings: %w", err)
	}

	s.config.Store(settings)

	// Now call callbacks after in-memory config is updated
	if changedPolling && s.OnImagePollingSettingsChanged != nil {
		s.OnImagePollingSettingsChanged(ctx)
	}
	if changedAutoUpdate && s.OnAutoUpdateSettingsChanged != nil {
		s.OnAutoUpdateSettingsChanged(ctx)
	}

	return settings.ToSettingVariableSlice(false, false), nil
}

func (s *SettingsService) prepareUpdateValues(updates settings.Update, cfg, defaultCfg *models.Settings) ([]models.SettingVariable, bool, bool, error) {
	rt := reflect.TypeOf(updates)
	rv := reflect.ValueOf(updates)
	valuesToUpdate := make([]models.SettingVariable, 0)

	changedPolling := false
	changedAutoUpdate := false

	for i := 0; i < rt.NumField(); i++ {
		field := rt.Field(i)
		fieldValue := rv.Field(i)

		if fieldValue.Kind() == reflect.Ptr && fieldValue.IsNil() {
			continue
		}

		key, _, _ := strings.Cut(field.Tag.Get("json"), ",")
		var value string
		if fieldValue.Kind() == reflect.Ptr {
			value = fieldValue.Elem().String()
		}

		var valueToSave string
		var err error

		if value == "" {
			defaultValue, _, _, _ := defaultCfg.FieldByKey(key)
			valueToSave = defaultValue
			err = cfg.UpdateField(key, defaultValue, true)
		} else {
			valueToSave = value
			err = cfg.UpdateField(key, value, true)
		}

		if errors.Is(err, models.SettingSensitiveForbiddenError{}) {
			continue
		} else if err != nil {
			return nil, false, false, fmt.Errorf("failed to update in-memory config for key '%s': %w", key, err)
		}

		valuesToUpdate = append(valuesToUpdate, models.SettingVariable{
			Key:   key,
			Value: valueToSave,
		})

		switch key {
		case "pollingEnabled", "pollingInterval":
			changedPolling = true
		case "autoUpdate", "autoUpdateInterval":
			changedAutoUpdate = true
		}
	}

	return valuesToUpdate, changedPolling, changedAutoUpdate, nil
}

func (s *SettingsService) persistSettings(ctx context.Context, values []models.SettingVariable) error {
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		for _, setting := range values {
			if err := tx.Save(&setting).Error; err != nil {
				return fmt.Errorf("failed to update setting %s: %w", setting.Key, err)
			}
		}
		return nil
	})
}

func (s *SettingsService) handleOidcConfigUpdate(ctx context.Context, updates settings.Update) error {
	// Handle legacy JSON config format (for backward compatibility during migration)
	if updates.AuthOidcConfig != nil {
		newCfgStr := *updates.AuthOidcConfig
		var incoming models.OidcConfig
		if err := json.Unmarshal([]byte(newCfgStr), &incoming); err != nil {
			return fmt.Errorf("invalid authOidcConfig JSON: %w", err)
		}

		current, err := s.GetSettings(ctx)
		if err != nil {
			return fmt.Errorf("failed to load current settings: %w", err)
		}

		if current.AuthOidcConfig.Value != "" {
			var existing models.OidcConfig
			if err := json.Unmarshal([]byte(current.AuthOidcConfig.Value), &existing); err == nil {
				if incoming.ClientSecret == "" {
					incoming.ClientSecret = existing.ClientSecret
				}
			}
		}

		mergedBytes, err := json.Marshal(incoming)
		if err != nil {
			return fmt.Errorf("failed to marshal merged OIDC config: %w", err)
		}

		if err := s.UpdateSetting(ctx, "authOidcConfig", string(mergedBytes)); err != nil {
			return fmt.Errorf("failed to update authOidcConfig: %w", err)
		}
	}

	// Handle new individual field for client secret (sensitive field)
	if updates.OidcClientSecret != nil {
		secret := *updates.OidcClientSecret

		// If empty secret provided, preserve existing secret
		if secret == "" {
			current, err := s.GetSettings(ctx)
			if err != nil {
				return fmt.Errorf("failed to load current settings for secret: %w", err)
			}
			if current.OidcClientSecret.Value != "" {
				// Keep existing secret, don't update
				return nil
			}
		}

		if err := s.UpdateSetting(ctx, "oidcClientSecret", secret); err != nil {
			return fmt.Errorf("failed to update oidcClientSecret: %w", err)
		}
	}

	return nil
}

func (s *SettingsService) EnsureDefaultSettings(ctx context.Context) error {
	defaultSettings := s.getDefaultSettings()
	defaultSettingVars := defaultSettings.ToSettingVariableSlice(true, false)

	if err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		for _, defaultSetting := range defaultSettingVars {
			var existing models.SettingVariable
			err := tx.Where("key = ?", defaultSetting.Key).First(&existing).Error

			if errors.Is(err, gorm.ErrRecordNotFound) {
				if err := tx.Create(&defaultSetting).Error; err != nil {
					return fmt.Errorf("failed to create default setting %s: %w", defaultSetting.Key, err)
				}
			} else if err != nil {
				return fmt.Errorf("failed to check for existing setting %s: %w", defaultSetting.Key, err)
			}
		}
		return nil
	}); err != nil {
		return err
	}

	return nil
}

func (s *SettingsService) PersistEnvSettingsIfMissing(ctx context.Context) error {
	rt := reflect.TypeOf(models.Settings{})

	if err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		for i := 0; i < rt.NumField(); i++ {
			field := rt.Field(i)
			key, attrs, _ := strings.Cut(field.Tag.Get("key"), ",")

			if key == "" || attrs == "internal" {
				continue
			}

			envVarName := utils.CamelCaseToScreamingSnakeCase(key)
			envVal, ok := os.LookupEnv(envVarName)
			if !ok {
				continue
			}

			var existing models.SettingVariable
			err := tx.Where("key = ?", key).First(&existing).Error
			switch {
			case errors.Is(err, gorm.ErrRecordNotFound):
				newVar := models.SettingVariable{Key: key, Value: envVal}
				if err := tx.Create(&newVar).Error; err != nil {
					return fmt.Errorf("persist env setting %s: %w", key, err)
				}
				slog.DebugContext(ctx, "Created setting from environment", "key", key)
			case err != nil:
				return fmt.Errorf("check setting %s: %w", key, err)
			default:
				if existing.Value != envVal {
					if err := tx.Model(&existing).Update("value", envVal).Error; err != nil {
						return fmt.Errorf("update env setting %s: %w", key, err)
					}
					slog.DebugContext(ctx, "Updated setting from environment", "key", key)
				}
			}
		}
		return nil
	}); err != nil {
		return err
	}

	// Reload settings after persisting env vars
	return s.LoadDatabaseSettings(ctx)
}

func (s *SettingsService) ListSettings(all bool) []models.SettingVariable {
	return s.GetSettingsConfig().ToSettingVariableSlice(all, true)
}

// GetSettingType returns the type from the setting metadata
func (s *SettingsService) GetSettingType(key string) string {
	rt := reflect.TypeOf(models.Settings{})
	for i := 0; i < rt.NumField(); i++ {
		field := rt.Field(i)
		keyTag := field.Tag.Get("key")
		fieldKey, _, _ := strings.Cut(keyTag, ",")
		if fieldKey == key {
			metaTag := field.Tag.Get("meta")
			parts := strings.Split(metaTag, ";")
			for _, part := range parts {
				if strings.HasPrefix(part, "type=") {
					return strings.TrimPrefix(part, "type=")
				}
			}
			return "text" // default type
		}
	}
	return "text" // default if not found
}

func (s *SettingsService) setupInstanceID(ctx context.Context) error {
	instanceID := s.GetSettingsConfig().InstanceID.Value
	if instanceID != "" {
		return nil
	}

	createdInstanceID, err := uuid.GenerateUUID()
	if err != nil {
		return fmt.Errorf("failed to created a new instance ID: %w", err)
	}

	err = s.UpdateSetting(ctx, "instanceId", createdInstanceID)
	if err != nil {
		return fmt.Errorf("failed to set instance ID in database: %w", err)
	}

	return nil
}

func (s *SettingsService) GetBoolSetting(ctx context.Context, key string, defaultValue bool) bool {
	cfg := s.GetSettingsConfig()
	val, _, _, err := cfg.FieldByKey(key)
	if err != nil || val == "" {
		return defaultValue
	}
	b, err := strconv.ParseBool(val)
	if err != nil {
		return defaultValue
	}
	return b
}

func (s *SettingsService) GetIntSetting(ctx context.Context, key string, defaultValue int) int {
	cfg := s.GetSettingsConfig()
	val, _, _, err := cfg.FieldByKey(key)
	if err != nil || val == "" {
		return defaultValue
	}
	i, err := strconv.Atoi(val)
	if err != nil {
		return defaultValue
	}
	return i
}

func (s *SettingsService) GetStringSetting(ctx context.Context, key, defaultValue string) string {
	cfg := s.GetSettingsConfig()
	val, _, _, err := cfg.FieldByKey(key)
	if err != nil || val == "" {
		return defaultValue
	}
	return val
}

func (s *SettingsService) SetBoolSetting(ctx context.Context, key string, value bool) error {
	if err := s.UpdateSetting(ctx, key, fmt.Sprintf("%t", value)); err != nil {
		return err
	}
	// Rebuild a fresh snapshot instead of mutating current pointer (avoids races)
	if err := s.LoadDatabaseSettings(ctx); err != nil {
		return fmt.Errorf("failed to refresh settings cache: %w", err)
	}
	return nil
}

func (s *SettingsService) SetIntSetting(ctx context.Context, key string, value int) error {
	if err := s.UpdateSetting(ctx, key, fmt.Sprintf("%d", value)); err != nil {
		return err
	}
	if err := s.LoadDatabaseSettings(ctx); err != nil {
		return fmt.Errorf("failed to refresh settings cache: %w", err)
	}
	return nil
}

func (s *SettingsService) SetStringSetting(ctx context.Context, key, value string) error {
	if err := s.UpdateSetting(ctx, key, value); err != nil {
		return err
	}
	if err := s.LoadDatabaseSettings(ctx); err != nil {
		return fmt.Errorf("failed to refresh settings cache: %w", err)
	}
	return nil
}

func (s *SettingsService) EnsureEncryptionKey(ctx context.Context) (string, error) {
	const keyName = "encryptionKey"
	var key string

	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var sv models.SettingVariable
		err := tx.Where("key = ?", keyName).First(&sv).Error

		if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
			return fmt.Errorf("failed to load encryption key: %w", err)
		}

		// If already present and non-empty, return it
		if sv.Value != "" {
			key = sv.Value
			return nil
		}

		notFound := errors.Is(err, gorm.ErrRecordNotFound)

		// Generate uuid -> sha256 -> base64 key (32 bytes raw -> 44 chars base64)
		u, genErr := uuid.GenerateUUID()
		if genErr != nil {
			return fmt.Errorf("failed to generate encryption key: %w", genErr)
		}
		sum := sha256.Sum256([]byte(u))
		generatedKey := base64.StdEncoding.EncodeToString(sum[:])
		key = generatedKey

		if notFound {
			if createErr := tx.Create(&models.SettingVariable{Key: keyName, Value: generatedKey}).Error; createErr != nil {
				return fmt.Errorf("failed to persist encryption key: %w", createErr)
			}
			return nil
		}

		// Record existed but empty value; update it
		if updErr := tx.Model(&models.SettingVariable{}).
			Where("key = ?", keyName).
			Update("value", generatedKey).Error; updErr != nil {
			return fmt.Errorf("failed to update encryption key: %w", updErr)
		}
		return nil
	})

	if err != nil {
		return "", err
	}

	return key, nil
}
