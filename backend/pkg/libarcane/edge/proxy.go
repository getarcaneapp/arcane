package edge

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"maps"
	"net/http"
	"time"

	"github.com/labstack/echo/v5"
	"github.com/samber/mo"
	kit "go.getarcane.app/kit/pkg"
)

const (
	DefaultProxyTimeout           = 5 * time.Minute
	DefaultTunnelAcquirePollEvery = 100 * time.Millisecond
	// Deprecated: legacy edge message protocol; remove in v3.
	tunnelStreamHeader        = "X-Arcane-Tunnel-Stream"
	maxLegacyCommandBodyBytes = 256 << 20
)

// browserSecurityHeaders must not cross a legacy tunnel: the agent's CORS check would reject the browser Origin,
// and the agent authenticates the manager by token instead.
//
// Deprecated: legacy edge message protocol; remove in v3.
var browserSecurityHeaders = map[string]struct{}{
	"Origin": {}, "Referer": {}, "Cookie": {}, "Access-Control-Request-Method": {},
	"Access-Control-Request-Headers": {}, "Sec-Fetch-Mode": {}, "Sec-Fetch-Site": {}, "Sec-Fetch-Dest": {},
}

// DefaultTunnelAcquireTimeout returns a poll-aware wait timeout for acquiring an on-demand edge tunnel.
func DefaultTunnelAcquireTimeout() time.Duration {
	return DefaultTunnelPollInterval + 2*time.Second
}

// ProxyRequest sends an HTTP request through an edge tunnel and returns the response status, headers, and body.
func ProxyRequest(ctx context.Context, tunnel *AgentTunnel, method, path, query string, headers map[string]string, body []byte) (int, map[string]string, []byte, error) {
	if tunnel != nil {
		TouchTunnelDemand(tunnel.EnvironmentID, DefaultTunnelDemandTTL)
	}
	if client, ok := tunnel.HTTPClient().Get(); ok {
		req, err := http.NewRequestWithContext(ctx, method, MuxAgentBaseURL+path+kit.Ternary(query != "", "?"+query, ""), bytes.NewReader(body))
		if err != nil {
			return 0, nil, nil, fmt.Errorf("create edge tunnel request: %w", err)
		}
		for k, v := range headers {
			req.Header.Set(k, v)
		}
		resp, err := client.Do(req)
		if err != nil {
			return 0, nil, nil, fmt.Errorf("tunnel request failed: %w", err)
		}
		defer func() { _ = resp.Body.Close() }()
		respBody, err := io.ReadAll(resp.Body)
		if err != nil {
			return 0, nil, nil, fmt.Errorf("read edge tunnel response: %w", err)
		}
		return resp.StatusCode, flattenResponseHeaders(resp.Header), respBody, nil
	}

	result, err := DefaultCommandClient.Execute(ctx, tunnel, &CommandRequest{
		Method:  method,
		Path:    path,
		Query:   query,
		Headers: headers,
		Body:    body,
	})
	if err != nil {
		return 0, nil, nil, err
	}

	return result.Status, result.Headers, result.Body, nil
}

// collectCommandResponse waits for a command response, streaming it to out when set (the returned body is then nil).
// credit, when set, returns flow-control credit for each consumed output chunk.
func collectCommandResponse(
	ctx context.Context, tunnel *AgentTunnel, pending *PendingRequest, method string, out http.ResponseWriter, credit func(consumed int),
) (int, map[string]string, []byte, error) {
	state := &grpcResponseState{out: out, credit: credit}

	for {
		select {
		case <-ctx.Done():
			return 0, nil, nil, ctx.Err()
		case <-tunnel.done:
			if done, status, headers, body, err := state.drainTerminalResponse(ctx, pending, method); done {
				return status, headers, body, err
			}
			return 0, nil, nil, fmt.Errorf("edge tunnel closed while waiting for response: %w", ErrTunnelConnectionClosed)
		case err := <-pending.failureCh:
			if done, status, headers, body, drainTerminalResponseErr := state.drainTerminalResponse(ctx, pending, method); done {
				return status, headers, body, drainTerminalResponseErr
			}
			return 0, nil, nil, err
		case incoming, ok := <-pending.ResponseCh:
			if !ok {
				return 0, nil, nil, fmt.Errorf("edge tunnel response channel closed before a response was received: %w", ErrTunnelConnectionClosed)
			}
			if done, status, headers, body, err := state.handleTunnelMessage(method, incoming); done {
				return status, headers, body, err
			}
		}
	}
}

