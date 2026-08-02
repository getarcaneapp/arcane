package services

import (
	"archive/tar"
	"context"
	"crypto/sha256"
	"encoding/hex"
	stderrors "errors"
	"fmt"
	"io"
	"log/slog"
	"mime/multipart"
	"net/http"
	"path"
	"slices"
	"strconv"
	"strings"
	"time"

	"emperror.dev/errors"
	cerrdefs "github.com/containerd/errdefs"

	"github.com/getarcaneapp/arcane/backend/v2/internal/common"
	"github.com/getarcaneapp/arcane/backend/v2/internal/models"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/libarcane/volumehelper"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/utils"
	volumetypes "github.com/getarcaneapp/arcane/types/v2/volume"
	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/api/types/mount"
	"github.com/moby/moby/client"
)

const maxEditableVolumeFileBytes int64 = 10 * 1024 * 1024

func (s *VolumeService) GetVolumeWorkspace(ctx context.Context, volumeName string) (*volumetypes.Workspace, error) {
	if err := s.isBrowsableVolumeInternal(ctx, volumeName); err != nil {
		return nil, classifyVolumeWorkspaceBrowseErrorInternal(err)
	}
	containerID, cleanup, err := s.createTempContainerInternal(ctx, volumeName, true)
	if err != nil {
		return nil, err
	}
	defer cleanup()
	return s.readVolumeWorkspaceFromContainerInternal(ctx, containerID)
}

func (s *VolumeService) readVolumeWorkspaceFromContainerInternal(ctx context.Context, containerID string) (*volumetypes.Workspace, error) {
	maxDepth := s.fileTreeMaxDepth
	if maxDepth <= 0 {
		maxDepth = 50
	}
	maxEntries := s.fileTreeMaxEntries
	if maxEntries <= 0 {
		maxEntries = 10000
	}
	entryLimit := maxEntries + 1
	script := `export LC_ALL=C
max_depth="$1"
entry_limit="$2"
next_depth=$((max_depth + 1))
depth_truncated=0
if [ -n "$(find -P /volume -mindepth "$next_depth" -maxdepth "$next_depth" -print -quit)" ]; then
  depth_truncated=1
fi
printf '%s\0' "$depth_truncated"
find -P /volume -mindepth 1 -maxdepth "$max_depth" -printf '%P\0' |
  sort -z |
  head -z -n "$entry_limit" |
  xargs -0 -r sh -c '
    set -e
    for rel do
      full="/volume/$rel"
      printf "%s\0" "$rel"
      find -P "$full" -maxdepth 0 -printf "%y\0%s\0%T@\0%M\0%l\0"
    done
  ' sh`
	stdout, _, err := s.execInContainerInternal(ctx, containerID, []string{"sh", "-c", script, "sh", strconv.Itoa(maxDepth), strconv.Itoa(entryLimit)})
	if err != nil {
		return nil, errors.WrapIf(err, "failed to read volume workspace")
	}
	return parseVolumeWorkspaceTreeInternal(stdout, maxEntries)
}

