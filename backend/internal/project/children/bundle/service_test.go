package bundle

import (
	"context"
	"encoding/json/v2"
	"errors"
	"net/http"
	"net/http/httptest"
	"path"
	"slices"
	"strings"
	"sync"
	"testing"

	composetypes "github.com/compose-spec/compose-go/v2/types"
	composeapi "github.com/docker/compose/v5/pkg/api"
	"github.com/libtnb/sqlite"
	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/api/types/mount"
	"github.com/moby/moby/client"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"kit.getarcane.app/docker/streams/bus"

	"getarcane.dev/arcane/v3/internal/config"
	"getarcane.dev/arcane/v3/internal/database"
	"getarcane.dev/arcane/v3/internal/docker"
	"getarcane.dev/arcane/v3/internal/settings"
)

func TestContainersStopAndStartOnlyThisProject(t *testing.T) {
	labelsFor := func(project, service string) map[string]string {
		return map[string]string{composeapi.ProjectLabel: project, composeapi.ServiceLabel: service}
	}
	arcane := labelsFor("demo", "arcane")
	arcane["com.getarcaneapp.arcane"] = "true"
	var (
		mu      sync.Mutex
		stopped []string
		started []string
	)
	states := map[string]container.ContainerState{
		"web-1": container.StateRunning, "db-1": container.StateRestarting, "cache-1": container.StatePaused,
		"arcane-1": container.StateRunning, "other-1": container.StateRunning,
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		switch {
		case strings.HasSuffix(r.URL.Path, "/containers/json"):
			all := []container.Summary{
				{ID: "web-1", Labels: labelsFor("demo", "web"), State: states["web-1"]},
				{ID: "db-1", Labels: labelsFor("demo", "db"), State: states["db-1"]},
				{ID: "cache-1", Labels: labelsFor("demo", "cache"), State: states["cache-1"]},
				{ID: "arcane-1", Labels: arcane, State: states["arcane-1"], Mounts: []container.MountPoint{{Type: mount.TypeVolume, Name: "demo_config"}}},
				{ID: "other-1", Labels: labelsFor("other", "app"), State: states["other-1"], Mounts: []container.MountPoint{{Type: mount.TypeVolume, Name: "demo_data"}}},
			}
			// The project label filter is what keeps other projects out.
			if filter := r.URL.Query().Get("filters"); filter != "" {
				all = slices.DeleteFunc(all, func(c container.Summary) bool {
					return !strings.Contains(filter, composeapi.ProjectLabel+"="+c.Labels[composeapi.ProjectLabel])
				})
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.MarshalWrite(w, all)
		case strings.HasSuffix(r.URL.Path, "/stop"):
			id := path.Base(path.Dir(r.URL.Path))
			stopped = append(stopped, id)
			states[id] = container.StateExited
			w.WriteHeader(http.StatusNoContent)
		case strings.HasSuffix(r.URL.Path, "/start"):
			id := path.Base(path.Dir(r.URL.Path))
			started = append(started, id)
			states[id] = container.StateRunning
			w.WriteHeader(http.StatusNoContent)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)
	service := New(Dependencies{
		Docker: newTestDockerService(t, server.URL),
		ComposeProject: func(context.Context, string) (*composetypes.Project, error) {
			return nil, errors.New("compose.yaml: invalid")
		},
	})

	active, inUse, err := service.ContainerUse(t.Context(), "demo")
	require.NoError(t, err)
	assert.Equal(t, []string{"web-1", "db-1"}, containerIDs(active))
	// Another project's container and Arcane itself keep running, so their volumes count as in use.
	assert.Equal(t, map[string]bool{"demo_config": true, "demo_data": true}, inUse)

	summaries, err := service.StopContainers(t.Context(), "demo")
	require.NoError(t, err)
	// The paused cache, Arcane itself, and the other project keep their state.
	assert.Equal(t, []string{"web-1", "db-1"}, stopped)
	assert.Equal(t, container.StatePaused, states["cache-1"])

	// Compose files that no longer load still get the stopped containers started.
	require.NoError(t, service.StartContainers(t.Context(), "proj-demo", summaries))
	assert.Equal(t, []string{"web-1", "db-1"}, started)
}

func containerIDs(summaries []container.Summary) []string {
	ids := make([]string, 0, len(summaries))
	for _, c := range summaries {
		ids = append(ids, c.ID)
	}
	return ids
}

func newTestDockerService(t *testing.T, host string) *docker.DockerClientService {
	t.Helper()
	dockerClient, err := client.New(client.WithHost(host), client.WithAPIVersion("1.41"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = dockerClient.Close() })
	gormDB, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, gormDB.AutoMigrate(&settings.SettingVariable{}))
	db := &database.DB{DB: gormDB}
	settingsService, err := settings.NewSettingsService(settings.SettingsServiceParams{
		Ctx:    t.Context(),
		DB:     db,
		Config: &config.Config{},
		Env:    settings.Env{},
	})
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, settingsService.Stop(context.WithoutCancel(t.Context()))) })
	return docker.NewDockerClientService(t.Context(), db, settingsService, bus.Config{}).WithClient(dockerClient)
}
