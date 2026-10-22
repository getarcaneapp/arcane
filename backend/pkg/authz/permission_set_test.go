package authz

import (
	"testing"

	"github.com/getarcaneapp/arcane/types/v3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func builtInRolePermissions(t *testing.T, id string) []string {
	t.Helper()
	role, ok := BuiltInRole(id)
	require.True(t, ok, "missing built-in role %q", id)
	return role.Permissions
}

func TestPermissionSetAllowsGlobal(t *testing.T) {
	ps := NewPermissionSet()
	ps.AddGlobal(PermContainersList)

	require.True(t, ps.Allows(PermContainersList, "env-1"),
		"global perm should apply to any env")

	require.True(t, ps.Allows(PermContainersList, ""),
		"global perm should apply org-level too")

	require.False(t, ps.Allows(PermContainersStart, "env-1"),
		"unrelated perm should be denied")
}

func TestPermissionSetEnvScopedDoesNotLeak(t *testing.T) {
	ps := NewPermissionSet()
	ps.AddEnv("env-1", PermContainersStart)

	require.True(t, ps.Allows(PermContainersStart, "env-1"),
		"env perm should apply to its own env")

	require.False(t, ps.Allows(PermContainersStart, "env-2"),
		"env perm must not leak to another env")

	require.False(t, ps.Allows(PermContainersStart, ""),
		"env perm must not satisfy an org-level check")
}

