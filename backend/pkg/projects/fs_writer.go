package projects

import (
	"context"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"emperror.dev/errors"

	"github.com/getarcaneapp/arcane/backend/v2/pkg/utils"
	"go.getarcane.app/acfs"
	"go.getarcane.app/acfs/atomic"
)

// DefaultComposeFileName is the compose filename Arcane writes when a project
// has no existing compose file.
const DefaultComposeFileName = "compose.yaml"

// composeFileCandidates lists supported base compose filenames in Arcane's
// detection order. This order is intentionally explicit rather than sourced
// from compose-go's cli.DefaultFileNames: compose-go prefers docker-compose.yml
// over docker-compose.yaml, and adopting that order would silently switch the
// base file for existing projects that ship both.
var composeFileCandidates = []string{
	DefaultComposeFileName,
	"compose.yml",
	"docker-compose.yaml",
	"docker-compose.yml",
	"podman-compose.yaml",
	"podman-compose.yml",
}

// ComposeFileCandidates returns the supported base compose filenames in Arcane's
// detection order. A copy is returned so callers can't mutate package state.
func ComposeFileCandidates() []string {
	return slices.Clone(composeFileCandidates)
}

// detectExistingComposeFileInternal finds an existing compose file in the directory
func detectExistingComposeFileInternal(ctx context.Context, projectsRoot, dir string) string {
	composePath, err := DetectComposeFile(ctx, projectsRoot, dir)
	if err == nil {
		return composePath
	}
	return ""
}

// WriteComposeFile writes a compose file to the specified directory.
// It detects existing compose file names (docker-compose.yml, compose.yaml, etc.)
// and uses the existing name if found, otherwise defaults to compose.yaml
// projectsRoot is the allowed root directory to prevent path traversal attacks
func WriteComposeFile(ctx context.Context, projectsRoot, dirPath, content string) error {
	if _, err := acfs.LogicalPath(projectsRoot, dirPath); err != nil {
		return errors.WrapIf(err, "refusing to write compose file: path outside projects root")
	}

	// The project directory is the confinement root for the write itself, so it
	// has to exist before acfs can open it.
	if err := os.MkdirAll(dirPath, utils.DirPerm); err != nil {
		return errors.WrapIf(err, "failed to create directory")
	}

	composeFileName := DefaultComposeFileName
	if existingFile := detectExistingComposeFileInternal(ctx, projectsRoot, dirPath); existingFile != "" {
		composeFileName = filepath.Base(existingFile)
	}

	if err := acfs.WriteFile(ctx, dirPath, "/"+composeFileName, []byte(content), utils.FilePerm); err != nil {
		return errors.WrapIf(err, "failed to write compose file")
	}

	return nil
}

func WriteProjectFile(ctx context.Context, projectsRoot, dirPath, fileName, content string) error {
	if _, err := acfs.LogicalPath(projectsRoot, dirPath); err != nil {
		return errors.WrapIf(err, "refusing to write project file: path outside projects root")
	}

	if fileName == "" || filepath.Base(fileName) != fileName || strings.Contains(fileName, string(filepath.Separator)) {
		return errors.Errorf("invalid project file name %q", fileName)
	}

	if err := os.MkdirAll(dirPath, utils.DirPerm); err != nil {
		return errors.WrapIf(err, "failed to create directory")
	}

	if fileName == EffectiveEnvFileName {
		written, err := writeEnvThroughSymlinkInternal(filepath.Join(dirPath, fileName), content)
		if err != nil {
			return errors.WrapIff(err, "failed to write project file %s", fileName)
		}
		if written {
			return nil
		}
	}

	logicalPath := "/" + fileName
	entry, statErr := acfs.Stat(ctx, dirPath, logicalPath, false)
	switch {
	case statErr == nil && entry.IsSymlink:
		// Only .env is written through a symlink (handled above); every other
		// project file refuses one rather than clobbering an unknown target.
		return errors.Errorf("refusing to write project file %s: destination is a symlink", fileName)
	case statErr != nil && !errors.Is(statErr, fs.ErrNotExist):
		return errors.WrapIff(statErr, "failed to inspect project file %s", fileName)
	}

	if statErr == nil {
		existingContent, readErr := acfs.ReadFile(ctx, dirPath, logicalPath)
		if readErr == nil && string(existingContent) == content {
			return nil
		}
		if readErr != nil && !errors.Is(readErr, fs.ErrNotExist) {
			return errors.WrapIff(readErr, "failed to read project file %s", fileName)
		}
	}

	if err := acfs.WriteFile(ctx, dirPath, logicalPath, []byte(content), utils.FilePerm); err != nil {
		return errors.WrapIff(err, "failed to write project file %s", fileName)
	}

	return nil
}

