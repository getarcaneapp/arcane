package middleware

import (
	"cmp"
	"context"
	"encoding/json/v2"
	"net"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/labstack/echo/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"getarcane.dev/arcane/v3/pkg/agent"
	"getarcane.dev/arcane/v3/pkg/authz"
	"getarcane.dev/arcane/v3/pkg/httpx"
	"getarcane.dev/types/v3"
)

type openTunnelConn struct {
	agent.TunnelConnection
}

const (
	proxyTestEnvID = "remote-1"
)

func newTestEnvironmentMiddleware() *EnvironmentMiddleware {
	return &EnvironmentMiddleware{
		localID:   "0",
		paramName: "id",
		resolver: func(ctx context.Context, id string) (*string, bool, error) {
			_ = ctx
			return nil, true, nil
		},
		authValidator: func(ctx context.Context, c *echo.Context) (*authz.PermissionSet, *types.Actor, bool) {
			_ = ctx
			_ = c
			return authz.SudoPermissionSet(), nil, true
		},
		registry: agent.NewTunnelRegistry(),
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

func TestEnvironmentMiddleware_ReturnsServiceUnavailableForAgentResourcesWithoutTunnel(t *testing.T) {
	middleware := newTestEnvironmentMiddleware()
	router := echo.New()
	api := attachMiddleware(router, middleware)

	localHandlerHit := false
	api.GET("/agents/:id/containers", func(c *echo.Context) error {
		localHandlerHit = true
		return c.JSON(http.StatusOK, map[string]any{"success": true})
	})

	req := httptest.NewRequest(http.MethodGet, "/api/agents/env-agent/containers", http.NoBody)
	recorder := httptest.NewRecorder()

	router.ServeHTTP(recorder, req)

	assert.Equal(t, http.StatusServiceUnavailable, recorder.Code)
	assert.Contains(t, recorder.Body.String(), "Agent is not connected")
	assert.False(t, localHandlerHit)
}

func TestEnvironmentMiddleware_ProxiesDashboardResourcesForRemoteEnvironments(t *testing.T) {
	middleware := newTestEnvironmentMiddleware()
	router := echo.New()
	api := attachMiddleware(router, middleware)

	localHandlerHit := false
	api.GET("/agents/:id/dashboard", func(c *echo.Context) error {
		localHandlerHit = true
		return c.JSON(http.StatusOK, map[string]any{"success": true})
	})

	req := httptest.NewRequest(http.MethodGet, "/api/agents/env-agent/dashboard", http.NoBody)
	recorder := httptest.NewRecorder()

	router.ServeHTTP(recorder, req)

	assert.Equal(t, http.StatusServiceUnavailable, recorder.Code)
	assert.Contains(t, recorder.Body.String(), "Agent is not connected")
	assert.False(t, localHandlerHit)
}

func TestEnvironmentMiddleware_KeepsAgentManagementEndpointsLocal(t *testing.T) {
	middleware := newTestEnvironmentMiddleware()
	router := echo.New()
	api := attachMiddleware(router, middleware)

	localHandlerHit := false
	api.GET("/agents/:id/settings", func(c *echo.Context) error {
		localHandlerHit = true
		return c.JSON(http.StatusOK, map[string]any{"success": true})
	})

	req := httptest.NewRequest(http.MethodGet, "/api/agents/env-agent/settings", http.NoBody)
	recorder := httptest.NewRecorder()

	router.ServeHTTP(recorder, req)

	assert.Equal(t, http.StatusOK, recorder.Code)
	assert.Contains(t, recorder.Body.String(), "\"success\":true")
	assert.True(t, localHandlerHit)
}

func TestEnvironmentMiddleware_KeepsAgentMTLSDownloadEndpointsLocal(t *testing.T) {
	middleware := newTestEnvironmentMiddleware()
	router := echo.New()
	api := attachMiddleware(router, middleware)

	localHandlerHit := false
	api.GET("/agents/:id/deployment/mtls/bundle", func(c *echo.Context) error {
		localHandlerHit = true
		return c.JSON(http.StatusOK, map[string]any{"success": true})
	})

	req := httptest.NewRequest(http.MethodGet, "/api/agents/env-agent/deployment/mtls/bundle", http.NoBody)
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
			route:  "/agents/:id/webhooks",
			path:   "/api/agents/env-agent/webhooks",
		},
		{
			name:   "delete webhook",
			method: http.MethodDelete,
			route:  "/agents/:id/webhooks/:webhookId",
			path:   "/api/agents/env-agent/webhooks/wh-1",
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
			route:  "/agents/:id/activities",
			path:   "/api/agents/env-agent/activities?limit=50",
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
	matcher := authz.NewPermissionMatcher()
	matcher.Add(http.MethodGet, "/containers", authz.PermContainersList)

	mw := &EnvironmentMiddleware{
		localID:   "0",
		paramName: "id",
		matcher:   matcher,
		authValidator: func(ctx context.Context, c *echo.Context) (*authz.PermissionSet, *types.Actor, bool) {
			_ = ctx
			_ = c
			return authz.NewPermissionSet(), nil, true
		},
		registry: agent.NewTunnelRegistry(),
	}

	router := echo.New()
	api := attachMiddleware(router, mw)

	localHandlerHit := false
	api.GET("/agents/:id/containers", func(c *echo.Context) error {
		localHandlerHit = true
		return c.JSON(http.StatusOK, map[string]any{"success": true})
	})

	req := httptest.NewRequest(http.MethodGet, "/api/agents/0/containers", http.NoBody)
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, req)

	assert.Equal(t, http.StatusOK, recorder.Code)
	assert.True(t, localHandlerHit, "local environment request must reach the local handler, not be proxy-authorized")
}

func TestEnvironmentMiddleware_KeepsNodeAgentDeploymentCreationLocal(t *testing.T) {
	middleware := newTestEnvironmentMiddleware()
	router := echo.New()
	api := attachMiddleware(router, middleware)

	localHandlerHit := false
	api.POST("/agents/:id/swarm/nodes/:nodeId/agent/deployment", func(c *echo.Context) error {
		localHandlerHit = true
		return c.JSON(http.StatusOK, map[string]any{"success": true})
	})

	req := httptest.NewRequest(http.MethodPost, "/api/agents/env-agent/swarm/nodes/node-1/agent/deployment", http.NoBody)
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, req)

	assert.Equal(t, http.StatusOK, recorder.Code)
	assert.Contains(t, recorder.Body.String(), "\"success\":true")
	assert.True(t, localHandlerHit)
}

