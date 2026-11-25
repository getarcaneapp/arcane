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

	"github.com/italypaleale/francis/builtin/workflow"
	"gorm.io/gorm"

	"getarcane.dev/arcane/v3/internal/backup"
	"getarcane.dev/arcane/v3/internal/common"
	"getarcane.dev/arcane/v3/internal/database"
	"getarcane.dev/arcane/v3/internal/docker"
	"getarcane.dev/arcane/v3/internal/s3"
	"getarcane.dev/arcane/v3/internal/scheduler/entityjobs"
	"getarcane.dev/arcane/v3/internal/scheduler/flow"
	"getarcane.dev/arcane/v3/internal/scheduler/jobcontext"
	"getarcane.dev/arcane/v3/internal/scheduler/runs"
	"getarcane.dev/arcane/v3/internal/settings"
	activitylib "getarcane.dev/arcane/v3/pkg/activity"
	"getarcane.dev/types/v3"
)

type (
	Dependencies struct {
		DB             *database.DB
		S3Destinations *s3.S3DestinationService
		Settings       *settings.SettingsService
		LatestRun      func(ctx context.Context, policyID string) (*types.VolumeBackupEntry, error)
		CreateBackup   func(ctx context.Context, volumeName, policyID string, admitted func(context.Context) error) (*types.VolumeBackup, error)
		Reconcile      func(ctx context.Context, previous types.SchedulerRun, volumeName string) (types.SchedulerOutcome, error)
		Docker         *docker.DockerClientService
		Ctx            context.Context
		Scheduler      types.DynamicScheduler
		Admission      *runs.Admission
	}

	Service struct {
		deps     Dependencies
		jobs     *entityjobs.Registry
		flow     *flow.Engine
		workflow *flow.Workflow
	}

	scheduledBackupInput struct {
		PolicyID string `json:"policyId"`
	}
)

const (
	defaultSchedule = "0 2 * * *"
)

// errRemoteOnlyDisabled stops a scheduled run whose only destination, S3, was just disabled for a missing repository.
var errRemoteOnlyDisabled = errors.New("the S3 repository is missing and the policy has no local destination")

func NewService(deps Dependencies) *Service {
	return &Service{
		deps: deps,
		jobs: entityjobs.New(entityjobs.Config{
			Lifecycle:      deps.Ctx,
			Scheduler:      deps.Scheduler,
			Admission:      deps.Admission,
			JobPrefix:      "volume-backup:",
			AdmissionScope: backup.VolumeAdmissionScope,
		}),
	}
}

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
			Type: types.ActivityTypeResourceAction, ResourceType: new("volume_backup"), StartedBy: &types.SystemUser,
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

func (s *Service) IDs(ctx context.Context) ([]string, error) {
	var ids []string
	if err := s.deps.DB.WithContext(ctx).Model(&VolumeBackupPolicy{}).Pluck("id", &ids).Error; err != nil {
		return nil, fmt.Errorf("failed to load volume backup policy IDs: %w", err)
	}
	return ids, nil
}

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

