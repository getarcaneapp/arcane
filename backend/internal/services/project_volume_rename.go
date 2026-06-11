package services

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	composetypes "github.com/compose-spec/compose-go/v2/types"
	cerrdefs "github.com/containerd/errdefs"
	"github.com/docker/compose/v5/pkg/api"
	composepkg "github.com/docker/compose/v5/pkg/compose"
	"github.com/getarcaneapp/arcane/backend/v2/internal/common"
	"github.com/getarcaneapp/arcane/backend/v2/internal/models"
	dockerutil "github.com/getarcaneapp/arcane/backend/v2/pkg/dockerutil"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/libarcane"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/projects"
	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/api/types/volume"
	"github.com/moby/moby/client"
)

type projectVolumeRenameMigrationInternal interface {
	Apply(context.Context) error
	Rollback(context.Context) error
}

type projectVolumeRenameEntryInternal struct {
	Key                 string
	OldName             string
	NewName             string
	OldVolume           volume.Volume
	NewConfig           composetypes.VolumeConfig
	TargetAlreadyExists bool
}

type dockerProjectVolumeRenameMigrationInternal struct {
	service        *ProjectService
	entries        []projectVolumeRenameEntryInternal
	createdNew     []projectVolumeRenameEntryInternal
	removedOld     []projectVolumeRenameEntryInternal
	oldComposeName string
	newComposeName string
}

var prepareProjectRenameVolumeMigrationInternal = func(ctx context.Context, svc *ProjectService, proj *models.Project, name *string, projectsDirectory string) (projectVolumeRenameMigrationInternal, error) {
	return svc.prepareProjectRenameVolumeMigrationInternal(ctx, proj, name, projectsDirectory)
}

