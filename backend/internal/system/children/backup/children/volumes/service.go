package volumes

import (
	"cmp"
	"context"
	"encoding/json/v2"
	"errors"
	"fmt"
	"log/slog"
	"path"
	"slices"
	"strings"
	"time"
	"uuid"

	activitytypes "github.com/getarcaneapp/arcane/types/v2/activity"
	backuptypes "github.com/getarcaneapp/arcane/types/v2/backup"
	"github.com/getarcaneapp/arcane/types/v2/scheduler"
	usertypes "github.com/getarcaneapp/arcane/types/v2/user"
	"github.com/italypaleale/francis/builtin/workflow"
	kit "go.getarcane.app/kit/pkg"
	"gorm.io/gorm"

	"github.com/getarcaneapp/arcane/backend/v2/internal/activity"
	"github.com/getarcaneapp/arcane/backend/v2/internal/backup"
	"github.com/getarcaneapp/arcane/backend/v2/internal/common"
	"github.com/getarcaneapp/arcane/backend/v2/internal/database"
	"github.com/getarcaneapp/arcane/backend/v2/internal/s3"
	"github.com/getarcaneapp/arcane/backend/v2/internal/settings"
	"github.com/getarcaneapp/arcane/backend/v2/internal/volume"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/authz"
	activitylib "github.com/getarcaneapp/arcane/backend/v2/pkg/libarcane/activity"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/scheduler/entityjobs"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/scheduler/flow"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/scheduler/jobcontext"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/scheduler/runs"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/utils"
)

const (
	systemVolumeBackupConfigKey = "systemVolumeBackupConfig"
	systemVolumeBackupJobPrefix = "volumes:"
	// systemVolumePlanTarget records a scheduled run's frozen selection.
	systemVolumePlanTarget      = "system-volume-plan"
	defaultSystemVolumeSchedule = "0 0 2 * * *"
)

// Dependencies are the services and system admission volume backups run on.
type Dependencies struct {
	DB             *database.DB
	Engine         *backup.Engine
	Volumes        *volume.VolumeService
	S3Destinations *s3.S3DestinationService
	Activity       *activity.ActivityService
	Settings       *settings.SettingsService
	Jobs           *entityjobs.Registry
	AlreadyRunning error
}

// Service owns system-managed volume backup policies and their runs.
type Service struct {
	db             *database.DB
	engine         *backup.Engine
	volumes        *volume.VolumeService
	s3Destinations *s3.S3DestinationService
	activity       *activity.ActivityService
	settings       *settings.SettingsService
	jobs           *entityjobs.Registry
	alreadyRunning error
	flow           *flow.Engine
	// backupWorkflow runs the selections Start accepted; policyWorkflow runs scheduled policies.
	backupWorkflow *flow.Workflow
	policyWorkflow *flow.Workflow
}

// selectionInput freezes the policy and volumes one run backs up; a manual run also carries its requester.
type selectionInput struct {
	Policy       backuptypes.SystemVolumeBackupPolicy   `json:"policy"`
	ManualPolicy bool                                   `json:"manualPolicy"`
	Candidates   []backuptypes.SystemVolumeBackupOption `json:"candidates"`
	Requester    *backuptypes.Requester                 `json:"requester,omitempty"`
	// Claim names the manual run that parked the system admission for its own task.
	Claim string `json:"claim,omitempty"`
}

func NewService(deps Dependencies) *Service {
	return &Service{
		db:             deps.DB,
		engine:         deps.Engine,
		volumes:        deps.Volumes,
		s3Destinations: deps.S3Destinations,
		activity:       deps.Activity,
		settings:       deps.Settings,
		jobs:           deps.Jobs,
		alreadyRunning: deps.AlreadyRunning,
	}
}

