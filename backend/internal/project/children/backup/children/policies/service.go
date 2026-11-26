package policies

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"time"
	"uuid"

	"github.com/italypaleale/francis/actor"
	"github.com/italypaleale/francis/builtin/workflow"

	"kit.getarcane.app"

	"getarcane.dev/arcane/v3/internal/backup"
	"getarcane.dev/arcane/v3/internal/common"
	"getarcane.dev/arcane/v3/internal/config"
	"getarcane.dev/arcane/v3/internal/database"
	"getarcane.dev/arcane/v3/internal/middleware"
	"getarcane.dev/arcane/v3/internal/s3"
	"getarcane.dev/arcane/v3/internal/scheduler/entityjobs"
	"getarcane.dev/arcane/v3/internal/scheduler/flow"
	"getarcane.dev/arcane/v3/internal/scheduler/jobcontext"
	"getarcane.dev/arcane/v3/internal/scheduler/runs"
	"getarcane.dev/arcane/v3/internal/settings"
	"getarcane.dev/arcane/v3/pkg/activity"
	"getarcane.dev/arcane/v3/pkg/authz"
	"getarcane.dev/arcane/v3/pkg/envcontext"
	"getarcane.dev/types/v3"
)

type (
	Dependencies struct {
		Engine         *backup.Engine
		Policies       *backup.Policies
		S3Destinations *s3.S3DestinationService
		Settings       *settings.SettingsService
		Config         *config.Config
		ProjectName    func(ctx context.Context, projectID string) (string, error)
		ListProjects   func(ctx context.Context) ([]types.ProjectReference, error)
		CheckVolumes   func(ctx context.Context, projectID string, keys []string) error
		Authorize      func(ctx context.Context, requester types.BackupRequester, permissions ...string) error
		LatestRun      func(ctx context.Context, query, series string) (*types.ProjectBackup, error)
		Resume         func(ctx context.Context, previous types.SchedulerRun, requester *types.BackupRequester, recoveryData []byte) error
		BackUp         func(ctx context.Context, run ScheduledBackup) error
		Expire         func(ctx context.Context, seriesID string, keep int) error
		Ctx            context.Context
		Scheduler      types.DynamicScheduler
		Admission      *runs.Admission
	}

	ScheduledBackup struct {
		Read      func(ctx context.Context) (types.CreateProjectBackupRequest, error)
		Retain    func(ctx context.Context)
		Requester *types.BackupRequester
		ProjectID string
		SeriesID  string
	}

	Service struct {
		deps      Dependencies
		jobs      *entityjobs.Registry
		flow      *flow.Engine
		scheduled *flow.Workflow
		global    *flow.Workflow
	}

	scheduledRun struct {
		PolicyID string `json:"policyId"`
	}

	globalRun struct {
		Requester *types.BackupRequester `json:"requester,omitempty"`
		PolicyID  string                 `json:"policyId"`
		Claim     string                 `json:"claim,omitempty"`
	}
)

const globalAdmissionScope = "global-project-backup"

var (
	errPolicyGone     = errors.New("backup policy disabled or deleted")
	errProjectSkipped = errors.New("the project is no longer selected or has its own enabled backup policy")
	errGlobalRunning  = common.Classify(common.ErrConflict, errors.New("this global project backup is already running"))
	StopPermissions   = []string{authz.PermProjectsDown, authz.PermProjectsDeploy}
)

func NewService(deps Dependencies) *Service {
	return &Service{
		deps: deps,
		jobs: entityjobs.New(entityjobs.Config{
			Lifecycle: deps.Ctx,
			Scheduler: deps.Scheduler,
			Admission: deps.Admission,
			JobPrefix: "project-backup:",
		}),
	}
}

