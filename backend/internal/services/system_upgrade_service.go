package services

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"slices"
	"strings"
	"sync/atomic"
	"time"

	"github.com/getarcaneapp/arcane/backend/v2/internal/common"
	"github.com/getarcaneapp/arcane/backend/v2/internal/models"
	dockerutils "github.com/getarcaneapp/arcane/backend/v2/pkg/dockerutil"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/libarcane"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/libarcane/timeouts"
	vuln "github.com/getarcaneapp/arcane/backend/v2/pkg/libarcane/vuln"
	containertypes "github.com/moby/moby/api/types/container"
	mounttypes "github.com/moby/moby/api/types/mount"
	"github.com/moby/moby/client"
	libupdater "go.getarcane.app/updater/pkg/labels"
	updatertypes "go.getarcane.app/updater/types"
)

const defaultArcaneUpgraderImageInternal = "ghcr.io/getarcaneapp/arcane:latest"

type SystemUpgradeService struct {
	upgrading       atomic.Bool
	dockerService   *DockerClientService
	versionService  *VersionService
	eventService    *EventService
	settingsService *SettingsService
}

type upgraderRuntimeOptionsInternal struct {
	ContainerEnv []string
	Mounts       []mounttypes.Mount
	NetworkMode  containertypes.NetworkMode
}

func NewSystemUpgradeService(
	dockerService *DockerClientService,
	versionService *VersionService,
	eventService *EventService,
	settingsService *SettingsService,
) *SystemUpgradeService {
	return &SystemUpgradeService{
		dockerService:   dockerService,
		versionService:  versionService,
		eventService:    eventService,
		settingsService: settingsService,
	}
}

// CanUpgrade checks if self-upgrade is possible
func (s *SystemUpgradeService) CanUpgrade(ctx context.Context) (bool, error) {
	// Check if running in Docker
	containerId, err := s.getCurrentContainerIDInternal()
	if err != nil {
		return false, err
	}

	// Verify we can access Docker
	_, err = s.dockerService.GetClient(ctx)
	if err != nil {
		return false, &common.DockerSocketAccessError{}
	}

	// Verify we can find our container
	_, err = s.findArcaneContainerInternal(ctx, containerId)
	if err != nil {
		return false, err
	}

	return true, nil
}

