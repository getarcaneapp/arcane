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
		ps   *PermissionSet
		name string
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

func TestForwardedPermissionsMirrorCallerForTargetEnvironment(t *testing.T) {
	caller := NewPermissionSet()
	caller.AddGlobal(PermUsersList, PermContainersStart, "unknown:perm")
	caller.AddEnv("env-A", PermContainersUpdate, PermContainersStart, PermSettingsUpdate)
	caller.AddEnv("env-B", PermContainersDelete)

	forwarded := caller.ForwardedPermissions("env-A")
	require.Equal(t, []string{PermUsersList, PermContainersStart}, forwarded.Global)
	require.Equal(t, []string{PermContainersUpdate}, forwarded.Environment)

	agent := forwarded.PermissionSet("0")
	tests := []struct {
		name  string
		perm  string
		envID string
		want  bool
	}{
		{name: "global org permission", perm: PermUsersList, want: true},
		{name: "global env permission", perm: PermContainersStart, envID: "0", want: true},
		{name: "env permission on agent env", perm: PermContainersUpdate, envID: "0", want: true},
		{name: "env permission is not global", perm: PermContainersUpdate},
		{name: "other environment grant is dropped", perm: PermContainersDelete, envID: "0"},
		{name: "env-scoped org permission is dropped", perm: PermSettingsUpdate},
		{name: "unheld permission", perm: PermUsersCreate},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, agent.Allows(tt.perm, tt.envID))
		})
	}
	require.False(t, agent.Sudo)
	require.False(t, agent.IsGlobalAdmin())
	require.NotContains(t, agent.Global, "unknown:perm")
}

func TestForwardedPermissionsDropUnknownAndMisscopedPermissions(t *testing.T) {
	agent := ForwardedPermissions{
		Global:      []string{"unknown:perm", PermUsersList},
		Environment: []string{"unknown:perm", PermUsersCreate, PermContainersStart},
	}.PermissionSet("0")

	require.Equal(t, map[string]struct{}{PermUsersList: {}}, agent.Global)
	require.Equal(t, map[string]struct{}{PermContainersStart: {}}, agent.PerEnv["0"])
	require.False(t, agent.Allows(PermUsersCreate, ""))
}

