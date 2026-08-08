package volume

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	stderrors "errors"
	"fmt"
	"io"
	"log/slog"
	"path"
	"strings"
	"time"

	"emperror.dev/errors"
	"gorm.io/gorm"

	"github.com/getarcaneapp/arcane/backend/v2/internal/backup"
	"github.com/getarcaneapp/arcane/backend/v2/internal/models"
	docker "github.com/getarcaneapp/arcane/backend/v2/pkg/dockerutil"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/libarcane"
	activitylib "github.com/getarcaneapp/arcane/backend/v2/pkg/libarcane/activity"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/libarcane/volumehelper"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/pagination"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/utils/schedule"
	volumetypes "github.com/getarcaneapp/arcane/types/v2/volume"
	"github.com/google/uuid"
	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/api/types/mount"
	"github.com/moby/moby/client"
	"github.com/samber/mo"
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
)

const backupMountMissingWarning = "No volume is mounted at /backups in the Arcane container. Backups will only live inside Docker unless you mount a host path."

const (
	volumeBackupContainerRecoveryTimeout  = 30 * time.Second
	volumeBackupContainerRecoveryInterval = 500 * time.Millisecond
)

type backupStorageMountInternal struct {
	mode           backupStorageMode
	mount          mount.Mount
	requiresEnsure bool
}

func resolveBackupStorageMountFromMountsInternal(mounts []container.MountPoint, target string, readOnly bool) mo.Option[backupStorageMountInternal] {
	mirroredMount := docker.MountForDestination(mounts, "/backups", target)
	if mirroredMount == nil {
		return mo.None[backupStorageMountInternal]()
	}
	// MountForDestination only returns non-nil for bind and named volume mounts.

	if !readOnly && mirroredMount.ReadOnly {
		slog.Warn("volume service: requested writable backup mount but source is read-only; writes may fail")
	}
	mirroredMount.ReadOnly = readOnly

	return mo.Some(backupStorageMountInternal{
		mode:  backupStorageModeArcaneMount,
		mount: *mirroredMount,
	})
}

func (s *VolumeService) resolveBackupStorageMountInternal(ctx context.Context, dockerClient *client.Client, target string, readOnly bool) backupStorageMountInternal {
	if dockerClient != nil {
		inspect, err := libarcane.InspectCurrentArcaneContainer(ctx, dockerClient)
		if err != nil {
			slog.WarnContext(ctx, "volume service: failed to inspect arcane container for backup mount resolution, falling back to named volume", "error", err.Error())
		} else if resolved, ok := resolveBackupStorageMountFromMountsInternal(inspect.Mounts, target, readOnly).Get(); ok {
			return resolved
		}
	}

	return backupStorageMountInternal{
		mode: backupStorageModeNamedVolumeFallback,
		mount: mount.Mount{
			Type:     mount.TypeVolume,
			Source:   s.backupVolumeName,
			Target:   target,
			ReadOnly: readOnly,
		},
		requiresEnsure: true,
	}
}

func (s *VolumeService) resolveUsableBackupStorageMountInternal(ctx context.Context, dockerClient *client.Client, target string, readOnly bool) (backupStorageMountInternal, error) {
	backupStorage := s.resolveBackupStorageMountInternal(ctx, dockerClient, target, readOnly)
	if backupStorage.requiresEnsure {
		if err := s.ensureBackupVolumeInternal(ctx); err != nil {
			return backupStorageMountInternal{}, err
		}
	}
	return backupStorage, nil
}

func backupMountWarningForStorageInternal(storage backupStorageMountInternal) string {
	if storage.mode == backupStorageModeArcaneMount {
		return ""
	}
	return backupMountMissingWarning
}

func backupMountWarningFromArcaneMountsInternal(mounts []container.MountPoint) string {
	backupStorage, ok := resolveBackupStorageMountFromMountsInternal(mounts, "/backups", true).Get()
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

func (s *VolumeService) BackupMountWarning(ctx context.Context) string {
	dockerClient, err := s.dockerService.GetClient(ctx)
	if err != nil {
		return ""
	}

	// Cannot determine Arcane mount status (e.g. running outside Docker); suppress warning.
	inspect, err := libarcane.InspectCurrentArcaneContainer(ctx, dockerClient)
	if err != nil {
		return ""
	}

	return backupMountWarningFromArcaneMountsInternal(inspect.Mounts)
}

// BackupStorageMount resolves the repository mount shared with system-backup operations.
func (s *VolumeService) BackupStorageMount(ctx context.Context, dockerClient *client.Client, target string, readOnly bool) (mount.Mount, error) {
	storage, err := s.resolveUsableBackupStorageMountInternal(ctx, dockerClient, target, readOnly)
	if err != nil {
		return mount.Mount{}, err
	}
	return storage.mount, nil
}

func (s *VolumeService) ensureBackupVolumeInternal(ctx context.Context) error {
	slog.DebugContext(ctx, "volume service: ensure backup volume", "backup_volume", s.backupVolumeName)
	dockerClient, err := s.dockerService.GetClient(ctx)
	if err != nil {
		return err
	}

	_, err = dockerClient.VolumeInspect(ctx, s.backupVolumeName, client.VolumeInspectOptions{})
	if err != nil {
		_, err = dockerClient.VolumeCreate(ctx, client.VolumeCreateOptions{
			Name: s.backupVolumeName,
		})
		if err != nil {
			return errors.WrapIf(err, "failed to create backup volume")
		}
	}
	return nil
}

func (s *VolumeService) stopRunningContainersForBackupInternal(ctx context.Context, dockerClient *client.Client, volumeName string, user models.User) ([]container.Summary, error) {
	if s.containerService == nil {
		return nil, errors.New("container service is unavailable")
	}
	containers, err := dockerClient.ContainerList(ctx, client.ContainerListOptions{})
	if err != nil {
		return nil, fmt.Errorf("failed to list running containers before volume backup: %w", err)
	}

	eligible := make([]container.Summary, 0, len(containers.Items))
	for _, candidate := range containers.Items {
		if strings.EqualFold(candidate.Labels["com.getarcaneapp.arcane"], "true") || strings.EqualFold(candidate.Labels["com.getarcaneapp.arcane.agent"], "true") {
			continue
		}
		eligible = append(eligible, candidate)
	}
	containerIDs := docker.FilterContainersUsingVolume(eligible, volumeName)
	containersByID := make(map[string]container.Summary, len(eligible))
	for _, candidate := range eligible {
		containersByID[candidate.ID] = candidate
	}
	stopped := make([]container.Summary, 0, len(containerIDs))
	for _, containerID := range containerIDs {
		candidate := containersByID[containerID]
		if err := s.containerService.StopContainer(ctx, containerID, user); err != nil {
			stillStopped, restartErr := s.startContainersAfterBackupInternal(context.WithoutCancel(ctx), dockerClient, stopped, user)
			return stillStopped, errors.Combine(fmt.Errorf("failed to stop container %s before volume backup: %w", containerID, err), restartErr)
		}
		stopped = append(stopped, candidate)
	}
	return stopped, nil
}

//nolint:gocognit // recovery retries must reconcile IDs, names, and Compose identities in one bounded loop
func (s *VolumeService) startContainersAfterBackupInternal(ctx context.Context, dockerClient *client.Client, stoppedContainers []container.Summary, user models.User) ([]container.Summary, error) {
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
					projectName := docker.ComposeProjectLabel(stopped.Labels)
					serviceName := docker.ComposeServiceLabel(stopped.Labels)
					containerNumber := strings.TrimSpace(stopped.Labels["com.docker.compose.container-number"])
					if projectName != "" && serviceName != "" {
						for _, candidate := range currentContainers.Items {
							if docker.ComposeProjectLabel(candidate.Labels) != projectName || docker.ComposeServiceLabel(candidate.Labels) != serviceName {
								continue
							}
							if containerNumber != "" && strings.TrimSpace(candidate.Labels["com.docker.compose.container-number"]) != containerNumber {
								continue
							}
							current = candidate
							found = true
							break
						}
					}
				}
				if !found {
					nextRemaining = append(nextRemaining, stopped)
					continue
				}
				if current.State == container.StateRunning || current.State == container.StateRestarting {
					if current.ID != stopped.ID {
						slog.InfoContext(ctx, "volume service: container was replaced during backup and is already running", "previous_container", stopped.ID, "current_container", current.ID)
					}
					continue
				}
				if startErr := s.containerService.StartContainer(recoveryCtx, current.ID, user); startErr != nil {
					lastErrors[stopped.ID] = startErr
					nextRemaining = append(nextRemaining, stopped)
					continue
				}
				if current.ID != stopped.ID {
					slog.InfoContext(ctx, "volume service: restarted replacement container after backup", "previous_container", stopped.ID, "current_container", current.ID)
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
					restartErr = errors.Combine(restartErr, fmt.Errorf("failed to restart container %s after volume backup: %w", stopped.ID, lastErr))
				} else {
					restartErr = errors.Combine(restartErr, fmt.Errorf("failed to restart container %s after volume backup: replacement did not appear within %s", stopped.ID, volumeBackupContainerRecoveryTimeout))
				}
			}
			return remaining, restartErr
		case <-timer.C:
		}
	}
	return nil, nil
}

