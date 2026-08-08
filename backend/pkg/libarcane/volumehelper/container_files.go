package volumehelper

import (
	"archive/tar"
	"context"
	"io"
	"strings"

	"emperror.dev/errors"
	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/client"

	"github.com/getarcaneapp/arcane/backend/v2/pkg/libarcane"
)

type cleanupReadCloserInternal struct {
	io.Reader
	io.Closer

	cleanup func()
}

func IsLegacyHelperContainer(c container.Summary) bool {
	if !libarcane.IsInternalContainer(c.Labels) {
		return false
	}

	command := strings.ToLower(c.Command)
	if !strings.Contains(command, "sleep") || !strings.Contains(command, "infinity") {
		return false
	}

	for _, m := range c.Mounts {
		if m.Destination == "/volume" {
			return true
		}
	}

	return false
}

func IsHelperContainer(c container.Summary) bool {
	if IsLegacyHelperContainer(c) {
		return true
	}
	if !libarcane.IsInternalContainer(c.Labels) {
		return false
	}

	return strings.EqualFold(c.Labels[ContainerLabel], "true")
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
	if hdr.FileInfo().IsDir() {
		_ = reader.Close()
		cleanup()
		return nil, 0, errors.New("path is a directory")
	}

	return &cleanupReadCloserInternal{
		Reader:  tr,
		Closer:  reader,
		cleanup: cleanup,
	}, hdr.Size, nil
}
