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

	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/api/types/mount"
	"github.com/moby/moby/client"

	"kit.getarcane.app"
	"kit.getarcane.app/concurrency"

	"getarcane.dev/arcane/v3/internal/backup"
	"getarcane.dev/arcane/v3/internal/common"
	"getarcane.dev/arcane/v3/internal/database"
	"getarcane.dev/arcane/v3/internal/docker"
	"getarcane.dev/arcane/v3/internal/event"
	"getarcane.dev/arcane/v3/internal/scheduler/runs"
	"getarcane.dev/arcane/v3/pkg/backupbrowser"
	"getarcane.dev/arcane/v3/pkg/projects"
	"getarcane.dev/arcane/v3/pkg/volumes"
	"getarcane.dev/types/v3"
)

type (
	// Dependencies are the backup runtime, storage, and helper-container operations restores run through.
	Dependencies struct {
		Docker          *docker.DockerClientService
		Engine          *backup.Engine
		Events          *event.EventService
		Locks           *concurrency.KeyedMutex
		Run             func(ctx context.Context, backupID string) (*types.VolumeBackup, error)
		Repository      func(ctx context.Context, dockerClient *client.Client, entry *types.VolumeBackup) (backup.Repository, string, error)
		Password        func(ctx context.Context, dockerClient *client.Client, repositories ...backup.Repository) (string, error)
		Containers      *backup.Containers
		SafetyBackup    func(ctx context.Context, volumeName string, user types.Actor) (string, error)
		ArchivePaths    func(ctx context.Context, backupID string) ([]string, error)
		ArchiveFilename func(backupID string) (string, error)
		HelperImage     func(ctx context.Context) (string, error)
		StorageMount    func(ctx context.Context, dockerClient *client.Client, target string, readOnly bool) (mount.Mount, error)
		StopHelper      func(ctx context.Context, volumeName string) error
		HoldLocal       func(ctx context.Context) (context.Context, func(), error)
		TempContainer   func(ctx context.Context, target mount.Mount) (string, func(), error)
		AcquireHelper   func(ctx context.Context, volumeName string) (string, func(), error)
		AcquireRun      func(ctx context.Context, volumeName string) (*runs.Lease, error)
		Exec            func(ctx context.Context, containerID, execUser string, cmd []string) (string, string, error)
	}

	// Service restores volume backups, selected backup files, and uploaded archives into volumes.
	Service struct {
		deps Dependencies
	}

	runOptions struct {
		Write      func(ctx context.Context) error
		User       types.Actor
		VolumeName string
		Repository string
		Create     []client.VolumeCreateOptions
	}

	snapshotSource struct {
		snapshotID string
		password   string
		repository backup.Repository
	}

	uploadWrite struct {
		archive     io.ReadSeeker
		root        *tar.Header
		volumeName  string
		archiveRoot bool
		newVolume   bool
	}

	selectionPlan struct {
		entries      []types.BackupFileEntry
		archivePaths []string
		globalRoot   bool
	}
)

const (
	restoreBackupFilesScript = `set -e
archive="$1"
shift
archive_mode=$(stat -c '%A' -- "$archive" 2>/dev/null) || { echo ARCANE_NOT_FOUND >&2; exit 44; }
case "$archive_mode" in -*) ;; *) echo ARCANE_NOT_FOUND >&2; exit 44 ;; esac
for member do
  if ! tar -tzf "$archive" -- "$member" >/dev/null 2>&1; then echo ARCANE_NOT_FOUND >&2; exit 44; fi
done
tar -xzf "$archive" -C /volume -- "$@"`

	replaceVolumeScript = `set -e
: "${1:?}"
for f in /volume/* /volume/.[!.]* /volume/..?*; do
  case "$f" in "$1") ;; *) rm -rf -- "$f" ;; esac
done
for f in "$1"/* "$1"/.[!.]* "$1"/..?*; do
  if stat -c '%A' -- "$f" >/dev/null 2>&1; then mv -- "$f" /volume/; fi
done
rmdir -- "$1"`

	restoreArchiveScript = `set -e
tmp=$(mktemp -d /volume/.restore_tmp.XXXXXX)
tar -tzf "$1" >/dev/null
tar -xzf "$1" -C "$tmp"
set -- "$tmp"
` + replaceVolumeScript
)

