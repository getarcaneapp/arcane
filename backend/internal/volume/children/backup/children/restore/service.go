package restore

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"path"
	"slices"
	"strings"
	"time"

	"github.com/containerd/errdefs"
	backuptypes "github.com/getarcaneapp/arcane/types/v2/backup"
	usertypes "github.com/getarcaneapp/arcane/types/v2/user"
	"github.com/getarcaneapp/arcane/types/v2/volume"
	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/api/types/mount"
	"github.com/moby/moby/client"

	"github.com/getarcaneapp/arcane/backend/v2/internal/backup"
	"github.com/getarcaneapp/arcane/backend/v2/internal/common"
	"github.com/getarcaneapp/arcane/backend/v2/internal/database"
	"github.com/getarcaneapp/arcane/backend/v2/internal/docker"
	"github.com/getarcaneapp/arcane/backend/v2/internal/event"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/libarcane/volumehelper"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/utils"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/utils/backupbrowser"
)

const restoreBackupFilesScriptInternal = `set -e
archive="$1"
shift
archive_mode=$(stat -c '%A' -- "$archive" 2>/dev/null) || { echo ARCANE_NOT_FOUND >&2; exit 44; }
case "$archive_mode" in -*) ;; *) echo ARCANE_NOT_FOUND >&2; exit 44 ;; esac
for member do
  if ! tar -tzf "$archive" -- "$member" >/dev/null 2>&1; then echo ARCANE_NOT_FOUND >&2; exit 44; fi
done
tar -xzf "$archive" -C /volume -- "$@"`

// Dependencies are the backup runtime, storage, and helper-container operations restores run through.
type Dependencies struct {
	Docker          *docker.DockerClientService
	Engine          *backup.Engine
	Events          *event.EventService
	Locks           *utils.KeyedMutex
	Run             func(ctx context.Context, backupID string) (*volume.Backup, error)
	Repository      func(ctx context.Context, dockerClient *client.Client, entry *volume.Backup) (backup.Repository, string, error)
	Password        func(ctx context.Context, dockerClient *client.Client, repositories ...backup.Repository) (string, error)
	StopContainers  func(ctx context.Context, dockerClient *client.Client, volumeName string, user usertypes.Actor, refuseArcaneWriters bool) ([]container.Summary, error)
	StartContainers func(ctx context.Context, dockerClient *client.Client, stopped []container.Summary, user usertypes.Actor) ([]container.Summary, error)
	SafetyBackup    func(ctx context.Context, volumeName string, user usertypes.Actor) (string, error)
	ArchivePaths    func(ctx context.Context, backupID string) ([]string, error)
	ArchiveFilename func(backupID string) (string, error)
	HelperImage     func(ctx context.Context, dockerClient *client.Client) (string, error)
	StorageMount    func(ctx context.Context, dockerClient *client.Client, target string, readOnly bool) (mount.Mount, error)
	AcquireHelper   func(ctx context.Context, volumeName string) (string, func(), error)
	Exec            func(ctx context.Context, containerID, execUser string, cmd []string) (string, string, error)
}

// Service restores volume backups, selected backup files, and uploaded archives into volumes.
type Service struct {
	deps Dependencies
}

func NewService(deps Dependencies) *Service {
	return &Service{deps: deps}
}

