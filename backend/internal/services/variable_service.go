package services

import (
	"bufio"
	"context"
	json "encoding/json/v2"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/getarcaneapp/arcane/backend/v2/internal/common"
	"github.com/getarcaneapp/arcane/backend/v2/internal/database"
	"github.com/getarcaneapp/arcane/backend/v2/internal/models"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/projects"
	"github.com/getarcaneapp/arcane/types/v2/env"
	"go.getarcane.app/sys/crypto"
	"gorm.io/gorm"
)

// envKeyPattern is the POSIX env-name shape used to validate variable keys
// before they are persisted to .env.global. Keys that do not match are
// rejected with a common.InvalidEnvKeyError.
var envKeyPattern = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

const (
	globalVariablesImportedKVPrefix = "globalvars:imported:"
	agentVariablesPath              = "/api/environments/0/templates/variables"
	variableSyncTimeout             = 15 * time.Second
)

// VariableService manages manager-level global variables (DB source of truth)
// and materializes each environment's effective set into that environment's
// .env.global file: locally via TemplateService, remotely via the existing
// agent variables endpoint.
type VariableService struct {
	db                 *database.DB
	environmentService *EnvironmentService
	settingsService    *SettingsService
	kvService          *KVService

	statusMu   sync.RWMutex
	syncStatus map[string]env.EnvironmentSyncStatus
}

func NewVariableService(db *database.DB, environmentService *EnvironmentService, settingsService *SettingsService, kvService *KVService) *VariableService {
	return &VariableService{
		db:                 db,
		environmentService: environmentService,
		settingsService:    settingsService,
		kvService:          kvService,
		syncStatus:         make(map[string]env.EnvironmentSyncStatus),
	}
}

//
// CRUD
//

func (s *VariableService) ListVariables(ctx context.Context) ([]env.GlobalVariable, error) {
	variables, err := s.loadVariablesInternal(ctx)
	if err != nil {
		return nil, &common.GlobalVariablesRetrievalError{Err: err}
	}

	result := make([]env.GlobalVariable, 0, len(variables))
	for _, variable := range variables {
		result = append(result, globalVariableToDTOInternal(variable))
	}
	return result, nil
}

func (s *VariableService) CreateVariable(ctx context.Context, req env.CreateGlobalVariableRequest) (*env.GlobalVariable, error) {
	key := strings.TrimSpace(req.Key)
	if !envKeyPattern.MatchString(key) {
		return nil, &common.InvalidEnvKeyError{Key: req.Key}
	}

	envIDs, err := s.normalizeScopeInternal(ctx, req.AllEnvironments, req.EnvironmentIDs)
	if err != nil {
		return nil, err
	}
	allEnvironments := len(envIDs) == 0

	value := req.Value
	if req.IsSecret {
		if value, err = crypto.Encrypt(value); err != nil {
			return nil, &common.GlobalVariablesUpdateError{Err: fmt.Errorf("failed to encrypt secret value: %w", err)}
		}
	}

	variable := models.GlobalVariable{
		Key:             key,
		Value:           value,
		IsSecret:        req.IsSecret,
		AllEnvironments: allEnvironments,
	}

	err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := s.validateScopeConflictInternal(tx, key, "", allEnvironments, envIDs); err != nil {
			return err
		}
		if err := tx.Omit("Environments").Create(&variable).Error; err != nil {
			return fmt.Errorf("failed to create global variable: %w", err)
		}
		return replaceVariableScopeRowsInternal(tx, variable.ID, envIDs)
	})
	if err != nil {
		return nil, wrapVariableMutationErrorInternal(err)
	}

	variable.Environments = environmentsFromIDsInternal(envIDs)
	dto := globalVariableToDTOInternal(variable)
	return &dto, nil
}

