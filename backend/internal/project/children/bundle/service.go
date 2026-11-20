package bundle

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"maps"
	"slices"
	"strings"
	"time"

	"github.com/containerd/errdefs"
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
	"getarcane.dev/arcane/v3/pkg/projects"
	"getarcane.dev/arcane/v3/pkg/volumes"
	"getarcane.dev/types/v3"
)

type (
	Environment interface {
		Project(ctx context.Context, projectID string) (types.Project, error)
		Volume(ctx context.Context, name string) (types.Volume, bool, error)
		ComposeProjectInUse(ctx context.Context, name string) (bool, error)
		ExportProject(ctx context.Context, projectID string) (types.UploadSession, error)
		ImportProject(ctx context.Context, request types.ImportProjectArchive, user types.Actor) (string, error)
		ExportVolume(ctx context.Context, name string) (types.UploadSession, error)
		RestoreVolume(ctx context.Context, name string, request types.RestoreVolumeUploadRequest, user types.Actor) error
		DeployProject(ctx context.Context, projectID string, user types.Actor) error
		DownProject(ctx context.Context, projectID string, user types.Actor) error
		DestroyProject(ctx context.Context, projectID string, user types.Actor) error
		DeleteVolume(ctx context.Context, name string, user types.Actor) error
		DeleteUpload(ctx context.Context, kind, uploadID string) error
	}

	Dependencies struct {
		Environment  Environment
		Docker       *dockerInternal.DockerClientService
		Uploads      *upload.UploadService
		ComposeFiles func(ctx context.Context, projectID string) ([]string, error)
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

	exportState struct {
		docker  map[string]types.Volume
		volumes []types.ProjectVolume
		active  bool
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
		ComposeName string
		Created     []types.ProjectVolume
	}
)

func New(deps Dependencies) *Service {
	return &Service{deps: deps}
}

func (s *Service) Options(ctx context.Context, projectID string) (types.ProjectExportOptions, error) {
	project, err := s.deps.Environment.Project(ctx, projectID)
	if err != nil {
		return types.ProjectExportOptions{}, err
	}
	state, err := s.exportState(ctx, project)
	if err != nil {
		return types.ProjectExportOptions{}, err
	}
	return types.ProjectExportOptions{
		Volumes: state.volumes,
		Running: state.active,
	}, nil
}

func (s *Service) Export(ctx context.Context, opts ExportOptions) (*types.UploadSession, error) {
	if len(opts.Request.Volumes) == 0 {
		files, err := s.deps.Environment.ExportProject(ctx, opts.ProjectID)
		if err != nil {
			return nil, err
		}
		return &files, nil
	}
	project, err := s.deps.Environment.Project(ctx, opts.ProjectID)
	if err != nil {
		return nil, err
	}
	state, err := s.exportState(ctx, project)
	if err != nil {
		return nil, err
	}
	manifest, err := s.manifest(ctx, project, opts.Request, state)
	if err != nil {
		return nil, err
	}
	write := func(w io.Writer) error {
		return s.writeBundle(ctx, w, opts, manifest, state.active && opts.Request.StopProject)
	}
	session, err := s.deps.Uploads.WriteSession(ctx, types.UploadKindProjectArchive, projects.SanitizeProjectName(project.Name)+types.ProjectBundleExtension, write)
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
	running, err := dockerClient.ContainerList(ctx, client.ContainerListOptions{})
	if err != nil {
		return exportState{}, fmt.Errorf("list containers: %w", err)
	}
	inUse := map[string]bool{}
	active := false
	for _, c := range running.Items {
		if project.ComposeName != "" && docker.ComposeProjectLabel(c.Labels) == project.ComposeName && !labels.IsArcaneContainer(c.Labels) {
			active = active || c.State == container.StateRunning || c.State == container.StateRestarting
			continue
		}
		for _, m := range c.Mounts {
			if m.Type == mount.TypeVolume {
				inUse[m.Name] = true
			}
		}
	}
	classified := projects.ClassifyProjectVolumes(project)
	resolved := make([]types.ProjectVolume, 0, len(classified))
	inspected := map[string]types.Volume{}
	for _, volume := range classified {
		if !volume.Included {
			resolved = append(resolved, volume)
			continue
		}
		existing, exists, inspectErr := s.deps.Environment.Volume(ctx, volume.Name)
		if inspectErr != nil {
			return exportState{}, fmt.Errorf("inspect volume %s: %w", volume.Name, inspectErr)
		}
		included := exists && (volume.Kind == types.ProjectVolumeNamed || projects.LocalVolume(existing.Driver, existing.Options))
		inspected[volume.Name] = existing
		resolved = append(resolved, types.ProjectVolume{
			Labels:     volume.Labels,
			DriverOpts: volume.DriverOpts,
			Key:        volume.Key,
			Name:       volume.Name,
			Kind:       volume.Kind,
			Driver:     volume.Driver,
			Included:   included,
			Missing:    !exists,
			InUse:      included && inUse[volume.Name],
		})
	}
	return exportState{
		docker:  inspected,
		volumes: resolved,
		active:  active,
	}, nil
}

func (s *Service) manifest(ctx context.Context, project types.Project, request types.ExportProjectArchive, state exportState) (types.ProjectManifest, error) {
	if project.ComposeName == "" {
		return types.ProjectManifest{}, common.Classify(common.ErrValidation, fmt.Errorf("the compose files of project %s cannot be loaded; fix them before exporting its volumes", project.Name))
	}
	composeFiles, err := s.deps.ComposeFiles(ctx, project.ID)
	if err != nil {
		return types.ProjectManifest{}, err
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
			manifestVolumes = append(manifestVolumes, types.ProjectVolume{
				Labels: volume.Labels,
				Key:    volume.Key,
				Name:   volume.Name,
				Kind:   volume.Kind,
				Driver: volume.Driver,
			})
			continue
		}
		consistent = consistent && !volume.InUse
		archive, archiveErr := projects.ProjectBundleVolumeArchive(volume.Key)
		if archiveErr != nil {
			return types.ProjectManifest{}, common.Classify(common.ErrValidation, archiveErr)
		}
		first, shared := archived[volume.Name]
		if shared && first.Kind != volume.Kind {
			return types.ProjectManifest{}, common.Classify(common.ErrValidation, fmt.Errorf(
				"volumes %s and %s both name the Docker volume %s but are %s and %s volumes", first.Key, volume.Key, volume.Name, first.Kind, volume.Kind,
			))
		}
		var dockerLabels map[string]string
		if volume.Kind == types.ProjectVolumeExternal {
			dockerLabels = maps.Clone(state.docker[volume.Name].Labels)
			delete(dockerLabels, volumes.ClaimLabel)
		}
		if labelErr := projects.ValidateVolumeLabels(dockerLabels); labelErr != nil {
			return types.ProjectManifest{}, common.Classify(common.ErrValidation, fmt.Errorf("external volume %s: %w", volume.Key, labelErr))
		}
		entry := types.ProjectVolume{
			Labels:       volume.Labels,
			DockerLabels: dockerLabels,
			Key:          volume.Key,
			Name:         volume.Name,
			Kind:         volume.Kind,
			Driver:       volume.Driver,
			Archive:      kit.Ternary(shared, first.Archive, archive),
			Included:     true,
			InUse:        volume.InUse,
		}
		manifestVolumes = append(manifestVolumes, entry)
		if !shared {
			archived[volume.Name] = entry
		}
	}
	return types.ProjectManifest{
		CreatedAt:     time.Now().UTC(),
		Kind:          types.ProjectManifestKindExport,
		ArcaneVersion: config.Version,
		Project: types.ProjectManifestProject{
			ID:          project.ID,
			Name:        project.Name,
			ComposeName: project.ComposeName,
		},
		ComposeFiles:  composeFiles,
		Volumes:       manifestVolumes,
		FormatVersion: types.ProjectManifestFormatVersion,
		Consistent:    consistent,
	}, nil
}

