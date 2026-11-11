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
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"sync"
	"syscall"
	"time"

	"go.uber.org/fx"
	"gorm.io/gorm"

	"kit.getarcane.app"
	"kit.getarcane.app/acfs"
	acfstypes "kit.getarcane.app/acfs/types"
	"kit.getarcane.app/concurrency"
	kitfs "kit.getarcane.app/fs"
	"kit.getarcane.app/sys/crypto"

	"getarcane.dev/arcane/v3/internal/common"
	"getarcane.dev/arcane/v3/internal/database"
	"getarcane.dev/arcane/v3/internal/environment"
	"getarcane.dev/arcane/v3/internal/kv"
	"getarcane.dev/arcane/v3/internal/settings"
	"getarcane.dev/arcane/v3/pkg/agent"
	"getarcane.dev/arcane/v3/pkg/projects"
	"getarcane.dev/types/v3"
)

type (
	VariableService struct {
		db              *database.DB
		environments    environment.RemoteProxy
		tunnels         *agent.TunnelRegistry
		settingsService *settings.SettingsService
		kvService       *kv.KVService
		syncStatus      map[string]types.EnvironmentSyncStatus

		importLocks concurrency.KeyedMutex

		// syncGate skips the agent PUT when the effective variable set is unchanged.
		syncGate concurrency.SyncGate

		statusMu  sync.RWMutex
		envFileMu sync.Mutex
	}

	VariableServiceParams struct {
		fx.In

		DB              *database.DB
		Environments    environment.RemoteProxy
		Tunnels         *agent.TunnelRegistry
		SettingsService *settings.SettingsService
		KVService       *kv.KVService
	}
)

const (
	globalVariablesImportedKVPrefix = "globalvars:imported:"
	agentVariablesPath              = "/api/agents/0/templates/variables"
	variableSyncTimeout             = 15 * time.Second
)

var envKeyPattern = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

func validateVariableKey(key string) error {
	if envKeyPattern.MatchString(key) {
		return nil
	}
	return common.Classify(common.ErrInvalidEnvKey, &types.FieldError{Field: "key", Err: fmt.Errorf("invalid key %q: use letters, numbers, and underscores, and do not start with a number", key)})
}

