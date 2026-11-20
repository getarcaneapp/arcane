package backup

import (
	"cmp"
	"context"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"errors"
	"fmt"
	"log/slog"
	"maps"
	"path"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"
	"uuid"

	"github.com/containerd/errdefs"
	"github.com/docker/compose/v5/pkg/api"
	"github.com/italypaleale/francis/actor"
	"github.com/italypaleale/francis/builtin/workflow"
	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/api/types/mount"
	"github.com/moby/moby/client"
	"gorm.io/gorm"

	"kit.getarcane.app"
	"kit.getarcane.app/concurrency"
	"kit.getarcane.app/docker"
	"kit.getarcane.app/updater/labels"

	"getarcane.dev/arcane/v3/internal/backup"
	"getarcane.dev/arcane/v3/internal/common"
	"getarcane.dev/arcane/v3/internal/config"
	"getarcane.dev/arcane/v3/internal/database"
	dockerInternal "getarcane.dev/arcane/v3/internal/docker"
	"getarcane.dev/arcane/v3/internal/middleware"
	"getarcane.dev/arcane/v3/internal/s3"
	"getarcane.dev/arcane/v3/internal/scheduler/entityjobs"
	"getarcane.dev/arcane/v3/internal/scheduler/flow"
	"getarcane.dev/arcane/v3/internal/scheduler/jobcontext"
	"getarcane.dev/arcane/v3/internal/scheduler/runs"
	"getarcane.dev/arcane/v3/internal/settings"
	"getarcane.dev/arcane/v3/pkg/activity"
	"getarcane.dev/arcane/v3/pkg/authz"
	"getarcane.dev/arcane/v3/pkg/pagination"
	"getarcane.dev/arcane/v3/pkg/projects"
	"getarcane.dev/arcane/v3/pkg/rustic"
	"getarcane.dev/arcane/v3/pkg/timeouts"
	"getarcane.dev/arcane/v3/pkg/volumes"
	"getarcane.dev/types/v3"
)

type (
	Dependencies struct {
		DB         *database.DB
		Docker     *dockerInternal.DockerClientService
		Engine     *backup.Engine
		Repository *backup.VolumeRepository
		// ForgetSnapshots deletes snapshots from a repository and prunes it once.
		ForgetSnapshots func(ctx context.Context, dockerClient *client.Client, repository backup.Repository, password string, snapshotIDs []string) error
		S3Destinations  *s3.S3DestinationService
		Settings        *settings.SettingsService
		// LockVolumes waits for the named volumes' workspace locks, which volume backups and restores also take.
		LockVolumes func(ctx context.Context, names []string) (func(), error)
		// NoteStopped records the project's containers a backup is stopping, and ForgetStopped drops the note once
		// they run again; RecoverStopped starts the project's recovery, which starts the noted containers, when they
		// could not be started. NotedStopped reports whether the note remains; a deploy, down or archive drops it.
		NoteStopped    func(ctx context.Context, projectID string, stopped []container.Summary) error
		ForgetStopped  func(ctx context.Context, projectID string) error
		NotedStopped   func(ctx context.Context, projectID string) (bool, error)
		RecoverStopped func()
		Project        func(ctx context.Context, projectID string, opts types.ProjectDetailsOptions) (types.Project, error)
		// Volume inspects a Docker volume and reports whether it exists.
		Volume func(ctx context.Context, name string) (types.Volume, bool, error)
		// BackupStore names the volume holding local backup storage, which a backup never copies.
		BackupStore func(ctx context.Context) string
		// LockProject takes the project lock that deploy, down and project edits also take.
		LockProject func(ctx context.Context, op, projectID string) (func(), error)
		// StartContainers starts the project's stopped containers again; recovery starts a noted project's the same way.
		StartContainers func(ctx context.Context, projectID string, stopped []container.Summary) error
		// ComposeFiles returns the compose files the project loads, relative to its directory.
		ComposeFiles func(ctx context.Context, projectID string) ([]string, error)
		// SoleDirectory refuses a project directory that is a projects directory or holds another project.
		SoleDirectory func(ctx context.Context, project types.Project) error
		// RunsArcane reports whether the project's containers include the running Arcane container.
		RunsArcane func(ctx context.Context, projectID string) (bool, error)
		// Down brings the project down without removing its volumes and Deploy deploys it; the caller holds the project lock.
		Down   func(ctx context.Context, projectID string, user types.Actor) error
		Deploy func(ctx context.Context, projectID string, user types.Actor, options *types.ProjectDeployOptions) error
		// RewriteComposeName sets the Compose project name in the project's files, as an import does.
		RewriteComposeName func(ctx context.Context, projectID, name string) error
		// ProjectName returns the project's name without loading its compose files.
		ProjectName func(ctx context.Context, projectID string) (string, error)
		// ListProjects returns the ID and name of every project on this server.
		ListProjects func(ctx context.Context) ([]types.ProjectReference, error)
		// ProjectRoot returns the projects directory holding the project directory, which project file writes stay in.
		ProjectRoot func(ctx context.Context, projectPath string) (string, error)
		// Projects lists every project, archived ones included.
		Projects   func(ctx context.Context) ([]types.ProjectRecord, error)
		Activities activity.MessageAppender
		// Config tells a manager, which runs the global policies, from an agent.
		Config *config.Config
		// Ctx, Scheduler and Admission register the policies' scheduled jobs.
		Ctx       context.Context
		Scheduler types.DynamicScheduler
		Admission *runs.Admission
	}

	Service struct {
		deps      Dependencies
		jobs      *entityjobs.Registry
		flow      *flow.Engine
		workflow  *flow.Workflow
		restores  *flow.Workflow
		scheduled *flow.Workflow
		global    *flow.Workflow
		// policyLocks serializes saving a project's policies with updating their jobs.
		policyLocks concurrency.KeyedMutex
		// globalConfig serializes every read-modify-save of the global policies' setting.
		globalConfig sync.Mutex
	}

	StartOptions struct {
		User          types.Actor
		EnvironmentID string
		ProjectID     string
		Request       types.CreateProjectBackupRequest
	}

	// plan is a run's frozen settings; it doubles as its recovery checkpoint.
	plan struct {
		BackupID        string                `json:"backupId"`
		ProjectID       string                `json:"projectId"`
		PolicyID        string                `json:"policyId,omitempty"`
		S3DestinationID string                `json:"s3DestinationId,omitempty"`
		Volumes         []types.ProjectVolume `json:"volumes"`
		Stopped         []container.Summary   `json:"stopped,omitempty"`
		LocalEnabled    bool                  `json:"localEnabled"`
		S3Enabled       bool                  `json:"s3Enabled"`
		StopContainers  bool                  `json:"stopContainers"`
	}

	runInput struct {
		Requester types.BackupRequester `json:"requester"`
		Plan      plan                  `json:"plan"`
	}

	scheduledRun struct {
		PolicyID string `json:"policyId"`
	}

	// globalRun runs a global policy; a run started by hand also carries its requester and the claim on the policy's
	// admission its start parked.
	globalRun struct {
		Requester *types.BackupRequester `json:"requester,omitempty"`
		PolicyID  string                 `json:"policyId"`
		Claim     string                 `json:"claim,omitempty"`
	}

	// globalPlan is the checkpoint of the projects a global run backs up.
	globalPlan struct {
		ProjectIDs []string `json:"projectIds"`
	}

	// admittedRun is a scheduled backup's plan, resolved under the admissions its leases hold, and the policy ID its
	// backup is recorded under.
	admittedRun struct {
		leases         []*runs.Lease
		plan           plan
		policyID       string
		project        types.Project
		remoteDisabled bool
	}

	StartRestoreOptions struct {
		User          types.Actor
		EnvironmentID string
		ProjectID     string
		BackupID      string
		Request       types.RestoreProjectBackupRequest
	}

	// restorePlan is an in-place restore's frozen request and, as it progresses, its recovery checkpoint.
	restorePlan struct {
		Redeploy  *bool  `json:"redeploy,omitempty"`
		ID        string `json:"id"`
		BackupID  string `json:"backupId"`
		ProjectID string `json:"projectId"`
		// ProjectPath is set when the project files are restored.
		ProjectPath string `json:"projectPath,omitempty"`
		// ComposeName is the project's Compose name before the restore, which restored files must keep.
		ComposeName string                             `json:"composeName"`
		SafetyID    string                             `json:"safetyId,omitempty"`
		Failure     string                             `json:"failure,omitempty"`
		Volumes     []types.ProjectBackupRestoreVolume `json:"volumes"`
		// Create are the volumes the restore creates, claimed with restoreOwner and its ID; a rollback removes those
		// that carry the claim.
		Create []client.VolumeCreateOptions `json:"create,omitempty"`
		Files  bool                         `json:"files"`
		// MayStop records that the project ran when the restore was requested, so the caller's right to stop it was checked.
		MayStop bool `json:"mayStop"`
		// Stopped records that the project had stopped containers, which the restore removes; projects:down was checked.
		Stopped     bool `json:"stopped,omitempty"`
		Running     bool `json:"running"`
		Started     bool `json:"started"`
		Restored    bool `json:"restored"`
		Deploying   bool `json:"deploying"`
		RollingBack bool `json:"rollingBack"`
	}

	restoreRun struct {
		Requester types.BackupRequester `json:"requester"`
		User      types.Actor           `json:"user"`
		Plan      restorePlan           `json:"plan"`
	}

	// restoreSteps are the operations an in-place restore applies, bound to its run; tests pass fakes.
	restoreSteps struct {
		record  func(ctx context.Context, p restorePlan) error
		report  func(ctx context.Context, message string)
		down    func(ctx context.Context) error
		clear   func(ctx context.Context, p restorePlan) error
		deploy  func(ctx context.Context) error
		safety  func(ctx context.Context, p restorePlan) (string, error)
		inUse   func(ctx context.Context, p restorePlan) error
		create  func(ctx context.Context, volume volumes.ClaimedVolume) error
		restore func(ctx context.Context, opts types.ProjectBackupRestore) error
		verify  func(ctx context.Context, p restorePlan) error
		remove  func(ctx context.Context, names []string, claim string) error
	}

	snapshotSource struct {
		password   string
		id         string
		repository backup.Repository
	}
)

const (
	// restoreOwner and the restore's ID label its Rustic containers, the same for the restore and its rollback, so
	// either finds one the other left.
	restoreOwner   = "project-restore:"
	admissionScope = "project-backup"
	// globalAdmissionScope keeps one run of each global policy at a time; it is always taken before a project's.
	globalAdmissionScope = "global-project-backup"
	snapshotRoot         = "/snapshot"
	restoreRoot          = "/dest"
	manifestName         = "arcane-project.json"
	// envName holds, beside the manifest, the file a project's .env links to outside the project directory.
	envName           = "project.env"
	targetType        = "project_backup"
	restoreTargetType = "project_restore"
	defaultSchedule   = "0 2 * * *"
	backupsTable      = "project_backups"
	globalConfigKey   = "projectBackupConfig"
	// globalPlanTarget records the projects a global policy's run backs up.
	globalPlanTarget = "global-project-plan"
	// dockerStopGraceSeconds is how long Docker waits for a container without its own stop grace to exit.
	dockerStopGraceSeconds = 10
)

var (
	errAlreadyRunning     = common.Classify(common.ErrConflict, errors.New("a backup or restore of this project is already running"))
	errGlobalRunning      = common.Classify(common.ErrConflict, errors.New("this backup policy is already running"))
	errPolicyGone         = errors.New("backup policy disabled or deleted")
	errRemoteOnlyDisabled = errors.New("the policy's only destination is missing its S3 repository, so the policy was disabled")
	errProjectSkipped     = errors.New("the project is no longer selected or has its own enabled backup policy")
	// errContainersDown marks a failure that left the project's containers stopped; its checkpoint still lists them.
	errContainersDown = errors.New("project containers left stopped")
	fallbackBackoff   = concurrency.Backoff{Base: 5 * time.Second, Max: 5 * time.Minute}
	projectDetails    = types.ProjectDetailsOptions{
		IncludeServiceConfigs: true,
		IncludeIncludeFiles:   true,
	}
)

func NewService(deps Dependencies) *Service {
	return &Service{
		deps: deps,
		jobs: entityjobs.New(entityjobs.Config{
			Lifecycle:      deps.Ctx,
			Scheduler:      deps.Scheduler,
			Admission:      deps.Admission,
			JobPrefix:      "project-backup:",
			AdmissionScope: admissionScope,
		}),
	}
}