func (s *Service) writeBundle(ctx context.Context, w io.Writer, opts ExportOptions, manifest types.ProjectManifest, restart bool) (err error) {
	writer, err := projects.NewProjectBundleWriter(w, manifest)
	if err != nil {
		return err
	}
	files, err := s.deps.Environment.ExportProject(ctx, opts.ProjectID)
	if err != nil {
		return fmt.Errorf("export project files: %w", err)
	}
	if appendErr := s.appendUpload(ctx, writer, projects.ProjectBundleProject, files); appendErr != nil {
		return appendErr
	}
	if restart {
		defer func() {
			if startErr := s.deps.Environment.DeployProject(context.WithoutCancel(ctx), opts.ProjectID, opts.User); startErr != nil {
				err = errors.Join(err, fmt.Errorf("start project %s again: %w", manifest.Project.Name, startErr))
			}
		}()
		if downErr := s.deps.Environment.DownProject(ctx, opts.ProjectID, opts.User); downErr != nil {
			return fmt.Errorf("stop project %s: %w", manifest.Project.Name, downErr)
		}
	}
	// A stopped project must stay stopped while its volumes are copied. Something else, such as a volume backup that
	// stopped it and finishes, can start it again, and the bundle would then hold live data marked consistent. Its
	// containers' start times are compared across the copy, so even a start that stops again before the end is seen.
	var started map[string]string
	if opts.Request.StopProject {
		current, currentErr := s.deps.Environment.Project(ctx, opts.ProjectID)
		if currentErr != nil {
			return fmt.Errorf("check project %s: %w", manifest.Project.Name, currentErr)
		}
		if current.RunningCount > 0 {
			return common.Classify(common.ErrConflict, fmt.Errorf("%d container(s) of project %s are running; volume data was not copied", current.RunningCount, current.Name))
		}
		if started, err = s.containerStarts(ctx, manifest.Project.ComposeName); err != nil {
			return err
		}
	}
	written := map[string]bool{}
	for _, volume := range manifest.Volumes {
		if !volume.Included || written[volume.Archive] {
			continue
		}
		written[volume.Archive] = true
		session, exportErr := s.deps.Environment.ExportVolume(ctx, volume.Name)
		if exportErr != nil {
			return fmt.Errorf("export volume %s: %w", volume.Name, exportErr)
		}
		if appendErr := s.appendUpload(ctx, writer, volume.Archive, session); appendErr != nil {
			return fmt.Errorf("add volume %s: %w", volume.Name, appendErr)
		}
	}
	if opts.Request.StopProject {
		after, startsErr := s.containerStarts(ctx, manifest.Project.ComposeName)
		if startsErr != nil {
			return startsErr
		}
		for id, startedAt := range after {
			if startedAt != started[id] {
				return common.Classify(common.ErrConflict, fmt.Errorf(
					"project %s was started while its volumes were copied; export it again", manifest.Project.Name,
				))
			}
		}
	}
	return writer.Close()
}

