package projects

import (
	"context"
	"io"

	"github.com/compose-spec/compose-go/v2/types"
	"github.com/docker/compose/v5/pkg/api"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/filters"
)

type UpOptions struct {
	Services      []string
	Detach        bool
	Build         bool
	Wait          bool
	RemoveOrphans bool
}

func ComposeRestart(ctx context.Context, proj *types.Project, services []string) error {
	c, err := NewClient(ctx)
	if err != nil {
		return err
	}
	defer c.Close()
	return c.svc.Restart(ctx, proj.Name, api.RestartOptions{Services: services})
}

func ComposeUp(ctx context.Context, proj *types.Project, options UpOptions) error {
	c, err := NewClient(ctx)
	if err != nil {
		return err
	}
	defer c.Close()

	// Use specified services or all services if none specified
	targetServices := options.Services
	if len(targetServices) == 0 {
		targetServices = proj.ServiceNames()
	}

	upOptions := api.CreateOptions{
		Services:      targetServices,
		RemoveOrphans: options.RemoveOrphans,
	}

	// Only include build options if building is requested
	var build *api.BuildOptions
	if options.Build {
		build = &api.BuildOptions{
			Services: targetServices,
		}
		upOptions.Build = build
	}

	startOptions := api.StartOptions{
		Project:  proj,
		Services: targetServices,
		Wait:     options.Wait && !options.Detach, // Only wait if not detached
	}

	return c.svc.Up(ctx, proj, api.UpOptions{Create: upOptions, Start: startOptions})
}

func ComposePs(ctx context.Context, proj *types.Project, services []string, all bool) ([]api.ContainerSummary, error) {
	c, err := NewClient(ctx)
	if err != nil {
		return nil, err
	}
	defer c.Close()

	return c.svc.Ps(ctx, proj.Name, api.PsOptions{All: all})
}

func ComposeDown(ctx context.Context, proj *types.Project, removeVolumes bool) error {
	c, err := NewClient(ctx)
	if err != nil {
		return err
	}
	defer c.Close()

	return c.svc.Down(ctx, proj.Name, api.DownOptions{RemoveOrphans: true, Volumes: removeVolumes})
}

func ComposeLogs(ctx context.Context, projectName string, out io.Writer, follow bool, tail string) error {
	c, err := NewClient(ctx)
	if err != nil {
		return err
	}
	defer c.Close()

	return c.svc.Logs(ctx, projectName, writerConsumer{out: out}, api.LogOptions{Follow: follow, Tail: tail})
}

func ListGlobalComposeContainers(ctx context.Context) ([]container.Summary, error) {
	c, err := NewClient(ctx)
	if err != nil {
		return nil, err
	}
	defer c.Close()

	cli := c.dockerCli.Client()
	filter := filters.NewArgs()
	filter.Add("label", "com.docker.compose.project")

	return cli.ContainerList(ctx, container.ListOptions{
		All:     true,
		Filters: filter,
	})
}
