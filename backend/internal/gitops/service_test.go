package gitops

import (
	"github.com/getarcaneapp/arcane/backend/v2/internal/environment"

	"github.com/getarcaneapp/arcane/backend/v2/internal/imageupdate"

	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"emperror.dev/errors"
	"github.com/getarcaneapp/arcane/backend/v2/internal/actors"
	"github.com/getarcaneapp/arcane/backend/v2/internal/common"
	"github.com/getarcaneapp/arcane/backend/v2/internal/config"
	"github.com/getarcaneapp/arcane/backend/v2/internal/database"
	"github.com/getarcaneapp/arcane/backend/v2/internal/event"
	"github.com/getarcaneapp/arcane/backend/v2/internal/gitrepo"
	projectpkg "github.com/getarcaneapp/arcane/backend/v2/internal/project"
	"github.com/getarcaneapp/arcane/backend/v2/internal/settings"
	git "github.com/getarcaneapp/arcane/backend/v2/pkg/gitutil"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/projects"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/scheduler/entityjobs"
	"github.com/getarcaneapp/arcane/types/v2/gitops"
	schedulertypes "github.com/getarcaneapp/arcane/types/v2/scheduler"
	swarmtypes "github.com/getarcaneapp/arcane/types/v2/swarm"
	"github.com/libtnb/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/fx/fxtest"
	"gorm.io/gorm"
)

func setupGitOpsProjectTestDBInternal(t *testing.T) *database.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&projectpkg.Project{}, &settings.SettingVariable{}, &imageupdate.ImageUpdateRecord{}, &event.Event{}))
	return &database.DB{DB: db}
}

func newGitOpsSettingsServiceForTestInternal(t testing.TB, ctx context.Context, db *database.DB) (*settings.SettingsService, error) {
	t.Helper()
	fxLifecycle := fxtest.NewLifecycle(t)
	runtime, err := actors.NewRuntime(t.Context(), fxLifecycle)
	require.NoError(t, err)
	executor, err := actors.NewExecutor(t.Context(), runtime, "gitops-settings-test", t.Name(), 3)
	require.NoError(t, err)
	effects, err := actors.NewExecutor(t.Context(), runtime, "gitops-settings-effects-test", t.Name(), 3)
	require.NoError(t, err)
	t.Cleanup(func() {
		stopCtx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		require.NoError(t, executor.Stop(stopCtx))
		require.NoError(t, effects.Stop(stopCtx))
		require.NoError(t, fxLifecycle.Stop(stopCtx))
	})
	return settings.NewSettingsService(ctx, db, executor, effects)
}

func newGitOpsAdmissionGateForTestInternal(t testing.TB) *actors.Gate[actors.AdmissionKey] {
	t.Helper()
	fxLifecycle := fxtest.NewLifecycle(t)
	runtime, err := actors.NewRuntime(t.Context(), fxLifecycle)
	require.NoError(t, err)
	gate, err := actors.NewGate[actors.AdmissionKey](t.Context(), runtime, "gitops-test-admission", t.Name())
	require.NoError(t, err)
	t.Cleanup(func() {
		stopCtx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		require.NoError(t, gate.Stop(stopCtx))
		require.NoError(t, fxLifecycle.Stop(stopCtx))
	})
	return gate
}

func setupGitOpsSyncDirectoryTestService(t *testing.T) (*GitOpsSyncService, *database.DB, string) {
	t.Helper()

	ctx := context.Background()
	db := setupGitOpsProjectTestDBInternal(t)
	require.NoError(t, db.AutoMigrate(&projectpkg.GitOpsSync{}))

	settingsService, err := newGitOpsSettingsServiceForTestInternal(t, ctx, db)
	require.NoError(t, err)

	projectsDir := t.TempDir()
	require.NoError(t, settingsService.SetStringSetting(ctx, "projectsDirectory", projectsDir))

	eventService := event.NewEventService(db, config.Load(), nil)
	projectService := projectpkg.NewProjectService(db, settingsService, eventService, nil, nil, nil, nil, nil, config.Load())

	return NewGitOpsSyncService(db, nil, projectService, nil, eventService, settingsService), db, projectsDir
}

func TestGitOpsSyncService_OverlappingSyncPreservesSuccessShapedSkipInternal(t *testing.T) {
	lifecycle := fxtest.NewLifecycle(t)
	actorRuntime, err := actors.NewRuntime(t.Context(), lifecycle)
	require.NoError(t, err)
	gate, err := actors.NewGate[actors.AdmissionKey](t.Context(), actorRuntime, "gitops-test-admission", "overlap")
	require.NoError(t, err)

	key := actors.AdmissionKey{Scope: gitOpsSyncAdmissionScopeInternal, ID: "sync-id"}
	lease, admitted, err := gate.TryAcquire(t.Context(), key)
	require.NoError(t, err)
	require.True(t, admitted)

	service := &GitOpsSyncService{jobs: entityjobs.New(entityjobs.GitOpsSyncJobPrefix, gitOpsSyncAdmissionScopeInternal)}
	require.NoError(t, service.SetScheduler(t.Context(), &gitOpsSyncTestSchedulerInternal{}, gate))
	result, err := service.PerformSync(t.Context(), "0", "sync-id", common.User{})
	require.NoError(t, err)
	require.False(t, result.Success)
	require.Equal(t, "sync already in progress", result.Message)
	lease.Release()

	stopCtx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	require.NoError(t, gate.Stop(stopCtx))
	require.NoError(t, lifecycle.Stop(stopCtx))
}

type gitOpsSyncTestSchedulerInternal struct {
	added   []string
	removed []string
}

func (s *gitOpsSyncTestSchedulerInternal) AddJob(_ context.Context, job schedulertypes.Job) error {
	s.added = append(s.added, job.Name())
	return nil
}

func (s *gitOpsSyncTestSchedulerInternal) RemoveJob(_ context.Context, name string) {
	s.removed = append(s.removed, name)
}

func (s *gitOpsSyncTestSchedulerInternal) HasJob(_ string) bool {
	return false
}

func writeFileInternal(t *testing.T, rootDir, relativePath string, content []byte) {
	t.Helper()

	targetPath := filepath.Join(rootDir, relativePath)
	require.NoError(t, os.MkdirAll(filepath.Dir(targetPath), 0o755))
	require.NoError(t, os.WriteFile(targetPath, content, 0o644))
}

func TestApplyLifecycleFieldsToSyncInternal_DefaultsPreDeployTimeout(t *testing.T) {
	var sync projectpkg.GitOpsSync

	applyLifecycleFieldsToSyncInternal(&sync, lifecycleConfigInputInternal{})

	require.Equal(t, projectpkg.DefaultTimeoutSec, sync.PreDeployTimeoutSec)
}

func TestApplyLifecycleFieldsToSyncInternal_UsesExplicitPreDeployTimeout(t *testing.T) {
	timeoutSec := 90
	var sync projectpkg.GitOpsSync

	applyLifecycleFieldsToSyncInternal(&sync, lifecycleConfigInputInternal{timeoutSec: &timeoutSec})

	require.Equal(t, timeoutSec, sync.PreDeployTimeoutSec)
}

func TestGitOpsSyncService_GetSyncByID_ReturnsNotFoundError(t *testing.T) {
	ctx := context.Background()
	svc, _, _ := setupGitOpsSyncDirectoryTestService(t)

	_, err := svc.GetSyncByID(ctx, "0", "missing-sync")

	require.ErrorIs(t, err, common.ErrNotFound)
}

func TestGitOpsSyncService_CleanupOrphanedSyncsOnStartup_DeletesOnlyOrphans(t *testing.T) {
	ctx := context.Background()
	svc, db, _ := setupGitOpsSyncDirectoryTestService(t)
	require.NoError(t, db.AutoMigrate(&environment.Environment{}))

	require.NoError(t, db.Create(&environment.Environment{
		ID:   "env-live",
		Name: "Live",
	}).Error)
	orphanSyncID := "sync-orphan"
	liveSyncID := "sync-live"
	require.NoError(t, db.Create(&projectpkg.GitOpsSync{
		ID:            orphanSyncID,
		Name:          "orphan",
		EnvironmentID: "env-missing",
		RepositoryID:  "repo-1",
		ComposePath:   "compose.yml",
		ProjectName:   "orphan",
		SyncInterval:  15,
	}).Error)
	require.NoError(t, db.Create(&projectpkg.GitOpsSync{
		ID:            liveSyncID,
		Name:          "live",
		EnvironmentID: "env-live",
		RepositoryID:  "repo-1",
		ComposePath:   "compose.yml",
		ProjectName:   "live",
		SyncInterval:  15,
	}).Error)
	require.NoError(t, db.Create(&projectpkg.Project{
		ID:              "project-orphan",
		Name:            "orphan",
		Path:            "/tmp/orphan",
		Status:          projectpkg.ProjectStatusStopped,
		GitOpsManagedBy: &orphanSyncID,
	}).Error)

	require.NoError(t, svc.CleanupOrphanedSyncsOnStartup(ctx))

	var orphanCount int64
	require.NoError(t, db.Model(&projectpkg.GitOpsSync{}).Where("id = ?", orphanSyncID).Count(&orphanCount).Error)
	require.Zero(t, orphanCount)

	var liveCount int64
	require.NoError(t, db.Model(&projectpkg.GitOpsSync{}).Where("id = ?", liveSyncID).Count(&liveCount).Error)
	require.EqualValues(t, 1, liveCount)

	var project projectpkg.Project
	require.NoError(t, db.First(&project, "id = ?", "project-orphan").Error)
	require.Nil(t, project.GitOpsManagedBy)
}

func TestGitOpsSyncService_RegisterAutoSyncJobsOnStartup_SkipsOrphans(t *testing.T) {
	ctx := context.Background()
	svc, db, _ := setupGitOpsSyncDirectoryTestService(t)
	require.NoError(t, db.AutoMigrate(&environment.Environment{}))

	require.NoError(t, db.Create(&environment.Environment{
		ID:   "env-live",
		Name: "Live",
	}).Error)
	require.NoError(t, db.Create(&projectpkg.GitOpsSync{
		ID:            "sync-orphan",
		Name:          "orphan",
		EnvironmentID: "env-missing",
		RepositoryID:  "repo-1",
		ComposePath:   "compose.yml",
		ProjectName:   "orphan",
		AutoSync:      true,
		SyncInterval:  15,
	}).Error)
	now := time.Now()
	require.NoError(t, db.Create(&projectpkg.GitOpsSync{
		ID:            "sync-live",
		Name:          "live",
		EnvironmentID: "env-live",
		RepositoryID:  "repo-1",
		ComposePath:   "compose.yml",
		ProjectName:   "live",
		AutoSync:      true,
		SyncInterval:  15,
		LastSyncAt:    &now,
	}).Error)

	scheduler := &gitOpsSyncTestSchedulerInternal{}
	require.NoError(t, svc.SetScheduler(ctx, scheduler, newGitOpsAdmissionGateForTestInternal(t)))
	svc.RegisterAutoSyncJobsOnStartup(ctx)

	require.Equal(t, []string{entityjobs.GitOpsSyncJobPrefix + "sync-live"}, scheduler.added)
	require.Empty(t, scheduler.removed)
}