func (s *ProjectService) prepareProjectRenameVolumeMigrationInternal(ctx context.Context, proj *models.Project, name *string, projectsDirectory string) (projectVolumeRenameMigrationInternal, error) {
	if s == nil || s.dockerService == nil || proj == nil || name == nil {
		return nil, nil
	}

	newProjectName := strings.TrimSpace(*name)
	if newProjectName == "" || proj.Name == newProjectName {
		return nil, nil
	}
	if proj.Status != models.ProjectStatusStopped {
		return nil, nil
	}

	oldComposeName := normalizeComposeProjectName(proj.Name)
	newComposeName := normalizeComposeProjectName(newProjectName)
	if oldComposeName == "" || newComposeName == "" || oldComposeName == newComposeName {
		return nil, nil
	}

	composeProject, _, err := s.loadComposeProjectForProjectInternal(ctx, proj, nil)
	if err != nil {
		var notFound *common.ProjectComposeFileNotFoundError
		if errors.As(err, &notFound) {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to load compose project for volume rename: %w", err)
	}
	if composeProject == nil || len(composeProject.Volumes) == 0 {
		return nil, nil
	}

	explicitVolumeNames, err := projects.ComposeVolumeKeysWithExplicitName(composeProject.ComposeFiles)
	if err != nil {
		return nil, fmt.Errorf("failed to parse compose volume names: %w", err)
	}

	dockerClient, err := s.dockerService.GetClient(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to Docker for volume rename: %w", err)
	}

	entries := make([]projectVolumeRenameEntryInternal, 0, len(composeProject.Volumes))
	for key, volumeConfig := range composeProject.Volumes {
		if _, explicit := explicitVolumeNames[key]; explicit {
			continue
		}
		if bool(volumeConfig.External) {
			continue
		}

		oldName := oldComposeName + "_" + key
		newName := newComposeName + "_" + key
		if volumeConfig.Name != oldName || oldName == newName {
			continue
		}

		targetVolume, targetExists, err := inspectProjectRenameTargetVolumeInternal(ctx, dockerClient, newName, newComposeName, key)
		if err != nil {
			return nil, err
		}

		oldVolume, err := dockerClient.VolumeInspect(ctx, oldName, client.VolumeInspectOptions{})
		if err != nil {
			if cerrdefs.IsNotFound(err) {
				if targetExists {
					slog.InfoContext(ctx, "project compose volume rename already completed", "oldVolume", oldName, "newVolume", targetVolume.Name)
				}
				continue
			}
			return nil, fmt.Errorf("inspect source volume %s: %w", oldName, err)
		}

		newConfig := buildProjectRenamedVolumeConfigInternal(volumeConfig, key, newName, newComposeName)

		entries = append(entries, projectVolumeRenameEntryInternal{
			Key:                 key,
			OldName:             oldName,
			NewName:             newName,
			OldVolume:           oldVolume.Volume,
			NewConfig:           newConfig,
			TargetAlreadyExists: targetExists,
		})
	}

	if len(entries) == 0 {
		return nil, nil
	}

	return &dockerProjectVolumeRenameMigrationInternal{
		service:        s,
		entries:        entries,
		oldComposeName: oldComposeName,
		newComposeName: newComposeName,
	}, nil
}

func (m *dockerProjectVolumeRenameMigrationInternal) Apply(ctx context.Context) error {
	if m == nil || len(m.entries) == 0 {
		return nil
	}
	if m.service == nil || m.service.dockerService == nil {
		return errors.New("docker service unavailable")
	}

	dockerClient, err := m.service.dockerService.GetClient(ctx)
	if err != nil {
		return fmt.Errorf("failed to connect to Docker: %w", err)
	}

	helperImage, err := getVolumeHelperImageInternal(ctx, m.service.dockerService, m.service.imageService, dockerClient)
	if err != nil {
		return err
	}

	for _, entry := range m.entries {
		if entry.TargetAlreadyExists {
			if err := removeProjectRenameStaleTargetVolumeInternal(ctx, dockerClient, entry.NewName); err != nil {
				return fmt.Errorf("remove stale target volume %s: %w", entry.NewName, err)
			}
		}

		if err := createProjectRenamedVolumeInternal(ctx, dockerClient, entry); err != nil {
			return errors.Join(err, m.rollbackCreatedTargets(ctx, dockerClient))
		}
		m.createdNew = append(m.createdNew, entry)

		if err := copyProjectVolumeDataInternal(ctx, dockerClient, helperImage, entry.OldName, entry.NewName); err != nil {
			return errors.Join(
				fmt.Errorf("copy volume data from %s to %s: %w", entry.OldName, entry.NewName, err),
				m.rollbackCreatedTargets(ctx, dockerClient),
			)
		}
	}

	for _, entry := range m.entries {
		if _, err := dockerClient.VolumeRemove(ctx, entry.OldName, client.VolumeRemoveOptions{Force: false}); err != nil {
			return errors.Join(
				fmt.Errorf("remove source volume %s: %w", entry.OldName, err),
				m.Rollback(ctx),
			)
		}
		m.removedOld = append(m.removedOld, entry)
	}

	dockerutil.InvalidateVolumeUsageCache()
	slog.InfoContext(ctx, "renamed project compose volumes", "oldProject", m.oldComposeName, "newProject", m.newComposeName, "count", len(m.entries))
	return nil
}

func (m *dockerProjectVolumeRenameMigrationInternal) Rollback(ctx context.Context) error {
	if m == nil || m.service == nil || m.service.dockerService == nil {
		return nil
	}

	dockerClient, err := m.service.dockerService.GetClient(ctx)
	if err != nil {
		return fmt.Errorf("failed to connect to Docker: %w", err)
	}

	var rollbackErr error
	if len(m.removedOld) > 0 {
		helperImage, err := getVolumeHelperImageInternal(ctx, m.service.dockerService, m.service.imageService, dockerClient)
		if err != nil {
			rollbackErr = errors.Join(rollbackErr, err)
		} else {
			for i := len(m.removedOld) - 1; i >= 0; i-- {
				entry := m.removedOld[i]
				if err := recreateProjectSourceVolumeInternal(ctx, dockerClient, entry); err != nil {
					rollbackErr = errors.Join(rollbackErr, err)
					continue
				}
				if err := copyProjectVolumeDataInternal(ctx, dockerClient, helperImage, entry.NewName, entry.OldName); err != nil {
					rollbackErr = errors.Join(rollbackErr, fmt.Errorf("restore volume data from %s to %s: %w", entry.NewName, entry.OldName, err))
				}
			}
		}
	}

	if err := m.rollbackCreatedTargets(ctx, dockerClient); err != nil {
		rollbackErr = errors.Join(rollbackErr, err)
	}
	if rollbackErr == nil {
		dockerutil.InvalidateVolumeUsageCache()
	}
	return rollbackErr
}

func (m *dockerProjectVolumeRenameMigrationInternal) rollbackCreatedTargets(ctx context.Context, dockerClient *client.Client) error {
	var rollbackErr error
	for i := len(m.createdNew) - 1; i >= 0; i-- {
		entry := m.createdNew[i]
		if err := removeProjectVolumeWithRetryInternal(ctx, dockerClient, entry.NewName, client.VolumeRemoveOptions{Force: true}); err != nil {
			rollbackErr = errors.Join(rollbackErr, fmt.Errorf("remove target volume %s: %w", entry.NewName, err))
		}
	}
	m.createdNew = nil
	return rollbackErr
}

func buildProjectRenamedVolumeConfigInternal(volumeConfig composetypes.VolumeConfig, key, newName, newComposeName string) composetypes.VolumeConfig {
	newConfig := volumeConfig
	newConfig.Name = newName
	newConfig.CustomLabels = composetypes.Labels{
		api.VolumeLabel:  key,
		api.ProjectLabel: newComposeName,
		api.VersionLabel: api.ComposeVersion,
	}
	return newConfig
}

func inspectProjectRenameTargetVolumeInternal(ctx context.Context, dockerClient *client.Client, newName, newComposeName, key string) (volume.Volume, bool, error) {
	targetVolume, err := dockerClient.VolumeInspect(ctx, newName, client.VolumeInspectOptions{})
	if err != nil {
		if cerrdefs.IsNotFound(err) {
			return volume.Volume{}, false, nil
		}
		return volume.Volume{}, false, fmt.Errorf("inspect target volume %s: %w", newName, err)
	}

	if !isProjectRenameComposeTargetVolumeInternal(targetVolume.Volume, newComposeName, key) {
		return volume.Volume{}, true, fmt.Errorf("target volume already exists: %s", newName)
	}

	return targetVolume.Volume, true, nil
}

func isProjectRenameComposeTargetVolumeInternal(targetVolume volume.Volume, newComposeName, key string) bool {
	labels := targetVolume.Labels
	if labels == nil {
		return false
	}
	return labels[api.ProjectLabel] == newComposeName && labels[api.VolumeLabel] == key
}

func createProjectRenamedVolumeInternal(ctx context.Context, dockerClient *client.Client, entry projectVolumeRenameEntryInternal) error {
	labels := map[string]string{}
	for k, v := range entry.OldVolume.Labels {
		labels[k] = v
	}
	for k, v := range entry.NewConfig.Labels {
		labels[k] = v
	}
	for k, v := range entry.NewConfig.CustomLabels {
		labels[k] = v
	}

	hash, err := composepkg.VolumeHash(entry.NewConfig)
	if err != nil {
		return fmt.Errorf("hash target volume %s: %w", entry.NewName, err)
	}
	labels[api.ConfigHashLabel] = hash

	driver := strings.TrimSpace(entry.OldVolume.Driver)
	if driver == "" {
		driver = entry.NewConfig.Driver
	}

	_, err = dockerClient.VolumeCreate(ctx, client.VolumeCreateOptions{
		Name:       entry.NewName,
		Driver:     driver,
		DriverOpts: entry.OldVolume.Options,
		Labels:     labels,
	})
	if err != nil {
		return fmt.Errorf("create target volume %s: %w", entry.NewName, err)
	}
	return nil
}

func removeProjectRenameStaleTargetVolumeInternal(ctx context.Context, dockerClient *client.Client, targetVolume string) error {
	if err := removeProjectVolumeHelperContainersInternal(ctx, dockerClient, targetVolume); err != nil {
		slog.WarnContext(ctx, "failed to remove project volume helper containers before stale target cleanup", "volume", targetVolume, "error", err)
	}
	return removeProjectVolumeWithRetryInternal(ctx, dockerClient, targetVolume, client.VolumeRemoveOptions{Force: false})
}

func removeProjectVolumeHelperContainersInternal(ctx context.Context, dockerClient *client.Client, volumeName string) error {
	containers, err := dockerClient.ContainerList(ctx, client.ContainerListOptions{All: true})
	if err != nil {
		return fmt.Errorf("list containers for helper cleanup: %w", err)
	}

	var removeErr error
	for _, c := range containers.Items {
		if !libarcane.IsInternalContainer(c.Labels) || !containerSummaryMountsVolumeInternal(c, volumeName) {
			continue
		}
		if _, err := dockerClient.ContainerRemove(ctx, c.ID, volumeHelperRemoveOptionsInternal()); err != nil && !cerrdefs.IsNotFound(err) {
			removeErr = errors.Join(removeErr, fmt.Errorf("remove helper container %s: %w", c.ID, err))
		}
	}
	return removeErr
}

func containerSummaryMountsVolumeInternal(c container.Summary, volumeName string) bool {
	for _, mount := range c.Mounts {
		if mount.Name == volumeName || mount.Source == volumeName {
			return true
		}
	}
	return false
}

func removeProjectVolumeWithRetryInternal(ctx context.Context, dockerClient *client.Client, volumeName string, options client.VolumeRemoveOptions) error {
	var err error
	for attempt := 0; attempt < 3; attempt++ {
		_, err = dockerClient.VolumeRemove(ctx, volumeName, options)
		if err == nil || cerrdefs.IsNotFound(err) {
			return nil
		}
		if attempt == 2 {
			break
		}
		select {
		case <-ctx.Done():
			return errors.Join(ctx.Err(), err)
		case <-time.After(200 * time.Millisecond):
		}
	}
	return err
}

func recreateProjectSourceVolumeInternal(ctx context.Context, dockerClient *client.Client, entry projectVolumeRenameEntryInternal) error {
	if _, err := dockerClient.VolumeInspect(ctx, entry.OldName, client.VolumeInspectOptions{}); err == nil {
		return nil
	} else if !cerrdefs.IsNotFound(err) {
		return fmt.Errorf("inspect source rollback volume %s: %w", entry.OldName, err)
	}

	_, err := dockerClient.VolumeCreate(ctx, client.VolumeCreateOptions{
		Name:       entry.OldName,
		Driver:     entry.OldVolume.Driver,
		DriverOpts: entry.OldVolume.Options,
		Labels:     entry.OldVolume.Labels,
	})
	if err != nil {
		return fmt.Errorf("recreate source volume %s: %w", entry.OldName, err)
	}
	return nil
}

func copyProjectVolumeDataInternal(ctx context.Context, dockerClient *client.Client, helperImage, sourceVolume, targetVolume string) error {
	config := &container.Config{
		Image:           helperImage,
		Cmd:             []string{"sh", "-c", "set -e; cd /from; tar -cf - . | tar -xf - -C /to"},
		NetworkDisabled: true,
		Labels:          buildVolumeHelperLabelsInternal(),
	}

	hostConfig := buildVolumeHelperHostConfigInternal(helperImage, []string{
		sourceVolume + ":/from:ro",
		targetVolume + ":/to",
	}, nil)

	resp, err := dockerClient.ContainerCreate(ctx, client.ContainerCreateOptions{
		Config:     config,
		HostConfig: hostConfig,
	})
	if err != nil {
		return fmt.Errorf("create volume copy container: %w", err)
	}

	cleanup := func() {
		if _, err := dockerClient.ContainerRemove(ctx, resp.ID, volumeHelperRemoveOptionsInternal()); err != nil && !cerrdefs.IsNotFound(err) {
			slog.WarnContext(ctx, "failed to remove volume copy helper", "containerID", resp.ID, "error", err)
		}
	}
	defer cleanup()

	if _, err := dockerClient.ContainerStart(ctx, resp.ID, client.ContainerStartOptions{}); err != nil {
		return fmt.Errorf("start volume copy container: %w", err)
	}

	waitResult := dockerClient.ContainerWait(ctx, resp.ID, client.ContainerWaitOptions{Condition: container.WaitConditionNotRunning})
	select {
	case err := <-waitResult.Error:
		if err != nil {
			return err
		}
	case waitBody := <-waitResult.Result:
		if waitBody.StatusCode != 0 {
			return fmt.Errorf("volume copy container exited with code %d", waitBody.StatusCode)
		}
	case <-ctx.Done():
		return ctx.Err()
	}

	return nil
}
