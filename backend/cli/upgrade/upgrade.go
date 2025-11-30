package upgrade

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"strings"
	"time"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/filters"
	"github.com/docker/docker/api/types/image"
	"github.com/docker/docker/api/types/network"
	"github.com/docker/docker/client"
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
	// Look for containers with "arcane" in the image name
	filter := filters.NewArgs()
	filter.Add("status", "running")

	containers, err := dockerClient.ContainerList(ctx, container.ListOptions{Filters: filter})
	if err != nil {
		return container.InspectResponse{}, err
	}

	selfID := getSelfContainerID()
	slog.Info("Searching for Arcane container", "selfID", selfID, "totalContainers", len(containers))

	now := time.Now()

	for _, c := range containers {
		if shouldSkipContainer(c, selfID, now) {
			continue
		}

		if strings.Contains(strings.ToLower(c.Image), "arcane") {
			slog.Info("Found matching container", "id", c.ID[:12], "image", c.Image, "names", c.Names)
			return dockerClient.ContainerInspect(ctx, c.ID)
		}
	}

	return container.InspectResponse{}, fmt.Errorf("no running Arcane container found")
}

// getSelfContainerID attempts to detect the container ID if running in Docker
func getSelfContainerID() string {
	data, err := os.ReadFile("/proc/self/cgroup")
	if err != nil {
		return ""
	}

	content := string(data)
	for _, line := range strings.Split(content, "\n") {
		// cgroup v1: 12:pids:/docker/abc123...
		if id := extractCgroupV1ID(line); id != "" {
			return id
		}
		// cgroup v2: 0::/system.slice/docker-abc123....scope
		if id := extractCgroupV2ID(line); id != "" {
			return id
		}
	}

	return ""
}

// extractCgroupV1ID extracts container ID from cgroup v1 format
func extractCgroupV1ID(line string) string {
	if !strings.Contains(line, "/docker/") {
		return ""
	}

	parts := strings.Split(line, "/docker/")
	if len(parts) <= 1 {
		return ""
	}

	id := strings.TrimSpace(parts[1])
	if idx := strings.Index(id, "/"); idx != -1 {
		id = id[:idx]
	}

	if len(id) >= 12 {
		return id[:12]
	}

	return ""
}

// extractCgroupV2ID extracts container ID from cgroup v2 format
func extractCgroupV2ID(line string) string {
	if !strings.Contains(line, "docker-") || !strings.Contains(line, ".scope") {
		return ""
	}

	start := strings.Index(line, "docker-")
	if start == -1 {
		return ""
	}

	start += 7 // len("docker-")
	end := strings.Index(line[start:], ".scope")
	if end == -1 {
		return ""
	}

	id := line[start : start+end]
	if len(id) >= 12 {
		return id[:12]
	}

	return ""
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
	tempName := fmt.Sprintf("%s-upgrading", originalName)

	// Create new container config
	config := *oldContainer.Config
	config.Image = newImage

	hostConfig := oldContainer.HostConfig

	// Build network config - preserve all network settings including IP addresses
	networkConfig := &network.NetworkingConfig{
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

	fmt.Println("PROGRESS:70:Stopping old container")
	slog.Info("Stopping old container", "name", originalName)
	timeout := 10
	if err := dockerClient.ContainerStop(ctx, oldContainer.ID, container.StopOptions{Timeout: &timeout}); err != nil {
		return fmt.Errorf("stop old container: %w", err)
	}

	fmt.Println("PROGRESS:75:Creating new container")
	slog.Info("Creating new container", "tempName", tempName)
	resp, err := dockerClient.ContainerCreate(ctx, &config, hostConfig, networkConfig, nil, tempName)
	if err != nil {
		// Try to restart old container on failure
		_ = dockerClient.ContainerStart(ctx, oldContainer.ID, container.StartOptions{})
		return fmt.Errorf("create new container: %w", err)
	}

	fmt.Println("PROGRESS:80:Starting new container")
	slog.Info("Starting new container", "id", resp.ID[:12])
	if err := dockerClient.ContainerStart(ctx, resp.ID, container.StartOptions{}); err != nil {
		// Cleanup new container and restart old one
		_ = dockerClient.ContainerRemove(ctx, resp.ID, container.RemoveOptions{Force: true})
		_ = dockerClient.ContainerStart(ctx, oldContainer.ID, container.StartOptions{})
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

	fmt.Println("PROGRESS:95:Renaming new container")
	slog.Info("Renaming new container", "from", tempName, "to", originalName)
	if err := dockerClient.ContainerRename(ctx, resp.ID, originalName); err != nil {
		slog.Warn("Failed to rename container", "error", err)
	}

	return nil
}
