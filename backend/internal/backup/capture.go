package backup

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"maps"
	"path"
	"slices"
	"strconv"
	"strings"

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
	// CapturePlan is one point-in-time read of a subject: its parts, mounted read-only under SnapshotRoot, and how to
	// quiesce it. The caller holds the locks that keep the parts from changing hands.
	CapturePlan struct {
		// Inspect reports a Docker volume, so the capture can tell the volumes it read are the ones it planned.
		Inspect func(ctx context.Context, name string) (types.Volume, bool, error)
		// Stop quiesces the subject and returns what starts it again; nil when nothing needs stopping.
		Stop func(ctx context.Context) (func(context.Context) error, error)
		// Verify, when set, refuses a read something could have changed: it runs once the subject stopped and again after
		// the read, before the subject starts again.
		Verify func(ctx context.Context) error
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

	// ProjectStop stops a Compose project's active containers for a capture or a restore.
	ProjectStop struct {
		Docker    *dockerInternal.DockerClientService
		Stop      func(ctx context.Context, projectID string, services []string, user types.Actor) error
		User      types.Actor
		ProjectID string
		// Compose names the Compose project; Active are its running and restarting containers.
		Compose string
		Active  []container.Summary
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
	if plan.Verify != nil {
		if verifyErr := plan.Verify(ctx); verifyErr != nil {
			return verifyErr
		}
	}
	if readErr := read(ctx, plan.Mounts); readErr != nil {
		return readErr
	}
	// A shutdown can't cut the check short: a cancelled check would reject a read that finished. It is still bounded,
	// since the subject stays stopped until it returns.
	checkCtx, cancelCheck := context.WithTimeout(context.WithoutCancel(ctx), timeouts.DefaultDockerAPI)
	after, err := volumeIdentities(checkCtx, plan.Inspect, plan.Volumes)
	if err == nil && !maps.Equal(before, after) {
		err = errVolumeReplaced
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

// Run stops the services of the project's active containers, then refuses any of its containers still running:
// Compose stops only the services its files name, so a one-off run, a hook or job, or a removed service keeps running.
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

// CheckNoWriters refuses volumes a running container mounts writable, so a capture called consistent copies data that
// nothing changes during the copy.
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

// Write tars each part from its root, so the archive's root entry keeps the part's owner and mode, and hands it to Add.
// The helper's create, start and removal are bounded by Docker's API timeout; the archiving is not.
func (o ArchiveOutput) Write(ctx context.Context, dockerService *dockerInternal.DockerClientService) error {
	if len(o.Parts) == 0 {
		return nil
	}
	dockerClient, err := dockerService.GetClient(ctx)
	if err != nil {
		return err
	}
	helper, err := dockerService.Call(ctx, func(ctx context.Context, dockerClient *client.Client) (client.ContainerCreateResult, error) {
		return dockerClient.ContainerCreate(ctx, client.ContainerCreateOptions{
			Config:     &container.Config{Image: o.Image, Cmd: []string{"sleep", "infinity"}, NetworkDisabled: true, Labels: volumes.Labels()},
			HostConfig: volumes.HostConfig(o.Image, nil, o.Parts),
		})
	})
	if err != nil {
		return fmt.Errorf("create archive helper: %w", err)
	}
	defer func() {
		_, _ = dockerService.Call(context.WithoutCancel(ctx), func(ctx context.Context, dockerClient *client.Client) (client.ContainerRemoveResult, error) {
			return dockerClient.ContainerRemove(ctx, helper.ID, volumes.RemoveOptions())
		})
	}()
	if _, err = dockerService.Call(ctx, func(ctx context.Context, dockerClient *client.Client) (client.ContainerStartResult, error) {
		return dockerClient.ContainerStart(ctx, helper.ID, client.ContainerStartOptions{})
	}); err != nil {
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
