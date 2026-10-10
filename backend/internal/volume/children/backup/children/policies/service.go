package policies

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"path"
	"slices"
	"strings"
	"time"

	activitytypes "github.com/getarcaneapp/arcane/types/v2/activity"
	"github.com/getarcaneapp/arcane/types/v2/scheduler"
	"github.com/getarcaneapp/arcane/types/v2/user"
	"github.com/getarcaneapp/arcane/types/v2/volume"
	"github.com/italypaleale/francis/builtin/workflow"
	"gorm.io/gorm"

	"github.com/getarcaneapp/arcane/backend/v2/internal/backup"
	"github.com/getarcaneapp/arcane/backend/v2/internal/database"
	"github.com/getarcaneapp/arcane/backend/v2/internal/s3"
	"github.com/getarcaneapp/arcane/backend/v2/internal/settings"
	activitylib "github.com/getarcaneapp/arcane/backend/v2/pkg/libarcane/activity"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/scheduler/entityjobs"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/scheduler/flow"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/scheduler/jobcontext"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/scheduler/runs"
)

const defaultSchedule = "0 0 2 * * *"

// Dependencies are the backup operations scheduled policies run through.
type Dependencies struct {
	DB             *database.DB
	S3Destinations *s3.S3DestinationService
	Settings       *settings.SettingsService
	AlreadyRunning error
	LatestRun      func(ctx context.Context, policyID string) (*volume.BackupEntry, error)
	CreateBackup   func(ctx context.Context, volumeName, policyID string) (*volume.Backup, error)
	Reconcile      func(ctx context.Context, previous scheduler.Run, volumeName string) (scheduler.Outcome, error)
}

// Service owns per-volume backup policies and their scheduled jobs.
type Service struct {
	deps     Dependencies
	jobs     *entityjobs.Registry
	flow     *flow.Engine
	workflow *flow.Workflow
}

// scheduledBackupInput is the policy a scheduled backup job runs for.
type scheduledBackupInput struct {
	PolicyID string `json:"policyId"`
}

func NewService(deps Dependencies) *Service {
	return &Service{deps: deps, jobs: entityjobs.New("volume-backup:", backup.VolumeAdmissionScope)}
}

// SetScheduler injects the dynamic scheduler and admission gate for per-policy
// backup jobs. Agent mode passes them too: agents run their own volume backups.
func (s *Service) SetScheduler(ctx context.Context, dynamicScheduler scheduler.DynamicScheduler, admissionGate *runs.Admission) error {
	return s.jobs.SetScheduler(ctx, dynamicScheduler, admissionGate)
}

// RegisterWorkflows defines the scheduled backup workflow while the host is still unstarted.
func (s *Service) RegisterWorkflows(engine *flow.Engine) error {
	var err error
	s.flow = engine
	s.workflow, err = engine.Define(flow.Definition{
		Name:        "volume-backup-policy",
		Version:     1,
		Fingerprint: "cbf39d2f46876dd35ec9c1a792e7946097827079c8fc33c00b0f8034e8044534",
		Concurrency: 4,
		Timeout:     24 * time.Hour,
		Activity: activitylib.StartRequest{
			Type: activitytypes.TypeResourceAction, ResourceType: new("volume_backup"), StartedBy: &user.SystemUser,
			Step: "Creating scheduled backup", LatestMessage: "Creating scheduled volume backup",
		},
		Labels: map[string]string{"backup": "Creating scheduled backup"},
		Steps:  []workflow.StepSpec{workflow.Step("backup", engine.Handler(s.runScheduledBackup), workflow.WithMaxAttempts(1))},
	})
	return err
}

func (s *Service) policies(ctx context.Context, volumeName string) ([]VolumeBackupPolicy, error) {
	var policies []VolumeBackupPolicy
	if err := s.deps.DB.WithContext(ctx).Where("volume_name = ?", volumeName).Order("created_at ASC").Find(&policies).Error; err != nil {
		return nil, fmt.Errorf("failed to load volume backup policies: %w", err)
	}
	return policies, nil
}

// Policy loads one policy of the volume; an empty or unknown ID returns nil.
func (s *Service) Policy(ctx context.Context, volumeName, policyID string) (*VolumeBackupPolicy, error) {
	if strings.TrimSpace(policyID) == "" {
		return nil, nil
	}
	var policy VolumeBackupPolicy
	err := s.deps.DB.WithContext(ctx).Where("id = ? AND volume_name = ?", policyID, volumeName).First(&policy).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to load volume backup policy: %w", err)
	}
	return &policy, nil
}

