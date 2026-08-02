package services

import (
	"archive/tar"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json/v2"
	stderrors "errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path"
	"strconv"
	"strings"
	"sync"
	"time"

	"emperror.dev/errors"
	"gorm.io/gorm"

	"github.com/getarcaneapp/arcane/backend/v2/internal/database"
	"github.com/getarcaneapp/arcane/backend/v2/internal/models"
	docker "github.com/getarcaneapp/arcane/backend/v2/pkg/dockerutil"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/libarcane"
	activitylib "github.com/getarcaneapp/arcane/backend/v2/pkg/libarcane/activity"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/libarcane/timeouts"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/libarcane/volumehelper"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/pagination"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/utils"
	schedulertypes "github.com/getarcaneapp/arcane/types/v2/scheduler"
	volumetypes "github.com/getarcaneapp/arcane/types/v2/volume"
	"github.com/google/uuid"
	"github.com/moby/moby/api/pkg/stdcopy"
	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/api/types/mount"
	"github.com/moby/moby/api/types/volume"
	"github.com/moby/moby/client"
	"github.com/robfig/cron/v3"
	"github.com/samber/mo"
	"golang.org/x/sync/singleflight"
)

type VolumeService struct {
	db               *database.DB
	dockerService    *DockerClientService
	eventService     *EventService
	activityService  *ActivityService
	settingsService  *SettingsService
	containerService *ContainerService
	imageService     *ImageService
	rusticService    *RusticService
	s3Destinations   *S3DestinationService
	backupVolumeName string
	encryptionKey    string
	helperMu         sync.Mutex
	helperByVolume   map[string]*volumeHelper
	scheduler        schedulertypes.DynamicScheduler
	lifecycleCtx     context.Context
	runningBackups   sync.Map
	// helperGroup deduplicates concurrent read-only helper creation per volume.
	// Without it two simultaneous browse requests each create a helper and the
	// second overwrites the first in helperByVolume, orphaning a `sleep infinity`
	// container that pins the volume until restart.
	helperGroup singleflight.Group
}

// volumeHelper tracks a reused read-only browse helper container and the last
// time it serviced a request, so idle helpers can be reaped. inUse counts the
// requests currently holding the helper: the reaper must not remove a helper
// mid-download just because it was acquired longer ago than the idle timeout.
type volumeHelper struct {
	id         string
	lastUsedAt time.Time
	inUse      int
}

const volumeHelperImage = volumehelper.DefaultToolsImage

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
const trivyCacheVolumePruneFilterValue = libarcane.InternalResourceLabel + "=true"
const volumeBackupContainerRecoveryTimeout = 30 * time.Second
const volumeBackupContainerRecoveryInterval = 500 * time.Millisecond

type backupStorageMountInternal struct {
	mode           backupStorageMode
	mount          mount.Mount
	requiresEnsure bool
}

func NewVolumeService(db *database.DB, dockerService *DockerClientService, eventService *EventService, activityService *ActivityService, settingsService *SettingsService, containerService *ContainerService, imageService *ImageService, rusticService *RusticService, s3Destinations *S3DestinationService, backupVolumeName, encryptionKey string) *VolumeService {
	slog.Debug("volume service: new")
	if strings.TrimSpace(backupVolumeName) == "" {
		backupVolumeName = "arcane-backups"
	}
	return &VolumeService{
		db:               db,
		dockerService:    dockerService,
		eventService:     eventService,
		activityService:  activityService,
		settingsService:  settingsService,
		containerService: containerService,
		imageService:     imageService,
		rusticService:    rusticService,
		s3Destinations:   s3Destinations,
		backupVolumeName: backupVolumeName,
		encryptionKey:    encryptionKey,
		helperByVolume:   make(map[string]*volumeHelper),
	}
}

func (s *VolumeService) GetVolumeByName(ctx context.Context, name string) (*volumetypes.Volume, error) {
	slog.DebugContext(ctx, "volume service: get volume", "volume", name)
	dockerClient, err := s.dockerService.GetClient(ctx)
	if err != nil {
		return nil, errors.WrapIf(err, "failed to connect to Docker")
	}

	volResult, err := dockerClient.VolumeInspect(ctx, name, client.VolumeInspectOptions{})
	if err != nil {
		return nil, errors.WrapIf(err, "volume not found")
	}
	vol := volResult.Volume

	settings := s.settingsService.GetSettingsConfig()
	usageCtx, usageCancel := context.WithTimeout(ctx, timeouts.GetDuration(settings.DockerAPITimeout.AsInt(), timeouts.DefaultDockerAPI))
	defer usageCancel()
	if usageVolumes, ok := docker.GetVolumeUsageDataStaleWhileRevalidate(usageCtx, dockerClient).Get(); ok {
		for _, uv := range usageVolumes {
			if uv.Name == vol.Name && uv.UsageData != nil {
				vol.UsageData = uv.UsageData
				slog.DebugContext(ctx, "attached volume usage data", "volume", vol.Name, "size_bytes", uv.UsageData.Size, "ref_count", uv.UsageData.RefCount)
				break
			}
		}
	}

	v := volumetypes.NewSummary(vol)

	containerIDs, err := docker.GetContainersUsingVolume(ctx, dockerClient, name)
	if err != nil {
		slog.WarnContext(ctx, "failed to get containers using volume", "volume", name, "error", err.Error())
	} else {
		v.Containers = containerIDs
		if len(containerIDs) > 0 {
			v.InUse = true
		}
	}

	return &v, nil
}

func (s *VolumeService) CreateVolume(ctx context.Context, options client.VolumeCreateOptions, user models.User) (*volumetypes.Volume, error) {
	slog.DebugContext(ctx, "volume service: create volume", "volume", options.Name, "driver", options.Driver, "user", user.ID)
	dockerClient, err := s.dockerService.GetClient(ctx)
	if err != nil {
		s.eventService.LogErrorEvent(ctx, models.EventTypeVolumeError, "volume", "", options.Name, user.ID, user.Username, "0", err, models.JSON{"action": "create", "driver": options.Driver})
		return nil, errors.WrapIf(err, "failed to connect to Docker")
	}

	created, err := dockerClient.VolumeCreate(ctx, options)
	if err != nil {
		s.eventService.LogErrorEvent(ctx, models.EventTypeVolumeError, "volume", "", options.Name, user.ID, user.Username, "0", err, models.JSON{"action": "create", "driver": options.Driver})
		return nil, errors.WrapIf(err, "failed to create volume")
	}

	vol, err := dockerClient.VolumeInspect(ctx, created.Volume.Name, client.VolumeInspectOptions{})
	if err != nil {
		s.eventService.LogErrorEvent(ctx, models.EventTypeVolumeError, "volume", created.Volume.Name, created.Volume.Name, user.ID, user.Username, "0", err, models.JSON{"action": "create", "driver": options.Driver, "step": "inspect"})
		return nil, errors.WrapIf(err, "failed to inspect created volume")
	}

	metadata := models.JSON{
		"action": "create",
		"driver": vol.Volume.Driver,
		"name":   vol.Volume.Name,
	}
	if logErr := s.eventService.LogVolumeEvent(ctx, models.EventTypeVolumeCreate, vol.Volume.Name, vol.Volume.Name, user.ID, user.Username, "0", metadata); logErr != nil {
		slog.WarnContext(ctx, "could not log volume creation action", "volume", vol.Volume.Name, "error", logErr.Error())
	}

	docker.InvalidateVolumeUsageCache(dockerClient)

	return new(volumetypes.NewSummary(vol.Volume)), nil
}

