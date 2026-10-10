package backup

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"strings"
	"time"

	usertypes "github.com/getarcaneapp/arcane/types/v2/user"
	"github.com/getarcaneapp/arcane/types/v2/volume"
	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/api/types/mount"
	"github.com/moby/moby/client"
	"go.getarcane.app/docker"
	kit "go.getarcane.app/kit/pkg"

	"github.com/getarcaneapp/arcane/backend/v2/internal/backup"
	"github.com/getarcaneapp/arcane/backend/v2/internal/database"
	"github.com/getarcaneapp/arcane/backend/v2/internal/event"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/libarcane"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/libarcane/volumehelper"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/projects"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/scheduler/runs"
)

const (
	backupMountMissingWarning = "No volume is mounted at /backups in the Arcane container. Backups will only live inside Docker unless you mount a host path."

	volumeBackupContainerRecoveryTimeout  = 30 * time.Second
	volumeBackupContainerRecoveryInterval = 500 * time.Millisecond

	volumeRusticRepositoryPath = "/repository/volumes"
	localVolumeRepositoryID    = "volumes:local"
	legacyVolumePasswordSalt   = "arcane-volume-backups:"

	systemRecoverySnapshotLabel = "arcane-system-recovery"
)

// resolveBackupStorageMountFromMounts mirrors the Arcane container's /backups mount at target, or returns nil.
func resolveBackupStorageMountFromMounts(ctx context.Context, mounts []container.MountPoint, target string, readOnly bool) *mount.Mount {
	mirroredMount := docker.MountForDestination(mounts, "/backups", target)
	if mirroredMount == nil {
		return nil
	}
	if !readOnly && mirroredMount.ReadOnly {
		slog.WarnContext(ctx, "volume service: requested writable backup mount but source is read-only; writes may fail")
	}
	mirroredMount.ReadOnly = readOnly
	return mirroredMount
}

// backupMountWarningFromArcaneMounts warns unless Arcane mounts /backups; a /restores mount also suppresses it for compatibility.
func backupMountWarningFromArcaneMounts(mounts []container.MountPoint) string {
	restoresMounted := slices.ContainsFunc(mounts, func(m container.MountPoint) bool { return m.Destination == "/restores" })
	if docker.MountForDestination(mounts, "/backups", "/backups") != nil || restoresMounted {
		return ""
	}
	return backupMountMissingWarning
}

func (s *Service) backupMountWarning(ctx context.Context) string {
	dockerClient, err := s.deps.Docker.GetClient(ctx)
	if err != nil {
		return ""
	}
	// Cannot determine Arcane mount status (e.g. running outside Docker); suppress warning.
	inspect, err := libarcane.InspectCurrentArcaneContainer(ctx, dockerClient)
	if err != nil {
		return ""
	}
	return backupMountWarningFromArcaneMounts(inspect.Mounts)
}

// StorageMount resolves the backup repository mount shared with system-backup and workspace operations.
// It mirrors Arcane's own /backups mount and otherwise falls back to the dedicated named backup volume.
func (s *Service) StorageMount(ctx context.Context, dockerClient *client.Client, target string, readOnly bool) (mount.Mount, error) {
	if dockerClient != nil {
		inspect, err := libarcane.InspectCurrentArcaneContainer(ctx, dockerClient)
		if err != nil {
			slog.WarnContext(ctx, "volume service: failed to inspect arcane container for backup mount resolution, falling back to named volume", "error", err.Error())
		} else if mirrored := resolveBackupStorageMountFromMounts(ctx, inspect.Mounts, target, readOnly); mirrored != nil {
			return *mirrored, nil
		}
	}
	slog.DebugContext(ctx, "volume service: ensure backup volume", "backupVolume", s.deps.BackupVolumeName)
	ensureClient, err := s.deps.Docker.GetClient(ctx)
	if err != nil {
		return mount.Mount{}, err
	}
	if _, inspectErr := ensureClient.VolumeInspect(ctx, s.deps.BackupVolumeName, client.VolumeInspectOptions{}); inspectErr != nil {
		if _, createErr := ensureClient.VolumeCreate(ctx, client.VolumeCreateOptions{Name: s.deps.BackupVolumeName}); createErr != nil {
			return mount.Mount{}, fmt.Errorf("failed to create backup volume: %w", createErr)
		}
	}
	return mount.Mount{Type: mount.TypeVolume, Source: s.deps.BackupVolumeName, Target: target, ReadOnly: readOnly}, nil
}

