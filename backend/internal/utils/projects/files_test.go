package projects

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/getarcaneapp/arcane/backend/internal/common"
)

func TestWriteIncludeFilePermissions(t *testing.T) {
	origFilePerm := common.FilePerm
	origDirPerm := common.DirPerm
	defer func() {
		common.FilePerm = origFilePerm
		common.DirPerm = origDirPerm
	}()

	projectDir := t.TempDir()
	includePath := filepath.Join("includes", "config.yaml")
	content := "services: {}\n"

	t.Run("Uses custom permissions", func(t *testing.T) {
		common.FilePerm = 0600
		common.DirPerm = 0700

		if err := WriteIncludeFile(projectDir, includePath, content, nil); err != nil {
			t.Fatalf("WriteIncludeFile() returned error: %v", err)
		}

		targetPath := filepath.Join(projectDir, includePath)
		info, err := os.Stat(targetPath)
		if err != nil {
			t.Fatalf("failed to stat include file: %v", err)
		}

		if runtime.GOOS != "windows" {
			if info.Mode().Perm() != 0600 {
				t.Errorf("unexpected file permissions: got %o, want %o", info.Mode().Perm(), 0600)
			}

			dirInfo, err := os.Stat(filepath.Dir(targetPath))
			if err != nil {
				t.Fatalf("failed to stat include directory: %v", err)
			}
			if dirInfo.Mode().Perm() != 0700 {
				t.Errorf("unexpected directory permissions: got %o, want %o", dirInfo.Mode().Perm(), 0700)
			}
		}
	})
}

func TestWriteIncludeFileCreatesSafeDirectory(t *testing.T) {
	t.Parallel()

	projectDir := t.TempDir()
	includePath := filepath.Join("includes", "config.yaml")
	content := "services: {}\n"

	if err := WriteIncludeFile(projectDir, includePath, content, nil); err != nil {
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
	err := WriteIncludeFile(projectDir, includePath, "malicious: true\n", nil)
	if err == nil {
		t.Fatalf("WriteIncludeFile() succeeded but expected rejection for symlink escape")
	}
}

func TestValidatePathWithinProject(t *testing.T) {
	t.Parallel()

	projectDir := t.TempDir()

	tests := []struct {
		name      string
		filePath  string
		wantError bool
	}{
		{"relative path within project", "subdir/file.txt", false},
		{"nested path within project", "a/b/c/file.txt", false},
		{"path traversal attempt", "../outside.txt", true},
		{"absolute path outside project", "/tmp/outside.txt", true},
		{"empty path", "", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := validatePath(projectDir, tt.filePath, nil, false)
			if (err != nil) != tt.wantError {
				t.Errorf("validatePath() error = %v, wantError %v", err, tt.wantError)
			}
		})
	}
}

func TestValidatePathWithAllowedExternalPaths(t *testing.T) {
	t.Parallel()

	projectDir := t.TempDir()
	allowedDir := t.TempDir()

	allowedPaths := []string{allowedDir}

	tests := []struct {
		name      string
		filePath  string
		wantError bool
	}{
		{"path within allowed directory", filepath.Join(allowedDir, "file.txt"), false},
		{"path within project", "subdir/file.txt", false},
		{"path outside both", "/tmp/notallowed/file.txt", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := validatePath(projectDir, tt.filePath, allowedPaths, false)
			if (err != nil) != tt.wantError {
				t.Errorf("validatePath() error = %v, wantError %v", err, tt.wantError)
			}
		})
	}
}

func TestValidatePathReservedNames(t *testing.T) {
	t.Parallel()

	projectDir := t.TempDir()

	tests := []struct {
		name          string
		filePath      string
		checkReserved bool
		wantError     bool
	}{
		{"compose.yaml at root with check", "compose.yaml", true, true},
		{"compose.yaml at root without check", "compose.yaml", false, false},
		{"compose.yaml in subdir with check", "subdir/compose.yaml", true, false},
		{".env at root with check", ".env", true, true},
		{".arcane at root with check", ".arcane", true, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := validatePath(projectDir, tt.filePath, nil, tt.checkReserved)
			if (err != nil) != tt.wantError {
				t.Errorf("validatePath() error = %v, wantError %v", err, tt.wantError)
			}
		})
	}
}