var (
	errBackupRunning   = common.Classify(common.ErrConflict, errors.New("only finished backups can be restored"))
	errVolumeUnchanged = errors.New("the volume was not changed")
)

func NewService(deps Dependencies) *Service {
	return &Service{deps: deps}
}

func (s *Service) RestoreBackup(ctx context.Context, volumeName, backupID string, user types.Actor) error {
	entry, err := s.deps.Run(ctx, backupID)
	if err != nil {
		return err
	}
	if entry.VolumeName != volumeName {
		return common.ErrVolumeBackupWrongVolume
	}
	if entry.Status == types.VolumeBackupStatusRunning {
		return errBackupRunning
	}
	unlock := s.deps.Locks.Lock(volumeName)
	defer unlock()
	dockerClient, err := s.deps.Docker.GetClient(ctx)
	if err != nil {
		return err
	}
	var source snapshotSource
	if entry.Format != types.VolumeBackupFormatArchive {
		if source, err = s.snapshotSource(ctx, dockerClient, entry); err != nil {
			return err
		}
	}
	_, exists, err := backup.InspectVolume(ctx, dockerClient, volumeName)
	if err != nil {
		return fmt.Errorf("failed to inspect volume for restore: %w", err)
	}
	safetyID, err := s.run(ctx, dockerClient, runOptions{
		VolumeName: volumeName,
		User:       user,
		Create:     kit.Ternary(exists, nil, []client.VolumeCreateOptions{{Name: volumeName}}),
		Write: func(ctx context.Context) error {
			if entry.Format == types.VolumeBackupFormatArchive {
				return s.restoreArchiveBackup(ctx, dockerClient, volumeName, backupID, nil)
			}
			return s.restoreSnapshot(ctx, dockerClient, volumeName, source)
		},
		Repository: source.repository.ID,
	})
	if err != nil {
		return err
	}
	metadata := database.JSON{"action": "backup_restore", "backup_id": backupID, "created_volume": !exists}
	if safetyID != "" {
		metadata["pre_restore_backupId"] = safetyID
	}
	if logErr := s.deps.Events.LogResourceEvent(ctx, event.ResourceEvent{
		Type:          event.EventTypeVolumeBackupRestore,
		ResourceType:  "volume",
		ResourceID:    volumeName,
		ResourceName:  volumeName,
		UserID:        user.ID,
		Username:      user.Username,
		EnvironmentID: "0",
		Metadata:      metadata,
	}); logErr != nil {
		slog.WarnContext(ctx, "could not log volume backup restore event", "volume", volumeName, "error", logErr)
	}
	return nil
}

