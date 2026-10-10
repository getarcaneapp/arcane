package snapshots

import (
	"cmp"
	"context"
	"database/sql"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"
	"syscall"
	"time"

	backuptypes "github.com/getarcaneapp/arcane/types/v2/backup"
	"github.com/getarcaneapp/arcane/types/v2/recovery"
	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/api/types/mount"
	"github.com/moby/moby/client"
	"go.getarcane.app/acfs"
	"go.getarcane.app/docker"
	"go.getarcane.app/kit/pkg"
	"go.getarcane.app/sys/cgroup"
	"gorm.io/gorm"

	"github.com/getarcaneapp/arcane/backend/v2/internal/backup"
	"github.com/getarcaneapp/arcane/backend/v2/internal/common"
	"github.com/getarcaneapp/arcane/backend/v2/internal/config"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/libarcane"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/libarcane/activity"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/pagination"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/utils/backupbrowser"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/utils/francis"
)

const (
	// RecoveryManifestName and RecoveryRequestName live in Arcane's data
	// directory: the manifest inside every snapshot, the request beside it.
	RecoveryManifestName = ".arcane-recovery.json"
	RecoveryRequestName  = ".arcane-recovery-request.json"

	snapshotDataPath     = "/data"
	snapshotProjectsPath = "/projects"

	recoveryDataRestoreTarget      = "/restore"
	recoveryProjectsRestoreTarget  = "/restore-projects"
	selectiveProjectsRestoreTarget = "/arcane-projects"
)

// Service writes system recovery snapshots and reads their layout and project files.
type Service struct {
	engine              *backup.Engine
	sqlDB               func() (*sql.DB, error)
	actorStore          func() (*gorm.DB, error)
	localRepository     func(ctx context.Context, dockerClient *client.Client, readOnly bool) (backup.Repository, error)
	remoteRepository    func(ctx context.Context, destinationID string) (backup.Repository, error)
	databaseFile        func() (string, error)
	projectsDirectory   func(ctx context.Context) string
	recoveryEnvironment func(ctx context.Context) map[string]string
}

func NewService(
	engine *backup.Engine,
	sqlDB func() (*sql.DB, error),
	actorStore func() (*gorm.DB, error),
	localRepository func(ctx context.Context, dockerClient *client.Client, readOnly bool) (backup.Repository, error),
	remoteRepository func(ctx context.Context, destinationID string) (backup.Repository, error),
	databaseFile func() (string, error),
	projectsDirectory func(ctx context.Context) string,
	recoveryEnvironment func(ctx context.Context) map[string]string,
) *Service {
	return &Service{
		engine:              engine,
		sqlDB:               sqlDB,
		actorStore:          actorStore,
		localRepository:     localRepository,
		remoteRepository:    remoteRepository,
		databaseFile:        databaseFile,
		projectsDirectory:   projectsDirectory,
		recoveryEnvironment: recoveryEnvironment,
	}
}

type systemBackupSnapshotLocation struct {
	name            string
	destination     backuptypes.SystemBackupDestination
	s3DestinationID string
	repository      backup.Repository
	snapshotID      string
}

type systemBackupSnapshot struct {
	systemBackupSnapshotLocation

	layout  snapshotLayout
	entries []backuptypes.BackupFileEntry
}

type systemBackupSafetySnapshot struct {
	systemBackupSnapshotLocation

	layout snapshotLayout
	paths  map[string]struct{}
}

// systemBackupSnapshotSession carries the Docker client and resolved
// recovery key through one browse or restore operation, so the snapshot steps
// below don't each thread them through their signatures.
type systemBackupSnapshotSession struct {
	service      *Service
	dockerClient *client.Client
	recoveryKey  string
}

func (s *Service) snapshotSession(dockerClient *client.Client, recoveryKey string) systemBackupSnapshotSession {
	return systemBackupSnapshotSession{service: s, dockerClient: dockerClient, recoveryKey: recoveryKey}
}

func (s *Service) backupSnapshotLocations(ctx context.Context, dockerClient *client.Client, run backuptypes.SystemBackupRun) ([]systemBackupSnapshotLocation, error) {
	locations := make([]systemBackupSnapshotLocation, 0, 2)
	var setupErr error
	if run.LocalSnapshotID != "" {
		repository, err := s.localRepository(ctx, dockerClient, true)
		if err != nil {
			setupErr = errors.Join(setupErr, fmt.Errorf("open local system backup repository: %w", err))
		} else {
			locations = append(locations, systemBackupSnapshotLocation{
				name: "local", destination: backuptypes.SystemBackupDestinationLocal,
				repository: repository, snapshotID: run.LocalSnapshotID,
			})
		}
	}
	if run.RemoteSnapshotID != "" {
		repository, err := s.remoteRepository(ctx, run.S3DestinationID)
		if err != nil {
			setupErr = errors.Join(setupErr, fmt.Errorf("open S3 system backup repository: %w", err))
		} else {
			locations = append(locations, systemBackupSnapshotLocation{
				name: "S3", destination: backuptypes.SystemBackupDestinationS3, s3DestinationID: run.S3DestinationID,
				repository: repository, snapshotID: run.RemoteSnapshotID,
			})
		}
	}
	if len(locations) == 0 && setupErr == nil {
		setupErr = errors.New("system backup has no Rustic snapshot")
	}
	return locations, setupErr
}

func (session systemBackupSnapshotSession) inspectReadableSnapshot(ctx context.Context, location systemBackupSnapshotLocation) (systemBackupSnapshot, error) {
	layout, err := session.readSnapshotLayout(ctx, location)
	if err != nil {
		return systemBackupSnapshot{}, fmt.Errorf("inspect %s system recovery snapshot: %w", location.name, err)
	}
	return systemBackupSnapshot{systemBackupSnapshotLocation: location, layout: layout}, nil
}

func (session systemBackupSnapshotSession) firstReadableSnapshot(
	ctx context.Context,
	locations []systemBackupSnapshotLocation,
	setupErr error,
) (
	systemBackupSnapshot,
	error,
) {
	inspectErr := setupErr
	for _, location := range locations {
		snapshot, err := session.inspectReadableSnapshot(ctx, location)
		if err == nil {
			return snapshot, nil
		}
		inspectErr = errors.Join(inspectErr, err)
	}
	if inspectErr == nil {
		inspectErr = errors.New("system backup has no Rustic snapshot")
	}
	return systemBackupSnapshot{}, fmt.Errorf("failed to open system recovery snapshot: %w", inspectErr)
}

