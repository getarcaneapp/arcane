//go:build buildables

package api

import (
	"errors"
	"net/http"
	"time"

	"github.com/getarcaneapp/arcane/backend/buildables"
	"github.com/getarcaneapp/arcane/backend/internal/common"
	"github.com/getarcaneapp/arcane/backend/internal/services"
	"github.com/getarcaneapp/arcane/backend/internal/utils/cookie"
	"github.com/getarcaneapp/arcane/backend/internal/utils/mapper"
	"github.com/getarcaneapp/arcane/types/auth"
	"github.com/getarcaneapp/arcane/types/base"
	"github.com/getarcaneapp/arcane/types/user"
	"github.com/gin-gonic/gin"
)

// SetupBuildablesRoutes registers buildable feature routes based on EnabledFeatures.
func SetupBuildablesRoutes(apiGroup *gin.RouterGroup, authService *services.AuthService) {
	if buildables.HasBuildFeature("autologin") {
		registerAutoLoginRoutes(apiGroup, authService)
	}
}

func registerAutoLoginRoutes(apiGroup *gin.RouterGroup, authService *services.AuthService) {
	authGroup := apiGroup.Group("/auth")

	authGroup.GET("/auto-login-config", func(c *gin.Context) {
		if authService == nil {
			c.JSON(http.StatusInternalServerError, base.ErrorResponse{Error: "service not available"})
			return
		}

		config, err := authService.GetAutoLoginConfig(c.Request.Context())
		if err != nil {
			c.JSON(http.StatusInternalServerError, base.ErrorResponse{Error: "failed to get auto-login config"})
			return
		}

		c.JSON(http.StatusOK, base.ApiResponse[auth.AutoLoginConfig]{
			Success: true,
			Data:    *config,
		})
	})

	authGroup.POST("/auto-login", func(c *gin.Context) {
		if authService == nil {
			c.JSON(http.StatusInternalServerError, base.ErrorResponse{Error: "service not available"})
			return
		}

		autoLoginConfig, err := authService.GetAutoLoginConfig(c.Request.Context())
		if err != nil {
			c.JSON(http.StatusInternalServerError, base.ErrorResponse{Error: "failed to get auto-login config"})
			return
		}

		if !autoLoginConfig.Enabled {
			c.JSON(http.StatusBadRequest, base.ErrorResponse{Error: "auto-login is not enabled"})
			return
		}

		password := authService.GetAutoLoginPassword()
		if password == "" {
			c.JSON(http.StatusInternalServerError, base.ErrorResponse{Error: "auto-login password not configured"})
			return
		}

		userModel, tokenPair, err := authService.Login(c.Request.Context(), autoLoginConfig.Username, password)
		if err != nil {
			switch {
			case errors.Is(err, services.ErrInvalidCredentials):
				c.JSON(http.StatusUnauthorized, base.ErrorResponse{Error: (&common.InvalidCredentialsError{}).Error()})
			case errors.Is(err, services.ErrLocalAuthDisabled):
				c.JSON(http.StatusBadRequest, base.ErrorResponse{Error: (&common.LocalAuthDisabledError{}).Error()})
			default:
				c.JSON(http.StatusInternalServerError, base.ErrorResponse{Error: (&common.AuthFailedError{Err: err}).Error()})
			}
			return
		}

		var userResp user.User
		if mapErr := mapper.MapStruct(userModel, &userResp); mapErr != nil {
			c.JSON(http.StatusInternalServerError, base.ErrorResponse{Error: (&common.UserMappingError{Err: mapErr}).Error()})
			return
		}

		maxAge := int(time.Until(tokenPair.ExpiresAt).Seconds())
		if maxAge < 0 {
			maxAge = 0
		}
		maxAge += 60

		c.Header("Set-Cookie", cookie.BuildTokenCookieString(maxAge, tokenPair.AccessToken))
		c.JSON(http.StatusOK, base.ApiResponse[auth.LoginResponse]{
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
