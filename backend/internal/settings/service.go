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
	"maps"
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
	// effectiveConfig is the env-override-applied snapshot, rebuilt whenever
	// config is stored. Overrides are resolved once at startup and never
	// change, so materializing them here turns every read (GetSettings and
	// the typed getters — ~100 call sites, some in per-project loops) into a
	// pointer load instead of a full struct Clone plus a reflection pass.
	// The snapshot is shared: readers must never mutate it.
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

type settingsUpdateResultInternal struct {
	settings []SettingVariable
	changes  []libarcane.SettingUpdate
}

type settingsEnvOverride struct {
	fieldIndex int
	key        string
	envVarName string
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
	svc.envOverrides = resolveSettingsEnvOverridesInternal()
	if len(svc.envOverrides) > 0 {
		slog.InfoContext(ctx, "Loaded Environment Settings Overrides", "count", len(svc.envOverrides))
	}

	err := svc.LoadDatabaseSettings(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to load settings: %w", err)
	}

	err = svc.setupInstanceID(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to setup instance ID: %w", err)
	}

	go svc.runEffectsInternal()
	return svc, nil
}

// SubscribeSettingsChanges invokes callback once when any subscribed key is
// present in a successful update. The callback receives only matching values.
// Callbacks run serially on the ordered effects worker, outside the write mutex.
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
			if err := s.enqueueEffectInternal(func() { callback(matching) }); err != nil && s.lifecycleCtx.Err() == nil {
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
	s.publishSettingsChangesInternal(updates)
	return nil
}

func (s *SettingsService) GetSettingsConfig() *Settings {
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

	effective := dst.Clone()
	s.applyEnvOverrides(ctx, effective)
	s.effectiveConfig.Store(effective)

	return nil
}

func (s *SettingsService) refreshSettingsCacheInternal(ctx context.Context) error {
	if err := s.LoadDatabaseSettings(ctx); err != nil {
		return fmt.Errorf("failed to refresh settings cache: %w", err)
	}

	return nil
}

