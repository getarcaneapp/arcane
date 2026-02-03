package fs

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/getarcaneapp/arcane/backend/internal/common"
)

// ParseAllowedPaths parses a comma-separated string of allowed paths.
func ParseAllowedPaths(s string) []string {
	if s == "" {
		return nil
	}
	var paths []string
	for _, p := range strings.Split(s, ",") {
		if t := strings.TrimSpace(p); t != "" && filepath.IsAbs(t) {
			paths = append(paths, filepath.Clean(t))
		}
	}
	return paths
}

// ResolveAbsPath resolves a directory path to an absolute, cleaned path with symlink resolution.
func ResolveAbsPath(dir string) (string, error) {
	absDir, err := filepath.Abs(dir)
	if err != nil {
		return "", err
	}
	absDir = filepath.Clean(absDir)
	if evalDir, err := filepath.EvalSymlinks(absDir); err == nil {
		return evalDir, nil
	}
	return absDir, nil
}

// ResolveFilePath resolves a file path relative to a base path.
// Returns both the absolute path and the symlink-evaluated path.
func ResolveFilePath(basePath, filePath string) (absPath, evalPath string, err error) {
	absPath = filePath
	if !filepath.IsAbs(filePath) {
		absPath = filepath.Join(basePath, filePath)
	}
	absPath, err = filepath.Abs(absPath)
	if err != nil {
		return "", "", fmt.Errorf("invalid file path: %w", err)
	}
	absPath = filepath.Clean(absPath)

	if resolved, err := filepath.EvalSymlinks(absPath); err == nil {
		return absPath, resolved, nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return "", "", fmt.Errorf("failed to resolve path: %w", err)
	}

	// File doesn't exist yet - evaluate parent directory symlinks
	dir := filepath.Dir(absPath)
	if evalDir, err := filepath.EvalSymlinks(dir); err == nil {
		return absPath, filepath.Join(evalDir, filepath.Base(absPath)), nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return "", "", fmt.Errorf("failed to resolve parent directory: %w", err)
	}
	return absPath, absPath, nil
}

// IsWithinDirectory checks if evalPath is within dir (not equal to it).
func IsWithinDirectory(evalPath, dir string) bool {
	if evalPath == dir {
		return false
	}
	evalPath = filepath.Clean(evalPath)
	prefix := dir + string(filepath.Separator)
	return strings.HasPrefix(evalPath+string(filepath.Separator), prefix)
}

// IsWithinAllowedPaths checks if evalPath is within any of the allowed paths.
func IsWithinAllowedPaths(evalPath string, allowedPaths []string) bool {
	for _, ap := range allowedPaths {
		evalAllowedPath := ap
		if resolved, err := filepath.EvalSymlinks(ap); err == nil {
			evalAllowedPath = resolved
		}
		if IsWithinDirectory(evalPath, evalAllowedPath) || evalPath == evalAllowedPath {
			return true
		}
	}
	return false
}

// IsReservedFileName checks if absPath is a reserved file name at the root of absBaseDir.
func IsReservedFileName(absPath, absBaseDir string, reservedNames []string) bool {
	rel, err := filepath.Rel(absBaseDir, absPath)
	if err != nil || filepath.Dir(rel) != "." {
		return false
	}
	baseName := filepath.Base(rel)
	for _, r := range reservedNames {
		if strings.EqualFold(baseName, r) {
			return true
		}
	}
	return false
}

// ReadFileWithPlaceholder reads a file, returning the placeholder content if the file doesn't exist.
func ReadFileWithPlaceholder(absPath, placeholder string) (string, error) {
	content, err := os.ReadFile(absPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return placeholder, nil
		}
		return "", err
	}
	return string(content), nil
}

