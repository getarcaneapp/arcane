package middleware

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"strings"

	"github.com/getarcaneapp/arcane/backend/internal/common"
	"github.com/getarcaneapp/arcane/backend/internal/config"
	"github.com/getarcaneapp/arcane/backend/internal/models"
	"github.com/getarcaneapp/arcane/backend/internal/services"
	pkgutils "github.com/getarcaneapp/arcane/backend/pkg/utils"
	"github.com/getarcaneapp/arcane/backend/pkg/utils/cookie"
	"github.com/labstack/echo/v4"
)

type AuthOptions struct {
	AdminRequired   bool
	SuccessOptional bool
}

type ApiKeyValidator interface {
	ValidateApiKey(ctx context.Context, rawKey string) (*models.User, error)
}

type EnvironmentAccessTokenResolver interface {
	ResolveEnvironmentByAccessToken(ctx context.Context, token string) (*models.Environment, error)
}

type AuthMiddleware struct {
	authService      *services.AuthService
	apiKeyValidator  ApiKeyValidator
	envTokenResolver EnvironmentAccessTokenResolver
	cfg              *config.Config
	options          AuthOptions
}

func NewAuthMiddleware(authService *services.AuthService, cfg *config.Config) *AuthMiddleware {
	return &AuthMiddleware{
		authService: authService,
		cfg:         cfg,
		options:     AuthOptions{},
	}
}

func (m *AuthMiddleware) WithApiKeyValidator(validator ApiKeyValidator) *AuthMiddleware {
	clone := *m
	clone.apiKeyValidator = validator
	return &clone
}

func (m *AuthMiddleware) WithEnvironmentAccessTokenResolver(resolver EnvironmentAccessTokenResolver) *AuthMiddleware {
	clone := *m
	clone.envTokenResolver = resolver
	return &clone
}

func (m *AuthMiddleware) WithAdminNotRequired() *AuthMiddleware {
	clone := *m
	clone.options.AdminRequired = false
	return &clone
}

func (m *AuthMiddleware) WithAdminRequired() *AuthMiddleware {
	clone := *m
	clone.options.AdminRequired = true
	return &clone
}

func (m *AuthMiddleware) Add() echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			reqCtx := c.Request().Context()
			if m.cfg != nil && m.cfg.AgentMode {
				return m.agentAuth(reqCtx, c, next)
			}
			return m.managerAuth(reqCtx, c, next)
		}
	}
}

func (m *AuthMiddleware) agentAuth(ctx context.Context, c echo.Context, next echo.HandlerFunc) error {
	if isPreflightInternal(c) {
		return next(c)
	}

	req := c.Request()
	if strings.HasPrefix(req.URL.Path, pkgutils.AgentPairingPrefix) &&
		m.cfg.AgentToken != "" &&
		req.Header.Get(pkgutils.HeaderAgentBootstrap) == m.cfg.AgentToken {
		slog.InfoContext(ctx, "Agent auth: bootstrap pairing accepted", "path", req.URL.Path, "method", req.Method)
		agentSudoInternal(c)
		return next(c)
	}

	if tok := req.Header.Get(pkgutils.HeaderAgentToken); tok != "" && m.cfg.AgentToken != "" && tok == m.cfg.AgentToken {
		agentSudoInternal(c)
		return next(c)
	}

	// Check for API key as agent token
	if tok := req.Header.Get(pkgutils.HeaderApiKey); tok != "" && m.cfg.AgentToken != "" && tok == m.cfg.AgentToken {
		agentSudoInternal(c)
		return next(c)
	}

	slog.WarnContext(ctx, "Agent auth forbidden",
		"path", req.URL.Path,
		"method", req.Method,
		"has_agent_token_hdr", req.Header.Get(pkgutils.HeaderAgentToken) != "",
		"agent_token_config_set", m.cfg.AgentToken != "",
	)
	return c.JSON(http.StatusForbidden, models.APIError{
		Code:    "FORBIDDEN",
		Message: "Invalid or missing agent token",
	})
}

