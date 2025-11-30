package services

import (
	"context"
	"fmt"
	"sync"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/image"
	"github.com/docker/docker/api/types/mount"
	"github.com/docker/docker/api/types/network"
	"github.com/docker/docker/api/types/volume"
	"github.com/docker/docker/client"
	"github.com/getarcaneapp/arcane/backend/internal/config"
	"github.com/getarcaneapp/arcane/backend/internal/database"
	"github.com/getarcaneapp/arcane/backend/internal/utils/docker"
)

type DockerClientService struct {
	db     *database.DB
	config *config.Config
	client *client.Client
	mu     sync.Mutex
}

func NewDockerClientService(db *database.DB, cfg *config.Config) *DockerClientService {
	return &DockerClientService{
		db:     db,
		config: cfg,
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

	cli, err := client.NewClientWithOpts(
		client.WithHost(s.config.DockerHost),
		client.WithAPIVersionNegotiation(),
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

	containers, err := dockerClient.ContainerList(ctx, container.ListOptions{All: true})
	if err != nil {
		return nil, 0, 0, 0, fmt.Errorf("failed to list Docker containers: %w", err)
	}

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

	images, err := dockerClient.ImageList(ctx, image.ListOptions{All: true})
	if err != nil {
		return nil, 0, 0, 0, fmt.Errorf("failed to list Docker containers: %w", err)
	}

	var inuse, unused, total int
	for _, i := range images {
		total++
		if i.Containers >= 1 {
			inuse++
		} else {
			unused++
		}
	}

	return images, inuse, unused, total, nil
}

func (s *DockerClientService) GetAllNetworks(ctx context.Context) (_ []network.Summary, totalNetworks int, inuseNetworks int, unusedNetworks int, error error) {
	dockerClient, err := s.GetClient()
	if err != nil {
		return nil, 0, 0, 0, fmt.Errorf("failed to connect to Docker: %w", err)
	}

	containers, err := dockerClient.ContainerList(ctx, container.ListOptions{All: true})
	if err != nil {
		return nil, 0, 0, 0, fmt.Errorf("failed to list Docker containers: %w", err)
	}
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

	networks, err := dockerClient.NetworkList(ctx, network.ListOptions{})
	if err != nil {
		return nil, 0, 0, 0, fmt.Errorf("failed to list Docker networks: %w", err)
	}

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

	containers, err := dockerClient.ContainerList(ctx, container.ListOptions{All: true})
	if err != nil {
		return nil, 0, 0, 0, fmt.Errorf("failed to list Docker containers: %w", err)
	}
	ref := make(map[string]int64, len(containers))
	for _, c := range containers {
		for _, m := range c.Mounts {
			if m.Type == mount.TypeVolume && m.Name != "" {
				ref[m.Name]++
			}
		}
	}

	volResp, err := dockerClient.VolumeList(ctx, volume.ListOptions{})
	if err != nil {
		return nil, 0, 0, 0, fmt.Errorf("failed to list Docker volumes: %w", err)
	}
	volumes := volResp.Volumes

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
