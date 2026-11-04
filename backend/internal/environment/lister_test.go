package environment

import (
	"cmp"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"getarcane.dev/arcane/v3/pkg/agent"
	"getarcane.dev/arcane/v3/pkg/pagination"
	"getarcane.dev/types/v3"
)

func TestLister_ListEnvironmentsPaginated_FiltersByAccessibleEnvIDs(t *testing.T) {
	ctx := t.Context()
	db := setupEnvironmentServiceTestDB(t)
	lister := NewLister(db)

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
	all, _, err := lister.ListEnvironmentsPaginated(ctx, newParams(""), nil)
	require.NoError(t, err)
	require.ElementsMatch(t, []string{"0", "env-a", "env-b"}, environmentIDs(all))

	// A non-nil allow-list restricts the result on the DB path.
	scoped, _, err := lister.ListEnvironmentsPaginated(ctx, newParams(""), []string{"env-a"})
	require.NoError(t, err)
	require.ElementsMatch(t, []string{"env-a"}, environmentIDs(scoped))

	// The runtime-filter path (type filter) honors the allow-list too.
	scopedTyped, _, err := lister.ListEnvironmentsPaginated(ctx, newParams("agent"), []string{"env-a"})
	require.NoError(t, err)
	require.ElementsMatch(t, []string{"env-a"}, environmentIDs(scopedTyped))

	// An empty (non-nil) allow-list yields no environments on either path.
	none, _, err := lister.ListEnvironmentsPaginated(ctx, newParams(""), []string{})
	require.NoError(t, err)
	require.Empty(t, none)

	noneTyped, _, err := lister.ListEnvironmentsPaginated(ctx, newParams("agent"), []string{})
	require.NoError(t, err)
	require.Empty(t, noneTyped)
}

func TestLister_ListEnvironmentsPaginated_FiltersByRuntimeType(t *testing.T) {
	ctx := t.Context()
	db := setupEnvironmentServiceTestDB(t)
	lister := NewLister(db)

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
			listedEnvironments, _, err := lister.ListEnvironmentsPaginated(ctx, pagination.QueryParams{
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
