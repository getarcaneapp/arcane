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
	"github.com/getarcaneapp/arcane/backend/internal/utils/cookie"
	"github.com/gin-gonic/gin"
)

const (
	headerAgentBootstrap = "X-Arcane-Agent-Bootstrap"
	headerAgentToken     = "X-Arcane-Agent-Token" // #nosec G101: header name, not a credential
	headerApiToken       = "X-API-TOKEN"          // #nosec G101: header name, not a credential
	agentPairingPrefix   = "/api/environments/0/agent/pair"
)

type AuthOptions struct {
	AdminRequired   bool
	SuccessOptional bool
}

type ApiKeyValidator interface {
	ValidateApiKey(ctx context.Context, rawKey string) (*models.User, error)
}

type AuthMiddleware struct {
	authService     *services.AuthService
	apiKeyValidator ApiKeyValidator
	cfg             *config.Config
	options         AuthOptions
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

func (m *AuthMiddleware) WithAdminRequired() *AuthMiddleware {
	clone := *m
	clone.options.AdminRequired = true
	return &clone
}
func (m *AuthMiddleware) WithAdminNotRequired() *AuthMiddleware {
	clone := *m
	clone.options.AdminRequired = false
	return &clone
}
func (m *AuthMiddleware) WithSuccessOptional() *AuthMiddleware {
	clone := *m
	clone.options.SuccessOptional = true
	return &clone
}

func (m *AuthMiddleware) Add() gin.HandlerFunc {
	return func(c *gin.Context) {
		if m.cfg != nil && m.cfg.AgentMode {
			m.agentAuth(c)
			return
		}
		m.managerAuth(c)
	}
}

func (m *AuthMiddleware) agentAuth(c *gin.Context) {
	if isPreflight(c) {
		c.Next()
		return
	}

	if strings.HasPrefix(c.Request.URL.Path, agentPairingPrefix) &&
		m.cfg.AgentBootstrapToken != "" &&
		c.GetHeader(headerAgentBootstrap) == m.cfg.AgentBootstrapToken {
		slog.Info("Agent auth: bootstrap pairing accepted", "path", c.Request.URL.Path, "method", c.Request.Method)
		agentSudo(c)
		return
	}

	if tok := c.GetHeader(headerAgentToken); tok != "" && m.cfg.AgentToken != "" && tok == m.cfg.AgentToken {
		agentSudo(c)
		return
	}

	slog.Warn("Agent auth forbidden",
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

func (m *AuthMiddleware) managerAuth(c *gin.Context) {
	// First, check for API token in X-API-TOKEN header
	if apiToken := c.GetHeader(headerApiToken); apiToken != "" && m.apiKeyValidator != nil {
		user, err := m.apiKeyValidator.ValidateApiKey(c.Request.Context(), apiToken)
		if err == nil && user != nil {
			isAdmin := userHasRole(user, "admin")
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

	user, err := m.authService.VerifyToken(c.Request.Context(), token)
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

	isAdmin := userHasRole(user, "admin")
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

func isPreflight(c *gin.Context) bool {
	return c.Request.Method == http.MethodOptions
}

func agentSudo(c *gin.Context) {
	email := "agent@arcane.dev"
	agentUser := &models.User{
		BaseModel: models.BaseModel{ID: "agent"},
		Email:     &email,
		Roles:     []string{"admin"},
	}
	c.Set("userID", agentUser.ID)
	c.Set("currentUser", agentUser)
	c.Set("userIsAdmin", true)
	c.Next()
}

func extractBearerOrCookieToken(c *gin.Context) string {
	authHeader := c.GetHeader("Authorization")
	if strings.HasPrefix(authHeader, "Bearer ") {
		return strings.TrimPrefix(authHeader, "Bearer ")
	}
	if tok, err := cookie.GetTokenCookie(c); err == nil && tok != "" {
		return tok
	}
	return ""
}

func userHasRole(user *models.User, role string) bool {
	for _, r := range user.Roles {
		if r == role {
			return true
		}
	}
	return false
}

func GetCurrentUserID(c *gin.Context) (string, bool) {
	userID, exists := c.Get("userID")
	if !exists {
		return "", false
	}
	userIDStr, ok := userID.(string)
	return userIDStr, ok
}

func GetCurrentUser(c *gin.Context) (*models.User, bool) {
	user, exists := c.Get("currentUser")
	if !exists {
		return nil, false
	}
	u, ok := user.(*models.User)
	return u, ok
}

func RequireAuthentication(c *gin.Context) (*models.User, bool) {
	user, exists := GetCurrentUser(c)
	if !exists {
		c.JSON(http.StatusUnauthorized, models.APIError{
			Code:    models.APIErrorCodeUnauthorized,
			Message: "Authentication required",
		})
		c.Abort()
		return nil, false
	}
	return user, true
}