// TriggerUpgradeViaCLI spawns the upgrade CLI command in a separate container
// This avoids self-termination issues by running the upgrade from outside.
// A zero-value target upgrades the current container to its own image tag;
// the updater engine passes an explicit target with the resolved new image.
func (s *SystemUpgradeService) TriggerUpgradeViaCLI(ctx context.Context, user models.User, target updatertypes.SelfUpdateTarget) error {
	if !s.upgrading.CompareAndSwap(false, true) {
		return &common.UpgradeInProgressError{}
	}
	defer s.upgrading.Store(false)

	containerId := strings.TrimSpace(target.ContainerID)
	if containerId == "" {
		// Fall back to the container this process runs in
		var err error
		containerId, err = s.getCurrentContainerIDInternal()
		if err != nil {
			return fmt.Errorf("get current container: %w", err)
		}
	}

	currentContainer, err := s.findArcaneContainerInternal(ctx, containerId)
	if err != nil {
		return fmt.Errorf("inspect container: %w", err)
	}

	containerName := strings.TrimPrefix(currentContainer.Name, "/")

	// Determine binary path based on container type (agent vs main)
	binaryPath := "/app/arcane"
	if currentContainer.Config != nil {
		binaryPath = determineUpgradeBinaryPathInternal(currentContainer.Config.Labels)
	}

	targetImage := strings.TrimSpace(target.NewImageRef)

	// Log upgrade event
	metadata := models.JSON{
		"action":        "system_upgrade_cli",
		"containerId":   containerId,
		"containerName": containerName,
		"method":        "cli",
		"targetImage":   targetImage,
	}
	if err := s.eventService.LogUserEvent(ctx, models.EventTypeSystemUpgrade, user.ID, user.Username, metadata); err != nil {
		slog.Warn("Failed to log upgrade event", "error", err)
	}

	// Run the upgrader from the image we are upgrading to, so the upgrade CLI
	// is the new version. Without a resolved target image, fall back to the
	// running container's own image reference.
	upgraderImage := targetImage
	if upgraderImage == "" {
		upgraderImage = defaultArcaneUpgraderImageInternal
		if currentContainer.Config != nil {
			if img := strings.TrimSpace(currentContainer.Config.Image); img != "" {
				upgraderImage = img
			}
		}
	}
	slog.Debug("Using upgrader image", "image", upgraderImage)

	slog.Info("Spawning upgrade CLI command", "containerName", containerName, "upgraderImage", upgraderImage)

	// Spawn the upgrade command in a detached container
	// This will run independently of the current container
	dockerClient, err := s.dockerService.GetClient(ctx)
	if err != nil {
		return fmt.Errorf("failed to connect to Docker: %w", err)
	}

	// Pull the upgrader image first to ensure it exists
	slog.Info("Pulling upgrader image", "image", upgraderImage)

	settings := s.settingsService.GetSettingsConfig()
	pullCtx, pullCancel := timeouts.WithTimeout(ctx, settings.DockerImagePullTimeout.AsInt(), timeouts.DefaultDockerImagePull)
	defer pullCancel()

	pullReader, err := dockerClient.ImagePull(pullCtx, upgraderImage, client.ImagePullOptions{})
	if err != nil {
		if errors.Is(pullCtx.Err(), context.DeadlineExceeded) {
			return fmt.Errorf("upgrader image pull timed out for %s (increase DOCKER_IMAGE_PULL_TIMEOUT or setting)", upgraderImage)
		}
		return fmt.Errorf("pull upgrader image: %w", err)
	}
	// Drain and validate the JSON stream to complete the pull.
	if err := dockerutils.ConsumeJSONMessageStream(pullReader, nil); err != nil {
		_ = pullReader.Close()
		return fmt.Errorf("failed to complete upgrader image pull: %w", err)
	}
	if closeErr := pullReader.Close(); closeErr != nil {
		slog.Warn("Failed to close upgrader image pull reader", "error", closeErr)
	}
	slog.Info("Upgrader image pulled successfully", "image", upgraderImage)

	// Try to get the /app/data mount from current container so upgrade logs persist.
	appDataMount := dockerutils.MountForDestination(currentContainer.Mounts, "/app/data", "/app/data")
	if appDataMount == nil {
		slog.Warn("Could not detect /app/data mount; upgrader logs may not persist")
	} else {
		slog.Debug("Mounting /app/data into upgrader container", "type", appDataMount.Type, "source", appDataMount.Source)
	}

	// Create the upgrader container config
	runtimeOptions, err := resolveSystemUpgraderRuntimeOptionsInternal(
		ctx,
		s.dockerService.DockerHost(),
		&currentContainer,
		func(ctx context.Context, containerPath string) (string, error) {
			return dockerutils.GetHostPathForContainerPath(ctx, dockerClient, containerPath)
		},
		func() bool {
			_, err := dockerutils.GetCurrentContainerID()
			return err == nil
		},
	)
	if err != nil {
		return fmt.Errorf("resolve upgrader docker runtime: %w", err)
	}

	upgradeCmd := []string{binaryPath, "upgrade", "--container", containerName}
	if targetImage != "" {
		upgradeCmd = append(upgradeCmd, "--image", targetImage)
	}

	config := &containertypes.Config{
		Image: upgraderImage,
		Cmd:   upgradeCmd,
		// The upgrader needs root for the Docker socket; unlike the server it
		// is short-lived and never goes through the runtime-identity drop, so
		// don't rely on the image's default user.
		User: "0:0",
		Env:  runtimeOptions.ContainerEnv,
		Labels: map[string]string{
			"com.getarcaneapp.arcane.upgrader": "true",
			"com.getarcaneapp.arcane":          "true",
		},
	}

	mounts := append([]mounttypes.Mount{}, runtimeOptions.Mounts...)
	if appDataMount != nil {
		mounts = append(mounts, *appDataMount)
	}

	keepUpgraderContainer := strings.EqualFold(strings.TrimSpace(os.Getenv("ARCANE_UPGRADE_KEEP_CONTAINER")), "true")
	if keepUpgraderContainer {
		slog.Info("Keeping upgrader container after exit (ARCANE_UPGRADE_KEEP_CONTAINER=true)")
	}

	hostConfig := &containertypes.HostConfig{
		AutoRemove:  !keepUpgraderContainer, // default: clean up after completion
		Mounts:      mounts,
		NetworkMode: runtimeOptions.NetworkMode,
	}
	// Inherit the security context that lets the running Arcane container reach
	// the Docker socket (e.g. SELinux label=disable, privileged); the upgrader
	// needs the same access on hardened hosts.
	if currentContainer.HostConfig != nil {
		hostConfig.SecurityOpt = slices.Clone(currentContainer.HostConfig.SecurityOpt)
		hostConfig.Privileged = currentContainer.HostConfig.Privileged
	}
	// On SELinux-enforcing hosts the socket carries container_var_run_t, which
	// container processes cannot connect to regardless of UID; without an
	// explicit label opt the upgrader would exit with EACCES and auto-remove.
	if !hostConfig.Privileged && !hasSELinuxLabelOptInternal(hostConfig.SecurityOpt) && daemonHasSELinuxEnabledInternal(ctx, dockerClient) {
		hostConfig.SecurityOpt = append(hostConfig.SecurityOpt, "label=disable")
	}

	containerName = fmt.Sprintf("%s-upgrader-%d", containerName, time.Now().Unix())

	resp, err := dockerClient.ContainerCreate(ctx, client.ContainerCreateOptions{
		Config:     config,
		HostConfig: hostConfig,
		Name:       containerName,
	})
	if err != nil {
		return fmt.Errorf("create upgrader container: %w", err)
	}

	// Start the upgrader container - it will run the upgrade and auto-remove
	if _, err := dockerClient.ContainerStart(ctx, resp.ID, client.ContainerStartOptions{}); err != nil {
		_, _ = dockerClient.ContainerRemove(ctx, resp.ID, client.ContainerRemoveOptions{Force: true})
		return fmt.Errorf("start upgrader container: %w", err)
	}

	slog.Info("Upgrade container started", "upgraderId", resp.ID[:12], "upgraderName", containerName)

	return nil
}

