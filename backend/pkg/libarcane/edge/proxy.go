package edge

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"time"

	"github.com/labstack/echo/v5"
	"github.com/samber/mo"
)

const (
	DefaultProxyTimeout           = 5 * time.Minute
	DefaultTunnelAcquirePollEvery = 100 * time.Millisecond
	tunnelStreamHeader            = "X-Arcane-Tunnel-Stream"
)

// browserSecurityHeaders are browser-enforced headers that must not cross the edge tunnel.
var browserSecurityHeaders = map[string]struct{}{
	"Origin": {}, "Referer": {}, "Cookie": {}, "Access-Control-Request-Method": {},
	"Access-Control-Request-Headers": {}, "Sec-Fetch-Mode": {}, "Sec-Fetch-Site": {}, "Sec-Fetch-Dest": {},
}

// DefaultTunnelAcquireTimeout returns a poll-aware wait timeout for acquiring
// an on-demand edge tunnel.
func DefaultTunnelAcquireTimeout() time.Duration {
	return DefaultTunnelPollInterval + 2*time.Second
}

// ProxyRequest sends an HTTP request through an edge tunnel
// Returns the response status, headers, and body
func ProxyRequest(ctx context.Context, tunnel *AgentTunnel, method, path, query string, headers map[string]string, body []byte) (int, map[string]string, []byte, error) {
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

func registerPendingRequestInternal(tunnel *AgentTunnel, requestID string) (*PendingRequest, error) {
	if requestID == "" {
		return nil, errors.New("request ID is required")
	}

	pending := &PendingRequest{
		ResponseCh: make(chan *TunnelMessage, 256),
		failureCh:  make(chan error, 1),
	}
	tunnel.Pending.Store(requestID, pending)
	return pending, nil
}

// collectCommandResponseInternal waits for a command response. When out is set
// the response is written to it as it arrives and the returned body is nil.
// credit, when set, returns flow-control credit for each consumed output chunk.
func collectCommandResponseInternal(
	ctx context.Context,
	tunnel *AgentTunnel,
	pending *PendingRequest,
	method string,
	out http.ResponseWriter,
	credit func(consumed int),
) (int, map[string]string, []byte, error) {
	state := &grpcResponseState{out: out, credit: credit}

	for {
		select {
		case <-ctx.Done():
			return 0, nil, nil, ctx.Err()
		case <-tunnel.done:
			if done, status, headers, body, err := state.drainTerminalResponseInternal(ctx, pending, method); done {
				return status, headers, body, err
			}
			return 0, nil, nil, fmt.Errorf("edge tunnel closed while waiting for response: %w", ErrTunnelConnectionClosed)
		case err := <-pending.failureCh:
			if done, status, headers, body, drainTerminalResponseErr := state.drainTerminalResponseInternal(ctx, pending, method); done {
				return status, headers, body, drainTerminalResponseErr
			}
			return 0, nil, nil, err
		case incoming, ok := <-pending.ResponseCh:
			if !ok {
				return 0, nil, nil, fmt.Errorf("edge tunnel response channel closed before a response was received: %w", ErrTunnelConnectionClosed)
			}
			if done, status, headers, body, err := state.handleTunnelMessageInternal(method, incoming); done {
				return status, headers, body, err
			}
		}
	}
}

// handleTunnelMessageInternal applies one response message. A failed write to the output
// ends the response with that error.
func (s *grpcResponseState) handleTunnelMessageInternal(method string, incoming *TunnelMessage) (bool, int, map[string]string, []byte, error) {
	if incoming == nil {
		return false, 0, nil, nil, nil
	}

	var done bool
	var status int
	var headers map[string]string
	var body []byte
	var err error
	switch incoming.Type {
	case MessageTypeResponse:
		done, status, headers, body = s.handleResponse(method, incoming)
	case MessageTypeCommandOutput, MessageTypeStreamData, MessageTypeFileChunk:
		s.handleStreamData(incoming)
		if incoming.Type == MessageTypeCommandOutput && s.credit != nil && len(incoming.Body) > 0 && s.outErr == nil {
			s.credit(len(incoming.Body))
		}
	case MessageTypeCommandComplete:
		done = true
		status, headers, body, err = s.handleCommandComplete(incoming)
	case MessageTypeStreamEnd:
		done, status, headers, body = s.handleStreamEnd()
	}
	if s.outErr != nil {
		return true, s.status, nil, nil, fmt.Errorf("failed to write proxied response: %w", s.outErr)
	}
	return done, status, headers, body, err
}

// drainTerminalResponseInternal applies messages already received for pending, including any still
// queued behind a full channel, until the response completes or nothing more is queued.
func (s *grpcResponseState) drainTerminalResponseInternal(ctx context.Context, pending *PendingRequest, method string) (bool, int, map[string]string, []byte, error) {
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
		if done, status, headers, body, err := s.handleTunnelMessageInternal(method, incoming); done {
			return true, status, headers, body, err
		}
	}
}

