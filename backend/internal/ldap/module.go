package ldap

import (
	"net/http"

	"github.com/danielgtaylor/huma/v2"

	kit "kit.getarcane.app"

	"getarcane.dev/arcane/v3/internal/middleware"
	"getarcane.dev/arcane/v3/pkg/authz"
	"getarcane.dev/arcane/v3/pkg/handlerutil"
)

type (
	Module struct {
		service *LdapService
	}
)

func New(service *LdapService) *Module {
	return &Module{service: service}
}

func (m *Module) RegisterRoutes(api huma.API) {
	h := &LdapHandler{service: kit.FromPtr(m).service}

	middleware.RegisterWithPermission(api, middleware.RequireGlobalAdmin(api, huma.Operation{
		OperationID: "sync-ldap",
		Method:      http.MethodPost,
		Path:        "/ldap/sync",
		Summary:     "Sync LDAP users",
		Description: "Synchronize users and role assignments from the configured LDAP directory. Requires a global admin",
		Tags:        []string{"Settings"},
		Security:    handlerutil.DefaultOperationSecurity(),
	}), authz.PermSettingsUpdate, h.Sync)
}