func TestEnvironmentMiddleware_ForwardsResolvedHeadersOnly(t *testing.T) {
	caller := authz.NewPermissionSet()
	caller.AddGlobal(authz.PermUsersList, authz.PermContainersStart)
	caller.AddEnv("env-remote", authz.PermContainersUpdate)
	caller.AddEnv("env-other", authz.PermContainersDelete)
	admin := authz.NewPermissionSet()
	admin.AddGlobal(authz.AllPermissions()...)

	tests := []struct {
		catalog        *string
		perms          *authz.PermissionSet
		name           string
		clientSupplied string
		wantForwarded  string
		wantHeader     string
		userID         string
		userName       string
		wantInitiator  string
	}{
		{name: "forwards the caller's preference", catalog: new("dashboard-icons"), wantHeader: "dashboard-icons", userID: "user-1", userName: "operator", wantInitiator: "user-1"},
		{name: "strips a client-supplied header when the caller has no preference", clientSupplied: "dashboard-icons", wantHeader: ""},
		{name: "forwards a non-sudo caller's permissions", userID: "user-2", userName: "limited", wantInitiator: "user-2", perms: caller},
		{
			name:          "forwards a global admin as a flag",
			userID:        "user-3",
			userName:      "admin",
			wantInitiator: "user-3",
			perms:         admin,
			wantForwarded: `{"global":[],"environment":[],"globalAdmin":true}`,
		},
	}

	updateMatcher := authz.NewPermissionMatcher()
	updateMatcher.Add(http.MethodPost, "/containers/{containerId}/update", authz.PermContainersUpdate)

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var forwarded string
			var forwardedInitiator string
			var forwardedName string
			var forwardedPermissions string
			registry := agent.NewTunnelRegistry()
			manager := echo.New()
			manager.GET("/api/tunnel/connect", agent.NewTunnelServer(agent.TunnelServerOptions{
				StreamWindowMiB: func() int {
					return 16
				},
				Registry: registry,
				Resolver: func(context.Context, string) (string, error) {
					return "env-remote", nil
				},
				MTLS: agent.NewMTLS(agent.MTLSConfig{}),
			}).HandleConnect)
			managerServer := httptest.NewServer(manager)
			defer managerServer.Close()
			agentCtx, cancelAgent := context.WithCancel(t.Context())
			defer cancelAgent()
			_, startErr := agent.NewTunnelClient(agent.TunnelClientConfig{
				StreamWindowMiB: func() int {
					return 16
				},
				Config: &agent.Config{
					MTLS:           agent.NewMTLS(agent.MTLSConfig{ManagerURL: managerServer.URL}),
					AgentEnabled:   true,
					AgentTransport: agent.TransportWebSocket,
					AgentToken:     "agent-token",
				},
				Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					forwarded = r.Header.Get(HeaderIconCatalog)
					forwardedInitiator = r.Header.Get(HeaderUpdateInitiatorID)
					forwardedName = r.Header.Get(HeaderUpdateInitiatorName)
					forwardedPermissions = r.Header.Get(HeaderForwardedPermissions)
					w.WriteHeader(http.StatusOK)
				}),
			}).Start(agentCtx)
			require.NoError(t, startErr)
			require.True(t, registry.WaitForActive(t.Context(), "env-remote", 5*time.Second).IsPresent())

			mw := &EnvironmentMiddleware{
				localID:   "0",
				paramName: "id",
				resolver: func(ctx context.Context, id string) (*string, bool, error) {
					_, _ = ctx, id
					return nil, true, nil
				},
				authValidator: func(ctx context.Context, c *echo.Context) (*authz.PermissionSet, *types.Actor, bool) {
					_, _ = ctx, c
					u := &types.Actor{}
					u.ID = tt.userID
					u.Username = tt.userName
					u.Preferences.IconCatalog = tt.catalog
					return cmp.Or(tt.perms, authz.SudoPermissionSet()), u, true
				},
				registry: registry,
				matcher:  updateMatcher,
			}

			router := echo.New()
			api := attachMiddleware(router, mw)
			api.POST("/agents/:id/containers/:containerId/update", func(c *echo.Context) error {
				return c.JSON(http.StatusOK, map[string]any{"success": true})
			})

			req := httptest.NewRequest(http.MethodPost, "/api/agents/env-remote/containers/container-1/update", http.NoBody)
			req.Header.Set(HeaderUpdateInitiatorID, "spoofed")
			req.Header.Set(HeaderUpdateInitiatorName, "spoofed")
			req.Header.Set(HeaderForwardedPermissions, `{"global":["users:create"],"environment":[]}`)
			if tt.clientSupplied != "" {
				req.Header.Set(HeaderIconCatalog, tt.clientSupplied)
			}
			recorder := httptest.NewRecorder()
			router.ServeHTTP(recorder, req)

			require.Equal(t, http.StatusOK, recorder.Code)
			assert.Equal(t, tt.wantHeader, forwarded)
			assert.Equal(t, tt.wantInitiator, forwardedInitiator)
			assert.Equal(t, tt.userName, forwardedName)
			if tt.perms == nil {
				assert.Empty(t, forwardedPermissions)
				return
			}
			if tt.wantForwarded != "" {
				assert.Equal(t, tt.wantForwarded, forwardedPermissions)
				return
			}
			var decoded authz.ForwardedPermissions
			require.NoError(t, json.Unmarshal([]byte(forwardedPermissions), &decoded))
			agentPerms := decoded.PermissionSet("0")
			assert.True(t, agentPerms.Allows(authz.PermUsersList, ""))
			assert.True(t, agentPerms.Allows(authz.PermContainersStart, "0"))
			assert.True(t, agentPerms.Allows(authz.PermContainersUpdate, "0"))
			assert.False(t, agentPerms.Allows(authz.PermContainersUpdate, ""))
			assert.False(t, agentPerms.Allows(authz.PermContainersDelete, "0"))
			assert.False(t, agentPerms.Allows(authz.PermUsersCreate, ""))
			assert.False(t, agentPerms.Sudo)
		})
	}
}

