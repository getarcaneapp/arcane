// Package vault is a small client for the KV secrets engine of HashiCorp Vault
// and OpenBao, which share the same HTTP API. It supports token and AppRole
// authentication, KV version 1 and 2, and an optional namespace.
package vault

import (
	"context"
	"encoding/json/jsontext"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"getarcane.dev/arcane/v3/pkg/secretapi"
)

type (
	// Config describes how to reach and log in to one server.
	Config struct {
		Address   string
		Namespace string
		// AuthMethod is AuthToken or AuthAppRole.
		AuthMethod string
		// RoleID and AppRoleMount are used with AuthAppRole.
		RoleID       string
		AppRoleMount string
		// Secret is the token (AuthToken) or the AppRole secret ID.
		Secret string
	}

	// TokenInfo describes the token the client uses.
	TokenInfo struct {
		DisplayName string   `json:"display_name"`
		Policies    []string `json:"policies"`
		TTL         int64    `json:"ttl"`
	}

	// Mount is a KV mount visible to the token.
	Mount struct {
		Path    string
		Type    string
		Version string
	}

	// loginRequestKey marks the context of the AppRole login request, which is
	// sent without a token.
	loginRequestKey struct{}

	// Client talks to one Vault or OpenBao server. It is safe for concurrent use.
	Client struct {
		expiresAt time.Time
		api       *secretapi.Requester
		config    Config
		token     string
		mu        sync.Mutex
	}
)

const (
	// Authentication methods.
	AuthToken   = "token"
	AuthAppRole = "approle"

	// serviceName names the server in errors.
	serviceName = "Vault/OpenBao"

	// tokenRefreshMargin renews an AppRole token this long before it expires.
	tokenRefreshMargin = 30 * time.Second
)

// ParseAddress validates a server address such as http://openbao:8200.
func ParseAddress(raw string) (*url.URL, error) {
	return secretapi.ParseBaseURL(raw, "the server address", "http://openbao:8200")
}

// NewClient validates config and returns a client. httpClient may be nil.
func NewClient(httpClient *http.Client, config Config) (*Client, error) {
	base, err := ParseAddress(config.Address)
	if err != nil {
		return nil, err
	}
	config.AuthMethod = strings.TrimSpace(config.AuthMethod)
	if config.AuthMethod == "" {
		config.AuthMethod = AuthToken
	}
	switch config.AuthMethod {
	case AuthToken:
	case AuthAppRole:
		if strings.TrimSpace(config.RoleID) == "" {
			return nil, errors.New("an AppRole role ID is required")
		}
		if strings.TrimSpace(config.AppRoleMount) == "" {
			config.AppRoleMount = "approle"
		}
	default:
		return nil, fmt.Errorf("unknown auth method %q; use token or approle", config.AuthMethod)
	}
	if strings.TrimSpace(config.Secret) == "" {
		if config.AuthMethod == AuthAppRole {
			return nil, errors.New("an AppRole secret ID is required")
		}
		return nil, errors.New("a token is required")
	}

	client := &Client{config: config}
	client.api = secretapi.NewRequester(httpClient, base, serviceName)
	client.api.Authorize = client.authorizeInternal
	return client, nil
}

func (c *Client) authorizeInternal(ctx context.Context, req *http.Request) error {
	if c.config.Namespace != "" {
		req.Header.Set("X-Vault-Namespace", c.config.Namespace)
	}
	// The AppRole login itself carries no token.
	if skip, _ := ctx.Value(loginRequestKey{}).(bool); skip {
		return nil
	}
	token, err := c.tokenInternal(ctx)
	if err != nil {
		return err
	}
	req.Header.Set("X-Vault-Token", token)
	return nil
}

func (c *Client) tokenInternal(ctx context.Context) (string, error) {
	if c.config.AuthMethod == AuthToken {
		return c.config.Secret, nil
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.token != "" && (c.expiresAt.IsZero() || time.Now().Before(c.expiresAt)) {
		return c.token, nil
	}

	var resp struct {
		Auth *struct {
			ClientToken   string `json:"client_token"`
			LeaseDuration int64  `json:"lease_duration"`
		} `json:"auth"`
	}
	body := map[string]string{"role_id": c.config.RoleID, "secret_id": c.config.Secret}
	path := "/v1/auth/" + secretapi.PathEscapeSegments(c.config.AppRoleMount) + "/login"
	loginCtx := context.WithValue(ctx, loginRequestKey{}, true)
	if err := c.api.Do(loginCtx, http.MethodPost, path, nil, body, &resp); err != nil {
		return "", fmt.Errorf("AppRole login failed: %w", err)
	}
	if resp.Auth == nil || resp.Auth.ClientToken == "" {
		return "", errors.New("AppRole login returned no token")
	}
	c.token = resp.Auth.ClientToken
	c.expiresAt = time.Time{}
	if resp.Auth.LeaseDuration > 0 {
		lease := time.Duration(resp.Auth.LeaseDuration) * time.Second
		// Renew a little early, but never use up more than half a short lease.
		c.expiresAt = time.Now().Add(lease - min(tokenRefreshMargin, lease/2))
	}
	return c.token, nil
}

// forgetTokenInternal drops a cached AppRole token, so the next request logs
// in again.
func (c *Client) forgetTokenInternal() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.token = ""
}

