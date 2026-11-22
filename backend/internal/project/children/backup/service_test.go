package backup

import (
	"archive/tar"
	"context"
	"encoding/json/v2"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	composeTypes "github.com/compose-spec/compose-go/v2/types"
	"github.com/docker/compose/v5/pkg/api"
	"github.com/libtnb/sqlite"
	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/api/types/mount"
	"github.com/moby/moby/api/types/volume"
	"github.com/moby/moby/client"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"kit.getarcane.app/concurrency"
	"kit.getarcane.app/docker/streams/bus"
	"kit.getarcane.app/sys/crypto"

	"getarcane.dev/arcane/v3/internal/activity"
	"getarcane.dev/arcane/v3/internal/backup"
	"getarcane.dev/arcane/v3/internal/common"
	"getarcane.dev/arcane/v3/internal/config"
	"getarcane.dev/arcane/v3/internal/database"
	"getarcane.dev/arcane/v3/internal/docker"
	"getarcane.dev/arcane/v3/internal/s3"
	"getarcane.dev/arcane/v3/internal/scheduler/flow/flowtest"
	"getarcane.dev/arcane/v3/internal/scheduler/runs"
	"getarcane.dev/arcane/v3/internal/settings"
	"getarcane.dev/arcane/v3/pkg/authz"
	"getarcane.dev/arcane/v3/pkg/projects"
	"getarcane.dev/arcane/v3/pkg/volumes"
	"getarcane.dev/types/v3"
)

type (
	// fakeRestore records the steps an in-place restore applies and fails the ones it names.
	fakeRestore struct {
		fail map[string]error
		// mayStop records, for each stop, whether the restore may stop the project.
		mayStop []bool
		calls   []string
	}

	// fakeScheduler records the jobs the policies register.
	fakeScheduler struct {
		jobs map[string]types.SchedulerJob
		mu   sync.Mutex
	}

	policyFixture struct {
		db          *database.DB
		engine      *backup.Engine
		service     *Service
		jobs        *fakeScheduler
		projects    *[]types.ProjectReference
		destination string
	}
)

func newTestDockerService(t *testing.T, handler http.Handler) *docker.DockerClientService {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	dockerClient, err := client.New(client.WithHost(server.URL), client.WithAPIVersion("1.41"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = dockerClient.Close() })
	gormDB, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, gormDB.AutoMigrate(&settings.SettingVariable{}))
	db := &database.DB{DB: gormDB}
	settingsService, err := settings.NewSettingsService(settings.SettingsServiceParams{Ctx: t.Context(), DB: db, Config: &config.Config{}, Env: settings.Env{}})
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, settingsService.Stop(context.WithoutCancel(t.Context()))) })
	return docker.NewDockerClientService(t.Context(), db, settingsService, bus.Config{}).WithClient(dockerClient)
}

func newTestDB(t *testing.T) *database.DB {
	t.Helper()
	db, err := database.Initialize(t.Context(), "file:"+t.Name()+"?mode=memory&cache=shared", database.MigrationOptions{})
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, db.Close()) })
	return db
}

// existingVolumes inspects the existing volumes and reports the rest missing.
func existingVolumes(existing ...string) func(context.Context, string) (types.Volume, bool, error) {
	return func(_ context.Context, name string) (types.Volume, bool, error) {
		return types.Volume{Name: name}, slices.Contains(existing, name), nil
	}
}

func testProject() types.Project {
	return types.Project{
		ID:          "p1",
		Name:        "Web",
		ComposeName: "web",
		Path:        "/app/data/projects/web",
		Volumes: composeTypes.Volumes{
			"data":   {Name: "web_data"},
			"cache":  {Name: "web_cache"},
			"shared": {Name: "shared", External: true},
			"nfs":    {Name: "web_nfs", DriverOpts: map[string]string{"type": "nfs"}},
		},
	}
}

func TestResolvePlanAndSnapshotInput(t *testing.T) {
	service := NewService(Dependencies{BackupStore: noBackupStore, Volume: existingVolumes("web_data", "web_nfs", "shared")})

	p, err := service.resolvePlan(t.Context(), testProject(), types.CreateProjectBackupRequest{})
	require.NoError(t, err)
	require.True(t, p.LocalEnabled)
	require.False(t, p.S3Enabled)
	require.True(t, p.StopContainers)
	included := map[string]bool{}
	missing := map[string]bool{}
	for _, volume := range p.Volumes {
		included[volume.Key], missing[volume.Key] = volume.Included, volume.Missing
	}
	// Existing named and local external volumes are captured; driver_opts volumes are listed but left out.
	require.Equal(t, map[string]bool{"cache": false, "data": true, "nfs": false, "shared": true}, included)
	require.True(t, missing["cache"])
	require.Equal(t, []string{"shared", "web_data"}, p.volumeNames())
	// The capture locks every volume of the project, captured or not.
	require.Equal(t, []string{"shared", "web_cache", "web_data", "web_nfs"}, p.mountedVolumeNames())

	projectMounts := []mount.Mount{{Type: mount.TypeBind, Source: "/host/projects/web", Target: "/snapshot/project", ReadOnly: true}}
	input := snapshotInput(projectMounts, "arcane-project-backup-meta", "b1", p)
	require.Equal(t, []string{"/snapshot"}, input.Sources)
	require.Equal(t, "/", input.AsPath)
	require.Equal(t, []string{backup.ProjectSnapshotTag, "arcane-project:p1", "arcane-run:b1"}, input.Tags)
	require.Equal(t, []mount.Mount{
		{Type: mount.TypeBind, Source: "/host/projects/web", Target: "/snapshot/project", ReadOnly: true},
		{Type: mount.TypeVolume, Source: "shared", Target: "/snapshot/volumes/shared", ReadOnly: true},
		{Type: mount.TypeVolume, Source: "web_data", Target: "/snapshot/volumes/web_data", ReadOnly: true},
		{Type: mount.TypeVolume, Source: "arcane-project-backup-meta", Target: "/snapshot/meta", ReadOnly: true},
	}, input.Mounts)

	none, err := service.resolvePlan(t.Context(), testProject(), types.CreateProjectBackupRequest{Volumes: []string{}, StopContainers: new(false)})
	require.NoError(t, err)
	require.Empty(t, none.volumeNames())
	require.False(t, none.StopContainers)

	for key, message := range map[string]string{
		"nfs":     "keeps its data outside Docker",
		"cache":   "has not been created yet",
		"missing": "has no volume missing",
	} {
		_, err = service.resolvePlan(t.Context(), testProject(), types.CreateProjectBackupRequest{Volumes: []string{key}})
		require.ErrorContains(t, err, message)
		require.ErrorIs(t, err, common.ErrValidation)
	}
}

// A backup of the volume holding Arcane's backups would read the repository it writes into, copying it into itself.
func TestResolvePlanLeavesOutTheBackupStore(t *testing.T) {
	service := NewService(Dependencies{
		BackupStore: func(context.Context) string { return "shared" },
		Volume:      existingVolumes("web_data", "web_nfs", "shared"),
	})

	p, err := service.resolvePlan(t.Context(), testProject(), types.CreateProjectBackupRequest{})
	require.NoError(t, err)
	require.Equal(t, []string{"web_data"}, p.volumeNames())

	_, err = service.resolvePlan(t.Context(), testProject(), types.CreateProjectBackupRequest{Volumes: []string{"data", "shared"}})
	require.ErrorContains(t, err, "volume shared holds Arcane's backups")
	require.ErrorIs(t, err, common.ErrValidation)
}

func TestCheckPlanVolumesRejectsChangedVolumes(t *testing.T) {
	service := NewService(Dependencies{BackupStore: noBackupStore, Volume: existingVolumes("web_data", "web_nfs", "shared")})
	p, err := service.resolvePlan(t.Context(), testProject(), types.CreateProjectBackupRequest{})
	require.NoError(t, err)
	require.NoError(t, service.checkPlanVolumes(t.Context(), testProject(), p))

	// A compose edit and deploy while the backup waited moved the data volume to a new name.
	changed := testProject()
	changed.Volumes["data"] = composeTypes.VolumeConfig{Name: "web_data_v2"}
	err = service.checkPlanVolumes(t.Context(), changed, p)
	require.ErrorIs(t, err, common.ErrConflict)
	require.ErrorContains(t, err, "changed after the backup was queued")

	// Or made it external.
	external := testProject()
	external.Volumes["data"] = composeTypes.VolumeConfig{Name: "web_data", External: true}
	err = service.checkPlanVolumes(t.Context(), external, p)
	require.ErrorIs(t, err, common.ErrConflict)
}