func (openTunnelConn) IsClosed() bool {
	return false
}

func TestEnvironmentMiddleware_RequiresForwardedPermissionsCapability(t *testing.T) {
	caller := authz.NewPermissionSet()
	caller.AddEnv("env-remote", authz.PermContainersUpdate)

	tests := []struct {
		name         string
		perms        *authz.PermissionSet
		capabilities []string
		wantStatus   int
	}{
		{
			name:       "refuses a non-sudo caller when the agent lacks the capability",
			perms:      caller,
			wantStatus: http.StatusBadGateway,
		},
		{
			name:         "forwards a non-sudo caller when the agent has the capability",
			perms:        caller,
			capabilities: []string{agent.TunnelCapabilityForwardedPermissions},
			wantStatus:   http.StatusOK,
		},
		{
			name:       "forwards a sudo caller when the agent lacks the capability",
			perms:      authz.SudoPermissionSet(),
			wantStatus: http.StatusOK,
		},
		{
			name:         "forwards a sudo caller when the agent has the capability",
			perms:        authz.SudoPermissionSet(),
			capabilities: []string{agent.TunnelCapabilityForwardedPermissions},
			wantStatus:   http.StatusOK,
		},
	}

	updateMatcher := authz.NewPermissionMatcher()
	updateMatcher.Add(http.MethodPost, "/containers/{containerId}/update", authz.PermContainersUpdate)

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var agentHits atomic.Int32
			agentServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				agentHits.Add(1)
				w.WriteHeader(http.StatusOK)
			}))
			defer agentServer.Close()

			registry := agent.NewTunnelRegistry()
			registered, err := registry.RegisterSession(t.Context(), &agent.AgentTunnel{
				Conn: openTunnelConn{},
				HTTPClient: &http.Client{
					Transport: &http.Transport{
						DialContext: func(ctx context.Context, network, _ string) (net.Conn, error) {
							return (&net.Dialer{}).DialContext(ctx, network, agentServer.Listener.Addr().String())
						},
					},
				},
				EnvironmentID: "env-remote",
				State:         "connected",
				Capabilities:  tt.capabilities,
			})
			require.NoError(t, err)
			require.True(t, registered.Accepted, registered.Reason)

			router := echo.New()
			api := attachMiddleware(router, &EnvironmentMiddleware{
				localID:   "0",
				paramName: "id",
				resolver: func(context.Context, string) (*string, bool, error) {
					return nil, true, nil
				},
				authValidator: func(context.Context, *echo.Context) (*authz.PermissionSet, *types.Actor, bool) {
					return tt.perms, &types.Actor{}, true
				},
				registry: registry,
				matcher:  updateMatcher,
			})
			api.POST("/agents/:id/containers/:containerId/update", func(c *echo.Context) error {
				return c.JSON(http.StatusTeapot, map[string]any{"success": true})
			})

			recorder := httptest.NewRecorder()
			router.ServeHTTP(recorder, httptest.NewRequest(http.MethodPost, "/api/agents/env-remote/containers/container-1/update", http.NoBody))

			require.Equal(t, tt.wantStatus, recorder.Code)
			if tt.wantStatus == http.StatusOK {
				assert.Equal(t, int32(1), agentHits.Load())
				return
			}
			assert.Zero(t, agentHits.Load())
			assert.Contains(t, recorder.Body.String(), "Agent must be upgraded to use this environment")
		})
	}
}

