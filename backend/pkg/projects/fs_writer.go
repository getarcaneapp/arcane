package projects

import (
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	pkgutils "github.com/getarcaneapp/arcane/backend/v2/pkg/utils"
	"go.getarcane.app/sys/atomic"
)

// DefaultComposeFileName is the compose filename Arcane writes when a project
// has no existing compose file.
const DefaultComposeFileName = "compose.yaml"

var composeFileCandidates = []string{
	DefaultComposeFileName,
	"compose.yml",
	"docker-compose.yaml",
	"docker-compose.yml",
	"podman-compose.yaml",
	"podman-compose.yml",
}

// ComposeFileCandidates returns the supported compose filenames in Arcane's
// detection order. A copy is returned so callers can't mutate package state.
func ComposeFileCandidates() []string {
	return append([]string(nil), composeFileCandidates...)
}

// detectExistingComposeFileInternal finds an existing compose file in the directory
func detectExistingComposeFileInternal(dir string) string {
	composePath, err := DetectComposeFile(dir)
	if err == nil {
		return composePath
	}
	return ""
}

// WriteComposeFile writes a compose file to the specified directory.
// It detects existing compose file names (docker-compose.yml, compose.yaml, etc.)
// and uses the existing name if found, otherwise defaults to compose.yaml
// projectsRoot is the allowed root directory to prevent path traversal attacks
func WriteComposeFile(projectsRoot, dirPath, content string) error {
	// Security: Validate dirPath is absolute and clean to prevent path traversal
	absPath, err := filepath.Abs(dirPath)
	if err != nil {
		return fmt.Errorf("failed to resolve directory path: %w", err)
	}
	dirPath = filepath.Clean(absPath)

	// Security: Validate dirPath is within projectsRoot
	rootAbs, err := filepath.Abs(projectsRoot)
	if err != nil {
		return fmt.Errorf("failed to resolve projects root: %w", err)
	}
	rootAbs = filepath.Clean(rootAbs)

	if !IsSafeSubdirectory(rootAbs, dirPath) {
		return errors.New("refusing to write compose file: path outside projects root")
	}

	if err := os.MkdirAll(dirPath, pkgutils.DirPerm); err != nil {
		return fmt.Errorf("failed to create directory: %w", err)
	}

	var composePath string
	if existingFile := detectExistingComposeFileInternal(dirPath); existingFile != "" {
		composePath = existingFile
	} else {
		composePath = filepath.Join(dirPath, DefaultComposeFileName)
	}

	if err := atomic.WriteFile(composePath, []byte(content), pkgutils.FilePerm); err != nil {
		return fmt.Errorf("failed to write compose file: %w", err)
	}

	return nil
}

func WriteProjectFile(projectsRoot, dirPath, fileName, content string) error {
	// Security: Validate dirPath is absolute and clean to prevent path traversal
	absPath, err := filepath.Abs(dirPath)
	if err != nil {
		return fmt.Errorf("failed to resolve directory path: %w", err)
	}
	dirPath = filepath.Clean(absPath)

	// Security: Validate dirPath is within projectsRoot
	rootAbs, err := filepath.Abs(projectsRoot)
	if err != nil {
		return fmt.Errorf("failed to resolve projects root: %w", err)
	}
	rootAbs = filepath.Clean(rootAbs)

	if !IsSafeSubdirectory(rootAbs, dirPath) {
		return errors.New("refusing to write project file: path outside projects root")
	}

	if err := os.MkdirAll(dirPath, pkgutils.DirPerm); err != nil {
		return fmt.Errorf("failed to create directory: %w", err)
	}

	if fileName == "" || filepath.Base(fileName) != fileName || strings.Contains(fileName, string(filepath.Separator)) {
		return fmt.Errorf("invalid project file name %q", fileName)
	}

	targetPath := filepath.Join(dirPath, fileName)
	if err := atomic.WriteFile(targetPath, []byte(content), pkgutils.FilePerm); err != nil {
		return fmt.Errorf("failed to write project file %s: %w", fileName, err)
	}

	return nil
}

func RemoveProjectFile(projectsRoot, dirPath, fileName string) error {
	absPath, err := filepath.Abs(dirPath)
	if err != nil {
		return fmt.Errorf("failed to resolve directory path: %w", err)
	}
	dirPath = filepath.Clean(absPath)

	rootAbs, err := filepath.Abs(projectsRoot)
	if err != nil {
		return fmt.Errorf("failed to resolve projects root: %w", err)
	}
	rootAbs = filepath.Clean(rootAbs)

	if !IsSafeSubdirectory(rootAbs, dirPath) {
		return errors.New("refusing to remove project file: path outside projects root")
	}

	if fileName == "" || filepath.Base(fileName) != fileName || strings.Contains(fileName, string(filepath.Separator)) {
		return fmt.Errorf("invalid project file name %q", fileName)
	}

	targetPath := filepath.Join(dirPath, fileName)
	if err := os.Remove(targetPath); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("failed to remove project file %s: %w", fileName, err)
	}

	return nil
}

