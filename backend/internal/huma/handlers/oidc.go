package handlers

import (
	"context"
	"net/http"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/getarcaneapp/arcane/backend/internal/common"
	humamw "github.com/getarcaneapp/arcane/backend/internal/huma/middleware"
	"github.com/getarcaneapp/arcane/backend/internal/services"
	"github.com/getarcaneapp/arcane/backend/internal/utils/cookie"
	"github.com/gin-gonic/gin"
	"go.getarcane.app/types/auth"
	"go.getarcane.app/types/user"
)

// OidcHandler handles OIDC authentication endpoints.
type OidcHandler struct {
	authService *services.AuthService
	oidcService *services.OidcService
}

// ============================================================================
// Input/Output Types
// ============================================================================

type GetOidcStatusInput struct{}

type GetOidcStatusOutput struct {
	Body auth.OidcStatusInfo
}

type GetOidcAuthUrlInput struct {
	Body auth.OidcAuthUrlRequest
}

type GetOidcAuthUrlOutput struct {
	SetCookie http.Cookie `header:"Set-Cookie"`
	Body      struct {
		AuthUrl string `json:"authUrl"`
	}
}

type HandleOidcCallbackInput struct {
	Body struct {
		Code  string `json:"code" required:"true"`
		State string `json:"state" required:"true"`
	}
}

type HandleOidcCallbackOutput struct {
	SetCookie http.Cookie `header:"Set-Cookie"`
	Body      struct {
		Success      bool      `json:"success"`
		Token        string    `json:"token"`
		RefreshToken string    `json:"refreshToken"`
		ExpiresAt    time.Time `json:"expiresAt"`
		User         user.User `json:"user"`
	}
}

type GetOidcConfigInput struct{}

type GetOidcConfigOutput struct {
	Body struct {
		ClientID              string `json:"clientId"`
		RedirectUri           string `json:"redirectUri"`
		IssuerUrl             string `json:"issuerUrl"`
		AuthorizationEndpoint string `json:"authorizationEndpoint"`
		TokenEndpoint         string `json:"tokenEndpoint"`
		UserinfoEndpoint      string `json:"userinfoEndpoint"`
		Scopes                string `json:"scopes"`
	}
}

// ============================================================================
// Registration
// ============================================================================

// RegisterOidc registers OIDC authentication endpoints.
func RegisterOidc(api huma.API, authService *services.AuthService, oidcService *services.OidcService) {
	h := &OidcHandler{authService: authService, oidcService: oidcService}

	huma.Register(api, huma.Operation{
		OperationID: "getOidcStatus",
		Method:      "GET",
		Path:        "/oidc/status",
		Summary:     "Get OIDC status",
		Description: "Get the current OIDC configuration status",
		Tags:        []string{"OIDC"},
	}, h.GetOidcStatus)

	huma.Register(api, huma.Operation{
		OperationID: "getOidcConfig",
		Method:      "GET",
		Path:        "/oidc/config",
		Summary:     "Get OIDC config",
		Description: "Get the OIDC client configuration",
		Tags:        []string{"OIDC"},
	}, h.GetOidcConfig)

	// Note: URL and callback endpoints need Gin context for cookie management
	// These are registered separately in the Gin router
}

