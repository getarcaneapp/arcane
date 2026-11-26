package backup

import (
	"cmp"
	"context"
	"fmt"
	"log/slog"
	"slices"

	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/api/types/mount"
	"github.com/moby/moby/client"

	"kit.getarcane.app"
	"kit.getarcane.app/docker"

	"getarcane.dev/arcane/v3/internal/backup"
	"getarcane.dev/arcane/v3/internal/common"
	"getarcane.dev/arcane/v3/internal/database"
	"getarcane.dev/arcane/v3/internal/event"
	"getarcane.dev/arcane/v3/internal/scheduler/runs"
	"getarcane.dev/arcane/v3/pkg/selfcontainer"
	"getarcane.dev/types/v3"
)

const (
	backupMountMissingWarning = "No volume is mounted at /backups in the Arcane container. Backups will only live inside Docker unless you mount a host path."

	systemRecoverySnapshotLabel = "arcane-system-recovery"
)

func backupMountWarningFromArcaneMounts(mounts []container.MountPoint) string {
	restoresMounted := slices.ContainsFunc(mounts, func(m container.MountPoint) bool { return m.Destination == "/restores" })
	if docker.MountForDestination(mounts, "/backups", "/backups") != nil || restoresMounted {
		return ""
	}
	return backupMountMissingWarning
}

func (s *Service) backupMountWarning(ctx context.Context) string {
	inspect, err := s.deps.Docker.Call(ctx, selfcontainer.InspectCurrentArcaneContainer)
	if err != nil {
		return ""
	}
	return backupMountWarningFromArcaneMounts(inspect.Mounts)
}

func backupArchiveFilename(backupID string) (string, error) {
	name, err := kit.ValidateFileName(backupID)
	if err != nil {
		return "", fmt.Errorf("invalid backup id: %w", err)
	}
	return name + ".tar.gz", nil
}

func (s *Service) snapshotRepository(ctx context.Context, dockerClient *client.Client, entry *types.VolumeBackup) (backup.Repository, string, error) {
	if entry.LocalSnapshotID != "" {
		local, err := s.deps.Repository.Local(ctx, dockerClient, backup.LocalRepositoryOptions{ReadOnly: true})
		return local, entry.LocalSnapshotID, err
	}
	if entry.RemoteSnapshotID != "" {
		remote, err := s.deps.Repository.Remote(ctx, entry.S3DestinationID, entry.RemoteInstanceID)
		return remote, entry.RemoteSnapshotID, err
	}
	return backup.Repository{}, "", common.ErrVolumeBackupNoSnapshot
}

func (s *Service) acquireVolumeRun(ctx context.Context, volumeName string) (*runs.Lease, error) {
	lease, admitted, err := s.deps.Engine.TryAcquireRun(ctx, backup.VolumeAdmissionScope, volumeName)
	if err != nil {
		return nil, err
	}
	if !admitted {
		return nil, s.deps.AlreadyRunning
	}
	return lease, nil
}

func (s *Service) logBackupEvent(ctx context.Context, eventType event.EventType, volumeName string, user *types.Actor, metadata database.JSON) {
	actor := cmp.Or(user, &types.SystemUser)
	if err := s.deps.Events.LogResourceEvent(ctx, event.ResourceEvent{
		Type:          eventType,
		ResourceType:  "volume",
		ResourceID:    volumeName,
		ResourceName:  volumeName,
		UserID:        actor.ID,
		Username:      actor.Username,
		EnvironmentID: "0",
		Metadata:      metadata,
	}); err != nil {
		slog.WarnContext(ctx, "could not log volume backup event", "volume", volumeName, "action", metadata["action"], "error", err)
	}
}

func (s *Service) createBackupTempContainerWithMount(ctx context.Context, backupMount mount.Mount) (string, func(), error) {
	helperImage, err := s.deps.HelperImage(ctx)
	if err != nil {
		return "", nil, err
	}
	return backup.StartHelper(ctx, s.deps.Docker, helperImage, []mount.Mount{backupMount})
}

func (s *Service) createBackupTempContainer(ctx context.Context, target string, readOnly bool) (string, func(), error) {
	slog.DebugContext(ctx, "volume service: create backup temp container", "target", target, "readOnly", readOnly)
	dockerClient, err := s.deps.Docker.GetClient(ctx)
	if err != nil {
		return "", nil, err
	}
	backupMount, err := s.deps.Repository.StorageMount(ctx, dockerClient, target, readOnly)
	if err != nil {
		return "", nil, err
	}
	return s.createBackupTempContainerWithMount(ctx, backupMount)
}

func runSnapshots(entry *types.VolumeBackup) backup.RunSnapshots {
	return backup.RunSnapshots{
		LocalID:          &entry.LocalSnapshotID,
		RemoteID:         &entry.RemoteSnapshotID,
		S3DestinationID:  &entry.S3DestinationID,
		Size:             &entry.Size,
		RemoteInstanceID: entry.RemoteInstanceID,
	}
}

func snapshotRun(entry *types.VolumeBackup, plan backupPlan) backup.SnapshotRun {
	return backup.SnapshotRun{
		Snapshots:          runSnapshots(entry),
		RunID:              entry.ID,
		Label:              entry.VolumeName,
		Tags:               []string{backup.RunSnapshotTag(entry.ID)},
		DestinationOptions: backup.DestinationOptions{Local: plan.LocalEnabled, Remote: plan.S3Enabled},
	}
}
