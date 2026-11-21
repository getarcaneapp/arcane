package projects

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"maps"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/compose-spec/compose-go/v2/consts"
	"github.com/compose-spec/compose-go/v2/dotenv"

	"kit.getarcane.app"
	"kit.getarcane.app/acfs"
	acfstypes "kit.getarcane.app/acfs/types"
	"kit.getarcane.app/cache"

	"getarcane.dev/arcane/v3/internal/common"
	"getarcane.dev/types/v3"
)

type (
	EnvMap = map[string]string

	ProjectEnvMode string

	ProjectEnvState struct {
		Mode                ProjectEnvMode
		EditableFileName    string
		EditableContent     string
		EffectiveContent    string
		DirectContent       string
		GitContent          string
		OverrideContent     string
		HasEffective        bool
		HasGitSource        bool
		HasOverride         bool
		EffectiveUnreadable bool
		GitSourceUnreadable bool
		OverrideUnreadable  bool
	}

	EnvLoader struct {
		projectsDir   string
		workdir       string
		autoInjectEnv bool
	}

	envFileCacheEntry struct {
		mtime        time.Time
		values       EnvMap
		noInjectKeys map[string]bool
		path         string
		size         int64
		exists       bool
	}

	ComposeEnvOptions struct {
		ConfigFiles   []string
		Profiles      []string
		ProjectName   string
		EnvFiles      []string
		RemoveOrphans bool
		IgnoreOrphans bool
		ParallelLimit int
	}
)

const (
	GlobalEnvFileName                         = ".env.global"
	EffectiveEnvFileName                      = ".env"
	GitSourceEnvFileName                      = ".env.git"
	OverrideEnvFileName                       = "project.env"
	ProjectEnvModeDirect       ProjectEnvMode = "direct"
	ProjectEnvModeOverride     ProjectEnvMode = "override"
	GlobalEnvNoInjectDirective                = "arcane:noinject"
	// Docker Compose pre-defined variable names that compose-go's consts package
	// does not export. See https://docs.docker.com/compose/how-tos/environment-variables/envvars.
	composeEnvFilesKey          = "COMPOSE_ENV_FILES"
	composeRemoveOrphansKey     = "COMPOSE_REMOVE_ORPHANS"
	composeIgnoreOrphansKey     = "COMPOSE_IGNORE_ORPHANS"
	composeParallelLimitKey     = "COMPOSE_PARALLEL_LIMIT"
	defaultComposePathSeparator = ":"
)

var (
	// ReservedRootEnvFiles are the project-root env files the override merge owns; git never writes them raw.
	ReservedRootEnvFiles = []string{EffectiveEnvFileName, GitSourceEnvFileName, OverrideEnvFileName, GlobalEnvFileName}

	globalEnvFileCache = &cache.Cache[string, envFileCacheEntry]{
		Capacity: 4096,
	}
	projectEnvFileCache = &cache.Cache[string, envFileCacheEntry]{
		Capacity: 4096,
	}
	processEnvAllowlist = []string{"TZ", "LANG", "LANGUAGE", "LC_ALL"}

	envKeyLineRegex = regexp.MustCompile(`^(\s*(?:export\s+)?)([A-Za-z_][A-Za-z0-9_.-]*)(\s*=)(.*)$`)
)

func NewEnvLoader(projectsDir, workdir string, autoInjectEnv bool) *EnvLoader {
	return &EnvLoader{
		projectsDir:   projectsDir,
		workdir:       workdir,
		autoInjectEnv: autoInjectEnv,
	}
}

func allowedProcessEnv() EnvMap {
	envMap := make(EnvMap)
	for _, key := range processEnvAllowlist {
		if val, ok := os.LookupEnv(key); ok {
			envMap[key] = val
		}
	}
	return envMap
}

