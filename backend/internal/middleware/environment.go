package middleware

import (
	"cmp"
	"context"
	"encoding/json/v2"
	"errors"
	"log/slog"
	"net"
	"net/http"
	"path"
	"slices"
	"strings"

	"github.com/labstack/echo/v5"

	"kit.getarcane.app"

	"getarcane.dev/arcane/v3/pkg/agent"
	"getarcane.dev/arcane/v3/pkg/authz"
	"getarcane.dev/arcane/v3/pkg/httpx"
	"getarcane.dev/arcane/v3/pkg/ws"
	"getarcane.dev/types/v3"
)

type (
	// EnvResolver resolves an environment ID to its connection details.
	// Returns: accessToken, enabled, error
	EnvResolver func(ctx context.Context, id string) (*string, bool, error)

	// AuthValidator authenticates a request and returns the caller's permission set and user (nil for non-user callers).
	// Sudo permission sets (internal agent proxies) bypass authorization.
	AuthValidator func(ctx context.Context, c *echo.Context) (*authz.PermissionSet, *types.Actor, bool)

	// EnvironmentMiddleware proxies requests for remote environments to their respective agents.
	EnvironmentMiddleware struct {
		resolver      EnvResolver
		authValidator AuthValidator
		registry      *agent.TunnelRegistry
		matcher       *authz.PermissionMatcher
		// checkOrigin is the local WebSocket Origin validator, so proxied upgrades cannot ride a cross-origin session.
		checkOrigin func(*http.Request) bool
		localID     string
		paramName   string
	}

	EnvironmentProxyConfig struct {
		Resolver      EnvResolver
		AuthValidator AuthValidator
		Matcher       *authz.PermissionMatcher
		Registry      *agent.TunnelRegistry
		CheckOrigin   func(*http.Request) bool
		LocalID       string
		ParamName     string
	}
)

const (
	apiEnvironmentsPrefix  = "/api/environments/"
	environmentsPathMarker = "/environments/"
)

// managementEndpointSet contains paths handled locally and never proxied to remote environments.
var managementEndpointSet = map[string]struct{}{
	"/test":       {},
	"/sync":       {},
	"/deployment": {},
	"/version":    {},
	"/settings":   {},
	"/jobs":       {},
}

func NewEnvironmentProxyMiddleware(cfg EnvironmentProxyConfig) echo.MiddlewareFunc {
	m := &EnvironmentMiddleware{
		localID:       cfg.LocalID,
		paramName:     cfg.ParamName,
		resolver:      cfg.Resolver,
		authValidator: cfg.AuthValidator,
		registry:      cfg.Registry,
		matcher:       cfg.Matcher,
		checkOrigin:   cfg.CheckOrigin,
	}
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c *echo.Context) error {
			return m.Handle(c, next)
		}
	}
}

