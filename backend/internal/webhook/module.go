// Package webhook owns inbound webhook targets: the tokens that authenticate
// them, the actions they trigger, and the HTTP surface that manages them.
package webhook

import (
	"github.com/danielgtaylor/huma/v2"
	"github.com/getarcaneapp/arcane/backend/v2/internal/environment"
	"github.com/getarcaneapp/arcane/backend/v2/internal/event"
	"github.com/getarcaneapp/arcane/backend/v2/internal/gitops"
	"github.com/getarcaneapp/arcane/backend/v2/internal/project"
	"github.com/getarcaneapp/arcane/backend/v2/internal/updater"

	"github.com/getarcaneapp/arcane/backend/v2/internal/container"
	"github.com/getarcaneapp/arcane/backend/v2/internal/database"
)

// Dependencies are the collaborators the webhook domain needs.
type Dependencies struct {
	DB          *database.DB
	Container   *container.ContainerService
	Updater     *updater.UpdaterService
	Project     *project.ProjectService
	GitOpsSync  *gitops.GitOpsSyncService
	Event       *event.EventService
	Environment *environment.EnvironmentService
}

// Module wires the webhook domain and mounts its routes.
type Module struct {
	service *WebhookService
}

// New builds the webhook domain from its dependencies.
func New(deps Dependencies) *Module {
	return &Module{
		service: NewWebhookService(deps.DB, deps.Container, deps.Updater, deps.Project, deps.GitOpsSync, deps.Event, deps.Environment),
	}
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
