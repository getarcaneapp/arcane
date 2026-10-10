package sync

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPersistGitSyncEnvFiles_UsesPreparedState(t *testing.T) {
	projectsDir := t.TempDir()
	t.Setenv("PROJECTS_DIRECTORY", projectsDir)

	dirName := "git-sync-prepared-state"
	projectPath := filepath.Join(projectsDir, dirName)
	require.NoError(t, os.MkdirAll(projectPath, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(projectPath, "compose.yaml"), []byte("services:\n  app:\n    image: nginx:alpine\n"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(projectPath, ".env"), []byte("BASE=git\nTOKEN=local\n"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(projectPath, ".env.git"), []byte("BASE=git\nTOKEN=git\n"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(projectPath, "project.env"), []byte("TOKEN=local\n"), 0o600))

	update, err := PrepareGitSyncEnvUpdate(projectPath, new("BASE=git-updated\nTOKEN=git\nREMOTE=1\n"))
	require.NoError(t, err)
	require.NotNil(t, update.EffectiveContent)

	require.NoError(t, os.WriteFile(filepath.Join(projectPath, "project.env"), []byte("TOKEN=unexpected\n"), 0o600))

	require.NoError(t, PersistGitSyncEnvFiles(t.Context(), projectPath, projectsDir, update))

	overrideBytes, readErr := os.ReadFile(filepath.Join(projectPath, "project.env"))
	require.NoError(t, readErr)
	assert.Equal(t, "TOKEN=local\n", string(overrideBytes))

	effectiveBytes, readErr := os.ReadFile(filepath.Join(projectPath, ".env"))
	require.NoError(t, readErr)
	assert.Equal(t, "BASE=git-updated\nTOKEN=local\nREMOTE=1\n", string(effectiveBytes))
}
