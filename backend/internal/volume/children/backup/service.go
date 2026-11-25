package backup

import (
	"cmp"
	"context"
	"encoding/json/v2"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"path"
	"slices"
	"strings"
	"time"
	"uuid"

	"github.com/containerd/errdefs"
	"github.com/italypaleale/francis/actor"
	"github.com/italypaleale/francis/builtin/workflow"
	"github.com/moby/moby/api/types/mount"
	"github.com/moby/moby/client"
	"gorm.io/gorm"

	"kit.getarcane.app"
	"kit.getarcane.app/concurrency"

	"getarcane.dev/arcane/v3/internal/backup"
	"getarcane.dev/arcane/v3/internal/common"
	"getarcane.dev/arcane/v3/internal/database"
	"getarcane.dev/arcane/v3/internal/docker"
	"getarcane.dev/arcane/v3/internal/event"
	"getarcane.dev/arcane/v3/internal/s3"
	"getarcane.dev/arcane/v3/internal/scheduler/flow"
	"getarcane.dev/arcane/v3/internal/scheduler/jobcontext"
	"getarcane.dev/arcane/v3/internal/scheduler/runs"
	"getarcane.dev/arcane/v3/internal/settings"
	"getarcane.dev/arcane/v3/internal/upload"
	userdomain "getarcane.dev/arcane/v3/internal/user"
	"getarcane.dev/arcane/v3/internal/volume/children/backup/children/browser"
	"getarcane.dev/arcane/v3/internal/volume/children/backup/children/policies"
	"getarcane.dev/arcane/v3/internal/volume/children/backup/children/restore"
	activitylib "getarcane.dev/arcane/v3/pkg/activity"
	"getarcane.dev/arcane/v3/pkg/authz"
	"getarcane.dev/arcane/v3/pkg/pagination"
	s3util "getarcane.dev/arcane/v3/pkg/s3"
	"getarcane.dev/arcane/v3/pkg/volumes"
	"getarcane.dev/types/v3"
)

type (
	Store struct {
		Create            func(ctx context.Context, entry *types.VolumeBackup) error
		Save              func(ctx context.Context, entry *types.VolumeBackup) error
		Fail              func(ctx context.Context, backupID, message string) error
		Delete            func(ctx context.Context, backupID string) error
		Run               func(ctx context.Context, backupID string) (*types.VolumeBackup, error)
		Runs              func(ctx context.Context, backupIDs []string) ([]*types.VolumeBackup, error)
		Latest            func(ctx context.Context, policyID string) (*types.VolumeBackup, error)
		List              func(ctx context.Context, volumeName string, params pagination.QueryParams) ([]types.VolumeBackup, pagination.Response, error)
		RemoteSnapshotIDs func(ctx context.Context, destinationID string) ([]string, error)
		LocalSnapshotIDs  func(ctx context.Context) ([]string, error)
		CountLocal        func(ctx context.Context) (int64, error)
	}

	ExpiredBackupDeletion struct {
		Expired       func(ctx context.Context, within []string) ([]string, error)
		IncludeRemote bool
	}

	// Dependencies are the persistence, runtime, and helper-container operations volume backups use.
	Dependencies struct {
		Store            Store
		DB               *database.DB
		Policies         *backup.Policies
		Docker           *docker.DockerClientService
		Events           *event.EventService
		Settings         *settings.SettingsService
		Engine           *backup.Engine
		S3Destinations   *s3.S3DestinationService
		RecoveryKeys     *backup.RecoveryKeyStore
		Uploads          *upload.UploadService
		Repository       *backup.VolumeRepository
		Containers       *backup.Containers
		Locks            *concurrency.KeyedMutex
		AlreadyRunning   error
		PolicyOverridden error
		HelperImage      func(ctx context.Context) (string, error)
		AcquireHelper    func(ctx context.Context, volumeName string) (string, func(), error)
		Exec             func(ctx context.Context, containerID, execUser string, cmd []string) (string, string, error)
		Ctx              context.Context
		Scheduler        types.DynamicScheduler
		Admission        *runs.Admission
	}

	Service struct {
		policies *policies.Service
		browser  *browser.Service
		restore  *restore.Service
		flow     *flow.Engine
		// backupWorkflow runs manual backups that StartBackup accepted.
		backupWorkflow *flow.Workflow
		watch          backup.RunWatch
		deps           Dependencies
	}

	// backupPlan is one run's resolved destinations; it doubles as the run's recovery checkpoint.
	backupPlan struct {
		Policy          *backup.Policy `json:"policy,omitempty"`
		BackupID        string         `json:"backupId"`
		S3DestinationID string         `json:"s3DestinationId"`
		LocalEnabled    bool           `json:"localEnabled"`
		S3Enabled       bool           `json:"s3Enabled"`
	}

	// manualBackupInput is a manual backup's workflow payload; the row and plan are frozen before it starts.
	manualBackupInput struct {
		Requester  types.BackupRequester `json:"requester"`
		VolumeName string                `json:"volumeName"`
		Checkpoint backupPlan            `json:"checkpoint"`
	}

	// lockHeld marks a context whose caller already holds the volume's workspace lock.
	lockHeld struct {
		service    *Service
		volumeName string
	}

	lockHeldKey struct{}

	unmanagedCandidate struct {
		ID         string
		VolumeName string
		PolicyID   string
		Trigger    string
	}
)

func NewService(deps Dependencies) *Service {
	s := &Service{deps: deps}
	s.policies = policies.NewService(policies.Dependencies{
		DB:             deps.DB,
		Policies:       deps.Policies,
		Docker:         deps.Docker,
		S3Destinations: deps.S3Destinations,
		Settings:       deps.Settings,
		Ctx:            deps.Ctx,
		Scheduler:      deps.Scheduler,
		Admission:      deps.Admission,
		LatestRun: func(ctx context.Context, policyID string) (*types.VolumeBackupEntry, error) {
			entry, err := s.deps.Store.Latest(ctx, policyID)
			if err != nil || entry == nil {
				return nil, err
			}
			return new(entry.Entry()), nil
		},
		CreateBackup: func(ctx context.Context, volumeName, policyID string, admitted func(context.Context) error) (*types.VolumeBackup, error) {
			return s.createBackup(ctx, volumeName, types.SystemUser, types.VolumeBackupTriggerScheduled, types.CreateVolumeBackupRequest{PolicyID: policyID}, admitted)
		},
		Reconcile: s.ReconcileBackup,
	})
	s.browser = browser.NewService(browser.Dependencies{
		Docker:     deps.Docker,
		Engine:     deps.Engine,
		Run:        deps.Store.Run,
		Repository: s.snapshotRepository,
		Password:   deps.Repository.Password,
		SanitizePath: func(input string) (string, error) {
			return kit.NormalizeRelativePath(strings.TrimLeft(strings.TrimSpace(input), "/"))
		},
		ArchiveFilename: backupArchiveFilename,
		TempContainer:   s.createBackupTempContainer,
		Exec:            deps.Exec,
	})
	s.restore = restore.NewService(restore.Dependencies{
		Docker:     deps.Docker,
		Engine:     deps.Engine,
		Events:     deps.Events,
		Locks:      deps.Locks,
		Run:        deps.Store.Run,
		Repository: s.snapshotRepository,
		Password:   deps.Repository.Password,
		Containers: deps.Containers,
		// The safety backup runs under the restore's workspace lock, so executeBackup must not take it again.
		SafetyBackup: func(ctx context.Context, volumeName string, user types.Actor) (string, error) {
			ctx = context.WithValue(ctx, lockHeldKey{}, lockHeld{service: s, volumeName: volumeName})
			request := types.CreateVolumeBackupRequest{Destination: types.VolumeBackupDestinationLocal}
			entry, err := s.createBackup(ctx, volumeName, user, types.VolumeBackupTriggerSafety, request, nil)
			if err != nil {
				return "", err
			}
			return entry.ID, nil
		},
		ArchivePaths:    s.browser.ArchivePaths,
		ArchiveFilename: backupArchiveFilename,
		HelperImage:     deps.HelperImage,
		StorageMount:    deps.Repository.StorageMount,
		HoldLocal:       deps.Repository.HoldLocal,
		TempContainer:   s.createBackupTempContainerWithMount,
		AcquireHelper:   deps.AcquireHelper,
		AcquireRun:      s.acquireVolumeRun,
		Exec:            deps.Exec,
	})
	return s
}

