package bundle

import (
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

	composetypes "github.com/compose-spec/compose-go/v2/types"
	"github.com/containerd/errdefs"
	composeapi "github.com/docker/compose/v5/pkg/api"
	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/api/types/mount"
	"github.com/moby/moby/client"

	"kit.getarcane.app"
	"kit.getarcane.app/concurrency"
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
	Dependencies struct {
		Docker         *dockerInternal.DockerClientService
		Uploads        *upload.UploadService
		VolumeLocks    *concurrency.KeyedMutex
		Project        func(ctx context.Context, projectID string, opts types.ProjectDetailsOptions) (types.Project, error)
		ComposeFiles   func(ctx context.Context, projectID string) ([]string, error)
		ComposeProject func(ctx context.Context, projectID string) (*composetypes.Project, error)
		Lock           func(ctx context.Context, op, projectID string) (func(), error)
		ExportProject  func(ctx context.Context, projectID string) (*types.UploadSession, error)
		Import         func(ctx context.Context, archive ProjectArchive, user types.Actor) (string, error)
		Deploy         func(ctx context.Context, projectID string, user types.Actor, options *types.ProjectDeployOptions) error
		Destroy        func(ctx context.Context, projectID string, opts types.ProjectDestroyOptions, user types.Actor) error
		Volume         func(ctx context.Context, name string) (types.Volume, bool, error)
		ExportVolume   func(ctx context.Context, name string) (*types.UploadSession, error)
		RestoreVolume  func(ctx context.Context, name string, request types.RestoreVolumeUploadRequest, user types.Actor) (bool, error)
		DeleteVolume   func(ctx context.Context, name string, force bool, user types.Actor) error
		DeleteUpload   func(ctx context.Context, kind, uploadID string) error
	}

	Service struct {
		deps Dependencies
	}

	ProjectArchive struct {
		ComposeFiles []string
		Archive      io.Reader
		Request      types.ImportProjectArchive
	}

	ExportOptions struct {
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

var projectDetails = types.ProjectDetailsOptions{
	IncludeServiceConfigs: true,
	IncludeIncludeFiles:   true,
}

func New(deps Dependencies) *Service {
	return &Service{deps: deps}
}

func (s *Service) Options(ctx context.Context, projectID string) (types.ProjectExportOptions, error) {
	project, err := s.deps.Project(ctx, projectID, projectDetails)
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
		return s.deps.ExportProject(ctx, opts.ProjectID)
	}
	if opts.Request.StopProject {
		unlock, lockErr := s.deps.Lock(ctx, "export project", opts.ProjectID)
		if lockErr != nil {
			return nil, lockErr
		}
		defer unlock()
	}
	project, err := s.deps.Project(ctx, opts.ProjectID, projectDetails)
	if err != nil {
		return nil, err
	}
	manifest, err := s.manifest(ctx, project, opts.Request)
	if err != nil {
		return nil, err
	}
	write := func(w io.Writer) error {
		return s.writeBundle(ctx, w, opts, manifest)
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
		existing, exists, inspectErr := s.deps.Volume(ctx, volume.Name)
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

func (s *Service) manifest(ctx context.Context, project types.Project, request types.ExportProjectArchive) (types.ProjectManifest, error) {
	if project.ComposeName == "" {
		return types.ProjectManifest{}, common.Classify(common.ErrValidation, fmt.Errorf("the compose files of project %s cannot be loaded; fix them before exporting its volumes", project.Name))
	}
	composeFiles, err := s.deps.ComposeFiles(ctx, project.ID)
	if err != nil {
		return types.ProjectManifest{}, err
	}
	state, err := s.exportState(ctx, project)
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
	includeFiles := make([]string, 0, len(project.IncludeFiles))
	for _, file := range project.IncludeFiles {
		includeFiles = append(includeFiles, file.RelativePath)
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
		IncludeFiles:  includeFiles,
		Volumes:       manifestVolumes,
		FormatVersion: types.ProjectManifestFormatVersion,
		Consistent:    consistent,
	}, nil
}

func (s *Service) writeBundle(ctx context.Context, w io.Writer, opts ExportOptions, manifest types.ProjectManifest) (err error) {
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
	names := make([]string, 0, len(manifest.Volumes))
	for _, volume := range manifest.Volumes {
		if volume.Name != "" {
			names = append(names, volume.Name)
		}
	}
	slices.Sort(names)
	held := slices.Compact(names)
	unlocks := make([]func(), 0, len(held))
	defer func() {
		for _, unlock := range slices.Backward(unlocks) {
			unlock()
		}
	}()
	for _, volumeName := range held {
		unlock, lockErr := s.deps.VolumeLocks.LockContext(ctx, volumeName)
		if lockErr != nil {
			return lockErr
		}
		unlocks = append(unlocks, unlock)
	}
	ctx = volumes.WithLocked(ctx, held)
	if opts.Request.StopProject {
		restart, loadErr := s.deps.ComposeProject(ctx, opts.ProjectID)
		if loadErr != nil {
			return fmt.Errorf("load compose project: %w", loadErr)
		}
		stopped, stopErr := s.stopRunningContainers(ctx, manifest.Project.ComposeName)
		defer func() {
			if startErr := s.startContainers(context.WithoutCancel(ctx), restart, stopped); startErr != nil {
				err = errors.Join(err, fmt.Errorf("start project %s again: %w", manifest.Project.Name, startErr))
			}
		}()
		if stopErr != nil {
			return fmt.Errorf("stop project %s: %w", manifest.Project.Name, stopErr)
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
	return writer.Close()
}

func (s *Service) appendUpload(ctx context.Context, writer *projects.ProjectBundleWriter, name string, session *types.UploadSession) error {
	file, consumed, cleanup, err := s.deps.Uploads.Consume(ctx, session.Kind, session.ID)
	if err != nil {
		return errors.Join(err, s.deps.DeleteUpload(context.WithoutCancel(ctx), session.Kind, session.ID))
	}
	defer cleanup()
	return writer.Add(name, consumed.Size, file)
}

func composeContainers(ctx context.Context, dockerClient *client.Client, composeName string) ([]container.Summary, error) {
	found, err := dockerClient.ContainerList(ctx, client.ContainerListOptions{
		All:     true,
		Filters: make(client.Filters).Add("label", docker.ComposeProjectLabelKey+"="+composeName),
	})
	if err != nil {
		return nil, fmt.Errorf("list containers: %w", err)
	}
	return found.Items, nil
}

func (s *Service) stopRunningContainers(ctx context.Context, composeName string) ([]container.Summary, error) {
	dockerClient, err := s.deps.Docker.GetClient(ctx)
	if err != nil {
		return nil, err
	}
	var stopped []container.Summary
	for pass := range 2 {
		found, listErr := composeContainers(ctx, dockerClient, composeName)
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
		for _, c := range running {
			if c.Labels[composeapi.OneoffLabel] != "True" && c.Labels[projects.JobRunLabel] == "" {
				continue
			}
			inspected, inspectErr := dockerClient.ContainerInspect(ctx, c.ID, client.ContainerInspectOptions{})
			if inspectErr != nil {
				return stopped, fmt.Errorf("inspect container %s: %w", docker.ContainerNameFromNames(c.Names), inspectErr)
			}
			if kit.FromPtr(inspected.Container.HostConfig).AutoRemove {
				return stopped, common.Classify(common.ErrConflict, fmt.Errorf(
					"container %s removes itself when it stops (docker compose run --rm); wait for it to finish, then try again",
					docker.ContainerNameFromNames(c.Names),
				))
			}
		}
		for _, c := range running {
			stopped = append(stopped, c)
			if _, err = dockerClient.ContainerStop(context.WithoutCancel(ctx), c.ID, client.ContainerStopOptions{}); err != nil {
				return stopped, fmt.Errorf("stop container %s: %w", docker.ContainerNameFromNames(c.Names), err)
			}
			if ctxErr := ctx.Err(); ctxErr != nil {
				return stopped, ctxErr
			}
		}
	}
	return stopped, nil
}

func (s *Service) startContainers(ctx context.Context, compose *composetypes.Project, stopped []container.Summary) error {
	if len(stopped) == 0 {
		return nil
	}
	dockerClient, err := s.deps.Docker.GetClient(ctx)
	if err != nil {
		return err
	}
	var errs []error
	found, listErr := composeContainers(ctx, dockerClient, compose.Name)
	if listErr != nil {
		errs = append(errs, listErr)
	}
	wasStopped := map[string]bool{}
	for _, c := range stopped {
		wasStopped[c.ID] = true
	}
	direct := map[string]bool{}
	for _, c := range found {
		if c.Labels[composeapi.OneoffLabel] != "True" && c.Labels[projects.JobRunLabel] == "" && !wasStopped[c.ID] && c.State != container.StateRunning {
			direct[c.Labels[composeapi.ServiceLabel]] = true
		}
	}
	var services, containerIDs, serviceContainerIDs []string
	for _, c := range stopped {
		service := c.Labels[composeapi.ServiceLabel]
		_, enabled := compose.Services[service]
		_, disabled := compose.DisabledServices[service]
		switch {
		case listErr != nil || direct[service] || (!enabled && !disabled) || c.Labels[composeapi.OneoffLabel] == "True" || c.Labels[projects.JobRunLabel] != "":
			containerIDs = append(containerIDs, c.ID)
		default:
			serviceContainerIDs = append(serviceContainerIDs, c.ID)
			if !slices.Contains(services, service) {
				services = append(services, service)
			}
		}
	}
	if len(services) > 0 {
		if startErr := projects.ComposeStart(ctx, types.ComposeTarget{
			Project:    compose,
			DockerHost: s.deps.Docker.DockerHost(),
			Services:   services,
		}); startErr != nil {
			errs = append(errs, fmt.Errorf("start services %s: %w", strings.Join(services, ", "), startErr))
			containerIDs = append(containerIDs, serviceContainerIDs...)
		}
	}
	for _, id := range containerIDs {
		if _, startErr := dockerClient.ContainerStart(ctx, id, client.ContainerStartOptions{}); startErr != nil && !errdefs.IsNotFound(startErr) {
			errs = append(errs, fmt.Errorf("start container %s: %w", id, startErr))
		}
	}
	return errors.Join(errs...)
}

func (s *Service) Import(ctx context.Context, opts ImportOptions) (string, error) {
	archive, _, cleanupSession, err := s.deps.Uploads.Consume(ctx, types.UploadKindProjectArchive, opts.Request.UploadID)
	if err != nil {
		return "", err
	}
	defer cleanupSession()
	manifest, bundled, err := projects.ReadProjectBundle(archive, nil)
	if err != nil {
		return "", common.Classify(common.ErrValidation, fmt.Errorf("invalid project bundle: %w", err))
	}
	if _, err = archive.Seek(0, io.SeekStart); err != nil {
		return "", fmt.Errorf("read project archive: %w", err)
	}
	request := types.ImportProjectArchive{
		UploadID:  opts.Request.UploadID,
		Name:      opts.Request.Name,
		ProjectID: cmp.Or(opts.Request.ProjectID, uuid.New().String()),
		Tags:      opts.Request.Tags,
		Start:     opts.Request.Start,
	}
	unlock, err := s.deps.Lock(ctx, "import project", request.ProjectID)
	if err != nil {
		return "", err
	}
	defer unlock()
	projectID, err := s.importArchive(ctx, archive, manifest, bundled, ImportOptions{
		User:           opts.User,
		Request:        request,
		RestoreVolumes: opts.RestoreVolumes,
	})
	if err != nil || !request.Start {
		return projectID, err
	}
	if deployErr := s.deps.Deploy(ctx, projectID, opts.User, nil); deployErr != nil {
		return "", fmt.Errorf("project %s was imported but could not be started: %w", request.Name, deployErr)
	}
	return projectID, nil
}

func (s *Service) importArchive(ctx context.Context, archive io.Reader, manifest types.ProjectManifest, bundled bool, opts ImportOptions) (string, error) {
	if !bundled {
		return s.deps.Import(ctx, ProjectArchive{
			Archive: archive,
			Request: opts.Request,
		}, opts.User)
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
	dockerClient, err := s.deps.Docker.GetClient(ctx)
	if err != nil {
		return "", err
	}
	composeName := projects.NormalizeProjectName(opts.Request.Name)
	stack, err := composeContainers(ctx, dockerClient, composeName)
	if err != nil {
		return "", fmt.Errorf("check Compose project %s: %w", composeName, err)
	}
	if len(stack) > 0 {
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
			var importErr error
			projectID, importErr = s.deps.Import(ctx, ProjectArchive{
				Archive:      body,
				Request:      opts.Request,
				ComposeFiles: manifest.ComposeFiles,
			}, opts.User)
			if importErr != nil {
				return importErr
			}
			target, importErr = s.importTarget(ctx, projectID, included)
			return importErr
		}
		index := target.restores[name]
		volume := target.volumes[index]
		external := volume.Kind == types.ProjectVolumeExternal
		restored, restoreErr := s.restoreVolume(ctx, volumeRestore{
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
		if restored {
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
	imported, err := s.deps.Project(ctx, projectID, projectDetails)
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
		_, exists, err := s.deps.Volume(ctx, target.Name)
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

func (s *Service) restoreVolume(ctx context.Context, restore volumeRestore) (bool, error) {
	session, err := s.deps.Uploads.IngestSession(ctx, types.UploadKindVolumeBackup, restore.Compose.Key+".tar.gz", restore.Size, restore.Body)
	if err != nil {
		return false, fmt.Errorf("store the data of volume %s: %w", restore.Compose.Key, err)
	}
	created, err := s.deps.RestoreVolume(ctx, restore.Name, types.RestoreVolumeUploadRequest{
		ComposeVolume: &restore.Compose,
		UploadID:      session.ID,
	}, restore.User)
	if err != nil {
		return created, errors.Join(
			fmt.Errorf("restore volume %s: %w", restore.Name, err),
			s.deps.DeleteUpload(context.WithoutCancel(ctx), types.UploadKindVolumeBackup, session.ID),
		)
	}
	return created, nil
}

func (s *Service) rollback(ctx context.Context, cause error, rollback importRollback) error {
	if rollback.ProjectID == "" {
		return cause
	}
	ctx = context.WithoutCancel(ctx)
	var leftovers []string
	if err := s.deps.Destroy(ctx, rollback.ProjectID, types.ProjectDestroyOptions{
		RemoveFiles: true,
		SkipDown:    true,
	}, rollback.User); err != nil {
		leftovers = append(leftovers, fmt.Sprintf("project %s (%v)", rollback.ProjectName, err))
	}
	for _, volume := range rollback.Created {
		existing, exists, err := s.deps.Volume(ctx, volume.Name)
		owned := volume.Kind == types.ProjectVolumeExternal || (rollback.ComposeName != "" && docker.ComposeProjectLabel(existing.Labels) == rollback.ComposeName)
		if err == nil && exists && owned {
			err = s.deps.DeleteVolume(ctx, volume.Name, false, rollback.User)
		}
		if err != nil && !errdefs.IsNotFound(err) {
			leftovers = append(leftovers, fmt.Sprintf("volume %s (%v)", volume.Name, err))
		}
	}
	if len(leftovers) > 0 {
		return fmt.Errorf("%w; could not remove %s", cause, strings.Join(leftovers, ", "))
	}
	return fmt.Errorf("%w; the imported project and its volumes were removed", cause)
}