// WriteEnvFile writes a .env file to the specified directory
// projectsRoot is the allowed root directory to prevent path traversal attacks
func WriteEnvFile(projectsRoot, dirPath, content string) error {
	return WriteProjectFile(projectsRoot, dirPath, ".env", content)
}

func EnsureEnvFile(projectsRoot, dirPath string) error {
	envPath := filepath.Join(dirPath, ".env")
	if _, err := os.Stat(envPath); err == nil {
		return nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("failed to stat env file: %w", err)
	}

	return WriteEnvFile(projectsRoot, dirPath, "")
}

// WriteProjectFiles writes both compose and env files to a project directory.
// An empty .env file is always created to prevent compose-go from failing when
// the compose file references env_file: .env
// projectsRoot is the allowed root directory to prevent path traversal attacks
func WriteProjectFiles(projectsRoot, dirPath, composeContent string, envContent *string) error {
	if err := WriteComposeFile(projectsRoot, dirPath, composeContent); err != nil {
		return err
	}

	// If envContent is nil, we check if .env already exists.
	// We only create an empty one if it doesn't exist, to satisfy
	// compose-go from failing when the compose file references env_file: .env
	if envContent != nil {
		if err := WriteEnvFile(projectsRoot, dirPath, *envContent); err != nil {
			return err
		}
	} else {
		if err := EnsureEnvFile(projectsRoot, dirPath); err != nil {
			return err
		}
	}

	return nil
}

// WriteTemplateFile writes a template file (like .compose.template or .env.template)
func WriteTemplateFile(filePath, content string) error {
	dir := filepath.Dir(filePath)
	if err := os.MkdirAll(dir, pkgutils.DirPerm); err != nil {
		return fmt.Errorf("failed to create template directory: %w", err)
	}

	if err := atomic.WriteFile(filePath, []byte(content), pkgutils.FilePerm); err != nil {
		return fmt.Errorf("failed to write template file: %w", err)
	}

	return nil
}

// WriteFileWithPerm is a generic file writer with custom permissions
func WriteFileWithPerm(filePath, content string, perm os.FileMode) error {
	dir := filepath.Dir(filePath)
	if err := os.MkdirAll(dir, pkgutils.DirPerm); err != nil {
		return fmt.Errorf("failed to create directory: %w", err)
	}

	if err := atomic.WriteFile(filePath, []byte(content), perm); err != nil {
		return fmt.Errorf("failed to write file: %w", err)
	}

	return nil
}

// RollbackRenamedProjectDirectory restores a project directory rename when possible.
func RollbackRenamedProjectDirectory(oldPath, newPath string) (pathsMissing bool, err error) {
	oldPath = filepath.Clean(oldPath)
	newPath = filepath.Clean(newPath)
	if oldPath == "" || newPath == "" || oldPath == newPath {
		return false, nil
	}

	oldExists, _ := pathExistsInternal(oldPath)
	newExists, _ := pathExistsInternal(newPath)
	switch {
	case oldExists && newExists:
		conflictPath, err := relocateRenameConflictDirectoryInternal(newPath)
		if err != nil {
			slog.Warn("project rename directory rollback found both paths and failed to relocate target path; keeping old path and clearing journal", "oldPath", oldPath, "newPath", newPath, "error", err)
		} else {
			slog.Warn("project rename directory rollback found both paths; moved target path aside and kept old path", "oldPath", oldPath, "newPath", newPath, "conflictPath", conflictPath)
		}
	case !oldExists && newExists:
		if err := os.Rename(newPath, oldPath); err != nil {
			return false, fmt.Errorf("rollback project directory rename: %w", err)
		}
	case !oldExists && !newExists:
		pathsMissing = true
		slog.Warn("project rename directory paths are missing during rollback", "oldPath", oldPath, "newPath", newPath)
	}
	return pathsMissing, nil
}

func relocateRenameConflictDirectoryInternal(path string) (string, error) {
	parent := filepath.Dir(path)
	base := filepath.Base(path)
	now := time.Now().UTC().UnixNano()
	for attempt := range 10 {
		conflictPath := filepath.Join(parent, fmt.Sprintf(".%s.rename-conflict-%d-%d", base, now, attempt))
		if _, err := os.Stat(conflictPath); err == nil {
			continue
		} else if !os.IsNotExist(err) {
			return "", fmt.Errorf("check conflict path: %w", err)
		}
		if err := os.Rename(path, conflictPath); err != nil {
			return "", fmt.Errorf("relocate project rename target path: %w", err)
		}
		return conflictPath, nil
	}
	return "", fmt.Errorf("relocate project rename target path: no available conflict path for %s", path)
}

// SyncFile represents a file to be written during directory sync
type SyncFile struct {
	RelativePath string // Path relative to the project directory
	Content      []byte
	// Executable preserves the source's +x bit so lifecycle hooks and other
	// repo-committed scripts arrive runnable in the project workspace.
	Executable bool
}

