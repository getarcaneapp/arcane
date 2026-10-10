package gitops

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/getarcaneapp/arcane/types/v2/base"
	"github.com/getarcaneapp/arcane/types/v2/gitops"
	schedulertypes "github.com/getarcaneapp/arcane/types/v2/scheduler"
	"github.com/getarcaneapp/arcane/types/v2/user"
	"github.com/go-git/go-billy/v5/osfs"
	gogit "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing/transport/client"
	"github.com/go-git/go-git/v5/plumbing/transport/server"
	"github.com/libtnb/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.getarcane.app/kit/pkg"
	"gorm.io/gorm"

	"github.com/getarcaneapp/arcane/backend/v2/internal/activity"
	"github.com/getarcaneapp/arcane/backend/v2/internal/common"
	"github.com/getarcaneapp/arcane/backend/v2/internal/config"
	"github.com/getarcaneapp/arcane/backend/v2/internal/database"
	"github.com/getarcaneapp/arcane/backend/v2/internal/environment"
	"github.com/getarcaneapp/arcane/backend/v2/internal/event"
	"github.com/getarcaneapp/arcane/backend/v2/internal/gitops/children/backup"
	"github.com/getarcaneapp/arcane/backend/v2/internal/gitrepo"
	"github.com/getarcaneapp/arcane/backend/v2/internal/imageupdate"
	projectpkg "github.com/getarcaneapp/arcane/backend/v2/internal/project"
	"github.com/getarcaneapp/arcane/backend/v2/internal/settings"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/gitutil"
	activitylib "github.com/getarcaneapp/arcane/backend/v2/pkg/libarcane/activity"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/projects"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/scheduler/entityjobs"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/scheduler/flow/flowtest"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/scheduler/runs"
	francistest "github.com/getarcaneapp/arcane/backend/v2/pkg/utils/francis/testing"
)

func setupGitOpsProjectTestDB(t *testing.T) *database.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(1)
	require.NoError(t, db.AutoMigrate(&projectpkg.Project{}, &settings.SettingVariable{}, &imageupdate.ImageUpdateRecord{}, &event.Event{}))
	return &database.DB{DB: db}
}

func newGitOpsSettingsServiceForTest(t testing.TB, ctx context.Context, db *database.DB) (*settings.SettingsService, error) {
	t.Helper()
	service, err := settings.NewSettingsService(ctx, db)
	if err == nil {
		t.Cleanup(func() { require.NoError(t, service.Stop(context.WithoutCancel(t.Context()))) })
	}
	return service, err
}

func newGitOpsAdmissionGateForTest(t testing.TB) *runs.Admission {
	t.Helper()
	runtime := francistest.New(t)
	gate := runs.NewAdmission(runtime.Service(), t.Name())
	require.NoError(t, gate.Register(runtime))
	francistest.Start(t, runtime)
	return gate
}

func setupGitOpsSyncDirectoryTestService(t *testing.T) (*GitOpsSyncService, *database.DB, string) {
	t.Helper()

	ctx := t.Context()
	db := setupGitOpsProjectTestDB(t)
	require.NoError(t, db.AutoMigrate(&projectpkg.GitOpsSync{}))

	settingsService, err := newGitOpsSettingsServiceForTest(t, ctx, db)
	require.NoError(t, err)

	projectsDir := t.TempDir()
	require.NoError(t, settingsService.SetStringSetting(ctx, "projectsDirectory", projectsDir))

	eventService := event.NewEventService(db, config.Load(), nil)
	projectService := projectpkg.NewProjectService(db, settingsService, eventService, nil, nil, nil, nil, nil, config.Load(), nil, nil)

	return NewGitOpsSyncService(db, nil, projectService, nil, eventService, settingsService), db, projectsDir
}

func TestGitOpsSyncService_OverlappingSyncPreservesSuccessShapedSkip(t *testing.T) {
	gate := newGitOpsAdmissionGateForTest(t)
	key := schedulertypes.AdmissionKey{Scope: gitOpsSyncAdmissionScope, ID: "sync-id"}
	lease, admitted, err := gate.TryAcquire(t.Context(), key)
	require.NoError(t, err)
	require.True(t, admitted)

	service := &GitOpsSyncService{jobs: entityjobs.New(entityjobs.GitOpsSyncJobPrefix, gitOpsSyncAdmissionScope)}
	require.NoError(t, service.SetScheduler(t.Context(), &gitOpsSyncTestScheduler{}, gate))
	result, err := service.PerformSync(t.Context(), "0", "sync-id", user.Actor{})
	require.NoError(t, err)
	require.False(t, result.Success)
	require.Equal(t, "sync already in progress", result.Message)
	lease.Release(t.Context())
}

type gitOpsSyncTestScheduler struct {
	submitted []schedulertypes.Request
	added     []string
	removed   []string
}

func (s *gitOpsSyncTestScheduler) AddJob(_ context.Context, job schedulertypes.Job) error {
	s.added = append(s.added, job.Name())
	return nil
}

func (s *gitOpsSyncTestScheduler) RemoveJob(_ context.Context, name string) {
	s.removed = append(s.removed, name)
}

func (s *gitOpsSyncTestScheduler) HasJob(_ string) bool {
	return false
}

// setupGitOpsSyncRemoteTestService wires a sync service with a real
// scheduler registry over a bare remote "repo-1" whose main branch holds files.
func setupGitOpsSyncRemoteTestService(t *testing.T, files map[string]string) (*GitOpsSyncService, *database.DB, string, *gitOpsSyncTestScheduler) {
	t.Helper()
	installBackupTestTransport()

	ctx := t.Context()
	db := setupGitOpsProjectTestDB(t)
	require.NoError(t, db.AutoMigrate(&projectpkg.GitOpsSync{}, &gitrepo.GitRepository{}))

	settingsService, err := newGitOpsSettingsServiceForTest(t, ctx, db)
	require.NoError(t, err)

	projectsDir := t.TempDir()
	require.NoError(t, settingsService.SetStringSetting(ctx, "projectsDirectory", projectsDir))

	eventService := event.NewEventService(db, config.Load(), nil)
	projectService := projectpkg.NewProjectService(db, settingsService, eventService, nil, nil, nil, nil, nil, config.Load(), nil, nil)
	repoService := gitrepo.NewGitRepositoryService(db, t.TempDir(), eventService, settingsService)

	service := NewGitOpsSyncService(db, repoService, projectService, nil, eventService, settingsService)
	testScheduler := &gitOpsSyncTestScheduler{}
	require.NoError(t, service.SetScheduler(ctx, testScheduler, newGitOpsAdmissionGateForTest(t)))

	bare := filepath.Join(t.TempDir(), "sync.git")
	_, err = gogit.PlainInit(bare, true)
	require.NoError(t, err)
	repoURL := "http://localhost" + bare
	require.NoError(t, db.Create(&gitrepo.GitRepository{ID: "repo-1", Name: "sync-remote", URL: repoURL, AuthType: "none", Enabled: true}).Error)
	pushRemoteCommit(t, git.NewClient(t.TempDir()), repoURL, "seed", files)

	return service, db, projectsDir, testScheduler
}

// TestGitOpsSyncService_GetOrCreateProject_RefusesDuplicateOnNameCollision is the
// single-file-sync analogue of the directory refuse: a name collision on create is a
// broken binding, not a "-N" duplicate.
func TestGitOpsSyncService_GetOrCreateProject_RefusesDuplicateOnNameCollision(t *testing.T) {
	ctx := t.Context()
	svc, db, projectsDir, _ := setupGitOpsSyncRemoteTestService(t, map[string]string{"docker-compose.yaml": "services:\n  app:\n    image: nginx:alpine\n"})
	require.NoError(t, os.MkdirAll(filepath.Join(projectsDir, "Dozzle"), 0o755))

	syncRecord := &projectpkg.GitOpsSync{
		ID:            "sync-single-dup",
		Name:          "Dozzle",
		EnvironmentID: "0",
		RepositoryID:  "repo-1",
		Branch:        "main",
		ComposePath:   "docker-compose.yaml",
		ProjectName:   "Dozzle",
		AutoSync:      true,
		SyncInterval:  60,
	}
	require.NoError(t, db.Create(syncRecord).Error)

	_, err := svc.PerformSync(ctx, "0", syncRecord.ID, user.Actor{})
	require.ErrorIs(t, err, common.ErrGitOpsSyncProjectBindingBroken)

	_, statErr := os.Stat(filepath.Join(projectsDir, "Dozzle-1"))
	require.ErrorIs(t, statErr, os.ErrNotExist, "must not mint a -N duplicate")

	var got projectpkg.GitOpsSync
	require.NoError(t, db.Where("id = ?", syncRecord.ID).First(&got).Error)
	assert.False(t, got.AutoSync, "auto-sync should be disabled on broken binding")
}

