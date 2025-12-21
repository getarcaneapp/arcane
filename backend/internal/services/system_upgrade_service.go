package services

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"strings"
	"sync/atomic"
	"time"

	containertypes "github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/filters"
	imagetypes "github.com/docker/docker/api/types/image"
	"github.com/getarcaneapp/arcane/backend/internal/models"
)

var (
	ErrNotRunningInDocker = errors.New("arcane is not running in a Docker container")
	ErrContainerNotFound  = errors.New("could not find Arcane container")
	ErrUpgradeInProgress  = errors.New("an upgrade is already in progress")
	ErrDockerSocketAccess = errors.New("docker socket is not accessible")
)

type SystemUpgradeService struct {
	upgrading      atomic.Bool
	dockerService  *DockerClientService
	versionService *VersionService
	eventService   *EventService
}

func NewSystemUpgradeService(
	dockerService *DockerClientService,
	versionService *VersionService,
	eventService *EventService,
) *SystemUpgradeService {
	return &SystemUpgradeService{
		dockerService:  dockerService,
		versionService: versionService,
		eventService:   eventService,
	}
}

// CanUpgrade checks if self-upgrade is possible
func (s *SystemUpgradeService) CanUpgrade(ctx context.Context) (bool, error) {
	// Check if running in Docker
	containerId, err := s.getCurrentContainerID()
	if err != nil {
		return false, ErrNotRunningInDocker
	}

	// Verify we can access Docker
	_, err = s.dockerService.GetClient()
	if err != nil {
		return false, ErrDockerSocketAccess
	}

	// Verify we can find our container
	_, err = s.findArcaneContainer(ctx, containerId)
	if err != nil {
		return false, err
	}

	return true, nil
}

// TriggerUpgradeViaCLI spawns the upgrade CLI command in a separate container
// This avoids self-termination issues by running the upgrade from outside
func (s *SystemUpgradeService) TriggerUpgradeViaCLI(ctx context.Context, user models.User) error {
	if !s.upgrading.CompareAndSwap(false, true) {
		return ErrUpgradeInProgress
	}
	defer s.upgrading.Store(false)

	// Get current container name
	containerId, err := s.getCurrentContainerID()
	if err != nil {
		return fmt.Errorf("get current container: %w", err)
	}

	currentContainer, err := s.findArcaneContainer(ctx, containerId)
	if err != nil {
		return fmt.Errorf("inspect container: %w", err)
	}

	containerName := strings.TrimPrefix(currentContainer.Name, "/")

	// Log upgrade event
	metadata := models.JSON{
		"action":        "system_upgrade_cli",
		"containerId":   containerId,
		"containerName": containerName,
		"method":        "cli",
	}
	if err := s.eventService.LogUserEvent(ctx, models.EventTypeSystemUpgrade, user.ID, user.Username, metadata); err != nil {
		slog.Warn("Failed to log upgrade event", "error", err)
	}

	// Use latest tag for the upgrader CLI container
	upgraderImage := "ghcr.io/getarcaneapp/arcane:latest"

	slog.Info("Spawning upgrade CLI command", "containerName", containerName, "upgraderImage", upgraderImage)

	// Spawn the upgrade command in a detached container
	// This will run independently of the current container
	dockerClient, err := s.dockerService.GetClient()
	if err != nil {
		return fmt.Errorf("failed to connect to Docker: %w", err)
	}

	// Pull the upgrader image first to ensure it exists
	slog.Info("Pulling upgrader image", "image", upgraderImage)
	pullReader, err := dockerClient.ImagePull(ctx, upgraderImage, imagetypes.PullOptions{})
	if err != nil {
		return fmt.Errorf("pull upgrader image: %w", err)
	}
	// Drain the reader to complete the pull
	_, _ = io.Copy(io.Discard, pullReader)
	pullReader.Close()
	slog.Info("Upgrader image pulled successfully", "image", upgraderImage)

	// Create the upgrader container config
	config := &containertypes.Config{
		Image: upgraderImage,
		Cmd:   []string{"/app/arcane", "upgrade", "--container", containerName},
	}

	hostConfig := &containertypes.HostConfig{
		AutoRemove: true, // Clean up after completion
		Binds: []string{
			"/var/run/docker.sock:/var/run/docker.sock",
		},
	}

	containerName = fmt.Sprintf("%s-upgrader-%d", containerName, time.Now().Unix())

	resp, err := dockerClient.ContainerCreate(ctx, config, hostConfig, nil, nil, containerName)
	if err != nil {
		return fmt.Errorf("create upgrader container: %w", err)
	}

	// Start the upgrader container - it will run the upgrade and auto-remove
	if err := dockerClient.ContainerStart(ctx, resp.ID, containertypes.StartOptions{}); err != nil {
		_ = dockerClient.ContainerRemove(ctx, resp.ID, containertypes.RemoveOptions{Force: true})
		return fmt.Errorf("start upgrader container: %w", err)
	}

	slog.Info("Upgrade container started", "upgraderId", resp.ID[:12], "upgraderName", containerName)

	return nil
}