// handleTunnelMessage applies one response message. A failed write to the output
// ends the response with that error.
func (s *grpcResponseState) handleTunnelMessage(method string, incoming *TunnelMessage) (bool, int, map[string]string, []byte, error) {
	if incoming == nil {
		return false, 0, nil, nil, nil
	}

	var done bool
	var status int
	var headers map[string]string
	var body []byte
	var err error
	// The first response message carries the status and headers.
	if !s.gotResponse && (incoming.Type == MessageTypeResponse || incoming.Type == MessageTypeCommandComplete ||
		(incoming.Type == MessageTypeCommandOutput && incoming.Status != 0)) {
		s.gotResponse = true
		s.status = incoming.Status
		s.respHeaders = incoming.Headers
		if incoming.Type == MessageTypeCommandOutput {
			s.commit()
		}
	}
	switch incoming.Type {
	case MessageTypeResponse:
		switch {
		case s.respHeaders[tunnelStreamHeader] == "1":
			s.commit()
			s.writeBody(incoming.Body)
		case len(incoming.Body) > 0:
			s.writeBody(incoming.Body)
			done = true
			status, headers, body = s.finish()
		case method == http.MethodHead || s.status == http.StatusNoContent || s.status == http.StatusNotModified:
			done = true
			status, headers, body = s.finish()
		}
	case MessageTypeCommandOutput, MessageTypeStreamData, MessageTypeFileChunk:
		s.writeBody(incoming.Body)
		if incoming.Type == MessageTypeCommandOutput && s.credit != nil && len(incoming.Body) > 0 && s.outErr == nil {
			s.credit(len(incoming.Body))
		}
	case MessageTypeCommandComplete:
		s.writeBody(incoming.Body)
		if incoming.Error != "" && incoming.Status >= http.StatusBadRequest {
			done, status, headers, body = true, incoming.Status, s.cleanHeaders(), s.respBody.Bytes()
			err = &commandStatusError{status: incoming.Status, message: incoming.Error}
		} else {
			done = true
			_, headers, body = s.finish()
			status = incoming.Status
		}
	case MessageTypeStreamEnd:
		if s.gotResponse {
			done = true
			status, headers, body = s.finish()
		}
	}
	if s.outErr != nil {
		return true, s.status, nil, nil, fmt.Errorf("failed to write proxied response: %w", s.outErr)
	}
	return done, status, headers, body, err
}

// drainTerminalResponse applies messages already received for pending, including any still
// queued behind a full channel, until the response completes or nothing more is queued.
func (s *grpcResponseState) drainTerminalResponse(ctx context.Context, pending *PendingRequest, method string) (bool, int, map[string]string, []byte, error) {
	for {
		if err := ctx.Err(); err != nil {
			return true, 0, nil, nil, err
		}
		// Checking idle before reading ensures every queued delivery is already in the channel.
		pending.mu.Lock()
		idle := pending.failed || (!pending.draining && len(pending.backlog) == 0)
		pending.mu.Unlock()

		var incoming *TunnelMessage
		ok := true
		select {
		case incoming, ok = <-pending.ResponseCh:
		default:
			if idle {
				return false, 0, nil, nil, nil
			}
			select {
			case incoming, ok = <-pending.ResponseCh:
			case <-ctx.Done():
				return true, 0, nil, nil, ctx.Err()
			case <-time.After(pendingDeliveryTimeout):
				return false, 0, nil, nil, nil
			}
		}
		if !ok {
			return true, 0, nil, nil, fmt.Errorf("edge tunnel response channel closed before a response was received: %w", ErrTunnelConnectionClosed)
		}
		if done, status, headers, body, err := s.handleTunnelMessage(method, incoming); done {
			return true, status, headers, body, err
		}
	}
}

// commit writes the status, headers and any buffered body to out.
func (s *grpcResponseState) commit() {
	if s.out == nil || s.committed {
		return
	}
	s.committed = true
	header := s.out.Header()
	for k, v := range s.cleanHeaders() {
		if _, hop := hopByHopHeaders[http.CanonicalHeaderKey(k)]; !hop {
			header.Set(k, v)
		}
	}
	s.out.WriteHeader(s.status)
	s.writeBody(s.respBody.Bytes())
	s.respBody.Reset()
}

