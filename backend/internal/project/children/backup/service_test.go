package backup

import (
	"archive/tar"
	"cmp"
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
	composeapi "github.com/docker/compose/v5/pkg/api"
	"github.com/italypaleale/francis/actor"
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
	"kit.getarcane.app/sys/crypto"

	"getarcane.dev/arcane/v3/internal/activity"
	"getarcane.dev/arcane/v3/internal/backup"
	"getarcane.dev/arcane/v3/internal/common"
	"getarcane.dev/arcane/v3/internal/config"
	"getarcane.dev/arcane/v3/internal/database"
	"getarcane.dev/arcane/v3/internal/docker"
	"getarcane.dev/arcane/v3/internal/s3"
	"getarcane.dev/arcane/v3/internal/scheduler/flow"
	"getarcane.dev/arcane/v3/internal/scheduler/flow/flowtest"
	"getarcane.dev/arcane/v3/internal/scheduler/jobcontext"
	"getarcane.dev/arcane/v3/internal/scheduler/runs"
	"getarcane.dev/arcane/v3/internal/settings"
	"getarcane.dev/arcane/v3/pkg/authz"
	"getarcane.dev/arcane/v3/pkg/rustic"
	"getarcane.dev/arcane/v3/pkg/volumes"
	"getarcane.dev/types/v3"
)

type (
	// fakeRestore records the steps an in-place restore applies and fails the ones it names.
	fakeRestore struct {
		calls      []string
		fail       map[string]error
		plans      []restorePlan
		rolledBack bool
	}

	// fakeScheduler records the jobs the policies register.
	fakeScheduler struct {
		jobs map[string]types.SchedulerJob
		mu   sync.Mutex
	}

	// fakeForget records each ForgetSnapshots call by repository and fails the repositories it names.
	fakeForget struct {
		calls   map[string][][]string
		failing map[string]bool
		mu      sync.Mutex
	}

	policyFixture struct {
		db       *database.DB
		engine   *backup.Engine
		service  *Service
		jobs     *fakeScheduler
		forgets  *fakeForget
		projects *[]types.ProjectReference
		// started lists the IDs of the containers resumed backups started again.
		started    func() []string
		s3Requests *atomic.Int64
		available  *atomic.Bool
		// onS3 runs once on the next S3 request, to change things while a check is in flight.
		onS3        *atomic.Pointer[func()]
		destination string
		secondary   string
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

// volumeServer answers volume inspects for the existing volumes and 404 for the rest.
func volumeServer(t *testing.T, existing ...string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		name, found := strings.CutPrefix(r.URL.Path, "/v1.41/volumes/")
		if !found || r.Method != http.MethodGet || !slices.Contains(existing, name) {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		assert.NoError(t, json.MarshalWrite(w, volume.Volume{Name: name}))
	})
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
	dockerService := newTestDockerService(t, volumeServer(t, "web_data", "web_nfs", "shared"))
	service := NewService(Dependencies{Docker: dockerService})

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

func TestCheckPlanVolumesRejectsChangedVolumes(t *testing.T) {
	dockerService := newTestDockerService(t, volumeServer(t, "web_data", "web_nfs", "shared"))
	service := NewService(Dependencies{Docker: dockerService})
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
	dockerService := newTestDockerService(t, volumeServer(t, "web_data"))
	service := NewService(Dependencies{
		DB:     db,
		Docker: dockerService,
		Engine: engine,
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
	service := NewService(Dependencies{DB: db, Engine: engine, VolumeLocks: locks})
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

func TestRestoreRecordsEachCreatedVolumeBeforeTheNext(t *testing.T) {
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
		DB:       db,
		Docker:   dockerService,
		Settings: settingsService,
		Project: func(context.Context, string, types.ProjectDetailsOptions) (types.Project, error) {
			return testProject(), nil
		},
		ComposeFiles: func(context.Context, string) ([]string, error) { return []string{"compose.yaml"}, nil },
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

func (f *fakeForget) forget(_ context.Context, _ *client.Client, repository backup.Repository, _ string, snapshotIDs []string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls[repository.ID] = append(f.calls[repository.ID], slices.Sorted(slices.Values(snapshotIDs)))
	if f.failing[repository.ID] {
		return errors.New("forget failed")
	}
	return nil
}

func (f *fakeScheduler) HasJob(name string) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	_, ok := f.jobs[name]
	return ok
}

// newPolicyFixture backs the service with real admissions and S3 destinations whose repository is missing until the
// fixture marks the storage available; ForgetSnapshots is recorded instead of run.
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

	requests, available, onS3 := &atomic.Int64{}, &atomic.Bool{}, &atomic.Pointer[func()]{}
	storage := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requests.Add(1)
		if hook := onS3.Swap(nil); hook != nil {
			(*hook)()
		}
		if available.Load() {
			w.Header().Set("Content-Range", "bytes 0-0/1")
			w.WriteHeader(http.StatusPartialContent)
			_, _ = w.Write([]byte("x"))
			return
		}
		w.Header().Set("Content-Type", "application/xml")
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`<?xml version="1.0" encoding="UTF-8"?><Error><Code>NoSuchKey</Code><Message>missing</Message></Error>`))
	}))
	t.Cleanup(storage.Close)
	crypto.InitEncryption(crypto.Config{EncryptionKey: "test-encryption-key-for-project-backups-32b", Environment: "test"})
	secret, err := crypto.Encrypt("secret-key")
	require.NoError(t, err)
	destinations := make([]string, 0, 2)
	for _, name := range []string{"Offsite", "Secondary"} {
		destination := &s3.S3Destination{
			Name: name, Endpoint: strings.TrimPrefix(storage.URL, "http://"), Bucket: "backups", Region: "us-east-1",
			AccessKeyID: "access-key", SecretAccessKey: secret, ForcePathStyle: true,
		}
		require.NoError(t, db.Create(destination).Error)
		destinations = append(destinations, destination.ID)
	}

	jobs := &fakeScheduler{jobs: map[string]types.SchedulerJob{}}
	projects := &[]types.ProjectReference{{ID: "p1", Name: "Web"}, {ID: "p2", Name: "Shop"}, {ID: "p3", Name: "Wiki"}}
	var (
		startedMu  sync.Mutex
		startedIDs []string
	)
	forgets := &fakeForget{calls: map[string][][]string{}, failing: map[string]bool{}}
	volumeHandler := volumeServer(t, "web_data", "web_nfs", "shared", "arcane-backups")
	// A new snapshot fails once it starts, when it lists the running containers that use its volumes; restarting stopped
	// containers lists all of the project's containers and starts them, which is recorded.
	dockerService := newTestDockerService(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/start"):
			startedMu.Lock()
			startedIDs = append(startedIDs, path.Base(path.Dir(r.URL.Path)))
			startedMu.Unlock()
			w.WriteHeader(http.StatusNoContent)
		case strings.HasSuffix(r.URL.Path, "/containers/json") && r.URL.Query().Get("all") != "1":
			http.Error(w, `{"message":"docker unavailable"}`, http.StatusInternalServerError)
		case strings.HasSuffix(r.URL.Path, "/containers/json"):
			w.Header().Set("Content-Type", "application/json")
			assert.NoError(t, json.MarshalWrite(w, []container.Summary{}))
		default:
			volumeHandler.ServeHTTP(w, r)
		}
	}))
	started := func() []string {
		startedMu.Lock()
		defer startedMu.Unlock()
		return slices.Clone(startedIDs)
	}
	destinationService := s3.NewS3DestinationService(db, func(context.Context, string) error { return nil })
	service := NewService(Dependencies{
		DB:     db,
		Docker: dockerService,
		Engine: engine,
		Repository: backup.NewVolumeRepository(backup.VolumeRepositoryDependencies{
			Docker: dockerService, Engine: engine, Settings: settingsService, S3Destinations: destinationService,
			RecoveryKeys: backup.NewRecoveryKeyStore(db), BackupVolumeName: "arcane-backups",
		}),
		ForgetSnapshots: forgets.forget,
		S3Destinations:  destinationService,
		Settings:        settingsService,
		VolumeLocks:     &concurrency.KeyedMutex{},
		Project: func(_ context.Context, projectID string, _ types.ProjectDetailsOptions) (types.Project, error) {
			project := testProject()
			return types.Project{ID: projectID, Name: project.Name, ComposeName: project.ComposeName, Path: project.Path, Volumes: project.Volumes}, nil
		},
		// Without a Compose model the stopped containers are started one by one, which the Docker fake records.
		ComposeProject: func(context.Context, string) (*composetypes.Project, error) {
			return nil, errors.New("no compose files")
		},
		ProjectName:  func(context.Context, string) (string, error) { return "Web", nil },
		ListProjects: func(context.Context) ([]types.ProjectReference, error) { return slices.Clone(*projects), nil },
		LockProject:  func(context.Context, string, string) (func(), error) { return func() {}, nil },
		Config:       &config.Config{},
		Ctx:          t.Context(),
		Scheduler:    jobs,
		Admission:    gate,
	})
	require.NoError(t, service.RegisterWorkflows(harness.Engine))
	harness.Start(t)
	return policyFixture{
		db: db, engine: engine, service: service, jobs: jobs, forgets: forgets, projects: projects, started: started,
		s3Requests: requests, available: available, onS3: onS3, destination: destinations[0], secondary: destinations[1],
	}
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