func (s *Service) RestoreBackup(ctx context.Context, volumeName, backupID string, user usertypes.Actor) (err error) {
	entry, err := s.deps.Run(ctx, backupID)
	if err != nil {
		return err
	}
	if entry.VolumeName != volumeName {
		return errors.New("backup does not belong to volume")
	}
	unlock := s.deps.Locks.Lock(volumeName)
	defer unlock()
	dockerClient, err := s.deps.Docker.GetClient(ctx)
	if err != nil {
		return err
	}
	var repository backup.Repository
	var snapshotID, password string
	if entry.Format != volume.BackupFormatArchive {
		repository, snapshotID, err = s.deps.Repository(ctx, dockerClient, entry)
		if err != nil {
			return err
		}
		password, err = s.deps.Password(ctx, dockerClient, repository)
		if err != nil {
			return err
		}
	}
	stopped, err := s.deps.StopContainers(ctx, dockerClient, volumeName, user, true)
	containersStopped := len(stopped) > 0
	defer func() {
		if containersStopped {
			_, restartErr := s.deps.StartContainers(context.WithoutCancel(ctx), dockerClient, stopped, user)
			err = errors.Join(err, restartErr)
		}
	}()
	if err != nil {
		return err
	}
	// A discovered backup may target a volume missing on this instance; create it and skip the safety backup of an empty volume.
	preBackupID := ""
	var createdVolume bool
	if _, inspectErr := dockerClient.VolumeInspect(ctx, volumeName, client.VolumeInspectOptions{}); inspectErr != nil {
		if !errdefs.IsNotFound(inspectErr) {
			return fmt.Errorf("failed to inspect volume for restore: %w", inspectErr)
		}
		if _, createErr := dockerClient.VolumeCreate(ctx, client.VolumeCreateOptions{Name: volumeName}); createErr != nil {
			return fmt.Errorf("failed to create missing volume for restore: %w", createErr)
		}
		createdVolume = true
		slog.InfoContext(ctx, "Created missing volume for restore", "volume", volumeName)
	} else {
		preBackupID, err = s.deps.SafetyBackup(ctx, volumeName, user)
		if err != nil {
			return fmt.Errorf("failed to create pre-restore backup: %w", err)
		}
	}
	if entry.Format == volume.BackupFormatArchive {
		if restoreArchiveBackupErr := s.restoreArchiveBackupInternal(ctx, dockerClient, volumeName, backupID); restoreArchiveBackupErr != nil {
			return restoreArchiveBackupErr
		}
	} else if restoreSnapshotErr := s.deps.Engine.RestoreSnapshot(
		ctx,
		dockerClient,
		repository,
		password,
		snapshotID,
		mount.Mount{
			Type:   mount.TypeVolume,
			Source: volumeName,
			Target: "/volume",
		},
		backup.RestoreOptions{
			DeleteExtra: true,
		},
	); restoreSnapshotErr != nil {
		return fmt.Errorf("failed to restore Rustic snapshot: %w", restoreSnapshotErr)
	}
	if containersStopped {
		stopped, err = s.deps.StartContainers(context.WithoutCancel(ctx), dockerClient, stopped, user)
		containersStopped = len(stopped) > 0
		if err != nil {
			return err
		}
	}
	metadata := database.JSON{"action": "backup_restore", "backup_id": backupID, "created_volume": createdVolume}
	if preBackupID != "" {
		metadata["pre_restore_backupId"] = preBackupID
	}
	if logErr := s.deps.Events.LogVolumeEvent(ctx, event.EventTypeVolumeBackupRestore, volumeName, volumeName, user.ID, user.Username, "0", metadata); logErr != nil {
		slog.WarnContext(ctx, "could not log volume backup restore event", "volume", volumeName, "error", logErr)
	}
	return nil
}

type selectionInternal struct {
	entries      []backuptypes.BackupFileEntry
	archivePaths []string
	globalRoot   bool
}

