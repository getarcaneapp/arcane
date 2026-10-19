package secrets

import (
	"context"

	podmantypes "github.com/getarcaneapp/arcane/types/v2/podman"

	"github.com/getarcaneapp/arcane/backend/v2/pkg/libarcane/podman"
)

type Service struct {
	client func(context.Context) (*podman.Client, error)
}

func NewService(client func(context.Context) (*podman.Client, error)) *Service {
	return &Service{client: client}
}

func (s *Service) List(ctx context.Context) ([]podmantypes.Secret, error) {
	client, err := s.client(ctx)
	if err != nil {
		return nil, err
	}
	return client.SecretList(ctx, nil)
}

func (s *Service) Create(ctx context.Context, req podmantypes.SecretCreateRequest) (string, error) {
	client, err := s.client(ctx)
	if err != nil {
		return "", err
	}
	return client.SecretCreate(ctx, req)
}

func (s *Service) Delete(ctx context.Context, nameOrID string) error {
	client, err := s.client(ctx)
	if err != nil {
		return err
	}
	return client.SecretRemove(ctx, nameOrID)
}
