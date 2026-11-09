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

	"kit.getarcane.app"
	"kit.getarcane.app/acfs"
	acfstypes "kit.getarcane.app/acfs/types"
	kitfs "kit.getarcane.app/fs"
	"kit.getarcane.app/fs/atomic"
)

type (
	SyncFile struct {
		RelativePath string
		Content      []byte
		Executable   bool
	}
)

const (
	DefaultComposeFileName = "compose.yaml"
	// DefaultComposeOverrideFileName is the override filename Arcane writes when a
	// project gains an override but has none on disk. It is deliberately
	// ComposeOverrideFileCandidates[1] ("compose.override.yaml"), NOT [0]
	// ("compose.override.yml"), so the default override extension matches Arcane's
	// ".yaml" base default (DefaultComposeFileName = "compose.yaml"). Do not "fix"
	// this to [0]: `docker compose` has no preference between the two, and keeping
	// ".yaml" avoids a mismatched base/override extension pair.
	DefaultComposeOverrideFileName = "compose.override.yaml"
)

var (
	ComposeFileCandidates = []string{
		DefaultComposeFileName,
		"compose.yml",
		"docker-compose.yaml",
		"docker-compose.yml",
		"podman-compose.yaml",
		"podman-compose.yml",
	}

	ComposeOverrideFileCandidates = slices.Clone(cli.DefaultOverrideFileNames)
)

func detectExistingComposeFile(ctx context.Context, projectsRoot, dir string) string {
	composePath, err := DetectComposeFile(ctx, projectsRoot, dir)
	return kit.Ternary(err == nil, composePath, "")
}

func WriteComposeFile(ctx context.Context, projectsRoot, dirPath, content string) error {
	if _, err := (acfs.Root{Path: projectsRoot}).LogicalPath(dirPath); err != nil {
		return fmt.Errorf("refusing to write compose file: path outside projects root: %w", err)
	}

	if err := (acfs.Host{}).MkdirAll(ctx, dirPath, kitfs.DirPerm); err != nil {
		return fmt.Errorf("failed to create directory: %w", err)
	}

	composeFileName := DefaultComposeFileName
	mode := kitfs.FilePerm
	if existingFile := detectExistingComposeFile(ctx, projectsRoot, dirPath); existingFile != "" {
		composeFileName = filepath.Base(existingFile)
		if entry, statErr := (acfs.Root{Path: dirPath}).Stat(ctx, "/"+composeFileName, acfstypes.StatOptions{}); statErr == nil {
			mode = os.FileMode(entry.UnixMode).Perm()
		}
	}

	if err := (acfs.Root{Path: dirPath}).Write(ctx, "/"+composeFileName, []byte(content), acfs.WriteOptions{Mode: mode, InPlace: true}); err != nil {
		if errors.Is(err, fs.ErrPermission) {
			return fmt.Errorf(
				"failed to write compose file %s: not writable by uid %d gid %d (check the file owner matches PUID/PGID): %w",
				filepath.Join(dirPath, composeFileName), os.Geteuid(), os.Getegid(), err,
			)
		}
		return fmt.Errorf("failed to write compose file %s: %w", filepath.Join(dirPath, composeFileName), err)
	}

	return nil
}

func WriteProjectFile(ctx context.Context, projectsRoot, dirPath, fileName, content string) error {
	if _, err := (acfs.Root{Path: projectsRoot}).LogicalPath(dirPath); err != nil {
		return fmt.Errorf("refusing to write project file: path outside projects root: %w", err)
	}

	if fileName == "" || filepath.Base(fileName) != fileName || strings.Contains(fileName, string(filepath.Separator)) {
		return fmt.Errorf("invalid project file name %q", fileName)
	}

	if err := (acfs.Host{}).MkdirAll(ctx, dirPath, kitfs.DirPerm); err != nil {
		return fmt.Errorf("failed to create directory: %w", err)
	}

	if fileName == EffectiveEnvFileName {
		written, err := writeEnvThroughSymlink(ctx, filepath.Join(dirPath, fileName), content)
		if err != nil {
			return fmt.Errorf("failed to write project file %s: %w", fileName, err)
		}
		if written {
			return nil
		}
	}

	logicalPath := "/" + fileName
	entry, statErr := acfs.Root{Path: dirPath}.Stat(ctx, logicalPath, acfstypes.StatOptions{})
	switch {
	case statErr == nil && entry.IsSymlink:
		// Only .env is written through a symlink (handled above); every other
		// project file refuses one rather than clobbering an unknown target.
		return fmt.Errorf("refusing to write project file %s: destination is a symlink", fileName)
	case statErr != nil && !errors.Is(statErr, fs.ErrNotExist):
		return fmt.Errorf("failed to inspect project file %s: %w", fileName, statErr)
	}

	mode := kitfs.FilePerm
	if statErr == nil {
		existingContent, readErr := acfs.Root{Path: dirPath}.ReadFile(ctx, logicalPath)
		if readErr == nil && string(existingContent) == content {
			return nil
		}
		if readErr != nil && !errors.Is(readErr, fs.ErrNotExist) {
			return fmt.Errorf("failed to read project file %s: %w", fileName, readErr)
		}
		mode = os.FileMode(entry.UnixMode).Perm()
	}

	if err := (acfs.Root{Path: dirPath}).Write(ctx, logicalPath, []byte(content), acfs.WriteOptions{Mode: mode, InPlace: true}); err != nil {
		return fmt.Errorf("failed to write project file %s: %w", fileName, err)
	}

	return nil
}

