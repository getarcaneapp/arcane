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
	"go.yaml.in/yaml/v4"

	"kit.getarcane.app/acfs"
	acfstypes "kit.getarcane.app/acfs/types"
)

type (
	IncludeFile struct {
		Path         string `json:"path"`
		RelativePath string `json:"relative_path"`
		Content      string `json:"content"`
	}

	MissingIncludeStubLoader struct {
		ctx         context.Context
		stubs       map[string]string
		projectPath string
		tempDir     string
	}
)

func (l *MissingIncludeStubLoader) Accept(localPath string) bool {
	_, ok := l.resolveMissingInclude(localPath).Get()
	return ok
}

func (l *MissingIncludeStubLoader) Load(ctx context.Context, filePath string) (string, error) {
	validatedPath, ok := l.resolveMissingInclude(filePath).Get()
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
		tempDir, err := acfs.Host{}.MkdirTemp(ctx, os.TempDir(), "arcane-compose-include-*")
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
	stubLogical, err := acfs.Root{Path: l.tempDir}.LogicalPath(stubPath)
	if err != nil {
		return "", fmt.Errorf("resolve validation include stub path: %w", err)
	}
	if mkdirAllErr := (acfs.Root{Path: l.tempDir}).MkdirAll(ctx, path.Dir(stubLogical), 0o755); mkdirAllErr != nil {
		return "", fmt.Errorf("create validation include directory: %w", mkdirAllErr)
	}
	if writeErr := (acfs.Root{Path: l.tempDir}).Write(ctx, stubLogical, []byte("services: {}\n"), acfs.WriteOptions{Mode: 0o600}); writeErr != nil {
		return "", fmt.Errorf("write validation include stub: %w", writeErr)
	}

	l.stubs[validatedPath] = stubPath
	return stubPath, nil
}

func (l *MissingIncludeStubLoader) Dir(localPath string) string {
	return filepath.Dir(localPath)
}

func (l *MissingIncludeStubLoader) resolveMissingInclude(localPath string) mo.Option[string] {
	validatedPath, err := ValidateIncludePathForWrite(l.projectPath, localPath)
	if err != nil {
		return mo.None[string]()
	}

	if _, statErr := (acfs.Host{}).Stat(l.ctx, validatedPath, acfstypes.StatOptions{FollowFinal: true}); statErr == nil {
		return mo.None[string]()
	} else if !errors.Is(statErr, os.ErrNotExist) {
		return mo.None[string]()
	}

	return mo.Some(validatedPath)
}

func (l *MissingIncludeStubLoader) Cleanup() {
	if l.tempDir != "" {
		_ = acfs.Host{}.RemoveAll(context.WithoutCancel(l.ctx), l.tempDir)
	}
}

func ParseIncludes(ctx context.Context, composeFilePath string, envMap EnvMap, includeContent bool) ([]IncludeFile, error) {
	content, err := acfs.Host{}.ReadFile(ctx, composeFilePath)
	if err != nil {
		return nil, fmt.Errorf("failed to read compose file: %w", err)
	}

	return ParseIncludesFromContent(ctx, composeFilePath, content, envMap, includeContent)
}

func ParseIncludesFromContent(ctx context.Context, composeFilePath string, content []byte, envMap EnvMap, includeContent bool) ([]IncludeFile, error) {
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
			incs, err := parseIncludeItem(ctx, item, composeDir, envMap, includeContent)
			if err != nil {
				return nil, err
			}
			includeFiles = append(includeFiles, incs...)
		}
	case string:
		incs, err := parseIncludeItem(ctx, v, composeDir, envMap, includeContent)
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

func parseIncludeItem(ctx context.Context, item any, baseDir string, envMap EnvMap, includeContent bool) ([]IncludeFile, error) {
	includePaths, err := extractIncludePaths(item)
	if err != nil {
		return nil, err
	}

	results := make([]IncludeFile, 0, len(includePaths))
	for _, includePath := range includePaths {
		inc, resolveIncludeFileErr := resolveIncludeFile(ctx, includePath, baseDir, envMap, includeContent)
		if resolveIncludeFileErr != nil {
			return nil, resolveIncludeFileErr
		}
		results = append(results, inc)
	}
	return results, nil
}

func extractIncludePaths(item any) ([]string, error) {
	switch v := item.(type) {
	case string:
		return []string{v}, nil
	case map[string]any:
		return extractIncludePathsFromMap(v)
	default:
		return nil, errors.New("invalid include item type")
	}
}

func extractIncludePathsFromMap(v map[string]any) ([]string, error) {
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

func resolveIncludeFile(ctx context.Context, includePath, baseDir string, envMap EnvMap, includeContent bool) (IncludeFile, error) {
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

	content, err := readIncludeContent(ctx, fullPath, includePath, includeContent)
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

func readIncludeContent(ctx context.Context, fullPath, includePath string, includeContent bool) (string, error) {
	if !includeContent {
		return "", nil
	}
	fileContent, err := acfs.Host{}.ReadFile(ctx, fullPath)
	if err == nil {
		return string(fileContent), nil
	}
	if errors.Is(err, os.ErrNotExist) {
		return "# This file will be created when you save changes\nservices:\n", nil
	}
	return "", fmt.Errorf("failed to read include file %s: %w", includePath, err)
}

func ValidateIncludePathForWrite(projectDir, includePath string) (string, error) {
	if includePath == "" {
		return "", errors.New("include path cannot be empty")
	}

	absProjectDir, err := filepath.Abs(projectDir)
	if err != nil {
		return "", fmt.Errorf("invalid project directory: %w", err)
	}
	absProjectDir = filepath.Clean(absProjectDir)

	if evalProjectDir, evalSymlinksErr := filepath.EvalSymlinks(absProjectDir); evalSymlinksErr == nil {
		absProjectDir = evalProjectDir
	}

	fullPath := includePath
	if !filepath.IsAbs(includePath) {
		fullPath = filepath.Join(absProjectDir, includePath)
	}

	absFullPath, err := filepath.Abs(fullPath)
	if err != nil {
		return "", fmt.Errorf("invalid include path: %w", err)
	}
	absFullPath = filepath.Clean(absFullPath)

	evalPath := absFullPath
	evalFullPath, err := filepath.EvalSymlinks(absFullPath)
	switch {
	case err == nil:
		evalPath = evalFullPath
	case !errors.Is(err, os.ErrNotExist):
		return "", fmt.Errorf("failed to resolve include path: %w", err)
	default:
		evalDir, dirErr := filepath.EvalSymlinks(filepath.Dir(absFullPath))
		if dirErr != nil && !errors.Is(dirErr, os.ErrNotExist) {
			return "", fmt.Errorf("failed to resolve parent directory: %w", dirErr)
		}
		if dirErr == nil {
			evalPath = filepath.Join(evalDir, filepath.Base(absFullPath))
		}
	}

	if evalPath == absProjectDir {
		return "", errors.New("include path cannot be the project directory itself")
	}

	projectPrefix := absProjectDir + string(filepath.Separator)
	isWithinProject := strings.HasPrefix(evalPath+string(filepath.Separator), projectPrefix)

	if !isWithinProject {
		return "", errors.New("write access denied: path is outside project directory")
	}

	return absFullPath, nil
}