func (s *Service) run(ctx context.Context, dockerClient *client.Client, opts runOptions) (string, error) {
	lease, leaseErr := s.deps.AcquireRun(ctx, opts.VolumeName)
	if leaseErr != nil {
		return "", leaseErr
	}
	defer lease.Release(ctx)
	var stopped []container.Summary
	var safetyID string
	var writeErr error
	steps := backup.RestoreSteps{
		Subject: "the volume",
		Hold: func(ctx context.Context) (context.Context, func(), error) {
			if len(opts.Create) > 0 {
				return ctx, func() {}, nil
			}
			held, releaseLocal, err := s.deps.HoldLocal(ctx)
			if err != nil || opts.Repository == "" {
				return held, releaseLocal, err
			}
			source, releaseSource, err := s.deps.Engine.HoldRepository(held, opts.Repository)
			if err != nil {
				releaseLocal()
				return nil, nil, err
			}
			return source, func() {
				releaseSource()
				releaseLocal()
			}, nil
		},
		Stop: func(ctx context.Context, _ backup.RestoreState) error {
			if len(opts.Create) > 0 {
				return nil
			}
			more, err := s.deps.Containers.StopContainers(ctx, backup.ContainerScope{Volumes: []string{opts.VolumeName}, RefuseArcaneWriters: true}, opts.User)
			stopped = append(stopped, more...)
			return err
		},
		Deploy: func(ctx context.Context) error {
			if len(stopped) == 0 {
				return nil
			}
			left, err := s.deps.Containers.StartContainers(context.WithoutCancel(ctx), stopped, opts.User)
			if err != nil && len(left) > 0 {
				remaining, retryErr := s.deps.Containers.StartContainers(context.WithoutCancel(ctx), left, opts.User)
				left, err = remaining, kit.Ternary(retryErr == nil, nil, errors.Join(err, retryErr))
			}
			stopped = left
			return err
		},
		Safety: func(ctx context.Context) (string, error) {
			if len(opts.Create) > 0 {
				return "", nil
			}
			id, err := s.deps.SafetyBackup(ctx, opts.VolumeName, opts.User)
			safetyID = id
			return id, err
		},
		InUse: func(ctx context.Context) error {
			if len(opts.Create) > 0 {
				return nil
			}
			return backup.CheckNoWriters(ctx, s.deps.Docker, []string{opts.VolumeName})
		},
		Docker: s.deps.Docker,
		Write: func(ctx context.Context, safety string) error {
			if safety == "" {
				writeErr = opts.Write(ctx)
				return writeErr
			}
			if errors.Is(writeErr, errVolumeUnchanged) {
				return nil
			}
			entry, err := s.deps.Run(ctx, safety)
			if err != nil {
				return err
			}
			return s.RestoreSnapshot(ctx, dockerClient, entry, opts.VolumeName)
		},
		DeleteVolume: func(ctx context.Context, name string, _ types.Actor) error {
			if err := s.deps.StopHelper(ctx, name); err != nil {
				return err
			}
			_, err := s.deps.Docker.Call(ctx, func(ctx context.Context, dockerClient *client.Client) (client.VolumeRemoveResult, error) {
				return dockerClient.VolumeRemove(ctx, name, client.VolumeRemoveOptions{})
			})
			return err
		},
		User: opts.User,
	}
	if applyErr := steps.Apply(ctx, backup.RestoreState{
		Redeploy: new(false),
		Create:   opts.Create,
		MayStop:  true,
		Running:  true,
	}); applyErr != nil {
		return safetyID, applyErr
	}
	if deployErr := steps.Deploy(ctx); deployErr != nil {
		return safetyID, fmt.Errorf("the volume was restored, but its containers could not be started again: %w", deployErr)
	}
	return safetyID, nil
}

func (s *Service) snapshotSource(ctx context.Context, dockerClient *client.Client, entry *types.VolumeBackup) (snapshotSource, error) {
	repository, snapshotID, err := s.deps.Repository(ctx, dockerClient, entry)
	if err != nil {
		return snapshotSource{}, err
	}
	password, err := s.deps.Password(ctx, dockerClient, repository)
	if err != nil {
		return snapshotSource{}, err
	}
	return snapshotSource{
		repository: repository,
		snapshotID: snapshotID,
		password:   password,
	}, nil
}

func (s *Service) RestoreSnapshot(ctx context.Context, dockerClient *client.Client, entry *types.VolumeBackup, volumeName string) error {
	source, err := s.snapshotSource(ctx, dockerClient, entry)
	if err != nil {
		return err
	}
	return s.restoreSnapshot(ctx, dockerClient, volumeName, source)
}

