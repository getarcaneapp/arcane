package projects

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"maps"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/compose-spec/compose-go/v2/dotenv"
	pkgutils "github.com/getarcaneapp/arcane/backend/pkg/utils"
)

const (
	GlobalEnvFileName    = ".env.global"
	EffectiveEnvFileName = ".env"
	GitSourceEnvFileName = ".env.git"
	OverrideEnvFileName  = "project.env"
	globalEnvHeader      = `# Global Environment Variables
# These variables are available to all projects
# Created: %s

`
)

type EnvMap = map[string]string

type ProjectEnvMode string

const (
	ProjectEnvModeDirect   ProjectEnvMode = "direct"
	ProjectEnvModeOverride ProjectEnvMode = "override"
)

type ProjectEnvState struct {
	Mode             ProjectEnvMode
	EditableFileName string
	EditableContent  string
	EffectiveContent string
	DirectContent    string
	HasEffective     bool
	GitContent       string
	HasGitSource     bool
	OverrideContent  string
	HasOverride      bool
}

type EnvLoader struct {
	projectsDir   string
	workdir       string
	autoInjectEnv bool
}

func NewEnvLoader(projectsDir, workdir string, autoInjectEnv bool) *EnvLoader {
	return &EnvLoader{
		projectsDir:   projectsDir,
		workdir:       workdir,
		autoInjectEnv: autoInjectEnv,
	}
}

// LoadEnvironment loads and merges environment variables from all sources:
// 1. Process environment
// 2. Global .env.global file (from projects directory)
// 3. Project-specific .env file (from workdir)
func (l *EnvLoader) LoadEnvironment(ctx context.Context) (envMap EnvMap, injectionVars EnvMap, err error) {
	envMap = l.loadProcessEnv()
	injectionVars = make(EnvMap)

	globalEnvPath := filepath.Join(l.projectsDir, GlobalEnvFileName)
	if err := l.ensureGlobalEnvFile(ctx, globalEnvPath); err != nil {
		slog.WarnContext(ctx, "Failed to ensure global env file", "path", globalEnvPath, "error", err)
	}

	if err := l.loadAndMergeGlobalEnv(ctx, globalEnvPath, envMap, injectionVars); err != nil {
		slog.WarnContext(ctx, "Failed to load global env", "path", globalEnvPath, "error", err)
	}

	projectEnvPath := filepath.Join(l.workdir, EffectiveEnvFileName)
	if err := l.loadAndMergeProjectEnv(ctx, projectEnvPath, envMap, injectionVars); err != nil {
		slog.WarnContext(ctx, "Failed to load project env", "path", projectEnvPath, "error", err)
	}

	return envMap, injectionVars, nil
}

func (l *EnvLoader) loadProcessEnv() EnvMap {
	envMap := make(EnvMap)
	for _, kv := range os.Environ() {
		if k, v, ok := strings.Cut(kv, "="); ok {
			envMap[k] = v
		}
	}
	return envMap
}

func (l *EnvLoader) ensureGlobalEnvFile(ctx context.Context, path string) error {
	if _, err := os.Stat(path); errors.Is(err, os.ErrNotExist) {
		header := fmt.Sprintf(globalEnvHeader, time.Now().Format(time.RFC3339))
		dir := filepath.Dir(path)
		if err := os.MkdirAll(dir, pkgutils.DirPerm); err != nil {
			return fmt.Errorf("create dir: %w", err)
		}
		if err := os.WriteFile(path, []byte(header), pkgutils.FilePerm); err != nil {
			return fmt.Errorf("write file: %w", err)
		}
		slog.InfoContext(ctx, "Created global env file", "path", path)
	} else if err != nil {
		slog.DebugContext(ctx, "Could not stat global env file", "path", path, "error", err)
	}
	return nil
}

func (l *EnvLoader) loadAndMergeGlobalEnv(ctx context.Context, path string, envMap, injectionVars EnvMap) error {
	slog.DebugContext(ctx, "Checking for global env file", "path", path)

	info, err := os.Stat(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			slog.DebugContext(ctx, "Global env file does not exist", "path", path)
		} else {
			slog.DebugContext(ctx, "Global env file not accessible", "path", path, "error", err)
		}
		return err
	}

	if info.IsDir() {
		return fmt.Errorf("path is a directory: %s", path)
	}

	globalEnv, err := ParseProjectEnvFile(path, envMap)
	if err != nil {
		return fmt.Errorf("parse env file: %w", err)
	}

	for k, v := range globalEnv {
		if _, exists := envMap[k]; !exists {
			envMap[k] = v
		}
		injectionVars[k] = v
	}

	slog.DebugContext(ctx, "Merged global env into environment map", "total_env_count", len(envMap))
	return nil
}

