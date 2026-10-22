package authz

import (
	"testing"

	"github.com/getarcaneapp/arcane/types/v3"
	"github.com/stretchr/testify/require"

	"github.com/getarcaneapp/arcane/backend/v3/internal/common"
)

func TestCheckResource(t *testing.T) {
	ps := &PermissionSet{SubjectID: "user-1"}
	ps.AddEnv("env-1", PermContainersRead)
	ps.AddScopedEnv("env-1", types.ResourceSelector{OwnerOnly: true}, PermContainersStart)
	ctx := WithRequestEnvironment(WithPermissions(t.Context(), ps), "env-1")

	owned := ResourceAttributes{OwnerID: "user-1"}
	other := ResourceAttributes{OwnerID: "user-2"}

	got, ok := PermissionsFromContext(ctx)
	require.True(t, ok)
	require.Same(t, ps, got)
	require.Equal(t, "env-1", RequestEnvironmentFromContext(ctx))

	require.NoError(t, CheckResource(ctx, PermContainersStart, PermContainersRead, owned))
	require.ErrorIs(t, CheckResource(ctx, PermContainersStart, PermContainersRead, other), ErrResourceForbidden)
	require.ErrorIs(t, CheckResource(ctx, PermContainersStart, PermContainersRead, other), common.ErrForbidden)
	require.ErrorIs(t, CheckResource(ctx, PermContainersStart, "", other), ErrResourceOutOfScope)
	require.ErrorIs(t, CheckResource(ctx, PermContainersStart, PermContainersList, other), common.ErrNotFound)

	require.NoError(t, CheckCreate(ctx, PermContainersStart, owned))
	require.ErrorIs(t, CheckCreate(ctx, PermContainersStart, other), ErrResourceForbidden)
}

func TestScopeFromContextWithoutPermissionsIsUnrestricted(t *testing.T) {
	require.True(t, ScopeFromContext(t.Context(), PermContainersStart).IsUnrestricted())
	require.NoError(t, CheckResource(t.Context(), PermContainersStart, PermContainersRead, ResourceAttributes{}))

	nilCtx := WithPermissions(t.Context(), nil)
	require.False(t, ScopeFromContext(nilCtx, PermContainersStart).IsUnrestricted())
	require.ErrorIs(t, CheckCreate(nilCtx, PermContainersStart, ResourceAttributes{}), ErrResourceForbidden)
}
