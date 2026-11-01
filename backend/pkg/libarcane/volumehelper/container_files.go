package volumehelper

import (
	"archive/tar"
	"context"
	"errors"
	"fmt"
	"io"

	"github.com/moby/moby/client"
)

type (
	cleanupReadCloser struct {
		io.Reader
		io.Closer

		cleanup func()
	}
)

func (c *cleanupReadCloser) Close() error {
	err := c.Closer.Close()
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
	copyResult, err := dockerClient.CopyFromContainer(ctx, containerID, client.CopyFromContainerOptions{
		SourcePath: containerPath,
	})
	if err != nil {
		cleanup()
		return nil, 0, fmt.Errorf("failed to download: %w", err)
	}
	reader := copyResult.Content

	tr := tar.NewReader(reader)
	hdr, err := tr.Next()
	if err != nil {
		_ = reader.Close()
		cleanup()
		return nil, 0, fmt.Errorf("failed to read tar stream: %w", err)
	}
	if validateDownloadHeaderErr := validateDownloadHeader(hdr); validateDownloadHeaderErr != nil {
		_ = reader.Close()
		cleanup()
		return nil, 0, validateDownloadHeaderErr
	}

	return &cleanupReadCloser{
		Reader:  tr,
		Closer:  reader,
		cleanup: cleanup,
	}, hdr.Size, nil
}

func validateDownloadHeader(header *tar.Header) error {
	if header.FileInfo().IsDir() {
		return errors.New("path is a directory")
	}
	return nil
}
