package projects

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"

	"github.com/compose-spec/compose-go/v2/loader"
	composetypes "github.com/compose-spec/compose-go/v2/types"
	"github.com/docker/compose/v5/pkg/api"
	"github.com/getarcaneapp/arcane/backend/internal/utils/pathmapper"
)

var ComposeFileCandidates = []string{
	"compose.yaml",
	"compose.yml",
	"docker-compose.yaml",
	"docker-compose.yml",
}

func locateComposeFile(dir string) string {
	for _, filename := range ComposeFileCandidates {
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

func LoadComposeProject(ctx context.Context, composeFile, projectName, projectsDirectory string, autoInjectEnv bool, pathMapper *pathmapper.PathMapper) (*composetypes.Project, error) {
	workdir := filepath.Dir(composeFile)

	projectsDir := projectsDirectory
	if projectsDir == "" {
		projectsDir = filepath.Dir(workdir)
	}

	envLoader := NewEnvLoader(projectsDir, workdir, autoInjectEnv)

	// Load full environment (process + global + project .env) for service injection
	fullEnvMap, injectionVars, err := envLoader.LoadEnvironment(ctx)
	if err != nil {
		slog.WarnContext(ctx, "Failed to load environment", "error", err)
	}

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

	project, err := loader.LoadWithContext(ctx, cfg, func(opts *loader.Options) {
		opts.SetProjectName(projectName, true)
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

	injectServiceConfiguration(project, injectionVars, workdir, composeFile)

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

func injectServiceConfiguration(project *composetypes.Project, injectionVars EnvMap, workdir, composeFile string) {
	for i, s := range project.Services {
		s.CustomLabels = applyCustomLabelsInternal(project.Name, s.Name, workdir, composeFile)

		// Initialize environment if nil
		if s.Environment == nil {
			s.Environment = make(composetypes.MappingWithEquals)
		}

		for k, v := range injectionVars {
			if _, exists := s.Environment[k]; !exists {
				vcopy := v
				s.Environment[k] = &vcopy
			}
		}

		project.Services[i] = s
	}
}

func LoadComposeProjectFromDir(ctx context.Context, dir, projectName, projectsDirectory string, autoInjectEnv bool, pathMapper *pathmapper.PathMapper) (*composetypes.Project, string, error) {
	composeFile, err := DetectComposeFile(dir)
	if err != nil {
		return nil, "", err
	}

	if projectsDirectory == "" {
		projectsDirectory = filepath.Dir(dir)
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
