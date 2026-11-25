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
	"time"

	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/api/types/mount"
	"github.com/moby/moby/client"
	"gorm.io/gorm"

	"kit.getarcane.app"
	"kit.getarcane.app/acfs"
	acfstypes "kit.getarcane.app/acfs/types"
	"kit.getarcane.app/docker"

	"getarcane.dev/arcane/v3/internal/backup"
	"getarcane.dev/arcane/v3/internal/common"
	"getarcane.dev/arcane/v3/internal/config"
	"getarcane.dev/arcane/v3/internal/config/schema"
	"getarcane.dev/arcane/v3/internal/settings"
	"getarcane.dev/arcane/v3/pkg/activity"
	"getarcane.dev/arcane/v3/pkg/backupbrowser"
	"getarcane.dev/arcane/v3/pkg/francis"
	git "getarcane.dev/arcane/v3/pkg/gitutil"
	"getarcane.dev/arcane/v3/pkg/pagination"
	"getarcane.dev/arcane/v3/pkg/rustic"
	"getarcane.dev/arcane/v3/pkg/volumes"
	"getarcane.dev/types/v3"
)

type (
	Service struct {
		engine              *backup.Engine
		sqlDB               func() (*sql.DB, error)
		actorStore          func() (*gorm.DB, error)
		localRepository     func(ctx context.Context, dockerClient *client.Client, readOnly bool) (backup.Repository, error)
		remoteRepository    func(ctx context.Context, destinationID string) (backup.Repository, error)
		databaseFile        func() (string, error)
		projectRoots        func(ctx context.Context) ([]string, error)
		recoveryEnvironment func(ctx context.Context) map[string]string
		gitWorkDirectory    string
	}

	ServiceParams struct {
		Engine              *backup.Engine
		SqlDB               func() (*sql.DB, error)
		ActorStore          func() (*gorm.DB, error)
		LocalRepository     func(ctx context.Context, dockerClient *client.Client, readOnly bool) (backup.Repository, error)
		RemoteRepository    func(ctx context.Context, destinationID string) (backup.Repository, error)
		DatabaseFile        func() (string, error)
		ProjectRoots        func(ctx context.Context) ([]string, error)
		RecoveryEnvironment func(ctx context.Context) map[string]string
		GitWorkDirectory    string
	}

	CreateOptions struct {
		RecoveryKey     string
		BackupID        string
		IncludeProjects bool
	}

	systemBackupSnapshotLocation struct {
		name            string
		destination     types.SystemBackupDestination
		s3DestinationID string
		snapshotID      string
		repository      backup.Repository
	}

	systemBackupSnapshot struct {
		systemBackupSnapshotLocation

		layout  snapshotLayout
		entries []types.BackupFileEntry
	}

	systemBackupSafetySnapshot struct {
		paths map[string]struct{}

		layout snapshotLayout
		systemBackupSnapshotLocation
	}

	systemBackupSnapshotSession struct {
		service      *Service
		dockerClient *client.Client
		recoveryKey  string
	}

	RestorePlan struct {
		SnapshotID       string
		Stages           []types.RecoveryRestoreStage
		ProjectsIncluded bool
	}

	RestoreTarget struct {
		DataDirectory string
		Mounts        []container.MountPoint
	}

	backupSourceLayout struct {
		dataDirectory     string
		projectsDirectory string
		databaseName      string
		projectsPath      string
		nestedDataPath    string // data directory inside /projects when the projects bind contains it on the host
		mounts            []mount.Mount
		sources           []string
		excludes          []string
		excludedRoots     []string // data-relative project roots a backup without projects leaves out
	}

	snapshotLayout struct {
		dataPath      string // "/" or "/app/data" for version 1, "/data" for version 2
		projectsPath  string // absolute snapshot path; empty when the backup omitted projects
		databaseName  string
		excludedRoots []string // data-relative project roots the backup left out
	}

	projectsRestoreDestination struct {
		directory string
		dataPath  string // project-relative path of Arcane's data directory when the projects bind contains it on the host
		target    types.RecoveryRestoreTarget
	}
)

const (
	RecoveryManifestName           = ".arcane-recovery.json"
	RecoveryRequestName            = ".arcane-recovery-request.json"
	snapshotDataPath               = "/data"
	snapshotProjectsPath           = "/projects"
	recoveryDataRestoreTarget      = "/restore"
	recoveryProjectsRestoreTarget  = "/restore-projects"
	selectiveProjectsRestoreTarget = "/arcane-projects"
)