func (s *VolumeService) ListBackupsPaginated(ctx context.Context, volumeName string, params pagination.QueryParams) ([]models.VolumeBackup, pagination.Response, error) {
	slog.DebugContext(ctx, "volume service: list backups paginated", "volume", volumeName, "search", params.Search, "sort", params.Sort, "order", params.Order, "start", params.Start, "limit", params.Limit)
	var backups []models.VolumeBackup
	query := s.db.WithContext(ctx).Model(&models.VolumeBackup{}).Where("volume_name = ?", volumeName)

	if params.Search != "" {
		pattern := "%" + params.Search + "%"
		query = query.Where("id LIKE ? OR status LIKE ? OR trigger LIKE ? OR destination LIKE ? OR COALESCE(local_snapshot_id, '') LIKE ? OR COALESCE(remote_snapshot_id, '') LIKE ? OR COALESCE(error, '') LIKE ?", pattern, pattern, pattern, pattern, pattern, pattern, pattern)
	}

	var totalItems int64
	if err := query.Count(&totalItems).Error; err != nil {
		return nil, pagination.Response{}, err
	}

	sortCol := "created_at"
	sortOrder := "DESC"
	if params.Sort != "" {
		switch params.Sort {
		case "createdAt", "created_at":
			sortCol = "created_at"
		case "id":
			sortCol = "id"
		case "size":
			sortCol = "size"
		case "status":
			sortCol = "status"
		case "trigger":
			sortCol = "trigger"
		case "destination":
			sortCol = "destination"
		case "remoteSnapshotId", "remote_snapshot_id":
			sortCol = "remote_snapshot_id"
		default:
			sortCol = "created_at"
		}

		if params.Order == pagination.SortDesc {
			sortOrder = "DESC"
		} else {
			sortOrder = "ASC"
		}
	}
	query = query.Order(fmt.Sprintf("%s %s", sortCol, sortOrder))

	if params.Limit > 0 {
		query = query.Offset(params.Start).Limit(params.Limit)
	}

	if err := query.Find(&backups).Error; err != nil {
		return nil, pagination.Response{}, err
	}
	hasS3Destination := false
	for i := range backups {
		if strings.TrimSpace(backups[i].S3DestinationID) != "" {
			hasS3Destination = true
			break
		}
	}
	if s.s3Destinations != nil && hasS3Destination {
		destinations, destinationErr := s.s3Destinations.ListAllS3Destinations(ctx)
		if destinationErr != nil {
			return nil, pagination.Response{}, fmt.Errorf("failed to resolve volume backup S3 destinations: %w", destinationErr)
		}
		destinationNames := make(map[string]string, len(destinations))
		for _, destination := range destinations {
			destinationNames[destination.ID] = destination.Name
		}
		for i := range backups {
			backups[i].S3DestinationName = destinationNames[backups[i].S3DestinationID]
		}
	}

	paginationResp := s.buildPaginationResponseFromCountsInternal(totalItems, totalItems, params)
	return backups, paginationResp, nil
}

func (s *VolumeService) buildPaginationResponseFromCountsInternal(totalCount int64, totalAvailable int64, params pagination.QueryParams) pagination.Response {
	slog.Debug("volume service: build pagination response", "total_count", totalCount, "total_available", totalAvailable, "start", params.Start, "limit", params.Limit)
	totalPages := int64(0)
	if params.Limit > 0 {
		totalPages = (totalCount + int64(params.Limit) - 1) / int64(params.Limit)
	}

	page := 1
	if params.Limit > 0 {
		page = (params.Start / params.Limit) + 1
	}

	return pagination.Response{
		TotalPages:      totalPages,
		TotalItems:      totalCount,
		CurrentPage:     page,
		ItemsPerPage:    params.Limit,
		GrandTotalItems: totalAvailable,
	}
}

func (s *VolumeService) ListBackups(ctx context.Context, volumeName string) ([]models.VolumeBackup, error) {
	slog.DebugContext(ctx, "volume service: list backups", "volume", volumeName)
	var backups []models.VolumeBackup
	err := s.db.WithContext(ctx).Where("volume_name = ?", volumeName).Order("created_at DESC").Find(&backups).Error
	return backups, err
}

func (s *VolumeService) sanitizeBackupPathInternal(input string) (string, error) {
	trimmed := strings.TrimSpace(input)
	if trimmed == "" {
		return "", errors.New("invalid path: empty")
	}
	cleaned := path.Clean(trimmed)
	if cleaned == "." || cleaned == "/" {
		return "", errors.Errorf("invalid path: %s", input)
	}
	if path.IsAbs(cleaned) {
		cleaned = strings.TrimPrefix(cleaned, "/")
	}
	if cleaned == "" || cleaned == "." || cleaned == "/" || strings.HasPrefix(cleaned, "..") || strings.Contains(cleaned, "/../") {
		return "", errors.Errorf("invalid path: %s", input)
	}
	return cleaned, nil
}

func (s *VolumeService) sanitizeBackupIDInternal(backupID string) (string, error) {
	cleaned, err := s.sanitizeBackupPathInternal(backupID)
	if err != nil {
		return "", errors.WrapIf(err, "invalid backup id")
	}
	if strings.Contains(cleaned, "/") {
		return "", errors.New("invalid backup id: path separators not allowed")
	}
	return cleaned, nil
}

func (s *VolumeService) backupArchiveFilenameInternal(backupID string) (string, error) {
	sanitizedBackupID, err := s.sanitizeBackupIDInternal(backupID)
	if err != nil {
		return "", err
	}
	return sanitizedBackupID + ".tar.gz", nil
}

const (
	volumeRusticRepositoryPath = "/repository/volumes"
	localVolumeRepositoryID    = "volumes:local"
)

func (s *VolumeService) rusticPasswordInternal() string {
	sum := sha256.Sum256([]byte("arcane-volume-backups:" + s.encryptionKey))
	return hex.EncodeToString(sum[:])
}

