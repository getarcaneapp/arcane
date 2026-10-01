// Package activity owns background activity persistence, execution tracking,
// streaming, and the HTTP surface used to inspect and cancel work.
package activity

import (
	"github.com/danielgtaylor/huma/v2"
)

type Module struct {
	service *ActivityService
	handler *ActivityHandler
}

func New(service *ActivityService, environment EnvironmentDependencies) *Module {
	return &Module{service: service, handler: NewHandler(service, environment)}
}

func (m *Module) Service() *ActivityService {
	if m == nil {
		return nil
	}
	return m.service
}

func (m *Module) Handler() *ActivityHandler {
	if m == nil {
		return nil
	}
	return m.handler
}

func (m *Module) RegisterRoutes(api huma.API) {
	if m == nil {
		RegisterActivities(api, NewHandler(nil, EnvironmentDependencies{}))
		return
	}
	RegisterActivities(api, m.handler)
}
