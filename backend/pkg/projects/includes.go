package projects

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	pkgutils "github.com/getarcaneapp/arcane/backend/v2/pkg/utils"
	"go.yaml.in/yaml/v4"
)

// expandEnvVarsInternal expands ${VAR} and $VAR references in a string using the provided env map.
func expandEnvVarsInternal(s string, envMap EnvMap) string {
	return os.Expand(s, func(key string) string {
		if val, ok := envMap[key]; ok {
			return val
		}
		return ""
	})
}

// Security Model for Include Files:
// - READ: Docker Compose's spec allows include files from anywhere (parent dirs,
//   absolute paths). ParseIncludes does NOT enforce containment; it returns whatever
//   the compose file points at. Callers must validate containment and symlinks before
//   reading include content or returning inc.Content to users.
// - WRITE/DELETE: Restricted to files within the project directory only for security.
//   Always go through ValidateIncludePathForWrite or WriteIncludeFile.

type IncludeFile struct {
	Path         string `json:"path"`
	RelativePath string `json:"relative_path"`
	Content      string `json:"content"`
}

// ParseIncludes reads a compose file and extracts all include directives.
// envMap is used to expand variables (e.g., ${VAR}) in include paths.
func ParseIncludes(composeFilePath string, envMap EnvMap, includeContent bool) ([]IncludeFile, error) {
	content, err := os.ReadFile(composeFilePath)
	if err != nil {
		return nil, fmt.Errorf("failed to read compose file: %w", err)
	}

	return ParseIncludesFromContent(composeFilePath, content, envMap, includeContent)
}

// ParseIncludesFromContent extracts include directives from compose content using composeFilePath as the base path.
func ParseIncludesFromContent(composeFilePath string, content []byte, envMap EnvMap, includeContent bool) ([]IncludeFile, error) {
	var composeData map[string]any
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
	case []any:
		for _, item := range v {
			incs, err := parseIncludeItemInternal(item, composeDir, envMap, includeContent)
			if err != nil {
				return nil, err
			}
			includeFiles = append(includeFiles, incs...)
		}
	case string:
		incs, err := parseIncludeItemInternal(v, composeDir, envMap, includeContent)
		if err != nil {
			return nil, err
		}
		includeFiles = append(includeFiles, incs...)
	case nil:
		// `include:` key present but null (e.g. `include: ~`) — treat as empty.
		return []IncludeFile{}, nil
	default:
		return nil, errors.New("invalid include type")
	}

	return includeFiles, nil
}

func parseIncludeItemInternal(item any, baseDir string, envMap EnvMap, includeContent bool) ([]IncludeFile, error) {
	includePaths, err := extractIncludePathsInternal(item)
	if err != nil {
		return nil, err
	}

	results := make([]IncludeFile, 0, len(includePaths))
	for _, includePath := range includePaths {
		inc, err := resolveIncludeFileInternal(includePath, baseDir, envMap, includeContent)
		if err != nil {
			return nil, err
		}
		results = append(results, inc)
	}
	return results, nil
}

func extractIncludePathsInternal(item any) ([]string, error) {
	switch v := item.(type) {
	case string:
		return []string{v}, nil
	case map[string]any:
		return extractIncludePathsFromMapInternal(v)
	default:
		return nil, errors.New("invalid include item type")
	}
}

func extractIncludePathsFromMapInternal(v map[string]any) ([]string, error) {
	switch p := v["path"].(type) {
	case string:
		return []string{p}, nil
	case []any:
		// Docker Compose allows `path: [./base.yaml, ./override.yaml]` for multi-file overrides.
		paths := make([]string, 0, len(p))
		for _, entry := range p {
			s, ok := entry.(string)
			if !ok {
				return nil, fmt.Errorf("invalid include path entry: expected string, got %T", entry)
			}
			paths = append(paths, s)
		}
		return paths, nil
	default:
		return nil, fmt.Errorf("invalid include path type: %T", v["path"])
	}
}

