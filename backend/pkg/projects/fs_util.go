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
	"go.yaml.in/yaml/v4"

	"kit.getarcane.app"
	"kit.getarcane.app/acfs"
	acfstypes "kit.getarcane.app/acfs/types"
	kitfs "kit.getarcane.app/fs"

	"getarcane.dev/arcane/v3/internal/common"
)

var ErrProjectDirExists = errors.New("project directory already exists")

func ResolveConfiguredContainerDirectory(ctx context.Context, configuredPath, defaultPath string) string {
	directory := cmp.Or(strings.TrimSpace(configuredPath), defaultPath)

	// Handle mapping format: "container_path:host_path"
	if parts := strings.SplitN(directory, ":", 2); len(parts) == 2 {
		if !IsWindowsDrivePath(directory) && strings.HasPrefix(parts[0], "/") {
			directory = parts[0]
		}
	}

	return resolveProjectsDirectoryPath(ctx, directory)
}

func GetProjectsDirectory(ctx context.Context, projectsDir string) (string, error) {
	projectsDirectory := ResolveConfiguredContainerDirectory(ctx, projectsDir, "/app/data/projects")

	if _, statErr := (acfs.Host{}).Stat(ctx, projectsDirectory, acfstypes.StatOptions{FollowFinal: true}); errors.Is(statErr, fs.ErrNotExist) {
		if mkdirAllErr := (acfs.Host{}).MkdirAll(ctx, projectsDirectory, kitfs.DirPerm); mkdirAllErr != nil {
			return "", mkdirAllErr
		}
		slog.InfoContext(ctx, "Created projects directory", "path", projectsDirectory)
	}

	return projectsDirectory, nil
}

func resolveProjectsDirectoryPath(ctx context.Context, projectsDirectory string) string {
	if filepath.IsAbs(projectsDirectory) {
		return filepath.Clean(projectsDirectory)
	}

	if backendRoot, ok := findBackendModuleRoot(ctx).Get(); ok {
		return filepath.Clean(filepath.Join(backendRoot, projectsDirectory))
	}

	absDir, err := filepath.Abs(projectsDirectory)
	if err == nil {
		return filepath.Clean(absDir)
	}

	return filepath.Clean(projectsDirectory)
}

func findBackendModuleRoot(ctx context.Context) mo.Option[string] {
	cwd, err := os.Getwd()
	if err != nil {
		return mo.None[string]()
	}

	candidates := []string{
		cwd,
		filepath.Join(cwd, "backend"),
	}

	for _, candidate := range candidates {
		if isBackendModuleRoot(ctx, candidate) {
			return mo.Some(candidate)
		}
	}

	return mo.None[string]()
}

func isBackendModuleRoot(ctx context.Context, path string) bool {
	for _, name := range []string{"go.mod", "internal", "pkg"} {
		if _, err := (acfs.Host{}).Stat(ctx, filepath.Join(path, name), acfstypes.StatOptions{FollowFinal: true}); err != nil {
			return false
		}
	}
	return true
}

func ReadProjectFiles(ctx context.Context, projectPath, composePath string) (composeContent, envContent string, err error) {
	if strings.TrimSpace(composePath) == "" {
		detected, derr := DetectComposeFile(ctx, "", projectPath)
		if derr != nil && !errors.Is(derr, common.ErrComposeFileNotFound) && (!errors.Is(derr, common.ErrProjectEnvUnreadable) || detected == "") {
			return "", "", derr
		}
		composePath = detected
	}

	if strings.TrimSpace(composePath) != "" {
		content, rerr := acfs.Host{}.ReadFile(ctx, composePath)
		if rerr != nil {
			return "", "", fmt.Errorf("failed to read compose file %s: %w", composePath, rerr)
		}
		composeContent = string(content)
	}

	if content, rerr := (acfs.Host{}).ReadFile(ctx, filepath.Join(projectPath, ".env")); rerr == nil {
		envContent = string(content)
	}

	return composeContent, envContent, nil
}

func HasComposeRootKeysInFile(ctx context.Context, path string) (bool, error) {
	content, err := acfs.Host{}.ReadFile(ctx, path)
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
	resolved := ResolveConfiguredContainerDirectory(ctx, templatesDir, "/app/data/templates")
	if _, statErr := (acfs.Host{}).Stat(ctx, resolved, acfstypes.StatOptions{FollowFinal: true}); errors.Is(statErr, fs.ErrNotExist) {
		if mkdirAllErr := (acfs.Host{}).MkdirAll(ctx, resolved, kitfs.DirPerm); mkdirAllErr != nil {
			return "", mkdirAllErr
		}
		slog.InfoContext(ctx, "Created templates directory", "path", resolved)
	}
	return resolved, nil
}

func syncedProjectFileMatches(ctx context.Context, projectPath string, file SyncFile) (bool, error) {
	logicalPath := "/" + filepath.ToSlash(file.RelativePath)
	entry, err := acfs.Root{Path: projectPath}.Stat(ctx, logicalPath, acfstypes.StatOptions{})
	if err != nil {
		return false, kit.Ternary(errors.Is(err, fs.ErrNotExist), nil, err)
	}
	if entry.IsDirectory {
		return false, nil
	}

	existingContent, err := acfs.Root{Path: projectPath}.ReadFile(ctx, logicalPath)
	if err != nil {
		return false, kit.Ternary(errors.Is(err, fs.ErrNotExist), nil, err)
	}

	return bytes.Equal(existingContent, file.Content), nil
}