// WriteFileWithDir writes content to a file, creating parent directories as needed.
func WriteFileWithDir(absPath, content string) error {
	if dir := filepath.Dir(absPath); dir != "" {
		if _, err := os.Stat(dir); errors.Is(err, os.ErrNotExist) {
			if err := os.MkdirAll(dir, common.DirPerm); err != nil {
				return fmt.Errorf("failed to create directory: %w", err)
			}
		}
	}
	return os.WriteFile(absPath, []byte(content), common.FilePerm)
}

// NormalizePath returns a normalized path for storage (relative if within baseDir, absolute otherwise).
func NormalizePath(absPath, absBaseDir string) string {
	if rel, err := filepath.Rel(absBaseDir, absPath); err == nil && !strings.HasPrefix(rel, "..") {
		return rel
	}
	return absPath
}

// ValidatePath validates a file path for read/write operations.
// Parameters:
//   - baseDir: the base directory (e.g., project directory)
//   - filePath: the file path to validate (relative or absolute)
//   - allowedPaths: additional allowed external paths
//   - reservedNames: file names that are not allowed at the root of baseDir (can be nil)
//
// Returns the validated absolute path or an error.
func ValidatePath(baseDir, filePath string, allowedPaths, reservedNames []string) (string, error) {
	if filePath == "" {
		return "", fmt.Errorf("file path cannot be empty")
	}

	absBaseDir, err := ResolveAbsPath(baseDir)
	if err != nil {
		return "", fmt.Errorf("invalid base directory: %w", err)
	}

	absPath, evalPath, err := ResolveFilePath(absBaseDir, filePath)
	if err != nil {
		return "", err
	}

	if evalPath == absBaseDir {
		return "", fmt.Errorf("path cannot be the base directory itself")
	}

	withinBase := IsWithinDirectory(evalPath, absBaseDir)
	withinAllowed := IsWithinAllowedPaths(evalPath, allowedPaths)

	if !withinBase && !withinAllowed {
		if len(allowedPaths) == 0 {
			return "", fmt.Errorf("path outside base directory; configure ALLOWED_EXTERNAL_PATHS to allow external paths")
		}
		return "", fmt.Errorf("path not in base directory or allowed directories")
	}

	if len(reservedNames) > 0 && withinBase && IsReservedFileName(absPath, absBaseDir, reservedNames) {
		return "", fmt.Errorf("reserved file name: %s", filepath.Base(absPath))
	}

	return absPath, nil
}

// ValidateAndNormalizePath validates a file path and returns the normalized path for storage.
// The normalized path is relative if within the base directory, absolute otherwise.
func ValidateAndNormalizePath(baseDir, filePath string, allowedPaths, reservedNames []string) (string, error) {
	absPath, err := ValidatePath(baseDir, filePath, allowedPaths, reservedNames)
	if err != nil {
		return "", err
	}

	absBaseDir, _ := ResolveAbsPath(baseDir)
	return NormalizePath(absPath, absBaseDir), nil
}

// WriteFile validates a path and writes content to it.
func WriteFile(baseDir, filePath, content string, allowedPaths, reservedNames []string) error {
	absPath, err := ValidatePath(baseDir, filePath, allowedPaths, reservedNames)
	if err != nil {
		return err
	}
	if err := WriteFileWithDir(absPath, content); err != nil {
		return fmt.Errorf("failed to write file: %w", err)
	}
	return nil
}

// ReadFile validates a path and reads the file content, returning placeholder if file doesn't exist.
func ReadFile(baseDir, filePath, placeholder string, allowedPaths []string) (string, error) {
	absPath, err := ValidatePath(baseDir, filePath, allowedPaths, nil)
	if err != nil {
		return "", err
	}
	return ReadFileWithPlaceholder(absPath, placeholder)
}

// DeleteFile validates a path and deletes the file.
func DeleteFile(baseDir, filePath string, allowedPaths []string) error {
	absPath, err := ValidatePath(baseDir, filePath, allowedPaths, nil)
	if err != nil {
		return fmt.Errorf("invalid file path: %w", err)
	}

	if err := os.Remove(absPath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("failed to delete file: %w", err)
	}
	return nil
}
