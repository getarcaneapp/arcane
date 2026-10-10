package middleware

import (
	"bytes"
	"context"
	"encoding/json/v2"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/getarcaneapp/arcane/types/v2/user"
	"github.com/getarcaneapp/arcane/types/v2/volume"
	"github.com/labstack/echo/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/getarcaneapp/arcane/backend/v2/pkg/authz"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/libarcane/edge"
)

func newTestEnvironmentMiddleware() *EnvironmentMiddleware {
	return &EnvironmentMiddleware{
		localID:   "0",
		paramName: "id",
		resolver: func(ctx context.Context, id string) (string, *string, bool, error) {
			_ = ctx
			return "edge://oracle-1", nil, true, nil
		},
		authValidator: func(ctx context.Context, c *echo.Context) (*authz.PermissionSet, *user.Actor, bool) {
			_ = ctx
			_ = c
			return authz.SudoPermissionSet(), nil, true
		},
		httpClient: &http.Client{Timeout: proxyTimeout},
		registry:   edge.NewTunnelRegistry(),
	}
}

func attachMiddleware(router *echo.Echo, mw *EnvironmentMiddleware) *echo.Group {
	api := router.Group("/api")
	api.Use(func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c *echo.Context) error {
			return mw.Handle(c, next)
		}
	})
	return api
}

func TestEnvironmentMiddleware_ReturnsBadGatewayForEdgeResourcesWithoutTunnel(t *testing.T) {
	middleware := newTestEnvironmentMiddleware()
	router := echo.New()
	api := attachMiddleware(router, middleware)

	localHandlerHit := false
	api.GET("/environments/:id/containers", func(c *echo.Context) error {
		localHandlerHit = true
		return c.JSON(http.StatusOK, map[string]any{"success": true})
	})

	req := httptest.NewRequest(http.MethodGet, "/api/environments/env-edge/containers", http.NoBody)
	recorder := httptest.NewRecorder()

	router.ServeHTTP(recorder, req)

	assert.Equal(t, http.StatusBadGateway, recorder.Code)
	assert.Contains(t, recorder.Body.String(), "Edge agent is not connected")
	assert.False(t, localHandlerHit)
}

func TestEnvironmentMiddleware_ProxiesDashboardResourcesForRemoteEnvironments(t *testing.T) {
	middleware := newTestEnvironmentMiddleware()
	router := echo.New()
	api := attachMiddleware(router, middleware)

	localHandlerHit := false
	api.GET("/environments/:id/dashboard", func(c *echo.Context) error {
		localHandlerHit = true
		return c.JSON(http.StatusOK, map[string]any{"success": true})
	})

	req := httptest.NewRequest(http.MethodGet, "/api/environments/env-edge/dashboard", http.NoBody)
	recorder := httptest.NewRecorder()

	router.ServeHTTP(recorder, req)

	assert.Equal(t, http.StatusBadGateway, recorder.Code)
	assert.Contains(t, recorder.Body.String(), "Edge agent is not connected")
	assert.False(t, localHandlerHit)
}

func TestEnvironmentMiddleware_KeepsEdgeManagementEndpointsLocal(t *testing.T) {
	middleware := newTestEnvironmentMiddleware()
	router := echo.New()
	api := attachMiddleware(router, middleware)

	localHandlerHit := false
	api.GET("/environments/:id/settings", func(c *echo.Context) error {
		localHandlerHit = true
		return c.JSON(http.StatusOK, map[string]any{"success": true})
	})

	req := httptest.NewRequest(http.MethodGet, "/api/environments/env-edge/settings", http.NoBody)
	recorder := httptest.NewRecorder()

	router.ServeHTTP(recorder, req)

	assert.Equal(t, http.StatusOK, recorder.Code)
	assert.Contains(t, recorder.Body.String(), "\"success\":true")
	assert.True(t, localHandlerHit)
}

func TestEnvironmentMiddleware_KeepsEdgeMTLSDownloadEndpointsLocal(t *testing.T) {
	middleware := newTestEnvironmentMiddleware()
	router := echo.New()
	api := attachMiddleware(router, middleware)

	localHandlerHit := false
	api.GET("/environments/:id/deployment/mtls/bundle", func(c *echo.Context) error {
		localHandlerHit = true
		return c.JSON(http.StatusOK, map[string]any{"success": true})
	})

	req := httptest.NewRequest(http.MethodGet, "/api/environments/env-edge/deployment/mtls/bundle", http.NoBody)
	recorder := httptest.NewRecorder()

	router.ServeHTTP(recorder, req)

	assert.Equal(t, http.StatusOK, recorder.Code)
	assert.Contains(t, recorder.Body.String(), "\"success\":true")
	assert.True(t, localHandlerHit)
}

