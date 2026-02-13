package edge

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"time"
)

// EdgeAwareClient is an HTTP client that automatically routes requests
// to edge environments through the active edge tunnel instead of direct HTTP.
// Works with both gRPC and WebSocket tunnel transports.
type EdgeAwareClient struct {
	httpClient *http.Client
}

// NewEdgeAwareClient creates a new edge-aware HTTP client
func NewEdgeAwareClient(timeout time.Duration) *EdgeAwareClient {
	return &EdgeAwareClient{
		httpClient: &http.Client{Timeout: timeout},
	}
}

// EdgeResponse wraps the response from either direct HTTP or tunnel request
type EdgeResponse struct {
	StatusCode int
	Body       []byte
	Headers    map[string]string
}

// DoForEnvironment makes an HTTP request, automatically routing through the edge
// tunnel if the environment is an edge environment with an active tunnel.
// Parameters:
//   - ctx: request context
//   - envID: environment ID (used to find tunnel for edge envs)
//   - isEdge: whether this is an edge environment
//   - method: HTTP method (GET, POST, etc.)
//   - url: full URL for direct requests (ignored for edge, only path is used)
//   - path: API path (e.g., "/api/health") - used for both edge and non-edge
//   - headers: HTTP headers to include
//   - body: request body (can be nil)
//
// Returns EdgeResponse with status code, body bytes, and headers
func (c *EdgeAwareClient) DoForEnvironment(
	ctx context.Context,
	envID string,
	isEdge bool,
	method string,
	url string,
	path string,
	headers map[string]string,
	body []byte,
) (*EdgeResponse, error) {
	// For edge environments with active tunnels, route through tunnel
	if isEdge && HasActiveTunnel(envID) {
		return c.doViaTunnel(ctx, envID, method, path, headers, body)
	}

	// For edge environments without active tunnel, return error
	if isEdge {
		return nil, fmt.Errorf("edge agent is not connected (no active tunnel)")
	}

	// For non-edge environments, do direct HTTP request
	return c.doDirectHTTP(ctx, method, url, headers, body)
}

// doViaTunnel routes the request through the edge tunnel
func (c *EdgeAwareClient) doViaTunnel(
	ctx context.Context,
	envID string,
	method string,
	path string,
	headers map[string]string,
	body []byte,
) (*EdgeResponse, error) {
	tunnel, ok := GetRegistry().Get(envID)
	if !ok {
		return nil, fmt.Errorf("no active tunnel for environment %s", envID)
	}
	if tunnel.Conn.IsClosed() {
		return nil, fmt.Errorf("tunnel for environment %s is closed", envID)
	}

	// Use the existing ProxyRequest function
	statusCode, respHeaders, respBody, err := ProxyRequest(ctx, tunnel, method, path, "", headers, body)
	if err != nil {
		return nil, fmt.Errorf("tunnel request failed: %w", err)
	}

	return &EdgeResponse{
		StatusCode: statusCode,
		Body:       respBody,
		Headers:    respHeaders,
	}, nil
}

// doDirectHTTP makes a direct HTTP request
func (c *EdgeAwareClient) doDirectHTTP(
	ctx context.Context,
	method string,
	url string,
	headers map[string]string,
	body []byte,
) (*EdgeResponse, error) {
	var bodyReader io.Reader
	if body != nil {
		bodyReader = bytes.NewReader(body)
	}

	req, err := http.NewRequestWithContext(ctx, method, url, bodyReader)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	// Set headers
	for k, v := range headers {
		req.Header.Set(k, v)
	}

	resp, err := c.httpClient.Do(req) //nolint:gosec // intentional request to configured environment URL in non-edge mode
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response body: %w", err)
	}

	// Convert response headers
	respHeaders := make(map[string]string)
	for k, v := range resp.Header {
		if len(v) > 0 {
			respHeaders[k] = v[0]
		}
	}

	return &EdgeResponse{
		StatusCode: resp.StatusCode,
		Body:       respBody,
		Headers:    respHeaders,
	}, nil
}

// DefaultEdgeAwareClient is a singleton client with reasonable defaults
var DefaultEdgeAwareClient = NewEdgeAwareClient(30 * time.Second)

// DoRequest is a convenience function for making edge-aware requests
// using the default client
func DoEdgeAwareRequest(
	ctx context.Context,
	envID string,
	isEdge bool,
	method string,
	url string,
	path string,
	headers map[string]string,
	body []byte,
) (*EdgeResponse, error) {
	return DefaultEdgeAwareClient.DoForEnvironment(ctx, envID, isEdge, method, url, path, headers, body)
}
