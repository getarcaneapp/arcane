package backup

import (
	"context"
	"encoding/json/v2"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	composetypes "github.com/compose-spec/compose-go/v2/types"
	"github.com/docker/compose/v5/pkg/api"
	"github.com/italypaleale/francis/actor"
	"github.com/libtnb/sqlite"
	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/api/types/mount"
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
	"getarcane.dev/types/v3"
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

// startServer lists no containers and records each container start; while fail is set, starts fail.
func startServer(t *testing.T, fail *atomic.Bool) (*docker.DockerClientService, func() []string) {
	var (
		mu      sync.Mutex
		started []string
	)
	dockerService := newTestDockerService(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/containers/json"):
			w.Header().Set("Content-Type", "application/json")
			assert.NoError(t, json.MarshalWrite(w, []container.Summary{}))
		case strings.HasSuffix(r.URL.Path, "/start"):
			mu.Lock()
			started = append(started, path.Base(path.Dir(r.URL.Path)))
			mu.Unlock()
			w.WriteHeader(kit.Ternary(fail.Load(), http.StatusInternalServerError, http.StatusNoContent))
		default:
			http.NotFound(w, r)
		}
	}))
	return dockerService, func() []string {
		mu.Lock()
		defer mu.Unlock()
		return slices.Clone(started)
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

	manifest := backupManifest(project, p, true, []string{"compose.yaml"})
	require.Equal(t, types.ProjectManifestKindBackup, manifest.Kind)
	require.Equal(t, types.ProjectManifestFormatVersion, manifest.FormatVersion)
	require.Equal(t, types.ProjectManifestProject{ID: "p1", Name: "Web", ComposeName: "web"}, manifest.Project)
	require.Equal(t, p.Volumes, manifest.Volumes)
	require.False(t, manifest.Consistent, "a live backup of a running project is not consistent")
	require.True(t, backupManifest(project, p, false, []string{"compose.yaml"}).Consistent, "nothing runs, so a live backup is consistent")

	p.StopContainers = true
	require.True(t, backupManifest(project, p, true, []string{"compose.yaml"}).Consistent)

	// Other projects are not stopped, so a captured volume one of them mounts makes the copy inconsistent.
	p.Volumes[0].InUse = true
	require.False(t, backupManifest(project, p, true, []string{"compose.yaml"}).Consistent)
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
	for _, key := range (plan{ProjectID: "p1", Volumes: []types.ProjectVolume{{Name: "web_data", Included: true}}}).admissions() {
		lease, admitted, err := engine.TryAcquireRun(t.Context(), key.Scope, key.ID)
		require.NoError(t, err)
		require.True(t, admitted, key)
		lease.Release(t.Context())
	}
}

