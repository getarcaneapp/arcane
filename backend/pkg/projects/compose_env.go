package projects

import (
	"context"
	"log/slog"
	"maps"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"emperror.dev/errors"

	"github.com/compose-spec/compose-go/v2/consts"
	"github.com/getarcaneapp/arcane/backend/v2/internal/common"
)

// Docker Compose pre-defined variable names that compose-go's consts package
// does not export. See https://docs.docker.com/compose/how-tos/environment-variables/envvars.
const (
	composeEnvFilesKey      = "COMPOSE_ENV_FILES"
	composeRemoveOrphansKey = "COMPOSE_REMOVE_ORPHANS"
	composeIgnoreOrphansKey = "COMPOSE_IGNORE_ORPHANS"
	composeParallelLimitKey = "COMPOSE_PARALLEL_LIMIT"

	defaultComposePathSeparator = ":"
)

// ComposeEnvOptions holds the deployment-relevant Docker Compose pre-defined
// environment variables parsed from a project's merged environment.
type ComposeEnvOptions struct {
	// ConfigFiles is the resolved, absolute COMPOSE_FILE selection in order (the
	// first entry is the base file). Nil when COMPOSE_FILE is unset.
	ConfigFiles []string
	// Profiles is the COMPOSE_PROFILES selection.
	Profiles []string
	// ProjectName is COMPOSE_PROJECT_NAME (not normalized).
	ProjectName string
	// EnvFiles is the resolved, absolute COMPOSE_ENV_FILES selection in order.
	EnvFiles []string
	// RemoveOrphans is COMPOSE_REMOVE_ORPHANS.
	RemoveOrphans bool
	// IgnoreOrphans is COMPOSE_IGNORE_ORPHANS.
	IgnoreOrphans bool
	// ParallelLimit is COMPOSE_PARALLEL_LIMIT; 0 means unset.
	ParallelLimit int
}

// ParseComposeEnvOptions extracts the deployment-relevant COMPOSE_* variables
// from an already-merged environment map. Malformed path selections return a
// classified common.ErrComposeFileEnvInvalid; malformed scalars are ignored
// with a debug log, matching `docker compose`'s tolerance.
func ParseComposeEnvOptions(workdir string, env EnvMap) (ComposeEnvOptions, error) {
	opts := ComposeEnvOptions{
		ProjectName: strings.TrimSpace(env[consts.ComposeProjectName]),
	}

	files, err := resolveComposeFileSelectionInternal(workdir, env)
	if err != nil {
		return ComposeEnvOptions{}, err
	}
	opts.ConfigFiles = files

	if raw := strings.TrimSpace(env[consts.ComposeProfiles]); raw != "" {
		opts.Profiles = splitAndTrimInternal(raw, ",")
	}

	for _, entry := range ComposeEnvFileEntriesFromEnv(env) {
		resolved, resErr := ResolvePathWithinDir(workdir, entry)
		if resErr != nil {
			return ComposeEnvOptions{}, common.Classify(common.ErrComposeFileEnvInvalid, errors.WrapIff(resErr, "COMPOSE_ENV_FILES entry %q", entry))
		}
		opts.EnvFiles = append(opts.EnvFiles, resolved)
	}

	opts.RemoveOrphans = parseComposeBoolInternal(env, composeRemoveOrphansKey)
	opts.IgnoreOrphans = parseComposeBoolInternal(env, composeIgnoreOrphansKey)

	if raw := strings.TrimSpace(env[composeParallelLimitKey]); raw != "" {
		if n, convErr := strconv.Atoi(raw); convErr == nil && n > 0 {
			opts.ParallelLimit = n
		} else {
			slog.Debug("ignoring invalid COMPOSE_PARALLEL_LIMIT", "value", raw)
		}
	}

	return opts, nil
}

// ComposeFileEnvSelection resolves the COMPOSE_FILE selection for dir with the
// same layering EnvLoader.LoadEnvironment uses at deployment: .env.global from
// projectsDir first, the project .env on top, then any COMPOSE_ENV_FILES
// entries. Returns nil when the merged environment does not set COMPOSE_FILE.
// COMPOSE_DISABLE_ENV_FILE (declared in .env.global) skips the project .env, as
// `docker compose` does; a global COMPOSE_FILE still applies. An empty
// projectsDir skips the global layer.
func ComposeFileEnvSelection(ctx context.Context, projectsDir, dir string) ([]string, error) {
	envMap := make(EnvMap)
	if strings.TrimSpace(projectsDir) != "" {
		globalEnv, err := ParseProjectEnvFile(filepath.Join(projectsDir, GlobalEnvFileName), envMap)
		if err != nil {
			return nil, err
		}
		maps.Copy(envMap, globalEnv)
	}

	// COMPOSE_DISABLE_ENV_FILE skips the project .env; it is read only from the
	// sources merged so far (.env.global), matching LoadEnvironment.
	if !parseComposeBoolInternal(envMap, consts.ComposeDisableDefaultEnvFile) {
		projectEnv, err := ParseProjectEnvFile(filepath.Join(dir, EffectiveEnvFileName), envMap)
		if err != nil {
			return nil, err
		}
		maps.Copy(envMap, projectEnv)
	}
	if len(envMap) == 0 {
		return nil, nil
	}
	mergeComposeEnvFilesInternal(ctx, dir, envMap, ParseProjectEnvFile, nil)
	return resolveComposeFileSelectionInternal(dir, envMap)
}

