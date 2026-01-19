package projects

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/getarcaneapp/arcane/backend/internal/common"
	"github.com/getarcaneapp/arcane/types/project"
	"github.com/goccy/go-yaml"
)

// ArcaneManifest is the project metadata file, extensible for future features.
type ArcaneManifest struct {
	CustomFiles []string `json:"customFiles,omitempty"`
}

// ArcaneManifestName is the project metadata file name.
const ArcaneManifestName = ".arcane"

var reservedRootFileNames = []string{
	"compose.yaml", "compose.yml", "docker-compose.yaml", "docker-compose.yml",
	".env", ArcaneManifestName,
}

const (
	// PlaceholderYAML is the placeholder content for new YAML files
	PlaceholderYAML = "# This file will be created when you save changes\nservices:\n"
	// PlaceholderGeneric is the placeholder content for new generic files
	PlaceholderGeneric = "# This file will be created when you save changes\n"
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

func resolveAbsPath(dir string) (string, error) {
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

func resolveFilePath(basePath, filePath string) (absPath, evalPath string, err error) {
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

func isWithinDirectory(evalPath, dir string) bool {
	if evalPath == dir {
		return false
	}
	evalPath = filepath.Clean(evalPath)
	prefix := dir + string(filepath.Separator)
	return strings.HasPrefix(evalPath+string(filepath.Separator), prefix)
}

func isWithinAllowedPaths(evalPath string, allowedPaths []string) bool {
	for _, ap := range allowedPaths {
		evalAllowedPath := ap
		if resolved, err := filepath.EvalSymlinks(ap); err == nil {
			evalAllowedPath = resolved
		}
		if isWithinDirectory(evalPath, evalAllowedPath) || evalPath == evalAllowedPath {
			return true
		}
	}
	return false
}

func isReservedFileName(absPath, absProjectDir string) bool {
	rel, err := filepath.Rel(absProjectDir, absPath)
	if err != nil || filepath.Dir(rel) != "." {
		return false
	}
	baseName := filepath.Base(rel)
	for _, r := range reservedRootFileNames {
		if strings.EqualFold(baseName, r) {
			return true
		}
	}
	return false
}

func readFileWithPlaceholder(absPath, placeholder string) (string, error) {
	content, err := os.ReadFile(absPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return placeholder, nil
		}
		return "", err
	}
	return string(content), nil
}

func writeFileWithDir(absPath, content string) error {
	if dir := filepath.Dir(absPath); dir != "" {
		if _, err := os.Stat(dir); errors.Is(err, os.ErrNotExist) {
			if err := os.MkdirAll(dir, common.DirPerm); err != nil {
				return fmt.Errorf("failed to create directory: %w", err)
			}
		}
	}
	return os.WriteFile(absPath, []byte(content), common.FilePerm)
}

// validatePath validates a file path for write operations.
// Returns the validated absolute path or an error.
func validatePath(projectDir, filePath string, allowedPaths []string, checkReserved bool) (string, error) {
	if filePath == "" {
		return "", fmt.Errorf("file path cannot be empty")
	}

	absProjectDir, err := resolveAbsPath(projectDir)
	if err != nil {
		return "", fmt.Errorf("invalid project directory: %w", err)
	}

	absPath, evalPath, err := resolveFilePath(absProjectDir, filePath)
	if err != nil {
		return "", err
	}

	if evalPath == absProjectDir {
		return "", fmt.Errorf("path cannot be the project directory itself")
	}

	withinProject := isWithinDirectory(evalPath, absProjectDir)
	withinAllowed := isWithinAllowedPaths(evalPath, allowedPaths)

	if !withinProject && !withinAllowed {
		if len(allowedPaths) == 0 {
			return "", fmt.Errorf("path outside project; configure ALLOWED_EXTERNAL_PATHS to allow external paths")
		}
		return "", fmt.Errorf("path not in project or allowed directories")
	}

	if checkReserved && withinProject && isReservedFileName(absPath, absProjectDir) {
		return "", fmt.Errorf("reserved file name: %s", filepath.Base(absPath))
	}

	return absPath, nil
}

// Security Model for Include Files:
// - READ: Docker Compose allows include files from anywhere (parent dirs, absolute paths, etc.)
//         We allow reading from any path to maintain compatibility with standard Docker Compose behavior
// - WRITE: Restricted to files within the project directory or configured allowed external paths

// ParseIncludes reads a compose file and extracts all include directives.
func ParseIncludes(composeFilePath string) ([]project.IncludeFile, error) {
	content, err := os.ReadFile(composeFilePath)
	if err != nil {
		return nil, fmt.Errorf("failed to read compose file: %w", err)
	}

	var composeData map[string]interface{}
	if err := yaml.Unmarshal(content, &composeData); err != nil {
		return nil, fmt.Errorf("failed to parse compose file: %w", err)
	}

	includes, ok := composeData["include"]
	if !ok {
		return nil, nil
	}

	composeDir := filepath.Dir(composeFilePath)
	var files []project.IncludeFile

	parseItem := func(item interface{}) {
		inc, err := parseIncludeItem(item, composeDir)
		if err == nil {
			files = append(files, inc)
		}
	}

	switch v := includes.(type) {
	case []interface{}:
		for _, item := range v {
			parseItem(item)
		}
	case string:
		parseItem(v)
	}

	return files, nil
}

func parseIncludeItem(item interface{}, baseDir string) (project.IncludeFile, error) {
	var includePath string

	switch v := item.(type) {
	case string:
		includePath = v
	case map[string]interface{}:
		if path, ok := v["path"].(string); ok {
			includePath = path
		}
	default:
		return project.IncludeFile{}, fmt.Errorf("invalid include item type")
	}

	if includePath == "" {
		return project.IncludeFile{}, fmt.Errorf("empty include path")
	}

	fullPath := includePath
	if !filepath.IsAbs(includePath) {
		fullPath = filepath.Join(baseDir, includePath)
	}
	fullPath = filepath.Clean(fullPath)

	content, err := readFileWithPlaceholder(fullPath, PlaceholderYAML)
	if err != nil {
		return project.IncludeFile{}, fmt.Errorf("failed to read include file %s: %w", includePath, err)
	}

	relativePath := includePath
	if filepath.IsAbs(includePath) {
		if rel, err := filepath.Rel(baseDir, fullPath); err == nil {
			relativePath = rel
		}
	}

	return project.IncludeFile{
		Path:         fullPath,
		RelativePath: relativePath,
		Content:      content,
	}, nil
}

// WriteIncludeFile writes content to an include file path.
func WriteIncludeFile(projectDir, includePath, content string, allowedPaths []string) error {
	absPath, err := validatePath(projectDir, includePath, allowedPaths, false)
	if err != nil {
		return err
	}
	return writeFileWithDir(absPath, content)
}

// ReadManifest reads the .arcane manifest file.
func ReadManifest(projectDir string) (*ArcaneManifest, error) {
	data, err := os.ReadFile(filepath.Join(projectDir, ArcaneManifestName))
	if err != nil {
		if os.IsNotExist(err) {
			return &ArcaneManifest{}, nil
		}
		return nil, err
	}
	var m ArcaneManifest
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, err
	}
	return &m, nil
}

// WriteManifest writes the .arcane manifest file.
func WriteManifest(projectDir string, m *ArcaneManifest) error {
	path := filepath.Join(projectDir, ArcaneManifestName)
	if len(m.CustomFiles) == 0 {
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			return err
		}
		return nil
	}
	data, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(data, '\n'), common.FilePerm)
}

