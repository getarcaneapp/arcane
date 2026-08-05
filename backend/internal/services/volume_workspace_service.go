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
	workspacepkg "github.com/getarcaneapp/arcane/backend/v2/pkg/workspace"
	volumetypes "github.com/getarcaneapp/arcane/types/v2/volume"
	workspacetypes "github.com/getarcaneapp/arcane/types/v2/workspace"
	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/api/types/mount"
	"github.com/moby/moby/client"
)

const volumeWorkspaceTreeScriptInternal = `export LC_ALL=C
depth_budget="$1"
entry_budget="$2"
truncated=0
stop=0

walk() {
  for full in "$1"/..?* "$1"/.[!.]* "$1"/*; do
    case "$stop" in 1) return ;; esac
    metadata=$(stat -c '%s|%y|%A' -- "$full" 2>/dev/null) || continue
    case "$entry_budget" in '') truncated=1; stop=1; return ;; esac

    name=${full##*/}
    case "$2" in '') rel=$name ;; *) rel="$2/$name" ;; esac
    size=${metadata%%|*}
    metadata=${metadata#*|}
    mtime=${metadata%|*}
    mode=${metadata##*|}
    link=
    case "$mode" in
      l*) kind=l; link=$(readlink "$full" 2>/dev/null) || link= ;;
      d*) kind=d ;;
      -*) kind=f ;;
      *) kind=s ;;
    esac
    printf '%s\0%s\0%s\0%s\0%s\0%s\0' "$rel" "$kind" "$size" "$mtime" "$mode" "$link"
    entry_budget=${entry_budget#?}

    case "$kind" in
      d)
        next_depth=${3#?}
        case "$next_depth" in
          '')
            for child in "$full"/..?* "$full"/.[!.]* "$full"/*; do
              if stat -c '%A' -- "$child" >/dev/null 2>&1; then truncated=1; break; fi
            done
            ;;
          *) walk "$full" "$rel" "$next_depth" ;;
        esac
        ;;
    esac
  done
}

walk /volume '' "$depth_budget"
printf 'ARCANE_TREE_END\0%s\0' "$truncated"`

func (s *VolumeService) GetVolumeWorkspace(ctx context.Context, volumeName string) (*workspacetypes.Workspace, error) {
	defer s.workspaceLocks.RLock(volumeName)()

	totalStartedAt := time.Now()
	defer func() {
		slog.DebugContext(ctx, "volume workspace load completed", "volume", volumeName, "total_duration", time.Since(totalStartedAt))
	}()

	inspectionStartedAt := time.Now()
	if err := s.validateVolumeHelperSupportInternal(ctx, volumeName); err != nil {
		return nil, classifyVolumeWorkspaceHelperSupportErrorInternal(err)
	}
	slog.DebugContext(ctx, "volume workspace inspection completed", "volume", volumeName, "duration", time.Since(inspectionStartedAt))

	helperStartedAt := time.Now()
	containerID, cleanup, err := s.acquireVolumeHelperInternal(ctx, volumeName)
	if err != nil {
		return nil, err
	}
	defer cleanup()
	slog.DebugContext(ctx, "volume workspace helper acquired", "volume", volumeName, "container_id", containerID, "duration", time.Since(helperStartedAt))

	scanStartedAt := time.Now()
	workspace, err := s.readVolumeWorkspaceFromContainerInternal(ctx, containerID)
	if err != nil {
		return nil, err
	}
	slog.DebugContext(ctx, "volume workspace tree scan completed", "volume", volumeName, "file_count", len(workspace.Files), "truncated", workspace.FileTreeTruncated, "duration", time.Since(scanStartedAt))
	return workspace, nil
}

func (s *VolumeService) readVolumeWorkspaceFromContainerInternal(ctx context.Context, containerID string) (*workspacetypes.Workspace, error) {
	maxDepth := s.workspaceMaxDepth
	if maxDepth <= 0 {
		maxDepth = 50
	}
	maxEntries := s.workspaceMaxEntries
	if maxEntries <= 0 {
		maxEntries = 10000
	}
	depthBudget := strings.Repeat("d", maxDepth)
	entryBudget := strings.Repeat("e", maxEntries)
	stdout, stderr, err := s.execInContainerInternal(ctx, containerID, []string{"sh", "-c", volumeWorkspaceTreeScriptInternal, "sh", depthBudget, entryBudget})
	if err != nil {
		return nil, classifyVolumeWorkspaceExecErrorInternal(err, stderr, "read volume workspace")
	}
	return parseVolumeWorkspaceTreeInternal(stdout, maxEntries, s.volumeWorkspaceMaxFileSizeBytesInternal())
}

