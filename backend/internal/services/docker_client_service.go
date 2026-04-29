package services

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/getarcaneapp/arcane/backend/internal/config"
	"github.com/getarcaneapp/arcane/backend/internal/database"
	docker "github.com/getarcaneapp/arcane/backend/pkg/dockerutil"
	"github.com/getarcaneapp/arcane/backend/pkg/libarcane"
	"github.com/getarcaneapp/arcane/backend/pkg/libarcane/timeouts"
	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/api/types/image"
	"github.com/moby/moby/api/types/mount"
	"github.com/moby/moby/api/types/network"
	"github.com/moby/moby/api/types/volume"
	"github.com/moby/moby/client"
)

const dockerClientNegotiationTimeout = 5 * time.Second

type DockerClientService struct {
	db              *database.DB
	config          *config.Config
	settingsService *SettingsService
	client          *client.Client
	clientVersion   string
	clientLastProbe time.Time
	mu              sync.Mutex
}

func NewDockerClientService(db *database.DB, cfg *config.Config, settingsService *SettingsService) *DockerClientService {
	return &DockerClientService{
		db:              db,
		config:          cfg,
		settingsService: settingsService,
	}
}

func newDockerClientInternal(ctx context.Context, host string) (*client.Client, error) {
	apiVersion, err := detectDockerAPIVersionInternal(ctx, host)
	if err != nil {
		return nil, err
	}

	configuredClient, err := newDockerClientWithAPIVersionInternal(host, apiVersion)
	if err != nil {
		return nil, err
	}

	return configuredClient, nil
}

func detectDockerAPIVersionInternal(ctx context.Context, host string) (string, error) {
	probeClient, err := client.New(
		client.WithHost(host),
	)
	if err != nil {
		return "", fmt.Errorf("failed to create Docker probe client: %w", err)
	}
	defer closeDockerClientInternal(probeClient, "failed to close probe Docker client")

	ctx, cancel := context.WithTimeout(ctx, dockerClientNegotiationTimeout)
	defer cancel()

	pingResult, err := probeClient.Ping(ctx, client.PingOptions{})
	if err != nil {
		return "", fmt.Errorf("failed to negotiate Docker API version: %w", err)
	}

	apiVersion := strings.TrimSpace(pingResult.APIVersion)
	if apiVersion == "" {
		slog.WarnContext(ctx, "Docker ping did not report an API version, using minimum supported client API version", "api_version", client.MinAPIVersion)
		return client.MinAPIVersion, nil
	}

	return apiVersion, nil
}

func newDockerClientWithAPIVersionInternal(host string, apiVersion string) (*client.Client, error) {
	configuredClient, err := client.New(
		client.WithHost(host),
		client.WithAPIVersion(apiVersion),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to configure Docker client API version %s: %w", apiVersion, err)
	}

	return configuredClient, nil
}

func closeDockerClientInternal(cli *client.Client, message string) {
	if cli == nil {
		return
	}

	if err := cli.Close(); err != nil {
		slog.Warn(message, "error", err)
	}
}

// GetClient returns a singleton Docker client instance.
// It initializes the client on the first call.
func (s *DockerClientService) GetClient(ctx context.Context) (*client.Client, error) {
	s.mu.Lock()
	if s.client != nil {
		cli := s.client
		s.mu.Unlock()
		return cli, nil
	}
	s.mu.Unlock()

	cli, err := newDockerClientInternal(ctx, s.config.DockerHost)
	if err != nil {
		return nil, fmt.Errorf("failed to create Docker client: %w", err)
	}

	s.mu.Lock()
	if s.client != nil {
		existingClient := s.client
		s.mu.Unlock()
		closeDockerClientInternal(cli, "failed to close unused Docker client after concurrent initialization")
		return existingClient, nil
	}

	s.client = cli
	s.clientVersion = cli.ClientVersion()
	s.clientLastProbe = time.Now()
	s.mu.Unlock()

	return cli, nil
}

