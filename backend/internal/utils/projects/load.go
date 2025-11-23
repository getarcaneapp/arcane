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

func LoadComposeProject(ctx context.Context, composeFile, projectName, projectsDirectory string) (*composetypes.Project, error) {
	workdir := filepath.Dir(composeFile)

	projectsDir := projectsDirectory
	if projectsDir == "" {
		projectsDir = filepath.Dir(workdir)
	}

	envLoader := NewEnvLoader(projectsDir, workdir)

	// Load full environment (process + global + project .env) for service injection
	fullEnvMap, injectionVars, err := envLoader.LoadEnvironment(ctx)
	if err != nil {
		slog.WarnContext(ctx, "Failed to load environment", "error", err)
	}

	// Pass full environment to compose-go for interpolation
	// compose-go will use this for ${VAR} expansion in the compose file
	cfg := composetypes.ConfigDetails{
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

	injectServiceConfiguration(project, injectionVars, workdir, composeFile)

	project.ComposeFiles = []string{composeFile}
	return project, nil
}

func injectServiceConfiguration(project *composetypes.Project, injectionVars EnvMap, workdir, composeFile string) {
	for i, s := range project.Services {
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

		if s.CustomLabels == nil {
			s.CustomLabels = composetypes.Labels{}
		}

		s.CustomLabels[api.ProjectLabel] = project.Name
		s.CustomLabels[api.ServiceLabel] = s.Name
		s.CustomLabels[api.VersionLabel] = api.ComposeVersion
		s.CustomLabels[api.OneoffLabel] = "False"
		s.CustomLabels[api.WorkingDirLabel] = workdir
		s.CustomLabels[api.ConfigFilesLabel] = composeFile

		project.Services[i] = s
	}
}

func LoadComposeProjectFromDir(ctx context.Context, dir, projectName, projectsDirectory string) (*composetypes.Project, string, error) {
	composeFile, err := DetectComposeFile(dir)
	if err != nil {
		return nil, "", err
	}

	if projectsDirectory == "" {
		projectsDirectory = filepath.Dir(dir)
	}

	proj, err := LoadComposeProject(ctx, composeFile, projectName, projectsDirectory)
	if err != nil {
		return nil, "", err
	}

	return proj, composeFile, nil
}
