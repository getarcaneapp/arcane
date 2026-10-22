// Package activity owns background activity persistence, execution tracking,
// streaming, and the HTTP surface used to inspect and cancel work.
package activity

import (
	"net/http"

	"github.com/danielgtaylor/huma/v2"

	"github.com/getarcaneapp/arcane/backend/v3/internal/middleware"
	"github.com/getarcaneapp/arcane/backend/v3/pkg/authz"
	"github.com/getarcaneapp/arcane/backend/v3/pkg/utils/handlerutil"
)

type Module struct {
	service *ActivityService
	handler *ActivityHandler
}

func New(service *ActivityService, environment EnvironmentDependencies) *Module {
	return &Module{service: service, handler: NewHandler(service, environment)}
}

func (m *Module) Service() *ActivityService {
	if m == nil {
		return nil
	}
	return m.service
}

func (m *Module) Handler() *ActivityHandler {
	if m == nil {
		return nil
	}
	return m.handler
}

func (m *Module) RegisterRoutes(api huma.API) {
	if m == nil {
		RegisterActivities(api, NewHandler(nil, EnvironmentDependencies{}))
		return
	}
	RegisterActivities(api, m.handler)
}

func RegisterActivities(api huma.API, h *ActivityHandler) {
	middleware.RegisterWithPermission(api, huma.Operation{
		OperationID: "list-activities",
		Method:      http.MethodGet,
		Path:        "/environments/{id}/activities",
		Summary:     "List background activities",
		Description: "Get current and recent background activities for an environment",
		Tags:        []string{"Activities"},
		Security:    handlerutil.DefaultOperationSecurity(),
	}, authz.PermActivitiesRead, h.ListActivities)

	middleware.RegisterWithPermission(api, huma.Operation{
		OperationID: "get-activity",
		Method:      http.MethodGet,
		Path:        "/environments/{id}/activities/{activityId}",
		Summary:     "Get background activity",
		Description: "Get a background activity with its recent output messages",
		Tags:        []string{"Activities"},
		Security:    handlerutil.DefaultOperationSecurity(),
	}, authz.PermActivitiesRead, h.GetActivity)

	middleware.RegisterWithActionPermissions(api, huma.Operation{
		OperationID: "activity-action",
		Method:      http.MethodPost,
		Path:        "/environments/{id}/activities/{activityId}",
		Summary:     "Cancel a background activity",
		Description: "Request cancellation of a running or queued background activity",
		Tags:        []string{"Activities"},
		Security:    handlerutil.DefaultOperationSecurity(),
	}, "action", map[string]string{
		"cancel": authz.PermActivitiesCancel,
	}, h.CancelActivity)

	middleware.RegisterWithPermission(api, huma.Operation{
		OperationID: "clear-activity-history",
		Method:      http.MethodDelete,
		Path:        "/environments/{id}/activities/history",
		Summary:     "Clear background activity history",
		Description: "Delete completed background activity history for an environment",
		Tags:        []string{"Activities"},
		Security:    handlerutil.DefaultOperationSecurity(),
	}, authz.PermActivitiesDelete, h.ClearHistory)
}
