package container

import (
	"github.com/getarcaneapp/arcane/backend/v2/internal/common"

	"github.com/getarcaneapp/arcane/backend/v2/internal/database"

	"context"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"fmt"
	"io"
	"log/slog"
	"maps"
	"net/netip"
	"strings"
	"sync"
	"time"

	"emperror.dev/errors"

	composetypes "github.com/compose-spec/compose-go/v2/types"
	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/api/types/mount"
	"github.com/moby/moby/api/types/network"
	"github.com/moby/moby/client"

	"github.com/getarcaneapp/arcane/backend/v2/internal/docker"
	"github.com/getarcaneapp/arcane/backend/v2/internal/event"
	"github.com/getarcaneapp/arcane/backend/v2/internal/image"
	"github.com/getarcaneapp/arcane/backend/v2/internal/project"
	"github.com/getarcaneapp/arcane/backend/v2/internal/settings"
	dockerutils "github.com/getarcaneapp/arcane/backend/v2/pkg/dockerutil"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/libarcane"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/libarcane/timeouts"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/pagination"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/projects"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/utils/iconcatalog"
	containertypes "github.com/getarcaneapp/arcane/types/v2/container"
	"github.com/getarcaneapp/arcane/types/v2/containerregistry"
	imagetypes "github.com/getarcaneapp/arcane/types/v2/image"
	"github.com/samber/hot"
	containerstats "go.getarcane.app/streams/stats"
	"go.getarcane.app/sys/cgroup"
	"go.getarcane.app/updater/labels"
)

type ContainerService struct {
	dockerService   *docker.DockerClientService
	eventService    *event.EventService
	imageService    *image.ImageService
	settingsService *settings.SettingsService
	projectService  *project.ProjectService
	statsHistory    containerstats.Store
	iconMetaCache   *hot.HotCache[string, projects.ArcaneComposeMetadata]
}

const (
	containerGroupByProject  = "project"
	containerNoProjectGroup  = "No Project"
	containerIconMetadataTTL = 5 * time.Second
)

type ContainerListResult struct {
	Items      []containertypes.Summary
	Groups     []containertypes.SummaryGroup
	Pagination pagination.Response
	Counts     containertypes.StatusCounts
}

func NewContainerService(eventService *event.EventService, dockerService *docker.DockerClientService, imageService *image.ImageService, settingsService *settings.SettingsService, projectService *project.ProjectService) *ContainerService {
	return &ContainerService{
		eventService:    eventService,
		dockerService:   dockerService,
		imageService:    imageService,
		settingsService: settingsService,
		projectService:  projectService,
		iconMetaCache: hot.NewHotCache[string, projects.ArcaneComposeMetadata](hot.LRU, 1024).
			WithTTL(containerIconMetadataTTL).
			WithJanitor().
			Build(),
	}
}

func buildCleanNetworkingConfigInternal(containerInspect container.InspectResponse, apiVersion string) *network.NetworkingConfig {
	if containerInspect.NetworkSettings == nil || len(containerInspect.NetworkSettings.Networks) == 0 {
		return nil
	}

	endpointsConfig := libarcane.SanitizeContainerCreateEndpointSettingsForDockerAPI(containerInspect.NetworkSettings.Networks, apiVersion)
	for networkName, endpoint := range endpointsConfig {
		if endpoint == nil {
			continue
		}

		endpointCopy := *endpoint
		endpointCopy.IPAMConfig = nil
		endpointsConfig[networkName] = &endpointCopy
	}

	if len(endpointsConfig) == 0 {
		return nil
	}

	return &network.NetworkingConfig{
		EndpointsConfig: endpointsConfig,
	}
}

func buildRedeployBackupNameInternal(containerName, containerID string) string {
	backupName := containerName
	if backupName == "" {
		backupName = "arcane-redeploy"
		if len(containerID) >= 12 {
			backupName = fmt.Sprintf("%s-%s", backupName, containerID[:12])
		}
	}

	return fmt.Sprintf("%s-arcane-redeploy-%d", backupName, time.Now().Unix())
}

func shouldStartRedeployedContainerInternal(containerInfo container.InspectResponse, wasRunning bool) bool {
	if !wasRunning && containerInfo.HostConfig == nil {
		return false
	}

	shouldStart := wasRunning
	if containerInfo.HostConfig != nil {
		rp := containerInfo.HostConfig.RestartPolicy.Name
		if rp == "always" || rp == "unless-stopped" || rp == "on-failure" {
			shouldStart = true
		}
	}

	return shouldStart
}

func (s *ContainerService) pullRedeployImageInternal(ctx context.Context, dockerClient *client.Client, imageName, containerID, containerName, action string, credentials []containerregistry.Credential, user common.User) error {
	settings := s.settingsService.GetSettingsConfig()
	pullCtx, pullCancel := context.WithTimeout(ctx, timeouts.GetDuration(settings.DockerImagePullTimeout.AsInt(), timeouts.DefaultDockerImagePull))
	defer pullCancel()

	pullOptions, authErr := s.imageService.PullOptionsWithAuth(ctx, imageName, credentials)
	if authErr != nil {
		slog.WarnContext(ctx, "failed to get registry authentication for container recreate pull; proceeding without auth",
			"image", imageName,
			"error", authErr.Error(),
		)
		pullOptions = client.ImagePullOptions{}
	}

	reader, pullErr := dockerClient.ImagePull(pullCtx, imageName, pullOptions)
	if pullErr != nil && image.ShouldRetryAnonymousPull(pullOptions, pullErr) {
		slog.WarnContext(ctx, "container recreate image pull failed with registry auth; retrying anonymously",
			"image", imageName,
			"error", pullErr.Error(),
		)
		pullOptions = client.ImagePullOptions{}
		reader, pullErr = dockerClient.ImagePull(pullCtx, imageName, pullOptions)
	}
	if pullErr != nil {
		if errors.Is(pullCtx.Err(), context.DeadlineExceeded) {
			s.eventService.LogErrorEvent(ctx, event.EventTypeContainerError, "container", containerID, containerName, user.ID, user.Username, "0", pullErr, database.JSON{
				"action": action,
				"step":   "pull_image_timeout",
				"image":  imageName,
			})
			return errors.Errorf("image pull timed out for %s (increase DOCKER_IMAGE_PULL_TIMEOUT or setting)", imageName)
		}

		s.eventService.LogErrorEvent(ctx, event.EventTypeContainerError, "container", containerID, containerName, user.ID, user.Username, "0", pullErr, database.JSON{
			"action": action,
			"step":   "pull_image",
			"image":  imageName,
		})
		return errors.WrapIff(pullErr, "failed to pull image %s", imageName)
	}
	defer func() { _ = reader.Close() }()

	progressWriter, _ := ctx.Value(dockerutils.ProgressWriterKey{}).(io.Writer)
	logWriter := dockerutils.NewLogLineWriter(progressWriter)
	defer func() { _ = logWriter.Close() }()

	streamErr := dockerutils.RenderJSONMessageStream(reader, logWriter)
	if streamErr != nil {
		s.eventService.LogErrorEvent(ctx, event.EventTypeContainerError, "container", containerID, containerName, user.ID, user.Username, "0", streamErr, database.JSON{
			"action": action,
			"step":   "complete_pull",
			"image":  imageName,
		})
		return errors.WrapIf(streamErr, "failed to complete image pull")
	}

	return nil
}

func (s *ContainerService) prepareContainerForRedeployInternal(ctx context.Context, dockerClient *client.Client, containerID, containerName, backupName string, wasRunning bool, user common.User) error {
	if containerName != "" {
		if _, err := dockerClient.ContainerRename(ctx, containerID, client.ContainerRenameOptions{NewName: backupName}); err != nil {
			s.eventService.LogErrorEvent(ctx, event.EventTypeContainerError, "container", containerID, containerName, user.ID, user.Username, "0", err, database.JSON{
				"action":     "redeploy",
				"step":       "rename_old",
				"backupName": backupName,
			})
			return errors.WrapIf(err, "failed to rename existing container")
		}
	}

	if !wasRunning {
		return nil
	}

	_, err := dockerClient.ContainerStop(ctx, containerID, client.ContainerStopOptions{Timeout: new(30)})
	if err == nil {
		return nil
	}

	if containerName != "" {
		if _, renameErr := dockerClient.ContainerRename(ctx, containerID, client.ContainerRenameOptions{NewName: containerName}); renameErr != nil {
			s.eventService.LogErrorEvent(ctx, event.EventTypeContainerError, "container", containerID, containerName, user.ID, user.Username, "0", renameErr, database.JSON{
				"action": "redeploy",
				"step":   "restore_name_after_stop_failure",
			})
		}
	}

	s.eventService.LogErrorEvent(ctx, event.EventTypeContainerError, "container", containerID, containerName, user.ID, user.Username, "0", err, database.JSON{
		"action": "redeploy",
		"step":   "stop",
	})
	return errors.WrapIf(err, "failed to stop container")
}

func (s *ContainerService) restoreContainerAfterRedeployFailureInternal(ctx context.Context, dockerClient *client.Client, containerID, containerName, backupName, failedStep string, wasRunning bool, user common.User) {
	if wasRunning {
		if _, startErr := dockerClient.ContainerStart(ctx, containerID, client.ContainerStartOptions{}); startErr != nil {
			s.eventService.LogErrorEvent(ctx, event.EventTypeContainerError, "container", containerID, containerName, user.ID, user.Username, "0", startErr, database.JSON{
				"action":     "redeploy",
				"step":       "restore_start_original",
				"failedStep": failedStep,
			})
		}
	}

	if containerName == "" {
		return
	}

	if _, renameErr := dockerClient.ContainerRename(ctx, containerID, client.ContainerRenameOptions{NewName: containerName}); renameErr != nil {
		s.eventService.LogErrorEvent(ctx, event.EventTypeContainerError, "container", containerID, backupName, user.ID, user.Username, "0", renameErr, database.JSON{
			"action":     "redeploy",
			"step":       "restore_name",
			"failedStep": failedStep,
		})
	}
}

