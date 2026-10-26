package variable

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json/v2"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"maps"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/getarcaneapp/arcane/types/v3"
	"go.getarcane.app/acfs"
	"go.getarcane.app/kit/pkg"
	"go.getarcane.app/sys/crypto"
	"gorm.io/gorm"

	"github.com/getarcaneapp/arcane/backend/v3/internal/common"
	"github.com/getarcaneapp/arcane/backend/v3/internal/database"
	"github.com/getarcaneapp/arcane/backend/v3/internal/environment"
	"github.com/getarcaneapp/arcane/backend/v3/internal/kv"
	"github.com/getarcaneapp/arcane/backend/v3/internal/settings"
	"github.com/getarcaneapp/arcane/backend/v3/pkg/libarcane/agent"
	"github.com/getarcaneapp/arcane/backend/v3/pkg/projects"
	"github.com/getarcaneapp/arcane/backend/v3/pkg/utils"
)

// envKeyPattern is the POSIX env-name shape used to validate variable keys
// before they are persisted to .env.global. Keys that do not match are
// rejected with common.ErrInvalidEnvKey.
var envKeyPattern = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

const (
	globalVariablesImportedKVPrefix = "globalvars:imported:"
	agentVariablesPath              = "/api/environments/0/templates/variables"
	variableSyncTimeout             = 15 * time.Second
)

// VariableService manages manager-level global variables (DB source of truth)
// and materializes each environment's effective set into that environment's
// .env.global file: locally via template.TemplateService, remotely via the existing
// agent variables endpoint.
type VariableService struct {
	db                            *database.DB
	proxyEnvironmentJSON          func(context.Context, string, string, string, []byte, any) error
	listEnabledRemoteEnvironments func(context.Context) ([]environment.Environment, error)
	settingsService               *settings.SettingsService
	kvService                     *kv.KVService
	importLocks                   utils.KeyedMutex

	statusMu   sync.RWMutex
	syncStatus map[string]types.EnvironmentSyncStatus

	// syncGate skips the agent PUT when the effective variable set is unchanged.
	syncGate utils.SyncGate
}

func NewVariableService(db *database.DB, environmentService *environment.EnvironmentService, settingsService *settings.SettingsService, kvService *kv.KVService) *VariableService {
	service := &VariableService{
		syncGate:        utils.SyncGate{Expiry: environment.SyncDeliveryExpiry},
		db:              db,
		settingsService: settingsService,
		kvService:       kvService,
		syncStatus:      make(map[string]types.EnvironmentSyncStatus),
	}
	if environmentService != nil {
		service.proxyEnvironmentJSON = environmentService.ProxyJSONRequest
		service.listEnabledRemoteEnvironments = environmentService.ListEnabledRemoteEnvironments
	}
	return service
}

func (s *VariableService) ListVariables(ctx context.Context) ([]types.GlobalVariable, error) {
	variables, err := s.loadVariables(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to retrieve global variables: %w", err)
	}

	result := make([]types.GlobalVariable, 0, len(variables))
	for _, variable := range variables {
		result = append(result, globalVariableToDTO(variable))
	}
	return result, nil
}

func (s *VariableService) CreateVariable(ctx context.Context, req types.GlobalVariable) (*types.GlobalVariable, error) {
	key := strings.TrimSpace(req.Key)
	if !envKeyPattern.MatchString(key) {
		return nil, common.Classify(common.ErrInvalidEnvKey, fmt.Errorf("invalid env key %q (must match [A-Za-z_][A-Za-z0-9_]*)", key))
	}

	envIDs, err := s.normalizeScope(ctx, req.AllEnvironments, req.EnvironmentIDs)
	if err != nil {
		return nil, err
	}
	allEnvironments := len(envIDs) == 0

	value := req.Value
	if req.IsSecret {
		if value, err = crypto.Encrypt(value); err != nil {
			return nil, fmt.Errorf("failed to update global variables: %w", err)
		}
	}

	variable := GlobalVariable{
		Key:             key,
		Value:           value,
		IsSecret:        req.IsSecret,
		AllEnvironments: allEnvironments,
	}

	err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if validateScopeConflictErr := s.validateScopeConflict(tx, key, "", allEnvironments, envIDs); validateScopeConflictErr != nil {
			return validateScopeConflictErr
		}
		if createVariableErr := tx.Omit("Environments").Create(&variable).Error; createVariableErr != nil {
			return fmt.Errorf("failed to create global variable: %w", createVariableErr)
		}
		return replaceVariableScopeRows(tx, variable.ID, envIDs)
	})
	if err != nil {
		return nil, wrapVariableMutationError(err)
	}

	variable.Environments = environmentsFromIDs(envIDs)
	dto := globalVariableToDTO(variable)
	return &dto, nil
}

