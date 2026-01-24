package fs

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/getarcaneapp/arcane/backend/internal/common"
)

func TestParseAllowedPaths(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		input    string
		expected []string
	}{
		{"empty string", "", nil},
		{"single path", "/home/user", []string{"/home/user"}},
		{"multiple paths", "/home/user,/var/data", []string{"/home/user", "/var/data"}},
		{"with spaces", " /home/user , /var/data ", []string{"/home/user", "/var/data"}},
		{"relative path ignored", "relative/path,/absolute/path", []string{"/absolute/path"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := ParseAllowedPaths(tt.input)
			if len(result) != len(tt.expected) {
				t.Errorf("ParseAllowedPaths() = %v, want %v", result, tt.expected)
				return
			}
			for i, v := range result {
				if v != tt.expected[i] {
					t.Errorf("ParseAllowedPaths()[%d] = %v, want %v", i, v, tt.expected[i])
				}
			}
		})
	}
}

func TestValidatePathWithinBase(t *testing.T) {
	t.Parallel()

	baseDir := t.TempDir()

	tests := []struct {
		name      string
		filePath  string
		wantError bool
	}{
		{"relative path within base", "subdir/file.txt", false},
		{"nested path within base", "a/b/c/file.txt", false},
		{"path traversal attempt", "../outside.txt", true},
		{"absolute path outside base", "/tmp/outside.txt", true},
		{"empty path", "", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := ValidatePath(baseDir, tt.filePath, nil, nil)
			if (err != nil) != tt.wantError {
				t.Errorf("ValidatePath() error = %v, wantError %v", err, tt.wantError)
			}
		})
	}
}

func TestValidatePathWithAllowedExternalPaths(t *testing.T) {
	t.Parallel()

	baseDir := t.TempDir()
	allowedDir := t.TempDir()

	allowedPaths := []string{allowedDir}

	tests := []struct {
		name      string
		filePath  string
		wantError bool
	}{
		{"path within allowed directory", filepath.Join(allowedDir, "file.txt"), false},
		{"path within base", "subdir/file.txt", false},
		{"path outside both", "/tmp/notallowed/file.txt", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := ValidatePath(baseDir, tt.filePath, allowedPaths, nil)
			if (err != nil) != tt.wantError {
				t.Errorf("ValidatePath() error = %v, wantError %v", err, tt.wantError)
			}
		})
	}
}

func TestValidatePathReservedNames(t *testing.T) {
	t.Parallel()

	baseDir := t.TempDir()
	reservedNames := []string{"compose.yaml", "compose.yml", ".env"}

	tests := []struct {
		name      string
		filePath  string
		wantError bool
	}{
		{"compose.yaml at root", "compose.yaml", true},
		{"compose.yaml in subdir", "subdir/compose.yaml", false},
		{".env at root", ".env", true},
		{"other file at root", "config.txt", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := ValidatePath(baseDir, tt.filePath, nil, reservedNames)
			if (err != nil) != tt.wantError {
				t.Errorf("ValidatePath() error = %v, wantError %v", err, tt.wantError)
			}
		})
	}
}

func TestValidatePathNoReservedNames(t *testing.T) {
	t.Parallel()

	baseDir := t.TempDir()

	// Without reserved names, all file names should be allowed
	tests := []struct {
		name     string
		filePath string
	}{
		{"compose.yaml at root", "compose.yaml"},
		{".env at root", ".env"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := ValidatePath(baseDir, tt.filePath, nil, nil)
			if err != nil {
				t.Errorf("ValidatePath() with no reserved names should allow %s: %v", tt.filePath, err)
			}
		})
	}
}

func TestValidatePathSymlinkEscape(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink creation requires elevated privileges on Windows")
	}
	t.Parallel()

	baseDir := t.TempDir()
	outsideDir := t.TempDir()

	linkPath := filepath.Join(baseDir, "link")
	if err := os.Symlink(outsideDir, linkPath); err != nil {
		t.Fatalf("failed to create symlink: %v", err)
	}

	_, err := ValidatePath(baseDir, "link/escape.txt", nil, nil)
	if err == nil {
		t.Error("ValidatePath() should reject symlink escape")
	}
}

func TestValidateAndNormalizePathRejectsPathTraversal(t *testing.T) {
	t.Parallel()

	baseDir := t.TempDir()

	_, err := ValidateAndNormalizePath(baseDir, "../../../etc/passwd", nil, nil)
	if err == nil {
		t.Error("ValidateAndNormalizePath() should reject path traversal")
	}
}

