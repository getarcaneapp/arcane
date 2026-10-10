package projects

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/compose-spec/compose-go/v2/template"
	"github.com/samber/mo"
	"go.getarcane.app/acfs"
	"go.yaml.in/yaml/v4"
)

// Security Model for Include Files:
// - READ: Docker Compose's spec allows include files from anywhere (parent dirs,
//   absolute paths). ParseIncludes does NOT enforce containment; it returns whatever
//   the compose file points at. Callers must validate containment and symlinks before
//   reading include content or returning inc.Content to users.
// - WRITE/DELETE: Restricted to files within the project directory only for security.
//   Always go through ValidateIncludePathForWrite.

type IncludeFile struct {
	Path         string `json:"path"`
	RelativePath string `json:"relative_path"`
	Content      string `json:"content"`
}

// MissingIncludeStubLoader creates temporary stub compose files for missing
// include paths during validation.
type MissingIncludeStubLoader struct {
	projectPath string
	tempDir     string
	stubs       map[string]string
}

// NewMissingIncludeStubLoader creates a loader for validation stubs under projectPath.
func NewMissingIncludeStubLoader(projectPath string) *MissingIncludeStubLoader {
	return &MissingIncludeStubLoader{projectPath: projectPath}
}

func (l *MissingIncludeStubLoader) Accept(localPath string) bool {
	_, ok := l.resolveMissingIncludeInternal(localPath).Get()
	return ok
}

func (l *MissingIncludeStubLoader) Load(ctx context.Context, filePath string) (string, error) {
	validatedPath, ok := l.resolveMissingIncludeInternal(filePath).Get()
	if !ok {
		return "", fmt.Errorf("include file is not eligible for validation stub: %s", filePath)
	}

	if l.stubs == nil {
		l.stubs = make(map[string]string)
	}
	if stubPath, localOk := l.stubs[validatedPath]; localOk {
		return stubPath, nil
	}

	if l.tempDir == "" {
		// System temp scratch dir: no acfs root exists for it.
		tempDir, err := os.MkdirTemp("", "arcane-compose-include-*")
		if err != nil {
			return "", fmt.Errorf("create validation include temp dir: %w", err)
		}
		l.tempDir = tempDir
	}

	relPath, err := filepath.Rel(l.projectPath, validatedPath)
	if err != nil || strings.HasPrefix(relPath, "..") || filepath.IsAbs(relPath) {
		relPath = filepath.Base(validatedPath)
	}
	stubPath := filepath.Join(l.tempDir, relPath)
	stubLogical, err := acfs.LogicalPath(l.tempDir, stubPath)
	if err != nil {
		return "", fmt.Errorf("resolve validation include stub path: %w", err)
	}
	if mkdirAllErr := acfs.MkdirAll(ctx, l.tempDir, path.Dir(stubLogical), 0o755); mkdirAllErr != nil {
		return "", fmt.Errorf("create validation include directory: %w", mkdirAllErr)
	}
	if writeErr := acfs.Write(ctx, l.tempDir, stubLogical, []byte("services: {}\n"), acfs.WriteOptions{Mode: 0o600}); writeErr != nil {
		return "", fmt.Errorf("write validation include stub: %w", writeErr)
	}

	l.stubs[validatedPath] = stubPath
	return stubPath, nil
}

func (l *MissingIncludeStubLoader) Dir(localPath string) string {
	return filepath.Dir(localPath)
}

func (l *MissingIncludeStubLoader) resolveMissingIncludeInternal(localPath string) mo.Option[string] {
	validatedPath, err := ValidateIncludePathForWrite(l.projectPath, localPath)
	if err != nil {
		return mo.None[string]()
	}

	// os.Stat rather than acfs: the validated include target may live outside
	// the project directory (#3556).
	if _, statErr := os.Stat(validatedPath); statErr == nil {
		return mo.None[string]()
	} else if !errors.Is(statErr, os.ErrNotExist) {
		return mo.None[string]()
	}

	return mo.Some(validatedPath)
}

// Cleanup removes any temporary validation stub files created by the loader.
func (l *MissingIncludeStubLoader) Cleanup() {
	if l.tempDir != "" {
		_ = os.RemoveAll(l.tempDir)
	}
}

// ParseIncludes reads a compose file and extracts all include directives.
// envMap is used to expand variables (e.g., ${VAR}) in include paths.
//
// Include handling stays on os.*: include files are allowed to live outside
// the project directory (#3556), which the root-confined acfs API cannot reach.
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
		inc, resolveIncludeFileErr := resolveIncludeFileInternal(includePath, baseDir, envMap, includeContent)
		if resolveIncludeFileErr != nil {
			return nil, resolveIncludeFileErr
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

	// Interpolate with Compose semantics so ${VAR:-default} and ${VAR:?err} resolve.
	expanded, err := template.SubstituteWithOptions(includePath, func(key string) (string, bool) {
		value, ok := envMap[key]
		return value, ok
	}, template.WithoutLogging)
	if err != nil {
		return IncludeFile{}, fmt.Errorf("interpolate include path %s: %w", includePath, err)
	}
	includePath = expanded

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
		if rel, relErr := filepath.Rel(baseDir, fullPath); relErr == nil {
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
	// os.ReadFile rather than acfs: fullPath may resolve outside the project
	// directory (#3556).
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
	if evalProjectDir, evalSymlinksErr := filepath.EvalSymlinks(absProjectDir); evalSymlinksErr == nil {
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
	evalFullPath, err := filepath.EvalSymlinks(absFullPath)
	switch {
	case err == nil:
		evalPath = evalFullPath
	case !errors.Is(err, os.ErrNotExist):
		return "", fmt.Errorf("failed to resolve include path: %w", err)
	default:
		// File doesn't exist yet - evaluate parent directory symlinks
		evalDir, dirErr := filepath.EvalSymlinks(filepath.Dir(absFullPath))
		if dirErr != nil && !errors.Is(dirErr, os.ErrNotExist) {
			return "", fmt.Errorf("failed to resolve parent directory: %w", dirErr)
		}
		if dirErr == nil {
			evalPath = filepath.Join(evalDir, filepath.Base(absFullPath))
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
