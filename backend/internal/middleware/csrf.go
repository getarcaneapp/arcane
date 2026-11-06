package middleware

import (
	"context"
	"log/slog"
	"net/http"
	"strings"

	"github.com/labstack/echo/v5"

	"getarcane.dev/arcane/v3/internal/common"
	"getarcane.dev/arcane/v3/internal/config"
)

type (
	CSRFMiddleware struct {
		cfg *config.Config
	}
)

var publicCrossOriginBypassPaths = []string{
	"/api/webhooks/trigger/{token}",
	"/api/auth/federated/token",
}

func NewCSRFMiddleware(cfg *config.Config) *CSRFMiddleware {
	return &CSRFMiddleware{cfg: cfg}
}

func (m *CSRFMiddleware) Add() echo.MiddlewareFunc {
	if m.cfg.AgentEnabled {
		return func(next echo.HandlerFunc) echo.HandlerFunc {
			return func(c *echo.Context) error { return next(c) }
		}
	}

	cop := http.NewCrossOriginProtection()
	for _, origin := range deriveAllowedOrigins(m.cfg, nil) {
		// AddTrustedOrigin expects scheme://host[:port]; deriveAllowedOrigins
		// already produces that form (shared with CORS). Skip anything malformed.
		if err := cop.AddTrustedOrigin(origin); err != nil {
			slog.WarnContext(context.Background(), "CSRF: ignoring invalid trusted origin", //nolint:forbidigo // Middleware configuration is derived at startup without a request context.
				"origin", origin, "error", err)
		}
	}
	for _, pattern := range publicCrossOriginBypassPaths {
		cop.AddInsecureBypassPattern(pattern)
	}

	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c *echo.Context) error {
			req := c.Request()
			if hasHeaderCredential(req) {
				return next(c)
			}
			if err := cop.Check(req); err != nil {
				slog.WarnContext(req.Context(), "CSRF: cross-origin request blocked",
					"path", req.URL.Path,
					"method", req.Method,
					"origin", req.Header.Get("Origin"),
					"secFetchSite", req.Header.Get("Sec-Fetch-Site"),
				)
				return c.JSON(http.StatusForbidden, common.APIError{
					Code:    common.APIErrorCodeForbidden,
					Message: "Cross-origin request blocked",
				})
			}
			return next(c)
		}
	}
}

func hasHeaderCredential(req *http.Request) bool {
	return strings.HasPrefix(req.Header.Get("Authorization"), "Bearer ") ||
		req.Header.Get(HeaderApiKey) != "" ||
		req.Header.Get(HeaderAgentToken) != ""
}