// writeEnvThroughSymlinkInternal writes content through a project .env that is
// a symbolic link and reports whether it did.
//
// The link target is allowed to live outside the projects root by design, so
// the write goes to the resolved absolute path rather than through the
// root-confined API; tightening that is the same class of regression as the
// includes handling in #3556. The resolved target is always a regular file.
func writeEnvThroughSymlinkInternal(envPath, content string) (bool, error) {
	resolvedPath, resolvedPerm, isSymlink, err := resolveEnvFileWriteTargetInternal(envPath)
	if err != nil {
		return false, errors.WrapIf(err, "resolve write target")
	}
	if !isSymlink {
		return false, nil
	}

	existingContent, readErr := os.ReadFile(resolvedPath)
	if readErr == nil && string(existingContent) == content {
		return true, nil
	}
	if readErr != nil && !errors.Is(readErr, os.ErrNotExist) {
		return true, errors.WrapIf(readErr, "read existing content")
	}
	return true, atomic.WriteFile(resolvedPath, []byte(content), resolvedPerm)
}

func resolveEnvFileWriteTargetInternal(envPath string) (writePath string, perm os.FileMode, isSymlink bool, err error) {
	info, err := os.Lstat(envPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return envPath, utils.FilePerm, false, nil
		}
		return "", 0, false, errors.WrapIf(err, "inspect env file")
	}
	if info.Mode()&os.ModeSymlink == 0 {
		return envPath, utils.FilePerm, false, nil
	}

	resolvedPath, err := filepath.EvalSymlinks(envPath)
	if err != nil {
		return "", 0, false, errors.WrapIf(err, "resolve env file symlink")
	}
	targetInfo, err := os.Stat(resolvedPath)
	if err != nil {
		return "", 0, false, errors.WrapIf(err, "inspect env file symlink target")
	}
	if !targetInfo.Mode().IsRegular() {
		return "", 0, false, errors.Errorf("env file symlink target is not a regular file: %s", resolvedPath)
	}

	return resolvedPath, targetInfo.Mode().Perm(), true, nil
}

func RemoveProjectFile(ctx context.Context, projectsRoot, dirPath, fileName string) error {
	if _, err := acfs.LogicalPath(projectsRoot, dirPath); err != nil {
		return errors.WrapIf(err, "refusing to remove project file: path outside projects root")
	}

	if fileName == "" || filepath.Base(fileName) != fileName || strings.Contains(fileName, string(filepath.Separator)) {
		return errors.Errorf("invalid project file name %q", fileName)
	}

	if err := acfs.Remove(ctx, dirPath, "/"+fileName); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return errors.WrapIff(err, "failed to remove project file %s", fileName)
	}

	return nil
}

func EnsureEnvFile(ctx context.Context, projectsRoot, dirPath string) error {
	if _, err := acfs.LogicalPath(projectsRoot, dirPath); err != nil {
		return errors.WrapIf(err, "refusing to create env file: path outside projects root")
	}

	exists, err := acfs.Exists(ctx, dirPath, "/.env")
	if err != nil {
		return errors.WrapIf(err, "failed to stat env file")
	}
	if exists {
		return nil
	}

	return WriteProjectFile(ctx, projectsRoot, dirPath, ".env", "")
}