func (s *Service) GetBackupPolicies(ctx context.Context, volumeName string) (*types.VolumeBackupPolicyCollection, error) {
	policies, err := s.policies(ctx, volumeName)
	if err != nil {
		return nil, err
	}
	destinations := s.deps.S3Destinations.DestinationsByID(ctx)
	result := &types.VolumeBackupPolicyCollection{
		Policies:    make([]types.VolumeBackupPolicy, 0, len(policies)),
		S3Available: len(destinations) > 0,
	}
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

func (s *Service) UpdateBackupPolicies(ctx context.Context, volumeName string, updates []types.UpdateBackupPolicy) (*types.VolumeBackupPolicyCollection, error) {
	existing, err := s.policies(ctx, volumeName)
	if err != nil {
		return nil, err
	}
	reconcile := backup.PolicyReconciliation[VolumeBackupPolicy, types.UpdateBackupPolicy]{
		Domain:   "volume",
		DB:       s.deps.DB,
		Existing: existing,
		ID:       func(policy *VolumeBackupPolicy) string { return policy.ID },
		UpdateID: func(update types.UpdateBackupPolicy) string { return update.ID },
		New:      func() VolumeBackupPolicy { return VolumeBackupPolicy{VolumeName: volumeName} },
		Build: func(ctx context.Context, policy *VolumeBackupPolicy, update types.UpdateBackupPolicy) error {
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

func (s *Service) HasEnabledBackupPolicy(ctx context.Context, volumeName string) (bool, error) {
	var count int64
	if err := s.deps.DB.WithContext(ctx).Model(&VolumeBackupPolicy{}).
		Where("volume_name = ? AND enabled = ?", volumeName, true).Count(&count).Error; err != nil {
		return false, fmt.Errorf("failed to load volume backup policy override: %w", err)
	}
	return count > 0, nil
}

func (s *Service) runScheduledBackup(ctx context.Context, t flow.Task) (any, error) {
	var input scheduledBackupInput
	if err := t.Payload(&input); err != nil {
		return nil, err
	}
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
			return types.SchedulerOutcome{Status: types.RunStatusCanceled, Message: "Backup policy disabled or deleted"}, nil
		}
		return nil, err
	}
	// The remote is checked once the run is admitted, against the policy the backup then runs with, so S3 enabled or
	// moved while the run waited is checked too. A missing repository disables S3 (or the policy) before the plan
	// resolves, so the backup never recreates it.
	var remoteDisabled bool
	current := policy
	entry, err := s.deps.CreateBackup(ctx, policy.VolumeName, policy.ID, func(ctx context.Context) error {
		if loadErr := s.deps.DB.WithContext(ctx).Where("id = ? AND enabled = ?", policy.ID, true).First(&current).Error; loadErr != nil {
			if errors.Is(loadErr, gorm.ErrRecordNotFound) {
				return common.ErrVolumeBackupPolicyNotFound
			}
			return loadErr
		}
		var remoteErr error
		if current.S3Enabled {
			root := path.Join(backup.VolumeRoot, s.deps.Settings.GetSettingsConfig().InstanceID.Value)
			remoteErr = backup.CheckScheduledRemote(ctx, s.deps.DB, s.deps.S3Destinations, "volume_backups", current.S3DestinationID, root)
		}
		var disableErr error
		remoteDisabled, disableErr = backup.DisableMissingRemote(remoteErr, current.LocalEnabled, &current.S3Enabled, &current.Enabled, func(field string) (bool, error) {
			return backup.DisableStoredRemote(ctx, s.deps.DB, backup.DisableRemoteRequest{
				Model:         &VolumeBackupPolicy{},
				ID:            current.ID,
				DestinationID: current.S3DestinationID,
				LocalEnabled:  current.LocalEnabled,
				Field:         field,
			})
		})
		if disableErr != nil {
			return disableErr
		}
		if !remoteDisabled {
			return nil
		}
		// Rescheduled before the backup runs, so a later edit of the policy is not undone afterwards.
		s.reschedule(ctx, &current)
		if !current.LocalEnabled {
			return errRemoteOnlyDisabled
		}
		return nil
	})
	if errors.Is(err, errRemoteOnlyDisabled) {
		return types.SchedulerOutcome{Status: types.RunStatusNeedsAttention, Message: backup.RemoteDisabledMessage}, nil
	}
	// The backup re-reads the policy once admitted, so a wait can still find it disabled or deleted.
	if errors.Is(err, common.ErrVolumeBackupPolicyNotFound) {
		return types.SchedulerOutcome{Status: types.RunStatusCanceled, Message: "Backup policy disabled or deleted"}, nil
	}
	if err != nil {
		slog.ErrorContext(ctx, "Scheduled volume backup failed", "volume", policy.VolumeName, "error", err)
		return nil, err
	}
	slog.InfoContext(ctx, "Scheduled volume backup completed", "volume", policy.VolumeName, "backupId", entry.ID, "remoteSnapshotId", entry.RemoteSnapshotID)
	if remoteDisabled {
		return types.SchedulerOutcome{Status: types.RunStatusPartial, Message: backup.RemoteDisabledMessage}, nil
	}
	return types.SchedulerOutcome{Status: types.RunStatusSucceeded, Message: "Scheduled volume backup created successfully"}, nil
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
		ShouldRunFn: func(context.Context) bool { return s.deps.Docker.LocalDockerEnabled() },
		FallbackFn: func(ctx context.Context, previous types.SchedulerRun) (types.SchedulerOutcome, error) {
			return s.deps.Reconcile(ctx, previous, volumeName)
		},
	})
}

func (s *Service) RegisterJobsOnStartup(ctx context.Context) {
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

func (s *Service) Rename(tx *gorm.DB, oldName, newName string) error {
	return tx.Model(&VolumeBackupPolicy{}).Where("volume_name = ?", oldName).Update("volume_name", newName).Error
}
