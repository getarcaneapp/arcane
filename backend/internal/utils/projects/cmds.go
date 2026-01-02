package projects

import (
	"context"
	"encoding/json"
	"io"
	"strings"
	"time"

	"github.com/compose-spec/compose-go/v2/types"
	"github.com/docker/compose/v5/pkg/api"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/filters"
)

// ProgressWriterKey can be set on a context to enable JSON-line progress updates.
// The value must be an io.Writer (typically the HTTP response writer).
type ProgressWriterKey struct{}

type flusher interface{ Flush() }

func writeJSONLine(w io.Writer, v any) {
	if w == nil {
		return
	}
	b, err := json.Marshal(v)
	if err != nil {
		return
	}
	_, _ = w.Write(append(b, '\n'))
	if f, ok := w.(flusher); ok {
		f.Flush()
	}
}

func ComposeRestart(ctx context.Context, proj *types.Project, services []string) error {
	c, err := NewClient(ctx)
	if err != nil {
		return err
	}
	defer c.Close()
	return c.svc.Restart(ctx, proj.Name, api.RestartOptions{Services: services})
}

func ComposeUp(ctx context.Context, proj *types.Project, services []string) error {
	c, err := NewClient(ctx)
	if err != nil {
		return err
	}
	defer c.Close()

	progressWriter, _ := ctx.Value(ProgressWriterKey{}).(io.Writer)

	upOptions, startOptions := composeUpOptions(proj, services)

	// If we don't need progress, just run compose up normally.
	if progressWriter == nil {
		return c.svc.Up(ctx, proj, api.UpOptions{Create: upOptions, Start: startOptions})
	}

	return composeUpWithProgress(ctx, c.svc, proj, api.UpOptions{Create: upOptions, Start: startOptions}, progressWriter)
}

func composeUpOptions(proj *types.Project, services []string) (api.CreateOptions, api.StartOptions) {
	upOptions := api.CreateOptions{
		Services:             services,
		Recreate:             api.RecreateDiverged,
		RecreateDependencies: api.RecreateDiverged,
	}

	startOptions := api.StartOptions{
		Project:  proj,
		Services: services,
		Wait:     true,
		// Reduced from 10 minutes to 2 minutes - if a service can't become healthy
		// in 2 minutes, there's likely a configuration issue (missing healthcheck, etc.)
		WaitTimeout: 2 * time.Minute,
		// CascadeFail ensures that if a dependency fails its health check,
		// the error propagates correctly instead of being ignored
		OnExit: api.CascadeFail,
	}

	return upOptions, startOptions
}

func composeUpWithProgress(ctx context.Context, svc api.Compose, proj *types.Project, opts api.UpOptions, progressWriter io.Writer) error {
	writeJSONLine(progressWriter, map[string]any{"type": "deploy", "phase": "begin"})

	// Poll in a goroutine while compose up runs on the calling goroutine.
	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	pollDone := make(chan struct{})
	go func() {
		defer close(pollDone)
		pollDeployProgress(runCtx, svc, proj.Name, progressWriter)
	}()

	err := svc.Up(runCtx, proj, opts)
	cancel()
	<-pollDone
	return err
}

func pollDeployProgress(ctx context.Context, svc api.Compose, projectName string, progressWriter io.Writer) {
	ticker := time.NewTicker(800 * time.Millisecond)
	defer ticker.Stop()

	// Dedupe emitted events so we don't spam the UI.
	lastSig := map[string]string{}

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			containers, err := svc.Ps(ctx, projectName, api.PsOptions{All: true})
			if err != nil {
				// Compose may still be creating containers.
				continue
			}
			for _, cs := range containers {
				emitDeployContainerUpdate(progressWriter, lastSig, cs)
			}
		}
	}
}

func emitDeployContainerUpdate(w io.Writer, lastSig map[string]string, cs api.ContainerSummary) {
	name := strings.TrimSpace(cs.Service)
	if name == "" {
		name = strings.TrimSpace(cs.Name)
	}
	if name == "" {
		return
	}

	phase := deployPhaseFromSummary(cs)
	sig := strings.Join([]string{phase, cs.State, cs.Health, strings.TrimSpace(cs.Status)}, "|")
	if lastSig[name] == sig {
		return
	}
	lastSig[name] = sig

	payload := map[string]any{
		"type":    "deploy",
		"phase":   phase,
		"service": name,
		"state":   cs.State,
		"health":  cs.Health,
	}
	if strings.TrimSpace(cs.Status) != "" {
		payload["status"] = strings.TrimSpace(cs.Status)
	}
	writeJSONLine(w, payload)
}

func deployPhaseFromSummary(cs api.ContainerSummary) string {
	state := strings.ToLower(strings.TrimSpace(cs.State))
	health := strings.ToLower(strings.TrimSpace(cs.Health))

	switch {
	case state == "running" && health == "healthy":
		return "service_healthy"
	case health == "starting" || health == "unhealthy":
		return "service_waiting_healthy"
	case state != "running" && state != "":
		return "service_state"
	default:
		return "service_status"
	}
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
