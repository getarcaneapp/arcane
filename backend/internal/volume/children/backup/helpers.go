package backup

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"path"
	"strings"
	"time"

	"github.com/getarcaneapp/arcane/types/v2/scheduler"
	usertypes "github.com/getarcaneapp/arcane/types/v2/user"
	"github.com/getarcaneapp/arcane/types/v2/volume"
	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/api/types/mount"
	"github.com/moby/moby/client"
	"github.com/samber/mo"
	"go.getarcane.app/docker"
	"go.getarcane.app/kit/pkg"

	"github.com/getarcaneapp/arcane/backend/v2/internal/backup"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/libarcane"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/libarcane/volumehelper"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/projects"
)

type backupStorageMode string

const (
	// backupStorageModeArcaneMount means backup helpers mirror an existing Arcane
	// container mount at /backups. This intentionally covers any mount the Arcane
	// container already has at /backups, not exclusively bind mounts.
	backupStorageModeArcaneMount backupStorageMode = "arcane_mount"
	// backupStorageModeNamedVolumeFallback means no suitable Arcane container
	// mount was found, so Arcane's dedicated named backup volume is used.
	backupStorageModeNamedVolumeFallback backupStorageMode = "named_volume_fallback"

	backupMountMissingWarning = "No volume is mounted at /backups in the Arcane container. Backups will only live inside Docker unless you mount a host path."

	volumeBackupContainerRecoveryTimeout  = 30 * time.Second
	volumeBackupContainerRecoveryInterval = 500 * time.Millisecond

	volumeRusticRepositoryPath       = "/repository/volumes"
	localVolumeRepositoryID          = "volumes:local"
	legacyVolumePasswordSaltInternal = "arcane-volume-backups:"

	systemRecoverySnapshotLabel = "arcane-system-recovery"
)

type backupStorageMountInternal struct {
	mode           backupStorageMode
	mount          mount.Mount
	requiresEnsure bool
}

func resolveBackupStorageMountFromMountsInternal(ctx context.Context, mounts []container.MountPoint, target string, readOnly bool) mo.Option[backupStorageMountInternal] {
	mirroredMount := docker.MountForDestination(mounts, "/backups", target)
	if mirroredMount == nil {
		return mo.None[backupStorageMountInternal]()
	}
	// MountForDestination only returns non-nil for bind and named volume mounts.

	if !readOnly && mirroredMount.ReadOnly {
		slog.WarnContext(ctx, "volume service: requested writable backup mount but source is read-only; writes may fail")
	}
	mirroredMount.ReadOnly = readOnly

	return mo.Some(backupStorageMountInternal{
		mode:  backupStorageModeArcaneMount,
		mount: *mirroredMount,
	})
}

func (s *Service) resolveBackupStorageMountInternal(ctx context.Context, dockerClient *client.Client, target string, readOnly bool) backupStorageMountInternal {
	if dockerClient != nil {
		inspect, err := libarcane.InspectCurrentArcaneContainer(ctx, dockerClient)
		if err != nil {
			slog.WarnContext(ctx, "volume service: failed to inspect arcane container for backup mount resolution, falling back to named volume", "error", err.Error())
		} else if resolved, ok := resolveBackupStorageMountFromMountsInternal(ctx, inspect.Mounts, target, readOnly).Get(); ok {
			return resolved
		}
	}

	return backupStorageMountInternal{
		mode: backupStorageModeNamedVolumeFallback,
		mount: mount.Mount{
			Type:     mount.TypeVolume,
			Source:   s.deps.BackupVolumeName,
			Target:   target,
			ReadOnly: readOnly,
		},
		requiresEnsure: true,
	}
}

func (s *Service) resolveUsableBackupStorageMountInternal(ctx context.Context, dockerClient *client.Client, target string, readOnly bool) (backupStorageMountInternal, error) {
	backupStorage := s.resolveBackupStorageMountInternal(ctx, dockerClient, target, readOnly)
	if backupStorage.requiresEnsure {
		if err := s.ensureBackupVolumeInternal(ctx); err != nil {
			return backupStorageMountInternal{}, err
		}
	}
	return backupStorage, nil
}

func backupMountWarningForStorageInternal(storage backupStorageMountInternal) string {
	return kit.Ternary(storage.mode == backupStorageModeArcaneMount, "", backupMountMissingWarning)
}

