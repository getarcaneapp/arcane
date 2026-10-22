package network

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"

	"github.com/getarcaneapp/arcane/types/v3"
	"github.com/stretchr/testify/require"

	"github.com/getarcaneapp/arcane/backend/v3/pkg/pagination"
	"github.com/getarcaneapp/arcane/backend/v3/pkg/utils/httpx"
)

func networkScopeContext(t *testing.T, target string, bodies map[string]any) (context.Context, *sync.Map) {
	t.Helper()
	var queries sync.Map
	return httpx.WithEnvironmentScope(t.Context(), &httpx.EnvironmentScope{
		IDs:     []string{"a", "b", "c"},
		Request: httptest.NewRequest(http.MethodGet, target, http.NoBody),
		Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			id := strings.Split(strings.TrimPrefix(r.URL.Path, "/api/environments/"), "/")[0]
			queries.Store(id, r.URL.Query())
			body, ok := bodies[id]
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
	}), &queries
}

func TestAggregateNetworksMergesEnvironments(t *testing.T) {
	bodies := map[string]any{
		"a": types.PaginatedWithCounts[types.NetworkSummary, types.NetworkUsageCounts]{
			Success: true,
			Data: []types.NetworkSummary{
				{
					ID:    "a1",
					Name:  "bridge",
					InUse: true,
				},
				{ID: "a2", Name: "frontend"},
			},
			Counts: types.NetworkUsageCounts{
				Inuse:  1,
				Unused: 1,
				Total:  2,
			},
			Pagination: types.PaginationResponse{TotalItems: 2, GrandTotalItems: 3},
		},
		"b": types.PaginatedWithCounts[types.NetworkSummary, types.NetworkUsageCounts]{
			Success: true,
			Data: []types.NetworkSummary{
				{
					ID:    "b1",
					Name:  "backend",
					InUse: true,
				},
				{
					ID:    "b2",
					Name:  "bridge",
					InUse: true,
				},
			},
			Counts:     types.NetworkUsageCounts{Inuse: 2, Total: 2},
			Pagination: types.PaginationResponse{TotalItems: 2},
		},
	}
	rowLabels := func(items []types.NetworkSummary) []string {
		out := make([]string, 0, len(items))
		for _, item := range items {
			out = append(out, item.EnvironmentID+"/"+item.Name)
		}
		return out
	}
	svc := &NetworkService{}

	t.Run("sorts by name across environments and pages", func(t *testing.T) {
		ctx, queries := networkScopeContext(t, "/api/environments/a,b,c/networks?start=2&limit=2&sort=name&order=asc", bodies)
		body, err := svc.AggregateNetworks(ctx, pagination.QueryParams{SortParams: pagination.SortParams{Sort: "name", Order: pagination.SortAsc}, Params: pagination.Params{Start: 2, Limit: 2}})
		require.NoError(t, err)

		require.True(t, body.Success)
		require.Equal(t, []string{"b/bridge", "a/frontend"}, rowLabels(body.Data))
		require.Equal(t, "Env b", body.Data[0].EnvironmentName)
		require.Equal(t, int64(4), body.Pagination.TotalItems)
		require.Equal(t, int64(5), body.Pagination.GrandTotalItems)
		require.Equal(t, int64(2), body.Pagination.TotalPages)
		require.Equal(t, 2, body.Pagination.CurrentPage)
		require.Equal(t, types.NetworkUsageCounts{
			Inuse:  3,
			Unused: 1,
			Total:  4,
		}, body.Counts)
		require.Equal(t, []types.EnvironmentFailure{{Environment: types.EnvironmentRef{ID: "c", Name: "c"}, Message: "environment offline"}}, body.FailedEnvironments)
		for _, id := range []string{"a", "b"} {
			stored, ok := queries.Load(id)
			require.True(t, ok)
			require.Equal(t, "0", stored.(url.Values).Get("start"))
			require.Equal(t, "-1", stored.(url.Values).Get("limit"))
		}
	})

	t.Run("sorts by environment", func(t *testing.T) {
		ctx, _ := networkScopeContext(t, "/api/environments/a,b,c/networks?sort=environment&order=asc", bodies)
		body, err := svc.AggregateNetworks(ctx, pagination.QueryParams{SortParams: pagination.SortParams{Sort: "environment", Order: pagination.SortAsc}, Params: pagination.Params{Limit: -1}})
		require.NoError(t, err)
		require.Equal(t, []string{"a/bridge", "a/frontend", "b/backend", "b/bridge"}, rowLabels(body.Data))
	})

	t.Run("all environments failing returns empty data", func(t *testing.T) {
		ctx, _ := networkScopeContext(t, "/api/environments/a,b,c/networks", map[string]any{})
		body, err := svc.AggregateNetworks(ctx, pagination.QueryParams{Params: pagination.Params{Limit: 20}})
		require.NoError(t, err)
		require.Empty(t, body.Data)
		require.Len(t, body.FailedEnvironments, 3)
	})
}

func TestAggregateNetworkCountsSumsEnvironments(t *testing.T) {
	ctx, _ := networkScopeContext(t, "/api/environments/a,b,c/networks/counts", map[string]any{
		"a": types.ApiResponse[types.NetworkUsageCounts]{
			Success: true,
			Data: types.NetworkUsageCounts{
				Inuse:  1,
				Unused: 1,
				Total:  2,
			},
		},
		"b": types.ApiResponse[types.NetworkUsageCounts]{
			Success: true,
			Data: types.NetworkUsageCounts{
				Inuse: 3,
				Total: 3,
			},
		},
	})

	body, err := (&NetworkService{}).AggregateNetworkCounts(ctx)
	require.NoError(t, err)

	require.Equal(t, types.NetworkUsageCounts{
		Inuse:  4,
		Unused: 1,
		Total:  5,
	}, body.Data)
	require.Len(t, body.Environments, 2)
	require.Len(t, body.FailedEnvironments, 1)
}

func TestAggregateNetworkTopologyPrefixesNodeIDs(t *testing.T) {
	topology := types.ApiResponse[types.NetworkTopology]{
		Success: true,
		Data: types.NetworkTopology{
			Nodes: []types.NetworkTopologyNode{
				{ID: "net", Name: "bridge", Type: types.NetworkTopologyNodeTypeNetwork},
				{ID: "ctr", Name: "web", Type: types.NetworkTopologyNodeTypeContainer},
			},
			Edges: []types.NetworkTopologyEdge{{ID: "net:ctr", Source: "net", Target: "ctr"}},
		},
	}
	ctx, _ := networkScopeContext(t, "/api/environments/a,b,c/networks/topology", map[string]any{
		"a": topology,
		"b": topology,
	})

	body, err := (&NetworkService{}).AggregateNetworkTopology(ctx)
	require.NoError(t, err)

	require.Len(t, body.Data.Nodes, 4)
	require.Equal(t, "a/net", body.Data.Nodes[0].ID)
	require.Equal(t, "Env a", body.Data.Nodes[0].EnvironmentName)
	require.Equal(t, "b/ctr", body.Data.Nodes[3].ID)
	require.Equal(t, "b", body.Data.Nodes[3].EnvironmentID)
	require.Equal(t, []types.NetworkTopologyEdge{
		{ID: "a/net:ctr", Source: "a/net", Target: "a/ctr"},
		{ID: "b/net:ctr", Source: "b/net", Target: "b/ctr"},
	}, body.Data.Edges)
	require.Len(t, body.FailedEnvironments, 1)
}
