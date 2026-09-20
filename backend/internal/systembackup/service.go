package systembackup

import (
	"context"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"fmt"
	"log/slog"
	"maps"
	"os"
	"path"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"time"

	"emperror.dev/errors"

	"github.com/getarcaneapp/arcane/backend/v2/internal/activity"
	"github.com/getarcaneapp/arcane/backend/v2/internal/actors"
	"github.com/getarcaneapp/arcane/backend/v2/internal/backup"
	"github.com/getarcaneapp/arcane/backend/v2/internal/common"
	"github.com/getarcaneapp/arcane/backend/v2/internal/config"
	"github.com/getarcaneapp/arcane/backend/v2/internal/database"
	"github.com/getarcaneapp/arcane/backend/v2/internal/docker"
	s3domain "github.com/getarcaneapp/arcane/backend/v2/internal/s3"
	"github.com/getarcaneapp/arcane/backend/v2/internal/settings"
	"github.com/getarcaneapp/arcane/backend/v2/internal/system"
	"github.com/getarcaneapp/arcane/backend/v2/internal/volume"
	dockerutil "github.com/getarcaneapp/arcane/backend/v2/pkg/dockerutil"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/libarcane"
	activitylib "github.com/getarcaneapp/arcane/backend/v2/pkg/libarcane/activity"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/pagination"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/projects"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/scheduler/entityjobs"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/scheduler/jobcontext"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/utils"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/utils/backupbrowser"
	activitytypes "github.com/getarcaneapp/arcane/types/v2/activity"
	backuptypes "github.com/getarcaneapp/arcane/types/v2/backup"
	recoverytypes "github.com/getarcaneapp/arcane/types/v2/recovery"
	schedulertypes "github.com/getarcaneapp/arcane/types/v2/scheduler"
	"github.com/google/uuid"
	containertypes "github.com/moby/moby/api/types/container"
	"github.com/moby/moby/api/types/mount"
	"github.com/moby/moby/client"
	"go.getarcane.app/acfs"
	"go.getarcane.app/sys/cgroup"
	"gorm.io/gorm"
)

const (
	defaultSystemBackupSchedule = "0 0 3 * * *"
	systemRecoveryManifestName  = ".arcane-recovery.json"
	systemRecoveryRequestName   = ".arcane-recovery-request.json"
	systemRecoveryHelperPath    = "/app/arcane-recovery-helper"
	systemAdmissionID           = "system"
)

var ErrSystemBackupAlreadyRunning = errors.New("an Arcane system backup is already running")

type SystemBackupService struct {
	db              *database.DB
	dockerService   *docker.DockerClientService
	volumeService   *volume.VolumeService
	engine          *backup.Engine
	s3Destinations  *s3domain.S3DestinationService
	activityService *activity.ActivityService
	settingsService *settings.SettingsService
	config          *config.Config
	recoveryKeys    *backup.RecoveryKeyStore
	jobs            *entityjobs.Registry
}

func NewSystemBackupService(db *database.DB, dockerService *docker.DockerClientService, volumeService *volume.VolumeService, engine *backup.Engine, s3Destinations *s3domain.S3DestinationService, activityService *activity.ActivityService, settingsService *settings.SettingsService, cfg *config.Config, recoveryKeys *backup.RecoveryKeyStore) *SystemBackupService {
	return &SystemBackupService{
		db: db, dockerService: dockerService, volumeService: volumeService, engine: engine,
		s3Destinations: s3Destinations, activityService: activityService, settingsService: settingsService, config: cfg,
		recoveryKeys: recoveryKeys,
		jobs:         entityjobs.New("system-backup:", backup.SystemAdmissionScope),
	}
}

// SupportedDatabaseProvider reports whether system recovery works on the
// configured database. Recovery is currently SQLite-only.
func (s *SystemBackupService) SupportedDatabaseProvider() bool {
	return strings.HasPrefix(s.config.DatabaseURL, "file:")
}

func recoveryHelperExecutableInternal(mounts []containertypes.MountPoint, executablePath string) (string, *mount.Mount, error) {
	executablePath = strings.TrimSpace(executablePath)
	if executablePath == "" {
		return "", nil, errors.New("arcane executable path is empty")
	}
	executableMount := dockerutil.MountForSubpath(mounts, executablePath, systemRecoveryHelperPath)
	if executableMount == nil {
		return executablePath, nil, nil
	}
	if executableMount.Type != mount.TypeBind {
		return "", nil, errors.New("the Arcane executable must come from the image or a bind mount")
	}
	executableMount.ReadOnly = true
	return systemRecoveryHelperPath, executableMount, nil
}

func (s *SystemBackupService) recoveryEnvironmentInternal(ctx context.Context) map[string]string {
	result := make(map[string]string)
	var visit func(reflect.Value)
	visit = func(value reflect.Value) {
		if value.Kind() == reflect.Pointer {
			if value.IsNil() {
				return
			}
			value = value.Elem()
		}
		if value.Kind() != reflect.Struct {
			return
		}
		typeOf := value.Type()
		for i := range value.NumField() {
			fieldType := typeOf.Field(i)
			field := value.Field(i)
			if fieldType.Anonymous {
				visit(field)
				continue
			}
			name := fieldType.Tag.Get("env")
			if name == "" || !field.CanInterface() {
				continue
			}
			if field.Type() == reflect.TypeFor[os.FileMode]() {
				mode, ok := reflect.TypeAssert[os.FileMode](field)
				if ok {
					result[name] = fmt.Sprintf("%#o", uint32(mode))
				}
			} else {
				result[name] = fmt.Sprint(field.Interface())
			}
		}
	}
	visit(reflect.ValueOf(s.config))
	if value := s.projectsSettingInternal(ctx); value != "" {
		result["PROJECTS_DIRECTORY"] = value
	}
	return result
}

// databaseFileInternal resolves the SQLite database file: under /app/data in
// the container, the local data directory in host development.
func (s *SystemBackupService) databaseFileInternal() (string, error) {
	databasePath, err := utils.SQLitePathFromDSN(s.config.DatabaseURL)
	if err != nil {
		return "", fmt.Errorf("failed to parse database URL: %w", err)
	}
	if strings.TrimSpace(databasePath) == "" {
		return "", errors.New("cannot resolve the Arcane data directory from the database URL")
	}
	absolute, err := filepath.Abs(databasePath)
	if err != nil {
		return "", fmt.Errorf("failed to resolve the Arcane data directory: %w", err)
	}
	return absolute, nil
}

// dataDirectoryInternal resolves the directory holding the SQLite database.
// The recovery manifest must live inside it so every snapshot includes it.
func (s *SystemBackupService) dataDirectoryInternal() (string, error) {
	databaseFile, err := s.databaseFileInternal()
	if err != nil {
		return "", err
	}
	return filepath.Dir(databaseFile), nil
}

func (s *SystemBackupService) writeManifestInternal(ctx context.Context, backupID string, layout backupSourceLayoutInternal) error {
	manifest := recoverytypes.Manifest{
		FormatVersion: recoverytypes.ManifestFormatVersion, ArcaneVersion: config.Version, BackupID: backupID,
		ActivityID: activitylib.IDFromContext(ctx), CreatedAt: time.Now().UTC(),
		DataPath: snapshotDataPath, ProjectsPath: layout.projectsPath, DatabasePath: layout.databaseName,
		Environment: s.recoveryEnvironmentInternal(ctx),
	}
	data, err := json.Marshal(manifest, jsontext.WithIndent("  "))
	if err != nil {
		return fmt.Errorf("failed to encode recovery manifest: %w", err)
	}
	if err := os.WriteFile(layout.manifestPathInternal(), data, 0o600); err != nil {
		return fmt.Errorf("failed to write recovery manifest: %w", err)
	}
	return nil
}

func (s *SystemBackupService) localRepositoryInternal(ctx context.Context, dockerClient *client.Client, readOnly bool) (backup.Repository, error) {
	storage, err := s.volumeService.BackupStorageMount(ctx, dockerClient, "/repository", readOnly)
	if err != nil {
		return backup.Repository{}, err
	}
	return backup.Repository{
		ID:          "system:local",
		Environment: []string{"RUSTIC_REPOSITORY=/repository/system-recovery"},
		Mounts:      []mount.Mount{storage},
	}, nil
}

func (s *SystemBackupService) remoteRepositoryInternal(ctx context.Context, destinationID string) (backup.Repository, error) {
	configuration, err := s.s3Destinations.Configuration(ctx, destinationID)
	if err != nil {
		return backup.Repository{}, errors.New("the selected S3 backup destination is not configured")
	}
	return backup.Repository{
		ID:          "system:s3:" + destinationID,
		Environment: configuration.RusticEnvironment("arcane-system-recovery"),
	}, nil
}

func (s *SystemBackupService) createSnapshotInternal(ctx context.Context, dockerClient *client.Client, repository backup.Repository, recoveryKey string, layout backupSourceLayoutInternal) (backup.Snapshot, error) {
	return s.engine.CreateSnapshot(ctx, dockerClient, repository, recoveryKey, "arcane-system-recovery", backup.CreateSnapshotInput{Mounts: layout.mounts, Sources: layout.sources})
}