func (s *Service) restoreSnapshot(ctx context.Context, dockerClient *client.Client, volumeName string, source snapshotSource) error {
	snapshot, _, err := s.deps.Engine.FindRunSnapshot(ctx, dockerClient, source.repository, source.password, "", source.snapshotID)
	if err != nil {
		return err
	}
	target := mount.Mount{
		Type:   mount.TypeVolume,
		Source: volumeName,
		Target: "/volume",
	}
	owner, recorded := backup.VolumeOwnerFromTags(snapshot.Tags)
	var containerID string
	if recorded {
		helperID, remove, helperErr := s.deps.TempContainer(ctx, target)
		if helperErr != nil {
			return fmt.Errorf("%w (%w)", helperErr, errVolumeUnchanged)
		}
		defer remove()
		if _, _, err = s.deps.Exec(ctx, helperID, "", []string{"sh", "-c", "command -v chown && command -v chmod"}); err != nil {
			return fmt.Errorf("the helper image cannot restore the volume owner, so pull the latest tools image and try again: %w (%w)", err, errVolumeUnchanged)
		}
		containerID = helperID
	}
	if err = s.deps.Engine.RestoreSnapshot(ctx, dockerClient, source.repository, source.password, source.snapshotID, target, backup.RestoreOptions{DeleteExtra: true}); err != nil {
		return fmt.Errorf("failed to restore Rustic snapshot: %w", err)
	}
	if !recorded {
		return nil
	}
	return owner.Apply(ctx, s.deps.Exec, containerID)
}

func (s *Service) resolveRestoreSelection(
	ctx context.Context,
	dockerClient *client.Client,
	entry *types.VolumeBackup,
	repository backup.Repository,
	snapshotID string,
	selection types.BackupRestoreSelection,
) (selectionPlan, error) {
	if selection.SelectAll && strings.TrimSpace(selection.Search) == "" {
		if _, err := backupbrowser.NormalizeSelection(selection, []types.BackupFileEntry{{Path: "", IsDirectory: true}}); err != nil {
			return selectionPlan{}, fmt.Errorf("%w: %w", common.ErrInvalidBackupSelection, err)
		}
		return selectionPlan{globalRoot: true}, nil
	}
	if entry.Format == types.VolumeBackupFormatArchive {
		archivePaths, err := s.deps.ArchivePaths(ctx, entry.ID)
		if err != nil {
			return selectionPlan{}, err
		}
		entries, err := backupbrowser.NormalizeSelection(selection, backupbrowser.BuildEntries(archivePaths, "", true))
		if err != nil {
			return selectionPlan{}, fmt.Errorf("%w: %w", common.ErrInvalidBackupSelection, err)
		}
		return selectionPlan{
			entries:      entries,
			archivePaths: archivePaths,
		}, nil
	}
	password, err := s.deps.Password(ctx, dockerClient, repository)
	if err != nil {
		return selectionPlan{}, err
	}
	list := func(folder string, recursive bool) ([]types.BackupFileEntry, error) {
		paths, listErr := s.deps.Engine.ListSnapshotFiles(ctx, dockerClient, repository, password, snapshotID, folder+"/", recursive)
		return backupbrowser.BuildEntries(paths, folder, recursive), listErr
	}
	eligible, err := backupbrowser.ParentEntries(selection, list)
	if err != nil {
		return selectionPlan{}, err
	}
	entries, err := backupbrowser.NormalizeSelection(selection, eligible)
	if err != nil {
		return selectionPlan{}, fmt.Errorf("%w: %w", common.ErrInvalidBackupSelection, err)
	}
	return selectionPlan{entries: entries}, nil
}

