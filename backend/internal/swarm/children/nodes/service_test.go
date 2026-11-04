package nodes

import (
	"context"
	"encoding/json/v2"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/labstack/echo/v5"
	"github.com/libtnb/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"kit.getarcane.app"

	"getarcane.dev/arcane/v3/internal/apikey"
	"getarcane.dev/arcane/v3/internal/common"
	"getarcane.dev/arcane/v3/internal/config"
	"getarcane.dev/arcane/v3/internal/database"
	"getarcane.dev/arcane/v3/internal/environment"
	"getarcane.dev/arcane/v3/internal/event"
	"getarcane.dev/arcane/v3/internal/role"
	"getarcane.dev/arcane/v3/internal/session"
	"getarcane.dev/arcane/v3/internal/settings"
	"getarcane.dev/arcane/v3/internal/user"
	"getarcane.dev/arcane/v3/pkg/agent"
	"getarcane.dev/arcane/v3/pkg/httpx"
	"getarcane.dev/arcane/v3/pkg/pagination"
	"getarcane.dev/types/v3"
)

type swarmTestScheduler struct{}

func (swarmTestScheduler) Submit(context.Context, types.SchedulerRequest) (types.SchedulerRun, error) {
	return types.SchedulerRun{}, nil
}

func (swarmTestScheduler) AddJob(context.Context, types.SchedulerJob) error { return nil }

func (swarmTestScheduler) RemoveJob(context.Context, string) {}

func (swarmTestScheduler) HasJob(string) bool { return false }

func setupSwarmServiceTestDB(t *testing.T) *database.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&settings.SettingVariable{}, &environment.Environment{}))
	return &database.DB{DB: db}
}