func backupMountWarningFromArcaneMountsInternal(ctx context.Context, mounts []container.MountPoint) string {
	backupStorage, ok := resolveBackupStorageMountFromMountsInternal(ctx, mounts, "/backups", true).Get()
	if ok {
		return backupMountWarningForStorageInternal(backupStorage)
	}

	// Backward compatibility: historically either /backups or /restores mount
	// suppressed the warning. Preserve that user-visible behavior.
	for _, m := range mounts {
		if m.Destination == "/restores" {
			return ""
		}
	}

	return backupMountMissingWarning
}

func (s *Service) backupMountWarningInternal(ctx context.Context) string {
	dockerClient, err := s.deps.Docker.GetClient(ctx)
	if err != nil {
		return ""
	}

	// Cannot determine Arcane mount status (e.g. running outside Docker); suppress warning.
	inspect, err := libarcane.InspectCurrentArcaneContainer(ctx, dockerClient)
	if err != nil {
		return ""
	}

	return backupMountWarningFromArcaneMountsInternal(ctx, inspect.Mounts)
}

// StorageMount resolves the backup repository mount shared with system-backup and workspace operations.
func (s *Service) StorageMount(ctx context.Context, dockerClient *client.Client, target string, readOnly bool) (mount.Mount, error) {
	storage, err := s.resolveUsableBackupStorageMountInternal(ctx, dockerClient, target, readOnly)
	if err != nil {
		return mount.Mount{}, err
	}
	return storage.mount, nil
}

func (s *Service) ensureBackupVolumeInternal(ctx context.Context) error {
	slog.DebugContext(ctx, "volume service: ensure backup volume", "backupVolume", s.deps.BackupVolumeName)
	dockerClient, err := s.deps.Docker.GetClient(ctx)
	if err != nil {
		return err
	}

	_, err = dockerClient.VolumeInspect(ctx, s.deps.BackupVolumeName, client.VolumeInspectOptions{})
	if err != nil {
		_, err = dockerClient.VolumeCreate(ctx, client.VolumeCreateOptions{
			Name: s.deps.BackupVolumeName,
		})
		if err != nil {
			return fmt.Errorf("failed to create backup volume: %w", err)
		}
	}
	return nil
}

func (
	s *Service,
) stopRunningContainersForBackupInternal(
	ctx context.Context,
	dockerClient *client.Client,
	volumeName string,
	user usertypes.Actor,
	refuseArcaneWriters bool,
) (
	[]container.Summary,
	error,
) {
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
		// Arcane's own helper containers mount the volume as well, but they are
		// auto-removed when stopped and recreated on demand. Stopping one here
		// would leave nothing for the restart pass to find, failing an
		// otherwise successful backup or restore.
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
	containersByID := make(map[string]container.Summary, len(eligible))
	for _, candidate := range eligible {
		containersByID[candidate.ID] = candidate
	}
	stopped := make([]container.Summary, 0, len(containerIDs))
	for _, containerID := range containerIDs {
		candidate := containersByID[containerID]
		if stopContainerErr := s.deps.StopContainer(ctx, containerID, user); stopContainerErr != nil {
			stillStopped, restartErr := s.startContainersAfterBackupInternal(context.WithoutCancel(ctx), dockerClient, stopped, user)
			return stillStopped, errors.Join(fmt.Errorf("failed to stop container %s before volume backup: %w", containerID, stopContainerErr), restartErr)
		}
		stopped = append(stopped, candidate)
	}
	return stopped, nil
}

