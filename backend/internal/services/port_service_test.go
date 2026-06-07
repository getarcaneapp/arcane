package services

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"strings"
	"testing"

	"github.com/getarcaneapp/arcane/backend/pkg/pagination"
	dockercontainer "github.com/moby/moby/api/types/container"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newPortServiceTestDockerService(t *testing.T, containers []dockercontainer.Summary) *DockerClientService {
	t.Helper()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		switch {
		case strings.HasSuffix(r.URL.Path, "/containers/json"):
			require.NoError(t, json.NewEncoder(w).Encode(containers))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)

	return &DockerClientService{client: newTestDockerClient(t, server)}
}

func TestPortService_ListPortsPaginated_FlattensPublishedAndExposedPorts(t *testing.T) {
	svc := NewPortService(newPortServiceTestDockerService(t, []dockercontainer.Summary{
		{
			ID:    "container-published",
			Names: []string{"/web"},
			Ports: []dockercontainer.PortSummary{
				{IP: netip.MustParseAddr("0.0.0.0"), PrivatePort: 80, PublicPort: 8080, Type: "tcp"},
				{PrivatePort: 443, Type: "tcp"},
			},
		},
		{
			ID:    "container-exposed",
			Names: []string{"/api"},
			Ports: []dockercontainer.PortSummary{
				{PrivatePort: 9000, Type: "udp"},
			},
		},
	}))

	items, page, err := svc.ListPortsPaginated(context.Background(), pagination.QueryParams{
		PaginationParams: pagination.PaginationParams{Limit: 20},
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

	assert.Equal(t, "web", items[1].ContainerName)
	assert.Equal(t, 0, items[1].HostPort)
	assert.Equal(t, 443, items[1].ContainerPort)
	assert.False(t, items[1].IsPublished)

	assert.Equal(t, "api", items[2].ContainerName)
	assert.Equal(t, 9000, items[2].ContainerPort)
	assert.Equal(t, "udp", items[2].Protocol)
	assert.False(t, items[2].IsPublished)
}

func TestPortService_ListPortsPaginated_SortsByHostPortWithUnpublishedLast(t *testing.T) {
	svc := NewPortService(newPortServiceTestDockerService(t, []dockercontainer.Summary{
		{
			ID:    "container-3000",
			Names: []string{"/api"},
			Ports: []dockercontainer.PortSummary{
				{IP: netip.MustParseAddr("127.0.0.1"), PrivatePort: 3000, PublicPort: 3000, Type: "tcp"},
			},
		},
		{
			ID:    "container-8080",
			Names: []string{"/web"},
			Ports: []dockercontainer.PortSummary{
				{IP: netip.MustParseAddr("0.0.0.0"), PrivatePort: 80, PublicPort: 8080, Type: "tcp"},
			},
		},
		{
			ID:    "container-unpublished",
			Names: []string{"/worker"},
			Ports: []dockercontainer.PortSummary{
				{PrivatePort: 9000, Type: "tcp"},
			},
		},
	}))

	items, _, err := svc.ListPortsPaginated(context.Background(), pagination.QueryParams{
		SortParams: pagination.SortParams{
			Sort:  "hostPort",
			Order: pagination.SortAsc,
		},
		PaginationParams: pagination.PaginationParams{Limit: 20},
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
	svc := NewPortService(newPortServiceTestDockerService(t, []dockercontainer.Summary{
		{
			ID:    "container-3000",
			Names: []string{"/api"},
			Ports: []dockercontainer.PortSummary{
				{IP: netip.MustParseAddr("127.0.0.1"), PrivatePort: 3000, PublicPort: 3000, Type: "tcp"},
			},
		},
		{
			ID:    "container-8080",
			Names: []string{"/web"},
			Ports: []dockercontainer.PortSummary{
				{IP: netip.MustParseAddr("0.0.0.0"), PrivatePort: 80, PublicPort: 8080, Type: "tcp"},
			},
		},
		{
			ID:    "container-unpublished",
			Names: []string{"/worker"},
			Ports: []dockercontainer.PortSummary{
				{PrivatePort: 9000, Type: "tcp"},
			},
		},
	}))

	items, _, err := svc.ListPortsPaginated(context.Background(), pagination.QueryParams{
		SortParams: pagination.SortParams{
			Sort:  "hostPort",
			Order: pagination.SortDesc,
		},
		PaginationParams: pagination.PaginationParams{Limit: 20},
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
	svc := NewPortService(newPortServiceTestDockerService(t, []dockercontainer.Summary{
		{
			ID:    "container-127",
			Names: []string{"/api"},
			Ports: []dockercontainer.PortSummary{
				{IP: netip.MustParseAddr("127.0.0.1"), PrivatePort: 3000, PublicPort: 3000, Type: "tcp"},
			},
		},
		{
			ID:    "container-000",
			Names: []string{"/web"},
			Ports: []dockercontainer.PortSummary{
				{IP: netip.MustParseAddr("0.0.0.0"), PrivatePort: 80, PublicPort: 8080, Type: "tcp"},
			},
		},
		{
			ID:    "container-unpublished",
			Names: []string{"/worker"},
			Ports: []dockercontainer.PortSummary{
				{PrivatePort: 9000, Type: "tcp"},
			},
		},
	}))

	items, _, err := svc.ListPortsPaginated(context.Background(), pagination.QueryParams{
		SortParams: pagination.SortParams{
			Sort:  "hostIp",
			Order: pagination.SortDesc,
		},
		PaginationParams: pagination.PaginationParams{Limit: 20},
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
