//go:build buildables

package api

import (
	"net/http"
	"time"

	"emperror.dev/errors"

	"github.com/getarcaneapp/arcane/backend/v2/buildables"
	"github.com/getarcaneapp/arcane/backend/v2/internal/auth"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/utils/cookie"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/utils/mapper"
	authtypes "github.com/getarcaneapp/arcane/types/v2/auth"
	"github.com/getarcaneapp/arcane/types/v2/base"
	"github.com/getarcaneapp/arcane/types/v2/user"
	"github.com/labstack/echo/v5"
)

// SetupBuildablesRoutes registers buildable feature routes based on EnabledFeatures.
func SetupBuildablesRoutes(apiGroup *echo.Group, authService *auth.AuthService) {
	if buildables.HasBuildFeature("autologin") {
		registerAutoLoginRoutes(apiGroup, authService)
	}
}

func registerAutoLoginRoutes(apiGroup *echo.Group, authService *auth.AuthService) {
	authGroup := apiGroup.Group("/auth")

	authGroup.GET("/auto-login-config", func(c *echo.Context) error {
		if authService == nil {
			return c.JSON(http.StatusInternalServerError, base.ErrorResponse{Error: "service not available"})
		}

		config, err := authService.GetAutoLoginConfig(c.Request().Context())
		if err != nil {
			return c.JSON(http.StatusInternalServerError, base.ErrorResponse{Error: "failed to get auto-login config"})
		}

		return c.JSON(http.StatusOK, base.ApiResponse[authtypes.AutoLoginConfig]{
			Success: true,
			Data:    *config,
		})
	})

	authGroup.POST("/auto-login", func(c *echo.Context) error {
		if authService == nil {
			return c.JSON(http.StatusInternalServerError, base.ErrorResponse{Error: "service not available"})
		}

		autoLoginConfig, err := authService.GetAutoLoginConfig(c.Request().Context())
		if err != nil {
			return c.JSON(http.StatusInternalServerError, base.ErrorResponse{Error: "failed to get auto-login config"})
		}

		if !autoLoginConfig.Enabled {
			return c.JSON(http.StatusBadRequest, base.ErrorResponse{Error: "auto-login is not enabled"})
		}

		password := authService.GetAutoLoginPassword()
		if password == "" {
			return c.JSON(http.StatusInternalServerError, base.ErrorResponse{Error: "auto-login password not configured"})
		}

		userModel, tokenPair, err := authService.Login(c.Request().Context(), autoLoginConfig.Username, password, authtypes.SessionMeta{
			UserAgent: c.Request().UserAgent(),
			IPAddress: c.RealIP(),
		})
		if err != nil {
			switch {
			case errors.Is(err, auth.ErrInvalidCredentials):
				return c.JSON(http.StatusUnauthorized, base.ErrorResponse{Error: "Invalid username or password"})
			case errors.Is(err, auth.ErrLocalAuthDisabled):
				return c.JSON(http.StatusBadRequest, base.ErrorResponse{Error: "Local authentication is disabled"})
			case errors.Is(err, auth.ErrMFARequired):
				return c.JSON(http.StatusForbidden, base.ErrorResponse{Error: "passkey MFA is required; auto-login cannot bypass MFA"})
			default:
				return c.JSON(http.StatusInternalServerError, base.ErrorResponse{Error: "Authentication failed"})
			}
		}

		var userResp user.User
		if mapErr := mapper.MapStruct(userModel, &userResp); mapErr != nil {
			return c.JSON(http.StatusInternalServerError, base.ErrorResponse{Error: "Failed to map user"})
		}

		maxAge := max(int(time.Until(tokenPair.ExpiresAt).Seconds()), 0)
		maxAge += 60
		expiresAt := tokenPair.ExpiresAt

		c.Response().Header().Set("Set-Cookie", cookie.BuildTokenCookieStringFor(maxAge, tokenPair.AccessToken, cookie.SecureCookieFromRequest(c.Request())))
		return c.JSON(http.StatusOK, base.ApiResponse[authtypes.AuthenticationResponse]{
			Success: true,
			Data: authtypes.AuthenticationResponse{
				Status:       authtypes.AuthenticationStatusAuthenticated,
				Token:        tokenPair.AccessToken,
				RefreshToken: tokenPair.RefreshToken,
				ExpiresAt:    &expiresAt,
				User:         &userResp,
			},
		})
	})
}
