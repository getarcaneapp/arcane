package services

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/getarcaneapp/arcane/backend/internal/database"
	"github.com/getarcaneapp/arcane/backend/internal/models"
	dockerutils "github.com/getarcaneapp/arcane/backend/pkg/dockerutil"
	"github.com/getarcaneapp/arcane/backend/pkg/libarcane"
	"github.com/getarcaneapp/arcane/backend/pkg/libarcane/timeouts"
	"github.com/getarcaneapp/arcane/backend/pkg/pagination"
	containertypes "github.com/getarcaneapp/arcane/types/container"
	"github.com/getarcaneapp/arcane/types/containerregistry"
	imagetypes "github.com/getarcaneapp/arcane/types/image"
	"github.com/moby/moby/api/pkg/stdcopy"
	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/api/types/network"
	"github.com/moby/moby/client"
)

type ContainerService struct {
	db              *database.DB
	dockerService   *DockerClientService
	eventService    *EventService
	imageService    *ImageService
	settingsService *SettingsService
}

const (
	containerGroupByProject = "project"
	containerNoProjectGroup = "No Project"
)

type ContainerListResult struct {
	Items      []containertypes.Summary
	Groups     []containertypes.SummaryGroup
	Pagination pagination.Response
	Counts     containertypes.StatusCounts
}