func TestGitOpsSyncService_SyncProjectDirectory_FailsWhenBoundProjectMissing(t *testing.T) {
	ctx := t.Context()
	svc, db, projectsDir, testScheduler := setupGitOpsSyncRemoteTestService(t, map[string]string{"apps/demo/docker-compose.yaml": "services:\n  app:\n    image: nginx:alpine\n"})

	missingProjectID := "missing-project"
	syncRecord := &projectpkg.GitOpsSync{
		ID:            "sync-directory-missing-bound-project",
		Name:          "demo-sync",
		EnvironmentID: "0",
		RepositoryID:  "repo-1",
		Branch:        "main",
		ComposePath:   "apps/demo/docker-compose.yaml",
		ProjectName:   "demo-project",
		ProjectID:     &missingProjectID,
		SyncDirectory: true,
		AutoSync:      true,
	}
	require.NoError(t, db.Create(syncRecord).Error)

	result, err := svc.PerformSync(ctx, "0", syncRecord.ID, user.Actor{})
	require.Error(t, err)
	require.ErrorIs(t, err, common.ErrGitOpsSyncProjectBindingBroken)
	require.NotNil(t, result)
	assert.False(t, result.Success)
	assert.Equal(t, "GitOps project binding broken", result.Message)

	var projectCount int64
	require.NoError(t, db.Model(&projectpkg.Project{}).Count(&projectCount).Error)
	assert.Zero(t, projectCount)

	_, statErr := os.Stat(filepath.Join(projectsDir, "demo-project"))
	require.ErrorIs(t, statErr, os.ErrNotExist)

	var storedSync projectpkg.GitOpsSync
	require.NoError(t, db.First(&storedSync, "id = ?", syncRecord.ID).Error)
	assert.False(t, storedSync.AutoSync)
	require.NotNil(t, storedSync.LastSyncStatus)
	assert.Equal(t, "failed", *storedSync.LastSyncStatus)
	require.NotNil(t, storedSync.LastSyncError)
	assert.Contains(t, *storedSync.LastSyncError, "project binding")
	assert.Contains(t, testScheduler.removed, entityjobs.GitOpsSyncJobPrefix+syncRecord.ID)
}

func TestGitOpsSyncService_SyncProjectDirectory_DisablesAutoSyncWhenBoundProjectRecoveryAmbiguous(t *testing.T) {
	ctx := t.Context()
	svc, db, projectsDir, testScheduler := setupGitOpsSyncRemoteTestService(t, map[string]string{"apps/media/radarr.yaml": "services:\n  app:\n    image: lscr.io/linuxserver/radarr:latest\n"})

	for _, dirName := range []string{"Radarr-3", "Radarr-30"} {
		projectPath := filepath.Join(projectsDir, dirName)
		require.NoError(t, os.MkdirAll(projectPath, 0o755))
		require.NoError(t, os.WriteFile(filepath.Join(projectPath, "radarr.yaml"), []byte("services:\n  app:\n    image: lscr.io/linuxserver/radarr:latest\n"), 0o644))
	}

	missingProjectID := "missing-project"
	syncRecord := &projectpkg.GitOpsSync{
		ID:            "sync-directory-ambiguous-bound-project",
		Name:          "radarr-sync",
		EnvironmentID: "0",
		RepositoryID:  "repo-1",
		Branch:        "main",
		ComposePath:   "apps/media/radarr.yaml",
		ProjectName:   "Radarr",
		ProjectID:     &missingProjectID,
		SyncDirectory: true,
		AutoSync:      true,
	}
	require.NoError(t, db.Create(syncRecord).Error)

	result, err := svc.PerformSync(ctx, "0", syncRecord.ID, user.Actor{})
	require.Error(t, err)
	require.ErrorIs(t, err, common.ErrGitOpsSyncProjectBindingBroken)
	require.NotNil(t, result)
	assert.False(t, result.Success)
	assert.Equal(t, "GitOps project binding broken", result.Message)

	var projectCount int64
	require.NoError(t, db.Model(&projectpkg.Project{}).Count(&projectCount).Error)
	assert.Zero(t, projectCount)

	var storedSync projectpkg.GitOpsSync
	require.NoError(t, db.First(&storedSync, "id = ?", syncRecord.ID).Error)
	assert.False(t, storedSync.AutoSync)
	require.NotNil(t, storedSync.LastSyncStatus)
	assert.Equal(t, "failed", *storedSync.LastSyncStatus)
	require.NotNil(t, storedSync.LastSyncError)
	assert.Contains(t, *storedSync.LastSyncError, "multiple candidate project directories")
	assert.Contains(t, testScheduler.removed, entityjobs.GitOpsSyncJobPrefix+syncRecord.ID)
}

func TestGitOpsSyncService_GetOrCreateProject_FailsWhenBoundProjectMissing(t *testing.T) {
	ctx := t.Context()
	svc, db, projectsDir, testScheduler := setupGitOpsSyncRemoteTestService(t, map[string]string{"apps/demo/docker-compose.yaml": "services:\n  app:\n    image: nginx:alpine\n"})

	missingProjectID := "missing-project"
	syncRecord := &projectpkg.GitOpsSync{
		ID:            "sync-file-missing-bound-project",
		Name:          "demo-sync",
		EnvironmentID: "0",
		RepositoryID:  "repo-1",
		Branch:        "main",
		ComposePath:   "apps/demo/docker-compose.yaml",
		ProjectName:   "demo-project",
		ProjectID:     &missingProjectID,
		AutoSync:      true,
	}
	require.NoError(t, db.Create(syncRecord).Error)

	result, err := svc.PerformSync(ctx, "0", syncRecord.ID, user.Actor{})
	require.Error(t, err)
	require.ErrorIs(t, err, common.ErrGitOpsSyncProjectBindingBroken)
	require.NotNil(t, result)
	assert.False(t, result.Success)

	var projectCount int64
	require.NoError(t, db.Model(&projectpkg.Project{}).Count(&projectCount).Error)
	assert.Zero(t, projectCount)

	_, statErr := os.Stat(filepath.Join(projectsDir, "demo-project"))
	require.ErrorIs(t, statErr, os.ErrNotExist)
	_, statErr = os.Stat(filepath.Join(projectsDir, "demo-project-1"))
	require.ErrorIs(t, statErr, os.ErrNotExist)

	var storedSync projectpkg.GitOpsSync
	require.NoError(t, db.First(&storedSync, "id = ?", syncRecord.ID).Error)
	assert.False(t, storedSync.AutoSync)
	require.NotNil(t, storedSync.LastSyncStatus)
	assert.Equal(t, "failed", *storedSync.LastSyncStatus)
	require.NotNil(t, storedSync.LastSyncError)
	assert.Contains(t, *storedSync.LastSyncError, "project binding")
	assert.Contains(t, testScheduler.removed, entityjobs.GitOpsSyncJobPrefix+syncRecord.ID)
}

func TestGitOpsSyncService_GetSyncByID_ReturnsNotFoundError(t *testing.T) {
	ctx := t.Context()
	svc, _, _ := setupGitOpsSyncDirectoryTestService(t)

	_, err := svc.GetSyncByID(ctx, "0", "missing-sync")

	require.ErrorIs(t, err, common.ErrNotFound)
}

func TestGitOpsSyncService_CleanupOrphanedSyncsOnStartup_DeletesOnlyOrphans(t *testing.T) {
	ctx := t.Context()
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
	ctx := t.Context()
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

	scheduler := &gitOpsSyncTestScheduler{}
	require.NoError(t, svc.SetScheduler(ctx, scheduler, newGitOpsAdmissionGateForTest(t)))
	svc.RegisterAutoSyncJobsOnStartup(ctx)

	require.Equal(t, []string{entityjobs.GitOpsSyncJobPrefix + "sync-live"}, scheduler.added)
	require.Empty(t, scheduler.removed)
}

func TestGitOpsSyncService_DeleteSync_DeletesStaleProjectReference(t *testing.T) {
	ctx := t.Context()
	svc, db, _ := setupGitOpsSyncDirectoryTestService(t)
	missingProjectID := "missing-project"

	syncRecord := &projectpkg.GitOpsSync{
		ID:            "sync-delete-stale-project",
		Name:          "demo-sync",
		EnvironmentID: "0",
		RepositoryID:  "repo-1",
		ComposePath:   "apps/demo/docker-compose.yaml",
		ProjectName:   "demo-project",
		ProjectID:     &missingProjectID,
		SyncInterval:  60,
	}
	require.NoError(t, db.Create(syncRecord).Error)

	require.NoError(t, svc.DeleteSync(ctx, "0", syncRecord.ID, user.Actor{}))

	var count int64
	require.NoError(t, db.Model(&projectpkg.GitOpsSync{}).Where("id = ?", syncRecord.ID).Count(&count).Error)
	assert.Zero(t, count)
}

