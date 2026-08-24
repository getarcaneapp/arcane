package projects

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"log/slog"
	"maps"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"emperror.dev/errors"

	"github.com/compose-spec/compose-go/v2/consts"
	"github.com/compose-spec/compose-go/v2/dotenv"
	"github.com/samber/hot"
	"go.getarcane.app/acfs"
)

const (
	GlobalEnvFileName                     = ".env.global"
	EffectiveEnvFileName                  = ".env"
	GitSourceEnvFileName                  = ".env.git"
	OverrideEnvFileName                   = "project.env"
	ProjectEnvModeDirect   ProjectEnvMode = "direct"
	ProjectEnvModeOverride ProjectEnvMode = "override"
)

type EnvMap = map[string]string

type ProjectEnvMode string

type ProjectEnvState struct {
	Mode             ProjectEnvMode
	EditableFileName string
	EditableContent  string
	EffectiveContent string
	DirectContent    string
	GitContent       string
	OverrideContent  string
	HasEffective     bool
	HasGitSource     bool
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
	globalEnvFileCache  = hot.NewHotCache[string, envFileCacheEntry](hot.LRU, 4096).Build()
	projectEnvFileCache = hot.NewHotCache[string, envFileCacheEntry](hot.LRU, 4096).Build()
)

func NewEnvLoader(projectsDir, workdir string, autoInjectEnv bool) *EnvLoader {
	return &EnvLoader{
		projectsDir:   projectsDir,
		workdir:       workdir,
		autoInjectEnv: autoInjectEnv,
	}
}

// processEnvAllowlist is the only part of Arcane's own process environment
// that flows into compose interpolation of managed projects: timezone and
// locale, whose container values are safe to share. Everything else is
// excluded so Arcane's variables never leak into ${VAR} references or
// pass-through environment entries — its PORT collides with project port
// mappings, secrets would be readable from any compose file, and vars like
// HOME or PUID carry container-internal values that are wrong for projects.
var processEnvAllowlist = []string{"TZ", "LANG", "LANGUAGE", "LC_ALL"}

func allowedProcessEnvInternal() EnvMap {
	envMap := make(EnvMap)
	for _, key := range processEnvAllowlist {
		if val, ok := os.LookupEnv(key); ok {
			envMap[key] = val
		}
	}
	return envMap
}

// LoadEnvironment loads and merges environment variables from all sources:
// 1. Allowlisted process environment (TZ)
// 2. Global .env.global file (from projects directory)
// 3. Project-specific .env file (from workdir)
// The rest of the Arcane process environment is intentionally excluded so its
// own variables never leak into compose interpolation of managed projects.
func (l *EnvLoader) LoadEnvironment(ctx context.Context) (envMap EnvMap, injectionVars EnvMap, err error) {
	envMap = allowedProcessEnvInternal()
	injectionVars = make(EnvMap)

	if strings.TrimSpace(l.projectsDir) != "" {
		globalEnvPath := filepath.Join(l.projectsDir, GlobalEnvFileName)
		if err := l.loadAndMergeGlobalEnv(ctx, globalEnvPath, envMap, injectionVars); err != nil && !errors.Is(err, os.ErrNotExist) {
			slog.WarnContext(ctx, "Failed to load global env", "path", globalEnvPath, "error", err)
		}
	}

	// COMPOSE_DISABLE_ENV_FILE skips the project .env entirely, as `docker
	// compose` does. It is read from the sources merged so far — in practice
	// only .env.global, since the process-env allowlist never includes
	// COMPOSE_* variables.
	if parseComposeBoolInternal(envMap, consts.ComposeDisableDefaultEnvFile) {
		slog.DebugContext(ctx, "COMPOSE_DISABLE_ENV_FILE set; skipping project .env", "workdir", l.workdir)
	} else {
		projectEnvPath := filepath.Join(l.workdir, EffectiveEnvFileName)
		if err := l.loadAndMergeProjectEnv(ctx, projectEnvPath, envMap, injectionVars); err != nil {
			if errors.Is(err, os.ErrNotExist) {
				slog.DebugContext(ctx, "Project .env file does not exist", "path", projectEnvPath)
			} else {
				slog.WarnContext(ctx, "Failed to load project env", "path", projectEnvPath, "error", err)
			}
		}
	}

	// COMPOSE_ENV_FILES, when declared, layers additional env files on top of
	// the project .env (later entries win). Reuses the mtime-keyed cache.
	l.mergeComposeEnvFilesInternal(ctx, envMap, injectionVars)

	return envMap, injectionVars, nil
}

