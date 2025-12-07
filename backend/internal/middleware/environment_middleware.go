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
	environmentsPathMarker = "/environments/"
	proxyTimeout           = 60 * time.Second
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

	// Resolve remote environment
	apiURL, accessToken, enabled, err := m.resolver(c.Request.Context(), envID)
	if err != nil || apiURL == "" {
		c.JSON(http.StatusNotFound, gin.H{
			"success": false,
			"data":    gin.H{"error": "Environment not found"},
		})
		c.Abort()
		return
	}

	if !enabled {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"data":    gin.H{"error": "Environment is disabled"},
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
	prefix := "/api/environments/" + envID
	suffix := strings.TrimPrefix(c.Request.URL.Path, prefix)
	if suffix != "" && !strings.HasPrefix(suffix, "/") {
		suffix = "/" + suffix
	}

	// Build target: apiURL + /api/environments/{localID} + suffix
	target := strings.TrimRight(apiURL, "/") + path.Join("/api/environments/", m.localID) + suffix

	// Append query string if present
	if qs := c.Request.URL.RawQuery; qs != "" {
		target += "?" + qs
	}

	return target
}

// isWebSocketUpgrade checks if this is a WebSocket upgrade request.
func (m *EnvironmentMiddleware) isWebSocketUpgrade(c *gin.Context) bool {
	return strings.EqualFold(c.GetHeader("Upgrade"), "websocket") ||
		strings.Contains(strings.ToLower(c.GetHeader("Connection")), "upgrade")
}

// proxyWebSocket handles WebSocket proxy requests.
func (m *EnvironmentMiddleware) proxyWebSocket(c *gin.Context, target string, accessToken *string, envID string) {
	wsTarget := httpToWebSocketURL(target)
	headers := m.buildWebSocketHeaders(c, accessToken)

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
			"data":    gin.H{"error": "Failed to create proxy request"},
		})
		c.Abort()
		return
	}

	resp, err := m.httpClient.Do(req)
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{
			"success": false,
			"data":    gin.H{"error": fmt.Sprintf("Proxy request failed: %v", err)},
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

	slog.DebugContext(c.Request.Context(), "Creating proxy request",
		"method", c.Request.Method,
		"target", target,
		"contentLength", c.Request.ContentLength,
		"contentType", c.GetHeader("Content-Type"),
		"bodyLength", len(bodyBytes),
		"body", string(bodyBytes))

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
		_, _ = io.Copy(c.Writer, resp.Body)
	}
}

// buildWebSocketHeaders creates headers for WebSocket proxy requests.
func (m *EnvironmentMiddleware) buildWebSocketHeaders(c *gin.Context, accessToken *string) http.Header {
	headers := http.Header{}

	// Forward API key if present
	if apiKey := c.GetHeader("X-Api-Key"); apiKey != "" {
		headers.Set("X-Api-Key", apiKey)
	}

	// Forward authorization (header or cookie)
	if auth := c.GetHeader("Authorization"); auth != "" {
		headers.Set("Authorization", auth)
	} else if token, err := c.Cookie("token"); err == nil && token != "" {
		headers.Set("Authorization", "Bearer "+token)
	}

	// Forward cookies if no other auth is present
	if headers.Get("Authorization") == "" && headers.Get("X-Api-Token") == "" {
		if cookies := c.Request.Header.Get("Cookie"); cookies != "" {
			headers.Set("Cookie", cookies)
		}
	}

	// Set agent token for remote environment authentication
	if accessToken != nil && *accessToken != "" {
		headers.Set("X-Arcane-Agent-Token", *accessToken)
	}

	return headers
}

// httpToWebSocketURL converts an HTTP(S) URL to WS(S).
func httpToWebSocketURL(url string) string {
	switch {
	case strings.HasPrefix(url, "https://"):
		return "wss://" + strings.TrimPrefix(url, "https://")
	case strings.HasPrefix(url, "http://"):
		return "ws://" + strings.TrimPrefix(url, "http://")
	default:
		return url
	}
}