func (s *grpcResponseState) handleResponse(method string, incoming *TunnelMessage) (bool, int, map[string]string, []byte) {
	if !s.gotResponse {
		s.gotResponse = true
		s.status = incoming.Status
		s.respHeaders = incoming.Headers
	}

	if s.respHeaders[tunnelStreamHeader] == "1" {
		s.commitInternal()
		s.writeBodyInternal(incoming.Body)
		return false, 0, nil, nil
	}

	if len(incoming.Body) > 0 {
		s.writeBodyInternal(incoming.Body)
		return s.finishInternal()
	}

	if method == http.MethodHead || s.status == http.StatusNoContent || s.status == http.StatusNotModified {
		return s.finishInternal()
	}

	return false, 0, nil, nil
}

func (s *grpcResponseState) handleStreamData(incoming *TunnelMessage) {
	// Agents that support streaming put the status and headers on the first output chunk.
	if !s.gotResponse && incoming.Type == MessageTypeCommandOutput && incoming.Status != 0 {
		s.gotResponse = true
		s.status = incoming.Status
		s.respHeaders = incoming.Headers
		s.commitInternal()
	}
	s.writeBodyInternal(incoming.Body)
}

func (s *grpcResponseState) handleStreamEnd() (bool, int, map[string]string, []byte) {
	if !s.gotResponse {
		return false, 0, nil, nil
	}
	return s.finishInternal()
}

func (s *grpcResponseState) handleCommandComplete(incoming *TunnelMessage) (int, map[string]string, []byte, error) {
	if !s.gotResponse {
		s.gotResponse = true
		s.status = incoming.Status
		s.respHeaders = incoming.Headers
	}
	s.writeBodyInternal(incoming.Body)
	if incoming.Error != "" && incoming.Status >= http.StatusBadRequest {
		return incoming.Status, stripInternalTunnelHeaders(s.respHeaders), s.respBody.Bytes(), errors.New(incoming.Error)
	}
	_, _, headers, body := s.finishInternal()
	return incoming.Status, headers, body, nil
}

// commitInternal writes the status, headers and any buffered body to out.
func (s *grpcResponseState) commitInternal() {
	if s.out == nil || s.committed {
		return
	}
	s.committed = true
	header := s.out.Header()
	for k, v := range s.respHeaders {
		if !isHopByHopHeader(k) && http.CanonicalHeaderKey(k) != tunnelStreamHeader {
			header.Set(k, v)
		}
	}
	s.out.WriteHeader(s.status)
	s.writeBodyInternal(s.respBody.Bytes())
	s.respBody.Reset()
}

