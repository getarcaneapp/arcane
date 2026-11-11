package updater

import (
	"net/http"
	"reflect"

	"github.com/danielgtaylor/huma/v2"

	"getarcane.dev/arcane/v3/internal/middleware"
	"getarcane.dev/arcane/v3/pkg/authz"
	"getarcane.dev/arcane/v3/pkg/handlerutil"
	"getarcane.dev/types/v3"
)

type (
	Module struct {
		service *UpdaterService
	}
)

func New(service *UpdaterService) *Module {
	return &Module{service: service}
}

func (m *Module) Service() *UpdaterService {
	if m == nil {
		return nil
	}
	return m.service
}

func (m *Module) RegisterRoutes(api huma.API, appCtx handlerutil.ActivityAppContext) {
	RegisterUpdater(api, m.Service(), appCtx)
}

func RegisterUpdater(api huma.API, updaterService *UpdaterService, appCtx handlerutil.ActivityAppContext) {
	h := &UpdaterHandler{
		updaterService: updaterService,
		appCtx:         appCtx.Context(),
	}

	middleware.RegisterWithPermission(api, huma.Operation{
		OperationID: "check-project-updates",
		Method:      http.MethodPost,
		Path:        "/agents/{id}/updater/projects/{projectId}/check",
		Summary:     "Check project service updates",
		Tags:        []string{"Updater"},
		Security:    handlerutil.DefaultOperationSecurity(),
	}, authz.PermImageUpdatesCheck, h.CheckProjectUpdates)
	middleware.RegisterWithPermission(api, huma.Operation{
		OperationID: "run-updater",
		Method:      http.MethodPost,
		Path:        "/agents/{id}/updater/run",
		Summary:     "Run updater",
		Description: "Apply pending container updates",
		Tags:        []string{"Updater"},
		Security:    handlerutil.DefaultOperationSecurity(),
	}, authz.PermImageUpdatesApply, h.RunUpdater)

	middleware.RegisterWithPermission(api, huma.Operation{
		OperationID: "get-updater-status",
		Method:      http.MethodGet,
		Path:        "/agents/{id}/updater/status",
		Summary:     "Get updater status",
		Description: "Get the current status of the updater",
		Tags:        []string{"Updater"},
		Security:    handlerutil.DefaultOperationSecurity(),
	}, authz.PermImageUpdatesRead, h.GetUpdaterStatus)

	middleware.RegisterWithPermission(api, huma.Operation{
		OperationID: "get-updater-history",
		Method:      http.MethodGet,
		Path:        "/agents/{id}/updater/history",
		Summary:     "Get updater history",
		Description: "Get the history of update operations",
		Tags:        []string{"Updater"},
		Security:    handlerutil.DefaultOperationSecurity(),
		Metadata:    map[string]any{authz.MetaEnvironmentAggregate: true},
	}, authz.PermImageUpdatesList, h.GetUpdaterHistory)

	acceptedSchema := huma.SchemaFromType(api.OpenAPI().Components.Schemas, reflect.TypeFor[types.ApiResponse[types.Activity]]())
	completedSchema := huma.SchemaFromType(api.OpenAPI().Components.Schemas, reflect.TypeFor[types.ApiResponse[*types.UpdaterResult]]())
	middleware.RegisterWithPermission(api, huma.Operation{
		OperationID: "update-container",
		Method:      http.MethodPost,
		Path:        "/agents/{id}/containers/{containerId}/update",
		Summary:     "Update a single container",
		Description: "Pull the latest image and apply the appropriate update strategy for a specific container",
		Tags:        []string{"Updater", "Containers"},
		Security:    handlerutil.DefaultOperationSecurity(),
		Responses: map[string]*huma.Response{
			"200": {Description: "Container update completed", Content: map[string]*huma.MediaType{"application/json": {Schema: completedSchema}}},
			"202": {Description: "Container update accepted", Content: map[string]*huma.MediaType{"application/json": {Schema: acceptedSchema}}},
		},
	}, authz.PermImageUpdatesApply, h.updateContainer)
}
