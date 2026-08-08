// Package notification owns notification settings, dispatch, and HTTP routes.
package notification

import (
	"github.com/danielgtaylor/huma/v2"

	"github.com/getarcaneapp/arcane/backend/v2/internal/config"
)

type Module struct {
	service *NotificationService
	config  *config.Config
}

func New(service *NotificationService, cfg *config.Config) *Module {
	return &Module{service: service, config: cfg}
}

func (m *Module) Service() *NotificationService {
	if m == nil {
		return nil
	}
	return m.service
}

func (m *Module) RegisterRoutes(api huma.API) {
	if m == nil {
		RegisterNotifications(api, nil, nil)
		return
	}
	RegisterNotifications(api, m.service, m.config)
}
