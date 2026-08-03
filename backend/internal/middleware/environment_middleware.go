package middleware

import (
	"github.com/samber/mo"

	"context"
	"io"
	"log/slog"
	"net/http"
	"path"
	"strings"
	"time"

	"emperror.dev/errors"
	"github.com/getarcaneapp/arcane/backend/v2/internal/common"
	"github.com/getarcaneapp/arcane/backend/v2/internal/models"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/authz"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/libarcane/edge"
	wsutil "github.com/getarcaneapp/arcane/backend/v2/pkg/libarcane/ws"
	pkgutils "github.com/getarcaneapp/arcane/backend/v2/pkg/utils"
	httputils "github.com/getarcaneapp/arcane/backend/v2/pkg/utils/httpx"
	"github.com/labstack/echo/v5"
)

const (
	apiEnvironmentsPrefix  = "/api/environments/"
	environmentsPathMarker = "/environments/"

	// proxyTimeout is intentionally generous because some proxied operations
	// (e.g., image pulls with progress streaming) can take multiple minutes.
	proxyTimeout = 30 * time.Minute
)

// managementEndpointSet contains paths handled locally and never proxied to remote environments.
var managementEndpointSet = map[string]struct{}{
	"/test":            {},
	"/heartbeat":       {},
	"/sync-registries": {},
	"/sync":            {},
	"/deployment":      {},
	"/agent/pair":      {},
	"/version":         {},
	"/settings":        {},
	"/job-schedules":   {},
	"/jobs":            {},
}

// EnvResolver resolves an environment ID to its connection details.
// Returns: apiURL, accessToken, enabled, error
type EnvResolver func(ctx context.Context, id string) (string, *string, bool, error)

// AuthValidator validates authentication for a request and resolves the
// caller's effective permission set. The boolean result reports whether the
// request is authenticated; the permission set is used to authorize proxied
// requests against the target environment. Sudo permission sets (internal
// agent proxies) bypass authorization. The resolved user is returned so
// per-user context (e.g. the icon catalog preference) can travel with the
// proxied request; it is nil for callers that are not a user (environment
// bootstrap keys).
type AuthValidator func(ctx context.Context, c *echo.Context) (*authz.PermissionSet, *models.User, bool)

// EnvironmentMiddleware proxies requests for remote environments to their respective agents.
type EnvironmentMiddleware struct {
	localID       string
	paramName     string
	resolver      EnvResolver
	authValidator AuthValidator
	httpClient    *http.Client
	registry      *edge.TunnelRegistry
	matcher       *authz.PermissionMatcher
	// checkOrigin is the same Origin validator the local WebSocket endpoints
	// use. Proxied upgrades previously accepted any Origin, so a cross-origin
	// page could ride the caller's session cookie into a remote environment's
	// terminal or log stream.
	checkOrigin func(*http.Request) bool
}

// NewEnvProxyMiddlewareWithParamAndRegistry creates middleware with an injected tunnel registry.
func NewEnvProxyMiddlewareWithParamAndRegistry(
	localID,
	paramName string,
	resolver EnvResolver,
	authValidator AuthValidator,
	matcher *authz.PermissionMatcher,
	registry *edge.TunnelRegistry,
	checkOrigin func(*http.Request) bool,
) echo.MiddlewareFunc {
	if registry == nil {
		registry = edge.NewTunnelRegistry()
	}

	m := &EnvironmentMiddleware{
		localID:       localID,
		paramName:     paramName,
		resolver:      resolver,
		authValidator: authValidator,
		httpClient:    &http.Client{Timeout: proxyTimeout},
		registry:      registry,
		matcher:       matcher,
		checkOrigin:   checkOrigin,
	}
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c *echo.Context) error {
			return m.Handle(c, next)
		}
	}
}