func TestEnvironmentMiddleware_KeepsNotificationEndpointsLocal(t *testing.T) {
	middleware := newTestEnvironmentMiddleware()
	router := echo.New()
	api := attachMiddleware(router, middleware)

	localHandlerHit := false
	api.GET("/environments/:id/notifications/settings", func(c *echo.Context) error {
		localHandlerHit = true
		return c.JSON(http.StatusOK, map[string]any{"success": true})
	})

	req := httptest.NewRequest(http.MethodGet, "/api/environments/env-edge/notifications/settings", http.NoBody)
	recorder := httptest.NewRecorder()

	router.ServeHTTP(recorder, req)

	assert.Equal(t, http.StatusOK, recorder.Code)
	assert.Contains(t, recorder.Body.String(), "\"success\":true")
	assert.True(t, localHandlerHit)
}

func TestEnvironmentMiddleware_KeepsWebhookEndpointsLocal(t *testing.T) {
	tests := []struct {
		name   string
		method string
		route  string
		path   string
	}{
		{
			name:   "list webhooks",
			method: http.MethodGet,
			route:  "/environments/:id/webhooks",
			path:   "/api/environments/env-edge/webhooks",
		},
		{
			name:   "delete webhook",
			method: http.MethodDelete,
			route:  "/environments/:id/webhooks/:webhookId",
			path:   "/api/environments/env-edge/webhooks/wh-1",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			middleware := newTestEnvironmentMiddleware()
			router := echo.New()
			api := attachMiddleware(router, middleware)

			localHandlerHit := false
			api.Add(tt.method, tt.route, func(c *echo.Context) error {
				localHandlerHit = true
				return c.JSON(http.StatusOK, map[string]any{"success": true})
			})

			req := httptest.NewRequest(tt.method, tt.path, http.NoBody)
			recorder := httptest.NewRecorder()

			router.ServeHTTP(recorder, req)

			assert.Equal(t, http.StatusOK, recorder.Code)
			assert.Contains(t, recorder.Body.String(), "\"success\":true")
			assert.True(t, localHandlerHit)
		})
	}
}

func TestEnvironmentMiddleware_KeepsActivityEndpointsLocal(t *testing.T) {
	tests := []struct {
		name   string
		method string
		route  string
		path   string
	}{
		{
			name:   "list activities",
			method: http.MethodGet,
			route:  "/environments/:id/activities",
			path:   "/api/environments/env-edge/activities?limit=50",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			middleware := newTestEnvironmentMiddleware()
			router := echo.New()
			api := attachMiddleware(router, middleware)

			localHandlerHit := false
			api.Add(tt.method, tt.route, func(c *echo.Context) error {
				localHandlerHit = true
				return c.JSON(http.StatusOK, map[string]any{"success": true})
			})

			req := httptest.NewRequest(tt.method, tt.path, http.NoBody)
			recorder := httptest.NewRecorder()

			router.ServeHTTP(recorder, req)

			assert.Equal(t, http.StatusOK, recorder.Code)
			assert.Contains(t, recorder.Body.String(), "\"success\":true")
			assert.True(t, localHandlerHit)
		})
	}
}

func TestEnvironmentMiddleware_LocalEnvironmentSkipsProxyPermissionCheck(t *testing.T) {
	// The local environment ("0") is served directly and is never proxied, so
	// the proxy's per-environment authorization must not apply to it. Set up a
	// matcher that would require containers:list and a caller with no
	// permissions at all: if local requests were subject to proxy authz, this
	// would be a 403. It must instead fall through to the local handler, where
	// the operation's own RequirePermission middleware enforces access.
	matcher := authz.NewPermissionMatcher()
	matcher.Add(http.MethodGet, "/containers", authz.PermContainersList)

	mw := &EnvironmentMiddleware{
		localID:   "0",
		paramName: "id",
		matcher:   matcher,
		authValidator: func(ctx context.Context, c *echo.Context) (*authz.PermissionSet, *user.Actor, bool) {
			_ = ctx
			_ = c
			return authz.NewPermissionSet(), nil, true
		},
		httpClient: &http.Client{Timeout: proxyTimeout},
		registry:   edge.NewTunnelRegistry(),
	}

	router := echo.New()
	api := attachMiddleware(router, mw)

	localHandlerHit := false
	api.GET("/environments/:id/containers", func(c *echo.Context) error {
		localHandlerHit = true
		return c.JSON(http.StatusOK, map[string]any{"success": true})
	})

	req := httptest.NewRequest(http.MethodGet, "/api/environments/0/containers", http.NoBody)
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, req)

	assert.Equal(t, http.StatusOK, recorder.Code)
	assert.True(t, localHandlerHit, "local environment request must reach the local handler, not be proxy-authorized")
}

