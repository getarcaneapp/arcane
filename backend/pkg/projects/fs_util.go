package projects

import (
	"bytes"
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"emperror.dev/errors"

	"github.com/getarcaneapp/arcane/backend/v2/internal/config"
	pkgutils "github.com/getarcaneapp/arcane/backend/v2/pkg/utils"
	"github.com/samber/mo"
	"go.yaml.in/yaml/v4"
)

func ResolveConfiguredContainerDirectory(configuredPath, defaultPath string) string {
	directory := strings.TrimSpace(configuredPath)
	if directory == "" {
		directory = defaultPath
	}

	// Handle mapping format: "container_path:host_path"
	if parts := strings.SplitN(directory, ":", 2); len(parts) == 2 {
		if !IsWindowsDrivePath(directory) && strings.HasPrefix(parts[0], "/") {
			directory = parts[0]
		}
	}

	return resolveProjectsDirectoryPath(directory)
}

func GetProjectsDirectory(ctx context.Context, projectsDir string) (string, error) {
	projectsDirectory := ResolveConfiguredContainerDirectory(projectsDir, "/app/data/projects")

	if _, err := os.Stat(projectsDirectory); os.IsNotExist(err) {
		if err := os.MkdirAll(projectsDirectory, pkgutils.DirPerm); err != nil {
			return "", err
		}
		slog.InfoContext(ctx, "Created projects directory", "path", projectsDirectory)
	}

	return projectsDirectory, nil
}

func resolveProjectsDirectoryPath(projectsDirectory string) string {
	if filepath.IsAbs(projectsDirectory) {
		return filepath.Clean(projectsDirectory)
	}

	if backendRoot, ok := findBackendModuleRoot().Get(); ok {
		return filepath.Clean(filepath.Join(backendRoot, projectsDirectory))
	}

	absDir, err := filepath.Abs(projectsDirectory)
	if err == nil {
		return filepath.Clean(absDir)
	}

	return filepath.Clean(projectsDirectory)
}

func findBackendModuleRoot() mo.Option[string] {
	cwd, err := os.Getwd()
	if err != nil {
		return mo.None[string]()
	}

	candidates := []string{
		cwd,
		filepath.Join(cwd, "backend"),
	}

	for _, candidate := range candidates {
		if isBackendModuleRoot(candidate) {
			return mo.Some(candidate)
		}
	}

	return mo.None[string]()
}

func isBackendModuleRoot(path string) bool {
	if _, err := os.Stat(filepath.Join(path, "go.mod")); err != nil {
		return false
	}
	if _, err := os.Stat(filepath.Join(path, "internal")); err != nil {
		return false
	}
	if _, err := os.Stat(filepath.Join(path, "pkg")); err != nil {
		return false
	}
	return true
}

func ReadProjectFiles(projectPath, composePath string) (composeContent, envContent string, err error) {
	if strings.TrimSpace(composePath) == "" {
		composePath, _ = DetectComposeFile(projectPath)
	}

	if strings.TrimSpace(composePath) != "" {
		if content, rerr := os.ReadFile(composePath); rerr == nil {
			composeContent = string(content)
		}
	}

	envPath := filepath.Join(projectPath, ".env")
	if content, rerr := os.ReadFile(envPath); rerr == nil {
		envContent = string(content)
	}

	return composeContent, envContent, nil
}

func HasComposeRootKeysInFile(path string) (bool, error) {
	content, err := os.ReadFile(path)
	if err != nil {
		return false, err
	}

	composeData := map[string]any{}
	if err := yaml.Unmarshal(content, &composeData); err != nil {
		return false, err
	}

	_, hasServices := composeData["services"]
	_, hasInclude := composeData["include"]
	return hasServices || hasInclude, nil
}

func GetTemplatesDirectory(ctx context.Context, templatesDir string) (string, error) {
	resolved := ResolveConfiguredContainerDirectory(templatesDir, "/app/data/templates")
	if _, err := os.Stat(resolved); os.IsNotExist(err) {
		if err := os.MkdirAll(resolved, pkgutils.DirPerm); err != nil {
			return "", err
		}
		slog.InfoContext(ctx, "Created templates directory", "path", resolved)
	}
	return resolved, nil
}

