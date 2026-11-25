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

	"github.com/italypaleale/francis/actor"
	"github.com/italypaleale/francis/builtin/workflow"
	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/api/types/mount"
	"github.com/moby/moby/client"
	"gorm.io/gorm"

	"kit.getarcane.app"
	"kit.getarcane.app/acfs"
	"kit.getarcane.app/concurrency"
	"kit.getarcane.app/docker"

	"getarcane.dev/arcane/v3/internal/backup"
	"getarcane.dev/arcane/v3/internal/common"
	"getarcane.dev/arcane/v3/internal/config"
	"getarcane.dev/arcane/v3/internal/database"
	dockerInternal "getarcane.dev/arcane/v3/internal/docker"
	"getarcane.dev/arcane/v3/internal/middleware"
	"getarcane.dev/arcane/v3/internal/project/children/backup/children/policies"
	"getarcane.dev/arcane/v3/internal/s3"
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
		LockVolumes func(ctx context.Context, names []string) (func(), error)
		Project     func(ctx context.Context, projectID string, opts types.ProjectDetailsOptions) (types.Project, error)
		StopProject func(ctx context.Context, projectID string, services []string, user types.Actor) error
		// Volume inspects a Docker volume and reports whether it exists.
		Volume func(ctx context.Context, name string) (types.Volume, bool, error)
		// BackupStore names the volume holding local backups, which a backup never copies.
		BackupStore func(ctx context.Context) string
		// LockProject takes the project lock that deploy, down and project edits also take.
		LockProject func(ctx context.Context, op, projectID string) (func(), error)
		// SoleDirectory refuses a project directory that is a projects directory or holds another project.
		SoleDirectory func(ctx context.Context, project types.Project) error
		// RunsArcane reports whether the project's containers include the running Arcane container.
		RunsArcane  func(ctx context.Context, projectID string) (bool, error)
		GitOpsOwned func(ctx context.Context, projectID string) (bool, error)
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
		deps     Dependencies
		policies *policies.Service
		flow     *flow.Engine
		workflow *flow.Workflow
		restores *flow.Workflow
		watch    backup.RunWatch
		// discovery serializes recording found backups, so concurrent discoveries never record a row twice.
		discovery sync.Mutex
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

	// admittedRun is a scheduled backup's plan, resolved under the admissions its leases hold, and the policy ID its
	// backup is recorded under.
	admittedRun struct {
		leases   []*runs.Lease
		plan     plan
		policyID string
		project  types.Project
	}

	checkpoint struct {
		Stopped []string `json:"stopped,omitempty"`
		Plan    plan     `json:",inline"`
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
	manifestName   = "arcane-project.json"
	// envName holds, beside the manifest, the file a project's .env links to outside the project directory.
	envName          = "project.env"
	backupsTable     = "project_backups"
	projectTagPrefix = "arcane-project:"
	triggerTagPrefix = "arcane-trigger:"
	policyTagPrefix  = "arcane-policy:"
)

var (
	errAlreadyRunning = common.Classify(common.ErrConflict, errors.New("a backup or restore of this project is already running"))
	stopPermissions   = []string{authz.PermProjectsDown, authz.PermProjectsDeploy}
	projectDetails    = types.ProjectDetailsOptions{
		IncludeServiceConfigs: true,
		IncludeIncludeFiles:   true,
	}
)