func (session systemBackupSnapshotSession) inspectProjectSnapshot(ctx context.Context, location systemBackupSnapshotLocation) (systemBackupSnapshot, error) {
	snapshot, err := session.inspectProjectManifest(ctx, location)
	if err != nil {
		return systemBackupSnapshot{}, err
	}
	listed, err := session.service.engine.ListSnapshotFiles(ctx, session.dockerClient, location.repository, session.recoveryKey, location.snapshotID, snapshot.layout.projectsPath+"/", true)
	if err != nil {
		return systemBackupSnapshot{}, fmt.Errorf("inspect %s project files: %w", location.name, err)
	}
	snapshot.entries = projectEntriesFromSnapshot(listed, snapshot.layout, "", true)
	return snapshot, nil
}

// inspectProjectManifest opens a snapshot for project browsing;
// backups that omitted projects are rejected with an actionable error.
func (session systemBackupSnapshotSession) inspectProjectManifest(ctx context.Context, location systemBackupSnapshotLocation) (systemBackupSnapshot, error) {
	snapshot, err := session.inspectReadableSnapshot(ctx, location)
	if err != nil {
		return systemBackupSnapshot{}, err
	}
	if !snapshot.layout.projectsIncluded() {
		return systemBackupSnapshot{}, errProjectsNotInBackup
	}
	return snapshot, nil
}

func (session systemBackupSnapshotSession) availableProjectSnapshots(
	ctx context.Context,
	locations []systemBackupSnapshotLocation,
	setupErr error,
	firstOnly bool,
) (
	[]systemBackupSnapshot,
	error,
) {
	snapshots := make([]systemBackupSnapshot, 0, len(locations))
	inspectErr := setupErr
	for _, location := range locations {
		snapshot, err := session.inspectProjectSnapshot(ctx, location)
		if err != nil {
			inspectErr = errors.Join(inspectErr, err)
			continue
		}
		snapshots = append(snapshots, snapshot)
		if firstOnly {
			break
		}
	}
	if len(snapshots) == 0 {
		if inspectErr == nil {
			inspectErr = errors.New("system backup has no Rustic snapshot")
		}
		return nil, fmt.Errorf("failed to open project files in system recovery snapshot: %w", inspectErr)
	}
	return snapshots, nil
}

func snapshotRelativePath(filePath, snapshotPath string) (string, bool) {
	cleanedFile := path.Clean("/" + strings.TrimPrefix(strings.TrimSpace(filePath), "/"))
	cleanedRoot := path.Clean("/" + strings.TrimPrefix(strings.TrimSpace(snapshotPath), "/"))
	if cleanedFile == "/" {
		return "", false
	}
	if cleanedRoot == "/" {
		return strings.TrimPrefix(cleanedFile, "/"), true
	}
	prefix := cleanedRoot + "/"
	if !strings.HasPrefix(cleanedFile, prefix) {
		return "", false
	}
	return strings.TrimPrefix(cleanedFile, prefix), true
}

// projectEntriesFromSnapshot maps a snapshot listing to entries
// relative to the projects root, dropping protected Arcane data files.
func projectEntriesFromSnapshot(files []string, layout snapshotLayout, browsePath string, recursive bool) []backuptypes.BackupFileEntry {
	eligible := make([]string, 0, len(files))
	for _, file := range files {
		projectRelative, ok := snapshotRelativePath(file, layout.projectsPath)
		if !ok || layout.protected(projectRelative) {
			continue
		}
		normalized, err := kit.NormalizeRelativePath(projectRelative)
		if err != nil {
			continue
		}
		if strings.HasSuffix(strings.TrimSpace(file), "/") {
			normalized += "/"
		}
		eligible = append(eligible, normalized)
	}
	return backupbrowser.BuildEntries(eligible, browsePath, recursive)
}

// Browse returns one page from the backup's logical project tree.
func (s *Service) Browse(
	ctx context.Context,
	dockerClient *client.Client,
	key string,
	run backuptypes.SystemBackupRun,
	listPath string,
	recursive bool,
	params pagination.QueryParams,
) ([]backuptypes.BackupFileEntry, pagination.Response, error) {
	session := s.snapshotSession(dockerClient, key)
	locations, setupErr := s.backupSnapshotLocations(ctx, dockerClient, run)
	browseErr := setupErr
	for _, location := range locations {
		snapshot, inspectErr := session.inspectProjectManifest(ctx, location)
		if inspectErr != nil {
			browseErr = errors.Join(browseErr, inspectErr)
			continue
		}
		snapshotRoot := path.Join(snapshot.layout.projectsPath, listPath)
		listed, listErr := s.engine.ListSnapshotFiles(ctx, dockerClient, location.repository, key, location.snapshotID, snapshotRoot+"/", recursive)
		if listErr != nil {
			browseErr = errors.Join(browseErr, fmt.Errorf("browse %s system recovery snapshot: %w", location.name, listErr))
			continue
		}
		entries := projectEntriesFromSnapshot(listed, snapshot.layout, listPath, recursive)
		items, page := backupbrowser.Browse(entries, params)
		return items, page, nil
	}
	return nil, pagination.Response{}, fmt.Errorf("failed to browse project files in system recovery snapshot: %w", browseErr)
}

// openSafetySnapshot resolves the safety run's snapshot in the same
// repository the restore reads from and indexes the project paths it holds.
func (session systemBackupSnapshotSession) openSafetySnapshot(
	ctx context.Context,
	source systemBackupSnapshot,
	safetyRun *backuptypes.SystemBackupRun,
) (
	systemBackupSafetySnapshot,
	error,
) {
	location := source.systemBackupSnapshotLocation
	location.name = "safety"
	if source.destination == backuptypes.SystemBackupDestinationS3 {
		location.snapshotID = safetyRun.RemoteSnapshotID
	} else {
		location.snapshotID = safetyRun.LocalSnapshotID
	}
	if location.snapshotID == "" {
		return systemBackupSafetySnapshot{}, errors.New("pre-restore system backup has no snapshot in the selected repository")
	}
	snapshot, err := session.inspectProjectSnapshot(ctx, location)
	if err != nil {
		return systemBackupSafetySnapshot{}, fmt.Errorf("open pre-restore system backup: %w", err)
	}
	paths := make(map[string]struct{}, len(snapshot.entries))
	for _, entry := range snapshot.entries {
		paths[entry.Path] = struct{}{}
	}
	return systemBackupSafetySnapshot{systemBackupSnapshotLocation: location, layout: snapshot.layout, paths: paths}, nil
}

