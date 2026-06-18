package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/labstack/echo/v4"

	"github.com/getarcaneapp/arcane/backend/v2/pkg/authz"
)

const proxyTestEnvID = "remote-1"

func newProxyAuthzMiddleware(matcher *authz.PermissionMatcher) *EnvironmentMiddleware {
	return &EnvironmentMiddleware{localID: "0", paramName: "id", matcher: matcher}
}

func newProxyRequestContext(method, path string) echo.Context {
	e := echo.New()
	req := httptest.NewRequest(method, path, nil)
	return e.NewContext(req, httptest.NewRecorder())
}

func containerMatcher() *authz.PermissionMatcher {
	m := authz.NewPermissionMatcher()
	m.Add(http.MethodGet, "/containers", authz.PermContainersList)
	m.Add(http.MethodPost, "/containers/{containerId}/restart", authz.PermContainersRestart)
	m.AddPublic(http.MethodGet, "/settings/public")
	return m
}

func TestProxyPermissionDeniedBlocksWriteForReadOnlyUser(t *testing.T) {
	m := newProxyAuthzMiddleware(containerMatcher())
	ps := authz.NewPermissionSet()
	ps.AddEnv(proxyTestEnvID, authz.PermContainersList, authz.PermContainersRead)

	c := newProxyRequestContext(http.MethodPost, "/api/environments/"+proxyTestEnvID+"/containers/abc/restart")
	if !m.proxyPermissionDenied(c, ps, proxyTestEnvID) {
		t.Fatal("expected restart to be denied for a read-only user")
	}
}

func TestProxyPermissionDeniedAllowsWriteForPermittedUser(t *testing.T) {
	m := newProxyAuthzMiddleware(containerMatcher())
	ps := authz.NewPermissionSet()
	ps.AddEnv(proxyTestEnvID, authz.PermContainersRestart)

	c := newProxyRequestContext(http.MethodPost, "/api/environments/"+proxyTestEnvID+"/containers/abc/restart")
	if m.proxyPermissionDenied(c, ps, proxyTestEnvID) {
		t.Fatal("expected restart to be allowed for a user with containers:restart")
	}
}

func TestProxyPermissionDeniedAllowsRead(t *testing.T) {
	m := newProxyAuthzMiddleware(containerMatcher())
	ps := authz.NewPermissionSet()
	ps.AddEnv(proxyTestEnvID, authz.PermContainersList)

	c := newProxyRequestContext(http.MethodGet, "/api/environments/"+proxyTestEnvID+"/containers")
	if m.proxyPermissionDenied(c, ps, proxyTestEnvID) {
		t.Fatal("expected list to be allowed for a user with containers:list")
	}
}

func TestProxyPermissionDeniedDeniesPermissionFromDifferentEnv(t *testing.T) {
	m := newProxyAuthzMiddleware(containerMatcher())
	// Caller holds containers:restart, but only for a DIFFERENT environment.
	ps := authz.NewPermissionSet()
	ps.AddEnv("other-env", authz.PermContainersRestart)

	c := newProxyRequestContext(http.MethodPost, "/api/environments/"+proxyTestEnvID+"/containers/abc/restart")
	if !m.proxyPermissionDenied(c, ps, proxyTestEnvID) {
		t.Fatal("expected denial: permission is scoped to a different environment")
	}
}

func TestProxyPermissionDeniedSudoBypasses(t *testing.T) {
	m := newProxyAuthzMiddleware(containerMatcher())
	c := newProxyRequestContext(http.MethodPost, "/api/environments/"+proxyTestEnvID+"/containers/abc/restart")
	if m.proxyPermissionDenied(c, authz.SudoPermissionSet(), proxyTestEnvID) {
		t.Fatal("expected sudo permission set to bypass the permission check")
	}
}

func TestProxyPermissionDeniedDefaultDeniesUnmappedRoute(t *testing.T) {
	m := newProxyAuthzMiddleware(containerMatcher())
	ps := authz.NewPermissionSet()
	ps.AddEnv(proxyTestEnvID, authz.PermContainersRestart, authz.PermContainersList)

	c := newProxyRequestContext(http.MethodPost, "/api/environments/"+proxyTestEnvID+"/unknown/resource")
	if !m.proxyPermissionDenied(c, ps, proxyTestEnvID) {
		t.Fatal("expected an unmapped proxied route to be denied by default")
	}
}

func TestProxyPermissionDeniedAllowsPublicRoute(t *testing.T) {
	m := newProxyAuthzMiddleware(containerMatcher())
	ps := authz.NewPermissionSet() // no permissions at all

	c := newProxyRequestContext(http.MethodGet, "/api/environments/"+proxyTestEnvID+"/settings/public")
	if m.proxyPermissionDenied(c, ps, proxyTestEnvID) {
		t.Fatal("expected an explicitly public route to be allowed for any authenticated caller")
	}
}

// wsTerminalMatcher mirrors ws.AddProxiedPermissions for the container terminal
// stream: the proxy computes the suffix "/ws/containers/{id}/terminal" for a
// forwarded WebSocket request, and the matcher requires containers:exec for it.
func wsTerminalMatcher() *authz.PermissionMatcher {
	m := authz.NewPermissionMatcher()
	m.Add(http.MethodGet, "/ws/containers/{containerId}/terminal", authz.PermContainersExec)
	return m
}

func TestProxyPermissionDeniedWSTerminalRequiresExec(t *testing.T) {
	m := newProxyAuthzMiddleware(wsTerminalMatcher())

	// A caller who can read and list containers but lacks containers:exec must
	// not be able to open a terminal stream on the remote environment.
	ps := authz.NewPermissionSet()
	ps.AddEnv(proxyTestEnvID, authz.PermContainersRead, authz.PermContainersList)

	c := newProxyRequestContext(http.MethodGet, "/api/environments/"+proxyTestEnvID+"/ws/containers/abc/terminal")
	if !m.proxyPermissionDenied(c, ps, proxyTestEnvID) {
		t.Fatal("expected WS terminal to be denied without containers:exec")
	}

	// Granting containers:exec allows the same stream.
	ps.AddEnv(proxyTestEnvID, authz.PermContainersExec)
	if m.proxyPermissionDenied(c, ps, proxyTestEnvID) {
		t.Fatal("expected WS terminal to be allowed with containers:exec")
	}
}
