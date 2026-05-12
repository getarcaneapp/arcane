package remenv

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

const (
	HeaderAPIKey        = "X-API-Key"            // #nosec G101: header name, not a credential
	HeaderAgentToken    = "X-Arcane-Agent-Token" // #nosec G101: header name, not a credential
	HeaderAuthorization = "Authorization"
	bearerScheme        = "Bearer "
)

// ExtractBearerToken returns the token portion of an "Authorization: Bearer <token>"
// header value. It is case-insensitive on the scheme and trims surrounding whitespace.
// Returns an empty string if the header is empty or does not use the Bearer scheme.
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

// RedactedTokenFingerprint returns a short, redacted identifier for a token
// suitable for debug logs. It is intentionally not reversible and never
// reveals the full secret.
func RedactedTokenFingerprint(token string) string {
	token = strings.TrimSpace(token)
	if len(token) <= 10 {
		return "***"
	}
	return token[:6] + "..." + token[len(token)-4:]
}

type Request struct {
	EnvironmentID string
	IsEdge        bool
	Method        string
	URL           string
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
		return fmt.Errorf("edge transport unavailable")
	}
	return t.EnsureAvailableFunc(ctx, envID)
}

func (t TunnelTransportFuncs) Do(ctx context.Context, envID, method, path string, headers map[string]string, body []byte) (*Response, error) {
	if t.DoFunc == nil {
		return nil, fmt.Errorf("edge transport unavailable")
	}
	return t.DoFunc(ctx, envID, method, path, headers, body)
}

type Client struct {
	httpClient *http.Client
	tunnel     TunnelTransport
}

func NewClient(httpClient *http.Client, tunnel TunnelTransport) *Client {
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 30 * time.Second}
	}

	return &Client{
		httpClient: httpClient,
		tunnel:     tunnel,
	}
}

func (c *Client) Do(ctx context.Context, req Request) (*Response, error) {
	if req.IsEdge {
		return c.doViaTunnelInternal(ctx, req)
	}

	return c.doDirectHTTPInternal(ctx, req)
}

func (c *Client) DoJSON(ctx context.Context, req Request, out any) error {
	resp, err := c.Do(ctx, req)
	if err != nil {
		return err
	}

	if err := resp.RequireSuccess(); err != nil {
		return err
	}

	return resp.DecodeJSON(out)
}

func (c *Client) doViaTunnelInternal(ctx context.Context, req Request) (*Response, error) {
	if c.tunnel == nil {
		return nil, &TransportError{Err: fmt.Errorf("edge transport unavailable")}
	}

	if err := c.tunnel.EnsureAvailable(ctx, req.EnvironmentID); err != nil {
		return nil, &TransportError{Err: err}
	}

	resp, err := c.tunnel.Do(ctx, req.EnvironmentID, req.Method, req.Path, cloneHeaders(req.Headers), req.Body)
	if err != nil {
		return nil, &TransportError{Err: err}
	}

	return resp, nil
}

func (c *Client) doDirectHTTPInternal(ctx context.Context, req Request) (*Response, error) {
	var bodyReader io.Reader
	if len(req.Body) > 0 {
		bodyReader = bytes.NewReader(req.Body)
	}

	httpReq, err := http.NewRequestWithContext(ctx, req.Method, req.URL, bodyReader)
	if err != nil {
		return nil, &TransportError{Err: fmt.Errorf("failed to create request: %w", err)}
	}

	for key, value := range req.Headers {
		httpReq.Header.Set(key, value)
	}

	resp, err := c.httpClient.Do(httpReq) //nolint:gosec // intentional request to configured remote environment URL
	if err != nil {
		return nil, &TransportError{Err: err}
	}
	defer func() { _ = resp.Body.Close() }()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, &TransportError{Err: fmt.Errorf("failed to read response body: %w", err)}
	}

	return &Response{
		StatusCode: resp.StatusCode,
		Body:       respBody,
		Headers:    flattenHeaders(resp.Header),
	}, nil
}

func (r *Response) RequireSuccess() error {
	if r != nil && r.StatusCode >= http.StatusOK && r.StatusCode < http.StatusMultipleChoices {
		return nil
	}

	if r == nil {
		return &StatusError{}
	}

	return &StatusError{
		StatusCode: r.StatusCode,
		Body:       r.Body,
	}
}

func (r *Response) DecodeJSON(out any) error {
	if out == nil {
		return nil
	}
	if r == nil {
		return &DecodeError{Err: fmt.Errorf("response is nil")}
	}
	if err := json.Unmarshal(r.Body, out); err != nil {
		return &DecodeError{Err: err}
	}
	return nil
}

type TransportError struct {
	Err error
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

// ApplyAgentTokenHeaders sets the agent token in both X- header forms on the
// given http.Header. It deliberately does NOT set the Authorization header:
// these helpers are used by the manager when proxying user requests through
// to remote environments, and the Authorization header in that case carries
// the calling user's bearer or session token. Direct agent->manager call
// sites (poll, websocket dial, grpc dial, mTLS enroll) set
// `Authorization: Bearer <token>` themselves so the token survives reverse
// proxies that strip non-standard X- headers.
func ApplyAgentTokenHeaders(headers http.Header, accessToken *string) {
	if headers == nil || accessToken == nil || strings.TrimSpace(*accessToken) == "" {
		return
	}

	headers.Set(HeaderAgentToken, *accessToken)
	headers.Set(HeaderAPIKey, *accessToken)
}

// ApplyAgentTokenHeaderMap mirrors ApplyAgentTokenHeaders for map-based
// header carriers. Same rationale: do not write Authorization here.
func ApplyAgentTokenHeaderMap(headers map[string]string, accessToken *string) {
	if headers == nil || accessToken == nil || strings.TrimSpace(*accessToken) == "" {
		return
	}

	headers[HeaderAgentToken] = *accessToken
	headers[HeaderAPIKey] = *accessToken
}

func flattenHeaders(headers http.Header) map[string]string {
	if len(headers) == 0 {
		return map[string]string{}
	}

	out := make(map[string]string, len(headers))
	for key, values := range headers {
		if len(values) > 0 {
			out[key] = values[0]
		}
	}
	return out
}

func cloneHeaders(headers map[string]string) map[string]string {
	if len(headers) == 0 {
		return map[string]string{}
	}

	out := make(map[string]string, len(headers))
	for key, value := range headers {
		out[key] = value
	}
	return out
}