func (session systemBackupSnapshotSession) restoreEntry(
	ctx context.Context,
	snapshots []systemBackupSnapshot,
	selected backuptypes.BackupFileEntry,
	destination projectsRestoreDestination,
) error {
	var restoreErr error
	for _, snapshot := range snapshots {
		projectsRoot := selected.Path == "" && selected.IsDirectory
		if !projectsRoot && !slices.ContainsFunc(snapshot.entries, func(entry backuptypes.BackupFileEntry) bool {
			return entry.Path == selected.Path && entry.IsDirectory == selected.IsDirectory
		}) {
			continue
		}
		sourcePath := path.Join(snapshot.layout.projectsPath, selected.Path)
		if selected.IsDirectory {
			sourcePath += "/"
		}
		err := session.service.engine.RestoreSnapshot(ctx, session.dockerClient, snapshot.repository, session.recoveryKey, snapshot.snapshotID, destination.target.Mounts[0], backup.RestoreOptions{
			DeleteExtra:     selected.IsDirectory,
			SourcePath:      sourcePath,
			DestinationPath: path.Join(destination.target.Path, selected.Path),
			ExtraMounts:     destination.target.Mounts[1:],
		})
		if err == nil {
			return nil
		}
		restoreErr = errors.Join(restoreErr, fmt.Errorf("restore from %s snapshot: %w", snapshot.name, err))
	}
	if restoreErr == nil {
		restoreErr = errors.New("file is unavailable in every readable system recovery snapshot")
	}
	return restoreErr
}

// safetySnapshotContainsPath reports whether the safety snapshot holds
// a project-relative path; the projects root itself always exists.
func safetySnapshotContainsPath(safety systemBackupSafetySnapshot, projectRelative string) bool {
	if projectRelative == "" {
		return true
	}
	if _, exists := safety.paths[projectRelative]; exists {
		return true
	}
	prefix := strings.TrimSuffix(projectRelative, "/") + "/"
	for candidate := range safety.paths {
		if strings.HasPrefix(candidate, prefix) {
			return true
		}
	}
	return false
}

func (session systemBackupSnapshotSession) rollback(
	ctx context.Context,
	safety systemBackupSafetySnapshot,
	selected []backuptypes.BackupFileEntry,
	destination projectsRestoreDestination,
) error {
	var rollbackErr error
	for _, selectedEntry := range slices.Backward(selected) {
		if !safetySnapshotContainsPath(safety, selectedEntry.Path) {
			if selectedEntry.Path == "" {
				rollbackErr = errors.Join(rollbackErr, errors.New("refusing to remove the projects directory itself"))
				continue
			}
			if err := acfs.RemoveAll(ctx, destination.directory, selectedEntry.Path); err != nil {
				rollbackErr = errors.Join(rollbackErr, fmt.Errorf("remove newly restored project path %s: %w", selectedEntry.Path, err))
			}
			continue
		}
		sourcePath := path.Join(safety.layout.projectsPath, selectedEntry.Path)
		if selectedEntry.IsDirectory {
			sourcePath += "/"
		}
		if err := session.service.engine.RestoreSnapshot(ctx, session.dockerClient, safety.repository, session.recoveryKey, safety.snapshotID, destination.target.Mounts[0], backup.RestoreOptions{
			DeleteExtra:     selectedEntry.IsDirectory,
			SourcePath:      sourcePath,
			DestinationPath: path.Join(destination.target.Path, selectedEntry.Path),
			ExtraMounts:     destination.target.Mounts[1:],
		}); err != nil {
			rollbackErr = errors.Join(rollbackErr, fmt.Errorf("restore project path %s from safety backup: %w", selectedEntry.Path, err))
		}
	}
	return rollbackErr
}

func (session systemBackupSnapshotSession) restoreSelected(
	ctx context.Context,
	snapshots []systemBackupSnapshot,
	safety systemBackupSafetySnapshot,
	selected []backuptypes.BackupFileEntry,
	destination projectsRestoreDestination,
) error {
	restored := make([]backuptypes.BackupFileEntry, 0, len(selected))
	for _, selectedEntry := range selected {
		if err := session.restoreEntry(ctx, snapshots, selectedEntry, destination); err != nil {
			affected := slices.Clone(restored)
			affected = append(affected, selectedEntry)
			rollbackErr := session.rollback(context.WithoutCancel(ctx), safety, affected, destination)
			restoreErr := fmt.Errorf("failed to restore project path %s from system backup: %w", selectedEntry.Path, err)
			if rollbackErr != nil {
				return errors.Join(restoreErr, fmt.Errorf("failed to roll back project files from pre-restore system backup: %w", rollbackErr))
			}
			return fmt.Errorf("%w; affected project files were rolled back", restoreErr)
		}
		restored = append(restored, selectedEntry)
	}
	return nil
}

// RestoreFiles restores selected project files into the current projects
// directory after createSafety snapshots them in the source repository.
func (s *Service) RestoreFiles(
	ctx context.Context,
	dockerClient *client.Client,
	key string,
	run backuptypes.SystemBackupRun,
	selection backuptypes.RestoreSelection,
	createSafety func(context.Context, backuptypes.CreateSystemBackupRequest) (*backuptypes.SystemBackupRun, error),
) error {
	session := s.snapshotSession(dockerClient, key)
	locations, setupErr := s.backupSnapshotLocations(ctx, dockerClient, run)
	snapshots, err := session.availableProjectSnapshots(ctx, locations, setupErr, false)
	if err != nil {
		return err
	}
	selected, err := normalizeSystemBackupSelection(selection, snapshots[0])
	if err != nil {
		return fmt.Errorf("%w: %w", common.ErrInvalidBackupSelection, err)
	}
	destination, err := s.projectsRestoreDestination(ctx, dockerClient)
	if err != nil {
		return err
	}
	// Entries inside Arcane's host-nested data directory, or directories enclosing
	// it, would overwrite or delete the live database through --delete.
	for _, entry := range selected {
		if destination.dataPath == "" {
			break
		}
		encloses := entry.IsDirectory && (entry.Path == "" || kit.FilePathMatches(destination.dataPath, entry.Path))
		if kit.FilePathMatches(entry.Path, destination.dataPath) || encloses {
			return fmt.Errorf(
				"%w: %s contains Arcane's data directory on the host and cannot be restored as a project file; select other entries individually",
				common.ErrInvalidBackupSelection, cmp.Or(entry.Path, "the projects directory"),
			)
		}
	}
	// The safety backup mirrors the restore source so the rollback snapshot
	// lives in the repository the restore is already reading from.
	source := snapshots[0]
	safetyRequest := backuptypes.CreateSystemBackupRequest{Destination: backuptypes.SystemBackupDestinationLocal, RecoveryKey: key}
	if source.destination == backuptypes.SystemBackupDestinationS3 {
		safetyRequest.Destination = backuptypes.SystemBackupDestinationS3
		safetyRequest.S3DestinationID = source.s3DestinationID
	}
	safetyRun, err := createSafety(ctx, safetyRequest)
	if err != nil {
		return fmt.Errorf("failed to create pre-restore system backup: %w", err)
	}
	safety, err := session.openSafetySnapshot(ctx, source, safetyRun)
	if err != nil {
		return err
	}
	return session.restoreSelected(ctx, snapshots, safety, selected, destination)
}

