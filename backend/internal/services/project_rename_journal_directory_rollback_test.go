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

	"github.com/getarcaneapp/arcane/backend/v2/internal/config"
	"github.com/getarcaneapp/arcane/backend/v2/internal/models"
	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/api/types/volume"
	"github.com/stretchr/testify/require"
)

func TestProjectService_RecoverProjectRenameJournals_KeepsJournalWhenDirectoryRollbackFails(t *testing.T) {
	db := setupProjectTestDB(t)
	require.NoError(t, db.AutoMigrate(&models.KVEntry{}))
	ctx := context.Background()

	var targetRemoved atomic.Bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/volumes/nginx_data"):
			w.Header().Set("Content-Type", "application/json")
			require.NoError(t, json.NewEncoder(w).Encode(volume.Volume{Name: "nginx_data"}))
		case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/volumes/web_data"):
			if targetRemoved.Load() {
				http.NotFound(w, r)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			require.NoError(t, json.NewEncoder(w).Encode(volume.Volume{Name: "web_data"}))
		case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/containers/json"):
			w.Header().Set("Content-Type", "application/json")
			require.NoError(t, json.NewEncoder(w).Encode([]container.Summary{}))
		case r.Method == http.MethodDelete && strings.HasSuffix(r.URL.Path, "/volumes/web_data"):
			targetRemoved.Store(true)
			w.WriteHeader(http.StatusNoContent)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)

	projectsDir := t.TempDir()
	oldDir := "nginx"
	newDir := "web"
	oldPath := filepath.Join(projectsDir, "missing-parent", oldDir)
	newPath := filepath.Join(projectsDir, newDir)
	require.NoError(t, os.MkdirAll(newPath, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(newPath, "compose.yaml"), []byte("services: {}\n"), 0o600))

	project := &models.Project{
		BaseModel: models.BaseModel{ID: "proj-rename-directory-rollback-fails"},
		Name:      "nginx",
		DirName:   &oldDir,
		Path:      oldPath,
		Status:    models.ProjectStatusStopped,
	}
	require.NoError(t, db.Create(project).Error)

	kvService := NewKVService(db)
	dockerService := &DockerClientService{client: newTestDockerClient(t, server)}
	svc := NewProjectService(db, nil, nil, nil, dockerService, nil, config.Load()).WithKVService(kvService)
	journal := projectRenameJournalInternal{
		ProjectID:  project.ID,
		OldName:    "nginx",
		NewName:    "web",
		OldPath:    oldPath,
		NewPath:    newPath,
		OldDirName: &oldDir,
		NewDirName: newDir,
		Phase:      projectRenameJournalPhaseTargetsCopiedInternal,
		Volumes: []projectRenameJournalVolumeInternal{
			{
				Key:     "data",
				OldName: "nginx_data",
				NewName: "web_data",
			},
		},
	}
	payload, err := json.Marshal(journal)
	require.NoError(t, err)
	require.NoError(t, kvService.Set(ctx, projectRenameJournalKeyInternal(project.ID), string(payload)))

	err = svc.RecoverProjectRenameJournals(ctx)
	require.Error(t, err)
	require.Contains(t, err.Error(), "rollback project directory rename")

	_, ok, err := kvService.Get(ctx, projectRenameJournalKeyInternal(project.ID))
	require.NoError(t, err)
	require.True(t, ok)
	require.True(t, targetRemoved.Load(), "target volume rollback should still run after directory rollback fails")
	require.FileExists(t, filepath.Join(newPath, "compose.yaml"), "failed directory rollback leaves the target path for retry")
	require.NoDirExists(t, oldPath)

	var fromDB models.Project
	require.NoError(t, db.First(&fromDB, "id = ?", project.ID).Error)
	require.Equal(t, "nginx", fromDB.Name)
	require.Equal(t, oldPath, fromDB.Path)
	require.NotNil(t, fromDB.DirName)
	require.Equal(t, oldDir, *fromDB.DirName)

	require.NoError(t, os.MkdirAll(filepath.Dir(oldPath), 0o755))
	require.NoError(t, svc.RecoverProjectRenameJournals(ctx))

	_, ok, err = kvService.Get(ctx, projectRenameJournalKeyInternal(project.ID))
	require.NoError(t, err)
	require.False(t, ok)
	require.FileExists(t, filepath.Join(oldPath, "compose.yaml"))
	require.NoDirExists(t, newPath)
}
