package projects

import (
	"context"
	"fmt"
	"log/slog"
	"maps"
	"math"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	interp "github.com/compose-spec/compose-go/v2/interpolation"
	"github.com/compose-spec/compose-go/v2/loader"
	"github.com/compose-spec/compose-go/v2/template"
	"github.com/compose-spec/compose-go/v2/tree"
	composetypes "github.com/compose-spec/compose-go/v2/types"
	"github.com/docker/compose/v5/pkg/api"
	"github.com/docker/go-units"
	"github.com/getarcaneapp/arcane/backend/v2/internal/common"
)

var ProjectFileCandidates = []string{
	"compose.yaml",
	"compose.yml",
	"docker-compose.yaml",
	"docker-compose.yml",
	"podman-compose.yaml",
	"podman-compose.yml",
	".env",
}

// IsProjectFile reports whether filename is a known project file or a plausible
// custom YAML filename worth watching for compose discovery.
func IsProjectFile(filename string) bool {
	if slices.Contains(ProjectFileCandidates, filename) {
		return true
	}

	ext := strings.ToLower(filepath.Ext(filename))
	if ext != ".yaml" && ext != ".yml" {
		return false
	}

	base := filepath.Base(filename)
	return base != "" && !strings.HasPrefix(base, ".")
}

func stripTrailingProjectCounterInternal(name string) string {
	trimmed := strings.TrimSpace(name)
	withoutDigits := strings.TrimRight(trimmed, "0123456789")
	if len(withoutDigits) == len(trimmed) || withoutDigits == "" {
		return trimmed
	}

	if last := withoutDigits[len(withoutDigits)-1]; last != '-' && last != '_' {
		return trimmed
	}

	return withoutDigits[:len(withoutDigits)-1]
}

func DetectComposeFile(dir string) (string, error) {
	for _, filename := range ProjectFileCandidates {
		if filename == ".env" {
			continue
		}

		composePath := filepath.Join(dir, filename)
		if info, err := os.Stat(composePath); err == nil && !info.IsDir() {
			return composePath, nil
		}
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		return "", err
	}

	dirBase := filepath.Base(filepath.Clean(dir))
	normalizedDirBase := loader.NormalizeProjectName(dirBase)
	normalizedTrimmedDirBase := loader.NormalizeProjectName(stripTrailingProjectCounterInternal(dirBase))

	customCandidates := make([]string, 0)
	dirMatchedCandidates := make([]string, 0)
	composeNamedCandidates := make([]string, 0)

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}

		name := entry.Name()
		if slices.Contains(ProjectFileCandidates, name) || !IsProjectFile(name) {
			continue
		}

		candidatePath := filepath.Join(dir, name)

		stem := strings.TrimSuffix(name, filepath.Ext(name))
		normalizedStem := loader.NormalizeProjectName(strings.TrimSpace(stem))
		dirMatched := normalizedStem != "" && (normalizedStem == normalizedDirBase || normalizedStem == normalizedTrimmedDirBase)
		composeNamed := strings.Contains(strings.ToLower(stem), "compose")

		hasComposeRootKeys, rootKeysErr := HasComposeRootKeysInFile(candidatePath)
		if rootKeysErr == nil {
			if !hasComposeRootKeys {
				continue
			}
		} else if !dirMatched && !composeNamed {
			continue
		}

		if dirMatched {
			dirMatchedCandidates = append(dirMatchedCandidates, candidatePath)
		}

		if composeNamed {
			composeNamedCandidates = append(composeNamedCandidates, candidatePath)
		}

		customCandidates = append(customCandidates, candidatePath)
	}

	switch {
	case len(dirMatchedCandidates) == 1:
		return dirMatchedCandidates[0], nil
	case len(dirMatchedCandidates) > 1:
		return "", &common.AmbiguousComposeFileError{Dir: dir}
	case len(composeNamedCandidates) == 1:
		return composeNamedCandidates[0], nil
	case len(composeNamedCandidates) > 1:
		return "", &common.AmbiguousComposeFileError{Dir: dir}
	case len(customCandidates) == 1:
		return customCandidates[0], nil
	case len(customCandidates) > 1:
		return "", &common.AmbiguousComposeFileError{Dir: dir}
	default:
		return "", &common.ComposeFileNotFoundError{Dir: dir}
	}
}

func LoadComposeProject(ctx context.Context, composeFile, projectName, projectsDirectory string, autoInjectEnv bool, pathMapper *PathMapper) (*composetypes.Project, error) {
	return loadComposeProjectInternal(ctx, composeFile, projectName, projectsDirectory, autoInjectEnv, pathMapper, nil, nil, false)
}