// RestorePlan is the full-restore source snapshot and the stages that put it
// into the current container layout.
type RestorePlan struct {
	SnapshotID       string
	ProjectsIncluded bool
	Stages           []recovery.RestoreStage
}

// PlanRestore opens the first readable snapshot of run and plans its restore stages.
func (s *Service) PlanRestore(
	ctx context.Context,
	dockerClient *client.Client,
	key string,
	run backuptypes.SystemBackupRun,
	mounts []container.MountPoint,
	dataDirectory, projectsDirectory string,
) (RestorePlan, error) {
	session := s.snapshotSession(dockerClient, key)
	locations, setupErr := s.backupSnapshotLocations(ctx, dockerClient, run)
	snapshot, err := session.firstReadableSnapshot(ctx, locations, setupErr)
	if err != nil {
		return RestorePlan{}, err
	}
	repository := recovery.RestoreRepository{Environment: snapshot.repository.Environment, Mounts: snapshot.repository.Mounts}
	stages, err := restoreStages(mounts, dataDirectory, projectsDirectory, repository, snapshot.snapshotID, snapshot.layout)
	if err != nil {
		return RestorePlan{}, fmt.Errorf("plan system restore: %w", err)
	}
	return RestorePlan{SnapshotID: snapshot.snapshotID, ProjectsIncluded: snapshot.layout.projectsIncluded(), Stages: stages}, nil
}

// PlanRollback plans the stages that restore the local safety snapshot if a full restore fails.
func (s *Service) PlanRollback(
	ctx context.Context,
	dockerClient *client.Client,
	key, localSnapshotID string,
	mounts []container.MountPoint,
	dataDirectory, projectsDirectory string,
) ([]recovery.RestoreStage, error) {
	localRepository, err := s.localRepository(ctx, dockerClient, true)
	if err != nil {
		return nil, err
	}
	session := s.snapshotSession(dockerClient, key)
	safetyLocation := systemBackupSnapshotLocation{name: "safety", destination: backuptypes.SystemBackupDestinationLocal, repository: localRepository, snapshotID: localSnapshotID}
	safetyLayout, err := session.readSnapshotLayout(ctx, safetyLocation)
	if err != nil {
		return nil, fmt.Errorf("open pre-restore system backup: %w", err)
	}
	repository := recovery.RestoreRepository{Environment: safetyLocation.repository.Environment, Mounts: safetyLocation.repository.Mounts}
	stages, err := restoreStages(mounts, dataDirectory, projectsDirectory, repository, localSnapshotID, safetyLayout)
	if err != nil {
		return nil, fmt.Errorf("plan system restore rollback: %w", err)
	}
	return stages, nil
}

// normalizeSystemBackupSelection collapses a plain select-all into the
// projects root, except when projects share the data root and a root restore
// would replace Arcane's own files.
func normalizeSystemBackupSelection(selection backuptypes.RestoreSelection, snapshot systemBackupSnapshot) ([]backuptypes.BackupFileEntry, error) {
	if selection.SelectAll && strings.TrimSpace(selection.Search) == "" && snapshot.layout.projectsPath != snapshot.layout.dataPath {
		return backupbrowser.NormalizeSelection(selection, []backuptypes.BackupFileEntry{{Path: "", Name: path.Base(snapshot.layout.projectsPath), IsDirectory: true}})
	}
	return backupbrowser.NormalizeSelection(selection, snapshot.entries)
}

// backupSourceLayout is what one system backup snapshots and where
// each source appears inside the snapshot.
type backupSourceLayout struct {
	dataDirectory     string
	projectsDirectory string
	databaseName      string
	projectsPath      string
	nestedDataPath    string // data directory inside /projects when the projects bind contains it on the host
	mounts            []mount.Mount
	sources           []string
	excludes          []string
}

// projectsSnapshotPath classifies the projects directory against the
// data directory and returns its snapshot path; external reports whether it
// needs its own snapshot source.
func projectsSnapshotPath(dataDirectory, projectsDirectory string) (projectsPath string, external bool, err error) {
	data := path.Clean(filepath.ToSlash(dataDirectory))
	projectsDir := path.Clean(filepath.ToSlash(projectsDirectory))
	switch {
	case projectsDir == data:
		return snapshotDataPath, false, nil
	case kit.FilePathMatches(projectsDir, data):
		return path.Join(snapshotDataPath, strings.TrimPrefix(projectsDir, data+"/")), false, nil
	case kit.FilePathMatches(data, projectsDir):
		return "", false, fmt.Errorf("the projects directory %s contains Arcane's data directory %s; move it inside or outside the data directory before creating system backups", projectsDir, data)
	default:
		return snapshotProjectsPath, true, nil
	}
}

// currentMounts returns the Arcane container's mounts, or inContainer
// false in host development where directories are bound directly.
func currentMounts(ctx context.Context, dockerClient *client.Client) (mounts []container.MountPoint, inContainer bool, err error) {
	_, containerErr := cgroup.CurrentContainerID()
	if inContainer = containerErr == nil; !inContainer {
		return nil, false, nil
	}
	inspect, err := libarcane.InspectCurrentArcaneContainer(ctx, dockerClient)
	if err != nil {
		return nil, true, fmt.Errorf("failed to inspect Arcane container mounts: %w", err)
	}
	return inspect.Mounts, true, nil
}

