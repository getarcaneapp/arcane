package projects

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	pkgutils "github.com/getarcaneapp/arcane/backend/pkg/utils"
)

func TestParseIncludes_NormalizesRelativePaths(t *testing.T) {
	t.Parallel()

	projectDir := t.TempDir()
	composePath := filepath.Join(projectDir, "compose.yaml")
	includePath := filepath.Join(projectDir, "includes", "config.yaml")

	requireNoError := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	}

	requireNoError(os.MkdirAll(filepath.Dir(includePath), 0o755))
	requireNoError(os.WriteFile(includePath, []byte("services: {}\n"), 0o600))
	requireNoError(os.WriteFile(composePath, []byte("include:\n  - ./includes/config.yaml\n"), 0o600))

	includes, err := ParseIncludes(composePath, nil)
	requireNoError(err)

	if len(includes) != 1 {
		t.Fatalf("expected 1 include, got %d", len(includes))
	}

	if includes[0].RelativePath != "includes/config.yaml" {
		t.Fatalf("unexpected relative path: got %q, want %q", includes[0].RelativePath, "includes/config.yaml")
	}
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

		if err := WriteIncludeFile(projectDir, includePath, content); err != nil {
			t.Fatalf("WriteIncludeFile() returned error: %v", err)
		}

		targetPath := filepath.Join(projectDir, includePath)
		info, err := os.Stat(targetPath)
		if err != nil {
			t.Fatalf("failed to stat include file: %v", err)
		}

		// On Linux/macOS, we can check permissions. On Windows, it's more limited.
		if runtime.GOOS != "windows" {
			if info.Mode().Perm() != 0o600 {
				t.Errorf("unexpected file permissions: got %o, want %o", info.Mode().Perm(), 0o600)
			}

			dirInfo, err := os.Stat(filepath.Dir(targetPath))
			if err != nil {
				t.Fatalf("failed to stat include directory: %v", err)
			}
			if dirInfo.Mode().Perm() != 0o700 {
				t.Errorf("unexpected directory permissions: got %o, want %o", dirInfo.Mode().Perm(), 0o700)
			}
		}
	})
}

func TestWriteIncludeFileCreatesSafeDirectory(t *testing.T) {
	t.Parallel()

	projectDir := t.TempDir()
	includePath := filepath.Join("includes", "config.yaml")
	content := "services: {}\n"

	if err := WriteIncludeFile(projectDir, includePath, content); err != nil {
		t.Fatalf("WriteIncludeFile() returned error: %v", err)
	}

	targetPath := filepath.Join(projectDir, includePath)
	data, err := os.ReadFile(targetPath)
	if err != nil {
		t.Fatalf("failed to read include file: %v", err)
	}

	if string(data) != content {
		t.Fatalf("unexpected file content: got %q, want %q", string(data), content)
	}
}

func TestWriteIncludeFileRejectsSymlinkEscape(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink creation requires elevated privileges on Windows")
	}
	t.Parallel()

	projectDir := t.TempDir()
	outsideDir := t.TempDir()

	linkPath := filepath.Join(projectDir, "link")
	if err := os.Symlink(outsideDir, linkPath); err != nil {
		t.Fatalf("failed to create symlink: %v", err)
	}

	includePath := filepath.Join("link", "escape.yaml")
	err := WriteIncludeFile(projectDir, includePath, "malicious: true\n")
	if err == nil {
		t.Fatalf("WriteIncludeFile() succeeded but expected rejection for symlink escape")
	}
}