func (s *VariableService) UpdateVariable(ctx context.Context, id string, req env.UpdateGlobalVariableRequest) (*env.GlobalVariable, error) {
	var variable models.GlobalVariable
	if err := s.db.WithContext(ctx).Preload("Environments").First(&variable, "id = ?", id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, &common.GlobalVariableNotFoundError{}
		}
		return nil, &common.GlobalVariablesRetrievalError{Err: err}
	}

	key := variable.Key
	if req.Key != nil {
		key = strings.TrimSpace(*req.Key)
		if !envKeyPattern.MatchString(key) {
			return nil, &common.InvalidEnvKeyError{Key: *req.Key}
		}
	}

	isSecret := variable.IsSecret
	if req.IsSecret != nil {
		isSecret = *req.IsSecret
	}

	value, err := resolveUpdatedValueInternal(&variable, req.Value, isSecret)
	if err != nil {
		return nil, err
	}

	envIDs, allEnvironments, err := s.resolveUpdatedScopeInternal(ctx, &variable, req)
	if err != nil {
		return nil, err
	}

	variable.Key = key
	variable.Value = value
	variable.IsSecret = isSecret
	variable.AllEnvironments = allEnvironments

	err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := s.validateScopeConflictInternal(tx, key, variable.ID, allEnvironments, envIDs); err != nil {
			return err
		}
		if err := tx.Omit("Environments").Save(&variable).Error; err != nil {
			return fmt.Errorf("failed to update global variable: %w", err)
		}
		return replaceVariableScopeRowsInternal(tx, variable.ID, envIDs)
	})
	if err != nil {
		return nil, wrapVariableMutationErrorInternal(err)
	}

	variable.Environments = environmentsFromIDsInternal(envIDs)
	dto := globalVariableToDTOInternal(variable)
	return &dto, nil
}

// resolveUpdatedValueInternal computes the stored value for an update:
// encrypts when the result is secret and enforces that a secret cannot be
// made readable without a replacement value (revealing the stored one).
func resolveUpdatedValueInternal(variable *models.GlobalVariable, reqValue *string, isSecret bool) (string, error) {
	if variable.IsSecret && !isSecret && reqValue == nil {
		return "", &common.GlobalVariableSecretValueRequiredError{}
	}

	switch {
	case reqValue != nil:
		if !isSecret {
			return *reqValue, nil
		}
		encrypted, err := crypto.Encrypt(*reqValue)
		if err != nil {
			return "", &common.GlobalVariablesUpdateError{Err: fmt.Errorf("failed to encrypt secret value: %w", err)}
		}
		return encrypted, nil
	case isSecret && !variable.IsSecret:
		// Readable value becoming secret keeps its current plaintext, encrypted.
		encrypted, err := crypto.Encrypt(variable.Value)
		if err != nil {
			return "", &common.GlobalVariablesUpdateError{Err: fmt.Errorf("failed to encrypt secret value: %w", err)}
		}
		return encrypted, nil
	default:
		return variable.Value, nil
	}
}

// resolveUpdatedScopeInternal merges the requested scope change (if any) with
// the variable's current scope and normalizes it.
func (s *VariableService) resolveUpdatedScopeInternal(ctx context.Context, variable *models.GlobalVariable, req env.UpdateGlobalVariableRequest) ([]string, bool, error) {
	envIDs := scopedEnvironmentIDsInternal(variable.Environments)
	if req.AllEnvironments == nil && req.EnvironmentIDs == nil {
		return envIDs, variable.AllEnvironments, nil
	}

	requestedAll := variable.AllEnvironments
	if req.AllEnvironments != nil {
		requestedAll = *req.AllEnvironments
	}
	requestedIDs := envIDs
	if req.EnvironmentIDs != nil {
		requestedIDs = *req.EnvironmentIDs
	}

	normalized, err := s.normalizeScopeInternal(ctx, requestedAll, requestedIDs)
	if err != nil {
		return nil, false, err
	}
	return normalized, len(normalized) == 0, nil
}

func (s *VariableService) DeleteVariable(ctx context.Context, id string) error {
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		result := tx.Delete(&models.GlobalVariable{}, "id = ?", id)
		if result.Error != nil {
			return fmt.Errorf("failed to delete global variable: %w", result.Error)
		}
		if result.RowsAffected == 0 {
			return &common.GlobalVariableNotFoundError{}
		}
		return tx.Exec("DELETE FROM global_variable_environments WHERE global_variable_id = ?", id).Error
	})
	if err != nil {
		return wrapVariableMutationErrorInternal(err)
	}
	return nil
}

//
// Materialization
//

