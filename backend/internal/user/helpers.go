package user

import (
	"context"

	"github.com/getarcaneapp/arcane/types/v3"
)

// CurrentUserFromContext retrieves the authenticated persistence model.
func CurrentUserFromContext(ctx context.Context) (*User, bool) {
	u, ok := ctx.Value(types.CurrentUserContextKey{}).(*User)
	return u, ok
}
