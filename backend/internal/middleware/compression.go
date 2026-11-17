package middleware

import (
	"fmt"
	"net/http"
	"strings"

	"github.com/klauspost/compress/gzhttp"
	"github.com/labstack/echo/v5"

	"getarcane.dev/arcane/v3/pkg/httpx"
)

const compressionMinSize = 1 << 10

func NewCompressionMiddleware() echo.MiddlewareFunc {
	compress, err := gzhttp.NewWrapper(
		gzhttp.MinSize(compressionMinSize),
		gzhttp.ContentTypes([]string{"application/json", "application/problem+json", "text/plain"}),
	)
	if err != nil {
		panic(fmt.Errorf("configure API compression: %w", err))
	}
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		compressed := echo.WrapMiddleware(func(handler http.Handler) http.Handler {
			return compress(handler)
		})(next)
		return func(c *echo.Context) error {
			req := c.Request()
			if httpx.IsWebSocketUpgradeRequest(req) || strings.Contains(req.Header.Get("Accept"), "text/event-stream") {
				return next(c)
			}
			response := c.Response()
			defer c.SetResponse(response)
			return compressed(c)
		}
	}
}
