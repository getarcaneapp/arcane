package volume

import (
	"archive/tar"
	"bytes"
	"context"
	"io"
	"log/slog"
	"net/http"
	"path"
	"strconv"
	"strings"
	"time"

	"emperror.dev/errors"

	"github.com/getarcaneapp/arcane/backend/v2/internal/models"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/libarcane/volumehelper"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/utils"
	volumetypes "github.com/getarcaneapp/arcane/types/v2/volume"
	"github.com/moby/moby/client"
)

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
	return volumehelper.DownloadFileFromContainer(ctx, dockerClient, containerID, targetPath, cleanup)
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
		actingUser = &models.SystemUser
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
		actingUser = &models.SystemUser
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
		actingUser = &models.SystemUser
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