// manifestPath returns the path to store in manifest (relative if in project, absolute otherwise).
func manifestPath(absPath, absProjectDir string) string {
	if rel, err := filepath.Rel(absProjectDir, absPath); err == nil && !strings.HasPrefix(rel, "..") {
		return rel
	}
	return absPath
}

func addToManifest(projectDir, absPath string) error {
	absProjectDir, _ := resolveAbsPath(projectDir)
	mPath := manifestPath(absPath, absProjectDir)

	manifest, err := ReadManifest(projectDir)
	if err != nil {
		return err
	}

	for _, f := range manifest.CustomFiles {
		if f == mPath {
			return nil
		}
	}

	manifest.CustomFiles = append(manifest.CustomFiles, mPath)
	return WriteManifest(projectDir, manifest)
}

// ParseCustomFiles reads all custom files for a project.
// Security: Validates paths against allowed external paths to prevent manifest tampering attacks.
func ParseCustomFiles(projectDir string, allowedPaths []string) ([]project.CustomFile, error) {
	manifest, err := ReadManifest(projectDir)
	if err != nil {
		return nil, err
	}

	var files []project.CustomFile
	for _, path := range manifest.CustomFiles {
		absPath, err := validatePath(projectDir, path, allowedPaths, false)
		if err != nil {
			continue // Skip invalid paths (manifest may be tampered)
		}

		content, err := readFileWithPlaceholder(absPath, PlaceholderGeneric)
		if err != nil {
			return nil, fmt.Errorf("failed to read %s: %w", path, err)
		}

		files = append(files, project.CustomFile{
			Path:    path,
			Content: content,
		})
	}
	return files, nil
}

// RegisterCustomFile adds a file to the manifest without creating it on disk.
func RegisterCustomFile(projectDir, filePath string, allowedPaths []string) error {
	absPath, err := validatePath(projectDir, filePath, allowedPaths, true)
	if err != nil {
		return err
	}
	return addToManifest(projectDir, absPath)
}

// WriteCustomFile writes content to a file and adds it to the manifest.
func WriteCustomFile(projectDir, filePath, content string, allowedPaths []string) error {
	absPath, err := validatePath(projectDir, filePath, allowedPaths, true)
	if err != nil {
		return err
	}
	if err := writeFileWithDir(absPath, content); err != nil {
		return fmt.Errorf("failed to write custom file: %w", err)
	}
	return addToManifest(projectDir, absPath)
}

// RemoveCustomFile removes a file from the manifest and optionally deletes it from disk.
func RemoveCustomFile(projectDir, filePath string, allowedPaths []string, deleteFromDisk bool) error {
	absPath, err := validatePath(projectDir, filePath, allowedPaths, false)
	if err != nil {
		return fmt.Errorf("invalid file path: %w", err)
	}

	absProjectDir, _ := resolveAbsPath(projectDir)
	mPath := manifestPath(absPath, absProjectDir)

	manifest, err := ReadManifest(projectDir)
	if err != nil {
		return err
	}

	var updated []string
	for _, f := range manifest.CustomFiles {
		if f != mPath {
			updated = append(updated, f)
		}
	}
	manifest.CustomFiles = updated

	if err := WriteManifest(projectDir, manifest); err != nil {
		return err
	}

	if deleteFromDisk {
		if err := os.Remove(absPath); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("failed to delete file from disk: %w", err)
		}
	}

	return nil
}
