package environment

import (
	"context"
	"encoding/json/v2"
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

	"github.com/labstack/echo/v5"
	"github.com/libtnb/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"kit.getarcane.app"
	"kit.getarcane.app/cache"
	"kit.getarcane.app/sys/crypto"

	"getarcane.dev/arcane/v3/internal/apikey"
	"getarcane.dev/arcane/v3/internal/common"
	"getarcane.dev/arcane/v3/internal/config"
	"getarcane.dev/arcane/v3/internal/database"
	"getarcane.dev/arcane/v3/internal/environment/children/enrollment"
	"getarcane.dev/arcane/v3/internal/event"
	"getarcane.dev/arcane/v3/internal/gitrepo"
	"getarcane.dev/arcane/v3/internal/registry"
	"getarcane.dev/arcane/v3/internal/role"
	"getarcane.dev/arcane/v3/internal/s3"
	"getarcane.dev/arcane/v3/internal/scheduler/entityjobs"
	"getarcane.dev/arcane/v3/internal/scheduler/runs"
	"getarcane.dev/arcane/v3/internal/session"
	"getarcane.dev/arcane/v3/internal/settings"
	"getarcane.dev/arcane/v3/internal/user"
	"getarcane.dev/arcane/v3/pkg/agent"
	francistest "getarcane.dev/arcane/v3/pkg/francis/testing"
	"getarcane.dev/arcane/v3/pkg/httpx"
	"getarcane.dev/arcane/v3/pkg/pagination"
	"getarcane.dev/types/v3"
)

type (
	environmentTestVariableSyncer struct{}

	environmentTestScheduler struct {
		submitted []types.SchedulerRequest
		added     []string
		removed   []string
	}

	// Project imports environment, so these rows avoid an import cycle in tests.
	testProjectRow struct {
		database.BaseModel
		GitOpsManagedBy *string `gorm:"column:gitops_managed_by"`
		Name            string
		Path            string
		Status          string
	}

	testGitOpsSyncRow struct {
		database.BaseModel
		Name          string
		EnvironmentID string
		RepositoryID  string
		ComposePath   string
		ProjectName   string
		SyncInterval  int
	}

	testFederatedCredentialRow struct {
		database.BaseModel
		EnvironmentID  *string `gorm:"column:environment_id"`
		Name           string
		IdentityUserID string `gorm:"column:identity_user_id"`
	}

	testGlobalVariableRow struct {
		database.BaseModel
		Key             string
		AllEnvironments bool `gorm:"column:all_environments"`
	}

	testGlobalVariableEnvironmentRow struct {
		GlobalVariableID string `gorm:"column:global_variable_id;primaryKey"`
		EnvironmentID    string `gorm:"column:environment_id;primaryKey"`
	}
)

func (testFederatedCredentialRow) TableName() string { return "federated_credentials" }

func (testGlobalVariableRow) TableName() string { return "global_variables" }