func TestPermissionSetAllowsAnyEffectiveScope(t *testing.T) {
	tests := []struct {
		name string
		ps   *PermissionSet
		want bool
	}{
		{name: "nil", ps: nil, want: false},
		{name: "empty", ps: NewPermissionSet(), want: false},
		{name: "unrelated environment permission", ps: func() *PermissionSet {
			ps := NewPermissionSet()
			ps.AddEnv("env-1", PermContainersList)
			return ps
		}(), want: false},
		{name: "matching environment permission", ps: func() *PermissionSet {
			ps := NewPermissionSet()
			ps.AddEnv("env-1", PermActivitiesRead)
			return ps
		}(), want: true},
		{name: "matching global permission", ps: func() *PermissionSet {
			ps := NewPermissionSet()
			ps.AddGlobal(PermActivitiesRead)
			return ps
		}(), want: true},
		{name: "sudo", ps: SudoPermissionSet(), want: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			{
				got := tt.ps.AllowsAny(PermActivitiesRead)
				require.Equal(t, tt.want, got,
					"AllowsAny() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestPermissionSetIsGlobalAdminRequiresWildcard(t *testing.T) {
	everything := NewPermissionSet()
	everything.AddGlobal(AllPermissions()...)
	require.False(t, everything.IsGlobalAdmin())

	envWildcard := NewPermissionSet()
	envWildcard.AddEnv("env-1", PermAll)
	require.False(t, envWildcard.IsGlobalAdmin())

	admin := NewPermissionSet()
	admin.AddGlobal(PermAll)
	require.True(t, admin.IsGlobalAdmin())
	require.False(t, admin.Sudo)
}

func TestPermissionSetWildcardAllowsEverything(t *testing.T) {
	admin := NewPermissionSet()
	admin.AddGlobal(PermAll)
	for _, permission := range AllPermissions() {
		require.True(t, admin.Allows(permission, ""), permission)
		require.True(t, admin.Allows(permission, "env-1"), permission)
		require.True(t, admin.Scope(permission, "env-1").IsUnrestricted(), permission)
	}

	envAdmin := NewPermissionSet()
	envAdmin.AddEnv("env-1", PermAll)
	require.True(t, envAdmin.Allows(PermContainersStart, "env-1"))
	require.False(t, envAdmin.Allows(PermContainersStart, "env-2"))
	require.False(t, envAdmin.Allows(PermUsersList, ""))
}

func TestPermissionSetScopedGrants(t *testing.T) {
	web := types.ResourceSelector{Labels: map[string]string{"team": "web"}}
	db := types.ResourceSelector{Projects: []string{"db"}}

	ps := NewPermissionSet()
	ps.AddScopedEnv("env-1", web, PermContainersStart)
	ps.AddScopedGlobal(db, PermContainersStart)

	require.True(t, ps.Allows(PermContainersStart, "env-1"))
	require.True(t, ps.Allows(PermContainersStart, "env-2"))
	require.False(t, ps.Allows(PermContainersStop, "env-1"))
	require.True(t, ps.AllowsAny(PermContainersStart))
	require.Contains(t, ps.PerEnv, "env-1")
	require.False(t, ps.IsGlobalAdmin())

	scope := ps.Scope(PermContainersStart, "env-1")
	require.False(t, scope.IsUnrestricted())
	require.True(t, scope.Matches(ResourceAttributes{Labels: map[string]string{"team": "web"}}))
	require.True(t, scope.Matches(ResourceAttributes{Project: "db"}))
	require.False(t, scope.Matches(ResourceAttributes{Project: "cache"}))

	other := ps.Scope(PermContainersStart, "env-2")
	require.False(t, other.Matches(ResourceAttributes{Labels: map[string]string{"team": "web"}}))
	require.True(t, other.Matches(ResourceAttributes{Project: "db"}))

	require.False(t, ps.Scope(PermContainersStop, "env-1").Matches(ResourceAttributes{Project: "db"}))
}

func TestPermissionSetScopeUnrestrictedGrantWins(t *testing.T) {
	ps := NewPermissionSet()
	ps.AddScopedEnv("env-1", types.ResourceSelector{Names: []string{"web-*"}}, PermContainersStart)
	ps.AddEnv("env-1", PermContainersStart)

	require.True(t, ps.Scope(PermContainersStart, "env-1").IsUnrestricted())
	require.False(t, ps.Scope(PermContainersStart, "env-2").IsUnrestricted())

	var nilSet *PermissionSet
	require.False(t, nilSet.Scope(PermContainersStart, "env-1").Matches(ResourceAttributes{Names: []string{"web-1"}}))
}

func TestPermissionSetPermissionsForEnv(t *testing.T) {
	ps := NewPermissionSet()
	ps.AddGlobal(PermUsersList)
	ps.AddEnv("env-1", PermContainersList)
	ps.AddEnv("env-2", PermImagesList)
	ps.AddScopedEnv("env-1", types.ResourceSelector{OwnerOnly: true}, PermContainersStart)
	ps.AddScopedEnv("env-1", types.ResourceSelector{OwnerOnly: true}, PermContainersList)

	all, scoped := ps.PermissionsForEnv("env-1")
	require.Equal(t, []string{PermContainersList, PermContainersStart, PermUsersList}, all)
	require.Equal(t, []string{PermContainersStart}, scoped)

	all, scoped = SudoPermissionSet().PermissionsForEnv("env-1")
	require.Equal(t, []string{PermAll}, all)
	require.Empty(t, scoped)
}

func TestSudoAllowsEverything(t *testing.T) {
	ps := SudoPermissionSet()

	require.True(t, ps.Allows(PermContainersDelete, "any-env"),
		"sudo should allow any perm on any env")

	require.True(t, ps.Allows(PermUsersDelete, ""),
		"sudo should allow org-level perms")

	require.True(t, ps.IsGlobalAdmin(),
		"sudo should report as global admin")
}

func TestEnvironmentPermissionSetScopesToOwnEnvironment(t *testing.T) {
	ps := EnvironmentPermissionSet("env-A")

	require.True(t, ps.Allows(PermContainersStart, "env-A"),
		"environment token should allow env-scoped permissions for its own env")

	require.False(t, ps.Allows(PermContainersStart, "env-B"),
		"environment token must not allow env-scoped permissions for another env")

	require.False(t, ps.Allows(PermUsersList, ""),
		"environment token must not allow org-level permissions")

	require.False(t, ps.IsGlobalAdmin(),
		"environment token must not be global admin")

	empty := EnvironmentPermissionSet("")

	require.False(t, empty.Allows(PermContainersStart, "env-A"),
		"environment token with empty env id must deny env-scoped permissions")
}

func TestEnvIDFromPath(t *testing.T) {
	cases := map[string]string{
		"/environments/abc-123/containers":     "abc-123",
		"/environments/abc-123/containers/foo": "abc-123",
		"/api/environments/abc-123/projects":   "abc-123",
		"/environments/abc-123":                "", // org-level env detail
		"/users":                               "",
		"":                                     "",
	}
	for input, want := range cases {
		{
			got := EnvIDFromPath(input)
			assert.Equal(t, want, got,
				"EnvIDFromPath(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestIsOrgLevelAndEnvScoped(t *testing.T) {
	require.True(t, IsOrgLevel(PermUsersList),
		"users:list should be org-level")

	require.False(t, IsEnvScoped(PermUsersList),
		"users:list should not be env-scoped")

	require.False(t, IsOrgLevel(PermContainersStart),
		"containers:start should not be org-level")

	require.True(t, IsEnvScoped(PermContainersStart),
		"containers:start should be env-scoped")
}

func TestIsKnownPermissionRejectsSyntheticPrefixMatches(t *testing.T) {
	// Synthetic permissions whose prefix matches a known env-scoped family
	// must not be accepted — otherwise an admin could inflate ps.Global past
	// TotalPermissionsCount() with bogus entries and trip IsGlobalAdmin().
	for _, p := range []string{"containers:fake1", "projects:bogus", "images:made-up"} {

		assert.False(t, IsKnownPermission(p),
			"IsKnownPermission(%q) = true, want false", p)

		assert.False(t, IsEnvScoped(p),
			"IsEnvScoped(%q) = true, want false", p)

	}
}

func TestVariablePermissionsAreSeparateGlobalGrantsWithBuiltInAccess(t *testing.T) {
	variablePermissions := []string{
		PermVariablesRead,
		PermVariablesCreate,
		PermVariablesUpdate,
		PermVariablesDelete,
		PermVariablesSync,
	}

	for _, permission := range variablePermissions {

		require.True(t, IsKnownPermission(permission),
			"variable permission %q must be known", permission)

		require.True(t, IsOrgLevel(permission),
			"variable permission %q must be global", permission)

		require.NotContains(t, builtInRolePermissions(t, BuiltInRoleMonitor), permission,
			"Monitor must not receive %q", permission)

	}

	admin := NewPermissionSet()
	admin.AddGlobal(builtInRolePermissions(t, BuiltInRoleAdmin)...)
	for _, permission := range variablePermissions {
		require.True(t, admin.Allows(permission, ""),
			"Admin must receive %q", permission)
	}

	for name, permissions := range map[string][]string{
		"Editor":          builtInRolePermissions(t, BuiltInRoleEditor),
		"No-Shell Editor": builtInRolePermissions(t, BuiltInRoleNoShellEditor),
	} {
		for _, permission := range variablePermissions {
			require.Contains(t, permissions, permission,
				"%s must receive %q", name, permission)
		}
	}

	for name, permissions := range map[string][]string{
		"Viewer":   builtInRolePermissions(t, BuiltInRoleViewer),
		"Deployer": builtInRolePermissions(t, BuiltInRoleDeployer),
	} {

		require.Contains(t, permissions, PermVariablesRead,
			"%s must receive %q", name, PermVariablesRead)

		for _, permission := range variablePermissions[1:] {
			require.NotContains(t, permissions, permission,
				"%s must not receive %q", name, permission)
		}
	}

	templatesOnly := NewPermissionSet()
	templatesOnly.AddGlobal(PermTemplatesRead, PermTemplatesCreate, PermTemplatesUpdate, PermTemplatesDelete)
	for _, permission := range variablePermissions {
		require.False(t, templatesOnly.Allows(permission, ""),
			"template grants must not satisfy %q", permission)
	}
}

func TestSystemBackupPermissionsAreAdminOnlyGlobalGrants(t *testing.T) {
	builtInRoles := map[string][]string{
		"Editor":          builtInRolePermissions(t, BuiltInRoleEditor),
		"No-Shell Editor": builtInRolePermissions(t, BuiltInRoleNoShellEditor),
		"Viewer":          builtInRolePermissions(t, BuiltInRoleViewer),
		"Monitor":         builtInRolePermissions(t, BuiltInRoleMonitor),
		"Deployer":        builtInRolePermissions(t, BuiltInRoleDeployer),
	}
	for _, permission := range []string{PermSystemBackupsRead, PermSystemBackupsManage, PermSystemBackupsRestore, PermSystemBackupsRecoveryKey} {
		require.True(t, IsKnownPermission(permission), "system backup permission %q must be known", permission)
		require.True(t, IsOrgLevel(permission), "system backup permission %q must be global", permission)
		require.Contains(t, AllPermissions(), permission, "Admin must receive %q", permission)
		for name, permissions := range builtInRoles {
			require.NotContains(t, permissions, permission, "%s must not receive %q", name, permission)
		}
	}
}

func TestS3DestinationPermissionsAreSeparateGlobalGrantsWithBuiltInAccess(t *testing.T) {
	s3Permissions := []string{
		PermS3DestinationsList,
		PermS3DestinationsRead,
		PermS3DestinationsCreate,
		PermS3DestinationsUpdate,
		PermS3DestinationsDelete,
		PermS3DestinationsTest,
		PermS3DestinationsSync,
	}

	for _, permission := range s3Permissions {

		require.True(t, IsKnownPermission(permission),
			"S3 destination permission %q must be known", permission)

		require.True(t, IsOrgLevel(permission),
			"S3 destination permission %q must be global", permission)

		require.Contains(t, AllPermissions(), permission,
			"Admin must receive %q", permission)

		require.NotContains(t, builtInRolePermissions(t, BuiltInRoleMonitor), permission,
			"Monitor must not receive %q", permission)

		require.NotContains(t, builtInRolePermissions(t, BuiltInRoleDeployer), permission,
			"Deployer must not receive %q", permission)

	}

	// Roles that can run backups pick a destination, so they read but never
	// manage the stored credentials.
	for name, permissions := range map[string][]string{
		"Editor":          builtInRolePermissions(t, BuiltInRoleEditor),
		"No-Shell Editor": builtInRolePermissions(t, BuiltInRoleNoShellEditor),
		"Viewer":          builtInRolePermissions(t, BuiltInRoleViewer),
	} {
		require.Contains(t, permissions, PermS3DestinationsList,
			"%s must receive %q", name, PermS3DestinationsList)

		require.Contains(t, permissions, PermS3DestinationsRead,
			"%s must receive %q", name, PermS3DestinationsRead)

		for _, permission := range s3Permissions[2:] {
			require.NotContains(t, permissions, permission,
				"%s must not receive %q", name, permission)
		}
	}

	settingsOnly := NewPermissionSet()
	settingsOnly.AddGlobal(PermSettingsRead, PermSettingsWrite)
	for _, permission := range s3Permissions {
		require.False(t, settingsOnly.Allows(permission, ""),
			"settings grants must not satisfy %q", permission)
	}
}

func TestPermissionCatalogDerivesKnownPermissionsAndScopes(t *testing.T) {
	catalog := PermissionCatalog()

	require.NotEmpty(t, catalog,
		"permission catalog must not be empty")

	all := AllPermissions()
	if len(all) != TotalPermissionsCount() {
		require.Len(t, all, TotalPermissionsCount(),
			"AllPermissions length = %d, TotalPermissionsCount = %d", len(all), TotalPermissionsCount())
	}

	seen := make(map[string]struct{}, len(all))
	var catalogCount int
	for _, resource := range catalog {

		require.False(t, resource.Scope != PermissionScopeGlobal && resource.Scope != PermissionScopeEnv,
			"resource %q has invalid scope %q", resource.Key, resource.Scope)

		for _, action := range resource.Actions {
			catalogCount++

			require.NotEmpty(t, action.Permission,
				"resource %q action %q has empty permission", resource.Key, action.Key)
			{

				_, exists := seen[action.Permission]
				require.False(t, exists,
					"duplicate permission %q in catalog", action.Permission)
			}

			seen[action.Permission] = struct{}{}

			require.True(t, IsKnownPermission(action.Permission),
				"catalog permission %q is not known", action.Permission)

			require.False(t, resource.Scope == PermissionScopeGlobal && !IsOrgLevel(action.Permission),
				"catalog permission %q should be org-level", action.Permission)

			require.False(t, resource.Scope == PermissionScopeEnv && !IsEnvScoped(action.Permission),
				"catalog permission %q should be env-scoped", action.Permission)

		}
	}

	require.Equal(t, len(all), catalogCount,
		"catalog permission count = %d, AllPermissions count = %d", catalogCount, len(all))

	for _, permission := range all {
		{
			_, exists := seen[permission]
			require.True(t, exists,
				"AllPermissions includes %q outside catalog", permission)
		}
	}
}

func TestVolumeRenamePermissionIsEditorOnlyByDefault(t *testing.T) {
	require.Contains(t, AllPermissions(), PermVolumesRename)
	require.Contains(t, builtInRolePermissions(t, BuiltInRoleEditor), PermVolumesRename)
	require.NotContains(t, builtInRolePermissions(t, BuiltInRoleViewer), PermVolumesRename)
	require.NotContains(t, builtInRolePermissions(t, BuiltInRoleDeployer), PermVolumesRename)
}

func TestNotificationsManageRequiresGlobalScope(t *testing.T) {
	require.True(t, IsOrgLevel(PermNotificationsManage),
		"notifications:manage must be org-level for manager-global notification settings")

	require.False(t, IsEnvScoped(PermNotificationsManage),
		"notifications:manage must not be environment-scoped")

	ps := NewPermissionSet()
	ps.AddEnv("env-1", PermNotificationsManage)

	require.False(t, ps.Allows(PermNotificationsManage, ""),
		"an environment-scoped notification grant must not authorize the global resource")

	ps.AddGlobal(PermNotificationsManage)

	require.True(t, ps.Allows(PermNotificationsManage, ""),
		"a global notification grant must authorize the global resource")
}