func (s *VolumeService) DeleteVolume(ctx context.Context, name string, force bool, user models.User) error {
	slog.DebugContext(ctx, "volume service: delete volume", "volume", name, "force", force, "user", user.ID)
	dockerClient, err := s.dockerService.GetClient(ctx)
	if err != nil {
		s.eventService.LogErrorEvent(ctx, models.EventTypeVolumeError, "volume", name, name, user.ID, user.Username, "0", err, models.JSON{"action": "delete", "force": force})
		return errors.WrapIf(err, "failed to connect to Docker")
	}

	// Stop any read-only browse helper first; a helper mounting the volume would
	// otherwise block a non-forced VolumeRemove with "volume is in use".
	if stopErr := s.StopHelper(ctx, name); stopErr != nil {
		slog.WarnContext(ctx, "could not stop volume browse helper before delete", "volume", name, "error", stopErr.Error())
	}

	if _, err := dockerClient.VolumeRemove(ctx, name, client.VolumeRemoveOptions{
		Force: force,
	}); err != nil {
		s.eventService.LogErrorEvent(ctx, models.EventTypeVolumeError, "volume", name, name, user.ID, user.Username, "0", err, models.JSON{"action": "delete", "force": force})
		return errors.WrapIf(err, "failed to remove volume")
	}

	metadata := models.JSON{
		"action": "delete",
		"name":   name,
	}
	if logErr := s.eventService.LogVolumeEvent(ctx, models.EventTypeVolumeDelete, name, name, user.ID, user.Username, "0", metadata); logErr != nil {
		slog.WarnContext(ctx, "could not log volume deletion action", "volume", name, "error", logErr.Error())
	}

	s.removeHelperEntry(name)
	s.removeVolumeBackupPolicyInternal(ctx, name)
	docker.InvalidateVolumeUsageCache(dockerClient)
	return nil
}

func (s *VolumeService) PruneVolumes(ctx context.Context) (*volumetypes.PruneReport, error) {
	slog.DebugContext(ctx, "volume service: prune volumes")
	return s.PruneVolumesWithOptions(ctx, false)
}

func (s *VolumeService) PruneVolumesWithOptions(ctx context.Context, all bool) (*volumetypes.PruneReport, error) {
	slog.DebugContext(ctx, "volume service: prune volumes with options", "all", all)
	dockerClient, err := s.dockerService.GetClient(ctx)
	if err != nil {
		return nil, errors.WrapIf(err, "failed to connect to Docker")
	}

	// Stop all read-only browse helpers first; a helper mounting a volume marks it
	// "in use" and would prevent VolumePrune from reclaiming an otherwise-unused
	// volume. Helpers are re-created on demand on the next browse request.
	s.CleanupHelperContainers(ctx)

	preserveTrivyCache := s.preserveTrivyCacheOnVolumePruneInternal()

	// Docker's VolumesPrune behavior (API v1.42+):
	// - Without 'all' flag: Only removes anonymous (unnamed) volumes that are not in use
	// - With 'all=true' flag: Removes ALL unused volumes (both named and anonymous)
	// Note: Volumes are considered "in use" if referenced by any container (running or stopped)
	volumePruneOptions := buildVolumePruneOptionsInternal(all, preserveTrivyCache)
	volumePruneResult, err := dockerClient.VolumePrune(ctx, volumePruneOptions)
	if err != nil {
		return nil, errors.WrapIf(err, "failed to prune volumes")
	}

	metadata := buildVolumePruneMetadataInternal(all, len(volumePruneResult.Report.VolumesDeleted), volumePruneResult.Report.SpaceReclaimed, preserveTrivyCache)
	if logErr := s.eventService.LogVolumeEvent(ctx, models.EventTypeVolumeDelete, "", "bulk_prune", systemUser.ID, systemUser.Username, "0", metadata); logErr != nil {
		slog.WarnContext(ctx, "could not log volume prune action", "error", logErr.Error())
	}

	for _, volumeName := range volumePruneResult.Report.VolumesDeleted {
		s.removeHelperEntry(volumeName)
		s.removeVolumeBackupPolicyInternal(ctx, volumeName)
	}

	docker.InvalidateVolumeUsageCache(dockerClient)

	return &volumetypes.PruneReport{
		VolumesDeleted: volumePruneResult.Report.VolumesDeleted,
		SpaceReclaimed: volumePruneResult.Report.SpaceReclaimed,
	}, nil
}

func (s *VolumeService) preserveTrivyCacheOnVolumePruneInternal() bool {
	if s.settingsService == nil {
		return true
	}

	return s.settingsService.GetSettingsConfig().TrivyPreserveCacheOnVolumePrune.IsTrue()
}

func buildVolumePruneOptionsInternal(all, preserveTrivyCache bool) client.VolumePruneOptions {
	options := client.VolumePruneOptions{
		All: all,
	}
	if !preserveTrivyCache {
		return options
	}

	filters := make(client.Filters)
	filters = filters.Add("label!", trivyCacheVolumePruneFilterValue)
	options.Filters = filters

	return options
}

func buildVolumePruneMetadataInternal(all bool, volumesDeleted int, spaceReclaimed uint64, preserveTrivyCache bool) models.JSON {
	return models.JSON{
		"action":                "prune",
		"all":                   all,
		"volumesDeleted":        volumesDeleted,
		"spaceReclaimed":        spaceReclaimed,
		"preserveTrivyCache":    preserveTrivyCache,
		"trivyCacheFilterLabel": trivyCacheVolumePruneFilterValue,
	}
}

// --- Volume Browsing & Backup ---

// isBrowsableVolumeInternal returns an error if the volume uses driver options
// that prevent it from being mounted inside a helper container, such as
// type=none or o=bind (host bind-mounts that require a device path on the host).
func (s *VolumeService) isBrowsableVolumeInternal(ctx context.Context, volumeName string) error {
	vol, err := s.GetVolumeByName(ctx, volumeName)
	if err != nil {
		return errors.WrapIf(err, "failed to inspect volume")
	}
	if vol.Options["type"] == "none" || strings.Contains(vol.Options["o"], "bind") {
		return errors.Errorf("volume %q uses a custom mount configuration and cannot be browsed", volumeName)
	}
	return nil
}

