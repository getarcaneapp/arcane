// Package doppler is a small read-only client for the Doppler API.
package doppler

import (
	"context"
	"encoding/json/jsontext"
	"errors"
	"net/http"
	"net/url"
	"strings"

	"getarcane.dev/arcane/v3/pkg/secretapi"
)

type (
	// TokenInfo describes the token the client uses.
	TokenInfo struct {
		Name      string `json:"name"`
		Type      string `json:"type_"`
		Workplace struct {
			Name string `json:"name"`
		} `json:"workplace"`
	}

	// Project is a Doppler project.
	Project struct {
		Slug string `json:"slug"`
		Name string `json:"name"`
	}

	// Config is a Doppler config of one project.
	Config struct {
		Name        string `json:"name"`
		Environment string `json:"environment"`
	}

	// Client talks to the Doppler API with one token. It is safe for concurrent use.
	Client struct {
		api *secretapi.Requester
	}
)

const (
	// DefaultAPIURL is Doppler's public API.
	DefaultAPIURL = "https://api.doppler.com"
)

var (
	// metaKeys are added to every download by Doppler and describe the config
	// rather than the application, so they are not delivered as secrets.
	metaKeys = []string{"DOPPLER_PROJECT", "DOPPLER_CONFIG", "DOPPLER_ENVIRONMENT"}
)

// ParseAPIURL validates the API address; empty means DefaultAPIURL.
func ParseAPIURL(raw string) (*url.URL, error) {
	if strings.TrimSpace(raw) == "" {
		raw = DefaultAPIURL
	}
	return secretapi.ParseBaseURL(raw, "the Doppler API URL", DefaultAPIURL)
}

// NewClient returns a client for token. httpClient may be nil.
func NewClient(httpClient *http.Client, apiURL, token string) (*Client, error) {
	base, err := ParseAPIURL(apiURL)
	if err != nil {
		return nil, err
	}
	token = strings.TrimSpace(token)
	if token == "" {
		return nil, errors.New("a Doppler token is required")
	}
	api := secretapi.NewRequester(httpClient, base, "Doppler")
	api.Authorize = func(_ context.Context, req *http.Request) error {
		req.Header.Set("Authorization", "Bearer "+token)
		return nil
	}
	return &Client{api: api}, nil
}

// Me describes the token, which also proves it works.
func (c *Client) Me(ctx context.Context) (*TokenInfo, error) {
	var info TokenInfo
	if err := c.api.Do(ctx, http.MethodGet, "/v3/me", nil, nil, &info); err != nil {
		return nil, err
	}
	return &info, nil
}

// Projects lists projects. Service tokens cannot list and get an error.
func (c *Client) Projects(ctx context.Context) ([]Project, error) {
	var resp struct {
		Projects []Project `json:"projects"`
	}
	if err := c.api.Do(ctx, http.MethodGet, "/v3/projects", url.Values{"per_page": {"100"}}, nil, &resp); err != nil {
		return nil, err
	}
	return resp.Projects, nil
}

// Configs lists the configs of a project.
func (c *Client) Configs(ctx context.Context, project string) ([]Config, error) {
	var resp struct {
		Configs []Config `json:"configs"`
	}
	query := url.Values{"project": {project}, "per_page": {"100"}}
	if err := c.api.Do(ctx, http.MethodGet, "/v3/configs", query, nil, &resp); err != nil {
		return nil, err
	}
	return resp.Configs, nil
}

// Download returns the secrets of a config as JSON values. project and config
// may be empty with a service token, which is scoped to one config.
func (c *Client) Download(ctx context.Context, project, config string) (map[string]jsontext.Value, error) {
	query := url.Values{"format": {"json"}}
	if project != "" {
		query.Set("project", project)
	}
	if config != "" {
		query.Set("config", config)
	}
	var secrets map[string]jsontext.Value
	if err := c.api.Do(ctx, http.MethodGet, "/v3/configs/config/secrets/download", query, nil, &secrets); err != nil {
		return nil, err
	}
	for _, key := range metaKeys {
		delete(secrets, key)
	}
	return secrets, nil
}
