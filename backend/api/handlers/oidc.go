package handlers

import (
	"context"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/danielgtaylor/huma/v2"
	humamw "github.com/getarcaneapp/arcane/backend/v2/api/middleware"
	"github.com/getarcaneapp/arcane/backend/v2/internal/common"
	"github.com/getarcaneapp/arcane/backend/v2/internal/config"
	"github.com/getarcaneapp/arcane/backend/v2/internal/models"
	"github.com/getarcaneapp/arcane/backend/v2/internal/services"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/utils/cookie"
	httputils "github.com/getarcaneapp/arcane/backend/v2/pkg/utils/httpx"
	"github.com/getarcaneapp/arcane/types/v2/auth"
	roletypes "github.com/getarcaneapp/arcane/types/v2/role"
)

// oidcHandler handles OIDC authentication endpoints, plus OIDC group → role
// mapping management (since mappings only make sense in the OIDC context).
type oidcHandler struct {
	authService *services.AuthService
	oidcService *services.OidcService
	roleService *services.RoleService
	userService *services.UserService
	config      *config.Config
}

// ============================================================================
// Input/Output Types
// ============================================================================

type oidcHeaders struct {
	Origin          string `header:"Origin"`
	XForwardedHost  string `header:"X-Forwarded-Host"`
	XForwardedProto string `header:"X-Forwarded-Proto"`
	Host            string `header:"Host"`
	UserAgent       string `header:"User-Agent"`
}

type getOidcStatusInput struct{}

type getOidcStatusOutput struct {
	Body auth.OidcStatusInfo
}

type getOidcAuthUrlInput struct {
	oidcHeaders

	Body auth.OidcAuthUrlRequest
}

type getOidcAuthUrlOutput struct {
	SetCookie string `header:"Set-Cookie" doc:"OIDC state cookie"`
	Body      auth.OidcAuthUrlResponse
}

type handleOidcCallbackInput struct {
	oidcHeaders

	OidcStateCookie string `cookie:"oidc_state" doc:"OIDC state cookie from auth URL request"`
	Body            auth.OidcCallbackRequest
}

type handleOidcCallbackOutput struct {
	SetCookie []string `header:"Set-Cookie" doc:"Session and clear state cookies"`
	Body      auth.OidcCallbackResponse
}

type getOidcConfigInput struct {
	oidcHeaders
}

type getOidcConfigOutput struct {
	Body auth.OidcConfigResponse
}

type initiateDeviceAuthInput struct{}

type initiateDeviceAuthOutput struct {
	Body auth.OidcDeviceAuthResponse
}

type exchangeDeviceTokenInput struct {
	UserAgent string `header:"User-Agent"`
	Body      auth.OidcDeviceTokenRequest
}

type exchangeDeviceTokenOutput struct {
	SetCookie []string `header:"Set-Cookie" doc:"Session token cookie"`
	Body      auth.OidcDeviceTokenResponse
}

// --- OIDC role mapping I/O ---

type listOidcRoleMappingsInput struct{}

type listOidcRoleMappingsOutput struct {
	Body struct {
		Success bool                        `json:"success"`
		Data    []roletypes.OidcRoleMapping `json:"data"`
	}
}

type createOidcRoleMappingInput struct {
	Body roletypes.CreateOidcRoleMapping
}

type createOidcRoleMappingOutput struct {
	Body struct {
		Success bool                      `json:"success"`
		Data    roletypes.OidcRoleMapping `json:"data"`
	}
}

type updateOidcRoleMappingInput struct {
	ID   string `path:"id" doc:"Mapping ID"`
	Body roletypes.UpdateOidcRoleMapping
}

type updateOidcRoleMappingOutput struct {
	Body struct {
		Success bool                      `json:"success"`
		Data    roletypes.OidcRoleMapping `json:"data"`
	}
}

type deleteOidcRoleMappingInput struct {
	ID string `path:"id" doc:"Mapping ID"`
}

type deleteOidcRoleMappingOutput struct {
	Body struct {
		Success bool   `json:"success"`
		Message string `json:"message"`
	}
}

