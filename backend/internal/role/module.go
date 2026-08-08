// Package role owns RBAC roles, assignments, permission resolution, and role HTTP routes.
package role

import (
	"github.com/danielgtaylor/huma/v2"
	"github.com/getarcaneapp/arcane/backend/v2/internal/database"
)

// Dependencies are the collaborators the role domain needs.
type Dependencies struct {
	DB *database.DB
}

// Module owns role persistence and its HTTP surface.
type Module struct {
	service *RoleService
}

// New builds the role domain from its dependencies.
func New(deps Dependencies) *Module {
	return &Module{service: NewRoleService(deps.DB)}
}

// Service exposes role operations to authentication and authorization collaborators.
func (m *Module) Service() *RoleService {
	if m == nil {
		return nil
	}
	return m.service
}

// RegisterRoutes mounts role endpoints for runtime and schema discovery.
func (m *Module) RegisterRoutes(api huma.API) {
	if m == nil {
		RegisterRoles(api, nil)
		return
	}
	RegisterRoles(api, m.service)
}