func (l *EnvLoader) mergeComposeEnvFilesInternal(ctx context.Context, envMap, injectionVars EnvMap) {
	parse := func(path string, contextEnv EnvMap) (EnvMap, error) {
		key := strings.Join([]string{path, l.projectsDir, strconv.FormatBool(l.autoInjectEnv), envContextFingerprintInternal(contextEnv)}, "\x00")
		entry, err := loadCachedEnvFileInternal(ctx, projectEnvFileCache, key, path, contextEnv)
		if err != nil {
			return nil, err
		}
		if !entry.exists {
			return nil, nil
		}
		return entry.values, nil
	}
	onMerged := func(values EnvMap) {
		if l.autoInjectEnv {
			maps.Copy(injectionVars, values)
		}
	}
	mergeComposeEnvFilesInternal(ctx, l.workdir, envMap, parse, onMerged)
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
		envMap[k] = v
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

func loadCachedEnvFileInternal(_ context.Context, envCache *hot.HotCache[string, envFileCacheEntry], key, path string, contextEnv EnvMap) (envFileCacheEntry, error) {
	if cached, ok := envCache.Peek(key); ok {
		if validEnvFileCacheEntryInternal(cached) {
			return cached, nil
		}
		envCache.Delete(key)
	}

	entry, found, err := envCache.GetWithLoaders(key, func(_ []string) (map[string]envFileCacheEntry, error) {
		entry := envFileCacheEntry{path: path}
		// Stays on os.*: env files may be symlinks resolving outside any
		// confinement root (a supported setup), which acfs cannot follow.
		info, err := os.Stat(path)
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				return map[string]envFileCacheEntry{key: entry}, nil
			}
			return nil, err
		}
		if info.IsDir() {
			return nil, errors.Errorf("path is a directory: %s", path)
		}

		parsed, err := parseProjectEnvFileExistingInternal(path, contextEnv)
		if err != nil {
			return nil, errors.WrapIf(err, "parse env file")
		}
		entry.exists = true
		entry.mtime = info.ModTime()
		entry.values = parsed
		return map[string]envFileCacheEntry{key: entry}, nil
	})
	if err != nil {
		return envFileCacheEntry{}, err
	}
	if !found {
		return envFileCacheEntry{}, errors.New("environment file cache loader returned no entry")
	}
	return entry, nil
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
	// os.Stat rather than acfs: env files may be symlinks resolving outside
	// any confinement root (a supported setup).
	info, err := os.Stat(entry.path)
	if err != nil {
		return !entry.exists && errors.Is(err, os.ErrNotExist)
	}
	if info.IsDir() {
		return false
	}
	return entry.exists && info.ModTime().Equal(entry.mtime)
}

func parseProjectEnvFileExistingInternal(path string, contextEnv EnvMap) (EnvMap, error) {
	content, err := os.ReadFile(path)
	if err != nil {
		return nil, errors.WrapIf(err, "read file")
	}
	return ParseProjectEnvContent(string(content), contextEnv)
}

// ParseProjectEnvFile parses a project .env file with variable expansion using the provided
// context map (e.g. process env). Returns nil without error when the file does not exist.
// Only the specified file is read — global env files are intentionally not loaded here.
//
// Stays on os.*: env files may be symlinks resolving outside any confinement
// root (a supported setup), which acfs cannot follow.
func ParseProjectEnvFile(path string, contextEnv EnvMap) (EnvMap, error) {
	if _, err := os.Stat(path); err != nil {
		return nil, nil //nolint:nilerr // missing .env is not an error
	}
	return parseProjectEnvFileExistingInternal(path, contextEnv)
}

// ParseProjectEnvContent parses project .env content from a string using
// compose-go's dotenv parser with variable expansion. Lookups resolve from
// contextEnv (previously loaded vars) only; the Arcane process environment is
// intentionally never consulted so its variables don't leak into project env.
func ParseProjectEnvContent(content string, contextEnv EnvMap) (EnvMap, error) {
	lookupFn := func(key string) (string, bool) {
		val, ok := contextEnv[key]
		return val, ok
	}

	envMap, err := dotenv.ParseWithLookup(strings.NewReader(content), lookupFn)
	if err != nil {
		return nil, errors.WrapIf(err, "parse env")
	}

	return envMap, nil
}

