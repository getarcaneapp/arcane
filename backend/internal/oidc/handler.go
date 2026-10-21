package oidc

import (
	"context"
	"errors"
	"log/slog"

	"github.com/danielgtaylor/huma/v2"
	"github.com/getarcaneapp/arcane/types/v3"
	"go.getarcane.app/kit/pkg"

	"github.com/getarcaneapp/arcane/backend/v2/internal/auth"
	"github.com/getarcaneapp/arcane/backend/v2/internal/common"
	"github.com/getarcaneapp/arcane/backend/v2/internal/config"
	"github.com/getarcaneapp/arcane/backend/v2/internal/database"
	"github.com/getarcaneapp/arcane/backend/v2/internal/passkey"
	"github.com/getarcaneapp/arcane/backend/v2/internal/role"
	"github.com/getarcaneapp/arcane/backend/v2/internal/session"
	"github.com/getarcaneapp/arcane/backend/v2/internal/user"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/utils/cookie"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/utils/handlerutil"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/utils/httpx"
)

// OidcHandler handles OIDC authentication endpoints, plus OIDC group → role
// mapping management (since mappings only make sense in the OIDC context).
type OidcHandler struct {
	authService    *auth.AuthService
	passkeyService *passkey.PasskeyService
	oidcService    *OidcService
	roleService    *role.RoleService
	userService    *user.UserService
	config         *config.Config
}

type OidcHeaders struct {
	Origin          string `header:"Origin"`
	XForwardedHost  string `header:"X-Forwarded-Host"`
	XForwardedProto string `header:"X-Forwarded-Proto"`
	Host            string `header:"Host"`
	UserAgent       string `header:"User-Agent"`
}

type GetOidcStatusInput struct{}

type GetOidcStatusOutput struct {
	Body types.OidcStatusInfo
}

type GetOidcAuthUrlInput struct {
	OidcHeaders

	Body types.OidcAuthUrlRequest
}

type GetOidcAuthUrlOutput struct {
	SetCookie string `header:"Set-Cookie" doc:"OIDC state cookie"`
	Body      types.OidcAuthUrlResponse
}

type HandleOidcCallbackInput struct {
	OidcHeaders

	OidcStateCookie string `cookie:"oidc_state" doc:"OIDC state cookie from auth URL request"`
	Body            types.OidcCallbackRequest
}

type HandleOidcCallbackOutput struct {
	SetCookie []string `header:"Set-Cookie" doc:"Session and clear state cookies"`
	Body      types.AuthenticationResponse
}

type GetOidcConfigInput struct {
	OidcHeaders
}

type GetOidcConfigOutput struct {
	Body types.OidcConfigResponse
}

type InitiateDeviceAuthInput struct{}

type InitiateDeviceAuthOutput struct {
	Body types.OidcDeviceAuthResponse
}

type ExchangeDeviceTokenInput struct {
	UserAgent string `header:"User-Agent"`
	Body      types.OidcDeviceTokenRequest
}

type ExchangeDeviceTokenOutput struct {
	Body types.AuthenticationResponse
}

type ListOidcRoleMappingsInput struct{}

type CreateOidcRoleMappingInput struct {
	types.Create[types.OidcRoleMapping]
}

type UpdateOidcRoleMappingInput struct {
	ID string `path:"id" doc:"Mapping ID"`
	types.Update[types.OidcRoleMapping]
}

type DeleteOidcRoleMappingInput struct {
	ID string `path:"id" doc:"Mapping ID"`
}

type DeleteOidcRoleMappingOutput struct {
	Body struct {
		Success bool   `json:"success"`
		Message string `json:"message"`
	}
}

// GetOidcStatus returns the OIDC configuration status.
func (h *OidcHandler) GetOidcStatus(ctx context.Context, _ *GetOidcStatusInput) (*GetOidcStatusOutput, error) {
	status, err := h.authService.GetOidcConfigurationStatus(ctx)
	if err != nil {
		return nil, huma.Error500InternalServerError("Failed to retrieve OIDC status: " + err.Error())
	}

	return &GetOidcStatusOutput{
		Body: *status,
	}, nil
}

