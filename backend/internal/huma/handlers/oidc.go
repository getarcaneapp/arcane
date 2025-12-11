package handlers

import (
	"context"
	"net/http"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/getarcaneapp/arcane/backend/internal/common"
	"github.com/getarcaneapp/arcane/backend/internal/config"
	"github.com/getarcaneapp/arcane/backend/internal/services"
	"github.com/getarcaneapp/arcane/backend/internal/utils/cookie"
	httputils "github.com/getarcaneapp/arcane/backend/internal/utils/http"
	"go.getarcane.app/types/auth"
	"go.getarcane.app/types/user"
)

// OidcHandler handles OIDC authentication endpoints.
type OidcHandler struct {
	authService *services.AuthService
	oidcService *services.OidcService
	config      *config.Config
}

// ============================================================================
// Input/Output Types
// ============================================================================

type OidcHeaders struct {
	Origin          string `header:"Origin"`
	XForwardedHost  string `header:"X-Forwarded-Host"`
	XForwardedProto string `header:"X-Forwarded-Proto"`
	Host            string `header:"Host"`
}

type GetOidcStatusInput struct{}

type GetOidcStatusOutput struct {
	Body auth.OidcStatusInfo
}

type GetOidcAuthUrlInput struct {
	OidcHeaders
	Body auth.OidcAuthUrlRequest
}

type GetOidcAuthUrlOutput struct {
	SetCookie string `header:"Set-Cookie" doc:"OIDC state cookie"`
	Body      auth.OidcAuthUrlResponse
}

type HandleOidcCallbackInput struct {
	OidcHeaders
	OidcStateCookie string `cookie:"oidc_state" doc:"OIDC state cookie from auth URL request"`
	Body            auth.OidcCallbackRequest
}

type HandleOidcCallbackOutput struct {
	SetCookie []string `header:"Set-Cookie" doc:"Session and clear state cookies"`
	Body      auth.OidcCallbackResponse
}

type GetOidcConfigInput struct {
	OidcHeaders
}

type GetOidcConfigOutput struct {
	Body auth.OidcConfigResponse
}

// ============================================================================
// Registration
// ============================================================================

// RegisterOidc registers all OIDC authentication endpoints using Huma.
func RegisterOidc(api huma.API, authService *services.AuthService, oidcService *services.OidcService, cfg *config.Config) {
	h := &OidcHandler{authService: authService, oidcService: oidcService, config: cfg}

	huma.Register(api, huma.Operation{
		OperationID: "get-oidc-status",
		Method:      http.MethodGet,
		Path:        "/oidc/status",
		Summary:     "Get OIDC status",
		Description: "Get the current OIDC configuration status",
		Tags:        []string{"OIDC"},
	}, h.GetOidcStatus)

	huma.Register(api, huma.Operation{
		OperationID: "get-oidc-config",
		Method:      http.MethodGet,
		Path:        "/oidc/config",
		Summary:     "Get OIDC config",
		Description: "Get the OIDC client configuration",
		Tags:        []string{"OIDC"},
	}, h.GetOidcConfig)

	huma.Register(api, huma.Operation{
		OperationID: "get-oidc-auth-url",
		Method:      http.MethodPost,
		Path:        "/oidc/url",
		Summary:     "Get OIDC auth URL",
		Description: "Generate an OIDC authorization URL for login",
		Tags:        []string{"OIDC"},
	}, h.GetOidcAuthUrl)

	huma.Register(api, huma.Operation{
		OperationID: "handle-oidc-callback",
		Method:      http.MethodPost,
		Path:        "/oidc/callback",
		Summary:     "Handle OIDC callback",
		Description: "Process the OIDC callback and complete authentication",
		Tags:        []string{"OIDC"},
	}, h.HandleOidcCallback)
}

// ============================================================================
// Handler Methods
// ============================================================================

// GetOidcStatus returns the OIDC configuration status.
func (h *OidcHandler) GetOidcStatus(ctx context.Context, _ *GetOidcStatusInput) (*GetOidcStatusOutput, error) {
	if h.authService == nil {
		return nil, huma.Error500InternalServerError("service not available")
	}

	status, err := h.authService.GetOidcConfigurationStatus(ctx)
	if err != nil {
		return nil, huma.Error500InternalServerError((&common.OidcStatusError{Err: err}).Error())
	}

	return &GetOidcStatusOutput{
		Body: *status,
	}, nil
}