func (s *Service) RegisterWorkflows(engine *flow.Engine) error {
	var err error
	s.flow = engine
	s.scheduled, err = engine.Define(flow.Definition{
		Name:        "project-backup-policy",
		Version:     1,
		Fingerprint: "34380becc92f77613e0b2782355044258bdd8a698d1c8018fe9bb6d6c8c28d98",
		Concurrency: 4,
		Timeout:     24 * time.Hour,
		Activity: activity.StartRequest{
			Type: types.ActivityTypeResourceAction, ResourceType: new("project"), StartedBy: &types.SystemUser,
			Step: "Creating scheduled backup", LatestMessage: "Creating scheduled project backup",
		},
		Labels: map[string]string{"backup": "Creating scheduled backup"},
		Steps:  []workflow.StepSpec{workflow.Step("backup", engine.Handler(s.runScheduled), workflow.WithMaxAttempts(1))},
	})
	if err != nil {
		return err
	}
	s.global, err = engine.Define(flow.Definition{
		Name:        "project-backup-global",
		Version:     1,
		Fingerprint: "c0c2d26db776e2d391a2856a2b0fa1d0d22bfa8a49362169b6a24c458c2516f9",
		Concurrency: 4,
		Timeout:     24 * time.Hour,
		Activity: activity.StartRequest{
			Type: types.ActivityTypeResourceAction, EnvironmentID: "0", ResourceType: new("project"), ResourceName: new("Projects"),
			StartedBy: &types.SystemUser, Step: "Backing up projects", LatestMessage: "Creating project backups",
		},
		Labels: map[string]string{"backup": "Backing up projects"},
		Steps:  []workflow.StepSpec{workflow.Step("backup", engine.Handler(s.runGlobal), workflow.WithMaxAttempts(1))},
	})
	return err
}

func (s *Service) Policies(ctx context.Context, projectID string) (types.ProjectBackupPolicyCollection, error) {
	policies, err := s.deps.Policies.List(ctx, backup.PolicyProject, projectID)
	if err != nil {
		return types.ProjectBackupPolicyCollection{}, err
	}
	destinations := s.deps.S3Destinations.DestinationsByID(ctx)
	dtos := make([]types.ProjectBackupPolicy, 0, len(policies))
	for i := range policies {
		lastRun, latestErr := s.deps.LatestRun(ctx, "policy_id = ?", policies[i].ID)
		if latestErr != nil {
			return types.ProjectBackupPolicyCollection{}, latestErr
		}
		dtos = append(dtos, types.ProjectBackupPolicy{ //exhaustruct:enforce
			LastRun:           lastRun,
			ID:                policies[i].ID,
			ProjectID:         policies[i].SubjectID,
			Schedule:          policies[i].Schedule,
			S3DestinationID:   policies[i].S3DestinationID,
			S3DestinationName: destinations[policies[i].S3DestinationID].Name,
			Volumes:           policies[i].Options.Volumes,
			RetentionCount:    policies[i].RetentionCount,
			Enabled:           policies[i].Enabled,
			StopContainers:    policies[i].StopContainers,
			LocalEnabled:      policies[i].LocalEnabled,
			S3Enabled:         policies[i].S3Enabled,
		})
	}
	return types.ProjectBackupPolicyCollection{Policies: dtos, S3Available: len(destinations) > 0}, nil
}

func (s *Service) UpdatePolicies(ctx context.Context, requester types.BackupRequester, projectID string, updates []types.UpdateProjectBackupPolicy) (types.ProjectBackupPolicyCollection, error) {
	if _, err := s.deps.ProjectName(ctx, projectID); err != nil {
		return types.ProjectBackupPolicyCollection{}, err
	}
	reconcile := backup.PolicyReconciliation[types.UpdateProjectBackupPolicy]{
		Kind:      backup.PolicyProject,
		SubjectID: projectID,
		Domain:    "project",
		UpdateID:  func(update types.UpdateProjectBackupPolicy) string { return update.ID },
		Build: func(ctx context.Context, policy *backup.Policy, update types.UpdateProjectBackupPolicy) error {
			normalized, validateErr := backup.ValidatePolicyUpdate(ctx, "project", update.UpdateBackupPolicy, s.deps.S3Destinations)
			if validateErr != nil {
				return common.Classify(common.ErrValidation, validateErr)
			}
			if *cmp.Or(update.StopContainers, new(true)) {
				if permErr := common.Classify(common.ErrForbidden, middleware.RequirePermissions(ctx, requester.EnvironmentID, StopPermissions)); permErr != nil {
					return permErr
				}
			}
			volumes := slices.Clone(update.Volumes)
			slices.Sort(volumes)
			volumes = slices.Compact(volumes)
			if volumes != nil && (policy.Options.Volumes == nil || !slices.Equal(volumes, policy.Options.Volumes)) {
				if checkErr := s.deps.CheckVolumes(ctx, projectID, volumes); checkErr != nil {
					return checkErr
				}
			}
			*policy = backup.Policy{ //exhaustruct:enforce
				BaseModel:       policy.BaseModel,
				SubjectKind:     backup.PolicyProject,
				SubjectID:       projectID,
				EnvironmentID:   policy.EnvironmentID,
				Schedule:        normalized.Schedule,
				S3DestinationID: normalized.S3DestinationID,
				Options:         backup.PolicyOptions{Volumes: volumes, SelectionMode: "", ProjectIDs: nil, VolumeNames: nil, IgnoreAnonymous: false},
				RetentionCount:  normalized.RetentionCount,
				Enabled:         normalized.Enabled,
				StopContainers:  *cmp.Or(update.StopContainers, new(true)),
				LocalEnabled:    normalized.LocalEnabled,
				S3Enabled:       normalized.S3Enabled,
			}
			return nil
		},
		Unregister: s.jobs.Unregister,
		Reschedule: s.reschedule,
	}
	if runErr := s.deps.Policies.Reconcile(ctx, reconcile, updates); runErr != nil {
		return types.ProjectBackupPolicyCollection{}, runErr
	}
	if _, err := s.deps.ProjectName(ctx, projectID); errors.Is(err, common.ErrNotFound) {
		s.Remove(ctx, projectID)
		return types.ProjectBackupPolicyCollection{}, err
	}
	return s.Policies(ctx, projectID)
}

