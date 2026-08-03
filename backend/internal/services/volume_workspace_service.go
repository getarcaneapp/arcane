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
	"os"
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

//nolint:dupword // Shell control-flow keywords repeat by design.
const volumeWorkspaceTreeScriptInternal = `export LC_ALL=C
depth_budget="$1"
entry_budget="$2"
truncated=0
stop=0

walk() {
  local dir="$1" rel_dir="$2" remaining_depth="$3"
  local full name rel kind metadata size mtime mode link child next_depth
  for full in "$dir"/..?* "$dir"/.[!.]* "$dir"/*; do
    if [ "$stop" = 1 ]; then return; fi
    if [ ! -e "$full" ] && [ ! -L "$full" ]; then continue; fi
    if [ -z "$entry_budget" ]; then
      truncated=1
      stop=1
      return
    fi

    name=${full##*/}
    if [ -n "$rel_dir" ]; then rel="$rel_dir/$name"; else rel="$name"; fi
    if [ -L "$full" ]; then
      kind=l
    elif [ -d "$full" ]; then
      kind=d
    elif [ -f "$full" ]; then
      kind=f
    else
      kind=s
    fi

    metadata=$(stat -c '%s|%y|%A' -- "$full" 2>/dev/null) || continue
    size=${metadata%%|*}
    metadata=${metadata#*|}
    mtime=${metadata%|*}
    mode=${metadata##*|}
    link=
    if [ "$kind" = l ]; then link=$(readlink "$full" 2>/dev/null) || link=; fi
    printf '%s\0%s\0%s\0%s\0%s\0%s\0' "$rel" "$kind" "$size" "$mtime" "$mode" "$link"
    entry_budget=${entry_budget#?}

    if [ "$kind" = d ]; then
      next_depth=${remaining_depth#?}
      if [ -n "$next_depth" ]; then
        walk "$full" "$rel" "$next_depth"
      else
        for child in "$full"/..?* "$full"/.[!.]* "$full"/*; do
          if [ -e "$child" ] || [ -L "$child" ]; then truncated=1; break; fi
        done
      fi
    fi
  done
}

walk /volume '' "$depth_budget"
printf 'ARCANE_TREE_END\0%s\0' "$truncated"`

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
	depthBudget := strings.Repeat("d", maxDepth)
	entryBudget := strings.Repeat("e", maxEntries)
	stdout, stderr, err := s.execInContainerInternal(ctx, containerID, []string{"sh", "-c", volumeWorkspaceTreeScriptInternal, "sh", depthBudget, entryBudget})
	if err != nil {
		return nil, classifyVolumeWorkspaceExecErrorInternal(err, stderr, "read volume workspace")
	}
	return parseVolumeWorkspaceTreeInternal(stdout, maxEntries)
}

func parseVolumeWorkspaceTreeInternal(stdout string, maxEntries int) (*volumetypes.Workspace, error) {
	fields := strings.Split(stdout, "\x00")
	if len(fields) > 0 && fields[len(fields)-1] == "" {
		fields = fields[:len(fields)-1]
	}
	if len(fields) < 2 || fields[len(fields)-2] != "ARCANE_TREE_END" || (fields[len(fields)-1] != "0" && fields[len(fields)-1] != "1") {
		return nil, errors.New("invalid volume workspace truncation marker")
	}
	truncated := fields[len(fields)-1] == "1"
	fields = fields[:len(fields)-2]
	if len(fields)%6 != 0 {
		return nil, errors.New("invalid volume workspace file entry")
	}
	entries := make([]volumetypes.FileEntry, 0, min(len(fields)/6, maxEntries+1))
	classifications := make(map[string]string, min(len(fields)/6, maxEntries+1))
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
		modTime, err := parseVolumeWorkspaceModTimeInternal(fields[i+3])
		if err != nil {
			return nil, errors.WrapIf(err, "parse volume workspace modification time")
		}
		mode := fields[i+4]
		classification := "special"
		classificationKey := "kind:" + kind
		if mode != "" {
			classificationKey = "mode:" + mode[:1]
		}
		switch classificationKey {
		case "kind:d", "mode:d":
			classification = "dir"
		case "kind:l", "mode:l":
			classification = "symlink"
		case "kind:f", "mode:-":
			classification = "file"
		}
		isDirectory := classification == "dir"
		isSymlink := classification == "symlink"
		if isDirectory {
			size = 0
		}
		classifications[relativePath] = classification
		entries = append(entries, volumetypes.FileEntry{
			ModTime:      modTime,
			Name:         path.Base(relativePath),
			Path:         "/" + relativePath,
			RelativePath: relativePath,
			Mode:         mode,
			LinkTarget:   fields[i+5],
			Size:         size,
			IsDirectory:  isDirectory,
			IsSymlink:    isSymlink,
		})
	}

	revisionEntries := slices.Clone(entries)
	slices.SortFunc(revisionEntries, func(a, b volumetypes.FileEntry) int {
		return strings.Compare(a.RelativePath, b.RelativePath)
	})
	truncated = truncated || len(entries) > maxEntries
	if len(revisionEntries) > maxEntries {
		revisionEntries = revisionEntries[:maxEntries]
	}
	entries = slices.Clone(revisionEntries)

	h := sha256.New()
	for _, entry := range revisionEntries {
		utils.WriteFileTreeRevisionEntry(h, entry.RelativePath, classifications[entry.RelativePath], entry.Size, entry.ModTime.UnixNano(), entry.Mode, false)
	}
	slices.SortFunc(entries, func(a, b volumetypes.FileEntry) int {
		if a.IsDirectory != b.IsDirectory {
			if a.IsDirectory {
				return -1
			}
			return 1
		}
		return strings.Compare(a.RelativePath, b.RelativePath)
	})

	return &volumetypes.Workspace{
		Files:             entries,
		FileTreeRevision:  hex.EncodeToString(h.Sum(nil)),
		FileTreeTruncated: truncated,
	}, nil
}