func TestCheckPlanVolumesKeepsVolumesWithDockerDriverOptions(t *testing.T) {
	// Compose names a plain volume, but the existing Docker volume binds a host directory.
	service := NewService(Dependencies{BackupStore: noBackupStore, Volume: func(_ context.Context, name string) (types.Volume, bool, error) {
		if name == "web_data" {
			return types.Volume{Name: name, Driver: "local", Options: map[string]string{"type": "none", "o": "bind", "device": "/srv/web"}}, true, nil
		}
		return types.Volume{Name: name}, slices.Contains([]string{"web_nfs", "shared"}, name), nil
	}})
	p, err := service.resolvePlan(t.Context(), testProject(), types.CreateProjectBackupRequest{})
	require.NoError(t, err)
	require.NotContains(t, p.volumeNames(), "web_data")

	require.NoError(t, service.checkPlanVolumes(t.Context(), testProject(), p))
}

func TestBackupManifestKind(t *testing.T) {
	project := testProject()
	p := plan{Volumes: []types.ProjectVolume{{Key: "data", Name: "web_data", Kind: types.ProjectVolumeNamed, Included: true}}}

	manifest := backupManifest(project, p, false)
	require.Equal(t, types.ProjectManifestKindBackup, manifest.Kind)
	require.Equal(t, types.ProjectManifestFormatVersion, manifest.FormatVersion)
	require.Equal(t, types.ProjectManifestProject{ID: "p1", Name: "Web", ComposeName: "web"}, manifest.Project)
	require.Equal(t, p.Volumes, manifest.Volumes)
	require.False(t, manifest.Consistent, "nothing holds the project during a live backup, so it is never consistent")
	require.False(t, manifest.ExternalEnv)
	require.Equal(t, []string{project.Path}, manifest.ProjectDirs, "a path that doesn't resolve is recorded as configured")
	require.True(t, backupManifest(project, p, true).ExternalEnv)

	p.StopContainers = true
	require.True(t, backupManifest(project, p, false).Consistent)

	// Other projects are not stopped, so a captured volume one of them mounts makes the copy inconsistent.
	p.Volumes[0].InUse = true
	require.False(t, backupManifest(project, p, false).Consistent)
}

func TestStartBackupFailsFastWhenProjectOrVolumeIsBusy(t *testing.T) {
	db := newTestDB(t)
	harness := flowtest.New(t, activity.NewActivityService(db, nil))
	gate := runs.NewAdmission(harness.Runtime.Service(), t.Name())
	require.NoError(t, gate.Register(harness.Runtime))
	engine := backup.NewEngine(gate, nil, func(context.Context, types.BackupRequester) error { return nil })
	t.Cleanup(func() { require.NoError(t, engine.Stop(context.WithoutCancel(t.Context()))) })
	service := NewService(Dependencies{
		BackupStore: noBackupStore,
		DB:          db,
		Engine:      engine,
		Volume:      existingVolumes("web_data"),
		Project: func(context.Context, string, types.ProjectDetailsOptions) (types.Project, error) {
			return testProject(), nil
		},
	})
	require.NoError(t, service.RegisterWorkflows(harness.Engine))
	harness.Start(t)
	flowtest.AssertDefinitions(t, harness)

	for _, held := range []types.SchedulerAdmissionKey{{Scope: admissionScope, ID: "p1"}, {Scope: backup.VolumeAdmissionScope, ID: "web_data"}} {
		lease, admitted, err := engine.TryAcquireRun(t.Context(), held.Scope, held.ID)
		require.NoError(t, err)
		require.True(t, admitted)

		_, err = service.StartBackup(t.Context(), StartOptions{EnvironmentID: "0", ProjectID: "p1"})
		require.ErrorIs(t, err, common.ErrConflict)
		lease.Release(t.Context())
	}
	var rows int64
	require.NoError(t, db.Model(&ProjectBackup{}).Count(&rows).Error)
	require.Zero(t, rows)
	// A refused start releases every admission it took.
	for _, key := range admissions("p1", []string{"web_data"}) {
		lease, admitted, err := engine.TryAcquireRun(t.Context(), key.Scope, key.ID)
		require.NoError(t, err)
		require.True(t, admitted, key)
		lease.Release(t.Context())
	}
}

func TestRestoreEnvPutsBackAnExternalEnv(t *testing.T) {
	for _, test := range []struct {
		name     string
		external string
		project  string
		// worker is what the other project's .env is: a regular file or a link to the file web's .env leads to.
		worker string
		own    bool
		shared bool
		// inside links web's .env by absolute path to a file in web's own directory, and a config file through ".." back
		// into it, both of which the backup holds itself, and restores into a new directory.
		inside bool
	}{
		// In place, the restored link leads to the file the project uses, which keeps its mode.
		{name: "in place", worker: "file", external: "TOKEN=restored"},
		// A new project must not share or change the original's file.
		{name: "as a new project", worker: "file", own: true, external: "TOKEN=current\n", project: "TOKEN=restored"},
		// The new project's links must lead to its own restored files, not the original's.
		{name: "as a new project, linked into the original's directory", worker: "file", own: true, inside: true, external: "TOKEN=current\n", project: "TOKEN=restored"},
		// Another project's .env leads to the same file, so the restore must not change that project's values.
		{name: "in place, shared with another project", worker: "link", shared: true, external: "TOKEN=current\n"},
	} {
		t.Run(test.name, func(t *testing.T) {
			// The projects root is reached through a link, so paths that aren't fully resolved name files differently.
			projectsRoot := filepath.Join(t.TempDir(), "projects")
			realRoot := t.TempDir()
			require.NoError(t, os.Symlink(realRoot, projectsRoot))
			projectPath := filepath.Join(projectsRoot, "web")
			workerPath := filepath.Join(projectsRoot, "worker")
			require.NoError(t, os.Mkdir(projectPath, 0o755))
			require.NoError(t, os.Mkdir(workerPath, 0o755))
			workerEnv := filepath.Join(workerPath, projects.EffectiveEnvFileName)
			external := filepath.Join(t.TempDir(), "web.env")
			link := external
			restored := projectPath
			if test.inside {
				external = filepath.Join(projectPath, "values.env")
				// Spelled through the resolved root, which the project's recorded path doesn't use.
				link = filepath.Join(realRoot, "web", "values.env")
				restored = filepath.Join(projectsRoot, "shop")
				// As the snapshot restores them, the copy's links keep the original's targets; config/latest.json
				// stays inside, so it is never rewritten.
				for _, dir := range []string{projectPath, restored} {
					require.NoError(t, os.MkdirAll(filepath.Join(dir, "config"), 0o755))
					require.NoError(t, os.WriteFile(filepath.Join(dir, "config", "v2.json"), []byte(dir), 0o600))
					require.NoError(t, os.Symlink("../../web/config/v2.json", filepath.Join(dir, "config", "current.json")))
					require.NoError(t, os.Symlink("v2.json", filepath.Join(dir, "config", "latest.json")))
				}
				require.NoError(t, os.WriteFile(filepath.Join(restored, "values.env"), []byte("TOKEN=restored"), 0o600))
				require.NoError(t, os.Symlink(link, filepath.Join(restored, projects.EffectiveEnvFileName)))
			}
			require.NoError(t, os.WriteFile(external, []byte("TOKEN=current\n"), 0o600))
			require.NoError(t, os.Symlink(link, filepath.Join(projectPath, projects.EffectiveEnvFileName)))
			envPath := filepath.Join(restored, projects.EffectiveEnvFileName)
			switch test.worker {
			case "file":
				require.NoError(t, os.WriteFile(workerEnv, []byte("TOKEN=worker\n"), 0o600))
			case "link":
				require.NoError(t, os.Symlink(external, workerEnv))
			}
			s := &Service{deps: Dependencies{Projects: func(context.Context) ([]types.ProjectRecord, error) {
				return []types.ProjectRecord{{ID: "p1", Name: "web", Path: projectPath}, {ID: "p2", Name: "worker", Path: workerPath}}, nil
			}}}
			var reported []string
			report := func(_ context.Context, message string) { reported = append(reported, message) }

			// As the backup does: it records the project directory as configured and resolved, which the links name.
			manifest := backupManifest(types.Project{Path: projectPath}, plan{}, false)
			resolvedPath, resolveErr := filepath.EvalSymlinks(projectPath)
			require.NoError(t, resolveErr)
			assert.Equal(t, []string{projectPath, resolvedPath}, manifest.ProjectDirs)
			// As RestoreTo does: the backup stored contents only for a .env leading outside the project directory.
			opts := types.ProjectBackupRestore{ProjectID: "p1", ProjectPath: restored, OwnEnv: test.own, Report: report}
			if test.own {
				require.NoError(t, projects.OwnLinks(t.Context(), restored, manifest.ProjectDirs, report))
			}
			if !test.inside {
				require.NoError(t, s.restoreEnv(t.Context(), projectsRoot, opts, "TOKEN=restored"))
			}
			if test.shared {
				resolved, err := filepath.EvalSymlinks(external)
				require.NoError(t, err)
				require.Equal(t, []string{"The .env file " + resolved + " was left as it is, since project worker uses it too; only the link to it was restored"}, reported)
			} else {
				require.Empty(t, reported)
			}
			content, err := os.ReadFile(external)
			require.NoError(t, err)
			assert.Equal(t, test.external, string(content))
			info, err := os.Stat(external)
			require.NoError(t, err)
			assert.Equal(t, os.FileMode(0o600), info.Mode().Perm())
			if test.inside {
				target, readlinkErr := os.Readlink(envPath)
				require.NoError(t, readlinkErr)
				assert.Equal(t, "values.env", target)
				for _, link := range []string{"current.json", "latest.json"} {
					content, err = os.ReadFile(filepath.Join(restored, "config", link))
					require.NoError(t, err)
					assert.Equal(t, restored, string(content), link)
				}
			} else {
				info, err = os.Lstat(envPath)
				require.NoError(t, err)
				assert.Equal(t, test.own, info.Mode().IsRegular())
			}
			if test.own {
				content, err = os.ReadFile(envPath)
				require.NoError(t, err)
				assert.Equal(t, test.project, string(content))
			}
		})
	}
}

