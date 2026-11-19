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
	Service struct {
		db         *database.DB
		engine     *secretsource.Engine
		entries    *cache.Cache[string, []types.EnvVariable]
		fetchLocks concurrency.KeyedMutex
	}

	ServiceParams struct {
		DB           *database.DB
		DockerClient secretsource.DockerClientFunc
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
)

const presetsDir = "secretsources"

var (
	ErrSecretSourceNotFound     = common.Classify(common.ErrNotFound, errors.New("secret source not found"))
	ErrSecretSourceConflict     = common.Classify(common.ErrConflict, errors.New("a secret source with this name already exists"))
	ErrSecretSourceNameRequired = common.Classify(common.ErrValidation, &types.FieldError{Field: "name", Err: errors.New("name is required")})
	ErrDefinitionChangeDenied   = common.Classify(common.ErrForbidden, fmt.Errorf("testing a saved secret source with changed settings requires %s", authz.PermVariablesSourcesUpdate))
	ErrUnsavedTestDenied        = common.Classify(common.ErrForbidden, fmt.Errorf("testing an unsaved secret source requires %s", authz.PermVariablesSourcesCreate))
	sourceScope                 = environment.ScopeTable{
		Table:       "secret_source_environments",
		OwnerColumn: "secret_source_id",
	}
)

func NewService(p ServiceParams) *Service {
	return &Service{
		db:      p.DB,
		engine:  secretsource.NewEngine(secretsource.EngineConfig{DockerClient: p.DockerClient}),
		entries: &cache.Cache[string, []types.EnvVariable]{},
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
		return types.SecretSource{}, ErrSecretSourceNameRequired
	}
	config, err := s.mergeConfig(sourceConfig{}, input, submitted)
	if err != nil {
		return types.SecretSource{}, err
	}
	envIDs, err := environment.NormalizeScope(ctx, s.db, input.AllEnvironments, input.EnvironmentIDs)
	if err != nil {
		return types.SecretSource{}, err
	}
	definition, credentials, err := encodeConfig(config)
	if err != nil {
		return types.SecretSource{}, err
	}

	record := SecretSource{
		Name:                 name,
		Kind:                 config.definition.Kind,
		PresetID:             config.presetID,
		Definition:           definition,
		Inputs:               config.inputs,
		Credentials:          credentials,
		Path:                 config.path,
		KeyPrefix:            config.keyPrefix,
		AllEnvironments:      len(envIDs) == 0,
		InjectIntoContainers: input.InjectIntoContainers,
		Enabled:              !submitted["enabled"] || input.Enabled,
	}
	err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if createErr := tx.Omit("Environments").Create(&record).Error; createErr != nil {
			if database.IsDuplicateKeyError(tx, createErr) {
				return ErrSecretSourceConflict
			}
			return fmt.Errorf("failed to create secret source: %w", createErr)
		}
		return sourceScope.Replace(tx, record.ID, envIDs)
	})
	if err != nil {
		return types.SecretSource{}, err
	}

	unlock, err := s.fetchLocks.LockContext(ctx, record.ID)
	if err != nil {
		return types.SecretSource{}, err
	}
	defer unlock()
	return s.refetch(ctx, record.ID, true)
}

func (s *Service) Update(ctx context.Context, id string, input types.SecretSourceInput, submitted map[string]bool) (types.SecretSource, error) {
	unlock, err := s.fetchLocks.LockContext(ctx, id)
	if err != nil {
		return types.SecretSource{}, err
	}
	defer unlock()

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
		return types.SecretSource{}, ErrSecretSourceNameRequired
	}
	envIDs, allEnvironments, err := environment.ResolveScope(ctx, s.db, environment.ScopeChange{
		Submitted:       submitted,
		Current:         record.Environments,
		EnvironmentIDs:  input.EnvironmentIDs,
		AllEnvironments: input.AllEnvironments,
		CurrentAll:      record.AllEnvironments,
	})
	if err != nil {
		return types.SecretSource{}, err
	}
	definition, credentials, err := encodeConfig(config)
	if err != nil {
		return types.SecretSource{}, err
	}

	updated := SecretSource{
		BaseModel:            record.BaseModel,
		LastFetchedAt:        record.LastFetchedAt,
		Name:                 name,
		Kind:                 config.definition.Kind,
		PresetID:             config.presetID,
		Definition:           definition,
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
	err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		result := tx.Model(&SecretSource{}).Where("id = ?", id).Select("*").Omit("Environments").Updates(&updated)
		if result.Error != nil {
			if database.IsDuplicateKeyError(tx, result.Error) {
				return ErrSecretSourceConflict
			}
			return fmt.Errorf("failed to update secret source: %w", result.Error)
		}
		if result.RowsAffected == 0 {
			return ErrSecretSourceNotFound
		}
		return sourceScope.Replace(tx, id, envIDs)
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
		return sourceScope.Replace(tx, id, nil)
	})
	if err != nil {
		return err
	}
	s.engine.ForgetSource(id)
	s.entries.Delete(id)
	return nil
}

