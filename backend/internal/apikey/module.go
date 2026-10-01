// Package apikey owns API-key persistence, permission grants, validation, and
// the API-key HTTP surface.
package apikey

import (
	"github.com/danielgtaylor/huma/v2"
)

type Module struct {
	service *ApiKeyService
}

func New(service *ApiKeyService) *Module {
	return &Module{service: service}
}

func (m *Module) Service() *ApiKeyService {
	if m == nil {
		return nil
	}
	return m.service
}

func (m *Module) RegisterRoutes(api huma.API) {
	if m == nil {
		RegisterApiKeys(api, nil)
		return
	}
	RegisterApiKeys(api, m.service)
}