func TestRestoreInputRestoresOnlyTheTargets(t *testing.T) {
	projectMounts := []mount.Mount{{Type: mount.TypeBind, Source: "/host/projects/web", Target: "/dest/project"}}
	targets := []types.ProjectBackupRestoreVolume{
		{Key: "data", Snapshot: "web_data", Name: "shop_data"},
		{Key: "alias", Snapshot: "web_data", Name: "shop_data"},
	}
	target, options, err := restoreInput(projectMounts, targets, []string{"shared", "web_cache", "web_data"})
	require.NoError(t, err)
	// /dest holds only the restored project directory and targets, each volume under its name in the snapshot.
	require.Equal(t, projectMounts[0], target)
	require.Equal(t, []mount.Mount{{Type: mount.TypeVolume, Source: "shop_data", Target: "/dest/volumes/web_data"}}, options.ExtraMounts)
	require.Equal(t, "/dest", options.DestinationPath)
	require.Empty(t, options.SourcePath, "the snapshot root is restored, so each target's root keeps its owner and mode")
	require.True(t, options.DeleteExtra)
	require.Equal(t, []string{"!/meta", "!/meta/**", "!/volumes/shared", "!/volumes/shared/**", "!/volumes/web_cache", "!/volumes/web_cache/**"}, options.Globs)

	target, options, err = restoreInput(nil, targets[:1], []string{"web_data"})
	require.NoError(t, err)
	require.Equal(t, mount.Mount{Type: mount.TypeVolume, Source: "shop_data", Target: "/dest/volumes/web_data"}, target)
	require.Empty(t, options.ExtraMounts)
	require.Equal(t, []string{"!/meta", "!/meta/**", "!/project", "!/project/**"}, options.Globs)

	for name, invalid := range map[string][]types.ProjectBackupRestoreVolume{
		"not in the snapshot": {{Key: "cache", Snapshot: "web_cache", Name: "shop_cache"}},
		"into two targets":    {{Key: "data", Snapshot: "web_data", Name: "a"}, {Key: "alias", Snapshot: "web_data", Name: "b"}},
		"two into one volume": {{Key: "data", Snapshot: "web_data", Name: "a"}, {Key: "other", Snapshot: "shared", Name: "a"}},
	} {
		_, _, err = restoreInput(nil, invalid, []string{"shared", "web_data"})
		require.ErrorIs(t, err, common.ErrValidation, name)
	}
}

func TestRestoreTargetsMapKeysToTheProjectsCurrentVolumes(t *testing.T) {
	captured := []types.ProjectBackupVolume{
		{Key: "data", Name: "web_data", Included: true},
		{Key: "shared", Name: "shared", Included: true},
		{Key: "cache", Name: "web_cache"},
		{Key: "nfs", Name: "web_nfs"},
	}
	targets, err := restoreTargets(testProject(), captured, []string{"data", "shared"})
	require.NoError(t, err)
	require.Equal(t, []types.ProjectBackupRestoreVolume{{Key: "data", Snapshot: "web_data", Name: "web_data"}, {Key: "shared", Snapshot: "shared", Name: "shared"}}, targets)

	// After a rename the snapshot's web_data restores into the volume the project now uses for the key.
	renamed := types.Project{
		ID:          "p1",
		Name:        "Web",
		ComposeName: "shop",
		Path:        "/app/data/projects/web",
		Volumes: composeTypes.Volumes{
			"data":   {Name: "shop_data"},
			"cache":  {Name: "web_cache"},
			"shared": {Name: "shared", External: true},
			"nfs":    {Name: "web_nfs", DriverOpts: map[string]string{"type": "nfs"}},
		},
	}
	targets, err = restoreTargets(renamed, captured, []string{"data"})
	require.NoError(t, err)
	require.Equal(t, []types.ProjectBackupRestoreVolume{{Key: "data", Snapshot: "web_data", Name: "shop_data"}}, targets)

	_, err = restoreTargets(testProject(), captured, []string{"cache"})
	require.ErrorContains(t, err, "does not include the data of volume cache")
}

func TestStartRestoreValidatesTheSelection(t *testing.T) {
	db := newTestDB(t)
	require.NoError(t, db.Create(&ProjectBackup{
		BaseModel:       database.BaseModel{ID: "b1", CreatedAt: time.Now()},
		ProjectID:       "p1",
		Status:          types.VolumeBackupStatusSucceeded,
		LocalSnapshotID: "snap",
		Volumes:         []types.ProjectBackupVolume{{Key: "data", Name: "web_data", Included: true}, {Key: "cache", Name: "web_cache"}},
	}).Error)
	require.NoError(t, db.Create(&ProjectBackup{BaseModel: database.BaseModel{ID: "b2", CreatedAt: time.Now()}, ProjectID: "p1", Status: types.VolumeBackupStatusFailed}).Error)
	service := NewService(Dependencies{
		DB: db,
		Project: func(context.Context, string, types.ProjectDetailsOptions) (types.Project, error) {
			return testProject(), nil
		},
	})
	for name, test := range map[string]struct {
		backupID string
		message  string
		request  types.RestoreProjectBackupRequest
	}{
		"nothing":      {backupID: "b1", request: types.RestoreProjectBackupRequest{Files: new(false), Volumes: []string{}}, message: "select the project files or at least one volume"},
		"not included": {backupID: "b1", request: types.RestoreProjectBackupRequest{Volumes: []string{"cache"}}, message: "does not include the data of volume cache"},
		"failed":       {backupID: "b2", message: "only a backup that succeeded"},
	} {
		_, err := service.StartRestore(t.Context(), StartRestoreOptions{EnvironmentID: "0", ProjectID: "p1", BackupID: test.backupID, Request: test.request})
		require.ErrorIs(t, err, common.ErrValidation, name)
		require.ErrorContains(t, err, test.message, name)
	}
	_, err := service.StartRestore(t.Context(), StartRestoreOptions{EnvironmentID: "0", ProjectID: "other", BackupID: "b1"})
	require.ErrorIs(t, err, common.ErrNotFound)
}

func TestCheckInPlaceRefusesRestoresThatReachBeyondTheProject(t *testing.T) {
	sharedErr := common.Classify(common.ErrValidation, errors.New("project Web is the projects directory"))
	containers := []container.Summary{{Names: []string{"/web-app-1"}, State: container.StateRunning}}
	dockerService := newTestDockerService(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		// Run inside a container, resolving the project folder's mounts inspects Arcane's own container first.
		if r.URL.Path != "/v1.41/containers/json" {
			assert.Regexp(t, `^/v1\.41/containers/[^/]+/json$`, r.URL.Path)
			assert.NoError(t, json.MarshalWrite(w, container.InspectResponse{
				Mounts: []container.MountPoint{{Type: mount.TypeBind, Source: "/srv/arcane", Destination: "/app/data"}},
			}))
			return
		}
		assert.NoError(t, json.MarshalWrite(w, containers))
	}))
	check := func(runsArcane, files bool) error {
		service := NewService(Dependencies{
			Docker:        dockerService,
			RunsArcane:    func(context.Context, string) (bool, error) { return runsArcane, nil },
			SoleDirectory: func(context.Context, types.Project) error { return sharedErr },
		})
		return service.checkInPlace(t.Context(), testProject(), files)
	}
	// Restoring the files of a project whose directory holds other projects would replace their files too.
	require.ErrorIs(t, check(false, true), sharedErr)
	// Its volumes alone can still be restored.
	require.NoError(t, check(false, false))
	// A project that runs Arcane would take Arcane down with it.
	err := check(true, false)
	require.ErrorIs(t, err, common.ErrValidation)
	require.ErrorContains(t, err, "runs Arcane itself")
	// The restore stops only running containers, so it would write under a paused one.
	containers = append(containers, container.Summary{Names: []string{"/web-worker-1"}, State: container.StatePaused})
	err = check(false, false)
	require.ErrorIs(t, err, common.ErrConflict)
	require.ErrorContains(t, err, "container web-worker-1 of project Web is paused; unpause or stop it before restoring")
}