// Handle is the main middleware handler.
func (m *EnvironmentMiddleware) Handle(c *echo.Context, next echo.HandlerFunc) error {
	envID := m.extractEnvironmentID(c)

	if envID == "" || envID == m.localID {
		return next(c)
	}

	if !m.hasResourcePath(c, envID) {
		return next(c)
	}

	// SECURITY: Validate authentication BEFORE proxying to remote environments.
	var perms *authz.PermissionSet
	var user *models.User
	if m.authValidator != nil {
		ps, u, ok := m.authValidator(c.Request().Context(), c)
		if !ok {
			return c.JSON(http.StatusUnauthorized, map[string]any{
				"success": false,
				"data":    map[string]any{"error": "Authentication required to access remote environments"},
			})
		}
		perms, user = ps, u
	}

	m.setIconCatalogHeaderInternal(c, user)

	apiURL, accessToken, enabled, err := m.resolver(c.Request().Context(), envID)
	if err != nil || apiURL == "" {
		return c.JSON(http.StatusNotFound, map[string]any{
			"success": false,
			"data":    map[string]any{"error": "Environment not found"},
		})
	}

	if !enabled {
		return c.JSON(http.StatusBadRequest, map[string]any{
			"success": false,
			"data":    map[string]any{"error": (errors.New("Environment is disabled")).Error()},
		})
	}

	// SECURITY: Enforce the caller's per-environment permission BEFORE proxying.
	// Remote agents run with a sudo permission set and perform no authorization
	// of their own, so authorization for proxied requests must happen here,
	// mirroring the per-operation RequirePermission checks used for the local
	// environment.
	if m.proxyPermissionDenied(c, perms, envID) {
		return c.JSON(http.StatusForbidden, map[string]any{
			"success": false,
			"data":    map[string]any{"error": "You don't have permission to perform this action on this environment"},
		})
	}

	// Stamp the manager's authenticated human user onto the forwarded
	// request so the agent can attribute audit events to them instead of
	// its own service account. Applies uniformly before any proxy path
	// (tunnel, direct WS, direct HTTP) so none of them can be missed.
	m.setProxyActorHeadersInternal(c)

	isEdgeEnvironment := isEdgeEnvironmentURLInternal(apiURL)

	if handled, err := m.proxyActiveEdgeTunnelInternal(c, envID, accessToken); handled {
		return err
	}

	if isEdgeEnvironment {
		if handled, err := m.proxyRecoveredEdgeTunnelInternal(c, envID, accessToken); handled {
			return err
		}

		slog.WarnContext(c.Request().Context(), "No active edge tunnel for environment", "environment_id", envID)
		return m.abortEdgeTunnelUnavailable(c)
	}

	target := m.buildTargetURL(c, envID, apiURL)

	if httputils.IsWebSocketUpgradeRequest(c.Request()) {
		return m.proxyWebSocket(c, target, accessToken, envID)
	}
	return m.proxyHTTP(c, target, accessToken)
}

// setIconCatalogHeaderInternal stamps the authenticated user's icon catalog
// preference onto the outgoing proxied request. Remote agents authenticate the
// proxy as a synthetic user with no preferences, so without this every remote
// environment resolves icons against the default catalog.
//
// SECURITY: the header is always cleared first, so a browser-supplied value
// never rides through; only the server-resolved preference is forwarded.
func (m *EnvironmentMiddleware) setIconCatalogHeaderInternal(c *echo.Context, user *models.User) {
	c.Request().Header.Del(pkgutils.HeaderIconCatalog)
	if user == nil || user.Preferences.IconCatalog == nil || *user.Preferences.IconCatalog == "" {
		return
	}
	c.Request().Header.Set(pkgutils.HeaderIconCatalog, *user.Preferences.IconCatalog)
}

