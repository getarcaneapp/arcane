package upgrade

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"strings"
	"time"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/filters"
	"github.com/docker/docker/api/types/image"
	"github.com/docker/docker/api/types/network"
	"github.com/docker/docker/client"
	"github.com/getarcaneapp/arcane/backend/internal/utils/arcaneupdater"
	"github.com/getarcaneapp/arcane/backend/internal/utils/docker"
	"github.com/spf13/cobra"
)

var (
	containerName string
	targetImage   string
	autoDetect    bool
)

var UpgradeCmd = &cobra.Command{
	Use:   "upgrade",
	Short: "Upgrade an Arcane container to the latest version",
	Long: `Upgrade an Arcane container by pulling the latest image and recreating the container.
This command should be run from outside the container (e.g., from the host or another container).`,
	Example: `  # Auto-detect and upgrade the Arcane container
  arcane upgrade --auto

  # Upgrade a specific container
  arcane upgrade --container arcane

  # Upgrade to a specific image tag
  arcane upgrade --container arcane --image ghcr.io/getarcaneapp/arcane:v1.2.3`,
	// Use background context to ignore signals during upgrade
	// This prevents the upgrade from being interrupted when the target container stops
	RunE: runUpgrade,
}

func init() {
	UpgradeCmd.Flags().StringVarP(&containerName, "container", "c", "", "Name of the container to upgrade")
	UpgradeCmd.Flags().StringVarP(&targetImage, "image", "i", "", "Target image to upgrade to (defaults to current tag)")
	UpgradeCmd.Flags().BoolVarP(&autoDetect, "auto", "a", false, "Auto-detect Arcane container")
}

func runUpgrade(cmd *cobra.Command, args []string) error {
	// Use background context instead of command context to ignore signals
	// This prevents interruption when stopping the target container
	ctx := context.Background()

	logFile, err := arcaneupdater.SetupMessageOnlyLogFile("/app/data", "arcane-upgrade", slog.LevelInfo)
	if err != nil {
		slog.Warn("Failed to setup file logging", "error", err)
	} else if logFile != nil {
		defer func() {
			if err := logFile.Close(); err != nil {
				slog.Warn("Failed to close upgrade log file", "error", err, "path", logFile.Name())
			}
		}()
		slog.Info("Upgrade log file created", "path", logFile.Name())
	}

	// Connect to Docker
	dockerClient, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	if err != nil {
		return fmt.Errorf("failed to connect to Docker: %w", err)
	}
	defer dockerClient.Close()

	// Find the container
	var targetContainer container.InspectResponse
	if autoDetect || containerName == "" {
		slog.Info("Auto-detecting Arcane container...")
		targetContainer, err = findArcaneContainer(ctx, dockerClient)
		if err != nil {
			return fmt.Errorf("failed to find Arcane container: %w", err)
		}
		containerName = strings.TrimPrefix(targetContainer.Name, "/")
		slog.Info("Found Arcane container", "name", containerName, "id", targetContainer.ID[:12])
	} else {
		targetContainer, err = dockerClient.ContainerInspect(ctx, containerName)
		if err != nil {
			return fmt.Errorf("failed to inspect container %s: %w", containerName, err)
		}
	}

	// Determine image to pull
	imageToPull := targetImage
	if imageToPull == "" {
		imageToPull = determineImageName(ctx, dockerClient, targetContainer)
		slog.Info("Determined image to pull", "image", imageToPull)
	}

	// Pull the new image
	slog.Info("Pulling new image", "image", imageToPull)
	if err := pullImage(ctx, dockerClient, imageToPull); err != nil {
		return fmt.Errorf("failed to pull image: %w", err)
	}

	// Perform the upgrade
	slog.Info("Starting container upgrade", "container", containerName)
	if err := upgradeContainer(ctx, dockerClient, targetContainer, imageToPull); err != nil {
		return fmt.Errorf("failed to upgrade container: %w", err)
	}

	slog.Info("Upgrade completed successfully", "container", containerName, "image", imageToPull)
	return nil
}

