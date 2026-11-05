package authz_test

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"getarcane.dev/arcane/v3/internal/settings"
	"getarcane.dev/arcane/v3/pkg/authz"
)

func TestAccessSurfaceRegistryDefinesSettingsCustomizeAndLandingSemantics(t *testing.T) {
	webhooks := requireAccessSurface(t, "settings.category.webhooks")
	require.Equal(t, authz.AccessSurfaceKindSettingsCategory, webhooks.Kind)
	require.Equal(t, "/system/webhooks", webhooks.URL)
	require.Equal(t, authz.AccessModePermissions, webhooks.AccessMode)
	require.Equal(t, authz.AccessMatchModeAnyOf, webhooks.MatchMode)
	require.Equal(t, authz.AccessScopeModeEnvPlusGlobal, webhooks.ScopeMode)
	require.ElementsMatch(t, []string{authz.PermWebhooksList}, webhooks.Permissions)

	apiKeys := requireAccessSurface(t, "settings.category.apikeys")
	require.Equal(t, authz.AccessScopeModeGlobalOnly, apiKeys.ScopeMode)
	require.ElementsMatch(t, []string{authz.PermApiKeysList, authz.PermApiKeysRead}, apiKeys.Permissions)

	notifications := requireAccessSurface(t, "settings.category.notifications")
	require.Equal(t, authz.AccessScopeModeGlobalOnly, notifications.ScopeMode)
	require.ElementsMatch(t, []string{authz.PermNotificationsList, authz.PermNotificationsRead}, notifications.Permissions)

	templates := requireAccessSurface(t, "route.projects.templates")
	require.Equal(t, authz.AccessSurfaceKindRoute, templates.Kind)
	require.Equal(t, authz.AccessScopeModeGlobalOnly, templates.ScopeMode)
	require.ElementsMatch(t, []string{authz.PermTemplatesList, authz.PermTemplatesRead}, templates.Permissions)
	require.Equal(t, 40, templates.FallbackOrder)

	variables := requireAccessSurface(t, "route.projects.variables")
	require.Equal(t, authz.AccessSurfaceKindRoute, variables.Kind)
	require.Equal(t, authz.AccessScopeModeGlobalOnly, variables.ScopeMode)
	require.Equal(t, authz.AccessMatchModeAnyOf, variables.MatchMode)
	require.ElementsMatch(t, []string{authz.PermVariablesList, authz.PermVariablesRead}, variables.Permissions)
	require.NotContains(t, variables.Permissions, authz.PermTemplatesRead)

	activities := requireAccessSurface(t, "route.activities")
	require.Equal(t, authz.AccessSurfaceKindRoute, activities.Kind)
	require.Empty(t, activities.URL)
	require.Equal(t, authz.AccessScopeModeAnyEffectiveScope, activities.ScopeMode)
	require.ElementsMatch(t, []string{authz.PermActivitiesList, authz.PermActivitiesRead}, activities.Permissions)

	oidcRoleMappings := requireAccessSurface(t, "route.oidc-role-mappings")
	require.Equal(t, authz.AccessSurfaceKindRoute, oidcRoleMappings.Kind)
	require.Empty(t, oidcRoleMappings.URL)
	require.Equal(t, authz.AccessScopeModeGlobalOnly, oidcRoleMappings.ScopeMode)
	require.Equal(t, authz.AccessMatchModeAllOf, oidcRoleMappings.MatchMode)
	require.ElementsMatch(t, authz.AllPermissions(), oidcRoleMappings.Permissions)

	settingsLanding := requireAccessSurface(t, "landing.settings")
	require.Equal(t, authz.AccessSurfaceKindLanding, settingsLanding.Kind)
	require.Equal(t, "/settings", settingsLanding.URL)
	require.Equal(t, authz.AccessModeAnyChild, settingsLanding.AccessMode)
	require.Contains(t, settingsLanding.Children, "settings.category.docker")
	require.NotContains(t, settingsLanding.Children, "settings.category.webhooks")

	systemLanding := requireAccessSurface(t, "landing.system")
	require.Equal(t, authz.AccessSurfaceKindLanding, systemLanding.Kind)
	require.Equal(t, "/system", systemLanding.URL)
	require.Equal(t, authz.AccessModeAnyChild, systemLanding.AccessMode)
	require.Contains(t, systemLanding.Children, "settings.category.webhooks")
	require.Contains(t, systemLanding.Children, "settings.category.apikeys")
}