// proxyPermissionDenied reports whether the caller lacks permission to perform
// the proxied request against environment envID. It mirrors the per-operation
// RequirePermission checks enforced for the local environment: the permission
// required for the (method, path) is looked up in the matcher and evaluated
// against the caller's permission set for the target environment.
//
// Sudo callers bypass the check. Requests whose (method, path) has no known
// permission mapping are denied (default-deny), so a newly added proxied route
// cannot silently bypass authorization before it is mapped.
func (m *EnvironmentMiddleware) proxyPermissionDenied(c *echo.Context, ps *authz.PermissionSet, envID string) bool {
	if m.matcher == nil {
		return false
	}
	if ps != nil && ps.Sudo {
		return false
	}

	method := c.Request().Method
	suffix := m.buildResourceSuffix(c.Request().URL.Path, envID)
	perm, ok := m.matcher.Lookup(method, suffix).Get()
	if !ok {
		slog.WarnContext(c.Request().Context(), "Denying proxied request with no known permission mapping",
			"method", method, "path", suffix, "environment_id", envID)
		return true
	}
	if perm == "" {
		// Explicitly public proxied route (e.g. public settings): allowed for
		// any authenticated caller, matching local enforcement.
		return false
	}

	scopeEnvID := ""
	if authz.IsEnvScoped(perm) {
		scopeEnvID = envID
	}
	if !ps.Allows(perm, scopeEnvID) {
		slog.DebugContext(c.Request().Context(), "Denying proxied request: permission denied",
			"method", method, "path", suffix, "permission", perm, "environment_id", envID)
		return true
	}
	return false
}

func (m *EnvironmentMiddleware) proxyActiveEdgeTunnelInternal(c *echo.Context, envID string, accessToken *string) (bool, error) {
	tunnel, ok := m.getActiveEdgeTunnelInternal(envID).Get()
	if !ok {
		return false, nil
	}

	slog.DebugContext(c.Request().Context(), "Routing request through edge tunnel", "environment_id", envID, "path", c.Request().URL.Path)
	m.setProxyContextHeadersInternal(c, accessToken)
	return true, m.proxyThroughTunnelInternal(c, tunnel, envID)
}

func (m *EnvironmentMiddleware) proxyRecoveredEdgeTunnelInternal(c *echo.Context, envID string, accessToken *string) (bool, error) {
	edge.TouchTunnelDemand(envID, edge.DefaultTunnelDemandTTL)

	tunnel, ok := m.waitForActiveEdgeTunnelInternal(c.Request().Context(), envID, edge.DefaultTunnelAcquireTimeout()).Get()
	if !ok {
		return false, nil
	}

	slog.InfoContext(c.Request().Context(), "Recovered edge tunnel during request", "environment_id", envID)
	m.setProxyContextHeadersInternal(c, accessToken)
	return true, m.proxyThroughTunnelInternal(c, tunnel, envID)
}

func (m *EnvironmentMiddleware) setProxyContextHeadersInternal(c *echo.Context, accessToken *string) {
	if accessToken != nil && *accessToken != "" {
		c.Request().Header.Set(edge.HeaderAgentToken, *accessToken)
		c.Request().Header.Set(edge.HeaderAPIKey, *accessToken)
	}
}

// setProxyActorHeadersInternal stamps X-Arcane-Actor-Id/-Username onto the
// outgoing request from the manager's own auth resolution (the "userID"/
// "currentUser" context keys createAuthValidatorInternal sets, mirroring
// what the local auth middleware sets for non-proxied requests). The
// headers are deleted first, unconditionally, so a client cannot forge them
// by sending its own values before this middleware runs — only the
// manager's own resolved identity can populate them.
func (m *EnvironmentMiddleware) setProxyActorHeadersInternal(c *echo.Context) {
	c.Request().Header.Del(pkgutils.HeaderActorUserID)
	c.Request().Header.Del(pkgutils.HeaderActorUsername)

	userID := contextUserIDInternal(c)
	if userID == "" {
		return
	}
	c.Request().Header.Set(pkgutils.HeaderActorUserID, userID)
	if username := contextUsernameInternal(c); username != "" {
		c.Request().Header.Set(pkgutils.HeaderActorUsername, username)
	}
}

