package registry

import (
	"context"

	"emperror.dev/errors"
	"github.com/danielgtaylor/huma/v2"
	"github.com/getarcaneapp/arcane/backend/v2/internal/common"
	"github.com/getarcaneapp/arcane/backend/v2/internal/middleware"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/authz"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/utils/handlerutil"
	"github.com/getarcaneapp/arcane/types/v2/base"
	"github.com/getarcaneapp/arcane/types/v2/containerregistry"
)

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

func registerContainerRegistryBrowseInternal(api huma.API, h *ContainerRegistryHandler) {
	middleware.RegisterWithPermission(api, huma.Operation{
		OperationID: "listContainerRegistryRepositories",
		Method:      "GET",
		Path:        "/container-registries/{id}/repositories",
		Summary:     "List registry repositories",
		Description: "List the repositories stored in a container registry through its catalog API",
		Tags:        []string{"Container Registries"},
		Security:    handlerutil.DefaultOperationSecurity(),
	}, authz.PermRegistriesBrowse, h.ListRepositories)

	middleware.RegisterWithPermission(api, huma.Operation{
		OperationID: "listContainerRegistryTags",
		Method:      "GET",
		Path:        "/container-registries/{id}/tags",
		Summary:     "List repository tags",
		Description: "List the tags of a registry repository with their manifest details",
		Tags:        []string{"Container Registries"},
		Security:    handlerutil.DefaultOperationSecurity(),
	}, authz.PermRegistriesBrowse, h.ListTags)

	middleware.RegisterWithPermission(api, huma.Operation{
		OperationID: "deleteContainerRegistryTag",
		Method:      "DELETE",
		Path:        "/container-registries/{id}/tags",
		Summary:     "Delete a repository tag",
		Description: "Delete the manifest a tag points to, which also removes every tag sharing its digest",
		Tags:        []string{"Container Registries"},
		Security:    handlerutil.DefaultOperationSecurity(),
	}, authz.PermRegistriesDeleteTags, h.DeleteTag)
}

// ListRepositories returns a paginated list of repositories stored in a registry.
func (h *ContainerRegistryHandler) ListRepositories(ctx context.Context, input *ListRegistryRepositoriesInput) (*handlerutil.Page[containerregistry.Repository], error) {
	params := handlerutil.PaginationParams(input.Start, input.Limit, input.Sort, input.Order, input.Search)

	repositories, paginationResp, err := h.registryService.ListRepositories(ctx, input.ID, params)
	if err != nil {
		apiErr := common.ToAPIError(err)
		return nil, huma.NewError(apiErr.HTTPStatus(), errors.WithMessage(err, "Failed to list repositories").Error())
	}

	return &handlerutil.Page[containerregistry.Repository]{
		Body: base.Paginated[containerregistry.Repository]{
			Success:    true,
			Data:       repositories,
			Pagination: handlerutil.PaginationResponse(paginationResp),
		},
	}, nil
}

// ListTags returns a paginated list of tags of a registry repository.
func (h *ContainerRegistryHandler) ListTags(ctx context.Context, input *ListRegistryTagsInput) (*handlerutil.Page[containerregistry.RepositoryTag], error) {
	params := handlerutil.PaginationParams(input.Start, input.Limit, input.Sort, input.Order, input.Search)

	tags, paginationResp, err := h.registryService.ListRepositoryTags(ctx, input.ID, input.Repository, params)
	if err != nil {
		apiErr := common.ToAPIError(err)
		return nil, huma.NewError(apiErr.HTTPStatus(), errors.WithMessage(err, "Failed to list tags").Error())
	}

	return &handlerutil.Page[containerregistry.RepositoryTag]{
		Body: base.Paginated[containerregistry.RepositoryTag]{
			Success:    true,
			Data:       tags,
			Pagination: handlerutil.PaginationResponse(paginationResp),
		},
	}, nil
}

// DeleteTag deletes the manifest a repository tag points to.
func (h *ContainerRegistryHandler) DeleteTag(ctx context.Context, input *DeleteRegistryTagInput) (*handlerutil.Out[containerregistry.DeleteTagResponse], error) {
	digest, err := h.registryService.DeleteRepositoryTag(ctx, input.ID, input.Repository, input.Tag)
	if err != nil {
		apiErr := common.ToAPIError(err)
		return nil, huma.NewError(apiErr.HTTPStatus(), errors.WithMessage(err, "Failed to delete tag").Error())
	}

	return &handlerutil.Out[containerregistry.DeleteTagResponse]{
		Body: base.ApiResponse[containerregistry.DeleteTagResponse]{
			Success: true,
			Data:    containerregistry.DeleteTagResponse{Digest: digest},
		},
	}, nil
}