// RegisterWorkflows defines the manual and scheduled backup workflows while the host is still unstarted.
func (s *Service) RegisterWorkflows(engine *flow.Engine) error {
	var err error
	s.flow = engine
	s.backupWorkflow, err = engine.Define(flow.Definition{
		Name:        "volume-backup",
		Version:     1,
		Fingerprint: "8bcb88f11ff66a2b5825101d5bd5f9914f9b76bc56e5d131a82499b819bf9c4a",
		Concurrency: 4,
		Timeout:     24 * time.Hour,
		Activity:    activitylib.StartRequest{Type: types.ActivityTypeResourceAction},
		Labels:      map[string]string{"backup": "Creating backup"},
		Steps:       []workflow.StepSpec{workflow.Step("backup", engine.Handler(s.runManualBackup), workflow.WithMaxAttempts(1))},
	})
	if err != nil {
		return err
	}
	s.watch = backup.RunWatch{Flow: engine, Workflow: s.backupWorkflow, Fail: s.failRunning}
	return s.policies.RegisterWorkflows(engine)
}

func (s *Service) RegisterJobsOnStartup(ctx context.Context) {
	s.policies.RegisterJobsOnStartup(ctx)
}

func (s *Service) RemovePolicies(ctx context.Context, volumeName string) {
	s.policies.Remove(ctx, volumeName)
}

func (s *Service) List(ctx context.Context, volumeName string, params pagination.QueryParams) ([]types.VolumeBackup, pagination.Response, error) {
	slog.DebugContext(ctx, "volume service: list backups paginated", "volume", volumeName, "search", params.Search, "sort", params.Sort,
		"order", params.Order, "start", params.Start, "limit", params.Limit)
	backups, page, err := s.deps.Store.List(ctx, volumeName, params)
	if err != nil {
		return nil, pagination.Response{}, err
	}
	describe := s.deps.Repository.DescribeRemote(ctx)
	for i := range backups {
		backups[i].S3DestinationName, backups[i].RemoteAvailable = describe(backups[i].S3DestinationID, backups[i].RemoteInstanceID, backups[i].RemoteSnapshotID)
	}
	return backups, page, nil
}

func (s *Service) resolveBackupPlan(ctx context.Context, volumeName, trigger string, request types.CreateVolumeBackupRequest, policy *backup.Policy) (backupPlan, error) {
	if trigger != types.VolumeBackupTriggerSafety && policy == nil {
		var err error
		if request.PolicyID != "" {
			if policy, err = s.deps.Policies.Get(ctx, backup.PolicyVolume, volumeName, request.PolicyID); err != nil {
				return backupPlan{}, err
			}
		}
		// A scheduled run resolves under its admission, after its policy may have been disabled.
		if request.PolicyID != "" && (policy == nil || (trigger == types.VolumeBackupTriggerScheduled && !policy.Enabled)) {
			return backupPlan{}, common.ErrVolumeBackupPolicyNotFound
		}
	}
	defaults, s3DestinationID := backup.DestinationOptions{Local: true, Remote: false}, request.S3DestinationID
	if policy != nil {
		defaults, s3DestinationID = backup.DestinationOptions{Local: policy.LocalEnabled, Remote: policy.S3Enabled}, policy.S3DestinationID
	}
	destination := kit.Ternary(trigger == types.VolumeBackupTriggerSafety, types.VolumeBackupDestinationLocal, request.Destination)
	selected, s3DestinationID, err := backup.ResolveDestination(ctx, backup.DestinationRequest{
		Class:           common.ErrBadRequest,
		S3Destinations:  s.deps.S3Destinations,
		Domain:          "volume",
		Destination:     string(destination),
		S3DestinationID: s3DestinationID,
		Default:         defaults,
	})
	if err != nil {
		return backupPlan{}, err
	}
	return backupPlan{Policy: policy, S3DestinationID: s3DestinationID, LocalEnabled: selected.Local, S3Enabled: selected.Remote}, nil
}

// createBackup runs admitted, when set, once the run is admitted and before its plan resolves, so checks made there
// see the same settings the backup runs with.
func (s *Service) createBackup(
	ctx context.Context, volumeName string, user types.Actor, trigger string, request types.CreateVolumeBackupRequest, admitted func(context.Context) error,
) (*types.VolumeBackup, error) {
	trigger = cmp.Or(trigger, types.VolumeBackupTriggerManual)
	entry, plan, lease, err := s.prepareBackup(ctx, volumeName, trigger, request.PolicyID, func() (backupPlan, error) {
		if admitted != nil {
			if err := admitted(ctx); err != nil {
				return backupPlan{}, err
			}
		}
		return s.resolveBackupPlan(ctx, volumeName, trigger, request, nil)
	})
	if err != nil {
		return nil, err
	}
	defer lease.Release(ctx)
	return entry, s.completeBackup(ctx, entry, s.executeBackup(ctx, entry, user, plan))
}

// CreateSystemManagedBackup runs the existing volume backup workflow with a transient centralized policy.
func (s *Service) CreateSystemManagedBackup(ctx context.Context, volumeName string, user types.Actor, trigger, policyID string, policy types.UpdateBackupPolicy) (*types.VolumeBackup, error) {
	if !strings.HasPrefix(policyID, types.SystemVolumePolicyPrefix) {
		return nil, common.Classify(common.ErrBadRequest, errors.New("invalid system-managed volume backup policy id"))
	}
	trigger = cmp.Or(trigger, types.VolumeBackupTriggerManual)
	transient := &backup.Policy{ //exhaustruct:enforce
		BaseModel:       database.BaseModel{ID: policyID, CreatedAt: time.Time{}, UpdatedAt: nil},
		SubjectKind:     backup.PolicyVolume,
		SubjectID:       volumeName,
		Schedule:        policy.Schedule,
		S3DestinationID: policy.S3DestinationID,
		Options:         backup.PolicyOptions{Volumes: nil, SelectionMode: "", ProjectIDs: nil},
		RetentionCount:  policy.RetentionCount,
		Enabled:         true,
		StopContainers:  policy.StopContainers,
		LocalEnabled:    policy.LocalEnabled,
		S3Enabled:       policy.S3Enabled,
	}
	entry, plan, lease, err := s.prepareBackup(ctx, volumeName, trigger, policyID, func() (backupPlan, error) {
		// A volume-level policy enabled while a scheduled run waited takes precedence over it.
		if trigger == types.VolumeBackupTriggerScheduled {
			overridden, err := s.deps.Policies.HasEnabled(ctx, backup.PolicyVolume, volumeName)
			if err != nil {
				return backupPlan{}, err
			}
			if overridden {
				return backupPlan{}, s.deps.PolicyOverridden
			}
		}
		return s.resolveBackupPlan(ctx, volumeName, trigger, types.CreateVolumeBackupRequest{}, transient)
	})
	if err != nil {
		return nil, err
	}
	defer lease.Release(ctx)
	return entry, s.completeBackup(ctx, entry, s.executeBackup(ctx, entry, user, plan))
}