// sourceMounts exposes containerPath read-only at target: the
// container's own mount plus every mount nested beneath it, so the helper
// sees the same files Arcane does.
func sourceMounts(mounts []container.MountPoint, inContainer bool, containerPath, target string) ([]mount.Mount, error) {
	if !inContainer {
		return []mount.Mount{{Type: mount.TypeBind, Source: containerPath, Target: target, ReadOnly: true}}, nil
	}
	primary := docker.MountForSubpath(mounts, containerPath, target)
	if primary == nil {
		return nil, fmt.Errorf("%s must be mounted into the Arcane container from a bind or named volume for system backups", containerPath)
	}
	result := append([]mount.Mount{*primary}, docker.NestedMounts(mounts, containerPath, target)...)
	for index := range result {
		result[index].ReadOnly = true
	}
	return result, nil
}

// nestedDataPath returns the data directory relative to the projects
// directory when the projects mount contains it on the host: the same bind
// path prefix or the same named volume and subpath. Mounting one host
// directory twice is an error.
func nestedDataPath(mounts []container.MountPoint, dataDirectory, projectsDirectory string) (relative string, nested bool, err error) {
	// Projects kept at the data root share one mount by design; projectsSnapshotPath owns that layout.
	if path.Clean(filepath.ToSlash(dataDirectory)) == path.Clean(filepath.ToSlash(projectsDirectory)) {
		return "", false, nil
	}
	var sources []string
	for _, directory := range []string{projectsDirectory, dataDirectory} {
		resolved := docker.MountForSubpath(mounts, directory, "")
		if resolved == nil {
			return "", false, nil
		}
		source := path.Clean(filepath.ToSlash(resolved.Source))
		if resolved.Type == mount.TypeVolume {
			source = "volume:" + resolved.Source
			if resolved.VolumeOptions != nil && resolved.VolumeOptions.Subpath != "" {
				source = path.Join(source, resolved.VolumeOptions.Subpath)
			}
		}
		sources = append(sources, source)
	}
	projectsSource, dataSource := sources[0], sources[1]
	if !kit.FilePathMatches(dataSource, projectsSource) {
		return "", false, nil
	}
	if dataSource == projectsSource {
		return "", true, fmt.Errorf(
			"the projects directory %s and Arcane's data directory %s are the same host directory; move one before backing up or restoring",
			projectsDirectory, dataDirectory,
		)
	}
	return strings.TrimPrefix(dataSource, projectsSource+"/"), true, nil
}

// backupSourceLayout resolves the sources of one system backup once so
// the manifest and the snapshot describe the same tree.
func (s *Service) backupSourceLayout(ctx context.Context, dockerClient *client.Client) (backupSourceLayout, error) {
	databaseFile, err := s.databaseFile()
	if err != nil {
		return backupSourceLayout{}, err
	}
	layout := backupSourceLayout{
		dataDirectory:     filepath.Dir(databaseFile),
		projectsDirectory: s.projectsDirectory(ctx),
		databaseName:      filepath.Base(databaseFile),
		sources:           []string{snapshotDataPath},
	}
	projectsPath, external, err := projectsSnapshotPath(layout.dataDirectory, layout.projectsDirectory)
	if err != nil {
		return backupSourceLayout{}, err
	}
	layout.projectsPath = projectsPath
	mounts, inContainer, err := currentMounts(ctx, dockerClient)
	if err != nil {
		return backupSourceLayout{}, err
	}
	layout.mounts, err = sourceMounts(mounts, inContainer, layout.dataDirectory, snapshotDataPath)
	if err != nil {
		return backupSourceLayout{}, fmt.Errorf("resolve Arcane data source: %w", err)
	}
	if !external {
		return layout, nil
	}
	projectMounts, err := sourceMounts(mounts, inContainer, layout.projectsDirectory, snapshotProjectsPath)
	if err != nil {
		return backupSourceLayout{}, fmt.Errorf("resolve projects source: %w", err)
	}
	layout.mounts = append(layout.mounts, projectMounts...)
	layout.sources = append(layout.sources, snapshotProjectsPath)
	relative, nested, err := nestedDataPath(mounts, layout.dataDirectory, layout.projectsDirectory)
	if err != nil {
		return backupSourceLayout{}, err
	}
	if nested {
		layout.nestedDataPath = path.Join(snapshotProjectsPath, relative)
	}
	return layout, nil
}

var (
	errProjectsOutsideData = errors.New("the backup-time projects directory is outside Arcane's system backup data")
	errProjectsNotInBackup = errors.New(
		"this system backup does not include the projects directory because it was created before Arcane " +
			"backed up separately mounted projects; create a new system backup to restore project files",
	)
	manifestCandidateRoots = []string{snapshotDataPath, "/", "/app/data"}
	sqliteSidecars         = []string{"-wal", "-shm", "-journal"}
)

// snapshotLayout locates Arcane data and projects inside one snapshot.
type snapshotLayout struct {
	dataPath     string // "/" or "/app/data" for version 1, "/data" for version 2
	projectsPath string // absolute snapshot path; empty when the backup omitted projects
	databaseName string
}

func (layout snapshotLayout) projectsIncluded() bool {
	return layout.projectsPath != ""
}

// projectsDataRelative returns the projects root relative to the data
// root when projects live inside it.
func (layout snapshotLayout) projectsDataRelative() (string, bool) {
	if !layout.projectsIncluded() {
		return "", false
	}
	if layout.projectsPath == layout.dataPath {
		return "", true
	}
	return snapshotRelativePath(layout.projectsPath, layout.dataPath)
}

// protected reports whether a project-relative path is an Arcane data
// file that must never be listed or restored as a project file.
func (layout snapshotLayout) protected(projectRelative string) bool {
	relative, inside := layout.projectsDataRelative()
	if !inside {
		return false
	}
	candidate := path.Join(relative, projectRelative)
	if candidate == RecoveryManifestName || candidate == RecoveryRequestName {
		return true
	}
	return slices.ContainsFunc([]string{layout.databaseName, francis.StorePath(layout.databaseName)}, func(database string) bool {
		sidecar, found := strings.CutPrefix(candidate, database)
		return found && (sidecar == "" || slices.Contains(sqliteSidecars, sidecar))
	})
}