func findArcaneContainer(ctx context.Context, dockerClient *client.Client) (container.InspectResponse, error) {
	// Prefer explicit Arcane labels; fall back to image name heuristics.
	filter := filters.NewArgs()
	filter.Add("status", "running")

	containers, err := dockerClient.ContainerList(ctx, container.ListOptions{Filters: filter})
	if err != nil {
		return container.InspectResponse{}, err
	}

	selfID, _ := docker.GetCurrentContainerID()
	slog.Info("Searching for Arcane container", "selfID", selfID, "totalContainers", len(containers))

	now := time.Now()

	for _, c := range containers {
		if shouldSkipContainer(c, selfID, now) {
			continue
		}

		inspect, err := dockerClient.ContainerInspect(ctx, c.ID)
		if err != nil {
			continue
		}

		if isAgentContainer(inspect) {
			slog.Debug("Skipping agent container", "id", c.ID[:12], "names", c.Names)
			continue
		}

		labels := map[string]string{}
		if inspect.Config != nil && inspect.Config.Labels != nil {
			labels = inspect.Config.Labels
		}

		// New label: com.getarcaneapp.arcane=true
		if arcaneupdater.IsArcaneContainer(labels) {
			slog.Info("Found Arcane container by label", "id", c.ID[:12], "image", c.Image, "names", c.Names)
			return inspect, nil
		}

		// Legacy label (pre-migration): com.getarcaneapp.arcane.server=true
		// NOTE: older agent images also used this label, so we must additionally exclude AGENT_MODE=true.
		if isLegacyServerLabel(labels) {
			slog.Info("Found Arcane container by legacy label", "id", c.ID[:12], "image", c.Image, "names", c.Names)
			return inspect, nil
		}

		// Fallback: image name heuristic
		if strings.Contains(strings.ToLower(c.Image), "arcane") {
			slog.Info("Found matching container by image name", "id", c.ID[:12], "image", c.Image, "names", c.Names)
			return inspect, nil
		}
	}

	return container.InspectResponse{}, fmt.Errorf("no running Arcane container found")
}

func isLegacyServerLabel(labels map[string]string) bool {
	if labels == nil {
		return false
	}
	for k, v := range labels {
		if strings.EqualFold(k, "com.getarcaneapp.arcane.server") {
			return strings.EqualFold(strings.TrimSpace(v), "true")
		}
	}
	return false
}

func isAgentContainer(inspect container.InspectResponse) bool {
	if inspect.Config == nil {
		return false
	}
	// New label for agent containers
	if inspect.Config.Labels != nil {
		for k, v := range inspect.Config.Labels {
			if strings.EqualFold(k, "com.getarcaneapp.arcane.agent") {
				return strings.EqualFold(strings.TrimSpace(v), "true")
			}
		}
	}
	// Legacy agent detection: AGENT_MODE=true in env
	for _, env := range inspect.Config.Env {
		if strings.EqualFold(env, "AGENT_MODE=true") {
			return true
		}
	}
	return false
}

// shouldSkipContainer determines if a container should be skipped during search
func shouldSkipContainer(c container.Summary, selfID string, now time.Time) bool {
	// Skip ourselves (the upgrader container) by ID
	if selfID != "" && strings.HasPrefix(c.ID, selfID) {
		slog.Info("Skipping self by ID", "id", c.ID[:12], "names", c.Names)
		return true
	}

	// Skip very recently created containers (likely the upgrader)
	if c.Created > 0 {
		createdTime := time.Unix(c.Created, 0)
		age := now.Sub(createdTime)
		if age < 30*time.Second {
			slog.Info("Skipping recently created container", "id", c.ID[:12], "age", age, "names", c.Names)
			return true
		}
	}

	// Skip containers with "upgrader" in the name
	for _, name := range c.Names {
		if strings.Contains(strings.ToLower(name), "upgrader") {
			slog.Info("Skipping upgrader container by name", "name", name)
			return true
		}
	}

	return false
}

