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
	"sync"
	"time"
	"uuid"

	"github.com/italypaleale/francis/actor"
	"github.com/italypaleale/francis/builtin/workflow"
	"github.com/moby/moby/api/types/container"
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
		CountLocal        func(ctx context.Context) (int64, error)
	}

	// Dependencies are the persistence, runtime, and helper-container operations volume backups use.
	Dependencies struct {
		Store            Store
		DB               *database.DB
		Docker           *docker.DockerClientService
		Events           *event.EventService
		Settings         *settings.SettingsService
		Engine           *backup.Engine
		S3Destinations   *s3.S3DestinationService
		RecoveryKeys     *backup.RecoveryKeyStore
		Locks            *concurrency.KeyedMutex
		AlreadyRunning   error
		StopContainer    func(ctx context.Context, containerID string, user types.Actor) error
		StartContainer   func(ctx context.Context, containerID string, user types.Actor) error
		HelperImage      func(ctx context.Context) (string, error)
		AcquireHelper    func(ctx context.Context, volumeName string) (string, func(), error)
		Exec             func(ctx context.Context, containerID, execUser string, cmd []string) (string, string, error)
		Ctx              context.Context
		Scheduler        types.DynamicScheduler
		Admission        *runs.Admission
		BackupVolumeName string
		EncryptionKey    string
	}

	Service struct {
		policies *policies.Service
		browser  *browser.Service
		restore  *restore.Service
		flow     *flow.Engine
		// backupWorkflow runs manual backups that StartBackup accepted.
		backupWorkflow *flow.Workflow
		deps           Dependencies
		rekeyed        sync.Map
	}

	// backupPlan is one run's resolved destinations; it doubles as the run's recovery checkpoint.
	backupPlan struct {
		Policy          *policies.VolumeBackupPolicy `json:"policy,omitempty"`
		BackupID        string                       `json:"backupId"`
		S3DestinationID string                       `json:"s3DestinationId"`
		LocalEnabled    bool                         `json:"localEnabled"`
		S3Enabled       bool                         `json:"s3Enabled"`
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

	remoteRepositoryKey struct {
		destinationID string
		instanceID    string
	}

	unmanagedCandidate struct {
		ID         string
		VolumeName string
		PolicyID   string
		Trigger    string
	}
)

