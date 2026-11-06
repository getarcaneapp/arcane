package projects

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"maps"
	"math"
	"os"
	"path/filepath"
	"reflect"
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
	"github.com/samber/mo"

	"kit.getarcane.app/concurrency"

	"getarcane.dev/arcane/v3/internal/common"
	"getarcane.dev/types/v3"
)

type (
	PrepareProjectFunc func(ctx context.Context, project *composetypes.Project) error

	LoadOptions struct {
		PathMapper        *PathMapper
		EnvOverride       EnvMap
		ConfigureLoader   func(*loader.Options)
		Dependencies      *types.ComposeDependencies
		Prepare           PrepareProjectFunc
		ComposeFile       string
		ProjectName       string
		ProjectsDirectory string
		Services          []string
		AutoInjectEnv     bool
		Lenient           bool
	}

	ValidateComposeRequest struct {
		ProjectsDirectory string
		ProjectPath       string
		ProjectName       string
		ComposeContent    string
		EnvContent        *string
		OverrideContent   *string
		OverrideFileName  string
		Lenient           bool
	}
)

const (
	lenientUndefinedPlaceholder = "/placeholder-undefined"
)

var ProjectFileCandidates = append(
	append(slices.Clone(ComposeFileCandidates), ComposeOverrideFileCandidates...),
	".env",
)

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

func stripTrailingProjectCounter(name string) string {
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

func DetectComposeFile(ctx context.Context, projectsDir, dir string) (string, error) {
	var envErr error
	if files, err := ComposeFileEnvSelection(ctx, projectsDir, dir); err != nil {
		if !errors.Is(err, common.ErrProjectEnvUnreadable) {
			return "", err
		}
		envErr = err
	} else if len(files) > 0 {
		return files[0], nil
	}

	// Stays on os.*: compose files may be symlinks resolving outside any
	// confinement root, and dir itself can be an imported project living
	// outside the projects directory; acfs cannot follow either.
	for _, filename := range ComposeFileCandidates {
		composePath := filepath.Join(dir, filename)
		if info, err := os.Stat(composePath); err == nil && !info.IsDir() {
			return composePath, envErr
		}
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		return "", err
	}

	dirBase := filepath.Base(filepath.Clean(dir))
	normalizedDirBase := loader.NormalizeProjectName(dirBase)
	normalizedTrimmedDirBase := loader.NormalizeProjectName(stripTrailingProjectCounter(dirBase))

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
		return dirMatchedCandidates[0], envErr
	case len(dirMatchedCandidates) > 1:
		return "", fmt.Errorf("multiple custom compose files found in %q", dir)

	case len(composeNamedCandidates) == 1:
		return composeNamedCandidates[0], envErr
	case len(composeNamedCandidates) > 1:
		return "", fmt.Errorf("multiple custom compose files found in %q", dir)

	case len(customCandidates) == 1:
		return customCandidates[0], envErr
	case len(customCandidates) > 1:
		return "", fmt.Errorf("multiple custom compose files found in %q", dir)

	default:
		return "", common.Classify(common.ErrComposeFileNotFound, fmt.Errorf("no compose file found in %q", dir))
	}
}

func LoadComposeProjectFromContent(ctx context.Context, opts types.ComposeContentOptions) (project *composetypes.Project, err error) {
	defer recoverComposeLoadPanic(ctx, "in-memory compose content", &project, &err)

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

	envMap := allowedProcessEnv()
	if strings.TrimSpace(opts.EnvContent) != "" {
		parsedEnv, parseErr := ParseProjectEnvContent(opts.EnvContent, envMap)
		if parseErr != nil {
			return nil, fmt.Errorf("failed to parse env content: %w", parseErr)
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
		return nil, fmt.Errorf("failed to load compose project: %w", err)
	}

	var rawSources map[string]string
	if !isNilVolumeSourcePathMapper(opts.PathMapper) {
		rawSources = harvestRawSources(ctx, configDetails, loaderOptions...)
	}

	return finishLoadedProject(ctx, project, workingDir, opts.PathMapper, false, rawSources, nil)
}

func wrapTypeCastMappingLenient(mapping map[tree.Path]interp.Cast) map[tree.Path]interp.Cast {
	wrapped := make(map[tree.Path]interp.Cast, len(mapping)+4)
	for path, original := range mapping {
		wrapped[path] = wrapCastWithLenientFallback(original)
	}
	addIfAbsent := func(path tree.Path, cast interp.Cast) {
		if _, exists := wrapped[path]; exists {
			return
		}
		wrapped[path] = wrapCastWithLenientFallback(cast)
	}
	for _, section := range []string{"limits", "reservations"} {
		addIfAbsent(tree.NewPath("services", tree.PathMatchAll, "deploy", "resources", section, "cpus"), func(value string) (any, error) {
			return strconv.ParseFloat(value, 64)
		})
		addIfAbsent(tree.NewPath("services", tree.PathMatchAll, "deploy", "resources", section, "memory"), lenientCastSize)
	}
	return wrapped
}

func wrapCastWithLenientFallback(original interp.Cast) interp.Cast {
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

func lenientCastSize(value string) (any, error) {
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

func ApplyLenientLoaderOptions(ctx context.Context, opts *loader.Options, composeFile string) {
	opts.SkipValidation = true
	opts.SkipConsistencyCheck = true
	if opts.Interpolate == nil {
		slog.WarnContext(ctx, "compose loader did not initialize Interpolate options; lenient variable substitution will not apply", "composeFile", composeFile)
		return
	}

	opts.Interpolate = &interp.Options{
		LookupValue:     opts.Interpolate.LookupValue,
		TypeCastMapping: wrapTypeCastMappingLenient(opts.Interpolate.TypeCastMapping),
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
						slog.DebugContext(ctx, "compose variable undefined during lenient load, using placeholder", "expression", substring, "composeFile", composeFile)
						return lenientUndefinedPlaceholder, nil
					}
				}),
			)
		},
	}
}

