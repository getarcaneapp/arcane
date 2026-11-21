package backup

import (
	"archive/tar"
	"cmp"
	"context"
	"encoding/json/v2"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	composetypes "github.com/compose-spec/compose-go/v2/types"
	"github.com/docker/compose/v5/pkg/api"
	"github.com/libtnb/sqlite"
	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/api/types/mount"
	"github.com/moby/moby/api/types/volume"
	"github.com/moby/moby/client"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"kit.getarcane.app"
	"kit.getarcane.app/concurrency"
	"kit.getarcane.app/docker/streams/bus"

	"getarcane.dev/arcane/v3/internal/activity"
	"getarcane.dev/arcane/v3/internal/backup"
	"getarcane.dev/arcane/v3/internal/common"
	"getarcane.dev/arcane/v3/internal/config"
	"getarcane.dev/arcane/v3/internal/database"
	"getarcane.dev/arcane/v3/internal/docker"
	"getarcane.dev/arcane/v3/internal/scheduler/flow/flowtest"
	"getarcane.dev/arcane/v3/internal/scheduler/runs"
	"getarcane.dev/arcane/v3/internal/settings"
	"getarcane.dev/arcane/v3/pkg/authz"
	"getarcane.dev/arcane/v3/pkg/projects"
	"getarcane.dev/arcane/v3/pkg/rustic"
	"getarcane.dev/arcane/v3/pkg/volumes"
	"getarcane.dev/types/v3"
)

// fakeRestore records the steps an in-place restore applies and fails the ones it names.
type fakeRestore struct {
	calls      []string
	fail       map[string]error
	plans      []restorePlan
	rolledBack bool
}

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
		Volumes: composetypes.Volumes{
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
	require.NoError(t, checkPlanVolumes(testProject(), p))

	// A compose edit and deploy while the backup waited moved the data volume to a new name.
	changed := testProject()
	changed.Volumes["data"] = composetypes.VolumeConfig{Name: "web_data_v2"}
	err = checkPlanVolumes(changed, p)
	require.ErrorIs(t, err, common.ErrConflict)
	require.ErrorContains(t, err, "changed after the backup was queued")
}

