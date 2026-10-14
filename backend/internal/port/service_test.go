package port

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"net/url"
	"strings"
	"sync"
	"testing"

	"github.com/getarcaneapp/arcane/types/v2/base"
	porttypes "github.com/getarcaneapp/arcane/types/v2/port"
	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/client"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/getarcaneapp/arcane/backend/v2/internal/docker"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/pagination"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/utils/httpx"
)

func newPortServiceTestDockerService(t *testing.T, containers []container.Summary) *docker.DockerClientService {
	t.Helper()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		switch {
		case strings.HasSuffix(r.URL.Path, "/containers/json"):
			if !assert.NoError(t, json.NewEncoder(w).Encode(containers)) {
				return
			}
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)

	return docker.NewDockerClientService(t.Context(), nil, nil, nil).WithClient(newTestDockerClientInternal(t, server))
}

func TestPortService_ListPortsPaginated_FlattensPublishedAndExposedPorts(t *testing.T) {
	svc := NewPortService(newPortServiceTestDockerService(t, []container.Summary{
		{
			ID:    "container-published",
			Names: []string{"/web"},
			Ports: []container.PortSummary{
				{IP: netip.MustParseAddr("0.0.0.0"), PrivatePort: 80, PublicPort: 8080, Type: "tcp"},
				{PrivatePort: 443, Type: "tcp"},
			},
		},
		{
			ID:    "container-exposed",
			Names: []string{"/api"},
			Ports: []container.PortSummary{
				{PrivatePort: 9000, Type: "udp"},
			},
		},
	}))

	items, page, err := svc.ListPortsPaginated(t.Context(), pagination.QueryParams{
		Limit: 20,
	})
	require.NoError(t, err)
	require.Len(t, items, 3)

	assert.Equal(t, int64(3), page.TotalItems)
	assert.Equal(t, "web", items[0].ContainerName)
	assert.Equal(t, "0.0.0.0", items[0].HostIP)
	assert.Equal(t, 8080, items[0].HostPort)
	assert.Equal(t, 80, items[0].ContainerPort)
	assert.True(t, items[0].IsPublished)
	assert.Equal(t, "container-published:0.0.0.0:8080:80:tcp", items[0].ID)

	// No sort requested: the default sort (hostPort, the first binding) places
	// published ports first and orders unpublished ones by container name.
	assert.Equal(t, "api", items[1].ContainerName)
	assert.Equal(t, 9000, items[1].ContainerPort)
	assert.Equal(t, "udp", items[1].Protocol)
	assert.False(t, items[1].IsPublished)

	assert.Equal(t, "web", items[2].ContainerName)
	assert.Equal(t, 0, items[2].HostPort)
	assert.Equal(t, 443, items[2].ContainerPort)
	assert.False(t, items[2].IsPublished)
}

func TestPortService_ListPortsPaginated_SortsByHostPortWithUnpublishedLast(t *testing.T) {
	svc := NewPortService(newPortServiceTestDockerService(t, []container.Summary{
		{
			ID:    "container-3000",
			Names: []string{"/api"},
			Ports: []container.PortSummary{
				{IP: netip.MustParseAddr("127.0.0.1"), PrivatePort: 3000, PublicPort: 3000, Type: "tcp"},
			},
		},
		{
			ID:    "container-8080",
			Names: []string{"/web"},
			Ports: []container.PortSummary{
				{IP: netip.MustParseAddr("0.0.0.0"), PrivatePort: 80, PublicPort: 8080, Type: "tcp"},
			},
		},
		{
			ID:    "container-unpublished",
			Names: []string{"/worker"},
			Ports: []container.PortSummary{
				{PrivatePort: 9000, Type: "tcp"},
			},
		},
	}))

	items, _, err := svc.ListPortsPaginated(t.Context(), pagination.QueryParams{
		Sort:  "hostPort",
		Order: pagination.SortAsc,
		Limit: 20,
	})
	require.NoError(t, err)
	require.Len(t, items, 3)

	assert.Equal(t, []string{"api", "web", "worker"}, []string{
		items[0].ContainerName,
		items[1].ContainerName,
		items[2].ContainerName,
	})
	assert.Equal(t, []int{3000, 8080, 0}, []int{
		items[0].HostPort,
		items[1].HostPort,
		items[2].HostPort,
	})
}

