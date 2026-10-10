// Package updater owns automatic container and project updates: deciding what
// is out of date, applying the update, and the HTTP surface that drives it.
package updater

import (
	"net/http"
	"reflect"

	"github.com/danielgtaylor/huma/v2"
	"github.com/getarcaneapp/arcane/types/v2/activity"
	"github.com/getarcaneapp/arcane/types/v2/base"
	"github.com/getarcaneapp/arcane/types/v2/updater"

	"github.com/getarcaneapp/arcane/backend/v2/internal/middleware"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/authz"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/utils/handlerutil"
)

// Module wires the updater domain and mounts its routes.
type Module struct {
	service *UpdaterService
}

// New assembles updater routes around the provided service.
func New(service *UpdaterService) *Module {
	return &Module{service: service}
}

// Service exposes the updater service to collaborators that trigger updates
// outside the HTTP surface, such as the auto-update job and webhooks.
func (m *Module) Service() *UpdaterService {
	if m == nil {
		return nil
	}
	return m.service
}

// RegisterRoutes mounts the updater endpoints. A nil module still registers, so
// OpenAPI spec generation can discover the routes without a service graph.
func (m *Module) RegisterRoutes(api huma.API, appCtx handlerutil.ActivityAppContext) {
	RegisterUpdater(api, m.Service(), appCtx)
}

// RegisterUpdater registers updater management routes using Huma.
func RegisterUpdater(api huma.API, updaterService *UpdaterService, appCtx handlerutil.ActivityAppContext) {
	h := &UpdaterHandler{
		updaterService: updaterService,
		appCtx:         appCtx.Context(),
	}

	middleware.RegisterWithPermission(api, huma.Operation{
		OperationID: "check-project-updates", Method: http.MethodPost,
		Path:    "/environments/{id}/updater/projects/{projectId}/check",
		Summary: "Check project service updates", Tags: []string{"Updater"}, Security: handlerutil.DefaultOperationSecurity(),
	}, authz.PermImageUpdatesCheck, h.CheckProjectUpdates)
	middleware.RegisterWithPermission(api, huma.Operation{
		OperationID: "run-updater",
		Method:      http.MethodPost,
		Path:        "/environments/{id}/updater/run",
		Summary:     "Run updater",
		Description: "Apply pending container updates",
		Tags:        []string{"Updater"},
		Security:    handlerutil.DefaultOperationSecurity(),
	}, authz.PermImageUpdatesCheck, h.RunUpdater)

	middleware.RegisterWithPermission(api, huma.Operation{
		OperationID: "get-updater-status",
		Method:      http.MethodGet,
		Path:        "/environments/{id}/updater/status",
		Summary:     "Get updater status",
		Description: "Get the current status of the updater",
		Tags:        []string{"Updater"},
		Security:    handlerutil.DefaultOperationSecurity(),
	}, authz.PermImageUpdatesRead, h.GetUpdaterStatus)

	middleware.RegisterWithPermission(api, huma.Operation{
		OperationID: "get-updater-history",
		Method:      http.MethodGet,
		Path:        "/environments/{id}/updater/history",
		Summary:     "Get updater history",
		Description: "Get the history of update operations",
		Tags:        []string{"Updater"},
		Security:    handlerutil.DefaultOperationSecurity(),
	}, authz.PermImageUpdatesRead, h.GetUpdaterHistory)

	acceptedSchema := huma.SchemaFromType(api.OpenAPI().Components.Schemas, reflect.TypeFor[base.ApiResponse[activity.Activity]]())
	completedSchema := huma.SchemaFromType(api.OpenAPI().Components.Schemas, reflect.TypeFor[base.ApiResponse[*updater.Result]]())
	middleware.RegisterWithPermission(api, huma.Operation{
		OperationID: "update-container",
		Method:      http.MethodPost,
		Path:        "/environments/{id}/containers/{containerId}/update",
		Summary:     "Update a single container",
		Description: "Pull the latest image and apply the appropriate update strategy for a specific container",
		Tags:        []string{"Updater", "Containers"},
		Security:    handlerutil.DefaultOperationSecurity(),
		Responses: map[string]*huma.Response{
			"200": {Description: "Container update completed", Content: map[string]*huma.MediaType{"application/json": {Schema: completedSchema}}},
			"202": {Description: "Container update accepted", Content: map[string]*huma.MediaType{"application/json": {Schema: acceptedSchema}}},
		},
	}, authz.PermImageUpdatesCheck, h.updateContainer)
}
