package projects

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGetProjectsDirectory_ResolvesRelativePathAgainstBackendModuleRoot(t *testing.T) {
	repoRoot := t.TempDir()
	backendRoot := filepath.Join(repoRoot, "backend")
	require.NoError(t, os.MkdirAll(filepath.Join(repoRoot, "data", "projects"), 0o755))
	require.NoError(t, os.MkdirAll(filepath.Join(backendRoot, "internal"), 0o755))
	require.NoError(t, os.MkdirAll(filepath.Join(backendRoot, "pkg"), 0o755))
	require.NoError(t, os.MkdirAll(filepath.Join(backendRoot, "data", "projects"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(backendRoot, "go.mod"), []byte("module example.com/backend\n"), 0o644))

	t.Chdir(repoRoot)

	resolved, err := GetProjectsDirectory(context.Background(), "data/projects")
	require.NoError(t, err)
	assert.Equal(t, filepath.Join(backendRoot, "data", "projects"), resolved)
}

func TestGetProjectsDirectory_ResolvesRelativePathFromBackendWorkingDirectory(t *testing.T) {
	backendRoot := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(backendRoot, "internal"), 0o755))
	require.NoError(t, os.MkdirAll(filepath.Join(backendRoot, "pkg"), 0o755))
	require.NoError(t, os.MkdirAll(filepath.Join(backendRoot, "data", "projects"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(backendRoot, "go.mod"), []byte("module example.com/backend\n"), 0o644))

	t.Chdir(backendRoot)

	resolved, err := GetProjectsDirectory(context.Background(), "data/projects")
	require.NoError(t, err)
	assert.Equal(t, filepath.Join(backendRoot, "data", "projects"), resolved)
}
