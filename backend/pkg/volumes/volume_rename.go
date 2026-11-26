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
	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/client"

	"kit.getarcane.app"
	"kit.getarcane.app/concurrency"
	"kit.getarcane.app/docker"

	"getarcane.dev/arcane/v3/internal/common"
	"getarcane.dev/types/v3"
)

type (
	dockerProjectVolumeRenameMigration struct {
		dockerClient   *client.Client
		usageCache     *docker.VolumeUsageCache
		oldComposeName string
		newComposeName string
		toolsImage     string
		entries        []types.VolumeRenameEntry
		createdNew     []types.VolumeRenameEntry
		removedOld     []types.VolumeRenameEntry
	}

	projectVolumeCopyRuntime struct {
		Image string
	}

	MigrationConfig struct {
		DockerClient *client.Client
		UsageCache   *docker.VolumeUsageCache
		OldProject   string
		NewProject   string
		ToolsImage   string
		Entries      []types.VolumeRenameEntry
	}

	PlanRenameConfig struct {
		DockerClient *client.Client
		UsageCache   *docker.VolumeUsageCache
		OldName      string
		NewName      string
		ToolsImage   string
	}
)

const (
	projectVolumeCopyMountPath = "/volume"
)

var errProjectRenameRollbackSourceMissing = errors.New("source volume is missing and target volume may contain the only remaining data copy")

func NewMigration(cfg MigrationConfig) types.VolumeMigration {
	if len(cfg.Entries) == 0 {
		return nil
	}
	return &dockerProjectVolumeRenameMigration{
		dockerClient:   cfg.DockerClient,
		usageCache:     cfg.UsageCache,
		entries:        cfg.Entries,
		oldComposeName: cfg.OldProject,
		newComposeName: cfg.NewProject,
		toolsImage:     cfg.ToolsImage,
	}
}

func PlanRename(ctx context.Context, cfg PlanRenameConfig) (types.VolumeMigration, error) {
	oldName := strings.TrimSpace(cfg.OldName)
	newName := strings.TrimSpace(cfg.NewName)
	if oldName == "" || newName == "" {
		return nil, errors.New("source and target volume names are required")
	}
	if oldName == newName {
		return nil, errors.New("source and target volume names must differ")
	}

	oldVolume, err := cfg.DockerClient.VolumeInspect(ctx, oldName, client.VolumeInspectOptions{})
	if err != nil {
		return nil, fmt.Errorf("inspect source volume %s: %w", oldName, err)
	}
	if ensureRenameSourceDetachedErr := EnsureRenameSourceDetached(ctx, cfg.DockerClient, oldName); ensureRenameSourceDetachedErr != nil {
		return nil, ensureRenameSourceDetachedErr
	}
	if ensureRenameTargetAbsentErr := EnsureRenameTargetAbsent(ctx, cfg.DockerClient, newName); ensureRenameTargetAbsentErr != nil {
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

	return NewMigration(MigrationConfig{
		DockerClient: cfg.DockerClient,
		UsageCache:   cfg.UsageCache,
		Entries:      []types.VolumeRenameEntry{entry},
		ToolsImage:   cfg.ToolsImage,
	}), nil
}

func (m *dockerProjectVolumeRenameMigration) Apply(ctx context.Context) error {
	if len(m.entries) == 0 {
		return nil
	}

	dockerClient := m.dockerClient

	copyRuntime, err := getProjectVolumeCopyRuntime(ctx, dockerClient, m.toolsImage)
	if err != nil {
		return err
	}

	for _, entry := range m.entries {
		if _, volumeCreateErr := dockerClient.VolumeCreate(ctx, entry.CreateOptions); volumeCreateErr != nil {
			return errors.Join(fmt.Errorf("create target volume %s: %w", entry.NewName, volumeCreateErr), m.rollbackCreatedTargetsPreserving(ctx, dockerClient, nil))
		}
		m.createdNew = append(m.createdNew, entry)

		if copyProjectVolumeDataErr := copyProjectVolumeData(ctx, dockerClient, copyRuntime, entry.OldName, entry.NewName); copyProjectVolumeDataErr != nil {
			return errors.Join(fmt.Errorf("copy volume data from %s to %s: %w", entry.OldName, entry.NewName, copyProjectVolumeDataErr), m.rollbackCreatedTargetsPreserving(ctx, dockerClient, nil))
		}
	}

	if m.usageCache != nil {
		m.usageCache.Invalidate(dockerClient)
	}
	slog.InfoContext(ctx, "copied project compose volumes for rename", "oldProject", m.oldComposeName, "newProject", m.newComposeName, "count", len(m.entries))
	return nil
}

func (m *dockerProjectVolumeRenameMigration) Commit(ctx context.Context) error {
	if len(m.entries) == 0 {
		return nil
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

	if m.usageCache != nil {
		m.usageCache.Invalidate(dockerClient)
	}
	slog.InfoContext(ctx, "renamed project compose volumes", "oldProject", m.oldComposeName, "newProject", m.newComposeName, "count", len(m.entries))
	return nil
}

func (m *dockerProjectVolumeRenameMigration) JournalVolumes() []types.VolumeJournalVolume {
	if len(m.entries) == 0 {
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

func (m *dockerProjectVolumeRenameMigration) Rollback(ctx context.Context) error {
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
			}, errProjectRenameRollbackSourceMissing))
		} else {
			rollbackErr = errors.Join(rollbackErr, fmt.Errorf("source volume %s and target volume %s are missing during rollback", entry.OldName, entry.NewName))
		}
	}

	rollbackErr = errors.Join(rollbackErr, m.rollbackCreatedTargetsPreserving(ctx, dockerClient, preservedTargets))
	if rollbackErr == nil && m.usageCache != nil {
		m.usageCache.Invalidate(dockerClient)
	}
	return rollbackErr
}

