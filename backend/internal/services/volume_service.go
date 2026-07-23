package services

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
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

	"github.com/getarcaneapp/arcane/backend/v2/internal/database"
	"github.com/getarcaneapp/arcane/backend/v2/internal/models"
	docker "github.com/getarcaneapp/arcane/backend/v2/pkg/dockerutil"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/libarcane"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/libarcane/timeouts"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/libarcane/volumehelper"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/pagination"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/utils"
	volumetypes "github.com/getarcaneapp/arcane/types/v2/volume"
	"github.com/google/uuid"
	"github.com/moby/moby/api/pkg/stdcopy"
	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/api/types/mount"
	"github.com/moby/moby/api/types/volume"
	"github.com/moby/moby/client"
	"github.com/samber/mo"
)

type VolumeService struct {
	db               *database.DB
	dockerService    *DockerClientService
	eventService     *EventService
	settingsService  *SettingsService
	containerService *ContainerService
	imageService     *ImageService
	backupVolumeName string
	helperMu         sync.Mutex
	helperByVolume   map[string]*volumeHelper
}

// volumeHelper tracks a reused read-only browse helper container and the last
// time it serviced a request, so idle helpers can be reaped.
type volumeHelper struct {
	id         string
	lastUsedAt time.Time
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

type backupStorageMountInternal struct {
	mode           backupStorageMode
	mount          mount.Mount
	requiresEnsure bool
}

func NewVolumeService(db *database.DB, dockerService *DockerClientService, eventService *EventService, settingsService *SettingsService, containerService *ContainerService, imageService *ImageService, backupVolumeName string) *VolumeService {
	slog.Debug("volume service: new")
	if strings.TrimSpace(backupVolumeName) == "" {
		backupVolumeName = "arcane-backups"
	}
	return &VolumeService{
		db:               db,
		dockerService:    dockerService,
		eventService:     eventService,
		settingsService:  settingsService,
		containerService: containerService,
		imageService:     imageService,
		backupVolumeName: backupVolumeName,
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
	vol := volResult.Volume
	if err != nil {
		return nil, errors.WrapIf(err, "volume not found")
	}

	settings := s.settingsService.GetSettingsConfig()
	usageCtx, usageCancel := timeouts.WithTimeout(ctx, settings.DockerAPITimeout.AsInt(), timeouts.DefaultDockerAPI)
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
	quotedPath := strconv.Quote(targetPath)
	cmd := []string{"sh", "-c", fmt.Sprintf("find %s -mindepth 1 -maxdepth 1 | while IFS= read -r f; do out=$(stat -c \"%%s %%Y %%f %%A\" -- \"$f\" 2>/dev/null) || continue; printf \"%%s\\0%%s\\0\" \"$f\" \"$out\"; done", quotedPath)}
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

	if readOnly {
		if containerID, ok := s.getReusableReadOnlyContainerInternal(ctx, dockerClient, volumeName).Get(); ok {
			return containerID, func() {}, nil
		}
	}

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
		_, _ = dockerClient.ContainerRemove(ctx, resp.ID, volumehelper.RemoveOptions())
	}

	if readOnly {
		s.helperMu.Lock()
		s.helperByVolume[volumeName] = &volumeHelper{id: resp.ID, lastUsedAt: time.Now()}
		s.helperMu.Unlock()
		return resp.ID, func() {}, nil
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
		_ = tw.Close()
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

func (s *VolumeService) CreateBackup(ctx context.Context, volumeName string, user models.User) (*models.VolumeBackup, error) {
	slog.DebugContext(ctx, "volume service: create backup", "volume", volumeName, "user", user.ID)
	dockerClient, err := s.dockerService.GetClient(ctx)
	if err != nil {
		return nil, err
	}

	backupID := fmt.Sprintf("%s-%d-%s", volumeName, time.Now().UnixNano(), uuid.NewString()[:8])
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

	backup := &models.VolumeBackup{
		VolumeName: volumeName,
		Size:       size,
		CreatedAt:  time.Now(),
	}
	backup.ID = backupID

	if err := s.db.WithContext(ctx).Create(backup).Error; err != nil {
		return nil, err
	}

	metadata := models.JSON{
		"action":    "backup_create",
		"backup_id": backup.ID,
		"filename":  filename,
		"size":      size,
	}
	if logErr := s.eventService.LogVolumeEvent(ctx, models.EventTypeVolumeBackupCreate, volumeName, volumeName, user.ID, user.Username, "0", metadata); logErr != nil {
		slog.WarnContext(ctx, "could not log volume backup create event", "volume", volumeName, "error", logErr.Error())
	}

	return backup, nil
}

func (s *VolumeService) ListBackupsPaginated(ctx context.Context, volumeName string, params pagination.QueryParams) ([]models.VolumeBackup, pagination.Response, error) {
	slog.DebugContext(ctx, "volume service: list backups paginated", "volume", volumeName, "search", params.Search, "sort", params.Sort, "order", params.Order, "start", params.Start, "limit", params.Limit)
	var backups []models.VolumeBackup
	query := s.db.WithContext(ctx).Model(&models.VolumeBackup{}).Where("volume_name = ?", volumeName)

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

func (s *VolumeService) DeleteBackup(ctx context.Context, backupID string, user *models.User) error {
	slog.DebugContext(ctx, "volume service: delete backup", "backup_id", backupID)
	var backup models.VolumeBackup
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
		actingUser = &systemUser
	}
	metadata := models.JSON{
		"action":    "backup_delete",
		"backup_id": backupID,
	}
	if logErr := s.eventService.LogVolumeEvent(ctx, models.EventTypeVolumeBackupDelete, volumeName, volumeName, actingUser.ID, actingUser.Username, "0", metadata); logErr != nil {
		slog.WarnContext(ctx, "could not log volume backup delete event", "volume", volumeName, "error", logErr.Error())
	}

	return nil
}

func (s *VolumeService) RestoreBackup(ctx context.Context, volumeName, backupID string, user models.User) error {
	slog.DebugContext(ctx, "volume service: restore backup", "volume", volumeName, "backup_id", backupID, "user", user.ID)
	var backup models.VolumeBackup
	if err := s.db.WithContext(ctx).Where("id = ?", backupID).First(&backup).Error; err != nil {
		return err
	}

	// Validate backup belongs to volume
	if backup.VolumeName != volumeName {
		return errors.Errorf("backup does not belong to volume %s", volumeName)
	}

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

	metadata := models.JSON{
		"action":               "backup_restore",
		"backup_id":            backupID,
		"pre_restore_backupId": preBackup.ID,
	}
	if logErr := s.eventService.LogVolumeEvent(ctx, models.EventTypeVolumeBackupRestore, volumeName, volumeName, user.ID, user.Username, "0", metadata); logErr != nil {
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

	var backup models.VolumeBackup
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

	var backup models.VolumeBackup
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

func (s *VolumeService) RestoreBackupFiles(ctx context.Context, volumeName, backupID string, paths []string, user models.User) error {
	slog.DebugContext(ctx, "volume service: restore backup files", "volume", volumeName, "backup_id", backupID, "paths_count", len(paths), "user", user.ID)
	if len(paths) == 0 {
		return errors.New("no paths provided")
	}
	filename, err := s.backupArchiveFilenameInternal(backupID)
	if err != nil {
		return err
	}

	var backup models.VolumeBackup
	if err := s.db.WithContext(ctx).Where("id = ?", backupID).First(&backup).Error; err != nil {
		return err
	}
	if backup.VolumeName != volumeName {
		return errors.New("backup does not belong to volume")
	}

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

	tarPaths := make([]string, 0, len(cleanedPaths))
	for _, p := range cleanedPaths {
		tarPaths = append(tarPaths, "./"+p)
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

	cmd := append([]string{"tar", "-xzf", path.Join("/backups", filename), "-C", "/volume", "--"}, tarPaths...)
	_, stderr, err := s.execInContainerInternal(ctx, resp.ID, cmd)
	if err != nil {
		return errors.WrapIf(err, "failed to restore files")
	}
	if strings.TrimSpace(stderr) != "" {
		slog.DebugContext(ctx, "volume service: restore files stderr", "backup_id", backupID, "stderr", strings.TrimSpace(stderr))
	}

	metadata := models.JSON{
		"action":               "backup_restore_files",
		"backup_id":            backupID,
		"pre_restore_backupId": preBackup.ID,
		"paths_count":          len(cleanedPaths),
	}
	if len(cleanedPaths) > 0 {
		limit := min(len(cleanedPaths), 5)
		metadata["paths_sample"] = cleanedPaths[:limit]
	}
	if logErr := s.eventService.LogVolumeEvent(ctx, models.EventTypeVolumeBackupRestoreFiles, volumeName, volumeName, user.ID, user.Username, "0", metadata); logErr != nil {
		slog.WarnContext(ctx, "could not log volume backup restore files event", "volume", volumeName, "error", logErr.Error())
	}

	return nil
}

func (s *VolumeService) DownloadBackup(ctx context.Context, backupID string, user *models.User) (io.ReadCloser, int64, error) {
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

	reader, size, err := s.downloadFileFromContainerInternal(ctx, dockerClient, containerID, path.Join("/volume", filename), cleanup)
	if err != nil {
		return nil, 0, err
	}

	actingUser := user
	if actingUser == nil {
		actingUser = &systemUser
	}
	volumeName := ""
	var backup models.VolumeBackup
	if err := s.db.WithContext(ctx).Where("id = ?", backupID).First(&backup).Error; err == nil {
		volumeName = backup.VolumeName
	}
	if volumeName != "" {
		metadata := models.JSON{
			"action":    "backup_download",
			"backup_id": backupID,
			"size":      size,
		}
		if logErr := s.eventService.LogVolumeEvent(ctx, models.EventTypeVolumeBackupDownload, volumeName, volumeName, actingUser.ID, actingUser.Username, "0", metadata); logErr != nil {
			slog.WarnContext(ctx, "could not log volume backup download event", "volume", volumeName, "error", logErr.Error())
		}
	}

	return reader, size, nil
}

func (s *VolumeService) UploadAndRestore(ctx context.Context, volumeName string, archive io.Reader, filename string, user models.User) error {
	slog.DebugContext(ctx, "volume service: upload and restore", "volume", volumeName, "filename", filename, "user", user.ID)

	tmpFile, err := os.CreateTemp("", "arcane-restore-*.tar.gz")
	if err != nil {
		return errors.WrapIf(err, "failed to buffer upload")
	}
	defer func() {
		_ = tmpFile.Close()
		_ = os.Remove(tmpFile.Name())
	}()
	if _, err := io.Copy(tmpFile, archive); err != nil {
		return errors.WrapIf(err, "failed to buffer upload")
	}
	if _, err := tmpFile.Seek(0, io.SeekStart); err != nil {
		return errors.WrapIf(err, "failed to read buffered upload")
	}
	gzr, err := gzip.NewReader(tmpFile)
	if err != nil {
		return errors.WrapIf(err, "invalid archive")
	}
	if _, err := tar.NewReader(gzr).Next(); err != nil {
		_ = gzr.Close()
		return errors.WrapIf(err, "invalid archive")
	}
	_ = gzr.Close()

	preBackup, err := s.CreateBackup(ctx, volumeName, user)
	if err != nil {
		return errors.WrapIf(err, "failed to create pre-restore backup")
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

	tmpDir := fmt.Sprintf("/volume/.restore_tmp_%d", time.Now().UnixNano())
	_, stderr, err := s.execInContainerInternal(ctx, containerID, []string{"mkdir", "-p", tmpDir})
	if err != nil {
		return errors.WrapIf(err, "failed to create temp restore dir")
	}
	if strings.TrimSpace(stderr) != "" {
		slog.DebugContext(ctx, "volume service: restore temp dir stderr", "volume", volumeName, "stderr", strings.TrimSpace(stderr))
	}

	if _, err := tmpFile.Seek(0, io.SeekStart); err != nil {
		return errors.WrapIf(err, "failed to read buffered upload")
	}
	_, err = dockerClient.CopyToContainer(ctx, containerID, client.CopyToContainerOptions{
		DestinationPath: tmpDir,
		Content:         tmpFile,
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

	metadata := models.JSON{
		"action":               "backup_upload_restore",
		"filename":             filename,
		"pre_restore_backupId": preBackup.ID,
	}
	if logErr := s.eventService.LogVolumeEvent(ctx, models.EventTypeVolumeBackupRestore, volumeName, volumeName, user.ID, user.Username, "0", metadata); logErr != nil {
		slog.WarnContext(ctx, "could not log volume backup upload restore event", "volume", volumeName, "error", logErr.Error())
	}

	return nil
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
	apiCtx, cancel := timeouts.WithTimeout(ctx, settings.DockerAPITimeout.AsInt(), timeouts.DefaultDockerAPI)
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
	apiCtx, cancel := timeouts.WithTimeout(ctx, settings.DockerAPITimeout.AsInt(), timeouts.DefaultDockerAPI)
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