// restoreAutoRemoveContainerAfterStartFailureInternal recreates and starts the
// original container when a deferred-stop auto-remove recreate fails to start
// the replacement. Stopping the original already made the daemon delete it, so
// the only rollback left is rebuilding from the inspected configuration. The
// failed replacement must be removed first so the original name is free again.
// A returned error means the original container is gone and could not be
// restored; the caller must surface it alongside the replacement failure.
func (s *ContainerService) restoreAutoRemoveContainerAfterStartFailureInternal(ctx context.Context, dockerClient *client.Client, containerInfo container.InspectResponse, apiVersion string, user common.User) error {
	containerID := containerInfo.ID
	containerName := strings.TrimPrefix(containerInfo.Name, "/")

	originalConfig := *containerInfo.Config
	if len(containerID) >= 12 && originalConfig.Hostname == containerID[:12] {
		originalConfig.Hostname = ""
	}

	createResp, err := libarcane.ContainerCreateWithCompatibilityForAPIVersion(ctx, dockerClient, client.ContainerCreateOptions{
		Config:           &originalConfig,
		HostConfig:       containerInfo.HostConfig,
		NetworkingConfig: buildCleanNetworkingConfigInternal(containerInfo, apiVersion),
		Name:             containerName,
	}, apiVersion)
	if err != nil {
		s.eventService.LogErrorEvent(ctx, event.EventTypeContainerError, "container", containerID, containerName, user.ID, user.Username, "0", err, database.JSON{
			"action": "redeploy",
			"step":   "restore_create_original",
		})
		return errors.WrapIf(err, "failed to recreate original auto-remove container")
	}

	if _, err := dockerClient.ContainerStart(ctx, createResp.ID, client.ContainerStartOptions{}); err != nil {
		s.eventService.LogErrorEvent(ctx, event.EventTypeContainerError, "container", createResp.ID, containerName, user.ID, user.Username, "0", err, database.JSON{
			"action": "redeploy",
			"step":   "restore_start_original",
		})
		return errors.WrapIf(err, "failed to restart original auto-remove container")
	}

	slog.InfoContext(ctx, "restored auto-remove container after failed recreate",
		"oldContainerId", containerID,
		"restoredContainerId", createResp.ID,
		"containerName", containerName,
	)
	return nil
}

// rollbackFailedReplacementStartInternal removes the replacement that failed
// to start and rolls back to the original container. For a deferred-stop
// auto-remove original the rollback rebuilds it from the inspected
// configuration; a failed restore is combined into the start error so the
// caller learns the workload is down, not just that the replacement failed.
func (s *ContainerService) rollbackFailedReplacementStartInternal(ctx context.Context, dockerClient *client.Client, containerInfo container.InspectResponse, replacementID, action, backupName string, stopAfterCreate, wasRunning bool, apiVersion string, startErr error, user common.User) error {
	containerName := strings.TrimPrefix(containerInfo.Name, "/")

	var cleanupErr error
	if _, removeErr := dockerClient.ContainerRemove(ctx, replacementID, client.ContainerRemoveOptions{Force: true}); removeErr != nil {
		cleanupErr = errors.WrapIf(removeErr, "failed to remove replacement container")
		s.eventService.LogErrorEvent(ctx, event.EventTypeContainerError, "container", replacementID, containerName, user.ID, user.Username, "0", removeErr, database.JSON{
			"action": action,
			"step":   "cleanup_failed_start",
		})
	}

	if stopAfterCreate {
		if restoreErr := s.restoreAutoRemoveContainerAfterStartFailureInternal(ctx, dockerClient, containerInfo, apiVersion, user); restoreErr != nil {
			return errors.Combine(startErr, cleanupErr, restoreErr)
		}
		return startErr
	}

	s.restoreContainerAfterRedeployFailureInternal(ctx, dockerClient, containerInfo.ID, containerName, backupName, "start", wasRunning, user)
	return startErr
}

type containerLifecycleActionInternal struct {
	action             string
	eventType          event.EventType
	metadata           database.JSON
	warnOnLogError     bool
	runContainerAction func(*client.Client) error
}

func (s *ContainerService) runContainerLifecycleActionInternal(ctx context.Context, containerID string, user common.User, cfg containerLifecycleActionInternal) error {
	dockerClient, err := s.dockerService.GetClient(ctx)
	if err != nil {
		s.eventService.LogErrorEvent(ctx, event.EventTypeContainerError, "container", containerID, "", user.ID, user.Username, "0", err, database.JSON{"action": cfg.action})
		return errors.WrapIf(err, "failed to connect to Docker")
	}

	metadata := database.JSON{
		"action":      cfg.action,
		"containerId": containerID,
	}
	maps.Copy(metadata, cfg.metadata)

	err = s.eventService.LogContainerEvent(ctx, cfg.eventType, containerID, "name", user.ID, user.Username, "0", metadata)
	if err != nil {
		if !cfg.warnOnLogError {
			return errors.WrapIf(err, "failed to log action")
		}
		slog.WarnContext(ctx, "could not log container action", "action", cfg.action, "error", err)
	}

	err = cfg.runContainerAction(dockerClient)
	if err != nil {
		s.eventService.LogErrorEvent(ctx, event.EventTypeContainerError, "container", containerID, "", user.ID, user.Username, "0", err, database.JSON{"action": cfg.action})
	}
	return err
}

func (s *ContainerService) StartContainer(ctx context.Context, containerID string, user common.User) error {
	return s.runContainerLifecycleActionInternal(ctx, containerID, user, containerLifecycleActionInternal{
		action:         "start",
		eventType:      event.EventTypeContainerStart,
		warnOnLogError: true,
		runContainerAction: func(dockerClient *client.Client) error {
			_, err := dockerClient.ContainerStart(ctx, containerID, client.ContainerStartOptions{})
			return err
		},
	})
}

func (s *ContainerService) StopContainer(ctx context.Context, containerID string, user common.User) error {
	return s.runContainerLifecycleActionInternal(ctx, containerID, user, containerLifecycleActionInternal{
		action:    "stop",
		eventType: event.EventTypeContainerStop,
		runContainerAction: func(dockerClient *client.Client) error {
			_, err := dockerClient.ContainerStop(ctx, containerID, client.ContainerStopOptions{Timeout: new(30)})
			return err
		},
	})
}

func (s *ContainerService) RestartContainer(ctx context.Context, containerID string, user common.User) error {
	return s.runContainerLifecycleActionInternal(ctx, containerID, user, containerLifecycleActionInternal{
		action:    "restart",
		eventType: event.EventTypeContainerRestart,
		runContainerAction: func(dockerClient *client.Client) error {
			_, err := dockerClient.ContainerRestart(ctx, containerID, client.ContainerRestartOptions{})
			return err
		},
	})
}

// KillContainer sends a signal to the container's main process (default SIGKILL
// when signal is empty) without removing the container.
func (s *ContainerService) KillContainer(ctx context.Context, containerID, signal string, user common.User) error {
	return s.runContainerLifecycleActionInternal(ctx, containerID, user, containerLifecycleActionInternal{
		action:         "kill",
		eventType:      event.EventTypeContainerKill,
		metadata:       database.JSON{"signal": signal},
		warnOnLogError: true,
		runContainerAction: func(dockerClient *client.Client) error {
			_, err := dockerClient.ContainerKill(ctx, containerID, client.ContainerKillOptions{Signal: signal})
			return err
		},
	})
}

// PauseContainer suspends all processes in the container.
func (s *ContainerService) PauseContainer(ctx context.Context, containerID string, user common.User) error {
	return s.runContainerLifecycleActionInternal(ctx, containerID, user, containerLifecycleActionInternal{
		action:         "pause",
		eventType:      event.EventTypeContainerPause,
		warnOnLogError: true,
		runContainerAction: func(dockerClient *client.Client) error {
			_, err := dockerClient.ContainerPause(ctx, containerID, client.ContainerPauseOptions{})
			return err
		},
	})
}

// UnpauseContainer resumes a previously paused container.
func (s *ContainerService) UnpauseContainer(ctx context.Context, containerID string, user common.User) error {
	return s.runContainerLifecycleActionInternal(ctx, containerID, user, containerLifecycleActionInternal{
		action:         "unpause",
		eventType:      event.EventTypeContainerUnpause,
		warnOnLogError: true,
		runContainerAction: func(dockerClient *client.Client) error {
			_, err := dockerClient.ContainerUnpause(ctx, containerID, client.ContainerUnpauseOptions{})
			return err
		},
	})
}

// CommitContainer creates an image from a container's current filesystem.
func (s *ContainerService) CommitContainer(ctx context.Context, containerID string, req containertypes.CommitRequest, user common.User) (*containertypes.CommitResult, error) {
	containerID = strings.TrimSpace(containerID)
	if containerID == "" {
		return nil, errors.New("container ID is required")
	}

	dockerClient, err := s.dockerService.GetClient(ctx)
	if err != nil {
		s.eventService.LogErrorEvent(ctx, event.EventTypeImageError, "container", containerID, "", user.ID, user.Username, "0", err, database.JSON{"action": "commit"})
		return nil, errors.WrapIf(err, "failed to connect to Docker")
	}

	repository := strings.TrimSpace(req.Repository)
	tag := strings.TrimSpace(req.Tag)
	reference := repository
	if repository != "" && tag != "" {
		reference = repository + ":" + tag
	}

	result, err := dockerClient.ContainerCommit(ctx, containerID, client.ContainerCommitOptions{
		Reference: reference,
		Comment:   strings.TrimSpace(req.Comment),
		Author:    strings.TrimSpace(req.Author),
		Changes:   req.Changes,
		NoPause:   req.NoPause,
	})
	if err != nil {
		s.eventService.LogErrorEvent(ctx, event.EventTypeImageError, "container", containerID, reference, user.ID, user.Username, "0", err, database.JSON{"action": "commit", "reference": reference})
		return nil, errors.WrapIf(err, "failed to commit container")
	}

	metadata := database.JSON{
		"action":      "commit",
		"containerId": containerID,
		"imageId":     result.ID,
		"repository":  repository,
		"tag":         tag,
		"reference":   reference,
		"noPause":     req.NoPause,
	}
	if logErr := s.eventService.LogImageEvent(ctx, event.EventTypeImageCommit, containerID, reference, user.ID, user.Username, "0", metadata); logErr != nil {
		slog.WarnContext(ctx, "could not log container commit action", "container", containerID, "image", result.ID, "error", logErr)
	}

	return &containertypes.CommitResult{ID: result.ID}, nil
}