func parseVolumeWorkspaceTreeInternal(stdout string, maxEntries int) (*volumetypes.Workspace, error) {
	fields := strings.Split(stdout, "\x00")
	depthTruncated := len(fields) > 0 && fields[0] == "1"
	if len(fields) == 0 || (fields[0] != "0" && fields[0] != "1") {
		return nil, errors.New("invalid volume workspace truncation marker")
	}
	if len(fields) > 0 {
		fields = fields[1:]
	}
	if len(fields) > 0 && fields[len(fields)-1] == "" {
		fields = fields[:len(fields)-1]
	}
	if len(fields)%6 != 0 {
		return nil, errors.New("invalid volume workspace file entry")
	}
	entries := make([]volumetypes.FileEntry, 0, min(len(fields)/6, maxEntries+1))
	for i := 0; i+5 < len(fields); i += 6 {
		relativePath := fields[i]
		if relativePath == "" {
			continue
		}
		kind := fields[i+1]
		size, err := strconv.ParseInt(fields[i+2], 10, 64)
		if err != nil {
			return nil, errors.WrapIf(err, "parse volume workspace file size")
		}
		modSeconds, err := strconv.ParseFloat(strings.TrimSpace(fields[i+3]), 64)
		if err != nil {
			return nil, errors.WrapIf(err, "parse volume workspace modification time")
		}
		isDirectory := kind == "d"
		if isDirectory {
			size = 0
		}
		entries = append(entries, volumetypes.FileEntry{
			ModTime:      time.Unix(0, int64(modSeconds*float64(time.Second))),
			Name:         path.Base(relativePath),
			Path:         "/" + relativePath,
			RelativePath: relativePath,
			Mode:         fields[i+4],
			LinkTarget:   fields[i+5],
			Size:         size,
			IsDirectory:  isDirectory,
			IsSymlink:    kind == "l",
		})
	}

	slices.SortFunc(entries, func(a, b volumetypes.FileEntry) int {
		return strings.Compare(a.RelativePath, b.RelativePath)
	})
	truncated := depthTruncated || len(entries) > maxEntries
	if len(entries) > maxEntries {
		entries = entries[:maxEntries]
	}

	h := sha256.New()
	for _, entry := range entries {
		kind := "file"
		if entry.IsDirectory {
			kind = "dir"
		} else if entry.IsSymlink {
			kind = "symlink"
		}
		utils.WriteFileTreeRevisionEntry(h, entry.RelativePath, kind, entry.Size, entry.ModTime.UnixNano(), entry.Mode, false)
	}

	return &volumetypes.Workspace{
		Files:             entries,
		FileTreeRevision:  hex.EncodeToString(h.Sum(nil)),
		FileTreeTruncated: truncated,
	}, nil
}

func (s *VolumeService) GetVolumeWorkspaceFile(ctx context.Context, volumeName, relativePath string) (*volumetypes.WorkspaceFileContent, error) {
	if err := s.isBrowsableVolumeInternal(ctx, volumeName); err != nil {
		return nil, classifyVolumeWorkspaceBrowseErrorInternal(err)
	}
	rel, err := utils.NormalizeRelativePath(relativePath)
	if err != nil {
		return nil, common.Classify(common.ErrVolumeFileForbidden, errors.WrapIf(err, "invalid volume file path"))
	}
	containerID, cleanup, err := s.createTempContainerInternal(ctx, volumeName, true)
	if err != nil {
		return nil, err
	}
	defer cleanup()

	script := `set -e
rel="$1"
limit="$2"
cur=/volume
remaining=$rel
while [ -n "$remaining" ]; do
  case "$remaining" in
    */*) segment=${remaining%%/*}; remaining=${remaining#*/} ;;
    *) segment=$remaining; remaining= ;;
  esac
  cur="$cur/$segment"
  if [ -L "$cur" ]; then echo ARCANE_SYMLINK >&2; exit 42; fi
  if [ -n "$remaining" ] && [ -e "$cur" ] && [ ! -d "$cur" ]; then echo ARCANE_NOT_DIRECTORY >&2; exit 47; fi
done
if [ ! -e "$cur" ]; then echo ARCANE_NOT_FOUND >&2; exit 44; fi
if [ -d "$cur" ]; then echo ARCANE_DIRECTORY >&2; exit 43; fi
if [ ! -f "$cur" ]; then printf '%s\0%s\0' special 0; exit 0; fi
size=$(stat -c %s -- "$cur")
printf 'regular\0%s\0' "$size"
if [ "$size" -gt "$limit" ]; then head -c 512 -- "$cur"; else cat -- "$cur"; fi`
	stdout, stderr, err := s.execInContainerInternal(ctx, containerID, []string{"sh", "-c", script, "sh", rel, strconv.FormatInt(maxEditableVolumeFileBytes, 10)})
	if err != nil {
		return nil, classifyVolumeWorkspaceExecErrorInternal(err, stderr, "read volume workspace file")
	}
	kind, remaining, found := strings.Cut(stdout, "\x00")
	if !found {
		return nil, errors.New("invalid volume file response")
	}
	sizeValue, contentValue, found := strings.Cut(remaining, "\x00")
	if !found {
		return nil, errors.New("invalid volume file response")
	}
	size, err := strconv.ParseInt(sizeValue, 10, 64)
	if err != nil {
		return nil, errors.WrapIf(err, "parse volume file size")
	}
	content := []byte(contentValue)
	return volumeWorkspaceFileContentResponseInternal(rel, kind, size, content)
}