func (f *fakeRestore) step(name string) error {
	f.calls = append(f.calls, name)
	return f.fail[name]
}

func (f *fakeRestore) steps() restoreSteps {
	return restoreSteps{
		report: func(_ context.Context, message string) {
			if message == "Rolling back to the safety backup" {
				f.calls = append(f.calls, "rolling back")
			}
		},
		stop: func(_ context.Context, p restorePlan) error {
			f.mayStop = append(f.mayStop, p.MayStop)
			return f.step("stop")
		},
		deploy: func(context.Context) error { return f.step("deploy") },
		safety: func(context.Context, restorePlan) (string, error) { return "s1", f.step("safety") },
		inUse:  func(context.Context, restorePlan) error { return f.step("in use") },
		create: func(_ context.Context, options client.VolumeCreateOptions) error {
			return f.step("create " + options.Name)
		},
		restore: func(_ context.Context, opts types.ProjectBackupRestore) error {
			names := make([]string, 0, len(opts.Volumes))
			for _, volume := range opts.Volumes {
				names = append(names, volume.Snapshot+">"+volume.Name)
			}
			return f.step("restore " + opts.BackupID + " " + opts.ProjectPath + " " + strings.Join(names, ","))
		},
		verify: func(context.Context, restorePlan) error { return f.step("verify") },
		remove: func(_ context.Context, names []string) error { return f.step("remove " + strings.Join(names, ",")) },
	}
}

func TestRestoreRollsBackToTheSafetyBackup(t *testing.T) {
	plan := func(running bool) restorePlan {
		return restorePlan{
			ID:          "r1",
			BackupID:    "b1",
			ProjectID:   "p1",
			ProjectPath: "/projects/web",
			Volumes:     []types.ProjectBackupRestoreVolume{{Key: "data", Snapshot: "web_data", Name: "shop_data"}, {Key: "cache", Snapshot: "web_cache", Name: "shop_cache"}},
			Create:      []client.VolumeCreateOptions{{Name: "shop_cache"}},
			Files:       true,
			MayStop:     running,
			Running:     running,
		}
	}
	restoreErr := errors.New("rustic failed")

	running := &fakeRestore{fail: map[string]error{"restore b1 /projects/web web_data>shop_data,web_cache>shop_cache": restoreErr}}
	err := running.steps().apply(t.Context(), plan(true))
	require.ErrorIs(t, err, restoreErr)
	require.ErrorContains(t, err, "rolled back to the safety backup s1")
	// The volume the restore created is removed; the one that existed comes back from the safety backup.
	require.Equal(t, []string{
		"stop", "safety", "in use", "create shop_cache", "restore b1 /projects/web web_data>shop_data,web_cache>shop_cache",
		"rolling back", "stop", "restore s1 /projects/web shop_data>shop_data", "remove shop_cache", "deploy",
	}, running.calls)

	stopped := &fakeRestore{fail: map[string]error{"verify": restoreErr}}
	err = stopped.steps().apply(t.Context(), plan(false))
	require.ErrorIs(t, err, restoreErr)
	require.NotContains(t, stopped.calls, "deploy", "a project that did not run is not deployed")
	require.Contains(t, stopped.calls, "restore s1 /projects/web shop_data>shop_data")

	// A failed redeploy of a project that did not run stops what the restore started before the safety backup is
	// restored; projects:down was not checked, so only the restore's own deploy may be stopped.
	redeploy := &fakeRestore{fail: map[string]error{"deploy": restoreErr}}
	redeployPlan := plan(false)
	redeployPlan.Redeploy = new(true)
	err = redeploy.steps().apply(t.Context(), redeployPlan)
	require.ErrorContains(t, err, "rolled back to the safety backup s1")
	require.Equal(t, []string{
		"stop", "safety", "in use", "create shop_cache", "restore b1 /projects/web web_data>shop_data,web_cache>shop_cache", "verify", "deploy",
		"rolling back", "stop", "restore s1 /projects/web shop_data>shop_data", "remove shop_cache",
	}, redeploy.calls)
	require.Equal(t, []bool{false, true}, redeploy.mayStop)

	// A safety backup that cannot be restored leaves the project down instead of starting it on half-restored data.
	undoFailed := &fakeRestore{fail: map[string]error{"verify": restoreErr, "restore s1 /projects/web shop_data>shop_data": errors.New("disk full")}}
	err = undoFailed.steps().apply(t.Context(), plan(true))
	require.ErrorContains(t, err, "the rollback failed too")
	require.ErrorContains(t, err, "the project was left down; restore the safety backup s1")
	require.Equal(t, "remove shop_cache", undoFailed.calls[len(undoFailed.calls)-1])

	// A container that started using a volume after the checks stops the restore before anything is written, so the
	// volume it uses is left alone and the project is deployed again.
	inUse := &fakeRestore{fail: map[string]error{"in use": common.Classify(common.ErrConflict, errors.New("volume shop_data is mounted"))}}
	err = inUse.steps().apply(t.Context(), plan(true))
	require.ErrorIs(t, err, common.ErrConflict)
	require.ErrorContains(t, err, "nothing was restored")
	require.Equal(t, []string{"stop", "safety", "in use", "deploy"}, inUse.calls)
}

func TestRestoreRemovesTheVolumesItCreates(t *testing.T) {
	p := restorePlan{
		ID:        "r1",
		BackupID:  "b1",
		ProjectID: "p1",
		Volumes:   []types.ProjectBackupRestoreVolume{{Key: "cache", Snapshot: "web_cache", Name: "shop_cache"}, {Key: "logs", Snapshot: "web_logs", Name: "shop_logs"}},
		Create:    []client.VolumeCreateOptions{{Name: "shop_cache"}, {Name: "shop_logs"}},
	}
	createErr := errors.New("disk full")
	failed := &fakeRestore{fail: map[string]error{"create shop_logs": createErr}}
	err := failed.steps().apply(t.Context(), p)
	require.ErrorIs(t, err, createErr)
	// The volume created before the failure is removed; the one that failed is not, since it may be someone else's.
	require.Equal(t, []string{"stop", "safety", "in use", "create shop_cache", "create shop_logs", "rolling back", "stop", "restore s1  ", "remove shop_cache"}, failed.calls)
}

func TestVerifyRestoreKeepsTheComposeName(t *testing.T) {
	// The restored files name the project's volumes explicitly, so only the Compose name tells them apart.
	p := restorePlan{ProjectID: "p1", ProjectPath: "/p", ComposeName: "web", Volumes: []types.ProjectBackupRestoreVolume{{Key: "shared", Snapshot: "shared", Name: "shared"}}}
	verify := func(restored string, rewrite func(name string) (string, error)) ([]string, error) {
		current := restored
		var rewrites []string
		service := NewService(Dependencies{
			Project: func(context.Context, string, types.ProjectDetailsOptions) (types.Project, error) {
				project := testProject()
				return types.Project{ID: project.ID, Name: project.Name, ComposeName: current, Path: project.Path, Volumes: project.Volumes}, nil
			},
			RewriteComposeName: func(_ context.Context, projectID, name string) error {
				rewrites = append(rewrites, projectID+" "+name)
				next, err := rewrite(name)
				current = next
				return err
			},
		})
		return rewrites, service.verifyRestore(t.Context(), p)
	}
	applied := func(name string) (string, error) { return name, nil }

	// A pre-rename backup's files declare the old name; they are set back to the current one, as an import does.
	rewrites, err := verify("shop", applied)
	require.NoError(t, err)
	require.Equal(t, []string{"p1 web"}, rewrites)

	// A name the rewrite cannot change, such as one from the global .env, fails the restore so it is rolled back.
	_, err = verify("shop", func(string) (string, error) { return "shop", nil })
	require.ErrorIs(t, err, common.ErrConflict)
	require.ErrorContains(t, err, `name the Compose project "shop" instead of "web"`)
}

