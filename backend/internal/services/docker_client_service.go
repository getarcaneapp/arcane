package services

import (
	"context"
	"fmt"
	"sync"

	"github.com/getarcaneapp/arcane/backend/internal/config"
	"github.com/getarcaneapp/arcane/backend/internal/database"
	"github.com/getarcaneapp/arcane/backend/internal/utils/docker"
	"github.com/getarcaneapp/arcane/backend/internal/utils/timeouts"
	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/api/types/image"
	"github.com/moby/moby/api/types/mount"
	"github.com/moby/moby/api/types/network"
	"github.com/moby/moby/api/types/volume"
	"github.com/moby/moby/client"
)

type DockerClientService struct {
	db              *database.DB
	config          *config.Config
	settingsService *SettingsService
	client          *client.Client
	mu              sync.Mutex
}

func NewDockerClientService(db *database.DB, cfg *config.Config, settingsService *SettingsService) *DockerClientService {
	return &DockerClientService{
		db:              db,
		config:          cfg,
		settingsService: settingsService,
	}
}

// GetClient returns a singleton Docker client instance.
// It initializes the client on the first call.
func (s *DockerClientService) GetClient() (*client.Client, error) {
	if s.client != nil {
		return s.client, nil
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	// Double-check locking
	if s.client != nil {
		return s.client, nil
	}

	cli, err := client.New(
		client.WithHost(s.config.DockerHost),
		client.WithAPIVersionFromEnv(),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create Docker client: %w", err)
	}

	s.client = cli
	return s.client, nil
}

func (s *DockerClientService) GetAllContainers(ctx context.Context) ([]container.Summary, int, int, int, error) {
	dockerClient, err := s.GetClient()
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
	dockerClient, err := s.GetClient()
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
	dockerClient, err := s.GetClient()
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

	networkList, err := dockerClient.NetworkList(apiCtx, client.NetworkListOptions{})
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
	dockerClient, err := s.GetClient()
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