func TestEnvironmentMiddleware_RejectsInvalidProxyTarget(t *testing.T) {
	middleware := newTestEnvironmentMiddleware()
	middleware.resolver = func(ctx context.Context, id string) (string, *string, bool, error) {
		_, _ = ctx, id
		return "ftp://example.com", nil, true, nil
	}
	router := echo.New()
	attachMiddleware(router, middleware)

	req := httptest.NewRequest(http.MethodGet, "/api/environments/env-remote/containers", http.NoBody)
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, req)

	assert.Equal(t, http.StatusInternalServerError, recorder.Code)
	assert.Contains(t, recorder.Body.String(), "Invalid proxy target URL")
}

func TestEnvironmentMiddleware_KeepsNodeAgentDeploymentCreationLocal(t *testing.T) {
	middleware := newTestEnvironmentMiddleware()
	router := echo.New()
	api := attachMiddleware(router, middleware)

	localHandlerHit := false
	api.POST("/environments/:id/swarm/nodes/:nodeId/agent/deployment", func(c *echo.Context) error {
		localHandlerHit = true
		return c.JSON(http.StatusOK, map[string]any{"success": true})
	})

	req := httptest.NewRequest(http.MethodPost, "/api/environments/env-edge/swarm/nodes/node-1/agent/deployment", http.NoBody)
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, req)

	assert.Equal(t, http.StatusOK, recorder.Code)
	assert.Contains(t, recorder.Body.String(), "\"success\":true")
	assert.True(t, localHandlerHit)
}

func TestEnvironmentMiddleware_ForwardsResolvedIconCatalogHeaderOnly(t *testing.T) {
	tests := []struct {
		name           string
		catalog        *string
		clientSupplied string
		wantHeader     string
		userID         string
		userName       string
		wantInitiator  string
	}{
		{name: "forwards the caller's preference", catalog: new("dashboard-icons"), wantHeader: "dashboard-icons", userID: "user-1", userName: "operator", wantInitiator: "user-1"},
		{name: "strips a client-supplied header when the caller has no preference", clientSupplied: "dashboard-icons", wantHeader: ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var forwarded string
			var forwardedInitiator string
			var forwardedName string
			backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				forwarded = r.Header.Get(HeaderIconCatalog)
				forwardedInitiator = r.Header.Get(HeaderUpdateInitiatorID)
				forwardedName = r.Header.Get(HeaderUpdateInitiatorName)
				w.WriteHeader(http.StatusOK)
			}))
			defer backend.Close()

			mw := &EnvironmentMiddleware{
				localID:   "0",
				paramName: "id",
				resolver: func(ctx context.Context, id string) (string, *string, bool, error) {
					_, _ = ctx, id
					return backend.URL, nil, true, nil
				},
				authValidator: func(ctx context.Context, c *echo.Context) (*authz.PermissionSet, *user.Actor, bool) {
					_, _ = ctx, c
					u := &user.Actor{}
					u.ID = tt.userID
					u.Username = tt.userName
					u.Preferences.IconCatalog = tt.catalog
					return authz.SudoPermissionSet(), u, true
				},
				httpClient: &http.Client{Timeout: proxyTimeout},
				registry:   edge.NewTunnelRegistry(),
			}

			router := echo.New()
			api := attachMiddleware(router, mw)
			api.POST("/environments/:id/containers/:containerId/update", func(c *echo.Context) error {
				return c.JSON(http.StatusOK, map[string]any{"success": true})
			})

			req := httptest.NewRequest(http.MethodPost, "/api/environments/env-remote/containers/container-1/update", http.NoBody)
			req.Header.Set(HeaderUpdateInitiatorID, "spoofed")
			req.Header.Set(HeaderUpdateInitiatorName, "spoofed")
			if tt.clientSupplied != "" {
				req.Header.Set(HeaderIconCatalog, tt.clientSupplied)
			}
			recorder := httptest.NewRecorder()
			router.ServeHTTP(recorder, req)

			require.Equal(t, http.StatusOK, recorder.Code)
			assert.Equal(t, tt.wantHeader, forwarded)
			assert.Equal(t, tt.wantInitiator, forwardedInitiator)
			assert.Equal(t, tt.userName, forwardedName)
		})
	}
}