func newSettingsServiceForSwarmTest(t testing.TB, ctx context.Context, db *database.DB) (*settings.SettingsService, error) {
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

func startSwarmTestAgent(t *testing.T, tunnels *agent.TunnelRegistry, envID string, handler http.Handler) {
	t.Helper()
	manager := echo.New()
	manager.GET("/api/tunnel/connect", agent.NewTunnelServer(agent.TunnelServerOptions{
		Registry: tunnels,
		Resolver: func(context.Context, string) (string, error) {
			return envID, nil
		},
		MTLS: agent.NewMTLS(agent.MTLSConfig{}),
	}).HandleConnect)
	managerServer := httptest.NewServer(manager)
	t.Cleanup(managerServer.Close)
	agentCtx, cancelAgent := context.WithCancel(t.Context())
	t.Cleanup(func() {
		cancelAgent()
		tunnels.Unregister(envID)
	})
	_, startErr := agent.NewTunnelClient(agent.TunnelClientConfig{
		Config: &agent.Config{
			MTLS:           agent.NewMTLS(agent.MTLSConfig{ManagerURL: managerServer.URL}),
			AgentEnabled:   true,
			AgentTransport: agent.TransportWebSocket,
			AgentToken:     "agent-token",
		},
		Handler: handler,
	}).Start(agentCtx)
	require.NoError(t, startErr)
	require.True(t, tunnels.WaitForActive(t.Context(), envID, 5*time.Second).IsPresent())
}

func createSwarmTestEnvironment(t *testing.T, db *database.DB, id, apiURL, status string, accessToken *string) {
	t.Helper()
	now := time.Now()
	require.NoError(t, db.Create(&environment.Environment{
		ID: id, CreatedAt: now, UpdatedAt: &now,
		Name:        "env-" + id,
		ApiUrl:      apiURL,
		Status:      status,
		Enabled:     true,
		AccessToken: accessToken,
	}).Error)
}

func TestSelectSwarmManagerAddresses(t *testing.T) {
	nodes := []types.SwarmNode{
		{ID: "manager-1", ManagerAddress: "10.0.0.1:2377"},
		{ID: "worker-1"},
		{ID: "manager-2", ManagerAddress: "10.0.0.2:2377"},
	}

	addrs, err := selectSwarmManagerAddresses([]string{" 100.64.0.10:2377 ", "", "100.64.0.11:2377"}, nodes)
	require.NoError(t, err)
	require.Equal(t, []string{"100.64.0.10:2377", "100.64.0.11:2377"}, addrs)

	addrs, err = selectSwarmManagerAddresses([]string{"  "}, nodes)
	require.NoError(t, err)
	require.Equal(t, []string{"10.0.0.1:2377", "10.0.0.2:2377"}, addrs)

	_, err = selectSwarmManagerAddresses(nil, []types.SwarmNode{{ID: "worker-1"}})
	require.ErrorIs(t, err, common.ErrBadRequest)
}

func TestDescribeSwarmJoinFailure(t *testing.T) {
	const token = "SWMTKN-1-secret"
	statusErr := func(code int, body string) error {
		return errors.Join(&agent.StatusError{StatusCode: code, Body: []byte(body)})
	}

	require.Equal(t,
		"failed to join swarm: Timeout was reached before node joined using [redacted]",
		describeSwarmJoinFailure(
			statusErr(
				500,
				"{\"title\":\"Internal Server Error\",\"status\":500,\"detail\":\"failed to join swarm: Timeout was reached be"+
					"fore node joined using "+token+`"}`,
			),
			token,
		),
	)
	require.Equal(t, "legacy join failure", describeSwarmJoinFailure(statusErr(400, `{"success":false,"error":"legacy join failure"}`), token))
	require.Equal(t, "swarm join failed with HTTP 502 from the target agent", describeSwarmJoinFailure(statusErr(502, "<html>bad gateway</html>"), token))
	require.Equal(t, "swarm join failed with HTTP 404 from the target agent", describeSwarmJoinFailure(statusErr(404, ""), token))
	require.Equal(t,
		"failed to send request to environment agent: dial tcp 10.0.0.9:3552: connect: connection refused",
		describeSwarmJoinFailure(errors.New("failed to send request to environment agent: dial tcp 10.0.0.9:3552: connect: connection refused"), token),
	)
	require.Equal(t, "token [redacted] rejected", describeSwarmJoinFailure(errors.New("token "+token+" rejected"), token))
}

func TestService_FetchSwarmNodeIdentityViaAgent_UsesEnvironmentAccessToken(t *testing.T) {
	tunnels := agent.NewTunnelRegistry()
	ctx := t.Context()
	db := setupSwarmServiceTestDB(t)
	settingsSvc, err := newSettingsServiceForSwarmTest(t, ctx, db)
	require.NoError(t, err)
	envSvc := environment.NewEnvironmentService(environment.EnvironmentServiceParams{
		Tunnels:         tunnels,
		DB:              db,
		SettingsService: settingsSvc,
		RemoteTransport: environment.NewRemoteTransport(db, settingsSvc, tunnels),
	})

	accessToken := "token-123"
	agentHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !assert.Equal(t, http.MethodGet, r.Method) {
			return
		}
		if !assert.Equal(t, "/api/swarm/identity", r.URL.Path) {
			return
		}
		if !assert.Equal(t, accessToken, r.Header.Get("X-API-Key")) {
			return
		}
		if !assert.Equal(t, accessToken, r.Header.Get("X-Arcane-Agent-Token")) {
			return
		}

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"success":true,"data":{"swarmNodeId":"node-1","hostname":"worker-1","role":"worker","engineVersion":"29.3.1","swarmActive":true}}`))
	})
	startSwarmTestAgent(t, tunnels, "env-1", agentHandler)

	createSwarmTestEnvironment(
		t,
		db,
		"env-1",
		"agent://env-1",
		string(environment.EnvironmentStatusOnline),
		&accessToken,
	)

	svc := NewService(ServiceParams{
		Tunnels:            tunnels,
		EnvironmentService: envSvc,
	})

	identity, err := svc.fetchSwarmNodeIdentityViaAgent(ctx, "env-1")
	require.NoError(t, err)
	require.NotNil(t, identity)
	require.Equal(t, "node-1", identity.SwarmNodeID)
	require.Equal(t, "worker-1", identity.Hostname)
	require.Equal(t, "worker", identity.Role)
	require.Equal(t, "29.3.1", identity.EngineVersion)
	require.True(t, identity.SwarmActive)
}

// TestService_BuildNodeAgentStatus covers the state classification.
func TestService_BuildNodeAgentStatus(t *testing.T) {
	const nodeID = "node-abc"
	now := time.Now()
	svc := &Service{}

	tests := []struct {
		name    string
		env     *environment.Environment
		runtime swarmNodeAgentRuntime
		want    types.NodeAgentState
	}{
		{
			name:    "no runtime activity and never paired stays pending",
			env:     &environment.Environment{Status: string(environment.EnvironmentStatusPending)},
			runtime: swarmNodeAgentRuntime{},
			want:    types.NodeAgentStatePending,
		},
		{
			name:    "tunnel with mismatched identity reports mismatched",
			env:     &environment.Environment{Status: string(environment.EnvironmentStatusOnline)},
			runtime: swarmNodeAgentRuntime{connected: true, identity: &SwarmNodeIdentity{SwarmNodeID: "other-node", SwarmActive: true}},
			want:    types.NodeAgentStateMismatched,
		},
		{
			name:    "tunnel connected without identity probe reports connected",
			env:     &environment.Environment{Status: string(environment.EnvironmentStatusPending)},
			runtime: swarmNodeAgentRuntime{connected: true},
			want:    types.NodeAgentStateConnected,
		},
		{
			name:    "previously seen agent with no activity reports offline",
			env:     &environment.Environment{Status: string(environment.EnvironmentStatusOnline), LastSeen: &now},
			runtime: swarmNodeAgentRuntime{},
			want:    types.NodeAgentStateOffline,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := svc.buildNodeAgentStatus(nodeID, tt.env, tt.runtime)
			require.Equal(t, tt.want, got.State)
		})
	}
}

func nodesScopeContext(t *testing.T, target string, bodies map[string]any) context.Context {
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
			_ = json.MarshalWrite(w, body)
		}),
		Resolve: func(_ context.Context, id string) (types.EnvironmentRef, error) {
			return types.EnvironmentRef{ID: id, Name: "Env " + id}, nil
		},
	})
}

func TestAggregateMergesEnvironments(t *testing.T) {
	ctx := nodesScopeContext(t, "/api/environments/a,b,c/swarm/nodes", map[string]any{
		"a": types.Paginated[types.SwarmNode]{
			Success: true,
			Data:    []types.SwarmNode{{ID: "n1", Hostname: "one"}},
		},
		"b": types.Paginated[types.SwarmNode]{
			Success: true,
			Data:    []types.SwarmNode{{ID: "n2", Hostname: "two"}},
		},
	})

	body, err := (&Service{}).Aggregate(ctx, pagination.QueryParams{Params: pagination.Params{Limit: 20}})
	require.NoError(t, err)

	require.Len(t, body.Data, 2)
	require.ElementsMatch(t, []string{"Env a", "Env b"}, []string{body.Data[0].Environment().Name, body.Data[1].Environment().Name})
	require.EqualValues(t, 2, body.Pagination.TotalItems)
	require.Len(t, body.FailedEnvironments, 1)
	require.Equal(t, "c", body.FailedEnvironments[0].Environment.ID)
}

func TestService_EnrollNodeAgent(t *testing.T) {
	tunnels := agent.NewTunnelRegistry()
	ctx := t.Context()
	db := setupSwarmServiceTestDB(t)
	require.NoError(t, db.AutoMigrate(&apikey.ApiKey{}, &role.Role{}, &role.UserRoleAssignment{}, &role.ApiKeyPermission{}, &user.User{}, &event.Event{}))
	sqlDB, err := db.DB.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(1)
	settingsSvc, err := newSettingsServiceForSwarmTest(t, ctx, db)
	require.NoError(t, err)
	roleSvc := role.NewRoleService(db)
	envSvc := environment.NewEnvironmentService(environment.EnvironmentServiceParams{
		Tunnels:         tunnels,
		DB:              db,
		EventService:    event.NewEventService(db, &config.Config{}, nil),
		SettingsService: settingsSvc,
		APIKeyService:   apikey.NewApiKeyService(db, user.NewUserService(db, roleSvc, session.RevokeAllUserSessionsExceptInDB), roleSvc),
		RemoteTransport: environment.NewRemoteTransport(db, settingsSvc, tunnels),
		Config:          &config.Config{},
		Ctx:             ctx,
		Scheduler:       swarmTestScheduler{},
	})
	svc := NewService(ServiceParams{
		EnvironmentService: envSvc,
		Tunnels:            tunnels,
	})

	worker1 := `{"success":true,"data":{"id":"workernode1","hostname":"worker-1","role":"worker","agent":{"state":"none"}}}`
	parentNodes := map[string]string{
		"managernode1": `{"success":true,"data":{"id":"managernode1","hostname":"manager-1","role":"manager","agent":{"state":"connected","bindingKind":"local"}}}`,
		"workernode1":  worker1,
		"w1":           worker1,
		"workernode2":  `{"success":true,"data":{"id":"workernode2","hostname":"worker-2","role":"worker","agent":{"state":"none"}}}`,
	}
	startSwarmTestAgent(t, tunnels, "parent", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, ok := parentNodes[strings.TrimPrefix(r.URL.Path, "/api/environments/0/swarm/nodes/")]
		if !ok {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(body))
	}))
	createSwarmTestEnvironment(t, db, "parent", "agent://parent", string(environment.EnvironmentStatusOnline), new("parent-token"))

	_, err = svc.EnrollNodeAgent(ctx, "parent", "../containers/json")
	require.ErrorIs(t, err, common.ErrBadRequest)

	enrolled, err := svc.EnrollNodeAgent(ctx, "parent", "workernode1")
	require.NoError(t, err)
	enrolledEnv, err := envSvc.GetEnvironmentByID(ctx, enrolled.EnvironmentID)
	require.NoError(t, err)
	assert.Equal(t, "worker-1", enrolledEnv.Name)
	assert.Equal(t, "parent", kit.FromPtr(enrolledEnv.ParentEnvironmentID))
	assert.Equal(t, "workernode1", kit.FromPtr(enrolledEnv.SwarmNodeID))
	assert.Equal(t, enrolled.Token, kit.FromPtr(enrolledEnv.AccessToken))

	_, err = svc.EnrollNodeAgent(ctx, "parent", "w1")
	require.ErrorIs(t, err, common.ErrBadRequest, "node names and ID prefixes are refused")
	unchanged, err := envSvc.GetEnvironmentByID(ctx, enrolled.EnvironmentID)
	require.NoError(t, err)
	assert.Equal(t, enrolled.Token, kit.FromPtr(unchanged.AccessToken))

	_, err = svc.EnrollNodeAgent(ctx, "parent", "workernode1")
	require.ErrorIs(t, err, common.ErrConflict, "an unbound token cannot take over an existing node environment")
	kept, err := envSvc.GetEnvironmentByID(ctx, enrolled.EnvironmentID)
	require.NoError(t, err)
	assert.Equal(t, enrolled.Token, kit.FromPtr(kept.AccessToken), "a refused enrollment keeps the environment's key")

	_, err = svc.EnrollNodeAgent(ctx, "parent", "managernode1")
	require.ErrorIs(t, err, common.ErrConflict, "the parent's own node is already covered")

	startSwarmTestAgent(t, tunnels, "worker2", http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"success":true,"data":{"swarmNodeId":"workernode2","hostname":"worker-2","role":"worker","swarmActive":true}}`))
	}))
	createSwarmTestEnvironment(t, db, "worker2", "agent://worker2", string(environment.EnvironmentStatusOnline), new("worker2-token"))
	_, err = svc.EnrollNodeAgent(ctx, "parent", "workernode2")
	require.ErrorIs(t, err, common.ErrConflict, "a connected agent that is not bound yet still covers its node")
	_, err = envSvc.BindSwarmNodeEnvironment(ctx, "parent", "workernode2", "worker2", false)
	require.NoError(t, err)
	_, err = svc.EnrollNodeAgent(ctx, "parent", "workernode2")
	require.ErrorIs(t, err, common.ErrConflict, "a node with a connected agent is not taken over")
	agentEnvs, err := envSvc.ListSwarmNodeAgentEnvironments(ctx, "parent")
	require.NoError(t, err)
	assert.Len(t, agentEnvs, 2, "no second environment is created for a covered node")
}
