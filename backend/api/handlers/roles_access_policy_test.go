package handlers

import (
	"testing"

	"github.com/getarcaneapp/arcane/backend/pkg/authz"
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
