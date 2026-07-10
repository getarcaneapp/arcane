package authz

import "testing"

func TestPermissionSetAllowsGlobal(t *testing.T) {
	ps := NewPermissionSet()
	ps.AddGlobal(PermContainersList)

	if !ps.Allows(PermContainersList, "env-1") {
		t.Fatal("global perm should apply to any env")
	}
	if !ps.Allows(PermContainersList, "") {
		t.Fatal("global perm should apply org-level too")
	}
	if ps.Allows(PermContainersStart, "env-1") {
		t.Fatal("unrelated perm should be denied")
	}
}

func TestPermissionSetEnvScopedDoesNotLeak(t *testing.T) {
	ps := NewPermissionSet()
	ps.AddEnv("env-1", PermContainersStart)

	if !ps.Allows(PermContainersStart, "env-1") {
		t.Fatal("env perm should apply to its own env")
	}
	if ps.Allows(PermContainersStart, "env-2") {
		t.Fatal("env perm must not leak to another env")
	}
	if ps.Allows(PermContainersStart, "") {
		t.Fatal("env perm must not satisfy an org-level check")
	}
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
			if got := tt.ps.AllowsAny(PermActivitiesRead); got != tt.want {
				t.Fatalf("AllowsAny() = %v, want %v", got, tt.want)
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

	if ps.IsGlobalAdmin() {
		t.Fatal("global admin should reject injected unknown permissions")
	}
}

func TestPermissionSetIsGlobalAdminRequiresExactKnownSet(t *testing.T) {
	ps := NewPermissionSet()
	perms := AllPermissions()
	for _, perm := range perms[1:] {
		ps.AddGlobal(perm)
	}
	ps.AddGlobal("containers:does-not-exist")

	if ps.IsGlobalAdmin() {
		t.Fatal("global admin should require the complete known permission set")
	}
}

func TestPermissionSetIsGlobalAdmin(t *testing.T) {
	ps := NewPermissionSet()
	for _, perm := range AllPermissions() {
		ps.AddGlobal(perm)
	}

	if !ps.IsGlobalAdmin() {
		t.Fatal("complete known permission set should be global admin")
	}
}

func TestSudoAllowsEverything(t *testing.T) {
	ps := SudoPermissionSet()
	if !ps.Allows(PermContainersDelete, "any-env") {
		t.Fatal("sudo should allow any perm on any env")
	}
	if !ps.Allows(PermUsersDelete, "") {
		t.Fatal("sudo should allow org-level perms")
	}
	if !ps.IsGlobalAdmin() {
		t.Fatal("sudo should report as global admin")
	}
}

func TestEnvironmentPermissionSetScopesToOwnEnvironment(t *testing.T) {
	ps := EnvironmentPermissionSet("env-A")

	if !ps.Allows(PermContainersStart, "env-A") {
		t.Fatal("environment token should allow env-scoped permissions for its own env")
	}
	if ps.Allows(PermContainersStart, "env-B") {
		t.Fatal("environment token must not allow env-scoped permissions for another env")
	}
	if ps.Allows(PermUsersList, "") {
		t.Fatal("environment token must not allow org-level permissions")
	}
	if ps.IsGlobalAdmin() {
		t.Fatal("environment token must not be global admin")
	}

	empty := EnvironmentPermissionSet("")
	if empty.Allows(PermContainersStart, "env-A") {
		t.Fatal("environment token with empty env id must deny env-scoped permissions")
	}
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
		if got := EnvIDFromPath(input); got != want {
			t.Errorf("EnvIDFromPath(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestIsOrgLevelAndEnvScoped(t *testing.T) {
	if !IsOrgLevel(PermUsersList) {
		t.Fatal("users:list should be org-level")
	}
	if IsEnvScoped(PermUsersList) {
		t.Fatal("users:list should not be env-scoped")
	}
	if IsOrgLevel(PermContainersStart) {
		t.Fatal("containers:start should not be org-level")
	}
	if !IsEnvScoped(PermContainersStart) {
		t.Fatal("containers:start should be env-scoped")
	}
}

func TestIsKnownPermissionRejectsSyntheticPrefixMatches(t *testing.T) {
	// Synthetic permissions whose prefix matches a known env-scoped family
	// must not be accepted — otherwise an admin could inflate ps.Global past
	// TotalPermissionsCount() with bogus entries and trip IsGlobalAdmin().
	for _, p := range []string{"containers:fake1", "projects:bogus", "images:made-up"} {
		if IsKnownPermission(p) {
			t.Errorf("IsKnownPermission(%q) = true, want false", p)
		}
		if IsEnvScoped(p) {
			t.Errorf("IsEnvScoped(%q) = true, want false", p)
		}
	}
}

func TestBuiltInRolesOnlyReferenceKnownPermissions(t *testing.T) {
	for _, p := range BuiltInEditorPermissions() {
		if !IsKnownPermission(p) {
			t.Errorf("editor references unknown perm %q", p)
		}
	}
	for _, p := range BuiltInDeployerPermissions() {
		if !IsKnownPermission(p) {
			t.Errorf("deployer references unknown perm %q", p)
		}
	}
	for _, p := range BuiltInViewerPermissions() {
		if !IsKnownPermission(p) {
			t.Errorf("viewer references unknown perm %q", p)
		}
	}
}

func TestPermissionCatalogDerivesKnownPermissionsAndScopes(t *testing.T) {
	catalog := PermissionCatalog()
	if len(catalog) == 0 {
		t.Fatal("permission catalog must not be empty")
	}

	all := AllPermissions()
	if len(all) != TotalPermissionsCount() {
		t.Fatalf("AllPermissions length = %d, TotalPermissionsCount = %d", len(all), TotalPermissionsCount())
	}

	seen := make(map[string]struct{}, len(all))
	var catalogCount int
	for _, resource := range catalog {
		if resource.Scope != PermissionScopeGlobal && resource.Scope != PermissionScopeEnv {
			t.Fatalf("resource %q has invalid scope %q", resource.Key, resource.Scope)
		}
		for _, action := range resource.Actions {
			catalogCount++
			if action.Permission == "" {
				t.Fatalf("resource %q action %q has empty permission", resource.Key, action.Key)
			}
			if _, exists := seen[action.Permission]; exists {
				t.Fatalf("duplicate permission %q in catalog", action.Permission)
			}
			seen[action.Permission] = struct{}{}
			if !IsKnownPermission(action.Permission) {
				t.Fatalf("catalog permission %q is not known", action.Permission)
			}
			if resource.Scope == PermissionScopeGlobal && !IsOrgLevel(action.Permission) {
				t.Fatalf("catalog permission %q should be org-level", action.Permission)
			}
			if resource.Scope == PermissionScopeEnv && !IsEnvScoped(action.Permission) {
				t.Fatalf("catalog permission %q should be env-scoped", action.Permission)
			}
		}
	}

	if catalogCount != len(all) {
		t.Fatalf("catalog permission count = %d, AllPermissions count = %d", catalogCount, len(all))
	}
	for _, permission := range all {
		if _, exists := seen[permission]; !exists {
			t.Fatalf("AllPermissions includes %q outside catalog", permission)
		}
	}
}
