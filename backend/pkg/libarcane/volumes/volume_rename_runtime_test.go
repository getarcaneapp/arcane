package volumes

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/getarcaneapp/arcane/backend/v2/pkg/libarcane/volumehelper"
	"github.com/stretchr/testify/require"
)

func TestGetProjectVolumeCopyRuntimeInternal_UsesToolsImageWhenAvailable(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && strings.Contains(r.URL.Path, "/images/") && strings.HasSuffix(r.URL.Path, "/json"):
			w.Header().Set("Content-Type", "application/json")
			require.NoError(t, json.NewEncoder(w).Encode(map[string]any{"Id": "tools-image"}))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)

	copyRuntime, err := getProjectVolumeCopyRuntimeInternal(context.Background(), newTestDockerClient(t, server))

	require.NoError(t, err)
	require.Equal(t, volumehelper.DefaultToolsImage, copyRuntime.Image)
}

func TestCreateProjectVolumeCopyHolderContainerInternal_UsesPassiveHolderCommand(t *testing.T) {
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
		projectVolumeCopyRuntimeInternal{Image: "arcane:local"},
		"nginx_data",
		true,
	)
	require.NoError(t, err)
	defer cleanup()

	require.Empty(t, createBody["Entrypoint"])
	require.Equal(t, []any{"sleep", "infinity"}, createBody["Cmd"])
}
