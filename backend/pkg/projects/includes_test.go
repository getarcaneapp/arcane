package projects

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	pkgutils "github.com/getarcaneapp/arcane/backend/v2/pkg/utils"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseIncludes_NormalizesRelativePaths(t *testing.T) {
	t.Parallel()

	projectDir := t.TempDir()
	composePath := filepath.Join(projectDir, "compose.yaml")
	includePath := filepath.Join(projectDir, "includes", "config.yaml")

	requireNoError := func(err error) {
		t.Helper()

		require.NoError(t, err,
			"unexpected error: %v", err)

	}

	requireNoError(os.MkdirAll(filepath.Dir(includePath), 0o755))
	requireNoError(os.WriteFile(includePath, []byte("services: {}\n"), 0o600))
	requireNoError(os.WriteFile(composePath, []byte("include:\n  - ./includes/config.yaml\n"), 0o600))

	includes, err := ParseIncludes(composePath, nil, false)
	requireNoError(err)

	require.Len(t, includes, 1,
		"expected 1 include, got %d", len(includes))

	require.Equal(t, "includes/config.yaml", includes[0].RelativePath,
		"unexpected relative path: got %q, want %q", includes[0].RelativePath, "includes/config.yaml")

}

func TestParseIncludes_ExpandsArrayPathForm(t *testing.T) {
	t.Parallel()

	projectDir := t.TempDir()
	composePath := filepath.Join(projectDir, "compose.yaml")

	requireNoError := func(err error) {
		t.Helper()

		require.NoError(t, err,
			"unexpected error: %v", err)

	}

	requireNoError(os.WriteFile(composePath, []byte("include:\n  - path:\n      - ./base.yaml\n      - ./override.yaml\n"), 0o600))

	includes, err := ParseIncludes(composePath, nil, false)
	requireNoError(err)

	require.Len(t, includes, 2,
		"expected 2 includes, got %d", len(includes))

	require.False(t, includes[0].RelativePath != "base.yaml" || includes[1].RelativePath != "override.yaml",
		"unexpected relative paths: %q, %q", includes[0].RelativePath, includes[1].RelativePath)

}

func TestWriteIncludeFilePermissions(t *testing.T) {
	// Save original perms
	origFilePerm := pkgutils.FilePerm
	origDirPerm := pkgutils.DirPerm
	defer func() {
		pkgutils.FilePerm = origFilePerm
		pkgutils.DirPerm = origDirPerm
	}()

	projectDir := t.TempDir()
	includePath := filepath.Join("includes", "config.yaml")
	content := "services: {}\n"

	t.Run("Uses custom permissions", func(t *testing.T) {
		pkgutils.FilePerm = 0o600
		pkgutils.DirPerm = 0o700
		{

			err := WriteIncludeFile(projectDir, includePath, content)
			require.NoError(t, err,
				"WriteIncludeFile() returned error: %v", err)
		}

		targetPath := filepath.Join(projectDir, includePath)
		info, err := os.Stat(targetPath)

		require.NoError(t, err,
			"failed to stat include file: %v", err)

		// On Linux/macOS, we can check permissions. On Windows, it's more limited.
		if runtime.GOOS != "windows" {

			assert.Equal(t, os.FileMode(0o600), info.Mode().Perm(),
				"unexpected file permissions: got %o, want %o", info.Mode().Perm(), 0o600)

			dirInfo, err := os.Stat(filepath.Dir(targetPath))

			require.NoError(t, err,
				"failed to stat include directory: %v", err)

			assert.Equal(t, os.FileMode(0o700), dirInfo.Mode().Perm(),
				"unexpected directory permissions: got %o, want %o", dirInfo.Mode().Perm(), 0o700)

		}
	})
}

func TestWriteIncludeFileCreatesSafeDirectory(t *testing.T) {
	t.Parallel()

	projectDir := t.TempDir()
	includePath := filepath.Join("includes", "config.yaml")
	content := "services: {}\n"
	{

		err := WriteIncludeFile(projectDir, includePath, content)
		require.NoError(t, err,
			"WriteIncludeFile() returned error: %v", err)
	}

	targetPath := filepath.Join(projectDir, includePath)
	data, err := os.ReadFile(targetPath)

	require.NoError(t, err,
		"failed to read include file: %v", err)

	require.Equal(t, content, string(data),
		"unexpected file content: got %q, want %q", string(data), content)

}

func TestWriteIncludeFileRejectsSymlinkEscape(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink creation requires elevated privileges on Windows")
	}
	t.Parallel()

	projectDir := t.TempDir()
	outsideDir := t.TempDir()

	linkPath := filepath.Join(projectDir, "link")
	{
		err := os.Symlink(outsideDir, linkPath)
		require.NoError(t, err,
			"failed to create symlink: %v", err)
	}

	includePath := filepath.Join("link", "escape.yaml")
	err := WriteIncludeFile(projectDir, includePath, "malicious: true\n")

	require.Error(t, err,
		"WriteIncludeFile() succeeded but expected rejection for symlink escape")

}
