package settings

import (
	"context"
	"crypto/mldsa"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"uuid"

	"github.com/getarcaneapp/arcane/types/v2/base"
	"github.com/getarcaneapp/arcane/types/v2/category"
	"github.com/getarcaneapp/arcane/types/v2/features"
	searchtypes "github.com/getarcaneapp/arcane/types/v2/search"
	"github.com/getarcaneapp/arcane/types/v2/settings"
	"github.com/samber/mo"
	"go.getarcane.app/kit/normalization"
	"go.getarcane.app/kit/pkg"
	"go.getarcane.app/sys/crypto"
	"gorm.io/gorm"

	"github.com/getarcaneapp/arcane/backend/v2/internal/common"
	"github.com/getarcaneapp/arcane/backend/v2/internal/config"
	"github.com/getarcaneapp/arcane/backend/v2/internal/database"
	"github.com/getarcaneapp/arcane/backend/v2/internal/search"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/libarcane"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/projects"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/utils"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/utils/concurrency"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/utils/httpx"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/utils/validation"
)

const (
	browserSessionSigningKeySize = 64
)

type SettingsService struct {
	db     *database.DB
	config atomic.Pointer[Settings]
	// effectiveConfig is the shared, read-only env-override-applied snapshot,
	// rebuilt whenever config is stored so reads are a pointer load.
	effectiveConfig atomic.Pointer[Settings]
	envOverrides    []settingsEnvOverride
	writes          sync.Mutex
	effectsMu       sync.Mutex
	effects         []func()
	effectsWake     chan struct{}
	effectsDone     chan struct{}
	effectsClosed   bool
	lifecycleCtx    context.Context
	changes         *concurrency.Signal[[]libarcane.SettingUpdate]
}

type settingsEnvOverride struct {
	fieldIndex int
	key        string
	value      string
}

func NewSettingsService(ctx context.Context, db *database.DB) (*SettingsService, error) {
	if ctx == nil {
		return nil, errors.New("settings lifecycle context unavailable")
	}
	svc := &SettingsService{
		db:           db,
		effectsWake:  make(chan struct{}, 1),
		effectsDone:  make(chan struct{}),
		lifecycleCtx: ctx,
		changes:      concurrency.NewSignal[[]libarcane.SettingUpdate](),
	}
	fields, _ := getSettingsFieldCacheInternal()
	for _, field := range fields {
		if !slices.Contains(splitSettingAttrsInternal(field.attrs), "envOverride") {
			continue
		}
		if val, ok, _ := utils.LookupEnvOrFile(settingEnvName(field.key)); ok && val != "" {
			svc.envOverrides = append(svc.envOverrides, settingsEnvOverride{fieldIndex: field.index, key: field.key, value: kit.TrimQuotes(val)})
		}
	}
	if len(svc.envOverrides) > 0 {
		slog.InfoContext(ctx, "Loaded Environment Settings Overrides", "count", len(svc.envOverrides))
	}

	if _, err := EnsureInstanceID(ctx, db); err != nil {
		return nil, fmt.Errorf("failed to setup instance ID: %w", err)
	}
	if err := svc.LoadDatabaseSettings(ctx); err != nil {
		return nil, fmt.Errorf("failed to load settings: %w", err)
	}

	go func() {
		defer close(svc.effectsDone)
		for {
			svc.effectsMu.Lock()
			if len(svc.effects) > 0 {
				effect := svc.effects[0]
				svc.effects[0] = nil
				svc.effects = svc.effects[1:]
				svc.effectsMu.Unlock()
				func() { defer utils.RecoverToError(nil, "settings effect"); effect() }()
				continue
			}
			closed := svc.effectsClosed
			svc.effectsMu.Unlock()
			if closed {
				return
			}
			select {
			case <-svc.effectsWake:
			case <-svc.lifecycleCtx.Done():
				svc.effectsMu.Lock()
				svc.effectsClosed = true
				svc.effectsMu.Unlock()
			}
		}
	}()
	return svc, nil
}

// SubscribeSettingsChanges invokes callback once with the matching values when any subscribed key
// is present in a successful update. Callbacks run serially on the effects worker, outside the write mutex.
func (s *SettingsService) SubscribeSettingsChanges(keys []string, callback func([]libarcane.SettingUpdate)) func() {
	if callback == nil || len(keys) == 0 {
		return func() {}
	}
	subscribed := make(map[string]struct{}, len(keys))
	for _, key := range keys {
		subscribed[key] = struct{}{}
	}
	return s.changes.Subscribe(func(updates []libarcane.SettingUpdate) {
		matching := make([]libarcane.SettingUpdate, 0, len(updates))
		for _, update := range updates {
			if _, ok := subscribed[update.Key]; ok {
				matching = append(matching, update)
			}
		}
		if len(matching) > 0 {
			if err := s.enqueueEffect(func() { callback(matching) }); err != nil && s.lifecycleCtx.Err() == nil {
				slog.ErrorContext(s.lifecycleCtx, "Failed to queue settings change effects", "error", err)
			}
		}
	})
}