func (s *Service) RegisterJobsOnStartup(ctx context.Context) {
	if err := s.removeOrphaned(ctx); err != nil {
		slog.WarnContext(ctx, "Failed to remove the backup policies of deleted projects", "error", err)
	}
	policies, err := s.deps.Policies.AllEnabled(ctx, backup.PolicyProject)
	if err != nil {
		slog.ErrorContext(ctx, "Failed to load scheduled project backups", "error", err)
		return
	}
	for i := range policies {
		s.reschedule(ctx, &policies[i])
	}
	slog.InfoContext(ctx, "Registered scheduled project backup jobs", "count", len(policies))
}

func (s *Service) Remove(ctx context.Context, projectID string) {
	policies, err := s.deps.Policies.Remove(ctx, backup.PolicyProject, projectID)
	for i := range policies {
		s.jobs.Unregister(ctx, policies[i].ID)
	}
	if err != nil {
		slog.WarnContext(ctx, "Failed to delete the backup policies of a deleted project", "projectId", projectID, "error", err)
	}
}

func (s *Service) removeOrphaned(ctx context.Context) error {
	subjects, err := s.deps.Policies.Subjects(ctx, backup.PolicyProject)
	if err != nil || len(subjects) == 0 {
		return err
	}
	references, err := s.deps.ListProjects(ctx)
	if err != nil {
		return err
	}
	for _, projectID := range subjects {
		if !slices.ContainsFunc(references, func(project types.ProjectReference) bool { return project.ID == projectID }) {
			s.Remove(ctx, projectID)
		}
	}
	return nil
}

func (s *Service) Managed(ctx context.Context) (map[string]bool, error) {
	if err := s.removeOrphaned(ctx); err != nil {
		return nil, err
	}
	policyIDs, err := s.deps.Policies.IDs(ctx, backup.PolicyProject)
	if err != nil {
		return nil, err
	}
	globals, err := s.deps.Policies.List(ctx, backup.PolicyProject, "")
	if err != nil {
		return nil, err
	}
	references, err := s.deps.ListProjects(ctx)
	if err != nil {
		return nil, err
	}
	managed := make(map[string]bool, len(policyIDs))
	for _, policyID := range policyIDs {
		managed[policyID] = true
	}
	for _, policy := range globals {
		for _, project := range selectProjects(policy, references) {
			managed[seriesID(policy.ID, project.ID)] = true
		}
	}
	return managed, nil
}

func (s *Service) reschedule(ctx context.Context, policy *backup.Policy) {
	if policy.SubjectID == "" {
		s.rescheduleGlobal(ctx, policy)
		return
	}
	if !policy.Enabled {
		s.jobs.Unregister(ctx, policy.ID)
		return
	}
	policyID, projectID := policy.ID, policy.SubjectID
	name, err := s.deps.ProjectName(ctx, projectID)
	if err != nil {
		slog.WarnContext(ctx, "Failed to load the name of a project with a backup policy", "projectId", projectID, "error", err)
	}
	s.jobs.Add(ctx, &flow.Job{
		Engine:   s.flow,
		Workflow: s.scheduled,
		JobName:  s.jobs.JobName(policyID),
		Payload:  scheduledRun{PolicyID: policyID},
		Activity: activity.StartRequest{
			ResourceID: new(projectID), ResourceName: new(cmp.Or(name, projectID)),
			Metadata: database.JSON{
				"action": "scheduled_project_backup", "policyId": policyID, "schedule": policy.Schedule, "projectId": projectID,
				"retentionCount": policy.RetentionCount, "stopContainers": policy.StopContainers,
				"localEnabled": policy.LocalEnabled, "s3Enabled": policy.S3Enabled, "s3DestinationId": policy.S3DestinationID,
			},
		},
		ScheduleFn:  s.deps.Policies.Schedule(policyID, policy.Schedule),
		ShouldRunFn: func(context.Context) bool { return s.deps.Config.LocalDockerEnabled },
		FallbackFn: func(ctx context.Context, previous types.SchedulerRun) (types.SchedulerOutcome, error) {
			outcome, resumed, resumeErr := s.resumeScheduled(ctx, previous, policyID)
			if resumed || resumeErr != nil {
				return outcome, resumeErr
			}
			return types.SchedulerOutcome{Status: types.RunStatusFailed, Message: "Scheduled backup was interrupted; it runs again at its next scheduled time"}, nil
		},
	})
}

