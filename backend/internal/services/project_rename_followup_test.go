package services

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/getarcaneapp/arcane/backend/v2/internal/common"
	"github.com/getarcaneapp/arcane/backend/v2/internal/config"
	"github.com/getarcaneapp/arcane/backend/v2/internal/models"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/projects"
	projecttypes "github.com/getarcaneapp/arcane/types/v2/project"
	"github.com/stretchr/testify/require"
)

func TestDockerProjectVolumeRenameMigrationInternal_RollbackExplainsPreservedTargetWhenSourceMissing(t *testing.T) {
	var targetRemoved atomic.Bool

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/volumes/nginx_data"):
			http.NotFound(w, r)
		case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/volumes/web_data"):
			w.Header().Set("Content-Type", "application/json")
			require.NoError(t, json.NewEncoder(w).Encode(map[string]any{"Name": "web_data"}))
		case r.Method == http.MethodDelete && strings.HasSuffix(r.URL.Path, "/volumes/web_data"):
			targetRemoved.Store(true)
			w.WriteHeader(http.StatusNoContent)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)

	migration := &dockerProjectVolumeRenameMigrationInternal{
		service: &ProjectService{
			dockerService: &DockerClientService{client: newTestDockerClient(t, server)},
		},
		createdNew: []projectVolumeRenameEntryInternal{
			{OldName: "nginx_data", NewName: "web_data"},
		},
	}

	err := migration.Rollback(context.Background())

	require.Error(t, err)
	var preserved *projectRenameTargetPreservedDuringRollbackInternalError
	require.ErrorAs(t, err, &preserved)
	require.Contains(t, err.Error(), "avoid data loss")
	require.Contains(t, err.Error(), "only remaining data copy")
	require.False(t, targetRemoved.Load())
}

func TestRollbackProjectRenameJournalVolumeInternal_ExplainsPreservedTargetWhenSourceMissing(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/volumes/nginx_data"):
			http.NotFound(w, r)
		case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/volumes/web_data"):
			w.Header().Set("Content-Type", "application/json")
			require.NoError(t, json.NewEncoder(w).Encode(map[string]any{"Name": "web_data"}))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)

	err := rollbackProjectRenameJournalVolumeInternal(context.Background(), newTestDockerClient(t, server), projectRenameJournalVolumeInternal{
		OldName: "nginx_data",
		NewName: "web_data",
	})

	require.Error(t, err)
	var preserved *projectRenameTargetPreservedDuringRollbackInternalError
	require.ErrorAs(t, err, &preserved)
	require.Contains(t, err.Error(), "avoid data loss")
	require.Contains(t, err.Error(), "only remaining data copy")
}

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
