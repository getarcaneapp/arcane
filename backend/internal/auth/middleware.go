package auth

import (
	"github.com/getarcaneapp/arcane/backend/v2/internal/environment"

	"github.com/getarcaneapp/arcane/backend/v2/internal/apikey"

	"github.com/samber/mo"

	"context"
	"crypto/subtle"
	"log/slog"
	"net/http"
	"strings"

	"emperror.dev/errors"

	"github.com/getarcaneapp/arcane/backend/v2/internal/common"
	"github.com/getarcaneapp/arcane/backend/v2/internal/config"
	"github.com/getarcaneapp/arcane/backend/v2/internal/middleware"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/authz"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/utils"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/utils/cookie"
	"github.com/labstack/echo/v5"
)

type AuthOptions struct {
	AdminRequired   bool
	SuccessOptional bool
}

type ApiKeyValidator interface {
	ValidateApiKeyWithID(ctx context.Context, rawKey string) (*common.User, *apikey.ApiKey, error)
}

type EnvironmentAccessTokenResolver interface {
	ResolveEnvironmentByAccessToken(ctx context.Context, token string) (*environment.Environment, error)
}

// PermissionResolver resolves a caller's effective permission set. Implemented
// by role.RoleService; kept as an interface so tests can stub it.
type PermissionResolver interface {
	ResolvePermissions(ctx context.Context, user *common.User) (*authz.PermissionSet, error)
	ResolveApiKeyPermissions(ctx context.Context, apiKeyID string) (*authz.PermissionSet, error)
}

type AuthMiddleware struct {
	authService      *AuthService
	apiKeyValidator  ApiKeyValidator
	envTokenResolver EnvironmentAccessTokenResolver
	roleResolver     PermissionResolver
	cfg              *config.Config
	options          AuthOptions
}

func NewAuthMiddleware(authService *AuthService, cfg *config.Config) *AuthMiddleware {
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

func (m *AuthMiddleware) WithPermissionResolver(resolver PermissionResolver) *AuthMiddleware {
	clone := *m
	clone.roleResolver = resolver
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
		return func(c *echo.Context) error {
			reqCtx := c.Request().Context()
			if m.cfg != nil && m.cfg.AgentMode {
				return m.agentAuth(reqCtx, c, next)
			}
			return m.managerAuth(reqCtx, c, next)
		}
	}
}

func (m *AuthMiddleware) agentAuth(ctx context.Context, c *echo.Context, next echo.HandlerFunc) error {
	if isPreflightInternal(c) {
		return next(c)
	}

	req := c.Request()
	if strings.HasPrefix(req.URL.Path, utils.AgentPairingPrefix) &&
		AgentTokenMatches(req.Header.Get(utils.HeaderAgentBootstrap), m.cfg.AgentToken) {
		slog.InfoContext(ctx, "Agent auth: bootstrap pairing accepted", "path", req.URL.Path, "method", req.Method)
		agentSudoInternal(c)
		return next(c)
	}

	if AgentTokenMatches(req.Header.Get(utils.HeaderAgentToken), m.cfg.AgentToken) {
		agentSudoInternal(c)
		return next(c)
	}

	// Check for API key as agent token
	if AgentTokenMatches(req.Header.Get(utils.HeaderApiKey), m.cfg.AgentToken) {
		agentSudoInternal(c)
		return next(c)
	}

	slog.WarnContext(ctx, "Agent auth forbidden",
		"path", req.URL.Path,
		"method", req.Method,
		"has_agent_token_hdr", req.Header.Get(utils.HeaderAgentToken) != "",
		"agent_token_config_set", m.cfg.AgentToken != "",
	)
	return c.JSON(http.StatusForbidden, common.APIError{
		Code:    "FORBIDDEN",
		Message: "Invalid or missing agent token",
	})
}

// AgentTokenMatches compares a presented token against the configured
// agent token in constant time to avoid timing side channels.
func AgentTokenMatches(presented, configured string) bool {
	if presented == "" || configured == "" {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(presented), []byte(configured)) == 1
}

