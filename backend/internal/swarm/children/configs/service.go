package configs

import (
	"context"
	"encoding/json/v2"
	"errors"
	"fmt"
	"strings"

	swarmtypes "github.com/getarcaneapp/arcane/types/v2/swarm"
	"github.com/moby/moby/api/types/swarm"
	"github.com/moby/moby/client"

	"github.com/getarcaneapp/arcane/backend/v2/internal/docker"
)

// Service manages swarm configs.
type Service struct {
	docker        *docker.DockerClientService
	ensureManager func(ctx context.Context) error
}

func NewService(dockerService *docker.DockerClientService, ensureManager func(ctx context.Context) error) *Service {
	return &Service{docker: dockerService, ensureManager: ensureManager}
}

func (s *Service) ListConfigs(ctx context.Context) ([]swarmtypes.ConfigSummary, error) {
	if err := s.ensureManager(ctx); err != nil {
		return nil, err
	}

	configsResult, err := s.docker.Call(ctx, func(ctx context.Context, dockerClient *client.Client) (client.ConfigListResult, error) {
		return dockerClient.ConfigList(ctx, client.ConfigListOptions{})
	})
	if err != nil {
		return nil, fmt.Errorf("failed to list swarm configs: %w", err)
	}

	items := make([]swarmtypes.ConfigSummary, 0, len(configsResult.Items))
	for _, cfg := range configsResult.Items {
		items = append(items, swarmtypes.NewConfigSummary(cfg))
	}
	return items, nil
}

func (s *Service) GetConfig(ctx context.Context, configID string) (*swarmtypes.ConfigSummary, error) {
	if err := s.ensureManager(ctx); err != nil {
		return nil, err
	}

	cfgResult, err := s.docker.Call(ctx, func(ctx context.Context, dockerClient *client.Client) (client.ConfigInspectResult, error) {
		return dockerClient.ConfigInspect(ctx, configID, client.ConfigInspectOptions{})
	})
	if err != nil {
		return nil, fmt.Errorf("failed to inspect swarm config: %w", err)
	}

	return new(swarmtypes.NewConfigSummary(cfgResult.Config)), nil
}

func (s *Service) CreateConfig(ctx context.Context, req swarmtypes.ConfigCreateRequest) (*swarmtypes.ConfigSummary, error) {
	if err := s.ensureManager(ctx); err != nil {
		return nil, err
	}

	if trimmed := strings.TrimSpace(string(req.Spec)); trimmed == "" || trimmed == "null" {
		return nil, errors.New("config spec is required")
	}
	var spec swarm.ConfigSpec
	if err := json.Unmarshal(req.Spec, &spec); err != nil {
		return nil, fmt.Errorf("failed to parse config spec: %w", err)
	}
	if strings.TrimSpace(spec.Name) == "" {
		return nil, errors.New("config spec name is required")
	}

	createResult, err := s.docker.Call(ctx, func(ctx context.Context, dockerClient *client.Client) (client.ConfigCreateResult, error) {
		return dockerClient.ConfigCreate(ctx, client.ConfigCreateOptions{Spec: spec})
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create swarm config: %w", err)
	}

	return s.GetConfig(ctx, createResult.ID)
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