func (s *VariableService) UpdateVariable(ctx context.Context, id string, req types.GlobalVariable, submitted map[string]bool) (*types.GlobalVariable, error) {
	var variable GlobalVariable
	if err := s.db.WithContext(ctx).Preload("Environments").First(&variable, "id = ?", id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, common.Classify(common.ErrGlobalVariableNotFound, errors.New("global variable not found"))
		}
		return nil, fmt.Errorf("failed to retrieve global variables: %w", err)
	}

	key := kit.Ternary(submitted["key"], strings.TrimSpace(req.Key), variable.Key)
	if !envKeyPattern.MatchString(key) {
		return nil, common.Classify(common.ErrInvalidEnvKey, fmt.Errorf("invalid env key %q (must match [A-Za-z_][A-Za-z0-9_]*)", key))
	}

	isSecret := kit.Ternary(submitted["isSecret"], req.IsSecret, variable.IsSecret)

	value, err := resolveUpdatedValue(&variable, req.Value, submitted["value"], isSecret)
	if err != nil {
		return nil, err
	}

	envIDs, allEnvironments, err := s.resolveUpdatedScope(ctx, &variable, req, submitted)
	if err != nil {
		return nil, err
	}

	variable.Key = key
	variable.Value = value
	variable.IsSecret = isSecret
	variable.AllEnvironments = allEnvironments

	err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if validateScopeConflictErr := s.validateScopeConflict(tx, key, variable.ID, allEnvironments, envIDs); validateScopeConflictErr != nil {
			return validateScopeConflictErr
		}
		if updateVariableErr := tx.Omit("Environments").Save(&variable).Error; updateVariableErr != nil {
			return fmt.Errorf("failed to update global variable: %w", updateVariableErr)
		}
		return replaceVariableScopeRows(tx, variable.ID, envIDs)
	})
	if err != nil {
		return nil, wrapVariableMutationError(err)
	}

	variable.Environments = environmentsFromIDs(envIDs)
	dto := globalVariableToDTO(variable)
	return &dto, nil
}

// resolveUpdatedValue computes the stored value for an update:
// encrypts when the result is secret and enforces that a secret cannot be
// made readable without a replacement value (revealing the stored one).
func resolveUpdatedValue(variable *GlobalVariable, reqValue string, valueSubmitted, isSecret bool) (string, error) {
	if variable.IsSecret && !isSecret && !valueSubmitted {
		return "", common.Classify(
			common.ErrGlobalVariableSecretValueRequired,
			errors.New(
				"a new value is required when making a secret variable readable",
			),
		)
	}

	switch {
	case valueSubmitted:
		if !isSecret {
			return reqValue, nil
		}
		encrypted, err := crypto.Encrypt(reqValue)
		if err != nil {
			return "", fmt.Errorf("failed to update global variables: %w", err)
		}
		return encrypted, nil
	case isSecret && !variable.IsSecret:
		// Readable value becoming secret keeps its current plaintext, encrypted.
		encrypted, err := crypto.Encrypt(variable.Value)
		if err != nil {
			return "", fmt.Errorf("failed to update global variables: %w", err)
		}
		return encrypted, nil
	default:
		return variable.Value, nil
	}
}

// resolveUpdatedScope merges the requested scope change (if any) with
// the variable's current scope and normalizes it.
func (s *VariableService) resolveUpdatedScope(ctx context.Context, variable *GlobalVariable, req types.GlobalVariable, submitted map[string]bool) ([]string, bool, error) {
	envIDs := scopedEnvironmentIDs(variable.Environments)
	if !submitted["allEnvironments"] && !submitted["environmentIds"] {
		return envIDs, variable.AllEnvironments, nil
	}

	normalized, err := s.normalizeScope(
		ctx,
		kit.Ternary(submitted["allEnvironments"], req.AllEnvironments, variable.AllEnvironments),
		kit.Ternary(submitted["environmentIds"], req.EnvironmentIDs, envIDs),
	)
	if err != nil {
		return nil, false, err
	}
	return normalized, len(normalized) == 0, nil
}

