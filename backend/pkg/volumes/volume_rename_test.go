package volumes

import (
	"encoding/json/v2"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/api/types/volume"
	"github.com/moby/moby/client"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newTestDockerClient(t *testing.T, server *httptest.Server) *client.Client {
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
			if !assert.NoError(t, json.MarshalWrite(w, volume.Volume{
				Name:    "source-data",
				Driver:  "local",
				Options: map[string]string{"type": "none", "device": "/srv/data"},
				Labels:  map[string]string{"owner": "arcane"},
			})) {
				return
			}
		case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/containers/json"):
			w.Header().Set("Content-Type", "application/json")
			if !assert.NoError(t, json.MarshalWrite(w, []container.Summary{})) {
				return
			}
		case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/volumes/renamed-data"):
			http.NotFound(w, r)
		default:
			http.Error(w, "unexpected Docker request: "+r.Method+" "+r.URL.Path, http.StatusInternalServerError)
		}
	}))
	t.Cleanup(server.Close)

	migration, err := PlanRename(t.Context(), PlanRenameConfig{
		DockerClient: newTestDockerClient(t, server),
		OldName:      "source-data",
		NewName:      "renamed-data",
		ToolsImage:   ToolsImage(""),
	})
	require.NoError(t, err)

	planned, ok := migration.(*dockerProjectVolumeRenameMigration)
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
