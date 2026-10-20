// Package infisical is a small client for the parts of the Infisical API that
// Arcane uses: machine identity login, project/folder discovery, and reading
// the secrets of one environment path.
package infisical

import (
	"bytes"
	"context"
	"encoding/json/v2"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

type (
	// Config identifies one Infisical instance and the machine identity used to
	// authenticate against it with Universal Auth.
	Config struct {
		SiteURL          string
		ClientID         string
		ClientSecret     string
		OrganizationSlug string
	}

	// APIError is a non-2xx response from Infisical.
	APIError struct {
		Message    string
		StatusCode int
	}

	// Session describes a successful login.
	Session struct {
		ExpiresIn time.Duration
	}

	Environment struct {
		ID   string `json:"id"`
		Name string `json:"name"`
		Slug string `json:"slug"`
	}

	Project struct {
		ID           string        `json:"id"`
		Name         string        `json:"name"`
		Slug         string        `json:"slug"`
		Environments []Environment `json:"environments"`
	}

	// SecretsQuery selects one environment path of one project.
	SecretsQuery struct {
		ProjectID              string
		Environment            string
		SecretPath             string
		IncludeImports         bool
		ExpandSecretReferences bool
	}

	// Client talks to one Infisical instance. It caches the Universal Auth access
	// token until shortly before it expires. A Client is safe for concurrent use.
	Client struct {
		tokenExpiry time.Time
		httpClient  *http.Client
		baseURL     *url.URL
		now         func() time.Time
		config      Config
		accessToken string
		mu          sync.Mutex
	}

	secretItem struct {
		SecretKey         string `json:"secretKey"`
		SecretValue       string `json:"secretValue"`
		SecretValueHidden bool   `json:"secretValueHidden"`
	}

	secretsResponse struct {
		Secrets []secretItem `json:"secrets"`
		Imports []struct {
			Secrets []secretItem `json:"secrets"`
		} `json:"imports"`
	}
)

const (
	// DefaultSiteURL is Infisical Cloud (US region).
	DefaultSiteURL = "https://app.infisical.com"

	maxResponseBytes = 10 << 20
	// tokenRefreshSkew renews the access token this long before it expires so
	// a request never starts with a token that lapses mid-flight.
	tokenRefreshSkew = 30 * time.Second
)

var (
	// ErrSecretValueHidden is returned when the identity can list secrets but
	// is not allowed to read their values.
	ErrSecretValueHidden  = errors.New("infisical returned hidden secret values; grant the machine identity read access to secret values")
	ErrMissingCredentials = errors.New("client ID and client secret are required")
)

func (e *APIError) Error() string {
	if e.Message == "" {
		return fmt.Sprintf("infisical returned HTTP %d", e.StatusCode)
	}
	return fmt.Sprintf("infisical returned HTTP %d: %s", e.StatusCode, e.Message)
}

// NewClient validates cfg and returns a client. httpClient may be nil.
func NewClient(httpClient *http.Client, cfg Config) (*Client, error) {
	baseURL, err := ParseSiteURL(cfg.SiteURL)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(cfg.ClientID) == "" || cfg.ClientSecret == "" {
		return nil, ErrMissingCredentials
	}
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 15 * time.Second}
	}
	return &Client{
		httpClient: httpClient,
		baseURL:    baseURL,
		config:     cfg,
		now:        time.Now,
	}, nil
}

// ParseSiteURL normalizes an Infisical site URL. An empty value means
// Infisical Cloud. A path prefix is kept for instances served under a subpath.
func ParseSiteURL(raw string) (*url.URL, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		raw = DefaultSiteURL
	}
	parsed, err := url.Parse(raw)
	if err != nil {
		return nil, fmt.Errorf("invalid Infisical site URL: %w", err)
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return nil, fmt.Errorf("invalid Infisical site URL: scheme must be http or https, got %q", parsed.Scheme)
	}
	if parsed.Host == "" {
		return nil, errors.New("invalid Infisical site URL: missing host")
	}
	if parsed.User != nil {
		return nil, errors.New("invalid Infisical site URL: credentials in the URL are not allowed")
	}
	parsed.Path = strings.TrimRight(parsed.Path, "/")
	parsed.RawQuery = ""
	parsed.Fragment = ""
	return parsed, nil
}

// Login authenticates with Universal Auth and caches the access token. It
// always performs a fresh login, so it doubles as a connection test.
func (c *Client) Login(ctx context.Context) (Session, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	expiresIn, err := c.loginLockedInternal(ctx)
	if err != nil {
		return Session{}, err
	}
	return Session{ExpiresIn: expiresIn}, nil
}

// ListProjects returns the secret-manager projects the identity can access.
// Instances where the identity cannot list projects return an APIError; callers
// should then let the user enter a project ID by hand.
func (c *Client) ListProjects(ctx context.Context) ([]Project, error) {
	var payload struct {
		Projects []Project `json:"projects"`
	}
	query := url.Values{"type": {"secret-manager"}}
	if err := c.getJSONInternal(ctx, "/api/v1/projects", query, &payload); err != nil {
		return nil, err
	}
	return payload.Projects, nil
}

// ListFolders returns the names of the folders directly under path.
func (c *Client) ListFolders(ctx context.Context, projectID, environment, path string) ([]string, error) {
	var payload struct {
		Folders []struct {
			Name string `json:"name"`
		} `json:"folders"`
	}
	query := url.Values{
		"projectId":   {projectID},
		"environment": {environment},
		"path":        {NormalizeSecretPath(path)},
	}
	if err := c.getJSONInternal(ctx, "/api/v2/folders", query, &payload); err != nil {
		return nil, err
	}
	names := make([]string, 0, len(payload.Folders))
	for _, folder := range payload.Folders {
		names = append(names, folder.Name)
	}
	return names, nil
}

