package services

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
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

func TestProjectRenameJournalTargetsCopiedInternal(t *testing.T) {
	require.False(t, projectRenameJournalTargetsCopiedInternal(projectRenameJournalPhaseStartedInternal))
	require.False(t, projectRenameJournalTargetsCopiedInternal(projectRenameJournalPhaseProjectStateRolledBack))
	require.True(t, projectRenameJournalTargetsCopiedInternal(projectRenameJournalPhaseTargetsCopiedInternal))
	require.True(t, projectRenameJournalTargetsCopiedInternal(projectRenameJournalPhaseOldVolumesRemoved))
	require.True(t, projectRenameJournalTargetsCopiedInternal(projectRenameJournalPhaseProjectStateCommitted))
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
		Phase:      projectRenameJournalPhaseOldVolumesRemoved,
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
	require.NoError(t, svc.finalizeProjectRenameAfterCommitInternal(ctx, project.ID, migration, journal, &journalActive))
	require.True(t, migration.commitCalled)
	require.False(t, journalActive)

	_, ok, err := kvService.Get(ctx, projectRenameJournalKeyInternal(project.ID))
	require.NoError(t, err)
	require.False(t, ok)
}

func TestProjectService_FinalizeProjectRenameAfterCommit_KeepsJournalWhenSourceCleanupFails(t *testing.T) {
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
	err := svc.finalizeProjectRenameAfterCommitInternal(ctx, project.ID, migration, journal, &journalActive)
	require.Error(t, err)
	require.True(t, migration.commitCalled)
	require.True(t, journalActive)

	raw, ok, err := kvService.Get(ctx, projectRenameJournalKeyInternal(project.ID))
	require.NoError(t, err)
	require.True(t, ok)

	var updatedJournal projectRenameJournalInternal
	require.NoError(t, json.Unmarshal([]byte(raw), &updatedJournal))
	require.Equal(t, projectRenameJournalPhaseProjectStateCommitted, updatedJournal.Phase)
}