func TestBackupManifestKind(t *testing.T) {
	project := testProject()
	p := plan{Volumes: []types.ProjectVolume{{Key: "data", Name: "web_data", Kind: types.ProjectVolumeNamed, Included: true}}}

	manifest := backupManifest(project, p, true, false)
	require.Equal(t, types.ProjectManifestKindBackup, manifest.Kind)
	require.Equal(t, types.ProjectManifestFormatVersion, manifest.FormatVersion)
	require.Equal(t, types.ProjectManifestProject{ID: "p1", Name: "Web", ComposeName: "web"}, manifest.Project)
	require.Equal(t, p.Volumes, manifest.Volumes)
	require.False(t, manifest.Consistent, "a live backup of a running project is not consistent")
	require.False(t, manifest.ExternalEnv)
	require.Equal(t, []string{project.Path}, manifest.ProjectDirs, "a path that doesn't resolve is recorded as configured")
	require.True(t, backupManifest(project, p, true, true).ExternalEnv)
	require.True(t, backupManifest(project, p, false, false).Consistent, "nothing runs, so a live backup is consistent")

	p.StopContainers = true
	require.True(t, backupManifest(project, p, true, false).Consistent)

	// Other projects are not stopped, so a captured volume one of them mounts makes the copy inconsistent.
	p.Volumes[0].InUse = true
	require.False(t, backupManifest(project, p, true, false).Consistent)
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

func TestRestoreToHoldsTheVolumesItWasAskedToLock(t *testing.T) {
	db := newTestDB(t)
	harness := flowtest.New(t, activity.NewActivityService(db, nil))
	gate := runs.NewAdmission(harness.Runtime.Service(), t.Name())
	require.NoError(t, gate.Register(harness.Runtime))
	engine := backup.NewEngine(gate, nil, func(context.Context, types.BackupRequester) error { return nil })
	t.Cleanup(func() { require.NoError(t, engine.Stop(context.WithoutCancel(t.Context()))) })
	locks := &concurrency.KeyedMutex{}
	service := NewService(Dependencies{DB: db, Engine: engine, LockVolumes: lockingWith(locks)})
	require.NoError(t, service.RegisterWorkflows(harness.Engine))
	harness.Start(t)
	opts := types.ProjectBackupRestore{
		ProjectID: "p1", BackupID: "missing", LockVolumes: true,
		Volumes: []types.ProjectBackupRestoreVolume{{Key: "data", Snapshot: "old_data", Name: "new_data"}},
	}

	// A volume backup of the new volume makes the restore wait its turn instead of copying under it.
	lease, admitted, err := engine.TryAcquireRun(t.Context(), backup.VolumeAdmissionScope, "new_data")
	require.NoError(t, err)
	require.True(t, admitted)
	require.ErrorIs(t, service.RestoreTo(t.Context(), opts), common.ErrConflict)
	lease.Release(t.Context())

	// File edits through the volume browser hold the lock the copy needs.
	unlock := locks.Lock("new_data")
	waiting, cancel := context.WithTimeout(t.Context(), 50*time.Millisecond)
	defer cancel()
	require.ErrorIs(t, service.RestoreTo(waiting, opts), context.DeadlineExceeded)
	unlock()

	// Once the copy ends, either way, both are free again.
	require.Error(t, service.RestoreTo(t.Context(), opts))
	lease, admitted, err = engine.TryAcquireRun(t.Context(), backup.VolumeAdmissionScope, "new_data")
	require.NoError(t, err)
	require.True(t, admitted)
	lease.Release(t.Context())
	locks.Lock("new_data")()
}

func TestRestoreEnvPutsBackAnExternalEnv(t *testing.T) {
	for _, test := range []struct {
		name     string
		external string
		project  string
		// worker is what the other project's .env is: a regular file, a link to the file web's .env leads to, a link
		// to a directory, or the file web's .env leads to.
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
		// A link to a directory leads to no file this project's .env could be, so it doesn't block the restore.
		{name: "in place, another project links to a directory", worker: "directory", external: "TOKEN=restored"},
		// Another project's .env that can't be resolved can't be this file, so it doesn't stop the restore.
		{name: "in place, another project's link loops", worker: "loop", external: "TOKEN=restored"},
		// web/.env -> ../worker/.env names worker's regular .env through the linked projects root.
		{name: "in place, leading to another project's .env", worker: "target", shared: true, external: "TOKEN=current\n"},
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
			if test.worker == "target" {
				external = workerEnv
				link = filepath.Join("..", "worker", projects.EffectiveEnvFileName)
			}
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
			case "directory":
				require.NoError(t, os.Symlink(t.TempDir(), workerEnv))
			case "loop":
				require.NoError(t, os.Symlink(workerEnv, workerEnv))
			}
			s := &Service{deps: Dependencies{Projects: func(context.Context) ([]types.ProjectRecord, error) {
				return []types.ProjectRecord{{ID: "p1", Name: "web", Path: projectPath}, {ID: "p2", Name: "worker", Path: workerPath}}, nil
			}}}
			var reported []string
			report := func(_ context.Context, message string) { reported = append(reported, message) }

			// As the backup does: it records the project directory as configured and resolved, which the links name.
			manifest := backupManifest(types.Project{Path: projectPath}, plan{}, false, false)
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
		"nothing to restore":  nil,
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
		Volumes: composetypes.Volumes{
			"data":   {Name: "shop_data"},
			"cache":  {Name: "web_cache"},
			"shared": {Name: "shared", External: true},
			"nfs":    {Name: "web_nfs", DriverOpts: map[string]string{"type": "nfs"}},
		},
	}
	targets, err = restoreTargets(renamed, captured, []string{"data"})
	require.NoError(t, err)
	require.Equal(t, []types.ProjectBackupRestoreVolume{{Key: "data", Snapshot: "web_data", Name: "shop_data"}}, targets)

	gone := types.Project{
		ID:          "p1",
		Name:        "Web",
		ComposeName: "web",
		Path:        "/app/data/projects/web",
		Volumes: composetypes.Volumes{
			"cache":  {Name: "web_cache"},
			"shared": {Name: "shared", External: true},
			"nfs":    {Name: "web_nfs", DriverOpts: map[string]string{"type": "nfs"}},
		},
	}
	_, err = restoreTargets(gone, captured, []string{"data"})
	require.ErrorContains(t, err, "no longer has volume data")
	outside := types.Project{
		ID:          "p1",
		Name:        "Web",
		ComposeName: "web",
		Path:        "/app/data/projects/web",
		Volumes: composetypes.Volumes{
			"data":   {Name: "web_data", DriverOpts: map[string]string{"type": "nfs"}},
			"cache":  {Name: "web_cache"},
			"shared": {Name: "shared", External: true},
			"nfs":    {Name: "web_nfs", DriverOpts: map[string]string{"type": "nfs"}},
		},
	}
	_, err = restoreTargets(outside, captured, []string{"data"})
	require.ErrorContains(t, err, "keeps its data outside Docker")
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
		"unknown key":  {backupID: "b1", request: types.RestoreProjectBackupRequest{Volumes: []string{"unknown"}}, message: "does not include the data of volume unknown"},
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

func TestStartRestoreRefusesRestoresThatReachBeyondTheProject(t *testing.T) {
	db := newTestDB(t)
	require.NoError(t, db.Create(&ProjectBackup{
		BaseModel:       database.BaseModel{ID: "b1", CreatedAt: time.Now()},
		ProjectID:       "p1",
		Status:          types.VolumeBackupStatusSucceeded,
		LocalSnapshotID: "snap",
		Volumes:         []types.ProjectBackupVolume{{Key: "data", Name: "web_data", Included: true}},
	}).Error)
	sharedErr := common.Classify(common.ErrValidation, errors.New("project Web is the projects directory"))
	containers := []container.Summary{{Names: []string{"/web-app-1"}, State: container.StateRunning}}
	dockerService := newTestDockerService(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/v1.41/containers/json", r.URL.Path)
		w.Header().Set("Content-Type", "application/json")
		// Past the checks, the restore lists every running container to find the project's.
		if !strings.Contains(r.URL.Query().Get("filters"), "com.docker.compose.project=web") {
			w.WriteHeader(http.StatusInternalServerError)
			_, writeErr := w.Write([]byte(`{"message":"past the checks"}`))
			assert.NoError(t, writeErr)
			return
		}
		assert.NoError(t, json.MarshalWrite(w, containers))
	}))
	start := func(runsArcane bool, request types.RestoreProjectBackupRequest) error {
		service := NewService(Dependencies{
			DB:     db,
			Docker: dockerService,
			Project: func(context.Context, string, types.ProjectDetailsOptions) (types.Project, error) {
				return testProject(), nil
			},
			RunsArcane:    func(context.Context, string) (bool, error) { return runsArcane, nil },
			SoleDirectory: func(context.Context, types.Project) error { return sharedErr },
		})
		_, err := service.StartRestore(t.Context(), StartRestoreOptions{EnvironmentID: "0", ProjectID: "p1", BackupID: "b1", Request: request})
		return err
	}
	// Restoring the files of a project whose directory holds other projects would replace their files too.
	require.ErrorIs(t, start(false, types.RestoreProjectBackupRequest{}), sharedErr)
	// Its volumes alone can still be restored.
	require.ErrorContains(t, start(false, types.RestoreProjectBackupRequest{Files: new(false)}), "past the checks")
	// A project that runs Arcane would take Arcane down with it.
	err := start(true, types.RestoreProjectBackupRequest{Files: new(false)})
	require.ErrorIs(t, err, common.ErrValidation)
	require.ErrorContains(t, err, "runs Arcane itself")
	// The restore stops only running containers, so it would write under a paused one.
	containers = append(containers, container.Summary{Names: []string{"/web-worker-1"}, State: container.StatePaused})
	err = start(false, types.RestoreProjectBackupRequest{Files: new(false)})
	require.ErrorIs(t, err, common.ErrConflict)
	require.ErrorContains(t, err, "container web-worker-1 of project Web is paused; unpause or stop it before restoring")
}

