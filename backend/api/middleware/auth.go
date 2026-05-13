package middleware

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"github.com/danielgtaylor/huma/v2"
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
func tryBearerAuthInternal(ctx huma.Context, authService *services.AuthService) (*models.User, error) {
	token := extractBearerTokenInternal(ctx)
	if token == "" {
		return nil, nil
	}
	user, err := authService.VerifyToken(ctx.Context(), token)
	if err != nil {
		return nil, err
	}
	return user, nil
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

		// Check agent authentication first (if in agent mode)
		if cfg != nil && cfg.AgentMode {
			if user, ok := tryAgentAuthInternal(ctx, cfg); ok {
				newCtx := setUserInContextInternal(ctx.Context(), user)
				ctx = huma.WithContext(ctx, newCtx)
				next(ctx)
				return
			}
		}

		reqs := parseSecurityRequirementsInternal(api, ctx)
		if !reqs.isRequired {
			next(ctx)
			return
		}

		// If API key header is present and API key auth is allowed, prioritize it.
		// If validation fails, do NOT fall back to Bearer auth.
		if reqs.apiKeyAuth && ctx.Header(pkgutils.HeaderApiKey) != "" {
			if user, ok := tryApiKeyAuthInternal(ctx, apiKeyService); ok {
				newCtx := setUserInContextInternal(ctx.Context(), user)
				ctx = huma.WithContext(ctx, newCtx)
				next(ctx)
				return
			}
			if user, ok := tryEnvironmentAccessTokenAuthInternal(ctx, envTokenResolver, ctx.Header(pkgutils.HeaderApiKey)); ok {
				newCtx := setUserInContextInternal(ctx.Context(), user)
				ctx = huma.WithContext(ctx, newCtx)
				next(ctx)
				return
			}
			// API key was present but invalid. Fail immediately.
			_ = huma.WriteErr(api, ctx, http.StatusUnauthorized, "Unauthorized: invalid API key")
			return
		}

		if user, ok := tryEnvironmentAccessTokenAuthInternal(ctx, envTokenResolver, ctx.Header(pkgutils.HeaderAgentToken)); ok {
			newCtx := setUserInContextInternal(ctx.Context(), user)
			ctx = huma.WithContext(ctx, newCtx)
			next(ctx)
			return
		}

		if reqs.bearerAuth {
			user, err := tryBearerAuthInternal(ctx, authService)
			if err == nil && user != nil {
				newCtx := setUserInContextInternal(ctx.Context(), user)
				ctx = huma.WithContext(ctx, newCtx)
				next(ctx)
				return
			}
			if errors.Is(err, services.ErrTokenVersionMismatch) {
				ctx.AppendHeader("Set-Cookie", cookie.BuildClearTokenCookieStringFor(ctx.TLS() != nil))
				_ = huma.WriteErr(api, ctx, http.StatusUnauthorized, "Application has been updated. Please log in again.")
				return
			}
		}

		// Write unauthorized response directly
		_ = huma.WriteErr(api, ctx, http.StatusUnauthorized, "Unauthorized: valid authentication required")
	}
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