func TestPortService_ListPortsPaginated_SortsByHostPortDescWithUnpublishedLast(t *testing.T) {
	svc := NewPortService(newPortServiceTestDockerService(t, []container.Summary{
		{
			ID:    "container-3000",
			Names: []string{"/api"},
			Ports: []container.PortSummary{
				{IP: netip.MustParseAddr("127.0.0.1"), PrivatePort: 3000, PublicPort: 3000, Type: "tcp"},
			},
		},
		{
			ID:    "container-8080",
			Names: []string{"/web"},
			Ports: []container.PortSummary{
				{IP: netip.MustParseAddr("0.0.0.0"), PrivatePort: 80, PublicPort: 8080, Type: "tcp"},
			},
		},
		{
			ID:    "container-unpublished",
			Names: []string{"/worker"},
			Ports: []container.PortSummary{
				{PrivatePort: 9000, Type: "tcp"},
			},
		},
	}))

	items, _, err := svc.ListPortsPaginated(t.Context(), pagination.QueryParams{
		Sort:  "hostPort",
		Order: pagination.SortDesc,
		Limit: 20,
	})
	require.NoError(t, err)
	require.Len(t, items, 3)

	assert.Equal(t, []string{"web", "api", "worker"}, []string{
		items[0].ContainerName,
		items[1].ContainerName,
		items[2].ContainerName,
	})
	assert.Equal(t, []int{8080, 3000, 0}, []int{
		items[0].HostPort,
		items[1].HostPort,
		items[2].HostPort,
	})
}

func TestPortService_ListPortsPaginated_SortsByHostIPDescWithUnpublishedLast(t *testing.T) {
	svc := NewPortService(newPortServiceTestDockerService(t, []container.Summary{
		{
			ID:    "container-127",
			Names: []string{"/api"},
			Ports: []container.PortSummary{
				{IP: netip.MustParseAddr("127.0.0.1"), PrivatePort: 3000, PublicPort: 3000, Type: "tcp"},
			},
		},
		{
			ID:    "container-000",
			Names: []string{"/web"},
			Ports: []container.PortSummary{
				{IP: netip.MustParseAddr("0.0.0.0"), PrivatePort: 80, PublicPort: 8080, Type: "tcp"},
			},
		},
		{
			ID:    "container-unpublished",
			Names: []string{"/worker"},
			Ports: []container.PortSummary{
				{PrivatePort: 9000, Type: "tcp"},
			},
		},
	}))

	items, _, err := svc.ListPortsPaginated(t.Context(), pagination.QueryParams{
		Sort:  "hostIp",
		Order: pagination.SortDesc,
		Limit: 20,
	})
	require.NoError(t, err)
	require.Len(t, items, 3)

	assert.Equal(t, []string{"api", "web", "worker"}, []string{
		items[0].ContainerName,
		items[1].ContainerName,
		items[2].ContainerName,
	})
	assert.Equal(t, []string{"127.0.0.1", "0.0.0.0", ""}, []string{
		items[0].HostIP,
		items[1].HostIP,
		normalizeOptionalStringSortValueInternal(items[2].HostIP),
	})
}

func TestPrimaryContainerName_TrimsDockerPrefix(t *testing.T) {
	assert.Equal(t, "web", primaryContainerNameInternal([]string{"/web"}, "container-id"))
	assert.Equal(t, "abcdef123456", primaryContainerNameInternal(nil, "abcdef1234567890"))
}

// Test fixtures shared by this package's tests.

