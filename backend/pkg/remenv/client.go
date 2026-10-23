package remenv

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"net"
	"net/http"
	"strings"
)

const (
	HeaderAPIKey        = "X-Api-Key"            // #nosec G101: header name, not a credential
	HeaderAgentToken    = "X-Arcane-Agent-Token" // #nosec G101: header name, not a credential
	HeaderAuthorization = "Authorization"
	bearerScheme        = "Bearer "
)

var ErrEnvironmentUnavailable = errors.New("edge agent is not connected")

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

type Request struct {
	EnvironmentID string
	Method        string
	Path          string
	Headers       map[string]string
	Body          []byte
}

type Response struct {
	StatusCode int
	Body       []byte
	Headers    map[string]string
}

type TunnelTransport interface {
	EnsureAvailable(ctx context.Context, envID string) error
	Do(ctx context.Context, envID, method, path string, headers map[string]string, body []byte) (*Response, error)
}

type TunnelTransportFuncs struct {
	EnsureAvailableFunc func(ctx context.Context, envID string) error
	DoFunc              func(ctx context.Context, envID, method, path string, headers map[string]string, body []byte) (*Response, error)
}

func (t TunnelTransportFuncs) EnsureAvailable(ctx context.Context, envID string) error {
	if t.EnsureAvailableFunc == nil {
		return errors.New("edge transport unavailable")
	}
	return t.EnsureAvailableFunc(ctx, envID)
}

func (t TunnelTransportFuncs) Do(ctx context.Context, envID, method, path string, headers map[string]string, body []byte) (*Response, error) {
	if t.DoFunc == nil {
		return nil, errors.New("edge transport unavailable")
	}
	return t.DoFunc(ctx, envID, method, path, headers, body)
}

type Client struct {
	tunnel TunnelTransport
}

func NewClient(tunnel TunnelTransport) *Client {
	return &Client{tunnel: tunnel}
}

func (c *Client) Do(ctx context.Context, req Request) (*Response, error) {
	if err := c.tunnel.EnsureAvailable(ctx, req.EnvironmentID); err != nil {
		return nil, &TransportError{Err: err}
	}
	headers := maps.Clone(req.Headers)
	if headers == nil {
		headers = map[string]string{}
	}
	resp, err := c.tunnel.Do(ctx, req.EnvironmentID, req.Method, req.Path, headers, req.Body)
	if err != nil {
		return nil, &TransportError{Err: err}
	}
	return resp, nil
}

func (r *Response) RequireSuccess() error {
	if r == nil {
		return &StatusError{}
	}
	if r.StatusCode >= http.StatusOK && r.StatusCode < http.StatusMultipleChoices {
		return nil
	}
	return &StatusError{StatusCode: r.StatusCode, Body: r.Body}
}

type TransportError struct {
	Err error
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

type StatusError struct {
	StatusCode int
	Body       []byte
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

type DecodeError struct {
	Err error
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

// ApplyAgentTokenHeaders sets the agent token in both X- header forms. It never sets
// Authorization, which carries the proxied user's own token.
func ApplyAgentTokenHeaders(headers http.Header, accessToken *string) {
	if headers == nil || accessToken == nil || strings.TrimSpace(*accessToken) == "" {
		return
	}

	headers.Set(HeaderAgentToken, *accessToken)
	headers.Set(HeaderAPIKey, *accessToken)
}

// ApplyAgentTokenHeaderMap mirrors ApplyAgentTokenHeaders for map-based header carriers.
func ApplyAgentTokenHeaderMap(headers map[string]string, accessToken *string) {
	if headers == nil || accessToken == nil || strings.TrimSpace(*accessToken) == "" {
		return
	}

	headers[HeaderAgentToken] = *accessToken
	headers[HeaderAPIKey] = *accessToken
}