func NewService(deps Dependencies) *Service {
	s := &Service{deps: deps}
	repository := func(ctx context.Context, dockerClient *client.Client, entry *types.VolumeBackup) (backup.Repository, string, error) {
		if entry.LocalSnapshotID != "" {
			local, err := s.localRusticRepository(ctx, dockerClient, true)
			return local, entry.LocalSnapshotID, err
		}
		if entry.RemoteSnapshotID != "" {
			remote, err := s.remoteRusticRepository(ctx, entry.S3DestinationID, entry.RemoteInstanceID)
			return remote, entry.RemoteSnapshotID, err
		}
		return backup.Repository{}, "", common.ErrVolumeBackupNoSnapshot
	}
	s.policies = policies.NewService(policies.Dependencies{
		DB:             deps.DB,
		S3Destinations: deps.S3Destinations,
		Settings:       deps.Settings,
		AlreadyRunning: deps.AlreadyRunning,
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
		CreateBackup: func(ctx context.Context, volumeName, policyID string) (*types.VolumeBackup, error) {
			return s.CreateBackup(ctx, volumeName, types.SystemUser, types.VolumeBackupTriggerScheduled, types.CreateVolumeBackupRequest{PolicyID: policyID})
		},
		Reconcile: s.ReconcileBackup,
	})
	s.browser = browser.NewService(browser.Dependencies{
		Docker:     deps.Docker,
		Engine:     deps.Engine,
		Run:        deps.Store.Run,
		Repository: repository,
		Password:   s.volumeBackupPassword,
		SanitizePath: func(input string) (string, error) {
			return kit.NormalizeRelativePath(strings.TrimLeft(strings.TrimSpace(input), "/"))
		},
		ArchiveFilename: backupArchiveFilename,
		TempContainer:   s.createBackupTempContainer,
		Exec:            deps.Exec,
	})
	s.restore = restore.NewService(restore.Dependencies{
		Docker:          deps.Docker,
		Engine:          deps.Engine,
		Events:          deps.Events,
		Locks:           deps.Locks,
		Run:             deps.Store.Run,
		Repository:      repository,
		Password:        s.volumeBackupPassword,
		StopContainers:  s.stopRunningContainersForBackup,
		StartContainers: s.startContainersAfterBackup,
		// The safety backup runs under the restore's workspace lock, so executeBackup must not take it again.
		SafetyBackup: func(ctx context.Context, volumeName string, user types.Actor) (string, error) {
			ctx = context.WithValue(ctx, lockHeldKey{}, lockHeld{service: s, volumeName: volumeName})
			request := types.CreateVolumeBackupRequest{Destination: types.VolumeBackupDestinationLocal}
			entry, err := s.CreateBackup(ctx, volumeName, user, types.VolumeBackupTriggerSafety, request)
			if err != nil {
				return "", err
			}
			return entry.ID, nil
		},
		ArchivePaths:    s.browser.ArchivePaths,
		ArchiveFilename: backupArchiveFilename,
		HelperImage:     deps.HelperImage,
		StorageMount:    s.StorageMount,
		AcquireHelper:   deps.AcquireHelper,
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
	return s.policies.RegisterWorkflows(engine)
}

func (s *Service) RegisterJobsOnStartup(ctx context.Context) {
	s.policies.RegisterJobsOnStartup(ctx)
}

// HasEnabledBackupPolicy reports whether a volume-level schedule takes precedence over centralized backups.
func (s *Service) HasEnabledBackupPolicy(ctx context.Context, volumeName string) (bool, error) {
	return s.policies.HasEnabledBackupPolicy(ctx, volumeName)
}

func (s *Service) RemovePolicies(ctx context.Context, volumeName string) {
	s.policies.Remove(ctx, volumeName)
}

// RenamePolicies moves a renamed volume's backup policies inside the caller's transaction.
func (s *Service) RenamePolicies(tx *gorm.DB, oldName, newName string) error {
	return s.policies.Rename(tx, oldName, newName)
}

func (s *Service) List(ctx context.Context, volumeName string, params pagination.QueryParams) ([]types.VolumeBackup, pagination.Response, error) {
	slog.DebugContext(ctx, "volume service: list backups paginated", "volume", volumeName, "search", params.Search, "sort", params.Sort,
		"order", params.Order, "start", params.Start, "limit", params.Limit)
	backups, page, err := s.deps.Store.List(ctx, volumeName, params)
	if err != nil {
		return nil, pagination.Response{}, err
	}
	if slices.ContainsFunc(backups, func(entry types.VolumeBackup) bool { return strings.TrimSpace(entry.S3DestinationID) != "" }) {
		destinations := s.deps.S3Destinations.DestinationsByID(ctx)
		for i := range backups {
			backups[i].S3DestinationName = destinations[backups[i].S3DestinationID].Name
		}
	}
	remoteAvailable := backup.RemoteSnapshotChecker(ctx, s.deps.S3Destinations)
	for i := range backups {
		// Discovered backups live under the instance that wrote them, not necessarily this one.
		root := ""
		root = path.Join(backup.VolumeRoot, cmp.Or(strings.TrimSpace(backups[i].RemoteInstanceID), strings.TrimSpace(s.deps.Settings.GetSettingsConfig().InstanceID.Value)))
		backups[i].RemoteAvailable = remoteAvailable(backups[i].S3DestinationID, root, backups[i].RemoteSnapshotID)
	}

	return backups, page, nil
}

func (s *Service) resolveBackupPlan(ctx context.Context, volumeName, trigger string, request types.CreateVolumeBackupRequest, policy *policies.VolumeBackupPolicy) (backupPlan, error) {
	destination := request.Destination
	validDestinations := []types.VolumeBackupDestination{types.VolumeBackupDestinationLocal, types.VolumeBackupDestinationS3, types.VolumeBackupDestinationLocalS3}
	if destination != "" && !slices.Contains(validDestinations, destination) {
		return backupPlan{}, common.Classify(common.ErrBadRequest, errors.New("invalid volume backup destination"))
	}
	if trigger != types.VolumeBackupTriggerSafety && policy == nil {
		var err error
		if policy, err = s.policies.Policy(ctx, volumeName, request.PolicyID); err != nil {
			return backupPlan{}, err
		}
		if request.PolicyID != "" && policy == nil {
			return backupPlan{}, common.ErrVolumeBackupPolicyNotFound
		}
	}
	plan := backupPlan{LocalEnabled: true, S3DestinationID: strings.TrimSpace(request.S3DestinationID), Policy: policy}
	if policy != nil {
		plan.LocalEnabled, plan.S3Enabled, plan.S3DestinationID = policy.LocalEnabled, policy.S3Enabled, policy.S3DestinationID
	}
	if trigger == types.VolumeBackupTriggerSafety {
		plan.LocalEnabled, plan.S3Enabled = true, false
	} else if destination != "" {
		plan.LocalEnabled = destination != types.VolumeBackupDestinationS3
		plan.S3Enabled = destination != types.VolumeBackupDestinationLocal
	}
	if !plan.LocalEnabled && !plan.S3Enabled {
		return backupPlan{}, common.Classify(common.ErrBadRequest, errors.New("select at least one volume backup destination"))
	}
	if !plan.S3Enabled {
		plan.S3DestinationID = ""
		return plan, nil
	}
	if strings.TrimSpace(plan.S3DestinationID) == "" {
		return backupPlan{}, common.Classify(common.ErrBadRequest, errors.New("select an S3 destination for the volume backup"))
	}
	if _, err := s.deps.S3Destinations.Configuration(ctx, plan.S3DestinationID); err != nil {
		return backupPlan{}, fmt.Errorf("load S3 destination: %w", err)
	}
	return plan, nil
}

func (s *Service) CreateBackup(ctx context.Context, volumeName string, user types.Actor, trigger string, request types.CreateVolumeBackupRequest) (*types.VolumeBackup, error) {
	trigger = cmp.Or(trigger, types.VolumeBackupTriggerManual)
	plan, err := s.resolveBackupPlan(ctx, volumeName, trigger, request, nil)
	if err != nil {
		return nil, err
	}
	entry, lease, err := s.prepareBackup(ctx, volumeName, trigger, request.PolicyID, &plan)
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
	transient := &policies.VolumeBackupPolicy{
		ID:              policyID,
		VolumeName:      volumeName,
		Enabled:         true,
		Schedule:        policy.Schedule,
		RetentionCount:  policy.RetentionCount,
		StopContainers:  policy.StopContainers,
		LocalEnabled:    policy.LocalEnabled,
		S3Enabled:       policy.S3Enabled,
		S3DestinationID: policy.S3DestinationID,
	}
	plan, err := s.resolveBackupPlan(ctx, volumeName, trigger, types.CreateVolumeBackupRequest{}, transient)
	if err != nil {
		return nil, err
	}
	entry, lease, err := s.prepareBackup(ctx, volumeName, trigger, policyID, &plan)
	if err != nil {
		return nil, err
	}
	defer lease.Release(ctx)
	return entry, s.completeBackup(ctx, entry, s.executeBackup(ctx, entry, user, plan))
}

// prepareBackup admits the run, persists its row, and records plan (now carrying the row ID) as the checkpoint.
func (s *Service) prepareBackup(ctx context.Context, volumeName, trigger, policyID string, plan *backupPlan) (*types.VolumeBackup, *runs.Lease, error) {
	lease, err := s.acquireVolumeRun(ctx, volumeName)
	if err != nil {
		return nil, nil, err
	}
	entry := &types.VolumeBackup{
		ID:              fmt.Sprintf("%s-%d-%s", volumeName, time.Now().UnixNano(), uuid.New().String()[:8]),
		VolumeName:      volumeName,
		CreatedAt:       time.Now(),
		Status:          types.VolumeBackupStatusRunning,
		Trigger:         trigger,
		Destination:     backupDestination(plan.LocalEnabled, plan.S3Enabled),
		Format:          types.VolumeBackupFormatRustic,
		S3DestinationID: plan.S3DestinationID,
		PolicyID:        policyID,
		Type:            types.BackupManagementTypeForPolicy(policyID),
	}
	if createBackupErr := s.deps.Store.Create(ctx, entry); createBackupErr != nil {
		lease.Release(ctx)
		return nil, nil, createBackupErr
	}
	plan.BackupID = entry.ID
	checkpoint, err := json.Marshal(*plan)
	if err == nil {
		err = jobcontext.Progress(ctx, types.TargetOutcome{ResourceType: "volume_backup", ID: volumeName, Status: types.RunStatusRunning, RecoveryData: checkpoint})
	}
	if err != nil {
		lease.Release(ctx)
		return nil, nil, err
	}
	return entry, lease, nil
}

func (s *Service) completeBackup(ctx context.Context, entry *types.VolumeBackup, err error) error {
	// A shutdown-cancelled workflow task is handed back with its row still running; any other cancel records the failure.
	if _, resumable := jobcontext.Run(ctx); resumable && errors.Is(context.Cause(ctx), context.Canceled) {
		return ctx.Err()
	}
	if err == nil {
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
	var stopped []container.Summary
	if plan.Policy != nil && plan.Policy.StopContainers {
		stopped, err = s.stopRunningContainersForBackup(ctx, volumeName, user, false)
		defer func() {
			if len(stopped) > 0 {
				_, restartErr := s.startContainersAfterBackup(context.WithoutCancel(ctx), stopped, user)
				err = errors.Join(err, restartErr)
			}
		}()
		if err != nil {
			return err
		}
	}
	if snapshotErr := s.createSnapshots(ctx, dockerClient, entry, plan); snapshotErr != nil {
		return snapshotErr
	}
	if len(stopped) > 0 {
		if stopped, err = s.startContainersAfterBackup(context.WithoutCancel(ctx), stopped, user); err != nil {
			return err
		}
	}
	// Retention failures must not fail the run, or the backup would hide from ExpiredRunIDs forever. System-managed
	// runs apply retention once for all their volumes through ApplySeriesRetention instead.
	if entry.Trigger != types.VolumeBackupTriggerSafety && plan.Policy != nil && plan.Policy.RetentionCount > 0 &&
		!strings.HasPrefix(plan.Policy.ID, types.SystemVolumePolicyPrefix) {
		expired, retentionErr := backup.ExpiredRunIDs(ctx, s.deps.DB, "volume_backups", plan.Policy.ID, backup.RunRetention{
			Succeeded: plan.Policy.RetentionCount,
			Failed:    plan.Policy.RetentionCount,
		})
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
	return ctx.Err()
}

// createSnapshots writes the plan's missing snapshots; S3 replicates the local snapshot when both are enabled.
func (s *Service) createSnapshots(ctx context.Context, dockerClient *client.Client, entry *types.VolumeBackup, plan backupPlan) error {
	source := mount.Mount{Type: mount.TypeVolume, Source: entry.VolumeName, Target: "/volume", ReadOnly: true}
	input := backup.RootSnapshotInput(source, backup.RunSnapshotTag(entry.ID))
	if plan.LocalEnabled && entry.LocalSnapshotID == "" {
		repository, err := s.localRusticRepository(ctx, dockerClient, false)
		if err != nil {
			return err
		}
		password, err := s.volumeBackupPassword(ctx, dockerClient, repository)
		if err != nil {
			return err
		}
		if progressErr := jobcontext.Progress(ctx, types.TargetOutcome{ResourceType: "backup_destination", ID: entry.ID + ":local", Status: types.RunStatusRunning}); progressErr != nil {
			return progressErr
		}
		snapshot, err := s.deps.Engine.CreateSnapshot(ctx, dockerClient, repository, password, entry.VolumeName, input)
		if err != nil {
			return fmt.Errorf("failed to create local Rustic snapshot: %w", err)
		}
		entry.LocalSnapshotID, entry.Size = snapshot.ID, snapshot.Size
		if saveErr := s.deps.Store.Save(ctx, entry); saveErr != nil {
			return saveErr
		}
		succeeded := types.TargetOutcome{ResourceType: "backup_destination", ID: entry.ID + ":local", Status: types.RunStatusSucceeded, Message: snapshot.ID}
		if progressErr := jobcontext.Progress(ctx, succeeded); progressErr != nil {
			return progressErr
		}
	}
	if !plan.S3Enabled || entry.RemoteSnapshotID != "" {
		return nil
	}
	remoteRepository, err := s.remoteRusticRepository(ctx, plan.S3DestinationID, "")
	if err != nil {
		return err
	}
	password, err := s.volumeBackupPassword(ctx, dockerClient, remoteRepository)
	if err != nil {
		return err
	}
	if progressErr := jobcontext.Progress(ctx, types.TargetOutcome{ResourceType: "backup_destination", ID: entry.ID + ":remote", Status: types.RunStatusRunning}); progressErr != nil {
		return progressErr
	}
	var snapshot backup.Snapshot
	if plan.LocalEnabled {
		localRepository, localErr := s.localRusticRepository(ctx, dockerClient, true)
		if localErr != nil {
			return localErr
		}
		snapshot, err = s.deps.Engine.Replicate(ctx, dockerClient, localRepository, entry.LocalSnapshotID, remoteRepository, password, entry.VolumeName, backup.RunSnapshotTag(entry.ID))
	} else {
		snapshot, err = s.deps.Engine.CreateSnapshot(ctx, dockerClient, remoteRepository, password, entry.VolumeName, input)
	}
	if err != nil {
		return fmt.Errorf("failed to create S3 Rustic snapshot: %w", err)
	}
	entry.RemoteSnapshotID, entry.Size = snapshot.ID, cmp.Or(entry.Size, snapshot.Size)
	if saveErr := s.deps.Store.Save(ctx, entry); saveErr != nil {
		return saveErr
	}
	return jobcontext.Progress(ctx, types.TargetOutcome{ResourceType: "backup_destination", ID: entry.ID + ":remote", Status: types.RunStatusSucceeded, Message: snapshot.ID})
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
	remoteRepository, err := s.remoteRusticRepository(ctx, s3DestinationID, "")
	if err != nil {
		return nil, err
	}
	dockerClient, err := s.deps.Docker.GetClient(ctx)
	if err != nil {
		return nil, err
	}
	localRepository, err := s.localRusticRepository(ctx, dockerClient, true)
	if err != nil {
		return nil, err
	}
	password, err := s.volumeBackupPassword(ctx, dockerClient, localRepository, remoteRepository)
	if err != nil {
		return nil, err
	}
	snapshot, err := s.deps.Engine.Replicate(ctx, dockerClient, localRepository, entry.LocalSnapshotID, remoteRepository, password, entry.VolumeName)
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
	return s.deleteExpiredBackups(ctx, true, func(ctx context.Context, within []string) ([]string, error) {
		return s.expiredUnmanagedBackupIDs(ctx, types.UnmanagedBackupSweep{
			Now:                   now,
			SystemVolumePolicyIDs: owners,
			Within:                within,
			Retention:             time.Duration(days) * 24 * time.Hour,
		})
	})
}

// ApplySeriesRetention trims every series to its newest backups in one deletion, pruning each repository once.
func (s *Service) ApplySeriesRetention(ctx context.Context, retention types.BackupSeriesRetention) error {
	if retention.Keep <= 0 || len(retention.SeriesIDs) == 0 {
		return nil
	}
	// Per-volume retention runs before its new backup is marked finished, so it keeps that backup plus Keep older
	// successful ones. Each series here just gained a successful backup, so keep one more success to match; failed
	// backups gained none and keep the same limit.
	keep := backup.RunRetention{Succeeded: retention.Keep + 1, Failed: retention.Keep}
	return s.deleteExpiredBackups(ctx, retention.IncludeRemote, func(ctx context.Context, within []string) ([]string, error) {
		var expired []string
		for _, seriesID := range retention.SeriesIDs {
			ids, err := backup.ExpiredRunIDs(ctx, s.deps.DB, "volume_backups", seriesID, keep)
			if err != nil {
				return nil, err
			}
			expired = append(expired, ids...)
		}
		if within != nil {
			locked := make(map[string]struct{}, len(within))
			for _, id := range within {
				locked[id] = struct{}{}
			}
			expired = slices.DeleteFunc(expired, func(id string) bool {
				_, ok := locked[id]
				return !ok
			})
		}
		return expired, nil
	})
}

// deleteExpiredBackups deletes the backups expired reports, rechecking that batch under the volumes' locks.
func (s *Service) deleteExpiredBackups(ctx context.Context, includeRemote bool, expired func(ctx context.Context, within []string) ([]string, error)) error {
	ids, err := expired(ctx, nil)
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
	// A restore or backup that finished first may have changed what is expired.
	if ids, err = expired(ctx, ids); err != nil {
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
		deleteErr = errors.Join(deleteErr, s.deleteRusticBackups(ctx, rusticEntries, nil, includeRemote))
	}
	slog.InfoContext(ctx, "Deleted expired volume backups", "backups", deleted, "volumes", len(held))
	return deleteErr
}

// backupsByID loads backups in bounded batches so a large deletion stays within SQL parameter limits.
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
	policyIDs, err := s.policies.IDs(ctx)
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
	var localSnapshotIDs []string
	remoteGroups := make(map[remoteRepositoryKey][]*types.VolumeBackup)
	for _, entry := range entries {
		if entry.LocalSnapshotID != "" {
			localSnapshotIDs = append(localSnapshotIDs, entry.LocalSnapshotID)
		}
		if includeRemote && entry.RemoteSnapshotID != "" {
			key := remoteRepositoryKey{
				destinationID: entry.S3DestinationID,
				instanceID:    cmp.Or(strings.TrimSpace(entry.RemoteInstanceID), strings.TrimSpace(s.deps.Settings.GetSettingsConfig().InstanceID.Value)),
			}
			remoteGroups[key] = append(remoteGroups[key], entry)
		}
	}
	var deleteErr error
	if len(localSnapshotIDs) > 0 {
		repository, repoErr := s.localRusticRepository(ctx, dockerClient, false)
		if repoErr == nil {
			repoErr = s.forgetSnapshots(ctx, dockerClient, repository, localSnapshotIDs)
		}
		if repoErr != nil {
			deleteErr = fmt.Errorf("failed to delete local Rustic snapshots: %w", repoErr)
		} else {
			for _, entry := range entries {
				entry.LocalSnapshotID = ""
			}
		}
	}
	for key, group := range remoteGroups {
		snapshotIDs := make([]string, len(group))
		for index, entry := range group {
			snapshotIDs[index] = entry.RemoteSnapshotID
		}
		repository, repoErr := s.remoteRusticRepository(ctx, key.destinationID, key.instanceID)
		var observation types.BackupRepositoryObservation
		if repoErr == nil {
			observation, repoErr = backup.CheckRemoteRepository(ctx, s.deps.S3Destinations, key.destinationID, path.Join(backup.VolumeRoot, key.instanceID))
		}
		if repoErr == nil && observation.Available {
			repoErr = s.forgetSnapshots(ctx, dockerClient, repository, snapshotIDs)
		} else if repoErr == nil {
			// Nothing remains to forget; drop the references so the entry can be removed.
			slog.WarnContext(ctx, "S3 repository is missing; dropping remote snapshot references", "destinationId", key.destinationID, "snapshots", len(snapshotIDs))
		}
		if repoErr != nil {
			deleteErr = errors.Join(deleteErr, fmt.Errorf("failed to delete S3 Rustic snapshots: %w", repoErr))
			continue
		}
		for _, entry := range group {
			entry.RemoteSnapshotID, entry.S3DestinationID = "", ""
		}
	}
	for _, entry := range entries {
		if entry.LocalSnapshotID == "" && entry.RemoteSnapshotID == "" {
			if deleteBackupErr := s.deps.Store.Delete(ctx, entry.ID); deleteBackupErr != nil {
				deleteErr = errors.Join(deleteErr, fmt.Errorf("failed to delete volume backup record: %w", deleteBackupErr))
				continue
			}
			s.logBackupEvent(ctx, event.EventTypeVolumeBackupDelete, entry.VolumeName, user, database.JSON{"action": "backup_delete", "backup_id": entry.ID})
			continue
		}
		entry.Destination = backupDestination(entry.LocalSnapshotID != "", entry.RemoteSnapshotID != "")
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
	if !s.localRusticRepositoryInitialized(ctx, dockerClient) {
		return nil
	}
	repository, err := s.localRusticRepository(ctx, dockerClient, false)
	if err != nil {
		return err
	}
	return s.forgetSnapshots(ctx, dockerClient, repository, nil)
}

func (s *Service) forgetSnapshots(ctx context.Context, dockerClient *client.Client, repository backup.Repository, snapshotIDs []string) error {
	password, err := s.volumeBackupPassword(ctx, dockerClient, repository)
	if err != nil {
		return err
	}
	return s.deps.Engine.ForgetSnapshots(ctx, dockerClient, repository, password, snapshotIDs)
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
		repository, repoErr := s.localRusticRepository(ctx, dockerClient, true)
		if repoErr != nil {
			return nil, 0, repoErr
		}
		password, passwordErr := s.volumeBackupPassword(ctx, dockerClient, repository)
		if passwordErr != nil {
			return nil, 0, passwordErr
		}
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
		scratchMount := mount.Mount{Type: mount.TypeVolume, Source: scratchVolume, Target: "/volume"}
		restoreOptions := backup.RestoreOptions{DeleteExtra: true}
		if restoreErr := s.deps.Engine.RestoreSnapshot(ctx, dockerClient, repository, password, entry.LocalSnapshotID, scratchMount, restoreOptions); restoreErr != nil {
			removeScratch()
			return nil, 0, fmt.Errorf("failed to restore Rustic snapshot for download: %w", restoreErr)
		}
		var removeContainer func()
		if containerID, removeContainer, err = s.createBackupTempContainerWithMount(ctx, scratchMount); err != nil {
			removeScratch()
			return nil, 0, err
		}
		cleanup = func() {
			removeContainer()
			removeScratch()
		}
		if _, _, err = s.deps.Exec(ctx, containerID, "", []string{"tar", "-czf", archivePath, "-C", "/volume", "."}); err != nil {
			cleanup()
			err = fmt.Errorf("failed to package Rustic snapshot for download: %w", err)
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

// ArchiveFileTarget resolves a legacy archive and the sanitized member path for a single-file restore.
func (s *Service) ArchiveFileTarget(ctx context.Context, volumeName, backupID, rel string) (string, string, error) {
	filename, err := backupArchiveFilename(backupID)
	if err != nil {
		return "", "", common.Classify(common.ErrVolumeWorkspaceNotFound, err)
	}
	entry, err := s.deps.Store.Run(ctx, backupID)
	if err != nil {
		return "", "", common.Classify(common.ErrVolumeWorkspaceNotFound, err)
	}
	if entry.VolumeName != volumeName {
		return "", "", common.Classify(common.ErrVolumeWorkspaceForbidden, errors.New("backup does not belong to volume"))
	}
	cleaned, err := kit.NormalizeRelativePath(strings.TrimLeft(strings.TrimSpace(rel), "/"))
	if err != nil {
		return "", "", common.Classify(common.ErrVolumeWorkspaceBadRequest, err)
	}
	return filename, cleaned, nil
}

// RestoreArchiveMembers extracts members of a legacy archive into a helper that mounts backup storage at /backups.
func (s *Service) RestoreArchiveMembers(ctx context.Context, containerID, filename string, cleanedPaths []string) (string, error) {
	return s.restore.RestoreArchiveMembers(ctx, containerID, filename, cleanedPaths)
}

// DiscoverRemoteBackups imports snapshots from every instance root on the destination; per-root failures are returned without blocking the others.
func (s *Service) DiscoverRemoteBackups(ctx context.Context, destinationID string) (int, []string, error) {
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
	created := 0
	var failures []string
	for _, root := range roots {
		repository, repoErr := s.remoteRusticRepository(ctx, destinationID, root)
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
			if createDiscoveredBackupErr := s.deps.Store.Create(ctx, entry); createDiscoveredBackupErr != nil {
				return created, failures, fmt.Errorf("failed to save discovered volume backup: %w", createDiscoveredBackupErr)
			}
			created++
		}
	}
	if len(failures) > 0 {
		slog.WarnContext(ctx, "volume backup discovery completed with failures", "destination", destinationID, "created", created, "failedRoots", len(failures))
	}
	return created, failures, nil
}

// discoveredVolumeBackup maps a snapshot to a backup record via its volume label; unlabeled and system snapshots map to nil.
func discoveredVolumeBackup(destinationID, root string, snapshot backup.DiscoveredSnapshot) *types.VolumeBackup {
	volumeName := strings.TrimSpace(snapshot.Label)
	if volumeName == "" || volumeName == systemRecoverySnapshotLabel {
		return nil
	}
	createdAt := snapshot.Time
	if createdAt.IsZero() {
		createdAt = time.Now().UTC()
	}
	return &types.VolumeBackup{
		ID:         fmt.Sprintf("remote-%s-%s-%s", destinationID, root, snapshot.ID),
		VolumeName: volumeName, Size: snapshot.Summary.TotalBytesProcessed, CreatedAt: createdAt,
		Status: types.VolumeBackupStatusSucceeded, Trigger: types.VolumeBackupTriggerManual,
		Destination: types.VolumeBackupDestinationS3, Format: types.VolumeBackupFormatRustic,
		RemoteSnapshotID: snapshot.ID, S3DestinationID: destinationID, RemoteInstanceID: root,
	}
}

// MigrateRepositoryPasswords re-keys this instance's own volume backup repositories to the recovery key; other instances re-key their own roots.
func (s *Service) MigrateRepositoryPasswords(ctx context.Context, previousKey, newKey string) error {
	dockerClient, err := s.deps.Docker.GetClient(ctx)
	if err != nil {
		return err
	}
	local, err := s.localRusticRepository(ctx, dockerClient, false)
	if err != nil {
		return err
	}
	repositories := []backup.Repository{local}
	destinations, listErr := s.deps.S3Destinations.ListAllS3Destinations(ctx)
	if listErr != nil {
		return fmt.Errorf("failed to list S3 destinations for re-key: %w", listErr)
	}
	for _, destination := range destinations {
		remote, remoteRusticRepositoryErr := s.remoteRusticRepository(ctx, destination.ID, "")
		if remoteRusticRepositoryErr != nil {
			return remoteRusticRepositoryErr
		}
		repositories = append(repositories, remote)
	}
	legacyPassword := kit.SHA256Hex(legacyVolumePasswordSalt + s.deps.EncryptionKey)
	if rekeyErr := s.deps.Engine.RekeyRepositories(ctx, dockerClient, repositories, newKey, previousKey, legacyPassword); rekeyErr != nil {
		return rekeyErr
	}
	for _, repository := range repositories {
		s.rekeyed.Store(repository.ID, newKey)
	}
	return nil
}

func (s *Service) StartBackup(ctx context.Context, environmentID, volumeName string, user types.Actor, request types.CreateVolumeBackupRequest) (types.VolumeBackupEntry, error) {
	plan, err := s.resolveBackupPlan(ctx, volumeName, types.VolumeBackupTriggerManual, request, nil)
	if err != nil {
		return types.VolumeBackupEntry{}, err
	}
	requester := backup.NewRequester(ctx, user, environmentID, authz.PermVolumesBackup)
	if authorizeErr := s.deps.Engine.Authorize(ctx, requester); authorizeErr != nil {
		return types.VolumeBackupEntry{}, authorizeErr
	}
	entry, lease, err := s.prepareBackup(ctx, volumeName, types.VolumeBackupTriggerManual, request.PolicyID, &plan)
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
		s.watchBackup(background, entry.ID, activityID)
		release(background)
	}()
	entry.ActivityID = &activityID
	return entry.Entry(), nil
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

// watchBackup fails the row when the workflow ends without its task completing it.
func (s *Service) watchBackup(ctx context.Context, backupID, instanceID string) {
	outcome, err := s.flow.Wait(ctx, s.backupWorkflow, instanceID)
	if err != nil || outcome.Status == types.RunStatusSucceeded {
		return
	}
	if current, loadErr := s.deps.Store.Run(ctx, backupID); loadErr == nil && current.Status == types.VolumeBackupStatusRunning {
		if failErr := s.deps.Store.Fail(ctx, backupID, cmp.Or(outcome.Message, "Backup did not run")); failErr != nil {
			slog.WarnContext(ctx, "Failed to close a volume backup whose workflow ended early", "backupId", backupID, "error", failErr)
		}
	}
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
			if failErr := s.deps.Store.Fail(ctx, entry.ID, "Backup was interrupted while its containers were stopped"); failErr != nil {
				return outcome, failErr
			}
			return outcome, backup.ErrBackupSettled
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
			return outcome, resumeErr
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
	var local, remote backup.Repository
	var repositories []backup.Repository
	if checkpoint.LocalEnabled {
		if local, err = s.localRusticRepository(ctx, dockerClient, false); err != nil {
			return err
		}
		repositories = append(repositories, local)
	}
	if checkpoint.S3Enabled {
		if remote, err = s.remoteRusticRepository(ctx, checkpoint.S3DestinationID, ""); err != nil {
			return err
		}
		repositories = append(repositories, remote)
	}
	password, err := s.volumeBackupPassword(ctx, dockerClient, repositories...)
	if err != nil {
		return err
	}
	attempted := func(destination string) bool {
		return slices.ContainsFunc(previous.Outcome.Targets, func(target types.TargetOutcome) bool { return target.ID == entry.ID+":"+destination })
	}
	if checkpoint.LocalEnabled && (entry.LocalSnapshotID != "" || attempted("local")) {
		snapshot, found, findErr := s.deps.Engine.FindRunSnapshot(ctx, dockerClient, local, password, entry.ID, entry.LocalSnapshotID)
		if findErr != nil {
			return findErr
		}
		if found {
			entry.LocalSnapshotID, entry.Size = snapshot.ID, snapshot.Size
		}
	}
	if checkpoint.S3Enabled && (entry.RemoteSnapshotID != "" || attempted("remote")) {
		snapshot, found, findErr := s.deps.Engine.FindRunSnapshot(ctx, dockerClient, remote, password, entry.ID, entry.RemoteSnapshotID)
		if findErr != nil {
			return findErr
		}
		if found {
			entry.RemoteSnapshotID, entry.Size = snapshot.ID, cmp.Or(entry.Size, snapshot.Size)
		}
	}
	if checkpoint.LocalEnabled && entry.LocalSnapshotID == "" && entry.RemoteSnapshotID != "" {
		snapshot, replicateErr := s.deps.Engine.Replicate(ctx, dockerClient, remote, entry.RemoteSnapshotID, local, password, entry.VolumeName, backup.RunSnapshotTag(entry.ID))
		if replicateErr != nil {
			return replicateErr
		}
		entry.LocalSnapshotID = snapshot.ID
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
		if err != nil && ctx.Err() == nil && !errors.Is(err, actor.ErrJobRejected) && !errors.Is(err, backup.ErrBackupSettled) {
			err = errors.Join(err, s.deps.Store.Fail(ctx, input.Checkpoint.BackupID, err.Error()))
		}
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
