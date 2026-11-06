package event

import (
	"context"
	"net/http"

	"github.com/danielgtaylor/huma/v2"

	"getarcane.dev/arcane/v3/internal/middleware"
	"getarcane.dev/arcane/v3/pkg/authz"
	"getarcane.dev/arcane/v3/pkg/handlerutil"
)

type (
	Module struct {
		service *EventService
	}
)

func New(service *EventService) *Module {
	return &Module{service: service}
}

func (m *Module) Service() *EventService {
	if m == nil {
		return nil
	}
	return m.service
}

func (m *Module) RegisterRoutes(api huma.API, resolveEnvironment func(context.Context, string) (string, error)) {
	RegisterEvents(api, m.Service(), resolveEnvironment)
}

func RegisterEvents(api huma.API, eventService *EventService, resolveEnvironment func(context.Context, string) (string, error)) {
	h := &EventHandler{
		eventService:       eventService,
		resolveEnvironment: resolveEnvironment,
	}

	huma.Register(api, huma.Operation{
		OperationID: "ingest-agent-event",
		Method:      http.MethodPost,
		Path:        "/events",
		Summary:     "Ingest an agent event",
		Tags:        []string{"Events"},
		Hidden:      true,
		Security:    []map[string][]string{},
		Metadata:    map[string]any{authz.MetaAccess: authz.EndpointAccessAgentToken},
	}, middleware.EchoHandler(h.IngestAgentEvent))

	middleware.RegisterWithPermission(api, huma.Operation{
		OperationID: "listEvents",
		Method:      "GET",
		Path:        "/events",
		Summary:     "List events",
		Description: "Get a paginated list of system events",
		Tags:        []string{"Events"},
		Security:    handlerutil.DefaultOperationSecurity(),
	}, authz.PermEventsList, h.ListEvents)

	middleware.RegisterWithPermission(api, huma.Operation{
		OperationID: "getEventStats",
		Method:      "GET",
		Path:        "/events/stats",
		Summary:     "Event severity counts",
		Description: "Get global event counts grouped by severity",
		Tags:        []string{"Events"},
		Security:    handlerutil.DefaultOperationSecurity(),
	}, authz.PermEventsList, h.GetEventStats)

	middleware.RegisterWithPermission(api, huma.Operation{
		OperationID: "deleteEvent",
		Method:      "DELETE",
		Path:        "/events/{eventId}",
		Summary:     "Delete an event",
		Description: "Delete a system event by ID",
		Tags:        []string{"Events"},
		Security:    handlerutil.DefaultOperationSecurity(),
	}, authz.PermEventsDelete, h.DeleteEvent)

	middleware.RegisterWithPermission(api, huma.Operation{
		OperationID: "getEventsByEnvironment",
		Method:      "GET",
		Path:        "/events/environment/{environmentId}",
		Summary:     "Get events by environment",
		Description: "Get a paginated list of events for a specific environment",
		Tags:        []string{"Events"},
		Security:    handlerutil.DefaultOperationSecurity(),
	}, authz.PermEventsList, h.GetEventsByEnvironment)

	middleware.RegisterWithPermission(api, huma.Operation{
		OperationID: "waitForEnvironmentChanges",
		Method:      http.MethodGet,
		Path:        "/environments/{id}/events/changes",
		Summary:     "Wait for environment changes",
		Description: "Long-poll the latest revision of each changed resource kind in an environment",
		Tags:        []string{"Events"},
		Security:    handlerutil.DefaultOperationSecurity(),
	}, authz.PermEventsList, h.WaitForEnvironmentChanges)
}