func TestMissingVolumesRefusesTargetsOutsideDocker(t *testing.T) {
	live := map[string]types.Volume{
		"web_data": {Name: "web_data", Driver: "local"},
		"shared":   {Name: "shared", Driver: "local", Options: map[string]string{"type": "none", "o": "bind", "device": "/mnt/nas/shared"}},
	}
	service := NewService(Dependencies{BackupStore: noBackupStore, Volume: func(_ context.Context, name string) (types.Volume, bool, error) {
		found, ok := live[name]
		return found, ok, nil
	}})

	// An existing target is restored into; a missing one is created the way Compose would.
	manifest := types.ProjectManifest{Volumes: []types.ProjectVolume{{Key: "data"}, {Key: "cache"}, {Key: "shared"}}}
	create, err := service.missingVolumes(t.Context(), testProject(), manifest, []types.ProjectBackupRestoreVolume{
		{Key: "data", Snapshot: "web_data", Name: "web_data"},
		{Key: "cache", Snapshot: "web_cache", Name: "web_cache"},
	})
	require.NoError(t, err)
	require.Len(t, create, 1)
	require.Equal(t, "web_cache", create[0].Name)

	// The external volume was recreated as a bind mount after the backup, so a restore would write to the host path.
	_, err = service.missingVolumes(t.Context(), testProject(), manifest, []types.ProjectBackupRestoreVolume{{Key: "shared", Snapshot: "shared", Name: "shared"}})
	require.ErrorIs(t, err, common.ErrValidation)
	require.ErrorContains(t, err, "volume shared now keeps its data outside Docker")
}

func TestSafetyBackupRecordsExternalVolumeLabels(t *testing.T) {
	db := newTestDB(t)
	settingsService, err := settings.NewSettingsService(settings.SettingsServiceParams{Ctx: t.Context(), DB: db, Config: &config.Config{}, Env: settings.Env{}})
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, settingsService.Stop(context.WithoutCancel(t.Context()))) })
	live := map[string]volume.Volume{
		"web_data": {Name: "web_data", Driver: "local"},
		"shared":   {Name: "shared", Driver: "local", Labels: map[string]string{"team": "ops"}},
	}
	var manifest types.ProjectManifest
	dockerService := newTestDockerService(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := strings.TrimPrefix(r.URL.Path, "/v1.41")
		w.Header().Set("Content-Type", "application/json")
		switch name, isVolume := strings.CutPrefix(path, "/volumes/"); {
		case isVolume && r.Method == http.MethodGet:
			found, ok := live[name]
			if !ok {
				http.NotFound(w, r)
				return
			}
			assert.NoError(t, json.MarshalWrite(w, found))
		case path == "/volumes/create":
			assert.NoError(t, json.MarshalWrite(w, volume.Volume{Name: "meta"}))
		case path == "/containers/json":
			assert.NoError(t, json.MarshalWrite(w, []container.Summary{}))
		case strings.HasPrefix(path, "/images/"):
			_, writeErr := w.Write([]byte("{}"))
			assert.NoError(t, writeErr)
		case path == "/containers/create":
			w.WriteHeader(http.StatusCreated)
			assert.NoError(t, json.MarshalWrite(w, container.CreateResponse{ID: "holder"}))
		case path == "/containers/holder/archive":
			reader := tar.NewReader(r.Body)
			for header, nextErr := reader.Next(); nextErr == nil; header, nextErr = reader.Next() {
				if header.Name == manifestName {
					assert.NoError(t, json.UnmarshalRead(reader, &manifest))
				}
			}
		default:
			// Failing the holder's removal ends the safety backup once its manifest is written.
			http.Error(w, "unavailable", http.StatusInternalServerError)
		}
	}))
	service := NewService(Dependencies{
		BackupStore: noBackupStore,
		DB:          db,
		Docker:      dockerService,
		Settings:    settingsService,
		Project: func(context.Context, string, types.ProjectDetailsOptions) (types.Project, error) {
			return testProject(), nil
		},
		Volume: func(_ context.Context, name string) (types.Volume, bool, error) {
			found, ok := live[name]
			return types.NewVolume(found), ok, nil
		},
	})

	_, err = service.safetyBackup(t.Context(), restorePlan{
		ProjectID: "p1",
		Volumes: []types.ProjectBackupRestoreVolume{
			{Key: "data", Snapshot: "web_data", Name: "web_data"},
			{Key: "shared", Snapshot: "shared", Name: "shared"},
			{Key: "cache", Snapshot: "web_cache", Name: "web_cache"},
		},
		Create: []client.VolumeCreateOptions{{Name: "web_cache"}},
	})
	require.ErrorContains(t, err, "write project backup manifest")
	included := map[string]bool{}
	labels := map[string]map[string]string{}
	for _, captured := range manifest.Volumes {
		included[captured.Key], labels[captured.Key] = captured.Included, captured.DockerLabels
	}
	// The targets that existed are captured; the one the restore creates is not.
	require.Equal(t, map[string]bool{"cache": false, "data": true, "nfs": false, "shared": true}, included)
	// Restoring the safety backup after the external volume is gone recreates it with its own labels.
	require.Equal(t, map[string]string{"team": "ops"}, labels["shared"])
	require.Nil(t, labels["data"])
}

func (f *fakeScheduler) Submit(context.Context, types.SchedulerRequest) (types.SchedulerRun, error) {
	return types.SchedulerRun{}, nil
}

func (f *fakeScheduler) AddJob(_ context.Context, job types.SchedulerJob) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.jobs[job.Name()] = job
	return nil
}

func (f *fakeScheduler) RemoveJob(_ context.Context, name string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.jobs, name)
}

func (f *fakeScheduler) HasJob(name string) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	_, ok := f.jobs[name]
	return ok
}

// newPolicyFixture backs the service with real admissions and an S3 destination whose repository is missing, so
// deleting a backup stored only there needs no backup repository.
func newPolicyFixture(t *testing.T) policyFixture {
	t.Helper()
	db := newTestDB(t)
	harness := flowtest.New(t, activity.NewActivityService(db, nil))
	gate := runs.NewAdmission(harness.Runtime.Service(), t.Name())
	require.NoError(t, gate.Register(harness.Runtime))
	engine := backup.NewEngine(gate, nil, func(context.Context, types.BackupRequester) error { return nil })
	t.Cleanup(func() { require.NoError(t, engine.Stop(context.WithoutCancel(t.Context()))) })
	settingsService, err := settings.NewSettingsService(settings.SettingsServiceParams{Ctx: t.Context(), DB: db, Config: &config.Config{}, Env: settings.Env{}})
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, settingsService.Stop(context.WithoutCancel(t.Context()))) })
	require.NoError(t, settingsService.SetIntSetting(t.Context(), "unmanagedBackupRetentionDays", 1))

	storage := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/xml")
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`<?xml version="1.0" encoding="UTF-8"?><Error><Code>NoSuchKey</Code><Message>missing</Message></Error>`))
	}))
	t.Cleanup(storage.Close)
	crypto.InitEncryption(crypto.Config{EncryptionKey: "test-encryption-key-for-project-backups-32b", Environment: "test"})
	secret, err := crypto.Encrypt("secret-key")
	require.NoError(t, err)
	destination := &s3.S3Destination{
		Name: "Offsite", Endpoint: strings.TrimPrefix(storage.URL, "http://"), Bucket: "backups", Region: "us-east-1",
		AccessKeyID: "access-key", SecretAccessKey: secret, ForcePathStyle: true,
	}
	require.NoError(t, db.Create(destination).Error)

	jobs := &fakeScheduler{jobs: map[string]types.SchedulerJob{}}
	references := &[]types.ProjectReference{{ID: "p1", Name: "Web"}, {ID: "p2", Name: "Shop"}, {ID: "p3", Name: "Wiki"}}
	dockerService := newTestDockerService(t, http.NotFoundHandler())
	destinationService := s3.NewS3DestinationService(db, func(context.Context, string) error { return nil })
	service := NewService(Dependencies{
		DB:     db,
		Docker: dockerService,
		Engine: engine,
		Repository: backup.NewVolumeRepository(backup.VolumeRepositoryDependencies{
			Docker: dockerService, Engine: engine, Settings: settingsService, S3Destinations: destinationService,
			RecoveryKeys: backup.NewRecoveryKeyStore(db), BackupVolumeName: "arcane-backups",
		}),
		S3Destinations: destinationService,
		Settings:       settingsService,
		LockVolumes:    lockingWith(&concurrency.KeyedMutex{}),
		Project: func(_ context.Context, projectID string, _ types.ProjectDetailsOptions) (types.Project, error) {
			project := testProject()
			return types.Project{ID: projectID, Name: project.Name, ComposeName: project.ComposeName, Path: project.Path, Volumes: project.Volumes}, nil
		},
		Volume:       existingVolumes("web_data", "web_nfs", "shared"),
		BackupStore:  noBackupStore,
		ProjectName:  func(context.Context, string) (string, error) { return "Web", nil },
		ListProjects: func(context.Context) ([]types.ProjectReference, error) { return slices.Clone(*references), nil },
		LockProject:  func(context.Context, string, string) (func(), error) { return func() {}, nil },
		Config:       &config.Config{},
		Ctx:          t.Context(),
		Scheduler:    jobs,
		Admission:    gate,
	})
	require.NoError(t, service.RegisterWorkflows(harness.Engine))
	harness.Start(t)
	return policyFixture{db: db, engine: engine, service: service, jobs: jobs, projects: references, destination: destination.ID}
}

