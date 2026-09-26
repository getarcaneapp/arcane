package registry

import (
	"context"
	"encoding/base64"
	"io"
	"net/http"
	"strings"
	"testing"

	"emperror.dev/errors"
	"github.com/getarcaneapp/arcane/types/v2/containerregistry"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestListImageTagsCredentialsInternal(t *testing.T) {
	for _, tt := range []struct {
		name         string
		external     []containerregistry.Credential
		wantUser     string
		wantPassword string
	}{
		{name: "stored", wantUser: "stored-user", wantPassword: "stored-token"},
		{name: "external overrides stored", external: []containerregistry.Credential{{URL: "docker.io", Username: "external-user", Token: "external-token", Enabled: true}}, wantUser: "external-user", wantPassword: "external-token"},
		{name: "external excludes unrelated stored", external: []containerregistry.Credential{{URL: "other.test", Username: "external-user", Token: "external-token", Enabled: true}}},
		{name: "disabled external ignored", external: []containerregistry.Credential{{URL: "docker.io", Username: "external-user", Token: "external-token", Enabled: false}}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			db := setupContainerRegistryTestDBInternal(t)
			createTestPullRegistryInternal(t, db, "https://index.docker.io/v1/", "stored-user", "stored-token")
			calls := 0
			client := &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
				assert.Equal(t, "registry-1.docker.io", req.URL.Host)
				if req.URL.Path == "/v2/" {
					return &http.Response{StatusCode: http.StatusOK, Body: http.NoBody, Header: http.Header{}}, nil
				}
				calls++
				assert.Equal(t, "/v2/library/nginx/tags/list", req.URL.Path)
				user, password, _ := req.BasicAuth()
				assert.Equal(t, tt.wantUser, user)
				assert.Equal(t, tt.wantPassword, password)
				return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(`{"name":"library/nginx","tags":["1.2.3","1.2.4"]}`)), Header: http.Header{}}, nil
			})}
			svc := NewContainerRegistryService(db, nil, nil, client)
			tags, err := svc.ListImageTags(t.Context(), "nginx:1.2.3", tt.external)
			require.NoError(t, err)
			assert.Equal(t, []string{"1.2.3", "1.2.4"}, tags)
			assert.Equal(t, 1, calls)
		})
	}
}

func TestListImageTagsDoesNotFallBackOnRateLimitInternal(t *testing.T) {
	// The registry client retries 429s itself, so count credentialed and anonymous listings rather than requests.
	credentialed, anonymous := 0, 0
	svc := NewContainerRegistryService(nil, nil, nil, &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		if req.URL.Path == "/v2/" {
			return &http.Response{StatusCode: http.StatusOK, Body: http.NoBody, Header: http.Header{}}, nil
		}
		if req.Header.Get("Authorization") == "" {
			anonymous++
		} else {
			credentialed++
		}
		return &http.Response{StatusCode: http.StatusTooManyRequests, Body: http.NoBody, Header: http.Header{}}, nil
	})})
	tags, err := svc.ListImageTags(t.Context(), "registry.test/team/app:1.0.0", []containerregistry.Credential{{URL: "registry.test", Username: "user", Token: "token", Enabled: true}})
	require.Error(t, err)
	assert.Nil(t, tags)
	assert.Positive(t, credentialed)
	assert.Zero(t, anonymous, "a rate-limited credential must not spend the anonymous quota")
}

func TestListImageTagsCancellationInternal(t *testing.T) {
	svc := NewContainerRegistryService(nil, nil, nil)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	_, err := svc.ListImageTags(ctx, "registry.test/team/app:1.0.0", nil)
	require.True(t, errors.Is(err, context.Canceled), "error = %v", err)
}

func TestListImageTagsRejectedCredentialFallbackInternal(t *testing.T) {
	basic := "Basic " + base64.StdEncoding.EncodeToString([]byte("user:token"))
	for _, tt := range []struct {
		name          string
		host          string
		wantError     bool
		wantTokenAuth []string
	}{
		{name: "private registry public image", host: "registry.test", wantTokenAuth: []string{basic, ""}},
		{name: "docker hub preserves quota", host: "docker.io", wantError: true, wantTokenAuth: []string{basic}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			// The registry challenges at /v2/ and its token endpoint rejects the stored credential but serves anonymous tokens.
			var tokenAuth []string
			svc := NewContainerRegistryService(nil, nil, nil, &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
				response := &http.Response{StatusCode: http.StatusOK, Body: http.NoBody, Header: http.Header{}}
				switch req.URL.Path {
				case "/v2/":
					response.StatusCode = http.StatusUnauthorized
					response.Header.Set("WWW-Authenticate", `Bearer realm="https://auth.test/token"`)
				case "/token":
					tokenAuth = append(tokenAuth, req.Header.Get("Authorization"))
					if req.Header.Get("Authorization") != "" {
						response.StatusCode = http.StatusUnauthorized
					} else {
						response.Body = io.NopCloser(strings.NewReader(`{"token":"anonymous-token"}`))
					}
				default:
					response.Body = io.NopCloser(strings.NewReader(`{"tags":["1.0.1"]}`))
				}
				return response, nil
			})})
			tags, err := svc.ListImageTags(t.Context(), tt.host+"/team/app:1.0.0", []containerregistry.Credential{{URL: tt.host, Username: "user", Token: "token", Enabled: true}})
			if tt.wantError {
				require.Error(t, err)
				assert.Nil(t, tags)
			} else {
				require.NoError(t, err)
				assert.Equal(t, []string{"1.0.1"}, tags)
			}
			assert.Equal(t, tt.wantTokenAuth, tokenAuth)
		})
	}
}
