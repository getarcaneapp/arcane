package projects

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

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

func TestValidateIncludePathForWriteRejectsSymlinkEscape(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink creation requires elevated privileges on Windows")
	}
	t.Parallel()

	projectDir := t.TempDir()
	outsideDir := t.TempDir()

	linkPath := filepath.Join(projectDir, "link")
	require.NoError(t, os.Symlink(outsideDir, linkPath), "failed to create symlink")

	includePath := filepath.Join("link", "escape.yaml")
	_, err := ValidateIncludePathForWrite(projectDir, includePath)
	require.Error(t, err, "ValidateIncludePathForWrite() succeeded but expected rejection for symlink escape")
}
