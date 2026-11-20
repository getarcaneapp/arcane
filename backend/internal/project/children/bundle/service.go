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
		DeleteVolume(ctx context.Context, name string, user types.Actor) error
		DeleteUpload(ctx context.Context, kind, uploadID string) error
	}

	Dependencies struct {
		Environment  Environment
		Docker       *dockerInternal.DockerClientService
		Uploads      *upload.UploadService
		ComposeFiles func(ctx context.Context, projectID string) ([]string, error)
		// BackupStore names the volume holding Arcane's local backups, which an export never copies.
		BackupStore   func(ctx context.Context) string
		LockProject   func(ctx context.Context, op, projectID string) (func(), error)
		StopProject   func(ctx context.Context, projectID string, user types.Actor) error
		StartProject  func(ctx context.Context, projectID string, user types.Actor, options *types.ProjectDeployOptions) error
		RemoveProject func(ctx context.Context, projectID string, opts types.ProjectDestroyOptions, user types.Actor) error
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
		docker     map[string]types.Volume
		volumes    []types.ProjectVolume
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
		Claim       string
		Attempted   []types.ProjectVolume
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
	unlock, err := s.deps.LockProject(ctx, "export project", opts.ProjectID)
	if err != nil {
		return nil, err
	}
	defer unlock()
	project, err := s.deps.Environment.Project(ctx, opts.ProjectID)
	if err != nil {
		return nil, err
	}
	state, err := s.exportState(ctx, project)
	if err != nil {
		return nil, err
	}
	if state.active && opts.Request.StopProject {
		if state.runsArcane {
			return nil, common.Classify(common.ErrValidation, fmt.Errorf("project %s runs Arcane itself, so it cannot be stopped for the export; export it without stopping it", project.Name))
		}
		if unnamedErr := s.refuseUnnamedVolumes(ctx, project); unnamedErr != nil {
			return nil, unnamedErr
		}
	}
	manifest, err := s.manifest(ctx, project, opts.Request, state)
	if err != nil {
		return nil, err
	}
	write := func(w io.Writer) error {
		return s.writeBundle(ctx, w, opts, manifest, state.active && opts.Request.StopProject)
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
	running, err := dockerClient.ContainerList(ctx, client.ContainerListOptions{})
	if err != nil {
		return exportState{}, fmt.Errorf("list containers: %w", err)
	}
	activeContainers, inUse := projects.ProjectContainerUse(project.ComposeName, running.Items)
	arcaneOfProject := func(c container.Summary) bool {
		return project.ComposeName != "" && docker.ComposeProjectLabel(c.Labels) == project.ComposeName && labels.IsArcaneContainer(c.Labels)
	}
	resolved, inspected, err := projects.ResolveProjectVolumes(ctx, projects.ResolveProjectVolumesOptions{
		Project:     project,
		Inspect:     s.deps.Environment.Volume,
		InUse:       inUse,
		BackupStore: s.deps.BackupStore(ctx),
	})
	if err != nil {
		return exportState{}, err
	}
	return exportState{
		docker:     inspected,
		volumes:    resolved,
		active:     len(activeContainers) > 0,
		runsArcane: slices.ContainsFunc(running.Items, arcaneOfProject),
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
			if startErr := s.deps.StartProject(context.WithoutCancel(ctx), opts.ProjectID, opts.User, nil); startErr != nil {
				err = errors.Join(err, fmt.Errorf("start project %s again: %w", manifest.Project.Name, startErr))
			}
		}()
		if downErr := s.deps.StopProject(ctx, opts.ProjectID, opts.User); downErr != nil {
			return fmt.Errorf("stop project %s: %w", manifest.Project.Name, downErr)
		}
	}
	if opts.Request.StopProject {
		current, currentErr := s.deps.Environment.Project(ctx, opts.ProjectID)
		if currentErr != nil {
			return fmt.Errorf("check project %s: %w", manifest.Project.Name, currentErr)
		}
		if current.RunningCount > 0 {
			return common.Classify(common.ErrConflict, fmt.Errorf("%d container(s) of project %s are running; volume data was not copied", current.RunningCount, current.Name))
		}
	}
	if manifest.Consistent {
		var copied []string
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
		session, exportErr := s.deps.Environment.ExportVolume(ctx, volume.Name)
		if exportErr != nil {
			return fmt.Errorf("export volume %s: %w", volume.Name, exportErr)
		}
		if appendErr := s.appendUpload(ctx, writer, volume.Archive, session); appendErr != nil {
			return fmt.Errorf("add volume %s: %w", volume.Name, appendErr)
		}
	}
	return writer.Close()
}

func (s *Service) refuseUnnamedVolumes(ctx context.Context, project types.Project) error {
	dockerClient, err := s.deps.Docker.GetClient(ctx)
	if err != nil {
		return err
	}
	found, err := dockerClient.ContainerList(ctx, client.ContainerListOptions{
		All:     true,
		Filters: make(client.Filters).Add("label", docker.ComposeProjectLabelKey+"="+project.ComposeName),
	})
	if err != nil {
		return fmt.Errorf("list containers: %w", err)
	}
	if c, m, detached := projects.UnnamedVolumeMount(project, found.Items); detached {
		return common.Classify(common.ErrValidation, fmt.Errorf(
			"container %s keeps data at %s in an unnamed volume, which stopping the project for the export would detach; "+
				"export it without stopping it, or move that data to a named volume",
			docker.ContainerNameFromNames(c.Names), m.Destination,
		))
	}
	return nil
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
	bundled := projects.IsProjectBundle(bytes.NewReader(head))
	request := types.ImportProjectArchive{
		UploadID:     opts.Request.UploadID,
		Name:         opts.Request.Name,
		ProjectID:    cmp.Or(opts.Request.ProjectID, uuid.New().String()),
		Tags:         opts.Request.Tags,
		ComposeFiles: opts.Request.ComposeFiles,
		Start:        opts.Request.Start,
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
	if deployErr := s.deps.StartProject(ctx, projectID, opts.User, nil); deployErr != nil {
		return "", fmt.Errorf("project %s was imported but could not be started: %w", request.Name, deployErr)
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
	checkSpace := func(_ string, size int64, _ io.Reader) error {
		return s.deps.Uploads.CheckFreeSpace(ctx, size)
	}
	manifest, _, err := projects.ReadProjectBundle(archive, checkSpace)
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
	claim := uuid.New().String()
	var (
		projectID string
		target    importTarget
		attempted []types.ProjectVolume
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
				Claim:    claim,
				External: external,
			},
			Size: size,
		})
		attempted = append(attempted, volume)
		return restoreErr
	}
	if _, _, err = projects.ReadProjectBundle(archive, visit); err != nil {
		return "", s.rollback(ctx, err, importRollback{
			User:        opts.User,
			ProjectID:   projectID,
			ProjectName: opts.Request.Name,
			Claim:       claim,
			Attempted:   attempted,
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
	if err := s.deps.RemoveProject(ctx, rollback.ProjectID, types.ProjectDestroyOptions{RemoveFiles: true}, rollback.User); err != nil {
		leftovers = append(leftovers, fmt.Sprintf("project %s (%v)", rollback.ProjectName, err))
	}
	for _, volume := range rollback.Attempted {
		existing, exists, err := s.deps.Environment.Volume(ctx, volume.Name)
		if err == nil && exists && existing.Labels[volumes.ClaimLabel] == rollback.Claim {
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
