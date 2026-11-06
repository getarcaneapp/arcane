package projects

import (
	"cmp"
	"context"
	"fmt"
	"io"
	"strings"
	"time"

	composetypes "github.com/compose-spec/compose-go/v2/types"
	"github.com/docker/compose/v5/pkg/api"
	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/client"

	"kit.getarcane.app"
	"kit.getarcane.app/docker"

	"getarcane.dev/arcane/v3/pkg/activity"
	"getarcane.dev/arcane/v3/pkg/timeouts"
	"getarcane.dev/types/v3"
)

const (
	defaultComposeTimeout = 30 * time.Minute
)

func detachFromHTTPContext(parent context.Context, timeout time.Duration) (context.Context, context.CancelFunc) {
	if activity.IsAppLifecycleContext(parent) {
		return context.WithTimeout(parent, timeout)
	}
	ctx := context.WithoutCancel(parent)
	return context.WithTimeout(ctx, timeout)
}

func openCompose(ctx context.Context, config ClientConfig) (context.Context, *Client, func(), error) {
	composeCtx, cancel := detachFromHTTPContext(ctx, cmp.Or(config.Timeout, defaultComposeTimeout))
	c, err := NewClient(composeCtx, config)
	if err != nil {
		cancel()
		return nil, nil, nil, err
	}
	return composeCtx, c, func() {
		_ = c.Close()
		cancel()
	}, nil
}

func ComposeRestart(ctx context.Context, target types.ComposeTarget) error {
	composeCtx, c, done, err := openCompose(ctx, ClientConfig{DockerHost: target.DockerHost})
	if err != nil {
		return err
	}
	defer done()

	return c.svc.Restart(composeCtx, target.Project.Name, api.RestartOptions{
		Project:  target.Project,
		Services: target.Services,
	})
}

func ComposeStop(ctx context.Context, target types.ComposeTarget) error {
	if len(target.Services) == 0 {
		return nil
	}
	composeCtx, c, done, err := openCompose(ctx, ClientConfig{DockerHost: target.DockerHost})
	if err != nil {
		return err
	}
	defer done()

	return c.svc.Stop(composeCtx, target.Project.Name, api.StopOptions{Services: target.Services})
}

func ComposeUp(ctx context.Context, opts types.ComposeUpOptions) error {
	selected, err := SelectServices(opts.Project, opts.Services)
	if err != nil {
		return err
	}
	waitTimeout := kit.Ternary(opts.WaitTimeout > 0, opts.WaitTimeout, timeouts.DefaultDeployWait)

	// Selection errors were already surfaced at load, so parse best-effort here.
	envOpts, _ := ParseComposeEnvOptions(selected.WorkingDir, EnvMap(selected.Environment))

	composeCtx, c, done, err := openCompose(ctx, ClientConfig{
		AuthConfigs:    opts.AuthConfigs,
		DockerHost:     opts.DockerHost,
		MaxConcurrency: envOpts.ParallelLimit,
		// Wait plus headroom for pulls and creation, so a long Deploy Wait Timeout is not cut short.
		Timeout:       waitTimeout + defaultComposeTimeout,
		AcceptPrompts: opts.RecreateVolumes,
	})
	if err != nil {
		return err
	}
	defer done()

	return c.svc.Up(composeCtx, selected, composeUpOptions(opts, waitTimeout, envOpts))
}

func ComposeCreate(ctx context.Context, opts types.ComposeCreateOptions) error {
	selected, err := SelectServices(opts.Project, opts.Services)
	if err != nil {
		return err
	}
	composeCtx, c, done, err := openCompose(ctx, ClientConfig{
		AuthConfigs: opts.AuthConfigs,
		DockerHost:  opts.DockerHost,
	})
	if err != nil {
		return err
	}
	defer done()

	return c.svc.Create(composeCtx, selected, api.CreateOptions{
		Services:             opts.Services,
		Recreate:             api.RecreateForce,
		RecreateDependencies: api.RecreateDiverged,
	})
}

func composeUpOptions(opts types.ComposeUpOptions, waitTimeout time.Duration, envOpts ComposeEnvOptions) api.UpOptions {
	return api.UpOptions{
		Create: api.CreateOptions{
			Services:             opts.Services,
			Recreate:             kit.Ternary(opts.ForceRecreate, api.RecreateForce, api.RecreateDiverged),
			RecreateDependencies: api.RecreateDiverged,
			// A caller opt-in (or GitOps-forced true) still wins over COMPOSE_REMOVE_ORPHANS.
			RemoveOrphans: opts.RemoveOrphans || envOpts.RemoveOrphans,
			IgnoreOrphans: envOpts.IgnoreOrphans,
		},
		Start: api.StartOptions{
			Services:    opts.Services,
			Wait:        true,
			WaitTimeout: waitTimeout,
		},
	}
}