func (s *Service) RegisterWorkflows(engine *flow.Engine) error {
	var err error
	s.flow = engine
	s.workflow, err = engine.Define(flow.Definition{
		Name:        "project-backup",
		Version:     1,
		Fingerprint: "0cad4244ee32070c434e67d79eb193796c68f733291ac7e89a10165ed091b758",
		Concurrency: 4,
		Timeout:     24 * time.Hour,
		Activity:    activity.StartRequest{Type: types.ActivityTypeResourceAction},
		Labels:      map[string]string{"backup": "Creating backup"},
		Steps:       []workflow.StepSpec{workflow.Step("backup", engine.Handler(s.run), workflow.WithMaxAttempts(1))},
	})
	if err != nil {
		return err
	}
	// A restore runs as a workflow so that one interrupted with the project down is rolled back when it resumes.
	s.restores, err = engine.Define(flow.Definition{
		Name:        "project-restore",
		Version:     1,
		Fingerprint: "34a558e74ada5f164a05b1b41ea0e7b81f7fe1634d9afc9b11a0819d3ade60b3",
		Concurrency: 4,
		Timeout:     24 * time.Hour,
		Activity:    activity.StartRequest{Type: types.ActivityTypeResourceAction},
		Labels:      map[string]string{"restore": "Restoring backup"},
		Steps:       []workflow.StepSpec{workflow.Step("restore", engine.Handler(s.runRestore), workflow.WithMaxAttempts(1))},
	})
	if err != nil {
		return err
	}
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

// StartBackup admits the run, records its row, and hands it to the workflow; a busy project or volume fails fast.
func (s *Service) StartBackup(ctx context.Context, opts StartOptions) (types.ProjectBackup, error) {
	requester := backup.NewRequester(ctx, opts.User, opts.EnvironmentID, authz.PermProjectsBackup)
	project, err := s.deps.Project(ctx, opts.ProjectID, projectDetails)
	if err != nil {
		return types.ProjectBackup{}, err
	}
	p, err := s.resolvePlan(ctx, project, opts.Request)
	if err != nil {
		return types.ProjectBackup{}, err
	}
	if authorizeErr := s.deps.Engine.Authorize(ctx, requester); authorizeErr != nil {
		return types.ProjectBackup{}, authorizeErr
	}
	keys := admissions(p.ProjectID, p.volumeNames())
	leases, err := s.admit(ctx, keys, "")
	if err != nil {
		return types.ProjectBackup{}, err
	}
	entry := &ProjectBackup{
		BaseModel:       database.BaseModel{ID: uuid.New().String(), CreatedAt: time.Now()},
		ProjectID:       project.ID,
		ProjectName:     project.Name,
		ComposeName:     project.ComposeName,
		Status:          types.VolumeBackupStatusRunning,
		Trigger:         types.VolumeBackupTriggerManual,
		Destination:     destination(p.LocalEnabled, p.S3Enabled),
		S3DestinationID: p.S3DestinationID,
		Volumes:         backupVolumes(p.Volumes),
	}
	if createErr := s.deps.DB.WithContext(ctx).Create(entry).Error; createErr != nil {
		release(ctx, leases)
		return types.ProjectBackup{}, fmt.Errorf("failed to save project backup: %w", createErr)
	}
	p.BackupID = entry.ID
	releaseHeld := s.hold(keys, entry.ID, leases)
	activityID, err := s.flow.Submit(ctx, s.workflow, runInput{Requester: requester, Plan: p}, activity.StartRequest{
		EnvironmentID: opts.EnvironmentID, ResourceType: new("project"), ResourceID: new(project.ID), ResourceName: new(project.Name), StartedBy: &opts.User,
		Step: "Creating backup", LatestMessage: "Creating project backup",
		Metadata: database.JSON{"action": "create_project_backup", "backupId": entry.ID, "destination": entry.Destination, "s3DestinationId": entry.S3DestinationID},
	}, backup.AcceptedTarget(entry.ID))
	if err != nil {
		releaseHeld(ctx)
		return types.ProjectBackup{}, errors.Join(err, s.fail(context.WithoutCancel(ctx), entry.ID, err.Error()))
	}
	// A backup canceled before its task runs never takes the parked leases.
	go func() {
		background := context.WithoutCancel(ctx)
		s.watch(background, entry.ID, activityID)
		releaseHeld(background)
	}()
	return entry.DTO(DTOOptions{ActivityID: &activityID}), nil
}

// resolvePlan validates the request against the project; by default it captures every volume that exists and keeps its
// data in Docker.
func (s *Service) resolvePlan(ctx context.Context, project types.Project, request types.CreateProjectBackupRequest) (plan, error) {
	selected := cmp.Or(request.Destination, types.VolumeBackupDestinationLocal)
	if !slices.Contains([]types.VolumeBackupDestination{types.VolumeBackupDestinationLocal, types.VolumeBackupDestinationS3, types.VolumeBackupDestinationLocalS3}, selected) {
		return plan{}, common.Classify(common.ErrBadRequest, errors.New("invalid project backup destination"))
	}
	s3Enabled := selected != types.VolumeBackupDestinationLocal
	s3DestinationID := kit.Ternary(s3Enabled, strings.TrimSpace(request.S3DestinationID), "")
	if s3Enabled {
		if s3DestinationID == "" {
			return plan{}, common.Classify(common.ErrBadRequest, errors.New("select an S3 destination for the project backup"))
		}
		if _, err := s.deps.S3Destinations.Configuration(ctx, s3DestinationID); err != nil {
			return plan{}, fmt.Errorf("load S3 destination: %w", err)
		}
	}
	if project.ComposeName == "" {
		return plan{}, common.Classify(common.ErrValidation, fmt.Errorf("the compose files of project %s cannot be loaded; fix them before backing it up", project.Name))
	}
	planned, err := s.inspectVolumes(ctx, project, request.Volumes)
	if err != nil {
		return plan{}, err
	}
	return plan{
		ProjectID:       project.ID,
		S3DestinationID: s3DestinationID,
		Volumes:         planned,
		LocalEnabled:    selected != types.VolumeBackupDestinationS3,
		S3Enabled:       s3Enabled,
		StopContainers:  *cmp.Or(request.StopContainers, new(true)),
	}, nil
}

// inspectVolumes classifies the project's volumes and includes the requested ones, or by default every one, that exist
// and keep their data in Docker, except the backup store; requesting any other volume fails.
func (s *Service) inspectVolumes(ctx context.Context, project types.Project, requested []string) ([]types.ProjectVolume, error) {
	resolved, dockerVolumes, err := projects.ResolveProjectVolumes(ctx, projects.ResolveProjectVolumesOptions{
		Project:     project,
		Inspect:     s.deps.Volume,
		BackupStore: s.deps.BackupStore(ctx),
	})
	if err != nil {
		return nil, err
	}
	inspected := make([]types.ProjectVolume, 0, len(resolved))
	for _, volume := range resolved {
		wanted := requested == nil || slices.Contains(requested, volume.Key)
		switch {
		case wanted && requested != nil && volume.BackupStore:
			// A backup of the store would read the repository it writes into, copying it into itself on every run.
			return nil, common.Classify(common.ErrValidation, fmt.Errorf("volume %s holds Arcane's backups, so it cannot be backed up", volume.Key))
		case wanted && requested != nil && volume.Missing:
			return nil, common.Classify(common.ErrValidation, fmt.Errorf("volume %s has not been created yet, so it holds no data to back up", volume.Key))
		case wanted && requested != nil && !volume.Included:
			return nil, common.Classify(common.ErrValidation, fmt.Errorf("volume %s keeps its data outside Docker (%s), so it cannot be backed up", volume.Key, volume.Kind))
		}
		inspected = append(inspected, types.ProjectVolume{
			Labels: volume.Labels,
			// A restore recreates a missing external volume with its own labels, not Compose's.
			DockerLabels: kit.Ternary(volume.Kind == types.ProjectVolumeExternal && volume.Included, dockerVolumes[volume.Name].Labels, nil),
			DriverOpts:   volume.DriverOpts,
			Key:          volume.Key,
			Name:         volume.Name,
			Kind:         volume.Kind,
			Driver:       volume.Driver,
			Included:     volume.Included && wanted,
			Missing:      volume.Missing,
			BackupStore:  volume.BackupStore,
		})
	}
	for _, key := range requested {
		if !slices.ContainsFunc(inspected, func(volume types.ProjectVolume) bool { return volume.Key == key }) {
			return nil, common.Classify(common.ErrValidation, fmt.Errorf("project %s has no volume %s", project.Name, key))
		}
	}
	return inspected, nil
}

// mountedVolumeNames are every volume of the project, captured or not. A volume backup stops and starts the containers
// mounting its volume under that volume's lock, so holding them all keeps any of the project's containers from being
// started or stopped by another backup.
func (p plan) mountedVolumeNames() []string {
	var names []string
	for _, volume := range p.Volumes {
		if volume.Name != "" {
			names = append(names, volume.Name)
		}
	}
	slices.Sort(names)
	return slices.Compact(names)
}

func (p plan) volumeNames() []string {
	var names []string
	for _, volume := range p.Volumes {
		if volume.Included {
			names = append(names, volume.Name)
		}
	}
	slices.Sort(names)
	return slices.Compact(names)
}

// admissions are the project's run and a volume-backup run for each volume, in the order every run takes them.
func admissions(projectID string, volumeNames []string) []types.SchedulerAdmissionKey {
	keys := []types.SchedulerAdmissionKey{{Scope: admissionScope, ID: projectID}}
	for _, name := range volumeNames {
		keys = append(keys, types.SchedulerAdmissionKey{Scope: backup.VolumeAdmissionScope, ID: name})
	}
	return keys
}

// admit takes every admission without waiting; run takes over the leases a start parked for it.
func (s *Service) admit(ctx context.Context, keys []types.SchedulerAdmissionKey, run string) ([]*runs.Lease, error) {
	leases := make([]*runs.Lease, 0, len(keys))
	for _, key := range keys {
		lease, admitted, err := s.deps.Engine.AcquireRun(ctx, key.Scope, key.ID, run)
		switch {
		case err == nil && !admitted && key.Scope == admissionScope:
			err = errAlreadyRunning
		case err == nil && !admitted:
			err = common.Classify(common.ErrConflict, fmt.Errorf("a backup or restore of volume %s is already running", key.ID))
		}
		if err != nil {
			release(ctx, leases)
			return nil, err
		}
		leases = append(leases, lease)
	}
	return leases, nil
}

// hold parks the leases a start took until the run it started takes them over, and returns what releases the ones the
// run never took.
func (s *Service) hold(keys []types.SchedulerAdmissionKey, run string, leases []*runs.Lease) func(context.Context) {
	releases := make([]func(context.Context), 0, len(leases))
	for index, key := range keys {
		releases = append(releases, s.deps.Engine.Hold(key.Scope, key.ID, run, leases[index]))
	}
	return func(ctx context.Context) {
		for _, releaseLease := range releases {
			releaseLease(ctx)
		}
	}
}

func release(ctx context.Context, leases []*runs.Lease) {
	for _, lease := range leases {
		lease.Release(ctx)
	}
}

// run executes an accepted backup, or resumes one whose checkpoint an earlier attempt recorded.
func (s *Service) run(ctx context.Context, t flow.Task) (_ any, err error) {
	var input runInput
	if decodeErr := t.Payload(&input); decodeErr != nil {
		return nil, decodeErr
	}
	defer func() {
		if err != nil && ctx.Err() == nil && !errors.Is(err, actor.ErrJobRejected) && !errors.Is(err, backup.ErrBackupSettled) {
			err = errors.Join(err, s.fail(ctx, input.Plan.BackupID, err.Error()))
		}
	}()
	succeeded := types.SchedulerOutcome{Status: types.RunStatusSucceeded, Message: "Project backup created successfully"}
	previous, _ := jobcontext.Run(ctx)
	index := slices.IndexFunc(previous.Outcome.Targets, func(target types.TargetOutcome) bool {
		return target.ID == input.Plan.ProjectID && len(target.RecoveryData) > 0
	})
	if index >= 0 {
		var recorded plan
		if decodeErr := json.Unmarshal(previous.Outcome.Targets[index].RecoveryData, &recorded); decodeErr != nil {
			return nil, decodeErr
		}
		if resumeErr := s.resume(ctx, previous, &input.Requester, recorded); resumeErr != nil {
			return nil, resumeErr
		}
		return succeeded, nil
	}
	leases, err := s.admit(ctx, admissions(input.Plan.ProjectID, input.Plan.volumeNames()), input.Plan.BackupID)
	if err != nil {
		return nil, err
	}
	defer release(context.WithoutCancel(ctx), leases)
	if authorizeErr := s.deps.Engine.Authorize(ctx, input.Requester); authorizeErr != nil {
		return nil, authorizeErr
	}
	if recordErr := s.record(ctx, input.Plan); recordErr != nil {
		return nil, recordErr
	}
	entry, err := s.load(ctx, input.Plan.BackupID)
	if err != nil {
		return nil, err
	}
	err = s.execute(ctx, entry, input.Plan)
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	if completeErr := s.complete(ctx, entry, err); completeErr != nil {
		return nil, completeErr
	}
	return succeeded, nil
}

// resume takes the admissions again, restarts the containers an interrupted attempt left stopped under the project
// lock unless their stop note is gone, then adopts the snapshots it already wrote and finishes the backup; a scheduled
// backup has no requester.
func (s *Service) resume(ctx context.Context, previous types.SchedulerRun, requester *types.BackupRequester, recorded plan) (err error) {
	// A resume that ends for good, a user cancel included, fails its backup as a first attempt does; shutdown, busy
	// admissions and containers still stopped retry it.
	defer func() {
		if err != nil && !handedBack(ctx, err) && !errors.Is(err, backup.ErrBackupSettled) {
			err = errors.Join(err, s.fail(context.WithoutCancel(ctx), recorded.BackupID, err.Error()))
		}
	}()
	// The admissions and project lock died with the process, so a restore may own the project until they are retaken.
	leases, err := s.admit(ctx, admissions(recorded.ProjectID, recorded.volumeNames()), "")
	// Another run holds an admission, or taking it failed; hand the task back so the stopped containers are restarted
	// on a later attempt instead of being left down.
	if err != nil {
		return fmt.Errorf("%w: %w", actor.ErrJobRejected, err)
	}
	defer release(context.WithoutCancel(ctx), leases)
	if len(recorded.Stopped) > 0 {
		unlockProject, lockErr := s.deps.LockProject(ctx, "back up project", recorded.ProjectID)
		// The project is left stopped, so wait for its turn instead of failing.
		if lockErr != nil {
			return fmt.Errorf("%w: %w: %w", actor.ErrJobRejected, errContainersDown, lockErr)
		}
		// A deploy, down or archive since the stop dropped the project's stop note: a person decided whether it runs,
		// so its containers are left as they are and the run goes on as if they had started.
		noted, notedErr := s.deps.NotedStopped(ctx, recorded.ProjectID)
		if notedErr != nil {
			unlockProject()
			return fmt.Errorf("%w: %w: %w", actor.ErrJobRejected, errContainersDown, notedErr)
		}
		var restartErr error
		if noted {
			// A restore of any of the project's volumes finishes before its containers start again.
			unlockVolumes, volumesErr := s.deps.LockVolumes(ctx, recorded.mountedVolumeNames())
			if volumesErr != nil {
				unlockProject()
				return fmt.Errorf("%w: %w: %w", actor.ErrJobRejected, errContainersDown, volumesErr)
			}
			restartErr = s.deps.StartContainers(context.WithoutCancel(ctx), recorded.ProjectID, recorded.Stopped)
			s.afterRestart(ctx, recorded.ProjectID, restartErr)
			unlockVolumes()
		}
		unlockProject()
		if restartErr == nil {
			recorded.Stopped = nil
		}
		recordErr := s.record(ctx, recorded)
		// The project stays down until its containers start, so try again later instead of failing; the checkpoint
		// saved before still lists them.
		if restartErr != nil {
			return fmt.Errorf("%w: %w: %w", actor.ErrJobRejected, errContainersDown, errors.Join(restartErr, recordErr))
		}
		if recordErr != nil {
			return recordErr
		}
	}
	entry, err := s.load(ctx, recorded.BackupID)
	if err != nil {
		return err
	}
	if entry.Status == types.VolumeBackupStatusRunning && entry.Error != "" {
		// The interrupted attempt rejected its snapshot after the copy; with the project running again, the backup fails.
		return errors.New(entry.Error)
	}
	switch entry.Status {
	case types.VolumeBackupStatusFailed:
		return fmt.Errorf("%w: %s", backup.ErrBackupSettled, entry.Error)
	case types.VolumeBackupStatusSucceeded:
		return nil
	}
	if requester != nil {
		if authorizeErr := s.deps.Engine.Authorize(ctx, *requester); authorizeErr != nil {
			return authorizeErr
		}
	}
	if adoptErr := s.adopt(ctx, previous, entry, recorded); adoptErr != nil {
		return adoptErr
	}
	err = s.execute(ctx, entry, recorded)
	if ctx.Err() != nil {
		return ctx.Err()
	}
	return s.complete(ctx, entry, err)
}

// adopt records snapshots an interrupted attempt wrote but did not save, found by their run tag.
func (s *Service) adopt(ctx context.Context, previous types.SchedulerRun, entry *ProjectBackup, p plan) error {
	attempted := func(destination string) bool {
		return slices.ContainsFunc(previous.Outcome.Targets, func(target types.TargetOutcome) bool { return target.ID == entry.ID+":"+destination })
	}
	dockerClient, err := s.deps.Docker.GetClient(ctx)
	if err != nil {
		return err
	}
	for _, remote := range []bool{false, true} {
		recorded := kit.Ternary(remote, &entry.RemoteSnapshotID, &entry.LocalSnapshotID)
		if *recorded != "" || !kit.Ternary(remote, p.S3Enabled, p.LocalEnabled) || !attempted(kit.Ternary(remote, "remote", "local")) {
			continue
		}
		repository, repoErr := s.repository(ctx, dockerClient, p, remote)
		if repoErr != nil {
			return repoErr
		}
		password, passwordErr := s.deps.Repository.Password(ctx, dockerClient, repository)
		if passwordErr != nil {
			return passwordErr
		}
		snapshot, found, findErr := s.deps.Engine.FindRunSnapshot(ctx, dockerClient, repository, password, entry.ID, "")
		if findErr != nil {
			return findErr
		}
		if found {
			*recorded, entry.Size = snapshot.ID, cmp.Or(entry.Size, snapshot.Size)
		}
	}
	return s.deps.DB.WithContext(ctx).Save(entry).Error
}

func (s *Service) repository(ctx context.Context, dockerClient *client.Client, p plan, remote bool) (backup.Repository, error) {
	if remote {
		return s.deps.Repository.Remote(ctx, p.S3DestinationID, "")
	}
	return s.deps.Repository.Local(ctx, dockerClient, backup.LocalRepositoryOptions{})
}

// execute takes the snapshot unless it already exists, then replicates a local snapshot to S3 when both are enabled.
func (s *Service) execute(ctx context.Context, entry *ProjectBackup, p plan) (err error) {
	defer concurrency.RecoverToError(&err, "project backup")
	if kit.Ternary(p.LocalEnabled, entry.LocalSnapshotID, entry.RemoteSnapshotID) == "" {
		if captureErr := s.capture(ctx, entry, p); captureErr != nil {
			return captureErr
		}
	}
	if !p.LocalEnabled || !p.S3Enabled || entry.RemoteSnapshotID != "" {
		return nil
	}
	dockerClient, err := s.deps.Docker.GetClient(ctx)
	if err != nil {
		return err
	}
	local, err := s.deps.Repository.Local(ctx, dockerClient, backup.LocalRepositoryOptions{ReadOnly: true})
	if err != nil {
		return err
	}
	remote, err := s.deps.Repository.Remote(ctx, p.S3DestinationID, "")
	if err != nil {
		return err
	}
	password, err := s.deps.Repository.Password(ctx, dockerClient, local, remote)
	if err != nil {
		return err
	}
	if progressErr := jobcontext.Progress(ctx, types.TargetOutcome{ResourceType: "backup_destination", ID: entry.ID + ":remote", Status: types.RunStatusRunning}); progressErr != nil {
		return progressErr
	}
	snapshot, err := s.deps.Engine.Replicate(ctx, dockerClient, local, entry.LocalSnapshotID, remote, password, entry.ProjectName, snapshotTags(entry.ProjectID, entry.ID)...)
	if err != nil {
		return fmt.Errorf("failed to replicate the project snapshot to S3: %w", err)
	}
	entry.RemoteSnapshotID = snapshot.ID
	if saveErr := s.deps.DB.WithContext(ctx).Save(entry).Error; saveErr != nil {
		return saveErr
	}
	return jobcontext.Progress(ctx, types.TargetOutcome{ResourceType: "backup_destination", ID: entry.ID + ":remote", Status: types.RunStatusSucceeded, Message: snapshot.ID})
}

// capture writes the manifest, stops the project when asked, and takes one snapshot of its files and volumes.
func (s *Service) capture(ctx context.Context, entry *ProjectBackup, p plan) error {
	// Like a consistent export, hold the project lock so nothing deploys or edits the project while it is stopped.
	if p.StopContainers {
		unlockProject, lockErr := s.deps.LockProject(ctx, "back up project", entry.ProjectID)
		if lockErr != nil {
			return lockErr
		}
		defer unlockProject()
	}
	unlock, err := s.deps.LockVolumes(ctx, p.mountedVolumeNames())
	if err != nil {
		return err
	}
	defer unlock()
	return s.snapshot(ctx, entry, p)
}

// snapshot takes the snapshot capture describes; the caller holds the volume locks, and the project lock when it stops
// containers.
func (s *Service) snapshot(ctx context.Context, entry *ProjectBackup, p plan) (err error) {
	dockerClient, err := s.deps.Docker.GetClient(ctx)
	if err != nil {
		return err
	}
	project, err := s.deps.Project(ctx, entry.ProjectID, projectDetails)
	if err != nil {
		return err
	}
	// The plan's locks and leases cover the volumes found when the backup was queued.
	if planErr := checkPlanVolumes(project, p); planErr != nil {
		return planErr
	}
	// Containers outside the project keep running, so a volume they mount may change while it is captured. A
	// restarting container is left out of the running count but can still write its volumes.
	active, inUse, err := s.containerUse(ctx, project.ComposeName)
	if err != nil {
		return err
	}
	p.Volumes = markInUse(p.Volumes, inUse)
	// A volume removed while the backup waited would be mounted, and so created, empty; one replaced during the copy
	// would be captured empty too. Each must exist now and be the same volume after the copy.
	captured, err := s.volumeIdentities(ctx, p.volumeNames())
	if err != nil {
		return err
	}
	composeFiles, err := s.deps.ComposeFiles(ctx, entry.ProjectID)
	if err != nil {
		return err
	}
	// The snapshot copies the project directory, which holds only the link of a .env kept outside it.
	env, _, externalEnv, err := projects.ReadExternalEnvFile(ctx, project.Path)
	if err != nil {
		return fmt.Errorf("read the .env file of project %s: %w", project.Name, err)
	}
	manifest := backupManifest(project, p, len(active) > 0, composeFiles, externalEnv)
	metaVolume, removeMeta, err := s.writeManifest(ctx, dockerClient, manifest, env)
	if err != nil {
		return err
	}
	defer removeMeta()
	projectMounts, err := projectSourceMounts(ctx, dockerClient, backup.SourceMountOptions{ContainerPath: project.Path, Target: path.Join(snapshotRoot, "project")})
	if err != nil {
		return err
	}
	remote := !p.LocalEnabled
	repository, err := s.repository(ctx, dockerClient, p, remote)
	if err != nil {
		return err
	}
	password, err := s.deps.Repository.Password(ctx, dockerClient, repository)
	if err != nil {
		return err
	}
	var (
		stopped []container.Summary
		noted   bool
	)
	defer func() {
		if len(stopped) == 0 {
			if noted {
				s.afterRestart(ctx, entry.ProjectID, nil)
			}
			return
		}
		// Left down, the project is restarted from the checkpoint when the run is tried again, or by recovery.
		restartErr := s.deps.StartContainers(context.WithoutCancel(ctx), entry.ProjectID, stopped)
		s.afterRestart(ctx, entry.ProjectID, restartErr)
		if restartErr != nil {
			err = fmt.Errorf("%w: %w: %w", actor.ErrJobRejected, errContainersDown, errors.Join(err, restartErr))
		}
	}()
	if p.StopContainers {
		// Saved before the stops, so a run resumed after a crash partway through restarts what was stopped; starting
		// one that still runs does nothing.
		p.Stopped = checkpointContainers(active)
		if recordErr := s.record(ctx, p); recordErr != nil {
			return recordErr
		}
		// Noted before the stops too, so the containers start again even when this run can never be resumed.
		var noteErr error
		if noted, noteErr = s.note(ctx, entry.ProjectID, p.Stopped); noteErr != nil {
			return fmt.Errorf("note that the backup stops project %s: %w", project.Name, noteErr)
		}
		var stopErr error
		stopped, stopErr = s.stopContainers(ctx, project.ComposeName)
		p.Stopped = checkpointContainers(mergeContainers(active, stopped))
		// A container that started after the list was stopped too, so the note lists it as well.
		if len(p.Stopped) > len(active) {
			noted, noteErr = s.note(ctx, entry.ProjectID, p.Stopped)
		}
		if recordErr := errors.Join(stopErr, noteErr, s.record(ctx, p)); recordErr != nil {
			return recordErr
		}
		entry.StoppedContainers = len(stopped) > 0
	}
	destinationID := entry.ID + ":" + kit.Ternary(remote, "remote", "local")
	if progressErr := jobcontext.Progress(ctx, types.TargetOutcome{ResourceType: "backup_destination", ID: destinationID, Status: types.RunStatusRunning}); progressErr != nil {
		return progressErr
	}
	snapshot, err := s.deps.Engine.CreateSnapshot(ctx, dockerClient, repository, password, project.Name, snapshotInput(projectMounts, metaVolume, entry.ID, p))
	if err != nil {
		return fmt.Errorf("failed to create the project snapshot: %w", err)
	}
	*kit.Ternary(remote, &entry.RemoteSnapshotID, &entry.LocalSnapshotID), entry.Size = snapshot.ID, snapshot.Size
	if saveErr := s.deps.DB.WithContext(ctx).Save(entry).Error; saveErr != nil {
		return saveErr
	}
	// Saved first, so retention removes the snapshot of a backup that fails here. A shutdown can't cut the check short:
	// a cancelled check would reject a snapshot the copy finished, which a retry would otherwise complete. It is still
	// bounded, since the containers stay stopped and the locks held until it returns.
	checkCtx, cancelCheck := context.WithTimeout(context.WithoutCancel(ctx), timeouts.DefaultDockerAPI)
	after, err := s.volumeIdentities(checkCtx, p.volumeNames())
	cancelCheck()
	if err == nil && !maps.Equal(captured, after) {
		err = common.Classify(common.ErrConflict, errors.New("a volume was removed or replaced while it was copied, so the backup failed; start it again"))
	}
	if err != nil && len(stopped) > 0 {
		// Noted on the backup before the restart, even during a shutdown, so a retry fails it instead of completing the
		// rejected snapshot. The backup stays running until the containers start, since that keeps a disabled schedule's
		// recovery; the note and the failure are written to the same store.
		err = errors.Join(err, s.reject(context.WithoutCancel(ctx), entry.ID, err.Error()))
	}
	if len(stopped) > 0 {
		restartErr := s.deps.StartContainers(context.WithoutCancel(ctx), entry.ProjectID, stopped)
		s.afterRestart(ctx, entry.ProjectID, restartErr)
		stopped, noted = nil, false
		if restartErr == nil {
			p.Stopped = nil
		}
		recordErr := s.record(ctx, p)
		// The snapshot is saved; the retry restarts the project from the checkpoint saved after the stop, then finishes
		// the backup, or fails it when its rejection was noted.
		if restartErr != nil {
			return fmt.Errorf("%w: %w: %w", actor.ErrJobRejected, errContainersDown, errors.Join(err, restartErr, recordErr))
		}
		if recordErr != nil && err == nil {
			return recordErr
		}
	}
	// A rejected snapshot whose containers run again fails here.
	if err != nil {
		return errors.Join(err, s.fail(context.WithoutCancel(ctx), entry.ID, err.Error()))
	}
	return jobcontext.Progress(ctx, types.TargetOutcome{ResourceType: "backup_destination", ID: destinationID, Status: types.RunStatusSucceeded, Message: snapshot.ID})
}

// volumeIdentities returns when Docker created each volume, refusing one that no longer exists.
func (s *Service) volumeIdentities(ctx context.Context, names []string) (map[string]string, error) {
	created := make(map[string]string, len(names))
	for _, name := range names {
		found, exists, err := s.deps.Volume(ctx, name)
		switch {
		case err != nil:
			return nil, fmt.Errorf("inspect volume %s: %w", name, err)
		case !exists:
			return nil, common.Classify(common.ErrConflict, fmt.Errorf("volume %s was removed after the backup was queued, so it was not backed up; start the backup again", name))
		}
		created[name] = found.CreatedAt
	}
	return created, nil
}

// containerUse lists the Compose project's running and restarting containers, the ones a stop stops, and the volumes
// running containers outside it mount. Arcane's own containers are never stopped, so they count as outside; its
// file-browser helpers only write while a file operation holds the volume's lock, so an idle one doesn't count.
func (s *Service) containerUse(ctx context.Context, composeName string) (active []container.Summary, inUse map[string]bool, err error) {
	dockerClient, err := s.deps.Docker.GetClient(ctx)
	if err != nil {
		return nil, nil, err
	}
	running, err := dockerClient.ContainerList(ctx, client.ContainerListOptions{})
	if err != nil {
		return nil, nil, fmt.Errorf("list containers: %w", err)
	}
	// An idle file-browser helper mounting a volume is not a writer.
	active, inUse = projects.ProjectContainerUse(composeName, slices.DeleteFunc(running.Items, volumes.IsHelperContainer))
	return active, inUse, nil
}

// stopContainers stops the Compose project's running and restarting containers, except Arcane's own, and returns
// those it stopped, also when it fails.
func (s *Service) stopContainers(ctx context.Context, composeName string) ([]container.Summary, error) {
	dockerClient, err := s.deps.Docker.GetClient(ctx)
	if err != nil {
		return nil, err
	}
	var stopped []container.Summary
	for pass := range 2 {
		found, listErr := projects.ListComposeProjectContainers(ctx, dockerClient, composeName)
		if listErr != nil {
			return stopped, listErr
		}
		var running []container.Summary
		for _, c := range found {
			if (c.State == container.StateRunning || c.State == container.StateRestarting) && !labels.IsArcaneContainer(c.Labels) {
				running = append(running, c)
			}
		}
		switch {
		case len(running) == 0:
			return stopped, nil
		case pass > 0:
			return stopped, fmt.Errorf("%d container(s) are still running; volume data was not copied", len(running))
		}
		stopBounds := make(map[string]time.Duration, len(running))
		for _, c := range running {
			inspected, inspectErr := dockerClient.ContainerInspect(ctx, c.ID, client.ContainerInspectOptions{})
			if inspectErr != nil {
				return stopped, fmt.Errorf("inspect container %s: %w", docker.ContainerNameFromNames(c.Names), inspectErr)
			}
			oneOff := c.Labels[api.OneoffLabel] == "True" || c.Labels[projects.JobRunLabel] != ""
			if oneOff && kit.FromPtr(inspected.Container.HostConfig).AutoRemove {
				return stopped, common.Classify(common.ErrConflict, fmt.Errorf(
					"container %s removes itself when it stops (docker compose run --rm); wait for it to finish, then try again",
					docker.ContainerNameFromNames(c.Names),
				))
			}
			// Docker waits the container's stop grace before it kills it, so the bound only cuts off a daemon that
			// stalls past that. A container told to wait forever is cut off too, since the run holds the project's locks.
			grace := max(kit.FromPtr(kit.FromPtr(inspected.Container.Config).StopTimeout), dockerStopGraceSeconds)
			stopBounds[c.ID] = time.Duration(grace)*time.Second + timeouts.DefaultDockerAPI
		}
		for _, c := range running {
			stopped = append(stopped, c)
			// Not cancelled with the run, so a container is never left half stopped, but bounded, so a stalled daemon
			// can't keep the earlier containers down and the locks held forever.
			stopCtx, cancelStop := context.WithTimeout(context.WithoutCancel(ctx), stopBounds[c.ID])
			_, err = dockerClient.ContainerStop(stopCtx, c.ID, client.ContainerStopOptions{})
			cancelStop()
			if err != nil {
				return stopped, fmt.Errorf("stop container %s: %w", docker.ContainerNameFromNames(c.Names), err)
			}
			if ctxErr := ctx.Err(); ctxErr != nil {
				return stopped, ctxErr
			}
		}
	}
	return stopped, nil
}

// note records the containers the backup stops before it stops them, so recovery starts them again if this run can't.
// It never notes an empty list: a note without containers is an export's, which recovery answers by deploying the
// whole project.
func (s *Service) note(ctx context.Context, projectID string, stopping []container.Summary) (bool, error) {
	if len(stopping) == 0 {
		return false, nil
	}
	if err := s.deps.NoteStopped(ctx, projectID, stopping); err != nil {
		return false, err
	}
	return true, nil
}

// afterRestart drops the project's stop note once its stopped containers run again, or hands them to recovery when they
// could not be started, which keeps retrying even after this run ends.
func (s *Service) afterRestart(ctx context.Context, projectID string, restartErr error) {
	if restartErr != nil {
		s.deps.RecoverStopped()
		return
	}
	if err := s.deps.ForgetStopped(context.WithoutCancel(ctx), projectID); err != nil {
		slog.WarnContext(ctx, "Failed to drop the stop note after restarting the project's containers", "projectId", projectID, "error", err)
	}
}

// checkPlanVolumes fails when the project's volumes changed after the backup was queued, for example by a compose edit
// and deploy, so the snapshot never pairs new compose files with old volume data.
func checkPlanVolumes(project types.Project, p plan) error {
	current := projects.ClassifyProjectVolumes(project)
	same := len(current) == len(p.Volumes) && !slices.ContainsFunc(current, func(volume types.ProjectVolume) bool {
		return !slices.ContainsFunc(p.Volumes, func(planned types.ProjectVolume) bool {
			return planned.Key == volume.Key && planned.Name == volume.Name && planned.Kind == volume.Kind
		})
	})
	if !same {
		return common.Classify(common.ErrConflict, fmt.Errorf("the volumes of project %s changed after the backup was queued; start the backup again", project.Name))
	}
	return nil
}

// markInUse returns the volumes with InUse set on each included one that a container outside the project mounts.
func markInUse(planned []types.ProjectVolume, inUse map[string]bool) []types.ProjectVolume {
	marked := make([]types.ProjectVolume, 0, len(planned))
	for _, volume := range planned {
		marked = append(marked, types.ProjectVolume{
			Labels:       volume.Labels,
			DockerLabels: volume.DockerLabels,
			DriverOpts:   volume.DriverOpts,
			Key:          volume.Key,
			Name:         volume.Name,
			Kind:         volume.Kind,
			Driver:       volume.Driver,
			Archive:      volume.Archive,
			Included:     volume.Included,
			Missing:      volume.Missing,
			InUse:        volume.Included && inUse[volume.Name],
			BackupStore:  volume.BackupStore,
		})
	}
	return marked
}

// mergeContainers adds the containers of extra that first lacks.
func mergeContainers(first, extra []container.Summary) []container.Summary {
	merged := slices.Clone(first)
	for _, c := range extra {
		if !slices.ContainsFunc(merged, func(known container.Summary) bool { return known.ID == c.ID }) {
			merged = append(merged, c)
		}
	}
	return merged
}

// checkpointContainers keeps what restarting a container needs, including the labels that name its Compose service.
func checkpointContainers(stopped []container.Summary) []container.Summary {
	kept := make([]container.Summary, 0, len(stopped))
	for _, summary := range stopped {
		// Mounts are kept so recovery locks the volumes these containers use, even after a Compose edit renames them.
		kept = append(kept, container.Summary{ID: summary.ID, Names: summary.Names, Labels: summary.Labels, Mounts: summary.Mounts})
	}
	return kept
}

func (s *Service) record(ctx context.Context, p plan) error {
	checkpoint, err := json.Marshal(p)
	if err != nil {
		return err
	}
	return jobcontext.Progress(ctx, types.TargetOutcome{ResourceType: targetType, ID: p.ProjectID, Status: types.RunStatusRunning, RecoveryData: checkpoint})
}

func snapshotTags(projectID, backupID string) []string {
	return []string{backup.ProjectSnapshotTag, "arcane-project:" + projectID, backup.RunSnapshotTag(backupID)}
}

// snapshotInput lays the project out under /snapshot: its files at project/, each captured volume at
// volumes/<name>/, and the manifest at meta/.
func snapshotInput(projectMounts []mount.Mount, metaVolume, backupID string, p plan) backup.CreateSnapshotInput {
	mounts := slices.Clone(projectMounts)
	for _, name := range p.volumeNames() {
		mounts = append(mounts, mount.Mount{Type: mount.TypeVolume, Source: name, Target: path.Join(snapshotRoot, "volumes", name), ReadOnly: true})
	}
	mounts = append(mounts, mount.Mount{Type: mount.TypeVolume, Source: metaVolume, Target: path.Join(snapshotRoot, "meta"), ReadOnly: true})
	return backup.CreateSnapshotInput{Mounts: mounts, Sources: []string{snapshotRoot}, AsPath: "/", Tags: snapshotTags(p.ProjectID, backupID)}
}

// nestedTargets names the mounts inside a project folder, given the folder's own mount first.
func nestedTargets(mounts []mount.Mount) []string {
	targets := make([]string, 0, len(mounts)-1)
	for _, nested := range mounts[1:] {
		targets = append(targets, nested.Target)
	}
	return targets
}

// projectSourceMounts resolves the host mounts behind the project directory source names.
func projectSourceMounts(ctx context.Context, dockerClient *client.Client, source backup.SourceMountOptions) ([]mount.Mount, error) {
	mounts, inContainer, err := backup.CurrentMounts(ctx, dockerClient)
	if err != nil {
		return nil, err
	}
	resolved, err := backup.SourceMounts(backup.SourceMountOptions{
		Mounts:        mounts,
		ContainerPath: source.ContainerPath,
		Target:        source.Target,
		InContainer:   inContainer,
		Writable:      source.Writable,
	})
	if err != nil {
		return nil, err
	}
	return volumes.BindSubpaths(ctx, dockerClient, resolved)
}

func backupManifest(project types.Project, p plan, active bool, composeFiles []string, externalEnv bool) types.ProjectManifest {
	// A directory that doesn't resolve has only its configured path; the snapshot then fails to copy it anyway.
	resolved, _ := filepath.EvalSymlinks(project.Path)
	return types.ProjectManifest{
		CreatedAt:     time.Now().UTC(),
		Kind:          types.ProjectManifestKindBackup,
		ArcaneVersion: config.Version,
		Project: types.ProjectManifestProject{
			ID:          project.ID,
			Name:        project.Name,
			ComposeName: project.ComposeName,
		},
		ComposeFiles:  composeFiles,
		ProjectDirs:   slices.Compact([]string{project.Path, cmp.Or(resolved, project.Path)}),
		Volumes:       p.Volumes,
		FormatVersion: types.ProjectManifestFormatVersion,
		Consistent:    (p.StopContainers || !active) && !slices.ContainsFunc(p.Volumes, func(volume types.ProjectVolume) bool { return volume.InUse }),
		ExternalEnv:   externalEnv,
	}
}

// writeManifest stores the manifest in a new volume, with the external .env contents when the manifest records them.
// The holder that receives it mounts only that volume and never starts, because Docker before 29.9 mishandles copies
// into stopped containers with other mounts (#4870).
func (s *Service) writeManifest(ctx context.Context, dockerClient *client.Client, manifest types.ProjectManifest, env []byte) (string, func(), error) {
	data, err := json.Marshal(manifest, jsontext.WithIndent("  "))
	if err != nil {
		return "", nil, fmt.Errorf("encode project backup manifest: %w", err)
	}
	files := map[string][]byte{manifestName: data}
	if manifest.ExternalEnv {
		files[envName] = env
	}
	name := "arcane-project-backup-" + uuid.New().String()
	if _, createErr := dockerClient.VolumeCreate(ctx, client.VolumeCreateOptions{Name: name, Labels: volumes.Labels()}); createErr != nil {
		return "", nil, fmt.Errorf("create project backup manifest volume: %w", createErr)
	}
	remove := func() {
		if _, removeErr := dockerClient.VolumeRemove(context.WithoutCancel(ctx), name, client.VolumeRemoveOptions{Force: true}); removeErr != nil {
			slog.WarnContext(ctx, "Failed to remove a project backup manifest volume", "volume", name, "error", removeErr)
		}
	}
	image, err := volumes.ResolveHelperImage(ctx, dockerClient, volumes.ToolsImage(s.deps.Settings.GetSettingsConfig().ToolsImageRegistry.Value))
	if err != nil {
		remove()
		return "", nil, err
	}
	holder, err := dockerClient.ContainerCreate(ctx, client.ContainerCreateOptions{
		Config:     &container.Config{Image: image, Cmd: []string{"true"}, NetworkDisabled: true, Labels: volumes.Labels()},
		HostConfig: &container.HostConfig{Mounts: []mount.Mount{{Type: mount.TypeVolume, Source: name, Target: "/meta"}}},
	})
	if err != nil {
		remove()
		return "", nil, fmt.Errorf("create project backup manifest holder: %w", err)
	}
	copyErr := docker.CopyToContainer(ctx, dockerClient, holder.ID, "/meta", files)
	_, removeHolderErr := dockerClient.ContainerRemove(context.WithoutCancel(ctx), holder.ID, volumes.RemoveOptions())
	if err = errors.Join(copyErr, removeHolderErr); err != nil {
		remove()
		return "", nil, fmt.Errorf("write project backup manifest: %w", err)
	}
	return name, remove, nil
}

func (s *Service) complete(ctx context.Context, entry *ProjectBackup, err error) error {
	// A run handed back stays running, so its retry resumes it.
	if errors.Is(err, actor.ErrJobRejected) {
		return err
	}
	entry.Status, entry.Error, entry.CompletedAt = types.VolumeBackupStatusSucceeded, "", new(time.Now())
	if err != nil {
		entry.Status, entry.Error = types.VolumeBackupStatusFailed, err.Error()
	}
	if saveErr := s.deps.DB.WithContext(context.WithoutCancel(ctx)).Save(entry).Error; saveErr != nil {
		return errors.Join(err, fmt.Errorf("failed to save project backup result: %w", saveErr))
	}
	if err == nil {
		err = jobcontext.Progress(ctx, types.TargetOutcome{ResourceType: targetType, ID: entry.ProjectID, Status: types.RunStatusSucceeded})
	}
	return err
}

// handedBack reports whether a failed attempt leaves its backup running for a retry to resume. A shutdown cancel and a
// rejected run that nobody cancelled are retried; a user cancel never is, so its backup is failed now, unless the
// project's containers are still stopped: the running backup keeps their restart recoverable.
func handedBack(ctx context.Context, err error) bool {
	if errors.Is(err, errContainersDown) {
		return true
	}
	if _, resumable := jobcontext.Run(ctx); resumable && errors.Is(context.Cause(ctx), context.Canceled) {
		return true
	}
	return ctx.Err() == nil && errors.Is(err, actor.ErrJobRejected)
}

// reject notes why a running backup's snapshot was rejected, leaving it running until the containers it stopped start.
func (s *Service) reject(ctx context.Context, backupID, message string) error {
	return s.deps.DB.WithContext(ctx).Model(&ProjectBackup{}).Where("id = ? AND status = ?", backupID, types.VolumeBackupStatusRunning).
		Update("error", message).Error
}

func (s *Service) fail(ctx context.Context, backupID, message string) error {
	return s.deps.DB.WithContext(ctx).Model(&ProjectBackup{}).Where("id = ? AND status = ?", backupID, types.VolumeBackupStatusRunning).
		Updates(map[string]any{"status": types.VolumeBackupStatusFailed, "error": message, "completed_at": time.Now()}).Error
}

func (s *Service) load(ctx context.Context, backupID string) (*ProjectBackup, error) {
	var entry ProjectBackup
	if err := s.deps.DB.WithContext(ctx).Where("id = ?", backupID).First(&entry).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, common.Classify(common.ErrNotFound, errors.New("project backup not found"))
		}
		return nil, err
	}
	return &entry, nil
}

