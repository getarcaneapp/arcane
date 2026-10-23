// Package client provides authenticated HTTP transport for the Arcane API.
package client

import (
	"bytes"
	"cmp"
	"context"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"errors"
	"fmt"
	"io"
	"maps"
	"net"
	"net/http"
	"net/url"
	"os"
	"slices"
	"strings"
	"time"

	"github.com/getarcaneapp/arcane/types/v3"

	"github.com/getarcaneapp/arcane/cli/v3/internal/config"
	"github.com/getarcaneapp/arcane/cli/v3/internal/logger"
	"github.com/getarcaneapp/arcane/cli/v3/internal/output"
	"github.com/getarcaneapp/arcane/cli/v3/internal/runstate"
	clitypes "github.com/getarcaneapp/arcane/cli/v3/internal/types"
)

const (
	headerAPIKey   = "X-Api-Key" // #nosec G101 -- HTTP header name, not a credential
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

type StatusError struct {
	StatusCode int
	Body       string
}

func (e *StatusError) Error() string {
	return fmt.Sprintf("request failed with status %d: %s", e.StatusCode, e.Body)
}

// Client provides authenticated requests, token refresh, and response decoding for the Arcane API.
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

// New validates configuration and creates an authenticated client with the configured environment and a ten-minute timeout.
func New(cfg *clitypes.Config) (*Client, error) {
	cfg = cfg.Clone()
	if token := strings.TrimSpace(os.Getenv("ARCANE_TOKEN")); token != "" {
		cfg.JWTToken = token
		cfg.APIKey = ""
		cfg.RefreshToken = ""
	}
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	c, err := NewUnauthenticated(cfg)
	if err != nil {
		return nil, err
	}
	c.apiKey = cfg.APIKey
	c.jwtToken = cfg.JWTToken
	c.refreshToken = cfg.RefreshToken
	return c, nil
}

// NewUnauthenticated creates a client that can call unauthenticated endpoints
// (e.g. /api/auth/login). It only validates that server_url is configured.
func NewUnauthenticated(cfg *clitypes.Config) (*Client, error) {
	cfg = cfg.Clone()
	if strings.TrimSpace(os.Getenv("ARCANE_TOKEN")) != "" {
		cfg.RefreshToken = ""
	}
	if err := cfg.ValidateServerURL(); err != nil {
		return nil, err
	}
	parsedURL, err := url.Parse(cfg.ServerURL)
	if err != nil {
		return nil, fmt.Errorf("invalid server_url: %w", err)
	}

	httpClient := &http.Client{Timeout: defaultTimeout}
	if transport, ok := http.DefaultTransport.(*http.Transport); ok {
		httpClient.Transport = transport.Clone()
	}

	return &Client{
		baseURL:       cfg.ServerURL,
		baseURLParsed: parsedURL,
		envID:         defaultEnvID,
		refreshToken:  cfg.RefreshToken,
		httpClient:    httpClient,
		maxAttempts:   3,
		baseBackoff:   150 * time.Millisecond,
		maxBackoff:    2 * time.Second,
	}, nil
}

// NewFromConfig loads persisted configuration and applies invocation overrides before creating a client.
func NewFromConfig() (*Client, error) {
	cfg, err := config.Load()
	if err != nil {
		return nil, fmt.Errorf("failed to load config: %w", err)
	}
	c, err := New(cfg)
	if err != nil {
		return nil, err
	}
	state := runstate.Get()
	c.SetEnvironment(cmp.Or(strings.TrimSpace(state.EnvOverride), defaultEnvID))
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
	c, err := NewUnauthenticated(cfg)
	if err != nil {
		return nil, err
	}
	state := runstate.Get()
	c.SetEnvironment(cmp.Or(strings.TrimSpace(state.EnvOverride), defaultEnvID))
	if state.RequestTimeout > 0 {
		c.SetTimeout(state.RequestTimeout)
	}
	return c, nil
}

// SetEnvironment changes the environment ID for subsequent requests on this client.
func (c *Client) SetEnvironment(envID string) {
	c.envID = envID
}

// SetTimeout changes the timeout for subsequent requests.
func (c *Client) SetTimeout(timeout time.Duration) {
	c.httpClient.Timeout = timeout
}

// SetRetryPolicy configures retry behavior for idempotent requests.
func (c *Client) SetRetryPolicy(maxAttempts int, baseBackoff, maxBackoff time.Duration) {
	c.maxAttempts = max(maxAttempts, 1)
	c.baseBackoff = baseBackoff
	c.maxBackoff = maxBackoff
}

// EnvID returns the current environment ID configured for this client.
// The environment ID is used to scope API requests to a specific environment.
func (c *Client) EnvID() string {
	return c.envID
}

// Request sends JSON or raw reader bodies with authentication; streaming bodies skip retries and token refresh.
func (c *Client) Request(ctx context.Context, method, path string, body any, headers ...http.Header) (*http.Response, error) {
	loading := output.StartLoading(ctx)
	defer loading.Stop()
	fullURL, err := c.resolveURL(path)
	if err != nil {
		return nil, err
	}

	var bodyBytes []byte
	if body != nil {
		switch v := body.(type) {
		case []byte:
			bodyBytes = v
		case jsontext.Value:
			bodyBytes = v
		case io.Reader:
			// Streaming bodies cannot be replayed, so skip auto-refresh.
			req, newRequestWithContextErr := http.NewRequestWithContext(ctx, method, fullURL, v)
			if newRequestWithContextErr != nil {
				return nil, fmt.Errorf("failed to create request: %w", newRequestWithContextErr)
			}
			c.applyAuth(req)
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("Accept", "application/json")
			for _, values := range headers {
				maps.Copy(req.Header, values.Clone())
			}

			start := time.Now()
			logger.GetLogger().Debug("Sending request", "method", method, "url", fullURL, "env_id", c.envID, "streaming_body", true)
			resp, newRequestWithContextErr := c.httpClient.Do(req)
			if newRequestWithContextErr != nil {
				logger.GetLogger().Debug("Request failed", "method", method, "url", fullURL, "env_id", c.envID, "duration", time.Since(start).String(), "error", newRequestWithContextErr)
				return nil, fmt.Errorf("request failed: %w", newRequestWithContextErr)
			}
			logger.GetLogger().Debug("Response received", "method", method, "url", fullURL, "env_id", c.envID, "status", resp.Status, "duration", time.Since(start).String())
			return resp, nil
		default:
			jsonBody, marshalErr := json.Marshal(body)
			if marshalErr != nil {
				return nil, fmt.Errorf("failed to marshal request body: %w", marshalErr)
			}
			bodyBytes = jsonBody
		}
	}

	return c.doRequest(ctx, method, fullURL, bodyBytes, true, headers...)
}

// RequestRaw sends a streaming body with custom headers, without JSON encoding or automatic replay.
func (c *Client) RequestRaw(ctx context.Context, method, path string, body io.Reader, headers map[string]string) (*http.Response, error) {
	loading := output.StartLoading(ctx)
	defer loading.Stop()
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

	start := time.Now()
	logger.GetLogger().Debug("Sending raw request", "method", method, "url", fullURL, "env_id", c.envID)
	resp, err := c.httpClient.Do(req)
	if err != nil {
		logger.GetLogger().Debug("Raw request failed", "method", method, "url", fullURL, "env_id", c.envID, "duration", time.Since(start).String(), "error", err)
		return nil, fmt.Errorf("request failed: %w", err)
	}
	logger.GetLogger().Debug("Raw response received", "method", method, "url", fullURL, "env_id", c.envID, "status", resp.Status, "duration", time.Since(start).String())

	return resp, nil
}

func (c *Client) resolveURL(path string) (string, error) {
	if strings.TrimSpace(path) == "" {
		return "", errors.New("invalid path: empty")
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
		return "", errors.New("invalid base URL")
	}
	return c.baseURLParsed.ResolveReference(rel).String(), nil
}

func (c *Client) doRequest(ctx context.Context, method, fullURL string, bodyBytes []byte, allowRefresh bool, headers ...http.Header) (*http.Response, error) {
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
		for _, values := range headers {
			maps.Copy(req.Header, values.Clone())
		}

		start := time.Now()
		logger.GetLogger().Debug("Sending request", "method", method, "url", fullURL, "env_id", c.envID, "attempt", attempt, "max_attempts", attempts, "body_bytes", len(bodyBytes))
		resp, err := c.httpClient.Do(req)
		if err != nil {
			logger.GetLogger().Debug("Request failed", "method", method, "url", fullURL, "env_id", c.envID, "attempt", attempt, "duration", time.Since(start).String(), "error", err)
			if attempt < attempts && c.shouldRetry(method, 0, err) {
				logger.GetLogger().Debug("Retrying request after transport error", "method", method, "url", fullURL, "env_id", c.envID, "attempt", attempt, "max_attempts", attempts)
				if backoffErr := c.waitRetry(ctx, attempt); backoffErr != nil {
					return nil, backoffErr
				}
				continue
			}
			return nil, fmt.Errorf("request failed: %w", err)
		}
		logger.GetLogger().Debug("Response received", "method", method, "url", fullURL, "env_id", c.envID, "attempt", attempt, "status", resp.Status, "duration", time.Since(start).String())

		if resp.StatusCode == http.StatusUnauthorized && allowRefresh && c.jwtToken != "" && c.refreshToken != "" {
			logger.GetLogger().Debug("Refreshing access token after unauthorized response", "method", method, "url", fullURL, "env_id", c.envID, "attempt", attempt)
			_ = resp.Body.Close()
			if refreshAccessTokenErr := c.refreshAccessToken(ctx); refreshAccessTokenErr != nil {
				return nil, refreshAccessTokenErr
			}
			allowRefresh = false
			continue
		}

		if attempt < attempts && c.shouldRetry(method, resp.StatusCode, nil) {
			logger.GetLogger().Debug("Retrying request after retryable status",
				"method", method, "url", fullURL, "env_id", c.envID, "attempt", attempt, "max_attempts", attempts, "status", resp.Status)
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
	if !slices.Contains([]string{http.MethodGet, http.MethodHead, http.MethodOptions, http.MethodPut, http.MethodDelete}, method) {
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

		if netErr, ok := errors.AsType[net.Error](err); ok {
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
	if c.maxBackoff > 0 {
		delay = min(delay, c.maxBackoff)
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
		return errors.New("refresh token not configured; run `arcane auth login`")
	}

	bodyBytes, err := json.Marshal(map[string]string{"refreshToken": c.refreshToken})
	if err != nil {
		return fmt.Errorf("failed to marshal refresh request: %w", err)
	}

	if c.baseURLParsed == nil {
		return errors.New("invalid base URL")
	}
	refreshURL := c.baseURLParsed.ResolveReference(&url.URL{Path: clitypes.AuthRefresh()}).String()
	logger.GetLogger().Debug("Sending token refresh request", "url", refreshURL)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, refreshURL, bytes.NewReader(bodyBytes))
	if err != nil {
		return fmt.Errorf("failed to create refresh request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")

	start := time.Now()
	resp, err := c.httpClient.Do(req)
	if err != nil {
		logger.GetLogger().Debug("Token refresh request failed", "url", refreshURL, "duration", time.Since(start).String(), "error", err)
		return fmt.Errorf("token refresh failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	logger.GetLogger().Debug("Token refresh response received", "url", refreshURL, "status", resp.Status, "duration", time.Since(start).String())

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("failed to read refresh response: %w", err)
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("token refresh failed (status %d): %s", resp.StatusCode, strings.TrimSpace(string(respBody)))
	}

	var result types.ApiResponse[types.TokenRefreshResponse]
	if unmarshalErr := json.Unmarshal(respBody, &result); unmarshalErr != nil {
		return fmt.Errorf("failed to parse refresh response: %w", unmarshalErr)
	}
	if !result.Success || result.Data.Token == "" {
		return errors.New("token refresh failed: unexpected response from server")
	}

	newRefresh := cmp.Or(result.Data.RefreshToken, c.refreshToken)

	c.jwtToken = result.Data.Token
	c.refreshToken = newRefresh
	logger.GetLogger().Debug("Access token refreshed successfully")

	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}
	cfg.JWTToken = result.Data.Token
	cfg.APIKey = ""
	cfg.RefreshToken = newRefresh
	if saveErr := config.SaveTokens(ctx, cfg); saveErr != nil {
		return fmt.Errorf("failed to save refreshed token: %w", saveErr)
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

// DeleteWithBody sends a DELETE request with a JSON options body.
func (c *Client) DeleteWithBody(ctx context.Context, path string, body any) (*http.Response, error) {
	return c.Request(ctx, http.MethodDelete, path, body)
}

// DoJSON enforces HTTP success and decodes a response without requiring the standard API envelope.
func (c *Client) DoJSON[T any](ctx context.Context, method, path string, body any, headers ...http.Header) (T, error) {
	loading := output.StartLoading(ctx)
	defer loading.Stop()
	var out T
	resp, err := c.Request(ctx, method, path, body, headers...)
	if err != nil {
		return out, err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, maxErrorBody))
		return out, &StatusError{StatusCode: resp.StatusCode, Body: strings.TrimSpace(string(b))}
	}

	if unmarshalReadErr := json.UnmarshalRead(resp.Body, &out); unmarshalReadErr != nil {
		return out, fmt.Errorf("failed to decode response: %w", unmarshalReadErr)
	}
	return out, nil
}

// DoEnvelope sends a request and decodes the shared envelope, enforcing HTTP and API success.
func (c *Client) DoEnvelope[T any](ctx context.Context, method, path string, body any) (*types.ApiResponse[T], error) {
	loading := output.StartLoading(ctx)
	defer loading.Stop()
	resp, err := c.Request(ctx, method, path, body)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	return decodeResponseStrict[T](resp)
}

// GetJSON performs a GET and decodes the standard API envelope, enforcing
// HTTP and API success.
func (c *Client) GetJSON[T any](ctx context.Context, path string) (*types.ApiResponse[T], error) {
	return c.DoEnvelope[T](ctx, http.MethodGet, path, nil)
}

// PostJSON performs a POST with a JSON body and decodes the standard API
// envelope, enforcing HTTP and API success.
func (c *Client) PostJSON[T any](ctx context.Context, path string, body any) (*types.ApiResponse[T], error) {
	return c.DoEnvelope[T](ctx, http.MethodPost, path, body)
}

// DoRaw performs a request and returns the response payload when status is 2xx.
func (c *Client) DoRaw(ctx context.Context, method, path string, body any) ([]byte, error) {
	loading := output.StartLoading(ctx)
	defer loading.Stop()
	resp, err := c.Request(ctx, method, path, body)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, maxErrorBody))
		return nil, &StatusError{StatusCode: resp.StatusCode, Body: strings.TrimSpace(string(b))}
	}

	b, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response body: %w", err)
	}
	return b, nil
}

