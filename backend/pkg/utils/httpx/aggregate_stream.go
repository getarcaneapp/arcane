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
	permission string,
	config agg.Config[T],
	localProducer agg.Producer[T],
	remoteProducer agg.Producer[T],
) error {
	config.Producers = make([]agg.Producer[T], 0, 2)
	if ps.Allows(permission, "0") {
		config.Producers = append(config.Producers, localProducer)
	}
	if ps.AllowsAny(permission) {
		config.Producers = append(config.Producers, remoteProducer)
	}
	return agg.Run(ctx, config)
}