// NotifySettingsChanges publishes current values for keys through the settings
// write mutex. It is used when another settings-table workflow performs persistence.
func (s *SettingsService) NotifySettingsChanges(ctx context.Context, keys ...string) error {
	s.writes.Lock()
	defer s.writes.Unlock()
	if err := ctx.Err(); err != nil {
		return err
	}
	updates := make([]libarcane.SettingUpdate, 0, len(keys))
	cfg := s.GetSettingsConfig()
	for _, key := range keys {
		value, _, _, err := cfg.FieldByKey(key)
		if err != nil {
			return fmt.Errorf("load changed setting '%s': %w", key, err)
		}
		updates = append(updates, libarcane.SettingUpdate{Key: key, Value: value})
	}
	s.publishSettingsChanges(updates)
	return nil
}

func (s *SettingsService) GetSettingsConfig() *Settings {
	v := s.config.Load()
	if v == nil {
		panic("GetSettingsConfig called before Settings has been loaded")
	}

	return v
}

func (s *SettingsService) LoadDatabaseSettings(ctx context.Context) error {
	var stored []SettingVariable
	queryCtx, queryCancel := context.WithTimeout(ctx, 10*time.Second)
	defer queryCancel()
	if err := s.db.WithContext(queryCtx).Find(&stored).Error; err != nil {
		return fmt.Errorf("failed to load configuration from the database: %w", err)
	}

	dst := DefaultSettingsConfig()
	appCfg := config.Load()
	fields, _ := getSettingsFieldCacheInternal()
	rv := reflect.ValueOf(dst).Elem()
	if appCfg.UIConfigurationDisabled || appCfg.AgentMode {
		slog.DebugContext(ctx, "LoadDatabaseSettings: using env path",
			"uiConfigurationDisabled", appCfg.UIConfigurationDisabled, "agentMode", appCfg.AgentMode, "environment", appCfg.Environment)

		storedValues := make(map[string]string, len(stored))
		for _, setting := range stored {
			storedValues[setting.Key] = setting.Value
		}
		for _, field := range fields {
			value := rv.Field(field.index).FieldByName("Value")
			if slices.Contains(splitSettingAttrsInternal(field.attrs), "internal") {
				if val, ok := storedValues[field.key]; ok {
					value.SetString(val)
				}
				continue
			}

			envVarName := settingEnvName(field.key)
			if val, ok, _ := utils.LookupEnvOrFile(envVarName); ok {
				mask := kit.Ternary(val == "", "<empty>", fmt.Sprintf("%d chars", len(val)))
				slog.DebugContext(ctx, "LoadDatabaseSettings: env override found", "key", field.key, "env", envVarName, "valueMasked", mask)
				value.SetString(kit.TrimQuotes(val))
				continue
			}
			if val, ok := storedValues[field.key]; ok {
				slog.DebugContext(ctx, "LoadDatabaseSettings: using database fallback", "key", field.key)
				value.SetString(val)
				continue
			}
			slog.DebugContext(ctx, "LoadDatabaseSettings: env not set and no database value", "key", field.key, "env", envVarName)
		}

		count := 0
		for _, field := range fields {
			if rv.Field(field.index).FieldByName("Value").String() != "" {
				count++
			}
		}
		slog.DebugContext(ctx, "LoadDatabaseSettings: completed env load", "loadedFields", count)
	} else {
		for _, setting := range stored {
			if err := dst.UpdateField(setting.Key, setting.Value, false); err != nil && !errors.Is(err, SettingKeyNotFoundError{}) {
				return fmt.Errorf("failed to process settings for key '%s': %w", setting.Key, err)
			}
		}
		s.applyEnvOverrides(dst)
	}

	s.config.Store(dst)

	effective := dst.Clone()
	s.applyEnvOverrides(effective)
	s.effectiveConfig.Store(effective)

	return nil
}

