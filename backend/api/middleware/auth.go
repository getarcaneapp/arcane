package middleware

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"strings"

	"github.com/danielgtaylor/huma/v2"
	"github.com/getarcaneapp/arcane/backend/v2/internal/common"
	"github.com/getarcaneapp/arcane/backend/v2/internal/config"
	"github.com/getarcaneapp/arcane/backend/v2/internal/models"
	"github.com/getarcaneapp/arcane/backend/v2/internal/services"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/authz"
	pkgutils "github.com/getarcaneapp/arcane/backend/v2/pkg/utils"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/utils/cookie"
)

// ContextKey is a type for context keys used by Huma handlers.
type ContextKey string

const (
	// ContextKeyUserID is the context key for the authenticated user's ID.
	ContextKeyUserID ContextKey = "userID"
	// ContextKeyCurrentUser is the context key for the authenticated user model.
	ContextKeyCurrentUser ContextKey = "currentUser"
	// ContextKeyCurrentSessionID is the context key for the authenticated session ID.
	ContextKeyCurrentSessionID ContextKey = "currentSessionID"
	// ContextKeyUserPermissions is the context key for the caller's resolved
	// PermissionSet, attached by the auth bridge.
	ContextKeyUserPermissions ContextKey = "userPermissions"
	// ContextKeyRemoteAddr is the context key for the request remote address.
	ContextKeyRemoteAddr ContextKey = "remoteAddr"
)

// GetUserIDFromContext retrieves the user ID from the context.
func GetUserIDFromContext(ctx context.Context) (string, bool) {
	userID, ok := ctx.Value(ContextKeyUserID).(string)
	return userID, ok
}

// GetCurrentUserFromContext retrieves the current user from the context.
func GetCurrentUserFromContext(ctx context.Context) (*models.User, bool) {
	u, ok := ctx.Value(ContextKeyCurrentUser).(*models.User)
	return u, ok
}

// GetCurrentSessionIDFromContext retrieves the current session ID from the context.
func GetCurrentSessionIDFromContext(ctx context.Context) (string, bool) {
	sessionID, ok := ctx.Value(ContextKeyCurrentSessionID).(string)
	return sessionID, ok
}

// PermissionsFromContext retrieves the caller's resolved PermissionSet.
// Returns nil, false on unauthenticated paths.
func PermissionsFromContext(ctx context.Context) (*authz.PermissionSet, bool) {
	ps, ok := ctx.Value(ContextKeyUserPermissions).(*authz.PermissionSet)
	return ps, ok
}

// GetRemoteAddrFromContext retrieves the request remote address from context.
func GetRemoteAddrFromContext(ctx context.Context) string {
	remoteAddr, _ := ctx.Value(ContextKeyRemoteAddr).(string)
	return remoteAddr
}

// securityRequirements holds parsed security requirements from an operation.
type securityRequirements struct {
	isRequired bool
	bearerAuth bool
	apiKeyAuth bool
}

type operationProvider interface {
	Operation() *huma.Operation
}

type environmentAccessTokenResolver interface {
	ResolveEnvironmentByAccessToken(ctx context.Context, token string) (*models.Environment, error)
}

// PermissionResolver resolves a caller's effective permission set. Implemented
// by services.RoleService; kept as an interface so tests can stub it.
type PermissionResolver interface {
	ResolvePermissions(ctx context.Context, user *models.User) (*authz.PermissionSet, error)
	ResolveApiKeyPermissions(ctx context.Context, apiKeyID string) (*authz.PermissionSet, error)
}

// parseSecurityRequirementsInternal extracts security requirements from a Huma operation.
func parseSecurityRequirementsInternal(api huma.API, ctx operationProvider) securityRequirements {
	reqs := securityRequirements{}
	if ctx.Operation() == nil {
		return reqs
	}

	security := ctx.Operation().Security
	if security == nil && api != nil && api.OpenAPI() != nil {
		security = api.OpenAPI().Security
	}
	if len(security) == 0 {
		return reqs
	}

	optional := false
	for _, secReq := range security {
		if len(secReq) == 0 {
			optional = true
			continue
		}
		if _, ok := secReq["BearerAuth"]; ok {
			reqs.bearerAuth = true
		}
		if _, ok := secReq["ApiKeyAuth"]; ok {
			reqs.apiKeyAuth = true
		}
	}

	reqs.isRequired = !optional && (reqs.bearerAuth || reqs.apiKeyAuth)

	return reqs
}

