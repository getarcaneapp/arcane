package registry

import (
	"context"
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
				calls++
				assert.Equal(t, "registry-1.docker.io", req.URL.Host)
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
	calls := 0
	svc := NewContainerRegistryService(nil, nil, nil, &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		calls++
		return &http.Response{StatusCode: http.StatusTooManyRequests, Body: io.NopCloser(strings.NewReader("")), Header: http.Header{}}, nil
	})})
	tags, err := svc.ListImageTags(t.Context(), "registry.test/team/app:1.0.0", []containerregistry.Credential{{URL: "registry.test", Username: "user", Token: "token", Enabled: true}})
	require.Error(t, err)
	assert.Nil(t, tags)
	assert.Equal(t, 1, calls)
}

func TestListImageTagsCancellationInternal(t *testing.T) {
	svc := NewContainerRegistryService(nil, nil, nil)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	_, err := svc.ListImageTags(ctx, "registry.test/team/app:1.0.0", nil)
	require.True(t, errors.Is(err, context.Canceled), "error = %v", err)
}

func TestListImageTagsRejectedCredentialFallbackInternal(t *testing.T) {
	for _, tt := range []struct {
		name      string
		host      string
		wantError bool
		wantCalls int
	}{
		{name: "private registry public image", host: "registry.test", wantCalls: 3},
		{name: "docker hub preserves quota", host: "docker.io", wantError: true, wantCalls: 2},
	} {
		t.Run(tt.name, func(t *testing.T) {
			calls := 0
			svc := NewContainerRegistryService(nil, nil, nil, &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
				calls++
				response := &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(`{"tags":["1.0.1"]}`)), Header: http.Header{}}
				if req.URL.Path == "/token" {
					response.StatusCode = http.StatusUnauthorized
				} else if req.Header.Get("Authorization") != "" {
					response.StatusCode = http.StatusUnauthorized
					response.Header.Set("WWW-Authenticate", `Bearer realm="https://auth.test/token"`)
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
			assert.Equal(t, tt.wantCalls, calls)
		})
	}
}
