package middleware

import (
	"context"
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

// NewAuthBridge creates a Huma middleware that extracts authentication
// from the request and sets it in the Go context for Huma handlers.
func NewAuthBridge(authService *services.AuthService, cfg *config.Config) func(ctx huma.Context, next func(huma.Context)) {
	return func(ctx huma.Context, next func(huma.Context)) {
		// Skip auth for nil service (spec generation)
		if authService == nil {
			next(ctx)
			return
		}

		// Extract token from Authorization header or cookie
		token := ""
		authHeader := ctx.Header("Authorization")
		if len(authHeader) > 7 && authHeader[:7] == "Bearer " {
			token = authHeader[7:]
		}
		if token == "" {
			// Try cookie - parse from the Cookie header
			cookieHeader := ctx.Header("Cookie")
			if cookieHeader != "" {
				token = extractTokenFromCookieHeader(cookieHeader)
			}
		}

		if token != "" {
			// Validate token and get user
			user, err := authService.VerifyToken(ctx.Context(), token)
			if err == nil && user != nil {
				// Add user info to context
				newCtx := context.WithValue(ctx.Context(), ContextKeyUserID, user.ID)
				newCtx = context.WithValue(newCtx, ContextKeyCurrentUser, user)
				newCtx = context.WithValue(newCtx, ContextKeyUserIsAdmin, userHasRole(user, "admin"))
				ctx = huma.WithContext(ctx, newCtx)
			}
		}

		next(ctx)
	}
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

func userHasRole(user *models.User, role string) bool {
	for _, r := range user.Roles {
		if r == role {
			return true
		}
	}
	return false
}
