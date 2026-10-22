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
	"github.com/libtnb/sqlite"
	"github.com/moby/moby/client"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"github.com/getarcaneapp/arcane/backend/v3/internal/database"
	"github.com/getarcaneapp/arcane/backend/v3/internal/docker"
	"github.com/getarcaneapp/arcane/backend/v3/internal/event"
	"github.com/getarcaneapp/arcane/backend/v3/pkg/authz"
	"github.com/getarcaneapp/arcane/backend/v3/pkg/libarcane"
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

type scopedNetworkTestEngine struct {
	service       *NetworkService
	createdLabels map[string]string
	createCalls   int
	connectCalls  int
}

func newScopedNetworkTestEngine(t *testing.T) *scopedNetworkTestEngine {
	t.Helper()
	gormDB, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, gormDB.AutoMigrate(&event.Event{}))
	db := &database.DB{DB: gormDB}
	engine := &scopedNetworkTestEngine{}
	networks := map[string]map[string]any{
		"n-team":  {"Id": "n-team", "Name": "team-net", "Driver": "bridge"},
		"n-team2": {"Id": "n-team2", "Name": "team-two", "Driver": "bridge"},
		"n-other": {"Id": "n-other", "Name": "other", "Driver": "bridge"},
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/networks/create"):
			var body struct {
				Name   string
				Labels map[string]string
			}
			if decodeErr := json.NewDecoder(r.Body).Decode(&body); decodeErr != nil {
				http.Error(w, decodeErr.Error(), http.StatusBadRequest)
				return
			}
			engine.createCalls++
			engine.createdLabels = body.Labels
			_ = json.NewEncoder(w).Encode(map[string]any{"Id": "n-new"})
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/connect"):
			engine.connectCalls++
			w.WriteHeader(http.StatusOK)
		case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/networks"):
			_ = json.NewEncoder(w).Encode([]map[string]any{networks["n-team"], networks["n-team2"], networks["n-other"]})
		case r.Method == http.MethodGet && strings.Contains(r.URL.Path, "/networks/"):
			inspect, ok := networks[r.URL.Path[strings.LastIndex(r.URL.Path, "/")+1:]]
			if !ok {
				http.Error(w, `{"message":"no such network"}`, http.StatusNotFound)
				return
			}
			_ = json.NewEncoder(w).Encode(inspect)
		case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/containers/json"):
			_ = json.NewEncoder(w).Encode([]map[string]any{})
		case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/containers/c-other/json"):
			_ = json.NewEncoder(w).Encode(map[string]any{
				"Id":     "c-other",
				"Name":   "/other-ctr",
				"Config": map[string]any{"Labels": map[string]string{}},
			})
		default:
			http.Error(w, "unexpected Docker request: "+r.Method+" "+r.URL.Path, http.StatusInternalServerError)
		}
	}))
	t.Cleanup(server.Close)
	dockerClient, err := client.New(
		client.WithHost(server.URL),
		client.WithAPIVersion("1.41"),
		client.WithHTTPClient(server.Client()),
	)
	require.NoError(t, err)
	t.Cleanup(func() {
		_ = dockerClient.Close()
	})
	engine.service = NewNetworkService(db, &docker.DockerClientService{Client: dockerClient}, event.NewEventService(db, nil, nil, nil))
	return engine
}

func scopedNetworkContext(t *testing.T, ps *authz.PermissionSet) context.Context {
	t.Helper()
	return authz.WithRequestEnvironment(authz.WithPermissions(t.Context(), ps), "0")
}

