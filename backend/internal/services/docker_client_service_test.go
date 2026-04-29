package services

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/getarcaneapp/arcane/backend/internal/config"
	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/api/types/image"
	"github.com/moby/moby/client"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewDockerClient_PinsEffectiveAPIVersion(t *testing.T) {
	t.Setenv("DOCKER_API_VERSION", "1.54")
	t.Setenv("DOCKER_HOST", "tcp://docker-from-env:2375")

	tests := []struct {
		name            string
		pingAPIVersion  string
		expectedVersion string
	}{
		{
			name:            "uses server API version",
			pingAPIVersion:  "1.41",
			expectedVersion: "1.41",
		},
		{
			name:            "falls back to minimum API version when ping version is empty",
			pingAPIVersion:  "",
			expectedVersion: client.MinAPIVersion,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := newDockerPingTestServerInternal(t, tt.pingAPIVersion)

			cli, err := newDockerClientInternal(context.Background(), server.URL)
			require.NoError(t, err)
			t.Cleanup(func() {
				_ = cli.Close()
			})

			assert.Equal(t, server.URL, cli.DaemonHost())
			assert.Equal(t, tt.expectedVersion, cli.ClientVersion())
			assert.NotEqual(t, client.MaxAPIVersion, cli.ClientVersion())
		})
	}
}

func TestDockerClientService_GetClientReturnsCachedClientUntilRefresh(t *testing.T) {
	server := newDockerPingTestServerInternal(t, "1.41")
	svc := newDockerClientServiceForTestInternal(server.URL)

	firstClient, err := svc.GetClient(context.Background())
	require.NoError(t, err)
	t.Cleanup(func() {
		_ = firstClient.Close()
	})

	secondClient, err := svc.GetClient(context.Background())
	require.NoError(t, err)

	assert.Same(t, firstClient, secondClient)
	assert.Equal(t, "1.41", secondClient.ClientVersion())
}

func TestDockerClientService_RefreshClientRecreatesCachedClientAfterAPIVersionChange(t *testing.T) {
	apiVersion := atomic.Value{}
	apiVersion.Store("1.41")
	server := newDockerPingTestServerWithVersionInternal(t, func() string {
		return apiVersion.Load().(string)
	})
	svc := newDockerClientServiceForTestInternal(server.URL)

	firstClient, err := svc.GetClient(context.Background())
	require.NoError(t, err)
	t.Cleanup(func() {
		_ = firstClient.Close()
	})
	assert.Equal(t, "1.41", firstClient.ClientVersion())

	apiVersion.Store("1.42")

	err = svc.RefreshClient(context.Background())
	require.NoError(t, err)
	secondClient := svc.client
	t.Cleanup(func() {
		_ = secondClient.Close()
	})

	assert.NotSame(t, firstClient, secondClient)
	assert.Equal(t, "1.42", secondClient.ClientVersion())
}

func TestDockerClientService_RefreshClientClosesOldCachedClientWhenReplaced(t *testing.T) {
	var oldServerClosedConnections atomic.Int32
	oldServer := newDockerPingTestServerWithConnStateInternal(t, "1.41", func(_ net.Conn, state http.ConnState) {
		if state == http.StateClosed {
			oldServerClosedConnections.Add(1)
		}
	})
	newServer := newDockerPingTestServerInternal(t, "1.42")
	svc := newDockerClientServiceForTestInternal(oldServer.URL)

	firstClient, err := svc.GetClient(context.Background())
	require.NoError(t, err)
	t.Cleanup(func() {
		_ = firstClient.Close()
	})

	_, err = firstClient.Ping(context.Background(), client.PingOptions{})
	require.NoError(t, err)
	closedBeforeReplace := oldServerClosedConnections.Load()

	svc.config.DockerHost = newServer.URL

	err = svc.RefreshClient(context.Background())
	require.NoError(t, err)
	secondClient := svc.client
	t.Cleanup(func() {
		_ = secondClient.Close()
	})

	require.Eventually(t, func() bool {
		return oldServerClosedConnections.Load() > closedBeforeReplace
	}, time.Second, 10*time.Millisecond)
	assert.NotSame(t, firstClient, secondClient)
	assert.Equal(t, "1.42", secondClient.ClientVersion())
}

func TestDockerClientService_RefreshClientProbeFailureKeepsCachedClient(t *testing.T) {
	var failProbe atomic.Bool
	server := newDockerPingTestServerWithHandlerInternal(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/_ping" {
			http.NotFound(w, r)
			return
		}
		if failProbe.Load() {
			w.WriteHeader(http.StatusForbidden)
			return
		}
		w.Header().Set("Api-Version", "1.41")
		w.WriteHeader(http.StatusOK)
	})
	svc := newDockerClientServiceForTestInternal(server.URL)

	firstClient, err := svc.GetClient(context.Background())
	require.NoError(t, err)
	t.Cleanup(func() {
		_ = firstClient.Close()
	})

	failProbe.Store(true)

	err = svc.RefreshClient(context.Background())
	require.Error(t, err)
	assert.Same(t, firstClient, svc.client)
	assert.Equal(t, "1.41", svc.clientVersion)
	assert.False(t, svc.clientLastProbe.IsZero())
}

func TestCountImageUsage_UsesContainerImageIDs(t *testing.T) {
	images := []image.Summary{
		{ID: "sha256:image-a", Containers: -1},
		{ID: "sha256:image-b", Containers: 0},
		{ID: "sha256:image-c", Containers: 99},
	}

	containers := []container.Summary{
		{ImageID: "sha256:image-a"},
		{ImageID: "sha256:image-c"},
		{ImageID: "sha256:image-a"}, // duplicate container ref should not affect counts
		{ImageID: ""},
	}

	inuse, unused, total := countImageUsageInternal(images, containers)

	assert.Equal(t, 2, inuse)
	assert.Equal(t, 1, unused)
	assert.Equal(t, 3, total)
}

func TestCountImageUsage_NoImages(t *testing.T) {
	inuse, unused, total := countImageUsageInternal(nil, []container.Summary{{ImageID: "sha256:image-a"}})

	assert.Equal(t, 0, inuse)
	assert.Equal(t, 0, unused)
	assert.Equal(t, 0, total)
}

func newDockerClientServiceForTestInternal(host string) *DockerClientService {
	return NewDockerClientService(nil, &config.Config{DockerHost: host}, nil)
}

func newDockerPingTestServerInternal(t *testing.T, apiVersion string) *httptest.Server {
	t.Helper()

	return newDockerPingTestServerWithVersionInternal(t, func() string {
		return apiVersion
	})
}

func newDockerPingTestServerWithVersionInternal(t *testing.T, apiVersion func() string) *httptest.Server {
	t.Helper()

	return newDockerPingTestServerWithHandlerInternal(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/_ping" {
			http.NotFound(w, r)
			return
		}

		if version := apiVersion(); version != "" {
			w.Header().Set("Api-Version", version)
		}
		w.WriteHeader(http.StatusOK)
	})
}

func newDockerPingTestServerWithConnStateInternal(t *testing.T, apiVersion string, connState func(net.Conn, http.ConnState)) *httptest.Server {
	t.Helper()

	server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/_ping" {
			http.NotFound(w, r)
			return
		}

		w.Header().Set("Api-Version", apiVersion)
		w.WriteHeader(http.StatusOK)
	}))
	server.Config.ConnState = connState
	server.Start()
	t.Cleanup(server.Close)

	return server
}

func newDockerPingTestServerWithHandlerInternal(t *testing.T, handler http.HandlerFunc) *httptest.Server {
	t.Helper()

	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)

	return server
}
