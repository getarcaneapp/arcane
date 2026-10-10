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

	activitytypes "github.com/getarcaneapp/arcane/types/v2/activity"
	backuptypes "github.com/getarcaneapp/arcane/types/v2/backup"
	"github.com/getarcaneapp/arcane/types/v2/scheduler"
	usertypes "github.com/getarcaneapp/arcane/types/v2/user"
	"github.com/getarcaneapp/arcane/types/v2/volume"
	"github.com/italypaleale/francis/actor"
	"github.com/italypaleale/francis/builtin/workflow"
	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/api/types/mount"
	"github.com/moby/moby/client"
	kit "go.getarcane.app/kit/pkg"
	"gorm.io/gorm"

	"github.com/getarcaneapp/arcane/backend/v2/internal/backup"
	"github.com/getarcaneapp/arcane/backend/v2/internal/common"
	"github.com/getarcaneapp/arcane/backend/v2/internal/database"
	"github.com/getarcaneapp/arcane/backend/v2/internal/docker"
	"github.com/getarcaneapp/arcane/backend/v2/internal/event"
	"github.com/getarcaneapp/arcane/backend/v2/internal/s3"
	"github.com/getarcaneapp/arcane/backend/v2/internal/settings"
	userdomain "github.com/getarcaneapp/arcane/backend/v2/internal/user"
	"github.com/getarcaneapp/arcane/backend/v2/internal/volume/children/backup/children/browser"
	"github.com/getarcaneapp/arcane/backend/v2/internal/volume/children/backup/children/policies"
	"github.com/getarcaneapp/arcane/backend/v2/internal/volume/children/backup/children/restore"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/authz"
	activitylib "github.com/getarcaneapp/arcane/backend/v2/pkg/libarcane/activity"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/libarcane/volumehelper"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/pagination"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/scheduler/flow"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/scheduler/jobcontext"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/scheduler/runs"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/utils"
	s3utils "github.com/getarcaneapp/arcane/backend/v2/pkg/utils/s3"
)

// Store persists volume backup records for this feature.
type Store struct {
	Create            func(ctx context.Context, entry *volume.Backup) error
	Save              func(ctx context.Context, entry *volume.Backup) error
	Fail              func(ctx context.Context, backupID, message string) error
	Delete            func(ctx context.Context, backupID string) error
	Run               func(ctx context.Context, backupID string) (*volume.Backup, error)
	Runs              func(ctx context.Context, backupIDs []string) ([]*volume.Backup, error)
	Latest            func(ctx context.Context, policyID string) (*volume.Backup, error)
	List              func(ctx context.Context, volumeName string, params pagination.QueryParams) ([]volume.Backup, int64, error)
	RemoteSnapshotIDs func(ctx context.Context, destinationID string) ([]string, error)
}

// Dependencies are the persistence, runtime, and helper-container operations volume backups use.
type Dependencies struct {
	Store            Store
	DB               *database.DB
	Docker           *docker.DockerClientService
	Events           *event.EventService
	Settings         *settings.SettingsService
	Engine           *backup.Engine
	S3Destinations   *s3.S3DestinationService
	RecoveryKeys     *backup.RecoveryKeyStore
	Locks            *utils.KeyedMutex
	AlreadyRunning   error
	StopContainer    func(ctx context.Context, containerID string, user usertypes.Actor) error
	StartContainer   func(ctx context.Context, containerID string, user usertypes.Actor) error
	HelperImage      func(ctx context.Context, dockerClient *client.Client) (string, error)
	AcquireHelper    func(ctx context.Context, volumeName string) (string, func(), error)
	Exec             func(ctx context.Context, containerID, execUser string, cmd []string) (string, string, error)
	BackupVolumeName string
	EncryptionKey    string
}

// Service runs volume backups and owns their policies, file browser, and restores.
type Service struct {
	deps     Dependencies
	rekeyed  sync.Map
	policies *policies.Service
	browser  *browser.Service
	restore  *restore.Service
	flow     *flow.Engine
	// backupWorkflow runs manual backups that StartBackup accepted.
	backupWorkflow *flow.Workflow
}