func (s *Service) runScheduled(ctx context.Context, t flow.Task) (any, error) {
	var input scheduledRun
	if decodeErr := t.Payload(&input); decodeErr != nil {
		return nil, decodeErr
	}
	previous, _ := jobcontext.Run(ctx)
	if outcome, resumed, resumeErr := s.resumeScheduled(ctx, previous, input.PolicyID); resumed || resumeErr != nil {
		return outcome, resumeErr
	}
	canceled := types.SchedulerOutcome{Status: types.RunStatusCanceled, Message: "Backup policy disabled or deleted"}
	policy, err := s.deps.Policies.Enabled(ctx, input.PolicyID)
	if err != nil {
		return nil, err
	}
	if policy == nil {
		return canceled, nil
	}
	// Scheduled jobs run in the manager's environment, so a policy owned by a docker-host environment
	// is refused rather than backing up the project against the wrong daemon.
	if policy.EnvironmentID != envcontext.EnvironmentIDOrDefault(ctx) {
		return types.SchedulerOutcome{Status: types.RunStatusCanceled, Message: "Backup policy belongs to another environment"}, nil
	}
	remoteDisabled := false
	err = s.deps.BackUp(ctx, ScheduledBackup{
		ProjectID: policy.SubjectID,
		SeriesID:  policy.ID,
		Read: func(ctx context.Context) (types.CreateProjectBackupRequest, error) {
			request, disabled, readErr := s.scheduledPolicy(ctx, policy.ID)
			remoteDisabled = remoteDisabled || disabled
			return request, readErr
		},
		Retain: func(ctx context.Context) { s.retain(ctx, policy, policy.SubjectID, policy.ID) },
	})
	switch {
	case errors.Is(err, errPolicyGone):
		return canceled, nil
	case errors.Is(err, backup.ErrRemoteOnlyDisabled):
		return types.SchedulerOutcome{Status: types.RunStatusNeedsAttention, Message: backup.RemoteDisabledMessage}, nil
	case err != nil:
		slog.ErrorContext(ctx, "Scheduled project backup failed", "projectId", policy.SubjectID, "policyId", policy.ID, "error", err)
		return nil, err
	case remoteDisabled:
		return types.SchedulerOutcome{Status: types.RunStatusPartial, Message: backup.RemoteDisabledMessage}, nil
	}
	return types.SchedulerOutcome{Status: types.RunStatusSucceeded, Message: "Scheduled project backup created successfully"}, nil
}

func (s *Service) resumeScheduled(ctx context.Context, previous types.SchedulerRun, policyID string) (types.SchedulerOutcome, bool, error) {
	index := slices.IndexFunc(previous.Outcome.Targets, func(target types.TargetOutcome) bool {
		return target.ResourceType == backup.ProjectTargetType && len(target.RecoveryData) > 0
	})
	if index < 0 {
		return types.SchedulerOutcome{}, false, nil
	}
	target := previous.Outcome.Targets[index]
	err := s.deps.Resume(ctx, previous, nil, target.RecoveryData)
	switch {
	case errors.Is(err, backup.ErrBackupSettled):
		return types.SchedulerOutcome{}, false, nil
	case err != nil:
		return types.SchedulerOutcome{}, true, err
	}
	s.retain(ctx, &backup.Policy{ID: policyID, SubjectID: target.ID}, target.ID, policyID)
	return types.SchedulerOutcome{Status: types.RunStatusSucceeded, Message: "Scheduled project backup created successfully"}, true, nil
}