func (s *VolumeService) ListDirectory(ctx context.Context, volumeName, dirPath string) ([]volumetypes.FileEntry, error) {
	slog.DebugContext(ctx, "volume service: list directory", "volume", volumeName, "path", dirPath)

	if err := s.isBrowsableVolumeInternal(ctx, volumeName); err != nil {
		return nil, err
	}

	sanitizedPath, err := utils.SanitizeBrowsePath(dirPath)
	if err != nil {
		return nil, errors.WrapIf(err, "invalid path")
	}

	containerID, cleanup, err := s.createTempContainerInternal(ctx, volumeName, true)
	if err != nil {
		return nil, err
	}
	defer cleanup()

	targetPath := path.Join("/volume", sanitizedPath)
	cmd := []string{
		"sh",
		"-c",
		`find "$1" -mindepth 1 -maxdepth 1 | while IFS= read -r f; do out=$(stat -c "%s %Y %f %A" -- "$f" 2>/dev/null) || continue; printf "%s\0%s\0" "$f" "$out"; done`,
		"sh",
		targetPath,
	}
	stdout, _, err := s.execInContainerInternal(ctx, containerID, cmd)
	if err != nil {
		return nil, errors.WrapIf(err, "failed to list directory")
	}

	lines := strings.Split(stdout, "\x00")
	entries := make([]volumetypes.FileEntry, 0)
	for i := 0; i+1 < len(lines); i += 2 {
		fullPath := lines[i]
		meta := strings.Fields(strings.TrimSpace(lines[i+1]))
		if fullPath == "" || len(meta) < 4 {
			continue
		}
		name := path.Base(fullPath)
		size, _ := strconv.ParseInt(meta[0], 10, 64)
		modTimeSec, _ := strconv.ParseInt(meta[1], 10, 64)
		mode := meta[3]

		isDir := strings.HasPrefix(mode, "d")
		isSymlink := strings.HasPrefix(mode, "l")

		relPath := strings.TrimPrefix(fullPath, "/volume")
		if relPath == "" {
			relPath = "/"
		}

		entry := volumetypes.FileEntry{
			Name:        name,
			Path:        relPath,
			IsDirectory: isDir,
			Size:        size,
			ModTime:     time.Unix(modTimeSec, 0),
			Mode:        mode,
			IsSymlink:   isSymlink,
		}

		if isSymlink {
			// Use readlink without -f to get the raw symlink target (not resolved)
			// This prevents exposing paths outside the volume
			target, _, _ := s.execInContainerInternal(ctx, containerID, []string{"readlink", fullPath})
			target = strings.TrimSpace(target)
			if target != "" {
				// If target is relative, it's safe to show
				// If target is absolute and within /volume, strip the /volume prefix
				// If target points outside /volume, indicate it's external
				switch {
				case strings.HasPrefix(target, "/volume/"):
					entry.LinkTarget = strings.TrimPrefix(target, "/volume")
				case strings.HasPrefix(target, "/volume"):
					entry.LinkTarget = "/"
				case !strings.HasPrefix(target, "/"):
					// Relative path - safe to show as-is
					entry.LinkTarget = target
				default:
					// Absolute path outside /volume - indicate it's external
					entry.LinkTarget = "(external)"
				}
			}
		}

		entries = append(entries, entry)
	}

	return entries, nil
}

func (s *VolumeService) GetFileContent(ctx context.Context, volumeName, filePath string, maxBytes int64) ([]byte, string, error) {
	slog.DebugContext(ctx, "volume service: get file content", "volume", volumeName, "path", filePath, "max_bytes", maxBytes)

	if err := s.isBrowsableVolumeInternal(ctx, volumeName); err != nil {
		return nil, "", err
	}

	sanitizedPath, err := utils.SanitizeBrowsePath(filePath)
	if err != nil {
		return nil, "", errors.WrapIf(err, "invalid path")
	}

	containerID, cleanup, err := s.createTempContainerInternal(ctx, volumeName, true)
	if err != nil {
		return nil, "", err
	}
	defer cleanup()

	targetPath := path.Join("/volume", sanitizedPath)
	cmd := []string{"head", "-c", strconv.FormatInt(maxBytes, 10), targetPath}
	stdout, _, err := s.execInContainerInternal(ctx, containerID, cmd)
	if err != nil {
		return nil, "", errors.WrapIf(err, "failed to read file")
	}

	content := []byte(stdout)
	mimeType := http.DetectContentType(content)

	return content, mimeType, nil
}

func (s *VolumeService) DownloadFile(ctx context.Context, volumeName, filePath string) (io.ReadCloser, int64, error) {
	slog.DebugContext(ctx, "volume service: download file", "volume", volumeName, "path", filePath)

	if err := s.isBrowsableVolumeInternal(ctx, volumeName); err != nil {
		return nil, 0, err
	}

	sanitizedPath, err := utils.SanitizeBrowsePath(filePath)
	if err != nil {
		return nil, 0, errors.WrapIf(err, "invalid path")
	}

	dockerClient, err := s.dockerService.GetClient(ctx)
	if err != nil {
		return nil, 0, err
	}

	containerID, cleanup, err := s.createTempContainerInternal(ctx, volumeName, true)
	if err != nil {
		return nil, 0, err
	}

	targetPath := path.Join("/volume", sanitizedPath)
	return s.downloadFileFromContainerInternal(ctx, dockerClient, containerID, targetPath, cleanup)
}

