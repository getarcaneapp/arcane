package services

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/getarcaneapp/arcane/backend/v2/internal/common"
	"github.com/getarcaneapp/arcane/backend/v2/internal/config"
	"github.com/getarcaneapp/arcane/backend/v2/internal/models"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/projects"
	projecttypes "github.com/getarcaneapp/arcane/types/v2/project"
	"github.com/stretchr/testify/require"
)

func TestProjectService_UpdateProject_RenameRollsBackWhenFileRevisionIsStale(t *testing.T) {
	db := setupProjectTestDB(t)
	ctx := context.Background()

	projectsDir := t.TempDir()
	t.Setenv("PROJECTS_DIRECTORY", projectsDir)

	settingsService, err := NewSettingsService(ctx, db)
	require.NoError(t, err)
	require.NoError(t, settingsService.SetStringSetting(ctx, "projectsDirectory", projectsDir))

	eventService := NewEventService(db, nil, nil)
	svc := NewProjectService(db, settingsService, eventService, nil, nil, nil, config.Load())
	configureProjectRuntimeDockerInternal(t, nil)

	originalDirName := "Foo"
	originalPath := createComposeProjectDir(t, projectsDir, originalDirName)
	project := &models.Project{
		BaseModel: models.BaseModel{ID: "proj-rename-stale-file-revision"},
		Name:      "Foo",
		DirName:   &originalDirName,
		Path:      originalPath,
		Status:    models.ProjectStatusStopped,
	}
	require.NoError(t, db.Create(project).Error)

	_, revision, err := projects.ReadProjectFileTree(project.Path, config.Load().ProjectFileTreeMaxDepth, config.Load().ProjectScanSkipDirs, "compose.yaml")
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(project.Path, "external.txt"), []byte("external\n"), 0o644))

	content := "new\n"
	_, err = svc.UpdateProject(ctx, project.ID, ptr("bar"), nil, nil, &revision, []projecttypes.ProjectFileChange{
		{Operation: projecttypes.FileOpCreateFile, RelativePath: "notes.txt", Content: &content},
	}, models.User{
		BaseModel: models.BaseModel{ID: "u1"},
		Username:  "tester",
	})

	require.Error(t, err)
	var conflictErr *common.ProjectFileConflictError
	require.ErrorAs(t, err, &conflictErr)
	require.DirExists(t, originalPath)
	require.NoDirExists(t, filepath.Join(projectsDir, "bar"))
	require.FileExists(t, filepath.Join(originalPath, "external.txt"))
	require.NoFileExists(t, filepath.Join(originalPath, "notes.txt"))

	var fromDB models.Project
	require.NoError(t, db.First(&fromDB, "id = ?", project.ID).Error)
	require.Equal(t, "Foo", fromDB.Name)
	require.Equal(t, originalPath, fromDB.Path)
}
