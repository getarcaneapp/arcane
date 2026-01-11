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
	mounttypes "github.com/docker/docker/api/types/mount"
	"github.com/getarcaneapp/arcane/backend/internal/models"
	dockerutils "github.com/getarcaneapp/arcane/backend/internal/utils/docker"
)

var (
	ErrNotRunningInDocker = errors.New("arcane is not running in a Docker container")
	ErrContainerNotFound  = errors.New("could not find Arcane container")
	ErrUpgradeInProgress  = errors.New("an upgrade is already in progress")
	ErrDockerSocketAccess = errors.New("docker socket is not accessible")
	ArcaneUpgraderImage   = "ghcr.io/getarcaneapp/arcane:latest"
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

	// Use the same image reference as the currently running Arcane container for the upgrader.
	// This avoids mismatches where a newer/older upgrader CLI expects different behavior.
	if currentContainer.Config != nil {
		if img := strings.TrimSpace(currentContainer.Config.Image); img != "" {
			ArcaneUpgraderImage = img
		}
	}
	slog.Debug("Using upgrader image", "image", ArcaneUpgraderImage)

	slog.Info("Spawning upgrade CLI command", "containerName", containerName, "upgraderImage", ArcaneUpgraderImage)

	// Spawn the upgrade command in a detached container
	// This will run independently of the current container
	dockerClient, err := s.dockerService.GetClient()
	if err != nil {
		return fmt.Errorf("failed to connect to Docker: %w", err)
	}

	// Pull the upgrader image first to ensure it exists
	slog.Info("Pulling upgrader image", "image", ArcaneUpgraderImage)
	pullReader, err := dockerClient.ImagePull(ctx, ArcaneUpgraderImage, imagetypes.PullOptions{})
	if err != nil {
		return fmt.Errorf("pull upgrader image: %w", err)
	}
	// Drain the reader to complete the pull
	_, _ = io.Copy(io.Discard, pullReader)
	pullReader.Close()
	slog.Info("Upgrader image pulled successfully", "image", ArcaneUpgraderImage)

	// Try to get the /app/data mount from current container so upgrade logs persist.
	appDataMount := dockerutils.MountForDestination(currentContainer.Mounts, "/app/data", "/app/data")
	if appDataMount == nil {
		slog.Warn("Could not detect /app/data mount; upgrader logs may not persist")
	} else {
		slog.Debug("Mounting /app/data into upgrader container", "type", appDataMount.Type, "source", appDataMount.Source)
	}

	// Create the upgrader container config
	config := &containertypes.Config{
		Image: ArcaneUpgraderImage,
		Cmd:   []string{"/app/arcane", "upgrade", "--container", containerName},
		Labels: map[string]string{
			"com.getarcaneapp.arcane.upgrader": "true",
			"com.getarcaneapp.arcane":          "true",
		},
	}

	mounts := []mounttypes.Mount{
		{Type: mounttypes.TypeBind, Source: "/var/run/docker.sock", Target: "/var/run/docker.sock"},
	}
	if appDataMount != nil {
		mounts = append(mounts, *appDataMount)
	}

	keepUpgraderContainer := strings.EqualFold(strings.TrimSpace(os.Getenv("ARCANE_UPGRADE_KEEP_CONTAINER")), "true")
	if keepUpgraderContainer {
		slog.Info("Keeping upgrader container after exit (ARCANE_UPGRADE_KEEP_CONTAINER=true)")
	}

	hostConfig := &containertypes.HostConfig{
		AutoRemove: !keepUpgraderContainer, // default: clean up after completion
		Mounts:     mounts,
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
	id, err := dockerutils.GetCurrentContainerID()
	if err != nil {
		return "", ErrNotRunningInDocker
	}
	return id, nil
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