// tryRedeployViaComposeProjectInternal attempts to redeploy a compose-managed
// container by delegating to project.ProjectService.UpdateProjectServices, which loads
// the compose project with full project_directory / env-file / include context
// and runs pull/stop/up for just the target service.
//
// Return semantics:
//   - handled=false: this container is not eligible for the compose path (no
//     labels, project not registered in Arcane's DB, etc.). The caller should
//     fall back to the standalone Docker-API redeploy.
//   - handled=true, err==nil: compose path ran successfully; newContainerID is
//     the ID of the recreated container (or the original ID if it couldn't be
//     re-located by labels).
//   - handled=true, err!=nil: compose path was attempted and failed. The
//     caller MUST surface the error and MUST NOT fall back to the standalone
//     path, which would clobber whatever partial state ComposeUp left behind.
func (s *ContainerService) tryRedeployViaComposeProjectInternal(ctx context.Context, containerInfo container.InspectResponse, containerID, containerName string, user common.User) (string, bool, error) {
	if s.projectService == nil || containerInfo.Config == nil {
		return "", false, nil
	}
	containerLabels := containerInfo.Config.Labels
	projectName := dockerutils.ComposeProjectLabel(containerLabels)
	serviceName := dockerutils.ComposeServiceLabel(containerLabels)
	if projectName == "" || serviceName == "" {
		return "", false, nil
	}

	proj, err := s.projectService.GetProjectByComposeName(ctx, projectName)
	if err != nil {
		// Distinguish "not found" (safe to fall back to standalone) from real DB
		// errors (should surface so a transient failure doesn't silently recreate
		// the container from stale cached config).
		if strings.Contains(err.Error(), "not found") {
			slog.WarnContext(ctx, "RedeployContainer: compose project not registered, falling back to standalone redeploy",
				"containerId", containerID,
				"project", projectName,
				"service", serviceName,
			)
			return "", false, nil
		}
		return "", true, errors.WrapIff(err, "failed to look up compose project %s", projectName)
	}
	if proj == nil {
		slog.WarnContext(ctx, "RedeployContainer: compose project not registered, falling back to standalone redeploy",
			"containerId", containerID,
			"project", projectName,
			"service", serviceName,
		)
		return "", false, nil
	}

	slog.InfoContext(ctx, "RedeployContainer: detected compose container, using project-based redeploy",
		"containerId", containerID,
		"project", projectName,
		"service", serviceName,
	)

	if err := s.projectService.UpdateProjectServices(ctx, proj.ID, []string{serviceName}, user); err != nil {
		s.eventService.LogErrorEvent(ctx, event.EventTypeContainerError, "container", containerID, containerName, user.ID, user.Username, "0", err, database.JSON{
			"action":      "redeploy",
			"step":        "compose_update_services",
			"project":     projectName,
			"service":     serviceName,
			"projectId":   proj.ID,
			"projectName": proj.Name,
		})
		return "", true, errors.WrapIff(err, "compose redeploy failed for %s/%s", projectName, serviceName)
	}

	newID := s.findComposeServiceContainerIDInternal(ctx, projectName, serviceName)
	if newID == "" {
		// Recreated successfully but couldn't locate the new container; return the
		// original ID so the handler can degrade gracefully.
		newID = containerID
	}

	if logErr := s.eventService.LogContainerEvent(ctx, event.EventTypeContainerDeploy, newID, containerName, user.ID, user.Username, "0", database.JSON{
		"action":        "redeploy",
		"containerId":   newID,
		"containerName": containerName,
		"project":       projectName,
		"service":       serviceName,
		"projectId":     proj.ID,
		"via":           "compose",
	}); logErr != nil {
		slog.WarnContext(ctx, "failed to log compose redeploy event", "err", logErr)
	}

	return newID, true, nil
}

// findComposeServiceContainerIDInternal locates the (presumably newly recreated)
// container for a given compose project+service pair using the compose SDK's Ps
// command. When multiple containers match (a stopped predecessor can briefly
// linger during recreation), the first running one is preferred; otherwise the
// first match is returned. Returns "" when none found.
func (s *ContainerService) findComposeServiceContainerIDInternal(ctx context.Context, projectName, serviceName string) string {
	containers, err := projects.ComposePs(ctx, s.dockerService.DockerHost(), &composetypes.Project{Name: projectName}, []string{serviceName}, true)
	if err != nil {
		slog.WarnContext(ctx, "failed to resolve container via compose ps after redeploy",
			"project", projectName,
			"service", serviceName,
			"err", err,
		)
		return ""
	}

	var firstMatch string
	for _, c := range containers {
		if c.Service != serviceName {
			continue
		}
		if firstMatch == "" {
			firstMatch = c.ID
		}
		if c.State == "running" {
			return c.ID
		}
	}
	return firstMatch
}

func (s *ContainerService) GenerateCompose(ctx context.Context, containerIDs []string) (string, error) {
	proj, err := projects.ComposeGenerate(ctx, s.dockerService.DockerHost(), "", containerIDs)
	if err != nil {
		return "", err
	}

	content, err := proj.MarshalYAML()
	if err != nil {
		return "", errors.WithMessage(err, "failed to marshal generated compose project")
	}
	return string(content), nil
}

func (s *ContainerService) RedeployContainer(ctx context.Context, containerID string, user common.User) (string, error) {
	dockerClient, err := s.dockerService.GetClient(ctx)
	if err != nil {
		s.eventService.LogErrorEvent(ctx, event.EventTypeContainerError, "container", containerID, "", user.ID, user.Username, "0", err, database.JSON{
			"action": "redeploy",
			"step":   "get_client",
		})
		return "", errors.WrapIf(err, "failed to connect to Docker")
	}

	containerJSON, err := libarcane.ContainerInspectWithCompatibility(ctx, dockerClient, containerID, client.ContainerInspectOptions{})
	if err != nil {
		s.eventService.LogErrorEvent(ctx, event.EventTypeContainerError, "container", containerID, "", user.ID, user.Username, "0", err, database.JSON{
			"action": "redeploy",
			"step":   "inspect",
		})
		return "", errors.WrapIf(err, "failed to inspect container")
	}

	containerInfo := containerJSON.Container
	if containerInfo.Config == nil {
		err = errors.New("container config is nil")
		s.eventService.LogErrorEvent(ctx, event.EventTypeContainerError, "container", containerID, "", user.ID, user.Username, "0", err, database.JSON{
			"action": "redeploy",
			"step":   "validate_config",
		})
		return "", errors.WrapIf(err, "failed to redeploy container")
	}

	containerName := strings.TrimPrefix(containerInfo.Name, "/")
	imageName := containerInfo.Config.Image
	apiVersion := libarcane.DetectDockerAPIVersion(ctx, dockerClient)

	currentContainerID, currentContainerErr := cgroup.CurrentContainerID()
	if labels.ShouldDisableArcaneServerRedeploy(containerInfo.Config.Labels, containerInfo.ID, currentContainerID, currentContainerErr) {
		err = errors.New("arcane cannot redeploy itself; use the system upgrade flow (Settings -> Updates) instead")
		s.eventService.LogErrorEvent(ctx, event.EventTypeContainerError, "container", containerID, containerName, user.ID, user.Username, "0", err, database.JSON{
			"action": "redeploy",
			"step":   "self_redeploy_blocked",
		})
		return "", err
	}

	// If this container belongs to a known compose project, redeploy through the
	// compose-aware path so that compose file changes (healthchecks, env, etc.) and
	// the project's include/project_directory/env-file context are honored. The
	// standalone Docker-API path below only clones the existing container config
	// from the daemon and would silently ignore any compose edits.
	if newID, handled, composeErr := s.tryRedeployViaComposeProjectInternal(ctx, containerInfo, containerID, containerName, user); handled {
		if composeErr != nil {
			return "", composeErr
		}
		return newID, nil
	}

	if imageName != "" {
		if err := s.pullRedeployImageInternal(ctx, dockerClient, imageName, containerID, containerName, "redeploy", nil, user); err != nil {
			return "", err
		}
	}

	networkingConfig := buildCleanNetworkingConfigInternal(containerInfo, apiVersion)
	newConfig := *containerInfo.Config

	return s.recreateContainerInternal(ctx, dockerClient, containerInfo, "redeploy", containerName, &newConfig, containerInfo.HostConfig, networkingConfig, apiVersion, event.EventTypeContainerDeploy, user)
}

