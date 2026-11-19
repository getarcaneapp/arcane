package source

import (
	"bytes"
	"context"
	"encoding/json/v2"
	"errors"
	"fmt"
	"log/slog"
	"maps"
	"slices"
	"strings"
	"time"

	"gorm.io/gorm"

	"kit.getarcane.app"
	"kit.getarcane.app/cache"
	"kit.getarcane.app/concurrency"
	"kit.getarcane.app/sys/crypto"

	"getarcane.dev/arcane/v3/internal/common"
	"getarcane.dev/arcane/v3/internal/database"
	"getarcane.dev/arcane/v3/internal/environment"
	"getarcane.dev/arcane/v3/pkg/authz"
	"getarcane.dev/arcane/v3/pkg/projects"
	"getarcane.dev/arcane/v3/pkg/secretsource"
	"getarcane.dev/arcane/v3/resources"
	"getarcane.dev/types/v3"
)

type (
	ScopeNormalizer func(ctx context.Context, db *database.DB, allEnvironments bool, envIDs []string) ([]string, error)

	Service struct {
		db             *database.DB
		engine         *secretsource.Engine
		normalizeScope ScopeNormalizer
		entries        *cache.Cache[string, []types.EnvVariable]
		fetchLocks     concurrency.KeyedMutex
	}

	ServiceParams struct {
		DB             *database.DB
		DockerClient   secretsource.DockerClientFunc
		NormalizeScope ScopeNormalizer
	}

	TestRequest struct {
		Permissions *authz.PermissionSet
		Submitted   map[string]bool
		Input       types.SecretSourceTestInput
	}

	sourceConfig struct {
		inputs      map[string]string
		credentials map[string]string
		presetID    string
		path        string
		keyPrefix   string
		definition  types.SecretSourceDefinition
	}

	fetchResult struct {
		entries []types.EnvVariable
		skipped int
	}
)

const presetsDir = "secretsources"

var (
	ErrSecretSourceNotFound   = common.Classify(common.ErrNotFound, errors.New("secret source not found"))
	ErrSecretSourceConflict   = common.Classify(common.ErrConflict, errors.New("a secret source with this name already exists"))
	ErrDefinitionChangeDenied = common.Classify(common.ErrForbidden, fmt.Errorf("testing a saved secret source with changed settings requires %s", authz.PermVariablesSourcesUpdate))
	ErrUnsavedTestDenied      = common.Classify(common.ErrForbidden, fmt.Errorf("testing an unsaved secret source requires %s", authz.PermVariablesSourcesCreate))
)

func NewService(p ServiceParams) *Service {
	return &Service{
		db:             p.DB,
		engine:         secretsource.NewEngine(secretsource.EngineConfig{DockerClient: p.DockerClient}),
		normalizeScope: p.NormalizeScope,
		entries:        &cache.Cache[string, []types.EnvVariable]{},
	}
}

func (s *Service) Presets() ([]types.SecretSourcePreset, error) {
	return secretsource.LoadPresets(resources.FS, presetsDir)
}

func (s *Service) ValidateDefinition(source string) (types.SecretSourceDefinition, error) {
	definition, err := secretsource.ParseDefinition([]byte(source))
	if err != nil {
		return types.SecretSourceDefinition{}, common.Classify(common.ErrValidation, err)
	}
	return definition, nil
}

func (s *Service) List(ctx context.Context) ([]types.SecretSource, error) {
	var records []SecretSource
	if err := s.db.WithContext(ctx).Preload("Environments").Order("created_at, id").Find(&records).Error; err != nil {
		return nil, fmt.Errorf("failed to list secret sources: %w", err)
	}
	result := make([]types.SecretSource, 0, len(records))
	for _, record := range records {
		dto, err := toDTO(record)
		if err != nil {
			return nil, err
		}
		result = append(result, dto)
	}
	return result, nil
}

func (s *Service) Get(ctx context.Context, id string) (types.SecretSource, error) {
	record, err := s.load(ctx, id)
	if err != nil {
		return types.SecretSource{}, err
	}
	return toDTO(record)
}