func (f policyFixture) policy(t *testing.T, policy ProjectBackupPolicy) ProjectBackupPolicy {
	t.Helper()
	require.NoError(t, f.db.Create(&policy).Error)
	return policy
}

func (f policyFixture) hold(t *testing.T, scope, id string) *runs.Lease {
	t.Helper()
	lease, admitted, err := f.engine.TryAcquireRun(t.Context(), scope, id)
	require.NoError(t, err)
	require.True(t, admitted, id)
	return lease
}

// remoteBackup is a finished project backup whose only snapshot is in the fixture's missing S3 repository.
func (f policyFixture) remoteBackup(t *testing.T, id, trigger, policyID string, age time.Duration) {
	t.Helper()
	require.NoError(t, f.db.Create(&ProjectBackup{
		BaseModel: database.BaseModel{ID: id, CreatedAt: time.Now().Add(-age)}, ProjectID: "p1", Status: types.VolumeBackupStatusSucceeded,
		Trigger: trigger, PolicyID: policyID, Destination: types.VolumeBackupDestinationS3, S3DestinationID: f.destination, RemoteSnapshotID: "snapshot-" + id,
	}).Error)
}

func (f policyFixture) backupIDs(t *testing.T) []string {
	t.Helper()
	var ids []string
	require.NoError(t, f.db.Model(&ProjectBackup{}).Order("id").Pluck("id", &ids).Error)
	return ids
}

func TestUpdatePoliciesSavesAndSchedulesPolicies(t *testing.T) {
	f := newPolicyFixture(t)
	base := types.UpdateBackupPolicy{Enabled: true, Schedule: "0 2 * * *", RetentionCount: 3, LocalEnabled: true}

	collection, err := f.service.UpdatePolicies(t.Context(), "p1", []types.UpdateProjectBackupPolicy{
		{UpdateBackupPolicy: base},
		{UpdateBackupPolicy: base, Volumes: []string{"data", "data"}, StopContainers: new(false)},
	})
	require.NoError(t, err)
	require.Len(t, collection.Policies, 2)
	// No selection follows the project's volumes; an absent stopContainers stops them as a manual backup does.
	require.Nil(t, collection.Policies[0].Volumes)
	require.True(t, collection.Policies[0].StopContainers)
	require.Equal(t, []string{"data"}, collection.Policies[1].Volumes)
	require.False(t, collection.Policies[1].StopContainers)
	for _, policy := range collection.Policies {
		require.True(t, f.jobs.HasJob("project-backup:"+policy.ID))
	}

	_, err = f.service.UpdatePolicies(t.Context(), "p1", []types.UpdateProjectBackupPolicy{{UpdateBackupPolicy: base, Volumes: []string{"missing"}}})
	require.ErrorIs(t, err, common.ErrValidation)

	// Disabling one policy and leaving out the other unschedules both.
	disabled := base
	disabled.ID, disabled.Enabled = collection.Policies[0].ID, false
	collection, err = f.service.UpdatePolicies(t.Context(), "p1", []types.UpdateProjectBackupPolicy{{UpdateBackupPolicy: disabled}})
	require.NoError(t, err)
	require.Len(t, collection.Policies, 1)
	require.Empty(t, f.jobs.jobs)

	// Deleting the project removes its policies and their jobs.
	disabled.Enabled = true
	_, err = f.service.UpdatePolicies(t.Context(), "p1", []types.UpdateProjectBackupPolicy{{UpdateBackupPolicy: disabled}})
	require.NoError(t, err)
	require.Len(t, f.jobs.jobs, 1)
	f.service.RemovePolicies(t.Context(), "p1")
	require.Empty(t, f.jobs.jobs)
	var remaining int64
	require.NoError(t, f.db.Model(&ProjectBackupPolicy{}).Count(&remaining).Error)
	require.Zero(t, remaining)
}

func TestScheduledBackupWaitsForTheAdmissionAndReadsThePolicyAgain(t *testing.T) {
	f := newPolicyFixture(t)
	policy := f.policy(t, ProjectBackupPolicy{ProjectID: "p1", Schedule: "0 2 * * *", Enabled: true, StopContainers: true, LocalEnabled: true})
	held := f.hold(t, admissionScope, "p1")
	done := make(chan admittedRun, 1)
	go func() {
		admitted, err := f.service.admitScheduled(t.Context(), "p1", policy.ID, func(ctx context.Context) (types.CreateProjectBackupRequest, bool, error) {
			return f.service.scheduledPolicy(ctx, policy.ID)
		})
		assert.NoError(t, err)
		done <- admitted
	}()
	select {
	case <-done:
		t.Fatal("the scheduled backup ran while another run held its admission")
	case <-time.After(300 * time.Millisecond):
	}

	// The run waits instead of failing, then backs up with the policy as it stands once admitted.
	require.NoError(t, f.db.Model(&ProjectBackupPolicy{}).Where("id = ?", policy.ID).
		Updates(map[string]any{"stop_containers": false, "volumes": `["data"]`}).Error)
	held.Release(t.Context())
	var admitted admittedRun
	select {
	case admitted = <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("the scheduled backup was not admitted after the other run finished")
	}
	require.False(t, admitted.plan.StopContainers)
	require.Equal(t, []string{"web_data"}, admitted.plan.volumeNames())
	_, busy, err := f.engine.TryAcquireRun(t.Context(), backup.VolumeAdmissionScope, "web_data")
	require.NoError(t, err)
	require.False(t, busy, "the run holds the lease of the volume it captures")
	release(t.Context(), admitted.leases)
}

func TestRetentionKeepsTheNewestBackupsOfThePolicy(t *testing.T) {
	f := newPolicyFixture(t)
	policy := f.policy(t, ProjectBackupPolicy{ProjectID: "p1", Schedule: "0 2 * * *", Enabled: true, LocalEnabled: true, RetentionCount: 2})
	other := f.policy(t, ProjectBackupPolicy{ProjectID: "p1", Schedule: "0 3 * * *", Enabled: true, LocalEnabled: true, RetentionCount: 1})
	for i, age := range []time.Duration{time.Hour, 2 * time.Hour, 3 * time.Hour, 4 * time.Hour} {
		f.remoteBackup(t, fmt.Sprintf("s%d", i+1), types.VolumeBackupTriggerScheduled, policy.ID, age)
	}
	f.remoteBackup(t, "manual", types.VolumeBackupTriggerManual, "", 10*time.Hour)
	f.remoteBackup(t, "safety", types.VolumeBackupTriggerSafety, "", 10*time.Hour)
	f.remoteBackup(t, "other", types.VolumeBackupTriggerScheduled, other.ID, 10*time.Hour)

	// A restore as new holds the project's backups, so a scheduled run, with its retention, and Delete wait or fail.
	releaseBackups, err := f.service.HoldBackups(t.Context(), "p1")
	require.NoError(t, err)
	_, err = f.service.HoldBackups(t.Context(), "p1")
	require.ErrorIs(t, err, errAlreadyRunning)
	require.ErrorIs(t, f.service.Delete(t.Context(), "p1", "s4"), errAlreadyRunning)
	releaseBackups(t.Context())

	// Only the policy's own backups beyond its count go; manual, safety and other schedules' backups stay.
	require.NoError(t, f.service.retain(t.Context(), policy.ID))
	require.Equal(t, []string{"manual", "other", "s1", "s2", "safety"}, f.backupIDs(t))
}

