package projects

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"maps"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/compose-spec/compose-go/v2/dotenv"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/utils/cache"
)

const (
	GlobalEnvFileName    = ".env.global"
	EffectiveEnvFileName = ".env"
	GitSourceEnvFileName = ".env.git"
	OverrideEnvFileName  = "project.env"
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
	// The *Unreadable fields report a file that exists on disk but could not be
	// read because of a permission error (e.g. a chmod 000 or foreign-owned
	// file reachable through a bind mount). Such a file is treated as absent
	// for merge purposes, and callers persisting env state must not attempt to
	// write or remove it — its contents are unknown, so writing could either
	// fail (bricking the caller) or silently clobber operator intent.
	EffectiveUnreadable bool
	GitSourceUnreadable bool
	OverrideUnreadable  bool
}

type EnvLoader struct {
	projectsDir   string
	workdir       string
	autoInjectEnv bool
}

type envFileCacheEntry struct {
	path   string
	mtime  time.Time
	exists bool
	values EnvMap
}

var (
	processEnvOnce      sync.Once
	processEnvSnapshot  EnvMap
	globalEnvFileCache  = cache.NewKeyed[string, envFileCacheEntry]()
	projectEnvFileCache = cache.NewKeyed[string, envFileCacheEntry]()
)

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
	envMap = cloneEnvMapInternal(loadProcessEnvSnapshotInternal())
	injectionVars = make(EnvMap)

	if strings.TrimSpace(l.projectsDir) != "" {
		globalEnvPath := filepath.Join(l.projectsDir, GlobalEnvFileName)
		if err := l.loadAndMergeGlobalEnv(ctx, globalEnvPath, envMap, injectionVars); err != nil && !errors.Is(err, os.ErrNotExist) {
			slog.WarnContext(ctx, "Failed to load global env", "path", globalEnvPath, "error", err)
		}
	}

	projectEnvPath := filepath.Join(l.workdir, EffectiveEnvFileName)
	if err := l.loadAndMergeProjectEnv(ctx, projectEnvPath, envMap, injectionVars); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			slog.DebugContext(ctx, "Project .env file does not exist", "path", projectEnvPath)
		} else {
			slog.WarnContext(ctx, "Failed to load project env", "path", projectEnvPath, "error", err)
		}
	}

	return envMap, injectionVars, nil
}

func loadProcessEnvSnapshotInternal() EnvMap {
	processEnvOnce.Do(func() {
		processEnvSnapshot = make(EnvMap)
		for _, kv := range os.Environ() {
			if k, v, ok := strings.Cut(kv, "="); ok {
				processEnvSnapshot[k] = v
			}
		}
	})
	return processEnvSnapshot
}

func (l *EnvLoader) loadAndMergeGlobalEnv(ctx context.Context, path string, envMap, injectionVars EnvMap) error {
	entry, err := loadCachedEnvFileInternal(ctx, globalEnvFileCache, path, path, envMap)
	if err != nil {
		return err
	}
	if !entry.exists {
		return os.ErrNotExist
	}

	for k, v := range entry.values {
		if _, exists := envMap[k]; !exists {
			envMap[k] = v
		}
		injectionVars[k] = v
	}

	slog.DebugContext(ctx, "Merged global env into environment map", "total_env_count", len(envMap))
	return nil
}

func (l *EnvLoader) loadAndMergeProjectEnv(ctx context.Context, path string, envMap, injectionVars EnvMap) error {
	key := strings.Join([]string{path, l.projectsDir, strconv.FormatBool(l.autoInjectEnv), envContextFingerprintInternal(envMap)}, "\x00")
	entry, err := loadCachedEnvFileInternal(ctx, projectEnvFileCache, key, path, envMap)
	if err != nil {
		return err
	}
	if !entry.exists {
		return os.ErrNotExist
	}

	for k, v := range entry.values {
		envMap[k] = v
		if l.autoInjectEnv {
			injectionVars[k] = v
		}
	}

	slog.DebugContext(ctx, "Merged project .env into environment map", "total_env_count", len(envMap))
	return nil
}

func loadCachedEnvFileInternal(ctx context.Context, envCache *cache.KeyedCache[string, envFileCacheEntry], key, path string, contextEnv EnvMap) (envFileCacheEntry, error) {
	return envCache.GetOrFetch(ctx, key, validEnvFileCacheEntryInternal, func(context.Context) (envFileCacheEntry, error) {
		entry := envFileCacheEntry{path: path}
		info, err := os.Stat(path)
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				return entry, nil
			}
			return entry, err
		}
		if info.IsDir() {
			return entry, fmt.Errorf("path is a directory: %s", path)
		}

		parsed, err := parseProjectEnvFileExistingInternal(path, contextEnv)
		if err != nil {
			return entry, fmt.Errorf("parse env file: %w", err)
		}
		entry.exists = true
		entry.mtime = info.ModTime()
		entry.values = parsed
		return entry, nil
	})
}