func volumeWorkspaceFileContentResponseInternal(relativePath, kind string, size int64, content []byte) (*volumetypes.WorkspaceFileContent, error) {
	response := &volumetypes.WorkspaceFileContent{
		Path:         "/" + relativePath,
		RelativePath: relativePath,
		Name:         path.Base(relativePath),
		MimeType:     http.DetectContentType(content),
		Size:         size,
	}
	if kind == "special" {
		response.ReadOnlyReason = volumetypes.FileReadOnlySpecial
		return response, nil
	}
	if kind != "regular" {
		return nil, errors.New("invalid volume file kind")
	}
	if size > maxEditableVolumeFileBytes {
		response.ReadOnlyReason = volumetypes.FileReadOnlyTooLarge
		return response, nil
	}
	if utils.IsBinaryFileContent(content) {
		response.ReadOnlyReason = volumetypes.FileReadOnlyBinary
		return response, nil
	}
	response.Editable = true
	response.Content = string(content)
	return response, nil
}

func classifyVolumeWorkspaceExecErrorInternal(err error, stderr, fallbackContext string) error {
	message := stderr + " " + err.Error()
	switch {
	case strings.Contains(message, "ARCANE_SYMLINK"):
		return common.Classify(common.ErrVolumeFileForbidden, errors.New("symlink volume paths are not supported"))
	case strings.Contains(message, "ARCANE_NOT_FOUND"):
		return common.Classify(common.ErrVolumeFileNotFound, errors.New("volume file not found"))
	case strings.Contains(message, "ARCANE_COLLISION"):
		return common.Classify(common.ErrVolumeFileBadRequest, errors.New("volume file destination already exists"))
	case strings.Contains(message, "ARCANE_NOT_DIRECTORY"):
		return common.Classify(common.ErrVolumeFileBadRequest, errors.New("volume file parent is not a directory"))
	case strings.Contains(message, "ARCANE_NOT_FILE"):
		return common.Classify(common.ErrVolumeFileBadRequest, errors.New("volume file path is not a regular file"))
	case strings.Contains(message, "ARCANE_NOT_EMPTY"):
		return common.Classify(common.ErrVolumeFileBadRequest, errors.New("volume directory is not empty; recursive delete is required"))
	case strings.Contains(message, "ARCANE_DIRECTORY"):
		return common.Classify(common.ErrVolumeFileBadRequest, errors.New("path is a directory"))
	default:
		return errors.WrapIf(err, fallbackContext)
	}
}

func classifyVolumeWorkspaceBrowseErrorInternal(err error) error {
	switch {
	case cerrdefs.IsNotFound(err):
		return common.Classify(common.ErrVolumeFileNotFound, err)
	case strings.Contains(err.Error(), "uses a custom mount configuration"):
		return common.Classify(common.ErrVolumeFileBadRequest, err)
	default:
		return err
	}
}

func (s *VolumeService) validateVolumeWorkspacePathInternal(ctx context.Context, containerID, relativePath string, allowMissing bool) error {
	allowMissingValue := "0"
	if allowMissing {
		allowMissingValue = "1"
	}
	script := `set -e
rel="$1"
allow_missing="$2"
cur=/volume
remaining=$rel
while [ -n "$remaining" ]; do
  case "$remaining" in
    */*) segment=${remaining%%/*}; remaining=${remaining#*/} ;;
    *) segment=$remaining; remaining= ;;
  esac
  cur="$cur/$segment"
  if [ -L "$cur" ]; then echo ARCANE_SYMLINK >&2; exit 42; fi
  if [ ! -e "$cur" ]; then
    if [ "$allow_missing" = 1 ]; then exit 0; fi
    echo ARCANE_NOT_FOUND >&2
    exit 44
  fi
  if [ -n "$remaining" ] && [ ! -d "$cur" ]; then echo ARCANE_NOT_DIRECTORY >&2; exit 47; fi
done`
	_, stderr, err := s.execInContainerInternal(ctx, containerID, []string{"sh", "-c", script, "sh", relativePath, allowMissingValue})
	if err != nil {
		return classifyVolumeWorkspaceExecErrorInternal(err, stderr, "validate volume workspace path")
	}
	return nil
}