// ComposeFileEntriesFromEnv returns the raw COMPOSE_FILE entries declared in env
// (split on COMPOSE_PATH_SEPARATOR, ":" by default), or nil when unset. Entries
// are not resolved, validated, or stat'd.
func ComposeFileEntriesFromEnv(env EnvMap) []string {
	raw, ok := env[consts.ComposeFilePath]
	if !ok || strings.TrimSpace(raw) == "" {
		return nil
	}

	separator := defaultComposePathSeparator
	if custom := strings.TrimSpace(env[consts.ComposePathSeparator]); custom != "" {
		separator = custom
	}

	return splitAndTrimInternal(raw, separator)
}

// ComposeEnvFileEntriesFromEnv returns the raw COMPOSE_ENV_FILES entries
// declared in env (comma-separated), or nil when unset. Entries are not
// resolved, validated, or stat'd.
func ComposeEnvFileEntriesFromEnv(env EnvMap) []string {
	raw := strings.TrimSpace(env[composeEnvFilesKey])
	if raw == "" {
		return nil
	}
	return splitAndTrimInternal(raw, ",")
}

// resolveComposeFileSelectionInternal parses COMPOSE_FILE from env into an
// ordered list of absolute compose-file paths, or nil when COMPOSE_FILE is
// unset. Every entry must be a relative path to an existing file within
// workdir; the first entry (the base file) must sit directly in workdir.
func resolveComposeFileSelectionInternal(workdir string, env EnvMap) ([]string, error) {
	entries := ComposeFileEntriesFromEnv(env)
	if len(entries) == 0 {
		return nil, nil
	}

	absWorkdir, err := filepath.Abs(filepath.Clean(workdir))
	if err != nil {
		return nil, common.Classify(common.ErrComposeFileEnvInvalid, errors.WrapIf(err, "resolve project directory"))
	}

	files := make([]string, 0, len(entries))
	for i, entry := range entries {
		if filepath.IsAbs(entry) {
			return nil, common.Classify(common.ErrComposeFileEnvInvalid, errors.Errorf("COMPOSE_FILE entry %q must be relative to the project directory", entry))
		}

		resolved, resErr := ResolvePathWithinDir(absWorkdir, entry)
		if resErr != nil {
			return nil, common.Classify(common.ErrComposeFileEnvInvalid, errors.WrapIff(resErr, "COMPOSE_FILE entry %q", entry))
		}

		if i == 0 && filepath.Dir(resolved) != absWorkdir {
			return nil, common.Classify(common.ErrComposeFileEnvInvalid, errors.Errorf("the first COMPOSE_FILE entry %q must be in the project directory", entry))
		}

		// os.Stat rather than acfs: compose files may be symlinks resolving
		// outside any confinement root, and workdir can be an imported project
		// living outside the projects directory.
		info, statErr := os.Stat(resolved)
		if statErr != nil {
			return nil, common.Classify(common.ErrComposeFileEnvInvalid, errors.WrapIff(statErr, "COMPOSE_FILE entry %q", entry))
		}
		if info.IsDir() {
			return nil, common.Classify(common.ErrComposeFileEnvInvalid, errors.Errorf("COMPOSE_FILE entry %q is a directory", entry))
		}

		files = append(files, resolved)
	}

	return files, nil
}

// mergeComposeEnvFilesInternal merges each COMPOSE_ENV_FILES entry (in order,
// later wins) on top of envMap; bad entries are warned and skipped. Unlike
// `docker compose`, the entries are additive on top of the project .env rather
// than a replacement: .env is Arcane's managed effective env file and the place
// COMPOSE_ENV_FILES itself is declared, so replacing it would be self-defeating.
func mergeComposeEnvFilesInternal(ctx context.Context, workdir string, envMap EnvMap, parse func(path string, contextEnv EnvMap) (EnvMap, error), onMerged func(values EnvMap)) {
	for _, entry := range ComposeEnvFileEntriesFromEnv(envMap) {
		resolved, err := ResolvePathWithinDir(workdir, entry)
		if err != nil {
			slog.WarnContext(ctx, "skipping COMPOSE_ENV_FILES entry outside project directory", "entry", entry, "error", err)
			continue
		}
		// os.Stat rather than acfs: env files may be symlinks resolving outside
		// any confinement root (a supported setup).
		if _, statErr := os.Stat(resolved); statErr != nil {
			slog.WarnContext(ctx, "COMPOSE_ENV_FILES entry not found", "path", resolved, "error", statErr)
			continue
		}
		values, err := parse(resolved, envMap)
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

func splitAndTrimInternal(raw, sep string) []string {
	parts := strings.Split(raw, sep)
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if trimmed := strings.TrimSpace(p); trimmed != "" {
			out = append(out, trimmed)
		}
	}
	return out
}

func parseComposeBoolInternal(env EnvMap, key string) bool {
	raw := strings.TrimSpace(env[key])
	if raw == "" {
		return false
	}
	v, err := strconv.ParseBool(raw)
	if err != nil {
		slog.Debug("ignoring invalid compose boolean env var", "key", key, "value", raw)
		return false
	}
	return v
}
