package secrets

import (
	"cmp"
	"context"
	"net/url"
	"strings"

	"github.com/getarcaneapp/arcane/types/v3"

	"github.com/getarcaneapp/arcane/backend/v3/pkg/libarcane/podman"
	"github.com/getarcaneapp/arcane/backend/v3/pkg/pagination"
	"github.com/getarcaneapp/arcane/backend/v3/pkg/utils/handlerutil"
	"github.com/getarcaneapp/arcane/backend/v3/pkg/utils/httpx"
)

type Service struct {
	runtime func(context.Context) (podman.Runtime, error)
}

func NewService(runtime func(context.Context) (podman.Runtime, error)) *Service {
	return &Service{runtime: runtime}
}

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

func (s *Service) Aggregate(ctx context.Context, params pagination.QueryParams) (types.Paginated[types.PodmanSecret], error) {
	collection, err := httpx.CollectEnvironments[types.Paginated[types.PodmanSecret]](ctx, url.Values{"start": {"0"}, "limit": {"-1"}})
	if err != nil {
		return types.Paginated[types.PodmanSecret]{}, err
	}

	pages := make([]pagination.EnvironmentPage[types.PodmanSecret], 0, len(collection.Results))
	for _, result := range collection.Results {
		pages = append(pages, pagination.EnvironmentPage[types.PodmanSecret]{
			Environment:    result.Environment,
			Items:          result.Value.Data,
			TotalAvailable: cmp.Or(result.Value.Pagination.GrandTotalItems, result.Value.Pagination.TotalItems),
		})
	}
	data, resp := pagination.MergeEnvironmentPages(secretPagination, pages, params)

	return types.Paginated[types.PodmanSecret]{
		Success:            true,
		Data:               data,
		Pagination:         handlerutil.PaginationResponse(resp),
		FailedEnvironments: collection.Failures,
	}, nil
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
