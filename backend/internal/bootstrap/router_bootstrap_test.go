package bootstrap

import (
	"net"
	"net/http/httptest"
	"testing"

	"github.com/getarcaneapp/arcane/backend/pkg/utils/cookie"
	"github.com/labstack/echo/v4"
	"github.com/stretchr/testify/assert"
)

func TestSecureCookieContextMiddleware_TrustGating(t *testing.T) {
	_, loopback, err := net.ParseCIDR("127.0.0.0/8")
	if err != nil {
		t.Fatalf("parse cidr: %v", err)
	}
	trusted := []*net.IPNet{loopback}

	runRequest := func(t *testing.T, nets []*net.IPNet, remoteAddr, forwardedProto string) bool {
		t.Helper()
		e := echo.New()
		req := httptest.NewRequest("GET", "/", nil)
		req.RemoteAddr = remoteAddr
		if forwardedProto != "" {
			req.Header.Set("X-Forwarded-Proto", forwardedProto)
		}
		rec := httptest.NewRecorder()
		c := e.NewContext(req, rec)

		var observed bool
		handler := secureCookieContextMiddlewareInternal(nets)(func(c echo.Context) error {
			observed = cookie.SecureCookieFromContext(c.Request().Context())
			return nil
		})
		if err := handler(c); err != nil {
			t.Fatalf("handler: %v", err)
		}
		return observed
	}

	t.Run("trusted proxy with X-Forwarded-Proto https sets secure", func(t *testing.T) {
		assert.True(t, runRequest(t, trusted, "127.0.0.1:54321", "https"))
	})

	t.Run("untrusted client setting X-Forwarded-Proto https is ignored", func(t *testing.T) {
		assert.False(t, runRequest(t, trusted, "203.0.113.10:54321", "https"))
	})

	t.Run("trusted proxy with X-Forwarded-Proto http stays insecure", func(t *testing.T) {
		assert.False(t, runRequest(t, trusted, "127.0.0.1:54321", "http"))
	})

	t.Run("no trusted proxies configured ignores header even from loopback", func(t *testing.T) {
		assert.False(t, runRequest(t, nil, "127.0.0.1:54321", "https"))
	})

	t.Run("unparseable remote addr is untrusted", func(t *testing.T) {
		assert.False(t, runRequest(t, trusted, "garbage", "https"))
	})
}
