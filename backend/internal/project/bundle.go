package project

import (
	"context"
	"fmt"

	composetypes "github.com/compose-spec/compose-go/v2/types"

	"getarcane.dev/arcane/v3/pkg/projects"
)

func (s *ProjectService) loadedComposeFiles(ctx context.Context, projectID string) ([]string, error) {
	projectsDirectory, err := s.GetProjectsDirectory(ctx)
	if err != nil {
		return nil, err
	}
	proj, err := s.GetProjectFromDatabaseByID(ctx, projectID)
	if err != nil {
		return nil, err
	}
	composeFile, err := s.ResolveProjectComposeFile(ctx, proj)
	if err != nil {
		return nil, fmt.Errorf("resolve the compose file of project %s: %w", proj.Name, err)
	}
	return projects.LoadedComposeFiles(ctx, projects.LoadedComposeFilesOptions{
		GlobalEnvDirectory: projectsDirectory,
		ProjectPath:        proj.Path,
		ComposeFile:        composeFile,
	}), nil
}

func (s *ProjectService) composeProject(ctx context.Context, projectID string) (*composetypes.Project, error) {
	proj, err := s.GetProjectFromDatabaseByID(ctx, projectID)
	if err != nil {
		return nil, err
	}
	return s.loadComposeProjectForProject(ctx, proj, nil)
}