func TestValidateAndNormalizePathReturnsRelativePath(t *testing.T) {
	t.Parallel()

	baseDir := t.TempDir()

	normalized, err := ValidateAndNormalizePath(baseDir, "subdir/file.txt", nil, nil)
	if err != nil {
		t.Fatalf("ValidateAndNormalizePath() failed: %v", err)
	}

	if normalized != "subdir/file.txt" {
		t.Errorf("expected relative path 'subdir/file.txt', got %q", normalized)
	}
}

func TestValidateAndNormalizePathReturnsAbsoluteForExternal(t *testing.T) {
	t.Parallel()

	baseDir := t.TempDir()
	externalDir := t.TempDir()
	externalFile := filepath.Join(externalDir, "external.txt")

	normalized, err := ValidateAndNormalizePath(baseDir, externalFile, []string{externalDir}, nil)
	if err != nil {
		t.Fatalf("ValidateAndNormalizePath() failed: %v", err)
	}

	if normalized != externalFile {
		t.Errorf("expected absolute path %q, got %q", externalFile, normalized)
	}
}

func TestWriteFile(t *testing.T) {
	t.Parallel()

	baseDir := t.TempDir()
	content := "test content"

	// Writing to a valid path should work
	err := WriteFile(baseDir, "subdir/file.txt", content, nil, nil)
	if err != nil {
		t.Fatalf("WriteFile() failed: %v", err)
	}

	// Verify content
	data, err := os.ReadFile(filepath.Join(baseDir, "subdir/file.txt"))
	if err != nil {
		t.Fatalf("failed to read written file: %v", err)
	}
	if string(data) != content {
		t.Errorf("expected content %q, got %q", content, string(data))
	}
}

func TestWriteFileRejectsOutsidePath(t *testing.T) {
	t.Parallel()

	baseDir := t.TempDir()

	err := WriteFile(baseDir, "/tmp/outside.txt", "content", nil, nil)
	if err == nil {
		t.Error("WriteFile() should reject path outside base directory")
	}
}

func TestWriteFileWithDir(t *testing.T) {
	t.Parallel()

	baseDir := t.TempDir()
	filePath := filepath.Join(baseDir, "nested", "dir", "file.txt")
	content := "test content"

	err := WriteFileWithDir(filePath, content)
	if err != nil {
		t.Fatalf("WriteFileWithDir() failed: %v", err)
	}

	data, err := os.ReadFile(filePath)
	if err != nil {
		t.Fatalf("failed to read file: %v", err)
	}
	if string(data) != content {
		t.Errorf("expected content %q, got %q", content, string(data))
	}
}

func TestReadFileWithPlaceholder(t *testing.T) {
	t.Parallel()

	baseDir := t.TempDir()
	placeholder := "# placeholder content"

	// Non-existent file should return placeholder
	content, err := ReadFileWithPlaceholder(filepath.Join(baseDir, "nonexistent.txt"), placeholder)
	if err != nil {
		t.Fatalf("ReadFileWithPlaceholder() failed: %v", err)
	}
	if content != placeholder {
		t.Errorf("expected placeholder %q, got %q", placeholder, content)
	}

	// Existing file should return its content
	existingFile := filepath.Join(baseDir, "existing.txt")
	expectedContent := "actual content"
	if err := os.WriteFile(existingFile, []byte(expectedContent), common.FilePerm); err != nil {
		t.Fatalf("failed to create file: %v", err)
	}

	content, err = ReadFileWithPlaceholder(existingFile, placeholder)
	if err != nil {
		t.Fatalf("ReadFileWithPlaceholder() failed: %v", err)
	}
	if content != expectedContent {
		t.Errorf("expected content %q, got %q", expectedContent, content)
	}
}

func TestReadFile(t *testing.T) {
	t.Parallel()

	baseDir := t.TempDir()
	placeholder := "# placeholder"

	// Create a file
	existingFile := filepath.Join(baseDir, "file.txt")
	expectedContent := "file content"
	if err := os.WriteFile(existingFile, []byte(expectedContent), common.FilePerm); err != nil {
		t.Fatalf("failed to create file: %v", err)
	}

	content, err := ReadFile(baseDir, "file.txt", placeholder, nil)
	if err != nil {
		t.Fatalf("ReadFile() failed: %v", err)
	}
	if content != expectedContent {
		t.Errorf("expected content %q, got %q", expectedContent, content)
	}

	// Non-existent file should return placeholder
	content, err = ReadFile(baseDir, "nonexistent.txt", placeholder, nil)
	if err != nil {
		t.Fatalf("ReadFile() failed for non-existent file: %v", err)
	}
	if content != placeholder {
		t.Errorf("expected placeholder %q, got %q", placeholder, content)
	}
}

