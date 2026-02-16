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
	"path/filepath"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"github.com/hashicorp/go-uuid"
	"github.com/robfig/cron/v3"
	"gorm.io/gorm"

	"github.com/getarcaneapp/arcane/backend/internal/config"
	"github.com/getarcaneapp/arcane/backend/internal/database"
	"github.com/getarcaneapp/arcane/backend/internal/models"
	"github.com/getarcaneapp/arcane/backend/internal/utils/pathmapper"
	"github.com/getarcaneapp/arcane/backend/internal/utils/stringutils"
	"github.com/getarcaneapp/arcane/types/settings"
)

type SettingsService struct {
	db     *database.DB
	config atomic.Pointer[models.Settings]

	OnImagePollingSettingsChanged      func(ctx context.Context)
	OnAutoUpdateSettingsChanged        func(ctx context.Context)
	OnProjectsDirectoryChanged         func(ctx context.Context)
	OnScheduledPruneSettingsChanged    func(ctx context.Context)
	OnVulnerabilityScanSettingsChanged func(ctx context.Context)
	OnTimeoutSettingsChanged           func(ctx context.Context, timeoutSettings map[string]string)
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
		ProjectsDirectory:             models.SettingVariable{Value: "/app/data/projects"},
		DiskUsagePath:                 models.SettingVariable{Value: "/app/data/projects"},
		AutoUpdate:                    models.SettingVariable{Value: "false"},
		AutoUpdateInterval:            models.SettingVariable{Value: "0 0 0 * * *"},
		AutoUpdateExcludedContainers:  models.SettingVariable{Value: ""},
		PollingEnabled:                models.SettingVariable{Value: "true"},
		PollingInterval:               models.SettingVariable{Value: "0 0 * * * *"},
		EventCleanupInterval:          models.SettingVariable{Value: "0 0 */6 * * *"},
		AnalyticsHeartbeatInterval:    models.SettingVariable{Value: "0 0 0 * * *"},
		AutoInjectEnv:                 models.SettingVariable{Value: "false"},
		PruneMode:                     models.SettingVariable{Value: "dangling"},
		ScheduledPruneEnabled:         models.SettingVariable{Value: "false"},
		ScheduledPruneInterval:        models.SettingVariable{Value: "0 0 0 * * *"},
		ScheduledPruneContainers:      models.SettingVariable{Value: "true"},
		ScheduledPruneImages:          models.SettingVariable{Value: "true"},
		ScheduledPruneVolumes:         models.SettingVariable{Value: "false"},
		ScheduledPruneNetworks:        models.SettingVariable{Value: "true"},
		ScheduledPruneBuildCache:      models.SettingVariable{Value: "false"},
		GitopsSyncInterval:            models.SettingVariable{Value: "0 */1 * * * *"},
		BaseServerURL:                 models.SettingVariable{Value: "http://localhost"},
		EnableGravatar:                models.SettingVariable{Value: "true"},
		DefaultShell:                  models.SettingVariable{Value: "/bin/sh"},
		DockerHost:                    models.SettingVariable{Value: "unix:///var/run/docker.sock"},
		AuthLocalEnabled:              models.SettingVariable{Value: "true"},
		AuthSessionTimeout:            models.SettingVariable{Value: "1440"},
		AuthPasswordPolicy:            models.SettingVariable{Value: "strong"},
		VulnerabilityScanEnabled:      models.SettingVariable{Value: "false"},
		VulnerabilityScanInterval:     models.SettingVariable{Value: "0 0 0 * * *"},
		TrivyImage:                    models.SettingVariable{Value: "ghcr.io/aquasecurity/trivy:latest"},
		TrivyResourceLimitsEnabled:    models.SettingVariable{Value: "true"},
		TrivyCpuLimit:                 models.SettingVariable{Value: "1"},
		TrivyMemoryLimitMb:            models.SettingVariable{Value: "0"},
		TrivyConcurrentScanContainers: models.SettingVariable{Value: "1"},
		// AuthOidcConfig DEPRECATED will be removed in a future release
		AuthOidcConfig:             models.SettingVariable{Value: "{}"},
		OidcEnabled:                models.SettingVariable{Value: "false"},
		OidcClientId:               models.SettingVariable{Value: ""},
		OidcClientSecret:           models.SettingVariable{Value: ""},
		OidcIssuerUrl:              models.SettingVariable{Value: ""},
		OidcAuthorizationEndpoint:  models.SettingVariable{Value: ""},
		OidcTokenEndpoint:          models.SettingVariable{Value: ""},
		OidcUserinfoEndpoint:       models.SettingVariable{Value: ""},
		OidcJwksEndpoint:           models.SettingVariable{Value: ""},
		OidcScopes:                 models.SettingVariable{Value: "openid email profile"},
		OidcAdminClaim:             models.SettingVariable{Value: ""},
		OidcAdminValue:             models.SettingVariable{Value: ""},
		OidcSkipTlsVerify:          models.SettingVariable{Value: "false"},
		OidcAutoRedirectToProvider: models.SettingVariable{Value: "false"},
		OidcMergeAccounts:          models.SettingVariable{Value: "false"},
		OidcProviderName:           models.SettingVariable{Value: ""},
		OidcProviderLogoUrl:        models.SettingVariable{Value: ""},
		MobileNavigationMode:       models.SettingVariable{Value: "floating"},
		MobileNavigationShowLabels: models.SettingVariable{Value: "true"},
		SidebarHoverExpansion:      models.SettingVariable{Value: "true"},
		KeyboardShortcutsEnabled:   models.SettingVariable{Value: "true"},
		AccentColor:                models.SettingVariable{Value: "oklch(0.606 0.25 292.717)"},
		MaxImageUploadSize:         models.SettingVariable{Value: "500"},
		EnvironmentHealthInterval:  models.SettingVariable{Value: "0 */2 * * * *"},

		DockerAPITimeout:       models.SettingVariable{Value: "30"},
		DockerImagePullTimeout: models.SettingVariable{Value: "600"},
		TrivyScanTimeout:       models.SettingVariable{Value: "900"},
		GitOperationTimeout:    models.SettingVariable{Value: "300"},
		HTTPClientTimeout:      models.SettingVariable{Value: "30"},
		RegistryTimeout:        models.SettingVariable{Value: "30"},
		ProxyRequestTimeout:    models.SettingVariable{Value: "60"},

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
		isInternal := slices.Contains(tagParts[1:], "internal")

		if isInternal {
			if val, ok := settingsMap[key]; ok {
				rv.Field(i).FieldByName("Value").SetString(val)
			}
			continue
		}

		envVarName := stringutils.CamelCaseToScreamingSnakeCase(key)

		// debug: log each env name checked and whether a value exists
		if val, ok := os.LookupEnv(envVarName); ok {
			mask := "<empty>"
			if len(val) > 0 {
				mask = fmt.Sprintf("%d chars", len(val))
			}
			slog.DebugContext(ctx, "loadDatabaseConfigFromEnv: env override found", "key", key, "env", envVarName, "valueMasked", mask)
			rv.Field(i).FieldByName("Value").SetString(stringutils.TrimQuotes(val))
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
		hasEnvOverride := slices.Contains(parts[1:], "envOverride")

		if !hasEnvOverride {
			continue
		}

		// Check if environment variable is set
		envVarName := stringutils.CamelCaseToScreamingSnakeCase(key)
		if val, ok := os.LookupEnv(envVarName); ok && val != "" {
			slog.DebugContext(ctx, "applyEnvOverrides: applying env override", "key", key, "env", envVarName)
			rv.Field(i).FieldByName("Value").SetString(stringutils.TrimQuotes(val))
		}
	}
}