func (l *EnvLoader) LoadEnvironment(ctx context.Context) (envMap, injectionVars EnvMap, err error) {
	envMap = allowedProcessEnv()
	injectionVars = make(EnvMap)

	if strings.TrimSpace(l.projectsDir) != "" {
		globalEnvPath := filepath.Join(l.projectsDir, GlobalEnvFileName)
		if loadAndMergeGlobalEnvErr := l.loadAndMergeGlobalEnv(ctx, globalEnvPath, envMap, injectionVars); loadAndMergeGlobalEnvErr != nil && !errors.Is(loadAndMergeGlobalEnvErr, os.ErrNotExist) {
			slog.WarnContext(ctx, "Failed to load global env", "path", globalEnvPath, "error", loadAndMergeGlobalEnvErr)
		}
	}

	// COMPOSE_DISABLE_ENV_FILE skips the project .env entirely, as `docker
	// compose` does. It is read from the sources merged so far — in practice
	// only .env.global, since the process-env allowlist never includes
	// COMPOSE_* variables.
	if parseComposeBool(envMap, consts.ComposeDisableDefaultEnvFile) {
		slog.DebugContext(ctx, "COMPOSE_DISABLE_ENV_FILE set; skipping project .env", "workdir", l.workdir)
	} else {
		projectEnvPath := filepath.Join(l.workdir, EffectiveEnvFileName)
		if loadAndMergeProjectEnvErr := l.loadAndMergeProjectEnv(ctx, projectEnvPath, envMap, injectionVars); loadAndMergeProjectEnvErr != nil {
			switch {
			case errors.Is(loadAndMergeProjectEnvErr, os.ErrNotExist):
				slog.DebugContext(ctx, "Project .env file does not exist", "path", projectEnvPath)
			case errors.Is(loadAndMergeProjectEnvErr, os.ErrPermission):
				return envMap,
					injectionVars,
					common.Classify(common.ErrProjectEnvUnreadable,
						fmt.Errorf("%s is not readable by the runtime user (uid %d, gid %d); fix its ownership/read permission or set PUID/PGID to a user that can read it: %w",
							projectEnvPath,
							os.Geteuid(),
							os.Getegid(),
							loadAndMergeProjectEnvErr))
			default:
				slog.WarnContext(ctx, "Failed to load project env", "path", projectEnvPath, "error", loadAndMergeProjectEnvErr)
			}
		}
	}

	// COMPOSE_ENV_FILES, when declared, layers additional env files on top of
	// the project .env (later entries win). Reuses the mtime-keyed cache.
	l.mergeComposeEnvFiles(ctx, envMap, injectionVars)

	return envMap, injectionVars, nil
}

func (l *EnvLoader) mergeComposeEnvFiles(ctx context.Context, envMap, injectionVars EnvMap) {
	parse := func(ctx context.Context, path string, contextEnv EnvMap) (EnvMap, error) {
		key := strings.Join([]string{path, l.projectsDir, strconv.FormatBool(l.autoInjectEnv), strconv.FormatUint(kit.Fingerprint(contextEnv), 16)}, "\x00")
		entry, err := loadCachedEnvFile(ctx, projectEnvFileCache, key, path, contextEnv)
		if err != nil {
			return nil, err
		}
		return kit.Ternary(!entry.exists, nil, entry.values), nil
	}
	onMerged := func(values EnvMap) {
		if l.autoInjectEnv {
			maps.Copy(injectionVars, values)
		}
	}
	mergeComposeEnvFiles(ctx, l.workdir, envMap, parse, onMerged)
}

func (l *EnvLoader) loadAndMergeGlobalEnv(ctx context.Context, path string, envMap, injectionVars EnvMap) error {
	entry, err := loadCachedEnvFile(ctx, globalEnvFileCache, path, path, envMap)
	if err != nil {
		return err
	}
	if !entry.exists {
		return os.ErrNotExist
	}

	for k, v := range entry.values {
		envMap[k] = v
		if !entry.noInjectKeys[k] {
			injectionVars[k] = v
		}
	}

	slog.DebugContext(ctx, "Merged global env into environment map", "totalEnvCount", len(envMap))
	return nil
}

func (l *EnvLoader) loadAndMergeProjectEnv(ctx context.Context, path string, envMap, injectionVars EnvMap) error {
	key := strings.Join([]string{path, l.projectsDir, strconv.FormatBool(l.autoInjectEnv), strconv.FormatUint(kit.Fingerprint(envMap), 16)}, "\x00")
	entry, err := loadCachedEnvFile(ctx, projectEnvFileCache, key, path, envMap)
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

	slog.DebugContext(ctx, "Merged project .env into environment map", "totalEnvCount", len(envMap))
	return nil
}

