package environment

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/getarcaneapp/arcane/types/v3"
	"github.com/labstack/echo/v5"
	"github.com/libtnb/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	kit "go.getarcane.app/kit/pkg"
	"go.getarcane.app/sys/crypto"
	"gorm.io/gorm"

	"github.com/getarcaneapp/arcane/backend/v3/internal/apikey"
	"github.com/getarcaneapp/arcane/backend/v3/internal/config"
	"github.com/getarcaneapp/arcane/backend/v3/internal/database"
	"github.com/getarcaneapp/arcane/backend/v3/internal/event"
	"github.com/getarcaneapp/arcane/backend/v3/internal/gitrepo"
	"github.com/getarcaneapp/arcane/backend/v3/internal/registry"
	"github.com/getarcaneapp/arcane/backend/v3/internal/s3"
	"github.com/getarcaneapp/arcane/backend/v3/internal/session"
	"github.com/getarcaneapp/arcane/backend/v3/internal/settings"
	"github.com/getarcaneapp/arcane/backend/v3/internal/user"
	"github.com/getarcaneapp/arcane/backend/v3/pkg/libarcane/agent"
	"github.com/getarcaneapp/arcane/backend/v3/pkg/pagination"
	"github.com/getarcaneapp/arcane/backend/v3/pkg/scheduler/entityjobs"
	"github.com/getarcaneapp/arcane/backend/v3/pkg/scheduler/runs"
	francistest "github.com/getarcaneapp/arcane/backend/v3/pkg/utils/francis/testing"
	"github.com/getarcaneapp/arcane/backend/v3/pkg/utils/httpx"
)

type environmentTestScheduler struct {
	submitted []types.SchedulerRequest
	added     []string
	removed   []string
}

// Project imports environment, so these rows avoid an import cycle in tests.
type testProjectRow struct {
	database.BaseModel
	Name            string
	Path            string
	Status          string
	GitOpsManagedBy *string `gorm:"column:gitops_managed_by"`
}

type testGitOpsSyncRow struct {
	database.BaseModel
	Name          string
	EnvironmentID string
	RepositoryID  string
	ComposePath   string
	ProjectName   string
	SyncInterval  int
}

type testFederatedCredentialRow struct {
	database.BaseModel
	Name           string
	IdentityUserID string  `gorm:"column:identity_user_id"`
	EnvironmentID  *string `gorm:"column:environment_id"`
}

type testGlobalVariableRow struct {
	database.BaseModel
	Key             string
	AllEnvironments bool `gorm:"column:all_environments"`
}

type testGlobalVariableEnvironmentRow struct {
	GlobalVariableID string `gorm:"column:global_variable_id;primaryKey"`
	EnvironmentID    string `gorm:"column:environment_id;primaryKey"`
}

func (testFederatedCredentialRow) TableName() string { return "federated_credentials" }

func (testGlobalVariableRow) TableName() string { return "global_variables" }

func (testGlobalVariableEnvironmentRow) TableName() string { return "global_variable_environments" }

func TestEnvironmentService_OverlappingHealthCheckIsSkipped(t *testing.T) {
	gate := newAdmissionGateForEnvironmentTest(t)

	key := types.SchedulerAdmissionKey{Scope: environmentHealthAdmissionScope, ID: "environment-id"}
	lease, admitted, err := gate.TryAcquire(t.Context(), key)
	require.NoError(t, err)
	require.True(t, admitted)

	service := NewEnvironmentService(nil, nil, nil, nil, nil)
	require.NoError(t, service.SetScheduler(t.Context(), &environmentTestScheduler{}, gate))
	outcome, runErr := service.runHealthCheck(t.Context(), "environment-id")
	require.NoError(t, runErr)
	require.Equal(t, types.RunStatusSkipped, outcome.Status)
	lease.Release(t.Context())
}

func (s *environmentTestScheduler) AddJob(_ context.Context, job types.SchedulerJob) error {
	s.added = append(s.added, job.Name())
	return nil
}

func (s *environmentTestScheduler) RemoveJob(_ context.Context, name string) {
	s.removed = append(s.removed, name)
}

func (s *environmentTestScheduler) HasJob(_ string) bool {
	return false
}

func newAdmissionGateForEnvironmentTest(t testing.TB) *runs.Admission {
	t.Helper()
	runtime := francistest.New(t)
	gate := runs.NewAdmission(runtime.Service(), t.Name())
	require.NoError(t, gate.Register(runtime))
	francistest.Start(t, runtime)
	return gate
}

func (testProjectRow) TableName() string { return "projects" }

func (testGitOpsSyncRow) TableName() string { return "gitops_syncs" }

func setupEnvironmentServiceTestDB(t *testing.T) *database.DB {
	t.Helper()

	dsn := fmt.Sprintf("file:%s?mode=memory&cache=shared", strings.NewReplacer("/", "_", " ", "_").Replace(t.Name()))
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(
		&Environment{},
		&registry.ContainerRegistry{},
		&s3.S3Destination{},
		&settings.SettingVariable{},
		&user.User{},
		&apikey.ApiKey{},
		&event.Event{},
		&testProjectRow{},
		&testGitOpsSyncRow{},
		&testFederatedCredentialRow{},
		&testGlobalVariableRow{},
		&testGlobalVariableEnvironmentRow{},
	))

	sqlDB, err := db.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(1)
	sqlDB.SetMaxIdleConns(1)

	testCfg := &config.Config{
		EncryptionKey: "test-encryption-key-for-testing-32bytes-min",
		Environment:   "test",
	}
	crypto.InitEncryption(&crypto.Config{
		EncryptionKey: testCfg.EncryptionKey,
		Environment:   string(testCfg.Environment),
		AgentMode:     testCfg.AgentEnabled,
	})

	return &database.DB{DB: db}
}

func createTestEnvironmentServiceUser(t *testing.T, ctx context.Context, userService *user.UserService, id string) *user.User {
	t.Helper()

	localUser := &user.User{
		ID:       id,
		Username: "user-" + id,
	}

	created, err := userService.CreateUser(ctx, localUser)
	require.NoError(t, err)
	return created
}

func createTestEnvironment(t *testing.T, db *database.DB, id, apiURL string, accessToken *string) {
	t.Helper()
	createNamedTestEnvironment(t, db, id, "env-"+id, apiURL, accessToken)
}

func createNamedTestEnvironment(t *testing.T, db *database.DB, id, name, apiURL string, accessToken *string) {
	t.Helper()

	now := time.Now()
	env := &Environment{
		ID:          id,
		CreatedAt:   now,
		UpdatedAt:   &now,
		Name:        name,
		ApiUrl:      apiURL,
		Status:      string(EnvironmentStatusOnline),
		Enabled:     true,
		AccessToken: accessToken,
	}

	require.NoError(t, db.WithContext(t.Context()).Create(env).Error)
}

func startTestAgent(t *testing.T, envID string, handler http.Handler) string {
	t.Helper()

	manager := echo.New()
	manager.GET("/api/tunnel/connect", agent.NewTunnelServer(agent.TunnelServerOptions{
		Registry: agent.GetRegistry(),
		Resolver: func(context.Context, string) (string, error) {
			return envID, nil
		},
	}).HandleConnect)
	server := httptest.NewServer(manager)
	t.Cleanup(server.Close)

	agentCtx, cancel := context.WithCancel(context.WithoutCancel(t.Context()))
	t.Cleanup(func() {
		cancel()
		agent.GetRegistry().Unregister(envID)
	})
	go agent.NewTunnelClient(&agent.Config{
		AgentEnabled:   true,
		AgentTransport: agent.TransportWebSocket,
		ManagerApiUrl:  server.URL,
		AgentToken:     "agent-" + envID,
	}, handler).StartWithErrorChan(agentCtx, nil)
	require.True(t, agent.GetRegistry().WaitForActive(t.Context(), envID, 5*time.Second).IsPresent())

	return "agent://" + envID
}

func createTestEnvironmentWithState(t *testing.T, db *database.DB, id, apiURL, status string, accessToken *string) {
	t.Helper()

	now := time.Now()
	env := &Environment{
		ID:          id,
		CreatedAt:   now,
		UpdatedAt:   &now,
		Name:        "env-" + id,
		ApiUrl:      apiURL,
		Status:      status,
		Enabled:     true,
		AccessToken: accessToken,
	}

	require.NoError(t, db.WithContext(t.Context()).Create(env).Error)
}