func TestCanAccessSurfaceEvaluatesScopeModesAndLandingChildren(t *testing.T) {
	ps := authz.NewPermissionSet()
	ps.AddEnv("env-a", authz.PermWebhooksList)

	require.True(t, authz.CanAccessSurface(ps, "settings.category.webhooks", "env-a"))
	require.True(t, authz.CanAccessSurface(ps, "landing.system", "env-a"))
	require.False(t, authz.CanAccessSurface(ps, "settings.category.webhooks", "env-b"))
	require.False(t, authz.CanAccessSurface(ps, "landing.system", "env-b"))
	require.False(t, authz.CanAccessSurface(ps, "landing.settings", "env-a"))

	ps.AddGlobal(authz.PermApiKeysRead)
	require.True(t, authz.CanAccessSurface(ps, "settings.category.apikeys", "env-b"))
	require.True(t, authz.CanAccessSurface(ps, "landing.system", "env-b"))

	notificationPS := authz.NewPermissionSet()
	notificationPS.AddEnv("env-a", authz.PermNotificationsList)
	require.False(t, authz.CanAccessSurface(notificationPS, "settings.category.notifications", "env-a"))
	notificationPS.AddGlobal(authz.PermNotificationsList)
	require.True(t, authz.CanAccessSurface(notificationPS, "settings.category.notifications", "env-a"))

	registryPS := authz.NewPermissionSet()
	registryPS.AddGlobal(authz.PermRegistriesRead)
	require.True(t, authz.CanAccessSurface(registryPS, "route.images.registries", "env-a"))
	require.Equal(t, 41, requireAccessSurface(t, "route.images.registries").FallbackOrder)
	require.False(t, authz.CanAccessSurface(registryPS, "route.projects.templates", "env-a"))

	variablePS := authz.NewPermissionSet()
	variablePS.AddGlobal(authz.PermVariablesList)
	require.True(t, authz.CanAccessSurface(variablePS, "route.projects.variables", "env-a"))
	require.Equal(t, 42, requireAccessSurface(t, "route.projects.variables").FallbackOrder)
	templatePS := authz.NewPermissionSet()
	templatePS.AddGlobal(authz.PermTemplatesRead)
	require.False(t, authz.CanAccessSurface(templatePS, "route.projects.variables", "env-a"))

	activitiesPS := authz.NewPermissionSet()
	activitiesPS.AddEnv("env-a", authz.PermActivitiesList)
	require.True(t, authz.CanAccessSurface(activitiesPS, "route.activities", "env-b"))
	require.False(t, authz.CanAccessSurface(authz.NewPermissionSet(), "route.activities", "env-a"))

	globalAdminPS := authz.NewPermissionSet()
	globalAdminPS.AddGlobal(authz.AllPermissions()...)
	require.True(t, authz.CanAccessSurface(globalAdminPS, "route.oidc-role-mappings", "env-a"))
	require.True(t, authz.CanAccessSurface(authz.SudoPermissionSet(), "route.oidc-role-mappings", "env-a"))

	nonAdminPS := authz.NewPermissionSet()
	nonAdminPS.AddGlobal(authz.BuiltInEditorPermissions()...)
	require.False(t, authz.CanAccessSurface(nonAdminPS, "route.oidc-role-mappings", "env-a"))

	for _, id := range []string{"route.settings.roles.new", "route.settings.roles.detail"} {
		require.True(t, authz.CanAccessSurface(globalAdminPS, id, "env-a"), "global admin must reach %s", id)
		require.False(t, authz.CanAccessSurface(nonAdminPS, id, "env-a"), "non-admin must not reach %s", id)
	}

	backupsPS := authz.NewPermissionSet()
	backupsPS.AddGlobal(authz.PermSystemBackupsList)
	require.True(t, authz.CanAccessSurface(backupsPS, "settings.category.systembackups", "env-a"))
	require.False(t, authz.CanAccessSurface(nonAdminPS, "settings.category.systembackups", "env-a"))

	environmentAdminPS := authz.NewPermissionSet()
	environmentAdminPS.AddEnv("env-a", authz.AllPermissions()...)
	require.False(t, authz.CanAccessSurface(environmentAdminPS, "route.oidc-role-mappings", "env-a"))

	require.False(t, authz.CanAccessSurface(authz.NewPermissionSet(), "missing.surface", "env-a"))
}