// GetOidcConfig returns the OIDC client configuration.
func (h *OidcHandler) GetOidcConfig(ctx context.Context, input *GetOidcConfigInput) (*GetOidcConfigOutput, error) {
	oidcConfig, err := h.authService.GetOidcConfig(ctx)
	if errors.Is(err, auth.ErrOidcAuthDisabled) {
		return nil, huma.Error400BadRequest(auth.ErrOidcAuthDisabled.Error())
	}
	if err != nil {
		return nil, huma.Error500InternalServerError("Failed to get OIDC configuration")
	}

	appUrl := ""
	if h.config != nil {
		appUrl = h.config.AppUrl
	}
	origin := httpx.GetClientBaseURL(input.Origin, input.XForwardedHost, input.XForwardedProto, input.Host, appUrl)

	return &GetOidcConfigOutput{
		Body: types.OidcConfigResponse{
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
func (h *OidcHandler) GetOidcAuthUrl(ctx context.Context, input *GetOidcAuthUrlInput) (*GetOidcAuthUrlOutput, error) {
	enabled, err := h.authService.IsOidcEnabled(ctx)
	if err != nil {
		return nil, huma.Error500InternalServerError("Failed to check OIDC status")
	}
	if !enabled {
		return nil, huma.Error400BadRequest("OIDC authentication is disabled")
	}

	appUrl := ""
	if h.config != nil {
		appUrl = h.config.AppUrl
	}
	origin := httpx.GetClientBaseURL(input.Origin, input.XForwardedHost, input.XForwardedProto, input.Host, appUrl)

	mobileRedirectURI := input.Body.MobileRedirectUri
	if mobileRedirectURI != "" {
		if validateMobileRedirectURIErr := h.oidcService.ValidateMobileRedirectURI(ctx, mobileRedirectURI); validateMobileRedirectURIErr != nil {
			slog.WarnContext(ctx, "OIDC auth URL: rejected mobile redirect URI", "uri", mobileRedirectURI, "error", validateMobileRedirectURIErr)
			return nil, huma.Error400BadRequest(validateMobileRedirectURIErr.Error())
		}
	}

	authUrl, stateCookieValue, err := h.oidcService.GenerateAuthURL(ctx, input.Body.RedirectUri, origin, mobileRedirectURI)
	if err != nil {
		return nil, huma.Error500InternalServerError("Failed to generate OIDC auth URL: " + err.Error())
	}

	// Build state cookie (600 seconds = 10 minutes). Secure is resolved from the
	// request the same way the session token cookie resolves it, rather than
	// being hard-coded off.
	stateCookie := cookie.BuildOidcStateCookieString(stateCookieValue, 600, cookie.SecureCookieFromContext(ctx))

	return &GetOidcAuthUrlOutput{
		SetCookie: stateCookie,
		Body: types.OidcAuthUrlResponse{
			AuthUrl: authUrl,
		},
	}, nil
}

// HandleOidcCallback processes the OIDC callback and completes authentication.
func (h *OidcHandler) HandleOidcCallback(ctx context.Context, input *HandleOidcCallbackInput) (*HandleOidcCallbackOutput, error) {
	// Validate state cookie
	if input.OidcStateCookie == "" {
		return nil, huma.Error400BadRequest("Missing or invalid OIDC state cookie")
	}

	appUrl := ""
	if h.config != nil {
		appUrl = h.config.AppUrl
	}
	origin := httpx.GetClientBaseURL(input.Origin, input.XForwardedHost, input.XForwardedProto, input.Host, appUrl)

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
		slog.WarnContext(ctx, "OIDC callback failed", "error", err, "origin", origin, "statePresent", input.Body.State != "", "codePresent", input.Body.Code != "")
		return nil, huma.Error400BadRequest("OIDC callback failed: " + err.Error())
	}

	// Reconcile the provider identity first, then require passkey MFA before
	// creating a session for accounts that enabled it.
	meta := handlerutil.SessionMetaFromContext(ctx, input.UserAgent)
	userModel, isNewUser, err := h.authService.PrepareOidcLogin(ctx, *userInfo, tokenResp)
	if err != nil {
		slog.ErrorContext(ctx, "OIDC login preparation failed", "error", err, "subject", userInfo.Subject)
		switch {
		case errors.Is(err, common.ErrForbidden):
			return nil, huma.Error403Forbidden(err.Error())
		case errors.Is(err, common.ErrConflict):
			return nil, huma.Error409Conflict(err.Error())
		}
		return nil, huma.Error500InternalServerError("Authentication failed")
	}

	// Build cookies: clear the state cookie always; only set the session
	// token cookie for browser flows (mobile clients use Bearer tokens from
	// the JSON body and never consume the cookie).
	clearStateCookie := cookie.BuildClearOidcStateCookieString(cookie.SecureCookieFromContext(ctx))
	setCookies := []string{clearStateCookie}
	if userModel.PasskeyMFAEnabled {
		challenge, beginMFAAuthenticationErr := h.passkeyService.BeginMFAAuthentication(ctx, userModel.ID, meta, session.UserSessionSourceOidc)
		if beginMFAAuthenticationErr != nil {
			return nil, huma.Error500InternalServerError("Authentication failed")
		}
		return &HandleOidcCallbackOutput{
			SetCookie: setCookies,
			Body: types.AuthenticationResponse{
				Success: true,
				Status:  types.AuthenticationStatusMFARequired,
				MFA:     challenge,
			},
		}, nil
	}
	tokenPair, err := h.authService.CompleteLogin(ctx, userModel, meta, session.UserSessionSourceOidc, "", database.JSON{
		"newUser": isNewUser,
		"subject": userInfo.Subject,
	})
	if err != nil {
		return nil, huma.Error500InternalServerError("Authentication failed")
	}
	if mobileRedirectURI == "" {
		setCookies = append(setCookies, tokenPair.CookieHeaders(cookie.SecureCookieFromContext(ctx))...)
	}
	expiresAt := tokenPair.ExpiresAt

	userDto, err := h.userService.ToUserResponseDto(ctx, *userModel)
	if err != nil {
		return nil, huma.Error500InternalServerError("Failed to map user")
	}

	return &HandleOidcCallbackOutput{
		SetCookie: setCookies,
		Body: types.AuthenticationResponse{
			Success:      true,
			Status:       types.AuthenticationStatusAuthenticated,
			Token:        tokenPair.AccessToken,
			RefreshToken: tokenPair.RefreshToken,
			ExpiresAt:    &expiresAt,
			User:         &userDto,
		},
	}, nil
}

// InitiateDeviceAuth initiates the OIDC device authorization flow.
func (h *OidcHandler) InitiateDeviceAuth(ctx context.Context, _ *InitiateDeviceAuthInput) (*InitiateDeviceAuthOutput, error) {
	enabled, err := h.authService.IsOidcEnabled(ctx)
	if err != nil {
		return nil, huma.Error500InternalServerError("Failed to check OIDC status")
	}
	if !enabled {
		return nil, huma.Error400BadRequest("OIDC authentication is disabled")
	}

	response, err := h.oidcService.InitiateDeviceAuth(ctx)
	if err != nil {
		slog.WarnContext(ctx, "Device authorization initiation failed", "error", err)
		return nil, huma.Error500InternalServerError("Failed to generate OIDC auth URL: " + err.Error())
	}

	return &InitiateDeviceAuthOutput{
		Body: *response,
	}, nil
}

// ExchangeDeviceToken exchanges a device code for authentication tokens.
func (h *OidcHandler) ExchangeDeviceToken(ctx context.Context, input *ExchangeDeviceTokenInput) (*ExchangeDeviceTokenOutput, error) {
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
			return nil, huma.Error400BadRequest("OIDC callback failed: " + err.Error())
		}
	}

	meta := handlerutil.SessionMetaFromContext(ctx, input.UserAgent)
	userModel, isNewUser, err := h.authService.PrepareOidcLogin(ctx, *userInfo, tokenResp)
	if err != nil {
		slog.ErrorContext(ctx, "OIDC login preparation failed", "error", err, "subject", userInfo.Subject)
		switch {
		case errors.Is(err, common.ErrForbidden):
			return nil, huma.Error403Forbidden(err.Error())
		case errors.Is(err, common.ErrConflict):
			return nil, huma.Error409Conflict(err.Error())
		}
		return nil, huma.Error500InternalServerError("Authentication failed")
	}
	// The device flow has no way to satisfy a WebAuthn assertion: the CLI has no
	// authenticator, and completing MFA with a recovery code would burn a
	// single-use code on every login. MFA accounts authenticate the CLI with a
	// personal API key instead.
	if userModel.PasskeyMFAEnabled {
		return nil, huma.Error403Forbidden("mfa_required")
	}
	tokenPair, err := h.authService.CompleteLogin(ctx, userModel, meta, session.UserSessionSourceOidc, "", database.JSON{
		"newUser": isNewUser,
		"subject": userInfo.Subject,
	})
	if err != nil {
		return nil, huma.Error500InternalServerError("Authentication failed")
	}

	expiresAt := tokenPair.ExpiresAt

	userDto, err := h.userService.ToUserResponseDto(ctx, *userModel)
	if err != nil {
		return nil, huma.Error500InternalServerError("Failed to map user")
	}

	return &ExchangeDeviceTokenOutput{
		Body: types.AuthenticationResponse{
			Success:      true,
			Status:       types.AuthenticationStatusAuthenticated,
			Token:        tokenPair.AccessToken,
			RefreshToken: tokenPair.RefreshToken,
			ExpiresAt:    &expiresAt,
			User:         &userDto,
		},
	}, nil
}