// containerStarts returns when each container of the Compose project last started, keyed by ID; one that never
// started is left out, so a container created but not started between two reads doesn't count.
func (s *Service) containerStarts(ctx context.Context, composeName string) (map[string]string, error) {
	dockerClient, err := s.deps.Docker.GetClient(ctx)
	if err != nil {
		return nil, err
	}
	found, err := dockerClient.ContainerList(ctx, client.ContainerListOptions{
		All:     true,
		Filters: make(client.Filters).Add("label", docker.ComposeProjectLabelKey+"="+composeName),
	})
	if err != nil {
		return nil, fmt.Errorf("list containers: %w", err)
	}
	starts := make(map[string]string, len(found.Items))
	for _, c := range found.Items {
		inspected, inspectErr := dockerClient.ContainerInspect(ctx, c.ID, client.ContainerInspectOptions{})
		if errdefs.IsNotFound(inspectErr) {
			continue
		}
		if inspectErr != nil {
			return nil, fmt.Errorf("inspect container %s: %w", docker.ContainerNameFromNames(c.Names), inspectErr)
		}
		startedAt := kit.FromPtr(inspected.Container.State).StartedAt
		if at, parseErr := time.Parse(time.RFC3339Nano, startedAt); parseErr == nil && !at.IsZero() {
			starts[c.ID] = startedAt
		}
	}
	return starts, nil
}

func (s *Service) appendUpload(ctx context.Context, writer *projects.ProjectBundleWriter, name string, session types.UploadSession) error {
	file, consumed, cleanup, err := s.deps.Uploads.Consume(ctx, session.Kind, session.ID)
	if err != nil {
		return errors.Join(err, s.deps.Environment.DeleteUpload(context.WithoutCancel(ctx), session.Kind, session.ID))
	}
	defer cleanup()
	return writer.Add(name, consumed.Size, file)
}