// ============================================================================
// Registration
// ============================================================================

// RegisterOidc registers all OIDC authentication endpoints (plus the OIDC
// group → role mapping CRUD) using Huma.
func RegisterOidc(api huma.API, authService *services.AuthService, oidcService *services.OidcService, roleService *services.RoleService, userService *services.UserService, cfg *config.Config) {
	h := &oidcHandler{authService: authService, oidcService: oidcService, roleService: roleService, userService: userService, config: cfg}

	huma.Register(api, huma.Operation{
		OperationID: "get-oidc-status",
		Method:      http.MethodGet,
		Path:        "/oidc/status",
		Summary:     "Get OIDC status",
		Description: "Get the current OIDC configuration status",
		Tags:        []string{"OIDC"},
		Security:    []map[string][]string{},
	}, h.getOidcStatusInternal)

	huma.Register(api, huma.Operation{
		OperationID: "get-oidc-config",
		Method:      http.MethodGet,
		Path:        "/oidc/config",
		Summary:     "Get OIDC config",
		Description: "Get the OIDC client configuration",
		Tags:        []string{"OIDC"},
		Security:    []map[string][]string{},
	}, h.getOidcConfigInternal)

	huma.Register(api, huma.Operation{
		OperationID: "get-oidc-auth-url",
		Method:      http.MethodPost,
		Path:        "/oidc/url",
		Summary:     "Get OIDC auth URL",
		Description: "Generate an OIDC authorization URL for login",
		Tags:        []string{"OIDC"},
		Security:    []map[string][]string{},
	}, h.getOidcAuthUrlInternal)

	huma.Register(api, huma.Operation{
		OperationID: "handle-oidc-callback",
		Method:      http.MethodPost,
		Path:        "/oidc/callback",
		Summary:     "Handle OIDC callback",
		Description: "Process the OIDC callback and complete authentication",
		Tags:        []string{"OIDC"},
		Security:    []map[string][]string{},
	}, h.handleOidcCallbackInternal)

	huma.Register(api, huma.Operation{
		OperationID: "initiate-oidc-device-auth",
		Method:      http.MethodPost,
		Path:        "/oidc/device/code",
		Summary:     "Initiate OIDC device authorization",
		Description: "Start the device authorization flow for CLI authentication",
		Tags:        []string{"OIDC"},
		Security:    []map[string][]string{},
	}, h.initiateDeviceAuthInternal)

	huma.Register(api, huma.Operation{
		OperationID: "exchange-oidc-device-token",
		Method:      http.MethodPost,
		Path:        "/oidc/device/token",
		Summary:     "Exchange device code for tokens",
		Description: "Exchange a device code for authentication tokens",
		Tags:        []string{"OIDC"},
		Security:    []map[string][]string{},
	}, h.exchangeDeviceTokenInternal)

	// --- OIDC role mapping endpoints ---

	huma.Register(api, huma.Operation{
		OperationID: "list-oidc-role-mappings",
		Method:      http.MethodGet,
		Path:        "/oidc/role-mappings",
		Summary:     "List OIDC group → role mappings",
		Description: "Returns every mapping. On each OIDC login the user's group claim is matched against ClaimValue and matching rows become source='oidc' role assignments.",
		Tags:        []string{"OIDC"},
		Security:    []map[string][]string{{"BearerAuth": {}}, {"ApiKeyAuth": {}}},
		Middlewares: humamw.RequireGlobalAdmin(api),
	}, h.listOidcRoleMappingsInternal)

	huma.Register(api, huma.Operation{
		OperationID: "create-oidc-role-mapping",
		Method:      http.MethodPost,
		Path:        "/oidc/role-mappings",
		Summary:     "Create an OIDC role mapping",
		Tags:        []string{"OIDC"},
		Security:    []map[string][]string{{"BearerAuth": {}}, {"ApiKeyAuth": {}}},
		Middlewares: humamw.RequireGlobalAdmin(api),
	}, h.createOidcRoleMappingInternal)

	huma.Register(api, huma.Operation{
		OperationID: "update-oidc-role-mapping",
		Method:      http.MethodPut,
		Path:        "/oidc/role-mappings/{id}",
		Summary:     "Update an OIDC role mapping",
		Tags:        []string{"OIDC"},
		Security:    []map[string][]string{{"BearerAuth": {}}, {"ApiKeyAuth": {}}},
		Middlewares: humamw.RequireGlobalAdmin(api),
	}, h.updateOidcRoleMappingInternal)

	huma.Register(api, huma.Operation{
		OperationID: "delete-oidc-role-mapping",
		Method:      http.MethodDelete,
		Path:        "/oidc/role-mappings/{id}",
		Summary:     "Delete an OIDC role mapping",
		Tags:        []string{"OIDC"},
		Security:    []map[string][]string{{"BearerAuth": {}}, {"ApiKeyAuth": {}}},
		Middlewares: humamw.RequireGlobalAdmin(api),
	}, h.deleteOidcRoleMappingInternal)
}