// WriteProjectFiles writes both compose and env files to a project directory.
// An empty .env file is always created to prevent compose-go from failing when
// the compose file references env_file: .env
// projectsRoot is the allowed root directory to prevent path traversal attacks
func WriteProjectFiles(ctx context.Context, projectsRoot, dirPath, composeContent string, envContent *string) error {
	if err := WriteComposeFile(ctx, projectsRoot, dirPath, composeContent); err != nil {
		return err
	}

	// If envContent is nil, we check if .env already exists.
	// We only create an empty one if it doesn't exist, to satisfy
	// compose-go from failing when the compose file references env_file: .env
	if envContent != nil {
		if err := WriteProjectFile(ctx, projectsRoot, dirPath, ".env", *envContent); err != nil {
			return err
		}
	} else {
		if err := EnsureEnvFile(ctx, projectsRoot, dirPath); err != nil {
			return err
		}
	}

	return nil
}

// WriteTemplateFile writes a template file (like .compose.template or .env.template)
func WriteTemplateFile(filePath, content string) error {
	dir := filepath.Dir(filePath)
	if err := os.MkdirAll(dir, utils.DirPerm); err != nil {
		return errors.WrapIf(err, "failed to create template directory")
	}

	if err := atomic.WriteFile(filePath, []byte(content), utils.FilePerm); err != nil {
		return errors.WrapIf(err, "failed to write template file")
	}

	return nil
}

// WriteFileWithPerm is a generic file writer with custom permissions
func WriteFileWithPerm(filePath, content string, perm os.FileMode) error {
	dir := filepath.Dir(filePath)
	if err := os.MkdirAll(dir, utils.DirPerm); err != nil {
		return errors.WrapIf(err, "failed to create directory")
	}

	if err := atomic.WriteFile(filePath, []byte(content), perm); err != nil {
		return errors.WrapIf(err, "failed to write file")
	}

	return nil
}

// RollbackRenamedProjectDirectory restores a project directory rename when possible.
func RollbackRenamedProjectDirectory(ctx context.Context, oldPath, newPath string) (pathsMissing bool, err error) {
	oldPath = filepath.Clean(oldPath)
	newPath = filepath.Clean(newPath)
	if oldPath == "" || newPath == "" || oldPath == newPath {
		return false, nil
	}

	oldExists, _ := pathExistsInternal(oldPath)
	newExists, _ := pathExistsInternal(newPath)
	switch {
	case oldExists && newExists:
		conflictPath, err := relocateRenameConflictDirectoryInternal(ctx, newPath)
		if err != nil {
			slog.Warn("project rename directory rollback found both paths and failed to relocate target path; keeping old path and clearing journal", "oldPath", oldPath, "newPath", newPath, "error", err)
		} else {
			slog.Warn("project rename directory rollback found both paths; moved target path aside and kept old path", "oldPath", oldPath, "newPath", newPath, "conflictPath", conflictPath)
		}
	case !oldExists && newExists:
		// Both paths share a parent whenever the rename stayed inside the
		// projects directory; an imported project can sit elsewhere, in which
		// case the move crosses roots and cannot be a confined rename.
		var renameErr error
		if parent := filepath.Dir(newPath); parent == filepath.Dir(oldPath) {
			renameErr = acfs.Rename(ctx, parent, "/"+filepath.Base(newPath), "/"+filepath.Base(oldPath))
		} else {
			renameErr = os.Rename(newPath, oldPath)
		}
		if renameErr != nil {
			return false, errors.WrapIf(renameErr, "rollback project directory rename")
		}
	case !oldExists:
		pathsMissing = true
		slog.Warn("project rename directory paths are missing during rollback", "oldPath", oldPath, "newPath", newPath)
	}
	return pathsMissing, nil
}

