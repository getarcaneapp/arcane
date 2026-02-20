package services

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	glsqlite "github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"github.com/getarcaneapp/arcane/backend/internal/config"
	"github.com/getarcaneapp/arcane/backend/internal/database"
	"github.com/getarcaneapp/arcane/backend/internal/models"
	"github.com/getarcaneapp/arcane/backend/internal/utils/crypto"
	"github.com/getarcaneapp/arcane/types/containerregistry"
)

func setupEnvironmentServiceTestDB(t *testing.T) *database.DB {
	t.Helper()

	db, err := gorm.Open(glsqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&models.Environment{}, &models.ContainerRegistry{}))

	testCfg := &config.Config{
		EncryptionKey: "test-encryption-key-for-testing-32bytes-min",
		Environment:   "test",
	}
	crypto.InitEncryption(testCfg)

	return &database.DB{DB: db}
}

func createTestEnvironment(t *testing.T, db *database.DB, id string, apiURL string, accessToken *string) {
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
		Status:      string(models.EnvironmentStatusOnline),
		Enabled:     true,
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
		URL:       "registry.example.com",
		Username:  "registry-user",
		Token:     encryptedToken,
		Enabled:   true,
		Insecure:  false,
		CreatedAt: now,
		UpdatedAt: now,
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
