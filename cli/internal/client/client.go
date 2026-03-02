// Package client provides an HTTP client for communicating with the Arcane API.
//
// The client handles authentication, request construction, and response parsing
// for all API calls. It supports JSON request/response bodies as well as raw
// multipart uploads.
//
// # Creating a Client
//
// The recommended way to create a client is from the CLI configuration:
//
//	c, err := client.NewFromConfig()
//	if err != nil {
//	    return err
//	}
//
// # Making Requests
//
// The client provides convenience methods for common HTTP methods:
//
//	resp, err := c.Get(ctx, "/api/images")
//	resp, err := c.Post(ctx, "/api/images/pull", body)
//	resp, err := c.Delete(ctx, "/api/images/abc123")
package client

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/getarcaneapp/arcane/cli/internal/config"
	"github.com/getarcaneapp/arcane/cli/internal/runstate"
	"github.com/getarcaneapp/arcane/cli/internal/types"
	"github.com/getarcaneapp/arcane/types/auth"
	"github.com/getarcaneapp/arcane/types/base"
)

const (
	headerAPIKey   = "X-API-KEY" //nolint:gosec
	defaultTimeout = 10 * time.Minute
	defaultEnvID   = "0"
	maxErrorBody   = 4096
)

var retryableStatusCodes = map[int]struct{}{
	http.StatusTooManyRequests:    {},
	http.StatusBadGateway:         {},
	http.StatusServiceUnavailable: {},
	http.StatusGatewayTimeout:     {},
}

// Client is an HTTP client for the Arcane API.
// It handles authentication via API tokens and provides methods for making
// HTTP requests to various API endpoints. The client automatically includes
// authentication headers and handles JSON serialization.
type Client struct {
	baseURL       string
	baseURLParsed *url.URL
	apiKey        string
	jwtToken      string
	refreshToken  string
	envID         string
	httpClient    *http.Client
	maxAttempts   int
	baseBackoff   time.Duration
	maxBackoff    time.Duration
}

// New creates a new API client from the provided configuration.
// It validates the configuration and returns an error if required fields
// (ServerURL, APIKey) are missing. The client is initialized with a default
// 30-second timeout and the configured environment ID.
func New(cfg *types.Config) (*Client, error) {
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	parsedURL, err := url.Parse(cfg.ServerURL)
	if err != nil {
		return nil, fmt.Errorf("invalid server_url: %w", err)
	}

	envID := cfg.DefaultEnvironment
	if envID == "" {
		envID = defaultEnvID
	}

	return &Client{
		baseURL:       cfg.ServerURL,
		baseURLParsed: parsedURL,
		apiKey:        cfg.APIKey,
		jwtToken:      cfg.JWTToken,
		refreshToken:  cfg.RefreshToken,
		envID:         envID,
		httpClient:    newHTTPClientInternal(),
		maxAttempts:   3,
		baseBackoff:   150 * time.Millisecond,
		maxBackoff:    2 * time.Second,
	}, nil
}

// NewUnauthenticated creates a client that can call unauthenticated endpoints
// (e.g. /api/auth/login). It only validates that server_url is configured.
func NewUnauthenticated(cfg *types.Config) (*Client, error) {
	if err := cfg.ValidateServerURL(); err != nil {
		return nil, err
	}
	parsedURL, err := url.Parse(cfg.ServerURL)
	if err != nil {
		return nil, fmt.Errorf("invalid server_url: %w", err)
	}

	envID := cfg.DefaultEnvironment
	if envID == "" {
		envID = defaultEnvID
	}

	return &Client{
		baseURL:       cfg.ServerURL,
		baseURLParsed: parsedURL,
		envID:         envID,
		refreshToken:  cfg.RefreshToken,
		httpClient:    newHTTPClientInternal(),
		maxAttempts:   3,
		baseBackoff:   150 * time.Millisecond,
		maxBackoff:    2 * time.Second,
	}, nil
}