func TestDeletedPolicyBackupsBecomeUnmanaged(t *testing.T) {
	f := newPolicyFixture(t)
	base := types.UpdateBackupPolicy{Enabled: true, Schedule: "0 2 * * *", RetentionCount: 3, LocalEnabled: true}
	collection, err := f.service.UpdatePolicies(t.Context(), "p1", []types.UpdateProjectBackupPolicy{{UpdateBackupPolicy: base}, {UpdateBackupPolicy: base}})
	require.NoError(t, err)
	kept, deleted := collection.Policies[0], collection.Policies[1]
	f.remoteBackup(t, "kept", types.VolumeBackupTriggerScheduled, kept.ID, 72*time.Hour)
	f.remoteBackup(t, "orphan", types.VolumeBackupTriggerScheduled, deleted.ID, 72*time.Hour)
	f.remoteBackup(t, "manual", types.VolumeBackupTriggerManual, "", 72*time.Hour)
	keptUpdate := base
	keptUpdate.ID = kept.ID
	_, err = f.service.UpdatePolicies(t.Context(), "p1", []types.UpdateProjectBackupPolicy{{UpdateBackupPolicy: keptUpdate}})
	require.NoError(t, err)

	// A sweep two days ago marked the deleted policy's backups unmanaged, and only those.
	_, err = f.service.expiredUnmanaged(t.Context(), types.UnmanagedBackupSweep{Now: time.Now().Add(-48 * time.Hour), Retention: 24 * time.Hour})
	require.NoError(t, err)
	var orphaned []string
	require.NoError(t, f.db.Model(&ProjectBackup{}).Where("orphaned_at IS NOT NULL").Pluck("id", &orphaned).Error)
	require.Equal(t, []string{"orphan"}, orphaned)

	// Past the one-day unmanaged retention they are deleted; a busy project's wait for the next run.
	held := f.hold(t, admissionScope, "p1")
	require.NoError(t, f.service.PruneUnmanagedBackups(t.Context()))
	require.Equal(t, []string{"kept", "manual", "orphan"}, f.backupIDs(t))
	held.Release(t.Context())
	require.NoError(t, f.service.PruneUnmanagedBackups(t.Context()))
	require.Equal(t, []string{"kept", "manual"}, f.backupIDs(t))
}

// globalPolicies saves the global policies and returns them as saved.
func (f policyFixture) globalPolicies(t *testing.T, updates ...types.UpdateGlobalProjectBackupPolicy) []types.GlobalProjectBackupPolicy {
	t.Helper()
	collection, err := f.service.UpdateGlobalPolicies(t.Context(), updates)
	require.NoError(t, err)
	return collection.Policies
}

func TestUpdateGlobalPoliciesSavesAndSchedules(t *testing.T) {
	f := newPolicyFixture(t)
	base := types.UpdateBackupPolicy{Enabled: true, Schedule: "0 2 * * *", RetentionCount: 3, LocalEnabled: true}

	policies := f.globalPolicies(t,
		types.UpdateGlobalProjectBackupPolicy{UpdateBackupPolicy: base, SelectionMode: types.SystemVolumeSelectionAll},
		types.UpdateGlobalProjectBackupPolicy{
			UpdateBackupPolicy: base, SelectionMode: types.SystemVolumeSelectionBlocklist, ProjectIDs: []string{" p2 ", "p2"}, StopContainers: new(false),
		},
	)
	require.Len(t, policies, 2)
	require.True(t, policies[0].StopContainers)
	require.Equal(t, []string{"p2"}, policies[1].ProjectIDs)
	require.False(t, policies[1].StopContainers)
	for _, policy := range policies {
		require.True(t, f.jobs.HasJob("project-backup:"+types.GlobalProjectPolicyPrefix+policy.ID))
	}

	// Disabling one policy and leaving out the other unschedules both.
	disabled := types.UpdateBackupPolicy{ID: policies[0].ID, Schedule: "0 2 * * *", LocalEnabled: true}
	policies = f.globalPolicies(t, types.UpdateGlobalProjectBackupPolicy{UpdateBackupPolicy: disabled, SelectionMode: types.SystemVolumeSelectionAll})
	require.Len(t, policies, 1)
	require.False(t, policies[0].Enabled)
	require.Empty(t, f.jobs.jobs)
}

func TestGlobalPolicySelection(t *testing.T) {
	references := []types.ProjectReference{{ID: "p1", Name: "Web"}, {ID: "p2", Name: "Shop"}, {ID: "p3", Name: "Wiki"}}
	for mode, expected := range map[types.SystemVolumeSelectionMode][]string{
		types.SystemVolumeSelectionAll:       {"p1", "p2", "p3"},
		types.SystemVolumeSelectionAllowlist: {"p2"},
		types.SystemVolumeSelectionBlocklist: {"p1", "p3"},
	} {
		selected := []string{}
		for _, project := range selectProjects(types.GlobalProjectBackupPolicy{SelectionMode: mode, ProjectIDs: []string{"p2"}}, references) {
			selected = append(selected, project.ID)
		}
		require.Equal(t, expected, selected, mode)
	}
}

func TestGlobalRunBacksUpSelectedProjectsAndAppliesRetention(t *testing.T) {
	f := newPolicyFixture(t)
	policy := f.globalPolicies(t, types.UpdateGlobalProjectBackupPolicy{
		UpdateBackupPolicy: types.UpdateBackupPolicy{Enabled: true, Schedule: "0 2 * * *", RetentionCount: 1, LocalEnabled: true},
		SelectionMode:      types.SystemVolumeSelectionAllowlist,
		ProjectIDs:         []string{"p1", "p2"},
		StopContainers:     new(false),
	})[0]
	// p2's own enabled policy replaces the global one.
	f.policy(t, ProjectBackupPolicy{ProjectID: "p2", Schedule: "0 3 * * *", Enabled: true, LocalEnabled: true})
	series := types.GlobalProjectPolicyPrefix + policy.ID + ":p1"
	// Earlier runs saved their snapshot in the fixture's missing S3 repository, then failed.
	for i, age := range []time.Duration{time.Hour, 2 * time.Hour} {
		require.NoError(t, f.db.Create(&ProjectBackup{
			BaseModel: database.BaseModel{ID: fmt.Sprintf("f%d", i+1), CreatedAt: time.Now().Add(-age)}, ProjectID: "p1",
			Status: types.VolumeBackupStatusFailed, Trigger: types.VolumeBackupTriggerScheduled, PolicyID: series,
			Destination: types.VolumeBackupDestinationS3, S3DestinationID: f.destination, RemoteSnapshotID: fmt.Sprintf("remote-f%d", i+1),
		}).Error)
	}

	outcome, err := f.service.backUpGlobal(t.Context(), globalRun{PolicyID: policy.ID}, types.SchedulerRun{})
	require.NoError(t, err)
	// p1's backup fails without Docker and is reported; p2 is skipped; the older failed run of p1 is deleted.
	require.Equal(t, types.RunStatusPartial, outcome.Status)
	require.Len(t, outcome.Targets, 1)
	require.Equal(t, "p1", outcome.Targets[0].ID)
	require.Equal(t, types.RunStatusFailed, outcome.Targets[0].Status)
	ids := f.backupIDs(t)
	require.Len(t, ids, 2)
	require.Contains(t, ids, "f1")
	require.NotContains(t, ids, "f2")
}