func (f *fakeRestore) step(name string) error {
	f.calls = append(f.calls, name)
	return f.fail[name]
}

func (f *fakeRestore) steps() restoreSteps {
	return restoreSteps{
		record: func(_ context.Context, p restorePlan) error {
			f.plans = append(f.plans, p)
			if p.RollingBack && p.Failure == "" {
				// The rollback saves itself first, then again before deploying.
				f.calls = append(f.calls, kit.Ternary(f.rolledBack, "save deploy", "rolling back"))
				f.rolledBack = true
			}
			return nil
		},
		report: func(context.Context, string) {},
		down:   func(context.Context) error { return f.step("down") },
		clear:  func(context.Context, restorePlan) error { return f.step("clear") },
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
		"down", "clear", "safety", "in use", "create shop_cache", "restore b1 /projects/web web_data>shop_data,web_cache>shop_cache",
		"rolling back", "restore s1 /projects/web shop_data>shop_data", "remove shop_cache", "save deploy", "deploy",
	}, running.calls)
	require.Equal(t, err.Error(), running.plans[len(running.plans)-1].Failure)

	stopped := &fakeRestore{fail: map[string]error{"verify": restoreErr}}
	err = stopped.steps().apply(t.Context(), plan(false))
	require.ErrorIs(t, err, restoreErr)
	require.NotContains(t, stopped.calls, "down")
	require.NotContains(t, stopped.calls, "deploy", "a project that did not run is not deployed")
	require.Contains(t, stopped.calls, "restore s1 /projects/web shop_data>shop_data")

	// A failed redeploy stops what it started before the safety backup is restored, then deploys again; the data was
	// rolled back, so only the deploy is reported as failed.
	redeploy := &fakeRestore{fail: map[string]error{"deploy": restoreErr}}
	err = redeploy.steps().apply(t.Context(), plan(true))
	require.ErrorContains(t, err, "rolled back to the safety backup s1, but could not be deployed")
	require.NotContains(t, err.Error(), "the rollback failed too")
	require.Equal(t, []string{
		"down", "clear", "safety", "in use", "create shop_cache", "restore b1 /projects/web web_data>shop_data,web_cache>shop_cache", "verify", "deploy",
		"rolling back", "down", "restore s1 /projects/web shop_data>shop_data", "remove shop_cache", "save deploy", "deploy",
	}, redeploy.calls)

	// A safety backup that cannot be restored leaves the project down instead of starting it on half-restored data.
	undoFailed := &fakeRestore{fail: map[string]error{"verify": restoreErr, "restore s1 /projects/web shop_data>shop_data": errors.New("disk full")}}
	err = undoFailed.steps().apply(t.Context(), plan(true))
	require.ErrorContains(t, err, "the rollback failed too")
	require.ErrorContains(t, err, "the project was left down; restore the safety backup s1")
	require.Equal(t, []string{
		"down", "clear", "safety", "in use", "create shop_cache", "restore b1 /projects/web web_data>shop_data,web_cache>shop_cache", "verify",
		"rolling back", "restore s1 /projects/web shop_data>shop_data", "remove shop_cache",
	}, undoFailed.calls)

	// A container that started using a volume after the checks stops the restore before anything is written.
	inUse := &fakeRestore{fail: map[string]error{"in use": common.Classify(common.ErrConflict, errors.New("volume shop_data is mounted"))}}
	err = inUse.steps().apply(t.Context(), plan(true))
	require.ErrorIs(t, err, common.ErrConflict)
	require.ErrorContains(t, err, "nothing was restored")
	// Nothing was written yet, so the volume a container now uses is left alone and the project is deployed again.
	require.Equal(t, []string{"down", "clear", "safety", "in use", "rolling back", "save deploy", "deploy"}, inUse.calls)

	safetyFailed := &fakeRestore{fail: map[string]error{"safety": restoreErr}}
	err = safetyFailed.steps().apply(t.Context(), plan(true))
	require.ErrorContains(t, err, "nothing was restored")
	require.Equal(t, []string{"down", "clear", "safety", "rolling back", "save deploy", "deploy"}, safetyFailed.calls)

	// A stopped container that was started again stops the restore before anything is written.
	started := &fakeRestore{fail: map[string]error{"clear": common.Classify(common.ErrConflict, errors.New("container web-app-1 was started during the restore"))}}
	err = started.steps().apply(t.Context(), plan(false))
	require.ErrorIs(t, err, common.ErrConflict)
	require.ErrorContains(t, err, "nothing was restored")
	require.Equal(t, []string{"clear", "rolling back"}, started.calls)

	// A redeploy that failed and cannot be stopped may still run on the restored data, so nothing is written under it.
	stuck := &fakeRestore{fail: map[string]error{"deploy": restoreErr, "down": errors.New("compose down failed")}}
	err = stuck.steps().apply(t.Context(), restorePlan{ID: "r1", BackupID: "b1", ProjectID: "p1", ProjectPath: "/p", Files: true, Running: true, Started: true, SafetyID: "s1", Restored: true})
	require.ErrorIs(t, err, restoreErr)
	require.ErrorContains(t, err, "so it was left as is; restore the safety backup s1 by hand")
	require.Equal(t, []string{"deploy", "rolling back", "down"}, stuck.calls)
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
	require.Equal(t, []string{"clear", "safety", "in use", "create shop_cache", "create shop_logs", "rolling back", "restore s1  ", "remove shop_cache"}, failed.calls)

	// Arcane stops between the two creates: the checkpoint saved after the first already names it, so the resumed run
	// removes it.
	index := slices.IndexFunc(failed.plans, func(recorded restorePlan) bool { return len(recorded.Created) > 0 })
	require.GreaterOrEqual(t, index, 0)
	require.Equal(t, []string{"shop_cache"}, failed.plans[index].Created)
	resumed := &fakeRestore{}
	err = resumed.steps().apply(t.Context(), failed.plans[index])
	require.ErrorContains(t, err, "the restore was interrupted")
	require.Contains(t, resumed.calls, "remove shop_cache")
	require.NotContains(t, resumed.calls, "create shop_logs")
}