func TestIsManagementPath_SwarmIsMethodAware(t *testing.T) {
	assert.True(t, isManagementPath(http.MethodGet, "/swarm/nodes"))
	assert.True(t, isManagementPath(http.MethodGet, "/swarm/nodes/node-1"))
	assert.False(t, isManagementPath(http.MethodPatch, "/swarm/nodes/node-1"))
	assert.True(t, isManagementPath(http.MethodPost, "/swarm/nodes/node-1/agent/deployment"))
	assert.True(t, isManagementPath(http.MethodDelete, "/swarm/nodes/node-1/agent/deployment"))
	assert.True(t, isManagementPath(http.MethodPut, "/swarm/nodes/node-1/agent/binding"))
	assert.True(t, isManagementPath(http.MethodDelete, "/swarm/nodes/node-1/agent/binding"))
}

func TestIsManagementPath_VolumeBackupPolicyIsMethodAware(t *testing.T) {
	assert.True(t, isManagementPath(http.MethodGet, "/volumes/app-data/backup-policy"))
	assert.True(t, isManagementPath(http.MethodPut, "/volumes/app-data/backup-policy"))
	assert.False(t, isManagementPath(http.MethodPost, "/volumes/app-data/backup-policy"))
	assert.False(t, isManagementPath(http.MethodGet, "/volumes/app-data/backups"))
}

const proxyTestEnvID = "remote-1"

func newProxyAuthzMiddleware(matcher *authz.PermissionMatcher) *EnvironmentMiddleware {
	return &EnvironmentMiddleware{localID: "0", paramName: "id", matcher: matcher}
}

func newProxyRequestContext(method, path string) *echo.Context {
	e := echo.New()
	req := httptest.NewRequest(method, path, http.NoBody)
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

	require.True(t, m.proxyPermissionDenied(c, ps, proxyTestEnvID, "/containers/abc/restart"),
		"expected restart to be denied for a read-only user")
}

func TestProxyPermissionDeniedAllowsWriteForPermittedUser(t *testing.T) {
	m := newProxyAuthzMiddleware(containerMatcher())
	ps := authz.NewPermissionSet()
	ps.AddEnv(proxyTestEnvID, authz.PermContainersRestart)

	c := newProxyRequestContext(http.MethodPost, "/api/environments/"+proxyTestEnvID+"/containers/abc/restart")

	require.False(t, m.proxyPermissionDenied(c, ps, proxyTestEnvID, "/containers/abc/restart"),
		"expected restart to be allowed for a user with containers:restart")
}

func TestProxyPermissionDeniedAllowsRead(t *testing.T) {
	m := newProxyAuthzMiddleware(containerMatcher())
	ps := authz.NewPermissionSet()
	ps.AddEnv(proxyTestEnvID, authz.PermContainersList)

	c := newProxyRequestContext(http.MethodGet, "/api/environments/"+proxyTestEnvID+"/containers")

	require.False(t, m.proxyPermissionDenied(c, ps, proxyTestEnvID, "/containers"),
		"expected list to be allowed for a user with containers:list")
}

func TestProxyPermissionDeniedDeniesPermissionFromDifferentEnv(t *testing.T) {
	m := newProxyAuthzMiddleware(containerMatcher())
	// Caller holds containers:restart, but only for a DIFFERENT environment.
	ps := authz.NewPermissionSet()
	ps.AddEnv("other-env", authz.PermContainersRestart)

	c := newProxyRequestContext(http.MethodPost, "/api/environments/"+proxyTestEnvID+"/containers/abc/restart")

	require.True(t, m.proxyPermissionDenied(c, ps, proxyTestEnvID, "/containers/abc/restart"),
		"expected denial: permission is scoped to a different environment")
}

