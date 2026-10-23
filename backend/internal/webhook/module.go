// Package webhook owns inbound webhook targets: the tokens that authenticate
// them, the actions they trigger, and the HTTP surface that manages them.
package webhook

import (
	"net/http"

	"github.com/danielgtaylor/huma/v2"

	"github.com/getarcaneapp/arcane/backend/v3/internal/middleware"
	"github.com/getarcaneapp/arcane/backend/v3/pkg/authz"
	"github.com/getarcaneapp/arcane/backend/v3/pkg/utils/handlerutil"
)

// Module wires the webhook domain and mounts its routes.
type Module struct {
	service *WebhookService
}

// New assembles webhook routes around the provided service.
func New(service *WebhookService) *Module {
	return &Module{service: service}
}

// Service exposes the webhook service to collaborators that trigger webhooks
// outside the HTTP surface.
func (m *Module) Service() *WebhookService {
	if m == nil {
		return nil
	}
	return m.service
}

// RegisterRoutes mounts the webhook endpoints. A nil module still registers, so
// OpenAPI spec generation can discover the routes without a service graph.
func (m *Module) RegisterRoutes(api huma.API) {
	if m == nil {
		RegisterWebhooks(api, nil)
		return
	}
	RegisterWebhooks(api, m.service)
}

// RegisterWebhooks registers the authenticated CRUD routes for webhook management.
func RegisterWebhooks(api huma.API, webhookService *WebhookService) {
	h := &WebhookHandler{webhookService: webhookService}

	middleware.RegisterWithPermission(api, huma.Operation{
		OperationID: "list-webhooks",
		Method:      http.MethodGet,
		Path:        "/environments/{id}/webhooks",
		Summary:     "List webhooks",
		Description: "List all webhooks configured for this environment",
		Tags:        []string{"Webhooks"},
		Security:    handlerutil.DefaultOperationSecurity(),
		Metadata:    map[string]any{authz.MetaEnvironmentAggregate: true},
	}, authz.PermWebhooksList, h.ListWebhooks)

	middleware.RegisterWithPermission(api, huma.Operation{
		OperationID: "create-webhook",
		Method:      http.MethodPost,
		Path:        "/environments/{id}/webhooks",
		Summary:     "Create webhook",
		Description: "Create a webhook that triggers a container or stack update. The token is only returned once.",
		Tags:        []string{"Webhooks"},
		Security:    handlerutil.DefaultOperationSecurity(),
	}, authz.PermWebhooksCreate, h.CreateWebhook)

	middleware.RegisterWithPermission(api, huma.Operation{
		OperationID: "update-webhook",
		Method:      http.MethodPatch,
		Path:        "/environments/{id}/webhooks/{webhookId}",
		Summary:     "Update webhook",
		Description: "Update a webhook's enabled state",
		Tags:        []string{"Webhooks"},
		Security:    handlerutil.DefaultOperationSecurity(),
	}, authz.PermWebhooksUpdate, h.UpdateWebhook)

	middleware.RegisterWithPermission(api, huma.Operation{
		OperationID: "delete-webhook",
		Method:      http.MethodDelete,
		Path:        "/environments/{id}/webhooks/{webhookId}",
		Summary:     "Delete webhook",
		Description: "Delete a webhook by ID",
		Tags:        []string{"Webhooks"},
		Security:    handlerutil.DefaultOperationSecurity(),
	}, authz.PermWebhooksDelete, h.DeleteWebhook)
}
