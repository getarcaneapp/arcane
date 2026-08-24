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

func TestResolvePathWithinDir(t *testing.T) {
	workingDir := filepath.Join(string(filepath.Separator), "tmp", "stack")

	t.Run("allows paths within the directory", func(t *testing.T) {
		resolved, err := ResolvePathWithinDir(workingDir, filepath.Join("configs", "app.env"))
		require.NoError(t, err)
		require.Equal(t, filepath.Join(workingDir, "configs", "app.env"), resolved)
	})

	t.Run("rejects escaping paths", func(t *testing.T) {
		_, err := ResolvePathWithinDir(workingDir, filepath.Join("..", "..", "etc", "shadow"))
		require.Error(t, err)
		require.Contains(t, err.Error(), "escapes directory")
	})
}

func TestReadProjectFiles(t *testing.T) {
	t.Run("detects compose path when not provided", func(t *testing.T) {
		projectPath := t.TempDir()
		require.NoError(t, os.WriteFile(filepath.Join(projectPath, "compose.yaml"), []byte("services:\n  app:\n    image: nginx:alpine\n"), 0o644))
		require.NoError(t, os.WriteFile(filepath.Join(projectPath, ".env"), []byte("TZ=UTC\n"), 0o644))

		composeContent, envContent, err := ReadProjectFiles(t.Context(), projectPath, "")
		require.NoError(t, err)
		assert.Contains(t, composeContent, "services:")
		assert.Equal(t, "TZ=UTC\n", envContent)
	})

	t.Run("uses explicit compose path when provided", func(t *testing.T) {
		projectPath := t.TempDir()
		require.NoError(t, os.WriteFile(filepath.Join(projectPath, "radarr.yaml"), []byte("services:\n  app:\n    image: lscr.io/linuxserver/radarr:latest\n"), 0o644))
		require.NoError(t, os.WriteFile(filepath.Join(projectPath, ".env"), []byte("TZ=UTC\n"), 0o644))

		composeContent, envContent, err := ReadProjectFiles(t.Context(), projectPath, filepath.Join(projectPath, "radarr.yaml"))
		require.NoError(t, err)
		assert.Contains(t, composeContent, "radarr")
		assert.Equal(t, "TZ=UTC\n", envContent)
	})
}