// writeBodyInternal buffers body until the response is committed, then writes and flushes it.
// The first output error is kept and stops later writes.
func (s *grpcResponseState) writeBodyInternal(body []byte) {
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

// finishInternal completes the response, committing it to out when set.
func (s *grpcResponseState) finishInternal() (bool, int, map[string]string, []byte) {
	headers := stripInternalTunnelHeaders(s.respHeaders)
	if s.out != nil {
		s.commitInternal()
		return true, s.status, headers, nil
	}
	return true, s.status, headers, s.respBody.Bytes()
}

// ProxyHTTPRequest is a helper that proxies an echo context through a tunnel
func ProxyHTTPRequest(c *echo.Context, tunnel *AgentTunnel, targetPath string) error {
	req := c.Request()
	ctx := req.Context()

	proxyCtx, cancel := context.WithTimeout(ctx, DefaultProxyTimeout)
	defer cancel()

	var body []byte
	if req.Body != nil {
		var err error
		body, err = io.ReadAll(req.Body)
		if err != nil {
			slog.ErrorContext(ctx, "Failed to read request body for tunnel proxy", "error", err)
			return c.JSON(http.StatusInternalServerError, map[string]any{"error": "failed to read request body"})
		}
		req.Body = io.NopCloser(bytes.NewReader(body))
	}

	headers := make(map[string]string)
	for k, v := range req.Header {
		if len(v) > 0 {
			if isHopByHopHeader(k) || isBrowserSecurityHeader(k) {
				continue
			}
			headers[k] = v[0]
		}
	}

	slog.DebugContext(ctx, "Proxying request through edge tunnel",
		"environmentId", tunnel.EnvironmentID,
		"method", req.Method,
		"path", targetPath,
		"bodyLength", len(body),
	)

	_, err := DefaultCommandClient.Execute(proxyCtx, tunnel, &CommandRequest{
		Method:  req.Method,
		Path:    targetPath,
		Query:   req.URL.RawQuery,
		Headers: headers,
		Body:    body,
		Output:  c.Response(),
	})
	if err != nil {
		slog.ErrorContext(ctx, "Edge tunnel proxy failed",
			"environmentId", tunnel.EnvironmentID,
			"error", err,
		)

		// Headers are already sent, so abort the connection rather than end a truncated
		// response as if it were complete. Echo's recover middleware re-panics this sentinel.
		if resp, unwrapErr := echo.UnwrapResponse(c.Response()); unwrapErr == nil && resp.Committed {
			panic(http.ErrAbortHandler)
		}
		if proxyCtx.Err() != nil {
			return c.JSON(http.StatusGatewayTimeout, map[string]any{"error": "request timed out"})
		}

		return c.JSON(http.StatusBadGateway, map[string]any{"error": "failed to proxy request through tunnel"})
	}

	return nil
}

// isHopByHopHeader returns true if the header should not be forwarded
func isHopByHopHeader(header string) bool {
	_, ok := hopByHopHeaders[http.CanonicalHeaderKey(header)]
	return ok
}

// isBrowserSecurityHeader returns true for headers that are browser-enforced
// security headers. These must NOT be forwarded through the edge tunnel because
// the agent's CORS middleware will reject requests whose Origin does not match
// its allowed origins, returning 403. The agent authenticates via
// X-Arcane-Agent-Token instead of browser cookies/origin checks.
func isBrowserSecurityHeader(header string) bool {
	_, ok := browserSecurityHeaders[http.CanonicalHeaderKey(header)]
	return ok
}

func stripInternalTunnelHeaders(headers map[string]string) map[string]string {
	if len(headers) == 0 {
		return headers
	}
	cleaned := make(map[string]string, len(headers))
	for k, v := range headers {
		if http.CanonicalHeaderKey(k) == tunnelStreamHeader {
			continue
		}
		cleaned[k] = v
	}
	return cleaned
}

// HasActiveTunnel checks if an environment has an active edge tunnel
func HasActiveTunnel(envID string) bool {
	return GetActiveTunnel(envID).IsPresent()
}

// GetActiveTunnel returns the active tunnel for an environment, if one exists.
func GetActiveTunnel(envID string) mo.Option[*AgentTunnel] {
	tunnel, ok := GetRegistry().Get(envID).Get()
	if !ok || tunnel == nil || tunnel.Conn == nil || tunnel.Conn.IsClosed() {
		return mo.None[*AgentTunnel]()
	}
	return mo.Some(tunnel)
}

// WaitForActiveTunnel waits for an environment to establish a live tunnel.
func WaitForActiveTunnel(ctx context.Context, envID string, timeout time.Duration) mo.Option[*AgentTunnel] {
	if timeout <= 0 {
		return GetActiveTunnel(envID)
	}

	if tunnel, ok := GetActiveTunnel(envID).Get(); ok {
		return mo.Some(tunnel)
	}

	waitCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	ticker := time.NewTicker(DefaultTunnelAcquirePollEvery)
	defer ticker.Stop()

	for {
		select {
		case <-waitCtx.Done():
			return mo.None[*AgentTunnel]()
		case <-ticker.C:
			if tunnel, ok := GetActiveTunnel(envID).Get(); ok {
				return mo.Some(tunnel)
			}
		}
	}
}

// RequestTunnelAndWait marks an edge environment as needed and waits for the
// agent to establish a live tunnel.
func RequestTunnelAndWait(ctx context.Context, envID string, demandTTL, timeout time.Duration) mo.Option[*AgentTunnel] {
	TouchTunnelDemand(envID, demandTTL)
	return WaitForActiveTunnel(ctx, envID, timeout)
}

// DoRequest performs an HTTP request through an edge tunnel.
// This is for service-level calls that need to route through the tunnel.
// Returns (statusCode, responseBody, error)
func DoRequest(ctx context.Context, envID, method, path string, body []byte) (int, []byte, error) {
	if ctx == nil {
		return 0, nil, errors.New("context is required")
	}
	if _, hasDeadline := ctx.Deadline(); !hasDeadline {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, DefaultProxyTimeout)
		defer cancel()
	}

	tunnel, ok := GetRegistry().Get(envID).Get()
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