func parseVolumeWorkspaceModTimeInternal(value string) (time.Time, error) {
	trimmed := strings.TrimSpace(value)
	if seconds, err := strconv.ParseFloat(trimmed, 64); err == nil {
		return time.Unix(0, int64(seconds*float64(time.Second))), nil
	}
	return time.Parse("2006-01-02 15:04:05 -0700", trimmed)
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
	if err := s.validateVolumeWorkspacePathInternal(ctx, containerID, rel, false); err != nil {
		return nil, err
	}
	dockerClient, err := s.dockerService.GetClient(ctx)
	if err != nil {
		return nil, err
	}
	statResult, err := dockerClient.ContainerStatPath(ctx, containerID, client.ContainerStatPathOptions{Path: path.Join("/volume", rel)})
	if err != nil {
		if cerrdefs.IsNotFound(err) {
			return nil, common.Classify(common.ErrVolumeFileNotFound, errors.New("volume file not found"))
		}
		return nil, errors.WrapIf(err, "inspect volume workspace file")
	}
	stat := statResult.Stat
	if stat.Mode&os.ModeSymlink != 0 {
		return nil, common.Classify(common.ErrVolumeFileForbidden, errors.New("symlink volume paths are not supported"))
	}
	if stat.Mode.IsDir() {
		return nil, common.Classify(common.ErrVolumeFileBadRequest, errors.New("path is a directory"))
	}
	if !stat.Mode.IsRegular() {
		return volumeWorkspaceFileContentResponseInternal(rel, "special", stat.Size, nil)
	}
	if stat.Size > maxEditableVolumeFileBytes {
		return volumeWorkspaceFileContentResponseInternal(rel, "regular", stat.Size, nil)
	}

	reader, size, err := s.downloadFileFromContainerInternal(ctx, dockerClient, containerID, path.Join("/volume", rel), func() {})
	if err != nil {
		return nil, errors.WrapIf(err, "read volume workspace file")
	}
	content, readErr := io.ReadAll(io.LimitReader(reader, maxEditableVolumeFileBytes+1))
	closeErr := reader.Close()
	if readErr != nil {
		return nil, errors.WrapIf(readErr, "read volume workspace file content")
	}
	if closeErr != nil {
		return nil, errors.WrapIf(closeErr, "close volume workspace file content")
	}
	if int64(len(content)) > maxEditableVolumeFileBytes || size > maxEditableVolumeFileBytes {
		return volumeWorkspaceFileContentResponseInternal(rel, "regular", size, nil)
	}
	return volumeWorkspaceFileContentResponseInternal(rel, "regular", size, content)
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
	stagedFiles, err := s.stageVolumeWorkspaceChangesInternal(ctx, dockerClient, containerID, manifest.FileChanges, uploads)
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
	backup, err := s.backupVolumeWorkspaceScopeInternal(ctx, containerID, scope)
	if err != nil {
		return err
	}

	if err := applyVolumeWorkspaceChangesTransactionInternal(
		manifest.FileChanges,
		func(index int, change volumetypes.FileChange) error {
			return s.applyVolumeWorkspaceChangeInternal(ctx, containerID, change, stagedFiles[index], volumeName)
		},
		func() error { return s.restoreVolumeWorkspaceScopeInternal(ctx, containerID, backup) },
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

type volumeWorkspaceStagedFileInternal struct {
	path string
	size int64
}

func (s *VolumeService) stageVolumeWorkspaceChangesInternal(ctx context.Context, dockerClient *client.Client, containerID string, changes []volumetypes.FileChange, uploads []*multipart.FileHeader) (map[int]volumeWorkspaceStagedFileInternal, error) {
	if _, _, err := s.execInContainerInternal(ctx, containerID, []string{"mkdir", "-p", "/tmp/arcane-workspace"}); err != nil {
		return nil, errors.WrapIf(err, "prepare volume workspace staging directory")
	}

	stagedFiles := make(map[int]volumeWorkspaceStagedFileInternal)
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
		stagedFiles[index] = volumeWorkspaceStagedFileInternal{path: stagedPath, size: size}
	}
	return stagedFiles, nil
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
	return normalizeVolumeWorkspaceScopeInternal(paths), nil
}

func normalizeVolumeWorkspaceScopeInternal(paths []string) []string {
	normalized := slices.Clone(paths)
	slices.Sort(normalized)
	normalized = slices.Compact(normalized)
	result := make([]string, 0, len(normalized))
	for _, candidate := range normalized {
		if candidate == "." || candidate == "" {
			continue
		}
		if slices.ContainsFunc(result, func(parent string) bool { return utils.FilePathMatches(candidate, parent) }) {
			continue
		}
		result = append(result, candidate)
	}
	return result
}

type volumeWorkspaceBackupArchiveInternal struct {
	relativePath string
	archivePath  string
}

type volumeWorkspaceBackupInternal struct {
	archives      []volumeWorkspaceBackupArchiveInternal
	absentEntries []string
}

const volumeWorkspaceBackupInspectScriptInternal = `set -e
rel="$1"
cur=/volume
current=
remaining=$rel
while [ -n "$remaining" ]; do
  case "$remaining" in
    */*) segment=${remaining%%/*}; remaining=${remaining#*/} ;;
    *) segment=$remaining; remaining= ;;
  esac
  if [ -n "$current" ]; then current="$current/$segment"; else current=$segment; fi
  cur="$cur/$segment"
  if [ -L "$cur" ]; then echo ARCANE_SYMLINK >&2; exit 42; fi
  if [ ! -e "$cur" ]; then printf 'absent\0%s\0' "$current"; exit 0; fi
  if [ -n "$remaining" ] && [ ! -d "$cur" ]; then echo ARCANE_NOT_DIRECTORY >&2; exit 47; fi
done
printf 'present\0'`

func (s *VolumeService) inspectVolumeWorkspaceBackupPathInternal(ctx context.Context, containerID, relativePath string) (bool, string, error) {
	stdout, stderr, err := s.execInContainerInternal(ctx, containerID, []string{"sh", "-c", volumeWorkspaceBackupInspectScriptInternal, "sh", relativePath})
	if err != nil {
		return false, "", classifyVolumeWorkspaceExecErrorInternal(err, stderr, "inspect volume workspace backup path")
	}
	fields := strings.Split(stdout, "\x00")
	switch {
	case len(fields) >= 1 && fields[0] == "present":
		return true, "", nil
	case len(fields) >= 2 && fields[0] == "absent" && fields[1] != "":
		return false, fields[1], nil
	default:
		return false, "", errors.New("invalid volume workspace backup path response")
	}
}

func (s *VolumeService) backupVolumeWorkspaceScopeInternal(ctx context.Context, containerID string, scope []string) (*volumeWorkspaceBackupInternal, error) {
	if _, _, err := s.execInContainerInternal(ctx, containerID, []string{"mkdir", "-p", "/tmp/arcane-workspace"}); err != nil {
		return nil, errors.WrapIf(err, "prepare volume workspace backup directory")
	}
	backup := &volumeWorkspaceBackupInternal{
		archives:      make([]volumeWorkspaceBackupArchiveInternal, 0, len(scope)),
		absentEntries: make([]string, 0, len(scope)),
	}
	for index, relativePath := range scope {
		exists, absentPath, err := s.inspectVolumeWorkspaceBackupPathInternal(ctx, containerID, relativePath)
		if err != nil {
			return nil, err
		}
		if !exists {
			backup.absentEntries = append(backup.absentEntries, absentPath)
			continue
		}

		archivePath := fmt.Sprintf("/tmp/arcane-workspace/backup-%d.tar", index)
		parent := path.Dir(relativePath)
		containerParent := "/volume"
		if parent != "." {
			containerParent = path.Join(containerParent, parent)
		}
		archiveEntry := "./" + path.Base(relativePath)
		_, stderr, err := s.execInContainerInternal(ctx, containerID, []string{"tar", "-cf", archivePath, "-C", containerParent, archiveEntry})
		if err != nil {
			return nil, classifyVolumeWorkspaceExecErrorInternal(err, stderr, "back up volume workspace path")
		}
		backup.archives = append(backup.archives, volumeWorkspaceBackupArchiveInternal{
			relativePath: relativePath,
			archivePath:  archivePath,
		})
	}
	backup.absentEntries = normalizeVolumeWorkspaceScopeInternal(backup.absentEntries)
	return backup, nil
}

func volumeWorkspaceRollbackPathsInternal(backup *volumeWorkspaceBackupInternal) []string {
	removePaths := slices.Clone(backup.absentEntries)
	for _, archive := range backup.archives {
		removePaths = append(removePaths, archive.relativePath)
	}
	slices.SortFunc(removePaths, func(a, b string) int {
		depthA := strings.Count(a, "/")
		depthB := strings.Count(b, "/")
		if depthA != depthB {
			return depthB - depthA
		}
		return strings.Compare(b, a)
	})
	return removePaths
}

func (s *VolumeService) restoreVolumeWorkspaceScopeInternal(ctx context.Context, containerID string, backup *volumeWorkspaceBackupInternal) error {
	removePaths := volumeWorkspaceRollbackPathsInternal(backup)
	args := append([]string{"sh", "-c", `set -e
for rel do rm -rf -- "/volume/$rel"; done`, "sh"}, removePaths...)
	if _, stderr, err := s.execInContainerInternal(ctx, containerID, args); err != nil {
		return classifyVolumeWorkspaceExecErrorInternal(err, stderr, "remove failed volume workspace changes")
	}

	for _, archive := range backup.archives {
		parent := path.Dir(archive.relativePath)
		containerParent := "/volume"
		if parent != "." {
			containerParent = path.Join(containerParent, parent)
		}
		if _, stderr, err := s.execInContainerInternal(ctx, containerID, []string{"mkdir", "-p", containerParent}); err != nil {
			return classifyVolumeWorkspaceExecErrorInternal(err, stderr, "recreate volume workspace backup parent")
		}
		if _, stderr, err := s.execInContainerInternal(ctx, containerID, []string{"tar", "-xf", archive.archivePath, "-C", containerParent}); err != nil {
			return classifyVolumeWorkspaceExecErrorInternal(err, stderr, "restore volume workspace backup")
		}
	}
	return nil
}

const (
	volumeWorkspaceCreateFileScriptInternal = `set -e
target="/volume/$1"
parent=/volume
if [ -n "$2" ]; then parent="/volume/$2"; fi
if [ -e "$target" ] || [ -L "$target" ]; then echo ARCANE_COLLISION >&2; exit 45; fi
if [ -e "$parent" ] && [ ! -d "$parent" ]; then echo ARCANE_NOT_DIRECTORY >&2; exit 47; fi
mkdir -p -- "$parent"
if [ -L "$parent" ]; then echo ARCANE_SYMLINK >&2; exit 42; fi
umask 022
set -C
head -c "$4" "$3" > "$target"`
	volumeWorkspaceCreateFolderScriptInternal = `set -e
target="/volume/$1"
parent=/volume
if [ -n "$2" ]; then parent="/volume/$2"; fi
if [ -e "$target" ] || [ -L "$target" ]; then echo ARCANE_COLLISION >&2; exit 45; fi
if [ -e "$parent" ] && [ ! -d "$parent" ]; then echo ARCANE_NOT_DIRECTORY >&2; exit 47; fi
mkdir -p -- "$parent"
if [ -L "$parent" ]; then echo ARCANE_SYMLINK >&2; exit 42; fi
umask 022
mkdir -m 0755 -- "$target"`
	volumeWorkspaceUpdateFileScriptInternal = `set -e
target="/volume/$1"
if [ -L "$target" ]; then echo ARCANE_SYMLINK >&2; exit 42; fi
if [ ! -e "$target" ]; then echo ARCANE_NOT_FOUND >&2; exit 44; fi
if [ ! -f "$target" ]; then echo ARCANE_NOT_FILE >&2; exit 46; fi
head -c "$3" "$2" > "$target"`
	volumeWorkspaceRenameScriptInternal = `set -e
source="/volume/$1"
target="/volume/$2"
if [ -e "$target" ] || [ -L "$target" ]; then echo ARCANE_COLLISION >&2; exit 45; fi
mv -- "$source" "$target"`
	volumeWorkspaceMoveScriptInternal = `set -e
source="/volume/$1"
parent="/volume/$2"
target="/volume/$3"
if [ ! -d "$parent" ]; then echo ARCANE_NOT_DIRECTORY >&2; exit 47; fi
if [ -e "$target" ] || [ -L "$target" ]; then echo ARCANE_COLLISION >&2; exit 45; fi
mv -- "$source" "$target"`
	volumeWorkspaceDeleteScriptInternal = `set -e
target="/volume/$1"
if [ "$2" = 1 ]; then rm -rf -- "$target"
elif [ -d "$target" ]; then
  if ! rmdir -- "$target"; then echo ARCANE_NOT_EMPTY >&2; exit 48; fi
else rm -- "$target"
fi`
)

func (s *VolumeService) applyVolumeWorkspaceChangeInternal(ctx context.Context, containerID string, change volumetypes.FileChange, stagedFile volumeWorkspaceStagedFileInternal, volumeName string) error {
	rel, _ := utils.NormalizeRelativePath(change.RelativePath)
	if change.Operation == volumetypes.FileOpRestoreFile {
		return s.restoreVolumeWorkspaceFileInternal(ctx, containerID, volumeName, change.BackupID, rel)
	}
	cmd, err := s.volumeWorkspaceMutationCommandInternal(ctx, containerID, change, stagedFile, rel)
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

func (s *VolumeService) volumeWorkspaceMutationCommandInternal(ctx context.Context, containerID string, change volumetypes.FileChange, stagedFile volumeWorkspaceStagedFileInternal, rel string) ([]string, error) {
	var cmd []string
	parent := path.Dir(rel)
	if parent == "." {
		parent = ""
	}
	switch change.Operation {
	case volumetypes.FileOpCreateFile:
		if err := s.validateVolumeWorkspacePathInternal(ctx, containerID, rel, true); err != nil {
			return nil, err
		}
		cmd = []string{"sh", "-c", volumeWorkspaceCreateFileScriptInternal, "sh", rel, parent, stagedFile.path, strconv.FormatInt(stagedFile.size, 10)}
	case volumetypes.FileOpCreateFolder:
		if err := s.validateVolumeWorkspacePathInternal(ctx, containerID, rel, true); err != nil {
			return nil, err
		}
		cmd = []string{"sh", "-c", volumeWorkspaceCreateFolderScriptInternal, "sh", rel, parent}
	case volumetypes.FileOpUpdateFile:
		if err := s.validateVolumeWorkspacePathInternal(ctx, containerID, rel, false); err != nil {
			return nil, err
		}
		cmd = []string{"sh", "-c", volumeWorkspaceUpdateFileScriptInternal, "sh", rel, stagedFile.path, strconv.FormatInt(stagedFile.size, 10)}
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
		cmd = []string{"sh", "-c", volumeWorkspaceRenameScriptInternal, "sh", rel, target}
	case volumetypes.FileOpMove:
		parent = ""
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
		cmd = []string{"sh", "-c", volumeWorkspaceMoveScriptInternal, "sh", rel, parent, target}
	case volumetypes.FileOpDelete:
		if err := s.validateVolumeWorkspacePathInternal(ctx, containerID, rel, false); err != nil {
			return nil, err
		}
		recursive := "0"
		if change.Recursive {
			recursive = "1"
		}
		cmd = []string{"sh", "-c", volumeWorkspaceDeleteScriptInternal, "sh", rel, recursive}
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
