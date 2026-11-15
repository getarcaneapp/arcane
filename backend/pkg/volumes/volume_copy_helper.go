package volumes

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
	"github.com/moby/moby/client"

	"getarcane.dev/arcane/v3/internal/common"
	"getarcane.dev/types/v3"
)

// projectVolumeCopyScript streams the source volume into the target one. pipefail makes the
// pipeline fail when the reading tar fails, so a partial archive cannot report success and let
// the caller replace the original volume with an incomplete copy.
const projectVolumeCopyScript = `set -o pipefail; tar -C /source -cf - . | tar -C ` + projectVolumeCopyMountPath + ` -xf -`

// copyProjectVolumeDataInHelper copies a volume inside one helper container that
// mounts both volumes. Windows uses it because docker cp cannot reach the volumes of a
// Windows container; the copy holders used on Linux are never started.
func copyProjectVolumeDataInHelper(ctx context.Context, dockerClient *client.Client, copyRuntime projectVolumeCopyRuntime, sourceVolume, targetVolume string) error {
	// This helper only runs on Windows, where the Linux bind targets move to the C: drive
	// (see WindowsContainerPath). Build the host config as a single literal; the short-lived
	// copy helper is removed explicitly after it finishes, not automatically on exit.
	binds, mounts := windowsMountsInternal([]string{
		sourceVolume + ":/source:ro",
		targetVolume + ":" + projectVolumeCopyMountPath,
	}, nil)
	hostConfig := &container.HostConfig{
		Binds:      binds,
		Mounts:     mounts,
		AutoRemove: false,
	}

	resp, err := dockerClient.ContainerCreate(ctx, client.ContainerCreateOptions{
		Config: &container.Config{
			Image:           copyRuntime.Image,
			Cmd:             []string{"sh", "-c", projectVolumeCopyScript},
			NetworkDisabled: true,
			Labels:          Labels(),
		},
		HostConfig: hostConfig,
	})
	if err != nil {
		return fmt.Errorf("create volume copy helper: %w", err)
	}
	defer func() {
		cleanupCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 30*time.Second)
		defer cancel()
		if _, removeErr := dockerClient.ContainerRemove(cleanupCtx, resp.ID, RemoveOptions()); removeErr != nil && !cerrdefs.IsNotFound(removeErr) {
			slog.WarnContext(cleanupCtx, "failed to remove volume copy helper", "containerId", resp.ID, "error", removeErr)
		}
	}()

	if _, startErr := dockerClient.ContainerStart(ctx, resp.ID, client.ContainerStartOptions{}); startErr != nil {
		return fmt.Errorf("start volume copy helper: %w", startErr)
	}
	wait := dockerClient.ContainerWait(ctx, resp.ID, client.ContainerWaitOptions{Condition: container.WaitConditionNotRunning})
	var status container.WaitResponse
	select {
	case waitErr := <-wait.Error:
		if waitErr != nil {
			return fmt.Errorf("wait for volume copy helper: %w", waitErr)
		}
	case status = <-wait.Result:
	}
	if status.StatusCode == 0 {
		return nil
	}

	message := fmt.Sprintf("exit code %d", status.StatusCode)
	if logs, logsErr := dockerClient.ContainerLogs(ctx, resp.ID, client.ContainerLogsOptions{ShowStdout: true, ShowStderr: true}); logsErr == nil {
		var stdout, stderr bytes.Buffer
		if _, copyErr := stdcopy.StdCopy(&stdout, &stderr, logs); copyErr == nil {
			message = cmp.Or(strings.TrimSpace(stderr.String()), strings.TrimSpace(stdout.String()), message)
		}
		_ = logs.Close()
	}
	if isProjectVolumeCopyNoSpaceError(fmt.Errorf("%s", message)) {
		return common.Classify(common.ErrInsufficientStorage, &types.ProjectVolumeRenameInsufficientSpaceError{
			SourceVolume: sourceVolume,
			TargetVolume: targetVolume,
			Detail:       message,
		})
	}
	return fmt.Errorf("copy volume data in helper: %s", message)
}