func determineImageName(ctx context.Context, dockerClient *client.Client, cont container.InspectResponse) string {
	imageName := extractImageNameFromConfig(cont)
	imageName = stripDigest(imageName)

	// If no explicit tag, try to infer from image RepoTags
	if !hasExplicitTag(imageName) {
		if inferredName := inferImageNameFromDocker(ctx, dockerClient, cont.Image); inferredName != "" {
			imageName = inferredName
		}
	}

	// Default to :latest if still no tag
	if !hasExplicitTag(imageName) {
		imageName = ensureDefaultTag(imageName)
	}

	return imageName
}

// extractImageNameFromConfig gets the image name from container config
func extractImageNameFromConfig(cont container.InspectResponse) string {
	if cont.Config == nil {
		return ""
	}
	return strings.TrimSpace(cont.Config.Image)
}

// stripDigest removes digest from image reference
func stripDigest(imageName string) string {
	if idx := strings.Index(imageName, "@"); idx != -1 {
		return imageName[:idx]
	}
	return imageName
}

// hasExplicitTag checks if image reference has a tag
func hasExplicitTag(ref string) bool {
	if ref == "" {
		return false
	}
	slash := strings.LastIndex(ref, "/")
	colon := strings.LastIndex(ref, ":")
	return colon > slash
}

// inferImageNameFromDocker attempts to find the best tag from Docker image inspect
func inferImageNameFromDocker(ctx context.Context, dockerClient *client.Client, imageID string) string {
	ii, err := dockerClient.ImageInspect(ctx, imageID)
	if err != nil {
		return ""
	}

	var arcaneNonLatest string
	var arcaneAny string

	for _, t := range ii.RepoTags {
		if t == "" || t == "<none>:<none>" {
			continue
		}

		t = stripDigest(t)

		if strings.Contains(t, "arcane") {
			if arcaneAny == "" {
				arcaneAny = t
			}
			if !strings.HasSuffix(t, ":latest") && arcaneNonLatest == "" {
				arcaneNonLatest = t
			}
		}
	}

	// Prefer non-latest tags
	if arcaneNonLatest != "" {
		return arcaneNonLatest
	}
	return arcaneAny
}

// ensureDefaultTag adds :latest tag if no tag is present
func ensureDefaultTag(imageName string) string {
	if imageName == "" {
		return "ghcr.io/getarcaneapp/arcane:latest"
	}
	return imageName + ":latest"
}

func pullImage(ctx context.Context, dockerClient *client.Client, imageName string) error {
	reader, err := dockerClient.ImagePull(ctx, imageName, image.PullOptions{})
	if err != nil {
		return err
	}
	defer reader.Close()

	// Copy output to discard but wait for completion
	_, err = io.Copy(io.Discard, reader)
	return err
}

