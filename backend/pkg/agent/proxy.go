package agent

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"maps"
	"net/http"
	"time"

	"kit.getarcane.app"
)

const (
	DefaultTunnelAcquirePollEvery = 100 * time.Millisecond
	// DefaultTunnelAcquireTimeout bounds how long a request waits for a reconnecting agent tunnel.
	DefaultTunnelAcquireTimeout = 4 * time.Second
)

// ProxyRequest sends an HTTP request through an agent tunnel and returns the response status, headers, and body.
func ProxyRequest(ctx context.Context, tunnel *AgentTunnel, method, path, query string, headers http.Header, body []byte) (int, map[string]string, []byte, error) {
	req, err := http.NewRequestWithContext(ctx, method, MuxAgentBaseURL+path+kit.Ternary(query != "", "?"+query, ""), bytes.NewReader(body))
	if err != nil {
		return 0, nil, nil, fmt.Errorf("create agent tunnel request: %w", err)
	}
	maps.Copy(req.Header, headers)
	resp, err := tunnel.HTTPClient.Do(req)
	if err != nil {
		return 0, nil, nil, fmt.Errorf("tunnel request failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return 0, nil, nil, fmt.Errorf("read agent tunnel response: %w", err)
	}
	return resp.StatusCode, flattenResponseHeaders(resp.Header), respBody, nil
}