// resolveEffectiveVariablesInternal computes the variable set for one
// environment: all-environment variables first, env-scoped variables override.
// Secret values are decrypted; variables that fail to decrypt are skipped.
func (s *VariableService) resolveEffectiveVariablesInternal(ctx context.Context, envID string) ([]env.Variable, error) {
	variables, err := s.loadVariablesInternal(ctx)
	if err != nil {
		return nil, err
	}

	effective := make(map[string]string)
	for _, scoped := range []bool{false, true} {
		for _, variable := range variables {
			if variable.AllEnvironments == scoped {
				continue
			}
			if scoped && !slices.Contains(scopedEnvironmentIDsInternal(variable.Environments), envID) {
				continue
			}
			value := variable.Value
			if variable.IsSecret {
				decrypted, err := crypto.Decrypt(value)
				if err != nil {
					slog.WarnContext(ctx, "Failed to decrypt global variable for sync; skipping",
						"variable_id", variable.ID, "key", variable.Key, "error", err)
					continue
				}
				value = decrypted
			}
			effective[variable.Key] = value
		}
	}

	keys := make([]string, 0, len(effective))
	for key := range effective {
		keys = append(keys, key)
	}
	slices.Sort(keys)

	result := make([]env.Variable, 0, len(effective))
	for _, key := range keys {
		result = append(result, env.Variable{Key: key, Value: effective[key]})
	}
	return result, nil
}

// SyncEnvironment materializes the effective variable set into one
// environment's .env.global. Remote environments are imported once before the
// first overwrite so pre-existing per-env variables are preserved.
func (s *VariableService) SyncEnvironment(ctx context.Context, envID string) error {
	err := s.syncEnvironmentInternal(ctx, envID)
	s.recordSyncStatusInternal(envID, err)
	return err
}

func (s *VariableService) syncEnvironmentInternal(ctx context.Context, envID string) error {
	if envID != localEnvironmentID {
		if err := s.importRemoteLegacyVarsOnceInternal(ctx, envID); err != nil {
			return fmt.Errorf("failed to import existing variables from environment %s: %w", envID, err)
		}
	}

	vars, err := s.resolveEffectiveVariablesInternal(ctx, envID)
	if err != nil {
		return err
	}

	if envID == localEnvironmentID {
		return s.WriteLocalEnvFile(ctx, vars)
	}

	body, err := json.Marshal(env.Summary{Variables: vars})
	if err != nil {
		return fmt.Errorf("failed to marshal variables for sync: %w", err)
	}

	var out struct {
		Success bool `json:"success"`
	}
	return s.environmentService.ProxyJSONRequest(ctx, envID, http.MethodPut, agentVariablesPath, body, &out)
}

// syncTargetsInternal returns the local environment plus every enabled
// environment, deduplicated.
func (s *VariableService) syncTargetsInternal(ctx context.Context) []string {
	envIDs, err := s.environmentService.listEnabledEnvironmentIDsInternal(ctx)
	if err != nil {
		slog.ErrorContext(ctx, "Failed to list environments for global variable sync", "error", err)
		envIDs = nil
	}
	if !slices.Contains(envIDs, localEnvironmentID) {
		envIDs = append([]string{localEnvironmentID}, envIDs...)
	}
	return envIDs
}

// SyncAll pushes the effective variable set to the local environment and every
// enabled remote environment in parallel, waiting for all of them. Failures
// are recorded per environment and never abort the other pushes.
func (s *VariableService) SyncAll(ctx context.Context) []env.EnvironmentSyncStatus {
	s.syncEnvironmentsInternal(ctx, s.syncTargetsInternal(ctx))
	return s.SyncStatuses()
}

// SyncAllBackground materializes the local environment synchronously (a fast
// file write, no network) and pushes to remote environments in the background,
// so mutation requests never block on slow or unreachable agents. Remotes are
// reported as "pending" until the background push records their result.
func (s *VariableService) SyncAllBackground(ctx context.Context) []env.EnvironmentSyncStatus {
	if err := s.SyncEnvironment(ctx, localEnvironmentID); err != nil {
		slog.WarnContext(ctx, "Failed to sync global variables locally", "error", err)
	}

	remoteIDs := slices.DeleteFunc(s.syncTargetsInternal(ctx), func(id string) bool { return id == localEnvironmentID })
	for _, envID := range remoteIDs {
		s.recordSyncPendingInternal(envID)
	}

	bgCtx := context.WithoutCancel(ctx)
	go s.syncEnvironmentsInternal(bgCtx, remoteIDs)

	return s.SyncStatuses()
}

func (s *VariableService) syncEnvironmentsInternal(ctx context.Context, envIDs []string) {
	var wg sync.WaitGroup
	for _, envID := range envIDs {
		wg.Go(func() {
			syncCtx, cancel := context.WithTimeout(ctx, variableSyncTimeout)
			defer cancel()
			if err := s.SyncEnvironment(syncCtx, envID); err != nil {
				slog.WarnContext(syncCtx, "Failed to sync global variables to environment", "environment_id", envID, "error", err)
			}
		})
	}
	wg.Wait()
}

