package secrets

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

// Service manages swarm secrets.
type Service struct {
	docker        *docker.DockerClientService
	ensureManager func(ctx context.Context) error
}

func NewService(dockerService *docker.DockerClientService, ensureManager func(ctx context.Context) error) *Service {
	return &Service{docker: dockerService, ensureManager: ensureManager}
}

func (s *Service) ListSecrets(ctx context.Context) ([]swarmtypes.SecretSummary, error) {
	if err := s.ensureManager(ctx); err != nil {
		return nil, err
	}

	secretsResult, err := s.docker.Call(ctx, func(ctx context.Context, dockerClient *client.Client) (client.SecretListResult, error) {
		return dockerClient.SecretList(ctx, client.SecretListOptions{})
	})
	if err != nil {
		return nil, fmt.Errorf("failed to list swarm secrets: %w", err)
	}

	items := make([]swarmtypes.SecretSummary, 0, len(secretsResult.Items))
	for _, secret := range secretsResult.Items {
		items = append(items, swarmtypes.NewSecretSummary(secret))
	}
	return items, nil
}

func (s *Service) GetSecret(ctx context.Context, secretID string) (*swarmtypes.SecretSummary, error) {
	if err := s.ensureManager(ctx); err != nil {
		return nil, err
	}

	secretResult, err := s.docker.Call(ctx, func(ctx context.Context, dockerClient *client.Client) (client.SecretInspectResult, error) {
		return dockerClient.SecretInspect(ctx, secretID, client.SecretInspectOptions{})
	})
	if err != nil {
		return nil, fmt.Errorf("failed to inspect swarm secret: %w", err)
	}

	return new(swarmtypes.NewSecretSummary(secretResult.Secret)), nil
}

func (s *Service) CreateSecret(ctx context.Context, req swarmtypes.SecretCreateRequest) (*swarmtypes.SecretSummary, error) {
	if err := s.ensureManager(ctx); err != nil {
		return nil, err
	}

	if trimmed := strings.TrimSpace(string(req.Spec)); trimmed == "" || trimmed == "null" {
		return nil, errors.New("secret spec is required")
	}
	var spec swarm.SecretSpec
	if err := json.Unmarshal(req.Spec, &spec); err != nil {
		return nil, fmt.Errorf("failed to parse secret spec: %w", err)
	}
	if strings.TrimSpace(spec.Name) == "" {
		return nil, errors.New("secret spec name is required")
	}

	createResult, err := s.docker.Call(ctx, func(ctx context.Context, dockerClient *client.Client) (client.SecretCreateResult, error) {
		return dockerClient.SecretCreate(ctx, client.SecretCreateOptions{Spec: spec})
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create swarm secret: %w", err)
	}

	return s.GetSecret(ctx, createResult.ID)
}

func (s *Service) RemoveSecret(ctx context.Context, secretID string) error {
	if err := s.ensureManager(ctx); err != nil {
		return err
	}

	if _, secretRemoveErr := s.docker.Call(ctx, func(ctx context.Context, dockerClient *client.Client) (client.SecretRemoveResult, error) {
		return dockerClient.SecretRemove(ctx, secretID, client.SecretRemoveOptions{})
	}); secretRemoveErr != nil {
		return fmt.Errorf("failed to remove swarm secret: %w", secretRemoveErr)
	}

	return nil
}
