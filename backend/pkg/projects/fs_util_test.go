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

func TestReadProjectFiles(t *testing.T) {
	t.Run("detects compose path when not provided", func(t *testing.T) {
		projectPath := t.TempDir()
		require.NoError(t, os.WriteFile(filepath.Join(projectPath, "compose.yaml"), []byte("services:\n  app:\n    image: nginx:alpine\n"), 0o644))
		require.NoError(t, os.WriteFile(filepath.Join(projectPath, ".env"), []byte("TZ=UTC\n"), 0o644))

		composeContent, envContent, err := ReadProjectFiles(projectPath, "")
		require.NoError(t, err)
		assert.Contains(t, composeContent, "services:")
		assert.Equal(t, "TZ=UTC\n", envContent)
	})

	t.Run("uses explicit compose path when provided", func(t *testing.T) {
		projectPath := t.TempDir()
		require.NoError(t, os.WriteFile(filepath.Join(projectPath, "radarr.yaml"), []byte("services:\n  app:\n    image: lscr.io/linuxserver/radarr:latest\n"), 0o644))
		require.NoError(t, os.WriteFile(filepath.Join(projectPath, ".env"), []byte("TZ=UTC\n"), 0o644))

		composeContent, envContent, err := ReadProjectFiles(projectPath, filepath.Join(projectPath, "radarr.yaml"))
		require.NoError(t, err)
		assert.Contains(t, composeContent, "radarr")
		assert.Equal(t, "TZ=UTC\n", envContent)
	})
}

func TestComposeVolumeKeysWithExplicitName(t *testing.T) {
	projectPath := t.TempDir()
	rootCompose := filepath.Join(projectPath, "compose.yaml")
	includeCompose := filepath.Join(projectPath, "included.yaml")

	require.NoError(t, os.WriteFile(rootCompose, []byte(`
services:
  app:
    image: nginx:alpine
    volumes:
      - data:/data
      - fixed:/fixed
      - env_named:/env
      - env_default:/env-default
      - env_unbraced:/env-unbraced
      - escaped:/escaped
      - scalar:/scalar
volumes:
  data:
    driver: local
  fixed:
    name: app-data
  env_named:
    name: ${APP_VOLUME}
  env_default:
    name: ${APP_VOLUME:-app-data}
  env_unbraced:
    name: $APP_VOLUME
  escaped:
    name: $${APP_VOLUME}
  scalar:
  inline: {}
`), 0o644))
	require.NoError(t, os.WriteFile(includeCompose, []byte(`
volumes:
  included:
    name: included-fixed
  included_implicit:
    driver: local
`), 0o644))

	explicit, err := ComposeVolumeKeysWithExplicitName([]string{rootCompose, includeCompose})
	require.NoError(t, err)

	assert.Contains(t, explicit, "fixed")
	assert.Contains(t, explicit, "escaped")
	assert.Contains(t, explicit, "included")
	assert.NotContains(t, explicit, "data")
	assert.NotContains(t, explicit, "env_named")
	assert.NotContains(t, explicit, "env_default")
	assert.NotContains(t, explicit, "env_unbraced")
	assert.NotContains(t, explicit, "scalar")
	assert.NotContains(t, explicit, "inline")
	assert.NotContains(t, explicit, "included_implicit")
}

func TestReadProjectDirectoryFiles_RespectsDepthAndSkipDirectories(t *testing.T) {
	projectPath := t.TempDir()

	require.NoError(t, os.WriteFile(filepath.Join(projectPath, "README.md"), []byte("root"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(projectPath, "shown.txt"), []byte("shown"), 0o644))
	require.NoError(t, os.MkdirAll(filepath.Join(projectPath, "nested", "deep"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(projectPath, "nested", "config.txt"), []byte("nested"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(projectPath, "nested", "deep", "secret.txt"), []byte("deep"), 0o644))
	require.NoError(t, os.MkdirAll(filepath.Join(projectPath, "vendor"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(projectPath, "vendor", "ignored.txt"), []byte("skip"), 0o644))
	require.NoError(t, os.MkdirAll(filepath.Join(projectPath, ".git"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(projectPath, ".git", "config"), []byte("token=secret"), 0o644))

	t.Run("uses configured max depth", func(t *testing.T) {
		dirFiles, err := ReadProjectDirectoryFiles(projectPath, map[string]bool{"shown.txt": true}, 1, "")
		require.NoError(t, err)

		relativePaths := make([]string, 0, len(dirFiles))
		for _, file := range dirFiles {
			relativePaths = append(relativePaths, file.RelativePath)
		}

		assert.ElementsMatch(t, []string{"README.md"}, relativePaths)
	})

	t.Run("uses configured skip directories", func(t *testing.T) {
		dirFiles, err := ReadProjectDirectoryFiles(projectPath, map[string]bool{"shown.txt": true}, 3, "vendor")
		require.NoError(t, err)

		relativePaths := make([]string, 0, len(dirFiles))
		for _, file := range dirFiles {
			relativePaths = append(relativePaths, file.RelativePath)
		}

		assert.ElementsMatch(t, []string{"README.md", filepath.Join("nested", "config.txt"), filepath.Join("nested", "deep", "secret.txt")}, relativePaths)
	})

	t.Run("always skips git directory", func(t *testing.T) {
		dirFiles, err := ReadProjectDirectoryFiles(projectPath, map[string]bool{"shown.txt": true}, 3, "vendor,nested")
		require.NoError(t, err)

		relativePaths := make([]string, 0, len(dirFiles))
		for _, file := range dirFiles {
			relativePaths = append(relativePaths, file.RelativePath)
		}

		assert.ElementsMatch(t, []string{"README.md"}, relativePaths)
	})
}
