package policies

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"time"

	"github.com/italypaleale/francis/actor"
	"github.com/italypaleale/francis/builtin/workflow"

	"kit.getarcane.app"

	"getarcane.dev/arcane/v3/internal/backup"
	"getarcane.dev/arcane/v3/internal/common"
	"getarcane.dev/arcane/v3/internal/config"
	"getarcane.dev/arcane/v3/internal/database"
	"getarcane.dev/arcane/v3/internal/s3"
	"getarcane.dev/arcane/v3/internal/scheduler/entityjobs"
	"getarcane.dev/arcane/v3/internal/scheduler/flow"
	"getarcane.dev/arcane/v3/internal/scheduler/jobcontext"
	"getarcane.dev/arcane/v3/internal/scheduler/runs"
	"getarcane.dev/arcane/v3/internal/settings"
	"getarcane.dev/arcane/v3/pkg/activity"
	"getarcane.dev/arcane/v3/pkg/authz"
	"getarcane.dev/types/v3"
)

type (
	Dependencies struct {
		DB             *database.DB
		Engine         *backup.Engine
		Policies       *backup.Policies
		S3Destinations *s3.S3DestinationService
		Settings       *settings.SettingsService
		// Config tells a manager, which runs the global policies, from an agent.
		Config *config.Config
		// ProjectName returns the project's name without loading its compose files.
		ProjectName func(ctx context.Context, projectID string) (string, error)
		// ListProjects returns the ID and name of every project on this server.
		ListProjects func(ctx context.Context) ([]types.ProjectReference, error)
		// CheckVolumes refuses a volume selection that a manual backup of the project would refuse.
		CheckVolumes func(ctx context.Context, projectID string, keys []string) error
		// LatestRun returns the newest backup the policy took, of any project for a global policy, or nil.
		LatestRun func(ctx context.Context, policyID string) (*types.ProjectBackup, error)
		// Resume finishes, or fails, the backup an interrupted attempt checkpointed in recoveryData;
		// backup.ErrBackupSettled means it had already failed, so a new one should run.
		Resume func(ctx context.Context, previous types.SchedulerRun, requester *types.BackupRequester, recoveryData []byte) error
		// BackUp takes a project's scheduled backup.
		BackUp func(ctx context.Context, run ScheduledBackup) error
		// Expire deletes the series' backups beyond its newest keep; the caller holds the project's admission.
		Expire func(ctx context.Context, seriesID string, keep int) error
		// Ctx, Scheduler and Admission register the policies' scheduled jobs.
		Ctx       context.Context
		Scheduler types.DynamicScheduler
		Admission *runs.Admission
	}

	// ScheduledBackup is one project's backup for a policy.
	ScheduledBackup struct {
		// Read reads the policy once the project's admission is held and returns the backup it asks for.
		Read func(ctx context.Context) (types.CreateProjectBackupRequest, error)
		// Retain runs under the project's admission once the backup ends, whether or not it succeeded.
		Retain    func(ctx context.Context)
		ProjectID string
		// SeriesID records the backup under the policy, or under a global policy's series for the project.
		SeriesID string
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

	// globalRun runs a global policy; a run started by hand also carries its requester.
	globalRun struct {
		Requester *types.BackupRequester `json:"requester,omitempty"`
		PolicyID  string                 `json:"policyId"`
	}
)

// globalAdmissionScope keeps one run of each global policy at a time; it is always taken before a project's.
const globalAdmissionScope = "global-project-backup"

var (
	errPolicyGone     = errors.New("backup policy disabled or deleted")
	errProjectSkipped = errors.New("the project is no longer selected or has its own enabled backup policy")
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

// Policies lists the project's backup policies with the latest backup each one took.
func (s *Service) Policies(ctx context.Context, projectID string) (types.ProjectBackupPolicyCollection, error) {
	policies, err := s.deps.Policies.List(ctx, backup.PolicyProject, projectID)
	if err != nil {
		return types.ProjectBackupPolicyCollection{}, err
	}
	destinations := s.deps.S3Destinations.DestinationsByID(ctx)
	dtos := make([]types.ProjectBackupPolicy, 0, len(policies))
	for i := range policies {
		lastRun, latestErr := s.deps.LatestRun(ctx, policies[i].ID)
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

// UpdatePolicies replaces the project's backup policies, schedules the enabled ones and unschedules the rest. A project
// deleted while they were saved has them removed again, since its deletion may have removed them before the save.
func (s *Service) UpdatePolicies(ctx context.Context, projectID string, updates []types.UpdateProjectBackupPolicy) (types.ProjectBackupPolicyCollection, error) {
	if _, err := s.deps.ProjectName(ctx, projectID); err != nil {
		return types.ProjectBackupPolicyCollection{}, err
	}
	existing, err := s.deps.Policies.List(ctx, backup.PolicyProject, projectID)
	if err != nil {
		return types.ProjectBackupPolicyCollection{}, err
	}
	reconcile := backup.PolicyReconciliation[backup.Policy, types.UpdateProjectBackupPolicy]{
		Domain:   "project",
		DB:       s.deps.DB,
		Existing: existing,
		ID:       func(policy *backup.Policy) string { return policy.ID },
		UpdateID: func(update types.UpdateProjectBackupPolicy) string { return update.ID },
		New:      func() backup.Policy { return backup.Policy{SubjectKind: backup.PolicyProject, SubjectID: projectID} },
		Build: func(ctx context.Context, policy *backup.Policy, update types.UpdateProjectBackupPolicy) error {
			normalized, validateErr := backup.ValidatePolicyUpdate(ctx, "project", update.UpdateBackupPolicy, s.deps.S3Destinations)
			if validateErr != nil {
				return common.Classify(common.ErrValidation, validateErr)
			}
			volumes := slices.Clone(update.Volumes)
			slices.Sort(volumes)
			volumes = slices.Compact(volumes)
			// No selection follows the project's volumes; a new or changed selection follows the rules of a manual backup.
			// A saved one is kept as it is, so a volume removed since never blocks editing; the scheduled backup reports it.
			if volumes != nil && (policy.Options.Volumes == nil || !slices.Equal(volumes, policy.Options.Volumes)) {
				if checkErr := s.deps.CheckVolumes(ctx, projectID, volumes); checkErr != nil {
					return checkErr
				}
			}
			*policy = backup.Policy{ //exhaustruct:enforce
				BaseModel:       policy.BaseModel,
				SubjectKind:     backup.PolicyProject,
				SubjectID:       projectID,
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
	if runErr := reconcile.Run(ctx, updates); runErr != nil {
		return types.ProjectBackupPolicyCollection{}, runErr
	}
	if _, err = s.deps.ProjectName(ctx, projectID); errors.Is(err, common.ErrNotFound) {
		s.Remove(ctx, projectID)
		return types.ProjectBackupPolicyCollection{}, err
	}
	return s.Policies(ctx, projectID)
}

// RegisterJobsOnStartup removes the policies of deleted projects, then schedules every enabled project backup policy
// and global project backup policy.
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

// Remove unschedules and deletes a deleted project's backup policies, which leaves their backups unmanaged.
func (s *Service) Remove(ctx context.Context, projectID string) {
	policies, err := s.deps.Policies.Remove(ctx, backup.PolicyProject, projectID)
	if err != nil {
		slog.WarnContext(ctx, "Failed to delete the backup policies of a deleted project", "projectId", projectID, "error", err)
		return
	}
	for i := range policies {
		s.jobs.Unregister(ctx, policies[i].ID)
	}
}

// removeOrphaned removes the policies of projects that no longer exist, which a missed deletion left behind. The
// policies are listed before the projects, and a policy is only saved for an existing project, so none of a project
// created meanwhile is removed.
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

// Managed removes the policies of projects that no longer exist, so their backups become unmanaged, and returns the
// policy IDs whose backups are managed: every policy's own, and each project series of a global policy while it still
// selects that project.
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

// reschedule schedules an enabled policy's job and unschedules a disabled one; a policy with no project is global.
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
		ScheduleFn: s.deps.Policies.Schedule(policyID),
		FallbackFn: func(ctx context.Context, previous types.SchedulerRun) (types.SchedulerOutcome, error) {
			outcome, resumed, resumeErr := s.resumeScheduled(ctx, previous)
			if resumed || resumeErr != nil {
				return outcome, resumeErr
			}
			return types.SchedulerOutcome{Status: types.RunStatusFailed, Message: "Scheduled backup was interrupted; it runs again at its next scheduled time"}, nil
		},
	})
}

// runScheduled resumes an interrupted backup of the policy, or backs the project up with the policy as it stands once
// the project is admitted.
func (s *Service) runScheduled(ctx context.Context, t flow.Task) (any, error) {
	var input scheduledRun
	if decodeErr := t.Payload(&input); decodeErr != nil {
		return nil, decodeErr
	}
	previous, _ := jobcontext.Run(ctx)
	if outcome, resumed, resumeErr := s.resumeScheduled(ctx, previous); resumed || resumeErr != nil {
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
	remoteDisabled := false
	err = s.deps.BackUp(ctx, ScheduledBackup{
		ProjectID: policy.SubjectID,
		SeriesID:  policy.ID,
		Read: func(ctx context.Context) (types.CreateProjectBackupRequest, error) {
			request, disabled, readErr := s.scheduledPolicy(ctx, policy.ID)
			remoteDisabled = disabled
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

// resumeScheduled finishes the backup an interrupted attempt checkpointed; it reports false when there is none, or when
// that backup already failed and a new one should run.
func (s *Service) resumeScheduled(ctx context.Context, previous types.SchedulerRun) (types.SchedulerOutcome, bool, error) {
	index := slices.IndexFunc(previous.Outcome.Targets, func(target types.TargetOutcome) bool {
		return target.ResourceType == backup.ProjectTargetType && len(target.RecoveryData) > 0
	})
	if index < 0 {
		return types.SchedulerOutcome{}, false, nil
	}
	err := s.deps.Resume(ctx, previous, nil, previous.Outcome.Targets[index].RecoveryData)
	switch {
	case errors.Is(err, backup.ErrBackupSettled):
		return types.SchedulerOutcome{}, false, nil
	case err != nil:
		return types.SchedulerOutcome{}, true, err
	}
	return types.SchedulerOutcome{Status: types.RunStatusSucceeded, Message: "Scheduled project backup created successfully"}, true, nil
}

// scheduledPolicy reads the enabled policy under the project's admission and checks its S3 repository.
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

// retain deletes the series' backups beyond the policy's newest RetentionCount; it runs under the project's admission.
// The policy is read again, since an edit or delete made while the backup ran must keep the backups it asks to keep. A
// global policy keeps its RetentionCount per project, in the project's series.
func (s *Service) retain(ctx context.Context, policy *backup.Policy, projectID, seriesID string) {
	current, err := s.deps.Policies.Get(ctx, backup.PolicyProject, policy.SubjectID, policy.ID)
	if err == nil && current != nil && current.RetentionCount > 0 {
		err = s.deps.Expire(ctx, seriesID, current.RetentionCount)
	}
	if err != nil {
		slog.ErrorContext(ctx, "Project backup retention failed", "projectId", projectID, "policyId", seriesID, "error", err)
	}
}

// seriesID is the policy ID that the backups of a project from a global policy are recorded under.
func seriesID(policyID, projectID string) string {
	return types.GlobalProjectPolicyPrefix + policyID + ":" + projectID
}

// selectProjects returns the projects the global policy selects; listed IDs of deleted projects match nothing.
func selectProjects(policy backup.Policy, references []types.ProjectReference) []types.ProjectReference {
	selected := make([]types.ProjectReference, 0, len(references))
	for _, project := range references {
		if policy.Options.SelectionMode.Selects(slices.Contains(policy.Options.ProjectIDs, project.ID)) {
			selected = append(selected, project)
		}
	}
	return selected
}

// GlobalPolicies lists the global project backup policies with the latest backup each one took.
func (s *Service) GlobalPolicies(ctx context.Context) (types.GlobalProjectBackupPolicyCollection, error) {
	policies, err := s.deps.Policies.List(ctx, backup.PolicyProject, "")
	if err != nil {
		return types.GlobalProjectBackupPolicyCollection{}, err
	}
	destinations := s.deps.S3Destinations.DestinationsByID(ctx)
	dtos := make([]types.GlobalProjectBackupPolicy, 0, len(policies))
	for _, policy := range policies {
		lastRun, latestErr := s.deps.LatestRun(ctx, policy.ID)
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

// UpdateGlobalPolicies replaces the global project backup policies, schedules the enabled ones and unschedules the
// rest. Listed IDs of projects that do not exist are kept, and match nothing.
func (s *Service) UpdateGlobalPolicies(ctx context.Context, updates []types.UpdateGlobalProjectBackupPolicy) (types.GlobalProjectBackupPolicyCollection, error) {
	existing, err := s.deps.Policies.List(ctx, backup.PolicyProject, "")
	if err != nil {
		return types.GlobalProjectBackupPolicyCollection{}, err
	}
	reconcile := backup.PolicyReconciliation[backup.Policy, types.UpdateGlobalProjectBackupPolicy]{
		Domain:   "global project",
		DB:       s.deps.DB,
		Existing: existing,
		ID:       func(policy *backup.Policy) string { return policy.ID },
		UpdateID: func(update types.UpdateGlobalProjectBackupPolicy) string { return update.ID },
		New:      func() backup.Policy { return backup.Policy{SubjectKind: backup.PolicyProject} },
		Build: func(ctx context.Context, policy *backup.Policy, update types.UpdateGlobalProjectBackupPolicy) error {
			if !update.SelectionMode.Valid() {
				return common.Classify(common.ErrValidation, errors.New("selectionMode must be all, allowlist, or blocklist"))
			}
			normalized, validateErr := backup.ValidatePolicyUpdate(ctx, "project", update.UpdateBackupPolicy, s.deps.S3Destinations)
			if validateErr != nil {
				return common.Classify(common.ErrValidation, validateErr)
			}
			projectIDs := kit.Unique(kit.TrimNonEmpty(update.ProjectIDs))
			slices.Sort(projectIDs)
			*policy = backup.Policy{ //exhaustruct:enforce
				BaseModel:       policy.BaseModel,
				SubjectKind:     backup.PolicyProject,
				SubjectID:       "",
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
	if runErr := reconcile.Run(ctx, updates); runErr != nil {
		return types.GlobalProjectBackupPolicyCollection{}, runErr
	}
	return s.GlobalPolicies(ctx)
}

// GlobalOptions lists the projects a global policy can select, followed by the listed ones that no longer exist.
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

// StartGlobal runs the enabled global policy now, as its schedule does.
func (s *Service) StartGlobal(ctx context.Context, user types.Actor, policyID string) (types.BackupRunAccepted, error) {
	requester := backup.NewRequester(ctx, user, "0", authz.PermSystemBackupsCreate)
	if err := s.deps.Engine.Authorize(ctx, requester); err != nil {
		return types.BackupRunAccepted{}, err
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
	activityID, err := s.flow.Submit(ctx, s.global, globalRun{Requester: &requester, PolicyID: policyID}, activity.StartRequest{
		StartedBy: &user,
		Metadata:  database.JSON{"action": "run_project_backups", "policyId": policyID},
	})
	if err != nil {
		return types.BackupRunAccepted{}, err
	}
	return types.BackupRunAccepted{ActivityID: activityID, Status: "running"}, nil
}

func (s *Service) rescheduleGlobal(ctx context.Context, policy *backup.Policy) {
	// Agents run no global policies yet.
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
		ScheduleFn: s.deps.Policies.Schedule(policyID),
		// A run whose workflow cannot be resumed settles its checkpointed backup and backs up the rest, once.
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

// backUpGlobal backs up the projects a global policy selects one at a time, each through the path of a project's
// scheduled backup. A resumed run first finishes, or fails, the backup the interrupted attempt checkpointed, as a project
// policy's resumed run does, then backs up the projects that attempt had not finished.
func (s *Service) backUpGlobal(ctx context.Context, input globalRun, previous types.SchedulerRun) (types.SchedulerOutcome, error) {
	canceled := types.SchedulerOutcome{Status: types.RunStatusCanceled, Message: "Backup policy disabled or deleted"}
	lease, err := s.deps.Engine.AwaitRun(ctx, globalAdmissionScope, input.PolicyID)
	if err != nil {
		return types.SchedulerOutcome{}, err
	}
	defer lease.Release(context.WithoutCancel(ctx))
	finished := make(map[string]types.TargetOutcome)
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
		if target.Status != types.RunStatusRunning {
			finished[target.ID] = target
			continue
		}
		resumeErr := s.deps.Resume(ctx, previous, input.Requester, target.RecoveryData)
		switch {
		case ctx.Err() != nil:
			return types.SchedulerOutcome{}, ctx.Err()
		case errors.Is(resumeErr, actor.ErrJobRejected):
			return types.SchedulerOutcome{}, resumeErr
		case errors.Is(resumeErr, backup.ErrBackupSettled):
			// The backup failed, so a new one runs below, as for a project policy.
			continue
		}
		if reportErr := report(target.ID, resumeErr); reportErr != nil {
			return types.SchedulerOutcome{}, reportErr
		}
	}
	// The checkpointed backups are settled even when the policy was disabled or deleted since.
	policy, err := s.deps.Policies.Get(ctx, backup.PolicyProject, "", input.PolicyID)
	if err != nil {
		return types.SchedulerOutcome{}, err
	}
	if policy == nil || !policy.Enabled {
		return canceled, nil
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
		// Shutdown interrupted this project; its checkpoint resumes it first.
		case ctx.Err() != nil:
			return types.SchedulerOutcome{}, ctx.Err()
		}
		if reportErr := report(project.ID, backupErr); reportErr != nil {
			return types.SchedulerOutcome{}, reportErr
		}
	}
	return globalOutcome(finished, remoteDisabled), nil
}

// globalSettings reads the global policy under the project's admission, checks the requester, that the policy still
// selects the project and that the project has no enabled policy of its own, then checks the S3 repository.
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
	references, err := s.deps.ListProjects(ctx)
	if err != nil {
		return types.CreateProjectBackupRequest{}, false, err
	}
	if !slices.ContainsFunc(selectProjects(*policy, references), func(project types.ProjectReference) bool { return project.ID == projectID }) {
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

// globalOutcome reports a global run; failed projects or disabled remote storage make it partial.
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
