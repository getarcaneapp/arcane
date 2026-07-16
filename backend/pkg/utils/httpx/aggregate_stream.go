package httpx

import (
	"context"

	"github.com/getarcaneapp/arcane/backend/v2/pkg/authz"
	"go.getarcane.app/streams/agg"
)

// RunAuthorizedAggregateStream selects local and remote producers from the
// caller's effective permissions before starting an aggregate stream. Remote
// producers must still filter individual environments with PermissionSet.Allows.
func RunAuthorizedAggregateStream[T any](
	ctx context.Context,
	ps *authz.PermissionSet,
	config agg.Config[T],
	localProducer agg.Producer[T],
	remoteProducer agg.Producer[T],
	permissions ...string,
) error {
	config.Producers = make([]agg.Producer[T], 0, 2)
	localAllowed := false
	remoteAllowed := false
	for _, permission := range permissions {
		scopeEnvironmentID := ""
		if authz.IsEnvScoped(permission) {
			scopeEnvironmentID = "0"
		}
		if ps.Allows(permission, scopeEnvironmentID) {
			localAllowed = true
		}
		if authz.IsEnvScoped(permission) && ps.AllowsAny(permission) {
			remoteAllowed = true
		}
	}
	if localAllowed {
		config.Producers = append(config.Producers, localProducer)
	}
	if remoteAllowed {
		config.Producers = append(config.Producers, remoteProducer)
	}
	return agg.Run(ctx, config)
}