// admitWhileHeld starts admitScheduled while the lease is held, applies change once it waits, then releases the lease.
func (f policyFixture) admitWhileHeld(t *testing.T, held *runs.Lease, policyID string, change map[string]any) (admittedRun, error) {
	t.Helper()
	type result struct {
		err      error
		admitted admittedRun
	}
	done := make(chan result, 1)
	go func() {
		admitted, err := f.service.admitScheduled(t.Context(), "p1", policyID, func(ctx context.Context) (types.CreateProjectBackupRequest, bool, error) {
			return f.service.scheduledPolicy(ctx, policyID)
		})
		done <- result{admitted: admitted, err: err}
	}()
	select {
	case <-done:
		t.Fatal("the scheduled backup ran while another run held its admission")
	case <-time.After(300 * time.Millisecond):
	}
	require.NoError(t, f.db.Model(&ProjectBackupPolicy{}).Where("id = ?", policyID).Updates(change).Error)
	held.Release(t.Context())
	select {
	case got := <-done:
		return got.admitted, got.err
	case <-time.After(10 * time.Second):
		t.Fatal("the scheduled backup was not admitted after the other run finished")
		return admittedRun{}, nil
	}
}

func TestUpdatePoliciesValidatesAndKeepsTheVolumeSelection(t *testing.T) {
	f := newPolicyFixture(t)
	base := types.UpdateBackupPolicy{Enabled: true, Schedule: "0 2 * * *", RetentionCount: 3, LocalEnabled: true}

	collection, err := f.service.UpdatePolicies(t.Context(), "p1", []types.UpdateProjectBackupPolicy{
		{UpdateBackupPolicy: base},
		{UpdateBackupPolicy: base, Volumes: []string{}},
		{UpdateBackupPolicy: base, Volumes: []string{"data", "data"}},
	})
	require.NoError(t, err)
	require.Len(t, collection.Policies, 3)
	// No selection follows the project's volumes; an empty one backs up only the files.
	require.Nil(t, collection.Policies[0].Volumes)
	require.NotNil(t, collection.Policies[1].Volumes)
	require.Empty(t, collection.Policies[1].Volumes)
	require.Equal(t, []string{"data"}, collection.Policies[2].Volumes)
	encoded, err := json.Marshal(collection.Policies[1])
	require.NoError(t, err)
	require.Contains(t, string(encoded), `"volumes":[]`)
	encoded, err = json.Marshal(collection.Policies[0])
	require.NoError(t, err)
	require.NotContains(t, string(encoded), `"volumes"`)
	require.Len(t, f.jobs.jobs, 3)
	for _, policy := range collection.Policies {
		require.True(t, f.jobs.HasJob("project-backup:"+policy.ID))
	}

	for name, update := range map[string]types.UpdateProjectBackupPolicy{
		"select an S3 destination": {UpdateBackupPolicy: types.UpdateBackupPolicy{Schedule: "0 2 * * *", S3Enabled: true}},
		"schedule":                 {UpdateBackupPolicy: types.UpdateBackupPolicy{Schedule: "every night", LocalEnabled: true}},
		"has no volume missing":    {UpdateBackupPolicy: base, Volumes: []string{"missing"}},
		"keeps its data outside":   {UpdateBackupPolicy: base, Volumes: []string{"nfs"}},
	} {
		_, err = f.service.UpdatePolicies(t.Context(), "p1", []types.UpdateProjectBackupPolicy{update})
		require.ErrorIs(t, err, common.ErrValidation, name)
		require.ErrorContains(t, err, name)
	}
	_, err = f.service.UpdatePolicies(t.Context(), "p1", []types.UpdateProjectBackupPolicy{{UpdateBackupPolicy: types.UpdateBackupPolicy{Schedule: "every night", LocalEnabled: true}}})
	fieldErr, ok := errors.AsType[*types.FieldError](err)
	require.True(t, ok)
	require.Equal(t, "schedule", fieldErr.Field)

	// An absent stopContainers stops the containers, as a manual backup does; an explicit false is kept.
	require.True(t, collection.Policies[0].StopContainers)
	collection, err = f.service.UpdatePolicies(t.Context(), "p1", []types.UpdateProjectBackupPolicy{
		{UpdateBackupPolicy: base},
		{UpdateBackupPolicy: base, StopContainers: new(false)},
	})
	require.NoError(t, err)
	require.True(t, collection.Policies[0].StopContainers)
	require.False(t, collection.Policies[1].StopContainers)

	// Disabling one policy and leaving out the others unschedules all of them.
	disabled := base
	disabled.ID, disabled.Enabled = collection.Policies[0].ID, false
	collection, err = f.service.UpdatePolicies(t.Context(), "p1", []types.UpdateProjectBackupPolicy{{UpdateBackupPolicy: disabled}})
	require.NoError(t, err)
	require.Len(t, collection.Policies, 1)
	require.False(t, collection.Policies[0].Enabled)
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

func TestUpdatePoliciesChecksOnlyChangedVolumeSelections(t *testing.T) {
	f := newPolicyFixture(t)
	// web_cache was removed after the policy selected it.
	stale := f.policy(t, ProjectBackupPolicy{ProjectID: "p1", Schedule: "0 2 * * *", Enabled: true, LocalEnabled: true, Volumes: []string{"cache", "data"}})
	base := types.UpdateBackupPolicy{ID: stale.ID, Enabled: true, Schedule: "0 2 * * *", LocalEnabled: true}

	// Resending the stale selection, in any order, does not block saving it or adding another policy.
	collection, err := f.service.UpdatePolicies(t.Context(), "p1", []types.UpdateProjectBackupPolicy{
		{UpdateBackupPolicy: base, Volumes: []string{"data", "cache", "data"}},
		{UpdateBackupPolicy: types.UpdateBackupPolicy{Enabled: true, Schedule: "0 3 * * *", LocalEnabled: true}, Volumes: []string{"data"}},
	})
	require.NoError(t, err)
	require.Len(t, collection.Policies, 2)
	require.Equal(t, []string{"cache", "data"}, collection.Policies[0].Volumes)

	// A changed selection and a new policy are checked.
	for name, update := range map[string]types.UpdateProjectBackupPolicy{
		"project Web has no volume missing": {UpdateBackupPolicy: base, Volumes: []string{"data", "missing"}},
		"volume cache has not been created": {UpdateBackupPolicy: types.UpdateBackupPolicy{Enabled: true, Schedule: "0 3 * * *", LocalEnabled: true}, Volumes: []string{"cache"}},
	} {
		_, err = f.service.UpdatePolicies(t.Context(), "p1", []types.UpdateProjectBackupPolicy{update})
		require.ErrorIs(t, err, common.ErrValidation, name)
		require.ErrorContains(t, err, name)
	}
}

func TestScheduledBackupWaitsForTheAdmissionAndReadsThePolicyAgain(t *testing.T) {
	f := newPolicyFixture(t)
	policy := f.policy(t, ProjectBackupPolicy{ProjectID: "p1", Schedule: "0 2 * * *", Enabled: true, StopContainers: true, LocalEnabled: true})

	// The run waits instead of failing, then backs up with the policy as it stands once admitted.
	admitted, err := f.admitWhileHeld(t, f.hold(t, admissionScope, "p1"), policy.ID, map[string]any{"stop_containers": false, "volumes": `["data"]`})
	require.NoError(t, err)
	require.False(t, admitted.plan.StopContainers)
	require.Equal(t, []string{"web_data"}, admitted.plan.volumeNames())
	require.Len(t, admitted.leases, 2)
	_, busy, err := f.engine.TryAcquireRun(t.Context(), backup.VolumeAdmissionScope, "web_data")
	require.NoError(t, err)
	require.False(t, busy, "the run holds the lease of the volume it captures")
	release(t.Context(), admitted.leases)

	// A policy disabled while the run waits for a volume cancels it and releases what it took.
	_, err = f.admitWhileHeld(t, f.hold(t, backup.VolumeAdmissionScope, "web_data"), policy.ID, map[string]any{"enabled": false})
	require.ErrorIs(t, err, errPolicyGone)
	for _, key := range admissions("p1", []string{"web_data"}) {
		f.hold(t, key.Scope, key.ID).Release(t.Context())
	}
}

func TestScheduledRemoteCheckRunsOnThePolicyReadUnderTheAdmission(t *testing.T) {
	f := newPolicyFixture(t)
	// A retained volume backup snapshot means the repository project backups share existed, so its absence is not a
	// first backup.
	require.NoError(t, f.db.Exec(
		"INSERT INTO volume_backups (id, volume_name, size, created_at, status, s3_destination_id, remote_snapshot_id) VALUES (?, ?, ?, ?, ?, ?, ?)",
		"retained", "app-data", 0, time.Now(), types.VolumeBackupStatusSucceeded, f.destination, "remote",
	).Error)
	policy := f.policy(t, ProjectBackupPolicy{ProjectID: "p1", Schedule: "0 2 * * *", Enabled: true, LocalEnabled: true, Volumes: []string{}})

	// S3 was enabled while the run waited, so only the policy read under the admission checks it.
	admitted, err := f.admitWhileHeld(t, f.hold(t, admissionScope, "p1"), policy.ID, map[string]any{"s3_enabled": true, "s3_destination_id": f.destination})
	require.NoError(t, err)
	require.Positive(t, f.s3Requests.Load())
	require.True(t, admitted.remoteDisabled)
	require.False(t, admitted.plan.S3Enabled)
	require.True(t, admitted.plan.LocalEnabled)
	var stored ProjectBackupPolicy
	require.NoError(t, f.db.Where("id = ?", policy.ID).First(&stored).Error)
	require.False(t, stored.S3Enabled)
	require.True(t, stored.Enabled)
	require.True(t, f.jobs.HasJob("project-backup:"+policy.ID), "the policy is rescheduled with S3 disabled")
	release(t.Context(), admitted.leases)

	// With S3 as the only destination the policy is disabled and the run needs attention.
	_, err = f.admitWhileHeld(t, f.hold(t, admissionScope, "p1"), policy.ID, map[string]any{"local_enabled": false, "s3_enabled": true})
	require.ErrorIs(t, err, errRemoteOnlyDisabled)
	require.NoError(t, f.db.Where("id = ?", policy.ID).First(&stored).Error)
	require.False(t, stored.Enabled)
	require.False(t, f.jobs.HasJob("project-backup:"+policy.ID))
	f.hold(t, admissionScope, "p1").Release(t.Context())
}

func TestScheduledRemoteCheckUsesThePolicyChangedDuringIt(t *testing.T) {
	f := newPolicyFixture(t)
	require.NoError(t, f.db.Exec(
		"INSERT INTO volume_backups (id, volume_name, size, created_at, status, s3_destination_id, remote_snapshot_id) VALUES (?, ?, ?, ?, ?, ?, ?)",
		"retained", "app-data", 0, time.Now(), types.VolumeBackupStatusSucceeded, f.destination, "remote",
	).Error)
	policy := f.policy(t, ProjectBackupPolicy{
		ProjectID: "p1", Schedule: "0 2 * * *", Enabled: true, LocalEnabled: true, S3Enabled: true, S3DestinationID: f.destination,
	})
	// The user turns S3 off while the run finds its repository missing, so the guarded disable matches nothing.
	turnOff := func() {
		assert.NoError(t, f.db.Model(&ProjectBackupPolicy{}).Where("id = ?", policy.ID).Update("s3_enabled", false).Error)
	}
	f.onS3.Store(&turnOff)

	current, disabled, err := f.service.scheduledPolicy(t.Context(), policy.ID)
	require.NoError(t, err)
	require.False(t, disabled)
	require.Equal(t, types.VolumeBackupDestinationLocal, current.Destination, "the run uses the policy as the user left it, so it never writes to the missing repository")
}

// remoteBackup is a finished project backup whose only snapshot is in the fixture's missing S3 repository, so deleting
// it needs no backup repository.
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

	// A busy project keeps its backups until the policy's next backup; a restore as new holds it the same way, and
	// fails fast while the project is busy.
	releaseBackups, err := f.service.HoldBackups(t.Context(), "p1")
	require.NoError(t, err)
	_, err = f.service.HoldBackups(t.Context(), "p1")
	require.ErrorIs(t, err, errAlreadyRunning)
	require.ErrorIs(t, f.service.Delete(t.Context(), "p1", "s4"), errAlreadyRunning)
	f.service.retain(t.Context(), "p1", policy.ID)
	require.Len(t, f.backupIDs(t), 7)
	releaseBackups(t.Context())

	f.service.retain(t.Context(), "p1", policy.ID)
	require.Equal(t, []string{"manual", "other", "s1", "s2", "safety"}, f.backupIDs(t))
}