// PutJSON performs a PUT with a JSON body and decodes the standard API
// envelope, enforcing HTTP and API success.
func (c *Client) PutJSON[T any](ctx context.Context, path string, body any) (*types.ApiResponse[T], error) {
	return c.DoEnvelope[T](ctx, http.MethodPut, path, body)
}

// DeleteJSON performs a DELETE and decodes the standard API envelope,
// enforcing HTTP and API success.
func (c *Client) DeleteJSON[T any](ctx context.Context, path string) (*types.ApiResponse[T], error) {
	return c.DoEnvelope[T](ctx, http.MethodDelete, path, nil)
}

// decodeResponseStrict enforces HTTP and API success while decoding the response. The caller owns the response body.
func decodeResponseStrict[T any](resp *http.Response) (*types.ApiResponse[T], error) {
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, maxErrorBody))
		return nil, &StatusError{StatusCode: resp.StatusCode, Body: strings.TrimSpace(string(body))}
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response body: %w", err)
	}

	var result struct {
		types.ApiResponse[T]
		types.ErrorResponse
	}
	if unmarshalErr := json.Unmarshal(body, &result); unmarshalErr != nil {
		return nil, fmt.Errorf("failed to decode response (body: %s): %w", string(body), unmarshalErr)
	}

	if !result.Success {
		if strings.TrimSpace(result.Error) != "" {
			return &result.ApiResponse, fmt.Errorf("API error: %s", result.Error)
		}
		return &result.ApiResponse, errors.New("API error: request was not successful")
	}

	return &result.ApiResponse, nil
}

// TestConnection checks the API version endpoint and returns transport or status failures.
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