// ============================================================================
// Handler Methods
// ============================================================================

// GetOidcStatus returns the OIDC configuration status.
func (h *oidcHandler) getOidcStatusInternal(ctx context.Context, _ *getOidcStatusInput) (*getOidcStatusOutput, error) {
	if h.authService == nil {
		return nil, huma.Error500InternalServerError("service not available")
	}

	status, err := h.authService.GetOidcConfigurationStatus(ctx)
	if err != nil {
		return nil, huma.Error500InternalServerError((&common.OidcStatusError{Err: err}).Error())
	}

	return &getOidcStatusOutput{
		Body: *status,
	}, nil
}

// GetOidcConfig returns the OIDC client configuration.
func (h *oidcHandler) getOidcConfigInternal(ctx context.Context, input *getOidcConfigInput) (*getOidcConfigOutput, error) {
	if h.authService == nil || h.oidcService == nil {
		return nil, huma.Error500InternalServerError("service not available")
	}

	oidcConfig, err := h.authService.GetOidcConfig(ctx)
	if err != nil {
		return nil, huma.Error500InternalServerError((&common.OidcConfigError{}).Error())
	}

	appUrl := ""
	if h.config != nil {
		appUrl = h.config.AppUrl
	}
	origin := httputils.GetClientBaseURL(input.Origin, input.XForwardedHost, input.XForwardedProto, input.Host, appUrl)

	return &getOidcConfigOutput{
		Body: auth.OidcConfigResponse{
			ClientID:                    oidcConfig.ClientID,
			RedirectUri:                 h.oidcService.GetOidcRedirectURL(origin),
			IssuerUrl:                   oidcConfig.IssuerURL,
			AuthorizationEndpoint:       oidcConfig.AuthorizationEndpoint,
			TokenEndpoint:               oidcConfig.TokenEndpoint,
			UserinfoEndpoint:            oidcConfig.UserinfoEndpoint,
			DeviceAuthorizationEndpoint: oidcConfig.DeviceAuthorizationEndpoint,
			Scopes:                      oidcConfig.Scopes,
		},
	}, nil
}

// GetOidcAuthUrl generates an OIDC authorization URL and sets the state cookie.
func (h *oidcHandler) getOidcAuthUrlInternal(ctx context.Context, input *getOidcAuthUrlInput) (*getOidcAuthUrlOutput, error) {
	if h.authService == nil || h.oidcService == nil {
		return nil, huma.Error500InternalServerError("service not available")
	}

	enabled, err := h.authService.IsOidcEnabled(ctx)
	if err != nil {
		return nil, huma.Error500InternalServerError((&common.OidcStatusCheckError{}).Error())
	}
	if !enabled {
		return nil, huma.Error400BadRequest((&common.OidcDisabledError{}).Error())
	}

	appUrl := ""
	if h.config != nil {
		appUrl = h.config.AppUrl
	}
	origin := httputils.GetClientBaseURL(input.Origin, input.XForwardedHost, input.XForwardedProto, input.Host, appUrl)

	mobileRedirectURI := input.Body.MobileRedirectUri
	if mobileRedirectURI != "" {
		if err := h.oidcService.ValidateMobileRedirectURI(ctx, mobileRedirectURI); err != nil {
			slog.WarnContext(ctx, "OIDC auth URL: rejected mobile redirect URI", "uri", mobileRedirectURI, "error", err)
			return nil, huma.Error400BadRequest(err.Error())
		}
	}

	authUrl, stateCookieValue, err := h.oidcService.GenerateAuthURL(ctx, input.Body.RedirectUri, origin, mobileRedirectURI)
	if err != nil {
		return nil, huma.Error500InternalServerError((&common.OidcAuthUrlGenerationError{Err: err}).Error())
	}

	// Build state cookie (600 seconds = 10 minutes)
	stateCookie := cookie.BuildOidcStateCookieString(stateCookieValue, 600, false)

	return &getOidcAuthUrlOutput{
		SetCookie: stateCookie,
		Body: auth.OidcAuthUrlResponse{
			AuthUrl: authUrl,
		},
	}, nil
}

