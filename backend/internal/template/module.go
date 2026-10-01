// Package template owns compose templates and the template HTTP surface.
package template

import (
	"github.com/danielgtaylor/huma/v2"
)

type Module struct {
	service *TemplateService
}

func New(service *TemplateService) *Module {
	return &Module{service: service}
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
