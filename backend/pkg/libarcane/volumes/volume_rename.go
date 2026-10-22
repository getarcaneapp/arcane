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

	cerrdefs "github.com/containerd/errdefs"
	"github.com/getarcaneapp/arcane/types/v3"
	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/client"
	"go.getarcane.app/docker"
	kit "go.getarcane.app/kit/pkg"

	"github.com/getarcaneapp/arcane/backend/v3/internal/common"
	"github.com/getarcaneapp/arcane/backend/v3/pkg/libarcane"
	"github.com/getarcaneapp/arcane/backend/v3/pkg/libarcane/volumehelper"
)

type dockerProjectVolumeRenameMigrationInternal struct {
	dockerClient   *client.Client
	entries        []types.VolumeRenameEntry
	createdNew     []types.VolumeRenameEntry
	removedOld     []types.VolumeRenameEntry
	oldComposeName string
	newComposeName string
	toolsImage     string
}

type projectVolumeCopyRuntimeInternal struct {
	Image string
}

const (
	projectVolumeCopyMountPathInternal = "/volume"
)

// NewMigration prepares generic volume copies with caller-supplied target configuration.
func NewMigration(dockerClient *client.Client, entries []types.VolumeRenameEntry, oldProject, newProject, toolsImage string) types.VolumeMigration {
	if len(entries) == 0 {
		return nil
	}
	return &dockerProjectVolumeRenameMigrationInternal{
		dockerClient:   dockerClient,
		entries:        entries,
		oldComposeName: oldProject,
		newComposeName: newProject,
		toolsImage:     toolsImage,
	}
}

// PlanRename validates and prepares an unused Docker volume for a copy-based rename.
func PlanRename(ctx context.Context, dockerClient *client.Client, oldName, newName, toolsImage string) (types.VolumeMigration, error) {
	if dockerClient == nil {
		return nil, errors.New("docker service unavailable")
	}

	oldName = strings.TrimSpace(oldName)
	newName = strings.TrimSpace(newName)
	if oldName == "" || newName == "" {
		return nil, errors.New("source and target volume names are required")
	}
	if oldName == newName {
		return nil, errors.New("source and target volume names must differ")
	}

	oldVolume, err := dockerClient.VolumeInspect(ctx, oldName, client.VolumeInspectOptions{})
	if err != nil {
		return nil, fmt.Errorf("inspect source volume %s: %w", oldName, err)
	}
	if ensureRenameSourceDetachedErr := EnsureRenameSourceDetached(ctx, dockerClient, oldName); ensureRenameSourceDetachedErr != nil {
		return nil, ensureRenameSourceDetachedErr
	}
	if ensureRenameTargetAbsentErr := EnsureRenameTargetAbsent(ctx, dockerClient, newName); ensureRenameTargetAbsentErr != nil {
		return nil, ensureRenameTargetAbsentErr
	}

	entry := types.VolumeRenameEntry{
		OldName:   oldName,
		NewName:   newName,
		OldVolume: oldVolume.Volume,
		CreateOptions: client.VolumeCreateOptions{
			Name:       newName,
			Driver:     oldVolume.Volume.Driver,
			DriverOpts: cloneStringMap(oldVolume.Volume.Options),
			Labels:     cloneStringMap(oldVolume.Volume.Labels),
		},
	}

	return NewMigration(dockerClient, []types.VolumeRenameEntry{entry}, "", "", toolsImage), nil
}

func (m *dockerProjectVolumeRenameMigrationInternal) Apply(ctx context.Context) error {
	if m == nil || len(m.entries) == 0 {
		return nil
	}
	if m.dockerClient == nil {
		return errors.New("docker service unavailable")
	}

	dockerClient := m.dockerClient

	copyRuntime, err := getProjectVolumeCopyRuntime(ctx, dockerClient, m.toolsImage)
	if err != nil {
		return err
	}

	for _, entry := range m.entries {
		if createProjectRenamedVolumeErr := createProjectRenamedVolume(ctx, dockerClient, entry); createProjectRenamedVolumeErr != nil {
			return errors.Join(createProjectRenamedVolumeErr, m.rollbackCreatedTargets(ctx, dockerClient))
		}
		m.createdNew = append(m.createdNew, entry)

		if copyProjectVolumeDataErr := copyProjectVolumeData(ctx, dockerClient, copyRuntime, entry.OldName, entry.NewName); copyProjectVolumeDataErr != nil {
			return errors.Join(fmt.Errorf("copy volume data from %s to %s: %w", entry.OldName, entry.NewName, copyProjectVolumeDataErr), m.rollbackCreatedTargets(ctx, dockerClient))
		}
	}

	docker.InvalidateVolumeUsageCache(dockerClient)
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
		if err := removeProjectVolumeWithRetry(ctx, dockerClient, entry.OldName, client.VolumeRemoveOptions{Force: false}); err != nil {
			return NewSourceCleanupError(entry.OldName, err)
		}
		m.removedOld = append(m.removedOld, entry)
	}

	docker.InvalidateVolumeUsageCache(dockerClient)
	slog.InfoContext(ctx, "renamed project compose volumes", "oldProject", m.oldComposeName, "newProject", m.newComposeName, "count", len(m.entries))
	return nil
}