func writeEnvThroughSymlink(ctx context.Context, envPath, content string) (bool, error) {
	resolvedPath, resolvedPerm, isSymlink, err := resolveEnvFileWriteTarget(ctx, envPath)
	if err != nil {
		return false, fmt.Errorf("resolve write target: %w", err)
	}
	if !isSymlink {
		return false, nil
	}

	existingContent, readErr := acfs.Host{}.ReadFile(ctx, resolvedPath)
	if readErr == nil && string(existingContent) == content {
		return true, nil
	}
	if readErr != nil && !errors.Is(readErr, os.ErrNotExist) {
		return true, fmt.Errorf("read existing content: %w", readErr)
	}
	return true, atomic.WriteFile(resolvedPath, []byte(content), resolvedPerm)
}

func resolveEnvFileWriteTarget(ctx context.Context, envPath string) (writePath string, perm os.FileMode, isSymlink bool, err error) {
	entry, err := acfs.Host{}.Stat(ctx, envPath, acfstypes.StatOptions{})
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return envPath, kitfs.FilePerm, false, nil
		}
		return "", 0, false, fmt.Errorf("inspect env file: %w", err)
	}
	if !entry.IsSymlink {
		return envPath, kitfs.FilePerm, false, nil
	}

	resolvedPath, err := filepath.EvalSymlinks(envPath)
	if err != nil {
		return "", 0, false, fmt.Errorf("resolve env file symlink: %w", err)
	}
	target, err := acfs.Host{}.Stat(ctx, resolvedPath, acfstypes.StatOptions{FollowFinal: true})
	if err != nil {
		return "", 0, false, fmt.Errorf("inspect env file symlink target: %w", err)
	}
	if !os.FileMode(target.UnixMode).IsRegular() {
		return "", 0, false, fmt.Errorf("env file symlink target is not a regular file: %s", resolvedPath)
	}

	return resolvedPath, os.FileMode(target.UnixMode).Perm(), true, nil
}

func RemoveProjectFile(ctx context.Context, projectsRoot, dirPath, fileName string) error {
	if _, err := (acfs.Root{Path: projectsRoot}).LogicalPath(dirPath); err != nil {
		return fmt.Errorf("refusing to remove project file: path outside projects root: %w", err)
	}

	if fileName == "" || filepath.Base(fileName) != fileName || strings.Contains(fileName, string(filepath.Separator)) {
		return fmt.Errorf("invalid project file name %q", fileName)
	}

	if err := (acfs.Root{Path: dirPath}).Remove(ctx, "/"+fileName); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("failed to remove project file %s: %w", fileName, err)
	}

	return nil
}

func WriteProjectFiles(ctx context.Context, projectsRoot, dirPath, composeContent string, envContent *string) error {
	if err := WriteComposeFile(ctx, projectsRoot, dirPath, composeContent); err != nil {
		return err
	}

	if strings.TrimSpace(kit.FromPtr(envContent)) == "" {
		return nil
	}

	return WriteProjectFile(ctx, projectsRoot, dirPath, ".env", *envContent)
}

func WriteTemplateFile(ctx context.Context, filePath, content string) error {
	if err := (acfs.Host{}).MkdirAll(ctx, filepath.Dir(filePath), kitfs.DirPerm); err != nil {
		return fmt.Errorf("failed to create template directory: %w", err)
	}

	if err := atomic.WriteFile(filePath, []byte(content), kitfs.FilePerm); err != nil {
		return fmt.Errorf("failed to write template file: %w", err)
	}

	return nil
}

func WriteFileWithPerm(ctx context.Context, filePath, content string, perm os.FileMode) error {
	if err := (acfs.Host{}).MkdirAll(ctx, filepath.Dir(filePath), kitfs.DirPerm); err != nil {
		return fmt.Errorf("failed to create directory: %w", err)
	}

	if err := atomic.WriteFile(filePath, []byte(content), perm); err != nil {
		return fmt.Errorf("failed to write file: %w", err)
	}

	return nil
}