func (s *Service) stopRunningContainersForBackup(ctx context.Context, dockerClient *client.Client, volumeName string, user usertypes.Actor, refuseArcaneWriters bool) ([]container.Summary, error) {
	if s.deps.StopContainer == nil || s.deps.StartContainer == nil {
		return nil, errors.New("container service is unavailable")
	}
	containers, err := dockerClient.ContainerList(ctx, client.ContainerListOptions{})
	if err != nil {
		return nil, fmt.Errorf("failed to list running containers before volume backup: %w", err)
	}

	eligible := make([]container.Summary, 0, len(containers.Items))
	arcaneOwned := make([]container.Summary, 0, 2)
	for _, candidate := range containers.Items {
		if strings.EqualFold(candidate.Labels["com.getarcaneapp.arcane"], "true") || strings.EqualFold(candidate.Labels["com.getarcaneapp.arcane.agent"], "true") {
			arcaneOwned = append(arcaneOwned, candidate)
			continue
		}
		// Arcane's helper containers are auto-removed when stopped, so stopping
		// one would leave nothing for the restart pass and fail the operation.
		if strings.EqualFold(candidate.Labels[libarcane.InternalResourceLabel], "true") {
			continue
		}
		eligible = append(eligible, candidate)
	}
	// Restores rewrite the volume in place; refusing beats corrupting the data
	// under a labeled container this pass deliberately never stops.
	if refuseArcaneWriters {
		if writers := docker.FilterContainersUsingVolume(arcaneOwned, volumeName); len(writers) > 0 {
			return nil, fmt.Errorf("volume %s is mounted by a running Arcane-managed container; restoring under it would corrupt its data", volumeName)
		}
	}
	containerIDs := docker.FilterContainersUsingVolume(eligible, volumeName)
	stopped := make([]container.Summary, 0, len(containerIDs))
	for _, candidate := range eligible {
		if !slices.Contains(containerIDs, candidate.ID) {
			continue
		}
		if stopContainerErr := s.deps.StopContainer(ctx, candidate.ID, user); stopContainerErr != nil {
			stillStopped, restartErr := s.startContainersAfterBackup(context.WithoutCancel(ctx), dockerClient, stopped, user)
			return stillStopped, errors.Join(fmt.Errorf("failed to stop container %s before volume backup: %w", candidate.ID, stopContainerErr), restartErr)
		}
		stopped = append(stopped, candidate)
	}
	return stopped, nil
}

//nolint:gocognit // recovery retries must reconcile IDs, names, and Compose identities in one bounded loop
func (s *Service) startContainersAfterBackup(ctx context.Context, dockerClient *client.Client, stoppedContainers []container.Summary, user usertypes.Actor) ([]container.Summary, error) {
	recoveryCtx, cancel := context.WithTimeout(ctx, volumeBackupContainerRecoveryTimeout)
	defer cancel()

	remaining := append([]container.Summary(nil), stoppedContainers...)
	lastErrors := make(map[string]error, len(stoppedContainers))
	for len(remaining) > 0 {
		currentContainers, listErr := dockerClient.ContainerList(recoveryCtx, client.ContainerListOptions{All: true})
		if listErr == nil {
			currentByID := make(map[string]container.Summary, len(currentContainers.Items))
			currentByName := make(map[string]container.Summary, len(currentContainers.Items))
			for _, current := range currentContainers.Items {
				currentByID[current.ID] = current
				if name := docker.ContainerNameFromNames(current.Names); name != "" {
					currentByName[name] = current
				}
			}

			nextRemaining := make([]container.Summary, 0, len(remaining))
			for _, stopped := range remaining {
				current, found := currentByID[stopped.ID]
				if !found {
					name := docker.ContainerNameFromNames(stopped.Names)
					current, found = currentByName[name]
				}
				if !found {
					current, found = projects.FindComposeReplica(currentContainers.Items, stopped.Labels)
				}
				if !found {
					nextRemaining = append(nextRemaining, stopped)
					continue
				}
				if current.State == container.StateRunning || current.State == container.StateRestarting {
					if current.ID != stopped.ID {
						slog.InfoContext(ctx, "volume service: container was replaced during backup and is already running", "previousContainer", stopped.ID, "currentContainer", current.ID)
					}
					continue
				}
				if startErr := s.deps.StartContainer(recoveryCtx, current.ID, user); startErr != nil {
					lastErrors[stopped.ID] = startErr
					nextRemaining = append(nextRemaining, stopped)
					continue
				}
				if current.ID != stopped.ID {
					slog.InfoContext(ctx, "volume service: restarted replacement container after backup", "previousContainer", stopped.ID, "currentContainer", current.ID)
				}
			}
			remaining = nextRemaining
		} else {
			for _, stopped := range remaining {
				lastErrors[stopped.ID] = listErr
			}
		}

		if len(remaining) == 0 {
			return nil, nil
		}
		timer := time.NewTimer(volumeBackupContainerRecoveryInterval)
		select {
		case <-recoveryCtx.Done():
			timer.Stop()
			var restartErr error
			for _, stopped := range remaining {
				if lastErr := lastErrors[stopped.ID]; lastErr != nil {
					restartErr = errors.Join(restartErr, fmt.Errorf("failed to restart container %s after volume backup: %w", stopped.ID, lastErr))
				} else {
					restartErr = errors.Join(restartErr, fmt.Errorf("failed to restart container %s after volume backup: replacement did not appear within %s", stopped.ID, volumeBackupContainerRecoveryTimeout))
				}
			}
			return remaining, restartErr
		case <-timer.C:
		}
	}
	return nil, nil
}