// doInternal sends one request. With AppRole, a 401/403 may mean the cached
// token was revoked or expired early, so it logs in again and retries once.
func (c *Client) doInternal(ctx context.Context, method, path string, query url.Values, body, out any) error {
	err := c.api.Do(ctx, method, path, query, body, out)
	if err != nil && c.config.AuthMethod == AuthAppRole && secretapi.IsUnauthorized(err) {
		c.forgetTokenInternal()
		err = c.api.Do(ctx, method, path, query, body, out)
	}
	return err
}

// LookupSelf describes the token, which also proves it works.
func (c *Client) LookupSelf(ctx context.Context) (*TokenInfo, error) {
	var resp struct {
		Data TokenInfo `json:"data"`
	}
	if err := c.doInternal(ctx, http.MethodGet, "/v1/auth/token/lookup-self", nil, nil, &resp); err != nil {
		return nil, err
	}
	return &resp.Data, nil
}

// KVMounts lists the KV mounts the token can see. Servers that hide this
// endpoint return an error; callers fall back to typing the mount.
func (c *Client) KVMounts(ctx context.Context) ([]Mount, error) {
	var resp struct {
		Data struct {
			Secret map[string]struct {
				Options map[string]string `json:"options"`
				Type    string            `json:"type"`
			} `json:"secret"`
		} `json:"data"`
	}
	if err := c.doInternal(ctx, http.MethodGet, "/v1/sys/internal/ui/mounts", nil, nil, &resp); err != nil {
		return nil, err
	}
	mounts := []Mount{}
	for path, mount := range resp.Data.Secret {
		if mount.Type != "kv" && mount.Type != "generic" {
			continue
		}
		version := mount.Options["version"]
		if version == "" {
			version = "1"
		}
		mounts = append(mounts, Mount{Path: strings.TrimSuffix(path, "/"), Type: mount.Type, Version: version})
	}
	return mounts, nil
}

// List returns the keys under path. Keys ending in "/" are sub-paths.
func (c *Client) List(ctx context.Context, mount string, kvVersion int, path string) ([]string, error) {
	var resp struct {
		Data struct {
			Keys []string `json:"keys"`
		} `json:"data"`
	}
	endpoint := kvPathInternal(mount, kvVersion, "metadata", path) + "/"
	err := c.doInternal(ctx, http.MethodGet, endpoint, url.Values{"list": {"true"}}, nil, &resp)
	if secretapi.IsNotFound(err) {
		return []string{}, nil
	}
	if err != nil {
		return nil, err
	}
	return resp.Data.Keys, nil
}

// Read returns the key/value pairs of one secret and, for KV version 2, its
// current version. A missing secret returns an error matched by IsNotFound.
func (c *Client) Read(ctx context.Context, mount string, kvVersion int, path string) (map[string]jsontext.Value, int, error) {
	endpoint := kvPathInternal(mount, kvVersion, "data", path)
	if kvVersion == 2 {
		var resp struct {
			Data *struct {
				Data     map[string]jsontext.Value `json:"data"`
				Metadata struct {
					Version int `json:"version"`
				} `json:"metadata"`
			} `json:"data"`
		}
		if err := c.doInternal(ctx, http.MethodGet, endpoint, nil, nil, &resp); err != nil {
			return nil, 0, err
		}
		if resp.Data == nil || resp.Data.Data == nil {
			// A deleted latest version answers 404 with metadata only.
			return nil, 0, &secretapi.APIError{Service: serviceName, StatusCode: http.StatusNotFound, Message: "the secret has no data"}
		}
		return resp.Data.Data, resp.Data.Metadata.Version, nil
	}
	var resp struct {
		Data map[string]jsontext.Value `json:"data"`
	}
	if err := c.doInternal(ctx, http.MethodGet, endpoint, nil, nil, &resp); err != nil {
		return nil, 0, err
	}
	return resp.Data, 0, nil
}

// CurrentVersion returns the latest version number of a KV v2 secret, even
// when that version is soft-deleted, or 0 when the secret does not exist.
func (c *Client) CurrentVersion(ctx context.Context, mount, path string) (int, error) {
	var resp struct {
		Data struct {
			CurrentVersion int `json:"current_version"`
		} `json:"data"`
	}
	err := c.doInternal(ctx, http.MethodGet, kvPathInternal(mount, 2, "metadata", path), nil, nil, &resp)
	if secretapi.IsNotFound(err) {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	return resp.Data.CurrentVersion, nil
}

// Write stores data at path, replacing what is there. For KV version 2, cas
// is the version the write expects to replace (0 creates); a negative cas
// skips the check.
func (c *Client) Write(ctx context.Context, mount string, kvVersion int, path string, data map[string]any, cas int) error {
	endpoint := kvPathInternal(mount, kvVersion, "data", path)
	if kvVersion == 2 {
		body := map[string]any{"data": data}
		if cas >= 0 {
			body["options"] = map[string]int{"cas": cas}
		}
		return c.doInternal(ctx, http.MethodPost, endpoint, nil, body, nil)
	}
	return c.doInternal(ctx, http.MethodPost, endpoint, nil, data, nil)
}

// IsNotFound reports whether err means the path does not exist.
func IsNotFound(err error) bool {
	return secretapi.IsNotFound(err)
}

// kvPathInternal builds /v1/<mount>/<data|metadata>/<path> for KV v2 and
// /v1/<mount>/<path> for KV v1.
func kvPathInternal(mount string, kvVersion int, kind, path string) string {
	endpoint := "/v1/" + secretapi.PathEscapeSegments(mount)
	if kvVersion == 2 {
		endpoint += "/" + kind
	}
	if trimmed := strings.Trim(path, "/"); trimmed != "" {
		endpoint += "/" + secretapi.PathEscapeSegments(trimmed)
	}
	return endpoint
}