func TestGitOpsSyncService_DeleteSync_DeletesStaleProjectReference(t *testing.T) {
	ctx := context.Background()
	svc, db, _ := setupGitOpsSyncDirectoryTestService(t)
	missingProjectID := "missing-project"

	sync := &projectpkg.GitOpsSync{
		ID:            "sync-delete-stale-project",
		Name:          "demo-sync",
		EnvironmentID: "0",
		RepositoryID:  "repo-1",
		ComposePath:   "apps/demo/docker-compose.yaml",
		ProjectName:   "demo-project",
		ProjectID:     &missingProjectID,
		SyncInterval:  60,
	}
	require.NoError(t, db.Create(sync).Error)

	require.NoError(t, svc.DeleteSync(ctx, "0", sync.ID, common.User{}))

	var count int64
	require.NoError(t, db.Model(&projectpkg.GitOpsSync{}).Where("id = ?", sync.ID).Count(&count).Error)
	assert.Zero(t, count)
}

// TestGitOpsSyncService_DeleteSync_SucceedsWhenEnvironmentMismatched proves a
// corrupt/env-mismatched sync is still deletable via the API: the request env ("0")
// does not match the row's env ("5"), yet the delete must succeed and stop the job.
func TestGitOpsSyncService_DeleteSync_SucceedsWhenEnvironmentMismatched(t *testing.T) {
	ctx := context.Background()
	svc, db, _ := setupGitOpsSyncDirectoryTestService(t)
	scheduler := &gitOpsSyncTestSchedulerInternal{}
	require.NoError(t, svc.SetScheduler(ctx, scheduler, newGitOpsAdmissionGateForTestInternal(t)))

	sync := &projectpkg.GitOpsSync{
		ID:            "sync-env-mismatch",
		Name:          "corrupt-sync",
		EnvironmentID: "5",
		ProjectName:   "demo-project",
		SyncInterval:  60,
	}
	require.NoError(t, db.Create(sync).Error)

	require.NoError(t, svc.DeleteSync(ctx, "0", sync.ID, common.User{}))

	var count int64
	require.NoError(t, db.Model(&projectpkg.GitOpsSync{}).Where("id = ?", sync.ID).Count(&count).Error)
	assert.Zero(t, count)
	assert.Contains(t, scheduler.removed, entityjobs.GitOpsSyncJobPrefix+sync.ID)
}

// TestGitOpsSyncService_DeleteSync_ClearsOrphanedManagedFlag verifies the managed
// flag is cleared keyed on the sync id even when the sync's ProjectID is nil.
func TestGitOpsSyncService_DeleteSync_ClearsOrphanedManagedFlag(t *testing.T) {
	ctx := context.Background()
	svc, db, _ := setupGitOpsSyncDirectoryTestService(t)

	syncID := "sync-orphan-flag"
	sync := &projectpkg.GitOpsSync{
		ID:            syncID,
		Name:          "demo-sync",
		EnvironmentID: "0",
		ProjectName:   "demo-project",
		SyncInterval:  60,
	}
	require.NoError(t, db.Create(sync).Error)

	managed := &projectpkg.Project{
		ID:              "proj-managed",
		Name:            "managed",
		Path:            filepath.Join(t.TempDir(), "managed"),
		GitOpsManagedBy: &syncID,
	}
	require.NoError(t, db.Create(managed).Error)

	require.NoError(t, svc.DeleteSync(ctx, "0", syncID, common.User{}))

	var got projectpkg.Project
	require.NoError(t, db.Where("id = ?", managed.ID).First(&got).Error)
	assert.Nil(t, got.GitOpsManagedBy)
}

// TestGitOpsSyncService_RunScheduledSync_UnregistersMissingSync verifies a scheduled
// run whose row no longer exists (e.g. deleted out-of-band via raw SQL) unregisters
// its own job instead of firing forever.
func TestGitOpsSyncService_RunScheduledSync_UnregistersMissingSync(t *testing.T) {
	ctx := context.Background()
	svc, _, _ := setupGitOpsSyncDirectoryTestService(t)
	scheduler := &gitOpsSyncTestSchedulerInternal{}
	require.NoError(t, svc.SetScheduler(ctx, scheduler, newGitOpsAdmissionGateForTestInternal(t)))

	svc.runScheduledSyncInternal(ctx, "0", "ghost-sync")

	assert.Contains(t, scheduler.removed, entityjobs.GitOpsSyncJobPrefix+"ghost-sync")
}

// TestGitOpsSyncService_CleanupLeakedScratchDirsOnStartup_RemovesOrphans verifies the
// startup sweep removes leaked gitops scratch dirs (hidden and legacy name-embedded
// forms) while leaving real project directories untouched.
func TestGitOpsSyncService_CleanupLeakedScratchDirsOnStartup_RemovesOrphans(t *testing.T) {
	ctx := context.Background()
	svc, _, projectsDir := setupGitOpsSyncDirectoryTestService(t)

	mkdir := func(name string) string {
		p := filepath.Join(projectsDir, name)
		require.NoError(t, os.MkdirAll(p, 0o755))
		return p
	}
	scratch := []string{
		mkdir(".gitops-sync-stage-1219810203"),
		mkdir(".gitops-backup-456"),
		mkdir("Makerra.gitops-backup-1780656786384743013"),
	}
	realProject := mkdir("app")
	require.NoError(t, os.WriteFile(filepath.Join(realProject, "compose.yaml"), []byte("services: {}\n"), 0o644))

	require.NoError(t, svc.CleanupLeakedScratchDirsOnStartup(ctx))

	for _, p := range scratch {
		_, err := os.Stat(p)
		require.ErrorIs(t, err, os.ErrNotExist, "scratch dir should be removed: %s", p)
	}
	_, err := os.Stat(realProject)
	assert.NoError(t, err, "real project dir must be kept")
}

// TestGitOpsSyncService_CleanupLeakedCloneDirsOnStartup_RemovesAll verifies the
// startup sweep purges leaked git clone scratch dirs from the git work dir.
func TestGitOpsSyncService_CleanupLeakedCloneDirsOnStartup_RemovesAll(t *testing.T) {
	ctx := context.Background()
	svc, _, _ := setupGitOpsSyncDirectoryTestService(t)

	workDir := t.TempDir()
	svc.repoService = &gitrepo.GitRepositoryService{Client: git.NewClient(workDir)}

	cloneDirs := []string{
		filepath.Join(workDir, "gitops-stale"),
		filepath.Join(workDir, "gitops-fresh"),
	}
	for _, p := range cloneDirs {
		require.NoError(t, os.MkdirAll(p, 0o755))
	}
	unrelated := filepath.Join(workDir, "keep-me")
	require.NoError(t, os.MkdirAll(unrelated, 0o755))

	require.NoError(t, svc.CleanupLeakedCloneDirsOnStartup(ctx))

	for _, p := range cloneDirs {
		_, err := os.Stat(p)
		require.ErrorIs(t, err, os.ErrNotExist, "clone scratch dir should be removed: %s", p)
	}
	_, err := os.Stat(unrelated)
	assert.NoError(t, err, "unrelated dir must be kept")
}

// TestGitOpsSyncService_CleanupLeakedCloneDirsOnStartup_NilRepoServiceIsNoop
// verifies the sweep tolerates a service built without a repo service.
func TestGitOpsSyncService_CleanupLeakedCloneDirsOnStartup_NilRepoServiceIsNoop(t *testing.T) {
	svc, _, _ := setupGitOpsSyncDirectoryTestService(t)
	svc.repoService = nil

	require.NoError(t, svc.CleanupLeakedCloneDirsOnStartup(context.Background()))
}