func TestResumeRetriesUntilTheStoppedContainersStart(t *testing.T) {
	db := newTestDB(t)
	harness := flowtest.New(t, activity.NewActivityService(db, nil))
	gate := runs.NewAdmission(harness.Runtime.Service(), t.Name())
	require.NoError(t, gate.Register(harness.Runtime))
	engine := backup.NewEngine(gate, nil, func(context.Context, types.BackupRequester) error { return nil })
	t.Cleanup(func() { require.NoError(t, engine.Stop(context.WithoutCancel(t.Context()))) })
	harness.Start(t)
	require.NoError(t, db.Create(&ProjectBackup{
		BaseModel: database.BaseModel{ID: "b1", CreatedAt: time.Now()}, ProjectID: "p1", Status: types.VolumeBackupStatusSucceeded,
	}).Error)
	var fail atomic.Bool
	fail.Store(true)
	dockerService, started := startServer(t, &fail)
	service := NewService(Dependencies{
		DB:          db,
		Docker:      dockerService,
		Engine:      engine,
		LockVolumes: lockingWith(&concurrency.KeyedMutex{}),
		LockProject: func(context.Context, string, string) (func(), error) { return func() {}, nil },
		ComposeProject: func(context.Context, string) (*composetypes.Project, error) {
			return &composetypes.Project{}, nil
		},
	})
	recorded := plan{
		BackupID: "b1", ProjectID: "p1", StopContainers: true,
		Stopped: []container.Summary{{ID: "web-1", Names: []string{"/web-1"}}},
	}

	// A failed start hands the run back instead of ending it with the project down.
	require.ErrorIs(t, service.resume(t.Context(), types.SchedulerRun{}, runInput{}, recorded), actor.ErrJobRejected)
	fail.Store(false)
	require.NoError(t, service.resume(t.Context(), types.SchedulerRun{}, runInput{}, recorded))
	require.Equal(t, []string{"web-1", "web-1"}, started())
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

func TestResumeRestartsOnlyOnceEveryProjectVolumeIsFree(t *testing.T) {
	db := newTestDB(t)
	harness := flowtest.New(t, activity.NewActivityService(db, nil))
	gate := runs.NewAdmission(harness.Runtime.Service(), t.Name())
	require.NoError(t, gate.Register(harness.Runtime))
	engine := backup.NewEngine(gate, nil, func(context.Context, types.BackupRequester) error { return nil })
	t.Cleanup(func() { require.NoError(t, engine.Stop(context.WithoutCancel(t.Context()))) })
	harness.Start(t)
	locks := &concurrency.KeyedMutex{}
	dockerService, started := startServer(t, &atomic.Bool{})
	service := NewService(Dependencies{
		DB:          db,
		Docker:      dockerService,
		Engine:      engine,
		LockVolumes: lockingWith(locks),
		LockProject: func(context.Context, string, string) (func(), error) { return func() {}, nil },
		ComposeProject: func(context.Context, string) (*composetypes.Project, error) {
			return &composetypes.Project{}, nil
		},
	})
	recorded := plan{
		BackupID: "b1", ProjectID: "p1", StopContainers: true,
		Volumes: []types.ProjectVolume{{Key: "data", Name: "web_data", Included: true}, {Key: "cache", Name: "web_cache"}},
		Stopped: []container.Summary{{ID: "web-1", Names: []string{"/web-1"}}},
	}

	// A restore of the cache volume, which this backup didn't capture, is still writing it; the containers wait.
	unlock := locks.Lock("web_cache")
	waiting, cancel := context.WithTimeout(t.Context(), 50*time.Millisecond)
	defer cancel()
	err := service.resume(waiting, types.SchedulerRun{}, runInput{}, recorded)
	require.ErrorIs(t, err, actor.ErrJobRejected)
	require.Empty(t, started())
	unlock()
}

func TestCompleteLeavesAHandedBackRunRunning(t *testing.T) {
	db := newTestDB(t)
	entry := &ProjectBackup{BaseModel: database.BaseModel{ID: "b1", CreatedAt: time.Now()}, ProjectID: "p1", Status: types.VolumeBackupStatusRunning}
	require.NoError(t, db.Create(entry).Error)
	service := NewService(Dependencies{DB: db})

	err := service.complete(t.Context(), entry, fmt.Errorf("%w: restart project containers: %w", actor.ErrJobRejected, errors.New("down")))
	require.ErrorIs(t, err, actor.ErrJobRejected)
	var stored ProjectBackup
	require.NoError(t, db.First(&stored, "id = ?", "b1").Error)
	require.Equal(t, types.VolumeBackupStatusRunning, stored.Status)
}

func TestResumeRestartsStoppedContainersOnlyOnceAdmitted(t *testing.T) {
	db := newTestDB(t)
	harness := flowtest.New(t, activity.NewActivityService(db, nil))
	gate := runs.NewAdmission(harness.Runtime.Service(), t.Name())
	require.NoError(t, gate.Register(harness.Runtime))
	engine := backup.NewEngine(gate, nil, func(context.Context, types.BackupRequester) error { return nil })
	t.Cleanup(func() { require.NoError(t, engine.Stop(context.WithoutCancel(t.Context()))) })
	harness.Start(t)
	entry := &ProjectBackup{
		BaseModel: database.BaseModel{ID: "b1", CreatedAt: time.Now()},
		ProjectID: "p1",
		Status:    types.VolumeBackupStatusFailed,
		Error:     "snapshot failed",
	}
	require.NoError(t, db.Create(entry).Error)
	locked := false
	dockerService, started := startServer(t, &atomic.Bool{})
	service := NewService(Dependencies{
		DB:          db,
		Docker:      dockerService,
		Engine:      engine,
		LockVolumes: lockingWith(&concurrency.KeyedMutex{}),
		LockProject: func(_ context.Context, _, projectID string) (func(), error) {
			assert.Equal(t, "p1", projectID)
			locked = true
			return func() { locked = false }, nil
		},
		ComposeProject: func(_ context.Context, projectID string) (*composetypes.Project, error) {
			assert.Equal(t, "p1", projectID)
			assert.True(t, locked, "the containers start under the project lock")
			return &composetypes.Project{}, nil
		},
	})

	checkpointed := []container.Summary{{ID: "web-1", Names: []string{"/web-1"}, Labels: map[string]string{"com.docker.compose.service": "web"}}}
	recorded := plan{
		BackupID:       "b1",
		ProjectID:      "p1",
		Volumes:        []types.ProjectVolume{{Name: "web_data", Included: true}},
		StopContainers: true,
		Stopped:        checkpointed,
	}
	// After a restart a restore can take the project or a volume first; the resumed backup waits instead of starting
	// the project under it.
	for _, held := range recorded.admissions() {
		lease, admitted, err := engine.TryAcquireRun(t.Context(), held.Scope, held.ID)
		require.NoError(t, err)
		require.True(t, admitted)

		err = service.resume(t.Context(), types.SchedulerRun{}, runInput{}, recorded)
		require.ErrorIs(t, err, actor.ErrJobRejected)
		require.Empty(t, started(), held)
		lease.Release(t.Context())
	}

	resumeErr := service.resume(t.Context(), types.SchedulerRun{}, runInput{}, recorded)
	require.ErrorIs(t, resumeErr, backup.ErrBackupSettled)
	require.ErrorContains(t, resumeErr, "snapshot failed")
	// The checkpointed containers are started again.
	require.Equal(t, []string{"web-1"}, started())
	require.False(t, locked)
	// The resumed run releases the admissions it took.
	for _, key := range recorded.admissions() {
		lease, admitted, err := engine.TryAcquireRun(t.Context(), key.Scope, key.ID)
		require.NoError(t, err)
		require.True(t, admitted, key)
		lease.Release(t.Context())
	}
}

func TestContainersStopAndStartOnlyThisProject(t *testing.T) {
	labelsFor := func(project, service string) map[string]string {
		return map[string]string{api.ProjectLabel: project, api.ServiceLabel: service}
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
			}
			// The project label filter is what keeps other projects out.
			if filter := r.URL.Query().Get("filters"); filter != "" {
				all = slices.DeleteFunc(all, func(c container.Summary) bool {
					return !strings.Contains(filter, api.ProjectLabel+"="+c.Labels[api.ProjectLabel])
				})
			}
			w.Header().Set("Content-Type", "application/json")
			assert.NoError(t, json.MarshalWrite(w, all))
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
	service := NewService(Dependencies{
		Docker: dockerService,
		ComposeProject: func(context.Context, string) (*composetypes.Project, error) {
			return nil, errors.New("compose.yaml: invalid")
		},
	})

	active, inUse, err := service.containerUse(t.Context(), "demo")
	require.NoError(t, err)
	assert.Equal(t, []string{"web-1", "db-1"}, containerIDs(active))
	// Another project's container and Arcane itself keep running, so their volumes count as in use.
	assert.Equal(t, map[string]bool{"demo_config": true, "demo_data": true}, inUse)

	summaries, err := service.stopContainers(t.Context(), "demo")
	require.NoError(t, err)
	// The paused cache, Arcane itself, and the other project keep their state.
	assert.Equal(t, []string{"web-1", "db-1"}, stopped)
	assert.Equal(t, container.StatePaused, states["cache-1"])

	// Compose files that no longer load still get the stopped containers started.
	require.NoError(t, service.startContainers(t.Context(), "p1", summaries))
	assert.Equal(t, []string{"web-1", "db-1"}, started)
}

func containerIDs(summaries []container.Summary) []string {
	ids := make([]string, 0, len(summaries))
	for _, c := range summaries {
		ids = append(ids, c.ID)
	}
	return ids
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