func newHTTPClientInternal() *http.Client {
	transport, ok := http.DefaultTransport.(*http.Transport)
	if ok {
		return &http.Client{
			Timeout:   defaultTimeout,
			Transport: transport.Clone(),
		}
	}

	return &http.Client{
		Timeout: defaultTimeout,
	}
}

// NewFromConfig loads the CLI configuration from disk and creates a new client.
// This is the recommended way to create a client in CLI commands.
// It returns an error if the configuration cannot be loaded or is invalid.
func NewFromConfig() (*Client, error) {
	cfg, err := config.Load()
	if err != nil {
		return nil, fmt.Errorf("failed to load config: %w", err)
	}
	state := runstate.Get()
	if strings.TrimSpace(state.EnvOverride) != "" {
		cfg.DefaultEnvironment = strings.TrimSpace(state.EnvOverride)
	}
	c, err := New(cfg)
	if err != nil {
		return nil, err
	}
	if state.RequestTimeout > 0 {
		c.SetTimeout(state.RequestTimeout)
	}
	return c, nil
}

// NewFromConfigUnauthenticated loads config and returns an unauthenticated client.
func NewFromConfigUnauthenticated() (*Client, error) {
	cfg, err := config.Load()
	if err != nil {
		return nil, fmt.Errorf("failed to load config: %w", err)
	}
	state := runstate.Get()
	if strings.TrimSpace(state.EnvOverride) != "" {
		cfg.DefaultEnvironment = strings.TrimSpace(state.EnvOverride)
	}
	c, err := NewUnauthenticated(cfg)
	if err != nil {
		return nil, err
	}
	if state.RequestTimeout > 0 {
		c.SetTimeout(state.RequestTimeout)
	}
	return c, nil
}

// SetEnvironment changes the environment ID for subsequent requests.
// This allows switching between different Arcane environments without
// creating a new client instance.
func (c *Client) SetEnvironment(envID string) {
	c.envID = envID
}

// SetTimeout changes the timeout for subsequent requests.
func (c *Client) SetTimeout(timeout time.Duration) {
	c.httpClient.Timeout = timeout
}

// SetRetryPolicy configures retry behavior for idempotent requests.
func (c *Client) SetRetryPolicy(maxAttempts int, baseBackoff, maxBackoff time.Duration) {
	if maxAttempts < 1 {
		maxAttempts = 1
	}
	c.maxAttempts = maxAttempts
	c.baseBackoff = baseBackoff
	c.maxBackoff = maxBackoff
}

// EnvID returns the current environment ID configured for this client.
// The environment ID is used to scope API requests to a specific environment.
func (c *Client) EnvID() string {
	return c.envID
}

// APIResponse wraps the standard Arcane API response format.
// All API responses include a Success field indicating whether the request
// succeeded, a Data field containing the response payload, and an optional
// Error field with error details on failure.
type APIResponse[T any] struct {
	Success bool   `json:"success"`
	Data    T      `json:"data"`
	Error   string `json:"error,omitempty"`
}

// PaginatedResponse wraps paginated API responses.
// It includes the list of items for the current page along with pagination
// metadata including current page, page size, total items, and total pages.
type PaginatedResponse[T any] struct {
	Items      []T `json:"items"`
	Pagination struct {
		CurrentPage int   `json:"currentPage"`
		PageSize    int   `json:"pageSize"`
		TotalItems  int64 `json:"totalItems"`
		TotalPages  int   `json:"totalPages"`
	} `json:"pagination"`
}