func (s *VariableService) DeleteVariable(ctx context.Context, id string) error {
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		result := tx.Delete(&GlobalVariable{}, "id = ?", id)
		if result.Error != nil {
			return fmt.Errorf("failed to delete global variable: %w", result.Error)
		}
		if result.RowsAffected == 0 {
			return common.Classify(common.ErrGlobalVariableNotFound, errors.New("global variable not found"))
		}
		return tx.Exec("DELETE FROM global_variable_environments WHERE global_variable_id = ?", id).Error
	})
	if err != nil {
		return wrapVariableMutationError(err)
	}
	return nil
}

// resolveEffectiveVariables computes the variable set for one
// environment: all-environment variables first, env-scoped variables override.
// Secret values are decrypted; variables that fail to decrypt are skipped.
func (s *VariableService) resolveEffectiveVariables(ctx context.Context, envID string) ([]types.EnvVariable, error) {
	variables, err := s.loadVariables(ctx)
	if err != nil {
		return nil, err
	}

	effective := make(map[string]string)
	for _, scoped := range []bool{false, true} {
		for _, variable := range variables {
			if variable.AllEnvironments == scoped {
				continue
			}
			if scoped && !slices.Contains(scopedEnvironmentIDs(variable.Environments), envID) {
				continue
			}
			value := variable.Value
			if variable.IsSecret {
				decrypted, decryptErr := crypto.Decrypt(value)
				if decryptErr != nil {
					slog.WarnContext(ctx, "Failed to decrypt global variable for sync; skipping",
						"variableId", variable.ID, "key", variable.Key, "error", decryptErr)
					continue
				}
				value = decrypted
			}
			effective[variable.Key] = value
		}
	}

	keys := slices.Sorted(maps.Keys(effective))

	result := make([]types.EnvVariable, 0, len(effective))
	for _, key := range keys {
		result = append(result, types.EnvVariable{Key: key, Value: effective[key]})
	}
	return result, nil
}

// SyncEnvironment materializes the effective variable set into one
// environment's .env.global. Remote environments are imported once before the
// first overwrite so pre-existing per-env variables are preserved.
func (s *VariableService) SyncEnvironment(ctx context.Context, envID string) error {
	err := s.syncEnvironment(ctx, envID)
	s.recordSyncStatus(envID, err)
	return err
}

// ForgetSyncState makes the next SyncEnvironment resend regardless of change.
func (s *VariableService) ForgetSyncState(envID string) {
	s.syncGate.Forget(envID)
	s.statusMu.Lock()
	delete(s.syncStatus, envID)
	s.statusMu.Unlock()
}

func (s *VariableService) syncEnvironment(ctx context.Context, envID string) error {
	if envID != environment.LocalEnvironmentID {
		if err := s.importRemoteLegacyVarsOnce(ctx, envID); err != nil {
			return fmt.Errorf("failed to import existing variables from environment %s: %w", envID, err)
		}
	}

	vars, err := s.resolveEffectiveVariables(ctx, envID)
	if err != nil {
		return err
	}

	if envID == environment.LocalEnvironmentID {
		return s.WriteLocalEnvFile(ctx, vars)
	}

	body, err := json.Marshal(types.EnvSummary{Variables: vars})
	if err != nil {
		return fmt.Errorf("failed to marshal variables for sync: %w", err)
	}
	unchanged, finishDelivery, err := s.syncGate.Begin(ctx, envID, agentVariablesPath, body)
	if err != nil {
		return fmt.Errorf("variable sync cancelled while waiting for an in-flight delivery: %w", err)
	}
	if unchanged {
		return nil
	}

	var out struct {
		Success bool `json:"success"`
	}
	err = s.proxyEnvironmentJSON(ctx, envID, http.MethodPut, agentVariablesPath, body, &out)
	finishDelivery(err == nil)
	return err
}

// syncTargets returns the local environment plus every enabled
// environment, deduplicated.
func (s *VariableService) syncTargets(ctx context.Context) []environment.Environment {
	remotes, err := s.listEnabledRemoteEnvironments(ctx)
	if err != nil {
		slog.ErrorContext(ctx, "Failed to list environments for global variable sync", "error", err)
		remotes = nil
	}
	return append([]environment.Environment{{ID: environment.LocalEnvironmentID}}, slices.DeleteFunc(remotes, func(remote environment.Environment) bool {
		return strings.TrimSpace(kit.FromPtr(remote.AccessToken)) == ""
	})...)
}

