package edge

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

const (
	// DefaultProxyTimeout is the default timeout for proxied requests
	DefaultProxyTimeout = 5 * time.Minute
)

// ProxyRequest sends an HTTP request through an edge tunnel
// Returns the response status, headers, and body
func ProxyRequest(ctx context.Context, tunnel *AgentTunnel, method, path, query string, headers map[string]string, body []byte) (int, map[string]string, []byte, error) {
	requestID := uuid.New().String()

	msg := &TunnelMessage{
		ID:      requestID,
		Type:    MessageTypeRequest,
		Method:  method,
		Path:    path,
		Query:   query,
		Headers: headers,
		Body:    body,
	}

	// Keep request/response flow compatibility for WebSocket transport.
	if _, isGRPC := tunnel.Conn.(*GRPCManagerTunnelConn); !isGRPC {
		return proxyRequestLegacy(ctx, tunnel, msg)
	}

	return proxyRequestGRPC(ctx, tunnel, method, requestID, msg)
}

func proxyRequestLegacy(ctx context.Context, tunnel *AgentTunnel, msg *TunnelMessage) (int, map[string]string, []byte, error) {
	resp, err := tunnel.Conn.SendRequest(ctx, msg, &tunnel.Pending)
	if err != nil {
		return 0, nil, nil, fmt.Errorf("tunnel request failed: %w", err)
	}
	return resp.Status, resp.Headers, resp.Body, nil
}

func proxyRequestGRPC(ctx context.Context, tunnel *AgentTunnel, method, requestID string, msg *TunnelMessage) (int, map[string]string, []byte, error) {
	respCh := make(chan *TunnelMessage, 256)
	tunnel.Pending.Store(requestID, &PendingRequest{
		ResponseCh: respCh,
		CreatedAt:  time.Now(),
	})
	defer tunnel.Pending.Delete(requestID)

	if err := tunnel.Conn.Send(msg); err != nil {
		return 0, nil, nil, fmt.Errorf("tunnel request failed: %w", err)
	}

	return collectGRPCResponse(ctx, method, respCh)
}

func collectGRPCResponse(ctx context.Context, method string, respCh <-chan *TunnelMessage) (int, map[string]string, []byte, error) {
	state := &grpcResponseState{}

	for {
		select {
		case <-ctx.Done():
			return 0, nil, nil, ctx.Err()
		case incoming := <-respCh:
			if incoming == nil {
				continue
			}

			switch incoming.Type {
			case MessageTypeResponse:
				if done, status, headers, body := state.handleResponse(method, incoming); done {
					return status, headers, body, nil
				}
			case MessageTypeStreamData:
				state.handleStreamData(incoming)
			case MessageTypeStreamEnd:
				if done, status, headers, body := state.handleStreamEnd(); done {
					return status, headers, body, nil
				}
			case MessageTypeRequest,
				MessageTypeHeartbeat,
				MessageTypeHeartbeatAck,
				MessageTypeWebSocketStart,
				MessageTypeWebSocketData,
				MessageTypeWebSocketClose,
				MessageTypeRegister,
				MessageTypeRegisterResponse,
				MessageTypeEvent:
				continue
			}
		}
	}
}

type grpcResponseState struct {
	status      int
	respHeaders map[string]string
	respBody    bytes.Buffer
	gotResponse bool
}

func (s *grpcResponseState) handleResponse(method string, incoming *TunnelMessage) (bool, int, map[string]string, []byte) {
	if !s.gotResponse {
		s.gotResponse = true
		s.status = incoming.Status
		s.respHeaders = incoming.Headers
	}

	if len(incoming.Body) > 0 {
		s.respBody.Write(incoming.Body)
		return true, s.status, stripInternalTunnelHeaders(s.respHeaders), s.respBody.Bytes()
	}

	if method == http.MethodHead || s.status == http.StatusNoContent || s.status == http.StatusNotModified {
		return true, s.status, stripInternalTunnelHeaders(s.respHeaders), nil
	}

	return false, 0, nil, nil
}

func (s *grpcResponseState) handleStreamData(incoming *TunnelMessage) {
	if !s.gotResponse {
		// Ignore out-of-order stream chunks until the response envelope arrives.
		return
	}
	if len(incoming.Body) > 0 {
		s.respBody.Write(incoming.Body)
	}
}