// tryBearerAuthInternal attempts Bearer token authentication. Returns the
// authenticated user on success, or the underlying error from VerifyToken so
// the caller can distinguish a missing/invalid token from a token-version
// mismatch (which requires clearing the stale cookie).
func tryBearerAuthInternal(ctx huma.Context, authService *services.AuthService) (*models.User, string, error) {
	token := extractBearerTokenInternal(ctx)
	if token == "" {
		return nil, "", nil
	}
	user, sessionID, err := authService.VerifyToken(ctx.Context(), token)
	if err != nil {
		return nil, "", err
	}
	return user, sessionID, nil
}

// tryApiKeyAuthInternal checks if API key authentication should be allowed
// through. Returns the resolved user plus the API key record so the caller
// can resolve permissions according to the key's kind.
func tryApiKeyAuthInternal(ctx huma.Context, apiKeyService *services.ApiKeyService) (*models.User, *models.ApiKey, bool) {
	apiKey := ctx.Header(pkgutils.HeaderApiKey)
	if apiKey == "" {
		return nil, nil, false
	}

	user, key, err := apiKeyService.ValidateApiKeyWithID(ctx.Context(), apiKey)
	if err != nil || user == nil {
		return nil, nil, false
	}

	return user, key, true
}

func tryEnvironmentAccessTokenAuthInternal(ctx huma.Context, resolver environmentAccessTokenResolver, token string) (*models.User, bool) {
	if resolver == nil || strings.TrimSpace(token) == "" {
		return nil, false
	}

	env, err := resolver.ResolveEnvironmentByAccessToken(ctx.Context(), token)
	if err != nil || env == nil {
		return nil, false
	}

	return createEnvironmentSudoUserInternal(env), true
}

// tryAgentAuthInternal checks if the request is from an authenticated agent.
// Returns a sudo agent user if the agent token is valid.
func tryAgentAuthInternal(ctx huma.Context, cfg *config.Config) (*models.User, bool) {
	if cfg == nil || !cfg.AgentMode {
		return nil, false
	}

	path := ctx.URL().Path

	// Check for agent bootstrap pairing
	if strings.HasPrefix(path, pkgutils.AgentPairingPrefix) &&
		cfg.AgentToken != "" &&
		ctx.Header(pkgutils.HeaderAgentBootstrap) == cfg.AgentToken {
		return createAgentSudoUserInternal(), true
	}

	// Check for agent token
	if tok := ctx.Header(pkgutils.HeaderAgentToken); tok != "" && cfg.AgentToken != "" && tok == cfg.AgentToken {
		return createAgentSudoUserInternal(), true
	}

	// Check for API key as agent token
	if tok := ctx.Header(pkgutils.HeaderApiKey); tok != "" && cfg.AgentToken != "" && tok == cfg.AgentToken {
		return createAgentSudoUserInternal(), true
	}

	return nil, false
}

// createAgentSudoUserInternal creates a sudo user for agent authentication.
// The PermissionSet attached to the context (via setUserInContextWithSudoInternal)
// bypasses every check; the user's Roles field is intentionally empty.
func createAgentSudoUserInternal() *models.User {
	return &models.User{
		BaseModel: models.BaseModel{ID: "agent"},
		Email:     new("agent@getarcane.app"),
		Username:  "agent",
	}
}

func createEnvironmentSudoUserInternal(env *models.Environment) *models.User {
	return &models.User{
		BaseModel: models.BaseModel{ID: "environment:" + env.ID},
		Username:  env.Name,
	}
}

