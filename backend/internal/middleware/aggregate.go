package middleware

import (
	"context"
	"net/http"
	"slices"
	"strconv"
	"strings"

	"github.com/labstack/echo/v5"
	"go.getarcane.app/kit/pkg"

	"github.com/getarcaneapp/arcane/backend/v2/pkg/authz"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/utils/httpx"
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
			case len(unique) > maxEnvironments:
				return writeProxyError(c, http.StatusBadRequest, "Environment scope is limited to "+strconv.Itoa(maxEnvironments)+" environments")
			case req.Method != http.MethodGet || matcher == nil || !matcher.SupportsEnvironmentAggregation(http.MethodGet, kit.Ternary(hasSuffix, "/"+suffix, "")):
				return writeProxyError(c, http.StatusBadRequest, "This operation does not support multiple environments")
			}

			scope := &httpx.EnvironmentScope{IDs: unique, Request: req.Clone(req.Context()), Handler: c.Echo(), Resolve: resolve}
			c.SetRequest(req.WithContext(httpx.WithEnvironmentScope(req.Context(), scope)))
			return next(c)
		}
	}
}