func TestDeletedPolicyBackupsBecomeUnmanaged(t *testing.T) {
	f := newPolicyFixture(t)
	base := types.UpdateBackupPolicy{Enabled: true, Schedule: "0 2 * * *", RetentionCount: 3, LocalEnabled: true}
	collection, err := f.service.UpdatePolicies(t.Context(), "p1", []types.UpdateProjectBackupPolicy{{UpdateBackupPolicy: base}, {UpdateBackupPolicy: base}})
	require.NoError(t, err)
	kept, deleted := collection.Policies[0], collection.Policies[1]
	f.remoteBackup(t, "kept", types.VolumeBackupTriggerScheduled, kept.ID, time.Hour)
	f.remoteBackup(t, "orphan", types.VolumeBackupTriggerScheduled, deleted.ID, time.Hour)
	f.remoteBackup(t, "manual", types.VolumeBackupTriggerManual, "", 72*time.Hour)
	keptUpdate := base
	keptUpdate.ID = kept.ID
	_, err = f.service.UpdatePolicies(t.Context(), "p1", []types.UpdateProjectBackupPolicy{{UpdateBackupPolicy: keptUpdate}})
	require.NoError(t, err)

	// The sweep marks the deleted policy's backups unmanaged, and only those.
	now := time.Now()
	expired, err := f.service.expiredUnmanaged(t.Context(), types.UnmanagedBackupSweep{Now: now, Retention: 24 * time.Hour})
	require.NoError(t, err)
	require.Empty(t, expired)
	var orphaned []string
	require.NoError(t, f.db.Model(&ProjectBackup{}).Where("orphaned_at IS NOT NULL").Pluck("id", &orphaned).Error)
	require.Equal(t, []string{"orphan"}, orphaned)

	// Once the unmanaged retention passes they are deleted; a busy project's wait for the next run.
	later := types.UnmanagedBackupSweep{Now: now.Add(48 * time.Hour), Retention: 24 * time.Hour}
	held := f.hold(t, admissionScope, "p1")
	candidates := map[string][]string{"p1": {"orphan"}}
	require.NoError(t, f.service.pruneUnmanaged(t.Context(), candidates, later))
	require.Equal(t, []string{"kept", "manual", "orphan"}, f.backupIDs(t))
	held.Release(t.Context())
	require.NoError(t, f.service.pruneUnmanaged(t.Context(), candidates, later))
	require.Equal(t, []string{"kept", "manual"}, f.backupIDs(t))
}

// globalPolicies saves the global policies and returns them as saved.
func (f policyFixture) globalPolicies(t *testing.T, updates ...types.UpdateGlobalProjectBackupPolicy) []types.GlobalProjectBackupPolicy {
	t.Helper()
	collection, err := f.service.UpdateGlobalPolicies(t.Context(), updates)
	require.NoError(t, err)
	return collection.Policies
}

// progress records the targets a run reports, as the scheduler does.
func progress(parent context.Context) (context.Context, func() []types.TargetOutcome) {
	var mu sync.Mutex
	var targets []types.TargetOutcome
	ctx := jobcontext.WithExecution(parent, types.SchedulerRun{}, func(target types.TargetOutcome) error {
		mu.Lock()
		defer mu.Unlock()
		targets = jobcontext.Merge(targets, target)
		return nil
	})
	return ctx, func() []types.TargetOutcome {
		mu.Lock()
		defer mu.Unlock()
		return slices.Clone(targets)
	}
}

func TestUpdateGlobalPoliciesValidatesAndSchedules(t *testing.T) {
	f := newPolicyFixture(t)
	base := types.UpdateBackupPolicy{Enabled: true, Schedule: "0 2 * * *", RetentionCount: 3, LocalEnabled: true}

	policies := f.globalPolicies(t,
		types.UpdateGlobalProjectBackupPolicy{UpdateBackupPolicy: base, SelectionMode: types.SystemVolumeSelectionAll, ProjectIDs: []string{"p1"}},
		types.UpdateGlobalProjectBackupPolicy{UpdateBackupPolicy: base, SelectionMode: types.SystemVolumeSelectionAllowlist, ProjectIDs: []string{}},
		types.UpdateGlobalProjectBackupPolicy{
			UpdateBackupPolicy: base, SelectionMode: types.SystemVolumeSelectionBlocklist, ProjectIDs: []string{"gone", " p2 ", "p2"}, StopContainers: new(false),
		},
	)
	require.Len(t, policies, 3)
	// All drops its list, an empty allowlist stays empty rather than selecting every project, and IDs of projects
	// that do not exist are kept.
	require.Empty(t, policies[0].ProjectIDs)
	require.Empty(t, policies[1].ProjectIDs)
	require.Empty(t, selectProjects(policies[1], *f.projects))
	require.Equal(t, []string{"gone", "p2"}, policies[2].ProjectIDs)
	require.Contains(t, f.service.deps.Settings.GetSettingsConfig().ProjectBackupConfig.Value, `"selectionMode":"allowlist","projectIds":[]`)
	// An absent stopContainers stops the containers, as a project's own policy does; an explicit false is kept.
	require.True(t, policies[0].StopContainers)
	require.False(t, policies[2].StopContainers)
	require.Len(t, f.jobs.jobs, 3)
	for _, policy := range policies {
		require.True(t, f.jobs.HasJob("project-backup:"+types.GlobalProjectPolicyPrefix+policy.ID))
	}

	all := types.SystemVolumeSelectionAll
	for name, update := range map[string]types.UpdateGlobalProjectBackupPolicy{
		"selectionMode must be":    {UpdateBackupPolicy: base, SelectionMode: "every"},
		"select an S3 destination": {UpdateBackupPolicy: types.UpdateBackupPolicy{Schedule: "0 2 * * *", S3Enabled: true}, SelectionMode: all},
		"schedule":                 {UpdateBackupPolicy: types.UpdateBackupPolicy{Schedule: "every night", LocalEnabled: true}, SelectionMode: all},
	} {
		_, err := f.service.UpdateGlobalPolicies(t.Context(), []types.UpdateGlobalProjectBackupPolicy{update})
		require.ErrorIs(t, err, common.ErrValidation, name)
		require.ErrorContains(t, err, name)
	}
	_, err := f.service.UpdateGlobalPolicies(t.Context(), []types.UpdateGlobalProjectBackupPolicy{
		{UpdateBackupPolicy: types.UpdateBackupPolicy{Schedule: "every night", LocalEnabled: true}, SelectionMode: all},
	})
	fieldErr, ok := errors.AsType[*types.FieldError](err)
	require.True(t, ok)
	require.Equal(t, "schedule", fieldErr.Field)

	// Disabling one policy and leaving out the others unschedules all of them.
	disabled := types.UpdateBackupPolicy{ID: policies[0].ID, Schedule: "0 2 * * *", RetentionCount: 3, LocalEnabled: true}
	policies = f.globalPolicies(t, types.UpdateGlobalProjectBackupPolicy{UpdateBackupPolicy: disabled, SelectionMode: all})
	require.Len(t, policies, 1)
	require.False(t, policies[0].Enabled)
	require.Empty(t, f.jobs.jobs)

	// Agents run no global policies.
	agentJobs := &fakeScheduler{jobs: map[string]types.SchedulerJob{}}
	agent := NewService(Dependencies{Config: &config.Config{AgentEnabled: true}, Ctx: t.Context(), Scheduler: agentJobs})
	agent.rescheduleGlobal(t.Context(), &types.GlobalProjectBackupPolicy{ID: "g1", Enabled: true, Schedule: "0 2 * * *"})
	require.Empty(t, agentJobs.jobs)
}