// watch fails the row when the workflow ends without its task completing it.
func (s *Service) watch(ctx context.Context, backupID, instanceID string) {
	outcome, err := s.flow.Wait(ctx, s.workflow, instanceID)
	if err != nil || outcome.Status == types.RunStatusSucceeded {
		return
	}
	if failErr := s.fail(ctx, backupID, cmp.Or(outcome.Message, "Backup did not run")); failErr != nil {
		slog.WarnContext(ctx, "Failed to close a project backup whose workflow ended early", "backupId", backupID, "error", failErr)
	}
}

// ReconcileInterruptedBackups fails backups left running by the previous process and watches those saved workflows resume.
func (s *Service) ReconcileInterruptedBackups(ctx context.Context, protectedIDs ...string) error {
	query := s.deps.DB.WithContext(ctx).Model(&ProjectBackup{}).Where("status = ?", types.VolumeBackupStatusRunning)
	if len(protectedIDs) > 0 {
		query = query.Where("id NOT IN ?", protectedIDs)
	}
	if err := query.Updates(map[string]any{"status": types.VolumeBackupStatusFailed, "error": "Backup interrupted by Arcane restart"}).Error; err != nil {
		return err
	}
	standalone, err := s.flow.Standalone(ctx)
	if err != nil {
		return err
	}
	for _, run := range standalone {
		if backupID := backup.AcceptedBackupID(run); backupID != "" && run.JobID == s.workflow.Francis().Name() {
			go s.watch(context.WithoutCancel(ctx), backupID, run.ID)
		}
	}
	return nil
}

func (s *Service) List(ctx context.Context, projectID string, params pagination.Params) ([]types.ProjectBackup, pagination.Response, error) {
	query := s.deps.DB.WithContext(ctx).Model(&ProjectBackup{}).Where("project_id = ?", projectID).Order("created_at DESC")
	var entries []ProjectBackup
	page, err := pagination.PaginateAndSortDB(pagination.QueryParams{Params: params}, query, &entries)
	if err != nil {
		return nil, pagination.Response{}, err
	}
	var destinations map[string]types.S3Destination
	if slices.ContainsFunc(entries, func(entry ProjectBackup) bool { return entry.S3DestinationID != "" }) {
		destinations = s.deps.S3Destinations.DestinationsByID(ctx)
	}
	backups := make([]types.ProjectBackup, 0, len(entries))
	for i := range entries {
		backups = append(backups, entries[i].DTO(DTOOptions{S3DestinationName: destinations[entries[i].S3DestinationID].Name}))
	}
	return backups, page, nil
}

func (s *Service) Get(ctx context.Context, projectID, backupID string) (types.ProjectBackup, error) {
	entry, err := s.load(ctx, backupID)
	if err != nil {
		return types.ProjectBackup{}, err
	}
	if entry.ProjectID != projectID {
		return types.ProjectBackup{}, common.Classify(common.ErrNotFound, errors.New("project backup not found"))
	}
	return entry.DTO(DTOOptions{}), nil
}

// Delete forgets the backup's snapshots and removes its row; it shares the project's admission with running backups.
func (s *Service) Delete(ctx context.Context, projectID, backupID string) error {
	releaseBackups, err := s.HoldBackups(ctx, projectID)
	if err != nil {
		return err
	}
	defer releaseBackups(ctx)
	entry, err := s.load(ctx, backupID)
	if err != nil {
		return err
	}
	if entry.ProjectID != projectID {
		return common.Classify(common.ErrNotFound, errors.New("project backup not found"))
	}
	return s.removeAll(ctx, []*ProjectBackup{entry})
}

