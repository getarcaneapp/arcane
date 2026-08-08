package middleware

import (
	"context"

	"github.com/getarcaneapp/arcane/backend/v2/pkg/authz"
)

// ContextKey is a type for context keys used by Huma handlers.
type ContextKey string

const (
	// ContextKeyUserID is the context key for the authenticated user's ID.
	ContextKeyUserID ContextKey = "userID"
	// ContextKeyCurrentSessionID is the context key for the authenticated session ID.
	ContextKeyCurrentSessionID ContextKey = "currentSessionID"
	// ContextKeyUserPermissions is the context key for the caller's resolved
	// PermissionSet, attached by the auth bridge.
	ContextKeyUserPermissions ContextKey = "userPermissions"
	// ContextKeyRemoteAddr is the context key for the request remote address.
	ContextKeyRemoteAddr ContextKey = "remoteAddr"
	// ContextKeyCurrentUser is the Echo context key for the authenticated user.
	ContextKeyCurrentUser ContextKey = "currentUser"
	// ContextKeyAuthMethod is the Echo context key for the authentication method.
	ContextKeyAuthMethod ContextKey = "authMethod"
)

// GetUserIDFromContext retrieves the user ID from the context.
func GetUserIDFromContext(ctx context.Context) (string, bool) {
	userID, ok := ctx.Value(ContextKeyUserID).(string)
	return userID, ok
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