func TestGlobalPolicySelection(t *testing.T) {
	projects := []types.ProjectReference{{ID: "p1", Name: "Web"}, {ID: "p2", Name: "Shop"}, {ID: "p3", Name: "Wiki"}}
	listed := []string{"p2", "gone"}
	for name, tt := range map[string]struct {
		expected []string
		policy   types.GlobalProjectBackupPolicy
	}{
		"all includes every project": {
			policy: types.GlobalProjectBackupPolicy{SelectionMode: types.SystemVolumeSelectionAll}, expected: []string{"p1", "p2", "p3"},
		},
		"allowlist includes listed live projects only": {
			policy: types.GlobalProjectBackupPolicy{SelectionMode: types.SystemVolumeSelectionAllowlist, ProjectIDs: listed}, expected: []string{"p2"},
		},
		"blocklist includes every project not listed": {
			policy: types.GlobalProjectBackupPolicy{SelectionMode: types.SystemVolumeSelectionBlocklist, ProjectIDs: listed}, expected: []string{"p1", "p3"},
		},
		"an unknown mode selects nothing": {
			policy: types.GlobalProjectBackupPolicy{SelectionMode: "every"}, expected: []string{},
		},
		"an allowlist without projects selects nothing": {
			policy: types.GlobalProjectBackupPolicy{SelectionMode: types.SystemVolumeSelectionAllowlist}, expected: []string{},
		},
	} {
		t.Run(name, func(t *testing.T) {
			selected := make([]string, 0, len(projects))
			for _, project := range selectProjects(tt.policy, projects) {
				selected = append(selected, project.ID)
			}
			require.Equal(t, tt.expected, selected)
		})
	}
}

func TestGlobalBackupRereadsThePolicyUnderEachAdmission(t *testing.T) {
	f := newPolicyFixture(t)
	policy := f.globalPolicies(t, types.UpdateGlobalProjectBackupPolicy{
		UpdateBackupPolicy: types.UpdateBackupPolicy{Enabled: true, Schedule: "0 2 * * *", RetentionCount: 2, LocalEnabled: true},
		SelectionMode:      types.SystemVolumeSelectionAll,
		ProjectIDs:         []string{},
	})[0]
	input := globalRun{PolicyID: policy.ID}

	// Each project is backed up as a scheduled backup of the project with its default volumes.
	request, disabled, err := f.service.globalSettings(t.Context(), input, "p1")
	require.NoError(t, err)
	require.False(t, disabled)
	require.Equal(t, types.CreateProjectBackupRequest{StopContainers: new(true), Destination: types.VolumeBackupDestinationLocal}, request)
	// A project's own enabled policy replaces the global one; a disabled one does not.
	f.policy(t, ProjectBackupPolicy{ProjectID: "p2", Schedule: "0 3 * * *", Enabled: true, LocalEnabled: true})
	f.policy(t, ProjectBackupPolicy{ProjectID: "p3", Schedule: "0 3 * * *", LocalEnabled: true})
	_, _, err = f.service.globalSettings(t.Context(), input, "p2")
	require.ErrorIs(t, err, errProjectSkipped)
	_, _, err = f.service.globalSettings(t.Context(), input, "p3")
	require.NoError(t, err)
	// A deleted project is skipped.
	*f.projects = (*f.projects)[:2]
	_, _, err = f.service.globalSettings(t.Context(), input, "p3")
	require.ErrorIs(t, err, errProjectSkipped)

	// The run plans the selected projects before any backup, waits for each project's admission, and only then
	// checks its own policies: p1 gained one while the run waited.
	run := func(ctx context.Context) <-chan types.SchedulerOutcome {
		done := make(chan types.SchedulerOutcome, 1)
		go func() {
			outcome, runErr := f.service.backUpGlobal(ctx, input, types.SchedulerRun{})
			assert.NoError(t, runErr)
			done <- outcome
		}()
		return done
	}
	awaitOutcome := func(held *runs.Lease, change func(), done <-chan types.SchedulerOutcome) types.SchedulerOutcome {
		select {
		case <-done:
			t.Fatal("the global backup ran while another run held the project's admission")
		case <-time.After(300 * time.Millisecond):
		}
		change()
		held.Release(t.Context())
		select {
		case outcome := <-done:
			return outcome
		case <-time.After(10 * time.Second):
			t.Fatal("the global backup was not admitted after the other run finished")
			return types.SchedulerOutcome{}
		}
	}
	ctx, recorded := progress(t.Context())
	outcome := awaitOutcome(f.hold(t, admissionScope, "p1"), func() {
		f.policy(t, ProjectBackupPolicy{ProjectID: "p1", Schedule: "0 3 * * *", Enabled: true, LocalEnabled: true})
	}, run(ctx))
	require.Equal(t, types.RunStatusSucceeded, outcome.Status)
	require.Equal(t, "Backed up 0 projects; 2 skipped", outcome.Message)
	targets := recorded()
	require.Len(t, targets, 3)
	require.Equal(t, globalPlanTarget, targets[0].ID)
	require.JSONEq(t, `{"projectIds":["p1","p2"]}`, string(targets[0].RecoveryData))
	for _, target := range targets[1:] {
		require.Equal(t, types.RunStatusSkipped, target.Status, target.ID)
	}
	require.Empty(t, f.backupIDs(t))

	// A policy disabled while the run waits stops it.
	require.NoError(t, f.db.Where("project_id = ?", "p1").Delete(&ProjectBackupPolicy{}).Error)
	ctx, recorded = progress(t.Context())
	outcome = awaitOutcome(f.hold(t, admissionScope, "p1"), func() {
		disabledPolicy := types.UpdateBackupPolicy{ID: policy.ID, Schedule: "0 2 * * *", LocalEnabled: true}
		f.globalPolicies(t, types.UpdateGlobalProjectBackupPolicy{UpdateBackupPolicy: disabledPolicy, SelectionMode: types.SystemVolumeSelectionAll})
	}, run(ctx))
	require.Equal(t, types.RunStatusCanceled, outcome.Status)
	require.Len(t, recorded(), 1, "only the plan was recorded")
	require.Empty(t, f.backupIDs(t))
	for _, key := range admissions("p1", nil) {
		f.hold(t, key.Scope, key.ID).Release(t.Context())
	}
}

func TestGlobalRemoteCheckUsesThePolicyChangedDuringIt(t *testing.T) {
	f := newPolicyFixture(t)
	require.NoError(t, f.db.Exec(
		"INSERT INTO volume_backups (id, volume_name, size, created_at, status, s3_destination_id, remote_snapshot_id) VALUES (?, ?, ?, ?, ?, ?, ?)",
		"retained", "app-data", 0, time.Now(), types.VolumeBackupStatusSucceeded, f.destination, "remote",
	).Error)
	policy := f.globalPolicies(t, types.UpdateGlobalProjectBackupPolicy{
		UpdateBackupPolicy: types.UpdateBackupPolicy{
			Enabled: true, Schedule: "0 2 * * *", LocalEnabled: true, S3Enabled: true, S3DestinationID: f.destination,
		},
		SelectionMode: types.SystemVolumeSelectionAll,
	})[0]
	// The user turns S3 off while the run finds its repository missing, so the guarded disable matches nothing.
	turnOff := func() {
		stored, err := f.service.globalPolicies()
		assert.NoError(t, err)
		assert.NoError(t, f.service.saveGlobalPolicies(t.Context(), []types.GlobalProjectBackupPolicy{{
			ID: stored[0].ID, Schedule: stored[0].Schedule, SelectionMode: stored[0].SelectionMode, Enabled: true, LocalEnabled: true,
		}}))
	}
	f.onS3.Store(&turnOff)

	request, disabled, err := f.service.globalSettings(t.Context(), globalRun{PolicyID: policy.ID}, "p1")
	require.NoError(t, err)
	require.False(t, disabled)
	require.Equal(t, types.VolumeBackupDestinationLocal, request.Destination, "the run uses the policy as the user left it")
}