// TestGitOpsSyncService_DeleteSync_SucceedsWhenEnvironmentMismatched proves a
// corrupt/env-mismatched sync is still deletable via the API: the request env ("0")
// does not match the row's env ("5"), yet the delete must succeed and stop the job.
func TestGitOpsSyncService_DeleteSync_SucceedsWhenEnvironmentMismatched(t *testing.T) {
	ctx := t.Context()
	svc, db, _ := setupGitOpsSyncDirectoryTestService(t)
	scheduler := &gitOpsSyncTestScheduler{}
	require.NoError(t, svc.SetScheduler(ctx, scheduler, newGitOpsAdmissionGateForTest(t)))

	syncRecord := &projectpkg.GitOpsSync{
		ID:            "sync-env-mismatch",
		Name:          "corrupt-sync",
		EnvironmentID: "5",
		ProjectName:   "demo-project",
		SyncInterval:  60,
	}
	require.NoError(t, db.Create(syncRecord).Error)

	require.NoError(t, svc.DeleteSync(ctx, "0", syncRecord.ID, user.Actor{}))

	var count int64
	require.NoError(t, db.Model(&projectpkg.GitOpsSync{}).Where("id = ?", syncRecord.ID).Count(&count).Error)
	assert.Zero(t, count)
	assert.Contains(t, scheduler.removed, entityjobs.GitOpsSyncJobPrefix+syncRecord.ID)
}

// TestGitOpsSyncService_DeleteSync_ClearsOrphanedManagedFlag verifies the managed
// flag is cleared keyed on the sync id even when the sync's ProjectID is nil.
func TestGitOpsSyncService_DeleteSync_ClearsOrphanedManagedFlag(t *testing.T) {
	ctx := t.Context()
	svc, db, _ := setupGitOpsSyncDirectoryTestService(t)

	syncID := "sync-orphan-flag"
	syncRecord := &projectpkg.GitOpsSync{
		ID:            syncID,
		Name:          "demo-sync",
		EnvironmentID: "0",
		ProjectName:   "demo-project",
		SyncInterval:  60,
	}
	require.NoError(t, db.Create(syncRecord).Error)

	managed := &projectpkg.Project{
		ID:              "proj-managed",
		Name:            "managed",
		Path:            filepath.Join(t.TempDir(), "managed"),
		GitOpsManagedBy: &syncID,
	}
	require.NoError(t, db.Create(managed).Error)

	require.NoError(t, svc.DeleteSync(ctx, "0", syncID, user.Actor{}))

	var got projectpkg.Project
	require.NoError(t, db.Where("id = ?", managed.ID).First(&got).Error)
	assert.Nil(t, got.GitOpsManagedBy)
}

// TestGitOpsSyncWorkflow_UnregistersMissingSync verifies a scheduled run whose row no longer exists
// (e.g. deleted out-of-band via raw SQL) unregisters its own job instead of firing forever.
func TestGitOpsSyncWorkflow_UnregistersMissingSync(t *testing.T) {
	ctx := t.Context()
	svc, db, _ := setupGitOpsSyncDirectoryTestService(t)
	require.NoError(t, db.AutoMigrate(&activity.Activity{}, &activity.ActivityMessage{}))
	harness := flowtest.New(t, activity.NewActivityService(db, nil))
	require.NoError(t, svc.RegisterWorkflows(harness.Engine))
	harness.Start(t)
	flowtest.AssertDefinitions(t, harness)
	scheduler := &gitOpsSyncTestScheduler{}
	require.NoError(t, svc.SetScheduler(ctx, scheduler, newGitOpsAdmissionGateForTest(t)))

	outcome, err := harness.Engine.Run(ctx, svc.syncWorkflow, syncTarget{EnvironmentID: "0", SyncID: "ghost-sync"}, activitylib.StartRequest{}, nil)
	require.NoError(t, err)
	assert.Equal(t, schedulertypes.Skipped, outcome.Status)
	assert.Contains(t, scheduler.removed, entityjobs.GitOpsSyncJobPrefix+"ghost-sync")
}

