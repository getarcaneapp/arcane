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

	"github.com/getarcaneapp/arcane/backend/internal/config"
	pkgutils "github.com/getarcaneapp/arcane/backend/pkg/utils"
	"github.com/getarcaneapp/arcane/types/project"
	"github.com/goccy/go-yaml"
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

	if backendRoot, ok := findBackendModuleRoot(); ok {
		return filepath.Clean(filepath.Join(backendRoot, projectsDirectory))
	}

	absDir, err := filepath.Abs(projectsDirectory)
	if err == nil {
		return filepath.Clean(absDir)
	}

	return filepath.Clean(projectsDirectory)
}

func findBackendModuleRoot() (string, bool) {
	cwd, err := os.Getwd()
	if err != nil {
		return "", false
	}

	candidates := []string{
		cwd,
		filepath.Join(cwd, "backend"),
	}

	for _, candidate := range candidates {
		if isBackendModuleRoot(candidate) {
			return candidate, true
		}
	}

	return "", false
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

func GetTemplatesDirectory(ctx context.Context) (string, error) {
	templatesDir := filepath.Join("data", "templates")
	if _, err := os.Stat(templatesDir); os.IsNotExist(err) {
		if err := os.MkdirAll(templatesDir, pkgutils.DirPerm); err != nil {
			return "", err
		}
		slog.InfoContext(ctx, "Created templates directory", "path", templatesDir)
	}
	return templatesDir, nil
}

func ReadProjectDirectoryFiles(projectPath string, shownFiles map[string]bool, maxDepth int, skipDirectories string) ([]project.IncludeFile, error) {
	return readProjectDirectoryFilesInternal(projectPath, shownFiles, maxDepth, skipDirectories, false)
}

func readProjectDirectoryFilesInternal(projectPath string, shownFiles map[string]bool, maxDepth int, skipDirectories string, includeContent bool) ([]project.IncludeFile, error) {
	if maxDepth <= 0 {
		maxDepth = config.Load().ProjectScanMaxDepth
	}

	var dirFiles []project.IncludeFile

	root, err := os.OpenRoot(projectPath)
	if err != nil {
		return dirFiles, err
	}
	defer func() { _ = root.Close() }()

	err = collectProjectDirectoryFilesInternal(root, ".", projectPath, shownFiles, &dirFiles, 0, maxDepth, projectScanSkipDirectorySetInternal(skipDirectories), includeContent)

	return dirFiles, err
}

func projectScanSkipDirectorySetInternal(skipDirectories string) map[string]bool {
	if strings.TrimSpace(skipDirectories) == "" {
		skipDirectories = config.Load().ProjectScanSkipDirs
	}

	dirs := map[string]bool{}
	for _, dir := range strings.Split(skipDirectories, ",") {
		dir = strings.TrimSpace(dir)
		if dir != "" {
			dirs[dir] = true
		}
	}

	// Never allow .git contents to be exposed through the project file browser.
	dirs[".git"] = true

	return dirs
}

func collectProjectDirectoryFilesInternal(
	root *os.Root,
	relDir string,
	projectPath string,
	shownFiles map[string]bool,
	dirFiles *[]project.IncludeFile,
	currentDepth int,
	maxDepth int,
	skipDirs map[string]bool,
	includeContent bool,
) error {
	if currentDepth >= maxDepth {
		return nil
	}

	dir, err := root.Open(relDir)
	if err != nil {
		return err
	}
	defer func() { _ = dir.Close() }()

	entries, err := dir.ReadDir(-1)
	if err != nil {
		return err
	}

	for _, entry := range entries {
		relPath := entry.Name()
		if relDir != "." {
			relPath = filepath.Join(relDir, entry.Name())
		}
		if entry.Type()&os.ModeSymlink != 0 {
			continue
		}
		if entry.IsDir() {
			if skipDirs[entry.Name()] {
				continue
			}
			if err := collectProjectDirectoryFilesInternal(root, relPath, projectPath, shownFiles, dirFiles, currentDepth+1, maxDepth, skipDirs, includeContent); err != nil {
				slog.Debug("Skipping unreadable project subdirectory", "relativePath", relPath, "error", err)
			}
			continue
		}
		if shownFiles[relPath] {
			continue
		}

		info, err := entry.Info()
		if err != nil || info.Size() > 1024*1024 {
			continue
		}

		file := project.IncludeFile{
			Path:         filepath.Join(projectPath, relPath),
			RelativePath: relPath,
		}

		if includeContent {
			content, err := root.ReadFile(relPath)
			if err != nil || IsBinaryProjectFileContent(content) {
				continue
			}
			file.Content = string(content)
		}

		*dirFiles = append(*dirFiles, file)
	}

	return nil
}

func IsBinaryProjectFileContent(content []byte) bool {
	checkSize := min(len(content), 512)
	return slices.Contains(content[:checkSize], 0)
}

func DirectorySyncContentsChanged(projectPath string, syncFiles []SyncFile, oldSyncedFiles []string, composeFileName string) (bool, error) {
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
		existingContent, err := os.ReadFile(filepath.Join(projectPath, file.RelativePath))
		if err != nil {
			if os.IsNotExist(err) {
				return true, nil
			}
			return false, err
		}
		if !bytes.Equal(existingContent, file.Content) {
			return true, nil
		}
	}

	for _, oldFile := range oldSyncedFiles {
		if _, exists := newFileSet[oldFile]; exists {
			continue
		}
		if _, err := os.Stat(filepath.Join(projectPath, oldFile)); err == nil {
			return true, nil
		} else if !os.IsNotExist(err) {
			return false, err
		}
	}

	for _, candidate := range ComposeFileCandidates() {
		if candidate == composeFileName {
			continue
		}
		if _, exists := newFileSet[candidate]; exists {
			continue
		}
		if _, err := os.Stat(filepath.Join(projectPath, candidate)); err == nil {
			return true, nil
		} else if !os.IsNotExist(err) {
			return false, err
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

func CopyDirectoryContents(srcDir, destDir string) error {
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
			return walkErr
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

		info, err := d.Info()
		if err != nil {
			return err
		}

		content, err := srcRoot.ReadFile(relPath)
		if err != nil {
			return err
		}

		if err := destRoot.MkdirAll(filepath.Dir(relPath), 0o755); err != nil {
			return err
		}

		return destRoot.WriteFile(relPath, content, info.Mode())
	})
}

// CreateUniqueDir creates a unique directory within the allowed projectsRoot.
// It validates that the created directory is always within projectsRoot.
func CreateUniqueDir(projectsRoot, basePath, name string, perm os.FileMode) (path, folderName string, err error) {
	sanitized := SanitizeProjectName(name)

	// Reject empty or invalid sanitized names
	if sanitized == "" || strings.Trim(sanitized, "_") == "" {
		return "", "", fmt.Errorf("invalid project name: results in empty directory name")
	}

	// Get absolute path of the true projects root for validation
	projectsRootAbs, err := filepath.Abs(projectsRoot)
	if err != nil {
		return "", "", fmt.Errorf("failed to resolve projects root directory: %w", err)
	}
	projectsRootAbs = filepath.Clean(projectsRootAbs)

	candidate := basePath
	folderName = sanitized

	for counter := 1; ; counter++ {
		// Validate candidate is within the allowed projects root
		candidateAbs, absErr := filepath.Abs(candidate)
		if absErr != nil {
			return "", "", fmt.Errorf("failed to resolve candidate path: %w", absErr)
		}
		candidateAbs = filepath.Clean(candidateAbs)

		// Security check: ensure candidate is a subdirectory of projectsRoot
		if !IsSafeSubdirectory(projectsRootAbs, candidateAbs) {
			return "", "", fmt.Errorf("project directory would be outside allowed projects root")
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
				return "", "", fmt.Errorf("created directory is outside allowed projects root")
			}

			return candidate, folderName, nil
		} else if !os.IsExist(mkErr) {
			return "", "", mkErr
		}
		candidate = fmt.Sprintf("%s-%d", basePath, counter)
		folderName = fmt.Sprintf("%s-%d", sanitized, counter)
	}
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

func SaveOrUpdateProjectFiles(projectsRoot, projectPath, composeContent string, envContent *string) error {
	return WriteProjectFiles(projectsRoot, projectPath, composeContent, envContent)
}