// Handle is the main middleware handler.
func (m *EnvironmentMiddleware) Handle(c *echo.Context, next echo.HandlerFunc) error {
	req := c.Request()
	ctx := req.Context()
	if _, ok := httpx.EnvironmentScopeFromContext(ctx); ok {
		return next(c)
	}

	_, rest, hasMarker := strings.Cut(req.URL.Path, environmentsPathMarker)
	pathEnvID, _, _ := strings.Cut(rest, "/")
	envID := cmp.Or(c.Param(m.paramName), pathEnvID)
	if !hasMarker || envID == "" || envID == m.localID {
		return next(c)
	}

	suffix, ok := strings.CutPrefix(req.URL.Path, apiEnvironmentsPrefix+envID)
	if !ok || len(suffix) <= 1 || suffix[0] != '/' || IsManagerLocalPath(req.Method, suffix) {
		return next(c)
	}

	// SECURITY: Validate authentication BEFORE proxying to remote environments.
	perms, user, authenticated := m.authValidator(ctx, c)
	if !authenticated {
		return writeProxyError(c, http.StatusUnauthorized, "Authentication required to access remote environments")
	}
	if user != nil {
		RecordAuthenticatedRequest(ctx, req.Header, user)
	}

	// SECURITY: forwarded identity headers are always cleared first; only server-resolved values are sent.
	req.Header.Del(HeaderIconCatalog)
	if iconCatalog := kit.FromPtr(kit.FromPtr(user).Preferences.IconCatalog); iconCatalog != "" {
		req.Header.Set(HeaderIconCatalog, iconCatalog)
	}
	req.Header.Del(HeaderUpdateInitiatorID)
	req.Header.Del(HeaderUpdateInitiatorName)
	req.Header.Del(HeaderUpdateInitiatorDisplayName)
	if req.Method == http.MethodPost && strings.Contains(req.URL.Path, "/containers/") && strings.HasSuffix(req.URL.Path, "/update") &&
		kit.FromPtr(user).ID != "" {
		req.Header.Set(HeaderUpdateInitiatorID, user.ID)
		req.Header.Set(HeaderUpdateInitiatorName, user.Username)
		if displayName := kit.FromPtr(user.DisplayName); displayName != "" {
			req.Header.Set(HeaderUpdateInitiatorDisplayName, displayName)
		}
	}

	req.Header.Del(HeaderForwardedPermissions)
	if !kit.FromPtr(perms).Sudo {
		forwarded, marshalErr := json.Marshal(perms.ForwardedPermissions(envID))
		if marshalErr != nil {
			return writeProxyError(c, http.StatusInternalServerError, "Failed to encode forwarded permissions")
		}
		req.Header.Set(HeaderForwardedPermissions, string(forwarded))
	}

	accessToken, enabled, err := m.resolver(ctx, envID)
	if err != nil {
		return writeProxyError(c, http.StatusNotFound, "Environment not found")
	}
	if !enabled {
		return writeProxyError(c, http.StatusBadRequest, "Environment is disabled")
	}

	if !m.proxyPermitted(c, perms, envID, suffix) {
		return writeProxyError(c, http.StatusForbidden, "You don't have permission to perform this action on this environment")
	}

	tunnel, connected := m.registry.WaitForActive(ctx, envID, kit.Ternary(httpx.IsEnvironmentFanOutRequest(ctx), 0, agent.DefaultTunnelAcquireTimeout)).Get()
	if !connected {
		slog.WarnContext(ctx, "No active agent tunnel for environment", "environmentId", envID)
		return writeProxyError(c, http.StatusServiceUnavailable, "Agent is not connected")
	}
	if !kit.FromPtr(perms).Sudo && !slices.Contains(tunnel.MetadataSnapshot().Capabilities, agent.TunnelCapabilityForwardedPermissions) {
		slog.WarnContext(ctx, "Refusing to proxy to agent without forwarded permissions support", "environmentId", envID)
		return writeProxyError(c, http.StatusBadGateway, "Agent must be upgraded to use this environment")
	}

	slog.DebugContext(ctx, "Routing request through agent tunnel", "environmentId", envID, "path", req.URL.Path)
	target := agent.MuxAgentBaseURL + path.Join(apiEnvironmentsPrefix, m.localID) + suffix + kit.Ternary(req.URL.RawQuery != "", "?"+req.URL.RawQuery, "")
	if httpx.IsWebSocketUpgradeRequest(req) {
		wsHeaders := agent.BuildWebSocketHeaders(c, accessToken)
		if forwarded := req.Header.Get(HeaderForwardedPermissions); forwarded != "" {
			wsHeaders.Set(HeaderForwardedPermissions, forwarded)
		}
		if proxyErr := ws.ProxyHTTP(c.Response(), req, agent.HTTPToWebSocketURL(target), wsHeaders, m.checkOrigin, tunnel.HTTPClient); proxyErr != nil {
			slog.ErrorContext(ctx, "websocket proxy failed", "err", proxyErr)
		}
		return nil
	}

	targetURL, err := httpx.ValidateOutboundHTTPURL(target)
	if err != nil {
		return writeProxyError(c, http.StatusInternalServerError, "Invalid proxy target URL: "+err.Error())
	}

	// Stream the body unbuffered (uploads reach gigabytes); ContentLength 0 means no body, -1 means chunked.
	if req.Body != nil && req.ContentLength == 0 {
		_ = req.Body.Close()
	}

	// The body is never logged: it can carry compose files and registry credentials.
	slog.DebugContext(ctx, "Creating proxy request", "method", req.Method, "target", target, "contentLength", req.ContentLength, "contentType", req.Header.Get("Content-Type"))

	proxyReq := (&http.Request{
		Method:        req.Method,
		URL:           targetURL,
		Host:          targetURL.Host,
		Header:        make(http.Header),
		Body:          kit.Ternary(req.ContentLength != 0, req.Body, nil),
		ContentLength: kit.Ternary(req.Body != nil, req.ContentLength, 0),
	}).WithContext(ctx)

	agent.CopyRequestHeaders(req.Header, proxyReq.Header)
	agent.SetAuthHeader(proxyReq.Header, c)
	agent.SetAgentToken(proxyReq.Header, kit.FromPtr(accessToken))
	proxyReq.Header.Set("X-Forwarded-For", c.RealIP())
	proxyReq.Header.Set("X-Forwarded-Host", req.Host)

	if proxyReq.Body != nil {
		if fullDuplexErr := http.NewResponseController(c.Response()).EnableFullDuplex(); fullDuplexErr != nil {
			slog.DebugContext(ctx, "Failed to enable full duplex for proxied request", "error", fullDuplexErr)
		}
	}

	resp, err := tunnel.HTTPClient.Do(proxyReq)
	if err != nil {
		netErr, isNetErr := errors.AsType[net.Error](err)
		return writeProxyError(c, kit.Ternary(isNetErr && netErr.Timeout(), http.StatusGatewayTimeout, http.StatusBadGateway), "Proxy request failed: "+err.Error())
	}
	defer func() { _ = resp.Body.Close() }()

	// An upstream 401 means the agent rejected the manager's credentials; a 502 keeps browsers from dropping the session.
	if resp.StatusCode == http.StatusUnauthorized {
		slog.WarnContext(ctx, "Remote environment rejected the manager's credentials", "target", resp.Request.URL.Host)
		return writeProxyError(c, http.StatusBadGateway, "Remote environment rejected the manager's credentials")
	}
	w := c.Response()
	agent.CopyResponseHeaders(resp.Header, w.Header(), agent.BuildHopByHopHeaders(resp.Header))
	w.WriteHeader(resp.StatusCode)
	if req.Method != http.MethodHead {
		if copyErr := agent.CopyBodyWithFlush(w, resp.Body); copyErr != nil {
			slog.WarnContext(ctx, "Proxied response ended early", "environmentId", envID, "error", copyErr)
			// Headers are already sent, so abort the connection instead of ending a truncated response as complete.
			panic(http.ErrAbortHandler)
		}
	}
	return nil
}