// RegisterOidcGinRoutes registers OIDC routes that require Gin context for cookie handling.
func RegisterOidcGinRoutes(group *gin.RouterGroup, authService *services.AuthService, oidcService *services.OidcService) {
	h := &OidcHandler{authService: authService, oidcService: oidcService}

	oidcGroup := group.Group("/oidc")
	{
		oidcGroup.POST("/url", h.GetOidcAuthUrlGin)
		oidcGroup.POST("/callback", h.HandleOidcCallbackGin)
	}
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
func (h *OidcHandler) GetOidcConfig(ctx context.Context, _ *GetOidcConfigInput) (*GetOidcConfigOutput, error) {
	if h.authService == nil || h.oidcService == nil {
		return nil, huma.Error500InternalServerError("service not available")
	}

	config, err := h.authService.GetOidcConfig(ctx)
	if err != nil {
		return nil, huma.Error500InternalServerError((&common.OidcConfigError{}).Error())
	}

	return &GetOidcConfigOutput{
		Body: struct {
			ClientID              string `json:"clientId"`
			RedirectUri           string `json:"redirectUri"`
			IssuerUrl             string `json:"issuerUrl"`
			AuthorizationEndpoint string `json:"authorizationEndpoint"`
			TokenEndpoint         string `json:"tokenEndpoint"`
			UserinfoEndpoint      string `json:"userinfoEndpoint"`
			Scopes                string `json:"scopes"`
		}{
			ClientID:              config.ClientID,
			RedirectUri:           h.oidcService.GetOidcRedirectURL(),
			IssuerUrl:             config.IssuerURL,
			AuthorizationEndpoint: config.AuthorizationEndpoint,
			TokenEndpoint:         config.TokenEndpoint,
			UserinfoEndpoint:      config.UserinfoEndpoint,
			Scopes:                config.Scopes,
		},
	}, nil
}

// GetOidcAuthUrlGin handles OIDC auth URL generation (requires Gin for cookie).
func (h *OidcHandler) GetOidcAuthUrlGin(c *gin.Context) {
	var req auth.OidcAuthUrlRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"data":    gin.H{"error": (&common.InvalidRequestFormatError{Err: err}).Error()},
		})
		return
	}

	enabled, err := h.authService.IsOidcEnabled(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "error": (&common.OidcStatusCheckError{}).Error()})
		return
	}
	if !enabled {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": (&common.OidcDisabledError{}).Error()})
		return
	}

	authUrl, stateCookieValue, err := h.oidcService.GenerateAuthURL(c.Request.Context(), req.RedirectUri)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "error": (&common.OidcAuthUrlGenerationError{Err: err}).Error()})
		return
	}

	cookie.CreateOidcStateCookie(c, stateCookieValue, 600)

	c.JSON(http.StatusOK, gin.H{
		"authUrl": authUrl,
	})
}

// HandleOidcCallbackGin handles the OIDC callback (requires Gin for cookie).
func (h *OidcHandler) HandleOidcCallbackGin(c *gin.Context) {
	var req struct {
		Code  string `json:"code" binding:"required"`
		State string `json:"state" binding:"required"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": (&common.InvalidRequestFormatError{Err: err}).Error()})
		return
	}

	encodedStateFromCookie, err := cookie.GetOidcStateCookie(c)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": (&common.OidcStateCookieError{}).Error()})
		return
	}
	cookie.ClearOidcStateCookie(c)

	userInfo, tokenResp, err := h.oidcService.HandleCallback(c.Request.Context(), req.Code, req.State, encodedStateFromCookie)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": (&common.OidcCallbackError{Err: err}).Error()})
		return
	}

	userModel, tokenPair, err := h.authService.OidcLogin(c.Request.Context(), *userInfo, tokenResp)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": (&common.AuthFailedError{Err: err}).Error()})
		return
	}

	c.SetSameSite(http.SameSiteLaxMode)
	maxAge := int(time.Until(tokenPair.ExpiresAt).Seconds())
	if maxAge < 0 {
		maxAge = 0
	}
	maxAge += 60
	cookie.CreateTokenCookie(c, maxAge, tokenPair.AccessToken)

	c.JSON(http.StatusOK, gin.H{
		"success":      true,
		"token":        tokenPair.AccessToken,
		"refreshToken": tokenPair.RefreshToken,
		"expiresAt":    tokenPair.ExpiresAt,
		"user": user.User{
			ID:            userModel.ID,
			Username:      userModel.Username,
			DisplayName:   userModel.DisplayName,
			Email:         userModel.Email,
			Roles:         userModel.Roles,
			OidcSubjectId: userModel.OidcSubjectId,
		},
	})
}

// Unused but needed for import
var _ = humamw.GetCurrentUserFromContext