func (s *Service) Create(ctx context.Context, input types.SecretSourceInput, submitted map[string]bool) (types.SecretSource, error) {
	name := strings.TrimSpace(input.Name)
	if name == "" {
		return types.SecretSource{}, common.Classify(common.ErrValidation, &types.FieldError{Field: "name", Err: errors.New("name is required")})
	}
	config, err := s.mergeConfig(sourceConfig{}, input, submitted)
	if err != nil {
		return types.SecretSource{}, err
	}
	envIDs, err := s.normalizeScope(ctx, s.db, input.AllEnvironments, input.EnvironmentIDs)
	if err != nil {
		return types.SecretSource{}, err
	}
	definition, err := json.Marshal(config.definition, json.Deterministic(true))
	if err != nil {
		return types.SecretSource{}, fmt.Errorf("failed to encode secret source definition: %w", err)
	}
	credentials, err := encodeCredentials(config.credentials)
	if err != nil {
		return types.SecretSource{}, err
	}

	record := SecretSource{
		Name:                 name,
		Kind:                 config.definition.Kind,
		PresetID:             config.presetID,
		Definition:           string(definition),
		Inputs:               config.inputs,
		Credentials:          credentials,
		Path:                 config.path,
		KeyPrefix:            config.keyPrefix,
		AllEnvironments:      len(envIDs) == 0,
		InjectIntoContainers: input.InjectIntoContainers,
		Enabled:              !submitted["enabled"] || input.Enabled,
	}
	err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if uniqueErr := ensureUniqueName(tx, name, ""); uniqueErr != nil {
			return uniqueErr
		}
		if createErr := tx.Omit("Environments").Create(&record).Error; createErr != nil {
			return fmt.Errorf("failed to create secret source: %w", createErr)
		}
		return environment.ReplaceScopeRows(tx, environment.ScopeRows{
			Table:          "secret_source_environments",
			OwnerColumn:    "secret_source_id",
			OwnerID:        record.ID,
			EnvironmentIDs: envIDs,
		})
	})
	if err != nil {
		return types.SecretSource{}, err
	}
	return s.reload(ctx, record.ID, true)
}

func (s *Service) Update(ctx context.Context, id string, input types.SecretSourceInput, submitted map[string]bool) (types.SecretSource, error) {
	record, err := s.load(ctx, id)
	if err != nil {
		return types.SecretSource{}, err
	}
	base, err := configOf(record)
	if err != nil {
		return types.SecretSource{}, err
	}
	config, err := s.mergeConfig(base, input, submitted)
	if err != nil {
		return types.SecretSource{}, err
	}
	name := kit.Ternary(submitted["name"], strings.TrimSpace(input.Name), record.Name)
	if name == "" {
		return types.SecretSource{}, common.Classify(common.ErrValidation, &types.FieldError{Field: "name", Err: errors.New("name is required")})
	}

	envIDs := environment.IDs(record.Environments)
	allEnvironments := record.AllEnvironments
	if submitted["allEnvironments"] || submitted["environmentIds"] {
		if envIDs, err = s.normalizeScope(
			ctx,
			s.db,
			kit.Ternary(submitted["allEnvironments"], input.AllEnvironments, record.AllEnvironments),
			kit.Ternary(submitted["environmentIds"], input.EnvironmentIDs, envIDs),
		); err != nil {
			return types.SecretSource{}, err
		}
		allEnvironments = len(envIDs) == 0
	}

	definition, err := json.Marshal(config.definition, json.Deterministic(true))
	if err != nil {
		return types.SecretSource{}, fmt.Errorf("failed to encode secret source definition: %w", err)
	}
	credentials, err := encodeCredentials(config.credentials)
	if err != nil {
		return types.SecretSource{}, err
	}

	updated := SecretSource{
		BaseModel:            record.BaseModel,
		LastFetchedAt:        record.LastFetchedAt,
		Name:                 name,
		Kind:                 config.definition.Kind,
		PresetID:             config.presetID,
		Definition:           string(definition),
		Inputs:               config.inputs,
		Credentials:          credentials,
		Path:                 config.path,
		KeyPrefix:            config.keyPrefix,
		LastError:            record.LastError,
		LastKeyCount:         record.LastKeyCount,
		LastSkippedKeyCount:  record.LastSkippedKeyCount,
		AllEnvironments:      allEnvironments,
		InjectIntoContainers: kit.Ternary(submitted["injectIntoContainers"], input.InjectIntoContainers, record.InjectIntoContainers),
		Enabled:              kit.Ternary(submitted["enabled"], input.Enabled, record.Enabled),
	}
	unlock, err := s.fetchLocks.LockContext(ctx, id)
	if err != nil {
		return types.SecretSource{}, err
	}
	defer unlock()

	err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if uniqueErr := ensureUniqueName(tx, name, id); uniqueErr != nil {
			return uniqueErr
		}
		result := tx.Model(&SecretSource{}).Where("id = ?", id).Select("*").Omit("Environments").Updates(&updated)
		if result.Error != nil {
			return fmt.Errorf("failed to update secret source: %w", result.Error)
		}
		if result.RowsAffected == 0 {
			return ErrSecretSourceNotFound
		}
		return environment.ReplaceScopeRows(tx, environment.ScopeRows{
			Table:          "secret_source_environments",
			OwnerColumn:    "secret_source_id",
			OwnerID:        id,
			EnvironmentIDs: envIDs,
		})
	})
	if err != nil {
		return types.SecretSource{}, err
	}
	if sameFetch(base, config) {
		return s.Get(ctx, id)
	}
	return s.refetch(ctx, id, true)
}