// recreateContainerInternal replaces an existing container with one created
// from the supplied config, using rename-as-backup so the original can be
// restored when create or start fails. Shared by redeploy and edit.
func (s *ContainerService) recreateContainerInternal(ctx context.Context, dockerClient *client.Client, containerInfo container.InspectResponse, action, newName string, newConfig *container.Config, newHostConfig *container.HostConfig, networkingConfig *network.NetworkingConfig, apiVersion string, eventType event.EventType, user common.User) (string, error) {
	containerID := containerInfo.ID
	containerName := strings.TrimPrefix(containerInfo.Name, "/")
	wasRunning := containerInfo.State != nil && containerInfo.State.Running
	imageName := newConfig.Image

	// A running auto-remove container is deleted by the daemon the moment it
	// stops, which would make rollback impossible. For that case the stop is
	// deferred until the replacement has been created: the rename alone frees
	// the name, and a create failure rolls back to the untouched, still-running
	// original. Only a start failure can still lose the original.
	stopAfterCreate := wasRunning && containerInfo.HostConfig != nil && containerInfo.HostConfig.AutoRemove

	backupName := buildRedeployBackupNameInternal(containerName, containerID)
	if err := s.prepareContainerForRedeployInternal(ctx, dockerClient, containerID, containerName, backupName, wasRunning && !stopAfterCreate, user); err != nil {
		return "", err
	}

	if len(containerID) >= 12 && newConfig.Hostname == containerID[:12] {
		newConfig.Hostname = ""
	}

	createResp, err := libarcane.ContainerCreateWithCompatibilityForAPIVersion(ctx, dockerClient, client.ContainerCreateOptions{
		Config:           newConfig,
		HostConfig:       newHostConfig,
		NetworkingConfig: networkingConfig,
		Name:             newName,
	}, apiVersion)
	if err != nil {
		s.restoreContainerAfterRedeployFailureInternal(ctx, dockerClient, containerID, containerName, backupName, "create", wasRunning && !stopAfterCreate, user)
		s.eventService.LogErrorEvent(ctx, event.EventTypeContainerError, "container", containerID, containerName, user.ID, user.Username, "0", err, database.JSON{
			"action": action,
			"step":   "create",
			"image":  imageName,
		})
		return "", errors.WrapIf(err, "failed to recreate container")
	}

	if stopAfterCreate {
		if _, err := dockerClient.ContainerStop(ctx, containerID, client.ContainerStopOptions{Timeout: new(30)}); err != nil {
			if _, removeErr := dockerClient.ContainerRemove(ctx, createResp.ID, client.ContainerRemoveOptions{Force: true}); removeErr != nil {
				s.eventService.LogErrorEvent(ctx, event.EventTypeContainerError, "container", createResp.ID, containerName, user.ID, user.Username, "0", removeErr, database.JSON{
					"action": action,
					"step":   "cleanup_failed_stop",
				})
			}
			s.restoreContainerAfterRedeployFailureInternal(ctx, dockerClient, containerID, containerName, backupName, "stop", false, user)
			s.eventService.LogErrorEvent(ctx, event.EventTypeContainerError, "container", containerID, containerName, user.ID, user.Username, "0", err, database.JSON{
				"action": action,
				"step":   "stop",
			})
			return "", errors.WrapIf(err, "failed to stop container")
		}
	}

	if shouldStartRedeployedContainerInternal(containerInfo, wasRunning) {
		_, err = dockerClient.ContainerStart(ctx, createResp.ID, client.ContainerStartOptions{})
		if err != nil {
			err = s.rollbackFailedReplacementStartInternal(ctx, dockerClient, containerInfo, createResp.ID, action, backupName, stopAfterCreate, wasRunning, apiVersion, err, user)
			s.eventService.LogErrorEvent(ctx, event.EventTypeContainerError, "container", createResp.ID, containerName, user.ID, user.Username, "0", err, database.JSON{
				"action": action,
				"step":   "start",
				"image":  imageName,
			})
			return "", errors.WrapIf(err, "failed to start new container")
		}
	}

	slog.InfoContext(ctx, "container recreated successfully",
		"action", action,
		"oldContainerId", containerID,
		"newContainerId", createResp.ID,
		"containerName", newName,
		"image", imageName,
	)

	// After a deferred stop the daemon has already auto-removed the original.
	if !stopAfterCreate {
		if _, err := dockerClient.ContainerRemove(ctx, containerID, client.ContainerRemoveOptions{
			Force:         true,
			RemoveVolumes: false,
			RemoveLinks:   false,
		}); err != nil {
			slog.WarnContext(ctx, "failed to remove old container after successful recreate",
				"containerId", containerID,
				"backupName", backupName,
				"error", err,
			)
		}
	}

	metadata := database.JSON{
		"action":        action,
		"containerId":   containerID,
		"containerName": newName,
		"image":         imageName,
	}
	if logErr := s.eventService.LogContainerEvent(ctx, eventType, createResp.ID, newName, user.ID, user.Username, "0", metadata); logErr != nil {
		slog.WarnContext(ctx, "failed to log recreate event", "err", logErr)
	}

	return createResp.ID, nil
}

func healthConfigFromCreateInternal(hc *containertypes.HealthcheckCreate) *container.HealthConfig {
	if hc == nil {
		return nil
	}

	return &container.HealthConfig{
		Test:          append([]string{}, hc.Test...),
		Interval:      time.Duration(hc.Interval) * time.Second,
		Timeout:       time.Duration(hc.Timeout) * time.Second,
		StartPeriod:   time.Duration(hc.StartPeriod) * time.Second,
		StartInterval: time.Duration(hc.StartInterval) * time.Second,
		Retries:       hc.Retries,
	}
}

// mergeEditMountsInternal builds the desired mount set for an edit. The edit
// form only carries the reduced {type, source, target, readOnly} view, so a
// requested mount that matches an existing one by type and target keeps the
// full original spec (bind/volume/tmpfs options, consistency, ...) with the
// source and read-only flag applied from the request; unmatched entries are
// new mounts built from the request.
func mergeEditMountsInternal(existing []mount.Mount, requested []containertypes.MountCreate) []mount.Mount {
	out := make([]mount.Mount, 0, len(requested))
	for _, req := range requested {
		merged := mount.Mount{
			Type:     mount.Type(req.Type),
			Source:   req.Source,
			Target:   req.Target,
			ReadOnly: req.ReadOnly,
		}
		for _, prior := range existing {
			if string(prior.Type) == req.Type && prior.Target == req.Target {
				merged = prior
				merged.Source = req.Source
				merged.ReadOnly = req.ReadOnly
				break
			}
		}
		out = append(out, merged)
	}
	return out
}

func mountsFromCreateInternal(mounts []containertypes.MountCreate) []mount.Mount {
	out := make([]mount.Mount, 0, len(mounts))
	for _, m := range mounts {
		out = append(out, mount.Mount{
			Type:     mount.Type(m.Type),
			Source:   m.Source,
			Target:   m.Target,
			ReadOnly: m.ReadOnly,
		})
	}
	return out
}

func portMapFromCreateInternal(bindings map[string][]containertypes.PortBindingCreate) (network.PortMap, error) {
	out := network.PortMap{}
	for portSpec, bindingList := range bindings {
		port, err := parsePortSpec(portSpec)
		if err != nil {
			return nil, errors.WrapIff(err, "invalid port %s", portSpec)
		}
		mapped := make([]network.PortBinding, 0, len(bindingList))
		for _, binding := range bindingList {
			pb := network.PortBinding{HostPort: binding.HostPort}
			if hostIP := strings.TrimSpace(binding.HostIP); hostIP != "" {
				parsedIP, err := netip.ParseAddr(hostIP)
				if err != nil {
					return nil, errors.WrapIff(err, "invalid host IP %s", hostIP)
				}
				pb.HostIP = parsedIP
			}
			mapped = append(mapped, pb)
		}
		out[port] = mapped
	}
	return out, nil
}

func endpointIPAMFromCreateInternal(ep containertypes.EndpointSettingsCreate) (*network.EndpointIPAMConfig, error) {
	ipv4 := strings.TrimSpace(ep.IPv4Address)
	ipv6 := strings.TrimSpace(ep.IPv6Address)
	if ipv4 == "" && ipv6 == "" {
		return nil, nil
	}

	cfg := &network.EndpointIPAMConfig{}
	if ipv4 != "" {
		addr, err := netip.ParseAddr(ipv4)
		if err != nil {
			return nil, errors.WrapIff(err, "invalid IPv4 address %s", ipv4)
		}
		cfg.IPv4Address = addr
	}
	if ipv6 != "" {
		addr, err := netip.ParseAddr(ipv6)
		if err != nil {
			return nil, errors.WrapIff(err, "invalid IPv6 address %s", ipv6)
		}
		cfg.IPv6Address = addr
	}
	return cfg, nil
}

// applyEditToContainerConfigInternal overwrites the non-nil form-owned Config
// sections of an edit request onto cfg. Settings the form does not own are
// left untouched. When port bindings are replaced, ExposedPorts is recomputed
// as the new binding keys plus any previously-exposed-but-unbound ports (image
// EXPOSE entries survive).
func applyEditToContainerConfigInternal(cfg *container.Config, previousBindings network.PortMap, req containertypes.Edit) error {
	if req.Image != nil && strings.TrimSpace(*req.Image) != "" {
		cfg.Image = strings.TrimSpace(*req.Image)
	}
	if req.WorkingDir != nil {
		cfg.WorkingDir = *req.WorkingDir
	}
	if req.User != nil {
		cfg.User = *req.User
	}
	if req.Command != nil {
		cfg.Cmd = append([]string{}, (*req.Command)...)
	}
	if req.Entrypoint != nil {
		cfg.Entrypoint = append([]string{}, (*req.Entrypoint)...)
	}
	if req.Environment != nil {
		cfg.Env = append([]string{}, (*req.Environment)...)
	}
	if req.Labels != nil {
		cfg.Labels = maps.Clone(*req.Labels)
	}

	switch {
	case req.ClearHealthcheck:
		cfg.Healthcheck = nil
	case req.Healthcheck != nil:
		cfg.Healthcheck = healthConfigFromCreateInternal(req.Healthcheck)
	}

	if req.HostConfig != nil && req.HostConfig.PortBindings != nil {
		newBindings, err := portMapFromCreateInternal(*req.HostConfig.PortBindings)
		if err != nil {
			return err
		}
		exposed := network.PortSet{}
		for port := range newBindings {
			exposed[port] = struct{}{}
		}
		for port := range cfg.ExposedPorts {
			if _, wasBound := previousBindings[port]; !wasBound {
				exposed[port] = struct{}{}
			}
		}
		cfg.ExposedPorts = exposed
	}

	return nil
}