var (
	errProjectsOutsideData = errors.New("the backup-time projects directory is outside Arcane's system backup data")
	errArcaneOnlyBackup    = common.Classify(common.ErrValidation, errors.New(
		"this system backup covers Arcane only; restore project files from the project's backups",
	))
	manifestCandidateRoots = []string{snapshotDataPath, "/", "/app/data"}
	sqliteSidecars         = []string{"-wal", "-shm", "-journal"}
	globEscaper            = strings.NewReplacer(`\`, `\\`, `*`, `\*`, `?`, `\?`, `[`, `\[`, `]`, `\]`, `{`, `\{`, `}`, `\}`)
)

func NewService(p ServiceParams) *Service {
	return &Service{
		engine:              p.Engine,
		sqlDB:               p.SqlDB,
		actorStore:          p.ActorStore,
		localRepository:     p.LocalRepository,
		remoteRepository:    p.RemoteRepository,
		databaseFile:        p.DatabaseFile,
		projectRoots:        p.ProjectRoots,
		recoveryEnvironment: p.RecoveryEnvironment,
		gitWorkDirectory:    p.GitWorkDirectory,
	}
}

func (s *Service) backupSnapshotLocations(ctx context.Context, dockerClient *client.Client, run types.SystemBackupRun) ([]systemBackupSnapshotLocation, error) {
	locations := make([]systemBackupSnapshotLocation, 0, 2)
	var setupErr error
	if run.LocalSnapshotID != "" {
		repository, err := s.localRepository(ctx, dockerClient, true)
		if err != nil {
			setupErr = errors.Join(setupErr, fmt.Errorf("open local system backup repository: %w", err))
		} else {
			locations = append(locations, systemBackupSnapshotLocation{
				name: "local", destination: types.SystemBackupDestinationLocal,
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
				name: "S3", destination: types.SystemBackupDestinationS3, s3DestinationID: run.S3DestinationID,
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

func (session systemBackupSnapshotSession) inspectProjectSnapshot(
	ctx context.Context,
	location systemBackupSnapshotLocation,
	selection types.BackupRestoreSelection,
) (
	systemBackupSnapshot,
	error,
) {
	snapshot, err := session.inspectProjectManifest(ctx, location)
	if err != nil {
		return systemBackupSnapshot{}, err
	}
	list := func(folder string, recursive bool) ([]types.BackupFileEntry, error) {
		listed, listErr := session.service.engine.ListSnapshotFiles(
			ctx,
			session.dockerClient,
			location.repository,
			session.recoveryKey,
			location.snapshotID,
			path.Join(snapshot.layout.projectsPath, folder)+"/",
			recursive,
		)
		return projectEntriesFromSnapshot(listed, snapshot.layout, folder, recursive), listErr
	}
	entries, err := backupbrowser.ParentEntries(selection, list)
	if err != nil {
		return systemBackupSnapshot{}, fmt.Errorf("inspect %s project files: %w", location.name, err)
	}
	return systemBackupSnapshot{
		systemBackupSnapshotLocation: snapshot.systemBackupSnapshotLocation,
		layout:                       snapshot.layout,
		entries:                      entries,
	}, nil
}

func (session systemBackupSnapshotSession) inspectProjectManifest(ctx context.Context, location systemBackupSnapshotLocation) (systemBackupSnapshot, error) {
	snapshot, err := session.inspectReadableSnapshot(ctx, location)
	if err != nil {
		return systemBackupSnapshot{}, err
	}
	if snapshot.layout.projectsPath == "" {
		return systemBackupSnapshot{}, errArcaneOnlyBackup
	}
	return snapshot, nil
}

func (session systemBackupSnapshotSession) availableProjectSnapshots(
	ctx context.Context,
	locations []systemBackupSnapshotLocation,
	setupErr error,
	selection types.BackupRestoreSelection,
	firstOnly bool,
) (
	[]systemBackupSnapshot,
	error,
) {
	snapshots := make([]systemBackupSnapshot, 0, len(locations))
	inspectErr := setupErr
	for _, location := range locations {
		snapshot, err := session.inspectProjectSnapshot(ctx, location, selection)
		if errors.Is(err, errArcaneOnlyBackup) {
			return nil, err
		}
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

func projectEntriesFromSnapshot(files []string, layout snapshotLayout, browsePath string, recursive bool) []types.BackupFileEntry {
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

func (s *Service) Browse(
	ctx context.Context,
	dockerClient *client.Client,
	key string,
	run types.SystemBackupRun,
	listPath string,
	recursive bool,
	params pagination.QueryParams,
) ([]types.BackupFileEntry, pagination.Response, error) {
	session := systemBackupSnapshotSession{service: s, dockerClient: dockerClient, recoveryKey: key}
	locations, setupErr := s.backupSnapshotLocations(ctx, dockerClient, run)
	browseErr := setupErr
	for _, location := range locations {
		snapshot, inspectErr := session.inspectProjectManifest(ctx, location)
		if errors.Is(inspectErr, errArcaneOnlyBackup) {
			return nil, pagination.Response{}, inspectErr
		}
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

func (session systemBackupSnapshotSession) openSafetySnapshot(
	ctx context.Context,
	source systemBackupSnapshot,
	safetyRun *types.SystemBackupRun,
	selection types.BackupRestoreSelection,
) (
	systemBackupSafetySnapshot,
	error,
) {
	location := systemBackupSnapshotLocation{ //exhaustruct:enforce
		name:            "safety",
		destination:     source.destination,
		s3DestinationID: source.s3DestinationID,
		repository:      source.repository,
		snapshotID:      kit.Ternary(source.destination == types.SystemBackupDestinationS3, safetyRun.RemoteSnapshotID, safetyRun.LocalSnapshotID),
	}
	if location.snapshotID == "" {
		return systemBackupSafetySnapshot{}, errors.New("pre-restore system backup has no snapshot in the selected repository")
	}
	snapshot, err := session.inspectProjectSnapshot(ctx, location, selection)
	if err != nil {
		return systemBackupSafetySnapshot{}, fmt.Errorf("open pre-restore system backup: %w", err)
	}
	paths := make(map[string]struct{}, len(snapshot.entries))
	for _, entry := range snapshot.entries {
		paths[entry.Path] = struct{}{}
	}
	return systemBackupSafetySnapshot{
		systemBackupSnapshotLocation: location,
		layout:                       snapshot.layout,
		paths:                        paths,
	}, nil
}

func (session systemBackupSnapshotSession) restoreEntry(
	ctx context.Context,
	snapshots []systemBackupSnapshot,
	selected types.BackupFileEntry,
	destination projectsRestoreDestination,
) error {
	var restoreErr error
	for _, snapshot := range snapshots {
		projectsRoot := selected.Path == "" && selected.IsDirectory
		if !projectsRoot && !slices.ContainsFunc(snapshot.entries, func(entry types.BackupFileEntry) bool {
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
	selected []types.BackupFileEntry,
	destination projectsRestoreDestination,
) error {
	var rollbackErr error
	for _, selectedEntry := range slices.Backward(selected) {
		if !safetySnapshotContainsPath(safety, selectedEntry.Path) {
			if selectedEntry.Path == "" {
				rollbackErr = errors.Join(rollbackErr, errors.New("refusing to remove the projects directory itself"))
				continue
			}
			if err := (acfs.Root{Path: destination.directory}).RemoveAll(ctx, selectedEntry.Path); err != nil {
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
	selected []types.BackupFileEntry,
	destination projectsRestoreDestination,
) error {
	restored := make([]types.BackupFileEntry, 0, len(selected))
	for _, selectedEntry := range selected {
		if err := session.restoreEntry(ctx, snapshots, selectedEntry, destination); err != nil {
			rollbackErr := session.rollback(context.WithoutCancel(ctx), safety, append(slices.Clone(restored), selectedEntry), destination)
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

func (s *Service) RestoreFiles(
	ctx context.Context,
	dockerClient *client.Client,
	key string,
	run types.SystemBackupRun,
	selection types.BackupRestoreSelection,
	createSafety func(context.Context, types.CreateSystemBackupRequest) (*types.SystemBackupRun, error),
) error {
	session := systemBackupSnapshotSession{service: s, dockerClient: dockerClient, recoveryKey: key}
	locations, setupErr := s.backupSnapshotLocations(ctx, dockerClient, run)
	snapshots, err := session.availableProjectSnapshots(ctx, locations, setupErr, selection, false)
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
	safetyRun, err := createSafety(ctx, types.CreateSystemBackupRequest{
		Destination:     kit.Ternary(source.destination == types.SystemBackupDestinationS3, types.SystemBackupDestinationS3, types.SystemBackupDestinationLocal),
		S3DestinationID: kit.Ternary(source.destination == types.SystemBackupDestinationS3, source.s3DestinationID, ""),
		RecoveryKey:     key,
	})
	if err != nil {
		return fmt.Errorf("failed to create pre-restore system backup: %w", err)
	}
	safety, err := session.openSafetySnapshot(ctx, source, safetyRun, selection)
	if err != nil {
		return err
	}
	return session.restoreSelected(ctx, snapshots, safety, selected, destination)
}

func (s *Service) PlanRestore(
	ctx context.Context,
	dockerClient *client.Client,
	key string,
	run types.SystemBackupRun,
	target RestoreTarget,
) (RestorePlan, error) {
	session := systemBackupSnapshotSession{service: s, dockerClient: dockerClient, recoveryKey: key}
	locations, setupErr := s.backupSnapshotLocations(ctx, dockerClient, run)
	snapshot, err := session.firstReadableSnapshot(ctx, locations, setupErr)
	if err != nil {
		return RestorePlan{}, err
	}
	roots, err := s.projectRoots(ctx)
	if err != nil {
		return RestorePlan{}, err
	}
	repository := types.RecoveryRestoreRepository{Environment: snapshot.repository.Environment, Mounts: snapshot.repository.Mounts}
	stages, err := restoreStages(target, roots, repository, snapshot.snapshotID, snapshot.layout)
	if err != nil {
		return RestorePlan{}, fmt.Errorf("plan system restore: %w", err)
	}
	if snapshot.layout.projectsPath == "" {
		stages, err = withoutMissingMountStages(stages, func(folder string) ([]string, error) {
			return s.engine.ListSnapshotFiles(ctx, dockerClient, snapshot.repository, key, snapshot.snapshotID, folder+"/", false)
		})
		if err != nil {
			return RestorePlan{}, fmt.Errorf("plan system restore: %w", err)
		}
	}
	return RestorePlan{SnapshotID: snapshot.snapshotID, ProjectsIncluded: (snapshot.layout.projectsPath != ""), Stages: stages}, nil
}

func (s *Service) PlanRollback(
	ctx context.Context,
	dockerClient *client.Client,
	key, localSnapshotID string,
	target RestoreTarget,
) ([]types.RecoveryRestoreStage, error) {
	localRepository, err := s.localRepository(ctx, dockerClient, true)
	if err != nil {
		return nil, err
	}
	roots, err := s.projectRoots(ctx)
	if err != nil {
		return nil, err
	}
	session := systemBackupSnapshotSession{service: s, dockerClient: dockerClient, recoveryKey: key}
	safetyLocation := systemBackupSnapshotLocation{
		name:        "safety",
		destination: types.SystemBackupDestinationLocal,
		repository:  localRepository,
		snapshotID:  localSnapshotID,
	}
	safetyLayout, err := session.readSnapshotLayout(ctx, safetyLocation)
	if err != nil {
		return nil, fmt.Errorf("open pre-restore system backup: %w", err)
	}
	repository := types.RecoveryRestoreRepository{Environment: safetyLocation.repository.Environment, Mounts: safetyLocation.repository.Mounts}
	stages, err := restoreStages(target, roots, repository, localSnapshotID, safetyLayout)
	if err != nil {
		return nil, fmt.Errorf("plan system restore rollback: %w", err)
	}
	return stages, nil
}

func normalizeSystemBackupSelection(selection types.BackupRestoreSelection, snapshot systemBackupSnapshot) ([]types.BackupFileEntry, error) {
	if selection.SelectAll && strings.TrimSpace(selection.Search) == "" && snapshot.layout.projectsPath != snapshot.layout.dataPath {
		return backupbrowser.NormalizeSelection(selection, []types.BackupFileEntry{{Path: "", Name: path.Base(snapshot.layout.projectsPath), IsDirectory: true}})
	}
	return backupbrowser.NormalizeSelection(selection, snapshot.entries)
}

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

func nestedDataPath(mounts []container.MountPoint, dataDirectory, projectsDirectory string) (relative string, nested bool, err error) {
	// Projects kept at the data root share one mount by design; projectsSnapshotPath owns that layout.
	if path.Clean(filepath.ToSlash(dataDirectory)) == path.Clean(filepath.ToSlash(projectsDirectory)) {
		return "", false, nil
	}
	projectsSource, projectsMounted := hostSource(mounts, projectsDirectory)
	dataSource, dataMounted := hostSource(mounts, dataDirectory)
	if !projectsMounted || !dataMounted || !kit.FilePathMatches(dataSource, projectsSource) {
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

func hostSource(mounts []container.MountPoint, directory string) (string, bool) {
	resolved := docker.MountForSubpath(mounts, directory, "")
	if resolved == nil {
		return "", false
	}
	return kit.Ternary(
		resolved.Type == mount.TypeVolume,
		path.Join("volume:"+resolved.Source, kit.FromPtr(resolved.VolumeOptions).Subpath),
		path.Clean(filepath.ToSlash(resolved.Source)),
	), true
}

func projectRootsInData(mounts []container.MountPoint, dataDirectory string, roots []string) ([]string, error) {
	data := path.Clean(filepath.ToSlash(dataDirectory))
	dataSource, dataMounted := hostSource(mounts, data)
	resolvedData := data
	if resolved, err := filepath.EvalSymlinks(data); err == nil {
		resolvedData = path.Clean(filepath.ToSlash(resolved))
	}
	relatives := make([]string, 0, len(roots))
	for _, root := range roots {
		cleaned := path.Clean(filepath.ToSlash(root))
		candidates := [][2]string{{cleaned, data}}
		if resolved, err := filepath.EvalSymlinks(root); err == nil {
			candidates = append(candidates, [2]string{path.Clean(filepath.ToSlash(resolved)), resolvedData})
		}
		rootSource, rootMounted := hostSource(mounts, cleaned)
		if rootMounted && dataMounted {
			candidates = append(candidates, [2]string{rootSource, dataSource})
		}
		for _, candidate := range candidates {
			if !kit.FilePathMatches(candidate[0], candidate[1]) {
				continue
			}
			if candidate[0] == candidate[1] {
				return nil, common.Classify(common.ErrValidation, fmt.Errorf(
					"the projects folder %s is Arcane's data folder. System backups cover Arcane only, so projects must "+
						"live in their own folder, such as %s; move them there and update the projects directory setting",
					root, path.Join(data, "projects"),
				))
			}
			relatives = append(relatives, strings.TrimPrefix(candidate[0], candidate[1]+"/"))
		}
		if !rootMounted {
			continue
		}
		for _, nested := range mounts {
			destination := path.Clean(filepath.ToSlash(nested.Destination))
			relative, inside := strings.CutPrefix(destination, data+"/")
			nestedSource, nestedMounted := hostSource(mounts, destination)
			switch {
			case !inside || !nestedMounted:
			case kit.FilePathMatches(nestedSource, rootSource):
				relatives = append(relatives, relative)
			case kit.FilePathMatches(rootSource, nestedSource):
				relatives = append(relatives, path.Join(relative, strings.TrimPrefix(rootSource, nestedSource+"/")))
			}
		}
	}
	slices.Sort(relatives)
	return slices.Compact(relatives), nil
}

func withoutProjects(layout backupSourceLayout, mounts []container.MountPoint, roots []string) (backupSourceLayout, error) {
	excluded, err := projectRootsInData(mounts, layout.dataDirectory, roots)
	if err != nil {
		return backupSourceLayout{}, err
	}
	if _, err = dataMountsOutsideProjects(mounts, layout.dataDirectory, excluded); err != nil {
		return backupSourceLayout{}, err
	}
	excludes := slices.Clone(layout.excludes)
	for _, relative := range excluded {
		excludes = append(excludes, globEscaper.Replace(relative))
	}
	return backupSourceLayout{
		dataDirectory: layout.dataDirectory,
		databaseName:  layout.databaseName,
		mounts: slices.DeleteFunc(slices.Clone(layout.mounts), func(source mount.Mount) bool {
			return slices.ContainsFunc(excluded, func(relative string) bool {
				return kit.FilePathMatches(source.Target, path.Join(snapshotDataPath, relative))
			})
		}),
		sources:       layout.sources,
		excludes:      excludes,
		excludedRoots: excluded,
	}, nil
}

func dataMountsOutsideProjects(mounts []container.MountPoint, dataDirectory string, projectRoots []string) ([]string, error) {
	data := path.Clean(filepath.ToSlash(dataDirectory))
	var nested []string
	for _, candidate := range mounts {
		relative, inside := strings.CutPrefix(path.Clean(candidate.Destination), data+"/")
		if !inside || slices.ContainsFunc(projectRoots, func(root string) bool { return kit.FilePathMatches(relative, root) }) {
			continue
		}
		if index := slices.IndexFunc(projectRoots, func(root string) bool { return kit.FilePathMatches(root, relative) }); index >= 0 {
			return nil, common.Classify(common.ErrValidation, fmt.Errorf(
				"%s is a separate mount that contains the projects folder %s; system backups cover Arcane only and a restore "+
					"cannot replace that mount without touching projects, so mount the projects folder outside it first",
				candidate.Destination, path.Join(data, projectRoots[index]),
			))
		}
		nested = append(nested, relative)
	}
	return nested, nil
}

func (s *Service) backupSourceLayout(ctx context.Context, dockerClient *client.Client, opts CreateOptions) (backupSourceLayout, error) {
	databaseFile, err := s.databaseFile()
	if err != nil {
		return backupSourceLayout{}, err
	}
	roots, err := s.projectRoots(ctx)
	if err != nil {
		return backupSourceLayout{}, err
	}
	dataDirectory, projectsDirectory, databaseName := filepath.Dir(databaseFile), roots[0], filepath.Base(databaseFile)
	excludes := []string{".arcane-snapshot-*", RecoveryRequestName, rustic.TrustBundleName}
	for _, name := range []string{databaseName, francis.StorePath(databaseName)} {
		for _, sidecar := range sqliteSidecars {
			excludes = append(excludes, name+sidecar)
		}
	}
	gitWorkDirectory, err := filepath.Abs(s.gitWorkDirectory)
	if err != nil {
		return backupSourceLayout{}, fmt.Errorf("failed to resolve the git work directory: %w", err)
	}
	if relative, relErr := filepath.Rel(dataDirectory, gitWorkDirectory); relErr == nil && relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		excludes = append(excludes, path.Join(globEscaper.Replace(filepath.ToSlash(relative)), git.CloneScratchPrefix+"*"))
	}
	mounts, inContainer, err := backup.CurrentMounts(ctx, dockerClient)
	if err != nil {
		return backupSourceLayout{}, err
	}
	dataMounts, err := backup.SourceMounts(backup.SourceMountOptions{Mounts: mounts, ContainerPath: dataDirectory, Target: snapshotDataPath, InContainer: inContainer})
	if err != nil {
		return backupSourceLayout{}, fmt.Errorf("resolve Arcane data source: %w", err)
	}
	if !opts.IncludeProjects {
		return withoutProjects(backupSourceLayout{
			dataDirectory: dataDirectory,
			databaseName:  databaseName,
			mounts:        dataMounts,
			sources:       []string{snapshotDataPath},
			excludes:      excludes,
		}, mounts, roots)
	}
	projectsPath, external, err := projectsSnapshotPath(dataDirectory, projectsDirectory)
	if err != nil {
		return backupSourceLayout{}, err
	}
	if !external {
		return backupSourceLayout{
			dataDirectory:     dataDirectory,
			projectsDirectory: projectsDirectory,
			databaseName:      databaseName,
			projectsPath:      projectsPath,
			mounts:            dataMounts,
			sources:           []string{snapshotDataPath},
			excludes:          excludes,
		}, nil
	}
	projectMounts, err := backup.SourceMounts(backup.SourceMountOptions{Mounts: mounts, ContainerPath: projectsDirectory, Target: snapshotProjectsPath, InContainer: inContainer})
	if err != nil {
		return backupSourceLayout{}, fmt.Errorf("resolve projects source: %w", err)
	}
	relative, nested, err := nestedDataPath(mounts, dataDirectory, projectsDirectory)
	if err != nil {
		return backupSourceLayout{}, err
	}
	return backupSourceLayout{
		dataDirectory:     dataDirectory,
		projectsDirectory: projectsDirectory,
		databaseName:      databaseName,
		projectsPath:      projectsPath,
		nestedDataPath:    kit.Ternary(nested, path.Join(snapshotProjectsPath, relative), ""),
		mounts:            append(dataMounts, projectMounts...),
		sources:           []string{snapshotDataPath, snapshotProjectsPath},
		excludes:          excludes,
	}, nil
}

func (layout snapshotLayout) projectsDataRelative() (string, bool) {
	if layout.projectsPath == "" {
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
	var manifest types.RecoveryManifest
	if err := json.Unmarshal([]byte(manifestData), &manifest); err != nil {
		return snapshotLayout{}, fmt.Errorf("decode %s system recovery manifest: %w", location.name, err)
	}
	layout, err := snapshotLayoutFromManifest(manifest, manifestRoot)
	if err != nil {
		return snapshotLayout{}, fmt.Errorf("%s system recovery manifest: %w", location.name, err)
	}
	return layout, nil
}

func snapshotLayoutFromManifest(manifest types.RecoveryManifest, manifestRoot string) (snapshotLayout, error) {
	manifest.Environment = schema.ManifestEnvironment(manifest)
	switch manifest.FormatVersion {
	case 1:
		// Version 1 derives the layout from the manifest environment; projects
		// outside the captured data are reported as omitted.
		databasePath, err := recoveryManifestDatabasePath(manifest)
		if err != nil {
			return snapshotLayout{}, err
		}
		relative, err := projectsRelativePathFromManifest(manifest)
		if errors.Is(err, errProjectsOutsideData) {
			return snapshotLayout{dataPath: manifestRoot, databaseName: path.Base(databasePath)}, nil
		}
		if err != nil {
			return snapshotLayout{}, fmt.Errorf("resolve projects directory: %w", err)
		}
		return snapshotLayout{
			dataPath:     manifestRoot,
			projectsPath: path.Join(manifestRoot, relative),
			databaseName: path.Base(databasePath),
		}, nil
	case 2, types.RecoveryManifestProjectsFormatVersion, types.RecoveryManifestFormatVersion:
		dataPath, err := kit.NormalizeRelativePath(strings.TrimPrefix(manifest.DataPath, "/"))
		if err != nil {
			return snapshotLayout{}, fmt.Errorf("invalid data path: %w", err)
		}
		dataPath = "/" + dataPath
		if dataPath != manifestRoot {
			return snapshotLayout{}, fmt.Errorf("records data path %s but was found at %s", dataPath, manifestRoot)
		}
		databaseName, err := kit.NormalizeRelativePath(manifest.DatabasePath)
		if err != nil {
			return snapshotLayout{}, fmt.Errorf("invalid database path: %w", err)
		}
		if manifest.FormatVersion == types.RecoveryManifestFormatVersion {
			for _, root := range manifest.ExcludedProjectRoots {
				if normalized, rootErr := kit.NormalizeRelativePath(root); rootErr != nil || normalized != root {
					return snapshotLayout{}, fmt.Errorf("invalid excluded projects folder %q", root)
				}
			}
			return snapshotLayout{dataPath: dataPath, databaseName: databaseName, excludedRoots: manifest.ExcludedProjectRoots}, nil
		}
		projectsPath, err := kit.NormalizeRelativePath(strings.TrimPrefix(manifest.ProjectsPath, "/"))
		if err != nil {
			return snapshotLayout{}, fmt.Errorf("invalid projects path: %w", err)
		}
		return snapshotLayout{dataPath: dataPath, projectsPath: "/" + projectsPath, databaseName: databaseName}, nil
	default:
		return snapshotLayout{}, fmt.Errorf("uses unsupported format %d", manifest.FormatVersion)
	}
}

func recoveryManifestDatabasePath(manifest types.RecoveryManifest) (string, error) {
	databaseURL := strings.TrimSpace(manifest.Environment["ARCANE_DATABASE_URL"])
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

func projectsRelativePathFromManifest(manifest types.RecoveryManifest) (string, error) {
	configured := strings.TrimSpace(manifest.Environment[settings.EnvName("projectsDirectory", "")])
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

func restoreTarget(mounts []container.MountPoint, containerPath, target, exclude string) (types.RecoveryRestoreTarget, error) {
	enclosing, relative := docker.MountForEnclosingPath(mounts, containerPath, target)
	if enclosing == nil {
		return types.RecoveryRestoreTarget{}, fmt.Errorf("%s must be mounted into the Arcane container from a bind or named volume to restore into it", containerPath)
	}
	if enclosing.ReadOnly {
		return types.RecoveryRestoreTarget{}, fmt.Errorf("%s is mounted read-only into the Arcane container", containerPath)
	}
	destination := path.Join(target, relative)
	candidates := slices.DeleteFunc(slices.Clone(mounts), func(m container.MountPoint) bool {
		return exclude != "" && kit.FilePathMatches(m.Destination, exclude)
	})
	return types.RecoveryRestoreTarget{
		Mounts: append([]mount.Mount{*enclosing}, docker.NestedMounts(candidates, containerPath, destination)...),
		Path:   destination,
	}, nil
}

func (s *Service) projectsRestoreDestination(ctx context.Context, dockerClient *client.Client) (projectsRestoreDestination, error) {
	roots, err := s.projectRoots(ctx)
	if err != nil {
		return projectsRestoreDestination{}, err
	}
	directory := roots[0]
	mounts, inContainer, err := backup.CurrentMounts(ctx, dockerClient)
	if err != nil {
		return projectsRestoreDestination{}, err
	}
	if !inContainer {
		return projectsRestoreDestination{
			directory: directory,
			target: types.RecoveryRestoreTarget{
				Mounts: []mount.Mount{{
					Type:   mount.TypeBind,
					Source: directory,
					Target: selectiveProjectsRestoreTarget,
				}},
				Path: selectiveProjectsRestoreTarget,
			},
		}, nil
	}
	target, err := restoreTarget(mounts, directory, selectiveProjectsRestoreTarget, "")
	if err != nil {
		return projectsRestoreDestination{}, fmt.Errorf("resolve projects directory for restore: %w", err)
	}
	databaseFile, err := s.databaseFile()
	if err != nil {
		return projectsRestoreDestination{}, err
	}
	dataPath, _, err := nestedDataPath(mounts, filepath.Dir(databaseFile), directory)
	if err != nil {
		return projectsRestoreDestination{}, err
	}
	return projectsRestoreDestination{
		directory: directory,
		dataPath:  dataPath,
		target:    target,
	}, nil
}

func restoreStages(
	target RestoreTarget,
	roots []string,
	repository types.RecoveryRestoreRepository,
	snapshotID string,
	layout snapshotLayout,
) (
	[]types.RecoveryRestoreStage,
	error,
) {
	if layout.projectsPath == "" {
		return stagedRestoreStages(target, roots, repository, snapshotID, layout)
	}
	mounts, dataDirectory, projectsDirectory := target.Mounts, target.DataDirectory, roots[0]
	separate := (layout.projectsPath != "") && !layout.projectsCoveredByData(dataDirectory, projectsDirectory)
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
	stages := []types.RecoveryRestoreStage{{
		Repository: repository,
		SnapshotID: snapshotID,
		SourcePath: layout.dataPath,
		Target:     dataTarget,
	}}
	if !separate {
		return stages, nil
	}
	projectsTarget, err := restoreTarget(mounts, projectsDirectory, recoveryProjectsRestoreTarget, "")
	if err != nil {
		return nil, err
	}
	return append(stages, types.RecoveryRestoreStage{
		Repository: repository,
		SnapshotID: snapshotID,
		SourcePath: layout.projectsPath,
		Target:     projectsTarget,
	}), nil
}

func stagedRestoreStages(
	target RestoreTarget,
	roots []string,
	repository types.RecoveryRestoreRepository,
	snapshotID string,
	layout snapshotLayout,
) (
	[]types.RecoveryRestoreStage,
	error,
) {
	current, err := projectRootsInData(target.Mounts, target.DataDirectory, roots)
	if err != nil {
		return nil, err
	}
	projectRoots := slices.Concat(layout.excludedRoots, current)
	data := path.Clean(filepath.ToSlash(target.DataDirectory))
	nested, err := dataMountsOutsideProjects(target.Mounts, data, projectRoots)
	if err != nil {
		return nil, err
	}
	slices.Sort(nested)
	var outermost []string
	for _, relative := range nested {
		if !slices.ContainsFunc(outermost, func(outer string) bool { return kit.FilePathMatches(relative, outer) }) {
			outermost = append(outermost, relative)
		}
	}
	preserve := slices.Concat(projectRoots, outermost, []string{rustic.TrustBundleName})
	slices.Sort(preserve)
	preserve = slices.Compact(preserve)
	globs := make([]string, 0, 2*len(preserve))
	for _, relative := range preserve {
		globs = append(globs, "!/"+globEscaper.Replace(relative), "!/"+globEscaper.Replace(relative)+"/**")
	}
	dataTarget, err := restoreTarget(target.Mounts, target.DataDirectory, recoveryDataRestoreTarget, "")
	if err != nil {
		return nil, err
	}
	stages := []types.RecoveryRestoreStage{{
		Swap:       &types.RecoveryRestoreSwap{Directory: target.DataDirectory, ID: snapshotID, Preserve: preserve},
		Target:     types.RecoveryRestoreTarget{Mounts: dataTarget.Mounts[:1], Path: path.Join(dataTarget.Path, backup.SwapStagingPrefix+snapshotID)},
		SnapshotID: snapshotID,
		SourcePath: layout.dataPath,
		Globs:      globs,
		Repository: repository,
	}}
	for _, relative := range outermost {
		destination := path.Join(data, relative)
		if source := docker.MountForDestination(target.Mounts, destination, ""); source == nil || source.ReadOnly {
			continue
		}
		mountTarget, targetErr := restoreTarget(target.Mounts, destination, recoveryDataRestoreTarget, "")
		if targetErr != nil {
			return nil, targetErr
		}
		stages = append(stages, types.RecoveryRestoreStage{
			Swap:       nil,
			Target:     mountTarget,
			SnapshotID: snapshotID,
			SourcePath: path.Join(layout.dataPath, relative),
			Globs:      nil,
			Repository: repository,
		})
	}
	return stages, nil
}

func withoutMissingMountStages(stages []types.RecoveryRestoreStage, list func(folder string) ([]string, error)) ([]types.RecoveryRestoreStage, error) {
	var mountPaths []string
	for _, stage := range stages {
		if stage.Swap == nil {
			mountPaths = append(mountPaths, strings.TrimPrefix(stage.SourcePath, "/"))
		}
	}
	entries, err := backupbrowser.ListParents(mountPaths, func(folder string) ([]types.BackupFileEntry, error) {
		listed, listErr := list(folder)
		return backupbrowser.BuildEntries(listed, folder, false), listErr
	})
	if err != nil {
		return nil, fmt.Errorf("check the nested mounts in the backup: %w", err)
	}
	return slices.DeleteFunc(stages, func(stage types.RecoveryRestoreStage) bool {
		return stage.Swap == nil && !slices.ContainsFunc(entries, func(entry types.BackupFileEntry) bool {
			return entry.Path == strings.TrimPrefix(stage.SourcePath, "/")
		})
	}), nil
}

func (s *Service) Create(
	ctx context.Context,
	dockerClient *client.Client,
	repository backup.Repository,
	opts CreateOptions,
) (
	backup.Snapshot,
	error,
) {
	recoveryKey, backupID := opts.RecoveryKey, opts.BackupID
	layout, err := s.backupSourceLayout(ctx, dockerClient, opts)
	if err != nil {
		return backup.Snapshot{}, err
	}
	stage, err := acfs.Host{}.MkdirTemp(ctx, layout.dataDirectory, ".arcane-snapshot-")
	if err != nil {
		return backup.Snapshot{}, fmt.Errorf("create system backup staging directory: %w", err)
	}
	defer func() {
		_ = acfs.Host{}.RemoveAll(context.WithoutCancel(ctx), stage)
	}()
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
	manifest := types.RecoveryManifest{
		FormatVersion: kit.Ternary(layout.projectsPath == "", types.RecoveryManifestFormatVersion, types.RecoveryManifestProjectsFormatVersion),
		ArcaneVersion: config.Version, BackupID: backupID, ActivityID: activity.IDFromContext(ctx), CreatedAt: time.Now().UTC(),
		DataPath: snapshotDataPath, ProjectsPath: layout.projectsPath, DatabasePath: layout.databaseName,
		ExcludedProjectRoots: layout.excludedRoots, Environment: s.recoveryEnvironment(ctx),
	}
	manifestData, err := json.Marshal(manifest, jsontext.WithIndent("  "))
	if err != nil {
		return backup.Snapshot{}, fmt.Errorf("failed to encode recovery manifest: %w", err)
	}
	if err = (acfs.Root{Path: layout.dataDirectory}).Write(ctx, "/"+RecoveryManifestName, manifestData, acfs.WriteOptions{Mode: 0o600}); err != nil {
		return backup.Snapshot{}, fmt.Errorf("failed to write recovery manifest: %w", err)
	}
	defer func() {
		_ = acfs.Root{Path: layout.dataDirectory}.Remove(context.WithoutCancel(ctx), "/"+RecoveryManifestName)
	}()
	mounts, inContainer, err := backup.CurrentMounts(ctx, dockerClient)
	if err != nil {
		return backup.Snapshot{}, err
	}
	snapshotMounts := slices.Clone(layout.mounts)
	for name := range databases {
		stagedMounts, mountErr := backup.SourceMounts(backup.SourceMountOptions{
			Mounts: mounts, ContainerPath: filepath.Join(stage, name), Target: filepath.Join(snapshotDataPath, name), InContainer: inContainer,
		})
		if mountErr != nil {
			return backup.Snapshot{}, mountErr
		}
		snapshotMounts = append(snapshotMounts, stagedMounts...)
	}
	snapshotMounts, err = volumes.BindSubpaths(ctx, dockerClient, snapshotMounts)
	if err != nil {
		return backup.Snapshot{}, fmt.Errorf("system backup mounts: %w", err)
	}
	globs := make([]string, 0, len(layout.excludes)+1)
	for _, excluded := range layout.excludes {
		globs = append(globs, "!"+snapshotDataPath+"/"+excluded)
	}
	if layout.nestedDataPath != "" {
		globs = append(globs, "!"+layout.nestedDataPath)
	}
	var snapshot backup.Snapshot
	forget := func(ctx context.Context) error {
		if snapshot.ID == "" {
			return nil
		}
		cleanupCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), time.Minute)
		defer cancel()
		if forgetErr := s.engine.ForgetSnapshots(cleanupCtx, dockerClient, repository, recoveryKey, []string{snapshot.ID}); forgetErr != nil {
			return fmt.Errorf("remove unconfirmed system snapshot %s: %w", snapshot.ID, forgetErr)
		}
		return nil
	}
	capture := backup.CapturePlan{
		Inspect: nil,
		Stop: func(ctx context.Context) (func(context.Context) error, error) {
			for name, db := range databases {
				if stageErr := stageDatabase(ctx, db, filepath.Join(layout.dataDirectory, name), filepath.Join(stage, name)); stageErr != nil {
					return nil, stageErr
				}
			}
			return nil, nil
		},
		Watch: func(ctx context.Context) (func(context.Context) error, error) {
			files, filesErr := snapshotSourceFiles(ctx, layout)
			if filesErr != nil {
				return nil, filesErr
			}
			return func(ctx context.Context) error { return sourceFilesUnchanged(ctx, layout, files) }, nil
		},
		Rejected: func(ctx context.Context, _ error) error { return forget(ctx) },
		Mounts:   snapshotMounts,
		Volumes:  nil,
	}
	if captureErr := backup.Capture(ctx, capture, func(ctx context.Context, mounts []mount.Mount) error {
		var createErr error
		snapshot, createErr = s.engine.CreateSnapshot(ctx, dockerClient, repository, recoveryKey, "arcane-system-recovery", backup.CreateSnapshotInput{
			Mounts:  mounts,
			Sources: layout.sources,
			Globs:   globs,
		})
		return createErr
	}); captureErr != nil {
		return backup.Snapshot{}, captureErr
	}
	confirmed, err := s.engine.ConfirmRunSnapshot(ctx, dockerClient, repository, recoveryKey, backupID, snapshot.ID)
	if err != nil {
		return backup.Snapshot{}, errors.Join(err, forget(ctx))
	}
	return confirmed, nil
}

func sourceFilesUnchanged(ctx context.Context, layout backupSourceLayout, files map[string]acfstypes.Entry) error {
	after, err := snapshotSourceFiles(ctx, layout)
	if err != nil {
		return err
	}
	for filePath, original := range files {
		current, exists := after[filePath]
		if !exists ||
			original.Device != current.Device ||
			original.Inode != current.Inode ||
			original.UnixMode != current.UnixMode ||
			(!original.IsDirectory && (original.Size != current.Size || original.ModTimeUnixNano != current.ModTimeUnixNano)) {
			return fmt.Errorf("system backup source %q changed during capture; retry when file updates finish", filePath)
		}
	}
	for filePath := range after {
		if _, exists := files[filePath]; !exists {
			return fmt.Errorf("system backup source %q appeared during capture; retry when file updates finish", filePath)
		}
	}
	return nil
}

func snapshotSourceFiles(ctx context.Context, layout backupSourceLayout) (map[string]acfstypes.Entry, error) {
	var projectRoots []acfstypes.Entry
	for _, root := range append([]string{layout.projectsDirectory}, layout.excludedRoots...) {
		if root == "" {
			continue
		}
		if entry, statErr := (acfs.Host{}).Stat(ctx, kit.Ternary(filepath.IsAbs(root), root, filepath.Join(layout.dataDirectory, root)), acfstypes.StatOptions{FollowFinal: true}); statErr == nil {
			projectRoots = append(projectRoots, entry)
		}
	}
	dataEntry, err := acfs.Host{}.Stat(ctx, layout.dataDirectory, acfstypes.StatOptions{})
	if err != nil {
		return nil, fmt.Errorf("inspect system backup sources: %w", err)
	}
	actorDatabase := francis.StorePath(layout.databaseName)
	files := map[string]acfstypes.Entry{layout.dataDirectory: dataEntry}
	visit := func(entry acfstypes.Entry) error {
		relative := strings.TrimPrefix(entry.Path, "/")
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
			if entry.IsDirectory {
				return fs.SkipDir
			}
			return nil
		}
		if entry.IsDirectory && slices.ContainsFunc(projectRoots, func(root acfstypes.Entry) bool {
			return entry.Device == root.Device && entry.Inode == root.Inode
		}) {
			return fs.SkipDir
		}
		files[filepath.Join(layout.dataDirectory, entry.Path)] = entry
		return nil
	}
	if _, err = (acfs.Root{Path: layout.dataDirectory}).Walk(ctx, "/", acfstypes.WalkOptions{}, visit); err != nil {
		return nil, fmt.Errorf("inspect system backup sources: %w", err)
	}
	return files, nil
}

func stageDatabase(ctx context.Context, db *sql.DB, databasePath, stagedDatabase string) error {
	entry, err := acfs.Host{}.Stat(ctx, databasePath, acfstypes.StatOptions{FollowFinal: true})
	if err != nil {
		return err
	}
	if _, err = db.ExecContext(ctx, "VACUUM INTO ?", stagedDatabase); err != nil {
		return fmt.Errorf("stage Arcane database: %w", err)
	}
	if err = (acfs.Host{}).Chown(ctx, stagedDatabase, entry.UID, entry.GID); err != nil {
		return fmt.Errorf("preserve staged database ownership: %w", err)
	}
	if err = (acfs.Host{}).Chmod(ctx, stagedDatabase, os.FileMode(entry.UnixMode)); err != nil {
		return fmt.Errorf("preserve staged database permissions: %w", err)
	}
	return nil
}