func projectScanSkipDirectorySetInternal(skipDirectories string) map[string]bool {
	if strings.TrimSpace(skipDirectories) == "" {
		skipDirectories = config.LoadProjectWorkspaceConfig().ProjectScanSkipDirs
	}

	dirs := map[string]bool{}
	for dir := range strings.SplitSeq(skipDirectories, ",") {
		dir = strings.TrimSpace(dir)
		if dir != "" {
			dirs[dir] = true
		}
	}

	// Never allow .git contents to be exposed through the project workspace.
	dirs[".git"] = true

	return dirs
}

func syncedProjectFileMatchesInternal(projectPath string, file SyncFile) (bool, error) {
	existingPath := filepath.Join(projectPath, file.RelativePath)
	info, err := os.Stat(existingPath)
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, err
	}
	if info.IsDir() {
		return false, nil
	}

	existingContent, err := os.ReadFile(existingPath)
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, err
	}

	return bytes.Equal(existingContent, file.Content), nil
}

func pathExistsInternal(path string) (bool, error) {
	_, err := os.Stat(path)
	if err == nil {
		return true, nil
	}
	if os.IsNotExist(err) {
		return false, nil
	}
	return false, err
}

func DirectorySyncContentsChanged(projectPath string, syncFiles []SyncFile, oldSyncedFiles []string, composeFileName string) (bool, error) {
	if info, err := os.Stat(projectPath); err != nil {
		if os.IsNotExist(err) {
			return true, nil
		}
		return false, err
	} else if !info.IsDir() {
		return false, errors.Errorf("project path is not a directory: %s", projectPath)
	}

	newFileSet := make(map[string]struct{}, len(syncFiles))
	for _, file := range syncFiles {
		newFileSet[file.RelativePath] = struct{}{}
		matches, err := syncedProjectFileMatchesInternal(projectPath, file)
		if err != nil {
			return false, err
		}
		if !matches {
			return true, nil
		}
	}

	for _, oldFile := range oldSyncedFiles {
		if _, exists := newFileSet[oldFile]; exists {
			continue
		}
		exists, err := pathExistsInternal(filepath.Join(projectPath, oldFile))
		if err != nil {
			return false, err
		}
		if exists {
			return true, nil
		}
	}

	for _, candidate := range ComposeFileCandidates() {
		if candidate == composeFileName {
			continue
		}
		if _, exists := newFileSet[candidate]; exists {
			continue
		}
		exists, err := pathExistsInternal(filepath.Join(projectPath, candidate))
		if err != nil {
			return false, err
		}
		if exists {
			return true, nil
		}
	}

	return false, nil
}

func RemoveStaleComposeFiles(projectPath, composeFileName string, syncedFiles []string) error {
	syncedFileSet := make(map[string]struct{}, len(syncedFiles))
	for _, file := range syncedFiles {
		syncedFileSet[file] = struct{}{}
	}

	for _, candidate := range ComposeFileCandidates() {
		if candidate == composeFileName {
			continue
		}
		if _, exists := syncedFileSet[candidate]; exists {
			continue
		}
		if err := os.Remove(filepath.Join(projectPath, candidate)); err != nil && !os.IsNotExist(err) {
			return err
		}
	}

	entries, err := os.ReadDir(projectPath)
	if err != nil {
		return err
	}

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}

		name := entry.Name()
		if name == composeFileName {
			continue
		}
		if _, exists := syncedFileSet[name]; exists {
			continue
		}
		if slices.Contains(ComposeFileCandidates(), name) || !IsProjectFile(name) {
			continue
		}

		path := filepath.Join(projectPath, name)
		hasComposeRootKeys, rootKeysErr := HasComposeRootKeysInFile(path)
		if rootKeysErr != nil || !hasComposeRootKeys {
			continue
		}

		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			return err
		}
	}

	return nil
}

// CopyDirectoryContentsTolerant copies srcDir into destDir like
// CopyDirectoryContents, except that files (or whole subdirectories) which
// cannot be read because of a permission error are skipped instead of aborting
// the copy. The skipped entries' project-relative paths are returned sorted so
// callers can avoid deleting them on a later restore. Any non-permission error
// still aborts the copy.
func CopyDirectoryContentsTolerant(srcDir, destDir string) (skipped []string, err error) {
	err = CopyDirectoryContents(srcDir, destDir, func(relPath string) {
		skipped = append(skipped, relPath)
	})
	slices.Sort(skipped)
	return skipped, err
}