func parseVolumeWorkspaceTreeInternal(stdout string, maxEntries int, configuredMaxFileSizeBytes ...int64) (*workspacetypes.Workspace, error) {
	maxFileSizeBytes := workspacepkg.MaxFileSizeBytes(workspacepkg.DefaultMaxFileSizeMB)
	if len(configuredMaxFileSizeBytes) == 1 && configuredMaxFileSizeBytes[0] > 0 {
		maxFileSizeBytes = configuredMaxFileSizeBytes[0]
	}
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
	entries := make([]workspacetypes.FileEntry, 0, min(len(fields)/6, maxEntries+1))
	classifications := make(map[string]string, min(len(fields)/6, maxEntries+1))
	for i := 0; i+5 < len(fields); i += 6 {
		if fields[i] == "" {
			continue
		}
		entry, classification, err := parseVolumeWorkspaceEntryInternal(fields[i:i+6], maxFileSizeBytes)
		if err != nil {
			return nil, err
		}
		classifications[entry.RelativePath] = classification
		entries = append(entries, entry)
	}

	revisionEntries := slices.Clone(entries)
	slices.SortFunc(revisionEntries, func(a, b workspacetypes.FileEntry) int {
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
	slices.SortFunc(entries, func(a, b workspacetypes.FileEntry) int {
		if a.IsDirectory != b.IsDirectory {
			if a.IsDirectory {
				return -1
			}
			return 1
		}
		return strings.Compare(a.RelativePath, b.RelativePath)
	})

	return &workspacetypes.Workspace{
		Files:             entries,
		FileTreeRevision:  hex.EncodeToString(h.Sum(nil)),
		FileTreeTruncated: truncated,
	}, nil
}

func parseVolumeWorkspaceEntryInternal(fields []string, maxFileSizeBytes int64) (workspacetypes.FileEntry, string, error) {
	relativePath := fields[0]
	size, err := strconv.ParseInt(fields[2], 10, 64)
	if err != nil {
		return workspacetypes.FileEntry{}, "", errors.WrapIf(err, "parse volume workspace file size")
	}
	modTime, err := parseVolumeWorkspaceModTimeInternal(fields[3])
	if err != nil {
		return workspacetypes.FileEntry{}, "", errors.WrapIf(err, "parse volume workspace modification time")
	}

	mode := fields[4]
	classificationKey := "kind:" + fields[1]
	if mode != "" {
		classificationKey = "mode:" + mode[:1]
	}
	classification := "special"
	switch classificationKey {
	case "kind:d", "mode:d":
		classification = "dir"
	case "kind:l", "mode:l":
		classification = "symlink"
	case "kind:f", "mode:-":
		classification = "file"
	}

	entry := workspacetypes.FileEntry{
		ModTime:      modTime,
		Name:         path.Base(relativePath),
		Path:         "/" + relativePath,
		RelativePath: relativePath,
		Mode:         mode,
		LinkTarget:   fields[5],
		Size:         size,
		IsDirectory:  classification == "dir",
		IsSymlink:    classification == "symlink",
	}
	switch classification {
	case "dir":
		entry.Size = 0
		entry.Editable = true
	case "symlink":
		entry.ReadOnlyReason = workspacetypes.FileReadOnlySymlink
	case "special":
		entry.ReadOnlyReason = workspacetypes.FileReadOnlySpecial
	case "file":
		if size > maxFileSizeBytes {
			entry.ReadOnlyReason = workspacetypes.FileReadOnlyTooLarge
		} else {
			entry.Editable = true
		}
	}
	return entry, classification, nil
}

func parseVolumeWorkspaceModTimeInternal(value string) (time.Time, error) {
	trimmed := strings.TrimSpace(value)
	if seconds, err := strconv.ParseFloat(trimmed, 64); err == nil {
		return time.Unix(0, int64(seconds*float64(time.Second))), nil
	}
	return time.Parse("2006-01-02 15:04:05 -0700", trimmed)
}

func (s *VolumeService) volumeWorkspaceMaxFileSizeBytesInternal() int64 {
	if s.workspaceMaxFileSizeBytes <= 0 {
		return workspacepkg.MaxFileSizeBytes(workspacepkg.DefaultMaxFileSizeMB)
	}
	return s.workspaceMaxFileSizeBytes
}

func (s *VolumeService) GetVolumeWorkspaceFile(ctx context.Context, volumeName, relativePath string) (*workspacetypes.FileContent, error) {
	defer s.workspaceLocks.RLock(volumeName)()

	if err := s.validateVolumeHelperSupportInternal(ctx, volumeName); err != nil {
		return nil, classifyVolumeWorkspaceHelperSupportErrorInternal(err)
	}
	rel, err := utils.NormalizeRelativePath(relativePath)
	if err != nil {
		return nil, common.Classify(common.ErrVolumeWorkspaceForbidden, errors.WrapIf(err, "invalid volume workspace path"))
	}
	containerID, cleanup, err := s.acquireVolumeHelperInternal(ctx, volumeName)
	if err != nil {
		return nil, err
	}
	defer cleanup()
	dockerClient, err := s.dockerService.GetClient(ctx)
	if err != nil {
		return nil, err
	}
	statResult, err := dockerClient.ContainerStatPath(ctx, containerID, client.ContainerStatPathOptions{Path: path.Join("/volume", rel)})
	if err != nil {
		if cerrdefs.IsNotFound(err) {
			return nil, common.Classify(common.ErrVolumeWorkspaceNotFound, errors.New("volume workspace file not found"))
		}
		return nil, errors.WrapIf(err, "inspect volume workspace file")
	}
	stat := statResult.Stat
	if stat.Mode&os.ModeSymlink != 0 {
		return &workspacetypes.FileContent{Path: "/" + rel, RelativePath: rel, Name: path.Base(rel), Size: stat.Size, ReadOnlyReason: workspacetypes.FileReadOnlySymlink}, nil
	}
	if stat.Mode.IsDir() {
		return nil, common.Classify(common.ErrVolumeWorkspaceBadRequest, errors.New("path is a directory"))
	}
	if !stat.Mode.IsRegular() {
		return volumeWorkspaceFileContentResponseInternal(rel, "special", stat.Size, nil, s.volumeWorkspaceMaxFileSizeBytesInternal())
	}
	maxFileSizeBytes := s.volumeWorkspaceMaxFileSizeBytesInternal()
	if stat.Size > maxFileSizeBytes {
		return volumeWorkspaceFileContentResponseInternal(rel, "regular", stat.Size, nil, maxFileSizeBytes)
	}

	reader, size, err := s.downloadFileFromContainerInternal(ctx, dockerClient, containerID, path.Join("/volume", rel), func() {})
	if err != nil {
		return nil, errors.WrapIf(err, "read volume workspace file")
	}
	content, readErr := io.ReadAll(io.LimitReader(reader, maxFileSizeBytes+1))
	closeErr := reader.Close()
	if readErr != nil {
		return nil, errors.WrapIf(readErr, "read volume workspace file content")
	}
	if closeErr != nil {
		return nil, errors.WrapIf(closeErr, "close volume workspace file content")
	}
	if int64(len(content)) > maxFileSizeBytes || size > maxFileSizeBytes {
		return volumeWorkspaceFileContentResponseInternal(rel, "regular", size, nil, maxFileSizeBytes)
	}
	return volumeWorkspaceFileContentResponseInternal(rel, "regular", size, content, maxFileSizeBytes)
}

func (s *VolumeService) DownloadVolumeWorkspaceFile(ctx context.Context, volumeName, relativePath string) (io.ReadCloser, int64, error) {
	unlock := s.workspaceLocks.RLock(volumeName)
	if err := s.validateVolumeHelperSupportInternal(ctx, volumeName); err != nil {
		unlock()
		return nil, 0, classifyVolumeWorkspaceHelperSupportErrorInternal(err)
	}
	rel, err := utils.NormalizeRelativePath(relativePath)
	if err != nil {
		unlock()
		return nil, 0, common.Classify(common.ErrVolumeWorkspaceForbidden, errors.WrapIf(err, "invalid volume workspace path"))
	}
	containerID, cleanup, err := s.acquireVolumeHelperInternal(ctx, volumeName)
	if err != nil {
		unlock()
		return nil, 0, err
	}
	cleanupAndUnlock := func() {
		cleanup()
		unlock()
	}
	parent := path.Dir(rel)
	if parent != "." {
		if err := s.validateVolumeWorkspacePathInternal(ctx, containerID, parent, false); err != nil {
			cleanupAndUnlock()
			return nil, 0, err
		}
	}
	dockerClient, err := s.dockerService.GetClient(ctx)
	if err != nil {
		cleanupAndUnlock()
		return nil, 0, err
	}
	return s.downloadFileFromContainerInternal(ctx, dockerClient, containerID, path.Join("/volume", rel), cleanupAndUnlock)
}

func volumeWorkspaceFileContentResponseInternal(relativePath, kind string, size int64, content []byte, maxFileSizeBytes int64) (*workspacetypes.FileContent, error) {
	response := &workspacetypes.FileContent{
		Path:         "/" + relativePath,
		RelativePath: relativePath,
		Name:         path.Base(relativePath),
		MimeType:     http.DetectContentType(content),
		Size:         size,
	}
	if kind == "special" {
		response.ReadOnlyReason = workspacetypes.FileReadOnlySpecial
		return response, nil
	}
	if kind != "regular" {
		return nil, errors.New("invalid volume workspace file kind")
	}
	if size > maxFileSizeBytes {
		response.ReadOnlyReason = workspacetypes.FileReadOnlyTooLarge
		return response, nil
	}
	if !workspacepkg.IsTextContent(content) {
		response.ReadOnlyReason = workspacetypes.FileReadOnlyBinary
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
		return common.Classify(common.ErrVolumeWorkspaceForbidden, errors.New("symlink volume paths are not supported"))
	case strings.Contains(message, "ARCANE_NOT_FOUND"):
		return common.Classify(common.ErrVolumeWorkspaceNotFound, errors.New("volume workspace file not found"))
	case strings.Contains(message, "ARCANE_COLLISION"):
		return common.Classify(common.ErrVolumeWorkspaceBadRequest, errors.New("volume workspace destination already exists"))
	case strings.Contains(message, "ARCANE_NOT_DIRECTORY"):
		return common.Classify(common.ErrVolumeWorkspaceBadRequest, errors.New("volume workspace parent is not a directory"))
	case strings.Contains(message, "ARCANE_NOT_FILE"):
		return common.Classify(common.ErrVolumeWorkspaceBadRequest, errors.New("volume workspace path is not a regular file"))
	case strings.Contains(message, "ARCANE_NOT_EMPTY"):
		return common.Classify(common.ErrVolumeWorkspaceBadRequest, errors.New("volume directory is not empty; recursive delete is required"))
	case strings.Contains(message, "ARCANE_DIRECTORY"):
		return common.Classify(common.ErrVolumeWorkspaceBadRequest, errors.New("path is a directory"))
	default:
		return errors.WrapIf(err, fallbackContext)
	}
}

func classifyVolumeWorkspaceHelperSupportErrorInternal(err error) error {
	switch {
	case cerrdefs.IsNotFound(err):
		return common.Classify(common.ErrVolumeWorkspaceNotFound, err)
	case strings.Contains(err.Error(), "uses a custom mount configuration"):
		return common.Classify(common.ErrVolumeWorkspaceBadRequest, err)
	default:
		return err
	}
}

func (s *VolumeService) validateVolumeWorkspacePathInternal(ctx context.Context, containerID, relativePath string, allowMissing bool) error {
	allowMissingValue := "0"
	if allowMissing {
		allowMissingValue = "1"
	}
	_, stderr, err := s.execInContainerInternal(ctx, containerID, []string{"sh", "-c", volumeWorkspaceValidatePathScriptInternal, "sh", relativePath, allowMissingValue})
	if err != nil {
		return classifyVolumeWorkspaceExecErrorInternal(err, stderr, "validate volume workspace path")
	}
	return nil
}

const volumeWorkspaceValidatePathScriptInternal = `set -e
rel="$1"
allow_missing="$2"
cur=/volume
remaining=$rel
while :; do
  case "$remaining" in '') break ;; esac
  case "$remaining" in
    */*) segment=${remaining%%/*}; remaining=${remaining#*/} ;;
    *) segment=$remaining; remaining= ;;
  esac
  cur="$cur/$segment"
  if mode=$(stat -c '%A' -- "$cur" 2>/dev/null); then
    case "$mode" in l*) echo ARCANE_SYMLINK >&2; exit 42 ;; esac
  else
    case "$allow_missing" in 1) exit 0 ;; esac
    echo ARCANE_NOT_FOUND >&2
    exit 44
  fi
  case "$remaining" in
    '') ;;
    *) case "$mode" in d*) ;; *) echo ARCANE_NOT_DIRECTORY >&2; exit 47 ;; esac ;;
  esac
done`

func (s *VolumeService) UpdateVolumeWorkspace(ctx context.Context, volumeName string, manifest volumetypes.WorkspaceUpdateManifest, uploads map[int][]byte, user models.User) (*workspacetypes.Workspace, error) {
	totalStartedAt := time.Now()
	defer func() {
		slog.DebugContext(ctx, "volume workspace update completed", "volume", volumeName, "file_change_count", len(manifest.FileChanges), "total_duration", time.Since(totalStartedAt))
	}()

	if err := workspacepkg.ValidateUpdateManifest(manifest.FileTreeRevision, len(manifest.FileChanges), 500); err != nil {
		return nil, common.Classify(common.ErrVolumeWorkspaceBadRequest, err)
	}
	uploadReferences := make([]workspacepkg.UploadReference, 0, len(manifest.FileChanges))
	for _, change := range manifest.FileChanges {
		uploadReferences = append(uploadReferences, workspacepkg.UploadReference{Operation: change.Operation, UploadIndex: change.UploadIndex})
		if err := validateVolumeWorkspaceFileChangeInternal(change); err != nil {
			return nil, common.Classify(common.ErrVolumeWorkspaceBadRequest, err)
		}
	}
	if err := workspacepkg.ValidateUploadIndices(uploadReferences, len(uploads), volumetypes.FileOpCreateFile, volumetypes.FileOpUpdateFile); err != nil {
		return nil, common.Classify(common.ErrVolumeWorkspaceBadRequest, err)
	}
	inspectionStartedAt := time.Now()
	if err := s.validateVolumeHelperSupportInternal(ctx, volumeName); err != nil {
		return nil, classifyVolumeWorkspaceHelperSupportErrorInternal(err)
	}
	slog.DebugContext(ctx, "volume workspace inspection completed", "volume", volumeName, "duration", time.Since(inspectionStartedAt))
	defer s.workspaceLocks.Lock(volumeName)()

	needsBackups := slices.ContainsFunc(manifest.FileChanges, func(change volumetypes.WorkspaceFileChange) bool {
		return change.Operation == volumetypes.FileOpRestoreFile
	})
	helperStartedAt := time.Now()
	containerID, cleanup, err := s.createVolumeWorkspaceMutationContainerInternal(ctx, volumeName, needsBackups)
	if err != nil {
		return nil, err
	}
	defer cleanup()
	slog.DebugContext(ctx, "volume workspace mutation helper acquired", "volume", volumeName, "container_id", containerID, "dedicated", needsBackups, "duration", time.Since(helperStartedAt))

	revisionScanStartedAt := time.Now()
	current, err := s.readVolumeWorkspaceFromContainerInternal(ctx, containerID)
	if err != nil {
		return nil, err
	}
	slog.DebugContext(ctx, "volume workspace revision tree scan completed", "volume", volumeName, "file_count", len(current.Files), "truncated", current.FileTreeTruncated, "duration", time.Since(revisionScanStartedAt))
	if err := validateVolumeWorkspaceRevisionInternal(manifest.FileTreeRevision, current.FileTreeRevision); err != nil {
		return nil, err
	}

	stagingStartedAt := time.Now()
	dockerClient, err := s.dockerService.GetClient(ctx)
	if err != nil {
		return nil, err
	}
	stagedFiles, err := s.stageVolumeWorkspaceChangesInternal(ctx, dockerClient, containerID, manifest.FileChanges, uploads)
	if err != nil {
		return nil, err
	}
	slog.DebugContext(ctx, "volume workspace staging completed", "volume", volumeName, "staged_file_count", len(stagedFiles), "duration", time.Since(stagingStartedAt))

	backupStartedAt := time.Now()
	scope, err := volumeWorkspaceBackupScopeInternal(manifest.FileChanges)
	if err != nil {
		return nil, common.Classify(common.ErrVolumeWorkspaceBadRequest, err)
	}
	for _, relativePath := range scope {
		if err := s.validateVolumeWorkspacePathInternal(ctx, containerID, relativePath, true); err != nil {
			return nil, err
		}
	}
	backup, err := s.backupVolumeWorkspaceScopeInternal(ctx, containerID, scope)
	if err != nil {
		return nil, err
	}
	slog.DebugContext(ctx, "volume workspace backup completed", "volume", volumeName, "scope_count", len(scope), "duration", time.Since(backupStartedAt))

	applyStartedAt := time.Now()
	if err := applyVolumeWorkspaceChangesTransactionInternal(
		manifest.FileChanges,
		func(index int, change volumetypes.WorkspaceFileChange) error {
			return s.applyVolumeWorkspaceChangeInternal(ctx, containerID, change, stagedFiles[index], volumeName)
		},
		func() error { return s.restoreVolumeWorkspaceScopeInternal(ctx, containerID, backup) },
	); err != nil {
		return nil, err
	}
	slog.DebugContext(ctx, "volume workspace changes applied", "volume", volumeName, "file_change_count", len(manifest.FileChanges), "duration", time.Since(applyStartedAt))

	finalScanStartedAt := time.Now()
	workspace, err := s.readVolumeWorkspaceFromContainerInternal(ctx, containerID)
	if err != nil {
		return nil, err
	}
	slog.DebugContext(ctx, "volume workspace final tree scan completed", "volume", volumeName, "file_count", len(workspace.Files), "truncated", workspace.FileTreeTruncated, "duration", time.Since(finalScanStartedAt))

	if s.eventService != nil {
		metadata := models.JSON{"action": "workspace_update", "fileChangeCount": len(manifest.FileChanges)}
		if logErr := s.eventService.LogVolumeEvent(ctx, models.EventTypeVolumeWorkspaceUpdate, volumeName, volumeName, user.ID, user.Username, "0", metadata); logErr != nil {
			slog.WarnContext(ctx, "could not log volume workspace update event", "volume", volumeName, "error", logErr.Error())
		}
	}
	return workspace, nil
}

type volumeWorkspaceStagedFileInternal struct {
	path string
	size int64
}

type volumeWorkspaceStagedContentInternal struct {
	name    string
	content io.ReadCloser
	size    int64
}

func (s *VolumeService) stageVolumeWorkspaceChangesInternal(ctx context.Context, dockerClient *client.Client, containerID string, changes []volumetypes.WorkspaceFileChange, uploads map[int][]byte) (map[int]volumeWorkspaceStagedFileInternal, error) {
	if _, _, err := s.execInContainerInternal(ctx, containerID, []string{"sh", "-c", "rm -rf -- /tmp/arcane-workspace && mkdir -p -- /tmp/arcane-workspace"}); err != nil {
		return nil, errors.WrapIf(err, "prepare volume workspace staging directory")
	}

	stagedFiles := make(map[int]volumeWorkspaceStagedFileInternal)
	stagedContents := make([]volumeWorkspaceStagedContentInternal, 0, len(changes))
	for index, change := range changes {
		if change.UploadIndex == nil {
			continue
		}
		stagedPath := fmt.Sprintf("/tmp/arcane-workspace/change-%d", index)
		content := uploads[*change.UploadIndex]
		reader := io.NopCloser(strings.NewReader(string(content)))
		size := int64(len(content))
		stagedFiles[index] = volumeWorkspaceStagedFileInternal{path: stagedPath, size: size}
		stagedContents = append(stagedContents, volumeWorkspaceStagedContentInternal{
			name:    path.Base(stagedPath),
			content: reader,
			size:    size,
		})
	}
	if len(stagedContents) == 0 {
		return stagedFiles, nil
	}
	if err := s.copyVolumeWorkspaceFilesToContainerInternal(ctx, dockerClient, containerID, stagedContents); err != nil {
		return nil, err
	}
	return stagedFiles, nil
}

func validateVolumeWorkspaceRevisionInternal(expected, current string) error {
	if strings.TrimSpace(expected) != current {
		return common.Classify(common.ErrVolumeWorkspaceConflict, errors.New("volume workspace changed; refresh it and try again"))
	}
	return nil
}

func applyVolumeWorkspaceChangesTransactionInternal(
	changes []volumetypes.WorkspaceFileChange,
	apply func(int, volumetypes.WorkspaceFileChange) error,
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

func validateVolumeWorkspaceFileChangeInternal(change volumetypes.WorkspaceFileChange) error {
	if _, err := utils.NormalizeRelativePath(change.RelativePath); err != nil {
		return errors.WrapIf(err, "invalid volume workspace path")
	}
	hasUpload := change.UploadIndex != nil
	switch change.Operation {
	case volumetypes.FileOpCreateFile, volumetypes.FileOpUpdateFile:
		if !hasUpload {
			return errors.New("create_file and update_file require uploadIndex")
		}
	case volumetypes.FileOpCreateFolder, volumetypes.FileOpDelete:
		if hasUpload {
			return errors.New("operation does not accept file content")
		}
	case volumetypes.FileOpRename:
		if hasUpload {
			return errors.New("rename does not accept file content")
		}
		if _, err := utils.ValidateFileName(change.NewName); err != nil {
			return errors.WrapIf(err, "invalid volume workspace file name")
		}
	case volumetypes.FileOpMove:
		if hasUpload {
			return errors.New("move does not accept file content")
		}
		if strings.TrimSpace(change.NewParentPath) != "" {
			if _, err := utils.NormalizeRelativePath(change.NewParentPath); err != nil {
				return errors.WrapIf(err, "invalid destination folder")
			}
		}
	case volumetypes.FileOpRestoreFile:
		if hasUpload {
			return errors.New("restore_file does not accept file content")
		}
		if strings.TrimSpace(change.BackupID) == "" {
			return errors.New("restore_file requires backupId")
		}
	default:
		return errors.Errorf("unsupported volume workspace operation %q", change.Operation)
	}
	return nil
}

func (s *VolumeService) copyVolumeWorkspaceFilesToContainerInternal(ctx context.Context, dockerClient *client.Client, containerID string, contents []volumeWorkspaceStagedContentInternal) error {
	pipeReader, pipeWriter := io.Pipe()
	archiveDone := make(chan error, 1)
	go func() {
		tarWriter := tar.NewWriter(pipeWriter)
		var err error
		for _, stagedContent := range contents {
			if err = tarWriter.WriteHeader(&tar.Header{Name: stagedContent.name, Mode: 0o600, Size: stagedContent.size}); err != nil {
				break
			}
			if _, err = io.CopyN(tarWriter, stagedContent.content, stagedContent.size); err != nil {
				break
			}
		}
		if closeErr := tarWriter.Close(); err == nil {
			err = closeErr
		}
		_ = pipeWriter.CloseWithError(err)
		archiveDone <- err
	}()
	_, copyErr := dockerClient.CopyToContainer(ctx, containerID, client.CopyToContainerOptions{DestinationPath: "/tmp/arcane-workspace", Content: pipeReader})
	_ = pipeReader.Close()
	archiveErr := <-archiveDone
	for _, stagedContent := range contents {
		_ = stagedContent.content.Close()
	}
	if copyErr != nil {
		return errors.WrapIf(copyErr, "stage volume workspace files")
	}
	if archiveErr != nil {
		return errors.WrapIf(archiveErr, "create volume workspace staging archive")
	}
	return nil
}

func (s *VolumeService) createVolumeWorkspaceMutationContainerInternal(ctx context.Context, volumeName string, withBackups bool) (string, func(), error) {
	if !withBackups {
		return s.acquireVolumeHelperInternal(ctx, volumeName)
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

func volumeWorkspaceBackupScopeInternal(changes []volumetypes.WorkspaceFileChange) ([]string, error) {
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

const volumeWorkspaceBackupCreateScriptInternal = `set -e
rel="$1"
archive="$2"
cur=/volume
current=
remaining=$rel
while :; do
  case "$remaining" in '') break ;; esac
  case "$remaining" in
    */*) segment=${remaining%%/*}; remaining=${remaining#*/} ;;
    *) segment=$remaining; remaining= ;;
  esac
  case "$current" in '') current=$segment ;; *) current="$current/$segment" ;; esac
  cur="$cur/$segment"
  if mode=$(stat -c '%A' -- "$cur" 2>/dev/null); then
    case "$mode" in l*) echo ARCANE_SYMLINK >&2; exit 42 ;; esac
  else
    printf 'absent\0%s\0' "$current"
    exit 0
  fi
  case "$remaining" in
    '') ;;
    *) case "$mode" in d*) ;; *) echo ARCANE_NOT_DIRECTORY >&2; exit 47 ;; esac ;;
  esac
done
case "$rel" in
  */*) parent="/volume/${rel%/*}"; entry=${rel##*/} ;;
  *) parent=/volume; entry=$rel ;;
esac
cd "$parent"
tar -cf "$archive" "./$entry"
printf 'present\0'`

func (s *VolumeService) backupVolumeWorkspaceScopeInternal(ctx context.Context, containerID string, scope []string) (*volumeWorkspaceBackupInternal, error) {
	if _, _, err := s.execInContainerInternal(ctx, containerID, []string{"mkdir", "-p", "/tmp/arcane-workspace"}); err != nil {
		return nil, errors.WrapIf(err, "prepare volume workspace backup directory")
	}
	backup := &volumeWorkspaceBackupInternal{
		archives:      make([]volumeWorkspaceBackupArchiveInternal, 0, len(scope)),
		absentEntries: make([]string, 0, len(scope)),
	}
	for index, relativePath := range scope {
		archivePath := fmt.Sprintf("/tmp/arcane-workspace/backup-%d.tar", index)
		stdout, stderr, err := s.execInContainerInternal(ctx, containerID, []string{"sh", "-c", volumeWorkspaceBackupCreateScriptInternal, "sh", relativePath, archivePath})
		if err != nil {
			return nil, classifyVolumeWorkspaceExecErrorInternal(err, stderr, "back up volume workspace path")
		}
		fields := strings.Split(stdout, "\x00")
		switch {
		case len(fields) >= 1 && fields[0] == "present":
			backup.archives = append(backup.archives, volumeWorkspaceBackupArchiveInternal{
				relativePath: relativePath,
				archivePath:  archivePath,
			})
		case len(fields) >= 2 && fields[0] == "absent" && fields[1] != "":
			backup.absentEntries = append(backup.absentEntries, fields[1])
		default:
			return nil, errors.New("invalid volume workspace backup path response")
		}
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
case "$2" in '') ;; *) parent="/volume/$2" ;; esac
if stat -c '%A' -- "$target" >/dev/null 2>&1; then echo ARCANE_COLLISION >&2; exit 45; fi
if parent_mode=$(stat -c '%A' -- "$parent" 2>/dev/null); then
  case "$parent_mode" in
    l*) echo ARCANE_SYMLINK >&2; exit 42 ;;
    d*) ;;
    *) echo ARCANE_NOT_DIRECTORY >&2; exit 47 ;;
  esac
fi
mkdir -p -- "$parent"
parent_mode=$(stat -c '%A' -- "$parent" 2>/dev/null) || { echo ARCANE_NOT_DIRECTORY >&2; exit 47; }
case "$parent_mode" in
  l*) echo ARCANE_SYMLINK >&2; exit 42 ;;
  d*) ;;
  *) echo ARCANE_NOT_DIRECTORY >&2; exit 47 ;;