// isEnvOverrideActiveInternal returns true when the given setting key has an envOverride tag
// and its corresponding environment variable is currently set to a non-empty value.
func (s *SettingsService) isEnvOverrideActiveInternal(key string) bool {
	rt := reflect.TypeFor[models.Settings]()
	for field := range rt.Fields() {
		tagValue := field.Tag.Get("key")
		if tagValue == "" {
			continue
		}

		tagParts := strings.Split(tagValue, ",")
		if len(tagParts) == 0 || tagParts[0] != key {
			continue
		}

		if !slices.Contains(tagParts[1:], "envOverride") {
			return false
		}

		envVarName := stringutils.CamelCaseToScreamingSnakeCase(key)
		val, ok := os.LookupEnv(envVarName)
		return ok && val != ""
	}

	return false
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
		OidcClientId:     new(oidcConfig.ClientID),
		OidcClientSecret: new(oidcConfig.ClientSecret),
		OidcIssuerUrl:    new(oidcConfig.IssuerURL),
		OidcScopes:       new(scopes),
		OidcAdminClaim:   new(oidcConfig.AdminClaim),
		OidcAdminValue:   new(oidcConfig.AdminValue),
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

	valuesToUpdate, changedPolling, changedAutoUpdate, changedScheduledPrune, changedVulnerabilityScan, changedTimeouts, err := s.prepareUpdateValues(updates, cfg, defaultCfg)
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
	if changedScheduledPrune && s.OnScheduledPruneSettingsChanged != nil {
		s.OnScheduledPruneSettingsChanged(ctx)
	}
	if changedVulnerabilityScan && s.OnVulnerabilityScanSettingsChanged != nil {
		s.OnVulnerabilityScanSettingsChanged(ctx)
	}
	if slices.ContainsFunc(valuesToUpdate, func(sv models.SettingVariable) bool { return sv.Key == "projectsDirectory" }) && s.OnProjectsDirectoryChanged != nil {
		s.OnProjectsDirectoryChanged(ctx)
	}
	if len(changedTimeouts) > 0 && s.OnTimeoutSettingsChanged != nil {
		s.OnTimeoutSettingsChanged(ctx, changedTimeouts)
	}

	return settings.ToSettingVariableSlice(false, false), nil
}

