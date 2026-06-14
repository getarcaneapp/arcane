package projects

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	composetypes "github.com/compose-spec/compose-go/v2/types"
	"github.com/docker/compose/v5/pkg/api"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/libarcane"
	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/api/types/volume"
	"github.com/stretchr/testify/require"
)

func TestBuildProjectRenamedVolumeConfigInternal(t *testing.T) {
	source := composetypes.VolumeConfig{
		Name:   "nginx_data",
		Driver: "local",
		Labels: composetypes.Labels{
			"existing": "kept",
		},
		CustomLabels: composetypes.Labels{
			api.ProjectLabel: "nginx",
			api.VolumeLabel:  "data",
		},
	}

	got := buildProjectRenamedVolumeConfigInternal(source, "data", "web_data", "web")

	require.Equal(t, "web_data", got.Name)
	require.Equal(t, "local", got.Driver)
	require.Equal(t, "kept", got.Labels["existing"])
	require.Equal(t, "web", got.CustomLabels[api.ProjectLabel])
	require.Equal(t, "data", got.CustomLabels[api.VolumeLabel])
	require.Equal(t, api.ComposeVersion, got.CustomLabels[api.VersionLabel])
}

func TestContainerSummaryMountsVolumeInternal(t *testing.T) {
	summary := container.Summary{
		Labels: map[string]string{
			libarcane.InternalResourceLabel: "true",
		},
		Mounts: []container.MountPoint{
			{Name: "web_data", Source: "/var/lib/docker/volumes/web_data/_data"},
			{Name: "other", Source: "other"},
		},
	}

	require.True(t, libarcane.IsInternalContainer(summary.Labels))
	require.True(t, containerSummaryMountsVolumeInternal(summary, "web_data"))
	require.False(t, containerSummaryMountsVolumeInternal(summary, "nginx_data"))
}

func TestCreateProjectRenamedVolumeInternal_UsesPendingComposeDriverOptions(t *testing.T) {
	var payload map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/volumes/create") {
			require.NoError(t, json.NewDecoder(r.Body).Decode(&payload))
			w.Header().Set("Content-Type", "application/json")
			require.NoError(t, json.NewEncoder(w).Encode(volume.Volume{Name: "web_data"}))
			return
		}
		http.NotFound(w, r)
	}))
	t.Cleanup(server.Close)

	entry := projectVolumeRenameEntryInternal{
		Key:     "data",
		OldName: "nginx_data",
		NewName: "web_data",
		OldVolume: volume.Volume{
			Driver:  "old-driver",
			Options: map[string]string{"old": "true"},
			Labels:  map[string]string{"existing": "kept"},
		},
		NewConfig: composetypes.VolumeConfig{
			Name:       "web_data",
			Driver:     "new-driver",
			DriverOpts: map[string]string{"new": "true"},
			CustomLabels: composetypes.Labels{
				api.ProjectLabel: "web",
				api.VolumeLabel:  "data",
			},
		},
	}

	err := createProjectRenamedVolumeInternal(context.Background(), newTestDockerClient(t, server), entry)

	require.NoError(t, err)
	require.Equal(t, "web_data", payload["Name"])
	require.Equal(t, "new-driver", payload["Driver"])
	require.Equal(t, map[string]any{"new": "true"}, payload["DriverOpts"])
	labels, ok := payload["Labels"].(map[string]any)
	require.True(t, ok)
	require.Equal(t, "kept", labels["existing"])
	require.Equal(t, "web", labels[api.ProjectLabel])
	require.Equal(t, "data", labels[api.VolumeLabel])
	require.NotEmpty(t, labels[api.ConfigHashLabel])
}
