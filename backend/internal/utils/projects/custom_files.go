package projects

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// CustomFile represents a user-defined custom file within a project.
type CustomFile struct {
	Path    string `json:"path"`
	Content string `json:"content"`
}

// CustomFilesConfig holds configuration for custom file operations.
type CustomFilesConfig struct {
	// AllowedPaths is a list of directories where custom files can be located.
	// Paths within these directories (or within the project directory) are allowed.
	AllowedPaths []string
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
	return os.WriteFile(path, append(data, '\n'), 0644)
}

// ParseCustomFiles reads all custom files for a project.
func ParseCustomFiles(projectDir string) ([]CustomFile, error) {
	manifest, err := ReadManifest(projectDir)
	if err != nil {
		return nil, err
	}

	absProjectDir, _ := filepath.Abs(projectDir)
	var files []CustomFile

	for _, path := range manifest.CustomFiles {
		fullPath := path
		if !filepath.IsAbs(path) {
			fullPath = filepath.Join(absProjectDir, path)
		}

		content, err := os.ReadFile(fullPath)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return nil, fmt.Errorf("failed to read %s: %w", path, err)
		}

		files = append(files, CustomFile{
			Path:    path,
			Content: string(content),
		})
	}
	return files, nil
}

// ValidateCustomFilePath validates a file path for custom file operations.
// Paths are allowed if they resolve to within the project directory or any of the configured AllowedPaths.
func ValidateCustomFilePath(projectDir, filePath string, cfg CustomFilesConfig) (string, error) {
	if filePath == "" {
		return "", fmt.Errorf("file path cannot be empty")
	}

	absProjectDir, err := filepath.Abs(projectDir)
	if err != nil {
		return "", err
	}

	// Resolve to absolute path
	absPath := filePath
	if !filepath.IsAbs(filePath) {
		absPath = filepath.Join(absProjectDir, filePath)
	}
	absPath, _ = filepath.Abs(absPath)

	// Check if path is within project directory
	withinProject := strings.HasPrefix(absPath, absProjectDir+string(filepath.Separator))

	// Check if path is within any allowed directory
	withinAllowed := false
	for _, ap := range cfg.AllowedPaths {
		if strings.HasPrefix(absPath, ap+string(filepath.Separator)) || absPath == ap {
			withinAllowed = true
			break
		}
	}

	// Path must be within project or an allowed directory
	if !withinProject && !withinAllowed {
		if len(cfg.AllowedPaths) == 0 {
			return "", fmt.Errorf("path outside project; configure CUSTOM_FILES_ALLOWED_PATHS to allow external paths")
		}
		return "", fmt.Errorf("path not in project or allowed directories")
	}

	// Check reserved names at project root
	if withinProject {
		rel, _ := filepath.Rel(absProjectDir, absPath)
		if filepath.Dir(rel) == "." {
			for _, r := range reservedRootFileNames {
				if strings.EqualFold(filepath.Base(rel), r) {
					return "", fmt.Errorf("reserved file name: %s", r)
				}
			}
		}
	}

	return absPath, nil
}

// manifestPath returns the path to store in manifest (relative if in project, absolute otherwise).
func manifestPath(absPath, absProjectDir string) string {
	if strings.HasPrefix(absPath, absProjectDir+string(filepath.Separator)) {
		if rel, err := filepath.Rel(absProjectDir, absPath); err == nil {
			return rel
		}
	}
	return absPath
}

// RegisterCustomFile adds a file to the manifest. Creates empty file if it doesn't exist.
func RegisterCustomFile(projectDir, filePath string, cfg CustomFilesConfig) error {
	absPath, err := ValidateCustomFilePath(projectDir, filePath, cfg)
	if err != nil {
		return err
	}

	// Create if doesn't exist
	if _, err := os.Stat(absPath); os.IsNotExist(err) {
		if dir := filepath.Dir(absPath); dir != "." {
			os.MkdirAll(dir, 0755)
		}
		if err := os.WriteFile(absPath, []byte{}, 0644); err != nil {
			return err
		}
	}

	absProjectDir, _ := filepath.Abs(projectDir)
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

// WriteCustomFile writes content to a file and adds it to the manifest.
func WriteCustomFile(projectDir, filePath, content string, cfg CustomFilesConfig) error {
	absPath, err := ValidateCustomFilePath(projectDir, filePath, cfg)
	if err != nil {
		return err
	}

	if dir := filepath.Dir(absPath); dir != "." {
		os.MkdirAll(dir, 0755)
	}
	if err := os.WriteFile(absPath, []byte(content), 0644); err != nil {
		return err
	}

	absProjectDir, _ := filepath.Abs(projectDir)
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

// RemoveCustomFile removes a file from the manifest.
func RemoveCustomFile(projectDir, filePath string) error {
	absProjectDir, _ := filepath.Abs(projectDir)

	// Compute possible manifest paths
	absPath := filePath
	if !filepath.IsAbs(filePath) {
		absPath = filepath.Join(absProjectDir, filePath)
	}
	mPath := manifestPath(absPath, absProjectDir)

	manifest, err := ReadManifest(projectDir)
	if err != nil {
		return err
	}

	var updated []string
	for _, f := range manifest.CustomFiles {
		if f != mPath && f != filePath {
			updated = append(updated, f)
		}
	}
	manifest.CustomFiles = updated
	return WriteManifest(projectDir, manifest)
}
