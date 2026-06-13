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
	"github.com/moby/moby/api/types/volume"
	"github.com/stretchr/testify/require"
)

func TestProjectService_RecoverProjectRenameJournals_CompletesCommittedVolumeJournalWithoutHelperImage(t *testing.T) {
	db := setupProjectTestDB(t)
	require.NoError(t, db.AutoMigrate(&models.KVEntry{}))
	ctx := context.Background()

	var imageInspectCalled atomic.Bool
	var oldVolumeRemoved atomic.Bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/volumes/web_data"):
			w.Header().Set("Content-Type", "application/json")
			require.NoError(t, json.NewEncoder(w).Encode(volume.Volume{Name: "web_data"}))
		case r.Method == http.MethodDelete && strings.HasSuffix(r.URL.Path, "/volumes/nginx_data"):
			oldVolumeRemoved.Store(true)
			w.WriteHeader(http.StatusNoContent)
		case strings.Contains(r.URL.Path, "/images/"):
			imageInspectCalled.Store(true)
			http.NotFound(w, r)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)

	projectsDir := t.TempDir()
	oldDir := "nginx"
	newDir := "web"
	oldPath := filepath.Join(projectsDir, oldDir)
	newPath := filepath.Join(projectsDir, newDir)
	require.NoError(t, os.MkdirAll(newPath, 0o755))

	project := &models.Project{
		BaseModel: models.BaseModel{ID: "proj-rename-committed-with-volumes"},
		Name:      "web",
		DirName:   &newDir,
		Path:      newPath,
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
		Phase:      projectRenameJournalPhaseOldVolumesRemoved,
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

	require.NoError(t, svc.RecoverProjectRenameJournals(ctx))

	_, ok, err := kvService.Get(ctx, projectRenameJournalKeyInternal(project.ID))
	require.NoError(t, err)
	require.False(t, ok)
	require.True(t, oldVolumeRemoved.Load(), "expected committed recovery to remove source volume")
	require.False(t, imageInspectCalled.Load(), "completion recovery should not inspect or pull the copy helper image")
}

func TestProjectService_RecoverProjectRenameJournals_ClearsStartedJournalWhenDirectoriesAreMissing(t *testing.T) {
	db := setupProjectTestDB(t)
	require.NoError(t, db.AutoMigrate(&models.KVEntry{}))
	ctx := context.Background()

	projectsDir := t.TempDir()
	oldDir := "nginx"
	newDir := "web"
	oldPath := filepath.Join(projectsDir, oldDir)
	newPath := filepath.Join(projectsDir, newDir)

	project := &models.Project{
		BaseModel: models.BaseModel{ID: "proj-rename-missing-directories"},
		Name:      "nginx",
		DirName:   &oldDir,
		Path:      oldPath,
		Status:    models.ProjectStatusStopped,
	}
	require.NoError(t, db.Create(project).Error)

	kvService := NewKVService(db)
	svc := NewProjectService(db, nil, nil, nil, nil, nil, config.Load()).WithKVService(kvService)
	journal := projectRenameJournalInternal{
		ProjectID:  project.ID,
		OldName:    "nginx",
		NewName:    "web",
		OldPath:    oldPath,
		NewPath:    newPath,
		OldDirName: &oldDir,
		NewDirName: newDir,
		Phase:      projectRenameJournalPhaseStartedInternal,
	}
	payload, err := json.Marshal(journal)
	require.NoError(t, err)
	require.NoError(t, kvService.Set(ctx, projectRenameJournalKeyInternal(project.ID), string(payload)))

	require.NoError(t, svc.RecoverProjectRenameJournals(ctx))

	require.NoDirExists(t, oldPath)
	require.NoDirExists(t, newPath)
	_, ok, err := kvService.Get(ctx, projectRenameJournalKeyInternal(project.ID))
	require.NoError(t, err)
	require.False(t, ok)
}