// writeBody buffers body until the response is committed, then writes and flushes it.
// The first output error is kept and stops later writes.
func (s *grpcResponseState) writeBody(body []byte) {
	if !s.committed {
		s.respBody.Write(body)
		return
	}
	if s.outErr != nil {
		return
	}
	if len(body) > 0 {
		if _, s.outErr = s.out.Write(body); s.outErr != nil {
			return
		}
	}
	if flushErr := http.NewResponseController(s.out).Flush(); flushErr != nil && !errors.Is(flushErr, http.ErrNotSupported) {
		s.outErr = flushErr
	}
}

// finish completes the response, committing it to out when set.
func (s *grpcResponseState) finish() (int, map[string]string, []byte) {
	headers := s.cleanHeaders()
	if s.out != nil {
		s.commit()
		return s.status, headers, nil
	}
	return s.status, headers, s.respBody.Bytes()
}

// ProxyHTTPRequest proxies an echo request through a legacy tunnel.
//
// Deprecated: legacy edge message protocol; remove in v3.
func ProxyHTTPRequest(c *echo.Context, tunnel *AgentTunnel, targetPath string) error {
	req := c.Request()
	ctx := req.Context()

	proxyCtx, cancel := context.WithTimeout(ctx, DefaultProxyTimeout)
	defer cancel()

	var body []byte
	if req.Body != nil {
		var err error
		body, err = io.ReadAll(http.MaxBytesReader(c.Response(), req.Body, maxLegacyCommandBodyBytes))
		if _, tooLarge := errors.AsType[*http.MaxBytesError](err); tooLarge {
			return c.JSON(http.StatusRequestEntityTooLarge, map[string]any{"error": "request body is too large for a legacy edge tunnel; upgrade the agent"})
		}
		if err != nil {
			slog.ErrorContext(ctx, "Failed to read request body for tunnel proxy", "error", err)
			return c.JSON(http.StatusInternalServerError, map[string]any{"error": "failed to read request body"})
		}
		req.Body = io.NopCloser(bytes.NewReader(body))
	}

	headers := make(map[string]string, len(req.Header))
	for k, v := range req.Header {
		_, hop := hopByHopHeaders[http.CanonicalHeaderKey(k)]
		_, browser := browserSecurityHeaders[http.CanonicalHeaderKey(k)]
		if len(v) > 0 && !hop && !browser {
			headers[k] = v[0]
		}
	}
	slog.DebugContext(ctx, "Proxying request through edge tunnel", "environmentId", tunnel.EnvironmentID, "method", req.Method, "path", targetPath, "bodyLength", len(body))

	_, err := DefaultCommandClient.Execute(proxyCtx, tunnel, &CommandRequest{
		Method:  req.Method,
		Path:    targetPath,
		Query:   req.URL.RawQuery,
		Headers: headers,
		Body:    body,
		Output:  c.Response(),
	})
	if err != nil {
		slog.ErrorContext(ctx, "Edge tunnel proxy failed", "environmentId", tunnel.EnvironmentID, "error", err)
		// Headers are already sent, so abort the connection rather than end a truncated
		// response as if it were complete. Echo's recover middleware re-panics this sentinel.
		if resp, unwrapErr := echo.UnwrapResponse(c.Response()); unwrapErr == nil && resp.Committed {
			panic(http.ErrAbortHandler)
		}
		if statusErr, ok := errors.AsType[*commandStatusError](err); ok {
			return c.JSON(statusErr.status, map[string]any{"error": statusErr.message})
		}
		if proxyCtx.Err() != nil {
			return c.JSON(http.StatusGatewayTimeout, map[string]any{"error": "request timed out"})
		}

		return c.JSON(http.StatusBadGateway, map[string]any{"error": "failed to proxy request through tunnel"})
	}

	return nil
}

func (e *commandStatusError) Error() string {
	return e.message
}

// cleanHeaders returns the response headers without the internal stream marker.
func (s *grpcResponseState) cleanHeaders() map[string]string {
	headers := maps.Clone(s.respHeaders)
	maps.DeleteFunc(headers, func(k, _ string) bool { return http.CanonicalHeaderKey(k) == tunnelStreamHeader })
	return headers
}

// HasActiveTunnel checks if an environment has an active edge tunnel
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

// RequestTunnelAndWait marks an edge environment as needed and waits for the agent to establish a live tunnel.
func RequestTunnelAndWait(ctx context.Context, envID string, demandTTL, timeout time.Duration) mo.Option[*AgentTunnel] {
	TouchTunnelDemand(envID, demandTTL)
	return WaitForActiveTunnel(ctx, envID, timeout)
}

// DoRequest performs a service-level HTTP request through an edge tunnel and returns the status and body.
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