esac
umask 022
set -C
head -c "$4" "$3" > "$target"`
	volumeWorkspaceCreateFolderScriptInternal = `set -e
target="/volume/$1"
parent=/volume
case "$2" in '') ;; *) parent="/volume/$2" ;; esac
if stat -c '%A' -- "$target" >/dev/null 2>&1; then echo ARCANE_COLLISION >&2; exit 45; fi
if parent_mode=$(stat -c '%A' -- "$parent" 2>/dev/null); then
  case "$parent_mode" in
    l*) echo ARCANE_SYMLINK >&2; exit 42 ;;
    d*) ;;
    *) echo ARCANE_NOT_DIRECTORY >&2; exit 47 ;;
  esac
fi
mkdir -p -- "$parent"
parent_mode=$(stat -c '%A' -- "$parent" 2>/dev/null) || { echo ARCANE_NOT_DIRECTORY >&2; exit 47; }
case "$parent_mode" in
  l*) echo ARCANE_SYMLINK >&2; exit 42 ;;
  d*) ;;
  *) echo ARCANE_NOT_DIRECTORY >&2; exit 47 ;;
esac
umask 022
mkdir -m 0755 -- "$target"`
	volumeWorkspaceUpdateFileScriptInternal = `set -e
target="/volume/$1"
target_mode=$(stat -c '%A' -- "$target" 2>/dev/null) || { echo ARCANE_NOT_FOUND >&2; exit 44; }
case "$target_mode" in
  l*) echo ARCANE_SYMLINK >&2; exit 42 ;;
  -*) ;;
  *) echo ARCANE_NOT_FILE >&2; exit 46 ;;