// applyEditToHostConfigInternal overwrites the non-nil form-owned HostConfig
// sections of an edit request onto hc. Everything else (sysctls, ulimits, log
// drivers, devices, tmpfs, DNS, ...) carries over unchanged.
func applyEditToHostConfigInternal(hc *container.HostConfig, req containertypes.Edit) error {
	edit := req.HostConfig
	if edit == nil {
		return nil
	}

	if edit.Binds != nil {
		hc.Binds = append([]string{}, (*edit.Binds)...)
	}
	if edit.Mounts != nil {
		hc.Mounts = mergeEditMountsInternal(hc.Mounts, *edit.Mounts)
	}
	if edit.PortBindings != nil {
		newBindings, err := portMapFromCreateInternal(*edit.PortBindings)
		if err != nil {
			return err
		}
		hc.PortBindings = newBindings
	}
	if edit.RestartPolicy != nil {
		hc.RestartPolicy = container.RestartPolicy{
			Name:              container.RestartPolicyMode(edit.RestartPolicy.Name),
			MaximumRetryCount: edit.RestartPolicy.MaximumRetryCount,
		}
	}
	if edit.Privileged != nil {
		hc.Privileged = *edit.Privileged
	}
	if edit.CapAdd != nil {
		hc.CapAdd = append([]string{}, (*edit.CapAdd)...)
	}
	if edit.CapDrop != nil {
		hc.CapDrop = append([]string{}, (*edit.CapDrop)...)
	}
	if edit.AutoRemove != nil {
		hc.AutoRemove = *edit.AutoRemove
	}
	if edit.ReadonlyRootfs != nil {
		hc.ReadonlyRootfs = *edit.ReadonlyRootfs
	}
	if edit.Memory != nil {
		hc.Memory = *edit.Memory
	}
	if edit.MemorySwap != nil {
		hc.MemorySwap = *edit.MemorySwap
	}
	if edit.NanoCPUs != nil {
		hc.NanoCPUs = *edit.NanoCPUs
	}
	if edit.CPUShares != nil {
		hc.CPUShares = *edit.CPUShares
	}

	return nil
}

// buildEditNetworkingConfigInternal builds the networking config for an edit
// recreate. A nil request preserves the existing attachments (same behavior as
// redeploy); otherwise the request is the full desired endpoint set, keeping
// the sanitized settings of endpoints that stay attached. Containers using
// host/none/container: network modes never get endpoint edits.
func buildEditNetworkingConfigInternal(containerInspect container.InspectResponse, req *containertypes.NetworkingConfigCreate, apiVersion string) (*network.NetworkingConfig, error) {
	if containerInspect.HostConfig != nil {
		mode := containerInspect.HostConfig.NetworkMode
		if mode.IsHost() || mode.IsNone() || mode.IsContainer() {
			return nil, nil
		}
	}

	if req == nil {
		return buildCleanNetworkingConfigInternal(containerInspect, apiVersion), nil
	}

	var existing map[string]*network.EndpointSettings
	if containerInspect.NetworkSettings != nil {
		existing = libarcane.SanitizeContainerCreateEndpointSettingsForDockerAPI(containerInspect.NetworkSettings.Networks, apiVersion)
	}

	endpoints := make(map[string]*network.EndpointSettings, len(req.EndpointsConfig))
	for name, epReq := range req.EndpointsConfig {
		endpoint := &network.EndpointSettings{}
		if prior := existing[name]; prior != nil {
			priorCopy := *prior
			priorCopy.IPAMConfig = nil
			endpoint = &priorCopy
		}
		endpoint.Aliases = append([]string{}, epReq.Aliases...)
		ipam, err := endpointIPAMFromCreateInternal(epReq)
		if err != nil {
			return nil, err
		}
		endpoint.IPAMConfig = ipam
		endpoints[name] = endpoint
	}

	if len(endpoints) == 0 {
		return nil, nil
	}

	return &network.NetworkingConfig{EndpointsConfig: endpoints}, nil
}

// GetContainerEditConfig returns the purpose-built DTO backing the container
// edit form.
func (s *ContainerService) GetContainerEditConfig(ctx context.Context, containerID string) (containertypes.EditConfig, error) {
	dockerClient, err := s.dockerService.GetClient(ctx)
	if err != nil {
		return containertypes.EditConfig{}, errors.WrapIf(err, "failed to connect to Docker")
	}

	containerJSON, err := libarcane.ContainerInspectWithCompatibility(ctx, dockerClient, containerID, client.ContainerInspectOptions{})
	if err != nil {
		return containertypes.EditConfig{}, errors.WrapIf(err, "failed to inspect container")
	}

	containerInfo := containerJSON.Container
	var containerLabels map[string]string
	if containerInfo.Config != nil {
		containerLabels = containerInfo.Config.Labels
	}

	isCompose := dockerutils.ComposeProjectLabel(containerLabels) != ""
	currentContainerID, currentContainerErr := cgroup.CurrentContainerID()
	editDisabled := labels.ShouldDisableArcaneServerRedeploy(containerLabels, containerInfo.ID, currentContainerID, currentContainerErr)

	return containertypes.NewEditConfigFromInspect(&containerInfo, isCompose, editDisabled), nil
}

// EditContainer applies a user-supplied config diff onto the container's
// inspected config and recreates it. Sections not owned by the edit form are
// preserved from the existing container.
func (s *ContainerService) EditContainer(ctx context.Context, containerID string, req containertypes.Edit, user common.User) (string, error) {
	dockerClient, err := s.dockerService.GetClient(ctx)
	if err != nil {
		s.eventService.LogErrorEvent(ctx, event.EventTypeContainerError, "container", containerID, "", user.ID, user.Username, "0", err, database.JSON{
			"action": "edit",
			"step":   "get_client",
		})
		return "", errors.WrapIf(err, "failed to connect to Docker")
	}

	containerJSON, err := libarcane.ContainerInspectWithCompatibility(ctx, dockerClient, containerID, client.ContainerInspectOptions{})
	if err != nil {
		s.eventService.LogErrorEvent(ctx, event.EventTypeContainerError, "container", containerID, "", user.ID, user.Username, "0", err, database.JSON{
			"action": "edit",
			"step":   "inspect",
		})
		return "", errors.WrapIf(err, "failed to inspect container")
	}

	containerInfo := containerJSON.Container
	if containerInfo.Config == nil {
		return "", errors.New("container config is nil")
	}

	containerName := strings.TrimPrefix(containerInfo.Name, "/")

	if project := dockerutils.ComposeProjectLabel(containerInfo.Config.Labels); project != "" {
		return "", errors.WrapIff(common.ErrContainerComposeManaged, "compose project %s", project)
	}

	currentContainerID, currentContainerErr := cgroup.CurrentContainerID()
	if labels.ShouldDisableArcaneServerRedeploy(containerInfo.Config.Labels, containerInfo.ID, currentContainerID, currentContainerErr) {
		err = errors.New("arcane cannot edit itself; use the system upgrade flow (Settings -> Updates) instead")
		s.eventService.LogErrorEvent(ctx, event.EventTypeContainerError, "container", containerID, containerName, user.ID, user.Username, "0", err, database.JSON{
			"action": "edit",
			"step":   "self_edit_blocked",
		})
		return "", err
	}

	newName := containerName
	if req.Name != nil {
		if trimmed := strings.TrimPrefix(strings.TrimSpace(*req.Name), "/"); trimmed != "" {
			newName = trimmed
		}
	}
	if newName != containerName {
		if _, inspectErr := libarcane.ContainerInspectWithCompatibility(ctx, dockerClient, newName, client.ContainerInspectOptions{}); inspectErr == nil {
			return "", errors.WrapIff(common.ErrContainerNameTaken, "container name %s", newName)
		}
	}

	apiVersion := libarcane.DetectDockerAPIVersion(ctx, dockerClient)

	newConfig := *containerInfo.Config
	var newHostConfig container.HostConfig
	if containerInfo.HostConfig != nil {
		newHostConfig = *containerInfo.HostConfig
	}

	if err := applyEditToContainerConfigInternal(&newConfig, newHostConfig.PortBindings, req); err != nil {
		return "", common.Classify(common.ErrValidation, err)
	}
	if err := applyEditToHostConfigInternal(&newHostConfig, req); err != nil {
		return "", common.Classify(common.ErrValidation, err)
	}
	networkingConfig, err := buildEditNetworkingConfigInternal(containerInfo, req.NetworkingConfig, apiVersion)
	if err != nil {
		return "", common.Classify(common.ErrValidation, err)
	}

	// Unlike redeploy, edit only pulls when the effective image is missing
	// locally so an unrelated config change never surprise-upgrades the image.
	imageName := newConfig.Image
	if imageName != "" {
		if _, inspectErr := dockerClient.ImageInspect(ctx, imageName); inspectErr != nil {
			if err := s.pullRedeployImageInternal(ctx, dockerClient, imageName, containerID, containerName, "edit", req.Credentials, user); err != nil {
				return "", err
			}
		}
	}

	return s.recreateContainerInternal(ctx, dockerClient, containerInfo, "edit", newName, &newConfig, &newHostConfig, networkingConfig, apiVersion, event.EventTypeContainerUpdate, user)
}

func (s *ContainerService) GetContainerByReference(ctx context.Context, ref string) (*container.InspectResponse, error) {
	dockerClient, err := s.dockerService.GetClient(ctx)
	if err != nil {
		return nil, errors.WrapIf(err, "failed to connect to Docker")
	}

	containerInspect, err := libarcane.ContainerInspectWithCompatibility(ctx, dockerClient, ref, client.ContainerInspectOptions{})
	if err != nil {
		return nil, errors.WrapIf(err, "container not found")
	}

	return new(containerInspect.Container), nil
}

func (s *ContainerService) GetContainerByID(ctx context.Context, id string) (*container.InspectResponse, error) {
	return s.GetContainerByReference(ctx, id)
}

