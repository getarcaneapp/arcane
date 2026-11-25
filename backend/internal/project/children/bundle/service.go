package bundle

import (
	"bytes"
	"cmp"
	"context"
	"errors"
	"fmt"
	"io"
	"maps"
	"slices"
	"strings"
	"time"
	"uuid"

	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/api/types/mount"
	"github.com/moby/moby/client"

	"kit.getarcane.app"
	"kit.getarcane.app/docker"
	"kit.getarcane.app/updater/labels"

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
	// Backups reads project backups and restores them into another project's directory and volumes.
	Backups interface {
		Manifest(ctx context.Context, projectID, backupID string) (types.ProjectManifest, error)
		RestoreTo(ctx context.Context, opts types.ProjectBackupRestore) error
	}

	Dependencies struct {
		Backups Backups
		Docker  *dockerInternal.DockerClientService
		Uploads *upload.UploadService
		// BackupStore names the volume holding Arcane's local backups, which an export never copies.
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
		ExportVolume        func(ctx context.Context, name string) (*types.UploadSession, error)
		RestoreVolume       func(ctx context.Context, name string, request types.RestoreVolumeUploadRequest, user types.Actor) error
		DeleteVolume        func(ctx context.Context, name string, user types.Actor) error
		// CreateProject creates a project as an import does, with files that fill writes into its directory.
		CreateProject func(ctx context.Context, project NewProject) (string, error)
		// Activities receives what a restore as a new project left as it is, on the activity that runs it.
		Activities activity.MessageAppender
	}

	// NewProject is a project created from files written into its directory.
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
		User      types.Actor
		ProjectID string
		BackupID  string
		Request   types.RestoreProjectBackupAsNewRequest
		// RestoreVolumes allows restoring the backup's volumes into new volumes, which needs volumes:create.
		RestoreVolumes bool
	}

	exportState struct {
		docker     map[string]types.Volume
		volumes    []types.ProjectVolume
		services   []string
		active     bool
		runsArcane bool
	}

	importTarget struct {
		restores    map[string]int
		composeName string
		volumes     []types.ProjectVolume
	}

	volumeRestore struct {
		User    types.Actor
		Body    io.Reader
		Name    string
		Compose types.ComposeVolumeTarget
		Size    int64
	}

	importRollback struct {
		User        types.Actor
		ProjectID   string
		ProjectName string
		Restored    []string
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
		return s.writeBundle(ctx, w, opts, manifest, state)
	}
	session, err := s.deps.Uploads.WriteSession(ctx, upload.WriteSessionOptions{
		Write:    write,
		Kind:     types.UploadKindProjectArchive,
		Filename: projects.SanitizeProjectName(project.Name) + types.ProjectBundleExtension,
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
	arcaneOfProject := func(c container.Summary) bool {
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
		services:   projects.ContainerServices(activeContainers),
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

func (s *Service) writeBundle(ctx context.Context, w io.Writer, opts ExportOptions, manifest types.ProjectManifest, state exportState) (err error) {
	writer, err := projects.NewProjectBundleWriter(w, manifest)
	if err != nil {
		return err
	}
	files, err := s.deps.ExportProject(ctx, opts.ProjectID)
	if err != nil {
		return fmt.Errorf("export project files: %w", err)
	}
	if appendErr := s.appendUpload(ctx, writer, projects.ProjectBundleProject, files); appendErr != nil {
		return appendErr
	}
	if state.active && opts.Request.StopProject {
		defer func() {
			if startErr := s.deps.StartProject(context.WithoutCancel(ctx), opts.ProjectID, state.services, opts.User); startErr != nil {
				err = errors.Join(err, fmt.Errorf("start project %s again: %w", manifest.Project.Name, startErr))
			}
		}()
		if stopErr := s.deps.StopProject(ctx, opts.ProjectID, state.services, opts.User); stopErr != nil {
			return fmt.Errorf("stop project %s: %w", manifest.Project.Name, stopErr)
		}
	}
	var copied []string
	if manifest.Consistent {
		for _, volume := range manifest.Volumes {
			if volume.Included {
				copied = append(copied, volume.Name)
			}
		}
		if writerErr := s.checkNoWriters(ctx, copied); writerErr != nil {
			return writerErr
		}
	}
	written := map[string]bool{}
	for _, volume := range manifest.Volumes {
		if !volume.Included || written[volume.Archive] {
			continue
		}
		written[volume.Archive] = true
		session, exportErr := s.deps.ExportVolume(ctx, volume.Name)
		if exportErr != nil {
			return fmt.Errorf("export volume %s: %w", volume.Name, exportErr)
		}
		if appendErr := s.appendUpload(ctx, writer, volume.Archive, session); appendErr != nil {
			return fmt.Errorf("add volume %s: %w", volume.Name, appendErr)
		}
	}
	// Something else, such as a volume backup of another volume restarting an app, can start a writer during the copy.
	if manifest.Consistent {
		if writerErr := s.checkNoWriters(ctx, copied); writerErr != nil {
			return writerErr
		}
	}
	return writer.Close()
}

func (s *Service) checkNoWriters(ctx context.Context, names []string) error {
	dockerClient, err := s.deps.Docker.GetClient(ctx)
	if err != nil {
		return err
	}
	running, err := dockerClient.ContainerList(ctx, client.ContainerListOptions{})
	if err != nil {
		return fmt.Errorf("list containers: %w", err)
	}
	for _, c := range running.Items {
		for _, m := range c.Mounts {
			if m.Type == mount.TypeVolume && m.RW && slices.Contains(names, m.Name) {
				return common.Classify(common.ErrConflict, fmt.Errorf("container %s writes to volume %s; stop it or export again", docker.ContainerNameFromNames(c.Names), m.Name))
			}
		}
	}
	return nil
}

func (s *Service) appendUpload(ctx context.Context, writer *projects.ProjectBundleWriter, name string, session *types.UploadSession) error {
	file, consumed, cleanup, err := s.deps.Uploads.Consume(ctx, session.Kind, session.ID)
	if err != nil {
		return errors.Join(err, s.deps.Uploads.DeleteSession(context.WithoutCancel(ctx), session.Kind, session.ID))
	}
	defer cleanup()
	return writer.Add(name, consumed.Size, file)
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
	if _, _, err = projects.ReadProjectBundle(archive, visit); err != nil {
		if !errors.Is(err, visitErr) {
			err = common.Classify(common.ErrValidation, fmt.Errorf("invalid project bundle: %w", err))
		}
		return "", s.rollback(ctx, err, importRollback{
			User:        opts.User,
			ProjectID:   projectID,
			ProjectName: opts.Request.Name,
			Restored:    restored,
		})
	}
	return projectID, nil
}

// checkComposeNameFree refuses a new project's name when Docker already runs a Compose stack under it.
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

// RestoreAsNew creates a project from a backup's files and the data of its selected volumes, restored into new
// volumes, by the rules of a bundle import: a failure removes the new project and the volumes it created.
func (s *Service) RestoreAsNew(ctx context.Context, opts RestoreAsNewOptions) (string, error) {
	request := opts.Request
	manifest, err := s.deps.Backups.Manifest(ctx, opts.ProjectID, opts.BackupID)
	if err != nil {
		return "", err
	}
	// An external volume keeps its name in the new project, so it is restored only when selected; one that exists is
	// refused, so the original's volume is never restored over.
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
		// Volumes that name one Docker volume share its data in the snapshot, as they share an archive in a bundle.
		included = append(included, types.ProjectVolume{
			Labels:       volume.Labels,
			DockerLabels: volume.DockerLabels,
			Key:          volume.Key,
			Name:         volume.Name,
			Kind:         volume.Kind,
			Driver:       volume.Driver,
			Archive:      volume.Name,
			Included:     true,
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
	// The new project stays locked until its volumes are restored and it is started, so nothing starts it on data still
	// being copied.
	newID := uuid.New().String()
	unlock, err := s.deps.LockProject(ctx, "restore project as new", newID)
	if err != nil {
		return "", err
	}
	defer unlock()
	projectID, err := s.deps.CreateProject(ctx, NewProject{
		User: opts.User,
		// The new project gets its own copy of a .env the original keeps outside its directory, so it never changes the
		// original's file and the import can set the new Compose name in it.
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
	target, err := s.importTarget(ctx, projectID, included)
	var created []string
	if err == nil {
		created, err = s.restoreBackupVolumes(ctx, opts, target, included)
	}
	if err != nil {
		return "", s.rollback(ctx, err, importRollback{
			User:        opts.User,
			ProjectID:   projectID,
			ProjectName: request.Name,
			Restored:    created,
		})
	}
	if !request.Start {
		return projectID, nil
	}
	if deployErr := s.deps.DeployProject(ctx, projectID, opts.User, nil); deployErr != nil {
		return "", fmt.Errorf("project %s was restored but could not be started: %w", request.Name, deployErr)
	}
	return projectID, nil
}

// restoreBackupVolumes creates the new project's volumes, once for volumes that share one, and restores the backup's
// data into them; it returns the names of the volumes it created, also when a step fails.
func (s *Service) restoreBackupVolumes(ctx context.Context, opts RestoreAsNewOptions, target importTarget, included []types.ProjectVolume) ([]string, error) {
	var created []string
	restores := make([]types.ProjectBackupRestoreVolume, 0, len(target.restores))
	for _, archive := range slices.Sorted(maps.Keys(target.restores)) {
		index := target.restores[archive]
		volume := target.volumes[index]
		options, err := projects.ComposeVolumeTargetCreateOptions(volume.Name, projects.NewComposeVolumeTarget(target.composeName, volume, included[index].DockerLabels))
		if err != nil {
			return created, err
		}
		if _, err = s.deps.Docker.Call(ctx, func(ctx context.Context, dockerClient *client.Client) (struct{}, error) {
			return struct{}{}, projects.CreateNewVolume(ctx, dockerClient, options)
		}); err != nil {
			return created, err
		}
		created = append(created, volume.Name)
		restores = append(restores, types.ProjectBackupRestoreVolume{Key: volume.Key, Snapshot: included[index].Name, Name: volume.Name})
	}
	return created, s.deps.Backups.RestoreTo(ctx, types.ProjectBackupRestore{
		ProjectID:   opts.ProjectID,
		BackupID:    opts.BackupID,
		Volumes:     restores,
		LockVolumes: true,
	})
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

func (s *Service) rollback(ctx context.Context, cause error, rollback importRollback) error {
	if rollback.ProjectID == "" {
		return cause
	}
	ctx = context.WithoutCancel(ctx)
	var leftovers []string
	if err := s.deps.RemoveProject(ctx, rollback.ProjectID, types.ProjectDestroyOptions{
		RemoveFiles:    true,
		KeepContainers: true,
	}, rollback.User); err != nil {
		leftovers = append(leftovers, fmt.Sprintf("project %s (%v)", rollback.ProjectName, err))
	}
	for _, name := range rollback.Restored {
		if err := s.deps.DeleteVolume(ctx, name, rollback.User); err != nil {
			leftovers = append(leftovers, fmt.Sprintf("volume %s (%v)", name, err))
		}
	}
	if len(leftovers) > 0 {
		return fmt.Errorf("%w; could not remove %s", cause, strings.Join(leftovers, ", "))
	}
	return fmt.Errorf("%w; the imported project and its volumes were removed", cause)
}
