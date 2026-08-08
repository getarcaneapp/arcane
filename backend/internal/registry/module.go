// Package registry owns registry credentials, digest inspection, pull usage,
// synchronization, and the registry HTTP surface.
package registry

import (
	"context"

	"github.com/danielgtaylor/huma/v2"

	"github.com/getarcaneapp/arcane/backend/v2/internal/database"
	"github.com/getarcaneapp/arcane/backend/v2/internal/docker"
	"github.com/getarcaneapp/arcane/backend/v2/internal/kv"
)

type Dependencies struct {
	DB                   *database.DB
	Docker               *docker.DockerClientService
	KV                   *kv.KVService
	SyncRemoteRegistries func(context.Context) error
}

type Module struct {
	service *ContainerRegistryService
	handler *ContainerRegistryHandler
}

func New(deps Dependencies) *Module {
	service := NewContainerRegistryService(deps.DB, func(ctx context.Context) (RegistryDaemonClient, error) {
		return deps.Docker.GetClient(ctx)
	}, deps.KV)
	return &Module{service: service, handler: NewHandler(service, deps.SyncRemoteRegistries)}
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
