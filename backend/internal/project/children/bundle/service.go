package bundle

import (
	"bytes"
	"cmp"
	"context"
	"errors"
	"fmt"
	"io"
	"maps"
	"path/filepath"
	"slices"
	"strings"
	"time"
	"uuid"

	composeTypes "github.com/compose-spec/compose-go/v2/types"
	mobyContainer "github.com/moby/moby/api/types/container"
	mobyMount "github.com/moby/moby/api/types/mount"
	mobyClient "github.com/moby/moby/client"

	kit "kit.getarcane.app"
	"kit.getarcane.app/docker"
	"kit.getarcane.app/updater/labels"

	"getarcane.dev/arcane/v3/internal/backup"
	"getarcane.dev/arcane/v3/internal/common"
	"getarcane.dev/arcane/v3/internal/config"
	dockerInternal "getarcane.dev/arcane/v3/internal/docker"
	"getarcane.dev/arcane/v3/internal/upload"
	"getarcane.dev/arcane/v3/pkg/activity"
	"getarcane.dev/arcane/v3/pkg/projects"
	"getarcane.dev/arcane/v3/pkg/volumes"
	"getarcane.dev/types/v3"
)

type (
	Backups interface {
		HoldBackups(ctx context.Context, projectID string) (func(context.Context), error)
		Manifest(ctx context.Context, projectID, backupID string) (types.ProjectManifest, error)
		RestoreTo(ctx context.Context, opts types.ProjectBackupRestore) error
	}

	Dependencies struct {
		Backups             Backups
		Docker              *dockerInternal.DockerClientService
		Uploads             *upload.UploadService
		BackupStore         func(ctx context.Context) string
		Project             func(ctx context.Context, projectID string) (types.Project, error)
		Volume              func(ctx context.Context, name string) (types.Volume, bool, error)
		ComposeProjectInUse func(ctx context.Context, name string) (bool, error)
		ExportProject       func(ctx context.Context, projectID string) (*types.UploadSession, error)
		ImportProject       func(ctx context.Context, request types.ImportProjectArchive, user types.Actor) (string, error)
		LockProject         func(ctx context.Context, op, projectID string) (func(), error)
		StopProject         func(ctx context.Context, projectID string, services []string, user types.Actor) error
		StartProject        func(ctx context.Context, projectID string, services []string, user types.Actor) error
		DeployProject       func(ctx context.Context, projectID string, user types.Actor, options *types.ProjectDeployOptions) error
		RemoveProject       func(ctx context.Context, projectID string, opts types.ProjectDestroyOptions, user types.Actor) error
		LockVolumes         func(ctx context.Context, names []string) (func(), error)
		ArchiveVolumes      func(ctx context.Context, parts []mobyMount.Mount, add func(ctx context.Context, part mobyMount.Mount, size int64, body io.Reader) error) error
		RestoreVolume       func(ctx context.Context, name string, request types.RestoreVolumeUploadRequest, user types.Actor) error
		DeleteVolume        func(ctx context.Context, name string, user types.Actor) error
		CreateProject       func(ctx context.Context, project NewProject) (string, error)
		Activities          activity.MessageAppender
	}

	NewProject struct {
		User types.Actor
		Fill func(ctx context.Context, projectPath string) error
		ID   string
		Name string
	}

	Service struct {
		deps Dependencies
	}

	ExportOptions struct {
		User      types.Actor
		ProjectID string
		Request   types.ExportProjectArchive
	}

	ImportOptions struct {
		User           types.Actor
		Request        types.ImportProjectArchive
		RestoreVolumes bool
	}

	RestoreAsNewOptions struct {
		User           types.Actor
		ProjectID      string
		BackupID       string
		Request        types.RestoreProjectBackupAsNewRequest
		RestoreVolumes bool
	}

	exportState struct {
		docker     map[string]types.Volume
		volumes    []types.ProjectVolume
		containers []mobyContainer.Summary
		active     bool
		runsArcane bool
	}

	importTarget struct {
		restores    map[string]int
		composeName string
		volumes     []types.ProjectVolume
		services    []composeTypes.ServiceConfig
	}

	volumeRestore struct {
		User    types.Actor
		Body    io.Reader
		Name    string
		Compose types.ComposeVolumeTarget
		Size    int64
	}
)