func (s *SettingsService) getDefaultSettings() *Settings {
	return DefaultSettingsConfig()
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

func (s *SettingsService) loadDatabaseSettingsInternal(ctx context.Context, db *database.DB) (*Settings, error) {
	if config.Load().UIConfigurationDisabled || config.Load().AgentMode {
		slog.DebugContext(
			ctx,
			"loadDatabaseSettingsInternal: using env path",
			"uiConfigurationDisabled",
			config.Load().UIConfigurationDisabled,
			"agentMode",
			config.Load().AgentMode,
			"environment",
			config.Load().Environment,
		)
		return s.loadDatabaseConfigFromEnv(ctx, db)
	}

	dest := s.getDefaultSettings()

	var loaded []SettingVariable
	queryCtx, queryCancel := context.WithTimeout(ctx, 10*time.Second)
	defer queryCancel()
	err := db.WithContext(queryCtx).
		Find(&loaded).Error
	if err != nil {
		return nil, fmt.Errorf("failed to load configuration from the database: %w", err)
	}

	for _, v := range loaded {
		err = dest.UpdateField(v.Key, v.Value, false)

		if err != nil && !errors.Is(err, SettingKeyNotFoundError{}) {
			return nil, fmt.Errorf("failed to process settings for key '%s': %w", v.Key, err)
		}
	}

	// Apply environment variable overrides for fields tagged with "envOverride"
	s.applyEnvOverrides(ctx, dest)

	return dest, nil
}

func (s *SettingsService) loadDatabaseConfigFromEnv(ctx context.Context, db *database.DB) (*Settings, error) {
	dest := s.getDefaultSettings()

	// Fetch all settings once to avoid N+1 queries for internal keys
	var allSettings []SettingVariable
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

		envVarName := strings.ToUpper(kit.SnakeCase(key))

		// debug: log each env name checked and whether a value exists
		if val, ok, _ := utils.LookupEnvOrFile(envVarName); ok {
			mask := "<empty>"
			if val != "" {
				mask = fmt.Sprintf("%d chars", len(val))
			}
			slog.DebugContext(ctx, "loadDatabaseConfigFromEnv: env override found", "key", key, "env", envVarName, "valueMasked", mask)
			rv.Field(i).FieldByName("Value").SetString(kit.TrimQuotes(val))
			continue
		}
		if val, ok := settingsMap[key]; ok {
			// Fallback to database if environment variable is not set
			slog.DebugContext(ctx, "loadDatabaseConfigFromEnv: using database fallback", "key", key)
			rv.Field(i).FieldByName("Value").SetString(val)
			continue
		}
		slog.DebugContext(ctx, "loadDatabaseConfigFromEnv: env not set and no database value", "key", key, "env", envVarName)
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

func (s *SettingsService) applyEnvOverrides(ctx context.Context, dest *Settings) {
	_ = ctx
	rv := reflect.ValueOf(dest).Elem()

	for _, override := range s.envOverrides {
		rv.Field(override.fieldIndex).FieldByName("Value").SetString(override.value)
	}
}

func resolveSettingsEnvOverridesInternal() []settingsEnvOverride {
	rt := reflect.TypeFor[Settings]()
	overrides := make([]settingsEnvOverride, 0)

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

		envVarName := strings.ToUpper(kit.SnakeCase(key))
		if val, ok, _ := utils.LookupEnvOrFile(envVarName); ok && val != "" {
			overrides = append(overrides, settingsEnvOverride{
				fieldIndex: i,
				key:        key,
				envVarName: envVarName,
				value:      kit.TrimQuotes(val),
			})
		}
	}

	return overrides
}

// IsEnvOverrideActive reports whether an environment override currently controls the setting.
// and its corresponding environment variable is currently set to a non-empty value.
func (s *SettingsService) IsEnvOverrideActive(key string) bool {
	for _, override := range s.envOverrides {
		if override.key == key {
			return true
		}
	}

	return false
}

// GetSettings returns the effective (env-override-applied) settings snapshot.
// The snapshot is shared across callers and must be treated as read-only;
// mutation flows go through UpdateSettings, which works on an explicit clone.
func (s *SettingsService) GetSettings(ctx context.Context) (*Settings, error) {
	settingsCfg := s.getEffectiveSettingsConfigInternal(ctx)
	return settingsCfg, nil
}

// GetSettingsOrDefaults is a convenience for hot paths that need a snapshot but cannot
// meaningfully recover from a settings load failure. It logs any error and guarantees a
// non-nil *Settings (defaults: a zero-valued struct, which the SettingVariable helpers
// like kit.ParseOrDefault treat as "use the caller's default").
func (s *SettingsService) GetSettingsOrDefaults(ctx context.Context) *Settings {
	cfg, err := s.GetSettings(ctx)
	if err != nil {
		slog.WarnContext(ctx, "failed to load settings, falling back to defaults", "error", err)
	}
	return kit.Ternary(cfg == nil, &Settings{}, cfg)
}

func (s *SettingsService) getEffectiveSettingsConfigInternal(ctx context.Context) *Settings {
	if effective := s.effectiveConfig.Load(); effective != nil {
		return effective
	}

	// Only reachable before LoadDatabaseSettings has completed in the
	// constructor; fall back to building the snapshot per call.
	settingsCfg := s.GetSettingsConfig().Clone()
	s.applyEnvOverrides(ctx, settingsCfg)
	return settingsCfg
}

func validateSettingValueInternal(key, value string) error {
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
	if err := validateSettingValueInternal(key, value); err != nil {
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

	if err := s.updateSettingValueNoRefreshInternal(ctx, key, value); err != nil {
		return err
	}
	return s.refreshSettingsCacheInternal(context.WithoutCancel(ctx))
}

// UpdateSettingValues persists a group of internal setting values atomically
// and publishes the refreshed snapshot before returning.
func (s *SettingsService) UpdateSettingValues(ctx context.Context, updates []libarcane.SettingUpdate) error {
	values := make([]SettingVariable, 0, len(updates))
	for _, update := range updates {
		if err := validateSettingValueInternal(update.Key, update.Value); err != nil {
			return err
		}
		values = append(values, SettingVariable{Key: update.Key, Value: update.Value})
	}
	s.writes.Lock()
	defer s.writes.Unlock()
	if err := ctx.Err(); err != nil {
		return err
	}

	if err := s.persistSettings(ctx, values); err != nil {
		return err
	}
	return s.refreshSettingsCacheInternal(context.WithoutCancel(ctx))
}

func (s *SettingsService) updateSettingValueNoRefreshInternal(ctx context.Context, key, value string) error {
	if key == "oidcProviderName" {
		value = normalization.Text(value, true, true)
	}
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		settingVar := &SettingVariable{
			Key:   key,
			Value: value,
		}
		return tx.Save(settingVar).Error
	})
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
	result, err := s.updateSettingsInternal(ctx, updates)
	if err == nil {
		s.publishSettingsChangesInternal(result.changes)
	}
	return result.settings, err
}

