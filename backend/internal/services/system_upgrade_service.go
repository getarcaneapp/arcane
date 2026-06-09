package services

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"sync/atomic"
	"time"

	ref "github.com/distribution/reference"
	"github.com/getarcaneapp/arcane/backend/v2/internal/common"
	"github.com/getarcaneapp/arcane/backend/v2/internal/models"
	dockerutils "github.com/getarcaneapp/arcane/backend/v2/pkg/dockerutil"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/libarcane"
	arcaneselector "github.com/getarcaneapp/arcane/backend/v2/pkg/libarcane/selector"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/libarcane/timeouts"
	containertypes "github.com/moby/moby/api/types/container"
	mounttypes "github.com/moby/moby/api/types/mount"
	"github.com/moby/moby/client"
	libupdater "go.getarcane.app/updater/pkg/labels"
	"golang.org/x/mod/semver"
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
	// Verify we can access Docker
	_, err := s.dockerService.GetClient(ctx)
	if err != nil {
		return false, &common.DockerSocketAccessError{}
	}

	// Verify we can find our container
	containerId, _ := s.getCurrentContainerIDInternal()
	_, err = s.findArcaneContainerInternal(ctx, containerId)
	if err != nil {
		return false, err
	}

	return true, nil
}

// TriggerUpgradeViaCLI spawns the upgrade CLI command in a separate container
// This avoids self-termination issues by running the upgrade from outside
func (s *SystemUpgradeService) TriggerUpgradeViaCLI(ctx context.Context, user models.User) error {
	if !s.upgrading.CompareAndSwap(false, true) {
		return &common.UpgradeInProgressError{}
	}
	defer s.upgrading.Store(false)

	// Current-container ID detection can fail under some container runtimes, so
	// treat it as a best-effort fast path and fall back to Arcane labels.
	containerId, currentContainerIDErr := s.getCurrentContainerIDInternal()
	if currentContainerIDErr != nil {
		slog.Debug("Failed to detect current container ID for upgrade; falling back to labels", "error", currentContainerIDErr)
	}

	currentContainer, err := s.findArcaneContainerInternal(ctx, containerId)
	if err != nil {
		return fmt.Errorf("inspect container: %w", err)
	}
	if strings.TrimSpace(containerId) == "" {
		containerId = currentContainer.ID
	}

	containerName := strings.TrimPrefix(currentContainer.Name, "/")

	// Determine binary path based on container type (agent vs main)
	binaryPath := "/app/arcane"
	if currentContainer.Config != nil {
		binaryPath = determineUpgradeBinaryPathInternal(currentContainer.Config.Labels)
	}

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
	upgraderImage := defaultArcaneUpgraderImageInternal
	if currentContainer.Config != nil {
		if img := strings.TrimSpace(currentContainer.Config.Image); img != "" {
			upgraderImage = img
		}
	}
	targetImage := s.resolveSystemUpgradeTargetImageInternal(ctx, currentContainer)
	slog.Debug("Using upgrader image", "image", upgraderImage, "targetImage", targetImage)

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
		Env:   runtimeOptions.ContainerEnv,
		Labels: map[string]string{
			arcaneselector.UpgraderLabel: "true",
			"com.getarcaneapp.arcane":    "true",
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

func determineUpgradeBinaryPathInternal(labels map[string]string) string {
	if libupdater.IsArcaneAgentContainer(labels) {
		return "/app/arcane-agent"
	}

	return "/app/arcane"
}

func (s *SystemUpgradeService) resolveSystemUpgradeTargetImageInternal(ctx context.Context, currentContainer containertypes.InspectResponse) string {
	if s == nil || s.versionService == nil || currentContainer.Config == nil {
		return ""
	}

	latestVersion, err := s.versionService.GetLatestVersion(ctx)
	if err != nil {
		slog.WarnContext(ctx, "Failed to resolve latest release for self-upgrade target image", "error", err)
		return ""
	}

	return resolveSystemUpgradeTargetImageInternal(currentContainer.Config.Image, latestVersion)
}

func resolveSystemUpgradeTargetImageInternal(currentImage, latestVersion string) string {
	currentImage = strings.TrimSpace(currentImage)
	if currentImage == "" || strings.Contains(currentImage, "@") {
		return ""
	}

	latestSemver := normalizeSystemUpgradeSemverInternal(latestVersion)
	if !semver.IsValid(latestSemver) {
		return ""
	}

	named, err := ref.ParseNormalizedNamed(currentImage)
	if err != nil {
		return ""
	}

	tagged, ok := named.(ref.Tagged)
	if !ok {
		return ""
	}

	currentSemver := normalizeSystemUpgradeSemverInternal(tagged.Tag())
	if !semver.IsValid(currentSemver) || semver.Compare(latestSemver, currentSemver) <= 0 {
		return ""
	}

	target, err := ref.WithTag(named, latestSemver)
	if err != nil {
		return ""
	}
	return target.String()
}

func normalizeSystemUpgradeSemverInternal(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}

	if !strings.HasPrefix(value, "v") {
		if value[0] < '0' || value[0] > '9' {
			return value
		}
		value = "v" + value
	}
	if semver.IsValid(value) {
		return value
	}

	core := value
	suffix := ""
	if idx := strings.IndexAny(value, "-+"); idx > 0 {
		core = value[:idx]
		suffix = value[idx:]
	}

	parts := strings.Split(strings.TrimPrefix(core, "v"), ".")
	for _, part := range parts {
		if part == "" {
			return value
		}
		for _, ch := range part {
			if ch < '0' || ch > '9' {
				return value
			}
		}
	}

	for len(parts) < 3 {
		parts = append(parts, "0")
	}
	normalized := "v" + strings.Join(parts[:3], ".") + suffix
	if semver.IsValid(normalized) {
		return normalized
	}
	return value
}

func resolveSystemUpgraderRuntimeOptionsInternal(
	ctx context.Context,
	dockerHost string,
	currentContainer *containertypes.InspectResponse,
	discoverHostPath func(context.Context, string) (string, error),
	isRunningInDocker func() bool,
) (upgraderRuntimeOptionsInternal, error) {
	options := upgraderRuntimeOptionsInternal{
		ContainerEnv: buildTrivyDockerHostEnvInternal(dockerHost),
	}

	scheme, socketPath, err := parseTrivyDockerHostInternal(dockerHost)
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
	containerId = strings.TrimSpace(containerId)
	if containerId != "" {
		container, err := libarcane.ContainerInspectWithCompatibility(ctx, dockerClient, containerId, client.ContainerInspectOptions{})
		if err == nil {
			return container.Container, nil
		}
	}

	// Fall back to all containers so aliases like ghcr.io/getarcaneapp/manager
	// and mixed manager/agent deployments are handled by Arcane labels.
	allContainers, err := dockerClient.ContainerList(ctx, client.ContainerListOptions{All: true})
	if err != nil {
		return containertypes.InspectResponse{}, err
	}

	if containerId != "" {
		for _, c := range allContainers.Items {
			if strings.HasPrefix(c.ID, containerId) || c.ID == containerId {
				inspect, inspectErr := libarcane.ContainerInspectWithCompatibility(ctx, dockerClient, c.ID, client.ContainerInspectOptions{})
				if inspectErr != nil {
					return containertypes.InspectResponse{}, inspectErr
				}
				return inspect.Container, nil
			}
		}
	}

	if selected, ok := arcaneselector.SelectArcaneContainerSummary(allContainers.Items); ok {
		inspect, inspectErr := libarcane.ContainerInspectWithCompatibility(ctx, dockerClient, selected.ID, client.ContainerInspectOptions{})
		if inspectErr != nil {
			return containertypes.InspectResponse{}, inspectErr
		}
		return inspect.Container, nil
	}

	return containertypes.InspectResponse{}, &common.ArcaneContainerNotFoundError{}
}