func (l *EnvLoader) loadAndMergeProjectEnv(ctx context.Context, path string, envMap, injectionVars EnvMap) error {
	slog.DebugContext(ctx, "Checking for project .env file", "path", path)

	info, err := os.Stat(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			slog.DebugContext(ctx, "Project .env file does not exist", "path", path)
		} else {
			slog.DebugContext(ctx, "Project .env file not accessible", "path", path, "error", err)
		}
		return err
	}

	if info.IsDir() {
		return fmt.Errorf("path is a directory: %s", path)
	}

	projectEnv, err := ParseProjectEnvFile(path, envMap)
	if err != nil {
		return fmt.Errorf("parse env file: %w", err)
	}

	for k, v := range projectEnv {
		envMap[k] = v
		if l.autoInjectEnv {
			injectionVars[k] = v
		}
	}

	slog.DebugContext(ctx, "Merged project .env into environment map", "total_env_count", len(envMap))
	return nil
}

// ParseProjectEnvFile parses a project .env file with variable expansion using the provided
// context map (e.g. process env). Returns nil without error when the file does not exist.
// Only the specified file is read — global env files are intentionally not loaded here.
func ParseProjectEnvFile(path string, contextEnv EnvMap) (EnvMap, error) {
	if _, err := os.Stat(path); err != nil {
		return nil, nil //nolint:nilerr // missing .env is not an error
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open file: %w", err)
	}
	defer func() { _ = f.Close() }()
	return parseEnvWithContext(f, contextEnv)
}

// ParseProjectEnvContent parses project .env content from a string with variable expansion.
func ParseProjectEnvContent(content string, contextEnv EnvMap) (EnvMap, error) {
	return parseEnvWithContext(strings.NewReader(content), contextEnv)
}

// BuildEffectiveEnvContent merges git and override env sources into the effective
// .env content written to disk. The output is normalized: comments are dropped,
// keys are sorted, and values are rewritten with Arcane's formatter.
func BuildEffectiveEnvContent(gitContent, overrideContent string) (string, error) {
	contextEnv := make(EnvMap)

	gitEnv, err := ParseProjectEnvContent(gitContent, contextEnv)
	if err != nil {
		return "", fmt.Errorf("parse git env content: %w", err)
	}
	maps.Copy(contextEnv, gitEnv)

	overrideEnv, err := ParseProjectEnvContent(overrideContent, contextEnv)
	if err != nil {
		return "", fmt.Errorf("parse override env content: %w", err)
	}

	merged := make(EnvMap, len(gitEnv)+len(overrideEnv))
	maps.Copy(merged, gitEnv)
	maps.Copy(merged, overrideEnv)

	return formatEnvMapInternal(merged), nil
}

// BuildOverrideEnvContent derives the editable override file from git-backed and
// effective env content. The generated output is normalized and does not retain
// comments or original key ordering.
func BuildOverrideEnvContent(gitContent, effectiveContent string) (string, error) {
	return buildOverrideEnvContentInternal(gitContent, effectiveContent)
}

// BuildAdditiveOverrideEnvContent derives override content from a pre-git local
// .env file. Like other generated env helpers, the result is normalized and does
// not preserve comments or original key ordering.
func BuildAdditiveOverrideEnvContent(gitContent, localContent string) (string, error) {
	contextEnv := make(EnvMap)

	gitEnv, err := ParseProjectEnvContent(gitContent, contextEnv)
	if err != nil {
		return "", fmt.Errorf("parse git env content: %w", err)
	}
	maps.Copy(contextEnv, gitEnv)

	localEnv, err := ParseProjectEnvContent(localContent, contextEnv)
	if err != nil {
		return "", fmt.Errorf("parse local env content: %w", err)
	}

	override := make(EnvMap)
	for key, value := range localEnv {
		if _, exists := gitEnv[key]; !exists {
			override[key] = value
		}
	}

	return formatEnvMapInternal(override), nil
}

