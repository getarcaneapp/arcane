package services

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"maps"
	"slices"
	"strings"
	"syscall"
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
	"github.com/moby/moby/api/pkg/stdcopy"
	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/api/types/volume"
	"github.com/moby/moby/client"
)

type projectVolumeRenameMigrationInternal interface {
	Apply(ctx context.Context) error
	Rollback(ctx context.Context) error
}

type projectVolumeRenameCommitterInternal interface {
	Commit(ctx context.Context) error
}

type projectVolumeRenameJournalSourceInternal interface {
	JournalVolumes() []projectRenameJournalVolumeInternal
}

type projectVolumeRenameEntryInternal struct {
	Key       string
	OldName   string
	NewName   string
	OldVolume volume.Volume
	NewConfig composetypes.VolumeConfig
}

type dockerProjectVolumeRenameMigrationInternal struct {
	service        *ProjectService
	entries        []projectVolumeRenameEntryInternal
	createdNew     []projectVolumeRenameEntryInternal
	removedOld     []projectVolumeRenameEntryInternal
	oldComposeName string
	newComposeName string
}

type projectVolumeCopyRuntimeInternal struct {
	Image   string
	Command []string
	Source  string
}

type projectVolumeCopyProbeInternal struct {
	Path           string `json:"path"`
	AllocatedBytes uint64 `json:"allocatedBytes"`
	AvailableBytes uint64 `json:"availableBytes"`
}

type projectVolumeHelperLogsInternal struct {
	Stdout string
	Stderr string
}

const (
	projectVolumeCopyMountPathInternal      = "/volume"
	projectVolumeCopyMinMarginBytesInternal = 256 * 1024 * 1024
)