// DefaultSettingsConfig returns the canonical default settings model used by Arcane.
func DefaultSettingsConfig() *Settings {
	return &Settings{
		ProjectsDirectory:                     SettingVariable{Value: "/app/data/projects"},
		TemplatesDirectory:                    SettingVariable{Value: "/app/data/templates"},
		FollowProjectSymlinks:                 SettingVariable{Value: "false"},
		SwarmStackSourcesDirectory:            SettingVariable{Value: "/app/data/swarm/sources"},
		DiskUsagePath:                         SettingVariable{Value: "/app/data/projects"},
		AutoUpdate:                            SettingVariable{Value: "false"},
		AutoUpdateInterval:                    SettingVariable{Value: "0 0 0 * * *"},
		AutoUpdateExcludedContainers:          SettingVariable{Value: ""},
		PollingEnabled:                        SettingVariable{Value: "true"},
		PollingInterval:                       SettingVariable{Value: "0 0 * * * *"},
		ImageEventWatcherEnabled:              SettingVariable{Value: "false"},
		DockerClientRefreshInterval:           SettingVariable{Value: "0 */5 * * * *"},
		EventCleanupInterval:                  SettingVariable{Value: "0 0 */6 * * *"},
		ExpiredSessionsCleanupInterval:        SettingVariable{Value: "0 0 0 * * *"},
		ActivityHistoryRetentionDays:          SettingVariable{Value: "30"},
		UpgradeLogRetentionDays:               SettingVariable{Value: "3"},
		ActivityHistoryMaxEntries:             SettingVariable{Value: "1000"},
		MaxConcurrentActivities:               SettingVariable{Value: "5"},
		AutoInjectEnv:                         SettingVariable{Value: "false"},
		DefaultDeployPullPolicy:               SettingVariable{Value: "missing"},
		ToolsImageRegistry:                    SettingVariable{Value: "ghcr.io"},
		UpdateCheckRegistry:                   SettingVariable{Value: "auto"},
		ScheduledPruneEnabled:                 SettingVariable{Value: "false"},
		ScheduledPruneInterval:                SettingVariable{Value: "0 0 0 * * *"},
		PruneContainerMode:                    SettingVariable{Value: "stopped"},
		PruneContainerUntil:                   SettingVariable{Value: ""},
		PruneImageMode:                        SettingVariable{Value: "dangling"},
		PruneImageUntil:                       SettingVariable{Value: ""},
		PruneVolumeMode:                       SettingVariable{Value: "none"},
		PruneNetworkMode:                      SettingVariable{Value: "unused"},
		PruneNetworkUntil:                     SettingVariable{Value: ""},
		PruneBuildCacheMode:                   SettingVariable{Value: "none"},
		PruneBuildCacheUntil:                  SettingVariable{Value: ""},
		AutoHealEnabled:                       SettingVariable{Value: "false"},
		AutoHealInterval:                      SettingVariable{Value: "0 */5 * * * *"},
		AutoHealExcludedContainers:            SettingVariable{Value: ""},
		AutoHealMaxRestarts:                   SettingVariable{Value: "5"},
		AutoHealRestartWindow:                 SettingVariable{Value: "30"},
		VolumeHelperIdleTimeout:               SettingVariable{Value: "10"},
		BaseServerURL:                         SettingVariable{Value: "http://localhost"},
		EnableGravatar:                        SettingVariable{Value: "false"},
		ExperimentalFeaturesEnabled:           SettingVariable{Value: "false"},
		DevelopmentBrandingEnabled:            SettingVariable{Value: "true"},
		AvatarMaxUploadSizeMb:                 SettingVariable{Value: "2"},
		DefaultShell:                          SettingVariable{Value: "/bin/sh"},
		DockerHost:                            SettingVariable{Value: "unix:///var/run/docker.sock"},
		BuildsDirectory:                       SettingVariable{Value: "/builds"},
		AuthLocalEnabled:                      SettingVariable{Value: "true"},
		AuthSessionTimeout:                    SettingVariable{Value: "1440"},
		AuthPasswordPolicy:                    SettingVariable{Value: "strong"},
		FeatureVulnerabilityManagementEnabled: SettingVariable{Value: "true"},
		FeatureSwarmEnabled:                   SettingVariable{Value: "false"},
		VulnerabilityScanEnabled:              SettingVariable{Value: "false"},
		VulnerabilityScanInterval:             SettingVariable{Value: "0 0 0 * * *"},
		VulnerabilityThreatIntelEnabled:       SettingVariable{Value: "true"},
		TrivyDbRegistry:                       SettingVariable{Value: "ghcr.io"},
		TrivyNetwork:                          SettingVariable{Value: ""},
		TrivySecurityOpts:                     SettingVariable{Value: ""},
		TrivyPrivileged:                       SettingVariable{Value: "false"},
		TrivyResourceLimitsEnabled:            SettingVariable{Value: "true"},
		TrivyCpuLimit:                         SettingVariable{Value: "1"},
		TrivyMemoryLimitMb:                    SettingVariable{Value: "0"},
		TrivyConcurrentScanContainers:         SettingVariable{Value: "1"},
		TrivyServerEnabled:                    SettingVariable{Value: "false"},
		TrivyServerUrl:                        SettingVariable{Value: ""},
		TrivyServerToken:                      SettingVariable{Value: ""},
		TrivyIgnoreUnfixed:                    SettingVariable{Value: "true"},
		OidcEnabled:                           SettingVariable{Value: "false"},
		OidcClientId:                          SettingVariable{Value: ""},
		OidcClientSecret:                      SettingVariable{Value: ""},
		OidcIssuerUrl:                         SettingVariable{Value: ""},
		OidcAuthorizationEndpoint:             SettingVariable{Value: ""},
		OidcTokenEndpoint:                     SettingVariable{Value: ""},
		OidcUserinfoEndpoint:                  SettingVariable{Value: ""},
		OidcJwksEndpoint:                      SettingVariable{Value: ""},
		OidcScopes:                            SettingVariable{Value: "openid email profile"},
		OidcGroupsClaim:                       SettingVariable{Value: "groups"},
		OidcSkipTlsVerify:                     SettingVariable{Value: "false"},
		OidcAutoRedirectToProvider:            SettingVariable{Value: "false"},
		OidcMergeAccounts:                     SettingVariable{Value: "false"},
		OidcProviderName:                      SettingVariable{Value: ""},
		OidcProviderLogoUrl:                   SettingVariable{Value: ""},
		OidcMobileRedirectUris:                SettingVariable{Value: "arcane-mobile://oidc-callback"},
		MaxImageUploadSize:                    SettingVariable{Value: "500"},
		GitSyncMaxFiles:                       SettingVariable{Value: "500"},
		GitSyncMaxTotalSizeMb:                 SettingVariable{Value: "50"},
		GitSyncMaxBinarySizeMb:                SettingVariable{Value: "10"},
		EnvironmentHealthInterval:             SettingVariable{Value: "0 */2 * * * *"},
		LifecycleEnabled:                      SettingVariable{Value: "false"},
		LifecycleDefaultRunnerImage:           SettingVariable{Value: "alpine:latest"},
		LifecycleMaxTimeoutSec:                SettingVariable{Value: "300"},

		DockerAPITimeout:       SettingVariable{Value: "30"},
		DockerImagePullTimeout: SettingVariable{Value: "600"},
		TrivyScanTimeout:       SettingVariable{Value: "900"},
		ImagePatchSuffix:       SettingVariable{Value: "patched"},
		ImagePatchTimeoutSec:   SettingVariable{Value: "600"},
		ImagePatchAllPlatforms: SettingVariable{Value: "false"},
		ImageAutoPatchEnabled:  SettingVariable{Value: "false"},
		ImageAutoPatchInterval: SettingVariable{Value: "0 0 3 * * *"},
		GitOperationTimeout:    SettingVariable{Value: "300"},
		HTTPClientTimeout:      SettingVariable{Value: "30"},
		RegistryTimeout:        SettingVariable{Value: "30"},
		RegistryTagTimeout:     SettingVariable{Value: "120"},
		ProxyRequestTimeout:    SettingVariable{Value: "60"},
		DeployWaitTimeout:      SettingVariable{Value: "600"},
		BuildProvider:          SettingVariable{Value: "local"},
		BuildTimeout:           SettingVariable{Value: "1800"},
		DepotProjectId:         SettingVariable{Value: ""},
		DepotToken:             SettingVariable{Value: ""},

		ApnsEnabled:    SettingVariable{Value: "false"},
		ApnsChannelID:  SettingVariable{Value: ""},
		ApnsSigningKey: SettingVariable{Value: ""},

		InstanceID:               SettingVariable{Value: ""},
		SystemVolumeBackupConfig: SettingVariable{Value: `{"policies":[]}`},
	}
}

