package authz

import (
	"context"
)

type permissionsContextKey struct{}

func WithPermissions(ctx context.Context, ps *PermissionSet) context.Context {
	return context.WithValue(ctx, permissionsContextKey{}, ps)
}

func PermissionsFromContext(ctx context.Context) (*PermissionSet, bool) {
	ps, ok := ctx.Value(permissionsContextKey{}).(*PermissionSet)
	return ps, ok
}
