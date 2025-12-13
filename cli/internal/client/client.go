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
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"

	"go.getarcane.app/cli/internal/config"
	"go.getarcane.app/cli/internal/types"
)

const (
	headerAPIKey   = "X-API-KEY" //nolint:gosec
	defaultTimeout = 30 * time.Second
	defaultEnvID   = "0"
)

// Client is an HTTP client for the Arcane API.
// It handles authentication via API tokens and provides methods for making
// HTTP requests to various API endpoints. The client automatically includes
// authentication headers and handles JSON serialization.
type Client struct {
	baseURL    string
	apiKey     string
	jwtToken   string
	envID      string
	httpClient *http.Client
}

// New creates a new API client from the provided configuration.
// It validates the configuration and returns an error if required fields
// (ServerURL, APIKey) are missing. The client is initialized with a default
// 30-second timeout and the configured environment ID.
func New(cfg *types.Config) (*Client, error) {
	if err := cfg.Validate(); err != nil {
		return nil, err
	}

	envID := cfg.DefaultEnvironment
	if envID == "" {
		envID = defaultEnvID
	}

	return &Client{
		baseURL:  cfg.ServerURL,
		apiKey:   cfg.APIKey,
		jwtToken: cfg.JWTToken,
		envID:    envID,
		httpClient: &http.Client{
			Timeout: defaultTimeout,
		},
	}, nil
}

// NewUnauthenticated creates a client that can call unauthenticated endpoints
// (e.g. /api/auth/login). It only validates that server_url is configured.
func NewUnauthenticated(cfg *types.Config) (*Client, error) {
	if err := cfg.ValidateServerURL(); err != nil {
		return nil, err
	}

	envID := cfg.DefaultEnvironment
	if envID == "" {
		envID = defaultEnvID
	}

	return &Client{
		baseURL: cfg.ServerURL,
		envID:   envID,
		httpClient: &http.Client{
			Timeout: defaultTimeout,
		},
	}, nil
}

// NewFromConfig loads the CLI configuration from disk and creates a new client.
// This is the recommended way to create a client in CLI commands.
// It returns an error if the configuration cannot be loaded or is invalid.
func NewFromConfig() (*Client, error) {
	cfg, err := config.Load()
	if err != nil {
		return nil, fmt.Errorf("failed to load config: %w", err)
	}
	return New(cfg)
}

// NewFromConfigUnauthenticated loads config and returns an unauthenticated client.
func NewFromConfigUnauthenticated() (*Client, error) {
	cfg, err := config.Load()
	if err != nil {
		return nil, fmt.Errorf("failed to load config: %w", err)
	}
	return NewUnauthenticated(cfg)
}

// SetEnvironment changes the environment ID for subsequent requests.
// This allows switching between different Arcane environments without
// creating a new client instance.
func (c *Client) SetEnvironment(envID string) {
	c.envID = envID
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
	u, err := url.Parse(c.baseURL)
	if err != nil {
		return nil, fmt.Errorf("invalid base URL: %w", err)
	}

	rel, err := url.Parse(path)
	if err != nil {
		return nil, fmt.Errorf("invalid path: %w", err)
	}

	fullURL := u.ResolveReference(rel).String()

	var bodyReader io.Reader
	if body != nil {
		switch v := body.(type) {
		case []byte:
			// Callers commonly pre-marshal JSON and pass the raw bytes.
			// Treat as already-encoded JSON to avoid double-marshalling.
			bodyReader = bytes.NewReader(v)
		case json.RawMessage:
			bodyReader = bytes.NewReader(v)
		case io.Reader:
			// Allow streaming bodies when needed.
			bodyReader = v
		default:
			jsonBody, err := json.Marshal(body)
			if err != nil {
				return nil, fmt.Errorf("failed to marshal request body: %w", err)
			}
			bodyReader = bytes.NewReader(jsonBody)
		}
	}

	req, err := http.NewRequestWithContext(ctx, method, fullURL, bodyReader)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	c.applyAuth(req)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}

	return resp, nil
}

// RequestRaw makes an HTTP request with a raw body and custom headers.
// Unlike Request, this method does not serialize the body as JSON, making it
// suitable for multipart form uploads and other non-JSON content types.
// Custom headers can be provided to set Content-Type and other headers.
func (c *Client) RequestRaw(ctx context.Context, method, path string, body io.Reader, headers map[string]string) (*http.Response, error) {
	u, err := url.Parse(c.baseURL)
	if err != nil {
		return nil, fmt.Errorf("invalid base URL: %w", err)
	}

	rel, err := url.Parse(path)
	if err != nil {
		return nil, fmt.Errorf("invalid path: %w", err)
	}

	fullURL := u.ResolveReference(rel).String()

	req, err := http.NewRequestWithContext(ctx, method, fullURL, body)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	c.applyAuth(req)
	req.Header.Set("Accept", "application/json")

	for k, v := range headers {
		req.Header.Set(k, v)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}

	return resp, nil
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

// DecodeResponse decodes an API response into the given type.
// It reads the response body, unmarshals it as JSON, and returns the typed
// result. If the response indicates failure (Success=false) with a 4xx/5xx
// status code, an error is returned with the error message from the API.
// Note: This function closes the response body.
func DecodeResponse[T any](resp *http.Response) (*APIResponse[T], error) {
	defer func() { _ = resp.Body.Close() }()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response body: %w", err)
	}

	var result APIResponse[T]
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w (body: %s)", err, string(body))
	}

	if !result.Success && resp.StatusCode >= 400 {
		return &result, fmt.Errorf("API error: %s", result.Error)
	}

	return &result, nil
}

// DecodePaginatedResponse decodes a paginated API response.
// It reads the response body and unmarshals it into a PaginatedResponse
// containing the items array and pagination metadata.
// Note: This function closes the response body.
func DecodePaginatedResponse[T any](resp *http.Response) (*PaginatedResponse[T], error) {
	defer func() { _ = resp.Body.Close() }()

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