func TestReadFileRejectsOutsidePath(t *testing.T) {
	t.Parallel()

	baseDir := t.TempDir()

	_, err := ReadFile(baseDir, "/tmp/outside.txt", "placeholder", nil)
	if err == nil {
		t.Error("ReadFile() should reject path outside base directory")
	}
}

func TestDeleteFile(t *testing.T) {
	t.Parallel()

	baseDir := t.TempDir()
	filePath := filepath.Join(baseDir, "todelete.txt")

	if err := os.WriteFile(filePath, []byte("content"), common.FilePerm); err != nil {
		t.Fatalf("failed to create file: %v", err)
	}

	if err := DeleteFile(baseDir, "todelete.txt", nil); err != nil {
		t.Fatalf("DeleteFile() failed: %v", err)
	}

	if _, err := os.Stat(filePath); !os.IsNotExist(err) {
		t.Error("DeleteFile() should delete file from disk")
	}
}

func TestDeleteFileNonExistent(t *testing.T) {
	t.Parallel()

	baseDir := t.TempDir()

	// Deleting a non-existent file should not error
	err := DeleteFile(baseDir, "nonexistent.txt", nil)
	if err != nil {
		t.Errorf("DeleteFile() should not error for non-existent file: %v", err)
	}
}

func TestDeleteFileRejectsOutsidePath(t *testing.T) {
	t.Parallel()

	baseDir := t.TempDir()

	err := DeleteFile(baseDir, "/tmp/outside.txt", nil)
	if err == nil {
		t.Error("DeleteFile() should reject path outside base directory")
	}
}

func TestIsWithinDirectory(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		path     string
		dir      string
		expected bool
	}{
		{"equal paths", "/project", "/project", false},
		{"subdirectory", "/project/subdir", "/project", true},
		{"sibling directory", "/project2", "/project", false},
		{"parent directory", "/project", "/project/subdir", false},
		{"prefix match but not subdir", "/project-other", "/project", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := IsWithinDirectory(tt.path, tt.dir)
			if result != tt.expected {
				t.Errorf("IsWithinDirectory(%q, %q) = %v, want %v", tt.path, tt.dir, result, tt.expected)
			}
		})
	}
}

func TestIsWithinAllowedPaths(t *testing.T) {
	t.Parallel()

	allowedPaths := []string{"/allowed1", "/allowed2/subdir"}

	tests := []struct {
		name     string
		path     string
		expected bool
	}{
		{"within first allowed", "/allowed1/file.txt", true},
		{"within second allowed", "/allowed2/subdir/file.txt", true},
		{"equal to allowed", "/allowed1", true},
		{"outside all allowed", "/notallowed/file.txt", false},
		{"parent of allowed", "/allowed2", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := IsWithinAllowedPaths(tt.path, allowedPaths)
			if result != tt.expected {
				t.Errorf("IsWithinAllowedPaths(%q) = %v, want %v", tt.path, result, tt.expected)
			}
		})
	}
}

func TestIsReservedFileName(t *testing.T) {
	t.Parallel()

	baseDir := "/project"
	reservedNames := []string{"compose.yaml", ".env"}

	tests := []struct {
		name     string
		path     string
		expected bool
	}{
		{"reserved at root", "/project/compose.yaml", true},
		{"reserved at root case insensitive", "/project/COMPOSE.YAML", true},
		{"reserved in subdir", "/project/subdir/compose.yaml", false},
		{"not reserved", "/project/config.txt", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := IsReservedFileName(tt.path, baseDir, reservedNames)
			if result != tt.expected {
				t.Errorf("IsReservedFileName(%q) = %v, want %v", tt.path, result, tt.expected)
			}
		})
	}
}

func TestNormalizePath(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		absPath  string
		baseDir  string
		expected string
	}{
		{"within base", "/project/subdir/file.txt", "/project", "subdir/file.txt"},
		{"at base root", "/project/file.txt", "/project", "file.txt"},
		{"outside base", "/other/file.txt", "/project", "/other/file.txt"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := NormalizePath(tt.absPath, tt.baseDir)
			if result != tt.expected {
				t.Errorf("NormalizePath(%q, %q) = %q, want %q", tt.absPath, tt.baseDir, result, tt.expected)
			}
		})
	}
}
