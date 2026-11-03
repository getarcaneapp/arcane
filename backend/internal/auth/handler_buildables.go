//go:build buildables

package auth

import (
	"errors"
	"net/http"

	"github.com/danielgtaylor/huma/v2"
	"github.com/labstack/echo/v5"

	"kit.getarcane.app/mapping"

	"getarcane.dev/arcane/v3/buildables"
	"getarcane.dev/arcane/v3/internal/middleware"
	"getarcane.dev/arcane/v3/pkg/authz"
	"getarcane.dev/arcane/v3/pkg/cookie"
	"getarcane.dev/types/v3"
)

type autoLoginHandler struct {
	authService *AuthService
}

func init() {
	buildableRoutes = append(buildableRoutes, registerAutoLogin)
}

func registerAutoLogin(api huma.API, authService *AuthService) {
	if !buildables.HasBuildFeature("autologin") {
		return
	}
	h := &autoLoginHandler{authService: authService}
	huma.Register(api, huma.Operation{
		OperationID: "get-auto-login-config",
		Method:      http.MethodGet,
		Path:        "/auth/autologin",
		Tags:        []string{"Auth"},
		Hidden:      true,
		Security:    []map[string][]string{},
		Metadata:    map[string]any{authz.MetaAccess: authz.EndpointAccessPublic},
	}, middleware.EchoHandler(h.GetConfig))
	huma.Register(api, huma.Operation{
		OperationID: "auto-login",
		Method:      http.MethodPost,
		Path:        "/auth/autologin",
		Tags:        []string{"Auth"},
		Hidden:      true,
		Security:    []map[string][]string{},
		Metadata:    map[string]any{authz.MetaAccess: authz.EndpointAccessPublic},
	}, middleware.EchoHandler(h.Login))
}

func (h *autoLoginHandler) GetConfig(c *echo.Context) error {
	config, err := h.authService.GetAutoLoginConfig(c.Request().Context())
	if err != nil {
		return c.JSON(http.StatusInternalServerError, types.ErrorResponse{Error: "failed to get auto-login config"})
	}

	return c.JSON(http.StatusOK, types.ApiResponse[types.AutoLoginConfig]{
		Success: true,
		Data:    *config,
	})
}

func (h *autoLoginHandler) Login(c *echo.Context) error {
	autoLoginConfig, err := h.authService.GetAutoLoginConfig(c.Request().Context())
	if err != nil {
		return c.JSON(http.StatusInternalServerError, types.ErrorResponse{Error: "failed to get auto-login config"})
	}

	if !autoLoginConfig.Enabled {
		return c.JSON(http.StatusBadRequest, types.ErrorResponse{Error: "auto-login is not enabled"})
	}

	password := h.authService.GetAutoLoginPassword()
	if password == "" {
		return c.JSON(http.StatusInternalServerError, types.ErrorResponse{Error: "auto-login password not configured"})
	}

	userModel, tokenPair, err := h.authService.Login(c.Request().Context(), autoLoginConfig.Username, password, types.SessionMeta{
		UserAgent: c.Request().UserAgent(),
		IPAddress: c.RealIP(),
	})
	if err != nil {
		switch {
		case errors.Is(err, ErrInvalidCredentials):
			return c.JSON(http.StatusUnauthorized, types.ErrorResponse{Error: "Invalid username or password"})
		case errors.Is(err, ErrLocalAuthDisabled):
			return c.JSON(http.StatusBadRequest, types.ErrorResponse{Error: "Local authentication is disabled"})
		case errors.Is(err, ErrMFARequired):
			return c.JSON(http.StatusForbidden, types.ErrorResponse{Error: "passkey MFA is required; auto-login cannot bypass MFA"})
		default:
			return c.JSON(http.StatusInternalServerError, types.ErrorResponse{Error: "Authentication failed"})
		}
	}

	var userResp types.User
	if mapErr := mapping.MapStruct(userModel, &userResp); mapErr != nil {
		return c.JSON(http.StatusInternalServerError, types.ErrorResponse{Error: "Failed to map user"})
	}

	for _, tokenCookie := range tokenPair.CookieHeaders(cookie.SecureCookieFromRequest(c.Request())) {
		c.Response().Header().Add("Set-Cookie", tokenCookie)
	}
	return c.JSON(http.StatusOK, types.ApiResponse[types.AuthenticationResponse]{
		Success: true,
		Data: types.AuthenticationResponse{
			Status:       types.AuthenticationStatusAuthenticated,
			Token:        tokenPair.AccessToken,
			RefreshToken: tokenPair.RefreshToken,
			ExpiresAt:    new(tokenPair.ExpiresAt),
			User:         new(userResp),
		},
	})
}
