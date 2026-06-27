package volumehelper

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/getarcaneapp/arcane/backend/v2/pkg/libarcane"
	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/client"
	"github.com/stretchr/testify/require"
)

func TestLabels(t *testing.T) {
	labels := Labels()

	require.Equal(t, "true", labels[libarcane.InternalResourceLabel])
	require.Equal(t, "true", labels[ContainerLabel])
	require.Len(t, labels, 2)
}

func TestResolveHelperImage_UsesLocalToolsImage(t *testing.T) {
	var pullCalls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && strings.Contains(r.URL.Path, "/images/") && strings.HasSuffix(r.URL.Path, "/json"):
			w.Header().Set("Content-Type", "application/json")
			require.NoError(t, json.NewEncoder(w).Encode(map[string]any{"Id": "tools-image"}))
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/images/create"):
			pullCalls.Add(1)
			http.Error(w, "unexpected pull", http.StatusInternalServerError)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)

	image, err := ResolveHelperImage(context.Background(), newTestDockerClientInternal(t, server))

	require.NoError(t, err)
	require.Equal(t, DefaultToolsImage, image)
	require.Zero(t, pullCalls.Load())
}

func TestResolveHelperImage_PullsToolsImageWhenMissing(t *testing.T) {
	var pullCalls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && strings.Contains(r.URL.Path, "/images/") && strings.HasSuffix(r.URL.Path, "/json"):
			http.NotFound(w, r)
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/images/create"):
			pullCalls.Add(1)
			require.Equal(t, "ghcr.io/getarcaneapp/tools", r.URL.Query().Get("fromImage"))
			require.Equal(t, "latest", r.URL.Query().Get("tag"))
			w.Header().Set("Content-Type", "application/json")
			_, err := w.Write([]byte(`{"status":"pulled"}` + "\n"))
			require.NoError(t, err)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)

	image, err := ResolveHelperImage(context.Background(), newTestDockerClientInternal(t, server))

	require.NoError(t, err)
	require.Equal(t, DefaultToolsImage, image)
	require.EqualValues(t, 1, pullCalls.Load())
}

func TestResolveHelperImage_FallsBackToArcaneRuntimeWhenToolsPullFails(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && strings.Contains(r.URL.Path, "/images/") && strings.HasSuffix(r.URL.Path, "/json"):
			http.NotFound(w, r)
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/images/create"):
			http.Error(w, "pull failed", http.StatusInternalServerError)
		case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/containers/json"):
			w.Header().Set("Content-Type", "application/json")
			require.NoError(t, json.NewEncoder(w).Encode([]container.Summary{
				{ID: "arcane-container", Image: "arcane:local", State: container.StateRunning},
			}))
		case r.Method == http.MethodGet && strings.Contains(r.URL.Path, "/containers/arcane-container/") && strings.HasSuffix(r.URL.Path, "/json"):
			w.Header().Set("Content-Type", "application/json")
			require.NoError(t, json.NewEncoder(w).Encode(container.InspectResponse{
				ID: "arcane-container",
				Config: &container.Config{
					Image: "arcane:local",
				},
			}))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)

	image, err := ResolveHelperImage(context.Background(), newTestDockerClientInternal(t, server))

	require.NoError(t, err)
	require.Equal(t, "arcane:local", image)
}

func TestResolveHelperImage_ReturnsPullErrorWhenNoFallbackExists(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && strings.Contains(r.URL.Path, "/images/") && strings.HasSuffix(r.URL.Path, "/json"):
			http.NotFound(w, r)
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/images/create"):
			http.Error(w, "pull failed", http.StatusInternalServerError)
		case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/containers/json"):
			w.Header().Set("Content-Type", "application/json")
			require.NoError(t, json.NewEncoder(w).Encode([]container.Summary{}))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)

	image, err := ResolveHelperImage(context.Background(), newTestDockerClientInternal(t, server))

	require.Error(t, err)
	require.Empty(t, image)
	require.ErrorContains(t, err, "failed to resolve helper image")
	require.ErrorContains(t, err, "pull failed")
}

func newTestDockerClientInternal(t *testing.T, server *httptest.Server) *client.Client {
	t.Helper()

	dockerClient, err := client.NewClientWithOpts(
		client.WithHost(server.URL),
		client.WithVersion("1.54"),
	)
	require.NoError(t, err)
	return dockerClient
}
