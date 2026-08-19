package volume

import (
	"github.com/getarcaneapp/arcane/backend/v2/internal/event"

	"github.com/getarcaneapp/arcane/backend/v2/internal/common"

	"github.com/getarcaneapp/arcane/backend/v2/internal/database"

	"archive/tar"
	"compress/gzip"
	"context"
	"fmt"
	"io"
	"log/slog"
	"path"
	"strconv"
	"strings"
	"time"
	"uuid"

	"emperror.dev/errors"

	docker "github.com/getarcaneapp/arcane/backend/v2/pkg/dockerutil"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/libarcane"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/libarcane/volumehelper"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/pagination"
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
		_, _ = dockerClient.ContainerRemove(ctx, resp.ID, volumehelper.RemoveOptions())
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

func (s *VolumeService) CreateBackup(ctx context.Context, volumeName string, user common.User) (*VolumeBackup, error) {
	slog.DebugContext(ctx, "volume service: create backup", "volume", volumeName, "user", user.ID)
	workspaceLock, _ := ctx.Value(volumeWorkspaceLockContextKeyInternal{}).(volumeWorkspaceLockContextInternal)
	if workspaceLock.service != s || workspaceLock.volumeName != volumeName {
		defer s.workspaceLocks.Lock(volumeName)()
	}

	dockerClient, err := s.dockerService.GetClient(ctx)
	if err != nil {
		return nil, err
	}

	backupID := fmt.Sprintf("%s-%d-%s", volumeName, time.Now().UnixNano(), uuid.New().String()[:8])
	filename, err := s.backupArchiveFilenameInternal(backupID)
	if err != nil {
		return nil, err
	}

	helperImage, err := getVolumeHelperImageInternal(ctx, s.dockerService, s.imageService, dockerClient)
	if err != nil {
		return nil, err
	}

	backupStorage, err := s.resolveUsableBackupStorageMountInternal(ctx, dockerClient, "/backups", false)
	if err != nil {
		return nil, err
	}

	config := &container.Config{
		Image:  helperImage,
		Cmd:    []string{"sh", "-c", fmt.Sprintf("tar -czf /backups/%s -C /volume .", filename)},
		Labels: volumehelper.Labels(),
	}

	hostConfig := volumehelper.HostConfig(helperImage, []string{
		volumeName + ":/volume:ro",
	}, []mount.Mount{backupStorage.mount})

	resp, err := dockerClient.ContainerCreate(ctx, client.ContainerCreateOptions{
		Config:     config,
		HostConfig: hostConfig,
	})
	if err != nil {
		return nil, errors.WrapIf(err, "failed to create backup container")
	}

	if _, err := dockerClient.ContainerStart(ctx, resp.ID, client.ContainerStartOptions{}); err != nil {
		_, _ = dockerClient.ContainerRemove(ctx, resp.ID, volumehelper.RemoveOptions())
		return nil, errors.WrapIf(err, "failed to start backup container")
	}

	waitResult := dockerClient.ContainerWait(ctx, resp.ID, client.ContainerWaitOptions{Condition: container.WaitConditionNotRunning})
	select {
	case err := <-waitResult.Error:
		if err != nil {
			return nil, err
		}
	case status := <-waitResult.Result:
		if status.StatusCode != 0 {
			return nil, errors.Errorf("backup container exited with status %d", status.StatusCode)
		}
	}

	sizeCheckMount := backupStorage.mount
	sizeCheckMount.Target = "/volume"
	sizeCheckMount.ReadOnly = true

	tempContainerID, cleanup, err := s.createBackupTempContainerWithMountInternal(ctx, dockerClient, "", sizeCheckMount)
	if err != nil {
		return nil, err
	}
	defer cleanup()

	sizeStr, _, err := s.execInContainerInternal(ctx, tempContainerID, []string{"stat", "-c", "%s", path.Join("/volume", filename)})
	if err != nil {
		return nil, err
	}
	size, err := strconv.ParseInt(strings.TrimSpace(sizeStr), 10, 64)
	if err != nil {
		return nil, err
	}

	backup := &VolumeBackup{
		VolumeName: volumeName,
		Size:       size,
		CreatedAt:  time.Now(),
	}
	backup.ID = backupID

	if err := s.db.WithContext(ctx).Create(backup).Error; err != nil {
		return nil, err
	}

	metadata := database.JSON{
		"action":    "backup_create",
		"backup_id": backup.ID,
		"filename":  filename,
		"size":      size,
	}
	if logErr := s.eventService.LogVolumeEvent(ctx, event.EventTypeVolumeBackupCreate, volumeName, volumeName, user.ID, user.Username, "0", metadata); logErr != nil {
		slog.WarnContext(ctx, "could not log volume backup create event", "volume", volumeName, "error", logErr.Error())
	}

	return backup, nil
}

func (s *VolumeService) ListBackupsPaginated(ctx context.Context, volumeName string, params pagination.QueryParams) ([]VolumeBackup, pagination.Response, error) {
	slog.DebugContext(ctx, "volume service: list backups paginated", "volume", volumeName, "search", params.Search, "sort", params.Sort, "order", params.Order, "start", params.Start, "limit", params.Limit)
	var backups []VolumeBackup
	query := s.db.WithContext(ctx).Model(&VolumeBackup{}).Where("volume_name = ?", volumeName)

	if params.Search != "" {
		query = query.Where("id LIKE ?", "%"+params.Search+"%")
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

	paginationResp := pagination.BuildResponse(totalItems, totalItems, params)
	return backups, paginationResp, nil
}

func (s *VolumeService) ListBackups(ctx context.Context, volumeName string) ([]VolumeBackup, error) {
	slog.DebugContext(ctx, "volume service: list backups", "volume", volumeName)
	var backups []VolumeBackup
	err := s.db.WithContext(ctx).Where("volume_name = ?", volumeName).Order("created_at DESC").Find(&backups).Error
	return backups, err
}

func (s *VolumeService) DeleteBackup(ctx context.Context, backupID string, user *common.User) error {
	slog.DebugContext(ctx, "volume service: delete backup", "backup_id", backupID)
	var backup VolumeBackup
	if err := s.db.WithContext(ctx).Where("id = ?", backupID).First(&backup).Error; err != nil {
		return err
	}

	// Delete from DB first - if this fails, no changes are made.
	// If file deletion fails afterward, we just have an orphan file (easier to clean up)
	// rather than an orphan DB record pointing to a non-existent file.
	volumeName := backup.VolumeName // Save before deletion
	if err := s.db.WithContext(ctx).Delete(&backup).Error; err != nil {
		return err
	}

	// Now delete the actual file - best effort since DB record is already gone
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

	actingUser := user
	if actingUser == nil {
		actingUser = &common.SystemUser
	}
	metadata := database.JSON{
		"action":    "backup_delete",
		"backup_id": backupID,
	}
	if logErr := s.eventService.LogVolumeEvent(ctx, event.EventTypeVolumeBackupDelete, volumeName, volumeName, actingUser.ID, actingUser.Username, "0", metadata); logErr != nil {
		slog.WarnContext(ctx, "could not log volume backup delete event", "volume", volumeName, "error", logErr.Error())
	}

	return nil
}

func (s *VolumeService) RestoreBackup(ctx context.Context, volumeName, backupID string, user common.User) error {
	slog.DebugContext(ctx, "volume service: restore backup", "volume", volumeName, "backup_id", backupID, "user", user.ID)
	var backup VolumeBackup
	if err := s.db.WithContext(ctx).Where("id = ?", backupID).First(&backup).Error; err != nil {
		return err
	}

	// Validate backup belongs to volume
	if backup.VolumeName != volumeName {
		return errors.Errorf("backup does not belong to volume %s", volumeName)
	}
	unlock := s.workspaceLocks.Lock(volumeName)
	defer unlock()
	ctx = context.WithValue(ctx, volumeWorkspaceLockContextKeyInternal{}, volumeWorkspaceLockContextInternal{
		service:    s,
		volumeName: volumeName,
	})

	// Check if volume is in use by running containers
	inUse, containerIDs, err := s.GetVolumeUsage(ctx, volumeName)
	if err != nil {
		slog.WarnContext(ctx, "could not check volume usage", "volume", volumeName, "error", err.Error())
	} else if inUse {
		return errors.Errorf("volume is in use by %d container(s): restoring while containers are running may cause data corruption. Stop the containers first or use selective file restore", len(containerIDs))
	}

	preBackup, err := s.CreateBackup(ctx, volumeName, user)
	if err != nil {
		return errors.WrapIf(err, "failed to create pre-restore backup")
	}

	dockerClient, err := s.dockerService.GetClient(ctx)
	if err != nil {
		return err
	}

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
			fmt.Sprintf("set -e; tmp=$(mktemp -d /volume/.restore_tmp.XXXXXX); tar -tzf /backups/%s >/dev/null; tar -xzf /backups/%s -C \"$tmp\"; find /volume -mindepth 1 -maxdepth 1 -exec rm -rf -- {} +; find \"$tmp\" -mindepth 1 -maxdepth 1 -exec mv -- {} /volume/ \\;; rmdir \"$tmp\"", filename, filename),
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

	if _, err := dockerClient.ContainerStart(ctx, resp.ID, client.ContainerStartOptions{}); err != nil {
		_, _ = dockerClient.ContainerRemove(ctx, resp.ID, volumehelper.RemoveOptions())
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

	metadata := database.JSON{
		"action":               "backup_restore",
		"backup_id":            backupID,
		"pre_restore_backupId": preBackup.ID,
	}
	if logErr := s.eventService.LogVolumeEvent(ctx, event.EventTypeVolumeBackupRestore, volumeName, volumeName, user.ID, user.Username, "0", metadata); logErr != nil {
		slog.WarnContext(ctx, "could not log volume backup restore event", "volume", volumeName, "error", logErr.Error())
	}

	return nil
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

func (s *VolumeService) restoreBackupFilesInContainerInternal(ctx context.Context, containerID, filename string, cleanedPaths []string) (string, error) {
	args := make([]string, 0, len(cleanedPaths)+5)
	args = append(args, "sh", "-c", restoreBackupFilesScriptInternal, "sh", path.Join("/backups", filename))
	for _, cleaned := range cleanedPaths {
		args = append(args, "./"+cleaned)
	}
	_, stderr, err := s.execInContainerInternal(ctx, containerID, args)
	return stderr, errors.WrapIf(err, "failed to restore files")
}

const restoreBackupFilesScriptInternal = `set -e
archive="$1"
shift
archive_mode=$(stat -c '%A' -- "$archive" 2>/dev/null) || { echo ARCANE_NOT_FOUND >&2; exit 44; }
case "$archive_mode" in -*) ;; *) echo ARCANE_NOT_FOUND >&2; exit 44 ;; esac
for member do
  if ! tar -tzf "$archive" -- "$member" >/dev/null 2>&1; then echo ARCANE_NOT_FOUND >&2; exit 44; fi
done
tar -xzf "$archive" -C /volume -- "$@"`

func (s *VolumeService) BackupHasPath(ctx context.Context, backupID string, filePath string) (bool, error) {
	slog.DebugContext(ctx, "volume service: backup has path", "backup_id", backupID, "path", filePath)
	cleaned, err := s.sanitizeBackupPathInternal(filePath)
	if err != nil {
		return false, err
	}
	filename, err := s.backupArchiveFilenameInternal(backupID)
	if err != nil {
		return false, err
	}

	var backup VolumeBackup
	if err := s.db.WithContext(ctx).Where("id = ?", backupID).First(&backup).Error; err != nil {
		return false, err
	}

	dockerClient, err := s.dockerService.GetClient(ctx)
	if err != nil {
		return false, err
	}

	helperImage, err := getVolumeHelperImageInternal(ctx, s.dockerService, s.imageService, dockerClient)
	if err != nil {
		return false, err
	}

	backupStorage, err := s.resolveUsableBackupStorageMountInternal(ctx, dockerClient, "/volume", true)
	if err != nil {
		return false, err
	}

	containerID, cleanup, err := s.createBackupTempContainerWithMountInternal(ctx, dockerClient, helperImage, backupStorage.mount)
	if err != nil {
		return false, err
	}
	defer cleanup()

	archivePath := path.Join("/volume", filename)
	cmd := []string{"tar", "-tzf", archivePath}
	stdout, stderr, err := s.execInContainerInternal(ctx, containerID, cmd)
	if err != nil {
		return false, err
	}
	if strings.TrimSpace(stderr) != "" {
		return false, errors.Errorf("failed to list backup contents: %s", strings.TrimSpace(stderr))
	}

	for line := range strings.SplitSeq(stdout, "\n") {
		entry := strings.TrimSpace(line)
		if entry == "" {
			continue
		}
		entry = strings.TrimPrefix(entry, "./")
		if entry == cleaned || strings.TrimSuffix(entry, "/") == cleaned {
			return true, nil
		}
	}

	return false, nil
}

func (s *VolumeService) ListBackupFiles(ctx context.Context, backupID string) ([]string, error) {
	slog.DebugContext(ctx, "volume service: list backup files", "backup_id", backupID)
	filename, err := s.backupArchiveFilenameInternal(backupID)
	if err != nil {
		return nil, err
	}

	var backup VolumeBackup
	if err := s.db.WithContext(ctx).Where("id = ?", backupID).First(&backup).Error; err != nil {
		return nil, err
	}

	dockerClient, err := s.dockerService.GetClient(ctx)
	if err != nil {
		return nil, err
	}

	helperImage, err := getVolumeHelperImageInternal(ctx, s.dockerService, s.imageService, dockerClient)
	if err != nil {
		return nil, err
	}

	backupStorage, err := s.resolveUsableBackupStorageMountInternal(ctx, dockerClient, "/volume", true)
	if err != nil {
		return nil, err
	}

	containerID, cleanup, err := s.createBackupTempContainerWithMountInternal(ctx, dockerClient, helperImage, backupStorage.mount)
	if err != nil {
		return nil, err
	}
	defer cleanup()

	archivePath := path.Join("/volume", filename)
	cmd := []string{"tar", "-tzf", archivePath}
	stdout, _, err := s.execInContainerInternal(ctx, containerID, cmd)
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

func (s *VolumeService) RestoreBackupFiles(ctx context.Context, volumeName, backupID string, paths []string, user common.User) error {
	slog.DebugContext(ctx, "volume service: restore backup files", "volume", volumeName, "backup_id", backupID, "paths_count", len(paths), "user", user.ID)
	if len(paths) == 0 {
		return errors.New("no paths provided")
	}
	filename, err := s.backupArchiveFilenameInternal(backupID)
	if err != nil {
		return err
	}

	var backup VolumeBackup
	if err := s.db.WithContext(ctx).Where("id = ?", backupID).First(&backup).Error; err != nil {
		return err
	}
	if backup.VolumeName != volumeName {
		return errors.New("backup does not belong to volume")
	}
	unlock := s.workspaceLocks.Lock(volumeName)
	defer unlock()
	ctx = context.WithValue(ctx, volumeWorkspaceLockContextKeyInternal{}, volumeWorkspaceLockContextInternal{
		service:    s,
		volumeName: volumeName,
	})

	// Create pre-restore backup for safety (consistent with RestoreBackup behavior)
	preBackup, err := s.CreateBackup(ctx, volumeName, user)
	if err != nil {
		return errors.WrapIf(err, "failed to create pre-restore backup")
	}
	slog.DebugContext(ctx, "created pre-restore backup", "volume", volumeName, "pre_backup_id", preBackup.ID)

	cleanedPaths := make([]string, 0, len(paths))
	for _, p := range paths {
		cleaned, err := s.sanitizeBackupPathInternal(p)
		if err != nil {
			return err
		}
		cleanedPaths = append(cleanedPaths, cleaned)
	}
	if len(cleanedPaths) == 0 {
		return errors.New("no valid paths provided")
	}

	dockerClient, err := s.dockerService.GetClient(ctx)
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

	if _, err := dockerClient.ContainerStart(ctx, resp.ID, client.ContainerStartOptions{}); err != nil {
		_, _ = dockerClient.ContainerRemove(ctx, resp.ID, volumehelper.RemoveOptions())
		return errors.WrapIf(err, "failed to start restore container")
	}

	cleanup := func() {
		_, _ = dockerClient.ContainerRemove(ctx, resp.ID, volumehelper.RemoveOptions())
	}
	defer cleanup()

	stderr, err := s.restoreBackupFilesInContainerInternal(ctx, resp.ID, filename, cleanedPaths)
	if err != nil {
		return errors.WrapIf(err, "failed to restore files")
	}
	if strings.TrimSpace(stderr) != "" {
		slog.DebugContext(ctx, "volume service: restore files stderr", "backup_id", backupID, "stderr", strings.TrimSpace(stderr))
	}

	metadata := database.JSON{
		"action":               "backup_restore_files",
		"backup_id":            backupID,
		"pre_restore_backupId": preBackup.ID,
		"paths_count":          len(cleanedPaths),
	}
	if len(cleanedPaths) > 0 {
		limit := min(len(cleanedPaths), 5)
		metadata["paths_sample"] = cleanedPaths[:limit]
	}
	if logErr := s.eventService.LogVolumeEvent(ctx, event.EventTypeVolumeBackupRestoreFiles, volumeName, volumeName, user.ID, user.Username, "0", metadata); logErr != nil {
		slog.WarnContext(ctx, "could not log volume backup restore files event", "volume", volumeName, "error", logErr.Error())
	}

	return nil
}

func (s *VolumeService) DownloadBackup(ctx context.Context, backupID string, user *common.User) (io.ReadCloser, int64, error) {
	slog.DebugContext(ctx, "volume service: download backup", "backup_id", backupID)
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

	reader, size, err := volumehelper.DownloadFileFromContainer(ctx, dockerClient, containerID, path.Join("/volume", filename), cleanup)
	if err != nil {
		return nil, 0, err
	}

	actingUser := user
	if actingUser == nil {
		actingUser = &common.SystemUser
	}
	volumeName := ""
	var backup VolumeBackup
	if err := s.db.WithContext(ctx).Where("id = ?", backupID).First(&backup).Error; err == nil {
		volumeName = backup.VolumeName
	}
	if volumeName != "" {
		metadata := database.JSON{
			"action":    "backup_download",
			"backup_id": backupID,
			"size":      size,
		}
		if logErr := s.eventService.LogVolumeEvent(ctx, event.EventTypeVolumeBackupDownload, volumeName, volumeName, actingUser.ID, actingUser.Username, "0", metadata); logErr != nil {
			slog.WarnContext(ctx, "could not log volume backup download event", "volume", volumeName, "error", logErr.Error())
		}
	}

	return reader, size, nil
}

func (s *VolumeService) UploadAndRestore(ctx context.Context, volumeName string, archive io.ReadSeeker, filename string, user common.User) error {
	slog.DebugContext(ctx, "volume service: upload and restore", "volume", volumeName, "filename", filename, "user", user.ID)

	gzr, err := gzip.NewReader(archive)
	if err != nil {
		return errors.WrapIf(err, "invalid archive")
	}
	if _, err := tar.NewReader(gzr).Next(); err != nil {
		_ = gzr.Close()
		return errors.WrapIf(err, "invalid archive")
	}
	_ = gzr.Close()
	unlock := s.workspaceLocks.Lock(volumeName)
	defer unlock()
	ctx = context.WithValue(ctx, volumeWorkspaceLockContextKeyInternal{}, volumeWorkspaceLockContextInternal{
		service:    s,
		volumeName: volumeName,
	})

	preBackup, err := s.CreateBackup(ctx, volumeName, user)
	if err != nil {
		return errors.WrapIf(err, "failed to create pre-restore backup")
	}

	dockerClient, err := s.dockerService.GetClient(ctx)
	if err != nil {
		return err
	}

	containerID, cleanup, err := s.acquireVolumeHelperInternal(ctx, volumeName)
	if err != nil {
		return err
	}
	defer cleanup()

	tmpDir := fmt.Sprintf("/volume/.restore_tmp_%d", time.Now().UnixNano())
	_, stderr, err := s.execInContainerInternal(ctx, containerID, []string{"mkdir", "-p", tmpDir})
	if err != nil {
		return errors.WrapIf(err, "failed to create temp restore dir")
	}
	if strings.TrimSpace(stderr) != "" {
		slog.DebugContext(ctx, "volume service: restore temp dir stderr", "volume", volumeName, "stderr", strings.TrimSpace(stderr))
	}

	if _, err := archive.Seek(0, io.SeekStart); err != nil {
		return errors.WrapIf(err, "failed to read uploaded archive")
	}
	_, err = dockerClient.CopyToContainer(ctx, containerID, client.CopyToContainerOptions{
		DestinationPath: tmpDir,
		Content:         archive,
	})
	if err != nil {
		return errors.WrapIf(err, "failed to restore from uploaded archive")
	}

	_, stderr, err = s.execInContainerInternal(ctx, containerID, []string{"sh", "-c", fmt.Sprintf("test -n \"$(find %s -mindepth 1 -maxdepth 1 -print -quit)\"", tmpDir)})
	if err != nil {
		return errors.WrapIf(err, "uploaded archive appears empty or invalid")
	}
	if strings.TrimSpace(stderr) != "" {
		slog.DebugContext(ctx, "volume service: restore validate stderr", "volume", volumeName, "stderr", strings.TrimSpace(stderr))
	}

	_, stderr, err = s.execInContainerInternal(ctx, containerID, []string{"sh", "-c", "rm -rf /volume/* /volume/.[!.]* /volume/..?* 2>/dev/null || true"})
	if err != nil {
		return errors.WrapIf(err, "failed to clear volume before restore")
	}
	if strings.TrimSpace(stderr) != "" {
		slog.DebugContext(ctx, "volume service: restore clear stderr", "volume", volumeName, "stderr", strings.TrimSpace(stderr))
	}

	moveCmd := fmt.Sprintf("find %s -mindepth 1 -maxdepth 1 -exec mv -- {} /volume/ \\; && rmdir %s", tmpDir, tmpDir)
	_, stderr, err = s.execInContainerInternal(ctx, containerID, []string{"sh", "-c", moveCmd})
	if err != nil {
		return errors.WrapIf(err, "failed to move restored files into place")
	}
	if strings.TrimSpace(stderr) != "" {
		slog.DebugContext(ctx, "volume service: restore move stderr", "volume", volumeName, "stderr", strings.TrimSpace(stderr))
	}

	metadata := database.JSON{
		"action":               "backup_upload_restore",
		"filename":             filename,
		"pre_restore_backupId": preBackup.ID,
	}
	if logErr := s.eventService.LogVolumeEvent(ctx, event.EventTypeVolumeBackupRestore, volumeName, volumeName, user.ID, user.Username, "0", metadata); logErr != nil {
		slog.WarnContext(ctx, "could not log volume backup upload restore event", "volume", volumeName, "error", logErr.Error())
	}

	return nil
}