func TestListNetworksPaginatedFiltersByListScopeBeforeCounts(t *testing.T) {
	engine := newScopedNetworkTestEngine(t)
	ps := authz.NewPermissionSet()
	ps.AddScopedEnv("0", types.ResourceSelector{Names: []string{"team-*"}}, authz.PermNetworksList)

	items, page, counts, err := engine.service.ListNetworksPaginated(scopedNetworkContext(t, ps), pagination.QueryParams{
		Params: pagination.Params{Limit: 1},
	})
	require.NoError(t, err)
	require.Len(t, items, 1)
	require.EqualValues(t, 2, page.TotalItems)
	require.Equal(t, types.NetworkUsageCounts{
		Unused: 2,
		Total:  2,
	}, counts)
}

func TestScopedNetworkAccessReturnsNotFoundOrForbidden(t *testing.T) {
	engine := newScopedNetworkTestEngine(t)
	selector := types.ResourceSelector{Names: []string{"team-*"}}

	hidden := authz.NewPermissionSet()
	hidden.AddScopedEnv("0", selector, authz.PermNetworksRead, authz.PermNetworksDelete)
	_, err := engine.service.GetNetworkByID(scopedNetworkContext(t, hidden), "n-other")
	require.ErrorIs(t, err, authz.ErrResourceOutOfScope)
	require.ErrorIs(t, engine.service.RemoveNetwork(scopedNetworkContext(t, hidden), "n-other", types.SystemUser), authz.ErrResourceOutOfScope)

	visible := authz.NewPermissionSet()
	visible.AddEnv("0", authz.PermNetworksRead)
	visible.AddScopedEnv("0", selector, authz.PermNetworksDelete)
	require.ErrorIs(t, engine.service.RemoveNetwork(scopedNetworkContext(t, visible), "n-other", types.SystemUser), authz.ErrResourceForbidden)
}

func TestCreateNetworkStampsOwnerAndChecksCreateScope(t *testing.T) {
	engine := newScopedNetworkTestEngine(t)
	owner := authz.NewPermissionSet()
	owner.SubjectID = "user-1"
	owner.AddScopedEnv("0", types.ResourceSelector{OwnerOnly: true}, authz.PermNetworksCreate)

	_, err := engine.service.CreateNetwork(scopedNetworkContext(t, owner), "mine", client.NetworkCreateOptions{
		Driver: "bridge",
		Labels: map[string]string{
			libarcane.LabelOwner: "someone-else",
			"team":               "a",
		},
	}, types.Actor{ID: "user-1", Username: "user"})
	require.NoError(t, err)
	require.Equal(t, map[string]string{
		libarcane.LabelOwner: "user-1",
		"team":               "a",
	}, engine.createdLabels)

	labelScoped := authz.NewPermissionSet()
	labelScoped.AddScopedEnv("0", types.ResourceSelector{Labels: map[string]string{"team": "a"}}, authz.PermNetworksCreate)
	_, err = engine.service.CreateNetwork(scopedNetworkContext(t, labelScoped), "unlabeled", client.NetworkCreateOptions{Driver: "bridge"}, types.Actor{ID: "user-1"})
	require.ErrorIs(t, err, authz.ErrResourceForbidden)
	require.Equal(t, 1, engine.createCalls)
}

func TestConnectContainerRequiresContainerInScope(t *testing.T) {
	engine := newScopedNetworkTestEngine(t)
	selector := types.ResourceSelector{Names: []string{"team-*"}}
	request := types.NetworkContainerRequest{ContainerID: "c-other"}

	hidden := authz.NewPermissionSet()
	hidden.AddScopedEnv("0", selector, authz.PermNetworksConnect)
	require.ErrorIs(t, engine.service.ConnectContainer(scopedNetworkContext(t, hidden), "n-team", request, types.SystemUser), authz.ErrResourceOutOfScope)

	visible := authz.NewPermissionSet()
	visible.AddEnv("0", authz.PermContainersRead)
	visible.AddScopedEnv("0", selector, authz.PermNetworksConnect)
	require.ErrorIs(t, engine.service.ConnectContainer(scopedNetworkContext(t, visible), "n-team", request, types.SystemUser), authz.ErrResourceForbidden)
	require.Zero(t, engine.connectCalls)
}