func (s *SettingsService) applyEnvOverrides(dest *Settings) {
	rv := reflect.ValueOf(dest).Elem()

	for _, override := range s.envOverrides {
		rv.Field(override.fieldIndex).FieldByName("Value").SetString(override.value)
	}
}

// IsEnvOverrideActive reports whether a non-empty environment override controls the setting.
func (s *SettingsService) IsEnvOverrideActive(key string) bool {
	return slices.ContainsFunc(s.envOverrides, func(override settingsEnvOverride) bool { return override.key == key })
}

// GetSettings returns the shared, read-only effective (env-override-applied) settings snapshot.
// Mutation flows go through UpdateSettings, which works on an explicit clone.
func (s *SettingsService) GetSettings(_ context.Context) (*Settings, error) {
	if effective := s.effectiveConfig.Load(); effective != nil {
		return effective, nil
	}

	// Only reachable before LoadDatabaseSettings has completed in the
	// constructor; fall back to building the snapshot per call.
	settingsCfg := s.GetSettingsConfig().Clone()
	s.applyEnvOverrides(settingsCfg)
	return settingsCfg, nil
}

// GetSettingsOrDefaults logs any settings load failure and always returns a non-nil *Settings;
// a zero-valued struct makes SettingVariable helpers fall back to the caller's default.
func (s *SettingsService) GetSettingsOrDefaults(ctx context.Context) *Settings {
	cfg, err := s.GetSettings(ctx)
	if err != nil {
		slog.WarnContext(ctx, "failed to load settings, falling back to defaults", "error", err)
	}
	return kit.Ternary(cfg == nil, &Settings{}, cfg)
}

func validateSettingValue(key, value string) error {
	if key == "upgradeLogRetentionDays" && value != "" {
		days, err := strconv.Atoi(value)
		if err != nil || days < 0 || days > 3650 {
			return common.Classify(common.ErrValidation, errors.New("upgradeLogRetentionDays must be a whole number between 0 and 3650"))
		}
	}
	for _, definition := range features.All() {
		if definition.SettingKey == key && value != "true" && value != "false" {
			return common.Classify(common.ErrValidation, fmt.Errorf("%s must be true or false", key))
		}
	}
	return nil
}

func (s *SettingsService) UpdateSetting(ctx context.Context, key, value string) error {
	if err := validateSettingValue(key, value); err != nil {
		return err
	}
	if err := libarcane.ValidateCronSetting(key, value); err != nil {
		return fmt.Errorf("invalid cron expression for %s: %w", key, err)
	}
	s.writes.Lock()
	defer s.writes.Unlock()
	if err := ctx.Err(); err != nil {
		return err
	}

	return s.persistSettings(ctx, []SettingVariable{{Key: key, Value: value}})
}

// UpdateSettingValues persists a group of internal setting values atomically
// and publishes the refreshed snapshot before returning.
func (s *SettingsService) UpdateSettingValues(ctx context.Context, updates []libarcane.SettingUpdate) error {
	values := make([]SettingVariable, 0, len(updates))
	for _, update := range updates {
		if err := validateSettingValue(update.Key, update.Value); err != nil {
			return err
		}
		values = append(values, SettingVariable{Key: update.Key, Value: update.Value})
	}
	s.writes.Lock()
	defer s.writes.Unlock()
	if err := ctx.Err(); err != nil {
		return err
	}

	return s.persistSettings(ctx, values)
}