func (
	s *Service,
) resolveRestoreSelectionInternal(
	ctx context.Context,
	dockerClient *client.Client,
	entry *volume.Backup,
	repository backup.Repository,
	snapshotID string,
	selection backuptypes.RestoreSelection,
) (
	selectionInternal,
	error,
) {
	if selection.SelectAll && strings.TrimSpace(selection.Search) == "" {
		if _, err := backupbrowser.NormalizeSelection(selection, []backuptypes.BackupFileEntry{{Path: "", IsDirectory: true}}); err != nil {
			return selectionInternal{}, fmt.Errorf("%w: %w", common.ErrInvalidBackupSelection, err)
		}
		return selectionInternal{globalRoot: true}, nil
	}
	resolved := selectionInternal{}
	var eligible []backuptypes.BackupFileEntry
	var err error
	if entry.Format == volume.BackupFormatArchive {
		resolved.archivePaths, err = s.deps.ArchivePaths(ctx, entry.ID)
		eligible = backupbrowser.BuildEntries(resolved.archivePaths, "", true)
	} else {
		var listed []string
		password, passwordErr := s.deps.Password(ctx, dockerClient, repository)
		if passwordErr != nil {
			return selectionInternal{}, passwordErr
		}
		listed, err = s.deps.Engine.ListSnapshotFiles(ctx, dockerClient, repository, password, snapshotID, "", true)
		eligible = backupbrowser.BuildEntries(listed, "", true)
	}
	if err != nil {
		return selectionInternal{}, err
	}
	resolved.entries, err = backupbrowser.NormalizeSelection(selection, eligible)
	if err != nil {
		return selectionInternal{}, fmt.Errorf("%w: %w", common.ErrInvalidBackupSelection, err)
	}
	return resolved, nil
}

func archiveMembersForSelectionInternal(paths []string, selected []backuptypes.BackupFileEntry) []string {
	files := make([]string, 0, len(paths))
	for _, raw := range paths {
		directory := strings.HasSuffix(strings.TrimSpace(raw), "/")
		cleaned, err := backupbrowser.NormalizePath(strings.TrimSuffix(strings.TrimPrefix(strings.TrimSpace(raw), "./"), "/"), false)
		if err != nil {
			continue
		}
		for _, entry := range selected {
			if cleaned == entry.Path || (entry.IsDirectory && strings.HasPrefix(cleaned, entry.Path+"/")) {
				if directory {
					cleaned += "/"
				}
				files = append(files, cleaned)
				break
			}
		}
	}
	slices.Sort(files)
	return slices.Compact(files)
}

func (
	s *Service,
) restoreSelectionInternal(
	ctx context.Context,
	dockerClient *client.Client,
	volumeName, backupID string,
	entry *volume.Backup,
	repository backup.Repository,
	snapshotID string,
	selection selectionInternal,
) error {
	if entry.Format == volume.BackupFormatArchive {
		if selection.globalRoot {
			return s.restoreArchiveBackupInternal(ctx, dockerClient, volumeName, backupID)
		}
		members := archiveMembersForSelectionInternal(selection.archivePaths, selection.entries)
		return s.restoreArchiveBackupFilesInternal(ctx, dockerClient, volumeName, backupID, members)
	}
	password, err := s.deps.Password(ctx, dockerClient, repository)
	if err != nil {
		return err
	}
	target := mount.Mount{Type: mount.TypeVolume, Source: volumeName, Target: "/volume"}
	if selection.globalRoot {
		if restoreSnapshotErr := s.deps.Engine.RestoreSnapshot(ctx, dockerClient, repository, password, snapshotID, target, backup.RestoreOptions{DeleteExtra: true}); restoreSnapshotErr != nil {
			return fmt.Errorf("failed to restore Rustic snapshot: %w", restoreSnapshotErr)
		}
		return nil
	}
	for _, selectedEntry := range selection.entries {
		sourcePath := selectedEntry.Path
		if selectedEntry.IsDirectory {
			sourcePath += "/"
		}
		options := backup.RestoreOptions{DeleteExtra: selectedEntry.IsDirectory, SourcePath: sourcePath, DestinationPath: path.Join(target.Target, selectedEntry.Path)}
		if restoreSelectedEntryErr := s.deps.Engine.RestoreSnapshot(ctx, dockerClient, repository, password, snapshotID, target, options); restoreSelectedEntryErr != nil {
			return fmt.Errorf("failed to restore %s from Rustic snapshot: %w", selectedEntry.Path, restoreSelectedEntryErr)
		}
	}
	return nil
}