func TestCanAccessSurfaceGatesSettingsCategoriesByScope(t *testing.T) {
	envSettingsPS := authz.NewPermissionSet()
	envSettingsPS.AddEnv("env-a", authz.PermEnvironmentSettingsRead)
	for _, category := range []string{"storage", "docker", "security", "timeouts", "activity", "build"} {
		require.True(t, authz.CanAccessSurface(envSettingsPS, "settings.category."+category, "env-a"), category)
		require.False(t, authz.CanAccessSurface(envSettingsPS, "settings.category."+category, "env-b"), category)
	}
	require.True(t, authz.CanAccessSurface(envSettingsPS, "landing.settings", "env-a"))
	require.False(t, authz.CanAccessSurface(envSettingsPS, "settings.category.jobschedule", "env-a"))
	envSettingsPS.AddEnv("env-a", authz.PermJobsList)
	require.True(t, authz.CanAccessSurface(envSettingsPS, "settings.category.jobschedule", "env-a"))

	jobsOnlyPS := authz.NewPermissionSet()
	jobsOnlyPS.AddGlobal(authz.PermJobsList, authz.PermJobsRead)
	require.False(t, authz.CanAccessSurface(jobsOnlyPS, "settings.category.jobschedule", "env-a"))

	managerSettingsPS := authz.NewPermissionSet()
	managerSettingsPS.AddGlobal(authz.PermSettingsRead, authz.PermSettingsUpdate)
	require.False(t, authz.CanAccessSurface(managerSettingsPS, "settings.category.docker", "env-a"))
	require.False(t, authz.CanAccessSurface(managerSettingsPS, "settings.category.authentication", ""))

	editorPS := authz.NewPermissionSet()
	editorPS.AddGlobal(authz.BuiltInEditorPermissions()...)
	require.True(t, authz.CanAccessSurface(editorPS, "settings.category.docker", "env-a"))
	require.False(t, authz.CanAccessSurface(editorPS, "settings.category.authentication", ""))

	globalAdminPS := authz.NewPermissionSet()
	globalAdminPS.AddGlobal(authz.AllPermissions()...)
	require.True(t, globalAdminPS.IsGlobalAdmin())
	require.True(t, authz.CanAccessSurface(globalAdminPS, "settings.category.authentication", ""))
	require.True(t, authz.CanAccessSurface(authz.SudoPermissionSet(), "settings.category.authentication", ""))

	environmentAdminPS := authz.NewPermissionSet()
	environmentAdminPS.AddEnv("env-a", authz.AllPermissions()...)
	require.False(t, authz.CanAccessSurface(environmentAdminPS, "settings.category.authentication", "env-a"))
}

func TestCanAccessSurfaceGatesCreateAndEditRoutesOnTheirMutatingPermission(t *testing.T) {
	tests := []struct {
		surfaceID  string
		permission string
		global     bool
	}{
		{surfaceID: "route.projects.new", permission: authz.PermProjectsCreate},
		{surfaceID: "route.containers.new", permission: authz.PermContainersCreate},
		{surfaceID: "route.containers.edit", permission: authz.PermContainersUpdate},
		{surfaceID: "route.podman.pods.detail", permission: authz.PermPodmanPodsRead},
		{surfaceID: "route.podman.quadlets.detail", permission: authz.PermPodmanQuadletsRead},
		{surfaceID: "route.projects.templates.create", permission: authz.PermTemplatesCreate, global: true},
		{surfaceID: "route.projects.templates.default", permission: authz.PermTemplatesUpdate, global: true},
	}

	viewerPS := authz.NewPermissionSet()
	viewerPS.AddGlobal(authz.BuiltInViewerPermissions()...)

	for _, tt := range tests {
		t.Run(tt.surfaceID, func(t *testing.T) {
			envGrant := authz.NewPermissionSet()
			envGrant.AddEnv("env-a", tt.permission)
			globalGrant := authz.NewPermissionSet()
			globalGrant.AddGlobal(tt.permission)

			require.True(t, authz.CanAccessSurface(globalGrant, tt.surfaceID, "env-b"))
			require.Equal(t, !tt.global, authz.CanAccessSurface(envGrant, tt.surfaceID, "env-a"))
			require.False(t, authz.CanAccessSurface(envGrant, tt.surfaceID, "env-b"))
			require.Equal(t, tt.permission == authz.PermPodmanPodsRead || tt.permission == authz.PermPodmanQuadletsRead, authz.CanAccessSurface(viewerPS, tt.surfaceID, "env-a"))
		})
	}
}

