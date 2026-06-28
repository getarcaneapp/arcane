package authz_test

import (
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"testing"

	"github.com/getarcaneapp/arcane/backend/v2/internal/services"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/authz"
	"github.com/stretchr/testify/require"
)

func TestAccessSurfaceRegistryDefinesSettingsCustomizeAndLandingSemantics(t *testing.T) {
	webhooks := requireAccessSurfaceInternal(t, "settings.category.webhooks")
	require.Equal(t, authz.AccessSurfaceKindSettingsCategory, webhooks.Kind)
	require.Equal(t, "/settings/webhooks", webhooks.URL)
	require.Equal(t, authz.AccessModePermissions, webhooks.AccessMode)
	require.Equal(t, authz.AccessMatchModeAnyOf, webhooks.MatchMode)
	require.Equal(t, authz.AccessScopeModeSelectedEnvPlusGlobal, webhooks.ScopeMode)
	require.ElementsMatch(t, []string{authz.PermWebhooksList}, webhooks.Permissions)

	apiKeys := requireAccessSurfaceInternal(t, "settings.category.apikeys")
	require.Equal(t, authz.AccessScopeModeGlobalOnly, apiKeys.ScopeMode)
	require.ElementsMatch(t, []string{authz.PermApiKeysList, authz.PermApiKeysRead}, apiKeys.Permissions)

	jobSchedule := requireAccessSurfaceInternal(t, "settings.category.jobschedule")
	require.Equal(t, authz.AccessScopeModeSelectedEnvPlusGlobal, jobSchedule.ScopeMode)
	require.Empty(t, jobSchedule.URL)
	require.ElementsMatch(t, []string{authz.PermJobsManage}, jobSchedule.Permissions)

	templates := requireAccessSurfaceInternal(t, "customize.category.templates")
	require.Equal(t, authz.AccessSurfaceKindCustomizeCategory, templates.Kind)
	require.Equal(t, authz.AccessScopeModeGlobalOnly, templates.ScopeMode)
	require.ElementsMatch(t, []string{authz.PermCustomizeManage, authz.PermTemplatesList, authz.PermTemplatesRead}, templates.Permissions)

	settingsLanding := requireAccessSurfaceInternal(t, "landing.settings")
	require.Equal(t, authz.AccessSurfaceKindLanding, settingsLanding.Kind)
	require.Equal(t, "/settings", settingsLanding.URL)
	require.Equal(t, authz.AccessModeAnyChild, settingsLanding.AccessMode)
	require.Contains(t, settingsLanding.Children, "settings.category.webhooks")
	require.Contains(t, settingsLanding.Children, "settings.category.apikeys")
	require.Contains(t, settingsLanding.Children, "settings.category.jobschedule")

	dashboard := requireAccessSurfaceInternal(t, "route.dashboard")
	require.Equal(t, authz.AccessSurfaceKindRoute, dashboard.Kind)
	require.Equal(t, "/dashboard", dashboard.URL)
	require.Positive(t, dashboard.FallbackOrder)
}

func TestCanAccessSurfaceEvaluatesScopeModesAndLandingChildren(t *testing.T) {
	ps := authz.NewPermissionSet()
	ps.AddEnv("env-a", authz.PermWebhooksList)

	require.True(t, authz.CanAccessSurface(ps, "settings.category.webhooks", "env-a"))
	require.True(t, authz.CanAccessSurface(ps, "landing.settings", "env-a"))
	require.False(t, authz.CanAccessSurface(ps, "settings.category.webhooks", "env-b"))
	require.False(t, authz.CanAccessSurface(ps, "landing.settings", "env-b"))

	ps.AddGlobal(authz.PermApiKeysRead)
	require.True(t, authz.CanAccessSurface(ps, "settings.category.apikeys", "env-b"))
	require.True(t, authz.CanAccessSurface(ps, "landing.settings", "env-b"))

	jobsPS := authz.NewPermissionSet()
	jobsPS.AddEnv("env-a", authz.PermJobsManage)
	require.True(t, authz.CanAccessSurface(jobsPS, "settings.category.jobschedule", "env-a"))
	require.True(t, authz.CanAccessSurface(jobsPS, "landing.settings", "env-a"))
	require.False(t, authz.CanAccessSurface(jobsPS, "settings.category.jobschedule", "env-b"))

	customizePS := authz.NewPermissionSet()
	customizePS.AddGlobal(authz.PermCustomizeManage)
	require.True(t, authz.CanAccessSurface(customizePS, "customize.category.registries", "env-a"))
	require.True(t, authz.CanAccessSurface(customizePS, "landing.customize", "env-a"))

	require.False(t, authz.CanAccessSurface(authz.NewPermissionSet(), "missing.surface", "env-a"))
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
			_, ok := findAccessSurfaceInternal(childID)
			require.True(t, ok, "surface %s references unknown child %s", surface.ID, childID)
		}
	}
}

func TestAccessSurfaceCategoryRegistryCoversBackendCategories(t *testing.T) {
	hiddenSettingsCategories := map[string]struct{}{
		"security": {},
	}

	for _, category := range services.NewSettingsSearchService().GetSettingsCategories() {
		if _, hidden := hiddenSettingsCategories[category.ID]; hidden {
			continue
		}
		_, ok := findAccessSurfaceInternal("settings.category." + category.ID)
		require.True(t, ok, "settings category %s must have an access surface", category.ID)
	}

	for _, category := range services.NewCustomizeSearchService().GetCustomizeCategories() {
		_, ok := findAccessSurfaceInternal("customize.category." + category.ID)
		require.True(t, ok, "customize category %s must have an access surface", category.ID)
	}
}

func TestPublishedAccessSurfaceURLsHaveFrontendRoutes(t *testing.T) {
	routesRoot := filepath.Join(repoRootInternal(t), "frontend", "src", "routes", "(app)")

	for _, surface := range authz.AccessSurfaces() {
		if surface.URL == "" {
			continue
		}
		routePath := frontendRoutePathInternal(routesRoot, surface.URL)
		_, err := os.Stat(routePath)
		require.NoError(t, err, "access surface %s URL %s must map to frontend route %s", surface.ID, surface.URL, routePath)
	}
}

func TestFrontendNavigationReferencesKnownAccessSurfaces(t *testing.T) {
	navPath := filepath.Join(repoRootInternal(t), "frontend", "src", "lib", "config", "navigation-config.ts")
	body, err := os.ReadFile(navPath)
	require.NoError(t, err)

	matches := regexp.MustCompile(`accessSurfaceId:\s*'([^']+)'`).FindAllStringSubmatch(string(body), -1)
	require.NotEmpty(t, matches)

	for _, match := range matches {
		require.Len(t, match, 2)
		_, ok := findAccessSurfaceInternal(match[1])
		require.True(t, ok, "frontend navigation references unknown access surface %s", match[1])
	}
}

func requireAccessSurfaceInternal(t *testing.T, id string) authz.AccessSurface {
	t.Helper()

	surface, ok := findAccessSurfaceInternal(id)
	require.True(t, ok, "expected access surface %s to exist", id)

	return surface
}

func findAccessSurfaceInternal(id string) (authz.AccessSurface, bool) {
	for _, surface := range authz.AccessSurfaces() {
		if surface.ID == id {
			return surface, true
		}
	}
	return authz.AccessSurface{}, false
}

func repoRootInternal(t *testing.T) string {
	t.Helper()

	_, filename, _, ok := runtime.Caller(0)
	require.True(t, ok)
	return filepath.Clean(filepath.Join(filepath.Dir(filename), "..", "..", ".."))
}

func frontendRoutePathInternal(routesRoot, url string) string {
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
