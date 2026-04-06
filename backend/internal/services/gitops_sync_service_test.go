package services

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/getarcaneapp/arcane/backend/internal/config"
	"github.com/getarcaneapp/arcane/backend/internal/database"
	"github.com/getarcaneapp/arcane/backend/internal/models"
	"github.com/getarcaneapp/arcane/backend/pkg/projects"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func setupGitOpsSyncDirectoryTestService(t *testing.T) (*GitOpsSyncService, *database.DB, string) {
	t.Helper()

	ctx := context.Background()
	db := setupProjectTestDB(t)
	require.NoError(t, db.AutoMigrate(&models.GitOpsSync{}))

	settingsService, err := NewSettingsService(ctx, db)
	require.NoError(t, err)

	projectsDir := t.TempDir()
	require.NoError(t, settingsService.SetStringSetting(ctx, "projectsDirectory", projectsDir))

	projectService := NewProjectService(db, settingsService, nil, nil, nil, nil, config.Load())

	return NewGitOpsSyncService(db, nil, projectService, nil, settingsService), db, projectsDir
}

func TestGitOpsSyncService_SyncProjectDirectory_CreatesProjectPreservingRepoLayout(t *testing.T) {
	ctx := context.Background()
	svc, db, _ := setupGitOpsSyncDirectoryTestService(t)

	sync := &models.GitOpsSync{
		BaseModel:     models.BaseModel{ID: "sync-directory-create"},
		Name:          "demo-sync",
		EnvironmentID: "0",
		RepositoryID:  "repo-1",
		ComposePath:   "apps/demo/docker-compose.yaml",
		ProjectName:   "demo-project",
		SyncDirectory: true,
	}
	require.NoError(t, db.Create(sync).Error)

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
			Content:      []byte("APP_MODE=production\n"),
		},
	}

	project, syncedFiles, created, changed, err := svc.syncProjectDirectoryInternal(ctx, sync, syncFiles, models.User{})
	require.NoError(t, err)
	require.NotNil(t, project)
	require.True(t, created)
	require.True(t, changed)
	require.ElementsMatch(t, []string{"docker-compose.yaml", "meta.yaml", ".env"}, syncedFiles)

	composePath, detectErr := projects.DetectComposeFile(project.Path)
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
	assert.Equal(t, "APP_MODE=production\n", string(envBytes))

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

	project := &models.Project{
		BaseModel: models.BaseModel{ID: "proj-directory-update"},
		Name:      "demo-project",
		DirName:   new("demo-project"),
		Path:      projectPath,
		Status:    models.ProjectStatusStopped,
	}
	require.NoError(t, db.Create(project).Error)

	oldSyncedFilesJSON, err := json.Marshal([]string{"docker-compose.yaml", "meta.yaml", "old.txt"})
	require.NoError(t, err)

	sync := &models.GitOpsSync{
		BaseModel:     models.BaseModel{ID: "sync-directory-update"},
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

	updatedProject, syncedFiles, created, changed, err := svc.syncProjectDirectoryInternal(ctx, sync, syncFiles, models.User{})
	require.NoError(t, err)
	require.NotNil(t, updatedProject)
	require.False(t, created)
	require.True(t, changed)
	require.ElementsMatch(t, []string{"docker-compose.yaml", "nested/feature.yaml"}, syncedFiles)

	composePath, detectErr := projects.DetectComposeFile(updatedProject.Path)
	require.NoError(t, detectErr)
	assert.Equal(t, filepath.Join(updatedProject.Path, "docker-compose.yaml"), composePath)

	_, statErr := os.Stat(filepath.Join(updatedProject.Path, "old.txt"))
	assert.ErrorIs(t, statErr, os.ErrNotExist)

	_, statErr = os.Stat(filepath.Join(updatedProject.Path, "compose.yaml"))
	assert.ErrorIs(t, statErr, os.ErrNotExist)

	keepBytes, err := os.ReadFile(filepath.Join(updatedProject.Path, "keep.txt"))
	require.NoError(t, err)
	assert.Equal(t, "keep me\n", string(keepBytes))

	featureBytes, err := os.ReadFile(filepath.Join(updatedProject.Path, "nested", "feature.yaml"))
	require.NoError(t, err)
	assert.Contains(t, string(featureBytes), "worker:")
}

