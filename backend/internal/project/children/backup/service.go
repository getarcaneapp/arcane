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
	"slices"
	"strings"
	"time"
	"uuid"

	"github.com/containerd/errdefs"
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
		S3Destinations *s3.S3DestinationService
		Settings       *settings.SettingsService
		VolumeLocks    *concurrency.KeyedMutex
		Project        func(ctx context.Context, projectID string) (types.Project, error)
		// LockProject takes the project lock that deploy, down and project edits also take.
		LockProject func(ctx context.Context, op, projectID string) (func(), error)
		// StopContainers stops the Compose project's running containers and returns those it stopped, also when it fails.
		StopContainers func(ctx context.Context, composeName string) ([]container.Summary, error)
		// StartContainers starts the project's stopped containers again in Compose order.
		StartContainers func(ctx context.Context, projectID string, stopped []container.Summary) error
		// ActiveContainers lists the Compose project's running and restarting containers, the ones a stop stops.
		ActiveContainers func(ctx context.Context, composeName string) ([]container.Summary, error)
		// ComposeFiles returns the compose files the project loads, relative to its directory.
		ComposeFiles func(ctx context.Context, projectID string) ([]string, error)
		// VolumesInUse returns the volumes that running containers outside the Compose project mount.
		VolumesInUse func(ctx context.Context, composeName string) (map[string]bool, error)
		// SoleDirectory refuses a project directory that is a projects directory or holds another project.
		SoleDirectory func(ctx context.Context, project types.Project) error
		// RunsArcane reports whether the project's containers include the running Arcane container.
		RunsArcane func(ctx context.Context, projectID string) (bool, error)
		// Down brings the project down without removing its volumes and Deploy deploys it; the caller holds the project lock.
		Down   func(ctx context.Context, projectID string, user types.Actor) error
		Deploy func(ctx context.Context, projectID string, user types.Actor, options *types.ProjectDeployOptions) error
		// RewriteComposeName sets the Compose project name in the project's files, as an import does.
		RewriteComposeName func(ctx context.Context, projectID, name string) error
		Activities         activity.MessageAppender
	}

	Service struct {
		deps     Dependencies
		flow     *flow.Engine
		workflow *flow.Workflow
		restores *flow.Workflow
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
		Stopped         []container.Summary   `json:"stopped,omitempty"`
		LocalEnabled    bool                  `json:"localEnabled"`
		S3Enabled       bool                  `json:"s3Enabled"`
		StopContainers  bool                  `json:"stopContainers"`
	}

	runInput struct {
		Requester types.BackupRequester `json:"requester"`
		Plan      plan                  `json:"plan"`
	}

	StartRestoreOptions struct {
		User          types.Actor
		EnvironmentID string
		ProjectID     string
		BackupID      string
		Request       types.RestoreProjectBackupRequest
	}

	// RestoreToOptions restores parts of a backup into a project directory and volumes, which need not be the ones the
	// backup was taken from.
	RestoreToOptions struct {
		ProjectID string
		BackupID  string
		// ProjectPath receives the project files; empty leaves them out.
		ProjectPath string
		Volumes     []RestoreVolume
		// LockVolumes holds the target volumes' backup admissions and locks for the copy, for a caller that holds none.
		LockVolumes bool
	}

	// RestoreVolume restores the volume the snapshot holds as volumes/<Snapshot> into the Docker volume Name.
	RestoreVolume struct {
		Key      string `json:"key"`
		Snapshot string `json:"snapshot"`
		Name     string `json:"name"`
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
		ComposeName string                       `json:"composeName"`
		SafetyID    string                       `json:"safetyId,omitempty"`
		Failure     string                       `json:"failure,omitempty"`
		Volumes     []RestoreVolume              `json:"volumes"`
		Create      []client.VolumeCreateOptions `json:"create,omitempty"`
		// Created are the volumes this restore created; only these are removed on rollback.
		Created []string `json:"created,omitempty"`
		Files   bool     `json:"files"`
		// MayStop records that the project ran when the restore was requested, so the caller's right to stop it was checked.
		MayStop     bool `json:"mayStop"`
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
		deploy  func(ctx context.Context) error
		safety  func(ctx context.Context, p restorePlan) (string, error)
		inUse   func(ctx context.Context, p restorePlan) error
		create  func(ctx context.Context, options client.VolumeCreateOptions) error
		restore func(ctx context.Context, opts RestoreToOptions) error
		verify  func(ctx context.Context, p restorePlan) error
		remove  func(ctx context.Context, names []string) error
	}

	snapshotSource struct {
		password   string
		id         string
		repository backup.Repository
	}
)

