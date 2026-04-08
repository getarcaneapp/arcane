package projects

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	pkgutils "github.com/getarcaneapp/arcane/backend/pkg/utils"
)

// writeFile is a small test helper.
func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

// TestBuildServiceOriginMap_PreservesIncludeProjectDirectory is the regression
// test for issue #2264. Services pulled in via `include:` with an explicit
// `project_directory` must be mapped to the include's working dir, not the
// top-level compose file's directory. Losing this mapping caused Postgres
// containers to be recreated against empty bind-mount paths.
func TestBuildServiceOriginMap_PreservesIncludeProjectDirectory(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	topDir := filepath.Join(root, "top")
	includeDir := filepath.Join(root, "media", "seerr")

	topCompose := filepath.Join(topDir, "compose.yaml")
	includeCompose := filepath.Join(includeDir, "compose.yaml")

	writeFile(t, topCompose, `
include:
  - path: `+filepath.ToSlash(includeCompose)+`
    project_directory: `+filepath.ToSlash(includeDir)+`
services:
  top_only:
    image: nginx:alpine
`)
	writeFile(t, includeCompose, `
services:
  seerr:
    image: linuxserver/jellyseerr
    volumes:
      - ./config:/config
`)

	originMap := buildServiceOriginMapInternal(context.Background(), topCompose, EnvMap{})

	origin, ok := originMap["seerr"]
	if !ok {
		t.Fatalf("expected seerr to be in origin map, got %#v", originMap)
	}
	if origin.WorkingDir != includeDir {
		t.Errorf("seerr WorkingDir = %q, want %q", origin.WorkingDir, includeDir)
	}
	if origin.ComposeFile != includeCompose {
		t.Errorf("seerr ComposeFile = %q, want %q", origin.ComposeFile, includeCompose)
	}

	if _, present := originMap["top_only"]; present {
		t.Errorf("top_only should NOT be in origin map (it's defined in the top-level compose), got %#v", originMap["top_only"])
	}
}

// TestBuildServiceOriginMap_DefaultsToIncludeFileDirectory verifies the
// Docker Compose spec behavior: when an include has no `project_directory`,
// the include's own directory is used as the working dir.
func TestBuildServiceOriginMap_DefaultsToIncludeFileDirectory(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	topDir := filepath.Join(root, "top")
	includeDir := filepath.Join(root, "shared")

	topCompose := filepath.Join(topDir, "compose.yaml")
	includeCompose := filepath.Join(includeDir, "compose.yaml")

	writeFile(t, topCompose, `
include:
  - `+filepath.ToSlash(includeCompose)+`
`)
	writeFile(t, includeCompose, `
services:
  shared_svc:
    image: alpine
`)

	originMap := buildServiceOriginMapInternal(context.Background(), topCompose, EnvMap{})

	origin, ok := originMap["shared_svc"]
	if !ok {
		t.Fatalf("expected shared_svc in origin map")
	}
	if origin.WorkingDir != includeDir {
		t.Errorf("shared_svc WorkingDir = %q, want %q (include file's dir)", origin.WorkingDir, includeDir)
	}
}

// TestBuildServiceOriginMap_NestedIncludes verifies the recursive walk and
// the cycle guard.
func TestBuildServiceOriginMap_NestedIncludes(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	aDir := filepath.Join(root, "a")
	bDir := filepath.Join(root, "b")
	cDir := filepath.Join(root, "c")

	a := filepath.Join(aDir, "compose.yaml")
	b := filepath.Join(bDir, "compose.yaml")
	c := filepath.Join(cDir, "compose.yaml")

	writeFile(t, a, `
include:
  - path: `+filepath.ToSlash(b)+`
`)
	writeFile(t, b, `
include:
  - path: `+filepath.ToSlash(c)+`
  - path: `+filepath.ToSlash(a)+`
services:
  b_svc:
    image: alpine
`)
	writeFile(t, c, `
services:
  c_svc:
    image: alpine
`)

	originMap := buildServiceOriginMapInternal(context.Background(), a, EnvMap{})

	if origin := originMap["b_svc"]; origin.WorkingDir != bDir {
		t.Errorf("b_svc WorkingDir = %q, want %q", origin.WorkingDir, bDir)
	}
	if origin := originMap["c_svc"]; origin.WorkingDir != cDir {
		t.Errorf("c_svc WorkingDir = %q, want %q", origin.WorkingDir, cDir)
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
