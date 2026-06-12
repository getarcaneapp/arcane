package cookie

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestIsSecureInternal(t *testing.T) {
	t.Run("plain http returns false", func(t *testing.T) {
		req := httptest.NewRequest("GET", "http://example.com/", nil)
		assert.False(t, isSecureInternal(req))
	})

	t.Run("X-Forwarded-Proto https is not trusted directly", func(t *testing.T) {
		req := httptest.NewRequest("GET", "http://example.com/", nil)
		req.Header.Set("X-Forwarded-Proto", "https")
		assert.False(t, isSecureInternal(req))
	})

	t.Run("secure cookie context returns true", func(t *testing.T) {
		req := httptest.NewRequest("GET", "http://example.com/", nil)
		req = req.WithContext(WithSecureCookieContext(context.Background(), true))
		assert.True(t, isSecureInternal(req))
	})
}

func TestBuildTokenCookieStringFor(t *testing.T) {
	t.Run("secure uses host-prefixed secure cookie", func(t *testing.T) {
		header := BuildTokenCookieStringFor(60, "abc", true)

		cookies := readSetCookieHeadersInternal(t, header)
		require.Len(t, cookies, 1)
		assert.Equal(t, TokenCookieName, cookies[0].Name)
		assert.True(t, cookies[0].Secure)
	})

	t.Run("insecure uses fallback cookie", func(t *testing.T) {
		header := BuildTokenCookieStringFor(60, "abc", false)

		cookies := readSetCookieHeadersInternal(t, header)
		require.Len(t, cookies, 1)
		assert.Equal(t, InsecureTokenCookieName, cookies[0].Name)
		assert.False(t, cookies[0].Secure)
	})
}

func TestBuildClearTokenCookieStringsFor(t *testing.T) {
	t.Run("secure clears fallback and host-prefixed cookies", func(t *testing.T) {
		headers := BuildClearTokenCookieStringsFor(true)

		require.Len(t, headers, 2)
		assert.Equal(t, InsecureTokenCookieName, readSetCookieHeadersInternal(t, headers[0])[0].Name)
		secureCookie := readSetCookieHeadersInternal(t, headers[1])[0]
		assert.Equal(t, TokenCookieName, secureCookie.Name)
		assert.True(t, secureCookie.Secure)
	})

	t.Run("insecure clears only fallback cookie", func(t *testing.T) {
		headers := BuildClearTokenCookieStringsFor(false)

		require.Len(t, headers, 1)
		clearCookie := readSetCookieHeadersInternal(t, headers[0])[0]
		assert.Equal(t, InsecureTokenCookieName, clearCookie.Name)
		assert.False(t, clearCookie.Secure)
	})
}

func readSetCookieHeadersInternal(t *testing.T, headers ...string) []*http.Cookie {
	t.Helper()

	resp := &http.Response{
		Header: http.Header{
			"Set-Cookie": headers,
		},
	}
	for _, header := range headers {
		require.NotContains(t, strings.ToLower(header), "samesite=none")
	}
	return resp.Cookies()
}
