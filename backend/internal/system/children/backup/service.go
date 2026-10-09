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
	"github.com/italypaleale/francis/builtin/workflow"
	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/api/types/mount"
	"github.com/moby/moby/client"
	"go.getarcane.app/docker"
	"go.getarcane.app/docker/compat"
	kit "go.getarcane.app/kit/pkg"
	"go.getarcane.app/sys/cgroup"
	"gorm.io/gorm"

	"github.com/getarcaneapp/arcane/backend/v2/internal/activity"
	"github.com/getarcaneapp/arcane/backend/v2/internal/backup"
	"github.com/getarcaneapp/arcane/backend/v2/internal/common"
	"github.com/getarcaneapp/arcane/backend/v2/internal/config"
	"github.com/getarcaneapp/arcane/backend/v2/internal/database"
	dockerInternal "github.com/getarcaneapp/arcane/backend/v2/internal/docker"
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
	"github.com/getarcaneapp/arcane/backend/v2/pkg/scheduler/flow"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/scheduler/jobcontext"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/scheduler/runs"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/utils"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/utils/backupbrowser"
)

const (
	defaultSystemBackupSchedule = "0 0 3 * * *"
	systemRecoveryHelperPath    = "/app/arcane-recovery-helper"
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
	DisablePolicyRemote  func(ctx context.Context, policy *backuptypes.SystemBackupPolicy, field string) (bool, error)
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
	flow                  *flow.Engine
	// backupWorkflow runs backups StartBackup accepted; policyWorkflow runs scheduled policies.
	backupWorkflow *flow.Workflow
	policyWorkflow *flow.Workflow
}

// manualBackupInput is a manual backup's workflow payload; the run row is created before it starts.
type manualBackupInput struct {
	Checkpoint systemBackupRecovery  `json:"checkpoint"`
	Requester  backuptypes.Requester `json:"requester"`
}

// systemBackupRecovery is the checkpoint a run records so an interrupted backup can resume.
type systemBackupRecovery struct {
	BackupID     string `json:"backupId"`
	LocalEnabled bool   `json:"localEnabled"`
	S3Enabled    bool   `json:"s3Enabled"`
}

type preparedSystemBackup struct {
	run         *backuptypes.SystemBackupRun
	recoveryKey string
	checkpoint  systemBackupRecovery
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
		service.localRepository,
		service.remoteRepository,
		service.databaseFile,
		func(ctx context.Context) string {
			return projects.ResolveConfiguredContainerDirectory(service.projectsSetting(ctx), "/app/data/projects")
		},
		service.recoveryEnvironment,
	)
	service.volumes = volumes.NewService(volumes.Dependencies{
		DB:             deps.DB,
		Engine:         deps.Engine,
		Volumes:        deps.Volumes,
		S3Destinations: deps.S3Destinations,
		Activity:       deps.Activity,
		Settings:       deps.Settings,
		Jobs:           service.jobs,
		AlreadyRunning: ErrSystemBackupAlreadyRunning,
	})
	return service
}

// recoveryEnvironment is the runtime configuration, secrets included, that a restored instance needs.
func (s *Service) recoveryEnvironment(ctx context.Context) map[string]string {
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
		for i := range value.NumField() {
			fieldType, field := value.Type().Field(i), value.Field(i)
			if fieldType.Anonymous {
				visit(field)
				continue
			}
			name := fieldType.Tag.Get("env")
			if name == "" || !field.CanInterface() {
				continue
			}
			if mode, ok := reflect.TypeAssert[os.FileMode](field); ok {
				result[name] = fmt.Sprintf("%#o", uint32(mode))
			} else {
				result[name] = fmt.Sprint(field.Interface())
			}
		}
	}
	visit(reflect.ValueOf(s.config))
	if value := s.projectsSetting(ctx); value != "" {
		result["PROJECTS_DIRECTORY"] = value
	}
	return result
}

// acquireRun takes the shared system backup admission lease.
func (s *Service) acquireRun(ctx context.Context) (*runs.Lease, error) {
	lease, admitted, err := s.engine.TryAcquireRun(ctx, backup.SystemAdmissionScope, backup.SystemAdmissionID)
	if err != nil {
		return nil, err
	}
	if !admitted {
		return nil, ErrSystemBackupAlreadyRunning
	}
	return lease, nil
}

// RegisterWorkflows defines the manual and scheduled backup workflows while the host is still unstarted.
func (s *Service) RegisterWorkflows(engine *flow.Engine) error {
	var err error
	s.flow = engine
	template := activitylib.StartRequest{Type: activitytypes.TypeResourceAction, EnvironmentID: "0", ResourceType: new("system_backup"), ResourceName: new("Arcane")}
	s.backupWorkflow, err = engine.Define(flow.Definition{
		Name:        "system-backup",
		Version:     1,
		Fingerprint: "9b44e4c38b7fcf8951468cc43a51b66c644d1104f1a56fa49bf8c065dc51c046",
		Concurrency: 1,
		Timeout:     24 * time.Hour,
		Activity:    template,
		Labels:      map[string]string{"backup": "Creating system backup"},
		Steps:       []workflow.StepSpec{workflow.Step("backup", engine.Handler(s.runManualBackup), workflow.WithMaxAttempts(1))},
	})
	if err != nil {
		return err
	}
	template.StartedBy, template.Step, template.LatestMessage = &usertypes.SystemUser, "Creating scheduled system backup", "Creating scheduled Arcane system backup"
	s.policyWorkflow, err = engine.Define(flow.Definition{
		Name:        "system-backup-policy",
		Version:     1,
		Fingerprint: "42fa9de20fefac192148a5f6d68132e34ae87948160b5bb0cc5876d506843bcf",
		Concurrency: 1,
		Timeout:     24 * time.Hour,
		Activity:    template,
		Labels:      map[string]string{"backup": "Creating scheduled system backup"},
		Steps: []workflow.StepSpec{workflow.Step("backup", engine.Handler(func(ctx context.Context, t flow.Task) (any, error) {
			var policyID string
			if decodeErr := t.Payload(&policyID); decodeErr != nil {
				return nil, decodeErr
			}
			previous, _ := jobcontext.Run(ctx)
			if slices.ContainsFunc(previous.Outcome.Targets, func(target scheduler.TargetOutcome) bool {
				return target.ID == policyID && len(target.RecoveryData) > 0
			}) {
				return s.resumeScheduledBackup(ctx, previous, policyID)
			}
			return s.runScheduledBackup(ctx, policyID)
		}), workflow.WithMaxAttempts(1))},
	})
	if err != nil {
		return err
	}
	return s.volumes.RegisterWorkflows(engine)
}