func (s *VolumeService) UpdateVolumeWorkspace(ctx context.Context, volumeName string, manifest volumetypes.FileUpdateManifest, uploads []*multipart.FileHeader, user models.User) error {
	if len(manifest.FileChanges) == 0 || len(manifest.FileChanges) > 500 {
		return common.Classify(common.ErrVolumeFileBadRequest, errors.New("fileChanges must contain between 1 and 500 changes"))
	}
	if strings.TrimSpace(manifest.FileTreeRevision) == "" {
		return common.Classify(common.ErrVolumeFileBadRequest, errors.New("file tree revision is required"))
	}
	for _, change := range manifest.FileChanges {
		if err := validateVolumeFileChangeInternal(change, uploads); err != nil {
			return common.Classify(common.ErrVolumeFileBadRequest, err)
		}
	}
	if err := s.isBrowsableVolumeInternal(ctx, volumeName); err != nil {
		return classifyVolumeWorkspaceBrowseErrorInternal(err)
	}

	needsBackups := slices.ContainsFunc(manifest.FileChanges, func(change volumetypes.FileChange) bool {
		return change.Operation == volumetypes.FileOpRestoreFile
	})
	containerID, cleanup, err := s.createVolumeWorkspaceMutationContainerInternal(ctx, volumeName, needsBackups)
	if err != nil {
		return err
	}
	defer cleanup()

	dockerClient, err := s.dockerService.GetClient(ctx)
	if err != nil {
		return err
	}
	stagedPaths, err := s.stageVolumeWorkspaceChangesInternal(ctx, dockerClient, containerID, manifest.FileChanges, uploads)
	if err != nil {
		return err
	}

	current, err := s.readVolumeWorkspaceFromContainerInternal(ctx, containerID)
	if err != nil {
		return err
	}
	if err := validateVolumeWorkspaceRevisionInternal(manifest.FileTreeRevision, current.FileTreeRevision); err != nil {
		return err
	}

	scope, err := volumeWorkspaceBackupScopeInternal(manifest.FileChanges)
	if err != nil {
		return common.Classify(common.ErrVolumeFileBadRequest, err)
	}
	for _, relativePath := range scope {
		if err := s.validateVolumeWorkspacePathInternal(ctx, containerID, relativePath, true); err != nil {
			return err
		}
	}
	if err := s.backupVolumeWorkspaceScopeInternal(ctx, containerID, scope); err != nil {
		return err
	}

	if err := applyVolumeWorkspaceChangesTransactionInternal(
		manifest.FileChanges,
		func(index int, change volumetypes.FileChange) error {
			return s.applyVolumeWorkspaceChangeInternal(ctx, containerID, change, stagedPaths[index], volumeName)
		},
		func() error { return s.restoreVolumeWorkspaceScopeInternal(ctx, containerID, scope) },
	); err != nil {
		return err
	}

	if s.eventService != nil {
		metadata := models.JSON{"action": "workspace_update", "fileChangeCount": len(manifest.FileChanges)}
		if logErr := s.eventService.LogVolumeEvent(ctx, models.EventTypeVolumeFileUpdate, volumeName, volumeName, user.ID, user.Username, "0", metadata); logErr != nil {
			slog.WarnContext(ctx, "could not log volume workspace update event", "volume", volumeName, "error", logErr.Error())
		}
	}
	return nil
}