func (s *Service) Import(ctx context.Context, opts ImportOptions) (string, error) {
	head, err := s.deps.Uploads.ReadChunk(ctx, types.UploadKindProjectArchive, opts.Request.UploadID, 0)
	if err != nil {
		return "", err
	}
	_, bundled, _ := projects.ReadProjectBundle(bytes.NewReader(head), nil)
	projectID, err := s.importProject(ctx, opts, bundled)
	if err != nil || !opts.Request.Start {
		return projectID, err
	}
	if deployErr := s.deps.Environment.DeployProject(ctx, projectID, opts.User); deployErr != nil {
		return "", fmt.Errorf("project %s was imported but could not be started: %w", opts.Request.Name, deployErr)
	}
	return projectID, nil
}

func (s *Service) importProject(ctx context.Context, opts ImportOptions, bundled bool) (string, error) {
	if !bundled {
		return s.deps.Environment.ImportProject(ctx, opts.Request, opts.User)
	}
	archive, _, cleanupSession, err := s.deps.Uploads.Consume(ctx, types.UploadKindProjectArchive, opts.Request.UploadID)
	if err != nil {
		return "", err
	}
	defer cleanupSession()
	manifest, _, err := projects.ReadProjectBundle(archive, nil)
	if err != nil {
		return "", common.Classify(common.ErrValidation, fmt.Errorf("invalid project bundle: %w", err))
	}
	if _, err = archive.Seek(0, io.SeekStart); err != nil {
		return "", fmt.Errorf("read project archive: %w", err)
	}
	var included []types.ProjectVolume
	for _, volume := range manifest.Volumes {
		if volume.Included {
			included = append(included, volume)
		}
	}
	if len(included) > 0 && !opts.RestoreVolumes {
		return "", common.Classify(common.ErrForbidden, errors.New("permission denied: restoring the bundle's volumes requires volumes:create and volumes:upload"))
	}
	composeName := projects.NormalizeProjectName(opts.Request.Name)
	stackExists, err := s.deps.Environment.ComposeProjectInUse(ctx, composeName)
	if err != nil {
		return "", fmt.Errorf("check Compose project %s: %w", composeName, err)
	}
	if stackExists {
		return "", common.Classify(common.ErrConflict, fmt.Errorf("docker already runs a Compose stack named %s; import the project under another name", composeName))
	}
	var certain, predicted []types.ProjectVolume
	for _, volume := range included {
		targetName := projects.ComposeVolumeTargetName(projects.ComposeVolumeTargetNameOptions{
			SourceProject: manifest.Project.ComposeName,
			Key:           volume.Key,
			SourceName:    volume.Name,
			TargetProject: opts.Request.Name,
		})
		if volume.Kind == types.ProjectVolumeExternal || targetName == volume.Name {
			certain = append(certain, volume)
			predicted = append(predicted, types.ProjectVolume{
				Key:  volume.Key,
				Name: volume.Name,
				Kind: volume.Kind,
			})
		}
	}
	if _, err = s.checkVolumeTargets(ctx, certain, predicted); err != nil {
		return "", err
	}
	var (
		projectID string
		target    importTarget
		created   []types.ProjectVolume
	)
	visit := func(name string, size int64, body io.Reader) error {
		if name == projects.ProjectBundleProject {
			files, importErr := s.deps.Uploads.IngestSession(ctx, types.UploadKindProjectArchive, projects.ProjectBundleProject, size, body)
			if importErr != nil {
				return fmt.Errorf("store the project files: %w", importErr)
			}
			projectID, importErr = s.deps.Environment.ImportProject(ctx, types.ImportProjectArchive{
				UploadID:     files.ID,
				Name:         opts.Request.Name,
				ProjectID:    opts.Request.ProjectID,
				Tags:         opts.Request.Tags,
				ComposeFiles: manifest.ComposeFiles,
			}, opts.User)
			if importErr != nil {
				return errors.Join(importErr, s.deps.Environment.DeleteUpload(context.WithoutCancel(ctx), types.UploadKindProjectArchive, files.ID))
			}
			target, importErr = s.importTarget(ctx, projectID, included)
			return importErr
		}
		index := target.restores[name]
		volume := target.volumes[index]
		external := volume.Kind == types.ProjectVolumeExternal
		restoreErr := s.restoreVolume(ctx, volumeRestore{
			User: opts.User,
			Body: body,
			Name: volume.Name,
			Compose: types.ComposeVolumeTarget{
				Labels:   kit.Ternary(external, included[index].DockerLabels, volume.Labels),
				Project:  target.composeName,
				Key:      volume.Key,
				Driver:   volume.Driver,
				External: external,
			},
			Size: size,
		})
		if !errors.Is(restoreErr, common.ErrConflict) {
			created = append(created, volume)
		}
		return restoreErr
	}
	if _, _, err = projects.ReadProjectBundle(archive, visit); err != nil {
		return "", s.rollback(ctx, err, importRollback{
			User:        opts.User,
			ProjectID:   projectID,
			ProjectName: opts.Request.Name,
			ComposeName: target.composeName,
			Created:     created,
		})
	}
	return projectID, nil
}