// HandleOidcCallback processes the OIDC callback and completes authentication.
func (h *oidcHandler) handleOidcCallbackInternal(ctx context.Context, input *handleOidcCallbackInput) (*handleOidcCallbackOutput, error) {
	if h.authService == nil || h.oidcService == nil {
		return nil, huma.Error500InternalServerError("service not available")
	}

	// Validate state cookie
	if input.OidcStateCookie == "" {
		return nil, huma.Error400BadRequest((&common.OidcStateCookieError{}).Error())
	}

	appUrl := ""
	if h.config != nil {
		appUrl = h.config.AppUrl
	}
	origin := httputils.GetClientBaseURL(input.Origin, input.XForwardedHost, input.XForwardedProto, input.Host, appUrl)

	mobileRedirectURI := input.Body.MobileRedirectUri
	if mobileRedirectURI != "" {
		if err := h.oidcService.ValidateMobileRedirectURI(ctx, mobileRedirectURI); err != nil {
			slog.WarnContext(ctx, "OIDC callback: rejected mobile redirect URI", "uri", mobileRedirectURI, "error", err)
			return nil, huma.Error400BadRequest(err.Error())
		}
	}

	// Process OIDC callback
	userInfo, tokenResp, err := h.oidcService.HandleCallback(ctx, input.Body.Code, input.Body.State, input.OidcStateCookie, origin, mobileRedirectURI)
	if err != nil {
		slog.WarnContext(ctx, "OIDC callback failed", "error", err, "origin", origin, "state_present", input.Body.State != "", "code_present", input.Body.Code != "")
		return nil, huma.Error400BadRequest((&common.OidcCallbackError{Err: err}).Error())
	}

	// Complete login
	userModel, tokenPair, err := h.authService.OidcLogin(ctx, *userInfo, tokenResp, sessionMetaFromContextInternal(ctx, input.UserAgent))
	if err != nil {
		return nil, huma.Error500InternalServerError((&common.AuthFailedError{Err: err}).Error())
	}

	// Build cookies: clear the state cookie always; only set the session
	// token cookie for browser flows (mobile clients use Bearer tokens from
	// the JSON body and never consume the cookie).
	clearStateCookie := cookie.BuildClearOidcStateCookieString(false)
	setCookies := []string{clearStateCookie}
	if mobileRedirectURI == "" {
		maxAge := max(int(time.Until(tokenPair.ExpiresAt).Seconds()), 0)
		maxAge += 60 // Add 60 seconds buffer for clock skew
		setCookies = append(setCookies, cookie.BuildTokenCookieStringFor(maxAge, tokenPair.AccessToken, cookie.SecureCookieFromContext(ctx)))
	}

	userDto, err := h.userService.ToUserResponseDto(ctx, *userModel)
	if err != nil {
		return nil, huma.Error500InternalServerError((&common.UserMappingError{Err: err}).Error())
	}

	return &handleOidcCallbackOutput{
		SetCookie: setCookies,
		Body: auth.OidcCallbackResponse{
			Success:      true,
			Token:        tokenPair.AccessToken,
			RefreshToken: tokenPair.RefreshToken,
			ExpiresAt:    tokenPair.ExpiresAt,
			User:         userDto,
		},
	}, nil
}

