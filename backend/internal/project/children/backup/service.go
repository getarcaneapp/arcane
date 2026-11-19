package backup

import (
	"cmp"
	"context"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"errors"
	"fmt"
	"log/slog"
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
	"getarcane.dev/arcane/v3/internal/s3"
	"getarcane.dev/arcane/v3/internal/scheduler/flow"
	"getarcane.dev/arcane/v3/internal/scheduler/jobcontext"
	"getarcane.dev/arcane/v3/internal/scheduler/runs"
	"getarcane.dev/arcane/v3/internal/settings"
	activitylib "getarcane.dev/arcane/v3/pkg/activity"
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
		Containers     *backup.Containers
		S3Destinations *s3.S3DestinationService
		Settings       *settings.SettingsService
		VolumeLocks    *concurrency.KeyedMutex
		Project        func(ctx context.Context, projectID string) (types.Project, error)
	}

	Service struct {
		deps     Dependencies
		flow     *flow.Engine
		workflow *flow.Workflow
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
		User      types.Actor           `json:"user"`
		Plan      plan                  `json:"plan"`
	}
)

const (
	admissionScope = "project-backup"
	snapshotRoot   = "/snapshot"
	manifestName   = "arcane-project.json"
	targetType     = "project_backup"
)

var errAlreadyRunning = common.Classify(common.ErrConflict, errors.New("a backup of this project is already running"))

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
		Activity:    activitylib.StartRequest{Type: types.ActivityTypeResourceAction},
		Labels:      map[string]string{"backup": "Creating backup"},
		Steps:       []workflow.StepSpec{workflow.Step("backup", engine.Handler(s.run), workflow.WithMaxAttempts(1))},
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
	leases, err := s.admit(ctx, p, "")
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
	releases := make([]func(context.Context), 0, len(leases))
	for index, key := range p.admissions() {
		releases = append(releases, s.deps.Engine.Hold(key.Scope, key.ID, entry.ID, leases[index]))
	}
	releaseHeld := func(ctx context.Context) {
		for _, releaseLease := range releases {
			releaseLease(ctx)
		}
	}
	activityID, err := s.flow.Submit(ctx, s.workflow, runInput{Requester: requester, User: opts.User, Plan: p}, activitylib.StartRequest{
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

// resolvePlan validates the request against the project; by default it captures every named volume that exists.
func (s *Service) resolvePlan(ctx context.Context, project types.Project, request types.CreateProjectBackupRequest) (plan, error) {
	selected := cmp.Or(request.Destination, types.VolumeBackupDestinationLocal)
	if !slices.Contains([]types.VolumeBackupDestination{types.VolumeBackupDestinationLocal, types.VolumeBackupDestinationS3, types.VolumeBackupDestinationLocalS3}, selected) {
		return plan{}, common.Classify(common.ErrBadRequest, errors.New("invalid project backup destination"))
	}
	p := plan{
		ProjectID:      project.ID,
		LocalEnabled:   selected != types.VolumeBackupDestinationS3,
		S3Enabled:      selected != types.VolumeBackupDestinationLocal,
		StopContainers: *cmp.Or(request.StopContainers, new(true)),
	}
	if p.S3Enabled {
		if p.S3DestinationID = strings.TrimSpace(request.S3DestinationID); p.S3DestinationID == "" {
			return plan{}, common.Classify(common.ErrBadRequest, errors.New("select an S3 destination for the project backup"))
		}
		if _, err := s.deps.S3Destinations.Configuration(ctx, p.S3DestinationID); err != nil {
			return plan{}, fmt.Errorf("load S3 destination: %w", err)
		}
	}
	if project.ComposeName == "" {
		return plan{}, common.Classify(common.ErrValidation, fmt.Errorf("the compose files of project %s cannot be loaded; fix them before backing it up", project.Name))
	}
	dockerClient, err := s.deps.Docker.GetClient(ctx)
	if err != nil {
		return plan{}, err
	}
	p.Volumes = projects.ClassifyProjectVolumes(project)
	backable := make(map[string]bool, len(p.Volumes))
	for index, volume := range p.Volumes {
		if !volume.Included {
			continue
		}
		inspected, inspectErr := dockerClient.VolumeInspect(ctx, volume.Name, client.VolumeInspectOptions{})
		if inspectErr != nil && !errdefs.IsNotFound(inspectErr) {
			return plan{}, fmt.Errorf("inspect volume %s: %w", volume.Name, inspectErr)
		}
		p.Volumes[index].Missing = inspectErr != nil
		backable[volume.Key] = inspectErr == nil && (volume.Kind == types.ProjectVolumeNamed || projects.LocalVolume(inspected.Volume.Driver, inspected.Volume.Options))
		p.Volumes[index].Included = backable[volume.Key] && (request.Volumes == nil || slices.Contains(request.Volumes, volume.Key))
	}
	for _, key := range request.Volumes {
		index := slices.IndexFunc(p.Volumes, func(volume types.ProjectVolume) bool { return volume.Key == key })
		switch {
		case index < 0:
			return plan{}, common.Classify(common.ErrValidation, fmt.Errorf("project %s has no volume %s", project.Name, key))
		case !p.Volumes[index].Missing && !backable[key]:
			return plan{}, common.Classify(common.ErrValidation, fmt.Errorf("volume %s keeps its data outside Docker (%s), so it cannot be backed up", key, p.Volumes[index].Kind))
		case p.Volumes[index].Missing:
			return plan{}, common.Classify(common.ErrValidation, fmt.Errorf("volume %s has not been created yet, so it holds no data to back up", key))
		}
	}
	return p, nil
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

// admissions are the project's run and a volume-backup run for each captured volume.
func (p plan) admissions() []types.SchedulerAdmissionKey {
	keys := []types.SchedulerAdmissionKey{{Scope: admissionScope, ID: p.ProjectID}}
	for _, name := range p.volumeNames() {
		keys = append(keys, types.SchedulerAdmissionKey{Scope: backup.VolumeAdmissionScope, ID: name})
	}
	return keys
}

// admit takes every admission without waiting; run takes over the leases StartBackup parked for it.
func (s *Service) admit(ctx context.Context, p plan, run string) ([]*runs.Lease, error) {
	keys := p.admissions()
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
	leases, err := s.admit(ctx, input.Plan, input.Plan.BackupID)
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
	err = s.execute(ctx, entry, input.Plan, input.User)
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	if completeErr := s.complete(ctx, entry, err); completeErr != nil {
		return nil, completeErr
	}
	return succeeded, nil
}

// resume restarts the containers an interrupted attempt left stopped before anything else, then adopts the
// snapshots it already wrote and finishes the backup.
func (s *Service) resume(ctx context.Context, previous types.SchedulerRun, input runInput, recorded plan) error {
	if len(recorded.Stopped) > 0 {
		remaining, restartErr := s.deps.Containers.StartContainers(context.WithoutCancel(ctx), recorded.Stopped, input.User)
		recorded.Stopped = remaining
		if err := errors.Join(restartErr, s.record(ctx, recorded)); err != nil {
			return err
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
	leases, err := s.admit(ctx, recorded, "")
	// Another run holds an admission; hand the task back instead of waiting inside the workflow.
	if errors.Is(err, common.ErrConflict) {
		return fmt.Errorf("%w: %w", actor.ErrJobRejected, err)
	}
	if err != nil {
		return err
	}
	defer release(context.WithoutCancel(ctx), leases)
	if authorizeErr := s.deps.Engine.Authorize(ctx, input.Requester); authorizeErr != nil {
		return authorizeErr
	}
	if adoptErr := s.adopt(ctx, previous, entry, recorded); adoptErr != nil {
		return adoptErr
	}
	err = s.execute(ctx, entry, recorded, input.User)
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
func (s *Service) execute(ctx context.Context, entry *ProjectBackup, p plan, user types.Actor) (err error) {
	defer concurrency.RecoverToError(&err, "project backup")
	if kit.Ternary(p.LocalEnabled, entry.LocalSnapshotID, entry.RemoteSnapshotID) == "" {
		if captureErr := s.capture(ctx, entry, p, user); captureErr != nil {
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
func (s *Service) capture(ctx context.Context, entry *ProjectBackup, p plan, user types.Actor) (err error) {
	unlock, err := s.lockVolumes(ctx, p.volumeNames())
	if err != nil {
		return err
	}
	defer unlock()
	dockerClient, err := s.deps.Docker.GetClient(ctx)
	if err != nil {
		return err
	}
	project, err := s.deps.Project(ctx, entry.ProjectID)
	if err != nil {
		return err
	}
	metaVolume, removeMeta, err := s.writeManifest(ctx, dockerClient, backupManifest(project, p))
	if err != nil {
		return err
	}
	defer removeMeta()
	projectMounts, err := projectSourceMounts(ctx, dockerClient, project.Path)
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
		if len(stopped) > 0 {
			_, restartErr := s.deps.Containers.StartContainers(context.WithoutCancel(ctx), stopped, user)
			err = errors.Join(err, restartErr)
		}
	}()
	if p.StopContainers {
		var stopErr error
		stopped, stopErr = s.deps.Containers.StopContainers(ctx, backup.ContainerScope{Project: project.ComposeName, Volumes: p.volumeNames()}, user)
		p.Stopped = checkpointContainers(stopped)
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
		var restartErr error
		stopped, restartErr = s.deps.Containers.StartContainers(context.WithoutCancel(ctx), stopped, user)
		p.Stopped = checkpointContainers(stopped)
		if recordErr := errors.Join(restartErr, s.record(ctx, p)); recordErr != nil {
			return recordErr
		}
	}
	return jobcontext.Progress(ctx, types.TargetOutcome{ResourceType: "backup_destination", ID: destinationID, Status: types.RunStatusSucceeded, Message: snapshot.ID})
}

// checkpointContainers keeps what restarting a container needs, including the labels that find its Compose replacement.
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

func projectSourceMounts(ctx context.Context, dockerClient *client.Client, projectPath string) ([]mount.Mount, error) {
	mounts, inContainer, err := backup.CurrentMounts(ctx, dockerClient)
	if err != nil {
		return nil, err
	}
	resolved, err := backup.SourceMounts(mounts, inContainer, projectPath, path.Join(snapshotRoot, "project"))
	if err != nil {
		return nil, err
	}
	return backup.BindVolumeSubpaths(ctx, dockerClient, resolved)
}

func backupManifest(project types.Project, p plan) types.ProjectManifest {
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
		ComposeFiles:  kit.Ternary(len(project.ComposeFiles) > 0, project.ComposeFiles, []string{project.ComposeFileName}),
		IncludeFiles:  includeFiles,
		Volumes:       p.Volumes,
		FormatVersion: types.ProjectManifestFormatVersion,
		Consistent:    p.StopContainers || project.RunningCount == 0,
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