func (s *Service) Delete(ctx context.Context, id string) error {
	unlock, err := s.fetchLocks.LockContext(ctx, id)
	if err != nil {
		return err
	}
	defer unlock()

	err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		result := tx.Delete(&SecretSource{}, "id = ?", id)
		if result.Error != nil {
			return fmt.Errorf("failed to delete secret source: %w", result.Error)
		}
		if result.RowsAffected == 0 {
			return ErrSecretSourceNotFound
		}
		return tx.Exec("DELETE FROM secret_source_environments WHERE secret_source_id = ?", id).Error
	})
	if err != nil {
		return err
	}
	s.engine.ForgetSource(id)
	s.entries.Delete(id)
	return nil
}

func (s *Service) Refresh(ctx context.Context, id string) (types.SecretSource, error) {
	return s.reload(ctx, id, false)
}

func (s *Service) Test(ctx context.Context, req TestRequest) (types.SecretSourceTestResult, error) {
	id := strings.TrimSpace(req.Input.ID)
	base, err := s.testBase(ctx, id)
	if err != nil {
		return types.SecretSourceTestResult{}, err
	}
	config, err := s.mergeConfig(base, req.Input.SecretSourceInput, req.Submitted)
	if err != nil {
		return types.SecretSourceTestResult{}, err
	}
	if id == "" && !req.Permissions.Allows(authz.PermVariablesSourcesCreate, "") {
		return types.SecretSourceTestResult{}, ErrUnsavedTestDenied
	}
	if id != "" && !sameFetch(base, config) && !req.Permissions.Allows(authz.PermVariablesSourcesUpdate, "") {
		return types.SecretSourceTestResult{}, ErrDefinitionChangeDenied
	}

	result, err := s.fetchConfig(ctx, id, config)
	if err != nil {
		return types.SecretSourceTestResult{Error: err.Error()}, nil
	}
	keys := make([]string, 0, len(result.entries))
	for _, entry := range result.entries {
		keys = append(keys, entry.Key)
	}
	return types.SecretSourceTestResult{
		Keys:            keys,
		KeyCount:        len(keys),
		SkippedKeyCount: result.skipped,
		Success:         true,
	}, nil
}

func (s *Service) testBase(ctx context.Context, id string) (sourceConfig, error) {
	if id == "" {
		return sourceConfig{}, nil
	}
	record, err := s.load(ctx, id)
	if err != nil {
		return sourceConfig{}, err
	}
	return configOf(record)
}

func (s *Service) EffectiveLayers(ctx context.Context, envID string) ([][]types.EnvVariable, error) {
	var records []SecretSource
	if err := s.db.WithContext(ctx).Where("enabled = ?", true).Order("created_at, id").Find(&records).Error; err != nil {
		return nil, fmt.Errorf("failed to load secret sources: %w", err)
	}

	var global, scoped [][]types.EnvVariable
	for _, record := range records {
		entries, allEnvironments, err := s.scopedEntries(ctx, record.ID, envID)
		if err != nil {
			return nil, fmt.Errorf("secret source %q has no values: %w", record.Name, err)
		}
		if entries == nil {
			continue
		}
		if allEnvironments {
			global = append(global, entries)
		} else {
			scoped = append(scoped, entries)
		}
	}
	return append(global, scoped...), nil
}

