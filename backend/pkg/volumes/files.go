package volumes

import (
	"context"
	"fmt"
	"io"

	"github.com/moby/moby/client"

	"kit.getarcane.app/docker"
)

type (
	cleanupReadCloser struct {
		io.ReadCloser

		cleanup func()
	}
)

func (c *cleanupReadCloser) Close() error {
	err := c.ReadCloser.Close()
	c.cleanup()
	return err
}

func DownloadFileFromContainer(
	ctx context.Context,
	dockerClient *client.Client,
	containerID string,
	containerPath string,
	cleanup func(),
) (io.ReadCloser, int64, error) {
	reader, size, err := docker.CopyFromContainer(ctx, dockerClient, containerID, containerPath)
	if err != nil {
		cleanup()
		return nil, 0, fmt.Errorf("failed to download: %w", err)
	}

	return &cleanupReadCloser{
		ReadCloser: reader,
		cleanup:    cleanup,
	}, size, nil
}
