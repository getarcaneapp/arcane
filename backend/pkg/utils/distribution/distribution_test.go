package distribution

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	digest "github.com/opencontainers/go-digest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func serverURLFromRequestInternal(r *http.Request) string {
	return "https://" + r.Host
}

func TestIsFallbackEligibleDaemonError(t *testing.T) {
	eligible := []string{
		"not found",
		"status: 404",
		"status 404",
		" 404 ",
		"403 forbidden",
		"status: 403",
		"status 403",
		"not implemented",
		"unsupported",
		"distribution disabled",
		"distribution api",
		"administrative rules",
		"proxyconnect tcp: dial tcp :0: connect: connection refused",
	}
	for _, msg := range eligible {
		t.Run("eligible/"+msg, func(t *testing.T) {
			assert.True(t, IsFallbackEligibleDaemonError(fmt.Errorf("%s", msg)))
		})
	}

	notEligible := []string{
		"unauthorized",
		"authentication required",
		"no basic auth credentials",
		"access denied",
		"status: 401",
		"x509: certificate signed by unknown authority",
		"tls handshake timeout",
		"connection refused",
		"no such host",
	}
	for _, msg := range notEligible {
		t.Run("not-eligible/"+msg, func(t *testing.T) {
			assert.False(t, IsFallbackEligibleDaemonError(fmt.Errorf("%s", msg)))
		})
	}

	assert.False(t, IsFallbackEligibleDaemonError(nil))
}

func TestFetchDigestWithHTTPClient_FallsBackToGetOnMethodNotAllowed(t *testing.T) {
	var headCalls int
	var getCalls int
	wantDigest := digest.FromString("method-not-allowed").String()

	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodHead:
			headCalls++
			w.WriteHeader(http.StatusMethodNotAllowed)
		case http.MethodGet:
			getCalls++
			w.Header().Set("Docker-Content-Digest", wantDigest)
			w.WriteHeader(http.StatusOK)
		default:
			t.Fatalf("unexpected method %s", r.Method)
		}
	}))
	defer server.Close()

	digest, err := FetchDigestWithHTTPClient(
		context.Background(),
		server.URL,
		"team/app",
		"1.2.3",
		nil,
		server.Client(),
	)
	require.NoError(t, err)
	assert.Equal(t, wantDigest, digest)
	assert.Equal(t, 1, headCalls)
	assert.Equal(t, 1, getCalls)
}

func TestFetchDigestWithHTTPClient_DoesNotFallbackToGetOnResourceErrors(t *testing.T) {
	testCases := []struct {
		name   string
		status int
	}{
		{name: "not found", status: http.StatusNotFound},
		{name: "forbidden", status: http.StatusForbidden},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			var headCalls int
			var getCalls int

			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch r.Method {
				case http.MethodHead:
					headCalls++
					w.WriteHeader(tc.status)
				case http.MethodGet:
					getCalls++
					w.Header().Set("Docker-Content-Digest", "sha256:unexpected-get")
					w.WriteHeader(http.StatusOK)
				default:
					t.Fatalf("unexpected method %s", r.Method)
				}
			}))
			defer server.Close()

			digest, err := FetchDigestWithHTTPClient(
				context.Background(),
				server.URL,
				"team/app",
				"1.2.3",
				nil,
				server.Client(),
			)
			require.Error(t, err)
			assert.Empty(t, digest)
			assert.Equal(t, fmt.Sprintf("manifest request failed with status: %d", tc.status), err.Error())
			assert.Equal(t, 1, headCalls)
			assert.Equal(t, 0, getCalls)
		})
	}
}

func TestFetchRegistryRateLimitWithHTTPClient_AnonymousTokenFlow(t *testing.T) {
	var tokenCalls int
	var manifestCalls int

	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/token":
			tokenCalls++
			assert.Empty(t, r.Header.Get("Authorization"))
			assert.Equal(t, "registry.example.com", r.URL.Query().Get("service"))
			assert.Equal(t, "repository:team/app:pull", r.URL.Query().Get("scope"))
			_, _ = w.Write([]byte(`{"token":"test-token"}`))
		case r.Method == http.MethodHead && r.URL.Path == "/v2/team/app/manifests/latest":
			manifestCalls++
			if r.Header.Get("Authorization") != "Bearer test-token" {
				w.Header().Set("WWW-Authenticate", fmt.Sprintf(`Bearer realm="%s/token",service="registry.example.com"`, serverURLFromRequestInternal(r)))
				w.WriteHeader(http.StatusUnauthorized)
				return
			}
			w.Header().Set("RateLimit-Limit", "100;w=21600")
			w.Header().Set("RateLimit-Remaining", "76;w=21600")
			w.Header().Set("Docker-RateLimit-Source", "203.0.113.10")
			w.WriteHeader(http.StatusOK)
		default:
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.Path)
		}
	}))
	defer server.Close()

	info, err := FetchRegistryRateLimitWithHTTPClient(
		context.Background(),
		server.URL,
		"team/app",
		"latest",
		nil,
		server.Client(),
	)

	require.NoError(t, err)
	require.NotNil(t, info.Limit)
	require.NotNil(t, info.Remaining)
	require.NotNil(t, info.Used)
	require.NotNil(t, info.WindowSeconds)
	assert.Equal(t, 100, *info.Limit)
	assert.Equal(t, 76, *info.Remaining)
	assert.Equal(t, 24, *info.Used)
	assert.Equal(t, 21600, *info.WindowSeconds)
	assert.Equal(t, "203.0.113.10", info.Source)
	assert.Equal(t, 1, tokenCalls)
	assert.Equal(t, 2, manifestCalls)
}