// hasSELinuxLabelOptInternal reports whether the security options already set
// an SELinux label policy (e.g. "label=disable", "label:disable", "label=type:...").
func hasSELinuxLabelOptInternal(securityOpts []string) bool {
	for _, opt := range securityOpts {
		if strings.HasPrefix(strings.TrimSpace(opt), "label") {
			return true
		}
	}
	return false
}

func daemonHasSELinuxEnabledInternal(ctx context.Context, dockerClient *client.Client) bool {
	infoResult, err := dockerClient.Info(ctx, client.InfoOptions{})
	if err != nil {
		slog.Debug("Failed to query daemon info for SELinux detection", "error", err)
		return false
	}
	return slices.Contains(infoResult.Info.SecurityOptions, "name=selinux")
}

func determineUpgradeBinaryPathInternal(labels map[string]string) string {
	if libupdater.IsArcaneAgentContainer(labels) {
		return "/app/arcane-agent"
	}

	return "/app/arcane"
}

func resolveSystemUpgraderRuntimeOptionsInternal(
	ctx context.Context,
	dockerHost string,
	currentContainer *containertypes.InspectResponse,
	discoverHostPath func(context.Context, string) (string, error),
	isRunningInDocker func() bool,
) (upgraderRuntimeOptionsInternal, error) {
	options := upgraderRuntimeOptionsInternal{
		ContainerEnv: vuln.BuildDockerHostEnv(dockerHost),
	}

	scheme, socketPath, err := vuln.ParseDockerHost(dockerHost)
	if err != nil {
		return upgraderRuntimeOptionsInternal{}, fmt.Errorf("resolve docker host %q: %w", dockerHost, err)
	}

	if scheme != "unix" {
		options.NetworkMode = containertypes.NetworkMode(selectTrivyAutoNetworkModeInternal(currentContainer))
		return options, nil
	}

	socketSource, err := resolveTrivyUnixSocketSourceInternal(
		ctx,
		socketPath,
		discoverHostPath,
		isRunningInDocker,
	)
	if err != nil {
		return upgraderRuntimeOptionsInternal{}, fmt.Errorf("resolve unix socket source: %w", err)
	}

	options.Mounts = append(options.Mounts, mounttypes.Mount{
		Type:   mounttypes.TypeBind,
		Source: socketSource,
		Target: socketPath,
	})

	return options, nil
}

// getCurrentContainerID detects if we're running in Docker and returns container ID
func (s *SystemUpgradeService) getCurrentContainerIDInternal() (string, error) {
	id, err := dockerutils.GetCurrentContainerID()
	if err != nil {
		return "", &common.NotRunningInDockerError{}
	}
	return id, nil
}

// findArcaneContainer finds the container using the ID
func (s *SystemUpgradeService) findArcaneContainerInternal(ctx context.Context, containerId string) (containertypes.InspectResponse, error) {
	dockerClient, err := s.dockerService.GetClient(ctx)
	if err != nil {
		return containertypes.InspectResponse{}, err
	}

	// Try to inspect the container directly
	container, err := libarcane.ContainerInspectWithCompatibility(ctx, dockerClient, containerId, client.ContainerInspectOptions{})
	if err == nil {
		return container.Container, nil
	}

	// Fallback: search for containers with arcane image
	filter := make(client.Filters)
	filter = filter.Add("ancestor", "ghcr.io/getarcaneapp/arcane")

	containers, err := dockerClient.ContainerList(ctx, client.ContainerListOptions{
		All:     true,
		Filters: filter,
	})
	if err != nil {
		return containertypes.InspectResponse{}, err
	}

	for _, c := range containers.Items {
		if strings.HasPrefix(c.ID, containerId) {
			inspect, inspectErr := libarcane.ContainerInspectWithCompatibility(ctx, dockerClient, c.ID, client.ContainerInspectOptions{})
			if inspectErr != nil {
				return containertypes.InspectResponse{}, inspectErr
			}
			return inspect.Container, nil
		}
	}

	// Try without filter - search all containers
	allContainers, err := dockerClient.ContainerList(ctx, client.ContainerListOptions{All: true})
	if err != nil {
		return containertypes.InspectResponse{}, err
	}

	for _, c := range allContainers.Items {
		if strings.HasPrefix(c.ID, containerId) || c.ID == containerId {
			inspect, inspectErr := libarcane.ContainerInspectWithCompatibility(ctx, dockerClient, c.ID, client.ContainerInspectOptions{})
			if inspectErr != nil {
				return containertypes.InspectResponse{}, inspectErr
			}
			return inspect.Container, nil
		}
	}

	return containertypes.InspectResponse{}, &common.ArcaneContainerNotFoundError{}
}
