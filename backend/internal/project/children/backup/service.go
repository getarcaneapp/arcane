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
	"kit.getarcane.app/docker"

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
	"getarcane.dev/arcane/v3/pkg/volumes"
	"getarcane.dev/types/v3"
)

type (
	Dependencies struct {
		DB             *database.DB
		Docker         *dockerInternal.DockerClientService
		Engine         *backup.Engine
		Repository     *backup.VolumeRepository
		Policies       *backup.Policies
		S3Destinations *s3.S3DestinationService
		Settings       *settings.SettingsService
		// LockVolumes takes the workspace locks that volume backups and restores also take.
		LockVolumes  func(ctx context.Context, names []string) (func(), error)
		Project      func(ctx context.Context, projectID string, opts types.ProjectDetailsOptions) (types.Project, error)
		StopProject  func(ctx context.Context, projectID string, services []string, user types.Actor) error
		StartProject func(ctx context.Context, projectID string, services []string, user types.Actor) error
		// Volume inspects a Docker volume and reports whether it exists.
		Volume func(ctx context.Context, name string) (types.Volume, bool, error)
		// BackupStore names the volume holding local backups, which a backup never copies.
		BackupStore func(ctx context.Context) string
		// LockProject takes the project lock that deploy, down and project edits also take.
		LockProject func(ctx context.Context, op, projectID string) (func(), error)
		// SoleDirectory refuses a project directory that is a projects directory or holds another project.
		SoleDirectory func(ctx context.Context, project types.Project) error
		// RunsArcane reports whether the project's containers include the running Arcane container.
		RunsArcane func(ctx context.Context, projectID string) (bool, error)
		// Deploy deploys the project; the caller holds the project lock.
		Deploy func(ctx context.Context, projectID string, user types.Actor, options *types.ProjectDeployOptions) error
		// RewriteComposeName sets the Compose project name in the project's files, as an import does.
		RewriteComposeName func(ctx context.Context, projectID, name string) error
		// ProjectName returns the project's name without loading its compose files.
		ProjectName func(ctx context.Context, projectID string) (string, error)
		// ListProjects returns the ID and name of every project on this server.
		ListProjects func(ctx context.Context) ([]types.ProjectReference, error)
		// SyncProjects brings the project rows in line with the project folders.
		SyncProjects func(ctx context.Context) error
		// ProjectRoot returns the projects directory holding the project directory, which project file writes stay in.
		ProjectRoot func(ctx context.Context, projectPath string) (string, error)
		// Projects lists every project, archived ones included.
		Projects func(ctx context.Context) ([]types.ProjectRecord, error)
		// DeleteVolume removes a volume a failed restore created.
		DeleteVolume func(ctx context.Context, name string, user types.Actor) error
		Activities   activity.MessageAppender
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
		watch     backup.RunWatch
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
		S3DestinationID string                `json:"s3DestinationId,omitempty"`
		Volumes         []types.ProjectVolume `json:"volumes"`
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

	// globalRun runs a global policy; a run started by hand also carries its requester.
	globalRun struct {
		Requester *types.BackupRequester `json:"requester,omitempty"`
		PolicyID  string                 `json:"policyId"`
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

	// restorePlan is an in-place restore's frozen request, completed under the restore's locks.
	restorePlan struct {
		ID        string `json:"id"`
		BackupID  string `json:"backupId"`
		ProjectID string `json:"projectId"`
		// ProjectPath is set when the project files are restored.
		ProjectPath string `json:"projectPath,omitempty"`
		// ComposeName is the project's Compose name before the restore, which restored files must keep.
		ComposeName string                             `json:"composeName"`
		Volumes     []types.ProjectBackupRestoreVolume `json:"volumes"`

		backup.RestoreState `json:",inline"`

		Files bool `json:"files"`
	}

	restoreRun struct {
		Requester types.BackupRequester `json:"requester"`
		User      types.Actor           `json:"user"`
		Plan      restorePlan           `json:"plan"`
	}

	snapshotSource struct {
		password   string
		id         string
		repository backup.Repository
	}

	// DiscoverOptions selects where discovery searches: the local repository and this instance's root on S3 destinations.
	DiscoverOptions struct {
		S3DestinationIDs []string
		Local            bool
	}

	// foundSnapshot is a project snapshot in the local repository or, with an S3 destination ID, on that destination.
	foundSnapshot struct {
		source          snapshotSource
		s3DestinationID string
		snapshot        backup.DiscoveredSnapshot
	}
)

const (
	admissionScope = "project-backup"
	// globalAdmissionScope keeps one run of each global policy at a time; it is always taken before a project's.
	globalAdmissionScope = "global-project-backup"
	manifestName         = "arcane-project.json"
	// envName holds, beside the manifest, the file a project's .env links to outside the project directory.
	envName          = "project.env"
	targetType       = "project_backup"
	backupsTable     = "project_backups"
	projectTagPrefix = "arcane-project:"
	triggerTagPrefix = "arcane-trigger:"
	policyTagPrefix  = "arcane-policy:"
	// discoveryPendingKey is set by a system restore and cleared once discovery searched every repository.
	discoveryPendingKey = "projectBackupDiscoveryPending"
)

var (
	errAlreadyRunning     = common.Classify(common.ErrConflict, errors.New("a backup or restore of this project is already running"))
	errPolicyGone         = errors.New("backup policy disabled or deleted")
	errRemoteOnlyDisabled = errors.New("the policy's only destination is missing its S3 repository, so the policy was disabled")
	errProjectSkipped     = errors.New("the project is no longer selected or has its own enabled backup policy")
	projectDetails        = types.ProjectDetailsOptions{
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
	s.watch = backup.RunWatch{Flow: engine, Workflow: s.workflow, Fail: s.fail}
	// Like a backup, a restore runs as a workflow so it holds its admissions and reports through its activity.
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
	entry := &ProjectBackup{ //exhaustruct:enforce
		BaseModel:         database.BaseModel{ID: p.BackupID, CreatedAt: time.Now(), UpdatedAt: nil},
		CompletedAt:       nil,
		ProjectID:         project.ID,
		ProjectName:       project.Name,
		ComposeName:       project.ComposeName,
		Status:            types.VolumeBackupStatusRunning,
		Trigger:           types.VolumeBackupTriggerManual,
		Destination:       backup.Destination(backup.DestinationOptions{Local: p.LocalEnabled, Remote: p.S3Enabled}),
		LocalSnapshotID:   "",
		RemoteSnapshotID:  "",
		S3DestinationID:   p.S3DestinationID,
		PolicyID:          "",
		Error:             "",
		Volumes:           backupVolumes(p.Volumes),
		Size:              0,
		StoppedContainers: false,
	}
	if createErr := s.deps.DB.WithContext(ctx).Create(entry).Error; createErr != nil {
		release(ctx, leases)
		return types.ProjectBackup{}, fmt.Errorf("failed to save project backup: %w", createErr)
	}
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
		s.watch.Watch(background, entry.ID, activityID)
		releaseHeld(background)
	}()
	return entry.DTO(DTOOptions{ActivityID: &activityID}), nil
}

// resolvePlan validates the request; by default it captures every existing volume that keeps its data in Docker.
func (s *Service) resolvePlan(ctx context.Context, project types.Project, request types.CreateProjectBackupRequest) (plan, error) {
	selected, s3DestinationID, err := backup.ResolveDestination(ctx, backup.DestinationRequest{
		Class:           common.ErrBadRequest,
		S3Destinations:  s.deps.S3Destinations,
		Domain:          "project",
		Destination:     string(request.Destination),
		S3DestinationID: request.S3DestinationID,
		Default:         backup.DestinationOptions{Local: true, Remote: false},
	})
	if err != nil {
		return plan{}, err
	}
	if project.ComposeName == "" {
		return plan{}, common.Classify(common.ErrValidation, fmt.Errorf("the compose files of project %s cannot be loaded; fix them before backing it up", project.Name))
	}
	planned, err := s.inspectVolumes(ctx, project, request.Volumes)
	if err != nil {
		return plan{}, err
	}
	return plan{ //exhaustruct:enforce
		BackupID:        uuid.New().String(),
		ProjectID:       project.ID,
		S3DestinationID: s3DestinationID,
		Volumes:         planned,
		LocalEnabled:    selected.Local,
		S3Enabled:       selected.Remote,
		StopContainers:  *cmp.Or(request.StopContainers, new(true)),
	}, nil
}

// inspectVolumes includes the requested volumes, or by default every one that can be backed up.
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
			return nil, common.Classify(common.ErrValidation, fmt.Errorf("volume %s holds Arcane's backups, so it cannot be backed up", volume.Key))
		case wanted && requested != nil && volume.Missing:
			return nil, common.Classify(common.ErrValidation, fmt.Errorf("volume %s has not been created yet, so it holds no data to back up", volume.Key))
		case wanted && requested != nil && !volume.Included:
			return nil, common.Classify(common.ErrValidation, fmt.Errorf("volume %s keeps its data outside Docker (%s), so it cannot be backed up", volume.Key, volume.Kind))
		}
		inspected = append(inspected, types.ProjectVolume{ //exhaustruct:enforce
			Labels: volume.Labels,
			// A restore recreates a missing external volume with its own labels, not Compose's.
			DockerLabels: kit.Ternary(volume.Kind == types.ProjectVolumeExternal && volume.Included, dockerVolumes[volume.Name].Labels, nil),
			Key:          volume.Key,
			Name:         volume.Name,
			Kind:         volume.Kind,
			Driver:       volume.Driver,
			Archive:      volume.Archive,
			Included:     volume.Included && wanted,
			Missing:      volume.Missing,
			InUse:        volume.InUse,
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

// run executes an accepted backup, or resumes one whose checkpoint an earlier attempt recorded.
func (s *Service) run(ctx context.Context, t flow.Task) (_ any, err error) {
	var input runInput
	if decodeErr := t.Payload(&input); decodeErr != nil {
		return nil, decodeErr
	}
	defer func() {
		err = backup.SettleFailure(ctx, err, func(ctx context.Context, message string) error {
			return s.fail(ctx, input.Plan.BackupID, message)
		})
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

// resume finishes a backup an earlier attempt left running; like a volume backup, one that stopped containers fails. A
// scheduled backup has no requester.
func (s *Service) resume(ctx context.Context, previous types.SchedulerRun, requester *types.BackupRequester, recorded plan) error {
	// The admissions died with the process, so a restore may own the project until they are retaken.
	leases, err := s.admit(ctx, admissions(recorded.ProjectID, recorded.volumeNames()), "")
	if err != nil {
		return fmt.Errorf("%w: %w", actor.ErrJobRejected, err)
	}
	defer release(context.WithoutCancel(ctx), leases)
	entry, err := s.load(ctx, recorded.BackupID)
	if err != nil {
		return err
	}
	switch entry.Status {
	case types.VolumeBackupStatusFailed:
		return fmt.Errorf("%w: %s", backup.ErrBackupSettled, entry.Error)
	case types.VolumeBackupStatusSucceeded:
		return nil
	}
	if recorded.StopContainers {
		return backup.FailStoppedRun(ctx, func(ctx context.Context, message string) error {
			return s.fail(ctx, entry.ID, message)
		})
	}
	if requester != nil {
		if authorizeErr := s.deps.Engine.Authorize(ctx, *requester); authorizeErr != nil {
			return authorizeErr
		}
	}
	dockerClient, err := s.deps.Docker.GetClient(ctx)
	if err != nil {
		return err
	}
	if adoptErr := s.deps.Repository.AdoptRunSnapshots(ctx, dockerClient, snapshotRun(entry, recorded), previous); adoptErr != nil {
		return adoptErr
	}
	if saveErr := s.deps.DB.WithContext(ctx).Save(entry).Error; saveErr != nil {
		return saveErr
	}
	err = s.execute(ctx, entry, recorded)
	if ctx.Err() != nil {
		return ctx.Err()
	}
	return s.complete(ctx, entry, err)
}

func (s *Service) repository(ctx context.Context, dockerClient *client.Client, s3DestinationID string, remote bool) (backup.Repository, error) {
	if remote {
		return s.deps.Repository.Remote(ctx, s3DestinationID, "")
	}
	return s.deps.Repository.Local(ctx, dockerClient, backup.LocalRepositoryOptions{})
}

// execute takes the snapshot unless it already exists, then replicates a local snapshot to S3 when both are enabled.
func (s *Service) execute(ctx context.Context, entry *ProjectBackup, p plan) (err error) {
	defer concurrency.RecoverToError(&err, "project backup")
	dockerClient, err := s.deps.Docker.GetClient(ctx)
	if err != nil {
		return err
	}
	return s.deps.Repository.WriteRunSnapshots(ctx, dockerClient, snapshotRun(entry, p), backup.SnapshotWrite{
		Capture: func(ctx context.Context, repository backup.Repository, password string) (backup.Snapshot, error) {
			return s.capture(ctx, entry, p, repository, password)
		},
		Save: s.saveSnapshots(entry),
	})
}

// saveSnapshots saves what a snapshot recorded on the backup, leaving its status to whatever settles the backup.
func (s *Service) saveSnapshots(entry *ProjectBackup) func(ctx context.Context) error {
	return func(ctx context.Context) error {
		return s.deps.DB.WithContext(ctx).Model(entry).Select("local_snapshot_id", "remote_snapshot_id", "size", "stopped_containers").Updates(entry).Error
	}
}

// capture writes the manifest, stops the project when asked, and takes one snapshot of its files and volumes.
func (s *Service) capture(ctx context.Context, entry *ProjectBackup, p plan, repository backup.Repository, password string) (backup.Snapshot, error) {
	// Like a consistent export, hold the project lock so nothing deploys or edits the project while it is stopped.
	if p.StopContainers {
		unlockProject, lockErr := s.deps.LockProject(ctx, "back up project", entry.ProjectID)
		if lockErr != nil {
			return backup.Snapshot{}, lockErr
		}
		defer unlockProject()
	}
	unlock, err := s.deps.LockVolumes(ctx, p.mountedVolumeNames())
	if err != nil {
		return backup.Snapshot{}, err
	}
	defer unlock()
	return s.snapshot(ctx, entry, p, repository, password)
}

// snapshot takes the snapshot capture describes into the repository; the caller holds the volume locks, and the project
// lock when it stops containers. A snapshot rejected by the check after the copy is returned with the error, so it is
// recorded and removed with the backup.
func (s *Service) snapshot(ctx context.Context, entry *ProjectBackup, p plan, repository backup.Repository, password string) (backup.Snapshot, error) {
	dockerClient, err := s.deps.Docker.GetClient(ctx)
	if err != nil {
		return backup.Snapshot{}, err
	}
	project, err := s.deps.Project(ctx, entry.ProjectID, projectDetails)
	if err != nil {
		return backup.Snapshot{}, err
	}
	// The plan's locks and leases cover the volumes found when the backup was queued.
	if planErr := s.checkPlanVolumes(ctx, project, p); planErr != nil {
		return backup.Snapshot{}, planErr
	}
	// Containers outside the project keep running, so a volume they mount may change while it is captured. A
	// restarting container is left out of the running count but can still write its volumes.
	active, inUse, err := s.containerUse(ctx, project.ComposeName)
	if err != nil {
		return backup.Snapshot{}, err
	}
	p.Volumes = markInUse(p.Volumes, inUse)
	// The snapshot copies the project directory, which holds only the link of a .env kept outside it.
	env, _, externalEnv, err := projects.ReadExternalEnvFile(ctx, project.Path)
	if err != nil {
		return backup.Snapshot{}, fmt.Errorf("read the .env file of project %s: %w", project.Name, err)
	}
	metaVolume, removeMeta, err := s.writeManifest(ctx, dockerClient, backupManifest(project, p, externalEnv), env)
	if err != nil {
		return backup.Snapshot{}, err
	}
	defer removeMeta()
	projectMounts, err := projectSourceMounts(ctx, dockerClient, backup.SourceMountOptions{ContainerPath: project.Path, Target: path.Join(backup.SnapshotRoot, "project")})
	if err != nil {
		return backup.Snapshot{}, err
	}
	// The repository is waited for before the project stops, so it stays stopped only while this snapshot is copied.
	held, releaseRepository, err := s.deps.Engine.HoldRepository(ctx, repository.ID)
	if err != nil {
		return backup.Snapshot{}, err
	}
	defer releaseRepository()
	input := snapshotInput(projectMounts, metaVolume, entry, p)
	capture := backup.CapturePlan{
		Inspect: s.deps.Volume,
		Stop:    nil,
		// Recorded now, so a run resumed after a shutdown can't complete the rejected snapshot.
		Rejected: func(ctx context.Context, err error) error { return s.fail(ctx, entry.ID, err.Error()) },
		Mounts:   input.Mounts,
		Volumes:  p.volumeNames(),
	}
	if p.StopContainers && len(active) > 0 {
		capture.Stop = func(ctx context.Context) (func(context.Context) error, error) {
			services := projects.ContainerServices(active)
			start := func(ctx context.Context) error {
				return s.deps.StartProject(ctx, entry.ProjectID, services, types.SystemUser)
			}
			entry.StoppedContainers = true
			return start, s.stopProject(ctx, entry.ProjectID, project.ComposeName, active, types.SystemUser)
		}
	}
	var snapshot backup.Snapshot
	err = backup.Capture(held, capture, func(ctx context.Context, mounts []mount.Mount) error {
		input.Mounts = mounts
		created, snapshotErr := s.deps.Engine.CreateSnapshot(ctx, dockerClient, repository, password, project.Name, input)
		releaseRepository()
		if snapshotErr != nil {
			return fmt.Errorf("failed to create the project snapshot: %w", snapshotErr)
		}
		snapshot = created
		return nil
	})
	return snapshot, err
}

// containerUse lists the Compose project's running and restarting containers, the ones a stop stops, and the volumes
// running containers outside it mount. Arcane's own containers are never stopped, so they count as outside.
func (s *Service) containerUse(ctx context.Context, composeName string) (active []container.Summary, inUse map[string]bool, err error) {
	dockerClient, err := s.deps.Docker.GetClient(ctx)
	if err != nil {
		return nil, nil, err
	}
	running, err := volumes.ListRunningContainers(ctx, dockerClient)
	if err != nil {
		return nil, nil, err
	}
	active, inUse = projects.ProjectContainerUse(composeName, running)
	return active, inUse, nil
}

// stopProject stops the project's active containers, then refuses any still running: Compose stops only the services
// its files name, so a one-off run or a removed service keeps running.
func (s *Service) stopProject(ctx context.Context, projectID, composeName string, active []container.Summary, user types.Actor) error {
	if err := s.deps.StopProject(ctx, projectID, projects.ContainerServices(active), user); err != nil {
		return err
	}
	still, _, err := s.containerUse(ctx, composeName)
	if err != nil {
		return err
	}
	if len(still) > 0 {
		return common.Classify(common.ErrConflict, fmt.Errorf(
			"%d container(s) of project %s are still running after its services stopped, such as one-off runs or removed services; nothing was written or copied",
			len(still), composeName,
		))
	}
	return nil
}

// checkPlanVolumes fails when the project's volumes changed after the backup was queued, for example by a compose edit
// and deploy, so the snapshot never pairs new compose files with old volume data. They are inspected as the plan's
// were, since Docker's driver options also decide a volume's kind.
func (s *Service) checkPlanVolumes(ctx context.Context, project types.Project, p plan) error {
	current, err := s.inspectVolumes(ctx, project, nil)
	if err != nil {
		return err
	}
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

func (s *Service) record(ctx context.Context, p plan) error {
	checkpoint, err := json.Marshal(p)
	if err != nil {
		return err
	}
	return jobcontext.Progress(ctx, types.TargetOutcome{ResourceType: targetType, ID: p.ProjectID, Status: types.RunStatusRunning, RecoveryData: checkpoint})
}

// writeManifest stores the manifest, and any external .env, in a new volume. The holder mounts only that volume and
// never starts, because Docker before 29.9 mishandles copies into stopped containers with other mounts (#4870).
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

// handedBack reports whether a failed scheduled attempt leaves its backup running for a retry: a shutdown cancel or a
// busy admission, but not a user cancel.
func handedBack(ctx context.Context, err error) bool {
	if _, resumable := jobcontext.Run(ctx); resumable && errors.Is(context.Cause(ctx), context.Canceled) {
		return true
	}
	return ctx.Err() == nil && errors.Is(err, actor.ErrJobRejected)
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

// ReconcileInterruptedBackups fails backups left running by the previous process and watches those saved workflows resume.
func (s *Service) ReconcileInterruptedBackups(ctx context.Context, protectedIDs ...string) error {
	query := s.deps.DB.WithContext(ctx).Model(&ProjectBackup{}).Where("status = ?", types.VolumeBackupStatusRunning)
	if len(protectedIDs) > 0 {
		query = query.Where("id NOT IN ?", protectedIDs)
	}
	if err := query.Updates(map[string]any{"status": types.VolumeBackupStatusFailed, "error": "Backup interrupted by Arcane restart"}).Error; err != nil {
		return err
	}
	return s.watch.WatchAccepted(ctx)
}

func (s *Service) List(ctx context.Context, projectID string, params pagination.Params) ([]types.ProjectBackup, pagination.Response, error) {
	query := s.deps.DB.WithContext(ctx).Model(&ProjectBackup{}).Where("project_id = ?", projectID).Order("created_at DESC")
	var entries []ProjectBackup
	page, err := pagination.PaginateAndSortDB(pagination.QueryParams{Params: params}, query, &entries)
	if err != nil {
		return nil, pagination.Response{}, err
	}
	describe := s.deps.Repository.DescribeRemote(ctx)
	backups := make([]types.ProjectBackup, 0, len(entries))
	for i := range entries {
		name, available := describe(entries[i].S3DestinationID, "", entries[i].RemoteSnapshotID)
		backups = append(backups, entries[i].DTO(DTOOptions{S3DestinationName: name, RemoteAvailable: available}))
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

// removeAll forgets the backups' snapshots once per repository and deletes the rows whose snapshots are gone; the
// caller holds each project's admission.
func (s *Service) removeAll(ctx context.Context, entries []*ProjectBackup) error {
	if len(entries) == 0 {
		return nil
	}
	dockerClient, err := s.deps.Docker.GetClient(ctx)
	if err != nil {
		return err
	}
	snapshots := make([]backup.RunSnapshots, len(entries))
	for index, entry := range entries {
		snapshots[index] = entry.snapshots()
	}
	removeErr := s.deps.Repository.ForgetRunSnapshots(ctx, dockerClient, snapshots, true)
	var deletedIDs []string
	for _, entry := range entries {
		if entry.LocalSnapshotID == "" && entry.RemoteSnapshotID == "" {
			deletedIDs = append(deletedIDs, entry.ID)
			continue
		}
		entry.Destination = backup.Destination(backup.DestinationOptions{Local: entry.LocalSnapshotID != "", Remote: entry.RemoteSnapshotID != ""})
		removeErr = errors.Join(removeErr, s.deps.DB.WithContext(ctx).Save(entry).Error)
	}
	for batch := range slices.Chunk(deletedIDs, backup.DeletionBatchSize) {
		removeErr = errors.Join(removeErr, s.deps.DB.WithContext(ctx).Where("id IN ?", batch).Delete(&ProjectBackup{}).Error)
	}
	return removeErr
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

// Policies lists the project's backup policies with the latest backup each one took.
func (s *Service) Policies(ctx context.Context, projectID string) (types.ProjectBackupPolicyCollection, error) {
	policies, err := s.deps.Policies.List(ctx, backup.PolicyProject, projectID)
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

// UpdatePolicies replaces the project's backup policies, schedules the enabled ones and unschedules the rest.
func (s *Service) UpdatePolicies(ctx context.Context, projectID string, updates []types.UpdateProjectBackupPolicy) (types.ProjectBackupPolicyCollection, error) {
	project, err := s.deps.Project(ctx, projectID, projectDetails)
	if err != nil {
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
			// No selection follows the project's volumes; a selection follows the rules of a manual backup.
			if volumes != nil {
				if _, inspectErr := s.inspectVolumes(ctx, project, volumes); inspectErr != nil {
					return inspectErr
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
	return s.Policies(ctx, projectID)
}

// RegisterJobsOnStartup schedules every enabled project backup policy and global project backup policy.
func (s *Service) RegisterJobsOnStartup(ctx context.Context) {
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

// RemovePolicies unschedules and deletes a deleted project's backup policies, which leaves their backups unmanaged.
func (s *Service) RemovePolicies(ctx context.Context, projectID string) {
	policies, err := s.deps.Policies.Remove(ctx, backup.PolicyProject, projectID)
	if err != nil {
		slog.WarnContext(ctx, "Failed to delete the backup policies of a deleted project", "projectId", projectID, "error", err)
		return
	}
	for i := range policies {
		s.jobs.Unregister(ctx, policies[i].ID)
	}
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

// runScheduled resumes an interrupted backup of the policy, or waits for the project's admission and its volumes'
// leases and backs the project up with the policy as it stands once admitted.
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
	admitted, err := s.admitScheduled(ctx, policy.SubjectID, policy.ID, func(ctx context.Context) (types.CreateProjectBackupRequest, bool, error) {
		return s.scheduledPolicy(ctx, policy.ID)
	})
	switch {
	case errors.Is(err, errPolicyGone):
		return canceled, nil
	case errors.Is(err, errRemoteOnlyDisabled):
		return types.SchedulerOutcome{Status: types.RunStatusNeedsAttention, Message: backup.RemoteDisabledMessage}, nil
	case err != nil:
		return nil, err
	}
	defer release(context.WithoutCancel(ctx), admitted.leases)
	backupErr := s.backUpScheduled(ctx, admitted)
	if backupErr != nil {
		slog.ErrorContext(ctx, "Scheduled project backup failed", "projectId", policy.SubjectID, "policyId", policy.ID, "error", backupErr)
	}
	// Retention runs under the project's admission, so it never deletes a backup a restore is reading. A failed run can
	// leave a local snapshot too, so it counts against retention as well.
	if retainErr := s.retain(ctx, policy, policy.ID); retainErr != nil {
		slog.ErrorContext(ctx, "Project backup retention failed", "projectId", policy.SubjectID, "policyId", policy.ID, "error", retainErr)
	}
	if backupErr != nil {
		return nil, backupErr
	}
	if admitted.remoteDisabled {
		return types.SchedulerOutcome{Status: types.RunStatusPartial, Message: backup.RemoteDisabledMessage}, nil
	}
	return types.SchedulerOutcome{Status: types.RunStatusSucceeded, Message: "Scheduled project backup created successfully"}, nil
}

// resumeScheduled finishes the backup an interrupted attempt checkpointed; it reports false when there is none, or when
// that backup already failed and a new one should run.
func (s *Service) resumeScheduled(ctx context.Context, previous types.SchedulerRun) (types.SchedulerOutcome, bool, error) {
	index := slices.IndexFunc(previous.Outcome.Targets, func(target types.TargetOutcome) bool {
		return target.ResourceType == targetType && len(target.RecoveryData) > 0
	})
	if index < 0 {
		return types.SchedulerOutcome{}, false, nil
	}
	var recorded plan
	if err := json.Unmarshal(previous.Outcome.Targets[index].RecoveryData, &recorded); err != nil {
		return types.SchedulerOutcome{}, true, err
	}
	err := s.resume(ctx, previous, nil, recorded)
	switch {
	case errors.Is(err, backup.ErrBackupSettled):
		return types.SchedulerOutcome{}, false, nil
	case err != nil && !handedBack(ctx, err):
		return types.SchedulerOutcome{}, true, errors.Join(err, s.fail(context.WithoutCancel(ctx), recorded.BackupID, err.Error()))
	case err != nil:
		return types.SchedulerOutcome{}, true, err
	}
	return types.SchedulerOutcome{Status: types.RunStatusSucceeded, Message: "Scheduled project backup created successfully"}, true, nil
}

// admitScheduled waits for the project's admission, reads the policy through read and plans the backup under it, then
// waits for the leases of the volumes the plan captures, in name order so two waiting backups never hold one each of the
// other's. The backup is recorded under policyID.
func (s *Service) admitScheduled(
	ctx context.Context, projectID, policyID string, read func(context.Context) (types.CreateProjectBackupRequest, bool, error),
) (admittedRun, error) {
	projectLease, err := s.deps.Engine.AwaitRun(ctx, admissionScope, projectID)
	if err != nil {
		return admittedRun{}, err
	}
	leases := []*runs.Lease{projectLease}
	fail := func(err error) (admittedRun, error) {
		release(context.WithoutCancel(ctx), leases)
		return admittedRun{}, err
	}
	request, remoteDisabled, err := read(ctx)
	if err != nil {
		return fail(err)
	}
	project, err := s.deps.Project(ctx, projectID, projectDetails)
	if err != nil {
		return fail(err)
	}
	p, err := s.resolvePlan(ctx, project, request)
	if err != nil {
		return fail(err)
	}
	for _, name := range p.volumeNames() {
		lease, leaseErr := s.deps.Engine.AwaitRun(ctx, backup.VolumeAdmissionScope, name)
		if leaseErr != nil {
			return fail(leaseErr)
		}
		leases = append(leases, lease)
	}
	return admittedRun{policyID: policyID, project: project, plan: p, leases: leases, remoteDisabled: remoteDisabled}, nil
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

// checkRemote checks a scheduled policy's S3 repository. A missing repository disables S3, or the policy when S3 is its
// only destination, so the backup never recreates it.
func (s *Service) checkRemote(ctx context.Context, policy *backup.Policy) (bool, error) {
	var remoteErr error
	if policy.S3Enabled {
		remoteErr = backup.CheckScheduledRemote(ctx, s.deps.DB, s.deps.S3Destinations, backup.ScheduledRemoteRequest{
			DestinationID: policy.S3DestinationID,
			Root:          path.Join(backup.VolumeRoot, s.deps.Settings.GetSettingsConfig().InstanceID.Value),
			Tables:        backup.VolumeRepositoryTables,
		})
	}
	disabled, err := backup.DisableMissingRemote(remoteErr, policy.LocalEnabled, &policy.S3Enabled, &policy.Enabled, func(field string) (bool, error) {
		return s.deps.Policies.DisableRemote(ctx, policy, field)
	})
	switch {
	case err != nil:
		return false, err
	// The policy changed while its repository was checked, so the guarded save matched nothing; the next run checks it.
	case !disabled && errors.Is(remoteErr, backup.ErrRemoteRepositoryMissing):
		return false, remoteErr
	case !disabled:
		return false, nil
	}
	// Rescheduled before the backup runs, so a later edit of the policy is not undone afterwards.
	s.reschedule(ctx, policy)
	if !policy.LocalEnabled {
		return true, errRemoteOnlyDisabled
	}
	return true, nil
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
		Destination:     backup.Destination(backup.DestinationOptions{Local: p.LocalEnabled, Remote: p.S3Enabled}),
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
	p.BackupID = entry.ID
	if recordErr := s.record(ctx, p); recordErr != nil {
		return recordErr
	}
	err = s.execute(ctx, entry, p)
	if ctx.Err() != nil {
		return ctx.Err()
	}
	return s.complete(ctx, entry, err)
}

// retain deletes the series' backups beyond the policy's newest RetentionCount; the caller holds the project's admission.
// The policy is read again, since an edit or delete made while the backup ran must keep the backups it asks to keep. A
// global policy keeps its RetentionCount per project, in the project's series.
func (s *Service) retain(ctx context.Context, policy *backup.Policy, seriesID string) error {
	current, err := s.deps.Policies.Get(ctx, backup.PolicyProject, policy.SubjectID, policy.ID)
	if err != nil || current == nil || current.RetentionCount <= 0 {
		return err
	}
	expired, err := backup.ExpiredRunIDs(ctx, s.deps.DB, backupsTable, seriesID, current.RetentionCount)
	if err != nil {
		return err
	}
	entries, err := s.backupsByID(ctx, expired)
	if err != nil {
		return err
	}
	return s.removeAll(ctx, entries)
}

// PruneUnmanagedBackups deletes safety backups and the backups of deleted policies once they have been unmanaged for
// unmanagedBackupRetentionDays, under each project's admission; a busy project's backups wait for the next run.
func (s *Service) PruneUnmanagedBackups(ctx context.Context) error {
	days := s.deps.Settings.GetIntSetting(ctx, "unmanagedBackupRetentionDays")
	if days <= 0 {
		return nil
	}
	expired, err := s.expiredUnmanaged(ctx, types.UnmanagedBackupSweep{Now: time.Now(), Retention: time.Duration(days) * 24 * time.Hour})
	if err != nil {
		return err
	}
	entries, err := s.backupsByID(ctx, expired)
	if err != nil {
		return err
	}
	byProject := make(map[string][]*ProjectBackup)
	for _, entry := range entries {
		byProject[entry.ProjectID] = append(byProject[entry.ProjectID], entry)
	}
	var pruneErr error
	var held []*ProjectBackup
	var releases []func(context.Context)
	defer func() {
		for _, releaseBackups := range releases {
			releaseBackups(context.WithoutCancel(ctx))
		}
	}()
	for projectID, group := range byProject {
		releaseBackups, holdErr := s.HoldBackups(ctx, projectID)
		if holdErr != nil {
			if !errors.Is(holdErr, errAlreadyRunning) {
				pruneErr = errors.Join(pruneErr, holdErr)
			}
			continue
		}
		releases = append(releases, releaseBackups)
		held = append(held, group...)
	}
	return errors.Join(pruneErr, s.removeAll(ctx, held))
}

// expiredUnmanaged marks scheduled backups whose policy no longer exists as unmanaged and returns those, and safety
// backups, that expired; manual backups and backups of existing policies are never unmanaged. A global policy manages
// a project's series while it exists and still selects that project.
func (s *Service) expiredUnmanaged(ctx context.Context, sweep types.UnmanagedBackupSweep) ([]string, error) {
	var candidates []ProjectBackup
	if err := s.deps.DB.WithContext(ctx).Model(&ProjectBackup{}).Select("id", "policy_id").
		Where("status <> ? AND trigger = ? AND policy_id <> ''", types.VolumeBackupStatusRunning, types.VolumeBackupTriggerScheduled).
		Find(&candidates).Error; err != nil {
		return nil, fmt.Errorf("failed to load unmanaged project backups: %w", err)
	}
	references, err := s.deps.ListProjects(ctx)
	if err != nil {
		return nil, err
	}
	managed, err := s.managedSeries(ctx, references)
	if err != nil {
		return nil, err
	}
	var orphaned []string
	for _, candidate := range candidates {
		if !managed[candidate.PolicyID] {
			orphaned = append(orphaned, candidate.ID)
		}
	}
	return backup.ExpiredUnmanagedRunIDs(ctx, s.deps.DB, backupsTable, orphaned, sweep)
}

// managedSeries returns the policy ID of every series a policy manages: each project policy, and one series for each
// project a global policy selects.
func (s *Service) managedSeries(ctx context.Context, references []types.ProjectReference) (map[string]bool, error) {
	policyIDs, err := s.deps.Policies.IDs(ctx, backup.PolicyProject)
	if err != nil {
		return nil, err
	}
	globals, err := s.deps.Policies.List(ctx, backup.PolicyProject, "")
	if err != nil {
		return nil, err
	}
	series := make(map[string]bool, len(policyIDs))
	for _, policyID := range policyIDs {
		series[policyID] = true
	}
	for _, policy := range globals {
		for _, project := range selectProjects(policy, references) {
			series[types.GlobalProjectPolicyPrefix+policy.ID+":"+project.ID] = true
		}
	}
	return series, nil
}

// DiscoverBackups records the project backups whose snapshots are in the selected repositories but whose rows are
// missing, as after a system restore; it never deletes anything. On S3 only this instance's root is searched.
func (s *Service) DiscoverBackups(ctx context.Context, opts DiscoverOptions) (int, error) {
	dockerClient, err := s.deps.Docker.GetClient(ctx)
	if err != nil {
		return 0, err
	}
	var repositories []backup.Repository
	var destinationIDs []string
	var listErr error
	if opts.Local {
		local, localErr := s.deps.Repository.Local(ctx, dockerClient, backup.LocalRepositoryOptions{ReadOnly: true})
		if localErr != nil {
			listErr = fmt.Errorf("open the local repository: %w", localErr)
		} else {
			repositories, destinationIDs = append(repositories, local), append(destinationIDs, "")
		}
	}
	root := path.Join(backup.VolumeRoot, s.deps.Settings.GetSettingsConfig().InstanceID.Value)
	for _, destinationID := range opts.S3DestinationIDs {
		observation, checkErr := backup.CheckRemoteRepository(ctx, s.deps.S3Destinations, destinationID, root)
		if checkErr != nil {
			listErr = errors.Join(listErr, fmt.Errorf("check S3 destination %s: %w", destinationID, checkErr))
			continue
		}
		if !observation.Available {
			continue
		}
		remote, remoteErr := s.deps.Repository.Remote(ctx, destinationID, "")
		if remoteErr != nil {
			listErr = errors.Join(listErr, fmt.Errorf("open S3 destination %s: %w", destinationID, remoteErr))
			continue
		}
		repositories, destinationIDs = append(repositories, remote), append(destinationIDs, destinationID)
	}
	var found []foundSnapshot
	for index, repository := range repositories {
		password, passwordErr := s.deps.Repository.Password(ctx, dockerClient, repository)
		if passwordErr != nil {
			listErr = errors.Join(listErr, fmt.Errorf("read the password for %s: %w", repository.ID, passwordErr))
			continue
		}
		listed, snapshotsErr := s.deps.Engine.ListSnapshots(ctx, dockerClient, repository, password)
		if errors.Is(snapshotsErr, backup.ErrRepositoryMissing) {
			continue
		}
		if snapshotsErr != nil {
			listErr = errors.Join(listErr, fmt.Errorf("list project snapshots in %s: %w", repository.ID, snapshotsErr))
			continue
		}
		for _, snapshot := range listed {
			if slices.Contains(snapshot.Tags, backup.ProjectSnapshotTag) {
				found = append(found, foundSnapshot{
					snapshot:        snapshot,
					s3DestinationID: destinationIDs[index],
					source:          snapshotSource{password: password, id: snapshot.ID, repository: repository},
				})
			}
		}
	}
	recorded, err := s.importSnapshots(ctx, found, func(ctx context.Context, snapshot foundSnapshot) (types.ProjectManifest, error) {
		return s.readManifest(ctx, dockerClient, snapshot.source)
	})
	if err = errors.Join(listErr, err); err != nil && recorded > 0 {
		return recorded, fmt.Errorf("recorded %d project backups, but discovery was incomplete: %w", recorded, err)
	}
	return recorded, err
}

// DiscoverAfterRestore runs the discovery a system restore asked for, after syncing the restored projects with their
// folders. Only it searches the local repository, so the request is cleared only on success and a later start retries.
func (s *Service) DiscoverAfterRestore(ctx context.Context) error {
	if !s.deps.Settings.GetBoolSetting(ctx, discoveryPendingKey) {
		return nil
	}
	if err := s.deps.SyncProjects(ctx); err != nil {
		return fmt.Errorf("sync projects before discovering their backups: %w", err)
	}
	destinations, err := s.deps.S3Destinations.ListAllS3Destinations(ctx)
	if err != nil {
		return err
	}
	destinationIDs := make([]string, 0, len(destinations))
	for _, destination := range destinations {
		destinationIDs = append(destinationIDs, destination.ID)
	}
	count, err := s.DiscoverBackups(ctx, DiscoverOptions{S3DestinationIDs: destinationIDs, Local: true})
	slog.InfoContext(ctx, "Discovered project backups after a system restore", "recorded", count)
	if err != nil {
		return err
	}
	return s.deps.Settings.UpdateSetting(ctx, discoveryPendingKey, "false")
}

// importSnapshots records a row, read from the backup's manifest, for each found backup whose row is missing, adds the
// copies found to a succeeded row missing them, and gives a failed row that recorded no snapshot the ones found. A
// backup keeps its policy only while that policy manages it, so the unmanaged sweep never deletes it.
func (s *Service) importSnapshots(
	ctx context.Context,
	found []foundSnapshot,
	readManifest func(context.Context, foundSnapshot) (types.ProjectManifest, error),
) (int, error) {
	groups := make(map[string][]foundSnapshot)
	for _, snapshot := range found {
		if backupID := snapshotTag(snapshot.snapshot.Tags, backup.RunSnapshotTag("")); backupID != "" {
			groups[backupID] = append(groups[backupID], snapshot)
		}
	}
	if len(groups) == 0 {
		return 0, nil
	}
	backupIDs := slices.Sorted(maps.Keys(groups))
	var stored []ProjectBackup
	if err := s.deps.DB.WithContext(ctx).Select("id", "status", "local_snapshot_id", "remote_snapshot_id").Where("id IN ?", backupIDs).Find(&stored).Error; err != nil {
		return 0, err
	}
	existing := make(map[string]ProjectBackup, len(stored))
	for _, entry := range stored {
		existing[entry.ID] = entry
	}
	references, err := s.deps.ListProjects(ctx)
	if err != nil {
		return 0, err
	}
	managed, err := s.managedSeries(ctx, references)
	if err != nil {
		return 0, err
	}
	recorded := 0
	var importErr error
	for _, backupID := range backupIDs {
		snapshots := groups[backupID]
		slices.SortFunc(snapshots, func(a, b foundSnapshot) int {
			return cmp.Or(strings.Compare(a.s3DestinationID, b.s3DestinationID), a.snapshot.Time.Compare(b.snapshot.Time))
		})
		local := slices.IndexFunc(snapshots, func(snapshot foundSnapshot) bool { return snapshot.s3DestinationID == "" })
		remote := slices.IndexFunc(snapshots, func(snapshot foundSnapshot) bool { return snapshot.s3DestinationID != "" })
		var localSnapshot, remoteSnapshot foundSnapshot
		if local >= 0 {
			localSnapshot = snapshots[local]
		}
		if remote >= 0 {
			remoteSnapshot = snapshots[remote]
		}
		largest := slices.MaxFunc(snapshots, func(a, b foundSnapshot) int {
			return cmp.Compare(a.snapshot.Summary.TotalBytesProcessed, b.snapshot.Summary.TotalBytesProcessed)
		})
		if entry, recordedBefore := existing[backupID]; recordedBefore {
			filled, fillErr := s.addFoundCopies(ctx, entry, localSnapshot, remoteSnapshot, largest.snapshot.Summary.TotalBytesProcessed)
			importErr = errors.Join(importErr, fillErr)
			recorded += int(filled)
			continue
		}
		first := snapshots[kit.Ternary(local >= 0, local, remote)]
		projectID := snapshotTag(first.snapshot.Tags, projectTagPrefix)
		// A project found again under a new ID is matched by its name, when exactly one project has it.
		if !slices.ContainsFunc(references, func(project types.ProjectReference) bool { return project.ID == projectID }) {
			named := slices.DeleteFunc(slices.Clone(references), func(project types.ProjectReference) bool { return project.Name != first.snapshot.Label })
			if len(named) == 1 {
				projectID = named[0].ID
			}
		}
		if projectID == "" {
			continue
		}
		trigger, policyID := snapshotTag(first.snapshot.Tags, triggerTagPrefix), snapshotTag(first.snapshot.Tags, policyTagPrefix)
		if trigger != types.VolumeBackupTriggerScheduled || !managed[policyID] {
			trigger, policyID = types.VolumeBackupTriggerManual, ""
		}
		manifest, manifestErr := readManifest(ctx, first)
		if manifestErr != nil {
			importErr = errors.Join(importErr, fmt.Errorf("read the manifest of project backup %s: %w", backupID, manifestErr))
			continue
		}
		createdAt := slices.MinFunc(snapshots, func(a, b foundSnapshot) int { return a.snapshot.Time.Compare(b.snapshot.Time) }).snapshot.Time
		entry := ProjectBackup{
			BaseModel:        database.BaseModel{ID: backupID, CreatedAt: createdAt},
			CompletedAt:      new(createdAt),
			ProjectID:        projectID,
			ProjectName:      cmp.Or(first.snapshot.Label, manifest.Project.Name),
			ComposeName:      manifest.Project.ComposeName,
			Status:           types.VolumeBackupStatusSucceeded,
			Trigger:          trigger,
			Destination:      backup.Destination(backup.DestinationOptions{Local: local >= 0, Remote: remote >= 0}),
			LocalSnapshotID:  localSnapshot.snapshot.ID,
			RemoteSnapshotID: remoteSnapshot.snapshot.ID,
			S3DestinationID:  remoteSnapshot.s3DestinationID,
			PolicyID:         policyID,
			Volumes:          backupVolumes(manifest.Volumes),
			Size:             largest.snapshot.Summary.TotalBytesProcessed,
		}
		if createErr := s.deps.DB.WithContext(ctx).Create(&entry).Error; createErr != nil {
			importErr = errors.Join(importErr, createErr)
			continue
		}
		recorded++
	}
	return recorded, importErr
}

// addFoundCopies adds the found local and remote snapshots, empty when not found, to a succeeded row missing them, and
// gives a failed row that recorded no snapshot the ones found. It returns the number of rows updated.
func (s *Service) addFoundCopies(ctx context.Context, entry ProjectBackup, localSnapshot, remoteSnapshot foundSnapshot, size int64) (int64, error) {
	hasLocal, hasRemote := localSnapshot.snapshot.ID != "", remoteSnapshot.snapshot.ID != ""
	// A backup a system backup caught running is failed on restore, though its snapshots finished.
	interrupted := entry.Status == types.VolumeBackupStatusFailed && entry.LocalSnapshotID == "" && entry.RemoteSnapshotID == ""
	addLocal, addRemote := hasLocal && entry.LocalSnapshotID == "", hasRemote && entry.RemoteSnapshotID == ""
	if (!interrupted && entry.Status != types.VolumeBackupStatusSucceeded) || (!addLocal && !addRemote) {
		return 0, nil
	}
	updates := map[string]any{"destination": backup.Destination(backup.DestinationOptions{Local: entry.LocalSnapshotID != "" || hasLocal, Remote: entry.RemoteSnapshotID != "" || hasRemote})}
	if addLocal || interrupted {
		updates["local_snapshot_id"] = localSnapshot.snapshot.ID
	}
	if addRemote || interrupted {
		updates["remote_snapshot_id"], updates["s3_destination_id"] = remoteSnapshot.snapshot.ID, remoteSnapshot.s3DestinationID
	}
	if interrupted {
		updates["status"], updates["error"], updates["size"] = types.VolumeBackupStatusSucceeded, "", size
	}
	// Only a row unchanged since it was read is updated, so no snapshot it records is overwritten.
	filled := s.deps.DB.WithContext(ctx).Model(&ProjectBackup{}).
		Where("id = ? AND status = ? AND COALESCE(local_snapshot_id, '') = ? AND COALESCE(remote_snapshot_id, '') = ?",
			entry.ID, entry.Status, entry.LocalSnapshotID, entry.RemoteSnapshotID).
		Updates(updates)
	return filled.RowsAffected, filled.Error
}

// snapshotTag returns the value of the snapshot's tag with the prefix, or empty.
func snapshotTag(tags []string, prefix string) string {
	for _, tag := range tags {
		if value, found := strings.CutPrefix(tag, prefix); found {
			return value
		}
	}
	return ""
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
		outcome := types.TargetOutcome{ResourceType: targetType, ID: projectID, Status: status, Message: message}
		finished[projectID] = outcome
		return jobcontext.Progress(ctx, outcome)
	}
	for _, target := range previous.Outcome.Targets {
		if target.ResourceType != targetType {
			continue
		}
		if target.Status != types.RunStatusRunning {
			finished[target.ID] = target
			continue
		}
		var recorded plan
		if decodeErr := json.Unmarshal(target.RecoveryData, &recorded); decodeErr != nil {
			return types.SchedulerOutcome{}, decodeErr
		}
		resumeErr := s.resume(ctx, previous, input.Requester, recorded)
		// A resume that ends for good, a user cancel included, fails its backup as a project policy's does.
		if resumeErr != nil && !errors.Is(resumeErr, backup.ErrBackupSettled) && !handedBack(ctx, resumeErr) {
			resumeErr = errors.Join(resumeErr, s.fail(context.WithoutCancel(ctx), recorded.BackupID, resumeErr.Error()))
		}
		switch {
		case ctx.Err() != nil:
			return types.SchedulerOutcome{}, ctx.Err()
		case errors.Is(resumeErr, actor.ErrJobRejected):
			return types.SchedulerOutcome{}, resumeErr
		case errors.Is(resumeErr, backup.ErrBackupSettled):
			// The backup failed, so a new one runs below, as for a project policy.
			continue
		}
		if reportErr := report(recorded.ProjectID, resumeErr); reportErr != nil {
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
		seriesID := types.GlobalProjectPolicyPrefix + input.PolicyID + ":" + project.ID
		admitted, backupErr := s.admitScheduled(ctx, project.ID, seriesID, func(ctx context.Context) (types.CreateProjectBackupRequest, bool, error) {
			return s.globalSettings(ctx, input, project.ID)
		})
		switch {
		case errors.Is(backupErr, errPolicyGone):
			return canceled, nil
		case errors.Is(backupErr, errRemoteOnlyDisabled):
			return types.SchedulerOutcome{Status: types.RunStatusNeedsAttention, Message: backup.RemoteDisabledMessage}, nil
		case backupErr == nil:
			remoteDisabled = remoteDisabled || admitted.remoteDisabled
			backupErr = s.backUpScheduled(ctx, admitted)
			// Retention runs under the project's admission, so it never deletes a backup a restore is reading. A failed
			// backup can leave a local snapshot too, so it counts against retention as well.
			if retainErr := s.retain(ctx, policy, seriesID); retainErr != nil {
				slog.ErrorContext(ctx, "Project backup retention failed", "projectId", project.ID, "policyId", seriesID, "error", retainErr)
			}
			release(context.WithoutCancel(ctx), admitted.leases)
		}
		// Shutdown interrupted this project; its checkpoint resumes it first.
		if ctx.Err() != nil {
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

// StartRestore checks an in-place restore against the backup, admits it, and hands it to the workflow, which checks the
// project under its locks; a busy project or volume fails fast. It returns the restore's activity ID.
func (s *Service) StartRestore(ctx context.Context, opts StartRestoreOptions) (string, error) {
	entry, err := s.restorable(ctx, opts.ProjectID, opts.BackupID)
	if err != nil {
		return "", err
	}
	project, err := s.deps.Project(ctx, opts.ProjectID, projectDetails)
	if err != nil {
		return "", err
	}
	// An archived project refuses changes until it is unarchived, as an edit does.
	if project.IsArchived {
		return "", common.ErrProjectArchived
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
	active, _, err := s.containerUse(ctx, project.ComposeName)
	if err != nil {
		return "", err
	}
	p := restorePlan{
		RestoreState: backup.RestoreState{Redeploy: opts.Request.Redeploy, MayStop: len(active) > 0},
		ID:           uuid.New().String(),
		BackupID:     entry.ID,
		ProjectID:    project.ID,
		Volumes:      targets,
		Files:        files,
	}
	if permErr := common.Classify(common.ErrForbidden, middleware.RequirePermissions(ctx, opts.EnvironmentID, p.extraPermissions())); permErr != nil {
		return "", permErr
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
// or deploying one it was asked to redeploy.
func (p restorePlan) extraPermissions() []string {
	switch {
	case p.MayStop:
		return []string{authz.PermProjectsDown, authz.PermProjectsDeploy}
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

// runRestore applies an accepted restore. One interrupted by a restart is not resumed; its safety backup undoes it.
func (s *Service) runRestore(ctx context.Context, t flow.Task) (any, error) {
	var input restoreRun
	if err := t.Payload(&input); err != nil {
		return nil, err
	}
	if t.Recovered() {
		return nil, errors.New("the restore was interrupted by a restart; restore the project's latest safety backup if it was left half restored")
	}
	unlock, err := s.lockRestore(ctx, input.Plan)
	if err != nil {
		return nil, err
	}
	defer unlock()
	// Rights can be revoked while a restore waits for its turn, so each one it needs is checked again before the first
	// change.
	for _, permission := range append([]string{input.Requester.Permission}, input.Plan.extraPermissions()...) {
		requester := types.BackupRequester{
			UserID: input.Requester.UserID, APIKeyID: input.Requester.APIKeyID, EnvironmentID: input.Requester.EnvironmentID, Permission: permission,
		}
		if authorizeErr := s.deps.Engine.Authorize(ctx, requester); authorizeErr != nil {
			return nil, authorizeErr
		}
	}
	p, err := s.prepareRestore(ctx, input.Plan)
	if err != nil {
		return nil, err
	}
	if applyErr := s.restoreSteps(input, t.ActivityID(), p).Apply(ctx, p.RestoreState); applyErr != nil {
		return nil, applyErr
	}
	return types.SchedulerOutcome{Status: types.RunStatusSucceeded, Message: "Project backup restored successfully"}, nil
}

// lockRestore takes the restore's admissions, then the project lock that deploy, down and edits take, then its volumes'
// locks; it takes over the leases StartRestore parked for it.
func (s *Service) lockRestore(ctx context.Context, p restorePlan) (func(), error) {
	leases, err := s.admit(ctx, admissions(p.ProjectID, p.volumeNames()), p.ID)
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
	locked := append(p.volumeNames(), plan{Volumes: projects.ClassifyProjectVolumes(project)}.mountedVolumeNames()...)
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

// prepareRestore checks the restore against the backup's manifest and the project as it is under the restore's locks.
func (s *Service) prepareRestore(ctx context.Context, p restorePlan) (restorePlan, error) {
	manifest, err := s.Manifest(ctx, p.ProjectID, p.BackupID)
	if err != nil {
		return restorePlan{}, err
	}
	project, err := s.deps.Project(ctx, p.ProjectID, projectDetails)
	if err != nil {
		return restorePlan{}, err
	}
	if project.IsArchived {
		return restorePlan{}, common.ErrProjectArchived
	}
	// Without its Compose name the project's containers can't be found, so none could be stopped.
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
	if checkErr := s.checkInPlace(ctx, project, p.Files); checkErr != nil {
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
		RestoreState: backup.RestoreState{Redeploy: p.Redeploy, Create: create, MayStop: p.MayStop, Running: len(active) > 0},
		ID:           p.ID,
		BackupID:     p.BackupID,
		ProjectID:    p.ProjectID,
		ProjectPath:  kit.Ternary(p.Files, project.Path, ""),
		ComposeName:  project.ComposeName,
		Volumes:      targets,
		Files:        p.Files,
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
// gets the Docker labels the backup recorded instead of Compose's. An existing target must keep its data in Docker, as
// when backing up.
func (s *Service) missingVolumes(ctx context.Context, project types.Project, manifest types.ProjectManifest, targets []types.ProjectBackupRestoreVolume) ([]client.VolumeCreateOptions, error) {
	resolved, _, err := projects.ResolveProjectVolumes(ctx, projects.ResolveProjectVolumesOptions{
		Project:     project,
		Inspect:     s.deps.Volume,
		BackupStore: s.deps.BackupStore(ctx),
	})
	if err != nil {
		return nil, err
	}
	var create []client.VolumeCreateOptions
	for _, target := range targets {
		current := resolved[slices.IndexFunc(resolved, func(volume types.ProjectVolume) bool { return volume.Key == target.Key })]
		switch {
		case slices.ContainsFunc(create, func(options client.VolumeCreateOptions) bool { return options.Name == target.Name }):
			continue
		case !current.Missing && !current.Included:
			return nil, common.Classify(common.ErrValidation, fmt.Errorf("volume %s now keeps its data outside Docker, so it cannot be restored", target.Name))
		case !current.Missing:
			continue
		}
		recorded := slices.IndexFunc(manifest.Volumes, func(volume types.ProjectVolume) bool { return volume.Key == target.Key })
		options, optionsErr := projects.ComposeVolumeTargetCreateOptions(target.Name, projects.NewComposeVolumeTarget(project.ComposeName, current, manifest.Volumes[recorded].DockerLabels))
		if optionsErr != nil {
			return nil, optionsErr
		}
		create = append(create, options)
	}
	return create, nil
}

// restoreSteps binds an in-place restore's operations to its run and its plan.
func (s *Service) restoreSteps(input restoreRun, activityID string, p restorePlan) backup.RestoreSteps {
	report := func(ctx context.Context, message string) {
		activity.AppendOrWarn(ctx, s.deps.Activities, activityID, activity.AppendMessageRequest{Level: types.ActivityMessageLevelInfo, Message: message})
	}
	return backup.RestoreSteps{
		Report: report,
		Hold:   s.deps.Repository.HoldLocal,
		Stop: func(ctx context.Context, state backup.RestoreState) error {
			active, _, err := s.containerUse(ctx, p.ComposeName)
			switch {
			case err != nil || len(active) == 0:
				return err
			// projects:down was checked only for a project that ran; one the restore deployed is its own to stop.
			case !state.MayStop:
				return common.Classify(common.ErrConflict, fmt.Errorf("container %s was started during the restore", docker.ContainerNameFromNames(active[0].Names)))
			}
			return s.stopProject(ctx, p.ProjectID, p.ComposeName, active, input.User)
		},
		Deploy: func(ctx context.Context) error {
			return s.deps.Deploy(ctx, p.ProjectID, input.User, nil)
		},
		Safety: func(ctx context.Context) (string, error) { return s.safetyBackup(ctx, p) },
		InUse: func(ctx context.Context) error {
			if len(p.Volumes) == 0 {
				return nil
			}
			project, err := s.deps.Project(ctx, p.ProjectID, projectDetails)
			if err != nil {
				return err
			}
			return s.volumesFree(ctx, project, p.volumeNames())
		},
		Create: func(ctx context.Context, options client.VolumeCreateOptions) error {
			dockerClient, err := s.deps.Docker.GetClient(ctx)
			if err != nil {
				return err
			}
			return projects.CreateNewVolume(ctx, dockerClient, options)
		},
		Write: func(ctx context.Context, safetyID string) error {
			if safetyID == "" {
				return s.RestoreTo(ctx, types.ProjectBackupRestore{ProjectID: p.ProjectID, BackupID: p.BackupID, ProjectPath: p.ProjectPath, Volumes: p.Volumes, Report: report})
			}
			return s.RestoreTo(ctx, types.ProjectBackupRestore{ProjectID: p.ProjectID, BackupID: safetyID, ProjectPath: p.ProjectPath, Volumes: p.safetyVolumes(), Report: report})
		},
		Verify: func(ctx context.Context) error { return s.verifyRestore(ctx, p) },
		Remove: func(ctx context.Context, names []string) error {
			errs := make([]error, 0, len(names))
			for _, name := range names {
				errs = append(errs, s.deps.DeleteVolume(ctx, name, input.User))
			}
			return errors.Join(errs...)
		},
		Discard: nil,
	}
}

// safetyBackup takes a local backup of the project files and the existing targets, recording them as a backup does;
// the restore holds the locks.
func (s *Service) safetyBackup(ctx context.Context, p restorePlan) (string, error) {
	project, err := s.deps.Project(ctx, p.ProjectID, projectDetails)
	if err != nil {
		return "", err
	}
	existing := make([]string, 0, len(p.Volumes))
	for _, target := range p.safetyVolumes() {
		existing = append(existing, target.Key)
	}
	captured, err := s.inspectVolumes(ctx, project, existing)
	if err != nil {
		return "", err
	}
	dockerClient, err := s.deps.Docker.GetClient(ctx)
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
	safety := plan{BackupID: entry.ID, ProjectID: project.ID, Volumes: captured, LocalEnabled: true}
	err = s.deps.Repository.WriteRunSnapshots(ctx, dockerClient, snapshotRun(entry, safety), backup.SnapshotWrite{
		Capture: func(ctx context.Context, repository backup.Repository, password string) (backup.Snapshot, error) {
			return s.snapshot(ctx, entry, safety, repository, password)
		},
		Save: s.saveSnapshots(entry),
	})
	if completeErr := s.complete(ctx, entry, err); completeErr != nil {
		return "", completeErr
	}
	return entry.ID, nil
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
	if opts.ProjectPath == "" && len(opts.Volumes) == 0 {
		return nil
	}
	if opts.LockVolumes {
		names := restorePlan{Volumes: opts.Volumes}.volumeNames()
		leases, admitErr := s.admit(ctx, volumeAdmissions(names), "restore-to:"+opts.BackupID)
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
		projectMounts, err = projectSourceMounts(ctx, dockerClient, backup.SourceMountOptions{ContainerPath: opts.ProjectPath, Target: path.Join(backup.RestoreRoot, "project"), Writable: true})
		if err != nil {
			return err
		}
		// Checked again here, since every mount inside the folder would be written to.
		if len(projectMounts) > 1 {
			return common.Classify(common.ErrValidation, fmt.Errorf("the project folder holds other mounts in the Arcane container (%s); its files were not restored",
				strings.Join(nestedTargets(projectMounts), ", ")))
		}
	}
	parts := make([]backup.RestorePart, 0, len(opts.Volumes))
	for _, volume := range opts.Volumes {
		parts = append(parts, backup.RestorePart{Key: volume.Key, Snapshot: volume.Snapshot, Target: volume.Name})
	}
	target, options, err := backup.RestoreLayout(projectMounts, parts, plan{Volumes: manifest.Volumes}.volumeNames())
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
	repository, err := s.repository(ctx, dockerClient, entry.S3DestinationID, remote)
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