func (m *dockerProjectVolumeRenameMigrationInternal) JournalVolumes() []types.VolumeJournalVolume {
	if m == nil || len(m.entries) == 0 {
		return nil
	}

	volumes := make([]types.VolumeJournalVolume, 0, len(m.entries))
	for _, entry := range m.entries {
		volumes = append(volumes, types.VolumeJournalVolume{
			Key:     entry.Key,
			OldName: entry.OldName,
			NewName: entry.NewName,
			Driver:  entry.OldVolume.Driver,
			Options: cloneStringMap(entry.OldVolume.Options),
			Labels:  cloneStringMap(entry.OldVolume.Labels),
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
		rollbackErr = errors.Join(rollbackErr, NewTargetPreservedDuringRollbackError(types.VolumeJournalVolume{
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
			rollbackErr = errors.Join(rollbackErr, NewTargetPreservedDuringRollbackError(types.VolumeJournalVolume{
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
			rollbackErr = errors.Join(rollbackErr, NewTargetPreservedDuringRollbackError(types.VolumeJournalVolume{
				OldName: entry.OldName,
				NewName: entry.NewName,
			}, fmt.Errorf("inspect target rollback volume %s: %w", entry.NewName, err)))
			continue
		}
		if targetExists {
			preservedTargets[entry.NewName] = struct{}{}
			rollbackErr = errors.Join(rollbackErr, NewTargetPreservedDuringRollbackError(types.VolumeJournalVolume{
				OldName: entry.OldName,
				NewName: entry.NewName,
			}, errProjectRenameRollbackSourceMissingInternal))
		} else {
			rollbackErr = errors.Join(rollbackErr, fmt.Errorf("source volume %s and target volume %s are missing during rollback", entry.OldName, entry.NewName))
		}
	}

	rollbackErr = errors.Join(rollbackErr, m.rollbackCreatedTargetsPreserving(ctx, dockerClient, preservedTargets))
	if rollbackErr == nil {
		docker.InvalidateVolumeUsageCache(dockerClient)
	}
	return rollbackErr
}

func (m *dockerProjectVolumeRenameMigrationInternal) rollbackCreatedTargets(ctx context.Context, dockerClient *client.Client) error {
	return m.rollbackCreatedTargetsPreserving(ctx, dockerClient, nil)
}

func (m *dockerProjectVolumeRenameMigrationInternal) rollbackCreatedTargetsPreserving(ctx context.Context, dockerClient *client.Client, preservedTargets map[string]struct{}) error {
	var rollbackErr error
	remainingCreated := make([]types.VolumeRenameEntry, 0, len(preservedTargets))
	for _, entry := range slices.Backward(m.createdNew) {
		if _, preserve := preservedTargets[entry.NewName]; preserve {
			remainingCreated = append(remainingCreated, entry)
			continue
		}
		if err := removeProjectVolumeHelperContainers(ctx, dockerClient, entry.NewName); err != nil {
			rollbackErr = errors.Join(rollbackErr, fmt.Errorf("remove helper containers for target volume %s: %w", entry.NewName, err))
			remainingCreated = append(remainingCreated, entry)
			continue
		}
		if err := removeProjectVolumeWithRetry(ctx, dockerClient, entry.NewName, client.VolumeRemoveOptions{Force: true}); err != nil {
			rollbackErr = errors.Join(rollbackErr, fmt.Errorf("remove target volume %s: %w", entry.NewName, err))
			remainingCreated = append(remainingCreated, entry)
		}
	}
	slices.Reverse(remainingCreated)
	m.createdNew = remainingCreated
	return rollbackErr
}

func EnsureRenameTargetAbsent(ctx context.Context, dockerClient *client.Client, newName string) error {
	_, err := dockerClient.VolumeInspect(ctx, newName, client.VolumeInspectOptions{})
	if err != nil {
		if cerrdefs.IsNotFound(err) {
			return nil
		}
		return fmt.Errorf("inspect target volume %s: %w", newName, err)
	}
	return common.Classify(common.ErrConflict, &types.ProjectVolumeRenameConflictError{VolumeName: newName})
}

func EnsureRenameSourceDetached(ctx context.Context, dockerClient *client.Client, oldName string) error {
	containerIDs, err := docker.GetContainersUsingVolume(ctx, dockerClient, oldName)
	if err != nil {
		return fmt.Errorf("inspect containers using source volume %s: %w", oldName, err)
	}
	if len(containerIDs) > 0 {
		return common.Classify(common.ErrConflict, &types.ProjectVolumeRenameInUseError{VolumeName: oldName, ContainerIDs: containerIDs})
	}
	return nil
}

func createProjectRenamedVolume(ctx context.Context, dockerClient *client.Client, entry types.VolumeRenameEntry) error {
	if _, err := dockerClient.VolumeCreate(ctx, entry.CreateOptions); err != nil {
		return fmt.Errorf("create target volume %s: %w", entry.NewName, err)
	}
	return nil
}

func removeProjectVolumeHelperContainers(ctx context.Context, dockerClient *client.Client, volumeName string) error {
	containers, err := dockerClient.ContainerList(ctx, client.ContainerListOptions{All: true})
	if err != nil {
		return fmt.Errorf("list containers for helper cleanup: %w", err)
	}

	var removeErr error
	for _, c := range containers.Items {
		if !isProjectVolumeHelperContainer(c) || !containerSummaryMountsVolume(c, volumeName) {
			continue
		}
		if _, containerRemoveErr := dockerClient.ContainerRemove(ctx, c.ID, volumehelper.RemoveOptions()); containerRemoveErr != nil && !cerrdefs.IsNotFound(containerRemoveErr) {
			removeErr = errors.Join(removeErr, fmt.Errorf("remove helper container %s: %w", c.ID, containerRemoveErr))
		}
	}
	return removeErr
}

func isProjectVolumeHelperContainer(c container.Summary) bool {
	if internal, _ := kit.ParseBool(c.Labels[libarcane.InternalResourceLabel]); !internal {
		return false
	}
	if strings.EqualFold(c.Labels[volumehelper.ContainerLabel], "true") {
		return true
	}
	command := strings.ToLower(c.Command)
	return strings.Contains(command, "sleep") && strings.Contains(command, "infinity")
}

func containerSummaryMountsVolume(c container.Summary, volumeName string) bool {
	for _, mount := range c.Mounts {
		if mount.Name == volumeName || mount.Source == volumeName {
			return true
		}
	}
	return false
}

func removeProjectVolumeWithRetry(ctx context.Context, dockerClient *client.Client, volumeName string, options client.VolumeRemoveOptions) error {
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

func copyProjectVolumeData(ctx context.Context, dockerClient *client.Client, copyRuntime projectVolumeCopyRuntimeInternal, sourceVolume, targetVolume string) error {
	sourceID, cleanupSource, err := createProjectVolumeCopyHolderContainer(ctx, dockerClient, copyRuntime, sourceVolume, true)
	if err != nil {
		return err
	}
	defer cleanupSource()

	targetID, cleanupTarget, err := createProjectVolumeCopyHolderContainer(ctx, dockerClient, copyRuntime, targetVolume, false)
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
		if isProjectVolumeCopyNoSpaceError(err) {
			return common.Classify(common.ErrInsufficientStorage, &types.ProjectVolumeRenameInsufficientSpaceError{
				SourceVolume: sourceVolume,
				TargetVolume: targetVolume,
				Detail:       err.Error(),
			})
		}
		return fmt.Errorf("write target volume archive: %w", err)
	}

	return nil
}

func createProjectVolumeCopyHolderContainer(ctx context.Context,
	dockerClient *client.Client,
	copyRuntime projectVolumeCopyRuntimeInternal,
	volumeName string,
	readOnly bool) (string,
	func(),
	error,
) {
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
		if _, containerRemoveErr := dockerClient.ContainerRemove(cleanupCtx, resp.ID, volumehelper.RemoveOptions()); containerRemoveErr != nil && !cerrdefs.IsNotFound(containerRemoveErr) {
			slog.WarnContext(cleanupCtx, "failed to remove volume copy holder", "containerId", resp.ID, "error", containerRemoveErr)
		}
	}

	return resp.ID, cleanup, nil
}

func getProjectVolumeCopyRuntime(ctx context.Context, dockerClient *client.Client, toolsImage string) (projectVolumeCopyRuntimeInternal, error) {
	if dockerClient == nil {
		return projectVolumeCopyRuntimeInternal{}, errors.New("docker service unavailable")
	}

	image, err := volumehelper.ResolveHelperImage(ctx, dockerClient, toolsImage)
	if err != nil {
		return projectVolumeCopyRuntimeInternal{}, err
	}

	return projectVolumeCopyRuntimeInternal{
		Image: image,
	}, nil
}

func isProjectVolumeCopyNoSpaceError(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, syscall.ENOSPC) {
		return true
	}
	return strings.Contains(strings.ToLower(err.Error()), "no space left on device")
}

func EnsureTargetsReadyForCleanup(ctx context.Context, dockerClient *client.Client, volumes []types.VolumeJournalVolume) error {
	if len(volumes) == 0 {
		return nil
	}
	if dockerClient == nil {
		return errors.New("docker service unavailable")
	}

	var missingWithSource *types.TargetMissingWithSourceError
	var externallyRemoved []types.VolumeJournalVolume
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
				missingWithSource = &types.TargetMissingWithSourceError{
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
		return &types.VolumesExternallyRemovedError{Volumes: externallyRemoved}
	}
	return nil
}

func RemoveSourceVolumes(ctx context.Context, dockerClient *client.Client, volumes []types.VolumeJournalVolume) error {
	for _, vol := range volumes {
		if err := removeProjectVolumeWithRetry(ctx, dockerClient, vol.OldName, client.VolumeRemoveOptions{Force: false}); err != nil {
			return NewSourceCleanupError(vol.OldName, err)
		}
	}
	docker.InvalidateVolumeUsageCache(dockerClient)
	return nil
}

func RollbackVolumes(ctx context.Context, dockerClient *client.Client, volumes []types.VolumeJournalVolume) error {
	var rollbackErr error
	for _, vol := range slices.Backward(volumes) {
		if err := RollbackVolume(ctx, dockerClient, vol); err != nil {
			rollbackErr = errors.Join(rollbackErr, err)
		}
	}
	if len(volumes) > 0 {
		docker.InvalidateVolumeUsageCache(dockerClient)
	}
	return rollbackErr
}

func RollbackVolume(ctx context.Context, dockerClient *client.Client, vol types.VolumeJournalVolume) error {
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
		return removeProjectRenameJournalTargetVolume(ctx, dockerClient, vol.NewName, oldExists, newExists)
	case !oldExists && newExists:
		return NewTargetPreservedDuringRollbackError(vol, errProjectRenameRollbackSourceMissingInternal)
	case !oldExists:
		slog.WarnContext(ctx, "project rename source and target volumes are missing during rollback", "sourceVolume", vol.OldName, "targetVolume", vol.NewName)
	}
	return nil
}

func CleanupRollbackTargetVolumes(ctx context.Context, dockerClient *client.Client, volumes []types.VolumeJournalVolume) error {
	var cleanupErr error
	for _, vol := range slices.Backward(volumes) {
		if err := cleanupProjectRenameRollbackTargetVolume(ctx, dockerClient, vol); err != nil {
			cleanupErr = errors.Join(cleanupErr, err)
		}
	}
	return cleanupErr
}

func cleanupProjectRenameRollbackTargetVolume(ctx context.Context, dockerClient *client.Client, vol types.VolumeJournalVolume) error {
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
		return removeProjectRenameJournalTargetVolume(ctx, dockerClient, vol.NewName, oldExists, newExists)
	case !oldExists && newExists:
		return NewTargetPreservedDuringRollbackError(vol, errProjectRenameRollbackSourceMissingInternal)
	case !oldExists:
		slog.WarnContext(ctx, "project rename source and target volumes are missing during rollback cleanup", "sourceVolume", vol.OldName, "targetVolume", vol.NewName)
	}
	return nil
}

func removeProjectRenameJournalTargetVolume(ctx context.Context, dockerClient *client.Client, newName string, oldExists, newExists bool) error {
	if !oldExists || !newExists {
		return nil
	}

	if err := removeProjectVolumeHelperContainers(ctx, dockerClient, newName); err != nil {
		return err
	}
	if err := removeProjectVolumeWithRetry(ctx, dockerClient, newName, client.VolumeRemoveOptions{Force: true}); err != nil {
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

func NewSourceCleanupError(sourceVolume string, err error) error {
	return &types.SourceCleanupError{
		SourceVolume: sourceVolume,
		Err:          err,
	}
}

var errProjectRenameRollbackSourceMissingInternal = errors.New("source volume is missing and target volume may contain the only remaining data copy")

func NewTargetPreservedDuringRollbackError(vol types.VolumeJournalVolume, err error) error {
	return &types.TargetPreservedDuringRollbackError{
		SourceVolume: vol.OldName,
		TargetVolume: vol.NewName,
		Err:          err,
	}
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

	var preserved *types.TargetPreservedDuringRollbackError
	return errors.As(err, &preserved)
}

func cloneStringMap(values map[string]string) map[string]string {
	if len(values) == 0 {
		return nil
	}
	cloned := make(map[string]string, len(values))
	maps.Copy(cloned, values)
	return cloned
}
