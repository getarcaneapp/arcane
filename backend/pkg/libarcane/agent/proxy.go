package agent

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/samber/mo"
	kit "go.getarcane.app/kit/pkg"
)

const (
	DefaultProxyTimeout           = 5 * time.Minute
	DefaultTunnelAcquirePollEvery = 100 * time.Millisecond
	// DefaultTunnelAcquireTimeout bounds how long a request waits for a reconnecting agent tunnel.
	DefaultTunnelAcquireTimeout = 4 * time.Second
)

// ProxyRequest sends an HTTP request through an agent tunnel and returns the response status, headers, and body.
func ProxyRequest(ctx context.Context, tunnel *AgentTunnel, method, path, query string, headers map[string]string, body []byte) (int, map[string]string, []byte, error) {
	req, err := http.NewRequestWithContext(ctx, method, MuxAgentBaseURL+path+kit.Ternary(query != "", "?"+query, ""), bytes.NewReader(body))
	if err != nil {
		return 0, nil, nil, fmt.Errorf("create agent tunnel request: %w", err)
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
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

// HasActiveTunnel checks if an environment has an active agent tunnel
func HasActiveTunnel(envID string) bool {
	return GetActiveTunnel(envID).IsPresent()
}

// GetActiveTunnel returns the active tunnel for an environment in the default registry, if one exists.
func GetActiveTunnel(envID string) mo.Option[*AgentTunnel] {
	return GetRegistry().Active(envID)
}

// WaitForActiveTunnel waits for an environment to establish a live tunnel in the default registry.
func WaitForActiveTunnel(ctx context.Context, envID string, timeout time.Duration) mo.Option[*AgentTunnel] {
	return GetRegistry().WaitForActive(ctx, envID, timeout)
}

// DoRequest performs a service-level HTTP request through an agent tunnel and returns the status and body.
func DoRequest(ctx context.Context, envID, method, path string, body []byte) (int, []byte, error) {
	if ctx == nil {
		return 0, nil, errors.New("context is required")
	}
	if _, hasDeadline := ctx.Deadline(); !hasDeadline {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, DefaultProxyTimeout)
		defer cancel()
	}

	tunnel, ok := GetActiveTunnel(envID).Get()
	if !ok {
		return 0, nil, fmt.Errorf("no active tunnel for environment %s", envID)
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