func (s *VolumeService) stageVolumeWorkspaceChangesInternal(ctx context.Context, dockerClient *client.Client, containerID string, changes []volumetypes.FileChange, uploads []*multipart.FileHeader) (map[int]string, error) {
	if _, _, err := s.execInContainerInternal(ctx, containerID, []string{"mkdir", "-p", "/tmp/arcane-workspace"}); err != nil {
		return nil, errors.WrapIf(err, "prepare volume workspace staging directory")
	}

	stagedPaths := make(map[int]string)
	for index, change := range changes {
		if change.Content == nil && change.UploadIndex == nil {
			continue
		}
		stagedPath := fmt.Sprintf("/tmp/arcane-workspace/change-%d", index)
		var reader io.ReadCloser
		var size int64
		var err error
		if change.UploadIndex != nil {
			header := uploads[*change.UploadIndex]
			reader, err = header.Open()
			size = header.Size
		} else {
			reader = io.NopCloser(strings.NewReader(*change.Content))
			size = int64(len(*change.Content))
		}
		if err != nil {
			return nil, errors.WrapIf(err, "open workspace upload")
		}
		copyErr := s.copyVolumeWorkspaceFileToContainerInternal(ctx, dockerClient, containerID, stagedPath, reader, size)
		_ = reader.Close()
		if copyErr != nil {
			return nil, copyErr
		}
		stagedPaths[index] = stagedPath
	}
	return stagedPaths, nil
}

func validateVolumeWorkspaceRevisionInternal(expected, current string) error {
	if strings.TrimSpace(expected) != current {
		return common.Classify(common.ErrVolumeFileConflict, errors.New("volume file tree changed; refresh the workspace and try again"))
	}
	return nil
}

func applyVolumeWorkspaceChangesTransactionInternal(
	changes []volumetypes.FileChange,
	apply func(int, volumetypes.FileChange) error,
	rollback func() error,
) error {
	for index, change := range changes {
		if err := apply(index, change); err != nil {
			if rollbackErr := rollback(); rollbackErr != nil {
				return stderrors.Join(err, errors.WrapIf(rollbackErr, "failed to roll back volume workspace"))
			}
			return err
		}
	}
	return nil
}

func validateVolumeFileChangeInternal(change volumetypes.FileChange, uploads []*multipart.FileHeader) error {
	if _, err := utils.NormalizeRelativePath(change.RelativePath); err != nil {
		return errors.WrapIf(err, "invalid volume file path")
	}
	hasContent := change.Content != nil
	hasUpload := change.UploadIndex != nil
	if hasUpload && (*change.UploadIndex < 0 || *change.UploadIndex >= len(uploads)) {
		return errors.New("uploadIndex does not reference an uploaded file")
	}
	switch change.Operation {
	case volumetypes.FileOpCreateFile, volumetypes.FileOpUpdateFile:
		if hasContent == hasUpload {
			return errors.New("create_file and update_file require exactly one of content or uploadIndex")
		}
	case volumetypes.FileOpCreateFolder, volumetypes.FileOpDelete:
		if hasContent || hasUpload {
			return errors.New("operation does not accept file content")
		}
	case volumetypes.FileOpRename:
		if hasContent || hasUpload {
			return errors.New("rename does not accept file content")
		}
		if _, err := utils.ValidateFileName(change.NewName); err != nil {
			return errors.WrapIf(err, "invalid volume file name")
		}
	case volumetypes.FileOpMove:
		if hasContent || hasUpload {
			return errors.New("move does not accept file content")
		}
		if strings.TrimSpace(change.NewParentPath) != "" {
			if _, err := utils.NormalizeRelativePath(change.NewParentPath); err != nil {
				return errors.WrapIf(err, "invalid destination folder")
			}
		}
	case volumetypes.FileOpRestoreFile:
		if hasContent || hasUpload {
			return errors.New("restore_file does not accept file content")
		}
		if strings.TrimSpace(change.BackupID) == "" {
			return errors.New("restore_file requires backupId")
		}
	default:
		return errors.Errorf("unsupported volume file operation %q", change.Operation)
	}
	return nil
}

