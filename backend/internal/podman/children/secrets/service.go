package secrets

import (
	"cmp"
	"context"
	"net/url"
	"strings"

	"github.com/getarcaneapp/arcane/types/v2/base"
	podmantypes "github.com/getarcaneapp/arcane/types/v2/podman"

	"github.com/getarcaneapp/arcane/backend/v2/pkg/libarcane/podman"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/pagination"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/utils/handlerutil"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/utils/httpx"
)

type Service struct {
	runtime func(context.Context) (podman.Runtime, error)
}

func NewService(runtime func(context.Context) (podman.Runtime, error)) *Service {
	return &Service{runtime: runtime}
}

var secretPagination = pagination.Config[podmantypes.Secret]{
	SearchAccessors: []pagination.SearchAccessor[podmantypes.Secret]{
		func(secret podmantypes.Secret) (string, error) {
			return secret.Spec.Name, nil
		},
		func(secret podmantypes.Secret) (string, error) {
			return secret.ID, nil
		},
		func(secret podmantypes.Secret) (string, error) {
			return secret.Spec.Driver.Name, nil
		},
	},
	SortBindings: []pagination.SortBinding[podmantypes.Secret]{
		{Key: "name", Fn: func(a, b podmantypes.Secret) int {
			return strings.Compare(a.Spec.Name, b.Spec.Name)
		}},
		{Key: "driver", Fn: func(a, b podmantypes.Secret) int {
			return strings.Compare(a.Spec.Driver.Name, b.Spec.Driver.Name)
		}},
		{Key: "createdAt", Fn: func(a, b podmantypes.Secret) int {
			return a.CreatedAt.Compare(b.CreatedAt)
		}},
		{Key: "updatedAt", Fn: func(a, b podmantypes.Secret) int {
			return a.UpdatedAt.Compare(b.UpdatedAt)
		}},
	},
}

func (s *Service) List(ctx context.Context) ([]podmantypes.Secret, error) {
	rt, err := s.runtime(ctx)
	if err != nil {
		return nil, err
	}
	return rt.Client.SecretList(ctx, nil)
}

func (s *Service) ListPaginated(ctx context.Context, params pagination.QueryParams) ([]podmantypes.Secret, pagination.Response, error) {
	items, err := s.List(ctx)
	if err != nil {
		return nil, pagination.Response{}, err
	}
	result := secretPagination.SearchOrderAndPaginate(items, params)
	return result.Items, pagination.BuildResponse(result.TotalCount, result.TotalAvailable, params), nil
}

func (s *Service) Aggregate(ctx context.Context, params pagination.QueryParams) (base.Paginated[podmantypes.Secret], error) {
	collection, err := httpx.CollectEnvironments[base.Paginated[podmantypes.Secret]](ctx, url.Values{"start": {"0"}, "limit": {"-1"}})
	if err != nil {
		return base.Paginated[podmantypes.Secret]{}, err
	}

	pages := make([]pagination.EnvironmentPage[podmantypes.Secret], 0, len(collection.Results))
	for _, result := range collection.Results {
		pages = append(pages, pagination.EnvironmentPage[podmantypes.Secret]{
			Environment:    result.Environment,
			Items:          result.Value.Data,
			TotalAvailable: cmp.Or(result.Value.Pagination.GrandTotalItems, result.Value.Pagination.TotalItems),
		})
	}
	data, resp := pagination.MergeEnvironmentPages(secretPagination, pages, params)

	return base.Paginated[podmantypes.Secret]{
		Success:            true,
		Data:               data,
		Pagination:         handlerutil.PaginationResponse(resp),
		FailedEnvironments: collection.Failures,
	}, nil
}

func (s *Service) Inspect(ctx context.Context, nameOrID string) (podmantypes.Secret, error) {
	rt, err := s.runtime(ctx)
	if err != nil {
		return podmantypes.Secret{}, err
	}
	return rt.Client.SecretInspect(ctx, nameOrID)
}

func (s *Service) Create(ctx context.Context, req podmantypes.SecretCreateRequest) (string, error) {
	rt, err := s.runtime(ctx)
	if err != nil {
		return "", err
	}
	return rt.Client.SecretCreate(ctx, req, false)
}

func (s *Service) Update(ctx context.Context, name string, req podmantypes.SecretUpdateRequest) (podmantypes.Secret, error) {
	rt, err := s.runtime(ctx)
	if err != nil {
		return podmantypes.Secret{}, err
	}
	if _, replaceErr := rt.Client.SecretCreate(ctx, podmantypes.SecretCreateRequest{
		Name:          name,
		Data:          req.Data,
		Driver:        req.Driver,
		DriverOptions: req.DriverOptions,
		Labels:        req.Labels,
	}, true); replaceErr != nil {
		return podmantypes.Secret{}, replaceErr
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
