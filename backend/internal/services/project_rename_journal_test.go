package services

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/getarcaneapp/arcane/backend/v2/internal/config"
	"github.com/getarcaneapp/arcane/backend/v2/internal/models"
	"github.com/stretchr/testify/require"
)

func TestProjectService_RecoverProjectRenameJournals_RollsBackUncommittedDirectoryRename(t *testing.T) {
	db := setupProjectTestDB(t)
	require.NoError(t, db.AutoMigrate(&models.KVEntry{}))
	ctx := context.Background()

	projectsDir := t.TempDir()
	oldDir := "nginx"
	newDir := "web"
	oldPath := filepath.Join(projectsDir, oldDir)
	newPath := filepath.Join(projectsDir, newDir)
	require.NoError(t, os.MkdirAll(newPath, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(newPath, "compose.yaml"), []byte("services: {}\n"), 0o600))

	project := &models.Project{
		BaseModel: models.BaseModel{ID: "proj-rename-recovery"},
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
		Phase:      projectRenameJournalPhaseTargetsCopiedInternal,
	}
	payload, err := json.Marshal(journal)
	require.NoError(t, err)
	require.NoError(t, kvService.Set(ctx, projectRenameJournalKeyInternal(project.ID), string(payload)))

	require.NoError(t, svc.RecoverProjectRenameJournals(ctx))

	require.FileExists(t, filepath.Join(oldPath, "compose.yaml"))
	require.NoDirExists(t, newPath)
	_, ok, err := kvService.Get(ctx, projectRenameJournalKeyInternal(project.ID))
	require.NoError(t, err)
	require.False(t, ok)

	var fromDB models.Project
	require.NoError(t, db.First(&fromDB, "id = ?", project.ID).Error)
	require.Equal(t, "nginx", fromDB.Name)
	require.Equal(t, oldPath, fromDB.Path)
	require.NotNil(t, fromDB.DirName)
	require.Equal(t, oldDir, *fromDB.DirName)
}

