package bootstrap

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/adapters/humaecho"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/utils/cookie"
	"github.com/labstack/echo/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewEchoInternal_DecodesPathParams(t *testing.T) {
	router := newEchoInternal()
	api := humaecho.NewWithGroup(router, router.Group("/api"), huma.DefaultConfig("test", "1.0.0"))

	var got string
	huma.Register(api, huma.Operation{
		OperationID: "get-image",
		Method:      http.MethodGet,
		Path:        "/environments/{id}/images/{imageId}",
	}, func(_ context.Context, input *struct {
		ID      string `path:"id"`
		ImageID string `path:"imageId"`
	}) (*struct{}, error) {
		got = input.ImageID
		return nil, nil
	})

	// RFC 3986 §6.2.2.2: these are all the same URI and must resolve identically.
	for _, segment := range []string{"sha256:abc", "sha256%3Aabc", "%73ha256:abc"} {
		got = ""
		req := httptest.NewRequest(http.MethodGet, "/api/environments/0/images/"+segment, nil)
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)

		require.Equal(t, http.StatusNoContent, rec.Code, "segment %q", segment)
		require.Equal(t, "sha256:abc", got, "segment %q", segment)
	}
}

func TestSecureCookieContextMiddleware_TrustGating(t *testing.T) {
	_, loopback, err := net.ParseCIDR("127.0.0.0/8")

	require.NoError(t, err,
		"parse cidr: %v", err)

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
		handler := secureCookieContextMiddlewareInternal(nets)(func(c *echo.Context) error {
			observed = cookie.SecureCookieFromContext(c.Request().Context())
			return nil
		})
		{
			err := handler(c)
			require.NoError(t, err,
				"handler: %v", err)
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