func loadCachedEnvFile(ctx context.Context, envCache *cache.Cache[string, envFileCacheEntry], key, path string, contextEnv EnvMap) (envFileCacheEntry, error) {
	if cached, ok := envCache.Peek(key); ok {
		if validEnvFileCacheEntry(ctx, cached) {
			return cached, nil
		}
		envCache.Delete(key)
	}

	entry, found, err := envCache.GetOrLoad(key, func(string) (envFileCacheEntry, bool, error) {
		info, err := acfs.Host{}.Stat(ctx, path, acfstypes.StatOptions{FollowFinal: true})
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				return envFileCacheEntry{path: path}, true, nil
			}
			return envFileCacheEntry{}, false, err
		}
		if info.IsDirectory {
			return envFileCacheEntry{}, false, fmt.Errorf("path is a directory: %s", path)
		}

		content, err := acfs.Host{}.ReadFile(ctx, path)
		if err != nil {
			return envFileCacheEntry{}, false, fmt.Errorf("parse env file: read file: %w", err)
		}
		parsed, err := ParseProjectEnvContent(string(content), contextEnv)
		if err != nil {
			return envFileCacheEntry{}, false, fmt.Errorf("parse env file: %w", err)
		}
		return envFileCacheEntry{
			path:         path,
			mtime:        time.Unix(0, info.ModTimeUnixNano),
			size:         info.Size,
			exists:       true,
			values:       parsed,
			noInjectKeys: ParseNoInjectDirectives(string(content)),
		}, true, nil
	})
	if err != nil {
		return envFileCacheEntry{}, err
	}
	if !found {
		return envFileCacheEntry{}, errors.New("environment file cache loader returned no entry")
	}
	return entry, nil
}

func validEnvFileCacheEntry(ctx context.Context, entry envFileCacheEntry) bool {
	info, err := acfs.Host{}.Stat(ctx, entry.path, acfstypes.StatOptions{FollowFinal: true})
	if err != nil {
		return !entry.exists && errors.Is(err, os.ErrNotExist)
	}
	if info.IsDirectory {
		return false
	}
	if !entry.exists || !time.Unix(0, info.ModTimeUnixNano).Equal(entry.mtime) || info.Size != entry.size {
		return false
	}
	// A chmod does not bump mtime, so confirm the file is still readable.
	file, err := acfs.Host{}.OpenFile(ctx, entry.path, acfs.OpenOptions{Read: true})
	if err != nil {
		return false
	}
	return file.Close() == nil
}

func parseProjectEnvFileExisting(ctx context.Context, path string, contextEnv EnvMap) (EnvMap, error) {
	content, err := acfs.Host{}.ReadFile(ctx, path)
	if err != nil {
		return nil, fmt.Errorf("read file: %w", err)
	}
	return ParseProjectEnvContent(string(content), contextEnv)
}

func ParseNoInjectDirectives(content string) map[string]bool {
	keys := map[string]bool{}
	for line := range strings.Lines(content) {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		comment, isComment := strings.CutPrefix(line, "#")
		if !isComment {
			break
		}
		fields := strings.Fields(comment)
		if len(fields) == 0 || fields[0] != GlobalEnvNoInjectDirective {
			continue
		}
		for _, key := range fields[1:] {
			keys[key] = true
		}
	}
	return keys
}

func ParseProjectEnvFile(ctx context.Context, path string, contextEnv EnvMap) (EnvMap, error) {
	info, err := acfs.Host{}.Stat(ctx, path, acfstypes.StatOptions{FollowFinal: true})
	if err != nil || info.IsDirectory {
		return nil, nil //nolint:nilerr // missing .env is not an error
	}
	return parseProjectEnvFileExisting(ctx, path, contextEnv)
}