func backupArchiveFilename(backupID string) (string, error) {
	name, err := kit.ValidateFileName(backupID)
	if err != nil {
		return "", fmt.Errorf("invalid backup id: %w", err)
	}
	return name + ".tar.gz", nil
}

// volumeBackupPassword returns the recovery key once one is stored, re-keying the given repositories off the legacy derivation first.
func (s *Service) volumeBackupPassword(ctx context.Context, dockerClient *client.Client, repositories ...backup.Repository) (string, error) {
	legacyPassword := kit.SHA256Hex(legacyVolumePasswordSalt + s.deps.EncryptionKey)
	if s.deps.RecoveryKeys == nil {
		return legacyPassword, nil
	}
	key, err := s.deps.RecoveryKeys.Get(ctx)
	if errors.Is(err, backup.ErrRecoveryKeyNotConfigured) {
		return legacyPassword, nil
	}
	if err != nil {
		return "", err
	}
	if s.deps.Engine == nil {
		return key, nil
	}
	// Re-key each legacy repository once per process; one that opens with neither password is left for the caller to create or report.
	for _, repository := range repositories {
		if done, _ := s.rekeyed.Load(repository.ID); done == key {
			continue
		}
		if _, listErr := s.deps.Engine.ListSnapshots(ctx, dockerClient, repository, key); listErr == nil {
			s.rekeyed.Store(repository.ID, key)
			continue
		}
		if repository.ID == localVolumeRepositoryID {
			writable, localErr := s.localRusticRepository(ctx, dockerClient, false)
			if localErr != nil {
				slog.WarnContext(ctx, "could not open the local volume backup repository for re-keying", "error", localErr.Error())
				continue
			}
			repository = writable
		}
		if changeErr := s.deps.Engine.ChangeRepositoryPassword(ctx, dockerClient, repository, legacyPassword, key); changeErr != nil {
			slog.DebugContext(ctx, "volume backup repository was not re-keyed", "repository", repository.ID, "error", changeErr.Error())
			continue
		}
		slog.InfoContext(ctx, "Re-keyed volume backup repository to the recovery key", "repository", repository.ID)
		s.rekeyed.Store(repository.ID, key)
	}
	return key, nil
}

func (s *Service) localRusticRepository(ctx context.Context, dockerClient *client.Client, readOnly bool) (backup.Repository, error) {
	storage, err := s.StorageMount(ctx, dockerClient, "/repository", readOnly)
	if err != nil {
		return backup.Repository{}, err
	}
	return backup.Repository{
		ID:          localVolumeRepositoryID,
		Environment: []string{"RUSTIC_REPOSITORY=" + volumeRusticRepositoryPath},
		Mounts:      []mount.Mount{storage},
	}, nil
}

