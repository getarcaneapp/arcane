package backup

import (
	"cmp"
	"context"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"errors"
	"fmt"
	"io/fs"
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
	"kit.getarcane.app/acfs"
	"kit.getarcane.app/concurrency"
	"kit.getarcane.app/docker"

	"getarcane.dev/arcane/v3/internal/backup"
	"getarcane.dev/arcane/v3/internal/common"
	"getarcane.dev/arcane/v3/internal/database"
	dockerInternal "getarcane.dev/arcane/v3/internal/docker"
	"getarcane.dev/arcane/v3/internal/middleware"
	"getarcane.dev/arcane/v3/internal/s3"
	"getarcane.dev/arcane/v3/internal/scheduler/flow"
	"getarcane.dev/arcane/v3/internal/scheduler/jobcontext"
	"getarcane.dev/arcane/v3/internal/scheduler/runs"
	"getarcane.dev/arcane/v3/internal/settings"
	"getarcane.dev/arcane/v3/pkg/activity"
	"getarcane.dev/arcane/v3/pkg/authz"
	"getarcane.dev/arcane/v3/pkg/pagination"
	"getarcane.dev/arcane/v3/pkg/projects"
	"getarcane.dev/arcane/v3/pkg/timeouts"
	"getarcane.dev/arcane/v3/pkg/volumes"
	"getarcane.dev/types/v3"
)

type (
	Dependencies struct {
		DB                 *database.DB
		Docker             *dockerInternal.DockerClientService
		Engine             *backup.Engine
		Repository         *backup.VolumeRepository
		S3Destinations     *s3.S3DestinationService
		Settings           *settings.SettingsService
		LockVolumes        func(ctx context.Context, names []string) (func(), error)
		Project            func(ctx context.Context, projectID string, opts types.ProjectDetailsOptions) (types.Project, error)
		StopProject        func(ctx context.Context, projectID string, services []string, user types.Actor) error
		Volume             func(ctx context.Context, name string) (types.Volume, bool, error)
		BackupStore        func(ctx context.Context) string
		LockProject        func(ctx context.Context, op, projectID string) (func(), error)
		SoleDirectory      func(ctx context.Context, project types.Project) error
		RunsArcane         func(ctx context.Context, projectID string) (bool, error)
		ProjectContainers  func(ctx context.Context, projectID string) ([]container.Summary, error)
		GitOpsOwned        func(ctx context.Context, projectID string) (bool, error)
		Deploy             func(ctx context.Context, projectID string, user types.Actor, options *types.ProjectDeployOptions) error
		RewriteComposeName func(ctx context.Context, projectID, name string) error
		ProjectRoot        func(ctx context.Context, projectPath string) (string, error)
		Projects           func(ctx context.Context) ([]types.ProjectRecord, error)
		DeleteVolume       func(ctx context.Context, name string, user types.Actor) error
		Activities         activity.MessageAppender
	}

	Service struct {
		deps     Dependencies
		flow     *flow.Engine
		workflow *flow.Workflow
		restores *flow.Workflow
		watch    backup.RunWatch
	}

	StartOptions struct {
		User          types.Actor
		EnvironmentID string
		ProjectID     string
		Request       types.CreateProjectBackupRequest
	}

	plan struct {
		BackupID        string                `json:"backupId"`
		ProjectID       string                `json:"projectId"`
		S3DestinationID string                `json:"s3DestinationId,omitempty"`
		Volumes         []types.ProjectVolume `json:"volumes"`
		LocalEnabled    bool                  `json:"localEnabled"`
		S3Enabled       bool                  `json:"s3Enabled"`
		StopContainers  bool                  `json:"stopContainers"`
		Files           bool                  `json:"files"`
	}

	runInput struct {
		Requester types.BackupRequester `json:"requester"`
		Plan      plan                  `json:"plan"`
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

	restorePlan struct {
		Redeploy    *bool                              `json:"redeploy,omitempty"`
		ID          string                             `json:"id"`
		BackupID    string                             `json:"backupId"`
		ProjectID   string                             `json:"projectId"`
		ProjectPath string                             `json:"projectPath,omitempty"`
		ComposeName string                             `json:"composeName"`
		SafetyID    string                             `json:"safetyId,omitempty"`
		Volumes     []types.ProjectBackupRestoreVolume `json:"volumes"`
		Create      []client.VolumeCreateOptions       `json:"create,omitempty"`
		Created     []string                           `json:"created,omitempty"`
		Files       bool                               `json:"files"`
		MayStop     bool                               `json:"mayStop"`
		Running     bool                               `json:"running"`
	}

	restoreRun struct {
		Requester types.BackupRequester `json:"requester"`
		User      types.Actor           `json:"user"`
		Plan      restorePlan           `json:"plan"`
	}

	restoring struct {
		s          *Service
		activityID string
		input      restoreRun
	}

	snapshotSource struct {
		password   string
		id         string
		repository backup.Repository
	}
)

const (
	admissionScope = "project-backup"
	snapshotRoot   = "/snapshot"
	restoreRoot    = "/dest"
	manifestName   = "arcane-project.json"
	envName        = "project.env"
	targetType     = "project_backup"
	stagingPrefix  = "arcane-project-backup-"
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
	return &Service{deps: deps}
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
	return err
}

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
		Error:             "",
		Volumes:           backupVolumes(p.Volumes),
		Size:              0,
		StoppedContainers: false,
		Files:             p.Files,
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
	go func() {
		background := context.WithoutCancel(ctx)
		s.watch.Watch(background, entry.ID, activityID)
		releaseHeld(background)
	}()
	return entry.DTO(DTOOptions{ActivityID: &activityID}), nil
}

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
		Files:           true,
	}, nil
}

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
			Labels:       volume.Labels,
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
		if resumeErr := s.resume(ctx, previous, input, recorded); resumeErr != nil {
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
	localChecked, executeErr := s.execute(ctx, entry, input.Plan, input.Requester)
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	if completeErr := s.complete(ctx, entry, executeErr, localChecked); completeErr != nil {
		return nil, completeErr
	}
	return succeeded, nil
}

