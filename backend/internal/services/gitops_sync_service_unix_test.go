//go:build unix

package services

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/getarcaneapp/arcane/backend/v2/internal/models"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/projects"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestGitOpsSyncService_SyncProjectDirectory_PreservesUnreadableBindMountData verifies
// that re-syncing an existing GitOps project tolerates a foreign-owned unreadable file
// inside the project directory (e.g. data a container wrote through a relative bind
// mount, owned by another UID with restrictive perms). The sync must succeed instead
// of aborting on the permission error while staging/backing up, and the unreadable
// file must be left untouched rather than pruned when the staged tree is promoted.
func TestGitOpsSyncService_SyncProjectDirectory_PreservesUnreadableBindMountData(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("permission bits are ignored when running as root")
	}

	ctx := context.Background()
	svc, db, projectsDir := setupGitOpsSyncDirectoryTestService(t)

	projectPath := filepath.Join(projectsDir, "demo-project")
	writeFileInternal(t, projectPath, "docker-compose.yaml", []byte(`services:
  app:
    image: nginx:1.26-alpine
`))

	// A container wrote a foreign-owned, unreadable file into a relative bind-mount
	// directory inside the project. Arcane's PUID cannot read it.
	secretPath := filepath.Join(projectPath, "data", "secret.bin")
	require.NoError(t, os.MkdirAll(filepath.Dir(secretPath), 0o755))
	require.NoError(t, os.WriteFile(secretPath, []byte("supersecret"), 0o644))
	require.NoError(t, os.Chmod(secretPath, 0o000))
	t.Cleanup(func() { _ = os.Chmod(secretPath, 0o644) })

	project := &models.Project{
		BaseModel: models.BaseModel{ID: "proj-unreadable-bindmount"},
		Name:      "demo-project",
		DirName:   new("demo-project"),
		Path:      projectPath,
		Status:    models.ProjectStatusStopped,
	}
	require.NoError(t, db.Create(project).Error)

	oldSyncedFilesJSON, err := json.Marshal([]string{"docker-compose.yaml"})
	require.NoError(t, err)

	sync := &models.GitOpsSync{
		BaseModel:     models.BaseModel{ID: "sync-unreadable-bindmount"},
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
	}

	updatedProject, _, created, _, err := svc.syncProjectDirectoryInternal(ctx, sync, syncFiles, models.User{})
	require.NoError(t, err)
	require.NotNil(t, updatedProject)
	require.False(t, created)

	// The unreadable foreign file survived promotion untouched (preserved, not pruned).
	require.NoError(t, os.Chmod(secretPath, 0o644))
	secretBytes, err := os.ReadFile(secretPath)
	require.NoError(t, err)
	assert.Equal(t, "supersecret", string(secretBytes))

	// The managed compose file was still updated by the sync.
	composeBytes, err := os.ReadFile(filepath.Join(updatedProject.Path, "docker-compose.yaml"))
	require.NoError(t, err)
	assert.Contains(t, string(composeBytes), "nginx:1.27-alpine")
}
