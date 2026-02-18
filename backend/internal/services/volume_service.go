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
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/filters"
	"github.com/docker/docker/api/types/mount"
	"github.com/docker/docker/api/types/volume"
	"github.com/docker/docker/client"
	"github.com/docker/docker/pkg/stdcopy"
	"github.com/getarcaneapp/arcane/backend/internal/database"
	"github.com/getarcaneapp/arcane/backend/internal/models"
	"github.com/getarcaneapp/arcane/backend/internal/utils/docker"
	"github.com/getarcaneapp/arcane/backend/internal/utils/pagination"
	"github.com/getarcaneapp/arcane/backend/internal/utils/timeouts"
	"github.com/getarcaneapp/arcane/backend/pkg/libarcane"
	volumetypes "github.com/getarcaneapp/arcane/types/volume"
	"github.com/google/uuid"
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
	helperByVolume   map[string]string
}

const volumeHelperImage = "busybox:stable-musl"

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
		helperByVolume:   make(map[string]string),
	}
}

func (s *VolumeService) GetVolumeByName(ctx context.Context, name string) (*volumetypes.Volume, error) {
	slog.DebugContext(ctx, "volume service: get volume", "volume", name)
	dockerClient, err := s.dockerService.GetClient()
	if err != nil {
		return nil, fmt.Errorf("failed to connect to Docker: %w", err)
	}

	vol, err := dockerClient.VolumeInspect(ctx, name)
	if err != nil {
		return nil, fmt.Errorf("volume not found: %w", err)
	}

	if usageVolumes, duErr := docker.GetVolumeUsageData(ctx, dockerClient); duErr == nil {
		for _, uv := range usageVolumes {
			if uv.Name == vol.Name && uv.UsageData != nil {
				vol.UsageData = uv.UsageData
				slog.DebugContext(ctx, "attached volume usage data", "volume", vol.Name, "size_bytes", uv.UsageData.Size, "ref_count", uv.UsageData.RefCount)
				break
			}
		}
	} else {
		slog.WarnContext(ctx, "failed to load volume usage data", "volume", vol.Name, "error", duErr.Error())
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

func (s *VolumeService) CreateVolume(ctx context.Context, options volume.CreateOptions, user models.User) (*volumetypes.Volume, error) {
	slog.DebugContext(ctx, "volume service: create volume", "volume", options.Name, "driver", options.Driver, "user", user.ID)
	dockerClient, err := s.dockerService.GetClient()
	if err != nil {
		s.eventService.LogErrorEvent(ctx, models.EventTypeVolumeError, "volume", "", options.Name, user.ID, user.Username, "0", err, models.JSON{"action": "create", "driver": options.Driver})
		return nil, fmt.Errorf("failed to connect to Docker: %w", err)
	}

	created, err := dockerClient.VolumeCreate(ctx, options)
	if err != nil {
		s.eventService.LogErrorEvent(ctx, models.EventTypeVolumeError, "volume", "", options.Name, user.ID, user.Username, "0", err, models.JSON{"action": "create", "driver": options.Driver})
		return nil, fmt.Errorf("failed to create volume: %w", err)
	}

	vol, err := dockerClient.VolumeInspect(ctx, created.Name)
	if err != nil {
		s.eventService.LogErrorEvent(ctx, models.EventTypeVolumeError, "volume", created.Name, created.Name, user.ID, user.Username, "0", err, models.JSON{"action": "create", "driver": options.Driver, "step": "inspect"})
		return nil, fmt.Errorf("failed to inspect created volume: %w", err)
	}

	metadata := models.JSON{
		"action": "create",
		"driver": vol.Driver,
		"name":   vol.Name,
	}
	if logErr := s.eventService.LogVolumeEvent(ctx, models.EventTypeVolumeCreate, vol.Name, vol.Name, user.ID, user.Username, "0", metadata); logErr != nil {
		slog.WarnContext(ctx, "could not log volume creation action", "volume", vol.Name, "error", logErr.Error())
	}

	docker.InvalidateVolumeUsageCache()

	dtoVol := volumetypes.NewSummary(vol)
	return &dtoVol, nil
}

func (s *VolumeService) DeleteVolume(ctx context.Context, name string, force bool, user models.User) error {
	slog.DebugContext(ctx, "volume service: delete volume", "volume", name, "force", force, "user", user.ID)
	dockerClient, err := s.dockerService.GetClient()
	if err != nil {
		s.eventService.LogErrorEvent(ctx, models.EventTypeVolumeError, "volume", name, name, user.ID, user.Username, "0", err, models.JSON{"action": "delete", "force": force})
		return fmt.Errorf("failed to connect to Docker: %w", err)
	}

	if err := dockerClient.VolumeRemove(ctx, name, force); err != nil {
		s.eventService.LogErrorEvent(ctx, models.EventTypeVolumeError, "volume", name, name, user.ID, user.Username, "0", err, models.JSON{"action": "delete", "force": force})
		return fmt.Errorf("failed to remove volume: %w", err)
	}

	metadata := models.JSON{
		"action": "delete",
		"name":   name,
	}
	if logErr := s.eventService.LogVolumeEvent(ctx, models.EventTypeVolumeDelete, name, name, user.ID, user.Username, "0", metadata); logErr != nil {
		slog.WarnContext(ctx, "could not log volume deletion action", "volume", name, "error", logErr.Error())
	}

	s.removeHelperEntry(name)
	return nil
}

func (s *VolumeService) PruneVolumes(ctx context.Context) (*volumetypes.PruneReport, error) {
	slog.DebugContext(ctx, "volume service: prune volumes")
	return s.PruneVolumesWithOptions(ctx, false)
}

func (s *VolumeService) PruneVolumesWithOptions(ctx context.Context, all bool) (*volumetypes.PruneReport, error) {
	slog.DebugContext(ctx, "volume service: prune volumes with options", "all", all)
	dockerClient, err := s.dockerService.GetClient()
	if err != nil {
		return nil, fmt.Errorf("failed to connect to Docker: %w", err)
	}

	// Docker's VolumesPrune behavior (API v1.42+):
	// - Without 'all' flag: Only removes anonymous (unnamed) volumes that are not in use
	// - With 'all=true' flag: Removes ALL unused volumes (both named and anonymous)
	// Note: Volumes are considered "in use" if referenced by any container (running or stopped)
	filterArgs := filters.NewArgs()
	if all {
		// The 'all' filter was added in Docker API v1.42
		// This tells Docker to prune ALL unused volumes, not just anonymous ones
		filterArgs.Add("all", "true")
	}
	// Other valid filters for volume prune:
	// - label=<key> or label=<key>=<value>
	// - label!=<key> or label!=<key>=<value>

	report, err := dockerClient.VolumesPrune(ctx, filterArgs)
	if err != nil {
		return nil, fmt.Errorf("failed to prune volumes: %w", err)
	}

	metadata := models.JSON{
		"action":         "prune",
		"all":            all,
		"volumesDeleted": len(report.VolumesDeleted),
		"spaceReclaimed": report.SpaceReclaimed,
	}
	if logErr := s.eventService.LogVolumeEvent(ctx, models.EventTypeVolumeDelete, "", "bulk_prune", systemUser.ID, systemUser.Username, "0", metadata); logErr != nil {
		slog.WarnContext(ctx, "could not log volume prune action", "error", logErr.Error())
	}

	for _, volumeName := range report.VolumesDeleted {
		s.removeHelperEntry(volumeName)
	}

	docker.InvalidateVolumeUsageCache()

	return &volumetypes.PruneReport{
		VolumesDeleted: report.VolumesDeleted,
		SpaceReclaimed: report.SpaceReclaimed,
	}, nil
}

// --- Volume Browsing & Backup ---

func (s *VolumeService) ListDirectory(ctx context.Context, volumeName, dirPath string) ([]volumetypes.FileEntry, error) {
	slog.DebugContext(ctx, "volume service: list directory", "volume", volumeName, "path", dirPath)

	sanitizedPath, err := s.sanitizeBrowsePathInternal(dirPath)
	if err != nil {
		return nil, fmt.Errorf("invalid path: %w", err)
	}

	containerID, cleanup, err := s.createTempContainerInternal(ctx, volumeName, true)
	if err != nil {
		return nil, err
	}
	defer cleanup()

	targetPath := path.Join("/volume", sanitizedPath)
	quotedPath := strconv.Quote(targetPath)
	cmd := []string{"sh", "-c", fmt.Sprintf("find %s -mindepth 1 -maxdepth 1 -exec sh -c 'for f; do out=$(stat -c \"%%s %%Y %%f %%A\" -- \"$f\" 2>/dev/null) || continue; printf \"%%s\\0%%s\\0\" \"$f\" \"$out\"; done' sh {} + || true", quotedPath)}
	stdout, _, err := s.execInContainerInternal(ctx, containerID, cmd)
	if err != nil {
		return nil, fmt.Errorf("failed to list directory: %w", err)
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

	sanitizedPath, err := s.sanitizeBrowsePathInternal(filePath)
	if err != nil {
		return nil, "", fmt.Errorf("invalid path: %w", err)
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
		return nil, "", fmt.Errorf("failed to read file: %w", err)
	}

	content := []byte(stdout)
	mimeType := http.DetectContentType(content)

	return content, mimeType, nil
}

func (s *VolumeService) DownloadFile(ctx context.Context, volumeName, filePath string) (io.ReadCloser, int64, error) {
	slog.DebugContext(ctx, "volume service: download file", "volume", volumeName, "path", filePath)

	sanitizedPath, err := s.sanitizeBrowsePathInternal(filePath)
	if err != nil {
		return nil, 0, fmt.Errorf("invalid path: %w", err)
	}

	dockerClient, err := s.dockerService.GetClient()
	if err != nil {
		return nil, 0, err
	}

	containerID, cleanup, err := s.createTempContainerInternal(ctx, volumeName, true)
	if err != nil {
		return nil, 0, err
	}

	targetPath := path.Join("/volume", sanitizedPath)
	reader, _, err := dockerClient.CopyFromContainer(ctx, containerID, targetPath)
	if err != nil {
		cleanup()
		return nil, 0, fmt.Errorf("failed to download: %w", err)
	}

	tr := tar.NewReader(reader)
	hdr, err := tr.Next()
	if err != nil {
		_ = reader.Close()
		cleanup()
		return nil, 0, fmt.Errorf("failed to read tar stream: %w", err)
	}
	if hdr.FileInfo().IsDir() {
		_ = reader.Close()
		cleanup()
		return nil, 0, fmt.Errorf("path is a directory")
	}
	size := hdr.Size

	return &cleanupReadCloser{
		Reader:  tr,
		Closer:  reader,
		cleanup: cleanup,
	}, size, nil
}

func (s *VolumeService) getHelperImageInternal(ctx context.Context) (string, error) {
	slog.DebugContext(ctx, "volume service: resolve helper image")
	dockerClient, err := s.dockerService.GetClient()
	if err != nil {
		return "", fmt.Errorf("failed to get docker client: %w", err)
	}

	if _, err := dockerClient.ImageInspect(ctx, volumeHelperImage); err == nil {
		slog.InfoContext(ctx, "volume service: helper image strategy selected", "strategy", "busybox-local", "image", volumeHelperImage)
		return volumeHelperImage, nil
	}

	var pullErr error
	if s.imageService != nil {
		pullImageErr := s.imageService.PullImage(ctx, volumeHelperImage, io.Discard, systemUser, nil)
		if pullImageErr == nil {
			slog.InfoContext(ctx, "volume service: helper image strategy selected", "strategy", "busybox-pulled", "image", volumeHelperImage)
			return volumeHelperImage, nil
		}
		pullErr = pullImageErr
		slog.WarnContext(ctx, "volume service: failed to pull busybox helper image, attempting arcane fallback", "error", pullImageErr.Error())
	} else {
		pullErr = fmt.Errorf("image service unavailable")
		slog.WarnContext(ctx, "volume service: image service unavailable, attempting arcane fallback")
	}

	if fallbackImage, source, ok := s.resolveArcaneHelperImageInternal(ctx, dockerClient); ok {
		slog.InfoContext(ctx, "volume service: helper image strategy selected", "strategy", "arcane-fallback", "source", source, "image", fallbackImage)
		return fallbackImage, nil
	}

	return "", fmt.Errorf("failed to resolve helper image: busybox unavailable and arcane fallback not found (pull error: %w)", pullErr)
}

func (s *VolumeService) resolveArcaneHelperImageInternal(ctx context.Context, dockerClient *client.Client) (string, string, bool) {
	hostname, _ := os.Hostname()
	if hostname != "" {
		if inspect, err := dockerClient.ContainerInspect(ctx, hostname); err == nil && inspect.Config != nil && strings.TrimSpace(inspect.Config.Image) != "" {
			return inspect.Config.Image, "hostname", true
		}
	}

	filter := filters.NewArgs()
	filter.Add("label", "com.getarcaneapp.arcane=true")
	if containers, err := dockerClient.ContainerList(ctx, container.ListOptions{Filters: filter, All: true}); err == nil && len(containers) > 0 && strings.TrimSpace(containers[0].Image) != "" {
		return containers[0].Image, "arcane-label", true
	}

	return "", "", false
}

func (s *VolumeService) BackupMountWarning(ctx context.Context) string {
	dockerClient, err := s.dockerService.GetClient()
	if err != nil {
		return ""
	}

	containerID := s.getArcaneContainerIDInternal(ctx, dockerClient)
	if containerID == "" {
		return ""
	}

	inspect, err := dockerClient.ContainerInspect(ctx, containerID)
	if err != nil {
		return ""
	}

	hasBackups := false
	hasRestores := false
	for _, mount := range inspect.Mounts {
		if mount.Destination == "/backups" {
			hasBackups = true
		}
		if mount.Destination == "/restores" {
			hasRestores = true
		}
	}

	if hasBackups || hasRestores {
		return ""
	}

	return "No volume is mounted at /backups or /restores in the Arcane container. Backups/restores will only live inside Docker unless you mount a host path."
}

func (s *VolumeService) getArcaneContainerIDInternal(ctx context.Context, dockerClient *client.Client) string {
	hostname, _ := os.Hostname()
	if hostname != "" {
		if inspect, err := dockerClient.ContainerInspect(ctx, hostname); err == nil {
			return inspect.ID
		}
	}

	filter := filters.NewArgs()
	filter.Add("label", "com.getarcaneapp.arcane=true")
	containers, err := dockerClient.ContainerList(ctx, container.ListOptions{Filters: filter, All: true})
	if err != nil || len(containers) == 0 {
		return ""
	}

	for _, c := range containers {
		if strings.EqualFold(strings.TrimSpace(c.State), "running") {
			return c.ID
		}
	}

	return containers[0].ID
}

type cleanupReadCloser struct {
	io.Reader
	io.Closer
	cleanup func()
}

func buildVolumeHelperLabelsInternal() map[string]string {
	return map[string]string{
		libarcane.InternalResourceLabel: "true",
	}
}

func volumeHelperRemoveOptionsInternal() container.RemoveOptions {
	return container.RemoveOptions{Force: true, RemoveVolumes: true}
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
	return isLegacyVolumeHelperContainerInternal(c)
}

func (s *VolumeService) isArcaneFallbackHelperImageInternal(helperImage string) bool {
	return !strings.EqualFold(strings.TrimSpace(helperImage), volumeHelperImage)
}

func (s *VolumeService) buildHelperHostConfigInternal(helperImage string, binds []string) *container.HostConfig {
	hostConfig := &container.HostConfig{
		Binds:      binds,
		AutoRemove: true,
	}

	if runtime.GOOS == "linux" && s.isArcaneFallbackHelperImageInternal(helperImage) {
		hostConfig.Tmpfs = map[string]string{
			"/app/data": "rw,noexec,nosuid,nodev",
		}
	}

	return hostConfig
}

func (c *cleanupReadCloser) Close() error {
	err := c.Closer.Close()
	c.cleanup()
	return err
}

func (s *VolumeService) createTempContainerInternal(ctx context.Context, volumeName string, readOnly bool) (string, func(), error) {
	slog.DebugContext(ctx, "volume service: create temp container", "volume", volumeName, "read_only", readOnly)
	dockerClient, err := s.dockerService.GetClient()
	if err != nil {
		return "", nil, err
	}

	if readOnly {
		if containerID, ok := s.getReusableReadOnlyContainerInternal(ctx, dockerClient, volumeName); ok {
			return containerID, func() {}, nil
		}
	}

	helperImage, err := s.getHelperImageInternal(ctx)
	if err != nil {
		return "", nil, err
	}

	config := &container.Config{
		Image:           helperImage,
		Cmd:             []string{"sleep", "infinity"},
		NetworkDisabled: true,
		Labels:          buildVolumeHelperLabelsInternal(),
	}

	hostConfig := s.buildHelperHostConfigInternal(helperImage, []string{
		fmt.Sprintf("%s:/volume%s", volumeName, func() string {
			if readOnly {
				return ":ro"
			}
			return ""
		}()),
	})

	resp, err := dockerClient.ContainerCreate(ctx, config, hostConfig, nil, nil, "")
	if err != nil {
		return "", nil, fmt.Errorf("failed to create temp container: %w", err)
	}

	if err := dockerClient.ContainerStart(ctx, resp.ID, container.StartOptions{}); err != nil {
		_ = dockerClient.ContainerRemove(ctx, resp.ID, volumeHelperRemoveOptionsInternal())
		return "", nil, fmt.Errorf("failed to start temp container: %w", err)
	}

	cleanup := func() {
		_ = dockerClient.ContainerRemove(ctx, resp.ID, volumeHelperRemoveOptionsInternal())
	}

	if readOnly {
		s.helperMu.Lock()
		s.helperByVolume[volumeName] = resp.ID
		s.helperMu.Unlock()
		return resp.ID, func() {}, nil
	}

	return resp.ID, cleanup, nil
}

func (s *VolumeService) getReusableReadOnlyContainerInternal(ctx context.Context, dockerClient *client.Client, volumeName string) (string, bool) {
	s.helperMu.Lock()
	containerID := s.helperByVolume[volumeName]
	s.helperMu.Unlock()
	if containerID == "" {
		return "", false
	}

	inspect, err := dockerClient.ContainerInspect(ctx, containerID)
	if err != nil || inspect.State == nil || !inspect.State.Running {
		s.helperMu.Lock()
		delete(s.helperByVolume, volumeName)
		s.helperMu.Unlock()
		return "", false
	}

	return containerID, true
}

func (s *VolumeService) CleanupHelperContainers(ctx context.Context) {
	dockerClient, err := s.dockerService.GetClient()
	if err != nil {
		slog.WarnContext(ctx, "failed to get docker client for helper cleanup", "error", err)
		return
	}

	s.helperMu.Lock()
	helperIDs := make([]string, 0, len(s.helperByVolume))
	for _, containerID := range s.helperByVolume {
		if containerID != "" {
			helperIDs = append(helperIDs, containerID)
		}
	}
	s.helperByVolume = make(map[string]string)
	s.helperMu.Unlock()

	for _, containerID := range helperIDs {
		if err := dockerClient.ContainerRemove(ctx, containerID, volumeHelperRemoveOptionsInternal()); err != nil {
			slog.WarnContext(ctx, "failed to remove helper container", "container_id", containerID, "error", err.Error())
		}
	}
}

func (s *VolumeService) CleanupOrphanedVolumeHelpers(ctx context.Context) error {
	dockerClient, err := s.dockerService.GetClient()
	if err != nil {
		return fmt.Errorf("failed to get docker client for orphan helper cleanup: %w", err)
	}

	containers, err := dockerClient.ContainerList(ctx, container.ListOptions{All: true})
	if err != nil {
		return fmt.Errorf("failed to list containers for orphan helper cleanup: %w", err)
	}

	removedCount := 0
	for _, c := range containers {
		if !isVolumeHelperContainerInternal(c) {
			continue
		}

		if err := dockerClient.ContainerRemove(ctx, c.ID, volumeHelperRemoveOptionsInternal()); err != nil {
			slog.WarnContext(ctx, "failed to remove orphaned volume helper container", "container_id", c.ID, "error", err.Error())
			continue
		}

		removedCount++
	}

	slog.InfoContext(ctx, "volume service: orphan helper cleanup completed", "removed_count", removedCount)
	return nil
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
	dockerClient, err := s.dockerService.GetClient()
	if err != nil {
		return "", "", err
	}

	execConfig := container.ExecOptions{
		AttachStdout: true,
		AttachStderr: true,
		Cmd:          cmd,
	}

	execResp, err := dockerClient.ContainerExecCreate(ctx, containerID, execConfig)
	if err != nil {
		return "", "", err
	}

	resp, err := dockerClient.ContainerExecAttach(ctx, execResp.ID, container.ExecAttachOptions{})
	if err != nil {
		return "", "", err
	}
	defer resp.Close()

	var stdout, stderr bytes.Buffer
	_, err = stdcopy.StdCopy(&stdout, &stderr, resp.Reader)
	if err != nil {
		return "", "", err
	}

	return stdout.String(), stderr.String(), nil
}

func (s *VolumeService) DeleteFile(ctx context.Context, volumeName, filePath string, user *models.User) error {
	slog.DebugContext(ctx, "volume service: delete file", "volume", volumeName, "path", filePath)

	sanitizedPath, err := s.sanitizeBrowsePathInternal(filePath)
	if err != nil {
		return fmt.Errorf("invalid path: %w", err)
	}
	// Prevent deleting root
	if sanitizedPath == "/" {
		return fmt.Errorf("cannot delete root directory")
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
		return fmt.Errorf("delete failed: %s", stderr)
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

	sanitizedPath, err := s.sanitizeBrowsePathInternal(dirPath)
	if err != nil {
		return fmt.Errorf("invalid path: %w", err)
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
		return fmt.Errorf("mkdir failed: %s", stderr)
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

	sanitizedPath, err := s.sanitizeBrowsePathInternal(destPath)
	if err != nil {
		return fmt.Errorf("invalid path: %w", err)
	}

	dockerClient, err := s.dockerService.GetClient()
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
		Mode: 0644,
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
	err = dockerClient.CopyToContainer(ctx, containerID, targetDir, &buf, container.CopyToContainerOptions{})
	if err != nil {
		return fmt.Errorf("failed to upload: %w", err)
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
	dockerClient, err := s.dockerService.GetClient()
	if err != nil {
		return err
	}

	_, err = dockerClient.VolumeInspect(ctx, s.backupVolumeName)
	if err != nil {
		_, err = dockerClient.VolumeCreate(ctx, volume.CreateOptions{
			Name: s.backupVolumeName,
		})
		if err != nil {
			return fmt.Errorf("failed to create backup volume: %w", err)
		}
	}
	return nil
}

func (s *VolumeService) CreateBackup(ctx context.Context, volumeName string, user models.User) (*models.VolumeBackup, error) {
	slog.DebugContext(ctx, "volume service: create backup", "volume", volumeName, "user", user.ID)
	if err := s.ensureBackupVolumeInternal(ctx); err != nil {
		return nil, err
	}

	dockerClient, err := s.dockerService.GetClient()
	if err != nil {
		return nil, err
	}

	backupID := fmt.Sprintf("%s-%d-%s", volumeName, time.Now().UnixNano(), uuid.NewString()[:8])
	filename := fmt.Sprintf("%s.tar.gz", backupID)

	helperImage, err := s.getHelperImageInternal(ctx)
	if err != nil {
		return nil, err
	}

	config := &container.Config{
		Image:  helperImage,
		Cmd:    []string{"sh", "-c", fmt.Sprintf("tar -czf /backups/%s -C /volume .", filename)},
		Labels: buildVolumeHelperLabelsInternal(),
	}

	hostConfig := s.buildHelperHostConfigInternal(helperImage, []string{
		fmt.Sprintf("%s:/volume:ro", volumeName),
		fmt.Sprintf("%s:/backups", s.backupVolumeName),
	})

	resp, err := dockerClient.ContainerCreate(ctx, config, hostConfig, nil, nil, "")
	if err != nil {
		return nil, fmt.Errorf("failed to create backup container: %w", err)
	}

	if err := dockerClient.ContainerStart(ctx, resp.ID, container.StartOptions{}); err != nil {
		_ = dockerClient.ContainerRemove(ctx, resp.ID, volumeHelperRemoveOptionsInternal())
		return nil, fmt.Errorf("failed to start backup container: %w", err)
	}

	statusCh, errCh := dockerClient.ContainerWait(ctx, resp.ID, container.WaitConditionNotRunning)
	select {
	case err := <-errCh:
		if err != nil {
			return nil, err
		}
	case status := <-statusCh:
		if status.StatusCode != 0 {
			return nil, fmt.Errorf("backup container exited with status %d", status.StatusCode)
		}
	}

	tempContainerID, cleanup, err := s.createTempContainerInternal(ctx, s.backupVolumeName, true)
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
	containerID, cleanup, err := s.createTempContainerInternal(ctx, s.backupVolumeName, false)
	if err != nil {
		slog.WarnContext(ctx, "failed to create container for backup file cleanup", "backup_id", backupID, "error", err.Error())
	} else {
		defer cleanup()
		filename := fmt.Sprintf("%s.tar.gz", backupID)
		if _, _, err = s.execInContainerInternal(ctx, containerID, []string{"rm", "-f", path.Join("/volume", filename)}); err != nil {
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
		return fmt.Errorf("backup does not belong to volume %s", volumeName)
	}

	// Check if volume is in use by running containers
	inUse, containerIDs, err := s.GetVolumeUsage(ctx, volumeName)
	if err != nil {
		slog.WarnContext(ctx, "could not check volume usage", "volume", volumeName, "error", err.Error())
	} else if inUse {
		return fmt.Errorf("volume is in use by %d container(s): restoring while containers are running may cause data corruption. Stop the containers first or use selective file restore", len(containerIDs))
	}

	preBackup, err := s.CreateBackup(ctx, volumeName, user)
	if err != nil {
		return fmt.Errorf("failed to create pre-restore backup: %w", err)
	}

	dockerClient, err := s.dockerService.GetClient()
	if err != nil {
		return err
	}

	filename := fmt.Sprintf("%s.tar.gz", backupID)

	helperImage, err := s.getHelperImageInternal(ctx)
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
		Labels: buildVolumeHelperLabelsInternal(),
	}

	hostConfig := s.buildHelperHostConfigInternal(helperImage, []string{
		fmt.Sprintf("%s:/volume", volumeName),
		fmt.Sprintf("%s:/backups:ro", s.backupVolumeName),
	})

	resp, err := dockerClient.ContainerCreate(ctx, config, hostConfig, nil, nil, "")
	if err != nil {
		return fmt.Errorf("failed to create restore container: %w", err)
	}

	if err := dockerClient.ContainerStart(ctx, resp.ID, container.StartOptions{}); err != nil {
		_ = dockerClient.ContainerRemove(ctx, resp.ID, volumeHelperRemoveOptionsInternal())
		return fmt.Errorf("failed to start restore container: %w", err)
	}

	statusCh, errCh := dockerClient.ContainerWait(ctx, resp.ID, container.WaitConditionNotRunning)
	var waitBody container.WaitResponse
	select {
	case err := <-errCh:
		if err != nil {
			return err
		}
	case waitBody = <-statusCh:
	}

	if waitBody.StatusCode != 0 {
		return fmt.Errorf("restore container exited with code %d (volume may be partially wiped)", waitBody.StatusCode)
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
		return "", fmt.Errorf("invalid path: empty")
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

// sanitizeBrowsePath validates and cleans a path for file browser operations.
// It ensures the path stays within the volume boundary.
func (s *VolumeService) sanitizeBrowsePathInternal(input string) (string, error) {
	trimmed := strings.TrimSpace(input)
	if trimmed == "" || trimmed == "/" {
		return "/", nil // Root is valid for browse
	}
	cleaned := path.Clean(trimmed)
	// Ensure path starts with /
	if !path.IsAbs(cleaned) {
		cleaned = "/" + cleaned
	}
	// Check for path traversal attempts
	if strings.Contains(cleaned, "/../") || strings.HasSuffix(cleaned, "/..") || cleaned == "/.." {
		return "", fmt.Errorf("invalid path: path traversal not allowed")
	}
	// After cleaning, the path should not escape root
	if !strings.HasPrefix(cleaned, "/") {
		return "", fmt.Errorf("invalid path: must be absolute")
	}
	return cleaned, nil
}

func (s *VolumeService) BackupHasPath(ctx context.Context, backupID string, filePath string) (bool, error) {
	slog.DebugContext(ctx, "volume service: backup has path", "backup_id", backupID, "path", filePath)
	if err := s.ensureBackupVolumeInternal(ctx); err != nil {
		return false, err
	}

	cleaned, err := s.sanitizeBackupPathInternal(filePath)
	if err != nil {
		return false, err
	}

	var backup models.VolumeBackup
	if err := s.db.WithContext(ctx).Where("id = ?", backupID).First(&backup).Error; err != nil {
		return false, err
	}

	containerID, cleanup, err := s.createTempContainerInternal(ctx, s.backupVolumeName, true)
	if err != nil {
		return false, err
	}
	defer cleanup()

	archivePath := path.Join("/volume", fmt.Sprintf("%s.tar.gz", backupID))
	cmd := []string{"tar", "-tzf", archivePath}
	stdout, stderr, err := s.execInContainerInternal(ctx, containerID, cmd)
	if err != nil {
		return false, err
	}
	if strings.TrimSpace(stderr) != "" {
		return false, fmt.Errorf("failed to list backup contents: %s", strings.TrimSpace(stderr))
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
	if err := s.ensureBackupVolumeInternal(ctx); err != nil {
		return nil, err
	}

	var backup models.VolumeBackup
	if err := s.db.WithContext(ctx).Where("id = ?", backupID).First(&backup).Error; err != nil {
		return nil, err
	}

	containerID, cleanup, err := s.createTempContainerInternal(ctx, s.backupVolumeName, true)
	if err != nil {
		return nil, err
	}
	defer cleanup()

	archivePath := path.Join("/volume", fmt.Sprintf("%s.tar.gz", backupID))
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
		return fmt.Errorf("no paths provided")
	}

	var backup models.VolumeBackup
	if err := s.db.WithContext(ctx).Where("id = ?", backupID).First(&backup).Error; err != nil {
		return err
	}
	if backup.VolumeName != volumeName {
		return fmt.Errorf("backup does not belong to volume")
	}

	// Create pre-restore backup for safety (consistent with RestoreBackup behavior)
	preBackup, err := s.CreateBackup(ctx, volumeName, user)
	if err != nil {
		return fmt.Errorf("failed to create pre-restore backup: %w", err)
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
		return fmt.Errorf("no valid paths provided")
	}

	tarPaths := make([]string, 0, len(cleanedPaths))
	for _, p := range cleanedPaths {
		tarPaths = append(tarPaths, "./"+p)
	}

	dockerClient, err := s.dockerService.GetClient()
	if err != nil {
		return err
	}

	helperImage, err := s.getHelperImageInternal(ctx)
	if err != nil {
		return err
	}

	config := &container.Config{
		Image:           helperImage,
		Cmd:             []string{"sleep", "infinity"},
		NetworkDisabled: true,
		Labels:          buildVolumeHelperLabelsInternal(),
	}

	hostConfig := s.buildHelperHostConfigInternal(helperImage, []string{
		fmt.Sprintf("%s:/volume", volumeName),
		fmt.Sprintf("%s:/backups:ro", s.backupVolumeName),
	})

	resp, err := dockerClient.ContainerCreate(ctx, config, hostConfig, nil, nil, "")
	if err != nil {
		return fmt.Errorf("failed to create restore container: %w", err)
	}

	if err := dockerClient.ContainerStart(ctx, resp.ID, container.StartOptions{}); err != nil {
		_ = dockerClient.ContainerRemove(ctx, resp.ID, volumeHelperRemoveOptionsInternal())
		return fmt.Errorf("failed to start restore container: %w", err)
	}

	cleanup := func() {
		_ = dockerClient.ContainerRemove(ctx, resp.ID, volumeHelperRemoveOptionsInternal())
	}
	defer cleanup()

	filename := fmt.Sprintf("%s.tar.gz", backupID)
	cmd := append([]string{"tar", "-xzf", path.Join("/backups", filename), "-C", "/volume", "--"}, tarPaths...)
	_, stderr, err := s.execInContainerInternal(ctx, resp.ID, cmd)
	if err != nil {
		return fmt.Errorf("failed to restore files: %w", err)
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
	filename := fmt.Sprintf("%s.tar.gz", backupID)
	reader, size, err := s.DownloadFile(ctx, s.backupVolumeName, filename)
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
		return fmt.Errorf("failed to buffer upload: %w", err)
	}
	defer func() {
		_ = tmpFile.Close()
		_ = os.Remove(tmpFile.Name()) //nolint:gosec // temp file path is generated by os.CreateTemp
	}()
	if _, err := io.Copy(tmpFile, archive); err != nil {
		return fmt.Errorf("failed to buffer upload: %w", err)
	}
	if _, err := tmpFile.Seek(0, io.SeekStart); err != nil {
		return fmt.Errorf("failed to read buffered upload: %w", err)
	}
	gzr, err := gzip.NewReader(tmpFile)
	if err != nil {
		return fmt.Errorf("invalid archive: %w", err)
	}
	if _, err := tar.NewReader(gzr).Next(); err != nil {
		_ = gzr.Close()
		return fmt.Errorf("invalid archive: %w", err)
	}
	_ = gzr.Close()

	preBackup, err := s.CreateBackup(ctx, volumeName, user)
	if err != nil {
		return fmt.Errorf("failed to create pre-restore backup: %w", err)
	}

	dockerClient, err := s.dockerService.GetClient()
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
		return fmt.Errorf("failed to create temp restore dir: %w", err)
	}
	if strings.TrimSpace(stderr) != "" {
		slog.DebugContext(ctx, "volume service: restore temp dir stderr", "volume", volumeName, "stderr", strings.TrimSpace(stderr))
	}

	if _, err := tmpFile.Seek(0, io.SeekStart); err != nil {
		return fmt.Errorf("failed to read buffered upload: %w", err)
	}
	err = dockerClient.CopyToContainer(ctx, containerID, tmpDir, tmpFile, container.CopyToContainerOptions{})
	if err != nil {
		return fmt.Errorf("failed to restore from uploaded archive: %w", err)
	}

	_, stderr, err = s.execInContainerInternal(ctx, containerID, []string{"sh", "-c", fmt.Sprintf("test -n \"$(find %s -mindepth 1 -maxdepth 1 -print -quit)\"", tmpDir)})
	if err != nil {
		return fmt.Errorf("uploaded archive appears empty or invalid: %w", err)
	}
	if strings.TrimSpace(stderr) != "" {
		slog.DebugContext(ctx, "volume service: restore validate stderr", "volume", volumeName, "stderr", strings.TrimSpace(stderr))
	}

	_, stderr, err = s.execInContainerInternal(ctx, containerID, []string{"sh", "-c", "rm -rf /volume/* /volume/.[!.]* /volume/..?* 2>/dev/null || true"})
	if err != nil {
		return fmt.Errorf("failed to clear volume before restore: %w", err)
	}
	if strings.TrimSpace(stderr) != "" {
		slog.DebugContext(ctx, "volume service: restore clear stderr", "volume", volumeName, "stderr", strings.TrimSpace(stderr))
	}

	moveCmd := fmt.Sprintf("find %s -mindepth 1 -maxdepth 1 -exec mv -- {} /volume/ \\; && rmdir %s", tmpDir, tmpDir)
	_, stderr, err = s.execInContainerInternal(ctx, containerID, []string{"sh", "-c", moveCmd})
	if err != nil {
		return fmt.Errorf("failed to move restored files into place: %w", err)
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
	dockerClient, err := s.dockerService.GetClient()
	if err != nil {
		return false, nil, fmt.Errorf("failed to connect to Docker: %w", err)
	}

	vol, err := dockerClient.VolumeInspect(ctx, name)
	if err != nil {
		return false, nil, fmt.Errorf("volume not found: %w", err)
	}

	containerIDs, err := docker.GetContainersUsingVolume(ctx, dockerClient, vol.Name)
	if err != nil {
		return false, nil, fmt.Errorf("failed to get containers using volume: %w", err)
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
	dockerClient, err := s.dockerService.GetClient()
	if err != nil {
		return nil, fmt.Errorf("failed to connect to Docker: %w", err)
	}

	usageVolumes, err := docker.GetVolumeUsageData(ctx, dockerClient)
	if err != nil {
		return nil, fmt.Errorf("failed to get volume usage data: %w", err)
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

func (s *VolumeService) enrichVolumesWithUsageDataInternal(volumes []*volume.Volume, usageVolumes []volume.Volume) []volume.Volume {
	slog.Debug("volume service: enrich volumes with usage data", "volumes", len(volumes), "usage_volumes", len(usageVolumes))
	result := make([]volume.Volume, 0, len(volumes))
	for _, v := range volumes {
		if v != nil {
			for _, uv := range usageVolumes {
				if uv.Name == v.Name && uv.UsageData != nil {
					v.UsageData = uv.UsageData
					break
				}
			}

			result = append(result, *v)
		}
	}
	return result
}

func (s *VolumeService) buildVolumeContainerMapInternal(ctx context.Context, dockerClient *client.Client) (map[string][]string, error) {
	slog.DebugContext(ctx, "volume service: build volume container map")
	containers, err := dockerClient.ContainerList(ctx, container.ListOptions{All: true})
	if err != nil {
		return nil, fmt.Errorf("failed to list containers: %w", err)
	}

	volumeContainerMap := make(map[string][]string)
	for _, c := range containers {
		for _, m := range c.Mounts {
			if m.Type == mount.TypeVolume && m.Name != "" {
				volumeContainerMap[m.Name] = append(volumeContainerMap[m.Name], c.ID)
			}
		}
	}

	return volumeContainerMap, nil
}

func (s *VolumeService) buildVolumePaginationConfigInternal() pagination.Config[volumetypes.Volume] {
	slog.Debug("volume service: build volume pagination config")
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
	slog.Debug("volume service: build volume sort bindings")
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
	slog.Debug("volume service: compare volume sizes")
	aSize := a.Size
	bSize := b.Size

	if aSize == 0 && a.UsageData != nil {
		aSize = a.UsageData.Size
	}
	if bSize == 0 && b.UsageData != nil {
		bSize = b.UsageData.Size
	}

	if aSize == bSize {
		return 0
	}
	if aSize < bSize {
		return -1
	}
	return 1
}

func (s *VolumeService) compareVolumeCreatedInternal(a, b volumetypes.Volume) int {
	slog.Debug("volume service: compare volume created time")
	aTime, aOk := s.parseVolumeCreatedAtInternal(a.CreatedAt)
	bTime, bOk := s.parseVolumeCreatedAtInternal(b.CreatedAt)
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

func (s *VolumeService) parseVolumeCreatedAtInternal(createdAt string) (time.Time, bool) {
	if createdAt == "" {
		return time.Time{}, false
	}
	if parsed, err := time.Parse(time.RFC3339Nano, createdAt); err == nil {
		return parsed, true
	}
	if parsed, err := time.Parse(time.RFC3339, createdAt); err == nil {
		return parsed, true
	}
	return time.Time{}, false
}

func (s *VolumeService) buildVolumeFilterAccessorsInternal() []pagination.FilterAccessor[volumetypes.Volume] {
	slog.Debug("volume service: build volume filter accessors")
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
	slog.Debug("volume service: calculate volume usage counts", "items", len(items))
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
	slog.DebugContext(ctx, "volume service: list volumes paginated", "search", params.Search, "sort", params.Sort, "order", params.Order, "start", params.Start, "limit", params.Limit, "include_internal", includeInternal)
	dockerClient, err := s.dockerService.GetClient()
	if err != nil {
		return nil, pagination.Response{}, volumetypes.UsageCounts{}, fmt.Errorf("failed to connect to Docker: %w", err)
	}

	// Run volume list and container list in parallel for better performance
	type volumeListResult struct {
		volumes []*volume.Volume
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
		volListBody, err := dockerClient.VolumeList(ctx, volume.ListOptions{})
		volChan <- volumeListResult{volumes: volListBody.Volumes, err: err}
	}(apiCtx)

	go func(ctx context.Context) {
		containerMap, err := s.buildVolumeContainerMapInternal(ctx, dockerClient)
		containerChan <- containerMapResult{containerMap: containerMap, err: err}
	}(apiCtx)

	// Wait for both results
	volResult := <-volChan
	if volResult.err != nil {
		return nil, pagination.Response{}, volumetypes.UsageCounts{}, fmt.Errorf("failed to list Docker volumes: %w", volResult.err)
	}

	containerResult := <-containerChan
	volumeContainerMap := containerResult.containerMap
	if containerResult.err != nil {
		slog.WarnContext(ctx, "failed to build volume-container map", "error", containerResult.err.Error())
		volumeContainerMap = make(map[string][]string)
	}

	// Fetch usage data if sorting by size is requested
	var usageVolumes []volume.Volume
	if params.Sort == "size" {
		if uv, err := docker.GetVolumeUsageData(apiCtx, dockerClient); err == nil {
			usageVolumes = uv
		} else {
			slog.WarnContext(ctx, "failed to get volume usage data for sorting", "error", err.Error())
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
	result := pagination.SearchOrderAndPaginate(items, params, config)
	counts := s.calculateVolumeUsageCountsInternal(items)
	paginationResp := pagination.BuildResponseFromFilterResult(result, params)

	return result.Items, paginationResp, counts, nil
}