func (testGlobalVariableEnvironmentRow) TableName() string { return "global_variable_environments" }

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
		&enrollment.Token{},
		&registry.ContainerRegistry{},
		&s3.S3Destination{},
		&settings.SettingVariable{},
		&user.User{},
		&apikey.ApiKey{},
		&role.Role{},
		&role.UserRoleAssignment{},
		&role.ApiKeyPermission{},
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
	crypto.InitEncryption(crypto.Config{
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

func startTestAgent(t *testing.T, tunnels *agent.TunnelRegistry, envID string, handler http.Handler) string {
	t.Helper()

	manager := echo.New()
	manager.GET("/api/tunnel/connect", agent.NewTunnelServer(agent.TunnelServerOptions{
		StreamWindowMiB: func() int {
			return 16
		},
		Registry: tunnels,
		Resolver: func(context.Context, string) (string, error) {
			return envID, nil
		},
		MTLS: agent.NewMTLS(agent.MTLSConfig{}),
	}).HandleConnect)
	server := httptest.NewServer(manager)
	t.Cleanup(server.Close)

	agentCtx, cancel := context.WithCancel(context.WithoutCancel(t.Context()))
	t.Cleanup(func() {
		cancel()
		tunnels.Unregister(envID)
	})
	_, startErr := agent.NewTunnelClient(agent.TunnelClientConfig{
		StreamWindowMiB: func() int {
			return 16
		},
		Config: &agent.Config{
			MTLS:           agent.NewMTLS(agent.MTLSConfig{ManagerURL: server.URL}),
			AgentEnabled:   true,
			AgentTransport: agent.TransportWebSocket,
			AgentToken:     "agent-" + envID,
		},
		Handler: handler,
	}).Start(agentCtx)
	require.NoError(t, startErr)
	require.True(t, tunnels.WaitForActive(t.Context(), envID, agent.WaitForActiveOptions{Timeout: 5 * time.Second}).IsPresent())

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
	tunnels := agent.NewTunnelRegistry()
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
	svc := NewEnvironmentService(EnvironmentServiceParams{
		Tunnels:         tunnels,
		DB:              db,
		RemoteTransport: NewRemoteTransport(db, nil, tunnels),
		EventService:    event.NewEventService(db, &config.Config{}, nil),
		VariableSyncer:  &environmentTestVariableSyncer{},
		InvalidateUser:  func(string) {},
		Config:          &config.Config{},
		Ctx:             ctx,
		Scheduler:       jobScheduler,
		Admission:       newAdmissionGateForEnvironmentTest(t),
	})

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
	tunnels := agent.NewTunnelRegistry()
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

	var invalidatedUserIDs []string
	svc := NewEnvironmentService(EnvironmentServiceParams{
		Tunnels:         tunnels,
		DB:              db,
		RemoteTransport: NewRemoteTransport(db, nil, tunnels),
		EventService:    event.NewEventService(db, &config.Config{}, nil),
		VariableSyncer:  &environmentTestVariableSyncer{},
		InvalidateUser: func(userID string) {
			invalidatedUserIDs = append(invalidatedUserIDs, userID)
		},
		Config:    &config.Config{},
		Ctx:       t.Context(),
		Scheduler: &environmentTestScheduler{},
	})
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
	tunnels := agent.NewTunnelRegistry()
	ctx := t.Context()
	db := setupEnvironmentServiceTestDB(t)
	createTestEnvironmentWithState(t, db, "env-known", "http://known.example", string(EnvironmentStatusOnline), new("seeded-token"))
	createTestEnvironmentWithState(t, db, "env-shared", "http://shared.example", string(EnvironmentStatusOnline), new("seeded-token"))
	svc := NewEnvironmentService(EnvironmentServiceParams{
		Tunnels:         tunnels,
		DB:              db,
		RemoteTransport: NewRemoteTransport(db, nil, tunnels),
		EventService:    event.NewEventService(db, &config.Config{}, nil),
		VariableSyncer:  &environmentTestVariableSyncer{},
		Config:          &config.Config{},
		Ctx:             t.Context(),
		Scheduler:       &environmentTestScheduler{},
	})

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
	tunnels := agent.NewTunnelRegistry()
	ctx := t.Context()
	db := setupEnvironmentServiceTestDB(t)
	svc := NewEnvironmentService(EnvironmentServiceParams{
		Tunnels:         tunnels,
		VariableSyncer:  &environmentTestVariableSyncer{},
		DB:              db,
		RemoteTransport: NewRemoteTransport(db, nil, tunnels),
		EventService:    event.NewEventService(db, &config.Config{}, nil),
		Config:          &config.Config{},
		Ctx:             t.Context(),
		Scheduler:       &environmentTestScheduler{},
	})

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
		if !assert.NoError(t, json.UnmarshalRead(r.Body, &syncReq)) {
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
		if !assert.NoError(t, json.UnmarshalRead(r.Body, &syncReq)) {
			return
		}
		if !assert.Len(t, syncReq.Registries, 1) {
			return
		}
		env2Calls.Add(1)

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"success":true,"data":{"message":"ok"}}`))
	})

	createTestEnvironment(t, db, "0", "http://localhost:3552", nil)
	createTestEnvironment(t, db, "env-1", startTestAgent(t, tunnels, "env-1", env1Agent), &env1Token)
	createTestEnvironment(t, db, "env-2", startTestAgent(t, tunnels, "env-2", env2Agent), &env2Token)

	err := svc.SyncToRemoteEnvironments(ctx, "registries", svc.SyncRegistriesToEnvironment)
	require.NoError(t, err)
	require.EqualValues(t, 1, env1Calls.Load())
	require.EqualValues(t, 1, env2Calls.Load())
}

func TestEnvironmentService_SyncRegistriesToEnvironment_IncludesECRFields(t *testing.T) {
	tunnels := agent.NewTunnelRegistry()
	ctx := t.Context()
	db := setupEnvironmentServiceTestDB(t)
	svc := NewEnvironmentService(EnvironmentServiceParams{
		Tunnels:         tunnels,
		VariableSyncer:  &environmentTestVariableSyncer{},
		DB:              db,
		RemoteTransport: NewRemoteTransport(db, nil, tunnels),
		EventService:    event.NewEventService(db, &config.Config{}, nil),
		Config:          &config.Config{},
		Ctx:             t.Context(),
		Scheduler:       &environmentTestScheduler{},
	})

	createTestECRRegistry(t, db, "reg-ecr")

	agentHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !assert.Equal(t, http.MethodPost, r.Method) {
			return
		}
		if !assert.Equal(t, "/api/registries/sync", r.URL.Path) {
			return
		}

		var syncReq types.ContainerRegistrySyncRequest
		if !assert.NoError(t, json.UnmarshalRead(r.Body, &syncReq)) {
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

	createTestEnvironment(t, db, "env-1", startTestAgent(t, tunnels, "env-1", agentHandler), new("token-1"))

	err := svc.SyncRegistriesToEnvironment(ctx, "env-1")
	require.NoError(t, err)
}

func TestEnvironmentService_SyncS3DestinationsToEnvironment(t *testing.T) {
	tunnels := agent.NewTunnelRegistry()
	ctx := t.Context()
	db := setupEnvironmentServiceTestDB(t)
	svc := NewEnvironmentService(EnvironmentServiceParams{
		Tunnels:         tunnels,
		VariableSyncer:  &environmentTestVariableSyncer{},
		DB:              db,
		RemoteTransport: NewRemoteTransport(db, nil, tunnels),
		EventService:    event.NewEventService(db, &config.Config{}, nil),
		Config:          &config.Config{},
		Ctx:             t.Context(),
		Scheduler:       &environmentTestScheduler{},
	})

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
		if !assert.NoError(t, json.UnmarshalRead(r.Body, &syncReq)) {
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

	createTestEnvironment(t, db, "env-s3", startTestAgent(t, tunnels, "env-s3", agentHandler), &accessToken)
	require.NoError(t, svc.SyncS3DestinationsToEnvironment(ctx, "env-s3"))
}

func TestEnvironmentService_SyncRepositoriesToEnvironment_UsesAgentHeaders(t *testing.T) {
	tunnels := agent.NewTunnelRegistry()
	ctx := t.Context()
	db := setupEnvironmentServiceTestDB(t)
	require.NoError(t, db.AutoMigrate(&gitrepo.GitRepository{}))
	svc := NewEnvironmentService(EnvironmentServiceParams{
		Tunnels:         tunnels,
		VariableSyncer:  &environmentTestVariableSyncer{},
		DB:              db,
		RemoteTransport: NewRemoteTransport(db, nil, tunnels),
		EventService:    event.NewEventService(db, &config.Config{}, nil),
		Config:          &config.Config{},
		Ctx:             t.Context(),
		Scheduler:       &environmentTestScheduler{},
	})

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
		if !assert.NoError(t, json.UnmarshalRead(r.Body, &syncReq)) {
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

	createTestEnvironment(t, db, "env-1", startTestAgent(t, tunnels, "env-1", agentHandler), &accessToken)

	err := svc.SyncRepositoriesToEnvironment(ctx, "env-1")
	require.NoError(t, err)
}

func TestEnvironmentService_SyncRegistriesToRemoteEnvironments_SkipsRemoteWithoutAccessToken(t *testing.T) {
	tunnels := agent.NewTunnelRegistry()
	ctx := t.Context()
	db := setupEnvironmentServiceTestDB(t)
	svc := NewEnvironmentService(EnvironmentServiceParams{
		Tunnels:         tunnels,
		VariableSyncer:  &environmentTestVariableSyncer{},
		DB:              db,
		RemoteTransport: NewRemoteTransport(db, nil, tunnels),
		EventService:    event.NewEventService(db, &config.Config{}, nil),
		Config:          &config.Config{},
		Ctx:             t.Context(),
		Scheduler:       &environmentTestScheduler{},
	})

	createTestRegistry(t, db, "reg-1")

	var syncCalls atomic.Int32
	agentHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		syncCalls.Add(1)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"success":true,"data":{"message":"ok"}}`))
	})

	createTestEnvironment(t, db, "env-auth", startTestAgent(t, tunnels, "env-auth", agentHandler), new("token-with-auth"))
	createTestEnvironment(t, db, "env-no-token", "agent://env-no-token", nil)

	err := svc.SyncToRemoteEnvironments(ctx, "registries", svc.SyncRegistriesToEnvironment)
	require.NoError(t, err)
	require.EqualValues(t, 1, syncCalls.Load())
}

