package oidc

import (
	"net/http"

	"github.com/danielgtaylor/huma/v2"

	"github.com/getarcaneapp/arcane/backend/v2/internal/auth"
	"github.com/getarcaneapp/arcane/backend/v2/internal/config"
	"github.com/getarcaneapp/arcane/backend/v2/internal/middleware"
	"github.com/getarcaneapp/arcane/backend/v2/internal/passkey"
	"github.com/getarcaneapp/arcane/backend/v2/internal/role"
	"github.com/getarcaneapp/arcane/backend/v2/internal/user"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/utils/handlerutil"
)

// RegisterOidc registers all OIDC authentication endpoints (plus the OIDC
// group → role mapping CRUD) using Huma.
func RegisterOidc(
	api huma.API,
	authService *auth.AuthService,
	passkeyService *passkey.PasskeyService,
	oidcService *OidcService,
	roleService *role.RoleService,
	userService *user.UserService,
	cfg *config.Config,
) {
	h := &OidcHandler{authService: authService, passkeyService: passkeyService, oidcService: oidcService, roleService: roleService, userService: userService, config: cfg}

	huma.Register(api, huma.Operation{
		OperationID: "get-oidc-status",
		Method:      http.MethodGet,
		Path:        "/oidc/status",
		Summary:     "Get OIDC status",
		Description: "Get the current OIDC configuration status",
		Tags:        []string{"OIDC"},
		Security:    []map[string][]string{},
	}, h.GetOidcStatus)

	huma.Register(api, huma.Operation{
		OperationID: "get-oidc-config",
		Method:      http.MethodGet,
		Path:        "/oidc/config",
		Summary:     "Get OIDC config",
		Description: "Get the OIDC client configuration",
		Tags:        []string{"OIDC"},
		Security:    []map[string][]string{},
	}, h.GetOidcConfig)

	huma.Register(api, huma.Operation{
		OperationID: "get-oidc-auth-url",
		Method:      http.MethodPost,
		Path:        "/oidc/url",
		Summary:     "Get OIDC auth URL",
		Description: "Generate an OIDC authorization URL for login",
		Tags:        []string{"OIDC"},
		Security:    []map[string][]string{},
	}, h.GetOidcAuthUrl)

	huma.Register(api, huma.Operation{
		OperationID: "handle-oidc-callback",
		Method:      http.MethodPost,
		Path:        "/oidc/callback",
		Summary:     "Handle OIDC callback",
		Description: "Process the OIDC callback and complete authentication",
		Tags:        []string{"OIDC"},
		Security:    []map[string][]string{},
	}, h.HandleOidcCallback)

	huma.Register(api, huma.Operation{
		OperationID: "initiate-oidc-device-auth",
		Method:      http.MethodPost,
		Path:        "/oidc/device/code",
		Summary:     "Initiate OIDC device authorization",
		Description: "Start the device authorization flow for CLI authentication",
		Tags:        []string{"OIDC"},
		Security:    []map[string][]string{},
	}, h.InitiateDeviceAuth)

	huma.Register(api, huma.Operation{
		OperationID: "exchange-oidc-device-token",
		Method:      http.MethodPost,
		Path:        "/oidc/device/token",
		Summary:     "Exchange device code for tokens",
		Description: "Exchange a device code for authentication tokens",
		Tags:        []string{"OIDC"},
		Security:    []map[string][]string{},
	}, h.ExchangeDeviceToken)

	// --- OIDC role mapping endpoints ---

	huma.Register(api, huma.Operation{
		OperationID: "list-oidc-role-mappings",
		Method:      http.MethodGet,
		Path:        "/oidc/mappings",
		Summary:     "List OIDC group → role mappings",
		Description: "Returns every mapping. On each OIDC login the user's group claim is matched against ClaimValue and matching rows become source='oidc' role assignments.",
		Tags:        []string{"OIDC"},
		Security:    handlerutil.DefaultOperationSecurity(),
		Middlewares: middleware.RequireGlobalAdmin(api),
	}, h.ListOidcRoleMappings)

	huma.Register(api, huma.Operation{
		OperationID: "create-oidc-role-mapping",
		Method:      http.MethodPost,
		Path:        "/oidc/mappings",
		Summary:     "Create an OIDC role mapping",
		Tags:        []string{"OIDC"},
		Security:    handlerutil.DefaultOperationSecurity(),
		Middlewares: middleware.RequireGlobalAdmin(api),
	}, h.CreateOidcRoleMapping)

	huma.Register(api, huma.Operation{
		OperationID: "update-oidc-role-mapping",
		Method:      http.MethodPatch,
		Path:        "/oidc/mappings/{id}",
		Summary:     "Update an OIDC role mapping",
		Tags:        []string{"OIDC"},
		Security:    handlerutil.DefaultOperationSecurity(),
		Middlewares: middleware.RequireGlobalAdmin(api),
	}, h.UpdateOidcRoleMapping)

	huma.Register(api, huma.Operation{
		OperationID: "delete-oidc-role-mapping",
		Method:      http.MethodDelete,
		Path:        "/oidc/mappings/{id}",
		Summary:     "Delete an OIDC role mapping",
		Tags:        []string{"OIDC"},
		Security:    handlerutil.DefaultOperationSecurity(),
		Middlewares: middleware.RequireGlobalAdmin(api),
	}, h.DeleteOidcRoleMapping)
}