// getCurrentContainerID detects if we're running in Docker and returns container ID
func (s *SystemUpgradeService) getCurrentContainerID() (string, error) {
	// Try reading from /proc/self/cgroup (Linux)
	if id, err := s.getContainerIDFromCgroup(); err == nil {
		return id, nil
	}

	// Try reading from /proc/self/mountinfo (alternative method)
	if id, err := s.getContainerIDFromMountinfo(); err == nil {
		return id, nil
	}

	// Try hostname (works in many Docker setups)
	if id, err := s.getContainerIDFromHostname(); err == nil {
		return id, nil
	}

	return "", ErrNotRunningInDocker
}

// getContainerIDFromCgroup reads container ID from /proc/self/cgroup
func (s *SystemUpgradeService) getContainerIDFromCgroup() (string, error) {
	data, err := os.ReadFile("/proc/self/cgroup")
	if err != nil {
		return "", err
	}

	lines := strings.Split(string(data), "\n")
	for _, line := range lines {
		if strings.Contains(line, "docker") || strings.Contains(line, "containerd") {
			parts := strings.Split(line, "/")
			if len(parts) > 0 {
				id := strings.TrimSpace(parts[len(parts)-1])
				if len(id) >= 12 {
					return id, nil
				}
			}
		}
	}

	return "", errors.New("container ID not found in cgroup")
}

// getContainerIDFromMountinfo reads container ID from /proc/self/mountinfo
func (s *SystemUpgradeService) getContainerIDFromMountinfo() (string, error) {
	data, err := os.ReadFile("/proc/self/mountinfo")
	if err != nil {
		return "", err
	}

	lines := strings.Split(string(data), "\n")
	for _, line := range lines {
		if strings.Contains(line, "/docker/containers/") {
			parts := strings.Split(line, "/docker/containers/")
			if len(parts) > 1 {
				idParts := strings.Split(parts[1], "/")
				if len(idParts) > 0 && len(idParts[0]) >= 12 {
					return idParts[0], nil
				}
			}
		}
	}

	return "", errors.New("container ID not found in mountinfo")
}

// getContainerIDFromHostname tries to get container ID from hostname
func (s *SystemUpgradeService) getContainerIDFromHostname() (string, error) {
	hostname, err := os.Hostname()
	if err != nil {
		return "", err
	}

	if len(hostname) == 12 || len(hostname) == 64 {
		return hostname, nil
	}

	return "", errors.New("hostname is not a valid container ID")
}

// findArcaneContainer finds the container using the ID
func (s *SystemUpgradeService) findArcaneContainer(ctx context.Context, containerId string) (containertypes.InspectResponse, error) {
	dockerClient, err := s.dockerService.GetClient()
	if err != nil {
		return containertypes.InspectResponse{}, err
	}

	// Try to inspect the container directly
	container, err := dockerClient.ContainerInspect(ctx, containerId)
	if err == nil {
		return container, nil
	}

	// Fallback: search for containers with arcane image
	filter := filters.NewArgs()
	filter.Add("ancestor", "ghcr.io/getarcaneapp/arcane")

	containers, err := dockerClient.ContainerList(ctx, containertypes.ListOptions{
		All:     true,
		Filters: filter,
	})
	if err != nil {
		return containertypes.InspectResponse{}, err
	}

	for _, c := range containers {
		if strings.HasPrefix(c.ID, containerId) {
			return dockerClient.ContainerInspect(ctx, c.ID)
		}
	}

	// Try without filter - search all containers
	allContainers, err := dockerClient.ContainerList(ctx, containertypes.ListOptions{All: true})
	if err != nil {
		return containertypes.InspectResponse{}, err
	}

	for _, c := range allContainers {
		if strings.HasPrefix(c.ID, containerId) || c.ID == containerId {
			return dockerClient.ContainerInspect(ctx, c.ID)
		}
	}

	return containertypes.InspectResponse{}, ErrContainerNotFound
}
