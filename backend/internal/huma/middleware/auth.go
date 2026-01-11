package middleware

import (
	"context"
	"net/http"
	"strings"

	"github.com/danielgtaylor/huma/v2"
	"github.com/getarcaneapp/arcane/backend/internal/config"
	"github.com/getarcaneapp/arcane/backend/internal/models"
	"github.com/getarcaneapp/arcane/backend/internal/services"
)

const (
	headerAgentBootstrap = "X-Arcane-Agent-Bootstrap"
	headerAgentToken     = "X-Arcane-Agent-Token" // #nosec G101: header name, not a credential
	headerApiKey         = "X-API-Key"            // #nosec G101: header name, not a credential
	agentPairingPrefix   = "/api/environments/0/agent/pair"
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

// securityRequirements holds parsed security requirements from an operation.
type securityRequirements struct {
	isRequired bool
	bearerAuth bool
	apiKeyAuth bool
}

type operationProvider interface {
	Operation() *huma.Operation
}

// parseSecurityRequirements extracts security requirements from a Huma operation.
func parseSecurityRequirements(ctx operationProvider) securityRequirements {
	reqs := securityRequirements{}
	if ctx.Operation() == nil || len(ctx.Operation().Security) == 0 {
		return reqs
	}
	for _, secReq := range ctx.Operation().Security {
		if _, ok := secReq["BearerAuth"]; ok {
			reqs.isRequired = true
			reqs.bearerAuth = true
		}
		if _, ok := secReq["ApiKeyAuth"]; ok {
			reqs.isRequired = true
			reqs.apiKeyAuth = true
		}
	}
	return reqs
}

// tryBearerAuth attempts Bearer token authentication.
func tryBearerAuth(ctx huma.Context, authService *services.AuthService) (*models.User, bool) {
	token := extractBearerToken(ctx)
	if token == "" {
		return nil, false
	}
	user, err := authService.VerifyToken(ctx.Context(), token)
	if err != nil || user == nil {
		return nil, false
	}
	return user, true
}

// tryApiKeyAuth checks if API key authentication should be allowed through.
func tryApiKeyAuth(ctx huma.Context, apiKeyService *services.ApiKeyService) (*models.User, bool) {
	apiKey := ctx.Header(headerApiKey)
	if apiKey == "" {
		return nil, false
	}

	user, err := apiKeyService.ValidateApiKey(ctx.Context(), apiKey)
	if err != nil || user == nil {
		return nil, false
	}

	return user, true
}

// tryAgentAuth checks if the request is from an authenticated agent.
// Returns a sudo agent user if the agent token is valid.
func tryAgentAuth(ctx huma.Context, cfg *config.Config) (*models.User, bool) {
	if cfg == nil || !cfg.AgentMode {
		return nil, false
	}

	path := ctx.URL().Path

	// Check for agent bootstrap pairing
	if strings.HasPrefix(path, agentPairingPrefix) &&
		cfg.AgentToken != "" &&
		ctx.Header(headerAgentBootstrap) == cfg.AgentToken {
		return createAgentSudoUser(), true
	}

	// Check for agent token
	if tok := ctx.Header(headerAgentToken); tok != "" && cfg.AgentToken != "" && tok == cfg.AgentToken {
		return createAgentSudoUser(), true
	}

	// Check for API key as agent token
	if tok := ctx.Header(headerApiKey); tok != "" && cfg.AgentToken != "" && tok == cfg.AgentToken {
		return createAgentSudoUser(), true
	}

	return nil, false
}

// createAgentSudoUser creates a sudo user for agent authentication.
func createAgentSudoUser() *models.User {
	email := "agent@getarcane.app"
	return &models.User{
		BaseModel: models.BaseModel{ID: "agent"},
		Email:     &email,
		Roles:     []string{"admin"},
	}
}

// NewAuthBridge creates a Huma middleware that validates JWT tokens and
// enforces security requirements defined on operations.
func NewAuthBridge(api huma.API, authService *services.AuthService, apiKeyService *services.ApiKeyService, cfg *config.Config) func(ctx huma.Context, next func(huma.Context)) {
	return func(ctx huma.Context, next func(huma.Context)) {
		if authService == nil {
			next(ctx)
			return
		}

		// Check agent authentication first (if in agent mode)
		if cfg != nil && cfg.AgentMode {
			if user, ok := tryAgentAuth(ctx, cfg); ok {
				newCtx := setUserInContext(ctx.Context(), user)
				ctx = huma.WithContext(ctx, newCtx)
				next(ctx)
				return
			}
		}

		reqs := parseSecurityRequirements(ctx)
		if !reqs.isRequired {
			next(ctx)
			return
		}

		// If API key header is present and API key auth is allowed, prioritize it.
		// If validation fails, do NOT fall back to Bearer auth.
		if reqs.apiKeyAuth && ctx.Header(headerApiKey) != "" {
			if user, ok := tryApiKeyAuth(ctx, apiKeyService); ok {
				newCtx := setUserInContext(ctx.Context(), user)
				ctx = huma.WithContext(ctx, newCtx)
				next(ctx)
				return
			}
			// API key was present but invalid. Fail immediately.
			_ = huma.WriteErr(api, ctx, http.StatusUnauthorized, "Unauthorized: invalid API key")
			return
		}

		if reqs.bearerAuth {
			if user, ok := tryBearerAuth(ctx, authService); ok {
				newCtx := setUserInContext(ctx.Context(), user)
				ctx = huma.WithContext(ctx, newCtx)
				next(ctx)
				return
			}
		}

		// Write unauthorized response directly
		_ = huma.WriteErr(api, ctx, http.StatusUnauthorized, "Unauthorized: valid authentication required")
	}
}

// extractBearerToken extracts the JWT token from Authorization header or cookie.
func extractBearerToken(ctx huma.Context) string {
	// Try Authorization header first
	authHeader := ctx.Header("Authorization")
	if len(authHeader) > 7 && strings.ToLower(authHeader[:7]) == "bearer " {
		return authHeader[7:]
	}

	// Try cookie as fallback
	cookieHeader := ctx.Header("Cookie")
	if cookieHeader != "" {
		return extractTokenFromCookieHeader(cookieHeader)
	}

	return ""
}

// extractTokenFromCookieHeader parses the token cookie from a Cookie header string.
func extractTokenFromCookieHeader(cookieHeader string) string {
	cookies := strings.Split(cookieHeader, ";")
	for _, c := range cookies {
		c = strings.TrimSpace(c)
		if strings.HasPrefix(c, "token=") {
			return strings.TrimPrefix(c, "token=")
		}
		if strings.HasPrefix(c, "__Host-token=") {
			return strings.TrimPrefix(c, "__Host-token=")
		}
	}
	return ""
}

// setUserInContext adds the authenticated user to the context.
func setUserInContext(ctx context.Context, user *models.User) context.Context {
	ctx = context.WithValue(ctx, ContextKeyUserID, user.ID)
	ctx = context.WithValue(ctx, ContextKeyCurrentUser, user)
	ctx = context.WithValue(ctx, ContextKeyUserIsAdmin, userHasRole(user, "admin"))
	return ctx
}

func userHasRole(user *models.User, role string) bool {
	for _, r := range user.Roles {
		if r == role {
			return true
		}
	}
	return false
}
