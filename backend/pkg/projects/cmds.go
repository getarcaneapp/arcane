package projects

import (
	"context"
	"io"
	"time"

	"github.com/compose-spec/compose-go/v2/types"
	"github.com/docker/compose/v5/pkg/api"
	"github.com/docker/compose/v5/pkg/compose"
	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/api/types/registry"
	"github.com/moby/moby/client"

	"github.com/getarcaneapp/arcane/backend/v2/pkg/libarcane/timeouts"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/utils"
)

// defaultComposeTimeout is applied to compose operations that have been
// detached from the HTTP request context. It must be generous enough to
// cover large image pulls + health-check waits.
const defaultComposeTimeout = 30 * time.Minute

// detachFromHTTPContextInternal creates a new context derived from
// context.WithoutCancel(parent) that carries any values from the parent
// (such as dockerutils.ProgressWriterKey) but is **not** cancelled or
// deadline-bounded by the parent. This allows compose operations to survive
// HTTP request timeouts and proxy deadline cancellations. A standalone timeout
// is applied so the operation cannot run forever. See #1209.
func detachFromHTTPContextInternal(parent context.Context, timeout time.Duration) (context.Context, context.CancelFunc) {
	if utils.IsAppLifecycleContext(parent) {
		return context.WithTimeout(parent, timeout)
	}
	ctx := context.WithoutCancel(parent)
	return context.WithTimeout(ctx, timeout)
}

func ComposeRestart(ctx context.Context, proj *types.Project, services []string) error {
	restartCtx, cancel := detachFromHTTPContextInternal(ctx, defaultComposeTimeout)
	defer cancel()

	c, err := NewClient(restartCtx, nil, nil)
	if err != nil {
		return err
	}
	defer func() { _ = c.Close() }()

	return c.svc.Restart(restartCtx, proj.Name, api.RestartOptions{Project: proj, Services: services})
}

func ComposeStop(ctx context.Context, proj *types.Project, services []string) error {
	if len(services) == 0 {
		return nil
	}
	stopCtx, cancel := detachFromHTTPContextInternal(ctx, defaultComposeTimeout)
	defer cancel()

	c, err := NewClient(stopCtx, nil, nil)
	if err != nil {
		return err
	}
	defer func() { _ = c.Close() }()

	return c.svc.Stop(stopCtx, proj.Name, api.StopOptions{Services: services})
}

func ComposeUp(ctx context.Context, proj *types.Project, services []string, removeOrphans bool, forceRecreate bool, recreateVolumes bool, authConfigs map[string]registry.AuthConfig, waitTimeout time.Duration) error {
	if waitTimeout <= 0 {
		waitTimeout = timeouts.DefaultDeployWait
	}

	// Detach from the HTTP request context so that proxy timeouts and client
	// disconnects do not cancel a long-running compose up. See #1209. The
	// operation deadline must exceed the configured wait, otherwise a Deploy
	// Wait Timeout above defaultComposeTimeout would be cut short; the extra
	// defaultComposeTimeout leaves room for image pulls and container creation
	// that happen before the wait starts.
	composeCtx, cancel := detachFromHTTPContextInternal(ctx, waitTimeout+defaultComposeTimeout)
	defer cancel()

	// Compose prompts before recreating a volume whose config diverged (data
	// loss). Only answer yes when the caller explicitly opted in; otherwise the
	// default prompt declines and logs the question.
	var prompt compose.Prompt
	if recreateVolumes {
		prompt = compose.AlwaysOkPrompt()
	}

	c, err := NewClient(composeCtx, authConfigs, prompt)
	if err != nil {
		return err
	}
	defer func() { _ = c.Close() }()

	upOptions, startOptions := composeUpOptions(proj, services, removeOrphans, forceRecreate, waitTimeout)

	return c.svc.Up(composeCtx, proj, api.UpOptions{Create: upOptions, Start: startOptions})
}

func composeUpOptions(proj *types.Project, services []string, removeOrphans bool, forceRecreate bool, waitTimeout time.Duration) (api.CreateOptions, api.StartOptions) {
	recreatePolicy := api.RecreateDiverged
	if forceRecreate {
		recreatePolicy = api.RecreateForce
	}

	upOptions := api.CreateOptions{
		Services:             services,
		Recreate:             recreatePolicy,
		RecreateDependencies: api.RecreateDiverged,
		RemoveOrphans:        removeOrphans,
	}

	startOptions := api.StartOptions{
		Project:     proj,
		Services:    services,
		Wait:        true,
		WaitTimeout: waitTimeout,
		// CascadeFail ensures that if a dependency fails its health check,
		// the error propagates correctly instead of being ignored
		OnExit: api.CascadeFail,
	}

	return upOptions, startOptions
}

func ComposePs(ctx context.Context, proj *types.Project, services []string, all bool) ([]api.ContainerSummary, error) {
	c, err := NewClient(ctx, nil, nil)
	if err != nil {
		return nil, err
	}
	defer func() { _ = c.Close() }()

	return c.svc.Ps(ctx, proj.Name, api.PsOptions{All: all, Services: services})
}

func ComposeDown(ctx context.Context, proj *types.Project, removeVolumes bool) error {
	downCtx, cancel := detachFromHTTPContextInternal(ctx, defaultComposeTimeout)
	defer cancel()

	c, err := NewClient(downCtx, nil, nil)
	if err != nil {
		return err
	}
	defer func() { _ = c.Close() }()

	return c.svc.Down(downCtx, proj.Name, api.DownOptions{RemoveOrphans: true, Volumes: removeVolumes})
}

func ComposeLogs(ctx context.Context, projectName string, out io.Writer, follow bool, tail, since string, timestamps bool) error {
	c, err := NewClient(ctx, nil, nil)
	if err != nil {
		return err
	}
	defer func() { _ = c.Close() }()

	return c.svc.Logs(ctx, projectName, writerConsumer{out: out}, api.LogOptions{Follow: follow, Tail: tail, Since: since, Timestamps: timestamps})
}

func ListGlobalComposeContainers(ctx context.Context) ([]container.Summary, error) {
	c, err := NewClient(ctx, nil, nil)
	if err != nil {
		return nil, err
	}
	defer func() { _ = c.Close() }()

	cli := c.dockerCli.Client()
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
