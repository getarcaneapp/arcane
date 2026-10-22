package authz

import (
	"context"
	"errors"

	"github.com/getarcaneapp/arcane/backend/v3/internal/common"
)

var (
	ErrResourceForbidden  = common.Classify(common.ErrForbidden, errors.New("resource is outside your permission scope"))
	ErrResourceOutOfScope = common.Classify(common.ErrNotFound, errors.New("resource not found"))
)

type permissionsContextKey struct{}

type requestEnvironmentContextKey struct{}

func WithPermissions(ctx context.Context, ps *PermissionSet) context.Context {
	return context.WithValue(ctx, permissionsContextKey{}, ps)
}

func PermissionsFromContext(ctx context.Context) (*PermissionSet, bool) {
	ps, ok := ctx.Value(permissionsContextKey{}).(*PermissionSet)
	return ps, ok
}

func WithRequestEnvironment(ctx context.Context, envID string) context.Context {
	return context.WithValue(ctx, requestEnvironmentContextKey{}, envID)
}

func RequestEnvironmentFromContext(ctx context.Context) string {
	envID, _ := ctx.Value(requestEnvironmentContextKey{}).(string)
	return envID
}

func ScopeFromContext(ctx context.Context, perm string) ResourceScope {
	ps, ok := PermissionsFromContext(ctx)
	if !ok {
		return UnrestrictedScope()
	}
	return ps.Scope(perm, RequestEnvironmentFromContext(ctx))
}

func CheckResource(ctx context.Context, actionPerm, readPerm string, a ResourceAttributes) error {
	if ScopeFromContext(ctx, actionPerm).Matches(a) {
		return nil
	}
	if readPerm != "" && ScopeFromContext(ctx, readPerm).Matches(a) {
		return ErrResourceForbidden
	}
	return ErrResourceOutOfScope
}

func CheckCreate(ctx context.Context, perm string, a ResourceAttributes) error {
	if ScopeFromContext(ctx, perm).Matches(a) {
		return nil
	}
	return ErrResourceForbidden
}