func upgradeContainer(ctx context.Context, dockerClient *client.Client, oldContainer container.InspectResponse, newImage string) error {
	originalName := strings.TrimPrefix(oldContainer.Name, "/")
	oldName := fmt.Sprintf("%s-old-%d", originalName, time.Now().UnixNano())

	// Create new container config
	config := *oldContainer.Config
	config.Image = newImage

	hostConfig := oldContainer.HostConfig

	// Fix for "conflicting options: hostname and the network mode"
	// When network mode is "host" or "container:...", Hostname must be empty
	nm := hostConfig.NetworkMode
	if nm.IsHost() || nm.IsContainer() {
		config.Hostname = ""
		config.Domainname = ""
	}

	// Clear hostname if it looks like a container ID (auto-generated by Docker)
	// This allows Docker to assign the new container's ID as the hostname
	if looksLikeContainerID(config.Hostname) {
		slog.Debug("Clearing auto-generated hostname", "oldHostname", config.Hostname)
		config.Hostname = ""
	}

	// Fix for "conflicting options: port exposing and the container type network mode"
	// When network mode is "container:...", port mappings are not allowed
	if nm.IsContainer() {
		config.ExposedPorts = nil
		hostConfig.PortBindings = nil
		hostConfig.PublishAllPorts = false
	}

	// Build network config - preserve all network settings including IP addresses
	var networkConfig *network.NetworkingConfig
	if !nm.IsContainer() {
		networkConfig = &network.NetworkingConfig{
			EndpointsConfig: make(map[string]*network.EndpointSettings),
		}
		for networkName, networkSettings := range oldContainer.NetworkSettings.Networks {
			networkConfig.EndpointsConfig[networkName] = &network.EndpointSettings{
				IPAMConfig:          networkSettings.IPAMConfig,
				Links:               networkSettings.Links,
				Aliases:             networkSettings.Aliases,
				NetworkID:           networkSettings.NetworkID,
				EndpointID:          networkSettings.EndpointID,
				Gateway:             networkSettings.Gateway,
				IPAddress:           networkSettings.IPAddress,
				IPPrefixLen:         networkSettings.IPPrefixLen,
				IPv6Gateway:         networkSettings.IPv6Gateway,
				GlobalIPv6Address:   networkSettings.GlobalIPv6Address,
				GlobalIPv6PrefixLen: networkSettings.GlobalIPv6PrefixLen,
				MacAddress:          networkSettings.MacAddress,
				DriverOpts:          networkSettings.DriverOpts,
			}
		}
	}

	fmt.Println("PROGRESS:65:Renaming old container")
	slog.Info("Renaming old container", "from", originalName, "to", oldName)
	if err := dockerClient.ContainerRename(ctx, oldContainer.ID, oldName); err != nil {
		return fmt.Errorf("rename old container: %w", err)
	}

	fmt.Println("PROGRESS:70:Stopping old container")
	slog.Info("Stopping old container", "name", oldName)
	timeout := 10
	if err := dockerClient.ContainerStop(ctx, oldContainer.ID, container.StopOptions{Timeout: &timeout}); err != nil {
		_ = dockerClient.ContainerRename(ctx, oldContainer.ID, originalName)
		return fmt.Errorf("stop old container: %w", err)
	}

	fmt.Println("PROGRESS:75:Creating new container")
	slog.Info("Creating new container", "name", originalName)
	resp, err := dockerClient.ContainerCreate(ctx, &config, hostConfig, networkConfig, nil, originalName)
	if err != nil {
		// Try to restart and restore old container on failure
		_ = dockerClient.ContainerStart(ctx, oldContainer.ID, container.StartOptions{})
		_ = dockerClient.ContainerRename(ctx, oldContainer.ID, originalName)
		return fmt.Errorf("create new container: %w", err)
	}

	fmt.Println("PROGRESS:80:Starting new container")
	slog.Info("Starting new container", "id", resp.ID[:12])
	if err := dockerClient.ContainerStart(ctx, resp.ID, container.StartOptions{}); err != nil {
		// Cleanup new container and restart old one
		_ = dockerClient.ContainerRemove(ctx, resp.ID, container.RemoveOptions{Force: true})
		_ = dockerClient.ContainerStart(ctx, oldContainer.ID, container.StartOptions{})
		_ = dockerClient.ContainerRename(ctx, oldContainer.ID, originalName)
		return fmt.Errorf("start new container: %w", err)
	}

	// Wait a moment for the new container to initialize
	// Wait a moment for the new container to initialize
	fmt.Println("PROGRESS:85:Waiting for container to start")
	time.Sleep(2 * time.Second)

	fmt.Println("PROGRESS:90:Removing old container")
	slog.Info("Removing old container", "id", oldContainer.ID[:12])
	if err := dockerClient.ContainerRemove(ctx, oldContainer.ID, container.RemoveOptions{}); err != nil {
		slog.Warn("Failed to remove old container", "error", err)
	}

	fmt.Println("PROGRESS:95:Upgrade complete")

	return nil
}

// looksLikeContainerID checks if a string looks like a Docker container ID
// (12 or 64 lowercase hex characters, which Docker auto-generates as hostnames)
func looksLikeContainerID(s string) bool {
	if len(s) != 12 && len(s) != 64 {
		return false
	}
	for _, c := range s {
		if (c < '0' || c > '9') && (c < 'a' || c > 'f') {
			return false
		}
	}
	return true
}
