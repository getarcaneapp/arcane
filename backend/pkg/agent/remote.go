package agent

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"strings"
	"time"

	"getarcane.dev/arcane/v3/pkg/timeouts"
)

type (
	RemoteRequest struct {
		EnvironmentID string
		Method        string
		Path          string
		Headers       http.Header
		Body          []byte
	}

	RemoteResponse struct {
		Headers    map[string]string
		Body       []byte
		StatusCode int
	}

	TunnelTransport interface {
		EnsureAvailable(ctx context.Context, envID string) error
		Do(ctx context.Context, envID, method, path string, headers http.Header, body []byte) (*RemoteResponse, error)
	}

	RemoteClientConfig struct {
		Tunnel         TunnelTransport
		DefaultTimeout time.Duration
	}

	RemoteClient struct {
		tunnel         TunnelTransport
		defaultTimeout time.Duration
	}

	registryTunnel struct{}

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

var ErrEnvironmentUnavailable = errors.New("agent is not connected")

func NewRemoteClient(cfg RemoteClientConfig) *RemoteClient {
	return &RemoteClient{
		tunnel:         cmp.Or[TunnelTransport](cfg.Tunnel, registryTunnel{}),
		defaultTimeout: cmp.Or(cfg.DefaultTimeout, timeouts.DefaultAgentProxy),
	}
}

func (c *RemoteClient) Do(ctx context.Context, req RemoteRequest) (*RemoteResponse, error) {
	ctx, cancel := timeouts.WithDefault(ctx, c.defaultTimeout)
	defer cancel()

	if err := c.tunnel.EnsureAvailable(ctx, req.EnvironmentID); err != nil {
		return nil, &TransportError{Err: err}
	}
	resp, err := c.tunnel.Do(ctx, req.EnvironmentID, req.Method, req.Path, req.Headers.Clone(), req.Body)
	if err != nil {
		return nil, &TransportError{Err: err}
	}
	return resp, nil
}

func (registryTunnel) EnsureAvailable(ctx context.Context, envID string) error {
	if GetRegistry().Active(envID).IsPresent() {
		return nil
	}
	if _, ok := GetRegistry().WaitForActive(ctx, envID, DefaultTunnelAcquireTimeout).Get(); ok {
		return nil
	}
	return ErrEnvironmentUnavailable
}

func (registryTunnel) Do(ctx context.Context, envID, method, path string, headers http.Header, body []byte) (*RemoteResponse, error) {
	tunnel, ok := GetRegistry().Get(envID).Get()
	if !ok {
		return nil, fmt.Errorf("no active tunnel for environment %s: %w", envID, ErrEnvironmentUnavailable)
	}
	if tunnel.Conn.IsClosed() {
		return nil, fmt.Errorf("tunnel for environment %s is closed: %w", envID, ErrEnvironmentUnavailable)
	}

	statusCode, respHeaders, respBody, err := ProxyRequest(ctx, tunnel, method, path, "", headers, body)
	if err != nil {
		if errors.Is(err, ErrTunnelConnectionClosed) {
			err = errors.Join(ErrEnvironmentUnavailable, err)
		}
		return nil, fmt.Errorf("tunnel request failed: %w", err)
	}

	return &RemoteResponse{
		StatusCode: statusCode,
		Body:       respBody,
		Headers:    respHeaders,
	}, nil
}

// ExtractBearerToken returns the trimmed token from an "Authorization: Bearer <token>" value,
// or "" when the scheme (case-insensitive) is not Bearer.
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

// RedactedTokenFingerprint returns a short, non-reversible token identifier for debug logs.
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

// Unavailable reports connection failures that permit using locally stored data.
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
