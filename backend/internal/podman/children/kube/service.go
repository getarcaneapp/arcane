package kube

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

func (s *Service) Play(ctx context.Context, req podmantypes.KubePlayRequest) (podmantypes.KubePlayReport, error) {
	client, err := s.client(ctx)
	if err != nil {
		return podmantypes.KubePlayReport{}, err
	}
	return client.KubePlay(ctx, req)
}

func (s *Service) Down(ctx context.Context, req podmantypes.KubeDownRequest) (podmantypes.KubeDownReport, error) {
	client, err := s.client(ctx)
	if err != nil {
		return podmantypes.KubeDownReport{}, err
	}
	return client.KubeDown(ctx, req)
}

func (s *Service) Generate(ctx context.Context, req podmantypes.KubeGenerateRequest) (podmantypes.KubeGenerateResponse, error) {
	client, err := s.client(ctx)
	if err != nil {
		return podmantypes.KubeGenerateResponse{}, err
	}

	yaml, err := client.KubeGenerate(ctx, req)
	if err != nil {
		return podmantypes.KubeGenerateResponse{}, err
	}
	return podmantypes.KubeGenerateResponse{YAML: yaml}, nil
}
