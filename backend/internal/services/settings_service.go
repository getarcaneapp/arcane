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
	"strings"
	"sync/atomic"
	"time"

	"github.com/hashicorp/go-uuid"
	"gorm.io/gorm"

	"github.com/ofkm/arcane-backend/internal/config"
	"github.com/ofkm/arcane-backend/internal/database"
	"github.com/ofkm/arcane-backend/internal/dto"
	"github.com/ofkm/arcane-backend/internal/models"
	"github.com/ofkm/arcane-backend/internal/utils"
	"github.com/ofkm/arcane-backend/internal/utils/cron"
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
		ProjectsDirectory:          models.SettingVariable{Value: "data/projects"},
		DiskUsagePath:              models.SettingVariable{Value: "data/projects"},
		AutoUpdate:                 models.SettingVariable{Value: "false"},
		AutoUpdateCron:             models.SettingVariable{Value: ""},
		PollingEnabled:             models.SettingVariable{Value: "true"},
		PollingInterval:            models.SettingVariable{Value: "60"},
		PruneMode:                  models.SettingVariable{Value: "dangling"},
		BaseServerURL:              models.SettingVariable{Value: "http://localhost"},
		EnableGravatar:             models.SettingVariable{Value: "true"},
		DefaultShell:               models.SettingVariable{Value: "/bin/sh"},
		DockerHost:                 models.SettingVariable{Value: "unix:///var/run/docker.sock"},
		AuthLocalEnabled:           models.SettingVariable{Value: "true"},
		AuthOidcEnabled:            models.SettingVariable{Value: "false"},
		AuthOidcMergeAccounts:      models.SettingVariable{Value: "false"},
		AuthSessionTimeout:         models.SettingVariable{Value: "1440"},
		AuthPasswordPolicy:         models.SettingVariable{Value: "strong"},
		AuthOidcConfig:             models.SettingVariable{Value: "{}"},
		OnboardingCompleted:        models.SettingVariable{Value: "false"},
		OnboardingSteps:            models.SettingVariable{Value: "[]"},
		MobileNavigationMode:       models.SettingVariable{Value: "floating"},
		MobileNavigationShowLabels: models.SettingVariable{Value: "true"},
		SidebarHoverExpansion:      models.SettingVariable{Value: "true"},
		GlassEffectEnabled:         models.SettingVariable{Value: "false"},
		AccentColor:                models.SettingVariable{Value: "oklch(0.606 0.25 292.717)"},

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

	rt := reflect.ValueOf(dest).Elem().Type()
	rv := reflect.ValueOf(dest).Elem()
	for i := range rt.NumField() {
		field := rt.Field(i)

		key, attrs, _ := strings.Cut(field.Tag.Get("key"), ",")

		if attrs == "internal" {
			var value string
			err := db.WithContext(ctx).
				Model(&models.SettingVariable{}).
				Where("key = ?", key).
				Select("value").
				First(&value).Error
			if err == nil {
				rv.Field(i).FieldByName("Value").SetString(value)
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
		} else {
			slog.DebugContext(ctx, "loadDatabaseConfigFromEnv: env not set", "key", key, "env", envVarName)
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

	return settings, nil
}

func (s *SettingsService) SyncOidcEnvToDatabase(ctx context.Context) ([]models.SettingVariable, error) {
	cfg := config.Load()
	if cfg.OidcClientID == "" || cfg.OidcIssuerURL == "" {
		return nil, errors.New("missing OIDC_CLIENT_ID or OIDC_ISSUER_URL")
	}

	envOidc := models.OidcConfig{
		ClientID:     cfg.OidcClientID,
		ClientSecret: cfg.OidcClientSecret,
		IssuerURL:    cfg.OidcIssuerURL,
		Scopes:       cfg.OidcScopes,
		AdminClaim:   cfg.OidcAdminClaim,
		AdminValue:   cfg.OidcAdminValue,
	}
	b, err := json.Marshal(envOidc)
	if err != nil {
		return nil, fmt.Errorf("marshal oidc config: %w", err)
	}

	trueStr := "true"
	cfgStr := string(b)

	return s.UpdateSettings(ctx, dto.UpdateSettingsDto{
		AuthOidcEnabled: &trueStr,
		AuthOidcConfig:  &cfgStr,
	})
}

func (s *SettingsService) UpdateSetting(ctx context.Context, key, value string) error {
	settingVar := &models.SettingVariable{
		Key:   key,
		Value: value,
	}

	return s.db.WithContext(ctx).Save(settingVar).Error
}

//nolint:gocognit
func (s *SettingsService) UpdateSettings(ctx context.Context, updates dto.UpdateSettingsDto) ([]models.SettingVariable, error) {
	defaultCfg := s.getDefaultSettings()
	cfg, err := s.GetSettings(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to load current settings: %w", err)
	}

	rt := reflect.TypeOf(updates)
	rv := reflect.ValueOf(updates)
	valuesToUpdate := make([]models.SettingVariable, 0)

	changedPolling := false
	changedAutoUpdate := false

	// Iterate through fields using reflection
	for i := 0; i < rt.NumField(); i++ {
		field := rt.Field(i)
		fieldValue := rv.Field(i)

		// Skip if the field is nil (not provided in request)
		if fieldValue.Kind() == reflect.Ptr && fieldValue.IsNil() {
			continue
		}

		// Get the value and json key
		key, _, _ := strings.Cut(field.Tag.Get("json"), ",")
		var value string
		if fieldValue.Kind() == reflect.Ptr {
			value = fieldValue.Elem().String()
		}

		// Validate cron expression if this is the autoUpdateCron field
		if key == "autoUpdateCron" && value != "" {
			if err := cron.ValidateCronExpression(value); err != nil {
				return nil, err
			}
		}

		// Determine the actual value to use and save
		var valueToSave string
		var err error

		if value == "" {
			// Use default value for empty strings
			defaultValue, _, _, _ := defaultCfg.FieldByKey(key)
			valueToSave = defaultValue
			err = cfg.UpdateField(key, defaultValue, true)
		} else {
			// Use the provided value
			valueToSave = value
			err = cfg.UpdateField(key, value, true)
		}

		// Handle internal field errors
		if errors.Is(err, models.SettingSensitiveForbiddenError{}) {
			continue
		} else if err != nil {
			return nil, fmt.Errorf("failed to update in-memory config for key '%s': %w", key, err)
		}

		// Save the correct value to database
		valuesToUpdate = append(valuesToUpdate, models.SettingVariable{
			Key:   key,
			Value: valueToSave,
		})

		switch key {
		case "pollingEnabled", "pollingInterval":
			changedPolling = true
		case "autoUpdate", "autoUpdateCron":
			changedAutoUpdate = true
		}
	}

	err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		for _, setting := range valuesToUpdate {
			if err := tx.Save(&setting).Error; err != nil {
				return fmt.Errorf("failed to update setting %s: %w", setting.Key, err)
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}

	// Merge OIDC config to avoid clearing secret when not provided
	if updates.AuthOidcConfig != nil {
		newCfgStr := *updates.AuthOidcConfig

		var incoming models.OidcConfig
		if err := json.Unmarshal([]byte(newCfgStr), &incoming); err != nil {
			return nil, fmt.Errorf("invalid authOidcConfig JSON: %w", err)
		}

		// Get current settings to preserve existing secret if empty
		current, err := s.GetSettings(ctx)
		if err != nil {
			return nil, fmt.Errorf("failed to load current settings: %w", err)
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
			return nil, fmt.Errorf("failed to marshal merged OIDC config: %w", err)
		}

		if err := s.UpdateSetting(ctx, "authOidcConfig", string(mergedBytes)); err != nil {
			return nil, fmt.Errorf("failed to update authOidcConfig: %w", err)
		}
	}

	if changedPolling && s.OnImagePollingSettingsChanged != nil {
		s.OnImagePollingSettingsChanged(ctx)
	}
	if changedAutoUpdate && s.OnAutoUpdateSettingsChanged != nil {
		s.OnAutoUpdateSettingsChanged(ctx)
	}

	settings, err := s.GetSettings(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to retrieve updated settings: %w", err)
	}

	s.config.Store(settings)

	return settings.ToSettingVariableSlice(false, false), nil
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
	var sv models.SettingVariable
	err := s.db.WithContext(ctx).Where("key = ?", key).First(&sv).Error
	if err != nil {
		return defaultValue
	}
	return sv.IsTrue()
}

func (s *SettingsService) GetIntSetting(ctx context.Context, key string, defaultValue int) int {
	var sv models.SettingVariable
	err := s.db.WithContext(ctx).Where("key = ?", key).First(&sv).Error
	if err != nil {
		return defaultValue
	}
	return sv.AsInt()
}

func (s *SettingsService) GetStringSetting(ctx context.Context, key, defaultValue string) string {
	var sv models.SettingVariable
	err := s.db.WithContext(ctx).Where("key = ?", key).First(&sv).Error
	if err != nil {
		return defaultValue
	}
	return sv.Value
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

	var sv models.SettingVariable
	err := s.db.WithContext(ctx).
		Where("key = ?", keyName).
		First(&sv).Error

	if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		return "", fmt.Errorf("failed to load encryption key: %w", err)
	}

	// If already present and non-empty, return it
	if sv.Value != "" {
		return sv.Value, nil
	}

	notFound := errors.Is(err, gorm.ErrRecordNotFound)

	// Generate uuid -> sha256 -> base64 key (32 bytes raw -> 44 chars base64)
	u, genErr := uuid.GenerateUUID()
	if genErr != nil {
		return "", fmt.Errorf("failed to generate encryption key: %w", genErr)
	}
	sum := sha256.Sum256([]byte(u))
	key := base64.StdEncoding.EncodeToString(sum[:])

	if notFound {
		if createErr := s.db.WithContext(ctx).
			Create(&models.SettingVariable{Key: keyName, Value: key}).Error; createErr != nil {
			return "", fmt.Errorf("failed to persist encryption key: %w", createErr)
		}
		return key, nil
	}

	// Record existed but empty value; update it
	if updErr := s.db.WithContext(ctx).
		Model(&models.SettingVariable{}).
		Where("key = ?", keyName).
		Update("value", key).Error; updErr != nil {
		return "", fmt.Errorf("failed to update encryption key: %w", updErr)
	}

	return key, nil
}