// HoldBackups takes the project's admission without waiting, so retention, the sweep and Delete leave its backups alone
// until the returned release runs; a busy project fails fast.
func (s *Service) HoldBackups(ctx context.Context, projectID string) (func(context.Context), error) {
	leases, err := s.admit(ctx, admissions(projectID, nil), "")
	if err != nil {
		return nil, err
	}
	return func(ctx context.Context) { release(ctx, leases) }, nil
}

// removeAll forgets the backups' snapshots with one ForgetSnapshots per repository, then deletes the rows whose
// snapshots are gone; a repository that fails keeps its rows. The caller holds each project's admission.
func (s *Service) removeAll(ctx context.Context, entries []*ProjectBackup) error {
	dockerClient, err := s.deps.Docker.GetClient(ctx)
	if err != nil {
		return err
	}
	var localIDs []string
	remoteGroups := make(map[string][]*ProjectBackup)
	for _, entry := range entries {
		if entry.LocalSnapshotID != "" {
			localIDs = append(localIDs, entry.LocalSnapshotID)
		}
		if entry.RemoteSnapshotID != "" {
			remoteGroups[entry.S3DestinationID] = append(remoteGroups[entry.S3DestinationID], entry)
		}
	}
	var removeErr error
	changed := make(map[string]bool)
	if len(localIDs) > 0 {
		repository, repoErr := s.deps.Repository.Local(ctx, dockerClient, backup.LocalRepositoryOptions{})
		if repoErr == nil {
			repoErr = s.forgetSnapshots(ctx, dockerClient, repository, localIDs)
		}
		if repoErr != nil {
			removeErr = fmt.Errorf("failed to delete the local project snapshots: %w", repoErr)
		} else {
			for _, entry := range entries {
				changed[entry.ID] = changed[entry.ID] || entry.LocalSnapshotID != ""
				entry.LocalSnapshotID = ""
			}
		}
	}
	instanceID := s.deps.Settings.GetSettingsConfig().InstanceID.Value
	for destinationID, group := range remoteGroups {
		repoErr := s.forgetRemote(ctx, dockerClient, destinationID, path.Join(backup.VolumeRoot, instanceID), group)
		if repoErr != nil {
			removeErr = errors.Join(removeErr, fmt.Errorf("failed to delete the S3 project snapshots: %w", repoErr))
			continue
		}
		for _, entry := range group {
			changed[entry.ID] = true
			entry.RemoteSnapshotID = ""
		}
	}
	var deletedIDs []string
	for _, entry := range entries {
		switch {
		case entry.LocalSnapshotID == "" && entry.RemoteSnapshotID == "":
			deletedIDs = append(deletedIDs, entry.ID)
		case changed[entry.ID]:
			entry.Destination = destination(entry.LocalSnapshotID != "", entry.RemoteSnapshotID != "")
			removeErr = errors.Join(removeErr, s.deps.DB.WithContext(ctx).Save(entry).Error)
		}
	}
	for batch := range slices.Chunk(deletedIDs, backup.DeletionBatchSize) {
		removeErr = errors.Join(removeErr, s.deps.DB.WithContext(ctx).Where("id IN ?", batch).Delete(&ProjectBackup{}).Error)
	}
	return removeErr
}

// forgetRemote forgets the group's snapshots in one S3 repository; a missing repository has nothing left to forget.
func (s *Service) forgetRemote(ctx context.Context, dockerClient *client.Client, destinationID, root string, group []*ProjectBackup) error {
	observation, err := backup.CheckRemoteRepository(ctx, s.deps.S3Destinations, destinationID, root)
	if err != nil {
		return err
	}
	if !observation.Available {
		slog.WarnContext(ctx, "S3 repository is missing; dropping the project backups' remote snapshot references", "destinationId", destinationID, "backups", len(group))
		return nil
	}
	repository, err := s.deps.Repository.Remote(ctx, destinationID, "")
	if err != nil {
		return err
	}
	snapshotIDs := make([]string, len(group))
	for index, entry := range group {
		snapshotIDs[index] = entry.RemoteSnapshotID
	}
	return s.forgetSnapshots(ctx, dockerClient, repository, snapshotIDs)
}

func (s *Service) forgetSnapshots(ctx context.Context, dockerClient *client.Client, repository backup.Repository, snapshotIDs []string) error {
	password, err := s.deps.Repository.Password(ctx, dockerClient, repository)
	if err != nil {
		return err
	}
	return s.deps.ForgetSnapshots(ctx, dockerClient, repository, password, snapshotIDs)
}

// backupsByID loads the backups in batches that fit the database's parameter limit.
func (s *Service) backupsByID(ctx context.Context, ids []string) ([]*ProjectBackup, error) {
	entries := make([]*ProjectBackup, 0, len(ids))
	for batch := range slices.Chunk(ids, backup.DeletionBatchSize) {
		var loaded []*ProjectBackup
		if err := s.deps.DB.WithContext(ctx).Where("id IN ?", batch).Find(&loaded).Error; err != nil {
			return nil, err
		}
		entries = append(entries, loaded...)
	}
	return entries, nil
}

func (s *Service) policies(ctx context.Context, projectID string) ([]ProjectBackupPolicy, error) {
	var policies []ProjectBackupPolicy
	if err := s.deps.DB.WithContext(ctx).Where("project_id = ?", projectID).Order("created_at ASC").Find(&policies).Error; err != nil {
		return nil, fmt.Errorf("failed to load project backup policies: %w", err)
	}
	return policies, nil
}

// Policies lists the project's backup policies with the latest backup each one took.
func (s *Service) Policies(ctx context.Context, projectID string) (types.ProjectBackupPolicyCollection, error) {
	policies, err := s.policies(ctx, projectID)
	if err != nil {
		return types.ProjectBackupPolicyCollection{}, err
	}
	destinations := s.deps.S3Destinations.DestinationsByID(ctx)
	dtos := make([]types.ProjectBackupPolicy, 0, len(policies))
	for i := range policies {
		var latest []ProjectBackup
		if latestErr := s.deps.DB.WithContext(ctx).Where("policy_id = ?", policies[i].ID).Order("created_at DESC").Limit(1).Find(&latest).Error; latestErr != nil {
			return types.ProjectBackupPolicyCollection{}, fmt.Errorf("failed to load the latest project backup: %w", latestErr)
		}
		var lastRun *types.ProjectBackup
		if len(latest) > 0 {
			lastRun = new(latest[0].DTO(DTOOptions{}))
		}
		dtos = append(dtos, policies[i].DTO(lastRun, destinations[policies[i].S3DestinationID].Name))
	}
	return types.ProjectBackupPolicyCollection{Policies: dtos, S3Available: len(destinations) > 0}, nil
}

// UpdatePolicies replaces the project's backup policies, schedules the enabled ones and unschedules the rest.
func (s *Service) UpdatePolicies(ctx context.Context, projectID string, updates []types.UpdateProjectBackupPolicy) (types.ProjectBackupPolicyCollection, error) {
	// Two saves for the same project must not register jobs in a different order from their commits.
	defer s.policyLocks.Lock(projectID)()
	project, err := s.deps.Project(ctx, projectID, projectDetails)
	if err != nil {
		return types.ProjectBackupPolicyCollection{}, err
	}
	existing, err := s.policies(ctx, projectID)
	if err != nil {
		return types.ProjectBackupPolicyCollection{}, err
	}
	reconcile := backup.PolicyReconciliation[ProjectBackupPolicy, types.UpdateProjectBackupPolicy]{
		Domain:   "project",
		DB:       s.deps.DB,
		Existing: existing,
		ID:       func(policy *ProjectBackupPolicy) string { return policy.ID },
		UpdateID: func(update types.UpdateProjectBackupPolicy) string { return update.ID },
		New:      func() ProjectBackupPolicy { return ProjectBackupPolicy{ProjectID: projectID} },
		Build: func(ctx context.Context, policy *ProjectBackupPolicy, update types.UpdateProjectBackupPolicy) error {
			normalized, validateErr := backup.ValidatePolicyUpdate(ctx, "project", update.UpdateBackupPolicy, s.deps.S3Destinations)
			if validateErr != nil {
				return common.Classify(common.ErrValidation, validateErr)
			}
			volumes := slices.Clone(update.Volumes)
			slices.Sort(volumes)
			volumes = slices.Compact(volumes)
			// A new selection follows the rules of a manual backup; an unchanged one is not checked again, so a volume
			// that went stale does not block saving, and no selection keeps following the project's volumes.
			if volumes != nil && (policy.ID == "" || !slices.Equal(volumes, policy.Volumes)) {
				if _, inspectErr := s.inspectVolumes(ctx, project, volumes); inspectErr != nil {
					return inspectErr
				}
			}
			*policy = ProjectBackupPolicy{
				BaseModel:       policy.BaseModel,
				ProjectID:       projectID,
				Schedule:        normalized.Schedule,
				S3DestinationID: normalized.S3DestinationID,
				Volumes:         volumes,
				RetentionCount:  normalized.RetentionCount,
				Enabled:         normalized.Enabled,
				StopContainers:  *cmp.Or(update.StopContainers, new(true)),
				LocalEnabled:    normalized.LocalEnabled,
				S3Enabled:       normalized.S3Enabled,
			}
			return nil
		},
		// A deleted policy's job stays while a backup it started is unfinished, so a restart can still resume it.
		Unregister: func(ctx context.Context, policyID string) {
			s.reschedule(ctx, &ProjectBackupPolicy{BaseModel: database.BaseModel{ID: policyID}, ProjectID: projectID})
		},
		Reschedule: s.reschedule,
	}
	if runErr := reconcile.Run(ctx, updates); runErr != nil {
		return types.ProjectBackupPolicyCollection{}, runErr
	}
	return s.Policies(ctx, projectID)
}

// RegisterJobsOnStartup schedules every enabled project backup policy and global project backup policy, and keeps the
// job of a disabled or deleted per-project policy whose backup was interrupted, so the run can resume and start the
// containers it stopped.
func (s *Service) RegisterJobsOnStartup(ctx context.Context) {
	db := s.deps.DB.WithContext(ctx)
	unfinished := db.Model(&ProjectBackup{}).Select("policy_id").
		Where("status = ? AND trigger = ? AND policy_id <> ''", types.VolumeBackupStatusRunning, types.VolumeBackupTriggerScheduled)
	var policies []ProjectBackupPolicy
	if err := db.Where("enabled = ? OR id IN (?)", true, unfinished).Find(&policies).Error; err != nil {
		slog.ErrorContext(ctx, "Failed to load scheduled project backups", "error", err)
	}
	var orphaned []ProjectBackup
	if err := db.Where("status = ? AND trigger = ? AND policy_id <> '' AND policy_id NOT LIKE ? AND policy_id NOT IN (?)",
		types.VolumeBackupStatusRunning, types.VolumeBackupTriggerScheduled, types.GlobalProjectPolicyPrefix+"%",
		db.Model(&ProjectBackupPolicy{}).Select("id"),
	).Find(&orphaned).Error; err != nil {
		slog.ErrorContext(ctx, "Failed to load interrupted project backups of deleted policies", "error", err)
	}
	for _, entry := range orphaned {
		policies = append(policies, ProjectBackupPolicy{BaseModel: database.BaseModel{ID: entry.PolicyID}, ProjectID: entry.ProjectID})
	}
	for i := range policies {
		s.reschedule(ctx, &policies[i])
	}
	globals, err := s.globalPolicies()
	if err != nil {
		slog.ErrorContext(ctx, "Failed to load global project backup policies", "error", err)
	}
	// A deleted global policy's interrupted run resumes through its job as well.
	var series []string
	if seriesErr := db.Model(&ProjectBackup{}).Distinct("policy_id").
		Where("status = ? AND trigger = ? AND policy_id LIKE ?", types.VolumeBackupStatusRunning, types.VolumeBackupTriggerScheduled, types.GlobalProjectPolicyPrefix+"%").
		Pluck("policy_id", &series).Error; seriesErr != nil {
		slog.ErrorContext(ctx, "Failed to load interrupted global project backups", "error", seriesErr)
	}
	for _, seriesID := range series {
		policyID, _, _ := strings.Cut(strings.TrimPrefix(seriesID, types.GlobalProjectPolicyPrefix), ":")
		if !slices.ContainsFunc(globals, func(policy types.GlobalProjectBackupPolicy) bool { return policy.ID == policyID }) {
			globals = append(globals, types.GlobalProjectBackupPolicy{ID: policyID})
		}
	}
	for i := range globals {
		s.rescheduleGlobal(ctx, &globals[i])
	}
	slog.InfoContext(ctx, "Registered scheduled project backup jobs", "count", len(policies), "global", len(globals))
}

// RemovePolicies unschedules and deletes a deleted project's backup policies, which leaves their backups unmanaged.
func (s *Service) RemovePolicies(ctx context.Context, projectID string) {
	defer s.policyLocks.Lock(projectID)()
	policies, err := s.policies(ctx, projectID)
	if err != nil {
		slog.WarnContext(ctx, "Failed to load the backup policies of a deleted project", "projectId", projectID, "error", err)
		return
	}
	// Deleted first, so a restart in between never schedules them again.
	if deleteErr := s.deps.DB.WithContext(ctx).Where("project_id = ?", projectID).Delete(&ProjectBackupPolicy{}).Error; deleteErr != nil {
		slog.WarnContext(ctx, "Failed to delete the backup policies of a deleted project", "projectId", projectID, "error", deleteErr)
		return
	}
	for i := range policies {
		s.jobs.Unregister(ctx, policies[i].ID)
	}
}

// unfinished reports whether a scheduled backup of the job's policy is still running, matching a global policy's
// per-project series too; a failed check counts as running, so its job is kept.
func (s *Service) unfinished(ctx context.Context, jobID string) bool {
	var count int64
	err := s.deps.DB.WithContext(ctx).Model(&ProjectBackup{}).
		Where("(policy_id = ? OR policy_id LIKE ?) AND status = ?", jobID, jobID+":%", types.VolumeBackupStatusRunning).Count(&count).Error
	return err != nil || count > 0
}

func (s *Service) reschedule(ctx context.Context, policy *ProjectBackupPolicy) {
	if !policy.Enabled && !s.unfinished(ctx, policy.ID) {
		s.jobs.Unregister(ctx, policy.ID)
		return
	}
	policyID, projectID := policy.ID, policy.ProjectID
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
		ScheduleFn: func(ctx context.Context) string {
			var current ProjectBackupPolicy
			if loadErr := s.deps.DB.WithContext(ctx).Where("id = ?", policyID).First(&current).Error; loadErr != nil {
				return defaultSchedule
			}
			return current.Schedule
		},
		// A disabled or deleted policy's job only finishes an interrupted backup; a run it starts finds the policy off
		// and cancels.
		ShouldRunFn: func(ctx context.Context) bool {
			var current ProjectBackupPolicy
			loadErr := s.deps.DB.WithContext(ctx).Where("id = ?", policyID).First(&current).Error
			return (loadErr == nil && current.Enabled) || s.unfinished(ctx, policyID)
		},
		// A run whose workflow cannot be resumed still restarts the containers it stopped and finishes its backup. Nothing
		// retries a fallback, so it waits for a busy admission instead of handing the run back. A failed container restart
		// is rejected too but leaves the admissions free, so attempts back off until the containers start.
		FallbackFn: func(ctx context.Context, previous types.SchedulerRun) (types.SchedulerOutcome, error) {
			outcome, resumed, resumeErr := s.resumeScheduled(ctx, previous)
			for attempt := 0; errors.Is(resumeErr, actor.ErrJobRejected); attempt++ {
				slog.WarnContext(ctx, "Retrying an interrupted scheduled project backup", "policyId", policyID, "error", resumeErr)
				waitErr := fallbackBackoff.Sleep(ctx, attempt)
				if waitErr == nil {
					waitErr = s.awaitCheckpointAdmissions(ctx, previous)
				}
				if waitErr != nil {
					return types.SchedulerOutcome{}, waitErr
				}
				outcome, resumed, resumeErr = s.resumeScheduled(ctx, previous)
			}
			if resumed || resumeErr != nil {
				return outcome, resumeErr
			}
			return types.SchedulerOutcome{Status: types.RunStatusFailed, Message: "Scheduled backup was interrupted; it runs again at its next scheduled time"}, nil
		},
	})
}

// runScheduled waits for the project's admission and its volumes' leases instead of failing, then backs the project up
// with its policy as it stands once admitted.
func (s *Service) runScheduled(ctx context.Context, t flow.Task) (any, error) {
	var input scheduledRun
	if decodeErr := t.Payload(&input); decodeErr != nil {
		return nil, decodeErr
	}
	previous, _ := jobcontext.Run(ctx)
	if outcome, resumed, resumeErr := s.resumeScheduled(ctx, previous); resumed || resumeErr != nil {
		return outcome, resumeErr
	}
	var policy ProjectBackupPolicy
	if loadErr := s.deps.DB.WithContext(ctx).Where("id = ? AND enabled = ?", input.PolicyID, true).First(&policy).Error; loadErr != nil {
		if errors.Is(loadErr, gorm.ErrRecordNotFound) {
			return types.SchedulerOutcome{Status: types.RunStatusCanceled, Message: "Backup policy disabled or deleted"}, nil
		}
		return nil, loadErr
	}
	admitted, err := s.admitScheduled(ctx, policy.ProjectID, policy.ID, func(ctx context.Context) (types.CreateProjectBackupRequest, bool, error) {
		return s.scheduledPolicy(ctx, policy.ID)
	})
	switch {
	case errors.Is(err, errPolicyGone):
		return types.SchedulerOutcome{Status: types.RunStatusCanceled, Message: "Backup policy disabled or deleted"}, nil
	case errors.Is(err, errRemoteOnlyDisabled):
		return types.SchedulerOutcome{Status: types.RunStatusNeedsAttention, Message: backup.RemoteDisabledMessage}, nil
	case err != nil:
		return nil, err
	}
	backupErr := s.backUpScheduled(ctx, admitted)
	release(context.WithoutCancel(ctx), admitted.leases)
	if backupErr != nil {
		slog.ErrorContext(ctx, "Scheduled project backup failed", "projectId", policy.ProjectID, "policyId", policy.ID, "error", backupErr)
	}
	// A failed run can leave a local snapshot too, so it counts against retention as well.
	s.retain(ctx, policy.ProjectID, policy.ID)
	if backupErr != nil {
		return nil, backupErr
	}
	if admitted.remoteDisabled {
		return types.SchedulerOutcome{Status: types.RunStatusPartial, Message: backup.RemoteDisabledMessage}, nil
	}
	return types.SchedulerOutcome{Status: types.RunStatusSucceeded, Message: "Scheduled project backup created successfully"}, nil
}

// checkpointedPlan decodes the plan an interrupted scheduled attempt checkpointed; it reports false when there is none.
func checkpointedPlan(previous types.SchedulerRun) (plan, bool, error) {
	index := slices.IndexFunc(previous.Outcome.Targets, func(target types.TargetOutcome) bool {
		return target.ResourceType == targetType && len(target.RecoveryData) > 0
	})
	if index < 0 {
		return plan{}, false, nil
	}
	var recorded plan
	err := json.Unmarshal(previous.Outcome.Targets[index].RecoveryData, &recorded)
	return recorded, true, err
}

// resumable reports whether the target is a backup an interrupted attempt checkpointed and left running.
func resumable(target types.TargetOutcome) bool {
	return target.ResourceType == targetType && target.Status == types.RunStatusRunning && len(target.RecoveryData) > 0
}

// resumeScheduled finishes the backup an interrupted attempt checkpointed; it reports false when there is none, or when
// that backup already failed and a new one should run.
func (s *Service) resumeScheduled(ctx context.Context, previous types.SchedulerRun) (types.SchedulerOutcome, bool, error) {
	recorded, found, err := checkpointedPlan(previous)
	if !found {
		return types.SchedulerOutcome{}, false, nil
	}
	if err != nil {
		return types.SchedulerOutcome{}, true, err
	}
	err = s.resume(ctx, previous, nil, recorded)
	if errors.Is(err, backup.ErrBackupSettled) {
		return types.SchedulerOutcome{}, false, nil
	}
	// A resumed run that fails keeps its snapshots, so it counts against retention as well.
	s.retain(ctx, recorded.ProjectID, recorded.PolicyID)
	if err != nil {
		return types.SchedulerOutcome{}, true, err
	}
	return types.SchedulerOutcome{Status: types.RunStatusSucceeded, Message: "Scheduled project backup created successfully"}, true, nil
}