// prepareBackup admits the run around resolve, persists its row, and records the plan (now carrying the row ID) as the checkpoint.
func (s *Service) prepareBackup(
	ctx context.Context, volumeName, trigger, policyID string, resolve func() (backupPlan, error),
) (*types.VolumeBackup, backupPlan, *runs.Lease, error) {
	var lease *runs.Lease
	// Scheduled backups of a busy volume run in turn and resolve once admitted, so a wait never runs stale settings.
	// Other triggers fail fast, after their validation errors.
	if trigger == types.VolumeBackupTriggerScheduled {
		var err error
		if lease, err = s.deps.Engine.AwaitRun(ctx, backup.VolumeAdmissionScope, volumeName); err != nil {
			return nil, backupPlan{}, nil, err
		}
	}
	plan, err := resolve()
	if err != nil {
		lease.Release(ctx)
		return nil, backupPlan{}, nil, err
	}
	if lease == nil && trigger != types.VolumeBackupTriggerSafety {
		if lease, err = s.acquireVolumeRun(ctx, volumeName); err != nil {
			return nil, backupPlan{}, nil, err
		}
	}
	entry := &types.VolumeBackup{
		ID:              fmt.Sprintf("%s-%d-%s", volumeName, time.Now().UnixNano(), uuid.New().String()[:8]),
		VolumeName:      volumeName,
		CreatedAt:       time.Now(),
		Status:          types.VolumeBackupStatusRunning,
		Trigger:         trigger,
		Destination:     backup.Destination(backup.DestinationOptions{Local: plan.LocalEnabled, Remote: plan.S3Enabled}),
		Format:          types.VolumeBackupFormatRustic,
		S3DestinationID: plan.S3DestinationID,
		PolicyID:        policyID,
		Type:            types.BackupManagementTypeForPolicy(policyID),
	}
	if createBackupErr := s.deps.Store.Create(ctx, entry); createBackupErr != nil {
		lease.Release(ctx)
		return nil, backupPlan{}, nil, createBackupErr
	}
	plan.BackupID = entry.ID
	checkpoint, err := json.Marshal(plan)
	if err == nil {
		err = jobcontext.Progress(ctx, types.TargetOutcome{ResourceType: "volume_backup", ID: volumeName, Status: types.RunStatusRunning, RecoveryData: checkpoint})
	}
	if err != nil {
		lease.Release(ctx)
		return nil, backupPlan{}, nil, err
	}
	return entry, plan, lease, nil
}

func (s *Service) completeBackup(ctx context.Context, entry *types.VolumeBackup, err error) error {
	// A shutdown-cancelled workflow task is handed back with its row still running; any other cancel records the failure.
	if _, resumable := jobcontext.Run(ctx); resumable && errors.Is(context.Cause(ctx), context.Canceled) {
		return ctx.Err()
	}
	if err == nil && entry.Status != types.VolumeBackupStatusSucceeded {
		err = ctx.Err()
	}
	if err != nil {
		entry.Status, entry.Error = types.VolumeBackupStatusFailed, err.Error()
	} else {
		entry.Status, entry.Error = types.VolumeBackupStatusSucceeded, ""
	}
	if saveErr := s.deps.Store.Save(context.WithoutCancel(ctx), entry); saveErr != nil {
		return errors.Join(err, fmt.Errorf("failed to save volume backup result: %w", saveErr))
	}
	if err == nil {
		err = jobcontext.Progress(ctx, types.TargetOutcome{ResourceType: "volume_backup", ID: entry.VolumeName, Status: types.RunStatusSucceeded})
	}
	return err
}

func (s *Service) executeBackup(ctx context.Context, entry *types.VolumeBackup, user types.Actor, plan backupPlan) (err error) {
	defer concurrency.RecoverToError(&err, "volume backup")
	volumeName := entry.VolumeName
	if held, _ := ctx.Value(lockHeldKey{}).(lockHeld); held.service != s || held.volumeName != volumeName {
		defer s.deps.Locks.Lock(volumeName)()
	}
	if cancelErr := ctx.Err(); cancelErr != nil {
		return cancelErr
	}
	dockerClient, err := s.deps.Docker.GetClient(ctx)
	if err != nil {
		return err
	}
	if snapshotErr := s.createSnapshots(ctx, dockerClient, entry, plan, user); snapshotErr != nil {
		return snapshotErr
	}
	// Retention failures must not fail the run, or the backup would hide from ExpiredRunIDs forever.
	if entry.Trigger != types.VolumeBackupTriggerSafety && plan.Policy != nil && plan.Policy.RetentionCount > 0 &&
		!strings.HasPrefix(plan.Policy.ID, types.SystemVolumePolicyPrefix) {
		entry.Status, entry.Error = types.VolumeBackupStatusSucceeded, ""
		if saveErr := s.deps.Store.Save(context.WithoutCancel(ctx), entry); saveErr != nil {
			return fmt.Errorf("failed to save volume backup result: %w", saveErr)
		}
		expired, retentionErr := backup.ExpiredRunIDs(ctx, s.deps.DB, "volume_backups", plan.Policy.ID, plan.Policy.RetentionCount)
		expired = slices.DeleteFunc(expired, func(id string) bool { return id == entry.ID })
		if retentionErr == nil && len(expired) > 0 {
			var expiredEntries []*types.VolumeBackup
			if expiredEntries, retentionErr = s.deps.Store.Runs(ctx, expired); retentionErr == nil {
				retentionErr = s.deleteRusticBackups(ctx, expiredEntries, nil, plan.S3Enabled)
			}
		}
		if retentionErr != nil {
			slog.ErrorContext(ctx, "Volume backup retention failed", "volume", volumeName, "policy", plan.Policy.ID, "error", retentionErr)
		}
	}
	metadata := database.JSON{"action": "backup_create", "backup_id": entry.ID, "size": entry.Size, "destination": entry.Destination}
	s.logBackupEvent(ctx, event.EventTypeVolumeBackupCreate, volumeName, &user, metadata)
	if entry.Status == types.VolumeBackupStatusSucceeded {
		return nil
	}
	return ctx.Err()
}

func (s *Service) createSnapshots(ctx context.Context, dockerClient *client.Client, entry *types.VolumeBackup, plan backupPlan, user types.Actor) error {
	return s.deps.Repository.WriteRunSnapshots(ctx, dockerClient, snapshotRun(entry, plan), backup.SnapshotWrite{
		Capture: func(ctx context.Context, repository backup.Repository, password string) (backup.Snapshot, error) {
			return s.captureSnapshot(ctx, dockerClient, entry, plan, user, repository, password)
		},
		Save: func(ctx context.Context) error { return s.deps.Store.Save(ctx, entry) },
	})
}

