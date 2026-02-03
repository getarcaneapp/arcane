package fs

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/getarcaneapp/arcane/backend/internal/common"
)

var composeFileCandidates = []string{
	"compose.yaml",
	"compose.yml",
	"docker-compose.yaml",
	"docker-compose.yml",
}

// detectExistingComposeFile finds an existing compose file in the directory
func detectExistingComposeFile(dir string) string {
	for _, filename := range composeFileCandidates {
		fullPath := filepath.Join(dir, filename)
		if info, err := os.Stat(fullPath); err == nil && !info.IsDir() {
			return fullPath
		}
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
		return fmt.Errorf("refusing to write compose file: path outside projects root")
	}

	if err := os.MkdirAll(dirPath, common.DirPerm); err != nil {
		return fmt.Errorf("failed to create directory: %w", err)
	}

	var composePath string
	if existingFile := detectExistingComposeFile(dirPath); existingFile != "" {
		composePath = existingFile
	} else {
		composePath = filepath.Join(dirPath, "compose.yaml")
	}

	if err := os.WriteFile(composePath, []byte(content), common.FilePerm); err != nil {
		return fmt.Errorf("failed to write compose file: %w", err)
	}

	return nil
}

// WriteEnvFile writes a .env file to the specified directory
// projectsRoot is the allowed root directory to prevent path traversal attacks
func WriteEnvFile(projectsRoot, dirPath, content string) error {
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
		return fmt.Errorf("refusing to write env file: path outside projects root")
	}

	if err := os.MkdirAll(dirPath, common.DirPerm); err != nil {
		return fmt.Errorf("failed to create directory: %w", err)
	}

	envPath := filepath.Join(dirPath, ".env")
	if err := os.WriteFile(envPath, []byte(content), common.FilePerm); err != nil {
		return fmt.Errorf("failed to write env file: %w", err)
	}

	return nil
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
		envPath := filepath.Join(dirPath, ".env")
		if _, err := os.Stat(envPath); os.IsNotExist(err) {
			if err := WriteEnvFile(projectsRoot, dirPath, ""); err != nil {
				return err
			}
		}
	}

	return nil
}

// WriteTemplateFile writes a template file (like .compose.template or .env.template)
func WriteTemplateFile(filePath, content string) error {
	dir := filepath.Dir(filePath)
	if err := os.MkdirAll(dir, common.DirPerm); err != nil {
		return fmt.Errorf("failed to create template directory: %w", err)
	}

	if err := os.WriteFile(filePath, []byte(content), common.FilePerm); err != nil {
		return fmt.Errorf("failed to write template file: %w", err)
	}

	return nil
}

// WriteFileWithPerm is a generic file writer with custom permissions
func WriteFileWithPerm(filePath, content string, perm os.FileMode) error {
	dir := filepath.Dir(filePath)
	if err := os.MkdirAll(dir, common.DirPerm); err != nil {
		return fmt.Errorf("failed to create directory: %w", err)
	}

	if err := os.WriteFile(filePath, []byte(content), perm); err != nil {
		return fmt.Errorf("failed to write file: %w", err)
	}

	return nil
}

// SyncFile represents a file to be written during directory sync
type SyncFile struct {
	RelativePath string // Path relative to the project directory
	Content      []byte
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
		return nil, fmt.Errorf("project path is outside projects root")
	}

	// Ensure project directory exists
	if err := os.MkdirAll(projectAbs, common.DirPerm); err != nil {
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
		if err := os.MkdirAll(parentDir, common.DirPerm); err != nil {
			return nil, fmt.Errorf("failed to create directory for %s: %w", file.RelativePath, err)
		}

		// Write the file
		if err := os.WriteFile(targetPathClean, file.Content, common.FilePerm); err != nil {
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
		return fmt.Errorf("project path is outside projects root")
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
				fmt.Printf("Warning: failed to remove old synced file %s: %v\n", oldFile, err)
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
		if err := os.Remove(current); err != nil {
			break // Directory not empty or other error
		}

		// Move to parent directory
		current = filepath.Dir(current)
	}
}
