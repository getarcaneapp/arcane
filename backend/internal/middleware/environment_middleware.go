package middleware

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"path"
	"strings"
	"time"

	"github.com/getarcaneapp/arcane/backend/internal/services"
	"github.com/getarcaneapp/arcane/backend/internal/utils/edge"
	"github.com/getarcaneapp/arcane/backend/internal/utils/remenv"
	wsutil "github.com/getarcaneapp/arcane/backend/internal/utils/ws"
	"github.com/gin-gonic/gin"
)

const (
	apiEnvironmentsPrefix  = "/api/environments/"
	environmentsPathMarker = "/environments/"

	errEnvironmentNotFound      = "Environment not found"
	errEnvironmentDisabled      = "Environment is disabled"
	errFailedCreateProxyRequest = "Failed to create proxy request"
	errProxyRequestFailedPrefix = "Proxy request failed:"
	errUnauthorized             = "Authentication required to access remote environments"

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
type AuthValidator func(ctx context.Context, c *gin.Context) bool

// EnvironmentMiddleware proxies requests for remote environments to their respective agents.
type EnvironmentMiddleware struct {
	localID       string
	paramName     string
	resolver      EnvResolver
	authValidator AuthValidator
	envService    *services.EnvironmentService
	httpClient    *http.Client
}

// NewEnvProxyMiddlewareWithParam creates middleware that proxies requests to remote environments.
// - localID: the ID representing the local environment (requests to this ID are not proxied)
// - paramName: the URL parameter name containing the environment ID (e.g., "id")
// - resolver: function to resolve environment ID to connection details
// - envService: environment service for additional lookups
// - authValidator: function to validate authentication before proxying (required for security)
func NewEnvProxyMiddlewareWithParam(localID, paramName string, resolver EnvResolver, envService *services.EnvironmentService, authValidator AuthValidator) gin.HandlerFunc {
	m := &EnvironmentMiddleware{
		localID:       localID,
		paramName:     paramName,
		resolver:      resolver,
		authValidator: authValidator,
		envService:    envService,
		httpClient:    &http.Client{Timeout: proxyTimeout},
	}
	return m.Handle
}

// Handle is the main middleware handler.
func (m *EnvironmentMiddleware) Handle(c *gin.Context) {
	envID := m.extractEnvironmentID(c)

	// Local environment or no environment - continue to next handler
	if envID == "" || envID == m.localID {
		c.Next()
		return
	}

	// Only proxy requests with additional path segments after the environment ID
	// Examples: /api/environments/{id}/containers, /api/environments/{id}/projects
	// Not proxied: /api/environments/{id} (management operations)
	if !m.hasResourcePath(c, envID) {
		c.Next()
		return
	}

	// SECURITY: Validate authentication BEFORE proxying to remote environments.
	// The proxy attaches the agent token to forwarded requests, which grants full access
	// on the remote agent. Without this check, unauthenticated users could access
	// remote environment resources.
	if m.authValidator != nil && !m.authValidator(c.Request.Context(), c) {
		c.JSON(http.StatusUnauthorized, gin.H{
			"success": false,
			"data":    gin.H{"error": errUnauthorized},
		})
		c.Abort()
		return
	}

	// Resolve remote environment
	apiURL, accessToken, enabled, err := m.resolver(c.Request.Context(), envID)
	if err != nil || apiURL == "" {
		c.JSON(http.StatusNotFound, gin.H{
			"success": false,
			"data":    gin.H{"error": errEnvironmentNotFound},
		})
		c.Abort()
		return
	}

	if !enabled {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"data":    gin.H{"error": errEnvironmentDisabled},
		})
		c.Abort()
		return
	}

	// Build target URL and proxy the request
	target := m.buildTargetURL(c, envID, apiURL)

	// Check if this environment has an active edge tunnel
	if tunnel, ok := edge.GetRegistry().Get(envID); ok && !tunnel.Conn.IsClosed() {
		slog.DebugContext(c.Request.Context(), "Routing request through edge tunnel", "environment_id", envID, "path", c.Request.URL.Path)

		// Inject agent token into request headers before proxying through the tunnel.
		// ProxyHTTPRequest and ProxyWebSocketRequest copy headers from c.Request.Header,
		// so setting the token here ensures the agent receives proper authentication.
		// Without this, the agent's agentAuth middleware rejects requests with 401
		// because the browser's session cookies are not valid on the agent.
		if accessToken != nil && *accessToken != "" {
			c.Request.Header.Set(remenv.HeaderAgentToken, *accessToken)
			c.Request.Header.Set(remenv.HeaderAPIKey, *accessToken)
		}

		proxyPath := m.buildProxyPath(c, envID)
		if m.isWebSocketUpgrade(c) {
			// Route WebSocket through the edge tunnel
			edge.ProxyWebSocketRequest(c, tunnel, proxyPath)
		} else {
			edge.ProxyHTTPRequest(c, tunnel, proxyPath)
		}
		c.Abort()
		return
	}

	if m.isWebSocketUpgrade(c) {
		m.proxyWebSocket(c, target, accessToken, envID)
	} else {
		m.proxyHTTP(c, target, accessToken)
	}
}

// hasResourcePath reports whether the request targets a proxiable resource path.
// Returns true for paths like /api/environments/{id}/containers (should be proxied).
// Returns false for /api/environments/{id} exactly or any management endpoint.
func (m *EnvironmentMiddleware) hasResourcePath(c *gin.Context, envID string) bool {
	suffix, ok := strings.CutPrefix(c.Request.URL.Path, apiEnvironmentsPrefix+envID)
	if !ok || len(suffix) <= 1 || suffix[0] != '/' {
		return false
	}
	_, isManagement := managementEndpointSet[suffix]
	return !isManagement
}