func (s *VolumeService) localRusticRepositoryInternal(ctx context.Context, dockerClient *client.Client, readOnly bool) (backup.Repository, error) {
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

func (s *VolumeService) remoteRusticRepositoryInternal(ctx context.Context, destinationID string) (backup.Repository, error) {
	if s.s3Destinations == nil {
		return backup.Repository{}, errors.New("S3 backup service is unavailable")
	}
	configuration, err := s.s3Destinations.Configuration(ctx, destinationID)
	if err != nil {
		return backup.Repository{}, errors.New("the selected S3 backup destination is not configured")
	}
	instanceID := strings.TrimSpace(s.settingsService.GetSettingsConfig().InstanceID.Value)
	if instanceID == "" {
		return backup.Repository{}, errors.New("arcane instance ID is unavailable")
	}
	return backup.Repository{
		ID:          "volumes:s3:" + destinationID,
		Environment: configuration.RusticEnvironment("arcane-volume-backups", instanceID),
	}, nil
}

func (s *VolumeService) rusticRepositoryForBackupInternal(ctx context.Context, dockerClient *client.Client, entry *models.VolumeBackup) (backup.Repository, string, error) {
	if entry.LocalSnapshotID != "" {
		repository, err := s.localRusticRepositoryInternal(ctx, dockerClient, true)
		return repository, entry.LocalSnapshotID, err
	}
	if entry.RemoteSnapshotID != "" {
		repository, err := s.remoteRusticRepositoryInternal(ctx, entry.S3DestinationID)
		return repository, entry.RemoteSnapshotID, err
	}
	return backup.Repository{}, "", errors.New("volume backup has no Rustic snapshot")
}

func volumeSourceMountInternal(volumeName string) mount.Mount {
	return mount.Mount{Type: mount.TypeVolume, Source: volumeName, Target: "/volume", ReadOnly: true}
}

var ErrVolumeBackupAlreadyRunning = errors.New("a backup is already running for this volume")

// backupPlanInternal is the resolved destination selection for one backup run.
type backupPlanInternal struct {
	policy          *models.VolumeBackupPolicy
	localEnabled    bool
	s3Enabled       bool
	s3DestinationID string
	destination     volumetypes.BackupDestination
}

func (s *VolumeService) resolveBackupPlanInternal(ctx context.Context, volumeName string, trigger models.VolumeBackupTrigger, request volumetypes.CreateBackupRequest) (backupPlanInternal, error) {
	destination := request.Destination
	if destination != "" && destination != volumetypes.BackupDestinationLocal && destination != volumetypes.BackupDestinationS3 && destination != volumetypes.BackupDestinationLocalS3 {
		return backupPlanInternal{}, errors.New("invalid volume backup destination")
	}
	var policy *models.VolumeBackupPolicy
	if trigger != models.VolumeBackupTriggerSafety {
		var err error
		policy, err = s.loadVolumeBackupPolicyInternal(ctx, volumeName, request.PolicyID)
		if err != nil {
			return backupPlanInternal{}, err
		}
		if request.PolicyID != "" && policy == nil {
			return backupPlanInternal{}, errors.New("volume backup policy not found")
		}
	}
	localEnabled, s3Enabled, s3DestinationID := true, false, strings.TrimSpace(request.S3DestinationID)
	if policy != nil {
		localEnabled, s3Enabled, s3DestinationID = policy.LocalEnabled, policy.S3Enabled, policy.S3DestinationID
	}
	if trigger == models.VolumeBackupTriggerSafety {
		localEnabled, s3Enabled = true, false
	} else if destination != "" {
		localEnabled = destination != volumetypes.BackupDestinationS3
		s3Enabled = destination != volumetypes.BackupDestinationLocal
	}
	if !localEnabled && !s3Enabled {
		return backupPlanInternal{}, errors.New("select at least one volume backup destination")
	}
	if s3Enabled && strings.TrimSpace(s3DestinationID) == "" {
		return backupPlanInternal{}, errors.New("select an S3 destination for the volume backup")
	}
	if s3Enabled {
		if s.s3Destinations == nil {
			return backupPlanInternal{}, errors.New("S3 backup destinations are unavailable")
		}
		if _, err := s.s3Destinations.Configuration(ctx, s3DestinationID); err != nil {
			return backupPlanInternal{}, errors.New("select a valid S3 destination for the volume backup")
		}
	} else {
		s3DestinationID = ""
	}
	switch {
	case localEnabled && s3Enabled:
		destination = volumetypes.BackupDestinationLocalS3
	case s3Enabled:
		destination = volumetypes.BackupDestinationS3
	default:
		destination = volumetypes.BackupDestinationLocal
	}
	return backupPlanInternal{policy: policy, localEnabled: localEnabled, s3Enabled: s3Enabled, s3DestinationID: s3DestinationID, destination: destination}, nil
}

//nolint:gocognit // backup orchestration keeps cleanup and persistence transitions in one transaction-like flow
func (s *VolumeService) CreateBackup(ctx context.Context, volumeName string, user models.User, trigger models.VolumeBackupTrigger, request volumetypes.CreateBackupRequest) (_ *models.VolumeBackup, err error) {
	if trigger == "" {
		trigger = models.VolumeBackupTriggerManual
	}
	plan, err := s.resolveBackupPlanInternal(ctx, volumeName, trigger, request)
	if err != nil {
		return nil, err
	}
	lease, admitted, leaseErr := s.engine.TryAcquireRun(ctx, backup.VolumeAdmissionScope, volumeName)
	if leaseErr != nil {
		return nil, leaseErr
	}
	if !admitted {
		return nil, ErrVolumeBackupAlreadyRunning
	}
	defer lease.Release()
	entry := &models.VolumeBackup{
		VolumeName: volumeName, CreatedAt: time.Now(), Status: models.VolumeBackupStatusRunning,
		Trigger: trigger, Destination: plan.destination, Format: models.VolumeBackupFormatRustic,
		S3DestinationID: plan.s3DestinationID, PolicyID: request.PolicyID,
	}
	entry.ID = fmt.Sprintf("%s-%d-%s", volumeName, time.Now().UnixNano(), uuid.NewString()[:8])
	if err := s.db.WithContext(ctx).Create(entry).Error; err != nil {
		return nil, err
	}
	defer func() {
		if err != nil {
			entry.Status, entry.Error = models.VolumeBackupStatusFailed, err.Error()
		} else {
			entry.Status, entry.Error = models.VolumeBackupStatusSucceeded, ""
		}
		if saveErr := s.db.WithContext(context.WithoutCancel(ctx)).Save(entry).Error; saveErr != nil {
			err = stderrors.Join(err, fmt.Errorf("failed to save volume backup result: %w", saveErr))
		}
	}()
	dockerClient, err := s.dockerService.GetClient(ctx)
	if err != nil {
		return entry, err
	}
	var stopped []container.Summary
	containersStopped := false
	if plan.policy != nil && plan.policy.StopContainers {
		stopped, err = s.stopRunningContainersForBackupInternal(ctx, dockerClient, volumeName, user)
		containersStopped = len(stopped) > 0
		defer func() {
			if containersStopped {
				_, restartErr := s.startContainersAfterBackupInternal(context.WithoutCancel(ctx), dockerClient, stopped, user)
				err = stderrors.Join(err, restartErr)
			}
		}()
		if err != nil {
			return entry, err
		}
	}
	// The live volume is read once. A local+S3 backup replicates the local
	// snapshot to S3 instead of scanning the source a second time.
	var localSnapshot backup.Snapshot
	if plan.localEnabled {
		repository, repoErr := s.localRusticRepositoryInternal(ctx, dockerClient, false)
		if repoErr != nil {
			return entry, repoErr
		}
		localSnapshot, err = s.engine.CreateSnapshot(ctx, dockerClient, repository, s.rusticPasswordInternal(), volumeName, volumeSourceMountInternal(volumeName))
		if err != nil {
			return entry, fmt.Errorf("failed to create local Rustic snapshot: %w", err)
		}
		entry.LocalSnapshotID = localSnapshot.ID
		entry.Size = localSnapshot.Size
	}
	if plan.s3Enabled {
		remoteRepository, repoErr := s.remoteRusticRepositoryInternal(ctx, plan.s3DestinationID)
		if repoErr != nil {
			return entry, repoErr
		}
		var remoteSnapshot backup.Snapshot
		if plan.localEnabled {
			localRepository, localErr := s.localRusticRepositoryInternal(ctx, dockerClient, true)
			if localErr != nil {
				return entry, localErr
			}
			remoteSnapshot, err = s.engine.Replicate(ctx, dockerClient, localRepository, localSnapshot.ID, remoteRepository, s.rusticPasswordInternal(), volumeName)
		} else {
			remoteSnapshot, err = s.engine.CreateSnapshot(ctx, dockerClient, remoteRepository, s.rusticPasswordInternal(), volumeName, volumeSourceMountInternal(volumeName))
		}
		if err != nil {
			return entry, fmt.Errorf("failed to create S3 Rustic snapshot: %w", err)
		}
		entry.RemoteSnapshotID = remoteSnapshot.ID
		if entry.Size == 0 {
			entry.Size = remoteSnapshot.Size
		}
	}
	if containersStopped {
		stopped, err = s.startContainersAfterBackupInternal(context.WithoutCancel(ctx), dockerClient, stopped, user)
		containersStopped = len(stopped) > 0
		if err != nil {
			return entry, err
		}
	}
	if trigger != models.VolumeBackupTriggerSafety && plan.policy != nil && plan.policy.RetentionCount > 0 {
		if err := s.applyVolumeBackupRetentionInternal(ctx, plan.policy.ID, plan.policy.RetentionCount); err != nil {
			return entry, fmt.Errorf("failed to apply volume backup retention: %w", err)
		}
	}
	metadata := models.JSON{"action": "backup_create", "backup_id": entry.ID, "size": entry.Size, "destination": entry.Destination}
	if logErr := s.eventService.LogVolumeEvent(ctx, models.EventTypeVolumeBackupCreate, volumeName, volumeName, user.ID, user.Username, "0", metadata); logErr != nil {
		slog.WarnContext(ctx, "could not log volume backup create event", "volume", volumeName, "error", logErr)
	}
	return entry, nil
}

func (s *VolumeService) UploadBackup(ctx context.Context, backupID, s3DestinationID string) (*models.VolumeBackup, error) {
	var entry models.VolumeBackup
	if err := s.db.WithContext(ctx).Where("id = ?", backupID).First(&entry).Error; err != nil {
		return nil, err
	}
	if entry.Status != models.VolumeBackupStatusSucceeded || entry.LocalSnapshotID == "" {
		return nil, errors.New("only successful local volume backups can be uploaded")
	}
	if entry.RemoteSnapshotID != "" {
		return nil, errors.New("volume backup has already been uploaded")
	}
	if strings.TrimSpace(s3DestinationID) == "" {
		return nil, errors.New("select an S3 destination for the upload")
	}
	if s.s3Destinations == nil {
		return nil, errors.New("S3 backup destinations are unavailable")
	}
	if _, err := s.s3Destinations.Configuration(ctx, s3DestinationID); err != nil {
		return nil, errors.New("select a valid S3 destination for the upload")
	}
	dockerClient, err := s.dockerService.GetClient(ctx)
	if err != nil {
		return nil, err
	}
	localRepository, err := s.localRusticRepositoryInternal(ctx, dockerClient, true)
	if err != nil {
		return nil, err
	}
	remoteRepository, err := s.remoteRusticRepositoryInternal(ctx, s3DestinationID)
	if err != nil {
		return nil, err
	}
	snapshot, err := s.engine.Replicate(ctx, dockerClient, localRepository, entry.LocalSnapshotID, remoteRepository, s.rusticPasswordInternal(), entry.VolumeName)
	if err != nil {
		return nil, fmt.Errorf("failed to upload Rustic snapshot to S3: %w", err)
	}
	entry.RemoteSnapshotID = snapshot.ID
	entry.S3DestinationID = s3DestinationID
	entry.Destination = volumetypes.BackupDestinationLocalS3
	if err := s.db.WithContext(ctx).Save(&entry).Error; err != nil {
		return nil, fmt.Errorf("failed to save uploaded volume backup: %w", err)
	}
	return &entry, nil
}

func (s *VolumeService) DeleteBackup(ctx context.Context, backupID string, user *models.User) error {
	var entry models.VolumeBackup
	if err := s.db.WithContext(ctx).Where("id = ?", backupID).First(&entry).Error; err != nil {
		return err
	}
	if entry.Format == models.VolumeBackupFormatArchive {
		return s.deleteArchiveBackupInternal(ctx, &entry, user)
	}
	dockerClient, err := s.dockerService.GetClient(ctx)
	if err != nil {
		return err
	}
	var deleteErr error
	if entry.LocalSnapshotID != "" {
		repository, repoErr := s.localRusticRepositoryInternal(ctx, dockerClient, false)
		if repoErr == nil {
			repoErr = s.engine.ForgetSnapshot(ctx, dockerClient, repository, s.rusticPasswordInternal(), entry.LocalSnapshotID)
		}
		if repoErr != nil {
			deleteErr = stderrors.Join(deleteErr, fmt.Errorf("failed to delete local Rustic snapshot: %w", repoErr))
		} else {
			entry.LocalSnapshotID = ""
		}
	}
	if entry.RemoteSnapshotID != "" {
		repository, repoErr := s.remoteRusticRepositoryInternal(ctx, entry.S3DestinationID)
		if repoErr == nil {
			repoErr = s.engine.ForgetSnapshot(ctx, dockerClient, repository, s.rusticPasswordInternal(), entry.RemoteSnapshotID)
		}
		if repoErr != nil {
			deleteErr = stderrors.Join(deleteErr, fmt.Errorf("failed to delete S3 Rustic snapshot: %w", repoErr))
		} else {
			entry.RemoteSnapshotID = ""
			entry.S3DestinationID = ""
		}
	}
	if deleteErr != nil {
		switch {
		case entry.LocalSnapshotID != "" && entry.RemoteSnapshotID != "":
			entry.Destination = volumetypes.BackupDestinationLocalS3
		case entry.LocalSnapshotID != "":
			entry.Destination = volumetypes.BackupDestinationLocal
		case entry.RemoteSnapshotID != "":
			entry.Destination = volumetypes.BackupDestinationS3
		}
		entry.Error = deleteErr.Error()
		if saveErr := s.db.WithContext(ctx).Save(&entry).Error; saveErr != nil {
			deleteErr = stderrors.Join(deleteErr, saveErr)
		}
		return deleteErr
	}
	if err := s.db.WithContext(ctx).Delete(&entry).Error; err != nil {
		return fmt.Errorf("failed to delete volume backup record: %w", err)
	}
	s.logBackupDeleteEventInternal(ctx, entry.VolumeName, backupID, user)
	return nil
}

func (s *VolumeService) logBackupDeleteEventInternal(ctx context.Context, volumeName, backupID string, user *models.User) {
	actingUser := user
	if actingUser == nil {
		actingUser = &models.SystemUser
	}
	if logErr := s.eventService.LogVolumeEvent(ctx, models.EventTypeVolumeBackupDelete, volumeName, volumeName, actingUser.ID, actingUser.Username, "0", models.JSON{"action": "backup_delete", "backup_id": backupID}); logErr != nil {
		slog.WarnContext(ctx, "could not log volume backup delete event", "volume", volumeName, "error", logErr)
	}
}

func (s *VolumeService) RestoreBackup(ctx context.Context, volumeName, backupID string, user models.User) (err error) {
	var entry models.VolumeBackup
	if err := s.db.WithContext(ctx).Where("id = ?", backupID).First(&entry).Error; err != nil {
		return err
	}
	if entry.VolumeName != volumeName {
		return errors.New("backup does not belong to volume")
	}
	dockerClient, err := s.dockerService.GetClient(ctx)
	if err != nil {
		return err
	}
	var repository backup.Repository
	var snapshotID string
	if entry.Format != models.VolumeBackupFormatArchive {
		repository, snapshotID, err = s.rusticRepositoryForBackupInternal(ctx, dockerClient, &entry)
		if err != nil {
			return err
		}
	}
	stopped, err := s.stopRunningContainersForBackupInternal(ctx, dockerClient, volumeName, user)
	containersStopped := len(stopped) > 0
	defer func() {
		if containersStopped {
			_, restartErr := s.startContainersAfterBackupInternal(context.WithoutCancel(ctx), dockerClient, stopped, user)
			err = stderrors.Join(err, restartErr)
		}
	}()
	if err != nil {
		return err
	}
	preBackup, err := s.CreateBackup(ctx, volumeName, user, models.VolumeBackupTriggerSafety, volumetypes.CreateBackupRequest{Destination: volumetypes.BackupDestinationLocal})
	if err != nil {
		return fmt.Errorf("failed to create pre-restore backup: %w", err)
	}
	if entry.Format == models.VolumeBackupFormatArchive {
		if err := s.restoreArchiveBackupInternal(ctx, dockerClient, volumeName, backupID); err != nil {
			return err
		}
	} else if err := s.engine.RestoreSnapshot(ctx, dockerClient, repository, s.rusticPasswordInternal(), snapshotID, mount.Mount{Type: mount.TypeVolume, Source: volumeName, Target: "/volume"}, backup.RestoreOptions{DeleteExtra: true}); err != nil {
		return fmt.Errorf("failed to restore Rustic snapshot: %w", err)
	}
	if containersStopped {
		stopped, err = s.startContainersAfterBackupInternal(context.WithoutCancel(ctx), dockerClient, stopped, user)
		containersStopped = len(stopped) > 0
		if err != nil {
			return err
		}
	}
	metadata := models.JSON{"action": "backup_restore", "backup_id": backupID, "pre_restore_backupId": preBackup.ID}
	if logErr := s.eventService.LogVolumeEvent(ctx, models.EventTypeVolumeBackupRestore, volumeName, volumeName, user.ID, user.Username, "0", metadata); logErr != nil {
		slog.WarnContext(ctx, "could not log volume backup restore event", "volume", volumeName, "error", logErr)
	}
	return nil
}

func (s *VolumeService) ListBackupFiles(ctx context.Context, backupID string) ([]string, error) {
	var entry models.VolumeBackup
	if err := s.db.WithContext(ctx).Where("id = ?", backupID).First(&entry).Error; err != nil {
		return nil, err
	}
	if entry.Format == models.VolumeBackupFormatArchive {
		return s.listArchiveBackupFilesInternal(ctx, backupID)
	}
	dockerClient, err := s.dockerService.GetClient(ctx)
	if err != nil {
		return nil, err
	}
	repository, snapshotID, err := s.rusticRepositoryForBackupInternal(ctx, dockerClient, &entry)
	if err != nil {
		return nil, err
	}
	return s.engine.ListSnapshotFiles(ctx, dockerClient, repository, s.rusticPasswordInternal(), snapshotID)
}

func (s *VolumeService) BackupHasPath(ctx context.Context, backupID, filePath string) (bool, error) {
	cleaned, err := s.sanitizeBackupPathInternal(filePath)
	if err != nil {
		return false, err
	}
	files, err := s.ListBackupFiles(ctx, backupID)
	if err != nil {
		return false, err
	}
	for _, candidate := range files {
		if strings.TrimPrefix(candidate, "/") == cleaned {
			return true, nil
		}
	}
	return false, nil
}

func (s *VolumeService) RestoreBackupFiles(ctx context.Context, volumeName, backupID string, paths []string, user models.User) (err error) {
	if len(paths) == 0 {
		return errors.New("no paths provided")
	}
	var entry models.VolumeBackup
	if err := s.db.WithContext(ctx).Where("id = ?", backupID).First(&entry).Error; err != nil {
		return err
	}
	if entry.VolumeName != volumeName {
		return errors.New("backup does not belong to volume")
	}
	cleanedPaths := make([]string, 0, len(paths))
	for _, requestedPath := range paths {
		cleaned, cleanErr := s.sanitizeBackupPathInternal(requestedPath)
		if cleanErr != nil {
			return cleanErr
		}
		cleanedPaths = append(cleanedPaths, cleaned)
	}
	dockerClient, err := s.dockerService.GetClient(ctx)
	if err != nil {
		return err
	}
	var repository backup.Repository
	var snapshotID string
	if entry.Format != models.VolumeBackupFormatArchive {
		repository, snapshotID, err = s.rusticRepositoryForBackupInternal(ctx, dockerClient, &entry)
		if err != nil {
			return err
		}
	}
	stopped, err := s.stopRunningContainersForBackupInternal(ctx, dockerClient, volumeName, user)
	containersStopped := len(stopped) > 0
	defer func() {
		if containersStopped {
			_, restartErr := s.startContainersAfterBackupInternal(context.WithoutCancel(ctx), dockerClient, stopped, user)
			err = stderrors.Join(err, restartErr)
		}
	}()
	if err != nil {
		return err
	}
	preBackup, err := s.CreateBackup(ctx, volumeName, user, models.VolumeBackupTriggerSafety, volumetypes.CreateBackupRequest{Destination: volumetypes.BackupDestinationLocal})
	if err != nil {
		return fmt.Errorf("failed to create pre-restore backup: %w", err)
	}
	if entry.Format == models.VolumeBackupFormatArchive {
		if err := s.restoreArchiveBackupFilesInternal(ctx, dockerClient, volumeName, backupID, cleanedPaths); err != nil {
			return err
		}
	} else {
		for _, cleaned := range cleanedPaths {
			if err := s.engine.RestoreSnapshot(ctx, dockerClient, repository, s.rusticPasswordInternal(), snapshotID, mount.Mount{Type: mount.TypeVolume, Source: volumeName, Target: "/volume"}, backup.RestoreOptions{SourcePath: cleaned, DestinationPath: path.Join("/volume", cleaned)}); err != nil {
				return fmt.Errorf("failed to restore %s from Rustic snapshot: %w", cleaned, err)
			}
		}
	}
	if containersStopped {
		stopped, err = s.startContainersAfterBackupInternal(context.WithoutCancel(ctx), dockerClient, stopped, user)
		containersStopped = len(stopped) > 0
		if err != nil {
			return err
		}
	}
	metadata := models.JSON{"action": "backup_restore_files", "backup_id": backupID, "pre_restore_backupId": preBackup.ID, "paths_count": len(cleanedPaths)}
	if logErr := s.eventService.LogVolumeEvent(ctx, models.EventTypeVolumeBackupRestoreFiles, volumeName, volumeName, user.ID, user.Username, "0", metadata); logErr != nil {
		slog.WarnContext(ctx, "could not log volume backup restore files event", "volume", volumeName, "error", logErr)
	}
	return nil
}

// DownloadBackup streams a backup as a tar.gz archive. Legacy archive rows
// stream their stored file; local Rustic rows are restored into a scratch
// volume and packaged on the fly.
func (s *VolumeService) DownloadBackup(ctx context.Context, backupID string, user *models.User) (io.ReadCloser, int64, error) {
	var entry models.VolumeBackup
	if err := s.db.WithContext(ctx).Where("id = ?", backupID).First(&entry).Error; err != nil {
		return nil, 0, err
	}
	var reader io.ReadCloser
	var size int64
	var err error
	if entry.Format == models.VolumeBackupFormatArchive {
		reader, size, err = s.downloadArchiveBackupInternal(ctx, backupID)
	} else {
		reader, size, err = s.downloadRusticBackupInternal(ctx, &entry)
	}
	if err != nil {
		return nil, 0, err
	}
	actingUser := user
	if actingUser == nil {
		actingUser = &models.SystemUser
	}
	metadata := models.JSON{"action": "backup_download", "backup_id": backupID, "size": size}
	if logErr := s.eventService.LogVolumeEvent(ctx, models.EventTypeVolumeBackupDownload, entry.VolumeName, entry.VolumeName, actingUser.ID, actingUser.Username, "0", metadata); logErr != nil {
		slog.WarnContext(ctx, "could not log volume backup download event", "volume", entry.VolumeName, "error", logErr)
	}
	return reader, size, nil
}

func (s *VolumeService) downloadRusticBackupInternal(ctx context.Context, entry *models.VolumeBackup) (io.ReadCloser, int64, error) {
	if entry.LocalSnapshotID == "" {
		return nil, 0, errors.New("only local volume backups can be downloaded")
	}
	dockerClient, err := s.dockerService.GetClient(ctx)
	if err != nil {
		return nil, 0, err
	}
	repository, err := s.localRusticRepositoryInternal(ctx, dockerClient, true)
	if err != nil {
		return nil, 0, err
	}
	scratchVolume := "arcane-rustic-download-" + uuid.NewString()
	if _, err := dockerClient.VolumeCreate(ctx, client.VolumeCreateOptions{Name: scratchVolume, Labels: volumehelper.Labels()}); err != nil {
		return nil, 0, fmt.Errorf("failed to create download scratch volume: %w", err)
	}
	removeScratch := func() {
		_, _ = dockerClient.VolumeRemove(context.WithoutCancel(ctx), scratchVolume, client.VolumeRemoveOptions{Force: true})
	}
	if err := s.engine.RestoreSnapshot(ctx, dockerClient, repository, s.rusticPasswordInternal(), entry.LocalSnapshotID, mount.Mount{Type: mount.TypeVolume, Source: scratchVolume, Target: "/volume"}, backup.RestoreOptions{DeleteExtra: true}); err != nil {
		removeScratch()
		return nil, 0, fmt.Errorf("failed to restore Rustic snapshot for download: %w", err)
	}
	helperImage, err := getVolumeHelperImageInternal(ctx, s.dockerService, s.imageService, dockerClient)
	if err != nil {
		removeScratch()
		return nil, 0, err
	}
	archiveMount := mount.Mount{Type: mount.TypeVolume, Source: scratchVolume, Target: "/volume", ReadOnly: false}
	containerID, removeContainer, err := s.createBackupTempContainerWithMountInternal(ctx, dockerClient, helperImage, archiveMount)
	if err != nil {
		removeScratch()
		return nil, 0, err
	}
	cleanup := func() {
		removeContainer()
		removeScratch()
	}
	archivePath := "/tmp/" + entry.ID + ".tar.gz"
	if _, _, err := s.execInContainerInternal(ctx, containerID, []string{"tar", "-czf", archivePath, "-C", "/volume", "."}); err != nil {
		cleanup()
		return nil, 0, fmt.Errorf("failed to package Rustic snapshot for download: %w", err)
	}
	return volumehelper.DownloadFileFromContainer(ctx, dockerClient, containerID, archivePath, cleanup)
}

func (s *VolumeService) createBackupTempContainerWithMountInternal(ctx context.Context, dockerClient *client.Client, helperImage string, backupMount mount.Mount) (string, func(), error) {
	var err error
	if dockerClient == nil {
		dockerClient, err = s.dockerService.GetClient(ctx)
		if err != nil {
			return "", nil, err
		}
	}

	if strings.TrimSpace(helperImage) == "" {
		helperImage, err = getVolumeHelperImageInternal(ctx, s.dockerService, s.imageService, dockerClient)
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
		return "", nil, errors.WrapIf(err, "failed to create backup temp container")
	}

	if _, err := dockerClient.ContainerStart(ctx, resp.ID, client.ContainerStartOptions{}); err != nil {
		_, _ = dockerClient.ContainerRemove(ctx, resp.ID, volumehelper.RemoveOptions())
		return "", nil, errors.WrapIf(err, "failed to start backup temp container")
	}

	cleanup := func() {
		_, _ = dockerClient.ContainerRemove(context.WithoutCancel(ctx), resp.ID, volumehelper.RemoveOptions())
	}

	return resp.ID, cleanup, nil
}

func (s *VolumeService) createBackupTempContainerInternal(ctx context.Context, dockerClient *client.Client, target string, readOnly bool) (string, func(), error) {
	slog.DebugContext(ctx, "volume service: create backup temp container", "target", target, "read_only", readOnly)
	var err error
	if dockerClient == nil {
		dockerClient, err = s.dockerService.GetClient(ctx)
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

func (s *VolumeService) downloadArchiveBackupInternal(ctx context.Context, backupID string) (io.ReadCloser, int64, error) {
	filename, err := s.backupArchiveFilenameInternal(backupID)
	if err != nil {
		return nil, 0, err
	}
	dockerClient, err := s.dockerService.GetClient(ctx)
	if err != nil {
		return nil, 0, err
	}
	containerID, cleanup, err := s.createBackupTempContainerInternal(ctx, dockerClient, "/volume", true)
	if err != nil {
		return nil, 0, err
	}
	return volumehelper.DownloadFileFromContainer(ctx, dockerClient, containerID, path.Join("/volume", filename), cleanup)
}

func (s *VolumeService) deleteArchiveBackupInternal(ctx context.Context, entry *models.VolumeBackup, user *models.User) error {
	// Delete from DB first - if this fails, no changes are made. If file
	// deletion fails afterward we just have an orphan file, easier to clean up
	// than an orphan DB record pointing at a missing file.
	volumeName := entry.VolumeName
	backupID := entry.ID
	if err := s.db.WithContext(ctx).Delete(entry).Error; err != nil {
		return err
	}
	containerID, cleanup, err := s.createBackupTempContainerInternal(ctx, nil, "/volume", false)
	if err != nil {
		slog.WarnContext(ctx, "failed to create container for backup file cleanup", "backup_id", backupID, "error", err.Error())
	} else {
		defer cleanup()
		filename, filenameErr := s.backupArchiveFilenameInternal(backupID)
		if filenameErr != nil {
			slog.WarnContext(ctx, "failed to sanitize backup id for file cleanup", "backup_id", backupID, "error", filenameErr.Error())
		} else if _, _, err = s.execInContainerInternal(ctx, containerID, []string{"rm", "-f", path.Join("/volume", filename)}); err != nil {
			slog.WarnContext(ctx, "failed to delete backup file (orphan file may remain)", "backup_id", backupID, "error", err.Error())
		}
	}
	s.logBackupDeleteEventInternal(ctx, volumeName, backupID, user)
	return nil
}

func (s *VolumeService) restoreArchiveBackupInternal(ctx context.Context, dockerClient *client.Client, volumeName, backupID string) error {
	filename, err := s.backupArchiveFilenameInternal(backupID)
	if err != nil {
		return err
	}
	helperImage, err := getVolumeHelperImageInternal(ctx, s.dockerService, s.imageService, dockerClient)
	if err != nil {
		return err
	}
	backupStorage, err := s.resolveUsableBackupStorageMountInternal(ctx, dockerClient, "/backups", true)
	if err != nil {
		return err
	}
	config := &container.Config{
		Image: helperImage,
		Cmd: []string{
			"sh",
			"-c",
			fmt.Sprintf("set -e; tmp=$(mktemp -d /volume/.restore_tmp.XXXXXX); tar -tzf /backups/%s >/dev/null; tar -xzf /backups/%s -C \"$tmp\"; find /volume -mindepth 1 -maxdepth 1 -not -path \"$tmp\" -exec rm -rf -- {} +; find \"$tmp\" -mindepth 1 -maxdepth 1 -exec mv -- {} /volume/ \\;; rmdir \"$tmp\"", filename, filename),
		},
		Labels: volumehelper.Labels(),
	}
	hostConfig := volumehelper.HostConfig(helperImage, []string{
		volumeName + ":/volume",
	}, []mount.Mount{backupStorage.mount})
	resp, err := dockerClient.ContainerCreate(ctx, client.ContainerCreateOptions{
		Config:     config,
		HostConfig: hostConfig,
	})
	if err != nil {
		return errors.WrapIf(err, "failed to create restore container")
	}
	defer func() {
		_, _ = dockerClient.ContainerRemove(context.WithoutCancel(ctx), resp.ID, volumehelper.RemoveOptions())
	}()
	if _, err := dockerClient.ContainerStart(ctx, resp.ID, client.ContainerStartOptions{}); err != nil {
		return errors.WrapIf(err, "failed to start restore container")
	}
	waitResult := dockerClient.ContainerWait(ctx, resp.ID, client.ContainerWaitOptions{Condition: container.WaitConditionNotRunning})
	var waitBody container.WaitResponse
	select {
	case err := <-waitResult.Error:
		if err != nil {
			return err
		}
	case waitBody = <-waitResult.Result:
	}
	if waitBody.StatusCode != 0 {
		return errors.Errorf("restore container exited with code %d (volume may be partially wiped)", waitBody.StatusCode)
	}
	return nil
}

func (s *VolumeService) listArchiveBackupFilesInternal(ctx context.Context, backupID string) ([]string, error) {
	filename, err := s.backupArchiveFilenameInternal(backupID)
	if err != nil {
		return nil, err
	}
	dockerClient, err := s.dockerService.GetClient(ctx)
	if err != nil {
		return nil, err
	}
	backupStorage, err := s.resolveUsableBackupStorageMountInternal(ctx, dockerClient, "/volume", true)
	if err != nil {
		return nil, err
	}
	containerID, cleanup, err := s.createBackupTempContainerWithMountInternal(ctx, dockerClient, "", backupStorage.mount)
	if err != nil {
		return nil, err
	}
	defer cleanup()
	stdout, _, err := s.execInContainerInternal(ctx, containerID, []string{"tar", "-tzf", path.Join("/volume", filename)})
	if err != nil {
		return nil, err
	}
	lines := strings.Split(strings.TrimSpace(stdout), "\n")
	files := make([]string, 0, len(lines))
	seen := make(map[string]struct{})
	for _, line := range lines {
		clean := strings.TrimSpace(line)
		if clean == "" {
			continue
		}
		clean = strings.TrimPrefix(clean, "./")
		if strings.HasSuffix(clean, "/") {
			continue
		}
		if _, ok := seen[clean]; ok {
			continue
		}
		seen[clean] = struct{}{}
		files = append(files, clean)
	}
	return files, nil
}

func (s *VolumeService) restoreArchiveBackupFilesInternal(ctx context.Context, dockerClient *client.Client, volumeName, backupID string, cleanedPaths []string) error {
	filename, err := s.backupArchiveFilenameInternal(backupID)
	if err != nil {
		return err
	}
	tarPaths := make([]string, 0, len(cleanedPaths))
	for _, cleaned := range cleanedPaths {
		tarPaths = append(tarPaths, "./"+cleaned)
	}
	helperImage, err := getVolumeHelperImageInternal(ctx, s.dockerService, s.imageService, dockerClient)
	if err != nil {
		return err
	}
	backupStorage, err := s.resolveUsableBackupStorageMountInternal(ctx, dockerClient, "/backups", true)
	if err != nil {
		return err
	}
	config := &container.Config{
		Image:           helperImage,
		Cmd:             []string{"sleep", "infinity"},
		NetworkDisabled: true,
		Labels:          volumehelper.Labels(),
	}
	hostConfig := volumehelper.HostConfig(helperImage, []string{
		volumeName + ":/volume",
	}, []mount.Mount{backupStorage.mount})
	resp, err := dockerClient.ContainerCreate(ctx, client.ContainerCreateOptions{
		Config:     config,
		HostConfig: hostConfig,
	})
	if err != nil {
		return errors.WrapIf(err, "failed to create restore container")
	}
	defer func() {
		_, _ = dockerClient.ContainerRemove(context.WithoutCancel(ctx), resp.ID, volumehelper.RemoveOptions())
	}()
	if _, err := dockerClient.ContainerStart(ctx, resp.ID, client.ContainerStartOptions{}); err != nil {
		return errors.WrapIf(err, "failed to start restore container")
	}
	cmd := append([]string{"tar", "-xzf", path.Join("/backups", filename), "-C", "/volume", "--"}, tarPaths...)
	_, stderr, err := s.execInContainerInternal(ctx, resp.ID, cmd)
	if err != nil {
		return errors.WrapIf(err, "failed to restore files")
	}
	if strings.TrimSpace(stderr) != "" {
		slog.DebugContext(ctx, "volume service: restore files stderr", "backup_id", backupID, "stderr", strings.TrimSpace(stderr))
	}
	return nil
}

const defaultVolumeBackupSchedule = "0 0 2 * * *"

func (s *VolumeService) loadVolumeBackupPoliciesInternal(ctx context.Context, volumeName string) ([]models.VolumeBackupPolicy, error) {
	var policies []models.VolumeBackupPolicy
	if err := s.db.WithContext(ctx).Where("volume_name = ?", volumeName).Order("created_at ASC").Find(&policies).Error; err != nil {
		return nil, fmt.Errorf("failed to load volume backup policies: %w", err)
	}
	return policies, nil
}

func (s *VolumeService) loadVolumeBackupPolicyInternal(ctx context.Context, volumeName, policyID string) (*models.VolumeBackupPolicy, error) {
	if strings.TrimSpace(policyID) == "" {
		return nil, nil
	}
	var policy models.VolumeBackupPolicy
	err := s.db.WithContext(ctx).Where("id = ? AND volume_name = ?", policyID, volumeName).First(&policy).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to load volume backup policy: %w", err)
	}
	return &policy, nil
}

func (s *VolumeService) GetBackupPolicies(ctx context.Context, volumeName string) (*volumetypes.BackupPolicyCollection, error) {
	policies, err := s.loadVolumeBackupPoliciesInternal(ctx, volumeName)
	if err != nil {
		return nil, err
	}
	result := &volumetypes.BackupPolicyCollection{Policies: make([]volumetypes.BackupPolicy, 0, len(policies))}
	destinations := make(map[string]models.S3Destination)
	if s.s3Destinations != nil {
		available, listErr := s.s3Destinations.ListAllS3Destinations(ctx)
		if listErr == nil {
			result.S3Available = len(available) > 0
			for _, destination := range available {
				destinations[destination.ID] = models.S3Destination{BaseModel: models.BaseModel{ID: destination.ID}, Name: destination.Name, Bucket: destination.Bucket}
			}
		}
	}
	for i := range policies {
		var lastRun *models.VolumeBackup
		var entry models.VolumeBackup
		if runErr := s.db.WithContext(ctx).Where("policy_id = ?", policies[i].ID).Order("created_at DESC").First(&entry).Error; runErr == nil {
			if destination, ok := destinations[entry.S3DestinationID]; ok {
				entry.S3DestinationName = destination.Name
			}
			lastRun = &entry
		} else if !errors.Is(runErr, gorm.ErrRecordNotFound) {
			return nil, fmt.Errorf("failed to load latest volume backup: %w", runErr)
		}
		dto := policies[i].ToDTO(lastRun)
		dto.S3Available = result.S3Available
		if destination, ok := destinations[policies[i].S3DestinationID]; ok {
			dto.S3Bucket = destination.Bucket
			dto.S3DestinationName = destination.Name
		}
		result.Policies = append(result.Policies, dto)
	}
	return result, nil
}

//nolint:gocognit // policy validation, persistence, and job replacement must remain atomic from the caller's perspective
func (s *VolumeService) UpdateBackupPolicies(ctx context.Context, volumeName string, updates []volumetypes.UpdateBackupPolicy) (*volumetypes.BackupPolicyCollection, error) {
	for i := range updates {
		schedule, err := schedule.NormalizeSixField(updates[i].Schedule, "volume backup")
		if err != nil {
			return nil, err
		}
		updates[i].Schedule = schedule
		if updates[i].RetentionCount < 0 || updates[i].RetentionCount > 3650 {
			return nil, errors.New("retentionCount must be between 0 and 3650")
		}
		if !updates[i].LocalEnabled && !updates[i].S3Enabled {
			return nil, errors.New("select at least one volume backup destination")
		}
		if updates[i].S3Enabled {
			if s.s3Destinations == nil {
				return nil, errors.New("S3 backup destinations are unavailable")
			}
			if strings.TrimSpace(updates[i].S3DestinationID) == "" {
				return nil, errors.New("select an S3 destination for volume backups")
			}
			if _, err := s.s3Destinations.Configuration(ctx, updates[i].S3DestinationID); err != nil {
				return nil, errors.New("select a valid S3 destination for volume backups")
			}
		} else {
			updates[i].S3DestinationID = ""
		}
	}
	existing, err := s.loadVolumeBackupPoliciesInternal(ctx, volumeName)
	if err != nil {
		return nil, err
	}
	byID := make(map[string]models.VolumeBackupPolicy, len(existing))
	for i := range existing {
		byID[existing[i].ID] = existing[i]
	}
	policies := make([]models.VolumeBackupPolicy, 0, len(updates))
	kept := make(map[string]struct{}, len(updates))
	for _, update := range updates {
		policy := models.VolumeBackupPolicy{VolumeName: volumeName}
		if update.ID != "" {
			var ok bool
			policy, ok = byID[update.ID]
			if !ok {
				return nil, errors.New("volume backup policy not found")
			}
			if _, duplicate := kept[update.ID]; duplicate {
				return nil, errors.New("duplicate volume backup policy")
			}
			kept[update.ID] = struct{}{}
		}
		policy.Enabled, policy.Schedule, policy.RetentionCount = update.Enabled, update.Schedule, update.RetentionCount
		policy.StopContainers, policy.LocalEnabled, policy.S3Enabled = update.StopContainers, update.LocalEnabled, update.S3Enabled
		policy.S3DestinationID = update.S3DestinationID
		policies = append(policies, policy)
	}
	err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		for i := range policies {
			if saveErr := tx.Save(&policies[i]).Error; saveErr != nil {
				return saveErr
			}
		}
		for i := range existing {
			if _, ok := kept[existing[i].ID]; !ok {
				if deleteErr := tx.Delete(&existing[i]).Error; deleteErr != nil {
					return deleteErr
				}
			}
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("failed to save volume backup policies: %w", err)
	}
	for i := range existing {
		if _, ok := kept[existing[i].ID]; !ok {
			s.jobs.Unregister(ctx, existing[i].ID)
		}
	}
	for i := range policies {
		s.rescheduleVolumeBackupPolicyInternal(ctx, &policies[i])
	}
	return s.GetBackupPolicies(ctx, volumeName)
}

func (s *VolumeService) applyVolumeBackupRetentionInternal(ctx context.Context, policyID string, retentionCount int) error {
	expired, err := backup.ExpiredRunIDs(ctx, s.db, "volume_backups", policyID, retentionCount)
	if err != nil {
		return err
	}
	for _, id := range expired {
		if err := s.DeleteBackup(ctx, id, nil); err != nil {
			return err
		}
	}
	return nil
}

func (s *VolumeService) runScheduledBackupInternal(ctx context.Context, policyID string) {
	var policy models.VolumeBackupPolicy
	if err := s.db.WithContext(ctx).Where("id = ? AND enabled = ?", policyID, true).First(&policy).Error; err != nil {
		return
	}
	var entry *models.VolumeBackup
	_, err := activitylib.RunHandlerActivity(ctx, s.activityService, activitylib.HandlerOptions{
		EnvironmentID:  "0",
		Type:           models.ActivityTypeResourceAction,
		ResourceType:   "volume_backup",
		ResourceID:     policy.VolumeName,
		ResourceName:   policy.VolumeName,
		User:           &models.SystemUser,
		Step:           "Creating scheduled backup",
		Message:        "Creating scheduled volume backup",
		SuccessMessage: "Scheduled volume backup created successfully",
		Metadata: models.JSON{
			"action":          "scheduled_volume_backup",
			"policyId":        policy.ID,
			"schedule":        policy.Schedule,
			"volumeName":      policy.VolumeName,
			"retentionCount":  policy.RetentionCount,
			"stopContainers":  policy.StopContainers,
			"localEnabled":    policy.LocalEnabled,
			"s3Enabled":       policy.S3Enabled,
			"s3DestinationId": policy.S3DestinationID,
		},
	}, func(activityCtx context.Context) error {
		var backupErr error
		entry, backupErr = s.CreateBackup(activityCtx, policy.VolumeName, models.SystemUser, models.VolumeBackupTriggerScheduled, volumetypes.CreateBackupRequest{PolicyID: policy.ID})
		return backupErr
	})
	if errors.Is(err, ErrVolumeBackupAlreadyRunning) {
		slog.InfoContext(ctx, "Scheduled volume backup skipped; another backup is running", "volume", policy.VolumeName)
		return
	}
	if err != nil {
		slog.ErrorContext(ctx, "Scheduled volume backup failed", "volume", policy.VolumeName, "error", err)
		return
	}
	slog.InfoContext(ctx, "Scheduled volume backup completed", "volume", policy.VolumeName, "backup_id", entry.ID, "remote_snapshot_id", entry.RemoteSnapshotID)
}

func (s *VolumeService) rescheduleVolumeBackupPolicyInternal(ctx context.Context, policy *models.VolumeBackupPolicy) {
	if policy == nil {
		return
	}
	if !policy.Enabled {
		s.jobs.Unregister(ctx, policy.ID)
		return
	}
	policyID := policy.ID
	s.jobs.Register(ctx, policyID,
		func(ctx context.Context) string {
			var current models.VolumeBackupPolicy
			if err := s.db.WithContext(ctx).Where("id = ?", policyID).First(&current).Error; err != nil {
				return defaultVolumeBackupSchedule
			}
			return current.Schedule
		},
		func(ctx context.Context) {
			s.runScheduledBackupInternal(ctx, policyID)
		},
	)
}

func (s *VolumeService) RegisterBackupJobsOnStartup(ctx context.Context) {
	if !s.jobs.Enabled() {
		return
	}
	var policies []models.VolumeBackupPolicy
	if err := s.db.WithContext(ctx).Where("enabled = ?", true).Find(&policies).Error; err != nil {
		slog.ErrorContext(ctx, "Failed to load scheduled volume backups", "error", err)
		return
	}
	for i := range policies {
		s.rescheduleVolumeBackupPolicyInternal(ctx, &policies[i])
	}
	slog.InfoContext(ctx, "Registered scheduled volume backup jobs", "count", len(policies))
}

func (s *VolumeService) removeVolumeBackupPolicyInternal(ctx context.Context, volumeName string) {
	policies, err := s.loadVolumeBackupPoliciesInternal(ctx, volumeName)
	if err != nil {
		return
	}
	for i := range policies {
		s.jobs.Unregister(ctx, policies[i].ID)
	}
	if err := s.db.WithContext(ctx).Where("volume_name = ?", volumeName).Delete(&models.VolumeBackupPolicy{}).Error; err != nil {
		slog.WarnContext(ctx, "Failed to delete volume backup policy", "volume", volumeName, "error", err)
	}
}