func TestRestoreRedeploysOnlyWhenTheProjectRanOrIsAsked(t *testing.T) {
	for name, test := range map[string]struct {
		redeploy *bool
		running  bool
		deployed bool
	}{
		"running":            {running: true, deployed: true},
		"stopped":            {running: false, deployed: false},
		"running, kept down": {redeploy: new(false), running: true, deployed: false},
		"stopped, deployed":  {redeploy: new(true), running: false, deployed: true},
	} {
		restore := &fakeRestore{}
		require.NoError(t, restore.steps().apply(t.Context(), restorePlan{ID: "r1", BackupID: "b1", ProjectID: "p1", Files: true, ProjectPath: "/p", Redeploy: test.redeploy, Running: test.running}), name)
		require.Equal(t, test.deployed, slices.Contains(restore.calls, "deploy"), name)
		require.Equal(t, test.running, slices.Contains(restore.calls, "down"), name)
		require.NotContains(t, restore.calls, "restore s1 /p ", name)
	}
}

func TestResumedRestoreNeverLeavesTheProjectDown(t *testing.T) {
	// Interrupted after the safety backup: the restore may be half done, so it is rolled back and the project deployed.
	resumed := &fakeRestore{}
	err := resumed.steps().apply(t.Context(), restorePlan{ID: "r1", BackupID: "b1", ProjectID: "p1", ProjectPath: "/p", Files: true, Running: true, Started: true, SafetyID: "s1"})
	require.ErrorContains(t, err, "the restore was interrupted; the project was rolled back to the safety backup s1")
	require.Equal(t, []string{"rolling back", "restore s1 /p ", "remove ", "save deploy", "deploy"}, resumed.calls)

	// Interrupted before the safety backup: nothing changed, so the project is only deployed again.
	early := &fakeRestore{}
	err = early.steps().apply(t.Context(), restorePlan{ID: "r1", BackupID: "b1", ProjectID: "p1", Files: true, Running: true, Started: true})
	require.ErrorContains(t, err, "nothing was restored")
	require.Equal(t, []string{"rolling back", "save deploy", "deploy"}, early.calls)

	// Interrupted after the restore finished: only the redeploy is left.
	restored := &fakeRestore{}
	require.NoError(t, restored.steps().apply(t.Context(), restorePlan{ID: "r1", Running: true, Started: true, SafetyID: "s1", Restored: true}))
	require.Equal(t, []string{"deploy"}, restored.calls)

	// Interrupted while rolling back a failed redeploy: the rollback goes on instead of deploying half-rolled-back data.
	rolling := &fakeRestore{}
	err = rolling.steps().apply(t.Context(), restorePlan{
		ID: "r1", BackupID: "b1", ProjectID: "p1", ProjectPath: "/p", Files: true, Running: true, Started: true, SafetyID: "s1", Restored: true, Deploying: true, RollingBack: true,
	})
	require.ErrorContains(t, err, "the restore was interrupted; the project was rolled back to the safety backup s1")
	require.Equal(t, []string{"rolling back", "down", "restore s1 /p ", "remove ", "save deploy", "deploy"}, rolling.calls)
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

	rewrites, err := verify("web", applied)
	require.NoError(t, err)
	require.Empty(t, rewrites)

	// A pre-rename backup's files declare the old name; they are set back to the current one, as an import does.
	rewrites, err = verify("shop", applied)
	require.NoError(t, err)
	require.Equal(t, []string{"p1 web"}, rewrites)

	// A name the rewrite cannot change, such as one from the global .env, fails the restore so it is rolled back.
	_, err = verify("shop", func(string) (string, error) { return "shop", nil })
	require.ErrorIs(t, err, common.ErrConflict)
	require.ErrorContains(t, err, `name the Compose project "shop" instead of "web"`)

	rewriteErr := errors.New("top-level Compose name must be a plain scalar")
	_, err = verify("shop", func(string) (string, error) { return "shop", rewriteErr })
	require.ErrorIs(t, err, rewriteErr)
}