func TestGlobalBackupResumesWithoutLeavingContainersStopped(t *testing.T) {
	f := newPolicyFixture(t)
	policy := f.globalPolicies(t, types.UpdateGlobalProjectBackupPolicy{
		UpdateBackupPolicy: types.UpdateBackupPolicy{Enabled: true, Schedule: "0 2 * * *", LocalEnabled: true},
		SelectionMode:      types.SystemVolumeSelectionAll,
		ProjectIDs:         []string{},
	})[0]
	// p2's own policy took over after the interrupted run planned it.
	f.policy(t, ProjectBackupPolicy{ProjectID: "p2", Schedule: "0 3 * * *", Enabled: true, LocalEnabled: true})
	seriesID := types.GlobalProjectPolicyPrefix + policy.ID + ":p1"
	require.NoError(t, f.db.Create(&ProjectBackup{
		BaseModel: database.BaseModel{ID: "b1", CreatedAt: time.Now()}, ProjectID: "p1", PolicyID: seriesID,
		Trigger: types.VolumeBackupTriggerScheduled, Status: types.VolumeBackupStatusFailed, Error: "snapshot failed",
	}).Error)
	checkpointed := []container.Summary{{ID: "web-1", Names: []string{"/web-1"}, Labels: map[string]string{"com.docker.compose.service": "web"}}}
	checkpoint, err := json.Marshal(plan{
		BackupID: "b1", ProjectID: "p1", PolicyID: seriesID, StopContainers: true, Stopped: checkpointed,
		Volumes: []types.ProjectVolume{{Name: "web_data", Included: true}},
	})
	require.NoError(t, err)
	planned, err := json.Marshal(globalPlan{ProjectIDs: []string{"p1", "p2"}})
	require.NoError(t, err)
	previous := types.SchedulerRun{Outcome: types.SchedulerOutcome{Targets: []types.TargetOutcome{
		{ResourceType: "backup_plan", ID: globalPlanTarget, Status: types.RunStatusSucceeded, RecoveryData: planned},
		{ResourceType: targetType, ID: "p1", Status: types.RunStatusRunning, RecoveryData: checkpoint},
	}}}

	// While another run holds the project the run is handed back, and the containers stay as they are until it can
	// take the project.
	held := f.hold(t, backup.VolumeAdmissionScope, "web_data")
	_, err = f.service.backUpGlobal(t.Context(), globalRun{PolicyID: policy.ID}, previous)
	require.ErrorIs(t, err, actor.ErrJobRejected)
	require.Empty(t, f.started())

	// A fallback waits for the admission instead, restarts the containers, then backs up the rest of the plan.
	job, ok := f.jobs.jobs["project-backup:"+types.GlobalProjectPolicyPrefix+policy.ID].(*flow.Job)
	require.True(t, ok)
	done := make(chan types.SchedulerOutcome, 1)
	go func() {
		outcome, fallbackErr := job.FallbackFn(t.Context(), previous)
		assert.NoError(t, fallbackErr)
		done <- outcome
	}()
	select {
	case <-done:
		t.Fatal("the fallback resumed while another run held the volume")
	case <-time.After(300 * time.Millisecond):
	}
	held.Release(t.Context())
	var outcome types.SchedulerOutcome
	select {
	case outcome = <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("the fallback did not resume after the other run finished")
	}
	// The checkpointed containers are started again.
	require.Equal(t, []string{checkpointed[0].ID}, f.started())
	require.Equal(t, types.RunStatusPartial, outcome.Status)
	require.Equal(t, "1 project backups failed", outcome.Message)
	require.Len(t, outcome.Targets, 1)
	require.Equal(t, "p1", outcome.Targets[0].ID)
	require.Contains(t, outcome.Targets[0].Message, "snapshot failed")
	require.Equal(t, []string{"b1"}, f.backupIDs(t))
}

func TestGlobalPlanPassesStartupRecovery(t *testing.T) {
	f := newPolicyFixture(t)
	base := types.UpdateBackupPolicy{Enabled: true, Schedule: "0 2 * * *", LocalEnabled: true}
	policies := f.globalPolicies(t,
		types.UpdateGlobalProjectBackupPolicy{UpdateBackupPolicy: base, SelectionMode: types.SystemVolumeSelectionAllowlist, ProjectIDs: []string{"p2"}},
		types.UpdateGlobalProjectBackupPolicy{UpdateBackupPolicy: base, SelectionMode: types.SystemVolumeSelectionAllowlist, ProjectIDs: []string{}},
	)
	for index, expected := range [][]string{{"p2"}, {}} {
		ctx, recorded := progress(t.Context())
		planned, err := f.service.planGlobal(ctx, policies[index].ID, types.SchedulerRun{})
		require.NoError(t, err)
		require.ElementsMatch(t, expected, planned)
		targets := recorded()
		require.Len(t, targets, 1)
		// Startup decodes every checkpoint of an unfinished run into this struct and refuses to start on anything else.
		var checkpoint struct {
			BackupID string `json:"backupId"`
		}
		require.NoError(t, json.Unmarshal(targets[0].RecoveryData, &checkpoint))
		require.Empty(t, checkpoint.BackupID)
		// A resumed run keeps to the plan.
		resumed, err := f.service.planGlobal(t.Context(), policies[index].ID, types.SchedulerRun{Outcome: types.SchedulerOutcome{Targets: targets}})
		require.NoError(t, err)
		require.ElementsMatch(t, expected, resumed)
	}
}

func TestGlobalPolicyRunsOneAtATime(t *testing.T) {
	f := newPolicyFixture(t)
	policy := f.globalPolicies(t, types.UpdateGlobalProjectBackupPolicy{
		UpdateBackupPolicy: types.UpdateBackupPolicy{Enabled: true, Schedule: "0 2 * * *", LocalEnabled: true},
		SelectionMode:      types.SystemVolumeSelectionAllowlist,
		ProjectIDs:         []string{"p1"},
	})[0]
	// p1 has its own policy, so each run skips it once admitted; until then a run waits for its admission.
	f.policy(t, ProjectBackupPolicy{ProjectID: "p1", Schedule: "0 3 * * *", Enabled: true, LocalEnabled: true})
	held := f.hold(t, admissionScope, "p1")
	accepted, err := f.service.StartGlobal(t.Context(), types.Actor{}, policy.ID)
	require.NoError(t, err)

	// Run now fails fast while the policy runs.
	_, err = f.service.StartGlobal(t.Context(), types.Actor{}, policy.ID)
	require.ErrorIs(t, err, errGlobalRunning)
	manual := globalRun{Requester: &types.BackupRequester{}, PolicyID: policy.ID}
	_, err = f.service.backUpGlobal(t.Context(), manual, types.SchedulerRun{})
	require.ErrorIs(t, err, errGlobalRunning)
	require.NotErrorIs(t, err, actor.ErrJobRejected)
	// A resumed manual run may have left containers stopped, so it is handed back instead.
	resumed := types.SchedulerRun{Outcome: types.SchedulerOutcome{Targets: []types.TargetOutcome{
		{ResourceType: "backup_plan", ID: globalPlanTarget, Status: types.RunStatusSucceeded, RecoveryData: []byte(`{"projectIds":["p1"]}`)},
	}}}
	_, err = f.service.backUpGlobal(t.Context(), manual, resumed)
	require.ErrorIs(t, err, actor.ErrJobRejected)

	// A scheduled run waits for the running one before it plans anything.
	ctx, recorded := progress(t.Context())
	scheduled := make(chan types.SchedulerOutcome, 1)
	go func() {
		outcome, runErr := f.service.backUpGlobal(ctx, globalRun{PolicyID: policy.ID}, types.SchedulerRun{})
		assert.NoError(t, runErr)
		scheduled <- outcome
	}()
	select {
	case <-scheduled:
		t.Fatal("the scheduled run started while the policy was running")
	case <-time.After(300 * time.Millisecond):
	}
	require.Empty(t, recorded())
	held.Release(t.Context())
	outcome, err := f.service.flow.Wait(t.Context(), f.service.global, accepted.ActivityID)
	require.NoError(t, err)
	require.Equal(t, "Backed up 0 projects; 1 skipped", outcome.Message)
	select {
	case outcome = <-scheduled:
		require.Equal(t, "Backed up 0 projects; 1 skipped", outcome.Message)
	case <-time.After(10 * time.Second):
		t.Fatal("the scheduled run did not start after the running one finished")
	}
	// Every run released the policy's admission.
	f.hold(t, globalAdmissionScope, policy.ID).Release(t.Context())
}

func TestCheckpointAdmissionsSkipFinishedProjects(t *testing.T) {
	f := newPolicyFixture(t)
	checkpoint := func(projectID string) []byte {
		encoded, err := json.Marshal(plan{BackupID: projectID, ProjectID: projectID, Volumes: []types.ProjectVolume{{Name: projectID + "_data", Included: true}}})
		require.NoError(t, err)
		return encoded
	}
	previous := types.SchedulerRun{Outcome: types.SchedulerOutcome{Targets: []types.TargetOutcome{
		{ResourceType: targetType, ID: "p1", Status: types.RunStatusSucceeded, RecoveryData: checkpoint("p1")},
		{ResourceType: targetType, ID: "p2", Status: types.RunStatusRunning, RecoveryData: checkpoint("p2")},
	}}}
	// Another run holds the project the interrupted run already finished; recovery does not wait for it.
	held := f.hold(t, admissionScope, "p1")
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	require.NoError(t, f.service.awaitCheckpointAdmissions(ctx, previous))
	held.Release(t.Context())
	// It still waits for the project left running.
	held = f.hold(t, backup.VolumeAdmissionScope, "p2_data")
	ctx, cancel = context.WithTimeout(t.Context(), 300*time.Millisecond)
	defer cancel()
	require.ErrorIs(t, f.service.awaitCheckpointAdmissions(ctx, previous), context.DeadlineExceeded)
	held.Release(t.Context())
}

func TestGlobalRetentionKeepsTheNewestBackupsPerProject(t *testing.T) {
	f := newPolicyFixture(t)
	policy := f.globalPolicies(t, types.UpdateGlobalProjectBackupPolicy{
		UpdateBackupPolicy: types.UpdateBackupPolicy{Enabled: true, Schedule: "0 2 * * *", RetentionCount: 1, LocalEnabled: true},
		SelectionMode:      types.SystemVolumeSelectionAll,
	})[0]
	web, shop := types.GlobalProjectPolicyPrefix+policy.ID+":p1", types.GlobalProjectPolicyPrefix+policy.ID+":p2"
	require.Equal(t, types.BackupManagementTypeProject, types.BackupManagementTypeForPolicy(web))
	f.remoteBackup(t, "web-new", types.VolumeBackupTriggerScheduled, web, time.Hour)
	f.remoteBackup(t, "web-old", types.VolumeBackupTriggerScheduled, web, 2*time.Hour)
	f.remoteBackup(t, "shop-new", types.VolumeBackupTriggerScheduled, shop, time.Hour)
	f.remoteBackup(t, "shop-old", types.VolumeBackupTriggerScheduled, shop, 2*time.Hour)

	f.service.retain(t.Context(), "p1", web)
	require.Equal(t, []string{"shop-new", "shop-old", "web-new"}, f.backupIDs(t))
}