func TestEnvironmentService_DeleteEnvironment_CascadesGitOpsSyncs(t *testing.T) {
	ctx := t.Context()
	db := setupEnvironmentServiceTestDB(t)
	require.NoError(t, db.AutoMigrate(&testProjectRow{}, &testGitOpsSyncRow{}))

	createTestEnvironment(t, db, "env-delete-gitops", "http://env.example", nil)
	syncID := "sync-delete-env"
	require.NoError(t, db.Create(&testGitOpsSyncRow{
		ID:            syncID,
		Name:          "delete-env-sync",
		EnvironmentID: "env-delete-gitops",
		RepositoryID:  "repo-1",
		ComposePath:   "compose.yml",
		ProjectName:   "demo",
		SyncInterval:  15,
	}).Error)
	require.NoError(t, db.Create(&testProjectRow{
		ID:              "project-managed-by-deleted-env",
		Name:            "demo",
		Path:            "/tmp/demo",
		Status:          "stopped",
		GitOpsManagedBy: &syncID,
	}).Error)

	jobScheduler := &environmentTestScheduler{}
	svc := NewEnvironmentService(db, nil, event.NewEventService(db, nil, nil), nil, nil)
	require.NoError(t, svc.SetScheduler(ctx, jobScheduler, newAdmissionGateForEnvironmentTest(t)))

	require.NoError(t, svc.DeleteEnvironment(ctx, "env-delete-gitops", nil, nil))

	var syncCount int64
	require.NoError(t, db.Model(&testGitOpsSyncRow{}).Where("id = ?", syncID).Count(&syncCount).Error)
	require.Zero(t, syncCount)

	var project testProjectRow
	require.NoError(t, db.First(&project, "id = ?", "project-managed-by-deleted-env").Error)
	require.Nil(t, project.GitOpsManagedBy)
	require.Contains(t, jobScheduler.removed, entityjobs.GitOpsSyncJobPrefix+syncID)
}

func TestEnvironmentService_DeleteEnvironment_RemovesScopedCredentialsAndVariables(t *testing.T) {
	ctx := t.Context()
	db := setupEnvironmentServiceTestDB(t)

	createTestEnvironment(t, db, "env-delete-scoped", "http://scoped.example", nil)
	createTestEnvironment(t, db, "env-keep", "http://keep.example", nil)
	require.NoError(t, db.Create(&user.User{ID: "federated-user", Username: "federated-user"}).Error)
	require.NoError(t, db.Create(&testFederatedCredentialRow{
		ID:             "credential-scoped",
		Name:           "scoped",
		IdentityUserID: "federated-user",
		EnvironmentID:  new("env-delete-scoped"),
	}).Error)
	require.NoError(t, db.Create(&[]testGlobalVariableRow{
		{ID: "var-only-deleted", Key: "ONLY_DELETED"},
		{ID: "var-shared", Key: "SHARED"},
		{ID: "var-all", Key: "ALL", AllEnvironments: true},
	}).Error)
	require.NoError(t, db.Create(&[]testGlobalVariableEnvironmentRow{
		{GlobalVariableID: "var-only-deleted", EnvironmentID: "env-delete-scoped"},
		{GlobalVariableID: "var-shared", EnvironmentID: "env-delete-scoped"},
		{GlobalVariableID: "var-shared", EnvironmentID: "env-keep"},
	}).Error)

	svc := NewEnvironmentService(db, nil, event.NewEventService(db, nil, nil), nil, nil)
	var invalidatedUserIDs []string
	svc.SetUserInvalidator(func(userID string) { invalidatedUserIDs = append(invalidatedUserIDs, userID) })
	require.NoError(t, svc.DeleteEnvironment(ctx, "env-delete-scoped", nil, nil))
	require.Equal(t, []string{"federated-user"}, invalidatedUserIDs)

	var credentialCount, userCount int64
	require.NoError(t, db.Model(&testFederatedCredentialRow{}).Count(&credentialCount).Error)
	require.NoError(t, db.Model(&user.User{}).Where("id = ?", "federated-user").Count(&userCount).Error)
	require.Zero(t, credentialCount)
	require.Zero(t, userCount)

	var variableIDs []string
	require.NoError(t, db.Model(&testGlobalVariableRow{}).Order("id").Pluck("id", &variableIDs).Error)
	require.Equal(t, []string{"var-all", "var-shared"}, variableIDs)

	var scopes []testGlobalVariableEnvironmentRow
	require.NoError(t, db.Find(&scopes).Error)
	require.Equal(t, []testGlobalVariableEnvironmentRow{{GlobalVariableID: "var-shared", EnvironmentID: "env-keep"}}, scopes)
}

func TestEnvironmentService_IsKnownAccessToken_TracksTokenWrites(t *testing.T) {
	ctx := t.Context()
	db := setupEnvironmentServiceTestDB(t)
	createTestEnvironmentWithState(t, db, "env-known", "http://known.example", string(EnvironmentStatusOnline), new("seeded-token"))
	createTestEnvironmentWithState(t, db, "env-shared", "http://shared.example", string(EnvironmentStatusOnline), new("seeded-token"))
	svc := NewEnvironmentService(db, nil, event.NewEventService(db, nil, nil), nil, nil)

	require.False(t, svc.IsKnownAccessToken("seeded-token"))
	require.NoError(t, svc.LoadAccessTokens(ctx))
	require.True(t, svc.IsKnownAccessToken("seeded-token"))
	require.False(t, svc.IsKnownAccessToken(strings.Repeat("x", maxAccessTokenLength+1)))

	require.NoError(t, svc.linkEnvironmentApiKey(ctx, "env-known", "key-rotated", "rotated-token", "", "", "env-known"))
	require.True(t, svc.IsKnownAccessToken("seeded-token"))
	require.True(t, svc.IsKnownAccessToken("rotated-token"))

	_, err := svc.UpdateEnvironment(ctx, "env-known", map[string]any{"access_token": "updated-token"}, nil, nil)
	require.NoError(t, err)
	require.False(t, svc.IsKnownAccessToken("rotated-token"))
	require.True(t, svc.IsKnownAccessToken("updated-token"))

	require.NoError(t, svc.DeleteEnvironment(ctx, "env-known", nil, nil))
	require.False(t, svc.IsKnownAccessToken("updated-token"))
	require.True(t, svc.IsKnownAccessToken("seeded-token"))

	require.NoError(t, svc.DeleteEnvironment(ctx, "env-shared", nil, nil))
	require.False(t, svc.IsKnownAccessToken("seeded-token"))
}

func createTestRegistry(t *testing.T, db *database.DB, id string) {
	t.Helper()

	encryptedToken, err := crypto.Encrypt("registry-token")
	require.NoError(t, err)

	now := time.Now()
	localRegistry := &registry.ContainerRegistry{
		BaseModel: database.BaseModel{
			ID:        id,
			CreatedAt: now,
			UpdatedAt: &now,
		},
		URL:          "registry.example.com",
		Username:     "registry-user",
		Token:        encryptedToken,
		Enabled:      true,
		Insecure:     false,
		RegistryType: registry.RegistryTypeGeneric,
		CreatedAt:    now,
		UpdatedAt:    now,
	}

	require.NoError(t, db.WithContext(t.Context()).Create(localRegistry).Error)
}

func createTestECRRegistry(t *testing.T, db *database.DB, id string) {
	t.Helper()

	encryptedSecret, err := crypto.Encrypt("aws-secret")
	require.NoError(t, err)

	now := time.Now()
	localRegistry := &registry.ContainerRegistry{
		BaseModel: database.BaseModel{
			ID:        id,
			CreatedAt: now,
			UpdatedAt: &now,
		},
		URL:                "123456789012.dkr.ecr.us-east-1.amazonaws.com",
		Enabled:            true,
		RegistryType:       registry.RegistryTypeECR,
		AWSAccessKeyID:     "AKIA1234567890EXAMPLE",
		AWSSecretAccessKey: encryptedSecret,
		AWSRegion:          "us-east-1",
		CreatedAt:          now,
		UpdatedAt:          now,
	}

	require.NoError(t, db.WithContext(t.Context()).Create(localRegistry).Error)
}

func createTestGitRepository(t *testing.T, db *database.DB, repository gitrepo.GitRepository) {
	t.Helper()
	require.NoError(t, db.Create(&repository).Error)
}

func encryptSecretForTest(t *testing.T, value string) string {
	t.Helper()
	encrypted, err := crypto.Encrypt(value)
	require.NoError(t, err)
	return encrypted
}

