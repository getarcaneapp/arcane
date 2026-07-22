package handlers

import (
	"testing"

	"github.com/getarcaneapp/arcane/backend/v2/pkg/authz"
	"github.com/stretchr/testify/require"
)

func TestBuildPermissionsManifestIncludesAccessSurfaces(t *testing.T) {
	manifest := buildPermissionsManifestInternal()

	require.NotEmpty(t, manifest.Resources)
	require.NotEmpty(t, manifest.AccessSurfaces)

	surfacesByID := make(map[string]struct{}, len(manifest.AccessSurfaces))
	for _, surface := range manifest.AccessSurfaces {
		surfacesByID[surface.ID] = struct{}{}
	}

	for _, id := range []string{
		"landing.settings",
		"landing.customize",
		"settings.category.webhooks",
		"customize.category.templates",
		"route.dashboard",
	} {
		_, ok := surfacesByID[id]
		require.True(t, ok, "expected manifest to include access surface %s", id)
	}

	registrySurfaces := authz.AccessSurfaces()
	require.Len(t, manifest.AccessSurfaces, len(registrySurfaces))
}

func TestBuildPermissionsManifestIncludesEventsDelete(t *testing.T) {
	manifest := buildPermissionsManifestInternal()

	for _, resource := range manifest.Resources {
		if resource.Key != "events" {
			continue
		}
		for _, action := range resource.Actions {
			if action.Permission == authz.PermEventsDelete {
				return
			}
		}
		t.Fatalf("events resource did not include %s", authz.PermEventsDelete)
	}

	t.Fatal("events resource not found")
}

func TestBuildPermissionsManifestKeepsVariablesSeparateFromTemplates(t *testing.T) {
	manifest := buildPermissionsManifestInternal()

	var variablePermissions []string
	for _, resource := range manifest.Resources {
		if resource.Key != "variables" {
			continue
		}
		for _, action := range resource.Actions {
			variablePermissions = append(variablePermissions, action.Permission)
		}
	}
	require.ElementsMatch(t, []string{
		authz.PermVariablesRead,
		authz.PermVariablesCreate,
		authz.PermVariablesUpdate,
		authz.PermVariablesDelete,
		authz.PermVariablesSync,
	}, variablePermissions)
	for _, permission := range variablePermissions {
		require.NotEqual(t, authz.PermTemplatesRead, permission)
	}

	surfaces := make(map[string][]string, len(manifest.AccessSurfaces))
	for _, surface := range manifest.AccessSurfaces {
		surfaces[surface.ID] = surface.Permissions
	}
	require.Equal(t, []string{authz.PermVariablesRead}, surfaces["customize.category.variables"])
	require.Equal(t, []string{authz.PermActivitiesRead}, surfaces["route.activities"])
	require.ElementsMatch(t, authz.AllPermissions(), surfaces["route.oidc-role-mappings"])
}