// SyncAll pushes the effective variable set to the local environment and every
// enabled remote environment in parallel, waiting for all of them. Failures
// are recorded per environment and never abort the other pushes.
func (s *VariableService) SyncAll(ctx context.Context) []types.EnvironmentSyncStatus {
	targets := s.syncTargets(ctx)
	// An explicit sync exists to repair agents, so every target is resent even
	// when the manager's payload has not changed.
	for _, target := range targets {
		s.ForgetSyncState(target.ID)
	}
	s.syncEnvironments(ctx, targets)
	return s.SyncStatuses()
}

// SyncAllBackground materializes the local environment synchronously (a fast
// file write, no network) and pushes to remote environments in the background,
// so mutation requests never block on slow or unreachable agents. Remotes are
// reported as "pending" until the background push records their result.
func (s *VariableService) SyncAllBackground(ctx context.Context) []types.EnvironmentSyncStatus {
	if err := s.SyncEnvironment(ctx, environment.LocalEnvironmentID); err != nil {
		slog.WarnContext(ctx, "Failed to sync global variables locally", "error", err)
	}

	remotes := slices.DeleteFunc(s.syncTargets(ctx), func(target environment.Environment) bool {
		return target.ID == environment.LocalEnvironmentID
	})
	for _, target := range remotes {
		s.recordSyncPending(target.ID)
	}

	bgCtx := context.WithoutCancel(ctx)
	go s.syncEnvironments(bgCtx, remotes)

	return s.SyncStatuses()
}

func (s *VariableService) syncEnvironments(ctx context.Context, targets []environment.Environment) {
	var wg sync.WaitGroup
	for _, target := range targets {
		wg.Go(func() {
			syncCtx, cancel := context.WithTimeout(ctx, variableSyncTimeout)
			defer cancel()
			if target.ID != environment.LocalEnvironmentID && !agent.HasActiveTunnel(target.ID) {
				if _, ok := agent.WaitForActiveTunnel(syncCtx, target.ID, agent.DefaultTunnelAcquireTimeout).Get(); !ok {
					s.recordSyncPending(target.ID)
					return
				}
			}
			if err := s.SyncEnvironment(syncCtx, target.ID); err != nil {
				slog.WarnContext(syncCtx, "Failed to sync global variables to environment", "environmentId", target.ID, "error", err)
			}
		})
	}
	wg.Wait()
}

func (s *VariableService) SyncStatuses() []types.EnvironmentSyncStatus {
	s.statusMu.RLock()
	defer s.statusMu.RUnlock()

	statuses := make([]types.EnvironmentSyncStatus, 0, len(s.syncStatus))
	for _, status := range s.syncStatus {
		statuses = append(statuses, status)
	}
	slices.SortFunc(statuses, func(a, b types.EnvironmentSyncStatus) int {
		return strings.Compare(a.EnvironmentID, b.EnvironmentID)
	})
	return statuses
}

func (s *VariableService) recordSyncPending(envID string) {
	s.statusMu.Lock()
	status := types.EnvironmentSyncStatus{EnvironmentID: envID, Status: "pending"}
	if previous, ok := s.syncStatus[envID]; ok {
		status.LastSyncedAt = previous.LastSyncedAt
	}
	s.syncStatus[envID] = status
	s.statusMu.Unlock()
}

func (s *VariableService) recordSyncStatus(envID string, err error) {
	status := types.EnvironmentSyncStatus{EnvironmentID: envID, Status: "synced"}
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

func (s *VariableService) localEnvFilePath(ctx context.Context) (string, error) {
	projectsDirectory, err := projects.GetProjectsDirectory(ctx, s.settingsService.GetStringSetting(ctx, "projectsDirectory"))
	if err != nil {
		return "", fmt.Errorf("failed to get projects directory: %w", err)
	}

	return filepath.Join(projectsDirectory, projects.GlobalEnvFileName), nil
}

func (s *VariableService) ReadLocalEnvFile(ctx context.Context) ([]types.EnvVariable, error) {
	envPath, err := s.localEnvFilePath(ctx)
	if err != nil {
		return nil, err
	}

	content, err := acfs.ReadFile(ctx, filepath.Dir(envPath), "/"+projects.GlobalEnvFileName)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			slog.DebugContext(ctx, "Global variables file does not exist yet", "path", envPath)
			return []types.EnvVariable{}, nil
		}
		return nil, fmt.Errorf("failed to open global variables file: %w", err)
	}

	vars := []types.EnvVariable{}
	scanner := bufio.NewScanner(bytes.NewReader(content))
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

		vars = append(vars, types.EnvVariable{
			Key:   key,
			Value: value,
		})
	}

	if readVariablesErr := scanner.Err(); readVariablesErr != nil {
		return nil, fmt.Errorf("error reading global variables file: %w", readVariablesErr)
	}

	return vars, nil
}

