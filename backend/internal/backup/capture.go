package backup

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"maps"
	"path"
	"slices"
	"strconv"

	"github.com/containerd/errdefs"
	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/api/types/mount"
	"github.com/moby/moby/client"

	"kit.getarcane.app/docker"

	"getarcane.dev/arcane/v3/internal/common"
	dockerInternal "getarcane.dev/arcane/v3/internal/docker"
	"getarcane.dev/arcane/v3/pkg/projects"
	"getarcane.dev/arcane/v3/pkg/timeouts"
	"getarcane.dev/arcane/v3/pkg/volumes"
	"getarcane.dev/types/v3"
)

type (
	CapturePlan struct {
		Inspect  func(ctx context.Context, name string) (types.Volume, bool, error)
		Stop     func(ctx context.Context) (func(context.Context) error, error)
		Watch    func(ctx context.Context) (func(context.Context) error, error)
		Verify   func(ctx context.Context) error
		Rejected func(ctx context.Context, err error) error
		Mounts   []mount.Mount
		Volumes  []string
	}

	ArchiveOutput struct {
		Add   func(ctx context.Context, part mount.Mount, size int64, body io.Reader) error
		Exec  func(ctx context.Context, containerID, execUser string, cmd []string) (string, string, error)
		Image string
		Parts []mount.Mount
	}

	ProjectStop struct {
		Docker    *dockerInternal.DockerClientService
		Stop      func(ctx context.Context, projectID string, services []string, user types.Actor) error
		User      types.Actor
		ProjectID string
		Compose   string
		Active    []container.Summary
	}
)

const (
	SnapshotRoot = "/snapshot"
	archiveDir   = "/archives"
)

var errVolumeReplaced = common.Classify(common.ErrConflict, errors.New("a volume was removed or replaced while it was copied, so the backup failed; start it again"))

func VolumePart(name string) mount.Mount {
	return mount.Mount{Type: mount.TypeVolume, Source: name, Target: path.Join(SnapshotRoot, "volumes", name), ReadOnly: true}
}

func InspectVolume(ctx context.Context, dockerClient *client.Client, name string) (types.Volume, bool, error) {
	inspect, err := dockerClient.VolumeInspect(ctx, name, client.VolumeInspectOptions{})
	if errdefs.IsNotFound(err) {
		return types.Volume{}, false, nil
	}
	if err != nil {
		return types.Volume{}, false, err
	}
	return types.NewVolume(inspect.Volume), true, nil
}

func Capture(ctx context.Context, plan CapturePlan, read func(ctx context.Context, mounts []mount.Mount) error) (err error) {
	before, err := volumeIdentities(ctx, plan.Inspect, plan.Volumes)
	if err != nil {
		return err
	}
	unchanged := func(context.Context) error { return nil }
	if plan.Watch != nil {
		if unchanged, err = plan.Watch(ctx); err != nil {
			return err
		}
	}
	if plan.Stop != nil {
		start, stopErr := plan.Stop(ctx)
		if start != nil {
			defer func() {
				if startErr := start(context.WithoutCancel(ctx)); startErr != nil {
					err = errors.Join(err, fmt.Errorf("start the stopped containers again: %w", startErr))
				}
			}()
		}
		if stopErr != nil {
			return stopErr
		}
	}
	if plan.Verify != nil {
		if verifyErr := plan.Verify(ctx); verifyErr != nil {
			return verifyErr
		}
	}
	if readErr := read(ctx, plan.Mounts); readErr != nil {
		return readErr
	}
	err = unchanged(ctx)
	checkCtx, cancelCheck := context.WithTimeout(context.WithoutCancel(ctx), timeouts.DefaultDockerAPI)
	if err == nil {
		var after map[string]string
		after, err = volumeIdentities(checkCtx, plan.Inspect, plan.Volumes)
		if err == nil && !maps.Equal(before, after) {
			err = errVolumeReplaced
		}
	}
	if err == nil && plan.Verify != nil {
		err = plan.Verify(checkCtx)
	}
	cancelCheck()
	if err != nil && plan.Rejected != nil {
		return errors.Join(err, plan.Rejected(context.WithoutCancel(ctx), err))
	}
	return err
}

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
		created[name] = fmt.Sprintf("%q %q %q %v %v", found.CreatedAt, found.Driver, found.Mountpoint, found.Labels, found.Options)
	}
	return created, nil
}

