package middleware

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"slices"
	"strconv"
	"strings"

	"github.com/labstack/echo/v5"

	kit "kit.getarcane.app"

	"getarcane.dev/arcane/v3/pkg/authz"
	"getarcane.dev/arcane/v3/pkg/httpx"
)

func NewEnvironmentScopeMiddleware(resolve httpx.EnvironmentResolver, matcher *authz.PermissionMatcher, limit func(context.Context) int) echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c *echo.Context) error {
			req := c.Request()
			rest, ok := strings.CutPrefix(req.URL.Path, apiEnvironmentsPrefix)
			segment, suffix, hasSuffix := strings.Cut(rest, "/")
			if !ok || !strings.Contains(segment, ",") {
				return next(c)
			}

			ids := strings.Split(segment, ",")
			unique := kit.Unique(ids)
			maxEnvironments := limit(req.Context())
			switch {
			case slices.Contains(ids, ""):
				return writeProxyError(c, http.StatusBadRequest, "Environment scope contains an empty environment ID")
			case req.Method == http.MethodGet && len(unique) > maxEnvironments:
				return writeProxyError(c, http.StatusBadRequest, "Environment scope is limited to "+strconv.Itoa(maxEnvironments)+" environments")
			case matcher == nil || !matcher.SupportsEnvironmentAggregation(req.Method, kit.Ternary(hasSuffix, "/"+suffix, "")):
				return writeProxyError(c, http.StatusBadRequest, "This operation does not support multiple environments")
			}

			body, err := io.ReadAll(http.MaxBytesReader(c.Response(), req.Body, 1<<20))
			if err != nil {
				return writeProxyError(c, http.StatusBadRequest, "Could not read the request body")
			}
			scope := &httpx.EnvironmentScope{
				Handler: c.Echo(),
				Request: req.Clone(req.Context()),
				Resolve: resolve,
				IDs:     unique,
				Body:    body,
			}
			scoped := req.WithContext(httpx.WithEnvironmentScope(req.Context(), scope))
			scoped.Body = io.NopCloser(bytes.NewReader(body))
			c.SetRequest(scoped)
			return next(c)
		}
	}
}
