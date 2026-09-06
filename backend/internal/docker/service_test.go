package docker

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"regexp"
	"sync/atomic"
	"testing"
	"time"

	"github.com/getarcaneapp/arcane/backend/v2/internal/actors"
	"github.com/getarcaneapp/arcane/backend/v2/internal/config"
	imagetypes "github.com/getarcaneapp/arcane/types/v2/image"
	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/api/types/events"
	"github.com/moby/moby/api/types/image"
	"github.com/moby/moby/client"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/fx/fxtest"
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
	secondClient := svc.Client
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
	assert.Same(t, firstClient, svc.Client)
	assert.Equal(t, "1.41", svc.clientVersion)
	assert.False(t, svc.clientLastProbe.IsZero())
}

func TestDockerClientService_PublishesDaemonImageEventsInternal(t *testing.T) {
	svc := NewDockerClientService(t.Context(), nil, &config.Config{}, nil)
	t.Cleanup(svc.Close)
	eventCh, unsubscribe := svc.EventBus().Subscribe(events.ImageEventType)
	t.Cleanup(unsubscribe)
	messages := make(chan events.Message, 1)
	expected := events.Message{Type: events.ImageEventType, Action: events.ActionPull, TimeNano: 123, Actor: events.Actor{ID: "nginx:latest"}}
	messages <- expected
	close(messages)
	require.NoError(t, svc.consumeEventsInternal(t.Context(), messages, nil))
	select {
	case message := <-eventCh:
		require.Equal(t, expected, message)
	default:
		t.Fatal("daemon image event was not published")
	}
}

func TestDockerClientService_EventActorStopCancelsAndJoinsStreamInternal(t *testing.T) {
	streamStarted := make(chan struct{})
	streamStopped := make(chan struct{})
	server := newDockerPingTestServerWithHandlerInternal(t, func(w http.ResponseWriter, r *http.Request) {
		switch dockerTestPathInternal(r.URL.Path) {
		case "/_ping":
			w.Header().Set("Api-Version", "1.41")
			w.WriteHeader(http.StatusOK)
		case "/events":
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

	lifecycle := fxtest.NewLifecycle(t)
	actorRuntime, err := actors.NewRuntime(t.Context(), lifecycle)
	require.NoError(t, err)
	service := newDockerClientServiceForTestInternal(server.URL)
	t.Cleanup(service.Close)

	eventsCh, unsubscribe := service.EventBus().Subscribe(events.ImageEventType)
	t.Cleanup(unsubscribe)
	runner, err := actors.NewRunner(t.Context(), actorRuntime, "docker-events-test", "stream", "Docker event watcher", 3, func(ctx context.Context) error {
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

	stopCtx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	require.NoError(t, runner.Stop(stopCtx))
	require.Eventually(t, func() bool {
		select {
		case <-streamStopped:
			return true
		default:
			return false
		}
	}, time.Second, time.Millisecond)
	require.NoError(t, lifecycle.Stop(stopCtx))
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

func newDockerClientServiceForTestInternal(host string) *DockerClientService {
	return NewDockerClientService(context.Background(), nil, &config.Config{DockerHost: host}, nil)
}

func dockerTestPathInternal(path string) string {
	return regexp.MustCompile(`^/v[0-9]+\.[0-9]+`).ReplaceAllString(path, "")
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