// TestGitOpsSyncService_SyncProjectDirectory_RefusesDuplicateOnNameCollision verifies a
// directory sync refuses to create a "-N" sibling when its target name is already taken
// by a non-adoptable directory; instead it errors as a broken binding and disables auto-sync.
func TestGitOpsSyncService_SyncProjectDirectory_RefusesDuplicateOnNameCollision(t *testing.T) {
	ctx := context.Background()
	svc, db, projectsDir := setupGitOpsSyncDirectoryTestService(t)

	// Occupy the target name with a dir that is NOT an adoptable GitOps project
	// (it has no matching compose file).
	require.NoError(t, os.MkdirAll(filepath.Join(projectsDir, "Dozzle"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(projectsDir, "Dozzle", "unrelated.txt"), []byte("x"), 0o644))

	sync := &projectpkg.GitOpsSync{
		ID:            "sync-dup-refuse",
		Name:          "Dozzle",
		EnvironmentID: "0",
		ComposePath:   "Dozzle/docker-compose.yaml",
		ProjectName:   "Dozzle",
		SyncDirectory: true,
		AutoSync:      true,
		SyncInterval:  60,
	}
	require.NoError(t, db.Create(sync).Error)

	syncFiles := []projects.SyncFile{
		{RelativePath: "docker-compose.yaml", Content: []byte("services:\n  app:\n    image: nginx:alpine\n")},
	}

	_, _, _, _, err := svc.syncProjectDirectoryInternal(ctx, sync, syncFiles, common.User{})
	require.ErrorIs(t, err, common.ErrGitOpsSyncProjectBindingBroken)

	_, statErr := os.Stat(filepath.Join(projectsDir, "Dozzle-1"))
	require.ErrorIs(t, statErr, os.ErrNotExist, "must not mint a -N duplicate")

	var got projectpkg.GitOpsSync
	require.NoError(t, db.Where("id = ?", sync.ID).First(&got).Error)
	assert.False(t, got.AutoSync, "auto-sync should be disabled on broken binding")
}

// TestGitOpsSyncService_GetOrCreateProject_RefusesDuplicateOnNameCollision is the
// single-file-sync analogue of the directory refuse: a name collision on create is a
// broken binding, not a "-N" duplicate.
func TestGitOpsSyncService_GetOrCreateProject_RefusesDuplicateOnNameCollision(t *testing.T) {
	ctx := context.Background()
	svc, db, projectsDir := setupGitOpsSyncDirectoryTestService(t)
	require.NoError(t, svc.SetScheduler(ctx, &gitOpsSyncTestSchedulerInternal{}, newGitOpsAdmissionGateForTestInternal(t)))

	require.NoError(t, os.MkdirAll(filepath.Join(projectsDir, "Dozzle"), 0o755))

	sync := &projectpkg.GitOpsSync{
		ID:            "sync-single-dup",
		Name:          "Dozzle",
		EnvironmentID: "0",
		ComposePath:   "docker-compose.yaml",
		ProjectName:   "Dozzle",
		AutoSync:      true,
		SyncInterval:  60,
	}
	require.NoError(t, db.Create(sync).Error)

	result := &gitops.SyncResult{}
	_, err := svc.getOrCreateProjectInternal(ctx, sync, sync.ID, "services:\n  app:\n    image: nginx:alpine\n", nil, nil, "", result, common.User{})
	require.ErrorIs(t, err, common.ErrGitOpsSyncProjectBindingBroken)

	_, statErr := os.Stat(filepath.Join(projectsDir, "Dozzle-1"))
	require.ErrorIs(t, statErr, os.ErrNotExist, "must not mint a -N duplicate")

	var got projectpkg.GitOpsSync
	require.NoError(t, db.Where("id = ?", sync.ID).First(&got).Error)
	assert.False(t, got.AutoSync, "auto-sync should be disabled on broken binding")
}

func TestGitOpsSyncService_SyncProjectDirectory_CreatesProjectPreservingRepoLayout(t *testing.T) {
	ctx := context.Background()
	svc, db, _ := setupGitOpsSyncDirectoryTestService(t)

	sync := &projectpkg.GitOpsSync{
		ID:            "sync-directory-create",
		Name:          "demo-sync",
		EnvironmentID: "0",
		RepositoryID:  "repo-1",
		ComposePath:   "apps/demo/docker-compose.yaml",
		ProjectName:   "demo-project",
		SyncDirectory: true,
	}
	require.NoError(t, db.Create(sync).Error)

	gitEnvContent := "# keep git formatting\r\nZ_LAST=last\r\nCLOUDFLARE_CLIENT_SECRET=$$pbkdf2-sha512$$310000$$XXX\r\nQUOTED_SECRET='$pbkdf2-sha512$310000$XXX'\r\nA_FIRST=first"
	syncFiles := []projects.SyncFile{
		{
			RelativePath: "docker-compose.yaml",
			Content: []byte(`include:
  - meta.yaml
services:
  app:
    image: nginx:alpine
    env_file:
      - .env
`),
		},
		{
			RelativePath: "meta.yaml",
			Content: []byte(`services:
  helper:
    image: busybox:latest
`),
		},
		{
			RelativePath: ".env",
			Content:      []byte(gitEnvContent),
		},
	}

	project, syncedFiles, created, changed, err := svc.syncProjectDirectoryInternal(ctx, sync, syncFiles, common.User{})
	require.NoError(t, err)
	require.NotNil(t, project)
	require.True(t, created)
	require.True(t, changed)
	// .env is a reserved root env file: it is routed through the override
	// merge rather than tracked as a raw synced file.
	require.ElementsMatch(t, []string{"docker-compose.yaml", "meta.yaml"}, syncedFiles)

	composePath, detectErr := projects.DetectComposeFile(t.Context(), "", project.Path)
	require.NoError(t, detectErr)
	assert.Equal(t, filepath.Join(project.Path, "docker-compose.yaml"), composePath)

	composeBytes, err := os.ReadFile(filepath.Join(project.Path, "docker-compose.yaml"))
	require.NoError(t, err)
	assert.Contains(t, string(composeBytes), "include:")

	metaBytes, err := os.ReadFile(filepath.Join(project.Path, "meta.yaml"))
	require.NoError(t, err)
	assert.Contains(t, string(metaBytes), "helper:")

	envBytes, err := os.ReadFile(filepath.Join(project.Path, ".env"))
	require.NoError(t, err)
	assert.Equal(t, gitEnvContent, string(envBytes))

	gitEnvBytes, err := os.ReadFile(filepath.Join(project.Path, ".env.git"))
	require.NoError(t, err)
	assert.Equal(t, gitEnvContent, string(gitEnvBytes))

	parsedEnv, err := projects.ParseProjectEnvFile(filepath.Join(project.Path, ".env"), nil)
	require.NoError(t, err)
	assert.Equal(t, "$pbkdf2-sha512$310000$XXX", parsedEnv["CLOUDFLARE_CLIENT_SECRET"])
	assert.Equal(t, "$pbkdf2-sha512$310000$XXX", parsedEnv["QUOTED_SECRET"])

	_, statErr := os.Stat(filepath.Join(project.Path, "compose.yaml"))
	assert.ErrorIs(t, statErr, os.ErrNotExist)
}

func TestGitOpsSyncService_SyncProjectDirectory_UpdatesProjectAndCleansOldSyncedFiles(t *testing.T) {
	ctx := context.Background()
	svc, db, projectsDir := setupGitOpsSyncDirectoryTestService(t)

	projectPath := filepath.Join(projectsDir, "demo-project")
	require.NoError(t, os.MkdirAll(filepath.Join(projectPath, "nested"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(projectPath, "docker-compose.yaml"), []byte(`include:
  - meta.yaml
services:
  app:
    image: nginx:1.26-alpine
`), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(projectPath, "meta.yaml"), []byte(`services:
  helper:
    image: busybox:1.36
`), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(projectPath, "old.txt"), []byte("remove me\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(projectPath, "keep.txt"), []byte("keep me\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(projectPath, "compose.yaml"), []byte("services: {}\n"), 0o644))

	project := &projectpkg.Project{
		ID:      "proj-directory-update",
		Name:    "demo-project",
		DirName: new("demo-project"),
		Path:    projectPath,
		Status:  projectpkg.ProjectStatusStopped,
	}
	require.NoError(t, db.Create(project).Error)

	oldSyncedFilesJSON, err := json.Marshal([]string{"docker-compose.yaml", "meta.yaml", "old.txt"})
	require.NoError(t, err)

	sync := &projectpkg.GitOpsSync{
		ID:            "sync-directory-update",
		Name:          "demo-sync",
		EnvironmentID: "0",
		RepositoryID:  "repo-1",
		ComposePath:   "apps/demo/docker-compose.yaml",
		ProjectName:   "demo-project",
		ProjectID:     &project.ID,
		SyncDirectory: true,
		SyncedFiles:   new(string(oldSyncedFilesJSON)),
	}
	require.NoError(t, db.Create(sync).Error)

	syncFiles := []projects.SyncFile{
		{
			RelativePath: "docker-compose.yaml",
			Content: []byte(`include:
  - nested/feature.yaml
services:
  app:
    image: nginx:1.27-alpine
`),
		},
		{
			RelativePath: "nested/feature.yaml",
			Content: []byte(`services:
  worker:
    image: busybox:latest
`),
		},
	}

	updatedProject, syncedFiles, created, changed, err := svc.syncProjectDirectoryInternal(ctx, sync, syncFiles, common.User{})
	require.NoError(t, err)
	require.NotNil(t, updatedProject)
	require.False(t, created)
	require.True(t, changed)
	require.ElementsMatch(t, []string{"docker-compose.yaml", "nested/feature.yaml"}, syncedFiles)

	composePath, detectErr := projects.DetectComposeFile(t.Context(), "", updatedProject.Path)
	require.NoError(t, detectErr)
	assert.Equal(t, filepath.Join(updatedProject.Path, "docker-compose.yaml"), composePath)

	_, statErr := os.Stat(filepath.Join(updatedProject.Path, "old.txt"))
	require.ErrorIs(t, statErr, os.ErrNotExist)

	_, statErr = os.Stat(filepath.Join(updatedProject.Path, "compose.yaml"))
	require.ErrorIs(t, statErr, os.ErrNotExist)

	keepBytes, err := os.ReadFile(filepath.Join(updatedProject.Path, "keep.txt"))
	require.NoError(t, err)
	assert.Equal(t, "keep me\n", string(keepBytes))

	featureBytes, err := os.ReadFile(filepath.Join(updatedProject.Path, "nested", "feature.yaml"))
	require.NoError(t, err)
	assert.Contains(t, string(featureBytes), "worker:")
}

// TestGitOpsSyncService_SyncProjectDirectory_PreservesEnvOverrideAndAddsNewGitKey
// verifies directory sync routes the project-root .env through the same
// three-file override merge single-file git sync uses: an edit made in Arcane
// (recorded as project.env) survives the sync, while a new key introduced by
// git still flows into the effective .env. This is the core regression test
// for https://github.com/getarcaneapp/arcane/issues/2476.
func TestGitOpsSyncService_SyncProjectDirectory_PreservesEnvOverrideAndAddsNewGitKey(t *testing.T) {
	ctx := context.Background()
	svc, db, projectsDir := setupGitOpsSyncDirectoryTestService(t)

	projectPath := filepath.Join(projectsDir, "demo-project")
	require.NoError(t, os.MkdirAll(projectPath, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(projectPath, "docker-compose.yaml"), []byte(`services:
  app:
    image: nginx:1.26-alpine
`), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(projectPath, ".env.git"), []byte("FOO=git\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(projectPath, "project.env"), []byte("FOO=useredit\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(projectPath, ".env"), []byte("FOO=useredit\n"), 0o644))

	project := &projectpkg.Project{
		ID:      "proj-directory-env-preserve",
		Name:    "demo-project",
		DirName: new("demo-project"),
		Path:    projectPath,
		Status:  projectpkg.ProjectStatusStopped,
	}
	require.NoError(t, db.Create(project).Error)

	oldSyncedFilesJSON, err := json.Marshal([]string{"docker-compose.yaml"})
	require.NoError(t, err)

	sync := &projectpkg.GitOpsSync{
		ID:            "sync-directory-env-preserve",
		Name:          "demo-sync",
		EnvironmentID: "0",
		RepositoryID:  "repo-1",
		ComposePath:   "apps/demo/docker-compose.yaml",
		ProjectName:   "demo-project",
		ProjectID:     &project.ID,
		SyncDirectory: true,
		SyncedFiles:   new(string(oldSyncedFilesJSON)),
	}
	require.NoError(t, db.Create(sync).Error)

	newGitEnvContent := "FOO=gitnew\nBAR=new\n"
	syncFiles := []projects.SyncFile{
		{
			RelativePath: "docker-compose.yaml",
			Content: []byte(`services:
  app:
    image: nginx:1.27-alpine
`),
		},
		{
			RelativePath: ".env",
			Content:      []byte(newGitEnvContent),
		},
	}

	updatedProject, syncedFiles, created, changed, err := svc.syncProjectDirectoryInternal(ctx, sync, syncFiles, common.User{})
	require.NoError(t, err)
	require.NotNil(t, updatedProject)
	require.False(t, created)
	require.True(t, changed)
	require.ElementsMatch(t, []string{"docker-compose.yaml"}, syncedFiles)

	effectiveBytes, err := os.ReadFile(filepath.Join(updatedProject.Path, ".env"))
	require.NoError(t, err)
	assert.Equal(t, "FOO=useredit\nBAR=new\n", string(effectiveBytes))

	gitBytes, err := os.ReadFile(filepath.Join(updatedProject.Path, ".env.git"))
	require.NoError(t, err)
	assert.Equal(t, newGitEnvContent, string(gitBytes))

	overrideBytes, err := os.ReadFile(filepath.Join(updatedProject.Path, "project.env"))
	require.NoError(t, err)
	assert.Equal(t, "FOO=useredit\n", string(overrideBytes))

	effectiveEnv, err := projects.ParseProjectEnvFile(filepath.Join(updatedProject.Path, ".env"), nil)
	require.NoError(t, err)
	assert.Equal(t, projects.EnvMap{"FOO": "useredit", "BAR": "new"}, effectiveEnv)

	gitEnv, err := projects.ParseProjectEnvFile(filepath.Join(updatedProject.Path, ".env.git"), nil)
	require.NoError(t, err)
	assert.Equal(t, projects.EnvMap{"FOO": "gitnew", "BAR": "new"}, gitEnv)

	overrideEnv, err := projects.ParseProjectEnvFile(filepath.Join(updatedProject.Path, "project.env"), nil)
	require.NoError(t, err)
	assert.Equal(t, projects.EnvMap{"FOO": "useredit"}, overrideEnv)
}

// TestGitOpsSyncService_SyncProjectDirectory_MigratesLegacyTrackedEnvOnFirstSyncAfterUpgrade
// verifies the first directory sync after upgrading from a pre-override-merge
// Arcane version: sync.SyncedFiles still lists ".env" from the old raw-write
// path, and the project has only a direct .env (no .env.git/project.env yet).
// CleanupRemovedFiles must not delete the live-copied stage .env before the
// merge step reads it, or a local-only key would be silently dropped instead
// of migrated into project.env.
func TestGitOpsSyncService_SyncProjectDirectory_MigratesLegacyTrackedEnvOnFirstSyncAfterUpgrade(t *testing.T) {
	ctx := context.Background()
	svc, db, projectsDir := setupGitOpsSyncDirectoryTestService(t)

	projectPath := filepath.Join(projectsDir, "demo-project")
	require.NoError(t, os.MkdirAll(projectPath, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(projectPath, "docker-compose.yaml"), []byte(`services:
  app:
    image: nginx:1.26-alpine
`), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(projectPath, ".env"), []byte("LOCAL_ONLY=1\n"), 0o644))

	project := &projectpkg.Project{
		ID:      "proj-directory-legacy-env-migrate",
		Name:    "demo-project",
		DirName: new("demo-project"),
		Path:    projectPath,
		Status:  projectpkg.ProjectStatusStopped,
	}
	require.NoError(t, db.Create(project).Error)

	// A pre-fix sync recorded .env as a plain tracked file.
	oldSyncedFilesJSON, err := json.Marshal([]string{"docker-compose.yaml", ".env"})
	require.NoError(t, err)

	sync := &projectpkg.GitOpsSync{
		ID:            "sync-directory-legacy-env-migrate",
		Name:          "demo-sync",
		EnvironmentID: "0",
		RepositoryID:  "repo-1",
		ComposePath:   "apps/demo/docker-compose.yaml",
		ProjectName:   "demo-project",
		ProjectID:     &project.ID,
		SyncDirectory: true,
		SyncedFiles:   new(string(oldSyncedFilesJSON)),
	}
	require.NoError(t, db.Create(sync).Error)

	syncFiles := []projects.SyncFile{
		{
			RelativePath: "docker-compose.yaml",
			Content: []byte(`services:
  app:
    image: nginx:1.27-alpine
`),
		},
		{
			RelativePath: ".env",
			Content:      []byte("BASE=git\n"),
		},
	}

	updatedProject, _, created, _, err := svc.syncProjectDirectoryInternal(ctx, sync, syncFiles, common.User{})
	require.NoError(t, err)
	require.NotNil(t, updatedProject)
	require.False(t, created)

	effectiveEnv, err := projects.ParseProjectEnvFile(filepath.Join(updatedProject.Path, ".env"), nil)
	require.NoError(t, err)
	assert.Equal(t, projects.EnvMap{"BASE": "git", "LOCAL_ONLY": "1"}, effectiveEnv)

	overrideEnv, err := projects.ParseProjectEnvFile(filepath.Join(updatedProject.Path, "project.env"), nil)
	require.NoError(t, err)
	assert.Equal(t, projects.EnvMap{"LOCAL_ONLY": "1"}, overrideEnv)
}

// TestGitOpsSyncService_SyncProjectDirectory_IgnoresCommittedReservedEnvFiles verifies
// that a repo committing files at the reserved bookkeeping paths (.env.git,
// project.env) cannot clobber Arcane's own override-merge bookkeeping: those paths
// are dropped from the raw sync write, never tracked in syncedFiles, and the actual
// .env.git/project.env on disk are still produced solely by the merge.
func TestGitOpsSyncService_SyncProjectDirectory_IgnoresCommittedReservedEnvFiles(t *testing.T) {
	ctx := context.Background()
	svc, db, _ := setupGitOpsSyncDirectoryTestService(t)

	sync := &projectpkg.GitOpsSync{
		ID:            "sync-directory-reserved-env",
		Name:          "demo-sync",
		EnvironmentID: "0",
		RepositoryID:  "repo-1",
		ComposePath:   "apps/demo/docker-compose.yaml",
		ProjectName:   "demo-project-reserved",
		SyncDirectory: true,
	}
	require.NoError(t, db.Create(sync).Error)

	syncFiles := []projects.SyncFile{
		{
			RelativePath: "docker-compose.yaml",
			Content: []byte(`services:
  app:
    image: nginx:alpine
`),
		},
		{
			RelativePath: ".env",
			Content:      []byte("FOO=fromgit\n"),
		},
		{
			RelativePath: ".env.git",
			Content:      []byte("poison\n"),
		},
		{
			RelativePath: "project.env",
			Content:      []byte("poison\n"),
		},
	}

	project, syncedFiles, created, _, err := svc.syncProjectDirectoryInternal(ctx, sync, syncFiles, common.User{})
	require.NoError(t, err)
	require.NotNil(t, project)
	require.True(t, created)
	require.ElementsMatch(t, []string{"docker-compose.yaml"}, syncedFiles)

	effectiveEnv, err := projects.ParseProjectEnvFile(filepath.Join(project.Path, ".env"), nil)
	require.NoError(t, err)
	assert.Equal(t, projects.EnvMap{"FOO": "fromgit"}, effectiveEnv)

	gitBytes, err := os.ReadFile(filepath.Join(project.Path, ".env.git"))
	require.NoError(t, err)
	assert.NotContains(t, string(gitBytes), "poison")
	gitEnv, err := projects.ParseProjectEnvFile(filepath.Join(project.Path, ".env.git"), nil)
	require.NoError(t, err)
	assert.Equal(t, projects.EnvMap{"FOO": "fromgit"}, gitEnv)

	_, statErr := os.Stat(filepath.Join(project.Path, "project.env"))
	assert.True(t, os.IsNotExist(statErr))
}

func TestGitOpsSyncService_DirectorySync_RealWalkWithNestedConfig(t *testing.T) {
	ctx := context.Background()
	svc, db, _ := setupGitOpsSyncDirectoryTestService(t)
	svc.repoService = &gitrepo.GitRepositoryService{Client: git.NewClient("")}

	repoPath := t.TempDir()
	writeFileInternal(t, repoPath, "traefik (nl10)/docker-compose.yml", []byte(`services:
  traefik:
    image: traefik:v3.4
    volumes:
      - ./letsencrypt:/letsencrypt
      - ./logs:/var/log/traefik
      - ./config/dynamic_config.yml:/etc/traefik/dynamic_config.yml:ro
`))
	writeFileInternal(t, repoPath, "traefik (nl10)/config/dynamic_config.yml", []byte(`http:
  routers:
    dashboard:
      rule: Host(`+"`"+`traefik.example.com`+"`"+`)
`))

	sync := &projectpkg.GitOpsSync{
		ID:            "sync-directory-real-walk",
		Name:          "traefik-sync",
		EnvironmentID: "0",
		RepositoryID:  "repo-1",
		ComposePath:   "traefik (nl10)/docker-compose.yml",
		ProjectName:   "traefik (nl10)",
		SyncDirectory: true,
	}
	require.NoError(t, db.Create(sync).Error)

	syncFiles, err := svc.walkAndParseSyncDirectory(ctx, sync, repoPath)
	require.NoError(t, err)
	var composeContent string
	for _, f := range syncFiles {
		if f.RelativePath == "docker-compose.yml" {
			composeContent = string(f.Content)
		}
	}
	assert.Contains(t, composeContent, "./config/dynamic_config.yml")

	project, syncedFiles, created, changed, err := svc.syncProjectDirectoryInternal(ctx, sync, syncFiles, common.User{})
	require.NoError(t, err)
	require.NotNil(t, project)
	require.True(t, created)
	require.True(t, changed)
	require.ElementsMatch(t, []string{"docker-compose.yml", "config/dynamic_config.yml"}, syncedFiles)

	composePath, detectErr := projects.DetectComposeFile(t.Context(), "", project.Path)
	require.NoError(t, detectErr)
	assert.Equal(t, filepath.Join(project.Path, "docker-compose.yml"), composePath)

	composeInfo, err := os.Stat(filepath.Join(project.Path, "docker-compose.yml"))
	require.NoError(t, err)
	assert.False(t, composeInfo.IsDir())

	configPath := filepath.Join(project.Path, "config", "dynamic_config.yml")
	configInfo, err := os.Stat(configPath)
	require.NoError(t, err)
	assert.False(t, configInfo.IsDir())

	configBytes, err := os.ReadFile(configPath)
	require.NoError(t, err)
	assert.Contains(t, string(configBytes), "dashboard:")
}

func TestGitOpsSyncService_DirectorySync_OverwritesExistingDirectoryAtFilePath(t *testing.T) {
	ctx := context.Background()
	svc, db, projectsDir := setupGitOpsSyncDirectoryTestService(t)
	svc.repoService = &gitrepo.GitRepositoryService{Client: git.NewClient("")}

	repoPath := t.TempDir()
	writeFileInternal(t, repoPath, "traefik (nl10)/docker-compose.yml", []byte(`services:
  traefik:
    image: traefik:v3.4
    volumes:
      - ./letsencrypt:/letsencrypt
      - ./logs:/var/log/traefik
      - ./config/dynamic_config.yml:/etc/traefik/dynamic_config.yml:ro
`))
	writeFileInternal(t, repoPath, "traefik (nl10)/config/dynamic_config.yml", []byte(`http:
  routers:
    dashboard:
      rule: Host(`+"`"+`traefik.example.com`+"`"+`)
`))

	projectPath := filepath.Join(projectsDir, "traefik-project")
	require.NoError(t, os.MkdirAll(filepath.Join(projectPath, "config", "dynamic_config.yml"), 0o755))
	require.NoError(t, os.MkdirAll(filepath.Join(projectPath, "letsencrypt"), 0o755))
	require.NoError(t, os.MkdirAll(filepath.Join(projectPath, "logs"), 0o755))

	dirName := "traefik-project"
	project := &projectpkg.Project{
		ID:      "proj-directory-docker-dir-conflict",
		Name:    "traefik-project",
		DirName: &dirName,
		Path:    projectPath,
		Status:  projectpkg.ProjectStatusStopped,
	}
	require.NoError(t, db.Create(project).Error)

	sync := &projectpkg.GitOpsSync{
		ID:            "sync-directory-docker-dir-conflict",
		Name:          "traefik-sync",
		EnvironmentID: "0",
		RepositoryID:  "repo-1",
		ComposePath:   "traefik (nl10)/docker-compose.yml",
		ProjectName:   "traefik-project",
		ProjectID:     &project.ID,
		SyncDirectory: true,
	}
	require.NoError(t, db.Create(sync).Error)

	syncFiles, err := svc.walkAndParseSyncDirectory(ctx, sync, repoPath)
	require.NoError(t, err)

	updatedProject, syncedFiles, created, changed, err := svc.syncProjectDirectoryInternal(ctx, sync, syncFiles, common.User{})
	require.NoError(t, err)
	require.NotNil(t, updatedProject)
	require.False(t, created)
	require.True(t, changed)
	require.ElementsMatch(t, []string{"docker-compose.yml", "config/dynamic_config.yml"}, syncedFiles)

	configPath := filepath.Join(updatedProject.Path, "config", "dynamic_config.yml")
	configInfo, err := os.Stat(configPath)
	require.NoError(t, err)
	assert.False(t, configInfo.IsDir())

	configBytes, err := os.ReadFile(configPath)
	require.NoError(t, err)
	assert.Contains(t, string(configBytes), "dashboard:")

	composeInfo, err := os.Stat(filepath.Join(updatedProject.Path, "docker-compose.yml"))
	require.NoError(t, err)
	assert.False(t, composeInfo.IsDir())

	dockerArtifactInfo, err := os.Stat(filepath.Join(updatedProject.Path, "letsencrypt"))
	require.NoError(t, err)
	assert.True(t, dockerArtifactInfo.IsDir())

	dockerArtifactInfo, err = os.Stat(filepath.Join(updatedProject.Path, "logs"))
	require.NoError(t, err)
	assert.True(t, dockerArtifactInfo.IsDir())
}

func TestGitOpsSyncService_CreateDirectorySyncProjectInternal_RollsBackProjectOnUpdateFailure(t *testing.T) {
	ctx := context.Background()
	svc, db, projectsDir := setupGitOpsSyncDirectoryTestService(t)

	sync := &projectpkg.GitOpsSync{
		ID:            "sync-directory-tx-rollback",
		Name:          "demo-sync",
		EnvironmentID: "0",
		RepositoryID:  "repo-1",
		ComposePath:   "apps/demo/docker-compose.yaml",
		ProjectName:   "demo-project",
		SyncDirectory: true,
	}
	require.NoError(t, db.Create(sync).Error)

	stagePath := filepath.Join(projectsDir, ".gitops-sync-stage-test")
	require.NoError(t, os.MkdirAll(stagePath, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(stagePath, "docker-compose.yaml"), []byte("services: {}\n"), 0o644))

	stage := &stagedDirectorySync{
		stagePath:       stagePath,
		stageLogical:    "/.gitops-sync-stage-test",
		projectsDir:     projectsDir,
		composeFileName: "docker-compose.yaml",
		serviceCount:    1,
	}

	callbackName := "test:fail_project_gitops_update"
	require.NoError(t, db.Callback().Update().Before("gorm:update").Register(callbackName, func(tx *gorm.DB) {
		if tx.Statement != nil && tx.Statement.Table == "projects" {
			_ = tx.AddError(errors.New("forced project update failure"))
		}
	}))
	defer func() {
		_ = db.Callback().Update().Remove(callbackName)
	}()

	project, err := svc.createDirectorySyncProjectInternal(ctx, sync, stage, common.User{})
	require.Error(t, err)
	require.Nil(t, project)
	assert.Contains(t, err.Error(), "failed to mark project as GitOps-managed")

	var projectCount int64
	require.NoError(t, db.Model(&projectpkg.Project{}).Count(&projectCount).Error)
	assert.Zero(t, projectCount)

	var storedSync projectpkg.GitOpsSync
	require.NoError(t, db.First(&storedSync, "id = ?", sync.ID).Error)
	assert.Nil(t, storedSync.ProjectID)

	_, statErr := os.Stat(filepath.Join(projectsDir, "demo-project"))
	assert.ErrorIs(t, statErr, os.ErrNotExist)
}

func TestProjectsRemoveStaleComposeFiles_RemovesStaleCustomComposeFiles(t *testing.T) {
	t.Parallel()

	projectPath := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(projectPath, "radarr.yaml"), []byte("services:\n  app:\n    image: nginx:alpine\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(projectPath, "sonarr.yaml"), []byte("services:\n  app:\n    image: nginx:alpine\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(projectPath, "values.yaml"), []byte("replicaCount: 2\nimage:\n  tag: latest\n"), 0o644))

	err := projects.RemoveStaleComposeFiles(t.Context(), projectPath, "sonarr.yaml", []string{"sonarr.yaml"})
	require.NoError(t, err)

	_, statErr := os.Stat(filepath.Join(projectPath, "radarr.yaml"))
	require.ErrorIs(t, statErr, os.ErrNotExist)

	_, statErr = os.Stat(filepath.Join(projectPath, "sonarr.yaml"))
	require.NoError(t, statErr)

	_, statErr = os.Stat(filepath.Join(projectPath, "values.yaml"))
	require.NoError(t, statErr)
}

func TestGitOpsSyncService_GetDirectorySyncProjectInternal_RelinksManagedProjectWhenProjectIDStale(t *testing.T) {
	ctx := context.Background()
	svc, db, projectsDir := setupGitOpsSyncDirectoryTestService(t)

	projectPath := filepath.Join(projectsDir, "Radarr-3")
	require.NoError(t, os.MkdirAll(projectPath, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(projectPath, "radarr.yaml"), []byte("services:\n  app:\n    image: lscr.io/linuxserver/radarr:latest\n"), 0o644))

	missingProjectID := "missing-project"
	sync := &projectpkg.GitOpsSync{
		ID:            "sync-directory-relink",
		Name:          "radarr-sync",
		EnvironmentID: "0",
		RepositoryID:  "repo-1",
		ComposePath:   "apps/media/radarr.yaml",
		ProjectName:   "Radarr",
		ProjectID:     &missingProjectID,
		SyncDirectory: true,
	}
	require.NoError(t, db.Create(sync).Error)

	dirName := "Radarr-3"
	project := &projectpkg.Project{
		ID:              "proj-directory-relink",
		Name:            "Radarr",
		DirName:         &dirName,
		Path:            projectPath,
		Status:          projectpkg.ProjectStatusStopped,
		GitOpsManagedBy: &sync.ID,
	}
	require.NoError(t, db.Create(project).Error)

	recovered, err := svc.getDirectorySyncProjectInternal(ctx, sync)
	require.NoError(t, err)
	require.NotNil(t, recovered)
	assert.Equal(t, project.ID, recovered.ID)

	var storedSync projectpkg.GitOpsSync
	require.NoError(t, db.First(&storedSync, "id = ?", sync.ID).Error)
	require.NotNil(t, storedSync.ProjectID)
	assert.Equal(t, project.ID, *storedSync.ProjectID)
}

func TestGitOpsSyncService_GetDirectorySyncProjectInternal_RecoversUniqueDirectoryCandidate(t *testing.T) {
	ctx := context.Background()
	svc, db, projectsDir := setupGitOpsSyncDirectoryTestService(t)

	projectPath := filepath.Join(projectsDir, "Radarr-3")
	require.NoError(t, os.MkdirAll(projectPath, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(projectPath, "radarr.yaml"), []byte("services:\n  app:\n    image: lscr.io/linuxserver/radarr:latest\n"), 0o644))

	missingProjectID := "missing-project"
	sync := &projectpkg.GitOpsSync{
		ID:            "sync-directory-disk-recovery",
		Name:          "radarr-sync",
		EnvironmentID: "0",
		RepositoryID:  "repo-1",
		ComposePath:   "apps/media/radarr.yaml",
		ProjectName:   "Radarr",
		ProjectID:     &missingProjectID,
		SyncDirectory: true,
	}
	require.NoError(t, db.Create(sync).Error)

	recovered, err := svc.getDirectorySyncProjectInternal(ctx, sync)
	require.NoError(t, err)
	require.NotNil(t, recovered)
	assert.Equal(t, projectPath, recovered.Path)
	require.NotNil(t, recovered.GitOpsManagedBy)
	assert.Equal(t, sync.ID, *recovered.GitOpsManagedBy)

	var storedSync projectpkg.GitOpsSync
	require.NoError(t, db.First(&storedSync, "id = ?", sync.ID).Error)
	require.NotNil(t, storedSync.ProjectID)
	assert.Equal(t, recovered.ID, *storedSync.ProjectID)

	var storedProject projectpkg.Project
	require.NoError(t, db.First(&storedProject, "id = ?", recovered.ID).Error)
	assert.Equal(t, projectPath, storedProject.Path)
	assert.Equal(t, 1, storedProject.ServiceCount)
}

func TestGitOpsSyncService_ReconcileDirectorySyncProjectsOnStartup_SkipsAmbiguousDuplicates(t *testing.T) {
	ctx := context.Background()
	svc, db, projectsDir := setupGitOpsSyncDirectoryTestService(t)

	for _, dirName := range []string{"Radarr-3", "Radarr-30"} {
		projectPath := filepath.Join(projectsDir, dirName)
		require.NoError(t, os.MkdirAll(projectPath, 0o755))
		require.NoError(t, os.WriteFile(filepath.Join(projectPath, "radarr.yaml"), []byte("services:\n  app:\n    image: lscr.io/linuxserver/radarr:latest\n"), 0o644))
	}

	missingProjectID := "missing-project"
	sync := &projectpkg.GitOpsSync{
		ID:            "sync-directory-ambiguous",
		Name:          "radarr-sync",
		EnvironmentID: "0",
		RepositoryID:  "repo-1",
		ComposePath:   "apps/media/radarr.yaml",
		ProjectName:   "Radarr",
		ProjectID:     &missingProjectID,
		SyncDirectory: true,
	}
	require.NoError(t, db.Create(sync).Error)

	require.NoError(t, svc.ReconcileDirectorySyncProjectsOnStartup(ctx))

	var projectsCount int64
	require.NoError(t, db.Model(&projectpkg.Project{}).Count(&projectsCount).Error)
	assert.Zero(t, projectsCount)

	var storedSync projectpkg.GitOpsSync
	require.NoError(t, db.First(&storedSync, "id = ?", sync.ID).Error)
	require.NotNil(t, storedSync.ProjectID)
	assert.Equal(t, "missing-project", *storedSync.ProjectID)
}

func TestGitOpsSyncService_SyncProjectDirectory_FailsWhenBoundProjectMissing(t *testing.T) {
	ctx := context.Background()
	svc, db, projectsDir := setupGitOpsSyncDirectoryTestService(t)
	testScheduler := &gitOpsSyncTestSchedulerInternal{}
	require.NoError(t, svc.SetScheduler(ctx, testScheduler, newGitOpsAdmissionGateForTestInternal(t)))

	missingProjectID := "missing-project"
	sync := &projectpkg.GitOpsSync{
		ID:            "sync-directory-missing-bound-project",
		Name:          "demo-sync",
		EnvironmentID: "0",
		RepositoryID:  "repo-1",
		ComposePath:   "apps/demo/docker-compose.yaml",
		ProjectName:   "demo-project",
		ProjectID:     &missingProjectID,
		SyncDirectory: true,
		AutoSync:      true,
	}
	require.NoError(t, db.Create(sync).Error)

	syncFiles := []projects.SyncFile{
		{
			RelativePath: "docker-compose.yaml",
			Content: []byte(`services:
  app:
    image: nginx:alpine
`),
		},
	}

	project, syncedFiles, created, changed, err := svc.syncProjectDirectoryInternal(ctx, sync, syncFiles, common.User{})
	require.Error(t, err)
	require.ErrorIs(t, err, common.ErrGitOpsSyncProjectBindingBroken)
	require.Nil(t, project)
	assert.Nil(t, syncedFiles)
	assert.False(t, created)
	assert.False(t, changed)

	var projectCount int64
	require.NoError(t, db.Model(&projectpkg.Project{}).Count(&projectCount).Error)
	assert.Zero(t, projectCount)

	_, statErr := os.Stat(filepath.Join(projectsDir, "demo-project"))
	require.ErrorIs(t, statErr, os.ErrNotExist)

	var storedSync projectpkg.GitOpsSync
	require.NoError(t, db.First(&storedSync, "id = ?", sync.ID).Error)
	assert.False(t, storedSync.AutoSync)
	require.NotNil(t, storedSync.LastSyncStatus)
	assert.Equal(t, "failed", *storedSync.LastSyncStatus)
	require.NotNil(t, storedSync.LastSyncError)
	assert.Contains(t, *storedSync.LastSyncError, "project binding")
	assert.Contains(t, testScheduler.removed, entityjobs.GitOpsSyncJobPrefix+sync.ID)
}

func TestGitOpsSyncService_SyncProjectDirectory_DisablesAutoSyncWhenBoundProjectRecoveryAmbiguous(t *testing.T) {
	ctx := context.Background()
	svc, db, projectsDir := setupGitOpsSyncDirectoryTestService(t)
	testScheduler := &gitOpsSyncTestSchedulerInternal{}
	require.NoError(t, svc.SetScheduler(ctx, testScheduler, newGitOpsAdmissionGateForTestInternal(t)))

	for _, dirName := range []string{"Radarr-3", "Radarr-30"} {
		projectPath := filepath.Join(projectsDir, dirName)
		require.NoError(t, os.MkdirAll(projectPath, 0o755))
		require.NoError(t, os.WriteFile(filepath.Join(projectPath, "radarr.yaml"), []byte("services:\n  app:\n    image: lscr.io/linuxserver/radarr:latest\n"), 0o644))
	}

	missingProjectID := "missing-project"
	sync := &projectpkg.GitOpsSync{
		ID:            "sync-directory-ambiguous-bound-project",
		Name:          "radarr-sync",
		EnvironmentID: "0",
		RepositoryID:  "repo-1",
		ComposePath:   "apps/media/radarr.yaml",
		ProjectName:   "Radarr",
		ProjectID:     &missingProjectID,
		SyncDirectory: true,
		AutoSync:      true,
	}
	require.NoError(t, db.Create(sync).Error)

	syncFiles := []projects.SyncFile{
		{
			RelativePath: "radarr.yaml",
			Content:      []byte("services:\n  app:\n    image: lscr.io/linuxserver/radarr:latest\n"),
		},
	}

	project, syncedFiles, created, changed, err := svc.syncProjectDirectoryInternal(ctx, sync, syncFiles, common.User{})
	require.Error(t, err)
	require.ErrorIs(t, err, common.ErrGitOpsSyncProjectBindingBroken)
	require.Nil(t, project)
	assert.Nil(t, syncedFiles)
	assert.False(t, created)
	assert.False(t, changed)

	var projectCount int64
	require.NoError(t, db.Model(&projectpkg.Project{}).Count(&projectCount).Error)
	assert.Zero(t, projectCount)

	var storedSync projectpkg.GitOpsSync
	require.NoError(t, db.First(&storedSync, "id = ?", sync.ID).Error)
	assert.False(t, storedSync.AutoSync)
	require.NotNil(t, storedSync.LastSyncStatus)
	assert.Equal(t, "failed", *storedSync.LastSyncStatus)
	require.NotNil(t, storedSync.LastSyncError)
	assert.Contains(t, *storedSync.LastSyncError, "multiple candidate project directories")
	assert.Contains(t, testScheduler.removed, entityjobs.GitOpsSyncJobPrefix+sync.ID)
}

func TestGitOpsSyncService_GetOrCreateProjectInternal_FailsWhenBoundProjectMissing(t *testing.T) {
	ctx := context.Background()
	svc, db, projectsDir := setupGitOpsSyncDirectoryTestService(t)
	testScheduler := &gitOpsSyncTestSchedulerInternal{}
	require.NoError(t, svc.SetScheduler(ctx, testScheduler, newGitOpsAdmissionGateForTestInternal(t)))

	missingProjectID := "missing-project"
	sync := &projectpkg.GitOpsSync{
		ID:            "sync-file-missing-bound-project",
		Name:          "demo-sync",
		EnvironmentID: "0",
		RepositoryID:  "repo-1",
		ComposePath:   "apps/demo/docker-compose.yaml",
		ProjectName:   "demo-project",
		ProjectID:     &missingProjectID,
		AutoSync:      true,
	}
	require.NoError(t, db.Create(sync).Error)

	result := &gitops.SyncResult{}
	project, err := svc.getOrCreateProjectInternal(ctx, sync, sync.ID, "services:\n  app:\n    image: nginx:alpine\n", nil, nil, "", result, common.User{})
	require.Error(t, err)
	require.ErrorIs(t, err, common.ErrGitOpsSyncProjectBindingBroken)
	require.Nil(t, project)

	var projectCount int64
	require.NoError(t, db.Model(&projectpkg.Project{}).Count(&projectCount).Error)
	assert.Zero(t, projectCount)

	_, statErr := os.Stat(filepath.Join(projectsDir, "demo-project"))
	require.ErrorIs(t, statErr, os.ErrNotExist)
	_, statErr = os.Stat(filepath.Join(projectsDir, "demo-project-1"))
	require.ErrorIs(t, statErr, os.ErrNotExist)

	var storedSync projectpkg.GitOpsSync
	require.NoError(t, db.First(&storedSync, "id = ?", sync.ID).Error)
	assert.False(t, storedSync.AutoSync)
	require.NotNil(t, storedSync.LastSyncStatus)
	assert.Equal(t, "failed", *storedSync.LastSyncStatus)
	require.NotNil(t, storedSync.LastSyncError)
	assert.Contains(t, *storedSync.LastSyncError, "project binding")
	assert.Contains(t, testScheduler.removed, entityjobs.GitOpsSyncJobPrefix+sync.ID)
}

// TestBuildSwarmStackDeployRequestInternal guards the Git Sync swarm deploy request.
// WithRegistryAuth must always be set: swarm tasks pull with only the auth embedded in
// the service spec, so a request without it makes every private image fail to pull.
func TestBuildSwarmStackDeployRequestInternal(t *testing.T) {
	t.Parallel()

	files := []swarmtypes.SyncFile{{RelativePath: "configs/app.conf", Content: []byte("k=v")}}

	cases := []struct {
		name            string
		sync            *projectpkg.GitOpsSync
		source          *preparedSyncSource
		overrideContent string
		envContent      string
		files           []swarmtypes.SyncFile
		want            swarmtypes.StackDeployRequest
	}{
		{
			name:            "populates all fields and enables registry auth",
			sync:            &projectpkg.GitOpsSync{ProjectName: "descent", ComposePath: "deploy/swarm/compose.yaml"},
			source:          &preparedSyncSource{repoPath: "/tmp/repo", composeContent: "services: {}\n"},
			overrideContent: "override",
			envContent:      "A=1",
			files:           files,
			want: swarmtypes.StackDeployRequest{
				Name:             "descent",
				ComposeContent:   "services: {}\n",
				OverrideContent:  "override",
				EnvContent:       "A=1",
				Files:            files,
				Prune:            true,
				WithRegistryAuth: true,
				WorkingDir:       filepath.Join("/tmp/repo", "deploy/swarm"),
			},
		},
		{
			name:   "enables registry auth with empty optional content and a root-level compose path",
			sync:   &projectpkg.GitOpsSync{ProjectName: "ptest", ComposePath: "compose.yaml"},
			source: &preparedSyncSource{repoPath: "/tmp/repo", composeContent: "services: {}\n"},
			want: swarmtypes.StackDeployRequest{
				Name:             "ptest",
				ComposeContent:   "services: {}\n",
				Prune:            true,
				WithRegistryAuth: true,
				WorkingDir:       "/tmp/repo",
			},
		},
		{
			name:   "passes an empty file list through unchanged",
			sync:   &projectpkg.GitOpsSync{ProjectName: "ptest", ComposePath: "stacks/compose.yaml"},
			source: &preparedSyncSource{repoPath: "/tmp/repo", composeContent: "services: {}\n"},
			// performSwarmStackSyncInternal always passes a non-nil slice; the builder must not reshape it.
			files: []swarmtypes.SyncFile{},
			want: swarmtypes.StackDeployRequest{
				Name:             "ptest",
				ComposeContent:   "services: {}\n",
				Files:            []swarmtypes.SyncFile{},
				Prune:            true,
				WithRegistryAuth: true,
				WorkingDir:       filepath.Join("/tmp/repo", "stacks"),
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := buildSwarmStackDeployRequestInternal(tc.sync, tc.source, tc.overrideContent, tc.envContent, tc.files)
			assert.Equal(t, tc.want, got)
		})
	}
}

func TestEnvContentChangedInternal(t *testing.T) {
	t.Run("ignores formatting-only changes", func(t *testing.T) {
		oldEnv := "B=2\nA=1\n# comment\n"
		newEnv := "A=1\nB=2\n"

		assert.False(t, envContentChangedInternal(oldEnv, newEnv))
	})

	t.Run("detects semantic changes", func(t *testing.T) {
		oldEnv := "A=1\nB=2\n"
		newEnv := "A=1\nB=3\n"

		assert.True(t, envContentChangedInternal(oldEnv, newEnv))
	})
}

func TestGitOpsSyncService_GetEnvironmentSyncLimits(t *testing.T) {
	ctx := context.Background()
	db := setupGitOpsProjectTestDBInternal(t)
	settingsSvc, err := newGitOpsSettingsServiceForTestInternal(t, ctx, db)
	require.NoError(t, err)

	require.NoError(t, settingsSvc.SetIntSetting(ctx, "gitSyncMaxFiles", 123))
	require.NoError(t, settingsSvc.SetIntSetting(ctx, "gitSyncMaxTotalSizeMb", 64))
	require.NoError(t, settingsSvc.SetIntSetting(ctx, "gitSyncMaxBinarySizeMb", 12))

	svc := &GitOpsSyncService{settingsService: settingsSvc}

	maxFiles, maxTotalSize, maxBinarySize := svc.getEnvironmentSyncLimits(ctx)

	require.Equal(t, 123, maxFiles)
	require.Equal(t, int64(64*1024*1024), maxTotalSize)
	require.Equal(t, int64(12*1024*1024), maxBinarySize)
}

func TestGitOpsSyncService_GetEffectiveSyncLimits(t *testing.T) {
	ctx := context.Background()
	t.Setenv("GIT_SYNC_MAX_FILES", "")
	t.Setenv("GIT_SYNC_MAX_TOTAL_SIZE_MB", "")
	t.Setenv("GIT_SYNC_MAX_BINARY_SIZE_MB", "")

	db := setupGitOpsProjectTestDBInternal(t)
	settingsSvc, err := newGitOpsSettingsServiceForTestInternal(t, ctx, db)
	require.NoError(t, err)

	require.NoError(t, settingsSvc.SetIntSetting(ctx, "gitSyncMaxFiles", 200))
	require.NoError(t, settingsSvc.SetIntSetting(ctx, "gitSyncMaxTotalSizeMb", 30))
	require.NoError(t, settingsSvc.SetIntSetting(ctx, "gitSyncMaxBinarySizeMb", 5))

	svc := &GitOpsSyncService{settingsService: settingsSvc}

	t.Run("preserves sync-specific limits when they exceed settings", func(t *testing.T) {
		sync := &projectpkg.GitOpsSync{
			MaxSyncFiles:      500,
			MaxSyncTotalSize:  50 * 1024 * 1024,
			MaxSyncBinarySize: 10 * 1024 * 1024,
		}

		maxFiles, maxTotalSize, maxBinarySize := svc.getEffectiveSyncLimits(ctx, sync)

		require.Equal(t, 500, maxFiles)
		require.Equal(t, int64(50*1024*1024), maxTotalSize)
		require.Equal(t, int64(10*1024*1024), maxBinarySize)
	})

	t.Run("preserves sync-specific limits when they are below settings", func(t *testing.T) {
		sync := &projectpkg.GitOpsSync{
			MaxSyncFiles:      75,
			MaxSyncTotalSize:  8 * 1024 * 1024,
			MaxSyncBinarySize: 2 * 1024 * 1024,
		}

		maxFiles, maxTotalSize, maxBinarySize := svc.getEffectiveSyncLimits(ctx, sync)

		require.Equal(t, 75, maxFiles)
		require.Equal(t, int64(8*1024*1024), maxTotalSize)
		require.Equal(t, int64(2*1024*1024), maxBinarySize)
	})

	t.Run("zero disables sync limits", func(t *testing.T) {
		sync := &projectpkg.GitOpsSync{
			MaxSyncFiles:      0,
			MaxSyncTotalSize:  0,
			MaxSyncBinarySize: 0,
		}

		maxFiles, maxTotalSize, maxBinarySize := svc.getEffectiveSyncLimits(ctx, sync)

		require.Equal(t, 0, maxFiles)
		require.Equal(t, int64(0), maxTotalSize)
		require.Equal(t, int64(0), maxBinarySize)
	})

	t.Run("environment variables override stored sync limits", func(t *testing.T) {
		t.Setenv("GIT_SYNC_MAX_FILES", "10000")
		t.Setenv("GIT_SYNC_MAX_TOTAL_SIZE_MB", "1024")
		t.Setenv("GIT_SYNC_MAX_BINARY_SIZE_MB", "12")
		settingsSvcEnv, svcErr := newGitOpsSettingsServiceForTestInternal(t, ctx, db)
		require.NoError(t, svcErr)
		svcEnv := &GitOpsSyncService{settingsService: settingsSvcEnv}

		sync := &projectpkg.GitOpsSync{
			MaxSyncFiles:      500,
			MaxSyncTotalSize:  50 * 1024 * 1024,
			MaxSyncBinarySize: 10 * 1024 * 1024,
		}

		maxFiles, maxTotalSize, maxBinarySize := svcEnv.getEffectiveSyncLimits(ctx, sync)

		require.Equal(t, 10000, maxFiles)
		require.Equal(t, int64(1024*1024*1024), maxTotalSize)
		require.Equal(t, int64(12*1024*1024), maxBinarySize)
	})

	t.Run("environment variable zero disables runtime caps", func(t *testing.T) {
		t.Setenv("GIT_SYNC_MAX_FILES", "0")
		t.Setenv("GIT_SYNC_MAX_TOTAL_SIZE_MB", "0")
		t.Setenv("GIT_SYNC_MAX_BINARY_SIZE_MB", "0")
		settingsSvcEnv, svcErr := newGitOpsSettingsServiceForTestInternal(t, ctx, db)
		require.NoError(t, svcErr)
		svcEnv := &GitOpsSyncService{settingsService: settingsSvcEnv}

		sync := &projectpkg.GitOpsSync{
			MaxSyncFiles:      75,
			MaxSyncTotalSize:  8 * 1024 * 1024,
			MaxSyncBinarySize: 2 * 1024 * 1024,
		}

		maxFiles, maxTotalSize, maxBinarySize := svcEnv.getEffectiveSyncLimits(ctx, sync)

		require.Equal(t, 0, maxFiles)
		require.Equal(t, int64(0), maxTotalSize)
		require.Equal(t, int64(0), maxBinarySize)
	})
}

// setupLifecycleValidationService builds a GitOpsSyncService with lifecycle
// hooks enabled in settings so the validator's gate doesn't short-circuit
// the rule checks under test.
func setupLifecycleValidationService(t *testing.T) (*GitOpsSyncService, context.Context) {
	t.Helper()
	ctx := context.Background()
	svc, _, _ := setupGitOpsSyncDirectoryTestService(t)
	require.NoError(t, svc.settingsService.SetStringSetting(ctx, "lifecycleEnabled", "true"))
	return svc, ctx
}

func TestValidateLifecycleConfig_AllNilNoError(t *testing.T) {
	svc, ctx := setupLifecycleValidationService(t)
	require.NoError(t, svc.validateLifecycleConfigInternal(ctx, nil, lifecycleConfigInputInternal{}))
}

func TestValidateLifecycleConfig_RejectsWhenGloballyDisabled(t *testing.T) {
	svc, _, _ := setupGitOpsSyncDirectoryTestService(t)
	ctx := context.Background()
	// lifecycleEnabled defaults to false; do not enable.
	err := svc.validateLifecycleConfigInternal(ctx, nil, lifecycleConfigInputInternal{
		scriptPath:  new("scripts/deploy.sh"),
		runnerImage: new("alpine:latest"),
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "disabled")
}

func TestValidateLifecycleConfig_RejectsAbsoluteScriptPath(t *testing.T) {
	svc, ctx := setupLifecycleValidationService(t)
	err := svc.validateLifecycleConfigInternal(ctx, nil, lifecycleConfigInputInternal{
		scriptPath:  new("/etc/passwd"),
		runnerImage: new("alpine:latest"),
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "relative")
}

func TestValidateLifecycleConfig_RejectsTraversalScriptPath(t *testing.T) {
	svc, ctx := setupLifecycleValidationService(t)
	err := svc.validateLifecycleConfigInternal(ctx, nil, lifecycleConfigInputInternal{
		scriptPath:  new("../outside.sh"),
		runnerImage: new("alpine:latest"),
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "escape")
}

func TestValidateLifecycleConfig_RejectsOverlongScriptPath(t *testing.T) {
	svc, ctx := setupLifecycleValidationService(t)
	long := make([]byte, 257)
	for i := range long {
		long[i] = 'a'
	}
	err := svc.validateLifecycleConfigInternal(ctx, nil, lifecycleConfigInputInternal{
		scriptPath:  new(string(long)),
		runnerImage: new("alpine:latest"),
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "256")
}

func TestValidateLifecycleConfig_RejectsScriptWithoutRunnerImageOnCreate(t *testing.T) {
	svc, ctx := setupLifecycleValidationService(t)
	require.NoError(t, svc.settingsService.SetStringSetting(ctx, "lifecycleDefaultRunnerImage", " "))
	err := svc.validateLifecycleConfigInternal(ctx, nil, lifecycleConfigInputInternal{
		scriptPath: new("scripts/deploy.sh"),
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "Runner image is required")
}

func TestValidateLifecycleConfig_AcceptsScriptWithDefaultRunnerImageOnCreate(t *testing.T) {
	svc, ctx := setupLifecycleValidationService(t)
	require.NoError(t, svc.settingsService.SetStringSetting(ctx, "lifecycleDefaultRunnerImage", "alpine:latest"))
	require.NoError(t, svc.validateLifecycleConfigInternal(ctx, nil, lifecycleConfigInputInternal{
		targetType:    new("project"),
		scriptPath:    new("scripts/deploy.sh"),
		syncDirectory: new(true),
	}))
}

func TestValidateLifecycleConfig_AcceptsScriptWithExistingRunnerImageOnUpdate(t *testing.T) {
	svc, ctx := setupLifecycleValidationService(t)
	existing := &projectpkg.GitOpsSync{SyncDirectory: true}
	existing.PreDeployRunnerImage = new("alpine:latest")
	require.NoError(t, svc.validateLifecycleConfigInternal(ctx, existing, lifecycleConfigInputInternal{
		scriptPath: new("scripts/deploy.sh"),
	}))
}

func TestValidateLifecycleConfig_RejectsTimeoutZeroOrNegative(t *testing.T) {
	svc, ctx := setupLifecycleValidationService(t)
	for _, v := range []int{0, -1, -3600} {
		err := svc.validateLifecycleConfigInternal(ctx, nil, lifecycleConfigInputInternal{
			timeoutSec: new(v),
		})
		require.Errorf(t, err, "expected error for timeoutSec=%d", v)
		require.Contains(t, err.Error(), "at least 1")
	}
}

func TestValidateLifecycleConfig_RejectsTimeoutAboveSettingCap(t *testing.T) {
	svc, ctx := setupLifecycleValidationService(t)
	require.NoError(t, svc.settingsService.SetStringSetting(ctx, "lifecycleMaxTimeoutSec", "120"))
	err := svc.validateLifecycleConfigInternal(ctx, nil, lifecycleConfigInputInternal{
		timeoutSec: new(300),
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "exceeds")
}

func TestValidateLifecycleConfig_RejectsInvalidEnvKey(t *testing.T) {
	svc, ctx := setupLifecycleValidationService(t)
	err := svc.validateLifecycleConfigInternal(ctx, nil, lifecycleConfigInputInternal{
		env: new("FOO-BAR=baz"),
	})
	require.Error(t, err)
}

func TestValidateLifecycleConfig_AcceptsValidEnv(t *testing.T) {
	svc, ctx := setupLifecycleValidationService(t)
	require.NoError(t, svc.validateLifecycleConfigInternal(ctx, nil, lifecycleConfigInputInternal{
		env: new("FOO=bar\nBAZ_2=qux"),
	}))
}

func TestValidateLifecycleConfig_RejectsRelativeMountSource(t *testing.T) {
	svc, ctx := setupLifecycleValidationService(t)
	err := svc.validateLifecycleConfigInternal(ctx, nil, lifecycleConfigInputInternal{
		extraMounts: new("relative/path:/in/container"),
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "absolute")
}

func TestValidateLifecycleConfig_RejectsRelativeMountTarget(t *testing.T) {
	svc, ctx := setupLifecycleValidationService(t)
	err := svc.validateLifecycleConfigInternal(ctx, nil, lifecycleConfigInputInternal{
		extraMounts: new("/host/path:relative/target"),
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "absolute")
}

func TestValidateLifecycleConfig_AllowsClearingScriptWithoutImage(t *testing.T) {
	svc, ctx := setupLifecycleValidationService(t)
	existing := &projectpkg.GitOpsSync{}
	existing.PreDeployScriptPath = new("scripts/old.sh")
	existing.PreDeployRunnerImage = new("alpine:latest")
	// User clears the script (empty string in update); image clear is implied not required.
	require.NoError(t, svc.validateLifecycleConfigInternal(ctx, existing, lifecycleConfigInputInternal{
		scriptPath: new(""),
	}))
}

func TestValidateLifecycleConfig_RejectsScriptWithoutSyncDirectoryOnCreate(t *testing.T) {
	svc, ctx := setupLifecycleValidationService(t)
	err := svc.validateLifecycleConfigInternal(ctx, nil, lifecycleConfigInputInternal{
		scriptPath:    new("scripts/deploy.sh"),
		runnerImage:   new("alpine:latest"),
		syncDirectory: new(false),
	})
	require.Error(t, err)
	require.ErrorIs(t, err, common.ErrValidation)
	require.Contains(t, errors.GetDetails(err), "preDeployScriptPath")
}

func TestValidateLifecycleConfig_AcceptsScriptWithSyncDirectoryOnCreate(t *testing.T) {
	svc, ctx := setupLifecycleValidationService(t)
	require.NoError(t, svc.validateLifecycleConfigInternal(ctx, nil, lifecycleConfigInputInternal{
		targetType:    new("project"),
		scriptPath:    new("scripts/deploy.sh"),
		runnerImage:   new("alpine:latest"),
		syncDirectory: new(true),
	}))
}

func TestValidateLifecycleConfig_RejectsLifecycleHookForSwarmStack(t *testing.T) {
	svc, ctx := setupLifecycleValidationService(t)
	err := svc.validateLifecycleConfigInternal(ctx, nil, lifecycleConfigInputInternal{
		targetType:    new("swarm_stack"),
		scriptPath:    new("scripts/deploy.sh"),
		runnerImage:   new("alpine:latest"),
		syncDirectory: new(true),
	})
	require.Error(t, err)
	require.ErrorIs(t, err, common.ErrValidation)
	require.Contains(t, errors.GetDetails(err), "preDeployScriptPath")
	require.Contains(t, err.Error(), "project syncs")
}

func TestValidateLifecycleConfig_RejectsSwarmTargetChangeWithExistingLifecycleHook(t *testing.T) {
	svc, ctx := setupLifecycleValidationService(t)
	existing := &projectpkg.GitOpsSync{TargetType: "project", SyncDirectory: true}
	existing.PreDeployScriptPath = new("scripts/deploy.sh")
	existing.PreDeployRunnerImage = new("alpine:latest")
	err := svc.validateLifecycleConfigInternal(ctx, existing, lifecycleConfigInputInternal{
		targetType: new("swarm_stack"),
	})
	require.Error(t, err)
	require.ErrorIs(t, err, common.ErrValidation)
	require.Contains(t, errors.GetDetails(err), "preDeployScriptPath")
	require.Contains(t, err.Error(), "project syncs")
}

func TestValidateLifecycleConfig_AcceptsScriptWhenExistingSyncHasSyncDirectory(t *testing.T) {
	svc, ctx := setupLifecycleValidationService(t)
	existing := &projectpkg.GitOpsSync{SyncDirectory: true}
	require.NoError(t, svc.validateLifecycleConfigInternal(ctx, existing, lifecycleConfigInputInternal{
		scriptPath:  new("scripts/deploy.sh"),
		runnerImage: new("alpine:latest"),
	}))
}

func TestValidateLifecycleConfig_RejectsSyncDirectoryToggleOffWhileScriptStillSet(t *testing.T) {
	svc, ctx := setupLifecycleValidationService(t)
	existing := &projectpkg.GitOpsSync{SyncDirectory: true}
	existing.PreDeployScriptPath = new("scripts/deploy.sh")
	existing.PreDeployRunnerImage = new("alpine:latest")
	// Admin toggles syncDirectory off without clearing the script — should be rejected.
	err := svc.validateLifecycleConfigInternal(ctx, existing, lifecycleConfigInputInternal{
		syncDirectory: new(false),
	})
	require.Error(t, err)
	require.ErrorIs(t, err, common.ErrValidation)
	require.Contains(t, errors.GetDetails(err), "preDeployScriptPath")
}

func TestRedeployAfterSyncFailedError_FormatAndUnwrap(t *testing.T) {
	cause := errors.New("pre-deploy hook bombed")
	err := common.Classify(common.ErrRedeployAfterSyncFailed, errors.WrapIf(cause, "redeploy failed"))

	require.Equal(t, "redeploy failed: pre-deploy hook bombed", err.Error())
	require.True(t, errors.Is(err, cause), "Unwrap should expose the cause for errors.Is")

	require.ErrorIs(t, err, common.ErrRedeployAfterSyncFailed)
}

func TestMarkSyncRedeployFailedInternal_PersistsErrorOnSyncRow(t *testing.T) {
	ctx := context.Background()
	svc, db, _ := setupGitOpsSyncDirectoryTestService(t)
	// Event logging requires a real event.EventService; the shared setup leaves it
	// nil since most tests don't exercise the event path.
	require.NoError(t, db.AutoMigrate(&event.Event{}))
	svc.eventService = event.NewEventService(db, config.Load(), nil)

	sync := &projectpkg.GitOpsSync{
		ID:            "sync-1",
		Name:          "redeploy-fail",
		EnvironmentID: "0",
		RepositoryID:  "repo-1",
		Branch:        "main",
		ComposePath:   "compose.yml",
		TargetType:    "project",
	}
	require.NoError(t, db.WithContext(ctx).Select("*").Omit("Environment", "Repository", "Project").Create(sync).Error)

	result := &gitops.SyncResult{Success: true}
	syncedFiles := []string{"compose.yml", "scripts/pre-deploy.sh"}
	hookErr := common.Classify(common.ErrRedeployAfterSyncFailed, errors.WrapIf(errors.New("pre-deploy hook failed: exit 1"), "redeploy failed"))

	svc.markSyncRedeployFailedInternal(ctx, sync, sync.ID, "abc123", syncedFiles, hookErr, common.User{ID: "user", Username: "tester"}, result)

	require.False(t, result.Success)
	require.NotNil(t, result.Error)
	require.Contains(t, *result.Error, "pre-deploy hook failed")

	var stored projectpkg.GitOpsSync
	require.NoError(t, db.WithContext(ctx).First(&stored, "id = ?", sync.ID).Error)
	require.NotNil(t, stored.LastSyncStatus)
	require.Equal(t, "failed", *stored.LastSyncStatus)
	require.NotNil(t, stored.LastSyncError)
	require.Contains(t, *stored.LastSyncError, "pre-deploy hook failed")
	// The synced-files list should still be populated so operators can see
	// what reached disk before the redeploy died.
	require.NotNil(t, stored.SyncedFiles)
	require.Contains(t, *stored.SyncedFiles, "compose.yml")
	require.Contains(t, *stored.SyncedFiles, "scripts/pre-deploy.sh")
}