func resolveIncludeFileInternal(includePath, baseDir string, envMap EnvMap, includeContent bool) (IncludeFile, error) {
	if includePath == "" {
		return IncludeFile{}, errors.New("empty include path")
	}

	// Expand environment variables in the include path (e.g., ${PROJECT_STACK_DIR})
	if len(envMap) > 0 {
		includePath = expandEnvVarsInternal(includePath, envMap)
	}

	fullPath := includePath
	if !filepath.IsAbs(includePath) {
		fullPath = filepath.Join(baseDir, includePath)
	}
	fullPath = filepath.Clean(fullPath)

	content, err := readIncludeContentInternal(fullPath, includePath, includeContent)
	if err != nil {
		return IncludeFile{}, err
	}

	relativePath := includePath
	if filepath.IsAbs(includePath) {
		if rel, err := filepath.Rel(baseDir, fullPath); err == nil {
			relativePath = rel
		}
	}
	relativePath = filepath.ToSlash(filepath.Clean(relativePath))
	if relativePath == "." {
		relativePath = filepath.Base(fullPath)
	}

	return IncludeFile{
		Path:         fullPath,
		RelativePath: relativePath,
		Content:      content,
	}, nil
}

func readIncludeContentInternal(fullPath, includePath string, includeContent bool) (string, error) {
	if !includeContent {
		return "", nil
	}
	fileContent, err := os.ReadFile(fullPath)
	if err == nil {
		return string(fileContent), nil
	}
	if errors.Is(err, os.ErrNotExist) {
		// File doesn't exist yet - return empty content so it can be created
		return "# This file will be created when you save changes\nservices:\n", nil
	}
	return "", fmt.Errorf("failed to read include file %s: %w", includePath, err)
}

// ValidateIncludePathForWrite ensures the include path is safe for write operations
// Returns the validated absolute path to prevent recomputation after validation
// Only allows writing within the project directory
func ValidateIncludePathForWrite(projectDir, includePath string) (string, error) {
	if includePath == "" {
		return "", errors.New("include path cannot be empty")
	}

	// Resolve project directory to absolute path and evaluate symlinks
	absProjectDir, err := filepath.Abs(projectDir)
	if err != nil {
		return "", fmt.Errorf("invalid project directory: %w", err)
	}
	absProjectDir = filepath.Clean(absProjectDir)

	// Try to resolve symlinks for the project directory if it exists
	if evalProjectDir, err := filepath.EvalSymlinks(absProjectDir); err == nil {
		absProjectDir = evalProjectDir
	}

	// Resolve include path to absolute path
	fullPath := includePath
	if !filepath.IsAbs(includePath) {
		fullPath = filepath.Join(absProjectDir, includePath)
	}

	absFullPath, err := filepath.Abs(fullPath)
	if err != nil {
		return "", fmt.Errorf("invalid include path: %w", err)
	}
	absFullPath = filepath.Clean(absFullPath)

	// Resolve symlinks in the include path to prevent symlink-based path traversal attacks
	evalPath := absFullPath
	if evalFullPath, err := filepath.EvalSymlinks(absFullPath); err == nil {
		evalPath = evalFullPath
	} else if !errors.Is(err, os.ErrNotExist) {
		return "", fmt.Errorf("failed to resolve include path: %w", err)
	} else {
		// File doesn't exist yet - evaluate parent directory symlinks
		dir := filepath.Dir(absFullPath)
		if evalDir, err := filepath.EvalSymlinks(dir); err == nil {
			evalPath = filepath.Join(evalDir, filepath.Base(absFullPath))
		} else if !errors.Is(err, os.ErrNotExist) {
			return "", fmt.Errorf("failed to resolve parent directory: %w", err)
		}
	}

	// Prevent targeting the project directory itself
	if evalPath == absProjectDir {
		return "", errors.New("include path cannot be the project directory itself")
	}

	// Check if resolved path is within project directory
	projectPrefix := absProjectDir + string(filepath.Separator)
	isWithinProject := strings.HasPrefix(evalPath+string(filepath.Separator), projectPrefix)

	if !isWithinProject {
		return "", errors.New("write access denied: path is outside project directory")
	}

	return absFullPath, nil
}

// WriteIncludeFile writes content to an include file path
func WriteIncludeFile(projectDir, includePath, content string) error {
	// Get validated absolute path - only allows writes within project
	validatedPath, err := ValidateIncludePathForWrite(projectDir, includePath)
	if err != nil {
		return err
	}

	dir := filepath.Dir(validatedPath)
	if dir == "" || dir == "." {
		return fmt.Errorf("invalid include path: cannot create directory '%s'", dir)
	}

	// Only create directory if it doesn't exist
	if _, err := os.Stat(dir); errors.Is(err, os.ErrNotExist) {
		if err := os.MkdirAll(dir, pkgutils.DirPerm); err != nil {
			return fmt.Errorf("failed to create directory: %w", err)
		}
	}

	if err := os.WriteFile(validatedPath, []byte(content), pkgutils.FilePerm); err != nil {
		return fmt.Errorf("failed to write include file: %w", err)
	}

	return nil
}
