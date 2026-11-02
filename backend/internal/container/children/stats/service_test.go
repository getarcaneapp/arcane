package stats

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/moby/moby/client"
	"github.com/stretchr/testify/require"

	"getarcane.dev/arcane/v3/pkg/authz"
	"getarcane.dev/types/v3"
)

func TestResourceSortPermissionCheck(t *testing.T) {
	listOnly := authz.NewPermissionSet()
	listOnly.AddEnv("env-1", authz.PermContainersList)
	full := authz.NewPermissionSet()
	full.AddEnv("env-1", authz.PermContainersList, authz.PermContainersRead)

	require.True(t, ContainerResourceSortPermissionDenied(listOnly, "env-1", types.ContainerSortCPUUsage))
	require.True(t, ContainerResourceSortPermissionDenied(listOnly, "env-1", types.ContainerSortMemoryUsage))
	require.False(t, ContainerResourceSortPermissionDenied(full, "env-1", types.ContainerSortMemoryUsage))
	require.False(t, ContainerResourceSortPermissionDenied(listOnly, "env-1", "name"),
		"non-resource sorts keep the endpoint's containers:list requirement")
	require.False(t, ContainerResourceSortPermissionDenied(authz.SudoPermissionSet(), "env-1", types.ContainerSortCPUUsage))
}

func TestResourceSortComparatorKeepsUnavailableLast(t *testing.T) {
	asc := ContainerResourceSampleSort(types.ContainerSortMemoryUsage, false)
	desc := ContainerResourceSampleSort(types.ContainerSortMemoryUsage, true)
	sampled := types.ContainerSummary{
		ID:             "b",
		Names:          []string{"b"},
		ResourceSample: &types.ContainerResourceSample{MemoryUsageBytes: 1},
	}
	unsampled := types.ContainerSummary{ID: "a", Names: []string{"a"}}

	require.Equal(t, 1, asc(unsampled, sampled))
	require.Equal(t, -1, asc(sampled, unsampled))
	require.Equal(t, 1, desc(unsampled, sampled))
	require.Equal(t, -1, desc(sampled, unsampled))

	zero := types.ContainerSummary{
		ID:             "z",
		Names:          []string{"z"},
		ResourceSample: &types.ContainerResourceSample{MemoryUsageBytes: 0},
	}
	require.Equal(t, -1, asc(zero, unsampled), "zero is a valid value, not unavailable")
	require.Equal(t, -1, asc(zero, sampled))
}

func TestCollectMaxAge(t *testing.T) {
	tests := []struct {
		name      string
		maxAge    time.Duration
		wantCalls int32
	}{
		{name: "TTL reuses the cached sample", maxAge: ContainerResourceSampleTTL, wantCalls: 1},
		{name: "maxAge older than the cached sample refetches", maxAge: time.Nanosecond, wantCalls: 2},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var calls atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if !strings.HasSuffix(r.URL.Path, "/containers/web/stats") {
					http.NotFound(w, r)
					return
				}
				calls.Add(1)
				_, _ = io.WriteString(w, `{"read":"2026-01-01T00:00:00Z","memory_stats":{"usage":100,"limit":1000}}`)
			}))
			t.Cleanup(server.Close)
			dockerClient, err := client.New(
				client.WithHost(server.URL),
				client.WithAPIVersion("1.41"),
				client.WithHTTPClient(server.Client()),
			)
			require.NoError(t, err)
			t.Cleanup(func() { _ = dockerClient.Close() })

			svc := New(Params{
				GetClient:  func(context.Context) (*client.Client, error) { return dockerClient, nil },
				DockerHost: func() string { return "test" },
				CacheTTL:   ContainerResourceSampleTTL,
			})
			for range 2 {
				samples, collectErr := svc.Collect(t.Context(), []types.ContainerSummary{{ID: "web", State: "running"}}, tt.maxAge, true)
				require.NoError(t, collectErr)
				require.Contains(t, samples, "web")
			}
			require.Equal(t, tt.wantCalls, calls.Load())
		})
	}
}

func TestCollectReportsIOAndPIDs(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "/containers/web/stats") {
			http.NotFound(w, r)
			return
		}
		_, _ = io.WriteString(w, `{
			"read":"2026-01-01T00:00:00Z",
			"memory_stats":{"usage":100,"limit":1000},
			"pids_stats":{"current":7},
			"networks":{"eth0":{"rx_bytes":10,"tx_bytes":20},"eth1":{"rx_bytes":1,"tx_bytes":2}},
			"blkio_stats":{"io_service_bytes_recursive":[
				{"major":8,"minor":0,"op":"Read","value":300},
				{"major":8,"minor":16,"op":"read","value":5},
				{"major":8,"minor":0,"op":"Write","value":400},
				{"major":8,"minor":0,"op":"Total","value":705}
			]}
		}`)
	}))
	t.Cleanup(server.Close)
	dockerClient, err := client.New(
		client.WithHost(server.URL),
		client.WithAPIVersion("1.41"),
		client.WithHTTPClient(server.Client()),
	)
	require.NoError(t, err)
	t.Cleanup(func() { _ = dockerClient.Close() })

	svc := New(Params{
		GetClient:  func(context.Context) (*client.Client, error) { return dockerClient, nil },
		DockerHost: func() string { return "test" },
		CacheTTL:   ContainerResourceSampleTTL,
	})
	samples, err := svc.Collect(t.Context(), []types.ContainerSummary{{ID: "web", State: "running"}}, ContainerResourceSampleTTL, false)
	require.NoError(t, err)
	require.Contains(t, samples, "web")
	sample := samples["web"]
	require.Equal(t, uint64(305), sample.BlockReadBytes)
	require.Equal(t, uint64(400), sample.BlockWriteBytes)
	require.Equal(t, uint64(11), sample.NetworkRxBytes)
	require.Equal(t, uint64(22), sample.NetworkTxBytes)
	require.Equal(t, uint64(7), sample.PIDs)
}