func getVolumeHelperImageInternal(ctx context.Context, dockerService *DockerClientService, imageService *ImageService, dockerClient *client.Client) (string, error) {
	slog.DebugContext(ctx, "volume service: resolve helper image")
	var err error
	if dockerClient == nil {
		if dockerService == nil {
			return "", errors.New("docker service unavailable")
		}
		dockerClient, err = dockerService.GetClient(ctx)
		if err != nil {
			return "", errors.WrapIf(err, "failed to get docker client")
		}
	}

	if _, err := dockerClient.ImageInspect(ctx, volumeHelperImage); err == nil {
		slog.InfoContext(ctx, "volume service: helper image strategy selected", "strategy", "tools-local", "image", volumeHelperImage)
		return volumeHelperImage, nil
	}

	var pullErr error
	if imageService != nil {
		pullImageErr := imageService.PullImage(ctx, volumeHelperImage, io.Discard, systemUser, nil)
		if pullImageErr == nil {
			slog.InfoContext(ctx, "volume service: helper image strategy selected", "strategy", "tools-pulled", "image", volumeHelperImage)
			return volumeHelperImage, nil
		}
		pullErr = pullImageErr
		slog.WarnContext(ctx, "volume service: failed to pull tools helper image, attempting arcane fallback", "error", pullImageErr.Error())
	} else {
		pullErr = errors.New("image service unavailable")
		slog.WarnContext(ctx, "volume service: image service unavailable, attempting arcane fallback")
	}

	if fallback, ok := volumehelper.ResolveArcaneRuntimeImage(ctx, dockerClient).Get(); ok {
		slog.InfoContext(ctx, "volume service: helper image strategy selected", "strategy", "arcane-fallback", "source", fallback.Source, "image", fallback.Image)
		return fallback.Image, nil
	}

	return "", errors.WrapIf(pullErr, "failed to resolve helper image: tools image unavailable and arcane fallback not found")
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
		containerID := s.getArcaneContainerIDInternal(ctx, dockerClient)
		if containerID != "" {
			inspect, err := libarcane.ContainerInspectWithCompatibility(ctx, dockerClient, containerID, client.ContainerInspectOptions{})
			if err != nil {
				slog.WarnContext(ctx, "volume service: failed to inspect arcane container for backup mount resolution, falling back to named volume", "container_id", containerID, "error", err.Error())
			} else if resolved, ok := resolveBackupStorageMountFromMountsInternal(inspect.Container.Mounts, target, readOnly).Get(); ok {
				return resolved
			}
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

	containerID := s.getArcaneContainerIDInternal(ctx, dockerClient)
	if containerID == "" {
		// Cannot determine Arcane mount status (e.g. running outside Docker); suppress warning.
		return ""
	}

	inspect, err := libarcane.ContainerInspectWithCompatibility(ctx, dockerClient, containerID, client.ContainerInspectOptions{})
	if err != nil {
		return ""
	}

	return backupMountWarningFromArcaneMountsInternal(inspect.Container.Mounts)
}

func (s *VolumeService) getArcaneContainerIDInternal(ctx context.Context, dockerClient *client.Client) string {
	hostname, _ := os.Hostname()
	if hostname != "" {
		if inspect, err := libarcane.ContainerInspectWithCompatibility(ctx, dockerClient, hostname, client.ContainerInspectOptions{}); err == nil {
			return inspect.Container.ID
		}
	}

	filter := make(client.Filters)
	filter = filter.Add("label", "com.getarcaneapp.arcane=true")
	containers, err := dockerClient.ContainerList(ctx, client.ContainerListOptions{Filters: filter, All: true})
	if err != nil || len(containers.Items) == 0 {
		return ""
	}

	for _, c := range containers.Items {
		if c.State == container.StateRunning {
			return c.ID
		}
	}

	return containers.Items[0].ID
}

type cleanupReadCloser struct {
	io.Reader
	io.Closer

	cleanup func()
}

func isLegacyVolumeHelperContainerInternal(c container.Summary) bool {
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

func isVolumeHelperContainerInternal(c container.Summary) bool {
	if isLegacyVolumeHelperContainerInternal(c) {
		return true
	}
	if !libarcane.IsInternalContainer(c.Labels) {
		return false
	}

	return strings.EqualFold(c.Labels[volumehelper.ContainerLabel], "true")
}

func (c *cleanupReadCloser) Close() error {
	err := c.Closer.Close()
	c.cleanup()
	return err
}

func (s *VolumeService) createTempContainerInternal(ctx context.Context, volumeName string, readOnly bool) (string, func(), error) {
	slog.DebugContext(ctx, "volume service: create temp container", "volume", volumeName, "read_only", readOnly)
	dockerClient, err := s.dockerService.GetClient(ctx)
	if err != nil {
		return "", nil, err
	}

	if !readOnly {
		return s.startHelperContainerInternal(ctx, dockerClient, volumeName, false)
	}

	// Read-only helpers are shared and reaped when idle; the caller's cleanup
	// releases its in-use hold instead of removing the container. The resolve →
	// acquire gap is racy against the reaper, so retry a resolve whose helper
	// was reaped before this caller could take its hold.
	for range 3 {
		containerID, err := s.resolveReadOnlyHelperInternal(ctx, dockerClient, volumeName)
		if err != nil {
			return "", nil, err
		}
		if release, ok := s.acquireHelperInternal(volumeName, containerID); ok {
			return containerID, release, nil
		}
	}
	return "", nil, errors.New("failed to acquire volume helper container")
}

// resolveReadOnlyHelperInternal returns the shared read-only helper container ID
// for volumeName, creating it if needed. singleflight collapses concurrent
// misses: without it both requests create a helper and the loser is orphaned,
// holding the volume mount with no cleanup path until Arcane restarts. The
// creation itself can pull an image, so it must not run under helperMu.
func (s *VolumeService) resolveReadOnlyHelperInternal(ctx context.Context, dockerClient *client.Client, volumeName string) (string, error) {
	resultCh := s.helperGroup.DoChan(volumeName, func() (any, error) {
		if containerID, ok := s.getReusableReadOnlyContainerInternal(ctx, dockerClient, volumeName).Get(); ok {
			return containerID, nil
		}

		// Detached from the caller's context so a client that walks away
		// mid-create doesn't abort the helper the other waiters are blocked on,
		// but still bounded: WithoutCancel alone would let a hung daemon or
		// image pull wedge this singleflight key forever, queueing every later
		// browse of the volume behind it with no recovery until restart.
		createCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), timeouts.DefaultDockerImagePull)
		defer cancel()

		containerID, _, createErr := s.startHelperContainerInternal(createCtx, dockerClient, volumeName, true)
		if createErr != nil {
			return nil, createErr
		}

		s.helperMu.Lock()
		s.helperByVolume[volumeName] = &volumeHelper{id: containerID, lastUsedAt: time.Now()}
		s.helperMu.Unlock()
		return containerID, nil
	})

	// DoChan instead of Do so a caller whose request is canceled unblocks
	// immediately; the shared create keeps running for the remaining waiters
	// (and lands in helperByVolume for the next request even if none remain).
	var shared any
	select {
	case <-ctx.Done():
		return "", errors.WrapIf(ctx.Err(), "canceled waiting for volume helper container")
	case result := <-resultCh:
		if result.Err != nil {
			return "", result.Err
		}
		shared = result.Val
	}

	containerID, ok := shared.(string)
	if !ok || containerID == "" {
		return "", errors.New("failed to resolve volume helper container")
	}
	return containerID, nil
}

// acquireHelperInternal takes an in-use hold on volumeName's helper, reporting
// false when the helper has been removed (e.g. reaped) since it was resolved.
// The returned release drops the hold and refreshes the idle clock, so the idle
// timeout is measured from the end of the last request, not its start — a
// download running longer than the timeout used to have its helper reaped out
// from under it, truncating the stream.
func (s *VolumeService) acquireHelperInternal(volumeName, containerID string) (func(), bool) {
	s.helperMu.Lock()
	defer s.helperMu.Unlock()

	helper := s.helperByVolume[volumeName]
	if helper == nil || helper.id != containerID {
		return nil, false
	}

	helper.inUse++
	helper.lastUsedAt = time.Now()

	return func() {
		s.helperMu.Lock()
		defer s.helperMu.Unlock()
		if current := s.helperByVolume[volumeName]; current == helper {
			helper.inUse--
			helper.lastUsedAt = time.Now()
		}
	}, true
}

// startHelperContainerInternal creates and starts a volume helper container and
// returns a cleanup that removes it. Tracking of reusable read-only helpers is
// the caller's responsibility.
func (s *VolumeService) startHelperContainerInternal(ctx context.Context, dockerClient *client.Client, volumeName string, readOnly bool) (string, func(), error) {
	helperImage, err := getVolumeHelperImageInternal(ctx, s.dockerService, s.imageService, dockerClient)
	if err != nil {
		return "", nil, err
	}

	config := &container.Config{
		Image:           helperImage,
		Cmd:             []string{"sleep", "infinity"},
		NetworkDisabled: true,
		Labels:          volumehelper.Labels(),
	}

	hostConfig := volumehelper.HostConfig(helperImage, []string{
		fmt.Sprintf("%s:/volume%s", volumeName, func() string {
			if readOnly {
				return ":ro"
			}
			return ""
		}()),
	}, nil)

	resp, err := dockerClient.ContainerCreate(ctx, client.ContainerCreateOptions{
		Config:     config,
		HostConfig: hostConfig,
	})
	if err != nil {
		return "", nil, errors.WrapIf(err, "failed to create temp container")
	}

	if _, err := dockerClient.ContainerStart(ctx, resp.ID, client.ContainerStartOptions{}); err != nil {
		_, _ = dockerClient.ContainerRemove(ctx, resp.ID, volumehelper.RemoveOptions())
		return "", nil, errors.WrapIf(err, "failed to start temp container")
	}

	cleanup := func() {
		_, _ = dockerClient.ContainerRemove(context.WithoutCancel(ctx), resp.ID, volumehelper.RemoveOptions())
	}

	return resp.ID, cleanup, nil
}