func TestIsManagementPath_SwarmIsMethodAware(t *testing.T) {
	assert.True(t, IsManagerLocalPath(http.MethodGet, "/swarm/nodes"))
	assert.True(t, IsManagerLocalPath(http.MethodGet, "/swarm/nodes/node-1"))
	assert.False(t, IsManagerLocalPath(http.MethodPatch, "/swarm/nodes/node-1"))
	assert.True(t, IsManagerLocalPath(http.MethodPost, "/swarm/nodes/node-1/agent/deployment"))
	assert.True(t, IsManagerLocalPath(http.MethodDelete, "/swarm/nodes/node-1/agent/deployment"))
	assert.True(t, IsManagerLocalPath(http.MethodPut, "/swarm/nodes/node-1/agent/binding"))
	assert.True(t, IsManagerLocalPath(http.MethodDelete, "/swarm/nodes/node-1/agent/binding"))
}

func TestIsManagementPath_VolumeBackupPolicyIsMethodAware(t *testing.T) {
	assert.True(t, IsManagerLocalPath(http.MethodGet, "/volumes/app-data/backups/policy"))
	assert.True(t, IsManagerLocalPath(http.MethodPut, "/volumes/app-data/backups/policy"))
	assert.False(t, IsManagerLocalPath(http.MethodPost, "/volumes/app-data/backups/policy"))
	assert.False(t, IsManagerLocalPath(http.MethodGet, "/volumes/app-data/backups"))
}

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

	c := newProxyRequestContext(http.MethodPost, "/api/agents/"+proxyTestEnvID+"/containers/abc/restart")

	require.False(t, m.proxyPermitted(c, ps, proxyTestEnvID, "/containers/abc/restart"),
		"expected restart to be denied for a read-only user")
}

func TestProxyPermissionDeniedAllowsWriteForPermittedUser(t *testing.T) {
	m := newProxyAuthzMiddleware(containerMatcher())
	ps := authz.NewPermissionSet()
	ps.AddEnv(proxyTestEnvID, authz.PermContainersRestart)

	c := newProxyRequestContext(http.MethodPost, "/api/agents/"+proxyTestEnvID+"/containers/abc/restart")

	require.True(t, m.proxyPermitted(c, ps, proxyTestEnvID, "/containers/abc/restart"),
		"expected restart to be allowed for a user with containers:restart")
}