func (s *VolumeService) copyVolumeWorkspaceFileToContainerInternal(ctx context.Context, dockerClient *client.Client, containerID, targetPath string, content io.Reader, size int64) error {
	dir, name := path.Split(targetPath)
	pipeReader, pipeWriter := io.Pipe()
	go func() {
		tarWriter := tar.NewWriter(pipeWriter)
		err := tarWriter.WriteHeader(&tar.Header{Name: name, Mode: 0o600, Size: size})
		if err == nil {
			_, err = io.CopyN(tarWriter, content, size)
		}
		if closeErr := tarWriter.Close(); err == nil {
			err = closeErr
		}
		_ = pipeWriter.CloseWithError(err)
	}()
	_, err := dockerClient.CopyToContainer(ctx, containerID, client.CopyToContainerOptions{DestinationPath: dir, Content: pipeReader})
	_ = pipeReader.Close()
	return errors.WrapIf(err, "stage volume workspace file")
}

func (s *VolumeService) createVolumeWorkspaceMutationContainerInternal(ctx context.Context, volumeName string, withBackups bool) (string, func(), error) {
	if !withBackups {
		return s.createTempContainerInternal(ctx, volumeName, false)
	}
	dockerClient, err := s.dockerService.GetClient(ctx)
	if err != nil {
		return "", nil, err
	}
	helperImage, err := getVolumeHelperImageInternal(ctx, s.dockerService, s.imageService, dockerClient)
	if err != nil {
		return "", nil, err
	}
	backupStorage, err := s.resolveUsableBackupStorageMountInternal(ctx, dockerClient, "/backups", true)
	if err != nil {
		return "", nil, err
	}
	config := &container.Config{Image: helperImage, Cmd: []string{"sleep", "infinity"}, NetworkDisabled: true, Labels: volumehelper.Labels()}
	hostConfig := volumehelper.HostConfig(helperImage, []string{volumeName + ":/volume"}, []mount.Mount{backupStorage.mount})
	resp, err := dockerClient.ContainerCreate(ctx, client.ContainerCreateOptions{Config: config, HostConfig: hostConfig})
	if err != nil {
		return "", nil, errors.WrapIf(err, "create volume workspace helper")
	}
	if _, err := dockerClient.ContainerStart(ctx, resp.ID, client.ContainerStartOptions{}); err != nil {
		_, _ = dockerClient.ContainerRemove(ctx, resp.ID, volumehelper.RemoveOptions())
		return "", nil, errors.WrapIf(err, "start volume workspace helper")
	}
	return resp.ID, func() {
		_, _ = dockerClient.ContainerRemove(context.WithoutCancel(ctx), resp.ID, volumehelper.RemoveOptions())
	}, nil
}

func volumeWorkspaceBackupScopeInternal(changes []volumetypes.FileChange) ([]string, error) {
	paths := make([]string, 0, len(changes)*2)
	for _, change := range changes {
		rel, err := utils.NormalizeRelativePath(change.RelativePath)
		if err != nil {
			return nil, err
		}
		paths = append(paths, rel)
		switch change.Operation {
		case volumetypes.FileOpRename:
			newName, err := utils.ValidateFileName(change.NewName)
			if err != nil {
				return nil, err
			}
			paths = append(paths, path.Join(path.Dir(rel), newName))
		case volumetypes.FileOpMove:
			parent := ""
			if strings.TrimSpace(change.NewParentPath) != "" {
				parent, err = utils.NormalizeRelativePath(change.NewParentPath)
				if err != nil {
					return nil, err
				}
			}
			paths = append(paths, path.Join(parent, path.Base(rel)))
		}
	}
	slices.Sort(paths)
	result := make([]string, 0, len(paths))
	for _, candidate := range paths {
		if candidate == "." || candidate == "" {
			continue
		}
		if slices.ContainsFunc(result, func(parent string) bool { return utils.FilePathMatches(candidate, parent) }) {
			continue
		}
		result = append(result, candidate)
	}
	return result, nil
}