// NewAuthBridge creates a Huma middleware that validates credentials and
// enforces security requirements defined on operations. It also resolves the
// caller's effective PermissionSet via permResolver and stashes it on the
// request context for downstream RequirePermission checks.
func NewAuthBridge(api huma.API, authService *services.AuthService, apiKeyService *services.ApiKeyService, permResolver PermissionResolver, envTokenResolver environmentAccessTokenResolver, cfg *config.Config) func(ctx huma.Context, next func(huma.Context)) {
	return func(ctx huma.Context, next func(huma.Context)) {
		ctx = huma.WithContext(ctx, context.WithValue(ctx.Context(), ContextKeyRemoteAddr, ctx.RemoteAddr()))
		if authService == nil {
			next(ctx)
			return
		}

		if newCtx, ok := tryAgentAuthCtxInternal(ctx, cfg); ok {
			next(newCtx)
			return
		}

		reqs := parseSecurityRequirementsInternal(api, ctx)
		if !reqs.isRequired {
			next(opportunisticBearerAuthInternal(ctx, authService, permResolver))
			return
		}

		if reqs.apiKeyAuth && ctx.Header(pkgutils.HeaderApiKey) != "" {
			handleApiKeyAuthInternal(api, ctx, apiKeyService, permResolver, envTokenResolver, next)
			return
		}

		if user, ok := tryEnvironmentAccessTokenAuthInternal(ctx, envTokenResolver, ctx.Header(pkgutils.HeaderAgentToken)); ok {
			newCtx := setUserInContextWithSudoInternal(ctx.Context(), user)
			next(huma.WithContext(ctx, newCtx))
			return
		}

		if reqs.bearerAuth {
			nextCtx, handled := handleBearerAuthInternal(api, ctx, authService, permResolver)
			if handled {
				if nextCtx != nil {
					next(nextCtx)
				}
				return
			}
		}

		_ = huma.WriteErr(api, ctx, http.StatusUnauthorized, "Unauthorized: valid authentication required")
	}
}

func tryAgentAuthCtxInternal(ctx huma.Context, cfg *config.Config) (huma.Context, bool) {
	if cfg == nil || !cfg.AgentMode {
		return ctx, false
	}
	user, ok := tryAgentAuthInternal(ctx, cfg)
	if !ok {
		return ctx, false
	}
	return huma.WithContext(ctx, setUserInContextWithSudoInternal(ctx.Context(), user)), true
}

// opportunisticBearerAuthInternal populates the user/session context if a valid
// bearer token is present, but never fails the request. Used for public routes
// (e.g. logout) that still need to know who the caller is when a token exists.
func opportunisticBearerAuthInternal(ctx huma.Context, authService *services.AuthService, permResolver PermissionResolver) huma.Context {
	if extractBearerTokenInternal(ctx) == "" {
		return ctx
	}
	user, sessionID, err := tryBearerAuthInternal(ctx, authService)
	if err != nil || user == nil {
		return ctx
	}
	newCtx := setUserInContextInternal(ctx.Context(), user, resolveUserPermissionsInternal(ctx.Context(), permResolver, user))
	newCtx = context.WithValue(newCtx, ContextKeyCurrentSessionID, sessionID)
	return huma.WithContext(ctx, newCtx)
}

// handleApiKeyAuthInternal handles the API-key-present branch. If validation
// fails, it writes 401 directly — Bearer is not attempted as fallback.
func handleApiKeyAuthInternal(api huma.API, ctx huma.Context, apiKeyService *services.ApiKeyService, permResolver PermissionResolver, envTokenResolver environmentAccessTokenResolver, next func(huma.Context)) {
	if user, key, ok := tryApiKeyAuthInternal(ctx, apiKeyService); ok {
		// Personal keys inherit the owner's role permissions (same resolution
		// as session auth); scoped keys are limited to their own grants.
		var ps *authz.PermissionSet
		if key.Kind == models.ApiKeyKindPersonal {
			ps = resolveUserPermissionsInternal(ctx.Context(), permResolver, user)
		} else {
			ps = resolveApiKeyPermissionsInternal(ctx.Context(), permResolver, key.ID)
		}
		newCtx := setUserInContextInternal(ctx.Context(), user, ps)
		next(huma.WithContext(ctx, newCtx))
		return
	}
	if user, ok := tryEnvironmentAccessTokenAuthInternal(ctx, envTokenResolver, ctx.Header(pkgutils.HeaderApiKey)); ok {
		newCtx := setUserInContextWithSudoInternal(ctx.Context(), user)
		next(huma.WithContext(ctx, newCtx))
		return
	}
	_ = huma.WriteErr(api, ctx, http.StatusUnauthorized, "Unauthorized: invalid API key")
}

