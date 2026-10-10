package volumes

import (
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
	template := activitylib.StartRequest{Type: activitytypes.TypeResourceAction, EnvironmentID: "0", ResourceType: new("system_backup"), ResourceID: new("volumes"), ResourceName: new("Volumes")}
	s.backupWorkflow, err = engine.Define(flow.Definition{
		Name:        "system-volume-backup",
		Version:     1,
		Fingerprint: "9b4a5e46a984446bc08df39a71e563e031548db3af1825a2413e8c96515db6ab",
		Concurrency: 1,
		Timeout:     24 * time.Hour,
		Activity:    template,
		Labels:      map[string]string{"backup": "Backing up volumes"},
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
	template.StartedBy, template.Step, template.LatestMessage = &usertypes.SystemUser, "Backing up volumes", "Creating scheduled system-managed volume backups"
	s.policyWorkflow, err = engine.Define(flow.Definition{
		Name:        "system-volume-backup-policy",
		Version:     1,
		Fingerprint: "94a13d1bac530e6eddcb73c27990a07d69915dc947ce5b559166eb3fb85e39ef",
		Concurrency: 1,
		Timeout:     24 * time.Hour,
		Activity:    template,
		Labels:      map[string]string{"backup": "Backing up volumes"},
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
	collection := &backuptypes.SystemVolumeBackupPolicyCollection{Policies: []backuptypes.SystemVolumeBackupPolicy{}}
	if s.settings == nil {
		return collection, nil
	}
	raw := strings.TrimSpace(s.settings.GetSettingsConfig().SystemVolumeBackupConfig.Value)
	if raw == "" {
		return collection, nil
	}
	if err := json.Unmarshal([]byte(raw), collection); err != nil {
		return nil, fmt.Errorf("decode system-managed volume backup policies: %w", err)
	}
	if collection.Policies == nil {
		collection.Policies = []backuptypes.SystemVolumeBackupPolicy{}
	}
	return collection, nil
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
	for i := range collection.Policies {
		policy := &collection.Policies[i]
		policy.S3DestinationName = destinations[policy.S3DestinationID].Name
		if s.db == nil {
			continue
		}
		var lastRun volume.VolumeBackup
		runErr := s.db.WithContext(ctx).
			Where("policy_id LIKE ?", backuptypes.SystemVolumePolicyPrefix+policy.ID+":%").
			Order("created_at DESC").First(&lastRun).Error
		if runErr == nil {
			policy.LastRun = &backuptypes.SystemBackupRun{
				ID: lastRun.ID, Size: lastRun.Size, CreatedAt: lastRun.CreatedAt, Status: string(lastRun.Status),
				Trigger: string(lastRun.Trigger), Destination: backuptypes.SystemBackupDestination(lastRun.Destination),
				LocalSnapshotID: lastRun.LocalSnapshotID, RemoteSnapshotID: lastRun.RemoteSnapshotID,
				S3DestinationID: lastRun.S3DestinationID, S3DestinationName: destinations[lastRun.S3DestinationID].Name,
				PolicyID: lastRun.PolicyID, Error: lastRun.Error,
			}
		} else if !errors.Is(runErr, gorm.ErrRecordNotFound) {
			return nil, fmt.Errorf("load latest system-managed volume backup: %w", runErr)
		}
	}
	return collection, nil
}

func (s *Service) normalizeSystemVolumePolicyUpdate(ctx context.Context, input backuptypes.UpdateSystemVolumeBackupPolicy) (backuptypes.SystemVolumeBackupPolicy, error) {
	mode := input.SelectionMode
	if mode != backuptypes.SystemVolumeSelectionAll && mode != backuptypes.SystemVolumeSelectionAllowlist && mode != backuptypes.SystemVolumeSelectionBlocklist {
		return backuptypes.SystemVolumeBackupPolicy{}, errors.New("selectionMode must be all, allowlist, or blocklist")
	}
	update, err := backup.ValidatePolicyUpdate(ctx, "system-managed volume", input.UpdateBackupPolicy, s.s3Destinations)
	if err != nil {
		return backuptypes.SystemVolumeBackupPolicy{}, err
	}
	names := kit.Unique(kit.TrimNonEmpty(input.VolumeNames))
	slices.Sort(names)
	if mode == backuptypes.SystemVolumeSelectionAll || names == nil {
		names = []string{}
	}
	return backuptypes.SystemVolumeBackupPolicy{
		ID: input.ID, Enabled: update.Enabled, Schedule: update.Schedule, RetentionCount: update.RetentionCount,
		StopContainers: update.StopContainers, LocalEnabled: update.LocalEnabled, S3Enabled: update.S3Enabled,
		S3DestinationID: update.S3DestinationID, SelectionMode: mode,
		VolumeNames: names, IgnoreAnonymous: input.IgnoreAnonymous,
	}, nil
}

func (s *Service) UpdateConfig(ctx context.Context, updates []backuptypes.UpdateSystemVolumeBackupPolicy) (*backuptypes.SystemVolumeBackupPolicyCollection, error) {
	if s.settings == nil {
		return nil, errors.New("settings service is unavailable")
	}
	existing, err := s.loadSystemVolumeBackupPolicies()
	if err != nil {
		return nil, err
	}
	reconcile := backup.PolicyReconciliation[backuptypes.SystemVolumeBackupPolicy, backuptypes.UpdateSystemVolumeBackupPolicy]{
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
			normalized.ID = policy.ID
			*policy = normalized
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
	}
	if runErr := reconcile.Run(ctx, updates); runErr != nil {
		return nil, runErr
	}
	return s.GetConfig(ctx)
}

// ListOptions returns live choices plus configured names that are currently unavailable.
func (s *Service) ListOptions(ctx context.Context) ([]backuptypes.SystemVolumeBackupOption, error) {
	if s.volumes == nil {
		return nil, errors.New("volume service is unavailable")
	}
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
	if s.volumes == nil {
		return selectionInput{}, nil, errors.New("volume service is unavailable")
	}
	var input selectionInput
	switch {
	case request.PolicyID != "" && request.Custom != nil:
		return selectionInput{}, nil, errors.New("select a saved policy or custom configuration, not both")
	case request.PolicyID != "":
		policy, err := s.systemVolumeBackupPolicy(request.PolicyID)
		if err != nil {
			return selectionInput{}, nil, err
		}
		if policy == nil {
			return selectionInput{}, nil, errors.New("system-managed volume backup policy not found")
		}
		input.Policy = *policy
	default:
		custom := backuptypes.UpdateSystemVolumeBackupPolicy{
			Enabled: true, Schedule: defaultSystemVolumeSchedule, LocalEnabled: true,
			SelectionMode: backuptypes.SystemVolumeSelectionAll, VolumeNames: []string{}, IgnoreAnonymous: true,
		}
		if run := request.Custom; run != nil {
			localEnabled := run.Destination == backuptypes.SystemBackupDestinationLocal || run.Destination == backuptypes.SystemBackupDestinationLocalS3
			s3Enabled := run.Destination == backuptypes.SystemBackupDestinationS3 || run.Destination == backuptypes.SystemBackupDestinationLocalS3
			if !localEnabled && !s3Enabled {
				return selectionInput{}, nil, errors.New("destination must be local, s3, or local_s3")
			}
			custom = backuptypes.UpdateSystemVolumeBackupPolicy{
				Enabled: true, Schedule: defaultSystemVolumeSchedule, StopContainers: run.StopContainers,
				LocalEnabled: localEnabled, S3Enabled: s3Enabled, S3DestinationID: run.S3DestinationID,
				SelectionMode: run.SelectionMode, VolumeNames: run.VolumeNames, IgnoreAnonymous: run.IgnoreAnonymous,
			}
		}
		policy, err := s.normalizeSystemVolumePolicyUpdate(ctx, custom)
		if err != nil {
			return selectionInput{}, nil, err
		}
		input.Policy, input.ManualPolicy = policy, true
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
	input.Candidates = selectSystemVolumeBackupCandidates(input.Policy, options)
	return input, lease, nil
}

func (s *Service) executeSystemVolumeBackups(
	ctx context.Context, input selectionInput, trigger volume.VolumeBackupTrigger, activityID string,
) (result *backuptypes.SystemVolumeBackupRunResult, err error) {
	result = &backuptypes.SystemVolumeBackupRunResult{Matched: len(input.Candidates), Failures: make([]backuptypes.SystemVolumeBackupFailure, 0)}
	reportProgress := func(ctx context.Context) {
		if activityID == "" {
			return
		}
		names := make([]string, len(input.Candidates))
		for i, candidate := range input.Candidates {
			names[i] = candidate.Name
		}
		progress := 100
		if result.Matched > 0 {
			progress = 100 * (result.Succeeded + result.Failed + result.Skipped) / result.Matched
		}
		_, updateErr := s.activity.UpdateActivity(ctx, activityID, activitylib.UpdateRequest{Progress: &progress, Metadata: database.JSON{
			"action": "run_system_volume_backups", "policyId": input.Policy.ID, "volumeNames": names,
			"matched": result.Matched, "succeeded": result.Succeeded, "failed": result.Failed, "skipped": result.Skipped, "failures": result.Failures,
		}})
		if updateErr != nil {
			slog.WarnContext(ctx, "Failed to report system-managed volume backup progress", "activityId", activityID, "policyId", input.Policy.ID, "error", updateErr)
		}
	}
	defer reportProgress(context.WithoutCancel(ctx))
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
			result.Succeeded++
			continue
		}
		if done >= 0 {
			result.Skipped++
			continue
		}
		if cancellationErr := ctx.Err(); cancellationErr != nil {
			return result, cancellationErr
		}
		reportProgress(ctx)
		overridden, backupErr := s.volumes.HasEnabledBackupPolicy(ctx, candidate.Name)
		if backupErr == nil && overridden {
			if skipProgressErr := jobcontext.Progress(ctx, scheduler.TargetOutcome{ID: candidate.Name, Status: scheduler.Skipped}); skipProgressErr != nil {
				return result, skipProgressErr
			}
			result.Skipped++
			continue
		}
		if backupErr == nil {
			seriesID := backuptypes.SystemVolumePolicyPrefix + kit.Ternary(input.ManualPolicy, "manual", input.Policy.ID) + ":" + kit.SHA256Hex(candidate.Name)[:16]
			_, backupErr = s.volumes.CreateSystemManagedBackup(ctx, candidate.Name, usertypes.SystemUser, trigger, seriesID, policy)
		}
		if errors.Is(backupErr, volume.ErrVolumeBackupAlreadyRunning) {
			result.Skipped++
			continue
		}
		if backupErr != nil && ctx.Err() != nil {
			// Shutdown interrupted this volume; hand the task back instead of counting it failed.
			return result, ctx.Err()
		}
		if backupErr != nil {
			result.Failed++
			result.Failures = append(result.Failures, backuptypes.SystemVolumeBackupFailure{VolumeName: candidate.Name, Error: backupErr.Error()})
			continue
		}
		if progressErr := jobcontext.Progress(ctx, scheduler.TargetOutcome{ID: candidate.Name, Status: scheduler.Succeeded}); progressErr != nil {
			return result, progressErr
		}
		result.Succeeded++
	}
	return result, nil
}

func (s *Service) runScheduledSystemVolumeBackup(ctx context.Context, policyID, activityID string) (scheduler.Outcome, error) {
	policy, err := s.systemVolumeBackupPolicy(policyID)
	if err != nil {
		return scheduler.Outcome{}, err
	}
	if policy == nil || !policy.Enabled {
		return scheduler.Outcome{Status: scheduler.Skipped}, nil
	}
	var remoteErr error
	if policy.S3Enabled {
		root := path.Join(backup.VolumeRoot, s.settings.GetSettingsConfig().InstanceID.Value)
		remoteErr = backup.CheckScheduledRemote(ctx, s.db, s.s3Destinations, "volume_backups", policy.S3DestinationID, root)
	}
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
		current := &collection.Policies[index]
		*kit.Ternary(field == "enabled", &current.Enabled, &current.S3Enabled) = false
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
	input, lease, err := s.prepareSystemVolumeBackups(ctx, backuptypes.RunSystemVolumeBackupsRequest{PolicyID: policyID})
	if errors.Is(err, s.alreadyRunning) {
		slog.InfoContext(ctx, "Scheduled system-managed volume backups skipped; a system backup is running", "policyId", policyID)
		return scheduler.Outcome{Status: scheduler.Skipped, Message: "Skipped: a system backup is running"}, nil
	}
	var result *backuptypes.SystemVolumeBackupRunResult
	if err == nil {
		defer lease.Release(ctx)
		var frozen []byte
		if frozen, err = json.Marshal(input); err == nil {
			err = jobcontext.Progress(ctx, scheduler.TargetOutcome{ResourceType: "backup_plan", ID: systemVolumePlanTarget, Status: scheduler.Succeeded, RecoveryData: frozen})
		}
		if err == nil {
			result, err = s.executeSystemVolumeBackups(ctx, input, volume.VolumeBackupTriggerScheduled, activityID)
		}
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
		for i := range previous.Outcome.Targets {
			if previous.Outcome.Targets[i].ID == candidate.Name {
				previous.Outcome.Targets[i].Status = scheduler.Succeeded
			}
		}
	}
	progressCtx := ctx
	ctx = jobcontext.WithExecution(ctx, previous, func(target scheduler.TargetOutcome) error { return jobcontext.Progress(progressCtx, target) })
	return s.executeSystemVolumeBackups(ctx, input, trigger, activityID)
}

// selectionOutcome reports a run's result; failed volumes or disabled remote storage make it partial.
func selectionOutcome(result *backuptypes.SystemVolumeBackupRunResult, remoteDisabled bool) scheduler.Outcome {
	outcome := scheduler.Outcome{Status: scheduler.Succeeded, Message: "System-managed volume backups completed"}
	if result.Failed > 0 {
		outcome.Status, outcome.Message = scheduler.Partial, fmt.Sprintf("%d volume backups failed", result.Failed)
	}
	if remoteDisabled {
		outcome.Status, outcome.Message = scheduler.Partial, backup.RemoteDisabledMessage
	}
	for _, failure := range result.Failures {
		outcome.Targets = append(outcome.Targets, scheduler.TargetOutcome{ID: failure.VolumeName, Status: scheduler.Failed, Message: failure.Error})
	}
	return outcome
}

func (s *Service) rescheduleSystemVolumeBackup(ctx context.Context, policy *backuptypes.SystemVolumeBackupPolicy) {
	if policy == nil {
		return
	}
	jobID := systemVolumeBackupJobPrefix + policy.ID
	if !policy.Enabled {
		s.jobs.Unregister(ctx, jobID)
		return
	}
	policyID := policy.ID
	s.jobs.Add(ctx, &flow.Job{
		Engine:   s.flow,
		Workflow: s.policyWorkflow,
		JobName:  s.jobs.JobName(jobID),
		Payload:  policyID,
		Activity: activitylib.StartRequest{Metadata: database.JSON{"action": "scheduled_system_volume_backups", "policyId": policyID, "schedule": policy.Schedule}},
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
	input, lease, err := s.prepareSystemVolumeBackups(ctx, request)
	if err != nil {
		return nil, err
	}
	input.Requester = &requester
	names := make([]string, len(input.Candidates))
	for i, candidate := range input.Candidates {
		names[i] = candidate.Name
	}
	input.Claim = uuid.NewV7().String()
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
