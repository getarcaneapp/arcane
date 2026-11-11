package agent

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"maps"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/hashicorp/yamux"

	"kit.getarcane.app"

	"getarcane.dev/arcane/v3/pkg/httpx"
	"getarcane.dev/arcane/v3/pkg/timeouts"
)

type (
	RemoteRequest struct {
		Headers       http.Header
		EnvironmentID string
		Method        string
		Path          string
		Body          []byte
	}

	RemoteResponse struct {
		Headers    map[string]string
		Body       []byte
		StatusCode int
	}

	TransportError struct {
		Err error
	}

	StatusError struct {
		Body       []byte
		StatusCode int
	}

	DecodeError struct {
		Err error
	}
)

const (
	DefaultTunnelAcquireTimeout   = 4 * time.Second
	DefaultTunnelReconnectTimeout = 10 * time.Second
)

var ErrEnvironmentUnavailable = errors.New("agent is not connected")

func (t *AgentTunnel) RetryAfterLoss(req *http.Request, err error) bool {
	return (req.Method == http.MethodGet || req.Method == http.MethodHead) && req.ContentLength == 0 && req.Context().Err() == nil &&
		(errors.Is(err, ErrTunnelConnectionClosed) || errors.Is(err, yamux.ErrSessionShutdown) || t.Conn.IsClosed())
}

func (r *TunnelRegistry) Do(ctx context.Context, req RemoteRequest) (*RemoteResponse, error) {
	ctx, cancel := timeouts.WithDefault(ctx, timeouts.DefaultAgentProxy)
	defer cancel()

	var lost *AgentTunnel
	for {
		tunnel, ok := r.WaitForActive(ctx, req.EnvironmentID, WaitForActiveOptions{
			Except:  lost,
			Timeout: kit.Ternary(lost != nil, DefaultTunnelReconnectTimeout, DefaultTunnelAcquireTimeout),
		}).Get()
		if !ok {
			return nil, &TransportError{Err: ErrEnvironmentUnavailable}
		}
		httpReq, err := http.NewRequestWithContext(ctx, req.Method, MuxAgentBaseURL+req.Path, bytes.NewReader(req.Body))
		if err != nil {
			return nil, &TransportError{Err: fmt.Errorf("create agent tunnel request: %w", err)}
		}
		maps.Copy(httpReq.Header, req.Headers.Clone())
		failure := "tunnel request failed"
		resp, err := tunnel.HTTPClient.Do(httpReq)
		var body []byte
		if err == nil {
			failure = "read agent tunnel response"
			body, err = io.ReadAll(resp.Body)
			_ = resp.Body.Close()
		}
		if err == nil {
			return &RemoteResponse{
				Headers:    flattenResponseHeaders(resp.Header),
				Body:       body,
				StatusCode: resp.StatusCode,
			}, nil
		}
		if lost == nil && !httpx.IsEnvironmentFanOutRequest(ctx) && tunnel.RetryAfterLoss(httpReq, err) {
			lost = tunnel
			continue
		}
		return nil, &TransportError{Err: fmt.Errorf("%s: %w", failure,
			kit.Ternary(errors.Is(err, ErrTunnelConnectionClosed), errors.Join(ErrEnvironmentUnavailable, err), err))}
	}
}

func ExtractBearerToken(headerValue string) string {
	headerValue = strings.TrimSpace(headerValue)
	if headerValue == "" {
		return ""
	}
	if len(headerValue) < len(bearerScheme) || !strings.EqualFold(headerValue[:len(bearerScheme)], bearerScheme) {
		return ""
	}
	return strings.TrimSpace(headerValue[len(bearerScheme):])
}

func RedactedTokenFingerprint(token string) string {
	token = strings.TrimSpace(token)
	if len(token) <= 10 {
		return "***"
	}
	return token[:6] + "..." + token[len(token)-4:]
}

func (r *RemoteResponse) RequireSuccess() error {
	if r == nil {
		return &StatusError{}
	}
	if r.StatusCode >= http.StatusOK && r.StatusCode < http.StatusMultipleChoices {
		return nil
	}
	return &StatusError{StatusCode: r.StatusCode, Body: r.Body}
}

// Invalid requests and failures reading an established response are not outages.
func (e *TransportError) Unavailable() bool {
	if e == nil || errors.Is(e.Err, context.Canceled) {
		return false
	}
	if errors.Is(e.Err, context.DeadlineExceeded) || errors.Is(e.Err, ErrEnvironmentUnavailable) {
		return true
	}
	networkErr, ok := errors.AsType[net.Error](e.Err)
	return ok && networkErr.Timeout()
}

func (e *TransportError) Error() string {
	if e == nil || e.Err == nil {
		return "transport error"
	}
	return e.Err.Error()
}

func (e *TransportError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Err
}

func (e *StatusError) Error() string {
	if e == nil {
		return "unexpected status code"
	}
	if body := strings.TrimSpace(string(e.Body)); body != "" {
		return fmt.Sprintf("unexpected status code %d: %s", e.StatusCode, body)
	}
	return fmt.Sprintf("unexpected status code %d", e.StatusCode)
}

func (e *DecodeError) Error() string {
	if e == nil || e.Err == nil {
		return "failed to decode response body"
	}
	return fmt.Sprintf("failed to decode response body: %v", e.Err)
}

func (e *DecodeError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Err
}