func TestWriteCustomFileValidation(t *testing.T) {
	t.Parallel()

	projectDir := t.TempDir()

	// Writing to a path outside project should fail without allowed paths
	err := WriteCustomFile(projectDir, "/tmp/outside.txt", "content", nil)
	if err == nil {
		t.Error("WriteCustomFile() should reject path outside project")
	}

	// Writing to project directory should work
	err = WriteCustomFile(projectDir, "subdir/file.txt", "content", nil)
	if err != nil {
		t.Errorf("WriteCustomFile() failed for valid path: %v", err)
	}
}

func TestIncludeAndCustomFilesShareValidation(t *testing.T) {
	t.Parallel()

	projectDir := t.TempDir()
	allowedDir := t.TempDir()
	allowedPaths := []string{allowedDir}

	// Both include and custom files should allow writing to allowed external paths
	externalFile := filepath.Join(allowedDir, "shared.yaml")

	err := WriteIncludeFile(projectDir, externalFile, "services: {}\n", allowedPaths)
	if err != nil {
		t.Errorf("WriteIncludeFile() should allow writing to allowed external path: %v", err)
	}

	err = WriteCustomFile(projectDir, externalFile, "updated content", allowedPaths)
	if err != nil {
		t.Errorf("WriteCustomFile() should allow writing to allowed external path: %v", err)
	}

	// Both should reject paths outside project and allowed paths
	outsideFile := "/tmp/not-allowed/file.yaml"

	err = WriteIncludeFile(projectDir, outsideFile, "content", allowedPaths)
	if err == nil {
		t.Error("WriteIncludeFile() should reject path outside project and allowed paths")
	}

	err = WriteCustomFile(projectDir, outsideFile, "content", allowedPaths)
	if err == nil {
		t.Error("WriteCustomFile() should reject path outside project and allowed paths")
	}
}

func TestParseCustomFilesHandlesNonExistentFiles(t *testing.T) {
	t.Parallel()

	projectDir := t.TempDir()

	// Create manifest with both existing and non-existing files
	manifest := ArcaneManifest{
		CustomFiles: []string{"nonexistent.txt", "valid.txt"},
	}
	if err := WriteManifest(projectDir, &manifest); err != nil {
		t.Fatalf("WriteManifest() failed: %v", err)
	}

	// Create only the valid file
	validPath := filepath.Join(projectDir, "valid.txt")
	if err := os.WriteFile(validPath, []byte("valid content"), common.FilePerm); err != nil {
		t.Fatalf("failed to create valid file: %v", err)
	}

	// ParseCustomFiles should return both files (placeholder for non-existent)
	files, err := ParseCustomFiles(projectDir, nil)
	if err != nil {
		t.Fatalf("ParseCustomFiles() returned error: %v", err)
	}

	if len(files) != 2 {
		t.Errorf("expected 2 files, got %d", len(files))
	}

	for _, f := range files {
		switch f.Path {
		case "nonexistent.txt":
			if f.Content != PlaceholderGeneric {
				t.Errorf("expected placeholder content for nonexistent.txt, got %q", f.Content)
			}
		case "valid.txt":
			if f.Content != "valid content" {
				t.Errorf("expected 'valid content' for valid.txt, got %q", f.Content)
			}
		}
	}
}

func TestParseCustomFilesRejectsPathTraversal(t *testing.T) {
	t.Parallel()

	projectDir := t.TempDir()

	// Create a malicious manifest with path traversal
	manifest := ArcaneManifest{
		CustomFiles: []string{"../../../etc/passwd", "valid.txt"},
	}
	if err := WriteManifest(projectDir, &manifest); err != nil {
		t.Fatalf("WriteManifest() failed: %v", err)
	}

	// Create the valid file
	validPath := filepath.Join(projectDir, "valid.txt")
	if err := os.WriteFile(validPath, []byte("valid content"), common.FilePerm); err != nil {
		t.Fatalf("failed to create valid file: %v", err)
	}

	// ParseCustomFiles should skip the malicious path
	files, err := ParseCustomFiles(projectDir, nil)
	if err != nil {
		t.Fatalf("ParseCustomFiles() returned error: %v", err)
	}

	if len(files) != 1 {
		t.Errorf("expected 1 file, got %d", len(files))
	}
	if len(files) > 0 && files[0].Path != "valid.txt" {
		t.Errorf("expected valid.txt, got %s", files[0].Path)
	}
}

