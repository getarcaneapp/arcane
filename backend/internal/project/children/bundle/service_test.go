package bundle

import (
	"context"
	"encoding/json/v2"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/libtnb/sqlite"
	"github.com/moby/moby/api/types/events"
	"github.com/moby/moby/client"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"kit.getarcane.app/docker/streams/bus"

	"getarcane.dev/arcane/v3/internal/common"
	"getarcane.dev/arcane/v3/internal/config"
	"getarcane.dev/arcane/v3/internal/database"
	dockerInternal "getarcane.dev/arcane/v3/internal/docker"
	"getarcane.dev/arcane/v3/internal/settings"
	"getarcane.dev/arcane/v3/internal/upload"
	"getarcane.dev/arcane/v3/pkg/projects"
	"getarcane.dev/types/v3"
)

// stoppedEnvironment reports the project stopped and serves its files and volume data.
type stoppedEnvironment struct {
	Environment

	uploads *upload.UploadService
}

func (e *stoppedEnvironment) Project(context.Context, string) (types.Project, error) {
	return types.Project{ID: "p1", Name: "demo"}, nil
}

func (e *stoppedEnvironment) ExportProject(ctx context.Context, _ string) (types.UploadSession, error) {
	session, err := e.uploads.IngestSession(ctx, types.UploadKindProjectArchive, "demo.tar.gz", 5, strings.NewReader("files"))
	return *session, err
}

func (e *stoppedEnvironment) ExportVolume(ctx context.Context, _ string) (types.UploadSession, error) {
	session, err := e.uploads.IngestSession(ctx, types.UploadKindVolumeBackup, "data.tar.gz", 4, strings.NewReader("data"))
	return *session, err
}

func newTestDockerService(t *testing.T, handler http.Handler) *dockerInternal.DockerClientService {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	dockerClient, err := client.New(client.WithHost(server.URL), client.WithAPIVersion("1.41"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = dockerClient.Close() })
	gormDB, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, gormDB.AutoMigrate(&settings.SettingVariable{}))
	settingsService, err := settings.NewSettingsService(settings.SettingsServiceParams{
		Ctx: t.Context(), DB: &database.DB{DB: gormDB}, Config: &config.Config{}, Env: settings.Env{},
	})
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, settingsService.Stop(context.WithoutCancel(t.Context()))) })
	return dockerInternal.NewDockerClientService(t.Context(), nil, settingsService, bus.Config{}).WithClient(dockerClient)
}

func TestExportFailsWhenTheProjectStartsDuringTheCopy(t *testing.T) {
	archive, err := projects.ProjectBundleVolumeArchive("data")
	require.NoError(t, err)
	manifest := types.ProjectManifest{
		Project:       types.ProjectManifestProject{ID: "p1", Name: "demo", ComposeName: "demo"},
		Volumes:       []types.ProjectVolume{{Key: "data", Name: "demo_data", Kind: types.ProjectVolumeNamed, Archive: archive, Included: true}},
		FormatVersion: types.ProjectManifestFormatVersion,
	}
	opts := ExportOptions{ProjectID: "p1", Request: types.ExportProjectArchive{Volumes: []string{"data"}, StopProject: true}}
	for _, test := range []struct {
		name     string
		events   []events.Message
		conflict bool
	}{
		{name: "a project that stays stopped is exported"},
		// A volume backup that had stopped the project starts it again during the copy.
		{name: "a project started during the copy fails the export", events: []events.Message{
			{Type: events.ContainerEventType, Action: events.ActionStart, Actor: events.Actor{ID: "web-1"}},
		}, conflict: true},
		// The container that started is gone again before the copy ends, so only its start event shows it.
		{name: "a container started and removed during the copy fails the export", events: []events.Message{
			{Type: events.ContainerEventType, Action: events.ActionStart, Actor: events.Actor{ID: "job-1"}},
		}, conflict: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			var since, until string
			dockerService := newTestDockerService(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				switch {
				case strings.HasSuffix(r.URL.Path, "/info"):
					_ = json.MarshalWrite(w, map[string]string{"SystemTime": "2026-11-20T01:00:00.5Z"})
				case strings.HasSuffix(r.URL.Path, "/events"):
					since, until = r.URL.Query().Get("since"), r.URL.Query().Get("until")
					for _, message := range test.events {
						_ = json.MarshalWrite(w, message)
					}
				default:
					http.NotFound(w, r)
				}
			}))
			uploads := upload.NewUploadService(nil)
			service := &Service{deps: Dependencies{Uploads: uploads, Docker: dockerService, Environment: &stoppedEnvironment{uploads: uploads}}}
			writeErr := service.writeBundle(t.Context(), io.Discard, opts, manifest, false)
			// The window is the daemon's own clock from before the copy to after it.
			require.Equal(t, "1795136400.500000000", since)
			require.Equal(t, since, until)
			if test.conflict {
				require.ErrorIs(t, writeErr, common.ErrConflict)
				require.ErrorContains(t, writeErr, "started while its volumes were copied")
				return
			}
			require.NoError(t, writeErr)
		})
	}
}
