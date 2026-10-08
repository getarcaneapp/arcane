package backup

import (
	"cmp"
	"context"
	"database/sql"
	"encoding/json/v2"
	"errors"
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
	"uuid"

	activitytypes "github.com/getarcaneapp/arcane/types/v2/activity"
	backuptypes "github.com/getarcaneapp/arcane/types/v2/backup"
	"github.com/getarcaneapp/arcane/types/v2/recovery"
	"github.com/getarcaneapp/arcane/types/v2/scheduler"
	usertypes "github.com/getarcaneapp/arcane/types/v2/user"
	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/api/types/mount"
	"github.com/moby/moby/client"
	"go.getarcane.app/docker"
	"go.getarcane.app/docker/compat"
	"go.getarcane.app/kit/pkg"
	"go.getarcane.app/sys/cgroup"
	"go.getarcane.app/sys/crypto"
	"gorm.io/gorm"

	"github.com/getarcaneapp/arcane/backend/v2/internal/activity"
	"github.com/getarcaneapp/arcane/backend/v2/internal/backup"
	"github.com/getarcaneapp/arcane/backend/v2/internal/common"
	"github.com/getarcaneapp/arcane/backend/v2/internal/config"
	"github.com/getarcaneapp/arcane/backend/v2/internal/database"
	dockerInternal "github.com/getarcaneapp/arcane/backend/v2/internal/docker"
	"github.com/getarcaneapp/arcane/backend/v2/internal/middleware"
	"github.com/getarcaneapp/arcane/backend/v2/internal/s3"
	"github.com/getarcaneapp/arcane/backend/v2/internal/settings"
	"github.com/getarcaneapp/arcane/backend/v2/internal/system/children/backup/children/snapshots"
	"github.com/getarcaneapp/arcane/backend/v2/internal/system/children/backup/children/volumes"
	"github.com/getarcaneapp/arcane/backend/v2/internal/volume"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/authz"
	activitylib "github.com/getarcaneapp/arcane/backend/v2/pkg/libarcane/activity"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/pagination"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/projects"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/scheduler/entityjobs"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/scheduler/jobcontext"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/scheduler/runs"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/utils"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/utils/backupbrowser"
)

const (
	defaultSystemBackupSchedule = "0 0 3 * * *"
	systemRecoveryHelperPath    = "/app/arcane-recovery-helper"
	systemAdmissionID           = "system"
)

var ErrSystemBackupAlreadyRunning = errors.New("an Arcane system backup is already running")

// Store persists system backup runs and policies through their public DTOs;
// the system domain owns the persistence models.
type Store struct {
	CreateRun                 func(ctx context.Context, run *backuptypes.SystemBackupRun) error
	SaveRun                   func(ctx context.Context, run *backuptypes.SystemBackupRun) error
	FailRun                   func(ctx context.Context, id, message string) error
	DeleteRun                 func(ctx context.Context, id string) error
	Run                       func(ctx context.Context, id string) (*backuptypes.SystemBackupRun, error)
	ListRuns                  func(ctx context.Context, params pagination.QueryParams) ([]backuptypes.SystemBackupRun, pagination.Response, error)
	ExpiredRunIDs             func(ctx context.Context, policyID string, keep int) ([]string, error)
	RunsByID                  func(ctx context.Context, ids []string) ([]*backuptypes.SystemBackupRun, error)
	LatestPolicyRun           func(ctx context.Context, policyID string) (*backuptypes.SystemBackupRun, error)
	RemoteSnapshotIDs         func(ctx context.Context, destinationID string) ([]string, error)
	CountRuns                 func(ctx context.Context) (int64, error)
	CountVolumeRepositoryRuns func(ctx context.Context) (int64, error)
	Policies                  func(ctx context.Context) ([]backuptypes.SystemBackupPolicy, error)
	Policy                    func(ctx context.Context, id string) (*backuptypes.SystemBackupPolicy, error)
	ReconcilePolicies         func(
		ctx context.Context,
		updates []backuptypes.UpdateSystemBackupPolicy,
		build func(context.Context, *backuptypes.SystemBackupPolicy, backuptypes.UpdateSystemBackupPolicy) error,
		unregister func(context.Context, string),
		reschedule func(context.Context, *backuptypes.SystemBackupPolicy),
	) error
	DisablePolicyRemote  func(ctx context.Context, policy *backuptypes.SystemBackupPolicy) (bool, error)
	CheckScheduledRemote func(ctx context.Context, destinationID string) error
	ListHistory          func(ctx context.Context, params pagination.QueryParams) ([]backuptypes.HistoryEntry, pagination.Response, error)
}

// RuntimeOptionsResolver determines how a helper container reaches the Docker daemon.
type RuntimeOptionsResolver func(
	ctx context.Context,
	dockerHost string,
	currentContainer *container.InspectResponse,
	discoverHostPath func(context.Context, string) (string, error),
	isRunningInDocker func() bool,
	selectReachableNetwork func(context.Context, *container.InspectResponse, string) string,
) ([]string, []mount.Mount, container.NetworkMode, error)

// Dependencies are the services, persistence, and helper hooks system backups run on.
type Dependencies struct {
	Store                 Store
	DB                    *database.DB
	SQLDB                 func() (*sql.DB, error)
	ActorStore            func() (*gorm.DB, error)
	Docker                *dockerInternal.DockerClientService
	Volumes               *volume.VolumeService
	Engine                *backup.Engine
	S3Destinations        *s3.S3DestinationService
	Activity              *activity.ActivityService
	Settings              *settings.SettingsService
	Config                *config.Config
	RecoveryKeys          *backup.RecoveryKeyStore
	ResolveRuntimeOptions RuntimeOptionsResolver
}

// Service orchestrates Arcane system recovery backups: runs, policies,
// schedules, replication, retention, and restore.
type Service struct {
	store                 Store
	dockerService         *dockerInternal.DockerClientService
	volumeService         *volume.VolumeService
	engine                *backup.Engine
	s3Destinations        *s3.S3DestinationService
	activityService       *activity.ActivityService
	settingsService       *settings.SettingsService
	config                *config.Config
	recoveryKeys          *backup.RecoveryKeyStore
	resolveRuntimeOptions RuntimeOptionsResolver
	jobs                  *entityjobs.Registry
	snapshots             *snapshots.Service
	volumes               *volumes.Service
}

func NewService(deps Dependencies) *Service {
	service := &Service{
		store: deps.Store, dockerService: deps.Docker, volumeService: deps.Volumes, engine: deps.Engine,
		s3Destinations: deps.S3Destinations, activityService: deps.Activity, settingsService: deps.Settings, config: deps.Config,
		recoveryKeys: deps.RecoveryKeys, resolveRuntimeOptions: deps.ResolveRuntimeOptions,
		jobs: entityjobs.New("system-backup:", backup.SystemAdmissionScope),
	}
	service.snapshots = snapshots.NewService(
		deps.Engine,
		deps.SQLDB,
		deps.ActorStore,
		service.localRepositoryInternal,
		service.remoteRepositoryInternal,
		service.databaseFileInternal,
		service.projectsDirectoryInternal,
		service.recoveryEnvironmentInternal,
	)
	service.volumes = volumes.NewService(volumes.Dependencies{
		DB:                deps.DB,
		Engine:            deps.Engine,
		Volumes:           deps.Volumes,
		S3Destinations:    deps.S3Destinations,
		Activity:          deps.Activity,
		Settings:          deps.Settings,
		Jobs:              service.jobs,
		AcquireRun:        service.acquireRunInternal,
		AcquireDurableRun: service.acquireDurableRunInternal,
		AlreadyRunning:    ErrSystemBackupAlreadyRunning,
	})
	if deps.Engine != nil {
		deps.Engine.RegisterRunKind("system", service.executeDurableBackupInternal, service.failDurableBackupInternal)
		deps.Engine.RegisterRunKind("system-volumes", service.volumes.ExecuteDurable, service.volumes.FailDurable)
	}
	return service
}