func buildOverrideEnvContentInternal(gitContent, effectiveContent string) (string, error) {
	contextEnv := make(EnvMap)

	gitEnv, err := ParseProjectEnvContent(gitContent, contextEnv)
	if err != nil {
		return "", fmt.Errorf("parse git env content: %w", err)
	}
	maps.Copy(contextEnv, gitEnv)

	effectiveEnv, err := ParseProjectEnvContent(effectiveContent, contextEnv)
	if err != nil {
		return "", fmt.Errorf("parse effective env content: %w", err)
	}

	override := make(EnvMap)
	for key, value := range effectiveEnv {
		gitValue, exists := gitEnv[key]
		switch {
		case !exists:
			override[key] = value
		case value == "":
			// Empty values for Git-backed keys are treated as deleting the local override,
			// so the Git value is restored on the next effective merge.
			continue
		case gitValue != value:
			override[key] = value
		}
	}

	return formatEnvMapInternal(override), nil
}

func ReadProjectEnvState(projectPath string) (ProjectEnvState, error) {
	effectiveContent, hasEffective, err := readOptionalProjectFileInternal(projectPath, EffectiveEnvFileName)
	if err != nil {
		return ProjectEnvState{}, err
	}

	gitContent, hasGitSource, err := readOptionalProjectFileInternal(projectPath, GitSourceEnvFileName)
	if err != nil {
		return ProjectEnvState{}, err
	}

	overrideContent, hasOverride, err := readOptionalProjectFileInternal(projectPath, OverrideEnvFileName)
	if err != nil {
		return ProjectEnvState{}, err
	}

	state := ProjectEnvState{
		DirectContent:    effectiveContent,
		EffectiveContent: effectiveContent,
		HasEffective:     hasEffective,
		GitContent:       gitContent,
		HasGitSource:     hasGitSource,
		OverrideContent:  overrideContent,
		HasOverride:      hasOverride,
	}

	if hasGitSource || hasOverride {
		state.Mode = ProjectEnvModeOverride
		state.EditableFileName = OverrideEnvFileName
		state.EditableContent = overrideContent

		if !hasEffective {
			mergedContent, mergeErr := BuildEffectiveEnvContent(gitContent, overrideContent)
			if mergeErr != nil {
				return ProjectEnvState{}, mergeErr
			}
			state.EffectiveContent = mergedContent
		}

		return state, nil
	}

	state.Mode = ProjectEnvModeDirect
	state.EditableFileName = EffectiveEnvFileName
	state.EditableContent = effectiveContent

	return state, nil
}

// parseEnvWithContext parses environment variables from an io.Reader using compose-go's
// dotenv parser with variable expansion using the provided context lookup map.
func parseEnvWithContext(r io.Reader, contextEnv EnvMap) (EnvMap, error) {
	// Create lookup function for variable expansion
	// Checks contextEnv first (previously loaded vars), then process environment
	lookupFn := func(key string) (string, bool) {
		if val, ok := contextEnv[key]; ok {
			return val, true
		}
		return os.LookupEnv(key)
	}

	// Use compose-go's dotenv parser with lookup support for variable expansion
	envMap, err := dotenv.ParseWithLookup(r, lookupFn)
	if err != nil {
		return nil, fmt.Errorf("parse env: %w", err)
	}

	return EnvMap(envMap), nil
}

func readOptionalProjectFileInternal(projectPath, fileName string) (string, bool, error) {
	content, err := os.ReadFile(filepath.Join(projectPath, fileName))
	if err == nil {
		return string(content), true, nil
	}
	if errors.Is(err, os.ErrNotExist) {
		return "", false, nil
	}
	return "", false, fmt.Errorf("read %s: %w", fileName, err)
}

// formatEnvMapInternal serializes env maps into Arcane's canonical generated
// format. This is intentionally lossy: comments are omitted and keys are sorted
// alphabetically to keep persisted merge output stable.
func formatEnvMapInternal(envMap EnvMap) string {
	if len(envMap) == 0 {
		return ""
	}

	keys := make([]string, 0, len(envMap))
	for key := range envMap {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	var builder strings.Builder
	for _, key := range keys {
		builder.WriteString(key)
		builder.WriteByte('=')
		builder.WriteString(formatEnvValueInternal(envMap[key]))
		builder.WriteByte('\n')
	}

	return builder.String()
}

func formatEnvValueInternal(value string) string {
	if value == "" {
		return value
	}

	needsQuotes := strings.ContainsAny(value, " \t\r\n#\"'") || strings.TrimSpace(value) != value
	if !needsQuotes {
		return value
	}

	escaped := strings.NewReplacer(
		"\\", "\\\\",
		`"`, `\"`,
		"\t", `\t`,
		"\n", `\n`,
		"\r", `\r`,
	).Replace(value)

	return `"` + escaped + `"`
}