func relocateRenameConflictDirectoryInternal(ctx context.Context, path string) (string, error) {
	parent := filepath.Dir(path)
	base := filepath.Base(path)
	now := time.Now().UTC().UnixNano()
	for attempt := range 10 {
		conflictName := fmt.Sprintf(".%s.rename-conflict-%d-%d", base, now, attempt)
		if exists, err := acfs.Exists(ctx, parent, "/"+conflictName); err != nil {
			return "", errors.WrapIf(err, "check conflict path")
		} else if exists {
			continue
		}
		if err := acfs.Rename(ctx, parent, "/"+base, "/"+conflictName); err != nil {
			return "", errors.WrapIf(err, "relocate project rename target path")
		}
		return filepath.Join(parent, conflictName), nil
	}
	return "", errors.Errorf("relocate project rename target path: no available conflict path for %s", path)
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
func WriteSyncedDirectory(ctx context.Context, projectsRoot, projectPath string, files []SyncFile) ([]string, error) {
	if _, err := acfs.LogicalPath(projectsRoot, projectPath); err != nil {
		return nil, errors.WrapIf(err, "project path is outside projects root")
	}

	// The project directory is the confinement root for every write below, so
	// it has to exist before acfs can open it.
	if err := os.MkdirAll(projectPath, utils.DirPerm); err != nil {
		return nil, errors.WrapIf(err, "failed to create project directory")
	}

	writtenPaths := make([]string, 0, len(files))

	for _, file := range files {
		logicalPath, err := acfs.LogicalPath(projectPath, filepath.Join(projectPath, file.RelativePath))
		if err != nil {
			return nil, errors.Errorf("file path %s would escape project directory", file.RelativePath)
		}

		if err := acfs.MkdirAll(ctx, projectPath, path.Dir(logicalPath), utils.DirPerm); err != nil {
			return nil, errors.WrapIff(err, "failed to create directory for %s", file.RelativePath)
		}

		entry, statErr := acfs.Stat(ctx, projectPath, logicalPath, false)
		switch {
		case statErr == nil && entry.IsDirectory:
			if err := acfs.RemoveAll(ctx, projectPath, logicalPath); err != nil {
				return nil, errors.WrapIff(err, "failed to replace directory at %s", file.RelativePath)
			}
		case statErr != nil && !errors.Is(statErr, fs.ErrNotExist):
			return nil, errors.WrapIff(statErr, "failed to inspect target path for %s", file.RelativePath)
		}

		// Write the file. Honor the source's executable bit so scripts arrive
		// runnable for lifecycle hooks and similar consumers.
		perm := utils.FilePerm
		if file.Executable {
			perm = 0o755
		}
		if err := acfs.WriteFile(ctx, projectPath, logicalPath, file.Content, perm); err != nil {
			return nil, errors.WrapIff(err, "failed to write file %s", file.RelativePath)
		}

		writtenPaths = append(writtenPaths, file.RelativePath)
	}

	return writtenPaths, nil
}

// CleanupRemovedFiles deletes files that were in the old sync but are not in the new sync.
// It only removes files that were previously synced (tracked in oldFiles).
// Empty directories are removed after file deletion.
// This is a best-effort operation - errors are logged but don't cause failure.
func CleanupRemovedFiles(ctx context.Context, projectsRoot, projectPath string, oldFiles, newFiles []string) error {
	if _, err := acfs.LogicalPath(projectsRoot, projectPath); err != nil {
		return errors.WrapIf(err, "project path is outside projects root")
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

		logicalPath, err := acfs.LogicalPath(projectPath, filepath.Join(projectPath, oldFile))
		if err != nil {
			continue // Skip files that would be outside project
		}

		// Delete the file (best effort)
		if err := acfs.Remove(ctx, projectPath, logicalPath); err != nil && !errors.Is(err, fs.ErrNotExist) {
			// Log but continue - this is best effort
			slog.WarnContext(ctx, "Failed to remove old synced file", "file", oldFile, "error", err)
		}

		// Track parent directory for potential cleanup
		if parentLogical := path.Dir(logicalPath); parentLogical != "/" {
			dirsToCheck[parentLogical] = true
		}
	}

	// Clean up empty directories (best effort, deepest first)
	for dir := range dirsToCheck {
		cleanupEmptyDirs(ctx, projectPath, dir)
	}

	return nil
}

// cleanupEmptyDirs removes empty directories starting from the given logical
// path up to (but not including) the project directory. It stops at the first
// error of any kind: a non-empty directory, an already-removed one, or
// anything else.
func cleanupEmptyDirs(ctx context.Context, projectPath, startLogical string) {
	for current := startLogical; current != "/"; current = path.Dir(current) {
		if err := acfs.Remove(ctx, projectPath, current); err != nil {
			return
		}
	}
}