// projectsCoveredByData reports whether restoring the data root already
// puts projects where the current projects directory is.
func (layout snapshotLayout) projectsCoveredByData(dataDirectory, projectsDirectory string) bool {
	relative, inside := layout.projectsDataRelative()
	if !inside {
		return false
	}
	return path.Clean(filepath.ToSlash(projectsDirectory)) == path.Join(path.Clean(filepath.ToSlash(dataDirectory)), relative)
}

func (session systemBackupSnapshotSession) readSnapshotLayout(ctx context.Context, location systemBackupSnapshotLocation) (snapshotLayout, error) {
	var manifestData, manifestRoot string
	var readErr error
	for _, candidate := range manifestCandidateRoots {
		data, err := session.service.engine.ReadSnapshotTextFile(ctx, session.dockerClient, location.repository, session.recoveryKey, location.snapshotID, path.Join(candidate, RecoveryManifestName))
		if err != nil {
			readErr = errors.Join(readErr, err)
			continue
		}
		manifestData, manifestRoot = data, candidate
		break
	}
	if manifestRoot == "" {
		return snapshotLayout{}, fmt.Errorf("read %s system recovery manifest: %w", location.name, readErr)
	}
	var manifest recovery.Manifest
	if err := json.Unmarshal([]byte(manifestData), &manifest); err != nil {
		return snapshotLayout{}, fmt.Errorf("decode %s system recovery manifest: %w", location.name, err)
	}
	layout, err := snapshotLayoutFromManifest(manifest, manifestRoot)
	if err != nil {
		return snapshotLayout{}, fmt.Errorf("%s system recovery manifest: %w", location.name, err)
	}
	return layout, nil
}

func snapshotLayoutFromManifest(manifest recovery.Manifest, manifestRoot string) (snapshotLayout, error) {
	switch manifest.FormatVersion {
	case 1:
		// Version 1 derives the layout from the manifest environment; projects
		// outside the captured data are reported as omitted.
		databasePath, err := recoveryManifestDatabasePath(manifest)
		if err != nil {
			return snapshotLayout{}, err
		}
		layout := snapshotLayout{dataPath: manifestRoot, databaseName: path.Base(databasePath)}
		relative, err := projectsRelativePathFromManifest(manifest)
		if errors.Is(err, errProjectsOutsideData) {
			return layout, nil
		}
		if err != nil {
			return snapshotLayout{}, fmt.Errorf("resolve projects directory: %w", err)
		}
		layout.projectsPath = path.Join(manifestRoot, relative)
		return layout, nil
	case recovery.ManifestFormatVersion:
		dataPath, err := kit.NormalizeRelativePath(strings.TrimPrefix(manifest.DataPath, "/"))
		if err != nil {
			return snapshotLayout{}, fmt.Errorf("invalid data path: %w", err)
		}
		dataPath = "/" + dataPath
		if dataPath != manifestRoot {
			return snapshotLayout{}, fmt.Errorf("records data path %s but was found at %s", dataPath, manifestRoot)
		}
		projectsPath, err := kit.NormalizeRelativePath(strings.TrimPrefix(manifest.ProjectsPath, "/"))
		if err != nil {
			return snapshotLayout{}, fmt.Errorf("invalid projects path: %w", err)
		}
		projectsPath = "/" + projectsPath
		databaseName, err := kit.NormalizeRelativePath(manifest.DatabasePath)
		if err != nil {
			return snapshotLayout{}, fmt.Errorf("invalid database path: %w", err)
		}
		return snapshotLayout{dataPath: dataPath, projectsPath: projectsPath, databaseName: databaseName}, nil
	default:
		return snapshotLayout{}, fmt.Errorf("uses unsupported format %d", manifest.FormatVersion)
	}
}