// RefreshClient probes the Docker daemon and recreates the cached client when
// the daemon's effective API version changed.
func (s *DockerClientService) RefreshClient(ctx context.Context) error {
	apiVersion, err := detectDockerAPIVersionInternal(ctx, s.config.DockerHost)
	if err != nil {
		return fmt.Errorf("failed to refresh Docker client: %w", err)
	}

	s.mu.Lock()
	if s.client != nil && apiVersion == s.clientVersion {
		s.clientLastProbe = time.Now()
		s.mu.Unlock()
		return nil
	}
	s.mu.Unlock()

	cli, err := newDockerClientWithAPIVersionInternal(s.config.DockerHost, apiVersion)
	if err != nil {
		return fmt.Errorf("failed to refresh Docker client: %w", err)
	}

	s.mu.Lock()
	if s.client != nil && apiVersion == s.clientVersion {
		s.clientLastProbe = time.Now()
		s.mu.Unlock()
		closeDockerClientInternal(cli, "failed to close unused Docker client after concurrent refresh")
		return nil
	}

	oldClient := s.client
	s.client = cli
	s.clientVersion = apiVersion
	s.clientLastProbe = time.Now()
	s.mu.Unlock()

	closeDockerClientInternal(oldClient, "failed to close replaced Docker client")

	return nil
}

// DockerHost returns the configured DOCKER_HOST value.
func (s *DockerClientService) DockerHost() string {
	return s.config.DockerHost
}

func (s *DockerClientService) GetAllContainers(ctx context.Context) ([]container.Summary, int, int, int, error) {
	dockerClient, err := s.GetClient(ctx)
	if err != nil {
		return nil, 0, 0, 0, fmt.Errorf("failed to connect to Docker: %w", err)
	}

	settings := s.settingsService.GetSettingsConfig()
	apiCtx, cancel := timeouts.WithTimeout(ctx, settings.DockerAPITimeout.AsInt(), timeouts.DefaultDockerAPI)
	defer cancel()

	containerList, err := dockerClient.ContainerList(apiCtx, client.ContainerListOptions{All: true})
	if err != nil {
		return nil, 0, 0, 0, fmt.Errorf("failed to list Docker containers: %w", err)
	}
	containers := containerList.Items

	var running, stopped, total int
	for _, c := range containers {
		total++
		if c.State == "running" {
			running++
		} else {
			stopped++
		}
	}

	return containers, running, stopped, total, nil
}

func (s *DockerClientService) GetAllImages(ctx context.Context) ([]image.Summary, int, int, int, error) {
	dockerClient, err := s.GetClient(ctx)
	if err != nil {
		return nil, 0, 0, 0, fmt.Errorf("failed to connect to Docker: %w", err)
	}

	settings := s.settingsService.GetSettingsConfig()
	apiCtx, cancel := timeouts.WithTimeout(ctx, settings.DockerAPITimeout.AsInt(), timeouts.DefaultDockerAPI)
	defer cancel()

	imageList, err := dockerClient.ImageList(apiCtx, client.ImageListOptions{All: true})
	if err != nil {
		return nil, 0, 0, 0, fmt.Errorf("failed to list Docker images: %w", err)
	}
	images := imageList.Items

	containerList, err := dockerClient.ContainerList(apiCtx, client.ContainerListOptions{All: true})
	if err != nil {
		return nil, 0, 0, 0, fmt.Errorf("failed to list Docker containers: %w", err)
	}
	containers := containerList.Items

	inuse, unused, total := countImageUsageInternal(images, containers)

	return images, inuse, unused, total, nil
}

func countImageUsageInternal(images []image.Summary, containers []container.Summary) (inuse int, unused int, total int) {
	inUseImageIDs := make(map[string]struct{}, len(containers))
	for _, c := range containers {
		if c.ImageID == "" {
			continue
		}
		inUseImageIDs[c.ImageID] = struct{}{}
	}

	for _, img := range images {
		total++
		if _, ok := inUseImageIDs[img.ID]; ok {
			inuse++
			continue
		}
		unused++
	}

	return inuse, unused, total
}

