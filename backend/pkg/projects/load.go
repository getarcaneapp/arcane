package projects

import (
	"context"
	"fmt"
	"log/slog"
	"maps"
	"os"
	"path/filepath"
	"slices"

	"github.com/compose-spec/compose-go/v2/loader"
	composetypes "github.com/compose-spec/compose-go/v2/types"
	"github.com/docker/compose/v5/pkg/api"
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

// IsProjectFile reports whether filename is a Compose file or an environment file.
func IsProjectFile(filename string) bool {
	return slices.Contains(ProjectFileCandidates, filename)
}

func locateComposeFile(dir string) string {
	for _, filename := range ProjectFileCandidates {
		if filename == ".env" {
			continue
		}
		fullPath := filepath.Join(dir, filename)
		if info, err := os.Stat(fullPath); err == nil && !info.IsDir() {
			return fullPath
		}
	}
	return ""
}

func DetectComposeFile(dir string) (string, error) {
	compose := locateComposeFile(dir)
	if compose == "" {
		return "", fmt.Errorf("no compose file found in %q", dir)
	}
	return compose, nil
}

func LoadComposeProject(ctx context.Context, composeFile, projectName, projectsDirectory string, autoInjectEnv bool, pathMapper *PathMapper) (*composetypes.Project, error) {
	return loadComposeProjectInternal(ctx, composeFile, projectName, projectsDirectory, autoInjectEnv, pathMapper, nil, nil)
}

// serviceOrigin records where a compose service was actually defined, so we
// can stamp accurate `com.docker.compose.project.working_dir` and
// `com.docker.compose.project.config_files` labels on its container. For
// services pulled in via an `include:` directive with a `project_directory`,
// the working_dir of the *include* (not the top-level compose file) is what
// resolves the service's relative bind-mount paths. Stamping the wrong
// working_dir causes compose to recreate the container with bind mounts
// rooted at the wrong base — which has destroyed databases in the wild
// (see issue #2264).
type serviceOrigin struct {
	WorkingDir  string
	ComposeFile string
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
		if configureLoader != nil {
			configureLoader(opts)
		}
	})
	if err != nil {
		return nil, fmt.Errorf("load compose project: %w", err)
	}

	project = project.WithoutUnnecessaryResources()

	// Resolve relative paths for bind mounts, secrets, and configs
	resolveRelativeProjectPaths(project, workdir)

	// Translate container paths to host paths for Docker execution
	if pathMapper != nil {
		if err := pathMapper.TranslateVolumeSources(project); err != nil {
			return nil, fmt.Errorf("failed to translate paths for docker host: %w", err)
		}
	}

	// Build a per-service origin map by walking `include:` directives, so
	// services pulled in via includes are labeled with the include's own
	// working_dir/compose_file rather than the top-level compose file's.
	originMap := buildServiceOriginMapInternal(ctx, composeFile, fullEnvMap)

	injectServiceConfiguration(project, injectionVars, workdir, composeFile, originMap)

	project.ComposeFiles = []string{composeFile}
	return project, nil
}

func applyCustomLabelsInternal(projectName string, serviceName string, workingDirectory string, composeFile string) composetypes.Labels {
	return composetypes.Labels{
		api.ProjectLabel:     projectName,
		api.ServiceLabel:     serviceName,
		api.VersionLabel:     api.ComposeVersion,
		api.OneoffLabel:      "False",
		api.WorkingDirLabel:  workingDirectory,
		api.ConfigFilesLabel: composeFile,
	}
}

func injectServiceConfiguration(project *composetypes.Project, injectionVars EnvMap, workdir, composeFile string, origins map[string]serviceOrigin) {
	for i, s := range project.Services {
		// Default to the top-level compose file's directory + path. For
		// services that came from an `include:` with its own
		// project_directory, override with the include's own working dir
		// so that compose's reconcile/recreate uses the right base for
		// relative bind-mount paths. See issue #2264.
		svcWorkdir := workdir
		svcComposeFile := composeFile
		if origin, ok := origins[s.Name]; ok && origin.WorkingDir != "" {
			svcWorkdir = origin.WorkingDir
			if origin.ComposeFile != "" {
				svcComposeFile = origin.ComposeFile
			}
		}
		s.CustomLabels = applyCustomLabelsInternal(project.Name, s.Name, svcWorkdir, svcComposeFile)

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

func resolveRelativeProjectPaths(project *composetypes.Project, workdir string) {
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