func (s *Service) captureSnapshot(
	ctx context.Context, dockerClient *client.Client, entry *types.VolumeBackup, plan backupPlan, user types.Actor, repository backup.Repository, password string,
) (backup.Snapshot, error) {
	volumeName := entry.VolumeName
	held, releaseRepository, err := s.deps.Engine.HoldRepository(ctx, repository.ID)
	if err != nil {
		return backup.Snapshot{}, err
	}
	defer releaseRepository()
	var snapshot backup.Snapshot
	reject := func(ctx context.Context, err error) error {
		cleanupCtx, cancelCleanup := context.WithTimeout(ctx, time.Minute)
		defer cancelCleanup()
		return errors.Join(
			s.deps.Store.Fail(ctx, entry.ID, err.Error()),
			s.deps.Engine.ForgetSnapshots(cleanupCtx, dockerClient, repository, password, []string{snapshot.ID}),
		)
	}
	capture := backup.CapturePlan{
		Inspect: func(ctx context.Context, name string) (types.Volume, bool, error) {
			return backup.InspectVolume(ctx, dockerClient, name)
		},
		Stop:     nil,
		Rejected: reject,
		Mounts:   []mount.Mount{{Type: mount.TypeVolume, Source: volumeName, Target: "/volume", ReadOnly: true}},
		Volumes:  []string{volumeName},
	}
	if plan.Policy != nil && plan.Policy.StopContainers {
		capture.Stop = func(ctx context.Context) (func(context.Context) error, error) {
			stopped, stopErr := s.deps.Containers.StopContainers(ctx, backup.ContainerScope{Volumes: []string{volumeName}}, user)
			start := func(ctx context.Context) error {
				remaining, startErr := s.deps.Containers.StartContainers(ctx, stopped, user)
				if startErr == nil || len(remaining) == 0 {
					return startErr
				}
				_, retryErr := s.deps.Containers.StartContainers(ctx, remaining, user)
				return errors.Join(startErr, retryErr)
			}
			return start, stopErr
		}
	}
	err = backup.Capture(held, capture, func(ctx context.Context, mounts []mount.Mount) error {
		containerID, remove, helperErr := s.createBackupTempContainerWithMount(ctx, mounts[0])
		if helperErr != nil {
			return helperErr
		}
		ownerTag, ownerErr := backup.ReadVolumeOwnerTag(ctx, s.deps.Exec, containerID)
		remove()
		if ownerErr != nil {
			return ownerErr
		}
		created, snapshotErr := s.deps.Engine.CreateSnapshot(ctx, dockerClient, repository, password, volumeName, backup.RootSnapshotInput(mounts[0], ownerTag))
		if snapshotErr != nil {
			return fmt.Errorf("failed to create %s Rustic snapshot: %w", kit.Ternary(plan.LocalEnabled, "local", "S3"), snapshotErr)
		}
		snapshot = created
		return nil
	})
	if err != nil {
		return backup.Snapshot{}, err
	}
	confirmed, err := s.deps.Engine.ConfirmRunSnapshot(held, dockerClient, repository, password, entry.ID, snapshot.ID)
	if err != nil {
		return backup.Snapshot{}, errors.Join(err, reject(context.WithoutCancel(held), err))
	}
	return confirmed, nil
}

func (s *Service) UploadBackup(ctx context.Context, backupID, s3DestinationID string) (*types.VolumeBackup, error) {
	entry, err := s.deps.Store.Run(ctx, backupID)
	if err != nil {
		return nil, err
	}
	lease, err := s.acquireVolumeRun(ctx, entry.VolumeName)
	if err != nil {
		return nil, err
	}
	defer lease.Release(ctx)
	// Reload under the lease: a delete may have raced the first read.
	if entry, err = s.deps.Store.Run(ctx, backupID); err != nil {
		return nil, err
	}
	if entry.Status != types.VolumeBackupStatusSucceeded || entry.LocalSnapshotID == "" {
		return nil, common.ErrVolumeBackupNotUploadable
	}
	if entry.RemoteSnapshotID != "" {
		return nil, common.ErrVolumeBackupAlreadyUploaded
	}
	if strings.TrimSpace(s3DestinationID) == "" {
		return nil, common.Classify(common.ErrValidation, errors.New("select an S3 destination for the upload"))
	}
	remoteRepository, err := s.deps.Repository.Remote(ctx, s3DestinationID, "")
	if err != nil {
		return nil, err
	}
	dockerClient, err := s.deps.Docker.GetClient(ctx)
	if err != nil {
		return nil, err
	}
	localRepository, err := s.deps.Repository.Local(ctx, dockerClient, backup.LocalRepositoryOptions{ReadOnly: true})
	if err != nil {
		return nil, err
	}
	password, err := s.deps.Repository.Password(ctx, dockerClient, localRepository, remoteRepository)
	if err != nil {
		return nil, err
	}
	source, _, err := s.deps.Engine.FindRunSnapshot(ctx, dockerClient, localRepository, password, "", entry.LocalSnapshotID)
	if err != nil {
		return nil, err
	}
	snapshot, err := s.deps.Engine.Replicate(ctx, dockerClient, localRepository, entry.LocalSnapshotID, remoteRepository, password, entry.VolumeName, source.Tags...)
	if err != nil {
		return nil, fmt.Errorf("failed to upload Rustic snapshot to S3: %w", err)
	}
	entry.RemoteSnapshotID, entry.S3DestinationID, entry.Destination = snapshot.ID, s3DestinationID, types.VolumeBackupDestinationLocalS3
	if saveUploadedBackupErr := s.deps.Store.Save(ctx, entry); saveUploadedBackupErr != nil {
		return nil, fmt.Errorf("failed to save uploaded volume backup: %w", saveUploadedBackupErr)
	}
	return entry, nil
}

func (s *Service) DeleteBackup(ctx context.Context, backupID string, user *types.Actor) error {
	entry, err := s.deps.Store.Run(ctx, backupID)
	if err != nil {
		return err
	}
	unlock, locked := s.deps.Locks.TryLock(entry.VolumeName)
	if !locked {
		return s.deps.AlreadyRunning
	}
	defer unlock()
	// Deletes share the run lease so a slow operation cannot resurrect or double-forget snapshots.
	lease, err := s.acquireVolumeRun(ctx, entry.VolumeName)
	if err != nil {
		return err
	}
	defer lease.Release(ctx)
	if entry, err = s.deps.Store.Run(ctx, backupID); err != nil {
		return err
	}
	if entry.Format != types.VolumeBackupFormatArchive {
		return s.deleteRusticBackups(ctx, []*types.VolumeBackup{entry}, user, true)
	}
	return s.deleteArchiveBackup(ctx, entry, user)
}

// deleteArchiveBackup expects the caller to hold the volume's run lease.
func (s *Service) deleteArchiveBackup(ctx context.Context, entry *types.VolumeBackup, user *types.Actor) error {
	// Remove the file before the row so a failed removal stays visible and can be retried.
	if filename, filenameErr := backupArchiveFilename(entry.ID); filenameErr == nil {
		containerID, cleanup, err := s.createBackupTempContainer(ctx, "/volume", false)
		if err != nil {
			return fmt.Errorf("failed to create container for backup file cleanup: %w", err)
		}
		defer cleanup()
		if _, _, err = s.deps.Exec(ctx, containerID, "", []string{"rm", "-f", path.Join("/volume", filename)}); err != nil {
			return fmt.Errorf("failed to delete backup file: %w", err)
		}
	}
	if err := s.deps.Store.Delete(ctx, entry.ID); err != nil {
		return err
	}
	s.logBackupEvent(ctx, event.EventTypeVolumeBackupDelete, entry.VolumeName, user, database.JSON{"action": "backup_delete", "backup_id": entry.ID})
	return nil
}

func (s *Service) pruneUnmanagedBackups(ctx context.Context) error {
	days := s.deps.Settings.GetIntSetting(ctx, "unmanagedBackupRetentionDays")
	if days <= 0 {
		return nil
	}
	systemPolicies, err := s.deps.Settings.SystemVolumeBackupPolicies()
	if err != nil {
		return err
	}
	owners := []string{types.SystemVolumeManualSeriesOwner}
	for _, policy := range systemPolicies.Policies {
		owners = append(owners, policy.ID)
	}
	now := time.Now()
	return s.DeleteExpiredBackups(ctx, ExpiredBackupDeletion{
		IncludeRemote: true,
		Expired: func(ctx context.Context, within []string) ([]string, error) {
			return s.expiredUnmanagedBackupIDs(ctx, types.UnmanagedBackupSweep{
				Now:                   now,
				SystemVolumePolicyIDs: owners,
				Within:                within,
				Retention:             time.Duration(days) * 24 * time.Hour,
			})
		},
	})
}

