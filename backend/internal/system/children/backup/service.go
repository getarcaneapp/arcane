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

	"github.com/getarcaneapp/arcane/types/v3"
	"github.com/italypaleale/francis/builtin/workflow"
	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/api/types/mount"
	"github.com/moby/moby/client"
	"go.getarcane.app/docker"
	"go.getarcane.app/docker/compat"
	kit "go.getarcane.app/kit/pkg"
	"go.getarcane.app/sys/cgroup"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"
	"gorm.io/gorm"

	"github.com/getarcaneapp/arcane/backend/v3/internal/activity"
	"github.com/getarcaneapp/arcane/backend/v3/internal/backup"
	"github.com/getarcaneapp/arcane/backend/v3/internal/common"
	"github.com/getarcaneapp/arcane/backend/v3/internal/config"
	"github.com/getarcaneapp/arcane/backend/v3/internal/database"
	dockerInternal "github.com/getarcaneapp/arcane/backend/v3/internal/docker"
	"github.com/getarcaneapp/arcane/backend/v3/internal/s3"
	"github.com/getarcaneapp/arcane/backend/v3/internal/settings"
	"github.com/getarcaneapp/arcane/backend/v3/internal/system/children/backup/children/snapshots"
	"github.com/getarcaneapp/arcane/backend/v3/internal/system/children/backup/children/volumes"
	"github.com/getarcaneapp/arcane/backend/v3/internal/volume"
	"github.com/getarcaneapp/arcane/backend/v3/pkg/authz"
	activitylib "github.com/getarcaneapp/arcane/backend/v3/pkg/libarcane/activity"
	"github.com/getarcaneapp/arcane/backend/v3/pkg/pagination"
	"github.com/getarcaneapp/arcane/backend/v3/pkg/projects"
	"github.com/getarcaneapp/arcane/backend/v3/pkg/scheduler/entityjobs"
	"github.com/getarcaneapp/arcane/backend/v3/pkg/scheduler/flow"
	"github.com/getarcaneapp/arcane/backend/v3/pkg/scheduler/jobcontext"
	"github.com/getarcaneapp/arcane/backend/v3/pkg/scheduler/runs"
	"github.com/getarcaneapp/arcane/backend/v3/pkg/utils"
	"github.com/getarcaneapp/arcane/backend/v3/pkg/utils/backupbrowser"
	"github.com/getarcaneapp/arcane/backend/v3/pkg/utils/tracing"
)

const (
	defaultSystemBackupSchedule = "0 0 3 * * *"
	systemRecoveryHelperPath    = "/app/arcane-recovery-helper"
)

var ErrSystemBackupAlreadyRunning = common.Classify(common.ErrConflict, errors.New("an Arcane system backup is already running"))

// Store persists system backup runs and policies through their public DTOs;
// the system domain owns the persistence models.
type Store struct {
	CreateRun                 func(ctx context.Context, run *types.SystemBackupRun) error
	SaveRun                   func(ctx context.Context, run *types.SystemBackupRun) error
	FailRun                   func(ctx context.Context, id, message string) error
	DeleteRun                 func(ctx context.Context, id string) error
	Run                       func(ctx context.Context, id string) (*types.SystemBackupRun, error)
	ListRuns                  func(ctx context.Context, params pagination.QueryParams) ([]types.SystemBackupRun, pagination.Response, error)
	ExpiredRunIDs             func(ctx context.Context, policyID string, keep int) ([]string, error)
	RunsByID                  func(ctx context.Context, ids []string) ([]*types.SystemBackupRun, error)
	LatestPolicyRun           func(ctx context.Context, policyID string) (*types.SystemBackupRun, error)
	RemoteSnapshotIDs         func(ctx context.Context, destinationID string) ([]string, error)
	CountRuns                 func(ctx context.Context) (int64, error)
	CountVolumeRepositoryRuns func(ctx context.Context) (int64, error)
	CountRunningVolumeBackups func(ctx context.Context) (int64, error)
	Policies                  func(ctx context.Context) ([]types.SystemBackupPolicy, error)
	Policy                    func(ctx context.Context, id string) (*types.SystemBackupPolicy, error)
	ReconcilePolicies         func(
		ctx context.Context,
		updates []types.UpdateBackupPolicy,
		build func(context.Context, *types.SystemBackupPolicy, types.UpdateBackupPolicy) error,
		unregister func(context.Context, string),
		reschedule func(context.Context, *types.SystemBackupPolicy),
	) error
	DisablePolicyRemote  func(ctx context.Context, policy *types.SystemBackupPolicy, field string) (bool, error)
	CheckScheduledRemote func(ctx context.Context, destinationID string) error
	ListHistory          func(ctx context.Context, params pagination.QueryParams) ([]types.SystemVolumeBackupHistoryEntry, pagination.Response, error)
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
	SettingsEnv           settings.Env
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
	settingsEnv           settings.Env
	recoveryKeys          *backup.RecoveryKeyStore
	resolveRuntimeOptions RuntimeOptionsResolver
	jobs                  *entityjobs.Registry
	snapshots             *snapshots.Service
	volumes               *volumes.Service
	flow                  *flow.Engine
	// backupWorkflow runs backups StartBackup accepted; policyWorkflow runs scheduled policies.
	backupWorkflow *flow.Workflow
	policyWorkflow *flow.Workflow
	operations     metric.Int64Counter
}

// manualBackupInput is a manual backup's workflow payload; the run row is created before it starts.
type manualBackupInput struct {
	Checkpoint systemBackupRecovery  `json:"checkpoint"`
	Requester  types.BackupRequester `json:"requester"`
}

// systemBackupRecovery is the checkpoint a run records so an interrupted backup can resume.
type systemBackupRecovery struct {
	BackupID     string `json:"backupId"`
	LocalEnabled bool   `json:"localEnabled"`
	S3Enabled    bool   `json:"s3Enabled"`
}

type preparedSystemBackup struct {
	run         *types.SystemBackupRun
	recoveryKey string
	checkpoint  systemBackupRecovery
}

func NewService(deps Dependencies) *Service {
	operations, counterErr := otel.Meter(tracing.InstrumentationName).Int64Counter("arcane.backups",
		metric.WithDescription("Backup operations by outcome"), metric.WithUnit("{backup}"))
	if counterErr != nil {
		otel.Handle(counterErr)
	}
	jobs := entityjobs.New("system-backup:", backup.SystemAdmissionScope)
	var service *Service
	service = &Service{
		store:                 deps.Store,
		dockerService:         deps.Docker,
		volumeService:         deps.Volumes,
		engine:                deps.Engine,
		s3Destinations:        deps.S3Destinations,
		activityService:       deps.Activity,
		settingsService:       deps.Settings,
		config:                deps.Config,
		settingsEnv:           deps.SettingsEnv,
		recoveryKeys:          deps.RecoveryKeys,
		resolveRuntimeOptions: deps.ResolveRuntimeOptions,
		jobs:                  jobs,
		operations:            operations,
		snapshots: snapshots.NewService(
			deps.Engine,
			deps.SQLDB,
			deps.ActorStore,
			func(ctx context.Context, dockerClient *client.Client, readOnly bool) (backup.Repository, error) {
				return service.localRepository(ctx, dockerClient, readOnly)
			},
			func(ctx context.Context, destinationID string) (backup.Repository, error) {
				return service.remoteRepository(ctx, destinationID)
			},
			func() (string, error) { return service.databaseFile() },
			func(ctx context.Context) string {
				return projects.ResolveConfiguredContainerDirectory(service.projectsSetting(ctx), "/app/data/projects")
			},
			func(ctx context.Context) map[string]string { return service.recoveryEnvironment(ctx) },
			kit.FromPtr(deps.Config).GitWorkDir,
		),
		volumes: volumes.NewService(volumes.Dependencies{
			DB:             deps.DB,
			Engine:         deps.Engine,
			Volumes:        deps.Volumes,
			S3Destinations: deps.S3Destinations,
			Activity:       deps.Activity,
			Settings:       deps.Settings,
			Jobs:           jobs,
			AlreadyRunning: ErrSystemBackupAlreadyRunning,
		}),
	}
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
	maps.Copy(result, s.settingsEnv.Variables())
	if value := s.projectsSetting(ctx); value != "" {
		result[settings.EnvName("projectsDirectory", "")] = value
	}
	return result
}