// CopyDirectoryContents copies srcDir into destDir. When skipUnreadable
// is non-nil and a file or subdirectory cannot be read because of a permission
// error, the offending project-relative path is reported via skipUnreadable and
// the copy continues; otherwise the error aborts the copy.
func CopyDirectoryContents(srcDir, destDir string, skipUnreadable func(relPath string)) error {
	srcRoot, err := os.OpenRoot(srcDir)
	if err != nil {
		return err
	}
	defer func() { _ = srcRoot.Close() }()

	destRoot, err := os.OpenRoot(destDir)
	if err != nil {
		return err
	}
	defer func() { _ = destRoot.Close() }()

	return filepath.WalkDir(srcDir, func(path string, d os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return handleCopyWalkErrorInternal(srcDir, path, d, walkErr, skipUnreadable)
		}
		if path == srcDir {
			return nil
		}

		relPath, err := filepath.Rel(srcDir, path)
		if err != nil {
			return err
		}

		if d.IsDir() {
			return destRoot.MkdirAll(relPath, 0o755)
		}
		if d.Type()&os.ModeSymlink != 0 {
			return nil
		}
		if !d.Type().IsRegular() {
			// Sockets, FIFOs, and device nodes left behind by running containers
			// have no copyable content — reading one fails with ENXIO, not a
			// permission error (#3003). Record them like unreadable entries so a
			// later restore preserves rather than prunes them.
			if skipUnreadable != nil {
				skipUnreadable(relPath)
			}
			return nil
		}

		return copyRegularFileInternal(srcRoot, destRoot, relPath, d, skipUnreadable)
	})
}

func handleCopyWalkErrorInternal(srcDir, path string, d os.DirEntry, walkErr error, skipUnreadable func(relPath string)) error {
	// An unreadable subdirectory surfaces here as a permission error on the
	// directory entry itself; an entry deleted mid-walk surfaces as not-exist.
	if skipUnreadable == nil || (!errors.Is(walkErr, os.ErrPermission) && !errors.Is(walkErr, os.ErrNotExist)) {
		return walkErr
	}
	if relPath, relErr := filepath.Rel(srcDir, path); relErr == nil {
		skipUnreadable(relPath)
	}
	if d != nil && d.IsDir() {
		return filepath.SkipDir
	}
	return nil
}

func copyRegularFileInternal(srcRoot, destRoot *os.Root, relPath string, d os.DirEntry, skipUnreadable func(relPath string)) error {
	info, err := d.Info()
	if err != nil {
		return err
	}

	content, err := srcRoot.ReadFile(relPath)
	if err != nil {
		if skipUnreadable != nil && (errors.Is(err, os.ErrPermission) || errors.Is(err, os.ErrNotExist)) {
			skipUnreadable(relPath)
			return nil
		}
		return err
	}

	if err := destRoot.MkdirAll(filepath.Dir(relPath), 0o755); err != nil {
		return err
	}

	return destRoot.WriteFile(relPath, content, info.Mode())
}

// MirrorDirectoryContentsPreserving makes destDir match srcDir while updating
// files and directories in place, so existing inodes (and therefore container
// bind mounts into destDir) stay valid. Entries missing from srcDir or whose
// type differs are removed, then srcDir is copied over the result. It never
// prunes a destDir entry whose project-relative path is listed in preserve, nor
// any directory that still contains a preserved entry. It is used when
// restoring a backup that intentionally omits files the caller could not read:
// those files must survive the restore rather than be deleted.
func MirrorDirectoryContentsPreserving(srcDir, destDir string, preserve []string) error {
	preserveSet := make(map[string]struct{}, len(preserve))
	for _, p := range preserve {
		preserveSet[filepath.Clean(p)] = struct{}{}
	}
	if err := pruneDirectoryContentsInternal(srcDir, destDir, preserveSet); err != nil {
		return err
	}
	// Tolerate unreadable source entries during the copy-over: a mirror runs
	// while promoting or restoring a live project directory, and failing
	// half-way leaves it in a mixed state — worse than leaving a stale copy of
	// one unreadable file (#3509, #3085).
	return CopyDirectoryContents(srcDir, destDir, func(string) {})
}

