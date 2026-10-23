package swarm

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/getarcaneapp/arcane/types/v3"
	"github.com/stretchr/testify/require"

	"github.com/getarcaneapp/arcane/backend/v3/pkg/pagination"
	"github.com/getarcaneapp/arcane/backend/v3/pkg/utils/httpx"
)

func TestDecodeSwarmSpec_AllowsEmptyObject(t *testing.T) {
	spec, err := decodeSwarmSpec(json.RawMessage(`{}`))
	require.NoError(t, err)
	require.NotNil(t, spec.Labels)
	require.Empty(t, spec.Labels)
}

func TestDecodeSwarmSpec_RejectsNull(t *testing.T) {
	_, err := decodeSwarmSpec(json.RawMessage(`null`))
	require.EqualError(t, err, "swarm spec is required")
}

func swarmScopeContext(t *testing.T, target string, bodies map[string]any) context.Context {
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

func TestAggregateTasksMergesEnvironments(t *testing.T) {
	ctx := swarmScopeContext(t, "/api/environments/a,b,c/swarm/tasks", map[string]any{
		"a": types.Paginated[types.SwarmTaskSummary]{
			Success: true,
			Data:    []types.SwarmTaskSummary{{ID: "t1", Name: "web.1"}},
		},
		"b": types.Paginated[types.SwarmTaskSummary]{
			Success: true,
			Data:    []types.SwarmTaskSummary{{ID: "t2", Name: "api.1"}},
		},
	})

	body, err := (&SwarmService{}).AggregateTasks(ctx, pagination.QueryParams{Params: pagination.Params{Limit: 20}})
	require.NoError(t, err)

	require.Len(t, body.Data, 2)
	require.ElementsMatch(t, []string{"Env a", "Env b"}, []string{body.Data[0].Environment().Name, body.Data[1].Environment().Name})
	require.EqualValues(t, 2, body.Pagination.TotalItems)
	require.Len(t, body.FailedEnvironments, 1)
	require.Equal(t, "c", body.FailedEnvironments[0].Environment.ID)
}

func TestAggregateStatusReportsEachEnvironment(t *testing.T) {
	ctx := swarmScopeContext(t, "/api/environments/a,b,c/swarm/status", map[string]any{
		"a": types.ApiResponse[types.SwarmRuntimeStatus]{
			Success: true,
			Data: types.SwarmRuntimeStatus{
				Enabled:   true,
				ClusterID: "cluster-1",
			},
		},
		"b": types.ApiResponse[types.SwarmRuntimeStatus]{
			Success: true,
			Data:    types.SwarmRuntimeStatus{},
		},
	})

	body, err := (&SwarmService{}).AggregateStatus(ctx)
	require.NoError(t, err)

	require.True(t, body.Data.Enabled)
	require.Equal(t, []types.EnvironmentData[types.SwarmRuntimeStatus]{
		{Environment: types.EnvironmentRef{ID: "a", Name: "Env a"}, Data: types.SwarmRuntimeStatus{Enabled: true, ClusterID: "cluster-1"}},
		{Environment: types.EnvironmentRef{ID: "b", Name: "Env b"}, Data: types.SwarmRuntimeStatus{}},
	}, body.Environments)
	require.Len(t, body.FailedEnvironments, 1)
}