func NewService(deps Dependencies) *Service {
	s := &Service{deps: deps}
	s.policies = policies.NewService(policies.Dependencies{
		Engine:         deps.Engine,
		Policies:       deps.Policies,
		S3Destinations: deps.S3Destinations,
		Settings:       deps.Settings,
		Config:         deps.Config,
		ProjectName:    deps.ProjectName,
		ListProjects:   deps.ListProjects,
		CheckVolumes:   s.checkVolumes,
		LatestRun:      s.latestRun,
		Resume:         s.resumeScheduled,
		BackUp:         s.backUpScheduled,
		Expire:         s.expire,
		Ctx:            deps.Ctx,
		Scheduler:      deps.Scheduler,
		Admission:      deps.Admission,
	})
	return s
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
	return s.policies.RegisterWorkflows(engine)
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
	if p.StopContainers {
		active, _, activeErr := s.containerUse(ctx, project.ComposeName)
		if activeErr != nil {
			return types.ProjectBackup{}, activeErr
		}
		if len(active) > 0 {
			if permErr := common.Classify(common.ErrForbidden, middleware.RequirePermissions(ctx, opts.EnvironmentID, stopPermissions)); permErr != nil {
				return types.ProjectBackup{}, permErr
			}
		}
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
		var recorded checkpoint
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
	if recordErr := s.record(ctx, input.Plan.ProjectID, checkpoint{Plan: input.Plan}); recordErr != nil {
		return nil, recordErr
	}
	entry, err := s.load(ctx, input.Plan.BackupID)
	if err != nil {
		return nil, err
	}
	localChecked, executeErr := s.execute(ctx, entry, input.Plan, &input.Requester)
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	if completeErr := s.complete(ctx, entry, executeErr, localChecked); completeErr != nil {
		return nil, completeErr
	}
	return succeeded, nil
}

// resume finishes a backup an earlier attempt left running; like a volume backup, one that stopped containers fails. A
// scheduled backup has no requester.
func (s *Service) resume(ctx context.Context, previous types.SchedulerRun, requester *types.BackupRequester, recorded checkpoint) error {
	// The admissions died with the process, so a restore may own the project until they are retaken.
	leases, err := s.admit(ctx, admissions(recorded.Plan.ProjectID, recorded.Plan.volumeNames()), "")
	if err != nil {
		return fmt.Errorf("%w: %w", actor.ErrJobRejected, err)
	}
	defer release(context.WithoutCancel(ctx), leases)
	entry, err := s.load(ctx, recorded.Plan.BackupID)
	if err != nil {
		return err
	}
	switch entry.Status {
	case types.VolumeBackupStatusFailed:
		return fmt.Errorf("%w: %s", backup.ErrBackupSettled, entry.Error)
	case types.VolumeBackupStatusSucceeded:
		return nil
	}
	if recorded.Plan.StopContainers {
		return errors.Join(s.startContainers(ctx, recorded.Stopped), backup.FailStoppedRun(ctx, func(ctx context.Context, message string) error {
			return s.fail(ctx, entry.ID, message)
		}))
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
	if adoptErr := s.deps.Repository.AdoptRunSnapshots(ctx, dockerClient, snapshotRun(entry, recorded.Plan), previous); adoptErr != nil {
		return adoptErr
	}
	if saveErr := s.deps.DB.WithContext(ctx).Save(entry).Error; saveErr != nil {
		return saveErr
	}
	localChecked, executeErr := s.execute(ctx, entry, recorded.Plan, requester)
	if ctx.Err() != nil {
		return ctx.Err()
	}
	return s.complete(ctx, entry, executeErr, localChecked)
}

func (s *Service) repository(ctx context.Context, dockerClient *client.Client, s3DestinationID string, remote bool) (backup.Repository, error) {
	if remote {
		return s.deps.Repository.Remote(ctx, s3DestinationID, "")
	}
	return s.deps.Repository.Local(ctx, dockerClient, backup.LocalRepositoryOptions{})
}

// execute takes the snapshot unless it already exists, then replicates a local snapshot to S3 when both are enabled.
func (s *Service) execute(ctx context.Context, entry *ProjectBackup, p plan, requester *types.BackupRequester) (localChecked bool, err error) {
	defer concurrency.RecoverToError(&err, "project backup")
	dockerClient, err := s.deps.Docker.GetClient(ctx)
	if err != nil {
		return false, err
	}
	err = s.deps.Repository.WriteRunSnapshots(ctx, dockerClient, snapshotRun(entry, p), backup.SnapshotWrite{
		Capture: func(ctx context.Context, repository backup.Repository, password string) (backup.Snapshot, error) {
			snapshot, captureErr := s.capture(ctx, entry, p, requester, repository, password)
			localChecked = captureErr == nil && p.LocalEnabled
			return snapshot, captureErr
		},
		Save: s.saveSnapshots(entry),
	})
	return localChecked, err
}

// saveSnapshots saves what a snapshot recorded on the backup, leaving its status to whatever settles the backup.
func (s *Service) saveSnapshots(entry *ProjectBackup) func(ctx context.Context) error {
	return func(ctx context.Context) error {
		return s.deps.DB.WithContext(ctx).Model(entry).Select("local_snapshot_id", "remote_snapshot_id", "size", "stopped_containers").Updates(entry).Error
	}
}

// capture writes the manifest, stops the project when asked, and takes one snapshot of its files and volumes.
func (s *Service) capture(ctx context.Context, entry *ProjectBackup, p plan, requester *types.BackupRequester, repository backup.Repository, password string) (backup.Snapshot, error) {
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
	return s.snapshot(ctx, entry, p, requester, repository, password)
}

// snapshot takes the snapshot capture describes into the repository; the caller holds the volume locks, and the project
// lock when it stops containers. A snapshot rejected by the check after the copy is returned with the error, so it is
// recorded and removed with the backup.
func (s *Service) snapshot(ctx context.Context, entry *ProjectBackup, p plan, requester *types.BackupRequester, repository backup.Repository, password string) (backup.Snapshot, error) {
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
	// The repository is waited for before the project stops, so it stays stopped only while this snapshot is copied.
	held, releaseRepository, err := s.deps.Engine.HoldRepository(ctx, repository.ID)
	if err != nil {
		return backup.Snapshot{}, err
	}
	defer releaseRepository()
	// Containers outside the project keep running, so a volume they mount may change while it is captured. A
	// restarting container is left out of the running count but can still write its volumes.
	active, inUse, err := s.containerUse(ctx, cmp.Or(project.ComposeName, entry.ComposeName))
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
	input := snapshotInput(projectMounts, metaVolume, entry, p)
	capture := backup.CapturePlan{
		Inspect: s.deps.Volume,
		Stop:    nil,
		Verify:  nil,
		// Recorded now, so a run resumed after a shutdown can't complete the rejected snapshot.
		Rejected: func(ctx context.Context, err error) error { return s.fail(ctx, entry.ID, err.Error()) },
		Mounts:   input.Mounts,
		Volumes:  p.volumeNames(),
	}
	if p.StopContainers && len(active) > 0 {
		capture.Stop = func(ctx context.Context) (func(context.Context) error, error) {
			if requester != nil {
				if authorizeErr := s.authorize(ctx, *requester, stopPermissions...); authorizeErr != nil {
					return nil, authorizeErr
				}
			}
			stopped := make([]string, 0, len(active))
			for _, c := range active {
				stopped = append(stopped, c.ID)
			}
			if recordErr := s.record(ctx, p.ProjectID, checkpoint{Plan: p, Stopped: stopped}); recordErr != nil {
				return nil, recordErr
			}
			start := func(ctx context.Context) error {
				if startErr := s.startContainers(ctx, stopped); startErr != nil {
					return startErr
				}
				return s.record(ctx, p.ProjectID, checkpoint{Plan: p})
			}
			entry.StoppedContainers = true
			return start, backup.ProjectStop{
				Docker:    s.deps.Docker,
				Stop:      s.deps.StopProject,
				User:      types.SystemUser,
				ProjectID: entry.ProjectID,
				Compose:   project.ComposeName,
				Active:    active,
			}.Run(ctx)
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

func (s *Service) startContainers(ctx context.Context, ids []string) error {
	if len(ids) == 0 {
		return nil
	}
	dockerClient, err := s.deps.Docker.GetClient(ctx)
	if err != nil {
		return err
	}
	errs := make([]error, 0, len(ids))
	for _, id := range ids {
		if _, startErr := dockerClient.ContainerStart(ctx, id, client.ContainerStartOptions{}); startErr != nil {
			errs = append(errs, fmt.Errorf("start container %s: %w", id, startErr))
		}
	}
	return errors.Join(errs...)
}

func (s *Service) authorize(ctx context.Context, requester types.BackupRequester, permissions ...string) error {
	for _, permission := range permissions {
		if err := s.deps.Engine.Authorize(ctx, types.BackupRequester{
			UserID:        requester.UserID,
			APIKeyID:      requester.APIKeyID,
			EnvironmentID: requester.EnvironmentID,
			Permission:    permission,
		}); err != nil {
			return err
		}
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

func (s *Service) record(ctx context.Context, id string, recovery any) error {
	data, err := json.Marshal(recovery)
	if err != nil {
		return err
	}
	return jobcontext.Progress(ctx, types.TargetOutcome{ResourceType: backup.ProjectTargetType, ID: id, Status: types.RunStatusRunning, RecoveryData: data})
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

func (s *Service) complete(ctx context.Context, entry *ProjectBackup, err error, localChecked bool) error {
	entry.Status, entry.Error, entry.CompletedAt = types.VolumeBackupStatusSucceeded, "", new(time.Now())
	switch {
	case err != nil && localChecked && entry.LocalSnapshotID != "":
		entry.Destination, entry.Error = types.VolumeBackupDestinationLocal, err.Error()
	case err != nil:
		entry.Status, entry.Error = types.VolumeBackupStatusFailed, err.Error()
	}
	if saveErr := s.deps.DB.WithContext(context.WithoutCancel(ctx)).Save(entry).Error; saveErr != nil {
		return errors.Join(err, fmt.Errorf("failed to save project backup result: %w", saveErr))
	}
	if err == nil {
		err = jobcontext.Progress(ctx, types.TargetOutcome{ResourceType: backup.ProjectTargetType, ID: entry.ProjectID, Status: types.RunStatusSucceeded})
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

// RegisterJobsOnStartup schedules every enabled project backup policy and global project backup policy.
func (s *Service) RegisterJobsOnStartup(ctx context.Context) {
	s.policies.RegisterJobsOnStartup(ctx)
}

// RemovePolicies unschedules and deletes a deleted project's backup policies, which leaves their backups unmanaged.
func (s *Service) RemovePolicies(ctx context.Context, projectID string) {
	s.policies.Remove(ctx, projectID)
}

// checkVolumes refuses a volume selection that a manual backup of the project would refuse.
func (s *Service) checkVolumes(ctx context.Context, projectID string, keys []string) error {
	project, err := s.deps.Project(ctx, projectID, projectDetails)
	if err != nil {
		return err
	}
	_, err = s.inspectVolumes(ctx, project, keys)
	return err
}

// latestRun returns the newest backup a policy took: a project policy's own, or a global policy's of any project.
func (s *Service) latestRun(ctx context.Context, policyID string) (*types.ProjectBackup, error) {
	var latest []ProjectBackup
	if err := s.deps.DB.WithContext(ctx).Where("policy_id = ? OR policy_id LIKE ?", policyID, types.GlobalProjectPolicyPrefix+policyID+":%").
		Order("created_at DESC").Limit(1).Find(&latest).Error; err != nil {
		return nil, fmt.Errorf("failed to load the latest project backup: %w", err)
	}
	if len(latest) == 0 {
		return nil, nil
	}
	return new(latest[0].DTO(DTOOptions{})), nil
}

// resumeScheduled finishes the backup an interrupted scheduled attempt checkpointed in recoveryData; a resume that ends
// for good, a user cancel included, fails the backup, while one handed back leaves it running for the retry.
// backup.ErrBackupSettled means it had already failed, so a new one should run. A project policy's run has no requester.
func (s *Service) resumeScheduled(ctx context.Context, previous types.SchedulerRun, requester *types.BackupRequester, recoveryData []byte) error {
	var recorded checkpoint
	if err := json.Unmarshal(recoveryData, &recorded); err != nil {
		return err
	}
	err := s.resume(ctx, previous, requester, recorded)
	if err != nil && !errors.Is(err, backup.ErrBackupSettled) && !handedBack(ctx, err) {
		err = errors.Join(err, s.fail(context.WithoutCancel(ctx), recorded.Plan.BackupID, err.Error()))
	}
	return err
}

// backUpScheduled backs a project up for a policy once admitted, then applies the policy's retention under the
// project's admission, so it never deletes a backup a restore is reading. A failed backup can leave a local snapshot
// too, so retention runs after it as well.
func (s *Service) backUpScheduled(ctx context.Context, run policies.ScheduledBackup) error {
	admitted, err := s.admitScheduled(ctx, run.ProjectID, run.SeriesID, run.Read)
	if err != nil {
		return err
	}
	defer release(context.WithoutCancel(ctx), admitted.leases)
	err = s.backUpAdmitted(ctx, admitted)
	run.Retain(ctx)
	return err
}

// admitScheduled waits for the project's admission, reads the policy through read and plans the backup under it, then
// waits for the leases of the volumes the plan captures, in name order so two waiting backups never hold one each of the
// other's. The backup is recorded under policyID.
func (s *Service) admitScheduled(
	ctx context.Context, projectID, policyID string, read func(context.Context) (types.CreateProjectBackupRequest, error),
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
	request, err := read(ctx)
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
	return admittedRun{policyID: policyID, project: project, plan: p, leases: leases}, nil
}

// backUpAdmitted records the admitted run's backup and its checkpoint, then takes it as a manual backup does.
func (s *Service) backUpAdmitted(ctx context.Context, admitted admittedRun) (err error) {
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
	if recordErr := s.record(ctx, p.ProjectID, checkpoint{Plan: p}); recordErr != nil {
		return recordErr
	}
	localChecked, executeErr := s.execute(ctx, entry, p, nil)
	if ctx.Err() != nil {
		return ctx.Err()
	}
	return s.complete(ctx, entry, executeErr, localChecked)
}

// expire deletes the series' backups beyond its newest keep; the caller holds the project's admission.
func (s *Service) expire(ctx context.Context, seriesID string, keep int) error {
	expired, err := backup.ExpiredRunIDs(ctx, s.deps.DB, backupsTable, seriesID, keep)
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

// expiredUnmanaged marks scheduled backups whose policy no longer manages them as unmanaged and returns those, and
// safety backups, that expired; manual backups are never unmanaged.
func (s *Service) expiredUnmanaged(ctx context.Context, sweep types.UnmanagedBackupSweep) ([]string, error) {
	var candidates []ProjectBackup
	if err := s.deps.DB.WithContext(ctx).Model(&ProjectBackup{}).Select("id", "policy_id").
		Where("status <> ? AND trigger = ? AND policy_id <> ''", types.VolumeBackupStatusRunning, types.VolumeBackupTriggerScheduled).
		Find(&candidates).Error; err != nil {
		return nil, fmt.Errorf("failed to load unmanaged project backups: %w", err)
	}
	managed, err := s.policies.Managed(ctx)
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
	if !s.deps.Settings.GetBoolSetting(ctx, backup.ProjectDiscoveryPendingKey) {
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
	return s.deps.Settings.UpdateSetting(ctx, backup.ProjectDiscoveryPendingKey, "false")
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
	s.discovery.Lock()
	defer s.discovery.Unlock()
	stored, err := s.backupsByID(ctx, backupIDs)
	if err != nil {
		return 0, err
	}
	existing := make(map[string]*ProjectBackup, len(stored))
	for _, entry := range stored {
		existing[entry.ID] = entry
	}
	references, err := s.deps.ListProjects(ctx)
	if err != nil {
		return 0, err
	}
	managed, err := s.policies.Managed(ctx)
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
			filled, fillErr := s.addFoundCopies(ctx, *entry, localSnapshot, remoteSnapshot, largest.snapshot.Summary.TotalBytesProcessed)
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
	active, _, err := s.containerUse(ctx, cmp.Or(project.ComposeName, entry.ComposeName))
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
		Metadata: database.JSON{"action": backup.ProjectRestoreAction, "backupId": entry.ID},
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

// extraPermissions are the rights a restore needs besides projects:restore: updating the project to replace its files,
// stopping and deploying a running project, or deploying one it was asked to redeploy.
func (p restorePlan) extraPermissions() []string {
	permissions := kit.Ternary(p.Files, []string{authz.PermProjectsUpdate}, nil)
	switch {
	case p.MayStop:
		return append(permissions, stopPermissions...)
	case kit.FromPtr(p.Redeploy):
		return append(permissions, authz.PermProjectsDeploy)
	}
	return permissions
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
		return nil, s.recoverRestore(ctx, input, t.ActivityID())
	}
	unlock, err := s.lockRestore(ctx, input.Plan)
	if err != nil {
		return nil, err
	}
	defer unlock()
	// Rights can be revoked while a restore waits for its turn, so each one it needs is checked again before the first
	// change.
	if authorizeErr := s.authorize(ctx, input.Requester, append([]string{input.Requester.Permission}, input.Plan.extraPermissions()...)...); authorizeErr != nil {
		return nil, authorizeErr
	}
	p, err := s.prepareRestore(ctx, input.Plan)
	if err != nil {
		return nil, err
	}
	if recordErr := s.record(ctx, p.ID, p); recordErr != nil {
		return nil, recordErr
	}
	if applyErr := s.restoreSteps(input, t.ActivityID(), p).Apply(ctx, p.RestoreState); applyErr != nil {
		return nil, applyErr
	}
	return types.SchedulerOutcome{Status: types.RunStatusSucceeded, Message: "Project backup restored successfully"}, nil
}

func (s *Service) recoverRestore(ctx context.Context, input restoreRun, activityID string) error {
	previous, _ := jobcontext.Run(ctx)
	index := slices.IndexFunc(previous.Outcome.Targets, func(target types.TargetOutcome) bool {
		return target.ID == input.Plan.ID && len(target.RecoveryData) > 0
	})
	if index < 0 {
		return errors.New("the restore was interrupted by a restart before it changed the project; nothing was restored")
	}
	var recorded restorePlan
	if err := json.Unmarshal(previous.Outcome.Targets[index].RecoveryData, &recorded); err != nil {
		return err
	}
	unlock, err := s.lockRestore(ctx, recorded)
	if err != nil {
		return err
	}
	defer unlock()
	return s.restoreSteps(input, activityID, recorded).Rollback(ctx, recorded.RestoreState, errors.New("the restore was interrupted by a restart"))
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
	composeName := cmp.Or(project.ComposeName, kit.Ternary(len(p.Volumes) == 0, manifest.Project.ComposeName, ""))
	if composeName == "" {
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
	if checkErr := s.checkInPlace(ctx, project, composeName, p.Files); checkErr != nil {
		return restorePlan{}, checkErr
	}
	active, inUse, err := s.containerUse(ctx, composeName)
	if err != nil {
		return restorePlan{}, err
	}
	if freeErr := volumesFree(project.Name, p.volumeNames(), inUse); freeErr != nil {
		return restorePlan{}, freeErr
	}
	create, err := s.missingVolumes(ctx, project, manifest, targets)
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
		ComposeName:  composeName,
		Volumes:      targets,
		Files:        p.Files,
	}, nil
}

// checkInPlace refuses an in-place restore that would take Arcane itself down, write under paused containers the
// restore does not stop, or, when it restores the project files, replace the files of other projects.
func (s *Service) checkInPlace(ctx context.Context, project types.Project, composeName string, files bool) error {
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
		Filters: make(client.Filters).Add("label", docker.ComposeProjectLabelKey+"="+composeName),
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
	gitOpsOwned, err := s.deps.GitOpsOwned(ctx, project.ID)
	if err != nil {
		return err
	}
	if gitOpsOwned {
		return common.Classify(common.ErrValidation, fmt.Errorf(
			"a GitOps sync manages files of project %s, so they cannot be restored in place; restore only its volumes, or restore it as a new project", project.Name,
		))
	}
	// A mount inside the project folder, such as a volume another project shares, would be rewritten with the files.
	mounts, err := projectSourceMounts(ctx, dockerClient, backup.SourceMountOptions{ContainerPath: project.Path, Target: "/project", Writable: true})
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
func volumesFree(projectName string, names []string, inUse map[string]bool) error {
	for _, name := range names {
		if inUse[name] {
			return common.Classify(common.ErrConflict, fmt.Errorf("volume %s is mounted by a running container outside project %s; stop it before restoring", name, projectName))
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
		Docker:       s.deps.Docker,
		DeleteVolume: s.deps.DeleteVolume,
		User:         input.User,
		Report:       report,
		Record: func(ctx context.Context, state backup.RestoreState) error {
			p.RestoreState = state
			return s.record(ctx, p.ID, p)
		},
		Hold: s.deps.Repository.HoldLocal,
		Stop: func(ctx context.Context, state backup.RestoreState) error {
			active, _, err := s.containerUse(ctx, p.ComposeName)
			switch {
			case err != nil || len(active) == 0:
				return err
			// projects:down was checked only for a project that ran; one the restore deployed is its own to stop.
			case !state.MayStop:
				return common.Classify(common.ErrConflict, fmt.Errorf("container %s was started during the restore", docker.ContainerNameFromNames(active[0].Names)))
			}
			return backup.ProjectStop{
				Docker:    s.deps.Docker,
				Stop:      s.deps.StopProject,
				User:      input.User,
				ProjectID: p.ProjectID,
				Compose:   p.ComposeName,
				Active:    active,
			}.Run(ctx)
		},
		Deploy: func(ctx context.Context) error {
			return s.deps.Deploy(ctx, p.ProjectID, input.User, nil)
		},
		Safety: func(ctx context.Context) (string, error) { return s.safetyBackup(ctx, p) },
		InUse: func(ctx context.Context) error {
			active, inUse, err := s.containerUse(ctx, p.ComposeName)
			switch {
			case err != nil:
				return err
			case len(active) > 0:
				return common.Classify(common.ErrConflict, fmt.Errorf("container %s was started during the safety backup", docker.ContainerNameFromNames(active[0].Names)))
			}
			return volumesFree(p.ComposeName, p.volumeNames(), inUse)
		},
		Write: func(ctx context.Context, safetyID string) error {
			if safetyID == "" {
				return s.RestoreTo(ctx, types.ProjectBackupRestore{ProjectID: p.ProjectID, BackupID: p.BackupID, ProjectPath: p.ProjectPath, Volumes: p.Volumes, Report: report})
			}
			return s.RestoreTo(ctx, types.ProjectBackupRestore{ProjectID: p.ProjectID, BackupID: safetyID, ProjectPath: p.ProjectPath, Volumes: p.safetyVolumes(), Report: report})
		},
		Written: nil,
		Verify:  func(ctx context.Context) error { return s.verifyRestore(ctx, p) },
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
		ComposeName: cmp.Or(project.ComposeName, p.ComposeName),
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
			return s.snapshot(ctx, entry, safety, nil, repository, password)
		},
		Save: s.saveSnapshots(entry),
	})
	if completeErr := s.complete(ctx, entry, err, false); completeErr != nil {
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
	_, manifest, err := s.open(ctx, dockerClient, entry)
	return manifest, err
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
	// The manifest, not the caller, says which volumes the snapshot holds.
	source, manifest, err := s.open(ctx, dockerClient, entry)
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
	envPath := filepath.Join(opts.ProjectPath, projects.EffectiveEnvFileName)
	liveEnv, liveErr := filepath.EvalSymlinks(envPath)
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
	restoredEnv, err := filepath.EvalSymlinks(envPath)
	if err != nil {
		return fmt.Errorf("resolve the restored .env file: %w", err)
	}
	if !opts.OwnEnv && entry.Trigger != types.VolumeBackupTriggerSafety && (liveErr != nil || liveEnv != restoredEnv) {
		return common.Classify(common.ErrConflict, fmt.Errorf("the backup's .env links to %s, not to the project's current .env file, so its contents were not restored", restoredEnv))
	}
	return s.restoreEnv(ctx, projectsRoot, opts, restoredEnv, env)
}

// restoreEnv writes a .env kept outside the project directory through the link the snapshot restored, as an edit of
// the .env does; with OwnEnv it replaces the link with a file of its own, as an import of an export does. A file
// another project's .env leads to keeps its contents, since the restore leaves other projects as they are.
func (s *Service) restoreEnv(ctx context.Context, projectsRoot string, opts types.ProjectBackupRestore, file, content string) error {
	if opts.OwnEnv {
		if err := projects.RemoveProjectFile(ctx, projectsRoot, opts.ProjectPath, projects.EffectiveEnvFileName); err != nil {
			return err
		}
		if err := (acfs.Root{Path: opts.ProjectPath}).Write(ctx, "/"+projects.EffectiveEnvFileName, []byte(content), acfs.WriteOptions{Mode: 0o600}); err != nil {
			return fmt.Errorf("restore the .env file the backup stored: %w", err)
		}
		return nil
	}
	sharer, err := s.envSharer(ctx, opts.ProjectID, file)
	if err != nil {
		return err
	}
	if sharer != "" {
		opts.Report(ctx, fmt.Sprintf("The .env file %s was left as it is, since project %s uses it too; only the link to it was restored", file, sharer))
		return nil
	}
	if err = projects.WriteProjectFile(ctx, projectsRoot, opts.ProjectPath, projects.EffectiveEnvFileName, content); err != nil {
		return fmt.Errorf("restore the .env file the backup stored: %w", err)
	}
	return nil
}

// envSharer returns the name of another project whose .env leads to file, if any. Every link on the way is resolved,
// since a path through a linked directory names the same file differently.
func (s *Service) envSharer(ctx context.Context, projectID, file string) (string, error) {
	all, err := s.deps.Projects(ctx)
	if err != nil {
		return "", err
	}
	for _, other := range all {
		if other.ID == projectID {
			continue
		}
		// A .env that doesn't resolve, such as a dangling link or a link loop, leads to no file, so it shares none.
		target, resolveErr := filepath.EvalSymlinks(filepath.Join(other.Path, projects.EffectiveEnvFileName))
		if resolveErr == nil && target == file {
			return other.Name, nil
		}
	}
	return "", nil
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

// open opens the repository holding the backup's snapshot and reads its manifest, preferring the local copy and trying
// the S3 copy when the local one can't be read.
func (s *Service) open(ctx context.Context, dockerClient *client.Client, entry *ProjectBackup) (snapshotSource, types.ProjectManifest, error) {
	var errs []error
	for _, remote := range []bool{false, true} {
		id := kit.Ternary(remote, entry.RemoteSnapshotID, entry.LocalSnapshotID)
		if id == "" {
			continue
		}
		repository, err := s.repository(ctx, dockerClient, entry.S3DestinationID, remote)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		password, err := s.deps.Repository.Password(ctx, dockerClient, repository)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		source := snapshotSource{repository: repository, password: password, id: id}
		manifest, err := s.readManifest(ctx, dockerClient, source)
		if err == nil {
			return source, manifest, nil
		}
		errs = append(errs, err)
	}
	return snapshotSource{}, types.ProjectManifest{}, errors.Join(errs...)
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
