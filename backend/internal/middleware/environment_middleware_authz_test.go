package middleware

import (
	"bytes"
	"encoding/json/v2"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/labstack/echo/v5"

	"github.com/getarcaneapp/arcane/backend/v2/pkg/authz"
	volumetypes "github.com/getarcaneapp/arcane/types/v2/volume"
	"github.com/stretchr/testify/require"
)

const proxyTestEnvID = "remote-1"

func newProxyAuthzMiddleware(matcher *authz.PermissionMatcher) *EnvironmentMiddleware {
	return &EnvironmentMiddleware{localID: "0", paramName: "id", matcher: matcher}
}

func newProxyRequestContext(method, path string) *echo.Context {
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

	require.True(t, m.proxyPermissionDenied(c, ps, proxyTestEnvID),
		"expected restart to be denied for a read-only user")

}

func TestProxyPermissionDeniedAllowsWriteForPermittedUser(t *testing.T) {
	m := newProxyAuthzMiddleware(containerMatcher())
	ps := authz.NewPermissionSet()
	ps.AddEnv(proxyTestEnvID, authz.PermContainersRestart)

	c := newProxyRequestContext(http.MethodPost, "/api/environments/"+proxyTestEnvID+"/containers/abc/restart")

	require.False(t, m.proxyPermissionDenied(c, ps, proxyTestEnvID),
		"expected restart to be allowed for a user with containers:restart")

}

func TestProxyPermissionDeniedAllowsRead(t *testing.T) {
	m := newProxyAuthzMiddleware(containerMatcher())
	ps := authz.NewPermissionSet()
	ps.AddEnv(proxyTestEnvID, authz.PermContainersList)

	c := newProxyRequestContext(http.MethodGet, "/api/environments/"+proxyTestEnvID+"/containers")

	require.False(t, m.proxyPermissionDenied(c, ps, proxyTestEnvID),
		"expected list to be allowed for a user with containers:list")

}

func TestProxyPermissionDeniedDeniesPermissionFromDifferentEnv(t *testing.T) {
	m := newProxyAuthzMiddleware(containerMatcher())
	// Caller holds containers:restart, but only for a DIFFERENT environment.
	ps := authz.NewPermissionSet()
	ps.AddEnv("other-env", authz.PermContainersRestart)

	c := newProxyRequestContext(http.MethodPost, "/api/environments/"+proxyTestEnvID+"/containers/abc/restart")

	require.True(t, m.proxyPermissionDenied(c, ps, proxyTestEnvID),
		"expected denial: permission is scoped to a different environment")

}

func TestProxyPermissionDeniedSudoBypasses(t *testing.T) {
	m := newProxyAuthzMiddleware(containerMatcher())
	c := newProxyRequestContext(http.MethodPost, "/api/environments/"+proxyTestEnvID+"/containers/abc/restart")

	require.False(t, m.proxyPermissionDenied(c, authz.SudoPermissionSet(), proxyTestEnvID),
		"expected sudo permission set to bypass the permission check")

}

func TestProxyPermissionDeniedDefaultDeniesUnmappedRoute(t *testing.T) {
	m := newProxyAuthzMiddleware(containerMatcher())
	ps := authz.NewPermissionSet()
	ps.AddEnv(proxyTestEnvID, authz.PermContainersRestart, authz.PermContainersList)

	c := newProxyRequestContext(http.MethodPost, "/api/environments/"+proxyTestEnvID+"/unknown/resource")

	require.True(t, m.proxyPermissionDenied(c, ps, proxyTestEnvID),
		"expected an unmapped proxied route to be denied by default")

}

func TestProxyPermissionDeniedAllowsPublicRoute(t *testing.T) {
	m := newProxyAuthzMiddleware(containerMatcher())
	ps := authz.NewPermissionSet() // no permissions at all

	c := newProxyRequestContext(http.MethodGet, "/api/environments/"+proxyTestEnvID+"/settings/public")

	require.False(t, m.proxyPermissionDenied(c, ps, proxyTestEnvID),
		"expected an explicitly public route to be allowed for any authenticated caller")

}