func TestAccessSurfaceRegistryIsDefensiveAndInternallyConsistent(t *testing.T) {
	surfaces := authz.AccessSurfaces()
	require.NotEmpty(t, surfaces)

	surfaces[0].Permissions = append(surfaces[0].Permissions, "unknown:permission")
	surfaces[0].Children = append(surfaces[0].Children, "missing.child")

	fresh := authz.AccessSurfaces()
	require.NotContains(t, fresh[0].Permissions, "unknown:permission")
	require.NotContains(t, fresh[0].Children, "missing.child")

	for _, surface := range fresh {
		for _, perm := range surface.Permissions {
			require.True(t, authz.IsKnownPermission(perm), "surface %s references unknown permission %s", surface.ID, perm)
		}
		for _, childID := range surface.Children {
			_, ok := findAccessSurface(childID)
			require.True(t, ok, "surface %s references unknown child %s", surface.ID, childID)
		}
	}
}

func TestAccessSurfaceCategoryRegistryCoversBackendCategories(t *testing.T) {
	for _, category := range settings.NewSettingsSearchService().GetSettingsCategories() {
		_, ok := findAccessSurface("settings.category." + category.ID)
		require.True(t, ok, "settings category %s must have an access surface", category.ID)
	}
}

func TestPublishedAccessSurfaceURLsHaveFrontendRoutes(t *testing.T) {
	routesRoot := filepath.Join(repoRoot(t), "frontend", "src", "routes", "(app)")

	for _, surface := range authz.AccessSurfaces() {
		if surface.URL == "" {
			continue
		}
		routePath := frontendRoutePath(routesRoot, surface.URL)
		_, err := os.Stat(routePath)
		require.NoError(t, err, "access surface %s URL %s must map to frontend route %s", surface.ID, surface.URL, routePath)
	}
}

func TestFrontendNavigationReferencesKnownAccessSurfaces(t *testing.T) {
	navPath := filepath.Join(repoRoot(t), "frontend", "src", "lib", "config", "navigation-config.ts")
	body, err := os.ReadFile(navPath)
	require.NoError(t, err)

	matches := regexp.MustCompile(`accessSurfaceId:\s*'([^']+)'`).FindAllStringSubmatch(string(body), -1)
	require.NotEmpty(t, matches)

	for _, match := range matches {
		require.Len(t, match, 2)
		_, ok := findAccessSurface(match[1])
		require.True(t, ok, "frontend navigation references unknown access surface %s", match[1])
	}
}

func requireAccessSurface(t *testing.T, id string) authz.AccessSurface {
	t.Helper()

	surface, ok := findAccessSurface(id)
	require.True(t, ok, "expected access surface %s to exist", id)

	return surface
}

func findAccessSurface(id string) (authz.AccessSurface, bool) {
	for _, surface := range authz.AccessSurfaces() {
		if surface.ID == id {
			return surface, true
		}
	}
	return authz.AccessSurface{}, false
}

func repoRoot(t *testing.T) string {
	t.Helper()

	// go test runs in the package directory; runtime.Caller breaks under -trimpath.
	wd, err := os.Getwd()
	require.NoError(t, err)
	return filepath.Clean(filepath.Join(wd, "..", "..", ".."))
}

func frontendRoutePath(routesRoot, url string) string {
	parts := strings.Split(strings.Trim(url, "/"), "/")
	for i, part := range parts {
		if strings.HasPrefix(part, "{") && strings.HasSuffix(part, "}") {
			parts[i] = "[" + strings.Trim(part, "{}") + "]"
			continue
		}
		if after, ok := strings.CutPrefix(part, ":"); ok {
			parts[i] = "[" + after + "]"
		}
	}
	return filepath.Join(append([]string{routesRoot}, parts...)...)
}
