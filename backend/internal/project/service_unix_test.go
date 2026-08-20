//go:build unix

package project

import (
	"github.com/getarcaneapp/arcane/backend/v2/internal/common"

	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/getarcaneapp/arcane/backend/v2/internal/config"
	"github.com/getarcaneapp/arcane/backend/v2/internal/event"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestProjectService_ApplyGitSyncProjectFiles_TolerantOfPermissionLockedEnv verifies
// a permission-locked (e.g. chmod 000, foreign-owned) .env does not brick a git sync:
// the compose file still updates, the sync succeeds, and the locked file is left
// untouched instead of aborting the whole update.
func TestProjectService_ApplyGitSyncProjectFiles_TolerantOfPermissionLockedEnv(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("permission bits are ignored when running as root")
	}

	db := setupProjectTestDB(t)
	ctx := context.Background()

	projectsDir := t.TempDir()
	t.Setenv("PROJECTS_DIRECTORY", projectsDir)

	settingsService, err := newSettingsServiceForTestInternal(t, ctx, db)
	require.NoError(t, err)

	eventService := event.NewEventService(db, nil, nil)
	svc := NewProjectService(db, settingsService, eventService, nil, nil, nil, nil, nil, config.Load())

	dirName := "git-sync-locked-env"
	projectPath := filepath.Join(projectsDir, dirName)
	require.NoError(t, os.MkdirAll(projectPath, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(projectPath, "compose.yaml"), []byte("services:\n  app:\n    image: nginx:alpine\n"), 0o600))

	envPath := filepath.Join(projectPath, ".env")
	require.NoError(t, os.WriteFile(envPath, []byte("FOO=locked\n"), 0o600))
	require.NoError(t, os.Chmod(envPath, 0o000))
	t.Cleanup(func() { _ = os.Chmod(envPath, 0o644) })

	project := &Project{
		ID:      "proj-git-sync-locked-env",
		Name:    "git-sync-locked-env",
		DirName: &dirName,
		Path:    projectPath,
		Status:  ProjectStatusStopped,
	}
	require.NoError(t, db.Create(project).Error)

	updated, err := svc.ApplyGitSyncProjectFiles(ctx, project.ID, "services:\n  app:\n    image: nginx:1.27-alpine\n", new("FOO=fromgit\n"), nil, "", common.User{
		ID:       "u1",
		Username: "tester",
	})
	require.NoError(t, err)
	require.NotNil(t, updated)

	composeBytes, err := os.ReadFile(filepath.Join(projectPath, "compose.yaml"))
	require.NoError(t, err)
	assert.Contains(t, string(composeBytes), "nginx:1.27-alpine")

	// The locked .env survived untouched — sync did not attempt to overwrite it.
	require.NoError(t, os.Chmod(envPath, 0o644))
	envBytes, err := os.ReadFile(envPath)
	require.NoError(t, err)
	assert.Equal(t, "FOO=locked\n", string(envBytes))
}