func (m *AuthMiddleware) managerAuth(ctx context.Context, c *echo.Context, next echo.HandlerFunc) error {
	req := c.Request()
	if agentToken := req.Header.Get(utils.HeaderAgentToken); agentToken != "" {
		if env, ok := m.resolveEnvironmentAccessToken(ctx, agentToken).Get(); ok {
			environmentScopedInternal(c, env)
			return next(c)
		}
	}

	// First, check for API key in X-API-Key header
	if apiKey := req.Header.Get(utils.HeaderApiKey); apiKey != "" {
		return m.apiKeyHeaderAuth(ctx, c, next, apiKey)
	}

	token, fromCookie := extractBearerOrCookieTokenInternal(c)
	if token == "" {
		if m.options.SuccessOptional {
			return next(c)
		}
		return c.JSON(http.StatusUnauthorized, common.APIError{
			Code:    common.APIErrorCodeUnauthorized,
			Message: "Authentication required",
		})
	}

	var user *common.User
	var sessionID string
	var err error
	if fromCookie {
		user, sessionID, err = m.authService.VerifyBrowserToken(ctx, token)
	} else {
		user, sessionID, err = m.authService.VerifyToken(ctx, token)
	}
	if err != nil {
		// A version mismatch means the app self-updated; the session is still valid
		// (the refresh path tolerates the version change and rotates the token), so do
		// NOT clear the cookies. Return a recoverable 401 the frontend refreshes from.
		if errors.Is(err, common.ErrTokenVersionMismatch) {
			return c.JSON(http.StatusUnauthorized, common.APIError{
				Code:    common.APIErrorCodeUnauthorized,
				Message: "Application has been updated. Refreshing session.",
			})
		}

		if errors.Is(err, common.ErrSessionRevoked) || errors.Is(err, common.ErrTokenValidation) {
			cookie.ClearTokenCookie(c.Response(), req)
			return c.JSON(http.StatusUnauthorized, common.APIError{
				Code:    common.APIErrorCodeUnauthorized,
				Message: "Session expired. Please log in again.",
			})
		}

		if m.options.SuccessOptional {
			return next(c)
		}
		return c.JSON(http.StatusUnauthorized, common.APIError{
			Code:    common.APIErrorCodeUnauthorized,
			Message: "Invalid or expired token",
		})
	}

	ps := m.resolvePermissionsOrDeny(ctx, user)
	if m.options.AdminRequired && !ps.IsGlobalAdmin() {
		return c.JSON(http.StatusForbidden, common.APIError{
			Code:    "FORBIDDEN",
			Message: "You don't have permission to access this resource",
		})
	}

	c.Set(string(middleware.ContextKeyUserID), user.ID)
	c.Set(string(middleware.ContextKeyCurrentUser), user)
	c.Set(string(middleware.ContextKeyCurrentSessionID), sessionID)
	c.Set(string(middleware.ContextKeyUserPermissions), ps)
	return next(c)
}

// apiKeyHeaderAuth authenticates an X-API-Key credential: a user-owned API key
// first, then an environment access token presented through the same header.
func (m *AuthMiddleware) apiKeyHeaderAuth(ctx context.Context, c *echo.Context, next echo.HandlerFunc, apiKey string) error {
	if m.apiKeyValidator != nil {
		user, key, err := m.apiKeyValidator.ValidateApiKeyWithID(ctx, apiKey)
		if err == nil && user != nil {
			// Personal keys inherit the owner's role permissions (same
			// resolution as session auth); scoped keys use their own grants.
			var ps *authz.PermissionSet
			if key.Kind == apikey.ApiKeyKindPersonal {
				ps = m.resolvePermissionsOrDeny(ctx, user)
			} else {
				ps = m.resolveApiKeyPermissionsOrDeny(ctx, key.ID)
			}
			if m.options.AdminRequired && !ps.IsGlobalAdmin() {
				return c.JSON(http.StatusForbidden, common.APIError{
					Code:    "FORBIDDEN",
					Message: "You don't have permission to access this resource",
				})
			}
			c.Set(string(middleware.ContextKeyUserID), user.ID)
			c.Set(string(middleware.ContextKeyCurrentUser), user)
			c.Set(string(middleware.ContextKeyUserPermissions), ps)
			c.Set(string(middleware.ContextKeyAuthMethod), "api_key")
			return next(c)
		}
	}
	if env, ok := m.resolveEnvironmentAccessToken(ctx, apiKey).Get(); ok {
		environmentScopedInternal(c, env)
		return next(c)
	}
	return c.JSON(http.StatusUnauthorized, common.APIError{
		Code:    common.APIErrorCodeUnauthorized,
		Message: "Invalid or expired API key",
	})
}