// Request makes an HTTP request to the API with JSON body serialization.
// It constructs the full URL from the base URL and path, serializes the body
// as JSON (if provided), and includes authentication headers. The caller is
// responsible for closing the response body.
func (c *Client) Request(ctx context.Context, method, path string, body any) (*http.Response, error) {
	fullURL, err := c.resolveURL(path)
	if err != nil {
		return nil, err
	}

	var bodyBytes []byte
	if body != nil {
		switch v := body.(type) {
		case []byte:
			bodyBytes = v
		case json.RawMessage:
			bodyBytes = v
		case io.Reader:
			// Streaming bodies cannot be replayed, so skip auto-refresh.
			req, err := http.NewRequestWithContext(ctx, method, fullURL, v)
			if err != nil {
				return nil, fmt.Errorf("failed to create request: %w", err)
			}
			c.applyAuth(req)
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("Accept", "application/json")
			resp, err := c.httpClient.Do(req) //nolint:gosec // intentional request to configured Arcane server URL
			if err != nil {
				return nil, fmt.Errorf("request failed: %w", err)
			}
			return resp, nil
		default:
			jsonBody, err := json.Marshal(body)
			if err != nil {
				return nil, fmt.Errorf("failed to marshal request body: %w", err)
			}
			bodyBytes = jsonBody
		}
	}

	return c.doRequest(ctx, method, fullURL, bodyBytes, true)
}

// RequestRaw makes an HTTP request with a raw body and custom headers.
// Unlike Request, this method does not serialize the body as JSON, making it
// suitable for multipart form uploads and other non-JSON content types.
// Custom headers can be provided to set Content-Type and other headers.
func (c *Client) RequestRaw(ctx context.Context, method, path string, body io.Reader, headers map[string]string) (*http.Response, error) {
	fullURL, err := c.resolveURL(path)
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequestWithContext(ctx, method, fullURL, body)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	c.applyAuth(req)
	req.Header.Set("Accept", "application/json")

	for k, v := range headers {
		req.Header.Set(k, v)
	}

	resp, err := c.httpClient.Do(req) //nolint:gosec // intentional request to configured Arcane server URL
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}

	return resp, nil
}

func (c *Client) resolveURL(path string) (string, error) {
	if strings.TrimSpace(path) == "" {
		return "", fmt.Errorf("invalid path: empty")
	}
	if strings.HasPrefix(path, "http://") || strings.HasPrefix(path, "https://") {
		if _, err := url.Parse(path); err != nil {
			return "", fmt.Errorf("invalid path: %w", err)
		}
		return path, nil
	}
	rel, err := url.Parse(path)
	if err != nil {
		return "", fmt.Errorf("invalid path: %w", err)
	}
	if c.baseURLParsed == nil {
		return "", fmt.Errorf("invalid base URL")
	}
	return c.baseURLParsed.ResolveReference(rel).String(), nil
}

func (c *Client) doRequest(ctx context.Context, method, fullURL string, bodyBytes []byte, allowRefresh bool) (*http.Response, error) {
	attempts := max(c.maxAttempts, 1)

	for attempt := 1; attempt <= attempts; attempt++ {
		var bodyReader io.Reader
		if bodyBytes != nil {
			bodyReader = bytes.NewReader(bodyBytes)
		}

		req, err := http.NewRequestWithContext(ctx, method, fullURL, bodyReader)
		if err != nil {
			return nil, fmt.Errorf("failed to create request: %w", err)
		}

		c.applyAuth(req)
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Accept", "application/json")

		resp, err := c.httpClient.Do(req) //nolint:gosec // intentional request to configured Arcane server URL
		if err != nil {
			if attempt < attempts && c.shouldRetry(method, 0, err) {
				if backoffErr := c.waitRetry(ctx, attempt); backoffErr != nil {
					return nil, backoffErr
				}
				continue
			}
			return nil, fmt.Errorf("request failed: %w", err)
		}

		if resp.StatusCode == http.StatusUnauthorized && allowRefresh && c.jwtToken != "" && c.refreshToken != "" {
			_ = resp.Body.Close()
			if err := c.refreshAccessToken(ctx); err != nil {
				return nil, err
			}
			allowRefresh = false
			continue
		}

		if attempt < attempts && c.shouldRetry(method, resp.StatusCode, nil) {
			_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, maxErrorBody))
			_ = resp.Body.Close()
			if backoffErr := c.waitRetry(ctx, attempt); backoffErr != nil {
				return nil, backoffErr
			}
			continue
		}

		return resp, nil
	}

	return nil, fmt.Errorf("request failed after %d attempts", attempts)
}