// InitiateDeviceAuth initiates the OIDC device authorization flow.
func (h *oidcHandler) initiateDeviceAuthInternal(ctx context.Context, _ *initiateDeviceAuthInput) (*initiateDeviceAuthOutput, error) {
	if h.authService == nil || h.oidcService == nil {
		return nil, huma.Error500InternalServerError("service not available")
	}

	enabled, err := h.authService.IsOidcEnabled(ctx)
	if err != nil {
		return nil, huma.Error500InternalServerError((&common.OidcStatusCheckError{}).Error())
	}
	if !enabled {
		return nil, huma.Error400BadRequest((&common.OidcDisabledError{}).Error())
	}

	response, err := h.oidcService.InitiateDeviceAuth(ctx)
	if err != nil {
		slog.WarnContext(ctx, "Device authorization initiation failed", "error", err)
		return nil, huma.Error500InternalServerError((&common.OidcAuthUrlGenerationError{Err: err}).Error())
	}

	return &initiateDeviceAuthOutput{
		Body: *response,
	}, nil
}

// ExchangeDeviceToken exchanges a device code for authentication tokens.
func (h *oidcHandler) exchangeDeviceTokenInternal(ctx context.Context, input *exchangeDeviceTokenInput) (*exchangeDeviceTokenOutput, error) {
	if h.authService == nil || h.oidcService == nil {
		return nil, huma.Error500InternalServerError("service not available")
	}

	if input.Body.DeviceCode == "" {
		return nil, huma.Error400BadRequest("device code is required")
	}

	userInfo, tokenResp, err := h.oidcService.ExchangeDeviceToken(ctx, input.Body.DeviceCode)
	if err != nil {
		errMsg := err.Error()
		switch errMsg {
		case "authorization_pending":
			return nil, huma.Error400BadRequest("authorization_pending")
		case "slow_down":
			return nil, huma.Error400BadRequest("slow_down")
		case "expired_token":
			return nil, huma.Error400BadRequest("expired_token")
		case "access_denied":
			return nil, huma.Error403Forbidden("access_denied")
		default:
			slog.WarnContext(ctx, "Device token exchange failed", "error", err)
			return nil, huma.Error400BadRequest((&common.OidcCallbackError{Err: err}).Error())
		}
	}

	userModel, tokenPair, err := h.authService.OidcLogin(ctx, *userInfo, tokenResp, sessionMetaFromContextInternal(ctx, input.UserAgent))
	if err != nil {
		return nil, huma.Error500InternalServerError((&common.AuthFailedError{Err: err}).Error())
	}

	maxAge := max(int(time.Until(tokenPair.ExpiresAt).Seconds()), 0)
	maxAge += 60

	tokenCookie := cookie.BuildTokenCookieStringFor(maxAge, tokenPair.AccessToken, cookie.SecureCookieFromContext(ctx))

	userDto, err := h.userService.ToUserResponseDto(ctx, *userModel)
	if err != nil {
		return nil, huma.Error500InternalServerError((&common.UserMappingError{Err: err}).Error())
	}

	return &exchangeDeviceTokenOutput{
		SetCookie: []string{tokenCookie},
		Body: auth.OidcDeviceTokenResponse{
			Success:      true,
			Token:        tokenPair.AccessToken,
			RefreshToken: tokenPair.RefreshToken,
			ExpiresAt:    tokenPair.ExpiresAt,
			User:         userDto,
		},
	}, nil
}

// ============================================================================
// OIDC Role Mapping Handlers
// ============================================================================

func (h *oidcHandler) listOidcRoleMappingsInternal(ctx context.Context, _ *listOidcRoleMappingsInput) (*listOidcRoleMappingsOutput, error) {
	if h.roleService == nil {
		return nil, huma.Error500InternalServerError("service not available")
	}
	rows, err := h.roleService.ListOidcMappings(ctx)
	if err != nil {
		return nil, huma.Error500InternalServerError("failed to list mappings: " + err.Error())
	}
	out := &listOidcRoleMappingsOutput{}
	out.Body.Success = true
	out.Body.Data = make([]roletypes.OidcRoleMapping, len(rows))
	for i := range rows {
		out.Body.Data[i] = toOidcMappingDTO(&rows[i])
	}
	return out, nil
}

