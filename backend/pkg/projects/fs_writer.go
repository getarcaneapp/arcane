package projects

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/compose-spec/compose-go/v2/cli"
	"go.getarcane.app/acfs"
	"go.getarcane.app/acfs/atomic"
	kit "go.getarcane.app/kit/pkg"

	"github.com/getarcaneapp/arcane/backend/v2/pkg/utils"
)

const (
	// DefaultComposeFileName is the compose filename Arcane writes when a project
	// has no existing compose file.
	DefaultComposeFileName = "compose.yaml"

	// DefaultComposeOverrideFileName is the override filename Arcane writes when a
	// project gains an override but has none on disk. It is deliberately
	// composeOverrideFileCandidates[1] ("compose.override.yaml"), NOT [0]
	// ("compose.override.yml"), so the default override extension matches Arcane's
	// ".yaml" base default (DefaultComposeFileName = "compose.yaml"). Do not "fix"
	// this to [0]: `docker compose` has no preference between the two, and keeping
	// ".yaml" avoids a mismatched base/override extension pair.
	DefaultComposeOverrideFileName = "compose.override.yaml"
)

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
	return kit.Ternary(err == nil, composePath, "")
}

// WriteComposeFile writes a compose file to the specified directory.
// It detects existing compose file names (docker-compose.yml, compose.yaml, etc.)
// and uses the existing name if found, otherwise defaults to compose.yaml
// projectsRoot is the allowed root directory to prevent path traversal attacks
func WriteComposeFile(ctx context.Context, projectsRoot, dirPath, content string) error {
	if _, err := acfs.LogicalPath(projectsRoot, dirPath); err != nil {
		return fmt.Errorf("refusing to write compose file: path outside projects root: %w", err)
	}

	// The project directory is the confinement root for the write itself, so it
	// has to exist before acfs can open it.
	if err := os.MkdirAll(dirPath, utils.DirPerm); err != nil {
		return fmt.Errorf("failed to create directory: %w", err)
	}

	composeFileName := DefaultComposeFileName
	mode := utils.FilePerm
	if existingFile := detectExistingComposeFileInternal(ctx, projectsRoot, dirPath); existingFile != "" {
		composeFileName = filepath.Base(existingFile)
		if entry, statErr := acfs.Stat(ctx, dirPath, "/"+composeFileName, false); statErr == nil {
			mode = os.FileMode(entry.UnixMode).Perm()
		}
	}

	if err := acfs.Write(ctx, dirPath, "/"+composeFileName, []byte(content), acfs.WriteOptions{Mode: mode, InPlace: true}); err != nil {
		return fmt.Errorf("failed to write compose file: %w", err)
	}

	return nil
}

func WriteProjectFile(ctx context.Context, projectsRoot, dirPath, fileName, content string) error {
	if _, err := acfs.LogicalPath(projectsRoot, dirPath); err != nil {
		return fmt.Errorf("refusing to write project file: path outside projects root: %w", err)
	}

	if fileName == "" || filepath.Base(fileName) != fileName || strings.Contains(fileName, string(filepath.Separator)) {
		return fmt.Errorf("invalid project file name %q", fileName)
	}

	if err := os.MkdirAll(dirPath, utils.DirPerm); err != nil {
		return fmt.Errorf("failed to create directory: %w", err)
	}

	if fileName == EffectiveEnvFileName {
		written, err := writeEnvThroughSymlinkInternal(filepath.Join(dirPath, fileName), content)
		if err != nil {
			return fmt.Errorf("failed to write project file %s: %w", fileName, err)
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
		return fmt.Errorf("refusing to write project file %s: destination is a symlink", fileName)
	case statErr != nil && !errors.Is(statErr, fs.ErrNotExist):
		return fmt.Errorf("failed to inspect project file %s: %w", fileName, statErr)
	}

	mode := utils.FilePerm
	if statErr == nil {
		existingContent, readErr := acfs.ReadFile(ctx, dirPath, logicalPath)
		if readErr == nil && string(existingContent) == content {
			return nil
		}
		if readErr != nil && !errors.Is(readErr, fs.ErrNotExist) {
			return fmt.Errorf("failed to read project file %s: %w", fileName, readErr)
		}
		mode = os.FileMode(entry.UnixMode).Perm()
	}

	if err := acfs.Write(ctx, dirPath, logicalPath, []byte(content), acfs.WriteOptions{Mode: mode, InPlace: true}); err != nil {
		return fmt.Errorf("failed to write project file %s: %w", fileName, err)
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
		return false, fmt.Errorf("resolve write target: %w", err)
	}
	if !isSymlink {
		return false, nil
	}

	existingContent, readErr := os.ReadFile(resolvedPath)
	if readErr == nil && string(existingContent) == content {
		return true, nil
	}
	if readErr != nil && !errors.Is(readErr, os.ErrNotExist) {
		return true, fmt.Errorf("read existing content: %w", readErr)
	}
	return true, atomic.WriteFile(resolvedPath, []byte(content), resolvedPerm)
}

func resolveEnvFileWriteTargetInternal(envPath string) (writePath string, perm os.FileMode, isSymlink bool, err error) {
	info, err := os.Lstat(envPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return envPath, utils.FilePerm, false, nil
		}
		return "", 0, false, fmt.Errorf("inspect env file: %w", err)
	}
	if info.Mode()&os.ModeSymlink == 0 {
		return envPath, utils.FilePerm, false, nil
	}

	resolvedPath, err := filepath.EvalSymlinks(envPath)
	if err != nil {
		return "", 0, false, fmt.Errorf("resolve env file symlink: %w", err)
	}
	targetInfo, err := os.Stat(resolvedPath)
	if err != nil {
		return "", 0, false, fmt.Errorf("inspect env file symlink target: %w", err)
	}
	if !targetInfo.Mode().IsRegular() {
		return "", 0, false, fmt.Errorf("env file symlink target is not a regular file: %s", resolvedPath)
	}

	return resolvedPath, targetInfo.Mode().Perm(), true, nil
}