func (s *Service) scheduledPolicy(ctx context.Context, policyID string) (types.CreateProjectBackupRequest, bool, error) {
	policy, err := s.deps.Policies.Enabled(ctx, policyID)
	if err != nil {
		return types.CreateProjectBackupRequest{}, false, err
	}
	if policy == nil {
		return types.CreateProjectBackupRequest{}, false, errPolicyGone
	}
	disabled, err := s.checkRemote(ctx, policy)
	if err != nil {
		return types.CreateProjectBackupRequest{}, disabled, err
	}
	return types.CreateProjectBackupRequest{
		StopContainers:  new(policy.StopContainers),
		Destination:     backup.Destination(backup.DestinationOptions{Local: policy.LocalEnabled, Remote: policy.S3Enabled}),
		S3DestinationID: policy.S3DestinationID,
		Volumes:         policy.Options.Volumes,
	}, disabled, nil
}

func (s *Service) checkRemote(ctx context.Context, policy *backup.Policy) (bool, error) {
	return s.deps.Policies.CheckRemote(ctx, policy, backup.RemoteCheck{
		S3Destinations: s.deps.S3Destinations,
		Settings:       s.deps.Settings,
		Reschedule:     s.reschedule,
	})
}

func (s *Service) retain(ctx context.Context, policy *backup.Policy, projectID, seriesID string) {
	current, err := s.deps.Policies.Get(ctx, backup.PolicyProject, policy.SubjectID, policy.ID)
	if err == nil && current != nil && current.RetentionCount > 0 {
		err = s.deps.Expire(ctx, seriesID, current.RetentionCount)
	}
	if err != nil {
		slog.ErrorContext(ctx, "Project backup retention failed", "projectId", projectID, "policyId", seriesID, "error", err)
	}
}

func seriesID(policyID, projectID string) string {
	return types.GlobalProjectPolicyPrefix + policyID + ":" + projectID
}

func selectProjects(policy backup.Policy, references []types.ProjectReference) []types.ProjectReference {
	selected := make([]types.ProjectReference, 0, len(references))
	for _, project := range references {
		if policy.Options.SelectionMode.Selects(slices.Contains(policy.Options.ProjectIDs, project.ID)) {
			selected = append(selected, project)
		}
	}
	return selected
}

func (s *Service) GlobalPolicies(ctx context.Context) (types.GlobalProjectBackupPolicyCollection, error) {
	policies, err := s.deps.Policies.List(ctx, backup.PolicyProject, "")
	if err != nil {
		return types.GlobalProjectBackupPolicyCollection{}, err
	}
	destinations := s.deps.S3Destinations.DestinationsByID(ctx)
	dtos := make([]types.GlobalProjectBackupPolicy, 0, len(policies))
	for _, policy := range policies {
		lastRun, latestErr := s.deps.LatestRun(ctx, "policy_id LIKE ?", seriesID(policy.ID, "%"))
		if latestErr != nil {
			return types.GlobalProjectBackupPolicyCollection{}, latestErr
		}
		dtos = append(dtos, types.GlobalProjectBackupPolicy{ //exhaustruct:enforce
			LastRun:           lastRun,
			ID:                policy.ID,
			Schedule:          policy.Schedule,
			S3DestinationID:   policy.S3DestinationID,
			S3DestinationName: destinations[policy.S3DestinationID].Name,
			SelectionMode:     policy.Options.SelectionMode,
			ProjectIDs:        policy.Options.ProjectIDs,
			RetentionCount:    policy.RetentionCount,
			Enabled:           policy.Enabled,
			StopContainers:    policy.StopContainers,
			LocalEnabled:      policy.LocalEnabled,
			S3Enabled:         policy.S3Enabled,
		})
	}
	return types.GlobalProjectBackupPolicyCollection{Policies: dtos}, nil
}

