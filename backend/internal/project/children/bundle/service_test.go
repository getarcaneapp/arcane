package bundle

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
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
	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/api/types/events"
	"github.com/moby/moby/api/types/mount"
	"github.com/moby/moby/client"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"kit.getarcane.app"
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

func TestExportFailsWhenItsVolumesAreWrittenDuringTheCopy(t *testing.T) {
	archive, err := projects.ProjectBundleVolumeArchive("data")
	require.NoError(t, err)
	manifest := types.ProjectManifest{
		Project:       types.ProjectManifestProject{ID: "p1", Name: "demo", ComposeName: "demo"},
		Volumes:       []types.ProjectVolume{{Key: "data", Name: "demo_data", Kind: types.ProjectVolumeNamed, Archive: archive, Included: true}},
		FormatVersion: types.ProjectManifestFormatVersion,
		Consistent:    true,
	}
	opts := ExportOptions{ProjectID: "p1", Request: types.ExportProjectArchive{Volumes: []string{"data"}, StopProject: true}}
	mounted := func(readWrite string) events.Message {
		return events.Message{
			Type:   events.VolumeEventType,
			Action: events.ActionMount,
			Actor:  events.Actor{ID: "demo_data", Attributes: map[string]string{"container": "c1", "read/write": readWrite}},
		}
	}
	for _, test := range []struct {
		name            string
		wantMessage     string
		containerEvents []events.Message
		volumeEvents    []events.Message
		running         []container.Summary
	}{
		{name: "a project that stays stopped is exported"},
		// The export's own helper mounts each volume read-only.
		{name: "a read-only mount during the copy is fine", volumeEvents: []events.Message{mounted("false")}},
		// A volume backup that had stopped the project starts it again during the copy.
		{
			name:            "a project started during the copy fails the export",
			containerEvents: []events.Message{{Type: events.ContainerEventType, Action: events.ActionStart, Actor: events.Actor{ID: "web-1"}}},
			wantMessage:     "started while its volumes were copied",
		},
		// A volume backup of another project that shares the volume starts that project's writer again during the copy;
		// the container may be gone again before the copy ends, so only its mount event shows it.
		{
			name:         "another project's writer started during the copy fails the export",
			volumeEvents: []events.Message{mounted("true")},
			wantMessage:  "started writing to volume demo_data",
		},
		{
			name: "a writer running when the copy starts fails the export",
			running: []container.Summary{{
				Names:  []string{"/other-app-1"},
				Mounts: []container.MountPoint{{Type: mount.TypeVolume, Name: "demo_data", RW: true}},
			}},
			wantMessage: "container other-app-1 writes to volume demo_data",
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			var windows [][2]string
			dockerService := newTestDockerService(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				// The handler runs on the server's goroutine, so a failed response write is reported with assert.
				switch {
				case strings.HasSuffix(r.URL.Path, "/info"):
					assert.NoError(t, json.MarshalWrite(w, map[string]string{"SystemTime": "2026-11-20T01:00:00.5Z"}))
				case strings.HasSuffix(r.URL.Path, "/containers/json"):
					assert.NoError(t, json.MarshalWrite(w, kit.Ternary(test.running == nil, []container.Summary{}, test.running)))
				case strings.HasSuffix(r.URL.Path, "/events"):
					windows = append(windows, [2]string{r.URL.Query().Get("since"), r.URL.Query().Get("until")})
					served := test.containerEvents
					if strings.Contains(r.URL.Query().Get("filters"), "volume") {
						served = test.volumeEvents
					}
					for _, message := range served {
						assert.NoError(t, json.MarshalWrite(w, message))
					}
				default:
					http.NotFound(w, r)
				}
			}))
			uploads := upload.NewUploadService(nil)
			service := &Service{deps: Dependencies{Uploads: uploads, Docker: dockerService, Environment: &stoppedEnvironment{uploads: uploads}}}
			writeErr := service.writeBundle(t.Context(), io.Discard, opts, manifest, false)
			if test.wantMessage != "" {
				require.ErrorIs(t, writeErr, common.ErrConflict)
				require.ErrorContains(t, writeErr, test.wantMessage)
			} else {
				require.NoError(t, writeErr)
			}
			if test.running != nil {
				require.Empty(t, windows, "a running writer is refused before anything is copied")
				return
			}
			// Both checks read the daemon's own clock from before the copy to after it.
			for _, window := range windows {
				require.Equal(t, [2]string{"1795136400.500000000", "1795136400.500000000"}, window)
			}
			require.NotEmpty(t, windows)
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
		// The handler runs on the server's goroutine, so a failed response write is reported with assert.
		case strings.HasSuffix(r.URL.Path, "/containers/json"):
			_, writeErr := w.Write([]byte("[]"))
			assert.NoError(t, writeErr)
		case strings.HasSuffix(r.URL.Path, "/info"):
			assert.NoError(t, json.MarshalWrite(w, map[string]string{"SystemTime": "2026-11-20T01:00:00Z"}))
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

func TestImportRefusesABundleThatExpandsBeyondTheUploadSpace(t *testing.T) {
	t.Setenv("TMPDIR", t.TempDir())
	var bundle bytes.Buffer
	gzipWriter := gzip.NewWriter(&bundle)
	tarWriter := tar.NewWriter(gzipWriter)
	manifest, err := json.Marshal(types.ProjectManifest{FormatVersion: types.ProjectManifestFormatVersion, Kind: types.ProjectManifestKindExport})
	require.NoError(t, err)
	require.NoError(t, tarWriter.WriteHeader(&tar.Header{Name: projects.ProjectBundleManifest, Typeflag: tar.TypeReg, Mode: 0o644, Size: int64(len(manifest))}))
	_, err = tarWriter.Write(manifest)
	require.NoError(t, err)
	// The entry claims a terabyte; only its header is written, as a small upload that expands without end would be.
	require.NoError(t, tarWriter.WriteHeader(&tar.Header{Name: projects.ProjectBundleProject, Typeflag: tar.TypeReg, Mode: 0o644, Size: 1 << 40}))
	require.NoError(t, gzipWriter.Close())

	uploads := upload.NewUploadService(nil)
	session, err := uploads.IngestSession(t.Context(), types.UploadKindProjectArchive, "demo.arcane.tar.gz", int64(bundle.Len()), &bundle)
	require.NoError(t, err)
	service := New(Dependencies{
		Uploads:     uploads,
		LockProject: func(context.Context, string) (func(), error) { return func() {}, nil },
	})
	_, err = service.Import(t.Context(), ImportOptions{Request: types.ImportProjectArchive{UploadID: session.ID, Name: "copy"}, RestoreVolumes: true})
	require.ErrorIs(t, err, common.ErrValidation)
	require.ErrorContains(t, err, "more than can be stored")
}

func TestExportRefusesToStopAProjectThatRunsArcane(t *testing.T) {
	dockerService := newTestDockerService(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if !strings.HasSuffix(r.URL.Path, "/containers/json") {
			http.NotFound(w, r)
			return
		}
		assert.NoError(t, json.MarshalWrite(w, []container.Summary{{
			Names:  []string{"/demo-arcane-1"},
			State:  container.StateRunning,
			Labels: map[string]string{"com.docker.compose.project": "demo", "com.getarcaneapp.arcane": "true"},
		}}))
	}))
	uploads := upload.NewUploadService(nil)
	environment := &claimEnvironment{uploads: uploads, volumes: map[string]types.Volume{"demo_data": {Name: "demo_data", Driver: "local"}}}
	service := New(Dependencies{
		Environment: environment,
		Docker:      dockerService,
		Uploads:     uploads,
		LockProject: func(context.Context, string) (func(), error) { return func() {}, nil },
		StopProject: func(context.Context, string, types.Actor) error {
			t.Fatal("the project running Arcane was stopped")
			return nil
		},
	})
	_, err := service.Export(t.Context(), ExportOptions{ProjectID: "p1", Request: types.ExportProjectArchive{Volumes: []string{"data"}, StopProject: true}})
	require.ErrorIs(t, err, common.ErrValidation)
	require.ErrorContains(t, err, "runs Arcane itself, so it cannot be stopped for the export")
}