func (s *SystemBackupService) recoveryKeyInternal(ctx context.Context, supplied string) (string, error) {
	if strings.TrimSpace(supplied) != "" {
		if err := backup.ValidateRecoveryKey(supplied); err != nil {
			return "", err
		}
		return supplied, nil
	}
	key, err := s.recoveryKeys.Get(ctx)
	if errors.Is(err, backup.ErrRecoveryKeyNotConfigured) {
		return "", errors.New("enter the recovery key or configure one in system backups")
	}
	if err != nil {
		return "", err
	}
	return key, nil
}

// resolveBackupPlanInternal resolves which destinations a run targets, from
// either the policy or the request's destination enum.
func (s *SystemBackupService) resolveBackupPlanInternal(ctx context.Context, request backuptypes.CreateSystemBackupRequest) (localEnabled, s3Enabled bool, destinationID string, destination backuptypes.SystemBackupDestination, err error) {
	destinationID = strings.TrimSpace(request.S3DestinationID)
	if request.PolicyID != "" {
		policy, policyErr := s.loadPolicyInternal(ctx, request.PolicyID)
		if policyErr != nil {
			return false, false, "", "", policyErr
		}
		if policy == nil {
			return false, false, "", "", errors.New("system backup policy not found")
		}
		localEnabled, s3Enabled, destinationID = policy.LocalEnabled, policy.S3Enabled, policy.S3DestinationID
	} else {
		switch request.Destination {
		case backuptypes.SystemBackupDestinationLocal, "":
			localEnabled = true
		case backuptypes.SystemBackupDestinationS3:
			s3Enabled = true
		case backuptypes.SystemBackupDestinationLocalS3:
			localEnabled, s3Enabled = true, true
		default:
			return false, false, "", "", errors.New("unknown system backup destination")
		}
	}
	if !localEnabled && !s3Enabled {
		return false, false, "", "", errors.New("select at least one system backup destination")
	}
	if s3Enabled && destinationID == "" {
		return false, false, "", "", errors.New("select an S3 destination for the system backup")
	}
	destination = backuptypes.SystemBackupDestinationLocal
	if localEnabled && s3Enabled {
		destination = backuptypes.SystemBackupDestinationLocalS3
	} else if s3Enabled {
		destination = backuptypes.SystemBackupDestinationS3
	}
	return localEnabled, s3Enabled, destinationID, destination, nil
}

func (s *SystemBackupService) CreateBackup(ctx context.Context, user common.User, trigger SystemBackupTrigger, request backuptypes.CreateSystemBackupRequest) (*SystemBackupRun, error) {
	lease, admitted, err := s.engine.TryAcquireRun(ctx, backup.SystemAdmissionScope, systemAdmissionID)
	if err != nil {
		return nil, err
	}
	if !admitted {
		return nil, ErrSystemBackupAlreadyRunning
	}
	defer lease.Release()
	return s.createBackupInternal(ctx, trigger, request)
}

func (s *SystemBackupService) createStagedSystemRecoverySnapshotInternal(ctx context.Context, dockerClient *client.Client, recoveryKey, backupID, destinationID string, localEnabled, s3Enabled bool) (backup.Snapshot, backup.Repository, bool, error) {
	localRepository, err := s.localRepositoryInternal(ctx, dockerClient, false)
	var snapshot backup.Snapshot
	if err == nil {
		snapshot, err = s.snapshotWithDatabaseLockInternal(ctx, dockerClient, localRepository, recoveryKey, backupID)
	}
	if err == nil {
		return snapshot, localRepository, false, nil
	}
	localStagingErr := fmt.Errorf("failed to create local system recovery staging snapshot: %w", err)
	if localEnabled || !s3Enabled {
		return backup.Snapshot{}, backup.Repository{}, false, localStagingErr
	}
	// S3-only backups normally use local staging so the database lock does
	// not span an upload. If local storage is unusable, write directly to
	// S3 so remote backups and restore safety snapshots remain available.
	remoteRepository, repoErr := s.remoteRepositoryInternal(ctx, destinationID)
	if repoErr != nil {
		return backup.Snapshot{}, backup.Repository{}, false, errors.Combine(localStagingErr, repoErr)
	}
	remoteSnapshot, snapshotErr := s.snapshotWithDatabaseLockInternal(ctx, dockerClient, remoteRepository, recoveryKey, backupID)
	if snapshotErr != nil {
		return backup.Snapshot{}, backup.Repository{}, false, errors.Combine(localStagingErr, fmt.Errorf("failed to create S3 system recovery snapshot: %w", snapshotErr))
	}
	return remoteSnapshot, remoteRepository, true, nil
}

type preparedSystemBackupInternal struct {
	run          *SystemBackupRun
	recoveryKey  string
	localEnabled bool
	s3Enabled    bool
}

func (s *SystemBackupService) createBackupInternal(ctx context.Context, trigger SystemBackupTrigger, request backuptypes.CreateSystemBackupRequest) (*SystemBackupRun, error) {
	prepared, err := s.prepareBackupInternal(ctx, trigger, request)
	if err != nil {
		return nil, err
	}
	return s.executeBackupInternal(ctx, prepared)
}

func (s *SystemBackupService) prepareBackupInternal(ctx context.Context, trigger SystemBackupTrigger, request backuptypes.CreateSystemBackupRequest) (*preparedSystemBackupInternal, error) {
	if !s.SupportedDatabaseProvider() {
		return nil, errors.New("arcane system recovery currently requires the SQLite database provider")
	}
	recoveryKey, err := s.recoveryKeyInternal(ctx, request.RecoveryKey)
	if err != nil {
		return nil, err
	}
	localEnabled, s3Enabled, destinationID, destination, err := s.resolveBackupPlanInternal(ctx, request)
	if err != nil {
		return nil, err
	}
	run := &SystemBackupRun{CreatedAt: time.Now().UTC(), Status: SystemBackupStatusRunning, Trigger: trigger, Destination: destination, S3DestinationID: destinationID, PolicyID: request.PolicyID}
	run.ID = "system-" + uuid.NewString()
	if err := s.db.WithContext(ctx).Create(run).Error; err != nil {
		return nil, err
	}
	return &preparedSystemBackupInternal{run: run, recoveryKey: recoveryKey, localEnabled: localEnabled, s3Enabled: s3Enabled}, nil
}

func (s *SystemBackupService) executeBackupInternal(ctx context.Context, prepared *preparedSystemBackupInternal) (_ *SystemBackupRun, err error) {
	run, recoveryKey := prepared.run, prepared.recoveryKey
	localEnabled, s3Enabled, destinationID := prepared.localEnabled, prepared.s3Enabled, run.S3DestinationID

	defer func() {
		if err == nil {
			err = ctx.Err()
		}
		if err != nil {
			run.Status, run.Error = SystemBackupStatusFailed, err.Error()
		} else {
			run.Status, run.Error = SystemBackupStatusSucceeded, ""
		}
		if saveErr := s.db.WithContext(context.WithoutCancel(ctx)).Save(run).Error; saveErr != nil {
			err = errors.Combine(err, fmt.Errorf("failed to save system backup result: %w", saveErr))
		}
	}()
	defer utils.RecoverToError(&err, "system backup")
	dockerClient, err := s.dockerService.GetClient(ctx)
	if err != nil {
		return run, err
	}
	stagedSnapshot, stagingRepository, directRemote, err := s.createStagedSystemRecoverySnapshotInternal(
		ctx, dockerClient, recoveryKey, run.ID, destinationID, localEnabled, s3Enabled,
	)
	if err != nil {
		return run, err
	}
	if directRemote {
		run.RemoteSnapshotID, run.Size = stagedSnapshot.ID, stagedSnapshot.Size
		return run, nil
	}
	localRepository := stagingRepository
	// The database is locked only while the local staging snapshot is taken.
	// An S3 copy replicates that immutable snapshot afterwards, so the
	// exclusive lock never spans a network upload and the source is read once.
	// The staged snapshot is kept only for local-enabled runs. Forgetting it in
	// a defer covers the failure paths too: a failed replication otherwise
	// orphans one unreferenced snapshot in the staging repository per run.
	defer func() {
		if localEnabled {
			return
		}
		cleanupCtx := context.WithoutCancel(ctx)
		if forgetErr := s.engine.ForgetSnapshots(cleanupCtx, dockerClient, localRepository, recoveryKey, []string{stagedSnapshot.ID}); forgetErr != nil {
			slog.WarnContext(cleanupCtx, "failed to remove staged system recovery snapshot", "snapshot_id", stagedSnapshot.ID, "error", forgetErr)
		}
	}()
	if localEnabled {
		run.LocalSnapshotID, run.Size = stagedSnapshot.ID, stagedSnapshot.Size
	}
	if s3Enabled {
		remoteRepository, repoErr := s.remoteRepositoryInternal(ctx, destinationID)
		if repoErr != nil {
			return run, repoErr
		}
		remoteSnapshot, replicateErr := s.engine.Replicate(ctx, dockerClient, localRepository, stagedSnapshot.ID, remoteRepository, recoveryKey, "arcane-system-recovery")
		if replicateErr != nil {
			return run, fmt.Errorf("failed to create S3 system recovery snapshot: %w", replicateErr)
		}
		run.RemoteSnapshotID = remoteSnapshot.ID
		if run.Size == 0 {
			run.Size = remoteSnapshot.Size
		}
	}
	return run, nil
}