func (m *EnvironmentMiddleware) proxyPermitted(c *echo.Context, ps *authz.PermissionSet, envID, suffix string) bool {
	if kit.FromPtr(ps).Sudo {
		return true
	}

	req := c.Request()
	ctx := req.Context()
	method := req.Method
	segments := strings.Split(strings.Trim(suffix, "/"), "/")

	// Upload-session routes derive their permission from the {kind} segment; unknown kinds fail closed.
	if len(segments) >= 2 && segments[0] == "uploads" && segments[1] != "" {
		if len(segments) == 2 && method == http.MethodPost ||
			len(segments) == 3 && (method == http.MethodGet || method == http.MethodDelete) ||
			len(segments) == 5 && method == http.MethodPut && segments[3] == "chunks" {
			perms := authz.UploadKindConditions.Required("kind", segments[1])
			denied := len(perms) == 0 || slices.ContainsFunc(perms, func(perm string) bool {
				return !ps.Allows(perm, envID)
			})
			if denied {
				slog.DebugContext(ctx, "Denying proxied upload session request: permission denied",
					"method", method, "path", suffix, "kind", segments[1], "environmentId", envID)
			}
			return !denied
		}
	}

	perm, ok := m.matcher.Lookup(method, suffix, req.URL.Query()).Get()
	if !ok {
		slog.WarnContext(ctx, "Denying proxied request with no known permission mapping", "method", method, "path", suffix, "environmentId", envID)
		return false
	}
	if perm == "" {
		// Explicitly public route: allowed for any authenticated caller.
		return true
	}
	if !ps.Allows(perm, kit.Ternary(authz.IsEnvScoped(perm), envID, "")) {
		slog.DebugContext(ctx, "Denying proxied request: permission denied", "method", method, "path", suffix, "permission", perm, "environmentId", envID)
		return false
	}
	return true
}

// IsManagerLocalPath reports whether an environment-scoped path suffix is served by the manager instead of being proxied.
func IsManagerLocalPath(method, suffix string) bool {
	if _, ok := managementEndpointSet[suffix]; ok {
		return true
	}
	// Webhooks live in the manager DB and their public trigger endpoint resolves tokens there.
	for _, prefix := range []string{"/jobs", "/activities", "/webhooks"} {
		if suffix == prefix || strings.HasPrefix(suffix, prefix+"/") {
			return true
		}
	}
	if strings.HasPrefix(suffix, "/deployment/mtls/") {
		return true
	}

	switch method + " " + suffix {
	case "GET /swarm/join/candidates", "GET /swarm/nodes", "POST /swarm/join/environments", "POST /swarm/nodes/agents/reconcile",
		"GET /swarm/nodes/agents/enrollment", "POST /swarm/nodes/agents/enrollment", "DELETE /swarm/nodes/agents/enrollment",
		"POST /system/upgrade/jobs", "GET /system/upgrade/jobs/current":
		return true
	}

	parts := strings.Split(strings.Trim(suffix, "/"), "/")
	if len(parts) == 4 && parts[0] == "volumes" && parts[1] != "" && parts[2] == "backups" && parts[3] == "policy" {
		return method == http.MethodGet || method == http.MethodPut
	}
	if len(parts) < 3 || parts[0] != "swarm" || parts[1] != "nodes" {
		return false
	}
	switch {
	case len(parts) == 3:
		return method == http.MethodGet
	case len(parts) == 5 && parts[3] == "agent" && parts[4] == "binding":
		return method == http.MethodPut || method == http.MethodDelete
	case len(parts) == 5 && parts[3] == "agent" && parts[4] == "deployment":
		return method == http.MethodPost || method == http.MethodDelete
	}
	return false
}

func writeProxyError(c *echo.Context, status int, message string) error {
	return c.JSON(status, map[string]any{
		"success": false,
		"data":    map[string]any{"error": message},
	})
}