func ComposePs(ctx context.Context, opts types.ComposePsOptions) ([]api.ContainerSummary, error) {
	c, err := plainComposeClient(opts.DockerHost)
	if err != nil {
		return nil, err
	}

	return c.svc.Ps(ctx, opts.Project.Name, api.PsOptions{
		All:      opts.All,
		Services: opts.Services,
	})
}

func ComposeGenerate(ctx context.Context, dockerHost, projectName string, containerIDs []string) (string, error) {
	c, err := plainComposeClient(dockerHost)
	if err != nil {
		return "", err
	}

	model, err := c.svc.Generate(ctx, api.GenerateOptions{ProjectName: projectName, Containers: containerIDs})
	if err != nil {
		return "", err
	}
	content, err := model.MarshalYAML()
	return string(content), err
}

func ComposeDown(ctx context.Context, opts types.ComposeDownOptions) error {
	composeCtx, c, done, err := openCompose(ctx, ClientConfig{DockerHost: opts.DockerHost})
	if err != nil {
		return err
	}
	defer done()

	return c.svc.Down(composeCtx, opts.Project.Name, api.DownOptions{
		RemoveOrphans: true,
		Volumes:       opts.RemoveVolumes,
	})
}

func ComposeLogs(ctx context.Context, dockerHost, projectName string, out io.Writer, opts types.LogStreamOptions) error {
	c, err := NewClient(ctx, ClientConfig{DockerHost: dockerHost, DemuxLogs: true})
	if err != nil {
		return err
	}
	defer func() { _ = c.Close() }()

	return c.svc.Logs(ctx, projectName, &writerConsumer{out: out}, api.LogOptions{
		Follow:     opts.Follow,
		Tail:       opts.Tail,
		Since:      opts.Since,
		Timestamps: opts.Timestamps,
	})
}

func ListGlobalComposeContainers(ctx context.Context, dockerClient client.APIClient, dockerHost string) ([]container.Summary, error) {
	cli := dockerClient
	if cli == nil {
		c, err := plainComposeClient(dockerHost)
		if err != nil {
			return nil, err
		}
		cli = c.dockerCli.Client()
	}

	filter := make(client.Filters)
	filter = filter.Add("label", "com.docker.compose.project")

	listResult, err := cli.ContainerList(ctx, client.ContainerListOptions{
		All:     true,
		Filters: filter,
	})
	if err != nil {
		return nil, err
	}

	return listResult.Items, nil
}

func FindComposeServiceContainerID(ctx context.Context, dockerHost, projectName, serviceName string) (string, error) {
	containers, err := ComposePs(ctx, types.ComposePsOptions{
		Project:    &composetypes.Project{Name: projectName},
		DockerHost: dockerHost,
		Services:   []string{serviceName},
		All:        true,
	})
	if err != nil {
		return "", err
	}
	var firstMatch string
	for _, c := range containers {
		if c.Service != serviceName {
			continue
		}
		firstMatch = cmp.Or(firstMatch, c.ID)
		if c.State == "running" {
			return c.ID, nil
		}
	}
	return firstMatch, nil
}

func FindComposeReplica(containers []container.Summary, labels map[string]string) (container.Summary, bool) {
	projectName := docker.ComposeProjectLabel(labels)
	serviceName := docker.ComposeServiceLabel(labels)
	if projectName == "" || serviceName == "" {
		return container.Summary{}, false
	}
	number := strings.TrimSpace(labels[api.ContainerNumberLabel])
	for _, candidate := range containers {
		if docker.ComposeProjectLabel(candidate.Labels) != projectName || docker.ComposeServiceLabel(candidate.Labels) != serviceName {
			continue
		}
		if number != "" && strings.TrimSpace(candidate.Labels[api.ContainerNumberLabel]) != number {
			continue
		}
		return candidate, true
	}
	return container.Summary{}, false
}

func FormatPorts(publishers []api.PortPublisher) []string {
	var ports []string
	for _, pub := range publishers {
		if pub.PublishedPort > 0 {
			ports = append(ports, fmt.Sprintf("%d:%d/%s", pub.PublishedPort, pub.TargetPort, pub.Protocol))
		} else {
			ports = append(ports, fmt.Sprintf("%d/%s", pub.TargetPort, pub.Protocol))
		}
	}
	return ports
}

func FormatDockerPorts(ports []container.PortSummary) []string {
	var res []string
	for _, p := range ports {
		if p.PublicPort == 0 {
			res = append(res, fmt.Sprintf("%d/%s", p.PrivatePort, p.Type))
		} else {
			res = append(res, fmt.Sprintf("%d:%d/%s", p.PublicPort, p.PrivatePort, p.Type))
		}
	}
	return res
}