func contextUserIDInternal(c *echo.Context) string {
	if val := c.Get("userID"); val != nil {
		if userID, ok := val.(string); ok {
			return userID
		}
	}
	return ""
}

func contextUsernameInternal(c *echo.Context) string {
	if val := c.Get("currentUser"); val != nil {
		if user, ok := val.(*models.User); ok {
			return user.Username
		}
	}
	return ""
}

func (m *EnvironmentMiddleware) proxyThroughTunnelInternal(c *echo.Context, tunnel *edge.AgentTunnel, envID string) error {
	proxyPath := m.buildProxyPath(c, envID)
	if httputils.IsWebSocketUpgradeRequest(c.Request()) {
		return edge.ProxyWebSocketRequest(c, tunnel, proxyPath, m.checkOrigin)
	}
	return edge.ProxyHTTPRequest(c, tunnel, proxyPath)
}

// hasResourcePath reports whether the request targets a proxiable resource path.
func (m *EnvironmentMiddleware) hasResourcePath(c *echo.Context, envID string) bool {
	suffix, ok := strings.CutPrefix(c.Request().URL.Path, apiEnvironmentsPrefix+envID)
	if !ok || len(suffix) <= 1 || suffix[0] != '/' {
		return false
	}
	return !isManagementPathInternal(c.Request().Method, suffix)
}

func isManagementPathInternal(method, suffix string) bool {
	if isCentralSwarmManagementPathInternal(method, suffix) {
		return true
	}
	if suffix == "/activities" || strings.HasPrefix(suffix, "/activities/") {
		return true
	}

	if strings.HasPrefix(suffix, "/notifications") {
		return true
	}

	// Webhooks are managed centrally: rows live in the manager DB (keyed by the
	// real environment ID) and the public trigger endpoint resolves tokens there.
	if suffix == "/webhooks" || strings.HasPrefix(suffix, "/webhooks/") {
		return true
	}

	if strings.HasPrefix(suffix, "/deployment/mtls/") {
		return true
	}

	_, isManagement := managementEndpointSet[suffix]
	return isManagement
}

func isCentralSwarmManagementPathInternal(method, suffix string) bool {
	if method == http.MethodGet && (suffix == "/swarm/join-candidates" || suffix == "/swarm/nodes") {
		return true
	}
	if method == http.MethodPost && (suffix == "/swarm/join-environments" || suffix == "/swarm/nodes/agents/reconcile") {
		return true
	}

	parts := strings.Split(strings.Trim(suffix, "/"), "/")
	if len(parts) == 3 && method == http.MethodGet && parts[0] == "swarm" && parts[1] == "nodes" {
		return true
	}
	if len(parts) == 5 && parts[0] == "swarm" && parts[1] == "nodes" && parts[3] == "agent" {
		if parts[4] == "binding" {
			return method == http.MethodPut || method == http.MethodDelete
		}
		if parts[4] == "deployment" {
			return method == http.MethodPost || method == http.MethodDelete
		}
	}

	return false
}

// extractEnvironmentID gets the environment ID from the request.
func (m *EnvironmentMiddleware) extractEnvironmentID(c *echo.Context) string {
	requestPath := c.Request().URL.Path

	if !strings.Contains(requestPath, environmentsPathMarker) {
		return ""
	}

	if envID := c.Param(m.paramName); envID != "" {
		return envID
	}

	if _, rest, ok := strings.Cut(requestPath, environmentsPathMarker); ok {
		if envID, _, _ := strings.Cut(rest, "/"); envID != "" {
			return envID
		}
	}

	return ""
}

// buildResourceSuffix extracts the resource path after stripping the environment ID prefix.
func (m *EnvironmentMiddleware) buildResourceSuffix(requestPath, envID string) string {
	suffix, _ := strings.CutPrefix(requestPath, apiEnvironmentsPrefix+envID)
	if suffix != "" && suffix[0] != '/' {
		suffix = "/" + suffix
	}
	return suffix
}