func TestEnvironmentService_SyncRegistriesToRemoteEnvironments_SyncsEligibleRemotes(t *testing.T) {
	ctx := t.Context()
	db := setupEnvironmentServiceTestDB(t)
	svc := NewEnvironmentService(db, nil, event.NewEventService(db, nil, nil), nil, nil)

	createTestRegistry(t, db, "reg-1")

	var env1Calls atomic.Int32
	env1Token := "token-1"
	env1Agent := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !assert.Equal(t, http.MethodPost, r.Method) {
			return
		}
		if !assert.Equal(t, "/api/registries/sync", r.URL.Path) {
			return
		}
		if !assert.Equal(t, env1Token, r.Header.Get("X-API-Key")) {
			return
		}
		if !assert.Equal(t, env1Token, r.Header.Get("X-Arcane-Agent-Token")) {
			return
		}

		var syncReq types.ContainerRegistrySyncRequest
		if !assert.NoError(t, json.NewDecoder(r.Body).Decode(&syncReq)) {
			return
		}
		if !assert.Len(t, syncReq.Registries, 1) {
			return
		}
		env1Calls.Add(1)

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"success":true,"data":{"message":"ok"}}`))
	})

	var env2Calls atomic.Int32
	env2Token := "token-2"
	env2Agent := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !assert.Equal(t, http.MethodPost, r.Method) {
			return
		}
		if !assert.Equal(t, "/api/registries/sync", r.URL.Path) {
			return
		}
		if !assert.Equal(t, env2Token, r.Header.Get("X-API-Key")) {
			return
		}
		if !assert.Equal(t, env2Token, r.Header.Get("X-Arcane-Agent-Token")) {
			return
		}

		var syncReq types.ContainerRegistrySyncRequest
		if !assert.NoError(t, json.NewDecoder(r.Body).Decode(&syncReq)) {
			return
		}
		if !assert.Len(t, syncReq.Registries, 1) {
			return
		}
		env2Calls.Add(1)

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"success":true,"data":{"message":"ok"}}`))
	})

	createTestEnvironment(t, db, "0", "http://localhost:3552", nil) // local env should be excluded
	createTestEnvironment(t, db, "env-1", startTestAgent(t, "env-1", env1Agent), &env1Token)
	createTestEnvironment(t, db, "env-2", startTestAgent(t, "env-2", env2Agent), &env2Token)

	err := svc.SyncRegistriesToRemoteEnvironments(ctx)
	require.NoError(t, err)
	require.EqualValues(t, 1, env1Calls.Load())
	require.EqualValues(t, 1, env2Calls.Load())
}

func TestEnvironmentService_SyncRegistriesToEnvironment_IncludesECRFields(t *testing.T) {
	ctx := t.Context()
	db := setupEnvironmentServiceTestDB(t)
	svc := NewEnvironmentService(db, nil, event.NewEventService(db, nil, nil), nil, nil)

	createTestECRRegistry(t, db, "reg-ecr")

	agentHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !assert.Equal(t, http.MethodPost, r.Method) {
			return
		}
		if !assert.Equal(t, "/api/registries/sync", r.URL.Path) {
			return
		}

		var syncReq types.ContainerRegistrySyncRequest
		if !assert.NoError(t, json.NewDecoder(r.Body).Decode(&syncReq)) {
			return
		}
		if !assert.Len(t, syncReq.Registries, 1) {
			return
		}

		syncedRegistry := syncReq.Registries[0]
		if !assert.Equal(t, registry.RegistryTypeECR, syncedRegistry.RegistryType) {
			return
		}
		if !assert.Equal(t, "123456789012.dkr.ecr.us-east-1.amazonaws.com", syncedRegistry.URL) {
			return
		}
		if !assert.Equal(t, "AKIA1234567890EXAMPLE", syncedRegistry.AWSAccessKeyID) {
			return
		}
		if !assert.Equal(t, "aws-secret", syncedRegistry.AWSSecretAccessKey) {
			return
		}
		if !assert.Equal(t, "us-east-1", syncedRegistry.AWSRegion) {
			return
		}
		if !assert.Empty(t, syncedRegistry.Username) {
			return
		}
		if !assert.Empty(t, syncedRegistry.Token) {
			return
		}

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"success":true,"data":{"message":"ok"}}`))
	})

	createTestEnvironment(t, db, "env-1", startTestAgent(t, "env-1", agentHandler), new("token-1"))

	err := svc.SyncRegistriesToEnvironment(ctx, "env-1")
	require.NoError(t, err)
}

func TestEnvironmentService_SyncS3DestinationsToEnvironment(t *testing.T) {
	ctx := t.Context()
	db := setupEnvironmentServiceTestDB(t)
	svc := NewEnvironmentService(db, nil, event.NewEventService(db, nil, nil), nil, nil)

	encryptedSecret, err := crypto.Encrypt("s3-secret")
	require.NoError(t, err)
	require.NoError(t, db.WithContext(ctx).Create(&s3.S3Destination{
		ID:              "s3-1",
		Name:            "Offsite",
		Endpoint:        "https://s3.example.com",
		Bucket:          "volume-backups",
		Region:          "eu-central-1",
		AccessKeyID:     "s3-access-key",
		SecretAccessKey: encryptedSecret,
		Prefix:          "agents",
		UseSSL:          true,
		ForcePathStyle:  true,
	}).Error)

	accessToken := "agent-token"
	agentHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !assert.Equal(t, http.MethodPost, r.Method) {
			return
		}
		if !assert.Equal(t, "/api/backups/s3/sync", r.URL.Path) {
			return
		}
		if !assert.Equal(t, accessToken, r.Header.Get("X-API-Key")) {
			return
		}
		if !assert.Equal(t, accessToken, r.Header.Get("X-Arcane-Agent-Token")) {
			return
		}

		var syncReq types.S3DestinationSyncRequest
		if !assert.NoError(t, json.NewDecoder(r.Body).Decode(&syncReq)) {
			return
		}
		if !assert.Len(t, syncReq.Destinations, 1) {
			return
		}
		if !assert.Equal(t, "s3-1", syncReq.Destinations[0].ID) {
			return
		}
		if !assert.Equal(t, "s3-secret", syncReq.Destinations[0].SecretAccessKey) {
			return
		}
		if !assert.Equal(t, "volume-backups", syncReq.Destinations[0].Bucket) {
			return
		}

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"success":true,"data":{"message":"ok"}}`))
	})

	createTestEnvironment(t, db, "env-s3", startTestAgent(t, "env-s3", agentHandler), &accessToken)
	require.NoError(t, svc.SyncS3DestinationsToEnvironment(ctx, "env-s3"))
}

func TestEnvironmentService_SyncRepositoriesToEnvironment_UsesAgentHeaders(t *testing.T) {
	ctx := t.Context()
	db := setupEnvironmentServiceTestDB(t)
	require.NoError(t, db.AutoMigrate(&gitrepo.GitRepository{}))
	svc := NewEnvironmentService(db, nil, event.NewEventService(db, nil, nil), nil, nil)

	createTestGitRepository(t, db, gitrepo.GitRepository{
		ID: "repo-1", CreatedAt: time.Now(),
		Name:        "repo-1",
		URL:         "https://github.com/getarcaneapp/arcane.git",
		AuthType:    "http",
		Username:    "arcane",
		Token:       encryptSecretForTest(t, "repo-token"),
		Enabled:     true,
		Description: new("test repo"),
	})

	accessToken := "token-1"
	agentHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !assert.Equal(t, http.MethodPost, r.Method) {
			return
		}
		if !assert.Equal(t, "/api/git/repositories/sync", r.URL.Path) {
			return
		}
		if !assert.Equal(t, accessToken, r.Header.Get("X-API-Key")) {
			return
		}
		if !assert.Equal(t, accessToken, r.Header.Get("X-Arcane-Agent-Token")) {
			return
		}

		var syncReq types.GitRepositorySyncRequest
		if !assert.NoError(t, json.NewDecoder(r.Body).Decode(&syncReq)) {
			return
		}
		if !assert.Len(t, syncReq.Repositories, 1) {
			return
		}
		if !assert.Equal(t, "repo-token", syncReq.Repositories[0].Token) {
			return
		}
		if !assert.Equal(t, "arcane", syncReq.Repositories[0].Username) {
			return
		}

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"success":true,"data":{"message":"ok"}}`))
	})

	createTestEnvironment(t, db, "env-1", startTestAgent(t, "env-1", agentHandler), &accessToken)

	err := svc.SyncRepositoriesToEnvironment(ctx, "env-1")
	require.NoError(t, err)
}

func TestEnvironmentService_SyncRegistriesToRemoteEnvironments_SkipsRemoteWithoutAccessToken(t *testing.T) {
	ctx := t.Context()
	db := setupEnvironmentServiceTestDB(t)
	svc := NewEnvironmentService(db, nil, event.NewEventService(db, nil, nil), nil, nil)

	createTestRegistry(t, db, "reg-1")

	var syncCalls atomic.Int32
	agentHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		syncCalls.Add(1)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"success":true,"data":{"message":"ok"}}`))
	})

	createTestEnvironment(t, db, "env-auth", startTestAgent(t, "env-auth", agentHandler), new("token-with-auth"))
	createTestEnvironment(t, db, "env-no-token", "agent://env-no-token", nil)

	err := svc.SyncRegistriesToRemoteEnvironments(ctx)
	require.NoError(t, err)
	require.EqualValues(t, 1, syncCalls.Load())
}

