// Package registry owns registry credentials, digest inspection, pull usage,
// synchronization, and the registry HTTP surface.
package registry

import (
	"context"

	"github.com/danielgtaylor/huma/v2"
)

type Module struct {
	service *ContainerRegistryService
	handler *ContainerRegistryHandler
}

func New(service *ContainerRegistryService, syncRemoteRegistries func(context.Context) error) *Module {
	return &Module{service: service, handler: NewHandler(service, syncRemoteRegistries)}
}

func (m *Module) Handler() *ContainerRegistryHandler {
	if m == nil {
		return nil
	}
	return m.handler
}

func (m *Module) Service() *ContainerRegistryService {
	if m == nil {
		return nil
	}
	return m.service
}

func (m *Module) RegisterRoutes(api huma.API) {
	if m == nil {
		RegisterContainerRegistries(api, NewHandler(nil, nil))
		return
	}
	RegisterContainerRegistries(api, m.handler)
}