func (h *oidcHandler) createOidcRoleMappingInternal(ctx context.Context, input *createOidcRoleMappingInput) (*createOidcRoleMappingOutput, error) {
	if h.roleService == nil {
		return nil, huma.Error500InternalServerError("service not available")
	}
	claimValue := strings.TrimSpace(input.Body.ClaimValue)
	roleID := strings.TrimSpace(input.Body.RoleID)
	if claimValue == "" {
		return nil, huma.Error400BadRequest("claim value is required")
	}
	if roleID == "" {
		return nil, huma.Error400BadRequest("role id is required")
	}
	mapping, err := h.roleService.CreateOidcMapping(ctx, claimValue, roleID, input.Body.EnvironmentID)
	if err != nil {
		if common.IsInvalidRoleAssignmentError(err) {
			return nil, huma.Error400BadRequest(err.Error())
		}
		return nil, huma.Error500InternalServerError("failed to create mapping: " + err.Error())
	}
	out := &createOidcRoleMappingOutput{}
	out.Body.Success = true
	out.Body.Data = toOidcMappingDTO(mapping)
	return out, nil
}

func (h *oidcHandler) updateOidcRoleMappingInternal(ctx context.Context, input *updateOidcRoleMappingInput) (*updateOidcRoleMappingOutput, error) {
	if h.roleService == nil {
		return nil, huma.Error500InternalServerError("service not available")
	}
	claimValue := strings.TrimSpace(input.Body.ClaimValue)
	roleID := strings.TrimSpace(input.Body.RoleID)
	if claimValue == "" {
		return nil, huma.Error400BadRequest("claim value is required")
	}
	if roleID == "" {
		return nil, huma.Error400BadRequest("role id is required")
	}
	mapping, err := h.roleService.UpdateOidcMapping(ctx, input.ID, claimValue, roleID, input.Body.EnvironmentID)
	if err != nil {
		if common.IsOidcMappingNotFoundError(err) {
			return nil, huma.Error404NotFound("mapping not found")
		}
		if common.IsOidcMappingEnvManagedError(err) {
			return nil, huma.Error409Conflict(err.Error())
		}
		if common.IsInvalidRoleAssignmentError(err) {
			return nil, huma.Error400BadRequest(err.Error())
		}
		return nil, huma.Error500InternalServerError("failed to update mapping: " + err.Error())
	}
	out := &updateOidcRoleMappingOutput{}
	out.Body.Success = true
	out.Body.Data = toOidcMappingDTO(mapping)
	return out, nil
}

func (h *oidcHandler) deleteOidcRoleMappingInternal(ctx context.Context, input *deleteOidcRoleMappingInput) (*deleteOidcRoleMappingOutput, error) {
	if h.roleService == nil {
		return nil, huma.Error500InternalServerError("service not available")
	}
	if err := h.roleService.DeleteOidcMapping(ctx, input.ID); err != nil {
		if common.IsOidcMappingNotFoundError(err) {
			return nil, huma.Error404NotFound("mapping not found")
		}
		if common.IsOidcMappingEnvManagedError(err) {
			return nil, huma.Error409Conflict(err.Error())
		}
		return nil, huma.Error500InternalServerError("failed to delete mapping: " + err.Error())
	}
	out := &deleteOidcRoleMappingOutput{}
	out.Body.Success = true
	out.Body.Message = "mapping deleted"
	return out, nil
}

func toOidcMappingDTO(m *models.OidcRoleMapping) roletypes.OidcRoleMapping {
	return roletypes.OidcRoleMapping{
		ID:            m.ID,
		ClaimValue:    m.ClaimValue,
		RoleID:        m.RoleID,
		EnvironmentID: m.EnvironmentID,
		Source:        m.Source,
		CreatedAt:     m.CreatedAt,
		UpdatedAt:     m.UpdatedAt,
	}
}
