package volume

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json/v2"
	stderrors "errors"
	"fmt"
	"log/slog"
	"path"
	"strings"
	"time"

	"emperror.dev/errors"
	"gorm.io/gorm"

	"github.com/getarcaneapp/arcane/backend/v2/internal/models"
	docker "github.com/getarcaneapp/arcane/backend/v2/pkg/dockerutil"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/libarcane"
	activitylib "github.com/getarcaneapp/arcane/backend/v2/pkg/libarcane/activity"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/libarcane/volumehelper"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/pagination"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/utils/schedule"
	schedulertypes "github.com/getarcaneapp/arcane/types/v2/scheduler"
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

type rusticRepositoryInternal struct {
	environment []string
	mounts      []mount.Mount
}

type rusticSnapshotInternal struct {
	ID      string `json:"id"`
	Summary struct {
		TotalBytesProcessed int64 `json:"total_bytes_processed"`
	} `json:"summary"`
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

const volumeRusticRepositoryPath = "/repository/volumes"

func (s *VolumeService) rusticPasswordInternal() string {
	sum := sha256.Sum256([]byte("arcane-volume-backups:" + s.encryptionKey))
	return hex.EncodeToString(sum[:])
}

func (s *VolumeService) localRusticRepositoryInternal(ctx context.Context, dockerClient *client.Client, readOnly bool) (rusticRepositoryInternal, error) {
	storage, err := s.resolveUsableBackupStorageMountInternal(ctx, dockerClient, "/repository", readOnly)
	if err != nil {
		return rusticRepositoryInternal{}, err
	}
	return rusticRepositoryInternal{
		environment: []string{"RUSTIC_REPOSITORY=" + volumeRusticRepositoryPath},
		mounts:      []mount.Mount{storage.mount},
	}, nil
}

func (s *VolumeService) remoteRusticRepositoryInternal(ctx context.Context, destinationID string) (rusticRepositoryInternal, error) {
	if s.s3Destinations == nil {
		return rusticRepositoryInternal{}, errors.New("S3 backup service is unavailable")
	}
	configuration, err := s.s3Destinations.Configuration(ctx, destinationID)
	if err != nil {
		return rusticRepositoryInternal{}, errors.New("the selected S3 backup destination is not configured")
	}
	instanceID := strings.TrimSpace(s.settingsService.GetSettingsConfig().InstanceID.Value)
	if instanceID == "" {
		return rusticRepositoryInternal{}, errors.New("arcane instance ID is unavailable")
	}
	return rusticRepositoryInternal{environment: configuration.RusticEnvironment("arcane-volume-backups", instanceID)}, nil
}

func (s *VolumeService) createRusticSnapshotInternal(ctx context.Context, dockerClient *client.Client, repository rusticRepositoryInternal, sourceVolumeName, label string) (rusticSnapshotInternal, error) {
	output, err := s.rusticService.Run(ctx, dockerClient, repository.environment, repository.mounts, s.rusticPasswordInternal(),
		[]string{"backup", "--init", "--json", "--as-path", "/", "--host", "arcane", "--label", label, "/volume"},
		mount.Mount{Type: mount.TypeVolume, Source: sourceVolumeName, Target: "/volume", ReadOnly: true},
	)
	if err != nil {
		return rusticSnapshotInternal{}, err
	}
	var snapshot rusticSnapshotInternal
	if err := json.Unmarshal([]byte(output), &snapshot); err != nil {
		return rusticSnapshotInternal{}, fmt.Errorf("failed to decode Rustic snapshot: %w", err)
	}
	if snapshot.ID == "" {
		return rusticSnapshotInternal{}, errors.New("rustic did not return a snapshot ID")
	}
	return snapshot, nil
}

func (s *VolumeService) rusticRepositoryForBackupInternal(ctx context.Context, dockerClient *client.Client, backup *models.VolumeBackup) (rusticRepositoryInternal, string, error) {
	if backup.LocalSnapshotID != "" {
		repository, err := s.localRusticRepositoryInternal(ctx, dockerClient, true)
		return repository, backup.LocalSnapshotID, err
	}
	if backup.RemoteSnapshotID != "" {
		repository, err := s.remoteRusticRepositoryInternal(ctx, backup.S3DestinationID)
		return repository, backup.RemoteSnapshotID, err
	}
	return rusticRepositoryInternal{}, "", errors.New("volume backup has no Rustic snapshot")
}

func (s *VolumeService) restoreRusticSnapshotInternal(ctx context.Context, dockerClient *client.Client, repository rusticRepositoryInternal, snapshotID, volumeName string, deleteExtra bool, sourcePath, destinationPath string) error {
	command := []string{"restore"}
	if deleteExtra {
		command = append(command, "--delete")
	}
	source := snapshotID + ":/"
	if sourcePath != "" {
		source = snapshotID + ":/" + strings.TrimPrefix(sourcePath, "/")
	}
	if destinationPath == "" {
		destinationPath = "/volume"
	}
	command = append(command, source, destinationPath)
	_, err := s.rusticService.Run(ctx, dockerClient, repository.environment, repository.mounts, s.rusticPasswordInternal(), command,
		mount.Mount{Type: mount.TypeVolume, Source: volumeName, Target: "/volume"},
	)
	return err
}

func (s *VolumeService) forgetRusticSnapshotInternal(ctx context.Context, dockerClient *client.Client, repository rusticRepositoryInternal, snapshotID string) error {
	_, err := s.rusticService.Run(ctx, dockerClient, repository.environment, repository.mounts, s.rusticPasswordInternal(), []string{"forget", "--prune", snapshotID})
	return err
}

//nolint:gocognit,gocyclo // backup orchestration keeps cleanup and persistence transitions in one transaction-like flow
func (s *VolumeService) CreateBackup(ctx context.Context, volumeName string, user models.User, trigger models.VolumeBackupTrigger, request volumetypes.CreateBackupRequest) (_ *models.VolumeBackup, err error) {
	if _, loaded := s.runningBackups.LoadOrStore(volumeName, struct{}{}); loaded {
		return nil, ErrVolumeBackupAlreadyRunning
	}
	defer s.runningBackups.Delete(volumeName)
	destination, policyID := request.Destination, request.PolicyID
	if trigger == "" {
		trigger = models.VolumeBackupTriggerManual
	}
	if destination != "" && destination != volumetypes.BackupDestinationLocal && destination != volumetypes.BackupDestinationS3 && destination != volumetypes.BackupDestinationLocalS3 {
		return nil, errors.New("invalid volume backup destination")
	}
	var policy *models.VolumeBackupPolicy
	if trigger != models.VolumeBackupTriggerSafety {
		policy, err = s.loadVolumeBackupPolicyInternal(ctx, volumeName, policyID)
		if err != nil {
			return nil, err
		}
		if policyID != "" && policy == nil {
			return nil, errors.New("volume backup policy not found")
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
		return nil, errors.New("select at least one volume backup destination")
	}
	if s3Enabled && strings.TrimSpace(s3DestinationID) == "" {
		return nil, errors.New("select an S3 destination for the volume backup")
	}
	if s3Enabled {
		if s.s3Destinations == nil {
			return nil, errors.New("S3 backup destinations are unavailable")
		}
		if _, err := s.s3Destinations.Configuration(ctx, s3DestinationID); err != nil {
			return nil, errors.New("select a valid S3 destination for the volume backup")
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
	backup := &models.VolumeBackup{
		VolumeName: volumeName, CreatedAt: time.Now(), Status: models.VolumeBackupStatusRunning,
		Trigger: trigger, Destination: destination, S3DestinationID: s3DestinationID, PolicyID: policyID,
	}
	backup.ID = fmt.Sprintf("%s-%d-%s", volumeName, time.Now().UnixNano(), uuid.NewString()[:8])
	if err := s.db.WithContext(ctx).Create(backup).Error; err != nil {
		return nil, err
	}
	defer func() {
		if err != nil {
			backup.Status, backup.Error = models.VolumeBackupStatusFailed, err.Error()
		} else {
			backup.Status, backup.Error = models.VolumeBackupStatusSucceeded, ""
		}
		if saveErr := s.db.WithContext(context.WithoutCancel(ctx)).Save(backup).Error; saveErr != nil {
			err = stderrors.Join(err, fmt.Errorf("failed to save volume backup result: %w", saveErr))
		}
	}()
	dockerClient, err := s.dockerService.GetClient(ctx)
	if err != nil {
		return backup, err
	}
	var stopped []container.Summary
	containersStopped := false
	if policy != nil && policy.StopContainers {
		stopped, err = s.stopRunningContainersForBackupInternal(ctx, dockerClient, volumeName, user)
		containersStopped = len(stopped) > 0
		defer func() {
			if containersStopped {
				_, restartErr := s.startContainersAfterBackupInternal(context.WithoutCancel(ctx), dockerClient, stopped, user)
				err = stderrors.Join(err, restartErr)
			}
		}()
		if err != nil {
			return backup, err
		}
	}
	if localEnabled {
		repository, repoErr := s.localRusticRepositoryInternal(ctx, dockerClient, false)
		if repoErr != nil {
			return backup, repoErr
		}
		snapshot, snapshotErr := s.createRusticSnapshotInternal(ctx, dockerClient, repository, volumeName, volumeName)
		if snapshotErr != nil {
			return backup, fmt.Errorf("failed to create local Rustic snapshot: %w", snapshotErr)
		}
		backup.LocalSnapshotID = snapshot.ID
		backup.Size = snapshot.Summary.TotalBytesProcessed
	}
	if s3Enabled {
		repository, repoErr := s.remoteRusticRepositoryInternal(ctx, s3DestinationID)
		if repoErr != nil {
			return backup, repoErr
		}
		snapshot, snapshotErr := s.createRusticSnapshotInternal(ctx, dockerClient, repository, volumeName, volumeName)
		if snapshotErr != nil {
			return backup, fmt.Errorf("failed to create S3 Rustic snapshot: %w", snapshotErr)
		}
		backup.RemoteSnapshotID = snapshot.ID
		if backup.Size == 0 {
			backup.Size = snapshot.Summary.TotalBytesProcessed
		}
	}
	if containersStopped {
		stopped, err = s.startContainersAfterBackupInternal(context.WithoutCancel(ctx), dockerClient, stopped, user)
		containersStopped = len(stopped) > 0
		if err != nil {
			return backup, err
		}
	}
	if trigger != models.VolumeBackupTriggerSafety && policy != nil && policy.RetentionCount > 0 {
		if err := s.applyVolumeBackupRetentionInternal(ctx, policy.ID, policy.RetentionCount); err != nil {
			return backup, fmt.Errorf("failed to apply volume backup retention: %w", err)
		}
	}
	metadata := models.JSON{"action": "backup_create", "backup_id": backup.ID, "size": backup.Size, "destination": backup.Destination}
	if logErr := s.eventService.LogVolumeEvent(ctx, models.EventTypeVolumeBackupCreate, volumeName, volumeName, user.ID, user.Username, "0", metadata); logErr != nil {
		slog.WarnContext(ctx, "could not log volume backup create event", "volume", volumeName, "error", logErr)
	}
	return backup, nil
}

func (s *VolumeService) UploadBackup(ctx context.Context, backupID, s3DestinationID string) (*models.VolumeBackup, error) {
	var backup models.VolumeBackup
	if err := s.db.WithContext(ctx).Where("id = ?", backupID).First(&backup).Error; err != nil {
		return nil, err
	}
	if backup.Status != models.VolumeBackupStatusSucceeded || backup.LocalSnapshotID == "" {
		return nil, errors.New("only successful local volume backups can be uploaded")
	}
	if backup.RemoteSnapshotID != "" {
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
	temporaryVolume := "arcane-rustic-copy-" + uuid.NewString()
	if _, err := dockerClient.VolumeCreate(ctx, client.VolumeCreateOptions{Name: temporaryVolume, Labels: volumehelper.Labels()}); err != nil {
		return nil, fmt.Errorf("failed to create temporary Rustic copy volume: %w", err)
	}
	defer func() {
		_, _ = dockerClient.VolumeRemove(context.WithoutCancel(ctx), temporaryVolume, client.VolumeRemoveOptions{Force: true})
	}()
	localRepository, err := s.localRusticRepositoryInternal(ctx, dockerClient, true)
	if err != nil {
		return nil, err
	}
	if err := s.restoreRusticSnapshotInternal(ctx, dockerClient, localRepository, backup.LocalSnapshotID, temporaryVolume, true, "", ""); err != nil {
		return nil, fmt.Errorf("failed to load local Rustic snapshot for upload: %w", err)
	}
	remoteRepository, err := s.remoteRusticRepositoryInternal(ctx, s3DestinationID)
	if err != nil {
		return nil, err
	}
	snapshot, err := s.createRusticSnapshotInternal(ctx, dockerClient, remoteRepository, temporaryVolume, backup.VolumeName)
	if err != nil {
		return nil, fmt.Errorf("failed to upload Rustic snapshot to S3: %w", err)
	}
	backup.RemoteSnapshotID = snapshot.ID
	backup.S3DestinationID = s3DestinationID
	backup.Destination = volumetypes.BackupDestinationLocalS3
	if err := s.db.WithContext(ctx).Save(&backup).Error; err != nil {
		return nil, fmt.Errorf("failed to save uploaded volume backup: %w", err)
	}
	return &backup, nil
}

func (s *VolumeService) DeleteBackup(ctx context.Context, backupID string, user *models.User) error {
	var backup models.VolumeBackup
	if err := s.db.WithContext(ctx).Where("id = ?", backupID).First(&backup).Error; err != nil {
		return err
	}
	dockerClient, err := s.dockerService.GetClient(ctx)
	if err != nil {
		return err
	}
	var deleteErr error
	if backup.LocalSnapshotID != "" {
		repository, repoErr := s.localRusticRepositoryInternal(ctx, dockerClient, false)
		if repoErr == nil {
			repoErr = s.forgetRusticSnapshotInternal(ctx, dockerClient, repository, backup.LocalSnapshotID)
		}
		if repoErr != nil {
			deleteErr = stderrors.Join(deleteErr, fmt.Errorf("failed to delete local Rustic snapshot: %w", repoErr))
		} else {
			backup.LocalSnapshotID = ""
		}
	}
	if backup.RemoteSnapshotID != "" {
		repository, repoErr := s.remoteRusticRepositoryInternal(ctx, backup.S3DestinationID)
		if repoErr == nil {
			repoErr = s.forgetRusticSnapshotInternal(ctx, dockerClient, repository, backup.RemoteSnapshotID)
		}
		if repoErr != nil {
			deleteErr = stderrors.Join(deleteErr, fmt.Errorf("failed to delete S3 Rustic snapshot: %w", repoErr))
		} else {
			backup.RemoteSnapshotID = ""
			backup.S3DestinationID = ""
		}
	}
	if deleteErr != nil {
		switch {
		case backup.LocalSnapshotID != "" && backup.RemoteSnapshotID != "":
			backup.Destination = volumetypes.BackupDestinationLocalS3
		case backup.LocalSnapshotID != "":
			backup.Destination = volumetypes.BackupDestinationLocal
		case backup.RemoteSnapshotID != "":
			backup.Destination = volumetypes.BackupDestinationS3
		}
		backup.Error = deleteErr.Error()
		if saveErr := s.db.WithContext(ctx).Save(&backup).Error; saveErr != nil {
			deleteErr = stderrors.Join(deleteErr, saveErr)
		}
		return deleteErr
	}
	if err := s.db.WithContext(ctx).Delete(&backup).Error; err != nil {
		return fmt.Errorf("failed to delete volume backup record: %w", err)
	}
	actingUser := user
	if actingUser == nil {
		actingUser = &models.SystemUser
	}
	if logErr := s.eventService.LogVolumeEvent(ctx, models.EventTypeVolumeBackupDelete, backup.VolumeName, backup.VolumeName, actingUser.ID, actingUser.Username, "0", models.JSON{"action": "backup_delete", "backup_id": backupID}); logErr != nil {
		slog.WarnContext(ctx, "could not log volume backup delete event", "volume", backup.VolumeName, "error", logErr)
	}
	return nil
}

func (s *VolumeService) RestoreBackup(ctx context.Context, volumeName, backupID string, user models.User) (err error) {
	var backup models.VolumeBackup
	if err := s.db.WithContext(ctx).Where("id = ?", backupID).First(&backup).Error; err != nil {
		return err
	}
	if backup.VolumeName != volumeName {
		return errors.New("backup does not belong to volume")
	}
	dockerClient, err := s.dockerService.GetClient(ctx)
	if err != nil {
		return err
	}
	repository, snapshotID, err := s.rusticRepositoryForBackupInternal(ctx, dockerClient, &backup)
	if err != nil {
		return err
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
	if err := s.restoreRusticSnapshotInternal(ctx, dockerClient, repository, snapshotID, volumeName, true, "", ""); err != nil {
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
	var backup models.VolumeBackup
	if err := s.db.WithContext(ctx).Where("id = ?", backupID).First(&backup).Error; err != nil {
		return nil, err
	}
	dockerClient, err := s.dockerService.GetClient(ctx)
	if err != nil {
		return nil, err
	}
	repository, snapshotID, err := s.rusticRepositoryForBackupInternal(ctx, dockerClient, &backup)
	if err != nil {
		return nil, err
	}
	output, err := s.rusticService.Run(ctx, dockerClient, repository.environment, repository.mounts, s.rusticPasswordInternal(), []string{"ls", "--json", "--recursive", snapshotID + ":/"})
	if err != nil {
		return nil, fmt.Errorf("failed to list Rustic snapshot: %w", err)
	}
	var files []string
	if err := json.Unmarshal([]byte(output), &files); err != nil {
		return nil, fmt.Errorf("failed to decode Rustic file list: %w", err)
	}
	return files, nil
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
	var backup models.VolumeBackup
	if err := s.db.WithContext(ctx).Where("id = ?", backupID).First(&backup).Error; err != nil {
		return err
	}
	if backup.VolumeName != volumeName {
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
	repository, snapshotID, err := s.rusticRepositoryForBackupInternal(ctx, dockerClient, &backup)
	if err != nil {
		return err
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
	for _, cleaned := range cleanedPaths {
		if err := s.restoreRusticSnapshotInternal(ctx, dockerClient, repository, snapshotID, volumeName, false, cleaned, path.Join("/volume", cleaned)); err != nil {
			return fmt.Errorf("failed to restore %s from Rustic snapshot: %w", cleaned, err)
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

const (
	defaultVolumeBackupSchedule = "0 0 2 * * *"
	volumeBackupJobPrefix       = "volume-backup:"
)

var ErrVolumeBackupAlreadyRunning = errors.New("a backup is already running for this volume")

func (s *VolumeService) SetScheduler(ctx context.Context, scheduler schedulertypes.DynamicScheduler) { //nolint:contextcheck // scheduled backups must use the application lifecycle context
	if ctx == nil {
		ctx = context.Background()
	}
	s.lifecycleCtx = ctx
	s.scheduler = scheduler
}

func (s *VolumeService) backupSchedulerContextInternal(ctx context.Context) context.Context {
	if s.lifecycleCtx != nil {
		return s.lifecycleCtx
	}
	if ctx != nil {
		return context.WithoutCancel(ctx)
	}
	return context.Background()
}

func volumeBackupJobNameInternal(policyID, schedule string) string {
	sum := sha256.Sum256([]byte(schedule))
	return fmt.Sprintf("%s%s:%x", volumeBackupJobPrefix, policyID, sum[:6])
}

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
		var backup models.VolumeBackup
		if runErr := s.db.WithContext(ctx).Where("policy_id = ?", policies[i].ID).Order("created_at DESC").First(&backup).Error; runErr == nil {
			if destination, ok := destinations[backup.S3DestinationID]; ok {
				backup.S3DestinationName = destination.Name
			}
			lastRun = &backup
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
		s.removeVolumeBackupPolicyJobsInternal(ctx, existing[i].ID, existing[i].Schedule)
	}
	for i := range policies {
		s.rescheduleVolumeBackupPolicyInternal(ctx, &policies[i])
	}
	return s.GetBackupPolicies(ctx, volumeName)
}

func (s *VolumeService) applyVolumeBackupRetentionInternal(ctx context.Context, policyID string, retentionCount int) error {
	var expired []models.VolumeBackup
	if err := s.db.WithContext(ctx).
		Where(
			"policy_id = ? AND status = ? AND (COALESCE(local_snapshot_id, '') <> '' OR COALESCE(remote_snapshot_id, '') <> '')",
			policyID,
			models.VolumeBackupStatusSucceeded,
		).
		Order("created_at DESC").
		Offset(retentionCount).
		Find(&expired).Error; err != nil {
		return err
	}
	for i := range expired {
		if err := s.DeleteBackup(ctx, expired[i].ID, nil); err != nil {
			return err
		}
	}
	return nil
}

func (s *VolumeService) buildVolumeBackupJobInternal(policyID, schedule string) *schedulertypes.GenericJob {
	return &schedulertypes.GenericJob{
		JobName: volumeBackupJobNameInternal(policyID, schedule),
		ScheduleFn: func(ctx context.Context) string {
			var policy models.VolumeBackupPolicy
			if err := s.db.WithContext(ctx).Where("id = ?", policyID).First(&policy).Error; err != nil {
				return defaultVolumeBackupSchedule
			}
			return policy.Schedule
		},
		ShouldRunFn: func(ctx context.Context) bool {
			var policy models.VolumeBackupPolicy
			if s.db.WithContext(ctx).Where("id = ? AND enabled = ?", policyID, true).First(&policy).Error != nil {
				return false
			}
			return policy.Schedule == schedule
		},
		RunFn: func(ctx context.Context) {
			var policy models.VolumeBackupPolicy
			if err := s.db.WithContext(ctx).Where("id = ? AND enabled = ?", policyID, true).First(&policy).Error; err != nil {
				return
			}
			var backup *models.VolumeBackup
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
					"schedule":        schedule,
					"volumeName":      policy.VolumeName,
					"retentionCount":  policy.RetentionCount,
					"stopContainers":  policy.StopContainers,
					"localEnabled":    policy.LocalEnabled,
					"s3Enabled":       policy.S3Enabled,
					"s3DestinationId": policy.S3DestinationID,
				},
			}, func(activityCtx context.Context) error {
				var backupErr error
				backup, backupErr = s.CreateBackup(activityCtx, policy.VolumeName, models.SystemUser, models.VolumeBackupTriggerScheduled, volumetypes.CreateBackupRequest{PolicyID: policy.ID})
				return backupErr
			})
			if errors.Is(err, ErrVolumeBackupAlreadyRunning) {
				slog.InfoContext(ctx, "Scheduled volume backup skipped; another backup is running", "volume", policy.VolumeName, "schedule", schedule)
				return
			}
			if err != nil {
				slog.ErrorContext(ctx, "Scheduled volume backup failed", "volume", policy.VolumeName, "schedule", schedule, "error", err)
				return
			}
			slog.InfoContext(ctx, "Scheduled volume backup completed", "volume", policy.VolumeName, "backup_id", backup.ID, "remote_snapshot_id", backup.RemoteSnapshotID)
		},
	}
}

func (s *VolumeService) removeVolumeBackupPolicyJobsInternal(ctx context.Context, policyID, schedule string) {
	if s.scheduler == nil {
		return
	}
	schedulerCtx := s.backupSchedulerContextInternal(ctx)
	s.scheduler.RemoveJob(schedulerCtx, volumeBackupJobNameInternal(policyID, schedule))
}

func (s *VolumeService) rescheduleVolumeBackupPolicyInternal(ctx context.Context, policy *models.VolumeBackupPolicy) {
	if s.scheduler == nil || policy == nil {
		return
	}
	schedulerCtx := s.backupSchedulerContextInternal(ctx)
	if !policy.Enabled {
		s.removeVolumeBackupPolicyJobsInternal(schedulerCtx, policy.ID, policy.Schedule)
		return
	}
	if err := s.scheduler.AddJob(schedulerCtx, s.buildVolumeBackupJobInternal(policy.ID, policy.Schedule)); err != nil {
		slog.ErrorContext(schedulerCtx, "Failed to register volume backup job", "volume", policy.VolumeName, "schedule", policy.Schedule, "error", err)
	}
}

func (s *VolumeService) RegisterBackupJobsOnStartup(ctx context.Context) {
	if s.scheduler == nil {
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
		s.removeVolumeBackupPolicyJobsInternal(ctx, policies[i].ID, policies[i].Schedule)
	}
	if err := s.db.WithContext(ctx).Where("volume_name = ?", volumeName).Delete(&models.VolumeBackupPolicy{}).Error; err != nil {
		slog.WarnContext(ctx, "Failed to delete volume backup policy", "volume", volumeName, "error", err)
	}
}