// WithTransientValidationEnvFile temporarily writes a project .env file while
// running compose validation, then restores the original file state.
func WithTransientValidationEnvFile(ctx context.Context, projectPath string, effectiveEnvContent *string, run func() error) (err error) {
	// Read through os rather than the root-confined API: a project .env is
	// allowed to be a symlink whose target lives outside the project directory,
	// and that write-through is deliberately preserved (#3556).
	originalContent, readErr := os.ReadFile(filepath.Join(projectPath, ".env"))
	originalExists := readErr == nil
	if readErr != nil && !os.IsNotExist(readErr) {
		if !errors.Is(readErr, os.ErrPermission) {
			return errors.WrapIf(readErr, "prepare env file for compose validation")
		}
		// The file exists but is permission-locked (e.g. chmod 000, foreign-owned).
		// Its contents can't be verified or safely overwritten, so leave it
		// untouched and validate against whatever's already on disk instead of
		// aborting the whole update.
		slog.Warn("skipping permission-locked .env file during compose validation; leaving it untouched", "projectPath", projectPath)
		if run == nil {
			return nil
		}
		return run()
	}

	contentMatches := effectiveEnvContent != nil && originalExists && string(originalContent) == *effectiveEnvContent
	shouldWrite := !contentMatches && (effectiveEnvContent != nil || !originalExists)
	if shouldWrite {
		content := ""
		if effectiveEnvContent != nil {
			content = *effectiveEnvContent
		}
		if writeErr := WriteProjectFile(ctx, projectPath, projectPath, ".env", content); writeErr != nil {
			return errors.WrapIf(writeErr, "prepare env file for compose validation")
		}

		defer func() {
			var restoreErr error
			switch {
			case originalExists:
				restoreErr = WriteProjectFile(ctx, projectPath, projectPath, ".env", string(originalContent))
			default:
				restoreErr = acfs.Remove(ctx, projectPath, "/.env")
			}

			if restoreErr != nil && !os.IsNotExist(restoreErr) {
				if err == nil {
					err = errors.WrapIf(restoreErr, "restore env file after compose validation")
				}
			}
		}()
	}

	if run == nil {
		return nil
	}

	return run()
}

// BuildEffectiveEnvContent merges git and override env sources into the effective
// .env content written to disk. Keys present in both layers are rewritten in place
// on the Git line, preserving ordering and inline comments; override-only keys are
// appended after the Git content. When the in-place rewrite cannot be verified to
// parse identically to plain concatenation (e.g. multiline values), the override
// is appended verbatim instead so duplicate keys resolve to the override value.
func BuildEffectiveEnvContent(gitContent, overrideContent string) (string, error) {
	contextEnv := make(EnvMap)

	gitEnv, err := ParseProjectEnvContent(gitContent, contextEnv)
	if err != nil {
		return "", errors.WrapIf(err, "parse git env content")
	}
	maps.Copy(contextEnv, gitEnv)

	overrideEnv, err := ParseProjectEnvContent(overrideContent, contextEnv)
	if err != nil {
		return "", errors.WrapIf(err, "parse override env content")
	}

	switch {
	case gitContent == "":
		return overrideContent, nil
	case overrideContent == "":
		return gitContent, nil
	}

	concatenated := gitContent + "\n" + overrideContent
	if strings.HasSuffix(gitContent, "\n") || strings.HasPrefix(overrideContent, "\n") {
		concatenated = gitContent + overrideContent
	}

	candidate, ok := mergeEnvOverridesInPlaceInternal(gitContent, overrideContent, overrideEnv)
	if !ok {
		return concatenated, nil
	}

	candidateEnv, candidateErr := ParseProjectEnvContent(candidate, make(EnvMap))
	expectedEnv, expectedErr := ParseProjectEnvContent(concatenated, make(EnvMap))
	if candidateErr == nil && expectedErr == nil && maps.Equal(candidateEnv, expectedEnv) {
		return candidate, nil
	}

	return concatenated, nil
}

var envKeyLineRegexInternal = regexp.MustCompile(`^(\s*(?:export\s+)?)([A-Za-z_][A-Za-z0-9_.-]*)(\s*=)(.*)$`)

// mergeEnvOverridesInPlaceInternal rewrites the value of every gitContent line
// whose key has an override — keeping line order and trailing inline comments —
// then appends the override lines whose keys were not rewritten. ok is false when
// no git line matched an override and the caller should fall back to plain
// concatenation.
func mergeEnvOverridesInPlaceInternal(gitContent, overrideContent string, overrideEnv EnvMap) (merged string, ok bool) {
	rewritten := make(map[string]struct{})
	gitLines := strings.Split(gitContent, "\n")

	for i, line := range gitLines {
		body, hadCR := strings.CutSuffix(line, "\r")
		match := envKeyLineRegexInternal.FindStringSubmatch(body)
		if match == nil {
			continue
		}
		overrideValue, exists := overrideEnv[match[2]]
		if !exists {
			continue
		}

		value, comment := splitEnvValueCommentInternal(match[4])
		separator := value[len(strings.TrimRight(value, " \t")):]
		if comment != "" && separator == "" {
			separator = " "
		}

		body = match[1] + match[2] + match[3] + formatEnvValueInternal(overrideValue) + separator + comment
		if hadCR {
			body += "\r"
		}
		gitLines[i] = body
		rewritten[match[2]] = struct{}{}
	}

	if len(rewritten) == 0 {
		return "", false
	}

	remainderLines := make([]string, 0)
	for line := range strings.SplitSeq(overrideContent, "\n") {
		if match := envKeyLineRegexInternal.FindStringSubmatch(strings.TrimSuffix(line, "\r")); match != nil {
			if _, drop := rewritten[match[2]]; drop {
				continue
			}
		}
		remainderLines = append(remainderLines, line)
	}

	merged = strings.Join(gitLines, "\n")
	remainder := strings.Join(remainderLines, "\n")
	switch {
	case strings.TrimSpace(remainder) == "":
		return merged, true
	case strings.HasSuffix(merged, "\n"), strings.HasPrefix(remainder, "\n"):
		return merged + remainder, true
	default:
		return merged + "\n" + remainder, true
	}
}

