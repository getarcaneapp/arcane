package oidc

import (
	"net/http"

	"github.com/danielgtaylor/huma/v2"

	"github.com/getarcaneapp/arcane/backend/v3/internal/auth"
	"github.com/getarcaneapp/arcane/backend/v3/internal/config"
	"github.com/getarcaneapp/arcane/backend/v3/internal/passkey"
	"github.com/getarcaneapp/arcane/backend/v3/internal/user"
)

// RegisterOidc registers all OIDC authentication endpoints using Huma.
func RegisterOidc(
	api huma.API,
	authService *auth.AuthService,
	passkeyService *passkey.PasskeyService,
	oidcService *OidcService,
	userService *user.UserService,
	cfg *config.Config,
) {
	h := &OidcHandler{authService: authService, passkeyService: passkeyService, oidcService: oidcService, userService: userService, config: cfg}

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
}