func TestEnvironmentService_SyncRegistriesToRemoteEnvironments_ReportsFailuresButContinues(t *testing.T) {
	tunnels := agent.NewTunnelRegistry()
	ctx := t.Context()
	db := setupEnvironmentServiceTestDB(t)
	svc := NewEnvironmentService(EnvironmentServiceParams{
		Tunnels:         tunnels,
		VariableSyncer:  &environmentTestVariableSyncer{},
		DB:              db,
		RemoteTransport: NewRemoteTransport(db, nil, tunnels),
		EventService:    event.NewEventService(db, &config.Config{}, nil),
		Config:          &config.Config{},
		Ctx:             t.Context(),
		Scheduler:       &environmentTestScheduler{},
	})

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

	createTestEnvironment(t, db, "env-success", startTestAgent(t, tunnels, "env-success", successAgent), new("token-success"))
	createTestEnvironment(t, db, "env-fail", startTestAgent(t, tunnels, "env-fail", failingAgent), new("token-fail"))

	err := svc.SyncToRemoteEnvironments(ctx, "registries", svc.SyncRegistriesToEnvironment)
	require.Error(t, err)
	require.Contains(t, err.Error(), "failed to sync registries to 1 remote environment")
	require.EqualValues(t, 1, successCalls.Load())
}

func TestEnvironmentService_ReconcileAgentStatusesOnStartup(t *testing.T) {
	tunnels := agent.NewTunnelRegistry()
	ctx := t.Context()
	db := setupEnvironmentServiceTestDB(t)
	svc := NewEnvironmentService(EnvironmentServiceParams{
		Tunnels:         tunnels,
		VariableSyncer:  &environmentTestVariableSyncer{},
		DB:              db,
		RemoteTransport: NewRemoteTransport(db, nil, tunnels),
		EventService:    event.NewEventService(db, &config.Config{}, nil),
		Config:          &config.Config{},
		Ctx:             t.Context(),
		Scheduler:       &environmentTestScheduler{},
	})

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
	tunnels := agent.NewTunnelRegistry()
	ctx := t.Context()
	db := setupEnvironmentServiceTestDB(t)
	svc := NewEnvironmentService(EnvironmentServiceParams{
		Tunnels:         tunnels,
		VariableSyncer:  &environmentTestVariableSyncer{},
		DB:              db,
		RemoteTransport: NewRemoteTransport(db, nil, tunnels),
		EventService:    event.NewEventService(db, &config.Config{}, nil),
		Config:          &config.Config{},
		Ctx:             t.Context(),
		Scheduler:       &environmentTestScheduler{},
	})

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
	tunnels := agent.NewTunnelRegistry()
	ctx := t.Context()
	db := setupEnvironmentServiceTestDB(t)
	svc := NewEnvironmentService(EnvironmentServiceParams{
		Tunnels:         tunnels,
		VariableSyncer:  &environmentTestVariableSyncer{},
		DB:              db,
		RemoteTransport: NewRemoteTransport(db, nil, tunnels),
		EventService:    event.NewEventService(db, &config.Config{}, nil),
		Config:          &config.Config{},
		Ctx:             t.Context(),
		Scheduler:       &environmentTestScheduler{},
	})

	createTestEnvironmentWithState(t, db, "agent-pending", "agent://pending", string(EnvironmentStatusPending), nil)

	require.NoError(t, svc.updateEnvironmentStatus(ctx, "agent-pending", string(EnvironmentStatusOffline)))
	require.NoError(t, svc.updateEnvironmentStatus(ctx, "agent-pending", string(EnvironmentStatusError)))

	var env Environment
	require.NoError(t, db.WithContext(ctx).Where("id = ?", "agent-pending").First(&env).Error)
	require.Equal(t, string(EnvironmentStatusPending), env.Status)
	require.Nil(t, env.LastSeen)
}

