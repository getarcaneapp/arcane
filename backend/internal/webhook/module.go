package webhook

import (
	"net/http"

	"github.com/danielgtaylor/huma/v2"

	"getarcane.dev/arcane/v3/internal/middleware"
	"getarcane.dev/arcane/v3/pkg/authz"
	"getarcane.dev/arcane/v3/pkg/handlerutil"
)

type (
	Module struct {
		service *WebhookService
	}
)

func New(service *WebhookService) *Module {
	return &Module{service: service}
}

func (m *Module) Service() *WebhookService {
	if m == nil {
		return nil
	}
	return m.service
}

func (m *Module) RegisterRoutes(api huma.API, appCtx handlerutil.ActivityAppContext) {
	RegisterWebhooks(api, m.Service(), appCtx)
}

func RegisterWebhooks(api huma.API, webhookService *WebhookService, appCtx handlerutil.ActivityAppContext) {
	h := &WebhookHandler{
		webhookService: webhookService,
		appCtx:         appCtx,
	}

	huma.Register(api, huma.Operation{
		OperationID: "trigger-webhook",
		Method:      http.MethodPost,
		Path:        "/webhooks/trigger/{token}",
		Summary:     "Trigger webhook",
		Tags:        []string{"Webhooks"},
		Hidden:      true,
		Security:    []map[string][]string{},
		Metadata:    map[string]any{authz.MetaAccess: authz.EndpointAccessWebhookToken},
	}, h.TriggerWebhook)

	middleware.RegisterWithPermission(api, huma.Operation{
		OperationID: "list-webhooks",
		Method:      http.MethodGet,
		Path:        "/agents/{id}/webhooks",
		Summary:     "List webhooks",
		Description: "List all webhooks configured for this environment",
		Tags:        []string{"Webhooks"},
		Security:    handlerutil.DefaultOperationSecurity(),
		Metadata:    map[string]any{authz.MetaEnvironmentAggregate: true},
	}, authz.PermWebhooksList, h.ListWebhooks)

	middleware.RegisterWithPermission(api, huma.Operation{
		OperationID: "create-webhook",
		Method:      http.MethodPost,
		Path:        "/agents/{id}/webhooks",
		Summary:     "Create webhook",
		Description: "Create a webhook that triggers a container or stack update. The token is only returned once.",
		Tags:        []string{"Webhooks"},
		Security:    handlerutil.DefaultOperationSecurity(),
	}, authz.PermWebhooksCreate, h.CreateWebhook)

	middleware.RegisterWithPermission(api, huma.Operation{
		OperationID: "update-webhook",
		Method:      http.MethodPatch,
		Path:        "/agents/{id}/webhooks/{webhookId}",
		Summary:     "Update webhook",
		Description: "Update a webhook's enabled state",
		Tags:        []string{"Webhooks"},
		Security:    handlerutil.DefaultOperationSecurity(),
	}, authz.PermWebhooksUpdate, h.UpdateWebhook)

	middleware.RegisterWithPermission(api, huma.Operation{
		OperationID: "delete-webhook",
		Method:      http.MethodDelete,
		Path:        "/agents/{id}/webhooks/{webhookId}",
		Summary:     "Delete webhook",
		Description: "Delete a webhook by ID",
		Tags:        []string{"Webhooks"},
		Security:    handlerutil.DefaultOperationSecurity(),
	}, authz.PermWebhooksDelete, h.DeleteWebhook)
}
