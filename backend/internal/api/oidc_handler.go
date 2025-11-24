package api

import (
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/ofkm/arcane-backend/internal/common"
	"github.com/ofkm/arcane-backend/internal/dto"
	"github.com/ofkm/arcane-backend/internal/services"
	"github.com/ofkm/arcane-backend/internal/utils/cookie"
)

type OidcHandler struct {
	authService *services.AuthService
	oidcService *services.OidcService
}

func NewOidcHandler(group *gin.RouterGroup, authService *services.AuthService, oidcService *services.OidcService) {

	handler := &OidcHandler{authService: authService, oidcService: oidcService}

	apiGroup := group.Group("/oidc")
	{
		apiGroup.POST("/url", handler.GetOidcAuthUrl)
		apiGroup.POST("/callback", handler.HandleOidcCallback)
		apiGroup.GET("/config", handler.GetOidcConfig)
		apiGroup.GET("/status", handler.GetOidcStatus)
	}
}

func (h *OidcHandler) GetOidcStatus(c *gin.Context) {
	status, err := h.authService.GetOidcConfigurationStatus(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"success": false,
			"error":   (&common.OidcStatusError{Err: err}).Error(),
		})
		return
	}
	c.JSON(http.StatusOK, status)
}

func (h *OidcHandler) GetOidcAuthUrl(c *gin.Context) {
	var req dto.OidcAuthUrlRequest
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

func (h *OidcHandler) HandleOidcCallback(c *gin.Context) {
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

	user, tokenPair, err := h.authService.OidcLogin(c.Request.Context(), *userInfo, tokenResp)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": (&common.AuthFailedError{Err: err}).Error()})
		return
	}

	c.SetSameSite(http.SameSiteLaxMode)
	maxAge := int(time.Until(tokenPair.ExpiresAt).Seconds())
	if maxAge < 0 {
		maxAge = 0
	}
	// Add 60 seconds buffer to account for clock skew and network latency
	maxAge += 60
	cookie.CreateTokenCookie(c, maxAge, tokenPair.AccessToken)

	c.JSON(http.StatusOK, gin.H{
		"success":      true,
		"token":        tokenPair.AccessToken,
		"refreshToken": tokenPair.RefreshToken,
		"expiresAt":    tokenPair.ExpiresAt,
		"user": dto.UserResponseDto{
			ID:            user.ID,
			Username:      user.Username,
			DisplayName:   user.DisplayName,
			Email:         user.Email,
			Roles:         user.Roles,
			OidcSubjectId: user.OidcSubjectId,
		},
	})
}

func (h *OidcHandler) GetOidcConfig(c *gin.Context) {
	config, err := h.authService.GetOidcConfig(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"success": false,
			"error":   (&common.OidcConfigError{}).Error(),
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"clientId":              config.ClientID,
		"redirectUri":           h.oidcService.GetOidcRedirectURL(),
		"issuerUrl":             config.IssuerURL,
		"authorizationEndpoint": config.AuthorizationEndpoint,
		"tokenEndpoint":         config.TokenEndpoint,
		"userinfoEndpoint":      config.UserinfoEndpoint,
		"scopes":                config.Scopes,
	})
}