func (m *dockerProjectVolumeRenameMigration) rollbackCreatedTargetsPreserving(ctx context.Context, dockerClient *client.Client, preservedTargets map[string]struct{}) error {
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

func removeProjectVolumeHelperContainers(ctx context.Context, dockerClient *client.Client, volumeName string) error {
	containers, err := dockerClient.ContainerList(ctx, client.ContainerListOptions{All: true})
	if err != nil {
		return fmt.Errorf("list containers for helper cleanup: %w", err)
	}

	var removeErr error
	for _, c := range containers.Items {
		if !IsHelperContainer(c) || !slices.ContainsFunc(c.Mounts, func(m container.MountPoint) bool {
			return m.Name == volumeName || m.Source == volumeName
		}) {
			continue
		}
		if _, containerRemoveErr := dockerClient.ContainerRemove(ctx, c.ID, RemoveOptions()); containerRemoveErr != nil && !cerrdefs.IsNotFound(containerRemoveErr) {
			removeErr = errors.Join(removeErr, fmt.Errorf("remove helper container %s: %w", c.ID, containerRemoveErr))
		}
	}
	return removeErr
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
		if sleepErr := (concurrency.Backoff{Base: 200 * time.Millisecond}).Sleep(ctx, attempt); sleepErr != nil {
			return errors.Join(sleepErr, err)
		}
	}
	return err
}

