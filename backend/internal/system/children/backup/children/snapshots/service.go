package snapshots

import (
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

	"github.com/getarcaneapp/arcane/backend/v2/internal/backup"
	"github.com/getarcaneapp/arcane/backend/v2/internal/common"
	"github.com/getarcaneapp/arcane/backend/v2/internal/config"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/libarcane"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/libarcane/activity"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/pagination"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/utils/backupbrowser"
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
	localRepository     func(ctx context.Context, dockerClient *client.Client, readOnly bool) (backup.Repository, error)
	remoteRepository    func(ctx context.Context, destinationID string) (backup.Repository, error)
	databaseFile        func() (string, error)
	projectsDirectory   func(ctx context.Context) string
	recoveryEnvironment func(ctx context.Context) map[string]string
}

func NewService(
	engine *backup.Engine,
	sqlDB func() (*sql.DB, error),
	localRepository func(ctx context.Context, dockerClient *client.Client, readOnly bool) (backup.Repository, error),
	remoteRepository func(ctx context.Context, destinationID string) (backup.Repository, error),
	databaseFile func() (string, error),
	projectsDirectory func(ctx context.Context) string,
	recoveryEnvironment func(ctx context.Context) map[string]string,
) *Service {
	return &Service{
		engine:              engine,
		sqlDB:               sqlDB,
		localRepository:     localRepository,
		remoteRepository:    remoteRepository,
		databaseFile:        databaseFile,
		projectsDirectory:   projectsDirectory,
		recoveryEnvironment: recoveryEnvironment,
	}
}

func (s *Service) writeManifestInternal(ctx context.Context, backupID string, layout backupSourceLayoutInternal) error {
	manifest := recovery.Manifest{
		FormatVersion: recovery.ManifestFormatVersion, ArcaneVersion: config.Version, BackupID: backupID,
		ActivityID: activity.IDFromContext(ctx), CreatedAt: time.Now().UTC(),
		DataPath: snapshotDataPath, ProjectsPath: layout.projectsPath, DatabasePath: layout.databaseName,
		Environment: s.recoveryEnvironment(ctx),
	}
	data, err := json.Marshal(manifest, jsontext.WithIndent("  "))
	if err != nil {
		return fmt.Errorf("failed to encode recovery manifest: %w", err)
	}
	if writeFileErr := os.WriteFile(layout.manifestPathInternal(), data, 0o600); writeFileErr != nil {
		return fmt.Errorf("failed to write recovery manifest: %w", writeFileErr)
	}
	return nil
}

type systemBackupSnapshotLocationInternal struct {
	name            string
	destination     backuptypes.SystemBackupDestination
	s3DestinationID string
	repository      backup.Repository
	snapshotID      string
}

func (location systemBackupSnapshotLocationInternal) restoreRepositoryInternal() recovery.RestoreRepository {
	return recovery.RestoreRepository{Environment: location.repository.Environment, Mounts: location.repository.Mounts}
}

type systemBackupSnapshotInternal struct {
	systemBackupSnapshotLocationInternal

	layout  snapshotLayoutInternal
	entries []backuptypes.BackupFileEntry
}

type systemBackupSafetySnapshotInternal struct {
	systemBackupSnapshotLocationInternal

	layout snapshotLayoutInternal
	paths  map[string]struct{}
}

// systemBackupSnapshotSessionInternal carries the Docker client and resolved
// recovery key through one browse or restore operation, so the snapshot steps
// below don't each thread them through their signatures.
type systemBackupSnapshotSessionInternal struct {
	service      *Service
	dockerClient *client.Client
	recoveryKey  string
}

func (s *Service) snapshotSessionInternal(dockerClient *client.Client, recoveryKey string) systemBackupSnapshotSessionInternal {
	return systemBackupSnapshotSessionInternal{service: s, dockerClient: dockerClient, recoveryKey: recoveryKey}
}