// acquireRunInternal takes the shared system backup admission lease.
func (s *Service) acquireRunInternal(ctx context.Context) (*runs.Lease, error) {
	lease, admitted, err := s.engine.TryAcquireRun(ctx, backup.SystemAdmissionScope, systemAdmissionID)
	if err != nil {
		return nil, err
	}
	if !admitted {
		return nil, ErrSystemBackupAlreadyRunning
	}
	return lease, nil
}

// acquireDurableRunInternal takes the shared system backup admission lease for a durable run.
func (s *Service) acquireDurableRunInternal(ctx context.Context, runID string) (*runs.Lease, error) {
	lease, admitted, err := s.engine.AcquireDurableRun(ctx, runID, backup.SystemAdmissionScope, systemAdmissionID)
	if err != nil {
		return nil, err
	}
	if !admitted {
		return nil, ErrSystemBackupAlreadyRunning
	}
	return lease, nil
}

// SupportedDatabaseProvider reports whether system recovery works on the
// configured database. Recovery is currently SQLite-only.
func (s *Service) SupportedDatabaseProvider() bool {
	return strings.HasPrefix(s.config.DatabaseURL, "file:")
}

func recoveryHelperExecutableInternal(mounts []container.MountPoint, executablePath string) (string, *mount.Mount, error) {
	executablePath = strings.TrimSpace(executablePath)
	if executablePath == "" {
		return "", nil, errors.New("arcane executable path is empty")
	}
	executableMount := docker.MountForSubpath(mounts, executablePath, systemRecoveryHelperPath)
	if executableMount == nil {
		return executablePath, nil, nil
	}
	if executableMount.Type != mount.TypeBind {
		return "", nil, errors.New("the Arcane executable must come from the image or a bind mount")
	}
	executableMount.ReadOnly = true
	return systemRecoveryHelperPath, executableMount, nil
}