func (s *Service) Refresh(ctx context.Context, id string) (types.SecretSource, error) {
	unlock, err := s.fetchLocks.LockContext(ctx, id)
	if err != nil {
		return types.SecretSource{}, err
	}
	defer unlock()
	return s.refetch(ctx, id, false)
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

	entries, skipped, err := s.fetchConfig(ctx, id, config)
	if err != nil {
		return types.SecretSourceTestResult{Error: err.Error()}, nil
	}
	keys := make([]string, 0, len(entries))
	for _, entry := range entries {
		keys = append(keys, entry.Key)
	}
	return types.SecretSourceTestResult{
		Keys:            keys,
		KeyCount:        len(keys),
		SkippedKeyCount: skipped,
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
	if err := s.db.WithContext(ctx).Preload("Environments").Where("enabled = ?", true).Order("created_at, id").Find(&records).Error; err != nil {
		return nil, fmt.Errorf("failed to load secret sources: %w", err)
	}

	var global, scoped [][]types.EnvVariable
	for _, record := range records {
		if !record.AllEnvironments && !slices.Contains(environment.IDs(record.Environments), envID) {
			continue
		}
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

func (s *Service) refetch(ctx context.Context, id string, invalidate bool) (types.SecretSource, error) {
	record, err := s.load(ctx, id)
	if err != nil {
		return types.SecretSource{}, err
	}
	if invalidate {
		s.engine.ForgetSource(id)
		s.entries.Delete(id)
		if record.Enabled {
			_, _ = s.fetch(ctx, record)
		}
		return s.Get(ctx, id)
	}

	_, fetchErr := s.fetch(ctx, record)
	source, err := s.Get(ctx, id)
	if err != nil {
		return types.SecretSource{}, err
	}
	if fetchErr != nil {
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
	if !current.Enabled || (!current.AllEnvironments && !slices.Contains(environment.IDs(current.Environments), envID)) {
		return nil, false, nil
	}
	entries, ok, _ := s.entries.Get(current.ID)
	if !ok {
		if entries, err = s.fetch(context.WithoutCancel(ctx), current); err != nil {
			return nil, false, err
		}
	}

	result := make([]types.EnvVariable, 0, len(entries))
	for _, entry := range entries {
		result = append(result, types.EnvVariable{
			Key:      entry.Key,
			Value:    entry.Value,
			NoInject: !current.InjectIntoContainers,
		})
	}
	return result, current.AllEnvironments, nil
}

func (s *Service) fetch(ctx context.Context, record SecretSource) ([]types.EnvVariable, error) {
	config, err := configOf(record)
	if err != nil {
		return nil, err
	}
	entries, skipped, err := s.fetchConfig(ctx, record.ID, config)
	status := map[string]any{
		"last_error":             "",
		"last_fetched_at":        time.Now(),
		"last_key_count":         len(entries),
		"last_skipped_key_count": skipped,
	}
	if err != nil {
		status = map[string]any{"last_error": err.Error()}
		slog.WarnContext(ctx, "Secret source fetch failed", "secretSourceId", record.ID, "error", err)
	}
	if updateErr := s.db.WithContext(ctx).Model(&SecretSource{}).Where("id = ?", record.ID).UpdateColumns(status).Error; updateErr != nil {
		slog.WarnContext(ctx, "Failed to record secret source fetch status", "secretSourceId", record.ID, "error", updateErr)
	}
	if err != nil {
		return nil, err
	}
	s.entries.Set(cache.Entry[string, []types.EnvVariable]{
		Key:   record.ID,
		Value: entries,
	})
	return entries, nil
}

func (s *Service) fetchConfig(ctx context.Context, sourceID string, config sourceConfig) ([]types.EnvVariable, int, error) {
	entries, err := s.engine.Fetch(ctx, secretsource.Request{
		Definition:  config.definition,
		Inputs:      config.inputs,
		Credentials: config.credentials,
		SourceID:    sourceID,
		Path:        config.path,
	})
	if err != nil {
		return nil, 0, err
	}

	accepted := make([]types.EnvVariable, 0, len(entries))
	for _, entry := range entries {
		key := config.keyPrefix + strings.TrimSpace(entry.Key)
		if projects.EnvKeyPattern.MatchString(key) {
			accepted = append(accepted, types.EnvVariable{
				Key:   key,
				Value: entry.Value,
			})
		}
	}
	return accepted, len(entries) - len(accepted), nil
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
	retargeted := presetID != base.presetID ||
		!sameDefinition(definition, base.definition) ||
		inputs[secretsource.BaseURLInput] != base.inputs[secretsource.BaseURLInput] ||
		(definition.Kind == secretsource.KindContainer && !maps.Equal(inputs, base.inputs))
	credentials := make(map[string]string, len(definition.Credentials))
	for _, field := range definition.Credentials {
		if stored, kept := base.credentials[field.Key]; kept && !retargeted {
			credentials[field.Key] = stored
		}
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

	config := sourceConfig{
		definition:  definition,
		inputs:      inputs,
		credentials: credentials,
		presetID:    presetID,
		path:        kit.Ternary(submitted["path"], strings.TrimSpace(input.Path), base.path),
		keyPrefix:   kit.Ternary(submitted["keyPrefix"], strings.TrimSpace(input.KeyPrefix), base.keyPrefix),
	}
	if err := secretsource.ValidateValues(secretsource.Request{
		Definition:  config.definition,
		Inputs:      config.inputs,
		Credentials: config.credentials,
		Path:        config.path,
	}); err != nil {
		return sourceConfig{}, common.Classify(common.ErrValidation, err)
	}
	if config.keyPrefix != "" && !projects.EnvKeyPattern.MatchString(config.keyPrefix) {
		return sourceConfig{}, common.Classify(common.ErrValidation, &types.FieldError{Field: "keyPrefix", Err: fmt.Errorf("invalid key prefix %q", config.keyPrefix)})
	}
	return config, nil
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
	credentials := map[string]string{}
	if record.Credentials != "" {
		decrypted, decryptErr := crypto.Decrypt(record.Credentials)
		if decryptErr != nil {
			return sourceConfig{}, fmt.Errorf("failed to decrypt secret source credentials: %w", decryptErr)
		}
		if unmarshalErr := json.Unmarshal([]byte(decrypted), &credentials); unmarshalErr != nil {
			return sourceConfig{}, fmt.Errorf("failed to decode secret source credentials: %w", unmarshalErr)
		}
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

func encodeConfig(config sourceConfig) (string, string, error) {
	definition, err := json.Marshal(config.definition, json.Deterministic(true))
	if err != nil {
		return "", "", fmt.Errorf("failed to encode secret source definition: %w", err)
	}
	if len(config.credentials) == 0 {
		return string(definition), "", nil
	}
	credentials, err := json.Marshal(config.credentials, json.Deterministic(true))
	if err != nil {
		return "", "", fmt.Errorf("failed to encode secret source credentials: %w", err)
	}
	encrypted, err := crypto.Encrypt(string(credentials))
	if err != nil {
		return "", "", fmt.Errorf("failed to encrypt secret source credentials: %w", err)
	}
	return string(definition), encrypted, nil
}

func sameDefinition(a, b types.SecretSourceDefinition) bool {
	encodedA, errA := json.Marshal(a, json.Deterministic(true))
	encodedB, errB := json.Marshal(b, json.Deterministic(true))
	return errA == nil && errB == nil && bytes.Equal(encodedA, encodedB)
}

func sameFetch(a, b sourceConfig) bool {
	return a.presetID == b.presetID &&
		sameDefinition(a.definition, b.definition) &&
		a.path == b.path &&
		a.keyPrefix == b.keyPrefix &&
		maps.Equal(a.inputs, b.inputs) &&
		maps.Equal(a.credentials, b.credentials)
}

func toDTO(record SecretSource) (types.SecretSource, error) {
	config, err := configOf(record)
	if err != nil {
		return types.SecretSource{}, err
	}
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
		CredentialKeys:       slices.Sorted(maps.Keys(config.credentials)),
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