func (s *SettingsService) publishSettingsChangesInternal(updates []libarcane.SettingUpdate) {
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

func (s *SettingsService) updateSettingsInternal(ctx context.Context, updates settings.Update) (settingsUpdateResultInternal, error) {
	defaultCfg := s.getDefaultSettings()
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
		return settingsUpdateResultInternal{}, err
	}

	effectiveValue := func(current SettingVariable, next *string) string {
		if next != nil {
			return *next
		}
		return current.Value
	}
	oidcClientSecret := cfg.OidcClientSecret.Value
	if oidcClientSecretUpdated {
		oidcClientSecret = *updates.OidcClientSecret
	}
	if effectiveValue(cfg.OidcEnabled, updates.OidcEnabled) == "true" {
		required := []struct{ key, value string }{
			{"oidcClientId", effectiveValue(cfg.OidcClientId, updates.OidcClientId)},
			{"oidcClientSecret", oidcClientSecret},
			{"oidcIssuerUrl", effectiveValue(cfg.OidcIssuerUrl, updates.OidcIssuerUrl)},
		}
		for _, field := range required {
			if strings.TrimSpace(field.value) == "" {
				return settingsUpdateResultInternal{}, common.Classify(
					common.ErrValidation,
					&base.FieldError{
						Field: field.key,
						Err: fmt.Errorf(
							"enabling OIDC requires %s",
							field.key,
						),
					},
				)
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
		return settingsUpdateResultInternal{}, err
	}

	valuesToUpdate, err := s.prepareUpdateValues(updates, cfg, defaultCfg)
	if err != nil {
		return settingsUpdateResultInternal{}, err
	}
	if oidcClientSecretUpdated {
		valuesToUpdate = append(valuesToUpdate, SettingVariable{Key: "oidcClientSecret", Value: *updates.OidcClientSecret})
	}
	if trivyServerTokenUpdated {
		valuesToUpdate = append(valuesToUpdate, SettingVariable{Key: "trivyServerToken", Value: *updates.TrivyServerToken})
	}

	if persistSettingsErr := s.persistSettings(ctx, valuesToUpdate); persistSettingsErr != nil {
		return settingsUpdateResultInternal{}, persistSettingsErr
	}

	if refreshSettingsCacheErr := s.refreshSettingsCacheInternal(context.WithoutCancel(ctx)); refreshSettingsCacheErr != nil {
		return settingsUpdateResultInternal{}, refreshSettingsCacheErr
	}
	settingsCfg := s.GetSettingsConfig()
	changes := make([]libarcane.SettingUpdate, 0, len(valuesToUpdate))
	for _, value := range valuesToUpdate {
		changes = append(changes, libarcane.SettingUpdate{Key: value.Key, Value: value.Value})
	}
	return settingsUpdateResultInternal{
		settings: settingsCfg.ToSettingVariableSlice(SettingVisibilityNonAdmin, false),
		changes:  changes,
	}, nil
}

func (s *SettingsService) prepareUpdateValues(updates settings.Update, cfg, defaultCfg *Settings) ([]SettingVariable, error) {
	rt := reflect.TypeFor[settings.Update]()
	rv := reflect.ValueOf(updates)
	valuesToUpdate := make([]SettingVariable, 0)

	for i := range rt.NumField() {
		field := rt.Field(i)
		fieldValue := rv.Field(i)

		key, value, ok := extractUpdateValue(field, fieldValue)
		if !ok {
			continue
		}

		if err := validateSettingValueInternal(key, value); err != nil {
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

func extractUpdateValue(field reflect.StructField, fieldValue reflect.Value) (string, string, bool) {
	if fieldValue.Kind() == reflect.Pointer && fieldValue.IsNil() {
		return "", "", false
	}

	key, _, _ := strings.Cut(field.Tag.Get("json"), ",")
	if key == "" {
		return "", "", false
	}

	var value string
	if fieldValue.Kind() == reflect.Pointer {
		value = fieldValue.Elem().String()
	}

	return key, value, true
}

func (s *SettingsService) persistSettings(ctx context.Context, values []SettingVariable) error {
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		for _, setting := range values {
			if setting.Key == "oidcProviderName" {
				setting.Value = normalization.Text(setting.Value, true, true)
			}
			if err := tx.Save(&setting).Error; err != nil {
				return fmt.Errorf("failed to update setting %s: %w", setting.Key, err)
			}
		}
		return nil
	})
}

// retiredSettingValuesInternal lists former defaults that startup replaces
// with the current default when an install still holds them verbatim.
var retiredSettingValuesInternal = map[string][]string{
	"dockerClientRefreshInterval": {"*/30 * * * * *"},
	"autoHealInterval":            {"*/30 * * * * *"},
}

func (s *SettingsService) EnsureDefaultSettings(ctx context.Context) error {
	s.writes.Lock()
	defer s.writes.Unlock()
	if err := ctx.Err(); err != nil {
		return err
	}

	defaultSettings := s.getDefaultSettings()
	defaultSettingVars := defaultSettings.ToSettingVariableSlice(SettingVisibilityAll, false)

	if err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
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
			case slices.Contains(retiredSettingValuesInternal[defaultSetting.Key], existing.Value):
				if replaceDefaultSettingErr := tx.Model(&SettingVariable{}).Where("key = ?", defaultSetting.Key).Update("value", defaultSetting.Value).Error; replaceDefaultSettingErr != nil {
					return fmt.Errorf("failed to replace retired default for setting %s: %w", defaultSetting.Key, replaceDefaultSettingErr)
				}
			}
		}
		return nil
	}); err != nil {
		return err
	}
	return nil
}

