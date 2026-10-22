package registry

import (
	"context"

	"github.com/danielgtaylor/huma/v2"

	"github.com/getarcaneapp/arcane/backend/v3/internal/middleware"
	"github.com/getarcaneapp/arcane/backend/v3/internal/registry/children/browse"
	"github.com/getarcaneapp/arcane/backend/v3/pkg/authz"
	"github.com/getarcaneapp/arcane/backend/v3/pkg/utils/handlerutil"
)

type Module struct {
	service       *ContainerRegistryService
	handler       *ContainerRegistryHandler
	browseHandler *browse.Handler
}

func New(service *ContainerRegistryService, syncRemoteRegistries func(context.Context)) *Module {
	return &Module{
		service:       service,
		handler:       NewHandler(service, syncRemoteRegistries),
		browseHandler: browse.NewHandler(service.browse),
	}
}

func (m *Module) Handler() *ContainerRegistryHandler {
	if m == nil {
		return nil
	}
	return m.handler
}

func (m *Module) Service() *ContainerRegistryService {
	if m == nil {
		return nil
	}
	return m.service
}

func (m *Module) RegisterRoutes(api huma.API) {
	if m == nil {
		RegisterContainerRegistries(api, NewHandler(nil, nil))
		browse.RegisterRoutes(api, browse.NewHandler(nil))
		return
	}
	RegisterContainerRegistries(api, m.handler)
	browse.RegisterRoutes(api, m.browseHandler)
}

func RegisterContainerRegistries(api huma.API, h *ContainerRegistryHandler) {
	huma.Register(api, huma.Operation{
		OperationID: "listContainerRegistries",
		Method:      "GET",
		Path:        "/registries",
		Summary:     "List container registries",
		Description: "Get a paginated list of container registries",
		Tags:        []string{"Container Registries"},
		Security:    handlerutil.DefaultOperationSecurity(),
		Middlewares: middleware.RequirePermission(api, authz.PermRegistriesList),
	}, h.ListRegistries)

	huma.Register(api, huma.Operation{
		OperationID: "createContainerRegistry",
		Method:      "POST",
		Path:        "/registries",
		Summary:     "Create a container registry",
		Description: "Create a new container registry",
		Tags:        []string{"Container Registries"},
		Security:    handlerutil.DefaultOperationSecurity(),
		Middlewares: middleware.RequirePermission(api, authz.PermRegistriesCreate),
	}, h.CreateRegistry)

	huma.Register(api, huma.Operation{
		OperationID: "syncContainerRegistries",
		Method:      "POST",
		Path:        "/registries/sync",
		Summary:     "Sync container registries",
		Description: "Sync container registries from a remote source",
		Tags:        []string{"Container Registries"},
		Security:    handlerutil.DefaultOperationSecurity(),
		Middlewares: middleware.RequirePermission(api, authz.PermRegistriesUpdate),
	}, h.SyncRegistries)

	huma.Register(api, huma.Operation{
		OperationID: "getContainerRegistryPullUsage",
		Method:      "GET",
		Path:        "/registries/usage",
		Summary:     "Get container registry pull usage",
		Description: "Get configured registry pull usage and rate limit visibility",
		Tags:        []string{"Container Registries"},
		Security:    handlerutil.DefaultOperationSecurity(),
		Middlewares: middleware.RequirePermission(api, authz.PermRegistriesRead),
	}, h.GetPullUsage)

	huma.Register(api, huma.Operation{
		OperationID: "getContainerRegistry",
		Method:      "GET",
		Path:        "/registries/{id}",
		Summary:     "Get a container registry",
		Description: "Get a container registry by ID",
		Tags:        []string{"Container Registries"},
		Security:    handlerutil.DefaultOperationSecurity(),
		Middlewares: middleware.RequirePermission(api, authz.PermRegistriesRead),
	}, h.GetRegistry)

	huma.Register(api, huma.Operation{
		OperationID: "updateContainerRegistry",
		Method:      "PATCH",
		Path:        "/registries/{id}",
		Summary:     "Update a container registry",
		Description: "Update an existing container registry",
		Tags:        []string{"Container Registries"},
		Security:    handlerutil.DefaultOperationSecurity(),
		Middlewares: middleware.RequirePermission(api, authz.PermRegistriesUpdate),
	}, h.UpdateRegistry)

	huma.Register(api, huma.Operation{
		OperationID: "deleteContainerRegistry",
		Method:      "DELETE",
		Path:        "/registries/{id}",
		Summary:     "Delete a container registry",
		Description: "Delete a container registry by ID",
		Tags:        []string{"Container Registries"},
		Security:    handlerutil.DefaultOperationSecurity(),
		Middlewares: middleware.RequirePermission(api, authz.PermRegistriesDelete),
	}, h.DeleteRegistry)

	middleware.RegisterWithActionPermissions(api, huma.Operation{
		OperationID: "containerRegistryAction",
		Method:      "POST",
		Path:        "/registries/{id}",
		Summary:     "Test a container registry",
		Description: "Test connectivity and authentication to a container registry",
		Tags:        []string{"Container Registries"},
		Security:    handlerutil.DefaultOperationSecurity(),
	}, "action", map[string]string{
		"test": authz.PermRegistriesTest,
	}, h.TestRegistry)
}
