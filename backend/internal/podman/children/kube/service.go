package kube

import (
	"context"

	podmantypes "github.com/getarcaneapp/arcane/types/v2/podman"

	"github.com/getarcaneapp/arcane/backend/v2/pkg/libarcane/podman"
)

type Service struct {
	runtime func(context.Context) (podman.Runtime, error)
}

func NewService(runtime func(context.Context) (podman.Runtime, error)) *Service {
	return &Service{runtime: runtime}
}

func (s *Service) Play(ctx context.Context, req podmantypes.KubePlayRequest) (podmantypes.KubePlayReport, error) {
	rt, err := s.runtime(ctx)
	if err != nil {
		return podmantypes.KubePlayReport{}, err
	}
	return rt.Client.KubePlay(ctx, req)
}

func (s *Service) Down(ctx context.Context, req podmantypes.KubeDownRequest) (podmantypes.KubeDownReport, error) {
	rt, err := s.runtime(ctx)
	if err != nil {
		return podmantypes.KubeDownReport{}, err
	}
	return rt.Client.KubeDown(ctx, req)
}

func (s *Service) Generate(ctx context.Context, req podmantypes.KubeGenerateRequest) (podmantypes.KubeGenerateResponse, error) {
	rt, err := s.runtime(ctx)
	if err != nil {
		return podmantypes.KubeGenerateResponse{}, err
	}

	yaml, err := rt.Client.KubeGenerate(ctx, req)
	if err != nil {
		return podmantypes.KubeGenerateResponse{}, err
	}
	return podmantypes.KubeGenerateResponse{YAML: yaml}, nil
}
