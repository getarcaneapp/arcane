package variable

import (
	"context"
	"encoding/json/v2"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"

	"github.com/libtnb/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"kit.getarcane.app/fs"
	"kit.getarcane.app/sys/crypto"

	"getarcane.dev/arcane/v3/internal/common"
	"getarcane.dev/arcane/v3/internal/config"
	"getarcane.dev/arcane/v3/internal/database"
	"getarcane.dev/arcane/v3/internal/environment"
	"getarcane.dev/arcane/v3/internal/kv"
	"getarcane.dev/arcane/v3/internal/settings"
	"getarcane.dev/arcane/v3/pkg/agent"
	"getarcane.dev/arcane/v3/pkg/projects"
	"getarcane.dev/types/v3"
)

type (
	variableTestRemoteProxy struct {
		proxyJSON func(ctx context.Context, envID, method, path string, body []byte, out any) error
	}

	capturedVariableSyncRequest struct {
		Method  string
		Path    string
		Headers map[string]string
		Body    []byte
	}
)

func newSettingsServiceForVariableTest(t testing.TB, ctx context.Context, db *database.DB) (*settings.SettingsService, error) {
	t.Helper()
	svc, err := settings.NewSettingsService(settings.SettingsServiceParams{
		Ctx:    ctx,
		DB:     db,
		Config: &config.Config{},
		Env:    settings.Env{},
	})
	if err == nil {
		t.Cleanup(func() { require.NoError(t, svc.Stop(context.WithoutCancel(t.Context()))) })
	}
	return svc, err
}

func setupVariableServiceTest(t *testing.T, environments environment.RemoteProxy) (*VariableService, *database.DB, string) {
	t.Helper()

	dsn := fmt.Sprintf("file:%s?mode=memory&cache=shared", strings.NewReplacer("/", "_", " ", "_").Replace(t.Name()))
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(
		&GlobalVariable{},
		&environment.Environment{},
		&kv.KVEntry{},
		&settings.SettingVariable{},
	))

	sqlDB, err := db.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(1)
	sqlDB.SetMaxIdleConns(1)

	crypto.InitEncryption(crypto.Config{
		EncryptionKey: "test-encryption-key-for-testing-32bytes-min",
		Environment:   "test",
	})

	projectsDir := t.TempDir()
	dbWrap := &database.DB{DB: db}
	settingsSvc, err := newSettingsServiceForVariableTest(t, t.Context(), dbWrap)
	require.NoError(t, err)
	require.NoError(t, settingsSvc.UpdateSetting(t.Context(), "projectsDirectory", projectsDir))

	service := NewVariableService(VariableServiceParams{
		DB:              dbWrap,
		Environments:    environments,
		Tunnels:         agent.NewTunnelRegistry(),
		SettingsService: settingsSvc,
		KVService:       kv.NewKVService(dbWrap),
	})
	return service, dbWrap, projectsDir
}

func createVariableTestEnvironment(t *testing.T, db *database.DB, id string) {
	t.Helper()
	require.NoError(t, db.Create(&environment.Environment{
		ID:      id,
		Name:    "env-" + id,
		ApiUrl:  "http://env-" + id,
		Status:  "online",
		Enabled: true,
	}).Error)
}

func TestCreateVariable_SecretEncryptedAtRestAndRedactedOnList(t *testing.T) {
	service, db, _ := setupVariableServiceTest(t, &variableTestRemoteProxy{})
	ctx := t.Context()

	created, err := service.CreateVariable(ctx, types.GlobalVariable{
		Key:             "API_TOKEN",
		Value:           "super-secret",
		IsSecret:        true,
		AllEnvironments: true,
	})
	require.NoError(t, err)
	require.Empty(t, created.Value, "mutation response must not echo the secret value")

	var stored GlobalVariable
	require.NoError(t, db.First(&stored, "id = ?", created.ID).Error)
	require.NotEqual(t, "super-secret", stored.Value, "secret must not be stored as plaintext")
	decrypted, err := crypto.Decrypt(stored.Value)
	require.NoError(t, err)
	require.Equal(t, "super-secret", decrypted)

	listed, err := service.ListVariables(ctx)
	require.NoError(t, err)
	require.Len(t, listed, 1)
	require.True(t, listed[0].IsSecret)
	require.Empty(t, listed[0].Value)
}