// UpdateSettings publishes the refreshed snapshot before returning. Each
// subscriber's effects remain ordered and may finish after this method returns.
func (s *SettingsService) UpdateSettings(ctx context.Context, updates settings.Update) ([]SettingVariable, error) {
	if err := normalization.Normalize(&updates); err != nil {
		return nil, err
	}
	s.writes.Lock()
	defer s.writes.Unlock()
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	cfg := s.GetSettingsConfig().Clone()
	oidcClientSecretUpdated := updates.OidcClientSecret != nil && !s.IsEnvOverrideActive("oidcClientSecret")
	trivyServerTokenUpdated := updates.TrivyServerToken != nil && *updates.TrivyServerToken != "" && !s.IsEnvOverrideActive("trivyServerToken")
	normalizeTargetURL := func(value string) string {
		normalized, err := httpx.NormalizeBaseURL(value)
		if err != nil {
			return strings.TrimSpace(value)
		}
		return normalized
	}

	if err := validation.ValidateCredentialTargetChange(
		"OIDC issuer URL",
		cfg.OidcIssuerUrl.Value,
		updates.OidcIssuerUrl,
		normalizeTargetURL,
		map[string]bool{"oidcClientSecret": cfg.OidcClientSecret.Value != ""},
		map[string]bool{"oidcClientSecret": updates.OidcClientSecret != nil},
	); err != nil {
		return nil, err
	}

	effectiveValue := func(current SettingVariable, next *string) string {
		if next != nil {
			return *next
		}
		return current.Value
	}
	oidcClientSecret := kit.Ternary(oidcClientSecretUpdated, kit.FromPtr(updates.OidcClientSecret), cfg.OidcClientSecret.Value)
	if effectiveValue(cfg.OidcEnabled, updates.OidcEnabled) == "true" {
		required := []struct{ key, value string }{
			{"oidcClientId", effectiveValue(cfg.OidcClientId, updates.OidcClientId)},
			{"oidcClientSecret", oidcClientSecret},
			{"oidcIssuerUrl", effectiveValue(cfg.OidcIssuerUrl, updates.OidcIssuerUrl)},
		}
		for _, field := range required {
			if strings.TrimSpace(field.value) == "" {
				return nil, common.Classify(common.ErrValidation, &base.FieldError{Field: field.key, Err: fmt.Errorf("enabling OIDC requires %s", field.key)})
			}
		}
	}

	if err := validation.ValidateCredentialTargetChange(
		"Trivy server URL",
		cfg.TrivyServerUrl.Value,
		updates.TrivyServerUrl,
		normalizeTargetURL,
		map[string]bool{"trivyServerToken": cfg.TrivyServerToken.Value != ""},
		map[string]bool{"trivyServerToken": trivyServerTokenUpdated},
	); err != nil {
		return nil, err
	}

	valuesToUpdate, err := s.prepareUpdateValues(updates, cfg, DefaultSettingsConfig())
	if err != nil {
		return nil, err
	}
	if oidcClientSecretUpdated {
		valuesToUpdate = append(valuesToUpdate, SettingVariable{Key: "oidcClientSecret", Value: *updates.OidcClientSecret})
	}
	if trivyServerTokenUpdated {
		valuesToUpdate = append(valuesToUpdate, SettingVariable{Key: "trivyServerToken", Value: *updates.TrivyServerToken})
	}

	if persistSettingsErr := s.persistSettings(ctx, valuesToUpdate); persistSettingsErr != nil {
		return nil, persistSettingsErr
	}

	changes := make([]libarcane.SettingUpdate, 0, len(valuesToUpdate))
	for _, value := range valuesToUpdate {
		changes = append(changes, libarcane.SettingUpdate{Key: value.Key, Value: value.Value})
	}
	result := s.GetSettingsConfig().ToSettingVariableSlice(SettingVisibilityNonAdmin, false)
	s.publishSettingsChanges(changes)
	return result, nil
}

func (s *SettingsService) publishSettingsChanges(updates []libarcane.SettingUpdate) {
	safe := make([]libarcane.SettingUpdate, 0, len(updates))
	cfg := s.GetSettingsConfig()
	for _, update := range updates {
		_, _, sensitive, err := cfg.FieldByKey(update.Key)
		if err != nil {
			slog.WarnContext(s.lifecycleCtx, "Skipping settings change notification for unknown key", "key", update.Key, "error", err)
			continue
		}
		if !sensitive {
			safe = append(safe, update)
		}
	}
	if len(safe) > 0 {
		s.changes.Publish(safe)
	}
}

func (s *SettingsService) prepareUpdateValues(updates settings.Update, cfg, defaultCfg *Settings) ([]SettingVariable, error) {
	rt := reflect.TypeFor[settings.Update]()
	rv := reflect.ValueOf(updates)
	valuesToUpdate := make([]SettingVariable, 0)

	for i := range rt.NumField() {
		fieldValue := rv.Field(i)
		if fieldValue.Kind() == reflect.Pointer && fieldValue.IsNil() {
			continue
		}
		key, _, _ := strings.Cut(rt.Field(i).Tag.Get("json"), ",")
		if key == "" {
			continue
		}
		var value string
		if fieldValue.Kind() == reflect.Pointer {
			value = fieldValue.Elem().String()
		}

		if err := validateSettingValue(key, value); err != nil {
			return nil, err
		}

		if key == libarcane.DepotTokenSettingKey {
			// Sensitive token: only update when explicitly provided.
			// Empty input preserves existing token.
			if strings.TrimSpace(value) == "" {
				continue
			}

			if err := cfg.UpdateField(key, value, false); err != nil {
				return nil, fmt.Errorf("failed to update in-memory config for key '%s': %w", key, err)
			}

			valuesToUpdate = append(valuesToUpdate, SettingVariable{Key: key, Value: value})

			continue
		}

		if err := libarcane.ValidateCronSetting(key, value); err != nil {
			return nil, fmt.Errorf("invalid cron expression for %s: %w", key, err)
		}

		valueToSave := value
		if valueToSave == "" {
			valueToSave, _, _, _ = defaultCfg.FieldByKey(key)
		}
		err := cfg.UpdateField(key, valueToSave, true)
		if errors.Is(err, SettingSensitiveForbiddenError{}) {
			continue
		}
		if err != nil {
			return nil, fmt.Errorf("failed to update in-memory config for key '%s': %w", key, err)
		}

		valuesToUpdate = append(valuesToUpdate, SettingVariable{Key: key, Value: valueToSave})
	}

	return valuesToUpdate, nil
}