func (s *VolumeService) backupVolumeWorkspaceScopeInternal(ctx context.Context, containerID string, scope []string) error {
	args := append([]string{"sh", "-c", `set -e
mkdir -p /tmp/arcane-workspace
tar -cf /tmp/arcane-workspace/backup.tar --files-from /dev/null
for rel do
  cur=/volume
  remaining=$rel
  while [ -n "$remaining" ]; do
    case "$remaining" in
      */*) segment=${remaining%%/*}; remaining=${remaining#*/} ;;
      *) segment=$remaining; remaining= ;;
    esac
    cur="$cur/$segment"
    if [ -L "$cur" ]; then echo ARCANE_SYMLINK >&2; exit 42; fi
    if [ ! -e "$cur" ]; then break; fi
    if [ -n "$remaining" ] && [ ! -d "$cur" ]; then echo ARCANE_NOT_DIRECTORY >&2; exit 47; fi
  done
  target="/volume/$rel"
  if [ -e "$target" ] || [ -L "$target" ]; then tar -rf /tmp/arcane-workspace/backup.tar -C /volume -- "$rel"; fi
done`, "sh"}, scope...)
	_, stderr, err := s.execInContainerInternal(ctx, containerID, args)
	if err != nil {
		return classifyVolumeWorkspaceExecErrorInternal(err, stderr, "back up volume workspace paths")
	}
	return nil
}

func (s *VolumeService) restoreVolumeWorkspaceScopeInternal(ctx context.Context, containerID string, scope []string) error {
	args := append([]string{"sh", "-c", `set -e
for rel do rm -rf -- "/volume/$rel"; done
tar -xf /tmp/arcane-workspace/backup.tar -C /volume`, "sh"}, scope...)
	_, _, err := s.execInContainerInternal(ctx, containerID, args)
	return errors.WrapIf(err, "restore volume workspace paths")
}

func (s *VolumeService) applyVolumeWorkspaceChangeInternal(ctx context.Context, containerID string, change volumetypes.FileChange, stagedPath, volumeName string) error {
	rel, _ := utils.NormalizeRelativePath(change.RelativePath)
	if change.Operation == volumetypes.FileOpRestoreFile {
		return s.restoreVolumeWorkspaceFileInternal(ctx, containerID, volumeName, change.BackupID, rel)
	}
	cmd, err := s.volumeWorkspaceMutationCommandInternal(ctx, containerID, change, stagedPath, rel)
	if err != nil {
		return err
	}
	_, stderr, err := s.execInContainerInternal(ctx, containerID, cmd)
	if err != nil {
		return classifyVolumeWorkspaceExecErrorInternal(err, stderr, "apply volume file change")
	}
	if strings.TrimSpace(stderr) != "" {
		slog.DebugContext(ctx, "volume workspace change stderr", "operation", change.Operation, "path", rel, "stderr", strings.TrimSpace(stderr))
	}
	return nil
}