func TestFetchRegistryRateLimitWithHTTPClient_CredentialBackedTokenFlow(t *testing.T) {
	var tokenAuth string
	var initialManifestAuth string

	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/token":
			tokenAuth = r.Header.Get("Authorization")
			_, _ = w.Write([]byte(`{"access_token":"credential-token"}`))
		case r.Method == http.MethodHead && r.URL.Path == "/v2/team/app/manifests/latest":
			if r.Header.Get("Authorization") != "Bearer credential-token" {
				initialManifestAuth = r.Header.Get("Authorization")
				w.Header().Set("WWW-Authenticate", fmt.Sprintf(`Bearer realm="%s/token",service="registry.example.com"`, serverURLFromRequestInternal(r)))
				w.WriteHeader(http.StatusUnauthorized)
				return
			}
			w.Header().Set("RateLimit-Limit", "200;w=21600")
			w.Header().Set("RateLimit-Remaining", "199;w=21600")
			w.WriteHeader(http.StatusOK)
		default:
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.Path)
		}
	}))
	defer server.Close()

	info, err := FetchRegistryRateLimitWithHTTPClient(
		context.Background(),
		server.URL,
		"team/app",
		"latest",
		&Credentials{Username: "docker-user", Token: "docker-token"},
		server.Client(),
	)

	require.NoError(t, err)
	require.NotNil(t, info.Limit)
	require.NotNil(t, info.Remaining)
	assert.Equal(t, 200, *info.Limit)
	assert.Equal(t, 199, *info.Remaining)
	assert.Contains(t, initialManifestAuth, "Basic ")
	assert.Contains(t, tokenAuth, "Basic ")
}

func TestFetchRegistryRateLimitWithHTTPClient_MissingHeaders(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	info, err := FetchRegistryRateLimitWithHTTPClient(context.Background(), server.URL, "team/app", "latest", nil, server.Client())

	require.Error(t, err)
	assert.Nil(t, info)
	assert.Contains(t, err.Error(), "rate limit headers not returned")
}

func TestFetchRegistryRateLimitWithHTTPClient_MalformedHeader(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("RateLimit-Limit", "bad;w=21600")
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	info, err := FetchRegistryRateLimitWithHTTPClient(context.Background(), server.URL, "team/app", "latest", nil, server.Client())

	require.Error(t, err)
	assert.Nil(t, info)
	assert.Contains(t, err.Error(), "parse RateLimit-Limit")
}

func TestFetchRegistryRateLimitWithHTTPClient_NonOKResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	defer server.Close()

	info, err := FetchRegistryRateLimitWithHTTPClient(context.Background(), server.URL, "team/app", "latest", nil, server.Client())

	require.Error(t, err)
	assert.Nil(t, info)
	assert.Contains(t, err.Error(), "manifest request failed with status: 429")
}

func TestParseWWWAuthInternal_AllowsCommasInsideQuotedRealm(t *testing.T) {
	realm, service := parseWWWAuthInternal(`Bearer realm="https://auth.example.com/token?a=1,b=2",service="registry.example.com"`)
	assert.Equal(t, "https://auth.example.com/token?a=1,b=2", realm)
	assert.Equal(t, "registry.example.com", service)
}

func TestFetchDigestWithHTTPClient_RejectsUntrustedTokenRealm(t *testing.T) {
	registry := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("WWW-Authenticate", `Bearer realm="https://169.254.169.254/token",service="registry.example.com"`)
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer registry.Close()

	digest, err := FetchDigestWithHTTPClient(
		context.Background(),
		registry.URL,
		"team/app",
		"latest",
		nil,
		registry.Client(),
	)

	require.Error(t, err)
	assert.Empty(t, digest)
	assert.Contains(t, err.Error(), "untrusted auth realm host")
}

func TestValidateAuthRealmInternal(t *testing.T) {
	tests := []struct {
		name         string
		registryHost string
		realm        string
		wantErr      bool
		errContains  string
	}{
		{
			name:         "same host allowed",
			registryHost: "registry.example.com",
			realm:        "https://registry.example.com/token",
		},
		{
			name:         "same host with explicit default https port allowed",
			registryHost: "registry.example.com",
			realm:        "https://registry.example.com:443/token",
		},
		{
			name:         "same host with matching non-default port allowed",
			registryHost: "127.0.0.1:5000",
			realm:        "https://127.0.0.1:5000/token",
		},
		{
			name:         "docker hub auth host allowed",
			registryHost: "registry-1.docker.io",
			realm:        "https://auth.docker.io/token",
		},
		{
			name:         "lscr.io delegates auth to ghcr.io",
			registryHost: "lscr.io",
			realm:        "https://ghcr.io/token",
		},
		{
			name:         "non https realm rejected",
			registryHost: "registry.example.com",
			realm:        "http://registry.example.com/token",
			wantErr:      true,
			errContains:  "auth realm must use HTTPS",
		},
		{
			name:         "empty realm rejected",
			registryHost: "registry.example.com",
			realm:        "",
			wantErr:      true,
			errContains:  "auth realm must use HTTPS",
		},
		{
			name:         "malformed realm rejected",
			registryHost: "registry.example.com",
			realm:        "https://%zz",
			wantErr:      true,
			errContains:  "invalid auth realm",
		},
		{
			name:         "relative realm rejected",
			registryHost: "registry.example.com",
			realm:        "/token",
			wantErr:      true,
			errContains:  "auth realm must use HTTPS",
		},
		{
			name:         "untrusted realm rejected",
			registryHost: "registry.example.com",
			realm:        "https://evil.com/token",
			wantErr:      true,
			errContains:  "untrusted auth realm host",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := validateAuthRealmInternal(tc.registryHost, tc.realm)
			if tc.wantErr {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tc.errContains)
				return
			}

			require.NoError(t, err)
		})
	}
}