const (
	admissionScope    = "project-backup"
	snapshotRoot      = "/snapshot"
	restoreRoot       = "/dest"
	manifestName      = "arcane-project.json"
	targetType        = "project_backup"
	restoreTargetType = "project_restore"
)

var errAlreadyRunning = common.Classify(common.ErrConflict, errors.New("a backup or restore of this project is already running"))

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
	return err
}

// StartBackup admits the run, records its row, and hands it to the workflow; a busy project or volume fails fast.
func (s *Service) StartBackup(ctx context.Context, opts StartOptions) (types.ProjectBackup, error) {
	requester := backup.NewRequester(ctx, opts.User, opts.EnvironmentID, authz.PermProjectsBackup)
	project, err := s.deps.Project(ctx, opts.ProjectID)
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
	dto := entry.DTO()
	dto.ActivityID = &activityID
	return dto, nil
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
// and keep their data in Docker; requesting any other volume fails.
func (s *Service) inspectVolumes(ctx context.Context, project types.Project, requested []string) ([]types.ProjectVolume, error) {
	dockerClient, err := s.deps.Docker.GetClient(ctx)
	if err != nil {
		return nil, err
	}
	classified := projects.ClassifyProjectVolumes(project)
	inspected := make([]types.ProjectVolume, 0, len(classified))
	for _, volume := range classified {
		if !volume.Included {
			inspected = append(inspected, volume)
			continue
		}
		found, inspectErr := dockerClient.VolumeInspect(ctx, volume.Name, client.VolumeInspectOptions{})
		if inspectErr != nil && !errdefs.IsNotFound(inspectErr) {
			return nil, fmt.Errorf("inspect volume %s: %w", volume.Name, inspectErr)
		}
		missing := inspectErr != nil
		backable := !missing && (volume.Kind == types.ProjectVolumeNamed || projects.LocalVolume(found.Volume.Driver, found.Volume.Options))
		wanted := requested == nil || slices.Contains(requested, volume.Key)
		switch {
		case wanted && requested != nil && missing:
			return nil, common.Classify(common.ErrValidation, fmt.Errorf("volume %s has not been created yet, so it holds no data to back up", volume.Key))
		case wanted && requested != nil && !backable:
			return nil, common.Classify(common.ErrValidation, fmt.Errorf("volume %s keeps its data outside Docker (%s), so it cannot be backed up", volume.Key, volume.Kind))
		}
		inspected = append(inspected, types.ProjectVolume{
			Labels: volume.Labels,
			// A restore recreates a missing external volume with its own labels, not Compose's.
			DockerLabels: kit.Ternary(volume.Kind == types.ProjectVolumeExternal && backable, found.Volume.Labels, nil),
			DriverOpts:   volume.DriverOpts,
			Key:          volume.Key,
			Name:         volume.Name,
			Kind:         volume.Kind,
			Driver:       volume.Driver,
			Included:     backable && wanted,
			Missing:      missing,
		})
	}
	for _, key := range requested {
		index := slices.IndexFunc(inspected, func(volume types.ProjectVolume) bool { return volume.Key == key })
		switch {
		case index < 0:
			return nil, common.Classify(common.ErrValidation, fmt.Errorf("project %s has no volume %s", project.Name, key))
		case !inspected[index].Included:
			return nil, common.Classify(common.ErrValidation, fmt.Errorf("volume %s keeps its data outside Docker (%s), so it cannot be backed up", key, inspected[index].Kind))
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

func (s *Service) lockVolumes(ctx context.Context, names []string) (func(), error) {
	unlocks := make([]func(), 0, len(names))
	unlockAll := func() {
		for _, unlock := range slices.Backward(unlocks) {
			unlock()
		}
	}
	for _, name := range names {
		unlock, err := s.deps.VolumeLocks.LockContext(ctx, name)
		if err != nil {
			unlockAll()
			return nil, err
		}
		unlocks = append(unlocks, unlock)
	}
	return unlockAll, nil
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
// lock, then adopts the snapshots it already wrote and finishes the backup.
func (s *Service) resume(ctx context.Context, previous types.SchedulerRun, input runInput, recorded plan) error {
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
			return fmt.Errorf("%w: %w", actor.ErrJobRejected, lockErr)
		}
		// A restore of any of the project's volumes finishes before its containers start again.
		unlockVolumes, lockErr := s.lockVolumes(ctx, recorded.mountedVolumeNames())
		if lockErr != nil {
			unlockProject()
			return fmt.Errorf("%w: %w", actor.ErrJobRejected, lockErr)
		}
		restartErr := s.deps.StartContainers(context.WithoutCancel(ctx), recorded.ProjectID, recorded.Stopped)
		unlockVolumes()
		unlockProject()
		if restartErr == nil {
			recorded.Stopped = nil
		}
		recordErr := s.record(ctx, recorded)
		// The project stays down until its containers start, so try again later instead of failing; the checkpoint
		// saved before still lists them.
		if restartErr != nil {
			return fmt.Errorf("%w: restart project containers: %w", actor.ErrJobRejected, errors.Join(restartErr, recordErr))
		}
		if recordErr != nil {
			return recordErr
		}
	}
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
	if authorizeErr := s.deps.Engine.Authorize(ctx, input.Requester); authorizeErr != nil {
		return authorizeErr
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
	return s.deps.Repository.Local(ctx, dockerClient, false)
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
	local, err := s.deps.Repository.Local(ctx, dockerClient, true)
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
	unlock, err := s.lockVolumes(ctx, p.mountedVolumeNames())
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
	project, err := s.deps.Project(ctx, entry.ProjectID)
	if err != nil {
		return err
	}
	// The plan's locks and leases cover the volumes found when the backup was queued.
	if planErr := checkPlanVolumes(project, p); planErr != nil {
		return planErr
	}
	// Containers outside the project keep running, so a volume they mount may change while it is captured.
	inUse, err := s.deps.VolumesInUse(ctx, project.ComposeName)
	if err != nil {
		return err
	}
	p.Volumes = markInUse(p.Volumes, inUse)
	// A restarting container is left out of the running count but can still write its volumes.
	active, err := s.deps.ActiveContainers(ctx, project.ComposeName)
	if err != nil {
		return err
	}
	composeFiles, err := s.deps.ComposeFiles(ctx, entry.ProjectID)
	if err != nil {
		return err
	}
	metaVolume, removeMeta, err := s.writeManifest(ctx, dockerClient, backupManifest(project, p, len(active) > 0, composeFiles))
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
	var stopped []container.Summary
	defer func() {
		if len(stopped) == 0 {
			return
		}
		// Left down, the project is restarted from the checkpoint when the run is tried again.
		if restartErr := s.deps.StartContainers(context.WithoutCancel(ctx), entry.ProjectID, stopped); restartErr != nil {
			err = fmt.Errorf("%w: restart project containers: %w", actor.ErrJobRejected, errors.Join(err, restartErr))
		}
	}()
	if p.StopContainers {
		// Saved before the stops, so a run resumed after a crash partway through restarts what was stopped; starting
		// one that still runs does nothing.
		p.Stopped = checkpointContainers(active)
		if recordErr := s.record(ctx, p); recordErr != nil {
			return recordErr
		}
		var stopErr error
		stopped, stopErr = s.deps.StopContainers(ctx, project.ComposeName)
		p.Stopped = checkpointContainers(mergeContainers(active, stopped))
		if recordErr := errors.Join(stopErr, s.record(ctx, p)); recordErr != nil {
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
	if len(stopped) > 0 {
		restartErr := s.deps.StartContainers(context.WithoutCancel(ctx), entry.ProjectID, stopped)
		stopped = nil
		if restartErr == nil {
			p.Stopped = nil
		}
		recordErr := s.record(ctx, p)
		// The snapshot is saved; the retry restarts the project from the checkpoint saved after the stop, and finishes.
		if restartErr != nil {
			return fmt.Errorf("%w: restart project containers: %w", actor.ErrJobRejected, errors.Join(restartErr, recordErr))
		}
		if recordErr != nil {
			return recordErr
		}
	}
	return jobcontext.Progress(ctx, types.TargetOutcome{ResourceType: "backup_destination", ID: destinationID, Status: types.RunStatusSucceeded, Message: snapshot.ID})
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
		kept = append(kept, container.Summary{ID: summary.ID, Names: summary.Names, Labels: summary.Labels})
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

func backupManifest(project types.Project, p plan, active bool, composeFiles []string) types.ProjectManifest {
	includeFiles := make([]string, 0, len(project.IncludeFiles))
	for _, file := range project.IncludeFiles {
		includeFiles = append(includeFiles, file.RelativePath)
	}
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
		IncludeFiles:  includeFiles,
		Volumes:       p.Volumes,
		FormatVersion: types.ProjectManifestFormatVersion,
		Consistent:    (p.StopContainers || !active) && !slices.ContainsFunc(p.Volumes, func(volume types.ProjectVolume) bool { return volume.InUse }),
	}
}

// writeManifest stores the manifest in a new volume. The holder that receives it mounts only that volume and never
// starts, because Docker before 29.9 mishandles copies into stopped containers with other mounts (#4870).
func (s *Service) writeManifest(ctx context.Context, dockerClient *client.Client, manifest types.ProjectManifest) (string, func(), error) {
	data, err := json.Marshal(manifest, jsontext.WithIndent("  "))
	if err != nil {
		return "", nil, fmt.Errorf("encode project backup manifest: %w", err)
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
	copyErr := docker.CopyToContainer(ctx, dockerClient, holder.ID, "/meta", map[string][]byte{manifestName: data})
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
		dto := entries[i].DTO()
		dto.S3DestinationName = destinations[dto.S3DestinationID].Name
		backups = append(backups, dto)
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
	return entry.DTO(), nil
}

// Delete forgets the backup's snapshots and removes its row; it shares the project's admission with running backups.
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
	if entry.LocalSnapshotID != "" {
		if forgetErr := s.forget(ctx, dockerClient, entry, false); forgetErr != nil {
			return fmt.Errorf("failed to delete the local project snapshot: %w", forgetErr)
		}
		entry.LocalSnapshotID = ""
	}
	if entry.RemoteSnapshotID != "" {
		if forgetErr := s.forget(ctx, dockerClient, entry, true); forgetErr != nil {
			entry.Destination = destination(entry.LocalSnapshotID != "", entry.RemoteSnapshotID != "")
			return errors.Join(fmt.Errorf("failed to delete the S3 project snapshot: %w", forgetErr), s.deps.DB.WithContext(ctx).Save(entry).Error)
		}
	}
	return s.deps.DB.WithContext(ctx).Delete(entry).Error
}

func (s *Service) forget(ctx context.Context, dockerClient *client.Client, entry *ProjectBackup, remote bool) error {
	if remote {
		instanceID := s.deps.Settings.GetSettingsConfig().InstanceID.Value
		observation, err := backup.CheckRemoteRepository(ctx, s.deps.S3Destinations, entry.S3DestinationID, path.Join(backup.VolumeRoot, instanceID))
		if err != nil {
			return err
		}
		if !observation.Available {
			slog.WarnContext(ctx, "S3 repository is missing; dropping the project backup's remote snapshot reference", "backupId", entry.ID)
			return nil
		}
	}
	repository, err := s.repository(ctx, dockerClient, plan{S3DestinationID: entry.S3DestinationID}, remote)
	if err != nil {
		return err
	}
	password, err := s.deps.Repository.Password(ctx, dockerClient, repository)
	if err != nil {
		return err
	}
	return s.deps.Engine.ForgetSnapshots(ctx, dockerClient, repository, password, []string{kit.Ternary(remote, entry.RemoteSnapshotID, entry.LocalSnapshotID)})
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
	project, err := s.deps.Project(ctx, opts.ProjectID)
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
	keys, err := SelectVolumes(included, opts.Request.Volumes)
	if err != nil {
		return "", err
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
	active, err := s.deps.ActiveContainers(ctx, project.ComposeName)
	if err != nil {
		return "", err
	}
	var required []string
	switch {
	case len(active) > 0:
		required = []string{authz.PermProjectsDown, authz.PermProjectsDeploy}
	case kit.FromPtr(opts.Request.Redeploy):
		required = []string{authz.PermProjectsDeploy}
	}
	if permissionErr := middleware.RequirePermissions(ctx, opts.EnvironmentID, required...); permissionErr != nil {
		return "", permissionErr
	}
	requester := backup.NewRequester(ctx, opts.User, opts.EnvironmentID, authz.PermProjectsRestore)
	if authorizeErr := s.deps.Engine.Authorize(ctx, requester); authorizeErr != nil {
		return "", authorizeErr
	}
	p := restorePlan{
		Redeploy:  opts.Request.Redeploy,
		ID:        uuid.New().String(),
		BackupID:  entry.ID,
		ProjectID: project.ID,
		Volumes:   targets,
		Files:     files,
		MayStop:   len(active) > 0,
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

// SelectVolumes returns the requested volume keys, or every included one when the request names none; the backup must
// include each requested key.
func SelectVolumes(included, requested []string) ([]string, error) {
	if requested == nil {
		return included, nil
	}
	for _, key := range requested {
		if !slices.Contains(included, key) {
			return nil, common.Classify(common.ErrValidation, fmt.Errorf("the backup does not include the data of volume %s", key))
		}
	}
	return slices.Compact(slices.Sorted(slices.Values(requested))), nil
}

// restoreTargets pairs each key's volume in the snapshot with the project's current volume for the key, so a backup
// restores into the project's volumes even after a rename.
func restoreTargets(project types.Project, captured []types.ProjectBackupVolume, keys []string) ([]RestoreVolume, error) {
	classified := projects.ClassifyProjectVolumes(project)
	targets := make([]RestoreVolume, 0, len(keys))
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
		targets = append(targets, RestoreVolume{Key: key, Snapshot: captured[source].Name, Name: classified[current].Name})
	}
	return targets, nil
}

func (p restorePlan) volumeNames() []string {
	names := make([]string, 0, len(p.Volumes))
	for _, volume := range p.Volumes {
		names = append(names, volume.Name)
	}
	return slices.Compact(slices.Sorted(slices.Values(names)))
}

// safetyVolumes restores each target from the safety backup, which holds the targets that existed before the restore.
func (p restorePlan) safetyVolumes() []RestoreVolume {
	existing := make([]RestoreVolume, 0, len(p.Volumes))
	for _, volume := range p.Volumes {
		if !slices.ContainsFunc(p.Create, func(created client.VolumeCreateOptions) bool { return created.Name == volume.Name }) {
			existing = append(existing, RestoreVolume{Key: volume.Key, Snapshot: volume.Name, Name: volume.Name})
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
		if authorizeErr := s.deps.Engine.Authorize(ctx, input.Requester); authorizeErr != nil {
			return nil, authorizeErr
		}
		if p, err = s.prepareRestore(ctx, p); err != nil {
			return nil, err
		}
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
	project, err := s.deps.Project(ctx, p.ProjectID)
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
	unlockVolumes, err := s.lockVolumes(ctx, slices.Compact(slices.Sorted(slices.Values(locked))))
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
	project, err := s.deps.Project(ctx, p.ProjectID)
	if err != nil {
		return restorePlan{}, err
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
	if freeErr := s.volumesFree(ctx, project, p.volumeNames()); freeErr != nil {
		return restorePlan{}, freeErr
	}
	create, err := s.missingVolumes(ctx, project, manifest, targets)
	if err != nil {
		return restorePlan{}, err
	}
	active, err := s.deps.ActiveContainers(ctx, project.ComposeName)
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
	return s.deps.SoleDirectory(ctx, project)
}

// volumesFree refuses volumes that a running container outside the project mounts, since the restore would write under it.
func (s *Service) volumesFree(ctx context.Context, project types.Project, names []string) error {
	inUse, err := s.deps.VolumesInUse(ctx, project.ComposeName)
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
func (s *Service) missingVolumes(ctx context.Context, project types.Project, manifest types.ProjectManifest, targets []RestoreVolume) ([]client.VolumeCreateOptions, error) {
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
		found, inspectErr := dockerClient.VolumeInspect(ctx, target.Name, client.VolumeInspectOptions{})
		switch {
		case inspectErr == nil && !projects.LocalVolume(found.Volume.Driver, found.Volume.Options):
			return nil, common.Classify(common.ErrValidation, fmt.Errorf("volume %s now keeps its data outside Docker, so it cannot be restored", target.Name))
		case inspectErr == nil:
			continue
		case !errdefs.IsNotFound(inspectErr):
			return nil, fmt.Errorf("inspect volume %s: %w", target.Name, inspectErr)
		}
		current := classified[slices.IndexFunc(classified, func(volume types.ProjectVolume) bool { return volume.Key == target.Key })]
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
	for _, options := range p.Create {
		if err = r.create(ctx, options); err != nil {
			return r.rollback(ctx, p, fmt.Errorf("create the missing volumes: %w", err))
		}
		// Saved before the next one is created, so a resumed rollback removes every volume this restore created.
		p.Created = append(p.Created, options.Name)
		if err = r.record(ctx, p); err != nil {
			return r.rollback(ctx, p, err)
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
	if err := r.restore(ctx, RestoreToOptions{ProjectID: p.ProjectID, BackupID: p.BackupID, ProjectPath: p.ProjectPath, Volumes: p.Volumes}); err != nil {
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
			undoErr = r.restore(ctx, RestoreToOptions{ProjectID: p.ProjectID, BackupID: p.SafetyID, ProjectPath: p.ProjectPath, Volumes: p.safetyVolumes()})
			cleanup = append(cleanup, r.remove(ctx, p.Created))
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
			if _, err := s.deps.Activities.AppendMessage(context.WithoutCancel(ctx), activityID, activity.AppendMessageRequest{
				Level:   types.ActivityMessageLevelInfo,
				Message: message,
			}); err != nil {
				slog.WarnContext(ctx, "Failed to append project restore activity message", "activityId", activityID, "error", err)
			}
		},
		down: func(ctx context.Context) error {
			return s.deps.Down(ctx, projectID, input.User)
		},
		deploy: func(ctx context.Context) error {
			return s.deps.Deploy(ctx, projectID, input.User, nil)
		},
		safety: s.safetyBackup,
		inUse: func(ctx context.Context, p restorePlan) error {
			if len(p.Volumes) == 0 {
				return nil
			}
			project, err := s.deps.Project(ctx, projectID)
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
	project, err := s.deps.Project(ctx, p.ProjectID)
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

// createVolume creates a missing target volume; one someone else created since the restore was checked is refused, so
// the restore never takes it over or removes it.
func (s *Service) createVolume(ctx context.Context, options client.VolumeCreateOptions) error {
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
	if _, createErr := dockerClient.VolumeCreate(ctx, options); createErr != nil {
		return fmt.Errorf("create volume %s: %w", options.Name, createErr)
	}
	return nil
}

func (s *Service) removeVolumes(ctx context.Context, names []string) error {
	dockerClient, err := s.deps.Docker.GetClient(ctx)
	if err != nil {
		return err
	}
	var errs []error
	for _, name := range names {
		if _, removeErr := dockerClient.VolumeRemove(ctx, name, client.VolumeRemoveOptions{}); removeErr != nil && !errdefs.IsNotFound(removeErr) {
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
	project, err := s.deps.Project(ctx, p.ProjectID)
	if err != nil {
		return err
	}
	if project.ComposeName != p.ComposeName {
		if rewriteErr := s.deps.RewriteComposeName(ctx, p.ProjectID, p.ComposeName); rewriteErr != nil {
			return fmt.Errorf("set the Compose project name %s in the restored files: %w", p.ComposeName, rewriteErr)
		}
		if project, err = s.deps.Project(ctx, p.ProjectID); err != nil {
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
func (s *Service) RestoreTo(ctx context.Context, opts RestoreToOptions) error {
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
		unlock, lockErr := s.lockVolumes(ctx, names)
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
	}
	target, options, err := restoreInput(projectMounts, opts.Volumes, plan{Volumes: manifest.Volumes}.volumeNames())
	if err != nil {
		return err
	}
	if restoreErr := s.deps.Engine.RestoreSnapshot(ctx, dockerClient, source.repository, source.password, source.id, target, options); restoreErr != nil {
		return fmt.Errorf("failed to restore the project snapshot: %w", restoreErr)
	}
	return nil
}

// restoreInput mounts the targets under /dest as the backup laid them out under /snapshot and restores the snapshot root,
// so each target root keeps its owner and mode; globs skip the rest, and /dest holds only targets, so --delete is safe.
func restoreInput(projectMounts []mount.Mount, targets []RestoreVolume, captured []string) (mount.Mount, backup.RestoreOptions, error) {
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