func TestProxyPermissionDeniedAllowsRead(t *testing.T) {
	m := newProxyAuthzMiddleware(containerMatcher())
	ps := authz.NewPermissionSet()
	ps.AddEnv(proxyTestEnvID, authz.PermContainersList)

	c := newProxyRequestContext(http.MethodGet, "/api/agents/"+proxyTestEnvID+"/containers")

	require.True(t, m.proxyPermitted(c, ps, proxyTestEnvID, "/containers"),
		"expected list to be allowed for a user with containers:list")
}

func TestProxyPermissionDeniedDeniesPermissionFromDifferentEnv(t *testing.T) {
	m := newProxyAuthzMiddleware(containerMatcher())
	// Caller holds containers:restart, but only for a DIFFERENT environment.
	ps := authz.NewPermissionSet()
	ps.AddEnv("other-env", authz.PermContainersRestart)

	c := newProxyRequestContext(http.MethodPost, "/api/agents/"+proxyTestEnvID+"/containers/abc/restart")

	require.False(t, m.proxyPermitted(c, ps, proxyTestEnvID, "/containers/abc/restart"),
		"expected denial: permission is scoped to a different environment")
}

func TestProxyPermissionDeniedSudoBypasses(t *testing.T) {
	m := newProxyAuthzMiddleware(containerMatcher())
	c := newProxyRequestContext(http.MethodPost, "/api/agents/"+proxyTestEnvID+"/containers/abc/restart")

	require.True(t, m.proxyPermitted(c, authz.SudoPermissionSet(), proxyTestEnvID, "/containers/abc/restart"),
		"expected sudo permission set to bypass the permission check")
}

func TestProxyPermissionDeniedDefaultDeniesUnmappedRoute(t *testing.T) {
	m := newProxyAuthzMiddleware(containerMatcher())
	ps := authz.NewPermissionSet()
	ps.AddEnv(proxyTestEnvID, authz.PermContainersRestart, authz.PermContainersList)

	c := newProxyRequestContext(http.MethodPost, "/api/agents/"+proxyTestEnvID+"/unknown/resource")

	require.False(t, m.proxyPermitted(c, ps, proxyTestEnvID, "/unknown/resource"),
		"expected an unmapped proxied route to be denied by default")
}

func TestProxyPermissionDeniedAllowsPublicRoute(t *testing.T) {
	m := newProxyAuthzMiddleware(containerMatcher())
	ps := authz.NewPermissionSet()

	c := newProxyRequestContext(http.MethodGet, "/api/agents/"+proxyTestEnvID+"/settings/public")

	require.True(t, m.proxyPermitted(c, ps, proxyTestEnvID, "/settings/public"),
		"expected an explicitly public route to be allowed for any authenticated caller")
}

func TestProxyPermissionDeniedWSTerminalRequiresExec(t *testing.T) {
	matcher := authz.NewPermissionMatcher()
	matcher.Add(http.MethodGet, "/ws/containers/{containerId}/terminal", authz.PermContainersExec)
	m := newProxyAuthzMiddleware(matcher)

	// A caller who can read and list containers but lacks containers:exec must
	// not be able to open a terminal stream on the remote environment.
	ps := authz.NewPermissionSet()
	ps.AddEnv(proxyTestEnvID, authz.PermContainersRead, authz.PermContainersList)

	c := newProxyRequestContext(http.MethodGet, "/api/agents/"+proxyTestEnvID+"/ws/containers/abc/terminal")

	require.False(t, m.proxyPermitted(c, ps, proxyTestEnvID, "/ws/containers/abc/terminal"),
		"expected WS terminal to be denied without containers:exec")

	ps.AddEnv(proxyTestEnvID, authz.PermContainersExec)

	require.True(t, m.proxyPermitted(c, ps, proxyTestEnvID, "/ws/containers/abc/terminal"),
		"expected WS terminal to be allowed with containers:exec")
}

func TestEnvironmentMiddleware_ServesMultiEnvironmentScopeLocally(t *testing.T) {
	router := echo.New()
	router.Pre(func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c *echo.Context) error {
			req := c.Request()
			c.SetRequest(req.WithContext(httpx.WithEnvironmentScope(req.Context(), &httpx.EnvironmentScope{IDs: []string{"env-agent", "0"}})))
			return next(c)
		}
	})
	api := attachMiddleware(router, newTestEnvironmentMiddleware())

	localHandlerHit := false
	api.GET("/agents/:id/containers", func(c *echo.Context) error {
		localHandlerHit = true
		return c.JSON(http.StatusOK, map[string]any{"success": true})
	})

	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/agents/env-agent/containers", http.NoBody))

	assert.Equal(t, http.StatusOK, recorder.Code)
	assert.True(t, localHandlerHit)
}
