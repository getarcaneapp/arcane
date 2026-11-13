package deployment

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"maps"
	"slices"

	composetypes "github.com/compose-spec/compose-go/v2/types"
	"github.com/docker/compose/v5/pkg/api"

	"kit.getarcane.app/acfs"
	buildtypes "kit.getarcane.app/builds/types"
	kitfs "kit.getarcane.app/fs"
	"kit.getarcane.app/updater/refs"

	"getarcane.dev/arcane/v3/internal/docker"
	"getarcane.dev/arcane/v3/internal/image"
	"getarcane.dev/arcane/v3/pkg/projects"
	"getarcane.dev/types/v3"
)

type (
	Builder interface {
		BuildImage(ctx context.Context, environmentID string, req buildtypes.BuildRequest, progressWriter io.Writer, serviceName string, user *types.Actor) (*buildtypes.BuildResult, error)
		BuildSettings() buildtypes.BuildSettings
	}

	Service struct {
		imageService  *image.ImageService
		dockerService *docker.DockerClientService
		buildService  Builder
	}
)

func New(imageService *image.ImageService, dockerService *docker.DockerClientService, buildService Builder) *Service {
	return &Service{
		imageService:  imageService,
		dockerService: dockerService,
		buildService:  buildService,
	}
}

func (s *Service) NamespaceDependents(ctx context.Context, compProj *composetypes.Project, services []string) (running, stopped []string) {
	if len(services) == 0 {
		return nil, nil
	}
	deps := projects.NamespaceDependents(compProj, services)
	if len(deps) == 0 {
		return nil, nil
	}
	summaries, err := projects.ComposePs(ctx, types.ComposePsOptions{
		Project:    compProj,
		DockerHost: s.dockerService.DockerHost(),
		Services:   deps,
		All:        true,
	})
	if err != nil {
		slog.WarnContext(ctx, "failed to list namespace dependent containers; recreating all dependents", "project", compProj.Name, "dependents", deps, "error", err)
		return deps, nil
	}
	for _, name := range deps {
		switch {
		case slices.ContainsFunc(summaries, func(summary api.ContainerSummary) bool {
			return summary.Service == name && summary.State == "running"
		}):
			running = append(running, name)
		case slices.ContainsFunc(summaries, func(summary api.ContainerSummary) bool {
			return summary.Service == name
		}):
			stopped = append(stopped, name)
		}
	}
	return running, stopped
}

func (s *Service) ImageOperations(user *types.Actor, credentials []types.ContainerRegistryCredential, pinnedImages map[string]string) types.ComposeImageOperations {
	return types.ComposeImageOperations{
		Exists:     s.imageService.ImageExistsLocally,
		LastTagged: s.imageService.ImageLastTagTime,
		Pull: func(ctx context.Context, ref string, progress io.Writer) error {
			return s.imageService.Pull(ctx, image.PullRequest{
				Ref:         cmp.Or(pinnedImages[refs.NormalizeImageUpdateRef(ref)], ref),
				Tag:         ref,
				Progress:    progress,
				User:        *cmp.Or(user, &types.SystemUser),
				Credentials: credentials,
			})
		},
		Build: func(ctx context.Context, request buildtypes.BuildRequest, progress io.Writer, service string) error {
			_, err := s.buildService.BuildImage(ctx, types.LocalDockerEnvironmentID, request, progress, service, user)
			return err
		},
		BuildProvider: s.buildService.BuildSettings().BuildProvider,
	}
}

// PrepareProjectBindDirectories creates missing bind sources as Arcane's user, not root (#4132).
func PrepareProjectBindDirectories(projectPath string) projects.PrepareProjectFunc {
	return func(ctx context.Context, project *composetypes.Project) error {
		for _, serviceName := range slices.Sorted(maps.Keys(project.Services)) {
			for _, volume := range project.Services[serviceName].Volumes {
				if volume.Type != composetypes.VolumeTypeBind || volume.Source == "" {
					continue
				}
				if volume.Bind != nil && !bool(volume.Bind.CreateHostPath) {
					continue
				}
				logicalPath, err := acfs.Root{Path: projectPath}.LogicalPath(volume.Source)
				if err == nil && logicalPath != "/" {
					exists, existsErr := acfs.Root{Path: projectPath}.Exists(ctx, logicalPath)
					switch {
					case exists || errors.Is(existsErr, acfs.ErrOutsideRoot) || errors.Is(existsErr, fs.ErrPermission):
					case existsErr != nil:
						err = existsErr
					default:
						if err = (acfs.Root{Path: projectPath}).MkdirAll(ctx, logicalPath, kitfs.DirPerm); err == nil {
							slog.InfoContext(ctx, "created missing bind directory for project deployment", "projectPath", projectPath, "source", volume.Source)
						}
					}
				}
				if err != nil && !errors.Is(err, acfs.ErrOutsideRoot) {
					return fmt.Errorf("bind source %s for service %s: %w", volume.Source, serviceName, err)
				}
			}
		}
		return nil
	}
}
