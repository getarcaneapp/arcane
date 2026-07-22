package services

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"emperror.dev/errors"
	"github.com/getarcaneapp/arcane/backend/v2/internal/common"
	"github.com/getarcaneapp/arcane/backend/v2/internal/database"
	"github.com/getarcaneapp/arcane/backend/v2/internal/models"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/remenv"
	envtypes "github.com/getarcaneapp/arcane/types/v2/env"
	sqlite "github.com/libtnb/sqlite"
	"github.com/stretchr/testify/require"
	"go.getarcane.app/sys/crypto"
	"gorm.io/gorm"
)

func setupVariableServiceTest(t *testing.T) (*VariableService, *database.DB, string) {
	t.Helper()

	dsn := fmt.Sprintf("file:%s?mode=memory&cache=shared", strings.NewReplacer("/", "_", " ", "_").Replace(t.Name()))
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(
		&models.GlobalVariable{},
		&models.Environment{},
		&models.KVEntry{},
		&models.SettingVariable{},
	))

	sqlDB, err := db.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(1)
	sqlDB.SetMaxIdleConns(1)

	crypto.InitEncryption(&crypto.Config{
		EncryptionKey: "test-encryption-key-for-testing-32bytes-min",
		Environment:   "test",
	})

	projectsDir := t.TempDir()
	dbWrap := &database.DB{DB: db}
	settingsSvc, err := NewSettingsService(context.Background(), dbWrap)
	require.NoError(t, err)
	require.NoError(t, settingsSvc.UpdateSetting(context.Background(), "projectsDirectory", projectsDir))

	service := NewVariableService(dbWrap, nil, settingsSvc, NewKVService(dbWrap))
	return service, dbWrap, projectsDir
}

func createVariableTestEnvironment(t *testing.T, db *database.DB, id string) {
	t.Helper()
	require.NoError(t, db.Create(&models.Environment{
		BaseModel: models.BaseModel{ID: id},
		Name:      "env-" + id,
		ApiUrl:    "http://env-" + id,
		Status:    "online",
		Enabled:   true,
	}).Error)
}

func TestCreateVariable_SecretEncryptedAtRestAndRedactedOnList(t *testing.T) {
	service, db, _ := setupVariableServiceTest(t)
	ctx := context.Background()

	created, err := service.CreateVariable(ctx, envtypes.CreateGlobalVariableRequest{
		Key:             "API_TOKEN",
		Value:           "super-secret",
		IsSecret:        true,
		AllEnvironments: true,
	})
	require.NoError(t, err)
	require.Empty(t, created.Value, "mutation response must not echo the secret value")

	var stored models.GlobalVariable
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
	service, db, _ := setupVariableServiceTest(t)
	ctx := context.Background()
	created := createMaterializedSecretVariableInternal(t, service, "API_TOKEN", "super-secret")

	var before models.GlobalVariable
	require.NoError(t, db.WithContext(ctx).First(&before, "id = ?", created.ID).Error)
	renamedKey := "RENAMED_API_TOKEN"
	updated, err := service.UpdateVariable(ctx, created.ID, envtypes.UpdateGlobalVariableRequest{Key: &renamedKey})
	require.NoError(t, err)
	require.True(t, updated.IsSecret)
	require.Empty(t, updated.Value)

	var after models.GlobalVariable
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
	service, db, _ := setupVariableServiceTest(t)
	ctx := context.Background()
	created := createMaterializedSecretVariableInternal(t, service, "API_TOKEN", "super-secret")

	plain := false
	_, err := service.UpdateVariable(ctx, created.ID, envtypes.UpdateGlobalVariableRequest{IsSecret: &plain})
	require.Error(t, err)
	require.True(t, errors.Is(err, common.ErrGlobalVariableSecretValueRequired))

	replacement := "public-value"
	updated, err := service.UpdateVariable(ctx, created.ID, envtypes.UpdateGlobalVariableRequest{
		Value:    &replacement,
		IsSecret: &plain,
	})
	require.NoError(t, err)
	require.False(t, updated.IsSecret)
	require.Equal(t, replacement, updated.Value)

	var stored models.GlobalVariable
	require.NoError(t, db.WithContext(ctx).First(&stored, "id = ?", created.ID).Error)
	require.False(t, stored.IsSecret)
	require.Equal(t, replacement, stored.Value)
}