func (s *Service) DeleteExpiredBackups(ctx context.Context, deletion ExpiredBackupDeletion) error {
	ids, err := deletion.Expired(ctx, nil)
	if err != nil || len(ids) == 0 {
		return err
	}
	entries, err := s.backupsByID(ctx, ids)
	if err != nil {
		return err
	}
	backupVolumes := make([]string, 0, len(entries))
	for _, entry := range entries {
		backupVolumes = append(backupVolumes, entry.VolumeName)
	}
	// Hold every volume's locks for the whole batch so Rustic prunes each repository once.
	held := make(map[string]func())
	defer func() {
		for _, release := range held {
			release()
		}
	}()
	var deleteErr error
	for _, volumeName := range kit.Unique(backupVolumes) {
		unlock, locked := s.deps.Locks.TryLock(volumeName)
		if !locked {
			slog.InfoContext(ctx, "Skipping backup deletion for a busy volume until the next run", "volume", volumeName)
			continue
		}
		lease, leaseErr := s.acquireVolumeRun(ctx, volumeName)
		if leaseErr != nil {
			unlock()
			if errors.Is(leaseErr, s.deps.AlreadyRunning) {
				slog.InfoContext(ctx, "Skipping backup deletion for a busy volume until the next run", "volume", volumeName)
				continue
			}
			deleteErr = errors.Join(deleteErr, leaseErr)
			continue
		}
		held[volumeName] = func() {
			lease.Release(ctx)
			unlock()
		}
	}
	if ids, err = deletion.Expired(ctx, ids); err != nil {
		return errors.Join(deleteErr, err)
	}
	if entries, err = s.backupsByID(ctx, ids); err != nil {
		return errors.Join(deleteErr, err)
	}
	var rusticEntries []*types.VolumeBackup
	deleted := 0
	for _, entry := range entries {
		if _, ok := held[entry.VolumeName]; !ok {
			continue
		}
		deleted++
		if entry.Format == types.VolumeBackupFormatArchive {
			deleteErr = errors.Join(deleteErr, s.deleteArchiveBackup(ctx, entry, nil))
			continue
		}
		rusticEntries = append(rusticEntries, entry)
	}
	if len(rusticEntries) > 0 {
		deleteErr = errors.Join(deleteErr, s.deleteRusticBackups(ctx, rusticEntries, nil, deletion.IncludeRemote))
	}
	if deleteErr == nil {
		slog.InfoContext(ctx, "Deleted expired volume backups", "backups", deleted, "volumes", len(held))
	}
	return deleteErr
}

func (s *Service) backupsByID(ctx context.Context, ids []string) ([]*types.VolumeBackup, error) {
	entries := make([]*types.VolumeBackup, 0, len(ids))
	for batch := range slices.Chunk(ids, backup.DeletionBatchSize) {
		batchEntries, err := s.deps.Store.Runs(ctx, batch)
		if err != nil {
			return nil, err
		}
		entries = append(entries, batchEntries...)
	}
	return entries, nil
}

func (s *Service) expiredUnmanagedBackupIDs(ctx context.Context, sweep types.UnmanagedBackupSweep) ([]string, error) {
	listed, err := s.deps.Docker.Call(ctx, func(ctx context.Context, dockerClient *client.Client) (client.VolumeListResult, error) {
		return dockerClient.VolumeList(ctx, client.VolumeListOptions{})
	})
	if err != nil {
		return nil, fmt.Errorf("failed to list Docker volumes: %w", err)
	}
	// A failing volume driver drops its volumes from the list; they must not look deleted.
	if len(listed.Warnings) > 0 {
		return nil, fmt.Errorf("docker volume list is incomplete: %s", strings.Join(listed.Warnings, "; "))
	}
	volumeNames := make([]string, 0, len(listed.Items))
	for _, item := range listed.Items {
		volumeNames = append(volumeNames, item.Name)
	}
	policyIDs, err := s.deps.Policies.IDs(ctx, backup.PolicyVolume)
	if err != nil {
		return nil, err
	}
	var candidates []unmanagedCandidate
	if loadErr := s.deps.DB.WithContext(ctx).Table("volume_backups").
		Select("id", "volume_name", "COALESCE(policy_id, '') AS policy_id", "trigger").
		Where("status <> ? AND trigger <> ? AND remote_instance_id = ''", types.VolumeBackupStatusRunning, types.VolumeBackupTriggerSafety).
		Find(&candidates).Error; loadErr != nil {
		return nil, fmt.Errorf("failed to load volume backups: %w", loadErr)
	}
	return backup.ExpiredUnmanagedRunIDs(ctx, s.deps.DB, "volume_backups", orphanedVolumeBackupIDs(candidates, volumeNames, policyIDs, sweep.SystemVolumePolicyIDs), sweep)
}

// Manual backups outlive their schedule, so only their volume decides.
func orphanedVolumeBackupIDs(candidates []unmanagedCandidate, volumeNames, policyIDs, systemPolicyIDs []string) []string {
	var orphaned []string
	for _, candidate := range candidates {
		if candidate.Trigger == types.VolumeBackupTriggerDiscovered {
			continue
		}
		managed := candidate.PolicyID == "" || candidate.Trigger == types.VolumeBackupTriggerManual || slices.Contains(policyIDs, candidate.PolicyID)
		if series, ok := strings.CutPrefix(candidate.PolicyID, types.SystemVolumePolicyPrefix); ok && candidate.Trigger != types.VolumeBackupTriggerManual {
			owner, _, _ := strings.Cut(series, ":")
			managed = slices.Contains(systemPolicyIDs, owner)
		}
		if !managed || !slices.Contains(volumeNames, candidate.VolumeName) {
			orphaned = append(orphaned, candidate.ID)
		}
	}
	return orphaned
}

// deleteRusticBackups forgets snapshots grouped by repository so each repository is pruned once.
func (s *Service) deleteRusticBackups(ctx context.Context, entries []*types.VolumeBackup, user *types.Actor, includeRemote bool) error {
	dockerClient, err := s.deps.Docker.GetClient(ctx)
	if err != nil {
		return err
	}
	snapshots := make([]backup.RunSnapshots, 0, len(entries))
	for _, entry := range entries {
		snapshots = append(snapshots, runSnapshots(entry))
	}
	deleteErr := s.deps.Repository.ForgetRunSnapshots(ctx, dockerClient, snapshots, includeRemote)
	for _, entry := range entries {
		if entry.LocalSnapshotID == "" && entry.RemoteSnapshotID == "" {
			if deleteBackupErr := s.deps.Store.Delete(ctx, entry.ID); deleteBackupErr != nil {
				deleteErr = errors.Join(deleteErr, fmt.Errorf("failed to delete volume backup record: %w", deleteBackupErr))
				continue
			}
			s.logBackupEvent(ctx, event.EventTypeVolumeBackupDelete, entry.VolumeName, user, database.JSON{"action": "backup_delete", "backup_id": entry.ID})
			continue
		}
		entry.Destination = backup.Destination(backup.DestinationOptions{Local: entry.LocalSnapshotID != "", Remote: entry.RemoteSnapshotID != ""})
		if saveErr := s.deps.Store.Save(ctx, entry); saveErr != nil {
			deleteErr = errors.Join(deleteErr, saveErr)
		}
	}
	return deleteErr
}

// PruneLocalRepository ages out unmanaged backups, then frees deleted local backups' data.
func (s *Service) PruneLocalRepository(ctx context.Context) error {
	return errors.Join(s.pruneUnmanagedBackups(ctx), s.pruneDeletedLocalBackups(ctx))
}

func (s *Service) pruneDeletedLocalBackups(ctx context.Context) error {
	localBackups, err := s.deps.Store.CountLocal(ctx)
	if err != nil || localBackups == 0 {
		return err
	}
	dockerClient, err := s.deps.Docker.GetClient(ctx)
	if err != nil {
		return err
	}
	// Skip before re-keying, which would start Rustic against a missing repository.
	if !s.deps.Repository.LocalInitialized(ctx, dockerClient) {
		return nil
	}
	repository, err := s.deps.Repository.Local(ctx, dockerClient, backup.LocalRepositoryOptions{})
	if err != nil {
		return err
	}
	return s.deps.Repository.Forget(ctx, dockerClient, repository, nil)
}

