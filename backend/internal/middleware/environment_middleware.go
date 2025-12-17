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
	"github.com/getarcaneapp/arcane/backend/internal/utils/remenv"
	wsutil "github.com/getarcaneapp/arcane/backend/internal/utils/ws"
	"github.com/gin-gonic/gin"
)

const (
	apiEnvironmentsPrefix  = "/api/environments/"
	environmentsPathMarker = "/environments/"

	managementEndpointTest           = "/test"
	managementEndpointHeartbeat      = "/heartbeat"
	managementEndpointSyncRegistries = "/sync-registries"
	managementEndpointDeployment     = "/deployment"
	managementEndpointAgentPair      = "/agent/pair"

	errEnvironmentNotFound      = "Environment not found"
	errEnvironmentDisabled      = "Environment is disabled"
	errFailedCreateProxyRequest = "Failed to create proxy request"
	errProxyRequestFailedPrefix = "Proxy request failed:"

	// proxyTimeout is intentionally generous because some proxied operations
	// (e.g., image pulls with progress streaming) can take multiple minutes.
	proxyTimeout = 30 * time.Minute
)

// EnvResolver resolves an environment ID to its connection details.
// Returns: apiURL, accessToken, enabled, error
type EnvResolver func(ctx context.Context, id string) (string, *string, bool, error)

// EnvironmentMiddleware proxies requests for remote environments to their respective agents.
type EnvironmentMiddleware struct {
	localID    string
	paramName  string
	resolver   EnvResolver
	envService *services.EnvironmentService
	httpClient *http.Client
}

// NewEnvProxyMiddlewareWithParam creates middleware that proxies requests to remote environments.
// - localID: the ID representing the local environment (requests to this ID are not proxied)
// - paramName: the URL parameter name containing the environment ID (e.g., "id")
// - resolver: function to resolve environment ID to connection details
// - envService: environment service for additional lookups
func NewEnvProxyMiddlewareWithParam(localID, paramName string, resolver EnvResolver, envService *services.EnvironmentService) gin.HandlerFunc {
	m := &EnvironmentMiddleware{
		localID:    localID,
		paramName:  paramName,
		resolver:   resolver,
		envService: envService,
		httpClient: &http.Client{Timeout: proxyTimeout},
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

	if m.isWebSocketUpgrade(c) {
		m.proxyWebSocket(c, target, accessToken, envID)
	} else {
		m.proxyHTTP(c, target, accessToken)
	}
}

// hasResourcePath checks if the request has additional path segments after the environment ID.
// Returns true for paths like /api/environments/{id}/containers (should be proxied)
// Returns false for paths like /api/environments/{id} or management endpoints (should be handled locally)
func (m *EnvironmentMiddleware) hasResourcePath(c *gin.Context, envID string) bool {
	path := c.Request.URL.Path
	prefix := apiEnvironmentsPrefix + envID

	// If there's content after the environment ID path, check if it's a management endpoint
	suffix := strings.TrimPrefix(path, prefix)

	// No suffix means it's exactly /api/environments/{id} - management endpoint
	if len(suffix) <= 1 || !strings.HasPrefix(suffix, "/") {
		return false
	}

	// Check if it's a management endpoint that should NOT be proxied
	// These are environment management operations handled by the manager
	managementEndpoints := []string{
		managementEndpointTest,
		managementEndpointHeartbeat,
		managementEndpointSyncRegistries,
		managementEndpointDeployment,
		managementEndpointAgentPair,
	}

	for _, endpoint := range managementEndpoints {
		if suffix == endpoint {
			return false
		}
	}

	// It's a resource operation (e.g., "/containers", "/images") - should be proxied
	return true
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
	if idx := strings.Index(requestPath, environmentsPathMarker); idx >= 0 {
		rest := requestPath[idx+len(environmentsPathMarker):]
		if parts := strings.SplitN(rest, "/", 2); len(parts) > 0 && parts[0] != "" {
			return parts[0]
		}
	}

	return ""
}

// buildTargetURL constructs the proxy target URL.
func (m *EnvironmentMiddleware) buildTargetURL(c *gin.Context, envID, apiURL string) string {
	// Remove the environment prefix from the path
	prefix := apiEnvironmentsPrefix + envID
	suffix := strings.TrimPrefix(c.Request.URL.Path, prefix)
	if suffix != "" && !strings.HasPrefix(suffix, "/") {
		suffix = "/" + suffix
	}

	// Build target: apiURL + /api/environments/{localID} + suffix
	target := strings.TrimRight(apiURL, "/") + path.Join(apiEnvironmentsPrefix, m.localID) + suffix

	// Append query string if present
	if qs := c.Request.URL.RawQuery; qs != "" {
		target += "?" + qs
	}

	return target
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
		slog.Error("websocket proxy failed", "env_id", envID, "target", wsTarget, "err", err)
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

	resp, err := m.httpClient.Do(req)
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{
			"success": false,
			"data":    gin.H{"error": fmt.Sprintf("%s %v", errProxyRequestFailedPrefix, err)},
		})
		c.Abort()
		return
	}
	defer resp.Body.Close()

	m.writeProxyResponse(c, resp)
	c.Abort()
}

// createProxyRequest builds the HTTP request to forward to the remote environment.
func (m *EnvironmentMiddleware) createProxyRequest(c *gin.Context, target string, accessToken *string) (*http.Request, error) {
	// Read the body to log it and then restore it for forwarding
	var bodyBytes []byte
	var err error
	if c.Request.Body != nil {
		bodyBytes, err = io.ReadAll(c.Request.Body)
		if err != nil {
			return nil, fmt.Errorf("failed to read request body: %w", err)
		}
		// Restore the body for forwarding
		c.Request.Body = io.NopCloser(bytes.NewBuffer(bodyBytes))
	}

	slog.DebugContext(c.Request.Context(), "Creating proxy request", "method", c.Request.Method, "target", target, "contentLength", c.Request.ContentLength, "contentType", c.GetHeader("Content-Type"), "bodyLength", len(bodyBytes), "body", string(bodyBytes))

	req, err := http.NewRequestWithContext(c.Request.Context(), c.Request.Method, target, bytes.NewBuffer(bodyBytes))
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