func (s *VariableService) WriteLocalEnvFile(ctx context.Context, vars []types.EnvVariable) error {
	envPath, err := s.localEnvFilePath(ctx)
	if err != nil {
		return err
	}

	// The data directory is the confinement root for the write, so it is created
	// through os before acfs opens it.
	projectsDirectory := filepath.Dir(envPath)
	if mkdirAllErr := os.MkdirAll(projectsDirectory, utils.DirPerm); mkdirAllErr != nil {
		return fmt.Errorf("failed to create projects directory: %w", mkdirAllErr)
	}

	var builder strings.Builder
	builder.WriteString("# Global Environment Variables\n")
	builder.WriteString("# These variables are available to all projects\n\n")

	for _, v := range vars {
		if strings.TrimSpace(v.Key) == "" {
			continue
		}

		key := strings.TrimSpace(v.Key)
		if !envKeyPattern.MatchString(key) {
			return common.Classify(common.ErrInvalidEnvKey, fmt.Errorf("invalid env key %q (must match [A-Za-z_][A-Za-z0-9_]*)", key))
		}

		// Preserve intentional whitespace inside the quoted value.
		value := v.Value

		if strings.ContainsAny(value, " \t\n\r#") {
			value = `"` + strings.ReplaceAll(value, `"`, `\"`) + `"`
		}

		_, _ = fmt.Fprintf(&builder, "%s=%s\n", key, value)
	}

	mode := utils.FilePerm
	if entry, statErr := acfs.Stat(ctx, projectsDirectory, "/"+projects.GlobalEnvFileName, false); statErr == nil {
		mode = os.FileMode(entry.UnixMode).Perm()
	}
	if writeErr := acfs.Write(ctx, projectsDirectory, "/"+projects.GlobalEnvFileName, []byte(builder.String()), acfs.WriteOptions{Mode: mode, InPlace: true}); writeErr != nil {
		return fmt.Errorf("failed to write global variables file: %w", writeErr)
	}

	slog.DebugContext(ctx, "Synchronized global variables file",
		"path", envPath,
		"count", len(vars))

	return nil
}

// importRemoteLegacyVarsOnce pulls a remote environment's current
// variables before the manager's first push and stores any key the manager
// would not already produce for that environment as an env-scoped variable.
// An import failure aborts the push so existing agent data is never wiped.
func (s *VariableService) importRemoteLegacyVarsOnce(ctx context.Context, envID string) error {
	unlock, err := s.importLocks.LockContext(ctx, envID)
	if err != nil {
		return err
	}
	defer unlock()

	flag := globalVariablesImportedKVPrefix + envID
	imported, err := s.kvService.GetBool(ctx, flag, false)
	if err != nil {
		return err
	}
	if imported {
		return nil
	}

	var out struct {
		Success bool                `json:"success"`
		Data    []types.EnvVariable `json:"data"`
	}
	if proxyEnvironmentJSONErr := s.proxyEnvironmentJSON(ctx, envID, http.MethodGet, agentVariablesPath, nil, &out); proxyEnvironmentJSONErr != nil {
		return proxyEnvironmentJSONErr
	}

	if importVariablesErr := s.importVariables(ctx, out.Data, envID); importVariablesErr != nil {
		return importVariablesErr
	}

	return s.kvService.SetBool(ctx, flag, true)
}

// importVariables inserts legacy entries that the DB does not cover
// yet. An empty scopeEnvID imports as all-environments; otherwise the entry is
// scoped to that single environment.
func (s *VariableService) importVariables(ctx context.Context, vars []types.EnvVariable, scopeEnvID string) error {
	if len(vars) == 0 {
		return nil
	}

	existing, err := s.loadVariables(ctx)
	if err != nil {
		return err
	}
	covered := make(map[string]bool, len(existing))
	for _, variable := range existing {
		if variable.AllEnvironments || scopeEnvID == "" ||
			slices.Contains(scopedEnvironmentIDs(variable.Environments), scopeEnvID) {
			covered[variable.Key] = true
		}
	}

	for _, entry := range vars {
		key := strings.TrimSpace(entry.Key)
		if key == "" || covered[key] || !envKeyPattern.MatchString(key) {
			continue
		}

		variable := GlobalVariable{
			Key:             key,
			Value:           entry.Value,
			AllEnvironments: scopeEnvID == "",
		}
		transactionErr := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
			if createImportedVariableErr := tx.Omit("Environments").Create(&variable).Error; createImportedVariableErr != nil {
				return createImportedVariableErr
			}
			if scopeEnvID == "" {
				return nil
			}
			return replaceVariableScopeRows(tx, variable.ID, []string{scopeEnvID})
		})
		if transactionErr != nil {
			return fmt.Errorf("failed to import legacy variable %q: %w", key, transactionErr)
		}
		covered[key] = true
		slog.InfoContext(ctx, "Imported legacy global variable", "key", key, "environmentId", scopeEnvID)
	}

	return nil
}

