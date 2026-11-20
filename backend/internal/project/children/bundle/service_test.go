package bundle

import (
	"context"
	"encoding/json/v2"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	composetypes "github.com/compose-spec/compose-go/v2/types"
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
	"getarcane.dev/arcane/v3/pkg/volumes"
	"getarcane.dev/types/v3"
)

type (
	// stoppedEnvironment reports the project stopped and serves its files and volume data.
	stoppedEnvironment struct {
		Environment

		uploads *upload.UploadService
	}

	// claimEnvironment holds Docker volumes by name and records what the export and rollback do.
	claimEnvironment struct {
		Environment

		uploads *upload.UploadService
		volumes map[string]types.Volume
		calls   []string
	}
)

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
	// The test server is gone by cleanup time; a failed close of its client changes nothing.
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

func (e *claimEnvironment) Project(context.Context, string) (types.Project, error) {
	e.calls = append(e.calls, "project")
	return types.Project{ID: "p1", Name: "demo", ComposeName: "demo", Volumes: composetypes.Volumes{"data": {Name: "demo_data"}}}, nil
}

func (e *claimEnvironment) Volume(_ context.Context, name string) (types.Volume, bool, error) {
	volume, exists := e.volumes[name]
	return volume, exists, nil
}

func (e *claimEnvironment) ExportProject(ctx context.Context, _ string) (types.UploadSession, error) {
	session, err := e.uploads.IngestSession(ctx, types.UploadKindProjectArchive, "demo.tar.gz", 5, strings.NewReader("files"))
	return *session, err
}

func (e *claimEnvironment) ExportVolume(ctx context.Context, name string) (types.UploadSession, error) {
	e.calls = append(e.calls, "copy "+name)
	session, err := e.uploads.IngestSession(ctx, types.UploadKindVolumeBackup, "data.tar.gz", 4, strings.NewReader("data"))
	return *session, err
}

func (e *claimEnvironment) DeleteVolume(_ context.Context, name string, _ types.Actor) error {
	e.calls = append(e.calls, "delete "+name)
	delete(e.volumes, name)
	return nil
}

func TestExportHoldsTheProjectLockUntilTheCopyEnds(t *testing.T) {
	dockerService := newTestDockerService(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case strings.HasSuffix(r.URL.Path, "/containers/json"):
			_, _ = w.Write([]byte("[]"))
		case strings.HasSuffix(r.URL.Path, "/info"):
			_ = json.MarshalWrite(w, map[string]string{"SystemTime": "2026-11-20T01:00:00Z"})
		case strings.HasSuffix(r.URL.Path, "/events"):
		default:
			http.NotFound(w, r)
		}
	}))
	uploads := upload.NewUploadService(nil)
	environment := &claimEnvironment{uploads: uploads, volumes: map[string]types.Volume{"demo_data": {Name: "demo_data", Driver: "local"}}}
	service := New(Dependencies{
		Environment: environment,
		Docker:      dockerService,
		Uploads:     uploads,
		ComposeFiles: func(context.Context, string) ([]string, error) {
			return []string{"compose.yaml"}, nil
		},
		LockProject: func(context.Context, string) (func(), error) {
			environment.calls = append(environment.calls, "lock")
			return func() { environment.calls = append(environment.calls, "unlock") }, nil
		},
	})
	session, err := service.Export(t.Context(), ExportOptions{
		ProjectID: "p1",
		Request:   types.ExportProjectArchive{Volumes: []string{"data"}, StopProject: true},
	})
	require.NoError(t, err)
	require.NotNil(t, session)
	// An edit or deploy waits on the lock, so it can't change the volumes between reading the project and the copy.
	require.Equal(t, []string{"lock", "project", "project", "copy demo_data", "unlock"}, environment.calls)
}

func TestImportRollbackRemovesOnlyVolumesItCreated(t *testing.T) {
	const claim = "import-claim"
	environment := &claimEnvironment{volumes: map[string]types.Volume{
		"demo_data":  {Name: "demo_data", Labels: map[string]string{volumes.ClaimLabel: claim}},
		"demo_cache": {Name: "demo_cache", Labels: map[string]string{volumes.ClaimLabel: "someone-else"}},
		"shared":     {Name: "shared"},
	}}
	service := New(Dependencies{
		Environment: environment,
		RemoveProject: func(context.Context, string, types.Actor) error {
			return nil
		},
	})
	err := service.rollback(t.Context(), errors.New("restore failed"), importRollback{
		ProjectID:   "p1",
		ProjectName: "demo",
		Claim:       claim,
		Attempted: []types.ProjectVolume{
			{Key: "data", Name: "demo_data", Kind: types.ProjectVolumeNamed},
			// Created by a deploy or another restore after the import's own check.
			{Key: "cache", Name: "demo_cache", Kind: types.ProjectVolumeNamed},
			// An external volume the restore failed before creating; another project created it meanwhile.
			{Key: "shared", Name: "shared", Kind: types.ProjectVolumeExternal},
			{Key: "logs", Name: "demo_logs", Kind: types.ProjectVolumeNamed},
		},
	})
	require.ErrorContains(t, err, "restore failed")
	require.Equal(t, []string{"delete demo_data"}, environment.calls)
	require.Contains(t, environment.volumes, "demo_cache")
	require.Contains(t, environment.volumes, "shared")
}