func TestMissingVolumesRefusesTargetsOutsideDocker(t *testing.T) {
	live := map[string]volume.Volume{
		"web_data":  {Name: "web_data", Driver: "local"},
		"web_cache": {Name: "web_cache", Driver: "rexray/ebs"},
		"shared":    {Name: "shared", Driver: "local", Options: map[string]string{"type": "none", "o": "bind", "device": "/mnt/nas/shared"}},
	}
	dockerService := newTestDockerService(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		found, ok := live[strings.TrimPrefix(r.URL.Path, "/v1.41/volumes/")]
		if !ok || r.Method != http.MethodGet {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		assert.NoError(t, json.MarshalWrite(w, found))
	}))
	service := NewService(Dependencies{Docker: dockerService})

	create, err := service.missingVolumes(t.Context(), testProject(), types.ProjectManifest{}, []types.ProjectBackupRestoreVolume{{Key: "data", Snapshot: "web_data", Name: "web_data"}})
	require.NoError(t, err)
	require.Empty(t, create)

	// A named volume on a storage plugin is backed up through its mount, so it restores the same way.
	create, err = service.missingVolumes(t.Context(), testProject(), types.ProjectManifest{}, []types.ProjectBackupRestoreVolume{{Key: "cache", Snapshot: "web_cache", Name: "web_cache"}})
	require.NoError(t, err)
	require.Empty(t, create)

	// The external volume was recreated as a bind mount after the backup, so a restore would write to the host path.
	_, err = service.missingVolumes(t.Context(), testProject(), types.ProjectManifest{}, []types.ProjectBackupRestoreVolume{{Key: "shared", Snapshot: "shared", Name: "shared"}})
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
	_, err := service.lockRestore(waiting, p, "")
	require.ErrorIs(t, err, context.DeadlineExceeded)
	unlock()

	release, err := service.lockRestore(t.Context(), p, "")
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

