package authz

import (
	"testing"

	"github.com/getarcaneapp/arcane/types/v3"
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

func TestScopablePermissionsAreEnvScoped(t *testing.T) {
	for _, resource := range PermissionCatalog() {
		for _, action := range resource.Actions {
			if !IsScopable(action.Permission) {
				require.Empty(t, action.Selectors, action.Permission)
				continue
			}
			require.True(t, IsEnvScoped(action.Permission), "scopable permission %q must be env-scoped", action.Permission)
			require.Equal(t, action.Selectors, SelectorKindsFor(action.Permission))
		}
	}
}

func TestPermissionSelectorKinds(t *testing.T) {
	all := []types.SelectorKind{
		types.SelectorKindLabels,
		types.SelectorKindProjects,
		types.SelectorKindOwner,
		types.SelectorKindNames,
	}
	tests := []struct {
		permission string
		want       []types.SelectorKind
	}{
		{permission: PermContainersStart, want: all},
		{permission: PermVolumesDelete, want: all},
		{permission: PermNetworksConnect, want: all},
		{permission: PermProjectsDeploy, want: []types.SelectorKind{types.SelectorKindProjects, types.SelectorKindOwner, types.SelectorKindNames}},
		{permission: PermImagesPull, want: []types.SelectorKind{types.SelectorKindLabels, types.SelectorKindNames}},
		{permission: PermSwarmServices, want: []types.SelectorKind{types.SelectorKindLabels, types.SelectorKindProjects, types.SelectorKindNames}},
		{permission: PermSwarmStacks, want: []types.SelectorKind{types.SelectorKindProjects, types.SelectorKindNames}},
		{permission: PermImagesPrune},
		{permission: PermVolumesPrune},
		{permission: PermNetworksPrune},
		{permission: PermSwarmNodes},
		{permission: PermDashboardRead},
		{permission: PermGitOpsSync},
		{permission: PermPodmanPods},
		{permission: PermUsersList},
		{permission: PermAll},
	}
	for _, tt := range tests {
		t.Run(tt.permission, func(t *testing.T) {
			require.Equal(t, tt.want, SelectorKindsFor(tt.permission))
			require.Equal(t, len(tt.want) > 0, IsScopable(tt.permission))
		})
	}

	kinds := SelectorKindsFor(PermContainersStart)
	kinds[0] = types.SelectorKindNames
	require.Equal(t, all, SelectorKindsFor(PermContainersStart))
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
		PermAuditExport,
	} {
		require.True(t, IsKnownPermission(permission), permission)
		require.True(t, IsOrgLevel(permission), permission)
		require.False(t, IsScopable(permission), permission)
	}
	require.False(t, IsKnownPermission("images:push"))
}
