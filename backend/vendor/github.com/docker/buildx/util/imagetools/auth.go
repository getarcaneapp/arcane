package imagetools

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"sync"
	"time"

	"github.com/containerd/containerd/v2/core/remotes/docker"
	"github.com/distribution/reference"
	"github.com/docker/cli/cli/config/types"
)

type authConfig struct {
	mu              sync.Mutex
	authConfigCache map[string]authConfigCacheEntry
	cfg             Auth
}

type authConfigCacheEntry struct {
	Created time.Time
	Auth    types.AuthConfig
}

func newAuthConfig(a Auth) *authConfig {
	return &authConfig{
		authConfigCache: map[string]authConfigCacheEntry{},
		cfg:             a,
	}
}

func (a *authConfig) credentials(host string) (string, string, error) {
	ac, err := a.authConfig(host)
	if err != nil {
		return "", "", err
	}
	if ac.IdentityToken != "" {
		return "", ac.IdentityToken, nil
	}
	return ac.Username, ac.Password, nil
}

func (a *authConfig) authConfig(host string) (types.AuthConfig, error) {
	const defaultExpiration = 2 * time.Minute

	if host == "registry-1.docker.io" {
		host = "https://index.docker.io/v1/"
	}
	a.mu.Lock()
	defer a.mu.Unlock()

	if c, ok := a.authConfigCache[host]; ok && time.Since(c.Created) <= defaultExpiration {
		return c.Auth, nil
	}
	ac, err := a.cfg.GetAuthConfig(host)
	if err != nil {
		return types.AuthConfig{}, err
	}
	a.authConfigCache[host] = authConfigCacheEntry{
		Created: time.Now(),
		Auth:    ac,
	}
	return ac, nil
}

func RegistryAuthForRef(ref string, a Auth) (string, error) {
	if a == nil {
		return "", nil
	}
	r, err := parseRef(ref)
	if err != nil {
		return "", err
	}
	host := reference.Domain(r)
	if host == "docker.io" {
		host = "https://index.docker.io/v1/"
	}
	ac, err := a.GetAuthConfig(host)
	if err != nil {
		return "", err
	}
	buf, err := json.Marshal(ac)
	if err != nil {
		return "", err
	}
	return base64.URLEncoding.EncodeToString(buf), nil
}

type withBearerAuthorizer struct {
	docker.Authorizer
	AuthConfig *authConfig
}

func (a *withBearerAuthorizer) Authorize(ctx context.Context, req *http.Request) error {
	ac, err := a.AuthConfig.authConfig(req.Host)
	if err == nil && ac.RegistryToken != "" {
		req.Header.Set("Authorization", "Bearer "+ac.RegistryToken)
		return nil
	}
	return a.Authorizer.Authorize(ctx, req)
}