// GetOidcConfig returns the OIDC client configuration.
func (h *OidcHandler) GetOidcConfig(ctx context.Context, input *GetOidcConfigInput) (*GetOidcConfigOutput, error) {
	if h.authService == nil || h.oidcService == nil {
		return nil, huma.Error500InternalServerError("service not available")
	}

	config, err := h.authService.GetOidcConfig(ctx)
	if err != nil {
		return nil, huma.Error500InternalServerError((&common.OidcConfigError{}).Error())
	}

	appUrl := ""
	if h.config != nil {
		appUrl = h.config.AppUrl
	}
	origin := httputils.GetClientBaseURL(input.Origin, input.XForwardedHost, input.XForwardedProto, input.Host, appUrl)

	return &GetOidcConfigOutput{
		Body: auth.OidcConfigResponse{
			ClientID:              config.ClientID,
			RedirectUri:           h.oidcService.GetOidcRedirectURL(origin),
			IssuerUrl:             config.IssuerURL,
			AuthorizationEndpoint: config.AuthorizationEndpoint,
			TokenEndpoint:         config.TokenEndpoint,
			UserinfoEndpoint:      config.UserinfoEndpoint,
			Scopes:                config.Scopes,
		},
	}, nil
}

// GetOidcAuthUrl generates an OIDC authorization URL and sets the state cookie.
func (h *OidcHandler) GetOidcAuthUrl(ctx context.Context, input *GetOidcAuthUrlInput) (*GetOidcAuthUrlOutput, error) {
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
	authUrl, stateCookieValue, err := h.oidcService.GenerateAuthURL(ctx, input.Body.RedirectUri, origin)
	if err != nil {
		return nil, huma.Error500InternalServerError((&common.OidcAuthUrlGenerationError{Err: err}).Error())
	}

	// Build state cookie (600 seconds = 10 minutes)
	stateCookie := cookie.BuildOidcStateCookieString(stateCookieValue, 600, false)

	return &GetOidcAuthUrlOutput{
		SetCookie: stateCookie,
		Body: auth.OidcAuthUrlResponse{
			AuthUrl: authUrl,
		},
	}, nil
}

// HandleOidcCallback processes the OIDC callback and completes authentication.
func (h *OidcHandler) HandleOidcCallback(ctx context.Context, input *HandleOidcCallbackInput) (*HandleOidcCallbackOutput, error) {
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

	// Process OIDC callback
	userInfo, tokenResp, err := h.oidcService.HandleCallback(ctx, input.Body.Code, input.Body.State, input.OidcStateCookie, origin)
	if err != nil {
		return nil, huma.Error400BadRequest((&common.OidcCallbackError{Err: err}).Error())
	}

	// Complete login
	userModel, tokenPair, err := h.authService.OidcLogin(ctx, *userInfo, tokenResp)
	if err != nil {
		return nil, huma.Error500InternalServerError((&common.AuthFailedError{Err: err}).Error())
	}

	// Calculate cookie max age
	maxAge := int(time.Until(tokenPair.ExpiresAt).Seconds())
	if maxAge < 0 {
		maxAge = 0
	}
	maxAge += 60 // Add 60 seconds buffer for clock skew

	// Build cookies: session token + clear state cookie
	tokenCookie := cookie.BuildTokenCookieString(maxAge, tokenPair.AccessToken)
	clearStateCookie := cookie.BuildClearOidcStateCookieString(false)

	return &HandleOidcCallbackOutput{
		SetCookie: []string{tokenCookie, clearStateCookie},
		Body: auth.OidcCallbackResponse{
			Success:      true,
			Token:        tokenPair.AccessToken,
			RefreshToken: tokenPair.RefreshToken,
			ExpiresAt:    tokenPair.ExpiresAt,
			User: user.User{
				ID:            userModel.ID,
				Username:      userModel.Username,
				DisplayName:   userModel.DisplayName,
				Email:         userModel.Email,
				Roles:         userModel.Roles,
				OidcSubjectId: userModel.OidcSubjectId,
			},
		},
	}, nil
}