// LoadComposeProjectLenient loads a compose project tolerating undefined variables.
// Instead of substituting undefined ${VAR} references with an empty string (which
// produces invalid volume/bind specs like ":/path"), it replaces them with a
// placeholder value so structural validation can succeed. This is useful during
// GitSync validation where a .env file may not yet exist.
func LoadComposeProjectLenient(ctx context.Context, composeFile, projectName, projectsDirectory string, autoInjectEnv bool, pathMapper *PathMapper) (*composetypes.Project, error) {
	return loadComposeProjectInternal(ctx, composeFile, projectName, projectsDirectory, autoInjectEnv, pathMapper, nil, nil, true)
}

// wrapTypeCastMappingLenientInternal prepares the interp type-cast mapping for lenient
// loading: every existing cast falls back to "0" when it fails (so the lenient
// placeholder for an undefined variable doesn't abort interpolation), and casts
// are added for typed struct fields that compose-go would otherwise decode at
// the mapstructure stage (NanoCPUs, UnitBytes under deploy.resources). Without
// the added casts, the placeholder string reaches mapstructure and fails to
// parse — handling it at interp time lets us hand mapstructure a typed value
// it accepts directly.
func wrapTypeCastMappingLenientInternal(mapping map[tree.Path]interp.Cast) map[tree.Path]interp.Cast {
	wrapped := make(map[tree.Path]interp.Cast, len(mapping)+4)
	for path, original := range mapping {
		wrapped[path] = wrapCastWithLenientFallbackInternal(original)
	}
	addIfAbsent := func(path tree.Path, cast interp.Cast) {
		if _, exists := wrapped[path]; exists {
			return
		}
		wrapped[path] = wrapCastWithLenientFallbackInternal(cast)
	}
	for _, section := range []string{"limits", "reservations"} {
		addIfAbsent(tree.NewPath("services", tree.PathMatchAll, "deploy", "resources", section, "cpus"), lenientCastFloatInternal)
		addIfAbsent(tree.NewPath("services", tree.PathMatchAll, "deploy", "resources", section, "memory"), lenientCastSizeInternal)
	}
	return wrapped
}

func wrapCastWithLenientFallbackInternal(original interp.Cast) interp.Cast {
	return func(value string) (any, error) {
		result, err := original(value)
		if err == nil {
			return result, nil
		}
		if fallback, fallbackErr := original("0"); fallbackErr == nil {
			return fallback, nil
		}
		return result, err
	}
}

func lenientCastFloatInternal(value string) (any, error) {
	return strconv.ParseFloat(value, 64)
}

func lenientCastSizeInternal(value string) (any, error) {
	n, err := strconv.ParseInt(value, 10, 64)
	if err != nil {
		b, parseErr := units.RAMInBytes(value)
		if parseErr != nil {
			return nil, parseErr
		}
		n = b
	}
	if n > math.MaxInt || n < math.MinInt {
		return nil, fmt.Errorf("size %d out of range for platform int", n)
	}
	return int(n), nil
}

// ApplyLenientLoaderOptions configures a compose loader.Options for tolerant
// loading: structural validation and consistency checks are skipped, and
// undefined ${VAR} references are substituted with a placeholder instead of
// an empty string (which would otherwise produce invalid volume/bind specs
// like ":/path"). Callers use this when a .env file may not yet exist or may
// not define every variable the compose file references.
func ApplyLenientLoaderOptions(ctx context.Context, opts *loader.Options, composeFile string) {
	opts.SkipValidation = true
	opts.SkipConsistencyCheck = true
	if opts.Interpolate == nil {
		slog.WarnContext(ctx, "compose loader did not initialize Interpolate options; lenient variable substitution will not apply", "compose_file", composeFile)
		return
	}

	realLookup := opts.Interpolate.LookupValue
	opts.Interpolate = &interp.Options{
		Substitute:      template.Substitute,
		TypeCastMapping: wrapTypeCastMappingLenientInternal(opts.Interpolate.TypeCastMapping),
		LookupValue: func(key string) (string, bool) {
			if val, ok := realLookup(key); ok {
				return val, true
			}
			slog.DebugContext(ctx, "compose variable undefined during lenient load, using placeholder", "variable", key, "compose_file", composeFile)
			return "/placeholder-undefined", true
		},
	}
}