func (s *grpcResponseState) handleStreamEnd() (bool, int, map[string]string, []byte) {
	if !s.gotResponse {
		return false, 0, nil, nil
	}
	return true, s.status, stripInternalTunnelHeaders(s.respHeaders), s.respBody.Bytes()
}

// ProxyHTTPRequest is a helper that proxies a gin context through a tunnel
func ProxyHTTPRequest(c *gin.Context, tunnel *AgentTunnel, targetPath string) {
	ctx := c.Request.Context()

	// Set a reasonable timeout
	proxyCtx, cancel := context.WithTimeout(ctx, DefaultProxyTimeout)
	defer cancel()

	// Read request body
	var body []byte
	if c.Request.Body != nil {
		var err error
		body, err = io.ReadAll(c.Request.Body)
		if err != nil {
			slog.ErrorContext(ctx, "Failed to read request body for tunnel proxy", "error", err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to read request body"})
			return
		}
		// Restore body for potential retry
		c.Request.Body = io.NopCloser(bytes.NewReader(body))
	}

	// Build headers map
	headers := make(map[string]string)
	for k, v := range c.Request.Header {
		if len(v) > 0 {
			// Skip hop-by-hop headers
			if isHopByHopHeader(k) {
				continue
			}
			headers[k] = v[0]
		}
	}

	slog.DebugContext(ctx, "Proxying request through edge tunnel",
		"environment_id", tunnel.EnvironmentID,
		"method", c.Request.Method,
		"path", targetPath,
	)

	status, respHeaders, respBody, err := ProxyRequest(proxyCtx, tunnel, c.Request.Method, targetPath, c.Request.URL.RawQuery, headers, body)
	if err != nil {
		slog.ErrorContext(ctx, "Edge tunnel proxy failed",
			"environment_id", tunnel.EnvironmentID,
			"error", err,
		)

		// Check if it's a context timeout/cancel
		if proxyCtx.Err() != nil {
			c.JSON(http.StatusGatewayTimeout, gin.H{"error": "request timed out"})
			return
		}

		c.JSON(http.StatusBadGateway, gin.H{"error": "failed to proxy request through tunnel"})
		return
	}

	// Copy response headers
	for k, v := range respHeaders {
		if !isHopByHopHeader(k) {
			c.Header(k, v)
		}
	}

	// Write response
	c.Data(status, respHeaders["Content-Type"], respBody)
}

// isHopByHopHeader returns true if the header should not be forwarded
func isHopByHopHeader(header string) bool {
	hopByHop := map[string]bool{
		"Connection":          true,
		"Keep-Alive":          true,
		"Proxy-Authenticate":  true,
		"Proxy-Authorization": true,
		"Te":                  true,
		"Trailers":            true,
		"Transfer-Encoding":   true,
		"Upgrade":             true,
	}
	return hopByHop[http.CanonicalHeaderKey(header)]
}

func stripInternalTunnelHeaders(headers map[string]string) map[string]string {
	if len(headers) == 0 {
		return headers
	}
	cleaned := make(map[string]string, len(headers))
	for k, v := range headers {
		if http.CanonicalHeaderKey(k) == "X-Arcane-Tunnel-Stream" {
			continue
		}
		cleaned[k] = v
	}
	return cleaned
}

// HasActiveTunnel checks if an environment has an active edge tunnel
func HasActiveTunnel(envID string) bool {
	tunnel, ok := GetRegistry().Get(envID)
	if !ok {
		return false
	}
	return !tunnel.Conn.IsClosed()
}

// DoRequest performs an HTTP request through an edge tunnel.
// This is for service-level calls that need to route through the tunnel.
// Returns (statusCode, responseBody, error)
func DoRequest(ctx context.Context, envID, method, path string, body []byte) (int, []byte, error) {
	tunnel, ok := GetRegistry().Get(envID)
	if !ok {
		return 0, nil, fmt.Errorf("no active tunnel for environment %s", envID)
	}
	if tunnel.Conn.IsClosed() {
		return 0, nil, fmt.Errorf("tunnel for environment %s is closed", envID)
	}

	headers := make(map[string]string)
	if method != http.MethodGet && len(body) > 0 {
		headers["Content-Type"] = "application/json"
	}

	status, _, respBody, err := ProxyRequest(ctx, tunnel, method, path, "", headers, body)
	if err != nil {
		return 0, nil, err
	}

	return status, respBody, nil
}
