//go:build buildables

package api

import (
	"errors"
	"net/http"

	"github.com/getarcaneapp/arcane/types/v3"
	"github.com/labstack/echo/v5"
	"go.getarcane.app/kit/pkg/mapping"

	"github.com/getarcaneapp/arcane/backend/v2/buildables"
	"github.com/getarcaneapp/arcane/backend/v2/internal/auth"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/utils/cookie"
)

// SetupBuildablesRoutes registers buildable feature routes based on EnabledFeatures.
func SetupBuildablesRoutes(apiGroup *echo.Group, authService *auth.AuthService) {
	if buildables.HasBuildFeature("autologin") {
		registerAutoLoginRoutes(apiGroup, authService)
	}
}

func registerAutoLoginRoutes(apiGroup *echo.Group, authService *auth.AuthService) {
	authGroup := apiGroup.Group("/auth")

	authGroup.GET("/autologin", func(c *echo.Context) error {
		if authService == nil {
			return c.JSON(http.StatusInternalServerError, types.ErrorResponse{Error: "service not available"})
		}

		config, err := authService.GetAutoLoginConfig(c.Request().Context())
		if err != nil {
			return c.JSON(http.StatusInternalServerError, types.ErrorResponse{Error: "failed to get auto-login config"})
		}

		return c.JSON(http.StatusOK, types.ApiResponse[types.AutoLoginConfig]{
			Success: true,
			Data:    *config,
		})
	})

	authGroup.POST("/autologin", func(c *echo.Context) error {
		if authService == nil {
			return c.JSON(http.StatusInternalServerError, types.ErrorResponse{Error: "service not available"})
		}

		autoLoginConfig, err := authService.GetAutoLoginConfig(c.Request().Context())
		if err != nil {
			return c.JSON(http.StatusInternalServerError, types.ErrorResponse{Error: "failed to get auto-login config"})
		}

		if !autoLoginConfig.Enabled {
			return c.JSON(http.StatusBadRequest, types.ErrorResponse{Error: "auto-login is not enabled"})
		}

		password := authService.GetAutoLoginPassword()
		if password == "" {
			return c.JSON(http.StatusInternalServerError, types.ErrorResponse{Error: "auto-login password not configured"})
		}

		userModel, tokenPair, err := authService.Login(c.Request().Context(), autoLoginConfig.Username, password, types.SessionMeta{
			UserAgent: c.Request().UserAgent(),
			IPAddress: c.RealIP(),
		})
		if err != nil {
			switch {
			case errors.Is(err, auth.ErrInvalidCredentials):
				return c.JSON(http.StatusUnauthorized, types.ErrorResponse{Error: "Invalid username or password"})
			case errors.Is(err, auth.ErrLocalAuthDisabled):
				return c.JSON(http.StatusBadRequest, types.ErrorResponse{Error: "Local authentication is disabled"})
			case errors.Is(err, auth.ErrMFARequired):
				return c.JSON(http.StatusForbidden, types.ErrorResponse{Error: "passkey MFA is required; auto-login cannot bypass MFA"})
			default:
				return c.JSON(http.StatusInternalServerError, types.ErrorResponse{Error: "Authentication failed"})
			}
		}

		var userResp types.User
		if mapErr := mapping.MapStruct(userModel, &userResp); mapErr != nil {
			return c.JSON(http.StatusInternalServerError, types.ErrorResponse{Error: "Failed to map user"})
		}

		expiresAt := tokenPair.ExpiresAt

		for _, tokenCookie := range tokenPair.CookieHeaders(cookie.SecureCookieFromRequest(c.Request())) {
			c.Response().Header().Add("Set-Cookie", tokenCookie)
		}
		return c.JSON(http.StatusOK, types.ApiResponse[types.AuthenticationResponse]{
			Success: true,
			Data: types.AuthenticationResponse{
				Status:       types.AuthenticationStatusAuthenticated,
				Token:        tokenPair.AccessToken,
				RefreshToken: tokenPair.RefreshToken,
				ExpiresAt:    new(expiresAt),
				User:         new(userResp),
			},
		})
	})
}