func (s *Service) GetBackupPolicies(ctx context.Context, volumeName string) (*volume.BackupPolicyCollection, error) {
	policies, err := s.policies(ctx, volumeName)
	if err != nil {
		return nil, err
	}
	result := &volume.BackupPolicyCollection{Policies: make([]volume.BackupPolicy, 0, len(policies))}
	destinations := s.deps.S3Destinations.DestinationsByID(ctx)
	result.S3Available = len(destinations) > 0
	for i := range policies {
		lastRun, runErr := s.deps.LatestRun(ctx, policies[i].ID)
		if runErr != nil {
			return nil, fmt.Errorf("failed to load latest volume backup: %w", runErr)
		}
		if lastRun != nil {
			if destination, ok := destinations[lastRun.S3DestinationID]; ok {
				lastRun.S3DestinationName = destination.Name
			}
		}
		dto := policies[i].ToDTO(lastRun)
		dto.S3Available = result.S3Available
		if destination, ok := destinations[policies[i].S3DestinationID]; ok {
			dto.S3Bucket = destination.Bucket
			dto.S3DestinationName = destination.Name
		}
		result.Policies = append(result.Policies, dto)
	}
	return result, nil
}

func (s *Service) UpdateBackupPolicies(ctx context.Context, volumeName string, updates []volume.UpdateBackupPolicy) (*volume.BackupPolicyCollection, error) {
	existing, err := s.policies(ctx, volumeName)
	if err != nil {
		return nil, err
	}
	reconcile := backup.PolicyReconciliation[VolumeBackupPolicy, volume.UpdateBackupPolicy]{
		Domain:   "volume",
		DB:       s.deps.DB,
		Existing: existing,
		ID:       func(policy *VolumeBackupPolicy) string { return policy.ID },
		UpdateID: func(update volume.UpdateBackupPolicy) string { return update.ID },
		New:      func() VolumeBackupPolicy { return VolumeBackupPolicy{VolumeName: volumeName} },
		Build: func(ctx context.Context, policy *VolumeBackupPolicy, update volume.UpdateBackupPolicy) error {
			normalized, validatePolicyUpdateErr := backup.ValidatePolicyUpdate(ctx, "volume", update, s.deps.S3Destinations)
			if validatePolicyUpdateErr != nil {
				return validatePolicyUpdateErr
			}
			update = normalized
			policy.Enabled, policy.Schedule, policy.RetentionCount = update.Enabled, update.Schedule, update.RetentionCount
			policy.StopContainers, policy.LocalEnabled, policy.S3Enabled = update.StopContainers, update.LocalEnabled, update.S3Enabled
			policy.S3DestinationID = update.S3DestinationID
			return nil
		},
		Unregister: s.jobs.Unregister,
		Reschedule: s.reschedule,
	}
	if runErr := reconcile.Run(ctx, updates); runErr != nil {
		return nil, runErr
	}
	return s.GetBackupPolicies(ctx, volumeName)
}

// HasEnabledBackupPolicy reports whether a volume-level schedule takes precedence over centralized backups.
func (s *Service) HasEnabledBackupPolicy(ctx context.Context, volumeName string) (bool, error) {
	var count int64
	if err := s.deps.DB.WithContext(ctx).Model(&VolumeBackupPolicy{}).
		Where("volume_name = ? AND enabled = ?", volumeName, true).Count(&count).Error; err != nil {
		return false, fmt.Errorf("failed to load volume backup policy override: %w", err)
	}
	return count > 0, nil
}

