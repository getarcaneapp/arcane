package container

import (
	"context"
	"io"

	"emperror.dev/errors"
	"github.com/moby/moby/api/pkg/stdcopy"
	"github.com/moby/moby/client"

	dockerutils "github.com/getarcaneapp/arcane/backend/v2/pkg/dockerutil"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/libarcane"
)

type demuxedLogsInternal struct {
	io.Reader

	pipe *io.PipeReader
	logs io.ReadCloser
}

func (d *demuxedLogsInternal) Close() error {
	return errors.Combine(d.logs.Close(), d.pipe.Close())
}

func (s *ContainerService) openLogsInternal(ctx context.Context, containerID string, options client.ContainerLogsOptions) (io.ReadCloser, client.ContainerInspectResult, error) {
	dockerClient, err := s.dockerService.GetClient(ctx)
	if err != nil {
		return nil, client.ContainerInspectResult{}, errors.WrapIf(err, "failed to connect to Docker")
	}

	containerInspect, err := libarcane.ContainerInspectWithCompatibility(ctx, dockerClient, containerID, client.ContainerInspectOptions{})
	if err != nil {
		return nil, client.ContainerInspectResult{}, errors.WrapIf(err, "failed to inspect container for logs")
	}

	logs, err := dockerClient.ContainerLogs(ctx, containerID, options)
	if err != nil {
		return nil, client.ContainerInspectResult{}, errors.WrapIf(err, "failed to get container logs")
	}
	return logs, containerInspect, nil
}

func (s *ContainerService) StreamLogs(ctx context.Context, containerID string, logsChan chan<- string, follow bool, tail, since string, timestamps bool) error {
	logs, containerInspect, err := s.openLogsInternal(ctx, containerID, client.ContainerLogsOptions{
		ShowStdout: true,
		ShowStderr: true,
		Follow:     follow,
		Tail:       tail,
		Since:      since,
		Timestamps: timestamps,
	})
	if err != nil {
		return err
	}
	defer func() { _ = logs.Close() }()

	isTTY := containerInspect.Container.Config != nil && containerInspect.Container.Config.Tty
	return dockerutils.StreamContainerLogs(ctx, logs, logsChan, follow, isTTY)
}

// DownloadLogs returns every log line Docker retains for the container as a
// single plain-text stream plus the attachment filename. Callers must Close it.
func (s *ContainerService) DownloadLogs(ctx context.Context, containerID string) (io.ReadCloser, string, error) {
	logs, containerInspect, err := s.openLogsInternal(ctx, containerID, client.ContainerLogsOptions{
		ShowStdout: true,
		ShowStderr: true,
		Tail:       "all",
		Timestamps: true,
	})
	if err != nil {
		return nil, "", err
	}

	filename := "container-" + containerInspect.Container.ID[:min(12, len(containerInspect.Container.ID))] + "-logs.log"
	if containerInspect.Container.Config != nil && containerInspect.Container.Config.Tty {
		return logs, filename, nil
	}

	pr, pw := io.Pipe()
	go func() {
		_, copyErr := stdcopy.StdCopy(pw, pw, logs)
		if dockerutils.IsExpectedStreamEndError(copyErr) {
			copyErr = nil
		}
		_ = pw.CloseWithError(copyErr)
	}()
	return &demuxedLogsInternal{Reader: pr, pipe: pr, logs: logs}, filename, nil
}
