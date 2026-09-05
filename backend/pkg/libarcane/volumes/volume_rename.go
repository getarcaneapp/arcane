package volumes

import (
	"context"
	stderrors "errors"
	"log/slog"
	"maps"
	"slices"
	"strings"
	"syscall"
	"time"

	"emperror.dev/errors"
	cerrdefs "github.com/containerd/errdefs"
	dockerutil "github.com/getarcaneapp/arcane/backend/v2/pkg/dockerutil"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/libarcane"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/libarcane/volumehelper"
	volumetypes "github.com/getarcaneapp/arcane/types/v2/volume"
	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/client"
)

type dockerProjectVolumeRenameMigrationInternal struct {
	dockerClient   *client.Client
	entries        []volumetypes.RenameEntry
	createdNew     []volumetypes.RenameEntry
	removedOld     []volumetypes.RenameEntry
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
func NewMigration(dockerClient *client.Client, entries []volumetypes.RenameEntry, oldProject, newProject, toolsImage string) volumetypes.Migration {
	if len(entries) == 0 {
		return nil
	}
	return &dockerProjectVolumeRenameMigrationInternal{dockerClient: dockerClient, entries: entries, oldComposeName: oldProject, newComposeName: newProject, toolsImage: toolsImage}
}

// PlanRename validates and prepares an unused Docker volume for a copy-based rename.
func PlanRename(ctx context.Context, dockerClient *client.Client, oldName, newName string, toolsImage string) (volumetypes.Migration, error) {
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
		return nil, errors.WrapIff(err, "inspect source volume %s", oldName)
	}
	if err := EnsureRenameSourceDetached(ctx, dockerClient, oldName); err != nil {
		return nil, err
	}
	if err := EnsureRenameTargetAbsent(ctx, dockerClient, newName); err != nil {
		return nil, err
	}

	entry := volumetypes.RenameEntry{
		OldName:   oldName,
		NewName:   newName,
		OldVolume: oldVolume.Volume,
		CreateOptions: client.VolumeCreateOptions{
			Name:       newName,
			Driver:     oldVolume.Volume.Driver,
			DriverOpts: cloneStringMapInternal(oldVolume.Volume.Options),
			Labels:     cloneStringMapInternal(oldVolume.Volume.Labels),
		},
	}

	return NewMigration(dockerClient, []volumetypes.RenameEntry{entry}, "", "", toolsImage), nil
}