// awaitCheckpointAdmissions waits until every admission the still-running checkpointed backups need is free, then lets
// them go, so a resume can take them.
func (s *Service) awaitCheckpointAdmissions(ctx context.Context, previous types.SchedulerRun) error {
	for _, target := range previous.Outcome.Targets {
		if !resumable(target) {
			continue
		}
		var recorded plan
		if err := json.Unmarshal(target.RecoveryData, &recorded); err != nil {
			return err
		}
		for _, key := range admissions(recorded.ProjectID, recorded.volumeNames()) {
			lease, err := s.deps.Engine.AwaitRun(ctx, key.Scope, key.ID)
			if err != nil {
				return err
			}
			lease.Release(context.WithoutCancel(ctx))
		}
	}
	return nil
}

// admitScheduled waits for the project's admission, then for the leases of the volumes its policy captures. Everything
// the backup relies on is read again once they are all held: the policy and its S3 repository through read, and the
// plan. Volumes that changed while it waited are leased in turn and checked again. The backup is recorded under policyID.
func (s *Service) admitScheduled(
	ctx context.Context, projectID, policyID string, read func(context.Context) (types.CreateProjectBackupRequest, bool, error),
) (admittedRun, error) {
	projectLease, err := s.deps.Engine.AwaitRun(ctx, admissionScope, projectID)
	if err != nil {
		return admittedRun{}, err
	}
	leases := []*runs.Lease{projectLease}
	var leased []string
	remoteDisabled := false
	for {
		request, disabled, policyErr := read(ctx)
		remoteDisabled = remoteDisabled || disabled
		var project types.Project
		var p plan
		if policyErr == nil {
			project, policyErr = s.deps.Project(ctx, projectID, projectDetails)
		}
		if policyErr == nil {
			p, policyErr = s.resolvePlan(ctx, project, request)
		}
		if policyErr != nil {
			release(context.WithoutCancel(ctx), leases)
			return admittedRun{}, policyErr
		}
		names := p.volumeNames()
		if slices.Equal(names, leased) {
			return admittedRun{policyID: policyID, project: project, plan: p, leases: leases, remoteDisabled: remoteDisabled}, nil
		}
		// Leases are always taken in name order, so two waiting backups never hold one each of the other's.
		release(context.WithoutCancel(ctx), leases[1:])
		leases, leased = leases[:1], names
		for _, name := range names {
			lease, leaseErr := s.deps.Engine.AwaitRun(ctx, backup.VolumeAdmissionScope, name)
			if leaseErr != nil {
				release(context.WithoutCancel(ctx), leases)
				return admittedRun{}, leaseErr
			}
			leases = append(leases, lease)
		}
	}
}

// scheduledPolicy reads the enabled policy under the project's admission and checks its S3 repository. A missing
// repository disables S3, or the policy when S3 is its only destination, so the backup never recreates it.
func (s *Service) scheduledPolicy(ctx context.Context, policyID string) (types.CreateProjectBackupRequest, bool, error) {
	var policy ProjectBackupPolicy
	if err := s.deps.DB.WithContext(ctx).Where("id = ? AND enabled = ?", policyID, true).First(&policy).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return types.CreateProjectBackupRequest{}, false, errPolicyGone
		}
		return types.CreateProjectBackupRequest{}, false, err
	}
	var remoteErr error
	if policy.S3Enabled {
		remoteErr = backup.CheckScheduledRemote(ctx, s.deps.DB, s.deps.S3Destinations, backup.ScheduledRemoteRequest{
			DestinationID: policy.S3DestinationID,
			Root:          path.Join(backup.VolumeRoot, s.deps.Settings.GetSettingsConfig().InstanceID.Value),
			InstanceID:    s.deps.Settings.GetSettingsConfig().InstanceID.Value,
			Tables:        backup.VolumeRepositoryTables,
		})
	}
	disabled, err := backup.DisableMissingRemote(remoteErr, policy.LocalEnabled, &policy.S3Enabled, &policy.Enabled, func(field string) (bool, error) {
		return backup.DisableStoredRemote(ctx, s.deps.DB, backup.DisableRemoteRequest{
			Model:         &ProjectBackupPolicy{},
			ID:            policy.ID,
			DestinationID: policy.S3DestinationID,
			LocalEnabled:  policy.LocalEnabled,
			Field:         field,
		})
	})
	// The repository is gone but the guarded save matched nothing: S3 was turned off or moved, or the policy changed,
	// while it was checked, so the policy as it is now is checked instead.
	if err == nil && !disabled && errors.Is(remoteErr, backup.ErrRemoteRepositoryMissing) {
		return s.scheduledPolicy(ctx, policyID)
	}
	if err != nil {
		return types.CreateProjectBackupRequest{}, false, err
	}
	if disabled {
		// Rescheduled before the backup runs, so a later edit of the policy is not undone afterwards.
		s.reschedule(ctx, &policy)
	}
	if disabled && !policy.LocalEnabled {
		return types.CreateProjectBackupRequest{}, true, errRemoteOnlyDisabled
	}
	return types.CreateProjectBackupRequest{
		StopContainers:  new(policy.StopContainers),
		Destination:     destination(policy.LocalEnabled, policy.S3Enabled),
		S3DestinationID: policy.S3DestinationID,
		Volumes:         policy.Volumes,
	}, disabled, nil
}

// backUpScheduled records the admitted run's backup and its checkpoint, then takes it as a manual backup does.
func (s *Service) backUpScheduled(ctx context.Context, admitted admittedRun) (err error) {
	p := admitted.plan
	entry := &ProjectBackup{
		BaseModel:       database.BaseModel{ID: uuid.New().String(), CreatedAt: time.Now()},
		ProjectID:       admitted.project.ID,
		ProjectName:     admitted.project.Name,
		ComposeName:     admitted.project.ComposeName,
		Status:          types.VolumeBackupStatusRunning,
		Trigger:         types.VolumeBackupTriggerScheduled,
		Destination:     destination(p.LocalEnabled, p.S3Enabled),
		S3DestinationID: p.S3DestinationID,
		PolicyID:        admitted.policyID,
		Volumes:         backupVolumes(p.Volumes),
	}
	if createErr := s.deps.DB.WithContext(ctx).Create(entry).Error; createErr != nil {
		return fmt.Errorf("failed to save project backup: %w", createErr)
	}
	defer func() {
		// A run handed back stays running, so its retry resumes it from the checkpoint; a user cancel fails it.
		if err != nil && !handedBack(ctx, err) {
			err = errors.Join(err, s.fail(context.WithoutCancel(ctx), entry.ID, err.Error()))
		}
	}()
	p.BackupID, p.PolicyID = entry.ID, entry.PolicyID
	if recordErr := s.record(ctx, p); recordErr != nil {
		return recordErr
	}
	err = s.execute(ctx, entry, p)
	if ctx.Err() != nil {
		return ctx.Err()
	}
	return s.complete(ctx, entry, err)
}

// retain deletes the policy's backups beyond its newest RetentionCount under the project's admission. A busy project
// keeps them until the policy's next backup, and a failure is logged without failing the backup.
func (s *Service) retain(ctx context.Context, projectID, policyID string) {
	lease, admitted, err := s.deps.Engine.TryAcquireRun(ctx, admissionScope, projectID)
	if err != nil || !admitted {
		slog.InfoContext(ctx, "Skipping project backup retention until the policy's next backup", "projectId", projectID, "policyId", policyID, "error", err)
		return
	}
	defer lease.Release(context.WithoutCancel(ctx))
	// A global policy keeps its RetentionCount per project, in the project's series.
	var keep int
	if series, global := strings.CutPrefix(policyID, types.GlobalProjectPolicyPrefix); global {
		owner, _, _ := strings.Cut(series, ":")
		policy, loadErr := s.globalPolicy(owner)
		if loadErr != nil || policy == nil {
			return
		}
		keep = policy.RetentionCount
	} else {
		var policy ProjectBackupPolicy
		if loadErr := s.deps.DB.WithContext(ctx).Where("id = ?", policyID).First(&policy).Error; loadErr != nil {
			return
		}
		keep = policy.RetentionCount
	}
	if keep <= 0 {
		return
	}
	expired, err := backup.ExpiredRunIDs(ctx, s.deps.DB, backupsTable, policyID, keep)
	if err == nil {
		var entries []*ProjectBackup
		if entries, err = s.backupsByID(ctx, expired); err == nil {
			err = s.removeAll(ctx, entries)
		}
	}
	if err != nil {
		slog.ErrorContext(ctx, "Project backup retention failed", "projectId", projectID, "policyId", policyID, "error", err)
	}
}

// PruneUnmanagedBackups deletes safety backups and the backups of deleted policies once they have been unmanaged for
// unmanagedBackupRetentionDays; a busy project's backups wait for the next run.
func (s *Service) PruneUnmanagedBackups(ctx context.Context) error {
	days := s.deps.Settings.GetIntSetting(ctx, "unmanagedBackupRetentionDays")
	if days <= 0 {
		return nil
	}
	now, retention := time.Now(), time.Duration(days)*24*time.Hour
	expired, err := s.expiredUnmanaged(ctx, types.UnmanagedBackupSweep{Now: now, Retention: retention})
	if err != nil || len(expired) == 0 {
		return err
	}
	var entries []ProjectBackup
	if loadErr := s.deps.DB.WithContext(ctx).Select("id", "project_id").Where("id IN ?", expired).Find(&entries).Error; loadErr != nil {
		return loadErr
	}
	byProject := make(map[string][]string)
	for _, entry := range entries {
		byProject[entry.ProjectID] = append(byProject[entry.ProjectID], entry.ID)
	}
	return s.pruneUnmanaged(ctx, byProject, types.UnmanagedBackupSweep{Now: now, Retention: retention})
}

// pruneUnmanaged holds every candidate project's admission, checks the candidates again, and deletes the expired ones of
// the projects it got in one pass, so each repository is pruned once; a busy project's backups wait for the next run.
func (s *Service) pruneUnmanaged(ctx context.Context, candidates map[string][]string, sweep types.UnmanagedBackupSweep) error {
	var pruneErr error
	var leases []*runs.Lease
	var within []string
	defer func() { release(context.WithoutCancel(ctx), leases) }()
	for projectID, ids := range candidates {
		lease, admitted, err := s.deps.Engine.TryAcquireRun(ctx, admissionScope, projectID)
		if err != nil {
			pruneErr = errors.Join(pruneErr, err)
			continue
		}
		if !admitted {
			slog.InfoContext(ctx, "Skipping unmanaged backup deletion for a busy project until the next run", "projectId", projectID)
			continue
		}
		leases = append(leases, lease)
		within = append(within, ids...)
	}
	if len(within) == 0 {
		return pruneErr
	}
	ids, err := s.expiredUnmanaged(ctx, types.UnmanagedBackupSweep{Now: sweep.Now, Within: within, Retention: sweep.Retention})
	if err != nil {
		return errors.Join(pruneErr, err)
	}
	entries, err := s.backupsByID(ctx, ids)
	if err != nil {
		return errors.Join(pruneErr, err)
	}
	return errors.Join(pruneErr, s.removeAll(ctx, entries))
}

// expiredUnmanaged marks scheduled backups whose policy no longer exists as unmanaged and returns those, and safety
// backups, that expired; manual backups and backups of existing policies are never unmanaged. A global policy manages
// a project's series while it exists and still selects that project.
func (s *Service) expiredUnmanaged(ctx context.Context, sweep types.UnmanagedBackupSweep) ([]string, error) {
	var candidates []ProjectBackup
	if err := s.deps.DB.WithContext(ctx).Model(&ProjectBackup{}).Select("id", "policy_id").
		Where("status <> ? AND trigger = ? AND policy_id <> ''", types.VolumeBackupStatusRunning, types.VolumeBackupTriggerScheduled).
		Where("policy_id NOT IN (?)", s.deps.DB.Model(&ProjectBackupPolicy{}).Select("id")).
		Find(&candidates).Error; err != nil {
		return nil, fmt.Errorf("failed to load unmanaged project backups: %w", err)
	}
	globals, err := s.globalPolicies()
	if err != nil {
		return nil, err
	}
	references, err := s.deps.ListProjects(ctx)
	if err != nil {
		return nil, err
	}
	var managed []string
	for _, policy := range globals {
		for _, project := range selectProjects(policy, references) {
			managed = append(managed, types.GlobalProjectPolicyPrefix+policy.ID+":"+project.ID)
		}
	}
	var orphaned []string
	for _, candidate := range candidates {
		if !slices.Contains(managed, candidate.PolicyID) {
			orphaned = append(orphaned, candidate.ID)
		}
	}
	return backup.ExpiredUnmanagedRunIDs(ctx, s.deps.DB, backupsTable, orphaned, sweep)
}

// globalPolicies reads the global project backup policies from their setting.
func (s *Service) globalPolicies() ([]types.GlobalProjectBackupPolicy, error) {
	var collection types.GlobalProjectBackupPolicyCollection
	if err := json.Unmarshal([]byte(cmp.Or(strings.TrimSpace(s.deps.Settings.GetSettingsConfig().ProjectBackupConfig.Value), "{}")), &collection); err != nil {
		return nil, fmt.Errorf("decode global project backup policies: %w", err)
	}
	return collection.Policies, nil
}

// globalPolicy returns the global policy, or nil when it does not exist.
func (s *Service) globalPolicy(policyID string) (*types.GlobalProjectBackupPolicy, error) {
	policies, err := s.globalPolicies()
	if err != nil {
		return nil, err
	}
	index := slices.IndexFunc(policies, func(policy types.GlobalProjectBackupPolicy) bool { return policy.ID == policyID })
	if index < 0 {
		return nil, nil
	}
	return &policies[index], nil
}

func (s *Service) saveGlobalPolicies(ctx context.Context, policies []types.GlobalProjectBackupPolicy) error {
	encoded, err := json.Marshal(types.GlobalProjectBackupPolicyCollection{Policies: policies})
	if err != nil {
		return fmt.Errorf("encode global project backup policies: %w", err)
	}
	return s.deps.Settings.UpdateSetting(ctx, globalConfigKey, string(encoded))
}

// selectProjects returns the projects the global policy selects; listed IDs of deleted projects match nothing.
func selectProjects(policy types.GlobalProjectBackupPolicy, references []types.ProjectReference) []types.ProjectReference {
	selected := make([]types.ProjectReference, 0, len(references))
	for _, project := range references {
		if policy.SelectionMode.Selects(slices.Contains(policy.ProjectIDs, project.ID)) {
			selected = append(selected, project)
		}
	}
	return selected
}

