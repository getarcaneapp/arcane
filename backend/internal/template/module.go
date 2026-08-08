// Package template owns compose templates and the template HTTP surface.
package template

import (
	"context"
	"net/http"

	"github.com/danielgtaylor/huma/v2"

	"github.com/getarcaneapp/arcane/backend/v2/internal/database"
	"github.com/getarcaneapp/arcane/backend/v2/internal/settings"
)

type Dependencies struct {
	Context    context.Context
	DB         *database.DB
	HTTPClient *http.Client
	Settings   *settings.SettingsService
}

type Module struct {
	service *TemplateService
}

func New(deps Dependencies) *Module {
	return &Module{service: NewTemplateService(deps.Context, deps.DB, deps.HTTPClient, deps.Settings)}
}

func (m *Module) Service() *TemplateService {
	if m == nil {
		return nil
	}
	return m.service
}

func (m *Module) RegisterRoutes(api huma.API) {
	if m == nil {
		RegisterTemplates(api, nil)
		return
	}
	RegisterTemplates(api, m.service)
}