func (s *SettingsService) PruneUnknownSettings(ctx context.Context) error {
	s.writes.Lock()
	defer s.writes.Unlock()
	if err := ctx.Err(); err != nil {
		return err
	}

	allowedKeys := allowedSettingKeys()
	if len(allowedKeys) == 0 {
		return nil
	}

	keys := slices.Collect(maps.Keys(allowedKeys))

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

	rt := reflect.TypeFor[Settings]()
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
	return s.LoadDatabaseSettings(context.WithoutCancel(ctx))
}

func allowedSettingKeys() map[string]struct{} {
	allowed := make(map[string]struct{})

	settingsType := reflect.TypeFor[Settings]()
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

	envVarName := strings.ToUpper(kit.SnakeCase(key))
	envVal, ok, _ := utils.LookupEnvOrFile(envVarName)
	if !ok {
		return nil
	}
	envVal = kit.TrimQuotes(envVal)

	return s.upsertEnvSetting(ctx, tx, key, envVal)
}

func (s *SettingsService) shouldProcessField(key, attrs string, isEnvOnlyMode bool) bool {
	if key == "" || strings.Contains(attrs, "internal") {
		return false
	}

	// If not in env-only mode, only persist if it's explicitly marked as envOverride
	return isEnvOnlyMode || strings.Contains(attrs, "envOverride")
}

