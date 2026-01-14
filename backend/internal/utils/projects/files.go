package projects

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/getarcaneapp/arcane/backend/internal/common"
	"github.com/goccy/go-yaml"
)

// ExternalPathsConfig holds configuration for file path validation.
// Used by both include files and custom files to validate external path access.
type ExternalPathsConfig struct {
	// AllowedPaths is a list of directories where files can be located outside the project directory.
	// Paths within these directories (or within the project directory) are allowed.
	AllowedPaths []string
}

// PathValidationOptions configures the behavior of ValidateFilePath.
type PathValidationOptions struct {
	// CheckReservedNames enables checking for reserved file names at project root (for custom files).
	CheckReservedNames bool
	// AllowProjectDir allows the path to be the project directory itself.
	AllowProjectDir bool
}

// IncludeFile represents an include directive from a Docker Compose file.
type IncludeFile struct {
	Path         string `json:"path"`
	RelativePath string `json:"relative_path"`
	Content      string `json:"content"`
}

// CustomFile represents a user-defined custom file within a project.
type CustomFile struct {
	Path    string `json:"path"`
	Content string `json:"content"`
}

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

// resolveAbsPath resolves a directory path to an absolute path with symlinks evaluated.
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

// resolveFilePath resolves a file path relative to a base directory, evaluating symlinks.
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

	// Resolve symlinks to prevent symlink-based path traversal attacks
	if resolved, err := filepath.EvalSymlinks(absPath); err == nil {
		return absPath, resolved, nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return "", "", fmt.Errorf("failed to resolve path: %w", err)
	}

	// File doesn't exist yet - evaluate parent directory symlinks
	evalPath, err = resolveParentSymlinks(absPath)
	if err != nil {
		return "", "", err
	}
	return absPath, evalPath, nil
}

// resolveParentSymlinks resolves symlinks in the parent directory of a non-existent path.
func resolveParentSymlinks(absPath string) (string, error) {
	dir := filepath.Dir(absPath)
	if evalDir, err := filepath.EvalSymlinks(dir); err == nil {
		return filepath.Join(evalDir, filepath.Base(absPath)), nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return "", fmt.Errorf("failed to resolve parent directory: %w", err)
	}
	return absPath, nil
}

// isWithinDirectory checks if evalPath is within the given directory.
func isWithinDirectory(evalPath, dir string) bool {
	if evalPath == dir {
		return false
	}
	evalPath = filepath.Clean(evalPath)
	prefix := dir + string(filepath.Separator)
	return strings.HasPrefix(evalPath+string(filepath.Separator), prefix)
}

// isWithinAllowedPaths checks if evalPath is within any of the allowed paths.
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

// isReservedFileName checks if the file at absPath is a reserved name at the project root.
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

// readFileWithPlaceholder reads a file, returning placeholder content if it doesn't exist.
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

// writeFileWithDir writes content to a file, creating parent directories as needed.
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

// ValidateFilePath validates a file path for write operations.
// Paths are allowed if they resolve to within the project directory or any of the configured AllowedPaths.
// Returns the validated absolute path.
func ValidateFilePath(projectDir, filePath string, cfg ExternalPathsConfig, opts PathValidationOptions) (string, error) {
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

	if evalPath == absProjectDir && !opts.AllowProjectDir {
		return "", fmt.Errorf("path cannot be the project directory itself")
	}

	withinProject := isWithinDirectory(evalPath, absProjectDir)
	withinAllowed := isWithinAllowedPaths(evalPath, cfg.AllowedPaths)

	if !withinProject && !withinAllowed {
		if len(cfg.AllowedPaths) == 0 {
			return "", fmt.Errorf("path outside project; configure ALLOWED_EXTERNAL_PATHS to allow external paths")
		}
		return "", fmt.Errorf("path not in project or allowed directories")
	}

	if opts.CheckReservedNames && withinProject && isReservedFileName(absPath, absProjectDir) {
		rel, _ := filepath.Rel(absProjectDir, absPath)
		return "", fmt.Errorf("reserved file name: %s", filepath.Base(rel))
	}

	return absPath, nil
}

// Security Model for Include Files:
// - READ: Docker Compose allows include files from anywhere (parent dirs, absolute paths, etc.)
//         We allow reading from any path to maintain compatibility with standard Docker Compose behavior
// - WRITE/DELETE: Restricted to files within the project directory or configured allowed external paths
//         This prevents malicious users from modifying files outside the allowed scope

// ParseIncludes reads a compose file and extracts all include directives
func ParseIncludes(composeFilePath string) ([]IncludeFile, error) {
	content, err := os.ReadFile(composeFilePath)
	if err != nil {
		return nil, fmt.Errorf("failed to read compose file: %w", err)
	}

	var composeData map[string]interface{}
	if err := yaml.Unmarshal(content, &composeData); err != nil {
		return nil, fmt.Errorf("failed to parse compose file: %w", err)
	}

	// Look for include at root level only (per Docker Compose spec)
	includes, ok := composeData["include"]
	if !ok {
		return []IncludeFile{}, nil
	}

	composeDir := filepath.Dir(composeFilePath)
	var includeFiles []IncludeFile

	switch v := includes.(type) {
	case []interface{}:
		for _, item := range v {
			if include, err := parseIncludeItem(item, composeDir); err == nil {
				includeFiles = append(includeFiles, include)
			}
		}
	case string:
		if include, err := parseIncludeItem(v, composeDir); err == nil {
			includeFiles = append(includeFiles, include)
		}
	}

	return includeFiles, nil
}