// DownloadBackup streams legacy archives as stored and packages local Rustic snapshots via a scratch volume.
func (s *Service) DownloadBackup(ctx context.Context, backupID string, user *types.Actor) (io.ReadCloser, int64, error) {
	entry, err := s.deps.Store.Run(ctx, backupID)
	if err != nil {
		return nil, 0, err
	}
	if entry.Format != types.VolumeBackupFormatArchive && entry.LocalSnapshotID == "" {
		return nil, 0, common.ErrVolumeBackupNotDownloadable
	}
	dockerClient, err := s.deps.Docker.GetClient(ctx)
	if err != nil {
		return nil, 0, err
	}
	var containerID, archivePath string
	var cleanup func()
	if entry.Format == types.VolumeBackupFormatArchive {
		filename, filenameErr := backupArchiveFilename(backupID)
		if filenameErr != nil {
			return nil, 0, filenameErr
		}
		archivePath = path.Join("/volume", filename)
		containerID, cleanup, err = s.createBackupTempContainer(ctx, "/volume", true)
	} else {
		archivePath = "/tmp/" + entry.ID + ".tar.gz"
		scratchVolume := "arcane-rustic-download-" + uuid.New().String()
		if _, volumeCreateErr := s.deps.Docker.Call(ctx, func(ctx context.Context, dockerClient *client.Client) (client.VolumeCreateResult, error) {
			return dockerClient.VolumeCreate(ctx, client.VolumeCreateOptions{Name: scratchVolume, Labels: volumes.Labels()})
		}); volumeCreateErr != nil {
			return nil, 0, fmt.Errorf("failed to create download scratch volume: %w", volumeCreateErr)
		}
		removeScratch := func() {
			_, _ = s.deps.Docker.Call(context.WithoutCancel(ctx), func(ctx context.Context, dockerClient *client.Client) (client.VolumeRemoveResult, error) {
				return dockerClient.VolumeRemove(ctx, scratchVolume, client.VolumeRemoveOptions{Force: true})
			})
		}
		if restoreErr := s.restore.RestoreSnapshot(ctx, dockerClient, entry, scratchVolume); restoreErr != nil {
			removeScratch()
			return nil, 0, fmt.Errorf("failed to prepare Rustic snapshot for download: %w", restoreErr)
		}
		var removeContainer func()
		if containerID, removeContainer, err = s.createBackupTempContainerWithMount(ctx, mount.Mount{Type: mount.TypeVolume, Source: scratchVolume, Target: "/volume"}); err != nil {
			removeScratch()
			return nil, 0, err
		}
		cleanup = func() {
			removeContainer()
			removeScratch()
		}
		if _, _, err = s.deps.Exec(ctx, containerID, "", []string{"tar", "-czf", archivePath, "-C", "/volume", "."}); err != nil {
			err = fmt.Errorf("failed to package Rustic snapshot for download: %w", err)
			cleanup()
		}
	}
	if err != nil {
		return nil, 0, err
	}
	reader, size, err := volumes.DownloadFileFromContainer(ctx, dockerClient, containerID, archivePath, cleanup)
	if err != nil {
		return nil, 0, err
	}
	s.logBackupEvent(ctx, event.EventTypeVolumeBackupDownload, entry.VolumeName, user, database.JSON{"action": "backup_download", "backup_id": backupID, "size": size})
	return reader, size, nil
}

func (s *Service) ExportVolumeArchive(ctx context.Context, volumeName string) (*types.UploadSession, error) {
	unlock, err := s.deps.Locks.LockContext(ctx, volumeName)
	if err != nil {
		return nil, err
	}
	defer unlock()
	if _, err = s.deps.Docker.Call(ctx, func(ctx context.Context, dockerClient *client.Client) (client.VolumeInspectResult, error) {
		return dockerClient.VolumeInspect(ctx, volumeName, client.VolumeInspectOptions{})
	}); errdefs.IsNotFound(err) {
		return nil, common.Classify(common.ErrNotFound, fmt.Errorf("volume %s not found", volumeName))
	}
	if err != nil {
		return nil, fmt.Errorf("failed to inspect volume for export: %w", err)
	}
	helperImage, err := s.deps.HelperImage(ctx)
	if err != nil {
		return nil, err
	}
	var session *types.UploadSession
	err = backup.ArchiveOutput{
		Add: func(ctx context.Context, _ mount.Mount, size int64, body io.Reader) error {
			var ingestErr error
			session, ingestErr = s.deps.Uploads.IngestSession(ctx, types.UploadKindVolumeBackup, volumeName+".tar.gz", size, body)
			if ingestErr != nil {
				return fmt.Errorf("failed to store volume archive: %w", ingestErr)
			}
			return nil
		},
		Image: helperImage,
		Parts: []mount.Mount{backup.VolumePart(volumeName)},
	}.Write(ctx, s.deps.Docker)
	if err != nil {
		return nil, fmt.Errorf("failed to package volume %s: %w", volumeName, err)
	}
	return session, nil
}

func (s *Service) RestoreUpload(ctx context.Context, volumeName string, request types.RestoreVolumeUploadRequest, user types.Actor) error {
	file, session, cleanup, err := s.deps.Uploads.Consume(ctx, types.UploadKindVolumeBackup, request.UploadID)
	if err != nil {
		return err
	}
	defer cleanup()
	return s.restore.UploadAndRestore(ctx, volumeName, file, session.Filename, request.ComposeVolume, user)
}

// RestoreFile restores one backed-up file into the volume; legacy archives extract inside a helper that mounts backup storage at /backups.
func (s *Service) RestoreFile(ctx context.Context, opts types.VolumeBackupFileRestoreOptions) (string, error) {
	entry, err := s.deps.Store.Run(ctx, opts.BackupID)
	if err != nil {
		return "", common.Classify(common.ErrVolumeWorkspaceNotFound, err)
	}
	if entry.VolumeName != opts.VolumeName {
		return "", common.Classify(common.ErrVolumeWorkspaceForbidden, errors.New("backup does not belong to volume"))
	}
	cleaned, err := kit.NormalizeRelativePath(strings.TrimLeft(strings.TrimSpace(opts.Path), "/"))
	if err != nil {
		return "", common.Classify(common.ErrVolumeWorkspaceBadRequest, err)
	}
	if entry.Format == types.VolumeBackupFormatArchive {
		filename, filenameErr := backupArchiveFilename(opts.BackupID)
		if filenameErr != nil {
			return "", common.Classify(common.ErrVolumeWorkspaceNotFound, filenameErr)
		}
		return s.restore.RestoreArchiveMembers(ctx, opts.ContainerID, filename, []string{cleaned})
	}
	dockerClient, err := s.deps.Docker.GetClient(ctx)
	if err != nil {
		return "", err
	}
	repository, snapshotID, err := s.snapshotRepository(ctx, dockerClient, entry)
	if err != nil {
		return "", err
	}
	password, err := s.deps.Repository.Password(ctx, dockerClient, repository)
	if err != nil {
		return "", err
	}
	target := mount.Mount{Type: mount.TypeVolume, Source: opts.VolumeName, Target: "/volume"}
	options := backup.RestoreOptions{SourcePath: cleaned, DestinationPath: path.Join(target.Target, cleaned)}
	if restoreErr := s.deps.Engine.RestoreSnapshot(ctx, dockerClient, repository, password, snapshotID, target, options); restoreErr != nil {
		return "", fmt.Errorf("failed to restore %s from Rustic snapshot: %w", cleaned, restoreErr)
	}
	return "", nil
}