func (s *VariableService) SyncStatuses() []env.EnvironmentSyncStatus {
	s.statusMu.RLock()
	defer s.statusMu.RUnlock()

	statuses := make([]env.EnvironmentSyncStatus, 0, len(s.syncStatus))
	for _, status := range s.syncStatus {
		statuses = append(statuses, status)
	}
	slices.SortFunc(statuses, func(a, b env.EnvironmentSyncStatus) int {
		return strings.Compare(a.EnvironmentID, b.EnvironmentID)
	})
	return statuses
}

func (s *VariableService) recordSyncPendingInternal(envID string) {
	s.statusMu.Lock()
	status := env.EnvironmentSyncStatus{EnvironmentID: envID, Status: "pending"}
	if previous, ok := s.syncStatus[envID]; ok {
		status.LastSyncedAt = previous.LastSyncedAt
	}
	s.syncStatus[envID] = status
	s.statusMu.Unlock()
}

func (s *VariableService) recordSyncStatusInternal(envID string, err error) {
	status := env.EnvironmentSyncStatus{EnvironmentID: envID, Status: "synced"}
	if err != nil {
		status.Status = "error"
		status.Error = err.Error()
	} else {
		now := time.Now()
		status.LastSyncedAt = &now
	}

	s.statusMu.Lock()
	if err != nil {
		if previous, ok := s.syncStatus[envID]; ok {
			status.LastSyncedAt = previous.LastSyncedAt
		}
	}
	s.syncStatus[envID] = status
	s.statusMu.Unlock()
}

//
// Local .env.global file primitives (moved from TemplateService). These back
// the local materialization and the agent-side variables endpoint.
//

func (s *VariableService) localEnvFilePathInternal(ctx context.Context) (string, error) {
	projectsDirectory, err := projects.GetProjectsDirectory(ctx, s.settingsService.GetStringSetting(ctx, "projectsDirectory", "/app/data/projects"))
	if err != nil {
		return "", fmt.Errorf("failed to get projects directory: %w", err)
	}

	return filepath.Join(projectsDirectory, projects.GlobalEnvFileName), nil
}

func (s *VariableService) ReadLocalEnvFile(ctx context.Context) ([]env.Variable, error) {
	envPath, err := s.localEnvFilePathInternal(ctx)
	if err != nil {
		return nil, err
	}

	if _, err := os.Stat(envPath); os.IsNotExist(err) {
		slog.DebugContext(ctx, "Global variables file does not exist yet", "path", envPath)
		return []env.Variable{}, nil
	}

	file, err := os.Open(envPath)
	if err != nil {
		return nil, fmt.Errorf("failed to open global variables file: %w", err)
	}
	defer func() { _ = file.Close() }()

	vars := []env.Variable{}
	scanner := bufio.NewScanner(file)
	lineNum := 0

	for scanner.Scan() {
		lineNum++
		line := strings.TrimSpace(scanner.Text())

		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		parts := strings.SplitN(line, "=", 2)
		if len(parts) != 2 {
			slog.WarnContext(ctx, "Skipping invalid line in global variables file",
				"line", lineNum,
				"content", line)
			continue
		}

		key := strings.TrimSpace(parts[0])
		value := strings.TrimSpace(parts[1])

		if len(value) >= 2 {
			if (strings.HasPrefix(value, `"`) && strings.HasSuffix(value, `"`)) ||
				(strings.HasPrefix(value, `'`) && strings.HasSuffix(value, `'`)) {
				value = value[1 : len(value)-1]
			}
		}

		vars = append(vars, env.Variable{
			Key:   key,
			Value: value,
		})
	}

	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("error reading global variables file: %w", err)
	}

	return vars, nil
}

func (s *VariableService) WriteLocalEnvFile(ctx context.Context, vars []env.Variable) error {
	envPath, err := s.localEnvFilePathInternal(ctx)
	if err != nil {
		return err
	}

	projectsDirectory := filepath.Dir(envPath)
	if err := os.MkdirAll(projectsDirectory, common.DirPerm); err != nil {
		return fmt.Errorf("failed to create projects directory: %w", err)
	}

	var builder strings.Builder
	builder.WriteString("# Global Environment Variables\n")
	builder.WriteString("# These variables are available to all projects\n")
	builder.WriteString("# Last updated: ")
	builder.WriteString(time.Now().Format(time.RFC3339))
	builder.WriteString("\n\n")

	for _, v := range vars {
		if strings.TrimSpace(v.Key) == "" {
			continue
		}

		key := strings.TrimSpace(v.Key)
		if !envKeyPattern.MatchString(key) {
			return &common.InvalidEnvKeyError{Key: v.Key}
		}
		// The value is written verbatim: leading/trailing whitespace is
		// intentional and survives inside the quoted form below.
		value := v.Value

		if strings.ContainsAny(value, " \t\n\r#") {
			value = fmt.Sprintf(`"%s"`, strings.ReplaceAll(value, `"`, `\"`))
		}

		_, _ = fmt.Fprintf(&builder, "%s=%s\n", key, value)
	}

	if err := projects.WriteFileWithPerm(envPath, builder.String(), common.FilePerm); err != nil {
		return fmt.Errorf("failed to write global variables file: %w", err)
	}

	slog.InfoContext(ctx, "Updated global variables",
		"path", envPath,
		"count", len(vars))

	return nil
}