// startOperation opens a backup span; the returned func ends it and counts the outcome.
func (s *Service) startOperation(ctx context.Context, operation string, attrs ...attribute.KeyValue) (context.Context, func(err error, results ...attribute.KeyValue)) {
	attrs = append(attrs, attribute.String("arcane.environment.id", "0"), attribute.String("arcane.backup.kind", "system"))
	ctx, span := otel.Tracer(tracing.InstrumentationName).Start(ctx, "backup."+operation, trace.WithAttributes(attrs...))
	return ctx, func(err error, results ...attribute.KeyValue) {
		span.SetAttributes(results...)
		tracing.End(span, err)
		if s.operations != nil {
			s.operations.Add(context.WithoutCancel(ctx), 1, metric.WithAttributes(
				attribute.String("arcane.backup.operation", operation),
				attribute.String("arcane.backup.outcome", kit.Ternary(err == nil, "success", "failure")),
			))
		}
	}
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
	s.backupWorkflow, err = engine.Define(flow.Definition{
		Name:        "system-backup",
		Version:     1,
		Fingerprint: "9b44e4c38b7fcf8951468cc43a51b66c644d1104f1a56fa49bf8c065dc51c046",
		Concurrency: 1,
		Timeout:     24 * time.Hour,
		Activity: activitylib.StartRequest{
			Type:          types.ActivityTypeResourceAction,
			EnvironmentID: "0",
			ResourceType:  new("system_backup"),
			ResourceName:  new("Arcane"),
		},
		Labels: map[string]string{"backup": "Creating system backup"},
		Steps:  []workflow.StepSpec{workflow.Step("backup", engine.Handler(s.runManualBackup), workflow.WithMaxAttempts(1))},
	})
	if err != nil {
		return err
	}
	s.policyWorkflow, err = engine.Define(flow.Definition{
		Name:        "system-backup-policy",
		Version:     1,
		Fingerprint: "42fa9de20fefac192148a5f6d68132e34ae87948160b5bb0cc5876d506843bcf",
		Concurrency: 1,
		Timeout:     24 * time.Hour,
		Activity: activitylib.StartRequest{
			Type:          types.ActivityTypeResourceAction,
			EnvironmentID: "0",
			ResourceType:  new("system_backup"),
			ResourceName:  new("Arcane"),
			StartedBy:     &types.SystemUser,
			Step:          "Creating scheduled system backup",
			LatestMessage: "Creating scheduled Arcane system backup",
		},
		Labels: map[string]string{"backup": "Creating scheduled system backup"},
		Steps: []workflow.StepSpec{workflow.Step("backup", engine.Handler(func(ctx context.Context, t flow.Task) (any, error) {
			var policyID string
			if decodeErr := t.Payload(&policyID); decodeErr != nil {
				return nil, decodeErr
			}
			previous, _ := jobcontext.Run(ctx)
			if slices.ContainsFunc(previous.Outcome.Targets, func(target types.TargetOutcome) bool {
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

func (s *Service) CreateBackup(ctx context.Context, user types.Actor, trigger string, request types.CreateSystemBackupRequest) (*types.SystemBackupRun, error) {
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
func (s *Service) prepareBackup(ctx context.Context, trigger string, request types.CreateSystemBackupRequest) (*preparedSystemBackup, error) {
	if !s.SupportedDatabaseProvider() {
		return nil, errors.New("arcane system recovery currently requires the SQLite database provider")
	}
	recoveryKey, err := s.recoveryKey(ctx, request.RecoveryKey)
	if err != nil {
		return nil, err
	}
	selected, err := func() (*types.SystemBackupPolicy, error) {
		if request.PolicyID != "" {
			policy, policyErr := s.store.Policy(ctx, request.PolicyID)
			if policyErr == nil && policy == nil {
				return nil, errors.New("system backup policy not found")
			}
			return policy, policyErr
		}
		switch request.Destination {
		case types.SystemBackupDestinationLocal, "":
			return &types.SystemBackupPolicy{
				LocalEnabled:    true,
				S3DestinationID: strings.TrimSpace(request.S3DestinationID),
			}, nil
		case types.SystemBackupDestinationS3:
			return &types.SystemBackupPolicy{
				S3Enabled:       true,
				S3DestinationID: strings.TrimSpace(request.S3DestinationID),
			}, nil
		case types.SystemBackupDestinationLocalS3:
			return &types.SystemBackupPolicy{
				LocalEnabled:    true,
				S3Enabled:       true,
				S3DestinationID: strings.TrimSpace(request.S3DestinationID),
			}, nil
		default:
			return nil, errors.New("unknown system backup destination")
		}
	}()
	if err != nil {
		return nil, err
	}
	if !selected.LocalEnabled && !selected.S3Enabled {
		return nil, errors.New("select at least one system backup destination")
	}
	if selected.S3Enabled && selected.S3DestinationID == "" {
		return nil, errors.New("select an S3 destination for the system backup")
	}
	run := &types.SystemBackupRun{
		ID:        "system-" + uuid.New().String(),
		CreatedAt: time.Now().UTC(),
		Status:    types.SystemBackupStatusRunning,
		Trigger:   trigger,
		Destination: func() types.SystemBackupDestination {
			switch {
			case selected.LocalEnabled && selected.S3Enabled:
				return types.SystemBackupDestinationLocalS3
			case selected.LocalEnabled:
				return types.SystemBackupDestinationLocal
			default:
				return types.SystemBackupDestinationS3
			}
		}(),
		S3DestinationID: selected.S3DestinationID,
		PolicyID:        request.PolicyID,
	}
	if createErr := s.store.CreateRun(ctx, run); createErr != nil {
		return nil, createErr
	}
	checkpoint := systemBackupRecovery{
		BackupID:     run.ID,
		LocalEnabled: selected.LocalEnabled,
		S3Enabled:    selected.S3Enabled,
	}
	recoveryData, err := json.Marshal(checkpoint)
	if err == nil {
		err = jobcontext.Progress(ctx, types.TargetOutcome{
			ResourceType: "system_backup",
			ID:           cmp.Or(run.PolicyID, run.ID),
			Status:       types.RunStatusRunning,
			RecoveryData: recoveryData,
		})
	}
	if err != nil {
		return nil, err
	}
	return &preparedSystemBackup{
		run:         run,
		recoveryKey: recoveryKey,
		checkpoint:  checkpoint,
	}, nil
}

// executeBackup snapshots straight to S3 for S3-only runs, otherwise keeps a local snapshot and optionally replicates it to S3, then records the run's result.
func (s *Service) executeBackup(ctx context.Context, prepared *preparedSystemBackup) (_ *types.SystemBackupRun, err error) {
	run, key, checkpoint := prepared.run, prepared.recoveryKey, prepared.checkpoint
	ctx, finish := s.startOperation(ctx, "create", attribute.String("arcane.backup.id", run.ID), attribute.String("arcane.backup.trigger", run.Trigger))
	defer func() {
		finish(err, attribute.String("arcane.backup.target", string(run.Destination)), attribute.Int64("arcane.backup.size_bytes", run.Size),
			attribute.String("arcane.backup.s3_destination_id", run.S3DestinationID))
	}()
	defer func() {
		err = cmp.Or(err, ctx.Err())
		*run = types.SystemBackupRun{ //exhaustruct:enforce
			RemoteAvailable:   run.RemoteAvailable,
			ActivityID:        run.ActivityID,
			ID:                run.ID,
			Size:              run.Size,
			CreatedAt:         run.CreatedAt,
			Status:            kit.Ternary(err != nil, types.SystemBackupStatusFailed, types.SystemBackupStatusSucceeded),
			Trigger:           run.Trigger,
			Destination:       run.Destination,
			LocalSnapshotID:   run.LocalSnapshotID,
			RemoteSnapshotID:  run.RemoteSnapshotID,
			S3DestinationID:   run.S3DestinationID,
			S3DestinationName: run.S3DestinationName,
			PolicyID:          run.PolicyID,
			Error:             kit.Ternary(err != nil, fmt.Sprint(err), ""),
		}
		if saveErr := s.store.SaveRun(context.WithoutCancel(ctx), run); saveErr != nil {
			err = errors.Join(err, fmt.Errorf("failed to save system backup result: %w", saveErr))
		}
		if err == nil {
			err = jobcontext.Progress(ctx, types.TargetOutcome{
				ResourceType: "system_backup",
				ID:           cmp.Or(run.PolicyID, run.ID),
				Status:       types.RunStatusSucceeded,
			})
		}
	}()
	defer utils.RecoverToError(&err, "system backup")
	progress := func(stage string, status types.RunStatus, message string) error {
		return jobcontext.Progress(ctx, types.TargetOutcome{
			ResourceType: "backup_destination",
			ID:           run.ID + ":" + stage,
			Status:       status,
			Message:      message,
		})
	}
	dockerClient, err := s.dockerService.GetClient(ctx)
	if err != nil {
		return run, err
	}
	if progressErr := progress("stage", types.RunStatusRunning, ""); progressErr != nil {
		return run, progressErr
	}
	// S3-only runs write straight to S3 so no local repository is created.
	repository, err := func() (backup.Repository, error) {
		if !checkpoint.LocalEnabled {
			return s.remoteRepository(ctx, run.S3DestinationID)
		}
		return s.localRepository(ctx, dockerClient, false)
	}()
	if err != nil {
		return run, fmt.Errorf("failed to create %s system recovery snapshot: %w", kit.Ternary(checkpoint.LocalEnabled, "local", "S3"), err)
	}
	staged, err := s.snapshots.Create(ctx, dockerClient, repository, key, run.ID)
	if err != nil {
		return run, fmt.Errorf("failed to create %s system recovery snapshot: %w", kit.Ternary(checkpoint.LocalEnabled, "local", "S3"), err)
	}
	if progressErr := progress("stage", types.RunStatusSucceeded, staged.ID); progressErr != nil {
		return run, progressErr
	}
	if !checkpoint.LocalEnabled {
		*run = types.SystemBackupRun{ //exhaustruct:enforce
			RemoteAvailable:   run.RemoteAvailable,
			ActivityID:        run.ActivityID,
			ID:                run.ID,
			Size:              staged.Size,
			CreatedAt:         run.CreatedAt,
			Status:            run.Status,
			Trigger:           run.Trigger,
			Destination:       run.Destination,
			LocalSnapshotID:   run.LocalSnapshotID,
			RemoteSnapshotID:  staged.ID,
			S3DestinationID:   run.S3DestinationID,
			S3DestinationName: run.S3DestinationName,
			PolicyID:          run.PolicyID,
			Error:             run.Error,
		}
		return run, s.store.SaveRun(ctx, run)
	}
	*run = types.SystemBackupRun{ //exhaustruct:enforce
		RemoteAvailable:   run.RemoteAvailable,
		ActivityID:        run.ActivityID,
		ID:                run.ID,
		Size:              staged.Size,
		CreatedAt:         run.CreatedAt,
		Status:            run.Status,
		Trigger:           run.Trigger,
		Destination:       run.Destination,
		LocalSnapshotID:   staged.ID,
		RemoteSnapshotID:  run.RemoteSnapshotID,
		S3DestinationID:   run.S3DestinationID,
		S3DestinationName: run.S3DestinationName,
		PolicyID:          run.PolicyID,
		Error:             run.Error,
	}
	if saveErr := s.store.SaveRun(ctx, run); saveErr != nil {
		return run, saveErr
	}
	if progressErr := progress("local", types.RunStatusSucceeded, staged.ID); progressErr != nil {
		return run, progressErr
	}
	if !checkpoint.S3Enabled {
		return run, nil
	}
	remote, err := s.remoteRepository(ctx, run.S3DestinationID)
	if err != nil {
		return run, err
	}
	if progressErr := progress("remote", types.RunStatusRunning, ""); progressErr != nil {
		return run, progressErr
	}
	snapshot, err := s.engine.Replicate(ctx, dockerClient, repository, staged.ID, remote, key, "arcane-system-recovery", backup.RunSnapshotTag(run.ID))
	if err != nil {
		return run, fmt.Errorf("failed to create S3 system recovery snapshot: %w", err)
	}
	*run = types.SystemBackupRun{ //exhaustruct:enforce
		RemoteAvailable:   run.RemoteAvailable,
		ActivityID:        run.ActivityID,
		ID:                run.ID,
		Size:              cmp.Or(run.Size, snapshot.Size),
		CreatedAt:         run.CreatedAt,
		Status:            run.Status,
		Trigger:           run.Trigger,
		Destination:       run.Destination,
		LocalSnapshotID:   run.LocalSnapshotID,
		RemoteSnapshotID:  snapshot.ID,
		S3DestinationID:   run.S3DestinationID,
		S3DestinationName: run.S3DestinationName,
		PolicyID:          run.PolicyID,
		Error:             run.Error,
	}
	if saveErr := s.store.SaveRun(ctx, run); saveErr != nil {
		return run, saveErr
	}
	return run, progress("remote", types.RunStatusSucceeded, snapshot.ID)
}

func (s *Service) ListBackups(ctx context.Context, params pagination.QueryParams) ([]types.SystemBackupRun, pagination.Response, error) {
	result, response, err := s.store.ListRuns(ctx, params)
	if err != nil {
		return nil, pagination.Response{}, fmt.Errorf("failed to list system backups: %w", err)
	}
	names := s.s3Destinations.DestinationsByID(ctx)
	remoteAvailable := backup.RemoteSnapshotChecker(ctx, s.s3Destinations)
	decorated := make([]types.SystemBackupRun, 0, len(result))
	for _, run := range result {
		decorated = append(decorated, types.SystemBackupRun{ //exhaustruct:enforce
			RemoteAvailable:   remoteAvailable(run.S3DestinationID, "arcane-system-recovery", run.RemoteSnapshotID),
			ActivityID:        run.ActivityID,
			ID:                run.ID,
			Size:              run.Size,
			CreatedAt:         run.CreatedAt,
			Status:            run.Status,
			Trigger:           run.Trigger,
			Destination:       run.Destination,
			LocalSnapshotID:   run.LocalSnapshotID,
			RemoteSnapshotID:  run.RemoteSnapshotID,
			S3DestinationID:   run.S3DestinationID,
			S3DestinationName: names[run.S3DestinationID].Name,
			PolicyID:          run.PolicyID,
			Error:             run.Error,
		})
	}
	return decorated, response, nil
}

// BrowseBackupFiles returns one page from the backup's logical project tree.
func (s *Service) BrowseBackupFiles(ctx context.Context, id, recoveryKey, requestedPath string, params pagination.QueryParams) ([]types.BackupFileEntry, pagination.Response, error) {
	listPath, recursive, err := backupbrowser.ListScope(requestedPath, params)
	if err != nil {
		return nil, pagination.Response{}, fmt.Errorf("%w: %w", common.ErrInvalidBackupSelection, err)
	}
	run, err := s.store.Run(ctx, id)
	if err != nil {
		return nil, pagination.Response{}, err
	}
	if run.Status != types.SystemBackupStatusSucceeded {
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
func (s *Service) RestoreBackupFiles(ctx context.Context, id string, request types.RestoreSystemBackupFilesRequest, user types.Actor) (opErr error) {
	ctx, finish := s.startOperation(ctx, "restore_files", attribute.String("arcane.backup.id", id),
		attribute.Int("arcane.result.count", len(request.Paths)), attribute.Bool("arcane.backup.select_all", request.SelectAll))
	defer func() { finish(opErr) }()
	lease, err := s.acquireRun(ctx)
	if err != nil {
		return err
	}
	defer lease.Release(ctx)
	run, err := s.store.Run(ctx, id)
	if err != nil {
		return err
	}
	if run.Status != types.SystemBackupStatusSucceeded {
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
	safetyBackup := func(ctx context.Context, safetyRequest types.CreateSystemBackupRequest) (*types.SystemBackupRun, error) {
		prepared, prepareErr := s.prepareBackup(ctx, types.SystemBackupTriggerSafety, safetyRequest)
		if prepareErr != nil {
			return nil, prepareErr
		}
		return s.executeBackup(ctx, prepared)
	}
	return s.snapshots.RestoreFiles(ctx, dockerClient, key, *run, request.BackupRestoreSelection, safetyBackup)
}

func (s *Service) DeleteBackup(ctx context.Context, id, recoveryKey string) (opErr error) {
	ctx, finish := s.startOperation(ctx, "delete", attribute.String("arcane.backup.id", id))
	defer func() { finish(opErr) }()
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
	return s.deleteRuns(ctx, []*types.SystemBackupRun{run}, recoveryKey, true)
}

// deleteRuns forgets snapshots grouped by repository under the caller's run lease.
func (s *Service) deleteRuns(ctx context.Context, backupRuns []*types.SystemBackupRun, recoveryKey string, includeRemote bool) error {
	var localRuns []*types.SystemBackupRun
	var localIDs []string
	remoteGroups := make(map[string][]*types.SystemBackupRun)
	for _, run := range backupRuns {
		if run.LocalSnapshotID != "" {
			localRuns, localIDs = append(localRuns, run), append(localIDs, run.LocalSnapshotID)
		}
		if includeRemote && run.RemoteSnapshotID != "" {
			remoteGroups[run.S3DestinationID] = append(remoteGroups[run.S3DestinationID], run)
		}
	}
	// A run without snapshots (a failed attempt) is just a row; deleting it needs neither the key nor Rustic.
	key, err := func() (string, error) {
		if len(localRuns) == 0 && len(remoteGroups) == 0 {
			return "", nil
		}
		return s.recoveryKey(ctx, recoveryKey)
	}()
	if err != nil {
		return err
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
				*run = types.SystemBackupRun{ //exhaustruct:enforce
					RemoteAvailable:   run.RemoteAvailable,
					ActivityID:        run.ActivityID,
					ID:                run.ID,
					Size:              run.Size,
					CreatedAt:         run.CreatedAt,
					Status:            run.Status,
					Trigger:           run.Trigger,
					Destination:       run.Destination,
					LocalSnapshotID:   "",
					RemoteSnapshotID:  run.RemoteSnapshotID,
					S3DestinationID:   run.S3DestinationID,
					S3DestinationName: run.S3DestinationName,
					PolicyID:          run.PolicyID,
					Error:             run.Error,
				}
			}
		}
	}
	for destinationID, group := range remoteGroups {
		snapshotIDs := make([]string, len(group))
		for index, run := range group {
			snapshotIDs[index] = run.RemoteSnapshotID
		}
		repository, repoErr := s.remoteRepository(ctx, destinationID)
		if repoErr == nil {
			observation, checkErr := backup.CheckRemoteRepository(ctx, s.s3Destinations, destinationID, "arcane-system-recovery")
			switch {
			case checkErr != nil:
				repoErr = checkErr
			case !observation.Available:
				// Nothing remains to forget; drop the references so the run can be removed.
				slog.WarnContext(ctx, "S3 repository is missing; dropping remote snapshot references", "destinationId", destinationID, "snapshots", len(snapshotIDs))
			default:
				repoErr = s.engine.ForgetSnapshots(ctx, dockerClient, repository, key, snapshotIDs)
			}
		}
		if repoErr != nil {
			deleteErr = errors.Join(deleteErr, fmt.Errorf("failed to delete S3 snapshots: %w", repoErr))
			continue
		}
		for _, run := range group {
			*run = types.SystemBackupRun{ //exhaustruct:enforce
				RemoteAvailable:   run.RemoteAvailable,
				ActivityID:        run.ActivityID,
				ID:                run.ID,
				Size:              run.Size,
				CreatedAt:         run.CreatedAt,
				Status:            run.Status,
				Trigger:           run.Trigger,
				Destination:       run.Destination,
				LocalSnapshotID:   run.LocalSnapshotID,
				RemoteSnapshotID:  "",
				S3DestinationID:   "",
				S3DestinationName: run.S3DestinationName,
				PolicyID:          run.PolicyID,
				Error:             run.Error,
			}
		}
	}
	for _, run := range backupRuns {
		if run.LocalSnapshotID == "" && run.RemoteSnapshotID == "" {
			if deleteRunErr := s.store.DeleteRun(ctx, run.ID); deleteRunErr != nil {
				deleteErr = errors.Join(deleteErr, fmt.Errorf("failed to delete system backup record: %w", deleteRunErr))
			}
			continue
		}
		*run = types.SystemBackupRun{ //exhaustruct:enforce
			RemoteAvailable: run.RemoteAvailable,
			ActivityID:      run.ActivityID,
			ID:              run.ID,
			Size:            run.Size,
			CreatedAt:       run.CreatedAt,
			Status:          run.Status,
			Trigger:         run.Trigger,
			Destination: func() types.SystemBackupDestination {
				switch {
				case run.LocalSnapshotID != "" && run.RemoteSnapshotID != "":
					return types.SystemBackupDestinationLocalS3
				case run.LocalSnapshotID != "":
					return types.SystemBackupDestinationLocal
				default:
					return types.SystemBackupDestinationS3
				}
			}(),
			LocalSnapshotID:   run.LocalSnapshotID,
			RemoteSnapshotID:  run.RemoteSnapshotID,
			S3DestinationID:   run.S3DestinationID,
			S3DestinationName: run.S3DestinationName,
			PolicyID:          run.PolicyID,
			Error:             run.Error,
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
	repository, err := s.localRepository(ctx, dockerClient, false)
	if err != nil {
		return err
	}
	return s.engine.ForgetSnapshots(ctx, dockerClient, repository, key, nil)
}

func (s *Service) DiscoverRemoteBackups(ctx context.Context, request types.DiscoverSystemBackupsRequest) (int, error) {
	lease, err := s.acquireRun(ctx)
	if err != nil {
		return 0, err
	}
	defer lease.Release(ctx)
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
		run := &types.SystemBackupRun{
			ID: fmt.Sprintf("remote-%s-%s", request.S3DestinationID, snapshot.ID), Size: snapshot.Summary.TotalBytesProcessed,
			CreatedAt: kit.Ternary(snapshot.Time.IsZero(), time.Now().UTC(), snapshot.Time), Status: types.SystemBackupStatusSucceeded,
			Trigger: types.SystemBackupTriggerManual, Destination: types.SystemBackupDestinationS3,
			RemoteSnapshotID: snapshot.ID, S3DestinationID: request.S3DestinationID,
		}
		if createDiscoveredBackupErr := s.store.CreateRun(ctx, run); createDiscoveredBackupErr != nil {
			return created, fmt.Errorf("failed to save discovered system backup: %w", createDiscoveredBackupErr)
		}
		created++
	}
	return created, nil
}

func (s *Service) UploadBackup(ctx context.Context, id string, request types.UploadSystemBackupRequest) (*types.SystemBackupRun, error) {
	lease, err := s.acquireRun(ctx)
	if err != nil {
		return nil, err
	}
	defer lease.Release(ctx)
	run, err := s.store.Run(ctx, id)
	if err != nil {
		return nil, err
	}
	if run.Status != types.SystemBackupStatusSucceeded || run.LocalSnapshotID == "" {
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
	*run = types.SystemBackupRun{ //exhaustruct:enforce
		RemoteAvailable:   run.RemoteAvailable,
		ActivityID:        run.ActivityID,
		ID:                run.ID,
		Size:              run.Size,
		CreatedAt:         run.CreatedAt,
		Status:            run.Status,
		Trigger:           run.Trigger,
		Destination:       types.SystemBackupDestinationLocalS3,
		LocalSnapshotID:   run.LocalSnapshotID,
		RemoteSnapshotID:  snapshot.ID,
		S3DestinationID:   request.S3DestinationID,
		S3DestinationName: run.S3DestinationName,
		PolicyID:          run.PolicyID,
		Error:             run.Error,
	}
	if saveUploadedBackupErr := s.store.SaveRun(ctx, run); saveUploadedBackupErr != nil {
		return nil, fmt.Errorf("failed to save uploaded system backup: %w", saveUploadedBackupErr)
	}
	return run, nil
}

func (s *Service) RestoreBackup(ctx context.Context, id, recoveryKey string) (opErr error) {
	ctx, finish := s.startOperation(ctx, "restore", attribute.String("arcane.backup.id", id))
	defer func() { finish(opErr) }()
	lease, err := s.acquireRun(ctx)
	if err != nil {
		return err
	}
	defer lease.Release(ctx)
	run, err := s.store.Run(ctx, id)
	if err != nil {
		return err
	}
	if run.Status != types.SystemBackupStatusSucceeded {
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
	inspectResult, err := s.dockerService.Call(ctx, func(ctx context.Context, cli *client.Client) (client.ContainerInspectResult, error) {
		return compat.ContainerInspectWithCompatibility(ctx, cli, containerID, client.ContainerInspectOptions{})
	})
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
	prepared, prepareErr := s.prepareBackup(ctx, types.SystemBackupTriggerSafety, types.CreateSystemBackupRequest{
		Destination: types.SystemBackupDestinationLocal,
		RecoveryKey: key,
	})
	if prepareErr != nil {
		return fmt.Errorf("failed to create pre-restore system backup: %w", prepareErr)
	}
	safetyBackup, err := s.executeBackup(ctx, prepared)
	if err != nil {
		return fmt.Errorf("failed to create pre-restore system backup: %w", err)
	}
	rollbackStages, err := s.snapshots.PlanRollback(ctx, dockerClient, key, safetyBackup.LocalSnapshotID, current.Mounts, dataDirectory, projectsDirectory)
	if err != nil {
		return err
	}
	requestData, err := json.Marshal(types.RecoveryRestoreRequest{
		BackupID:         run.ID,
		ContainerID:      current.ID,
		ContainerImage:   current.Config.Image,
		SnapshotID:       plan.SnapshotID,
		RecoveryKey:      key,
		LocalSnapshotID:  run.LocalSnapshotID,
		RemoteSnapshotID: run.RemoteSnapshotID,
		S3DestinationID:  run.S3DestinationID,
		Size:             run.Size,
		// Rustic needs a named network to reach network-local S3 once Arcane stops; bridge and none carry no DNS.
		NetworkMode: func() string {
			names := slices.Sorted(maps.Keys(kit.FromPtr(current.NetworkSettings).Networks))
			if index := slices.IndexFunc(names, func(name string) bool { return name != "bridge" && name != "none" }); index >= 0 {
				return names[index]
			}
			return ""
		}(),
		SafetyBackup: &types.RecoverySafetyBackup{
			ID:              safetyBackup.ID,
			LocalSnapshotID: safetyBackup.LocalSnapshotID,
			Size:            safetyBackup.Size,
			CreatedAt:       safetyBackup.CreatedAt,
		},
		ProjectsSetting:  s.projectsSetting(ctx),
		ProjectsIncluded: plan.ProjectsIncluded,
		Stages:           plan.Stages,
		RollbackStages:   rollbackStages,
	})
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
	executableMount := docker.MountForSubpath(current.Mounts, executablePath, systemRecoveryHelperPath)
	if executableMount != nil && executableMount.Type != mount.TypeBind {
		return errors.New("resolve recovery helper executable: the Arcane executable must come from the image or a bind mount")
	}
	mounts := append(slices.Clone(runtimeMounts), *appDataMount)
	if executableMount != nil {
		mounts = append(mounts, mount.Mount{
			Type:           executableMount.Type,
			Source:         executableMount.Source,
			Target:         executableMount.Target,
			ReadOnly:       true,
			Consistency:    executableMount.Consistency,
			BindOptions:    executableMount.BindOptions,
			VolumeOptions:  executableMount.VolumeOptions,
			ImageOptions:   executableMount.ImageOptions,
			TmpfsOptions:   executableMount.TmpfsOptions,
			ClusterOptions: executableMount.ClusterOptions,
		})
	}
	created, err := s.dockerService.Call(ctx, func(ctx context.Context, cli *client.Client) (client.ContainerCreateResult, error) {
		return cli.ContainerCreate(ctx, client.ContainerCreateOptions{
			Config: &container.Config{
				Image:  current.Config.Image,
				Cmd:    []string{kit.Ternary(executableMount != nil, systemRecoveryHelperPath, executablePath), "recovery-restore", "--request", requestFile},
				User:   "0:0",
				Env:    containerEnv,
				Labels: map[string]string{"com.getarcaneapp.arcane.recovery": "true", "com.getarcaneapp.arcane": "true"},
			},
			HostConfig: &container.HostConfig{
				AutoRemove:  true,
				Mounts:      mounts,
				NetworkMode: networkMode,
				SecurityOpt: slices.Clone(kit.FromPtr(current.HostConfig).SecurityOpt),
				Privileged:  kit.FromPtr(current.HostConfig).Privileged,
			},
			Name: fmt.Sprintf("%s-recovery-%d", strings.TrimPrefix(current.Name, "/"), time.Now().Unix()),
		})
	})
	if err != nil {
		return fmt.Errorf("create recovery helper container: %w", err)
	}
	if _, containerStartErr := s.dockerService.Call(ctx, func(ctx context.Context, cli *client.Client) (client.ContainerStartResult, error) {
		return cli.ContainerStart(ctx, created.ID, client.ContainerStartOptions{})
	}); containerStartErr != nil {
		_, _ = s.dockerService.Call(ctx, func(ctx context.Context, cli *client.Client) (client.ContainerRemoveResult, error) {
			return cli.ContainerRemove(ctx, created.ID, client.ContainerRemoveOptions{Force: true})
		})
		return fmt.Errorf("start recovery helper container: %w", containerStartErr)
	}
	cleanupRequest = false
	return nil
}

func (s *Service) GenerateRecoveryKey() (*types.SystemBackupRecoveryKey, error) {
	recoveryKey, err := backup.GenerateRecoveryKey()
	if err != nil {
		return nil, err
	}
	return &types.SystemBackupRecoveryKey{RecoveryKey: recoveryKey}, nil
}

func (s *Service) SetRecoveryKey(ctx context.Context, recoveryKey string) (*types.SystemBackupRecoveryKeyStatus, error) {
	if err := backup.ValidateRecoveryKey(recoveryKey); err != nil {
		return nil, err
	}
	lease, err := s.acquireRun(ctx)
	if err != nil {
		return nil, err
	}
	defer lease.Release(ctx)
	previous, err := s.recoveryKeys.Get(ctx)
	if err != nil {
		if !errors.Is(err, backup.ErrRecoveryKeyNotConfigured) {
			slog.WarnContext(ctx, "could not read the current recovery key; existing backups must already open with the new key", "error", err.Error())
		}
		previous = ""
	}
	if previous == recoveryKey {
		if setErr := s.recoveryKeys.Set(ctx, recoveryKey); setErr != nil {
			return nil, setErr
		}
		return &types.SystemBackupRecoveryKeyStatus{Configured: true}, nil
	}
	runningVolumeBackupsErr := common.Classify(common.ErrConflict, errors.New("wait for the running volume backups to finish before replacing the recovery key"))
	runningVolumeBackups, countErr := s.store.CountRunningVolumeBackups(ctx)
	if countErr != nil {
		return nil, countErr
	}
	if runningVolumeBackups > 0 {
		return nil, runningVolumeBackupsErr
	}
	if rekeyErr := s.rekeyRepositories(ctx, previous, recoveryKey); rekeyErr != nil {
		return nil, rekeyErr
	}
	rollback := func(cause error) error {
		if previous == "" {
			return cause
		}
		return errors.Join(cause, s.rekeyRepositories(context.WithoutCancel(ctx), recoveryKey, previous))
	}
	runningVolumeBackups, countErr = s.store.CountRunningVolumeBackups(ctx)
	if countErr != nil {
		return nil, rollback(errors.Join(runningVolumeBackupsErr, countErr))
	}
	if runningVolumeBackups > 0 {
		return nil, rollback(runningVolumeBackupsErr)
	}
	if setErr := s.recoveryKeys.Set(ctx, recoveryKey); setErr != nil {
		return nil, rollback(setErr)
	}
	return &types.SystemBackupRecoveryKeyStatus{Configured: true}, nil
}

func (s *Service) rekeyRepositories(ctx context.Context, previous, next string) error {
	systemRuns, err := s.store.CountRuns(ctx)
	if err != nil {
		return err
	}
	volumeRuns, err := s.store.CountVolumeRepositoryRuns(ctx)
	if err != nil {
		return err
	}
	if systemRuns == 0 && volumeRuns == 0 {
		return nil
	}
	var dockerClient *client.Client
	var systemRepositories []backup.Repository
	if systemRuns > 0 {
		dockerClient, err = s.dockerService.GetClient(ctx)
		if err != nil {
			return err
		}
		local, localErr := s.localRepository(ctx, dockerClient, false)
		if localErr != nil {
			return localErr
		}
		systemRepositories = append(systemRepositories, local)
		destinations, listErr := s.s3Destinations.ListAllS3Destinations(ctx)
		if listErr != nil {
			return fmt.Errorf("failed to list S3 destinations for re-key: %w", listErr)
		}
		for _, destination := range destinations {
			remote, remoteErr := s.remoteRepository(ctx, destination.ID)
			if remoteErr != nil {
				return remoteErr
			}
			systemRepositories = append(systemRepositories, remote)
		}
		if rekeyErr := s.engine.RekeyRepositories(ctx, dockerClient, systemRepositories, next, previous); rekeyErr != nil {
			return rekeyErr
		}
	}
	if volumeRuns == 0 {
		return nil
	}
	migrateErr := s.volumeService.MigrateRepositoryPasswords(ctx, previous, next)
	if migrateErr == nil || len(systemRepositories) == 0 || previous == "" {
		return migrateErr
	}
	return errors.Join(migrateErr, s.engine.RekeyRepositories(context.WithoutCancel(ctx), dockerClient, systemRepositories, previous, next))
}

func (s *Service) GetPolicies(ctx context.Context) (*types.SystemBackupPolicyCollection, error) {
	policies, err := s.store.Policies(ctx)
	if err != nil {
		return nil, err
	}
	configured, err := s.recoveryKeys.Configured(ctx)
	if err != nil {
		return nil, err
	}
	destinations := s.s3Destinations.DestinationsByID(ctx)
	decorated := make([]types.SystemBackupPolicy, 0, len(policies))
	for _, policy := range policies {
		lastRun, runErr := s.store.LatestPolicyRun(ctx, policy.ID)
		if runErr != nil {
			return nil, fmt.Errorf("failed to load latest system backup: %w", runErr)
		}
		decorated = append(decorated, types.SystemBackupPolicy{ //exhaustruct:enforce
			ID:                policy.ID,
			Enabled:           policy.Enabled,
			Schedule:          policy.Schedule,
			RetentionCount:    policy.RetentionCount,
			LocalEnabled:      policy.LocalEnabled,
			S3Enabled:         policy.S3Enabled,
			S3DestinationID:   policy.S3DestinationID,
			S3DestinationName: destinations[policy.S3DestinationID].Name,
			LastRun: func() *types.SystemBackupRun {
				if lastRun == nil {
					return nil
				}
				return &types.SystemBackupRun{ //exhaustruct:enforce
					RemoteAvailable:   lastRun.RemoteAvailable,
					ActivityID:        lastRun.ActivityID,
					ID:                lastRun.ID,
					Size:              lastRun.Size,
					CreatedAt:         lastRun.CreatedAt,
					Status:            lastRun.Status,
					Trigger:           lastRun.Trigger,
					Destination:       lastRun.Destination,
					LocalSnapshotID:   lastRun.LocalSnapshotID,
					RemoteSnapshotID:  lastRun.RemoteSnapshotID,
					S3DestinationID:   lastRun.S3DestinationID,
					S3DestinationName: destinations[lastRun.S3DestinationID].Name,
					PolicyID:          lastRun.PolicyID,
					Error:             lastRun.Error,
				}
			}(),
		})
	}
	return &types.SystemBackupPolicyCollection{
		Policies:          decorated,
		RecoveryKeyStored: configured,
	}, nil
}

func (s *Service) UpdatePolicies(ctx context.Context, updates []types.UpdateBackupPolicy) (*types.SystemBackupPolicyCollection, error) {
	configured, err := s.recoveryKeys.Configured(ctx)
	if err != nil {
		return nil, err
	}
	build := func(ctx context.Context, policy *types.SystemBackupPolicy, update types.UpdateBackupPolicy) error {
		normalized, normalizeErr := backup.ValidatePolicyUpdate(ctx, "system", update, s.s3Destinations)
		if normalizeErr != nil {
			return normalizeErr
		}
		if normalized.Enabled && !configured {
			return errors.New("configure a recovery key before enabling scheduled system backups")
		}
		if normalized.Enabled && !s.SupportedDatabaseProvider() {
			return errors.New("scheduled system backups require the SQLite database provider")
		}
		*policy = types.SystemBackupPolicy{ //exhaustruct:enforce
			ID:                policy.ID,
			Enabled:           normalized.Enabled,
			Schedule:          normalized.Schedule,
			RetentionCount:    normalized.RetentionCount,
			LocalEnabled:      normalized.LocalEnabled,
			S3Enabled:         normalized.S3Enabled,
			S3DestinationID:   normalized.S3DestinationID,
			S3DestinationName: policy.S3DestinationName,
			LastRun:           policy.LastRun,
		}
		return nil
	}
	if runErr := s.store.ReconcilePolicies(ctx, updates, build, s.jobs.Unregister, s.rescheduleSystemBackupPolicy); runErr != nil {
		return nil, runErr
	}
	return s.GetPolicies(ctx)
}

// SetScheduler injects the dynamic scheduler and admission gate for per-policy
// system backup jobs. Agent mode leaves them unset.
func (s *Service) SetScheduler(ctx context.Context, dynamicScheduler types.DynamicScheduler, admissionGate *runs.Admission) error {
	return s.jobs.SetScheduler(ctx, dynamicScheduler, admissionGate)
}

func (s *Service) runScheduledBackup(ctx context.Context, policyID string) (types.SchedulerOutcome, error) {
	policy, loadErr := s.store.Policy(ctx, policyID)
	if loadErr != nil {
		return types.SchedulerOutcome{}, loadErr
	}
	if policy == nil || !policy.Enabled {
		return types.SchedulerOutcome{Status: types.RunStatusSkipped}, nil
	}
	remoteErr := func() error {
		if !policy.S3Enabled {
			return nil
		}
		return s.store.CheckScheduledRemote(ctx, policy.S3DestinationID)
	}()
	remoteDisabled, disableErr := backup.DisableMissingRemote(remoteErr, policy.LocalEnabled, &policy.S3Enabled, &policy.Enabled, func(field string) (bool, error) {
		return s.store.DisablePolicyRemote(ctx, policy, field)
	})
	if disableErr != nil {
		return types.SchedulerOutcome{}, disableErr
	}
	if remoteDisabled {
		s.rescheduleSystemBackupPolicy(ctx, policy)
	}
	if remoteDisabled && !policy.LocalEnabled {
		return types.SchedulerOutcome{Status: types.RunStatusNeedsAttention, Message: backup.RemoteDisabledMessage}, nil
	}
	run, runErr := s.CreateBackup(ctx, types.SystemUser, types.SystemBackupTriggerScheduled, types.CreateSystemBackupRequest{PolicyID: policy.ID})
	if errors.Is(runErr, ErrSystemBackupAlreadyRunning) {
		slog.InfoContext(ctx, "Scheduled Arcane system backup skipped; another backup is running", "policyId", policy.ID)
		return types.SchedulerOutcome{Status: types.RunStatusSkipped, Message: "Skipped: another system backup is running"}, nil
	}
	if runErr != nil {
		slog.ErrorContext(ctx, "Scheduled Arcane system backup failed", "policyId", policy.ID, "error", runErr)
		if policy.RetentionCount > 0 {
			if retentionErr := s.applyRetention(ctx, policy.ID, policy.RetentionCount, policy.S3Enabled); retentionErr != nil {
				slog.ErrorContext(ctx, "System backup retention failed", "policyId", policy.ID, "error", retentionErr)
				return types.SchedulerOutcome{}, errors.Join(runErr, retentionErr)
			}
		}
		return types.SchedulerOutcome{}, runErr
	}
	if policy.RetentionCount > 0 {
		if retentionErr := s.applyRetention(ctx, policy.ID, policy.RetentionCount, policy.S3Enabled); retentionErr != nil {
			slog.ErrorContext(ctx, "System backup retention failed", "policyId", policy.ID, "error", retentionErr)
			return types.SchedulerOutcome{Status: types.RunStatusPartial, Message: "Backup completed but retention failed"}, retentionErr
		}
	}
	slog.InfoContext(ctx, "Scheduled Arcane system backup completed", "backupId", run.ID, "policyId", policy.ID)
	if remoteDisabled {
		return types.SchedulerOutcome{Status: types.RunStatusPartial, Message: backup.RemoteDisabledMessage}, nil
	}
	return types.SchedulerOutcome{Status: types.RunStatusSucceeded, Message: "Scheduled Arcane system backup created successfully"}, nil
}

// resumeScheduledBackup finishes the backup an interrupted scheduled run recorded, then applies retention.
func (s *Service) resumeScheduledBackup(ctx context.Context, previous types.SchedulerRun, policyID string) (types.SchedulerOutcome, error) {
	outcome, err := s.reconcileBackup(ctx, previous, policyID, "")
	if err == nil && outcome.Status != types.RunStatusSucceeded {
		return outcome, nil
	}
	current, policyErr := s.store.Policy(ctx, policyID)
	if policyErr != nil {
		return outcome, errors.Join(err, policyErr)
	}
	if err != nil {
		if current != nil && current.RetentionCount > 0 {
			if applyRetentionErr := s.applyRetention(ctx, current.ID, current.RetentionCount, current.S3Enabled); applyRetentionErr != nil {
				return outcome, errors.Join(err, applyRetentionErr)
			}
		}
		return outcome, err
	}
	if current != nil && current.RetentionCount > 0 {
		if applyRetentionErr := s.applyRetention(ctx, current.ID, current.RetentionCount, current.S3Enabled); applyRetentionErr != nil {
			return types.SchedulerOutcome{
				Status:     types.RunStatusPartial,
				Message:    outcome.Message,
				ActivityID: outcome.ActivityID,
				Targets:    outcome.Targets,
				Steps:      outcome.Steps,
			}, applyRetentionErr
		}
	}
	return outcome, nil
}

func (s *Service) rescheduleSystemBackupPolicy(ctx context.Context, policy *types.SystemBackupPolicy) {
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
				"action":          "scheduled_system_backup",
				"policyId":        policyID,
				"schedule":        policy.Schedule,
				"retentionCount":  policy.RetentionCount,
				"localEnabled":    policy.LocalEnabled,
				"s3Enabled":       policy.S3Enabled,
				"s3DestinationId": policy.S3DestinationID,
			},
		},
		ScheduleFn: func(ctx context.Context) string {
			current, err := s.store.Policy(ctx, policyID)
			if err != nil || current == nil {
				return defaultSystemBackupSchedule
			}
			return current.Schedule
		},
		FallbackFn: func(ctx context.Context, previous types.SchedulerRun) (types.SchedulerOutcome, error) {
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
	return s.settingsService.GetStringSetting(ctx, "projectsDirectory")
}

// StartBackup prepares a manual backup, then submits its workflow.
func (s *Service) StartBackup(ctx context.Context, user types.Actor, request types.CreateSystemBackupRequest) (*types.SystemBackupRun, error) {
	requester := backup.NewRequester(ctx, user, "0", authz.PermSystemBackupsManage)
	if err := s.engine.Authorize(ctx, requester); err != nil {
		return nil, err
	}
	lease, err := s.acquireRun(ctx)
	if err != nil {
		return nil, err
	}
	prepared, err := s.prepareBackup(ctx, types.SystemBackupTriggerManual, request)
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
	activityID, err := s.flow.Submit(ctx, s.backupWorkflow, manualBackupInput{
		Checkpoint: prepared.checkpoint,
		Requester:  requester,
	}, activitylib.StartRequest{
		ResourceID:    new("arcane"),
		StartedBy:     &user,
		Step:          "Creating system backup",
		LatestMessage: "Creating Arcane system backup",
		Metadata: database.JSON{
			"action":          "create_system_backup",
			"backupId":        run.ID,
			"destination":     run.Destination,
			"s3DestinationId": run.S3DestinationID,
			"policyId":        run.PolicyID,
		},
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
	return &types.SystemBackupRun{ //exhaustruct:enforce
		RemoteAvailable:   run.RemoteAvailable,
		ActivityID:        activityID,
		ID:                run.ID,
		Size:              run.Size,
		CreatedAt:         run.CreatedAt,
		Status:            run.Status,
		Trigger:           run.Trigger,
		Destination:       run.Destination,
		LocalSnapshotID:   run.LocalSnapshotID,
		RemoteSnapshotID:  run.RemoteSnapshotID,
		S3DestinationID:   run.S3DestinationID,
		S3DestinationName: run.S3DestinationName,
		PolicyID:          run.PolicyID,
		Error:             run.Error,
	}, nil
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
	if outcome.Status == types.RunStatusSucceeded {
		return
	}
	if current, loadErr := s.store.Run(ctx, runID); loadErr == nil && current.Status == types.SystemBackupStatusRunning {
		if failErr := s.store.FailRun(ctx, runID, cmp.Or(outcome.Message, "Backup did not run")); failErr != nil {
			slog.WarnContext(ctx, "Failed to close a system backup whose workflow ended early", "runId", runID, "error", failErr)
		}
	}
}

// reconcileBackup finishes the backup checkpointed under targetID from the snapshots its interrupted run left behind.
func (s *Service) reconcileBackup(ctx context.Context, previous types.SchedulerRun, targetID, suppliedKey string) (types.SchedulerOutcome, error) {
	outcome := jobcontext.ConfirmedTarget(previous, targetID)
	index := slices.IndexFunc(previous.Outcome.Targets, func(target types.TargetOutcome) bool {
		return target.ID == targetID && len(target.RecoveryData) > 0
	})
	if outcome.Status == types.RunStatusSucceeded || index < 0 {
		return outcome, nil
	}
	target := previous.Outcome.Targets[index]
	var checkpoint systemBackupRecovery
	if err := json.Unmarshal(target.RecoveryData, &checkpoint); err != nil {
		return outcome, err
	}
	confirm := func() (types.SchedulerOutcome, error) {
		confirmed := types.TargetOutcome{ //exhaustruct:enforce
			RecoveryData: target.RecoveryData,
			ResourceType: target.ResourceType,
			ID:           target.ID,
			Status:       types.RunStatusSucceeded,
			Message:      target.Message,
			ActivityID:   target.ActivityID,
		}
		if progressErr := jobcontext.Progress(ctx, confirmed); progressErr != nil {
			return outcome, progressErr
		}
		return types.SchedulerOutcome{
			Status:  types.RunStatusSucceeded,
			Targets: []types.TargetOutcome{confirmed},
		}, nil
	}
	run, err := s.store.Run(ctx, checkpoint.BackupID)
	if err != nil {
		return outcome, err
	}
	if run.Status == types.SystemBackupStatusSucceeded {
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
		if _, err = s.executeBackup(ctx, &preparedSystemBackup{
			run:         run,
			recoveryKey: key,
			checkpoint:  checkpoint,
		}); err != nil {
			return outcome, err
		}
		return confirm()
	}
	// A missing remote snapshot implies a staged one; the rerun above handled neither surviving.
	remoteSnapshotID := run.RemoteSnapshotID
	if checkpoint.S3Enabled && run.RemoteSnapshotID == "" {
		snapshot, replicateErr := s.engine.Replicate(ctx, dockerClient, local, staged.ID, remote, key, "arcane-system-recovery", backup.RunSnapshotTag(run.ID))
		if replicateErr != nil {
			return outcome, replicateErr
		}
		remoteSnapshotID = snapshot.ID
	}
	if !checkpoint.LocalEnabled && staged.ID != "" {
		if forgetErr := s.engine.ForgetSnapshots(ctx, dockerClient, local, key, []string{staged.ID}); forgetErr != nil {
			return outcome, forgetErr
		}
	}
	*run = types.SystemBackupRun{ //exhaustruct:enforce
		RemoteAvailable:   run.RemoteAvailable,
		ActivityID:        run.ActivityID,
		ID:                run.ID,
		Size:              kit.Ternary(checkpoint.LocalEnabled, staged.Size, run.Size),
		CreatedAt:         run.CreatedAt,
		Status:            types.SystemBackupStatusSucceeded,
		Trigger:           run.Trigger,
		Destination:       run.Destination,
		LocalSnapshotID:   kit.Ternary(checkpoint.LocalEnabled, staged.ID, run.LocalSnapshotID),
		RemoteSnapshotID:  remoteSnapshotID,
		S3DestinationID:   run.S3DestinationID,
		S3DestinationName: run.S3DestinationName,
		PolicyID:          run.PolicyID,
		Error:             "",
	}
	if saveErr := s.store.SaveRun(ctx, run); saveErr != nil {
		return outcome, saveErr
	}
	return confirm()
}

// observeBackupSnapshots finds the snapshots an interrupted run already wrote and records any remote one on run.
func (s *Service) observeBackupSnapshots(
	ctx context.Context, dockerClient *client.Client, previous types.SchedulerRun, run *types.SystemBackupRun, checkpoint systemBackupRecovery, key string,
) (backup.Snapshot, backup.Repository, backup.Repository, error) {
	var staged backup.Snapshot
	var local, remote backup.Repository
	targets := previous.Outcome.Targets
	stageIndex := slices.IndexFunc(targets, func(target types.TargetOutcome) bool { return target.ID == run.ID+":stage" })
	stageID := run.LocalSnapshotID
	if stageIndex >= 0 && stageID == "" && targets[stageIndex].Status == types.RunStatusSucceeded {
		stageID = targets[stageIndex].Message
	}
	remoteAttempted := slices.ContainsFunc(targets, func(target types.TargetOutcome) bool { return target.ID == run.ID+":remote" })
	findStaged := func() error {
		repository, err := s.localRepository(ctx, dockerClient, false)
		if err != nil {
			return err
		}
		local = repository
		if stageIndex < 0 && stageID == "" {
			return nil
		}
		snapshot, found, err := s.engine.FindRunSnapshot(ctx, dockerClient, local, key, run.ID, stageID)
		if found {
			staged = snapshot
		}
		return err
	}
	if checkpoint.LocalEnabled {
		if err := findStaged(); err != nil {
			return staged, local, remote, err
		}
	}
	if !checkpoint.S3Enabled {
		return staged, local, remote, nil
	}
	remote, err := s.remoteRepository(ctx, run.S3DestinationID)
	if err != nil {
		return staged, local, remote, err
	}
	if (stageIndex >= 0 && staged.ID == "") || remoteAttempted || run.RemoteSnapshotID != "" {
		snapshot, found, findErr := s.engine.FindRunSnapshot(ctx, dockerClient, remote, key, run.ID, run.RemoteSnapshotID)
		if findErr != nil {
			return staged, local, remote, findErr
		}
		if found {
			*run = types.SystemBackupRun{ //exhaustruct:enforce
				RemoteAvailable:   run.RemoteAvailable,
				ActivityID:        run.ActivityID,
				ID:                run.ID,
				Size:              cmp.Or(run.Size, snapshot.Size),
				CreatedAt:         run.CreatedAt,
				Status:            run.Status,
				Trigger:           run.Trigger,
				Destination:       run.Destination,
				LocalSnapshotID:   run.LocalSnapshotID,
				RemoteSnapshotID:  snapshot.ID,
				S3DestinationID:   run.S3DestinationID,
				S3DestinationName: run.S3DestinationName,
				PolicyID:          run.PolicyID,
				Error:             run.Error,
			}
		}
	}
	// S3-only runs snapshot straight to S3, so only runs from older versions left a local staged snapshot to recover,
	// including one tagged before its stage success was recorded.
	if !checkpoint.LocalEnabled && (remoteAttempted || ((stageIndex >= 0 || stageID != "") && run.RemoteSnapshotID == "")) {
		if err = findStaged(); err != nil {
			slog.WarnContext(ctx, "failed to find staged system recovery snapshot", "backupId", run.ID, "error", err)
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
	resuming := slices.ContainsFunc(previous.Outcome.Targets, func(target types.TargetOutcome) bool {
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
	succeeded := types.SchedulerOutcome{Status: types.RunStatusSucceeded, Message: "Arcane system backup created successfully"}
	run, err := s.store.Run(ctx, runID)
	if err != nil {
		return nil, err
	}
	if run.Status == types.SystemBackupStatusSucceeded {
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
		if outcome.Status != types.RunStatusSucceeded {
			return nil, errors.New(outcome.Message)
		}
		return succeeded, nil
	}
	checkpoint, err := json.Marshal(input.Checkpoint)
	if err != nil {
		return nil, err
	}
	if progressErr := jobcontext.Progress(ctx, types.TargetOutcome{
		ResourceType: "system_backup",
		ID:           runID,
		Status:       types.RunStatusRunning,
		RecoveryData: checkpoint,
		ActivityID:   t.ActivityID(),
	}); progressErr != nil {
		return nil, progressErr
	}
	if _, executeErr := s.executeBackup(ctx, &preparedSystemBackup{
		run:         run,
		recoveryKey: key,
		checkpoint:  input.Checkpoint,
	}); executeErr != nil {
		return nil, executeErr
	}
	return succeeded, nil
}

// ListBackupHistory returns a unified, server-paginated view of local backup records.
func (s *Service) ListBackupHistory(ctx context.Context, params pagination.QueryParams) ([]types.SystemVolumeBackupHistoryEntry, pagination.Response, error) {
	history, page, err := s.store.ListHistory(ctx, params)
	if err != nil {
		return nil, pagination.Response{}, fmt.Errorf("list backup history: %w", err)
	}
	destinations := s.s3Destinations.DestinationsByID(ctx)
	remoteAvailable := backup.RemoteSnapshotChecker(ctx, s.s3Destinations)
	instanceID := s.settingsService.GetSettingsConfig().InstanceID.Value
	decorated := make([]types.SystemVolumeBackupHistoryEntry, 0, len(history))
	for _, entry := range history {
		// Volume backups discovered from another instance live under that instance's root.
		root := func() string {
			owner := cmp.Or(entry.RemoteInstanceID, instanceID)
			switch {
			case entry.ResourceType != "volume":
				return "arcane-system-recovery"
			case owner == "":
				return ""
			default:
				return path.Join(backup.VolumeRoot, owner)
			}
		}()
		decorated = append(decorated, types.SystemVolumeBackupHistoryEntry{ //exhaustruct:enforce
			RemoteAvailable:   remoteAvailable(entry.S3DestinationID, root, entry.RemoteSnapshotID),
			ID:                entry.ID,
			Size:              entry.Size,
			CreatedAt:         entry.CreatedAt,
			Status:            entry.Status,
			Trigger:           entry.Trigger,
			Destination:       entry.Destination,
			Format:            entry.Format,
			LocalSnapshotID:   entry.LocalSnapshotID,
			RemoteSnapshotID:  entry.RemoteSnapshotID,
			RemoteInstanceID:  entry.RemoteInstanceID,
			S3DestinationID:   entry.S3DestinationID,
			S3DestinationName: destinations[entry.S3DestinationID].Name,
			PolicyID:          entry.PolicyID,
			Error:             entry.Error,
			Type:              entry.Type,
			ResourceType:      entry.ResourceType,
			ResourceName:      entry.ResourceName,
		})
	}
	return decorated, pagination.Response{
		TotalPages:      page.TotalPages,
		TotalItems:      page.TotalItems,
		CurrentPage:     page.CurrentPage,
		ItemsPerPage:    page.ItemsPerPage,
		GrandTotalItems: max(page.TotalItems, 0),
		HasNextPage:     page.HasNextPage,
	}, nil
}
