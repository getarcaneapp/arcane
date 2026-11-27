package policies

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"time"

	"github.com/italypaleale/francis/builtin/workflow"

	"getarcane.dev/arcane/v3/internal/backup"
	"getarcane.dev/arcane/v3/internal/common"
	"getarcane.dev/arcane/v3/internal/database"
	dockerInternal "getarcane.dev/arcane/v3/internal/docker"
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
		Policies       *backup.Policies
		S3Destinations *s3.S3DestinationService
		Settings       *settings.SettingsService
		LatestRun      func(ctx context.Context, policyID string) (*types.VolumeBackupEntry, error)
		CreateBackup   func(ctx context.Context, volumeName, policyID string, admitted func(context.Context) error) (*types.VolumeBackup, error)
		Reconcile      func(ctx context.Context, previous types.SchedulerRun, volumeName string) (types.SchedulerOutcome, error)
		Docker         *dockerInternal.DockerClientService
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

func (s *Service) GetBackupPolicies(ctx context.Context, volumeName string) (*types.VolumeBackupPolicyCollection, error) {
	policies, err := s.deps.Policies.List(ctx, backup.PolicyVolume, volumeName)
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
		destination := destinations[policies[i].S3DestinationID]
		result.Policies = append(result.Policies, types.VolumeBackupPolicy{ //exhaustruct:enforce
			LastRun:           lastRun,
			ID:                policies[i].ID,
			VolumeName:        policies[i].SubjectID,
			Schedule:          policies[i].Schedule,
			S3DestinationID:   policies[i].S3DestinationID,
			S3DestinationName: destination.Name,
			S3Bucket:          destination.Bucket,
			RetentionCount:    policies[i].RetentionCount,
			Enabled:           policies[i].Enabled,
			StopContainers:    policies[i].StopContainers,
			LocalEnabled:      policies[i].LocalEnabled,
			S3Enabled:         policies[i].S3Enabled,
			S3Available:       result.S3Available,
		})
	}
	return result, nil
}