//
// One-time import of pre-existing .env.global contents
//

// ImportLegacyLocalEnvFile imports the manager's existing .env.global entries
// as all-environment readable variables before the first materialization
// overwrites the file. Runs once, guarded by a KV flag.
func (s *VariableService) ImportLegacyLocalEnvFile(ctx context.Context) error {
	flag := globalVariablesImportedKVPrefix + localEnvironmentID
	imported, err := s.kvService.GetBool(ctx, flag, false)
	if err != nil {
		return err
	}
	if imported {
		return nil
	}

	vars, err := s.ReadLocalEnvFile(ctx)
	if err != nil {
		return err
	}
	if err := s.importVariablesInternal(ctx, vars, ""); err != nil {
		return err
	}

	return s.kvService.SetBool(ctx, flag, true)
}

// importRemoteLegacyVarsOnceInternal pulls a remote environment's current
// variables before the manager's first push and stores any key the manager
// would not already produce for that environment as an env-scoped variable.
// An import failure aborts the push so existing agent data is never wiped.
func (s *VariableService) importRemoteLegacyVarsOnceInternal(ctx context.Context, envID string) error {
	flag := globalVariablesImportedKVPrefix + envID
	imported, err := s.kvService.GetBool(ctx, flag, false)
	if err != nil {
		return err
	}
	if imported {
		return nil
	}

	var out struct {
		Success bool           `json:"success"`
		Data    []env.Variable `json:"data"`
	}
	if err := s.environmentService.ProxyJSONRequest(ctx, envID, http.MethodGet, agentVariablesPath, nil, &out); err != nil {
		return err
	}

	if err := s.importVariablesInternal(ctx, out.Data, envID); err != nil {
		return err
	}

	return s.kvService.SetBool(ctx, flag, true)
}

// importVariablesInternal inserts legacy entries that the DB does not cover
// yet. An empty scopeEnvID imports as all-environments; otherwise the entry is
// scoped to that single environment.
func (s *VariableService) importVariablesInternal(ctx context.Context, vars []env.Variable, scopeEnvID string) error {
	if len(vars) == 0 {
		return nil
	}

	existing, err := s.loadVariablesInternal(ctx)
	if err != nil {
		return err
	}
	covered := make(map[string]bool, len(existing))
	for _, variable := range existing {
		if variable.AllEnvironments || scopeEnvID == "" ||
			slices.Contains(scopedEnvironmentIDsInternal(variable.Environments), scopeEnvID) {
			covered[variable.Key] = true
		}
	}

	for _, entry := range vars {
		key := strings.TrimSpace(entry.Key)
		if key == "" || covered[key] || !envKeyPattern.MatchString(key) {
			continue
		}

		variable := models.GlobalVariable{
			Key:             key,
			Value:           entry.Value,
			AllEnvironments: scopeEnvID == "",
		}
		err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
			if err := tx.Omit("Environments").Create(&variable).Error; err != nil {
				return err
			}
			if scopeEnvID == "" {
				return nil
			}
			return replaceVariableScopeRowsInternal(tx, variable.ID, []string{scopeEnvID})
		})
		if err != nil {
			return fmt.Errorf("failed to import legacy variable %q: %w", key, err)
		}
		covered[key] = true
		slog.InfoContext(ctx, "Imported legacy global variable", "key", key, "environment_id", scopeEnvID)
	}

	return nil
}

//
// Helpers
//

func (s *VariableService) loadVariablesInternal(ctx context.Context) ([]models.GlobalVariable, error) {
	var variables []models.GlobalVariable
	if err := s.db.WithContext(ctx).Preload("Environments").Order("key").Find(&variables).Error; err != nil {
		return nil, fmt.Errorf("failed to load global variables: %w", err)
	}
	return variables, nil
}