func (s *Service) UpdateGlobalPolicies(ctx context.Context, requester types.BackupRequester, updates []types.UpdateGlobalProjectBackupPolicy) (types.GlobalProjectBackupPolicyCollection, error) {
	reconcile := backup.PolicyReconciliation[types.UpdateGlobalProjectBackupPolicy]{
		Kind:     backup.PolicyProject,
		Domain:   "global project",
		UpdateID: func(update types.UpdateGlobalProjectBackupPolicy) string { return update.ID },
		Build: func(ctx context.Context, policy *backup.Policy, update types.UpdateGlobalProjectBackupPolicy) error {
			if !update.SelectionMode.Valid() {
				return common.Classify(common.ErrValidation, errors.New("selectionMode must be all, allowlist, or blocklist"))
			}
			normalized, validateErr := backup.ValidatePolicyUpdate(ctx, "project", update.UpdateBackupPolicy, s.deps.S3Destinations)
			if validateErr != nil {
				return common.Classify(common.ErrValidation, validateErr)
			}
			if *cmp.Or(update.StopContainers, new(true)) {
				if authorizeErr := s.deps.Authorize(ctx, requester, StopPermissions...); authorizeErr != nil {
					return authorizeErr
				}
			}
			projectIDs := kit.Unique(kit.TrimNonEmpty(update.ProjectIDs))
			slices.Sort(projectIDs)
			*policy = backup.Policy{ //exhaustruct:enforce
				BaseModel:       policy.BaseModel,
				SubjectKind:     backup.PolicyProject,
				SubjectID:       "",
				EnvironmentID:   policy.EnvironmentID,
				Schedule:        normalized.Schedule,
				S3DestinationID: normalized.S3DestinationID,
				Options: backup.PolicyOptions{
					Volumes:         nil,
					SelectionMode:   update.SelectionMode,
					ProjectIDs:      kit.Ternary(update.SelectionMode == types.SystemVolumeSelectionAll, nil, projectIDs),
					VolumeNames:     nil,
					IgnoreAnonymous: false,
				},
				RetentionCount: normalized.RetentionCount,
				Enabled:        normalized.Enabled,
				StopContainers: *cmp.Or(update.StopContainers, new(true)),
				LocalEnabled:   normalized.LocalEnabled,
				S3Enabled:      normalized.S3Enabled,
			}
			return nil
		},
		Unregister: func(ctx context.Context, policyID string) {
			s.jobs.Unregister(ctx, types.GlobalProjectPolicyPrefix+policyID)
		},
		Reschedule: s.rescheduleGlobal,
	}
	if runErr := s.deps.Policies.Reconcile(ctx, reconcile, updates); runErr != nil {
		return types.GlobalProjectBackupPolicyCollection{}, runErr
	}
	return s.GlobalPolicies(ctx)
}

func (s *Service) GlobalOptions(ctx context.Context) ([]types.GlobalProjectBackupOption, error) {
	references, err := s.deps.ListProjects(ctx)
	if err != nil {
		return nil, err
	}
	policies, err := s.deps.Policies.List(ctx, backup.PolicyProject, "")
	if err != nil {
		return nil, err
	}
	options := make([]types.GlobalProjectBackupOption, 0, len(references))
	for _, project := range references {
		options = append(options, types.GlobalProjectBackupOption{ID: project.ID, Name: project.Name, Available: true})
	}
	for _, policy := range policies {
		for _, projectID := range policy.Options.ProjectIDs {
			if !slices.ContainsFunc(options, func(option types.GlobalProjectBackupOption) bool { return option.ID == projectID }) {
				options = append(options, types.GlobalProjectBackupOption{ID: projectID, Name: projectID})
			}
		}
	}
	return options, nil
}

func (s *Service) StartGlobal(ctx context.Context, user types.Actor, policyID string) (types.BackupRunAccepted, error) {
	requester := backup.NewRequester(ctx, user, "0", authz.PermSystemBackupsCreate)
	if err := s.deps.Engine.Authorize(ctx, requester); err != nil {
		return types.BackupRunAccepted{}, err
	}
	if !s.deps.Config.LocalDockerEnabled {
		return types.BackupRunAccepted{}, common.ErrLocalDockerDisabled
	}
	policy, err := s.deps.Policies.Get(ctx, backup.PolicyProject, "", policyID)
	switch {
	case err != nil:
		return types.BackupRunAccepted{}, err
	case policy == nil:
		return types.BackupRunAccepted{}, common.Classify(common.ErrNotFound, errors.New("global project backup policy not found"))
	case !policy.Enabled:
		return types.BackupRunAccepted{}, common.Classify(common.ErrValidation, errors.New("enable the backup policy before running it"))
	}
	lease, admitted, err := s.deps.Engine.TryAcquireRun(ctx, globalAdmissionScope, policyID)
	switch {
	case err != nil:
		return types.BackupRunAccepted{}, err
	case !admitted:
		return types.BackupRunAccepted{}, errGlobalRunning
	}
	input := globalRun{
		Requester: &requester,
		PolicyID:  policyID,
		Claim:     uuid.NewV7().String(),
	}
	releaseHeld := s.deps.Engine.Hold(globalAdmissionScope, policyID, input.Claim, lease)
	activityID, err := s.flow.Submit(ctx, s.global, input, activity.StartRequest{
		StartedBy: &user,
		Metadata:  database.JSON{"action": "run_project_backups", "policyId": policyID},
	})
	if err != nil {
		releaseHeld(ctx)
		return types.BackupRunAccepted{}, err
	}
	go func() {
		background := context.WithoutCancel(ctx)
		_, _ = s.flow.Wait(background, s.global, activityID)
		releaseHeld(background)
	}()
	return types.BackupRunAccepted{ActivityID: activityID, Status: "running"}, nil
}