func archiveMembersForSelection(paths []string, selected []types.BackupFileEntry) []string {
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

func (s *Service) restoreSelection(
	ctx context.Context,
	dockerClient *client.Client,
	volumeName, backupID string,
	entry *types.VolumeBackup,
	repository backup.Repository,
	snapshotID string,
	selection selectionPlan,
) error {
	if entry.Format == types.VolumeBackupFormatArchive {
		// nil members restores the whole archive.
		return s.restoreArchiveBackup(ctx, dockerClient, volumeName, backupID, kit.Ternary(selection.globalRoot, nil, archiveMembersForSelection(selection.archivePaths, selection.entries)))
	}
	password, err := s.deps.Password(ctx, dockerClient, repository)
	if err != nil {
		return err
	}
	if selection.globalRoot {
		return s.restoreSnapshot(ctx, dockerClient, volumeName, snapshotSource{repository: repository, snapshotID: snapshotID, password: password})
	}
	target := mount.Mount{Type: mount.TypeVolume, Source: volumeName, Target: "/volume"}
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

func (s *Service) RestoreBackupFiles(ctx context.Context, volumeName, backupID string, selection types.BackupRestoreSelection, user types.Actor) error {
	entry, err := s.deps.Run(ctx, backupID)
	if err != nil {
		return err
	}
	if entry.VolumeName != volumeName {
		return common.ErrVolumeBackupWrongVolume
	}
	if entry.Status == types.VolumeBackupStatusRunning {
		return errBackupRunning
	}
	unlock := s.deps.Locks.Lock(volumeName)
	defer unlock()
	dockerClient, err := s.deps.Docker.GetClient(ctx)
	if err != nil {
		return err
	}
	var repository backup.Repository
	var snapshotID string
	if entry.Format != types.VolumeBackupFormatArchive {
		repository, snapshotID, err = s.deps.Repository(ctx, dockerClient, entry)
		if err != nil {
			return err
		}
	}
	resolvedSelection, err := s.resolveRestoreSelection(ctx, dockerClient, entry, repository, snapshotID, selection)
	if err != nil {
		return err
	}
	preBackupID, err := s.run(ctx, dockerClient, runOptions{
		VolumeName: volumeName,
		User:       user,
		Write: func(ctx context.Context) error {
			return s.restoreSelection(ctx, dockerClient, volumeName, backupID, entry, repository, snapshotID, resolvedSelection)
		},
		Repository: repository.ID,
	})
	if err != nil {
		return err
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
	if logErr := s.deps.Events.LogResourceEvent(ctx, event.ResourceEvent{
		Type:          event.EventTypeVolumeBackupRestoreFiles,
		ResourceType:  "volume",
		ResourceID:    volumeName,
		ResourceName:  volumeName,
		UserID:        user.ID,
		Username:      user.Username,
		EnvironmentID: "0",
		Metadata:      metadata,
	}); logErr != nil {
		slog.WarnContext(ctx, "could not log volume backup restore files event", "volume", volumeName, "error", logErr)
	}
	return nil
}

// restoreArchiveBackup restores a legacy archive into volumeName: all of it when members is nil, otherwise only members.
func (s *Service) restoreArchiveBackup(ctx context.Context, dockerClient *client.Client, volumeName, backupID string, members []string) (err error) {
	filename, err := s.deps.ArchiveFilename(backupID)
	if err != nil {
		return err
	}
	helperImage, err := s.deps.HelperImage(ctx)
	if err != nil {
		return err
	}
	backupMount, err := s.deps.StorageMount(ctx, dockerClient, "/backups", true)
	if err != nil {
		return err
	}
	// A full restore runs the replace script as the container command; a partial one execs into a sleeping helper.
	fullRestoreCmd := []string{
		"sh",
		"-c",
		restoreArchiveScript,
		"sh",
		path.Join("/backups", filename),
	}
	resp, err := s.deps.Docker.Call(ctx, func(ctx context.Context, dockerClient *client.Client) (client.ContainerCreateResult, error) {
		return dockerClient.ContainerCreate(ctx, client.ContainerCreateOptions{
			Config: &container.Config{
				Image:           helperImage,
				Cmd:             kit.Ternary(members == nil, fullRestoreCmd, []string{"sleep", "infinity"}),
				NetworkDisabled: true,
				Labels:          volumes.Labels(),
			},
			HostConfig: volumes.HostConfig(helperImage, []string{volumeName + ":/volume"}, []mount.Mount{backupMount}),
		})
	})
	if err != nil {
		return fmt.Errorf("failed to create restore container: %w", err)
	}
	defer func() {
		_, removeErr := s.deps.Docker.Call(context.WithoutCancel(ctx), func(ctx context.Context, dockerClient *client.Client) (client.ContainerRemoveResult, error) {
			return dockerClient.ContainerRemove(ctx, resp.ID, volumes.RemoveOptions())
		})
		if err != nil && removeErr != nil {
			err = errors.Join(err, fmt.Errorf("remove restore container: %w", removeErr))
		}
	}()
	if _, containerStartErr := s.deps.Docker.Call(ctx, func(ctx context.Context, dockerClient *client.Client) (client.ContainerStartResult, error) {
		return dockerClient.ContainerStart(ctx, resp.ID, client.ContainerStartOptions{})
	}); containerStartErr != nil {
		return fmt.Errorf("failed to start restore container: %w", containerStartErr)
	}
	if members != nil {
		stderr, restoreErr := s.RestoreArchiveMembers(ctx, resp.ID, filename, members)
		if restoreErr != nil {
			return restoreErr
		}
		if strings.TrimSpace(stderr) != "" {
			slog.DebugContext(ctx, "volume service: restore files stderr", "backupId", backupID, "stderr", strings.TrimSpace(stderr))
		}
		return nil
	}
	waitResult := dockerClient.ContainerWait(context.WithoutCancel(ctx), resp.ID, client.ContainerWaitOptions{Condition: container.WaitConditionNotRunning})
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
	args = append(args, "sh", "-c", restoreBackupFilesScript, "sh", path.Join("/backups", filename))
	for _, cleaned := range cleanedPaths {
		args = append(args, "./"+cleaned)
	}
	_, stderr, err := s.deps.Exec(ctx, containerID, "", args)
	if err != nil {
		return stderr, fmt.Errorf("failed to restore files: %w", err)
	}
	return stderr, nil
}

func (s *Service) UploadAndRestore(ctx context.Context, volumeName string, archive io.ReadSeeker, filename string, composeVolume *types.ComposeVolumeTarget, user types.Actor) error {
	slog.DebugContext(ctx, "volume service: upload and restore", "volume", volumeName, "filename", filename, "user", user.ID)

	gzr, err := gzip.NewReader(archive)
	if err != nil {
		return fmt.Errorf("invalid archive: %w", err)
	}
	first, nextErr := tar.NewReader(gzr).Next()
	if nextErr != nil {
		_ = gzr.Close()
		return fmt.Errorf("invalid archive: %w", nextErr)
	}
	_ = gzr.Close()
	// Extracting into an existing directory skips the archive's root entry, so a new volume takes its root owner and mode from it.
	archiveRoot := first.Typeflag == tar.TypeDir && path.Clean(first.Name) == "."

	unlock := s.deps.Locks.Lock(volumeName)
	defer unlock()
	dockerClient, err := s.deps.Docker.GetClient(ctx)
	if err != nil {
		return err
	}
	var create []client.VolumeCreateOptions
	if composeVolume != nil {
		createOptions, optionsErr := projects.ComposeVolumeTargetCreateOptions(volumeName, *composeVolume)
		if optionsErr != nil {
			return optionsErr
		}
		create = []client.VolumeCreateOptions{createOptions}
	}
	newVolume := composeVolume != nil
	preBackupID, err := s.run(ctx, dockerClient, runOptions{
		VolumeName: volumeName,
		User:       user,
		Create:     create,
		Write: func(ctx context.Context) error {
			return s.writeUpload(ctx, dockerClient, uploadWrite{
				archive:     archive,
				root:        first,
				volumeName:  volumeName,
				archiveRoot: archiveRoot,
				newVolume:   newVolume,
			})
		},
	})
	if err != nil {
		return err
	}

	metadata := database.JSON{
		"action":               "backup_upload_restore",
		"filename":             filename,
		"pre_restore_backupId": preBackupID,
		"created_volume":       newVolume,
	}
	if logErr := s.deps.Events.LogResourceEvent(ctx, event.ResourceEvent{
		Type:          event.EventTypeVolumeBackupRestore,
		ResourceType:  "volume",
		ResourceID:    volumeName,
		ResourceName:  volumeName,
		UserID:        user.ID,
		Username:      user.Username,
		EnvironmentID: "0",
		Metadata:      metadata,
	}); logErr != nil {
		slog.WarnContext(ctx, "could not log volume backup upload restore event", "volume", volumeName, "error", logErr.Error())
	}

	return nil
}

func (s *Service) writeUpload(ctx context.Context, dockerClient *client.Client, upload uploadWrite) (err error) {
	containerID, cleanup, err := s.deps.AcquireHelper(ctx, upload.volumeName)
	if err != nil {
		return err
	}
	defer cleanup()

	tmpDir := fmt.Sprintf("/volume/.restore_tmp_%d", time.Now().UnixNano())
	_, stderr, err := s.deps.Exec(ctx, containerID, "", []string{"mkdir", "-p", tmpDir})
	if err != nil {
		return fmt.Errorf("failed to create temp restore dir: %w", err)
	}
	replaceStarted := false
	defer func() {
		if !replaceStarted {
			_, _, cleanupErr := s.deps.Exec(context.WithoutCancel(ctx), containerID, "", []string{"rm", "-rf", "--", tmpDir})
			err = errors.Join(err, cleanupErr)
			if err != nil && cleanupErr == nil && !upload.newVolume {
				err = fmt.Errorf("%w (%w)", err, errVolumeUnchanged)
			}
		}
	}()
	if strings.TrimSpace(stderr) != "" {
		slog.DebugContext(ctx, "volume service: restore temp dir stderr", "volume", upload.volumeName, "stderr", strings.TrimSpace(stderr))
	}

	if _, seekErr := upload.archive.Seek(0, io.SeekStart); seekErr != nil {
		return fmt.Errorf("failed to read uploaded archive: %w", seekErr)
	}
	if _, err = dockerClient.CopyToContainer(ctx, containerID, client.CopyToContainerOptions{DestinationPath: tmpDir, Content: upload.archive}); err != nil {
		return fmt.Errorf("failed to restore from uploaded archive: %w", err)
	}

	if !upload.newVolume || !upload.archiveRoot {
		_, stderr, err = s.deps.Exec(ctx, containerID, "", []string{"sh", "-c", `test -n "$(find "$1" -mindepth 1 -maxdepth 1)"`, "sh", tmpDir})
		if err != nil {
			return fmt.Errorf("uploaded archive appears empty or invalid: %w", err)
		}
		if strings.TrimSpace(stderr) != "" {
			slog.DebugContext(ctx, "volume service: restore validate stderr", "volume", upload.volumeName, "stderr", strings.TrimSpace(stderr))
		}
	}
	if upload.newVolume && upload.archiveRoot {
		owner := backup.VolumeOwner{
			UID:  uint32(upload.root.Uid),
			GID:  uint32(upload.root.Gid),
			Mode: upload.root.Mode,
		}
		if applyErr := owner.Apply(ctx, s.deps.Exec, containerID); applyErr != nil {
			return applyErr
		}
	}

	replaceStarted = true
	_, stderr, err = s.deps.Exec(ctx, containerID, "", []string{"sh", "-c", replaceVolumeScript, "sh", tmpDir})
	if err != nil {
		return fmt.Errorf("failed to replace volume contents: %w", err)
	}
	if strings.TrimSpace(stderr) != "" {
		slog.DebugContext(ctx, "volume service: restore replace stderr", "volume", upload.volumeName, "stderr", strings.TrimSpace(stderr))
	}
	return nil
}