// ListSecrets returns the key/value pairs of one environment path.
//
// Precedence matches the Infisical CLI: a secret defined in the path itself
// wins over an imported one, and a later import wins over an earlier import.
func (c *Client) ListSecrets(ctx context.Context, q SecretsQuery) (map[string]string, error) {
	if strings.TrimSpace(q.ProjectID) == "" || strings.TrimSpace(q.Environment) == "" {
		return nil, errors.New("project ID and environment are required")
	}
	query := url.Values{
		"projectId":              {q.ProjectID},
		"environment":            {q.Environment},
		"secretPath":             {NormalizeSecretPath(q.SecretPath)},
		"includeImports":         {formatBoolInternal(q.IncludeImports)},
		"expandSecretReferences": {formatBoolInternal(q.ExpandSecretReferences)},
		"viewSecretValue":        {"true"},
	}
	var payload secretsResponse
	if err := c.getJSONInternal(ctx, "/api/v4/secrets", query, &payload); err != nil {
		return nil, err
	}
	return mergeSecretsInternal(payload)
}

func mergeSecretsInternal(payload secretsResponse) (map[string]string, error) {
	values := make(map[string]string, len(payload.Secrets))
	apply := func(items []secretItem) error {
		for _, item := range items {
			if item.SecretValueHidden {
				return ErrSecretValueHidden
			}
			values[item.SecretKey] = item.SecretValue
		}
		return nil
	}
	for _, imported := range payload.Imports {
		if err := apply(imported.Secrets); err != nil {
			return nil, err
		}
	}
	if err := apply(payload.Secrets); err != nil {
		return nil, err
	}
	return values, nil
}

// NormalizeSecretPath turns user input such as "", "api", or "/api/" into the
// absolute folder path Infisical expects.
func NormalizeSecretPath(path string) string {
	path = strings.Trim(strings.TrimSpace(path), "/")
	if path == "" {
		return "/"
	}
	return "/" + path
}

func formatBoolInternal(v bool) string {
	if v {
		return "true"
	}
	return "false"
}

func (c *Client) tokenInternal(ctx context.Context) (string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.accessToken != "" && c.now().Before(c.tokenExpiry) {
		return c.accessToken, nil
	}
	if _, err := c.loginLockedInternal(ctx); err != nil {
		return "", err
	}
	return c.accessToken, nil
}

// loginLockedInternal must be called with c.mu held.
func (c *Client) loginLockedInternal(ctx context.Context) (time.Duration, error) {
	body := map[string]string{
		"clientId":     strings.TrimSpace(c.config.ClientID),
		"clientSecret": c.config.ClientSecret,
	}
	if slug := strings.TrimSpace(c.config.OrganizationSlug); slug != "" {
		body["organizationSlug"] = slug
	}
	encoded, err := json.Marshal(body)
	if err != nil {
		return 0, fmt.Errorf("encode login request: %w", err)
	}

	var payload struct {
		AccessToken string  `json:"accessToken"`
		ExpiresIn   float64 `json:"expiresIn"`
	}
	if doErr := c.doInternal(ctx, http.MethodPost, "/api/v1/auth/universal-auth/login", nil, encoded, "", &payload); doErr != nil {
		return 0, doErr
	}
	if payload.AccessToken == "" {
		return 0, errors.New("infisical login returned no access token")
	}

	expiresIn := time.Duration(payload.ExpiresIn) * time.Second
	c.accessToken = payload.AccessToken
	c.tokenExpiry = c.now().Add(expiresIn - tokenRefreshSkew)
	return expiresIn, nil
}

func (c *Client) getJSONInternal(ctx context.Context, path string, query url.Values, out any) error {
	token, err := c.tokenInternal(ctx)
	if err != nil {
		return err
	}
	return c.doInternal(ctx, http.MethodGet, path, query, nil, token, out)
}

func (c *Client) doInternal(ctx context.Context, method, path string, query url.Values, body []byte, token string, out any) error {
	endpoint := *c.baseURL
	endpoint.Path = c.baseURL.Path + path
	endpoint.RawQuery = query.Encode()

	var reader io.Reader
	if body != nil {
		reader = bytes.NewReader(body)
	}
	req, err := http.NewRequestWithContext(ctx, method, endpoint.String(), reader)
	if err != nil {
		return fmt.Errorf("build infisical request: %w", err)
	}
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("contact infisical: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	raw, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes))
	if err != nil {
		return fmt.Errorf("read infisical response: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return &APIError{StatusCode: resp.StatusCode, Message: errorMessageInternal(raw)}
	}
	if out == nil {
		return nil
	}
	if unmarshalErr := json.Unmarshal(raw, out); unmarshalErr != nil {
		return fmt.Errorf("decode infisical response: %w", unmarshalErr)
	}
	return nil
}

// errorMessageInternal extracts Infisical's error message without echoing an
// arbitrary response body back to the user.
func errorMessageInternal(raw []byte) string {
	var payload struct {
		Message string `json:"message"`
		Error   string `json:"error"`
	}
	if err := json.Unmarshal(raw, &payload); err != nil {
		return ""
	}
	message := strings.TrimSpace(payload.Message)
	if message == "" {
		message = strings.TrimSpace(payload.Error)
	}
	const maxMessage = 300
	if len(message) > maxMessage {
		message = message[:maxMessage]
	}
	return message
}