func (h *OidcHandler) ListOidcRoleMappings(ctx context.Context, _ *ListOidcRoleMappingsInput) (*handlerutil.Out[[]types.OidcRoleMapping], error) {
	rows, err := h.roleService.ListOidcMappings(ctx)
	if err != nil {
		return nil, huma.Error500InternalServerError("failed to list mappings: " + err.Error())
	}
	out := &handlerutil.Out[[]types.OidcRoleMapping]{}
	out.Body.Success = true
	out.Body.Data = make([]types.OidcRoleMapping, len(rows))
	for i := range rows {
		out.Body.Data[i] = toOidcMappingDTO(&rows[i])
	}
	return out, nil
}

func (h *OidcHandler) CreateOidcRoleMapping(ctx context.Context, input *CreateOidcRoleMappingInput) (*handlerutil.Out[types.OidcRoleMapping], error) {
	mapping, err := h.roleService.CreateOidcMapping(ctx, input.Body.ClaimValue, input.Body.RoleID, input.Body.EnvironmentID)
	if err != nil {
		apiErr := common.ToAPIError(err)
		return nil, huma.NewError(apiErr.HTTPStatus(), apiErr.Message)
	}
	out := &handlerutil.Out[types.OidcRoleMapping]{}
	out.Body.Success = true
	out.Body.Data = toOidcMappingDTO(mapping)
	return out, nil
}