func (s *Service) backupSnapshotLocationsInternal(ctx context.Context, dockerClient *client.Client, run backuptypes.SystemBackupRun) ([]systemBackupSnapshotLocationInternal, error) {
	locations := make([]systemBackupSnapshotLocationInternal, 0, 2)
	var setupErr error
	if run.LocalSnapshotID != "" {
		repository, err := s.localRepository(ctx, dockerClient, true)
		if err != nil {
			setupErr = errors.Join(setupErr, fmt.Errorf("open local system backup repository: %w", err))
		} else {
			locations = append(locations, systemBackupSnapshotLocationInternal{
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
			locations = append(locations, systemBackupSnapshotLocationInternal{
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

func (session systemBackupSnapshotSessionInternal) inspectReadableSnapshotInternal(ctx context.Context, location systemBackupSnapshotLocationInternal) (systemBackupSnapshotInternal, error) {
	layout, err := session.readSnapshotLayoutInternal(ctx, location)
	if err != nil {
		return systemBackupSnapshotInternal{}, fmt.Errorf("inspect %s system recovery snapshot: %w", location.name, err)
	}
	return systemBackupSnapshotInternal{systemBackupSnapshotLocationInternal: location, layout: layout}, nil
}

func (
	session systemBackupSnapshotSessionInternal,
) firstReadableSnapshotInternal(
	ctx context.Context,
	locations []systemBackupSnapshotLocationInternal,
	setupErr error,
) (
	systemBackupSnapshotInternal,
	error,
) {
	inspectErr := setupErr
	for _, location := range locations {
		snapshot, err := session.inspectReadableSnapshotInternal(ctx, location)
		if err == nil {
			return snapshot, nil
		}
		inspectErr = errors.Join(inspectErr, err)
	}
	if inspectErr == nil {
		inspectErr = errors.New("system backup has no Rustic snapshot")
	}
	return systemBackupSnapshotInternal{}, fmt.Errorf("failed to open system recovery snapshot: %w", inspectErr)
}

func (session systemBackupSnapshotSessionInternal) inspectProjectSnapshotInternal(ctx context.Context, location systemBackupSnapshotLocationInternal) (systemBackupSnapshotInternal, error) {
	snapshot, err := session.inspectProjectManifestInternal(ctx, location)
	if err != nil {
		return systemBackupSnapshotInternal{}, err
	}
	listed, err := session.service.engine.ListSnapshotFiles(ctx, session.dockerClient, location.repository, session.recoveryKey, location.snapshotID, snapshot.layout.projectsPath+"/", true)
	if err != nil {
		return systemBackupSnapshotInternal{}, fmt.Errorf("inspect %s project files: %w", location.name, err)
	}
	snapshot.entries = projectEntriesFromSnapshotInternal(listed, snapshot.layout, "", true)
	return snapshot, nil
}

// inspectProjectManifestInternal opens a snapshot for project browsing;
// backups that omitted projects are rejected with an actionable error.
func (session systemBackupSnapshotSessionInternal) inspectProjectManifestInternal(ctx context.Context, location systemBackupSnapshotLocationInternal) (systemBackupSnapshotInternal, error) {
	snapshot, err := session.inspectReadableSnapshotInternal(ctx, location)
	if err != nil {
		return systemBackupSnapshotInternal{}, err
	}
	if !snapshot.layout.projectsIncludedInternal() {
		return systemBackupSnapshotInternal{}, errProjectsNotInBackupInternal
	}
	return snapshot, nil
}

func (
	session systemBackupSnapshotSessionInternal,
) availableProjectSnapshotsInternal(
	ctx context.Context,
	locations []systemBackupSnapshotLocationInternal,
	setupErr error,
	firstOnly bool,
) (
	[]systemBackupSnapshotInternal,
	error,
) {
	snapshots := make([]systemBackupSnapshotInternal, 0, len(locations))
	inspectErr := setupErr
	for _, location := range locations {
		snapshot, err := session.inspectProjectSnapshotInternal(ctx, location)
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

func snapshotRelativePathInternal(filePath, snapshotPath string) (string, bool) {
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

// projectEntriesFromSnapshotInternal maps a snapshot listing to entries
// relative to the projects root, dropping protected Arcane data files.
func projectEntriesFromSnapshotInternal(files []string, layout snapshotLayoutInternal, browsePath string, recursive bool) []backuptypes.BackupFileEntry {
	eligible := make([]string, 0, len(files))
	for _, file := range files {
		projectRelative, ok := snapshotRelativePathInternal(file, layout.projectsPath)
		if !ok || layout.protectedInternal(projectRelative) {
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
	session := s.snapshotSessionInternal(dockerClient, key)
	locations, setupErr := s.backupSnapshotLocationsInternal(ctx, dockerClient, run)
	browseErr := setupErr
	for _, location := range locations {
		snapshot, inspectErr := session.inspectProjectManifestInternal(ctx, location)
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
		entries := projectEntriesFromSnapshotInternal(listed, snapshot.layout, listPath, recursive)
		items, page := backupbrowser.Browse(entries, params)
		return items, page, nil
	}
	return nil, pagination.Response{}, fmt.Errorf("failed to browse project files in system recovery snapshot: %w", browseErr)
}

// openSafetySnapshotInternal resolves the safety run's snapshot in the same
// repository the restore reads from and indexes the project paths it holds.
func (
	session systemBackupSnapshotSessionInternal,
) openSafetySnapshotInternal(
	ctx context.Context,
	source systemBackupSnapshotInternal,
	safetyRun *backuptypes.SystemBackupRun,
) (
	systemBackupSafetySnapshotInternal,
	error,
) {
	location := source.systemBackupSnapshotLocationInternal
	location.name = "safety"
	if source.destination == backuptypes.SystemBackupDestinationS3 {
		location.snapshotID = safetyRun.RemoteSnapshotID
	} else {
		location.snapshotID = safetyRun.LocalSnapshotID
	}
	if location.snapshotID == "" {
		return systemBackupSafetySnapshotInternal{}, errors.New("pre-restore system backup has no snapshot in the selected repository")
	}
	snapshot, err := session.inspectProjectSnapshotInternal(ctx, location)
	if err != nil {
		return systemBackupSafetySnapshotInternal{}, fmt.Errorf("open pre-restore system backup: %w", err)
	}
	paths := make(map[string]struct{}, len(snapshot.entries))
	for _, entry := range snapshot.entries {
		paths[entry.Path] = struct{}{}
	}
	return systemBackupSafetySnapshotInternal{systemBackupSnapshotLocationInternal: location, layout: snapshot.layout, paths: paths}, nil
}

func removeProjectFileInternal(ctx context.Context, projectsDirectory, selectedPath string) error {
	if selectedPath == "" {
		return errors.New("refusing to remove the projects directory itself")
	}
	return acfs.RemoveAll(ctx, projectsDirectory, selectedPath)
}

func (
	session systemBackupSnapshotSessionInternal,
) restoreEntryInternal(
	ctx context.Context,
	snapshots []systemBackupSnapshotInternal,
	selected backuptypes.BackupFileEntry,
	destination projectsRestoreDestinationInternal,
) error {
	var restoreErr error
	for _, snapshot := range snapshots {
		if !snapshotContainsProjectEntryInternal(snapshot, selected) {
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

func snapshotContainsProjectEntryInternal(snapshot systemBackupSnapshotInternal, selected backuptypes.BackupFileEntry) bool {
	if selected.Path == "" && selected.IsDirectory {
		return true
	}
	for _, entry := range snapshot.entries {
		if entry.Path == selected.Path && entry.IsDirectory == selected.IsDirectory {
			return true
		}
	}
	return false
}

// safetySnapshotContainsPathInternal reports whether the safety snapshot holds
// a project-relative path; the projects root itself always exists.
func safetySnapshotContainsPathInternal(safety systemBackupSafetySnapshotInternal, projectRelative string) bool {
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

func (
	session systemBackupSnapshotSessionInternal,
) rollbackInternal(
	ctx context.Context,
	safety systemBackupSafetySnapshotInternal,
	selected []backuptypes.BackupFileEntry,
	destination projectsRestoreDestinationInternal,
) error {
	var rollbackErr error
	for _, selectedEntry := range slices.Backward(selected) {
		if !safetySnapshotContainsPathInternal(safety, selectedEntry.Path) {
			if err := removeProjectFileInternal(ctx, destination.directory, selectedEntry.Path); err != nil {
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

func (
	session systemBackupSnapshotSessionInternal,
) restoreSelectedInternal(
	ctx context.Context,
	snapshots []systemBackupSnapshotInternal,
	safety systemBackupSafetySnapshotInternal,
	selected []backuptypes.BackupFileEntry,
	destination projectsRestoreDestinationInternal,
) error {
	restored := make([]backuptypes.BackupFileEntry, 0, len(selected))
	for _, selectedEntry := range selected {
		if err := session.restoreEntryInternal(ctx, snapshots, selectedEntry, destination); err != nil {
			affected := slices.Clone(restored)
			affected = append(affected, selectedEntry)
			rollbackErr := session.rollbackInternal(context.WithoutCancel(ctx), safety, affected, destination)
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
func (
	s *Service,
) RestoreFiles(
	ctx context.Context,
	dockerClient *client.Client,
	key string,
	run backuptypes.SystemBackupRun,
	selection backuptypes.RestoreSelection,
	createSafety func(context.Context, backuptypes.CreateSystemBackupRequest) (*backuptypes.SystemBackupRun, error),
) error {
	session := s.snapshotSessionInternal(dockerClient, key)
	locations, setupErr := s.backupSnapshotLocationsInternal(ctx, dockerClient, run)
	snapshots, err := session.availableProjectSnapshotsInternal(ctx, locations, setupErr, false)
	if err != nil {
		return err
	}
	selected, err := normalizeSystemBackupSelectionInternal(selection, snapshots[0])
	if err != nil {
		return fmt.Errorf("%w: %w", common.ErrInvalidBackupSelection, err)
	}
	destination, err := s.projectsRestoreDestinationInternal(ctx, dockerClient)
	if err != nil {
		return err
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
	safety, err := session.openSafetySnapshotInternal(ctx, source, safetyRun)
	if err != nil {
		return err
	}
	return session.restoreSelectedInternal(ctx, snapshots, safety, selected, destination)
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
	session := s.snapshotSessionInternal(dockerClient, key)
	locations, setupErr := s.backupSnapshotLocationsInternal(ctx, dockerClient, run)
	snapshot, err := session.firstReadableSnapshotInternal(ctx, locations, setupErr)
	if err != nil {
		return RestorePlan{}, err
	}
	stages, err := restoreStagesInternal(mounts, dataDirectory, projectsDirectory, snapshot.restoreRepositoryInternal(), snapshot.snapshotID, snapshot.layout)
	if err != nil {
		return RestorePlan{}, fmt.Errorf("plan system restore: %w", err)
	}
	return RestorePlan{SnapshotID: snapshot.snapshotID, ProjectsIncluded: snapshot.layout.projectsIncludedInternal(), Stages: stages}, nil
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
	session := s.snapshotSessionInternal(dockerClient, key)
	safetyLocation := systemBackupSnapshotLocationInternal{name: "safety", destination: backuptypes.SystemBackupDestinationLocal, repository: localRepository, snapshotID: localSnapshotID}
	safetyLayout, err := session.readSnapshotLayoutInternal(ctx, safetyLocation)
	if err != nil {
		return nil, fmt.Errorf("open pre-restore system backup: %w", err)
	}
	stages, err := restoreStagesInternal(mounts, dataDirectory, projectsDirectory, safetyLocation.restoreRepositoryInternal(), localSnapshotID, safetyLayout)
	if err != nil {
		return nil, fmt.Errorf("plan system restore rollback: %w", err)
	}
	return stages, nil
}

// normalizeSystemBackupSelectionInternal collapses a plain select-all into the
// projects root, except when projects share the data root and a root restore
// would replace Arcane's own files.
func normalizeSystemBackupSelectionInternal(selection backuptypes.RestoreSelection, snapshot systemBackupSnapshotInternal) ([]backuptypes.BackupFileEntry, error) {
	if selection.SelectAll && strings.TrimSpace(selection.Search) == "" && snapshot.layout.projectsPath != snapshot.layout.dataPath {
		return backupbrowser.NormalizeSelection(selection, []backuptypes.BackupFileEntry{{Path: "", Name: path.Base(snapshot.layout.projectsPath), IsDirectory: true}})
	}
	return backupbrowser.NormalizeSelection(selection, snapshot.entries)
}

// backupSourceLayoutInternal is what one system backup snapshots and where
// each source appears inside the snapshot.
type backupSourceLayoutInternal struct {
	dataDirectory     string
	projectsDirectory string
	databaseName      string
	projectsPath      string
	mounts            []mount.Mount
	sources           []string
	excludes          []string
}

func (layout backupSourceLayoutInternal) manifestPathInternal() string {
	return filepath.Join(layout.dataDirectory, RecoveryManifestName)
}

// projectsSnapshotPathInternal classifies the projects directory against the
// data directory and returns its snapshot path; external reports whether it
// needs its own snapshot source.
func projectsSnapshotPathInternal(dataDirectory, projectsDirectory string) (projectsPath string, external bool, err error) {
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

// currentMountsInternal returns the Arcane container's mounts, or inContainer
// false in host development where directories are bound directly.
func currentMountsInternal(ctx context.Context, dockerClient *client.Client) (mounts []container.MountPoint, inContainer bool, err error) {
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

// sourceMountsInternal exposes containerPath read-only at target: the
// container's own mount plus every mount nested beneath it, so the helper
// sees the same files Arcane does.
func sourceMountsInternal(mounts []container.MountPoint, inContainer bool, containerPath, target string) ([]mount.Mount, error) {
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

// backupSourceLayoutInternal resolves the sources of one system backup once so
// the manifest and the snapshot describe the same tree.
func (s *Service) backupSourceLayoutInternal(ctx context.Context, dockerClient *client.Client) (backupSourceLayoutInternal, error) {
	databaseFile, err := s.databaseFile()
	if err != nil {
		return backupSourceLayoutInternal{}, err
	}
	layout := backupSourceLayoutInternal{
		dataDirectory:     filepath.Dir(databaseFile),
		projectsDirectory: s.projectsDirectory(ctx),
		databaseName:      filepath.Base(databaseFile),
		sources:           []string{snapshotDataPath},
	}
	projectsPath, external, err := projectsSnapshotPathInternal(layout.dataDirectory, layout.projectsDirectory)
	if err != nil {
		return backupSourceLayoutInternal{}, err
	}
	layout.projectsPath = projectsPath
	mounts, inContainer, err := currentMountsInternal(ctx, dockerClient)
	if err != nil {
		return backupSourceLayoutInternal{}, err
	}
	layout.mounts, err = sourceMountsInternal(mounts, inContainer, layout.dataDirectory, snapshotDataPath)
	if err != nil {
		return backupSourceLayoutInternal{}, fmt.Errorf("resolve Arcane data source: %w", err)
	}
	if !external {
		return layout, nil
	}
	projectMounts, err := sourceMountsInternal(mounts, inContainer, layout.projectsDirectory, snapshotProjectsPath)
	if err != nil {
		return backupSourceLayoutInternal{}, fmt.Errorf("resolve projects source: %w", err)
	}
	layout.mounts = append(layout.mounts, projectMounts...)
	layout.sources = append(layout.sources, snapshotProjectsPath)
	return layout, nil
}

var (
	errProjectsOutsideDataInternal = errors.New("the backup-time projects directory is outside Arcane's system backup data")
	errProjectsNotInBackupInternal = errors.New(
		"this system backup does not include the projects directory because it was created before Arcane " +
			"backed up separately mounted projects; create a new system backup to restore project files",
	)
	manifestCandidateRootsInternal = []string{snapshotDataPath, "/", "/app/data"}
)

// snapshotLayoutInternal locates Arcane data and projects inside one snapshot.
type snapshotLayoutInternal struct {
	dataPath     string // "/" or "/app/data" for version 1, "/data" for version 2
	projectsPath string // absolute snapshot path; empty when the backup omitted projects
	databaseName string
}

func (layout snapshotLayoutInternal) projectsIncludedInternal() bool {
	return layout.projectsPath != ""
}

// projectsDataRelativeInternal returns the projects root relative to the data
// root when projects live inside it.
func (layout snapshotLayoutInternal) projectsDataRelativeInternal() (string, bool) {
	if !layout.projectsIncludedInternal() {
		return "", false
	}
	if layout.projectsPath == layout.dataPath {
		return "", true
	}
	return snapshotRelativePathInternal(layout.projectsPath, layout.dataPath)
}

// protectedInternal reports whether a project-relative path is an Arcane data
// file that must never be listed or restored as a project file.
func (layout snapshotLayoutInternal) protectedInternal(projectRelative string) bool {
	relative, inside := layout.projectsDataRelativeInternal()
	if !inside {
		return false
	}
	candidate := path.Join(relative, projectRelative)
	switch candidate {
	case RecoveryManifestName, RecoveryRequestName, layout.databaseName, layout.databaseName + "-wal", layout.databaseName + "-shm", layout.databaseName + "-journal":
		return true
	default:
		return false
	}
}

// projectsCoveredByDataInternal reports whether restoring the data root already
// puts projects where the current projects directory is.
func (layout snapshotLayoutInternal) projectsCoveredByDataInternal(dataDirectory, projectsDirectory string) bool {
	relative, inside := layout.projectsDataRelativeInternal()
	if !inside {
		return false
	}
	return path.Clean(filepath.ToSlash(projectsDirectory)) == path.Join(path.Clean(filepath.ToSlash(dataDirectory)), relative)
}

func (session systemBackupSnapshotSessionInternal) readSnapshotLayoutInternal(ctx context.Context, location systemBackupSnapshotLocationInternal) (snapshotLayoutInternal, error) {
	var manifestData, manifestRoot string
	var readErr error
	for _, candidate := range manifestCandidateRootsInternal {
		data, err := session.service.engine.ReadSnapshotTextFile(ctx, session.dockerClient, location.repository, session.recoveryKey, location.snapshotID, path.Join(candidate, RecoveryManifestName))
		if err != nil {
			readErr = errors.Join(readErr, err)
			continue
		}
		manifestData, manifestRoot = data, candidate
		break
	}
	if manifestRoot == "" {
		return snapshotLayoutInternal{}, fmt.Errorf("read %s system recovery manifest: %w", location.name, readErr)
	}
	var manifest recovery.Manifest
	if err := json.Unmarshal([]byte(manifestData), &manifest); err != nil {
		return snapshotLayoutInternal{}, fmt.Errorf("decode %s system recovery manifest: %w", location.name, err)
	}
	layout, err := snapshotLayoutFromManifestInternal(manifest, manifestRoot)
	if err != nil {
		return snapshotLayoutInternal{}, fmt.Errorf("%s system recovery manifest: %w", location.name, err)
	}
	return layout, nil
}

func snapshotLayoutFromManifestInternal(manifest recovery.Manifest, manifestRoot string) (snapshotLayoutInternal, error) {
	switch manifest.FormatVersion {
	case 1:
		return legacySnapshotLayoutInternal(manifest, manifestRoot)
	case recovery.ManifestFormatVersion:
		dataPath, err := confinedSnapshotPathInternal(manifest.DataPath)
		if err != nil {
			return snapshotLayoutInternal{}, fmt.Errorf("invalid data path: %w", err)
		}
		if dataPath != manifestRoot {
			return snapshotLayoutInternal{}, fmt.Errorf("records data path %s but was found at %s", dataPath, manifestRoot)
		}
		projectsPath, err := confinedSnapshotPathInternal(manifest.ProjectsPath)
		if err != nil {
			return snapshotLayoutInternal{}, fmt.Errorf("invalid projects path: %w", err)
		}
		databaseName, err := kit.NormalizeRelativePath(manifest.DatabasePath)
		if err != nil {
			return snapshotLayoutInternal{}, fmt.Errorf("invalid database path: %w", err)
		}
		return snapshotLayoutInternal{dataPath: dataPath, projectsPath: projectsPath, databaseName: databaseName}, nil
	default:
		return snapshotLayoutInternal{}, fmt.Errorf("uses unsupported format %d", manifest.FormatVersion)
	}
}

// confinedSnapshotPathInternal validates a recorded snapshot path and returns
// it in absolute form.
func confinedSnapshotPathInternal(value string) (string, error) {
	relative, err := kit.NormalizeRelativePath(strings.TrimPrefix(strings.TrimSpace(value), "/"))
	if err != nil {
		return "", err
	}
	return "/" + relative, nil
}

// legacySnapshotLayoutInternal derives a version-1 layout from the manifest
// environment. Projects outside the captured data are reported as omitted.
func legacySnapshotLayoutInternal(manifest recovery.Manifest, manifestRoot string) (snapshotLayoutInternal, error) {
	databasePath, err := recoveryManifestDatabasePathInternal(manifest)
	if err != nil {
		return snapshotLayoutInternal{}, err
	}
	layout := snapshotLayoutInternal{dataPath: manifestRoot, databaseName: path.Base(databasePath)}
	relative, err := projectsRelativePathFromManifestInternal(manifest)
	if errors.Is(err, errProjectsOutsideDataInternal) {
		return layout, nil
	}
	if err != nil {
		return snapshotLayoutInternal{}, fmt.Errorf("resolve projects directory: %w", err)
	}
	layout.projectsPath = path.Join(manifestRoot, relative)
	return layout, nil
}

func recoveryManifestDatabasePathInternal(manifest recovery.Manifest) (string, error) {
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

func portablePathIsAbsInternal(filePath string) bool {
	return path.IsAbs(filePath) || (len(filePath) >= 3 && filePath[1] == ':' && filePath[2] == '/')
}

// projectsRelativePathFromManifestInternal relates a version-1 manifest's
// projects directory to its data directory.
func projectsRelativePathFromManifestInternal(manifest recovery.Manifest) (string, error) {
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
	databasePath, err := recoveryManifestDatabasePathInternal(manifest)
	if err != nil {
		return "", err
	}
	dataPath := path.Dir(databasePath)
	dataAbsolute := portablePathIsAbsInternal(dataPath)
	projectsAbsolute := portablePathIsAbsInternal(projectsPath)
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
		return "", errProjectsOutsideDataInternal
	}
	return strings.TrimPrefix(projectsPath, dataPath+"/"), nil
}

// restoreTargetInternal resolves the writable destination for containerPath
// from the Arcane container's mounts: its enclosing mount at target plus the
// mounts nested beneath it, minus any under exclude that a later stage writes
// through their own mount.
func restoreTargetInternal(mounts []container.MountPoint, containerPath, target, exclude string) (recovery.RestoreTarget, error) {
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

func hostRestoreTargetInternal(directory, target string) recovery.RestoreTarget {
	return recovery.RestoreTarget{Mounts: []mount.Mount{{Type: mount.TypeBind, Source: directory, Target: target}}, Path: target}
}

// projectsRestoreDestinationInternal is the current projects directory as a
// helper-container target plus its container path for confined cleanup.
type projectsRestoreDestinationInternal struct {
	directory string
	target    recovery.RestoreTarget
}

func (s *Service) projectsRestoreDestinationInternal(ctx context.Context, dockerClient *client.Client) (projectsRestoreDestinationInternal, error) {
	directory := s.projectsDirectory(ctx)
	mounts, inContainer, err := currentMountsInternal(ctx, dockerClient)
	if err != nil {
		return projectsRestoreDestinationInternal{}, err
	}
	if !inContainer {
		return projectsRestoreDestinationInternal{directory: directory, target: hostRestoreTargetInternal(directory, selectiveProjectsRestoreTarget)}, nil
	}
	target, err := restoreTargetInternal(mounts, directory, selectiveProjectsRestoreTarget, "")
	if err != nil {
		return projectsRestoreDestinationInternal{}, fmt.Errorf("resolve projects directory for restore: %w", err)
	}
	return projectsRestoreDestinationInternal{directory: directory, target: target}, nil
}

// restoreStagesInternal plans the Rustic restores that put a snapshot's data
// and projects into the current container layout. Projects get their own
// stage unless the snapshot already holds them at their current place under
// the data root.
func restoreStagesInternal(
	mounts []container.MountPoint,
	dataDirectory, projectsDirectory string,
	repository recovery.RestoreRepository,
	snapshotID string,
	layout snapshotLayoutInternal,
) (
	[]recovery.RestoreStage,
	error,
) {
	separate := layout.projectsIncludedInternal() && !layout.projectsCoveredByDataInternal(dataDirectory, projectsDirectory)
	if separate && layout.projectsPath == layout.dataPath {
		return nil, fmt.Errorf("the backup keeps projects in Arcane's data directory; set the projects directory to %s before a full restore, or restore individual project files instead", dataDirectory)
	}
	exclude := kit.Ternary(separate, projectsDirectory, "")
	dataTarget, err := restoreTargetInternal(mounts, dataDirectory, recoveryDataRestoreTarget, exclude)
	if err != nil {
		return nil, err
	}
	stages := make([]recovery.RestoreStage, 0, 2)
	stages = append(stages, recovery.RestoreStage{Repository: repository, SnapshotID: snapshotID, SourcePath: layout.dataPath, Target: dataTarget})
	if !separate {
		return stages, nil
	}
	projectsTarget, err := restoreTargetInternal(mounts, projectsDirectory, recoveryProjectsRestoreTarget, "")
	if err != nil {
		return nil, err
	}
	return append(stages, recovery.RestoreStage{Repository: repository, SnapshotID: snapshotID, SourcePath: layout.projectsPath, Target: projectsTarget}), nil
}

// Create archives a consistent system snapshot without blocking actor leases during the upload.
func (
	s *Service,
) Create(
	ctx context.Context,
	dockerClient *client.Client,
	repository backup.Repository,
	recoveryKey, backupID string,
) (
	backup.Snapshot,
	error,
) {
	layout, err := s.backupSourceLayoutInternal(ctx, dockerClient)
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
	databasePath := filepath.Join(layout.dataDirectory, layout.databaseName)
	stagedDatabase := filepath.Join(stage, layout.databaseName)
	snapshotDatabase := filepath.Join(snapshotDataPath, layout.databaseName)
	if writeManifestErr := s.writeManifestInternal(ctx, backupID, layout); writeManifestErr != nil {
		return backup.Snapshot{}, writeManifestErr
	}
	defer func() { _ = os.Remove(layout.manifestPathInternal()) }()
	layout.excludes = []string{
		".arcane-snapshot-*", RecoveryRequestName,
		layout.databaseName + "-wal", layout.databaseName + "-shm", layout.databaseName + "-journal",
	}
	files, err := snapshotSourceFilesInternal(ctx, layout)
	if err != nil {
		return backup.Snapshot{}, err
	}
	if stageSystemDatabaseErr := stageSystemDatabaseInternal(ctx, sqlDB, databasePath, stagedDatabase); stageSystemDatabaseErr != nil {
		return backup.Snapshot{}, stageSystemDatabaseErr
	}
	mounts, inContainer, err := currentMountsInternal(ctx, dockerClient)
	if err != nil {
		return backup.Snapshot{}, err
	}
	stagedMounts, err := sourceMountsInternal(mounts, inContainer, stagedDatabase, snapshotDatabase)
	if err != nil {
		return backup.Snapshot{}, err
	}
	layout.mounts = append(layout.mounts, stagedMounts...)
	input := backup.CreateSnapshotInput{Mounts: layout.mounts, Sources: layout.sources}
	for _, excluded := range layout.excludes {
		input.Globs = append(input.Globs, "!"+snapshotDataPath+"/"+excluded)
	}
	snapshot, err := s.engine.CreateSnapshot(ctx, dockerClient, repository, recoveryKey, "arcane-system-recovery", input)
	if err != nil {
		return backup.Snapshot{}, err
	}
	err = validateSnapshotSourcesInternal(ctx, layout, files)
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

// snapshotSourceFilesInternal records inputs before taking the database snapshot.
func snapshotSourceFilesInternal(ctx context.Context, layout backupSourceLayoutInternal) (map[string]os.FileInfo, error) {
	roots := []string{layout.dataDirectory}
	if layout.projectsPath == snapshotProjectsPath {
		roots = append(roots, layout.projectsDirectory)
	}
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
		if relative == layout.databaseName {
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
		files[filePath] = info
		return nil
	}
	for _, root := range roots {
		err := filepath.WalkDir(root, visit)
		if err != nil {
			return nil, fmt.Errorf("inspect system backup sources: %w", err)
		}
	}
	return files, nil
}

func validateSnapshotSourcesInternal(ctx context.Context, layout backupSourceLayoutInternal, before map[string]os.FileInfo) error {
	after, err := snapshotSourceFilesInternal(ctx, layout)
	if err != nil {
		return err
	}
	if len(before) != len(after) {
		return errors.New("system backup source files changed during capture; retry when file updates finish")
	}
	for filePath, original := range before {
		current, exists := after[filePath]
		if !exists || !os.SameFile(original, current) || original.Size() != current.Size() || original.Mode() != current.Mode() || !original.ModTime().Equal(current.ModTime()) {
			return fmt.Errorf("system backup source %q changed during capture; retry when file updates finish", filePath)
		}
	}
	return nil
}

func stageSystemDatabaseInternal(ctx context.Context, db *sql.DB, databasePath, stagedDatabase string) error {
	info, err := os.Stat(databasePath)
	if err != nil {
		return err
	}
	if _, execContextErr := db.ExecContext(ctx, "VACUUM INTO ?", stagedDatabase); execContextErr != nil {
		return fmt.Errorf("stage Arcane database: %w", execContextErr)
	}
	if stat, ok := info.Sys().(*syscall.Stat_t); ok {
		if chownErr := os.Chown(stagedDatabase, int(stat.Uid), int(stat.Gid)); chownErr != nil {
			return fmt.Errorf("preserve staged database ownership: %w", chownErr)
		}
	}
	if chmodErr := os.Chmod(stagedDatabase, info.Mode()); chmodErr != nil {
		return fmt.Errorf("preserve staged database permissions: %w", chmodErr)
	}
	return nil
}
