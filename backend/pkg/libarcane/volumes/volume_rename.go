package volumes

import (
	"context"
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
	dockerutil "github.com/getarcaneapp/arcane/backend/v2/pkg/dockerutil"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/libarcane"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/libarcane/volumehelper"
	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/api/types/volume"
	"github.com/moby/moby/client"
)

type Migration interface {
	Apply(ctx context.Context) error
	Rollback(ctx context.Context) error
}

type Committer interface {
	Commit(ctx context.Context) error
}

type JournalSource interface {
	JournalVolumes() []JournalVolume
}

type projectVolumeRenameEntryInternal struct {
	Key       string
	OldName   string
	NewName   string
	OldVolume volume.Volume
	NewConfig composetypes.VolumeConfig
}

type dockerProjectVolumeRenameMigrationInternal struct {
	dockerClient   *client.Client
	entries        []projectVolumeRenameEntryInternal
	createdNew     []projectVolumeRenameEntryInternal
	removedOld     []projectVolumeRenameEntryInternal
	oldComposeName string
	newComposeName string
}

type JournalVolume struct {
	Key     string            `json:"key"`
	OldName string            `json:"oldName"`
	NewName string            `json:"newName"`
	Driver  string            `json:"driver,omitempty"`
	Options map[string]string `json:"options,omitempty"`
	Labels  map[string]string `json:"labels,omitempty"`
}

type projectVolumeCopyRuntimeInternal struct {
	Image string
}

const (
	projectVolumeCopyMountPathInternal = "/volume"
)

func PlanMigration(ctx context.Context, dockerClient *client.Client, composeProject *composetypes.Project, oldComposeName, newComposeName string) (Migration, error) {
	if dockerClient == nil {
		return nil, errors.New("docker service unavailable")
	}
	if composeProject == nil || len(composeProject.Volumes) == 0 {
		return nil, nil
	}
	if strings.TrimSpace(oldComposeName) == "" || strings.TrimSpace(newComposeName) == "" || oldComposeName == newComposeName {
		return nil, nil
	}

	explicitVolumeNames, err := composeVolumeKeysWithExplicitNameInternal(composeProject.ComposeFiles)
	if err != nil {
		return nil, fmt.Errorf("failed to parse compose volume names: %w", err)
	}

	entries, err := collectProjectRenameVolumeEntriesInternal(ctx, dockerClient, composeProject.Volumes, explicitVolumeNames, oldComposeName, newComposeName)
	if err != nil {
		return nil, err
	}

	if len(entries) == 0 {
		return nil, nil
	}

	return &dockerProjectVolumeRenameMigrationInternal{
		dockerClient:   dockerClient,
		entries:        entries,
		oldComposeName: oldComposeName,
		newComposeName: newComposeName,
	}, nil
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
	if m.dockerClient == nil {
		return errors.New("docker service unavailable")
	}

	dockerClient := m.dockerClient

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

	dockerutil.InvalidateVolumeUsageCache(dockerClient)
	slog.InfoContext(ctx, "copied project compose volumes for rename", "oldProject", m.oldComposeName, "newProject", m.newComposeName, "count", len(m.entries))
	return nil
}

func (m *dockerProjectVolumeRenameMigrationInternal) Commit(ctx context.Context) error {
	if m == nil || len(m.entries) == 0 {
		return nil
	}
	if m.dockerClient == nil {
		return errors.New("docker service unavailable")
	}

	dockerClient := m.dockerClient

	if err := EnsureTargetsReadyForCleanup(ctx, dockerClient, m.JournalVolumes()); err != nil {
		return err
	}

	for _, entry := range m.entries {
		if err := removeProjectVolumeWithRetryInternal(ctx, dockerClient, entry.OldName, client.VolumeRemoveOptions{Force: false}); err != nil {
			return NewSourceCleanupError(entry.OldName, err)
		}
		m.removedOld = append(m.removedOld, entry)
	}

	dockerutil.InvalidateVolumeUsageCache(dockerClient)
	slog.InfoContext(ctx, "renamed project compose volumes", "oldProject", m.oldComposeName, "newProject", m.newComposeName, "count", len(m.entries))
	return nil
}