func ParseProjectEnvContent(content string, contextEnv EnvMap) (EnvMap, error) {
	lookupFn := func(key string) (string, bool) {
		val, ok := contextEnv[key]
		return val, ok
	}

	envMap, err := dotenv.ParseWithLookup(strings.NewReader(content), lookupFn)
	if err != nil {
		return nil, fmt.Errorf("parse env: %w", err)
	}

	return envMap, nil
}

func WithTransientValidationEnvFile(ctx context.Context, projectPath string, effectiveEnvContent *string, run func() error) (err error) {
	originalContent, readErr := acfs.Host{}.ReadFile(ctx, filepath.Join(projectPath, ".env"))
	originalExists := readErr == nil
	if readErr != nil && !errors.Is(readErr, os.ErrNotExist) {
		if !errors.Is(readErr, os.ErrPermission) && !errors.Is(readErr, acfs.ErrIsDirectory) {
			return fmt.Errorf("prepare env file for compose validation: %w", readErr)
		}
		// The path exists but is permission-locked (e.g. chmod 000, foreign-owned) or a directory.
		// Its contents can't be verified or safely overwritten, so leave it
		// untouched and validate against whatever's already on disk instead of
		// aborting the whole update.
		slog.WarnContext(ctx, "skipping unreadable .env during compose validation; leaving it untouched", "projectPath", projectPath, "error", readErr)
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
			return fmt.Errorf("prepare env file for compose validation: %w", writeErr)
		}

		defer func() {
			var restoreErr error
			switch {
			case originalExists:
				restoreErr = WriteProjectFile(ctx, projectPath, projectPath, ".env", string(originalContent))
			default:
				restoreErr = acfs.Root{Path: projectPath}.Remove(ctx, "/.env")
			}

			if restoreErr != nil && !errors.Is(restoreErr, os.ErrNotExist) {
				if err == nil {
					err = fmt.Errorf("restore env file after compose validation: %w", restoreErr)
				}
			}
		}()
	}

	if run == nil {
		return nil
	}

	return run()
}

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

	switch {
	case gitContent == "":
		return overrideContent, nil
	case overrideContent == "":
		return gitContent, nil
	}

	separated := strings.HasSuffix(gitContent, "\n") || strings.HasPrefix(overrideContent, "\n")
	concatenated := kit.Ternary(separated, gitContent+overrideContent, gitContent+"\n"+overrideContent)

	candidate, ok := mergeEnvOverridesInPlace(gitContent, overrideContent, overrideEnv)
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

func EnvContentChanged(oldContent, newContent string) bool {
	oldEnv, oldErr := ParseProjectEnvContent(oldContent, nil)
	newEnv, newErr := ParseProjectEnvContent(newContent, nil)
	if oldErr != nil || newErr != nil {
		return oldContent != newContent
	}
	return !maps.Equal(oldEnv, newEnv)
}

