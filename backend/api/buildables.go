//go:build buildables

package api

import (
	"errors"
	"net/http"
	"time"

	"github.com/getarcaneapp/arcane/backend/buildables"
	"github.com/getarcaneapp/arcane/backend/internal/common"
	"github.com/getarcaneapp/arcane/backend/internal/services"
	"github.com/getarcaneapp/arcane/backend/pkg/utils/cookie"
	"github.com/getarcaneapp/arcane/backend/pkg/utils/mapper"
	"github.com/getarcaneapp/arcane/types/auth"
	"github.com/getarcaneapp/arcane/types/base"
	"github.com/getarcaneapp/arcane/types/user"
	"github.com/labstack/echo/v4"
)

// SetupBuildablesRoutes registers buildable feature routes based on EnabledFeatures.
func SetupBuildablesRoutes(apiGroup *echo.Group, authService *services.AuthService) {
	if buildables.HasBuildFeature("autologin") {
		registerAutoLoginRoutes(apiGroup, authService)
	}
}

func registerAutoLoginRoutes(apiGroup *echo.Group, authService *services.AuthService) {
	authGroup := apiGroup.Group("/auth")

	authGroup.GET("/auto-login-config", func(c echo.Context) error {
		if authService == nil {
			return c.JSON(http.StatusInternalServerError, base.ErrorResponse{Error: "service not available"})
		}

		config, err := authService.GetAutoLoginConfig(c.Request().Context())
		if err != nil {
			return c.JSON(http.StatusInternalServerError, base.ErrorResponse{Error: "failed to get auto-login config"})
		}

		return c.JSON(http.StatusOK, base.ApiResponse[auth.AutoLoginConfig]{
			Success: true,
			Data:    *config,
		})
	})

	authGroup.POST("/auto-login", func(c echo.Context) error {
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

		userModel, tokenPair, err := authService.Login(c.Request().Context(), autoLoginConfig.Username, password, auth.SessionMeta{
			UserAgent: c.Request().UserAgent(),
			IPAddress: c.RealIP(),
		})
		if err != nil {
			switch {
			case errors.Is(err, services.ErrInvalidCredentials):
				return c.JSON(http.StatusUnauthorized, base.ErrorResponse{Error: (&common.InvalidCredentialsError{}).Error()})
			case errors.Is(err, services.ErrLocalAuthDisabled):
				return c.JSON(http.StatusBadRequest, base.ErrorResponse{Error: (&common.LocalAuthDisabledError{}).Error()})
			default:
				return c.JSON(http.StatusInternalServerError, base.ErrorResponse{Error: (&common.AuthFailedError{Err: err}).Error()})
			}
		}

		var userResp user.User
		if mapErr := mapper.MapStruct(userModel, &userResp); mapErr != nil {
			return c.JSON(http.StatusInternalServerError, base.ErrorResponse{Error: (&common.UserMappingError{Err: mapErr}).Error()})
		}

		maxAge := int(time.Until(tokenPair.ExpiresAt).Seconds())
		if maxAge < 0 {
			maxAge = 0
		}
		maxAge += 60

		c.Response().Header().Set("Set-Cookie", cookie.BuildTokenCookieString(maxAge, tokenPair.AccessToken))
		return c.JSON(http.StatusOK, base.ApiResponse[auth.LoginResponse]{
			Success: true,
			Data: auth.LoginResponse{
				Token:        tokenPair.AccessToken,
				RefreshToken: tokenPair.RefreshToken,
				ExpiresAt:    tokenPair.ExpiresAt,
				User:         userResp,
			},
		})
	})
}
