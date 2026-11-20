package bundle

import (
	"context"
	"encoding/json/v2"
	"errors"
	"io"
	"maps"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	composetypes "github.com/compose-spec/compose-go/v2/types"
	"github.com/docker/compose/v5/pkg/api"
	"github.com/libtnb/sqlite"
	"github.com/moby/moby/api/types/events"
	"github.com/moby/moby/api/types/volume"
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

	// restoreEnvironment holds the Docker volumes a restore as a new project checks, creates and removes, and the project
	// it creates.
	restoreEnvironment struct {
		Environment

		volumes  map[string]map[string]string
		deleted  []string
		deployed []string
		mu       sync.Mutex
		stack    bool
		created  bool
	}

	// fakeBackups serves a backup's manifest and records what it restores; restoring volume data fails with fail.
	fakeBackups struct {
		fail     error
		restores []types.ProjectBackupRestore
		manifest types.ProjectManifest
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

func (e *restoreEnvironment) ComposeProjectInUse(context.Context, string) (bool, error) {
	return e.stack, nil
}

func (e *restoreEnvironment) Volume(_ context.Context, name string) (types.Volume, bool, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	labels, exists := e.volumes[name]
	return types.Volume{Name: name, Labels: labels}, exists, nil
}

// Project is the restored project, whose files rename the backed up project's data volume.
func (e *restoreEnvironment) Project(context.Context, string) (types.Project, error) {
	return types.Project{
		ID:          "new",
		Name:        "copy",
		ComposeName: "copy",
		Volumes: composetypes.Volumes{
			"data":   {Name: "copy_data"},
			"logs":   {Name: "copy_logs"},
			"shared": {Name: "shared-data", External: true},
		},
	}, nil
}

func (e *restoreEnvironment) DestroyProject(context.Context, string, types.Actor) error {
	e.created = false
	return nil
}

func (e *restoreEnvironment) DeleteVolume(_ context.Context, name string, _ types.Actor) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.deleted = append(e.deleted, name)
	delete(e.volumes, name)
	return nil
}

func (e *restoreEnvironment) DeployProject(_ context.Context, projectID string, _ types.Actor) error {
	e.deployed = append(e.deployed, projectID)
	return nil
}

func (f *fakeBackups) Manifest(context.Context, string, string) (types.ProjectManifest, error) {
	return f.manifest, nil
}

func (f *fakeBackups) RestoreTo(_ context.Context, opts types.ProjectBackupRestore) error {
	f.restores = append(f.restores, opts)
	if len(opts.Volumes) > 0 {
		return f.fail
	}
	return nil
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

func TestRestoreAsNew(t *testing.T) {
	manifest := types.ProjectManifest{
		Kind:         types.ProjectManifestKindBackup,
		Project:      types.ProjectManifestProject{ID: "p1", Name: "web", ComposeName: "web"},
		ComposeFiles: []string{"compose.yaml"},
		Volumes: []types.ProjectVolume{
			{Key: "data", Name: "web_data", Kind: types.ProjectVolumeNamed, Included: true},
			{Key: "logs", Name: "web_logs", Kind: types.ProjectVolumeNamed},
			{Key: "shared", Name: "shared-data", Kind: types.ProjectVolumeExternal, DockerLabels: map[string]string{"owner": "ops"}, Included: true},
		},
		FormatVersion: types.ProjectManifestFormatVersion,
	}
	for _, test := range []struct {
		name           string
		wantErr        error
		wantMessage    string
		failCreate     string
		existing       []string
		wantCreated    []string
		wantDeleted    []string
		request        types.RestoreProjectBackupAsNewRequest
		failRestore    bool
		stack          bool
		noVolumeRights bool
	}{
		{name: "restores and starts", request: types.RestoreProjectBackupAsNewRequest{Name: "copy", Start: true}, wantCreated: []string{"shared-data", "copy_data"}},
		{
			name: "refuses a new project without the backup's files", request: types.RestoreProjectBackupAsNewRequest{Name: "copy", Files: new(false)},
			wantErr: common.ErrValidation, wantMessage: "needs the project files",
		},
		{
			name: "needs the right to create volumes for the backup's volumes", request: types.RestoreProjectBackupAsNewRequest{Name: "copy"}, noVolumeRights: true,
			wantErr: common.ErrForbidden, wantMessage: "requires volumes:create",
		},
		// These refusals come before the project or any volume is created.
		{
			name: "refuses a running Compose stack of the same name", request: types.RestoreProjectBackupAsNewRequest{Name: "copy"}, stack: true,
			wantErr: common.ErrConflict, wantMessage: "docker already runs a Compose stack named copy",
		},
		{
			name: "refuses an existing volume whose name can't change", request: types.RestoreProjectBackupAsNewRequest{Name: "copy"}, existing: []string{"shared-data"},
			wantErr: common.ErrConflict, wantMessage: "these volumes already exist: shared-data",
		},
		// A volume named after the new project is only known once it exists, so the project is removed again.
		{
			name: "refuses an existing volume of the new project", request: types.RestoreProjectBackupAsNewRequest{Name: "copy"}, existing: []string{"copy_data"},
			wantErr: common.ErrConflict, wantMessage: "these volumes already exist: copy_data",
		},
		{
			name: "removes what it created when the restore fails", request: types.RestoreProjectBackupAsNewRequest{Name: "copy"}, failRestore: true,
			wantMessage: "rustic failed", wantCreated: []string{"shared-data", "copy_data"}, wantDeleted: []string{"shared-data", "copy_data"},
		},
		// Someone else made copy_data after the check, so it is refused and only shared-data is removed.
		{
			name: "removes only what it created", request: types.RestoreProjectBackupAsNewRequest{Name: "copy"}, failCreate: "copy_data",
			wantErr: common.ErrConflict, wantMessage: "volume copy_data already exists", wantCreated: []string{"shared-data"}, wantDeleted: []string{"shared-data"},
		},
		// An external volume made by someone else after the check is refused, not taken over and later removed.
		{
			name: "refuses an external volume made after the check", request: types.RestoreProjectBackupAsNewRequest{Name: "copy"}, failCreate: "shared-data",
			wantErr: common.ErrConflict, wantMessage: "volume shared-data already exists",
		},
		{name: "restores only the files", request: types.RestoreProjectBackupAsNewRequest{Name: "copy", Volumes: []string{}}, noVolumeRights: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			environment := &restoreEnvironment{volumes: map[string]map[string]string{}, stack: test.stack}
			for _, name := range test.existing {
				environment.volumes[name] = nil
			}
			var created []client.VolumeCreateOptions
			dockerService := newTestDockerService(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if !strings.HasSuffix(r.URL.Path, "/volumes/create") {
					http.NotFound(w, r)
					return
				}
				var options client.VolumeCreateOptions
				assert.NoError(t, json.UnmarshalRead(r.Body, &options))
				environment.mu.Lock()
				defer environment.mu.Unlock()
				labels := maps.Clone(options.Labels)
				if options.Name == test.failCreate {
					// Docker hands back the volume someone else made, without this call's claim.
					labels = nil
				} else {
					created = append(created, options)
				}
				environment.volumes[options.Name] = labels
				w.Header().Set("Content-Type", "application/json")
				assert.NoError(t, json.MarshalWrite(w, volume.Volume{Name: options.Name, Labels: labels}))
			}))
			backups := &fakeBackups{manifest: manifest, fail: kit.Ternary(test.failRestore, errors.New("rustic failed"), nil)}
			var filled string
			service := New(Dependencies{
				Environment: environment,
				Backups:     backups,
				Docker:      dockerService,
				CreateProject: func(ctx context.Context, project NewProject) (string, error) {
					assert.Equal(t, "copy", project.Name)
					assert.Equal(t, []string{"compose.yaml"}, project.ComposeFiles)
					filled = t.TempDir()
					environment.created = true
					return "new", project.Fill(ctx, filled)
				},
			})

			projectID, err := service.RestoreAsNew(t.Context(), RestoreAsNewOptions{ProjectID: "p1", BackupID: "b1", Request: test.request, RestoreVolumes: !test.noVolumeRights})
			names := make([]string, 0, len(created))
			for _, options := range created {
				names = append(names, options.Name)
			}
			assert.Equal(t, test.wantCreated, kit.Ternary(len(names) == 0, nil, names))
			assert.Equal(t, test.wantDeleted, environment.deleted)
			if test.wantMessage != "" {
				require.ErrorContains(t, err, test.wantMessage)
				if test.wantErr != nil {
					require.ErrorIs(t, err, test.wantErr)
				}
				// A refused or failed restore leaves no project behind, and restores no volume data before its volumes exist.
				assert.False(t, environment.created)
				assert.Empty(t, environment.deployed)
				if !test.failRestore {
					for _, restore := range backups.restores {
						assert.Empty(t, restore.Volumes, "no volume data is restored when the new project is refused")
					}
				}
				return
			}
			require.NoError(t, err)
			require.Equal(t, "new", projectID)
			assert.True(t, environment.created)
			assert.Equal(t, kit.Ternary(test.request.Start, []string{"new"}, nil), environment.deployed)
			// The files restore into the new project directory, then the volumes into the volumes it created.
			require.Equal(t, types.ProjectBackupRestore{ProjectID: "p1", BackupID: "b1", ProjectPath: filled}, backups.restores[0])
			require.Len(t, backups.restores, 2)
			if len(test.wantCreated) == 0 {
				assert.Empty(t, backups.restores[1].Volumes)
				return
			}
			assert.Equal(t, types.ProjectBackupRestore{
				ProjectID: "p1",
				BackupID:  "b1",
				Volumes: []types.ProjectBackupRestoreVolume{
					{Key: "shared", Snapshot: "shared-data", Name: "shared-data"},
					{Key: "data", Snapshot: "web_data", Name: "copy_data"},
				},
				LockVolumes: true,
			}, backups.restores[1])
			// An external volume gets its recorded Docker labels and no Compose labels; each carries this restore's claim.
			assert.Equal(t, "ops", created[0].Labels["owner"])
			assert.NotContains(t, created[0].Labels, api.ProjectLabel)
			assert.NotEmpty(t, created[0].Labels[volumes.ClaimLabel])
			assert.Equal(t, "copy", created[1].Labels[api.ProjectLabel])
			assert.Equal(t, "data", created[1].Labels[api.VolumeLabel])
		})
	}
}