func TestUpdateVariable_OmittedSecretValuePreservesCiphertextAndRedactsResponse(t *testing.T) {
	service, db, _ := setupVariableServiceTest(t, &variableTestRemoteProxy{})
	ctx := t.Context()
	created := createMaterializedSecretVariable(t, service)

	var before GlobalVariable
	require.NoError(t, db.WithContext(ctx).First(&before, "id = ?", created.ID).Error)
	updated, err := service.UpdateVariable(ctx, created.ID, types.GlobalVariable{Key: "RENAMED_API_TOKEN"}, map[string]bool{"key": true})
	require.NoError(t, err)
	require.True(t, updated.IsSecret)
	require.Empty(t, updated.Value)

	var after GlobalVariable
	require.NoError(t, db.WithContext(ctx).First(&after, "id = ?", created.ID).Error)
	require.Equal(t, before.Value, after.Value, "omitting value must preserve the stored ciphertext")
	decrypted, err := crypto.Decrypt(after.Value)
	require.NoError(t, err)
	require.Equal(t, "super-secret", decrypted)

	listed, err := service.ListVariables(ctx)
	require.NoError(t, err)
	require.Len(t, listed, 1)
	require.Empty(t, listed[0].Value)
}

func TestUpdateVariable_SecretToPlainRequiresReplacementValue(t *testing.T) {
	service, db, _ := setupVariableServiceTest(t, &variableTestRemoteProxy{})
	ctx := t.Context()
	created := createMaterializedSecretVariable(t, service)

	_, err := service.UpdateVariable(ctx, created.ID, types.GlobalVariable{IsSecret: false}, map[string]bool{"isSecret": true})
	require.Error(t, err)
	require.ErrorIs(t, err, common.ErrGlobalVariableSecretValueRequired)

	replacement := "public-value"
	updated, err := service.UpdateVariable(ctx, created.ID, types.GlobalVariable{Value: replacement}, map[string]bool{"value": true, "isSecret": true})
	require.NoError(t, err)
	require.False(t, updated.IsSecret)
	require.Equal(t, replacement, updated.Value)

	var stored GlobalVariable
	require.NoError(t, db.WithContext(ctx).First(&stored, "id = ?", created.ID).Error)
	require.False(t, stored.IsSecret)
	require.Equal(t, replacement, stored.Value)
}

func TestResolveEffectiveVariables_EnvScopedOverridesAllEnv(t *testing.T) {
	service, db, _ := setupVariableServiceTest(t, &variableTestRemoteProxy{})
	ctx := t.Context()
	createVariableTestEnvironment(t, db, "env-a")
	createVariableTestEnvironment(t, db, "env-b")

	_, err := service.CreateVariable(ctx, types.GlobalVariable{
		Key: "REGION", Value: "default", AllEnvironments: true,
	})
	require.NoError(t, err)
	_, err = service.CreateVariable(ctx, types.GlobalVariable{
		Key: "REGION", Value: "eu-west", EnvironmentIDs: []string{"env-a"},
	})
	require.NoError(t, err)

	// A second variable with the same key and an overlapping scope is rejected.
	_, err = service.CreateVariable(ctx, types.GlobalVariable{
		Key: "REGION", Value: "dup", EnvironmentIDs: []string{"env-a", "env-b"},
	})
	require.Error(t, err)

	// A specific scope without environments must not widen to all environments.
	_, err = service.CreateVariable(ctx, types.GlobalVariable{
		Key: "NO_SCOPE", Value: "x", AllEnvironments: false, EnvironmentIDs: []string{},
	})
	require.ErrorIs(t, err, common.ErrGlobalVariableScopeRequired, "expected GlobalVariableScopeRequiredError, got %v", err)

	forA, err := service.resolveEffectiveVariables(ctx, "env-a")
	require.NoError(t, err)
	require.Equal(t, []types.EnvVariable{{Key: "REGION", Value: "eu-west", NoInject: true}}, forA)

	forB, err := service.resolveEffectiveVariables(ctx, "env-b")
	require.NoError(t, err)
	require.Equal(t, []types.EnvVariable{{Key: "REGION", Value: "default", NoInject: true}}, forB)
}