func (s *Service) recoveryEnvironmentInternal(ctx context.Context) map[string]string {
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
func (s *Service) databaseFileInternal() (string, error) {
	databasePath, err := kit.SQLitePathFromDSN(s.config.DatabaseURL)
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
func (s *Service) dataDirectoryInternal() (string, error) {
	databaseFile, err := s.databaseFileInternal()
	if err != nil {
		return "", err
	}
	return filepath.Dir(databaseFile), nil
}

func (s *Service) localRepositoryInternal(ctx context.Context, dockerClient *client.Client, readOnly bool) (backup.Repository, error) {
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

func (s *Service) remoteRepositoryInternal(ctx context.Context, destinationID string) (backup.Repository, error) {
	configuration, err := s.s3Destinations.Configuration(ctx, destinationID)
	if err != nil {
		return backup.Repository{}, errors.New("the selected S3 backup destination is not configured")
	}
	return backup.Repository{
		ID:          "system:s3:" + destinationID,
		Environment: configuration.RusticEnvironment("arcane-system-recovery"),
	}, nil
}

func (s *Service) recoveryKeyInternal(ctx context.Context, supplied string) (string, error) {
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
func (
	s *Service,
) resolveBackupPlanInternal(
	ctx context.Context,
	request backuptypes.CreateSystemBackupRequest,
) (
	localEnabled, s3Enabled bool,
	destinationID string,
	destination backuptypes.SystemBackupDestination,
	err error,
) {
	destinationID = strings.TrimSpace(request.S3DestinationID)
	if request.PolicyID != "" {
		policy, policyErr := s.store.Policy(ctx, request.PolicyID)
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

func (s *Service) CreateBackup(ctx context.Context, user usertypes.Actor, trigger string, request backuptypes.CreateSystemBackupRequest) (*backuptypes.SystemBackupRun, error) {
	lease, err := s.acquireRunInternal(ctx)
	if err != nil {
		return nil, err
	}
	defer lease.Release(ctx)
	return s.createBackupInternal(ctx, trigger, request)
}

func (
	s *Service,
) createStagedSystemRecoverySnapshotInternal(
	ctx context.Context,
	dockerClient *client.Client,
	recoveryKey, backupID, destinationID string,
	localEnabled, s3Enabled bool,
) (
	backup.Snapshot,
	backup.Repository,
	bool,
	error,
) {
	localRepository, err := s.localRepositoryInternal(ctx, dockerClient, false)
	var snapshot backup.Snapshot
	if err == nil {
		snapshot, err = s.snapshots.Create(ctx, dockerClient, localRepository, recoveryKey, backupID)
	}
	if err == nil {
		return snapshot, localRepository, false, nil
	}
	localStagingErr := fmt.Errorf("failed to create local system recovery staging snapshot: %w", err)
	if localEnabled || !s3Enabled {
		return backup.Snapshot{}, backup.Repository{}, false, localStagingErr
	}
	// S3-only backups normally use local repository staging. If local storage is unusable, write directly to
	// S3 so remote backups and restore safety snapshots remain available.
	remoteRepository, repoErr := s.remoteRepositoryInternal(ctx, destinationID)
	if repoErr != nil {
		return backup.Snapshot{}, backup.Repository{}, false, errors.Join(localStagingErr, repoErr)
	}
	remoteSnapshot, snapshotErr := s.snapshots.Create(ctx, dockerClient, remoteRepository, recoveryKey, backupID)
	if snapshotErr != nil {
		return backup.Snapshot{}, backup.Repository{}, false, errors.Join(localStagingErr, fmt.Errorf("failed to create S3 system recovery snapshot: %w", snapshotErr))
	}
	return remoteSnapshot, remoteRepository, true, nil
}

type systemBackupRecoveryInternal struct {
	BackupID     string `json:"backupId"`
	LocalEnabled bool   `json:"localEnabled"`
	S3Enabled    bool   `json:"s3Enabled"`
}

type preparedSystemBackupInternal struct {
	run          *backuptypes.SystemBackupRun
	recoveryKey  string
	localEnabled bool
	s3Enabled    bool
}

func (s *Service) createBackupInternal(ctx context.Context, trigger string, request backuptypes.CreateSystemBackupRequest) (*backuptypes.SystemBackupRun, error) {
	prepared, err := s.prepareBackupInternal(ctx, trigger, request)
	if err != nil {
		return nil, err
	}
	return s.executeBackupInternal(ctx, prepared)
}

func (s *Service) prepareBackupInternal(ctx context.Context, trigger string, request backuptypes.CreateSystemBackupRequest) (*preparedSystemBackupInternal, error) {
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
	run := &backuptypes.SystemBackupRun{
		CreatedAt: time.Now().UTC(), Status: backuptypes.SystemBackupStatusRunning,
		Trigger: trigger, Destination: destination, S3DestinationID: destinationID, PolicyID: request.PolicyID,
	}
	run.ID = "system-" + uuid.New().String()
	if createBackupRunErr := s.store.CreateRun(ctx, run); createBackupRunErr != nil {
		return nil, createBackupRunErr
	}
	checkpoint, err := json.Marshal(systemBackupRecoveryInternal{BackupID: run.ID, LocalEnabled: localEnabled, S3Enabled: s3Enabled})
	if err == nil {
		err = jobcontext.Progress(ctx, scheduler.TargetOutcome{ResourceType: "system_backup", ID: cmp.Or(run.PolicyID, run.ID), Status: scheduler.Running, RecoveryData: checkpoint})
	}
	if err != nil {
		return nil, err
	}
	return &preparedSystemBackupInternal{run: run, recoveryKey: recoveryKey, localEnabled: localEnabled, s3Enabled: s3Enabled}, nil
}

func (s *Service) executeBackupInternal(ctx context.Context, prepared *preparedSystemBackupInternal) (_ *backuptypes.SystemBackupRun, err error) {
	run, recoveryKey := prepared.run, prepared.recoveryKey
	localEnabled, s3Enabled, destinationID := prepared.localEnabled, prepared.s3Enabled, run.S3DestinationID

	defer func() { err = s.completeBackupInternal(ctx, run, err) }()
	defer utils.RecoverToError(&err, "system backup")
	dockerClient, err := s.dockerService.GetClient(ctx)
	if err != nil {
		return run, err
	}
	if progressErr := jobcontext.Progress(ctx, scheduler.TargetOutcome{ResourceType: "backup_destination", ID: run.ID + ":stage", Status: scheduler.Running}); progressErr != nil {
		return run, progressErr
	}
	stagedSnapshot, stagingRepository, directRemote, err := s.createStagedSystemRecoverySnapshotInternal(
		ctx, dockerClient, recoveryKey, run.ID, destinationID, localEnabled, s3Enabled,
	)
	if err != nil {
		return run, err
	}
	if startDestinationProgressErr := jobcontext.Progress(
		ctx,
		scheduler.TargetOutcome{
			ResourceType: "backup_destination",
			ID:           run.ID + ":stage",
			Status:       scheduler.Succeeded,
			Message:      stagedSnapshot.ID,
		},
	); startDestinationProgressErr != nil {
		return run, startDestinationProgressErr
	}
	if directRemote {
		run.RemoteSnapshotID, run.Size = stagedSnapshot.ID, stagedSnapshot.Size
		if saveDisabledBackupErr := s.store.SaveRun(ctx, run); saveDisabledBackupErr != nil {
			return run, saveDisabledBackupErr
		}
		return run, nil
	}
	localRepository := stagingRepository
	// Both destinations use the same staged database copy.
	// The staged snapshot is kept only for local-enabled runs. Forgetting it in
	// a defer covers the failure paths too: a failed replication otherwise
	// orphans one unreferenced snapshot in the staging repository per run.
	defer func() {
		if localEnabled {
			return
		}
		cleanupCtx := context.WithoutCancel(ctx)
		if forgetErr := s.engine.ForgetSnapshots(cleanupCtx, dockerClient, localRepository, recoveryKey, []string{stagedSnapshot.ID}); forgetErr != nil {
			slog.WarnContext(cleanupCtx, "failed to remove staged system recovery snapshot", "snapshotId", stagedSnapshot.ID, "error", forgetErr)
		}
	}()
	if localEnabled {
		run.LocalSnapshotID, run.Size = stagedSnapshot.ID, stagedSnapshot.Size
		if saveLocalBackupErr := s.store.SaveRun(ctx, run); saveLocalBackupErr != nil {
			return run, saveLocalBackupErr
		}
		if localDestinationProgressErr := jobcontext.Progress(
			ctx,
			scheduler.TargetOutcome{
				ResourceType: "backup_destination",
				ID:           run.ID + ":local",
				Status:       scheduler.Succeeded,
				Message:      stagedSnapshot.ID,
			},
		); localDestinationProgressErr != nil {
			return run, localDestinationProgressErr
		}
	}
	if s3Enabled {
		if replicateBackupErr := s.replicateBackupInternal(ctx, dockerClient, prepared, localRepository, stagedSnapshot); replicateBackupErr != nil {
			return run, replicateBackupErr
		}
	}
	return run, nil
}

func (s *Service) completeBackupInternal(ctx context.Context, run *backuptypes.SystemBackupRun, err error) error {
	if err == nil {
		err = ctx.Err()
	}
	if err != nil {
		run.Status, run.Error = backuptypes.SystemBackupStatusFailed, err.Error()
	} else {
		run.Status, run.Error = backuptypes.SystemBackupStatusSucceeded, ""
	}
	if saveErr := s.store.SaveRun(context.WithoutCancel(ctx), run); saveErr != nil {
		err = errors.Join(err, fmt.Errorf("failed to save system backup result: %w", saveErr))
	}
	if err == nil {
		err = jobcontext.Progress(ctx, scheduler.TargetOutcome{ResourceType: "system_backup", ID: cmp.Or(run.PolicyID, run.ID), Status: scheduler.Succeeded})
	}
	return err
}

func (
	s *Service,
) replicateBackupInternal(
	ctx context.Context,
	dockerClient *client.Client,
	prepared *preparedSystemBackupInternal,
	localRepository backup.Repository,
	stagedSnapshot backup.Snapshot,
) error {
	run := prepared.run
	remoteRepository, repoErr := s.remoteRepositoryInternal(ctx, run.S3DestinationID)
	if repoErr != nil {
		return repoErr
	}
	if err := jobcontext.Progress(ctx, scheduler.TargetOutcome{ResourceType: "backup_destination", ID: run.ID + ":remote", Status: scheduler.Running}); err != nil {
		return err
	}
	remoteSnapshot, replicateErr := s.engine.Replicate(
		ctx,
		dockerClient,
		localRepository,
		stagedSnapshot.ID,
		remoteRepository,
		prepared.recoveryKey,
		"arcane-system-recovery",
		backup.RunSnapshotTag(
			run.ID,
		),
	)
	if replicateErr != nil {
		return fmt.Errorf("failed to create S3 system recovery snapshot: %w", replicateErr)
	}
	run.RemoteSnapshotID = remoteSnapshot.ID
	if run.Size == 0 {
		run.Size = remoteSnapshot.Size
	}
	if err := s.store.SaveRun(ctx, run); err != nil {
		return err
	}
	if err := jobcontext.Progress(
		ctx,
		scheduler.TargetOutcome{
			ResourceType: "backup_destination",
			ID:           run.ID + ":remote",
			Status:       scheduler.Succeeded,
			Message:      remoteSnapshot.ID,
		},
	); err != nil {
		return err
	}
	return nil
}

func (s *Service) ListBackups(ctx context.Context, params pagination.QueryParams) ([]backuptypes.SystemBackupRun, pagination.Response, error) {
	result, response, err := s.store.ListRuns(ctx, params)
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
	for i := range result {
		result[i].S3DestinationName = names[result[i].S3DestinationID].Name
		result[i].RemoteAvailable = remoteAvailable(result[i].S3DestinationID, result[i].RemoteSnapshotID)
	}
	return result, response, nil
}

// BrowseBackupFiles returns one page from the backup's logical project tree.
func (s *Service) BrowseBackupFiles(ctx context.Context, id, recoveryKey, requestedPath string, params pagination.QueryParams) ([]backuptypes.BackupFileEntry, pagination.Response, error) {
	listPath, recursive, err := backupbrowser.ListScope(requestedPath, params)
	if err != nil {
		return nil, pagination.Response{}, fmt.Errorf("%w: %w", common.ErrInvalidBackupSelection, err)
	}
	run, err := s.store.Run(ctx, id)
	if err != nil {
		return nil, pagination.Response{}, err
	}
	if run.Status != backuptypes.SystemBackupStatusSucceeded {
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
	return s.snapshots.Browse(ctx, dockerClient, key, *run, listPath, recursive, params)
}

// RestoreBackupFiles restores selected project files into the current projects
// directory after creating a safety snapshot.
func (s *Service) RestoreBackupFiles(ctx context.Context, id string, request backuptypes.RestoreSystemBackupFilesRequest, user usertypes.Actor) error {
	lease, err := s.acquireRunInternal(ctx)
	if err != nil {
		return err
	}
	defer lease.Release(ctx)
	run, err := s.store.Run(ctx, id)
	if err != nil {
		return err
	}
	if run.Status != backuptypes.SystemBackupStatusSucceeded {
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
	return s.snapshots.RestoreFiles(ctx, dockerClient, key, *run, request.RestoreSelection, func(
		ctx context.Context, safetyRequest backuptypes.CreateSystemBackupRequest,
	) (*backuptypes.SystemBackupRun, error) {
		return s.createBackupInternal(ctx, backuptypes.SystemBackupTriggerSafety, safetyRequest)
	})
}

func (s *Service) DeleteBackup(ctx context.Context, id, recoveryKey string) error {
	// Deletes contend with create/restore/upload on the system run lease so a
	// slow operation cannot resurrect or double-forget snapshots.
	lease, err := s.acquireRunInternal(ctx)
	if err != nil {
		return err
	}
	defer lease.Release(ctx)
	run, err := s.store.Run(ctx, id)
	if err != nil {
		return err
	}
	return s.deleteRunsInternal(ctx, []*backuptypes.SystemBackupRun{run}, recoveryKey, true)
}

// deleteRunsInternal forgets snapshots grouped by repository under the caller's run lease.
func (s *Service) deleteRunsInternal(ctx context.Context, localRuns2 []*backuptypes.SystemBackupRun, recoveryKey string, includeRemote bool) error {
	var localRuns []*backuptypes.SystemBackupRun
	remoteGroups := make(map[string][]*backuptypes.SystemBackupRun)
	for _, run := range localRuns2 {
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
		deleteErr = errors.Join(deleteErr, s.forgetRemoteSnapshotsInternal(ctx, dockerClient, key, destinationID, group))
	}
	for _, run := range localRuns2 {
		if run.LocalSnapshotID == "" && run.RemoteSnapshotID == "" {
			if deleteBackupRunErr := s.store.DeleteRun(ctx, run.ID); deleteBackupRunErr != nil {
				deleteErr = errors.Join(deleteErr, fmt.Errorf("failed to delete system backup record: %w", deleteBackupRunErr))
			}
			continue
		}
		switch {
		case run.LocalSnapshotID != "" && run.RemoteSnapshotID != "":
			run.Destination = backuptypes.SystemBackupDestinationLocalS3
		case run.LocalSnapshotID != "":
			run.Destination = backuptypes.SystemBackupDestinationLocal
		default:
			run.Destination = backuptypes.SystemBackupDestinationS3
		}
		if saveErr := s.store.SaveRun(ctx, run); saveErr != nil {
			deleteErr = errors.Join(deleteErr, saveErr)
		}
	}
	return deleteErr
}

// PruneLocalRepository frees the data of deleted local system backups once Rustic's keep-delete window passes.
func (s *Service) PruneLocalRepository(ctx context.Context) error {
	key, err := s.recoveryKeys.Get(ctx)
	if errors.Is(err, backup.ErrRecoveryKeyNotConfigured) {
		return nil
	}
	if err != nil {
		return err
	}
	dockerClient, err := s.dockerService.GetClient(ctx)
	if err != nil {
		return err
	}
	repository, err := s.localRepositoryInternal(ctx, dockerClient, false)
	if err != nil {
		return err
	}
	return s.engine.ForgetSnapshots(ctx, dockerClient, repository, key, nil)
}

func (s *Service) forgetLocalSnapshotsInternal(ctx context.Context, dockerClient *client.Client, key string, localRuns []*backuptypes.SystemBackupRun) error {
	if len(localRuns) == 0 {
		return nil
	}
	snapshotIDs := make([]string, len(localRuns))
	for index, run := range localRuns {
		snapshotIDs[index] = run.LocalSnapshotID
	}
	repository, repoErr := s.localRepositoryInternal(ctx, dockerClient, false)
	if repoErr == nil {
		repoErr = s.engine.ForgetSnapshots(ctx, dockerClient, repository, key, snapshotIDs)
	}
	if repoErr != nil {
		return fmt.Errorf("failed to delete local snapshots: %w", repoErr)
	}
	for _, run := range localRuns {
		run.LocalSnapshotID = ""
	}
	return nil
}

func (s *Service) forgetRemoteSnapshotsInternal(ctx context.Context, dockerClient *client.Client, key, destinationID string, localRuns []*backuptypes.SystemBackupRun) error {
	snapshotIDs := make([]string, len(localRuns))
	for index, run := range localRuns {
		snapshotIDs[index] = run.RemoteSnapshotID
	}
	repository, repoErr := s.remoteRepositoryInternal(ctx, destinationID)
	if repoErr == nil {
		var observation backuptypes.RepositoryObservation
		observation, repoErr = backup.CheckRemoteRepository(ctx, s.s3Destinations, destinationID, "arcane-system-recovery")
		switch {
		case repoErr != nil:
		case !observation.Available:
			// Nothing remains to forget; drop the references so the run can be removed.
			slog.WarnContext(ctx, "S3 repository is missing; dropping remote snapshot references", "destinationId", destinationID, "snapshots", len(snapshotIDs))
		default:
			repoErr = s.engine.ForgetSnapshots(ctx, dockerClient, repository, key, snapshotIDs)
		}
	}
	if repoErr != nil {
		return fmt.Errorf("failed to delete S3 snapshots: %w", repoErr)
	}
	for _, run := range localRuns {
		run.RemoteSnapshotID, run.S3DestinationID = "", ""
	}
	return nil
}

func (s *Service) DiscoverRemoteBackups(ctx context.Context, request backuptypes.DiscoverSystemBackupsRequest) (int, error) {
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
	listedSnapshots, err := s.engine.ListSnapshots(ctx, dockerClient, repository, recoveryKey)
	if err != nil {
		return 0, fmt.Errorf("failed to open system recovery repository: %w", err)
	}
	knownIDs, err := s.store.RemoteSnapshotIDs(ctx, request.S3DestinationID)
	if err != nil {
		return 0, err
	}
	known := make(map[string]struct{}, len(knownIDs))
	for _, id := range knownIDs {
		known[id] = struct{}{}
	}
	created := 0
	for _, snapshot := range listedSnapshots {
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
		run := &backuptypes.SystemBackupRun{
			Size: snapshot.Summary.TotalBytesProcessed, CreatedAt: createdAt, Status: backuptypes.SystemBackupStatusSucceeded,
			Trigger: backuptypes.SystemBackupTriggerManual, Destination: backuptypes.SystemBackupDestinationS3,
			RemoteSnapshotID: snapshot.ID, S3DestinationID: request.S3DestinationID,
		}
		run.ID = fmt.Sprintf("remote-%s-%s", request.S3DestinationID, snapshot.ID)
		if createDiscoveredBackupErr := s.store.CreateRun(ctx, run); createDiscoveredBackupErr != nil {
			return created, fmt.Errorf("failed to save discovered system backup: %w", createDiscoveredBackupErr)
		}
		created++
	}
	return created, nil
}

func (s *Service) UploadBackup(ctx context.Context, id string, request backuptypes.UploadSystemBackupRequest) (*backuptypes.SystemBackupRun, error) {
	lease, err := s.acquireRunInternal(ctx)
	if err != nil {
		return nil, err
	}
	defer lease.Release(ctx)
	run, err := s.store.Run(ctx, id)
	if err != nil {
		return nil, err
	}
	if run.Status != backuptypes.SystemBackupStatusSucceeded || run.LocalSnapshotID == "" {
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
	if saveUploadedBackupErr := s.store.SaveRun(ctx, run); saveUploadedBackupErr != nil {
		return nil, fmt.Errorf("failed to save uploaded system backup: %w", saveUploadedBackupErr)
	}
	return run, nil
}

func (s *Service) RestoreBackup(ctx context.Context, id, recoveryKey string, user usertypes.Actor) error {
	run, err := s.store.Run(ctx, id)
	if err != nil {
		return err
	}
	if run.Status != backuptypes.SystemBackupStatusSucceeded {
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
	inspectResult, err := compat.ContainerInspectWithCompatibility(ctx, dockerClient, containerID, client.ContainerInspectOptions{})
	if err != nil {
		return fmt.Errorf("inspect Arcane container: %w", err)
	}
	current := inspectResult.Container
	// The helper reads the restored manifest and database through /app/data.
	appDataMount := docker.MountForDestination(current.Mounts, "/app/data", "/app/data")
	if appDataMount == nil {
		return errors.New("arcane system restore requires /app/data to be mounted")
	}
	dataDirectory, err := s.dataDirectoryInternal()
	if err != nil {
		return err
	}
	projectsDirectory := s.projectsDirectoryInternal(ctx)
	plan, err := s.snapshots.PlanRestore(ctx, dockerClient, key, *run, current.Mounts, dataDirectory, projectsDirectory)
	if err != nil {
		return err
	}
	// A local safety snapshot is created before the detached helper is allowed to
	// stop Arcane and replace its data.
	safetyBackup, err := s.CreateBackup(ctx, user, backuptypes.SystemBackupTriggerSafety, backuptypes.CreateSystemBackupRequest{
		Destination: backuptypes.SystemBackupDestinationLocal,
		RecoveryKey: key,
	})
	if err != nil {
		return fmt.Errorf("failed to create pre-restore system backup: %w", err)
	}
	rollbackStages, err := s.snapshots.PlanRollback(ctx, dockerClient, key, safetyBackup.LocalSnapshotID, current.Mounts, dataDirectory, projectsDirectory)
	if err != nil {
		return err
	}
	request := recovery.RestoreRequest{
		BackupID: run.ID, ContainerID: current.ID, ContainerImage: current.Config.Image, SnapshotID: plan.SnapshotID, RecoveryKey: key,
		LocalSnapshotID: run.LocalSnapshotID, RemoteSnapshotID: run.RemoteSnapshotID, S3DestinationID: run.S3DestinationID,
		Size: run.Size, NetworkMode: rusticRestoreNetworkModeInternal(&current),
		SafetyBackup: &recovery.SafetyBackup{
			ID: safetyBackup.ID, LocalSnapshotID: safetyBackup.LocalSnapshotID,
			Size: safetyBackup.Size, CreatedAt: safetyBackup.CreatedAt,
		},
		ProjectsSetting: s.projectsSettingInternal(ctx), ProjectsIncluded: plan.ProjectsIncluded,
		Stages: plan.Stages, RollbackStages: rollbackStages,
	}
	requestData, err := json.Marshal(request)
	if err != nil {
		return fmt.Errorf("encode recovery request: %w", err)
	}
	requestFile := path.Join("/app/data", snapshots.RecoveryRequestName)
	if writeFileErr := os.WriteFile(requestFile, requestData, 0o600); writeFileErr != nil {
		return fmt.Errorf("write recovery request: %w", writeFileErr)
	}
	cleanupRequest := true
	defer func() {
		if cleanupRequest {
			_ = os.Remove(requestFile)
		}
	}()
	containerEnv, runtimeMounts, networkMode, err := s.resolveRuntimeOptions(ctx, s.dockerService.DockerHost(), &current,
		func(ctx context.Context, containerPath string) (string, error) {
			return projects.GetHostPathForContainerPath(ctx, dockerClient, containerPath)
		},
		func() bool {
			_, currentContainerIDErr := cgroup.CurrentContainerID()
			return currentContainerIDErr == nil
		},
		func(ctx context.Context, inspect *container.InspectResponse, dockerHost string) string {
			return docker.SelectDockerHostReachableNetworkMode(ctx, dockerClient, inspect, dockerHost)
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
	hostConfig := &container.HostConfig{AutoRemove: true, Mounts: mounts, NetworkMode: networkMode}
	if current.HostConfig != nil {
		hostConfig.SecurityOpt = append([]string{}, current.HostConfig.SecurityOpt...)
		hostConfig.Privileged = current.HostConfig.Privileged
	}
	created, err := dockerClient.ContainerCreate(ctx, client.ContainerCreateOptions{
		Config: &container.Config{
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
	if _, containerStartErr := dockerClient.ContainerStart(ctx, created.ID, client.ContainerStartOptions{}); containerStartErr != nil {
		_, _ = dockerClient.ContainerRemove(ctx, created.ID, client.ContainerRemoveOptions{Force: true})
		return fmt.Errorf("start recovery helper container: %w", containerStartErr)
	}
	cleanupRequest = false
	return nil
}

// rusticRestoreNetworkModeInternal picks a named network Arcane is attached to
// so the detached restore's Rustic container can still resolve network-local
// S3 endpoints once Arcane is stopped. The default bridge carries no DNS, so
// it (and none) fall back to the default network.
func rusticRestoreNetworkModeInternal(current *container.InspectResponse) string {
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

func (s *Service) GenerateRecoveryKey() (*backuptypes.SystemBackupRecoveryKey, error) {
	recoveryKey, err := backup.GenerateRecoveryKey()
	if err != nil {
		return nil, err
	}
	return &backuptypes.SystemBackupRecoveryKey{RecoveryKey: recoveryKey}, nil
}

func (s *Service) SetRecoveryKey(ctx context.Context, recoveryKey string) (*backuptypes.SystemBackupRecoveryKeyStatus, error) {
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
			localRuns, countLocalBackupsErr := s.store.CountRuns(ctx)
			if countLocalBackupsErr != nil {
				return nil, countLocalBackupsErr
			}
			if localRuns > 0 {
				return nil, errors.New("delete the existing system backups before replacing the recovery key; they can only be opened with the current key")
			}
			volumeBackups, countVolumeBackupsErr := s.store.CountVolumeRepositoryRuns(ctx)
			if countVolumeBackupsErr != nil {
				return nil, countVolumeBackupsErr
			}
			if volumeBackups > 0 {
				return nil, errors.New("delete the existing volume backups before replacing the recovery key; they can only be opened with the current key")
			}
		}
	}
	if setErr := s.recoveryKeys.Set(ctx, recoveryKey); setErr != nil {
		return nil, setErr
	}
	if s.volumeService != nil {
		if migrateRepositoryPasswordsErr := s.volumeService.MigrateRepositoryPasswords(ctx); migrateRepositoryPasswordsErr != nil {
			slog.WarnContext(ctx, "failed to re-key volume backup repositories to the recovery key", "error", migrateRepositoryPasswordsErr.Error())
		}
	}
	return &backuptypes.SystemBackupRecoveryKeyStatus{Configured: true}, nil
}

func (s *Service) GetPolicies(ctx context.Context) (*backuptypes.SystemBackupPolicyCollection, error) {
	policies, err := s.store.Policies(ctx)
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
		lastRun, runErr := s.store.LatestPolicyRun(ctx, policies[i].ID)
		if runErr != nil {
			return nil, fmt.Errorf("failed to load latest system backup: %w", runErr)
		}
		if lastRun != nil {
			lastRun.S3DestinationName = destinations[lastRun.S3DestinationID].Name
		}
		dto := policies[i]
		dto.LastRun = lastRun
		dto.S3DestinationName = destinations[dto.S3DestinationID].Name
		result.Policies = append(result.Policies, dto)
	}
	return result, nil
}

func (s *Service) UpdatePolicies(ctx context.Context, updates []backuptypes.UpdateSystemBackupPolicy) (*backuptypes.SystemBackupPolicyCollection, error) {
	configured, err := s.recoveryKeys.Configured(ctx)
	if err != nil {
		return nil, err
	}
	build := func(ctx context.Context, policy *backuptypes.SystemBackupPolicy, update backuptypes.UpdateSystemBackupPolicy) error {
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
	}
	if runErr := s.store.ReconcilePolicies(ctx, updates, build, s.jobs.Unregister, s.rescheduleSystemBackupPolicyInternal); runErr != nil {
		return nil, runErr
	}
	return s.GetPolicies(ctx)
}

// SetScheduler injects the dynamic scheduler and admission gate for per-policy
// system backup jobs. Agent mode leaves them unset.
func (s *Service) SetScheduler(ctx context.Context, dynamicScheduler scheduler.DynamicScheduler, admissionGate *runs.Admission) error {
	return s.jobs.SetScheduler(ctx, dynamicScheduler, admissionGate)
}

func (s *Service) runScheduledBackupInternal(ctx context.Context, policyID string) (scheduler.Outcome, error) {
	policy, loadErr := s.store.Policy(ctx, policyID)
	if loadErr != nil {
		return scheduler.Outcome{}, loadErr
	}
	if policy == nil || !policy.Enabled {
		return scheduler.Outcome{Status: scheduler.Skipped}, nil
	}
	if previous, ok := jobcontext.Run(ctx); ok {
		outcome := jobcontext.ConfirmedTarget(previous, policy.ID)
		if outcome.Status == scheduler.Succeeded {
			if policy.RetentionCount > 0 {
				if err := s.applyRetentionInternal(ctx, policy.ID, policy.RetentionCount, policy.S3Enabled); err != nil {
					outcome.Status = scheduler.Partial
					return outcome, err
				}
			}
			return outcome, nil
		}
	}
	remoteDisabled, checkErr := s.disableMissingS3Internal(ctx, policy)
	if checkErr != nil {
		return scheduler.Outcome{}, checkErr
	}
	if remoteDisabled && !policy.LocalEnabled {
		return scheduler.Outcome{Status: scheduler.NeedsAttention, Message: backup.RemoteDisabledMessage}, nil
	}
	var run *backuptypes.SystemBackupRun
	activityID, runErr := activitylib.RunHandlerActivity(ctx, s.activityService, activitylib.HandlerOptions{
		EnvironmentID: "0", Type: activitytypes.TypeResourceAction, ResourceType: "system_backup",
		ResourceID: policy.ID, ResourceName: "Arcane", User: &usertypes.SystemUser,
		Step: "Creating scheduled system backup", Message: "Creating scheduled Arcane system backup",
		SuccessMessage: "Scheduled Arcane system backup created successfully",
		Metadata: database.JSON{
			"action": "scheduled_system_backup", "policyId": policy.ID, "schedule": policy.Schedule,
			"retentionCount": policy.RetentionCount, "localEnabled": policy.LocalEnabled,
			"s3Enabled": policy.S3Enabled, "s3DestinationId": policy.S3DestinationID,
		},
	}, func(activityCtx context.Context) error {
		var backupErr error
		run, backupErr = s.CreateBackup(activityCtx, usertypes.SystemUser, backuptypes.SystemBackupTriggerScheduled,
			backuptypes.CreateSystemBackupRequest{PolicyID: policy.ID})
		return backupErr
	})
	if errors.Is(runErr, ErrSystemBackupAlreadyRunning) {
		slog.InfoContext(ctx, "Scheduled Arcane system backup skipped; another backup is running", "policyId", policy.ID)
		return scheduler.Outcome{Status: scheduler.Skipped}, nil
	}
	if runErr != nil {
		slog.ErrorContext(ctx, "Scheduled Arcane system backup failed", "policyId", policy.ID, "error", runErr)
		return scheduler.Outcome{ActivityID: activityID}, runErr
	}
	if policy.RetentionCount > 0 {
		if retentionErr := s.applyRetentionInternal(ctx, policy.ID, policy.RetentionCount, policy.S3Enabled); retentionErr != nil {
			slog.ErrorContext(ctx, "System backup retention failed", "policyId", policy.ID, "error", retentionErr)
			return scheduler.Outcome{Status: scheduler.Partial, ActivityID: activityID, Message: "Backup completed but retention failed"}, retentionErr
		}
	}
	slog.InfoContext(ctx, "Scheduled Arcane system backup completed", "backupId", run.ID, "policyId", policy.ID)
	if remoteDisabled {
		return scheduler.Outcome{Status: scheduler.Partial, ActivityID: activityID, Message: backup.RemoteDisabledMessage}, nil
	}
	return scheduler.Outcome{Status: scheduler.Succeeded, ActivityID: activityID}, nil
}

func (s *Service) rescheduleSystemBackupPolicyInternal(ctx context.Context, policy *backuptypes.SystemBackupPolicy) {
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
			current, err := s.store.Policy(ctx, policyID)
			if err != nil || current == nil {
				return defaultSystemBackupSchedule
			}
			return current.Schedule
		},
		func(ctx context.Context) (scheduler.Outcome, error) {
			return s.runScheduledBackupInternal(ctx, policyID)
		},
		func(ctx context.Context, previous scheduler.Run) (scheduler.Outcome, error) {
			outcome, err := s.reconcileBackupInternal(ctx, previous, policyID)
			if err != nil || outcome.Status != scheduler.Succeeded {
				return outcome, err
			}
			current, err := s.store.Policy(ctx, policyID)
			if err != nil {
				return outcome, err
			}
			if current != nil && current.RetentionCount > 0 {
				if applyRetentionErr := s.applyRetentionInternal(ctx, current.ID, current.RetentionCount, current.S3Enabled); applyRetentionErr != nil {
					outcome.Status = scheduler.Partial
					return outcome, applyRetentionErr
				}
			}
			return outcome, nil
		},
	)
}

func (s *Service) RegisterBackupJobOnStartup(ctx context.Context) {
	policies, err := s.store.Policies(ctx)
	if err != nil {
		slog.ErrorContext(ctx, "Failed to load Arcane system backup policies", "error", err)
		return
	}
	for i := range policies {
		s.rescheduleSystemBackupPolicyInternal(ctx, &policies[i])
	}
	volumePolicyCount := s.volumes.RegisterJobsOnStartup(ctx)
	slog.InfoContext(ctx, "Registered backup schedules", "systemPolicies", len(policies), "volumePolicies", volumePolicyCount)
}

func (s *Service) applyRetentionInternal(ctx context.Context, policyID string, keep int, includeRemote bool) error {
	expired, err := s.store.ExpiredRunIDs(ctx, policyID, keep)
	if err != nil || len(expired) == 0 {
		return err
	}
	lease, err := s.acquireRunInternal(ctx)
	if err != nil {
		return err
	}
	defer lease.Release(ctx)
	localRuns, err := s.store.RunsByID(ctx, expired)
	if err != nil {
		return err
	}
	return s.deleteRunsInternal(ctx, localRuns, "", includeRemote)
}

func (s *Service) disableMissingS3Internal(ctx context.Context, policy *backuptypes.SystemBackupPolicy) (bool, error) {
	if !policy.S3Enabled {
		return false, nil
	}
	err := s.store.CheckScheduledRemote(ctx, policy.S3DestinationID)
	if !errors.Is(err, backup.ErrRemoteRepositoryMissing) {
		return false, nil
	}
	disabled, err := s.store.DisablePolicyRemote(ctx, policy)
	if err != nil || !disabled {
		return false, err
	}
	if policy.LocalEnabled {
		policy.S3Enabled = false
	} else {
		policy.Enabled = false
	}
	s.rescheduleSystemBackupPolicyInternal(ctx, policy)
	return true, nil
}

// projectsSettingInternal returns the effective projectsDirectory setting.
func (s *Service) projectsSettingInternal(ctx context.Context) string {
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
func (s *Service) projectsDirectoryInternal(ctx context.Context) string {
	return projects.ResolveConfiguredContainerDirectory(s.projectsSettingInternal(ctx), "/app/data/projects")
}

// StartBackup prepares a manual backup before submitting its snapshot work.
func (s *Service) StartBackup(ctx context.Context, user usertypes.Actor, request backuptypes.CreateSystemBackupRequest) (*backuptypes.SystemBackupRun, error) {
	lease, err := s.acquireRunInternal(ctx)
	if err != nil {
		return nil, err
	}
	prepared, err := s.prepareBackupInternal(ctx, backuptypes.SystemBackupTriggerManual, request)
	if err != nil {
		lease.Release(ctx)
		return nil, err
	}
	activityID, workCtx := activitylib.StartHandlerActivity(ctx, s.activityService, "0", activitytypes.TypeResourceAction, "system_backup", "arcane", "Arcane", &user,
		"Creating system backup", "Creating Arcane system backup", database.JSON{
			"action":          "create_system_backup",
			"backupId":        prepared.run.ID,
			"destination":     prepared.run.Destination,
			"s3DestinationId": prepared.run.S3DestinationID,
			"policyId":        prepared.run.PolicyID,
		}, false)
	finish := func(runErr error) {
		defer lease.Release(ctx)
		if runErr != nil {
			if saveErr := s.store.FailRun(context.WithoutCancel(workCtx), prepared.run.ID, runErr.Error()); saveErr != nil {
				runErr = errors.Join(runErr, fmt.Errorf("save backup failure: %w", saveErr))
			}
		}
		activitylib.CompleteHandlerActivity(workCtx, s.activityService, activityID, "Arcane system backup created successfully", runErr)
	}
	if activityID == "" {
		err = errors.New("failed to create system backup activity")
		finish(err)
		return nil, err
	}
	dto := *prepared.run
	dto.ActivityID = activityID
	keyID, _ := ctx.Value(middleware.ContextKeyApiKeyID).(string)
	encryptedKey, keyErr := crypto.Encrypt(prepared.recoveryKey)
	if keyErr != nil {
		finish(keyErr)
		return nil, keyErr
	}
	payload, err := json.Marshal(
		manualSystemBackupInternal{
			Checkpoint: systemBackupRecoveryInternal{
				BackupID:     prepared.run.ID,
				LocalEnabled: prepared.localEnabled,
				S3Enabled:    prepared.s3Enabled,
			},
			EncryptedKey: encryptedKey,
			ActivityID:   activityID,
			UserID:       user.ID,
		},
	)
	if err == nil {
		err = s.engine.SubmitDurableRun(
			workCtx,
			backuptypes.DurableRunCommand{
				Kind:             "system",
				RunID:            prepared.run.ID,
				ActivityID:       activityID,
				Payload:          payload,
				UserID:           user.ID,
				EnvironmentID:    "0",
				Permission:       authz.PermSystemBackupsManage,
				RequestedWithKey: keyID,
			},
			lease,
		)
	}
	if err != nil {
		finish(err)
		return nil, err
	}

	return &dto, nil
}

func (s *Service) reconcileBackupInternal(ctx context.Context, previous scheduler.Run, policyID string, suppliedKeys ...string) (scheduler.Outcome, error) {
	outcome := jobcontext.ConfirmedTarget(previous, policyID)
	if outcome.Status == scheduler.Succeeded {
		return outcome, nil
	}
	for _, target := range previous.Outcome.Targets {
		if target.ID != policyID || len(target.RecoveryData) == 0 {
			continue
		}
		var checkpoint systemBackupRecoveryInternal
		if err := json.Unmarshal(target.RecoveryData, &checkpoint); err != nil {
			return outcome, err
		}
		run, loadRunErr := s.store.Run(ctx, checkpoint.BackupID)
		if loadRunErr != nil {
			return outcome, loadRunErr
		}
		if run.Status != backuptypes.SystemBackupStatusSucceeded {
			lease, admitted, err := s.engine.AcquireDurableRun(ctx, previous.ID, backup.SystemAdmissionScope, systemAdmissionID)
			if err != nil {
				return outcome, err
			}
			if !admitted {
				return outcome, nil
			}
			defer lease.Release(ctx) //nolint:gocritic // The matching recovery target always returns before the loop advances.
			key := ""
			if len(suppliedKeys) > 0 {
				key = suppliedKeys[0]
			}
			if resumeBackupErr := s.resumeBackupInternal(ctx, previous, run, checkpoint, key); resumeBackupErr != nil {
				return outcome, resumeBackupErr
			}
		}
		target.Status = scheduler.Succeeded
		if err := jobcontext.Progress(ctx, target); err != nil {
			return outcome, err
		}
		return scheduler.Outcome{Status: scheduler.Succeeded, Targets: []scheduler.TargetOutcome{target}}, nil
	}
	return outcome, nil
}

func systemBackupStageEvidenceInternal(previous scheduler.Run, run *backuptypes.SystemBackupRun) (string, bool) {
	stageID, attempted := run.LocalSnapshotID, false
	for _, evidence := range previous.Outcome.Targets {
		if evidence.ID != run.ID+":stage" {
			continue
		}
		attempted = true
		if stageID == "" && evidence.Status == scheduler.Succeeded {
			stageID = evidence.Message
		}
	}
	return stageID, attempted
}

func systemBackupRemoteAttemptedInternal(previous scheduler.Run, runID string) bool {
	for _, evidence := range previous.Outcome.Targets {
		if evidence.ID == runID+":remote" {
			return true
		}
	}
	return false
}

func (
	s *Service,
) observeBackupSnapshotsInternal(
	ctx context.Context,
	dockerClient *client.Client,
	previous scheduler.Run,
	run *backuptypes.SystemBackupRun,
	checkpoint systemBackupRecoveryInternal,
	key string,
) (
	backup.Snapshot,
	backup.Repository,
	backup.Repository,
	error,
) {
	var staged backup.Snapshot
	var remote backup.Repository
	local, err := s.localRepositoryInternal(ctx, dockerClient, false)
	if err != nil && checkpoint.LocalEnabled {
		return staged, local, remote, err
	}
	stageID, stageAttempted := systemBackupStageEvidenceInternal(previous, run)
	if err == nil && (stageAttempted || stageID != "") {
		snapshot, found, findErr := s.engine.FindRunSnapshot(ctx, dockerClient, local, key, run.ID, stageID)
		if findErr == nil && found {
			staged = snapshot
		}
		if checkpoint.LocalEnabled && findErr != nil {
			return staged, local, remote, findErr
		}
	}
	if !checkpoint.S3Enabled {
		return staged, local, remote, nil
	}
	remote, err = s.remoteRepositoryInternal(ctx, run.S3DestinationID)
	if err != nil {
		return staged, local, remote, err
	}
	attempted := (stageAttempted && staged.ID == "") || systemBackupRemoteAttemptedInternal(previous, run.ID)
	if attempted || run.RemoteSnapshotID != "" {
		snapshot, found, findRunSnapshotErr := s.engine.FindRunSnapshot(ctx, dockerClient, remote, key, run.ID, run.RemoteSnapshotID)
		if findRunSnapshotErr != nil {
			return staged, local, remote, findRunSnapshotErr
		}
		if found {
			run.RemoteSnapshotID = snapshot.ID
			if run.Size == 0 {
				run.Size = snapshot.Size
			}
		}
	}
	return staged, local, remote, nil
}

func (
	s *Service,
) finishRecoveredDestinationsInternal(
	ctx context.Context,
	dockerClient *client.Client,
	run *backuptypes.SystemBackupRun,
	checkpoint systemBackupRecoveryInternal,
	key string,
	staged backup.Snapshot,
	local, remote backup.Repository,
) error {
	if checkpoint.LocalEnabled && staged.ID == "" && run.RemoteSnapshotID != "" {
		var err error
		staged, err = s.engine.Replicate(ctx, dockerClient, remote, run.RemoteSnapshotID, local, key, "arcane-system-recovery", backup.RunSnapshotTag(run.ID))
		if err != nil {
			return err
		}
	}
	if staged.ID == "" && run.RemoteSnapshotID == "" {
		_, err := s.executeBackupInternal(ctx, &preparedSystemBackupInternal{run: run, recoveryKey: key, localEnabled: checkpoint.LocalEnabled, s3Enabled: checkpoint.S3Enabled})
		return err
	}
	if checkpoint.LocalEnabled {
		run.LocalSnapshotID, run.Size = staged.ID, staged.Size
	}
	if checkpoint.S3Enabled && run.RemoteSnapshotID == "" {
		if staged.ID == "" {
			return errors.New("System backup has no snapshot to replicate") //nolint:staticcheck // Preserve the existing error message.
		}
		snapshot, err := s.engine.Replicate(ctx, dockerClient, local, staged.ID, remote, key, "arcane-system-recovery", backup.RunSnapshotTag(run.ID))
		if err != nil {
			return err
		}
		run.RemoteSnapshotID = snapshot.ID
	}
	if !checkpoint.LocalEnabled && staged.ID != "" {
		return s.engine.ForgetSnapshots(ctx, dockerClient, local, key, []string{staged.ID})
	}
	return nil
}

func (s *Service) resumeBackupInternal(ctx context.Context, previous scheduler.Run, run *backuptypes.SystemBackupRun, checkpoint systemBackupRecoveryInternal, suppliedKey string) error {
	dockerClient, err := s.dockerService.GetClient(ctx)
	if err != nil {
		return err
	}
	key, err := s.recoveryKeyInternal(ctx, suppliedKey)
	if err != nil {
		return err
	}
	staged, local, remote, err := s.observeBackupSnapshotsInternal(ctx, dockerClient, previous, run, checkpoint, key)
	if err != nil {
		return err
	}
	if finishRecoveredDestinationsErr := s.finishRecoveredDestinationsInternal(ctx, dockerClient, run, checkpoint, key, staged, local, remote); finishRecoveredDestinationsErr != nil {
		return finishRecoveredDestinationsErr
	}
	run.Status, run.Error = backuptypes.SystemBackupStatusSucceeded, ""
	return s.store.SaveRun(ctx, run)
}

type manualSystemBackupInternal struct {
	Checkpoint   systemBackupRecoveryInternal `json:"checkpoint"`
	EncryptedKey string                       `json:"encryptedKey"`
	ActivityID   string                       `json:"activityId"`
	UserID       string                       `json:"userId"`
}

func (s *Service) executeDurableBackupInternal(ctx context.Context, runID string, payload []byte, interrupted bool) (err error) {
	var command manualSystemBackupInternal
	if decodeErr := json.Unmarshal(payload, &command); decodeErr != nil {
		return decodeErr
	}
	defer func() {
		if ctx.Err() == nil {
			if err != nil {
				saveErr := s.store.FailRun(ctx, command.Checkpoint.BackupID, err.Error())
				err = errors.Join(err, saveErr)
			}
			activitylib.CompleteHandlerActivity(ctx, s.activityService, command.ActivityID, "Arcane system backup created successfully", err)
		}
	}()
	run, loadBackupErr := s.store.Run(ctx, command.Checkpoint.BackupID)
	if loadBackupErr != nil {
		return loadBackupErr
	}
	if run.Status == backuptypes.SystemBackupStatusSucceeded {
		return nil
	}
	key, err := crypto.Decrypt(command.EncryptedKey)
	if err != nil {
		return err
	}
	if interrupted {
		previous, _ := jobcontext.Run(ctx)
		outcome, recoveryErr := s.reconcileBackupInternal(ctx, previous, runID, key)
		if recoveryErr != nil {
			return recoveryErr
		}
		if outcome.Status != scheduler.Succeeded {
			return errors.New(outcome.Message)
		}
		return nil
	}
	lease, err := s.acquireDurableRunInternal(ctx, runID)
	if err != nil {
		return err
	}
	defer lease.Release(ctx)
	checkpoint, err := json.Marshal(command.Checkpoint)
	if err != nil {
		return err
	}
	if progressErr := jobcontext.Progress(
		ctx,
		scheduler.TargetOutcome{
			ResourceType: "system_backup",
			ID:           runID,
			Status:       scheduler.Running,
			RecoveryData: checkpoint,
			ActivityID:   command.ActivityID,
		},
	); progressErr != nil {
		return progressErr
	}
	_, err = s.executeBackupInternal(ctx, &preparedSystemBackupInternal{run: run, recoveryKey: key, localEnabled: command.Checkpoint.LocalEnabled, s3Enabled: command.Checkpoint.S3Enabled})
	return err
}

func (s *Service) failDurableBackupInternal(ctx context.Context, _ string, payload []byte, runErr error) error {
	var command manualSystemBackupInternal
	if err := json.Unmarshal(payload, &command); err != nil {
		return err
	}
	err := s.store.FailRun(ctx, command.Checkpoint.BackupID, runErr.Error())
	activitylib.CompleteHandlerActivity(ctx, s.activityService, command.ActivityID, "Arcane system backup created successfully", runErr)
	return err
}

// ListBackupHistory returns a unified, server-paginated view of local backup records.
func (s *Service) ListBackupHistory(ctx context.Context, params pagination.QueryParams) ([]backuptypes.HistoryEntry, pagination.Response, error) {
	history, page, err := s.store.ListHistory(ctx, params)
	if err != nil {
		return nil, pagination.Response{}, fmt.Errorf("list backup history: %w", err)
	}
	decorateHistoryDestinationsInternal(ctx, s.s3Destinations, history)
	systemAvailable := backup.RemoteSnapshotChecker(ctx, s.s3Destinations, "arcane-system-recovery")
	volumeRoot := ""
	if s.settingsService != nil {
		volumeRoot = "arcane-volume-backups/" + s.settingsService.GetSettingsConfig().InstanceID.Value
	}
	volumeAvailable := backup.RemoteSnapshotChecker(ctx, s.s3Destinations, volumeRoot)
	for i := range history {
		check := kit.Ternary(history[i].ResourceType == "volume", volumeAvailable, systemAvailable)
		history[i].RemoteAvailable = check(history[i].S3DestinationID, history[i].RemoteSnapshotID)
	}

	page.GrandTotalItems = page.TotalItems
	return history, page, nil
}

func decorateHistoryDestinationsInternal(ctx context.Context, service *s3.S3DestinationService, history []backuptypes.HistoryEntry) {
	if service == nil {
		return
	}
	destinations, err := service.ListS3DestinationsByID(ctx)
	if err != nil {
		return
	}
	for i := range history {
		history[i].S3DestinationName = destinations[history[i].S3DestinationID].Name
	}
}