// newTestDockerClientInternal builds a Docker client pointed at a fake Docker HTTP server.
func newTestDockerClientInternal(t *testing.T, server *httptest.Server) *client.Client {
	t.Helper()

	httpClient := server.Client()
	cli, err := client.New(
		client.WithHost(server.URL),
		client.WithAPIVersion("1.41"),
		client.WithHTTPClient(httpClient),
	)
	require.NoError(t, err)

	t.Cleanup(func() {
		_ = cli.Close()
	})

	return cli
}

func portScopeContext(t *testing.T, target string, bodies map[string]any) (context.Context, *sync.Map) {
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

func TestAggregatePortsMergesEnvironments(t *testing.T) {
	bodies := map[string]any{
		"a": base.Paginated[porttypes.PortMapping]{Success: true, Data: []porttypes.PortMapping{
			{ID: "a1", ContainerName: "web", HostPort: 8080, ContainerPort: 80, Protocol: "tcp", IsPublished: true},
			{ID: "a2", ContainerName: "db", HostPort: 5432, ContainerPort: 5432, Protocol: "tcp", IsPublished: true},
		}, Pagination: base.PaginationResponse{TotalItems: 2, GrandTotalItems: 3}},
		"b": base.Paginated[porttypes.PortMapping]{Success: true, Data: []porttypes.PortMapping{
			{ID: "b1", ContainerName: "api", HostPort: 3000, ContainerPort: 3000, Protocol: "tcp", IsPublished: true},
		}, Pagination: base.PaginationResponse{TotalItems: 1}},
	}
	rowLabels := func(items []porttypes.PortMapping) []string {
		out := make([]string, 0, len(items))
		for _, item := range items {
			out = append(out, item.EnvironmentID+"/"+item.ContainerName)
		}
		return out
	}
	svc := &PortService{}

	t.Run("sorts by host port across environments and pages", func(t *testing.T) {
		ctx, queries := portScopeContext(t, "/api/environments/a,b,c/ports?start=0&limit=2&sort=hostPort&order=asc", bodies)
		body, err := svc.AggregatePorts(ctx, pagination.QueryParams{SortParams: pagination.SortParams{Sort: "hostPort", Order: pagination.SortAsc}, Params: pagination.Params{Limit: 2}})
		require.NoError(t, err)

		assert.True(t, body.Success)
		assert.Equal(t, []string{"b/api", "a/db"}, rowLabels(body.Data))
		assert.Equal(t, "Env b", body.Data[0].EnvironmentName)
		assert.Equal(t, int64(3), body.Pagination.TotalItems)
		assert.Equal(t, int64(4), body.Pagination.GrandTotalItems)
		assert.Equal(t, int64(2), body.Pagination.TotalPages)
		require.Len(t, body.FailedEnvironments, 1)
		assert.Equal(t, "c", body.FailedEnvironments[0].Environment.ID)
		assert.Equal(t, "environment offline", body.FailedEnvironments[0].Message)
		for _, id := range []string{"a", "b"} {
			query, ok := queries.Load(id)
			require.True(t, ok)
			assert.Equal(t, "0", query.(url.Values).Get("start"))
			assert.Equal(t, "-1", query.(url.Values).Get("limit"))
		}
	})

	t.Run("sorts by environment", func(t *testing.T) {
		ctx, _ := portScopeContext(t, "/api/environments/a,b,c/ports?sort=environment&order=asc", bodies)
		body, err := svc.AggregatePorts(ctx, pagination.QueryParams{SortParams: pagination.SortParams{Sort: "environment", Order: pagination.SortAsc}, Params: pagination.Params{Limit: -1}})
		require.NoError(t, err)
		assert.Equal(t, []string{"a/db", "a/web", "b/api"}, rowLabels(body.Data))
	})

	t.Run("all environments failing returns empty data", func(t *testing.T) {
		ctx, _ := portScopeContext(t, "/api/environments/a,b,c/ports", map[string]any{})
		body, err := svc.AggregatePorts(ctx, pagination.QueryParams{Params: pagination.Params{Limit: 20}})
		require.NoError(t, err)
		assert.Empty(t, body.Data)
		assert.Len(t, body.FailedEnvironments, 3)
	})
}