func (s *SettingsService) upsertEnvSetting(ctx context.Context, tx *gorm.DB, key, envVal string) error {
	var existing SettingVariable
	err := tx.Where("key = ?", key).First(&existing).Error

	switch {
	case errors.Is(err, gorm.ErrRecordNotFound):
		newVar := SettingVariable{Key: key, Value: envVal}
		if createEnvSettingErr := tx.Create(&newVar).Error; createEnvSettingErr != nil {
			return fmt.Errorf("persist env setting %s: %w", key, createEnvSettingErr)
		}
		slog.DebugContext(ctx, "Created setting from environment", "key", key)
	case err != nil:
		return fmt.Errorf("check setting %s: %w", key, err)
	default:
		if existing.Value != envVal {
			if updateEnvSettingErr := tx.Model(&existing).Update("value", envVal).Error; updateEnvSettingErr != nil {
				return fmt.Errorf("update env setting %s: %w", key, updateEnvSettingErr)
			}
			slog.DebugContext(ctx, "Updated setting from environment", "key", key)
		}
	}

	return nil
}

func (s *SettingsService) ListSettings(visibility SettingVisibility) []SettingVariable {
	return s.GetSettingsConfig().ToSettingVariableSlice(visibility, true)
}

// GetSettingType returns the type from the setting metadata
func (s *SettingsService) GetSettingType(key string) string {
	rt := reflect.TypeFor[Settings]()
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

	err := s.UpdateSetting(ctx, "instanceId", uuid.New().String())
	if err != nil {
		return fmt.Errorf("failed to set instance ID in database: %w", err)
	}

	return nil
}

func (s *SettingsService) settingValue[T any](ctx context.Context, key string, parse func(string) (T, error)) mo.Option[T] {
	cfg := s.getEffectiveSettingsConfigInternal(ctx)
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

// SetContainerAutoUpdateExclusionInternal adds or removes a container name from
// the autoUpdateExcludedContainers setting. When excluded is true the container
// is added to the list; when false it is removed.
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
		filtered := ordered[:0]
		for _, name := range ordered {
			if name != containerName {
				filtered = append(filtered, name)
			}
		}
		ordered = filtered
	}

	if err := s.updateSettingValueNoRefreshInternal(ctx, "autoUpdateExcludedContainers", strings.Join(ordered, ",")); err != nil {
		return err
	}
	return s.refreshSettingsCacheInternal(context.WithoutCancel(ctx))
}

func (s *SettingsService) EnsureEncryptionKey(ctx context.Context) (string, error) {
	s.writes.Lock()
	defer s.writes.Unlock()
	if err := ctx.Err(); err != nil {
		return "", err
	}

	const keyName = "encryptionKey"
	var key string

	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var sv SettingVariable
		err := tx.Where("key = ?", keyName).First(&sv).Error

		if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
			return fmt.Errorf("failed to load encryption key: %w", err)
		}

		if sv.Value != "" {
			key = sv.Value
			return nil
		}

		notFound := errors.Is(err, gorm.ErrRecordNotFound)
		sum := sha256.Sum256([]byte(uuid.New().String()))
		generatedKey := base64.StdEncoding.EncodeToString(sum[:])
		key = generatedKey

		if notFound {
			if createErr := tx.Create(&SettingVariable{Key: keyName, Value: generatedKey}).Error; createErr != nil {
				return fmt.Errorf("failed to persist encryption key: %w", createErr)
			}
			return nil
		}

		if updErr := tx.Model(&SettingVariable{}).
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