func (s *Service) resume(ctx context.Context, previous types.SchedulerRun, input runInput, recorded checkpoint) error {
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
	if len(recorded.Stopped) > 0 {
		return errors.Join(s.startContainers(ctx, recorded.Stopped), backup.FailStoppedRun(ctx, func(ctx context.Context, message string) error {
			return s.fail(ctx, entry.ID, message)
		}))
	}
	if authorizeErr := s.deps.Engine.Authorize(ctx, input.Requester); authorizeErr != nil {
		return authorizeErr
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
	adopted := recorded.Plan.LocalEnabled && entry.LocalSnapshotID != ""
	localChecked, executeErr := s.execute(ctx, entry, recorded.Plan, input.Requester)
	if ctx.Err() != nil {
		return ctx.Err()
	}
	return s.complete(ctx, entry, executeErr, adopted || localChecked)
}

func (s *Service) execute(ctx context.Context, entry *ProjectBackup, p plan, requester types.BackupRequester) (localChecked bool, err error) {
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

func (s *Service) saveSnapshots(entry *ProjectBackup) func(ctx context.Context) error {
	return func(ctx context.Context) error {
		return s.deps.DB.WithContext(ctx).Model(entry).Select("local_snapshot_id", "remote_snapshot_id", "size", "stopped_containers").Updates(entry).Error
	}
}

func (s *Service) capture(ctx context.Context, entry *ProjectBackup, p plan, requester types.BackupRequester, repository backup.Repository, password string) (backup.Snapshot, error) {
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

func (s *Service) snapshot(ctx context.Context, entry *ProjectBackup, p plan, requester types.BackupRequester, repository backup.Repository, password string) (_ backup.Snapshot, err error) {
	dockerClient, err := s.deps.Docker.GetClient(ctx)
	if err != nil {
		return backup.Snapshot{}, err
	}
	project, err := s.deps.Project(ctx, entry.ProjectID, projectDetails)
	if err != nil {
		return backup.Snapshot{}, err
	}
	if planErr := s.checkPlanVolumes(ctx, project, p); planErr != nil {
		return backup.Snapshot{}, planErr
	}
	env, externalEnv, projectMounts, err := s.projectFiles(ctx, dockerClient, project, p.Files)
	if err != nil {
		return backup.Snapshot{}, err
	}
	held, releaseRepository, err := s.deps.Engine.HoldRepository(ctx, repository.ID)
	if err != nil {
		return backup.Snapshot{}, err
	}
	defer releaseRepository()
	active, inUse, err := s.containerUse(ctx, cmp.Or(project.ComposeName, entry.ComposeName))
	if err != nil {
		return backup.Snapshot{}, err
	}
	p.Volumes = markInUse(p.Volumes, inUse)
	captured, err := s.volumeIdentities(ctx, p.volumeNames())
	if err != nil {
		return backup.Snapshot{}, err
	}
	metaVolume, removeMeta, err := s.writeManifest(ctx, dockerClient, backupManifest(project, p, externalEnv), env)
	if err != nil {
		return backup.Snapshot{}, err
	}
	defer removeMeta()
	if p.StopContainers && len(active) > 0 {
		if authorizeErr := s.authorize(ctx, requester, stopPermissions...); authorizeErr != nil {
			return backup.Snapshot{}, authorizeErr
		}
		stopped := make([]string, 0, len(active))
		for _, c := range active {
			stopped = append(stopped, c.ID)
		}
		if recordErr := s.record(ctx, p.ProjectID, checkpoint{Plan: p, Stopped: stopped}); recordErr != nil {
			return backup.Snapshot{}, recordErr
		}
		defer func() {
			background := context.WithoutCancel(ctx)
			if startErr := s.startContainers(background, stopped); startErr != nil {
				err = errors.Join(err, fmt.Errorf("restart project containers: %w", startErr))
				return
			}
			err = errors.Join(err, s.record(background, p.ProjectID, checkpoint{Plan: p}))
		}()
		entry.StoppedContainers = true
		if stopErr := s.stopProject(ctx, entry.ProjectID, project.ComposeName, active, types.SystemUser); stopErr != nil {
			return backup.Snapshot{}, stopErr
		}
	}
	snapshot, err := s.deps.Engine.CreateSnapshot(held, dockerClient, repository, password, project.Name, snapshotInput(projectMounts, metaVolume, entry.ID, p))
	releaseRepository()
	if err != nil {
		return backup.Snapshot{}, fmt.Errorf("failed to create the project snapshot: %w", err)
	}
	checkCtx, cancelCheck := context.WithTimeout(context.WithoutCancel(ctx), timeouts.DefaultDockerAPI)
	after, err := s.volumeIdentities(checkCtx, p.volumeNames())
	cancelCheck()
	if err == nil && !maps.Equal(captured, after) {
		err = common.Classify(common.ErrConflict, errors.New("a volume was removed or replaced while it was copied, so the backup failed; start it again"))
	}
	if err != nil {
		return snapshot, errors.Join(err, s.fail(context.WithoutCancel(ctx), entry.ID, err.Error()))
	}
	return snapshot, nil
}

func (s *Service) projectFiles(ctx context.Context, dockerClient *client.Client, project types.Project, files bool) ([]byte, bool, []mount.Mount, error) {
	if !files {
		return nil, false, nil, nil
	}
	if err := s.deps.SoleDirectory(ctx, project); err != nil {
		return nil, false, nil, err
	}
	env, _, externalEnv, err := projects.ReadExternalEnvFile(ctx, project.Path)
	if err != nil {
		return nil, false, nil, fmt.Errorf("read the .env file of project %s: %w", project.Name, err)
	}
	projectMounts, err := projectSourceMounts(ctx, dockerClient, backup.SourceMountOptions{ContainerPath: project.Path, Target: path.Join(snapshotRoot, "project")})
	if err != nil {
		return nil, false, nil, err
	}
	return env, externalEnv, projectMounts, nil
}

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

func (s *Service) checkPlanVolumes(ctx context.Context, project types.Project, p plan) error {
	current, err := s.inspectVolumes(ctx, project, nil)
	if err != nil {
		return err
	}
	same := len(current) == len(p.Volumes) && !slices.ContainsFunc(current, func(volume types.ProjectVolume) bool {
		return !slices.ContainsFunc(p.Volumes, func(planned types.ProjectVolume) bool {
			return planned.Key == volume.Key && planned.Name == volume.Name && planned.Kind == volume.Kind && (!planned.Included || volume.Included)
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
	return jobcontext.Progress(ctx, types.TargetOutcome{ResourceType: targetType, ID: id, Status: types.RunStatusRunning, RecoveryData: data})
}

func (s *Service) writeManifest(ctx context.Context, dockerClient *client.Client, manifest types.ProjectManifest, env []byte) (string, func(), error) {
	data, err := json.Marshal(manifest, jsontext.WithIndent("  "))
	if err != nil {
		return "", nil, fmt.Errorf("encode project backup manifest: %w", err)
	}
	files := map[string][]byte{manifestName: data}
	if manifest.ExternalEnv {
		files[envName] = env
	}
	name := stagingPrefix + uuid.New().String()
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
	partial := err != nil && localChecked && entry.LocalSnapshotID != ""
	saveErr := s.deps.DB.WithContext(context.WithoutCancel(ctx)).Model(entry).
		Select("completed_at", "status", "destination", "error", "local_snapshot_id", "remote_snapshot_id", "size", "stopped_containers").
		Updates(ProjectBackup{
			CompletedAt:       new(time.Now()),
			Status:            kit.Ternary(err != nil && !partial, types.VolumeBackupStatusFailed, types.VolumeBackupStatusSucceeded),
			Destination:       kit.Ternary(partial, types.VolumeBackupDestinationLocal, entry.Destination),
			LocalSnapshotID:   entry.LocalSnapshotID,
			RemoteSnapshotID:  entry.RemoteSnapshotID,
			Error:             kit.Ternary(err != nil, fmt.Sprint(err), ""),
			Size:              entry.Size,
			StoppedContainers: entry.StoppedContainers,
		}).Error
	if saveErr != nil {
		return errors.Join(err, fmt.Errorf("failed to save project backup result: %w", saveErr))
	}
	if err == nil {
		err = jobcontext.Progress(ctx, types.TargetOutcome{ResourceType: targetType, ID: entry.ProjectID, Status: types.RunStatusSucceeded})
	}
	return err
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

func (s *Service) ReconcileInterruptedBackups(ctx context.Context, protectedIDs ...string) error {
	query := s.deps.DB.WithContext(ctx).Model(&ProjectBackup{}).Where("status = ?", types.VolumeBackupStatusRunning)
	if len(protectedIDs) > 0 {
		query = query.Where("id NOT IN ?", protectedIDs)
	}
	if err := query.Updates(map[string]any{"status": types.VolumeBackupStatusFailed, "error": "Backup interrupted by Arcane restart"}).Error; err != nil {
		return err
	}
	s.removeStagingVolumes(ctx)
	return s.watch.WatchAccepted(ctx)
}

func (s *Service) removeStagingVolumes(ctx context.Context) {
	dockerClient, err := s.deps.Docker.GetClient(ctx)
	if err != nil {
		slog.WarnContext(ctx, "Failed to connect to Docker to remove project backup staging volumes", "error", err)
		return
	}
	staged, err := dockerClient.VolumeList(ctx, client.VolumeListOptions{Filters: make(client.Filters).Add("label", volumes.ContainerLabel).Add("dangling", "true")})
	if err != nil {
		slog.WarnContext(ctx, "Failed to list project backup staging volumes", "error", err)
		return
	}
	for _, volume := range staged.Items {
		if !strings.HasPrefix(volume.Name, stagingPrefix) {
			continue
		}
		if _, removeErr := dockerClient.VolumeRemove(ctx, volume.Name, client.VolumeRemoveOptions{}); removeErr != nil {
			slog.WarnContext(ctx, "Failed to remove a project backup staging volume", "volume", volume.Name, "error", removeErr)
		}
	}
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

func (s *Service) Delete(ctx context.Context, projectID, backupID string) error {
	lease, admitted, err := s.deps.Engine.TryAcquireRun(ctx, admissionScope, projectID)
	if err != nil {
		return err
	}
	if !admitted {
		return errAlreadyRunning
	}
	defer lease.Release(ctx)
	entry, err := s.load(ctx, backupID)
	if err != nil {
		return err
	}
	if entry.ProjectID != projectID {
		return common.Classify(common.ErrNotFound, errors.New("project backup not found"))
	}
	dockerClient, err := s.deps.Docker.GetClient(ctx)
	if err != nil {
		return err
	}
	forgetErr := s.deps.Repository.ForgetRunSnapshots(ctx, dockerClient, []backup.RunSnapshots{entry.snapshots()}, true)
	if entry.LocalSnapshotID == "" && entry.RemoteSnapshotID == "" {
		return errors.Join(forgetErr, s.deps.DB.WithContext(ctx).Delete(entry).Error)
	}
	entry.Destination = backup.Destination(backup.DestinationOptions{Local: entry.LocalSnapshotID != "", Remote: entry.RemoteSnapshotID != ""})
	return errors.Join(forgetErr, s.deps.DB.WithContext(ctx).Save(entry).Error)
}

func (s *Service) StartRestore(ctx context.Context, opts StartRestoreOptions) (string, error) {
	entry, err := s.restorable(ctx, opts.ProjectID, opts.BackupID)
	if err != nil {
		return "", err
	}
	project, err := s.deps.Project(ctx, opts.ProjectID, projectDetails)
	if err != nil {
		return "", err
	}
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
	files := *cmp.Or(opts.Request.Files, new(entry.Files))
	switch {
	case files && !entry.Files:
		return "", common.Classify(common.ErrValidation, errors.New("the backup does not include the project files; restore only its volumes"))
	case !files && len(keys) == 0:
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
	manifest, err := s.Manifest(ctx, opts.ProjectID, entry.ID)
	if err != nil {
		return "", err
	}
	create, err := s.missingVolumes(ctx, project, manifest, targets)
	if err != nil {
		return "", err
	}
	p := restorePlan{
		Redeploy:  opts.Request.Redeploy,
		ID:        uuid.New().String(),
		BackupID:  entry.ID,
		ProjectID: project.ID,
		Volumes:   targets,
		Create:    create,
		Files:     files,
		MayStop:   len(active) > 0,
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
	go func() {
		background := context.WithoutCancel(ctx)
		_, _ = s.flow.Wait(background, s.restores, activityID)
		releaseHeld(background)
	}()
	return activityID, nil
}

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

func (p restorePlan) extraPermissions() []string {
	permissions := slices.Concat(
		kit.Ternary(p.Files, []string{authz.PermProjectsUpdate}, nil),
		kit.Ternary(len(p.Create) > 0, []string{authz.PermVolumesCreate}, nil),
	)
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

func (p restorePlan) safetyVolumes() []types.ProjectBackupRestoreVolume {
	existing := make([]types.ProjectBackupRestoreVolume, 0, len(p.Volumes))
	for _, volume := range p.Volumes {
		if !slices.ContainsFunc(p.Create, func(created client.VolumeCreateOptions) bool { return created.Name == volume.Name }) {
			existing = append(existing, types.ProjectBackupRestoreVolume{Key: volume.Key, Snapshot: volume.Name, Name: volume.Name})
		}
	}
	return existing
}

func (s *Service) runRestore(ctx context.Context, t flow.Task) (any, error) {
	var input restoreRun
	if err := t.Payload(&input); err != nil {
		return nil, err
	}
	r := restoring{s: s, input: input, activityID: t.ActivityID()}
	if t.Recovered() {
		return nil, r.recover(ctx)
	}
	unlock, err := s.lockRestore(ctx, input.Plan)
	if err != nil {
		return nil, err
	}
	defer unlock()
	p, err := s.prepareRestore(ctx, input.Plan)
	if err != nil {
		return nil, err
	}
	if authorizeErr := s.authorize(ctx, input.Requester, append([]string{input.Requester.Permission}, p.extraPermissions()...)...); authorizeErr != nil {
		return nil, authorizeErr
	}
	if recordErr := s.record(ctx, p.ID, p); recordErr != nil {
		return nil, recordErr
	}
	if applyErr := r.apply(ctx, p); applyErr != nil {
		return nil, applyErr
	}
	return types.SchedulerOutcome{Status: types.RunStatusSucceeded, Message: "Project backup restored successfully"}, nil
}

func (r restoring) recover(ctx context.Context) error {
	previous, _ := jobcontext.Run(ctx)
	index := slices.IndexFunc(previous.Outcome.Targets, func(target types.TargetOutcome) bool {
		return target.ID == r.input.Plan.ID && len(target.RecoveryData) > 0
	})
	if index < 0 {
		return errors.New("the restore was interrupted by a restart before it changed the project; nothing was restored")
	}
	var recorded restorePlan
	if err := json.Unmarshal(previous.Outcome.Targets[index].RecoveryData, &recorded); err != nil {
		return err
	}
	unlock, err := r.s.lockRestore(ctx, recorded)
	if errors.Is(err, common.ErrConflict) {
		return fmt.Errorf("%w: %w", actor.ErrJobRejected, err)
	}
	if err != nil {
		return err
	}
	defer unlock()
	return r.rollback(ctx, recorded, errors.New("the restore was interrupted by a restart"))
}

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
	if !slices.Equal(targets, p.Volumes) {
		return restorePlan{}, common.Classify(common.ErrConflict, fmt.Errorf("the volumes of project %s changed after the restore was requested; start it again", project.Name))
	}
	if checkErr := s.checkInPlace(ctx, project, p.Files); checkErr != nil {
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
		Redeploy:    p.Redeploy,
		ID:          p.ID,
		BackupID:    p.BackupID,
		ProjectID:   p.ProjectID,
		ProjectPath: kit.Ternary(p.Files, project.Path, ""),
		ComposeName: composeName,
		Volumes:     targets,
		Create:      create,
		Files:       p.Files,
		MayStop:     p.MayStop,
		Running:     len(active) > 0,
	}, nil
}

func (s *Service) checkInPlace(ctx context.Context, project types.Project, files bool) error {
	runsArcane, err := s.deps.RunsArcane(ctx, project.ID)
	if err != nil {
		return err
	}
	if runsArcane {
		return common.Classify(common.ErrValidation, fmt.Errorf("project %s runs Arcane itself, so it cannot be restored in place; restore it as a new project instead", project.Name))
	}
	found, err := s.deps.ProjectContainers(ctx, project.ID)
	if err != nil {
		return fmt.Errorf("list containers: %w", err)
	}
	for _, c := range found {
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
	dockerClient, err := s.deps.Docker.GetClient(ctx)
	if err != nil {
		return err
	}
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

func volumesFree(projectName string, names []string, inUse map[string]bool) error {
	for _, name := range names {
		if inUse[name] {
			return common.Classify(common.ErrConflict, fmt.Errorf("volume %s is mounted by a running container outside project %s; stop it before restoring", name, projectName))
		}
	}
	return nil
}

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

func (r restoring) apply(ctx context.Context, p restorePlan) error {
	held, releaseRepository, err := r.s.deps.Repository.HoldLocal(ctx)
	if err != nil {
		return err
	}
	defer releaseRepository()
	if p.Running {
		r.report(held, "Stopping the project")
	}
	if err = r.stop(held, p); err != nil {
		return r.rollback(held, p, fmt.Errorf("stop the project: %w", err))
	}
	r.report(held, "Taking a safety backup")
	safetyID, err := r.s.safetyBackup(held, p)
	if err != nil {
		return r.rollback(held, p, fmt.Errorf("take a safety backup: %w", err))
	}
	if err = r.inUse(held, p); err != nil {
		return r.rollback(held, p, err)
	}
	p.SafetyID = safetyID
	if err = r.s.record(held, p.ID, p); err != nil {
		return r.rollback(held, p, err)
	}
	r.report(held, "Restoring the backup")
	for _, options := range p.Create {
		if err = r.create(held, options); err != nil {
			return r.rollback(held, p, fmt.Errorf("create the missing volumes: %w", err))
		}
		p.Created = append(p.Created, options.Name)
		if err = r.s.record(held, p.ID, p); err != nil {
			return r.rollback(held, p, err)
		}
	}
	if err = r.s.RestoreTo(held, types.ProjectBackupRestore{
		ProjectID: p.ProjectID, BackupID: p.BackupID, ProjectPath: p.ProjectPath, Volumes: p.Volumes, Report: r.report,
	}); err == nil {
		err = r.s.verifyRestore(held, p)
	}
	if err != nil {
		return r.rollback(held, p, err)
	}
	releaseRepository()
	if !*cmp.Or(p.Redeploy, new(p.Running)) {
		return nil
	}
	r.report(ctx, "Deploying the project")
	p.MayStop = true
	if err = r.s.record(ctx, p.ID, p); err != nil {
		return r.rollback(ctx, p, err)
	}
	if err = r.deploy(ctx); err != nil {
		return r.rollback(ctx, p, fmt.Errorf("deploy the restored project: %w", err))
	}
	return nil
}

func (r restoring) rollback(ctx context.Context, p restorePlan, cause error) error {
	ctx = context.WithoutCancel(ctx)
	if p.SafetyID == "" {
		if p.Running {
			if err := r.deploy(ctx); err != nil {
				return fmt.Errorf("%w; nothing was restored, but the project could not be deployed again: %w", cause, err)
			}
		}
		return fmt.Errorf("%w; nothing was restored", cause)
	}
	r.report(ctx, "Rolling back to the safety backup")
	held, releaseRepository, err := r.s.deps.Repository.HoldLocal(ctx)
	if err != nil {
		return fmt.Errorf("%w; the rollback could not start (%w); restore the safety backup %s by hand to undo the restore", cause, err, p.SafetyID)
	}
	defer releaseRepository()
	if err = r.stop(held, p); err != nil {
		return fmt.Errorf("%w; the project could not be stopped for the rollback (%w), so it was left as is; restore the safety backup %s by hand to undo the restore", cause, err, p.SafetyID)
	}
	undoErr := r.s.RestoreTo(held, types.ProjectBackupRestore{
		ProjectID: p.ProjectID, BackupID: p.SafetyID, ProjectPath: p.ProjectPath, Volumes: p.safetyVolumes(), Report: r.report,
	})
	removeErr := r.remove(held, p.Created)
	releaseRepository()
	if undoErr != nil {
		return fmt.Errorf("%w; the rollback failed too (%w), so the project was left down; restore the safety backup %s to undo the restore", cause, errors.Join(undoErr, removeErr), p.SafetyID)
	}
	var deployErr error
	if p.Running {
		deployErr = r.deploy(ctx)
	}
	if failed := errors.Join(removeErr, deployErr); failed != nil {
		return fmt.Errorf("%w; the project was rolled back to the safety backup %s, but the rollback hit errors: %w", cause, p.SafetyID, failed)
	}
	return fmt.Errorf("%w; the project was rolled back to the safety backup %s", cause, p.SafetyID)
}

func (r restoring) report(ctx context.Context, message string) {
	activity.AppendOrWarn(ctx, r.s.deps.Activities, r.activityID, activity.AppendMessageRequest{Level: types.ActivityMessageLevelInfo, Message: message})
}

func (r restoring) stop(ctx context.Context, p restorePlan) error {
	active, _, err := r.s.containerUse(ctx, p.ComposeName)
	switch {
	case err != nil || len(active) == 0:
		return err
	case !p.MayStop:
		return common.Classify(common.ErrConflict, fmt.Errorf("container %s was started during the restore", docker.ContainerNameFromNames(active[0].Names)))
	}
	return r.s.stopProject(ctx, p.ProjectID, p.ComposeName, active, r.input.User)
}

func (r restoring) deploy(ctx context.Context) error {
	return r.s.deps.Deploy(ctx, r.input.Plan.ProjectID, r.input.User, nil)
}

func (r restoring) inUse(ctx context.Context, p restorePlan) error {
	active, inUse, err := r.s.containerUse(ctx, p.ComposeName)
	switch {
	case err != nil:
		return err
	case len(active) > 0:
		return common.Classify(common.ErrConflict, fmt.Errorf("container %s was started during the safety backup", docker.ContainerNameFromNames(active[0].Names)))
	}
	return volumesFree(p.ComposeName, p.volumeNames(), inUse)
}

func (r restoring) create(ctx context.Context, options client.VolumeCreateOptions) error {
	dockerClient, err := r.s.deps.Docker.GetClient(ctx)
	if err != nil {
		return err
	}
	return projects.CreateNewVolume(ctx, dockerClient, options)
}

func (r restoring) remove(ctx context.Context, names []string) error {
	errs := make([]error, 0, len(names))
	for _, name := range names {
		errs = append(errs, r.s.deps.DeleteVolume(ctx, name, r.input.User))
	}
	return errors.Join(errs...)
}

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
	entry := &ProjectBackup{ //exhaustruct:enforce
		BaseModel:         database.BaseModel{ID: uuid.New().String(), CreatedAt: time.Now(), UpdatedAt: nil},
		CompletedAt:       nil,
		ProjectID:         project.ID,
		ProjectName:       project.Name,
		ComposeName:       cmp.Or(project.ComposeName, p.ComposeName),
		Status:            types.VolumeBackupStatusRunning,
		Trigger:           types.VolumeBackupTriggerSafety,
		Destination:       types.VolumeBackupDestinationLocal,
		LocalSnapshotID:   "",
		RemoteSnapshotID:  "",
		S3DestinationID:   "",
		Error:             "",
		Volumes:           backupVolumes(captured),
		Size:              0,
		StoppedContainers: false,
		Files:             p.Files,
	}
	if createErr := s.deps.DB.WithContext(ctx).Create(entry).Error; createErr != nil {
		return "", fmt.Errorf("failed to save the safety backup: %w", createErr)
	}
	safety := plan{BackupID: entry.ID, ProjectID: project.ID, Volumes: captured, LocalEnabled: true, Files: p.Files}
	err = s.deps.Repository.WriteRunSnapshots(ctx, dockerClient, snapshotRun(entry, safety), backup.SnapshotWrite{
		Capture: func(ctx context.Context, repository backup.Repository, password string) (backup.Snapshot, error) {
			return s.snapshot(ctx, entry, safety, types.BackupRequester{}, repository, password)
		},
		Save: s.saveSnapshots(entry),
	})
	if completeErr := s.complete(ctx, entry, err, false); completeErr != nil {
		return "", completeErr
	}
	return entry.ID, nil
}

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
	source, manifest, err := s.open(ctx, dockerClient, entry)
	if err != nil {
		return err
	}
	envPath := filepath.Join(opts.ProjectPath, projects.EffectiveEnvFileName)
	var projectMounts []mount.Mount
	var liveEnv string
	var liveErr error
	if opts.ProjectPath != "" {
		liveEnv, liveErr = filepath.EvalSymlinks(envPath)
		projectMounts, err = projectSourceMounts(ctx, dockerClient, backup.SourceMountOptions{ContainerPath: opts.ProjectPath, Target: path.Join(restoreRoot, "project"), Writable: true})
		if err != nil {
			return err
		}
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
	if opts.OwnEnv {
		return s.restoreEnv(ctx, projectsRoot, opts, "", env)
	}
	restoredEnv, err := filepath.EvalSymlinks(envPath)
	if errors.Is(err, fs.ErrNotExist) {
		return s.restoreEnv(ctx, projectsRoot, types.ProjectBackupRestore{ProjectPath: opts.ProjectPath, OwnEnv: true}, "", env)
	}
	if err != nil {
		return fmt.Errorf("resolve the restored .env file: %w", err)
	}
	if entry.Trigger != types.VolumeBackupTriggerSafety && (liveErr != nil || liveEnv != restoredEnv) {
		return common.Classify(common.ErrConflict, fmt.Errorf("the backup's .env links to %s, not to the project's current .env file, so its contents were not restored", restoredEnv))
	}
	return s.restoreEnv(ctx, projectsRoot, opts, restoredEnv, env)
}

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

func (s *Service) envSharer(ctx context.Context, projectID, file string) (string, error) {
	all, err := s.deps.Projects(ctx)
	if err != nil {
		return "", err
	}
	for _, other := range all {
		if other.ID == projectID {
			continue
		}
		target, resolveErr := filepath.EvalSymlinks(filepath.Join(other.Path, projects.EffectiveEnvFileName))
		if resolveErr == nil && target == file {
			return other.Name, nil
		}
	}
	return "", nil
}

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
	return mounts[0], backup.RestoreOptions{DestinationPath: restoreRoot, ExtraMounts: mounts[1:], Globs: globs, DeleteExtra: true}, nil
}

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

func (s *Service) open(ctx context.Context, dockerClient *client.Client, entry *ProjectBackup) (snapshotSource, types.ProjectManifest, error) {
	var errs []error
	for _, remote := range []bool{false, true} {
		id := kit.Ternary(remote, entry.RemoteSnapshotID, entry.LocalSnapshotID)
		if id == "" {
			continue
		}
		source, manifest, err := s.readManifest(ctx, dockerClient, entry.S3DestinationID, remote, id)
		if err == nil {
			return source, manifest, nil
		}
		errs = append(errs, err)
	}
	return snapshotSource{}, types.ProjectManifest{}, errors.Join(errs...)
}

func (s *Service) repository(ctx context.Context, dockerClient *client.Client, s3DestinationID string, remote bool) (backup.Repository, error) {
	if remote {
		return s.deps.Repository.Remote(ctx, s3DestinationID, "")
	}
	return s.deps.Repository.Local(ctx, dockerClient, backup.LocalRepositoryOptions{})
}

func (s *Service) readManifest(ctx context.Context, dockerClient *client.Client, s3DestinationID string, remote bool, id string) (snapshotSource, types.ProjectManifest, error) {
	repository, err := s.repository(ctx, dockerClient, s3DestinationID, remote)
	if err != nil {
		return snapshotSource{}, types.ProjectManifest{}, err
	}
	password, err := s.deps.Repository.Password(ctx, dockerClient, repository)
	if err != nil {
		return snapshotSource{}, types.ProjectManifest{}, err
	}
	source := snapshotSource{repository: repository, password: password, id: id}
	text, err := s.deps.Engine.ReadSnapshotTextFile(ctx, dockerClient, source.repository, source.password, source.id, path.Join("meta", manifestName))
	if err != nil {
		return snapshotSource{}, types.ProjectManifest{}, err
	}
	var manifest types.ProjectManifest
	if decodeErr := json.Unmarshal([]byte(text), &manifest); decodeErr != nil {
		return snapshotSource{}, types.ProjectManifest{}, fmt.Errorf("decode the project backup manifest: %w", decodeErr)
	}
	if manifest.Kind != types.ProjectManifestKindBackup || manifest.FormatVersion > types.ProjectManifestFormatVersion {
		return snapshotSource{}, types.ProjectManifest{}, common.Classify(common.ErrValidation, fmt.Errorf(
			"the backup's manifest is a %q manifest in format %d, which this version of Arcane cannot restore", manifest.Kind, manifest.FormatVersion,
		))
	}
	return source, manifest, nil
}