func TestGlobalRetentionKeepsEachProjectsNewestAndManagedSeries(t *testing.T) {
	f := newPolicyFixture(t)
	base := types.UpdateBackupPolicy{Enabled: true, Schedule: "0 2 * * *", RetentionCount: 1, LocalEnabled: true}
	policies := f.globalPolicies(t,
		types.UpdateGlobalProjectBackupPolicy{UpdateBackupPolicy: base, SelectionMode: types.SystemVolumeSelectionAllowlist, ProjectIDs: []string{"p1", "p2"}},
		types.UpdateGlobalProjectBackupPolicy{UpdateBackupPolicy: base, SelectionMode: types.SystemVolumeSelectionAll},
	)
	kept, removed := policies[0], policies[1]
	series := func(policy types.GlobalProjectBackupPolicy, projectID string) string {
		return types.GlobalProjectPolicyPrefix + policy.ID + ":" + projectID
	}
	f.remoteBackup(t, "web-new", types.VolumeBackupTriggerScheduled, series(kept, "p1"), time.Hour)
	f.remoteBackup(t, "web-old", types.VolumeBackupTriggerScheduled, series(kept, "p1"), 2*time.Hour)
	f.remoteBackup(t, "shop", types.VolumeBackupTriggerScheduled, series(kept, "p2"), time.Hour)
	f.remoteBackup(t, "wiki", types.VolumeBackupTriggerScheduled, series(kept, "p3"), time.Hour)
	f.remoteBackup(t, "deleted-policy", types.VolumeBackupTriggerScheduled, series(removed, "p1"), time.Hour)

	// Retention keeps the policy's newest backups per project.
	require.NoError(t, f.service.retain(t.Context(), series(kept, "p1")))
	require.Equal(t, []string{"deleted-policy", "shop", "web-new", "wiki"}, f.backupIDs(t))

	// A series stays managed while its policy exists and selects its project.
	keptUpdate := types.UpdateBackupPolicy{ID: kept.ID, Enabled: true, Schedule: "0 2 * * *", LocalEnabled: true}
	f.globalPolicies(t, types.UpdateGlobalProjectBackupPolicy{UpdateBackupPolicy: keptUpdate, SelectionMode: types.SystemVolumeSelectionAllowlist, ProjectIDs: []string{"p1", "p2"}})
	*f.projects = []types.ProjectReference{{ID: "p1", Name: "Web"}, {ID: "p3", Name: "Wiki"}}
	_, err := f.service.expiredUnmanaged(t.Context(), types.UnmanagedBackupSweep{Now: time.Now(), Retention: 24 * time.Hour})
	require.NoError(t, err)
	var orphaned []string
	require.NoError(t, f.db.Model(&ProjectBackup{}).Where("orphaned_at IS NOT NULL").Order("id").Pluck("id", &orphaned).Error)
	require.Equal(t, []string{"deleted-policy", "shop", "wiki"}, orphaned)
}

func TestRestoreWaitsForEveryVolumeOfTheProject(t *testing.T) {
	db := newTestDB(t)
	harness := flowtest.New(t, activity.NewActivityService(db, nil))
	gate := runs.NewAdmission(harness.Runtime.Service(), t.Name())
	require.NoError(t, gate.Register(harness.Runtime))
	engine := backup.NewEngine(gate, nil, func(context.Context, types.BackupRequester) error { return nil })
	t.Cleanup(func() { require.NoError(t, engine.Stop(context.WithoutCancel(t.Context()))) })
	harness.Start(t)
	locks := &concurrency.KeyedMutex{}
	service := NewService(Dependencies{
		DB: db, Engine: engine, LockVolumes: lockingWith(locks),
		Project: func(context.Context, string, types.ProjectDetailsOptions) (types.Project, error) {
			return testProject(), nil
		},
		LockProject: func(context.Context, string, string) (func(), error) { return func() {}, nil },
	})
	p := restorePlan{ID: "r1", BackupID: "b1", ProjectID: "p1", Volumes: []types.ProjectBackupRestoreVolume{{Key: "data", Snapshot: "web_data", Name: "web_data"}}}

	// A volume backup of the cache volume can start the project's containers; the restore of data waits for it.
	unlock := locks.Lock("web_cache")
	waiting, cancel := context.WithTimeout(t.Context(), 50*time.Millisecond)
	defer cancel()
	_, err := service.lockRestore(waiting, p)
	require.ErrorIs(t, err, context.DeadlineExceeded)
	unlock()

	release, err := service.lockRestore(t.Context(), p)
	require.NoError(t, err)
	release()
}

func TestCaptureWaitsForEveryVolumeOfTheProject(t *testing.T) {
	locks := &concurrency.KeyedMutex{}
	service := NewService(Dependencies{LockVolumes: lockingWith(locks)})
	p := plan{BackupID: "b1", ProjectID: "p1", Volumes: []types.ProjectVolume{
		{Key: "data", Name: "web_data", Included: true},
		{Key: "cache", Name: "web_cache", Included: false},
	}}
	// A volume backup of the cache volume has the project's containers stopped; the project backup waits for it.
	unlock := locks.Lock("web_cache")
	defer unlock()
	waiting, cancel := context.WithTimeout(t.Context(), 50*time.Millisecond)
	defer cancel()
	require.ErrorIs(t, service.capture(waiting, &ProjectBackup{BaseModel: database.BaseModel{ID: "b1"}, ProjectID: "p1"}, p), context.DeadlineExceeded)
}

func TestContainerUseCountsOnlyThisProject(t *testing.T) {
	labelsFor := func(project, service string) map[string]string {
		return map[string]string{api.ProjectLabel: project, api.ServiceLabel: service}
	}
	arcane := labelsFor("demo", "arcane")
	arcane["com.getarcaneapp.arcane"] = "true"
	dockerService := newTestDockerService(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "/containers/json") {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		assert.NoError(t, json.MarshalWrite(w, []container.Summary{
			{ID: "web-1", Labels: labelsFor("demo", "web"), State: container.StateRunning},
			{ID: "db-1", Labels: labelsFor("demo", "db"), State: container.StateRestarting},
			{ID: "cache-1", Labels: labelsFor("demo", "cache"), State: container.StatePaused},
			{ID: "arcane-1", Labels: arcane, State: container.StateRunning, Mounts: []container.MountPoint{{Type: mount.TypeVolume, Name: "demo_config"}}},
			{ID: "other-1", Labels: labelsFor("other", "app"), State: container.StateRunning, Mounts: []container.MountPoint{{Type: mount.TypeVolume, Name: "demo_data"}}},
			{ID: "helper-1", Labels: volumes.Labels(), State: container.StateRunning, Mounts: []container.MountPoint{{Type: mount.TypeVolume, Name: "browsed_data"}}},
		}))
	}))
	service := NewService(Dependencies{Docker: dockerService})

	active, inUse, err := service.containerUse(t.Context(), "demo")
	require.NoError(t, err)
	// The paused cache, Arcane itself, and the other project are not stopped.
	assert.Equal(t, []string{"web-1", "db-1"}, containerIDs(active))
	// Another project's container and Arcane itself keep running, so their volumes count as in use; an idle
	// file-browser helper writes nothing.
	assert.Equal(t, map[string]bool{"demo_config": true, "demo_data": true}, inUse)
}

func containerIDs(summaries []container.Summary) []string {
	ids := make([]string, 0, len(summaries))
	for _, c := range summaries {
		ids = append(ids, c.ID)
	}
	return ids
}

func TestRestoreNeedsTheRightsToStopAndDeployTheProject(t *testing.T) {
	for _, test := range []struct {
		name string
		want []string
		plan restorePlan
	}{
		{name: "a running project is stopped and deployed again", plan: restorePlan{MayStop: true}, want: []string{authz.PermProjectsDown, authz.PermProjectsDeploy}},
		{name: "a stopped project asked to start is deployed", plan: restorePlan{Redeploy: new(true)}, want: []string{authz.PermProjectsDeploy}},
		{name: "a stopped project left stopped needs nothing more", plan: restorePlan{Redeploy: new(false)}},
	} {
		t.Run(test.name, func(t *testing.T) {
			require.Equal(t, test.want, test.plan.extraPermissions())
		})
	}
}

func TestBackupRefusesVolumesRemovedOrReplacedSinceItWasQueued(t *testing.T) {
	created := map[string]string{"web_data": "2026-11-20T09:00:00Z"}
	service := NewService(Dependencies{BackupStore: noBackupStore, Volume: func(_ context.Context, name string) (types.Volume, bool, error) {
		at, exists := created[name]
		return types.Volume{Name: name, CreatedAt: at}, exists, nil
	}})
	before, err := service.volumeIdentities(t.Context(), []string{"web_data"})
	require.NoError(t, err)

	// Mounting a volume removed while the backup waited would create it empty, so the backup stops instead.
	_, err = service.volumeIdentities(t.Context(), []string{"web_data", "web_cache"})
	require.ErrorIs(t, err, common.ErrConflict)
	require.ErrorContains(t, err, "volume web_cache was removed after the backup was queued")

	// A volume removed and created again during the copy is a different volume, which capture refuses to keep.
	created["web_data"] = "2026-11-20T09:05:00Z"
	after, err := service.volumeIdentities(t.Context(), []string{"web_data"})
	require.NoError(t, err)
	require.NotEqual(t, before, after)
}

// noBackupStore is a Docker host without a backup volume.
func noBackupStore(context.Context) string { return "" }

// lockingWith stands in for the volume service's LockVolumes, taking each volume's lock from locks.
func lockingWith(locks *concurrency.KeyedMutex) func(context.Context, []string) (func(), error) {
	return func(ctx context.Context, names []string) (func(), error) {
		var unlocks []func()
		release := func() {
			for _, unlock := range slices.Backward(unlocks) {
				unlock()
			}
		}
		for _, name := range names {
			unlock, err := locks.LockContext(ctx, name)
			if err != nil {
				release()
				return nil, err
			}
			unlocks = append(unlocks, unlock)
		}
		return release, nil
	}
}