func (h *OidcHandler) UpdateOidcRoleMapping(ctx context.Context, input *UpdateOidcRoleMappingInput) (*handlerutil.Out[types.OidcRoleMapping], error) {
	submitted, err := input.SubmittedFields()
	if err != nil {
		return nil, huma.Error400BadRequest("invalid request body")
	}
	current, err := h.roleService.GetOidcMapping(ctx, input.ID)
	if err != nil {
		apiErr := common.ToAPIError(err)
		return nil, huma.NewError(apiErr.HTTPStatus(), apiErr.Message)
	}
	mapping, err := h.roleService.UpdateOidcMapping(
		ctx,
		input.ID,
		kit.Ternary(submitted["claimValue"], input.Body.ClaimValue, current.ClaimValue),
		kit.Ternary(submitted["roleId"], input.Body.RoleID, current.RoleID),
		kit.Ternary(submitted["environmentId"], input.Body.EnvironmentID, current.EnvironmentID),
	)
	if err != nil {
		apiErr := common.ToAPIError(err)
		return nil, huma.NewError(apiErr.HTTPStatus(), apiErr.Message)
	}
	out := &handlerutil.Out[types.OidcRoleMapping]{}
	out.Body.Success = true
	out.Body.Data = toOidcMappingDTO(mapping)
	return out, nil
}

func (h *OidcHandler) DeleteOidcRoleMapping(ctx context.Context, input *DeleteOidcRoleMappingInput) (*DeleteOidcRoleMappingOutput, error) {
	if err := h.roleService.DeleteOidcMapping(ctx, input.ID); err != nil {
		if errors.Is(err, common.ErrOidcMappingNotFound) {
			return nil, huma.Error404NotFound("mapping not found")
		}
		if errors.Is(err, common.ErrOidcMappingEnvManaged) {
			return nil, huma.Error409Conflict(err.Error())
		}
		return nil, huma.Error500InternalServerError("failed to delete mapping: " + err.Error())
	}
	out := &DeleteOidcRoleMappingOutput{}
	out.Body.Success = true
	out.Body.Message = "mapping deleted"
	return out, nil
}

func toOidcMappingDTO(m *role.OidcRoleMapping) types.OidcRoleMapping {
	return types.OidcRoleMapping{
		ID:            m.ID,
		ClaimValue:    m.ClaimValue,
		RoleID:        m.RoleID,
		EnvironmentID: m.EnvironmentID,
		Source:        m.Source,
		CreatedAt:     m.CreatedAt,
		UpdatedAt:     m.UpdatedAt,
	}
}
