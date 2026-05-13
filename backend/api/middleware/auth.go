package middleware

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"github.com/danielgtaylor/huma/v2"
	"github.com/getarcaneapp/arcane/backend/internal/common"
	"github.com/getarcaneapp/arcane/backend/internal/config"
	"github.com/getarcaneapp/arcane/backend/internal/models"
	"github.com/getarcaneapp/arcane/backend/internal/services"
	pkgutils "github.com/getarcaneapp/arcane/backend/pkg/utils"
	"github.com/getarcaneapp/arcane/backend/pkg/utils/cookie"
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
	// ContextKeyUserIsAdmin is the context key for whether the user is an admin.
	ContextKeyUserIsAdmin ContextKey = "userIsAdmin"
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

// IsAdminFromContext checks if the current user is an admin.
func IsAdminFromContext(ctx context.Context) bool {
	isAdmin, ok := ctx.Value(ContextKeyUserIsAdmin).(bool)
	return ok && isAdmin
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

// tryApiKeyAuthInternal checks if API key authentication should be allowed through.
func tryApiKeyAuthInternal(ctx huma.Context, apiKeyService *services.ApiKeyService) (*models.User, bool) {
	apiKey := ctx.Header(pkgutils.HeaderApiKey)
	if apiKey == "" {
		return nil, false
	}

	user, err := apiKeyService.ValidateApiKey(ctx.Context(), apiKey)
	if err != nil || user == nil {
		return nil, false
	}

	return user, true
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
func createAgentSudoUserInternal() *models.User {
	return &models.User{
		BaseModel: models.BaseModel{ID: "agent"},
		Email:     new("agent@getarcane.app"),
		Username:  "agent",
		Roles:     []string{"admin"},
	}
}

func createEnvironmentSudoUserInternal(env *models.Environment) *models.User {
	return &models.User{
		BaseModel: models.BaseModel{ID: "environment:" + env.ID},
		Username:  env.Name,
		Roles:     []string{"admin"},
	}
}

// NewAuthBridge creates a Huma middleware that validates JWT tokens and
// enforces security requirements defined on operations.
func NewAuthBridge(api huma.API, authService *services.AuthService, apiKeyService *services.ApiKeyService, envTokenResolver environmentAccessTokenResolver, cfg *config.Config) func(ctx huma.Context, next func(huma.Context)) {
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
			next(opportunisticBearerAuthInternal(ctx, authService))
			return
		}

		if reqs.apiKeyAuth && ctx.Header(pkgutils.HeaderApiKey) != "" {
			handleApiKeyAuthInternal(api, ctx, apiKeyService, envTokenResolver, next)
			return
		}

		if user, ok := tryEnvironmentAccessTokenAuthInternal(ctx, envTokenResolver, ctx.Header(pkgutils.HeaderAgentToken)); ok {
			newCtx := setUserInContextInternal(ctx.Context(), user)
			next(huma.WithContext(ctx, newCtx))
			return
		}

		if reqs.bearerAuth {
			nextCtx, handled := handleBearerAuthInternal(api, ctx, authService)
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
	return huma.WithContext(ctx, setUserInContextInternal(ctx.Context(), user)), true
}

// opportunisticBearerAuthInternal populates the user/session context if a valid
// bearer token is present, but never fails the request. Used for public routes
// (e.g. logout) that still need to know who the caller is when a token exists.
func opportunisticBearerAuthInternal(ctx huma.Context, authService *services.AuthService) huma.Context {
	if extractBearerTokenInternal(ctx) == "" {
		return ctx
	}
	user, sessionID, err := tryBearerAuthInternal(ctx, authService)
	if err != nil || user == nil {
		return ctx
	}
	newCtx := setUserInContextInternal(ctx.Context(), user)
	newCtx = context.WithValue(newCtx, ContextKeyCurrentSessionID, sessionID)
	return huma.WithContext(ctx, newCtx)
}

// handleApiKeyAuthInternal handles the API-key-present branch. If validation
// fails, it writes 401 directly — Bearer is not attempted as fallback.
func handleApiKeyAuthInternal(api huma.API, ctx huma.Context, apiKeyService *services.ApiKeyService, envTokenResolver environmentAccessTokenResolver, next func(huma.Context)) {
	if user, ok := tryApiKeyAuthInternal(ctx, apiKeyService); ok {
		newCtx := setUserInContextInternal(ctx.Context(), user)
		next(huma.WithContext(ctx, newCtx))
		return
	}
	if user, ok := tryEnvironmentAccessTokenAuthInternal(ctx, envTokenResolver, ctx.Header(pkgutils.HeaderApiKey)); ok {
		newCtx := setUserInContextInternal(ctx.Context(), user)
		next(huma.WithContext(ctx, newCtx))
		return
	}
	_ = huma.WriteErr(api, ctx, http.StatusUnauthorized, "Unauthorized: invalid API key")
}

func handleBearerAuthInternal(api huma.API, ctx huma.Context, authService *services.AuthService) (huma.Context, bool) {
	user, sessionID, err := tryBearerAuthInternal(ctx, authService)
	if err == nil && user != nil {
		newCtx := setUserInContextInternal(ctx.Context(), user)
		newCtx = context.WithValue(newCtx, ContextKeyCurrentSessionID, sessionID)
		return huma.WithContext(ctx, newCtx), true
	}
	if errors.Is(err, services.ErrTokenVersionMismatch) || common.IsSessionRevokedError(err) || common.IsTokenValidationError(err) {
		ctx.AppendHeader("Set-Cookie", cookie.BuildClearTokenCookieStringFor(ctx.TLS() != nil))
		_ = huma.WriteErr(api, ctx, http.StatusUnauthorized, "Session expired. Please log in again.")
		return nil, true
	}
	return nil, false
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

// setUserInContextInternal adds the authenticated user to the context.
func setUserInContextInternal(ctx context.Context, user *models.User) context.Context {
	ctx = context.WithValue(ctx, ContextKeyUserID, user.ID)
	ctx = context.WithValue(ctx, ContextKeyCurrentUser, user)
	ctx = context.WithValue(ctx, ContextKeyUserIsAdmin, pkgutils.UserHasRole(user.Roles, "admin"))
	return ctx
}