// normalizeScopeInternal validates the requested scope and returns the
// deduplicated environment ID list ([] means all environments).
func (s *VariableService) normalizeScopeInternal(ctx context.Context, allEnvironments bool, envIDs []string) ([]string, error) {
	if allEnvironments {
		return nil, nil
	}

	unique := make([]string, 0, len(envIDs))
	for _, id := range envIDs {
		id = strings.TrimSpace(id)
		if id != "" && !slices.Contains(unique, id) {
			unique = append(unique, id)
		}
	}
	// An explicitly specific scope with no environments must not silently
	// widen to all environments.
	if len(unique) == 0 {
		return nil, &common.GlobalVariableScopeRequiredError{}
	}

	var count int64
	if err := s.db.WithContext(ctx).Model(&models.Environment{}).Where("id IN ?", unique).Count(&count).Error; err != nil {
		return nil, &common.GlobalVariablesUpdateError{Err: fmt.Errorf("failed to validate environment scope: %w", err)}
	}
	if count != int64(len(unique)) {
		return nil, &common.GlobalVariablesUpdateError{Err: errors.New("scope references unknown environments")}
	}
	return unique, nil
}

// validateScopeConflictInternal enforces the duplicate-key rule: a key may
// exist once per overlapping scope. The same key as both an all-environments
// variable and an env-scoped variable is allowed (that is the override).
func (s *VariableService) validateScopeConflictInternal(tx *gorm.DB, key string, excludeID string, allEnvironments bool, envIDs []string) error {
	var others []models.GlobalVariable
	query := tx.Preload("Environments").Where("key = ?", key)
	if excludeID != "" {
		query = query.Where("id <> ?", excludeID)
	}
	if err := query.Find(&others).Error; err != nil {
		return fmt.Errorf("failed to check for conflicting variables: %w", err)
	}

	for _, other := range others {
		if allEnvironments != other.AllEnvironments {
			continue
		}
		if allEnvironments {
			return &common.GlobalVariableConflictError{Key: key}
		}
		otherIDs := scopedEnvironmentIDsInternal(other.Environments)
		for _, id := range envIDs {
			if slices.Contains(otherIDs, id) {
				return &common.GlobalVariableConflictError{Key: key}
			}
		}
	}
	return nil
}

// replaceVariableScopeRowsInternal rewrites the join rows explicitly instead
// of relying on FK cascades (the sqlite FK pragma is DSN-dependent).
func replaceVariableScopeRowsInternal(tx *gorm.DB, variableID string, envIDs []string) error {
	if err := tx.Exec("DELETE FROM global_variable_environments WHERE global_variable_id = ?", variableID).Error; err != nil {
		return fmt.Errorf("failed to clear variable scope: %w", err)
	}
	for _, envID := range envIDs {
		if err := tx.Exec("INSERT INTO global_variable_environments (global_variable_id, environment_id) VALUES (?, ?)", variableID, envID).Error; err != nil {
			return fmt.Errorf("failed to set variable scope: %w", err)
		}
	}
	return nil
}

func wrapVariableMutationErrorInternal(err error) error {
	if err == nil ||
		common.IsGlobalVariableConflictError(err) ||
		common.IsGlobalVariableNotFoundError(err) ||
		common.IsInvalidEnvKeyError(err) {
		return err
	}
	return &common.GlobalVariablesUpdateError{Err: err}
}

func scopedEnvironmentIDsInternal(environments []models.Environment) []string {
	ids := make([]string, 0, len(environments))
	for _, environment := range environments {
		ids = append(ids, environment.ID)
	}
	return ids
}

func environmentsFromIDsInternal(envIDs []string) []models.Environment {
	environments := make([]models.Environment, 0, len(envIDs))
	for _, id := range envIDs {
		environments = append(environments, models.Environment{BaseModel: models.BaseModel{ID: id}})
	}
	return environments
}

func globalVariableToDTOInternal(variable models.GlobalVariable) env.GlobalVariable {
	value := variable.Value
	if variable.IsSecret {
		value = ""
	}
	return env.GlobalVariable{
		ID:              variable.ID,
		Key:             variable.Key,
		Value:           value,
		IsSecret:        variable.IsSecret,
		AllEnvironments: variable.AllEnvironments,
		EnvironmentIDs:  scopedEnvironmentIDsInternal(variable.Environments),
		CreatedAt:       variable.CreatedAt,
		UpdatedAt:       variable.UpdatedAt,
	}
}