//nolint:gocognit // recovery retries must reconcile IDs, names, and Compose identities in one bounded loop
func (s *Service) startContainersAfterBackupInternal(ctx context.Context, dockerClient *client.Client, stoppedContainers []container.Summary, user usertypes.Actor) ([]container.Summary, error) {
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

func sanitizeBackupPathInternal(input string) (string, error) {
	trimmed := strings.TrimSpace(input)
	if trimmed == "" {
		return "", errors.New("invalid path: empty")
	}
	cleaned := path.Clean(trimmed)
	if cleaned == "." || cleaned == "/" {
		return "", fmt.Errorf("invalid path: %s", input)
	}
	if path.IsAbs(cleaned) {
		cleaned = strings.TrimPrefix(cleaned, "/")
	}
	if cleaned == "" || cleaned == "." || cleaned == "/" || strings.HasPrefix(cleaned, "..") || strings.Contains(cleaned, "/../") {
		return "", fmt.Errorf("invalid path: %s", input)
	}
	return cleaned, nil
}

func sanitizeBackupIDInternal(backupID string) (string, error) {
	cleaned, err := sanitizeBackupPathInternal(backupID)
	if err != nil {
		return "", fmt.Errorf("invalid backup id: %w", err)
	}
	if strings.Contains(cleaned, "/") {
		return "", errors.New("invalid backup id: path separators not allowed")
	}
	return cleaned, nil
}

func backupArchiveFilenameInternal(backupID string) (string, error) {
	sanitizedBackupID, err := sanitizeBackupIDInternal(backupID)
	if err != nil {
		return "", err
	}
	return sanitizedBackupID + ".tar.gz", nil
}

// volumeBackupPasswordInternal returns the recovery key once one is stored, re-keying the given repositories off the legacy derivation first.
func (s *Service) volumeBackupPasswordInternal(ctx context.Context, dockerClient *client.Client, repositories ...backup.Repository) (string, error) {
	if s.deps.RecoveryKeys == nil {
		return kit.SHA256Hex(legacyVolumePasswordSaltInternal + s.deps.EncryptionKey), nil
	}
	key, err := s.deps.RecoveryKeys.Get(ctx)
	if errors.Is(err, backup.ErrRecoveryKeyNotConfigured) {
		return kit.SHA256Hex(legacyVolumePasswordSaltInternal + s.deps.EncryptionKey), nil
	}
	if err != nil {
		return "", err
	}
	for _, repository := range repositories {
		s.rekeyRepositoryInternal(ctx, dockerClient, repository, key)
	}
	return key, nil
}

// rekeyRepositoryInternal re-keys a legacy repository once per process; one that opens with neither password is left for the caller's operation to create or report.
func (s *Service) rekeyRepositoryInternal(ctx context.Context, dockerClient *client.Client, repository backup.Repository, recoveryKey string) {
	if s.deps.Engine == nil {
		return
	}
	if done, _ := s.rekeyed.Load(repository.ID); done == recoveryKey {
		return
	}
	if _, err := s.deps.Engine.ListSnapshots(ctx, dockerClient, repository, recoveryKey); err == nil {
		s.rekeyed.Store(repository.ID, recoveryKey)
		return
	}
	if repository.ID == localVolumeRepositoryID {
		writable, err := s.localRusticRepositoryInternal(ctx, dockerClient, false)
		if err != nil {
			slog.WarnContext(ctx, "could not open the local volume backup repository for re-keying", "error", err.Error())
			return
		}
		repository = writable
	}
	if err := s.deps.Engine.ChangeRepositoryPassword(ctx, dockerClient, repository, kit.SHA256Hex(legacyVolumePasswordSaltInternal+s.deps.EncryptionKey), recoveryKey); err != nil {
		slog.DebugContext(ctx, "volume backup repository was not re-keyed", "repository", repository.ID, "error", err.Error())
		return
	}
	slog.InfoContext(ctx, "Re-keyed volume backup repository to the recovery key", "repository", repository.ID)
	s.rekeyed.Store(repository.ID, recoveryKey)
}

func (s *Service) localRusticRepositoryInternal(ctx context.Context, dockerClient *client.Client, readOnly bool) (backup.Repository, error) {
	storage, err := s.resolveUsableBackupStorageMountInternal(ctx, dockerClient, "/repository", readOnly)
	if err != nil {
		return backup.Repository{}, err
	}
	return backup.Repository{
		ID:          localVolumeRepositoryID,
		Environment: []string{"RUSTIC_REPOSITORY=" + volumeRusticRepositoryPath},
		Mounts:      []mount.Mount{storage.mount},
	}, nil
}

func (s *Service) remoteRusticRepositoryInternal(ctx context.Context, destinationID string) (backup.Repository, error) {
	return s.remoteRusticRepositoryForInstanceInternal(ctx, destinationID, "")
}

// remoteRusticRepositoryForInstanceInternal addresses one instance's root on the destination; empty means this instance.
func (s *Service) remoteRusticRepositoryForInstanceInternal(ctx context.Context, destinationID, instanceID string) (backup.Repository, error) {
	if s.deps.S3Destinations == nil {
		return backup.Repository{}, errors.New("S3 backup service is unavailable")
	}
	if strings.TrimSpace(instanceID) == "" {
		instanceID = strings.TrimSpace(s.deps.Settings.GetSettingsConfig().InstanceID.Value)
	}
	if instanceID == "" {
		return backup.Repository{}, errors.New("arcane instance ID is unavailable")
	}
	configuration, err := s.deps.S3Destinations.Configuration(ctx, destinationID)
	if err != nil {
		return backup.Repository{}, errors.New("the selected S3 backup destination is not configured")
	}
	return backup.Repository{
		ID:          "volumes:s3:" + destinationID + ":" + instanceID,
		Environment: configuration.RusticEnvironment("arcane-volume-backups", instanceID),
	}, nil
}

func (s *Service) rusticRepositoryForBackupInternal(ctx context.Context, dockerClient *client.Client, entry *volume.Backup) (backup.Repository, string, error) {
	if entry.LocalSnapshotID != "" {
		repository, err := s.localRusticRepositoryInternal(ctx, dockerClient, true)
		return repository, entry.LocalSnapshotID, err
	}
	if entry.RemoteSnapshotID != "" {
		repository, err := s.remoteRusticRepositoryForInstanceInternal(ctx, entry.S3DestinationID, entry.RemoteInstanceID)
		return repository, entry.RemoteSnapshotID, err
	}
	return backup.Repository{}, "", errors.New("volume backup has no Rustic snapshot")
}

func (s *Service) createBackupTempContainerWithMountInternal(ctx context.Context, dockerClient *client.Client, helperImage string, backupMount mount.Mount) (string, func(), error) {
	var err error
	if dockerClient == nil {
		dockerClient, err = s.deps.Docker.GetClient(ctx)
		if err != nil {
			return "", nil, err
		}
	}

	if strings.TrimSpace(helperImage) == "" {
		helperImage, err = s.deps.HelperImage(ctx, dockerClient)
		if err != nil {
			return "", nil, err
		}
	}

	config := &container.Config{
		Image:           helperImage,
		Cmd:             []string{"sleep", "infinity"},
		NetworkDisabled: true,
		Labels:          volumehelper.Labels(),
	}

	hostConfig := volumehelper.HostConfig(helperImage, nil, []mount.Mount{backupMount})

	resp, err := dockerClient.ContainerCreate(ctx, client.ContainerCreateOptions{
		Config:     config,
		HostConfig: hostConfig,
	})
	if err != nil {
		return "", nil, fmt.Errorf("failed to create backup temp container: %w", err)
	}

	if _, containerStartErr := dockerClient.ContainerStart(ctx, resp.ID, client.ContainerStartOptions{}); containerStartErr != nil {
		_, _ = dockerClient.ContainerRemove(ctx, resp.ID, volumehelper.RemoveOptions())
		return "", nil, fmt.Errorf("failed to start backup temp container: %w", containerStartErr)
	}

	cleanup := func() {
		_, _ = dockerClient.ContainerRemove(context.WithoutCancel(ctx), resp.ID, volumehelper.RemoveOptions())
	}

	return resp.ID, cleanup, nil
}

func (s *Service) createBackupTempContainerInternal(ctx context.Context, dockerClient *client.Client, target string, readOnly bool) (string, func(), error) {
	slog.DebugContext(ctx, "volume service: create backup temp container", "target", target, "readOnly", readOnly)
	var err error
	if dockerClient == nil {
		dockerClient, err = s.deps.Docker.GetClient(ctx)
		if err != nil {
			return "", nil, err
		}
	}

	backupStorage, err := s.resolveUsableBackupStorageMountInternal(ctx, dockerClient, target, readOnly)
	if err != nil {
		return "", nil, err
	}

	return s.createBackupTempContainerWithMountInternal(ctx, dockerClient, "", backupStorage.mount)
}

func volumeSourceMountInternal(volumeName string) mount.Mount {
	return mount.Mount{Type: mount.TypeVolume, Source: volumeName, Target: "/volume", ReadOnly: true}
}

func backupDestinationAttemptedInternal(previous scheduler.Run, backupID, destination string) bool {
	for _, evidence := range previous.Outcome.Targets {
		if evidence.ID == backupID+":"+destination {
			return true
		}
	}
	return false
}
