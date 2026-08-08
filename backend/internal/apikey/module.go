// Package apikey owns API-key persistence, permission grants, validation, and
// the API-key HTTP surface.
package apikey

import (
	"github.com/danielgtaylor/huma/v2"

	"github.com/getarcaneapp/arcane/backend/v2/internal/database"
	"github.com/getarcaneapp/arcane/backend/v2/internal/role"
	"github.com/getarcaneapp/arcane/backend/v2/internal/user"
)

type Dependencies struct {
	DB   *database.DB
	User *user.UserService
	Role *role.RoleService
}

type Module struct {
	service *ApiKeyService
}

func New(deps Dependencies) *Module {
	return &Module{service: NewApiKeyService(deps.DB, deps.User).WithRoleService(deps.Role)}
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