func TestResolveEffectiveVariables_MarksInterpolationOnlyVariables(t *testing.T) {
	service, db, _ := setupVariableServiceTest(t, &variableTestRemoteProxy{})
	ctx := t.Context()
	createVariableTestEnvironment(t, db, "env-a")
	createVariableTestEnvironment(t, db, "env-b")

	timezone, err := service.CreateVariable(ctx, types.GlobalVariable{
		Key: "TZ", Value: "UTC", AllEnvironments: true, InjectIntoContainers: true,
	})
	require.NoError(t, err)
	require.True(t, timezone.InjectIntoContainers)

	password, err := service.CreateVariable(ctx, types.GlobalVariable{
		Key: "DB_PASSWORD", Value: "hunter2", IsSecret: true, AllEnvironments: true,
	})
	require.NoError(t, err)
	require.False(t, password.InjectIntoContainers, "new variables are interpolation-only unless injection is requested")

	_, err = service.CreateVariable(ctx, types.GlobalVariable{
		Key: "TZ", Value: "Europe/Paris", EnvironmentIDs: []string{"env-a"},
	})
	require.NoError(t, err)

	forA, err := service.resolveEffectiveVariables(ctx, "env-a")
	require.NoError(t, err)
	require.Equal(t, []types.EnvVariable{
		{Key: "TZ", Value: "Europe/Paris", NoInject: true},
		{Key: "DB_PASSWORD", Value: "hunter2", NoInject: true},
	}, forA)

	forB, err := service.resolveEffectiveVariables(ctx, "env-b")
	require.NoError(t, err)
	require.Equal(t, []types.EnvVariable{
		{Key: "TZ", Value: "UTC"},
		{Key: "DB_PASSWORD", Value: "hunter2", NoInject: true},
	}, forB)
}

func TestUpdateVariable_InjectIntoContainersKeptUnlessSubmitted(t *testing.T) {
	service, db, _ := setupVariableServiceTest(t, &variableTestRemoteProxy{})
	ctx := t.Context()
	created, err := service.CreateVariable(ctx, types.GlobalVariable{
		Key: "PUID", Value: "1000", AllEnvironments: true,
	})
	require.NoError(t, err)

	updated, err := service.UpdateVariable(ctx, created.ID, types.GlobalVariable{InjectIntoContainers: true}, map[string]bool{"injectIntoContainers": true})
	require.NoError(t, err)
	require.True(t, updated.InjectIntoContainers)

	updated, err = service.UpdateVariable(ctx, created.ID, types.GlobalVariable{Value: "1001"}, map[string]bool{"value": true})
	require.NoError(t, err)
	require.True(t, updated.InjectIntoContainers, "omitting the flag must keep the stored value")

	var stored GlobalVariable
	require.NoError(t, db.WithContext(ctx).First(&stored, "id = ?", created.ID).Error)
	require.True(t, stored.InjectIntoContainers)
}

func TestImportVariables_InjectsOnlyUnmarkedEntries(t *testing.T) {
	service, db, _ := setupVariableServiceTest(t, &variableTestRemoteProxy{})
	ctx := t.Context()
	createVariableTestEnvironment(t, db, "env-a")

	require.NoError(t, service.importVariables(ctx, []types.EnvVariable{
		{Key: "PUID", Value: "1000"},
		{Key: "DB_PASSWORD", Value: "hunter2", NoInject: true},
	}, "env-a"))

	listed, err := service.ListVariables(ctx)
	require.NoError(t, err)
	injected := map[string]bool{}
	for _, variable := range listed {
		injected[variable.Key] = variable.InjectIntoContainers
	}
	require.Equal(t, map[string]bool{"PUID": true, "DB_PASSWORD": false}, injected, "unmarked legacy entries were always injected; marked ones stay interpolation-only")
}

func TestReadLocalEnvFile_ReportsNoInjectMarks(t *testing.T) {
	service, _, _ := setupVariableServiceTest(t, &variableTestRemoteProxy{})
	ctx := t.Context()
	vars := []types.EnvVariable{
		{Key: "DB_PASSWORD", Value: "hunter2", NoInject: true},
		{Key: "TZ", Value: "UTC"},
	}

	require.NoError(t, service.WriteLocalEnvFile(ctx, vars))

	read, err := service.ReadLocalEnvFile(ctx)
	require.NoError(t, err)
	require.Equal(t, vars, read, "a re-added agent must report which keys are interpolation-only")
}

func TestWriteLocalEnvFile_RejectsNewlineInjectionKey(t *testing.T) {
	service, _, projectsDir := setupVariableServiceTest(t, &variableTestRemoteProxy{})

	err := service.WriteLocalEnvFile(t.Context(), []types.EnvVariable{
		{Key: "BENIGN\nINJECTED", Value: "x"},
	})
	require.ErrorIs(t, err, common.ErrInvalidEnvKey, "expected InvalidEnvKeyError, got %v", err)

	_, statErr := os.Stat(filepath.Join(projectsDir, ".env.global"))
	require.True(t, os.IsNotExist(statErr), ".env.global must not be written on validation failure")
}

