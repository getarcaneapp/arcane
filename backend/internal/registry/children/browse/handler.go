package browse

import (
	"context"

	"github.com/danielgtaylor/huma/v2"

	"getarcane.dev/arcane/v3/internal/common"
	"getarcane.dev/arcane/v3/pkg/handlerutil"
	"getarcane.dev/types/v3"
)

type (
	Handler struct {
		service *Service
	}

	ListRegistryRepositoriesInput struct {
		ID     string `path:"id" doc:"Registry ID"`
		Search string `query:"search" doc:"Search query"`
		Sort   string `query:"sort" doc:"Column to sort by"`
		Order  string `query:"order" default:"asc" doc:"Sort direction"`
		Start  int    `query:"start" default:"0" doc:"Start index"`
		Limit  int    `query:"limit" default:"20" doc:"Items per page"`
	}

	ListRegistryTagsInput struct {
		ID         string `path:"id" doc:"Registry ID"`
		Repository string `query:"repository" required:"true" minLength:"1" doc:"Repository name"`
		Search     string `query:"search" doc:"Search query"`
		Sort       string `query:"sort" doc:"Column to sort by"`
		Order      string `query:"order" default:"asc" doc:"Sort direction"`
		Start      int    `query:"start" default:"0" doc:"Start index"`
		Limit      int    `query:"limit" default:"20" doc:"Items per page"`
	}

	DeleteRegistryTagInput struct {
		ID         string `path:"id" doc:"Registry ID"`
		Repository string `query:"repository" required:"true" minLength:"1" doc:"Repository name"`
		Tag        string `query:"tag" required:"true" minLength:"1" doc:"Tag to delete"`
	}
)

func NewHandler(service *Service) *Handler {
	return &Handler{service: service}
}

func (h *Handler) ListRepositories(ctx context.Context, input *ListRegistryRepositoriesInput) (*handlerutil.Page[types.ContainerRegistryRepository], error) {
	params := handlerutil.PaginationParams(input.Start, input.Limit, input.Sort, input.Order, input.Search)

	repositories, paginationResp, err := h.service.ListRepositories(ctx, input.ID, params)
	if err != nil {
		apiErr := common.ToAPIError(err)
		return nil, huma.NewError(apiErr.HTTPStatus(), "Failed to list repositories: "+err.Error())
	}

	return &handlerutil.Page[types.ContainerRegistryRepository]{
		Body: types.Paginated[types.ContainerRegistryRepository]{
			Success:    true,
			Data:       repositories,
			Pagination: handlerutil.PaginationResponse(paginationResp),
		},
	}, nil
}

func (h *Handler) ListTags(ctx context.Context, input *ListRegistryTagsInput) (*handlerutil.Page[types.ContainerRegistryRepositoryTag], error) {
	params := handlerutil.PaginationParams(input.Start, input.Limit, input.Sort, input.Order, input.Search)

	tags, paginationResp, err := h.service.ListRepositoryTags(ctx, input.ID, input.Repository, params)
	if err != nil {
		apiErr := common.ToAPIError(err)
		return nil, huma.NewError(apiErr.HTTPStatus(), "Failed to list tags: "+err.Error())
	}

	return &handlerutil.Page[types.ContainerRegistryRepositoryTag]{
		Body: types.Paginated[types.ContainerRegistryRepositoryTag]{
			Success:    true,
			Data:       tags,
			Pagination: handlerutil.PaginationResponse(paginationResp),
		},
	}, nil
}

func (h *Handler) DeleteTag(ctx context.Context, input *DeleteRegistryTagInput) (*handlerutil.Out[types.ContainerRegistryDeleteTagResponse], error) {
	digest, err := h.service.DeleteRepositoryTag(ctx, input.ID, input.Repository, input.Tag)
	if err != nil {
		apiErr := common.ToAPIError(err)
		return nil, huma.NewError(apiErr.HTTPStatus(), "Failed to delete tag: "+err.Error())
	}

	return &handlerutil.Out[types.ContainerRegistryDeleteTagResponse]{
		Body: types.ApiResponse[types.ContainerRegistryDeleteTagResponse]{
			Success: true,
			Data:    types.ContainerRegistryDeleteTagResponse{Digest: digest},
		},
	}, nil
}