func (s *DockerClientService) GetAllNetworks(ctx context.Context) (_ []network.Summary, inuseNetworks int, unusedNetworks int, totalNetworks int, error error) {
	dockerClient, err := s.GetClient(ctx)
	if err != nil {
		return nil, 0, 0, 0, fmt.Errorf("failed to connect to Docker: %w", err)
	}

	settings := s.settingsService.GetSettingsConfig()
	apiCtx, cancel := timeouts.WithTimeout(ctx, settings.DockerAPITimeout.AsInt(), timeouts.DefaultDockerAPI)
	defer cancel()

	containerList, err := dockerClient.ContainerList(apiCtx, client.ContainerListOptions{All: true})
	if err != nil {
		return nil, 0, 0, 0, fmt.Errorf("failed to list Docker containers: %w", err)
	}
	containers := containerList.Items
	inUseByID := make(map[string]bool)
	inUseByName := make(map[string]bool)
	for _, c := range containers {
		if c.NetworkSettings == nil || c.NetworkSettings.Networks == nil {
			continue
		}
		for netName, es := range c.NetworkSettings.Networks {
			if es.NetworkID != "" {
				inUseByID[es.NetworkID] = true
			}
			inUseByName[netName] = true
		}
	}

	networkList, err := libarcane.NetworkListWithCompatibility(apiCtx, dockerClient, client.NetworkListOptions{})
	if err != nil {
		return nil, 0, 0, 0, fmt.Errorf("failed to list Docker networks: %w", err)
	}
	networks := networkList.Items

	var inuse, unused, total int
	for _, n := range networks {
		total++ // total includes all networks (including defaults)

		// Only count non-default networks towards in-use/unused breakdown
		if !docker.IsDefaultNetwork(n.Name) {
			used := inUseByID[n.ID] || inUseByName[n.Name]
			if used {
				inuse++
			} else {
				unused++
			}
		}
	}

	// Return order: inuse, unused, total (matches handler expectations)
	return networks, inuse, unused, total, nil
}

func (s *DockerClientService) GetAllVolumes(ctx context.Context) ([]*volume.Volume, int, int, int, error) {
	dockerClient, err := s.GetClient(ctx)
	if err != nil {
		return nil, 0, 0, 0, fmt.Errorf("failed to connect to Docker: %w", err)
	}

	settings := s.settingsService.GetSettingsConfig()
	apiCtx, cancel := timeouts.WithTimeout(ctx, settings.DockerAPITimeout.AsInt(), timeouts.DefaultDockerAPI)
	defer cancel()

	containerList, err := dockerClient.ContainerList(apiCtx, client.ContainerListOptions{All: true})
	if err != nil {
		return nil, 0, 0, 0, fmt.Errorf("failed to list Docker containers: %w", err)
	}
	containers := containerList.Items
	ref := make(map[string]int64, len(containers))
	for _, c := range containers {
		for _, m := range c.Mounts {
			if m.Type == mount.TypeVolume && m.Name != "" {
				ref[m.Name]++
			}
		}
	}

	volResp, err := dockerClient.VolumeList(apiCtx, client.VolumeListOptions{})
	if err != nil {
		return nil, 0, 0, 0, fmt.Errorf("failed to list Docker volumes: %w", err)
	}
	volumeItems := volResp.Items
	volumes := make([]*volume.Volume, 0, len(volumeItems))
	for i := range volumeItems {
		volumes = append(volumes, &volumeItems[i])
	}

	var inuse, unused, total int
	for _, v := range volumes {
		total++
		if ref[v.Name] > 0 {
			inuse++
		} else {
			unused++
		}
	}

	return volumes, inuse, unused, total, nil
}