esac
head -c "$3" "$2" > "$target"`
	volumeWorkspaceRenameScriptInternal = `set -e
source="/volume/$1"
target="/volume/$2"
if stat -c '%A' -- "$target" >/dev/null 2>&1; then echo ARCANE_COLLISION >&2; exit 45; fi
mv -- "$source" "$target"`
	volumeWorkspaceMoveScriptInternal = `set -e
source="/volume/$1"
parent="/volume/$2"
target="/volume/$3"
parent_mode=$(stat -c '%A' -- "$parent" 2>/dev/null) || { echo ARCANE_NOT_DIRECTORY >&2; exit 47; }
case "$parent_mode" in d*) ;; *) echo ARCANE_NOT_DIRECTORY >&2; exit 47 ;; esac
if stat -c '%A' -- "$target" >/dev/null 2>&1; then echo ARCANE_COLLISION >&2; exit 45; fi
mv -- "$source" "$target"`
	volumeWorkspaceDeleteScriptInternal = `set -e
target="/volume/$1"
case "$2" in
  1) rm -rf -- "$target" ;;
  *)
    target_mode=$(stat -c '%A' -- "$target" 2>/dev/null) || { echo ARCANE_NOT_FOUND >&2; exit 44; }
    case "$target_mode" in
      d*) if ! rmdir -- "$target"; then echo ARCANE_NOT_EMPTY >&2; exit 48; fi ;;
      *) rm -- "$target" ;;
    esac
    ;;
