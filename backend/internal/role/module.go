// Package role owns RBAC roles, assignments, permission resolution, and role HTTP routes.
package role

import (
	"github.com/danielgtaylor/huma/v2"
)

// Module owns role persistence and its HTTP surface.
type Module struct {
	service *RoleService
}

// New assembles the role routes around its service.
func New(service *RoleService) *Module {
	return &Module{service: service}
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