func (s *SettingsService) persistSettings(ctx context.Context, values []SettingVariable) error {
	if err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		for _, setting := range values {
			if setting.Key == "oidcProviderName" {
				setting.Value = normalization.Text(setting.Value, true, true)
			}
			if err := tx.Save(&setting).Error; err != nil {
				return fmt.Errorf("failed to update setting %s: %w", setting.Key, err)
			}
		}
		return nil
	}); err != nil {
		return err
	}

	if err := s.LoadDatabaseSettings(context.WithoutCancel(ctx)); err != nil {
		return fmt.Errorf("failed to refresh settings cache: %w", err)
	}
	return nil
}

// retiredSettingValues lists former defaults that startup replaces
// with the current default when an install still holds them verbatim.
var retiredSettingValues = map[string][]string{
	"dockerClientRefreshInterval": {"*/30 * * * * *"},
	"autoHealInterval":            {"*/30 * * * * *"},
}

func (s *SettingsService) EnsureDefaultSettings(ctx context.Context) error {
	s.writes.Lock()
	defer s.writes.Unlock()
	if err := ctx.Err(); err != nil {
		return err
	}

	defaultSettingVars := DefaultSettingsConfig().ToSettingVariableSlice(SettingVisibilityAll, false)

	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		for _, defaultSetting := range defaultSettingVars {
			var existing SettingVariable
			err := tx.Where("key = ?", defaultSetting.Key).First(&existing).Error

			switch {
			case errors.Is(err, gorm.ErrRecordNotFound):
				if createDefaultSettingErr := tx.Create(&defaultSetting).Error; createDefaultSettingErr != nil {
					return fmt.Errorf("failed to create default setting %s: %w", defaultSetting.Key, createDefaultSettingErr)
				}
			case err != nil:
				return fmt.Errorf("failed to check for existing setting %s: %w", defaultSetting.Key, err)
			case slices.Contains(retiredSettingValues[defaultSetting.Key], existing.Value):
				replaceDefaultSettingErr := tx.Model(&SettingVariable{}).Where("key = ?", defaultSetting.Key).Update("value", defaultSetting.Value).Error
				if replaceDefaultSettingErr != nil {
					return fmt.Errorf("failed to replace retired default for setting %s: %w", defaultSetting.Key, replaceDefaultSettingErr)
				}
			}
		}
		return nil
	})
}

func (s *SettingsService) PruneUnknownSettings(ctx context.Context) error {
	s.writes.Lock()
	defer s.writes.Unlock()
	if err := ctx.Err(); err != nil {
		return err
	}

	fields, _ := getSettingsFieldCacheInternal()
	keys := make([]string, 0, len(fields)+1)
	for _, field := range fields {
		keys = append(keys, field.key)
	}
	keys = append(keys, "encryptionKey")

	result := s.db.WithContext(ctx).Where("key NOT IN ?", keys).Delete(&SettingVariable{})
	if result.Error != nil {
		return fmt.Errorf("failed to prune unknown settings: %w", result.Error)
	}
	if result.RowsAffected > 0 {
		slog.InfoContext(ctx, "Pruned unknown settings", "count", result.RowsAffected)
	}
	return nil
}

func (s *SettingsService) PersistEnvSettingsIfMissing(ctx context.Context) error {
	s.writes.Lock()
	defer s.writes.Unlock()
	if err := ctx.Err(); err != nil {
		return err
	}

	appCfg := config.Load()
	isEnvOnlyMode := appCfg.AgentMode || appCfg.UIConfigurationDisabled
	fields, _ := getSettingsFieldCacheInternal()

	if err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		for _, field := range fields {
			attrs := splitSettingAttrsInternal(field.attrs)
			// Outside env-only mode, only persist fields explicitly marked as envOverride.
			if slices.Contains(attrs, "internal") || (!isEnvOnlyMode && !slices.Contains(attrs, "envOverride")) {
				continue
			}
			envVal, ok, _ := utils.LookupEnvOrFile(settingEnvName(field.key))
			if !ok {
				continue
			}
			envVal = kit.TrimQuotes(envVal)

			var existing SettingVariable
			err := tx.Where("key = ?", field.key).First(&existing).Error
			switch {
			case errors.Is(err, gorm.ErrRecordNotFound):
				if createEnvSettingErr := tx.Create(&SettingVariable{Key: field.key, Value: envVal}).Error; createEnvSettingErr != nil {
					return fmt.Errorf("persist env setting %s: %w", field.key, createEnvSettingErr)
				}
				slog.DebugContext(ctx, "Created setting from environment", "key", field.key)
			case err != nil:
				return fmt.Errorf("check setting %s: %w", field.key, err)
			case existing.Value != envVal:
				if updateEnvSettingErr := tx.Model(&existing).Update("value", envVal).Error; updateEnvSettingErr != nil {
					return fmt.Errorf("update env setting %s: %w", field.key, updateEnvSettingErr)
				}
				slog.DebugContext(ctx, "Updated setting from environment", "key", field.key)
			}
		}
		return nil
	}); err != nil {
		return err
	}
	return s.LoadDatabaseSettings(context.WithoutCancel(ctx))
}

