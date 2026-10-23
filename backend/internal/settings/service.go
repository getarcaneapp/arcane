package settings

import (
	"cmp"
	"context"
	"crypto/mldsa"
	"crypto/rand"
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

	"github.com/getarcaneapp/arcane/types/v3"
	"go.getarcane.app/kit/normalization"
	"go.getarcane.app/kit/pkg"
	"go.getarcane.app/sys/crypto"
	"go.yaml.in/yaml/v4"
	"gorm.io/gorm"

	"github.com/getarcaneapp/arcane/backend/v3/internal/common"
	"github.com/getarcaneapp/arcane/backend/v3/internal/config"
	"github.com/getarcaneapp/arcane/backend/v3/internal/database"
	"github.com/getarcaneapp/arcane/backend/v3/internal/search"
	"github.com/getarcaneapp/arcane/backend/v3/pkg/libarcane"
	"github.com/getarcaneapp/arcane/backend/v3/pkg/projects"
	"github.com/getarcaneapp/arcane/backend/v3/pkg/utils"
	"github.com/getarcaneapp/arcane/backend/v3/pkg/utils/concurrency"
	"github.com/getarcaneapp/arcane/backend/v3/pkg/utils/httpx"
	"github.com/getarcaneapp/arcane/backend/v3/pkg/utils/validation"
)

const (
	browserSessionSigningKeySize  = 64
	minCiphertextBytes            = 28
	sensitiveSettingsEncryptedKey = "sensitiveSettingsEncrypted"
)

type Source string

const (
	SourceDefault Source = "default"
	SourceDB      Source = "db"
	SourceEnv     Source = "env"
)

type LockReason string

const (
	LockReasonEnv      LockReason = "env"
	LockReasonReadOnly LockReason = "readOnly"
)

type snapshot struct {
	values  *Settings
	sources []Source
}

type SettingsService struct {
	db            *database.DB
	appConfig     *config.Config
	env           Env
	snapshot      atomic.Pointer[snapshot]
	writes        sync.Mutex
	effectsMu     sync.Mutex
	effects       []func()
	effectsWake   chan struct{}
	effectsDone   chan struct{}
	effectsClosed bool
	lifecycleCtx  context.Context
	changes       *concurrency.Signal[[]libarcane.SettingUpdate]
}