func TestResolveEffectiveVariables_EnvScopedOverridesAllEnv(t *testing.T) {
	service, db, _ := setupVariableServiceTest(t)
	ctx := context.Background()
	createVariableTestEnvironment(t, db, "env-a")
	createVariableTestEnvironment(t, db, "env-b")

	_, err := service.CreateVariable(ctx, envtypes.CreateGlobalVariableRequest{
		Key: "REGION", Value: "default", AllEnvironments: true,
	})
	require.NoError(t, err)
	_, err = service.CreateVariable(ctx, envtypes.CreateGlobalVariableRequest{
		Key: "REGION", Value: "eu-west", EnvironmentIDs: []string{"env-a"},
	})
	require.NoError(t, err)

	// A second variable with the same key and an overlapping scope is rejected.
	_, err = service.CreateVariable(ctx, envtypes.CreateGlobalVariableRequest{
		Key: "REGION", Value: "dup", EnvironmentIDs: []string{"env-a", "env-b"},
	})
	require.Error(t, err)

	// A specific scope without environments must not widen to all environments.
	_, err = service.CreateVariable(ctx, envtypes.CreateGlobalVariableRequest{
		Key: "NO_SCOPE", Value: "x", AllEnvironments: false, EnvironmentIDs: []string{},
	})
	require.True(t, errors.Is(err, common.ErrGlobalVariableScopeRequired), "expected GlobalVariableScopeRequiredError, got %v", err)

	forA, err := service.resolveEffectiveVariablesInternal(ctx, "env-a")
	require.NoError(t, err)
	require.Equal(t, []envtypes.Variable{{Key: "REGION", Value: "eu-west"}}, forA)

	forB, err := service.resolveEffectiveVariablesInternal(ctx, "env-b")
	require.NoError(t, err)
	require.Equal(t, []envtypes.Variable{{Key: "REGION", Value: "default"}}, forB)
}

func TestWriteLocalEnvFile_RejectsNewlineInjectionKey(t *testing.T) {
	service, _, projectsDir := setupVariableServiceTest(t)

	err := service.WriteLocalEnvFile(context.Background(), []envtypes.Variable{
		{Key: "BENIGN\nINJECTED", Value: "x"},
	})
	require.True(t, errors.Is(err, common.ErrInvalidEnvKey), "expected InvalidEnvKeyError, got %v", err)

	_, statErr := os.Stat(filepath.Join(projectsDir, ".env.global"))
	require.True(t, os.IsNotExist(statErr), ".env.global must not be written on validation failure")
}

func TestSyncEnvironment_LocalWritesEnvGlobalFile(t *testing.T) {
	service, _, projectsDir := setupVariableServiceTest(t)
	ctx := context.Background()

	_, err := service.CreateVariable(ctx, envtypes.CreateGlobalVariableRequest{
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

type capturedVariableSyncRequestInternal struct {
	Method  string
	Path    string
	Headers map[string]string
	Body    []byte
}

func TestSyncEnvironment_DirectMaterializesSecretsThroughAgentRoute(t *testing.T) {
	service, db, _ := setupVariableServiceTest(t)
	token := "direct-agent-token"

	var requestMu sync.Mutex
	requests := make([]capturedVariableSyncRequestInternal, 0, 2)
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		body, err := io.ReadAll(request.Body)
		require.NoError(t, err)
		requestMu.Lock()
		requests = append(requests, capturedVariableSyncRequestInternal{
			Method: request.Method,
			Path:   request.URL.Path,
			Headers: map[string]string{
				remenv.HeaderAPIKey:     request.Header.Get(remenv.HeaderAPIKey),
				remenv.HeaderAgentToken: request.Header.Get(remenv.HeaderAgentToken),
			},
			Body: body,
		})
		requestMu.Unlock()

		writer.Header().Set("Content-Type", "application/json")
		if request.Method == http.MethodGet {
			_, _ = writer.Write([]byte(`{"success":true,"data":[]}`))
			return
		}
		_, _ = writer.Write([]byte(`{"success":true,"data":{"message":"updated"}}`))
	}))
	defer server.Close()

	require.NoError(t, db.Create(&models.Environment{
		BaseModel:   models.BaseModel{ID: "env-direct"},
		Name:        "Direct",
		ApiUrl:      server.URL,
		Status:      string(models.EnvironmentStatusOnline),
		Enabled:     true,
		AccessToken: &token,
	}).Error)
	service.environmentService = NewEnvironmentService(db, nil, nil, nil, service.settingsService, nil)
	createMaterializedSecretVariableInternal(t, service, "API_TOKEN", "super-secret")

	require.NoError(t, service.SyncEnvironment(context.Background(), "env-direct"))
	requestMu.Lock()
	captured := append([]capturedVariableSyncRequestInternal(nil), requests...)
	requestMu.Unlock()
	requireMaterializationRequestsInternal(t, captured, token)
}