func (s *VolumeService) getReusableReadOnlyContainerInternal(ctx context.Context, dockerClient *client.Client, volumeName string) mo.Option[string] {
	s.helperMu.Lock()
	helper := s.helperByVolume[volumeName]
	s.helperMu.Unlock()
	if helper == nil || helper.id == "" {
		return mo.None[string]()
	}

	inspect, err := libarcane.ContainerInspectWithCompatibility(ctx, dockerClient, helper.id, client.ContainerInspectOptions{})
	if err != nil || inspect.Container.State == nil || !inspect.Container.State.Running {
		s.helperMu.Lock()
		delete(s.helperByVolume, volumeName)
		s.helperMu.Unlock()
		return mo.None[string]()
	}

	s.touchHelperInternal(volumeName)

	return mo.Some(helper.id)
}

// touchHelperInternal records that the helper for volumeName just serviced a
// request, resetting its idle clock. No-op if the entry is gone.
func (s *VolumeService) touchHelperInternal(volumeName string) {
	s.helperMu.Lock()
	defer s.helperMu.Unlock()
	if helper := s.helperByVolume[volumeName]; helper != nil {
		helper.lastUsedAt = time.Now()
	}
}

func (s *VolumeService) CleanupHelperContainers(ctx context.Context) {
	dockerClient, err := s.dockerService.GetClient(ctx)
	if err != nil {
		slog.WarnContext(ctx, "failed to get docker client for helper cleanup", "error", err)
		return
	}

	s.helperMu.Lock()
	helperIDs := make([]string, 0, len(s.helperByVolume))
	for _, helper := range s.helperByVolume {
		if helper != nil && helper.id != "" {
			helperIDs = append(helperIDs, helper.id)
		}
	}
	s.helperByVolume = make(map[string]*volumeHelper)
	s.helperMu.Unlock()

	for _, containerID := range helperIDs {
		if _, err := dockerClient.ContainerRemove(ctx, containerID, volumehelper.RemoveOptions()); err != nil {
			slog.WarnContext(ctx, "failed to remove helper container", "container_id", containerID, "error", err.Error())
		}
	}
}

// ReapIdleHelpers removes reused read-only browse helper containers that have
// not serviced a request within idleTimeout. It is map-driven (orphaned helpers
// not tracked in helperByVolume are left to the startup orphan sweep). Entries
// are removed from the map before the container is removed, so a concurrent
// request simply gets a cache miss and re-creates a fresh helper.
func (s *VolumeService) ReapIdleHelpers(ctx context.Context, idleTimeout time.Duration) (int, error) {
	if idleTimeout <= 0 {
		return 0, nil
	}

	dockerClient, err := s.dockerService.GetClient(ctx)
	if err != nil {
		return 0, errors.WrapIf(err, "failed to get docker client for idle helper reap")
	}

	staleIDs := s.collectStaleHelperIDsInternal(time.Now(), idleTimeout)

	removed := 0
	for _, containerID := range staleIDs {
		if _, err := dockerClient.ContainerRemove(ctx, containerID, volumehelper.RemoveOptions()); err != nil {
			slog.WarnContext(ctx, "failed to remove idle helper container", "container_id", containerID, "error", err.Error())
			continue
		}
		removed++
	}

	return removed, nil
}

// collectStaleHelperIDsInternal removes idle (and any nil) entries from the helper
// map and returns the container IDs that should be removed. Pure map/mutex
// bookkeeping with no Docker calls, so it can be unit-tested directly. Entries are
// dropped before their containers are removed so a concurrent request gets a clean
// cache miss and re-creates a fresh helper.
func (s *VolumeService) collectStaleHelperIDsInternal(now time.Time, idleTimeout time.Duration) []string {
	staleIDs := make([]string, 0)
	s.helperMu.Lock()
	defer s.helperMu.Unlock()
	for volumeName, helper := range s.helperByVolume {
		if helper == nil {
			delete(s.helperByVolume, volumeName)
			continue
		}
		// A helper with active holds is serving a request right now (e.g. a
		// download streaming for longer than the idle timeout); it becomes
		// reapable once released, since release refreshes lastUsedAt.
		if helper.inUse > 0 {
			continue
		}
		if now.Sub(helper.lastUsedAt) >= idleTimeout {
			staleIDs = append(staleIDs, helper.id)
			delete(s.helperByVolume, volumeName)
		}
	}
	return staleIDs
}

// StopHelper removes the reused read-only browse helper for a single volume, if
// one exists. It is idempotent: stopping a volume with no active helper returns
// nil.
func (s *VolumeService) StopHelper(ctx context.Context, volumeName string) error {
	if strings.TrimSpace(volumeName) == "" {
		return nil
	}

	dockerClient, err := s.dockerService.GetClient(ctx)
	if err != nil {
		return errors.WrapIf(err, "failed to get docker client for helper stop")
	}

	containerID := s.takeHelperIDInternal(volumeName)
	if containerID == "" {
		return nil
	}

	if _, err := dockerClient.ContainerRemove(ctx, containerID, volumehelper.RemoveOptions()); err != nil {
		return errors.WrapIf(err, "failed to remove helper container")
	}

	return nil
}

// takeHelperIDInternal removes the helper entry for volumeName and returns its
// container ID, or "" if there was none. Pure map/mutex bookkeeping.
func (s *VolumeService) takeHelperIDInternal(volumeName string) string {
	s.helperMu.Lock()
	defer s.helperMu.Unlock()
	helper := s.helperByVolume[volumeName]
	delete(s.helperByVolume, volumeName)
	if helper == nil {
		return ""
	}
	return helper.id
}

func (s *VolumeService) CleanupOrphanedVolumeHelpers(ctx context.Context) (int, error) {
	slog.DebugContext(ctx, "volume service: cleanup orphaned volume helper containers")

	dockerClient, err := s.dockerService.GetClient(ctx)
	if err != nil {
		return 0, errors.WrapIf(err, "failed to get docker client for orphan helper cleanup")
	}

	containers, err := dockerClient.ContainerList(ctx, client.ContainerListOptions{All: true})
	if err != nil {
		return 0, errors.WrapIf(err, "failed to list containers for orphan helper cleanup")
	}

	removedCount := 0
	for _, c := range containers.Items {
		if !isVolumeHelperContainerInternal(c) {
			continue
		}

		if _, err := dockerClient.ContainerRemove(ctx, c.ID, volumehelper.RemoveOptions()); err != nil {
			slog.WarnContext(ctx,
				"volume service: failed to remove orphaned volume helper container",
				"container_id", c.ID,
				"container_names", c.Names,
				"error", err.Error(),
			)
			continue
		}

		removedCount++
	}

	return removedCount, nil
}

func (s *VolumeService) removeHelperEntry(volumeName string) {
	if strings.TrimSpace(volumeName) == "" {
		return
	}
	s.helperMu.Lock()
	delete(s.helperByVolume, volumeName)
	s.helperMu.Unlock()
}

