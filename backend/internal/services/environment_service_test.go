package services

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	glsqlite "github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"github.com/getarcaneapp/arcane/backend/internal/config"
	"github.com/getarcaneapp/arcane/backend/internal/database"
	"github.com/getarcaneapp/arcane/backend/internal/models"
	"github.com/getarcaneapp/arcane/backend/pkg/libarcane/crypto"
	"github.com/getarcaneapp/arcane/types/containerregistry"
)

func setupEnvironmentServiceTestDB(t *testing.T) *database.DB {
	t.Helper()

	db, err := gorm.Open(glsqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&models.Environment{}, &models.ContainerRegistry{}, &models.SettingVariable{}))

	testCfg := &config.Config{
		EncryptionKey: "test-encryption-key-for-testing-32bytes-min",
		Environment:   "test",
	}
	crypto.InitEncryption(&crypto.Config{
		EncryptionKey: testCfg.EncryptionKey,
		Environment:   string(testCfg.Environment),
		AgentMode:     testCfg.AgentMode,
	})

	return &database.DB{DB: db}
}

func createTestEnvironment(t *testing.T, db *database.DB, id string, apiURL string, accessToken *string) {
	t.Helper()
	createNamedTestEnvironmentInternal(t, db, id, "env-"+id, apiURL, accessToken)
}

func createNamedTestEnvironmentInternal(t *testing.T, db *database.DB, id, name, apiURL string, accessToken *string) {
	t.Helper()

	now := time.Now()
	env := &models.Environment{
		BaseModel: models.BaseModel{
			ID:        id,
			CreatedAt: now,
			UpdatedAt: &now,
		},
		Name:        name,
		ApiUrl:      apiURL,
		Status:      string(models.EnvironmentStatusOnline),
		Enabled:     true,
		AccessToken: accessToken,
	}

	require.NoError(t, db.WithContext(context.Background()).Create(env).Error)
}

func createTestEnvironmentWithState(t *testing.T, db *database.DB, id, apiURL, status string, isEdge bool, accessToken *string) {
	t.Helper()

	now := time.Now()
	env := &models.Environment{
		BaseModel: models.BaseModel{
			ID:        id,
			CreatedAt: now,
			UpdatedAt: &now,
		},
		Name:        "env-" + id,
		ApiUrl:      apiURL,
		Status:      status,
		Enabled:     true,
		IsEdge:      isEdge,
		AccessToken: accessToken,
	}

	require.NoError(t, db.WithContext(context.Background()).Create(env).Error)
}

func createTestRegistry(t *testing.T, db *database.DB, id string) {
	t.Helper()

	encryptedToken, err := crypto.Encrypt("registry-token")
	require.NoError(t, err)

	now := time.Now()
	registry := &models.ContainerRegistry{
		BaseModel: models.BaseModel{
			ID:        id,
			CreatedAt: now,
			UpdatedAt: &now,
		},
		URL:          "registry.example.com",
		Username:     "registry-user",
		Token:        encryptedToken,
		Enabled:      true,
		Insecure:     false,
		RegistryType: registryTypeGeneric,
		CreatedAt:    now,
		UpdatedAt:    now,
	}

	require.NoError(t, db.WithContext(context.Background()).Create(registry).Error)
}

func createTestECRRegistry(t *testing.T, db *database.DB, id string) {
	t.Helper()

	encryptedSecret, err := crypto.Encrypt("aws-secret")
	require.NoError(t, err)

	now := time.Now()
	registry := &models.ContainerRegistry{
		BaseModel: models.BaseModel{
			ID:        id,
			CreatedAt: now,
			UpdatedAt: &now,
		},
		URL:                "123456789012.dkr.ecr.us-east-1.amazonaws.com",
		Enabled:            true,
		RegistryType:       registryTypeECR,
		AWSAccessKeyID:     "AKIA1234567890EXAMPLE",
		AWSSecretAccessKey: encryptedSecret,
		AWSRegion:          "us-east-1",
		CreatedAt:          now,
		UpdatedAt:          now,
	}

	require.NoError(t, db.WithContext(context.Background()).Create(registry).Error)
}