func NewVariableService(p VariableServiceParams) *VariableService {
	return &VariableService{
		syncGate:        concurrency.SyncGate{Expiry: environment.SyncDeliveryExpiry},
		db:              p.DB,
		environments:    p.Environments,
		tunnels:         p.Tunnels,
		settingsService: p.SettingsService,
		kvService:       p.KVService,
		syncStatus:      make(map[string]types.EnvironmentSyncStatus),
	}
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
	if validateKeyErr := validateVariableKey(key); validateKeyErr != nil {
		return nil, validateKeyErr
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
		Key:                  key,
		Value:                value,
		IsSecret:             req.IsSecret,
		AllEnvironments:      allEnvironments,
		InjectIntoContainers: req.InjectIntoContainers,
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
	if validateKeyErr := validateVariableKey(key); validateKeyErr != nil {
		return nil, validateKeyErr
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
	variable.InjectIntoContainers = kit.Ternary(submitted["injectIntoContainers"], req.InjectIntoContainers, variable.InjectIntoContainers)

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

func (s *VariableService) resolveEffectiveVariables(ctx context.Context, envID string) ([]types.EnvVariable, error) {
	variables, err := s.loadVariables(ctx)
	if err != nil {
		return nil, err
	}

	result := make([]types.EnvVariable, 0, len(variables))
	positions := make(map[string]int, len(variables))
	scopedKeys := make(map[string]bool)
	for _, variable := range variables {
		scoped := !variable.AllEnvironments
		if scoped && !slices.Contains(scopedEnvironmentIDs(variable.Environments), envID) {
			continue
		}
		if !scoped && scopedKeys[variable.Key] {
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
		entry := types.EnvVariable{
			Key:      variable.Key,
			Value:    value,
			NoInject: !variable.InjectIntoContainers,
		}
		if index, ok := positions[variable.Key]; ok {
			result[index] = entry
		} else {
			positions[variable.Key] = len(result)
			result = append(result, entry)
		}
		scopedKeys[variable.Key] = scopedKeys[variable.Key] || scoped
	}
	return result, nil
}

func (s *VariableService) SyncEnvironment(ctx context.Context, envID string) error {
	err := s.syncEnvironment(ctx, envID)
	s.recordSyncStatus(envID, err)
	return err
}

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
	err = s.environments.ProxyJSONRequest(ctx, envID, http.MethodPut, agentVariablesPath, body, &out)
	finishDelivery(err == nil)
	return err
}

func (s *VariableService) syncTargets(ctx context.Context) []string {
	remoteIDs, err := s.environments.ListEnabledRemoteEnvironmentIDs(ctx)
	if err != nil {
		slog.ErrorContext(ctx, "Failed to list environments for global variable sync", "error", err)
	}
	return append([]string{environment.LocalEnvironmentID}, remoteIDs...)
}

func (s *VariableService) SyncAll(ctx context.Context) []types.EnvironmentSyncStatus {
	targets := s.syncTargets(ctx)
	// An explicit sync exists to repair agents, so every target is resent even
	// when the manager's payload has not changed.
	for _, target := range targets {
		s.ForgetSyncState(target)
	}
	s.syncEnvironments(ctx, targets)
	return s.SyncStatuses()
}

func (s *VariableService) SyncAllBackground(ctx context.Context) []types.EnvironmentSyncStatus {
	if err := s.SyncEnvironment(ctx, environment.LocalEnvironmentID); err != nil {
		slog.WarnContext(ctx, "Failed to sync global variables locally", "error", err)
	}

	remotes := slices.DeleteFunc(s.syncTargets(ctx), func(target string) bool {
		return target == environment.LocalEnvironmentID
	})
	for _, target := range remotes {
		s.recordSyncPending(target)
	}

	bgCtx := context.WithoutCancel(ctx)
	go s.syncEnvironments(bgCtx, remotes)

	return s.SyncStatuses()
}

func (s *VariableService) syncEnvironments(ctx context.Context, targets []string) {
	var wg sync.WaitGroup
	for _, target := range targets {
		wg.Go(func() {
			syncCtx, cancel := context.WithTimeout(ctx, variableSyncTimeout)
			defer cancel()
			if target != environment.LocalEnvironmentID && s.tunnels.WaitForActive(syncCtx, target, agent.DefaultTunnelAcquireTimeout).IsAbsent() {
				s.recordSyncPending(target)
				return
			}
			if err := s.SyncEnvironment(syncCtx, target); err != nil {
				slog.WarnContext(syncCtx, "Failed to sync global variables to environment", "environmentId", target, "error", err)
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

	content, err := acfs.Root{Path: filepath.Dir(envPath)}.ReadFile(ctx, "/"+projects.GlobalEnvFileName)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			slog.DebugContext(ctx, "Global variables file does not exist yet", "path", envPath)
			return []types.EnvVariable{}, nil
		}
		return nil, fmt.Errorf("failed to open global variables file: %w", err)
	}

	noInject := projects.ParseNoInjectDirectives(string(content))
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
			Key:      key,
			Value:    value,
			NoInject: noInject[key],
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

	projectsDirectory := filepath.Dir(envPath)
	if mkdirAllErr := (acfs.Host{}).MkdirAll(ctx, projectsDirectory, kitfs.DirPerm); mkdirAllErr != nil {
		return fmt.Errorf("failed to create projects directory: %w", mkdirAllErr)
	}

	var builder strings.Builder
	builder.WriteString("# Global Environment Variables\n")
	builder.WriteString("# These variables are available to all projects\n")

	for _, v := range vars {
		if v.NoInject {
			_, _ = fmt.Fprintf(&builder, "# %s %s\n", projects.GlobalEnvNoInjectDirective, strings.TrimSpace(v.Key))
		}
	}
	builder.WriteString("\n")

	for _, v := range vars {
		if strings.TrimSpace(v.Key) == "" {
			continue
		}

		key := strings.TrimSpace(v.Key)
		if validateKeyErr := validateVariableKey(key); validateKeyErr != nil {
			return validateKeyErr
		}

		// Preserve intentional whitespace inside the quoted value.
		value := v.Value

		if strings.ContainsAny(value, " \t\n\r#") {
			value = `"` + strings.ReplaceAll(value, `"`, `\"`) + `"`
		}

		_, _ = fmt.Fprintf(&builder, "%s=%s\n", key, value)
	}

	s.envFileMu.Lock()
	defer s.envFileMu.Unlock()

	envRoot := acfs.Root{Path: projectsDirectory}
	mode := kitfs.FilePerm
	if entry, statErr := envRoot.Stat(ctx, "/"+projects.GlobalEnvFileName, acfstypes.StatOptions{}); statErr == nil {
		mode = os.FileMode(entry.UnixMode).Perm()
	}
	writeErr := envRoot.Write(ctx, "/"+projects.GlobalEnvFileName, []byte(builder.String()), acfs.WriteOptions{Mode: mode})
	if errors.Is(writeErr, syscall.EBUSY) {
		writeErr = envRoot.Write(ctx, "/"+projects.GlobalEnvFileName, []byte(builder.String()), acfs.WriteOptions{
			Mode:    mode,
			InPlace: true,
		})
	}
	if writeErr != nil {
		return fmt.Errorf("failed to write global variables file: %w", writeErr)
	}

	slog.DebugContext(ctx, "Synchronized global variables file",
		"path", envPath,
		"count", len(vars))

	return nil
}

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
		Data    []types.EnvVariable `json:"data"`
		Success bool                `json:"success"`
	}
	if proxyEnvironmentJSONErr := s.environments.ProxyJSONRequest(ctx, envID, http.MethodGet, agentVariablesPath, nil, &out); proxyEnvironmentJSONErr != nil {
		return proxyEnvironmentJSONErr
	}

	if importVariablesErr := s.importVariables(ctx, out.Data, envID); importVariablesErr != nil {
		return importVariablesErr
	}

	return s.kvService.SetBool(ctx, flag, true)
}

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
			Key:                  key,
			Value:                entry.Value,
			AllEnvironments:      scopeEnvID == "",
			InjectIntoContainers: !entry.NoInject,
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
	if err := s.db.WithContext(ctx).Preload("Environments").Order("created_at, key").Find(&variables).Error; err != nil {
		return nil, fmt.Errorf("failed to load global variables: %w", err)
	}
	return variables, nil
}

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
		ID:                   variable.ID,
		Key:                  variable.Key,
		Value:                value,
		IsSecret:             variable.IsSecret,
		AllEnvironments:      variable.AllEnvironments,
		InjectIntoContainers: variable.InjectIntoContainers,
		EnvironmentIDs:       scopedEnvironmentIDs(variable.Environments),
		CreatedAt:            variable.CreatedAt,
		UpdatedAt:            variable.UpdatedAt,
	}
}