func DirectorySyncContentsChanged(ctx context.Context, projectPath string, syncFiles []SyncFile, oldSyncedFiles []string, composeFileName string) (bool, error) {
	if entry, err := (acfs.Host{}).Stat(ctx, projectPath, acfstypes.StatOptions{FollowFinal: true}); err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return true, nil
		}
		return false, err
	} else if !entry.IsDirectory {
		return false, fmt.Errorf("project path is not a directory: %s", projectPath)
	}

	newFileSet := make(map[string]struct{}, len(syncFiles))
	for _, file := range syncFiles {
		newFileSet[file.RelativePath] = struct{}{}
		matches, err := syncedProjectFileMatches(ctx, projectPath, file)
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
		exists, err := acfs.Root{Path: projectPath}.Exists(ctx, "/"+filepath.ToSlash(oldFile))
		if err != nil {
			return false, err
		}
		if exists {
			return true, nil
		}
	}

	for _, candidate := range ComposeFileCandidates {
		if candidate == composeFileName {
			continue
		}
		if _, exists := newFileSet[candidate]; exists {
			continue
		}
		exists, err := acfs.Root{Path: projectPath}.Exists(ctx, "/"+candidate)
		if err != nil {
			return false, err
		}
		if exists {
			return true, nil
		}
	}

	return false, nil
}

func StaleComposeFiles(ctx context.Context, projectPath, composeFileName string, syncedFiles []string) ([]string, error) {
	syncedFileSet := make(map[string]struct{}, len(syncedFiles))
	for _, file := range syncedFiles {
		syncedFileSet[file] = struct{}{}
	}

	entries, err := acfs.Root{Path: projectPath}.List(ctx, "/")
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
		if slices.Contains(ComposeFileCandidates, name) {
			stale = append(stale, name)
			continue
		}
		if !IsProjectFile(name) {
			continue
		}

		hasComposeRootKeys, rootKeysErr := HasComposeRootKeysInFile(ctx, filepath.Join(projectPath, name))
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
		if removeErr := (acfs.Root{Path: projectPath}).Remove(ctx, "/"+name); removeErr != nil && !errors.Is(removeErr, fs.ErrNotExist) {
			return removeErr
		}
	}

	return nil
}

func CreateUniqueDir(ctx context.Context, projectsRoot, basePath, name string, perm os.FileMode, claimed func(dir string) (bool, error)) (path, folderName string, reused bool, err error) {
	sanitized := SanitizeProjectName(name)

	if sanitized == "" || strings.Trim(sanitized, "_") == "" {
		return "", "", false, errors.New("invalid project name: results in empty directory name")
	}

	candidate := basePath
	folderName = sanitized

	for counter := 1; ; counter++ {
		logicalPath, logicalErr := acfs.Root{Path: projectsRoot}.LogicalPath(candidate)
		if logicalErr != nil {
			return "", "", false, fmt.Errorf("project directory would be outside allowed projects root: %w", logicalErr)
		}

		mkErr := acfs.Root{Path: projectsRoot}.Mkdir(ctx, logicalPath, perm)
		if mkErr == nil {
			return candidate, folderName, false, nil
		}
		if !errors.Is(mkErr, os.ErrExist) {
			return "", "", false, projectDirError(projectsRoot, candidate, mkErr)
		}

		if candidate == basePath {
			entry, statErr := acfs.Root{Path: projectsRoot}.Stat(ctx, logicalPath, acfstypes.StatOptions{})
			if statErr != nil {
				return "", "", false, fmt.Errorf("inspect existing project directory: %w", statErr)
			}
			isClaimed, claimedErr := claimed(basePath)
			if claimedErr != nil {
				return "", "", false, fmt.Errorf("check existing project directory claims: %w", claimedErr)
			}
			if entry.IsDirectory && !isClaimed {
				if composePath, detectErr := DetectComposeFile(ctx, projectsRoot, basePath); composePath == "" && errors.Is(detectErr, common.ErrComposeFileNotFound) {
					return basePath, folderName, true, nil
				}
			}
		}

		candidate = fmt.Sprintf("%s-%d", basePath, counter)
		folderName = fmt.Sprintf("%s-%d", sanitized, counter)
	}
}

func CreateExactDir(ctx context.Context, projectsRoot, basePath, name string, perm os.FileMode) (path, folderName string, err error) {
	sanitized := SanitizeProjectName(name)
	if sanitized == "" || strings.Trim(sanitized, "_") == "" {
		return "", "", errors.New("invalid project name: results in empty directory name")
	}

	logicalPath, err := acfs.Root{Path: projectsRoot}.LogicalPath(basePath)
	if err != nil {
		return "", "", fmt.Errorf("project directory would be outside allowed projects root: %w", err)
	}

	if mkdirErr := (acfs.Root{Path: projectsRoot}).Mkdir(ctx, logicalPath, perm); mkdirErr != nil {
		return "", "", kit.Ternary[error](errors.Is(mkdirErr, os.ErrExist), ErrProjectDirExists, projectDirError(projectsRoot, basePath, mkdirErr))
	}

	return basePath, sanitized, nil
}

func projectDirError(projectsRoot, path string, err error) error {
	wrapped := fmt.Errorf("%s: %w", path, err)
	if errors.Is(err, os.ErrPermission) {
		return common.Classify(common.ErrForbidden, fmt.Errorf("%w (projects directory %s must be writable by uid %d)", wrapped, projectsRoot, os.Getuid()))
	}
	return wrapped
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

func IsSafeSubdirectory(baseDir, subdir string) bool {
	absBase, err1 := filepath.Abs(baseDir)
	absSubdir, err2 := filepath.Abs(subdir)
	if err1 != nil || err2 != nil {
		return false
	}
	rel, err := filepath.Rel(absBase, absSubdir)
	return err == nil && filepath.IsLocal(rel)
}

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