// buildTargetURL constructs the full proxy target URL for a remote environment.
func (m *EnvironmentMiddleware) buildTargetURL(c *echo.Context, envID, apiURL string) string {
	req := c.Request()
	suffix := m.buildResourceSuffix(req.URL.Path, envID)
	target := strings.TrimRight(apiURL, "/") + path.Join(apiEnvironmentsPrefix, m.localID) + suffix
	if qs := req.URL.RawQuery; qs != "" {
		target += "?" + qs
	}
	return target
}

// buildProxyPath constructs the path sent through the edge tunnel.
func (m *EnvironmentMiddleware) buildProxyPath(c *echo.Context, envID string) string {
	return path.Join(apiEnvironmentsPrefix, m.localID) + m.buildResourceSuffix(c.Request().URL.Path, envID)
}

func isEdgeEnvironmentURLInternal(apiURL string) bool {
	normalized := strings.ToLower(strings.TrimSpace(apiURL))
	return strings.HasPrefix(normalized, "edge://")
}

func (m *EnvironmentMiddleware) getActiveEdgeTunnelInternal(envID string) mo.Option[*edge.AgentTunnel] {
	if m.registry == nil {
		return mo.None[*edge.AgentTunnel]()
	}

	tunnel, ok := m.registry.Get(envID).Get()
	if !ok || tunnel == nil || tunnel.Conn == nil || tunnel.Conn.IsClosed() {
		return mo.None[*edge.AgentTunnel]()
	}
	return mo.Some(tunnel)
}

func (m *EnvironmentMiddleware) waitForActiveEdgeTunnelInternal(ctx context.Context, envID string, timeout time.Duration) mo.Option[*edge.AgentTunnel] {
	if timeout <= 0 {
		return m.getActiveEdgeTunnelInternal(envID)
	}

	if tunnel, ok := m.getActiveEdgeTunnelInternal(envID).Get(); ok {
		return mo.Some(tunnel)
	}

	waitCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	ticker := time.NewTicker(edge.DefaultTunnelAcquirePollEvery)
	defer ticker.Stop()

	for {
		select {
		case <-waitCtx.Done():
			return mo.None[*edge.AgentTunnel]()
		case <-ticker.C:
			if tunnel, ok := m.getActiveEdgeTunnelInternal(envID).Get(); ok {
				return mo.Some(tunnel)
			}
		}
	}
}

func (m *EnvironmentMiddleware) abortEdgeTunnelUnavailable(c *echo.Context) error {
	return c.JSON(http.StatusBadGateway, map[string]any{
		"success": false,
		"data": map[string]any{
			"error": "Edge agent is not connected",
		},
	})
}

// proxyWebSocket handles WebSocket proxy requests.
func (m *EnvironmentMiddleware) proxyWebSocket(c *echo.Context, target string, accessToken *string, envID string) error {
	if isEdgeEnvironmentURLInternal(target) {
		slog.WarnContext(c.Request().Context(), "Refusing direct websocket proxy to edge environment without active tunnel", "environment_id", envID, "target", target)
		return m.abortEdgeTunnelUnavailable(c)
	}

	wsTarget := edge.HTTPToWebSocketURL(target)
	headers := edge.BuildWebSocketHeaders(c, accessToken)

	if err := wsutil.ProxyHTTP(c.Response(), c.Request(), wsTarget, headers, m.checkOrigin); err != nil {
		slog.Error("websocket proxy failed", "err", err)
	}
	return nil
}