var errVolumeRestoreForbidden = errors.New("permission denied: restoring the bundle's volumes requires volumes:create and volumes:upload")

func New(deps Dependencies) *Service {
	return &Service{deps: deps}
}

func (s *Service) Options(ctx context.Context, projectID string) (types.ProjectExportOptions, error) {
	project, err := s.deps.Project(ctx, projectID)
	if err != nil {
		return types.ProjectExportOptions{}, err
	}
	state, err := s.exportState(ctx, project)
	if err != nil {
		return types.ProjectExportOptions{}, err
	}
	return types.ProjectExportOptions{ //exhaustruct:enforce
		Volumes: state.volumes,
		Running: state.active,
	}, nil
}

func (s *Service) Export(ctx context.Context, opts ExportOptions) (*types.UploadSession, error) {
	if len(opts.Request.Volumes) == 0 {
		return s.deps.ExportProject(ctx, opts.ProjectID)
	}
	unlock, err := s.deps.LockProject(ctx, "export project", opts.ProjectID)
	if err != nil {
		return nil, err
	}
	defer unlock()
	project, err := s.deps.Project(ctx, opts.ProjectID)
	if err != nil {
		return nil, err
	}
	state, err := s.exportState(ctx, project)
	if err != nil {
		return nil, err
	}
	if state.active && opts.Request.StopProject && state.runsArcane {
		return nil, common.Classify(common.ErrValidation, fmt.Errorf("project %s runs Arcane itself, so it cannot be stopped for the export; export it without stopping it", project.Name))
	}
	manifest, err := s.manifest(project, opts.Request, state)
	if err != nil {
		return nil, err
	}
	write := func(w io.Writer) error {
		return s.capture(ctx, w, opts, manifest, state)
	}
	session, err := s.deps.Uploads.WriteSession(ctx, upload.WriteSessionOptions{
		Write:    write,
		Kind:     types.UploadKindProjectArchive,
		Filename: projects.SanitizeProjectName(project.Name) + types.ProjectBundleExtension,
		Target:   project.ID,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to package project %s: %w", project.Name, err)
	}
	return session, nil
}

func (s *Service) exportState(ctx context.Context, project types.Project) (exportState, error) {
	dockerClient, err := s.deps.Docker.GetClient(ctx)
	if err != nil {
		return exportState{}, err
	}
	running, err := volumes.ListRunningContainers(ctx, dockerClient)
	if err != nil {
		return exportState{}, err
	}
	activeContainers, inUse := projects.ProjectContainerUse(project.ComposeName, running)
	arcaneOfProject := func(c mobyContainer.Summary) bool {
		return project.ComposeName != "" && docker.ComposeProjectLabel(c.Labels) == project.ComposeName && labels.IsArcaneContainer(c.Labels)
	}
	resolved, inspected, err := projects.ResolveProjectVolumes(ctx, projects.ResolveProjectVolumesOptions{
		Project:     project,
		Inspect:     s.deps.Volume,
		InUse:       inUse,
		BackupStore: s.deps.BackupStore(ctx),
	})
	if err != nil {
		return exportState{}, err
	}
	return exportState{
		docker:     inspected,
		volumes:    resolved,
		containers: activeContainers,
		active:     len(activeContainers) > 0,
		runsArcane: slices.ContainsFunc(running, arcaneOfProject),
	}, nil
}

func (s *Service) manifest(project types.Project, request types.ExportProjectArchive, state exportState) (types.ProjectManifest, error) {
	if project.ComposeName == "" {
		return types.ProjectManifest{}, common.Classify(common.ErrValidation, fmt.Errorf("the compose files of project %s cannot be loaded; fix them before exporting its volumes", project.Name))
	}
	byKey := map[string]types.ProjectVolume{}
	for _, volume := range state.volumes {
		byKey[volume.Key] = volume
	}
	var missing, outside []string
	for _, key := range request.Volumes {
		volume, known := byKey[key]
		switch {
		case !known:
			return types.ProjectManifest{}, common.Classify(common.ErrValidation, fmt.Errorf("project %s has no volume %s", project.Name, key))
		case volume.BackupStore:
			return types.ProjectManifest{}, common.Classify(common.ErrValidation, fmt.Errorf("volume %s holds Arcane's backups, so it cannot be exported", key))
		case volume.Missing:
			missing = append(missing, key)
		case !volume.Included:
			outside = append(outside, key)
		}
	}
	if len(missing) > 0 {
		return types.ProjectManifest{}, common.Classify(common.ErrValidation, fmt.Errorf("these volumes have not been created yet, so they hold no data to export: %s", strings.Join(missing, ", ")))
	}
	if len(outside) > 0 {
		return types.ProjectManifest{}, common.Classify(common.ErrValidation, fmt.Errorf("these volumes keep their data outside Docker, so their data cannot be exported: %s", strings.Join(outside, ", ")))
	}
	manifestVolumes := make([]types.ProjectVolume, 0, len(state.volumes))
	archived := map[string]types.ProjectVolume{}
	consistent := !state.active || request.StopProject
	for _, volume := range state.volumes {
		if !slices.Contains(request.Volumes, volume.Key) {
			manifestVolumes = append(manifestVolumes, types.ProjectVolume{ //exhaustruct:enforce
				Labels:       volume.Labels,
				DockerLabels: nil,
				Key:          volume.Key,
				Name:         volume.Name,
				Kind:         volume.Kind,
				Driver:       volume.Driver,
				Archive:      "",
				Included:     false,
				Missing:      false,
				InUse:        false,
				BackupStore:  false,
			})
			continue
		}
		consistent = consistent && !volume.InUse
		first, shared := archived[volume.Name]
		if shared && first.Kind != volume.Kind {
			return types.ProjectManifest{}, common.Classify(common.ErrValidation, fmt.Errorf(
				"volumes %s and %s both name the Docker volume %s but are %s and %s volumes", first.Key, volume.Key, volume.Name, first.Kind, volume.Kind,
			))
		}
		entry := types.ProjectVolume{ //exhaustruct:enforce
			Labels:       volume.Labels,
			DockerLabels: kit.Ternary(volume.Kind == types.ProjectVolumeExternal, state.docker[volume.Name].Labels, nil),
			Key:          volume.Key,
			Name:         volume.Name,
			Kind:         volume.Kind,
			Driver:       volume.Driver,
			Archive:      kit.Ternary(shared, first.Archive, projects.ProjectBundleVolumeArchive(volume.Key)),
			Included:     true,
			Missing:      false,
			InUse:        volume.InUse,
			BackupStore:  false,
		}
		manifestVolumes = append(manifestVolumes, entry)
		if !shared {
			archived[volume.Name] = entry
		}
	}
	return types.ProjectManifest{ //exhaustruct:enforce
		CreatedAt:     time.Now().UTC(),
		Kind:          types.ProjectManifestKindExport,
		ArcaneVersion: config.Version,
		Project: types.ProjectManifestProject{ //exhaustruct:enforce
			ID:          project.ID,
			Name:        project.Name,
			ComposeName: project.ComposeName,
		},
		Volumes:       manifestVolumes,
		FormatVersion: types.ProjectManifestFormatVersion,
		Consistent:    consistent,
		ExternalEnv:   false,
		ProjectDirs:   nil,
	}, nil
}

func (s *Service) capture(ctx context.Context, w io.Writer, opts ExportOptions, manifest types.ProjectManifest, state exportState) error {
	archives := map[string]string{}
	var names []string
	for _, volume := range manifest.Volumes {
		if _, seen := archives[volume.Name]; volume.Included && !seen {
			archives[volume.Name] = volume.Archive
			names = append(names, volume.Name)
		}
	}
	slices.Sort(names)
	unlock, err := s.deps.LockVolumes(ctx, names)
	if err != nil {
		return err
	}
	defer unlock()
	mounts := make([]mobyMount.Mount, 0, len(names))
	for _, name := range names {
		mounts = append(mounts, backup.VolumePart(name))
	}
	return backup.Capture(ctx, backup.CapturePlan{
		Inspect:  s.deps.Volume,
		Stop:     s.captureStop(opts, manifest, state),
		Verify:   s.captureVerify(manifest.Consistent, names),
		Rejected: nil,
		Mounts:   mounts,
		Volumes:  names,
	}, func(ctx context.Context, mounts []mobyMount.Mount) error {
		writer, writerErr := projects.NewProjectBundleWriter(w, manifest)
		if writerErr != nil {
			return writerErr
		}
		files, exportErr := s.deps.ExportProject(ctx, opts.ProjectID)
		if exportErr != nil {
			return fmt.Errorf("export project files: %w", exportErr)
		}
		file, consumed, cleanup, consumeErr := s.deps.Uploads.Consume(ctx, files.Kind, files.ID)
		if consumeErr != nil {
			return errors.Join(consumeErr, s.deps.Uploads.DeleteSession(context.WithoutCancel(ctx), files.Kind, files.ID))
		}
		addErr := writer.Add(projects.ProjectBundleProject, consumed.Size, file)
		cleanup()
		if addErr != nil {
			return addErr
		}
		if archiveErr := s.deps.ArchiveVolumes(ctx, mounts, func(_ context.Context, part mobyMount.Mount, size int64, body io.Reader) error {
			return writer.Add(archives[part.Source], size, body)
		}); archiveErr != nil {
			return archiveErr
		}
		return writer.Close()
	})
}

func (s *Service) captureStop(opts ExportOptions, manifest types.ProjectManifest, state exportState) func(context.Context) (func(context.Context) error, error) {
	if !state.active || !opts.Request.StopProject {
		return nil
	}
	return func(ctx context.Context) (func(context.Context) error, error) {
		services := projects.ContainerServices(state.containers)
		start := func(ctx context.Context) error {
			return s.deps.StartProject(ctx, opts.ProjectID, services, opts.User)
		}
		if stopErr := (backup.ProjectStop{
			Docker:    s.deps.Docker,
			Stop:      s.deps.StopProject,
			User:      opts.User,
			ProjectID: opts.ProjectID,
			Compose:   manifest.Project.ComposeName,
			Active:    state.containers,
		}).Run(ctx); stopErr != nil {
			return start, fmt.Errorf("stop project %s: %w", manifest.Project.Name, stopErr)
		}
		return start, nil
	}
}

func (s *Service) captureVerify(consistent bool, names []string) func(context.Context) error {
	if !consistent {
		return nil
	}
	return func(ctx context.Context) error {
		return backup.CheckNoWriters(ctx, s.deps.Docker, names)
	}
}

func (s *Service) Import(ctx context.Context, opts ImportOptions) (string, error) {
	head, err := s.deps.Uploads.ReadChunk(ctx, types.UploadKindProjectArchive, opts.Request.UploadID, 0)
	if err != nil {
		return "", err
	}
	manifest, bundled := projects.ReadProjectBundleManifest(bytes.NewReader(head))
	if bundled && !opts.RestoreVolumes && slices.ContainsFunc(manifest.Volumes, func(volume types.ProjectVolume) bool {
		return volume.Included
	}) {
		return "", common.Classify(common.ErrForbidden, errVolumeRestoreForbidden)
	}
	if bundled || opts.Request.Start {
		if nameErr := s.checkComposeNameFree(ctx, opts.Request.Name); nameErr != nil {
			return "", nameErr
		}
	}
	request := types.ImportProjectArchive{ //exhaustruct:enforce
		UploadID:  opts.Request.UploadID,
		Name:      opts.Request.Name,
		ProjectID: cmp.Or(opts.Request.ProjectID, uuid.New().String()),
		Tags:      opts.Request.Tags,
		Start:     opts.Request.Start,
	}
	unlock, err := s.deps.LockProject(ctx, "import project", request.ProjectID)
	if err != nil {
		return "", err
	}
	defer unlock()
	projectID, err := s.importProject(ctx, ImportOptions{
		User:           opts.User,
		Request:        request,
		RestoreVolumes: opts.RestoreVolumes,
	}, bundled)
	if err != nil || !request.Start {
		return projectID, err
	}
	if deployErr := s.deps.DeployProject(ctx, projectID, opts.User, nil); deployErr != nil {
		return "", fmt.Errorf("project %s was imported but could not be started: %w", request.Name, deployErr)
	}
	return projectID, nil
}

func (s *Service) importProject(ctx context.Context, opts ImportOptions, bundled bool) (string, error) {
	if !bundled {
		return s.deps.ImportProject(ctx, opts.Request, opts.User)
	}
	archive, _, cleanupSession, err := s.deps.Uploads.Consume(ctx, types.UploadKindProjectArchive, opts.Request.UploadID)
	if err != nil {
		return "", err
	}
	defer cleanupSession()
	var (
		projectID string
		target    importTarget
		included  []types.ProjectVolume
		restored  []string
		visitErr  error
	)
	importFiles := func(manifest types.ProjectManifest, size int64, body io.Reader) error {
		for _, volume := range manifest.Volumes {
			if volume.Included {
				included = append(included, volume)
			}
		}
		if len(included) > 0 && !opts.RestoreVolumes {
			return common.Classify(common.ErrForbidden, errVolumeRestoreForbidden)
		}
		files, importErr := s.deps.Uploads.IngestSession(ctx, types.UploadKindProjectArchive, projects.ProjectBundleProject, size, body)
		if importErr != nil {
			return fmt.Errorf("store the project files: %w", importErr)
		}
		projectID, importErr = s.deps.ImportProject(ctx, types.ImportProjectArchive{
			UploadID:  files.ID,
			Name:      opts.Request.Name,
			ProjectID: opts.Request.ProjectID,
			Tags:      opts.Request.Tags,
		}, opts.User)
		if importErr != nil {
			if deleteErr := s.deps.Uploads.DeleteSession(context.WithoutCancel(ctx), files.Kind, files.ID); deleteErr != nil && !errors.Is(deleteErr, common.ErrUploadSessionNotFound) {
				return errors.Join(importErr, fmt.Errorf("clean up staged project archive: %w", deleteErr))
			}
			return importErr
		}
		target, importErr = s.importTarget(ctx, projectID, included)
		return importErr
	}
	visit := func(manifest types.ProjectManifest, name string, size int64, body io.Reader) error {
		if name == projects.ProjectBundleProject {
			visitErr = importFiles(manifest, size, body)
			return visitErr
		}
		index := target.restores[name]
		volume := target.volumes[index]
		visitErr = s.restoreVolume(ctx, volumeRestore{
			User:    opts.User,
			Body:    body,
			Name:    volume.Name,
			Compose: projects.NewComposeVolumeTarget(target.composeName, volume, included[index].DockerLabels),
			Size:    size,
		})
		if visitErr == nil {
			restored = append(restored, volume.Name)
		}
		return visitErr
	}
	steps := backup.RestoreSteps{
		Docker:       s.deps.Docker,
		DeleteVolume: s.deps.DeleteVolume,
		User:         opts.User,
		Write: func(context.Context, string) error {
			_, _, readErr := projects.ReadProjectBundle(archive, visit)
			if readErr != nil && !errors.Is(readErr, visitErr) {
				return common.Classify(common.ErrValidation, fmt.Errorf("invalid project bundle: %w", readErr))
			}
			return readErr
		},
		Written: func() []string { return restored },
		Discard: func(ctx context.Context) (bool, []string) {
			return s.discard(ctx, opts.User, projectID, opts.Request.Name)
		},
	}
	if applyErr := steps.Apply(ctx, backup.RestoreState{Redeploy: new(false)}); applyErr != nil {
		return "", applyErr
	}
	return projectID, nil
}

func (s *Service) checkComposeNameFree(ctx context.Context, name string) error {
	composeName := projects.NormalizeProjectName(name)
	stackExists, err := s.deps.ComposeProjectInUse(ctx, composeName)
	if err != nil {
		return fmt.Errorf("check Compose project %s: %w", composeName, err)
	}
	if stackExists {
		return common.Classify(common.ErrConflict, fmt.Errorf("docker already runs a Compose stack named %s; use another name for the project", composeName))
	}
	return nil
}

func (s *Service) RestoreAsNew(ctx context.Context, opts RestoreAsNewOptions) (string, error) {
	request := opts.Request
	releaseBackups, err := s.deps.Backups.HoldBackups(ctx, opts.ProjectID)
	if err != nil {
		return "", err
	}
	defer releaseBackups(context.WithoutCancel(ctx))
	manifest, err := s.deps.Backups.Manifest(ctx, opts.ProjectID, opts.BackupID)
	if err != nil {
		return "", err
	}
	var keys []string
	defaults := []string{}
	for _, volume := range manifest.Volumes {
		if volume.Included {
			keys = append(keys, volume.Key)
		}
		if volume.Included && volume.Kind != types.ProjectVolumeExternal {
			defaults = append(defaults, volume.Key)
		}
	}
	selected, err := projects.SelectBackupVolumes(keys, kit.Ternary(request.Volumes == nil, defaults, request.Volumes))
	if err != nil {
		return "", common.Classify(common.ErrValidation, err)
	}
	included := make([]types.ProjectVolume, 0, len(selected))
	var shared []string
	for _, volume := range manifest.Volumes {
		if volume.Kind == types.ProjectVolumeExternal && !slices.Contains(selected, volume.Key) && !slices.Contains(shared, volume.Name) {
			shared = append(shared, volume.Name)
		}
		if !volume.Included || !slices.Contains(selected, volume.Key) {
			continue
		}
		included = append(included, types.ProjectVolume{ //exhaustruct:enforce
			Labels:       volume.Labels,
			DockerLabels: volume.DockerLabels,
			Key:          volume.Key,
			Name:         volume.Name,
			Kind:         volume.Kind,
			Driver:       volume.Driver,
			Archive:      volume.Name,
			Included:     true,
			Missing:      false,
			InUse:        false,
			BackupStore:  false,
		})
	}
	if len(included) > 0 && !opts.RestoreVolumes {
		return "", common.Classify(common.ErrForbidden, errors.New("permission denied: restoring the backup's volumes requires volumes:create"))
	}
	if len(shared) > 0 && !request.ShareExternalVolumes {
		return "", common.Classify(common.ErrValidation, fmt.Errorf(
			"the new project would use the external volumes %s under the same names and share their data with the original project; restore them into new volumes or allow sharing them",
			strings.Join(shared, ", "),
		))
	}
	if nameErr := s.checkComposeNameFree(ctx, request.Name); nameErr != nil {
		return "", nameErr
	}
	newID := uuid.New().String()
	unlock, err := s.deps.LockProject(ctx, "restore project as new", newID)
	if err != nil {
		return "", err
	}
	defer unlock()
	projectID, err := s.deps.CreateProject(ctx, NewProject{
		User: opts.User,
		Fill: func(ctx context.Context, projectPath string) error {
			return s.deps.Backups.RestoreTo(ctx, types.ProjectBackupRestore{
				Report: func(ctx context.Context, message string) {
					activity.AppendOrWarn(ctx, s.deps.Activities, activity.IDFromContext(ctx), activity.AppendMessageRequest{
						Level: types.ActivityMessageLevelWarning, Message: message,
					})
				},
				ProjectID:   opts.ProjectID,
				BackupID:    opts.BackupID,
				ProjectPath: projectPath,
				OwnEnv:      true,
			})
		},
		ID:   newID,
		Name: request.Name,
	})
	if err != nil {
		return "", err
	}
	var restores []types.ProjectBackupRestoreVolume
	steps := backup.RestoreSteps{
		Docker:       s.deps.Docker,
		DeleteVolume: s.deps.DeleteVolume,
		User:         opts.User,
		Write: func(ctx context.Context, _ string) error {
			return s.deps.Backups.RestoreTo(ctx, types.ProjectBackupRestore{
				ProjectID:   opts.ProjectID,
				BackupID:    opts.BackupID,
				Volumes:     restores,
				LockVolumes: true,
			})
		},
		Discard: func(ctx context.Context) (bool, []string) {
			return s.discard(ctx, opts.User, projectID, request.Name)
		},
	}
	target, err := s.importTarget(ctx, projectID, included)
	if binds := sharedBinds(target.services, manifest.ProjectDirs); err == nil && len(binds) > 0 && !request.ShareExternalVolumes {
		err = common.Classify(common.ErrValidation, fmt.Errorf(
			"the new project would bind-mount %s from the original project's folder and share that data with the original project; change those bind mounts or allow sharing them",
			strings.Join(binds, ", "),
		))
	}
	create, restores, volumesErr := newVolumes(target, included)
	err = cmp.Or(err, volumesErr)
	state := backup.RestoreState{Redeploy: new(false), Create: create}
	if err != nil {
		return "", steps.Rollback(ctx, state, err)
	}
	err = steps.Apply(ctx, state)
	releaseBackups(context.WithoutCancel(ctx))
	if err != nil {
		return "", err
	}
	if !request.Start {
		return projectID, nil
	}
	if deployErr := s.deps.DeployProject(ctx, projectID, opts.User, nil); deployErr != nil {
		return "", fmt.Errorf("project %s was restored but could not be started: %w", request.Name, deployErr)
	}
	return projectID, nil
}

func sharedBinds(services []composeTypes.ServiceConfig, projectDirs []string) []string {
	var shared []string
	for _, service := range services {
		for _, volume := range service.Volumes {
			if volume.Type == composeTypes.VolumeTypeBind && !slices.Contains(shared, volume.Source) && slices.ContainsFunc(projectDirs, func(dir string) bool {
				return kit.FilePathMatches(filepath.Clean(volume.Source), filepath.Clean(dir))
			}) {
				shared = append(shared, volume.Source)
			}
		}
	}
	return shared
}

func newVolumes(target importTarget, included []types.ProjectVolume) ([]mobyClient.VolumeCreateOptions, []types.ProjectBackupRestoreVolume, error) {
	create := make([]mobyClient.VolumeCreateOptions, 0, len(target.restores))
	restores := make([]types.ProjectBackupRestoreVolume, 0, len(target.restores))
	for _, archive := range slices.Sorted(maps.Keys(target.restores)) {
		index := target.restores[archive]
		volume := target.volumes[index]
		options, err := projects.ComposeVolumeTargetCreateOptions(volume.Name, projects.NewComposeVolumeTarget(target.composeName, volume, included[index].DockerLabels))
		if err != nil {
			return nil, nil, err
		}
		create = append(create, options)
		restores = append(restores, types.ProjectBackupRestoreVolume{Key: volume.Key, Snapshot: included[index].Name, Name: volume.Name})
	}
	return create, restores, nil
}

func (s *Service) importTarget(ctx context.Context, projectID string, included []types.ProjectVolume) (importTarget, error) {
	imported, err := s.deps.Project(ctx, projectID)
	if err != nil {
		return importTarget{}, fmt.Errorf("load the imported project: %w", err)
	}
	if imported.ComposeName == "" {
		return importTarget{}, fmt.Errorf("the compose files of the imported project %s cannot be loaded", imported.Name)
	}
	classified := projects.ClassifyProjectVolumes(imported)
	targets := map[string]types.ProjectVolume{}
	for _, volume := range classified {
		targets[volume.Key] = volume
	}
	resolved := make([]types.ProjectVolume, 0, len(included))
	for _, volume := range included {
		target, ok := targets[volume.Key]
		if !ok || target.Kind != volume.Kind || !target.Included {
			return importTarget{}, fmt.Errorf("volume %s is not a Docker-managed %s volume of the imported project", volume.Key, volume.Kind)
		}
		resolved = append(resolved, target)
	}
	restores, err := s.checkVolumeTargets(ctx, included, resolved, classified)
	if err != nil {
		return importTarget{}, err
	}
	return importTarget{
		restores:    restores,
		composeName: imported.ComposeName,
		volumes:     resolved,
		services:    imported.Services,
	}, nil
}

func (s *Service) checkVolumeTargets(ctx context.Context, included, targets, classified []types.ProjectVolume) (map[string]int, error) {
	byName := map[string]int{}
	restores := map[string]int{}
	checked := map[string]bool{}
	var checkNames []string
	for index, target := range targets {
		if first, shared := byName[target.Name]; shared {
			switch {
			case targets[first].Kind != target.Kind:
				return nil, common.Classify(common.ErrValidation, fmt.Errorf(
					"volumes %s and %s both resolve to the Docker volume %s but are %s and %s volumes", targets[first].Key, target.Key, target.Name, targets[first].Kind, target.Kind,
				))
			case included[first].Archive != included[index].Archive:
				return nil, common.Classify(common.ErrValidation, fmt.Errorf("volumes %s and %s both resolve to the Docker volume %s", targets[first].Key, target.Key, target.Name))
			}
			continue
		}
		if other, shared := restores[included[index].Archive]; shared {
			return nil, common.Classify(common.ErrValidation, fmt.Errorf(
				"volumes %s and %s share their data but resolve to the Docker volumes %s and %s", targets[other].Key, target.Key, targets[other].Name, target.Name,
			))
		}
		byName[target.Name] = index
		restores[included[index].Archive] = index
		checked[target.Name] = true
		checkNames = append(checkNames, target.Name)
	}
	for _, volume := range classified {
		if volume.Kind != types.ProjectVolumeNamed || checked[volume.Name] {
			continue
		}
		checked[volume.Name] = true
		checkNames = append(checkNames, volume.Name)
	}
	var conflicts, external []string
	for _, name := range checkNames {
		_, exists, err := s.deps.Volume(ctx, name)
		if err != nil {
			return nil, fmt.Errorf("inspect volume %s: %w", name, err)
		}
		switch {
		case !exists:
		case slices.ContainsFunc(targets, func(target types.ProjectVolume) bool {
			return target.Name == name && target.Kind == types.ProjectVolumeExternal
		}):
			external = append(external, name)
		default:
			conflicts = append(conflicts, name)
		}
	}
	if len(external) > 0 {
		return nil, common.Classify(common.ErrConflict, fmt.Errorf(
			"the external volumes %s already exist and the new project would use them as they are; leave them out to keep their data untouched",
			strings.Join(external, ", "),
		))
	}
	if len(conflicts) > 0 {
		return nil, common.Classify(common.ErrConflict, fmt.Errorf("these volumes already exist: %s", strings.Join(conflicts, ", ")))
	}
	return restores, nil
}

func (s *Service) restoreVolume(ctx context.Context, restore volumeRestore) error {
	session, err := s.deps.Uploads.IngestSession(ctx, types.UploadKindVolumeBackup, restore.Compose.Key+".tar.gz", restore.Size, restore.Body)
	if err != nil {
		return fmt.Errorf("store the data of volume %s: %w", restore.Compose.Key, err)
	}
	if err = s.deps.RestoreVolume(ctx, restore.Name, types.RestoreVolumeUploadRequest{
		ComposeVolume: &restore.Compose,
		UploadID:      session.ID,
	}, restore.User); err != nil {
		restoreErr := fmt.Errorf("restore volume %s: %w", restore.Name, err)
		if deleteErr := s.deps.Uploads.DeleteSession(context.WithoutCancel(ctx), session.Kind, session.ID); deleteErr != nil && !errors.Is(deleteErr, common.ErrUploadSessionNotFound) {
			return errors.Join(restoreErr, fmt.Errorf("clean up staged volume archive: %w", deleteErr))
		}
		return restoreErr
	}
	return nil
}

func (s *Service) discard(ctx context.Context, user types.Actor, projectID, name string) (bool, []string) {
	if projectID == "" {
		return false, nil
	}
	if err := s.deps.RemoveProject(ctx, projectID, types.ProjectDestroyOptions{
		RemoveFiles:    true,
		KeepContainers: true,
	}, user); err != nil {
		return true, []string{fmt.Sprintf("project %s (%v)", name, err)}
	}
	return true, nil
}