func (s *Service) rescheduleGlobal(ctx context.Context, policy *backup.Policy) {
	if s.deps.Config.AgentEnabled {
		return
	}
	jobID := types.GlobalProjectPolicyPrefix + policy.ID
	if !policy.Enabled {
		s.jobs.Unregister(ctx, jobID)
		return
	}
	policyID := policy.ID
	s.jobs.Add(ctx, &flow.Job{
		Engine:   s.flow,
		Workflow: s.global,
		JobName:  s.jobs.JobName(jobID),
		Payload:  globalRun{PolicyID: policyID},
		Activity: activity.StartRequest{
			Metadata: database.JSON{"action": "scheduled_project_backups", "policyId": policyID, "schedule": policy.Schedule},
		},
		ScheduleFn:  s.deps.Policies.Schedule(policyID, policy.Schedule),
		ShouldRunFn: func(context.Context) bool { return s.deps.Config.LocalDockerEnabled },
		FallbackFn: func(ctx context.Context, previous types.SchedulerRun) (types.SchedulerOutcome, error) {
			return s.backUpGlobal(ctx, globalRun{PolicyID: policyID}, previous)
		},
	})
}

func (s *Service) runGlobal(ctx context.Context, t flow.Task) (any, error) {
	var input globalRun
	if err := t.Payload(&input); err != nil {
		return nil, err
	}
	previous, _ := jobcontext.Run(ctx)
	return s.backUpGlobal(ctx, input, previous)
}

func (s *Service) backUpGlobal(ctx context.Context, input globalRun, previous types.SchedulerRun) (types.SchedulerOutcome, error) {
	canceled := types.SchedulerOutcome{Status: types.RunStatusCanceled, Message: "Backup policy disabled or deleted"}
	lease, admitted, err := s.deps.Engine.AcquireRun(ctx, globalAdmissionScope, input.PolicyID, input.Claim)
	if err == nil && !admitted {
		if input.Claim != "" {
			return types.SchedulerOutcome{}, errGlobalRunning
		}
		lease, err = s.deps.Engine.AwaitRun(ctx, globalAdmissionScope, input.PolicyID)
	}
	if err != nil {
		return types.SchedulerOutcome{}, err
	}
	defer lease.Release(context.WithoutCancel(ctx))
	finished := make(map[string]types.TargetOutcome)
	var resumed []string
	report := func(projectID string, backupErr error) error {
		status, message := types.RunStatusSucceeded, ""
		switch {
		case errors.Is(backupErr, errProjectSkipped):
			status, message = types.RunStatusSkipped, backupErr.Error()
		case backupErr != nil:
			slog.ErrorContext(ctx, "Global project backup failed", "projectId", projectID, "policyId", input.PolicyID, "error", backupErr)
			status, message = types.RunStatusFailed, backupErr.Error()
		}
		outcome := types.TargetOutcome{ResourceType: backup.ProjectTargetType, ID: projectID, Status: status, Message: message}
		finished[projectID] = outcome
		return jobcontext.Progress(ctx, outcome)
	}
	for _, target := range previous.Outcome.Targets {
		if target.ResourceType != backup.ProjectTargetType {
			continue
		}
		if target.Status == types.RunStatusSucceeded || target.Status == types.RunStatusSkipped {
			finished[target.ID] = target
			continue
		}
		if target.Status != types.RunStatusRunning {
			continue
		}
		resumeErr := s.deps.Resume(ctx, previous, input.Requester, target.RecoveryData)
		switch {
		case ctx.Err() != nil:
			return types.SchedulerOutcome{}, ctx.Err()
		case errors.Is(resumeErr, actor.ErrJobRejected):
			return types.SchedulerOutcome{}, resumeErr
		case errors.Is(resumeErr, backup.ErrBackupSettled):
			continue
		case resumeErr == nil:
			resumed = append(resumed, target.ID)
		}
		if reportErr := report(target.ID, resumeErr); reportErr != nil {
			return types.SchedulerOutcome{}, reportErr
		}
	}
	policy, err := s.deps.Policies.Get(ctx, backup.PolicyProject, "", input.PolicyID)
	if err != nil {
		return types.SchedulerOutcome{}, err
	}
	if policy == nil || !policy.Enabled {
		return canceled, nil
	}
	for _, projectID := range resumed {
		s.retain(ctx, policy, projectID, seriesID(input.PolicyID, projectID))
	}
	references, err := s.deps.ListProjects(ctx)
	if err != nil {
		return types.SchedulerOutcome{}, err
	}
	remoteDisabled := false
	for _, project := range selectProjects(*policy, references) {
		if _, done := finished[project.ID]; done {
			continue
		}
		series := seriesID(input.PolicyID, project.ID)
		backupErr := s.deps.BackUp(ctx, ScheduledBackup{
			Requester: input.Requester,
			ProjectID: project.ID,
			SeriesID:  series,
			Read: func(ctx context.Context) (types.CreateProjectBackupRequest, error) {
				request, disabled, readErr := s.globalSettings(ctx, input, project.ID)
				remoteDisabled = remoteDisabled || disabled
				return request, readErr
			},
			Retain: func(ctx context.Context) { s.retain(ctx, policy, project.ID, series) },
		})
		switch {
		case errors.Is(backupErr, errPolicyGone):
			return canceled, nil
		case errors.Is(backupErr, backup.ErrRemoteOnlyDisabled):
			return types.SchedulerOutcome{Status: types.RunStatusNeedsAttention, Message: backup.RemoteDisabledMessage}, nil
		case ctx.Err() != nil:
			return types.SchedulerOutcome{}, ctx.Err()
		}
		if reportErr := report(project.ID, backupErr); reportErr != nil {
			return types.SchedulerOutcome{}, reportErr
		}
	}
	return globalOutcome(finished, remoteDisabled), nil
}