// TestGitOpsSyncService_CleanupLeakedScratchDirsOnStartup_RemovesOrphans verifies the
// startup sweep removes leaked gitops scratch dirs (hidden and legacy name-embedded
// forms) while leaving real project directories untouched.
func TestGitOpsSyncService_CleanupLeakedScratchDirsOnStartup_RemovesOrphans(t *testing.T) {
	ctx := t.Context()
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
	ctx := t.Context()
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

	require.NoError(t, svc.CleanupLeakedCloneDirsOnStartup(t.Context()))
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

func TestGitOpsSyncService_ReconcileDirectorySyncProjectsOnStartup_SkipsAmbiguousDuplicates(t *testing.T) {
	ctx := t.Context()
	svc, db, projectsDir := setupGitOpsSyncDirectoryTestService(t)

	for _, dirName := range []string{"Radarr-3", "Radarr-30"} {
		projectPath := filepath.Join(projectsDir, dirName)
		require.NoError(t, os.MkdirAll(projectPath, 0o755))
		require.NoError(t, os.WriteFile(filepath.Join(projectPath, "radarr.yaml"), []byte("services:\n  app:\n    image: lscr.io/linuxserver/radarr:latest\n"), 0o644))
	}

	missingProjectID := "missing-project"
	syncRecord := &projectpkg.GitOpsSync{
		ID:            "sync-directory-ambiguous",
		Name:          "radarr-sync",
		EnvironmentID: "0",
		RepositoryID:  "repo-1",
		ComposePath:   "apps/media/radarr.yaml",
		ProjectName:   "Radarr",
		ProjectID:     &missingProjectID,
		SyncDirectory: true,
	}
	require.NoError(t, db.Create(syncRecord).Error)

	require.NoError(t, svc.ReconcileDirectorySyncProjectsOnStartup(ctx))

	var projectsCount int64
	require.NoError(t, db.Model(&projectpkg.Project{}).Count(&projectsCount).Error)
	assert.Zero(t, projectsCount)

	var storedSync projectpkg.GitOpsSync
	require.NoError(t, db.First(&storedSync, "id = ?", syncRecord.ID).Error)
	require.NotNil(t, storedSync.ProjectID)
	assert.Equal(t, "missing-project", *storedSync.ProjectID)
}

func TestGitOpsSyncService_GetEnvironmentSyncLimits(t *testing.T) {
	ctx := t.Context()
	db := setupGitOpsProjectTestDB(t)
	settingsSvc, err := newGitOpsSettingsServiceForTest(t, ctx, db)
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
	ctx := t.Context()
	t.Setenv("GIT_SYNC_MAX_FILES", "")
	t.Setenv("GIT_SYNC_MAX_TOTAL_SIZE_MB", "")
	t.Setenv("GIT_SYNC_MAX_BINARY_SIZE_MB", "")

	db := setupGitOpsProjectTestDB(t)
	settingsSvc, err := newGitOpsSettingsServiceForTest(t, ctx, db)
	require.NoError(t, err)

	require.NoError(t, settingsSvc.SetIntSetting(ctx, "gitSyncMaxFiles", 200))
	require.NoError(t, settingsSvc.SetIntSetting(ctx, "gitSyncMaxTotalSizeMb", 30))
	require.NoError(t, settingsSvc.SetIntSetting(ctx, "gitSyncMaxBinarySizeMb", 5))

	svc := &GitOpsSyncService{settingsService: settingsSvc}

	t.Run("preserves sync-specific limits when they exceed settings", func(t *testing.T) {
		syncRecord := &projectpkg.GitOpsSync{
			MaxSyncFiles:      500,
			MaxSyncTotalSize:  50 * 1024 * 1024,
			MaxSyncBinarySize: 10 * 1024 * 1024,
		}

		maxFiles, maxTotalSize, maxBinarySize := svc.getEffectiveSyncLimits(ctx, syncRecord)

		require.Equal(t, 500, maxFiles)
		require.Equal(t, int64(50*1024*1024), maxTotalSize)
		require.Equal(t, int64(10*1024*1024), maxBinarySize)
	})

	t.Run("preserves sync-specific limits when they are below settings", func(t *testing.T) {
		syncRecord := &projectpkg.GitOpsSync{
			MaxSyncFiles:      75,
			MaxSyncTotalSize:  8 * 1024 * 1024,
			MaxSyncBinarySize: 2 * 1024 * 1024,
		}

		maxFiles, maxTotalSize, maxBinarySize := svc.getEffectiveSyncLimits(ctx, syncRecord)

		require.Equal(t, 75, maxFiles)
		require.Equal(t, int64(8*1024*1024), maxTotalSize)
		require.Equal(t, int64(2*1024*1024), maxBinarySize)
	})

	t.Run("zero disables sync limits", func(t *testing.T) {
		syncRecord := &projectpkg.GitOpsSync{
			MaxSyncFiles:      0,
			MaxSyncTotalSize:  0,
			MaxSyncBinarySize: 0,
		}

		maxFiles, maxTotalSize, maxBinarySize := svc.getEffectiveSyncLimits(ctx, syncRecord)

		require.Equal(t, 0, maxFiles)
		require.Equal(t, int64(0), maxTotalSize)
		require.Equal(t, int64(0), maxBinarySize)
	})

	t.Run("environment variables override stored sync limits", func(t *testing.T) {
		t.Setenv("GIT_SYNC_MAX_FILES", "10000")
		t.Setenv("GIT_SYNC_MAX_TOTAL_SIZE_MB", "1024")
		t.Setenv("GIT_SYNC_MAX_BINARY_SIZE_MB", "12")
		settingsSvcEnv, svcErr := newGitOpsSettingsServiceForTest(t, ctx, db)
		require.NoError(t, svcErr)
		svcEnv := &GitOpsSyncService{settingsService: settingsSvcEnv}

		syncRecord := &projectpkg.GitOpsSync{
			MaxSyncFiles:      500,
			MaxSyncTotalSize:  50 * 1024 * 1024,
			MaxSyncBinarySize: 10 * 1024 * 1024,
		}

		maxFiles, maxTotalSize, maxBinarySize := svcEnv.getEffectiveSyncLimits(ctx, syncRecord)

		require.Equal(t, 10000, maxFiles)
		require.Equal(t, int64(1024*1024*1024), maxTotalSize)
		require.Equal(t, int64(12*1024*1024), maxBinarySize)
	})

	t.Run("environment variable zero disables runtime caps", func(t *testing.T) {
		t.Setenv("GIT_SYNC_MAX_FILES", "0")
		t.Setenv("GIT_SYNC_MAX_TOTAL_SIZE_MB", "0")
		t.Setenv("GIT_SYNC_MAX_BINARY_SIZE_MB", "0")
		settingsSvcEnv, svcErr := newGitOpsSettingsServiceForTest(t, ctx, db)
		require.NoError(t, svcErr)
		svcEnv := &GitOpsSyncService{settingsService: settingsSvcEnv}

		syncRecord := &projectpkg.GitOpsSync{
			MaxSyncFiles:      75,
			MaxSyncTotalSize:  8 * 1024 * 1024,
			MaxSyncBinarySize: 2 * 1024 * 1024,
		}

		maxFiles, maxTotalSize, maxBinarySize := svcEnv.getEffectiveSyncLimits(ctx, syncRecord)

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
	ctx := t.Context()
	svc, _, _ := setupGitOpsSyncDirectoryTestService(t)
	require.NoError(t, svc.settingsService.SetStringSetting(ctx, "lifecycleEnabled", "true"))
	return svc, ctx
}

func TestValidateLifecycleConfig_AllNilNoError(t *testing.T) {
	svc, ctx := setupLifecycleValidationService(t)
	require.NoError(t, svc.validateLifecycleConfig(ctx, nil, lifecycleConfigInput{}))
}

func TestValidateLifecycleConfig_RejectsWhenGloballyDisabled(t *testing.T) {
	svc, _, _ := setupGitOpsSyncDirectoryTestService(t)
	ctx := t.Context()
	// lifecycleEnabled defaults to false; do not enable.
	err := svc.validateLifecycleConfig(ctx, nil, lifecycleConfigInput{
		scriptPath:  new("scripts/deploy.sh"),
		runnerImage: new("alpine:latest"),
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "disabled")
}

func TestValidateLifecycleConfig_RejectsAbsoluteScriptPath(t *testing.T) {
	svc, ctx := setupLifecycleValidationService(t)
	err := svc.validateLifecycleConfig(ctx, nil, lifecycleConfigInput{
		scriptPath:  new("/etc/passwd"),
		runnerImage: new("alpine:latest"),
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "relative")
}

func TestValidateLifecycleConfig_RejectsTraversalScriptPath(t *testing.T) {
	svc, ctx := setupLifecycleValidationService(t)
	err := svc.validateLifecycleConfig(ctx, nil, lifecycleConfigInput{
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
	err := svc.validateLifecycleConfig(ctx, nil, lifecycleConfigInput{
		scriptPath:  new(string(long)),
		runnerImage: new("alpine:latest"),
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "256")
}

func TestValidateLifecycleConfig_RejectsScriptWithoutRunnerImageOnCreate(t *testing.T) {
	svc, ctx := setupLifecycleValidationService(t)
	require.NoError(t, svc.settingsService.SetStringSetting(ctx, "lifecycleDefaultRunnerImage", " "))
	err := svc.validateLifecycleConfig(ctx, nil, lifecycleConfigInput{
		scriptPath: new("scripts/deploy.sh"),
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "Runner image is required")
}

func TestValidateLifecycleConfig_AcceptsScriptWithDefaultRunnerImageOnCreate(t *testing.T) {
	svc, ctx := setupLifecycleValidationService(t)
	require.NoError(t, svc.settingsService.SetStringSetting(ctx, "lifecycleDefaultRunnerImage", "alpine:latest"))
	require.NoError(t, svc.validateLifecycleConfig(ctx, nil, lifecycleConfigInput{
		targetType:    new("project"),
		scriptPath:    new("scripts/deploy.sh"),
		syncDirectory: new(true),
	}))
}

func TestValidateLifecycleConfig_AcceptsScriptWithExistingRunnerImageOnUpdate(t *testing.T) {
	svc, ctx := setupLifecycleValidationService(t)
	existing := &projectpkg.GitOpsSync{SyncDirectory: true}
	existing.PreDeployRunnerImage = new("alpine:latest")
	require.NoError(t, svc.validateLifecycleConfig(ctx, existing, lifecycleConfigInput{
		scriptPath: new("scripts/deploy.sh"),
	}))
}

func TestValidateLifecycleConfig_RejectsTimeoutZeroOrNegative(t *testing.T) {
	svc, ctx := setupLifecycleValidationService(t)
	for _, v := range []int{0, -1, -3600} {
		err := svc.validateLifecycleConfig(ctx, nil, lifecycleConfigInput{
			timeoutSec: new(v),
		})
		require.Errorf(t, err, "expected error for timeoutSec=%d", v)
		require.Contains(t, err.Error(), "at least 1")
	}
}

func TestValidateLifecycleConfig_RejectsTimeoutAboveSettingCap(t *testing.T) {
	svc, ctx := setupLifecycleValidationService(t)
	require.NoError(t, svc.settingsService.SetStringSetting(ctx, "lifecycleMaxTimeoutSec", "120"))
	err := svc.validateLifecycleConfig(ctx, nil, lifecycleConfigInput{
		timeoutSec: new(300),
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "exceeds")
}

func TestValidateLifecycleConfig_RejectsInvalidEnvKey(t *testing.T) {
	svc, ctx := setupLifecycleValidationService(t)
	err := svc.validateLifecycleConfig(ctx, nil, lifecycleConfigInput{
		env: new("FOO-BAR=baz"),
	})
	require.Error(t, err)
}

func TestValidateLifecycleConfig_AcceptsValidEnv(t *testing.T) {
	svc, ctx := setupLifecycleValidationService(t)
	require.NoError(t, svc.validateLifecycleConfig(ctx, nil, lifecycleConfigInput{
		env: new("FOO=bar\nBAZ_2=qux"),
	}))
}

func TestValidateLifecycleConfig_RejectsRelativeMountSource(t *testing.T) {
	svc, ctx := setupLifecycleValidationService(t)
	err := svc.validateLifecycleConfig(ctx, nil, lifecycleConfigInput{
		extraMounts: new("relative/path:/in/container"),
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "absolute")
}

func TestValidateLifecycleConfig_RejectsRelativeMountTarget(t *testing.T) {
	svc, ctx := setupLifecycleValidationService(t)
	err := svc.validateLifecycleConfig(ctx, nil, lifecycleConfigInput{
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
	require.NoError(t, svc.validateLifecycleConfig(ctx, existing, lifecycleConfigInput{
		scriptPath: new(""),
	}))
}

func TestValidateLifecycleConfig_RejectsScriptWithoutSyncDirectoryOnCreate(t *testing.T) {
	svc, ctx := setupLifecycleValidationService(t)
	err := svc.validateLifecycleConfig(ctx, nil, lifecycleConfigInput{
		scriptPath:    new("scripts/deploy.sh"),
		runnerImage:   new("alpine:latest"),
		syncDirectory: new(false),
	})
	require.Error(t, err)
	require.ErrorIs(t, err, common.ErrValidation)
	fieldErr, ok := errors.AsType[*base.FieldError](err)
	require.True(t, ok)
	require.Equal(t, "preDeployScriptPath", fieldErr.Field)
}

func TestValidateLifecycleConfig_AcceptsScriptWithSyncDirectoryOnCreate(t *testing.T) {
	svc, ctx := setupLifecycleValidationService(t)
	require.NoError(t, svc.validateLifecycleConfig(ctx, nil, lifecycleConfigInput{
		targetType:    new("project"),
		scriptPath:    new("scripts/deploy.sh"),
		runnerImage:   new("alpine:latest"),
		syncDirectory: new(true),
	}))
}

func TestValidateLifecycleConfig_RejectsLifecycleHookForSwarmStack(t *testing.T) {
	svc, ctx := setupLifecycleValidationService(t)
	err := svc.validateLifecycleConfig(ctx, nil, lifecycleConfigInput{
		targetType:    new("swarm_stack"),
		scriptPath:    new("scripts/deploy.sh"),
		runnerImage:   new("alpine:latest"),
		syncDirectory: new(true),
	})
	require.Error(t, err)
	require.ErrorIs(t, err, common.ErrValidation)
	fieldErr, ok := errors.AsType[*base.FieldError](err)
	require.True(t, ok)
	require.Equal(t, "preDeployScriptPath", fieldErr.Field)
	require.Contains(t, err.Error(), "project syncs")
}

func TestValidateLifecycleConfig_RejectsSwarmTargetChangeWithExistingLifecycleHook(t *testing.T) {
	svc, ctx := setupLifecycleValidationService(t)
	existing := &projectpkg.GitOpsSync{TargetType: "project", SyncDirectory: true}
	existing.PreDeployScriptPath = new("scripts/deploy.sh")
	existing.PreDeployRunnerImage = new("alpine:latest")
	err := svc.validateLifecycleConfig(ctx, existing, lifecycleConfigInput{
		targetType: new("swarm_stack"),
	})
	require.Error(t, err)
	require.ErrorIs(t, err, common.ErrValidation)
	fieldErr, ok := errors.AsType[*base.FieldError](err)
	require.True(t, ok)
	require.Equal(t, "preDeployScriptPath", fieldErr.Field)
	require.Contains(t, err.Error(), "project syncs")
}

func TestValidateLifecycleConfig_AcceptsScriptWhenExistingSyncHasSyncDirectory(t *testing.T) {
	svc, ctx := setupLifecycleValidationService(t)
	existing := &projectpkg.GitOpsSync{SyncDirectory: true}
	require.NoError(t, svc.validateLifecycleConfig(ctx, existing, lifecycleConfigInput{
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
	err := svc.validateLifecycleConfig(ctx, existing, lifecycleConfigInput{
		syncDirectory: new(false),
	})
	require.Error(t, err)
	require.ErrorIs(t, err, common.ErrValidation)
	fieldErr, ok := errors.AsType[*base.FieldError](err)
	require.True(t, ok)
	require.Equal(t, "preDeployScriptPath", fieldErr.Field)
}

func TestRedeployAfterSyncFailedError_FormatAndUnwrap(t *testing.T) {
	cause := errors.New("pre-deploy hook bombed")
	err := common.Classify(common.ErrRedeployAfterSyncFailed, fmt.Errorf("redeploy failed: %w", cause))

	require.Equal(t, "redeploy failed: pre-deploy hook bombed", err.Error())
	require.ErrorIs(t, err, cause, "Unwrap should expose the cause for errors.Is")

	require.ErrorIs(t, err, common.ErrRedeployAfterSyncFailed)
}

func (s *gitOpsSyncTestScheduler) Submit(_ context.Context, request schedulertypes.Request) (schedulertypes.Run, error) {
	s.submitted = append(s.submitted, request)
	return schedulertypes.Run{ID: request.RunID, JobID: request.JobID, EnvironmentID: request.EnvironmentID, Status: schedulertypes.Queued}, nil
}

var installBackupTestTransportOnce sync.Once

// installBackupTestTransport serves bare repositories on disk over the
// "http" scheme so backups push without a network or the git binary.
func installBackupTestTransport() {
	installBackupTestTransportOnce.Do(func() {
		client.InstallProtocol("http", server.NewClient(server.NewFilesystemLoader(osfs.New("/"))))
	})
}

// backupTestEnv is one wired-up backup fixture: a bare remote, a git
// repository row, a project directory and the sync service under test.
type backupTestEnv struct {
	service     *GitOpsSyncService
	db          *database.DB
	scheduler   *gitOpsBackupTestScheduler
	projectsDir string
	projectPath string
	project     *projectpkg.Project
	repoURL     string
	remote      *git.Client
}

// gitOpsBackupTestScheduler runs a submitted job body inline so the
// save-debounce path reaches PerformSync the way the real scheduler does.
type gitOpsBackupTestScheduler struct {
	mu        sync.Mutex
	jobs      map[string]schedulertypes.Job
	submitted []schedulertypes.Request
}

func (s *gitOpsBackupTestScheduler) AddJob(_ context.Context, job schedulertypes.Job) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.jobs == nil {
		s.jobs = make(map[string]schedulertypes.Job)
	}
	s.jobs[job.Name()] = job
	return nil
}

func (s *gitOpsBackupTestScheduler) RemoveJob(_ context.Context, name string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.jobs, name)
}

func (s *gitOpsBackupTestScheduler) HasJob(name string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, ok := s.jobs[name]
	return ok
}

func (s *gitOpsBackupTestScheduler) Submit(ctx context.Context, request schedulertypes.Request) (schedulertypes.Run, error) {
	s.mu.Lock()
	s.submitted = append(s.submitted, request)
	job := s.jobs[request.JobID]
	s.mu.Unlock()
	if job != nil {
		_, _ = job.Run(ctx)
	}
	return schedulertypes.Run{ID: request.RunID, JobID: request.JobID, EnvironmentID: request.EnvironmentID, Status: schedulertypes.Queued}, nil
}

func (s *gitOpsBackupTestScheduler) submitCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.submitted)
}

func setupGitOpsBackupTestService(t *testing.T) *backupTestEnv {
	t.Helper()
	installBackupTestTransport()

	ctx := t.Context()
	db := setupGitOpsProjectTestDB(t)
	require.NoError(t, db.AutoMigrate(&projectpkg.GitOpsSync{}, &projectpkg.ProjectTag{}, &gitrepo.GitRepository{}, &environment.Environment{}, &activity.Activity{}, &activity.ActivityMessage{}))

	settingsService, err := newGitOpsSettingsServiceForTest(t, ctx, db)
	require.NoError(t, err)

	projectsDir := t.TempDir()
	require.NoError(t, settingsService.SetStringSetting(ctx, "projectsDirectory", projectsDir))

	eventService := event.NewEventService(db, config.Load(), nil)
	projectService := projectpkg.NewProjectService(db, settingsService, eventService, nil, nil, nil, nil, nil, config.Load(), nil, nil)
	repoService := gitrepo.NewGitRepositoryService(db, t.TempDir(), eventService, settingsService)

	service := NewGitOpsSyncService(db, repoService, projectService, nil, eventService, settingsService)
	// The test scheduler runs submitted jobs inline, which runs the sync workflow.
	harness := flowtest.New(t, activity.NewActivityService(db, nil))
	require.NoError(t, service.RegisterWorkflows(harness.Engine))
	harness.Start(t)
	scheduler := &gitOpsBackupTestScheduler{}
	require.NoError(t, service.SetScheduler(ctx, scheduler, newGitOpsAdmissionGateForTest(t)))

	bare := filepath.Join(t.TempDir(), "backups.git")
	_, err = gogit.PlainInit(bare, true)
	require.NoError(t, err)
	repoURL := "http://localhost" + bare

	require.NoError(t, db.Create(&gitrepo.GitRepository{
		ID:       "repo-backup",
		Name:     "backup-remote",
		URL:      repoURL,
		AuthType: "none",
		Enabled:  true,
	}).Error)

	projectPath := filepath.Join(projectsDir, "demo-project")
	require.NoError(t, os.MkdirAll(projectPath, 0o755))
	writeBackupProjectFile(t, projectPath, "compose.yaml", "services:\n  app:\n    image: nginx:1.27-alpine\n")

	project := &projectpkg.Project{
		ID:      "proj-backup",
		Name:    "demo-project",
		DirName: new("demo-project"),
		Path:    projectPath,
		Status:  projectpkg.ProjectStatusStopped,
	}
	require.NoError(t, db.Create(project).Error)

	return &backupTestEnv{
		service:     service,
		db:          db,
		scheduler:   scheduler,
		projectsDir: projectsDir,
		projectPath: projectPath,
		project:     project,
		repoURL:     repoURL,
		remote:      git.NewClient(t.TempDir()),
	}
}

func writeBackupProjectFile(t *testing.T, root, relative, content string) {
	t.Helper()
	target := filepath.Join(root, filepath.FromSlash(relative))
	require.NoError(t, os.MkdirAll(filepath.Dir(target), 0o755))
	require.NoError(t, os.WriteFile(target, []byte(content), 0o644))
}

func (e *backupTestEnv) createBackup(t *testing.T, req gitops.CreateSyncRequest) *projectpkg.GitOpsSync {
	t.Helper()
	req.Name = cmp.Or(req.Name, "demo-backup")
	req.RepositoryID = cmp.Or(req.RepositoryID, "repo-backup")
	req.Branch = cmp.Or(req.Branch, "main")
	req.Mode = cmp.Or(req.Mode, gitops.SyncModeBackup)
	if req.ProjectID == "" {
		req.ProjectID = e.project.ID
	}
	req.BackupDirectory = cmp.Or(req.BackupDirectory, "backups/demo")
	syncRecord, err := e.service.CreateSync(t.Context(), "0", req, user.SystemUser)
	require.NoError(t, err)
	return syncRecord
}

func (e *backupTestEnv) reload(t *testing.T, id string) *projectpkg.GitOpsSync {
	t.Helper()
	var syncRecord projectpkg.GitOpsSync
	require.NoError(t, e.db.Where("id = ?", id).First(&syncRecord).Error)
	return &syncRecord
}

// checkoutRemote clones the backup branch so the pushed tree can be inspected.
func (e *backupTestEnv) checkoutRemote(t *testing.T) string {
	t.Helper()
	repoPath, err := e.remote.Clone(t.Context(), e.repoURL, "main", git.AuthConfig{AuthType: "none"}, 0)
	require.NoError(t, err)
	t.Cleanup(func() { _ = e.remote.Cleanup(repoPath) })
	return repoPath
}

func (e *backupTestEnv) remoteHead(t *testing.T) string {
	t.Helper()
	head, exists, err := e.remote.RemoteBranchHead(t.Context(), e.repoURL, "main", git.AuthConfig{AuthType: "none"})
	require.NoError(t, err)
	require.True(t, exists)
	return head
}

// pushRemoteCommit commits directly to the branch as another writer.
func pushRemoteCommit(t *testing.T, remote *git.Client, repoURL, message string, files map[string]string) {
	t.Helper()
	auth := git.AuthConfig{AuthType: "none"}
	checkout, err := remote.CheckoutForWrite(t.Context(), repoURL, "main", auth)
	require.NoError(t, err)
	defer func() { _ = remote.Cleanup(checkout.RepoPath) }()

	request := git.CommitRequest{Message: message, AuthorName: "Other", AuthorEmail: "other@localhost"}
	for path, content := range files {
		request.Files = append(request.Files, git.CommitFile{Path: path, Content: []byte(content)})
	}
	_, _, err = remote.CommitAndPush(t.Context(), checkout, request, auth)
	require.NoError(t, err)
}

func TestGitOpsBackup_FirstRunPushesSelectedFiles(t *testing.T) {
	env := setupGitOpsBackupTestService(t)
	writeBackupProjectFile(t, env.projectPath, "config/app.conf", "key = value\n")

	syncRecord := env.createBackup(t, gitops.CreateSyncRequest{BackupPaths: []string{"compose.yaml", "config"}})

	require.Equal(t, gitops.SyncModeBackup, syncRecord.Mode)
	require.Equal(t, "backups/demo", syncRecord.BackupDirectory)
	require.Equal(t, "backups/demo/compose.yaml", syncRecord.ComposePath)

	stored := env.reload(t, syncRecord.ID)
	require.NotNil(t, stored.LastSyncStatus)
	assert.Equal(t, "success", *stored.LastSyncStatus)
	assert.NotNil(t, stored.LastBackupAt)
	assert.Equal(t, gitops.BackupStateBackedUp, stored.BackupState())
	assert.False(t, stored.BackupConflict)
	assert.Nil(t, stored.BackupFailureReason)
	require.NotNil(t, stored.LastSyncCommit)
	assert.Equal(t, env.remoteHead(t), *stored.LastSyncCommit)
	require.NotNil(t, stored.LastBackupSnapshot)

	repoPath := env.checkoutRemote(t)
	composeBytes, err := os.ReadFile(filepath.Join(repoPath, "backups", "demo", "compose.yaml"))
	require.NoError(t, err)
	assert.Contains(t, string(composeBytes), "nginx:1.27-alpine")
	assert.FileExists(t, filepath.Join(repoPath, "backups", "demo", "config", "app.conf"))

	assert.NoFileExists(t, filepath.Join(repoPath, "backups", "demo", backup.LegacyBackupManifestFileName))

	snapshot := stored.BackupSnapshot()
	assert.Len(t, snapshot, 2)
	assert.Equal(t, kit.SHA256Hex(composeBytes), snapshot["compose.yaml"])
}

func TestGitOpsBackup_UnchangedContentMakesNoNewCommit(t *testing.T) {
	env := setupGitOpsBackupTestService(t)
	syncRecord := env.createBackup(t, gitops.CreateSyncRequest{})
	firstHead := env.remoteHead(t)

	result, err := env.service.PerformSync(t.Context(), "0", syncRecord.ID, user.SystemUser)
	require.NoError(t, err)
	assert.True(t, result.Success)
	assert.Contains(t, result.Message, "already contains")
	assert.Equal(t, firstHead, env.remoteHead(t))

	stored := env.reload(t, syncRecord.ID)
	require.NotNil(t, stored.LastSyncStatus)
	assert.Equal(t, "success", *stored.LastSyncStatus)
	assert.Equal(t, gitops.BackupStateBackedUp, stored.BackupState())
}

func TestGitOpsBackup_AddsAndRemovesFilesAndKeepsUnrelatedRemoteFiles(t *testing.T) {
	env := setupGitOpsBackupTestService(t)
	writeBackupProjectFile(t, env.projectPath, "config/app.conf", "key = value\n")
	writeBackupProjectFile(t, env.projectPath, "config/extra.conf", "extra = 1\n")
	syncRecord := env.createBackup(t, gitops.CreateSyncRequest{BackupPaths: []string{"compose.yaml", "config"}})

	pushRemoteCommit(t, env.remote, env.repoURL, "unrelated docs", map[string]string{"docs/readme.md": "docs\n"})

	require.NoError(t, os.Remove(filepath.Join(env.projectPath, "config", "extra.conf")))
	writeBackupProjectFile(t, env.projectPath, "scripts/run.sh", "#!/bin/sh\necho hi\n")

	_, err := env.service.UpdateSync(t.Context(), "0", syncRecord.ID, gitops.UpdateSyncRequest{
		BackupPaths: []string{"compose.yaml", "config", "scripts"},
	}, user.SystemUser)
	require.NoError(t, err)

	result, err := env.service.PerformSync(t.Context(), "0", syncRecord.ID, user.SystemUser)
	require.NoError(t, err)
	require.True(t, result.Success)

	repoPath := env.checkoutRemote(t)
	assert.FileExists(t, filepath.Join(repoPath, "backups", "demo", "scripts", "run.sh"))
	assert.FileExists(t, filepath.Join(repoPath, "backups", "demo", "config", "app.conf"))
	assert.FileExists(t, filepath.Join(repoPath, "docs", "readme.md"))
	assert.NoFileExists(t, filepath.Join(repoPath, "backups", "demo", "config", "extra.conf"))
}

func TestGitOpsBackup_EnvFilesOnlyIncludedWhenListedExplicitly(t *testing.T) {
	tests := []struct {
		name        string
		paths       []string
		wantEnvFile bool
	}{
		{name: "directory selection skips env files", paths: []string{"config"}, wantEnvFile: false},
		{name: "explicit env file is included", paths: []string{"config", ".env"}, wantEnvFile: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			env := setupGitOpsBackupTestService(t)
			writeBackupProjectFile(t, env.projectPath, ".env", "TOKEN=secret\n")
			writeBackupProjectFile(t, env.projectPath, "config/app.conf", "key = value\n")
			writeBackupProjectFile(t, env.projectPath, "config/.env", "TOKEN=nested\n")
			writeBackupProjectFile(t, env.projectPath, "config/staging.env", "TOKEN=staging\n")

			env.createBackup(t, gitops.CreateSyncRequest{BackupPaths: test.paths})

			repoPath := env.checkoutRemote(t)
			assert.FileExists(t, filepath.Join(repoPath, "backups", "demo", "compose.yaml"))
			assert.FileExists(t, filepath.Join(repoPath, "backups", "demo", "config", "app.conf"))
			assert.NoFileExists(t, filepath.Join(repoPath, "backups", "demo", "config", ".env"))
			assert.NoFileExists(t, filepath.Join(repoPath, "backups", "demo", "config", "staging.env"))
			if test.wantEnvFile {
				assert.FileExists(t, filepath.Join(repoPath, "backups", "demo", ".env"))
			} else {
				assert.NoFileExists(t, filepath.Join(repoPath, "backups", "demo", ".env"))
			}
		})
	}
}

func TestGitOpsBackup_RemoteEditConflictsThenResolvesWithArcane(t *testing.T) {
	env := setupGitOpsBackupTestService(t)
	syncRecord := env.createBackup(t, gitops.CreateSyncRequest{})

	pushRemoteCommit(t, env.remote, env.repoURL, "edited backup outside arcane", map[string]string{
		"backups/demo/compose.yaml": "services:\n  app:\n    image: tampered\n",
	})
	writeBackupProjectFile(t, env.projectPath, "compose.yaml", "services:\n  app:\n    image: nginx:1.28-alpine\n")

	_, err := env.service.PerformSync(t.Context(), "0", syncRecord.ID, user.SystemUser)
	require.Error(t, err)
	require.ErrorIs(t, err, common.ErrConflict)

	stored := env.reload(t, syncRecord.ID)
	assert.True(t, stored.BackupConflict)
	assert.Equal(t, gitops.BackupStateNeedsAttention, stored.BackupState())
	require.NotNil(t, stored.BackupFailureReason)
	assert.Equal(t, gitops.BackupFailureConflict, *stored.BackupFailureReason)

	preview, err := env.service.PreviewBackup(t.Context(), "0", syncRecord.ID)
	require.NoError(t, err)
	assert.Equal(t, backup.BackupPreviewConflict, preview.State)
	assert.NotEmpty(t, preview.Conflicts)

	result, err := env.service.ResolveBackupConflict(t.Context(), "0", syncRecord.ID, gitops.ResolveBackupConflictRequest{
		Strategy: gitops.BackupConflictUseArcane,
	}, user.SystemUser)
	require.NoError(t, err)
	require.True(t, result.Success)

	resolved := env.reload(t, syncRecord.ID)
	assert.False(t, resolved.BackupConflict)
	assert.Nil(t, resolved.BackupFailureReason)
	assert.Equal(t, gitops.BackupStateBackedUp, resolved.BackupState())

	repoPath := env.checkoutRemote(t)
	composeBytes, err := os.ReadFile(filepath.Join(repoPath, "backups", "demo", "compose.yaml"))
	require.NoError(t, err)
	assert.Contains(t, string(composeBytes), "nginx:1.28-alpine")
}

func TestGitOpsBackup_OccupiedDestinationNeedsAttention(t *testing.T) {
	env := setupGitOpsBackupTestService(t)
	pushRemoteCommit(t, env.remote, env.repoURL, "pre-existing files", map[string]string{
		"backups/demo/unrelated.txt": "not an arcane backup\n",
	})

	syncRecord := env.createBackup(t, gitops.CreateSyncRequest{})

	stored := env.reload(t, syncRecord.ID)
	assert.True(t, stored.BackupConflict)
	assert.Equal(t, gitops.BackupStateNeedsAttention, stored.BackupState())
	require.NotNil(t, stored.BackupFailureReason)
	assert.Equal(t, gitops.BackupFailureDestinationOccupied, *stored.BackupFailureReason)
	assert.Nil(t, stored.LastBackupAt)
}

func TestGitOpsBackup_AdoptsMatchingRemoteWithoutSnapshot(t *testing.T) {
	env := setupGitOpsBackupTestService(t)
	syncRecord := env.createBackup(t, gitops.CreateSyncRequest{})
	head := env.remoteHead(t)

	require.NoError(t, env.db.Model(&projectpkg.GitOpsSync{}).Where("id = ?", syncRecord.ID).
		Update("last_backup_snapshot", nil).Error)

	result, err := env.service.PerformSync(t.Context(), "0", syncRecord.ID, user.SystemUser)
	require.NoError(t, err)
	assert.True(t, result.Success)
	assert.Equal(t, head, env.remoteHead(t))

	stored := env.reload(t, syncRecord.ID)
	assert.False(t, stored.BackupConflict)
	assert.Equal(t, gitops.BackupStateBackedUp, stored.BackupState())
}

func TestGitOpsBackup_SucceedsOnTopOfAnotherWritersCommit(t *testing.T) {
	env := setupGitOpsBackupTestService(t)
	syncRecord := env.createBackup(t, gitops.CreateSyncRequest{})

	pushRemoteCommit(t, env.remote, env.repoURL, "other writer", map[string]string{"other/notes.txt": "notes\n"})
	writeBackupProjectFile(t, env.projectPath, "compose.yaml", "services:\n  app:\n    image: nginx:1.29-alpine\n")

	result, err := env.service.PerformSync(t.Context(), "0", syncRecord.ID, user.SystemUser)
	require.NoError(t, err)
	require.True(t, result.Success)

	repoPath := env.checkoutRemote(t)
	assert.FileExists(t, filepath.Join(repoPath, "other", "notes.txt"))
	composeBytes, err := os.ReadFile(filepath.Join(repoPath, "backups", "demo", "compose.yaml"))
	require.NoError(t, err)
	assert.Contains(t, string(composeBytes), "nginx:1.29-alpine")
}

func TestGitOpsBackup_CreateValidation(t *testing.T) {
	syncDirectory := true
	scriptPath := "deploy.sh"

	tests := []struct {
		name    string
		mutate  func(t *testing.T, env *backupTestEnv)
		request gitops.CreateSyncRequest
		wantErr error
	}{
		{
			name: "project deployed from git",
			mutate: func(t *testing.T, env *backupTestEnv) {
				t.Helper()
				require.NoError(t, env.db.Model(&projectpkg.Project{}).Where("id = ?", env.project.ID).
					Update("gitops_managed_by", "sync-deploy").Error)
			},
			wantErr: common.ErrConflict,
		},
		{
			name: "second backup for the same project",
			mutate: func(t *testing.T, env *backupTestEnv) {
				t.Helper()
				env.createBackup(t, gitops.CreateSyncRequest{})
			},
			request: gitops.CreateSyncRequest{Name: "second", BackupDirectory: "backups/other"},
			wantErr: common.ErrConflict,
		},
		{
			name: "overlapping destination on the same branch",
			mutate: func(t *testing.T, env *backupTestEnv) {
				t.Helper()
				other := &projectpkg.Project{
					ID:      "proj-other",
					Name:    "other-project",
					DirName: new("other-project"),
					Path:    filepath.Join(env.projectsDir, "other-project"),
					Status:  projectpkg.ProjectStatusStopped,
				}
				writeBackupProjectFile(t, other.Path, "compose.yaml", "services: {}\n")
				require.NoError(t, env.db.Create(other).Error)
				env.createBackup(t, gitops.CreateSyncRequest{})
				env.project = other
			},
			request: gitops.CreateSyncRequest{Name: "nested", BackupDirectory: "backups/demo/nested"},
			wantErr: common.ErrConflict,
		},
		{
			name:    "deployment-only directory sync",
			request: gitops.CreateSyncRequest{SyncDirectory: &syncDirectory},
			wantErr: common.ErrValidation,
		},
		{
			name:    "deployment-only pre-deploy script",
			request: gitops.CreateSyncRequest{PreDeployConfigRequest: gitops.PreDeployConfigRequest{PreDeployScriptPath: &scriptPath}},
			wantErr: common.ErrValidation,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			env := setupGitOpsBackupTestService(t)
			writeBackupProjectFile(t, env.projectPath, "config/app.conf", "key = value\n")
			if test.mutate != nil {
				test.mutate(t, env)
			}
			request := test.request
			request.Mode = gitops.SyncModeBackup
			request.Name = cmp.Or(request.Name, "demo-backup")
			request.RepositoryID = "repo-backup"
			request.Branch = "main"
			request.ProjectID = env.project.ID
			request.BackupDirectory = cmp.Or(request.BackupDirectory, "backups/demo")

			_, err := env.service.CreateSync(t.Context(), "0", request, user.SystemUser)
			require.Error(t, err)
			assert.ErrorIs(t, err, test.wantErr)
		})
	}
}

func TestGitOpsBackup_CreateWithoutProjectIsRejected(t *testing.T) {
	env := setupGitOpsBackupTestService(t)

	_, err := env.service.CreateSync(t.Context(), "0", gitops.CreateSyncRequest{
		Name:            "demo-backup",
		RepositoryID:    "repo-backup",
		Branch:          "main",
		Mode:            gitops.SyncModeBackup,
		BackupDirectory: "backups/demo",
	}, user.SystemUser)

	require.Error(t, err)
	assert.ErrorIs(t, err, common.ErrValidation)
}

func TestGitOpsBackup_SaveSignalMarksPendingAndRunsAfterDebounce(t *testing.T) {
	env := setupGitOpsBackupTestService(t)
	syncRecord := env.createBackup(t, gitops.CreateSyncRequest{})
	firstHead := env.remoteHead(t)

	env.service.backups.debounce = 50 * time.Millisecond
	env.service.SubscribeProjectFileChanges(t.Context())

	writeBackupProjectFile(t, env.projectPath, "compose.yaml", "services:\n  app:\n    image: nginx:1.29-alpine\n")
	env.service.projectService.FilesChanged.Publish(env.project.ID)

	require.Eventually(t, func() bool {
		head, exists, err := env.remote.RemoteBranchHead(t.Context(), env.repoURL, "main", git.AuthConfig{AuthType: "none"})
		return err == nil && exists && head != firstHead
	}, 5*time.Second, 25*time.Millisecond)
	require.Positive(t, env.scheduler.submitCount())
	require.NotEqual(t, firstHead, env.remoteHead(t))

	require.Eventually(t, func() bool {
		stored := env.reload(t, syncRecord.ID)
		return stored.LastSyncStatus != nil && *stored.LastSyncStatus == "success" && !stored.BackupPending
	}, 5*time.Second, 25*time.Millisecond)

	stored := env.reload(t, syncRecord.ID)
	require.NotNil(t, stored.LastBackupAt)
	assert.False(t, stored.BackupPending)
	assert.Equal(t, gitops.BackupStateBackedUp, stored.BackupState())
}

func TestGitOpsBackup_SaveSignalOnlyMarksPendingWhenAutoSyncIsOff(t *testing.T) {
	env := setupGitOpsBackupTestService(t)
	syncRecord := env.createBackup(t, gitops.CreateSyncRequest{})
	head := env.remoteHead(t)

	autoSync := false
	_, err := env.service.UpdateSync(t.Context(), "0", syncRecord.ID, gitops.UpdateSyncRequest{AutoSync: &autoSync}, user.SystemUser)
	require.NoError(t, err)

	env.service.backups.debounce = 50 * time.Millisecond
	env.service.SubscribeProjectFileChanges(t.Context())

	writeBackupProjectFile(t, env.projectPath, "compose.yaml", "services:\n  app:\n    image: nginx:1.29-alpine\n")
	env.service.projectService.FilesChanged.Publish(env.project.ID)

	require.Eventually(t, func() bool {
		return env.reload(t, syncRecord.ID).BackupPending
	}, 5*time.Second, 25*time.Millisecond)

	time.Sleep(300 * time.Millisecond)
	assert.Equal(t, head, env.remoteHead(t))

	stored := env.reload(t, syncRecord.ID)
	assert.True(t, stored.BackupPending)
	assert.Equal(t, gitops.BackupStatePaused, stored.BackupState())
}

func TestGitOpsBackup_ReconcileInterruptedBackupsOnStartup(t *testing.T) {
	env := setupGitOpsBackupTestService(t)
	syncRecord := env.createBackup(t, gitops.CreateSyncRequest{})

	require.NoError(t, env.db.Model(&projectpkg.GitOpsSync{}).Where("id = ?", syncRecord.ID).
		Update("last_sync_status", backup.BackupStatusRunning).Error)

	require.NoError(t, env.service.ReconcileInterruptedBackupsOnStartup(t.Context()))

	stored := env.reload(t, syncRecord.ID)
	require.NotNil(t, stored.LastSyncStatus)
	assert.Equal(t, "failed", *stored.LastSyncStatus)
	assert.True(t, stored.BackupPending)
	assert.NotNil(t, stored.BackupPendingSince)
	assert.Equal(t, gitops.BackupStateFailed, stored.BackupState())
}

func TestGitOpsBackup_DeletingProjectRemovesBackupSync(t *testing.T) {
	env := setupGitOpsBackupTestService(t)
	syncRecord := env.createBackup(t, gitops.CreateSyncRequest{})

	require.NoError(t, env.service.projectService.DestroyProject(t.Context(), env.project.ID, true, false, user.SystemUser))

	var remaining int64
	require.NoError(t, env.db.Model(&projectpkg.GitOpsSync{}).Where("id = ?", syncRecord.ID).Count(&remaining).Error)
	assert.Zero(t, remaining)
}

func TestGitOpsDeploy_LinksExistingProject(t *testing.T) {
	env := setupGitOpsBackupTestService(t)
	ctx := t.Context()
	remoteCompose := "services:\n  app:\n    image: nginx:1.28-alpine\n"
	pushRemoteCommit(t, env.remote, env.repoURL, "seed", map[string]string{"apps/demo/compose.yaml": remoteCompose})
	writeBackupProjectFile(t, env.projectPath, ".env", "TOKEN=keep-me\n")

	created, err := env.service.CreateSync(ctx, "0", gitops.CreateSyncRequest{
		Name:         "deploy-existing",
		RepositoryID: "repo-backup",
		Branch:       "main",
		ComposePath:  "apps/demo/compose.yaml",
		ProjectID:    env.project.ID,
	}, user.Actor{ID: "user-1", Username: "tester"})
	require.NoError(t, err)
	require.NotNil(t, created.ProjectID)
	require.Equal(t, env.project.ID, *created.ProjectID)
	require.Equal(t, env.project.Name, created.ProjectName)

	var project projectpkg.Project
	require.NoError(t, env.db.Where("id = ?", env.project.ID).First(&project).Error)
	require.NotNil(t, project.GitOpsManagedBy)
	require.Equal(t, created.ID, *project.GitOpsManagedBy)

	compose, err := os.ReadFile(filepath.Join(env.projectPath, "compose.yaml"))
	require.NoError(t, err)
	require.Equal(t, remoteCompose, string(compose))
	envFile, err := os.ReadFile(filepath.Join(env.projectPath, ".env"))
	require.NoError(t, err)
	require.Contains(t, string(envFile), "TOKEN=keep-me")

	_, err = env.service.CreateSync(ctx, "0", gitops.CreateSyncRequest{
		Name:         "deploy-again",
		RepositoryID: "repo-backup",
		Branch:       "main",
		ComposePath:  "apps/demo/compose.yaml",
		ProjectID:    env.project.ID,
	}, user.Actor{ID: "user-1", Username: "tester"})
	require.ErrorIs(t, err, common.ErrConflict)
}

func TestGitOpsImport_ForwardsDeployAndLifecycleFields(t *testing.T) {
	env := setupGitOpsBackupTestService(t)
	ctx := t.Context()
	require.NoError(t, env.service.settingsService.SetStringSetting(ctx, "lifecycleEnabled", "true"))

	resp, err := env.service.ImportSyncs(ctx, "0", []gitops.ImportGitOpsSyncRequest{
		{
			SyncName:             "Media-Server",
			GitRepo:              "backup-remote",
			Branch:               "main",
			DockerComposePath:    "apps/demo/compose.yaml",
			SyncDirectory:        new(true),
			ProjectName:          "media-server",
			PullImageAfterSync:   new(true),
			RedeployAfterSync:    new(true),
			PreDeployScriptPath:  new("scripts/decrypt.sh"),
			PreDeployRunnerImage: new("alpine:3.20"),
			PreDeployEnv:         new("SOPS_AGE_KEY=secret\n"),
			PreDeployTimeoutSec:  new(120),
			PreDeployNetworkMode: new("bridge"),
		},
		{
			SyncName:          "plain",
			GitRepo:           "backup-remote",
			Branch:            "main",
			DockerComposePath: "apps/demo/compose.yaml",
		},
	}, user.SystemUser)
	require.NoError(t, err)
	require.Empty(t, resp.Errors)
	require.Equal(t, 2, resp.SuccessCount)

	var full, plain projectpkg.GitOpsSync
	require.NoError(t, env.db.Where("name = ?", "Media-Server").First(&full).Error)
	require.NoError(t, env.db.Where("name = ?", "plain").First(&plain).Error)

	assert.Equal(t, "media-server", full.ProjectName)
	assert.True(t, full.PullImageAfterSync)
	assert.True(t, full.RedeployAfterSync)
	assert.Equal(t, "scripts/decrypt.sh", *full.PreDeployScriptPath)
	assert.Equal(t, "alpine:3.20", *full.PreDeployRunnerImage)
	assert.Equal(t, "SOPS_AGE_KEY=secret", *full.PreDeployEnv)
	assert.Equal(t, 120, full.PreDeployTimeoutSec)
	assert.Equal(t, "bridge", full.PreDeployNetworkMode)

	assert.Equal(t, "plain", plain.ProjectName)
	assert.False(t, plain.PullImageAfterSync)
	assert.False(t, plain.RedeployAfterSync)
	assert.Nil(t, plain.PreDeployScriptPath)
	assert.Equal(t, 60, plain.PreDeployTimeoutSec)
	assert.Equal(t, "none", plain.PreDeployNetworkMode)
}
