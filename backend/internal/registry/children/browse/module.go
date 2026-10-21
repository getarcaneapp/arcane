package browse

import (
	"github.com/danielgtaylor/huma/v2"

	"github.com/getarcaneapp/arcane/backend/v2/internal/middleware"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/authz"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/utils/handlerutil"
)

// RegisterRoutes registers the registry browse endpoints.
func RegisterRoutes(api huma.API, h *Handler) {
	middleware.RegisterWithPermission(api, huma.Operation{
		OperationID: "listContainerRegistryRepositories",
		Method:      "GET",
		Path:        "/registries/{id}/repositories",
		Summary:     "List registry repositories",
		Description: "List the repositories stored in a container registry through its catalog API",
		Tags:        []string{"Container Registries"},
		Security:    handlerutil.DefaultOperationSecurity(),
	}, authz.PermRegistriesBrowse, h.ListRepositories)

	middleware.RegisterWithPermission(api, huma.Operation{
		OperationID: "listContainerRegistryTags",
		Method:      "GET",
		Path:        "/registries/{id}/tags",
		Summary:     "List repository tags",
		Description: "List the tags of a registry repository with their manifest details",
		Tags:        []string{"Container Registries"},
		Security:    handlerutil.DefaultOperationSecurity(),
	}, authz.PermRegistriesBrowse, h.ListTags)

	middleware.RegisterWithPermission(api, huma.Operation{
		OperationID: "deleteContainerRegistryTag",
		Method:      "DELETE",
		Path:        "/registries/{id}/tags",
		Summary:     "Delete a repository tag",
		Description: "Delete the manifest a tag points to, which also removes every tag sharing its digest",
		Tags:        []string{"Container Registries"},
		Security:    handlerutil.DefaultOperationSecurity(),
	}, authz.PermRegistriesDeleteTags, h.DeleteTag)
}
