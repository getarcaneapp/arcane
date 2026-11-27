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
	mobyContainer "github.com/moby/moby/api/types/container"
	mobyMount "github.com/moby/moby/api/types/mount"
	mobyClient "github.com/moby/moby/client"

	"kit.getarcane.app/docker"

	"getarcane.dev/arcane/v3/pkg/timeouts"
	"getarcane.dev/arcane/v3/pkg/volumes"
)

func Run(ctx context.Context, dockerClient *mobyClient.Client, password string, command, environment []string, mounts []mobyMount.Mount, networkMode mobyContainer.NetworkMode) (string, error) {
	if _, err := dockerClient.VolumeCreate(ctx, mobyClient.VolumeCreateOptions{Name: CacheVolume, Labels: volumes.Labels()}); err != nil {
		return "", fmt.Errorf("failed to provision Rustic cache volume: %w", err)
	}
	// Secrets go in files copied into the container: inspect exposes Config.Env to any containers:read user.
	env := []string{
		"RUSTIC_PASSWORD_FILE=/" + secretsDir + "/password",
		"RUSTIC_NO_PROGRESS=true",
		"RUSTIC_LOG_LEVEL=error",
		"RUSTIC_CACHE_DIR=/cache",
	}
	secrets := map[string][]byte{secretsDir + "/password": []byte(password)}
	var credentials strings.Builder
	for _, entry := range environment {
		switch name, value, _ := strings.Cut(entry, "="); name {
		case "AWS_ACCESS_KEY_ID", "AWS_SECRET_ACCESS_KEY":
			fmt.Fprintf(&credentials, "%s = %s\n", strings.ToLower(name), value)
		default:
			env = append(env, entry)
		}
	}
	if credentials.Len() > 0 {
		env = append(env, "AWS_SHARED_CREDENTIALS_FILE=/"+secretsDir+"/credentials")
		secrets[secretsDir+"/credentials"] = []byte("[default]\n" + credentials.String())
	}
	allMounts := append([]mobyMount.Mount{}, mounts...)
	allMounts = append(allMounts, mobyMount.Mount{Type: mobyMount.TypeVolume, Source: CacheVolume, Target: "/cache"})
	created, err := dockerClient.ContainerCreate(ctx, mobyClient.ContainerCreateOptions{
		Config: &mobyContainer.Config{
			Image:  DefaultImage,
			Cmd:    command,
			Env:    env,
			Labels: volumes.Labels(),
		},
		HostConfig: volumes.HostConfig(volumes.HelperHost{
			Image:       DefaultImage,
			Mounts:      allMounts,
			NetworkMode: networkMode,
			Keep:        true,
		}),
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
			_, containerRemoveErr := dockerClient.ContainerRemove(attemptCtx, created.ID, volumes.RemoveOptions())
			cancel()
			if containerRemoveErr == nil || cerrdefs.IsNotFound(containerRemoveErr) {
				return
			}
			slog.WarnContext(cleanupCtx, "failed to remove Rustic container, retrying", "containerId", created.ID, "error", containerRemoveErr)
			time.Sleep(5 * time.Second)
		}
	}()
	if copyErr := docker.CopyToContainer(ctx, dockerClient, created.ID, "/", secrets); copyErr != nil {
		return "", fmt.Errorf("failed to provide Rustic secrets: %w", copyErr)
	}
	if _, containerStartErr := dockerClient.ContainerStart(ctx, created.ID, mobyClient.ContainerStartOptions{}); containerStartErr != nil {
		return "", fmt.Errorf("failed to start Rustic container: %w", containerStartErr)
	}
	wait := dockerClient.ContainerWait(ctx, created.ID, mobyClient.ContainerWaitOptions{Condition: mobyContainer.WaitConditionNotRunning})
	var status mobyContainer.WaitResponse
	select {
	case operationErr := <-wait.Error:
		if operationErr != nil {
			return "", fmt.Errorf("failed to wait for Rustic container: %w", operationErr)
		}
	case status = <-wait.Result:
	}
	logs, err := dockerClient.ContainerLogs(ctx, created.ID, mobyClient.ContainerLogsOptions{ShowStdout: true, ShowStderr: true})
	if err != nil {
		return "", fmt.Errorf("failed to read Rustic output: %w", err)
	}
	defer func() { _ = logs.Close() }()
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	if stdCopyErr := docker.WaitStdCopy(docker.StartStdCopy(logs, &stdout, &stderr)); stdCopyErr != nil {
		return "", fmt.Errorf("failed to decode Rustic output: %w", stdCopyErr)
	}
	if status.StatusCode != 0 {
		message := cmp.Or(strings.TrimSpace(stderr.String()), strings.TrimSpace(stdout.String()))
		return "", fmt.Errorf("rustic exited with code %d: %s", status.StatusCode, message)
	}
	return stdout.String(), nil
}