func TestGlobalSeriesStayManagedWhileTheirPolicySelectsTheProject(t *testing.T) {
	f := newPolicyFixture(t)
	all := types.SystemVolumeSelectionAll
	base := types.UpdateBackupPolicy{Enabled: true, Schedule: "0 2 * * *", LocalEnabled: true}
	policies := f.globalPolicies(t,
		types.UpdateGlobalProjectBackupPolicy{UpdateBackupPolicy: base, SelectionMode: types.SystemVolumeSelectionAllowlist, ProjectIDs: []string{"p1", "p2"}},
		types.UpdateGlobalProjectBackupPolicy{UpdateBackupPolicy: base, SelectionMode: all},
	)
	kept, removed := policies[0], policies[1]
	series := func(policy types.GlobalProjectBackupPolicy, projectID string) string {
		return types.GlobalProjectPolicyPrefix + policy.ID + ":" + projectID
	}
	f.remoteBackup(t, "web", types.VolumeBackupTriggerScheduled, series(kept, "p1"), time.Hour)
	f.remoteBackup(t, "shop", types.VolumeBackupTriggerScheduled, series(kept, "p2"), time.Hour)
	f.remoteBackup(t, "wiki", types.VolumeBackupTriggerScheduled, series(kept, "p3"), time.Hour)
	f.remoteBackup(t, "deleted-policy", types.VolumeBackupTriggerScheduled, series(removed, "p1"), time.Hour)
	keptUpdate := types.UpdateBackupPolicy{ID: kept.ID, Enabled: true, Schedule: "0 2 * * *", LocalEnabled: true}
	f.globalPolicies(t, types.UpdateGlobalProjectBackupPolicy{UpdateBackupPolicy: keptUpdate, SelectionMode: types.SystemVolumeSelectionAllowlist, ProjectIDs: []string{"p1", "p2"}})
	// p2 was deleted.
	*f.projects = []types.ProjectReference{{ID: "p1", Name: "Web"}, {ID: "p3", Name: "Wiki"}}

	orphaned := func() []string {
		t.Helper()
		_, err := f.service.expiredUnmanaged(t.Context(), types.UnmanagedBackupSweep{Now: time.Now(), Retention: 24 * time.Hour})
		require.NoError(t, err)
		var ids []string
		require.NoError(t, f.db.Model(&ProjectBackup{}).Where("orphaned_at IS NOT NULL").Order("id").Pluck("id", &ids).Error)
		return ids
	}
	// A series stays managed while its policy exists and selects its project.
	require.Equal(t, []string{"deleted-policy", "shop", "wiki"}, orphaned())
	// A project that is selected again manages its series again.
	*f.projects = append(*f.projects, types.ProjectReference{ID: "p2", Name: "Shop"})
	require.Equal(t, []string{"deleted-policy", "wiki"}, orphaned())
}

// storedBackup is a finished backup row of project p1 with the given snapshots.
func (f policyFixture) storedBackup(t *testing.T, entry ProjectBackup) {
	t.Helper()
	require.NoError(t, f.db.Create(&entry).Error)
}

func (f policyFixture) remoteID(destinationID string) string {
	return "volumes:s3:" + destinationID + ":" + f.service.deps.Settings.GetSettingsConfig().InstanceID.Value
}

func TestResumedRunThatCannotAdoptItsSnapshotFails(t *testing.T) {
	f := newPolicyFixture(t)
	policy := f.policy(t, ProjectBackupPolicy{ProjectID: "p1", Schedule: "0 2 * * *", Enabled: true, LocalEnabled: true, RetentionCount: 1})
	f.storedBackup(t, ProjectBackup{
		BaseModel: database.BaseModel{ID: "resumed", CreatedAt: time.Now()}, ProjectID: "p1", Status: types.VolumeBackupStatusRunning,
		Trigger: types.VolumeBackupTriggerScheduled, PolicyID: policy.ID, Destination: types.VolumeBackupDestinationS3, S3DestinationID: "gone",
	})
	checkpoint, err := json.Marshal(plan{BackupID: "resumed", ProjectID: "p1", PolicyID: policy.ID, S3DestinationID: "gone", S3Enabled: true})
	require.NoError(t, err)
	// The interrupted attempt was writing to S3, whose destination is gone now.
	previous := types.SchedulerRun{Outcome: types.SchedulerOutcome{Targets: []types.TargetOutcome{
		{ResourceType: targetType, ID: "p1", RecoveryData: checkpoint},
		{ResourceType: targetType, ID: "resumed:remote"},
	}}}

	_, resumed, err := f.service.resumeScheduled(t.Context(), previous)
	require.True(t, resumed)
	require.Error(t, err)
	var entry ProjectBackup
	require.NoError(t, f.db.First(&entry, "id = ?", "resumed").Error)
	require.Equal(t, types.VolumeBackupStatusFailed, entry.Status)
	require.NotEmpty(t, entry.Error)
}

func TestFailedResumedRunStillAppliesRetention(t *testing.T) {
	f := newPolicyFixture(t)
	policy := f.policy(t, ProjectBackupPolicy{ProjectID: "p1", Schedule: "0 2 * * *", Enabled: true, LocalEnabled: true, S3Enabled: true, S3DestinationID: f.destination, RetentionCount: 1})
	// Earlier runs saved their local snapshot, then failed to copy it to S3.
	for i, age := range []time.Duration{time.Hour, 2 * time.Hour, 3 * time.Hour} {
		f.storedBackup(t, ProjectBackup{
			BaseModel: database.BaseModel{ID: fmt.Sprintf("f%d", i+1), CreatedAt: time.Now().Add(-age)}, ProjectID: "p1",
			Status: types.VolumeBackupStatusFailed, Trigger: types.VolumeBackupTriggerScheduled, PolicyID: policy.ID,
			Destination: types.VolumeBackupDestinationLocalS3, S3DestinationID: f.destination, LocalSnapshotID: fmt.Sprintf("local-f%d", i+1),
		})
	}
	// The resumed run has its local snapshot, and the copy to S3 fails again.
	f.storedBackup(t, ProjectBackup{
		BaseModel: database.BaseModel{ID: "resumed", CreatedAt: time.Now()}, ProjectID: "p1", Status: types.VolumeBackupStatusRunning,
		Trigger: types.VolumeBackupTriggerScheduled, PolicyID: policy.ID, Destination: types.VolumeBackupDestinationLocalS3,
		S3DestinationID: f.destination, LocalSnapshotID: "local-resumed",
	})
	checkpoint, err := json.Marshal(plan{BackupID: "resumed", ProjectID: "p1", PolicyID: policy.ID, S3DestinationID: f.destination, LocalEnabled: true, S3Enabled: true})
	require.NoError(t, err)
	previous := types.SchedulerRun{Outcome: types.SchedulerOutcome{Targets: []types.TargetOutcome{{ResourceType: targetType, ID: "p1", RecoveryData: checkpoint}}}}

	_, resumed, err := f.service.resumeScheduled(t.Context(), previous)
	require.True(t, resumed)
	require.ErrorContains(t, err, "replicate")
	// The failure is reported as it was, and the older failed runs' local snapshots are gone.
	require.Equal(t, []string{"resumed"}, f.backupIDs(t))
	require.Equal(t, map[string][][]string{"volumes:local": {{"local-f1", "local-f2", "local-f3"}}}, f.forgets.calls)
}

func TestInterruptedScheduledBackupsKeepTheirJobs(t *testing.T) {
	f := newPolicyFixture(t)
	enabled := f.policy(t, ProjectBackupPolicy{ProjectID: "p1", Schedule: "0 2 * * *", Enabled: true, LocalEnabled: true})
	disabled := f.policy(t, ProjectBackupPolicy{ProjectID: "p1", Schedule: "0 3 * * *", Enabled: false, LocalEnabled: true})
	idle := f.policy(t, ProjectBackupPolicy{ProjectID: "p1", Schedule: "0 4 * * *", Enabled: false, LocalEnabled: true})
	for _, row := range []struct{ id, policy string }{{id: "b-disabled", policy: disabled.ID}, {id: "b-deleted", policy: "deleted-policy"}} {
		f.storedBackup(t, ProjectBackup{
			BaseModel: database.BaseModel{ID: row.id, CreatedAt: time.Now()}, ProjectID: "p1", Status: types.VolumeBackupStatusRunning,
			Trigger: types.VolumeBackupTriggerScheduled, PolicyID: row.policy, Destination: types.VolumeBackupDestinationLocal,
		})
	}

	// After a restart, a run interrupted with the project stopped can only resume through its policy's job.
	f.service.RegisterJobsOnStartup(t.Context())
	require.True(t, f.jobs.HasJob("project-backup:"+enabled.ID))
	require.True(t, f.jobs.HasJob("project-backup:"+disabled.ID))
	require.True(t, f.jobs.HasJob("project-backup:deleted-policy"))
	require.False(t, f.jobs.HasJob("project-backup:"+idle.ID))

	// Deleting a policy while its backup runs keeps the job until the backup ends.
	_, err := f.service.UpdatePolicies(t.Context(), "p1", []types.UpdateProjectBackupPolicy{{
		UpdateBackupPolicy: types.UpdateBackupPolicy{ID: idle.ID, Schedule: "0 4 * * *", LocalEnabled: true},
	}})
	require.NoError(t, err)
	require.True(t, f.jobs.HasJob("project-backup:"+disabled.ID))
	require.False(t, f.jobs.HasJob("project-backup:"+enabled.ID))
}