// snapshotWithDatabaseLockInternal takes a snapshot while the SQLite database
// is checkpointed and exclusively locked, and releases the lock before returning.
func (s *SystemBackupService) snapshotWithDatabaseLockInternal(ctx context.Context, dockerClient *client.Client, repository backup.Repository, recoveryKey, backupID string) (backup.Snapshot, error) {
	layout, err := s.backupSourceLayoutInternal(ctx, dockerClient)
	if err != nil {
		return backup.Snapshot{}, err
	}
	if err := s.writeManifestInternal(ctx, backupID, layout); err != nil {
		return backup.Snapshot{}, err
	}
	defer func() { _ = os.Remove(layout.manifestPathInternal()) }()
	sqlDB, err := s.db.SQLDB()
	if err != nil {
		return backup.Snapshot{}, err
	}
	if _, err := sqlDB.ExecContext(ctx, "PRAGMA wal_checkpoint(FULL)"); err != nil {
		return backup.Snapshot{}, fmt.Errorf("failed to checkpoint Arcane database: %w", err)
	}
	connection, err := sqlDB.Conn(ctx)
	if err != nil {
		return backup.Snapshot{}, fmt.Errorf("failed to reserve Arcane database connection: %w", err)
	}
	defer func() { _ = connection.Close() }()
	if _, err := connection.ExecContext(ctx, "BEGIN EXCLUSIVE"); err != nil {
		return backup.Snapshot{}, fmt.Errorf("failed to lock Arcane database for backup: %w", err)
	}
	defer func() { _, _ = connection.ExecContext(context.WithoutCancel(ctx), "ROLLBACK") }()
	snapshot, err := s.createSnapshotInternal(ctx, dockerClient, repository, recoveryKey, layout)
	if err != nil {
		return backup.Snapshot{}, fmt.Errorf("failed to create system recovery snapshot: %w", err)
	}
	return snapshot, nil
}

func (s *SystemBackupService) ListBackups(ctx context.Context, params pagination.QueryParams) ([]backuptypes.SystemBackupRun, pagination.Response, error) {
	var runs []SystemBackupRun
	query := s.db.WithContext(ctx).Model(&SystemBackupRun{})
	if term := strings.TrimSpace(params.Search); term != "" {
		pattern := "%" + term + "%"
		query = query.Where("status LIKE ? OR trigger LIKE ? OR destination LIKE ? OR error LIKE ?", pattern, pattern, pattern, pattern)
	}
	response, err := pagination.PaginateAndSortDB(params, query, &runs)
	if err != nil {
		return nil, pagination.Response{}, fmt.Errorf("failed to list system backups: %w", err)
	}
	names := make(map[string]backuptypes.S3Destination)
	if s.s3Destinations != nil {
		if available, listErr := s.s3Destinations.ListS3DestinationsByID(ctx); listErr == nil {
			names = available
		}
	}
	remoteAvailable := backup.RemoteSnapshotChecker(ctx, s.s3Destinations, "arcane-system-recovery")
	result := make([]backuptypes.SystemBackupRun, len(runs))
	for i := range runs {
		runs[i].S3DestinationName = names[runs[i].S3DestinationID].Name
		result[i] = runs[i].ToDTO()
		result[i].RemoteAvailable = remoteAvailable(runs[i].S3DestinationID, runs[i].RemoteSnapshotID)
	}
	return result, response, nil
}

func (s *SystemBackupService) backupInternal(ctx context.Context, id string) (*SystemBackupRun, error) {
	var run SystemBackupRun
	if err := s.db.WithContext(ctx).Where("id = ?", id).First(&run).Error; err != nil {
		return nil, err
	}
	return &run, nil
}

type systemBackupSnapshotLocationInternal struct {
	name            string
	destination     backuptypes.SystemBackupDestination
	s3DestinationID string
	repository      backup.Repository
	snapshotID      string
}

func (location systemBackupSnapshotLocationInternal) restoreRepositoryInternal() recoverytypes.RestoreRepository {
	return recoverytypes.RestoreRepository{Environment: location.repository.Environment, Mounts: location.repository.Mounts}
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
	service      *SystemBackupService
	dockerClient *client.Client
	recoveryKey  string
}

func (s *SystemBackupService) snapshotSessionInternal(dockerClient *client.Client, recoveryKey string) systemBackupSnapshotSessionInternal {
	return systemBackupSnapshotSessionInternal{service: s, dockerClient: dockerClient, recoveryKey: recoveryKey}
}

