package docker

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"path"
	"sync/atomic"
	"testing"
	"time"

	imagetypes "github.com/getarcaneapp/arcane/types/v2/image"
	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/api/types/events"
	"github.com/moby/moby/api/types/image"
	"github.com/moby/moby/client"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/getarcaneapp/arcane/backend/v2/internal/config"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/utils/concurrency"
)

func TestDockerClientService_GetClientPinsEffectiveAPIVersion(t *testing.T) {
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
			server := newDockerPingTestServer(t, func() string { return tt.pingAPIVersion })
			svc := newDockerClientServiceForTest(t, server.URL)
			t.Cleanup(svc.Close)

			cli, err := svc.GetClient(t.Context())
			require.NoError(t, err)

			assert.Equal(t, server.URL, cli.DaemonHost())
			assert.Equal(t, tt.expectedVersion, cli.ClientVersion())
			assert.NotEqual(t, client.MaxAPIVersion, cli.ClientVersion())
		})
	}
}

func TestDockerClientService_GetClientReturnsCachedClientUntilRefresh(t *testing.T) {
	server := newDockerPingTestServer(t, func() string { return "1.41" })
	svc := newDockerClientServiceForTest(t, server.URL)

	firstClient, err := svc.GetClient(t.Context())
	require.NoError(t, err)
	t.Cleanup(func() {
		_ = firstClient.Close()
	})

	secondClient, err := svc.GetClient(t.Context())
	require.NoError(t, err)

	assert.Same(t, firstClient, secondClient)
	assert.Equal(t, "1.41", secondClient.ClientVersion())
}

func TestDockerClientService_RefreshClientRecreatesCachedClientAfterAPIVersionChange(t *testing.T) {
	apiVersion := atomic.Value{}
	apiVersion.Store("1.41")
	server := newDockerPingTestServer(t, func() string {
		return apiVersion.Load().(string)
	})
	svc := newDockerClientServiceForTest(t, server.URL)

	firstClient, err := svc.GetClient(t.Context())
	require.NoError(t, err)
	t.Cleanup(func() {
		_ = firstClient.Close()
	})
	assert.Equal(t, "1.41", firstClient.ClientVersion())

	apiVersion.Store("1.42")

	err = svc.RefreshClient(t.Context())
	require.NoError(t, err)
	secondClient := svc.Client
	t.Cleanup(func() {
		_ = secondClient.Close()
	})

	assert.NotSame(t, firstClient, secondClient)
	assert.Equal(t, "1.42", secondClient.ClientVersion())
}