func recoveryManifestDatabasePath(manifest recovery.Manifest) (string, error) {
	databaseURL := strings.TrimSpace(manifest.Environment["DATABASE_URL"])
	if databaseURL == "" {
		return "", errors.New("system recovery manifest does not record the database path")
	}
	databasePath, err := kit.SQLitePathFromDSN(databaseURL)
	if err != nil {
		return "", fmt.Errorf("parse database URL from system recovery manifest: %w", err)
	}
	databasePath = strings.ReplaceAll(strings.TrimSpace(databasePath), `\`, "/")
	if databasePath == "" {
		return "", errors.New("system recovery manifest contains an empty database path")
	}
	return path.Clean(databasePath), nil
}

// projectsRelativePathFromManifest relates a version-1 manifest's
// projects directory to its data directory.
func projectsRelativePathFromManifest(manifest recovery.Manifest) (string, error) {
	configured := strings.TrimSpace(manifest.Environment["PROJECTS_DIRECTORY"])
	if configured == "" {
		return "", errors.New("system recovery manifest does not record the projects directory")
	}
	if strings.HasPrefix(configured, "/") {
		if separator := strings.Index(configured, ":"); separator > 0 {
			configured = configured[:separator]
		}
	}
	projectsPath := path.Clean(strings.ReplaceAll(configured, `\`, "/"))
	databasePath, err := recoveryManifestDatabasePath(manifest)
	if err != nil {
		return "", err
	}
	dataPath := path.Dir(databasePath)
	// Windows drive paths like C:/data count as absolute alongside POSIX paths.
	dataAbsolute := path.IsAbs(dataPath) || (len(dataPath) >= 3 && dataPath[1] == ':' && dataPath[2] == '/')
	projectsAbsolute := path.IsAbs(projectsPath) || (len(projectsPath) >= 3 && projectsPath[1] == ':' && projectsPath[2] == '/')
	if !dataAbsolute && projectsAbsolute {
		relativeDataPath := strings.Trim(dataPath, "/")
		if relativeDataPath == "." {
			return "", errors.New("cannot relate the absolute projects directory to the relative database path in the system recovery manifest")
		}
		marker := "/" + relativeDataPath
		index := strings.LastIndex(projectsPath, marker)
		if index < 0 || (len(projectsPath) > index+len(marker) && projectsPath[index+len(marker)] != '/') {
			return "", errors.New("cannot relate the projects directory to the database path in the system recovery manifest")
		}
		dataPath = projectsPath[:index+len(marker)]
		dataAbsolute = true
	} else if dataAbsolute && !projectsAbsolute {
		projectsPath = path.Join(path.Dir(dataPath), projectsPath)
		projectsAbsolute = true
	}
	if dataAbsolute != projectsAbsolute {
		return "", errors.New("projects and database paths in the system recovery manifest use incompatible roots")
	}
	if projectsPath == dataPath {
		return "", nil
	}
	if !kit.FilePathMatches(projectsPath, dataPath) {
		return "", errProjectsOutsideData
	}
	return strings.TrimPrefix(projectsPath, dataPath+"/"), nil
}

// restoreTarget resolves the writable destination for containerPath
// from the Arcane container's mounts: its enclosing mount at target plus the
// mounts nested beneath it, minus any under exclude that a later stage writes
// through their own mount.
func restoreTarget(mounts []container.MountPoint, containerPath, target, exclude string) (recovery.RestoreTarget, error) {
	enclosing, relative := docker.MountForEnclosingPath(mounts, containerPath, target)
	if enclosing == nil {
		return recovery.RestoreTarget{}, fmt.Errorf("%s must be mounted into the Arcane container from a bind or named volume to restore into it", containerPath)
	}
	if enclosing.ReadOnly {
		return recovery.RestoreTarget{}, fmt.Errorf("%s is mounted read-only into the Arcane container", containerPath)
	}
	destination := path.Join(target, relative)
	candidates := slices.DeleteFunc(slices.Clone(mounts), func(m container.MountPoint) bool {
		return exclude != "" && kit.FilePathMatches(m.Destination, exclude)
	})
	result := recovery.RestoreTarget{Mounts: []mount.Mount{*enclosing}, Path: destination}
	result.Mounts = append(result.Mounts, docker.NestedMounts(candidates, containerPath, destination)...)
	return result, nil
}

// projectsRestoreDestination is the current projects directory as a
// helper-container target plus its container path for confined cleanup.
type projectsRestoreDestination struct {
	directory string
	dataPath  string // project-relative path of Arcane's data directory when the projects bind contains it on the host
	target    recovery.RestoreTarget
}

func (s *Service) projectsRestoreDestination(ctx context.Context, dockerClient *client.Client) (projectsRestoreDestination, error) {
	directory := s.projectsDirectory(ctx)
	mounts, inContainer, err := currentMounts(ctx, dockerClient)
	if err != nil {
		return projectsRestoreDestination{}, err
	}
	if !inContainer {
		hostTarget := recovery.RestoreTarget{
			Mounts: []mount.Mount{{Type: mount.TypeBind, Source: directory, Target: selectiveProjectsRestoreTarget}},
			Path:   selectiveProjectsRestoreTarget,
		}
		return projectsRestoreDestination{directory: directory, target: hostTarget}, nil
	}
	target, err := restoreTarget(mounts, directory, selectiveProjectsRestoreTarget, "")
	if err != nil {
		return projectsRestoreDestination{}, fmt.Errorf("resolve projects directory for restore: %w", err)
	}
	databaseFile, err := s.databaseFile()
	if err != nil {
		return projectsRestoreDestination{}, err
	}
	dataDirectory := filepath.Dir(databaseFile)
	dataPath, _, err := nestedDataPath(mounts, dataDirectory, directory)
	if err != nil {
		return projectsRestoreDestination{}, err
	}
	return projectsRestoreDestination{directory: directory, dataPath: dataPath, target: target}, nil
}

// restoreStages plans the Rustic restores that put a snapshot's data
// and projects into the current container layout. Projects get their own
// stage unless the snapshot already holds them at their current place under
// the data root.
func restoreStages(
	mounts []container.MountPoint,
	dataDirectory, projectsDirectory string,
	repository recovery.RestoreRepository,
	snapshotID string,
	layout snapshotLayout,
) (
	[]recovery.RestoreStage,
	error,
) {
	separate := layout.projectsIncluded() && !layout.projectsCoveredByData(dataDirectory, projectsDirectory)
	if separate && layout.projectsPath == layout.dataPath {
		return nil, fmt.Errorf("the backup keeps projects in Arcane's data directory; set the projects directory to %s before a full restore, or restore individual project files instead", dataDirectory)
	}
	exclude := kit.Ternary(separate, projectsDirectory, "")
	dataTarget, err := restoreTarget(mounts, dataDirectory, recoveryDataRestoreTarget, exclude)
	if err != nil {
		return nil, err
	}
	// Either stage restores with --delete, so a projects mount that contains
	// the data directory on the host would remove it mid-restore.
	_, nested, err := nestedDataPath(mounts, dataDirectory, projectsDirectory)
	if err != nil {
		return nil, err
	}
	if nested {
		return nil, fmt.Errorf(
			"the projects directory %s contains Arcane's data directory %s on the host; a full restore would delete it, so restore individual project files instead",
			projectsDirectory, dataDirectory,
		)
	}
	stages := make([]recovery.RestoreStage, 0, 2)
	stages = append(stages, recovery.RestoreStage{Repository: repository, SnapshotID: snapshotID, SourcePath: layout.dataPath, Target: dataTarget})
	if !separate {
		return stages, nil
	}
	projectsTarget, err := restoreTarget(mounts, projectsDirectory, recoveryProjectsRestoreTarget, "")
	if err != nil {
		return nil, err
	}
	return append(stages, recovery.RestoreStage{Repository: repository, SnapshotID: snapshotID, SourcePath: layout.projectsPath, Target: projectsTarget}), nil
}

// Create archives a consistent system snapshot without blocking actor leases during the upload.
func (s *Service) Create(
	ctx context.Context,
	dockerClient *client.Client,
	repository backup.Repository,
	recoveryKey, backupID string,
) (
	backup.Snapshot,
	error,
) {
	layout, err := s.backupSourceLayout(ctx, dockerClient)
	if err != nil {
		return backup.Snapshot{}, err
	}
	stage, err := os.MkdirTemp(layout.dataDirectory, ".arcane-snapshot-")
	if err != nil {
		return backup.Snapshot{}, fmt.Errorf("create system backup staging directory: %w", err)
	}
	defer func() { _ = os.RemoveAll(stage) }()
	sqlDB, err := s.sqlDB()
	if err != nil {
		return backup.Snapshot{}, err
	}
	actorStore, err := s.actorStore()
	if err != nil {
		return backup.Snapshot{}, err
	}
	actorSQLDB, err := actorStore.DB()
	if err != nil {
		return backup.Snapshot{}, err
	}
	databases := map[string]*sql.DB{layout.databaseName: sqlDB, francis.StorePath(layout.databaseName): actorSQLDB}
	manifest := recovery.Manifest{
		FormatVersion: recovery.ManifestFormatVersion, ArcaneVersion: config.Version, BackupID: backupID,
		ActivityID: activity.IDFromContext(ctx), CreatedAt: time.Now().UTC(),
		DataPath: snapshotDataPath, ProjectsPath: layout.projectsPath, DatabasePath: layout.databaseName,
		Environment: s.recoveryEnvironment(ctx),
	}
	manifestData, err := json.Marshal(manifest, jsontext.WithIndent("  "))
	if err != nil {
		return backup.Snapshot{}, fmt.Errorf("failed to encode recovery manifest: %w", err)
	}
	manifestPath := filepath.Join(layout.dataDirectory, RecoveryManifestName)
	if err = os.WriteFile(manifestPath, manifestData, 0o600); err != nil {
		return backup.Snapshot{}, fmt.Errorf("failed to write recovery manifest: %w", err)
	}
	defer func() { _ = os.Remove(manifestPath) }()
	layout.excludes = []string{".arcane-snapshot-*", RecoveryRequestName}
	for name := range databases {
		for _, sidecar := range sqliteSidecars {
			layout.excludes = append(layout.excludes, name+sidecar)
		}
	}
	files, err := snapshotSourceFiles(ctx, layout)
	if err != nil {
		return backup.Snapshot{}, err
	}
	mounts, inContainer, err := currentMounts(ctx, dockerClient)
	if err != nil {
		return backup.Snapshot{}, err
	}
	// Mount VACUUM INTO copies over the live databases so the snapshot is consistent.
	for name, db := range databases {
		stagedDatabase := filepath.Join(stage, name)
		if stageErr := stageDatabase(ctx, db, filepath.Join(layout.dataDirectory, name), stagedDatabase); stageErr != nil {
			return backup.Snapshot{}, stageErr
		}
		stagedMounts, mountErr := sourceMounts(mounts, inContainer, stagedDatabase, filepath.Join(snapshotDataPath, name))
		if mountErr != nil {
			return backup.Snapshot{}, mountErr
		}
		layout.mounts = append(layout.mounts, stagedMounts...)
	}
	input := backup.CreateSnapshotInput{Mounts: layout.mounts, Sources: layout.sources}
	for _, excluded := range layout.excludes {
		input.Globs = append(input.Globs, "!"+snapshotDataPath+"/"+excluded)
	}
	if layout.nestedDataPath != "" {
		input.Globs = append(input.Globs, "!"+layout.nestedDataPath)
	}
	snapshot, err := s.engine.CreateSnapshot(ctx, dockerClient, repository, recoveryKey, "arcane-system-recovery", input)
	if err != nil {
		return backup.Snapshot{}, err
	}
	after, err := snapshotSourceFiles(ctx, layout)
	for filePath, original := range files {
		if err != nil {
			break
		}
		current, exists := after[filePath]
		if !exists || !os.SameFile(original, current) || original.Size() != current.Size() || original.Mode() != current.Mode() || !original.ModTime().Equal(current.ModTime()) {
			err = fmt.Errorf("system backup source %q changed during capture; retry when file updates finish", filePath)
		}
	}
	for filePath := range after {
		if err != nil {
			break
		}
		if _, exists := files[filePath]; !exists {
			err = fmt.Errorf("system backup source %q appeared during capture; retry when file updates finish", filePath)
		}
	}
	if err == nil {
		var confirmed backup.Snapshot
		confirmed, err = s.engine.ConfirmRunSnapshot(ctx, dockerClient, repository, recoveryKey, backupID, snapshot.ID)
		if err == nil {
			return confirmed, nil
		}
	}
	// Tagging changes the ID; only remove the original, unconfirmed snapshot.
	cleanupCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), time.Minute)
	defer cancel()
	cleanupErr := s.engine.ForgetSnapshots(cleanupCtx, dockerClient, repository, recoveryKey, []string{snapshot.ID})
	if cleanupErr != nil {
		cleanupErr = fmt.Errorf("remove unconfirmed system snapshot %s: %w", snapshot.ID, cleanupErr)
	}
	return backup.Snapshot{}, errors.Join(err, cleanupErr)
}

// snapshotSourceFiles records Arcane's data directory before taking the
// database snapshot, skipping the projects directory wherever it appears since
// project folders hold live container data.
func snapshotSourceFiles(ctx context.Context, layout backupSourceLayout) (map[string]os.FileInfo, error) {
	projectsInfo, statErr := os.Stat(layout.projectsDirectory)
	if statErr != nil {
		projectsInfo = nil
	}
	actorDatabase := francis.StorePath(layout.databaseName)
	files := make(map[string]os.FileInfo)
	visit := func(filePath string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		relative, err := filepath.Rel(layout.dataDirectory, filePath)
		if err != nil {
			return err
		}
		if relative == layout.databaseName || relative == actorDatabase {
			return nil
		}
		for _, excluded := range layout.excludes {
			matched, matchErr := filepath.Match(excluded, relative)
			if matchErr != nil {
				return matchErr
			}
			if !matched {
				continue
			}
			if entry.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if entry.IsDir() && relative != "." && projectsInfo != nil && os.SameFile(info, projectsInfo) {
			return filepath.SkipDir
		}
		files[filePath] = info
		return nil
	}
	if err := filepath.WalkDir(layout.dataDirectory, visit); err != nil {
		return nil, fmt.Errorf("inspect system backup sources: %w", err)
	}
	return files, nil
}

// stageDatabase writes a consistent copy of the live database with VACUUM INTO,
// keeping the original ownership and mode so restores preserve them.
func stageDatabase(ctx context.Context, db *sql.DB, databasePath, stagedDatabase string) error {
	info, err := os.Stat(databasePath)
	if err != nil {
		return err
	}
	if _, err = db.ExecContext(ctx, "VACUUM INTO ?", stagedDatabase); err != nil {
		return fmt.Errorf("stage Arcane database: %w", err)
	}
	if stat, ok := info.Sys().(*syscall.Stat_t); ok {
		if err = os.Chown(stagedDatabase, int(stat.Uid), int(stat.Gid)); err != nil {
			return fmt.Errorf("preserve staged database ownership: %w", err)
		}
	}
	if err = os.Chmod(stagedDatabase, info.Mode()); err != nil {
		return fmt.Errorf("preserve staged database permissions: %w", err)
	}
	return nil
}
