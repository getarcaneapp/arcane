package services

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/getarcaneapp/arcane/backend/v2/internal/common"
	"github.com/getarcaneapp/arcane/backend/v2/internal/config"
	"github.com/getarcaneapp/arcane/backend/v2/internal/database"
	"github.com/getarcaneapp/arcane/backend/v2/internal/models"
	git "github.com/getarcaneapp/arcane/backend/v2/pkg/gitutil"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/projects"
	gitopstypes "github.com/getarcaneapp/arcane/types/v2/gitops"
	schedulertypes "github.com/getarcaneapp/arcane/types/v2/scheduler"
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

	eventService := NewEventService(db, config.Load(), nil)
	projectService := NewProjectService(db, settingsService, eventService, nil, nil, nil, config.Load())

	return NewGitOpsSyncService(db, nil, projectService, nil, eventService, settingsService), db, projectsDir
}

type gitOpsSyncTestSchedulerInternal struct {
	removed []string
}

func (s *gitOpsSyncTestSchedulerInternal) AddJob(_ context.Context, _ schedulertypes.Job) error {
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

func TestGitOpsSyncService_GetSyncByID_ReturnsNotFoundError(t *testing.T) {
	ctx := context.Background()
	svc, _, _ := setupGitOpsSyncDirectoryTestService(t)

	_, err := svc.GetSyncByID(ctx, "0", "missing-sync")

	var notFound *models.NotFoundError
	require.ErrorAs(t, err, &notFound)
}

func TestGitOpsSyncService_DeleteSync_DeletesStaleProjectReference(t *testing.T) {
	ctx := context.Background()
	svc, db, _ := setupGitOpsSyncDirectoryTestService(t)
	missingProjectID := "missing-project"

	sync := &models.GitOpsSync{
		BaseModel:     models.BaseModel{ID: "sync-delete-stale-project"},
		Name:          "demo-sync",
		EnvironmentID: "0",
		RepositoryID:  "repo-1",
		ComposePath:   "apps/demo/docker-compose.yaml",
		ProjectName:   "demo-project",
		ProjectID:     &missingProjectID,
		SyncInterval:  60,
	}
	require.NoError(t, db.Create(sync).Error)

	require.NoError(t, svc.DeleteSync(ctx, "0", sync.ID, models.User{}))

	var count int64
	require.NoError(t, db.Model(&models.GitOpsSync{}).Where("id = ?", sync.ID).Count(&count).Error)
	assert.Zero(t, count)
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

func TestGitOpsSyncService_DirectorySync_RealWalkWithNestedConfig(t *testing.T) {
	ctx := context.Background()
	svc, db, _ := setupGitOpsSyncDirectoryTestService(t)
	svc.repoService = &GitRepositoryService{gitClient: git.NewClient("")}

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

	sync := &models.GitOpsSync{
		BaseModel:     models.BaseModel{ID: "sync-directory-real-walk"},
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

	project, syncedFiles, created, changed, err := svc.syncProjectDirectoryInternal(ctx, sync, syncFiles, models.User{})
	require.NoError(t, err)
	require.NotNil(t, project)
	require.True(t, created)
	require.True(t, changed)
	require.ElementsMatch(t, []string{"docker-compose.yml", "config/dynamic_config.yml"}, syncedFiles)

	composePath, detectErr := projects.DetectComposeFile(project.Path)
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
	svc.repoService = &GitRepositoryService{gitClient: git.NewClient("")}

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
	project := &models.Project{
		BaseModel: models.BaseModel{ID: "proj-directory-docker-dir-conflict"},
		Name:      "traefik-project",
		DirName:   &dirName,
		Path:      projectPath,
		Status:    models.ProjectStatusStopped,
	}
	require.NoError(t, db.Create(project).Error)

	sync := &models.GitOpsSync{
		BaseModel:     models.BaseModel{ID: "sync-directory-docker-dir-conflict"},
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

	updatedProject, syncedFiles, created, changed, err := svc.syncProjectDirectoryInternal(ctx, sync, syncFiles, models.User{})
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
			_ = tx.AddError(errors.New("forced project update failure"))
		}
	}))
	defer func() {
		_ = db.Callback().Update().Remove(callbackName)
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

func TestProjectsRemoveStaleComposeFiles_RemovesStaleCustomComposeFiles(t *testing.T) {
	t.Parallel()

	projectPath := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(projectPath, "radarr.yaml"), []byte("services:\n  app:\n    image: nginx:alpine\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(projectPath, "sonarr.yaml"), []byte("services:\n  app:\n    image: nginx:alpine\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(projectPath, "values.yaml"), []byte("replicaCount: 2\nimage:\n  tag: latest\n"), 0o644))

	err := projects.RemoveStaleComposeFiles(projectPath, "sonarr.yaml", []string{"sonarr.yaml"})
	require.NoError(t, err)

	_, statErr := os.Stat(filepath.Join(projectPath, "radarr.yaml"))
	assert.ErrorIs(t, statErr, os.ErrNotExist)

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
	sync := &models.GitOpsSync{
		BaseModel:     models.BaseModel{ID: "sync-directory-relink"},
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
	project := &models.Project{
		BaseModel:       models.BaseModel{ID: "proj-directory-relink"},
		Name:            "Radarr",
		DirName:         &dirName,
		Path:            projectPath,
		Status:          models.ProjectStatusStopped,
		GitOpsManagedBy: &sync.ID,
	}
	require.NoError(t, db.Create(project).Error)

	recovered, err := svc.getDirectorySyncProjectInternal(ctx, sync)
	require.NoError(t, err)
	require.NotNil(t, recovered)
	assert.Equal(t, project.ID, recovered.ID)

	var storedSync models.GitOpsSync
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
	sync := &models.GitOpsSync{
		BaseModel:     models.BaseModel{ID: "sync-directory-disk-recovery"},
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

	var storedSync models.GitOpsSync
	require.NoError(t, db.First(&storedSync, "id = ?", sync.ID).Error)
	require.NotNil(t, storedSync.ProjectID)
	assert.Equal(t, recovered.ID, *storedSync.ProjectID)

	var storedProject models.Project
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
	sync := &models.GitOpsSync{
		BaseModel:     models.BaseModel{ID: "sync-directory-ambiguous"},
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
	require.NoError(t, db.Model(&models.Project{}).Count(&projectsCount).Error)
	assert.Zero(t, projectsCount)

	var storedSync models.GitOpsSync
	require.NoError(t, db.First(&storedSync, "id = ?", sync.ID).Error)
	require.NotNil(t, storedSync.ProjectID)
	assert.Equal(t, "missing-project", *storedSync.ProjectID)
}

func TestGitOpsSyncService_SyncProjectDirectory_FailsWhenBoundProjectMissing(t *testing.T) {
	ctx := context.Background()
	svc, db, projectsDir := setupGitOpsSyncDirectoryTestService(t)
	testScheduler := &gitOpsSyncTestSchedulerInternal{}
	svc.SetScheduler(ctx, testScheduler)

	missingProjectID := "missing-project"
	sync := &models.GitOpsSync{
		BaseModel:     models.BaseModel{ID: "sync-directory-missing-bound-project"},
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

	project, syncedFiles, created, changed, err := svc.syncProjectDirectoryInternal(ctx, sync, syncFiles, models.User{})
	require.Error(t, err)
	var bindingErr *common.GitOpsSyncProjectBindingBrokenError
	require.ErrorAs(t, err, &bindingErr)
	require.Nil(t, project)
	assert.Nil(t, syncedFiles)
	assert.False(t, created)
	assert.False(t, changed)

	var projectCount int64
	require.NoError(t, db.Model(&models.Project{}).Count(&projectCount).Error)
	assert.Zero(t, projectCount)

	_, statErr := os.Stat(filepath.Join(projectsDir, "demo-project"))
	assert.ErrorIs(t, statErr, os.ErrNotExist)

	var storedSync models.GitOpsSync
	require.NoError(t, db.First(&storedSync, "id = ?", sync.ID).Error)
	assert.False(t, storedSync.AutoSync)
	require.NotNil(t, storedSync.LastSyncStatus)
	assert.Equal(t, "failed", *storedSync.LastSyncStatus)
	require.NotNil(t, storedSync.LastSyncError)
	assert.Contains(t, *storedSync.LastSyncError, "project binding")
	assert.Contains(t, testScheduler.removed, gitOpsSyncJobNameInternal(sync.ID))
}

func TestGitOpsSyncService_SyncProjectDirectory_DisablesAutoSyncWhenBoundProjectRecoveryAmbiguous(t *testing.T) {
	ctx := context.Background()
	svc, db, projectsDir := setupGitOpsSyncDirectoryTestService(t)
	testScheduler := &gitOpsSyncTestSchedulerInternal{}
	svc.SetScheduler(ctx, testScheduler)

	for _, dirName := range []string{"Radarr-3", "Radarr-30"} {
		projectPath := filepath.Join(projectsDir, dirName)
		require.NoError(t, os.MkdirAll(projectPath, 0o755))
		require.NoError(t, os.WriteFile(filepath.Join(projectPath, "radarr.yaml"), []byte("services:\n  app:\n    image: lscr.io/linuxserver/radarr:latest\n"), 0o644))
	}

	missingProjectID := "missing-project"
	sync := &models.GitOpsSync{
		BaseModel:     models.BaseModel{ID: "sync-directory-ambiguous-bound-project"},
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

	project, syncedFiles, created, changed, err := svc.syncProjectDirectoryInternal(ctx, sync, syncFiles, models.User{})
	require.Error(t, err)
	var bindingErr *common.GitOpsSyncProjectBindingBrokenError
	require.ErrorAs(t, err, &bindingErr)
	require.Nil(t, project)
	assert.Nil(t, syncedFiles)
	assert.False(t, created)
	assert.False(t, changed)

	var projectCount int64
	require.NoError(t, db.Model(&models.Project{}).Count(&projectCount).Error)
	assert.Zero(t, projectCount)

	var storedSync models.GitOpsSync
	require.NoError(t, db.First(&storedSync, "id = ?", sync.ID).Error)
	assert.False(t, storedSync.AutoSync)
	require.NotNil(t, storedSync.LastSyncStatus)
	assert.Equal(t, "failed", *storedSync.LastSyncStatus)
	require.NotNil(t, storedSync.LastSyncError)
	assert.Contains(t, *storedSync.LastSyncError, "multiple candidate project directories")
	assert.Contains(t, testScheduler.removed, gitOpsSyncJobNameInternal(sync.ID))
}

func TestGitOpsSyncService_GetOrCreateProjectInternal_FailsWhenBoundProjectMissing(t *testing.T) {
	ctx := context.Background()
	svc, db, projectsDir := setupGitOpsSyncDirectoryTestService(t)
	testScheduler := &gitOpsSyncTestSchedulerInternal{}
	svc.SetScheduler(ctx, testScheduler)

	missingProjectID := "missing-project"
	sync := &models.GitOpsSync{
		BaseModel:     models.BaseModel{ID: "sync-file-missing-bound-project"},
		Name:          "demo-sync",
		EnvironmentID: "0",
		RepositoryID:  "repo-1",
		ComposePath:   "apps/demo/docker-compose.yaml",
		ProjectName:   "demo-project",
		ProjectID:     &missingProjectID,
		AutoSync:      true,
	}
	require.NoError(t, db.Create(sync).Error)

	result := &gitopstypes.SyncResult{}
	project, err := svc.getOrCreateProjectInternal(ctx, sync, sync.ID, "services:\n  app:\n    image: nginx:alpine\n", nil, result, models.User{})
	require.Error(t, err)
	var bindingErr *common.GitOpsSyncProjectBindingBrokenError
	require.ErrorAs(t, err, &bindingErr)
	require.Nil(t, project)

	var projectCount int64
	require.NoError(t, db.Model(&models.Project{}).Count(&projectCount).Error)
	assert.Zero(t, projectCount)

	_, statErr := os.Stat(filepath.Join(projectsDir, "demo-project"))
	assert.ErrorIs(t, statErr, os.ErrNotExist)
	_, statErr = os.Stat(filepath.Join(projectsDir, "demo-project-1"))
	assert.ErrorIs(t, statErr, os.ErrNotExist)

	var storedSync models.GitOpsSync
	require.NoError(t, db.First(&storedSync, "id = ?", sync.ID).Error)
	assert.False(t, storedSync.AutoSync)
	require.NotNil(t, storedSync.LastSyncStatus)
	assert.Equal(t, "failed", *storedSync.LastSyncStatus)
	require.NotNil(t, storedSync.LastSyncError)
	assert.Contains(t, *storedSync.LastSyncError, "project binding")
	assert.Contains(t, testScheduler.removed, gitOpsSyncJobNameInternal(sync.ID))
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
	t.Setenv("GIT_SYNC_MAX_FILES", "")
	t.Setenv("GIT_SYNC_MAX_TOTAL_SIZE_MB", "")
	t.Setenv("GIT_SYNC_MAX_BINARY_SIZE_MB", "")

	db := setupSettingsTestDB(t)
	settingsSvc, err := NewSettingsService(ctx, db)
	require.NoError(t, err)

	require.NoError(t, settingsSvc.SetIntSetting(ctx, "gitSyncMaxFiles", 200))
	require.NoError(t, settingsSvc.SetIntSetting(ctx, "gitSyncMaxTotalSizeMb", 30))
	require.NoError(t, settingsSvc.SetIntSetting(ctx, "gitSyncMaxBinarySizeMb", 5))

	svc := &GitOpsSyncService{settingsService: settingsSvc}

	t.Run("preserves sync-specific limits when they exceed settings", func(t *testing.T) {
		sync := &models.GitOpsSync{
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

	t.Run("zero disables sync limits", func(t *testing.T) {
		sync := &models.GitOpsSync{
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
		settingsSvcEnv, svcErr := NewSettingsService(ctx, db)
		require.NoError(t, svcErr)
		svcEnv := &GitOpsSyncService{settingsService: settingsSvcEnv}

		sync := &models.GitOpsSync{
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
		settingsSvcEnv, svcErr := NewSettingsService(ctx, db)
		require.NoError(t, svcErr)
		svcEnv := &GitOpsSyncService{settingsService: settingsSvcEnv}

		sync := &models.GitOpsSync{
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
