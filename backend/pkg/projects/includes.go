package projects

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	pkgutils "github.com/getarcaneapp/arcane/backend/pkg/utils"
	"github.com/goccy/go-yaml"
)

// buildServiceOriginMapInternal walks `include:` directives in composeFilePath and
// returns a map of service name -> origin (working dir + compose file) for
// every service contributed by an include. Services defined directly in the
// top-level compose file are NOT in the map; callers should fall back to
// the top-level workdir for those.
//
// Per the Docker Compose spec, an include's effective working directory is
// its `project_directory` if set, otherwise the directory of the first
// listed include path. Relative bind mounts inside an included compose
// resolve against that working directory — losing it (as Arcane v1.17.0 did)
// caused #2264, where included services' bind mounts were re-rooted at the
// top-level project's directory and Postgres ran initdb on what it thought
// were empty data directories.
func buildServiceOriginMapInternal(ctx context.Context, composeFilePath string, envMap EnvMap) map[string]serviceOrigin {
	out := map[string]serviceOrigin{}
	visited := map[string]bool{}
	collectServiceOriginsInternal(ctx, composeFilePath, envMap, out, visited)
	return out
}

func collectServiceOriginsInternal(ctx context.Context, composeFilePath string, envMap EnvMap, out map[string]serviceOrigin, visited map[string]bool) {
	abs, err := filepath.Abs(composeFilePath)
	if err != nil {
		return
	}
	abs = filepath.Clean(abs)
	if visited[abs] {
		return // cycle guard
	}
	visited[abs] = true

	content, err := os.ReadFile(abs)
	if err != nil {
		return
	}
	var data map[string]any
	if err := yaml.Unmarshal(content, &data); err != nil {
		slog.DebugContext(ctx, "buildServiceOriginMapInternal: failed to parse compose file", "path", abs, "error", err)
		return
	}

	composeDir := filepath.Dir(abs)

	includesRaw, ok := data["include"]
	if !ok {
		return
	}
	var items []any
	switch v := includesRaw.(type) {
	case []any:
		items = v
	case string:
		items = []any{v}
	default:
		return
	}

	for _, item := range items {
		var pathField string
		var projectDirField string
		switch v := item.(type) {
		case string:
			pathField = v
		case map[string]any:
			if p, ok := v["path"].(string); ok {
				pathField = p
			} else if pl, ok := v["path"].([]any); ok && len(pl) > 0 {
				if first, ok := pl[0].(string); ok {
					pathField = first
				}
			}
			if pd, ok := v["project_directory"].(string); ok {
				projectDirField = pd
			}
		default:
			continue
		}
		if pathField == "" {
			continue
		}

		if len(envMap) > 0 {
			pathField = expandEnvVarsInternal(pathField, envMap)
			if projectDirField != "" {
				projectDirField = expandEnvVarsInternal(projectDirField, envMap)
			}
		}

		includePath := pathField
		if !filepath.IsAbs(includePath) {
			includePath = filepath.Join(composeDir, includePath)
		}
		includePath = filepath.Clean(includePath)

		// Per spec: project_directory if set, otherwise dir of the first
		// include path.
		var workingDir string
		switch {
		case projectDirField == "":
			workingDir = filepath.Dir(includePath)
		case filepath.IsAbs(projectDirField):
			workingDir = filepath.Clean(projectDirField)
		default:
			workingDir = filepath.Clean(filepath.Join(composeDir, projectDirField))
		}

		// Parse included file to enumerate its services and recurse into
		// any nested includes.
		incContent, err := os.ReadFile(includePath)
		if err != nil {
			continue
		}
		var incData map[string]any
		if err := yaml.Unmarshal(incContent, &incData); err != nil {
			continue
		}
		if svcs, ok := incData["services"].(map[string]any); ok {
			for svcName := range svcs {
				// First write wins: if a service appears in multiple
				// includes, keep the earliest origin (matches compose's
				// merge order).
				if _, exists := out[svcName]; !exists {
					out[svcName] = serviceOrigin{
						WorkingDir:  workingDir,
						ComposeFile: includePath,
					}
				}
			}
		}
		collectServiceOriginsInternal(ctx, includePath, envMap, out, visited)
	}
}

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
// - READ: Docker Compose allows include files from anywhere (parent dirs, absolute paths, etc.)
//         We allow reading from any path to maintain compatibility with standard Docker Compose behavior
// - WRITE/DELETE: Restricted to files within the project directory only for security
//         This prevents malicious users from modifying files outside the project scope

type IncludeFile struct {
	Path         string `json:"path"`
	RelativePath string `json:"relative_path"`
	Content      string `json:"content"`
}

// ParseIncludes reads a compose file and extracts all include directives.
// envMap is used to expand variables (e.g., ${VAR}) in include paths.
func ParseIncludes(composeFilePath string, envMap EnvMap) ([]IncludeFile, error) {
	content, err := os.ReadFile(composeFilePath)
	if err != nil {
		return nil, fmt.Errorf("failed to read compose file: %w", err)
	}

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
			if include, err := parseIncludeItemInternal(item, composeDir, envMap); err == nil {
				includeFiles = append(includeFiles, include)
			}
		}
	case string:
		if include, err := parseIncludeItemInternal(v, composeDir, envMap); err == nil {
			includeFiles = append(includeFiles, include)
		}
	}

	return includeFiles, nil
}

func parseIncludeItemInternal(item any, baseDir string, envMap EnvMap) (IncludeFile, error) {
	var includePath string

	switch v := item.(type) {
	case string:
		includePath = v
	case map[string]any:
		if path, ok := v["path"].(string); ok {
			includePath = path
		}
	default:
		return IncludeFile{}, fmt.Errorf("invalid include item type")
	}

	if includePath == "" {
		return IncludeFile{}, fmt.Errorf("empty include path")
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

	var content string
	fileContent, err := os.ReadFile(fullPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			// File doesn't exist yet - return empty content so it can be created
			content = "# This file will be created when you save changes\nservices:\n"
		} else {
			return IncludeFile{}, fmt.Errorf("failed to read include file %s: %w", includePath, err)
		}
	} else {
		content = string(fileContent)
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

// ValidateIncludePathForWrite ensures the include path is safe for write operations
// Returns the validated absolute path to prevent recomputation after validation
// Only allows writing within the project directory
func ValidateIncludePathForWrite(projectDir, includePath string) (string, error) {
	if includePath == "" {
		return "", fmt.Errorf("include path cannot be empty")
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
		return "", fmt.Errorf("include path cannot be the project directory itself")
	}

	// Check if resolved path is within project directory
	projectPrefix := absProjectDir + string(filepath.Separator)
	isWithinProject := strings.HasPrefix(evalPath+string(filepath.Separator), projectPrefix)

	if !isWithinProject {
		return "", fmt.Errorf("write access denied: path is outside project directory")
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