func loadComposeProjectInternal(
	ctx context.Context,
	composeFile string,
	projectName string,
	projectsDirectory string,
	autoInjectEnv bool,
	pathMapper *PathMapper,
	envOverride EnvMap,
	configureLoader func(*loader.Options),
	lenient bool,
) (project *composetypes.Project, err error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			slog.WarnContext(ctx,
				"panic while loading compose project; compose file may contain invalid syntax",
				"path", composeFile,
				"error", recovered,
			)
			err = fmt.Errorf("load compose project panic for %s: %v", composeFile, recovered)
			project = nil
		}
	}()

	workdir := filepath.Dir(composeFile)

	envLoader := NewEnvLoader(projectsDirectory, workdir, autoInjectEnv)

	// Load full environment (process + global + project .env) for service injection
	fullEnvMap, injectionVars, err := envLoader.LoadEnvironment(ctx)
	if err != nil {
		slog.WarnContext(ctx, "Failed to load environment", "error", err)
	}

	// Override wins: maps.Copy(dst, src) copies src into dst.
	maps.Copy(fullEnvMap, envOverride)

	// Set PWD
	if absWorkdir, absErr := filepath.Abs(workdir); absErr == nil {
		fullEnvMap["PWD"] = absWorkdir
	} else {
		slog.WarnContext(ctx, "Failed to set PWD environment variable", "workdir", workdir, "error", absErr)
	}

	// Pass full environment to compose-go for interpolation, compose-go will use this for ${VAR} expansion in the compose file
	cfg := composetypes.ConfigDetails{
		Version:    api.ComposeVersion,
		WorkingDir: workdir,
		ConfigFiles: []composetypes.ConfigFile{
			{Filename: composeFile},
		},
		Environment: composetypes.Mapping(fullEnvMap),
	}

	project, err = loader.LoadWithContext(ctx, cfg, func(opts *loader.Options) {
		if projectName != "" {
			opts.SetProjectName(projectName, true)
		}
		// Discard env_file after folding into environment, as the compose CLI
		// does, so config-hashes match and both tools stop recreating services.
		loader.WithDiscardEnvFiles(opts)
		if lenient {
			ApplyLenientLoaderOptions(ctx, opts, composeFile)
		}
		if configureLoader != nil {
			configureLoader(opts)
		}
	})
	if err != nil {
		return nil, fmt.Errorf("load compose project: %w", err)
	}

	for _, configFile := range cfg.ConfigFiles {
		project.ComposeFiles = append(project.ComposeFiles, configFile.Filename)
	}

	project = project.WithoutUnnecessaryResources()

	// Resolve relative paths for bind mounts, secrets, and configs
	ResolveRelativeProjectPaths(project, workdir)

	// Translate container paths to host paths for Docker execution
	if pathMapper != nil {
		if err := pathMapper.TranslateVolumeSources(project); err != nil {
			return nil, fmt.Errorf("failed to translate paths for docker host: %w", err)
		}
	}

	injectServiceConfiguration(project, injectionVars)
	return project, nil
}

func applyCustomLabelsInternal(projectName string, serviceName string, workingDirectory string, composeFiles []string) composetypes.Labels {
	return composetypes.Labels{
		api.ProjectLabel:     projectName,
		api.ServiceLabel:     serviceName,
		api.VersionLabel:     api.ComposeVersion,
		api.OneoffLabel:      "False",
		api.WorkingDirLabel:  workingDirectory,
		api.ConfigFilesLabel: strings.Join(composeFiles, ","),
	}
}

func injectServiceConfiguration(project *composetypes.Project, injectionVars EnvMap) {
	for i, s := range project.Services {
		s.CustomLabels = applyCustomLabelsInternal(project.Name, s.Name, project.WorkingDir, project.ComposeFiles)

		// Initialize environment if nil
		if s.Environment == nil {
			s.Environment = make(composetypes.MappingWithEquals)
		}

		for k, v := range injectionVars {
			if _, exists := s.Environment[k]; !exists {
				s.Environment[k] = new(v)
			}
		}

		project.Services[i] = s
	}
}

func LoadComposeProjectFromDir(ctx context.Context, dir, projectName, projectsDirectory string, autoInjectEnv bool, pathMapper *PathMapper) (*composetypes.Project, string, error) {
	composeFile, err := DetectComposeFile(dir)
	if err != nil {
		return nil, "", err
	}

	proj, err := LoadComposeProject(ctx, composeFile, projectName, projectsDirectory, autoInjectEnv, pathMapper)
	if err != nil {
		return nil, "", err
	}

	return proj, composeFile, nil
}

func ResolveRelativeProjectPaths(project *composetypes.Project, workdir string) {
	if project == nil || workdir == "" {
		return
	}

	for name, service := range project.Services {
		modified := false
		for i := range service.Volumes {
			v := &service.Volumes[i]
			if v.Type == composetypes.VolumeTypeBind {
				if resolved, ok := resolvePathRelative(workdir, v.Source); ok {
					v.Source = resolved
					modified = true
				}
			}
		}
		if modified {
			project.Services[name] = service
		}
	}

	for name, secret := range project.Secrets {
		if resolved, ok := resolvePathRelative(workdir, secret.File); ok {
			secret.File = resolved
			project.Secrets[name] = secret
		}
	}

	for name, config := range project.Configs {
		if resolved, ok := resolvePathRelative(workdir, config.File); ok {
			config.File = resolved
			project.Configs[name] = config
		}
	}
}

func resolvePathRelative(workdir, candidate string) (string, bool) {
	if candidate == "" || filepath.IsAbs(candidate) || workdir == "" {
		return filepath.Clean(candidate), false
	}
	return filepath.Clean(filepath.Join(workdir, candidate)), true
}