// SupportedDatabaseProvider reports whether system recovery works on the
// configured database. Recovery is currently SQLite-only.
func (s *Service) SupportedDatabaseProvider() bool {
	return strings.HasPrefix(s.config.DatabaseURL, "file:")
}

// databaseFile resolves the SQLite database file: under /app/data in
// the container, the local data directory in host development.
func (s *Service) databaseFile() (string, error) {
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

func (s *Service) localRepository(ctx context.Context, dockerClient *client.Client, readOnly bool) (backup.Repository, error) {
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

func (s *Service) remoteRepository(ctx context.Context, destinationID string) (backup.Repository, error) {
	configuration, err := s.s3Destinations.Configuration(ctx, destinationID)
	if err != nil {
		return backup.Repository{}, errors.New("the selected S3 backup destination is not configured")
	}
	return backup.Repository{
		ID:          "system:s3:" + destinationID,
		Environment: configuration.RusticEnvironment("arcane-system-recovery"),
	}, nil
}

func (s *Service) recoveryKey(ctx context.Context, supplied string) (string, error) {
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

func (s *Service) CreateBackup(ctx context.Context, user usertypes.Actor, trigger string, request backuptypes.CreateSystemBackupRequest) (*backuptypes.SystemBackupRun, error) {
	lease, err := s.acquireRun(ctx)
	if err != nil {
		return nil, err
	}
	defer lease.Release(ctx)
	prepared, err := s.prepareBackup(ctx, trigger, request)
	if err != nil {
		return nil, err
	}
	return s.executeBackup(ctx, prepared)
}

// prepareBackup resolves the run's destinations from its policy or destination enum, creates the run row, and records its checkpoint.
func (s *Service) prepareBackup(ctx context.Context, trigger string, request backuptypes.CreateSystemBackupRequest) (*preparedSystemBackup, error) {
	if !s.SupportedDatabaseProvider() {
		return nil, errors.New("arcane system recovery currently requires the SQLite database provider")
	}
	recoveryKey, err := s.recoveryKey(ctx, request.RecoveryKey)
	if err != nil {
		return nil, err
	}
	var checkpoint systemBackupRecovery
	destinationID := strings.TrimSpace(request.S3DestinationID)
	if request.PolicyID != "" {
		policy, policyErr := s.store.Policy(ctx, request.PolicyID)
		if policyErr != nil {
			return nil, policyErr
		}
		if policy == nil {
			return nil, errors.New("system backup policy not found")
		}
		checkpoint.LocalEnabled, checkpoint.S3Enabled, destinationID = policy.LocalEnabled, policy.S3Enabled, policy.S3DestinationID
	} else {
		switch request.Destination {
		case backuptypes.SystemBackupDestinationLocal, "":
			checkpoint.LocalEnabled = true
		case backuptypes.SystemBackupDestinationS3:
			checkpoint.S3Enabled = true
		case backuptypes.SystemBackupDestinationLocalS3:
			checkpoint.LocalEnabled, checkpoint.S3Enabled = true, true
		default:
			return nil, errors.New("unknown system backup destination")
		}
	}
	if !checkpoint.LocalEnabled && !checkpoint.S3Enabled {
		return nil, errors.New("select at least one system backup destination")
	}
	if checkpoint.S3Enabled && destinationID == "" {
		return nil, errors.New("select an S3 destination for the system backup")
	}
	destination := backuptypes.SystemBackupDestinationS3
	switch {
	case checkpoint.LocalEnabled && checkpoint.S3Enabled:
		destination = backuptypes.SystemBackupDestinationLocalS3
	case checkpoint.LocalEnabled:
		destination = backuptypes.SystemBackupDestinationLocal
	}
	run := &backuptypes.SystemBackupRun{
		ID: "system-" + uuid.New().String(), CreatedAt: time.Now().UTC(), Status: backuptypes.SystemBackupStatusRunning,
		Trigger: trigger, Destination: destination, S3DestinationID: destinationID, PolicyID: request.PolicyID,
	}
	if createErr := s.store.CreateRun(ctx, run); createErr != nil {
		return nil, createErr
	}
	checkpoint.BackupID = run.ID
	recoveryData, err := json.Marshal(checkpoint)
	if err == nil {
		err = jobcontext.Progress(ctx, scheduler.TargetOutcome{ResourceType: "system_backup", ID: cmp.Or(run.PolicyID, run.ID), Status: scheduler.Running, RecoveryData: recoveryData})
	}
	if err != nil {
		return nil, err
	}
	return &preparedSystemBackup{run: run, recoveryKey: recoveryKey, checkpoint: checkpoint}, nil
}

// executeBackup stages one snapshot, keeps it locally and/or replicates it to S3, then records the run's result.
func (s *Service) executeBackup(ctx context.Context, prepared *preparedSystemBackup) (_ *backuptypes.SystemBackupRun, err error) {
	run, key, checkpoint := prepared.run, prepared.recoveryKey, prepared.checkpoint
	defer func() {
		if err = cmp.Or(err, ctx.Err()); err != nil {
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
	}()
	defer utils.RecoverToError(&err, "system backup")
	progress := func(stage string, status scheduler.RunStatus, message string) error {
		return jobcontext.Progress(ctx, scheduler.TargetOutcome{ResourceType: "backup_destination", ID: run.ID + ":" + stage, Status: status, Message: message})
	}
	dockerClient, err := s.dockerService.GetClient(ctx)
	if err != nil {
		return run, err
	}
	if progressErr := progress("stage", scheduler.Running, ""); progressErr != nil {
		return run, progressErr
	}
	local, err := s.localRepository(ctx, dockerClient, false)
	var staged backup.Snapshot
	if err == nil {
		staged, err = s.snapshots.Create(ctx, dockerClient, local, key, run.ID)
	}
	directRemote := err != nil
	if directRemote {
		stagingErr := fmt.Errorf("failed to create local system recovery staging snapshot: %w", err)
		if checkpoint.LocalEnabled || !checkpoint.S3Enabled {
			return run, stagingErr
		}
		// S3-only backups write straight to S3 when local staging storage is unusable.
		direct, repoErr := s.remoteRepository(ctx, run.S3DestinationID)
		if repoErr != nil {
			return run, errors.Join(stagingErr, repoErr)
		}
		if staged, err = s.snapshots.Create(ctx, dockerClient, direct, key, run.ID); err != nil {
			return run, errors.Join(stagingErr, fmt.Errorf("failed to create S3 system recovery snapshot: %w", err))
		}
	}
	if progressErr := progress("stage", scheduler.Succeeded, staged.ID); progressErr != nil {
		return run, progressErr
	}
	if directRemote {
		run.RemoteSnapshotID, run.Size = staged.ID, staged.Size
		return run, s.store.SaveRun(ctx, run)
	}
	if checkpoint.LocalEnabled {
		run.LocalSnapshotID, run.Size = staged.ID, staged.Size
		if saveErr := s.store.SaveRun(ctx, run); saveErr != nil {
			return run, saveErr
		}
		if progressErr := progress("local", scheduler.Succeeded, staged.ID); progressErr != nil {
			return run, progressErr
		}
	} else {
		// S3-only runs forget the staged snapshot even when replication fails.
		defer func() {
			cleanupCtx := context.WithoutCancel(ctx)
			if forgetErr := s.engine.ForgetSnapshots(cleanupCtx, dockerClient, local, key, []string{staged.ID}); forgetErr != nil {
				slog.WarnContext(cleanupCtx, "failed to remove staged system recovery snapshot", "snapshotId", staged.ID, "error", forgetErr)
			}
		}()
	}
	if !checkpoint.S3Enabled {
		return run, nil
	}
	remote, err := s.remoteRepository(ctx, run.S3DestinationID)
	if err != nil {
		return run, err
	}
	if progressErr := progress("remote", scheduler.Running, ""); progressErr != nil {
		return run, progressErr
	}
	snapshot, err := s.engine.Replicate(ctx, dockerClient, local, staged.ID, remote, key, "arcane-system-recovery", backup.RunSnapshotTag(run.ID))
	if err != nil {
		return run, fmt.Errorf("failed to create S3 system recovery snapshot: %w", err)
	}
	run.RemoteSnapshotID, run.Size = snapshot.ID, cmp.Or(run.Size, snapshot.Size)
	if saveErr := s.store.SaveRun(ctx, run); saveErr != nil {
		return run, saveErr
	}
	return run, progress("remote", scheduler.Succeeded, snapshot.ID)
}

func (s *Service) ListBackups(ctx context.Context, params pagination.QueryParams) ([]backuptypes.SystemBackupRun, pagination.Response, error) {
	result, response, err := s.store.ListRuns(ctx, params)
	if err != nil {
		return nil, pagination.Response{}, fmt.Errorf("failed to list system backups: %w", err)
	}
	names := s.s3Destinations.DestinationsByID(ctx)
	remoteAvailable := backup.RemoteSnapshotChecker(ctx, s.s3Destinations)
	for i := range result {
		result[i].S3DestinationName = names[result[i].S3DestinationID].Name
		result[i].RemoteAvailable = remoteAvailable(result[i].S3DestinationID, "arcane-system-recovery", result[i].RemoteSnapshotID)
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
	key, err := s.recoveryKey(ctx, recoveryKey)
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
	lease, err := s.acquireRun(ctx)
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
	key, err := s.recoveryKey(ctx, request.RecoveryKey)
	if err != nil {
		return err
	}
	dockerClient, err := s.dockerService.GetClient(ctx)
	if err != nil {
		return err
	}
	safetyBackup := func(ctx context.Context, safetyRequest backuptypes.CreateSystemBackupRequest) (*backuptypes.SystemBackupRun, error) {
		prepared, prepareErr := s.prepareBackup(ctx, backuptypes.SystemBackupTriggerSafety, safetyRequest)
		if prepareErr != nil {
			return nil, prepareErr
		}
		return s.executeBackup(ctx, prepared)
	}
	return s.snapshots.RestoreFiles(ctx, dockerClient, key, *run, request.RestoreSelection, safetyBackup)
}

func (s *Service) DeleteBackup(ctx context.Context, id, recoveryKey string) error {
	// Deletes contend with create/restore/upload on the system run lease so a
	// slow operation cannot resurrect or double-forget snapshots.
	lease, err := s.acquireRun(ctx)
	if err != nil {
		return err
	}
	defer lease.Release(ctx)
	run, err := s.store.Run(ctx, id)
	if err != nil {
		return err
	}
	return s.deleteRuns(ctx, []*backuptypes.SystemBackupRun{run}, recoveryKey, true)
}

// deleteRuns forgets snapshots grouped by repository under the caller's run lease.
func (s *Service) deleteRuns(ctx context.Context, backupRuns []*backuptypes.SystemBackupRun, recoveryKey string, includeRemote bool) error {
	var localRuns []*backuptypes.SystemBackupRun
	var localIDs []string
	remoteGroups := make(map[string][]*backuptypes.SystemBackupRun)
	for _, run := range backupRuns {
		if run.LocalSnapshotID != "" {
			localRuns, localIDs = append(localRuns, run), append(localIDs, run.LocalSnapshotID)
		}
		if includeRemote && run.RemoteSnapshotID != "" {
			remoteGroups[run.S3DestinationID] = append(remoteGroups[run.S3DestinationID], run)
		}
	}
	// A run without snapshots (a failed attempt) is just a row; deleting it needs neither the key nor Rustic.
	key := ""
	if len(localRuns) > 0 || len(remoteGroups) > 0 {
		var err error
		if key, err = s.recoveryKey(ctx, recoveryKey); err != nil {
			return err
		}
	}
	dockerClient, err := s.dockerService.GetClient(ctx)
	if err != nil {
		return err
	}
	var deleteErr error
	if len(localRuns) > 0 {
		repository, repoErr := s.localRepository(ctx, dockerClient, false)
		if repoErr == nil {
			repoErr = s.engine.ForgetSnapshots(ctx, dockerClient, repository, key, localIDs)
		}
		if repoErr != nil {
			deleteErr = fmt.Errorf("failed to delete local snapshots: %w", repoErr)
		} else {
			for _, run := range localRuns {
				run.LocalSnapshotID = ""
			}
		}
	}
	for destinationID, group := range remoteGroups {
		deleteErr = errors.Join(deleteErr, s.forgetRemoteSnapshots(ctx, dockerClient, key, destinationID, group))
	}
	for _, run := range backupRuns {
		if run.LocalSnapshotID == "" && run.RemoteSnapshotID == "" {
			if deleteRunErr := s.store.DeleteRun(ctx, run.ID); deleteRunErr != nil {
				deleteErr = errors.Join(deleteErr, fmt.Errorf("failed to delete system backup record: %w", deleteRunErr))
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

// forgetRemoteSnapshots forgets one S3 destination's snapshots and drops the runs' references to them.
func (s *Service) forgetRemoteSnapshots(ctx context.Context, dockerClient *client.Client, key, destinationID string, group []*backuptypes.SystemBackupRun) error {
	snapshotIDs := make([]string, len(group))
	for index, run := range group {
		snapshotIDs[index] = run.RemoteSnapshotID
	}
	repository, repoErr := s.remoteRepository(ctx, destinationID)
	var observation backuptypes.RepositoryObservation
	if repoErr == nil {
		observation, repoErr = backup.CheckRemoteRepository(ctx, s.s3Destinations, destinationID, "arcane-system-recovery")
	}
	switch {
	case repoErr != nil:
	case !observation.Available:
		// Nothing remains to forget; drop the references so the run can be removed.
		slog.WarnContext(ctx, "S3 repository is missing; dropping remote snapshot references", "destinationId", destinationID, "snapshots", len(snapshotIDs))
	default:
		repoErr = s.engine.ForgetSnapshots(ctx, dockerClient, repository, key, snapshotIDs)
	}
	if repoErr != nil {
		return fmt.Errorf("failed to delete S3 snapshots: %w", repoErr)
	}
	for _, run := range group {
		run.RemoteSnapshotID, run.S3DestinationID = "", ""
	}
	return nil
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
	repository, err := s.localRepository(ctx, dockerClient, false)
	if err != nil {
		return err
	}
	return s.engine.ForgetSnapshots(ctx, dockerClient, repository, key, nil)
}

func (s *Service) DiscoverRemoteBackups(ctx context.Context, request backuptypes.DiscoverSystemBackupsRequest) (int, error) {
	recoveryKey, err := s.recoveryKey(ctx, request.RecoveryKey)
	if err != nil {
		return 0, err
	}
	repository, err := s.remoteRepository(ctx, request.S3DestinationID)
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
	created := 0
	for _, snapshot := range listedSnapshots {
		if strings.TrimSpace(snapshot.ID) == "" || slices.Contains(knownIDs, snapshot.ID) {
			continue
		}
		run := &backuptypes.SystemBackupRun{
			ID: fmt.Sprintf("remote-%s-%s", request.S3DestinationID, snapshot.ID), Size: snapshot.Summary.TotalBytesProcessed,
			CreatedAt: kit.Ternary(snapshot.Time.IsZero(), time.Now().UTC(), snapshot.Time), Status: backuptypes.SystemBackupStatusSucceeded,
			Trigger: backuptypes.SystemBackupTriggerManual, Destination: backuptypes.SystemBackupDestinationS3,
			RemoteSnapshotID: snapshot.ID, S3DestinationID: request.S3DestinationID,
		}
		if createDiscoveredBackupErr := s.store.CreateRun(ctx, run); createDiscoveredBackupErr != nil {
			return created, fmt.Errorf("failed to save discovered system backup: %w", createDiscoveredBackupErr)
		}
		created++
	}
	return created, nil
}

func (s *Service) UploadBackup(ctx context.Context, id string, request backuptypes.UploadSystemBackupRequest) (*backuptypes.SystemBackupRun, error) {
	lease, err := s.acquireRun(ctx)
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
	key, err := s.recoveryKey(ctx, request.RecoveryKey)
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
	localRepository, err := s.localRepository(ctx, dockerClient, true)
	if err != nil {
		return nil, err
	}
	remoteRepository, err := s.remoteRepository(ctx, request.S3DestinationID)
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
	key, err := s.recoveryKey(ctx, recoveryKey)
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
	databaseFile, err := s.databaseFile()
	if err != nil {
		return err
	}
	dataDirectory := filepath.Dir(databaseFile)
	projectsDirectory := projects.ResolveConfiguredContainerDirectory(s.projectsSetting(ctx), "/app/data/projects")
	plan, err := s.snapshots.PlanRestore(ctx, dockerClient, key, *run, current.Mounts, dataDirectory, projectsDirectory)
	if err != nil {
		return err
	}
	// A local safety snapshot is created before the detached helper is allowed to stop Arcane and replace its data.
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
	// Rustic needs a named network to reach network-local S3 once Arcane stops; bridge and none carry no DNS.
	rusticNetwork := ""
	if current.NetworkSettings != nil {
		names := slices.Sorted(maps.Keys(current.NetworkSettings.Networks))
		if index := slices.IndexFunc(names, func(name string) bool { return name != "bridge" && name != "none" }); index >= 0 {
			rusticNetwork = names[index]
		}
	}
	request := recovery.RestoreRequest{
		BackupID: run.ID, ContainerID: current.ID, ContainerImage: current.Config.Image, SnapshotID: plan.SnapshotID, RecoveryKey: key,
		LocalSnapshotID: run.LocalSnapshotID, RemoteSnapshotID: run.RemoteSnapshotID, S3DestinationID: run.S3DestinationID,
		Size: run.Size, NetworkMode: rusticNetwork,
		SafetyBackup: &recovery.SafetyBackup{
			ID: safetyBackup.ID, LocalSnapshotID: safetyBackup.LocalSnapshotID,
			Size: safetyBackup.Size, CreatedAt: safetyBackup.CreatedAt,
		},
		ProjectsSetting: s.projectsSetting(ctx), ProjectsIncluded: plan.ProjectsIncluded,
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
	helperExecutable, mounts := executablePath, append(slices.Clone(runtimeMounts), *appDataMount)
	if executableMount := docker.MountForSubpath(current.Mounts, executablePath, systemRecoveryHelperPath); executableMount != nil {
		if executableMount.Type != mount.TypeBind {
			return errors.New("resolve recovery helper executable: the Arcane executable must come from the image or a bind mount")
		}
		executableMount.ReadOnly = true
		helperExecutable, mounts = systemRecoveryHelperPath, append(mounts, *executableMount)
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
	// Repositories only open with the key that initialized them, so refuse a rotation while backups exist.
	configured, err := s.recoveryKeys.Configured(ctx)
	if err != nil {
		return nil, err
	}
	if configured {
		if current, keyErr := s.recoveryKeys.Get(ctx); keyErr == nil && current != recoveryKey {
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
	destinations := s.s3Destinations.DestinationsByID(ctx)
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
	if runErr := s.store.ReconcilePolicies(ctx, updates, build, s.jobs.Unregister, s.rescheduleSystemBackupPolicy); runErr != nil {
		return nil, runErr
	}
	return s.GetPolicies(ctx)
}

// SetScheduler injects the dynamic scheduler and admission gate for per-policy
// system backup jobs. Agent mode leaves them unset.
func (s *Service) SetScheduler(ctx context.Context, dynamicScheduler scheduler.DynamicScheduler, admissionGate *runs.Admission) error {
	return s.jobs.SetScheduler(ctx, dynamicScheduler, admissionGate)
}

func (s *Service) runScheduledBackup(ctx context.Context, policyID string) (scheduler.Outcome, error) {
	policy, loadErr := s.store.Policy(ctx, policyID)
	if loadErr != nil {
		return scheduler.Outcome{}, loadErr
	}
	if policy == nil || !policy.Enabled {
		return scheduler.Outcome{Status: scheduler.Skipped}, nil
	}
	var remoteErr error
	if policy.S3Enabled {
		remoteErr = s.store.CheckScheduledRemote(ctx, policy.S3DestinationID)
	}
	remoteDisabled, disableErr := backup.DisableMissingRemote(remoteErr, policy.LocalEnabled, &policy.S3Enabled, &policy.Enabled, func(field string) (bool, error) {
		return s.store.DisablePolicyRemote(ctx, policy, field)
	})
	if disableErr != nil {
		return scheduler.Outcome{}, disableErr
	}
	if remoteDisabled {
		s.rescheduleSystemBackupPolicy(ctx, policy)
	}
	if remoteDisabled && !policy.LocalEnabled {
		return scheduler.Outcome{Status: scheduler.NeedsAttention, Message: backup.RemoteDisabledMessage}, nil
	}
	run, runErr := s.CreateBackup(ctx, usertypes.SystemUser, backuptypes.SystemBackupTriggerScheduled, backuptypes.CreateSystemBackupRequest{PolicyID: policy.ID})
	if errors.Is(runErr, ErrSystemBackupAlreadyRunning) {
		slog.InfoContext(ctx, "Scheduled Arcane system backup skipped; another backup is running", "policyId", policy.ID)
		return scheduler.Outcome{Status: scheduler.Skipped, Message: "Skipped: another system backup is running"}, nil
	}
	if runErr != nil {
		slog.ErrorContext(ctx, "Scheduled Arcane system backup failed", "policyId", policy.ID, "error", runErr)
		return scheduler.Outcome{}, runErr
	}
	if policy.RetentionCount > 0 {
		if retentionErr := s.applyRetention(ctx, policy.ID, policy.RetentionCount, policy.S3Enabled); retentionErr != nil {
			slog.ErrorContext(ctx, "System backup retention failed", "policyId", policy.ID, "error", retentionErr)
			return scheduler.Outcome{Status: scheduler.Partial, Message: "Backup completed but retention failed"}, retentionErr
		}
	}
	slog.InfoContext(ctx, "Scheduled Arcane system backup completed", "backupId", run.ID, "policyId", policy.ID)
	if remoteDisabled {
		return scheduler.Outcome{Status: scheduler.Partial, Message: backup.RemoteDisabledMessage}, nil
	}
	return scheduler.Outcome{Status: scheduler.Succeeded, Message: "Scheduled Arcane system backup created successfully"}, nil
}

// resumeScheduledBackup finishes the backup an interrupted scheduled run recorded, then applies retention.
func (s *Service) resumeScheduledBackup(ctx context.Context, previous scheduler.Run, policyID string) (scheduler.Outcome, error) {
	outcome, err := s.reconcileBackup(ctx, previous, policyID, "")
	if err != nil || outcome.Status != scheduler.Succeeded {
		return outcome, err
	}
	current, err := s.store.Policy(ctx, policyID)
	if err != nil {
		return outcome, err
	}
	if current != nil && current.RetentionCount > 0 {
		if applyRetentionErr := s.applyRetention(ctx, current.ID, current.RetentionCount, current.S3Enabled); applyRetentionErr != nil {
			outcome.Status = scheduler.Partial
			return outcome, applyRetentionErr
		}
	}
	return outcome, nil
}

func (s *Service) rescheduleSystemBackupPolicy(ctx context.Context, policy *backuptypes.SystemBackupPolicy) {
	if policy == nil {
		return
	}
	if !policy.Enabled {
		s.jobs.Unregister(ctx, policy.ID)
		return
	}
	policyID := policy.ID
	s.jobs.Add(ctx, &flow.Job{
		Engine:   s.flow,
		Workflow: s.policyWorkflow,
		JobName:  s.jobs.JobName(policyID),
		Payload:  policyID,
		Activity: activitylib.StartRequest{
			ResourceID: new(policyID),
			Metadata: database.JSON{
				"action": "scheduled_system_backup", "policyId": policyID, "schedule": policy.Schedule,
				"retentionCount": policy.RetentionCount, "localEnabled": policy.LocalEnabled,
				"s3Enabled": policy.S3Enabled, "s3DestinationId": policy.S3DestinationID,
			},
		},
		ScheduleFn: func(ctx context.Context) string {
			current, err := s.store.Policy(ctx, policyID)
			if err != nil || current == nil {
				return defaultSystemBackupSchedule
			}
			return current.Schedule
		},
		FallbackFn: func(ctx context.Context, previous scheduler.Run) (scheduler.Outcome, error) {
			return s.resumeScheduledBackup(ctx, previous, policyID)
		},
	})
}

func (s *Service) RegisterBackupJobOnStartup(ctx context.Context) {
	policies, err := s.store.Policies(ctx)
	if err != nil {
		slog.ErrorContext(ctx, "Failed to load Arcane system backup policies", "error", err)
		return
	}
	for i := range policies {
		s.rescheduleSystemBackupPolicy(ctx, &policies[i])
	}
	volumePolicyCount := s.volumes.RegisterJobsOnStartup(ctx)
	slog.InfoContext(ctx, "Registered backup schedules", "systemPolicies", len(policies), "volumePolicies", volumePolicyCount)
}

func (s *Service) applyRetention(ctx context.Context, policyID string, keep int, includeRemote bool) error {
	expired, err := s.store.ExpiredRunIDs(ctx, policyID, keep)
	if err != nil || len(expired) == 0 {
		return err
	}
	lease, err := s.acquireRun(ctx)
	if err != nil {
		return err
	}
	defer lease.Release(ctx)
	expiredRuns, err := s.store.RunsByID(ctx, expired)
	if err != nil {
		return err
	}
	return s.deleteRuns(ctx, expiredRuns, "", includeRemote)
}

// projectsSetting returns the effective projectsDirectory setting.
func (s *Service) projectsSetting(ctx context.Context) string {
	fallback := ""
	if s.config != nil {
		fallback = s.config.ProjectsDirectory
	}
	if s.settingsService == nil {
		return fallback
	}
	return s.settingsService.GetStringSetting(ctx, "projectsDirectory", fallback)
}

// StartBackup prepares a manual backup, then submits its workflow.
func (s *Service) StartBackup(ctx context.Context, user usertypes.Actor, request backuptypes.CreateSystemBackupRequest) (*backuptypes.SystemBackupRun, error) {
	requester := backup.NewRequester(ctx, user, "0", authz.PermSystemBackupsManage)
	if err := s.engine.Authorize(ctx, requester); err != nil {
		return nil, err
	}
	lease, err := s.acquireRun(ctx)
	if err != nil {
		return nil, err
	}
	prepared, err := s.prepareBackup(ctx, backuptypes.SystemBackupTriggerManual, request)
	if err != nil {
		lease.Release(ctx)
		return nil, err
	}
	run := prepared.run
	release := s.engine.Hold(backup.SystemAdmissionScope, backup.SystemAdmissionID, run.ID, lease)
	// An entered key is held encrypted, outside the workflow payload, so a restart can still resume the backup.
	if request.RecoveryKey != "" {
		if holdErr := s.recoveryKeys.HoldRunKey(ctx, run.ID, prepared.recoveryKey); holdErr != nil {
			release(ctx)
			return nil, errors.Join(holdErr, s.store.FailRun(context.WithoutCancel(ctx), run.ID, holdErr.Error()))
		}
	}
	input := manualBackupInput{Checkpoint: prepared.checkpoint, Requester: requester}
	activityID, err := s.flow.Submit(ctx, s.backupWorkflow, input, activitylib.StartRequest{
		ResourceID: new("arcane"), StartedBy: &user, Step: "Creating system backup", LatestMessage: "Creating Arcane system backup",
		Metadata: database.JSON{"action": "create_system_backup", "backupId": run.ID, "destination": run.Destination, "s3DestinationId": run.S3DestinationID, "policyId": run.PolicyID},
	}, backup.AcceptedTarget(run.ID))
	if err != nil {
		release(ctx)
		return nil, errors.Join(err, s.recoveryKeys.ReleaseRunKey(context.WithoutCancel(ctx), run.ID), s.store.FailRun(context.WithoutCancel(ctx), run.ID, err.Error()))
	}
	// A backup canceled before its task runs never takes the parked lease or uses the held key.
	go func() {
		background := context.WithoutCancel(ctx)
		s.watchBackup(background, run.ID, activityID)
		release(background)
	}()
	dto := *run
	dto.ActivityID = activityID
	return &dto, nil
}

// WatchAcceptedBackups restores, at startup, the watch over manual backups whose saved workflows resume.
func (s *Service) WatchAcceptedBackups(ctx context.Context) error {
	standalone, err := s.flow.Standalone(ctx)
	if err != nil {
		return err
	}
	for _, run := range standalone {
		if backupID := backup.AcceptedBackupID(run); backupID != "" && run.JobID == s.backupWorkflow.Francis().Name() {
			go s.watchBackup(context.WithoutCancel(ctx), backupID, run.ID)
		}
	}
	return nil
}

// watchBackup releases the run's held key once its workflow ends, and fails the run's row when the workflow
// ended without the task completing it, such as a cancel before the task started.
func (s *Service) watchBackup(ctx context.Context, runID, instanceID string) {
	outcome, err := s.flow.Wait(ctx, s.backupWorkflow, instanceID)
	if err != nil {
		return
	}
	if releaseErr := s.recoveryKeys.ReleaseRunKey(ctx, runID); releaseErr != nil {
		slog.WarnContext(ctx, "Failed to release system backup recovery key", "runId", runID, "error", releaseErr)
	}
	if outcome.Status == scheduler.Succeeded {
		return
	}
	if current, loadErr := s.store.Run(ctx, runID); loadErr == nil && current.Status == backuptypes.SystemBackupStatusRunning {
		if failErr := s.store.FailRun(ctx, runID, cmp.Or(outcome.Message, "Backup did not run")); failErr != nil {
			slog.WarnContext(ctx, "Failed to close a system backup whose workflow ended early", "runId", runID, "error", failErr)
		}
	}
}

// reconcileBackup finishes the backup checkpointed under targetID from the snapshots its interrupted run left behind.
func (s *Service) reconcileBackup(ctx context.Context, previous scheduler.Run, targetID, suppliedKey string) (scheduler.Outcome, error) {
	outcome := jobcontext.ConfirmedTarget(previous, targetID)
	index := slices.IndexFunc(previous.Outcome.Targets, func(target scheduler.TargetOutcome) bool {
		return target.ID == targetID && len(target.RecoveryData) > 0
	})
	if outcome.Status == scheduler.Succeeded || index < 0 {
		return outcome, nil
	}
	target := previous.Outcome.Targets[index]
	var checkpoint systemBackupRecovery
	if err := json.Unmarshal(target.RecoveryData, &checkpoint); err != nil {
		return outcome, err
	}
	confirm := func() (scheduler.Outcome, error) {
		target.Status = scheduler.Succeeded
		if progressErr := jobcontext.Progress(ctx, target); progressErr != nil {
			return outcome, progressErr
		}
		return scheduler.Outcome{Status: scheduler.Succeeded, Targets: []scheduler.TargetOutcome{target}}, nil
	}
	run, err := s.store.Run(ctx, checkpoint.BackupID)
	if err != nil {
		return outcome, err
	}
	if run.Status == backuptypes.SystemBackupStatusSucceeded {
		return confirm()
	}
	lease, admitted, err := s.engine.AcquireRun(ctx, backup.SystemAdmissionScope, backup.SystemAdmissionID, "")
	if err != nil || !admitted {
		return outcome, err
	}
	defer lease.Release(ctx)
	dockerClient, err := s.dockerService.GetClient(ctx)
	if err != nil {
		return outcome, err
	}
	key, err := s.recoveryKey(ctx, suppliedKey)
	if err != nil {
		return outcome, err
	}
	staged, local, remote, err := s.observeBackupSnapshots(ctx, dockerClient, previous, run, checkpoint, key)
	if err != nil {
		return outcome, err
	}
	if checkpoint.LocalEnabled && staged.ID == "" && run.RemoteSnapshotID != "" {
		if staged, err = s.engine.Replicate(ctx, dockerClient, remote, run.RemoteSnapshotID, local, key, "arcane-system-recovery", backup.RunSnapshotTag(run.ID)); err != nil {
			return outcome, err
		}
	}
	if staged.ID == "" && run.RemoteSnapshotID == "" {
		// Nothing survived the interruption, so the backup runs again.
		if _, err = s.executeBackup(ctx, &preparedSystemBackup{run: run, recoveryKey: key, checkpoint: checkpoint}); err != nil {
			return outcome, err
		}
		return confirm()
	}
	if checkpoint.LocalEnabled {
		run.LocalSnapshotID, run.Size = staged.ID, staged.Size
	}
	// A missing remote snapshot implies a staged one; the rerun above handled neither surviving.
	if checkpoint.S3Enabled && run.RemoteSnapshotID == "" {
		snapshot, replicateErr := s.engine.Replicate(ctx, dockerClient, local, staged.ID, remote, key, "arcane-system-recovery", backup.RunSnapshotTag(run.ID))
		if replicateErr != nil {
			return outcome, replicateErr
		}
		run.RemoteSnapshotID = snapshot.ID
	}
	if !checkpoint.LocalEnabled && staged.ID != "" {
		if forgetErr := s.engine.ForgetSnapshots(ctx, dockerClient, local, key, []string{staged.ID}); forgetErr != nil {
			return outcome, forgetErr
		}
	}
	run.Status, run.Error = backuptypes.SystemBackupStatusSucceeded, ""
	if saveErr := s.store.SaveRun(ctx, run); saveErr != nil {
		return outcome, saveErr
	}
	return confirm()
}

// observeBackupSnapshots finds the snapshots an interrupted run already wrote and records any remote one on run.
func (s *Service) observeBackupSnapshots(
	ctx context.Context, dockerClient *client.Client, previous scheduler.Run, run *backuptypes.SystemBackupRun, checkpoint systemBackupRecovery, key string,
) (backup.Snapshot, backup.Repository, backup.Repository, error) {
	var staged backup.Snapshot
	var remote backup.Repository
	local, err := s.localRepository(ctx, dockerClient, false)
	if err != nil && checkpoint.LocalEnabled {
		return staged, local, remote, err
	}
	targets := previous.Outcome.Targets
	stageIndex := slices.IndexFunc(targets, func(target scheduler.TargetOutcome) bool { return target.ID == run.ID+":stage" })
	stageID := run.LocalSnapshotID
	if stageIndex >= 0 && stageID == "" && targets[stageIndex].Status == scheduler.Succeeded {
		stageID = targets[stageIndex].Message
	}
	if err == nil && (stageIndex >= 0 || stageID != "") {
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
	remote, err = s.remoteRepository(ctx, run.S3DestinationID)
	if err != nil {
		return staged, local, remote, err
	}
	remoteAttempted := slices.ContainsFunc(targets, func(target scheduler.TargetOutcome) bool { return target.ID == run.ID+":remote" })
	if (stageIndex >= 0 && staged.ID == "") || remoteAttempted || run.RemoteSnapshotID != "" {
		snapshot, found, findErr := s.engine.FindRunSnapshot(ctx, dockerClient, remote, key, run.ID, run.RemoteSnapshotID)
		if findErr != nil {
			return staged, local, remote, findErr
		}
		if found {
			run.RemoteSnapshotID, run.Size = snapshot.ID, cmp.Or(run.Size, snapshot.Size)
		}
	}
	return staged, local, remote, nil
}

// runManualBackup is the system-backup step. A delivery that finds the checkpoint it recorded
// resumes that backup; otherwise it takes over the admission StartBackup won and runs it.
func (s *Service) runManualBackup(ctx context.Context, t flow.Task) (_ any, err error) {
	var input manualBackupInput
	if decodeErr := t.Payload(&input); decodeErr != nil {
		return nil, decodeErr
	}
	runID := input.Checkpoint.BackupID
	defer func() {
		// Shutdown leaves the run, and any key held for it, to the redelivered task.
		if ctx.Err() != nil {
			return
		}
		if err != nil {
			err = errors.Join(err, s.store.FailRun(ctx, runID, err.Error()))
		}
		err = errors.Join(err, s.recoveryKeys.ReleaseRunKey(ctx, runID))
	}()
	previous, _ := jobcontext.Run(ctx)
	resuming := slices.ContainsFunc(previous.Outcome.Targets, func(target scheduler.TargetOutcome) bool {
		return target.ID == runID && len(target.RecoveryData) > 0
	})
	if !resuming {
		lease, admitted, acquireErr := s.engine.AcquireRun(ctx, backup.SystemAdmissionScope, backup.SystemAdmissionID, runID)
		if acquireErr != nil {
			return nil, acquireErr
		}
		if !admitted {
			return nil, ErrSystemBackupAlreadyRunning
		}
		defer lease.Release(ctx)
	}
	if authorizeErr := s.engine.Authorize(ctx, input.Requester); authorizeErr != nil {
		return nil, authorizeErr
	}
	succeeded := scheduler.Outcome{Status: scheduler.Succeeded, Message: "Arcane system backup created successfully"}
	run, err := s.store.Run(ctx, runID)
	if err != nil {
		return nil, err
	}
	if run.Status == backuptypes.SystemBackupStatusSucceeded {
		return succeeded, nil
	}
	supplied, err := s.recoveryKeys.RunKey(ctx, runID)
	if err != nil {
		return nil, err
	}
	key, err := s.recoveryKey(ctx, supplied)
	if err != nil {
		return nil, err
	}
	if resuming {
		outcome, reconcileErr := s.reconcileBackup(ctx, previous, runID, key)
		if reconcileErr != nil {
			return nil, reconcileErr
		}
		if outcome.Status != scheduler.Succeeded {
			return nil, errors.New(outcome.Message)
		}
		return succeeded, nil
	}
	checkpoint, err := json.Marshal(input.Checkpoint)
	if err != nil {
		return nil, err
	}
	target := scheduler.TargetOutcome{ResourceType: "system_backup", ID: runID, Status: scheduler.Running, RecoveryData: checkpoint, ActivityID: t.ActivityID()}
	if progressErr := jobcontext.Progress(ctx, target); progressErr != nil {
		return nil, progressErr
	}
	if _, executeErr := s.executeBackup(ctx, &preparedSystemBackup{run: run, recoveryKey: key, checkpoint: input.Checkpoint}); executeErr != nil {
		return nil, executeErr
	}
	return succeeded, nil
}

// ListBackupHistory returns a unified, server-paginated view of local backup records.
func (s *Service) ListBackupHistory(ctx context.Context, params pagination.QueryParams) ([]backuptypes.HistoryEntry, pagination.Response, error) {
	history, page, err := s.store.ListHistory(ctx, params)
	if err != nil {
		return nil, pagination.Response{}, fmt.Errorf("list backup history: %w", err)
	}
	destinations := s.s3Destinations.DestinationsByID(ctx)
	remoteAvailable := backup.RemoteSnapshotChecker(ctx, s.s3Destinations)
	instanceID := ""
	if s.settingsService != nil {
		instanceID = s.settingsService.GetSettingsConfig().InstanceID.Value
	}
	for i := range history {
		history[i].S3DestinationName = destinations[history[i].S3DestinationID].Name
		// Volume backups discovered from another instance live under that instance's root.
		root := "arcane-system-recovery"
		if history[i].ResourceType == "volume" {
			root = ""
			if owner := cmp.Or(history[i].RemoteInstanceID, instanceID); owner != "" {
				root = path.Join(backup.VolumeRoot, owner)
			}
		}
		history[i].RemoteAvailable = remoteAvailable(history[i].S3DestinationID, root, history[i].RemoteSnapshotID)
	}

	page.GrandTotalItems = page.TotalItems
	return history, page, nil
}