func TestEnvironmentService_UpdateEnvironmentStatus_LeavesPendingAgentEnvAlone(t *testing.T) {
	tunnels := agent.NewTunnelRegistry()
	ctx := t.Context()
	db := setupEnvironmentServiceTestDB(t)
	svc := NewEnvironmentService(EnvironmentServiceParams{
		Tunnels:         tunnels,
		VariableSyncer:  &environmentTestVariableSyncer{},
		DB:              db,
		RemoteTransport: NewRemoteTransport(db, nil, tunnels),
		EventService:    event.NewEventService(db, &config.Config{}, nil),
		Config:          &config.Config{},
		Ctx:             t.Context(),
		Scheduler:       &environmentTestScheduler{},
	})

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
	tunnels := agent.NewTunnelRegistry()
	ctx := t.Context()
	db := setupEnvironmentServiceTestDB(t)
	svc := NewEnvironmentService(EnvironmentServiceParams{
		Tunnels:         tunnels,
		VariableSyncer:  &environmentTestVariableSyncer{},
		DB:              db,
		RemoteTransport: NewRemoteTransport(db, nil, tunnels),
		EventService:    event.NewEventService(db, &config.Config{}, nil),
		Config:          &config.Config{},
		Ctx:             t.Context(),
		Scheduler:       &environmentTestScheduler{},
	})

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
	tunnels := agent.NewTunnelRegistry()
	ctx := t.Context()
	db := setupEnvironmentServiceTestDB(t)
	svc := NewEnvironmentService(EnvironmentServiceParams{
		Tunnels:         tunnels,
		VariableSyncer:  &environmentTestVariableSyncer{},
		DB:              db,
		RemoteTransport: NewRemoteTransport(db, nil, tunnels),
		EventService:    event.NewEventService(db, &config.Config{}, nil),
		Config:          &config.Config{},
		Ctx:             t.Context(),
		Scheduler:       &environmentTestScheduler{},
	})

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
	tunnels := agent.NewTunnelRegistry()
	db := setupEnvironmentServiceTestDB(t)
	svc := NewEnvironmentService(EnvironmentServiceParams{
		Tunnels:         tunnels,
		VariableSyncer:  &environmentTestVariableSyncer{},
		DB:              db,
		RemoteTransport: NewRemoteTransport(db, nil, tunnels),
		EventService:    event.NewEventService(db, &config.Config{}, nil),
		Config:          &config.Config{},
		Ctx:             t.Context(),
		Scheduler:       &environmentTestScheduler{},
	})
	svc.agentTokens.mu.Lock()
	svc.agentTokens.byEnvID["env-expired"] = "expired-token"
	svc.agentTokens.byToken.Set(cache.Entry[string, string]{
		Key:   "expired-token",
		Value: "env-expired",
		TTL:   time.Nanosecond,
	})
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
	tunnels := agent.NewTunnelRegistry()
	ctx := t.Context()
	db := setupEnvironmentServiceTestDB(t)
	svc := NewEnvironmentService(EnvironmentServiceParams{
		Tunnels:         tunnels,
		VariableSyncer:  &environmentTestVariableSyncer{},
		DB:              db,
		RemoteTransport: NewRemoteTransport(db, nil, tunnels),
		EventService:    event.NewEventService(db, &config.Config{}, nil),
		Config:          &config.Config{},
		Ctx:             t.Context(),
		Scheduler:       &environmentTestScheduler{},
	})

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

func TestEnvironmentService_ResolveEventEnvironmentByAccessToken(t *testing.T) {
	tunnels := agent.NewTunnelRegistry()
	ctx := t.Context()
	db := setupEnvironmentServiceTestDB(t)
	svc := NewEnvironmentService(EnvironmentServiceParams{
		Tunnels:         tunnels,
		VariableSyncer:  &environmentTestVariableSyncer{},
		DB:              db,
		RemoteTransport: NewRemoteTransport(db, nil, tunnels),
		EventService:    event.NewEventService(db, &config.Config{}, nil),
		Config:          &config.Config{},
		Ctx:             t.Context(),
		Scheduler:       &environmentTestScheduler{},
	})

	enabledToken := "enabled-token"
	disabledToken := "disabled-token"
	createNamedTestEnvironment(t, db, "env-enabled", "Enabled", "http://enabled.example", &enabledToken)
	createNamedTestEnvironment(t, db, "env-disabled", "Disabled", "http://disabled.example", &disabledToken)
	require.NoError(t, db.Model(&Environment{}).Where("id = ?", "env-disabled").Update("enabled", false).Error)

	envID, err := svc.ResolveEventEnvironmentByAccessToken(ctx, enabledToken)
	require.NoError(t, err)
	require.Equal(t, "env-enabled", envID)

	envID, err = svc.ResolveEventEnvironmentByAccessToken(ctx, disabledToken)
	require.NoError(t, err)
	require.Empty(t, envID)

	_, err = svc.ResolveEventEnvironmentByAccessToken(ctx, "missing-token")
	require.ErrorIs(t, err, ErrInvalidEnvironmentAccessToken)
}

func TestEnvironmentService_GenerateDeploymentSnippets_TransportDefaults(t *testing.T) {
	tunnels := agent.NewTunnelRegistry()
	db := setupEnvironmentServiceTestDB(t)
	svc := NewEnvironmentService(EnvironmentServiceParams{
		Tunnels:         tunnels,
		VariableSyncer:  &environmentTestVariableSyncer{},
		DB:              db,
		RemoteTransport: NewRemoteTransport(db, nil, tunnels),
		EventService:    event.NewEventService(db, &config.Config{}, nil),
		Config:          &config.Config{},
		Ctx:             t.Context(),
		Scheduler:       &environmentTestScheduler{},
	})

	agentSnippets, err := svc.GenerateAgentDeploymentSnippets(t.Context(), AgentDeploymentSnippetsParams{
		EnvironmentID: "env-2",
		ManagerURL:    "https://manager.example.com",
		Token:         "token-456",
		MTLS:          agent.NewMTLS(agent.MTLSConfig{}),
	})
	require.NoError(t, err)
	require.NotNil(t, agentSnippets)
	require.Contains(t, agentSnippets.DockerRun, "ARCANE_AGENT_TRANSPORT=auto")
	require.Contains(t, agentSnippets.DockerCompose, "ARCANE_AGENT_TRANSPORT=auto")
	require.Contains(t, agentSnippets.DockerRun, "ARCANE_AGENT_TOKEN=token-456")
	require.Contains(t, agentSnippets.DockerRun, "-v arcane-data:/app/data")
	require.Contains(t, agentSnippets.DockerCompose, "- arcane-data:/app/data")
	require.NotContains(t, agentSnippets.DockerRun, "-v arcane-data:/data")
}

func TestEnvironmentService_EnsureSwarmNodeAgentEnvironment_CreatesVisibleEnvironmentWithoutApiKey(t *testing.T) {
	tunnels := agent.NewTunnelRegistry()
	ctx := t.Context()
	db := setupEnvironmentServiceTestDB(t)
	roleService := role.NewRoleService(db)
	userService := user.NewUserService(db, roleService, session.RevokeAllUserSessionsExceptInDB)
	apiKeyService := apikey.NewApiKeyService(db, userService, roleService)
	svc := NewEnvironmentService(EnvironmentServiceParams{
		Tunnels:         tunnels,
		VariableSyncer:  &environmentTestVariableSyncer{},
		DB:              db,
		RemoteTransport: NewRemoteTransport(db, nil, tunnels),
		EventService:    event.NewEventService(db, &config.Config{}, nil),
		APIKeyService:   apiKeyService,
		Config:          &config.Config{},
		Ctx:             t.Context(),
		Scheduler:       &environmentTestScheduler{},
	})
	localUser := createTestEnvironmentServiceUser(t, ctx, userService, "swarm-admin")
	params := SwarmNodeAgentEnvironmentParams{
		ParentEnvironmentID: "manager-env",
		NodeID:              "node-1234567890abcdef",
		Hostname:            "worker-1",
		UserID:              localUser.ID,
		Username:            localUser.Username,
	}

	createdEnv, err := svc.EnsureSwarmNodeAgentEnvironment(ctx, params)
	require.NoError(t, err)
	require.Equal(t, "worker-1", createdEnv.Name)
	require.Equal(t, "agent://swarm-node-node-1234567", createdEnv.ApiUrl)
	require.False(t, createdEnv.Hidden)
	require.True(t, createdEnv.Enabled)
	require.Equal(t, string(EnvironmentStatusPending), createdEnv.Status)
	require.Equal(t, "manager-env", kit.FromPtr(createdEnv.ParentEnvironmentID))
	require.Equal(t, "node-1234567890abcdef", kit.FromPtr(createdEnv.SwarmNodeID))
	require.Nil(t, createdEnv.AccessToken)
	require.Nil(t, createdEnv.ApiKeyID)

	reusedEnv, err := svc.EnsureSwarmNodeAgentEnvironment(ctx, params)
	require.NoError(t, err)
	require.Equal(t, createdEnv.ID, reusedEnv.ID)

	var childEnvironments []Environment
	require.NoError(t, db.WithContext(ctx).
		Where("parent_environment_id = ?", "manager-env").
		Where("swarm_node_id = ?", "node-1234567890abcdef").
		Find(&childEnvironments).Error)
	require.Len(t, childEnvironments, 1)

	var apiKeyCount int64
	require.NoError(t, db.WithContext(ctx).Model(&apikey.ApiKey{}).Where("environment_id = ?", createdEnv.ID).Count(&apiKeyCount).Error)
	require.Zero(t, apiKeyCount)
}

func TestEnvironmentService_EnsureSwarmNodeAgentEnvironment_ReusesLegacyHiddenRegistration(t *testing.T) {
	tunnels := agent.NewTunnelRegistry()
	ctx := t.Context()
	db := setupEnvironmentServiceTestDB(t)
	svc := NewEnvironmentService(EnvironmentServiceParams{
		Tunnels:         tunnels,
		VariableSyncer:  &environmentTestVariableSyncer{},
		DB:              db,
		RemoteTransport: NewRemoteTransport(db, nil, tunnels),
		EventService:    event.NewEventService(db, &config.Config{}, nil),
		Config:          &config.Config{},
		Ctx:             t.Context(),
		Scheduler:       &environmentTestScheduler{},
	})
	legacy := &Environment{
		ID:                  "legacy-agent",
		Name:                "Legacy node agent",
		ApiUrl:              "agent://legacy-node",
		Status:              string(EnvironmentStatusOffline),
		Enabled:             true,
		Hidden:              true,
		AccessToken:         new("legacy-agent-token"),
		ParentEnvironmentID: new("manager-env"),
		SwarmNodeID:         new("legacy-node"),
	}
	require.NoError(t, db.WithContext(ctx).Create(legacy).Error)

	reused, err := svc.EnsureSwarmNodeAgentEnvironment(ctx, SwarmNodeAgentEnvironmentParams{
		ParentEnvironmentID: "manager-env",
		NodeID:              "legacy-node",
		Hostname:            "legacy-host",
		UserID:              "user-id",
		Username:            "username",
	})
	require.NoError(t, err)
	require.Equal(t, legacy.ID, reused.ID)
	require.True(t, reused.Hidden, "legacy environment was unexpectedly converted to visible")
	require.Equal(t, "legacy-agent-token", kit.FromPtr(reused.AccessToken))

	var environments []Environment
	require.NoError(t, db.WithContext(ctx).
		Where("parent_environment_id = ? AND swarm_node_id = ?", "manager-env", "legacy-node").
		Find(&environments).Error)
	require.Len(t, environments, 1)
}

func TestEnvironmentService_RegenerateEnvironmentApiKey_TokenResolvesEndToEnd(t *testing.T) {
	tunnels := agent.NewTunnelRegistry()
	ctx := t.Context()
	db := setupEnvironmentServiceTestDB(t)
	roleService := role.NewRoleService(db)
	userService := user.NewUserService(db, roleService, session.RevokeAllUserSessionsExceptInDB)
	apiKeyService := apikey.NewApiKeyService(db, userService, roleService)
	svc := NewEnvironmentService(EnvironmentServiceParams{
		Tunnels:         tunnels,
		VariableSyncer:  &environmentTestVariableSyncer{},
		DB:              db,
		RemoteTransport: NewRemoteTransport(db, nil, tunnels),
		EventService:    event.NewEventService(db, &config.Config{}, nil),
		APIKeyService:   apiKeyService,
		Config:          &config.Config{},
		Ctx:             t.Context(),
		Scheduler:       &environmentTestScheduler{},
	})
	localUser := createTestEnvironmentServiceUser(t, ctx, userService, "swarm-resolve-admin")

	env, err := svc.EnsureSwarmNodeAgentEnvironment(ctx, SwarmNodeAgentEnvironmentParams{
		ParentEnvironmentID: "manager-env-resolve",
		NodeID:              "node-resolve-1234567890",
		Hostname:            "resolve-host",
		UserID:              localUser.ID,
		Username:            localUser.Username,
	})
	require.NoError(t, err)
	createdToken, err := svc.RegenerateEnvironmentApiKey(ctx, env, localUser.ID, localUser.Username)
	require.NoError(t, err)
	require.NotEmpty(t, createdToken)

	resolvedID, err := svc.ResolveAgentEnvironmentByToken(ctx, createdToken)
	require.NoError(t, err)
	require.Equal(t, env.ID, resolvedID)

	resolvedID, err = svc.ResolveAgentEnvironmentByToken(ctx, "  "+createdToken+"\n")
	require.NoError(t, err)
	require.Equal(t, env.ID, resolvedID)

	linked, err := svc.GetEnvironmentByID(ctx, env.ID)
	require.NoError(t, err)
	rotatedToken, err := svc.RegenerateEnvironmentApiKey(ctx, linked, localUser.ID, localUser.Username)
	require.NoError(t, err)
	require.NotEqual(t, createdToken, rotatedToken)

	resolvedID, err = svc.ResolveAgentEnvironmentByToken(ctx, rotatedToken)
	require.NoError(t, err)
	require.Equal(t, env.ID, resolvedID)

	_, err = svc.ResolveAgentEnvironmentByToken(ctx, createdToken)
	require.Error(t, err)
	require.Contains(t, err.Error(), "invalid agent token")

	var apiKeyCount int64
	require.NoError(t, db.WithContext(ctx).Model(&apikey.ApiKey{}).Where("environment_id = ?", env.ID).Count(&apiKeyCount).Error)
	require.Equal(t, int64(1), apiKeyCount)
}

func TestEnvironmentService_BindSwarmNodeEnvironment_PreservesVisibleEnvironmentToken(t *testing.T) {
	tunnels := agent.NewTunnelRegistry()
	ctx := t.Context()
	db := setupEnvironmentServiceTestDB(t)
	token := "existing-agent-token"
	createTestEnvironment(t, db, "visible-env", "http://visible.example", &token)
	svc := NewEnvironmentService(EnvironmentServiceParams{
		Tunnels:         tunnels,
		VariableSyncer:  &environmentTestVariableSyncer{},
		DB:              db,
		RemoteTransport: NewRemoteTransport(db, nil, tunnels),
		EventService:    event.NewEventService(db, &config.Config{}, nil),
		Config:          &config.Config{},
		Ctx:             t.Context(),
		Scheduler:       &environmentTestScheduler{},
	})

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
	tunnels := agent.NewTunnelRegistry()
	ctx := t.Context()
	db := setupEnvironmentServiceTestDB(t)
	createTestEnvironment(t, db, "visible-env", "http://visible.example", nil)
	svc := NewEnvironmentService(EnvironmentServiceParams{
		Tunnels:         tunnels,
		VariableSyncer:  &environmentTestVariableSyncer{},
		DB:              db,
		RemoteTransport: NewRemoteTransport(db, nil, tunnels),
		EventService:    event.NewEventService(db, &config.Config{}, nil),
		Config:          &config.Config{},
		Ctx:             t.Context(),
		Scheduler:       &environmentTestScheduler{},
	})

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
	tunnels := agent.NewTunnelRegistry()
	ctx := t.Context()
	db := setupEnvironmentServiceTestDB(t)
	svc := NewEnvironmentService(EnvironmentServiceParams{
		Tunnels:         tunnels,
		VariableSyncer:  &environmentTestVariableSyncer{},
		DB:              db,
		RemoteTransport: NewRemoteTransport(db, nil, tunnels),
		EventService:    event.NewEventService(db, &config.Config{}, nil),
		Lister:          NewLister(db, tunnels),
		Config:          &config.Config{},
		Ctx:             t.Context(),
		Scheduler:       &environmentTestScheduler{},
	})

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

func TestEnvironmentService_GenerateAgentDeploymentSnippets_WithAutoGeneratedMTLS(t *testing.T) {
	tunnels := agent.NewTunnelRegistry()
	crypto.InitEncryption(crypto.Config{
		EncryptionKey: "test-encryption-key-for-testing-32bytes-min",
		Environment:   "test",
	})
	db := setupEnvironmentServiceTestDB(t)
	svc := NewEnvironmentService(EnvironmentServiceParams{
		Tunnels:         tunnels,
		VariableSyncer:  &environmentTestVariableSyncer{},
		DB:              db,
		RemoteTransport: NewRemoteTransport(db, nil, tunnels),
		EventService:    event.NewEventService(db, &config.Config{}, nil),
		Config:          &config.Config{},
		Ctx:             t.Context(),
		Scheduler:       &environmentTestScheduler{},
	})

	assetsDir := filepath.Join(t.TempDir(), "agent-mtls")
	deploymentSnippets, err := svc.GenerateAgentDeploymentSnippets(t.Context(), AgentDeploymentSnippetsParams{
		EnvironmentID: "env-mtls",
		ManagerURL:    "https://manager.example.com",
		Token:         "token-789",
		MTLS: agent.NewMTLS(agent.MTLSConfig{
			Mode:      agent.MTLSModeRequired,
			AssetsDir: assetsDir,
		}),
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
	tunnels := agent.NewTunnelRegistry()
	db := setupEnvironmentServiceTestDB(t)
	svc := NewEnvironmentService(EnvironmentServiceParams{
		Tunnels:         tunnels,
		VariableSyncer:  &environmentTestVariableSyncer{},
		DB:              db,
		RemoteTransport: NewRemoteTransport(db, nil, tunnels),
		EventService:    event.NewEventService(db, &config.Config{}, nil),
		Config:          &config.Config{},
		Ctx:             t.Context(),
		Scheduler:       &environmentTestScheduler{},
	})

	assetsPath := filepath.Join(t.TempDir(), "agent-mtls-file")
	require.NoError(t, os.WriteFile(assetsPath, []byte("not a directory"), 0o600))

	deploymentSnippets, err := svc.GenerateAgentDeploymentSnippets(t.Context(), AgentDeploymentSnippetsParams{
		EnvironmentID: "env-mtls",
		ManagerURL:    "https://manager.example.com",
		Token:         "token-789",
		MTLS: agent.NewMTLS(agent.MTLSConfig{
			Mode:      agent.MTLSModeRequired,
			AssetsDir: assetsPath,
		}),
	})

	require.NoError(t, err)
	require.NotNil(t, deploymentSnippets)
	require.Nil(t, deploymentSnippets.MTLS)
	require.Contains(t, deploymentSnippets.DockerRun, "ARCANE_AGENT_TRANSPORT=auto")
	require.Contains(t, deploymentSnippets.DockerCompose, "ARCANE_MANAGER_API_URL=https://manager.example.com")
}

func TestEnvironmentServiceTestConnectionProbesThroughTunnel(t *testing.T) {
	tunnels := agent.NewTunnelRegistry()
	ctx := t.Context()
	db := setupEnvironmentServiceTestDB(t)
	svc := NewEnvironmentService(EnvironmentServiceParams{
		Tunnels:         tunnels,
		VariableSyncer:  &environmentTestVariableSyncer{},
		DB:              db,
		RemoteTransport: NewRemoteTransport(db, nil, tunnels),
		EventService:    event.NewEventService(db, &config.Config{}, nil),
		Config:          &config.Config{},
		Ctx:             t.Context(),
		Scheduler:       &environmentTestScheduler{},
	})
	createTestEnvironment(t, db, "env-tunnel", startTestAgent(t, tunnels, "env-tunnel", http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
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
	crypto.InitEncryption(crypto.Config{EncryptionKey: "test-encryption-key-for-testing-32bytes-min", Environment: "test"})
	return &database.DB{DB: db}
}

func TestSyncCredentialDecryptionAbortsSnapshot(t *testing.T) {
	tunnels := agent.NewTunnelRegistry()
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
				ApiUrl:      startTestAgent(t, tunnels, "remote", agentHandler),
				AccessToken: new("agent-token"),
			}).Error)
			service := NewEnvironmentService(EnvironmentServiceParams{
				Tunnels:         tunnels,
				VariableSyncer:  &environmentTestVariableSyncer{},
				DB:              db,
				RemoteTransport: NewRemoteTransport(db, nil, tunnels),
				EventService:    event.NewEventService(db, &config.Config{}, nil),
				Config:          &config.Config{},
				Ctx:             t.Context(),
				Scheduler:       &environmentTestScheduler{},
			})
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
	tunnels := agent.NewTunnelRegistry()
	db := setupSyncDB(t)
	require.NoError(t, db.Create(&gitrepo.GitRepository{
		ID:       "public-repo",
		Name:     "Public",
		URL:      "https://example.com/public.git",
		AuthType: "none",
	}).Error)
	agentHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var payload types.GitRepositorySyncRequest
		if err := json.UnmarshalRead(r.Body, &payload); err != nil {
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
		ApiUrl:      startTestAgent(t, tunnels, "remote", agentHandler),
		AccessToken: new("token"),
	}).Error)
	service := NewEnvironmentService(EnvironmentServiceParams{
		Tunnels:         tunnels,
		VariableSyncer:  &environmentTestVariableSyncer{},
		DB:              db,
		RemoteTransport: NewRemoteTransport(db, nil, tunnels),
		EventService:    event.NewEventService(db, &config.Config{}, nil),
		Config:          &config.Config{},
		Ctx:             t.Context(),
		Scheduler:       &environmentTestScheduler{},
	})
	require.NoError(t, service.SyncRepositoriesToEnvironment(t.Context(), "remote"))
}

func TestSyncSkipsUnchangedPayloadUntilForgotten(t *testing.T) {
	tunnels := agent.NewTunnelRegistry()
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
		ApiUrl:      startTestAgent(t, tunnels, "remote", agentHandler),
		AccessToken: new("agent-token"),
	}).Error)
	service := NewEnvironmentService(EnvironmentServiceParams{
		Tunnels:         tunnels,
		DB:              db,
		RemoteTransport: NewRemoteTransport(db, nil, tunnels),
		EventService:    event.NewEventService(db, &config.Config{}, nil),
		VariableSyncer:  &environmentTestVariableSyncer{},
		Config:          &config.Config{},
		Ctx:             t.Context(),
		Scheduler:       &environmentTestScheduler{},
	})
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