func TestGitOpsSyncService_CreateDirectorySyncProjectInternal_RollsBackProjectOnUpdateFailure(t *testing.T) {
	ctx := context.Background()
	svc, db, projectsDir := setupGitOpsSyncDirectoryTestService(t)

	sync := &models.GitOpsSync{
		BaseModel:     models.BaseModel{ID: "sync-directory-tx-rollback"},
		Name:          "demo-sync",
		EnvironmentID: "0",
		RepositoryID:  "repo-1",
		ComposePath:   "apps/demo/docker-compose.yaml",
		ProjectName:   "demo-project",
		SyncDirectory: true,
	}
	require.NoError(t, db.Create(sync).Error)

	stagePath := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(stagePath, "docker-compose.yaml"), []byte("services: {}\n"), 0o644))

	stage := &stagedDirectorySync{
		stagePath:       stagePath,
		composeFileName: "docker-compose.yaml",
		serviceCount:    1,
	}

	callbackName := "test:fail_project_gitops_update"
	require.NoError(t, db.Callback().Update().Before("gorm:update").Register(callbackName, func(tx *gorm.DB) {
		if tx.Statement != nil && tx.Statement.Table == "projects" {
			tx.AddError(errors.New("forced project update failure"))
		}
	}))
	defer func() {
		db.Callback().Update().Remove(callbackName)
	}()

	project, err := svc.createDirectorySyncProjectInternal(ctx, sync, stage, models.User{})
	require.Error(t, err)
	require.Nil(t, project)
	assert.Contains(t, err.Error(), "failed to mark project as GitOps-managed")

	var projectCount int64
	require.NoError(t, db.Model(&models.Project{}).Count(&projectCount).Error)
	assert.Zero(t, projectCount)

	var storedSync models.GitOpsSync
	require.NoError(t, db.First(&storedSync, "id = ?", sync.ID).Error)
	assert.Nil(t, storedSync.ProjectID)

	_, statErr := os.Stat(filepath.Join(projectsDir, "demo-project"))
	assert.ErrorIs(t, statErr, os.ErrNotExist)
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
	db := setupSettingsTestDB(t)
	settingsSvc, err := NewSettingsService(ctx, db)
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
	db := setupSettingsTestDB(t)
	settingsSvc, err := NewSettingsService(ctx, db)
	require.NoError(t, err)

	require.NoError(t, settingsSvc.SetIntSetting(ctx, "gitSyncMaxFiles", 200))
	require.NoError(t, settingsSvc.SetIntSetting(ctx, "gitSyncMaxTotalSizeMb", 30))
	require.NoError(t, settingsSvc.SetIntSetting(ctx, "gitSyncMaxBinarySizeMb", 5))

	svc := &GitOpsSyncService{settingsService: settingsSvc}

	t.Run("uses environment caps when sync values are looser", func(t *testing.T) {
		sync := &models.GitOpsSync{
			MaxSyncFiles:      500,
			MaxSyncTotalSize:  50 * 1024 * 1024,
			MaxSyncBinarySize: 10 * 1024 * 1024,
		}

		maxFiles, maxTotalSize, maxBinarySize := svc.getEffectiveSyncLimits(ctx, sync)

		require.Equal(t, 200, maxFiles)
		require.Equal(t, int64(30*1024*1024), maxTotalSize)
		require.Equal(t, int64(5*1024*1024), maxBinarySize)
	})

	t.Run("preserves tighter sync-specific limits", func(t *testing.T) {
		sync := &models.GitOpsSync{
			MaxSyncFiles:      75,
			MaxSyncTotalSize:  8 * 1024 * 1024,
			MaxSyncBinarySize: 2 * 1024 * 1024,
		}

		maxFiles, maxTotalSize, maxBinarySize := svc.getEffectiveSyncLimits(ctx, sync)

		require.Equal(t, 75, maxFiles)
		require.Equal(t, int64(8*1024*1024), maxTotalSize)
		require.Equal(t, int64(2*1024*1024), maxBinarySize)
	})

	t.Run("treats zero as unlimited", func(t *testing.T) {
		sync := &models.GitOpsSync{
			MaxSyncFiles:      0,
			MaxSyncTotalSize:  0,
			MaxSyncBinarySize: 0,
		}

		maxFiles, maxTotalSize, maxBinarySize := svc.getEffectiveSyncLimits(ctx, sync)

		require.Equal(t, 200, maxFiles)
		require.Equal(t, int64(30*1024*1024), maxTotalSize)
		require.Equal(t, int64(5*1024*1024), maxBinarySize)
	})
}
