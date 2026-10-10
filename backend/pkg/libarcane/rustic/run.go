package rustic

import (
	"bytes"
	"cmp"
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	cerrdefs "github.com/containerd/errdefs"
	"github.com/moby/moby/api/pkg/stdcopy"
	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/api/types/mount"
	"github.com/moby/moby/client"

	"github.com/getarcaneapp/arcane/backend/v2/pkg/libarcane/timeouts"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/libarcane/volumehelper"
)

// Run executes one Rustic command in an ephemeral container built from
// DefaultImage and returns its trimmed stdout. It is the single Rustic
// invocation contract, shared by the backup engine and the detached recovery
// helper; the image must already be present. The standard RUSTIC_* variables
// are appended to environment, and networkMode is applied verbatim ("" keeps
// the default network). Every run mounts the shared CacheVolume.
func Run(ctx context.Context, dockerClient *client.Client, password string, command, environment []string, mounts []mount.Mount, networkMode container.NetworkMode) (string, error) {
	if _, err := dockerClient.VolumeCreate(ctx, client.VolumeCreateOptions{Name: CacheVolume, Labels: volumehelper.Labels()}); err != nil {
		return "", fmt.Errorf("failed to provision Rustic cache volume: %w", err)
	}
	env := append([]string{}, environment...)
	env = append(env,
		"RUSTIC_PASSWORD="+password,
		"RUSTIC_NO_PROGRESS=true",
		"RUSTIC_LOG_LEVEL=error",
		"RUSTIC_CACHE_DIR=/cache",
	)
	allMounts := append([]mount.Mount{}, mounts...)
	allMounts = append(allMounts, mount.Mount{Type: mount.TypeVolume, Source: CacheVolume, Target: "/cache"})
	hostConfig := volumehelper.HostConfig(DefaultImage, nil, allMounts)
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
		// Keep caller locks and stopped consumers protected until removal is confirmed.
		// Request cancellation cannot prove that Rustic has stopped writing.
		cleanupCtx := context.WithoutCancel(ctx)
		for {
			attemptCtx, cancel := context.WithTimeout(cleanupCtx, timeouts.DefaultDockerAPI)
			_, containerRemoveErr := dockerClient.ContainerRemove(attemptCtx, created.ID, volumehelper.RemoveOptions())
			cancel()
			if containerRemoveErr == nil || cerrdefs.IsNotFound(containerRemoveErr) {
				return
			}
			slog.WarnContext(cleanupCtx, "failed to remove Rustic container, retrying", "containerId", created.ID, "error", containerRemoveErr)
			time.Sleep(5 * time.Second)
		}
	}()
	if _, containerStartErr := dockerClient.ContainerStart(ctx, created.ID, client.ContainerStartOptions{}); containerStartErr != nil {
		return "", fmt.Errorf("failed to start Rustic container: %w", containerStartErr)
	}
	wait := dockerClient.ContainerWait(ctx, created.ID, client.ContainerWaitOptions{Condition: container.WaitConditionNotRunning})
	var status container.WaitResponse
	select {
	case operationErr := <-wait.Error:
		if operationErr != nil {
			return "", fmt.Errorf("failed to wait for Rustic container: %w", operationErr)
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
	if _, stdCopyErr := stdcopy.StdCopy(&stdout, &stderr, logs); stdCopyErr != nil {
		return "", fmt.Errorf("failed to decode Rustic output: %w", stdCopyErr)
	}
	if status.StatusCode != 0 {
		message := cmp.Or(strings.TrimSpace(stderr.String()), strings.TrimSpace(stdout.String()))
		return "", fmt.Errorf("rustic exited with code %d: %s", status.StatusCode, message)
	}
	return strings.TrimSpace(stdout.String()), nil
}
