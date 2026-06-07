package middleware

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"path"
	"strings"
	"time"

	"github.com/getarcaneapp/arcane/backend/internal/common"
	"github.com/getarcaneapp/arcane/backend/pkg/libarcane/edge"
	wsutil "github.com/getarcaneapp/arcane/backend/pkg/libarcane/ws"
	httputils "github.com/getarcaneapp/arcane/backend/pkg/utils/httpx"
	"github.com/gorilla/websocket"
	"github.com/labstack/echo/v4"
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

// AuthValidator validates authentication for a request.
// Returns true if the request is authenticated, false otherwise.
type AuthValidator func(ctx context.Context, c echo.Context) bool

// EnvironmentMiddleware proxies requests for remote environments to their respective agents.
type EnvironmentMiddleware struct {
	localID       string
	paramName     string
	resolver      EnvResolver
	authValidator AuthValidator
	httpClient    *http.Client
	registry      *edge.TunnelRegistry
}

// NewEnvProxyMiddlewareWithParam creates middleware that proxies requests to remote environments.
func NewEnvProxyMiddlewareWithParam(localID, paramName string, resolver EnvResolver, authValidator AuthValidator) echo.MiddlewareFunc {
	return NewEnvProxyMiddlewareWithParamAndRegistry(localID, paramName, resolver, authValidator, edge.GetRegistry())
}

// NewEnvProxyMiddlewareWithParamAndRegistry creates middleware with an injected tunnel registry.
func NewEnvProxyMiddlewareWithParamAndRegistry(
	localID,
	paramName string,
	resolver EnvResolver,
	authValidator AuthValidator,
	registry *edge.TunnelRegistry,
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
	}
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			return m.Handle(c, next)
		}
	}
}

// Handle is the main middleware handler.
func (m *EnvironmentMiddleware) Handle(c echo.Context, next echo.HandlerFunc) error {
	envID := m.extractEnvironmentID(c)

	if envID == "" || envID == m.localID {
		return next(c)
	}

	if !m.hasResourcePath(c, envID) {
		return next(c)
	}

	// SECURITY: Validate authentication BEFORE proxying to remote environments.
	if m.authValidator != nil && !m.authValidator(c.Request().Context(), c) {
		return c.JSON(http.StatusUnauthorized, map[string]any{
			"success": false,
			"data":    map[string]any{"error": (&common.EnvironmentUnauthorizedError{}).Error()},
		})
	}

	apiURL, accessToken, enabled, err := m.resolver(c.Request().Context(), envID)
	if err != nil || apiURL == "" {
		return c.JSON(http.StatusNotFound, map[string]any{
			"success": false,
			"data":    map[string]any{"error": (&common.EnvironmentNotFoundError{}).Error()},
		})
	}

	if !enabled {
		return c.JSON(http.StatusBadRequest, map[string]any{
			"success": false,
			"data":    map[string]any{"error": (&common.EnvironmentDisabledError{}).Error()},
		})
	}

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

	if m.isWebSocketUpgrade(c) {
		return m.proxyWebSocket(c, target, accessToken, envID)
	}
	return m.proxyHTTP(c, target, accessToken)
}

func (m *EnvironmentMiddleware) proxyActiveEdgeTunnelInternal(c echo.Context, envID string, accessToken *string) (bool, error) {
	tunnel, ok := m.getActiveEdgeTunnelInternal(envID)
	if !ok {
		return false, nil
	}

	slog.DebugContext(c.Request().Context(), "Routing request through edge tunnel", "environment_id", envID, "path", c.Request().URL.Path)
	m.setProxyContextHeadersInternal(c, accessToken)
	return true, m.proxyThroughTunnelInternal(c, tunnel, envID)
}

func (m *EnvironmentMiddleware) proxyRecoveredEdgeTunnelInternal(c echo.Context, envID string, accessToken *string) (bool, error) {
	edge.TouchTunnelDemand(envID, edge.DefaultTunnelDemandTTL)

	tunnel, ok := m.waitForActiveEdgeTunnelInternal(c.Request().Context(), envID, edge.DefaultTunnelAcquireTimeout())
	if !ok {
		return false, nil
	}

	slog.InfoContext(c.Request().Context(), "Recovered edge tunnel during request", "environment_id", envID)
	m.setProxyContextHeadersInternal(c, accessToken)
	return true, m.proxyThroughTunnelInternal(c, tunnel, envID)
}

