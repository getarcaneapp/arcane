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

func TestResolveConfiguredContainerDirectory(t *testing.T) {
	t.Run("uses default when empty", func(t *testing.T) {
		got := ResolveConfiguredContainerDirectory("", "/app/data/swarm/sources")
		assert.Equal(t, "/app/data/swarm/sources", got)
	})

	t.Run("preserves plain absolute path", func(t *testing.T) {
		got := ResolveConfiguredContainerDirectory("/app/data/custom/stacks", "/app/data/swarm/sources")
		assert.Equal(t, "/app/data/custom/stacks", got)
	})

	t.Run("extracts container path from bind mapping", func(t *testing.T) {
		got := ResolveConfiguredContainerDirectory("/app/data/swarm/sources:/srv/arcane/swarm", "/app/data/swarm/sources")
		assert.Equal(t, "/app/data/swarm/sources", got)
	})

	t.Run("normalizes relative path", func(t *testing.T) {
		cwd := t.TempDir()
		t.Chdir(cwd)

		got := ResolveConfiguredContainerDirectory("data/swarm/sources", "/app/data/swarm/sources")
		assert.Equal(t, filepath.Join(cwd, "data", "swarm", "sources"), got)
	})
}
