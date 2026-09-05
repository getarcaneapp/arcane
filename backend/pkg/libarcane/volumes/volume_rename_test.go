package volumes

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/getarcaneapp/arcane/backend/v2/pkg/libarcane"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/libarcane/volumehelper"
	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/api/types/volume"
	"github.com/moby/moby/client"
	"github.com/stretchr/testify/require"
)

func newTestDockerClientInternal(t *testing.T, server *httptest.Server) *client.Client {
	t.Helper()

	httpClient := server.Client()
	cli, err := client.New(
		client.WithHost(server.URL),
		client.WithAPIVersion("1.41"),
		client.WithHTTPClient(httpClient),
	)
	require.NoError(t, err)

	t.Cleanup(func() {
		_ = cli.Close()
	})

	return cli
}

func TestPlanRenamePreservesStandaloneVolumeConfiguration(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/volumes/source-data"):
			w.Header().Set("Content-Type", "application/json")
			require.NoError(t, json.NewEncoder(w).Encode(volume.Volume{
				Name:    "source-data",
				Driver:  "local",
				Options: map[string]string{"type": "none", "device": "/srv/data"},
				Labels:  map[string]string{"owner": "arcane"},
			}))
		case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/containers/json"):
			w.Header().Set("Content-Type", "application/json")
			require.NoError(t, json.NewEncoder(w).Encode([]container.Summary{}))
		case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/volumes/renamed-data"):
			http.NotFound(w, r)
		default:
			http.Error(w, "unexpected Docker request: "+r.Method+" "+r.URL.Path, http.StatusInternalServerError)
		}
	}))
	t.Cleanup(server.Close)

	migration, err := PlanRename(t.Context(), newTestDockerClientInternal(t, server), "source-data", "renamed-data", volumehelper.ToolsImage(""))
	require.NoError(t, err)

	planned, ok := migration.(*dockerProjectVolumeRenameMigrationInternal)
	require.True(t, ok)
	require.Len(t, planned.entries, 1)
	entry := planned.entries[0]
	require.Equal(t, "source-data", entry.OldName)
	require.Equal(t, "renamed-data", entry.NewName)
	require.Equal(t, "renamed-data", entry.CreateOptions.Name)
	require.Equal(t, "local", entry.CreateOptions.Driver)
	require.Equal(t, map[string]string{"type": "none", "device": "/srv/data"}, entry.CreateOptions.DriverOpts)
	require.Equal(t, map[string]string{"owner": "arcane"}, entry.CreateOptions.Labels)
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