const (
	// PlaceholderYAML is the placeholder content for new YAML files (compose includes)
	PlaceholderYAML = "# This file will be created when you save changes\nservices:\n"
	// PlaceholderGeneric is the placeholder content for new generic files
	PlaceholderGeneric = "# This file will be created when you save changes\n"
)

func parseIncludeItem(item interface{}, baseDir string) (IncludeFile, error) {
	var includePath string

	switch v := item.(type) {
	case string:
		includePath = v
	case map[string]interface{}:
		if path, ok := v["path"].(string); ok {
			includePath = path
		}
	default:
		return IncludeFile{}, fmt.Errorf("invalid include item type")
	}

	if includePath == "" {
		return IncludeFile{}, fmt.Errorf("empty include path")
	}

	fullPath := includePath
	if !filepath.IsAbs(includePath) {
		fullPath = filepath.Join(baseDir, includePath)
	}
	fullPath = filepath.Clean(fullPath)

	content, err := readFileWithPlaceholder(fullPath, PlaceholderYAML)
	if err != nil {
		return IncludeFile{}, fmt.Errorf("failed to read include file %s: %w", includePath, err)
	}

	relativePath := includePath
	if filepath.IsAbs(includePath) {
		if rel, err := filepath.Rel(baseDir, fullPath); err == nil {
			relativePath = rel
		}
	}

	return IncludeFile{
		Path:         fullPath,
		RelativePath: relativePath,
		Content:      content,
	}, nil
}

// WriteIncludeFile writes content to an include file path
func WriteIncludeFile(projectDir, includePath, content string, cfg ExternalPathsConfig) error {
	validatedPath, err := ValidateFilePath(projectDir, includePath, cfg, PathValidationOptions{
		CheckReservedNames: false,
		AllowProjectDir:    false,
	})
	if err != nil {
		return err
	}

	if err := writeFileWithDir(validatedPath, content); err != nil {
		return fmt.Errorf("failed to write include file: %w", err)
	}

	return nil
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

// ParseCustomFiles reads all custom files for a project.
// Security: Validates paths against allowed external paths to prevent manifest tampering attacks.
func ParseCustomFiles(projectDir string, cfg ExternalPathsConfig) ([]CustomFile, error) {
	manifest, err := ReadManifest(projectDir)
	if err != nil {
		return nil, err
	}

	var files []CustomFile

	for _, path := range manifest.CustomFiles {
		// Validate path using the same rules as RegisterCustomFile
		absPath, err := ValidateFilePath(projectDir, path, cfg, PathValidationOptions{
			CheckReservedNames: false,
			AllowProjectDir:    false,
		})
		if err != nil {
			continue // Skip invalid paths (manifest may be tampered)
		}

		content, err := readFileWithPlaceholder(absPath, PlaceholderGeneric)
		if err != nil {
			return nil, fmt.Errorf("failed to read %s: %w", path, err)
		}

		files = append(files, CustomFile{
			Path:    path,
			Content: content,
		})
	}
	return files, nil
}

// manifestPath returns the path to store in manifest (relative if in project, absolute otherwise).
func manifestPath(absPath, absProjectDir string) string {
	if rel, err := filepath.Rel(absProjectDir, absPath); err == nil && !strings.HasPrefix(rel, "..") {
		return rel
	}
	return absPath
}

// addToManifest adds a file path to the manifest if not already present.
func addToManifest(projectDir, absPath string) error {
	absProjectDir, _ := resolveAbsPath(projectDir)
	mPath := manifestPath(absPath, absProjectDir)

	manifest, err := ReadManifest(projectDir)
	if err != nil {
		return err
	}

	for _, f := range manifest.CustomFiles {
		if f == mPath {
			return nil // Already registered
		}
	}

	manifest.CustomFiles = append(manifest.CustomFiles, mPath)
	return WriteManifest(projectDir, manifest)
}

// RegisterCustomFile adds a file to the manifest without creating it on disk.
// The file will be created when content is saved via WriteCustomFile.
func RegisterCustomFile(projectDir, filePath string, cfg ExternalPathsConfig) error {
	absPath, err := ValidateFilePath(projectDir, filePath, cfg, PathValidationOptions{
		CheckReservedNames: true,
		AllowProjectDir:    false,
	})
	if err != nil {
		return err
	}

	return addToManifest(projectDir, absPath)
}

// WriteCustomFile writes content to a file and adds it to the manifest.
// Security: Validates paths against allowed external paths before writing.
func WriteCustomFile(projectDir, filePath, content string, cfg ExternalPathsConfig) error {
	absPath, err := ValidateFilePath(projectDir, filePath, cfg, PathValidationOptions{
		CheckReservedNames: true,
		AllowProjectDir:    false,
	})
	if err != nil {
		return err
	}

	if err := writeFileWithDir(absPath, content); err != nil {
		return fmt.Errorf("failed to write custom file: %w", err)
	}

	return addToManifest(projectDir, absPath)
}

// RemoveCustomFile removes a file from the manifest.
func RemoveCustomFile(projectDir, filePath string, cfg ExternalPathsConfig) error {
	absPath, err := ValidateFilePath(projectDir, filePath, cfg, PathValidationOptions{
		CheckReservedNames: false,
		AllowProjectDir:    false,
	})
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
	return WriteManifest(projectDir, manifest)
}