// GlobalPolicies lists the global project backup policies with the latest backup each one took.
func (s *Service) GlobalPolicies(ctx context.Context) (types.GlobalProjectBackupPolicyCollection, error) {
	policies, err := s.globalPolicies()
	if err != nil {
		return types.GlobalProjectBackupPolicyCollection{}, err
	}
	destinations := s.deps.S3Destinations.DestinationsByID(ctx)
	dtos := make([]types.GlobalProjectBackupPolicy, 0, len(policies))
	for _, policy := range policies {
		var latest []ProjectBackup
		if latestErr := s.deps.DB.WithContext(ctx).Where("policy_id LIKE ?", types.GlobalProjectPolicyPrefix+policy.ID+":%").
			Order("created_at DESC").Limit(1).Find(&latest).Error; latestErr != nil {
			return types.GlobalProjectBackupPolicyCollection{}, fmt.Errorf("failed to load the latest project backup: %w", latestErr)
		}
		var lastRun *types.ProjectBackup
		if len(latest) > 0 {
			lastRun = new(latest[0].DTO(DTOOptions{}))
		}
		dtos = append(dtos, types.GlobalProjectBackupPolicy{ //exhaustruct:enforce
			LastRun:           lastRun,
			ID:                policy.ID,
			Schedule:          policy.Schedule,
			S3DestinationID:   policy.S3DestinationID,
			S3DestinationName: destinations[policy.S3DestinationID].Name,
			SelectionMode:     policy.SelectionMode,
			ProjectIDs:        policy.ProjectIDs,
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
	s.globalConfig.Lock()
	defer s.globalConfig.Unlock()
	existing, err := s.globalPolicies()
	if err != nil {
		return types.GlobalProjectBackupPolicyCollection{}, err
	}
	reconcile := backup.PolicyReconciliation[types.GlobalProjectBackupPolicy, types.UpdateGlobalProjectBackupPolicy]{
		Domain:   "global project",
		Existing: existing,
		ID:       func(policy *types.GlobalProjectBackupPolicy) string { return policy.ID },
		UpdateID: func(update types.UpdateGlobalProjectBackupPolicy) string { return update.ID },
		New: func() types.GlobalProjectBackupPolicy {
			return types.GlobalProjectBackupPolicy{ID: uuid.New().String()}
		},
		Build: func(ctx context.Context, policy *types.GlobalProjectBackupPolicy, update types.UpdateGlobalProjectBackupPolicy) error {
			if !update.SelectionMode.Valid() {
				return common.Classify(common.ErrValidation, errors.New("selectionMode must be all, allowlist, or blocklist"))
			}
			normalized, validateErr := backup.ValidatePolicyUpdate(ctx, "project", update.UpdateBackupPolicy, s.deps.S3Destinations)
			if validateErr != nil {
				return common.Classify(common.ErrValidation, validateErr)
			}
			projectIDs := kit.Unique(kit.TrimNonEmpty(update.ProjectIDs))
			slices.Sort(projectIDs)
			*policy = types.GlobalProjectBackupPolicy{ //exhaustruct:enforce
				LastRun:           nil,
				ID:                policy.ID,
				Schedule:          normalized.Schedule,
				S3DestinationID:   normalized.S3DestinationID,
				S3DestinationName: "",
				SelectionMode:     update.SelectionMode,
				ProjectIDs:        kit.Ternary(update.SelectionMode == types.SystemVolumeSelectionAll, nil, projectIDs),
				RetentionCount:    normalized.RetentionCount,
				Enabled:           normalized.Enabled,
				StopContainers:    *cmp.Or(update.StopContainers, new(true)),
				LocalEnabled:      normalized.LocalEnabled,
				S3Enabled:         normalized.S3Enabled,
			}
			return nil
		},
		Persist: s.saveGlobalPolicies,
		// A deleted policy's job stays while a backup it started is unfinished, so a restart can still resume it.
		Unregister: func(ctx context.Context, policyID string) {
			s.rescheduleGlobal(ctx, &types.GlobalProjectBackupPolicy{ID: policyID})
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
	policies, err := s.globalPolicies()
	if err != nil {
		return nil, err
	}
	options := make([]types.GlobalProjectBackupOption, 0, len(references))
	for _, project := range references {
		options = append(options, types.GlobalProjectBackupOption{ID: project.ID, Name: project.Name, Available: true})
	}
	for _, policy := range policies {
		for _, projectID := range policy.ProjectIDs {
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
	policy, err := s.globalPolicy(policyID)
	switch {
	case err != nil:
		return types.BackupRunAccepted{}, err
	case policy == nil:
		return types.BackupRunAccepted{}, common.Classify(common.ErrNotFound, errors.New("global project backup policy not found"))
	case !policy.Enabled:
		return types.BackupRunAccepted{}, common.Classify(common.ErrValidation, errors.New("enable the backup policy before running it"))
	}
	lease, admitted, err := s.deps.Engine.TryAcquireRun(ctx, globalAdmissionScope, policyID)
	if err != nil {
		return types.BackupRunAccepted{}, err
	}
	if !admitted {
		return types.BackupRunAccepted{}, errGlobalRunning
	}
	input := globalRun{Requester: &requester, PolicyID: policyID, Claim: uuid.NewV7().String()}
	releaseHeld := s.deps.Engine.Hold(globalAdmissionScope, policyID, input.Claim, lease)
	activityID, err := s.flow.Submit(ctx, s.global, input, activity.StartRequest{
		StartedBy: &user,
		Metadata:  database.JSON{"action": "run_project_backups", "policyId": policyID},
	})
	if err != nil {
		releaseHeld(ctx)
		return types.BackupRunAccepted{}, err
	}
	// A run canceled before its task starts never takes the parked lease.
	go func() {
		_, _ = s.flow.Wait(context.WithoutCancel(ctx), s.global, activityID)
		releaseHeld(context.WithoutCancel(ctx))
	}()
	return types.BackupRunAccepted{ActivityID: activityID, Status: "running"}, nil
}

func (s *Service) rescheduleGlobal(ctx context.Context, policy *types.GlobalProjectBackupPolicy) {
	// Agents run no global policies yet.
	if s.deps.Config.AgentEnabled {
		return
	}
	jobID := types.GlobalProjectPolicyPrefix + policy.ID
	if !policy.Enabled && !s.unfinished(ctx, jobID) {
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
		ScheduleFn: func(context.Context) string {
			current, err := s.globalPolicy(policyID)
			if err != nil || current == nil {
				return defaultSchedule
			}
			return current.Schedule
		},
		// A disabled or deleted policy's job only finishes an interrupted run; a run it starts finds the policy off.
		ShouldRunFn: func(ctx context.Context) bool {
			current, err := s.globalPolicy(policyID)
			return (err == nil && current != nil && current.Enabled) || s.unfinished(ctx, jobID)
		},
		// A run whose workflow cannot be resumed still restarts the containers it stopped and backs up the rest of its
		// projects. Nothing retries a fallback, so it waits for a busy admission instead of handing the run back. A failed
		// container restart is rejected too but leaves the admissions free, so attempts back off until the containers start.
		FallbackFn: func(ctx context.Context, previous types.SchedulerRun) (types.SchedulerOutcome, error) {
			// Each attempt goes on from what the one before it saved, so containers a later backup stopped are restarted
			// rather than backed up again.
			latest := previous
			latest.Outcome.Targets = slices.Clone(previous.Outcome.Targets)
			attempt := func() (types.SchedulerOutcome, error) {
				tracked := jobcontext.WithExecution(ctx, latest, func(target types.TargetOutcome) error {
					latest.Outcome.Targets = jobcontext.Merge(latest.Outcome.Targets, target)
					return jobcontext.Progress(ctx, target)
				})
				return s.backUpGlobal(tracked, globalRun{PolicyID: policyID}, latest)
			}
			outcome, err := attempt()
			for retry := 0; errors.Is(err, actor.ErrJobRejected); retry++ {
				slog.WarnContext(ctx, "Retrying an interrupted global project backup", "policyId", policyID, "error", err)
				waitErr := fallbackBackoff.Sleep(ctx, retry)
				if waitErr == nil {
					waitErr = s.awaitCheckpointAdmissions(ctx, latest)
				}
				if waitErr != nil {
					return types.SchedulerOutcome{}, waitErr
				}
				outcome, err = attempt()
			}
			return outcome, err
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
// scheduled backup. A resumed run first finishes the backups the interrupted attempt checkpointed, restarting the
// containers they stopped, then backs up the rest of the projects that attempt planned.
func (s *Service) backUpGlobal(ctx context.Context, input globalRun, previous types.SchedulerRun) (types.SchedulerOutcome, error) {
	lease, admitted, err := s.deps.Engine.AcquireRun(ctx, globalAdmissionScope, input.PolicyID, input.Claim)
	switch {
	case err == nil && !admitted && input.Requester == nil:
		// A scheduled run or a fallback waits for the other run of the policy.
		lease, err = s.deps.Engine.AwaitRun(ctx, globalAdmissionScope, input.PolicyID)
	case err == nil && !admitted && len(previous.Outcome.Targets) > 0:
		// A resumed run may have left containers stopped, so it is handed back instead of failing.
		return types.SchedulerOutcome{}, fmt.Errorf("%w: %w", actor.ErrJobRejected, errGlobalRunning)
	case err == nil && !admitted:
		return types.SchedulerOutcome{}, errGlobalRunning
	}
	if err != nil {
		return types.SchedulerOutcome{}, err
	}
	defer lease.Release(context.WithoutCancel(ctx))
	planned, err := s.planGlobal(ctx, input.PolicyID, previous)
	if errors.Is(err, errPolicyGone) {
		return types.SchedulerOutcome{Status: types.RunStatusCanceled, Message: "Backup policy disabled or deleted"}, nil
	}
	if err != nil {
		return types.SchedulerOutcome{}, err
	}
	finished := make(map[string]types.TargetOutcome)
	for _, target := range previous.Outcome.Targets {
		if target.ResourceType != targetType {
			continue
		}
		if target.Status != types.RunStatusRunning {
			finished[target.ID] = target
			continue
		}
		if !resumable(target) {
			continue
		}
		var recorded plan
		if decodeErr := json.Unmarshal(target.RecoveryData, &recorded); decodeErr != nil {
			return types.SchedulerOutcome{}, decodeErr
		}
		resumeErr := s.resume(ctx, previous, input.Requester, recorded)
		if ctx.Err() != nil {
			return types.SchedulerOutcome{}, ctx.Err()
		}
		if errors.Is(resumeErr, actor.ErrJobRejected) {
			return types.SchedulerOutcome{}, resumeErr
		}
		// A resumed backup that fails keeps its snapshots, so it counts against retention as well.
		s.retain(ctx, recorded.ProjectID, recorded.PolicyID)
		var outcome types.TargetOutcome
		if resumeErr == nil {
			outcome = types.TargetOutcome{ResourceType: targetType, ID: recorded.ProjectID, Status: types.RunStatusSucceeded}
		} else {
			outcome = types.TargetOutcome{ResourceType: targetType, ID: recorded.ProjectID, Status: types.RunStatusFailed, Message: resumeErr.Error()}
		}
		if progressErr := jobcontext.Progress(ctx, outcome); progressErr != nil {
			return types.SchedulerOutcome{}, progressErr
		}
		finished[recorded.ProjectID] = outcome
	}
	remoteDisabled := false
	for _, projectID := range planned {
		if _, done := finished[projectID]; done {
			continue
		}
		if ctxErr := ctx.Err(); ctxErr != nil {
			return types.SchedulerOutcome{}, ctxErr
		}
		seriesID := types.GlobalProjectPolicyPrefix + input.PolicyID + ":" + projectID
		admitted, admitErr := s.admitScheduled(ctx, projectID, seriesID, func(ctx context.Context) (types.CreateProjectBackupRequest, bool, error) {
			return s.globalSettings(ctx, input, projectID)
		})
		switch {
		case errors.Is(admitErr, errPolicyGone):
			return types.SchedulerOutcome{Status: types.RunStatusCanceled, Message: "Backup policy disabled or deleted"}, nil
		case errors.Is(admitErr, errRemoteOnlyDisabled):
			return types.SchedulerOutcome{Status: types.RunStatusNeedsAttention, Message: backup.RemoteDisabledMessage}, nil
		case admitErr != nil && ctx.Err() != nil:
			return types.SchedulerOutcome{}, ctx.Err()
		}
		backupErr := admitErr
		if admitErr == nil {
			remoteDisabled = remoteDisabled || admitted.remoteDisabled
			backupErr = s.backUpScheduled(ctx, admitted)
			release(context.WithoutCancel(ctx), admitted.leases)
		}
		// Shutdown interrupted this project, or it could not be started again; its checkpoint resumes it first.
		if ctx.Err() != nil || errors.Is(backupErr, actor.ErrJobRejected) {
			return types.SchedulerOutcome{}, cmp.Or(ctx.Err(), backupErr)
		}
		var outcome types.TargetOutcome
		switch {
		case backupErr == nil:
			outcome = types.TargetOutcome{ResourceType: targetType, ID: projectID, Status: types.RunStatusSucceeded}
		case errors.Is(backupErr, errProjectSkipped):
			outcome = types.TargetOutcome{ResourceType: targetType, ID: projectID, Status: types.RunStatusSkipped, Message: backupErr.Error()}
		default:
			slog.ErrorContext(ctx, "Global project backup failed", "projectId", projectID, "policyId", input.PolicyID, "error", backupErr)
			outcome = types.TargetOutcome{ResourceType: targetType, ID: projectID, Status: types.RunStatusFailed, Message: backupErr.Error()}
		}
		// A failed backup can leave a local snapshot too, so it counts against retention as well.
		if outcome.Status != types.RunStatusSkipped {
			s.retain(ctx, projectID, seriesID)
		}
		if progressErr := jobcontext.Progress(ctx, outcome); progressErr != nil {
			return types.SchedulerOutcome{}, progressErr
		}
		finished[projectID] = outcome
	}
	return globalOutcome(finished, remoteDisabled), nil
}

// planGlobal returns the projects the run backs up: the ones an interrupted attempt planned, or else the ones the
// enabled policy selects now, saved before any is backed up so a resumed run keeps to them.
func (s *Service) planGlobal(ctx context.Context, policyID string, previous types.SchedulerRun) ([]string, error) {
	index := slices.IndexFunc(previous.Outcome.Targets, func(target types.TargetOutcome) bool {
		return target.ID == globalPlanTarget && len(target.RecoveryData) > 0
	})
	if index >= 0 {
		var recorded globalPlan
		err := json.Unmarshal(previous.Outcome.Targets[index].RecoveryData, &recorded)
		return recorded.ProjectIDs, err
	}
	policy, err := s.globalPolicy(policyID)
	if err != nil {
		return nil, err
	}
	if policy == nil || !policy.Enabled {
		return nil, errPolicyGone
	}
	references, err := s.deps.ListProjects(ctx)
	if err != nil {
		return nil, err
	}
	var planned []string
	for _, project := range selectProjects(*policy, references) {
		planned = append(planned, project.ID)
	}
	frozen, err := json.Marshal(globalPlan{ProjectIDs: planned})
	if err != nil {
		return nil, err
	}
	return planned, jobcontext.Progress(ctx, types.TargetOutcome{
		ResourceType: "backup_plan",
		ID:           globalPlanTarget,
		Status:       types.RunStatusSucceeded,
		RecoveryData: frozen,
	})
}

// globalSettings reads the global policy under the project's admission, checks the requester, that the policy still
// selects the project and that the project has no enabled policy of its own, then checks the S3 repository. A missing
// repository disables S3, or the policy when S3 is its only destination, so the backup never recreates it.
func (s *Service) globalSettings(ctx context.Context, input globalRun, projectID string) (types.CreateProjectBackupRequest, bool, error) {
	if input.Requester != nil {
		if err := s.deps.Engine.Authorize(ctx, *input.Requester); err != nil {
			return types.CreateProjectBackupRequest{}, false, err
		}
	}
	policy, err := s.globalPolicy(input.PolicyID)
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
	var own int64
	if countErr := s.deps.DB.WithContext(ctx).Model(&ProjectBackupPolicy{}).Where("project_id = ? AND enabled = ?", projectID, true).Count(&own).Error; countErr != nil {
		return types.CreateProjectBackupRequest{}, false, countErr
	}
	if own > 0 {
		return types.CreateProjectBackupRequest{}, false, errProjectSkipped
	}
	var remoteErr error
	if policy.S3Enabled {
		remoteErr = backup.CheckScheduledRemote(ctx, s.deps.DB, s.deps.S3Destinations, backup.ScheduledRemoteRequest{
			DestinationID: policy.S3DestinationID,
			Root:          path.Join(backup.VolumeRoot, s.deps.Settings.GetSettingsConfig().InstanceID.Value),
			InstanceID:    s.deps.Settings.GetSettingsConfig().InstanceID.Value,
			Tables:        backup.VolumeRepositoryTables,
		})
	}
	disabled, err := backup.DisableMissingRemote(remoteErr, policy.LocalEnabled, &policy.S3Enabled, &policy.Enabled, func(field string) (bool, error) {
		// Read again under the lock, so a concurrent edit or another run's disable is kept.
		s.globalConfig.Lock()
		defer s.globalConfig.Unlock()
		policies, loadErr := s.globalPolicies()
		if loadErr != nil {
			return false, loadErr
		}
		index := slices.IndexFunc(policies, func(current types.GlobalProjectBackupPolicy) bool {
			return current.ID == policy.ID && current.S3DestinationID == policy.S3DestinationID && current.S3Enabled && current.Enabled &&
				current.LocalEnabled == policy.LocalEnabled
		})
		if index < 0 {
			return false, nil
		}
		current := policies[index]
		policies[index] = types.GlobalProjectBackupPolicy{ //exhaustruct:enforce
			LastRun:           current.LastRun,
			ID:                current.ID,
			Schedule:          current.Schedule,
			S3DestinationID:   current.S3DestinationID,
			S3DestinationName: current.S3DestinationName,
			SelectionMode:     current.SelectionMode,
			ProjectIDs:        current.ProjectIDs,
			RetentionCount:    current.RetentionCount,
			Enabled:           current.Enabled && field != "enabled",
			StopContainers:    current.StopContainers,
			LocalEnabled:      current.LocalEnabled,
			S3Enabled:         current.S3Enabled && field != "s3_enabled",
		}
		return true, s.saveGlobalPolicies(ctx, policies)
	})
	// The repository is gone but the guarded save matched nothing: S3 was turned off or moved, or the policy changed,
	// while it was checked, so the policy as it is now is checked instead.
	if err == nil && !disabled && errors.Is(remoteErr, backup.ErrRemoteRepositoryMissing) {
		return s.globalSettings(ctx, input, projectID)
	}
	if err != nil {
		return types.CreateProjectBackupRequest{}, false, err
	}
	if disabled {
		// Rescheduled before the backup runs, so a later edit of the policy is not undone afterwards.
		s.rescheduleGlobal(ctx, policy)
	}
	if disabled && !policy.LocalEnabled {
		return types.CreateProjectBackupRequest{}, true, errRemoteOnlyDisabled
	}
	return types.CreateProjectBackupRequest{
		StopContainers:  new(policy.StopContainers),
		Destination:     destination(policy.LocalEnabled, policy.S3Enabled),
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

func backupVolumes(classified []types.ProjectVolume) []types.ProjectBackupVolume {
	captured := make([]types.ProjectBackupVolume, 0, len(classified))
	for _, volume := range classified {
		captured = append(captured, types.ProjectBackupVolume{Key: volume.Key, Name: volume.Name, Included: volume.Included})
	}
	return captured
}

func destination(local, remote bool) types.VolumeBackupDestination {
	switch {
	case local && remote:
		return types.VolumeBackupDestinationLocalS3
	case remote:
		return types.VolumeBackupDestinationS3
	default:
		return types.VolumeBackupDestinationLocal
	}
}

// StartRestore checks an in-place restore against the backup and the project, admits it, and hands it to the
// workflow; a busy project or volume fails fast. It returns the restore's activity ID.
func (s *Service) StartRestore(ctx context.Context, opts StartRestoreOptions) (string, error) {
	entry, err := s.restorable(ctx, opts.ProjectID, opts.BackupID)
	if err != nil {
		return "", err
	}
	project, err := s.deps.Project(ctx, opts.ProjectID, projectDetails)
	if err != nil {
		return "", err
	}
	if project.ComposeName == "" {
		return "", common.Classify(common.ErrValidation, fmt.Errorf("the compose files of project %s cannot be loaded; fix them before restoring it", project.Name))
	}
	var included []string
	for _, volume := range entry.Volumes {
		if volume.Included {
			included = append(included, volume.Key)
		}
	}
	keys, err := projects.SelectBackupVolumes(included, opts.Request.Volumes)
	if err != nil {
		return "", common.Classify(common.ErrValidation, err)
	}
	files := *cmp.Or(opts.Request.Files, new(true))
	if !files && len(keys) == 0 {
		return "", common.Classify(common.ErrValidation, errors.New("select the project files or at least one volume to restore"))
	}
	targets, err := restoreTargets(project, entry.Volumes, keys)
	if err != nil {
		return "", err
	}
	if checkErr := s.checkInPlace(ctx, project, files); checkErr != nil {
		return "", checkErr
	}
	active, _, err := s.containerUse(ctx, project.ComposeName)
	if err != nil {
		return "", err
	}
	stopped, running, err := s.projectContainers(ctx, project.ComposeName)
	if err != nil {
		return "", err
	}
	if unnamedErr := refuseUnnamedVolumes(project, slices.Concat(running, stopped)); unnamedErr != nil {
		return "", unnamedErr
	}
	p := restorePlan{
		Redeploy:  opts.Request.Redeploy,
		ID:        uuid.New().String(),
		BackupID:  entry.ID,
		ProjectID: project.ID,
		Volumes:   targets,
		Files:     files,
		MayStop:   len(active) > 0,
		Stopped:   len(stopped) > 0,
	}
	if permissionErr := middleware.RequirePermissions(ctx, opts.EnvironmentID, p.extraPermissions()); permissionErr != nil {
		return "", permissionErr
	}
	requester := backup.NewRequester(ctx, opts.User, opts.EnvironmentID, authz.PermProjectsRestore)
	if authorizeErr := s.deps.Engine.Authorize(ctx, requester); authorizeErr != nil {
		return "", authorizeErr
	}
	keysToAdmit := admissions(p.ProjectID, p.volumeNames())
	leases, err := s.admit(ctx, keysToAdmit, "")
	if err != nil {
		return "", err
	}
	releaseHeld := s.hold(keysToAdmit, p.ID, leases)
	activityID, err := s.flow.Submit(ctx, s.restores, restoreRun{Requester: requester, User: opts.User, Plan: p}, activity.StartRequest{
		EnvironmentID: opts.EnvironmentID, ResourceType: new("project"), ResourceID: new(project.ID), ResourceName: new(project.Name), StartedBy: &opts.User,
		Step: "Restoring backup", LatestMessage: "Restoring project backup",
		Metadata: database.JSON{"action": "restore_project_backup", "backupId": entry.ID},
	})
	if err != nil {
		releaseHeld(ctx)
		return "", err
	}
	// A restore canceled before its task runs never takes the parked leases.
	go func() {
		background := context.WithoutCancel(ctx)
		_, _ = s.flow.Wait(background, s.restores, activityID)
		releaseHeld(background)
	}()
	return activityID, nil
}

// restoreTargets pairs each key's volume in the snapshot with the project's current volume for the key, so a backup
// restores into the project's volumes even after a rename.
func restoreTargets(project types.Project, captured []types.ProjectBackupVolume, keys []string) ([]types.ProjectBackupRestoreVolume, error) {
	classified := projects.ClassifyProjectVolumes(project)
	targets := make([]types.ProjectBackupRestoreVolume, 0, len(keys))
	for _, key := range keys {
		source := slices.IndexFunc(captured, func(volume types.ProjectBackupVolume) bool { return volume.Key == key && volume.Included })
		current := slices.IndexFunc(classified, func(volume types.ProjectVolume) bool { return volume.Key == key })
		switch {
		case source < 0:
			return nil, common.Classify(common.ErrValidation, fmt.Errorf("the backup does not include the data of volume %s", key))
		case current < 0:
			return nil, common.Classify(common.ErrValidation, fmt.Errorf("project %s no longer has volume %s", project.Name, key))
		case !classified[current].Included:
			return nil, common.Classify(common.ErrValidation, fmt.Errorf("volume %s now keeps its data outside Docker (%s), so it cannot be restored", key, classified[current].Kind))
		}
		targets = append(targets, types.ProjectBackupRestoreVolume{Key: key, Snapshot: captured[source].Name, Name: classified[current].Name})
	}
	return targets, nil
}

// extraPermissions are the rights a restore needs besides projects:restore: stopping and deploying a running project,
// removing a stopped one's containers, or deploying one it was asked to redeploy.
func (p restorePlan) extraPermissions() []string {
	switch {
	case p.MayStop:
		return []string{authz.PermProjectsDown, authz.PermProjectsDeploy}
	case p.Stopped && kit.FromPtr(p.Redeploy):
		return []string{authz.PermProjectsDown, authz.PermProjectsDeploy}
	case p.Stopped:
		return []string{authz.PermProjectsDown}
	case kit.FromPtr(p.Redeploy):
		return []string{authz.PermProjectsDeploy}
	}
	return nil
}

func (p restorePlan) volumeNames() []string {
	names := make([]string, 0, len(p.Volumes))
	for _, volume := range p.Volumes {
		names = append(names, volume.Name)
	}
	return slices.Compact(slices.Sorted(slices.Values(names)))
}

// safetyVolumes restores each target from the safety backup, which holds the targets that existed before the restore.
func (p restorePlan) safetyVolumes() []types.ProjectBackupRestoreVolume {
	existing := make([]types.ProjectBackupRestoreVolume, 0, len(p.Volumes))
	for _, volume := range p.Volumes {
		if !slices.ContainsFunc(p.Create, func(created client.VolumeCreateOptions) bool { return created.Name == volume.Name }) {
			existing = append(existing, types.ProjectBackupRestoreVolume{Key: volume.Key, Snapshot: volume.Name, Name: volume.Name})
		}
	}
	return existing
}

// runRestore applies an accepted restore, or resumes one whose checkpoint an earlier attempt recorded.
func (s *Service) runRestore(ctx context.Context, t flow.Task) (any, error) {
	var input restoreRun
	if err := t.Payload(&input); err != nil {
		return nil, err
	}
	p := input.Plan
	previous, _ := jobcontext.Run(ctx)
	index := slices.IndexFunc(previous.Outcome.Targets, func(target types.TargetOutcome) bool {
		return target.ID == p.ID && len(target.RecoveryData) > 0
	})
	resumed := index >= 0
	if resumed {
		var recorded restorePlan
		if err := json.Unmarshal(previous.Outcome.Targets[index].RecoveryData, &recorded); err != nil {
			return nil, err
		}
		p = recorded
	}
	unlock, err := s.lockRestore(ctx, p, kit.Ternary(resumed, "", p.ID))
	// A resumed restore may have left the project down, so it waits for its turn instead of failing.
	if err != nil && resumed {
		return nil, fmt.Errorf("%w: %w", actor.ErrJobRejected, err)
	}
	if err != nil {
		return nil, err
	}
	defer unlock()
	if !resumed {
		// Rights can be revoked while a restore waits for its turn, so each one it needs is checked again before the
		// first change; a resumed restore must finish or roll back regardless.
		for _, permission := range append([]string{input.Requester.Permission}, p.extraPermissions()...) {
			requester := types.BackupRequester{
				UserID: input.Requester.UserID, APIKeyID: input.Requester.APIKeyID, EnvironmentID: input.Requester.EnvironmentID, Permission: permission,
			}
			if authorizeErr := s.deps.Engine.Authorize(ctx, requester); authorizeErr != nil {
				return nil, authorizeErr
			}
		}
		if p, err = s.prepareRestore(ctx, p); err != nil {
			return nil, err
		}
	}
	// From here the restore decides whether the project runs, by redeploying it or not. A resumed restore may have left
	// the project down, so it retries instead of failing.
	if forgetErr := s.deps.ForgetStopped(ctx, p.ProjectID); forgetErr != nil {
		if resumed {
			return nil, fmt.Errorf("%w: %w", actor.ErrJobRejected, forgetErr)
		}
		return nil, forgetErr
	}
	if applyErr := s.restoreSteps(input, t.ActivityID()).apply(ctx, p); applyErr != nil {
		return nil, applyErr
	}
	return types.SchedulerOutcome{Status: types.RunStatusSucceeded, Message: "Project backup restored successfully"}, nil
}

// lockRestore takes the restore's admissions, then the project lock that deploy, down and edits take, then its volumes'
// locks; run takes over the leases StartRestore parked for it.
func (s *Service) lockRestore(ctx context.Context, p restorePlan, run string) (func(), error) {
	leases, err := s.admit(ctx, admissions(p.ProjectID, p.volumeNames()), run)
	if err != nil {
		return nil, err
	}
	unlockProject, err := s.deps.LockProject(ctx, "restore project", p.ProjectID)
	if err != nil {
		release(context.WithoutCancel(ctx), leases)
		return nil, err
	}
	// A volume backup stops and starts the containers mounting its volume under that volume's lock, so every volume of
	// the project is held, not only the targets: none of its containers starts while the targets are rewritten.
	project, err := s.deps.Project(ctx, p.ProjectID, projectDetails)
	if err != nil {
		unlockProject()
		release(context.WithoutCancel(ctx), leases)
		return nil, err
	}
	locked := p.volumeNames()
	for _, volume := range projects.ClassifyProjectVolumes(project) {
		if volume.Name != "" {
			locked = append(locked, volume.Name)
		}
	}
	unlockVolumes, err := s.deps.LockVolumes(ctx, slices.Compact(slices.Sorted(slices.Values(locked))))
	if err != nil {
		unlockProject()
		release(context.WithoutCancel(ctx), leases)
		return nil, err
	}
	return func() {
		unlockVolumes()
		unlockProject()
		release(context.WithoutCancel(ctx), leases)
	}, nil
}

// prepareRestore checks a fresh restore against the backup's manifest and the project as it is now.
func (s *Service) prepareRestore(ctx context.Context, p restorePlan) (restorePlan, error) {
	manifest, err := s.Manifest(ctx, p.ProjectID, p.BackupID)
	if err != nil {
		return restorePlan{}, err
	}
	project, err := s.deps.Project(ctx, p.ProjectID, projectDetails)
	if err != nil {
		return restorePlan{}, err
	}
	// Without its Compose name the project's containers can't be found, so none could be stopped or removed.
	if project.ComposeName == "" {
		return restorePlan{}, common.Classify(common.ErrValidation, fmt.Errorf("the compose files of project %s cannot be loaded; fix them before restoring it", project.Name))
	}
	keys := make([]string, 0, len(p.Volumes))
	for _, volume := range p.Volumes {
		keys = append(keys, volume.Key)
	}
	targets, err := restoreTargets(project, backupVolumes(manifest.Volumes), keys)
	if err != nil {
		return restorePlan{}, err
	}
	// The admissions and locks cover the volumes found when the restore was requested.
	if !slices.Equal(targets, p.Volumes) {
		return restorePlan{}, common.Classify(common.ErrConflict, fmt.Errorf("the volumes of project %s changed after the restore was requested; start it again", project.Name))
	}
	// The project's directory or containers can change after the request.
	if checkErr := s.checkInPlace(ctx, project, p.Files); checkErr != nil {
		return restorePlan{}, checkErr
	}
	stopped, running, err := s.projectContainers(ctx, project.ComposeName)
	if err != nil {
		return restorePlan{}, err
	}
	if checkErr := refuseUnnamedVolumes(project, slices.Concat(running, stopped)); checkErr != nil {
		return restorePlan{}, checkErr
	}
	if freeErr := s.volumesFree(ctx, project, p.volumeNames()); freeErr != nil {
		return restorePlan{}, freeErr
	}
	create, err := s.missingVolumes(ctx, project, manifest, targets)
	if err != nil {
		return restorePlan{}, err
	}
	active, _, err := s.containerUse(ctx, project.ComposeName)
	if err != nil {
		return restorePlan{}, err
	}
	if len(active) > 0 && !p.MayStop {
		return restorePlan{}, common.Classify(common.ErrConflict, fmt.Errorf("project %s started after the restore was requested; start the restore again", project.Name))
	}
	return restorePlan{
		Redeploy:    p.Redeploy,
		ID:          p.ID,
		BackupID:    p.BackupID,
		ProjectID:   p.ProjectID,
		ProjectPath: kit.Ternary(p.Files, project.Path, ""),
		ComposeName: project.ComposeName,
		Volumes:     targets,
		Create:      create,
		Files:       p.Files,
		MayStop:     p.MayStop,
		Stopped:     p.Stopped,
		Running:     len(active) > 0,
	}, nil
}

// checkInPlace refuses an in-place restore that would take Arcane itself down, write under paused containers the
// restore does not stop, or, when it restores the project files, replace the files of other projects.
func (s *Service) checkInPlace(ctx context.Context, project types.Project, files bool) error {
	runsArcane, err := s.deps.RunsArcane(ctx, project.ID)
	if err != nil {
		return err
	}
	if runsArcane {
		return common.Classify(common.ErrValidation, fmt.Errorf("project %s runs Arcane itself, so it cannot be restored in place; restore it as a new project instead", project.Name))
	}
	dockerClient, err := s.deps.Docker.GetClient(ctx)
	if err != nil {
		return err
	}
	found, err := dockerClient.ContainerList(ctx, client.ContainerListOptions{
		Filters: make(client.Filters).Add("label", docker.ComposeProjectLabelKey+"="+project.ComposeName),
	})
	if err != nil {
		return fmt.Errorf("list containers: %w", err)
	}
	for _, c := range found.Items {
		if c.State == container.StatePaused {
			return common.Classify(common.ErrConflict, fmt.Errorf("container %s of project %s is paused; unpause or stop it before restoring", docker.ContainerNameFromNames(c.Names), project.Name))
		}
	}
	if !files {
		return nil
	}
	// A mount inside the project folder, such as a volume another project shares, would be rewritten with the files.
	mounts, err := projectSourceMounts(ctx, dockerClient, backup.SourceMountOptions{ContainerPath: project.Path, Target: "/project"})
	if err != nil {
		return err
	}
	if len(mounts) > 1 {
		return common.Classify(common.ErrValidation, fmt.Errorf(
			"the folder of project %s holds other mounts in the Arcane container (%s), so its files cannot be restored in place; restore it as a new project instead",
			project.Name, strings.Join(nestedTargets(mounts), ", "),
		))
	}
	return s.deps.SoleDirectory(ctx, project)
}

// volumesFree refuses volumes that a running container outside the project mounts, since the restore would write under it.
func (s *Service) volumesFree(ctx context.Context, project types.Project, names []string) error {
	_, inUse, err := s.containerUse(ctx, project.ComposeName)
	if err != nil {
		return err
	}
	for _, name := range names {
		if inUse[name] {
			return common.Classify(common.ErrConflict, fmt.Errorf("volume %s is mounted by a running container outside project %s; stop it before restoring", name, project.Name))
		}
	}
	return nil
}

// missingVolumes returns how to create each target that does not exist yet the way Compose would; an external volume
// gets the Docker labels the backup recorded instead of Compose's. An existing target must keep its data in Docker.
func (s *Service) missingVolumes(ctx context.Context, project types.Project, manifest types.ProjectManifest, targets []types.ProjectBackupRestoreVolume) ([]client.VolumeCreateOptions, error) {
	dockerClient, err := s.deps.Docker.GetClient(ctx)
	if err != nil {
		return nil, err
	}
	classified := projects.ClassifyProjectVolumes(project)
	var create []client.VolumeCreateOptions
	for _, target := range targets {
		if slices.ContainsFunc(create, func(options client.VolumeCreateOptions) bool { return options.Name == target.Name }) {
			continue
		}
		current := classified[slices.IndexFunc(classified, func(volume types.ProjectVolume) bool { return volume.Key == target.Key })]
		found, inspectErr := dockerClient.VolumeInspect(ctx, target.Name, client.VolumeInspectOptions{})
		switch {
		// As when backing up: a named volume is restored whatever its driver; an external one only while Docker keeps
		// its data.
		case inspectErr == nil && current.Kind == types.ProjectVolumeExternal && (cmp.Or(found.Volume.Driver, "local") != "local" || len(found.Volume.Options) > 0):
			return nil, common.Classify(common.ErrValidation, fmt.Errorf("volume %s now keeps its data outside Docker, so it cannot be restored", target.Name))
		case inspectErr == nil:
			continue
		case !errdefs.IsNotFound(inspectErr):
			return nil, fmt.Errorf("inspect volume %s: %w", target.Name, inspectErr)
		}
		recorded := slices.IndexFunc(manifest.Volumes, func(volume types.ProjectVolume) bool { return volume.Key == target.Key })
		external := current.Kind == types.ProjectVolumeExternal
		options, optionsErr := projects.ComposeVolumeTargetCreateOptions(target.Name, types.ComposeVolumeTarget{
			Labels:   kit.Ternary(external, manifest.Volumes[recorded].DockerLabels, current.Labels),
			Project:  project.ComposeName,
			Key:      target.Key,
			Driver:   current.Driver,
			External: external,
		})
		if optionsErr != nil {
			return nil, optionsErr
		}
		create = append(create, options)
	}
	return create, nil
}

// apply runs an in-place restore from its checkpoint; one resumed before its restore finished, or during its rollback,
// rolls back instead. On any failure the project is rolled back and deployed again if it ran.
func (r restoreSteps) apply(ctx context.Context, p restorePlan) error {
	switch {
	case p.Failure != "":
		return errors.New(p.Failure)
	case p.RollingBack:
		return r.rollback(ctx, p, errors.New("the restore was interrupted"))
	case p.Restored:
		return r.finish(ctx, p)
	case p.Started:
		return r.rollback(ctx, p, errors.New("the restore was interrupted"))
	}
	p.Started = true
	if err := r.record(ctx, p); err != nil {
		return err
	}
	if p.Running {
		r.report(ctx, "Stopping the project")
		if err := r.down(ctx); err != nil {
			return r.rollback(ctx, p, fmt.Errorf("stop the project: %w", err))
		}
	}
	// A stopped container can be started on its own, without the project lock the restore holds, so none is left.
	if p.Stopped {
		r.report(ctx, "Removing the project's stopped containers")
	}
	if err := r.clear(ctx, p); err != nil {
		return r.rollback(ctx, p, fmt.Errorf("remove the project's stopped containers: %w", err))
	}
	r.report(ctx, "Taking a safety backup")
	safetyID, err := r.safety(ctx, p)
	if err != nil {
		return r.rollback(ctx, p, fmt.Errorf("take a safety backup: %w", err))
	}
	// A container started during the safety backup could be using a volume now; nothing has been written yet, so
	// the rollback leaves the volumes alone.
	if err = r.inUse(ctx, p); err != nil {
		return r.rollback(ctx, p, err)
	}
	p.SafetyID = safetyID
	if err = r.record(ctx, p); err != nil {
		return r.rollback(ctx, p, err)
	}
	r.report(ctx, "Restoring the backup")
	// The claim marks each volume as this restore's from the moment Docker creates it, so a rollback, resumed or not,
	// removes it even when the restore stopped before learning it was created.
	for _, options := range p.Create {
		if err = r.create(ctx, volumes.ClaimedVolume{Options: options, Claim: restoreOwner + p.ID}); err != nil {
			return r.rollback(ctx, p, fmt.Errorf("create the missing volumes: %w", err))
		}
	}
	if err = r.restoreBackup(ctx, p); err != nil {
		return r.rollback(ctx, p, err)
	}
	p.Restored = true
	if err = r.record(ctx, p); err != nil {
		return r.rollback(ctx, p, err)
	}
	return r.finish(ctx, p)
}

func (r restoreSteps) restoreBackup(ctx context.Context, p restorePlan) error {
	if err := r.restore(ctx, types.ProjectBackupRestore{
		ProjectID: p.ProjectID, BackupID: p.BackupID, ProjectPath: p.ProjectPath, Volumes: p.Volumes, Owner: restoreOwner + p.ID, Report: r.report,
	}); err != nil {
		return err
	}
	return r.verify(ctx, p)
}

// finish deploys the restored project when asked, by default when it ran before the restore.
func (r restoreSteps) finish(ctx context.Context, p restorePlan) error {
	if !*cmp.Or(p.Redeploy, new(p.Running)) {
		return nil
	}
	p.Deploying = true
	if err := r.record(ctx, p); err != nil {
		return r.rollback(ctx, p, err)
	}
	r.report(ctx, "Deploying the project")
	if err := r.deploy(ctx); err != nil {
		return r.rollback(ctx, p, fmt.Errorf("deploy the restored project: %w", err))
	}
	return nil
}

// rollback restores the safety backup, removes the volumes the restore created, and deploys the project if it ran; a
// project whose safety backup could not be restored is left down rather than started on half-restored data, and one
// that could not be stopped is left as is.
func (r restoreSteps) rollback(ctx context.Context, p restorePlan, cause error) error {
	ctx = context.WithoutCancel(ctx)
	p.RollingBack = true
	cleanup := []error{r.record(ctx, p)}
	var downErr, undoErr, deployErr error
	if p.SafetyID != "" {
		r.report(ctx, "Rolling back to the safety backup")
		if p.Deploying {
			downErr = r.down(ctx)
		}
		if downErr == nil {
			undoErr = r.restore(ctx, types.ProjectBackupRestore{
				ProjectID: p.ProjectID, BackupID: p.SafetyID, ProjectPath: p.ProjectPath, Volumes: p.safetyVolumes(), Owner: restoreOwner + p.ID, Report: r.report,
			})
			cleanup = append(cleanup, r.remove(ctx, createdNames(p.Create), restoreOwner+p.ID))
		}
	}
	if p.Running && downErr == nil && undoErr == nil {
		// Saved first, so a run resumed during this deploy stops the project before restoring again.
		p.Deploying = true
		if deployErr = r.record(ctx, p); deployErr == nil {
			deployErr = r.deploy(ctx)
		}
	}
	var err error
	switch failed := errors.Join(cleanup...); {
	case downErr != nil:
		err = fmt.Errorf(
			"%w; the project could not be stopped for the rollback (%w), so it was left as is; restore the safety backup %s by hand to undo the restore",
			cause, errors.Join(downErr, failed), p.SafetyID,
		)
	case undoErr != nil:
		err = fmt.Errorf("%w; the rollback failed too (%w), so the project was left down; restore the safety backup %s to undo the restore", cause, errors.Join(undoErr, failed), p.SafetyID)
	case deployErr != nil && p.SafetyID != "":
		err = fmt.Errorf("%w; the project was rolled back to the safety backup %s, but could not be deployed: %w", cause, p.SafetyID, errors.Join(deployErr, failed))
	case deployErr != nil:
		err = fmt.Errorf("%w; nothing was restored, but the project could not be deployed again: %w", cause, deployErr)
	case failed != nil:
		err = fmt.Errorf("%w; the project was rolled back to the safety backup %s, but the rollback hit errors: %w", cause, p.SafetyID, failed)
	case p.SafetyID != "":
		err = fmt.Errorf("%w; the project was rolled back to the safety backup %s", cause, p.SafetyID)
	default:
		err = fmt.Errorf("%w; nothing was restored", cause)
	}
	p.Failure = err.Error()
	return errors.Join(err, r.record(ctx, p))
}

func createdNames(create []client.VolumeCreateOptions) []string {
	names := make([]string, 0, len(create))
	for _, options := range create {
		names = append(names, options.Name)
	}
	return names
}

func (s *Service) restoreSteps(input restoreRun, activityID string) restoreSteps {
	projectID := input.Plan.ProjectID
	return restoreSteps{
		record: func(ctx context.Context, p restorePlan) error {
			checkpoint, err := json.Marshal(p)
			if err != nil {
				return err
			}
			return jobcontext.Progress(ctx, types.TargetOutcome{ResourceType: restoreTargetType, ID: p.ID, Status: types.RunStatusRunning, RecoveryData: checkpoint})
		},
		report: func(ctx context.Context, message string) {
			activity.AppendOrWarn(ctx, s.deps.Activities, activityID, types.ActivityMessageLevelInfo, message)
		},
		down: func(ctx context.Context) error {
			return s.deps.Down(ctx, projectID, input.User)
		},
		clear: s.removeStoppedContainers,
		deploy: func(ctx context.Context) error {
			return s.deps.Deploy(ctx, projectID, input.User, nil)
		},
		safety: s.safetyBackup,
		inUse: func(ctx context.Context, p restorePlan) error {
			if len(p.Volumes) == 0 {
				return nil
			}
			project, err := s.deps.Project(ctx, projectID, projectDetails)
			if err != nil {
				return err
			}
			return s.volumesFree(ctx, project, p.volumeNames())
		},
		create:  s.createVolume,
		restore: s.RestoreTo,
		verify:  s.verifyRestore,
		remove:  s.removeVolumes,
	}
}

// safetyBackup takes a local backup of the project files and the existing targets, recording them as a backup does;
// the restore holds the locks.
func (s *Service) safetyBackup(ctx context.Context, p restorePlan) (string, error) {
	project, err := s.deps.Project(ctx, p.ProjectID, projectDetails)
	if err != nil {
		return "", err
	}
	created := createdNames(p.Create)
	existing := make([]string, 0, len(p.Volumes))
	for _, target := range p.Volumes {
		if !slices.Contains(created, target.Name) {
			existing = append(existing, target.Key)
		}
	}
	captured, err := s.inspectVolumes(ctx, project, existing)
	if err != nil {
		return "", err
	}
	entry := &ProjectBackup{
		BaseModel:   database.BaseModel{ID: uuid.New().String(), CreatedAt: time.Now()},
		ProjectID:   project.ID,
		ProjectName: project.Name,
		ComposeName: project.ComposeName,
		Status:      types.VolumeBackupStatusRunning,
		Trigger:     types.VolumeBackupTriggerSafety,
		Destination: types.VolumeBackupDestinationLocal,
		Volumes:     backupVolumes(captured),
	}
	if createErr := s.deps.DB.WithContext(ctx).Create(entry).Error; createErr != nil {
		return "", fmt.Errorf("failed to save the safety backup: %w", createErr)
	}
	err = s.snapshot(ctx, entry, plan{BackupID: entry.ID, ProjectID: project.ID, Volumes: captured, LocalEnabled: true})
	if completeErr := s.complete(ctx, entry, err); completeErr != nil {
		return "", completeErr
	}
	return entry.ID, nil
}

// removeStoppedContainers removes the project's stopped containers, which a deploy creates again. The project is
// stopped by now, so a running container is refused, and so is a stopped one the request did not check for.
func (s *Service) removeStoppedContainers(ctx context.Context, p restorePlan) error {
	stopped, active, err := s.projectContainers(ctx, p.ComposeName)
	switch {
	case err != nil:
		return err
	case len(active) > 0:
		return common.Classify(common.ErrConflict, fmt.Errorf("container %s was started during the restore", docker.ContainerNameFromNames(active[0].Names)))
	case len(stopped) > 0 && !p.Stopped && !p.MayStop:
		return common.Classify(common.ErrConflict, errors.New("the project's containers changed after the restore was requested; start it again"))
	}
	dockerClient, err := s.deps.Docker.GetClient(ctx)
	if err != nil {
		return err
	}
	var errs []error
	for _, c := range stopped {
		if _, removeErr := dockerClient.ContainerRemove(ctx, c.ID, client.ContainerRemoveOptions{}); removeErr != nil && !errdefs.IsNotFound(removeErr) {
			errs = append(errs, fmt.Errorf("remove container %s: %w", docker.ContainerNameFromNames(c.Names), removeErr))
		}
	}
	return errors.Join(errs...)
}

// refuseUnnamedVolumes refuses containers that keep data in an unnamed volume: only the container links the volume to
// its service, so taking the project down or removing the container would leave the data behind, and the next deploy
// would start on an empty volume that no safety backup holds. A volume the project declares by name, even a generated
// one, is attached again by Compose.
func refuseUnnamedVolumes(project types.Project, containers []container.Summary) error {
	if c, m, detached := projects.UnnamedVolumeMount(project, containers); detached {
		return common.Classify(common.ErrValidation, fmt.Errorf(
			"container %s keeps data at %s in an unnamed volume, which taking the project down for the restore would lose; "+
				"remove the container if that data is not needed, or move it to a named volume, then restore again",
			docker.ContainerNameFromNames(c.Names), m.Destination,
		))
	}
	return nil
}

// projectContainers lists the project's own containers, leaving out Arcane's and its helpers as containerUse does.
func (s *Service) projectContainers(ctx context.Context, composeName string) (stopped, active []container.Summary, err error) {
	if composeName == "" {
		return nil, nil, errors.New("the project's Compose name is unknown, so its containers can't be found")
	}
	dockerClient, err := s.deps.Docker.GetClient(ctx)
	if err != nil {
		return nil, nil, err
	}
	found, err := dockerClient.ContainerList(ctx, client.ContainerListOptions{
		All:     true,
		Filters: make(client.Filters).Add("label", docker.ComposeProjectLabelKey+"="+composeName),
	})
	if err != nil {
		return nil, nil, fmt.Errorf("list containers: %w", err)
	}
	for _, c := range found.Items {
		switch {
		case volumes.IsHelperContainer(c) || labels.IsArcaneContainer(c.Labels):
		case c.State == container.StateRunning || c.State == container.StateRestarting || c.State == container.StatePaused:
			active = append(active, c)
		default:
			stopped = append(stopped, c)
		}
	}
	return stopped, active, nil
}

// removeLeftovers removes the Rustic containers of an earlier run of this restore, left by an Arcane that stopped
// mid-restore; writing alongside one would mix two restores, so the restore stops if one can't be removed.
func removeLeftovers(ctx context.Context, dockerClient *client.Client, owner string) error {
	found, err := dockerClient.ContainerList(ctx, client.ContainerListOptions{
		All:     true,
		Filters: make(client.Filters).Add("label", rustic.OwnerLabel+"="+owner),
	})
	if err != nil {
		return fmt.Errorf("list containers: %w", err)
	}
	for _, c := range found.Items {
		slog.WarnContext(ctx, "Removing a Rustic container left over from an earlier run of this restore", "containerId", c.ID)
		if _, removeErr := dockerClient.ContainerRemove(ctx, c.ID, volumes.RemoveOptions()); removeErr != nil && !errdefs.IsNotFound(removeErr) {
			return common.Classify(common.ErrConflict, fmt.Errorf(
				"a Rustic container left over from an earlier run of this restore (%s) could not be removed, so nothing was written; remove it and try again: %w",
				c.ID, removeErr,
			))
		}
	}
	return nil
}

// createVolume creates a missing target volume with the restore's claim; one someone else created since the restore was
// checked is refused, so the restore never takes it over or removes it.
func (s *Service) createVolume(ctx context.Context, volume volumes.ClaimedVolume) error {
	options := volume.Options
	dockerClient, err := s.deps.Docker.GetClient(ctx)
	if err != nil {
		return err
	}
	_, inspectErr := dockerClient.VolumeInspect(ctx, options.Name, client.VolumeInspectOptions{})
	switch {
	case inspectErr == nil:
		return common.Classify(common.ErrConflict, fmt.Errorf("volume %s was created after the restore was checked", options.Name))
	case !errdefs.IsNotFound(inspectErr):
		return fmt.Errorf("inspect volume %s: %w", options.Name, inspectErr)
	}
	// Docker hands back a volume made between the check above and this create, so only one carrying the restore's
	// claim is the restore's.
	created, createErr := volumes.CreateClaimed(ctx, dockerClient, volume)
	if createErr != nil {
		return createErr
	}
	if !created {
		return common.Classify(common.ErrConflict, fmt.Errorf("volume %s was created after the restore was checked", options.Name))
	}
	return nil
}

// removeVolumes removes the named volumes that carry claim, so one that existed before the restore, or that someone
// else created, is left alone.
func (s *Service) removeVolumes(ctx context.Context, names []string, claim string) error {
	dockerClient, err := s.deps.Docker.GetClient(ctx)
	if err != nil {
		return err
	}
	var errs []error
	for _, name := range names {
		found, removeErr := dockerClient.VolumeInspect(ctx, name, client.VolumeInspectOptions{})
		if removeErr == nil && found.Volume.Labels[volumes.ClaimLabel] == claim {
			_, removeErr = dockerClient.VolumeRemove(ctx, name, client.VolumeRemoveOptions{})
		}
		if removeErr != nil && !errdefs.IsNotFound(removeErr) {
			errs = append(errs, fmt.Errorf("remove volume %s: %w", name, removeErr))
		}
	}
	return errors.Join(errs...)
}

// verifyRestore sets the project's Compose name back in restored files that declare another one, then fails when they
// still do or name a restored volume differently, since the project would no longer use the data restored into it.
func (s *Service) verifyRestore(ctx context.Context, p restorePlan) error {
	if p.ProjectPath == "" {
		return nil
	}
	project, err := s.deps.Project(ctx, p.ProjectID, projectDetails)
	if err != nil {
		return err
	}
	if project.ComposeName != p.ComposeName {
		if rewriteErr := s.deps.RewriteComposeName(ctx, p.ProjectID, p.ComposeName); rewriteErr != nil {
			return fmt.Errorf("set the Compose project name %s in the restored files: %w", p.ComposeName, rewriteErr)
		}
		if project, err = s.deps.Project(ctx, p.ProjectID, projectDetails); err != nil {
			return err
		}
	}
	if project.ComposeName != p.ComposeName {
		return common.Classify(common.ErrConflict, fmt.Errorf(
			"the restored files name the Compose project %q instead of %q, and it could not be changed back; restore the backup as a new project instead",
			project.ComposeName, p.ComposeName,
		))
	}
	classified := projects.ClassifyProjectVolumes(project)
	for _, target := range p.Volumes {
		if !slices.ContainsFunc(classified, func(volume types.ProjectVolume) bool { return volume.Key == target.Key && volume.Name == target.Name }) {
			return common.Classify(common.ErrConflict, fmt.Errorf(
				"the restored compose files no longer name volume %s %s, so the project would not use its restored data; restore the backup as a new project instead",
				target.Key, target.Name,
			))
		}
	}
	return nil
}

// Manifest returns the manifest a succeeded backup of the project recorded in its snapshot.
func (s *Service) Manifest(ctx context.Context, projectID, backupID string) (types.ProjectManifest, error) {
	entry, err := s.restorable(ctx, projectID, backupID)
	if err != nil {
		return types.ProjectManifest{}, err
	}
	dockerClient, err := s.deps.Docker.GetClient(ctx)
	if err != nil {
		return types.ProjectManifest{}, err
	}
	source, err := s.source(ctx, dockerClient, entry)
	if err != nil {
		return types.ProjectManifest{}, err
	}
	return s.readManifest(ctx, dockerClient, source)
}

// RestoreTo restores parts of a succeeded backup of the project, replacing the contents of the project directory and
// volumes it names; the caller holds the locks they need, or asks for the volumes' with LockVolumes.
func (s *Service) RestoreTo(ctx context.Context, opts types.ProjectBackupRestore) error {
	// Checked even with nothing to restore, since a leftover may still write to a volume a rollback is about to remove.
	if opts.Owner != "" {
		dockerClient, err := s.deps.Docker.GetClient(ctx)
		if err != nil {
			return err
		}
		if leftoverErr := removeLeftovers(ctx, dockerClient, opts.Owner); leftoverErr != nil {
			return leftoverErr
		}
		ctx = rustic.WithOwner(ctx, opts.Owner)
	}
	if opts.ProjectPath == "" && len(opts.Volumes) == 0 {
		return nil
	}
	if opts.LockVolumes {
		var names []string
		var keys []types.SchedulerAdmissionKey
		for _, volume := range opts.Volumes {
			if !slices.Contains(names, volume.Name) {
				names = append(names, volume.Name)
				keys = append(keys, types.SchedulerAdmissionKey{Scope: backup.VolumeAdmissionScope, ID: volume.Name})
			}
		}
		leases, admitErr := s.admit(ctx, keys, "restore-to:"+opts.BackupID)
		if admitErr != nil {
			return admitErr
		}
		defer release(context.WithoutCancel(ctx), leases)
		unlock, lockErr := s.deps.LockVolumes(ctx, names)
		if lockErr != nil {
			return lockErr
		}
		defer unlock()
	}
	entry, err := s.restorable(ctx, opts.ProjectID, opts.BackupID)
	if err != nil {
		return err
	}
	dockerClient, err := s.deps.Docker.GetClient(ctx)
	if err != nil {
		return err
	}
	source, err := s.source(ctx, dockerClient, entry)
	if err != nil {
		return err
	}
	// The manifest, not the caller, says which volumes the snapshot holds.
	manifest, err := s.readManifest(ctx, dockerClient, source)
	if err != nil {
		return err
	}
	var projectMounts []mount.Mount
	if opts.ProjectPath != "" {
		projectMounts, err = projectSourceMounts(ctx, dockerClient, backup.SourceMountOptions{ContainerPath: opts.ProjectPath, Target: path.Join(restoreRoot, "project"), Writable: true})
		if err != nil {
			return err
		}
		// Checked again here, since every mount inside the folder would be written to.
		if len(projectMounts) > 1 {
			return common.Classify(common.ErrValidation, fmt.Errorf("the project folder holds other mounts in the Arcane container (%s); its files were not restored",
				strings.Join(nestedTargets(projectMounts), ", ")))
		}
	}
	target, options, err := restoreInput(projectMounts, opts.Volumes, plan{Volumes: manifest.Volumes}.volumeNames())
	if err != nil {
		return err
	}
	if restoreErr := s.deps.Engine.RestoreSnapshot(ctx, dockerClient, source.repository, source.password, source.id, target, options); restoreErr != nil {
		return fmt.Errorf("failed to restore the project snapshot: %w", restoreErr)
	}
	if opts.OwnEnv && opts.ProjectPath != "" {
		if linkErr := projects.OwnLinks(ctx, opts.ProjectPath, manifest.ProjectDirs, opts.Report); linkErr != nil {
			return linkErr
		}
	}
	// The snapshot holds only the link of a .env kept outside the project directory, so its contents come from beside
	// the manifest. A rollback restores the safety backup through here too, which puts the earlier contents back.
	if opts.ProjectPath == "" || !manifest.ExternalEnv {
		return nil
	}
	env, err := s.deps.Engine.ReadSnapshotTextFile(ctx, dockerClient, source.repository, source.password, source.id, path.Join("meta", envName))
	if err != nil {
		return fmt.Errorf("read the .env file the backup stored: %w", err)
	}
	projectsRoot, err := s.deps.ProjectRoot(ctx, opts.ProjectPath)
	if err != nil {
		return err
	}
	return s.restoreEnv(ctx, projectsRoot, opts, env)
}

// restoreEnv writes a .env kept outside the project directory through the link the snapshot restored, as an edit of
// the .env does; with OwnEnv it replaces the link with a file of its own, as an import of an export does. A file
// another project's .env leads to keeps its contents, since the restore leaves other projects as they are.
func (s *Service) restoreEnv(ctx context.Context, projectsRoot string, opts types.ProjectBackupRestore, content string) error {
	if opts.OwnEnv {
		if err := projects.RemoveProjectFile(ctx, projectsRoot, opts.ProjectPath, projects.EffectiveEnvFileName); err != nil {
			return err
		}
	} else {
		file, sharer, err := s.envSharer(ctx, opts)
		if err != nil {
			return err
		}
		if sharer != "" {
			opts.Report(ctx, fmt.Sprintf("The .env file %s was left as it is, since project %s uses it too; only the link to it was restored", file, sharer))
			return nil
		}
	}
	if err := projects.WriteProjectFile(ctx, projectsRoot, opts.ProjectPath, projects.EffectiveEnvFileName, content); err != nil {
		return fmt.Errorf("restore the .env file the backup stored: %w", err)
	}
	return nil
}

// envSharer returns the file the project's .env leads to and the name of another project whose .env leads to the same
// file, if any. Both sides resolve every link on the way, since a path through a linked directory names the same file
// differently.
func (s *Service) envSharer(ctx context.Context, opts types.ProjectBackupRestore) (file, sharer string, err error) {
	file, err = filepath.EvalSymlinks(filepath.Join(opts.ProjectPath, projects.EffectiveEnvFileName))
	if err != nil {
		return "", "", fmt.Errorf("resolve the restored .env file: %w", err)
	}
	all, err := s.deps.Projects(ctx)
	if err != nil {
		return "", "", err
	}
	for _, other := range all {
		if other.ID == opts.ProjectID {
			continue
		}
		// A .env that doesn't resolve, such as a dangling link or a link loop, leads to no file, so it shares none.
		target, resolveErr := filepath.EvalSymlinks(filepath.Join(other.Path, projects.EffectiveEnvFileName))
		if resolveErr == nil && target == file {
			return file, other.Name, nil
		}
	}
	return file, "", nil
}

// restoreInput mounts the targets under /dest as the backup laid them out under /snapshot and restores the snapshot root,
// so each target root keeps its owner and mode; globs skip the rest, and /dest holds only targets, so --delete is safe.
func restoreInput(projectMounts []mount.Mount, targets []types.ProjectBackupRestoreVolume, captured []string) (mount.Mount, backup.RestoreOptions, error) {
	mounts := slices.Clone(projectMounts)
	globs := []string{"!/meta", "!/meta/**"}
	if len(projectMounts) == 0 {
		globs = append(globs, "!/project", "!/project/**")
	}
	restored := map[string]string{}
	for _, volume := range targets {
		into, seen := restored[volume.Snapshot]
		switch {
		case !slices.Contains(captured, volume.Snapshot):
			return mount.Mount{}, backup.RestoreOptions{}, common.Classify(common.ErrValidation, fmt.Errorf("the backup holds no data for volume %s", volume.Key))
		case seen && into != volume.Name:
			return mount.Mount{}, backup.RestoreOptions{}, common.Classify(common.ErrValidation, fmt.Errorf("the backup's volume %s cannot be restored into both %s and %s", volume.Snapshot, into, volume.Name))
		case !seen && slices.Contains(slices.Collect(maps.Values(restored)), volume.Name):
			return mount.Mount{}, backup.RestoreOptions{}, common.Classify(common.ErrValidation, fmt.Errorf("two of the backup's volumes would be restored into %s", volume.Name))
		case seen:
			continue
		}
		restored[volume.Snapshot] = volume.Name
		mounts = append(mounts, mount.Mount{Type: mount.TypeVolume, Source: volume.Name, Target: path.Join(restoreRoot, "volumes", volume.Snapshot)})
	}
	for _, name := range captured {
		if _, ok := restored[name]; !ok {
			globs = append(globs, "!/volumes/"+name, "!/volumes/"+name+"/**")
		}
	}
	if len(mounts) == 0 {
		return mount.Mount{}, backup.RestoreOptions{}, common.Classify(common.ErrValidation, errors.New("select the project files or at least one volume to restore"))
	}
	return mounts[0], backup.RestoreOptions{DestinationPath: restoreRoot, ExtraMounts: mounts[1:], Globs: globs, DeleteExtra: true}, nil
}

// restorable loads a backup of the project that succeeded, so it has a snapshot to restore.
func (s *Service) restorable(ctx context.Context, projectID, backupID string) (*ProjectBackup, error) {
	entry, err := s.load(ctx, backupID)
	if err != nil {
		return nil, err
	}
	switch {
	case entry.ProjectID != projectID:
		return nil, common.Classify(common.ErrNotFound, errors.New("project backup not found"))
	case entry.Status != types.VolumeBackupStatusSucceeded || (entry.LocalSnapshotID == "" && entry.RemoteSnapshotID == ""):
		return nil, common.Classify(common.ErrValidation, errors.New("only a backup that succeeded can be restored"))
	}
	return entry, nil
}

// source opens the repository holding the backup's snapshot, preferring the local copy.
func (s *Service) source(ctx context.Context, dockerClient *client.Client, entry *ProjectBackup) (snapshotSource, error) {
	remote := entry.LocalSnapshotID == ""
	repository, err := s.repository(ctx, dockerClient, plan{S3DestinationID: entry.S3DestinationID}, remote)
	if err != nil {
		return snapshotSource{}, err
	}
	password, err := s.deps.Repository.Password(ctx, dockerClient, repository)
	if err != nil {
		return snapshotSource{}, err
	}
	return snapshotSource{repository: repository, password: password, id: kit.Ternary(remote, entry.RemoteSnapshotID, entry.LocalSnapshotID)}, nil
}

func (s *Service) readManifest(ctx context.Context, dockerClient *client.Client, source snapshotSource) (types.ProjectManifest, error) {
	text, err := s.deps.Engine.ReadSnapshotTextFile(ctx, dockerClient, source.repository, source.password, source.id, path.Join("meta", manifestName))
	if err != nil {
		return types.ProjectManifest{}, err
	}
	var manifest types.ProjectManifest
	if decodeErr := json.Unmarshal([]byte(text), &manifest); decodeErr != nil {
		return types.ProjectManifest{}, fmt.Errorf("decode the project backup manifest: %w", decodeErr)
	}
	if manifest.Kind != types.ProjectManifestKindBackup || manifest.FormatVersion > types.ProjectManifestFormatVersion {
		return types.ProjectManifest{}, common.Classify(common.ErrValidation, fmt.Errorf(
			"the backup's manifest is a %q manifest in format %d, which this version of Arcane cannot restore", manifest.Kind, manifest.FormatVersion,
		))
	}
	return manifest, nil
}