func TestDockerClientService_RefreshClientClosesOldCachedClientWhenReplaced(t *testing.T) {
	var oldServerClosedConnections atomic.Int32
	oldServer := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/_ping" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Api-Version", "1.41")
		w.WriteHeader(http.StatusOK)
	}))
	oldServer.Config.ConnState = func(_ net.Conn, state http.ConnState) {
		if state == http.StateClosed {
			oldServerClosedConnections.Add(1)
		}
	}
	oldServer.Start()
	t.Cleanup(oldServer.Close)
	newServer := newDockerPingTestServer(t, func() string { return "1.42" })
	svc := newDockerClientServiceForTest(t, oldServer.URL)

	firstClient, err := svc.GetClient(t.Context())
	require.NoError(t, err)
	t.Cleanup(func() {
		_ = firstClient.Close()
	})

	_, err = firstClient.Ping(t.Context(), client.PingOptions{})
	require.NoError(t, err)
	closedBeforeReplace := oldServerClosedConnections.Load()

	svc.config.DockerHost = newServer.URL

	err = svc.RefreshClient(t.Context())
	require.NoError(t, err)
	secondClient := svc.Client
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
	server := newDockerPingTestServerWithHandler(t, func(w http.ResponseWriter, r *http.Request) {
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
	svc := newDockerClientServiceForTest(t, server.URL)

	firstClient, err := svc.GetClient(t.Context())
	require.NoError(t, err)
	t.Cleanup(func() {
		_ = firstClient.Close()
	})

	failProbe.Store(true)

	err = svc.RefreshClient(t.Context())
	require.Error(t, err)
	assert.Same(t, firstClient, svc.Client)
	assert.Equal(t, "1.41", svc.clientVersion)
	assert.False(t, svc.clientLastProbe.IsZero())
}

func TestDockerClientService_PublishesDaemonImageEvents(t *testing.T) {
	expected := events.Message{Type: events.ImageEventType, Action: events.ActionPull, TimeNano: 123, Actor: events.Actor{ID: "nginx:latest"}}
	payload, err := json.Marshal(expected)
	require.NoError(t, err)
	server := newDockerPingTestServerWithHandler(t, func(w http.ResponseWriter, r *http.Request) {
		switch path.Base(r.URL.Path) {
		case "_ping":
			w.Header().Set("Api-Version", "1.41")
			w.WriteHeader(http.StatusOK)
		case "events":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write(payload)
		default:
			http.NotFound(w, r)
		}
	})
	svc := newDockerClientServiceForTest(t, server.URL)
	t.Cleanup(svc.Close)
	eventCh, unsubscribe := svc.EventBus().Subscribe(events.ImageEventType)
	t.Cleanup(unsubscribe)
	stop, err := concurrency.StartSupervised(t.Context(), "Docker event watcher", func(ctx context.Context) error {
		svc.WatchEvents(ctx)
		return nil
	})
	require.NoError(t, err)
	t.Cleanup(func() { _ = stop(context.WithoutCancel(t.Context())) })

	select {
	case message := <-eventCh:
		require.Equal(t, expected, message)
	case <-time.After(time.Second):
		t.Fatal("daemon image event was not published")
	}
}

func TestDockerClientService_EventActorStopCancelsAndJoinsStream(t *testing.T) {
	streamStarted := make(chan struct{})
	streamStopped := make(chan struct{})
	server := newDockerPingTestServerWithHandler(t, func(w http.ResponseWriter, r *http.Request) {
		switch path.Base(r.URL.Path) {
		case "_ping":
			w.Header().Set("Api-Version", "1.41")
			w.WriteHeader(http.StatusOK)
		case "events":
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			if flusher, ok := w.(http.Flusher); ok {
				flusher.Flush()
			}
			close(streamStarted)
			<-r.Context().Done()
			close(streamStopped)
		default:
			http.NotFound(w, r)
		}
	})

	service := newDockerClientServiceForTest(t, server.URL)
	t.Cleanup(service.Close)

	eventsCh, unsubscribe := service.EventBus().Subscribe(events.ImageEventType)
	t.Cleanup(unsubscribe)
	stop, err := concurrency.StartSupervised(t.Context(), "Docker event watcher", func(ctx context.Context) error {
		service.WatchEvents(ctx)
		return nil
	})
	require.NoError(t, err)

	select {
	case <-streamStarted:
	case <-time.After(time.Second):
		require.FailNow(t, "Docker event stream did not start")
	}
	select {
	case message := <-eventsCh:
		t.Fatalf("connecting without daemon events published an unexpected event: %v", message)
	default:
	}

	stopCtx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	require.NoError(t, stop(stopCtx))
	require.Eventually(t, func() bool {
		select {
		case <-streamStopped:
			return true
		default:
			return false
		}
	}, time.Second, time.Millisecond)
}

func TestCountImageUsage_UsesContainerImageIDs(t *testing.T) {
	images := []image.Summary{
		{ID: "sha256:image-a", Containers: -1, Size: 100},
		{ID: "sha256:image-b", Containers: 0, Size: 25},
		{ID: "sha256:image-c", Containers: 99, Size: 3},
	}

	containers := []container.Summary{
		{ImageID: "sha256:image-a"},
		{ImageID: "sha256:image-c"},
		{ImageID: "sha256:image-a"}, // duplicate container ref should not affect counts
		{ImageID: ""},
	}

	counts := CountImageUsage(images, containers)

	assert.Equal(t, 2, counts.Inuse)
	assert.Equal(t, 1, counts.Unused)
	assert.Equal(t, 3, counts.Total)
	assert.Equal(t, int64(128), counts.TotalSize)
}

func TestCountImageUsage_NoImages(t *testing.T) {
	counts := CountImageUsage(nil, []container.Summary{{ImageID: "sha256:image-a"}})

	assert.Equal(t, imagetypes.UsageCounts{}, counts)
}

func newDockerClientServiceForTest(t *testing.T, host string) *DockerClientService {
	return NewDockerClientService(t.Context(), nil, &config.Config{DockerHost: host}, nil)
}

func newDockerPingTestServer(t *testing.T, apiVersion func() string) *httptest.Server {
	t.Helper()

	return newDockerPingTestServerWithHandler(t, func(w http.ResponseWriter, r *http.Request) {
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

func newDockerPingTestServerWithHandler(t *testing.T, handler http.HandlerFunc) *httptest.Server {
	t.Helper()

	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)

	return server
}