// extractEnvironmentID gets the environment ID from the request.
// Only processes paths containing "/environments/" to avoid conflicts with other routes.
func (m *EnvironmentMiddleware) extractEnvironmentID(c *gin.Context) string {
	requestPath := c.Request.URL.Path

	// Skip non-environment routes (e.g., /api-keys/{id})
	if !strings.Contains(requestPath, environmentsPathMarker) {
		return ""
	}

	// Try path parameter first
	if envID := c.Param(m.paramName); envID != "" {
		return envID
	}

	// Fall back to parsing the URL path
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
func (m *EnvironmentMiddleware) buildTargetURL(c *gin.Context, envID, apiURL string) string {
	suffix := m.buildResourceSuffix(c.Request.URL.Path, envID)
	target := strings.TrimRight(apiURL, "/") + path.Join(apiEnvironmentsPrefix, m.localID) + suffix
	if qs := c.Request.URL.RawQuery; qs != "" {
		target += "?" + qs
	}
	return target
}

// buildProxyPath constructs the path sent through the edge tunnel.
// Includes the /api/environments/{localID} prefix so the agent can route it properly.
func (m *EnvironmentMiddleware) buildProxyPath(c *gin.Context, envID string) string {
	return path.Join(apiEnvironmentsPrefix, m.localID) + m.buildResourceSuffix(c.Request.URL.Path, envID)
}

// isWebSocketUpgrade checks if this is a WebSocket upgrade request.
func (m *EnvironmentMiddleware) isWebSocketUpgrade(c *gin.Context) bool {
	return strings.EqualFold(c.GetHeader(remenv.HeaderUpgrade), "websocket") ||
		strings.Contains(strings.ToLower(c.GetHeader(remenv.HeaderConnection)), remenv.ConnectionUpgradeToken)
}

// proxyWebSocket handles WebSocket proxy requests.
func (m *EnvironmentMiddleware) proxyWebSocket(c *gin.Context, target string, accessToken *string, envID string) {
	wsTarget := remenv.HTTPToWebSocketURL(target)
	headers := remenv.BuildWebSocketHeaders(c, accessToken)

	if err := wsutil.ProxyHTTP(c.Writer, c.Request, wsTarget, headers); err != nil {
		slog.Error("websocket proxy failed", "err", err)
	}
	c.Abort()
}

// proxyHTTP handles standard HTTP proxy requests.
func (m *EnvironmentMiddleware) proxyHTTP(c *gin.Context, target string, accessToken *string) {
	req, err := m.createProxyRequest(c, target, accessToken)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"success": false,
			"data":    gin.H{"error": errFailedCreateProxyRequest},
		})
		c.Abort()
		return
	}

	resp, err := m.httpClient.Do(req) //nolint:gosec // intentional proxy request to resolved remote environment URL
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{
			"success": false,
			"data":    gin.H{"error": fmt.Sprintf("%s %v", errProxyRequestFailedPrefix, err)},
		})
		c.Abort()
		return
	}
	defer func() { _ = resp.Body.Close() }()

	m.writeProxyResponse(c, resp)
	c.Abort()
}

// createProxyRequest builds the HTTP request to forward to the remote environment.
func (m *EnvironmentMiddleware) createProxyRequest(c *gin.Context, target string, accessToken *string) (*http.Request, error) {
	var bodyBytes []byte
	if c.Request.Body != nil {
		var err error
		bodyBytes, err = io.ReadAll(c.Request.Body)
		_ = c.Request.Body.Close()
		if err != nil {
			return nil, fmt.Errorf("failed to read request body: %w", err)
		}
	}

	slog.DebugContext(c.Request.Context(), "Creating proxy request", "method", c.Request.Method, "target", target, "contentLength", c.Request.ContentLength, "contentType", c.GetHeader("Content-Type"), "bodyLength", len(bodyBytes), "body", string(bodyBytes))

	// bytes.NewReader is preferred over bytes.NewBuffer: it implements io.ReadSeeker
	// and avoids the internal grow-buffer logic that NewBuffer carries.
	var body io.Reader
	if len(bodyBytes) > 0 {
		body = bytes.NewReader(bodyBytes)
	}
	req, err := http.NewRequestWithContext(c.Request.Context(), c.Request.Method, target, body)
	if err != nil {
		return nil, err
	}

	skip := remenv.GetSkipHeaders()
	remenv.CopyRequestHeaders(c.Request.Header, req.Header, skip)
	remenv.SetAuthHeader(req, c)
	remenv.SetAgentToken(req, accessToken)
	remenv.SetForwardedHeaders(req, c.ClientIP(), c.Request.Host)

	// Set Content-Length based on actual body size
	if len(bodyBytes) > 0 {
		req.ContentLength = int64(len(bodyBytes))
	}

	return req, nil
}

// writeProxyResponse copies the proxy response back to the client.
func (m *EnvironmentMiddleware) writeProxyResponse(c *gin.Context, resp *http.Response) {
	hopByHop := remenv.BuildHopByHopHeaders(resp.Header)
	remenv.CopyResponseHeaders(resp.Header, c.Writer.Header(), hopByHop)

	c.Status(resp.StatusCode)
	if c.Request.Method != http.MethodHead {
		// Ensure headers are sent before streaming the body.
		// This is critical for streaming responses (e.g., JSON line streams) where
		// clients expect incremental updates.
		c.Writer.WriteHeaderNow()
		remenv.CopyBodyWithFlush(c.Writer, resp.Body)
	}
}