// backupPlan is one run's resolved destinations; it doubles as the run's recovery checkpoint.
type backupPlan struct {
	BackupID        string                       `json:"backupId"`
	LocalEnabled    bool                         `json:"localEnabled"`
	S3Enabled       bool                         `json:"s3Enabled"`
	S3DestinationID string                       `json:"s3DestinationId"`
	Policy          *policies.VolumeBackupPolicy `json:"policy,omitempty"`
}

// manualBackupInput is a manual backup's workflow payload; the row and plan are frozen before it starts.
type manualBackupInput struct {
	Checkpoint backupPlan            `json:"checkpoint"`
	VolumeName string                `json:"volumeName"`
	Requester  backuptypes.Requester `json:"requester"`
}

// lockHeld marks a context whose caller already holds the volume's workspace lock.
type lockHeld struct {
	service    *Service
	volumeName string
}

type lockHeldKey struct{}

type remoteRepositoryKey struct {
	destinationID string
	instanceID    string
}

func NewService(deps Dependencies) *Service {
	s := &Service{deps: deps}
	repository := func(ctx context.Context, dockerClient *client.Client, entry *volume.Backup) (backup.Repository, string, error) {
		if entry.LocalSnapshotID != "" {
			local, err := s.localRusticRepository(ctx, dockerClient, true)
			return local, entry.LocalSnapshotID, err
		}
		if entry.RemoteSnapshotID != "" {
			remote, err := s.remoteRusticRepository(ctx, entry.S3DestinationID, entry.RemoteInstanceID)
			return remote, entry.RemoteSnapshotID, err
		}
		return backup.Repository{}, "", errors.New("volume backup has no Rustic snapshot")
	}
	s.policies = policies.NewService(policies.Dependencies{
		DB:             deps.DB,
		S3Destinations: deps.S3Destinations,
		Settings:       deps.Settings,
		AlreadyRunning: deps.AlreadyRunning,
		LatestRun: func(ctx context.Context, policyID string) (*volume.BackupEntry, error) {
			entry, err := s.deps.Store.Latest(ctx, policyID)
			if err != nil || entry == nil {
				return nil, err
			}
			return new(entry.Entry()), nil
		},
		CreateBackup: func(ctx context.Context, volumeName, policyID string) (*volume.Backup, error) {
			return s.CreateBackup(ctx, volumeName, usertypes.SystemUser, backuptypes.VolumeBackupTriggerScheduled, volume.CreateBackupRequest{PolicyID: policyID})
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
		SafetyBackup: func(ctx context.Context, volumeName string, user usertypes.Actor) (string, error) {
			ctx = context.WithValue(ctx, lockHeldKey{}, lockHeld{service: s, volumeName: volumeName})
			request := volume.CreateBackupRequest{Destination: volume.BackupDestinationLocal}
			entry, err := s.CreateBackup(ctx, volumeName, user, backuptypes.VolumeBackupTriggerSafety, request)
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
		Activity:    activitylib.StartRequest{Type: activitytypes.TypeResourceAction},
		Labels:      map[string]string{"backup": "Creating backup"},
		Steps:       []workflow.StepSpec{workflow.Step("backup", engine.Handler(s.runManualBackup), workflow.WithMaxAttempts(1))},
	})
	if err != nil {
		return err
	}
	return s.policies.RegisterWorkflows(engine)
}

// SetScheduler injects the dynamic scheduler and admission gate for per-policy
// backup jobs. Agent mode passes them too: agents run their own volume backups.
func (s *Service) SetScheduler(ctx context.Context, dynamicScheduler scheduler.DynamicScheduler, admissionGate *runs.Admission) error {
	return s.policies.SetScheduler(ctx, dynamicScheduler, admissionGate)
}

func (s *Service) RegisterJobsOnStartup(ctx context.Context) {
	s.policies.RegisterJobsOnStartup(ctx)
}

// HasEnabledBackupPolicy reports whether a volume-level schedule takes precedence over centralized backups.
func (s *Service) HasEnabledBackupPolicy(ctx context.Context, volumeName string) (bool, error) {
	return s.policies.HasEnabledBackupPolicy(ctx, volumeName)
}

// RemovePolicies drops the backup schedules of a removed volume.
func (s *Service) RemovePolicies(ctx context.Context, volumeName string) {
	s.policies.Remove(ctx, volumeName)
}

// RenamePolicies moves a renamed volume's backup policies inside the caller's transaction.
func (s *Service) RenamePolicies(tx *gorm.DB, oldName, newName string) error {
	return s.policies.Rename(tx, oldName, newName)
}

// backupEntry converts a backup row to its API entry for policy last runs and accepted manual starts.
func (s *Service) List(ctx context.Context, volumeName string, params pagination.QueryParams) ([]volume.Backup, pagination.Response, error) {
	slog.DebugContext(ctx, "volume service: list backups paginated", "volume", volumeName, "search", params.Search, "sort", params.Sort,
		"order", params.Order, "start", params.Start, "limit", params.Limit)
	backups, totalItems, err := s.deps.Store.List(ctx, volumeName, params)
	if err != nil {
		return nil, pagination.Response{}, err
	}
	if slices.ContainsFunc(backups, func(entry volume.Backup) bool { return strings.TrimSpace(entry.S3DestinationID) != "" }) {
		destinations := s.deps.S3Destinations.DestinationsByID(ctx)
		for i := range backups {
			backups[i].S3DestinationName = destinations[backups[i].S3DestinationID].Name
		}
	}
	remoteAvailable := backup.RemoteSnapshotChecker(ctx, s.deps.S3Destinations)
	for i := range backups {
		// Discovered backups live under the instance that wrote them, not necessarily this one.
		root := ""
		if s.deps.Settings != nil {
			root = path.Join(backup.VolumeRoot, s.remoteInstanceID(backups[i].RemoteInstanceID))
		}
		backups[i].RemoteAvailable = remoteAvailable(backups[i].S3DestinationID, root, backups[i].RemoteSnapshotID)
	}

	return backups, pagination.BuildResponse(totalItems, totalItems, params), nil
}

func (s *Service) resolveBackupPlan(ctx context.Context, volumeName, trigger string, request volume.CreateBackupRequest, policy *policies.VolumeBackupPolicy) (backupPlan, error) {
	destination := request.Destination
	validDestinations := []volume.BackupDestination{volume.BackupDestinationLocal, volume.BackupDestinationS3, volume.BackupDestinationLocalS3}
	if destination != "" && !slices.Contains(validDestinations, destination) {
		return backupPlan{}, errors.New("invalid volume backup destination")
	}
	if trigger != backuptypes.VolumeBackupTriggerSafety && policy == nil {
		var err error
		if policy, err = s.policies.Policy(ctx, volumeName, request.PolicyID); err != nil {
			return backupPlan{}, err
		}
		if request.PolicyID != "" && policy == nil {
			return backupPlan{}, errors.New("volume backup policy not found")
		}
	}
	plan := backupPlan{LocalEnabled: true, S3DestinationID: strings.TrimSpace(request.S3DestinationID), Policy: policy}
	if policy != nil {
		plan.LocalEnabled, plan.S3Enabled, plan.S3DestinationID = policy.LocalEnabled, policy.S3Enabled, policy.S3DestinationID
	}
	if trigger == backuptypes.VolumeBackupTriggerSafety {
		plan.LocalEnabled, plan.S3Enabled = true, false
	} else if destination != "" {
		plan.LocalEnabled = destination != volume.BackupDestinationS3
		plan.S3Enabled = destination != volume.BackupDestinationLocal
	}
	if !plan.LocalEnabled && !plan.S3Enabled {
		return backupPlan{}, errors.New("select at least one volume backup destination")
	}
	if !plan.S3Enabled {
		plan.S3DestinationID = ""
		return plan, nil
	}
	if strings.TrimSpace(plan.S3DestinationID) == "" {
		return backupPlan{}, errors.New("select an S3 destination for the volume backup")
	}
	if s.deps.S3Destinations == nil {
		return backupPlan{}, errors.New("S3 backup destinations are unavailable")
	}
	if _, err := s.deps.S3Destinations.Configuration(ctx, plan.S3DestinationID); err != nil {
		return backupPlan{}, errors.New("select a valid S3 destination for the volume backup")
	}
	return plan, nil
}

func (s *Service) CreateBackup(ctx context.Context, volumeName string, user usertypes.Actor, trigger string, request volume.CreateBackupRequest) (*volume.Backup, error) {
	trigger = cmp.Or(trigger, backuptypes.VolumeBackupTriggerManual)
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
func (s *Service) CreateSystemManagedBackup(ctx context.Context, volumeName string, user usertypes.Actor, trigger, policyID string, policy backuptypes.UpdateBackupPolicy) (*volume.Backup, error) {
	if !strings.HasPrefix(policyID, backuptypes.SystemVolumePolicyPrefix) {
		return nil, errors.New("invalid system-managed volume backup policy id")
	}
	trigger = cmp.Or(trigger, backuptypes.VolumeBackupTriggerManual)
	transient := &policies.VolumeBackupPolicy{
		VolumeName: volumeName, Enabled: true, Schedule: policy.Schedule, RetentionCount: policy.RetentionCount,
		StopContainers: policy.StopContainers, LocalEnabled: policy.LocalEnabled, S3Enabled: policy.S3Enabled,
		S3DestinationID: policy.S3DestinationID,
	}
	transient.ID = policyID
	plan, err := s.resolveBackupPlan(ctx, volumeName, trigger, volume.CreateBackupRequest{}, transient)
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
func (s *Service) prepareBackup(ctx context.Context, volumeName, trigger, policyID string, plan *backupPlan) (*volume.Backup, *runs.Lease, error) {
	lease, err := s.acquireVolumeRun(ctx, volumeName)
	if err != nil {
		return nil, nil, err
	}
	entry := &volume.Backup{
		VolumeName: volumeName, CreatedAt: time.Now(), Status: backuptypes.VolumeBackupStatusRunning,
		Trigger: trigger, Destination: backupDestination(plan.LocalEnabled, plan.S3Enabled), Format: volume.BackupFormatRustic,
		S3DestinationID: plan.S3DestinationID, PolicyID: policyID,
		Type: backuptypes.ManagementTypeForPolicy(policyID),
	}
	entry.ID = fmt.Sprintf("%s-%d-%s", volumeName, time.Now().UnixNano(), uuid.New().String()[:8])
	if createBackupErr := s.deps.Store.Create(ctx, entry); createBackupErr != nil {
		lease.Release(ctx)
		return nil, nil, createBackupErr
	}
	plan.BackupID = entry.ID
	checkpoint, err := json.Marshal(*plan)
	if err == nil {
		err = jobcontext.Progress(ctx, scheduler.TargetOutcome{ResourceType: "volume_backup", ID: volumeName, Status: scheduler.Running, RecoveryData: checkpoint})
	}
	if err != nil {
		lease.Release(ctx)
		return nil, nil, err
	}
	return entry, lease, nil
}

func (s *Service) completeBackup(ctx context.Context, entry *volume.Backup, err error) error {
	// A workflow task stopped by shutdown, a plain cancel, is handed back, so its row stays running for the resumed
	// task to finish; a user cancel, a workflow that ended or timed out, or work outside a workflow records the failure.
	if _, resumable := jobcontext.Run(ctx); resumable && errors.Is(context.Cause(ctx), context.Canceled) {
		return ctx.Err()
	}
	if err == nil {
		err = ctx.Err()
	}
	if err != nil {
		entry.Status, entry.Error = backuptypes.VolumeBackupStatusFailed, err.Error()
	} else {
		entry.Status, entry.Error = backuptypes.VolumeBackupStatusSucceeded, ""
	}
	if saveErr := s.deps.Store.Save(context.WithoutCancel(ctx), entry); saveErr != nil {
		return errors.Join(err, fmt.Errorf("failed to save volume backup result: %w", saveErr))
	}
	if err == nil {
		err = jobcontext.Progress(ctx, scheduler.TargetOutcome{ResourceType: "volume_backup", ID: entry.VolumeName, Status: scheduler.Succeeded})
	}
	return err
}

func (s *Service) executeBackup(ctx context.Context, entry *volume.Backup, user usertypes.Actor, plan backupPlan) (err error) {
	defer utils.RecoverToError(&err, "volume backup")
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
	// stopped holds the containers still awaiting a restart.
	var stopped []container.Summary
	if plan.Policy != nil && plan.Policy.StopContainers {
		stopped, err = s.stopRunningContainersForBackup(ctx, dockerClient, volumeName, user, false)
		defer func() {
			if len(stopped) > 0 {
				_, restartErr := s.startContainersAfterBackup(context.WithoutCancel(ctx), dockerClient, stopped, user)
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
		if stopped, err = s.startContainersAfterBackup(context.WithoutCancel(ctx), dockerClient, stopped, user); err != nil {
			return err
		}
	}
	// Retention failures must not fail the run: the status save would mark the
	// succeeded backup failed and hide it from ExpiredRunIDs forever.
	if entry.Trigger != backuptypes.VolumeBackupTriggerSafety && plan.Policy != nil && plan.Policy.RetentionCount > 0 {
		expired, retentionErr := backup.ExpiredRunIDs(ctx, s.deps.DB, "volume_backups", plan.Policy.ID, plan.Policy.RetentionCount)
		if retentionErr == nil && len(expired) > 0 {
			var expiredEntries []*volume.Backup
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
func (s *Service) createSnapshots(ctx context.Context, dockerClient *client.Client, entry *volume.Backup, plan backupPlan) error {
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
		if progressErr := jobcontext.Progress(ctx, scheduler.TargetOutcome{ResourceType: "backup_destination", ID: entry.ID + ":local", Status: scheduler.Running}); progressErr != nil {
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
		succeeded := scheduler.TargetOutcome{ResourceType: "backup_destination", ID: entry.ID + ":local", Status: scheduler.Succeeded, Message: snapshot.ID}
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
	if progressErr := jobcontext.Progress(ctx, scheduler.TargetOutcome{ResourceType: "backup_destination", ID: entry.ID + ":remote", Status: scheduler.Running}); progressErr != nil {
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
	return jobcontext.Progress(ctx, scheduler.TargetOutcome{ResourceType: "backup_destination", ID: entry.ID + ":remote", Status: scheduler.Succeeded, Message: snapshot.ID})
}

func (s *Service) UploadBackup(ctx context.Context, backupID, s3DestinationID string) (*volume.Backup, error) {
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
	if entry.Status != backuptypes.VolumeBackupStatusSucceeded || entry.LocalSnapshotID == "" {
		return nil, errors.New("only successful local volume backups can be uploaded")
	}
	if entry.RemoteSnapshotID != "" {
		return nil, errors.New("volume backup has already been uploaded")
	}
	if strings.TrimSpace(s3DestinationID) == "" {
		return nil, errors.New("select an S3 destination for the upload")
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
	entry.RemoteSnapshotID, entry.S3DestinationID, entry.Destination = snapshot.ID, s3DestinationID, volume.BackupDestinationLocalS3
	if saveUploadedBackupErr := s.deps.Store.Save(ctx, entry); saveUploadedBackupErr != nil {
		return nil, fmt.Errorf("failed to save uploaded volume backup: %w", saveUploadedBackupErr)
	}
	return entry, nil
}

func (s *Service) DeleteBackup(ctx context.Context, backupID string, user *usertypes.Actor) error {
	entry, err := s.deps.Store.Run(ctx, backupID)
	if err != nil {
		return err
	}
	// Deletes share the volume's run lease with create/restore/upload so a slow
	// operation cannot resurrect or double-forget snapshots.
	lease, err := s.acquireVolumeRun(ctx, entry.VolumeName)
	if err != nil {
		return err
	}
	defer lease.Release(ctx)
	if entry, err = s.deps.Store.Run(ctx, backupID); err != nil {
		return err
	}
	if entry.Format != volume.BackupFormatArchive {
		return s.deleteRusticBackups(ctx, []*volume.Backup{entry}, user, true)
	}
	// Delete the row first: a failed file removal then only leaves an orphan file.
	if deleteErr := s.deps.Store.Delete(ctx, backupID); deleteErr != nil {
		return deleteErr
	}
	containerID, cleanup, err := s.createBackupTempContainer(ctx, nil, "/volume", false)
	if err != nil {
		slog.WarnContext(ctx, "failed to create container for backup file cleanup", "backupId", backupID, "error", err.Error())
	} else {
		defer cleanup()
		filename, filenameErr := backupArchiveFilename(backupID)
		if filenameErr != nil {
			slog.WarnContext(ctx, "failed to sanitize backup id for file cleanup", "backupId", backupID, "error", filenameErr.Error())
		} else if _, _, err = s.deps.Exec(ctx, containerID, "", []string{"rm", "-f", path.Join("/volume", filename)}); err != nil {
			slog.WarnContext(ctx, "failed to delete backup file (orphan file may remain)", "backupId", backupID, "error", err.Error())
		}
	}
	s.logBackupEvent(ctx, event.EventTypeVolumeBackupDelete, entry.VolumeName, user, database.JSON{"action": "backup_delete", "backup_id": backupID})
	return nil
}

// deleteRusticBackups forgets snapshots grouped by repository so each repository is pruned once.
func (s *Service) deleteRusticBackups(ctx context.Context, entries []*volume.Backup, user *usertypes.Actor, includeRemote bool) error {
	dockerClient, err := s.deps.Docker.GetClient(ctx)
	if err != nil {
		return err
	}
	var localSnapshotIDs []string
	remoteGroups := make(map[remoteRepositoryKey][]*volume.Backup)
	for _, entry := range entries {
		if entry.LocalSnapshotID != "" {
			localSnapshotIDs = append(localSnapshotIDs, entry.LocalSnapshotID)
		}
		if includeRemote && entry.RemoteSnapshotID != "" {
			key := remoteRepositoryKey{destinationID: entry.S3DestinationID, instanceID: s.remoteInstanceID(entry.RemoteInstanceID)}
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
		var observation backuptypes.RepositoryObservation
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

// PruneLocalRepository frees the data of deleted local volume backups once Rustic's keep-delete window passes.
func (s *Service) PruneLocalRepository(ctx context.Context) error {
	dockerClient, err := s.deps.Docker.GetClient(ctx)
	if err != nil {
		return err
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

// DownloadBackup streams a backup as a tar.gz archive: legacy archives stream their stored
// file, and local Rustic snapshots are restored into a scratch volume and packaged on the fly.
func (s *Service) DownloadBackup(ctx context.Context, backupID string, user *usertypes.Actor) (io.ReadCloser, int64, error) {
	entry, err := s.deps.Store.Run(ctx, backupID)
	if err != nil {
		return nil, 0, err
	}
	if entry.Format != volume.BackupFormatArchive && entry.LocalSnapshotID == "" {
		return nil, 0, errors.New("only local volume backups can be downloaded")
	}
	dockerClient, err := s.deps.Docker.GetClient(ctx)
	if err != nil {
		return nil, 0, err
	}
	var containerID, archivePath string
	var cleanup func()
	if entry.Format == volume.BackupFormatArchive {
		filename, filenameErr := backupArchiveFilename(backupID)
		if filenameErr != nil {
			return nil, 0, filenameErr
		}
		archivePath = path.Join("/volume", filename)
		containerID, cleanup, err = s.createBackupTempContainer(ctx, dockerClient, "/volume", true)
	} else {
		// Restore the local snapshot into a scratch volume and tar it inside a helper container.
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
		if _, volumeCreateErr := dockerClient.VolumeCreate(ctx, client.VolumeCreateOptions{Name: scratchVolume, Labels: volumehelper.Labels()}); volumeCreateErr != nil {
			return nil, 0, fmt.Errorf("failed to create download scratch volume: %w", volumeCreateErr)
		}
		removeScratch := func() {
			_, _ = dockerClient.VolumeRemove(context.WithoutCancel(ctx), scratchVolume, client.VolumeRemoveOptions{Force: true})
		}
		scratchMount := mount.Mount{Type: mount.TypeVolume, Source: scratchVolume, Target: "/volume"}
		restoreOptions := backup.RestoreOptions{DeleteExtra: true}
		if restoreErr := s.deps.Engine.RestoreSnapshot(ctx, dockerClient, repository, password, entry.LocalSnapshotID, scratchMount, restoreOptions); restoreErr != nil {
			removeScratch()
			return nil, 0, fmt.Errorf("failed to restore Rustic snapshot for download: %w", restoreErr)
		}
		var removeContainer func()
		if containerID, removeContainer, err = s.createBackupTempContainerWithMount(ctx, dockerClient, scratchMount); err != nil {
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
	reader, size, err := volumehelper.DownloadFileFromContainer(ctx, dockerClient, containerID, archivePath, cleanup)
	if err != nil {
		return nil, 0, err
	}
	s.logBackupEvent(ctx, event.EventTypeVolumeBackupDownload, entry.VolumeName, user, database.JSON{"action": "backup_download", "backup_id": backupID, "size": size})
	return reader, size, nil
}

// ArchiveFileTarget resolves a legacy archive backup of volumeName and the
// sanitized member path for a single-file workspace restore.
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
	if s.deps.Engine == nil || s.deps.S3Destinations == nil || s.deps.RecoveryKeys == nil {
		return 0, nil, errors.New("volume backup discovery is unavailable")
	}
	key, err := s.deps.RecoveryKeys.Get(ctx)
	if errors.Is(err, backup.ErrRecoveryKeyNotConfigured) {
		return 0, nil, errors.New("configure a recovery key in system backups before discovering volume backups")
	}
	if err != nil {
		return 0, nil, err
	}
	configuration, err := s.deps.S3Destinations.Configuration(ctx, destinationID)
	if err != nil {
		return 0, nil, errors.New("the selected S3 backup destination is not configured")
	}
	roots, err := s3utils.ListRepositoryRoots(ctx, configuration, backup.VolumeRoot)
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
func discoveredVolumeBackup(destinationID, root string, snapshot backup.DiscoveredSnapshot) *volume.Backup {
	volumeName := strings.TrimSpace(snapshot.Label)
	if volumeName == "" || volumeName == systemRecoverySnapshotLabel {
		return nil
	}
	createdAt := snapshot.Time
	if createdAt.IsZero() {
		createdAt = time.Now().UTC()
	}
	return &volume.Backup{
		ID:         fmt.Sprintf("remote-%s-%s-%s", destinationID, root, snapshot.ID),
		VolumeName: volumeName, Size: snapshot.Summary.TotalBytesProcessed, CreatedAt: createdAt,
		Status: backuptypes.VolumeBackupStatusSucceeded, Trigger: backuptypes.VolumeBackupTriggerManual,
		Destination: volume.BackupDestinationS3, Format: volume.BackupFormatRustic,
		RemoteSnapshotID: snapshot.ID, S3DestinationID: destinationID, RemoteInstanceID: root,
	}
}

// MigrateRepositoryPasswords re-keys this instance's own volume backup repositories to the recovery key; other instances re-key their own roots.
func (s *Service) MigrateRepositoryPasswords(ctx context.Context) error {
	if s.deps.RecoveryKeys == nil || s.deps.Engine == nil {
		return nil
	}
	_, err := s.deps.RecoveryKeys.Get(ctx)
	if errors.Is(err, backup.ErrRecoveryKeyNotConfigured) {
		return nil
	}
	if err != nil {
		return err
	}
	dockerClient, err := s.deps.Docker.GetClient(ctx)
	if err != nil {
		return err
	}
	local, err := s.localRusticRepository(ctx, dockerClient, true)
	if err != nil {
		return err
	}
	repositories := []backup.Repository{local}
	if s.deps.S3Destinations != nil {
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
	}
	_, err = s.volumeBackupPassword(ctx, dockerClient, repositories...)
	return err
}

// StartBackup persists a running backup and submits its workflow.
func (s *Service) StartBackup(ctx context.Context, environmentID, volumeName string, user usertypes.Actor, request volume.CreateBackupRequest) (volume.BackupEntry, error) {
	plan, err := s.resolveBackupPlan(ctx, volumeName, backuptypes.VolumeBackupTriggerManual, request, nil)
	if err != nil {
		return volume.BackupEntry{}, err
	}
	requester := backup.NewRequester(ctx, user, environmentID, authz.PermVolumesBackup)
	if authorizeErr := s.deps.Engine.Authorize(ctx, requester); authorizeErr != nil {
		return volume.BackupEntry{}, authorizeErr
	}
	entry, lease, err := s.prepareBackup(ctx, volumeName, backuptypes.VolumeBackupTriggerManual, request.PolicyID, &plan)
	if err != nil {
		return volume.BackupEntry{}, err
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
		return volume.BackupEntry{}, s.completeBackup(ctx, entry, err)
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

// watchBackup fails the backup's row when its workflow ends without the task completing it, such as a cancel
// before the task started.
func (s *Service) watchBackup(ctx context.Context, backupID, instanceID string) {
	outcome, err := s.flow.Wait(ctx, s.backupWorkflow, instanceID)
	if err != nil || outcome.Status == scheduler.Succeeded {
		return
	}
	if current, loadErr := s.deps.Store.Run(ctx, backupID); loadErr == nil && current.Status == backuptypes.VolumeBackupStatusRunning {
		if failErr := s.deps.Store.Fail(ctx, backupID, cmp.Or(outcome.Message, "Backup did not run")); failErr != nil {
			slog.WarnContext(ctx, "Failed to close a volume backup whose workflow ended early", "backupId", backupID, "error", failErr)
		}
	}
}

func (s *Service) ReconcileBackup(ctx context.Context, previous scheduler.Run, volumeName string) (scheduler.Outcome, error) {
	outcome := jobcontext.ConfirmedTarget(previous, volumeName)
	if outcome.Status == scheduler.Succeeded {
		return outcome, nil
	}
	index := slices.IndexFunc(previous.Outcome.Targets, func(target scheduler.TargetOutcome) bool {
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
	// Only an interrupted backup is still running; a failed one was settled by its attempt, even if its volume
	// was renamed since, and the caller starts a new backup under the current name.
	if entry.Status == backuptypes.VolumeBackupStatusFailed {
		return outcome, backup.ErrBackupSettled
	}
	if entry.VolumeName != volumeName {
		return outcome, gorm.ErrRecordNotFound
	}
	if entry.Status != backuptypes.VolumeBackupStatusSucceeded {
		// Resuming would copy data its stopped containers may since have changed, so the row fails and the
		// caller starts a new backup that stops them again.
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
		// Another backup of this volume is running; the task is handed back, without spending its one attempt,
		// so a later delivery resumes this one rather than leaving it running.
		if !admitted {
			return outcome, fmt.Errorf("%w: %w", actor.ErrJobRejected, s.deps.AlreadyRunning)
		}
		defer lease.Release(ctx)
		if resumeErr := s.resumeBackup(ctx, previous, entry, checkpoint); resumeErr != nil {
			return outcome, resumeErr
		}
	}
	target.Status = scheduler.Succeeded
	if progressErr := jobcontext.Progress(ctx, target); progressErr != nil {
		return outcome, progressErr
	}
	return scheduler.Outcome{Status: scheduler.Succeeded, Targets: []scheduler.TargetOutcome{target}}, nil
}

// resumeBackup adopts snapshots an interrupted run already wrote, then finishes the backup.
func (s *Service) resumeBackup(ctx context.Context, previous scheduler.Run, entry *volume.Backup, checkpoint backupPlan) error {
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
		return slices.ContainsFunc(previous.Outcome.Targets, func(target scheduler.TargetOutcome) bool { return target.ID == entry.ID+":"+destination })
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
	return s.completeBackup(ctx, entry, s.executeBackup(ctx, entry, usertypes.SystemUser, checkpoint))
}

// runManualBackup is the volume-backup step. A delivery that finds the checkpoint it recorded
// resumes that backup; otherwise it takes over the admission StartBackup won and runs it.
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
	resuming := slices.ContainsFunc(previous.Outcome.Targets, func(target scheduler.TargetOutcome) bool {
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
	succeeded := scheduler.Outcome{Status: scheduler.Succeeded, Message: "Volume backup created successfully"}
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
		if outcome.Status != scheduler.Succeeded {
			return nil, errors.New(outcome.Message)
		}
		return succeeded, nil
	}
	entry, err := s.deps.Store.Run(ctx, input.Checkpoint.BackupID)
	if err != nil {
		return nil, err
	}
	requestedBy := usertypes.SystemUser
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
	target := scheduler.TargetOutcome{ResourceType: "volume_backup", ID: input.VolumeName, Status: scheduler.Running, RecoveryData: checkpoint, ActivityID: t.ActivityID()}
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
