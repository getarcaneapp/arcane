package browse

import (
	"context"

	"github.com/danielgtaylor/huma/v2"
	"github.com/getarcaneapp/arcane/types/v3"

	"github.com/getarcaneapp/arcane/backend/v3/internal/common"
	"github.com/getarcaneapp/arcane/backend/v3/pkg/utils/handlerutil"
)

// Handler serves registry repository and tag browsing.
type Handler struct {
	service *Service
}

// ListRegistryRepositoriesInput is the input for listing the repositories of a registry.
type ListRegistryRepositoriesInput struct {
	ID     string `path:"id" doc:"Registry ID"`
	Search string `query:"search" doc:"Search query"`
	Sort   string `query:"sort" doc:"Column to sort by"`
	Order  string `query:"order" default:"asc" doc:"Sort direction"`
	Start  int    `query:"start" default:"0" doc:"Start index"`
	Limit  int    `query:"limit" default:"20" doc:"Items per page"`
}

// ListRegistryTagsInput is the input for listing the tags of a registry repository.
type ListRegistryTagsInput struct {
	ID         string `path:"id" doc:"Registry ID"`
	Repository string `query:"repository" required:"true" minLength:"1" doc:"Repository name"`
	Search     string `query:"search" doc:"Search query"`
	Sort       string `query:"sort" doc:"Column to sort by"`
	Order      string `query:"order" default:"asc" doc:"Sort direction"`
	Start      int    `query:"start" default:"0" doc:"Start index"`
	Limit      int    `query:"limit" default:"20" doc:"Items per page"`
}

// DeleteRegistryTagInput is the input for deleting a tag from a registry repository.
type DeleteRegistryTagInput struct {
	ID         string `path:"id" doc:"Registry ID"`
	Repository string `query:"repository" required:"true" minLength:"1" doc:"Repository name"`
	Tag        string `query:"tag" required:"true" minLength:"1" doc:"Tag to delete"`
}

func NewHandler(service *Service) *Handler {
	return &Handler{service: service}
}

// ListRepositories returns a paginated list of repositories stored in a registry.
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

// ListTags returns a paginated list of tags of a registry repository.
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

// DeleteTag deletes the manifest a repository tag points to.
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