// timeoutSettingKeys defines settings keys that should be synced to agents.
// This currently includes timeout settings and Trivy runtime resource limits.
var timeoutSettingKeys = []string{
	"dockerApiTimeout",
	"dockerImagePullTimeout",
	"trivyScanTimeout",
	"gitOperationTimeout",
	"httpClientTimeout",
	"registryTimeout",
	"proxyRequestTimeout",
	"trivyResourceLimitsEnabled",
	"trivyCpuLimit",
	"trivyMemoryLimitMb",
	"trivyConcurrentScanContainers",
}

func (s *SettingsService) prepareUpdateValues(updates settings.Update, cfg, defaultCfg *models.Settings) ([]models.SettingVariable, bool, bool, bool, bool, map[string]string, error) {
	rt := reflect.TypeFor[settings.Update]()
	rv := reflect.ValueOf(updates)
	valuesToUpdate := make([]models.SettingVariable, 0)

	changedPolling := false
	changedAutoUpdate := false
	changedScheduledPrune := false
	changedVulnerabilityScan := false
	changedTimeouts := make(map[string]string)

	for i := 0; i < rt.NumField(); i++ {
		field := rt.Field(i)
		fieldValue := rv.Field(i)

		if fieldValue.Kind() == reflect.Pointer && fieldValue.IsNil() {
			continue
		}

		key, _, _ := strings.Cut(field.Tag.Get("json"), ",")
		var value string
		if fieldValue.Kind() == reflect.Pointer {
			value = fieldValue.Elem().String()
		}

		// Validate cron settings
		cronFields := []string{"scheduledPruneInterval", "autoUpdateInterval", "pollingInterval", "environmentHealthInterval", "eventCleanupInterval", "analyticsHeartbeatInterval", "vulnerabilityScanInterval", "gitopsSyncInterval"}
		if slices.Contains(cronFields, key) && value != "" {
			if _, err := cron.NewParser(cron.Second | cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow).Parse(value); err != nil {
				return nil, false, false, false, false, nil, fmt.Errorf("invalid cron expression for %s: %w", key, err)
			}
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
			return nil, false, false, false, false, nil, fmt.Errorf("failed to update in-memory config for key '%s': %w", key, err)
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
		case "scheduledPruneEnabled", "scheduledPruneInterval", "scheduledPruneContainers", "scheduledPruneImages", "scheduledPruneVolumes", "scheduledPruneNetworks", "scheduledPruneBuildCache":
			changedScheduledPrune = true
		case "vulnerabilityScanEnabled", "vulnerabilityScanInterval", "trivyResourceLimitsEnabled", "trivyCpuLimit", "trivyMemoryLimitMb", "trivyConcurrentScanContainers":
			changedVulnerabilityScan = true
		}

		// Track timeout setting changes
		if slices.Contains(timeoutSettingKeys, key) {
			changedTimeouts[key] = valueToSave
		}
	}

	return valuesToUpdate, changedPolling, changedAutoUpdate, changedScheduledPrune, changedVulnerabilityScan, changedTimeouts, nil
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

func (s *SettingsService) PruneUnknownSettings(ctx context.Context) error {
	allowedKeys := allowedSettingKeys()
	if len(allowedKeys) == 0 {
		return nil
	}

	keys := make([]string, 0, len(allowedKeys))
	for key := range allowedKeys {
		keys = append(keys, key)
	}

	result := s.db.WithContext(ctx).Where("key NOT IN ?", keys).Delete(&models.SettingVariable{})
	if result.Error != nil {
		return fmt.Errorf("failed to prune unknown settings: %w", result.Error)
	}

	if result.RowsAffected > 0 {
		slog.InfoContext(ctx, "Pruned unknown settings", "count", result.RowsAffected)
	}

	return nil
}

func (s *SettingsService) PersistEnvSettingsIfMissing(ctx context.Context) error {
	rt := reflect.TypeFor[models.Settings]()
	appCfg := config.Load()
	isEnvOnlyMode := appCfg.AgentMode || appCfg.UIConfigurationDisabled

	if err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		for field := range rt.Fields() {
			if err := s.processEnvField(ctx, tx, field, isEnvOnlyMode); err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		return err
	}

	// Reload settings after persisting env vars
	return s.LoadDatabaseSettings(ctx)
}

func allowedSettingKeys() map[string]struct{} {
	allowed := make(map[string]struct{})

	settingsType := reflect.TypeFor[models.Settings]()
	for field := range settingsType.Fields() {
		key, _, _ := strings.Cut(field.Tag.Get("key"), ",")
		if key == "" {
			continue
		}
		allowed[key] = struct{}{}
	}

	allowed["encryptionKey"] = struct{}{}

	return allowed
}

func (s *SettingsService) processEnvField(ctx context.Context, tx *gorm.DB, field reflect.StructField, isEnvOnlyMode bool) error {
	tag := field.Tag.Get("key")
	key, attrs, _ := strings.Cut(tag, ",")

	if !s.shouldProcessField(key, attrs, isEnvOnlyMode) {
		return nil
	}

	envVarName := stringutils.CamelCaseToScreamingSnakeCase(key)
	envVal, ok := os.LookupEnv(envVarName)
	if !ok {
		return nil
	}
	envVal = stringutils.TrimQuotes(envVal)

	return s.upsertEnvSetting(ctx, tx, key, envVal)
}

func (s *SettingsService) shouldProcessField(key, attrs string, isEnvOnlyMode bool) bool {
	if key == "" || strings.Contains(attrs, "internal") {
		return false
	}

	// If not in env-only mode, only persist if it's explicitly marked as envOverride
	if !isEnvOnlyMode && !strings.Contains(attrs, "envOverride") {
		return false
	}

	return true
}

func (s *SettingsService) upsertEnvSetting(ctx context.Context, tx *gorm.DB, key, envVal string) error {
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

	return nil
}

func (s *SettingsService) ListSettings(all bool) []models.SettingVariable {
	return s.GetSettingsConfig().ToSettingVariableSlice(all, true)
}

// GetSettingType returns the type from the setting metadata
func (s *SettingsService) GetSettingType(key string) string {
	rt := reflect.TypeFor[models.Settings]()
	for field := range rt.Fields() {
		keyTag := field.Tag.Get("key")
		fieldKey, _, _ := strings.Cut(keyTag, ",")
		if fieldKey == key {
			metaTag := field.Tag.Get("meta")
			parts := strings.SplitSeq(metaTag, ";")
			for part := range parts {
				if after, ok := strings.CutPrefix(part, "type="); ok {
					return after
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

func (s *SettingsService) NormalizeProjectsDirectory(ctx context.Context, projectsDirEnv string) error {
	if projectsDirEnv != "" {
		slog.DebugContext(ctx, "PROJECTS_DIRECTORY environment variable is set, skipping normalization", "value", projectsDirEnv)
		return nil
	}

	var projectsDirSetting models.SettingVariable
	err := s.db.WithContext(ctx).Where("key = ?", "projectsDirectory").First(&projectsDirSetting).Error

	if errors.Is(err, gorm.ErrRecordNotFound) {
		slog.DebugContext(ctx, "No projectsDirectory setting found, skipping normalization")
		return nil
	}

	if err != nil {
		return fmt.Errorf("failed to load projectsDirectory setting: %w", err)
	}

	value := strings.TrimSpace(projectsDirSetting.Value)
	// Detect mapping format (container:host), allowing Windows or Unix container paths.
	isMapping := false
	if strings.Contains(value, ":") {
		// Treat as mapping if the container side looks like an absolute Unix path
		// or a Windows drive path (C:/ or C:\). We purposely avoid splitting on the
		// first colon to not break on Windows drive letters.
		if strings.HasPrefix(value, "/") || pathmapper.IsWindowsDrivePath(value) {
			isMapping = true
		}
	}

	if !filepath.IsAbs(value) && !isMapping {
		// Resolve relative path using current working directory for transparency.
		// Note: In containers, WORKDIR is set to /app so "data/..." becomes "/app/data/...".
		cwd, _ := os.Getwd()
		absPath, absErr := filepath.Abs(value)
		if absErr != nil {
			return fmt.Errorf("failed to resolve relative path to absolute: %w", absErr)
		}
		slog.InfoContext(ctx, "Normalizing projects directory from relative to absolute path", "from", value, "to", absPath, "base", cwd)

		if err := s.UpdateSetting(ctx, "projectsDirectory", absPath); err != nil {
			return fmt.Errorf("failed to update projectsDirectory: %w", err)
		}

		if err := s.LoadDatabaseSettings(ctx); err != nil {
			return fmt.Errorf("failed to reload settings after normalization: %w", err)
		}

		slog.InfoContext(ctx, "Successfully normalized projects directory")
	} else {
		slog.DebugContext(ctx, "Projects directory already normalized or custom, skipping", "value", projectsDirSetting.Value)
	}

	return nil
}
