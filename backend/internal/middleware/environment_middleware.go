package middleware

import (
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

// EnvResolver should return the environment api url, optional access token, whether the env is enabled, and an error.
type EnvResolver func(ctx context.Context, id string) (apiURL string, accessToken *string, enabled bool, err error)

// NewEnvProxyMiddlewareWithParam returns a gin middleware that proxies requests whose environment id
// is remote. paramName is the URL param key (e.g. "id") that contains the environment id when using
// router groups; if that param is not present the middleware will attempt to auto-detect the id
// by parsing the request path after the first "/environments/" segment.
func NewEnvProxyMiddlewareWithParam(localID string, paramName string, resolver EnvResolver, envService *services.EnvironmentService) gin.HandlerFunc {
	m := &EnvironmentMiddleware{
		localID:    localID,
		resolver:   resolver,
		envService: envService,
		httpClient: &http.Client{
			Timeout: 60 * time.Second,
		},
	}
	return m.handle(paramName)
}

type EnvironmentMiddleware struct {
	localID    string
	resolver   EnvResolver
	envService *services.EnvironmentService
	httpClient *http.Client
}

func (m *EnvironmentMiddleware) handle(paramName string) gin.HandlerFunc {
	return func(c *gin.Context) {
		envID := m.extractEnvironmentID(c, paramName)

		if envID == "" || envID == m.localID {
			c.Next()
			return
		}

		apiURL, accessToken, enabled, err := m.resolver(c.Request.Context(), envID)
		if err != nil || apiURL == "" {
			c.JSON(http.StatusNotFound, gin.H{"success": false, "data": gin.H{"error": "Environment not found"}})
			c.Abort()
			return
		}
		if !enabled {
			c.JSON(http.StatusBadRequest, gin.H{"success": false, "data": gin.H{"error": "Environment is disabled"}})
			c.Abort()
			return
		}

		target := m.buildTargetURL(c, envID, apiURL)

		if m.isWebSocketRequest(c) {
			m.handleWebSocket(c, target, accessToken, envID)
			return
		}

		m.proxyHTTP(c, target, accessToken)
	}
}

func (m *EnvironmentMiddleware) extractEnvironmentID(c *gin.Context, paramName string) string {
	envID := c.Param(paramName)
	if envID != "" {
		return envID
	}

	const marker = "/environments/"
	if idx := strings.Index(c.Request.URL.Path, marker); idx >= 0 {
		rest := c.Request.URL.Path[idx+len(marker):]
		parts := strings.SplitN(rest, "/", 2)
		if len(parts) > 0 && parts[0] != "" {
			return parts[0]
		}
	}

	return ""
}

func (m *EnvironmentMiddleware) buildTargetURL(c *gin.Context, envID, apiURL string) string {
	prefix := "/api/environments/" + envID
	suffix := strings.TrimPrefix(c.Request.URL.Path, prefix)
	if !strings.HasPrefix(suffix, "/") && suffix != "" {
		suffix = "/" + suffix
	}

	target := strings.TrimRight(apiURL, "/") + path.Join("/api/environments/", m.localID) + suffix
	if qs := c.Request.URL.RawQuery; qs != "" {
		target += "?" + qs
	}

	return target
}

func (m *EnvironmentMiddleware) isWebSocketRequest(c *gin.Context) bool {
	return strings.EqualFold(c.GetHeader("Upgrade"), "websocket") ||
		strings.Contains(strings.ToLower(c.GetHeader("Connection")), "upgrade")
}

func (m *EnvironmentMiddleware) handleWebSocket(c *gin.Context, target string, accessToken *string, envID string) {
	wsTarget := m.convertToWebSocketURL(target)
	hdr := m.buildWebSocketHeaders(c, accessToken)

	if err := wsutil.ProxyHTTP(c.Writer, c.Request, wsTarget, hdr); err != nil {
		slog.Error("websocket proxy failed", "env_id", envID, "target", wsTarget, "err", err)
	}
	c.Abort()
}

func (m *EnvironmentMiddleware) convertToWebSocketURL(target string) string {
	if strings.HasPrefix(target, "https://") {
		return "wss://" + strings.TrimPrefix(target, "https://")
	}
	if strings.HasPrefix(target, "http://") {
		return "ws://" + strings.TrimPrefix(target, "http://")
	}
	return target
}

func (m *EnvironmentMiddleware) buildWebSocketHeaders(c *gin.Context, accessToken *string) http.Header {
	hdr := http.Header{}

	// Forward API key header if present
	if apiKey := c.GetHeader("X-Api-Token"); apiKey != "" {
		hdr.Set("X-Api-Token", apiKey)
	}

	if auth := c.GetHeader("Authorization"); auth != "" {
		hdr.Set("Authorization", auth)
	} else if cookieToken, err := c.Cookie("token"); err == nil && cookieToken != "" {
		hdr.Set("Authorization", "Bearer "+cookieToken)
	}

	if hdr.Get("Authorization") == "" && hdr.Get("X-Api-Token") == "" {
		if cookieHeader := c.Request.Header.Get("Cookie"); cookieHeader != "" {
			hdr.Set("Cookie", cookieHeader)
		}
	}

	if accessToken != nil && *accessToken != "" {
		hdr.Set("X-Arcane-Agent-Token", *accessToken)
	}

	return hdr
}

func (m *EnvironmentMiddleware) proxyHTTP(c *gin.Context, target string, accessToken *string) {
	req, err := m.createProxyRequest(c, target, accessToken)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "data": gin.H{"error": "Failed to create proxy request"}})
		c.Abort()
		return
	}

	resp, err := m.httpClient.Do(req)
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"success": false, "data": gin.H{"error": fmt.Sprintf("Proxy request failed: %v", err)}})
		c.Abort()
		return
	}
	defer resp.Body.Close()

	m.copyResponseToClient(c, resp)
	c.Abort()
}

func (m *EnvironmentMiddleware) createProxyRequest(c *gin.Context, target string, accessToken *string) (*http.Request, error) {
	req, err := http.NewRequestWithContext(c.Request.Context(), c.Request.Method, target, c.Request.Body)
	if err != nil {
		return nil, err
	}

	skip := remenv.GetSkipHeaders()
	remenv.CopyRequestHeaders(c.Request.Header, req.Header, skip)
	remenv.SetAuthHeader(req, c)
	remenv.SetAgentToken(req, accessToken)
	remenv.SetForwardedHeaders(req, c.ClientIP(), c.Request.Host)

	return req, nil
}

func (m *EnvironmentMiddleware) copyResponseToClient(c *gin.Context, resp *http.Response) {
	hop := remenv.BuildHopByHopHeaders(resp.Header)
	remenv.CopyResponseHeaders(resp.Header, c.Writer.Header(), hop)

	c.Status(resp.StatusCode)
	if c.Request.Method != http.MethodHead {
		_, _ = io.Copy(c.Writer, resp.Body)
	}
}
