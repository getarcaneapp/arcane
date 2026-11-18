// Package httpsecrets reads secrets from any HTTP endpoint that answers GET
// with a flat JSON object of names and values:
//
//	{"DB_PASSWORD": "s3cret", "API_TOKEN": "abc", "PORT": 5432}
//
// It is the contract for sidecars such as sops-kit and bws-kit, and for
// anything else that can speak it.
package httpsecrets

import (
	"context"
	"encoding/json/jsontext"
	"net/http"
	"net/url"
	"strings"

	"getarcane.dev/arcane/v3/pkg/secretapi"
)

// Client reads from one base URL. It is safe for concurrent use.
type Client struct {
	api *secretapi.Requester
}

// ParseBaseURL validates the endpoint address.
func ParseBaseURL(raw string) (*url.URL, error) {
	return secretapi.ParseBaseURL(raw, "the endpoint URL", "http://sops-kit:8080")
}

// NewClient returns a client. token is optional; when set it is sent as a
// bearer token. httpClient may be nil.
func NewClient(httpClient *http.Client, baseURL, token string) (*Client, error) {
	base, err := ParseBaseURL(baseURL)
	if err != nil {
		return nil, err
	}
	api := secretapi.NewRequester(httpClient, base, "the secrets endpoint")
	if token = strings.TrimSpace(token); token != "" {
		api.Authorize = func(_ context.Context, req *http.Request) error {
			req.Header.Set("Authorization", "Bearer "+token)
			return nil
		}
	}
	return &Client{api: api}, nil
}

// Get reads the object at path, which is appended to the base URL. An empty
// path reads the base URL itself.
func (c *Client) Get(ctx context.Context, path string) (map[string]jsontext.Value, error) {
	endpoint := ""
	if trimmed := strings.Trim(path, "/"); trimmed != "" {
		endpoint = "/" + secretapi.PathEscapeSegments(trimmed)
	}
	var object map[string]jsontext.Value
	if err := c.api.Do(ctx, http.MethodGet, endpoint, nil, nil, &object); err != nil {
		return nil, err
	}
	if object == nil {
		object = map[string]jsontext.Value{}
	}
	return object, nil
}