func mergeEnvOverridesInPlace(gitContent, overrideContent string, overrideEnv EnvMap) (merged string, ok bool) {
	rewritten := make(map[string]struct{})
	gitLines := strings.Split(gitContent, "\n")

	for i, line := range gitLines {
		body, hadCR := strings.CutSuffix(line, "\r")
		match := envKeyLineRegex.FindStringSubmatch(body)
		if match == nil {
			continue
		}
		overrideValue, exists := overrideEnv[match[2]]
		if !exists {
			continue
		}

		value, comment := splitEnvValueComment(match[4])
		separator := value[len(strings.TrimRight(value, " \t")):]
		if comment != "" && separator == "" {
			separator = " "
		}

		body = match[1] + match[2] + match[3] + formatEnvValue(overrideValue) + separator + comment
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
		if match := envKeyLineRegex.FindStringSubmatch(strings.TrimSuffix(line, "\r")); match != nil {
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

func splitEnvValueComment(raw string) (value, comment string) {
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

	return formatEnvMap(override), nil
}

func BuildOverrideEnvContent(gitContent, effectiveContent string) (string, error) {
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

	if maps.Equal(effectiveEnv, override) {
		return effectiveContent, nil
	}

	return formatEnvMap(override), nil
}

func CheckProjectEnvAccess(ctx context.Context, projectsDir, dir string) *types.ProjectConfigurationError {
	envPath := filepath.Join(dir, EffectiveEnvFileName)
	file, err := acfs.Host{}.OpenFile(ctx, envPath, acfs.OpenOptions{Read: true})
	if err == nil {
		if closeErr := file.Close(); closeErr != nil {
			slog.WarnContext(ctx, "failed to close project env access probe", "path", envPath, "error", closeErr)
		}
		return nil
	}
	if !errors.Is(err, os.ErrPermission) {
		return nil
	}

	disabled := false
	if strings.TrimSpace(projectsDir) != "" {
		globalEnv, globalErr := ParseProjectEnvFile(ctx, filepath.Join(projectsDir, GlobalEnvFileName), make(EnvMap))
		if globalErr != nil {
			slog.DebugContext(ctx, "Failed to read global env while checking project env access", "path", projectsDir, "error", globalErr)
		} else {
			disabled = parseComposeBool(globalEnv, consts.ComposeDisableDefaultEnvFile)
		}
	}

	return &types.ProjectConfigurationError{
		Code:             common.ConfigurationErrorCodeEnvFileUnreadable,
		Path:             envPath,
		UID:              os.Geteuid(),
		GID:              os.Getegid(),
		BlocksOperations: !disabled,
	}
}

func ReadProjectEnvState(ctx context.Context, projectPath string) (ProjectEnvState, error) {
	effectiveContent, hasEffective, effectiveUnreadable, err := readOptionalProjectFile(ctx, projectPath, EffectiveEnvFileName)
	if err != nil {
		return ProjectEnvState{}, err
	}

	gitContent, hasGitSource, gitSourceUnreadable, err := readOptionalProjectFile(ctx, projectPath, GitSourceEnvFileName)
	if err != nil {
		return ProjectEnvState{}, err
	}

	overrideContent, hasOverride, overrideUnreadable, err := readOptionalProjectFile(ctx, projectPath, OverrideEnvFileName)
	if err != nil {
		return ProjectEnvState{}, err
	}

	if effectiveUnreadable || gitSourceUnreadable || overrideUnreadable {
		slog.WarnContext(ctx, "skipping unreadable project env file(s); leaving them untouched",
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

func WriteManagedEnvFile(ctx context.Context, projectsDirectory, projectPath, fileName string, unreadable bool, content string) error {
	if unreadable {
		slog.WarnContext(ctx, "skipping unreadable project env file; leaving it untouched", "projectPath", projectPath, "file", fileName)
		return nil
	}

	switch fileName {
	case EffectiveEnvFileName:
		entry, statErr := acfs.Host{}.Stat(ctx, filepath.Join(projectPath, EffectiveEnvFileName), acfstypes.StatOptions{})
		if strings.TrimSpace(content) == "" && (statErr != nil || !entry.IsSymlink) {
			return RemoveProjectFile(ctx, projectsDirectory, projectPath, EffectiveEnvFileName)
		}
		return WriteProjectFile(ctx, projectsDirectory, projectPath, EffectiveEnvFileName, content)
	case GitSourceEnvFileName:
		return WriteProjectFile(ctx, projectsDirectory, projectPath, GitSourceEnvFileName, content)
	case OverrideEnvFileName:
		if strings.TrimSpace(content) == "" {
			return RemoveProjectFile(ctx, projectsDirectory, projectPath, OverrideEnvFileName)
		}
		return WriteProjectFile(ctx, projectsDirectory, projectPath, OverrideEnvFileName, content)
	default:
		return fmt.Errorf("write managed env file: unsupported file name %q", fileName)
	}
}

func readOptionalProjectFile(ctx context.Context, projectPath, fileName string) (content string, exists, unreadable bool, err error) {
	raw, readErr := acfs.Host{}.ReadFile(ctx, filepath.Join(projectPath, fileName))
	if readErr == nil {
		return string(raw), true, false, nil
	}
	if errors.Is(readErr, os.ErrNotExist) {
		return "", false, false, nil
	}
	if errors.Is(readErr, os.ErrPermission) || errors.Is(readErr, acfs.ErrIsDirectory) {
		return "", false, true, nil
	}
	return "", false, false, fmt.Errorf("read %s: %w", fileName, readErr)
}

func formatEnvMap(envMap EnvMap) string {
	if len(envMap) == 0 {
		return ""
	}

	keys := slices.Sorted(maps.Keys(envMap))

	var builder strings.Builder
	for _, key := range keys {
		builder.WriteString(key)
		builder.WriteByte('=')
		builder.WriteString(formatEnvValue(envMap[key]))
		builder.WriteByte('\n')
	}

	return builder.String()
}

func formatEnvValue(value string) string {
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

func ParseComposeEnvOptions(ctx context.Context, workdir string, env EnvMap) (ComposeEnvOptions, error) {
	opts := ComposeEnvOptions{
		ProjectName: strings.TrimSpace(env[consts.ComposeProjectName]),
	}

	files, err := resolveComposeFileSelection(ctx, workdir, env)
	if err != nil {
		return ComposeEnvOptions{}, err
	}
	opts.ConfigFiles = files

	if raw := strings.TrimSpace(env[consts.ComposeProfiles]); raw != "" {
		opts.Profiles = kit.TrimNonEmpty(strings.Split(raw, ","))
	}

	for _, entry := range ComposeEnvFileEntriesFromEnv(env) {
		resolved, resErr := ResolvePathWithinDir(workdir, entry)
		if resErr != nil {
			return ComposeEnvOptions{}, common.Classify(common.ErrComposeFileEnvInvalid, fmt.Errorf("COMPOSE_ENV_FILES entry %q: %w", entry, resErr))
		}
		opts.EnvFiles = append(opts.EnvFiles, resolved)
	}

	opts.RemoveOrphans = parseComposeBool(env, composeRemoveOrphansKey)
	opts.IgnoreOrphans = parseComposeBool(env, composeIgnoreOrphansKey)

	if raw := strings.TrimSpace(env[composeParallelLimitKey]); raw != "" {
		if n, convErr := strconv.Atoi(raw); convErr == nil && n > 0 {
			opts.ParallelLimit = n
		} else {
			slog.DebugContext(context.Background(), "ignoring invalid COMPOSE_PARALLEL_LIMIT", "value", raw) //nolint:forbidigo // Compose environment parsing is a pure helper without a request context.
		}
	}

	return opts, nil
}

func ComposeFileEnvSelection(ctx context.Context, projectsDir, dir string) ([]string, error) {
	envMap := make(EnvMap)
	if strings.TrimSpace(projectsDir) != "" {
		globalEnvPath := filepath.Join(projectsDir, GlobalEnvFileName)
		globalEnv, err := loadCachedEnvFile(ctx, globalEnvFileCache, globalEnvPath+"\x00compose-file-selection", globalEnvPath, envMap)
		if err != nil {
			return nil, err
		}
		maps.Copy(envMap, globalEnv.values)
	}

	// COMPOSE_DISABLE_ENV_FILE skips the project .env; it is read only from the
	// sources merged so far (.env.global), matching LoadEnvironment.
	if !parseComposeBool(envMap, consts.ComposeDisableDefaultEnvFile) {
		projectEnvPath := filepath.Join(dir, EffectiveEnvFileName)
		projectEnv, err := ParseProjectEnvFile(ctx, projectEnvPath, envMap)
		if err != nil {
			if errors.Is(err, os.ErrPermission) {
				return nil,
					common.Classify(common.ErrProjectEnvUnreadable,
						fmt.Errorf("%s is not readable by the runtime user (uid %d, gid %d); fix its ownership/read permission or set PUID/PGID to a user that can read it: %w",
							projectEnvPath,
							os.Geteuid(),
							os.Getegid(),
							err))
			}
			return nil, err
		}
		maps.Copy(envMap, projectEnv)
	}
	if len(envMap) == 0 {
		return nil, nil
	}
	mergeComposeEnvFiles(ctx, dir, envMap, ParseProjectEnvFile, nil)
	return resolveComposeFileSelection(ctx, dir, envMap)
}

func ComposeFileEntriesFromEnv(env EnvMap) []string {
	raw, ok := env[consts.ComposeFilePath]
	if !ok || strings.TrimSpace(raw) == "" {
		return nil
	}

	separator := defaultComposePathSeparator
	if custom := strings.TrimSpace(env[consts.ComposePathSeparator]); custom != "" {
		separator = custom
	}

	return kit.TrimNonEmpty(strings.Split(raw, separator))
}

func ComposeEnvFileEntriesFromEnv(env EnvMap) []string {
	raw := strings.TrimSpace(env[composeEnvFilesKey])
	if raw == "" {
		return nil
	}
	return kit.TrimNonEmpty(strings.Split(raw, ","))
}

func resolveComposeFileSelection(ctx context.Context, workdir string, env EnvMap) ([]string, error) {
	entries := ComposeFileEntriesFromEnv(env)
	if len(entries) == 0 {
		return nil, nil
	}

	absWorkdir, err := filepath.Abs(filepath.Clean(workdir))
	if err != nil {
		return nil, common.Classify(common.ErrComposeFileEnvInvalid, fmt.Errorf("resolve project directory: %w", err))
	}

	files := make([]string, 0, len(entries))
	for i, entry := range entries {
		if filepath.IsAbs(entry) {
			return nil, common.Classify(common.ErrComposeFileEnvInvalid, fmt.Errorf("COMPOSE_FILE entry %q must be relative to the project directory", entry))
		}

		resolved, resErr := ResolvePathWithinDir(absWorkdir, entry)
		if resErr != nil {
			return nil, common.Classify(common.ErrComposeFileEnvInvalid, fmt.Errorf("COMPOSE_FILE entry %q: %w", entry, resErr))
		}

		if i == 0 && filepath.Dir(resolved) != absWorkdir {
			return nil, common.Classify(common.ErrComposeFileEnvInvalid, fmt.Errorf("the first COMPOSE_FILE entry %q must be in the project directory", entry))
		}

		info, statErr := acfs.Host{}.Stat(ctx, resolved, acfstypes.StatOptions{FollowFinal: true})
		if statErr != nil {
			return nil, common.Classify(common.ErrComposeFileEnvInvalid, fmt.Errorf("COMPOSE_FILE entry %q: %w", entry, statErr))
		}
		if info.IsDirectory {
			return nil, common.Classify(common.ErrComposeFileEnvInvalid, fmt.Errorf("COMPOSE_FILE entry %q is a directory", entry))
		}

		files = append(files, resolved)
	}

	return files, nil
}

func mergeComposeEnvFiles(ctx context.Context, workdir string, envMap EnvMap, parse func(ctx context.Context, path string, contextEnv EnvMap) (EnvMap, error), onMerged func(values EnvMap)) {
	for _, entry := range ComposeEnvFileEntriesFromEnv(envMap) {
		resolved, err := ResolvePathWithinDir(workdir, entry)
		if err != nil {
			slog.WarnContext(ctx, "skipping COMPOSE_ENV_FILES entry outside project directory", "entry", entry, "error", err)
			continue
		}
		if _, statErr := (acfs.Host{}).Stat(ctx, resolved, acfstypes.StatOptions{FollowFinal: true}); statErr != nil {
			slog.WarnContext(ctx, "COMPOSE_ENV_FILES entry not found", "path", resolved, "error", statErr)
			continue
		}
		values, err := parse(ctx, resolved, envMap)
		if err != nil {
			slog.WarnContext(ctx, "failed to load COMPOSE_ENV_FILES entry", "path", resolved, "error", err)
			continue
		}
		if len(values) == 0 {
			continue
		}
		maps.Copy(envMap, values)
		if onMerged != nil {
			onMerged(values)
		}
	}
}

func parseComposeBool(env EnvMap, key string) bool {
	raw := strings.TrimSpace(env[key])
	if raw == "" {
		return false
	}
	v, ok := kit.ParseBool(raw)
	if !ok {
		slog.DebugContext(context.Background(), "ignoring invalid compose boolean env var", //nolint:forbidigo // Compose environment parsing is a pure helper without a request context.
			"key", key, "value", raw)
	}
	return v
}