func (s *Service) RestoreBackupFiles(ctx context.Context, volumeName, backupID string, selection backuptypes.RestoreSelection, user usertypes.Actor) (err error) {
	entry, err := s.deps.Run(ctx, backupID)
	if err != nil {
		return err
	}
	if entry.VolumeName != volumeName {
		return errors.New("backup does not belong to volume")
	}
	unlock := s.deps.Locks.Lock(volumeName)
	defer unlock()
	dockerClient, err := s.deps.Docker.GetClient(ctx)
	if err != nil {
		return err
	}
	var repository backup.Repository
	var snapshotID string
	if entry.Format != volume.BackupFormatArchive {
		repository, snapshotID, err = s.deps.Repository(ctx, dockerClient, entry)
		if err != nil {
			return err
		}
	}
	resolvedSelection, err := s.resolveRestoreSelectionInternal(ctx, dockerClient, entry, repository, snapshotID, selection)
	if err != nil {
		return err
	}
	stopped, err := s.deps.StopContainers(ctx, dockerClient, volumeName, user, true)
	containersStopped := len(stopped) > 0
	defer func() {
		if containersStopped {
			_, restartErr := s.deps.StartContainers(context.WithoutCancel(ctx), dockerClient, stopped, user)
			err = errors.Join(err, restartErr)
		}
	}()
	if err != nil {
		return err
	}
	preBackupID, err := s.deps.SafetyBackup(ctx, volumeName, user)
	if err != nil {
		return fmt.Errorf("failed to create pre-restore backup: %w", err)
	}
	if restoreSelectionErr := s.restoreSelectionInternal(
		ctx,
		dockerClient,
		volumeName,
		backupID,
		entry,
		repository,
		snapshotID,
		resolvedSelection,
	); restoreSelectionErr != nil {
		return restoreSelectionErr
	}
	if containersStopped {
		stopped, err = s.deps.StartContainers(context.WithoutCancel(ctx), dockerClient, stopped, user)
		containersStopped = len(stopped) > 0
		if err != nil {
			return err
		}
	}
	metadata := database.JSON{
		"action":               "backup_restore_files",
		"backup_id":            backupID,
		"pre_restore_backupId": preBackupID,
		"paths_count": len(
			resolvedSelection.entries,
		),
		"select_all": selection.SelectAll,
		"search":     selection.Search,
	}
	if logErr := s.deps.Events.LogVolumeEvent(ctx, event.EventTypeVolumeBackupRestoreFiles, volumeName, volumeName, user.ID, user.Username, "0", metadata); logErr != nil {
		slog.WarnContext(ctx, "could not log volume backup restore files event", "volume", volumeName, "error", logErr)
	}
	return nil
}

func (s *Service) restoreArchiveBackupInternal(ctx context.Context, dockerClient *client.Client, volumeName, backupID string) error {
	filename, err := s.deps.ArchiveFilename(backupID)
	if err != nil {
		return err
	}
	helperImage, err := s.deps.HelperImage(ctx, dockerClient)
	if err != nil {
		return err
	}
	backupMount, err := s.deps.StorageMount(ctx, dockerClient, "/backups", true)
	if err != nil {
		return err
	}
	config := &container.Config{
		Image: helperImage,
		Cmd: []string{
			"sh",
			"-c",
			fmt.Sprintf(
				"set -e; tmp=$(mktemp -d /volume/.restore_tmp.XXXXXX); tar -tzf /backups/%s >/dev/null; tar -xzf "+
					"/backups/%s -C \"$tmp\"; find /volume -mindepth 1 -maxdepth 1 -not -path \"$tmp\" -exec rm -rf -- {} +; "+
					"find \"$tmp\" -mindepth 1 -maxdepth 1 -exec mv -- {} /volume/ \\;; rmdir \"$tmp\"",
				filename,
				filename,
			),
		},
		Labels: volumehelper.Labels(),
	}
	hostConfig := volumehelper.HostConfig(helperImage, []string{
		volumeName + ":/volume",
	}, []mount.Mount{backupMount})
	resp, err := dockerClient.ContainerCreate(ctx, client.ContainerCreateOptions{
		Config:     config,
		HostConfig: hostConfig,
	})
	if err != nil {
		return fmt.Errorf("failed to create restore container: %w", err)
	}
	defer func() {
		_, _ = dockerClient.ContainerRemove(context.WithoutCancel(ctx), resp.ID, volumehelper.RemoveOptions())
	}()
	if _, containerStartErr := dockerClient.ContainerStart(ctx, resp.ID, client.ContainerStartOptions{}); containerStartErr != nil {
		return fmt.Errorf("failed to start restore container: %w", containerStartErr)
	}
	waitResult := dockerClient.ContainerWait(ctx, resp.ID, client.ContainerWaitOptions{Condition: container.WaitConditionNotRunning})
	var waitBody container.WaitResponse
	select {
	case waitHelperErr := <-waitResult.Error:
		if waitHelperErr != nil {
			return waitHelperErr
		}
	case waitBody = <-waitResult.Result:
	}
	if waitBody.StatusCode != 0 {
		return fmt.Errorf("restore container exited with code %d (volume may be partially wiped)", waitBody.StatusCode)
	}
	return nil
}