func TestParseCustomFilesAllowsExternalPaths(t *testing.T) {
	t.Parallel()

	projectDir := t.TempDir()
	externalDir := t.TempDir()

	// Create an external file
	externalFile := filepath.Join(externalDir, "external.txt")
	if err := os.WriteFile(externalFile, []byte("external content"), common.FilePerm); err != nil {
		t.Fatalf("failed to create external file: %v", err)
	}

	// Create manifest with external path
	manifest := ArcaneManifest{
		CustomFiles: []string{externalFile},
	}
	if err := WriteManifest(projectDir, &manifest); err != nil {
		t.Fatalf("WriteManifest() failed: %v", err)
	}

	// Without allowed paths, should be rejected
	files, err := ParseCustomFiles(projectDir, nil)
	if err != nil {
		t.Fatalf("ParseCustomFiles() returned error: %v", err)
	}
	if len(files) != 0 {
		t.Errorf("expected 0 files without allowed paths, got %d", len(files))
	}

	// With allowed paths, should be included
	files, err = ParseCustomFiles(projectDir, []string{externalDir})
	if err != nil {
		t.Fatalf("ParseCustomFiles() returned error: %v", err)
	}
	if len(files) != 1 {
		t.Errorf("expected 1 file with allowed paths, got %d", len(files))
	}
}

func TestRegisterCustomFileRejectsPathTraversal(t *testing.T) {
	t.Parallel()

	projectDir := t.TempDir()

	err := RegisterCustomFile(projectDir, "../../../etc/passwd", nil)
	if err == nil {
		t.Error("RegisterCustomFile() should reject path traversal")
	}
}

func TestRegisterCustomFileDoesNotOverwriteExisting(t *testing.T) {
	t.Parallel()

	projectDir := t.TempDir()
	existingContent := "existing content that should not be overwritten"
	filePath := filepath.Join(projectDir, "existing.txt")

	if err := os.WriteFile(filePath, []byte(existingContent), common.FilePerm); err != nil {
		t.Fatalf("failed to create existing file: %v", err)
	}

	if err := RegisterCustomFile(projectDir, "existing.txt", nil); err != nil {
		t.Fatalf("RegisterCustomFile() failed: %v", err)
	}

	content, err := os.ReadFile(filePath)
	if err != nil {
		t.Fatalf("failed to read file: %v", err)
	}
	if string(content) != existingContent {
		t.Errorf("file content was modified: got %q, want %q", string(content), existingContent)
	}
}

func TestRegisterCustomFileDoesNotCreateFile(t *testing.T) {
	t.Parallel()

	projectDir := t.TempDir()
	filePath := filepath.Join(projectDir, "newfile.txt")

	if err := RegisterCustomFile(projectDir, "newfile.txt", nil); err != nil {
		t.Fatalf("RegisterCustomFile() failed: %v", err)
	}

	if _, err := os.Stat(filePath); !os.IsNotExist(err) {
		t.Error("RegisterCustomFile() should not create file on disk")
	}

	manifest, err := ReadManifest(projectDir)
	if err != nil {
		t.Fatalf("ReadManifest() failed: %v", err)
	}
	if len(manifest.CustomFiles) != 1 || manifest.CustomFiles[0] != "newfile.txt" {
		t.Errorf("expected manifest to contain newfile.txt, got %v", manifest.CustomFiles)
	}

	files, err := ParseCustomFiles(projectDir, nil)
	if err != nil {
		t.Fatalf("ParseCustomFiles() failed: %v", err)
	}
	if len(files) != 1 {
		t.Fatalf("expected 1 file, got %d", len(files))
	}
	if files[0].Content != PlaceholderGeneric {
		t.Errorf("expected placeholder content, got %q", files[0].Content)
	}
}

func TestIsWithinDirectoryEqualityCase(t *testing.T) {
	t.Parallel()

	dir := "/project"

	if isWithinDirectory(dir, dir) {
		t.Error("isWithinDirectory() should return false for equal paths")
	}

	if !isWithinDirectory("/project/subdir", dir) {
		t.Error("isWithinDirectory() should return true for subdirectory")
	}

	if isWithinDirectory("/project2", dir) {
		t.Error("isWithinDirectory() should return false for sibling directory")
	}
}
