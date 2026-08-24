package projects

import (
	"context"
	"log/slog"
	"maps"
	"math"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strconv"
	"strings"

	"emperror.dev/emperror"
	"emperror.dev/errors"

	interp "github.com/compose-spec/compose-go/v2/interpolation"
	"github.com/compose-spec/compose-go/v2/loader"
	"github.com/compose-spec/compose-go/v2/template"
	"github.com/compose-spec/compose-go/v2/tree"
	composetypes "github.com/compose-spec/compose-go/v2/types"
	"github.com/docker/compose/v5/pkg/api"
	"github.com/docker/go-units"
	"github.com/getarcaneapp/arcane/backend/v2/internal/common"
	projecttypes "github.com/getarcaneapp/arcane/backend/v2/pkg/projects/types"
	"github.com/samber/mo"
)

// ProjectFileCandidates enumerates known project files: base compose names,
// compose override names, and .env. It is used as a membership set for
// recognizing and skipping known files during discovery; it is NOT the
// base-detection order (see composeFileCandidates for that).
var ProjectFileCandidates = append(
	append(slices.Clone(composeFileCandidates), composeOverrideFileCandidates...),
	".env",
)

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

// DetectComposeFile returns the base compose file for dir. COMPOSE_FILE in the
// project's .env takes precedence over standard detection, mirroring `docker
// compose`; projectsDir locates .env.global for the COMPOSE_DISABLE_ENV_FILE
// check and may be empty where no projects directory applies.
func DetectComposeFile(ctx context.Context, projectsDir, dir string) (string, error) {
	if files, err := ComposeFileEnvSelection(ctx, projectsDir, dir); err != nil {
		return "", err
	} else if len(files) > 0 {
		return files[0], nil
	}

	// Stays on os.*: compose files may be symlinks resolving outside any
	// confinement root, and dir itself can be an imported project living
	// outside the projects directory; acfs cannot follow either.
	for _, filename := range composeFileCandidates {
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
		return "", errors.Errorf("multiple custom compose files found in %q", dir)

	case len(composeNamedCandidates) == 1:
		return composeNamedCandidates[0], nil
	case len(composeNamedCandidates) > 1:
		return "", errors.Errorf("multiple custom compose files found in %q", dir)

	case len(customCandidates) == 1:
		return customCandidates[0], nil
	case len(customCandidates) > 1:
		return "", errors.Errorf("multiple custom compose files found in %q", dir)

	default:
		return "", common.Classify(common.ErrComposeFileNotFound, errors.Errorf("no compose file found in %q", dir))
	}
}

// LoadComposeProjectFromContent loads a Compose project from in-memory source content.
func LoadComposeProjectFromContent(ctx context.Context, opts projecttypes.ComposeContentOptions) (project *composetypes.Project, err error) {
	defer recoverComposeLoadPanicInternal(ctx, "in-memory compose content", &project, &err)

	if strings.TrimSpace(opts.ComposeContent) == "" {
		return nil, errors.New("compose content is required")
	}
	composeContent := opts.ComposeContent

	workingDir := strings.TrimSpace(opts.WorkingDir)
	if workingDir == "" {
		workingDir, err = os.Getwd()
		if err != nil {
			workingDir = os.TempDir()
		}
	}

	envMap := allowedProcessEnvInternal()
	if strings.TrimSpace(opts.EnvContent) != "" {
		parsedEnv, parseErr := ParseProjectEnvContent(opts.EnvContent, envMap)
		if parseErr != nil {
			return nil, errors.WrapIf(parseErr, "failed to parse env content")
		}
		maps.Copy(envMap, parsedEnv)
	}
	envMap["PWD"] = workingDir

	configFiles := []composetypes.ConfigFile{{Content: []byte(composeContent)}}
	if strings.TrimSpace(opts.OverrideContent) != "" {
		configFiles = append(configFiles, composetypes.ConfigFile{Content: []byte(opts.OverrideContent)})
	}

	configDetails := composetypes.ConfigDetails{
		Version:     api.ComposeVersion,
		WorkingDir:  workingDir,
		ConfigFiles: configFiles,
		Environment: composetypes.Mapping(envMap),
	}

	loaderOptions := []func(*loader.Options){func(loaderOpts *loader.Options) {
		if projectName := strings.TrimSpace(opts.ProjectName); projectName != "" {
			loaderOpts.SetProjectName(projectName, true)
		}
		loader.WithDiscardEnvFiles(loaderOpts)
	}}

	project, err = loader.LoadWithContext(ctx, configDetails, loaderOptions...)
	if err != nil {
		return nil, errors.WrapIf(err, "failed to load compose project")
	}

	var rawSources map[string]string
	if !isNilVolumeSourcePathMapperInternal(opts.PathMapper) {
		rawSources = harvestRawSourcesInternal(ctx, configDetails, loaderOptions...)
	}

	return finishLoadedProjectInternal(ctx, project, workingDir, opts.PathMapper, false, rawSources)
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
		addIfAbsent(tree.NewPath("services", tree.PathMatchAll, "deploy", "resources", section, "cpus"), func(value string) (any, error) {
			return strconv.ParseFloat(value, 64)
		})
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
		return nil, errors.Errorf("size %d out of range for platform int", n)
	}
	return int(n), nil
}