func TestEnvironmentServiceRuntimeChangeSignalCoalesces(t *testing.T) {
	tunnels := agent.NewTunnelRegistry()
	s := NewEnvironmentService(EnvironmentServiceParams{Tunnels: tunnels})

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
			body, ok := bodies[strings.Split(strings.TrimPrefix(r.URL.Path, "/api/agents/"), "/")[0]]
			if !ok {
				w.WriteHeader(http.StatusBadGateway)
				_, _ = w.Write([]byte(`{"detail":"environment offline"}`))
				return
			}
			_ = json.MarshalWrite(w, body)
		}),
		Resolve: func(_ context.Context, id string) (types.EnvironmentRef, error) {
			return types.EnvironmentRef{ID: id, Name: "Env " + id}, nil
		},
	})
}

func TestAggregateVersionsReportsEachEnvironment(t *testing.T) {
	ctx := environmentScopeContext(t, "/api/agents/a,b,c/version", map[string]any{
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

func TestListerApplyRuntimeState_MapsAgentHostAndTunnelStats(t *testing.T) {
	tunnels := agent.NewTunnelRegistry()
	envID := "env-runtime-host-stats"
	result, err := tunnels.RegisterSession(t.Context(), agent.NewAgentTunnel(agent.AgentTunnelConfig{
		Conn: agent.NewGRPCManagerTunnelConn(nil),
		Host: &agent.AgentHostInfo{
			Hostname:      "edge-1",
			OS:            "Debian GNU/Linux 13",
			KernelVersion: "6.12.0",
			Architecture:  "x86_64",
			DockerVersion: "28.5.0",
			AgentVersion:  "3.0.0",
			MemoryBytes:   8 << 30,
			CPUs:          4,
		},
		EnvironmentID: envID,
		RemoteAddr:    "203.0.113.7",
	}))
	require.NoError(t, err)
	require.True(t, result.Accepted)
	t.Cleanup(func() {
		tunnels.Unregister(envID)
	})

	env := NewLister(nil, tunnels).ApplyRuntimeState(types.Environment{ID: envID, Status: string(EnvironmentStatusOffline)})

	assert.Equal(t, &types.AgentHostInfo{
		Hostname:      "edge-1",
		OS:            "Debian GNU/Linux 13",
		KernelVersion: "6.12.0",
		Architecture:  "x86_64",
		DockerVersion: "28.5.0",
		AgentVersion:  "3.0.0",
		PublicIP:      "203.0.113.7",
		MemoryBytes:   8 << 30,
		CPUs:          4,
	}, env.AgentHost)
	require.NotNil(t, env.AgentTunnelStats)
	assert.Equal(t, 1, env.AgentTunnelStats.Sessions)
	assert.Nil(t, env.AgentTunnelStats.LastDisconnectAt)
	assert.Nil(t, env.AgentTunnelStats.RoundTripAvgMs)
}

func TestListerApplyRuntimeState_DisconnectedAgentKeepsTunnelHistory(t *testing.T) {
	tunnels := agent.NewTunnelRegistry()
	envID := "env-runtime-history"
	tunnel := agent.NewAgentTunnel(agent.AgentTunnelConfig{
		Conn:          agent.NewGRPCManagerTunnelConn(nil),
		Host:          &agent.AgentHostInfo{Hostname: "edge-2"},
		EnvironmentID: envID,
	})
	result, err := tunnels.RegisterSession(t.Context(), tunnel)
	require.NoError(t, err)
	require.True(t, result.Accepted)
	require.NoError(t, tunnel.CloseWithReason("agent tunnel keepalive failed"))
	require.True(t, tunnels.UnregisterCurrent(t.Context(), tunnel))

	env := NewLister(nil, tunnels).ApplyRuntimeState(types.Environment{ID: envID, Status: string(EnvironmentStatusOnline)})

	assert.Nil(t, env.AgentHost)
	require.NotNil(t, env.AgentTunnelStats)
	assert.Equal(t, 1, env.AgentTunnelStats.Sessions)
	assert.Equal(t, "agent tunnel keepalive failed", env.AgentTunnelStats.LastDisconnectReason)
	assert.NotNil(t, env.AgentTunnelStats.LastDisconnectAt)
	assert.Zero(t, env.AgentTunnelStats.BytesIn)
	assert.False(t, kit.FromPtr(env.Connected))
}

func TestListerApplyRuntimeState_OmitsTunnelStatsWithoutHistory(t *testing.T) {
	tunnels := agent.NewTunnelRegistry()
	env := NewLister(nil, tunnels).ApplyRuntimeState(types.Environment{ID: "env-runtime-never-connected", Status: string(EnvironmentStatusOffline)})

	assert.Nil(t, env.AgentHost)
	assert.Nil(t, env.AgentTunnelStats)
}

func TestEnvironmentService_RestartTunnel(t *testing.T) {
	tunnels := agent.NewTunnelRegistry()
	db := setupEnvironmentServiceTestDB(t)
	svc := NewEnvironmentService(EnvironmentServiceParams{
		Tunnels:         tunnels,
		VariableSyncer:  &environmentTestVariableSyncer{},
		DB:              db,
		RemoteTransport: NewRemoteTransport(db, nil, tunnels),
		EventService:    event.NewEventService(db, &config.Config{}, nil),
		Config:          &config.Config{},
		Ctx:             t.Context(),
		Scheduler:       &environmentTestScheduler{},
	})
	createTestEnvironment(t, db, "env-restart", "http://env.example", nil)

	t.Run("rejects local environment", func(t *testing.T) {
		_, err := svc.RestartTunnel(t.Context(), LocalEnvironmentID, nil, nil)
		require.ErrorIs(t, err, common.ErrValidation)
	})

	t.Run("conflict without active tunnel", func(t *testing.T) {
		_, err := svc.RestartTunnel(t.Context(), "env-restart", nil, nil)
		require.ErrorIs(t, err, common.ErrConflict)
	})

	t.Run("closes tunnel and returns replacement session", func(t *testing.T) {
		previous := agent.NewAgentTunnel(agent.AgentTunnelConfig{
			Conn:          agent.NewGRPCManagerTunnelConn(nil),
			EnvironmentID: "env-restart",
			SessionID:     "session-old",
		})
		registered, err := tunnels.RegisterSession(t.Context(), previous)
		require.NoError(t, err)
		require.True(t, registered.Accepted, registered.Reason)

		go func() {
			if assert.Eventually(t, previous.Conn.IsClosed, 5*time.Second, 5*time.Millisecond) {
				tunnels.UnregisterCurrent(t.Context(), previous)
				replacement, registerErr := tunnels.RegisterSession(t.Context(), agent.NewAgentTunnel(agent.AgentTunnelConfig{
					Conn:          agent.NewGRPCManagerTunnelConn(nil),
					EnvironmentID: "env-restart",
					SessionID:     "session-new",
				}))
				assert.NoError(t, registerErr)
				assert.True(t, replacement.Accepted, replacement.Reason)
			}
		}()

		result, err := svc.RestartTunnel(t.Context(), "env-restart", nil, nil)
		require.NoError(t, err)
		require.Equal(t, types.EnvironmentTunnelRestart{
			SessionID:   "session-new",
			Reconnected: true,
		}, result)
		require.Equal(t, "restarted by user", previous.MetadataSnapshot().DisconnectReason)
	})

	t.Run("reports pending when agent has not redialed", func(t *testing.T) {
		tunnels.Unregister("env-restart")
		registered, err := tunnels.RegisterSession(t.Context(), agent.NewAgentTunnel(agent.AgentTunnelConfig{
			EnvironmentID: "env-restart",
			Conn:          agent.NewGRPCManagerTunnelConn(nil),
		}))
		require.NoError(t, err)
		require.True(t, registered.Accepted, registered.Reason)
		ctx, cancel := context.WithTimeout(t.Context(), 50*time.Millisecond)
		defer cancel()

		result, err := svc.RestartTunnel(ctx, "env-restart", nil, nil)
		require.NoError(t, err)
		require.Equal(t, types.EnvironmentTunnelRestart{}, result)
	})
}

func TestEnvironmentService_CreateEnrollmentToken(t *testing.T) {
	tunnels := agent.NewTunnelRegistry()
	db := setupEnvironmentServiceTestDB(t)
	svc := NewEnvironmentService(EnvironmentServiceParams{
		Tunnels:      tunnels,
		DB:           db,
		EventService: event.NewEventService(db, &config.Config{}, nil),
	})
	actor := types.Actor{
		ID:       "user-1",
		Username: "admin",
	}

	plain, err := svc.CreateEnrollmentToken(t.Context(), CreateEnrollmentTokenParams{
		Request: types.CreateEnrollmentTokenRequest{
			MaxUses: new(3),
			Name:    "Lab",
		},
		Actor: actor,
	})
	require.NoError(t, err)
	assert.True(t, strings.HasPrefix(plain.Token, types.EnrollmentTokenPrefix))
	assert.Equal(t, 3, kit.FromPtr(plain.MaxUses))

	unlimited, err := svc.CreateEnrollmentToken(t.Context(), CreateEnrollmentTokenParams{
		Request: types.CreateEnrollmentTokenRequest{Name: "Secure"},
		Actor:   actor,
	})
	require.NoError(t, err)
	assert.Nil(t, unlimited.MaxUses)

	revealed, err := svc.RevealEnrollmentToken(t.Context(), RevealEnrollmentTokenParams{
		ID:    plain.ID,
		Actor: actor,
	})
	require.NoError(t, err)
	assert.Equal(t, plain.Token, revealed.Token)
	_, err = svc.RevealEnrollmentToken(t.Context(), RevealEnrollmentTokenParams{
		ID:    "missing",
		Actor: actor,
	})
	require.ErrorIs(t, err, common.ErrNotFound)

	var storedSecret string
	require.NoError(t, db.WithContext(t.Context()).Raw("SELECT token_encrypted FROM enrollment_tokens WHERE id = ?", plain.ID).Scan(&storedSecret).Error)
	assert.NotEmpty(t, storedSecret)
	assert.NotContains(t, storedSecret, plain.Token)

	require.NoError(t, svc.DeleteEnrollmentToken(t.Context(), unlimited.ID, actor))
	tokens, err := svc.ListEnrollmentTokens(t.Context())
	require.NoError(t, err)
	require.Len(t, tokens, 1)
	assert.Equal(t, plain.ID, tokens[0].ID)
	require.ErrorIs(t, svc.DeleteEnrollmentToken(t.Context(), unlimited.ID, actor), common.ErrNotFound)

	var events []event.Event
	require.NoError(t, db.WithContext(t.Context()).Where("resource_type = ?", "enrollment_token").Order("created_at ASC").Find(&events).Error)
	require.Len(t, events, 4)
	assert.Equal(t, []event.EventType{
		event.EventTypeEnrollmentTokenCreate,
		event.EventTypeEnrollmentTokenCreate,
		event.EventTypeEnrollmentTokenReveal,
		event.EventTypeEnrollmentTokenDelete,
	}, []event.EventType{
		events[0].Type,
		events[1].Type,
		events[2].Type,
		events[3].Type,
	})
	assert.Equal(t, plain.ID, kit.FromPtr(events[2].ResourceID))
	assert.Equal(t, unlimited.ID, kit.FromPtr(events[3].ResourceID))
	assert.Equal(t, "admin", kit.FromPtr(events[3].Username))
}

func TestEnvironmentService_DeleteEnvironment_RemovesBoundEnrollmentTokens(t *testing.T) {
	tunnels := agent.NewTunnelRegistry()
	ctx := t.Context()
	db := setupEnvironmentServiceTestDB(t)
	createTestEnvironment(t, db, "env-bound-tokens", "agent://env-bound-tokens", nil)
	svc := NewEnvironmentService(EnvironmentServiceParams{
		Tunnels:         tunnels,
		DB:              db,
		RemoteTransport: NewRemoteTransport(db, nil, tunnels),
		EventService:    event.NewEventService(db, &config.Config{}, nil),
		VariableSyncer:  &environmentTestVariableSyncer{},
		InvalidateUser:  func(string) {},
		Config:          &config.Config{},
		Ctx:             ctx,
		Scheduler:       &environmentTestScheduler{},
		Admission:       newAdmissionGateForEnvironmentTest(t),
	})

	_, err := svc.MintDeploymentSnippets(ctx, LocalEnvironmentID, agent.NewMTLS(agent.MTLSConfig{AppURL: "https://arcane.example.com"}))
	require.ErrorIs(t, err, ErrLocalEnvironmentTunnel)
	deployment, err := svc.MintDeploymentSnippets(ctx, "env-bound-tokens", agent.NewMTLS(agent.MTLSConfig{AppURL: "https://arcane.example.com"}))
	require.NoError(t, err)
	unbound, err := svc.CreateEnrollmentToken(ctx, CreateEnrollmentTokenParams{
		Request: types.CreateEnrollmentTokenRequest{Name: "fleet"},
	})
	require.NoError(t, err)

	require.NoError(t, svc.DeleteEnvironment(ctx, "env-bound-tokens", nil, nil))

	_, err = svc.ResolveEnrollmentToken(ctx, deployment.EnrollmentToken)
	require.ErrorIs(t, err, common.ErrUnauthorized)
	_, err = svc.ResolveEnrollmentToken(ctx, unbound.Token)
	require.NoError(t, err, "unbound tokens survive environment deletion")
}

func (*environmentTestVariableSyncer) SyncEnvironment(context.Context, string) error {
	return nil
}

func (*environmentTestVariableSyncer) ForgetSyncState(string) {}
