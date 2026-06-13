package services

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"maps"
	"slices"
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

const projectVolumeCopyHelperImageInternal = "busybox:1.37.0"

var prepareProjectRenameVolumeMigrationInternal = func(ctx context.Context, svc *ProjectService, proj *models.Project, name *string, projectsDirectory string) (projectVolumeRenameMigrationInternal, error) {
	return svc.prepareProjectRenameVolumeMigrationInternal(ctx, proj, name)
}

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
	if volumeConfig.Name != oldName || oldName == newName {
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

	helperImage, err := getProjectVolumeCopyHelperImageInternal(ctx, m.service.imageService, dockerClient)
	if err != nil {
		return err
	}

	for _, entry := range m.entries {
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

	for _, entry := range m.entries {
		if err := removeProjectVolumeWithRetryInternal(ctx, dockerClient, entry.OldName, client.VolumeRemoveOptions{Force: false}); err != nil {
			return fmt.Errorf("remove source volume %s: %w", entry.OldName, err)
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

	var restoreErr error
	if len(m.removedOld) > 0 {
		helperImage, err := getProjectVolumeCopyHelperImageInternal(ctx, m.service.imageService, dockerClient)
		if err != nil {
			restoreErr = errors.Join(restoreErr, err)
		} else {
			for _, entry := range slices.Backward(m.removedOld) {
				if err := recreateProjectSourceVolumeInternal(ctx, dockerClient, entry); err != nil {
					restoreErr = errors.Join(restoreErr, err)
					continue
				}
				if err := copyProjectVolumeDataInternal(ctx, dockerClient, helperImage, entry.NewName, entry.OldName); err != nil {
					restoreErr = errors.Join(restoreErr, fmt.Errorf("restore volume data from %s to %s: %w", entry.NewName, entry.OldName, err))
				}
			}
		}
	}

	rollbackErr := errors.Join(restoreErr, m.rollbackCreatedTargets(ctx, dockerClient))
	if rollbackErr == nil {
		dockerutil.InvalidateVolumeUsageCache()
	}
	return rollbackErr
}

func (m *dockerProjectVolumeRenameMigrationInternal) rollbackCreatedTargets(ctx context.Context, dockerClient *client.Client) error {
	var rollbackErr error
	for _, entry := range slices.Backward(m.createdNew) {
		if err := removeProjectVolumeHelperContainersInternal(ctx, dockerClient, entry.NewName); err != nil {
			rollbackErr = errors.Join(rollbackErr, fmt.Errorf("remove helper containers for target volume %s: %w", entry.NewName, err))
			continue
		}
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
		Cmd:             []string{"sh", "-c", projectVolumeCopyCommandInternal()},
		NetworkDisabled: true,
		Labels:          buildVolumeHelperLabelsInternal(),
	}

	hostConfig := buildVolumeHelperHostConfigInternal(helperImage, []string{
		sourceVolume + ":/from:ro",
		targetVolume + ":/to",
	}, nil)
	// Keep the helper until logs are read; runProjectVolumeHelperContainerInternal removes it.
	hostConfig.AutoRemove = false

	if err := runProjectVolumeHelperContainerInternal(ctx, dockerClient, config, hostConfig); err != nil {
		var insufficientErr *ProjectVolumeRenameInsufficientSpaceError
		if errors.As(err, &insufficientErr) {
			insufficientErr.SourceVolume = sourceVolume
			insufficientErr.TargetVolume = targetVolume
		}
		return err
	}

	return nil
}

func runProjectVolumeHelperContainerInternal(ctx context.Context, dockerClient *client.Client, config *container.Config, hostConfig *container.HostConfig) error {
	resp, err := dockerClient.ContainerCreate(ctx, client.ContainerCreateOptions{
		Config:     config,
		HostConfig: hostConfig,
	})
	if err != nil {
		return fmt.Errorf("create volume copy container: %w", err)
	}

	cleanup := func() {
		cleanupCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 30*time.Second)
		defer cancel()
		if _, err := dockerClient.ContainerRemove(cleanupCtx, resp.ID, volumeHelperRemoveOptionsInternal()); err != nil && !cerrdefs.IsNotFound(err) {
			slog.WarnContext(cleanupCtx, "failed to remove volume copy helper", "containerID", resp.ID, "error", err)
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
			logs := readProjectVolumeHelperLogsInternal(ctx, dockerClient, resp.ID)
			if waitBody.StatusCode == projectVolumeCopyInsufficientSpaceExitCodeInternal {
				return &ProjectVolumeRenameInsufficientSpaceError{Detail: logs}
			}
			if logs != "" {
				return fmt.Errorf("volume copy container exited with code %d: %s", waitBody.StatusCode, logs)
			}
			return fmt.Errorf("volume copy container exited with code %d", waitBody.StatusCode)
		}
	case <-ctx.Done():
		return ctx.Err()
	}

	return nil
}

func getProjectVolumeCopyHelperImageInternal(ctx context.Context, imageService *ImageService, dockerClient *client.Client) (string, error) {
	if dockerClient == nil {
		return "", errors.New("docker service unavailable")
	}

	if _, err := dockerClient.ImageInspect(ctx, projectVolumeCopyHelperImageInternal); err == nil {
		return projectVolumeCopyHelperImageInternal, nil
	}

	if imageService == nil {
		return "", fmt.Errorf("volume copy helper image %s unavailable and image service unavailable", projectVolumeCopyHelperImageInternal)
	}
	if err := imageService.PullImage(ctx, projectVolumeCopyHelperImageInternal, io.Discard, systemUser, nil); err != nil {
		return "", fmt.Errorf("pull volume copy helper image %s: %w", projectVolumeCopyHelperImageInternal, err)
	}
	return projectVolumeCopyHelperImageInternal, nil
}

const projectVolumeCopyInsufficientSpaceExitCodeInternal = 99

func projectVolumeCopyCommandInternal() string {
	return fmt.Sprintf(`set -eu
for required_cmd in du df tar; do
  if ! command -v "$required_cmd" >/dev/null 2>&1; then
    echo "volume helper image is missing required command: $required_cmd" >&2
    exit 127
  fi
done
set -- $(du -sk /from)
source_kb="$1"
df_line=""
while IFS= read -r line; do
  df_line="$line"
done <<EOF
$(df -Pk /to)
EOF
set -- $df_line
available_kb="$4"
margin_kb="$((source_kb / 10))"
if [ "$margin_kb" -lt 262144 ]; then margin_kb=262144; fi
required_kb="$((source_kb + margin_kb))"
if [ "$available_kb" -lt "$required_kb" ]; then
  echo "insufficient target volume space: source=${source_kb}KiB available=${available_kb}KiB required=${required_kb}KiB" >&2
  exit %d
fi
cd /from
tar -cf - . | tar -xf - -C /to`, projectVolumeCopyInsufficientSpaceExitCodeInternal)
}

func readProjectVolumeHelperLogsInternal(ctx context.Context, dockerClient *client.Client, containerID string) string {
	logs, err := dockerClient.ContainerLogs(ctx, containerID, client.ContainerLogsOptions{
		ShowStdout: true,
		ShowStderr: true,
	})
	if err != nil {
		slog.DebugContext(ctx, "failed to read volume helper logs", "containerID", containerID, "error", err)
		return ""
	}
	defer func() { _ = logs.Close() }()

	var stdout, stderr bytes.Buffer
	if _, err := stdcopy.StdCopy(&stdout, &stderr, logs); err != nil {
		slog.DebugContext(ctx, "failed to decode volume helper logs", "containerID", containerID, "error", err)
		return ""
	}

	output := strings.TrimSpace(strings.Join([]string{
		strings.TrimSpace(stderr.String()),
		strings.TrimSpace(stdout.String()),
	}, "\n"))
	return output
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