func (m *EnvironmentMiddleware) setProxyContextHeadersInternal(c echo.Context, accessToken *string) {
	if accessToken != nil && *accessToken != "" {
		c.Request().Header.Set(edge.HeaderAgentToken, *accessToken)
		c.Request().Header.Set(edge.HeaderAPIKey, *accessToken)
	}
}

func (m *EnvironmentMiddleware) proxyThroughTunnelInternal(c echo.Context, tunnel *edge.AgentTunnel, envID string) error {
	proxyPath := m.buildProxyPath(c, envID)
	if m.isWebSocketUpgrade(c) {
		return edge.ProxyWebSocketRequest(c, tunnel, proxyPath)
	}
	return edge.ProxyHTTPRequest(c, tunnel, proxyPath)
}

// hasResourcePath reports whether the request targets a proxiable resource path.
func (m *EnvironmentMiddleware) hasResourcePath(c echo.Context, envID string) bool {
	suffix, ok := strings.CutPrefix(c.Request().URL.Path, apiEnvironmentsPrefix+envID)
	if !ok || len(suffix) <= 1 || suffix[0] != '/' {
		return false
	}
	return !isManagementPathInternal(suffix)
}

func isManagementPathInternal(suffix string) bool {
	if strings.HasPrefix(suffix, "/notifications") {
		return true
	}

	if strings.HasPrefix(suffix, "/deployment/mtls/") {
		return true
	}

	_, isManagement := managementEndpointSet[suffix]
	return isManagement
}