func TestProxyPermissionDeniedSudoBypasses(t *testing.T) {
	m := newProxyAuthzMiddleware(containerMatcher())
	c := newProxyRequestContext(http.MethodPost, "/api/environments/"+proxyTestEnvID+"/containers/abc/restart")

	require.False(t, m.proxyPermissionDenied(c, authz.SudoPermissionSet(), proxyTestEnvID, "/containers/abc/restart"),
		"expected sudo permission set to bypass the permission check")
}

func TestProxyPermissionDeniedDefaultDeniesUnmappedRoute(t *testing.T) {
	m := newProxyAuthzMiddleware(containerMatcher())
	ps := authz.NewPermissionSet()
	ps.AddEnv(proxyTestEnvID, authz.PermContainersRestart, authz.PermContainersList)

	c := newProxyRequestContext(http.MethodPost, "/api/environments/"+proxyTestEnvID+"/unknown/resource")

	require.True(t, m.proxyPermissionDenied(c, ps, proxyTestEnvID, "/unknown/resource"),
		"expected an unmapped proxied route to be denied by default")
}

func TestProxyPermissionDeniedAllowsPublicRoute(t *testing.T) {
	m := newProxyAuthzMiddleware(containerMatcher())
	ps := authz.NewPermissionSet() // no permissions at all

	c := newProxyRequestContext(http.MethodGet, "/api/environments/"+proxyTestEnvID+"/settings/public")

	require.False(t, m.proxyPermissionDenied(c, ps, proxyTestEnvID, "/settings/public"),
		"expected an explicitly public route to be allowed for any authenticated caller")
}

func newProxyVolumeWorkspaceContext(t *testing.T, changes []volume.WorkspaceFileChange, fileFirst ...bool) (*echo.Context, []byte) {
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
	manifestJSON, err := json.Marshal(volume.WorkspaceUpdateManifest{FileTreeRevision: "revision", FileChanges: changes})
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
	matcher := authz.NewPermissionMatcher()
	matcher.Add(http.MethodPut, "/volumes/{volumeName}/workspace", authz.PermVolumesRead)
	m := newProxyAuthzMiddleware(matcher)
	changes := []volume.WorkspaceFileChange{{Operation: volume.FileOpRename}}

	readOnly := authz.NewPermissionSet()
	readOnly.AddEnv(proxyTestEnvID, authz.PermVolumesRead)
	c, _ := newProxyVolumeWorkspaceContext(t, changes)
	require.True(t, m.proxyPermissionDenied(c, readOnly, proxyTestEnvID, "/volumes/data/workspace"))

	uploadOnly := authz.NewPermissionSet()
	uploadOnly.AddEnv(proxyTestEnvID, authz.PermVolumesRead, authz.PermVolumesUpload)
	c, _ = newProxyVolumeWorkspaceContext(t, changes)
	require.True(t, m.proxyPermissionDenied(c, uploadOnly, proxyTestEnvID, "/volumes/data/workspace"))

	permitted := authz.NewPermissionSet()
	permitted.AddEnv(proxyTestEnvID, authz.PermVolumesRead, authz.PermVolumesUpload, authz.PermVolumesDelete)
	c, rawBody := newProxyVolumeWorkspaceContext(t, changes)
	require.False(t, m.proxyPermissionDenied(c, permitted, proxyTestEnvID, "/volumes/data/workspace"))
	replayed, err := io.ReadAll(c.Request().Body)
	require.NoError(t, err)
	require.Equal(t, rawBody, replayed)

	c, rawBody = newProxyVolumeWorkspaceContext(t, changes, true)
	require.False(t, m.proxyPermissionDenied(c, permitted, proxyTestEnvID, "/volumes/data/workspace"))
	replayed, err = io.ReadAll(c.Request().Body)
	require.NoError(t, err)
	require.Equal(t, rawBody, replayed)
}

func newProxyGitOpsImportContext(body string) *echo.Context {
	e := echo.New()
	req := httptest.NewRequest(http.MethodPost, "/api/environments/"+proxyTestEnvID+"/gitops-syncs/import", bytes.NewReader([]byte(body)))
	req.Header.Set("Content-Type", "application/json")
	return e.NewContext(req, httptest.NewRecorder())
}

