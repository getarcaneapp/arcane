package middleware

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"strings"

	"github.com/getarcaneapp/arcane/backend/internal/config"
	"github.com/getarcaneapp/arcane/backend/internal/models"
	"github.com/getarcaneapp/arcane/backend/internal/services"
	pkgutils "github.com/getarcaneapp/arcane/backend/pkg/utils"
	"github.com/getarcaneapp/arcane/backend/pkg/utils/cookie"
	"github.com/gin-gonic/gin"
)

const (
	headerAgentBootstrap = "X-Arcane-Agent-Bootstrap"
	headerAgentToken     = "X-Arcane-Agent-Token" // #nosec G101: header name, not a credential
	headerApiKey         = "X-API-Key"            // #nosec G101: header name, not a credential
	agentPairingPrefix   = "/api/environments/0/agent/pair"
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

func (m *AuthMiddleware) Add() gin.HandlerFunc {
	return func(c *gin.Context) {
		reqCtx := c.Request.Context()
		if m.cfg != nil && m.cfg.AgentMode {
			m.agentAuth(reqCtx, c)
			return
		}
		m.managerAuth(reqCtx, c)
	}
}

func (m *AuthMiddleware) agentAuth(ctx context.Context, c *gin.Context) {
	if isPreflight(c) {
		c.Next()
		return
	}

	if strings.HasPrefix(c.Request.URL.Path, agentPairingPrefix) &&
		m.cfg.AgentToken != "" &&
		c.GetHeader(headerAgentBootstrap) == m.cfg.AgentToken {
		slog.InfoContext(ctx, "Agent auth: bootstrap pairing accepted", "path", c.Request.URL.Path, "method", c.Request.Method)
		agentSudo(c)
		return
	}

	if tok := c.GetHeader(headerAgentToken); tok != "" && m.cfg.AgentToken != "" && tok == m.cfg.AgentToken {
		agentSudo(c)
		return
	}

	// Check for API key as agent token
	if tok := c.GetHeader(headerApiKey); tok != "" && m.cfg.AgentToken != "" && tok == m.cfg.AgentToken {
		agentSudo(c)
		return
	}

	slog.WarnContext(ctx, "Agent auth forbidden",
		"path", c.Request.URL.Path,
		"method", c.Request.Method,
		"has_agent_token_hdr", c.GetHeader(headerAgentToken) != "",
		"agent_token_config_set", m.cfg.AgentToken != "",
	)
	c.JSON(http.StatusForbidden, models.APIError{
		Code:    "FORBIDDEN",
		Message: "Invalid or missing agent token",
	})
	c.Abort()
}

func (m *AuthMiddleware) managerAuth(ctx context.Context, c *gin.Context) {
	if agentToken := c.GetHeader(headerAgentToken); agentToken != "" {
		if env, ok := m.resolveEnvironmentAccessToken(ctx, agentToken); ok {
			environmentSudo(c, env)
			return
		}
	}

	// First, check for API key in X-API-Key header
	if apiKey := c.GetHeader(headerApiKey); apiKey != "" {
		if m.apiKeyValidator != nil {
			user, err := m.apiKeyValidator.ValidateApiKey(ctx, apiKey)
			if err == nil && user != nil {
				isAdmin := pkgutils.UserHasRole(user.Roles, "admin")
				if m.options.AdminRequired && !isAdmin {
					c.JSON(http.StatusForbidden, models.APIError{
						Code:    "FORBIDDEN",
						Message: "You don't have permission to access this resource",
					})
					c.Abort()
					return
				}
				c.Set("userID", user.ID)
				c.Set("currentUser", user)
				c.Set("userIsAdmin", isAdmin)
				c.Set("authMethod", "api_key")
				c.Next()
				return
			}
		}
		if env, ok := m.resolveEnvironmentAccessToken(ctx, apiKey); ok {
			environmentSudo(c, env)
			return
		}
		// If API key validation fails, return unauthorized
		c.JSON(http.StatusUnauthorized, models.APIError{
			Code:    models.APIErrorCodeUnauthorized,
			Message: "Invalid or expired API key",
		})
		c.Abort()
		return
	}

	token := extractBearerOrCookieToken(c)
	if token == "" {
		if m.options.SuccessOptional {
			c.Next()
			return
		}
		c.JSON(http.StatusUnauthorized, models.APIError{
			Code:    models.APIErrorCodeUnauthorized,
			Message: "Authentication required",
		})
		c.Abort()
		return
	}

	user, err := m.authService.VerifyToken(ctx, token)
	if err != nil {
		if errors.Is(err, services.ErrTokenVersionMismatch) {
			cookie.ClearTokenCookie(c)
			c.JSON(http.StatusUnauthorized, models.APIError{
				Code:    models.APIErrorCodeUnauthorized,
				Message: "Application has been updated. Please log in again.",
			})
			c.Abort()
			return
		}

		if m.options.SuccessOptional {
			c.Next()
			return
		}
		c.JSON(http.StatusUnauthorized, models.APIError{
			Code:    models.APIErrorCodeUnauthorized,
			Message: "Invalid or expired token",
		})
		c.Abort()
		return
	}

	isAdmin := pkgutils.UserHasRole(user.Roles, "admin")
	if m.options.AdminRequired && !isAdmin {
		c.JSON(http.StatusForbidden, models.APIError{
			Code:    "FORBIDDEN",
			Message: "You don't have permission to access this resource",
		})
		c.Abort()
		return
	}

	c.Set("userID", user.ID)
	c.Set("currentUser", user)
	c.Set("userIsAdmin", isAdmin)
	c.Next()
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

func isPreflight(c *gin.Context) bool {
	return c.Request.Method == http.MethodOptions
}

func agentSudo(c *gin.Context) {
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
	c.Next()
}

func environmentSudo(c *gin.Context, env *models.Environment) {
	envUser := &models.User{
		BaseModel: models.BaseModel{ID: "environment:" + env.ID},
		Username:  env.Name,
		Roles:     []string{"admin"},
	}
	c.Set("userID", envUser.ID)
	c.Set("currentUser", envUser)
	c.Set("userIsAdmin", true)
	c.Set("authMethod", "environment_access_token")
	c.Next()
}

func extractBearerOrCookieToken(c *gin.Context) string {
	authHeader := c.GetHeader("Authorization")
	if after, ok := strings.CutPrefix(authHeader, "Bearer "); ok {
		return after
	}
	if tok, err := cookie.GetTokenCookie(c); err == nil && tok != "" {
		return tok
	}
	return ""
}