func TestEnvironmentService_SyncRegistriesToRemoteEnvironments_SyncsEligibleRemotes(t *testing.T) {
	ctx := context.Background()
	db := setupEnvironmentServiceTestDB(t)
	svc := NewEnvironmentService(db, nil, nil, nil, nil)

	createTestRegistry(t, db, "reg-1")

	var env1Calls atomic.Int32
	env1Token := "token-1"
	env1Server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, http.MethodPost, r.Method)
		require.Equal(t, "/api/container-registries/sync", r.URL.Path)
		require.Equal(t, env1Token, r.Header.Get("X-API-Key"))
		require.Equal(t, env1Token, r.Header.Get("X-Arcane-Agent-Token"))

		var syncReq containerregistry.SyncRequest
		require.NoError(t, json.NewDecoder(r.Body).Decode(&syncReq))
		require.Len(t, syncReq.Registries, 1)
		env1Calls.Add(1)

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"success":true,"data":{"message":"ok"}}`))
	}))
	defer env1Server.Close()

	var env2Calls atomic.Int32
	env2Token := "token-2"
	env2Server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, http.MethodPost, r.Method)
		require.Equal(t, "/api/container-registries/sync", r.URL.Path)
		require.Equal(t, env2Token, r.Header.Get("X-API-Key"))
		require.Equal(t, env2Token, r.Header.Get("X-Arcane-Agent-Token"))

		var syncReq containerregistry.SyncRequest
		require.NoError(t, json.NewDecoder(r.Body).Decode(&syncReq))
		require.Len(t, syncReq.Registries, 1)
		env2Calls.Add(1)

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"success":true,"data":{"message":"ok"}}`))
	}))
	defer env2Server.Close()

	createTestEnvironment(t, db, "0", "http://localhost:3552", nil) // local env should be excluded
	createTestEnvironment(t, db, "env-1", env1Server.URL, &env1Token)
	createTestEnvironment(t, db, "env-2", env2Server.URL, &env2Token)

	err := svc.SyncRegistriesToRemoteEnvironments(ctx)
	require.NoError(t, err)
	require.EqualValues(t, 1, env1Calls.Load())
	require.EqualValues(t, 1, env2Calls.Load())
}

func TestEnvironmentService_SyncRegistriesToEnvironment_IncludesECRFields(t *testing.T) {
	ctx := context.Background()
	db := setupEnvironmentServiceTestDB(t)
	svc := NewEnvironmentService(db, nil, nil, nil, nil)

	createTestECRRegistry(t, db, "reg-ecr")

	token := "token-1"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, http.MethodPost, r.Method)
		require.Equal(t, "/api/container-registries/sync", r.URL.Path)

		var syncReq containerregistry.SyncRequest
		require.NoError(t, json.NewDecoder(r.Body).Decode(&syncReq))
		require.Len(t, syncReq.Registries, 1)

		registry := syncReq.Registries[0]
		require.Equal(t, registryTypeECR, registry.RegistryType)
		require.Equal(t, "123456789012.dkr.ecr.us-east-1.amazonaws.com", registry.URL)
		require.Equal(t, "AKIA1234567890EXAMPLE", registry.AWSAccessKeyID)
		require.Equal(t, "aws-secret", registry.AWSSecretAccessKey)
		require.Equal(t, "us-east-1", registry.AWSRegion)
		require.Empty(t, registry.Username)
		require.Empty(t, registry.Token)

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"success":true,"data":{"message":"ok"}}`))
	}))
	defer server.Close()

	createTestEnvironment(t, db, "env-1", server.URL, &token)

	err := svc.SyncRegistriesToEnvironment(ctx, "env-1")
	require.NoError(t, err)
}

func TestEnvironmentService_SyncRegistriesToRemoteEnvironments_SkipsRemoteWithoutAccessToken(t *testing.T) {
	ctx := context.Background()
	db := setupEnvironmentServiceTestDB(t)
	svc := NewEnvironmentService(db, nil, nil, nil, nil)

	createTestRegistry(t, db, "reg-1")

	var syncCalls atomic.Int32
	token := "token-with-auth"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		syncCalls.Add(1)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"success":true,"data":{"message":"ok"}}`))
	}))
	defer server.Close()

	createTestEnvironment(t, db, "env-auth", server.URL, &token)
	createTestEnvironment(t, db, "env-no-token", "http://127.0.0.1:1", nil)

	err := svc.SyncRegistriesToRemoteEnvironments(ctx)
	require.NoError(t, err)
	require.EqualValues(t, 1, syncCalls.Load())
}