func handleBearerAuthInternal(api huma.API, ctx huma.Context, authService *services.AuthService, permResolver PermissionResolver) (huma.Context, bool) {
	user, sessionID, err := tryBearerAuthInternal(ctx, authService)
	if err == nil && user != nil {
		ps := resolveUserPermissionsInternal(ctx.Context(), permResolver, user)
		newCtx := setUserInContextInternal(ctx.Context(), user, ps)
		newCtx = context.WithValue(newCtx, ContextKeyCurrentSessionID, sessionID)
		return huma.WithContext(ctx, newCtx), true
	}
	if errors.Is(err, services.ErrTokenVersionMismatch) {
		// The app version changed (a self-update). The session is still valid — the
		// refresh path tolerates the version change and rotates the token — so do NOT
		// clear the auth cookies. Return a recoverable 401 the frontend refreshes from.
		_ = huma.WriteErr(api, ctx, http.StatusUnauthorized, "Application has been updated. Refreshing session.")
		return nil, true
	}
	if common.IsSessionRevokedError(err) || common.IsTokenValidationError(err) {
		for _, cookieHeader := range cookie.BuildClearTokenCookieStringsFor(cookie.SecureCookieFromContext(ctx.Context())) {
			ctx.AppendHeader("Set-Cookie", cookieHeader)
		}
		_ = huma.WriteErr(api, ctx, http.StatusUnauthorized, "Session expired. Please log in again.")
		return nil, true
	}
	return nil, false
}

// resolveUserPermissionsInternal asks the RoleService for the user's resolved
// PermissionSet. If RoleService is unavailable or the lookup fails (boot-time
// edge cases, broken DB) it returns nil and logs a warning — handlers then
// see deny-all, which is the safe default.
func resolveUserPermissionsInternal(ctx context.Context, permResolver PermissionResolver, user *models.User) *authz.PermissionSet {
	if permResolver == nil || user == nil {
		return nil
	}
	ps, err := permResolver.ResolvePermissions(ctx, user)
	if err != nil {
		slog.WarnContext(ctx, "failed to resolve user permissions", "error", err, "user_id", user.ID)
		return nil
	}
	return ps
}

func resolveApiKeyPermissionsInternal(ctx context.Context, permResolver PermissionResolver, apiKeyID string) *authz.PermissionSet {
	if permResolver == nil || apiKeyID == "" {
		return nil
	}
	ps, err := permResolver.ResolveApiKeyPermissions(ctx, apiKeyID)
	if err != nil {
		slog.WarnContext(ctx, "failed to resolve api key permissions", "error", err, "api_key_id", apiKeyID)
		return nil
	}
	return ps
}

// extractBearerTokenInternal extracts the JWT token from Authorization header or cookie.
func extractBearerTokenInternal(ctx huma.Context) string {
	// Try Authorization header first
	authHeader := ctx.Header("Authorization")
	if len(authHeader) > 7 && strings.ToLower(authHeader[:7]) == "bearer " {
		return authHeader[7:]
	}

	// Try cookie as fallback
	cookieHeader := ctx.Header("Cookie")
	if cookieHeader != "" {
		return extractTokenFromCookieHeaderInternal(cookieHeader)
	}

	return ""
}

// extractTokenFromCookieHeaderInternal parses the token cookie from a Cookie header string.
func extractTokenFromCookieHeaderInternal(cookieHeader string) string {
	cookies := strings.SplitSeq(cookieHeader, ";")
	for c := range cookies {
		c = strings.TrimSpace(c)
		if after, ok := strings.CutPrefix(c, "token="); ok {
			return after
		}
		if after, ok := strings.CutPrefix(c, "__Host-token="); ok {
			return after
		}
	}
	return ""
}

// setUserInContextInternal adds the authenticated user and the resolved
// PermissionSet to the context. Callers must supply a non-nil PermissionSet;
// pass authz.NewPermissionSet() to express deny-all.
func setUserInContextInternal(ctx context.Context, user *models.User, ps *authz.PermissionSet) context.Context {
	if ps == nil {
		ps = authz.NewPermissionSet()
	}
	ctx = context.WithValue(ctx, ContextKeyUserID, user.ID)
	ctx = context.WithValue(ctx, ContextKeyCurrentUser, user)
	ctx = context.WithValue(ctx, ContextKeyUserPermissions, ps)
	return ctx
}

// setUserInContextWithSudoInternal attaches a sudo PermissionSet (bypasses
// every check) plus the user. Used by the agent token and environment
// access token paths, which are infrastructure-level and not per-user.
func setUserInContextWithSudoInternal(ctx context.Context, user *models.User) context.Context {
	return setUserInContextInternal(ctx, user, authz.SudoPermissionSet())
}