func (s *ContainerService) GetContainerDetails(ctx context.Context, id string) (containertypes.Details, error) {
	containerInspect, err := s.GetContainerByID(ctx, id)
	if err != nil {
		return containertypes.Details{}, err
	}

	details := containertypes.NewDetails(containerInspect)
	currentContainerID, currentContainerErr := cgroup.CurrentContainerID()
	details.RedeployDisabled = labels.ShouldDisableArcaneServerRedeploy(details.Labels, details.ID, currentContainerID, currentContainerErr)
	s.applyContainerDetailsIconInternal(ctx, &details)

	return details, nil
}

// GetContainerNameByReference resolves a container's clean name from a Docker ID or name.
func (s *ContainerService) GetContainerNameByReference(ctx context.Context, ref string) (string, error) {
	info, err := s.GetContainerByReference(ctx, ref)
	if err != nil {
		return "", err
	}
	return strings.TrimPrefix(info.Name, "/"), nil
}

// GetContainerNameByID resolves a container's clean name from its Docker ID.
func (s *ContainerService) GetContainerNameByID(ctx context.Context, id string) (string, error) {
	return s.GetContainerNameByReference(ctx, id)
}

func (s *ContainerService) DeleteContainer(ctx context.Context, containerID string, force bool, removeVolumes bool, user common.User) error {
	dockerClient, err := s.dockerService.GetClient(ctx)
	if err != nil {
		s.eventService.LogErrorEvent(ctx, event.EventTypeContainerError, "container", containerID, "", user.ID, user.Username, "0", err, database.JSON{"action": "delete", "force": force, "removeVolumes": removeVolumes})
		return errors.WrapIf(err, "failed to connect to Docker")
	}

	// Get container mounts before deletion if we need to remove volumes
	var volumesToRemove []string
	if removeVolumes {
		containerJSON, inspectErr := libarcane.ContainerInspectWithCompatibility(ctx, dockerClient, containerID, client.ContainerInspectOptions{})
		if inspectErr == nil {
			for _, mount := range containerJSON.Container.Mounts {
				// Only collect named volumes (not bind mounts or tmpfs)
				if mount.Type == "volume" && mount.Name != "" {
					volumesToRemove = append(volumesToRemove, mount.Name)
				}
			}
		}
	}

	_, err = dockerClient.ContainerRemove(ctx, containerID, client.ContainerRemoveOptions{
		Force:         force,
		RemoveVolumes: removeVolumes,
		RemoveLinks:   false,
	})
	if err != nil {
		s.eventService.LogErrorEvent(ctx, event.EventTypeContainerError, "container", containerID, "", user.ID, user.Username, "0", err, database.JSON{"action": "delete", "force": force, "removeVolumes": removeVolumes})
		return errors.WrapIf(err, "failed to delete container")
	}

	// Remove named volumes if requested
	if removeVolumes && len(volumesToRemove) > 0 {
		for _, volumeName := range volumesToRemove {
			if _, removeErr := dockerClient.VolumeRemove(ctx, volumeName, client.VolumeRemoveOptions{Force: false}); removeErr != nil {
				// Log but don't fail if volume removal fails (might be in use by another container)
				s.eventService.LogErrorEvent(ctx, event.EventTypeVolumeError, "volume", volumeName, "", user.ID, user.Username, "0", removeErr, database.JSON{"action": "delete", "container": containerID})
			}
		}
	}

	metadata := database.JSON{
		"action":      "delete",
		"containerId": containerID,
	}

	err = s.eventService.LogContainerEvent(ctx, event.EventTypeContainerDelete, containerID, "name", user.ID, user.Username, "0", metadata)
	if err != nil {
		return errors.WrapIf(err, "failed to log action")
	}

	return nil
}

func (s *ContainerService) CreateContainer(ctx context.Context, config *container.Config, hostConfig *container.HostConfig, networkingConfig *network.NetworkingConfig, containerName string, user common.User, credentials []containerregistry.Credential) (*container.InspectResponse, error) {
	dockerClient, err := s.dockerService.GetClient(ctx)
	if err != nil {
		s.eventService.LogErrorEvent(ctx, event.EventTypeContainerError, "container", "", containerName, user.ID, user.Username, "0", err, database.JSON{"action": "create", "image": config.Image})
		return nil, errors.WrapIf(err, "failed to connect to Docker")
	}

	_, err = dockerClient.ImageInspect(ctx, config.Image)
	if err != nil {
		// Image not found locally, need to pull it
		pullOptions, authErr := s.imageService.PullOptionsWithAuth(ctx, config.Image, credentials)
		if authErr != nil {
			slog.WarnContext(ctx, "Failed to get registry authentication for container image; proceeding without auth",
				"image", config.Image,
				"error", authErr.Error())
			pullOptions = client.ImagePullOptions{}
		}

		settings := s.settingsService.GetSettingsConfig()
		pullCtx, pullCancel := context.WithTimeout(ctx, timeouts.GetDuration(settings.DockerImagePullTimeout.AsInt(), timeouts.DefaultDockerImagePull))
		defer pullCancel()

		reader, pullErr := dockerClient.ImagePull(pullCtx, config.Image, pullOptions)
		if pullErr != nil {
			if errors.Is(pullCtx.Err(), context.DeadlineExceeded) {
				s.eventService.LogErrorEvent(ctx, event.EventTypeContainerError, "container", "", containerName, user.ID, user.Username, "0", pullErr, database.JSON{"action": "create", "image": config.Image, "step": "pull_image_timeout"})
				return nil, errors.Errorf("image pull timed out for %s (increase DOCKER_IMAGE_PULL_TIMEOUT or setting)", config.Image)
			}
			s.eventService.LogErrorEvent(ctx, event.EventTypeContainerError, "container", "", containerName, user.ID, user.Username, "0", pullErr, database.JSON{"action": "create", "image": config.Image, "step": "pull_image"})
			return nil, errors.WrapIff(pullErr, "failed to pull image %s", config.Image)
		}
		defer func() { _ = reader.Close() }()

		progressWriter, _ := ctx.Value(dockerutils.ProgressWriterKey{}).(io.Writer)
		logWriter := dockerutils.NewLogLineWriter(progressWriter)
		streamErr := dockerutils.RenderJSONMessageStream(reader, logWriter)
		_ = logWriter.Close()
		if streamErr != nil {
			s.eventService.LogErrorEvent(ctx, event.EventTypeContainerError, "container", "", containerName, user.ID, user.Username, "0", streamErr, database.JSON{"action": "create", "image": config.Image, "step": "complete_pull"})
			return nil, errors.WrapIf(streamErr, "failed to complete image pull")
		}
	}

	resp, err := libarcane.ContainerCreateWithCompatibility(ctx, dockerClient, client.ContainerCreateOptions{
		Config:           config,
		HostConfig:       hostConfig,
		NetworkingConfig: networkingConfig,
		Name:             containerName,
	})
	if err != nil {
		s.eventService.LogErrorEvent(ctx, event.EventTypeContainerError, "container", "", containerName, user.ID, user.Username, "0", err, database.JSON{"action": "create", "image": config.Image, "step": "create"})
		return nil, errors.WrapIf(err, "failed to create container")
	}

	metadata := database.JSON{
		"action":      "create",
		"containerId": resp.ID,
	}

	if logErr := s.eventService.LogContainerEvent(ctx, event.EventTypeContainerCreate, resp.ID, "name", user.ID, user.Username, "0", metadata); logErr != nil {
		slog.WarnContext(ctx, "could not log container stop action", "error", logErr)
	}

	if _, err := dockerClient.ContainerStart(ctx, resp.ID, client.ContainerStartOptions{}); err != nil {
		_, _ = dockerClient.ContainerRemove(ctx, resp.ID, client.ContainerRemoveOptions{Force: true})
		s.eventService.LogErrorEvent(ctx, event.EventTypeContainerError, "container", resp.ID, containerName, user.ID, user.Username, "0", err, database.JSON{"action": "create", "image": config.Image, "step": "start"})
		return nil, errors.WrapIf(err, "failed to start container")
	}

	containerJSON, err := libarcane.ContainerInspectWithCompatibility(ctx, dockerClient, resp.ID, client.ContainerInspectOptions{})
	if err != nil {
		s.eventService.LogErrorEvent(ctx, event.EventTypeContainerError, "container", resp.ID, containerName, user.ID, user.Username, "0", err, database.JSON{"action": "create", "image": config.Image, "step": "inspect"})
		return nil, errors.WrapIf(err, "failed to inspect created container")
	}

	return new(containerJSON.Container), nil
}