func TestWriteLocalEnvFile_PreservesExistingFileMode(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("file mode bits are not represented on Windows FS")
	}
	service, _, projectsDir := setupVariableServiceTest(t, &variableTestRemoteProxy{})
	ctx := t.Context()
	envPath := filepath.Join(projectsDir, ".env.global")

	require.NoError(t, service.WriteLocalEnvFile(ctx, []types.EnvVariable{{Key: "A", Value: "1"}}))
	info, err := os.Stat(envPath)
	require.NoError(t, err)
	require.Equal(t, fs.FilePerm, info.Mode().Perm(), "new file must use the configured default mode")

	require.NoError(t, os.Chmod(envPath, 0o600))
	require.NoError(t, service.WriteLocalEnvFile(ctx, []types.EnvVariable{{Key: "A", Value: "2"}}))
	info, err = os.Stat(envPath)
	require.NoError(t, err)
	require.Equal(t, os.FileMode(0o600), info.Mode().Perm(), "rewrite must keep the existing mode")
	content, err := os.ReadFile(envPath)
	require.NoError(t, err)
	require.Contains(t, string(content), "A=2")
}

func TestSyncEnvironment_LocalWritesEnvGlobalFile(t *testing.T) {
	service, _, projectsDir := setupVariableServiceTest(t, &variableTestRemoteProxy{})
	ctx := t.Context()

	_, err := service.CreateVariable(ctx, types.GlobalVariable{
		Key: "DB_PASSWORD", Value: "hunter2", IsSecret: true, AllEnvironments: true,
	})
	require.NoError(t, err)

	require.NoError(t, service.SyncEnvironment(ctx, "0"))

	content, err := os.ReadFile(filepath.Join(projectsDir, ".env.global"))
	require.NoError(t, err)
	require.Contains(t, string(content), "DB_PASSWORD=hunter2", "materialized file must contain the decrypted secret")

	statuses := service.SyncStatuses()
	require.Len(t, statuses, 1)
	require.Equal(t, "synced", statuses[0].Status)
}

func TestSyncEnvironment_LocalSkipsInjectionForInterpolationOnlyVariables(t *testing.T) {
	service, _, projectsDir := setupVariableServiceTest(t, &variableTestRemoteProxy{})
	ctx := t.Context()

	_, err := service.CreateVariable(ctx, types.GlobalVariable{
		Key: "TZ", Value: "UTC", AllEnvironments: true, InjectIntoContainers: true,
	})
	require.NoError(t, err)
	_, err = service.CreateVariable(ctx, types.GlobalVariable{
		Key: "DB_PASSWORD", Value: "hunter2", IsSecret: true, AllEnvironments: true,
	})
	require.NoError(t, err)

	require.NoError(t, service.SyncEnvironment(ctx, "0"))

	workdir := filepath.Join(projectsDir, "app")
	require.NoError(t, os.MkdirAll(workdir, fs.DirPerm))
	envMap, injectionVars, err := projects.NewEnvLoader(projectsDir, workdir, false).LoadEnvironment(ctx)
	require.NoError(t, err)
	require.Equal(t, "hunter2", envMap["DB_PASSWORD"], "interpolation-only variables stay available for ${VAR}")
	require.Equal(t, projects.EnvMap{"TZ": "UTC"}, injectionVars)
}

func TestSyncEnvironment_AgentPayloadOmitsNoInjectWhenAllInjected(t *testing.T) {
	var putBodies [][]byte
	service, db, _ := setupVariableServiceTest(t, &variableTestRemoteProxy{
		proxyJSON: func(_ context.Context, _, method, _ string, body []byte, response any) error {
			if method == http.MethodGet {
				return json.Unmarshal([]byte(`{"success":true,"data":[]}`), response)
			}
			putBodies = append(putBodies, append([]byte(nil), body...))
			return json.Unmarshal([]byte(`{"success":true,"data":{"message":"updated"}}`), response)
		},
	})
	ctx := t.Context()
	token := "agent-token"
	require.NoError(t, db.Create(&environment.Environment{
		ID:          "env-agent",
		Name:        "Agent",
		ApiUrl:      "agent://env-agent",
		Status:      string(environment.EnvironmentStatusOnline),
		Enabled:     true,
		AccessToken: &token,
	}).Error)

	_, err := service.CreateVariable(ctx, types.GlobalVariable{
		Key: "TZ", Value: "UTC", AllEnvironments: true, InjectIntoContainers: true,
	})
	require.NoError(t, err)
	require.NoError(t, service.SyncEnvironment(ctx, "env-agent"))

	_, err = service.CreateVariable(ctx, types.GlobalVariable{
		Key: "DB_PASSWORD", Value: "hunter2", AllEnvironments: true,
	})
	require.NoError(t, err)
	require.NoError(t, service.SyncEnvironment(ctx, "env-agent"))

	require.Len(t, putBodies, 2)
	require.NotContains(t, string(putBodies[0]), "noInject")
	var materialized types.EnvSummary
	require.NoError(t, json.Unmarshal(putBodies[1], &materialized))
	require.Equal(t, []types.EnvVariable{
		{Key: "TZ", Value: "UTC"},
		{Key: "DB_PASSWORD", Value: "hunter2", NoInject: true},
	}, materialized.Variables)
}