func LoadComposeProject(ctx context.Context, opts LoadOptions) (project *composetypes.Project, err error) {
	defer recoverComposeLoadPanic(ctx, opts.ComposeFile, &project, &err)

	workdir := filepath.Dir(opts.ComposeFile)

	envLoader := NewEnvLoader(opts.ProjectsDirectory, workdir, opts.AutoInjectEnv)

	fullEnvMap, injectionVars, err := envLoader.LoadEnvironment(ctx)
	if err != nil {
		if errors.Is(err, common.ErrProjectEnvUnreadable) {
			return nil, err
		}
		slog.WarnContext(ctx, "Failed to load environment", "error", err)
	}

	// Override wins: maps.Copy(dst, src) copies src into dst.
	maps.Copy(fullEnvMap, opts.EnvOverride)

	if absWorkdir, absErr := filepath.Abs(workdir); absErr == nil {
		fullEnvMap["PWD"] = absWorkdir
	} else {
		slog.WarnContext(ctx, "Failed to set PWD environment variable", "workdir", workdir, "error", absErr)
	}

	envOpts, envOptsErr := ParseComposeEnvOptions(workdir, fullEnvMap)
	if envOptsErr != nil {
		return nil, envOptsErr
	}

	var configFiles []composetypes.ConfigFile
	switch {
	case len(envOpts.ConfigFiles) > 0:
		// COMPOSE_FILE selects the exact file set (first entry is the base, later
		// entries merge over it). As `docker compose` does for an explicit `-f`,
		// no override file is auto-loaded.
		for _, f := range envOpts.ConfigFiles {
			configFiles = append(configFiles, composetypes.ConfigFile{Filename: f})
		}
		if !slices.Contains(envOpts.ConfigFiles, opts.ComposeFile) {
			slog.DebugContext(ctx, "COMPOSE_FILE selection supersedes resolved compose file", "resolved", opts.ComposeFile, "selection", envOpts.ConfigFiles)
		}
	default:
		configFiles = []composetypes.ConfigFile{{Filename: opts.ComposeFile}}
		if slices.Contains(ComposeFileCandidates, filepath.Base(opts.ComposeFile)) {
			if overrideFile := DetectComposeOverrideFile(workdir); overrideFile != "" {
				configFiles = append(configFiles, composetypes.ConfigFile{Filename: overrideFile})
				slog.DebugContext(ctx, "merging compose override file", "base", opts.ComposeFile, "override", overrideFile)
			}
		}
	}

	cfg := composetypes.ConfigDetails{
		Version:     api.ComposeVersion,
		WorkingDir:  workdir,
		ConfigFiles: configFiles,
		Environment: composetypes.Mapping(fullEnvMap),
	}

	loaderOptions := []func(*loader.Options){func(loaderOpts *loader.Options) {
		// A caller-supplied name acts like `docker compose -p`, which outranks
		// COMPOSE_PROJECT_NAME. Only when no name is forced (e.g. the sync
		// metadata path passes "") does COMPOSE_PROJECT_NAME take effect.
		switch {
		case opts.ProjectName != "":
			loaderOpts.SetProjectName(opts.ProjectName, false)
		case envOpts.ProjectName != "":
			loaderOpts.SetProjectName(loader.NormalizeProjectName(envOpts.ProjectName), true)
		}
		if len(envOpts.Profiles) > 0 {
			loader.WithProfiles(envOpts.Profiles)(loaderOpts)
		}
		// Discard env_file after folding into environment, as the compose CLI
		// does, so config-hashes match and both tools stop recreating services.
		if opts.Dependencies == nil {
			loader.WithDiscardEnvFiles(loaderOpts)
		}
		if opts.Lenient {
			ApplyLenientLoaderOptions(ctx, loaderOpts, opts.ComposeFile)
		}
		if opts.ConfigureLoader != nil {
			opts.ConfigureLoader(loaderOpts)
		}
		// Select before environment resolution and Arcane resource/path preparation.
		loader.WithSelectedServices(opts.Services)(loaderOpts)
	}}

	project, err = loader.LoadWithContext(ctx, cfg, loaderOptions...)
	if err != nil {
		return nil, fmt.Errorf("load compose project: %w", err)
	}

	recordComposeDependencies(project, opts.Dependencies)

	for _, configFile := range cfg.ConfigFiles {
		project.ComposeFiles = append(project.ComposeFiles, configFile.Filename)
	}

	var rawSources map[string]string
	if !isNilVolumeSourcePathMapper(opts.PathMapper) {
		rawSources = harvestRawSources(ctx, cfg, loaderOptions...)
	}

	project, err = finishLoadedProject(ctx, project, workdir, opts.PathMapper, true, rawSources, opts.Prepare)
	if err != nil {
		return nil, err
	}

	injectServiceConfiguration(project, injectionVars)
	return project, nil
}

