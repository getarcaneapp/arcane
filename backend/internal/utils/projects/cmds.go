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

	upOptions := api.CreateOptions{
		Services:             services,
		Recreate:             api.RecreateDiverged,
		RecreateDependencies: api.RecreateDiverged,
	}
	startOptions := api.StartOptions{
		Project:     proj,
		Services:    services,
		Wait:        true,
		// Reduced from 10 minutes to 2 minutes - if a service can't become healthy
		// in 2 minutes, there's likely a configuration issue (missing healthcheck, etc.)
		WaitTimeout: 2 * time.Minute,
		// CascadeFail ensures that if a dependency fails its health check,
		// the error propagates correctly instead of being ignored
		OnExit: api.CascadeFail,
	}

	// If we don't need progress, just run compose up normally.
	if progressWriter == nil {
		return c.svc.Up(ctx, proj, api.UpOptions{Create: upOptions, Start: startOptions})
	}

	writeJSONLine(progressWriter, map[string]any{"type": "deploy", "phase": "begin"})

	// Run compose up in the background, and poll container health to emit live status.
	errCh := make(chan error, 1)
	go func() {
		errCh <- c.svc.Up(ctx, proj, api.UpOptions{Create: upOptions, Start: startOptions})
	}()

	ticker := time.NewTicker(800 * time.Millisecond)
	defer ticker.Stop()

	// Dedupe emitted events so we don't spam the UI.
	lastSig := map[string]string{}

	for {
		select {
		case err := <-errCh:
			return err
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			containers, psErr := c.svc.Ps(ctx, proj.Name, api.PsOptions{All: true})
			if psErr != nil {
				// Keep waiting; compose up may still be creating containers.
				continue
			}

			for _, cs := range containers {
				name := strings.TrimSpace(cs.Service)
				if name == "" {
					name = strings.TrimSpace(cs.Name)
				}
				if name == "" {
					continue
				}

				state := strings.ToLower(strings.TrimSpace(cs.State))
				health := strings.ToLower(strings.TrimSpace(cs.Health))

				phase := "service_status"
				switch {
				case state == "running" && health == "healthy":
					phase = "service_healthy"
				case health == "starting" || health == "unhealthy":
					phase = "service_waiting_healthy"
				case state != "running" && state != "":
					phase = "service_state"
				}

				sig := strings.Join([]string{phase, cs.State, cs.Health, strings.TrimSpace(cs.Status)}, "|")
				if lastSig[name] == sig {
					continue
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
				writeJSONLine(progressWriter, payload)
			}
		}
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