func (s *SettingsService) EnsureJwtSigningKey(ctx context.Context) (*mldsa.PrivateKey, error) {
	seed, err := s.ensureEncryptedKeyInternal(ctx, "jwtSigningKeySeed", mldsa.PrivateKeySize)
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
	return s.ensureEncryptedKeyInternal(ctx, "browserSessionSigningKey", browserSessionSigningKeySize)
}

func (s *SettingsService) ensureEncryptedKeyInternal(ctx context.Context, keyName string, size int) ([]byte, error) {
	s.writes.Lock()
	defer s.writes.Unlock()
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	var key []byte

	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var sv SettingVariable
		err := tx.Where("key = ?", keyName).First(&sv).Error
		if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
			return fmt.Errorf("failed to load signing key: %w", err)
		}

		if sv.Value != "" {
			decoded, decErr := crypto.Decrypt(sv.Value)
			if decErr != nil {
				return fmt.Errorf("failed to decrypt signing key: %w", decErr)
			}
			key, decErr = base64.StdEncoding.DecodeString(decoded)
			if decErr != nil {
				return fmt.Errorf("failed to decode signing key: %w", decErr)
			}
			if len(key) != size {
				return fmt.Errorf("invalid signing key length: got %d, want %d", len(key), size)
			}
			return nil
		}

		key = make([]byte, size)
		if _, genErr := rand.Read(key); genErr != nil {
			return fmt.Errorf("failed to generate signing key: %w", genErr)
		}
		encrypted, encErr := crypto.Encrypt(base64.StdEncoding.EncodeToString(key))
		if encErr != nil {
			return fmt.Errorf("failed to encrypt signing key: %w", encErr)
		}

		if errors.Is(err, gorm.ErrRecordNotFound) {
			if createSigningKeyErr := tx.Create(&SettingVariable{Key: keyName, Value: encrypted}).Error; createSigningKeyErr != nil {
				return fmt.Errorf("failed to persist signing key: %w", createSigningKeyErr)
			}
			return nil
		}
		if updateSigningKeyErr := tx.Model(&SettingVariable{}).
			Where("key = ?", keyName).
			Update("value", encrypted).Error; updateSigningKeyErr != nil {
			return fmt.Errorf("failed to update signing key: %w", updateSigningKeyErr)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}

	return key, nil
}

func (s *SettingsService) NormalizeProjectsDirectory(ctx context.Context, projectsDirEnv string) error {
	if projectsDirEnv != "" {
		slog.DebugContext(ctx, "PROJECTS_DIRECTORY environment variable is set, skipping normalization", "value", projectsDirEnv)
		return nil
	}

	s.writes.Lock()
	defer s.writes.Unlock()
	if err := ctx.Err(); err != nil {
		return err
	}

	var projectsDirSetting SettingVariable
	err := s.db.WithContext(ctx).Where("key = ?", "projectsDirectory").First(&projectsDirSetting).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		slog.DebugContext(ctx, "No projectsDirectory setting found, skipping normalization")
		return nil
	}
	if err != nil {
		return fmt.Errorf("failed to load projectsDirectory setting: %w", err)
	}

	value := strings.TrimSpace(projectsDirSetting.Value)
	isMapping := strings.Contains(value, ":") && (strings.HasPrefix(value, "/") || projects.IsWindowsDrivePath(value))
	if filepath.IsAbs(value) || isMapping {
		slog.DebugContext(ctx, "Projects directory already normalized or custom, skipping", "value", projectsDirSetting.Value)
		return nil
	}

	cwd, _ := os.Getwd()
	absPath, absErr := filepath.Abs(value)
	if absErr != nil {
		return fmt.Errorf("failed to resolve relative path to absolute: %w", absErr)
	}
	slog.InfoContext(ctx, "Normalizing projects directory from relative to absolute path", "from", value, "to", absPath, "base", cwd)
	if updateSettingValueNoRefreshErr := s.updateSettingValueNoRefreshInternal(ctx, "projectsDirectory", absPath); updateSettingValueNoRefreshErr != nil {
		return fmt.Errorf("failed to update projectsDirectory: %w", updateSettingValueNoRefreshErr)
	}
	if refreshSettingsCacheErr := s.refreshSettingsCacheInternal(context.WithoutCancel(ctx)); refreshSettingsCacheErr != nil {
		return refreshSettingsCacheErr
	}
	slog.InfoContext(ctx, "Successfully normalized projects directory")
	s.publishSettingsChangesInternal([]libarcane.SettingUpdate{{Key: "projectsDirectory", Value: absPath}})
	return nil
}