func TestInterruptedGlobalBackupsKeepTheirJobs(t *testing.T) {
	f := newPolicyFixture(t)
	all := types.SystemVolumeSelectionAll
	require.NoError(t, f.service.saveGlobalPolicies(t.Context(), []types.GlobalProjectBackupPolicy{
		{ID: "g-disabled", Schedule: "0 4 * * *", SelectionMode: all, LocalEnabled: true},
		{ID: "g-idle", Schedule: "0 5 * * *", SelectionMode: all, LocalEnabled: true},
	}))
	for _, row := range []struct{ id, series string }{
		{id: "b-disabled", series: types.GlobalProjectPolicyPrefix + "g-disabled:p1"},
		{id: "b-deleted", series: types.GlobalProjectPolicyPrefix + "g-deleted:p1"},
	} {
		f.storedBackup(t, ProjectBackup{
			BaseModel: database.BaseModel{ID: row.id, CreatedAt: time.Now()}, ProjectID: "p1", Status: types.VolumeBackupStatusRunning,
			Trigger: types.VolumeBackupTriggerScheduled, PolicyID: row.series, Destination: types.VolumeBackupDestinationLocal,
		})
	}

	// A global run interrupted with a project stopped resumes through its policy's job, even when that policy is off.
	f.service.RegisterJobsOnStartup(t.Context())
	require.True(t, f.jobs.HasJob("project-backup:"+types.GlobalProjectPolicyPrefix+"g-disabled"))
	require.True(t, f.jobs.HasJob("project-backup:"+types.GlobalProjectPolicyPrefix+"g-deleted"))
	require.False(t, f.jobs.HasJob("project-backup:"+types.GlobalProjectPolicyPrefix+"g-idle"))
	// Global series never get a per-project policy job.
	require.False(t, f.jobs.HasJob("project-backup:"+types.GlobalProjectPolicyPrefix+"g-deleted:p1"))
}

func TestPolicySavesForAProjectRunOneAtATime(t *testing.T) {
	f := newPolicyFixture(t)
	// Another save of p1 is committing and updating its jobs; this one waits, so the jobs follow the commit order.
	unlock := f.service.policyLocks.Lock("p1")
	done := make(chan error, 1)
	go func() {
		_, err := f.service.UpdatePolicies(t.Context(), "p1", nil)
		done <- err
	}()
	select {
	case err := <-done:
		t.Fatalf("a save ran while another save of the project held it: %v", err)
	case <-time.After(50 * time.Millisecond):
	}
	unlock()
	require.NoError(t, <-done)
}

func TestExpiredBackupsAreForgottenOncePerRepository(t *testing.T) {
	f := newPolicyFixture(t)
	f.available.Store(true)
	policy := f.policy(t, ProjectBackupPolicy{ProjectID: "p1", Schedule: "0 2 * * *", Enabled: true, LocalEnabled: true, RetentionCount: 1})
	rows := []struct{ id, local, remote, destination string }{
		{id: "keep", local: "local-keep"},
		{id: "la1", local: "local-la1", remote: "remote-la1", destination: f.destination},
		{id: "la2", local: "local-la2", remote: "remote-la2", destination: f.destination},
		{id: "ra1", remote: "remote-ra1", destination: f.destination},
		{id: "lb1", local: "local-lb1", remote: "remote-lb1", destination: f.secondary},
		{id: "rb1", remote: "remote-rb1", destination: f.secondary},
		{id: "l1", local: "local-l1"},
	}
	for i, row := range rows {
		f.storedBackup(t, ProjectBackup{
			BaseModel: database.BaseModel{ID: row.id, CreatedAt: time.Now().Add(-time.Duration(i+1) * time.Hour)}, ProjectID: "p1",
			Status: types.VolumeBackupStatusSucceeded, Trigger: types.VolumeBackupTriggerScheduled, PolicyID: policy.ID,
			Destination: types.VolumeBackupDestinationLocalS3, S3DestinationID: row.destination, LocalSnapshotID: row.local, RemoteSnapshotID: row.remote,
		})
	}
	primary, secondary := f.remoteID(f.destination), f.remoteID(f.secondary)

	// One repository failing keeps the rows whose snapshots it still holds; the rest are deleted.
	f.forgets.failing[secondary] = true
	f.service.retain(t.Context(), "p1", policy.ID)
	require.Equal(t, map[string][][]string{
		"volumes:local": {{"local-l1", "local-la1", "local-la2", "local-lb1"}},
		primary:         {{"remote-la1", "remote-la2", "remote-ra1"}},
		secondary:       {{"remote-lb1", "remote-rb1"}},
	}, f.forgets.calls)
	require.Equal(t, []string{"keep", "lb1", "rb1"}, f.backupIDs(t))
	var left ProjectBackup
	require.NoError(t, f.db.First(&left, "id = ?", "lb1").Error)
	require.Empty(t, left.LocalSnapshotID)
	require.Equal(t, "remote-lb1", left.RemoteSnapshotID)
	require.Equal(t, types.VolumeBackupDestinationS3, left.Destination)

	// Once it recovers, its leftover snapshots go in one call.
	f.forgets.failing[secondary] = false
	clear(f.forgets.calls)
	f.service.retain(t.Context(), "p1", policy.ID)
	require.Equal(t, map[string][][]string{secondary: {{"remote-lb1", "remote-rb1"}}}, f.forgets.calls)
	require.Equal(t, []string{"keep"}, f.backupIDs(t))
}

func TestUnmanagedSweepForgetsOncePerRepositoryAcrossProjects(t *testing.T) {
	f := newPolicyFixture(t)
	f.available.Store(true)
	for _, row := range []struct{ id, project string }{
		{id: "o1", project: "p1"}, {id: "o2", project: "p2"}, {id: "o3", project: "p2"},
	} {
		f.storedBackup(t, ProjectBackup{
			BaseModel: database.BaseModel{ID: row.id, CreatedAt: time.Now()}, ProjectID: row.project, Status: types.VolumeBackupStatusSucceeded,
			Trigger: types.VolumeBackupTriggerScheduled, PolicyID: "deleted-policy", Destination: types.VolumeBackupDestinationS3,
			S3DestinationID: f.destination, RemoteSnapshotID: "remote-" + row.id,
		})
	}
	candidates := map[string][]string{"p1": {"o1"}, "p2": {"o2", "o3"}}
	now := time.Now()
	require.NoError(t, f.service.pruneUnmanaged(t.Context(), candidates, types.UnmanagedBackupSweep{Now: now, Retention: 24 * time.Hour}))
	require.Empty(t, f.forgets.calls)

	// p2 is busy, so only p1's backup goes, and a later pass takes the rest together.
	held := f.hold(t, admissionScope, "p2")
	later := types.UnmanagedBackupSweep{Now: now.Add(48 * time.Hour), Retention: 24 * time.Hour}
	require.NoError(t, f.service.pruneUnmanaged(t.Context(), candidates, later))
	require.Equal(t, []string{"o2", "o3"}, f.backupIDs(t))
	held.Release(t.Context())
	clear(f.forgets.calls)
	require.NoError(t, f.service.pruneUnmanaged(t.Context(), candidates, later))
	require.Equal(t, map[string][][]string{f.remoteID(f.destination): {{"remote-o2", "remote-o3"}}}, f.forgets.calls)
	require.Empty(t, f.backupIDs(t))
}

func TestGlobalPolicySavesDoNotOverwriteEachOther(t *testing.T) {
	f := newPolicyFixture(t)
	// A retained snapshot means the missing repository existed, so the run disables S3.
	require.NoError(t, f.db.Exec(
		"INSERT INTO volume_backups (id, volume_name, size, created_at, status, s3_destination_id, remote_snapshot_id) VALUES (?, ?, ?, ?, ?, ?, ?)",
		"retained", "app-data", 0, time.Now(), types.VolumeBackupStatusSucceeded, f.destination, "remote",
	).Error)
	all := types.SystemVolumeSelectionAll
	base := types.UpdateBackupPolicy{Enabled: true, Schedule: "0 2 * * *", LocalEnabled: true}
	remote := types.UpdateBackupPolicy{Enabled: true, Schedule: "0 2 * * *", LocalEnabled: true, S3Enabled: true, S3DestinationID: f.destination}
	policies := f.globalPolicies(t,
		types.UpdateGlobalProjectBackupPolicy{UpdateBackupPolicy: remote, SelectionMode: all},
		types.UpdateGlobalProjectBackupPolicy{UpdateBackupPolicy: base, SelectionMode: all},
	)
	blocked := func(done <-chan struct{}, what string) {
		t.Helper()
		select {
		case <-done:
			t.Fatal(what + " saved while another save held the global policies")
		case <-time.After(300 * time.Millisecond):
		}
	}
	finished := func(done <-chan struct{}, what string) {
		t.Helper()
		select {
		case <-done:
		case <-time.After(10 * time.Second):
			t.Fatal(what + " did not save after the other save finished")
		}
	}

	// The run's disable waits for an edit in progress, then reads it and changes only S3 of its own policy.
	f.service.globalConfig.Lock()
	disabling := make(chan struct{})
	go func() {
		defer close(disabling)
		_, disabled, err := f.service.globalSettings(t.Context(), globalRun{PolicyID: policies[0].ID}, "p1")
		assert.NoError(t, err)
		assert.True(t, disabled)
	}()
	require.Eventually(t, func() bool { return f.s3Requests.Load() > 0 }, 10*time.Second, 10*time.Millisecond)
	blocked(disabling, "the S3 disable")
	require.NoError(t, f.service.saveGlobalPolicies(t.Context(), []types.GlobalProjectBackupPolicy{
		{ID: policies[0].ID, Schedule: "0 4 * * *", S3DestinationID: f.destination, SelectionMode: all, Enabled: true, LocalEnabled: true, S3Enabled: true},
		{ID: policies[1].ID, Schedule: "0 5 * * *", SelectionMode: all, Enabled: true, LocalEnabled: true},
	}))
	f.service.globalConfig.Unlock()
	finished(disabling, "the S3 disable")
	stored, err := f.service.globalPolicies()
	require.NoError(t, err)
	require.Len(t, stored, 2)
	require.Equal(t, "0 4 * * *", stored[0].Schedule)
	require.False(t, stored[0].S3Enabled)
	require.True(t, stored[0].Enabled)
	require.Equal(t, "0 5 * * *", stored[1].Schedule)

	// An edit waits for a run's disable in progress the same way.
	f.service.globalConfig.Lock()
	editing := make(chan struct{})
	go func() {
		defer close(editing)
		_, editErr := f.service.UpdateGlobalPolicies(t.Context(), []types.UpdateGlobalProjectBackupPolicy{{UpdateBackupPolicy: base, SelectionMode: all}})
		assert.NoError(t, editErr)
	}()
	blocked(editing, "the edit")
	f.service.globalConfig.Unlock()
	finished(editing, "the edit")
}