func envContextFingerprintInternal(envMap EnvMap) string {
	if len(envMap) == 0 {
		return ""
	}
	keys := make([]string, 0, len(envMap))
	for key := range envMap {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	var b strings.Builder
	for _, key := range keys {
		b.WriteString(key)
		b.WriteByte('=')
		b.WriteString(envMap[key])
		b.WriteByte('\n')
	}
	sum := sha256.Sum256([]byte(b.String()))
	return hex.EncodeToString(sum[:])
}

func validEnvFileCacheEntryInternal(entry envFileCacheEntry) bool {
	info, err := os.Stat(entry.path)
	if err != nil {
		return !entry.exists && errors.Is(err, os.ErrNotExist)
	}
	if info.IsDir() {
		return false
	}
	return entry.exists && info.ModTime().Equal(entry.mtime)
}

func cloneEnvMapInternal(src EnvMap) EnvMap {
	dst := make(EnvMap, len(src))
	maps.Copy(dst, src)
	return dst
}

func parseProjectEnvFileExistingInternal(path string, contextEnv EnvMap) (EnvMap, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open file: %w", err)
	}
	defer func() { _ = f.Close() }()
	return parseEnvWithContext(f, contextEnv)
}

// ParseProjectEnvFile parses a project .env file with variable expansion using the provided
// context map (e.g. process env). Returns nil without error when the file does not exist.
// Only the specified file is read — global env files are intentionally not loaded here.
func ParseProjectEnvFile(path string, contextEnv EnvMap) (EnvMap, error) {
	if _, err := os.Stat(path); err != nil {
		return nil, nil //nolint:nilerr // missing .env is not an error
	}
	return parseProjectEnvFileExistingInternal(path, contextEnv)
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
	effectiveContent, hasEffective, effectiveUnreadable, err := readOptionalProjectFileInternal(projectPath, EffectiveEnvFileName)
	if err != nil {
		return ProjectEnvState{}, err
	}

	gitContent, hasGitSource, gitSourceUnreadable, err := readOptionalProjectFileInternal(projectPath, GitSourceEnvFileName)
	if err != nil {
		return ProjectEnvState{}, err
	}

	overrideContent, hasOverride, overrideUnreadable, err := readOptionalProjectFileInternal(projectPath, OverrideEnvFileName)
	if err != nil {
		return ProjectEnvState{}, err
	}

	if effectiveUnreadable || gitSourceUnreadable || overrideUnreadable {
		slog.Warn("skipping permission-locked project env file(s); leaving them untouched",
			"projectPath", projectPath,
			"effectiveUnreadable", effectiveUnreadable,
			"gitSourceUnreadable", gitSourceUnreadable,
			"overrideUnreadable", overrideUnreadable,
		)
	}

	state := ProjectEnvState{
		DirectContent:       effectiveContent,
		EffectiveContent:    effectiveContent,
		HasEffective:        hasEffective,
		EffectiveUnreadable: effectiveUnreadable,
		GitContent:          gitContent,
		HasGitSource:        hasGitSource,
		GitSourceUnreadable: gitSourceUnreadable,
		OverrideContent:     overrideContent,
		HasOverride:         hasOverride,
		OverrideUnreadable:  overrideUnreadable,
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

// WriteManagedEnvFile writes (or, for project.env, removes) one of the three
// env-merge bookkeeping files — fileName must be EffectiveEnvFileName,
// GitSourceEnvFileName, or OverrideEnvFileName. If the existing file is
// permission-locked, the write is skipped and a warning logged instead: its
// contents can't be verified, and a locked file is typically unwritable too,
// so attempting the write would abort the whole caller.
func WriteManagedEnvFile(projectsDirectory, projectPath, fileName string, unreadable bool, content string) error {
	if unreadable {
		slog.Warn("skipping permission-locked project env file; leaving it untouched", "projectPath", projectPath, "file", fileName)
		return nil
	}

	switch fileName {
	case EffectiveEnvFileName:
		return WriteEnvFile(projectsDirectory, projectPath, content)
	case GitSourceEnvFileName:
		return WriteProjectFile(projectsDirectory, projectPath, GitSourceEnvFileName, content)
	case OverrideEnvFileName:
		if strings.TrimSpace(content) == "" {
			return RemoveProjectFile(projectsDirectory, projectPath, OverrideEnvFileName)
		}
		return WriteProjectFile(projectsDirectory, projectPath, OverrideEnvFileName, content)
	default:
		return fmt.Errorf("write managed env file: unsupported file name %q", fileName)
	}
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

	return envMap, nil
}

// readOptionalProjectFileInternal reads fileName from projectPath. A missing
// file is reported via exists=false with no error. A permission error is
// reported via unreadable=true with no error: the file is present but its
// contents cannot be verified, so callers must treat it as absent for merge
// purposes and must not attempt to overwrite or remove it. Any other I/O
// error (e.g. the path is a directory) is still returned as a hard failure.
func readOptionalProjectFileInternal(projectPath, fileName string) (content string, exists, unreadable bool, err error) {
	raw, readErr := os.ReadFile(filepath.Join(projectPath, fileName))
	if readErr == nil {
		return string(raw), true, false, nil
	}
	if errors.Is(readErr, os.ErrNotExist) {
		return "", false, false, nil
	}
	if errors.Is(readErr, os.ErrPermission) {
		return "", false, true, nil
	}
	return "", false, false, fmt.Errorf("read %s: %w", fileName, readErr)
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
