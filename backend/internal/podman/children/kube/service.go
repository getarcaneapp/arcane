package kube

import (
	"context"

	"github.com/getarcaneapp/arcane/types/v3"

	"github.com/getarcaneapp/arcane/backend/v3/pkg/libarcane/podman"
)

type Service struct {
	runtime func(context.Context) (podman.Runtime, error)
}

func NewService(runtime func(context.Context) (podman.Runtime, error)) *Service {
	return &Service{runtime: runtime}
}

func (s *Service) Play(ctx context.Context, req types.KubePlayRequest) (types.KubePlayReport, error) {
	rt, err := s.runtime(ctx)
	if err != nil {
		return types.KubePlayReport{}, err
	}
	return rt.Client.KubePlay(ctx, req)
}

func (s *Service) Down(ctx context.Context, req types.KubeDownRequest) (types.KubeDownReport, error) {
	rt, err := s.runtime(ctx)
	if err != nil {
		return types.KubeDownReport{}, err
	}
	return rt.Client.KubeDown(ctx, req)
}

func (s *Service) Generate(ctx context.Context, req types.KubeGenerateRequest) (types.KubeGenerateResponse, error) {
	rt, err := s.runtime(ctx)
	if err != nil {
		return types.KubeGenerateResponse{}, err
	}

	yaml, err := rt.Client.KubeGenerate(ctx, req)
	if err != nil {
		return types.KubeGenerateResponse{}, err
	}
	return types.KubeGenerateResponse{YAML: yaml}, nil
}
