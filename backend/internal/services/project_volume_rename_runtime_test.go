package services

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/moby/moby/api/types/container"
	"github.com/stretchr/testify/require"
)

func TestGetProjectVolumeCopyRuntimeInternal_UsesEntrypointBeforeCmd(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/containers/json"):
			w.Header().Set("Content-Type", "application/json")
			require.NoError(t, json.NewEncoder(w).Encode([]container.Summary{
				{
					ID:    "arcane-container",
					Image: "arcane:local",
					State: container.StateRunning,
				},
			}))
		case strings.Contains(r.URL.Path, "/containers/arcane-container/") && strings.HasSuffix(r.URL.Path, "/json"):
			w.Header().Set("Content-Type", "application/json")
			require.NoError(t, json.NewEncoder(w).Encode(container.InspectResponse{
				ID: "arcane-container",
				Config: &container.Config{
					Image:      "arcane:local",
					Entrypoint: []string{"./arcane"},
					Cmd:        []string{"server"},
				},
			}))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)

	copyRuntime, err := getProjectVolumeCopyRuntimeInternal(context.Background(), newTestDockerClient(t, server))

	require.NoError(t, err)
	require.Equal(t, "arcane:local", copyRuntime.Image)
	require.Equal(t, []string{"./arcane"}, copyRuntime.Command)
	require.Equal(t, "arcane-label", copyRuntime.Source)
}

func TestCreateProjectVolumeCopyHolderContainerInternal_OverridesEntrypointWithHelperArgs(t *testing.T) {
	var createBody map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/containers/create"):
			require.NoError(t, json.NewDecoder(r.Body).Decode(&createBody))
			w.Header().Set("Content-Type", "application/json")
			require.NoError(t, json.NewEncoder(w).Encode(map[string]any{
				"Id":       "helper-container",
				"Warnings": []string{},
			}))
		case r.Method == http.MethodDelete && strings.Contains(r.URL.Path, "/containers/helper-container"):
			w.WriteHeader(http.StatusNoContent)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)

	_, cleanup, err := createProjectVolumeCopyHolderContainerInternal(
		context.Background(),
		newTestDockerClient(t, server),
		projectVolumeCopyRuntimeInternal{Image: "arcane:local", Command: []string{"./arcane"}},
		"nginx_data",
		true,
	)
	require.NoError(t, err)
	defer cleanup()

	require.Equal(t, []any{"./arcane"}, createBody["Entrypoint"])
	require.Equal(t, []any{"internal-volume-helper", "probe", "--path", projectVolumeCopyMountPathInternal}, createBody["Cmd"])
}