func (s *Service) importTarget(ctx context.Context, projectID string, included []types.ProjectVolume) (importTarget, error) {
	imported, err := s.deps.Environment.Project(ctx, projectID)
	if err != nil {
		return importTarget{}, fmt.Errorf("load the imported project: %w", err)
	}
	if imported.ComposeName == "" {
		return importTarget{}, fmt.Errorf("the compose files of the imported project %s cannot be loaded", imported.Name)
	}
	targets := map[string]types.ProjectVolume{}
	for _, volume := range projects.ClassifyProjectVolumes(imported) {
		targets[volume.Key] = volume
	}
	resolved := make([]types.ProjectVolume, 0, len(included))
	for _, volume := range included {
		target, ok := targets[volume.Key]
		if !ok || target.Kind != volume.Kind || !target.Included {
			return importTarget{composeName: imported.ComposeName}, fmt.Errorf("volume %s is not a Docker-managed %s volume of the imported project", volume.Key, volume.Kind)
		}
		resolved = append(resolved, target)
	}
	restores, err := s.checkVolumeTargets(ctx, included, resolved)
	return importTarget{
		restores:    restores,
		composeName: imported.ComposeName,
		volumes:     resolved,
	}, err
}

func (s *Service) checkVolumeTargets(ctx context.Context, included, targets []types.ProjectVolume) (map[string]int, error) {
	byName := map[string]int{}
	restores := map[string]int{}
	var conflicts []string
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
		_, exists, err := s.deps.Environment.Volume(ctx, target.Name)
		if err != nil {
			return nil, fmt.Errorf("inspect volume %s: %w", target.Name, err)
		}
		if exists {
			conflicts = append(conflicts, target.Name)
		}
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
	if err = s.deps.Environment.RestoreVolume(ctx, restore.Name, types.RestoreVolumeUploadRequest{
		ComposeVolume: &restore.Compose,
		UploadID:      session.ID,
	}, restore.User); err != nil {
		return errors.Join(
			fmt.Errorf("restore volume %s: %w", restore.Name, err),
			s.deps.Environment.DeleteUpload(context.WithoutCancel(ctx), types.UploadKindVolumeBackup, session.ID),
		)
	}
	return nil
}

func (s *Service) rollback(ctx context.Context, cause error, rollback importRollback) error {
	if rollback.ProjectID == "" {
		return cause
	}
	ctx = context.WithoutCancel(ctx)
	var leftovers []string
	if err := s.deps.Environment.DestroyProject(ctx, rollback.ProjectID, rollback.User); err != nil {
		leftovers = append(leftovers, fmt.Sprintf("project %s (%v)", rollback.ProjectName, err))
	}
	for _, volume := range rollback.Created {
		existing, exists, err := s.deps.Environment.Volume(ctx, volume.Name)
		owned := volume.Kind == types.ProjectVolumeExternal || (rollback.ComposeName != "" && docker.ComposeProjectLabel(existing.Labels) == rollback.ComposeName)
		if err == nil && exists && owned {
			err = s.deps.Environment.DeleteVolume(ctx, volume.Name, rollback.User)
		}
		if err != nil {
			leftovers = append(leftovers, fmt.Sprintf("volume %s (%v)", volume.Name, err))
		}
	}
	if len(leftovers) > 0 {
		return fmt.Errorf("%w; could not remove %s", cause, strings.Join(leftovers, ", "))
	}
	return fmt.Errorf("%w; the imported project and its volumes were removed", cause)
}