func volumeWorkspaceMatcher() *authz.PermissionMatcher {
	m := authz.NewPermissionMatcher()
	m.Add(http.MethodPut, "/volumes/{volumeName}/workspace", authz.PermVolumesRead)
	return m
}

func newProxyVolumeWorkspaceContext(t *testing.T, changes []volumetypes.WorkspaceFileChange, fileFirst ...bool) (*echo.Context, []byte) {
	t.Helper()
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	if len(fileFirst) == 1 && fileFirst[0] {
		filePart, err := writer.CreateFormFile("files", "example.txt")
		require.NoError(t, err)
		_, err = filePart.Write([]byte("content"))
		require.NoError(t, err)
	}
	manifestPart, err := writer.CreateFormField("manifest")
	require.NoError(t, err)
	manifestJSON, err := json.Marshal(volumetypes.WorkspaceUpdateManifest{FileTreeRevision: "revision", FileChanges: changes})
	require.NoError(t, err)
	_, err = manifestPart.Write(manifestJSON)
	require.NoError(t, err)
	require.NoError(t, writer.Close())

	rawBody := append([]byte(nil), body.Bytes()...)
	e := echo.New()
	req := httptest.NewRequest(http.MethodPut, "/api/environments/"+proxyTestEnvID+"/volumes/data/workspace", bytes.NewReader(rawBody))
	req.Header.Set("Content-Type", writer.FormDataContentType())
	t.Cleanup(func() { _ = req.Body.Close() })
	return e.NewContext(req, httptest.NewRecorder()), rawBody
}

func TestProxyPermissionDeniedChecksVolumeWorkspaceManifestPermissions(t *testing.T) {
	m := newProxyAuthzMiddleware(volumeWorkspaceMatcher())
	changes := []volumetypes.WorkspaceFileChange{{Operation: volumetypes.FileOpRename}}

	readOnly := authz.NewPermissionSet()
	readOnly.AddEnv(proxyTestEnvID, authz.PermVolumesRead)
	c, _ := newProxyVolumeWorkspaceContext(t, changes)
	require.True(t, m.proxyPermissionDenied(c, readOnly, proxyTestEnvID))

	uploadOnly := authz.NewPermissionSet()
	uploadOnly.AddEnv(proxyTestEnvID, authz.PermVolumesRead, authz.PermVolumesUpload)
	c, _ = newProxyVolumeWorkspaceContext(t, changes)
	require.True(t, m.proxyPermissionDenied(c, uploadOnly, proxyTestEnvID))

	permitted := authz.NewPermissionSet()
	permitted.AddEnv(proxyTestEnvID, authz.PermVolumesRead, authz.PermVolumesUpload, authz.PermVolumesDelete)
	c, rawBody := newProxyVolumeWorkspaceContext(t, changes)
	require.False(t, m.proxyPermissionDenied(c, permitted, proxyTestEnvID))
	replayed, err := io.ReadAll(c.Request().Body)
	require.NoError(t, err)
	require.Equal(t, rawBody, replayed)

	c, rawBody = newProxyVolumeWorkspaceContext(t, changes, true)
	require.False(t, m.proxyPermissionDenied(c, permitted, proxyTestEnvID))
	replayed, err = io.ReadAll(c.Request().Body)
	require.NoError(t, err)
	require.Equal(t, rawBody, replayed)
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

	require.True(t, m.proxyPermissionDenied(c, ps, proxyTestEnvID),
		"expected WS terminal to be denied without containers:exec")

	// Granting containers:exec allows the same stream.
	ps.AddEnv(proxyTestEnvID, authz.PermContainersExec)

	require.False(t, m.proxyPermissionDenied(c, ps, proxyTestEnvID),
		"expected WS terminal to be allowed with containers:exec")

}