// WriteSyncedDirectory writes multiple files to a project directory.
// It validates all paths are within the project directory and creates
// subdirectories as needed. Returns the list of written file paths.
func WriteSyncedDirectory(projectsRoot, projectPath string, files []SyncFile) ([]string, error) {
	// Security: Validate projectPath is within projectsRoot
	rootAbs, err := filepath.Abs(projectsRoot)
	if err != nil {
		return nil, fmt.Errorf("failed to resolve projects root: %w", err)
	}
	rootAbs = filepath.Clean(rootAbs)

	projectAbs, err := filepath.Abs(projectPath)
	if err != nil {
		return nil, fmt.Errorf("failed to resolve project path: %w", err)
	}
	projectAbs = filepath.Clean(projectAbs)

	if !IsSafeSubdirectory(rootAbs, projectAbs) {
		return nil, errors.New("project path is outside projects root")
	}

	// Ensure project directory exists
	if err := os.MkdirAll(projectAbs, pkgutils.DirPerm); err != nil {
		return nil, fmt.Errorf("failed to create project directory: %w", err)
	}

	writtenPaths := make([]string, 0, len(files))

	for _, file := range files {
		// Validate relative path doesn't escape project directory
		targetPath := filepath.Join(projectAbs, file.RelativePath)
		targetPathClean := filepath.Clean(targetPath)

		if !IsSafeSubdirectory(projectAbs, targetPathClean) {
			return nil, fmt.Errorf("file path %s would escape project directory", file.RelativePath)
		}

		// Create parent directories
		parentDir := filepath.Dir(targetPathClean)
		if err := os.MkdirAll(parentDir, pkgutils.DirPerm); err != nil {
			return nil, fmt.Errorf("failed to create directory for %s: %w", file.RelativePath, err)
		}

		info, err := os.Stat(targetPathClean)
		if err == nil && info.IsDir() {
			if err := os.RemoveAll(targetPathClean); err != nil {
				return nil, fmt.Errorf("failed to replace directory at %s: %w", file.RelativePath, err)
			}
		} else if err != nil && !os.IsNotExist(err) {
			return nil, fmt.Errorf("failed to inspect target path for %s: %w", file.RelativePath, err)
		}

		// Write the file. Honor the source's executable bit so scripts arrive
		// runnable for lifecycle hooks and similar consumers.
		perm := pkgutils.FilePerm
		if file.Executable {
			perm = 0o755
		}
		if err := atomic.WriteFile(targetPathClean, file.Content, perm); err != nil {
			return nil, fmt.Errorf("failed to write file %s: %w", file.RelativePath, err)
		}

		writtenPaths = append(writtenPaths, file.RelativePath)
	}

	return writtenPaths, nil
}

// CleanupRemovedFiles deletes files that were in the old sync but are not in the new sync.
// It only removes files that were previously synced (tracked in oldFiles).
// Empty directories are removed after file deletion.
// This is a best-effort operation - errors are logged but don't cause failure.
func CleanupRemovedFiles(projectsRoot, projectPath string, oldFiles, newFiles []string) error {
	// Security: Validate projectPath is within projectsRoot
	rootAbs, err := filepath.Abs(projectsRoot)
	if err != nil {
		return fmt.Errorf("failed to resolve projects root: %w", err)
	}
	rootAbs = filepath.Clean(rootAbs)

	projectAbs, err := filepath.Abs(projectPath)
	if err != nil {
		return fmt.Errorf("failed to resolve project path: %w", err)
	}
	projectAbs = filepath.Clean(projectAbs)

	if !IsSafeSubdirectory(rootAbs, projectAbs) {
		return errors.New("project path is outside projects root")
	}

	// Build set of new files for quick lookup
	newFileSet := make(map[string]bool, len(newFiles))
	for _, f := range newFiles {
		newFileSet[f] = true
	}

	// Track directories that may need cleanup
	dirsToCheck := make(map[string]bool)

	// Delete files that are in old but not in new
	for _, oldFile := range oldFiles {
		if newFileSet[oldFile] {
			continue // File still exists in new sync
		}

		targetPath := filepath.Join(projectAbs, oldFile)
		targetPathClean := filepath.Clean(targetPath)

		// Security check
		if !IsSafeSubdirectory(projectAbs, targetPathClean) {
			continue // Skip files that would be outside project
		}

		// Delete the file (best effort)
		if err := os.Remove(targetPathClean); err != nil {
			if !os.IsNotExist(err) {
				// Log but continue - this is best effort
				slog.Warn("Failed to remove old synced file", "file", oldFile, "error", err)
			}
		}

		// Track parent directory for potential cleanup
		parentDir := filepath.Dir(targetPathClean)
		if parentDir != projectAbs {
			dirsToCheck[parentDir] = true
		}
	}

	// Clean up empty directories (best effort, deepest first)
	for dir := range dirsToCheck {
		cleanupEmptyDirs(projectAbs, dir)
	}

	return nil
}

// cleanupEmptyDirs removes empty directories starting from the given path
// up to (but not including) the project root.
func cleanupEmptyDirs(projectRoot, startDir string) {
	current := startDir
	for current != projectRoot && IsSafeSubdirectory(projectRoot, current) {
		// Try to remove the directory (will fail if not empty)
		err := os.Remove(current)
		if err != nil {
			break // Directory not empty or other error
		}

		// Move to parent directory
		current = filepath.Dir(current)
	}
}
