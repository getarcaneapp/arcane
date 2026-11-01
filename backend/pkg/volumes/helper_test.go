package volumes

import (
	"encoding/json/v2"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/moby/moby/api/types/container"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"getarcane.dev/arcane/v3/pkg/selfcontainer"
)

func TestResolveHelperImage_UsesLocalToolsImage(t *testing.T) {
	var pullCalls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && strings.Contains(r.URL.Path, "/images/") && strings.HasSuffix(r.URL.Path, "/json"):
			w.Header().Set("Content-Type", "application/json")
			if !assert.NoError(t, json.MarshalWrite(w, map[string]any{"Id": "tools-image"})) {
				return
			}
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/images/create"):
			pullCalls.Add(1)
			http.Error(w, "unexpected pull", http.StatusInternalServerError)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)

	image, err := ResolveHelperImage(t.Context(), newTestDockerClient(t, server), ToolsImage(""))

	require.NoError(t, err)
	require.Equal(t, ToolsImage(""), image)
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
			if !assert.Equal(t, "ghcr.io/getarcaneapp/tools", r.URL.Query().Get("fromImage")) {
				return
			}
			if !assert.Equal(t, "latest", r.URL.Query().Get("tag")) {
				return
			}
			w.Header().Set("Content-Type", "application/json")
			_, err := w.Write([]byte("{\"status\":\"pulled\"}\n"))
			if !assert.NoError(t, err) {
				return
			}
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)

	image, err := ResolveHelperImage(t.Context(), newTestDockerClient(t, server), ToolsImage(""))

	require.NoError(t, err)
	require.Equal(t, ToolsImage(""), image)
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
			if !assert.NoError(t, json.MarshalWrite(w, []container.Summary{
				{ID: "arcane-container", Image: "arcane:local", State: container.StateRunning},
			})) {
				return
			}
		case r.Method == http.MethodGet && strings.Contains(r.URL.Path, "/containers/arcane-container/") && strings.HasSuffix(r.URL.Path, "/json"):
			w.Header().Set("Content-Type", "application/json")
			if !assert.NoError(t, json.MarshalWrite(w, container.InspectResponse{
				ID: "arcane-container",
				Config: &container.Config{
					Image: "arcane:local",
				},
			})) {
				return
			}
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)

	image, err := ResolveHelperImage(t.Context(), newTestDockerClient(t, server), ToolsImage(""))

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
			if !assert.NoError(t, json.MarshalWrite(w, []container.Summary{})) {
				return
			}
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)

	image, err := ResolveHelperImage(t.Context(), newTestDockerClient(t, server), ToolsImage(""))

	require.Error(t, err)
	require.Empty(t, image)
	require.ErrorContains(t, err, "failed to resolve helper image")
	require.ErrorContains(t, err, "pull failed")
}

func TestIsHelperContainerUnlabeled(t *testing.T) {
	tests := []struct {
		name    string
		summary container.Summary
		want    bool
	}{
		{
			name: "unlabeled helper signature matches",
			summary: container.Summary{
				Labels: map[string]string{
					selfcontainer.InternalResourceLabel: "true",
				},
				Command: "sleep infinity",
				Mounts: []container.MountPoint{
					{Destination: "/volume"},
				},
			},
			want: true,
		},
		{
			name: "internal trivy-like helper is not treated as an unlabeled volume helper",
			summary: container.Summary{
				Labels: map[string]string{
					selfcontainer.InternalResourceLabel: "true",
				},
				Command: "trivy image --quiet alpine:latest",
				Mounts: []container.MountPoint{
					{Destination: "/var/run/docker.sock"},
				},
			},
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, IsHelperContainer(tt.summary))
		})
	}
}

func TestIsHelperContainerUsesExplicitHelperLabel(t *testing.T) {
	tests := []struct {
		name    string
		summary container.Summary
		want    bool
	}{
		{
			name: "new helper label matches",
			summary: container.Summary{
				Labels: map[string]string{
					selfcontainer.InternalResourceLabel: "true",
					ContainerLabel:                      "true",
				},
			},
			want: true,
		},
		{
			name: "generic internal volume mount does not match",
			summary: container.Summary{
				Labels: map[string]string{
					selfcontainer.InternalResourceLabel: "true",
				},
				Mounts: []container.MountPoint{
					{Destination: "/volume"},
				},
			},
			want: false,
		},
		{
			name: "unlabeled helper still matches",
			summary: container.Summary{
				Labels: map[string]string{
					selfcontainer.InternalResourceLabel: "true",
				},
				Command: "sleep infinity",
				Mounts: []container.MountPoint{
					{Destination: "/volume"},
				},
			},
			want: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, IsHelperContainer(tt.summary))
		})
	}
}
