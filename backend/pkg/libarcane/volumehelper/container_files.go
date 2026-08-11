package volumehelper

import (
	"archive/tar"
	"context"
	"io"

	"emperror.dev/errors"
	"github.com/moby/moby/client"
)

type cleanupReadCloserInternal struct {
	io.Reader
	io.Closer

	cleanup func()
}

func (c *cleanupReadCloserInternal) Close() error {
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
		return nil, 0, errors.WrapIf(err, "failed to download")
	}
	reader := copyResult.Content

	tr := tar.NewReader(reader)
	hdr, err := tr.Next()
	if err != nil {
		_ = reader.Close()
		cleanup()
		return nil, 0, errors.WrapIf(err, "failed to read tar stream")
	}
	if err := validateDownloadHeaderInternal(hdr); err != nil {
		_ = reader.Close()
		cleanup()
		return nil, 0, err
	}

	return &cleanupReadCloserInternal{
		Reader:  tr,
		Closer:  reader,
		cleanup: cleanup,
	}, hdr.Size, nil
}

func validateDownloadHeaderInternal(header *tar.Header) error {
	if header.FileInfo().IsDir() {
		return errors.New("path is a directory")
	}
	return nil
}