func (s *Service) DiscoverBackups(ctx context.Context, request types.DiscoverBackupsRequest) (int, []string, error) {
	if request.Destination == types.VolumeBackupDestinationLocal {
		created, err := s.discoverLocalBackups(ctx)
		return created, nil, err
	}
	return s.discoverRemoteBackups(ctx, request.S3DestinationID)
}

func (s *Service) discoverLocalBackups(ctx context.Context) (int, error) {
	dockerClient, err := s.deps.Docker.GetClient(ctx)
	if err != nil {
		return 0, err
	}
	if !s.deps.Repository.LocalInitialized(ctx, dockerClient) {
		return 0, nil
	}
	repository, err := s.deps.Repository.Local(ctx, dockerClient, backup.LocalRepositoryOptions{ReadOnly: true})
	if err != nil {
		return 0, err
	}
	password, err := s.deps.Repository.Password(ctx, dockerClient, repository)
	if err != nil {
		return 0, err
	}
	snapshots, err := s.deps.Engine.ListSnapshots(ctx, dockerClient, repository, password)
	if errors.Is(err, backup.ErrRepositoryMissing) {
		return 0, nil
	}
	if err != nil {
		return 0, fmt.Errorf("failed to list local volume backups: %w", err)
	}
	knownIDs, err := s.deps.Store.LocalSnapshotIDs(ctx)
	if err != nil {
		return 0, err
	}
	var entries []*types.VolumeBackup
	for _, snapshot := range snapshots {
		if slices.Contains(knownIDs, snapshot.ID) {
			continue
		}
		if entry := discoveredVolumeBackup("", "", snapshot); entry != nil {
			entries = append(entries, entry)
		}
	}
	return backup.RecordDiscovered(ctx, backup.DiscoveredRecords[*types.VolumeBackup]{
		Entries: entries,
		ID: func(entry *types.VolumeBackup) string {
			return entry.ID
		},
		Existing: s.deps.Store.Runs,
		Create:   s.deps.Store.Create,
	})
}

// discoverRemoteBackups imports snapshots from every instance root on the destination; per-root failures are returned without blocking the others.
func (s *Service) discoverRemoteBackups(ctx context.Context, destinationID string) (int, []string, error) {
	key, err := s.deps.RecoveryKeys.Get(ctx, backup.RecoveryKeyConfigID)
	if errors.Is(err, backup.ErrRecoveryKeyNotConfigured) {
		return 0, nil, errors.New("configure a recovery key in system backups before discovering volume backups")
	}
	if err != nil {
		return 0, nil, err
	}
	configuration, err := s.deps.S3Destinations.Configuration(ctx, destinationID)
	if err != nil {
		return 0, nil, fmt.Errorf("the selected S3 backup destination is not configured: %w", err)
	}
	roots, err := s3util.ListRepositoryRoots(ctx, configuration, backup.VolumeRoot)
	if err != nil {
		return 0, nil, fmt.Errorf("failed to list volume backup repositories: %w", err)
	}
	knownIDs, err := s.deps.Store.RemoteSnapshotIDs(ctx, destinationID)
	if err != nil {
		return 0, nil, err
	}
	known := make(map[string]struct{}, len(knownIDs))
	for _, id := range knownIDs {
		known[id] = struct{}{}
	}
	dockerClient, err := s.deps.Docker.GetClient(ctx)
	if err != nil {
		return 0, nil, err
	}
	var entries []*types.VolumeBackup
	var failures []string
	for _, root := range roots {
		repository, repoErr := s.deps.Repository.Remote(ctx, destinationID, root)
		if repoErr != nil {
			failures = append(failures, fmt.Sprintf("instance %s: %v", root, repoErr))
			continue
		}
		snapshots, snapErr := s.deps.Engine.ListSnapshots(ctx, dockerClient, repository, key)
		if snapErr != nil {
			failures = append(failures, fmt.Sprintf("instance %s: %v", root, snapErr))
			continue
		}
		for _, snapshot := range snapshots {
			if _, exists := known[snapshot.ID]; exists {
				continue
			}
			entry := discoveredVolumeBackup(destinationID, root, snapshot)
			if entry == nil {
				continue
			}
			known[snapshot.ID] = struct{}{}
			entries = append(entries, entry)
		}
	}
	created, err := backup.RecordDiscovered(ctx, backup.DiscoveredRecords[*types.VolumeBackup]{
		Entries: entries,
		ID: func(entry *types.VolumeBackup) string {
			return entry.ID
		},
		Existing: s.deps.Store.Runs,
		Create:   s.deps.Store.Create,
	})
	if len(failures) > 0 {
		slog.WarnContext(ctx, "volume backup discovery completed with failures", "destination", destinationID, "created", created, "failedRoots", len(failures))
	}
	return created, failures, err
}

func discoveredVolumeBackup(destinationID, root string, snapshot backup.DiscoveredSnapshot) *types.VolumeBackup {
	volumeName := strings.TrimSpace(snapshot.Label)
	if volumeName == "" || volumeName == systemRecoverySnapshotLabel || slices.Contains(snapshot.Tags, backup.ProjectSnapshotTag) {
		return nil
	}
	createdAt := snapshot.Time
	if createdAt.IsZero() {
		createdAt = time.Now().UTC()
	}
	if destinationID == "" {
		return &types.VolumeBackup{
			ID:              snapshot.LocalRunID(),
			VolumeName:      volumeName,
			Size:            snapshot.Summary.TotalBytesProcessed,
			CreatedAt:       createdAt,
			Status:          types.VolumeBackupStatusSucceeded,
			Trigger:         types.VolumeBackupTriggerDiscovered,
			Destination:     types.VolumeBackupDestinationLocal,
			Format:          types.VolumeBackupFormatRustic,
			LocalSnapshotID: snapshot.ID,
		}
	}
	return &types.VolumeBackup{
		ID:         fmt.Sprintf("remote-%s-%s-%s", destinationID, root, snapshot.ID),
		VolumeName: volumeName, Size: snapshot.Summary.TotalBytesProcessed, CreatedAt: createdAt,
		Status: types.VolumeBackupStatusSucceeded, Trigger: types.VolumeBackupTriggerManual,
		Destination: types.VolumeBackupDestinationS3, Format: types.VolumeBackupFormatRustic,
		RemoteSnapshotID: snapshot.ID, S3DestinationID: destinationID, RemoteInstanceID: root,
	}
}

func (s *Service) StartBackup(ctx context.Context, environmentID, volumeName string, user types.Actor, request types.CreateVolumeBackupRequest) (types.VolumeBackupEntry, error) {
	requester := backup.NewRequester(ctx, user, environmentID, authz.PermVolumesBackup)
	entry, plan, lease, err := s.prepareBackup(ctx, volumeName, types.VolumeBackupTriggerManual, request.PolicyID, func() (backupPlan, error) {
		plan, err := s.resolveBackupPlan(ctx, volumeName, types.VolumeBackupTriggerManual, request, nil)
		if err != nil {
			return backupPlan{}, err
		}
		return plan, s.deps.Engine.Authorize(ctx, requester)
	})
	if err != nil {
		return types.VolumeBackupEntry{}, err
	}
	release := s.deps.Engine.Hold(backup.VolumeAdmissionScope, volumeName, entry.ID, lease)
	input := manualBackupInput{Checkpoint: plan, VolumeName: volumeName, Requester: requester}
	activityID, err := s.flow.Submit(ctx, s.backupWorkflow, input, activitylib.StartRequest{
		EnvironmentID: environmentID, ResourceType: new("volume"), ResourceID: new(volumeName), ResourceName: new(volumeName), StartedBy: &user,
		Step: "Creating backup", LatestMessage: "Creating volume backup",
		Metadata: database.JSON{"action": "create_volume_backup", "backupId": entry.ID, "destination": entry.Destination, "policyId": request.PolicyID, "s3DestinationId": entry.S3DestinationID},
	}, backup.AcceptedTarget(entry.ID))
	if err != nil {
		release(ctx)
		return types.VolumeBackupEntry{}, s.completeBackup(ctx, entry, err)
	}
	// A backup canceled before its task runs never takes the parked lease.
	go func() {
		background := context.WithoutCancel(ctx)
		s.watch.Watch(background, entry.ID, activityID)
		release(background)
	}()
	entry.ActivityID = &activityID
	return entry.Entry(), nil
}