// RestoreArchiveMembers extracts the given members of a legacy archive into the
// container's /volume; the container must mount backup storage at /backups.
func (s *Service) RestoreArchiveMembers(ctx context.Context, containerID, filename string, cleanedPaths []string) (string, error) {
	args := make([]string, 0, len(cleanedPaths)+5)
	args = append(args, "sh", "-c", restoreBackupFilesScriptInternal, "sh", path.Join("/backups", filename))
	for _, cleaned := range cleanedPaths {
		args = append(args, "./"+cleaned)
	}
	_, stderr, err := s.deps.Exec(ctx, containerID, "", args)
	if err != nil {
		return stderr, fmt.Errorf("failed to restore files: %w", err)
	}
	return stderr, nil
}

func (s *Service) restoreArchiveBackupFilesInternal(ctx context.Context, dockerClient *client.Client, volumeName, backupID string, cleanedPaths []string) error {
	filename, err := s.deps.ArchiveFilename(backupID)
	if err != nil {
		return err
	}
	helperImage, err := s.deps.HelperImage(ctx, dockerClient)
	if err != nil {
		return err
	}
	backupMount, err := s.deps.StorageMount(ctx, dockerClient, "/backups", true)
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
	}, []mount.Mount{backupMount})
	resp, err := dockerClient.ContainerCreate(ctx, client.ContainerCreateOptions{
		Config:     config,
		HostConfig: hostConfig,
	})
	if err != nil {
		return fmt.Errorf("failed to create restore container: %w", err)
	}
	defer func() {
		_, _ = dockerClient.ContainerRemove(context.WithoutCancel(ctx), resp.ID, volumehelper.RemoveOptions())
	}()
	if _, containerStartErr := dockerClient.ContainerStart(ctx, resp.ID, client.ContainerStartOptions{}); containerStartErr != nil {
		return fmt.Errorf("failed to start restore container: %w", containerStartErr)
	}
	stderr, err := s.RestoreArchiveMembers(ctx, resp.ID, filename, cleanedPaths)
	if err != nil {
		return fmt.Errorf("failed to restore files: %w", err)
	}
	if strings.TrimSpace(stderr) != "" {
		slog.DebugContext(ctx, "volume service: restore files stderr", "backupId", backupID, "stderr", strings.TrimSpace(stderr))
	}
	return nil
}

