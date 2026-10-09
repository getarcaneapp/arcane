package projects

import (
	"bytes"
	"cmp"
	"context"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/samber/mo"
	"go.getarcane.app/acfs"
	kit "go.getarcane.app/kit/pkg"
	"go.yaml.in/yaml/v4"

	"github.com/getarcaneapp/arcane/backend/v2/internal/common"
	"github.com/getarcaneapp/arcane/backend/v2/internal/config"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/utils"
)

func ResolveConfiguredContainerDirectory(configuredPath, defaultPath string) string {
	directory := cmp.Or(strings.TrimSpace(configuredPath), defaultPath)

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

	// os.* rather than acfs: this creates the confinement root itself, which
	// has to exist before acfs can open it.
	if _, err := os.Stat(projectsDirectory); os.IsNotExist(err) {
		if mkdirAllErr := os.MkdirAll(projectsDirectory, utils.DirPerm); mkdirAllErr != nil {
			return "", mkdirAllErr
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

// ReadProjectFiles stays on os.*: compose and env files may be symlinks
// resolving outside any confinement root, and projectPath can be an imported
// project outside the projects directory; acfs cannot follow either.
func ReadProjectFiles(ctx context.Context, projectPath, composePath string) (composeContent, envContent string, err error) {
	if strings.TrimSpace(composePath) == "" {
		detected, derr := DetectComposeFile(ctx, "", projectPath)
		if derr != nil && !errors.Is(derr, common.ErrComposeFileNotFound) && (!errors.Is(derr, common.ErrProjectEnvUnreadable) || detected == "") {
			return "", "", derr
		}
		composePath = detected
	}

	if strings.TrimSpace(composePath) != "" {
		content, rerr := os.ReadFile(composePath)
		if rerr != nil {
			return "", "", fmt.Errorf("failed to read compose file %s: %w", composePath, rerr)
		}
		composeContent = string(content)
	}

	envPath := filepath.Join(projectPath, ".env")
	if content, rerr := os.ReadFile(envPath); rerr == nil {
		envContent = string(content)
	}

	return composeContent, envContent, nil
}

func HasComposeRootKeysInFile(path string) (bool, error) {
	// Stays on os.*: callers pass paths in imported projects outside the
	// projects directory, where no acfs root exists.
	content, err := os.ReadFile(path)
	if err != nil {
		return false, err
	}

	composeData := map[string]any{}
	if unmarshalErr := yaml.Unmarshal(content, &composeData); unmarshalErr != nil {
		return false, unmarshalErr
	}

	_, hasServices := composeData["services"]
	_, hasInclude := composeData["include"]
	return hasServices || hasInclude, nil
}

func GetTemplatesDirectory(ctx context.Context, templatesDir string) (string, error) {
	resolved := ResolveConfiguredContainerDirectory(templatesDir, "/app/data/templates")
	// os.* rather than acfs: this creates the confinement root itself, which
	// has to exist before acfs can open it.
	if _, err := os.Stat(resolved); os.IsNotExist(err) {
		if mkdirAllErr := os.MkdirAll(resolved, utils.DirPerm); mkdirAllErr != nil {
			return "", mkdirAllErr
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

func syncedProjectFileMatchesInternal(ctx context.Context, projectPath string, file SyncFile) (bool, error) {
	logicalPath := "/" + filepath.ToSlash(file.RelativePath)
	entry, err := acfs.Stat(ctx, projectPath, logicalPath, false)
	if err != nil {
		return false, kit.Ternary(errors.Is(err, fs.ErrNotExist), nil, err)
	}
	if entry.IsDirectory {
		return false, nil
	}

	existingContent, err := acfs.ReadFile(ctx, projectPath, logicalPath)
	if err != nil {
		return false, kit.Ternary(errors.Is(err, fs.ErrNotExist), nil, err)
	}

	return bytes.Equal(existingContent, file.Content), nil
}

// pathExistsInternal stays on os.*: it probes arbitrary absolute paths,
// including imported projects outside any acfs root.
func pathExistsInternal(path string) (bool, error) {
	_, err := os.Stat(path)
	if err == nil {
		return true, nil
	}
	return false, kit.Ternary(os.IsNotExist(err), nil, err)
}

func DirectorySyncContentsChanged(ctx context.Context, projectPath string, syncFiles []SyncFile, oldSyncedFiles []string, composeFileName string) (bool, error) {
	// os.Stat rather than acfs: projectPath is the would-be confinement root
	// itself, which acfs cannot probe before it exists.
	if info, err := os.Stat(projectPath); err != nil {
		if os.IsNotExist(err) {
			return true, nil
		}
		return false, err
	} else if !info.IsDir() {
		return false, fmt.Errorf("project path is not a directory: %s", projectPath)
	}

	newFileSet := make(map[string]struct{}, len(syncFiles))
	for _, file := range syncFiles {
		newFileSet[file.RelativePath] = struct{}{}
		matches, err := syncedProjectFileMatchesInternal(ctx, projectPath, file)
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
		exists, err := acfs.Exists(ctx, projectPath, "/"+filepath.ToSlash(oldFile))
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
		exists, err := acfs.Exists(ctx, projectPath, "/"+candidate)
		if err != nil {
			return false, err
		}
		if exists {
			return true, nil
		}
	}

	return false, nil
}

// StaleComposeFiles lists project-root compose files a directory sync no longer
// manages: other standard compose names, plus custom YAML files with compose
// root keys that are not part of the synced set. A missing project directory
// has nothing stale.
func StaleComposeFiles(ctx context.Context, projectPath, composeFileName string, syncedFiles []string) ([]string, error) {
	syncedFileSet := make(map[string]struct{}, len(syncedFiles))
	for _, file := range syncedFiles {
		syncedFileSet[file] = struct{}{}
	}

	entries, err := acfs.List(ctx, projectPath, "/")
	if err != nil {
		return nil, kit.Ternary(errors.Is(err, fs.ErrNotExist), nil, err)
	}

	var stale []string
	for _, entry := range entries {
		name := entry.Name
		if entry.IsDirectory || name == composeFileName {
			continue
		}
		if _, exists := syncedFileSet[name]; exists {
			continue
		}
		if slices.Contains(ComposeFileCandidates(), name) {
			stale = append(stale, name)
			continue
		}
		if !IsProjectFile(name) {
			continue
		}

		hasComposeRootKeys, rootKeysErr := HasComposeRootKeysInFile(filepath.Join(projectPath, name))
		if rootKeysErr != nil || !hasComposeRootKeys {
			continue
		}
		stale = append(stale, name)
	}

	return stale, nil
}

func RemoveStaleComposeFiles(ctx context.Context, projectPath, composeFileName string, syncedFiles []string) error {
	stale, err := StaleComposeFiles(ctx, projectPath, composeFileName, syncedFiles)
	if err != nil {
		return err
	}

	for _, name := range stale {
		if removeErr := acfs.Remove(ctx, projectPath, "/"+name); removeErr != nil && !errors.Is(removeErr, fs.ErrNotExist) {
			return removeErr
		}
	}

	return nil
}

// CreateUniqueDir creates a unique directory within the allowed projectsRoot,
// suffixing "-N" until an unused name is found. An existing basePath is reused
// instead when it is a real directory that claimed reports as unowned and in
// which Compose detection finds no file (an ambiguous result counts as one);
// reused reports that case.
func CreateUniqueDir(ctx context.Context, projectsRoot, basePath, name string, perm os.FileMode, claimed func(dir string) (bool, error)) (path, folderName string, reused bool, err error) {
	sanitized := SanitizeProjectName(name)

	// Reject empty or invalid sanitized names
	if sanitized == "" || strings.Trim(sanitized, "_") == "" {
		return "", "", false, errors.New("invalid project name: results in empty directory name")
	}

	candidate := basePath
	folderName = sanitized

	for counter := 1; ; counter++ {
		logicalPath, logicalErr := acfs.LogicalPath(projectsRoot, candidate)
		if logicalErr != nil {
			return "", "", false, fmt.Errorf("project directory would be outside allowed projects root: %w", logicalErr)
		}

		mkErr := acfs.Mkdir(ctx, projectsRoot, logicalPath, perm)
		if mkErr == nil {
			return candidate, folderName, false, nil
		}
		if !errors.Is(mkErr, os.ErrExist) {
			return "", "", false, mkErr
		}

		if candidate == basePath {
			info, statErr := os.Lstat(basePath)
			if statErr != nil {
				return "", "", false, fmt.Errorf("inspect existing project directory: %w", statErr)
			}
			isClaimed, claimedErr := claimed(basePath)
			if claimedErr != nil {
				return "", "", false, fmt.Errorf("check existing project directory claims: %w", claimedErr)
			}
			if info.IsDir() && !isClaimed {
				if composePath, detectErr := DetectComposeFile(ctx, projectsRoot, basePath); composePath == "" && errors.Is(detectErr, common.ErrComposeFileNotFound) {
					return basePath, folderName, true, nil
				}
			}
		}

		candidate = fmt.Sprintf("%s-%d", basePath, counter)
		folderName = fmt.Sprintf("%s-%d", sanitized, counter)
	}
}

// ErrProjectDirExists is returned by CreateExactDir when the target directory
// already exists. Callers that must not auto-rename (e.g. GitOps creates, which
// must never mint "-N" duplicate projects on a broken binding) use this to fail
// loudly instead of suffixing.
var ErrProjectDirExists = errors.New("project directory already exists")

// CreateExactDir creates basePath (the sanitized project directory) under
// projectsRoot WITHOUT any "-N" collision suffixing. It returns ErrProjectDirExists
// when the directory already exists, leaving the caller to decide how to proceed.
func CreateExactDir(ctx context.Context, projectsRoot, basePath, name string, perm os.FileMode) (path, folderName string, err error) {
	sanitized := SanitizeProjectName(name)
	if sanitized == "" || strings.Trim(sanitized, "_") == "" {
		return "", "", errors.New("invalid project name: results in empty directory name")
	}

	logicalPath, err := acfs.LogicalPath(projectsRoot, basePath)
	if err != nil {
		return "", "", fmt.Errorf("project directory would be outside allowed projects root: %w", err)
	}

	if mkdirErr := acfs.Mkdir(ctx, projectsRoot, logicalPath, perm); mkdirErr != nil {
		return "", "", kit.Ternary[error](errors.Is(mkdirErr, os.ErrExist), ErrProjectDirExists, mkdirErr)
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
		return "", fmt.Errorf("failed to resolve base directory %q: %w", baseDir, err)
	}

	resolvedPath := path
	if !filepath.IsAbs(resolvedPath) {
		resolvedPath = filepath.Join(resolvedBase, resolvedPath)
	}
	resolvedPath, err = filepath.Abs(filepath.Clean(resolvedPath))
	if err != nil {
		return "", fmt.Errorf("failed to resolve path %q: %w", path, err)
	}
	if !IsSafeSubdirectory(resolvedBase, resolvedPath) {
		return "", fmt.Errorf("path %q escapes directory %q", path, baseDir)
	}

	return resolvedPath, nil
}
