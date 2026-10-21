package userctx

import (
	"context"

	"github.com/getarcaneapp/arcane/types/v3"
)

// CurrentUserFromContext retrieves the actor for an authenticated request.
func CurrentUserFromContext(ctx context.Context) (*types.Actor, bool) {
	value := ctx.Value(types.CurrentUserContextKey{})
	if actor, ok := value.(*types.Actor); ok {
		return actor, true
	}
	provider, ok := value.(types.ActorProvider)
	if !ok {
		return nil, false
	}
	return provider.Actor(), true
}