const lenientUndefinedPlaceholder = "/placeholder-undefined"

// ApplyLenientLoaderOptions configures a compose loader.Options for tolerant
// loading: structural validation and consistency checks are skipped, and
// unresolvable ${VAR} references are substituted with a placeholder instead of
// an empty string (which would otherwise produce invalid volume/bind specs
// like ":/path"). Callers use this when a .env file may not yet exist or may
// not define every variable the compose file references.
//
// The placeholder is applied at the substitution level, not the lookup level:
// the lookup must keep reporting undefined variables as missing so compose-go
// resolves default operators (${VAR:-default}, ${VAR-default}) normally.
// Reporting the placeholder as the variable's value would suppress those
// defaults and feed the placeholder into fields like ports host entries.
func ApplyLenientLoaderOptions(ctx context.Context, opts *loader.Options, composeFile string) {
	opts.SkipValidation = true
	opts.SkipConsistencyCheck = true
	if opts.Interpolate == nil {
		slog.WarnContext(ctx, "compose loader did not initialize Interpolate options; lenient variable substitution will not apply", "compose_file", composeFile)
		return
	}

	opts.Interpolate = &interp.Options{
		LookupValue:     opts.Interpolate.LookupValue,
		TypeCastMapping: wrapTypeCastMappingLenientInternal(opts.Interpolate.TypeCastMapping),
		Substitute: func(tmpl string, mapping template.Mapping) (string, error) {
			return template.SubstituteWithOptions(tmpl, mapping,
				template.WithoutLogging,
				template.WithReplacementFunction(func(substring string, mapping template.Mapping, cfg *template.Config) (string, error) {
					value, applied, err := template.DefaultReplacementAppliedFunc(substring, mapping, cfg)
					var missingErr *template.MissingRequiredError
					switch {
					case err == nil && applied:
						return value, nil
					case err != nil && !errors.As(err, &missingErr):
						return "", err
					default:
						// Variable is unset with no default (or a ${VAR:?msg}
						// required variable, which lenient loading tolerates).
						slog.DebugContext(ctx, "compose variable undefined during lenient load, using placeholder", "expression", substring, "compose_file", composeFile)
						return lenientUndefinedPlaceholder, nil
					}
				}),
			)
		},
	}
}

