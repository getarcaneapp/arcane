package secrets

import (
	"cmp"
	"context"
	"encoding/json/v2"
	"errors"
	"fmt"
	"strings"

	swarmtypes "github.com/getarcaneapp/arcane/types/v2/swarm"
	"github.com/moby/moby/api/types/swarm"
	"github.com/moby/moby/client"

	"github.com/getarcaneapp/arcane/backend/v2/internal/docker"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/pagination"
)

// Service manages swarm secrets.
type Service struct {
	docker        *docker.DockerClientService
	ensureManager func(ctx context.Context) error
}

func NewService(dockerService *docker.DockerClientService, ensureManager func(ctx context.Context) error) *Service {
	return &Service{docker: dockerService, ensureManager: ensureManager}
}

var secretPagination = pagination.Config[swarmtypes.SecretSummary]{
	SearchAccessors: []pagination.SearchAccessor[swarmtypes.SecretSummary]{
		func(item swarmtypes.SecretSummary) (string, error) {
			return item.Spec.Name, nil
		},
		func(item swarmtypes.SecretSummary) (string, error) {
			return item.ID, nil
		},
	},
	SortBindings: []pagination.SortBinding[swarmtypes.SecretSummary]{
		{Key: "name", Fn: func(a, b swarmtypes.SecretSummary) int {
			return strings.Compare(a.Spec.Name, b.Spec.Name)
		}},
		{Key: "createdAt", Fn: func(a, b swarmtypes.SecretSummary) int {
			return a.CreatedAt.Compare(b.CreatedAt)
		}},
		{Key: "updatedAt", Fn: func(a, b swarmtypes.SecretSummary) int {
			return a.UpdatedAt.Compare(b.UpdatedAt)
		}},
	},
}

func (s *Service) ListSecrets(ctx context.Context, params pagination.QueryParams) ([]swarmtypes.SecretSummary, pagination.Response, error) {
	if err := s.ensureManager(ctx); err != nil {
		return nil, pagination.Response{}, err
	}

	listResult, err := s.docker.Call(ctx, func(ctx context.Context, dockerClient *client.Client) (client.SecretListResult, error) {
		return dockerClient.SecretList(ctx, client.SecretListOptions{})
	})
	if err != nil {
		return nil, pagination.Response{}, fmt.Errorf("failed to list swarm secrets: %w", err)
	}

	items := make([]swarmtypes.SecretSummary, 0, len(listResult.Items))
	for _, item := range listResult.Items {
		items = append(items, swarmtypes.NewSecretSummary(item))
	}
	result := secretPagination.SearchOrderAndPaginate(items, params)
	return result.Items, pagination.BuildResponse(result.TotalCount, result.TotalAvailable, params), nil
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

func (s *Service) UpdateSecret(ctx context.Context, secretID string, req swarmtypes.SecretUpdateRequest) (*swarmtypes.SecretSummary, error) {
	current, err := s.GetSecret(ctx, secretID)
	if err != nil {
		return nil, err
	}

	if _, updateErr := s.docker.Call(ctx, func(ctx context.Context, dockerClient *client.Client) (client.SecretUpdateResult, error) {
		return dockerClient.SecretUpdate(ctx, current.ID, client.SecretUpdateOptions{
			Version: swarm.Version{Index: cmp.Or(req.Version, current.Version.Index)},
			Spec: swarm.SecretSpec{
				Annotations: swarm.Annotations{
					Name:   current.Spec.Name,
					Labels: req.Labels,
				},
				Driver:     current.Spec.Driver,
				Templating: current.Spec.Templating,
			},
		})
	}); updateErr != nil {
		return nil, fmt.Errorf("failed to update swarm secret: %w", updateErr)
	}

	return s.GetSecret(ctx, current.ID)
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
