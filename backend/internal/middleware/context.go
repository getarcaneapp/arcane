package middleware

import (
	"context"
	"strings"

	"github.com/danielgtaylor/huma/v2"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/authz"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/utils"
)

const (
	// Auth header names and path prefixes shared between the Echo middleware
	// (WebSocket/diagnostics) and the Huma auth bridge (REST). Keep these in one
	// place so a change to a header name applies to every route type at once.

	HeaderAgentBootstrap             = "X-Arcane-Agent-Bootstrap"
	HeaderAgentToken                 = "X-Arcane-Agent-Token" // #nosec G101: header name, not a credential
	HeaderApiKey                     = "X-Api-Key"            // #nosec G101: header name, not a credential
	HeaderActivityBatchID            = "X-Arcane-Batch-Id"
	HeaderUpdateInitiatorID          = "X-Arcane-Update-Initiator-Id"
	HeaderUpdateInitiatorName        = "X-Arcane-Update-Initiator-Name"
	HeaderUpdateInitiatorDisplayName = "X-Arcane-Update-Initiator-Display-Name"
	// HeaderIconCatalog carries the requesting user's icon catalog preference to
	// remote environments. Agents authenticate proxied calls as a synthetic user
	// with no preferences, so without it every remote environment would resolve
	// container/project icons against the default catalog.
	HeaderIconCatalog  = "X-Arcane-Icon-Catalog"
	AgentPairingPrefix = "/api/environments/0/agent/pair"

	ContextKeyApiKeyID ContextKey = "apiKeyID"
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

// ContextKey is a type for context keys used by Huma handlers.
type ContextKey string

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

// NewActivityBatchID lifts the client-supplied activity batch ID header into the
// request context so activities spawned by one logical bulk action can be
// grouped without threading the ID through every handler. Invalid values are
// ignored by utils.WithActivityBatchID.
func NewActivityBatchID() func(ctx huma.Context, next func(huma.Context)) {
	return func(ctx huma.Context, next func(huma.Context)) {
		if batchID := strings.TrimSpace(ctx.Header(HeaderActivityBatchID)); batchID != "" {
			ctx = huma.WithContext(ctx, utils.WithActivityBatchID(ctx.Context(), batchID))
		}
		next(ctx)
	}
}