// LoadComposeProject loads a compose project from composeFile. envOverride and
// configureLoader are optional and may be nil. When lenient is true, undefined
// ${VAR} references are tolerated: instead of substituting them with an empty
// string (which produces invalid volume/bind specs like ":/path"), they are
// replaced with a placeholder value so structural validation can succeed. This
// is useful during GitSync validation where a .env file may not yet exist.
func LoadComposeProject(
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
	defer recoverComposeLoadPanicInternal(ctx, composeFile, &project, &err)

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

	// Deployment-relevant COMPOSE_* variables (COMPOSE_FILE, COMPOSE_PROFILES,
	// COMPOSE_PROJECT_NAME) come from the merged project environment.
	envOpts, envOptsErr := ParseComposeEnvOptions(workdir, fullEnvMap)
	if envOptsErr != nil {
		return nil, envOptsErr
	}

	// Pass full environment to compose-go for interpolation, compose-go will use this for ${VAR} expansion in the compose file
	var configFiles []composetypes.ConfigFile
	switch {
	case len(envOpts.ConfigFiles) > 0:
		// COMPOSE_FILE selects the exact file set (first entry is the base, later
		// entries merge over it). As `docker compose` does for an explicit `-f`,
		// no override file is auto-loaded.
		for _, f := range envOpts.ConfigFiles {
			configFiles = append(configFiles, composetypes.ConfigFile{Filename: f})
		}
		if !slices.Contains(envOpts.ConfigFiles, composeFile) {
			slog.DebugContext(ctx, "COMPOSE_FILE selection supersedes resolved compose file", "resolved", composeFile, "selection", envOpts.ConfigFiles)
		}
	default:
		configFiles = []composetypes.ConfigFile{{Filename: composeFile}}
		// Merge a Docker Compose override file (compose.override.yaml, etc.) when
		// present alongside the base file, as `docker compose` does. Only auto-load an
		// override when the base was discovered by its standard filename: `docker
		// compose` never auto-loads overrides for an explicit custom `-f` file, and
		// our custom-YAML fallback scan (e.g. mystack.yaml) is that case. Listing the
		// override after the base makes it take precedence.
		if slices.Contains(composeFileCandidates, filepath.Base(composeFile)) {
			if overrideFile := DetectComposeOverrideFile(workdir); overrideFile != "" {
				configFiles = append(configFiles, composetypes.ConfigFile{Filename: overrideFile})
				slog.DebugContext(ctx, "merging compose override file", "base", composeFile, "override", overrideFile)
			}
		}
	}

	cfg := composetypes.ConfigDetails{
		Version:     api.ComposeVersion,
		WorkingDir:  workdir,
		ConfigFiles: configFiles,
		Environment: composetypes.Mapping(fullEnvMap),
	}

	loaderOptions := []func(*loader.Options){func(opts *loader.Options) {
		// A caller-supplied name acts like `docker compose -p`, which outranks
		// COMPOSE_PROJECT_NAME. Only when no name is forced (e.g. the sync
		// metadata path passes "") does COMPOSE_PROJECT_NAME take effect.
		switch {
		case projectName != "":
			opts.SetProjectName(projectName, false)
		case envOpts.ProjectName != "":
			opts.SetProjectName(loader.NormalizeProjectName(envOpts.ProjectName), true)
		}
		if len(envOpts.Profiles) > 0 {
			loader.WithProfiles(envOpts.Profiles)(opts)
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
	}}

	project, err = loader.LoadWithContext(ctx, cfg, loaderOptions...)
	if err != nil {
		return nil, errors.WrapIf(err, "load compose project")
	}

	for _, configFile := range cfg.ConfigFiles {
		project.ComposeFiles = append(project.ComposeFiles, configFile.Filename)
	}

	var rawSources map[string]string
	if !isNilVolumeSourcePathMapperInternal(pathMapper) {
		rawSources = harvestRawSourcesInternal(ctx, cfg, loaderOptions...)
	}

	project, err = finishLoadedProjectInternal(ctx, project, workdir, pathMapper, true, rawSources)
	if err != nil {
		return nil, err
	}

	injectServiceConfiguration(project, injectionVars)
	return project, nil
}

func finishLoadedProjectInternal(ctx context.Context, project *composetypes.Project, workingDir string, pathMapper projecttypes.VolumeSourcePathMapper, translateFileResources bool, rawSources map[string]string) (*composetypes.Project, error) {
	project = project.WithoutUnnecessaryResources()
	ResolveRelativeProjectPaths(project, workingDir)

	if !isNilVolumeSourcePathMapperInternal(pathMapper) {
		if err := pathMapper.TranslateVolumeSources(project, translateFileResources); err != nil {
			return nil, errors.WrapIf(err, "failed to translate paths for docker host")
		}
		RemapEscapedRelativeSources(ctx, pathMapper, project, workingDir, rawSources, translateFileResources)
	}

	return project, nil
}

// harvestRawSourcesInternal re-parses the same Compose input with path
// resolution disabled, so the raw still-relative paths survive for
// RemapEscapedRelativeSources. The resolved project on its own cannot tell a
// relative path that escaped the projects mount from an intentionally absolute
// one, and only the former may be re-resolved against the host directory.
//
// Best effort by design: any failure yields a nil map and the caller then
// behaves exactly as it did before. Environment resolution is skipped because
// env_file paths are left relative here and would otherwise be read against the
// process working directory; label_file is read regardless, which is one of the
// reasons this cannot be fatal.
func harvestRawSourcesInternal(ctx context.Context, cfg composetypes.ConfigDetails, loaderOptions ...func(*loader.Options)) (rawSources map[string]string) {
	defer func() {
		if panicErr := emperror.Recover(recover()); panicErr != nil {
			slog.DebugContext(ctx, "panic while harvesting raw compose paths", "error", panicErr)
			rawSources = nil
		}
	}()

	options := append(slices.Clone(loaderOptions), func(opts *loader.Options) {
		opts.ResolvePaths = false
		opts.SkipResolveEnvironment = true
		opts.SkipValidation = true
		opts.SkipConsistencyCheck = true
	})

	project, err := loader.LoadWithContext(ctx, cfg, options...)
	if err != nil {
		slog.DebugContext(ctx, "unable to harvest raw compose paths; relative paths outside the projects mount may resolve incorrectly", "error", err)
		return nil
	}

	rawSources = make(map[string]string)
	for name, service := range project.Services {
		for _, volume := range service.Volumes {
			if volume.Type == composetypes.VolumeTypeBind && volume.Source != "" {
				rawSources[VolumeSourceKey(name, volume.Target)] = volume.Source
			}
		}
	}

	for name, volume := range project.Volumes {
		if device, ok := bindVolumeDeviceInternal(volume); ok {
			rawSources["volume_device:"+name] = device
		}
	}

	for name, secret := range project.Secrets {
		if secret.File != "" {
			rawSources["secret:"+name] = secret.File
		}
	}

	for name, config := range project.Configs {
		if config.File != "" {
			rawSources["config:"+name] = config.File
		}
	}

	return rawSources
}

func isNilVolumeSourcePathMapperInternal(pathMapper projecttypes.VolumeSourcePathMapper) bool {
	if pathMapper == nil {
		return true
	}

	value := reflect.ValueOf(pathMapper)
	return value.Kind() == reflect.Pointer && value.IsNil()
}

func recoverComposeLoadPanicInternal(ctx context.Context, source string, project **composetypes.Project, err *error) {
	if panicErr := emperror.Recover(recover()); panicErr != nil {
		slog.WarnContext(ctx,
			"panic while loading compose project; compose file may contain invalid syntax",
			"path", source,
			"error", panicErr,
		)
		*err = errors.WrapIff(panicErr, "load compose project panic for %s", source)
		*project = nil
	}
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
	composeFile, err := DetectComposeFile(ctx, projectsDirectory, dir)
	if err != nil {
		return nil, "", err
	}

	proj, err := LoadComposeProject(ctx, composeFile, projectName, projectsDirectory, autoInjectEnv, pathMapper, nil, nil, false)
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
				if resolved, ok := resolvePathRelative(workdir, v.Source).Get(); ok {
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
		if resolved, ok := resolvePathRelative(workdir, secret.File).Get(); ok {
			secret.File = resolved
			project.Secrets[name] = secret
		}
	}

	for name, config := range project.Configs {
		if resolved, ok := resolvePathRelative(workdir, config.File).Get(); ok {
			config.File = resolved
			project.Configs[name] = config
		}
	}
}

func resolvePathRelative(workdir, candidate string) mo.Option[string] {
	if candidate == "" || filepath.IsAbs(candidate) || workdir == "" {
		return mo.None[string]()
	}
	return mo.Some(filepath.Clean(filepath.Join(workdir, candidate)))
}
