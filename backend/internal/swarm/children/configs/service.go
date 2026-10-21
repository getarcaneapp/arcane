package configs

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/getarcaneapp/arcane/types/v3"
	"github.com/moby/moby/api/types/swarm"
	"github.com/moby/moby/client"

	"github.com/getarcaneapp/arcane/backend/v2/internal/docker"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/pagination"
)

// Service manages swarm configs.
type Service struct {
	docker        *docker.DockerClientService
	ensureManager func(ctx context.Context) error
}

func NewService(dockerService *docker.DockerClientService, ensureManager func(ctx context.Context) error) *Service {
	return &Service{docker: dockerService, ensureManager: ensureManager}
}

var configPagination = pagination.Config[types.SwarmConfig]{
	SearchAccessors: []pagination.SearchAccessor[types.SwarmConfig]{
		func(item types.SwarmConfig) (string, error) {
			return item.Spec.Name, nil
		},
		func(item types.SwarmConfig) (string, error) {
			return item.ID, nil
		},
	},
	SortBindings: []pagination.SortBinding[types.SwarmConfig]{
		{Key: "name", Fn: func(a, b types.SwarmConfig) int {
			return strings.Compare(a.Spec.Name, b.Spec.Name)
		}},
		{Key: "createdAt", Fn: func(a, b types.SwarmConfig) int {
			return a.CreatedAt.Compare(b.CreatedAt)
		}},
		{Key: "updatedAt", Fn: func(a, b types.SwarmConfig) int {
			return a.UpdatedAt.Compare(b.UpdatedAt)
		}},
	},
}

func (s *Service) ListConfigs(ctx context.Context, params pagination.QueryParams) ([]types.SwarmConfig, pagination.Response, error) {
	if err := s.ensureManager(ctx); err != nil {
		return nil, pagination.Response{}, err
	}

	listResult, err := s.docker.Call(ctx, func(ctx context.Context, dockerClient *client.Client) (client.ConfigListResult, error) {
		return dockerClient.ConfigList(ctx, client.ConfigListOptions{})
	})
	if err != nil {
		return nil, pagination.Response{}, fmt.Errorf("failed to list swarm configs: %w", err)
	}

	items := make([]types.SwarmConfig, 0, len(listResult.Items))
	for _, item := range listResult.Items {
		items = append(items, types.NewSwarmConfig(item))
	}
	result := configPagination.SearchOrderAndPaginate(items, params)
	return result.Items, pagination.BuildResponse(result.TotalCount, result.TotalAvailable, params), nil
}

func (s *Service) GetConfig(ctx context.Context, configID string) (*types.SwarmConfig, error) {
	if err := s.ensureManager(ctx); err != nil {
		return nil, err
	}

	cfgResult, err := s.docker.Call(ctx, func(ctx context.Context, dockerClient *client.Client) (client.ConfigInspectResult, error) {
		return dockerClient.ConfigInspect(ctx, configID, client.ConfigInspectOptions{})
	})
	if err != nil {
		return nil, fmt.Errorf("failed to inspect swarm config: %w", err)
	}

	return new(types.NewSwarmConfig(cfgResult.Config)), nil
}

func (s *Service) CreateConfig(ctx context.Context, config types.SwarmConfig) (*types.SwarmConfig, error) {
	if err := s.ensureManager(ctx); err != nil {
		return nil, err
	}

	if strings.TrimSpace(config.Spec.Name) == "" {
		return nil, errors.New("config spec name is required")
	}

	createResult, err := s.docker.Call(ctx, func(ctx context.Context, dockerClient *client.Client) (client.ConfigCreateResult, error) {
		return dockerClient.ConfigCreate(ctx, client.ConfigCreateOptions{Spec: config.Spec})
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create swarm config: %w", err)
	}

	return s.GetConfig(ctx, createResult.ID)
}

func (s *Service) UpdateConfig(ctx context.Context, configID string, req types.SwarmConfigUpdateRequest) (*types.SwarmConfig, error) {
	current, err := s.GetConfig(ctx, configID)
	if err != nil {
		return nil, err
	}

	if _, updateErr := s.docker.Call(ctx, func(ctx context.Context, dockerClient *client.Client) (client.ConfigUpdateResult, error) {
		return dockerClient.ConfigUpdate(ctx, current.ID, client.ConfigUpdateOptions{
			Version: swarm.Version{Index: cmp.Or(req.Version, current.Version.Index)},
			Spec: swarm.ConfigSpec{
				Annotations: swarm.Annotations{
					Name:   current.Spec.Name,
					Labels: req.Labels,
				},
				Data:       current.Spec.Data,
				Templating: current.Spec.Templating,
			},
		})
	}); updateErr != nil {
		return nil, fmt.Errorf("failed to update swarm config: %w", updateErr)
	}

	return s.GetConfig(ctx, current.ID)
}

func (s *Service) RemoveConfig(ctx context.Context, configID string) error {
	if err := s.ensureManager(ctx); err != nil {
		return err
	}

	if _, configRemoveErr := s.docker.Call(ctx, func(ctx context.Context, dockerClient *client.Client) (client.ConfigRemoveResult, error) {
		return dockerClient.ConfigRemove(ctx, configID, client.ConfigRemoveOptions{})
	}); configRemoveErr != nil {
		return fmt.Errorf("failed to remove swarm config: %w", configRemoveErr)
	}

	return nil
}