// runScheduledBackup is the volume-backup-policy step. A delivery that finds the checkpoint
// it recorded resumes that backup instead of starting another.
func (s *Service) runScheduledBackup(ctx context.Context, t flow.Task) (any, error) {
	var input scheduledBackupInput
	if err := t.Payload(&input); err != nil {
		return nil, err
	}
	// The run belongs to one policy, so its checkpoint is found by kind; it names the volume as the backup did,
	// even if the volume was renamed since the job was registered.
	// A retry after a rename can leave an older checkpoint first, so the latest one is the run's own.
	previous, _ := jobcontext.Run(ctx)
	for _, target := range slices.Backward(previous.Outcome.Targets) {
		if target.ResourceType != "volume_backup" || len(target.RecoveryData) == 0 {
			continue
		}
		// A retry of a backup that already failed runs a new one below.
		if outcome, err := s.deps.Reconcile(ctx, previous, target.ID); !errors.Is(err, backup.ErrBackupSettled) {
			return outcome, err
		}
		break
	}
	var policy VolumeBackupPolicy
	if err := s.deps.DB.WithContext(ctx).Where("id = ? AND enabled = ?", input.PolicyID, true).First(&policy).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return scheduler.Outcome{Status: scheduler.Canceled, Message: "Backup policy disabled or deleted"}, nil
		}
		return nil, err
	}
	var remoteErr error
	if policy.S3Enabled {
		root := path.Join(backup.VolumeRoot, s.deps.Settings.GetSettingsConfig().InstanceID.Value)
		remoteErr = backup.CheckScheduledRemote(ctx, s.deps.DB, s.deps.S3Destinations, "volume_backups", policy.S3DestinationID, root)
	}
	remoteDisabled, disableErr := backup.DisableMissingRemote(remoteErr, policy.LocalEnabled, &policy.S3Enabled, &policy.Enabled, func(field string) (bool, error) {
		return backup.DisableStoredRemote(ctx, s.deps.DB, &VolumeBackupPolicy{}, policy.ID, policy.S3DestinationID, policy.LocalEnabled, field)
	})
	if disableErr != nil {
		return nil, disableErr
	}
	if remoteDisabled {
		s.reschedule(ctx, &policy)
	}
	if remoteDisabled && !policy.LocalEnabled {
		return scheduler.Outcome{Status: scheduler.NeedsAttention, Message: backup.RemoteDisabledMessage}, nil
	}
	entry, err := s.deps.CreateBackup(ctx, policy.VolumeName, policy.ID)
	if errors.Is(err, s.deps.AlreadyRunning) {
		slog.InfoContext(ctx, "Scheduled volume backup skipped; another backup is running", "volume", policy.VolumeName)
		return scheduler.Outcome{Status: scheduler.Skipped, Message: "Skipped: another backup is running for this volume"}, nil
	}
	if err != nil {
		slog.ErrorContext(ctx, "Scheduled volume backup failed", "volume", policy.VolumeName, "error", err)
		return nil, err
	}
	slog.InfoContext(ctx, "Scheduled volume backup completed", "volume", policy.VolumeName, "backupId", entry.ID, "remoteSnapshotId", entry.RemoteSnapshotID)
	if remoteDisabled {
		return scheduler.Outcome{Status: scheduler.Partial, Message: backup.RemoteDisabledMessage}, nil
	}
	return scheduler.Outcome{Status: scheduler.Succeeded, Message: "Scheduled volume backup created successfully"}, nil
}

func (s *Service) reschedule(ctx context.Context, policy *VolumeBackupPolicy) {
	if policy == nil {
		return
	}
	if !policy.Enabled {
		s.jobs.Unregister(ctx, policy.ID)
		return
	}
	policyID, volumeName := policy.ID, policy.VolumeName
	s.jobs.Add(ctx, &flow.Job{
		Engine:   s.flow,
		Workflow: s.workflow,
		JobName:  s.jobs.JobName(policyID),
		Payload:  scheduledBackupInput{PolicyID: policyID},
		Activity: activitylib.StartRequest{
			ResourceID: new(volumeName), ResourceName: new(volumeName),
			Metadata: database.JSON{
				"action": "scheduled_volume_backup", "policyId": policyID, "schedule": policy.Schedule, "volumeName": volumeName,
				"retentionCount": policy.RetentionCount, "stopContainers": policy.StopContainers,
				"localEnabled": policy.LocalEnabled, "s3Enabled": policy.S3Enabled, "s3DestinationId": policy.S3DestinationID,
			},
		},
		ScheduleFn: func(ctx context.Context) string {
			var current VolumeBackupPolicy
			if err := s.deps.DB.WithContext(ctx).Where("id = ?", policyID).First(&current).Error; err != nil {
				return defaultSchedule
			}
			return current.Schedule
		},
		FallbackFn: func(ctx context.Context, previous scheduler.Run) (scheduler.Outcome, error) {
			return s.deps.Reconcile(ctx, previous, volumeName)
		},
	})
}

func (s *Service) RegisterJobsOnStartup(ctx context.Context) {
	if !s.jobs.Enabled() {
		return
	}
	var policies []VolumeBackupPolicy
	if err := s.deps.DB.WithContext(ctx).Where("enabled = ?", true).Find(&policies).Error; err != nil {
		slog.ErrorContext(ctx, "Failed to load scheduled volume backups", "error", err)
		return
	}
	for i := range policies {
		s.reschedule(ctx, &policies[i])
	}
	slog.InfoContext(ctx, "Registered scheduled volume backup jobs", "count", len(policies))
}

// Remove unregisters and deletes every policy of a removed volume.
func (s *Service) Remove(ctx context.Context, volumeName string) {
	policies, err := s.policies(ctx, volumeName)
	if err != nil {
		return
	}
	for i := range policies {
		s.jobs.Unregister(ctx, policies[i].ID)
	}
	if deleteBackupPolicyErr := s.deps.DB.WithContext(ctx).Where("volume_name = ?", volumeName).Delete(&VolumeBackupPolicy{}).Error; deleteBackupPolicyErr != nil {
		slog.WarnContext(ctx, "Failed to delete volume backup policy", "volume", volumeName, "error", deleteBackupPolicyErr)
	}
}

// Rename moves a renamed volume's policies inside the caller's transaction.
func (s *Service) Rename(tx *gorm.DB, oldName, newName string) error {
	return tx.Model(&VolumeBackupPolicy{}).Where("volume_name = ?", oldName).Update("volume_name", newName).Error
}