func TestSyncEnvironment_EdgeMaterializesSecretsThroughAgentRoute(t *testing.T) {
	service, db, _ := setupVariableServiceTest(t)
	token := "edge-agent-token"
	require.NoError(t, db.Create(&models.Environment{
		BaseModel:   models.BaseModel{ID: "env-edge"},
		Name:        "Edge",
		ApiUrl:      "http://edge.invalid",
		Status:      string(models.EnvironmentStatusOnline),
		Enabled:     true,
		IsEdge:      true,
		AccessToken: &token,
	}).Error)

	environmentService := NewEnvironmentService(db, nil, nil, nil, service.settingsService, nil)
	var requestMu sync.Mutex
	requests := make([]capturedVariableSyncRequestInternal, 0, 2)
	environmentService.remoteClient = remenv.NewClient(nil, remenv.TunnelTransportFuncs{
		EnsureAvailableFunc: func(_ context.Context, environmentID string) error {
			require.Equal(t, "env-edge", environmentID)
			return nil
		},
		DoFunc: func(_ context.Context, environmentID, method, path string, headers map[string]string, body []byte) (*remenv.Response, error) {
			require.Equal(t, "env-edge", environmentID)
			requestMu.Lock()
			requests = append(requests, capturedVariableSyncRequestInternal{
				Method:  method,
				Path:    path,
				Headers: headers,
				Body:    append([]byte(nil), body...),
			})
			requestMu.Unlock()
			if method == http.MethodGet {
				return &remenv.Response{StatusCode: http.StatusOK, Body: []byte(`{"success":true,"data":[]}`)}, nil
			}
			return &remenv.Response{StatusCode: http.StatusOK, Body: []byte(`{"success":true,"data":{"message":"updated"}}`)}, nil
		},
	})
	service.environmentService = environmentService
	createMaterializedSecretVariableInternal(t, service, "API_TOKEN", "super-secret")

	require.NoError(t, service.SyncEnvironment(context.Background(), "env-edge"))
	requestMu.Lock()
	captured := append([]capturedVariableSyncRequestInternal(nil), requests...)
	requestMu.Unlock()
	requireMaterializationRequestsInternal(t, captured, token)
}

func createMaterializedSecretVariableInternal(t *testing.T, service *VariableService, key, value string) *envtypes.GlobalVariable {
	t.Helper()
	created, err := service.CreateVariable(context.Background(), envtypes.CreateGlobalVariableRequest{
		Key:             key,
		Value:           value,
		IsSecret:        true,
		AllEnvironments: true,
	})
	require.NoError(t, err)
	return created
}

func requireMaterializationRequestsInternal(t *testing.T, requests []capturedVariableSyncRequestInternal, token string) {
	t.Helper()
	require.Len(t, requests, 2)

	require.Equal(t, http.MethodGet, requests[0].Method)
	require.Equal(t, agentVariablesPath, requests[0].Path)
	require.Empty(t, requests[0].Body)
	require.Equal(t, token, requests[0].Headers[remenv.HeaderAPIKey])
	require.Equal(t, token, requests[0].Headers[remenv.HeaderAgentToken])

	require.Equal(t, http.MethodPut, requests[1].Method)
	require.Equal(t, agentVariablesPath, requests[1].Path)
	require.Equal(t, token, requests[1].Headers[remenv.HeaderAPIKey])
	require.Equal(t, token, requests[1].Headers[remenv.HeaderAgentToken])
	var materialized envtypes.Summary
	require.NoError(t, json.Unmarshal(requests[1].Body, &materialized))
	require.Equal(t, []envtypes.Variable{{Key: "API_TOKEN", Value: "super-secret"}}, materialized.Variables)
}