func (s *VolumeService) execInContainerInternal(ctx context.Context, containerID string, cmd []string) (string, string, error) {
	slog.DebugContext(ctx, "volume service: exec in container", "container_id", containerID, "cmd", cmd)
	dockerClient, err := s.dockerService.GetClient(ctx)
	if err != nil {
		return "", "", err
	}

	execConfig := client.ExecCreateOptions{
		AttachStdout: true,
		AttachStderr: true,
		Cmd:          cmd,
	}

	execResp, err := dockerClient.ExecCreate(ctx, containerID, execConfig)
	if err != nil {
		return "", "", err
	}

	resp, err := dockerClient.ExecAttach(ctx, execResp.ID, client.ExecAttachOptions{})
	if err != nil {
		return "", "", err
	}
	defer resp.Close()

	var stdout, stderr bytes.Buffer
	_, err = stdcopy.StdCopy(&stdout, &stderr, resp.Reader)
	if err != nil {
		return "", "", err
	}

	inspect, err := dockerClient.ExecInspect(ctx, execResp.ID, client.ExecInspectOptions{})
	if err != nil {
		return stdout.String(), stderr.String(), errors.WrapIf(err, "failed to inspect exec result")
	}

	if inspect.ExitCode != 0 {
		execErr := strings.TrimSpace(stderr.String())
		if execErr == "" {
			execErr = strings.TrimSpace(stdout.String())
		}
		if execErr != "" {
			return stdout.String(), stderr.String(), errors.Errorf("command exited with code %d: %s", inspect.ExitCode, execErr)
		}
		return stdout.String(), stderr.String(), errors.Errorf("command exited with code %d", inspect.ExitCode)
	}

	return stdout.String(), stderr.String(), nil
}

func (s *VolumeService) DeleteFile(ctx context.Context, volumeName, filePath string, user *models.User) error {
	slog.DebugContext(ctx, "volume service: delete file", "volume", volumeName, "path", filePath)

	sanitizedPath, err := utils.SanitizeBrowsePath(filePath)
	if err != nil {
		return errors.WrapIf(err, "invalid path")
	}
	// Prevent deleting root
	if sanitizedPath == "/" {
		return errors.New("cannot delete root directory")
	}

	containerID, cleanup, err := s.createTempContainerInternal(ctx, volumeName, false)
	if err != nil {
		return err
	}
	defer cleanup()

	targetPath := path.Join("/volume", sanitizedPath)
	_, stderr, err := s.execInContainerInternal(ctx, containerID, []string{"rm", "-rf", targetPath})
	if err != nil {
		return err
	}
	if stderr != "" {
		return errors.Errorf("delete failed: %s", stderr)
	}

	actingUser := user
	if actingUser == nil {
		actingUser = &systemUser
	}
	metadata := models.JSON{
		"action": "file_delete",
		"path":   filePath,
	}
	if logErr := s.eventService.LogVolumeEvent(ctx, models.EventTypeVolumeFileDelete, volumeName, volumeName, actingUser.ID, actingUser.Username, "0", metadata); logErr != nil {
		slog.WarnContext(ctx, "could not log volume file delete event", "volume", volumeName, "error", logErr.Error())
	}
	return nil
}

func (s *VolumeService) CreateDirectory(ctx context.Context, volumeName, dirPath string, user *models.User) error {
	slog.DebugContext(ctx, "volume service: create directory", "volume", volumeName, "path", dirPath)

	sanitizedPath, err := utils.SanitizeBrowsePath(dirPath)
	if err != nil {
		return errors.WrapIf(err, "invalid path")
	}

	containerID, cleanup, err := s.createTempContainerInternal(ctx, volumeName, false)
	if err != nil {
		return err
	}
	defer cleanup()

	targetPath := path.Join("/volume", sanitizedPath)
	_, stderr, err := s.execInContainerInternal(ctx, containerID, []string{"mkdir", "-p", targetPath})
	if err != nil {
		return err
	}
	if stderr != "" {
		return errors.Errorf("mkdir failed: %s", stderr)
	}

	actingUser := user
	if actingUser == nil {
		actingUser = &systemUser
	}
	metadata := models.JSON{
		"action": "file_create",
		"path":   dirPath,
	}
	if logErr := s.eventService.LogVolumeEvent(ctx, models.EventTypeVolumeFileCreate, volumeName, volumeName, actingUser.ID, actingUser.Username, "0", metadata); logErr != nil {
		slog.WarnContext(ctx, "could not log volume file create event", "volume", volumeName, "error", logErr.Error())
	}
	return nil
}