func TestProjectService_RecoverProjectRenameJournals_StartedPhaseSkipsVolumeRollback(t *testing.T) {
	db := setupProjectTestDB(t)
	require.NoError(t, db.AutoMigrate(&models.KVEntry{}))
	ctx := context.Background()

	projectsDir := t.TempDir()
	oldDir := "nginx"
	newDir := "web"
	oldPath := filepath.Join(projectsDir, oldDir)
	newPath := filepath.Join(projectsDir, newDir)
	require.NoError(t, os.MkdirAll(oldPath, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(oldPath, "compose.yaml"), []byte("services: {}\n"), 0o600))

	project := &models.Project{
		BaseModel: models.BaseModel{ID: "proj-rename-started-volume-recovery"},
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

	require.FileExists(t, filepath.Join(oldPath, "compose.yaml"))
	require.NoDirExists(t, newPath)
	_, ok, err := kvService.Get(ctx, projectRenameJournalKeyInternal(project.ID))
	require.NoError(t, err)
	require.False(t, ok)

	var fromDB models.Project
	require.NoError(t, db.First(&fromDB, "id = ?", project.ID).Error)
	require.Equal(t, "nginx", fromDB.Name)
	require.Equal(t, oldPath, fromDB.Path)
	require.NotNil(t, fromDB.DirName)
	require.Equal(t, oldDir, *fromDB.DirName)
}

func TestProjectService_RecoverProjectRenameJournals_ClearsStartedJournalWhenDirectoryPathsMissing(t *testing.T) {
	db := setupProjectTestDB(t)
	require.NoError(t, db.AutoMigrate(&models.KVEntry{}))
	ctx := context.Background()

	projectsDir := t.TempDir()
	oldDir := "nginx"
	newDir := "web"
	oldPath := filepath.Join(projectsDir, oldDir)
	newPath := filepath.Join(projectsDir, newDir)

	project := &models.Project{
		BaseModel: models.BaseModel{ID: "proj-rename-missing-paths"},
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

	_, ok, err := kvService.Get(ctx, projectRenameJournalKeyInternal(project.ID))
	require.NoError(t, err)
	require.False(t, ok)
	require.NoDirExists(t, oldPath)
	require.NoDirExists(t, newPath)
}

func TestProjectService_RecoverProjectRenameJournals_ClearsPreservedTargetJournalWhenPathExists(t *testing.T) {
	db := setupProjectTestDB(t)
	require.NoError(t, db.AutoMigrate(&models.KVEntry{}))
	ctx := context.Background()

	projectsDir := t.TempDir()
	oldDir := "nginx"
	newDir := "web"
	oldPath := filepath.Join(projectsDir, oldDir)
	newPath := filepath.Join(projectsDir, newDir)
	require.NoError(t, os.MkdirAll(oldPath, 0o755))

	project := &models.Project{
		BaseModel: models.BaseModel{ID: "proj-rename-preserved-target"},
		Name:      "nginx",
		DirName:   &oldDir,
		Path:      oldPath,
		Status:    models.ProjectStatusStopped,
	}
	require.NoError(t, db.Create(project).Error)

	var targetRemoved bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/volumes/nginx_data"):
			http.NotFound(w, r)
		case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/volumes/web_data"):
			w.Header().Set("Content-Type", "application/json")
			require.NoError(t, json.NewEncoder(w).Encode(map[string]any{"Name": "web_data"}))
		case r.Method == http.MethodDelete && strings.HasSuffix(r.URL.Path, "/volumes/web_data"):
			targetRemoved = true
			w.WriteHeader(http.StatusNoContent)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)

	kvService := NewKVService(db)
	svc := NewProjectService(db, nil, nil, nil, &DockerClientService{client: newTestDockerClient(t, server)}, nil, config.Load()).WithKVService(kvService)
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

	require.NoError(t, svc.RecoverProjectRenameJournals(ctx))

	_, ok, err := kvService.Get(ctx, projectRenameJournalKeyInternal(project.ID))
	require.NoError(t, err)
	require.False(t, ok)
	require.False(t, targetRemoved, "preserved target volume should remain for manual inspection")
	require.DirExists(t, oldPath)
}

func TestProjectRenameJournalTargetsCopiedInternal(t *testing.T) {
	require.False(t, projectRenameJournalTargetsCopiedInternal(projectRenameJournalPhaseStartedInternal))
	require.False(t, projectRenameJournalTargetsCopiedInternal(projectRenameJournalPhaseProjectStateRolledBackInternal))
	require.True(t, projectRenameJournalTargetsCopiedInternal(projectRenameJournalPhaseTargetsCopiedInternal))
	require.True(t, projectRenameJournalTargetsCopiedInternal(projectRenameJournalPhaseOldVolumesRemovedInternal))
	require.True(t, projectRenameJournalTargetsCopiedInternal(projectRenameJournalPhaseProjectStateCommittedInternal))
}

func TestProjectService_RecoverProjectRenameJournals_ClearsCommittedJournal(t *testing.T) {
	db := setupProjectTestDB(t)
	require.NoError(t, db.AutoMigrate(&models.KVEntry{}))
	ctx := context.Background()

	projectsDir := t.TempDir()
	oldDir := "nginx"
	newDir := "web"
	oldPath := filepath.Join(projectsDir, oldDir)
	newPath := filepath.Join(projectsDir, newDir)
	require.NoError(t, os.MkdirAll(newPath, 0o755))

	project := &models.Project{
		BaseModel: models.BaseModel{ID: "proj-rename-committed"},
		Name:      "web",
		DirName:   &newDir,
		Path:      newPath,
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
		Phase:      projectRenameJournalPhaseOldVolumesRemovedInternal,
	}
	payload, err := json.Marshal(journal)
	require.NoError(t, err)
	require.NoError(t, kvService.Set(ctx, projectRenameJournalKeyInternal(project.ID), string(payload)))

	require.NoError(t, svc.RecoverProjectRenameJournals(ctx))

	_, ok, err := kvService.Get(ctx, projectRenameJournalKeyInternal(project.ID))
	require.NoError(t, err)
	require.False(t, ok)
	require.DirExists(t, newPath)
}

func TestProjectService_FinalizeProjectRenameAfterCommit_ClearsJournalAfterSourceCleanup(t *testing.T) {
	db := setupProjectTestDB(t)
	require.NoError(t, db.AutoMigrate(&models.KVEntry{}))
	ctx := context.Background()

	oldDir := "nginx"
	newDir := "web"
	project := &models.Project{
		BaseModel: models.BaseModel{ID: "proj-rename-old-volumes-removed"},
		Name:      "web",
		DirName:   &newDir,
		Path:      filepath.Join(t.TempDir(), newDir),
		Status:    models.ProjectStatusStopped,
	}
	require.NoError(t, db.Create(project).Error)

	kvService := NewKVService(db)
	svc := NewProjectService(db, nil, nil, nil, nil, nil, config.Load()).WithKVService(kvService)
	journal := &projectRenameJournalInternal{
		ProjectID:  project.ID,
		OldName:    "nginx",
		NewName:    "web",
		OldPath:    filepath.Join(t.TempDir(), oldDir),
		NewPath:    project.Path,
		OldDirName: &oldDir,
		NewDirName: newDir,
		Phase:      projectRenameJournalPhaseTargetsCopiedInternal,
	}
	require.NoError(t, svc.writeProjectRenameJournalInternal(ctx, journal, projectRenameJournalPhaseTargetsCopiedInternal))

	migration := &fakeProjectVolumeRenameMigrationInternal{}
	journalActive := true
	svc.finalizeProjectRenameAfterCommitInternal(ctx, project.ID, migration, journal, &journalActive)
	require.True(t, migration.commitCalled)
	require.False(t, journalActive)

	_, ok, err := kvService.Get(ctx, projectRenameJournalKeyInternal(project.ID))
	require.NoError(t, err)
	require.False(t, ok)
}

func TestProjectService_FinalizeProjectRenameAfterCommit_ClearsJournalWhenSourceCleanupFails(t *testing.T) {
	db := setupProjectTestDB(t)
	require.NoError(t, db.AutoMigrate(&models.KVEntry{}))
	ctx := context.Background()

	oldDir := "nginx"
	newDir := "web"
	project := &models.Project{
		BaseModel: models.BaseModel{ID: "proj-rename-cleanup-failure"},
		Name:      "web",
		DirName:   &newDir,
		Path:      filepath.Join(t.TempDir(), newDir),
		Status:    models.ProjectStatusStopped,
	}
	require.NoError(t, db.Create(project).Error)

	kvService := NewKVService(db)
	svc := NewProjectService(db, nil, nil, nil, nil, nil, config.Load()).WithKVService(kvService)
	journal := &projectRenameJournalInternal{
		ProjectID:  project.ID,
		OldName:    "nginx",
		NewName:    "web",
		OldPath:    filepath.Join(t.TempDir(), oldDir),
		NewPath:    project.Path,
		OldDirName: &oldDir,
		NewDirName: newDir,
		Phase:      projectRenameJournalPhaseTargetsCopiedInternal,
	}
	require.NoError(t, svc.writeProjectRenameJournalInternal(ctx, journal, projectRenameJournalPhaseTargetsCopiedInternal))

	migration := &fakeProjectVolumeRenameMigrationInternal{commitErr: errors.New("source cleanup failed")}
	journalActive := true
	svc.finalizeProjectRenameAfterCommitInternal(ctx, project.ID, migration, journal, &journalActive)
	require.True(t, migration.commitCalled)
	require.False(t, journalActive)

	_, ok, err := kvService.Get(ctx, projectRenameJournalKeyInternal(project.ID))
	require.NoError(t, err)
	require.False(t, ok)
}