func TestProxyPermissionDeniedChecksGitOpsImportLifecyclePermission(t *testing.T) {
	matcher := authz.NewPermissionMatcher()
	matcher.Add(http.MethodPost, "/gitops-syncs/import", authz.PermGitOpsCreate)
	m := newProxyAuthzMiddleware(matcher)
	hookBody := `[{"syncName":"a","gitRepo":"r","branch":"main","dockerComposePath":"compose.yaml","preDeployScriptPath":"scripts/run.sh"}]`
	plainBody := `[{"syncName":"a","gitRepo":"r","branch":"main","dockerComposePath":"compose.yaml"}]`

	createOnly := authz.NewPermissionSet()
	createOnly.AddEnv(proxyTestEnvID, authz.PermGitOpsCreate)
	require.True(t, m.proxyPermissionDenied(newProxyGitOpsImportContext(hookBody), createOnly, proxyTestEnvID, "/gitops-syncs/import"))
	require.False(t, m.proxyPermissionDenied(newProxyGitOpsImportContext(plainBody), createOnly, proxyTestEnvID, "/gitops-syncs/import"))

	withLifecycle := authz.NewPermissionSet()
	withLifecycle.AddEnv(proxyTestEnvID, authz.PermGitOpsCreate, authz.PermGitOpsLifecycle)
	c := newProxyGitOpsImportContext(hookBody)
	require.False(t, m.proxyPermissionDenied(c, withLifecycle, proxyTestEnvID, "/gitops-syncs/import"))
	replayed, err := io.ReadAll(c.Request().Body)
	require.NoError(t, err)
	require.Equal(t, hookBody, string(replayed))
}

func TestProxyPermissionDeniedWSTerminalRequiresExec(t *testing.T) {
	matcher := authz.NewPermissionMatcher()
	matcher.Add(http.MethodGet, "/ws/containers/{containerId}/terminal", authz.PermContainersExec)
	m := newProxyAuthzMiddleware(matcher)

	// A caller who can read and list containers but lacks containers:exec must
	// not be able to open a terminal stream on the remote environment.
	ps := authz.NewPermissionSet()
	ps.AddEnv(proxyTestEnvID, authz.PermContainersRead, authz.PermContainersList)

	c := newProxyRequestContext(http.MethodGet, "/api/environments/"+proxyTestEnvID+"/ws/containers/abc/terminal")

	require.True(t, m.proxyPermissionDenied(c, ps, proxyTestEnvID, "/ws/containers/abc/terminal"),
		"expected WS terminal to be denied without containers:exec")

	// Granting containers:exec allows the same stream.
	ps.AddEnv(proxyTestEnvID, authz.PermContainersExec)

	require.False(t, m.proxyPermissionDenied(c, ps, proxyTestEnvID, "/ws/containers/abc/terminal"),
		"expected WS terminal to be allowed with containers:exec")
}

func TestProxyPermissionDeniedResourceSortRequiresRead(t *testing.T) {
	m := newProxyAuthzMiddleware(containerMatcher())

	listOnly := authz.NewPermissionSet()
	listOnly.AddEnv(proxyTestEnvID, authz.PermContainersList)

	for _, sort := range []string{"cpuUsage", "memoryUsage"} {
		c := newProxyRequestContext(http.MethodGet, "/api/environments/"+proxyTestEnvID+"/containers?sort="+sort)
		require.True(t, m.proxyPermissionDenied(c, listOnly, proxyTestEnvID, "/containers"),
			"expected resource sort %q to be denied with only containers:list", sort)
	}

	c := newProxyRequestContext(http.MethodGet, "/api/environments/"+proxyTestEnvID+"/containers?sort=name")
	require.False(t, m.proxyPermissionDenied(c, listOnly, proxyTestEnvID, "/containers"),
		"expected non-resource sorts to stay allowed with containers:list")

	c = newProxyRequestContext(http.MethodGet, "/api/environments/"+proxyTestEnvID+"/containers")
	require.False(t, m.proxyPermissionDenied(c, listOnly, proxyTestEnvID, "/containers"),
		"expected the plain list to stay allowed with containers:list")

	withRead := authz.NewPermissionSet()
	withRead.AddEnv(proxyTestEnvID, authz.PermContainersList, authz.PermContainersRead)
	c = newProxyRequestContext(http.MethodGet, "/api/environments/"+proxyTestEnvID+"/containers?sort=memoryUsage&order=desc")
	require.False(t, m.proxyPermissionDenied(c, withRead, proxyTestEnvID, "/containers"),
		"expected resource sort to be allowed with containers:list and containers:read")
}