func TestContainersStopOnlyThisProject(t *testing.T) {
	labelsFor := func(project, service string) map[string]string {
		return map[string]string{api.ProjectLabel: project, api.ServiceLabel: service}
	}
	arcane := labelsFor("demo", "arcane")
	arcane["com.getarcaneapp.arcane"] = "true"
	var (
		mu      sync.Mutex
		stopped []string
	)
	states := map[string]container.ContainerState{
		"web-1": container.StateRunning, "db-1": container.StateRestarting, "cache-1": container.StatePaused,
		"arcane-1": container.StateRunning, "other-1": container.StateRunning,
	}
	dockerService := newTestDockerService(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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
				{ID: "helper-1", Labels: volumes.Labels(), State: container.StateRunning, Mounts: []container.MountPoint{{Type: mount.TypeVolume, Name: "browsed_data"}}},
			}
			// The project label filter is what keeps other projects out.
			if filter := r.URL.Query().Get("filters"); filter != "" {
				all = slices.DeleteFunc(all, func(c container.Summary) bool {
					return !strings.Contains(filter, `"`+api.ProjectLabel+"="+c.Labels[api.ProjectLabel]+`"`)
				})
			}
			w.Header().Set("Content-Type", "application/json")
			assert.NoError(t, json.MarshalWrite(w, all))
		case strings.HasSuffix(r.URL.Path, "/json"):
			// Each container is inspected for its stop grace, which bounds how long its stop may take.
			w.Header().Set("Content-Type", "application/json")
			assert.NoError(t, json.MarshalWrite(w, container.InspectResponse{
				ID:         path.Base(path.Dir(r.URL.Path)),
				Config:     &container.Config{StopTimeout: new(30)},
				HostConfig: &container.HostConfig{},
			}))
		case strings.HasSuffix(r.URL.Path, "/stop"):
			id := path.Base(path.Dir(r.URL.Path))
			stopped = append(stopped, id)
			states[id] = container.StateExited
			w.WriteHeader(http.StatusNoContent)
		default:
			http.NotFound(w, r)
		}
	}))
	service := NewService(Dependencies{Docker: dockerService})

	active, inUse, err := service.containerUse(t.Context(), "demo")
	require.NoError(t, err)
	assert.Equal(t, []string{"web-1", "db-1"}, containerIDs(active))
	// Another project's container and Arcane itself keep running, so their volumes count as in use; an idle
	// file-browser helper writes nothing.
	assert.Equal(t, map[string]bool{"demo_config": true, "demo_data": true}, inUse)

	summaries, err := service.stopContainers(t.Context(), "demo")
	require.NoError(t, err)
	assert.Equal(t, []string{"web-1", "db-1"}, containerIDs(summaries))
	// The paused cache, Arcane itself, and the other project keep their state.
	assert.Equal(t, []string{"web-1", "db-1"}, stopped)
	assert.Equal(t, container.StatePaused, states["cache-1"])
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
		// Removing a stopped project's containers takes them down as stopping it would.
		{name: "a project with stopped containers needs the right to take it down", plan: restorePlan{Stopped: true}, want: []string{authz.PermProjectsDown}},
		{
			name: "a project with stopped containers asked to start", plan: restorePlan{Stopped: true, Redeploy: new(true)},
			want: []string{authz.PermProjectsDown, authz.PermProjectsDeploy},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			require.Equal(t, test.want, test.plan.extraPermissions())
		})
	}
}

