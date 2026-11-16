package middleware

import (
	"net/http"
	"strings"

	"github.com/klauspost/compress/gzhttp"
	"github.com/labstack/echo/v5"

	"getarcane.dev/arcane/v3/pkg/httpx"
)

func NewCompressionMiddleware() echo.MiddlewareFunc {
	compress, err := gzhttp.NewWrapper(
		gzhttp.MinSize(1024),
		gzhttp.ContentTypes([]string{"application/json", "application/problem+json", "text/plain"}),
	)
	if err != nil {
		panic(err)
	}
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		compressed := echo.WrapMiddleware(func(handler http.Handler) http.Handler {
			return compress(handler)
		})(next)
		return func(c *echo.Context) error {
			req := c.Request()
			path := req.URL.Path
			if httpx.IsEnvironmentFanOutRequest(req.Context()) || httpx.IsWebSocketUpgradeRequest(req) ||
				(req.Method == http.MethodGet && path == "/api/stream") || strings.Contains(path, "/ws/") ||
				strings.HasSuffix(path, "/logs") || strings.Contains(path, "/logs/") ||
				strings.HasSuffix(path, "/terminal") || strings.Contains(path, "/terminal/") {
				return next(c)
			}
			response := c.Response()
			defer c.SetResponse(response)
			return compressed(c)
		}
	}
}