func (s *SystemBackupService) backupSnapshotLocationsInternal(ctx context.Context, dockerClient *client.Client, run *SystemBackupRun) ([]systemBackupSnapshotLocationInternal, error) {
	locations := make([]systemBackupSnapshotLocationInternal, 0, 2)
	var setupErr error
	if run.LocalSnapshotID != "" {
		repository, err := s.localRepositoryInternal(ctx, dockerClient, true)
		if err != nil {
			setupErr = errors.Combine(setupErr, fmt.Errorf("open local system backup repository: %w", err))
		} else {
			locations = append(locations, systemBackupSnapshotLocationInternal{
				name: "local", destination: backuptypes.SystemBackupDestinationLocal,
				repository: repository, snapshotID: run.LocalSnapshotID,
			})
		}
	}
	if run.RemoteSnapshotID != "" {
		repository, err := s.remoteRepositoryInternal(ctx, run.S3DestinationID)
		if err != nil {
			setupErr = errors.Combine(setupErr, fmt.Errorf("open S3 system backup repository: %w", err))
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

func (session systemBackupSnapshotSessionInternal) firstReadableSnapshotInternal(ctx context.Context, locations []systemBackupSnapshotLocationInternal, setupErr error) (systemBackupSnapshotInternal, error) {
	inspectErr := setupErr
	for _, location := range locations {
		snapshot, err := session.inspectReadableSnapshotInternal(ctx, location)
		if err == nil {
			return snapshot, nil
		}
		inspectErr = errors.Combine(inspectErr, err)
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

func (session systemBackupSnapshotSessionInternal) availableProjectSnapshotsInternal(ctx context.Context, locations []systemBackupSnapshotLocationInternal, setupErr error, firstOnly bool) ([]systemBackupSnapshotInternal, error) {
	snapshots := make([]systemBackupSnapshotInternal, 0, len(locations))
	inspectErr := setupErr
	for _, location := range locations {
		snapshot, err := session.inspectProjectSnapshotInternal(ctx, location)
		if err != nil {
			inspectErr = errors.Combine(inspectErr, err)
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

func protectedSystemBackupPathInternal(candidate, databasePath string) bool {
	if candidate == systemRecoveryManifestName || candidate == systemRecoveryRequestName {
		return true
	}
	return candidate == databasePath || candidate == databasePath+"-wal" || candidate == databasePath+"-shm" || candidate == databasePath+"-journal"
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
		normalized, err := utils.NormalizeRelativePath(projectRelative)
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

// BrowseBackupFiles returns one page from the backup's logical project tree.
func (s *SystemBackupService) BrowseBackupFiles(ctx context.Context, id, recoveryKey, requestedPath string, params pagination.QueryParams) ([]backuptypes.BackupFileEntry, pagination.Response, error) {
	listPath, recursive, err := backupbrowser.ListScope(requestedPath, params)
	if err != nil {
		return nil, pagination.Response{}, fmt.Errorf("%w: %w", common.ErrInvalidBackupSelection, err)
	}
	run, err := s.backupInternal(ctx, id)
	if err != nil {
		return nil, pagination.Response{}, err
	}
	if run.Status != SystemBackupStatusSucceeded {
		return nil, pagination.Response{}, errors.New("only successful system backups can be opened")
	}
	key, err := s.recoveryKeyInternal(ctx, recoveryKey)
	if err != nil {
		return nil, pagination.Response{}, err
	}
	dockerClient, err := s.dockerService.GetClient(ctx)
	if err != nil {
		return nil, pagination.Response{}, err
	}
	session := s.snapshotSessionInternal(dockerClient, key)
	locations, setupErr := s.backupSnapshotLocationsInternal(ctx, dockerClient, run)
	browseErr := setupErr
	for _, location := range locations {
		snapshot, inspectErr := session.inspectProjectManifestInternal(ctx, location)
		if inspectErr != nil {
			browseErr = errors.Combine(browseErr, inspectErr)
			continue
		}
		snapshotRoot := path.Join(snapshot.layout.projectsPath, listPath)
		listed, listErr := s.engine.ListSnapshotFiles(ctx, dockerClient, location.repository, key, location.snapshotID, snapshotRoot+"/", recursive)
		if listErr != nil {
			browseErr = errors.Combine(browseErr, fmt.Errorf("browse %s system recovery snapshot: %w", location.name, listErr))
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
func (session systemBackupSnapshotSessionInternal) openSafetySnapshotInternal(ctx context.Context, source systemBackupSnapshotInternal, safetyRun *SystemBackupRun) (systemBackupSafetySnapshotInternal, error) {
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

func (session systemBackupSnapshotSessionInternal) restoreEntryInternal(ctx context.Context, snapshots []systemBackupSnapshotInternal, selected backuptypes.BackupFileEntry, destination projectsRestoreDestinationInternal) error {
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
		restoreErr = errors.Combine(restoreErr, fmt.Errorf("restore from %s snapshot: %w", snapshot.name, err))
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

func (session systemBackupSnapshotSessionInternal) rollbackInternal(ctx context.Context, safety systemBackupSafetySnapshotInternal, selected []backuptypes.BackupFileEntry, destination projectsRestoreDestinationInternal) error {
	var rollbackErr error
	for _, selectedEntry := range slices.Backward(selected) {
		if !safetySnapshotContainsPathInternal(safety, selectedEntry.Path) {
			if err := removeProjectFileInternal(ctx, destination.directory, selectedEntry.Path); err != nil {
				rollbackErr = errors.Combine(rollbackErr, fmt.Errorf("remove newly restored project path %s: %w", selectedEntry.Path, err))
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
			rollbackErr = errors.Combine(rollbackErr, fmt.Errorf("restore project path %s from safety backup: %w", selectedEntry.Path, err))
		}
	}
	return rollbackErr
}

func (session systemBackupSnapshotSessionInternal) restoreSelectedInternal(ctx context.Context, snapshots []systemBackupSnapshotInternal, safety systemBackupSafetySnapshotInternal, selected []backuptypes.BackupFileEntry, destination projectsRestoreDestinationInternal) error {
	restored := make([]backuptypes.BackupFileEntry, 0, len(selected))
	for _, selectedEntry := range selected {
		if err := session.restoreEntryInternal(ctx, snapshots, selectedEntry, destination); err != nil {
			affected := slices.Clone(restored)
			affected = append(affected, selectedEntry)
			rollbackErr := session.rollbackInternal(context.WithoutCancel(ctx), safety, affected, destination)
			restoreErr := fmt.Errorf("failed to restore project path %s from system backup: %w", selectedEntry.Path, err)
			if rollbackErr != nil {
				return errors.Combine(restoreErr, fmt.Errorf("failed to roll back project files from pre-restore system backup: %w", rollbackErr))
			}
			return fmt.Errorf("%w; affected project files were rolled back", restoreErr)
		}
		restored = append(restored, selectedEntry)
	}
	return nil
}

// RestoreBackupFiles restores selected project files into the current projects
// directory after creating a safety snapshot.
func (s *SystemBackupService) RestoreBackupFiles(ctx context.Context, id string, request backuptypes.RestoreSystemBackupFilesRequest, user common.User) error {
	lease, admitted, err := s.engine.TryAcquireRun(ctx, backup.SystemAdmissionScope, systemAdmissionID)
	if err != nil {
		return err
	}
	if !admitted {
		return ErrSystemBackupAlreadyRunning
	}
	defer lease.Release()
	run, err := s.backupInternal(ctx, id)
	if err != nil {
		return err
	}
	if run.Status != SystemBackupStatusSucceeded {
		return errors.New("only successful system backups can be restored")
	}
	key, err := s.recoveryKeyInternal(ctx, request.RecoveryKey)
	if err != nil {
		return err
	}
	dockerClient, err := s.dockerService.GetClient(ctx)
	if err != nil {
		return err
	}
	session := s.snapshotSessionInternal(dockerClient, key)
	locations, setupErr := s.backupSnapshotLocationsInternal(ctx, dockerClient, run)
	snapshots, err := session.availableProjectSnapshotsInternal(ctx, locations, setupErr, false)
	if err != nil {
		return err
	}
	selected, err := normalizeSystemBackupSelectionInternal(request.RestoreSelection, snapshots[0])
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
	safetyRun, err := s.createBackupInternal(ctx, SystemBackupTriggerSafety, safetyRequest)
	if err != nil {
		return fmt.Errorf("failed to create pre-restore system backup: %w", err)
	}
	safety, err := session.openSafetySnapshotInternal(ctx, source, safetyRun)
	if err != nil {
		return err
	}
	return session.restoreSelectedInternal(ctx, snapshots, safety, selected, destination)
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

func (s *SystemBackupService) DeleteBackup(ctx context.Context, id, recoveryKey string) error {
	// Deletes contend with create/restore/upload on the system run lease so a
	// slow operation cannot resurrect or double-forget snapshots.
	lease, admitted, err := s.engine.TryAcquireRun(ctx, backup.SystemAdmissionScope, systemAdmissionID)
	if err != nil {
		return err
	}
	if !admitted {
		return ErrSystemBackupAlreadyRunning
	}
	defer lease.Release()
	run, err := s.backupInternal(ctx, id)
	if err != nil {
		return err
	}
	return s.deleteRunsInternal(ctx, []*SystemBackupRun{run}, recoveryKey, true)
}

// deleteRunsInternal forgets snapshots grouped by repository under the caller's run lease.
func (s *SystemBackupService) deleteRunsInternal(ctx context.Context, runs []*SystemBackupRun, recoveryKey string, includeRemote bool) error {
	var localRuns []*SystemBackupRun
	remoteGroups := make(map[string][]*SystemBackupRun)
	for _, run := range runs {
		if run.LocalSnapshotID != "" {
			localRuns = append(localRuns, run)
		}
		if includeRemote && run.RemoteSnapshotID != "" {
			remoteGroups[run.S3DestinationID] = append(remoteGroups[run.S3DestinationID], run)
		}
	}
	// A run without snapshots (a failed attempt) is just a row; deleting it
	// needs neither the key nor Rustic.
	key := ""
	if len(localRuns) > 0 || len(remoteGroups) > 0 {
		var err error
		if key, err = s.recoveryKeyInternal(ctx, recoveryKey); err != nil {
			return err
		}
	}
	dockerClient, err := s.dockerService.GetClient(ctx)
	if err != nil {
		return err
	}
	deleteErr := s.forgetLocalSnapshotsInternal(ctx, dockerClient, key, localRuns)
	for destinationID, group := range remoteGroups {
		deleteErr = errors.Combine(deleteErr, s.forgetRemoteSnapshotsInternal(ctx, dockerClient, key, destinationID, group))
	}
	for _, run := range runs {
		if run.LocalSnapshotID == "" && run.RemoteSnapshotID == "" {
			if err := s.db.WithContext(ctx).Delete(run).Error; err != nil {
				deleteErr = errors.Combine(deleteErr, fmt.Errorf("failed to delete system backup record: %w", err))
			}
			continue
		}
		if saveErr := s.db.WithContext(ctx).Save(run).Error; saveErr != nil {
			deleteErr = errors.Combine(deleteErr, saveErr)
		}
	}
	return deleteErr
}

func (s *SystemBackupService) forgetLocalSnapshotsInternal(ctx context.Context, dockerClient *client.Client, key string, runs []*SystemBackupRun) error {
	if len(runs) == 0 {
		return nil
	}
	snapshotIDs := make([]string, len(runs))
	for index, run := range runs {
		snapshotIDs[index] = run.LocalSnapshotID
	}
	repository, repoErr := s.localRepositoryInternal(ctx, dockerClient, false)
	if repoErr == nil {
		repoErr = s.engine.ForgetSnapshots(ctx, dockerClient, repository, key, snapshotIDs)
	}
	if repoErr != nil {
		return fmt.Errorf("failed to delete local snapshots: %w", repoErr)
	}
	for _, run := range runs {
		run.LocalSnapshotID = ""
	}
	return nil
}

func (s *SystemBackupService) forgetRemoteSnapshotsInternal(ctx context.Context, dockerClient *client.Client, key, destinationID string, runs []*SystemBackupRun) error {
	snapshotIDs := make([]string, len(runs))
	for index, run := range runs {
		snapshotIDs[index] = run.RemoteSnapshotID
	}
	repository, repoErr := s.remoteRepositoryInternal(ctx, destinationID)
	if repoErr == nil {
		repoErr = s.engine.ForgetSnapshots(ctx, dockerClient, repository, key, snapshotIDs)
	}
	if repoErr != nil {
		return fmt.Errorf("failed to delete S3 snapshots: %w", repoErr)
	}
	for _, run := range runs {
		run.RemoteSnapshotID, run.S3DestinationID = "", ""
	}
	return nil
}

func (s *SystemBackupService) DiscoverRemoteBackups(ctx context.Context, request backuptypes.DiscoverSystemBackupsRequest) (int, error) {
	recoveryKey, err := s.recoveryKeyInternal(ctx, request.RecoveryKey)
	if err != nil {
		return 0, err
	}
	repository, err := s.remoteRepositoryInternal(ctx, request.S3DestinationID)
	if err != nil {
		return 0, err
	}
	dockerClient, err := s.dockerService.GetClient(ctx)
	if err != nil {
		return 0, err
	}
	snapshots, err := s.engine.ListSnapshots(ctx, dockerClient, repository, recoveryKey)
	if err != nil {
		return 0, fmt.Errorf("failed to open system recovery repository: %w", err)
	}
	var knownIDs []string
	if err := s.db.WithContext(ctx).Model(&SystemBackupRun{}).
		Where("s3_destination_id = ? AND remote_snapshot_id <> ''", request.S3DestinationID).
		Pluck("remote_snapshot_id", &knownIDs).Error; err != nil {
		return 0, err
	}
	known := make(map[string]struct{}, len(knownIDs))
	for _, id := range knownIDs {
		known[id] = struct{}{}
	}
	created := 0
	for _, snapshot := range snapshots {
		if strings.TrimSpace(snapshot.ID) == "" {
			continue
		}
		if _, exists := known[snapshot.ID]; exists {
			continue
		}
		createdAt := snapshot.Time
		if createdAt.IsZero() {
			createdAt = time.Now().UTC()
		}
		run := &SystemBackupRun{
			Size: snapshot.Summary.TotalBytesProcessed, CreatedAt: createdAt, Status: SystemBackupStatusSucceeded,
			Trigger: SystemBackupTriggerManual, Destination: backuptypes.SystemBackupDestinationS3,
			RemoteSnapshotID: snapshot.ID, S3DestinationID: request.S3DestinationID,
		}
		run.ID = fmt.Sprintf("remote-%s-%s", request.S3DestinationID, snapshot.ID)
		if err := s.db.WithContext(ctx).Create(run).Error; err != nil {
			return created, fmt.Errorf("failed to save discovered system backup: %w", err)
		}
		created++
	}
	return created, nil
}

func (s *SystemBackupService) UploadBackup(ctx context.Context, id string, request backuptypes.UploadSystemBackupRequest) (*SystemBackupRun, error) {
	lease, admitted, err := s.engine.TryAcquireRun(ctx, backup.SystemAdmissionScope, systemAdmissionID)
	if err != nil {
		return nil, err
	}
	if !admitted {
		return nil, ErrSystemBackupAlreadyRunning
	}
	defer lease.Release()
	run, err := s.backupInternal(ctx, id)
	if err != nil {
		return nil, err
	}
	if run.Status != SystemBackupStatusSucceeded || run.LocalSnapshotID == "" {
		return nil, errors.New("only successful local system backups can be uploaded")
	}
	if run.RemoteSnapshotID != "" {
		return nil, errors.New("system backup has already been uploaded")
	}
	key, err := s.recoveryKeyInternal(ctx, request.RecoveryKey)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(request.S3DestinationID) == "" {
		return nil, errors.New("select an S3 destination for the upload")
	}
	dockerClient, err := s.dockerService.GetClient(ctx)
	if err != nil {
		return nil, err
	}
	localRepository, err := s.localRepositoryInternal(ctx, dockerClient, true)
	if err != nil {
		return nil, err
	}
	remoteRepository, err := s.remoteRepositoryInternal(ctx, request.S3DestinationID)
	if err != nil {
		return nil, err
	}
	snapshot, err := s.engine.Replicate(ctx, dockerClient, localRepository, run.LocalSnapshotID, remoteRepository, key, "arcane-system-recovery")
	if err != nil {
		return nil, fmt.Errorf("failed to upload system recovery snapshot: %w", err)
	}
	run.RemoteSnapshotID, run.S3DestinationID = snapshot.ID, request.S3DestinationID
	run.Destination = backuptypes.SystemBackupDestinationLocalS3
	if err := s.db.WithContext(ctx).Save(run).Error; err != nil {
		return nil, fmt.Errorf("failed to save uploaded system backup: %w", err)
	}
	return run, nil
}

func (s *SystemBackupService) RestoreBackup(ctx context.Context, id, recoveryKey string, user common.User) error {
	run, err := s.backupInternal(ctx, id)
	if err != nil {
		return err
	}
	if run.Status != SystemBackupStatusSucceeded {
		return errors.New("only successful system backups can be restored")
	}
	key, err := s.recoveryKeyInternal(ctx, recoveryKey)
	if err != nil {
		return err
	}
	dockerClient, err := s.dockerService.GetClient(ctx)
	if err != nil {
		return err
	}
	containerID, err := cgroup.CurrentContainerID()
	if err != nil {
		return errors.New("arcane system restore requires Arcane to run in Docker")
	}
	inspectResult, err := libarcane.ContainerInspectWithCompatibility(ctx, dockerClient, containerID, client.ContainerInspectOptions{})
	if err != nil {
		return fmt.Errorf("inspect Arcane container: %w", err)
	}
	current := inspectResult.Container
	// The helper reads the restored manifest and database through /app/data.
	appDataMount := dockerutil.MountForDestination(current.Mounts, "/app/data", "/app/data")
	if appDataMount == nil {
		return errors.New("arcane system restore requires /app/data to be mounted")
	}
	dataDirectory, err := s.dataDirectoryInternal()
	if err != nil {
		return err
	}
	projectsDirectory := s.projectsDirectoryInternal(ctx)
	session := s.snapshotSessionInternal(dockerClient, key)
	locations, setupErr := s.backupSnapshotLocationsInternal(ctx, dockerClient, run)
	snapshot, err := session.firstReadableSnapshotInternal(ctx, locations, setupErr)
	if err != nil {
		return err
	}
	stages, err := restoreStagesInternal(current.Mounts, dataDirectory, projectsDirectory, snapshot.restoreRepositoryInternal(), snapshot.snapshotID, snapshot.layout)
	if err != nil {
		return fmt.Errorf("plan system restore: %w", err)
	}
	// A local safety snapshot is created before the detached helper is allowed to
	// stop Arcane and replace its data.
	safetyBackup, err := s.CreateBackup(ctx, user, SystemBackupTriggerSafety, backuptypes.CreateSystemBackupRequest{
		Destination: backuptypes.SystemBackupDestinationLocal,
		RecoveryKey: key,
	})
	if err != nil {
		return fmt.Errorf("failed to create pre-restore system backup: %w", err)
	}
	localRepository, err := s.localRepositoryInternal(ctx, dockerClient, true)
	if err != nil {
		return err
	}
	safetyLocation := systemBackupSnapshotLocationInternal{name: "safety", destination: backuptypes.SystemBackupDestinationLocal, repository: localRepository, snapshotID: safetyBackup.LocalSnapshotID}
	safetyLayout, err := session.readSnapshotLayoutInternal(ctx, safetyLocation)
	if err != nil {
		return fmt.Errorf("open pre-restore system backup: %w", err)
	}
	rollbackStages, err := restoreStagesInternal(current.Mounts, dataDirectory, projectsDirectory, safetyLocation.restoreRepositoryInternal(), safetyBackup.LocalSnapshotID, safetyLayout)
	if err != nil {
		return fmt.Errorf("plan system restore rollback: %w", err)
	}
	request := recoverytypes.RestoreRequest{
		BackupID: run.ID, ContainerID: current.ID, ContainerImage: current.Config.Image, SnapshotID: snapshot.snapshotID, RecoveryKey: key,
		LocalSnapshotID: run.LocalSnapshotID, RemoteSnapshotID: run.RemoteSnapshotID, S3DestinationID: run.S3DestinationID,
		Size: run.Size, NetworkMode: rusticRestoreNetworkModeInternal(&current),
		SafetyBackup: &recoverytypes.SafetyBackup{
			ID: safetyBackup.ID, LocalSnapshotID: safetyBackup.LocalSnapshotID,
			Size: safetyBackup.Size, CreatedAt: safetyBackup.CreatedAt,
		},
		ProjectsSetting: s.projectsSettingInternal(ctx), ProjectsIncluded: snapshot.layout.projectsIncludedInternal(),
		Stages: stages, RollbackStages: rollbackStages,
	}
	requestData, err := json.Marshal(request)
	if err != nil {
		return fmt.Errorf("encode recovery request: %w", err)
	}
	requestFile := path.Join("/app/data", systemRecoveryRequestName)
	if err := os.WriteFile(requestFile, requestData, 0o600); err != nil {
		return fmt.Errorf("write recovery request: %w", err)
	}
	cleanupRequest := true
	defer func() {
		if cleanupRequest {
			_ = os.Remove(requestFile)
		}
	}()
	containerEnv, runtimeMounts, networkMode, err := system.ResolveUpgraderRuntimeOptions(ctx, s.dockerService.DockerHost(), &current,
		func(ctx context.Context, containerPath string) (string, error) {
			return projects.GetHostPathForContainerPath(ctx, dockerClient, containerPath)
		},
		func() bool { _, err := cgroup.CurrentContainerID(); return err == nil },
		func(ctx context.Context, inspect *containertypes.InspectResponse, dockerHost string) string {
			return dockerutil.SelectDockerHostReachableNetworkMode(ctx, dockerClient, inspect, dockerHost)
		},
	)
	if err != nil {
		return fmt.Errorf("resolve recovery helper runtime: %w", err)
	}
	executablePath, err := os.Executable()
	if err != nil {
		return fmt.Errorf("resolve Arcane executable: %w", err)
	}
	helperExecutable, executableMount, err := recoveryHelperExecutableInternal(current.Mounts, executablePath)
	if err != nil {
		return fmt.Errorf("resolve recovery helper executable: %w", err)
	}
	mounts := append([]mount.Mount{}, runtimeMounts...)
	mounts = append(mounts, *appDataMount)
	if executableMount != nil {
		mounts = append(mounts, *executableMount)
	}
	hostConfig := &containertypes.HostConfig{AutoRemove: true, Mounts: mounts, NetworkMode: networkMode}
	if current.HostConfig != nil {
		hostConfig.SecurityOpt = append([]string{}, current.HostConfig.SecurityOpt...)
		hostConfig.Privileged = current.HostConfig.Privileged
	}
	created, err := dockerClient.ContainerCreate(ctx, client.ContainerCreateOptions{
		Config: &containertypes.Config{
			Image: current.Config.Image, Cmd: []string{helperExecutable, "recovery-restore", "--request", requestFile}, User: "0:0",
			Env:    containerEnv,
			Labels: map[string]string{"com.getarcaneapp.arcane.recovery": "true", "com.getarcaneapp.arcane": "true"},
		},
		HostConfig: hostConfig,
		Name:       fmt.Sprintf("%s-recovery-%d", strings.TrimPrefix(current.Name, "/"), time.Now().Unix()),
	})
	if err != nil {
		return fmt.Errorf("create recovery helper container: %w", err)
	}
	if _, err := dockerClient.ContainerStart(ctx, created.ID, client.ContainerStartOptions{}); err != nil {
		_, _ = dockerClient.ContainerRemove(ctx, created.ID, client.ContainerRemoveOptions{Force: true})
		return fmt.Errorf("start recovery helper container: %w", err)
	}
	cleanupRequest = false
	return nil
}

func (s *SystemBackupService) loadPoliciesInternal(ctx context.Context) ([]SystemBackupPolicy, error) {
	var policies []SystemBackupPolicy
	if err := s.db.WithContext(ctx).Order("created_at ASC").Find(&policies).Error; err != nil {
		return nil, fmt.Errorf("failed to load system backup policies: %w", err)
	}
	return policies, nil
}

func (s *SystemBackupService) loadPolicyInternal(ctx context.Context, policyID string) (*SystemBackupPolicy, error) {
	if strings.TrimSpace(policyID) == "" {
		return nil, nil
	}
	var policy SystemBackupPolicy
	err := s.db.WithContext(ctx).Where("id = ?", policyID).First(&policy).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to load system backup policy: %w", err)
	}
	return &policy, nil
}

// rusticRestoreNetworkModeInternal picks a named network Arcane is attached to
// so the detached restore's Rustic container can still resolve network-local
// S3 endpoints once Arcane is stopped. The default bridge carries no DNS, so
// it (and none) fall back to the default network.
func rusticRestoreNetworkModeInternal(current *containertypes.InspectResponse) string {
	if current == nil || current.NetworkSettings == nil {
		return ""
	}
	for _, name := range slices.Sorted(maps.Keys(current.NetworkSettings.Networks)) {
		if name != "bridge" && name != "none" {
			return name
		}
	}
	return ""
}

func (s *SystemBackupService) GenerateRecoveryKey() (*backuptypes.SystemBackupRecoveryKey, error) {
	recoveryKey, err := backup.GenerateRecoveryKey()
	if err != nil {
		return nil, err
	}
	return &backuptypes.SystemBackupRecoveryKey{RecoveryKey: recoveryKey}, nil
}

func (s *SystemBackupService) SetRecoveryKey(ctx context.Context, recoveryKey string) (*backuptypes.SystemBackupRecoveryKeyStatus, error) {
	if err := backup.ValidateRecoveryKey(recoveryKey); err != nil {
		return nil, err
	}
	// The repositories are encrypted with the key that initialized them and a
	// different key cannot open them, so a rotation would silently break every
	// scheduled backup and keyless restore. Refuse it while backups exist.
	configured, err := s.recoveryKeys.Configured(ctx)
	if err != nil {
		return nil, err
	}
	if configured {
		if current, keyErr := s.recoveryKeyInternal(ctx, ""); keyErr == nil && current != recoveryKey {
			var runs int64
			if err := s.db.WithContext(ctx).Model(&SystemBackupRun{}).Count(&runs).Error; err != nil {
				return nil, err
			}
			if runs > 0 {
				return nil, errors.New("delete the existing system backups before replacing the recovery key; they can only be opened with the current key")
			}
			var volumeBackups int64
			if err := s.db.WithContext(ctx).Model(&volume.VolumeBackup{}).Where("format = ?", volume.VolumeBackupFormatRustic).Count(&volumeBackups).Error; err != nil {
				return nil, err
			}
			if volumeBackups > 0 {
				return nil, errors.New("delete the existing volume backups before replacing the recovery key; they can only be opened with the current key")
			}
		}
	}
	if err := s.recoveryKeys.Set(ctx, recoveryKey); err != nil {
		return nil, err
	}
	if s.volumeService != nil {
		if err := s.volumeService.MigrateRepositoryPasswords(ctx); err != nil {
			slog.WarnContext(ctx, "failed to re-key volume backup repositories to the recovery key", "error", err.Error())
		}
	}
	return &backuptypes.SystemBackupRecoveryKeyStatus{Configured: true}, nil
}

func (s *SystemBackupService) GetPolicies(ctx context.Context) (*backuptypes.SystemBackupPolicyCollection, error) {
	policies, err := s.loadPoliciesInternal(ctx)
	if err != nil {
		return nil, err
	}
	configured, err := s.recoveryKeys.Configured(ctx)
	if err != nil {
		return nil, err
	}
	result := &backuptypes.SystemBackupPolicyCollection{
		Policies: make([]backuptypes.SystemBackupPolicy, 0, len(policies)), RecoveryKeyStored: configured,
	}
	destinations := make(map[string]backuptypes.S3Destination)
	if s.s3Destinations != nil {
		if available, listErr := s.s3Destinations.ListS3DestinationsByID(ctx); listErr == nil {
			destinations = available
		}
	}
	for i := range policies {
		var lastRun *SystemBackupRun
		var run SystemBackupRun
		if runErr := s.db.WithContext(ctx).Where("policy_id = ?", policies[i].ID).Order("created_at DESC").First(&run).Error; runErr == nil {
			run.S3DestinationName = destinations[run.S3DestinationID].Name
			lastRun = &run
		} else if !errors.Is(runErr, gorm.ErrRecordNotFound) {
			return nil, fmt.Errorf("failed to load latest system backup: %w", runErr)
		}
		dto := policies[i].ToDTO(lastRun)
		dto.S3DestinationName = destinations[policies[i].S3DestinationID].Name
		result.Policies = append(result.Policies, dto)
	}
	return result, nil
}

func (s *SystemBackupService) UpdatePolicies(ctx context.Context, updates []backuptypes.UpdateSystemBackupPolicy) (*backuptypes.SystemBackupPolicyCollection, error) {
	configured, err := s.recoveryKeys.Configured(ctx)
	if err != nil {
		return nil, err
	}
	existing, err := s.loadPoliciesInternal(ctx)
	if err != nil {
		return nil, err
	}
	reconcile := backup.PolicyReconciliation[SystemBackupPolicy, backuptypes.UpdateSystemBackupPolicy]{
		Domain:   "system",
		DB:       s.db,
		Existing: existing,
		ID:       func(policy *SystemBackupPolicy) string { return policy.ID },
		UpdateID: func(update backuptypes.UpdateSystemBackupPolicy) string { return update.ID },
		New:      func() SystemBackupPolicy { return SystemBackupPolicy{} },
		Build: func(ctx context.Context, policy *SystemBackupPolicy, update backuptypes.UpdateSystemBackupPolicy) error {
			normalized, normalizeErr := backup.ValidatePolicyUpdate(ctx, "system", update, s.s3Destinations)
			if normalizeErr != nil {
				return normalizeErr
			}
			update = normalized
			if update.Enabled && !configured {
				return errors.New("configure a recovery key before enabling scheduled system backups")
			}
			if update.Enabled && !s.SupportedDatabaseProvider() {
				return errors.New("scheduled system backups require the SQLite database provider")
			}
			policy.Enabled, policy.Schedule, policy.RetentionCount = update.Enabled, update.Schedule, update.RetentionCount
			policy.LocalEnabled, policy.S3Enabled, policy.S3DestinationID = update.LocalEnabled, update.S3Enabled, update.S3DestinationID
			return nil
		},
		Unregister: s.jobs.Unregister,
		Reschedule: s.rescheduleSystemBackupPolicyInternal,
	}
	if err := reconcile.Run(ctx, updates); err != nil {
		return nil, err
	}
	return s.GetPolicies(ctx)
}

// SetScheduler injects the dynamic scheduler and admission gate for per-policy
// system backup jobs. Agent mode leaves them unset.
func (s *SystemBackupService) SetScheduler(ctx context.Context, scheduler schedulertypes.DynamicScheduler, admissionGate *actors.Gate[actors.AdmissionKey]) error {
	return s.jobs.SetScheduler(ctx, scheduler, admissionGate)
}

func (s *SystemBackupService) runScheduledBackupInternal(ctx context.Context, policyID string) (schedulertypes.Outcome, error) {
	policy, loadErr := s.loadPolicyInternal(ctx, policyID)
	if loadErr != nil {
		return schedulertypes.Outcome{}, loadErr
	}
	if policy == nil || !policy.Enabled {
		return schedulertypes.Outcome{Status: schedulertypes.Skipped}, nil
	}
	if previous, ok := jobcontext.Run(ctx); ok {
		outcome := jobcontext.ConfirmedTarget(previous, policy.ID)
		if outcome.Status == schedulertypes.Succeeded {
			if policy.RetentionCount > 0 {
				if err := s.applyRetentionInternal(ctx, policy.ID, policy.RetentionCount, policy.S3Enabled); err != nil {
					outcome.Status = schedulertypes.Partial
					return outcome, err
				}
			}
			return outcome, nil
		}
	}
	remoteDisabled, checkErr := s.disableMissingS3Internal(ctx, policy)
	if checkErr != nil {
		return schedulertypes.Outcome{}, checkErr
	}
	if remoteDisabled && !policy.LocalEnabled {
		return schedulertypes.Outcome{Status: schedulertypes.NeedsAttention, Message: backup.RemoteDisabledMessage}, nil
	}
	var run *SystemBackupRun
	activityID, runErr := activitylib.RunHandlerActivity(ctx, s.activityService, activitylib.HandlerOptions{
		EnvironmentID: "0", Type: activitytypes.TypeResourceAction, ResourceType: "system_backup",
		ResourceID: policy.ID, ResourceName: "Arcane", User: &common.SystemUser,
		Step: "Creating scheduled system backup", Message: "Creating scheduled Arcane system backup",
		SuccessMessage: "Scheduled Arcane system backup created successfully",
		Metadata: database.JSON{"action": "scheduled_system_backup", "policyId": policy.ID, "schedule": policy.Schedule,
			"retentionCount": policy.RetentionCount, "localEnabled": policy.LocalEnabled,
			"s3Enabled": policy.S3Enabled, "s3DestinationId": policy.S3DestinationID},
	}, func(activityCtx context.Context) error {
		var backupErr error
		run, backupErr = s.CreateBackup(activityCtx, common.SystemUser, SystemBackupTriggerScheduled,
			backuptypes.CreateSystemBackupRequest{PolicyID: policy.ID})
		return backupErr
	})
	if errors.Is(runErr, ErrSystemBackupAlreadyRunning) {
		slog.InfoContext(ctx, "Scheduled Arcane system backup skipped; another backup is running", "policyId", policy.ID)
		return schedulertypes.Outcome{Status: schedulertypes.Skipped}, nil
	}
	if runErr != nil {
		slog.ErrorContext(ctx, "Scheduled Arcane system backup failed", "policyId", policy.ID, "error", runErr)
		return schedulertypes.Outcome{ActivityID: activityID}, runErr
	}
	if policy.RetentionCount > 0 {
		if retentionErr := s.applyRetentionInternal(ctx, policy.ID, policy.RetentionCount, policy.S3Enabled); retentionErr != nil {
			slog.ErrorContext(ctx, "System backup retention failed", "policyId", policy.ID, "error", retentionErr)
			return schedulertypes.Outcome{Status: schedulertypes.Partial, ActivityID: activityID, Message: "Backup completed but retention failed"}, retentionErr
		}
	}
	slog.InfoContext(ctx, "Scheduled Arcane system backup completed", "backupId", run.ID, "policyId", policy.ID)
	if remoteDisabled {
		return schedulertypes.Outcome{Status: schedulertypes.Partial, ActivityID: activityID, Message: backup.RemoteDisabledMessage}, nil
	}
	return schedulertypes.Outcome{Status: schedulertypes.Succeeded, ActivityID: activityID}, nil
}

func (s *SystemBackupService) rescheduleSystemBackupPolicyInternal(ctx context.Context, policy *SystemBackupPolicy) {
	if policy == nil {
		return
	}
	if !policy.Enabled {
		s.jobs.Unregister(ctx, policy.ID)
		return
	}
	policyID := policy.ID
	s.jobs.Register(ctx, policyID,
		func(ctx context.Context) string {
			current, err := s.loadPolicyInternal(ctx, policyID)
			if err != nil || current == nil {
				return defaultSystemBackupSchedule
			}
			return current.Schedule
		},
		func(ctx context.Context) (schedulertypes.Outcome, error) {
			return s.runScheduledBackupInternal(ctx, policyID)
		},
		func(ctx context.Context, previous schedulertypes.Run) (schedulertypes.Outcome, error) {
			outcome := jobcontext.ConfirmedTarget(previous, policy.ID)
			if outcome.Status == schedulertypes.Succeeded {
				current, err := s.loadPolicyInternal(ctx, policyID)
				if err != nil {
					return outcome, err
				}
				if current != nil && current.RetentionCount > 0 {
					if err := s.applyRetentionInternal(ctx, policyID, current.RetentionCount, current.S3Enabled); err != nil {
						outcome.Status = schedulertypes.Partial
						outcome.Message = "Backup completed but retention failed"
						return outcome, err
					}
				}
			}
			return outcome, nil
		},
	)
}

func (s *SystemBackupService) RegisterBackupJobOnStartup(ctx context.Context) {
	policies, err := s.loadPoliciesInternal(ctx)
	if err != nil {
		slog.ErrorContext(ctx, "Failed to load Arcane system backup policies", "error", err)
		return
	}
	for i := range policies {
		s.rescheduleSystemBackupPolicyInternal(ctx, &policies[i])
	}
	volumePolicyCount := 0
	volumePolicies, configErr := s.loadSystemVolumeBackupPoliciesInternal()
	if configErr != nil {
		slog.ErrorContext(ctx, "Failed to load system-managed volume backup policies", "error", configErr)
	} else {
		volumePolicyCount = len(volumePolicies.Policies)
		for i := range volumePolicies.Policies {
			s.rescheduleSystemVolumeBackupInternal(ctx, &volumePolicies.Policies[i])
		}
	}
	slog.InfoContext(ctx, "Registered backup schedules", "systemPolicies", len(policies), "volumePolicies", volumePolicyCount)
}

func (s *SystemBackupService) applyRetentionInternal(ctx context.Context, policyID string, keep int, includeRemote bool) error {
	expired, err := backup.ExpiredRunIDs(ctx, s.db, "system_backup_runs", policyID, keep)
	if err != nil || len(expired) == 0 {
		return err
	}
	lease, admitted, err := s.engine.TryAcquireRun(ctx, backup.SystemAdmissionScope, systemAdmissionID)
	if err != nil {
		return err
	}
	if !admitted {
		return ErrSystemBackupAlreadyRunning
	}
	defer lease.Release()
	var runs []*SystemBackupRun
	if err := s.db.WithContext(ctx).Where("id IN ?", expired).Find(&runs).Error; err != nil {
		return err
	}
	return s.deleteRunsInternal(ctx, runs, "", includeRemote)
}

func (s *SystemBackupService) disableMissingS3Internal(ctx context.Context, policy *SystemBackupPolicy) (bool, error) {
	if !policy.S3Enabled {
		return false, nil
	}
	err := backup.CheckScheduledRemote(ctx, s.db, s.s3Destinations, "system_backup_runs", policy.S3DestinationID, "arcane-system-recovery")
	if !errors.Is(err, backup.ErrRemoteRepositoryMissing) {
		return false, nil
	}
	column := "enabled"
	if policy.LocalEnabled {
		column = "s3_enabled"
	}
	result := s.db.WithContext(ctx).Model(&SystemBackupPolicy{}).
		Where("id = ? AND s3_destination_id = ? AND enabled = ? AND s3_enabled = ? AND local_enabled = ?", policy.ID, policy.S3DestinationID, true, true, policy.LocalEnabled).
		Update(column, false)
	if result.Error != nil || result.RowsAffected == 0 {
		return false, result.Error
	}
	if policy.LocalEnabled {
		policy.S3Enabled = false
	} else {
		policy.Enabled = false
	}
	s.rescheduleSystemBackupPolicyInternal(ctx, policy)
	return true, nil
}

const (
	snapshotDataPath     = "/data"
	snapshotProjectsPath = "/projects"
)

// backupSourceLayoutInternal is what one system backup snapshots and where
// each source appears inside the snapshot.
type backupSourceLayoutInternal struct {
	dataDirectory     string
	projectsDirectory string
	databaseName      string
	projectsPath      string
	mounts            []mount.Mount
	sources           []string
}

func (layout backupSourceLayoutInternal) manifestPathInternal() string {
	return filepath.Join(layout.dataDirectory, systemRecoveryManifestName)
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
	case utils.FilePathMatches(projectsDir, data):
		return path.Join(snapshotDataPath, strings.TrimPrefix(projectsDir, data+"/")), false, nil
	case utils.FilePathMatches(data, projectsDir):
		return "", false, fmt.Errorf("the projects directory %s contains Arcane's data directory %s; move it inside or outside the data directory before creating system backups", projectsDir, data)
	default:
		return snapshotProjectsPath, true, nil
	}
}

// projectsSettingInternal returns the effective projectsDirectory setting.
func (s *SystemBackupService) projectsSettingInternal(ctx context.Context) string {
	fallback := ""
	if s.config != nil {
		fallback = s.config.ProjectsDirectory
	}
	if s.settingsService == nil {
		return fallback
	}
	return s.settingsService.GetStringSetting(ctx, "projectsDirectory", fallback)
}

// projectsDirectoryInternal resolves the configured projects directory to the
// container path Arcane reads, dropping any host-path mapping suffix.
func (s *SystemBackupService) projectsDirectoryInternal(ctx context.Context) string {
	return projects.ResolveConfiguredContainerDirectory(s.projectsSettingInternal(ctx), "/app/data/projects")
}

// currentMountsInternal returns the Arcane container's mounts, or inContainer
// false in host development where directories are bound directly.
func currentMountsInternal(ctx context.Context, dockerClient *client.Client) (mounts []containertypes.MountPoint, inContainer bool, err error) {
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
func sourceMountsInternal(mounts []containertypes.MountPoint, inContainer bool, containerPath, target string) ([]mount.Mount, error) {
	if !inContainer {
		return []mount.Mount{{Type: mount.TypeBind, Source: containerPath, Target: target, ReadOnly: true}}, nil
	}
	primary := dockerutil.MountForSubpath(mounts, containerPath, target)
	if primary == nil {
		return nil, fmt.Errorf("%s must be mounted into the Arcane container from a bind or named volume for system backups", containerPath)
	}
	result := append([]mount.Mount{*primary}, dockerutil.NestedMounts(mounts, containerPath, target)...)
	for index := range result {
		result[index].ReadOnly = true
	}
	return result, nil
}

// backupSourceLayoutInternal resolves the sources of one system backup once so
// the manifest and the snapshot describe the same tree.
func (s *SystemBackupService) backupSourceLayoutInternal(ctx context.Context, dockerClient *client.Client) (backupSourceLayoutInternal, error) {
	databaseFile, err := s.databaseFileInternal()
	if err != nil {
		return backupSourceLayoutInternal{}, err
	}
	layout := backupSourceLayoutInternal{
		dataDirectory:     filepath.Dir(databaseFile),
		projectsDirectory: s.projectsDirectoryInternal(ctx),
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
		return backupSourceLayoutInternal{}, errors.WrapIf(err, "resolve Arcane data source")
	}
	if !external {
		return layout, nil
	}
	projectMounts, err := sourceMountsInternal(mounts, inContainer, layout.projectsDirectory, snapshotProjectsPath)
	if err != nil {
		return backupSourceLayoutInternal{}, errors.WrapIf(err, "resolve projects source")
	}
	layout.mounts = append(layout.mounts, projectMounts...)
	layout.sources = append(layout.sources, snapshotProjectsPath)
	return layout, nil
}

const (
	recoveryDataRestoreTarget      = "/restore"
	recoveryProjectsRestoreTarget  = "/restore-projects"
	selectiveProjectsRestoreTarget = "/arcane-projects"
)

var (
	errProjectsOutsideDataInternal = errors.New("the backup-time projects directory is outside Arcane's system backup data")
	errProjectsNotInBackupInternal = errors.New("this system backup does not include the projects directory because it was created before Arcane backed up separately mounted projects; create a new system backup to restore project files")
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
	return protectedSystemBackupPathInternal(path.Join(relative, projectRelative), layout.databaseName)
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
		data, err := session.service.engine.ReadSnapshotTextFile(ctx, session.dockerClient, location.repository, session.recoveryKey, location.snapshotID, path.Join(candidate, systemRecoveryManifestName))
		if err != nil {
			readErr = errors.Combine(readErr, err)
			continue
		}
		manifestData, manifestRoot = data, candidate
		break
	}
	if manifestRoot == "" {
		return snapshotLayoutInternal{}, fmt.Errorf("read %s system recovery manifest: %w", location.name, readErr)
	}
	var manifest recoverytypes.Manifest
	if err := json.Unmarshal([]byte(manifestData), &manifest); err != nil {
		return snapshotLayoutInternal{}, fmt.Errorf("decode %s system recovery manifest: %w", location.name, err)
	}
	layout, err := snapshotLayoutFromManifestInternal(manifest, manifestRoot)
	if err != nil {
		return snapshotLayoutInternal{}, fmt.Errorf("%s system recovery manifest: %w", location.name, err)
	}
	return layout, nil
}

func snapshotLayoutFromManifestInternal(manifest recoverytypes.Manifest, manifestRoot string) (snapshotLayoutInternal, error) {
	switch manifest.FormatVersion {
	case 1:
		return legacySnapshotLayoutInternal(manifest, manifestRoot)
	case recoverytypes.ManifestFormatVersion:
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
		databaseName, err := utils.NormalizeRelativePath(manifest.DatabasePath)
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
	relative, err := utils.NormalizeRelativePath(strings.TrimPrefix(strings.TrimSpace(value), "/"))
	if err != nil {
		return "", err
	}
	return "/" + relative, nil
}

// legacySnapshotLayoutInternal derives a version-1 layout from the manifest
// environment. Projects outside the captured data are reported as omitted.
func legacySnapshotLayoutInternal(manifest recoverytypes.Manifest, manifestRoot string) (snapshotLayoutInternal, error) {
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

func recoveryManifestDatabasePathInternal(manifest recoverytypes.Manifest) (string, error) {
	databaseURL := strings.TrimSpace(manifest.Environment["DATABASE_URL"])
	if databaseURL == "" {
		return "", errors.New("system recovery manifest does not record the database path")
	}
	databasePath, err := utils.SQLitePathFromDSN(databaseURL)
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
func projectsRelativePathFromManifestInternal(manifest recoverytypes.Manifest) (string, error) {
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
	if !utils.FilePathMatches(projectsPath, dataPath) {
		return "", errProjectsOutsideDataInternal
	}
	return strings.TrimPrefix(projectsPath, dataPath+"/"), nil
}

// restoreTargetInternal resolves the writable destination for containerPath
// from the Arcane container's mounts: its enclosing mount at target plus the
// mounts nested beneath it, minus any under exclude that a later stage writes
// through their own mount.
func restoreTargetInternal(mounts []containertypes.MountPoint, containerPath, target, exclude string) (recoverytypes.RestoreTarget, error) {
	enclosing, relative := dockerutil.MountForEnclosingPath(mounts, containerPath, target)
	if enclosing == nil {
		return recoverytypes.RestoreTarget{}, fmt.Errorf("%s must be mounted into the Arcane container from a bind or named volume to restore into it", containerPath)
	}
	if enclosing.ReadOnly {
		return recoverytypes.RestoreTarget{}, fmt.Errorf("%s is mounted read-only into the Arcane container", containerPath)
	}
	destination := path.Join(target, relative)
	candidates := slices.DeleteFunc(slices.Clone(mounts), func(m containertypes.MountPoint) bool {
		return exclude != "" && utils.FilePathMatches(m.Destination, exclude)
	})
	result := recoverytypes.RestoreTarget{Mounts: []mount.Mount{*enclosing}, Path: destination}
	result.Mounts = append(result.Mounts, dockerutil.NestedMounts(candidates, containerPath, destination)...)
	return result, nil
}

func hostRestoreTargetInternal(directory, target string) recoverytypes.RestoreTarget {
	return recoverytypes.RestoreTarget{Mounts: []mount.Mount{{Type: mount.TypeBind, Source: directory, Target: target}}, Path: target}
}

// projectsRestoreDestinationInternal is the current projects directory as a
// helper-container target plus its container path for confined cleanup.
type projectsRestoreDestinationInternal struct {
	directory string
	target    recoverytypes.RestoreTarget
}

func (s *SystemBackupService) projectsRestoreDestinationInternal(ctx context.Context, dockerClient *client.Client) (projectsRestoreDestinationInternal, error) {
	directory := s.projectsDirectoryInternal(ctx)
	mounts, inContainer, err := currentMountsInternal(ctx, dockerClient)
	if err != nil {
		return projectsRestoreDestinationInternal{}, err
	}
	if !inContainer {
		return projectsRestoreDestinationInternal{directory: directory, target: hostRestoreTargetInternal(directory, selectiveProjectsRestoreTarget)}, nil
	}
	target, err := restoreTargetInternal(mounts, directory, selectiveProjectsRestoreTarget, "")
	if err != nil {
		return projectsRestoreDestinationInternal{}, errors.WrapIf(err, "resolve projects directory for restore")
	}
	return projectsRestoreDestinationInternal{directory: directory, target: target}, nil
}

// restoreStagesInternal plans the Rustic restores that put a snapshot's data
// and projects into the current container layout. Projects get their own
// stage unless the snapshot already holds them at their current place under
// the data root.
func restoreStagesInternal(mounts []containertypes.MountPoint, dataDirectory, projectsDirectory string, repository recoverytypes.RestoreRepository, snapshotID string, layout snapshotLayoutInternal) ([]recoverytypes.RestoreStage, error) {
	separate := layout.projectsIncludedInternal() && !layout.projectsCoveredByDataInternal(dataDirectory, projectsDirectory)
	if separate && layout.projectsPath == layout.dataPath {
		return nil, fmt.Errorf("the backup keeps projects in Arcane's data directory; set the projects directory to %s before a full restore, or restore individual project files instead", dataDirectory)
	}
	exclude := ""
	if separate {
		exclude = projectsDirectory
	}
	dataTarget, err := restoreTargetInternal(mounts, dataDirectory, recoveryDataRestoreTarget, exclude)
	if err != nil {
		return nil, err
	}
	stages := make([]recoverytypes.RestoreStage, 0, 2)
	stages = append(stages, recoverytypes.RestoreStage{Repository: repository, SnapshotID: snapshotID, SourcePath: layout.dataPath, Target: dataTarget})
	if !separate {
		return stages, nil
	}
	projectsTarget, err := restoreTargetInternal(mounts, projectsDirectory, recoveryProjectsRestoreTarget, "")
	if err != nil {
		return nil, err
	}
	return append(stages, recoverytypes.RestoreStage{Repository: repository, SnapshotID: snapshotID, SourcePath: layout.projectsPath, Target: projectsTarget}), nil
}
