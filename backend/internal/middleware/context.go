package middleware

import (
	"context"
	"strings"

	"github.com/danielgtaylor/huma/v2"

	activitylib "getarcane.dev/arcane/v3/pkg/activity"
	"getarcane.dev/arcane/v3/pkg/authz"
)

type (
	ContextKey string
)

const (
	HeaderAgentToken                            = "X-Arcane-Agent-Token" // #nosec G101: header name, not a credential
	HeaderApiKey                                = "X-Api-Key"            // #nosec G101: header name, not a credential
	HeaderActivityBatchID                       = "X-Arcane-Batch-Id"
	HeaderUpdateInitiatorID                     = "X-Arcane-Update-Initiator-Id"
	HeaderUpdateInitiatorName                   = "X-Arcane-Update-Initiator-Name"
	HeaderUpdateInitiatorDisplayName            = "X-Arcane-Update-Initiator-Display-Name"
	HeaderForwardedPermissions                  = "X-Arcane-Forwarded-Permissions"
	HeaderIconCatalog                           = "X-Arcane-Icon-Catalog"
	ContextKeyApiKeyID               ContextKey = "apiKeyID"
	ContextKeyUserID                 ContextKey = "userID"
	ContextKeyCurrentSessionID       ContextKey = "currentSessionID"
	ContextKeyUserPermissions        ContextKey = "userPermissions"
	ContextKeyRemoteAddr             ContextKey = "remoteAddr"
	ContextKeyCurrentUser            ContextKey = "currentUser"
	ContextKeyAuthMethod             ContextKey = "authMethod"
)

func GetUserIDFromContext(ctx context.Context) (string, bool) {
	userID, ok := ctx.Value(ContextKeyUserID).(string)
	return userID, ok
}

func GetCurrentSessionIDFromContext(ctx context.Context) (string, bool) {
	sessionID, ok := ctx.Value(ContextKeyCurrentSessionID).(string)
	return sessionID, ok
}

// Returns nil, false on unauthenticated paths.
func PermissionsFromContext(ctx context.Context) (*authz.PermissionSet, bool) {
	ps, ok := ctx.Value(ContextKeyUserPermissions).(*authz.PermissionSet)
	return ps, ok
}

func GetRemoteAddrFromContext(ctx context.Context) string {
	remoteAddr, _ := ctx.Value(ContextKeyRemoteAddr).(string)
	return remoteAddr
}

func NewActivityBatchID() func(ctx huma.Context, next func(huma.Context)) {
	return func(ctx huma.Context, next func(huma.Context)) {
		if batchID := strings.TrimSpace(ctx.Header(HeaderActivityBatchID)); batchID != "" {
			ctx = huma.WithContext(ctx, activitylib.WithActivityBatchID(ctx.Context(), batchID))
		}
		next(ctx)
	}
}
