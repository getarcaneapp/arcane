package distribution

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

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

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodHead:
			headCalls++
			w.WriteHeader(http.StatusMethodNotAllowed)
		case http.MethodGet:
			getCalls++
			w.Header().Set("Docker-Content-Digest", "sha256:method-not-allowed")
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
	assert.Equal(t, "sha256:method-not-allowed", digest)
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
