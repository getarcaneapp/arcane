package middleware

import (
	"context"
	"crypto/subtle"
	"errors"
	"log/slog"
	"net/http"
	"strings"

	"github.com/getarcaneapp/arcane/backend/v2/internal/common"
	"github.com/getarcaneapp/arcane/backend/v2/internal/config"
	"github.com/getarcaneapp/arcane/backend/v2/internal/models"
	"github.com/getarcaneapp/arcane/backend/v2/internal/services"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/authz"
	pkgutils "github.com/getarcaneapp/arcane/backend/v2/pkg/utils"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/utils/cookie"
	"github.com/labstack/echo/v4"
)

// Echo context keys, kept aligned with api/middleware/auth.go constants so
// shared handlers can read them regardless of which auth layer attached them.
const (
	echoCtxKeyUserID          = "userID"
	echoCtxKeyCurrentUser     = "currentUser"
	echoCtxKeyUserPermissions = "userPermissions"
	echoCtxKeyAuthMethod      = "authMethod"
	echoCtxKeySessionID       = "currentSessionID"
)

type AuthOptions struct {
	AdminRequired   bool
	SuccessOptional bool
}

type ApiKeyValidator interface {
	ValidateApiKeyWithID(ctx context.Context, rawKey string) (*models.User, string, error)
}

type EnvironmentAccessTokenResolver interface {
	ResolveEnvironmentByAccessToken(ctx context.Context, token string) (*models.Environment, error)
}

// PermissionResolver resolves a caller's effective permission set. Implemented
// by services.RoleService; kept as an interface so tests can stub it.
type PermissionResolver interface {
	ResolvePermissions(ctx context.Context, user *models.User) (*authz.PermissionSet, error)
	ResolveApiKeyPermissions(ctx context.Context, apiKeyID string) (*authz.PermissionSet, error)
}

type AuthMiddleware struct {
	authService      *services.AuthService
	apiKeyValidator  ApiKeyValidator
	envTokenResolver EnvironmentAccessTokenResolver
	roleResolver     PermissionResolver
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

// RequirePermission returns an Echo middleware that rejects callers lacking
// `perm` for the environment ID in the request path (or globally for org-level
// permissions). Use on streaming/WS routes that aren't served by Huma. Expects
// the caller's PermissionSet to already be on the Echo context via the
// AuthMiddleware (i.e., chain it AFTER auth).
func RequirePermission(perm string) echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			ps, _ := c.Get(echoCtxKeyUserPermissions).(*authz.PermissionSet)
			envID := ""
			if authz.IsEnvScoped(perm) {
				envID = authz.EnvIDFromPath(c.Request().URL.Path)
			}
			if !ps.Allows(perm, envID) {
				return c.JSON(http.StatusForbidden, models.APIError{
					Code:    "FORBIDDEN",
					Message: "permission denied: " + perm,
				})
			}
			return next(c)
		}
	}
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
		agentTokenMatchesInternal(req.Header.Get(pkgutils.HeaderAgentBootstrap), m.cfg.AgentToken) {
		slog.InfoContext(ctx, "Agent auth: bootstrap pairing accepted", "path", req.URL.Path, "method", req.Method)
		agentSudoInternal(c)
		return next(c)
	}

	if agentTokenMatchesInternal(req.Header.Get(pkgutils.HeaderAgentToken), m.cfg.AgentToken) {
		agentSudoInternal(c)
		return next(c)
	}

	// Check for API key as agent token
	if agentTokenMatchesInternal(req.Header.Get(pkgutils.HeaderApiKey), m.cfg.AgentToken) {
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

// agentTokenMatchesInternal compares a presented token against the configured
// agent token in constant time to avoid timing side channels.
func agentTokenMatchesInternal(presented, configured string) bool {
	if presented == "" || configured == "" {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(presented), []byte(configured)) == 1
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
			user, keyID, err := m.apiKeyValidator.ValidateApiKeyWithID(ctx, apiKey)
			if err == nil && user != nil {
				ps := m.resolveApiKeyPermissionsOrDeny(ctx, keyID)
				if m.options.AdminRequired && !ps.IsGlobalAdmin() {
					return c.JSON(http.StatusForbidden, models.APIError{
						Code:    "FORBIDDEN",
						Message: "You don't have permission to access this resource",
					})
				}
				c.Set(echoCtxKeyUserID, user.ID)
				c.Set(echoCtxKeyCurrentUser, user)
				c.Set(echoCtxKeyUserPermissions, ps)
				c.Set(echoCtxKeyAuthMethod, "api_key")
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

	ps := m.resolvePermissionsOrDeny(ctx, user)
	if m.options.AdminRequired && !ps.IsGlobalAdmin() {
		return c.JSON(http.StatusForbidden, models.APIError{
			Code:    "FORBIDDEN",
			Message: "You don't have permission to access this resource",
		})
	}

	c.Set(echoCtxKeyUserID, user.ID)
	c.Set(echoCtxKeyCurrentUser, user)
	c.Set(echoCtxKeySessionID, sessionID)
	c.Set(echoCtxKeyUserPermissions, ps)
	return next(c)
}

// resolvePermissionsOrDeny returns the user's permission set, or an empty
// (deny-all) set if the resolver is unavailable or fails. Resolver failures
// are logged.
func (m *AuthMiddleware) resolvePermissionsOrDeny(ctx context.Context, user *models.User) *authz.PermissionSet {
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
	}
	c.Set(echoCtxKeyUserID, agentUser.ID)
	c.Set(echoCtxKeyCurrentUser, agentUser)
	c.Set(echoCtxKeyUserPermissions, authz.SudoPermissionSet())
	c.Set(echoCtxKeyAuthMethod, "agent_token")
}

func environmentSudoInternal(c echo.Context, env *models.Environment) {
	envUser := &models.User{
		BaseModel: models.BaseModel{ID: "environment:" + env.ID},
		Username:  env.Name,
	}
	c.Set(echoCtxKeyUserID, envUser.ID)
	c.Set(echoCtxKeyCurrentUser, envUser)
	c.Set(echoCtxKeyUserPermissions, authz.SudoPermissionSet())
	c.Set(echoCtxKeyAuthMethod, "environment_access_token")
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