func TestForwardedPermissionsKeepGlobalAdminWithoutSudo(t *testing.T) {
	admin := NewPermissionSet()
	admin.AddGlobal(AllPermissions()...)
	agent := admin.ForwardedPermissions("env-A").PermissionSet("0")
	require.True(t, agent.IsGlobalAdmin())
	require.False(t, agent.Sudo)

	envAdmin := NewPermissionSet()
	envAdmin.AddEnv("env-A", AllPermissions()...)
	envAgent := envAdmin.ForwardedPermissions("env-A").PermissionSet("0")
	require.False(t, envAgent.IsGlobalAdmin())
	require.True(t, envAgent.Allows(PermContainersStart, "0"))

	var none *PermissionSet
	empty := none.ForwardedPermissions("env-A").PermissionSet("0")
	require.False(t, empty.Allows(PermContainersList, "0"))
	require.False(t, empty.Sudo)
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

func TestBuiltInRolesAreUniqueSubsetsOfAllPermissions(t *testing.T) {
	for name, permissions := range builtInRoleSets() {
		require.NotEmpty(t, permissions, "%s must not be empty", name)
		seen := make(map[string]struct{}, len(permissions))
		for _, p := range permissions {
			require.True(t, IsKnownPermission(p), "%s references unknown perm %q", name, p)
			_, dup := seen[p]
			require.False(t, dup, "%s lists %q more than once", name, p)
			seen[p] = struct{}{}
		}
	}
}

func TestReadOnlyBuiltInRolesContainOnlyNonMutatingActions(t *testing.T) {
	actionKeys := make(map[string]string)
	for _, resource := range PermissionCatalog() {
		for _, action := range resource.Actions {
			actionKeys[action.Permission] = action.Key
		}
	}
	readOnlyKeys := []string{"list", "read", "logs"}

	for name, permissions := range map[string][]string{
		"Viewer":  BuiltInViewerPermissions(),
		"Monitor": BuiltInMonitorPermissions(),
	} {
		for _, p := range permissions {
			require.Contains(t, readOnlyKeys, actionKeys[p], "%s must not receive mutating permission %q", name, p)
		}
	}
}

func TestBuiltInRoleUpdatePermissions(t *testing.T) {
	deployer := BuiltInDeployerPermissions()
	require.NotContains(t, deployer, PermContainersUpdate)
	require.Contains(t, deployer, PermContainersAutoUpdate)
	require.Contains(t, deployer, PermImageUpdatesApply)

	require.Contains(t, BuiltInEditorPermissions(), PermImageUpdatesApply)
	require.Contains(t, BuiltInNoShellEditorPermissions(), PermImageUpdatesApply)
	require.NotContains(t, BuiltInNoShellEditorPermissions(), PermContainersExec)
	require.Len(t, BuiltInNoShellEditorPermissions(), len(BuiltInEditorPermissions())-1)

	for name, permissions := range builtInRoleSets() {
		require.NotContains(t, permissions, PermDiagnosticsProfile, "%s must not receive %q", name, PermDiagnosticsProfile)
	}
	require.True(t, IsOrgLevel(PermDiagnosticsProfile))
	require.True(t, IsEnvScoped(PermImageUpdatesApply))
}

func TestBuiltInRoleSettingsPermissions(t *testing.T) {
	require.True(t, IsOrgLevel(PermSettingsRead))
	require.True(t, IsOrgLevel(PermSettingsUpdate))
	require.True(t, IsEnvScoped(PermEnvironmentSettingsRead))
	require.True(t, IsEnvScoped(PermEnvironmentSettingsUpdate))
	require.Equal(t, []string{PermEnvironmentSettingsRead, PermEnvironmentSettingsUpdate}, ExpandPermissions([]string{PermEnvironmentSettingsUpdate}))
	require.Equal(t, []string{PermUsersList}, ExpandPermissions([]string{PermUsersList}))

	for name, permissions := range map[string][]string{
		"Editor": BuiltInEditorPermissions(),
		"Viewer": BuiltInViewerPermissions(),
	} {
		require.Contains(t, permissions, PermSettingsRead, name)
		require.Contains(t, permissions, PermEnvironmentSettingsRead, name)
		require.NotContains(t, permissions, PermSettingsUpdate, name)
		require.NotContains(t, permissions, PermEnvironmentSettingsUpdate, name)
	}
	for name, permissions := range map[string][]string{
		"Deployer": BuiltInDeployerPermissions(),
		"Monitor":  BuiltInMonitorPermissions(),
	} {
		require.NotContains(t, permissions, PermSettingsRead, name)
		require.NotContains(t, permissions, PermEnvironmentSettingsRead, name)
	}

	admin := NewPermissionSet()
	admin.AddGlobal(AllPermissions()...)
	require.True(t, admin.IsGlobalAdmin())
	delete(admin.Global, PermEnvironmentSettingsUpdate)
	require.False(t, admin.IsGlobalAdmin())
}

func builtInRoleSets() map[string][]string {
	return map[string][]string{
		"Editor":          BuiltInEditorPermissions(),
		"No-Shell Editor": BuiltInNoShellEditorPermissions(),
		"Deployer":        BuiltInDeployerPermissions(),
		"Viewer":          BuiltInViewerPermissions(),
		"Monitor":         BuiltInMonitorPermissions(),
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
	for _, permission := range []string{
		PermSystemBackupsList,
		PermSystemBackupsRead,
		PermSystemBackupsCreate,
		PermSystemBackupsUpdate,
		PermSystemBackupsDelete,
		PermSystemBackupsUpload,
		PermSystemBackupsRestore,
		PermSystemBackupsRecoveryKey,
	} {
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
	settingsOnly.AddGlobal(PermSettingsRead, PermSettingsUpdate)
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
		require.NotEmpty(t, resource.Label, "resource %q has no label", resource.Key)
		require.NotEmpty(t, resource.Description, "resource %q has no description", resource.Key)

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
	require.Contains(t, BuiltInEditorPermissions(), PermVolumesRename)
	require.NotContains(t, BuiltInViewerPermissions(), PermVolumesRename)
	require.NotContains(t, BuiltInDeployerPermissions(), PermVolumesRename)
}

func TestNotificationPermissionsRequireGlobalScope(t *testing.T) {
	for _, permission := range []string{
		PermNotificationsList,
		PermNotificationsRead,
		PermNotificationsUpdate,
		PermNotificationsDelete,
		PermNotificationsTest,
	} {
		require.True(t, IsOrgLevel(permission),
			"%s must be org-level for manager-global notification settings", permission)

		require.False(t, IsEnvScoped(permission),
			"%s must not be environment-scoped", permission)

		ps := NewPermissionSet()
		ps.AddEnv("env-1", permission)

		require.False(t, ps.Allows(permission, ""),
			"an environment-scoped notification grant must not authorize the global resource")

		ps.AddGlobal(permission)

		require.True(t, ps.Allows(permission, ""),
			"a global notification grant must authorize the global resource")
	}
}

func TestEveryPermissionHasCatalogDescription(t *testing.T) {
	descriptions := make(map[string]string)
	for _, resource := range PermissionCatalog() {
		for _, action := range resource.Actions {
			descriptions[action.Permission] = action.Description
		}
	}
	for _, permission := range AllPermissions() {
		require.NotEmpty(t, descriptions[permission], "permission %q has no catalog description", permission)
	}
}

func TestPermissionRequiresAreKnownAndAcyclic(t *testing.T) {
	requires := make(map[string][]string)
	scopes := make(map[string]string)
	for _, resource := range PermissionCatalog() {
		for _, action := range resource.Actions {
			requires[action.Permission] = action.Requires
			scopes[action.Permission] = resource.Scope
		}
	}

	for permission, deps := range requires {
		for _, dep := range deps {
			require.True(t, IsKnownPermission(dep), "%q requires unknown permission %q", permission, dep)
			require.NotEqual(t, permission, dep, "%q requires itself", permission)
			require.Equal(t, scopes[permission], scopes[dep], "%q requires %q from another scope", permission, dep)
		}
	}

	state := make(map[string]int)
	var visit func(permission string, path []string)
	visit = func(permission string, path []string) {
		switch state[permission] {
		case 1:
			require.FailNow(t, "permission dependency cycle", "%v -> %s", path, permission)
		case 2:
			return
		}
		state[permission] = 1
		for _, dep := range requires[permission] {
			visit(dep, append(path, permission))
		}
		state[permission] = 2
	}
	for _, permission := range AllPermissions() {
		visit(permission, nil)
	}
}

func TestExpandPermissionsAddsTransitiveRequirementsInCatalogOrder(t *testing.T) {
	require.Equal(t, []string{
		PermContainersList,
		PermContainersRead,
		PermContainersCreate,
		PermImagesList,
		PermImagesRead,
		PermVolumesList,
		PermNetworksList,
	}, ExpandPermissions([]string{PermContainersCreate}))

	require.Equal(t, []string{
		PermSettingsRead,
		PermDiagnosticsRead,
		PermDiagnosticsProfile,
	}, ExpandPermissions([]string{PermDiagnosticsProfile, PermSettingsRead}))

	require.Equal(t, []string{
		PermSwarmRead,
		PermSwarmNodesList,
		PermSwarmNodesRead,
		PermSwarmNodesPromote,
		"unknown:permission",
	}, ExpandPermissions([]string{"unknown:permission", PermSwarmNodesPromote}))

	require.Empty(t, ExpandPermissions(nil))
}

func TestBuiltInRolesAreClosedUnderExpansion(t *testing.T) {
	for name, permissions := range map[string][]string{
		"Admin":           AllPermissions(),
		"Editor":          BuiltInEditorPermissions(),
		"No-Shell Editor": BuiltInNoShellEditorPermissions(),
		"Deployer":        BuiltInDeployerPermissions(),
		"Monitor":         BuiltInMonitorPermissions(),
		"Viewer":          BuiltInViewerPermissions(),
	} {
		require.ElementsMatch(t, permissions, ExpandPermissions(permissions), "%s must already include every required permission", name)
	}
}