func (s *Service) UpdateBackupPolicies(ctx context.Context, volumeName string, updates []types.UpdateBackupPolicy) (*types.VolumeBackupPolicyCollection, error) {
	reconcile := backup.PolicyReconciliation[types.UpdateBackupPolicy]{
		Kind:      backup.PolicyVolume,
		SubjectID: volumeName,
		Domain:    "volume",
		UpdateID:  func(update types.UpdateBackupPolicy) string { return update.ID },
		Build: func(ctx context.Context, policy *backup.Policy, update types.UpdateBackupPolicy) error {
			normalized, validatePolicyUpdateErr := backup.ValidatePolicyUpdate(ctx, "volume", update, s.deps.S3Destinations)
			if validatePolicyUpdateErr != nil {
				return validatePolicyUpdateErr
			}
			*policy = backup.Policy{ //exhaustruct:enforce
				BaseModel:       policy.BaseModel,
				SubjectKind:     backup.PolicyVolume,
				SubjectID:       volumeName,
				Schedule:        normalized.Schedule,
				S3DestinationID: normalized.S3DestinationID,
				Options:         backup.PolicyOptions{Volumes: nil, SelectionMode: "", ProjectIDs: nil, VolumeNames: nil, IgnoreAnonymous: false},
				RetentionCount:  normalized.RetentionCount,
				Enabled:         normalized.Enabled,
				StopContainers:  normalized.StopContainers,
				LocalEnabled:    normalized.LocalEnabled,
				S3Enabled:       normalized.S3Enabled,
			}
			return nil
		},
		Unregister: s.jobs.Unregister,
		Reschedule: s.reschedule,
	}
	if runErr := s.deps.Policies.Reconcile(ctx, reconcile, updates); runErr != nil {
		return nil, runErr
	}
	return s.GetBackupPolicies(ctx, volumeName)
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
	policy, err := s.deps.Policies.Enabled(ctx, input.PolicyID)
	if err != nil {
		return nil, err
	}
	if policy == nil {
		return types.SchedulerOutcome{Status: types.RunStatusCanceled, Message: "Backup policy disabled or deleted"}, nil
	}
	// The remote is checked once the run is admitted, against the policy the backup then runs with, so S3 enabled or
	// moved while the run waited is checked too. A missing repository disables S3 (or the policy) before the plan
	// resolves, so the backup never recreates it.
	var remoteDisabled bool
	entry, err := s.deps.CreateBackup(ctx, policy.SubjectID, policy.ID, func(ctx context.Context) error {
		current, loadErr := s.deps.Policies.Enabled(ctx, policy.ID)
		if loadErr != nil {
			return loadErr
		}
		if current == nil {
			return common.ErrVolumeBackupPolicyNotFound
		}
		var checkErr error
		remoteDisabled, checkErr = s.deps.Policies.CheckRemote(ctx, current, backup.RemoteCheck{
			S3Destinations: s.deps.S3Destinations,
			Settings:       s.deps.Settings,
			Reschedule:     s.reschedule,
		})
		return checkErr
	})
	if errors.Is(err, backup.ErrRemoteOnlyDisabled) {
		return types.SchedulerOutcome{Status: types.RunStatusNeedsAttention, Message: backup.RemoteDisabledMessage}, nil
	}
	// The backup re-reads the policy once admitted, so a wait can still find it disabled or deleted.
	if errors.Is(err, common.ErrVolumeBackupPolicyNotFound) {
		return types.SchedulerOutcome{Status: types.RunStatusCanceled, Message: "Backup policy disabled or deleted"}, nil
	}
	if err != nil {
		slog.ErrorContext(ctx, "Scheduled volume backup failed", "volume", policy.SubjectID, "error", err)
		return nil, err
	}
	slog.InfoContext(ctx, "Scheduled volume backup completed", "volume", policy.SubjectID, "backupId", entry.ID, "remoteSnapshotId", entry.RemoteSnapshotID)
	if remoteDisabled {
		return types.SchedulerOutcome{Status: types.RunStatusPartial, Message: backup.RemoteDisabledMessage}, nil
	}
	return types.SchedulerOutcome{Status: types.RunStatusSucceeded, Message: "Scheduled volume backup created successfully"}, nil
}

func (s *Service) reschedule(ctx context.Context, policy *backup.Policy) {
	if !policy.Enabled {
		s.jobs.Unregister(ctx, policy.ID)
		return
	}
	policyID, volumeName := policy.ID, policy.SubjectID
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
		ScheduleFn:  s.deps.Policies.Schedule(policyID, policy.Schedule),
		ShouldRunFn: func(context.Context) bool { return s.deps.Docker.LocalDockerEnabled() },
		FallbackFn: func(ctx context.Context, previous types.SchedulerRun) (types.SchedulerOutcome, error) {
			return s.deps.Reconcile(ctx, previous, volumeName)
		},
	})
}

func (s *Service) RegisterJobsOnStartup(ctx context.Context) {
	policies, err := s.deps.Policies.AllEnabled(ctx, backup.PolicyVolume)
	if err != nil {
		slog.ErrorContext(ctx, "Failed to load scheduled volume backups", "error", err)
		return
	}
	count := 0
	for i := range policies {
		if policies[i].SubjectID == "" {
			continue
		}
		s.reschedule(ctx, &policies[i])
		count++
	}
	slog.InfoContext(ctx, "Registered scheduled volume backup jobs", "count", count)
}

func (s *Service) Remove(ctx context.Context, volumeName string) {
	policies, err := s.deps.Policies.Remove(ctx, backup.PolicyVolume, volumeName)
	for i := range policies {
		s.jobs.Unregister(ctx, policies[i].ID)
	}
	if err != nil {
		slog.WarnContext(ctx, "Failed to delete volume backup policy", "volume", volumeName, "error", err)
	}
}