func TestSyncEnvironment_AgentMaterializesSecretsThroughAgentRoute(t *testing.T) {
	token := "agent-token"
	var requestMu sync.Mutex
	requests := make([]capturedVariableSyncRequest, 0, 2)
	service, db, _ := setupVariableServiceTest(t, &variableTestRemoteProxy{
		proxyJSON: func(_ context.Context, environmentID, method, path string, body []byte, response any) error {
			require.Equal(t, "env-agent", environmentID)
			requestMu.Lock()
			requests = append(requests, capturedVariableSyncRequest{
				Method: method,
				Path:   path,
				Headers: map[string]string{
					agent.HeaderAPIKey:     token,
					agent.HeaderAgentToken: token,
				},
				Body: append([]byte(nil), body...),
			})
			requestMu.Unlock()
			if method == http.MethodGet {
				return json.Unmarshal([]byte(`{"success":true,"data":[]}`), response)
			}
			return json.Unmarshal([]byte(`{"success":true,"data":{"message":"updated"}}`), response)
		},
	})
	require.NoError(t, db.Create(&environment.Environment{
		ID:          "env-agent",
		Name:        "Agent",
		ApiUrl:      "agent://env-agent",
		Status:      string(environment.EnvironmentStatusOnline),
		Enabled:     true,
		AccessToken: &token,
	}).Error)
	createMaterializedSecretVariable(t, service)

	require.NoError(t, service.SyncEnvironment(t.Context(), "env-agent"))
	requestMu.Lock()
	captured := append([]capturedVariableSyncRequest(nil), requests...)
	requestMu.Unlock()
	requireMaterializationRequests(t, captured, token)
}

func createMaterializedSecretVariable(t *testing.T, service *VariableService) *types.GlobalVariable {
	t.Helper()
	created, err := service.CreateVariable(t.Context(), types.GlobalVariable{
		Key:             "API_TOKEN",
		Value:           "super-secret",
		IsSecret:        true,
		AllEnvironments: true,
	})
	require.NoError(t, err)
	return created
}

func requireMaterializationRequests(t *testing.T, requests []capturedVariableSyncRequest, token string) {
	t.Helper()
	require.Len(t, requests, 2)

	require.Equal(t, http.MethodGet, requests[0].Method)
	require.Equal(t, agentVariablesPath, requests[0].Path)
	require.Empty(t, requests[0].Body)
	require.Equal(t, token, requests[0].Headers[agent.HeaderAPIKey])
	require.Equal(t, token, requests[0].Headers[agent.HeaderAgentToken])

	require.Equal(t, http.MethodPut, requests[1].Method)
	require.Equal(t, agentVariablesPath, requests[1].Path)
	require.Equal(t, token, requests[1].Headers[agent.HeaderAPIKey])
	require.Equal(t, token, requests[1].Headers[agent.HeaderAgentToken])
	var materialized types.EnvSummary
	require.NoError(t, json.Unmarshal(requests[1].Body, &materialized))
	require.Equal(t, []types.EnvVariable{{Key: "API_TOKEN", Value: "super-secret", NoInject: true}}, materialized.Variables)
}

func (p *variableTestRemoteProxy) ProxyJSONRequest(ctx context.Context, envID, method, path string, body []byte, out any) error {
	return p.proxyJSON(ctx, envID, method, path, body, out)
}

func (*variableTestRemoteProxy) ListEnabledRemoteEnvironmentIDs(context.Context) ([]string, error) {
	return nil, nil
}