func NewSettingsService(ctx context.Context, db *database.DB, appConfig *config.Config, env Env) (*SettingsService, error) {
	if ctx == nil {
		return nil, errors.New("settings lifecycle context unavailable")
	}
	svc := &SettingsService{
		db:           db,
		appConfig:    appConfig,
		env:          env,
		effectsWake:  make(chan struct{}, 1),
		effectsDone:  make(chan struct{}),
		lifecycleCtx: ctx,
		changes:      concurrency.NewSignal[[]libarcane.SettingUpdate](),
	}
	if len(env) > 0 {
		slog.InfoContext(ctx, "Loaded environment settings", "count", len(env))
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
	current := s.snapshot.Load()
	if current == nil {
		panic("GetSettingsConfig called before Settings has been loaded")
	}

	return current.values
}

func (s *SettingsService) LoadDatabaseSettings(ctx context.Context) error {
	var stored []SettingVariable
	queryCtx, queryCancel := context.WithTimeout(ctx, 10*time.Second)
	defer queryCancel()
	if err := s.db.WithContext(queryCtx).Find(&stored).Error; err != nil {
		return fmt.Errorf("failed to load configuration from the database: %w", err)
	}

	fields, byKey := settingsFields()
	storedValues := make(map[string]string, len(stored))
	for _, setting := range stored {
		storedValues[setting.Key] = setting.Value
		if !byKey[setting.Key].isEncrypted || setting.Value == "" {
			continue
		}
		plain, decryptErr := crypto.Decrypt(setting.Value)
		switch {
		case decryptErr == nil:
			storedValues[setting.Key] = plain
		case looksLikeCiphertext(setting.Value, decryptErr):
			slog.WarnContext(ctx, "Failed to decrypt stored sensitive setting with the current encryption key", "key", setting.Key)
		}
	}

	values := DefaultSettingsConfig()
	sources := make([]Source, len(fields))
	rv := reflect.ValueOf(values).Elem()
	for _, field := range fields {
		storedValue, inDB := storedValues[field.key]
		envValue, inEnv := s.env[field.key]
		dbErr := validateValue(field, storedValue)
		if inDB && dbErr != nil {
			slog.WarnContext(ctx, "Ignoring invalid stored setting value; using default", "key", field.key, "error", dbErr)
		}
		switch {
		case inEnv && !field.isInternal:
			sources[field.position] = SourceEnv
			rv.Field(field.index).FieldByName("Value").SetString(envValue)
		case inDB && dbErr == nil:
			sources[field.position] = SourceDB
			rv.Field(field.index).FieldByName("Value").SetString(storedValue)
		default:
			sources[field.position] = SourceDefault
		}
	}

	s.snapshot.Store(&snapshot{
		values:  values,
		sources: sources,
	})
	return nil
}

// DefaultSettingsConfig returns the canonical default settings model used by Arcane.
func DefaultSettingsConfig() *Settings {
	cfg := &Settings{}
	rv := reflect.ValueOf(cfg).Elem()
	fields, _ := settingsFields()
	for _, field := range fields {
		rv.Field(field.index).FieldByName("Value").SetString(field.defaultValue)
	}
	return cfg
}

func (s *SettingsService) IsLocked(key string) (bool, LockReason) {
	_, byKey := settingsFields()
	field, ok := byKey[key]
	switch {
	case !ok || field.isInternal:
		return false, ""
	case s.snapshot.Load().sources[field.position] == SourceEnv:
		return true, LockReasonEnv
	case s.appConfig.UIConfigurationDisabled:
		return true, LockReasonReadOnly
	default:
		return false, ""
	}
}

func (s *SettingsService) GetSettings(_ context.Context) (*Settings, error) {
	return s.GetSettingsConfig(), nil
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

func (s *SettingsService) UpdateSetting(ctx context.Context, key, value string) error {
	_, byKey := settingsFields()
	if err := validateValue(byKey[key], value); err != nil {
		return err
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
	s.writes.Lock()
	defer s.writes.Unlock()
	if err := ctx.Err(); err != nil {
		return err
	}

	values, err := s.prepareUpdateValues(updates)
	if err != nil {
		return err
	}
	return s.persistSettings(ctx, values)
}

// UpdateSettings publishes the refreshed snapshot before returning. Each
// subscriber's effects remain ordered and may finish after this method returns.
func (s *SettingsService) UpdateSettings(ctx context.Context, updates types.SettingsUpdate) error {
	s.writes.Lock()
	defer s.writes.Unlock()
	if err := ctx.Err(); err != nil {
		return err
	}

	_, byKey := settingsFields()
	fieldErr := func(key string, err error) error {
		return common.Classify(common.ErrValidation, &types.FieldError{Field: key, Err: err})
	}
	pending := make(map[string]string, len(updates))
	for _, key := range slices.Sorted(maps.Keys(updates)) {
		field, ok := byKey[key]
		switch {
		case !ok || field.isInternal:
			return fieldErr(key, fmt.Errorf("unknown setting %s", key))
		case !field.isSensitive || updates[key] != redactionMask:
			pending[key] = updates[key]
		}
	}
	submittedValue := func(key string) *string {
		value, ok := pending[key]
		return kit.Ternary(ok, &value, nil)
	}

	cfg := s.GetSettingsConfig()
	effectiveValue := func(key string) string {
		if value, ok := pending[key]; ok {
			return value
		}
		current, _, _, _ := cfg.FieldByKey(key)
		return current
	}
	projectsDir := kit.Ternary(pending["projectsDirectory"] != cfg.ProjectsDirectory.Value, pending["projectsDirectory"], "")
	projectsContainerDir, _, projectsDirIsMapping := strings.Cut(projectsDir, ":")
	swarmDir := kit.Ternary(pending["swarmStackSourcesDirectory"] != cfg.SwarmStackSourcesDirectory.Value, pending["swarmStackSourcesDirectory"], "")
	var trivyConfig map[string]any
	trivyConfigErr := yaml.Unmarshal([]byte(pending["trivyConfig"]), &trivyConfig)

	switch {
	case projectsDir != "" && !projects.IsWindowsDrivePath(projectsDir) && !strings.HasPrefix(projectsContainerDir, "/"):
		return fieldErr("projectsDirectory", errors.New(kit.Ternary(projectsDirIsMapping,
			"projectsDirectory mapping format: container path must be absolute",
			"projectsDirectory must be an absolute path starting with '/'")))
	case swarmDir != "" && !projects.IsWindowsDrivePath(swarmDir) && !strings.HasPrefix(swarmDir, "/"):
		return fieldErr("swarmStackSourcesDirectory", errors.New("swarmStackSourcesDirectory must be an absolute path"))
	case strings.TrimSpace(pending["trivyConfig"]) != "" && trivyConfigErr != nil:
		return fieldErr("trivyConfig", fmt.Errorf("trivyConfig must be a YAML mapping: %w", trivyConfigErr))
	case strings.TrimSpace(pending["trivyConfig"]) != "" && trivyConfig == nil:
		return fieldErr("trivyConfig", errors.New("trivyConfig must be a YAML mapping"))
	}

	if projectsDir != "" {
		resolved, err := projects.GetProjectsDirectory(ctx, strings.TrimSpace(projectsDir))
		if err != nil {
			return fieldErr("projectsDirectory", fmt.Errorf("cannot use projects directory %q: %w", projectsDir, err))
		}
		f, err := os.Open(resolved)
		if err == nil {
			err = f.Close()
		}
		if err != nil {
			return fieldErr("projectsDirectory", fmt.Errorf("cannot read projects directory %q: %w", resolved, err))
		}
	}

	normalizeTargetURL := func(value string) string {
		normalized, err := httpx.NormalizeBaseURL(value)
		if err != nil {
			return strings.TrimSpace(value)
		}
		return normalized
	}

	for _, target := range []struct {
		key  string
		name string
	}{
		{key: "oidcIssuerUrl", name: "OIDC issuer URL"},
		{key: "oidcAuthorizationEndpoint", name: "OIDC authorization endpoint"},
		{key: "oidcTokenEndpoint", name: "OIDC token endpoint"},
		{key: "oidcUserinfoEndpoint", name: "OIDC userinfo endpoint"},
		{key: "oidcJwksEndpoint", name: "OIDC JWKS endpoint"},
		{key: "oidcDeviceAuthorizationEndpoint", name: "OIDC device authorization endpoint"},
	} {
		current, _, _, _ := cfg.FieldByKey(target.key)
		if err := validation.ValidateCredentialTargetChange(
			target.name,
			current,
			submittedValue(target.key),
			normalizeTargetURL,
			map[string]bool{"oidcClientSecret": cfg.OidcClientSecret.Value != ""},
			map[string]bool{"oidcClientSecret": submittedValue("oidcClientSecret") != nil},
		); err != nil {
			return err
		}
	}

	if effectiveValue("oidcEnabled") == "true" {
		for _, key := range []string{"oidcClientId", "oidcClientSecret", "oidcIssuerUrl"} {
			if strings.TrimSpace(effectiveValue(key)) == "" {
				return fieldErr(key, fmt.Errorf("enabling OIDC requires %s", key))
			}
		}
	}

	if err := validation.ValidateCredentialTargetChange(
		"Trivy server URL",
		cfg.TrivyServerUrl.Value,
		submittedValue("trivyServerUrl"),
		normalizeTargetURL,
		map[string]bool{"trivyServerToken": cfg.TrivyServerToken.Value != ""},
		map[string]bool{"trivyServerToken": pending["trivyServerToken"] != ""},
	); err != nil {
		return err
	}

	submitted := make([]libarcane.SettingUpdate, 0, len(pending))
	for _, key := range slices.Sorted(maps.Keys(pending)) {
		submitted = append(submitted, libarcane.SettingUpdate{Key: key, Value: pending[key]})
	}
	valuesToUpdate, err := s.prepareUpdateValues(submitted)
	if err != nil {
		return err
	}

	if persistSettingsErr := s.persistSettings(ctx, valuesToUpdate); persistSettingsErr != nil {
		return persistSettingsErr
	}

	changes := make([]libarcane.SettingUpdate, 0, len(valuesToUpdate))
	for _, value := range valuesToUpdate {
		changes = append(changes, libarcane.SettingUpdate{Key: value.Key, Value: value.Value})
	}
	s.publishSettingsChanges(changes)
	return nil
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

func (s *SettingsService) prepareUpdateValues(updates []libarcane.SettingUpdate) ([]SettingVariable, error) {
	cfg := s.GetSettingsConfig()
	_, byKey := settingsFields()
	valuesToUpdate := make([]SettingVariable, 0, len(updates))
	for _, update := range updates {
		field, ok := byKey[update.Key]
		if !ok {
			return nil, SettingKeyNotFoundError{field: update.Key}
		}
		if err := validateValue(field, update.Value); err != nil {
			return nil, err
		}
		if slices.Contains([]string{libarcane.DepotTokenSettingKey, "trivyServerToken"}, update.Key) && strings.TrimSpace(update.Value) == "" {
			continue
		}
		value := cmp.Or(update.Value, field.defaultValue)
		current, _, _, _ := cfg.FieldByKey(update.Key)
		if value == current {
			continue
		}
		locked, reason := s.IsLocked(update.Key)
		switch {
		case locked && reason == LockReasonEnv:
			return nil, common.Classify(common.ErrValidation, &types.FieldError{
				Field: update.Key,
				Err:   fmt.Errorf("%s is set by environment variable %s and cannot be changed", update.Key, field.envName),
			})
		case locked:
			return nil, common.Classify(common.ErrValidation, &types.FieldError{
				Field: update.Key,
				Err:   fmt.Errorf("%s cannot be changed because UI configuration is disabled", update.Key),
			})
		}
		valuesToUpdate = append(valuesToUpdate, SettingVariable{Key: update.Key, Value: value})
	}

	return valuesToUpdate, nil
}

func (s *SettingsService) persistSettings(ctx context.Context, values []SettingVariable) error {
	_, byKey := settingsFields()
	if err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		for _, setting := range values {
			if setting.Key == "oidcProviderName" {
				setting.Value = normalization.Text(setting.Value, true, true)
			}
			if byKey[setting.Key].isEncrypted && setting.Value != "" {
				encrypted, encryptErr := crypto.Encrypt(setting.Value)
				if encryptErr != nil {
					return fmt.Errorf("failed to encrypt setting %s: %w", setting.Key, encryptErr)
				}
				setting.Value = encrypted
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

func (s *SettingsService) PruneUnknownSettings(ctx context.Context) error {
	s.writes.Lock()
	defer s.writes.Unlock()
	if err := ctx.Err(); err != nil {
		return err
	}

	fields, _ := settingsFields()
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

func (s *SettingsService) EncryptStoredSensitiveSettings(ctx context.Context) error {
	s.writes.Lock()
	defer s.writes.Unlock()
	if err := ctx.Err(); err != nil {
		return err
	}

	fields, _ := settingsFields()
	keys := make([]string, 0, len(fields))
	for _, field := range fields {
		if field.isEncrypted {
			keys = append(keys, field.key)
		}
	}

	encryptedCount := 0
	completed := false
	if err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var marker SettingVariable
		if findErr := tx.Where("key = ?", sensitiveSettingsEncryptedKey).Limit(1).Find(&marker).Error; findErr != nil {
			return fmt.Errorf("load sensitive settings marker: %w", findErr)
		}
		if marker.Value == "true" {
			completed = true
			return nil
		}
		var stored []SettingVariable
		if findErr := tx.Where("key IN ? AND value <> ''", keys).Find(&stored).Error; findErr != nil {
			return fmt.Errorf("load sensitive settings: %w", findErr)
		}
		for _, setting := range stored {
			if _, decryptErr := crypto.Decrypt(setting.Value); decryptErr == nil {
				continue
			}
			encrypted, encryptErr := crypto.Encrypt(setting.Value)
			if encryptErr != nil {
				return fmt.Errorf("encrypt setting %s: %w", setting.Key, encryptErr)
			}
			if updateErr := tx.Model(&SettingVariable{}).Where("key = ?", setting.Key).Update("value", encrypted).Error; updateErr != nil {
				return fmt.Errorf("update setting %s: %w", setting.Key, updateErr)
			}
			encryptedCount++
		}
		if saveErr := tx.Save(&SettingVariable{Key: sensitiveSettingsEncryptedKey, Value: "true"}).Error; saveErr != nil {
			return fmt.Errorf("save sensitive settings marker: %w", saveErr)
		}
		return nil
	}); err != nil {
		return err
	}
	if completed {
		return nil
	}
	slog.InfoContext(ctx, "Encrypted stored sensitive settings", "count", encryptedCount)
	return s.LoadDatabaseSettings(context.WithoutCancel(ctx))
}

func looksLikeCiphertext(value string, decryptErr error) bool {
	_, isDecryptErr := errors.AsType[*crypto.DecryptError](decryptErr)
	decoded, decodeErr := base64.StdEncoding.DecodeString(value)
	return isDecryptErr && decodeErr == nil && len(decoded) >= minCiphertextBytes
}

func (s *SettingsService) SettingsSnapshot(visibility SettingVisibility) types.SettingsSnapshot {
	current := s.snapshot.Load()
	values := reflect.ValueOf(current.values).Elem()
	fields, _ := settingsFields()
	states := make([]types.SettingState, 0, len(fields))
	for _, field := range fields {
		if field.isInternal || !fieldVisibleForSettingVisibility(field, visibility) {
			continue
		}
		value := values.Field(field.index).FieldByName("Value").String()
		locked, reason := s.IsLocked(field.key)
		states = append(states, types.SettingState{
			Key:        field.key,
			Value:      kit.Ternary(field.isSensitive && value != "", redactionMask, value),
			Type:       field.valueType,
			Default:    field.defaultValue,
			Source:     string(current.sources[field.position]),
			Locked:     locked,
			LockReason: string(reason),
			Sensitive:  field.isSensitive,
			Scope:      kit.Ternary(field.isManagerOnly, "manager", "environment"),
			EnvName:    field.envName,
		})
	}
	return types.SettingsSnapshot{
		Settings: states,
		ReadOnly: s.appConfig.UIConfigurationDisabled,
	}
}

func (s *SettingsService) settingValue[T any](ctx context.Context, key string, parse func(string) (T, error)) T {
	_, byKey := settingsFields()
	value, _, _, lookupErr := s.GetSettingsConfig().FieldByKey(key)
	parsed, parseErr := parse(value)
	if lookupErr == nil && parseErr == nil {
		return parsed
	}
	slog.WarnContext(ctx, "Failed to read setting; using default", "key", key, "error", errors.Join(lookupErr, parseErr))
	fallback, _ := parse(byKey[key].defaultValue)
	return fallback
}

func (s *SettingsService) GetBoolSetting(ctx context.Context, key string) bool {
	return s.settingValue(ctx, key, strconv.ParseBool)
}

func (s *SettingsService) GetIntSetting(ctx context.Context, key string) int {
	return s.settingValue(ctx, key, strconv.Atoi)
}

func (s *SettingsService) GetStringSetting(ctx context.Context, key string) string {
	return s.settingValue(ctx, key, func(value string) (string, error) {
		return value, nil
	})
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

// SetContainerAutoUpdateExclusion adds (excluded) or removes a container name
// in the autoUpdateExcludedContainers setting.
func (s *SettingsService) SetContainerAutoUpdateExclusion(ctx context.Context, containerName string, excluded bool) error {
	s.writes.Lock()
	defer s.writes.Unlock()
	if err := ctx.Err(); err != nil {
		return err
	}

	ordered := kit.Unique(kit.TrimNonEmpty(strings.Split(s.GetStringSetting(ctx, "autoUpdateExcludedContainers"), ",")))
	if excluded {
		ordered = kit.Unique(append(ordered, containerName))
	} else {
		ordered = slices.DeleteFunc(ordered, func(name string) bool { return name == containerName })
	}

	values, err := s.prepareUpdateValues([]libarcane.SettingUpdate{{Key: "autoUpdateExcludedContainers", Value: strings.Join(ordered, ",")}})
	if err != nil {
		return err
	}
	return s.persistSettings(ctx, values)
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

func (s *SettingsService) NormalizeProjectsDirectory(ctx context.Context) error {
	if envVal, ok := s.env["projectsDirectory"]; ok {
		slog.DebugContext(ctx, "Projects directory environment variable is set, skipping normalization", "value", envVal)
		return nil
	}
	return s.normalizeDirectorySetting(ctx, "projectsDirectory")
}

func (s *SettingsService) NormalizeBuildsDirectory(ctx context.Context) error {
	if envVal, ok := s.env["buildsDirectory"]; ok {
		slog.DebugContext(ctx, "Builds directory environment variable is set, skipping normalization", "value", envVal)
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
func (s *SettingsService) IsFeatureEnabled(ctx context.Context, id types.FeatureID) bool {
	definition, ok := types.LookupFeature(id)
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
func (s *SettingsService) RequireFeature(ctx context.Context, id types.FeatureID) error {
	if s.IsFeatureEnabled(ctx, id) {
		return nil
	}
	return fmt.Errorf("feature %s is disabled: %w", id, common.ErrFeatureDisabled)
}

type SettingsSearchService struct {
	categories []types.Category
}

func NewSettingsSearchService() *SettingsSearchService {
	return &SettingsSearchService{categories: search.BuildCategories[Settings](types.SearchProfileSettings)}
}

// GetSettingsCategories returns the category index initialized by the service.
func (s *SettingsSearchService) GetSettingsCategories() []types.Category {
	return s.categories
}