func (c *Client) shouldRetry(method string, status int, err error) bool {
	if !isIdempotentMethod(method) {
		return false
	}
	if err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return false
		}
		lowerErr := strings.ToLower(err.Error())
		if strings.Contains(lowerErr, "context canceled") || strings.Contains(lowerErr, "deadline exceeded") {
			return false
		}

		var netErr net.Error
		if errors.As(err, &netErr) {
			return netErr.Timeout()
		}
		return true
	}
	_, ok := retryableStatusCodes[status]
	return ok
}

func (c *Client) waitRetry(ctx context.Context, attempt int) error {
	delay := c.baseBackoff
	if delay <= 0 {
		delay = 150 * time.Millisecond
	}
	for i := 1; i < attempt; i++ {
		delay *= 2
	}
	if c.maxBackoff > 0 && delay > c.maxBackoff {
		delay = c.maxBackoff
	}
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func isIdempotentMethod(method string) bool {
	switch method {
	case http.MethodGet, http.MethodHead, http.MethodOptions, http.MethodPut, http.MethodDelete:
		return true
	default:
		return false
	}
}

func (c *Client) applyAuth(req *http.Request) {
	// Prefer JWT bearer token if present.
	if c.jwtToken != "" {
		req.Header.Set("Authorization", "Bearer "+c.jwtToken)
		return
	}
	if c.apiKey != "" {
		req.Header.Set(headerAPIKey, c.apiKey)
	}
}

func (c *Client) refreshAccessToken(ctx context.Context) error {
	if c.refreshToken == "" {
		return fmt.Errorf("refresh token not configured; run `arcane auth login`")
	}

	refreshReq := auth.Refresh{RefreshToken: c.refreshToken}
	bodyBytes, err := json.Marshal(refreshReq)
	if err != nil {
		return fmt.Errorf("failed to marshal refresh request: %w", err)
	}

	if c.baseURLParsed == nil {
		return fmt.Errorf("invalid base URL")
	}
	refreshURL := c.baseURLParsed.ResolveReference(&url.URL{Path: types.Endpoints.AuthRefresh()}).String()

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, refreshURL, bytes.NewReader(bodyBytes))
	if err != nil {
		return fmt.Errorf("failed to create refresh request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")

	resp, err := c.httpClient.Do(req) //nolint:gosec // intentional request to configured Arcane server URL
	if err != nil {
		return fmt.Errorf("token refresh failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("failed to read refresh response: %w", err)
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("token refresh failed (status %d): %s", resp.StatusCode, strings.TrimSpace(string(respBody)))
	}

	var result base.ApiResponse[auth.TokenRefreshResponse]
	if err := json.Unmarshal(respBody, &result); err != nil {
		return fmt.Errorf("failed to parse refresh response: %w", err)
	}
	if !result.Success || result.Data.Token == "" {
		return fmt.Errorf("token refresh failed: unexpected response from server")
	}

	newRefresh := result.Data.RefreshToken
	if newRefresh == "" {
		newRefresh = c.refreshToken
	}

	c.jwtToken = result.Data.Token
	c.refreshToken = newRefresh

	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}
	cfg.JWTToken = result.Data.Token
	cfg.APIKey = ""
	cfg.RefreshToken = newRefresh
	if err := config.Save(cfg); err != nil {
		return fmt.Errorf("failed to save refreshed token: %w", err)
	}

	return nil
}

// Get makes a GET request to the specified path.
// It is a convenience wrapper around Request for retrieving resources.
func (c *Client) Get(ctx context.Context, path string) (*http.Response, error) {
	return c.Request(ctx, http.MethodGet, path, nil)
}

// Post makes a POST request to the specified path with a JSON body.
// It is a convenience wrapper around Request for creating resources.
func (c *Client) Post(ctx context.Context, path string, body any) (*http.Response, error) {
	return c.Request(ctx, http.MethodPost, path, body)
}

// Put makes a PUT request to the specified path with a JSON body.
// It is a convenience wrapper around Request for updating resources.
func (c *Client) Put(ctx context.Context, path string, body any) (*http.Response, error) {
	return c.Request(ctx, http.MethodPut, path, body)
}

// Delete makes a DELETE request to the specified path.
// It is a convenience wrapper around Request for removing resources.
func (c *Client) Delete(ctx context.Context, path string) (*http.Response, error) {
	return c.Request(ctx, http.MethodDelete, path, nil)
}

// EnvPath returns a path prefixed with the environment.
// It constructs an environment-scoped API path in the format:
// /api/environments/{envID}{path}
func (c *Client) EnvPath(path string) string {
	return fmt.Sprintf("/api/environments/%s%s", c.envID, path)
}

// DoJSON performs a request and decodes a standard API envelope into out.
func (c *Client) DoJSON(ctx context.Context, method, path string, body any, out any) error {
	resp, err := c.Request(ctx, method, path, body)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, maxErrorBody))
		return fmt.Errorf("request failed with status %d: %s", resp.StatusCode, strings.TrimSpace(string(b)))
	}

	if out == nil {
		return nil
	}
	if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
		return fmt.Errorf("failed to decode response: %w", err)
	}
	return nil
}