func (s *VariableService) loadVariables(ctx context.Context) ([]GlobalVariable, error) {
	var variables []GlobalVariable
	if err := s.db.WithContext(ctx).Preload("Environments").Order("key").Find(&variables).Error; err != nil {
		return nil, fmt.Errorf("failed to load global variables: %w", err)
	}
	return variables, nil
}

// normalizeScope validates the requested scope and returns the
// deduplicated environment ID list ([] means all environments).
func (s *VariableService) normalizeScope(ctx context.Context, allEnvironments bool, envIDs []string) ([]string, error) {
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
		return nil, common.Classify(
			common.ErrGlobalVariableScopeRequired,
			errors.New(
				"at least one environment is required when a variable is not scoped to all environments",
			),
		)
	}

	var count int64
	if err := s.db.WithContext(ctx).Model(&environment.Environment{}).Where("id IN ?", unique).Count(&count).Error; err != nil {
		return nil, fmt.Errorf("failed to update global variables: %w", err)
	}
	if count != int64(len(unique)) {
		return nil, common.Classify(
			common.ErrGlobalVariableUnknownEnvironment,
			errors.New("failed to update global variables: one or more environments do not exist"),
		)
	}
	return unique, nil
}

// validateScopeConflict enforces the duplicate-key rule: a key may
// exist once per overlapping scope. The same key as both an all-environments
// variable and an env-scoped variable is allowed (that is the override).
func (s *VariableService) validateScopeConflict(tx *gorm.DB, key, excludeID string, allEnvironments bool, envIDs []string) error {
	var others []GlobalVariable
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
			return common.Classify(
				common.ErrGlobalVariableConflict,
				fmt.Errorf(
					"a variable named %q already exists for an overlapping environment scope",
					key,
				),
			)
		}
		otherIDs := scopedEnvironmentIDs(other.Environments)
		for _, id := range envIDs {
			if slices.Contains(otherIDs, id) {
				return common.Classify(
					common.ErrGlobalVariableConflict,
					fmt.Errorf(
						"a variable named %q already exists for an overlapping environment scope",
						key,
					),
				)
			}
		}
	}
	return nil
}

// replaceVariableScopeRows rewrites the join rows explicitly instead
// of relying on FK cascades (the sqlite FK pragma is DSN-dependent).
func replaceVariableScopeRows(tx *gorm.DB, variableID string, envIDs []string) error {
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

func wrapVariableMutationError(err error) error {
	if err == nil || errors.Is(err, common.ErrGlobalVariableConflict) || errors.Is(err, common.ErrGlobalVariableNotFound) || errors.Is(err, common.ErrInvalidEnvKey) {
		return err
	}
	return fmt.Errorf("failed to update global variables: %w", err)
}

func scopedEnvironmentIDs(environments []environment.Environment) []string {
	ids := make([]string, 0, len(environments))
	for _, environment := range environments {
		ids = append(ids, environment.ID)
	}
	return ids
}

func environmentsFromIDs(envIDs []string) []environment.Environment {
	environments := make([]environment.Environment, 0, len(envIDs))
	for _, id := range envIDs {
		environments = append(environments, environment.Environment{ID: id})
	}
	return environments
}

func globalVariableToDTO(variable GlobalVariable) types.GlobalVariable {
	value := kit.Ternary(variable.IsSecret, "", variable.Value)
	return types.GlobalVariable{
		ID:              variable.ID,
		Key:             variable.Key,
		Value:           value,
		IsSecret:        variable.IsSecret,
		AllEnvironments: variable.AllEnvironments,
		EnvironmentIDs:  scopedEnvironmentIDs(variable.Environments),
		CreatedAt:       variable.CreatedAt,
		UpdatedAt:       variable.UpdatedAt,
	}
}