// extractEnvironmentID gets the environment ID from the request.
func (m *EnvironmentMiddleware) extractEnvironmentID(c echo.Context) string {
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
func (m *EnvironmentMiddleware) buildTargetURL(c echo.Context, envID, apiURL string) string {
	req := c.Request()
	suffix := m.buildResourceSuffix(req.URL.Path, envID)
	target := strings.TrimRight(apiURL, "/") + path.Join(apiEnvironmentsPrefix, m.localID) + suffix
	if qs := req.URL.RawQuery; qs != "" {
		target += "?" + qs
	}
	return target
}

// buildProxyPath constructs the path sent through the edge tunnel.
func (m *EnvironmentMiddleware) buildProxyPath(c echo.Context, envID string) string {
	return path.Join(apiEnvironmentsPrefix, m.localID) + m.buildResourceSuffix(c.Request().URL.Path, envID)
}

// isWebSocketUpgrade checks if this is a WebSocket upgrade request.
func (m *EnvironmentMiddleware) isWebSocketUpgrade(c echo.Context) bool {
	return websocket.IsWebSocketUpgrade(c.Request())
}

func isEdgeEnvironmentURLInternal(apiURL string) bool {
	normalized := strings.ToLower(strings.TrimSpace(apiURL))
	return strings.HasPrefix(normalized, "edge://")
}

func (m *EnvironmentMiddleware) getActiveEdgeTunnelInternal(envID string) (*edge.AgentTunnel, bool) {
	if m.registry == nil {
		return nil, false
	}

	tunnel, ok := m.registry.Get(envID)
	if !ok || tunnel == nil || tunnel.Conn == nil || tunnel.Conn.IsClosed() {
		return nil, false
	}
	return tunnel, true
}

func (m *EnvironmentMiddleware) waitForActiveEdgeTunnelInternal(ctx context.Context, envID string, timeout time.Duration) (*edge.AgentTunnel, bool) {
	if timeout <= 0 {
		return m.getActiveEdgeTunnelInternal(envID)
	}

	if tunnel, ok := m.getActiveEdgeTunnelInternal(envID); ok {
		return tunnel, true
	}

	waitCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	ticker := time.NewTicker(edge.DefaultTunnelAcquirePollEvery)
	defer ticker.Stop()

	for {
		select {
		case <-waitCtx.Done():
			return nil, false
		case <-ticker.C:
			if tunnel, ok := m.getActiveEdgeTunnelInternal(envID); ok {
				return tunnel, true
			}
		}
	}
}

func (m *EnvironmentMiddleware) abortEdgeTunnelUnavailable(c echo.Context) error {
	return c.JSON(http.StatusBadGateway, map[string]any{
		"success": false,
		"data": map[string]any{
			"error": (&common.EdgeAgentNotConnectedError{}).Error(),
		},
	})
}

// proxyWebSocket handles WebSocket proxy requests.
func (m *EnvironmentMiddleware) proxyWebSocket(c echo.Context, target string, accessToken *string, envID string) error {
	if isEdgeEnvironmentURLInternal(target) {
		slog.WarnContext(c.Request().Context(), "Refusing direct websocket proxy to edge environment without active tunnel", "environment_id", envID, "target", target)
		return m.abortEdgeTunnelUnavailable(c)
	}

	wsTarget := edge.HTTPToWebSocketURL(target)
	headers := edge.BuildWebSocketHeaders(c, accessToken)

	if err := wsutil.ProxyHTTP(c.Response().Writer, c.Request(), wsTarget, headers); err != nil {
		slog.Error("websocket proxy failed", "err", err)
	}
	return nil
}

// proxyHTTP handles standard HTTP proxy requests.
func (m *EnvironmentMiddleware) proxyHTTP(c echo.Context, target string, accessToken *string) error {
	if isEdgeEnvironmentURLInternal(target) {
		slog.WarnContext(c.Request().Context(), "Refusing direct HTTP proxy to edge environment without active tunnel", "target", target)
		return m.abortEdgeTunnelUnavailable(c)
	}

	req, err := m.createProxyRequest(c, target, accessToken)
	if err != nil {
		errMessage := (&common.EnvironmentProxyRequestCreationError{Err: err}).Error()
		var invalidTargetErr *common.EnvironmentInvalidProxyTargetError
		if errors.As(err, &invalidTargetErr) {
			errMessage = invalidTargetErr.Error()
		}
		return c.JSON(http.StatusInternalServerError, map[string]any{
			"success": false,
			"data":    map[string]any{"error": errMessage},
		})
	}

	resp, err := m.httpClient.Do(req) //nolint:gosec // intentional proxy request to resolved remote environment URL
	if err != nil {
		return c.JSON(http.StatusBadGateway, map[string]any{
			"success": false,
			"data":    map[string]any{"error": (&common.EnvironmentProxyRequestFailedError{Err: err}).Error()},
		})
	}
	defer func() { _ = resp.Body.Close() }()

	m.writeProxyResponse(c, resp)
	return nil
}

// createProxyRequest builds the HTTP request to forward to the remote environment.
func (m *EnvironmentMiddleware) createProxyRequest(c echo.Context, target string, accessToken *string) (*http.Request, error) {
	srcReq := c.Request()
	validatedTarget, err := httputils.ValidateOutboundHTTPURL(target)
	if err != nil {
		return nil, &common.EnvironmentInvalidProxyTargetError{Err: err}
	}

	var bodyBytes []byte
	if srcReq.Body != nil {
		var err error
		bodyBytes, err = io.ReadAll(srcReq.Body)
		_ = srcReq.Body.Close()
		if err != nil {
			return nil, fmt.Errorf("failed to read request body: %w", err)
		}
	}

	slog.DebugContext(srcReq.Context(), "Creating proxy request", "method", srcReq.Method, "target", target, "contentLength", srcReq.ContentLength, "contentType", srcReq.Header.Get("Content-Type"), "bodyLength", len(bodyBytes), "body", string(bodyBytes))

	var requestBody io.ReadCloser
	var getBody func() (io.ReadCloser, error)
	if len(bodyBytes) > 0 {
		requestBody = io.NopCloser(bytes.NewReader(bodyBytes))
		getBody = func() (io.ReadCloser, error) {
			return io.NopCloser(bytes.NewReader(bodyBytes)), nil
		}
	}
	requestURL := *validatedTarget
	req := (&http.Request{
		Method:  srcReq.Method,
		URL:     &requestURL,
		Host:    requestURL.Host,
		Header:  make(http.Header),
		Body:    requestBody,
		GetBody: getBody,
	}).WithContext(srcReq.Context())

	skip := edge.GetSkipHeaders()
	edge.CopyRequestHeaders(srcReq.Header, req.Header, skip)
	edge.SetAuthHeader(req, c)
	edge.SetAgentToken(req, accessToken)
	edge.SetForwardedHeaders(req, c.RealIP(), srcReq.Host)

	if len(bodyBytes) > 0 {
		req.ContentLength = int64(len(bodyBytes))
	}

	return req, nil
}

// writeProxyResponse copies the proxy response back to the client.
func (m *EnvironmentMiddleware) writeProxyResponse(c echo.Context, resp *http.Response) {
	w := c.Response().Writer
	hopByHop := edge.BuildHopByHopHeaders(resp.Header)
	edge.CopyResponseHeaders(resp.Header, w.Header(), hopByHop)

	w.WriteHeader(resp.StatusCode)
	if c.Request().Method != http.MethodHead {
		edge.CopyBodyWithFlush(w, resp.Body)
	}
}
