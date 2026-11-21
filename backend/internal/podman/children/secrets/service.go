package secrets

import (
	"context"
	"strings"

	"getarcane.dev/arcane/v3/pkg/pagination"
	"getarcane.dev/arcane/v3/pkg/podman"
	"getarcane.dev/types/v3"
)

type (
	Service struct {
		runtime func(context.Context) (podman.Runtime, error)
	}
)

var secretPagination = pagination.Config[types.PodmanSecret]{
	SearchAccessors: []pagination.SearchAccessor[types.PodmanSecret]{
		func(secret types.PodmanSecret) (string, error) {
			return secret.Spec.Name, nil
		},
		func(secret types.PodmanSecret) (string, error) {
			return secret.ID, nil
		},
		func(secret types.PodmanSecret) (string, error) {
			return secret.Spec.Driver.Name, nil
		},
	},
	SortBindings: []pagination.SortBinding[types.PodmanSecret]{
		{Key: "name", Fn: func(a, b types.PodmanSecret) int {
			return strings.Compare(a.Spec.Name, b.Spec.Name)
		}},
		{Key: "driver", Fn: func(a, b types.PodmanSecret) int {
			return strings.Compare(a.Spec.Driver.Name, b.Spec.Driver.Name)
		}},
		{Key: "createdAt", Fn: func(a, b types.PodmanSecret) int {
			return a.CreatedAt.Compare(b.CreatedAt)
		}},
		{Key: "updatedAt", Fn: func(a, b types.PodmanSecret) int {
			return a.UpdatedAt.Compare(b.UpdatedAt)
		}},
	},
}

func NewService(runtime func(context.Context) (podman.Runtime, error)) *Service {
	return &Service{runtime: runtime}
}

func (s *Service) List(ctx context.Context) ([]types.PodmanSecret, error) {
	rt, err := s.runtime(ctx)
	if err != nil {
		return nil, err
	}
	return rt.Client.SecretList(ctx, nil)
}

func (s *Service) ListPaginated(ctx context.Context, params pagination.QueryParams) ([]types.PodmanSecret, pagination.Response, error) {
	items, err := s.List(ctx)
	if err != nil {
		return nil, pagination.Response{}, err
	}
	result := secretPagination.SearchOrderAndPaginate(items, params)
	return result.Items, pagination.BuildResponse(result.TotalCount, result.TotalAvailable, params), nil
}

func (s *Service) Inspect(ctx context.Context, nameOrID string) (types.PodmanSecret, error) {
	rt, err := s.runtime(ctx)
	if err != nil {
		return types.PodmanSecret{}, err
	}
	return rt.Client.SecretInspect(ctx, nameOrID)
}

func (s *Service) Create(ctx context.Context, req types.PodmanSecretCreateRequest) (string, error) {
	rt, err := s.runtime(ctx)
	if err != nil {
		return "", err
	}
	return rt.Client.SecretCreate(ctx, req, false)
}

func (s *Service) Update(ctx context.Context, name string, req types.PodmanSecretUpdateRequest) (types.PodmanSecret, error) {
	rt, err := s.runtime(ctx)
	if err != nil {
		return types.PodmanSecret{}, err
	}
	if _, replaceErr := rt.Client.SecretCreate(ctx, types.PodmanSecretCreateRequest{
		Name:          name,
		Data:          req.Data,
		Driver:        req.Driver,
		DriverOptions: req.DriverOptions,
		Labels:        req.Labels,
	}, true); replaceErr != nil {
		return types.PodmanSecret{}, replaceErr
	}
	return rt.Client.SecretInspect(ctx, name)
}

func (s *Service) Delete(ctx context.Context, nameOrID string) error {
	rt, err := s.runtime(ctx)
	if err != nil {
		return err
	}
	return rt.Client.SecretRemove(ctx, nameOrID)
}
