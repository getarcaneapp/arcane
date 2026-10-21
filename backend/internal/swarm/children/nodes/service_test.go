package nodes

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/getarcaneapp/arcane/types/v3"
	"github.com/libtnb/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"github.com/getarcaneapp/arcane/backend/v2/internal/common"
	"github.com/getarcaneapp/arcane/backend/v2/internal/config"
	"github.com/getarcaneapp/arcane/backend/v2/internal/database"
	"github.com/getarcaneapp/arcane/backend/v2/internal/environment"
	"github.com/getarcaneapp/arcane/backend/v2/internal/settings"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/remenv"
)

func setupSwarmServiceTestDB(t *testing.T) *database.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&settings.SettingVariable{}, &environment.Environment{}))
	return &database.DB{DB: db}
}

func newSettingsServiceForSwarmTest(t testing.TB, ctx context.Context, db *database.DB) (*settings.SettingsService, error) {
	t.Helper()
	svc, err := settings.NewSettingsService(ctx, db, &config.Config{})
	if err == nil {
		t.Cleanup(func() { require.NoError(t, svc.Stop(context.WithoutCancel(t.Context()))) })
	}
	return svc, err
}

func createSwarmTestEnvironment(t *testing.T, db *database.DB, id, apiURL, status string, isEdge bool, accessToken *string) {
	t.Helper()
	now := time.Now()
	require.NoError(t, db.Create(&environment.Environment{
		ID: id, CreatedAt: now, UpdatedAt: &now,
		Name:        "env-" + id,
		ApiUrl:      apiURL,
		Status:      status,
		Enabled:     true,
		IsEdge:      isEdge,
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
		return errors.Join(&remenv.StatusError{StatusCode: code, Body: []byte(body)})
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
		"failed to send request to environment edge: dial tcp 10.0.0.9:3552: connect: connection refused",
		describeSwarmJoinFailure(errors.New("failed to send request to environment edge: dial tcp 10.0.0.9:3552: connect: connection refused"), token),
	)
	require.Equal(t, "token [redacted] rejected", describeSwarmJoinFailure(errors.New("token "+token+" rejected"), token))
}

func TestService_FetchSwarmNodeIdentityViaEdge_UsesEnvironmentAccessToken(t *testing.T) {
	ctx := t.Context()
	db := setupSwarmServiceTestDB(t)
	settingsSvc, err := newSettingsServiceForSwarmTest(t, ctx, db)
	require.NoError(t, err)
	envSvc := environment.NewEnvironmentService(db, nil, nil, nil, settingsSvc, nil)

	accessToken := "token-123"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !assert.Equal(t, http.MethodGet, r.Method) {
			return
		}
		if !assert.Equal(t, "/api/swarm/node-identity", r.URL.Path) {
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
	}))
	defer server.Close()

	createSwarmTestEnvironment(
		t,
		db,
		"env-1",
		server.URL,
		string(environment.EnvironmentStatusOnline),
		false,
		&accessToken,
	)

	svc := NewService(nil, nil, nil, nil, envSvc)

	identity, err := svc.fetchSwarmNodeIdentityViaEdge(ctx, "env-1")
	require.NoError(t, err)
	require.NotNil(t, identity)
	require.Equal(t, "node-1", identity.SwarmNodeID)
	require.Equal(t, "worker-1", identity.Hostname)
	require.Equal(t, "worker", identity.Role)
	require.Equal(t, "29.3.1", identity.EngineVersion)
	require.True(t, identity.SwarmActive)
}

// TestService_BuildNodeAgentStatus covers the state classification.
// The regression case is a poll-mode agent: its persisted env.Status never leaves
// "pending" (HandlePoll only updates the in-memory poll registry), so a fresh
// lastPollAt must still resolve to "connected" rather than "pending".
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
			name:    "poll-mode check-in reports connected despite stale pending status",
			env:     &environment.Environment{Status: string(environment.EnvironmentStatusPending)},
			runtime: swarmNodeAgentRuntime{lastPollAt: &now},
			want:    types.NodeAgentStateConnected,
		},
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