func (m *dockerProjectVolumeRenameMigrationInternal) JournalVolumes() []JournalVolume {
	if m == nil || len(m.entries) == 0 {
		return nil
	}

	volumes := make([]JournalVolume, 0, len(m.entries))
	for _, entry := range m.entries {
		volumes = append(volumes, JournalVolume{
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
	if m == nil || m.dockerClient == nil {
		return nil
	}

	dockerClient := m.dockerClient

	preservedTargets := map[string]struct{}{}
	var rollbackErr error
	for _, entry := range m.removedOld {
		if _, preserved := preservedTargets[entry.NewName]; preserved {
			continue
		}
		preservedTargets[entry.NewName] = struct{}{}
		rollbackErr = errors.Join(rollbackErr, NewTargetPreservedDuringRollbackError(JournalVolume{
			OldName: entry.OldName,
			NewName: entry.NewName,
		}, errors.New("source volume was already removed")))
	}

	for _, entry := range m.createdNew {
		if _, preserved := preservedTargets[entry.NewName]; preserved {
			continue
		}
		sourceExists, err := VolumeExists(ctx, dockerClient, entry.OldName)
		if err != nil {
			preservedTargets[entry.NewName] = struct{}{}
			rollbackErr = errors.Join(rollbackErr, NewTargetPreservedDuringRollbackError(JournalVolume{
				OldName: entry.OldName,
				NewName: entry.NewName,
			}, fmt.Errorf("inspect source rollback volume %s: %w", entry.OldName, err)))
			continue
		}
		if sourceExists {
			continue
		}

		targetExists, err := VolumeExists(ctx, dockerClient, entry.NewName)
		if err != nil {
			preservedTargets[entry.NewName] = struct{}{}
			rollbackErr = errors.Join(rollbackErr, NewTargetPreservedDuringRollbackError(JournalVolume{
				OldName: entry.OldName,
				NewName: entry.NewName,
			}, fmt.Errorf("inspect target rollback volume %s: %w", entry.NewName, err)))
			continue
		}
		if targetExists {
			preservedTargets[entry.NewName] = struct{}{}
			rollbackErr = errors.Join(rollbackErr, NewTargetPreservedDuringRollbackError(JournalVolume{
				OldName: entry.OldName,
				NewName: entry.NewName,
			}, errProjectRenameRollbackSourceMissingInternal))
		} else {
			rollbackErr = errors.Join(rollbackErr, fmt.Errorf("source volume %s and target volume %s are missing during rollback", entry.OldName, entry.NewName))
		}
	}

	rollbackErr = errors.Join(rollbackErr, m.rollbackCreatedTargetsPreserving(ctx, dockerClient, preservedTargets))
	if rollbackErr == nil {
		dockerutil.InvalidateVolumeUsageCache(dockerClient)
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
		if !isProjectVolumeHelperContainerInternal(c) || !containerSummaryMountsVolumeInternal(c, volumeName) {
			continue
		}
		if _, err := dockerClient.ContainerRemove(ctx, c.ID, volumehelper.RemoveOptions()); err != nil && !cerrdefs.IsNotFound(err) {
			removeErr = errors.Join(removeErr, fmt.Errorf("remove helper container %s: %w", c.ID, err))
		}
	}
	return removeErr
}

func isProjectVolumeHelperContainerInternal(c container.Summary) bool {
	if !libarcane.IsInternalContainer(c.Labels) {
		return false
	}
	if strings.EqualFold(c.Labels[volumehelper.ContainerLabel], "true") {
		return true
	}
	command := strings.ToLower(c.Command)
	return strings.Contains(command, "sleep") && strings.Contains(command, "infinity")
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
		Cmd:             []string{"sleep", "infinity"},
		NetworkDisabled: true,
		Labels:          volumehelper.Labels(),
	}

	hostConfig := volumehelper.HostConfig(copyRuntime.Image, []string{bind}, nil)
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
		if _, err := dockerClient.ContainerRemove(cleanupCtx, resp.ID, volumehelper.RemoveOptions()); err != nil && !cerrdefs.IsNotFound(err) {
			slog.WarnContext(cleanupCtx, "failed to remove volume copy holder", "containerID", resp.ID, "error", err)
		}
	}

	return resp.ID, cleanup, nil
}

func getProjectVolumeCopyRuntimeInternal(ctx context.Context, dockerClient *client.Client) (projectVolumeCopyRuntimeInternal, error) {
	if dockerClient == nil {
		return projectVolumeCopyRuntimeInternal{}, errors.New("docker service unavailable")
	}

	image, err := volumehelper.ResolveHelperImage(ctx, dockerClient)
	if err != nil {
		return projectVolumeCopyRuntimeInternal{}, err
	}

	return projectVolumeCopyRuntimeInternal{
		Image: image,
	}, nil
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

func EnsureTargetsReadyForCleanup(ctx context.Context, dockerClient *client.Client, volumes []JournalVolume) error {
	if len(volumes) == 0 {
		return nil
	}
	if dockerClient == nil {
		return errors.New("docker service unavailable")
	}

	var missingWithSource *TargetMissingWithSourceError
	var externallyRemoved []JournalVolume
	for _, vol := range volumes {
		targetExists, err := VolumeExists(ctx, dockerClient, vol.NewName)
		if err != nil {
			return err
		}
		if targetExists {
			continue
		}

		sourceExists, err := VolumeExists(ctx, dockerClient, vol.OldName)
		if err != nil {
			return err
		}
		if sourceExists {
			if missingWithSource == nil {
				missingWithSource = &TargetMissingWithSourceError{
					SourceVolume: vol.OldName,
					TargetVolume: vol.NewName,
				}
			}
			continue
		}
		externallyRemoved = append(externallyRemoved, vol)
	}
	if missingWithSource != nil {
		return missingWithSource
	}
	if len(externallyRemoved) > 0 {
		return &VolumesExternallyRemovedError{Volumes: externallyRemoved}
	}
	return nil
}

func RemoveSourceVolumes(ctx context.Context, dockerClient *client.Client, volumes []JournalVolume) error {
	for _, vol := range volumes {
		if err := removeProjectVolumeWithRetryInternal(ctx, dockerClient, vol.OldName, client.VolumeRemoveOptions{Force: false}); err != nil {
			return NewSourceCleanupError(vol.OldName, err)
		}
	}
	dockerutil.InvalidateVolumeUsageCache(dockerClient)
	return nil
}

func RollbackVolumes(ctx context.Context, dockerClient *client.Client, volumes []JournalVolume) error {
	var rollbackErr error
	for _, vol := range slices.Backward(volumes) {
		if err := RollbackVolume(ctx, dockerClient, vol); err != nil {
			rollbackErr = errors.Join(rollbackErr, err)
		}
	}
	if len(volumes) > 0 {
		dockerutil.InvalidateVolumeUsageCache(dockerClient)
	}
	return rollbackErr
}

func RollbackVolume(ctx context.Context, dockerClient *client.Client, vol JournalVolume) error {
	oldExists, err := VolumeExists(ctx, dockerClient, vol.OldName)
	if err != nil {
		return NewTargetPreservedDuringRollbackError(vol, fmt.Errorf("inspect source rollback volume %s: %w", vol.OldName, err))
	}
	newExists, err := VolumeExists(ctx, dockerClient, vol.NewName)
	if err != nil {
		return NewTargetPreservedDuringRollbackError(vol, fmt.Errorf("inspect target rollback volume %s: %w", vol.NewName, err))
	}

	switch {
	case oldExists && newExists:
		return removeProjectRenameJournalTargetVolumeInternal(ctx, dockerClient, vol.NewName, oldExists, newExists)
	case !oldExists && newExists:
		return NewTargetPreservedDuringRollbackError(vol, errProjectRenameRollbackSourceMissingInternal)
	case !oldExists && !newExists:
		slog.WarnContext(ctx, "project rename source and target volumes are missing during rollback", "sourceVolume", vol.OldName, "targetVolume", vol.NewName)
	}
	return nil
}

func CleanupRollbackTargetVolumes(ctx context.Context, dockerClient *client.Client, volumes []JournalVolume) error {
	var cleanupErr error
	for _, vol := range slices.Backward(volumes) {
		if err := cleanupProjectRenameRollbackTargetVolumeInternal(ctx, dockerClient, vol); err != nil {
			cleanupErr = errors.Join(cleanupErr, err)
		}
	}
	return cleanupErr
}

func cleanupProjectRenameRollbackTargetVolumeInternal(ctx context.Context, dockerClient *client.Client, vol JournalVolume) error {
	oldExists, err := VolumeExists(ctx, dockerClient, vol.OldName)
	if err != nil {
		return fmt.Errorf("inspect source cleanup volume %s: %w", vol.OldName, err)
	}
	newExists, err := VolumeExists(ctx, dockerClient, vol.NewName)
	if err != nil {
		return fmt.Errorf("inspect target cleanup volume %s: %w", vol.NewName, err)
	}

	switch {
	case oldExists && newExists:
		return removeProjectRenameJournalTargetVolumeInternal(ctx, dockerClient, vol.NewName, oldExists, newExists)
	case !oldExists && newExists:
		return NewTargetPreservedDuringRollbackError(vol, errProjectRenameRollbackSourceMissingInternal)
	case !oldExists && !newExists:
		slog.WarnContext(ctx, "project rename source and target volumes are missing during rollback cleanup", "sourceVolume", vol.OldName, "targetVolume", vol.NewName)
	}
	return nil
}

func removeProjectRenameJournalTargetVolumeInternal(ctx context.Context, dockerClient *client.Client, newName string, oldExists bool, newExists bool) error {
	if !oldExists || !newExists {
		return nil
	}

	if err := removeProjectVolumeHelperContainersInternal(ctx, dockerClient, newName); err != nil {
		return err
	}
	if err := removeProjectVolumeWithRetryInternal(ctx, dockerClient, newName, client.VolumeRemoveOptions{Force: true}); err != nil {
		return fmt.Errorf("remove rollback target volume %s: %w", newName, err)
	}
	return nil
}

func VolumeExists(ctx context.Context, dockerClient *client.Client, name string) (bool, error) {
	_, err := dockerClient.VolumeInspect(ctx, name, client.VolumeInspectOptions{})
	if err == nil {
		return true, nil
	}
	if cerrdefs.IsNotFound(err) {
		return false, nil
	}
	return false, fmt.Errorf("inspect volume %s: %w", name, err)
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

type TargetMissingWithSourceError struct {
	SourceVolume string
	TargetVolume string
}

func (e *TargetMissingWithSourceError) Error() string {
	return fmt.Sprintf("committed project rename target volume %s is missing while source volume %s still exists", e.TargetVolume, e.SourceVolume)
}

type SourceCleanupError struct {
	SourceVolume string
	Err          error
}

func NewSourceCleanupError(sourceVolume string, err error) error {
	return &SourceCleanupError{
		SourceVolume: sourceVolume,
		Err:          err,
	}
}

func (e *SourceCleanupError) Error() string {
	if e == nil {
		return "clean up committed project rename source volume"
	}
	if strings.TrimSpace(e.SourceVolume) == "" {
		return fmt.Sprintf("clean up committed project rename source volume: %v", e.Err)
	}
	return fmt.Sprintf("clean up committed project rename source volume %s: %v", e.SourceVolume, e.Err)
}

func (e *SourceCleanupError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Err
}

type TargetPreservedDuringRollbackError struct {
	SourceVolume string
	TargetVolume string
	Err          error
}

var errProjectRenameRollbackSourceMissingInternal = errors.New("source volume is missing and target volume may contain the only remaining data copy")

func NewTargetPreservedDuringRollbackError(vol JournalVolume, err error) error {
	return &TargetPreservedDuringRollbackError{
		SourceVolume: vol.OldName,
		TargetVolume: vol.NewName,
		Err:          err,
	}
}

func (e *TargetPreservedDuringRollbackError) Error() string {
	return fmt.Sprintf("preserved project rename target volume %s during rollback to avoid data loss; source volume %s was not safe to rely on: %v", e.TargetVolume, e.SourceVolume, e.Err)
}

func (e *TargetPreservedDuringRollbackError) Unwrap() error {
	return e.Err
}

func OnlyPreservedTargetErrors(err error) bool {
	if err == nil {
		return false
	}
	if joined, ok := err.(interface{ Unwrap() []error }); ok {
		children := joined.Unwrap()
		if len(children) == 0 {
			return false
		}
		for _, child := range children {
			if !OnlyPreservedTargetErrors(child) {
				return false
			}
		}
		return true
	}

	var preserved *TargetPreservedDuringRollbackError
	return errors.As(err, &preserved)
}

type VolumesExternallyRemovedError struct {
	Volumes []JournalVolume
}

func (e *VolumesExternallyRemovedError) Error() string {
	if e == nil || len(e.Volumes) == 0 {
		return "committed project rename source and target volumes are both missing"
	}
	if len(e.Volumes) == 1 {
		vol := e.Volumes[0]
		return fmt.Sprintf("committed project rename target volume %s is missing and source volume %s is also missing", vol.NewName, vol.OldName)
	}
	return fmt.Sprintf("committed project rename source and target volumes are both missing for %d volume pairs", len(e.Volumes))
}

func cloneStringMapInternal(values map[string]string) map[string]string {
	if len(values) == 0 {
		return nil
	}
	cloned := make(map[string]string, len(values))
	maps.Copy(cloned, values)
	return cloned
}