func (s *Service) UploadAndRestore(ctx context.Context, volumeName string, archive io.ReadSeeker, filename string, user usertypes.Actor) error {
	slog.DebugContext(ctx, "volume service: upload and restore", "volume", volumeName, "filename", filename, "user", user.ID)

	gzr, err := gzip.NewReader(archive)
	if err != nil {
		return fmt.Errorf("invalid archive: %w", err)
	}
	if _, nextErr := tar.NewReader(gzr).Next(); nextErr != nil {
		_ = gzr.Close()
		return fmt.Errorf("invalid archive: %w", nextErr)
	}
	_ = gzr.Close()

	unlock := s.deps.Locks.Lock(volumeName)
	defer unlock()
	preBackupID, err := s.deps.SafetyBackup(ctx, volumeName, user)
	if err != nil {
		return fmt.Errorf("failed to create pre-restore backup: %w", err)
	}

	dockerClient, err := s.deps.Docker.GetClient(ctx)
	if err != nil {
		return err
	}

	containerID, cleanup, err := s.deps.AcquireHelper(ctx, volumeName)
	if err != nil {
		return err
	}
	defer cleanup()

	tmpDir := fmt.Sprintf("/volume/.restore_tmp_%d", time.Now().UnixNano())
	_, stderr, err := s.deps.Exec(ctx, containerID, "", []string{"mkdir", "-p", tmpDir})
	if err != nil {
		return fmt.Errorf("failed to create temp restore dir: %w", err)
	}
	if strings.TrimSpace(stderr) != "" {
		slog.DebugContext(ctx, "volume service: restore temp dir stderr", "volume", volumeName, "stderr", strings.TrimSpace(stderr))
	}

	if _, seekErr := archive.Seek(0, io.SeekStart); seekErr != nil {
		return fmt.Errorf("failed to read uploaded archive: %w", seekErr)
	}
	_, err = dockerClient.CopyToContainer(ctx, containerID, client.CopyToContainerOptions{
		DestinationPath: tmpDir,
		Content:         archive,
	})
	if err != nil {
		return fmt.Errorf("failed to restore from uploaded archive: %w", err)
	}

	_, stderr, err = s.deps.Exec(ctx, containerID, "", []string{"sh", "-c", fmt.Sprintf("test -n \"$(find %s -mindepth 1 -maxdepth 1 -print -quit)\"", tmpDir)})
	if err != nil {
		return fmt.Errorf("uploaded archive appears empty or invalid: %w", err)
	}
	if strings.TrimSpace(stderr) != "" {
		slog.DebugContext(ctx, "volume service: restore validate stderr", "volume", volumeName, "stderr", strings.TrimSpace(stderr))
	}

	_, stderr, err = s.deps.Exec(ctx, containerID, "", []string{"sh", "-c", "rm -rf /volume/* /volume/.[!.]* /volume/..?* 2>/dev/null || true"})
	if err != nil {
		return fmt.Errorf("failed to clear volume before restore: %w", err)
	}
	if strings.TrimSpace(stderr) != "" {
		slog.DebugContext(ctx, "volume service: restore clear stderr", "volume", volumeName, "stderr", strings.TrimSpace(stderr))
	}

	moveCmd := fmt.Sprintf("find %s -mindepth 1 -maxdepth 1 -exec mv -- {} /volume/ \\; && rmdir %s", tmpDir, tmpDir)
	_, stderr, err = s.deps.Exec(ctx, containerID, "", []string{"sh", "-c", moveCmd})
	if err != nil {
		return fmt.Errorf("failed to move restored files into place: %w", err)
	}
	if strings.TrimSpace(stderr) != "" {
		slog.DebugContext(ctx, "volume service: restore move stderr", "volume", volumeName, "stderr", strings.TrimSpace(stderr))
	}

	metadata := database.JSON{
		"action":               "backup_upload_restore",
		"filename":             filename,
		"pre_restore_backupId": preBackupID,
	}
	if logErr := s.deps.Events.LogVolumeEvent(ctx, event.EventTypeVolumeBackupRestore, volumeName, volumeName, user.ID, user.Username, "0", metadata); logErr != nil {
		slog.WarnContext(ctx, "could not log volume backup upload restore event", "volume", volumeName, "error", logErr.Error())
	}

	return nil
}