// remoteInstanceID resolves the instance whose S3 root holds a backup; empty means this instance.
func (s *Service) remoteInstanceID(instanceID string) string {
	return cmp.Or(strings.TrimSpace(instanceID), strings.TrimSpace(s.deps.Settings.GetSettingsConfig().InstanceID.Value))
}

// remoteRusticRepository addresses one instance's root on the destination; empty instanceID means this instance.
func (s *Service) remoteRusticRepository(ctx context.Context, destinationID, instanceID string) (backup.Repository, error) {
	if s.deps.S3Destinations == nil {
		return backup.Repository{}, errors.New("S3 backup destinations are unavailable")
	}
	instanceID = s.remoteInstanceID(instanceID)
	if instanceID == "" {
		return backup.Repository{}, errors.New("arcane instance ID is unavailable")
	}
	configuration, err := s.deps.S3Destinations.Configuration(ctx, destinationID)
	if err != nil {
		return backup.Repository{}, errors.New("the selected S3 backup destination is not configured")
	}
	return backup.Repository{
		ID:          "volumes:s3:" + destinationID + ":" + instanceID,
		Environment: configuration.RusticEnvironment(backup.VolumeRoot, instanceID),
	}, nil
}

// acquireVolumeRun takes the volume's run lease, failing with AlreadyRunning while another backup operation holds it.
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

// backupDestination names the destination that holds a backup's local and remote copies.
func backupDestination(local, remote bool) volume.BackupDestination {
	switch {
	case local && remote:
		return volume.BackupDestinationLocalS3
	case remote:
		return volume.BackupDestinationS3
	default:
		return volume.BackupDestinationLocal
	}
}

// logBackupEvent records a volume backup event; a nil user logs as the system user.
func (s *Service) logBackupEvent(ctx context.Context, eventType event.EventType, volumeName string, user *usertypes.Actor, metadata database.JSON) {
	actor := cmp.Or(user, &usertypes.SystemUser)
	if err := s.deps.Events.LogVolumeEvent(ctx, eventType, volumeName, volumeName, actor.ID, actor.Username, "0", metadata); err != nil {
		slog.WarnContext(ctx, "could not log volume backup event", "volume", volumeName, "action", metadata["action"], "error", err)
	}
}

func (s *Service) createBackupTempContainerWithMount(ctx context.Context, dockerClient *client.Client, backupMount mount.Mount) (string, func(), error) {
	helperImage, err := s.deps.HelperImage(ctx, dockerClient)
	if err != nil {
		return "", nil, err
	}
	resp, err := dockerClient.ContainerCreate(ctx, client.ContainerCreateOptions{
		Config:     &container.Config{Image: helperImage, Cmd: []string{"sleep", "infinity"}, NetworkDisabled: true, Labels: volumehelper.Labels()},
		HostConfig: volumehelper.HostConfig(helperImage, nil, []mount.Mount{backupMount}),
	})
	if err != nil {
		return "", nil, fmt.Errorf("failed to create backup temp container: %w", err)
	}
	if _, containerStartErr := dockerClient.ContainerStart(ctx, resp.ID, client.ContainerStartOptions{}); containerStartErr != nil {
		_, _ = dockerClient.ContainerRemove(ctx, resp.ID, volumehelper.RemoveOptions())
		return "", nil, fmt.Errorf("failed to start backup temp container: %w", containerStartErr)
	}
	return resp.ID, func() {
		_, _ = dockerClient.ContainerRemove(context.WithoutCancel(ctx), resp.ID, volumehelper.RemoveOptions())
	}, nil
}

func (s *Service) createBackupTempContainer(ctx context.Context, dockerClient *client.Client, target string, readOnly bool) (string, func(), error) {
	slog.DebugContext(ctx, "volume service: create backup temp container", "target", target, "readOnly", readOnly)
	var err error
	if dockerClient == nil {
		if dockerClient, err = s.deps.Docker.GetClient(ctx); err != nil {
			return "", nil, err
		}
	}
	backupMount, err := s.StorageMount(ctx, dockerClient, target, readOnly)
	if err != nil {
		return "", nil, err
	}
	return s.createBackupTempContainerWithMount(ctx, dockerClient, backupMount)
}