func pruneDirectoryContentsInternal(srcDir, destDir string, preserve map[string]struct{}) error {
	srcRoot, err := os.OpenRoot(srcDir)
	if err != nil {
		return err
	}
	defer func() { _ = srcRoot.Close() }()

	destRoot, err := os.OpenRoot(destDir)
	if err != nil {
		return err
	}
	defer func() { _ = destRoot.Close() }()

	return filepath.WalkDir(destDir, func(path string, d os.DirEntry, walkErr error) error {
		if walkErr != nil {
			// An unreadable destination subdirectory (e.g. foreign-owned
			// bind-mount data) cannot be pruned; leave it in place instead of
			// failing the whole mirror mid-restore (#3509, #3085). Entries that
			// vanished mid-walk need no pruning either.
			if errors.Is(walkErr, os.ErrPermission) || errors.Is(walkErr, os.ErrNotExist) {
				return keepUnprunableEntryInternal(d)
			}
			return walkErr
		}
		if path == destDir {
			return nil
		}

		relPath, err := filepath.Rel(destDir, path)
		if err != nil {
			return err
		}

		// Never delete a preserved entry.
		if _, ok := preserve[relPath]; ok {
			return keepUnprunableEntryInternal(d)
		}

		srcInfo, err := srcRoot.Lstat(relPath)
		if err == nil && srcInfo.Mode()&os.ModeType == d.Type() {
			return nil
		}
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			// Cannot inspect the source counterpart — keep the destination
			// entry rather than deleting something the source may still have.
			if errors.Is(err, os.ErrPermission) {
				return keepUnprunableEntryInternal(d)
			}
			return err
		}

		// This entry is absent from (or type-changed vs) the source, so it would
		// normally be pruned. If it is a directory that still contains a preserved
		// entry, descend and prune its other children instead of removing it whole.
		if d.IsDir() && hasPreservedDescendantInternal(relPath, preserve) {
			return nil
		}

		if err := destRoot.RemoveAll(relPath); err != nil {
			return err
		}
		if d.IsDir() {
			return filepath.SkipDir
		}
		return nil
	})
}

// keepUnprunableEntryInternal continues a prune walk past an entry that must
// stay: descend no further into directories, ignore files.
func keepUnprunableEntryInternal(d os.DirEntry) error {
	if d != nil && d.IsDir() {
		return filepath.SkipDir
	}
	return nil
}

// hasPreservedDescendantInternal reports whether any preserved path lives under
// dir, so the prune knows to descend into dir rather than remove it wholesale.
func hasPreservedDescendantInternal(dir string, preserve map[string]struct{}) bool {
	prefix := dir + string(filepath.Separator)
	for p := range preserve {
		if strings.HasPrefix(p, prefix) {
			return true
		}
	}
	return false
}

// CreateUniqueDir creates a unique directory within the allowed projectsRoot.
// It validates that the created directory is always within projectsRoot.
func CreateUniqueDir(projectsRoot, basePath, name string, perm os.FileMode) (path, folderName string, err error) {
	sanitized := SanitizeProjectName(name)

	// Reject empty or invalid sanitized names
	if sanitized == "" || strings.Trim(sanitized, "_") == "" {
		return "", "", errors.New("invalid project name: results in empty directory name")
	}

	// Get absolute path of the true projects root for validation
	projectsRootAbs, err := filepath.Abs(projectsRoot)
	if err != nil {
		return "", "", errors.WrapIf(err, "failed to resolve projects root directory")
	}
	projectsRootAbs = filepath.Clean(projectsRootAbs)

	candidate := basePath
	folderName = sanitized

	for counter := 1; ; counter++ {
		// Validate candidate is within the allowed projects root
		candidateAbs, absErr := filepath.Abs(candidate)
		if absErr != nil {
			return "", "", errors.WrapIf(absErr, "failed to resolve candidate path")
		}
		candidateAbs = filepath.Clean(candidateAbs)

		// Security check: ensure candidate is a subdirectory of projectsRoot
		if !IsSafeSubdirectory(projectsRootAbs, candidateAbs) {
			return "", "", errors.New("project directory would be outside allowed projects root")
		}

		if mkErr := os.Mkdir(candidate, perm); mkErr == nil {
			// Double-check after creation - paranoid validation
			if !IsSafeSubdirectory(projectsRootAbs, candidateAbs) {
				// Security violation detected - remove the unsafe directory
				// We only reach here if somehow a directory was created outside the root
				// despite pre-checks. Clean up by removing ONLY if it's actually within root.
				if strings.HasPrefix(candidateAbs, projectsRootAbs+string(filepath.Separator)) {
					_ = os.Remove(candidateAbs)
				}
				return "", "", errors.New("created directory is outside allowed projects root")
			}

			return candidate, folderName, nil
		} else if !os.IsExist(mkErr) {
			return "", "", mkErr
		}
		candidate = fmt.Sprintf("%s-%d", basePath, counter)
		folderName = fmt.Sprintf("%s-%d", sanitized, counter)
	}
}