func (s *Service) reload(ctx context.Context, id string, invalidate bool) (types.SecretSource, error) {
	unlock, err := s.fetchLocks.LockContext(ctx, id)
	if err != nil {
		return types.SecretSource{}, err
	}
	defer unlock()
	return s.refetch(ctx, id, invalidate)
}

func (s *Service) refetch(ctx context.Context, id string, invalidate bool) (types.SecretSource, error) {
	record, err := s.load(ctx, id)
	if err != nil {
		return types.SecretSource{}, err
	}
	if invalidate {
		s.engine.ForgetSource(id)
		s.entries.Delete(id)
	}

	var fetchErr error
	if record.Enabled || !invalidate {
		result, resultErr := s.fetch(ctx, record)
		s.recordStatus(ctx, id, result, resultErr)
		if resultErr == nil {
			s.entries.Set(cache.Entry[string, []types.EnvVariable]{
				Key:   id,
				Value: result.entries,
			})
		}
		fetchErr = resultErr
	}

	source, err := s.Get(ctx, id)
	if err != nil {
		return types.SecretSource{}, err
	}
	if fetchErr != nil && !invalidate {
		return source, common.Classify(common.ErrUnavailable, fmt.Errorf("failed to refresh secret source %q: %w", record.Name, fetchErr))
	}
	return source, nil
}

func (s *Service) scopedEntries(ctx context.Context, id, envID string) ([]types.EnvVariable, bool, error) {
	unlock, err := s.fetchLocks.LockContext(ctx, id)
	if err != nil {
		return nil, false, err
	}
	defer unlock()

	current, err := s.load(ctx, id)
	if errors.Is(err, ErrSecretSourceNotFound) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	inScope := current.AllEnvironments || slices.Contains(environment.IDs(current.Environments), envID)
	if !current.Enabled || !inScope {
		return nil, false, nil
	}
	if entries, ok, _ := s.entries.Get(current.ID); ok {
		return withInject(entries, current.InjectIntoContainers), current.AllEnvironments, nil
	}

	fetchCtx := context.WithoutCancel(ctx)
	result, err := s.fetch(fetchCtx, current)
	s.recordStatus(fetchCtx, current.ID, result, err)
	if err != nil {
		return nil, false, err
	}
	s.entries.Set(cache.Entry[string, []types.EnvVariable]{
		Key:   current.ID,
		Value: result.entries,
	})
	return withInject(result.entries, current.InjectIntoContainers), current.AllEnvironments, nil
}

func withInject(entries []types.EnvVariable, inject bool) []types.EnvVariable {
	result := make([]types.EnvVariable, 0, len(entries))
	for _, entry := range entries {
		result = append(result, types.EnvVariable{
			Key:      entry.Key,
			Value:    entry.Value,
			NoInject: !inject,
		})
	}
	return result
}

func (s *Service) fetch(ctx context.Context, record SecretSource) (fetchResult, error) {
	config, err := configOf(record)
	if err != nil {
		return fetchResult{}, err
	}
	return s.fetchConfig(ctx, record.ID, config)
}

func (s *Service) fetchConfig(ctx context.Context, sourceID string, config sourceConfig) (fetchResult, error) {
	entries, err := s.engine.Fetch(ctx, secretsource.Request{
		Definition:  config.definition,
		Inputs:      config.inputs,
		Credentials: config.credentials,
		SourceID:    sourceID,
		Path:        config.path,
	})
	if err != nil {
		return fetchResult{}, err
	}

	accepted := make([]types.EnvVariable, 0, len(entries))
	skipped := 0
	for _, entry := range entries {
		key := config.keyPrefix + strings.TrimSpace(entry.Key)
		if !projects.EnvKeyPattern.MatchString(key) {
			skipped++
			continue
		}
		accepted = append(accepted, types.EnvVariable{
			Key:   key,
			Value: entry.Value,
		})
	}
	return fetchResult{
		entries: accepted,
		skipped: skipped,
	}, nil
}

func (s *Service) recordStatus(ctx context.Context, id string, result fetchResult, fetchErr error) {
	updates := map[string]any{
		"last_error":             "",
		"last_fetched_at":        time.Now(),
		"last_key_count":         len(result.entries),
		"last_skipped_key_count": result.skipped,
	}
	if fetchErr != nil {
		updates = map[string]any{"last_error": fetchErr.Error()}
		slog.WarnContext(ctx, "Secret source fetch failed", "secretSourceId", id, "error", fetchErr)
	}
	if err := s.db.WithContext(ctx).Model(&SecretSource{}).Where("id = ?", id).UpdateColumns(updates).Error; err != nil {
		slog.WarnContext(ctx, "Failed to record secret source fetch status", "secretSourceId", id, "error", err)
	}
}