func (s *SettingsService) ListSettings(visibility SettingVisibility) []SettingVariable {
	return s.GetSettingsConfig().ToSettingVariableSlice(visibility, true)
}

// GetSettingType returns the type from the setting metadata
func (s *SettingsService) GetSettingType(key string) string {
	_, byKey := getSettingsFieldCacheInternal()
	if field, ok := byKey[key]; ok {
		metaTag := reflect.TypeFor[Settings]().Field(field.index).Tag.Get("meta")
		for part := range strings.SplitSeq(metaTag, ";") {
			if after, found := strings.CutPrefix(part, "type="); found {
				return after
			}
		}
	}
	return "text"
}

func (s *SettingsService) settingValue[T any](ctx context.Context, key string, parse func(string) (T, error)) mo.Option[T] {
	cfg := s.GetSettingsOrDefaults(ctx)
	value, _, _, err := cfg.FieldByKey(key)
	if err != nil || value == "" {
		return mo.None[T]()
	}

	parsed, err := parse(value)
	if err != nil {
		return mo.None[T]()
	}
	return mo.Some(parsed)
}

func (s *SettingsService) GetBoolSetting(ctx context.Context, key string, defaultValue bool) bool {
	return s.settingValue(ctx, key, strconv.ParseBool).OrElse(defaultValue)
}

func (s *SettingsService) GetIntSetting(ctx context.Context, key string, defaultValue int) int {
	return s.settingValue(ctx, key, strconv.Atoi).OrElse(defaultValue)
}

func (s *SettingsService) GetStringSetting(ctx context.Context, key, defaultValue string) string {
	return s.settingValue(ctx, key, func(value string) (string, error) { return value, nil }).OrElse(defaultValue)
}

func (s *SettingsService) SetBoolSetting(ctx context.Context, key string, value bool) error {
	return s.UpdateSetting(ctx, key, strconv.FormatBool(value))
}

func (s *SettingsService) SetIntSetting(ctx context.Context, key string, value int) error {
	return s.UpdateSetting(ctx, key, strconv.Itoa(value))
}

func (s *SettingsService) SetStringSetting(ctx context.Context, key, value string) error {
	return s.UpdateSetting(ctx, key, value)
}

// SetContainerAutoUpdateExclusionInternal adds (excluded) or removes a container name
// in the autoUpdateExcludedContainers setting.
func (s *SettingsService) SetContainerAutoUpdateExclusionInternal(ctx context.Context, containerName string, excluded bool) error {
	s.writes.Lock()
	defer s.writes.Unlock()
	if err := ctx.Err(); err != nil {
		return err
	}

	ordered := kit.Unique(kit.TrimNonEmpty(strings.Split(s.GetStringSetting(ctx, "autoUpdateExcludedContainers", ""), ",")))
	if excluded {
		ordered = kit.Unique(append(ordered, containerName))
	} else {
		ordered = slices.DeleteFunc(ordered, func(name string) bool { return name == containerName })
	}

	return s.persistSettings(ctx, []SettingVariable{{Key: "autoUpdateExcludedContainers", Value: strings.Join(ordered, ",")}})
}

func (s *SettingsService) EnsureEncryptionKey(ctx context.Context) (string, error) {
	s.writes.Lock()
	defer s.writes.Unlock()
	if err := ctx.Err(); err != nil {
		return "", err
	}

	return ensureSettingValue(ctx, s.db, "encryptionKey", "encryption key", func() (string, error) {
		sum := sha256.Sum256([]byte(uuid.New().String()))
		return base64.StdEncoding.EncodeToString(sum[:]), nil
	})
}

func (s *SettingsService) EnsureJwtSigningKey(ctx context.Context) (*mldsa.PrivateKey, error) {
	seed, err := s.ensureEncryptedKey(ctx, "jwtSigningKeySeed", mldsa.PrivateKeySize)
	if err != nil {
		return nil, err
	}
	key, err := mldsa.NewPrivateKey(mldsa.MLDSA87(), seed)
	if err != nil {
		return key, fmt.Errorf("failed to load jwt signing key: %w", err)
	}
	return key, nil
}

func (s *SettingsService) EnsureBrowserSessionSigningKey(ctx context.Context) ([]byte, error) {
	return s.ensureEncryptedKey(ctx, "browserSessionSigningKey", browserSessionSigningKeySize)
}