// proxyHTTP handles standard HTTP proxy requests.
func (m *EnvironmentMiddleware) proxyHTTP(c *echo.Context, target string, accessToken *string) error {
	if isEdgeEnvironmentURLInternal(target) {
		slog.WarnContext(c.Request().Context(), "Refusing direct HTTP proxy to edge environment without active tunnel", "target", target)
		return m.abortEdgeTunnelUnavailable(c)
	}

	req, err := m.createProxyRequest(c, target, accessToken)
	if err != nil {
		errMessage := errors.WithMessage(err, "Failed to create proxy request").Error()
		if errors.Is(err, common.ErrEnvironmentInvalidProxyTarget) {
			errMessage = err.Error()
		}
		return c.JSON(http.StatusInternalServerError, map[string]any{
			"success": false,
			"data":    map[string]any{"error": errMessage},
		})
	}

	resp, err := m.httpClient.Do(req)
	if err != nil {
		return c.JSON(http.StatusBadGateway, map[string]any{
			"success": false,
			"data":    map[string]any{"error": errors.WithMessage(err, "Proxy request failed").Error()},
		})
	}
	defer func() { _ = resp.Body.Close() }()

	m.writeProxyResponse(c, resp)
	return nil
}

// createProxyRequest builds the HTTP request to forward to the remote environment.
func (m *EnvironmentMiddleware) createProxyRequest(c *echo.Context, target string, accessToken *string) (*http.Request, error) {
	srcReq := c.Request()
	validatedTarget, err := httputils.ValidateOutboundHTTPURL(target)
	if err != nil {
		return nil, common.Classify(common.ErrEnvironmentInvalidProxyTarget, errors.WrapIf(err, "Invalid proxy target URL"))
	}

	// The body is streamed straight through rather than buffered: volume backup
	// and image import uploads run to gigabytes, and reading them into memory
	// cost roughly twice their size per in-flight request. This drops GetBody,
	// so the transport can no longer replay the body across a redirect or an
	// idle-connection retry; a proxied API call has no legitimate need for
	// either, and a failure surfaces as a 502 rather than silent corruption.
	//
	// A server request's ContentLength is 0 only when there is genuinely no
	// body (-1 means chunked/unknown), so it is the safe signal for whether to
	// forward one at all.
	var requestBody io.ReadCloser
	var contentLength int64
	switch {
	case srcReq.Body == nil:
	case srcReq.ContentLength != 0:
		requestBody, contentLength = srcReq.Body, srcReq.ContentLength
	default:
		_ = srcReq.Body.Close()
	}

	// The body is deliberately not logged: at debug level it would put compose
	// files and registry credentials into the ring buffer served by
	// /api/diagnostics/logs, and stringifying it allocated even when debug was off.
	slog.DebugContext(srcReq.Context(), "Creating proxy request", "method", srcReq.Method, "target", target, "contentLength", srcReq.ContentLength, "contentType", srcReq.Header.Get("Content-Type"))

	requestURL := *validatedTarget
	req := (&http.Request{
		Method:        srcReq.Method,
		URL:           &requestURL,
		Host:          requestURL.Host,
		Header:        make(http.Header),
		Body:          requestBody,
		ContentLength: contentLength,
	}).WithContext(srcReq.Context())

	skip := edge.GetSkipHeaders()
	edge.CopyRequestHeaders(srcReq.Header, req.Header, skip)
	edge.SetAuthHeader(req, c)
	edge.SetAgentToken(req, accessToken)
	edge.SetForwardedHeaders(req, c.RealIP(), srcReq.Host)

	return req, nil
}

// writeProxyResponse copies the proxy response back to the client.
func (m *EnvironmentMiddleware) writeProxyResponse(c *echo.Context, resp *http.Response) {
	w := c.Response()
	hopByHop := edge.BuildHopByHopHeaders(resp.Header)
	edge.CopyResponseHeaders(resp.Header, w.Header(), hopByHop)

	w.WriteHeader(resp.StatusCode)
	if c.Request().Method != http.MethodHead {
		edge.CopyBodyWithFlush(w, resp.Body)
	}
}
