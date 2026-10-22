package authz

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestBuiltInRolesReferenceKnownPermissions(t *testing.T) {
	roles := BuiltInRoles()
	require.Len(t, roles, 6)
	for _, role := range roles {
		require.True(t, role.BuiltIn, role.ID)
		require.NotEmpty(t, role.Permissions, role.ID)
		for _, permission := range role.Permissions {
			if role.ID == BuiltInRoleAdmin {
				require.Equal(t, PermAll, permission)
				continue
			}
			require.NotEqual(t, PermAll, permission, "%s must not hold the wildcard", role.ID)
			require.True(t, IsKnownPermission(permission), "%s references unknown permission %q", role.ID, permission)
		}
	}
	require.False(t, IsKnownPermission(PermAll))
	require.NotContains(t, AllPermissions(), PermAll)
}

func TestBuiltInRoleLookup(t *testing.T) {
	admin, ok := BuiltInRole(BuiltInRoleAdmin)
	require.True(t, ok)
	require.Equal(t, []string{PermAll}, admin.Permissions)

	noShell, ok := BuiltInRole(BuiltInRoleNoShellEditor)
	require.True(t, ok)
	require.NotContains(t, noShell.Permissions, PermContainersExec)
	require.Contains(t, builtInRolePermissions(t, BuiltInRoleEditor), PermContainersExec)

	noShell.Permissions[0] = "mutated"
	require.NotContains(t, builtInRolePermissions(t, BuiltInRoleNoShellEditor), "mutated")

	_, ok = BuiltInRole("role_missing")
	require.False(t, ok)
}

func TestRBACManagementPermissionsAreGlobal(t *testing.T) {
	for _, permission := range []string{
		PermRolesCreate,
		PermRolesUpdate,
		PermRolesDelete,
		PermBindingsList,
		PermBindingsCreate,
		PermBindingsDelete,
		PermGroupsList,
		PermGroupsRead,
		PermGroupsCreate,
		PermGroupsUpdate,
		PermGroupsDelete,
		PermGroupsMembers,
		PermOidcMappingsList,
		PermOidcMappingsCreate,
		PermOidcMappingsUpdate,
		PermOidcMappingsDelete,
		PermDiagnosticsRead,
		PermAuditRead,
	} {
		require.True(t, IsKnownPermission(permission), permission)
		require.True(t, IsOrgLevel(permission), permission)
	}
	require.False(t, IsKnownPermission("images:push"))
}
