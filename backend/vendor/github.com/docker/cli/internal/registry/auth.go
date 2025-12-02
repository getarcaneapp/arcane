package registry

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/containerd/log"
	"github.com/docker/distribution/registry/client/auth"
	"github.com/docker/distribution/registry/client/auth/challenge"
	"github.com/docker/distribution/registry/client/transport"
	"github.com/docker/docker/api/types/registry"
)

// AuthClientID is used the ClientID used for the token server
const AuthClientID = "docker"

type loginCredentialStore struct {
	authConfig *registry.AuthConfig
}

func (lcs loginCredentialStore) Basic(*url.URL) (string, string) {
	return lcs.authConfig.Username, lcs.authConfig.Password
}

func (lcs loginCredentialStore) RefreshToken(*url.URL, string) string {
	return lcs.authConfig.IdentityToken
}

func (lcs loginCredentialStore) SetRefreshToken(u *url.URL, service, token string) {
	lcs.authConfig.IdentityToken = token
}

// loginV2 tries to login to the v2 registry server. The given registry
// endpoint will be pinged to get authorization challenges. These challenges
// will be used to authenticate against the registry to validate credentials.
func loginV2(ctx context.Context, authConfig *registry.AuthConfig, endpoint APIEndpoint, userAgent string) (token string, _ error) {
	endpointStr := strings.TrimRight(endpoint.URL.String(), "/") + "/v2/"
	log.G(ctx).WithField("endpoint", endpointStr).Debug("attempting v2 login to registry endpoint")

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpointStr, http.NoBody)
	if err != nil {
		return "", err
	}

	var (
		modifiers            = Headers(userAgent, nil)
		authTrans            = transport.NewTransport(newTransport(endpoint.TLSConfig), modifiers...)
		credentialAuthConfig = *authConfig
		creds                = loginCredentialStore{authConfig: &credentialAuthConfig}
	)

	loginClient, err := v2AuthHTTPClient(endpoint.URL, authTrans, modifiers, creds, nil)
	if err != nil {
		return "", err
	}

	resp, err := loginClient.Do(req)
	if err != nil {
		err = translateV2AuthError(err)
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		// TODO(dmcgowan): Attempt to further interpret result, status code and error code string
		return "", fmt.Errorf("login attempt to %s failed with status: %d %s", endpointStr, resp.StatusCode, http.StatusText(resp.StatusCode))
	}

	return credentialAuthConfig.IdentityToken, nil
}

func v2AuthHTTPClient(endpoint *url.URL, authTransport http.RoundTripper, modifiers []transport.RequestModifier, creds auth.CredentialStore, scopes []auth.Scope) (*http.Client, error) {
	challengeManager, err := PingV2Registry(endpoint, authTransport)
	if err != nil {
		return nil, err
	}

	authHandlers := []auth.AuthenticationHandler{
		auth.NewTokenHandlerWithOptions(auth.TokenHandlerOptions{
			Transport:     authTransport,
			Credentials:   creds,
			OfflineAccess: true,
			ClientID:      AuthClientID,
			Scopes:        scopes,
		}),
		auth.NewBasicHandler(creds),
	}

	modifiers = append(modifiers, auth.NewAuthorizer(challengeManager, authHandlers...))

	return &http.Client{
		Transport: transport.NewTransport(authTransport, modifiers...),
		Timeout:   15 * time.Second,
	}, nil
}

// PingV2Registry attempts to ping a v2 registry and on success return a
// challenge manager for the supported authentication types.
// If a response is received but cannot be interpreted, a PingResponseError will be returned.
func PingV2Registry(endpoint *url.URL, authTransport http.RoundTripper) (challenge.Manager, error) {
	endpointStr := strings.TrimRight(endpoint.String(), "/") + "/v2/"
	req, err := http.NewRequest(http.MethodGet, endpointStr, http.NoBody)
	if err != nil {
		return nil, err
	}
	pingClient := &http.Client{
		Transport: authTransport,
		Timeout:   15 * time.Second,
	}
	resp, err := pingClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	challengeManager := challenge.NewSimpleManager()
	if err := challengeManager.AddResponse(resp); err != nil {
		return nil, err
	}

	return challengeManager, nil
}
