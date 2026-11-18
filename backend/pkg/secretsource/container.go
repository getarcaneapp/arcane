package secretsource

import (
	"bytes"
	"cmp"
	"context"
	"encoding/json/jsontext"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"maps"
	"slices"
	"strings"
	"time"

	cerrdefs "github.com/containerd/errdefs"
	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/client"

	"kit.getarcane.app/capture"
	"kit.getarcane.app/docker"

	"getarcane.dev/arcane/v3/pkg/projects"
	"getarcane.dev/arcane/v3/pkg/selfcontainer"
	"getarcane.dev/arcane/v3/pkg/timeouts"
	"getarcane.dev/types/v3"
)

const (
	containerTimeout  = 2 * time.Minute
	secretSourceLabel = "com.getarcaneapp.secret-source"
)

func (e *Engine) fetchContainer(ctx context.Context, definition types.SecretSourceDefinition, data map[string]any) ([]types.EnvVariable, error) {
	image, err := render("image", definition.Image, data)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(image) == "" {
		return nil, errors.New("container image is empty")
	}
	format, err := render("format", definition.Format, data)
	if err != nil {
		return nil, err
	}
	format = cmp.Or(strings.ToLower(strings.TrimSpace(format)), formatJSON)
	if format != formatJSON && format != formatDotenv {
		return nil, fmt.Errorf("format must be %q or %q", formatJSON, formatDotenv)
	}
	env, err := renderMap("env", definition.Env, data)
	if err != nil {
		return nil, err
	}
	args := make([]string, 0, len(definition.Args))
	for i, text := range definition.Args {
		arg, renderErr := render(fmt.Sprintf("args[%d]", i), text, data)
		if renderErr != nil {
			return nil, renderErr
		}
		args = append(args, arg)
	}
	containerEnv := make([]string, 0, len(env))
	for key, value := range env {
		containerEnv = append(containerEnv, key+"="+value)
	}
	slices.Sort(containerEnv)

	dockerClient, err := e.dockerClient(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to Docker: %w", err)
	}
	stdout, err := runContainer(ctx, dockerClient, container.Config{
		Image:  strings.TrimSpace(image),
		Cmd:    args,
		Env:    containerEnv,
		Labels: map[string]string{selfcontainer.InternalResourceLabel: "true", secretSourceLabel: "true"},
	})
	if err != nil {
		return nil, err
	}

	if format == formatDotenv {
		values, parseErr := projects.ParseProjectEnvContent(string(stdout), nil)
		if parseErr != nil {
			return nil, fmt.Errorf("container output is not valid dotenv: %w", parseErr)
		}
		entries := make([]types.EnvVariable, 0, len(values))
		for _, key := range slices.Sorted(maps.Keys(values)) {
			entries = append(entries, types.EnvVariable{
				Key:   key,
				Value: values[key],
			})
		}
		return entries, nil
	}
	document := jsontext.Value(bytes.TrimSpace(stdout))
	if !document.IsValid() {
		return nil, errors.New("container output is not valid JSON")
	}
	return extract(document, definition.Extract)
}

func runContainer(ctx context.Context, dockerClient *client.Client, config container.Config) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, containerTimeout)
	defer cancel()

	if err := ensureImage(ctx, dockerClient, config.Image); err != nil {
		return nil, err
	}

	created, err := dockerClient.ContainerCreate(ctx, client.ContainerCreateOptions{
		Config:     &config,
		HostConfig: &container.HostConfig{},
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create secret source container: %w", err)
	}
	defer func() {
		removeCtx, removeCancel := context.WithTimeout(context.WithoutCancel(ctx), timeouts.DefaultDockerAPI)
		defer removeCancel()
		if _, removeErr := dockerClient.ContainerRemove(removeCtx, created.ID, client.ContainerRemoveOptions{Force: true}); removeErr != nil && !cerrdefs.IsNotFound(removeErr) {
			slog.WarnContext(removeCtx, "failed to remove secret source container", "containerId", created.ID, "error", removeErr)
		}
	}()

	if _, startErr := dockerClient.ContainerStart(ctx, created.ID, client.ContainerStartOptions{}); startErr != nil {
		return nil, fmt.Errorf("failed to start secret source container: %w", startErr)
	}
	wait := dockerClient.ContainerWait(ctx, created.ID, client.ContainerWaitOptions{Condition: container.WaitConditionNotRunning})
	var status container.WaitResponse
	select {
	case waitErr := <-wait.Error:
		if waitErr != nil {
			return nil, fmt.Errorf("failed to wait for secret source container: %w", waitErr)
		}
	case status = <-wait.Result:
	}

	if status.StatusCode != 0 {
		return nil, fmt.Errorf("secret source container exited with code %d", status.StatusCode)
	}

	logs, err := dockerClient.ContainerLogs(ctx, created.ID, client.ContainerLogsOptions{ShowStdout: true})
	if err != nil {
		return nil, fmt.Errorf("failed to read secret source container output: %w", err)
	}
	defer func() { _ = logs.Close() }()
	stdout := capture.New(maxResponseBytes)
	if copyErr := docker.WaitStdCopy(docker.StartStdCopy(logs, stdout, io.Discard)); copyErr != nil {
		return nil, fmt.Errorf("failed to decode secret source container output: %w", copyErr)
	}
	if stdout.Truncated() {
		return nil, fmt.Errorf("secret source container output exceeds %d bytes", maxResponseBytes)
	}
	return []byte(stdout.String()), nil
}

func ensureImage(ctx context.Context, dockerClient *client.Client, image string) error {
	if _, err := dockerClient.ImageInspect(ctx, image); err == nil {
		return nil
	}
	pullCtx, cancel := context.WithTimeout(ctx, timeouts.DefaultDockerImagePull)
	defer cancel()
	reader, err := dockerClient.ImagePull(pullCtx, image, client.ImagePullOptions{})
	if err != nil {
		return fmt.Errorf("failed to pull secret source image %s: %w", image, err)
	}
	defer func() { _ = reader.Close() }()
	if renderErr := docker.RenderJSONMessageStream(reader, io.Discard); renderErr != nil {
		return fmt.Errorf("failed to pull secret source image %s: %w", image, renderErr)
	}
	return nil
}