// DoPaginated performs a request and decodes a paginated API envelope into out.
func (c *Client) DoPaginated(ctx context.Context, method, path string, body any, out any) error {
	return c.DoJSON(ctx, method, path, body, out)
}

// DoRaw performs a request and returns the response payload when status is 2xx.
func (c *Client) DoRaw(ctx context.Context, method, path string, body any) ([]byte, error) {
	resp, err := c.Request(ctx, method, path, body)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, maxErrorBody))
		return nil, fmt.Errorf("request failed with status %d: %s", resp.StatusCode, strings.TrimSpace(string(b)))
	}

	b, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response body: %w", err)
	}
	return b, nil
}

// DecodeResponse decodes an API response into the given type.
// It reads the response body, unmarshals it as JSON, and returns the typed
// result. If the response indicates failure (Success=false) with a 4xx/5xx
// status code, an error is returned with the error message from the API.
// Note: This function closes the response body.
func DecodeResponse[T any](resp *http.Response) (*APIResponse[T], error) {
	return DecodeResponseStrict[T](resp)
}

// DecodeResponseStrict decodes a response envelope and enforces HTTP and API success.
func DecodeResponseStrict[T any](resp *http.Response) (*APIResponse[T], error) {
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, maxErrorBody))
		return nil, fmt.Errorf("request failed with status %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response body: %w", err)
	}

	var result APIResponse[T]
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w (body: %s)", err, string(body))
	}

	if !result.Success {
		if strings.TrimSpace(result.Error) != "" {
			return &result, fmt.Errorf("API error: %s", result.Error)
		}
		return &result, fmt.Errorf("API error: request was not successful")
	}

	return &result, nil
}

// DecodePaginatedResponse decodes a paginated API response.
// It reads the response body and unmarshals it into a PaginatedResponse
// containing the items array and pagination metadata.
// Note: This function closes the response body.
func DecodePaginatedResponse[T any](resp *http.Response) (*PaginatedResponse[T], error) {
	return DecodePaginatedResponseStrict[T](resp)
}

// DecodePaginatedResponseStrict decodes a paginated envelope and enforces HTTP and API success.
func DecodePaginatedResponseStrict[T any](resp *http.Response) (*PaginatedResponse[T], error) {
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, maxErrorBody))
		return nil, fmt.Errorf("request failed with status %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response body: %w", err)
	}

	var result PaginatedResponse[T]
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w (body: %s)", err, string(body))
	}

	return &result, nil
}

// TestConnection tests the API connection by making a request to the version endpoint.
// It returns nil if the connection is successful, or an error describing the failure.
// This is useful for verifying configuration before making other API calls.
func (c *Client) TestConnection(ctx context.Context) error {
	resp, err := c.Get(ctx, "/api/version")
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("connection test failed with status %d: %s", resp.StatusCode, string(body))
	}

	return nil
}