func (s *Service) mergeConfig(base sourceConfig, input types.SecretSourceInput, submitted map[string]bool) (sourceConfig, error) {
	definition := base.definition
	switch {
	case submitted["definition"] && strings.TrimSpace(input.Definition) != "":
		parsed, err := s.ValidateDefinition(input.Definition)
		if err != nil {
			return sourceConfig{}, err
		}
		definition = parsed
	case submitted["presetId"] && strings.TrimSpace(input.PresetID) != "":
		presets, err := s.Presets()
		if err != nil {
			return sourceConfig{}, err
		}
		index := slices.IndexFunc(presets, func(preset types.SecretSourcePreset) bool {
			return preset.Definition.ID == strings.TrimSpace(input.PresetID)
		})
		if index < 0 {
			return sourceConfig{}, common.Classify(common.ErrValidation, &types.FieldError{Field: "presetId", Err: fmt.Errorf("unknown preset %q", input.PresetID)})
		}
		definition = presets[index].Definition
	}
	if definition.Kind == "" {
		return sourceConfig{}, common.Classify(common.ErrValidation, &types.FieldError{Field: "definition", Err: errors.New("a presetId or definition is required")})
	}

	inputs := base.inputs
	if submitted["inputs"] {
		inputs = make(map[string]string, len(input.Inputs))
		for key, value := range input.Inputs {
			if trimmed := strings.TrimSpace(value); trimmed != "" {
				inputs[key] = trimmed
			}
		}
	}
	presetID := kit.Ternary(submitted["presetId"], strings.TrimSpace(input.PresetID), base.presetID)
	retargeted := !sameDefinition(base, sourceConfig{definition: definition, presetID: presetID}) ||
		inputs[secretsource.BaseURLInput] != base.inputs[secretsource.BaseURLInput] ||
		(definition.Kind == secretsource.KindContainer && !maps.Equal(inputs, base.inputs))
	credentials := make(map[string]string, len(base.credentials))
	if !retargeted {
		maps.Copy(credentials, base.credentials)
	}
	if submitted["credentials"] {
		for key, value := range input.Credentials {
			if value == "" {
				delete(credentials, key)
				continue
			}
			credentials[key] = value
		}
	}

	inputKeys := fieldKeys(definition.Inputs)
	credentialKeys := fieldKeys(definition.Credentials)
	for key := range inputs {
		if !slices.Contains(inputKeys, key) {
			return sourceConfig{}, common.Classify(common.ErrValidation, &types.FieldError{Field: "inputs", Err: fmt.Errorf("unknown input %q", key)})
		}
	}
	if submitted["credentials"] {
		for key := range input.Credentials {
			if !slices.Contains(credentialKeys, key) {
				return sourceConfig{}, common.Classify(common.ErrValidation, &types.FieldError{Field: "credentials", Err: fmt.Errorf("unknown credential %q", key)})
			}
		}
	}
	maps.DeleteFunc(credentials, func(key, _ string) bool { return !slices.Contains(credentialKeys, key) })
	path := kit.Ternary(submitted["path"], strings.TrimSpace(input.Path), base.path)
	if err := secretsource.ValidateValues(secretsource.Request{
		Definition:  definition,
		Inputs:      inputs,
		Credentials: credentials,
		Path:        path,
	}); err != nil {
		return sourceConfig{}, common.Classify(common.ErrValidation, err)
	}

	keyPrefix := kit.Ternary(submitted["keyPrefix"], strings.TrimSpace(input.KeyPrefix), base.keyPrefix)
	if keyPrefix != "" && !projects.EnvKeyPattern.MatchString(keyPrefix) {
		return sourceConfig{}, common.Classify(common.ErrValidation, &types.FieldError{Field: "keyPrefix", Err: fmt.Errorf("invalid key prefix %q", keyPrefix)})
	}

	return sourceConfig{
		definition:  definition,
		inputs:      inputs,
		credentials: credentials,
		presetID:    presetID,
		path:        path,
		keyPrefix:   keyPrefix,
	}, nil
}