// resolvePermissionsOrDeny returns the user's permission set, or an empty
// (deny-all) set if the resolver is unavailable or fails. Resolver failures
// are logged.
func (m *AuthMiddleware) resolvePermissionsOrDeny(ctx context.Context, user *common.User) *authz.PermissionSet {
	if m.roleResolver == nil || user == nil {
		return authz.NewPermissionSet()
	}
	ps, err := m.roleResolver.ResolvePermissions(ctx, user)
	if err != nil || ps == nil {
		slog.WarnContext(ctx, "failed to resolve user permissions for Echo auth", "error", err)
		return authz.NewPermissionSet()
	}
	return ps
}

// resolveApiKeyPermissionsOrDeny returns the API key's per-key PermissionSet,
// or an empty (deny-all) set on failure. Falling back to the owner's role
// permissions would defeat per-key scoping, so failures are explicitly denied.
func (m *AuthMiddleware) resolveApiKeyPermissionsOrDeny(ctx context.Context, apiKeyID string) *authz.PermissionSet {
	if m.roleResolver == nil || apiKeyID == "" {
		return authz.NewPermissionSet()
	}
	ps, err := m.roleResolver.ResolveApiKeyPermissions(ctx, apiKeyID)
	if err != nil || ps == nil {
		slog.WarnContext(ctx, "failed to resolve api key permissions for Echo auth", "error", err)
		return authz.NewPermissionSet()
	}
	return ps
}

func (m *AuthMiddleware) resolveEnvironmentAccessToken(ctx context.Context, token string) mo.Option[*environment.Environment] {
	if m.envTokenResolver == nil {
		return mo.None[*environment.Environment]()
	}

	env, err := m.envTokenResolver.ResolveEnvironmentByAccessToken(ctx, token)
	if err != nil || env == nil {
		return mo.None[*environment.Environment]()
	}

	return mo.Some(env)
}

func isPreflightInternal(c *echo.Context) bool {
	return c.Request().Method == http.MethodOptions
}

func agentSudoInternal(c *echo.Context) {
	agentUser := &common.User{
		ID:       "agent",
		Email:    new("agent@getarcane.app"),
		Username: "agent",
	}
	c.Set(string(middleware.ContextKeyUserID), agentUser.ID)
	c.Set(string(middleware.ContextKeyCurrentUser), agentUser)
	c.Set(string(middleware.ContextKeyUserPermissions), authz.SudoPermissionSet())
	c.Set(string(middleware.ContextKeyAuthMethod), "agent_token")
}

func environmentScopedInternal(c *echo.Context, env *environment.Environment) {
	envUser := &common.User{
		ID:       "environment:" + env.ID,
		Username: env.Name,
	}
	c.Set(string(middleware.ContextKeyUserID), envUser.ID)
	c.Set(string(middleware.ContextKeyCurrentUser), envUser)
	c.Set(string(middleware.ContextKeyUserPermissions), authz.EnvironmentPermissionSet(env.ID))
	c.Set(string(middleware.ContextKeyAuthMethod), "environment_access_token")
}

func extractBearerOrCookieTokenInternal(c *echo.Context) (string, bool) {
	req := c.Request()
	authHeader := req.Header.Get("Authorization")
	if after, ok := strings.CutPrefix(authHeader, "Bearer "); ok {
		return after, false
	}
	if tok, err := cookie.GetTokenCookie(req); err == nil && tok != "" {
		return tok, true
	}
	return "", false
}