func (m *AuthMiddleware) managerAuth(ctx context.Context, c echo.Context, next echo.HandlerFunc) error {
	req := c.Request()
	if agentToken := req.Header.Get(pkgutils.HeaderAgentToken); agentToken != "" {
		if env, ok := m.resolveEnvironmentAccessToken(ctx, agentToken); ok {
			environmentSudoInternal(c, env)
			return next(c)
		}
	}

	// First, check for API key in X-API-Key header
	if apiKey := req.Header.Get(pkgutils.HeaderApiKey); apiKey != "" {
		if m.apiKeyValidator != nil {
			user, err := m.apiKeyValidator.ValidateApiKey(ctx, apiKey)
			if err == nil && user != nil {
				isAdmin := pkgutils.UserHasRole(user.Roles, "admin")
				if m.options.AdminRequired && !isAdmin {
					return c.JSON(http.StatusForbidden, models.APIError{
						Code:    "FORBIDDEN",
						Message: "You don't have permission to access this resource",
					})
				}
				c.Set("userID", user.ID)
				c.Set("currentUser", user)
				c.Set("userIsAdmin", isAdmin)
				c.Set("authMethod", "api_key")
				return next(c)
			}
		}
		if env, ok := m.resolveEnvironmentAccessToken(ctx, apiKey); ok {
			environmentSudoInternal(c, env)
			return next(c)
		}
		return c.JSON(http.StatusUnauthorized, models.APIError{
			Code:    models.APIErrorCodeUnauthorized,
			Message: "Invalid or expired API key",
		})
	}

	token := extractBearerOrCookieTokenInternal(c)
	if token == "" {
		if m.options.SuccessOptional {
			return next(c)
		}
		return c.JSON(http.StatusUnauthorized, models.APIError{
			Code:    models.APIErrorCodeUnauthorized,
			Message: "Authentication required",
		})
	}

	user, sessionID, err := m.authService.VerifyToken(ctx, token)
	if err != nil {
		if errors.Is(err, services.ErrTokenVersionMismatch) || common.IsSessionRevokedError(err) || common.IsTokenValidationError(err) {
			cookie.ClearTokenCookie(c.Response().Writer, req)
			return c.JSON(http.StatusUnauthorized, models.APIError{
				Code:    models.APIErrorCodeUnauthorized,
				Message: "Session expired. Please log in again.",
			})
		}

		if m.options.SuccessOptional {
			return next(c)
		}
		return c.JSON(http.StatusUnauthorized, models.APIError{
			Code:    models.APIErrorCodeUnauthorized,
			Message: "Invalid or expired token",
		})
	}

	isAdmin := pkgutils.UserHasRole(user.Roles, "admin")
	if m.options.AdminRequired && !isAdmin {
		return c.JSON(http.StatusForbidden, models.APIError{
			Code:    "FORBIDDEN",
			Message: "You don't have permission to access this resource",
		})
	}

	c.Set("userID", user.ID)
	c.Set("currentUser", user)
	c.Set("currentSessionID", sessionID)
	c.Set("userIsAdmin", isAdmin)
	return next(c)
}

func (m *AuthMiddleware) resolveEnvironmentAccessToken(ctx context.Context, token string) (*models.Environment, bool) {
	if m.envTokenResolver == nil {
		return nil, false
	}

	env, err := m.envTokenResolver.ResolveEnvironmentByAccessToken(ctx, token)
	if err != nil || env == nil {
		return nil, false
	}

	return env, true
}

func isPreflightInternal(c echo.Context) bool {
	return c.Request().Method == http.MethodOptions
}

func agentSudoInternal(c echo.Context) {
	agentUser := &models.User{
		BaseModel: models.BaseModel{ID: "agent"},
		Email:     new("agent@getarcane.app"),
		Username:  "agent",
		Roles:     []string{"admin"},
	}
	c.Set("userID", agentUser.ID)
	c.Set("currentUser", agentUser)
	c.Set("userIsAdmin", true)
	c.Set("authMethod", "agent_token")
}

func environmentSudoInternal(c echo.Context, env *models.Environment) {
	envUser := &models.User{
		BaseModel: models.BaseModel{ID: "environment:" + env.ID},
		Username:  env.Name,
		Roles:     []string{"admin"},
	}
	c.Set("userID", envUser.ID)
	c.Set("currentUser", envUser)
	c.Set("userIsAdmin", true)
	c.Set("authMethod", "environment_access_token")
}

func extractBearerOrCookieTokenInternal(c echo.Context) string {
	req := c.Request()
	authHeader := req.Header.Get("Authorization")
	if after, ok := strings.CutPrefix(authHeader, "Bearer "); ok {
		return after
	}
	if tok, err := cookie.GetTokenCookie(req); err == nil && tok != "" {
		return tok
	}
	return ""
}
