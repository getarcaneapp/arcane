// Package webhook owns inbound webhook targets: the tokens that authenticate
// them, the actions they trigger, and the HTTP surface that manages them.
package webhook

import (
	"github.com/danielgtaylor/huma/v2"
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