func (s *SettingsService) ensureEncryptedKey(ctx context.Context, keyName string, size int) ([]byte, error) {
	s.writes.Lock()
	defer s.writes.Unlock()
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	var generated []byte
	stored, err := ensureSettingValue(ctx, s.db, keyName, "signing key", func() (string, error) {
		generated = make([]byte, size)
		if _, genErr := rand.Read(generated); genErr != nil {
			return "", fmt.Errorf("failed to generate signing key: %w", genErr)
		}
		encrypted, encErr := crypto.Encrypt(base64.StdEncoding.EncodeToString(generated))
		if encErr != nil {
			return "", fmt.Errorf("failed to encrypt signing key: %w", encErr)
		}
		return encrypted, nil
	})
	if err != nil {
		return nil, err
	}
	if generated != nil {
		return generated, nil
	}

	decoded, err := crypto.Decrypt(stored)
	if err != nil {
		return nil, fmt.Errorf("failed to decrypt signing key: %w", err)
	}
	key, err := base64.StdEncoding.DecodeString(decoded)
	if err != nil {
		return nil, fmt.Errorf("failed to decode signing key: %w", err)
	}
	if len(key) != size {
		return nil, fmt.Errorf("invalid signing key length: got %d, want %d", len(key), size)
	}
	return key, nil
}

func (s *SettingsService) NormalizeProjectsDirectory(ctx context.Context, projectsDirEnv string) error {
	if projectsDirEnv != "" {
		slog.DebugContext(ctx, "PROJECTS_DIRECTORY environment variable is set, skipping normalization", "value", projectsDirEnv)
		return nil
	}
	return s.normalizeDirectorySetting(ctx, "projectsDirectory")
}

func (s *SettingsService) NormalizeBuildsDirectory(ctx context.Context) error {
	if envVal, ok, _ := utils.LookupEnvOrFile(settingEnvName("buildsDirectory")); ok && strings.TrimSpace(envVal) != "" {
		slog.DebugContext(ctx, "BUILDS_DIRECTORY environment variable is set, skipping normalization", "value", envVal)
		return nil
	}
	return s.normalizeDirectorySetting(ctx, "buildsDirectory")
}

func (s *SettingsService) normalizeDirectorySetting(ctx context.Context, key string) error {
	s.writes.Lock()
	defer s.writes.Unlock()
	if err := ctx.Err(); err != nil {
		return err
	}

	var setting SettingVariable
	err := s.db.WithContext(ctx).Where("key = ?", key).First(&setting).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		slog.DebugContext(ctx, "No directory setting found, skipping normalization", "key", key)
		return nil
	}
	if err != nil {
		return fmt.Errorf("failed to load %s setting: %w", key, err)
	}

	value := strings.TrimSpace(setting.Value)
	isMapping := strings.Contains(value, ":") && (strings.HasPrefix(value, "/") || projects.IsWindowsDrivePath(value))
	if value == "" || filepath.IsAbs(value) || isMapping {
		slog.DebugContext(ctx, "Directory setting already normalized, custom, or empty, skipping", "key", key, "value", setting.Value)
		return nil
	}

	cwd, _ := os.Getwd()
	absPath, err := filepath.Abs(value)
	if err != nil {
		return fmt.Errorf("failed to resolve relative path to absolute: %w", err)
	}
	slog.InfoContext(ctx, "Normalizing directory setting from relative to absolute path", "key", key, "from", value, "to", absPath, "base", cwd)
	if persistSettingsErr := s.persistSettings(ctx, []SettingVariable{{Key: key, Value: absPath}}); persistSettingsErr != nil {
		return persistSettingsErr
	}
	slog.InfoContext(ctx, "Successfully normalized directory setting", "key", key)
	s.publishSettingsChanges([]libarcane.SettingUpdate{{Key: key, Value: absPath}})
	return nil
}

func (s *SettingsService) enqueueEffect(effect func()) error {
	s.effectsMu.Lock()
	defer s.effectsMu.Unlock()
	if s.effectsClosed {
		return errors.New("settings effects stopped")
	}
	s.effects = append(s.effects, effect)
	select {
	case s.effectsWake <- struct{}{}:
	default:
	}
	return nil
}

// Stop drains accepted effects and joins the settings worker.
func (s *SettingsService) Stop(ctx context.Context) error {
	s.writes.Lock()
	s.effectsMu.Lock()
	s.effectsClosed = true
	s.effectsMu.Unlock()
	s.writes.Unlock()
	select {
	case s.effectsWake <- struct{}{}:
	default:
	}
	select {
	case <-s.effectsDone:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// IsFeatureEnabled resolves feature availability on this environment.
func (s *SettingsService) IsFeatureEnabled(ctx context.Context, id features.ID) bool {
	definition, ok := features.Lookup(id)
	if !ok {
		return false
	}
	if s == nil {
		return definition.DefaultEnabled
	}
	cfg := s.GetSettingsOrDefaults(ctx)
	value, _, _, err := cfg.FieldByKey(definition.SettingKey)
	if err != nil || value == "" {
		return definition.DefaultEnabled
	}
	enabled, err := strconv.ParseBool(value)
	return kit.Ternary(err != nil, definition.DefaultEnabled, enabled)
}

// RequireFeature rejects operations when their runtime feature is disabled.
func (s *SettingsService) RequireFeature(ctx context.Context, id features.ID) error {
	if s.IsFeatureEnabled(ctx, id) {
		return nil
	}
	return fmt.Errorf("feature %s is disabled: %w", id, common.ErrFeatureDisabled)
}

type SettingsSearchService struct {
	categories []category.Category
}

func NewSettingsSearchService() *SettingsSearchService {
	return &SettingsSearchService{categories: search.BuildCategories[Settings](searchtypes.SettingsProfile)}
}

// GetSettingsCategories returns the category index initialized by the service.
func (s *SettingsSearchService) GetSettingsCategories() []category.Category {
	return s.categories
}