esac`
)

func (s *VolumeService) applyVolumeWorkspaceChangeInternal(ctx context.Context, containerID string, change volumetypes.WorkspaceFileChange, stagedFile volumeWorkspaceStagedFileInternal, volumeName string) error {
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
		return classifyVolumeWorkspaceExecErrorInternal(err, stderr, "apply volume workspace change")
	}
	if strings.TrimSpace(stderr) != "" {
		slog.DebugContext(ctx, "volume workspace change stderr", "operation", change.Operation, "path", rel, "stderr", strings.TrimSpace(stderr))
	}
	return nil
}

func (s *VolumeService) volumeWorkspaceMutationCommandInternal(ctx context.Context, containerID string, change volumetypes.WorkspaceFileChange, stagedFile volumeWorkspaceStagedFileInternal, rel string) ([]string, error) {
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
			return nil, common.Classify(common.ErrVolumeWorkspaceBadRequest, errors.New("new name must change the file path"))
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
			return nil, common.Classify(common.ErrVolumeWorkspaceBadRequest, errors.New("invalid move destination"))
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
		return common.Classify(common.ErrVolumeWorkspaceNotFound, err)
	}
	var backup models.VolumeBackup
	if err := s.db.WithContext(ctx).Where("id = ?", backupID).First(&backup).Error; err != nil {
		return common.Classify(common.ErrVolumeWorkspaceNotFound, err)
	}
	if backup.VolumeName != volumeName {
		return common.Classify(common.ErrVolumeWorkspaceForbidden, errors.New("backup does not belong to volume"))
	}
	cleaned, err := s.sanitizeBackupPathInternal(rel)
	if err != nil {
		return common.Classify(common.ErrVolumeWorkspaceBadRequest, err)
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
