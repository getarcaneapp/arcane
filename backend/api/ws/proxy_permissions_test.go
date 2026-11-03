package ws

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"

	"getarcane.dev/arcane/v3/pkg/authz"
)

func TestAddProxiedPermissionsMatchesRouteTable(t *testing.T) {
	matcher := authz.NewPermissionMatcher()
	AddProxiedPermissions(matcher)

	perm, ok := matcher.Lookup(http.MethodGet, "/ws/containers/abc/terminal", nil).Get()
	require.True(t, ok)
	require.Equal(t, authz.PermContainersExec, perm)
	require.True(t, matcher.Lookup(http.MethodGet, "/diagnostics/stream", nil).IsAbsent())
}

func TestRoutesDeclareDiagnosticsPermissions(t *testing.T) {
	perms := map[string]string{}
	var streamConditions authz.PermissionConditions
	for _, route := range Routes() {
		perms[route.Method+" "+route.Path] = route.Permission
		if route.Path == "/diagnostics/stream" {
			streamConditions = route.Conditions
		}
	}

	require.Equal(t, authz.PermDiagnosticsRead, perms["GET /diagnostics/logs/stream"])
	require.Equal(t, authz.PermProjectsLogs, perms["GET /environments/:id/ws/projects/:projectId/logs"])
	require.Equal(t, []string{authz.PermDiagnosticsProfile}, streamConditions.Required("type", "dump"))
	require.Empty(t, streamConditions.Required("type", "leakScan"))
}