func (s *VolumeService) UploadFile(ctx context.Context, volumeName, destPath string, content io.Reader, filename string, user *models.User) error {
	slog.DebugContext(ctx, "volume service: upload file", "volume", volumeName, "dest_path", destPath, "filename", filename)

	sanitizedPath, err := utils.SanitizeBrowsePath(destPath)
	if err != nil {
		return errors.WrapIf(err, "invalid path")
	}

	dockerClient, err := s.dockerService.GetClient(ctx)
	if err != nil {
		return err
	}

	containerID, cleanup, err := s.createTempContainerInternal(ctx, volumeName, false)
	if err != nil {
		return err
	}
	defer cleanup()

	var buf bytes.Buffer
	tw := tar.NewWriter(&buf)

	contentBytes, err := io.ReadAll(content)
	if err != nil {
		return err
	}

	hdr := &tar.Header{
		Name: filename,
		Mode: 0o644,
		Size: int64(len(contentBytes)),
	}
	if err := tw.WriteHeader(hdr); err != nil {
		return err
	}
	if _, err := tw.Write(contentBytes); err != nil {
		return err
	}
	if err := tw.Close(); err != nil {
		return err
	}

	targetDir := path.Join("/volume", sanitizedPath)
	_, err = dockerClient.CopyToContainer(ctx, containerID, client.CopyToContainerOptions{
		DestinationPath: targetDir,
		Content:         &buf,
	})
	if err != nil {
		return errors.WrapIf(err, "failed to upload")
	}

	actingUser := user
	if actingUser == nil {
		actingUser = &systemUser
	}
	metadata := models.JSON{
		"action":   "file_upload",
		"path":     destPath,
		"filename": filename,
	}
	if logErr := s.eventService.LogVolumeEvent(ctx, models.EventTypeVolumeFileUpload, volumeName, volumeName, actingUser.ID, actingUser.Username, "0", metadata); logErr != nil {
		slog.WarnContext(ctx, "could not log volume file upload event", "volume", volumeName, "error", logErr.Error())
	}

	return nil
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

func (s *VolumeService) GetVolumeUsage(ctx context.Context, name string) (bool, []string, error) {
	slog.DebugContext(ctx, "volume service: get volume usage", "volume", name)
	dockerClient, err := s.dockerService.GetClient(ctx)
	if err != nil {
		return false, nil, errors.WrapIf(err, "failed to connect to Docker")
	}

	vol, err := dockerClient.VolumeInspect(ctx, name, client.VolumeInspectOptions{})
	if err != nil {
		return false, nil, errors.WrapIf(err, "volume not found")
	}

	containerIDs, err := docker.GetContainersUsingVolume(ctx, dockerClient, vol.Volume.Name)
	if err != nil {
		return false, nil, errors.WrapIf(err, "failed to get containers using volume")
	}

	inUse := len(containerIDs) > 0
	return inUse, containerIDs, nil
}

// VolumeSizeData holds size information for a volume.
type VolumeSizeData struct {
	Size     int64
	RefCount int64
}

// GetVolumeSizes returns disk usage data for all volumes.
// This is a slow operation as it calls Docker's DiskUsage API.
func (s *VolumeService) GetVolumeSizes(ctx context.Context) (map[string]VolumeSizeData, error) {
	slog.DebugContext(ctx, "volume service: get volume sizes")
	settings := s.settingsService.GetSettingsConfig()
	apiCtx, cancel := context.WithTimeout(ctx, timeouts.GetDuration(settings.DockerAPITimeout.AsInt(), timeouts.DefaultDockerAPI))
	defer cancel()

	dockerClient, err := s.dockerService.GetClient(apiCtx)
	if err != nil {
		return nil, errors.WrapIf(err, "failed to connect to Docker")
	}

	usageVolumes, err := docker.GetVolumeUsageData(apiCtx, dockerClient)
	if err != nil {
		return nil, errors.WrapIf(err, "failed to get volume usage data")
	}

	result := make(map[string]VolumeSizeData, len(usageVolumes))
	for _, v := range usageVolumes {
		if v.UsageData != nil {
			result[v.Name] = VolumeSizeData{
				Size:     v.UsageData.Size,
				RefCount: v.UsageData.RefCount,
			}
		}
	}

	return result, nil
}

func (s *VolumeService) enrichVolumesWithUsageDataInternal(volumes []volume.Volume, usageVolumes []volume.Volume) []volume.Volume {
	usageByName := make(map[string]*volume.UsageData, len(usageVolumes))
	for _, uv := range usageVolumes {
		if uv.Name == "" || uv.UsageData == nil {
			continue
		}
		// Keep first-seen value to preserve previous nested-loop behavior.
		if _, exists := usageByName[uv.Name]; !exists {
			usageByName[uv.Name] = uv.UsageData
		}
	}

	result := make([]volume.Volume, 0, len(volumes))
	for _, v := range volumes {
		if usageData, exists := usageByName[v.Name]; exists {
			v.UsageData = usageData
		}

		result = append(result, v)
	}
	return result
}

func (s *VolumeService) buildVolumeContainerMapInternal(ctx context.Context, dockerClient *client.Client) (map[string][]string, error) {
	containers, err := dockerClient.ContainerList(ctx, client.ContainerListOptions{All: true})
	if err != nil {
		return nil, errors.WrapIf(err, "failed to list containers")
	}

	volumeContainerMap := make(map[string][]string)
	for _, c := range containers.Items {
		for _, m := range c.Mounts {
			if m.Type == mount.TypeVolume && m.Name != "" {
				volumeContainerMap[m.Name] = append(volumeContainerMap[m.Name], c.ID)
			}
		}
	}

	return volumeContainerMap, nil
}

func (s *VolumeService) buildVolumePaginationConfigInternal() pagination.Config[volumetypes.Volume] {
	return pagination.Config[volumetypes.Volume]{
		SearchAccessors: []pagination.SearchAccessor[volumetypes.Volume]{
			func(v volumetypes.Volume) (string, error) { return v.Name, nil },
			func(v volumetypes.Volume) (string, error) { return v.Driver, nil },
			func(v volumetypes.Volume) (string, error) { return v.Mountpoint, nil },
			func(v volumetypes.Volume) (string, error) { return v.Scope, nil },
		},
		SortBindings:    s.buildVolumeSortBindingsInternal(),
		FilterAccessors: s.buildVolumeFilterAccessorsInternal(),
	}
}

func (s *VolumeService) buildVolumeSortBindingsInternal() []pagination.SortBinding[volumetypes.Volume] {
	createdSortFn := s.compareVolumeCreatedInternal

	return []pagination.SortBinding[volumetypes.Volume]{
		{
			Key: "name",
			Fn:  func(a, b volumetypes.Volume) int { return strings.Compare(a.Name, b.Name) },
		},
		{
			Key: "driver",
			Fn:  func(a, b volumetypes.Volume) int { return strings.Compare(a.Driver, b.Driver) },
		},
		{
			Key: "mountpoint",
			Fn:  func(a, b volumetypes.Volume) int { return strings.Compare(a.Mountpoint, b.Mountpoint) },
		},
		{
			Key: "scope",
			Fn:  func(a, b volumetypes.Volume) int { return strings.Compare(a.Scope, b.Scope) },
		},
		{
			Key: "created",
			Fn:  createdSortFn,
		},
		{
			Key: "createdAt",
			Fn:  createdSortFn,
		},
		{
			Key: "inUse",
			Fn: func(a, b volumetypes.Volume) int {
				if a.InUse == b.InUse {
					return 0
				}
				if a.InUse {
					return -1
				}
				return 1
			},
		},
		{
			Key: "size",
			Fn:  s.compareVolumeSizesInternal,
		},
	}
}

func (s *VolumeService) compareVolumeSizesInternal(a, b volumetypes.Volume) int {
	aSize := a.Size
	bSize := b.Size

	if aSize == 0 && a.UsageData != nil {
		aSize = a.UsageData.Size
	}
	if bSize == 0 && b.UsageData != nil {
		bSize = b.UsageData.Size
	}

	if aSize == bSize {
		return strings.Compare(a.Name, b.Name)
	}
	if aSize < bSize {
		return -1
	}
	return 1
}

func (s *VolumeService) compareVolumeCreatedInternal(a, b volumetypes.Volume) int {
	aTime, aOk := s.parseVolumeCreatedAtInternal(a.CreatedAt).Get()
	bTime, bOk := s.parseVolumeCreatedAtInternal(b.CreatedAt).Get()
	if aOk && bOk {
		if aTime.Before(bTime) {
			return -1
		}
		if aTime.After(bTime) {
			return 1
		}
		return 0
	}
	return strings.Compare(a.CreatedAt, b.CreatedAt)
}

func (s *VolumeService) parseVolumeCreatedAtInternal(createdAt string) mo.Option[time.Time] {
	if createdAt == "" {
		return mo.None[time.Time]()
	}
	if parsed, err := time.Parse(time.RFC3339Nano, createdAt); err == nil {
		return mo.Some(parsed)
	}
	if parsed, err := time.Parse(time.RFC3339, createdAt); err == nil {
		return mo.Some(parsed)
	}
	return mo.None[time.Time]()
}

func (s *VolumeService) buildVolumeFilterAccessorsInternal() []pagination.FilterAccessor[volumetypes.Volume] {
	return []pagination.FilterAccessor[volumetypes.Volume]{
		{
			Key: "inUse",
			Fn: func(v volumetypes.Volume, filterValue string) bool {
				if filterValue == "true" {
					return v.InUse
				}
				if filterValue == "false" {
					return !v.InUse
				}
				return true
			},
		},
	}
}

func (s *VolumeService) calculateVolumeUsageCountsInternal(items []volumetypes.Volume) volumetypes.UsageCounts {
	counts := volumetypes.UsageCounts{
		Total: len(items),
	}
	for _, v := range items {
		if v.InUse {
			counts.Inuse++
		} else {
			counts.Unused++
		}
	}
	return counts
}

// countVolumeUsageFromSnapshotInternal derives volume usage counts from an
// already-fetched volume and container listing. It mirrors the semantics of
// ListVolumesPaginated — internal volumes are excluded and a volume counts as
// in use when any container mounts it — so dashboard tile numbers agree with
// the volumes page.
func (s *VolumeService) countVolumeUsageFromSnapshotInternal(volumes []volume.Volume, containers []container.Summary) volumetypes.UsageCounts {
	inUse := make(map[string]struct{})
	for _, c := range containers {
		for _, m := range c.Mounts {
			if m.Type == mount.TypeVolume && m.Name != "" {
				inUse[m.Name] = struct{}{}
			}
		}
	}

	counts := volumetypes.UsageCounts{}
	for _, v := range volumes {
		if s.isInternalVolumeInternal(volumetypes.NewSummary(v)) {
			continue
		}
		counts.Total++
		if _, ok := inUse[v.Name]; ok {
			counts.Inuse++
		} else {
			counts.Unused++
		}
	}
	return counts
}

func (s *VolumeService) isInternalVolumeInternal(v volumetypes.Volume) bool {
	if strings.EqualFold(strings.TrimSpace(v.Name), strings.TrimSpace(s.backupVolumeName)) {
		return true
	}

	return libarcane.IsInternalContainer(v.Labels)
}

func (s *VolumeService) ListVolumesPaginated(ctx context.Context, params pagination.QueryParams, includeInternal bool) ([]volumetypes.Volume, pagination.Response, volumetypes.UsageCounts, error) {
	startedAt := time.Now()
	slog.DebugContext(ctx, "volume service: list volumes paginated", "search", params.Search, "sort", params.Sort, "order", params.Order, "start", params.Start, "limit", params.Limit, "include_internal", includeInternal)
	dockerClient, err := s.dockerService.GetClient(ctx)
	if err != nil {
		return nil, pagination.Response{}, volumetypes.UsageCounts{}, errors.WrapIf(err, "failed to connect to Docker")
	}

	// Run volume list and container list in parallel for better performance
	type volumeListResult struct {
		volumes []volume.Volume
		err     error
	}
	type containerMapResult struct {
		containerMap map[string][]string
		err          error
	}

	volChan := make(chan volumeListResult, 1)
	containerChan := make(chan containerMapResult, 1)

	settings := s.settingsService.GetSettingsConfig()
	apiCtx, cancel := context.WithTimeout(ctx, timeouts.GetDuration(settings.DockerAPITimeout.AsInt(), timeouts.DefaultDockerAPI))
	defer cancel()

	go func(ctx context.Context) {
		volListBody, err := dockerClient.VolumeList(ctx, client.VolumeListOptions{})
		volChan <- volumeListResult{volumes: volListBody.Items, err: err}
	}(apiCtx)

	go func(ctx context.Context) {
		containerMap, err := s.buildVolumeContainerMapInternal(ctx, dockerClient)
		containerChan <- containerMapResult{containerMap: containerMap, err: err}
	}(apiCtx)

	// Wait for both results
	volResult := <-volChan
	if volResult.err != nil {
		return nil, pagination.Response{}, volumetypes.UsageCounts{}, errors.WrapIf(volResult.err, "failed to list Docker volumes")
	}

	containerResult := <-containerChan
	volumeContainerMap := containerResult.containerMap
	if containerResult.err != nil {
		slog.WarnContext(ctx, "failed to build volume-container map", "error", containerResult.err.Error())
		volumeContainerMap = make(map[string][]string)
	}

	effectiveParams := params
	usageCacheSnapshot := "not_requested"

	// Size sorting consumes the current cache snapshot and refreshes it in the
	// background so this list request never waits for Docker's DiskUsage call.
	var usageVolumes []volume.Volume
	if params.Sort == "size" {
		if uv, found := docker.GetVolumeUsageDataStaleWhileRevalidate(apiCtx, dockerClient).Get(); found && (len(uv) > 0 || len(volResult.volumes) == 0) {
			usageVolumes = uv
			usageCacheSnapshot = "available"
		} else {
			usageCacheSnapshot = "missing"
			effectiveParams.Sort = "name"
			effectiveParams.Order = pagination.SortAsc
		}
	}

	volumes := s.enrichVolumesWithUsageDataInternal(volResult.volumes, usageVolumes)

	items := make([]volumetypes.Volume, 0, len(volumes))
	for _, v := range volumes {
		volDto := volumetypes.NewSummary(v)
		if !includeInternal && s.isInternalVolumeInternal(volDto) {
			continue
		}
		if containerIDs, ok := volumeContainerMap[v.Name]; ok {
			volDto.Containers = containerIDs
			if len(containerIDs) > 0 {
				volDto.InUse = true
			}
		}
		items = append(items, volDto)
	}

	config := s.buildVolumePaginationConfigInternal()
	result := pagination.SearchOrderAndPaginate(items, effectiveParams, config)
	counts := s.calculateVolumeUsageCountsInternal(items)
	paginationResp := pagination.BuildResponseFromFilterResult(result, effectiveParams)
	slog.DebugContext(ctx, "volume service: listed volumes",
		"docker_host", dockerClient.DaemonHost(),
		"requested_sort", params.Sort,
		"requested_order", params.Order,
		"effective_sort", effectiveParams.Sort,
		"effective_order", effectiveParams.Order,
		"usage_cache_snapshot", usageCacheSnapshot,
		"docker_volumes", len(volResult.volumes),
		"usage_volumes", len(usageVolumes),
		"included_volumes", len(items),
		"matched_volumes", result.TotalCount,
		"returned_volumes", len(result.Items),
		"container_volume_count", len(volumeContainerMap),
		"filter_count", len(params.Filters),
		"current_page", paginationResp.CurrentPage,
		"total_pages", paginationResp.TotalPages,
		"duration", time.Since(startedAt),
	)

	return result.Items, paginationResp, counts, nil
}

func (s *VolumeService) downloadFileFromContainerInternal(
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

	return &cleanupReadCloser{
		Reader:  tr,
		Closer:  reader,
		cleanup: cleanup,
	}, hdr.Size, nil
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
	configuration, err := s.s3Destinations.configurationInternal(ctx, destinationID)
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
	output, err := s.rusticService.runInternal(ctx, dockerClient, repository, s.rusticPasswordInternal(),
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
	_, err := s.rusticService.runInternal(ctx, dockerClient, repository, s.rusticPasswordInternal(), command,
		mount.Mount{Type: mount.TypeVolume, Source: volumeName, Target: "/volume"},
	)
	return err
}

func (s *VolumeService) forgetRusticSnapshotInternal(ctx context.Context, dockerClient *client.Client, repository rusticRepositoryInternal, snapshotID string) error {
	_, err := s.rusticService.runInternal(ctx, dockerClient, repository, s.rusticPasswordInternal(), []string{"forget", "--prune", snapshotID})
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
		if _, err := s.s3Destinations.configurationInternal(ctx, s3DestinationID); err != nil {
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
	if _, err := s.s3Destinations.configurationInternal(ctx, s3DestinationID); err != nil {
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
		actingUser = &systemUser
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
	output, err := s.rusticService.runInternal(ctx, dockerClient, repository, s.rusticPasswordInternal(), []string{"ls", "--json", "--recursive", snapshotID + ":/"})
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

func normalizeVolumeBackupScheduleInternal(schedule string) (string, error) {
	parser := cron.NewParser(cron.Second | cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow | cron.Descriptor)
	fields := strings.Fields(schedule)
	if len(fields) == 0 {
		return "", errors.New("volume backup schedule is required")
	}
	if len(fields) != 6 {
		return "", fmt.Errorf("invalid volume backup schedule %q: expected six fields", strings.TrimSpace(schedule))
	}
	schedule = strings.Join(fields, " ")
	if _, err := parser.Parse(schedule); err != nil {
		return "", fmt.Errorf("invalid volume backup schedule %q: %w", schedule, err)
	}
	return schedule, nil
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
		schedule, err := normalizeVolumeBackupScheduleInternal(updates[i].Schedule)
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
			if _, err := s.s3Destinations.configurationInternal(ctx, updates[i].S3DestinationID); err != nil {
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
				User:           &systemUser,
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
				backup, backupErr = s.CreateBackup(activityCtx, policy.VolumeName, systemUser, models.VolumeBackupTriggerScheduled, volumetypes.CreateBackupRequest{PolicyID: policy.ID})
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