func RemoveProjectFile(ctx context.Context, projectsRoot, dirPath, fileName string) error {
	if _, err := acfs.LogicalPath(projectsRoot, dirPath); err != nil {
		return fmt.Errorf("refusing to remove project file: path outside projects root: %w", err)
	}

	if fileName == "" || filepath.Base(fileName) != fileName || strings.Contains(fileName, string(filepath.Separator)) {
		return fmt.Errorf("invalid project file name %q", fileName)
	}

	if err := acfs.Remove(ctx, dirPath, "/"+fileName); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("failed to remove project file %s: %w", fileName, err)
	}

	return nil
}

func EnsureEnvFile(ctx context.Context, projectsRoot, dirPath string) error {
	if _, err := acfs.LogicalPath(projectsRoot, dirPath); err != nil {
		return fmt.Errorf("refusing to create env file: path outside projects root: %w", err)
	}

	exists, err := acfs.Exists(ctx, dirPath, "/.env")
	if err != nil {
		return fmt.Errorf("failed to stat env file: %w", err)
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
		return fmt.Errorf("failed to create template directory: %w", err)
	}

	if err := atomic.WriteFile(filePath, []byte(content), utils.FilePerm); err != nil {
		return fmt.Errorf("failed to write template file: %w", err)
	}

	return nil
}

// WriteFileWithPerm is a generic file writer with custom permissions
func WriteFileWithPerm(filePath, content string, perm os.FileMode) error {
	dir := filepath.Dir(filePath)
	if err := os.MkdirAll(dir, utils.DirPerm); err != nil {
		return fmt.Errorf("failed to create directory: %w", err)
	}

	if err := atomic.WriteFile(filePath, []byte(content), perm); err != nil {
		return fmt.Errorf("failed to write file: %w", err)
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
		conflictPath, relocateRenameConflictDirectoryErr := relocateRenameConflictDirectoryInternal(ctx, newPath)
		if relocateRenameConflictDirectoryErr != nil {
			slog.WarnContext(ctx, "project rename directory rollback found both paths and failed to relocate target path; keeping old path and clearing journal",
				"oldPath", oldPath, "newPath", newPath, "error", relocateRenameConflictDirectoryErr)
		} else {
			slog.WarnContext(ctx, "project rename directory rollback found both paths; moved target path aside and kept old path", "oldPath", oldPath, "newPath", newPath, "conflictPath", conflictPath)
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
			return false, fmt.Errorf("rollback project directory rename: %w", renameErr)
		}
	case !oldExists:
		pathsMissing = true
		slog.WarnContext(ctx, "project rename directory paths are missing during rollback", "oldPath", oldPath, "newPath", newPath)
	}
	return pathsMissing, nil
}

