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

	"github.com/getarcaneapp/arcane/types/v2/base"
	networktypes "github.com/getarcaneapp/arcane/types/v2/network"
	"github.com/stretchr/testify/require"

	"github.com/getarcaneapp/arcane/backend/v2/pkg/pagination"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/utils/httpx"
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
		Resolve: func(_ context.Context, id string) (base.EnvironmentRef, error) {
			return base.EnvironmentRef{ID: id, Name: "Env " + id}, nil
		},
	}), &queries
}

func TestAggregateNetworksMergesEnvironments(t *testing.T) {
	bodies := map[string]any{
		"a": base.PaginatedWithCounts[networktypes.Summary, networktypes.UsageCounts]{Success: true, Data: []networktypes.Summary{
			{ID: "a1", Name: "bridge", InUse: true},
			{ID: "a2", Name: "frontend"},
		}, Counts: networktypes.UsageCounts{Inuse: 1, Unused: 1, Total: 2}, Pagination: base.PaginationResponse{TotalItems: 2, GrandTotalItems: 3}},
		"b": base.PaginatedWithCounts[networktypes.Summary, networktypes.UsageCounts]{Success: true, Data: []networktypes.Summary{
			{ID: "b1", Name: "backend", InUse: true},
			{ID: "b2", Name: "bridge", InUse: true},
		}, Counts: networktypes.UsageCounts{Inuse: 2, Total: 2}, Pagination: base.PaginationResponse{TotalItems: 2}},
	}
	rowLabels := func(items []networktypes.Summary) []string {
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
		require.Equal(t, networktypes.UsageCounts{Inuse: 3, Unused: 1, Total: 4}, body.Counts)
		require.Equal(t, []base.EnvironmentFailure{{Environment: base.EnvironmentRef{ID: "c", Name: "c"}, Message: "environment offline"}}, body.FailedEnvironments)
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