func TestGlobalRunAppliesRetentionAfterAFailedBackup(t *testing.T) {
	f := newPolicyFixture(t)
	policy := f.globalPolicies(t, types.UpdateGlobalProjectBackupPolicy{
		UpdateBackupPolicy: types.UpdateBackupPolicy{Enabled: true, Schedule: "0 2 * * *", RetentionCount: 1, LocalEnabled: true},
		SelectionMode:      types.SystemVolumeSelectionAllowlist,
		ProjectIDs:         []string{"p1"},
		StopContainers:     new(false),
	})[0]
	series := types.GlobalProjectPolicyPrefix + policy.ID + ":p1"
	// Earlier runs saved their local snapshot, then failed.
	for i, age := range []time.Duration{time.Hour, 2 * time.Hour} {
		f.storedBackup(t, ProjectBackup{
			BaseModel: database.BaseModel{ID: fmt.Sprintf("f%d", i+1), CreatedAt: time.Now().Add(-age)}, ProjectID: "p1",
			Status: types.VolumeBackupStatusFailed, Trigger: types.VolumeBackupTriggerScheduled, PolicyID: series,
			Destination: types.VolumeBackupDestinationLocal, LocalSnapshotID: fmt.Sprintf("local-f%d", i+1),
		})
	}

	ctx, _ := progress(t.Context())
	outcome, err := f.service.backUpGlobal(ctx, globalRun{PolicyID: policy.ID}, types.SchedulerRun{})
	require.NoError(t, err)
	// The failure is reported as it was, and the older failed run's local snapshot is gone.
	require.Equal(t, types.RunStatusPartial, outcome.Status)
	require.Len(t, outcome.Targets, 1)
	require.Contains(t, outcome.Targets[0].Message, "docker unavailable")
	require.Equal(t, map[string][][]string{"volumes:local": {{"local-f2"}}}, f.forgets.calls)
	ids := f.backupIDs(t)
	require.Len(t, ids, 2)
	require.Contains(t, ids, "f1")
	require.NotContains(t, ids, "f2")
}

func TestGlobalResumeAppliesRetentionAfterAFailedBackup(t *testing.T) {
	f := newPolicyFixture(t)
	policy := f.globalPolicies(t, types.UpdateGlobalProjectBackupPolicy{
		UpdateBackupPolicy: types.UpdateBackupPolicy{Enabled: true, Schedule: "0 2 * * *", RetentionCount: 1, LocalEnabled: true},
		SelectionMode:      types.SystemVolumeSelectionAll,
	})[0]
	series := types.GlobalProjectPolicyPrefix + policy.ID + ":p1"
	// Earlier runs saved their local snapshot, then failed to copy it to S3.
	for i, age := range []time.Duration{time.Hour, 2 * time.Hour, 3 * time.Hour} {
		f.storedBackup(t, ProjectBackup{
			BaseModel: database.BaseModel{ID: fmt.Sprintf("f%d", i+1), CreatedAt: time.Now().Add(-age)}, ProjectID: "p1",
			Status: types.VolumeBackupStatusFailed, Trigger: types.VolumeBackupTriggerScheduled, PolicyID: series,
			Destination: types.VolumeBackupDestinationLocalS3, S3DestinationID: f.destination, LocalSnapshotID: fmt.Sprintf("local-f%d", i+1),
		})
	}
	// The resumed run has its local snapshot, and the copy to S3 fails again.
	f.storedBackup(t, ProjectBackup{
		BaseModel: database.BaseModel{ID: "resumed", CreatedAt: time.Now()}, ProjectID: "p1", Status: types.VolumeBackupStatusRunning,
		Trigger: types.VolumeBackupTriggerScheduled, PolicyID: series, Destination: types.VolumeBackupDestinationLocalS3,
		S3DestinationID: f.destination, LocalSnapshotID: "local-resumed",
	})
	checkpoint, err := json.Marshal(plan{BackupID: "resumed", ProjectID: "p1", PolicyID: series, S3DestinationID: f.destination, LocalEnabled: true, S3Enabled: true})
	require.NoError(t, err)
	planned, err := json.Marshal(globalPlan{ProjectIDs: []string{"p1"}})
	require.NoError(t, err)
	previous := types.SchedulerRun{Outcome: types.SchedulerOutcome{Targets: []types.TargetOutcome{
		{ResourceType: "backup_plan", ID: globalPlanTarget, Status: types.RunStatusSucceeded, RecoveryData: planned},
		{ResourceType: targetType, ID: "p1", Status: types.RunStatusRunning, RecoveryData: checkpoint},
	}}}

	outcome, err := f.service.backUpGlobal(t.Context(), globalRun{PolicyID: policy.ID}, previous)
	require.NoError(t, err)
	// The failure is reported as it was, and the older failed runs' local snapshots are gone.
	require.Equal(t, types.RunStatusPartial, outcome.Status)
	require.Len(t, outcome.Targets, 1)
	require.Contains(t, outcome.Targets[0].Message, "replicate")
	require.Equal(t, []string{"resumed"}, f.backupIDs(t))
	require.Equal(t, map[string][][]string{"volumes:local": {{"local-f1", "local-f2", "local-f3"}}}, f.forgets.calls)
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
		VolumeLocks: &concurrency.KeyedMutex{},
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
	require.ErrorIs(t, service.resume(t.Context(), types.SchedulerRun{}, nil, recorded), actor.ErrJobRejected)
	fail.Store(false)
	require.NoError(t, service.resume(t.Context(), types.SchedulerRun{}, nil, recorded))
	require.Equal(t, []string{"web-1", "web-1"}, started())
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
		DB: db, Engine: engine, VolumeLocks: locks,
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
	service := NewService(Dependencies{VolumeLocks: locks})
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
		VolumeLocks: locks,
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
	err := service.resume(waiting, types.SchedulerRun{}, nil, recorded)
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
		VolumeLocks: &concurrency.KeyedMutex{},
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
	checkpoint, marshalErr := json.Marshal(recorded)
	require.NoError(t, marshalErr)
	previous := types.SchedulerRun{Outcome: types.SchedulerOutcome{Targets: []types.TargetOutcome{{ResourceType: targetType, ID: "p1", RecoveryData: checkpoint}}}}
	// After a restart a restore can take the project or a volume first; the resumed backup waits instead of starting
	// the project under it.
	for _, held := range admissions(recorded.ProjectID, recorded.volumeNames()) {
		lease, admitted, err := engine.TryAcquireRun(t.Context(), held.Scope, held.ID)
		require.NoError(t, err)
		require.True(t, admitted)

		err = service.resume(t.Context(), types.SchedulerRun{}, &types.BackupRequester{}, recorded)
		require.ErrorIs(t, err, actor.ErrJobRejected)
		// A resumed scheduled backup is handed back too, never replaced by a new one.
		_, resumed, err := service.resumeScheduled(t.Context(), previous)
		require.ErrorIs(t, err, actor.ErrJobRejected)
		require.True(t, resumed)
		require.Empty(t, started(), held)
		lease.Release(t.Context())
	}

	resumeErr := service.resume(t.Context(), types.SchedulerRun{}, nil, recorded)
	require.ErrorIs(t, resumeErr, backup.ErrBackupSettled)
	require.ErrorContains(t, resumeErr, "snapshot failed")
	// The checkpointed containers are started again.
	require.Equal(t, []string{"web-1"}, started())
	require.False(t, locked)
	// The resumed run releases the admissions it took.
	for _, key := range admissions(recorded.ProjectID, recorded.volumeNames()) {
		lease, admitted, err := engine.TryAcquireRun(t.Context(), key.Scope, key.ID)
		require.NoError(t, err)
		require.True(t, admitted, key)
		lease.Release(t.Context())
	}
}

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
					return !strings.Contains(filter, `"`+composeapi.ProjectLabel+"="+c.Labels[composeapi.ProjectLabel]+`"`)
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
	// Another project's container and Arcane itself keep running, so their volumes count as in use; an idle
	// file-browser helper writes nothing.
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
		// Only the container links an unnamed volume to its service, so removing it would lose that data.
		{
			name: "a stopped container with an unnamed volume is refused",
			containers: []container.Summary{{ID: "c1", Names: []string{"/web-app-1"}, State: container.StateExited, Mounts: []container.MountPoint{
				{Type: mount.TypeVolume, Name: strings.Repeat("ab", 32), Destination: "/data"},
			}}},
			wantErr:     common.ErrValidation,
			wantMessage: "container web-app-1 keeps data at /data in an unnamed volume",
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
	unnamed := container.MountPoint{Type: mount.TypeVolume, Name: strings.Repeat("ab", 32), Destination: "/data"}
	named := container.MountPoint{Type: mount.TypeVolume, Name: "web_data", Destination: "/data"}
	// Taking a running project down removes its containers too, so their unnamed volumes are refused as well.
	err := refuseUnnamedVolumes([]container.Summary{{Names: []string{"/web-app-1"}, State: container.StateRunning, Mounts: []container.MountPoint{named, unnamed}}})
	require.ErrorIs(t, err, common.ErrValidation)
	require.ErrorContains(t, err, "container web-app-1 keeps data at /data in an unnamed volume")
	require.NoError(t, refuseUnnamedVolumes([]container.Summary{{Names: []string{"/web-app-1"}, State: container.StateRunning, Mounts: []container.MountPoint{named}}}))
}