func NewContainerService(db *database.DB, eventService *EventService, dockerService *DockerClientService, imageService *ImageService, settingsService *SettingsService) *ContainerService {
	return &ContainerService{
		db:              db,
		eventService:    eventService,
		dockerService:   dockerService,
		imageService:    imageService,
		settingsService: settingsService,
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

func (s *ContainerService) pullRedeployImageInternal(ctx context.Context, dockerClient *client.Client, imageName, containerID, containerName string, user models.User) error {
	settings := s.settingsService.GetSettingsConfig()
	pullCtx, pullCancel := timeouts.WithTimeout(ctx, settings.DockerImagePullTimeout.AsInt(), timeouts.DefaultDockerImagePull)
	defer pullCancel()

	pullOptions, authErr := s.imageService.getPullOptionsWithAuth(ctx, imageName, nil)
	if authErr != nil {
		slog.WarnContext(ctx, "failed to get registry authentication for container redeploy pull; proceeding without auth",
			"image", imageName,
			"error", authErr.Error(),
		)
		pullOptions = client.ImagePullOptions{}
	}

	reader, pullErr := dockerClient.ImagePull(pullCtx, imageName, pullOptions)
	if pullErr != nil && shouldRetryAnonymousPullInternal(pullOptions, pullErr) {
		slog.WarnContext(ctx, "container redeploy image pull failed with registry auth; retrying anonymously",
			"image", imageName,
			"error", pullErr.Error(),
		)
		pullOptions = client.ImagePullOptions{}
		reader, pullErr = dockerClient.ImagePull(pullCtx, imageName, pullOptions)
	}
	if pullErr != nil {
		if errors.Is(pullCtx.Err(), context.DeadlineExceeded) {
			s.eventService.LogErrorEvent(ctx, models.EventTypeContainerError, "container", containerID, containerName, user.ID, user.Username, "0", pullErr, models.JSON{
				"action": "redeploy",
				"step":   "pull_image_timeout",
				"image":  imageName,
			})
			return fmt.Errorf("image pull timed out for %s (increase DOCKER_IMAGE_PULL_TIMEOUT or setting)", imageName)
		}

		s.eventService.LogErrorEvent(ctx, models.EventTypeContainerError, "container", containerID, containerName, user.ID, user.Username, "0", pullErr, models.JSON{
			"action": "redeploy",
			"step":   "pull_image",
			"image":  imageName,
		})
		return fmt.Errorf("failed to pull image %s: %w", imageName, pullErr)
	}
	defer func() { _ = reader.Close() }()

	streamErr := dockerutils.ConsumeJSONMessageStream(reader, nil)
	if streamErr != nil {
		s.eventService.LogErrorEvent(ctx, models.EventTypeContainerError, "container", containerID, containerName, user.ID, user.Username, "0", streamErr, models.JSON{
			"action": "redeploy",
			"step":   "complete_pull",
			"image":  imageName,
		})
		return fmt.Errorf("failed to complete image pull: %w", streamErr)
	}

	return nil
}

func (s *ContainerService) prepareContainerForRedeployInternal(ctx context.Context, dockerClient *client.Client, containerID, containerName, backupName string, wasRunning bool, user models.User) error {
	if containerName != "" {
		if _, err := dockerClient.ContainerRename(ctx, containerID, client.ContainerRenameOptions{NewName: backupName}); err != nil {
			s.eventService.LogErrorEvent(ctx, models.EventTypeContainerError, "container", containerID, containerName, user.ID, user.Username, "0", err, models.JSON{
				"action":     "redeploy",
				"step":       "rename_old",
				"backupName": backupName,
			})
			return fmt.Errorf("failed to rename existing container: %w", err)
		}
	}

	if !wasRunning {
		return nil
	}

	timeout := 30
	_, err := dockerClient.ContainerStop(ctx, containerID, client.ContainerStopOptions{Timeout: &timeout})
	if err == nil {
		return nil
	}

	if containerName != "" {
		if _, renameErr := dockerClient.ContainerRename(ctx, containerID, client.ContainerRenameOptions{NewName: containerName}); renameErr != nil {
			s.eventService.LogErrorEvent(ctx, models.EventTypeContainerError, "container", containerID, containerName, user.ID, user.Username, "0", renameErr, models.JSON{
				"action": "redeploy",
				"step":   "restore_name_after_stop_failure",
			})
		}
	}

	s.eventService.LogErrorEvent(ctx, models.EventTypeContainerError, "container", containerID, containerName, user.ID, user.Username, "0", err, models.JSON{
		"action": "redeploy",
		"step":   "stop",
	})
	return fmt.Errorf("failed to stop container: %w", err)
}

func (s *ContainerService) restoreContainerAfterRedeployFailureInternal(ctx context.Context, dockerClient *client.Client, containerID, containerName, backupName, failedStep string, wasRunning bool, user models.User) {
	if wasRunning {
		if _, startErr := dockerClient.ContainerStart(ctx, containerID, client.ContainerStartOptions{}); startErr != nil {
			s.eventService.LogErrorEvent(ctx, models.EventTypeContainerError, "container", containerID, containerName, user.ID, user.Username, "0", startErr, models.JSON{
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
		s.eventService.LogErrorEvent(ctx, models.EventTypeContainerError, "container", containerID, backupName, user.ID, user.Username, "0", renameErr, models.JSON{
			"action":     "redeploy",
			"step":       "restore_name",
			"failedStep": failedStep,
		})
	}
}

func (s *ContainerService) StartContainer(ctx context.Context, containerID string, user models.User) error {
	dockerClient, err := s.dockerService.GetClient(ctx)
	if err != nil {
		s.eventService.LogErrorEvent(ctx, models.EventTypeContainerError, "container", containerID, "", user.ID, user.Username, "0", err, models.JSON{"action": "start"})
		return fmt.Errorf("failed to connect to Docker: %w", err)
	}

	metadata := models.JSON{
		"action":      "start",
		"containerId": containerID,
	}

	err = s.eventService.LogContainerEvent(ctx, models.EventTypeContainerStart, containerID, "name", user.ID, user.Username, "0", metadata)
	if err != nil {
		fmt.Printf("Could not log container start action: %s\n", err)
	}

	_, err = dockerClient.ContainerStart(ctx, containerID, client.ContainerStartOptions{})
	if err != nil {
		s.eventService.LogErrorEvent(ctx, models.EventTypeContainerError, "container", containerID, "", user.ID, user.Username, "0", err, models.JSON{"action": "start"})
	}
	return err
}

func (s *ContainerService) StopContainer(ctx context.Context, containerID string, user models.User) error {
	dockerClient, err := s.dockerService.GetClient(ctx)
	if err != nil {
		s.eventService.LogErrorEvent(ctx, models.EventTypeContainerError, "container", containerID, "", user.ID, user.Username, "0", err, models.JSON{"action": "stop"})
		return fmt.Errorf("failed to connect to Docker: %w", err)
	}

	metadata := models.JSON{
		"action":      "stop",
		"containerId": containerID,
	}

	err = s.eventService.LogContainerEvent(ctx, models.EventTypeContainerStop, containerID, "name", user.ID, user.Username, "0", metadata)
	if err != nil {
		return fmt.Errorf("failed to log action: %w", err)
	}

	timeout := 30
	_, err = dockerClient.ContainerStop(ctx, containerID, client.ContainerStopOptions{Timeout: &timeout})
	if err != nil {
		s.eventService.LogErrorEvent(ctx, models.EventTypeContainerError, "container", containerID, "", user.ID, user.Username, "0", err, models.JSON{"action": "stop"})
	}
	return err
}

func (s *ContainerService) RestartContainer(ctx context.Context, containerID string, user models.User) error {
	dockerClient, err := s.dockerService.GetClient(ctx)
	if err != nil {
		s.eventService.LogErrorEvent(ctx, models.EventTypeContainerError, "container", containerID, "", user.ID, user.Username, "0", err, models.JSON{"action": "restart"})
		return fmt.Errorf("failed to connect to Docker: %w", err)
	}

	metadata := models.JSON{
		"action":      "restart",
		"containerId": containerID,
	}

	err = s.eventService.LogContainerEvent(ctx, models.EventTypeContainerRestart, containerID, "name", user.ID, user.Username, "0", metadata)
	if err != nil {
		return fmt.Errorf("failed to log action: %w", err)
	}

	_, err = dockerClient.ContainerRestart(ctx, containerID, client.ContainerRestartOptions{})
	if err != nil {
		s.eventService.LogErrorEvent(ctx, models.EventTypeContainerError, "container", containerID, "", user.ID, user.Username, "0", err, models.JSON{"action": "restart"})
	}
	return err
}

func (s *ContainerService) RedeployContainer(ctx context.Context, containerID string, user models.User) (string, error) {
	dockerClient, err := s.dockerService.GetClient(ctx)
	if err != nil {
		s.eventService.LogErrorEvent(ctx, models.EventTypeContainerError, "container", containerID, "", user.ID, user.Username, "0", err, models.JSON{
			"action": "redeploy",
			"step":   "get_client",
		})
		return "", fmt.Errorf("failed to connect to Docker: %w", err)
	}

	containerJSON, err := libarcane.ContainerInspectWithCompatibility(ctx, dockerClient, containerID, client.ContainerInspectOptions{})
	if err != nil {
		s.eventService.LogErrorEvent(ctx, models.EventTypeContainerError, "container", containerID, "", user.ID, user.Username, "0", err, models.JSON{
			"action": "redeploy",
			"step":   "inspect",
		})
		return "", fmt.Errorf("failed to inspect container: %w", err)
	}

	containerInfo := containerJSON.Container
	if containerInfo.Config == nil {
		err = errors.New("container config is nil")
		s.eventService.LogErrorEvent(ctx, models.EventTypeContainerError, "container", containerID, "", user.ID, user.Username, "0", err, models.JSON{
			"action": "redeploy",
			"step":   "validate_config",
		})
		return "", fmt.Errorf("failed to redeploy container: %w", err)
	}

	containerName := strings.TrimPrefix(containerInfo.Name, "/")
	imageName := containerInfo.Config.Image
	wasRunning := containerInfo.State != nil && containerInfo.State.Running
	apiVersion := libarcane.DetectDockerAPIVersion(ctx, dockerClient)

	metadata := models.JSON{
		"action":        "redeploy",
		"containerId":   containerID,
		"containerName": containerName,
		"image":         imageName,
	}

	if imageName != "" {
		if err := s.pullRedeployImageInternal(ctx, dockerClient, imageName, containerID, containerName, user); err != nil {
			return "", err
		}
	}

	backupName := buildRedeployBackupNameInternal(containerName, containerID)
	if err := s.prepareContainerForRedeployInternal(ctx, dockerClient, containerID, containerName, backupName, wasRunning, user); err != nil {
		return "", err
	}

	networkingConfig := buildCleanNetworkingConfigInternal(containerInfo, apiVersion)

	newConfig := *containerInfo.Config
	if len(containerID) >= 12 && newConfig.Hostname == containerID[:12] {
		newConfig.Hostname = ""
	}

	createResp, err := libarcane.ContainerCreateWithCompatibilityForAPIVersion(ctx, dockerClient, client.ContainerCreateOptions{
		Config:           &newConfig,
		HostConfig:       containerInfo.HostConfig,
		NetworkingConfig: networkingConfig,
		Name:             containerName,
	}, apiVersion)
	if err != nil {
		s.restoreContainerAfterRedeployFailureInternal(ctx, dockerClient, containerID, containerName, backupName, "create", wasRunning, user)
		s.eventService.LogErrorEvent(ctx, models.EventTypeContainerError, "container", containerID, containerName, user.ID, user.Username, "0", err, models.JSON{
			"action": "redeploy",
			"step":   "create",
			"image":  imageName,
		})
		return "", fmt.Errorf("failed to recreate container: %w", err)
	}

	if shouldStartRedeployedContainerInternal(containerInfo, wasRunning) {
		_, err = dockerClient.ContainerStart(ctx, createResp.ID, client.ContainerStartOptions{})
		if err != nil {
			if _, removeErr := dockerClient.ContainerRemove(ctx, createResp.ID, client.ContainerRemoveOptions{Force: true}); removeErr != nil {
				s.eventService.LogErrorEvent(ctx, models.EventTypeContainerError, "container", createResp.ID, containerName, user.ID, user.Username, "0", removeErr, models.JSON{
					"action": "redeploy",
					"step":   "cleanup_failed_start",
				})
			}
			s.restoreContainerAfterRedeployFailureInternal(ctx, dockerClient, containerID, containerName, backupName, "start", wasRunning, user)
			s.eventService.LogErrorEvent(ctx, models.EventTypeContainerError, "container", createResp.ID, containerName, user.ID, user.Username, "0", err, models.JSON{
				"action": "redeploy",
				"step":   "start",
				"image":  imageName,
			})
			return "", fmt.Errorf("failed to start new container: %w", err)
		}
	}

	slog.InfoContext(ctx, "container redeployed successfully",
		"oldContainerId", containerID,
		"newContainerId", createResp.ID,
		"containerName", containerName,
		"image", imageName,
	)

	if _, err := dockerClient.ContainerRemove(ctx, containerID, client.ContainerRemoveOptions{
		Force:         true,
		RemoveVolumes: false,
		RemoveLinks:   false,
	}); err != nil {
		slog.WarnContext(ctx, "failed to remove old container after successful redeploy",
			"containerId", containerID,
			"backupName", backupName,
			"error", err,
		)
	}

	if logErr := s.eventService.LogContainerEvent(ctx, models.EventTypeContainerDeploy, createResp.ID, containerName, user.ID, user.Username, "0", metadata); logErr != nil {
		slog.WarnContext(ctx, "failed to log deploy event", "err", logErr)
	}

	return createResp.ID, nil
}

func (s *ContainerService) GetContainerByID(ctx context.Context, id string) (*container.InspectResponse, error) {
	dockerClient, err := s.dockerService.GetClient(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to Docker: %w", err)
	}

	containerInspect, err := libarcane.ContainerInspectWithCompatibility(ctx, dockerClient, id, client.ContainerInspectOptions{})
	if err != nil {
		return nil, fmt.Errorf("container not found: %w", err)
	}

	containerInfo := containerInspect.Container
	return &containerInfo, nil
}

func (s *ContainerService) DeleteContainer(ctx context.Context, containerID string, force bool, removeVolumes bool, user models.User) error {
	dockerClient, err := s.dockerService.GetClient(ctx)
	if err != nil {
		s.eventService.LogErrorEvent(ctx, models.EventTypeContainerError, "container", containerID, "", user.ID, user.Username, "0", err, models.JSON{"action": "delete", "force": force, "removeVolumes": removeVolumes})
		return fmt.Errorf("failed to connect to Docker: %w", err)
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
		s.eventService.LogErrorEvent(ctx, models.EventTypeContainerError, "container", containerID, "", user.ID, user.Username, "0", err, models.JSON{"action": "delete", "force": force, "removeVolumes": removeVolumes})
		return fmt.Errorf("failed to delete container: %w", err)
	}

	// Remove named volumes if requested
	if removeVolumes && len(volumesToRemove) > 0 {
		for _, volumeName := range volumesToRemove {
			if _, removeErr := dockerClient.VolumeRemove(ctx, volumeName, client.VolumeRemoveOptions{Force: false}); removeErr != nil {
				// Log but don't fail if volume removal fails (might be in use by another container)
				s.eventService.LogErrorEvent(ctx, models.EventTypeVolumeError, "volume", volumeName, "", user.ID, user.Username, "0", removeErr, models.JSON{"action": "delete", "container": containerID})
			}
		}
	}

	metadata := models.JSON{
		"action":      "delete",
		"containerId": containerID,
	}

	err = s.eventService.LogContainerEvent(ctx, models.EventTypeContainerDelete, containerID, "name", user.ID, user.Username, "0", metadata)
	if err != nil {
		return fmt.Errorf("failed to log action: %w", err)
	}

	return nil
}

func (s *ContainerService) CreateContainer(ctx context.Context, config *container.Config, hostConfig *container.HostConfig, networkingConfig *network.NetworkingConfig, containerName string, user models.User, credentials []containerregistry.Credential) (*container.InspectResponse, error) {
	dockerClient, err := s.dockerService.GetClient(ctx)
	if err != nil {
		s.eventService.LogErrorEvent(ctx, models.EventTypeContainerError, "container", "", containerName, user.ID, user.Username, "0", err, models.JSON{"action": "create", "image": config.Image})
		return nil, fmt.Errorf("failed to connect to Docker: %w", err)
	}

	_, err = dockerClient.ImageInspect(ctx, config.Image)
	if err != nil {
		// Image not found locally, need to pull it
		pullOptions, authErr := s.imageService.getPullOptionsWithAuth(ctx, config.Image, credentials)
		if authErr != nil {
			slog.WarnContext(ctx, "Failed to get registry authentication for container image; proceeding without auth",
				"image", config.Image,
				"error", authErr.Error())
			pullOptions = client.ImagePullOptions{}
		}

		settings := s.settingsService.GetSettingsConfig()
		pullCtx, pullCancel := timeouts.WithTimeout(ctx, settings.DockerImagePullTimeout.AsInt(), timeouts.DefaultDockerImagePull)
		defer pullCancel()

		reader, pullErr := dockerClient.ImagePull(pullCtx, config.Image, pullOptions)
		if pullErr != nil {
			if errors.Is(pullCtx.Err(), context.DeadlineExceeded) {
				s.eventService.LogErrorEvent(ctx, models.EventTypeContainerError, "container", "", containerName, user.ID, user.Username, "0", pullErr, models.JSON{"action": "create", "image": config.Image, "step": "pull_image_timeout"})
				return nil, fmt.Errorf("image pull timed out for %s (increase DOCKER_IMAGE_PULL_TIMEOUT or setting)", config.Image)
			}
			s.eventService.LogErrorEvent(ctx, models.EventTypeContainerError, "container", "", containerName, user.ID, user.Username, "0", pullErr, models.JSON{"action": "create", "image": config.Image, "step": "pull_image"})
			return nil, fmt.Errorf("failed to pull image %s: %w", config.Image, pullErr)
		}
		defer func() { _ = reader.Close() }()

		streamErr := dockerutils.ConsumeJSONMessageStream(reader, nil)
		if streamErr != nil {
			s.eventService.LogErrorEvent(ctx, models.EventTypeContainerError, "container", "", containerName, user.ID, user.Username, "0", streamErr, models.JSON{"action": "create", "image": config.Image, "step": "complete_pull"})
			return nil, fmt.Errorf("failed to complete image pull: %w", streamErr)
		}
	}

	resp, err := libarcane.ContainerCreateWithCompatibility(ctx, dockerClient, client.ContainerCreateOptions{
		Config:           config,
		HostConfig:       hostConfig,
		NetworkingConfig: networkingConfig,
		Name:             containerName,
	})
	if err != nil {
		s.eventService.LogErrorEvent(ctx, models.EventTypeContainerError, "container", "", containerName, user.ID, user.Username, "0", err, models.JSON{"action": "create", "image": config.Image, "step": "create"})
		return nil, fmt.Errorf("failed to create container: %w", err)
	}

	metadata := models.JSON{
		"action":      "create",
		"containerId": resp.ID,
	}

	if logErr := s.eventService.LogContainerEvent(ctx, models.EventTypeContainerCreate, resp.ID, "name", user.ID, user.Username, "0", metadata); logErr != nil {
		fmt.Printf("Could not log container stop action: %s\n", logErr)
	}

	if _, err := dockerClient.ContainerStart(ctx, resp.ID, client.ContainerStartOptions{}); err != nil {
		_, _ = dockerClient.ContainerRemove(ctx, resp.ID, client.ContainerRemoveOptions{Force: true})
		s.eventService.LogErrorEvent(ctx, models.EventTypeContainerError, "container", resp.ID, containerName, user.ID, user.Username, "0", err, models.JSON{"action": "create", "image": config.Image, "step": "start"})
		return nil, fmt.Errorf("failed to start container: %w", err)
	}

	containerJSON, err := libarcane.ContainerInspectWithCompatibility(ctx, dockerClient, resp.ID, client.ContainerInspectOptions{})
	if err != nil {
		s.eventService.LogErrorEvent(ctx, models.EventTypeContainerError, "container", resp.ID, containerName, user.ID, user.Username, "0", err, models.JSON{"action": "create", "image": config.Image, "step": "inspect"})
		return nil, fmt.Errorf("failed to inspect created container: %w", err)
	}

	containerInfo := containerJSON.Container
	return &containerInfo, nil
}

func (s *ContainerService) StreamStats(ctx context.Context, containerID string, statsChan chan<- any) error {
	dockerClient, err := s.dockerService.GetClient(ctx)
	if err != nil {
		return fmt.Errorf("failed to connect to Docker: %w", err)
	}

	stats, err := dockerClient.ContainerStats(ctx, containerID, client.ContainerStatsOptions{Stream: true})
	if err != nil {
		return fmt.Errorf("failed to start stats stream: %w", err)
	}
	defer func() { _ = stats.Body.Close() }()

	decoder := json.NewDecoder(stats.Body)

	for {
		if err := ctx.Err(); err != nil {
			return err
		}

		var statsData any
		if err := decoder.Decode(&statsData); err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			if err == io.EOF {
				return nil
			}
			return fmt.Errorf("failed to decode stats: %w", err)
		}

		select {
		case statsChan <- statsData:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
}

func (s *ContainerService) StreamLogs(ctx context.Context, containerID string, logsChan chan<- string, follow bool, tail, since string, timestamps bool) error {
	dockerClient, err := s.dockerService.GetClient(ctx)
	if err != nil {
		return fmt.Errorf("failed to connect to Docker: %w", err)
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
		return fmt.Errorf("failed to get container logs: %w", err)
	}
	defer func() { _ = logs.Close() }()

	if follow {
		return s.streamMultiplexedLogs(ctx, logs, logsChan)
	}

	return s.readAllLogs(logs, logsChan)
}

func (s *ContainerService) streamMultiplexedLogs(ctx context.Context, logs io.ReadCloser, logsChan chan<- string) error {
	// Use stdcopy to demultiplex Docker's stream format
	// Docker multiplexes stdout and stderr in a special format
	stdoutReader, stdoutWriter := io.Pipe()
	stderrReader, stderrWriter := io.Pipe()

	// Start demultiplexing in a goroutine
	go func() {
		defer func() { _ = stdoutWriter.Close() }()
		defer func() { _ = stderrWriter.Close() }()
		_, err := stdcopy.StdCopy(stdoutWriter, stderrWriter, logs)
		if err != nil && !errors.Is(err, io.EOF) {
			fmt.Printf("Error demultiplexing logs: %v\n", err)
		}
	}()

	// Read from both stdout and stderr concurrently
	done := make(chan error, 2)

	// Read stdout
	go func() {
		done <- s.readLogsFromReader(ctx, stdoutReader, logsChan, "stdout")
	}()

	// Read stderr
	go func() {
		done <- s.readLogsFromReader(ctx, stderrReader, logsChan, "stderr")
	}()

	// Wait for context cancellation or error
	select {
	case <-ctx.Done():
		return ctx.Err()
	case err := <-done:
		if err != nil && !errors.Is(err, io.EOF) {
			return err
		}
		// Wait for the other goroutine or context cancellation
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-done:
			return nil
		}
	}
}

// readLogsFromReader reads logs line by line from a reader
func (s *ContainerService) readLogsFromReader(ctx context.Context, reader io.Reader, logsChan chan<- string, source string) error {
	scanner := bufio.NewScanner(reader)

	for scanner.Scan() {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
			line := scanner.Text()
			if line != "" {
				// Add source prefix for stderr logs
				if source == "stderr" {
					line = "[STDERR] " + line
				}

				select {
				case logsChan <- line:
				case <-ctx.Done():
					return ctx.Err()
				}
			}
		}
	}

	return scanner.Err()
}

func (s *ContainerService) readAllLogs(logs io.ReadCloser, logsChan chan<- string) error {
	stdoutBuf := &strings.Builder{}
	stderrBuf := &strings.Builder{}

	_, err := stdcopy.StdCopy(stdoutBuf, stderrBuf, logs)
	if err != nil && !errors.Is(err, io.EOF) {
		return fmt.Errorf("failed to demultiplex logs: %w", err)
	}

	// Send stdout lines
	if stdoutBuf.Len() > 0 {
		lines := strings.SplitSeq(strings.TrimRight(stdoutBuf.String(), "\n"), "\n")
		for line := range lines {
			if line != "" {
				logsChan <- line
			}
		}
	}

	// Send stderr lines with prefix
	if stderrBuf.Len() > 0 {
		lines := strings.SplitSeq(strings.TrimRight(stderrBuf.String(), "\n"), "\n")
		for line := range lines {
			if line != "" {
				logsChan <- "[STDERR] " + line
			}
		}
	}

	return nil
}

func (s *ContainerService) ListContainersPaginated(
	ctx context.Context,
	params pagination.QueryParams,
	includeAll bool,
	includeInternal bool,
	groupBy string,
) (ContainerListResult, error) {
	dockerClient, err := s.dockerService.GetClient(ctx)
	if err != nil {
		return ContainerListResult{}, fmt.Errorf("failed to connect to Docker: %w", err)
	}

	containerList, err := dockerClient.ContainerList(ctx, client.ContainerListOptions{All: includeAll})
	if err != nil {
		return ContainerListResult{}, fmt.Errorf("failed to list Docker containers: %w", err)
	}
	dockerContainers := containerList.Items

	dockerContainers = filterInternalContainers(dockerContainers, includeInternal)
	imageIDs := collectImageIDs(dockerContainers)
	updateInfoMap := s.getUpdateInfoMap(ctx, imageIDs)
	items := s.buildContainerSummaries(dockerContainers, updateInfoMap)

	config := s.buildContainerPaginationConfig()
	counts := s.calculateContainerStatusCounts(items)

	if groupBy == containerGroupByProject {
		ungroupedParams := params
		ungroupedParams.Start = 0
		ungroupedParams.Limit = -1

		result := pagination.SearchOrderAndPaginate(items, ungroupedParams, config)
		groups, paginationResp := paginateContainerProjectGroupsInternal(result, params)
		return ContainerListResult{
			Items:      flattenContainerProjectGroupsInternal(groups),
			Groups:     groups,
			Pagination: paginationResp,
			Counts:     counts,
		}, nil
	}

	result := pagination.SearchOrderAndPaginate(items, params, config)
	paginationResp := pagination.BuildResponseFromFilterResult(result, params)

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
	groupedItems := flattenContainerProjectGroupsInternal(groups)
	totalCount := len(groupedItems)

	if params.Limit <= 0 {
		return groups, pagination.Response{
			TotalPages:      1,
			TotalItems:      int64(totalCount),
			CurrentPage:     1,
			ItemsPerPage:    totalCount,
			GrandTotalItems: result.TotalAvailable,
		}
	}

	pages := partitionContainerProjectPagesInternal(groups, params.Limit)
	requestedPage := 1
	if params.Limit > 0 {
		requestedPage = (params.Start / params.Limit) + 1
	}

	if requestedPage < 1 {
		requestedPage = 1
	}

	totalPages := len(pages)
	if totalPages == 0 {
		totalPages = 1
	}
	if requestedPage > totalPages {
		requestedPage = totalPages
	}

	pageGroups := []containertypes.SummaryGroup{}
	if len(pages) > 0 {
		pageGroups = pages[requestedPage-1]
	}

	return pageGroups, pagination.Response{
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

func partitionContainerProjectPagesInternal(groups []containertypes.SummaryGroup, limit int) [][]containertypes.SummaryGroup {
	if len(groups) == 0 {
		return [][]containertypes.SummaryGroup{{}}
	}

	pages := make([][]containertypes.SummaryGroup, 0)
	currentPage := make([]containertypes.SummaryGroup, 0)
	currentCount := 0

	for _, group := range groups {
		currentPage = append(currentPage, group)
		currentCount += len(group.Items)

		if currentCount >= limit {
			pages = append(pages, currentPage)
			currentPage = make([]containertypes.SummaryGroup, 0)
			currentCount = 0
		}
	}

	if len(currentPage) > 0 || len(pages) == 0 {
		pages = append(pages, currentPage)
	}

	return pages
}

func getContainerProjectNameInternal(container containertypes.Summary) string {
	if container.Labels == nil {
		return containerNoProjectGroup
	}

	projectName := strings.TrimSpace(container.Labels["com.docker.compose.project"])
	if projectName == "" {
		return containerNoProjectGroup
	}

	return projectName
}

func filterInternalContainers(containers []container.Summary, includeInternal bool) []container.Summary {
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

func collectImageIDs(containers []container.Summary) []string {
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

func (s *ContainerService) getUpdateInfoMap(ctx context.Context, imageIDs []string) map[string]*imagetypes.UpdateInfo {
	if s.imageService == nil || len(imageIDs) == 0 {
		return make(map[string]*imagetypes.UpdateInfo)
	}

	updateInfoMap, err := s.imageService.GetUpdateInfoByImageIDs(ctx, imageIDs)
	if err != nil {
		// Log error but continue - update info is optional
		slog.WarnContext(ctx, "Failed to fetch image update info for containers", "error", err)
		return make(map[string]*imagetypes.UpdateInfo)
	}

	return updateInfoMap
}

func (s *ContainerService) buildContainerSummaries(containers []container.Summary, updateInfoMap map[string]*imagetypes.UpdateInfo) []containertypes.Summary {
	items := make([]containertypes.Summary, 0, len(containers))
	for _, dc := range containers {
		summary := containertypes.NewSummary(dc)
		if info, exists := updateInfoMap[dc.ImageID]; exists {
			summary.UpdateInfo = info
		}
		items = append(items, summary)
	}
	return items
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
	}
}

func (s *ContainerService) calculateContainerStatusCounts(items []containertypes.Summary) containertypes.StatusCounts {
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
		return "", fmt.Errorf("failed to connect to Docker: %w", err)
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
		return "", fmt.Errorf("failed to create exec: %w", err)
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
		return nil, fmt.Errorf("failed to connect to Docker: %w", err)
	}

	execAttach, err := dockerClient.ExecAttach(ctx, execID, client.ExecAttachOptions{
		TTY: true,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to attach to exec: %w", err)
	}

	return &ExecSession{
		execID:       execID,
		containerID:  containerID,
		hijackedResp: execAttach.HijackedResponse,
		dockerClient: dockerClient,
	}, nil
}