func (s *ProjectService) prepareProjectRenameVolumeMigrationInternal(ctx context.Context, proj *models.Project, name *string) (projectVolumeRenameMigrationInternal, error) {
	oldComposeName, newComposeName, ok := projectRenameVolumeMigrationComposeNamesInternal(s, proj, name)
	if !ok {
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

	entries, err := collectProjectRenameVolumeEntriesInternal(ctx, dockerClient, composeProject.Volumes, explicitVolumeNames, oldComposeName, newComposeName)
	if err != nil {
		return nil, err
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

func projectRenameVolumeMigrationComposeNamesInternal(s *ProjectService, proj *models.Project, name *string) (string, string, bool) {
	if s == nil || s.dockerService == nil || proj == nil || name == nil {
		return "", "", false
	}

	newProjectName := strings.TrimSpace(*name)
	if newProjectName == "" || proj.Name == newProjectName || proj.Status != models.ProjectStatusStopped {
		return "", "", false
	}

	oldComposeName := normalizeComposeProjectName(proj.Name)
	newComposeName := normalizeComposeProjectName(newProjectName)
	if oldComposeName == "" || newComposeName == "" || oldComposeName == newComposeName {
		return "", "", false
	}

	return oldComposeName, newComposeName, true
}

func collectProjectRenameVolumeEntriesInternal(ctx context.Context, dockerClient *client.Client, volumes map[string]composetypes.VolumeConfig, explicitVolumeNames map[string]struct{}, oldComposeName, newComposeName string) ([]projectVolumeRenameEntryInternal, error) {
	entries := make([]projectVolumeRenameEntryInternal, 0, len(volumes))
	for key, volumeConfig := range volumes {
		entry, ok, err := inspectProjectRenameVolumeEntryInternal(ctx, dockerClient, key, volumeConfig, explicitVolumeNames, oldComposeName, newComposeName)
		if err != nil {
			return nil, err
		}
		if ok {
			entries = append(entries, entry)
		}
	}
	return entries, nil
}

func inspectProjectRenameVolumeEntryInternal(ctx context.Context, dockerClient *client.Client, key string, volumeConfig composetypes.VolumeConfig, explicitVolumeNames map[string]struct{}, oldComposeName, newComposeName string) (projectVolumeRenameEntryInternal, bool, error) {
	if _, explicit := explicitVolumeNames[key]; explicit || bool(volumeConfig.External) {
		return projectVolumeRenameEntryInternal{}, false, nil
	}

	oldName := oldComposeName + "_" + key
	newName := newComposeName + "_" + key
	if oldName == newName || (volumeConfig.Name != oldName && volumeConfig.Name != newName) {
		return projectVolumeRenameEntryInternal{}, false, nil
	}

	if err := ensureProjectRenameTargetVolumeAbsentInternal(ctx, dockerClient, newName); err != nil {
		return projectVolumeRenameEntryInternal{}, false, err
	}

	oldVolume, err := dockerClient.VolumeInspect(ctx, oldName, client.VolumeInspectOptions{})
	if err != nil {
		if cerrdefs.IsNotFound(err) {
			return projectVolumeRenameEntryInternal{}, false, nil
		}
		return projectVolumeRenameEntryInternal{}, false, fmt.Errorf("inspect source volume %s: %w", oldName, err)
	}
	if err := ensureProjectRenameSourceVolumeDetachedInternal(ctx, dockerClient, oldName); err != nil {
		return projectVolumeRenameEntryInternal{}, false, err
	}

	return projectVolumeRenameEntryInternal{
		Key:       key,
		OldName:   oldName,
		NewName:   newName,
		OldVolume: oldVolume.Volume,
		NewConfig: buildProjectRenamedVolumeConfigInternal(volumeConfig, key, newName, newComposeName),
	}, true, nil
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

	copyRuntime, err := getProjectVolumeCopyRuntimeInternal(ctx, dockerClient)
	if err != nil {
		return err
	}

	for _, entry := range m.entries {
		if err := createProjectRenamedVolumeInternal(ctx, dockerClient, entry); err != nil {
			return errors.Join(err, m.rollbackCreatedTargets(ctx, dockerClient))
		}
		m.createdNew = append(m.createdNew, entry)

		if err := copyProjectVolumeDataInternal(ctx, dockerClient, copyRuntime, entry.OldName, entry.NewName); err != nil {
			return errors.Join(
				fmt.Errorf("copy volume data from %s to %s: %w", entry.OldName, entry.NewName, err),
				m.rollbackCreatedTargets(ctx, dockerClient),
			)
		}
	}

	dockerutil.InvalidateVolumeUsageCache()
	slog.InfoContext(ctx, "copied project compose volumes for rename", "oldProject", m.oldComposeName, "newProject", m.newComposeName, "count", len(m.entries))
	return nil
}

func (m *dockerProjectVolumeRenameMigrationInternal) Commit(ctx context.Context) error {
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

	if err := ensureProjectRenameTargetsReadyForCleanupInternal(ctx, dockerClient, m.JournalVolumes()); err != nil {
		return err
	}

	for _, entry := range m.entries {
		if err := removeProjectVolumeWithRetryInternal(ctx, dockerClient, entry.OldName, client.VolumeRemoveOptions{Force: false}); err != nil {
			return newProjectRenameSourceCleanupErrorInternal(entry.OldName, err)
		}
		m.removedOld = append(m.removedOld, entry)
	}

	dockerutil.InvalidateVolumeUsageCache()
	slog.InfoContext(ctx, "renamed project compose volumes", "oldProject", m.oldComposeName, "newProject", m.newComposeName, "count", len(m.entries))
	return nil
}

func (m *dockerProjectVolumeRenameMigrationInternal) JournalVolumes() []projectRenameJournalVolumeInternal {
	if m == nil || len(m.entries) == 0 {
		return nil
	}

	volumes := make([]projectRenameJournalVolumeInternal, 0, len(m.entries))
	for _, entry := range m.entries {
		volumes = append(volumes, projectRenameJournalVolumeInternal{
			Key:     entry.Key,
			OldName: entry.OldName,
			NewName: entry.NewName,
			Driver:  entry.OldVolume.Driver,
			Options: cloneStringMapInternal(entry.OldVolume.Options),
			Labels:  cloneStringMapInternal(entry.OldVolume.Labels),
		})
	}
	return volumes
}

func (m *dockerProjectVolumeRenameMigrationInternal) Rollback(ctx context.Context) error {
	if m == nil || m.service == nil || m.service.dockerService == nil {
		return nil
	}

	dockerClient, err := m.service.dockerService.GetClient(ctx)
	if err != nil {
		return fmt.Errorf("failed to connect to Docker: %w", err)
	}

	preservedTargets := map[string]struct{}{}
	var rollbackErr error
	for _, entry := range m.removedOld {
		if _, preserved := preservedTargets[entry.NewName]; preserved {
			continue
		}
		preservedTargets[entry.NewName] = struct{}{}
		rollbackErr = errors.Join(rollbackErr, newProjectRenameTargetPreservedDuringRollbackErrorInternal(projectRenameJournalVolumeInternal{
			OldName: entry.OldName,
			NewName: entry.NewName,
		}, errors.New("source volume was already removed")))
	}

	for _, entry := range m.createdNew {
		if _, preserved := preservedTargets[entry.NewName]; preserved {
			continue
		}
		sourceExists, err := projectRenameVolumeExistsInternal(ctx, dockerClient, entry.OldName)
		if err != nil {
			preservedTargets[entry.NewName] = struct{}{}
			rollbackErr = errors.Join(rollbackErr, newProjectRenameTargetPreservedDuringRollbackErrorInternal(projectRenameJournalVolumeInternal{
				OldName: entry.OldName,
				NewName: entry.NewName,
			}, fmt.Errorf("inspect source rollback volume %s: %w", entry.OldName, err)))
			continue
		}
		if sourceExists {
			continue
		}

		targetExists, err := projectRenameVolumeExistsInternal(ctx, dockerClient, entry.NewName)
		if err != nil {
			preservedTargets[entry.NewName] = struct{}{}
			rollbackErr = errors.Join(rollbackErr, newProjectRenameTargetPreservedDuringRollbackErrorInternal(projectRenameJournalVolumeInternal{
				OldName: entry.OldName,
				NewName: entry.NewName,
			}, fmt.Errorf("inspect target rollback volume %s: %w", entry.NewName, err)))
			continue
		}
		if targetExists {
			preservedTargets[entry.NewName] = struct{}{}
			rollbackErr = errors.Join(rollbackErr, newProjectRenameTargetPreservedDuringRollbackErrorInternal(projectRenameJournalVolumeInternal{
				OldName: entry.OldName,
				NewName: entry.NewName,
			}, errProjectRenameRollbackSourceMissingInternal))
		} else {
			rollbackErr = errors.Join(rollbackErr, fmt.Errorf("source volume %s and target volume %s are missing during rollback", entry.OldName, entry.NewName))
		}
	}

	rollbackErr = errors.Join(rollbackErr, m.rollbackCreatedTargetsPreserving(ctx, dockerClient, preservedTargets))
	if rollbackErr == nil {
		dockerutil.InvalidateVolumeUsageCache()
	}
	return rollbackErr
}

func (m *dockerProjectVolumeRenameMigrationInternal) rollbackCreatedTargets(ctx context.Context, dockerClient *client.Client) error {
	return m.rollbackCreatedTargetsPreserving(ctx, dockerClient, nil)
}

func (m *dockerProjectVolumeRenameMigrationInternal) rollbackCreatedTargetsPreserving(ctx context.Context, dockerClient *client.Client, preservedTargets map[string]struct{}) error {
	var rollbackErr error
	remainingCreated := make([]projectVolumeRenameEntryInternal, 0, len(preservedTargets))
	for _, entry := range slices.Backward(m.createdNew) {
		if _, preserve := preservedTargets[entry.NewName]; preserve {
			remainingCreated = append(remainingCreated, entry)
			continue
		}
		if err := removeProjectVolumeHelperContainersInternal(ctx, dockerClient, entry.NewName); err != nil {
			rollbackErr = errors.Join(rollbackErr, fmt.Errorf("remove helper containers for target volume %s: %w", entry.NewName, err))
			remainingCreated = append(remainingCreated, entry)
			continue
		}
		if err := removeProjectVolumeWithRetryInternal(ctx, dockerClient, entry.NewName, client.VolumeRemoveOptions{Force: true}); err != nil {
			rollbackErr = errors.Join(rollbackErr, fmt.Errorf("remove target volume %s: %w", entry.NewName, err))
			remainingCreated = append(remainingCreated, entry)
		}
	}
	slices.Reverse(remainingCreated)
	m.createdNew = remainingCreated
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

func ensureProjectRenameTargetVolumeAbsentInternal(ctx context.Context, dockerClient *client.Client, newName string) error {
	_, err := dockerClient.VolumeInspect(ctx, newName, client.VolumeInspectOptions{})
	if err != nil {
		if cerrdefs.IsNotFound(err) {
			return nil
		}
		return fmt.Errorf("inspect target volume %s: %w", newName, err)
	}
	return &ProjectVolumeRenameConflictError{VolumeName: newName}
}

func ensureProjectRenameSourceVolumeDetachedInternal(ctx context.Context, dockerClient *client.Client, oldName string) error {
	containerIDs, err := dockerutil.GetContainersUsingVolume(ctx, dockerClient, oldName)
	if err != nil {
		return fmt.Errorf("inspect containers using source volume %s: %w", oldName, err)
	}
	if len(containerIDs) > 0 {
		return &ProjectVolumeRenameInUseError{VolumeName: oldName, ContainerIDs: containerIDs}
	}
	return nil
}

func createProjectRenamedVolumeInternal(ctx context.Context, dockerClient *client.Client, entry projectVolumeRenameEntryInternal) error {
	labels := map[string]string{}
	maps.Copy(labels, entry.OldVolume.Labels)
	maps.Copy(labels, entry.NewConfig.Labels)
	maps.Copy(labels, entry.NewConfig.CustomLabels)

	hash, err := composepkg.VolumeHash(entry.NewConfig)
	if err != nil {
		return fmt.Errorf("hash target volume %s: %w", entry.NewName, err)
	}
	labels[api.ConfigHashLabel] = hash

	_, err = dockerClient.VolumeCreate(ctx, client.VolumeCreateOptions{
		Name:       entry.NewName,
		Driver:     entry.NewConfig.Driver,
		DriverOpts: entry.NewConfig.DriverOpts,
		Labels:     labels,
	})
	if err != nil {
		return fmt.Errorf("create target volume %s: %w", entry.NewName, err)
	}
	return nil
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
	for attempt := range 3 {
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

func copyProjectVolumeDataInternal(ctx context.Context, dockerClient *client.Client, copyRuntime projectVolumeCopyRuntimeInternal, sourceVolume, targetVolume string) error {
	sourceID, cleanupSource, err := createProjectVolumeCopyHolderContainerInternal(ctx, dockerClient, copyRuntime, sourceVolume, true)
	if err != nil {
		return err
	}
	defer cleanupSource()

	targetID, cleanupTarget, err := createProjectVolumeCopyHolderContainerInternal(ctx, dockerClient, copyRuntime, targetVolume, false)
	if err != nil {
		return err
	}
	defer cleanupTarget()

	sourceProbe, err := probeProjectVolumeCopyContainerInternal(ctx, dockerClient, sourceID)
	if err != nil {
		return fmt.Errorf("probe source volume %s: %w", sourceVolume, err)
	}
	targetProbe, err := probeProjectVolumeCopyContainerInternal(ctx, dockerClient, targetID)
	if err != nil {
		return fmt.Errorf("probe target volume %s: %w", targetVolume, err)
	}
	if err := ensureProjectVolumeCopyCapacityInternal(sourceProbe, targetProbe, sourceVolume, targetVolume); err != nil {
		return err
	}

	copyResult, err := dockerClient.CopyFromContainer(ctx, sourceID, client.CopyFromContainerOptions{
		SourcePath: projectVolumeCopyMountPathInternal + "/.",
	})
	if err != nil {
		return fmt.Errorf("read source volume archive: %w", err)
	}
	defer func() { _ = copyResult.Content.Close() }()

	_, err = dockerClient.CopyToContainer(ctx, targetID, client.CopyToContainerOptions{
		DestinationPath: projectVolumeCopyMountPathInternal,
		Content:         copyResult.Content,
	})
	if err != nil {
		if isProjectVolumeCopyNoSpaceErrorInternal(err) {
			return &ProjectVolumeRenameInsufficientSpaceError{
				SourceVolume: sourceVolume,
				TargetVolume: targetVolume,
				Detail:       err.Error(),
			}
		}
		return fmt.Errorf("write target volume archive: %w", err)
	}

	return nil
}

func createProjectVolumeCopyHolderContainerInternal(ctx context.Context, dockerClient *client.Client, copyRuntime projectVolumeCopyRuntimeInternal, volumeName string, readOnly bool) (string, func(), error) {
	bind := volumeName + ":" + projectVolumeCopyMountPathInternal
	if readOnly {
		bind += ":ro"
	}

	config := &container.Config{
		Image:           copyRuntime.Image,
		Entrypoint:      append([]string(nil), copyRuntime.Command...),
		Cmd:             []string{"internal-volume-helper", "probe", "--path", projectVolumeCopyMountPathInternal},
		NetworkDisabled: true,
		Labels:          buildVolumeHelperLabelsInternal(),
	}

	hostConfig := buildVolumeHelperHostConfigInternal(copyRuntime.Image, []string{bind}, nil)
	hostConfig.AutoRemove = false

	resp, err := dockerClient.ContainerCreate(ctx, client.ContainerCreateOptions{
		Config:     config,
		HostConfig: hostConfig,
	})
	if err != nil {
		return "", nil, fmt.Errorf("create volume copy holder: %w", err)
	}

	cleanup := func() {
		cleanupCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 30*time.Second)
		defer cancel()
		if _, err := dockerClient.ContainerRemove(cleanupCtx, resp.ID, volumeHelperRemoveOptionsInternal()); err != nil && !cerrdefs.IsNotFound(err) {
			slog.WarnContext(cleanupCtx, "failed to remove volume copy holder", "containerID", resp.ID, "error", err)
		}
	}

	return resp.ID, cleanup, nil
}

func probeProjectVolumeCopyContainerInternal(ctx context.Context, dockerClient *client.Client, containerID string) (projectVolumeCopyProbeInternal, error) {
	logs, err := startProjectVolumeHelperContainerInternal(ctx, dockerClient, containerID)
	if err != nil {
		return projectVolumeCopyProbeInternal{}, err
	}

	var probe projectVolumeCopyProbeInternal
	if err := json.Unmarshal([]byte(strings.TrimSpace(logs)), &probe); err != nil {
		return projectVolumeCopyProbeInternal{}, fmt.Errorf("decode volume probe output: %w", err)
	}
	return probe, nil
}

func startProjectVolumeHelperContainerInternal(ctx context.Context, dockerClient *client.Client, containerID string) (string, error) {
	if _, err := dockerClient.ContainerStart(ctx, containerID, client.ContainerStartOptions{}); err != nil {
		return "", fmt.Errorf("start volume copy container: %w", err)
	}

	waitResult := dockerClient.ContainerWait(ctx, containerID, client.ContainerWaitOptions{Condition: container.WaitConditionNotRunning})
	select {
	case err, ok := <-waitResult.Error:
		if !ok {
			return "", errors.New("volume copy container wait error channel closed")
		}
		if err != nil {
			return "", err
		}
		return "", errors.New("volume copy container wait returned nil error without result")
	case waitBody, ok := <-waitResult.Result:
		if !ok {
			return "", errors.New("volume copy container wait result channel closed")
		}
		logs := readProjectVolumeHelperLogsInternal(ctx, dockerClient, containerID)
		if waitBody.StatusCode != 0 {
			output := logs.String()
			if output != "" {
				return output, fmt.Errorf("volume copy container exited with code %d: %s", waitBody.StatusCode, output)
			}
			return "", fmt.Errorf("volume copy container exited with code %d", waitBody.StatusCode)
		}
		return logs.Stdout, nil
	case <-ctx.Done():
		return "", ctx.Err()
	}
}

func getProjectVolumeCopyRuntimeInternal(ctx context.Context, dockerClient *client.Client) (projectVolumeCopyRuntimeInternal, error) {
	if dockerClient == nil {
		return projectVolumeCopyRuntimeInternal{}, errors.New("docker service unavailable")
	}

	resolved, ok := resolveArcaneRuntimeHelperImageInternal(ctx, dockerClient)
	if !ok || strings.TrimSpace(resolved.Image) == "" {
		return projectVolumeCopyRuntimeInternal{}, errors.New("local Arcane runtime image unavailable for volume copy")
	}

	command := normalizeProjectVolumeCopyRuntimeCommandInternal(resolved)
	if len(command) == 0 {
		return projectVolumeCopyRuntimeInternal{}, fmt.Errorf("local Arcane runtime image %s has no command for volume copy helper", resolved.Image)
	}

	return projectVolumeCopyRuntimeInternal{
		Image:   resolved.Image,
		Command: command,
		Source:  resolved.Source,
	}, nil
}

func normalizeProjectVolumeCopyRuntimeCommandInternal(resolved arcaneRuntimeHelperImageInternal) []string {
	if len(resolved.Entrypoint) > 0 {
		command := slices.DeleteFunc(append([]string(nil), resolved.Entrypoint...), func(part string) bool {
			return strings.TrimSpace(part) == ""
		})
		if len(command) > 0 {
			return command
		}
	}

	if len(resolved.Command) > 0 {
		command := strings.TrimSpace(resolved.Command[0])
		if command != "" {
			return []string{command}
		}
	}

	image := strings.ToLower(strings.TrimSpace(resolved.Image))
	source := strings.ToLower(strings.TrimSpace(resolved.Source))
	if strings.Contains(image, "agent") || strings.Contains(source, "agent") {
		return []string{"./arcane-agent"}
	}
	if image != "" {
		return []string{"./arcane"}
	}
	return nil
}

func ensureProjectVolumeCopyCapacityInternal(sourceProbe, targetProbe projectVolumeCopyProbeInternal, sourceVolume, targetVolume string) error {
	requiredBytes := projectVolumeCopyRequiredBytesInternal(sourceProbe.AllocatedBytes)
	if targetProbe.AvailableBytes >= requiredBytes {
		return nil
	}

	return &ProjectVolumeRenameInsufficientSpaceError{
		SourceVolume: sourceVolume,
		TargetVolume: targetVolume,
		Detail: fmt.Sprintf(
			"source=%dB available=%dB required=%dB",
			sourceProbe.AllocatedBytes,
			targetProbe.AvailableBytes,
			requiredBytes,
		),
	}
}

func projectVolumeCopyRequiredBytesInternal(sourceBytes uint64) uint64 {
	margin := max(sourceBytes/10, projectVolumeCopyMinMarginBytesInternal)
	if sourceBytes > ^uint64(0)-margin {
		return ^uint64(0)
	}
	return sourceBytes + margin
}

func isProjectVolumeCopyNoSpaceErrorInternal(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, syscall.ENOSPC) {
		return true
	}
	return strings.Contains(strings.ToLower(err.Error()), "no space left on device")
}

func readProjectVolumeHelperLogsInternal(ctx context.Context, dockerClient *client.Client, containerID string) projectVolumeHelperLogsInternal {
	logs, err := dockerClient.ContainerLogs(ctx, containerID, client.ContainerLogsOptions{
		ShowStdout: true,
		ShowStderr: true,
	})
	if err != nil {
		slog.DebugContext(ctx, "failed to read volume helper logs", "containerID", containerID, "error", err)
		return projectVolumeHelperLogsInternal{}
	}
	defer func() { _ = logs.Close() }()

	var stdout, stderr bytes.Buffer
	if _, err := stdcopy.StdCopy(&stdout, &stderr, logs); err != nil {
		slog.DebugContext(ctx, "failed to decode volume helper logs", "containerID", containerID, "error", err)
		return projectVolumeHelperLogsInternal{}
	}

	return projectVolumeHelperLogsInternal{
		Stdout: strings.TrimSpace(stdout.String()),
		Stderr: strings.TrimSpace(stderr.String()),
	}
}

func (l projectVolumeHelperLogsInternal) String() string {
	return strings.TrimSpace(strings.Join([]string{
		strings.TrimSpace(l.Stderr),
		strings.TrimSpace(l.Stdout),
	}, "\n"))
}

type ProjectVolumeRenameConflictError struct {
	VolumeName string
}

func (e *ProjectVolumeRenameConflictError) Error() string {
	if strings.TrimSpace(e.VolumeName) == "" {
		return "target volume already exists"
	}
	return "target volume already exists: " + e.VolumeName
}

type ProjectVolumeRenameInUseError struct {
	VolumeName   string
	ContainerIDs []string
}

func (e *ProjectVolumeRenameInUseError) Error() string {
	if strings.TrimSpace(e.VolumeName) == "" {
		return "source volume is still attached to containers"
	}
	if len(e.ContainerIDs) == 0 {
		return "source volume is still attached to containers: " + e.VolumeName
	}
	return fmt.Sprintf("source volume is still attached to %d container(s): %s", len(e.ContainerIDs), e.VolumeName)
}

type ProjectVolumeRenameInsufficientSpaceError struct {
	SourceVolume string
	TargetVolume string
	Detail       string
}

func (e *ProjectVolumeRenameInsufficientSpaceError) Error() string {
	msg := "insufficient disk space to rename project volume"
	if e.SourceVolume != "" && e.TargetVolume != "" {
		msg = fmt.Sprintf("insufficient disk space to copy volume %s to %s", e.SourceVolume, e.TargetVolume)
	}
	if strings.TrimSpace(e.Detail) != "" {
		msg += ": " + strings.TrimSpace(e.Detail)
	}
	return msg
}

func cloneStringMapInternal(values map[string]string) map[string]string {
	if len(values) == 0 {
		return nil
	}
	cloned := make(map[string]string, len(values))
	maps.Copy(cloned, values)
	return cloned
}