func (s *Service) load(ctx context.Context, id string) (SecretSource, error) {
	var record SecretSource
	err := s.db.WithContext(ctx).Preload("Environments").First(&record, "id = ?", id).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return SecretSource{}, ErrSecretSourceNotFound
	}
	if err != nil {
		return SecretSource{}, fmt.Errorf("failed to load secret source: %w", err)
	}
	return record, nil
}

func configOf(record SecretSource) (sourceConfig, error) {
	var definition types.SecretSourceDefinition
	if err := json.Unmarshal([]byte(record.Definition), &definition); err != nil {
		return sourceConfig{}, fmt.Errorf("failed to decode secret source definition: %w", err)
	}
	credentials, err := decodeCredentials(record.Credentials)
	if err != nil {
		return sourceConfig{}, err
	}
	return sourceConfig{
		definition:  definition,
		inputs:      record.Inputs,
		credentials: credentials,
		presetID:    record.PresetID,
		path:        record.Path,
		keyPrefix:   record.KeyPrefix,
	}, nil
}

func encodeCredentials(credentials map[string]string) (string, error) {
	if len(credentials) == 0 {
		return "", nil
	}
	encoded, err := json.Marshal(credentials, json.Deterministic(true))
	if err != nil {
		return "", fmt.Errorf("failed to encode secret source credentials: %w", err)
	}
	encrypted, err := crypto.Encrypt(string(encoded))
	if err != nil {
		return "", fmt.Errorf("failed to encrypt secret source credentials: %w", err)
	}
	return encrypted, nil
}

func decodeCredentials(stored string) (map[string]string, error) {
	credentials := map[string]string{}
	if stored == "" {
		return credentials, nil
	}
	decrypted, err := crypto.Decrypt(stored)
	if err != nil {
		return nil, fmt.Errorf("failed to decrypt secret source credentials: %w", err)
	}
	if unmarshalErr := json.Unmarshal([]byte(decrypted), &credentials); unmarshalErr != nil {
		return nil, fmt.Errorf("failed to decode secret source credentials: %w", unmarshalErr)
	}
	return credentials, nil
}

func sameDefinition(a, b sourceConfig) bool {
	encodedA, errA := json.Marshal(a.definition, json.Deterministic(true))
	encodedB, errB := json.Marshal(b.definition, json.Deterministic(true))
	return a.presetID == b.presetID && errA == nil && errB == nil && bytes.Equal(encodedA, encodedB)
}

func sameFetch(a, b sourceConfig) bool {
	return sameDefinition(a, b) &&
		a.path == b.path &&
		a.keyPrefix == b.keyPrefix &&
		maps.Equal(a.inputs, b.inputs) &&
		maps.Equal(a.credentials, b.credentials)
}

func ensureUniqueName(tx *gorm.DB, name, excludeID string) error {
	var count int64
	if err := tx.Model(&SecretSource{}).Where("name = ? AND id <> ?", name, excludeID).Count(&count).Error; err != nil {
		return fmt.Errorf("failed to check secret source name: %w", err)
	}
	if count > 0 {
		return ErrSecretSourceConflict
	}
	return nil
}

func fieldKeys(fields []types.SecretSourceField) []string {
	keys := make([]string, 0, len(fields))
	for _, field := range fields {
		keys = append(keys, field.Key)
	}
	return keys
}

func toDTO(record SecretSource) (types.SecretSource, error) {
	config, err := configOf(record)
	if err != nil {
		return types.SecretSource{}, err
	}
	credentialKeys := slices.Sorted(maps.Keys(config.credentials))
	return types.SecretSource{
		CreatedAt:            record.CreatedAt,
		UpdatedAt:            record.UpdatedAt,
		LastFetchedAt:        record.LastFetchedAt,
		ID:                   record.ID,
		Name:                 record.Name,
		Kind:                 record.Kind,
		PresetID:             record.PresetID,
		Definition:           config.definition,
		Inputs:               record.Inputs,
		CredentialKeys:       credentialKeys,
		Path:                 record.Path,
		KeyPrefix:            record.KeyPrefix,
		EnvironmentIDs:       environment.IDs(record.Environments),
		LastError:            record.LastError,
		LastKeyCount:         record.LastKeyCount,
		LastSkippedKeyCount:  record.LastSkippedKeyCount,
		AllEnvironments:      record.AllEnvironments,
		InjectIntoContainers: record.InjectIntoContainers,
		Enabled:              record.Enabled,
	}, nil
}