func copyProjectVolumeData(ctx context.Context, dockerClient *client.Client, copyRuntime projectVolumeCopyRuntime, sourceVolume, targetVolume string) error {
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
		SourcePath: projectVolumeCopyMountPath + "/.",
	})
	if err != nil {
		return fmt.Errorf("read source volume archive: %w", err)
	}
	defer func() { _ = copyResult.Content.Close() }()

	_, err = dockerClient.CopyToContainer(ctx, targetID, client.CopyToContainerOptions{
		DestinationPath: projectVolumeCopyMountPath,
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
	copyRuntime projectVolumeCopyRuntime,
	volumeName string,
	readOnly bool) (string,
	func(),
	error,
) {
	bind := volumeName + ":" + projectVolumeCopyMountPath
	if readOnly {
		bind += ":ro"
	}

	resp, err := dockerClient.ContainerCreate(ctx, client.ContainerCreateOptions{
		Config: &container.Config{
			Image:           copyRuntime.Image,
			Cmd:             []string{"sleep", "infinity"},
			NetworkDisabled: true,
			Labels:          Labels(),
		},
		HostConfig: HostConfig(HelperHost{
			Image: copyRuntime.Image,
			Binds: []string{bind},
			Keep:  true,
		}),
	})
	if err != nil {
		return "", nil, fmt.Errorf("create volume copy holder: %w", err)
	}

	cleanup := func() {
		cleanupCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 30*time.Second)
		defer cancel()
		if _, containerRemoveErr := dockerClient.ContainerRemove(cleanupCtx, resp.ID, RemoveOptions()); containerRemoveErr != nil && !cerrdefs.IsNotFound(containerRemoveErr) {
			slog.WarnContext(cleanupCtx, "failed to remove volume copy holder", "containerId", resp.ID, "error", containerRemoveErr)
		}
	}

	return resp.ID, cleanup, nil
}

func getProjectVolumeCopyRuntime(ctx context.Context, dockerClient *client.Client, toolsImage string) (projectVolumeCopyRuntime, error) {
	image, err := ResolveHelperImage(ctx, dockerClient, toolsImage)
	if err != nil {
		return projectVolumeCopyRuntime{}, err
	}

	return projectVolumeCopyRuntime{
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
	return nil
}

func RollbackVolumes(ctx context.Context, dockerClient *client.Client, volumes []types.VolumeJournalVolume) error {
	var rollbackErr error
	for _, vol := range slices.Backward(volumes) {
		if err := reconcileRollbackTargetVolume(ctx, dockerClient, vol, true); err != nil {
			rollbackErr = errors.Join(rollbackErr, err)
		}
	}
	return rollbackErr
}

func CleanupRollbackTargetVolumes(ctx context.Context, dockerClient *client.Client, volumes []types.VolumeJournalVolume) error {
	var cleanupErr error
	for _, vol := range slices.Backward(volumes) {
		if err := reconcileRollbackTargetVolume(ctx, dockerClient, vol, false); err != nil {
			cleanupErr = errors.Join(cleanupErr, err)
		}
	}
	return cleanupErr
}

func reconcileRollbackTargetVolume(ctx context.Context, dockerClient *client.Client, vol types.VolumeJournalVolume, preserveInspectErrors bool) error {
	phase := kit.Ternary(preserveInspectErrors, "rollback", "cleanup")
	wrapInspectErr := func(role, name string, err error) error {
		wrapped := fmt.Errorf("inspect %s %s volume %s: %w", role, phase, name, err)
		if preserveInspectErrors {
			return NewTargetPreservedDuringRollbackError(vol, wrapped)
		}
		return wrapped
	}

	oldExists, err := VolumeExists(ctx, dockerClient, vol.OldName)
	if err != nil {
		return wrapInspectErr("source", vol.OldName, err)
	}
	newExists, err := VolumeExists(ctx, dockerClient, vol.NewName)
	if err != nil {
		return wrapInspectErr("target", vol.NewName, err)
	}

	switch {
	case oldExists && newExists:
		if removeHelpersErr := removeProjectVolumeHelperContainers(ctx, dockerClient, vol.NewName); removeHelpersErr != nil {
			return removeHelpersErr
		}
		if removeVolErr := removeProjectVolumeWithRetry(ctx, dockerClient, vol.NewName, client.VolumeRemoveOptions{Force: true}); removeVolErr != nil {
			return fmt.Errorf("remove rollback target volume %s: %w", vol.NewName, removeVolErr)
		}
	case !oldExists && newExists:
		return NewTargetPreservedDuringRollbackError(vol, errProjectRenameRollbackSourceMissing)
	case !oldExists:
		slog.WarnContext(ctx, "project rename source and target volumes are missing", "phase", phase, "sourceVolume", vol.OldName, "targetVolume", vol.NewName)
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
	return maps.Clone(values)
}