func (s *ContainerService) StreamStats(ctx context.Context, containerID string, statsChan chan<- any) error {
	dockerClient, err := s.dockerService.GetClient(ctx)
	if err != nil {
		return errors.WrapIf(err, "failed to connect to Docker")
	}

	stats, err := dockerClient.ContainerStats(ctx, containerID, client.ContainerStatsOptions{Stream: true})
	if err != nil {
		return errors.WrapIf(err, "failed to start stats stream")
	}
	defer func() { _ = stats.Body.Close() }()

	decoder := jsontext.NewDecoder(stats.Body)
	historySent := false

	for {
		if err := ctx.Err(); err != nil {
			return err
		}

		var statsData container.StatsResponse
		if err := json.UnmarshalDecode(decoder, &statsData); err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			if errors.Is(err, io.EOF) {
				return nil
			}
			return errors.WrapIf(err, "failed to decode stats")
		}

		recordedAt := statsData.Read
		if recordedAt.IsZero() {
			recordedAt = time.Now()
		}

		payload := containerstats.StatsStreamPayload{
			StatsResponse:        statsData,
			CurrentHistorySample: containerstats.BuildSample(statsData),
		}
		payload.StatsHistory = s.statsHistory.Record(
			containerID,
			payload.CurrentHistorySample,
			!historySent,
			recordedAt,
		)
		historySent = true

		select {
		case statsChan <- payload:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
}

func (s *ContainerService) StreamLogs(ctx context.Context, containerID string, logsChan chan<- string, follow bool, tail, since string, timestamps bool) error {
	dockerClient, err := s.dockerService.GetClient(ctx)
	if err != nil {
		return errors.WrapIf(err, "failed to connect to Docker")
	}

	containerInspect, err := libarcane.ContainerInspectWithCompatibility(ctx, dockerClient, containerID, client.ContainerInspectOptions{})
	if err != nil {
		return errors.WrapIf(err, "failed to inspect container for logs")
	}

	options := client.ContainerLogsOptions{
		ShowStdout: true,
		ShowStderr: true,
		Follow:     follow,
		Tail:       tail,
		Since:      since,
		Timestamps: timestamps,
	}

	logs, err := dockerClient.ContainerLogs(ctx, containerID, options)
	if err != nil {
		return errors.WrapIf(err, "failed to get container logs")
	}
	defer func() { _ = logs.Close() }()

	isTTY := containerInspect.Container.Config != nil && containerInspect.Container.Config.Tty
	return dockerutils.StreamContainerLogs(ctx, logs, logsChan, follow, isTTY)
}

func (s *ContainerService) ListContainersPaginated(
	ctx context.Context,
	params pagination.QueryParams,
	includeAll bool,
	includeInternal bool,
	groupBy string,
) (ContainerListResult, error) {
	dockerContainers, err := s.dockerService.ListContainers(ctx)
	if err != nil {
		return ContainerListResult{}, err
	}
	if !includeAll {
		running := make([]container.Summary, 0, len(dockerContainers))
		for _, dc := range dockerContainers {
			switch dc.State {
			case container.StateRunning, container.StatePaused, container.StateRestarting:
				running = append(running, dc)
			}
		}
		dockerContainers = running
	}

	dockerContainers = FilterInternalContainers(dockerContainers, includeInternal)
	imageIDs := CollectImageIDs(dockerContainers)
	updateInfoMap := s.getUpdateInfoMapInternal(ctx, imageIDs)
	currentContainerID, currentContainerErr := cgroup.CurrentContainerID()
	items := s.BuildSummaries(dockerContainers, updateInfoMap, currentContainerID, currentContainerErr)

	config := s.buildContainerPaginationConfig()
	counts := s.CalculateStatusCounts(items)

	if groupBy == containerGroupByProject {
		ungroupedParams := params
		ungroupedParams.Start = 0
		ungroupedParams.Limit = -1

		result := config.SearchOrderAndPaginate(items, ungroupedParams)
		groups, paginationResp := paginateContainerProjectGroupsInternal(result, params)

		// Icons must be resolved before flattening: groups hold value copies,
		// so the flattened items only carry icons applied to the groups first.
		metadataByProject := map[string]projects.ArcaneComposeMetadata{}
		for gi := range groups {
			s.ApplySummaryIcons(ctx, groups[gi].Items, metadataByProject)
		}

		return ContainerListResult{
			Items:      flattenContainerProjectGroupsInternal(groups),
			Groups:     groups,
			Pagination: paginationResp,
			Counts:     counts,
		}, nil
	}

	result := config.SearchOrderAndPaginate(items, params)
	s.ApplySummaryIcons(ctx, result.Items, nil)
	paginationResp := pagination.BuildResponse(result.TotalCount, result.TotalAvailable, params)

	return ContainerListResult{
		Items:      result.Items,
		Pagination: paginationResp,
		Counts:     counts,
	}, nil
}

func paginateContainerProjectGroupsInternal(
	result pagination.FilterResult[containertypes.Summary],
	params pagination.QueryParams,
) ([]containertypes.SummaryGroup, pagination.Response) {
	groups := groupContainersByProjectInternal(result.Items)
	totalCount := len(result.Items)

	if params.Limit <= 0 {
		return groups, pagination.BuildResponse(int64(totalCount), result.TotalAvailable, params)
	}

	requestedPage := max((params.Start/params.Limit)+1, 1)

	// Pages are contiguous runs of whole groups: a group is never split, so the
	// group that crosses the limit finishes its page. One walk over group sizes
	// finds the requested page's group range without materializing other pages.
	totalPages := 0
	pageStart, currentCount := 0, 0
	selStart, selEnd := 0, 0
	lastStart, lastEnd := 0, 0

	closePage := func(end int) {
		totalPages++
		if totalPages == requestedPage {
			selStart, selEnd = pageStart, end
		}
		lastStart, lastEnd = pageStart, end
		pageStart, currentCount = end, 0
	}

	for i := range groups {
		currentCount += len(groups[i].Items)
		if currentCount >= params.Limit {
			closePage(i + 1)
		}
	}
	if pageStart < len(groups) || totalPages == 0 {
		closePage(len(groups))
	}

	if requestedPage > totalPages {
		requestedPage = totalPages
		selStart, selEnd = lastStart, lastEnd
	}

	return groups[selStart:selEnd], pagination.Response{
		TotalPages:      int64(totalPages),
		TotalItems:      int64(totalCount),
		CurrentPage:     requestedPage,
		ItemsPerPage:    params.Limit,
		GrandTotalItems: result.TotalAvailable,
	}
}

func groupContainersByProjectInternal(items []containertypes.Summary) []containertypes.SummaryGroup {
	groups := make([]containertypes.SummaryGroup, 0)
	groupIndexes := make(map[string]int)

	for _, item := range items {
		groupName := getContainerProjectNameInternal(item)
		groupIndex, exists := groupIndexes[groupName]
		if !exists {
			groupIndex = len(groups)
			groupIndexes[groupName] = groupIndex
			groups = append(groups, containertypes.SummaryGroup{GroupName: groupName})
		}

		groups[groupIndex].Items = append(groups[groupIndex].Items, item)
	}

	return groups
}

func flattenContainerProjectGroupsInternal(groups []containertypes.SummaryGroup) []containertypes.Summary {
	flattened := make([]containertypes.Summary, 0)
	for _, group := range groups {
		flattened = append(flattened, group.Items...)
	}

	return flattened
}

func getContainerProjectNameInternal(container containertypes.Summary) string {
	if container.Labels == nil {
		return containerNoProjectGroup
	}

	projectName := dockerutils.ComposeProjectLabel(container.Labels)
	if projectName == "" {
		return containerNoProjectGroup
	}

	return projectName
}

func FilterInternalContainers(containers []container.Summary, includeInternal bool) []container.Summary {
	if includeInternal {
		return containers
	}

	filtered := make([]container.Summary, 0, len(containers))
	for _, dc := range containers {
		if libarcane.IsInternalContainer(dc.Labels) {
			continue
		}
		filtered = append(filtered, dc)
	}
	return filtered
}

func CollectImageIDs(containers []container.Summary) []string {
	imageIDSet := make(map[string]struct{}, len(containers))
	for _, dc := range containers {
		if dc.ImageID != "" {
			imageIDSet[dc.ImageID] = struct{}{}
		}
	}

	imageIDs := make([]string, 0, len(imageIDSet))
	for id := range imageIDSet {
		imageIDs = append(imageIDs, id)
	}
	return imageIDs
}

func (s *ContainerService) getUpdateInfoMapInternal(ctx context.Context, imageIDs []string) map[string]*imagetypes.UpdateInfo {
	if s.imageService == nil || len(imageIDs) == 0 {
		return make(map[string]*imagetypes.UpdateInfo)
	}

	updateInfoMap, err := s.imageService.GetUpdateInfoByImageIDs(ctx, imageIDs)
	if err != nil {
		slog.WarnContext(ctx, "Failed to fetch image update info for containers", "error", err)
		return make(map[string]*imagetypes.UpdateInfo)
	}
	return updateInfoMap
}

func (s *ContainerService) BuildSummaries(containers []container.Summary, updateInfoMap map[string]*imagetypes.UpdateInfo, currentContainerID string, currentContainerErr error) []containertypes.Summary {
	items := make([]containertypes.Summary, 0, len(containers))
	for _, dc := range containers {
		summary := containertypes.NewSummary(dc)
		if info, exists := updateInfoMap[dc.ImageID]; exists {
			summary.UpdateInfo = info
		}
		summary.RedeployDisabled = labels.ShouldDisableArcaneServerRedeploy(summary.Labels, summary.ID, currentContainerID, currentContainerErr)
		items = append(items, summary)
	}
	return items
}

// ApplySummaryIcons resolves icons for a page of summaries.
// Icon resolution is deferred until after pagination so the cost is bounded by
// page size rather than the full container list.
func (s *ContainerService) ApplySummaryIcons(ctx context.Context, summaries []containertypes.Summary, metadataByProject map[string]projects.ArcaneComposeMetadata) {
	if metadataByProject == nil {
		metadataByProject = map[string]projects.ArcaneComposeMetadata{}
	}
	for i := range summaries {
		s.applyContainerSummaryIconInternal(ctx, &summaries[i], metadataByProject)
	}
}

func (s *ContainerService) applyContainerSummaryIconInternal(ctx context.Context, summary *containertypes.Summary, metadataByProject map[string]projects.ArcaneComposeMetadata) {
	if summary == nil {
		return
	}
	resolvedIcon := s.resolveContainerIconInternal(ctx, summary.Labels, metadataByProject)
	summary.IconLightURL = resolvedIcon.IconLightURL
	summary.IconDarkURL = resolvedIcon.IconDarkURL
}

func (s *ContainerService) applyContainerDetailsIconInternal(ctx context.Context, details *containertypes.Details) {
	if details == nil {
		return
	}
	resolvedIcon := s.resolveContainerIconInternal(ctx, details.Labels, nil)
	details.IconLightURL = resolvedIcon.IconLightURL
	details.IconDarkURL = resolvedIcon.IconDarkURL
}

func (s *ContainerService) resolveContainerIconInternal(ctx context.Context, labels map[string]string, metadataByProject map[string]projects.ArcaneComposeMetadata) iconcatalog.ResolvedIconSet {
	explicitIcon := projects.FindArcaneIconSet(labels)
	if !explicitIcon.IsEmpty() {
		return iconcatalog.Resolve(project.IconCatalogForContext(ctx), explicitIcon)
	}

	projectName := dockerutils.ComposeProjectLabel(labels)
	if projectName == "" || s == nil || s.projectService == nil {
		return iconcatalog.Resolve(project.IconCatalogForContext(ctx), explicitIcon)
	}

	meta := s.getCachedProjectIconMetadataInternal(ctx, projectName, metadataByProject)

	serviceName := dockerutils.ComposeServiceLabel(labels)
	return iconcatalog.Resolve(project.IconCatalogForContext(ctx), iconcatalog.FirstNonEmpty(
		explicitIcon,
		meta.ServiceIconSets[serviceName],
		meta.ProjectIcon,
	))
}

func (s *ContainerService) getCachedProjectIconMetadataInternal(ctx context.Context, projectName string, metadataByProject map[string]projects.ArcaneComposeMetadata) projects.ArcaneComposeMetadata {
	if metadataByProject != nil {
		if meta, ok := metadataByProject[projectName]; ok {
			return meta
		}
	}

	if s.iconMetaCache != nil {
		if meta, ok, _ := s.iconMetaCache.Get(projectName); ok {
			if metadataByProject != nil {
				metadataByProject[projectName] = meta
			}
			return meta
		}
	}

	meta := projects.ArcaneComposeMetadata{ServiceIconSets: map[string]projects.IconSet{}}
	proj, err := s.projectService.GetProjectByComposeName(ctx, projectName)
	if err == nil && proj != nil {
		meta = s.projectService.ProjectMetadata(ctx, *proj, nil)
	}
	if s.iconMetaCache != nil {
		s.iconMetaCache.Set(projectName, meta)
	}
	if metadataByProject != nil {
		metadataByProject[projectName] = meta
	}
	return meta
}

func (s *ContainerService) buildContainerPaginationConfig() pagination.Config[containertypes.Summary] {
	return pagination.Config[containertypes.Summary]{
		SearchAccessors: []pagination.SearchAccessor[containertypes.Summary]{
			func(c containertypes.Summary) (string, error) {
				if len(c.Names) > 0 {
					return c.Names[0], nil
				}
				return "", nil
			},
			func(c containertypes.Summary) (string, error) { return c.Image, nil },
			func(c containertypes.Summary) (string, error) { return c.State, nil },
			func(c containertypes.Summary) (string, error) { return c.Status, nil },
		},
		SortBindings:    s.buildContainerSortBindings(),
		FilterAccessors: s.buildContainerFilterAccessors(),
	}
}

func (s *ContainerService) buildContainerSortBindings() []pagination.SortBinding[containertypes.Summary] {
	return []pagination.SortBinding[containertypes.Summary]{
		{
			Key: "name",
			Fn: func(a, b containertypes.Summary) int {
				nameA, nameB := "", ""
				if len(a.Names) > 0 {
					nameA = a.Names[0]
				}
				if len(b.Names) > 0 {
					nameB = b.Names[0]
				}
				return strings.Compare(nameA, nameB)
			},
		},
		{
			Key: "image",
			Fn: func(a, b containertypes.Summary) int {
				return strings.Compare(a.Image, b.Image)
			},
		},
		{
			Key: "state",
			Fn: func(a, b containertypes.Summary) int {
				return strings.Compare(a.State, b.State)
			},
		},
		{
			Key: "status",
			Fn: func(a, b containertypes.Summary) int {
				return strings.Compare(a.Status, b.Status)
			},
		},
		{
			Key:    "ports",
			Fn:     compareContainerPortsForSortInternal,
			DescFn: compareContainerPortsForSortDescInternal,
		},
		{
			Key: "created",
			Fn: func(a, b containertypes.Summary) int {
				if a.Created < b.Created {
					return -1
				}
				if a.Created > b.Created {
					return 1
				}
				return 0
			},
		},
	}
}

func compareContainerPortsForSortInternal(a, b containertypes.Summary) int {
	hasPortsA, portA := lowestContainerPortSortValueInternal(a.Ports)
	hasPortsB, portB := lowestContainerPortSortValueInternal(b.Ports)

	switch {
	case !hasPortsA && !hasPortsB:
		return compareContainerNamesForSortInternal(a, b)
	case !hasPortsA:
		return 1
	case !hasPortsB:
		return -1
	case portA < portB:
		return -1
	case portA > portB:
		return 1
	default:
		return compareContainerNamesForSortInternal(a, b)
	}
}

func compareContainerPortsForSortDescInternal(a, b containertypes.Summary) int {
	hasPortsA, portA := lowestContainerPortSortValueInternal(a.Ports)
	hasPortsB, portB := lowestContainerPortSortValueInternal(b.Ports)

	switch {
	case !hasPortsA && !hasPortsB:
		return compareContainerNamesForSortInternal(a, b)
	case !hasPortsA:
		return 1
	case !hasPortsB:
		return -1
	case portA > portB:
		return -1
	case portA < portB:
		return 1
	default:
		return compareContainerNamesForSortInternal(a, b)
	}
}

func lowestContainerPortSortValueInternal(ports []containertypes.Port) (bool, int) {
	if len(ports) == 0 {
		return false, 0
	}

	lowestPublished := 0
	lowestPrivate := 0
	for _, port := range ports {
		if port.PublicPort > 0 && (lowestPublished == 0 || port.PublicPort < lowestPublished) {
			lowestPublished = port.PublicPort
		}
		if port.PrivatePort > 0 && (lowestPrivate == 0 || port.PrivatePort < lowestPrivate) {
			lowestPrivate = port.PrivatePort
		}
	}

	switch {
	case lowestPublished > 0:
		return true, lowestPublished
	case lowestPrivate > 0:
		return true, lowestPrivate
	default:
		return false, 0
	}
}

func compareContainerNamesForSortInternal(a, b containertypes.Summary) int {
	nameA, nameB := "", ""
	if len(a.Names) > 0 {
		nameA = a.Names[0]
	}
	if len(b.Names) > 0 {
		nameB = b.Names[0]
	}
	return strings.Compare(nameA, nameB)
}

func (s *ContainerService) buildContainerFilterAccessors() []pagination.FilterAccessor[containertypes.Summary] {
	return []pagination.FilterAccessor[containertypes.Summary]{
		{
			Key: "updates",
			Fn: func(c containertypes.Summary, filterValue string) bool {
				switch filterValue {
				case "has_update":
					return c.UpdateInfo != nil && c.UpdateInfo.HasUpdate
				case "up_to_date":
					return c.UpdateInfo != nil && !c.UpdateInfo.HasUpdate && c.UpdateInfo.Error == ""
				case "error":
					return c.UpdateInfo != nil && c.UpdateInfo.Error != ""
				case "unknown":
					return c.UpdateInfo == nil
				default:
					return true
				}
			},
		},
		{
			Key: "standalone",
			Fn: func(c containertypes.Summary, filterValue string) bool {
				isStandalone := dockerutils.ComposeProjectLabel(c.Labels) == ""
				switch filterValue {
				case "true", "1":
					return isStandalone
				case "false", "0":
					return !isStandalone
				default:
					return true
				}
			},
		},
	}
}

func (s *ContainerService) CalculateStatusCounts(items []containertypes.Summary) containertypes.StatusCounts {
	counts := containertypes.StatusCounts{
		TotalContainers: len(items),
	}
	for _, c := range items {
		if c.State == "running" {
			counts.RunningContainers++
		} else {
			counts.StoppedContainers++
		}
	}
	return counts
}

// CreateExec creates an exec instance in the container
func (s *ContainerService) CreateExec(ctx context.Context, containerID string, cmd []string) (string, error) {
	dockerClient, err := s.dockerService.GetClient(ctx)
	if err != nil {
		return "", errors.WrapIf(err, "failed to connect to Docker")
	}

	execConfig := client.ExecCreateOptions{
		AttachStdin:  true,
		AttachStdout: true,
		AttachStderr: true,
		TTY:          true,
		Cmd:          cmd,
	}

	execResp, err := dockerClient.ExecCreate(ctx, containerID, execConfig)
	if err != nil {
		return "", errors.WrapIf(err, "failed to create exec")
	}

	return execResp.ID, nil
}

// ExecSession manages the lifecycle of a Docker exec session.
type ExecSession struct {
	execID       string
	containerID  string
	hijackedResp client.HijackedResponse
	dockerClient *client.Client
	closeOnce    sync.Once
}

func (e *ExecSession) Stdin() io.WriteCloser { return e.hijackedResp.Conn }
func (e *ExecSession) Stdout() io.Reader     { return e.hijackedResp.Reader }

// Close terminates the exec session and kills the process if still running.
func (e *ExecSession) Close(ctx context.Context) error {
	var closeErr error
	e.closeOnce.Do(func() {
		slog.Debug("Closing exec session", "execID", e.execID, "containerID", e.containerID)

		// Send EOF (Ctrl-D) then exit to terminate the shell gracefully.
		_, _ = e.hijackedResp.Conn.Write([]byte{0x04})
		time.Sleep(50 * time.Millisecond)
		_, _ = e.hijackedResp.Conn.Write([]byte("exit\n"))
		time.Sleep(100 * time.Millisecond)

		e.hijackedResp.Close()
	})

	return closeErr
}

// AttachExec attaches to an exec instance and returns an ExecSession for lifecycle management.
func (s *ContainerService) AttachExec(ctx context.Context, containerID, execID string) (*ExecSession, error) {
	dockerClient, err := s.dockerService.GetClient(ctx)
	if err != nil {
		return nil, errors.WrapIf(err, "failed to connect to Docker")
	}

	execAttach, err := dockerClient.ExecAttach(ctx, execID, client.ExecAttachOptions{
		TTY: true,
	})
	if err != nil {
		return nil, errors.WrapIf(err, "failed to attach to exec")
	}

	return &ExecSession{
		execID:       execID,
		containerID:  containerID,
		hijackedResp: execAttach.HijackedResponse,
		dockerClient: dockerClient,
	}, nil
}
