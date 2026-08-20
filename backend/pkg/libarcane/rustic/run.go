package rustic

import (
	"bytes"
	"context"
	"fmt"
	"strings"

	"github.com/getarcaneapp/arcane/backend/v2/pkg/libarcane/volumehelper"
	"github.com/moby/moby/api/pkg/stdcopy"
	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/api/types/mount"
	"github.com/moby/moby/client"
)

// Run executes one Rustic command in an ephemeral container built from
// DefaultImage and returns its trimmed stdout. It is the single Rustic
// invocation contract, shared by the backup engine and the detached recovery
// helper; the image must already be present. The standard RUSTIC_* variables
// are appended to environment, and networkMode is applied verbatim ("" keeps
// the default network).
func Run(ctx context.Context, dockerClient *client.Client, password string, command, environment []string, mounts []mount.Mount, networkMode container.NetworkMode) (string, error) {
	env := append([]string{}, environment...)
	env = append(env,
		"RUSTIC_PASSWORD="+password,
		"RUSTIC_NO_PROGRESS=true",
		"RUSTIC_LOG_LEVEL=error",
	)
	hostConfig := volumehelper.HostConfig(DefaultImage, nil, mounts)
	hostConfig.AutoRemove = false
	hostConfig.NetworkMode = networkMode
	created, err := dockerClient.ContainerCreate(ctx, client.ContainerCreateOptions{
		Config: &container.Config{
			Image:  DefaultImage,
			Cmd:    command,
			Env:    env,
			Labels: volumehelper.Labels(),
		},
		HostConfig: hostConfig,
	})
	if err != nil {
		return "", fmt.Errorf("failed to create Rustic container: %w", err)
	}
	defer func() {
		_, _ = dockerClient.ContainerRemove(context.WithoutCancel(ctx), created.ID, volumehelper.RemoveOptions())
	}()
	if _, err := dockerClient.ContainerStart(ctx, created.ID, client.ContainerStartOptions{}); err != nil {
		return "", fmt.Errorf("failed to start Rustic container: %w", err)
	}
	wait := dockerClient.ContainerWait(ctx, created.ID, client.ContainerWaitOptions{Condition: container.WaitConditionNotRunning})
	var status container.WaitResponse
	select {
	case err := <-wait.Error:
		if err != nil {
			return "", fmt.Errorf("failed to wait for Rustic container: %w", err)
		}
	case status = <-wait.Result:
	}
	logs, err := dockerClient.ContainerLogs(ctx, created.ID, client.ContainerLogsOptions{ShowStdout: true, ShowStderr: true})
	if err != nil {
		return "", fmt.Errorf("failed to read Rustic output: %w", err)
	}
	defer func() { _ = logs.Close() }()
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	if _, err := stdcopy.StdCopy(&stdout, &stderr, logs); err != nil {
		return "", fmt.Errorf("failed to decode Rustic output: %w", err)
	}
	if status.StatusCode != 0 {
		message := strings.TrimSpace(stderr.String())
		if message == "" {
			message = strings.TrimSpace(stdout.String())
		}
		return "", fmt.Errorf("rustic exited with code %d: %s", status.StatusCode, message)
	}
	return strings.TrimSpace(stdout.String()), nil
}