func finishLoadedProject(ctx context.Context,
	project *composetypes.Project,
	workingDir string,
	pathMapper types.VolumeSourcePathMapper,
	translateFileResources bool,
	rawSources map[string]string,
	prepare PrepareProjectFunc) (*composetypes.Project,
	error,
) {
	if err := applyServiceLabelMetadata(project); err != nil {
		return nil, err
	}
	project = project.WithoutUnnecessaryResources()
	ResolveRelativeProjectPaths(project, workingDir)

	// Sources are local absolute paths here; host translation below rewrites
	// them for the Docker daemon and they would no longer be usable locally.
	if prepare != nil {
		if err := prepare(ctx, project); err != nil {
			return nil, fmt.Errorf("prepare compose project: %w", err)
		}
	}

	if !isNilVolumeSourcePathMapper(pathMapper) {
		if err := pathMapper.TranslateVolumeSources(project, translateFileResources); err != nil {
			return nil, fmt.Errorf("failed to translate paths for docker host: %w", err)
		}
		RemapEscapedRelativeSources(ctx, pathMapper, project, workingDir, rawSources, translateFileResources)
	}

	return project, nil
}

func harvestRawSources(ctx context.Context, cfg composetypes.ConfigDetails, loaderOptions ...func(*loader.Options)) (rawSources map[string]string) {
	defer func() {
		if panicErr := concurrency.PanicToError(recover()); panicErr != nil {
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
				rawSources[("volume:" + name + "\x00" + filepath.Clean(volume.Target))] = volume.Source
			}
		}
	}

	for name, volume := range project.Volumes {
		if device, ok := bindVolumeDevice(volume); ok {
			rawSources["volume_device:"+name] = device
		}
	}

	for name, secret := range project.Secrets {
		rawSources["secret:"+name] = cmp.Or(secret.File, rawSources["secret:"+name])
	}

	for name, config := range project.Configs {
		rawSources["config:"+name] = cmp.Or(config.File, rawSources["config:"+name])
	}

	return rawSources
}

func isNilVolumeSourcePathMapper(pathMapper types.VolumeSourcePathMapper) bool {
	if pathMapper == nil {
		return true
	}

	value := reflect.ValueOf(pathMapper)
	return value.Kind() == reflect.Pointer && value.IsNil()
}

//nolint:gocritic // Pointer output updates the caller's named error result after recovery.
func recoverComposeLoadPanic(ctx context.Context,
	source string,
	project **composetypes.Project,
	err *error,
) {
	if panicErr := concurrency.PanicToError(recover()); panicErr != nil {
		slog.WarnContext(ctx,
			"panic while loading compose project; compose file may contain invalid syntax",
			"path", source,
			"error", panicErr,
		)
		*err = fmt.Errorf("load compose project panic for %s: %w", source, panicErr)
		*project = nil
	}
}