// WatchAcceptedBackups restores, at startup, the watch over manual backups whose saved workflows resume.
func (s *Service) WatchAcceptedBackups(ctx context.Context) error {
	return s.watch.WatchAccepted(ctx)
}

func (s *Service) failRunning(ctx context.Context, backupID, message string) error {
	current, err := s.deps.Store.Run(ctx, backupID)
	if err != nil || current.Status != types.VolumeBackupStatusRunning {
		return nil
	}
	return s.deps.Store.Fail(ctx, backupID, message)
}

func (s *Service) ReconcileBackup(ctx context.Context, previous types.SchedulerRun, volumeName string) (types.SchedulerOutcome, error) {
	outcome := jobcontext.ConfirmedTarget(previous, volumeName)
	if outcome.Status == types.RunStatusSucceeded {
		return outcome, nil
	}
	index := slices.IndexFunc(previous.Outcome.Targets, func(target types.TargetOutcome) bool {
		return target.ID == volumeName && len(target.RecoveryData) > 0
	})
	if index < 0 {
		return outcome, nil
	}
	target := previous.Outcome.Targets[index]
	var checkpoint backupPlan
	if err := json.Unmarshal(target.RecoveryData, &checkpoint); err != nil {
		return outcome, err
	}
	entry, err := s.deps.Store.Run(ctx, checkpoint.BackupID)
	if err != nil {
		return outcome, err
	}
	// A failed backup was settled by its attempt; only an interrupted one is still running.
	if entry.Status == types.VolumeBackupStatusFailed {
		return outcome, backup.ErrBackupSettled
	}
	if entry.VolumeName != volumeName {
		return outcome, gorm.ErrRecordNotFound
	}
	if entry.Status != types.VolumeBackupStatusSucceeded {
		// Its stopped containers may have changed since, so fail rather than resume.
		if checkpoint.Policy != nil && checkpoint.Policy.StopContainers {
			return outcome, backup.FailStoppedRun(ctx, func(ctx context.Context, message string) error {
				return s.deps.Store.Fail(ctx, entry.ID, message)
			})
		}
		lease, admitted, acquireErr := s.deps.Engine.AcquireRun(ctx, backup.VolumeAdmissionScope, volumeName, "")
		if acquireErr != nil {
			return outcome, acquireErr
		}
		// Another backup of this volume is running; hand the task back without spending its attempt.
		if !admitted {
			return outcome, fmt.Errorf("%w: %w", actor.ErrJobRejected, s.deps.AlreadyRunning)
		}
		defer lease.Release(ctx)
		if resumeErr := s.resumeBackup(ctx, previous, entry, checkpoint); resumeErr != nil {
			return outcome, backup.SettleFailure(ctx, resumeErr, func(ctx context.Context, message string) error {
				return s.failRunning(ctx, entry.ID, message)
			})
		}
	}
	target.Status = types.RunStatusSucceeded
	if progressErr := jobcontext.Progress(ctx, target); progressErr != nil {
		return outcome, progressErr
	}
	return types.SchedulerOutcome{Status: types.RunStatusSucceeded, Targets: []types.TargetOutcome{target}}, nil
}

// resumeBackup adopts snapshots an interrupted run already wrote, then finishes the backup.
func (s *Service) resumeBackup(ctx context.Context, previous types.SchedulerRun, entry *types.VolumeBackup, checkpoint backupPlan) error {
	dockerClient, err := s.deps.Docker.GetClient(ctx)
	if err != nil {
		return err
	}
	if adoptErr := s.deps.Repository.AdoptRunSnapshots(ctx, dockerClient, snapshotRun(entry, checkpoint), previous); adoptErr != nil {
		return adoptErr
	}
	if saveErr := s.deps.Store.Save(ctx, entry); saveErr != nil {
		return saveErr
	}
	return s.completeBackup(ctx, entry, s.executeBackup(ctx, entry, types.SystemUser, checkpoint))
}

// runManualBackup resumes a recorded checkpoint or takes over the admission StartBackup won.
func (s *Service) runManualBackup(ctx context.Context, t flow.Task) (_ any, err error) {
	var input manualBackupInput
	if decodeErr := t.Payload(&input); decodeErr != nil {
		return nil, decodeErr
	}
	defer func() {
		err = backup.SettleFailure(ctx, err, func(ctx context.Context, message string) error {
			return s.deps.Store.Fail(ctx, input.Checkpoint.BackupID, message)
		})
	}()
	previous, _ := jobcontext.Run(ctx)
	resuming := slices.ContainsFunc(previous.Outcome.Targets, func(target types.TargetOutcome) bool {
		return target.ID == input.VolumeName && len(target.RecoveryData) > 0
	})
	if !resuming {
		lease, admitted, acquireErr := s.deps.Engine.AcquireRun(ctx, backup.VolumeAdmissionScope, input.VolumeName, input.Checkpoint.BackupID)
		if acquireErr != nil {
			return nil, acquireErr
		}
		if !admitted {
			return nil, s.deps.AlreadyRunning
		}
		defer lease.Release(ctx)
	}
	if authorizeErr := s.deps.Engine.Authorize(ctx, input.Requester); authorizeErr != nil {
		return nil, authorizeErr
	}
	succeeded := types.SchedulerOutcome{Status: types.RunStatusSucceeded, Message: "Volume backup created successfully"}
	if resuming {
		outcome, reconcileErr := s.ReconcileBackup(ctx, previous, input.VolumeName)
		// A settled row already records why it failed, so the run reports that rather than the sentinel.
		if errors.Is(reconcileErr, backup.ErrBackupSettled) {
			if entry, loadErr := s.deps.Store.Run(ctx, input.Checkpoint.BackupID); loadErr == nil && entry.Error != "" {
				return nil, fmt.Errorf("%w: %s", backup.ErrBackupSettled, entry.Error)
			}
		}
		if reconcileErr != nil {
			return nil, reconcileErr
		}
		if outcome.Status != types.RunStatusSucceeded {
			return nil, errors.New(outcome.Message)
		}
		return succeeded, nil
	}
	entry, err := s.deps.Store.Run(ctx, input.Checkpoint.BackupID)
	if err != nil {
		return nil, err
	}
	requestedBy := types.SystemUser
	if input.Requester.UserID != "" && input.Requester.UserID != "agent" {
		var localUser userdomain.User
		if loadUserErr := s.deps.DB.WithContext(ctx).Where("id = ?", input.Requester.UserID).First(&localUser).Error; loadUserErr != nil {
			return nil, loadUserErr
		}
		requestedBy = *localUser.Actor()
	}
	checkpoint, err := json.Marshal(input.Checkpoint)
	if err != nil {
		return nil, err
	}
	target := types.TargetOutcome{ResourceType: "volume_backup", ID: input.VolumeName, Status: types.RunStatusRunning, RecoveryData: checkpoint, ActivityID: t.ActivityID()}
	if progressErr := jobcontext.Progress(ctx, target); progressErr != nil {
		return nil, progressErr
	}
	err = s.executeBackup(ctx, entry, requestedBy, input.Checkpoint)
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	if completeErr := s.completeBackup(ctx, entry, err); completeErr != nil {
		return nil, completeErr
	}
	return succeeded, nil
}