func (p ProjectStop) Run(ctx context.Context) error {
	if err := p.Stop(ctx, p.ProjectID, projects.ContainerServices(p.Active), p.User); err != nil {
		return err
	}
	dockerClient, err := p.Docker.GetClient(ctx)
	if err != nil {
		return err
	}
	running, err := volumes.ListRunningContainers(ctx, dockerClient)
	if err != nil {
		return err
	}
	if still, _ := projects.ProjectContainerUse(p.Compose, running); len(still) > 0 {
		return common.Classify(common.ErrConflict, fmt.Errorf(
			"%d container(s) of project %s are still running after its services stopped, such as one-off runs or removed services; nothing was written or copied",
			len(still), p.Compose,
		))
	}
	return nil
}

func CheckNoWriters(ctx context.Context, dockerService *dockerInternal.DockerClientService, names []string) error {
	dockerClient, err := dockerService.GetClient(ctx)
	if err != nil {
		return err
	}
	running, err := volumes.ListRunningContainers(ctx, dockerClient)
	if err != nil {
		return err
	}
	for _, c := range running {
		for _, m := range c.Mounts {
			if m.Type == mount.TypeVolume && m.RW && slices.Contains(names, m.Name) {
				return common.Classify(common.ErrConflict, fmt.Errorf("container %s writes to volume %s; stop it or export again", docker.ContainerNameFromNames(c.Names), m.Name))
			}
		}
	}
	return nil
}

func (o ArchiveOutput) Write(ctx context.Context, dockerService *dockerInternal.DockerClientService) error {
	if len(o.Parts) == 0 {
		return nil
	}
	dockerClient, err := dockerService.GetClient(ctx)
	if err != nil {
		return err
	}
	helperID, removeHelper, err := StartHelper(ctx, dockerService, o.Image, o.Parts)
	if err != nil {
		return err
	}
	defer removeHelper()
	if _, _, mkdirErr := o.Exec(ctx, helperID, "", []string{"mkdir", "-p", archiveDir}); mkdirErr != nil {
		return mkdirErr
	}
	for index, part := range o.Parts {
		archive := path.Join(archiveDir, strconv.Itoa(index)+".tar.gz")
		if _, _, tarErr := o.Exec(ctx, helperID, "", []string{"tar", "-czf", archive, "-C", part.Target, "."}); tarErr != nil {
			return fmt.Errorf("archive %s: %w", part.Source, tarErr)
		}
		if addErr := o.add(ctx, dockerClient, helperID, archive, part); addErr != nil {
			return addErr
		}
		if index+1 < len(o.Parts) {
			if _, _, rmErr := o.Exec(ctx, helperID, "", []string{"rm", "-f", archive}); rmErr != nil {
				return rmErr
			}
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

func StartHelper(ctx context.Context, dockerService *dockerInternal.DockerClientService, image string, mounts []mount.Mount) (string, func(), error) {
	helper, err := dockerService.Call(ctx, func(ctx context.Context, dockerClient *client.Client) (client.ContainerCreateResult, error) {
		return dockerClient.ContainerCreate(ctx, client.ContainerCreateOptions{
			Config:     &container.Config{Image: image, Cmd: []string{"sleep", "infinity"}, NetworkDisabled: true, Labels: volumes.Labels()},
			HostConfig: volumes.HostConfig(image, nil, mounts),
		})
	})
	if err != nil {
		return "", nil, fmt.Errorf("create helper container: %w", err)
	}
	remove := func() {
		if _, removeErr := dockerService.Call(context.WithoutCancel(ctx), func(ctx context.Context, dockerClient *client.Client) (client.ContainerRemoveResult, error) {
			return dockerClient.ContainerRemove(ctx, helper.ID, volumes.RemoveOptions())
		}); removeErr != nil {
			slog.WarnContext(ctx, "Failed to remove the backup helper container", "containerId", helper.ID, "error", removeErr)
		}
	}
	if _, err = dockerService.Call(ctx, func(ctx context.Context, dockerClient *client.Client) (client.ContainerStartResult, error) {
		return dockerClient.ContainerStart(ctx, helper.ID, client.ContainerStartOptions{})
	}); err != nil {
		remove()
		return "", nil, fmt.Errorf("start helper container: %w", err)
	}
	return helper.ID, remove, nil
}