func TestEnvironmentService_SyncRegistriesToRemoteEnvironments_ReportsFailuresButContinues(t *testing.T) {
	ctx := context.Background()
	db := setupEnvironmentServiceTestDB(t)
	svc := NewEnvironmentService(db, nil, nil, nil, nil)

	createTestRegistry(t, db, "reg-1")

	var successCalls atomic.Int32
	successToken := "token-success"
	successServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		successCalls.Add(1)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"success":true,"data":{"message":"ok"}}`))
	}))
	defer successServer.Close()

	failingToken := "token-fail"
	createTestEnvironment(t, db, "env-success", successServer.URL, &successToken)
	createTestEnvironment(t, db, "env-fail", "http://127.0.0.1:1", &failingToken)

	err := svc.SyncRegistriesToRemoteEnvironments(ctx)
	require.Error(t, err)
	require.Contains(t, err.Error(), "failed to sync registries to 1 remote environment")
	require.EqualValues(t, 1, successCalls.Load())
}

func TestEnvironmentService_ReconcileEdgeStatusesOnStartup(t *testing.T) {
	ctx := context.Background()
	db := setupEnvironmentServiceTestDB(t)
	svc := NewEnvironmentService(db, nil, nil, nil, nil)

	createTestEnvironmentWithState(t, db, "edge-online", "edge://online", string(models.EnvironmentStatusOnline), true, nil)
	createTestEnvironmentWithState(t, db, "edge-error", "edge://error", string(models.EnvironmentStatusError), true, nil)
	createTestEnvironmentWithState(t, db, "edge-pending", "edge://pending", string(models.EnvironmentStatusPending), true, nil)
	createTestEnvironmentWithState(t, db, "remote-http", "http://remote.example", string(models.EnvironmentStatusOnline), false, nil)

	err := svc.ReconcileEdgeStatusesOnStartup(ctx)
	require.NoError(t, err)

	var edgeOnline models.Environment
	require.NoError(t, db.WithContext(ctx).Where("id = ?", "edge-online").First(&edgeOnline).Error)
	require.Equal(t, string(models.EnvironmentStatusOffline), edgeOnline.Status)

	var edgeError models.Environment
	require.NoError(t, db.WithContext(ctx).Where("id = ?", "edge-error").First(&edgeError).Error)
	require.Equal(t, string(models.EnvironmentStatusOffline), edgeError.Status)

	var edgePending models.Environment
	require.NoError(t, db.WithContext(ctx).Where("id = ?", "edge-pending").First(&edgePending).Error)
	require.Equal(t, string(models.EnvironmentStatusPending), edgePending.Status)

	var remoteHTTP models.Environment
	require.NoError(t, db.WithContext(ctx).Where("id = ?", "remote-http").First(&remoteHTTP).Error)
	require.Equal(t, string(models.EnvironmentStatusOnline), remoteHTTP.Status)
}

func TestEnvironmentService_UpdateEnvironmentConnectionState(t *testing.T) {
	ctx := context.Background()
	db := setupEnvironmentServiceTestDB(t)
	svc := NewEnvironmentService(db, nil, nil, nil, nil)

	createTestEnvironmentWithState(t, db, "edge-runtime", "edge://runtime", string(models.EnvironmentStatusOffline), true, nil)

	err := svc.UpdateEnvironmentConnectionState(ctx, "edge-runtime", true)
	require.NoError(t, err)

	var env models.Environment
	require.NoError(t, db.WithContext(ctx).Where("id = ?", "edge-runtime").First(&env).Error)
	require.Equal(t, string(models.EnvironmentStatusOnline), env.Status)
	require.NotNil(t, env.LastSeen)

	lastSeen := env.LastSeen

	err = svc.UpdateEnvironmentConnectionState(ctx, "edge-runtime", false)
	require.NoError(t, err)

	require.NoError(t, db.WithContext(ctx).Where("id = ?", "edge-runtime").First(&env).Error)
	require.Equal(t, string(models.EnvironmentStatusOffline), env.Status)
	require.NotNil(t, env.LastSeen)
	require.Equal(t, *lastSeen, *env.LastSeen)
}

func TestEnvironmentService_ResolveEdgeEnvironmentByToken_CachesAndInvalidatesOnUpdate(t *testing.T) {
	ctx := context.Background()
	db := setupEnvironmentServiceTestDB(t)
	svc := NewEnvironmentService(db, nil, nil, nil, nil)

	oldToken := "edge-token-old"
	newToken := "edge-token-new"
	createTestEnvironmentWithState(t, db, "edge-auth", "edge://auth", string(models.EnvironmentStatusPending), true, &oldToken)

	envID, err := svc.ResolveEdgeEnvironmentByToken(ctx, oldToken)
	require.NoError(t, err)
	require.Equal(t, "edge-auth", envID)

	_, err = svc.UpdateEnvironment(ctx, "edge-auth", map[string]any{"access_token": newToken}, nil, nil)
	require.NoError(t, err)

	_, err = svc.ResolveEdgeEnvironmentByToken(ctx, oldToken)
	require.Error(t, err)
	require.Contains(t, err.Error(), "invalid agent token")

	envID, err = svc.ResolveEdgeEnvironmentByToken(ctx, newToken)
	require.NoError(t, err)
	require.Equal(t, "edge-auth", envID)

	require.NoError(t, svc.DeleteEnvironment(ctx, "edge-auth", nil, nil))
	_, err = svc.ResolveEdgeEnvironmentByToken(ctx, newToken)
	require.Error(t, err)
	require.Contains(t, err.Error(), "invalid agent token")
}

func TestEnvironmentService_UpdateEnvironment_ClearingAccessTokenInvalidatesCache(t *testing.T) {
	ctx := context.Background()
	db := setupEnvironmentServiceTestDB(t)
	svc := NewEnvironmentService(db, nil, nil, nil, nil)

	oldToken := "edge-token-clear"
	createTestEnvironmentWithState(t, db, "edge-auth-clear", "edge://auth-clear", string(models.EnvironmentStatusPending), true, &oldToken)

	envID, err := svc.ResolveEdgeEnvironmentByToken(ctx, oldToken)
	require.NoError(t, err)
	require.Equal(t, "edge-auth-clear", envID)

	_, err = svc.UpdateEnvironment(ctx, "edge-auth-clear", map[string]any{"access_token": nil}, nil, nil)
	require.NoError(t, err)

	cachedEnvID, ok := svc.getCachedEnvironmentIDForTokenInternal(oldToken, time.Now())
	require.False(t, ok)
	require.Empty(t, cachedEnvID)

	svc.tokenCacheMu.RLock()
	_, tokenStillCached := svc.tokenCache[oldToken]
	_, reverseIndexStillCached := svc.tokenByEnvID["edge-auth-clear"]
	svc.tokenCacheMu.RUnlock()

	require.False(t, tokenStillCached)
	require.False(t, reverseIndexStillCached)

	_, err = svc.ResolveEdgeEnvironmentByToken(ctx, oldToken)
	require.Error(t, err)
	require.Contains(t, err.Error(), "invalid agent token")
}

func TestEnvironmentService_getCachedEnvironmentIDForTokenInternal_ExpiresAndCleansReverseIndex(t *testing.T) {
	svc := NewEnvironmentService(nil, nil, nil, nil, nil)
	now := time.Now()

	svc.cacheEnvironmentTokenInternal("env-expired", "expired-token", now.Add(-2*edgeTokenCacheTTL))

	envID, ok := svc.getCachedEnvironmentIDForTokenInternal("expired-token", now)
	require.False(t, ok)
	require.Empty(t, envID)

	svc.tokenCacheMu.RLock()
	_, tokenStillCached := svc.tokenCache["expired-token"]
	_, reverseIndexStillCached := svc.tokenByEnvID["env-expired"]
	svc.tokenCacheMu.RUnlock()

	require.False(t, tokenStillCached)
	require.False(t, reverseIndexStillCached)
}

func TestEnvironmentService_ResolveEnvironmentByAccessToken(t *testing.T) {
	ctx := context.Background()
	db := setupEnvironmentServiceTestDB(t)
	svc := NewEnvironmentService(db, nil, nil, nil, nil)

	accessToken := "remote-token"
	createNamedTestEnvironmentInternal(t, db, "env-remote", "Remote Alpha", "http://remote.example", &accessToken)

	env, err := svc.ResolveEnvironmentByAccessToken(ctx, accessToken)
	require.NoError(t, err)
	require.NotNil(t, env)
	require.Equal(t, "env-remote", env.ID)
	require.Equal(t, "Remote Alpha", env.Name)

	_, err = svc.ResolveEnvironmentByAccessToken(ctx, "missing-token")
	require.Error(t, err)
	require.ErrorIs(t, err, ErrInvalidEnvironmentAccessToken)
}

func TestEnvironmentService_GenerateDeploymentSnippets_ExplicitlyUsePollTransport(t *testing.T) {
	svc := NewEnvironmentService(nil, nil, nil, nil, nil)

	standard, err := svc.GenerateDeploymentSnippets(context.Background(), "env-1", "https://manager.example.com", "token-123")
	require.NoError(t, err)
	require.NotNil(t, standard)
	require.NotContains(t, standard.DockerRun, "EDGE_TRANSPORT=websocket")
	require.NotContains(t, standard.DockerCompose, "EDGE_TRANSPORT=websocket")
	require.Contains(t, standard.DockerRun, "EDGE_TRANSPORT=poll")
	require.Contains(t, standard.DockerCompose, "EDGE_TRANSPORT=poll")
	require.True(t, strings.Contains(standard.DockerRun, "AGENT_TOKEN=token-123"))
	require.Contains(t, standard.DockerRun, "-v arcane-data:/app/data")
	require.Contains(t, standard.DockerCompose, "- arcane-data:/app/data")
	require.NotContains(t, standard.DockerRun, "-v arcane-data:/data")

	edgeSnippets, err := svc.GenerateEdgeDeploymentSnippets(context.Background(), "env-2", "https://manager.example.com", "token-456")
	require.NoError(t, err)
	require.NotNil(t, edgeSnippets)
	require.NotContains(t, edgeSnippets.DockerRun, "EDGE_TRANSPORT=websocket")
	require.NotContains(t, edgeSnippets.DockerCompose, "EDGE_TRANSPORT=websocket")
	require.Contains(t, edgeSnippets.DockerRun, "EDGE_TRANSPORT=poll")
	require.Contains(t, edgeSnippets.DockerCompose, "EDGE_TRANSPORT=poll")
	require.True(t, strings.Contains(edgeSnippets.DockerRun, "AGENT_TOKEN=token-456"))
	require.Contains(t, edgeSnippets.DockerRun, "-v arcane-data:/app/data")
	require.Contains(t, edgeSnippets.DockerCompose, "- arcane-data:/app/data")
	require.NotContains(t, edgeSnippets.DockerRun, "-v arcane-data:/data")
}
