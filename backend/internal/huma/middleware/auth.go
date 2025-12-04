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

// NewAuthBridge creates a Huma middleware that validates JWT tokens and
// enforces security requirements defined on operations.
func NewAuthBridge(authService *services.AuthService, cfg *config.Config) func(ctx huma.Context, next func(huma.Context)) {
	return func(ctx huma.Context, next func(huma.Context)) {
		// Skip auth for nil service (spec generation)
		if authService == nil {
			next(ctx)
			return
		}

		// Check if this operation requires authentication
		isAuthRequired := false
		requiresBearerAuth := false
		requiresApiKeyAuth := false

		if ctx.Operation() != nil && len(ctx.Operation().Security) > 0 {
			for _, secReq := range ctx.Operation().Security {
				if _, ok := secReq["BearerAuth"]; ok {
					isAuthRequired = true
					requiresBearerAuth = true
				}
				if _, ok := secReq["ApiKeyAuth"]; ok {
					isAuthRequired = true
					requiresApiKeyAuth = true
				}
			}
		}

		// If no security requirements, allow the request through
		if !isAuthRequired {
			next(ctx)
			return
		}

		var user *models.User
		var authErr error

		// Try Bearer token authentication
		if requiresBearerAuth {
			token := extractBearerToken(ctx)
			if token != "" {
				user, authErr = authService.VerifyToken(ctx.Context(), token)
				if authErr == nil && user != nil {
					// Success - add user to context and continue
					newCtx := setUserInContext(ctx.Context(), user)
					ctx = huma.WithContext(ctx, newCtx)
					next(ctx)
					return
				}
			}
		}

		// Try API Key authentication (if Bearer failed or wasn't provided)
		if requiresApiKeyAuth && user == nil {
			apiKey := ctx.Header("X-API-Key")
			if apiKey != "" {
				// API key validation is handled by the ApiKeyService
				// For now, we'll let it through and let the handler validate
				// This could be enhanced to validate API keys here
				next(ctx)
				return
			}
		}

		// Authentication failed
		huma.WriteErr(nil, ctx, http.StatusUnauthorized, "Unauthorized: valid authentication required")
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