func (m *dockerProjectVolumeRenameMigrationInternal) Apply(ctx context.Context) error {
	if m == nil || len(m.entries) == 0 {
		return nil
	}
	if m.dockerClient == nil {
		return errors.New("docker service unavailable")
	}

	dockerClient := m.dockerClient

	copyRuntime, err := getProjectVolumeCopyRuntimeInternal(ctx, dockerClient, m.toolsImage)
	if err != nil {
		return err
	}

	for _, entry := range m.entries {
		if err := createProjectRenamedVolumeInternal(ctx, dockerClient, entry); err != nil {
			return stderrors.Join(err, m.rollbackCreatedTargetsInternal(ctx, dockerClient))
		}
		m.createdNew = append(m.createdNew, entry)

		if err := copyProjectVolumeDataInternal(ctx, dockerClient, copyRuntime, entry.OldName, entry.NewName); err != nil {
			return stderrors.Join(errors.
				WrapIff(err, "copy volume data from %s to %s", entry.OldName, entry.NewName), m.rollbackCreatedTargetsInternal(ctx, dockerClient),
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

func (m *dockerProjectVolumeRenameMigrationInternal) JournalVolumes() []volumetypes.JournalVolume {
	if m == nil || len(m.entries) == 0 {
		return nil
	}

	volumes := make([]volumetypes.JournalVolume, 0, len(m.entries))
	for _, entry := range m.entries {
		volumes = append(volumes, volumetypes.JournalVolume{
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
		rollbackErr = stderrors.Join(rollbackErr, NewTargetPreservedDuringRollbackError(volumetypes.JournalVolume{
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
			rollbackErr = stderrors.Join(rollbackErr, NewTargetPreservedDuringRollbackError(volumetypes.JournalVolume{
				OldName: entry.OldName,
				NewName: entry.NewName,
			}, errors.WrapIff(err, "inspect source rollback volume %s", entry.OldName)))
			continue
		}
		if sourceExists {
			continue
		}

		targetExists, err := VolumeExists(ctx, dockerClient, entry.NewName)
		if err != nil {
			preservedTargets[entry.NewName] = struct{}{}
			rollbackErr = stderrors.Join(rollbackErr, NewTargetPreservedDuringRollbackError(volumetypes.JournalVolume{
				OldName: entry.OldName,
				NewName: entry.NewName,
			}, errors.WrapIff(err, "inspect target rollback volume %s", entry.NewName)))
			continue
		}
		if targetExists {
			preservedTargets[entry.NewName] = struct{}{}
			rollbackErr = stderrors.Join(rollbackErr, NewTargetPreservedDuringRollbackError(volumetypes.JournalVolume{
				OldName: entry.OldName,
				NewName: entry.NewName,
			}, errProjectRenameRollbackSourceMissingInternal))
		} else {
			rollbackErr = stderrors.Join(rollbackErr, errors.Errorf("source volume %s and target volume %s are missing during rollback", entry.OldName, entry.NewName))
		}
	}

	rollbackErr = stderrors.Join(rollbackErr, m.rollbackCreatedTargetsPreservingInternal(ctx, dockerClient, preservedTargets))
	if rollbackErr == nil {
		dockerutil.InvalidateVolumeUsageCache(dockerClient)
	}
	return rollbackErr
}

func (m *dockerProjectVolumeRenameMigrationInternal) rollbackCreatedTargetsInternal(ctx context.Context, dockerClient *client.Client) error {
	return m.rollbackCreatedTargetsPreservingInternal(ctx, dockerClient, nil)
}

func (m *dockerProjectVolumeRenameMigrationInternal) rollbackCreatedTargetsPreservingInternal(ctx context.Context, dockerClient *client.Client, preservedTargets map[string]struct{}) error {
	var rollbackErr error
	remainingCreated := make([]volumetypes.RenameEntry, 0, len(preservedTargets))
	for _, entry := range slices.Backward(m.createdNew) {
		if _, preserve := preservedTargets[entry.NewName]; preserve {
			remainingCreated = append(remainingCreated, entry)
			continue
		}
		if err := removeProjectVolumeHelperContainersInternal(ctx, dockerClient, entry.NewName); err != nil {
			rollbackErr = stderrors.Join(rollbackErr, errors.WrapIff(err, "remove helper containers for target volume %s", entry.NewName))
			remainingCreated = append(remainingCreated, entry)
			continue
		}
		if err := removeProjectVolumeWithRetryInternal(ctx, dockerClient, entry.NewName, client.VolumeRemoveOptions{Force: true}); err != nil {
			rollbackErr = stderrors.Join(rollbackErr, errors.WrapIff(err, "remove target volume %s", entry.NewName))
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
		return errors.WrapIff(err, "inspect target volume %s", newName)
	}
	return &volumetypes.ProjectVolumeRenameConflictError{VolumeName: newName}
}

func EnsureRenameSourceDetached(ctx context.Context, dockerClient *client.Client, oldName string) error {
	containerIDs, err := dockerutil.GetContainersUsingVolume(ctx, dockerClient, oldName)
	if err != nil {
		return errors.WrapIff(err, "inspect containers using source volume %s", oldName)
	}
	if len(containerIDs) > 0 {
		return &volumetypes.ProjectVolumeRenameInUseError{VolumeName: oldName, ContainerIDs: containerIDs}
	}
	return nil
}

func createProjectRenamedVolumeInternal(ctx context.Context, dockerClient *client.Client, entry volumetypes.RenameEntry) error {
	if _, err := dockerClient.VolumeCreate(ctx, entry.CreateOptions); err != nil {
		return errors.WrapIff(err, "create target volume %s", entry.NewName)
	}
	return nil
}

func removeProjectVolumeHelperContainersInternal(ctx context.Context, dockerClient *client.Client, volumeName string) error {
	containers, err := dockerClient.ContainerList(ctx, client.ContainerListOptions{All: true})
	if err != nil {
		return errors.WrapIf(err, "list containers for helper cleanup")
	}

	var removeErr error
	for _, c := range containers.Items {
		if !isProjectVolumeHelperContainerInternal(c) || !containerSummaryMountsVolumeInternal(c, volumeName) {
			continue
		}
		if _, err := dockerClient.ContainerRemove(ctx, c.ID, volumehelper.RemoveOptions()); err != nil && !cerrdefs.IsNotFound(err) {
			removeErr = stderrors.Join(removeErr, errors.WrapIff(err, "remove helper container %s", c.ID))
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
			return stderrors.Join(ctx.Err(), err)
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
		return errors.WrapIf(err, "read source volume archive")
	}
	defer func() { _ = copyResult.Content.Close() }()

	_, err = dockerClient.CopyToContainer(ctx, targetID, client.CopyToContainerOptions{
		DestinationPath: projectVolumeCopyMountPathInternal,
		Content:         copyResult.Content,
	})
	if err != nil {
		if isProjectVolumeCopyNoSpaceErrorInternal(err) {
			return &volumetypes.ProjectVolumeRenameInsufficientSpaceError{
				SourceVolume: sourceVolume,
				TargetVolume: targetVolume,
				Detail:       err.Error(),
			}
		}
		return errors.WrapIf(err, "write target volume archive")
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
		return "", nil, errors.WrapIf(err, "create volume copy holder")
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

func getProjectVolumeCopyRuntimeInternal(ctx context.Context, dockerClient *client.Client, toolsImage string) (projectVolumeCopyRuntimeInternal, error) {
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

func isProjectVolumeCopyNoSpaceErrorInternal(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, syscall.ENOSPC) {
		return true
	}
	return strings.Contains(strings.ToLower(err.Error()), "no space left on device")
}

func EnsureTargetsReadyForCleanup(ctx context.Context, dockerClient *client.Client, volumes []volumetypes.JournalVolume) error {
	if len(volumes) == 0 {
		return nil
	}
	if dockerClient == nil {
		return errors.New("docker service unavailable")
	}

	var missingWithSource *volumetypes.TargetMissingWithSourceError
	var externallyRemoved []volumetypes.JournalVolume
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
				missingWithSource = &volumetypes.TargetMissingWithSourceError{
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
		return &volumetypes.VolumesExternallyRemovedError{Volumes: externallyRemoved}
	}
	return nil
}

func RemoveSourceVolumes(ctx context.Context, dockerClient *client.Client, volumes []volumetypes.JournalVolume) error {
	for _, vol := range volumes {
		if err := removeProjectVolumeWithRetryInternal(ctx, dockerClient, vol.OldName, client.VolumeRemoveOptions{Force: false}); err != nil {
			return NewSourceCleanupError(vol.OldName, err)
		}
	}
	dockerutil.InvalidateVolumeUsageCache(dockerClient)
	return nil
}

func RollbackVolumes(ctx context.Context, dockerClient *client.Client, volumes []volumetypes.JournalVolume) error {
	var rollbackErr error
	for _, vol := range slices.Backward(volumes) {
		if err := RollbackVolume(ctx, dockerClient, vol); err != nil {
			rollbackErr = stderrors.Join(rollbackErr, err)
		}
	}
	if len(volumes) > 0 {
		dockerutil.InvalidateVolumeUsageCache(dockerClient)
	}
	return rollbackErr
}

func RollbackVolume(ctx context.Context, dockerClient *client.Client, vol volumetypes.JournalVolume) error {
	oldExists, err := VolumeExists(ctx, dockerClient, vol.OldName)
	if err != nil {
		return NewTargetPreservedDuringRollbackError(vol, errors.WrapIff(err, "inspect source rollback volume %s", vol.OldName))
	}
	newExists, err := VolumeExists(ctx, dockerClient, vol.NewName)
	if err != nil {
		return NewTargetPreservedDuringRollbackError(vol, errors.WrapIff(err, "inspect target rollback volume %s", vol.NewName))
	}

	switch {
	case oldExists && newExists:
		return removeProjectRenameJournalTargetVolumeInternal(ctx, dockerClient, vol.NewName, oldExists, newExists)
	case !oldExists && newExists:
		return NewTargetPreservedDuringRollbackError(vol, errProjectRenameRollbackSourceMissingInternal)
	case !oldExists:
		slog.WarnContext(ctx, "project rename source and target volumes are missing during rollback", "sourceVolume", vol.OldName, "targetVolume", vol.NewName)
	}
	return nil
}

func CleanupRollbackTargetVolumes(ctx context.Context, dockerClient *client.Client, volumes []volumetypes.JournalVolume) error {
	var cleanupErr error
	for _, vol := range slices.Backward(volumes) {
		if err := cleanupProjectRenameRollbackTargetVolumeInternal(ctx, dockerClient, vol); err != nil {
			cleanupErr = stderrors.Join(cleanupErr, err)
		}
	}
	return cleanupErr
}

func cleanupProjectRenameRollbackTargetVolumeInternal(ctx context.Context, dockerClient *client.Client, vol volumetypes.JournalVolume) error {
	oldExists, err := VolumeExists(ctx, dockerClient, vol.OldName)
	if err != nil {
		return errors.WrapIff(err, "inspect source cleanup volume %s", vol.OldName)
	}
	newExists, err := VolumeExists(ctx, dockerClient, vol.NewName)
	if err != nil {
		return errors.WrapIff(err, "inspect target cleanup volume %s", vol.NewName)
	}

	switch {
	case oldExists && newExists:
		return removeProjectRenameJournalTargetVolumeInternal(ctx, dockerClient, vol.NewName, oldExists, newExists)
	case !oldExists && newExists:
		return NewTargetPreservedDuringRollbackError(vol, errProjectRenameRollbackSourceMissingInternal)
	case !oldExists:
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
		return errors.WrapIff(err, "remove rollback target volume %s", newName)
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
	return false, errors.WrapIff(err, "inspect volume %s", name)
}

func NewSourceCleanupError(sourceVolume string, err error) error {
	return &volumetypes.SourceCleanupError{
		SourceVolume: sourceVolume,
		Err:          err,
	}
}

var errProjectRenameRollbackSourceMissingInternal = errors.New("source volume is missing and target volume may contain the only remaining data copy")

func NewTargetPreservedDuringRollbackError(vol volumetypes.JournalVolume, err error) error {
	return &volumetypes.TargetPreservedDuringRollbackError{
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

	var preserved *volumetypes.TargetPreservedDuringRollbackError
	return errors.As(err, &preserved)
}

func cloneStringMapInternal(values map[string]string) map[string]string {
	if len(values) == 0 {
		return nil
	}
	cloned := make(map[string]string, len(values))
	maps.Copy(cloned, values)
	return cloned
}