func TestEnvironmentService_SyncRegistriesToRemoteEnvironments_ReportsFailuresButContinues(t *testing.T) {
	ctx := t.Context()
	db := setupEnvironmentServiceTestDB(t)
	svc := NewEnvironmentService(db, nil, event.NewEventService(db, nil, nil), nil, nil)

	createTestRegistry(t, db, "reg-1")

	var successCalls atomic.Int32
	successAgent := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		successCalls.Add(1)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"success":true,"data":{"message":"ok"}}`))
	})
	failingAgent := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "agent unavailable", http.StatusInternalServerError)
	})

	createTestEnvironment(t, db, "env-success", startTestAgent(t, "env-success", successAgent), new("token-success"))
	createTestEnvironment(t, db, "env-fail", startTestAgent(t, "env-fail", failingAgent), new("token-fail"))

	err := svc.SyncRegistriesToRemoteEnvironments(ctx)
	require.Error(t, err)
	require.Contains(t, err.Error(), "failed to sync registries to 1 remote environment")
	require.EqualValues(t, 1, successCalls.Load())
}

func TestEnvironmentService_ReconcileAgentStatusesOnStartup(t *testing.T) {
	ctx := t.Context()
	db := setupEnvironmentServiceTestDB(t)
	svc := NewEnvironmentService(db, nil, event.NewEventService(db, nil, nil), nil, nil)

	createTestEnvironmentWithState(t, db, "agent-online", "agent://online", string(EnvironmentStatusOnline), nil)
	createTestEnvironmentWithState(t, db, "agent-error", "agent://error", string(EnvironmentStatusError), nil)
	createTestEnvironmentWithState(t, db, "agent-pending", "agent://pending", string(EnvironmentStatusPending), nil)
	createTestEnvironmentWithState(t, db, "0", "http://localhost:3552", string(EnvironmentStatusOnline), nil)

	err := svc.ReconcileAgentStatusesOnStartup(ctx)
	require.NoError(t, err)

	var agentOnline Environment
	require.NoError(t, db.WithContext(ctx).Where("id = ?", "agent-online").First(&agentOnline).Error)
	require.Equal(t, string(EnvironmentStatusOffline), agentOnline.Status)

	var agentError Environment
	require.NoError(t, db.WithContext(ctx).Where("id = ?", "agent-error").First(&agentError).Error)
	require.Equal(t, string(EnvironmentStatusOffline), agentError.Status)

	var agentPending Environment
	require.NoError(t, db.WithContext(ctx).Where("id = ?", "agent-pending").First(&agentPending).Error)
	require.Equal(t, string(EnvironmentStatusPending), agentPending.Status)

	var local Environment
	require.NoError(t, db.WithContext(ctx).Where("id = ?", "0").First(&local).Error)
	require.Equal(t, string(EnvironmentStatusOnline), local.Status)
}

func TestEnvironmentService_UpdateEnvironmentConnectionState(t *testing.T) {
	ctx := t.Context()
	db := setupEnvironmentServiceTestDB(t)
	svc := NewEnvironmentService(db, nil, event.NewEventService(db, nil, nil), nil, nil)

	createTestEnvironmentWithState(t, db, "agent-runtime", "agent://runtime", string(EnvironmentStatusOffline), nil)

	err := svc.UpdateEnvironmentConnectionState(ctx, "agent-runtime", true)
	require.NoError(t, err)

	var env Environment
	require.NoError(t, db.WithContext(ctx).Where("id = ?", "agent-runtime").First(&env).Error)
	require.Equal(t, string(EnvironmentStatusOnline), env.Status)
	require.NotNil(t, env.LastSeen)

	lastSeen := env.LastSeen

	err = svc.UpdateEnvironmentConnectionState(ctx, "agent-runtime", false)
	require.NoError(t, err)

	require.NoError(t, db.WithContext(ctx).Where("id = ?", "agent-runtime").First(&env).Error)
	require.Equal(t, string(EnvironmentStatusOffline), env.Status)
	require.NotNil(t, env.LastSeen)
	require.Equal(t, *lastSeen, *env.LastSeen)
}

func TestEnvironmentService_UpdateEnvironmentStatus_DoesNotDemotePendingEnvOnFailedTick(t *testing.T) {
	ctx := t.Context()
	db := setupEnvironmentServiceTestDB(t)
	svc := NewEnvironmentService(db, nil, event.NewEventService(db, nil, nil), nil, nil)

	createTestEnvironmentWithState(t, db, "agent-pending", "agent://pending", string(EnvironmentStatusPending), nil)

	require.NoError(t, svc.updateEnvironmentStatus(ctx, "agent-pending", string(EnvironmentStatusOffline)))
	require.NoError(t, svc.updateEnvironmentStatus(ctx, "agent-pending", string(EnvironmentStatusError)))

	var env Environment
	require.NoError(t, db.WithContext(ctx).Where("id = ?", "agent-pending").First(&env).Error)
	require.Equal(t, string(EnvironmentStatusPending), env.Status)
	require.Nil(t, env.LastSeen)
}

func TestEnvironmentService_UpdateEnvironmentStatus_LeavesPendingAgentEnvAlone(t *testing.T) {
	ctx := t.Context()
	db := setupEnvironmentServiceTestDB(t)
	svc := NewEnvironmentService(db, nil, event.NewEventService(db, nil, nil), nil, nil)

	createTestEnvironmentWithState(t, db, "agent-pending", "agent://agent", string(EnvironmentStatusPending), nil)

	// Agent envs in pending must complete pairing via the agent's outbound tunnel;
	// a manager-side reachability tick must NOT promote them.
	require.NoError(t, svc.updateEnvironmentStatus(ctx, "agent-pending", string(EnvironmentStatusOnline)))

	var env Environment
	require.NoError(t, db.WithContext(ctx).Where("id = ?", "agent-pending").First(&env).Error)
	require.Equal(t, string(EnvironmentStatusPending), env.Status)
	require.Nil(t, env.LastSeen)
}

func TestEnvironmentService_ResolveAgentEnvironmentByToken_CachesAndInvalidatesOnUpdate(t *testing.T) {
	ctx := t.Context()
	db := setupEnvironmentServiceTestDB(t)
	svc := NewEnvironmentService(db, nil, event.NewEventService(db, nil, nil), nil, nil)

	oldToken := "agent-token-old"
	newToken := "agent-token-new"
	createTestEnvironmentWithState(t, db, "agent-auth", "agent://auth", string(EnvironmentStatusPending), &oldToken)

	envID, err := svc.ResolveAgentEnvironmentByToken(ctx, oldToken)
	require.NoError(t, err)
	require.Equal(t, "agent-auth", envID)

	_, err = svc.UpdateEnvironment(ctx, "agent-auth", map[string]any{"access_token": newToken}, nil, nil)
	require.NoError(t, err)

	_, err = svc.ResolveAgentEnvironmentByToken(ctx, oldToken)
	require.Error(t, err)
	require.Contains(t, err.Error(), "invalid agent token")

	envID, err = svc.ResolveAgentEnvironmentByToken(ctx, newToken)
	require.NoError(t, err)
	require.Equal(t, "agent-auth", envID)

	_, err = svc.UpdateEnvironment(ctx, "agent-auth", map[string]any{"enabled": false}, nil, nil)
	require.NoError(t, err)
	_, err = svc.ResolveAgentEnvironmentByToken(ctx, newToken)
	require.Error(t, err)

	require.NoError(t, svc.linkEnvironmentApiKey(ctx, "agent-auth", "key-rotated", "agent-token-rotated", "", "", "agent-auth"))
	_, err = svc.ResolveAgentEnvironmentByToken(ctx, "agent-token-rotated")
	require.Error(t, err)

	_, err = svc.UpdateEnvironment(ctx, "agent-auth", map[string]any{"enabled": true}, nil, nil)
	require.NoError(t, err)
	envID, err = svc.ResolveAgentEnvironmentByToken(ctx, "agent-token-rotated")
	require.NoError(t, err)
	require.Equal(t, "agent-auth", envID)

	require.NoError(t, db.WithContext(ctx).Model(&Environment{}).Where("id = ?", "agent-auth").Update("enabled", false).Error)
	svc.invalidateEnvironmentCache("agent-auth")
	_, err = svc.ResolveAgentEnvironmentByToken(ctx, "agent-token-rotated")
	require.Error(t, err)

	require.NoError(t, svc.DeleteEnvironment(ctx, "agent-auth", nil, nil))
	_, err = svc.ResolveAgentEnvironmentByToken(ctx, "agent-token-rotated")
	require.Error(t, err)
	require.Contains(t, err.Error(), "invalid agent token")
}

func TestEnvironmentService_UpdateEnvironment_ClearingAccessTokenInvalidatesCache(t *testing.T) {
	ctx := t.Context()
	db := setupEnvironmentServiceTestDB(t)
	svc := NewEnvironmentService(db, nil, event.NewEventService(db, nil, nil), nil, nil)

	oldToken := "agent-token-clear"
	createTestEnvironmentWithState(t, db, "agent-auth-clear", "agent://auth-clear", string(EnvironmentStatusPending), &oldToken)

	envID, err := svc.ResolveAgentEnvironmentByToken(ctx, oldToken)
	require.NoError(t, err)
	require.Equal(t, "agent-auth-clear", envID)

	_, err = svc.UpdateEnvironment(ctx, "agent-auth-clear", map[string]any{"access_token": nil}, nil, nil)
	require.NoError(t, err)

	_, tokenStillCached := svc.agentTokens.byToken.Peek(oldToken)
	svc.agentTokens.mu.RLock()
	_, reverseIndexStillCached := svc.agentTokens.byEnvID["agent-auth-clear"]
	svc.agentTokens.mu.RUnlock()

	require.False(t, tokenStillCached)
	require.False(t, reverseIndexStillCached)

	_, err = svc.ResolveAgentEnvironmentByToken(ctx, oldToken)
	require.Error(t, err)
	require.Contains(t, err.Error(), "invalid agent token")
}

func TestEnvironmentService_getCachedEnvironmentIDForToken_ExpiresAndCleansReverseIndex(t *testing.T) {
	db := setupEnvironmentServiceTestDB(t)
	svc := NewEnvironmentService(db, nil, event.NewEventService(db, nil, nil), nil, nil)
	svc.agentTokens.mu.Lock()
	svc.agentTokens.byEnvID["env-expired"] = "expired-token"
	svc.agentTokens.byToken.SetWithTTL("expired-token", "env-expired", time.Nanosecond)
	time.Sleep(time.Millisecond)
	svc.agentTokens.mu.Unlock()

	envID, err := svc.ResolveAgentEnvironmentByToken(t.Context(), "expired-token")
	require.ErrorContains(t, err, "invalid agent token")
	require.Empty(t, envID)

	_, tokenStillCached := svc.agentTokens.byToken.Peek("expired-token")
	svc.agentTokens.mu.RLock()
	_, reverseIndexStillCached := svc.agentTokens.byEnvID["env-expired"]
	svc.agentTokens.mu.RUnlock()

	require.False(t, tokenStillCached)
	require.False(t, reverseIndexStillCached)
}

func TestEnvironmentService_ResolveEnvironmentByAccessToken(t *testing.T) {
	ctx := t.Context()
	db := setupEnvironmentServiceTestDB(t)
	svc := NewEnvironmentService(db, nil, event.NewEventService(db, nil, nil), nil, nil)

	accessToken := "remote-token"
	createNamedTestEnvironment(t, db, "env-remote", "Remote Alpha", "http://remote.example", &accessToken)

	env, err := svc.ResolveEnvironmentByAccessToken(ctx, accessToken)
	require.NoError(t, err)
	require.NotNil(t, env)
	require.Equal(t, "env-remote", env.ID)
	require.Equal(t, "Remote Alpha", env.Name)

	_, err = svc.ResolveEnvironmentByAccessToken(ctx, "missing-token")
	require.Error(t, err)
	require.ErrorIs(t, err, ErrInvalidEnvironmentAccessToken)
}

func TestEnvironmentService_GenerateDeploymentSnippets_TransportDefaults(t *testing.T) {
	db := setupEnvironmentServiceTestDB(t)
	svc := NewEnvironmentService(db, nil, event.NewEventService(db, nil, nil), nil, nil)

	agentSnippets, err := svc.GenerateAgentDeploymentSnippets(t.Context(), "env-2", "https://manager.example.com", "token-456", nil)
	require.NoError(t, err)
	require.NotNil(t, agentSnippets)
	require.Contains(t, agentSnippets.DockerRun, "ARCANE_AGENT_TRANSPORT=auto")
	require.Contains(t, agentSnippets.DockerCompose, "ARCANE_AGENT_TRANSPORT=auto")
	require.Contains(t, agentSnippets.DockerRun, "ARCANE_AGENT_TOKEN=token-456")
	require.Contains(t, agentSnippets.DockerRun, "-v arcane-data:/app/data")
	require.Contains(t, agentSnippets.DockerCompose, "- arcane-data:/app/data")
	require.NotContains(t, agentSnippets.DockerRun, "-v arcane-data:/data")
}

func TestEnvironmentService_EnsureSwarmNodeAgentEnvironment_CreatesVisibleEnvironmentAndReusesToken(t *testing.T) {
	ctx := t.Context()
	db := setupEnvironmentServiceTestDB(t)
	userService := user.NewUserService(db, nil, session.RevokeAllUserSessionsExceptInDB)
	apiKeyService := apikey.NewApiKeyService(db, userService, nil)
	svc := NewEnvironmentService(db, nil, event.NewEventService(db, nil, nil), nil, apiKeyService)
	localUser := createTestEnvironmentServiceUser(t, ctx, userService, "swarm-admin")

	createdEnv, createdToken, err := svc.EnsureSwarmNodeAgentEnvironment(
		ctx,
		"manager-env",
		"node-1234567890abcdef",
		"worker-1",
		localUser.ID,
		localUser.Username,
		false,
	)
	require.NoError(t, err)
	require.NotNil(t, createdEnv)
	require.NotEmpty(t, createdToken)
	require.Equal(t, "worker-1", createdEnv.Name)
	require.Equal(t, "agent://swarm-node-node-1234567", createdEnv.ApiUrl)
	require.False(t, createdEnv.Hidden)
	require.True(t, createdEnv.Enabled)
	require.Equal(t, string(EnvironmentStatusPending), createdEnv.Status)
	require.NotNil(t, createdEnv.ParentEnvironmentID)
	require.Equal(t, "manager-env", *createdEnv.ParentEnvironmentID)
	require.NotNil(t, createdEnv.SwarmNodeID)
	require.Equal(t, "node-1234567890abcdef", *createdEnv.SwarmNodeID)
	require.NotNil(t, createdEnv.AccessToken)
	require.Equal(t, createdToken, *createdEnv.AccessToken)
	require.NotNil(t, createdEnv.ApiKeyID)

	var childEnvironments []Environment
	require.NoError(t, db.WithContext(ctx).
		Where("parent_environment_id = ?", "manager-env").
		Where("swarm_node_id = ?", "node-1234567890abcdef").
		Find(&childEnvironments).Error)
	require.Len(t, childEnvironments, 1)

	var apiKeys []apikey.ApiKey
	require.NoError(t, db.WithContext(ctx).
		Where("environment_id = ?", createdEnv.ID).
		Order("created_at asc").
		Find(&apiKeys).Error)
	require.Len(t, apiKeys, 1)
	require.Nil(t, apiKeys[0].UserID) // environment bootstrap keys have no owner

	reusedEnv, reusedToken, err := svc.EnsureSwarmNodeAgentEnvironment(
		ctx,
		"manager-env",
		"node-1234567890abcdef",
		"worker-1",
		localUser.ID,
		localUser.Username,
		false,
	)
	require.NoError(t, err)
	require.NotNil(t, reusedEnv)
	require.Equal(t, createdEnv.ID, reusedEnv.ID)
	require.Equal(t, createdToken, reusedToken)
	require.Equal(t, createdEnv.ApiKeyID, reusedEnv.ApiKeyID)

	var apiKeysAfterReuse []apikey.ApiKey
	require.NoError(t, db.WithContext(ctx).
		Where("environment_id = ?", createdEnv.ID).
		Order("created_at asc").
		Find(&apiKeysAfterReuse).Error)
	require.Len(t, apiKeysAfterReuse, 1)
}

func TestEnvironmentService_EnsureSwarmNodeAgentEnvironment_ReusesLegacyHiddenRegistration(t *testing.T) {
	ctx := t.Context()
	db := setupEnvironmentServiceTestDB(t)
	svc := NewEnvironmentService(db, nil, event.NewEventService(db, nil, nil), nil, nil)
	parentEnvironmentID := "manager-env"
	nodeID := "legacy-node"
	token := "legacy-agent-token"
	legacy := &Environment{
		ID:                  "legacy-agent",
		Name:                "Legacy node agent",
		ApiUrl:              "agent://legacy-node",
		Status:              string(EnvironmentStatusOffline),
		Enabled:             true,
		Hidden:              true,
		AccessToken:         &token,
		ParentEnvironmentID: &parentEnvironmentID,
		SwarmNodeID:         &nodeID,
	}
	{
		err := db.WithContext(ctx).Create(legacy).Error
		require.NoError(t, err,
			"create legacy environment: %v", err)
	}

	reused, reusedToken, err := svc.EnsureSwarmNodeAgentEnvironment(
		ctx,
		parentEnvironmentID,
		nodeID,
		"legacy-host",
		"user-id",
		"username",
		false,
	)

	require.NoError(t, err,
		"EnsureSwarmNodeAgentEnvironment() error = %v", err)

	require.Equal(t, legacy.ID, reused.ID,
		"environment ID = %q, want %q", reused.ID, legacy.ID)

	require.True(t, reused.Hidden,
		"legacy environment was unexpectedly converted to visible")

	require.Equal(t, token, reusedToken,
		"token = %q, want existing token", reusedToken)

	var environments []Environment
	{
		operationErr := db.WithContext(ctx).
			Where("parent_environment_id = ? AND swarm_node_id = ?", parentEnvironmentID, nodeID).
			Find(&environments).Error
		require.NoError(t, operationErr,
			"list node environments: %v", operationErr)
	}

	require.Len(t, environments, 1,
		"environment count = %d, want 1", len(environments))
}

// The provisioned agent token must resolve back to its environment, and rotation
// must retire the old token, with no transformation between issue and validation.
func TestEnvironmentService_EnsureSwarmNodeAgentEnvironment_TokenResolvesEndToEnd(t *testing.T) {
	ctx := t.Context()
	db := setupEnvironmentServiceTestDB(t)
	userService := user.NewUserService(db, nil, session.RevokeAllUserSessionsExceptInDB)
	apiKeyService := apikey.NewApiKeyService(db, userService, nil)
	svc := NewEnvironmentService(db, nil, event.NewEventService(db, nil, nil), nil, apiKeyService)
	localUser := createTestEnvironmentServiceUser(t, ctx, userService, "swarm-resolve-admin")

	createdEnv, createdToken, err := svc.EnsureSwarmNodeAgentEnvironment(
		ctx,
		"manager-env-resolve",
		"node-resolve-1234567890",
		"resolve-host",
		localUser.ID,
		localUser.Username,
		false,
	)
	require.NoError(t, err)
	require.NotEmpty(t, createdToken)

	resolvedID, err := svc.ResolveAgentEnvironmentByToken(ctx, createdToken)
	require.NoError(t, err)
	require.Equal(t, createdEnv.ID, resolvedID)

	// Whitespace normalization on the wire (e.g. a proxy adding a trailing
	// newline) must still resolve.
	resolvedID, err = svc.ResolveAgentEnvironmentByToken(ctx, "  "+createdToken+"\n")
	require.NoError(t, err)
	require.Equal(t, createdEnv.ID, resolvedID)

	// Rotate the token: the new token must resolve and the old token must not.
	rotatedEnv, rotatedToken, err := svc.EnsureSwarmNodeAgentEnvironment(
		ctx,
		"manager-env-resolve",
		"node-resolve-1234567890",
		"resolve-host",
		localUser.ID,
		localUser.Username,
		true,
	)
	require.NoError(t, err)
	require.Equal(t, createdEnv.ID, rotatedEnv.ID)
	require.NotEqual(t, createdToken, rotatedToken)

	resolvedID, err = svc.ResolveAgentEnvironmentByToken(ctx, rotatedToken)
	require.NoError(t, err)
	require.Equal(t, createdEnv.ID, resolvedID)

	_, err = svc.ResolveAgentEnvironmentByToken(ctx, createdToken)
	require.Error(t, err)
	require.Contains(t, err.Error(), "invalid agent token")
}

func TestEnvironmentService_BindSwarmNodeEnvironment_PreservesVisibleEnvironmentToken(t *testing.T) {
	ctx := t.Context()
	db := setupEnvironmentServiceTestDB(t)
	token := "existing-agent-token"
	createTestEnvironment(t, db, "visible-env", "http://visible.example", &token)
	svc := NewEnvironmentService(db, nil, event.NewEventService(db, nil, nil), nil, nil)

	bound, err := svc.BindSwarmNodeEnvironment(ctx, "manager-env", "node-1", "visible-env", false)
	require.NoError(t, err)
	require.False(t, bound.Hidden)
	require.Equal(t, "manager-env", *bound.ParentEnvironmentID)
	require.Equal(t, "node-1", *bound.SwarmNodeID)
	require.Equal(t, token, *bound.AccessToken)

	require.NoError(t, svc.DetachSwarmNodeEnvironment(ctx, "manager-env", "node-1"))
	detached, err := svc.GetEnvironmentByID(ctx, "visible-env")
	require.NoError(t, err)
	require.Nil(t, detached.ParentEnvironmentID)
	require.Nil(t, detached.SwarmNodeID)
	require.Equal(t, token, *detached.AccessToken)
}

func TestEnvironmentService_BindSwarmNodeEnvironment_RequiresExplicitRebind(t *testing.T) {
	ctx := t.Context()
	db := setupEnvironmentServiceTestDB(t)
	createTestEnvironment(t, db, "visible-env", "http://visible.example", nil)
	svc := NewEnvironmentService(db, nil, event.NewEventService(db, nil, nil), nil, nil)

	_, err := svc.BindSwarmNodeEnvironment(ctx, "manager-a", "node-a", "visible-env", false)
	require.NoError(t, err)
	_, err = svc.BindSwarmNodeEnvironment(ctx, "manager-b", "node-b", "visible-env", false)
	require.ErrorContains(t, err, "explicit rebinding is required")

	rebound, err := svc.BindSwarmNodeEnvironment(ctx, "manager-b", "node-b", "visible-env", true)
	require.NoError(t, err)
	require.Equal(t, "manager-b", *rebound.ParentEnvironmentID)
	require.Equal(t, "node-b", *rebound.SwarmNodeID)
}

func TestEnvironmentService_ListMethods_ExcludeHiddenEnvironments(t *testing.T) {
	ctx := t.Context()
	db := setupEnvironmentServiceTestDB(t)
	svc := NewEnvironmentService(db, nil, event.NewEventService(db, nil, nil), nil, nil)

	createTestEnvironment(t, db, "0", "http://localhost:3552", nil)
	createNamedTestEnvironment(t, db, "env-visible", "Visible Remote", "http://visible.example", new("visible-token"))
	createNamedTestEnvironment(t, db, "env-hidden", "Hidden Node Agent", "agent://swarm-node-hidden", new("hidden-token"))

	require.NoError(t, db.WithContext(ctx).
		Model(&Environment{}).
		Where("id = ?", "env-hidden").
		Updates(map[string]any{
			"hidden":                true,
			"parent_environment_id": "0",
			"swarm_node_id":         "node-hidden",
		}).Error)

	listedEnvironments, _, err := svc.ListEnvironmentsPaginated(ctx, pagination.QueryParams{
		Start: 0, Limit: 20,
		Filters: map[string]string{},
	}, nil)
	require.NoError(t, err)
	require.Len(t, listedEnvironments, 2)
	for _, env := range listedEnvironments {
		require.NotEqual(t, "env-hidden", env.ID)
	}

	remoteEnvironments, err := svc.ListRemoteEnvironments(ctx)
	require.NoError(t, err)
	require.Len(t, remoteEnvironments, 1)
	require.Equal(t, "env-visible", remoteEnvironments[0].ID)
}

func TestEnvironmentService_ListEnvironmentsPaginated_FiltersByAccessibleEnvIDs(t *testing.T) {
	ctx := t.Context()
	db := setupEnvironmentServiceTestDB(t)
	svc := NewEnvironmentService(db, nil, event.NewEventService(db, nil, nil), nil, nil)

	createTestEnvironment(t, db, "0", "http://localhost:3552", nil)
	createNamedTestEnvironment(t, db, "env-a", "Env A", "agent://a", new("token-a"))
	createNamedTestEnvironment(t, db, "env-b", "Env B", "agent://b", new("token-b"))

	newParams := func(typeFilter string) pagination.QueryParams {
		filters := map[string]string{}
		filters["type"] = cmp.Or(typeFilter, filters["type"])
		return pagination.QueryParams{
			Start: 0, Limit: 20,
			Filters: filters,
		}
	}

	// nil = no restriction: every non-hidden environment is returned.
	all, _, err := svc.ListEnvironmentsPaginated(ctx, newParams(""), nil)
	require.NoError(t, err)
	require.ElementsMatch(t, []string{"0", "env-a", "env-b"}, environmentIDs(all))

	// A non-nil allow-list restricts the result on the DB path.
	scoped, _, err := svc.ListEnvironmentsPaginated(ctx, newParams(""), []string{"env-a"})
	require.NoError(t, err)
	require.ElementsMatch(t, []string{"env-a"}, environmentIDs(scoped))

	// The runtime-filter path (type filter) honors the allow-list too.
	scopedTyped, _, err := svc.ListEnvironmentsPaginated(ctx, newParams("agent"), []string{"env-a"})
	require.NoError(t, err)
	require.ElementsMatch(t, []string{"env-a"}, environmentIDs(scopedTyped))

	// An empty (non-nil) allow-list yields no environments on either path.
	none, _, err := svc.ListEnvironmentsPaginated(ctx, newParams(""), []string{})
	require.NoError(t, err)
	require.Empty(t, none)

	noneTyped, _, err := svc.ListEnvironmentsPaginated(ctx, newParams("agent"), []string{})
	require.NoError(t, err)
	require.Empty(t, noneTyped)
}

func TestEnvironmentService_ListEnvironmentsPaginated_FiltersByRuntimeType(t *testing.T) {
	ctx := t.Context()
	db := setupEnvironmentServiceTestDB(t)
	svc := NewEnvironmentService(db, nil, event.NewEventService(db, nil, nil), nil, nil)

	now := time.Now()
	envs := []Environment{
		{
			ID: "0", CreatedAt: now, UpdatedAt: &now,
			Name:    "Local",
			ApiUrl:  "http://localhost:3552",
			Status:  string(EnvironmentStatusOnline),
			Enabled: true,
		},
		{
			ID: "env-agent", CreatedAt: now, UpdatedAt: &now,
			Name:    "Agent",
			ApiUrl:  "agent://agent",
			Status:  string(EnvironmentStatusOffline),
			Enabled: true,
		},
		{
			ID: "env-grpc", CreatedAt: now, UpdatedAt: &now,
			Name:    "gRPC",
			ApiUrl:  "agent://grpc",
			Status:  string(EnvironmentStatusOffline),
			Enabled: true,
		},
		{
			ID: "env-websocket", CreatedAt: now, UpdatedAt: &now,
			Name:    "WebSocket",
			ApiUrl:  "agent://websocket",
			Status:  string(EnvironmentStatusOffline),
			Enabled: true,
		},
		{
			ID: "env-last-ws", CreatedAt: now, UpdatedAt: &now,
			Name:               "LastWebsocket",
			ApiUrl:             "agent://last-ws",
			Status:             string(EnvironmentStatusOffline),
			Enabled:            true,
			LastAgentTransport: new("websocket"),
		},
	}
	require.NoError(t, db.WithContext(ctx).Create(&envs).Error)

	agent.GetRegistry().Unregister("env-grpc")
	agent.GetRegistry().Unregister("env-websocket")
	t.Cleanup(func() {
		agent.GetRegistry().Unregister("env-grpc")
		agent.GetRegistry().Unregister("env-websocket")
	})

	grpcTunnel := agent.NewAgentTunnelWithConn("env-grpc", agent.NewGRPCManagerTunnelConn(nil))
	agent.GetRegistry().Register("env-grpc", grpcTunnel)

	websocketTunnel, closeWebSocketTunnel := newTestWebSocketTunnel(t, "env-websocket")
	defer closeWebSocketTunnel()
	agent.GetRegistry().Register("env-websocket", websocketTunnel)

	tests := []struct {
		name       string
		typeFilter string
		wantIDs    []string
	}{
		{name: "agent", typeFilter: "agent", wantIDs: []string{"env-agent"}},
		{name: "grpc", typeFilter: "grpc", wantIDs: []string{"env-grpc"}},
		// Disconnected agents classify by the transport they last used.
		{name: "websocket", typeFilter: "websocket", wantIDs: []string{"env-websocket", "env-last-ws"}},
		{name: "multiple", typeFilter: "agent,grpc", wantIDs: []string{"env-agent", "env-grpc"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			listedEnvironments, _, err := svc.ListEnvironmentsPaginated(ctx, pagination.QueryParams{
				Start: 0, Limit: 20,
				Filters: map[string]string{"type": tt.typeFilter},
			}, nil)
			require.NoError(t, err)
			require.ElementsMatch(t, tt.wantIDs, environmentIDs(listedEnvironments))
		})
	}
}

func environmentIDs(environments []types.Environment) []string {
	ids := make([]string, 0, len(environments))
	for _, env := range environments {
		ids = append(ids, env.ID)
	}
	return ids
}

func newTestWebSocketTunnel(t *testing.T, envID string) (*agent.AgentTunnel, func()) {
	t.Helper()

	connCh := make(chan *websocket.Conn, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := websocket.Accept(w, r, nil)
		if !assert.NoError(t, err) {
			return
		}
		connCh <- conn
	}))

	wsURL := "ws" + strings.TrimPrefix(server.URL, "http")
	clientConn, _, err := websocket.Dial(t.Context(), wsURL, nil)
	require.NoError(t, err)

	serverConn := <-connCh
	return agent.NewAgentTunnelWithConn(envID, agent.NewTunnelConn(serverConn)), func() {
		_ = clientConn.CloseNow()
		server.Close()
	}
}

func TestEnvironmentService_GenerateAgentDeploymentSnippets_WithAutoGeneratedMTLS(t *testing.T) {
	crypto.InitEncryption(&crypto.Config{
		EncryptionKey: "test-encryption-key-for-testing-32bytes-min",
		Environment:   "test",
	})
	db := setupEnvironmentServiceTestDB(t)
	svc := NewEnvironmentService(db, nil, event.NewEventService(db, nil, nil), nil, nil)

	assetsDir := filepath.Join(t.TempDir(), "agent-mtls")
	deploymentSnippets, err := svc.GenerateAgentDeploymentSnippets(t.Context(), "env-mtls", "https://manager.example.com", "token-789", &agent.Config{
		AgentMTLSMode:      agent.MTLSModeRequired,
		AgentMTLSAssetsDir: assetsDir,
	})
	require.NoError(t, err)
	require.NotNil(t, deploymentSnippets)
	require.NotNil(t, deploymentSnippets.MTLS)
	require.Contains(t, deploymentSnippets.MTLS.DockerRun, "ARCANE_AGENT_MTLS_MODE=required")
	require.Contains(t, deploymentSnippets.MTLS.DockerRun, "ARCANE_AGENT_MTLS_ASSETS_DIR=/app/data/agent-mtls-client")
	require.NotContains(t, deploymentSnippets.MTLS.DockerRun, "ARCANE_AGENT_MTLS_CA_FILE")
	require.NotContains(t, deploymentSnippets.MTLS.DockerRun, "ARCANE_AGENT_MTLS_CERT_FILE")
	require.NotContains(t, deploymentSnippets.MTLS.DockerRun, "ARCANE_AGENT_MTLS_KEY_FILE")
	require.NotContains(t, deploymentSnippets.MTLS.DockerRun, "./arcane-agent-certs:/app/data/agent-mtls-client:ro")
	require.Contains(t, deploymentSnippets.MTLS.DockerCompose, "ARCANE_AGENT_MTLS_ASSETS_DIR=/app/data/agent-mtls-client")
	require.NotContains(t, deploymentSnippets.MTLS.DockerCompose, "ARCANE_AGENT_MTLS_CA_FILE")
	require.NotContains(t, deploymentSnippets.MTLS.DockerCompose, "ARCANE_AGENT_MTLS_CERT_FILE")
	require.NotContains(t, deploymentSnippets.MTLS.DockerCompose, "ARCANE_AGENT_MTLS_KEY_FILE")
	require.NotContains(t, deploymentSnippets.MTLS.DockerCompose, "./arcane-agent-certs:/app/data/agent-mtls-client:ro")
	require.Equal(t, "./arcane-agent-certs", deploymentSnippets.MTLS.HostDirHint)
	require.Len(t, deploymentSnippets.MTLS.Files, 3)
	require.Equal(t, "ca.crt", deploymentSnippets.MTLS.Files[0].Name)
	require.Equal(t, "/app/data/agent-mtls-client/ca.crt", deploymentSnippets.MTLS.Files[0].ContainerPath)
	require.Contains(t, deploymentSnippets.MTLS.Files[0].Content, "BEGIN CERTIFICATE")
	require.Equal(t, "agent.key", deploymentSnippets.MTLS.Files[2].Name)
	require.Equal(t, "/app/data/agent-mtls-client/agent.key", deploymentSnippets.MTLS.Files[2].ContainerPath)
	require.Contains(t, deploymentSnippets.MTLS.Files[2].Content, "BEGIN PRIVATE KEY")
}

func TestEnvironmentService_GenerateAgentDeploymentSnippets_ReturnsBasicSnippetsWhenMTLSGenerationFails(t *testing.T) {
	db := setupEnvironmentServiceTestDB(t)
	svc := NewEnvironmentService(db, nil, event.NewEventService(db, nil, nil), nil, nil)

	assetsPath := filepath.Join(t.TempDir(), "agent-mtls-file")
	require.NoError(t, os.WriteFile(assetsPath, []byte("not a directory"), 0o600))

	deploymentSnippets, err := svc.GenerateAgentDeploymentSnippets(t.Context(), "env-mtls", "https://manager.example.com", "token-789", &agent.Config{
		AgentMTLSMode:      agent.MTLSModeRequired,
		AgentMTLSAssetsDir: assetsPath,
	})

	require.NoError(t, err)
	require.NotNil(t, deploymentSnippets)
	require.Nil(t, deploymentSnippets.MTLS)
	require.Contains(t, deploymentSnippets.DockerRun, "ARCANE_AGENT_TRANSPORT=auto")
	require.Contains(t, deploymentSnippets.DockerCompose, "ARCANE_MANAGER_API_URL=https://manager.example.com")
}

func TestEnvironmentServiceTestConnectionProbesThroughTunnel(t *testing.T) {
	ctx := t.Context()
	db := setupEnvironmentServiceTestDB(t)
	svc := NewEnvironmentService(db, nil, event.NewEventService(db, nil, nil), nil, nil)
	createTestEnvironment(t, db, "env-tunnel", startTestAgent(t, "env-tunnel", http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})), nil)

	status, err := svc.TestConnection(ctx, "env-tunnel")
	require.NoError(t, err)
	require.Equal(t, "online", status)
}

func (s *environmentTestScheduler) Submit(_ context.Context, request types.SchedulerRequest) (types.SchedulerRun, error) {
	s.submitted = append(s.submitted, request)
	return types.SchedulerRun{
		ID:            request.RunID,
		JobID:         request.JobID,
		EnvironmentID: request.EnvironmentID,
		Status:        types.RunStatusQueued,
	}, nil
}

func setupSyncDB(t *testing.T) *database.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "sync.db")), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(
		t,
		db.AutoMigrate(
			&Environment{},
			&registry.ContainerRegistry{},
			&gitrepo.GitRepository{},
			&s3.S3Destination{},
		),
	)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, sqlDB.Close()) })
	crypto.InitEncryption(&crypto.Config{EncryptionKey: "test-encryption-key-for-testing-32bytes-min", Environment: "test"})
	return &database.DB{DB: db}
}

func TestSyncCredentialDecryptionAbortsSnapshot(t *testing.T) {
	for _, kind := range []string{"registry token", "ECR secret", "repository token", "repository SSH key", "S3 secret"} {
		t.Run(kind, func(t *testing.T) {
			db := setupSyncDB(t)
			var calls atomic.Int32
			agentHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				_, _ = w.Write([]byte(`{"success":true}`))
			})
			require.NoError(t, db.Create(&Environment{
				ID:          "remote",
				ApiUrl:      startTestAgent(t, "remote", agentHandler),
				AccessToken: new("agent-token"),
			}).Error)
			service := NewEnvironmentService(db, nil, event.NewEventService(db, nil, nil), nil, nil)
			var syncResource func(context.Context, string) error
			switch kind {
			case "registry token", "ECR secret":
				require.NoError(t, db.Create(&registry.ContainerRegistry{
					ID:                 "bad-credential",
					URL:                "registry.example.com",
					Token:              "invalid-ciphertext",
					RegistryType:       kit.Ternary(kind == "ECR secret", registry.RegistryTypeECR, registry.RegistryTypeGeneric),
					AWSSecretAccessKey: kit.Ternary(kind == "ECR secret", "invalid-ciphertext", ""),
				}).Error)
				syncResource = service.SyncRegistriesToEnvironment
			case "repository token", "repository SSH key":
				require.NoError(t, db.Create(&gitrepo.GitRepository{
					ID:     "bad-credential",
					Name:   "Repository",
					URL:    "https://example.com/repo.git",
					Token:  kit.Ternary(kind == "repository token", "invalid-ciphertext", ""),
					SSHKey: kit.Ternary(kind == "repository token", "", "invalid-ciphertext"),
				}).Error)
				syncResource = service.SyncRepositoriesToEnvironment
			case "S3 secret":
				require.NoError(t, db.Create(&s3.S3Destination{
					ID:              "bad-credential",
					Name:            "Backup",
					SecretAccessKey: "invalid-ciphertext",
				}).Error)
				syncResource = service.SyncS3DestinationsToEnvironment
			}
			err := syncResource(t.Context(), "remote")
			require.Error(t, err)
			require.Contains(t, err.Error(), "bad-credential")
			require.Error(t, errors.Unwrap(err))
			require.Zero(t, calls.Load(), "an incomplete snapshot must never reach the agent")
		})
	}
}

func TestSyncRepositoriesPreservesEmptyCredentials(t *testing.T) {
	db := setupSyncDB(t)
	require.NoError(t, db.Create(&gitrepo.GitRepository{
		ID:       "public-repo",
		Name:     "Public",
		URL:      "https://example.com/public.git",
		AuthType: "none",
	}).Error)
	agentHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var payload types.GitRepositorySyncRequest
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Error(err)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		if rows := payload.Repositories; len(rows) != 1 || rows[0].ID != "public-repo" {
			t.Errorf("missing public repository: %v", payload)
		}
		_, _ = w.Write([]byte(`{"success":true}`))
	})
	require.NoError(t, db.Create(&Environment{
		ID:          "remote",
		ApiUrl:      startTestAgent(t, "remote", agentHandler),
		AccessToken: new("token"),
	}).Error)
	service := NewEnvironmentService(db, nil, event.NewEventService(db, nil, nil), nil, nil)
	require.NoError(t, service.SyncRepositoriesToEnvironment(t.Context(), "remote"))
}

func TestSyncSkipsUnchangedPayloadUntilForgotten(t *testing.T) {
	db := setupSyncDB(t)
	var calls atomic.Int32
	var reject atomic.Bool
	agentHandler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		if reject.Load() {
			http.Error(w, "agent unavailable", http.StatusInternalServerError)
			return
		}
		// A write failure only means the manager already gave up on this
		// request, which the call-count assertions below would surface.
		_, _ = w.Write([]byte(`{"success":true}`))
	})
	token, err := crypto.Encrypt("secret")
	require.NoError(t, err)
	require.NoError(t, db.Create(&registry.ContainerRegistry{
		ID:           "ghcr",
		URL:          "ghcr.io",
		Username:     "user",
		Token:        token,
		RegistryType: registry.RegistryTypeGeneric,
	}).Error)
	require.NoError(t, db.Create(&Environment{
		ID:          "remote",
		ApiUrl:      startTestAgent(t, "remote", agentHandler),
		AccessToken: new("agent-token"),
	}).Error)
	service := NewEnvironmentService(db, nil, event.NewEventService(db, nil, nil), nil, nil)
	localSync := func() error { return service.SyncRegistriesToEnvironment(t.Context(), "remote") }
	changeRegistry := func(url string) {
		require.NoError(t, db.Model(&registry.ContainerRegistry{}).Where("id = ?", "ghcr").Update("url", url).Error)
	}

	require.NoError(t, localSync())
	require.NoError(t, localSync())
	require.EqualValues(t, 1, calls.Load(), "an unchanged payload is not resent")

	changeRegistry("ghcr.io/v2")
	require.NoError(t, localSync())
	require.EqualValues(t, 2, calls.Load(), "a changed payload is resent")

	service.ForgetSyncState("remote")
	require.NoError(t, localSync())
	require.EqualValues(t, 3, calls.Load(), "forgetting forces a resend")

	reject.Store(true)
	changeRegistry("ghcr.io/v3")
	require.Error(t, localSync())
	require.EqualValues(t, 4, calls.Load())
	reject.Store(false)
	require.NoError(t, localSync())
	require.EqualValues(t, 5, calls.Load(), "a rejected payload is retried")
	require.NoError(t, localSync())
	require.EqualValues(t, 5, calls.Load(), "an accepted payload is remembered")
}

// A watcher must see a change signal, and a burst must not block the notifier —
// it runs on the agent tunnel's connect/disconnect path.
func TestEnvironmentServiceRuntimeChangeSignalCoalesces(t *testing.T) {
	s := NewEnvironmentService(nil, nil, nil, nil, nil)

	changes, unsubscribe := s.SubscribeRuntimeChanges()
	defer unsubscribe()

	// More notifications than the channel can hold: the extras are dropped
	// rather than blocking, because a watcher re-derives live state on wake.
	for range 5 {
		s.NotifyRuntimeStateChanged()
	}

	select {
	case <-changes:
	default:
		require.FailNow(t, "expected a pending runtime change signal")
	}

	select {
	case <-changes:
		require.FailNow(t, "expected coalesced signals, got a second wake-up")
	default:
	}

	// Unsubscribing must stop delivery so a closed stream cannot leak.
	unsubscribe()
	s.NotifyRuntimeStateChanged()
	select {
	case <-changes:
		require.FailNow(t, "expected no signal after unsubscribe")
	default:
	}

	require.Empty(t, s.runtimeWatchers.chans)
}

func environmentScopeContext(t *testing.T, target string, bodies map[string]any) context.Context {
	t.Helper()
	return httpx.WithEnvironmentScope(t.Context(), &httpx.EnvironmentScope{
		IDs:     []string{"a", "b", "c"},
		Request: httptest.NewRequest(http.MethodGet, target, http.NoBody),
		Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			body, ok := bodies[strings.Split(strings.TrimPrefix(r.URL.Path, "/api/environments/"), "/")[0]]
			if !ok {
				w.WriteHeader(http.StatusBadGateway)
				_, _ = w.Write([]byte(`{"detail":"environment offline"}`))
				return
			}
			_ = json.NewEncoder(w).Encode(body)
		}),
		Resolve: func(_ context.Context, id string) (types.EnvironmentRef, error) {
			return types.EnvironmentRef{ID: id, Name: "Env " + id}, nil
		},
	})
}

func TestAggregateVersionsReportsEachEnvironment(t *testing.T) {
	ctx := environmentScopeContext(t, "/api/environments/a,b,c/version", map[string]any{
		"a": types.ApiResponse[types.VersionInfo]{
			Success: true,
			Data:    types.VersionInfo{CurrentVersion: "2.0.0"},
		},
		"b": types.ApiResponse[types.VersionInfo]{
			Success: true,
			Data:    types.VersionInfo{CurrentVersion: "1.9.0"},
		},
	})

	body, err := (&EnvironmentService{}).AggregateVersions(ctx)
	require.NoError(t, err)

	require.Equal(t, []types.EnvironmentData[types.VersionInfo]{
		{Environment: types.EnvironmentRef{ID: "a", Name: "Env a"}, Data: types.VersionInfo{CurrentVersion: "2.0.0"}},
		{Environment: types.EnvironmentRef{ID: "b", Name: "Env b"}, Data: types.VersionInfo{CurrentVersion: "1.9.0"}},
	}, body.Environments)
	require.Len(t, body.FailedEnvironments, 1)
}