// RegisterWorkflows defines the manual and scheduled volume backup workflows while the host is still unstarted.
func (s *Service) RegisterWorkflows(engine *flow.Engine) error {
	var err error
	s.flow = engine
	s.backupWorkflow, err = engine.Define(flow.Definition{
		Name:        "system-volume-backup",
		Version:     1,
		Fingerprint: "9b4a5e46a984446bc08df39a71e563e031548db3af1825a2413e8c96515db6ab",
		Concurrency: 1,
		Timeout:     24 * time.Hour,
		Activity: activitylib.StartRequest{
			Type:          activitytypes.TypeResourceAction,
			EnvironmentID: "0",
			ResourceType:  new("system_backup"),
			ResourceID:    new("volumes"),
			ResourceName:  new("Volumes"),
		},
		Labels: map[string]string{"backup": "Backing up volumes"},
		Steps: []workflow.StepSpec{workflow.Step("backup", engine.Handler(func(ctx context.Context, t flow.Task) (any, error) {
			var input selectionInput
			if decodeErr := t.Payload(&input); decodeErr != nil {
				return nil, decodeErr
			}
			result, backupErr := s.backUpSelection(ctx, input, volume.VolumeBackupTriggerManual, t.ActivityID())
			if backupErr != nil {
				return nil, backupErr
			}
			return selectionOutcome(result, false), nil
		}), workflow.WithMaxAttempts(1))},
	})
	if err != nil {
		return err
	}
	s.policyWorkflow, err = engine.Define(flow.Definition{
		Name:        "system-volume-backup-policy",
		Version:     1,
		Fingerprint: "94a13d1bac530e6eddcb73c27990a07d69915dc947ce5b559166eb3fb85e39ef",
		Concurrency: 1,
		Timeout:     24 * time.Hour,
		Activity: activitylib.StartRequest{
			Type:          activitytypes.TypeResourceAction,
			EnvironmentID: "0",
			ResourceType:  new("system_backup"),
			ResourceID:    new("volumes"),
			ResourceName:  new("Volumes"),
			StartedBy:     &usertypes.SystemUser,
			Step:          "Backing up volumes",
			LatestMessage: "Creating scheduled system-managed volume backups",
		},
		Labels: map[string]string{"backup": "Backing up volumes"},
		Steps: []workflow.StepSpec{workflow.Step("backup", engine.Handler(func(ctx context.Context, t flow.Task) (any, error) {
			previous, _ := jobcontext.Run(ctx)
			if slices.ContainsFunc(previous.Outcome.Targets, func(target scheduler.TargetOutcome) bool { return target.ID == systemVolumePlanTarget }) {
				return s.resumeSelection(ctx, previous, t.ActivityID())
			}
			var policyID string
			if decodeErr := t.Payload(&policyID); decodeErr != nil {
				return nil, decodeErr
			}
			return s.runScheduledSystemVolumeBackup(ctx, policyID, t.ActivityID())
		}), workflow.WithMaxAttempts(1))},
	})
	return err
}

func (s *Service) loadSystemVolumeBackupPolicies() (*backuptypes.SystemVolumeBackupPolicyCollection, error) {
	var collection backuptypes.SystemVolumeBackupPolicyCollection
	if raw := strings.TrimSpace(s.settings.GetSettingsConfig().SystemVolumeBackupConfig.Value); raw != "" {
		if err := json.Unmarshal([]byte(raw), &collection); err != nil {
			return nil, fmt.Errorf("decode system-managed volume backup policies: %w", err)
		}
	}
	return &backuptypes.SystemVolumeBackupPolicyCollection{
		Policies: kit.Ternary(collection.Policies == nil, []backuptypes.SystemVolumeBackupPolicy{}, collection.Policies),
	}, nil
}

func (s *Service) systemVolumeBackupPolicy(policyID string) (*backuptypes.SystemVolumeBackupPolicy, error) {
	collection, err := s.loadSystemVolumeBackupPolicies()
	if err != nil {
		return nil, err
	}
	index := slices.IndexFunc(collection.Policies, func(policy backuptypes.SystemVolumeBackupPolicy) bool { return policy.ID == policyID })
	if index < 0 {
		return nil, nil
	}
	return &collection.Policies[index], nil
}

