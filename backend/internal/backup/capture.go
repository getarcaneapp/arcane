package backup

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"maps"
	"path"
	"strconv"
	"strings"

	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/api/types/mount"
	"github.com/moby/moby/client"

	"kit.getarcane.app/docker"

	"getarcane.dev/arcane/v3/internal/common"
	"getarcane.dev/arcane/v3/pkg/timeouts"
	"getarcane.dev/arcane/v3/pkg/volumes"
	"getarcane.dev/types/v3"
)

type (
	// CapturePlan is one point-in-time read of a subject: its parts, mounted read-only under SnapshotRoot, and how to
	// quiesce it. The caller holds the locks that keep the parts from changing hands.
	CapturePlan struct {
		// Inspect reports a Docker volume, so the capture can tell the volumes it read are the ones it planned.
		Inspect func(ctx context.Context, name string) (types.Volume, bool, error)
		// Stop quiesces the subject and returns what starts it again; nil when nothing needs stopping.
		Stop func(ctx context.Context) (func(context.Context) error, error)
		// Rejected records a read the capture rejects after it finished, before the error is returned.
		Rejected func(ctx context.Context, err error) error
		Mounts   []mount.Mount
		// Volumes are the Docker volumes among the mounts.
		Volumes []string
	}

	// ArchiveOutput tars mounted parts in one helper, each as a volume export does.
	ArchiveOutput struct {
		// Add receives each part's archive, in the order of the parts.
		Add   func(ctx context.Context, part mount.Mount, size int64, body io.Reader) error
		Image string
		Parts []mount.Mount
	}
)

const (
	// SnapshotRoot is where a capture mounts a subject's parts: project/, volumes/<name>/ and meta/.
	SnapshotRoot = "/snapshot"
	archiveDir   = "/archives"
)

var errVolumeReplaced = common.Classify(common.ErrConflict, errors.New("a volume was removed or replaced while it was copied, so the backup failed; start it again"))

// VolumePart mounts a Docker volume read-only where a capture lays out the subject's volumes.
func VolumePart(name string) mount.Mount {
	return mount.Mount{Type: mount.TypeVolume, Source: name, Target: path.Join(SnapshotRoot, "volumes", name), ReadOnly: true}
}

// Capture quiesces the subject once, hands its mounted parts to read, and checks that every volume read is the one
// planned, before the subject starts again.
func Capture(ctx context.Context, plan CapturePlan, read func(ctx context.Context, mounts []mount.Mount) error) (err error) {
	// A volume removed while the capture waited would be mounted, and so created, empty; one replaced during the read
	// would be read empty too. Each must exist now and be the same volume after the read.
	before, err := volumeIdentities(ctx, plan.Inspect, plan.Volumes)
	if err != nil {
		return err
	}
	if plan.Stop != nil {
		start, stopErr := plan.Stop(ctx)
		if start != nil {
			// Started again however the capture ends.
			defer func() {
				if startErr := start(context.WithoutCancel(ctx)); startErr != nil {
					err = errors.Join(err, fmt.Errorf("restart project containers: %w", startErr))
				}
			}()
		}
		if stopErr != nil {
			return stopErr
		}
	}
	if readErr := read(ctx, plan.Mounts); readErr != nil {
		return readErr
	}
	// A shutdown can't cut the check short: a cancelled check would reject a read that finished. It is still bounded,
	// since the subject stays stopped until it returns.
	checkCtx, cancelCheck := context.WithTimeout(context.WithoutCancel(ctx), timeouts.DefaultDockerAPI)
	after, err := volumeIdentities(checkCtx, plan.Inspect, plan.Volumes)
	cancelCheck()
	if err == nil && !maps.Equal(before, after) {
		err = errVolumeReplaced
	}
	if err != nil && plan.Rejected != nil {
		return errors.Join(err, plan.Rejected(context.WithoutCancel(ctx), err))
	}
	return err
}

// volumeIdentities returns when Docker created each volume, refusing one that no longer exists.
func volumeIdentities(ctx context.Context, inspect func(ctx context.Context, name string) (types.Volume, bool, error), names []string) (map[string]string, error) {
	created := make(map[string]string, len(names))
	for _, name := range names {
		found, exists, err := inspect(ctx, name)
		switch {
		case err != nil:
			return nil, fmt.Errorf("inspect volume %s: %w", name, err)
		case !exists:
			return nil, common.Classify(common.ErrConflict, fmt.Errorf("volume %s was removed after the backup was queued, so it was not backed up; start the backup again", name))
		}
		created[name] = found.CreatedAt
	}
	return created, nil
}

// Write tars each part from its root, so the archive's root entry keeps the part's owner and mode, and hands it to Add.
func (o ArchiveOutput) Write(ctx context.Context, dockerClient *client.Client) error {
	if len(o.Parts) == 0 {
		return nil
	}
	helper, err := dockerClient.ContainerCreate(ctx, client.ContainerCreateOptions{
		Config:     &container.Config{Image: o.Image, Cmd: []string{"sleep", "infinity"}, NetworkDisabled: true, Labels: volumes.Labels()},
		HostConfig: volumes.HostConfig(o.Image, nil, o.Parts),
	})
	if err != nil {
		return fmt.Errorf("create archive helper: %w", err)
	}
	defer func() {
		_, _ = dockerClient.ContainerRemove(context.WithoutCancel(ctx), helper.ID, volumes.RemoveOptions())
	}()
	if _, err = dockerClient.ContainerStart(ctx, helper.ID, client.ContainerStartOptions{}); err != nil {
		return fmt.Errorf("start archive helper: %w", err)
	}
	if mkdirErr := runInHelper(ctx, dockerClient, helper.ID, "mkdir", "-p", archiveDir); mkdirErr != nil {
		return mkdirErr
	}
	for index, part := range o.Parts {
		archive := path.Join(archiveDir, strconv.Itoa(index)+".tar.gz")
		if tarErr := runInHelper(ctx, dockerClient, helper.ID, "tar", "-czf", archive, "-C", part.Target, "."); tarErr != nil {
			return fmt.Errorf("archive %s: %w", part.Source, tarErr)
		}
		if addErr := o.add(ctx, dockerClient, helper.ID, archive, part); addErr != nil {
			return addErr
		}
		// Each archive is dropped once handed over, so the helper never holds more than one.
		if rmErr := runInHelper(ctx, dockerClient, helper.ID, "rm", "-f", archive); rmErr != nil {
			return rmErr
		}
	}
	return nil
}

func (o ArchiveOutput) add(ctx context.Context, dockerClient *client.Client, containerID, archive string, part mount.Mount) error {
	reader, size, err := docker.CopyFromContainer(ctx, dockerClient, containerID, archive)
	if err != nil {
		return fmt.Errorf("read the archive of %s: %w", part.Source, err)
	}
	defer func() { _ = reader.Close() }()
	return o.Add(ctx, part, size, reader)
}

func runInHelper(ctx context.Context, dockerClient *client.Client, containerID string, cmd ...string) error {
	var output bytes.Buffer
	exitCode, err := docker.ExecInContainer(ctx, dockerClient, docker.ExecRequest{
		Stdout:      &output,
		Stderr:      &output,
		ContainerID: containerID,
		Options:     client.ExecCreateOptions{AttachStdout: true, AttachStderr: true, Cmd: cmd},
	})
	if err != nil {
		return err
	}
	if exitCode != 0 {
		return fmt.Errorf("%s exited with code %d: %s", cmd[0], exitCode, strings.TrimSpace(output.String()))
	}
	return nil
}