func (s *VolumeService) volumeWorkspaceMutationCommandInternal(ctx context.Context, containerID string, change volumetypes.FileChange, stagedPath, rel string) ([]string, error) {
	var cmd []string
	switch change.Operation {
	case volumetypes.FileOpCreateFile:
		if err := s.validateVolumeWorkspacePathInternal(ctx, containerID, rel, true); err != nil {
			return nil, err
		}
		cmd = []string{"sh", "-c", `set -e
target="/volume/$1"
if [ -e "$target" ] || [ -L "$target" ]; then echo ARCANE_COLLISION >&2; exit 45; fi
mkdir -p -- "$(dirname "$target")"
install -m 0644 -- "$2" "$target"`, "sh", rel, stagedPath}
	case volumetypes.FileOpCreateFolder:
		if err := s.validateVolumeWorkspacePathInternal(ctx, containerID, rel, true); err != nil {
			return nil, err
		}
		cmd = []string{"sh", "-c", `set -e
target="/volume/$1"
if [ -e "$target" ] || [ -L "$target" ]; then echo ARCANE_COLLISION >&2; exit 45; fi
install -d -m 0755 -- "$target"`, "sh", rel}
	case volumetypes.FileOpUpdateFile:
		if err := s.validateVolumeWorkspacePathInternal(ctx, containerID, rel, false); err != nil {
			return nil, err
		}
		cmd = []string{"sh", "-c", `set -e
target="/volume/$1"
if [ ! -f "$target" ]; then echo ARCANE_NOT_FILE >&2; exit 46; fi
cat -- "$2" > "$target"`, "sh", rel, stagedPath}
	case volumetypes.FileOpRename:
		newName, _ := utils.ValidateFileName(change.NewName)
		target := path.Join(path.Dir(rel), newName)
		if target == rel {
			return nil, common.Classify(common.ErrVolumeFileBadRequest, errors.New("new name must change the file path"))
		}
		if err := s.validateVolumeWorkspacePathInternal(ctx, containerID, rel, false); err != nil {
			return nil, err
		}
		if err := s.validateVolumeWorkspacePathInternal(ctx, containerID, target, true); err != nil {
			return nil, err
		}
		cmd = []string{"sh", "-c", `set -e
source="/volume/$1"
target="/volume/$2"
if [ -e "$target" ] || [ -L "$target" ]; then echo ARCANE_COLLISION >&2; exit 45; fi
mv -- "$source" "$target"`, "sh", rel, target}
	case volumetypes.FileOpMove:
		parent := ""
		if strings.TrimSpace(change.NewParentPath) != "" {
			parent, _ = utils.NormalizeRelativePath(change.NewParentPath)
		}
		target := path.Join(parent, path.Base(rel))
		if target == rel || (parent != "" && utils.FilePathMatches(parent, rel)) {
			return nil, common.Classify(common.ErrVolumeFileBadRequest, errors.New("invalid move destination"))
		}
		if err := s.validateVolumeWorkspacePathInternal(ctx, containerID, rel, false); err != nil {
			return nil, err
		}
		if err := s.validateVolumeWorkspacePathInternal(ctx, containerID, target, true); err != nil {
			return nil, err
		}
		cmd = []string{"sh", "-c", `set -e
source="/volume/$1"
parent="/volume/$2"
target="/volume/$3"
if [ ! -d "$parent" ]; then echo ARCANE_NOT_DIRECTORY >&2; exit 47; fi
if [ -e "$target" ] || [ -L "$target" ]; then echo ARCANE_COLLISION >&2; exit 45; fi
mv -- "$source" "$target"`, "sh", rel, parent, target}
	case volumetypes.FileOpDelete:
		if err := s.validateVolumeWorkspacePathInternal(ctx, containerID, rel, false); err != nil {
			return nil, err
		}
		recursive := "0"
		if change.Recursive {
			recursive = "1"
		}
		cmd = []string{"sh", "-c", `set -e
target="/volume/$1"
if [ "$2" = 1 ]; then rm -rf -- "$target"
elif [ -d "$target" ]; then
  if ! rmdir -- "$target"; then echo ARCANE_NOT_EMPTY >&2; exit 48; fi
else rm -- "$target"
fi`, "sh", rel, recursive}
	}
	return cmd, nil
}

func (s *VolumeService) restoreVolumeWorkspaceFileInternal(ctx context.Context, containerID, volumeName, backupID, rel string) error {
	if err := s.validateVolumeWorkspacePathInternal(ctx, containerID, rel, true); err != nil {
		return err
	}
	filename, err := s.backupArchiveFilenameInternal(backupID)
	if err != nil {
		return common.Classify(common.ErrVolumeFileNotFound, err)
	}
	var backup models.VolumeBackup
	if err := s.db.WithContext(ctx).Where("id = ?", backupID).First(&backup).Error; err != nil {
		return common.Classify(common.ErrVolumeFileNotFound, err)
	}
	if backup.VolumeName != volumeName {
		return common.Classify(common.ErrVolumeFileForbidden, errors.New("backup does not belong to volume"))
	}
	cleaned, err := s.sanitizeBackupPathInternal(rel)
	if err != nil {
		return common.Classify(common.ErrVolumeFileBadRequest, err)
	}
	stderr, err := s.restoreBackupFilesInContainerInternal(ctx, containerID, filename, []string{cleaned})
	if err != nil {
		return classifyVolumeWorkspaceExecErrorInternal(err, stderr, "restore volume workspace file")
	}
	if strings.TrimSpace(stderr) != "" {
		slog.DebugContext(ctx, "volume workspace change stderr", "operation", volumetypes.FileOpRestoreFile, "path", rel, "stderr", strings.TrimSpace(stderr))
	}
	return nil
}