func applyCustomLabels(projectName, serviceName, workingDirectory string, composeFiles []string) composetypes.Labels {
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
		s.CustomLabels = applyCustomLabels(project.Name, s.Name, project.WorkingDir, project.ComposeFiles)

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

func LoadComposeProjectFromDir(ctx context.Context, dir string, opts LoadOptions) (*composetypes.Project, string, error) {
	composeFile, err := DetectComposeFile(ctx, opts.ProjectsDirectory, dir)
	if err != nil {
		return nil, "", err
	}

	proj, err := LoadComposeProject(ctx, LoadOptions{
		ComposeFile:       composeFile,
		ProjectName:       opts.ProjectName,
		ProjectsDirectory: opts.ProjectsDirectory,
		AutoInjectEnv:     opts.AutoInjectEnv,
		PathMapper:        opts.PathMapper,
		EnvOverride:       opts.EnvOverride,
		ConfigureLoader:   opts.ConfigureLoader,
		Lenient:           opts.Lenient,
		Dependencies:      opts.Dependencies,
		Services:          opts.Services,
		Prepare:           opts.Prepare,
	})
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

func recordComposeDependencies(project *composetypes.Project, dependencies *types.ComposeDependencies) {
	if dependencies != nil {
		for name, service := range project.Services {
			for _, envFile := range service.EnvFiles {
				dependencies.EnvFiles = append(dependencies.EnvFiles, envFile.Path)
			}
			dependencies.EnvFiles = append(dependencies.EnvFiles, service.LabelFiles...)
			service.EnvFiles = nil
			project.Services[name] = service
		}
	}
}

// Missing includes are tolerated here; deployment uses strict executable loading.
func ValidateComposeContentForUpdate(ctx context.Context, req ValidateComposeRequest) (err error) {
	defer func() {
		if panicErr := concurrency.PanicToError(recover()); panicErr != nil {
			err = fmt.Errorf("compose file contains invalid syntax: %w", panicErr)
		}
	}()

	fullEnvMap, envErr := BuildValidationEnvironment(ctx, req.ProjectsDirectory, req.ProjectPath, req.EnvContent)
	if envErr != nil {
		return envErr
	}

	// COMPOSE_FILE in the (edited) env selects the exact file set, so validate
	// against it — a save that introduces a broken selection fails fast.
	envOpts, envOptsErr := ParseComposeEnvOptions(req.ProjectPath, fullEnvMap)
	if envOptsErr != nil {
		return envOptsErr
	}

	validationProjectName := NormalizeProjectName(req.ProjectName)
	var configFiles []composetypes.ConfigFile
	switch {
	case len(envOpts.ConfigFiles) > 0:
		overrideName := cmp.Or(strings.TrimSpace(req.OverrideFileName), DefaultComposeOverrideFileName)
		overridePath := ""
		if absProjectPath, absErr := filepath.Abs(filepath.Clean(req.ProjectPath)); absErr == nil {
			overridePath = filepath.Join(absProjectPath, overrideName)
		}
		for i, f := range envOpts.ConfigFiles {
			switch {
			case i == 0:
				configFiles = append(configFiles, composetypes.ConfigFile{Filename: f, Content: []byte(req.ComposeContent)})
			case req.OverrideContent != nil && overridePath != "" && f == overridePath:
				configFiles = append(configFiles, composetypes.ConfigFile{Filename: f, Content: []byte(*req.OverrideContent)})
			default:
				configFiles = append(configFiles, composetypes.ConfigFile{Filename: f})
			}
		}
	default:
		configFiles = []composetypes.ConfigFile{
			{Filename: filepath.Join(req.ProjectPath, "compose.yaml"), Content: []byte(req.ComposeContent)},
		}
		if req.OverrideContent != nil {
			overrideName := cmp.Or(strings.TrimSpace(req.OverrideFileName), DefaultComposeOverrideFileName)
			configFiles = append(configFiles, composetypes.ConfigFile{
				Filename: filepath.Join(req.ProjectPath, overrideName),
				Content:  []byte(*req.OverrideContent),
			})
		}
	}

	cfg := composetypes.ConfigDetails{
		Version:     api.ComposeVersion,
		WorkingDir:  req.ProjectPath,
		ConfigFiles: configFiles,
		Environment: composetypes.Mapping(fullEnvMap),
	}

	missingIncludeLoader := &MissingIncludeStubLoader{projectPath: req.ProjectPath}
	defer missingIncludeLoader.Cleanup()

	err = WithTransientValidationEnvFile(ctx, req.ProjectPath, req.EnvContent, func() error {
		_, loadErr := loader.LoadWithContext(ctx, cfg, func(opts *loader.Options) {
			opts.ResourceLoaders = append([]loader.ResourceLoader{missingIncludeLoader}, opts.ResourceLoaders...)
			if validationProjectName != "" {
				opts.SetProjectName(validationProjectName, false)
			}
			if req.Lenient {
				ApplyLenientLoaderOptions(ctx, opts, cfg.ConfigFiles[0].Filename)
			}
		})
		return loadErr
	})

	return err
}
