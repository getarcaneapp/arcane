// Package updater owns automatic container and project updates: deciding what
// is out of date, applying the update, and the HTTP surface that drives it.
package updater

import (
	"github.com/danielgtaylor/huma/v2"
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
	if m == nil {
		RegisterUpdater(api, nil, appCtx)
		return
	}
	RegisterUpdater(api, m.service, appCtx)
}