func TestRestoreRemovesStoppedContainersAndRefusesStartedOnes(t *testing.T) {
	exited := container.Summary{ID: "c1", Names: []string{"/web-app-1"}, State: container.StateExited}
	for _, test := range []struct {
		wantErr     error
		name        string
		wantMessage string
		containers  []container.Summary
		wantRemoved []string
		stopped     bool
	}{
		{
			name:        "stopped containers are removed",
			containers:  []container.Summary{exited, {ID: "c2", Names: []string{"/web-db-1"}, State: container.StateCreated}},
			wantRemoved: []string{"c1", "c2"},
			stopped:     true,
		},
		// An agent the project runs is Arcane's, not the project's to remove or refuse on.
		{
			name:        "Arcane's own containers are left alone",
			containers:  []container.Summary{exited, {ID: "agent", Names: []string{"/web-agent-1"}, State: container.StateRunning, Labels: map[string]string{"com.getarcaneapp.arcane.agent": "true"}}},
			wantRemoved: []string{"c1"},
			stopped:     true,
		},
		{
			name:        "a container started again is refused",
			containers:  []container.Summary{{ID: "c1", Names: []string{"/web-app-1"}, State: container.StateRunning}},
			wantMessage: "container web-app-1 was started during the restore",
			stopped:     true,
		},
		// The request checked projects:down only when it found stopped containers.
		{
			name:        "stopped containers the request did not find are refused",
			containers:  []container.Summary{exited},
			wantMessage: "the project's containers changed after the restore was requested",
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			var removed []string
			dockerService := newTestDockerService(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch r.Method {
				case http.MethodDelete:
					removed = append(removed, path.Base(r.URL.Path))
					w.WriteHeader(http.StatusNoContent)
				default:
					assert.Equal(t, "1", r.URL.Query().Get("all"), "stopped containers are listed too")
					assert.Contains(t, r.URL.Query().Get("filters"), "com.docker.compose.project=web")
					w.Header().Set("Content-Type", "application/json")
					assert.NoError(t, json.MarshalWrite(w, test.containers))
				}
			}))
			service := NewService(Dependencies{Docker: dockerService})
			err := service.removeStoppedContainers(t.Context(), restorePlan{ComposeName: "web", Stopped: test.stopped})
			if test.wantMessage != "" {
				require.ErrorIs(t, err, cmp.Or(test.wantErr, common.ErrConflict))
				require.ErrorContains(t, err, test.wantMessage)
				require.Empty(t, removed)
				return
			}
			require.NoError(t, err)
			require.Equal(t, test.wantRemoved, removed)
		})
	}
}