// ErrProjectDirExists is returned by CreateExactDir when the target directory
// already exists. Callers that must not auto-rename (e.g. GitOps creates, which
// must never mint "-N" duplicate projects on a broken binding) use this to fail
// loudly instead of suffixing.
const ErrProjectDirExists = errors.Sentinel("project directory already exists")

// CreateExactDir creates basePath (the sanitized project directory) under
// projectsRoot WITHOUT any "-N" collision suffixing. It returns ErrProjectDirExists
// when the directory already exists, leaving the caller to decide how to proceed.
// Validation mirrors CreateUniqueDir so the created directory is always within
// projectsRoot.
func CreateExactDir(projectsRoot, basePath, name string, perm os.FileMode) (path, folderName string, err error) {
	sanitized := SanitizeProjectName(name)
	if sanitized == "" || strings.Trim(sanitized, "_") == "" {
		return "", "", errors.New("invalid project name: results in empty directory name")
	}

	projectsRootAbs, err := filepath.Abs(projectsRoot)
	if err != nil {
		return "", "", errors.WrapIf(err, "failed to resolve projects root directory")
	}
	projectsRootAbs = filepath.Clean(projectsRootAbs)

	candidateAbs, err := filepath.Abs(basePath)
	if err != nil {
		return "", "", errors.WrapIf(err, "failed to resolve candidate path")
	}
	candidateAbs = filepath.Clean(candidateAbs)

	if !IsSafeSubdirectory(projectsRootAbs, candidateAbs) {
		return "", "", errors.New("project directory would be outside allowed projects root")
	}

	if mkErr := os.Mkdir(basePath, perm); mkErr != nil {
		if os.IsExist(mkErr) {
			return "", "", ErrProjectDirExists
		}
		return "", "", mkErr
	}

	// Paranoid post-create validation, matching CreateUniqueDir.
	if !IsSafeSubdirectory(projectsRootAbs, candidateAbs) {
		if strings.HasPrefix(candidateAbs, projectsRootAbs+string(filepath.Separator)) {
			_ = os.Remove(candidateAbs)
		}
		return "", "", errors.New("created directory is outside allowed projects root")
	}

	return basePath, sanitized, nil
}

func SanitizeProjectName(name string) string {
	name = strings.TrimSpace(name)
	return strings.Map(func(r rune) rune {
		if (r >= 'a' && r <= 'z') ||
			(r >= 'A' && r <= 'Z') ||
			(r >= '0' && r <= '9') ||
			r == '-' || r == '_' {
			return r
		}
		return '_'
	}, name)
}

// IsSafeSubdirectory returns true if subdir is a subdirectory of baseDir (absolute, normalized)
func IsSafeSubdirectory(baseDir, subdir string) bool {
	absBase, err1 := filepath.Abs(baseDir)
	absSubdir, err2 := filepath.Abs(subdir)
	if err1 != nil || err2 != nil {
		return false
	}

	// Ensure both paths end consistently for comparison
	absBase = filepath.Clean(absBase)
	absSubdir = filepath.Clean(absSubdir)

	rel, err := filepath.Rel(absBase, absSubdir)
	if err != nil {
		return false
	}

	// The path must not escape the base directory
	return !strings.HasPrefix(rel, "..") && !filepath.IsAbs(rel)
}

// ResolvePathWithinDir resolves path and rejects paths that escape baseDir.
func ResolvePathWithinDir(baseDir, path string) (string, error) {
	resolvedBase, err := filepath.Abs(filepath.Clean(baseDir))
	if err != nil {
		return "", errors.WrapIff(err, "failed to resolve base directory %q", baseDir)
	}

	resolvedPath := path
	if !filepath.IsAbs(resolvedPath) {
		resolvedPath = filepath.Join(resolvedBase, resolvedPath)
	}
	resolvedPath, err = filepath.Abs(filepath.Clean(resolvedPath))
	if err != nil {
		return "", errors.WrapIff(err, "failed to resolve path %q", path)
	}
	if !IsSafeSubdirectory(resolvedBase, resolvedPath) {
		return "", errors.Errorf("path %q escapes directory %q", path, baseDir)
	}

	return resolvedPath, nil
}
