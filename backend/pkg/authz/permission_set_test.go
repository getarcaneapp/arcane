package authz

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

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

func TestPermissionSetIsGlobalAdminRejectsUnknownPermissions(t *testing.T) {
	ps := NewPermissionSet()
	perms := AllPermissions()
	for i, perm := range perms {
		if i == 0 {
			continue
		}
		ps.AddGlobal(perm)
	}
	ps.AddGlobal("projects:made-up")

	require.False(t, ps.IsGlobalAdmin(),
		"global admin should reject injected unknown permissions")

}

func TestPermissionSetIsGlobalAdminRequiresExactKnownSet(t *testing.T) {
	ps := NewPermissionSet()
	perms := AllPermissions()
	for _, perm := range perms[1:] {
		ps.AddGlobal(perm)
	}
	ps.AddGlobal("containers:does-not-exist")

	require.False(t, ps.IsGlobalAdmin(),
		"global admin should require the complete known permission set")

}

func TestPermissionSetIsGlobalAdmin(t *testing.T) {
	ps := NewPermissionSet()
	for _, perm := range AllPermissions() {
		ps.AddGlobal(perm)
	}

	require.True(t, ps.IsGlobalAdmin(),
		"complete known permission set should be global admin")

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

func TestBuiltInRolesOnlyReferenceKnownPermissions(t *testing.T) {
	for _, p := range BuiltInEditorPermissions() {

		assert.True(t, IsKnownPermission(p),
			"editor references unknown perm %q", p)

	}
	for _, p := range BuiltInDeployerPermissions() {

		assert.True(t, IsKnownPermission(p),
			"deployer references unknown perm %q", p)

	}
	for _, p := range BuiltInViewerPermissions() {

		assert.True(t, IsKnownPermission(p),
			"viewer references unknown perm %q", p)

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

		require.NotContains(t, BuiltInMonitorPermissions(), permission,
			"Monitor must not receive %q", permission)

	}

	for name, permissions := range map[string][]string{
		"Admin":           AllPermissions(),
		"Editor":          BuiltInEditorPermissions(),
		"No-Shell Editor": BuiltInNoShellEditorPermissions(),
	} {
		for _, permission := range variablePermissions {

			require.Contains(t, permissions, permission,
				"%s must receive %q", name, permission)

		}
	}

	for name, permissions := range map[string][]string{
		"Viewer":   BuiltInViewerPermissions(),
		"Deployer": BuiltInDeployerPermissions(),
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
		"Editor":          BuiltInEditorPermissions(),
		"No-Shell Editor": BuiltInNoShellEditorPermissions(),
		"Viewer":          BuiltInViewerPermissions(),
		"Monitor":         BuiltInMonitorPermissions(),
		"Deployer":        BuiltInDeployerPermissions(),
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

		require.NotContains(t, BuiltInMonitorPermissions(), permission,
			"Monitor must not receive %q", permission)

		require.NotContains(t, BuiltInDeployerPermissions(), permission,
			"Deployer must not receive %q", permission)

	}

	// Roles that can run backups pick a destination, so they read but never
	// manage the stored credentials.
	for name, permissions := range map[string][]string{
		"Editor":          BuiltInEditorPermissions(),
		"No-Shell Editor": BuiltInNoShellEditorPermissions(),
		"Viewer":          BuiltInViewerPermissions(),
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