func RollbackRenamedProjectDirectory(ctx context.Context, oldPath, newPath string) (pathsMissing bool, err error) {
	oldPath = filepath.Clean(oldPath)
	newPath = filepath.Clean(newPath)
	if oldPath == "" || newPath == "" || oldPath == newPath {
		return false, nil
	}

	oldExists, _ := acfs.Host{}.Exists(ctx, oldPath)
	newExists, _ := acfs.Host{}.Exists(ctx, newPath)
	switch {
	case oldExists && newExists:
		conflictPath, relocateRenameConflictDirectoryErr := relocateRenameConflictDirectory(ctx, newPath)
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
			renameErr = acfs.Root{Path: parent}.Rename(ctx, "/"+filepath.Base(newPath), "/"+filepath.Base(oldPath))
		} else {
			renameErr = acfs.Host{}.Rename(ctx, newPath, oldPath)
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

func relocateRenameConflictDirectory(ctx context.Context, localPath string) (string, error) {
	parent := filepath.Dir(localPath)
	base := filepath.Base(localPath)
	now := time.Now().UTC().UnixNano()
	for attempt := range 10 {
		conflictName := fmt.Sprintf(".%s.rename-conflict-%d-%d", base, now, attempt)
		if exists, err := (acfs.Root{Path: parent}).Exists(ctx, "/"+conflictName); err != nil {
			return "", fmt.Errorf("check conflict path: %w", err)
		} else if exists {
			continue
		}
		if err := (acfs.Root{Path: parent}).Rename(ctx, "/"+base, "/"+conflictName); err != nil {
			return "", fmt.Errorf("relocate project rename target path: %w", err)
		}
		return filepath.Join(parent, conflictName), nil
	}
	return "", fmt.Errorf("relocate project rename target path: no available conflict path for %s", localPath)
}

func WriteSyncedDirectory(ctx context.Context, projectsRoot, projectPath string, files []SyncFile) ([]string, error) {
	if _, err := (acfs.Root{Path: projectsRoot}).LogicalPath(projectPath); err != nil {
		return nil, fmt.Errorf("project path is outside projects root: %w", err)
	}

	if err := (acfs.Host{}).MkdirAll(ctx, projectPath, kitfs.DirPerm); err != nil {
		return nil, fmt.Errorf("failed to create project directory: %w", err)
	}

	writtenPaths := make([]string, 0, len(files))

	for _, file := range files {
		logicalPath, err := acfs.Root{Path: projectPath}.LogicalPath(filepath.Join(projectPath, file.RelativePath))
		if err != nil {
			return nil, fmt.Errorf("file path %s would escape project directory", file.RelativePath)
		}

		if mkdirAllErr := (acfs.Root{Path: projectPath}).MkdirAll(ctx, path.Dir(logicalPath), kitfs.DirPerm); mkdirAllErr != nil {
			return nil, fmt.Errorf("failed to create directory for %s: %w", file.RelativePath, mkdirAllErr)
		}

		entry, statErr := acfs.Root{Path: projectPath}.Stat(ctx, logicalPath, acfstypes.StatOptions{})
		switch {
		case statErr == nil && entry.IsDirectory:
			if removeAllErr := (acfs.Root{Path: projectPath}).RemoveAll(ctx, logicalPath); removeAllErr != nil {
				return nil, fmt.Errorf("failed to replace directory at %s: %w", file.RelativePath, removeAllErr)
			}
		case statErr != nil && !errors.Is(statErr, fs.ErrNotExist):
			return nil, fmt.Errorf("failed to inspect target path for %s: %w", file.RelativePath, statErr)
		}

		perm := kit.Ternary(file.Executable, 0o755, kitfs.FilePerm)
		if writeErr := (acfs.Root{Path: projectPath}).Write(ctx, logicalPath, file.Content, acfs.WriteOptions{Mode: perm, InPlace: true}); writeErr != nil {
			return nil, fmt.Errorf("failed to write file %s: %w", file.RelativePath, writeErr)
		}

		writtenPaths = append(writtenPaths, file.RelativePath)
	}

	return writtenPaths, nil
}

func CleanupRemovedFiles(ctx context.Context, projectsRoot, projectPath string, oldFiles, newFiles []string) error {
	if _, err := (acfs.Root{Path: projectsRoot}).LogicalPath(projectPath); err != nil {
		return fmt.Errorf("project path is outside projects root: %w", err)
	}

	newFileSet := make(map[string]bool, len(newFiles))
	for _, f := range newFiles {
		newFileSet[f] = true
	}

	dirsToCheck := make(map[string]bool)

	for _, oldFile := range oldFiles {
		if newFileSet[oldFile] {
			continue
		}

		logicalPath, err := acfs.Root{Path: projectPath}.LogicalPath(filepath.Join(projectPath, oldFile))
		if err != nil {
			continue
		}

		if removeErr := (acfs.Root{Path: projectPath}).Remove(ctx, logicalPath); removeErr != nil && !errors.Is(removeErr, fs.ErrNotExist) {
			slog.WarnContext(ctx, "Failed to remove old synced file", "file", oldFile, "error", removeErr)
		}

		if parentLogical := path.Dir(logicalPath); parentLogical != "/" {
			dirsToCheck[parentLogical] = true
		}
	}

	for dir := range dirsToCheck {
		cleanupEmptyDirs(ctx, projectPath, dir)
	}

	return nil
}

func cleanupEmptyDirs(ctx context.Context, projectPath, startLogical string) {
	for current := startLogical; current != "/"; current = path.Dir(current) {
		if err := (acfs.Root{Path: projectPath}).Remove(ctx, current); err != nil {
			return
		}
	}
}

func findComposeOverrideCandidates(exists func(name string) bool) []string {
	found := make([]string, 0, 1)
	for _, name := range ComposeOverrideFileCandidates {
		if exists(name) {
			found = append(found, name)
		}
	}
	return found
}

// warnOnMultipleComposeOverrides mirrors compose-go: when more than one
// override candidate is present it logs which candidates were found and which
// highest-preference match will be used.
func warnOnMultipleComposeOverrides(found []string) {
	if len(found) > 1 {
		ctx := context.Background() //nolint:forbidigo // Compose file discovery is a pure helper without a request context.
		slog.WarnContext(ctx, "multiple compose override files found; using highest-preference match",
			"using", found[0], "found", found)
	}
}

func DetectComposeOverrideFile(ctx context.Context, dir string) string {
	found := findComposeOverrideCandidates(func(name string) bool {
		entry, err := acfs.Host{}.Stat(ctx, filepath.Join(dir, name), acfstypes.StatOptions{FollowFinal: true})
		return err == nil && !entry.IsDirectory
	})
	if len(found) == 0 {
		return ""
	}
	warnOnMultipleComposeOverrides(found)
	return filepath.Join(dir, found[0])
}

func ReadComposeOverrideContent(ctx context.Context, dir string) string {
	overridePath := DetectComposeOverrideFile(ctx, dir)
	if overridePath == "" {
		return ""
	}
	content, err := acfs.Host{}.ReadFile(ctx, overridePath)
	return kit.Ternary(err != nil, "", string(content))
}

func ResolveComposeOverride(exists func(name string) bool, read func(name string) (string, error)) (fileName, content string, found bool, err error) {
	matches := findComposeOverrideCandidates(exists)
	if len(matches) == 0 {
		return "", "", false, nil
	}
	warnOnMultipleComposeOverrides(matches)
	name := matches[0]
	c, readErr := read(name)
	if readErr != nil {
		return "", "", false, readErr
	}
	return name, c, true, nil
}

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
	for _, candidate := range ComposeOverrideFileCandidates {
		if candidate == keep {
			continue
		}
		if err := RemoveProjectFile(ctx, projectsRoot, dir, candidate); err != nil {
			return err
		}
	}

	return nil
}

func ExistingOrDefaultOverrideName(ctx context.Context, dir string) string {
	if overridePath := DetectComposeOverrideFile(ctx, dir); overridePath != "" {
		return filepath.Base(overridePath)
	}
	return DefaultComposeOverrideFileName
}

func ResolveEffectiveOverrideForValidation(ctx context.Context, dir string, overrideContent *string) (*string, string) {
	switch {
	case overrideContent == nil:
		overridePath := DetectComposeOverrideFile(ctx, dir)
		if overridePath == "" {
			return nil, ""
		}
		onDisk := ReadComposeOverrideContent(ctx, dir)
		return &onDisk, filepath.Base(overridePath)
	case strings.TrimSpace(*overrideContent) == "":
		return nil, ""
	default:
		return overrideContent, ExistingOrDefaultOverrideName(ctx, dir)
	}
}

func ApplyOverrideFileChange(ctx context.Context, projectsRoot, dir string, overrideContent *string) error {
	if overrideContent == nil {
		return nil
	}
	if strings.TrimSpace(*overrideContent) == "" {
		return WriteComposeOverrideFile(ctx, projectsRoot, dir, nil, "")
	}
	return WriteComposeOverrideFile(ctx, projectsRoot, dir, overrideContent, ExistingOrDefaultOverrideName(ctx, dir))
}