func (s *SettingsService) NormalizeBuildsDirectory(ctx context.Context) error {
	const buildsKey = "buildsDirectory"
	envVarName := strings.ToUpper(kit.SnakeCase(buildsKey))
	if envVal, ok, _ := utils.LookupEnvOrFile(envVarName); ok && strings.TrimSpace(envVal) != "" {
		slog.DebugContext(ctx, "BUILDS_DIRECTORY environment variable is set, skipping normalization", "value", envVal)
		return nil
	}

	s.writes.Lock()
	defer s.writes.Unlock()
	if err := ctx.Err(); err != nil {
		return err
	}

	var buildsDirSetting SettingVariable
	err := s.db.WithContext(ctx).Where("key = ?", buildsKey).First(&buildsDirSetting).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		slog.DebugContext(ctx, "No buildsDirectory setting found, skipping normalization")
		return nil
	}
	if err != nil {
		return fmt.Errorf("failed to load buildsDirectory setting: %w", err)
	}

	value := strings.TrimSpace(buildsDirSetting.Value)
	if value == "" || filepath.IsAbs(value) {
		slog.DebugContext(ctx, "Builds directory already normalized or empty, skipping", "value", buildsDirSetting.Value)
		return nil
	}

	cwd, _ := os.Getwd()
	absPath, absErr := filepath.Abs(value)
	if absErr != nil {
		return fmt.Errorf("failed to resolve relative path to absolute: %w", absErr)
	}
	slog.InfoContext(ctx, "Normalizing builds directory from relative to absolute path", "from", value, "to", absPath, "base", cwd)
	if updateSettingValueNoRefreshErr := s.updateSettingValueNoRefreshInternal(ctx, buildsKey, absPath); updateSettingValueNoRefreshErr != nil {
		return fmt.Errorf("failed to update buildsDirectory: %w", updateSettingValueNoRefreshErr)
	}
	if refreshSettingsCacheErr := s.refreshSettingsCacheInternal(context.WithoutCancel(ctx)); refreshSettingsCacheErr != nil {
		return refreshSettingsCacheErr
	}
	slog.InfoContext(ctx, "Successfully normalized builds directory")
	s.publishSettingsChangesInternal([]libarcane.SettingUpdate{{Key: buildsKey, Value: absPath}})
	return nil
}

func (s *SettingsService) enqueueEffectInternal(effect func()) error {
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

func (s *SettingsService) runEffectsInternal() {
	defer close(s.effectsDone)
	for {
		s.effectsMu.Lock()
		if len(s.effects) > 0 {
			effect := s.effects[0]
			s.effects[0] = nil
			s.effects = s.effects[1:]
			s.effectsMu.Unlock()
			func() { var err error; defer utils.RecoverToError(&err, "settings effect"); effect() }()
			continue
		}
		closed := s.effectsClosed
		s.effectsMu.Unlock()
		if closed {
			return
		}
		select {
		case <-s.effectsWake:
		case <-s.lifecycleCtx.Done():
			s.effectsMu.Lock()
			s.effectsClosed = true
			s.effectsMu.Unlock()
		}
	}
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