func (s *Service) GetConfig(ctx context.Context) (*backuptypes.SystemVolumeBackupPolicyCollection, error) {
	collection, err := s.loadSystemVolumeBackupPolicies()
	if err != nil {
		return nil, err
	}
	destinations := s.s3Destinations.DestinationsByID(ctx)
	policies := make([]backuptypes.SystemVolumeBackupPolicy, 0, len(collection.Policies))
	for _, policy := range collection.Policies {
		var lastRun volume.VolumeBackup
		runErr := s.db.WithContext(ctx).
			Where("policy_id LIKE ?", backuptypes.SystemVolumePolicyPrefix+policy.ID+":%").
			Order("created_at DESC").First(&lastRun).Error
		if runErr != nil && !errors.Is(runErr, gorm.ErrRecordNotFound) {
			return nil, fmt.Errorf("load latest system-managed volume backup: %w", runErr)
		}
		policies = append(policies, backuptypes.SystemVolumeBackupPolicy{ //exhaustruct:enforce
			ID:                policy.ID,
			Enabled:           policy.Enabled,
			Schedule:          policy.Schedule,
			RetentionCount:    policy.RetentionCount,
			StopContainers:    policy.StopContainers,
			LocalEnabled:      policy.LocalEnabled,
			S3Enabled:         policy.S3Enabled,
			S3DestinationID:   policy.S3DestinationID,
			S3DestinationName: destinations[policy.S3DestinationID].Name,
			SelectionMode:     policy.SelectionMode,
			VolumeNames:       policy.VolumeNames,
			IgnoreAnonymous:   policy.IgnoreAnonymous,
			LastRun: func() *backuptypes.SystemBackupRun {
				if runErr != nil {
					return policy.LastRun
				}
				return &backuptypes.SystemBackupRun{
					ID:                lastRun.ID,
					Size:              lastRun.Size,
					CreatedAt:         lastRun.CreatedAt,
					Status:            string(lastRun.Status),
					Trigger:           string(lastRun.Trigger),
					Destination:       backuptypes.SystemBackupDestination(lastRun.Destination),
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
	return &backuptypes.SystemVolumeBackupPolicyCollection{Policies: policies}, nil
}

func (s *Service) normalizeSystemVolumePolicyUpdate(ctx context.Context, input backuptypes.UpdateSystemVolumeBackupPolicy) (backuptypes.SystemVolumeBackupPolicy, error) {
	if !slices.Contains(
		[]backuptypes.SystemVolumeSelectionMode{backuptypes.SystemVolumeSelectionAll, backuptypes.SystemVolumeSelectionAllowlist, backuptypes.SystemVolumeSelectionBlocklist},
		input.SelectionMode,
	) {
		return backuptypes.SystemVolumeBackupPolicy{}, errors.New("selectionMode must be all, allowlist, or blocklist")
	}
	update, err := backup.ValidatePolicyUpdate(ctx, "system-managed volume", input.UpdateBackupPolicy, s.s3Destinations)
	if err != nil {
		return backuptypes.SystemVolumeBackupPolicy{}, err
	}
	names := kit.Unique(kit.TrimNonEmpty(input.VolumeNames))
	slices.Sort(names)
	return backuptypes.SystemVolumeBackupPolicy{
		ID:              input.ID,
		Enabled:         update.Enabled,
		Schedule:        update.Schedule,
		RetentionCount:  update.RetentionCount,
		StopContainers:  update.StopContainers,
		LocalEnabled:    update.LocalEnabled,
		S3Enabled:       update.S3Enabled,
		S3DestinationID: update.S3DestinationID,
		SelectionMode:   input.SelectionMode,
		VolumeNames:     kit.Ternary(input.SelectionMode == backuptypes.SystemVolumeSelectionAll || names == nil, []string{}, names),
		IgnoreAnonymous: input.IgnoreAnonymous,
	}, nil
}

func (s *Service) UpdateConfig(ctx context.Context, updates []backuptypes.UpdateSystemVolumeBackupPolicy) (*backuptypes.SystemVolumeBackupPolicyCollection, error) {
	existing, err := s.loadSystemVolumeBackupPolicies()
	if err != nil {
		return nil, err
	}
	if runErr := (backup.PolicyReconciliation[backuptypes.SystemVolumeBackupPolicy, backuptypes.UpdateSystemVolumeBackupPolicy]{
		Domain:   "system-managed volume",
		Existing: existing.Policies,
		ID:       func(policy *backuptypes.SystemVolumeBackupPolicy) string { return policy.ID },
		UpdateID: func(update backuptypes.UpdateSystemVolumeBackupPolicy) string { return update.ID },
		New: func() backuptypes.SystemVolumeBackupPolicy {
			return backuptypes.SystemVolumeBackupPolicy{ID: uuid.New().String()}
		},
		Build: func(ctx context.Context, policy *backuptypes.SystemVolumeBackupPolicy, update backuptypes.UpdateSystemVolumeBackupPolicy) error {
			normalized, normalizeErr := s.normalizeSystemVolumePolicyUpdate(ctx, update)
			if normalizeErr != nil {
				return normalizeErr
			}
			*policy = backuptypes.SystemVolumeBackupPolicy{ //exhaustruct:enforce
				ID:                policy.ID,
				Enabled:           normalized.Enabled,
				Schedule:          normalized.Schedule,
				RetentionCount:    normalized.RetentionCount,
				StopContainers:    normalized.StopContainers,
				LocalEnabled:      normalized.LocalEnabled,
				S3Enabled:         normalized.S3Enabled,
				S3DestinationID:   normalized.S3DestinationID,
				S3DestinationName: normalized.S3DestinationName,
				SelectionMode:     normalized.SelectionMode,
				VolumeNames:       normalized.VolumeNames,
				IgnoreAnonymous:   normalized.IgnoreAnonymous,
				LastRun:           normalized.LastRun,
			}
			return nil
		},
		Persist: func(ctx context.Context, policies []backuptypes.SystemVolumeBackupPolicy) error {
			encoded, encodeErr := json.Marshal(backuptypes.SystemVolumeBackupPolicyCollection{Policies: policies})
			if encodeErr != nil {
				return fmt.Errorf("encode policies: %w", encodeErr)
			}
			return s.settings.UpdateSetting(ctx, systemVolumeBackupConfigKey, string(encoded))
		},
		Unregister: func(ctx context.Context, policyID string) {
			s.jobs.Unregister(ctx, systemVolumeBackupJobPrefix+policyID)
		},
		Reschedule: s.rescheduleSystemVolumeBackup,
	}).Run(ctx, updates); runErr != nil {
		return nil, runErr
	}
	return s.GetConfig(ctx)
}

// ListOptions returns live choices plus configured names that are currently unavailable.
func (s *Service) ListOptions(ctx context.Context) ([]backuptypes.SystemVolumeBackupOption, error) {
	options, err := s.volumes.ListBackupVolumeOptions(ctx)
	if err != nil {
		return nil, err
	}
	collection, err := s.loadSystemVolumeBackupPolicies()
	if err != nil {
		return nil, err
	}
	known := make(map[string]struct{}, len(options))
	for _, option := range options {
		known[option.Name] = struct{}{}
	}
	for _, policy := range collection.Policies {
		for _, name := range policy.VolumeNames {
			if _, ok := known[name]; !ok {
				options = append(options, backuptypes.SystemVolumeBackupOption{Name: name})
				known[name] = struct{}{}
			}
		}
	}
	slices.SortFunc(options, func(a, b backuptypes.SystemVolumeBackupOption) int {
		return strings.Compare(strings.ToLower(a.Name), strings.ToLower(b.Name))
	})
	return options, nil
}

// prepareSystemVolumeBackups resolves the requested policy, takes the system admission, and selects the live volumes.
func (s *Service) prepareSystemVolumeBackups(ctx context.Context, request backuptypes.RunSystemVolumeBackupsRequest) (selectionInput, *runs.Lease, error) {
	if request.PolicyID != "" && request.Custom != nil {
		return selectionInput{}, nil, common.Classify(common.ErrValidation, errors.New("select a saved policy or custom configuration, not both"))
	}
	if request.Custom != nil && !slices.Contains(
		[]backuptypes.SystemBackupDestination{backuptypes.SystemBackupDestinationLocal, backuptypes.SystemBackupDestinationS3, backuptypes.SystemBackupDestinationLocalS3},
		request.Custom.Destination,
	) {
		return selectionInput{}, nil, common.Classify(common.ErrValidation, errors.New("destination must be local, s3, or local_s3"))
	}
	policy, err := func() (*backuptypes.SystemVolumeBackupPolicy, error) {
		if request.PolicyID != "" {
			stored, loadErr := s.systemVolumeBackupPolicy(request.PolicyID)
			if loadErr == nil && stored == nil {
				return nil, common.Classify(common.ErrNotFound, errors.New("system-managed volume backup policy not found"))
			}
			return stored, loadErr
		}
		if request.Custom == nil {
			normalized, normalizeErr := s.normalizeSystemVolumePolicyUpdate(ctx, backuptypes.UpdateSystemVolumeBackupPolicy{
				UpdateBackupPolicy: backuptypes.UpdateBackupPolicy{
					Enabled:      true,
					Schedule:     defaultSystemVolumeSchedule,
					LocalEnabled: true,
				},
				SelectionMode:   backuptypes.SystemVolumeSelectionAll,
				VolumeNames:     []string{},
				IgnoreAnonymous: true,
			})
			return &normalized, normalizeErr
		}
		normalized, normalizeErr := s.normalizeSystemVolumePolicyUpdate(ctx, backuptypes.UpdateSystemVolumeBackupPolicy{
			UpdateBackupPolicy: backuptypes.UpdateBackupPolicy{
				Enabled:         true,
				Schedule:        defaultSystemVolumeSchedule,
				StopContainers:  request.Custom.StopContainers,
				LocalEnabled:    request.Custom.Destination == backuptypes.SystemBackupDestinationLocal || request.Custom.Destination == backuptypes.SystemBackupDestinationLocalS3,
				S3Enabled:       request.Custom.Destination == backuptypes.SystemBackupDestinationS3 || request.Custom.Destination == backuptypes.SystemBackupDestinationLocalS3,
				S3DestinationID: request.Custom.S3DestinationID,
			},
			SelectionMode:   request.Custom.SelectionMode,
			VolumeNames:     request.Custom.VolumeNames,
			IgnoreAnonymous: request.Custom.IgnoreAnonymous,
		})
		return &normalized, normalizeErr
	}()
	if err != nil {
		return selectionInput{}, nil, err
	}
	lease, admitted, err := s.engine.TryAcquireRun(ctx, backup.SystemAdmissionScope, backup.SystemAdmissionID)
	if err != nil {
		return selectionInput{}, nil, err
	}
	if !admitted {
		return selectionInput{}, nil, s.alreadyRunning
	}
	options, err := s.volumes.ListBackupVolumeOptions(ctx)
	if err != nil {
		lease.Release(ctx)
		return selectionInput{}, nil, err
	}
	return selectionInput{
		Policy:       *policy,
		ManualPolicy: request.PolicyID == "",
		Candidates:   selectSystemVolumeBackupCandidates(*policy, options),
	}, lease, nil
}

func (s *Service) executeSystemVolumeBackups(
	ctx context.Context, input selectionInput, trigger volume.VolumeBackupTrigger, activityID string,
) (result *backuptypes.SystemVolumeBackupRunResult, err error) {
	succeeded, failed, skipped, failures := 0, 0, 0, make([]backuptypes.SystemVolumeBackupFailure, 0)
	reportProgress := func(ctx context.Context) {
		if activityID == "" {
			return
		}
		names := make([]string, len(input.Candidates))
		for i, candidate := range input.Candidates {
			names[i] = candidate.Name
		}
		progress := 100
		if len(input.Candidates) > 0 {
			progress = 100 * (succeeded + failed + skipped) / len(input.Candidates)
		}
		_, updateErr := s.activity.UpdateActivity(ctx, activityID, activitylib.UpdateRequest{
			Progress: &progress,
			Metadata: database.JSON{
				"action":      "run_system_volume_backups",
				"policyId":    input.Policy.ID,
				"volumeNames": names,
				"matched":     len(input.Candidates),
				"succeeded":   succeeded,
				"failed":      failed,
				"skipped":     skipped,
				"failures":    failures,
			},
		})
		if updateErr != nil {
			slog.WarnContext(ctx, "Failed to report system-managed volume backup progress", "activityId", activityID, "policyId", input.Policy.ID, "error", updateErr)
		}
	}
	defer reportProgress(context.WithoutCancel(ctx))
	defer func() {
		result = &backuptypes.SystemVolumeBackupRunResult{
			Matched:   len(input.Candidates),
			Succeeded: succeeded,
			Failed:    failed,
			Skipped:   skipped,
			Failures:  failures,
		}
	}()
	defer utils.RecoverToError(&err, "system-managed volume backup")

	policy := backuptypes.UpdateBackupPolicy{
		Enabled: true, Schedule: input.Policy.Schedule, RetentionCount: input.Policy.RetentionCount,
		StopContainers: input.Policy.StopContainers, LocalEnabled: input.Policy.LocalEnabled, S3Enabled: input.Policy.S3Enabled,
		S3DestinationID: input.Policy.S3DestinationID,
	}
	previous, _ := jobcontext.Run(ctx)
	for _, candidate := range input.Candidates {
		done := slices.IndexFunc(previous.Outcome.Targets, func(target scheduler.TargetOutcome) bool {
			return target.ID == candidate.Name && (target.Status == scheduler.Succeeded || target.Status == scheduler.Skipped)
		})
		if done >= 0 && previous.Outcome.Targets[done].Status == scheduler.Succeeded {
			succeeded++
			continue
		}
		if done >= 0 {
			skipped++
			continue
		}
		if cancellationErr := ctx.Err(); cancellationErr != nil {
			return nil, cancellationErr
		}
		reportProgress(ctx)
		overridden, backupErr := s.volumes.HasEnabledBackupPolicy(ctx, candidate.Name)
		if backupErr == nil && overridden {
			if skipProgressErr := jobcontext.Progress(ctx, scheduler.TargetOutcome{ID: candidate.Name, Status: scheduler.Skipped}); skipProgressErr != nil {
				return nil, skipProgressErr
			}
			skipped++
			continue
		}
		if backupErr == nil {
			seriesID := backuptypes.SystemVolumePolicyPrefix + kit.Ternary(input.ManualPolicy, "manual", input.Policy.ID) + ":" + kit.SHA256Hex(candidate.Name)[:16]
			_, backupErr = s.volumes.CreateSystemManagedBackup(ctx, candidate.Name, usertypes.SystemUser, trigger, seriesID, policy)
		}
		if errors.Is(backupErr, volume.ErrVolumeBackupAlreadyRunning) {
			skipped++
			continue
		}
		if backupErr != nil && ctx.Err() != nil {
			// Shutdown interrupted this volume; hand the task back instead of counting it failed.
			return nil, ctx.Err()
		}
		if backupErr != nil {
			failed++
			failures = append(failures, backuptypes.SystemVolumeBackupFailure{
				VolumeName: candidate.Name,
				Error:      backupErr.Error(),
			})
			continue
		}
		if progressErr := jobcontext.Progress(ctx, scheduler.TargetOutcome{ID: candidate.Name, Status: scheduler.Succeeded}); progressErr != nil {
			return nil, progressErr
		}
		succeeded++
	}
	return nil, nil
}

func (s *Service) runScheduledSystemVolumeBackup(ctx context.Context, policyID, activityID string) (scheduler.Outcome, error) {
	policy, err := s.systemVolumeBackupPolicy(policyID)
	if err != nil {
		return scheduler.Outcome{}, err
	}
	if policy == nil || !policy.Enabled {
		return scheduler.Outcome{Status: scheduler.Skipped}, nil
	}
	remoteErr := func() error {
		if !policy.S3Enabled {
			return nil
		}
		root := path.Join(backup.VolumeRoot, s.settings.GetSettingsConfig().InstanceID.Value)
		return backup.CheckScheduledRemote(ctx, s.db, s.s3Destinations, "volume_backups", policy.S3DestinationID, root)
	}()
	remoteDisabled, err := backup.DisableMissingRemote(remoteErr, policy.LocalEnabled, &policy.S3Enabled, &policy.Enabled, func(field string) (bool, error) {
		collection, loadErr := s.loadSystemVolumeBackupPolicies()
		if loadErr != nil {
			return false, loadErr
		}
		index := slices.IndexFunc(collection.Policies, func(current backuptypes.SystemVolumeBackupPolicy) bool {
			return current.ID == policy.ID && current.S3DestinationID == policy.S3DestinationID && current.S3Enabled == policy.S3Enabled &&
				current.Enabled == policy.Enabled && current.LocalEnabled == policy.LocalEnabled
		})
		if index < 0 {
			return false, nil
		}
		current := collection.Policies[index]
		collection.Policies[index] = backuptypes.SystemVolumeBackupPolicy{ //exhaustruct:enforce
			ID:                current.ID,
			Enabled:           current.Enabled && field != "enabled",
			Schedule:          current.Schedule,
			RetentionCount:    current.RetentionCount,
			StopContainers:    current.StopContainers,
			LocalEnabled:      current.LocalEnabled,
			S3Enabled:         current.S3Enabled && field != "s3_enabled",
			S3DestinationID:   current.S3DestinationID,
			S3DestinationName: current.S3DestinationName,
			SelectionMode:     current.SelectionMode,
			VolumeNames:       current.VolumeNames,
			IgnoreAnonymous:   current.IgnoreAnonymous,
			LastRun:           current.LastRun,
		}
		encoded, encodeErr := json.Marshal(collection)
		if encodeErr != nil {
			return false, fmt.Errorf("encode policies: %w", encodeErr)
		}
		return true, s.settings.UpdateSetting(ctx, systemVolumeBackupConfigKey, string(encoded))
	})
	if err != nil {
		return scheduler.Outcome{}, err
	}
	if remoteDisabled {
		s.rescheduleSystemVolumeBackup(ctx, policy)
	}
	if remoteDisabled && !policy.LocalEnabled {
		return scheduler.Outcome{Status: scheduler.NeedsAttention, Message: backup.RemoteDisabledMessage}, nil
	}
	result, err := func() (*backuptypes.SystemVolumeBackupRunResult, error) {
		input, lease, prepareErr := s.prepareSystemVolumeBackups(ctx, backuptypes.RunSystemVolumeBackupsRequest{PolicyID: policyID})
		if prepareErr != nil {
			return nil, prepareErr
		}
		defer lease.Release(ctx)
		frozen, marshalErr := json.Marshal(input)
		if marshalErr != nil {
			return nil, marshalErr
		}
		if progressErr := jobcontext.Progress(ctx, scheduler.TargetOutcome{
			ResourceType: "backup_plan",
			ID:           systemVolumePlanTarget,
			Status:       scheduler.Succeeded,
			RecoveryData: frozen,
		}); progressErr != nil {
			return nil, progressErr
		}
		return s.executeSystemVolumeBackups(ctx, input, volume.VolumeBackupTriggerScheduled, activityID)
	}()
	if errors.Is(err, s.alreadyRunning) {
		slog.InfoContext(ctx, "Scheduled system-managed volume backups skipped; a system backup is running", "policyId", policyID)
		return scheduler.Outcome{Status: scheduler.Skipped, Message: "Skipped: a system backup is running"}, nil
	}
	if err != nil {
		slog.ErrorContext(ctx, "Scheduled system-managed volume backups failed", "policyId", policyID, "error", err)
		return scheduler.Outcome{}, err
	}
	slog.InfoContext(ctx, "Scheduled system-managed volume backups completed", "policyId", policyID,
		"matched", result.Matched, "succeeded", result.Succeeded, "failed", result.Failed, "skipped", result.Skipped)
	return selectionOutcome(result, remoteDisabled), nil
}

// resumeSelection finishes the frozen selection an interrupted scheduled run recorded.
func (s *Service) resumeSelection(ctx context.Context, previous scheduler.Run, activityID string) (scheduler.Outcome, error) {
	index := slices.IndexFunc(previous.Outcome.Targets, func(target scheduler.TargetOutcome) bool {
		return target.ID == systemVolumePlanTarget && len(target.RecoveryData) > 0
	})
	if index < 0 {
		return scheduler.Outcome{Status: scheduler.NeedsAttention, Message: "The interrupted volume backup has no frozen selection"}, nil
	}
	var input selectionInput
	if err := json.Unmarshal(previous.Outcome.Targets[index].RecoveryData, &input); err != nil {
		return scheduler.Outcome{}, err
	}
	result, err := s.backUpSelection(ctx, input, volume.VolumeBackupTriggerScheduled, activityID)
	if err != nil {
		return scheduler.Outcome{Status: scheduler.NeedsAttention, Message: err.Error()}, err
	}
	return selectionOutcome(result, false), nil
}

// backUpSelection takes over the system admission, resumes the selection's interrupted volumes, and backs up the rest.
func (s *Service) backUpSelection(ctx context.Context, input selectionInput, trigger volume.VolumeBackupTrigger, activityID string) (*backuptypes.SystemVolumeBackupRunResult, error) {
	lease, admitted, err := s.engine.AcquireRun(ctx, backup.SystemAdmissionScope, backup.SystemAdmissionID, input.Claim)
	if err != nil {
		return nil, err
	}
	if !admitted {
		return nil, s.alreadyRunning
	}
	defer lease.Release(ctx)
	if input.Requester != nil {
		if authorizeErr := s.engine.Authorize(ctx, *input.Requester); authorizeErr != nil {
			return nil, authorizeErr
		}
	}
	previous, _ := jobcontext.Run(ctx)
	for _, candidate := range input.Candidates {
		if !slices.ContainsFunc(previous.Outcome.Targets, func(target scheduler.TargetOutcome) bool {
			return target.ID == candidate.Name && target.Status != scheduler.Succeeded && target.Status != scheduler.Skipped
		}) {
			continue
		}
		outcome, recoveryErr := s.volumes.ReconcileBackup(ctx, previous, candidate.Name)
		if errors.Is(recoveryErr, backup.ErrBackupSettled) {
			// That volume's backup already failed, so the selection backs it up again below.
			continue
		}
		if recoveryErr != nil {
			return nil, recoveryErr
		}
		if outcome.Status != scheduler.Succeeded {
			return nil, errors.New(outcome.Message)
		}
		for i, target := range previous.Outcome.Targets {
			if target.ID == candidate.Name {
				previous.Outcome.Targets[i] = scheduler.TargetOutcome{ //exhaustruct:enforce
					RecoveryData: target.RecoveryData,
					ResourceType: target.ResourceType,
					ID:           target.ID,
					Status:       scheduler.Succeeded,
					Message:      target.Message,
					ActivityID:   target.ActivityID,
				}
			}
		}
	}
	return s.executeSystemVolumeBackups(
		jobcontext.WithExecution(ctx, previous, func(target scheduler.TargetOutcome) error { return jobcontext.Progress(ctx, target) }),
		input,
		trigger,
		activityID,
	)
}

// selectionOutcome reports a run's result; failed volumes or disabled remote storage make it partial.
func selectionOutcome(result *backuptypes.SystemVolumeBackupRunResult, remoteDisabled bool) scheduler.Outcome {
	var targets []scheduler.TargetOutcome
	for _, failure := range result.Failures {
		targets = append(targets, scheduler.TargetOutcome{
			ID:      failure.VolumeName,
			Status:  scheduler.Failed,
			Message: failure.Error,
		})
	}
	return scheduler.Outcome{
		Status: kit.Ternary(result.Failed > 0 || remoteDisabled, scheduler.Partial, scheduler.Succeeded),
		Message: cmp.Or(
			kit.Ternary(remoteDisabled, backup.RemoteDisabledMessage, ""),
			kit.Ternary(result.Failed > 0, fmt.Sprintf("%d volume backups failed", result.Failed), ""),
			"System-managed volume backups completed",
		),
		Targets: targets,
	}
}

func (s *Service) rescheduleSystemVolumeBackup(ctx context.Context, policy *backuptypes.SystemVolumeBackupPolicy) {
	if !policy.Enabled {
		s.jobs.Unregister(ctx, systemVolumeBackupJobPrefix+policy.ID)
		return
	}
	policyID := policy.ID
	s.jobs.Add(ctx, &flow.Job{
		Engine:   s.flow,
		Workflow: s.policyWorkflow,
		JobName:  s.jobs.JobName(systemVolumeBackupJobPrefix + policyID),
		Payload:  policyID,
		Activity: activitylib.StartRequest{
			Metadata: database.JSON{
				"action":   "scheduled_system_volume_backups",
				"policyId": policyID,
				"schedule": policy.Schedule,
			},
		},
		ScheduleFn: func(context.Context) string {
			current, err := s.systemVolumeBackupPolicy(policyID)
			if err != nil || current == nil {
				return defaultSystemVolumeSchedule
			}
			return current.Schedule
		},
		FallbackFn: func(ctx context.Context, previous scheduler.Run) (scheduler.Outcome, error) {
			return s.resumeSelection(ctx, previous, "")
		},
	})
}

// Start freezes the selected policy and volumes, then submits their workflow.
func (s *Service) Start(ctx context.Context, user usertypes.Actor, request backuptypes.RunSystemVolumeBackupsRequest) (*backuptypes.BackupRunAccepted, error) {
	requester := backup.NewRequester(ctx, user, "0", authz.PermSystemBackupsManage)
	if err := s.engine.Authorize(ctx, requester); err != nil {
		return nil, err
	}
	prepared, lease, err := s.prepareSystemVolumeBackups(ctx, request)
	if err != nil {
		return nil, err
	}
	names := make([]string, len(prepared.Candidates))
	for i, candidate := range prepared.Candidates {
		names[i] = candidate.Name
	}
	input := selectionInput{
		Policy:       prepared.Policy,
		ManualPolicy: prepared.ManualPolicy,
		Candidates:   prepared.Candidates,
		Requester:    &requester,
		Claim:        uuid.NewV7().String(),
	}
	release := s.engine.Hold(backup.SystemAdmissionScope, backup.SystemAdmissionID, input.Claim, lease)
	activityID, err := s.flow.Submit(ctx, s.backupWorkflow, input, activitylib.StartRequest{
		StartedBy: &user, Step: "Backing up volumes", LatestMessage: "Creating system-managed volume backups",
		Metadata: database.JSON{
			"action": "run_system_volume_backups", "policyId": input.Policy.ID, "volumeNames": names, "matched": len(names),
			"succeeded": 0, "failed": 0, "skipped": 0, "failures": []backuptypes.SystemVolumeBackupFailure{},
		},
	})
	if err != nil {
		release(ctx)
		return nil, err
	}
	// A run canceled before its task starts never takes the parked lease.
	go func() {
		_, _ = s.flow.Wait(context.WithoutCancel(ctx), s.backupWorkflow, activityID)
		release(context.WithoutCancel(ctx))
	}()
	return &backuptypes.BackupRunAccepted{ActivityID: activityID, Status: "running"}, nil
}

// RegisterJobsOnStartup schedules every saved policy and returns how many were loaded.
func (s *Service) RegisterJobsOnStartup(ctx context.Context) int {
	policies, err := s.loadSystemVolumeBackupPolicies()
	if err != nil {
		slog.ErrorContext(ctx, "Failed to load system-managed volume backup policies", "error", err)
		return 0
	}
	for i := range policies.Policies {
		s.rescheduleSystemVolumeBackup(ctx, &policies.Policies[i])
	}
	return len(policies.Policies)
}

func selectSystemVolumeBackupCandidates(policy backuptypes.SystemVolumeBackupPolicy, options []backuptypes.SystemVolumeBackupOption) []backuptypes.SystemVolumeBackupOption {
	result := make([]backuptypes.SystemVolumeBackupOption, 0, len(options))
	for _, option := range options {
		if !option.Available {
			continue
		}
		selected := slices.Contains(policy.VolumeNames, option.Name)
		matches := policy.SelectionMode == backuptypes.SystemVolumeSelectionAll ||
			(policy.SelectionMode == backuptypes.SystemVolumeSelectionAllowlist && selected) ||
			(policy.SelectionMode == backuptypes.SystemVolumeSelectionBlocklist && !selected)
		if matches && (!policy.IgnoreAnonymous || !option.Anonymous) {
			result = append(result, option)
		}
	}
	return result
}