func relocateRenameConflictDirectoryInternal(ctx context.Context, localPath string) (string, error) {
	parent := filepath.Dir(localPath)
	base := filepath.Base(localPath)
	now := time.Now().UTC().UnixNano()
	for attempt := range 10 {
		conflictName := fmt.Sprintf(".%s.rename-conflict-%d-%d", base, now, attempt)
		if exists, err := acfs.Exists(ctx, parent, "/"+conflictName); err != nil {
			return "", fmt.Errorf("check conflict path: %w", err)
		} else if exists {
			continue
		}
		if err := acfs.Rename(ctx, parent, "/"+base, "/"+conflictName); err != nil {
			return "", fmt.Errorf("relocate project rename target path: %w", err)
		}
		return filepath.Join(parent, conflictName), nil
	}
	return "", fmt.Errorf("relocate project rename target path: no available conflict path for %s", localPath)
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
		return nil, fmt.Errorf("project path is outside projects root: %w", err)
	}

	// The project directory is the confinement root for every write below, so
	// it has to exist before acfs can open it.
	if err := os.MkdirAll(projectPath, utils.DirPerm); err != nil {
		return nil, fmt.Errorf("failed to create project directory: %w", err)
	}

	writtenPaths := make([]string, 0, len(files))

	for _, file := range files {
		logicalPath, err := acfs.LogicalPath(projectPath, filepath.Join(projectPath, file.RelativePath))
		if err != nil {
			return nil, fmt.Errorf("file path %s would escape project directory", file.RelativePath)
		}

		if mkdirAllErr := acfs.MkdirAll(ctx, projectPath, path.Dir(logicalPath), utils.DirPerm); mkdirAllErr != nil {
			return nil, fmt.Errorf("failed to create directory for %s: %w", file.RelativePath, mkdirAllErr)
		}

		entry, statErr := acfs.Stat(ctx, projectPath, logicalPath, false)
		switch {
		case statErr == nil && entry.IsDirectory:
			if removeAllErr := acfs.RemoveAll(ctx, projectPath, logicalPath); removeAllErr != nil {
				return nil, fmt.Errorf("failed to replace directory at %s: %w", file.RelativePath, removeAllErr)
			}
		case statErr != nil && !errors.Is(statErr, fs.ErrNotExist):
			return nil, fmt.Errorf("failed to inspect target path for %s: %w", file.RelativePath, statErr)
		}

		// Write the file. Honor the source's executable bit so scripts arrive
		// runnable for lifecycle hooks and similar consumers.
		perm := kit.Ternary(file.Executable, 0o755, utils.FilePerm)
		if writeErr := acfs.Write(ctx, projectPath, logicalPath, file.Content, acfs.WriteOptions{Mode: perm, InPlace: true}); writeErr != nil {
			return nil, fmt.Errorf("failed to write file %s: %w", file.RelativePath, writeErr)
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
		return fmt.Errorf("project path is outside projects root: %w", err)
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
		if removeErr := acfs.Remove(ctx, projectPath, logicalPath); removeErr != nil && !errors.Is(removeErr, fs.ErrNotExist) {
			// Log but continue - this is best effort
			slog.WarnContext(ctx, "Failed to remove old synced file", "file", oldFile, "error", removeErr)
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

// composeOverrideFileCandidates lists supported compose override filenames in
// detection order, sourced from compose-go.
var composeOverrideFileCandidates = slices.Clone(cli.DefaultOverrideFileNames)

// ComposeOverrideFileCandidates returns the supported compose override filenames
// in detection order. A copy is returned so callers can't mutate package state.
func ComposeOverrideFileCandidates() []string {
	return slices.Clone(composeOverrideFileCandidates)
}

// findComposeOverrideCandidatesInternal returns every supported override
// candidate name for which exists reports true, in compose-go preference order.
// It is the single primitive behind both filesystem discovery
// (DetectComposeOverrideFile) and non-filesystem discovery
// (ResolveComposeOverride), so the two can never drift in preference order.
func findComposeOverrideCandidatesInternal(exists func(name string) bool) []string {
	found := make([]string, 0, 1)
	for _, name := range composeOverrideFileCandidates {
		if exists(name) {
			found = append(found, name)
		}
	}
	return found
}

// warnOnMultipleComposeOverridesInternal mirrors compose-go: when more than one
// override candidate is present it logs which candidates were found and which
// highest-preference match will be used.
func warnOnMultipleComposeOverridesInternal(found []string) {
	if len(found) > 1 {
		ctx := context.Background() //nolint:forbidigo // Compose file discovery is a pure helper without a request context.
		slog.WarnContext(ctx, "multiple compose override files found; using highest-preference match",
			"using", found[0], "found", found)
	}
}

// DetectComposeOverrideFile returns the path to the highest-preference compose
// override file present in dir, following compose-go's preference order, or ""
// when none exists. When multiple override files are present it returns the
// highest-preference match and logs a warning, mirroring compose-go behavior.
func DetectComposeOverrideFile(dir string) string {
	// Stays on os.*: override files may be symlinks resolving outside any
	// confinement root (same class as #3556 includes), which acfs cannot follow.
	found := findComposeOverrideCandidatesInternal(func(name string) bool {
		info, err := os.Stat(filepath.Join(dir, name))
		return err == nil && !info.IsDir()
	})
	if len(found) == 0 {
		return ""
	}
	warnOnMultipleComposeOverridesInternal(found)
	return filepath.Join(dir, found[0])
}

// ReadComposeOverrideContent returns the content of the highest-preference
// compose override file present in dir, or "" when none exists or it cannot be
// read. It is a best-effort read intended for change detection.
func ReadComposeOverrideContent(dir string) string {
	overridePath := DetectComposeOverrideFile(dir)
	if overridePath == "" {
		return ""
	}
	content, err := os.ReadFile(overridePath)
	return kit.Ternary(err != nil, "", string(content))
}

// ResolveComposeOverride finds the highest-preference compose override file among
// the supported candidates using the exists probe, then loads it via read. It
// centralizes override discovery so non-filesystem sources (e.g. a Git working
// tree accessed through a validated client) reuse the same preference order.
// When multiple candidates exist it warns and uses the highest-preference match,
// mirroring DetectComposeOverrideFile. found is false (with empty name/content)
// when no candidate exists.
func ResolveComposeOverride(exists func(name string) bool, read func(name string) (string, error)) (fileName, content string, found bool, err error) {
	matches := findComposeOverrideCandidatesInternal(exists)
	if len(matches) == 0 {
		return "", "", false, nil
	}
	warnOnMultipleComposeOverridesInternal(matches)
	name := matches[0]
	c, readErr := read(name)
	if readErr != nil {
		return "", "", false, readErr
	}
	return name, c, true, nil
}

// WriteComposeOverrideFile writes content as the override file named fileName in
// dir when content is non-nil, and removes every other override candidate so a
// renamed or deleted override never leaves a stale copy behind. When content is
// nil, all supported override files are removed. projectsRoot bounds writes to
// prevent path traversal.
func WriteComposeOverrideFile(ctx context.Context, projectsRoot, dir string, content *string, fileName string) error {
	keep := ""
	if content != nil {
		keep = strings.TrimSpace(fileName)
		if keep == "" {
			return errors.New("missing override file name")
		}
		if err := WriteProjectFile(ctx, projectsRoot, dir, keep, *content); err != nil {
			return err
		}
	}

	// Remove stale override files: deleted from the source, or renamed to a
	// different candidate name than the one currently written.
	for _, candidate := range composeOverrideFileCandidates {
		if candidate == keep {
			continue
		}
		if err := RemoveProjectFile(ctx, projectsRoot, dir, candidate); err != nil {
			return err
		}
	}

	return nil
}

// ExistingOrDefaultOverrideName returns the override file name already present
// in dir, falling back to DefaultComposeOverrideFileName.
func ExistingOrDefaultOverrideName(dir string) string {
	if overridePath := DetectComposeOverrideFile(dir); overridePath != "" {
		return filepath.Base(overridePath)
	}
	return DefaultComposeOverrideFileName
}

// ResolveEffectiveOverrideForValidation resolves the override content compose
// validation should see: the on-disk override when the caller supplied none,
// nothing when the caller explicitly blanked it, otherwise the caller's content.
func ResolveEffectiveOverrideForValidation(dir string, overrideContent *string) (*string, string) {
	switch {
	case overrideContent == nil:
		overridePath := DetectComposeOverrideFile(dir)
		if overridePath == "" {
			return nil, ""
		}
		onDisk := ReadComposeOverrideContent(dir)
		return &onDisk, filepath.Base(overridePath)
	case strings.TrimSpace(*overrideContent) == "":
		return nil, ""
	default:
		return overrideContent, ExistingOrDefaultOverrideName(dir)
	}
}

// ApplyOverrideFileChange writes, clears, or leaves the override file alone
// depending on whether overrideContent is nil, blank, or set.
func ApplyOverrideFileChange(ctx context.Context, projectsRoot, dir string, overrideContent *string) error {
	if overrideContent == nil {
		return nil
	}
	if strings.TrimSpace(*overrideContent) == "" {
		return WriteComposeOverrideFile(ctx, projectsRoot, dir, nil, "")
	}
	return WriteComposeOverrideFile(ctx, projectsRoot, dir, overrideContent, ExistingOrDefaultOverrideName(dir))
}