func TestRestoreRemovesItsOwnLeftoverRusticContainers(t *testing.T) {
	for _, test := range []struct {
		name      string
		removeErr bool
	}{
		{name: "a leftover of this restore is removed"},
		{name: "a leftover that can't be removed stops the restore", removeErr: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			var removed []string
			dockerService := newTestDockerService(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch r.Method {
				case http.MethodDelete:
					removed = append(removed, path.Base(r.URL.Path))
					if test.removeErr {
						http.Error(w, `{"message":"device busy"}`, http.StatusInternalServerError)
						return
					}
					w.WriteHeader(http.StatusNoContent)
				default:
					// Only this restore's own containers are listed, never another operation's.
					assert.Contains(t, r.URL.Query().Get("filters"), rustic.OwnerLabel+"="+restoreOwner+"r1")
					assert.Equal(t, "1", r.URL.Query().Get("all"), "an exited leftover still holds its volumes")
					w.Header().Set("Content-Type", "application/json")
					assert.NoError(t, json.MarshalWrite(w, []container.Summary{{ID: "stale"}}))
				}
			}))
			dockerClient, err := dockerService.GetClient(t.Context())
			require.NoError(t, err)
			err = removeLeftovers(t.Context(), dockerClient, restoreOwner+"r1")
			require.Equal(t, []string{"stale"}, removed)
			if test.removeErr {
				require.ErrorIs(t, err, common.ErrConflict)
				require.ErrorContains(t, err, "left over from an earlier run of this restore (stale)")
				return
			}
			require.NoError(t, err)
		})
	}
}

func TestRestoreRefusesContainersThatKeepDataInUnnamedVolumes(t *testing.T) {
	generated := strings.Repeat("ab", 32)
	unnamed := container.MountPoint{Type: mount.TypeVolume, Name: generated, Destination: "/data"}
	named := container.MountPoint{Type: mount.TypeVolume, Name: "web_data", Destination: "/data"}
	project := testProject()
	// Taking a running project down removes its containers too, so their unnamed volumes are refused as well.
	err := refuseUnnamedVolumes(project, []container.Summary{{Names: []string{"/web-app-1"}, State: container.StateRunning, Mounts: []container.MountPoint{named, unnamed}}})
	require.ErrorIs(t, err, common.ErrValidation)
	require.ErrorContains(t, err, "container web-app-1 keeps data at /data in an unnamed volume")
	require.NoError(t, refuseUnnamedVolumes(project, []container.Summary{{Names: []string{"/web-app-1"}, State: container.StateExited, Mounts: []container.MountPoint{named}}}))
	// A generated name the project adopts as an external volume is attached again by Compose.
	project.Volumes["adopted"] = composetypes.VolumeConfig{Name: generated, External: true}
	require.NoError(t, refuseUnnamedVolumes(project, []container.Summary{{Names: []string{"/web-app-1"}, State: container.StateExited, Mounts: []container.MountPoint{unnamed}}}))
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