// splitEnvValueCommentInternal splits a raw single-line env value into the value
// part and a trailing inline comment. A comment starts at an unquoted '#' that is
// at the start of the value or preceded by whitespace.
func splitEnvValueCommentInternal(raw string) (value, comment string) {
	var quote byte
	for i := 0; i < len(raw); i++ {
		c := raw[i]
		switch {
		case quote != 0:
			if c == quote {
				quote = 0
			} else if c == '\\' && quote == '"' {
				i++
			}
		case c == '\'' || c == '"':
			quote = c
		case c == '#' && (i == 0 || raw[i-1] == ' ' || raw[i-1] == '\t'):
			return raw[:i], raw[i:]
		}
	}
	return raw, ""
}

// BuildAdditiveOverrideEnvContent derives override content from a pre-git local
// .env file. Like other generated env helpers, the result is normalized and does
// not preserve comments or original key ordering.
func BuildAdditiveOverrideEnvContent(gitContent, localContent string) (string, error) {
	contextEnv := make(EnvMap)

	gitEnv, err := ParseProjectEnvContent(gitContent, contextEnv)
	if err != nil {
		return "", errors.WrapIf(err, "parse git env content")
	}
	maps.Copy(contextEnv, gitEnv)

	localEnv, err := ParseProjectEnvContent(localContent, contextEnv)
	if err != nil {
		return "", errors.WrapIf(err, "parse local env content")
	}

	override := make(EnvMap)
	for key, value := range localEnv {
		if _, exists := gitEnv[key]; !exists {
			override[key] = value
		}
	}

	return formatEnvMapInternal(override), nil
}

// BuildOverrideEnvContent derives the editable override file from git-backed and
// effective env content. Content that already contains only real overrides is
// returned verbatim; derived or cleaned output uses Arcane's canonical format.
func BuildOverrideEnvContent(gitContent, effectiveContent string) (string, error) {
	contextEnv := make(EnvMap)

	gitEnv, err := ParseProjectEnvContent(gitContent, contextEnv)
	if err != nil {
		return "", errors.WrapIf(err, "parse git env content")
	}
	maps.Copy(contextEnv, gitEnv)

	effectiveEnv, err := ParseProjectEnvContent(effectiveContent, contextEnv)
	if err != nil {
		return "", errors.WrapIf(err, "parse effective env content")
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

	if maps.Equal(effectiveEnv, override) {
		return effectiveContent, nil
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
func WriteManagedEnvFile(ctx context.Context, projectsDirectory, projectPath, fileName string, unreadable bool, content string) error {
	if unreadable {
		slog.Warn("skipping permission-locked project env file; leaving it untouched", "projectPath", projectPath, "file", fileName)
		return nil
	}

	switch fileName {
	case EffectiveEnvFileName:
		return WriteProjectFile(ctx, projectsDirectory, projectPath, ".env", content)
	case GitSourceEnvFileName:
		return WriteProjectFile(ctx, projectsDirectory, projectPath, GitSourceEnvFileName, content)
	case OverrideEnvFileName:
		if strings.TrimSpace(content) == "" {
			return RemoveProjectFile(ctx, projectsDirectory, projectPath, OverrideEnvFileName)
		}
		return WriteProjectFile(ctx, projectsDirectory, projectPath, OverrideEnvFileName, content)
	default:
		return errors.Errorf("write managed env file: unsupported file name %q", fileName)
	}
}

// readOptionalProjectFileInternal reads fileName from projectPath. A missing
// file is reported via exists=false with no error. A permission error is
// reported via unreadable=true with no error: the file is present but its
// contents cannot be verified, so callers must treat it as absent for merge
// purposes and must not attempt to overwrite or remove it. Any other I/O
// error (e.g. the path is a directory) is still returned as a hard failure.
// A project env file may itself be a symlink whose target lives outside the
// project directory, so the read goes through os rather than the root-confined
// API — the same deliberate exception the .env write path makes (#3556).
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
	return "", false, false, errors.WrapIff(readErr, "read %s", fileName)
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

	value = strings.ReplaceAll(value, "$", "$$")
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