func (s *Service) globalSettings(ctx context.Context, input globalRun, projectID string) (types.CreateProjectBackupRequest, bool, error) {
	if input.Requester != nil {
		if err := s.deps.Engine.Authorize(ctx, *input.Requester); err != nil {
			return types.CreateProjectBackupRequest{}, false, err
		}
	}
	policy, err := s.deps.Policies.Get(ctx, backup.PolicyProject, "", input.PolicyID)
	if err != nil {
		return types.CreateProjectBackupRequest{}, false, err
	}
	if policy == nil || !policy.Enabled {
		return types.CreateProjectBackupRequest{}, false, errPolicyGone
	}
	if !policy.Options.SelectionMode.Selects(slices.Contains(policy.Options.ProjectIDs, projectID)) {
		return types.CreateProjectBackupRequest{}, false, errProjectSkipped
	}
	own, err := s.deps.Policies.HasEnabled(ctx, backup.PolicyProject, projectID)
	if err != nil {
		return types.CreateProjectBackupRequest{}, false, err
	}
	if own {
		return types.CreateProjectBackupRequest{}, false, errProjectSkipped
	}
	disabled, err := s.checkRemote(ctx, policy)
	if err != nil {
		return types.CreateProjectBackupRequest{}, disabled, err
	}
	return types.CreateProjectBackupRequest{
		StopContainers:  new(policy.StopContainers),
		Destination:     backup.Destination(backup.DestinationOptions{Local: policy.LocalEnabled, Remote: policy.S3Enabled}),
		S3DestinationID: policy.S3DestinationID,
	}, disabled, nil
}

func globalOutcome(finished map[string]types.TargetOutcome, remoteDisabled bool) types.SchedulerOutcome {
	succeeded, skipped := 0, 0
	var failures []types.TargetOutcome
	for _, target := range finished {
		switch target.Status {
		case types.RunStatusSucceeded:
			succeeded++
		case types.RunStatusFailed:
			failures = append(failures, target)
		default:
			skipped++
		}
	}
	return types.SchedulerOutcome{
		Status: kit.Ternary(len(failures) > 0 || remoteDisabled, types.RunStatusPartial, types.RunStatusSucceeded),
		Message: cmp.Or(
			kit.Ternary(remoteDisabled, backup.RemoteDisabledMessage, ""),
			kit.Ternary(len(failures) > 0, fmt.Sprintf("%d project backups failed", len(failures)), ""),
			fmt.Sprintf("Backed up %d projects; %d skipped", succeeded, skipped),
		),
		Targets: failures,
	}
}
