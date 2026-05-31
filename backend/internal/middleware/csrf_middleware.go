package middleware

import (
	"log/slog"
	"net/http"
	"strings"

	"github.com/getarcaneapp/arcane/backend/internal/config"
	"github.com/getarcaneapp/arcane/backend/internal/models"
	pkgutils "github.com/getarcaneapp/arcane/backend/pkg/utils"
	"github.com/labstack/echo/v4"
)

// publicCrossOriginBypassPaths are endpoints intentionally reachable cross-origin
// by non-browser callers that authenticate with their OWN token carried in the
// path or request body (not the session cookie). Cross-origin protection must not
// block them, since a legitimate external caller (CI, webhook source) may attach
// an Origin header. Patterns use net/http ServeMux syntax, matched against URL path.
var publicCrossOriginBypassPaths = []string{
	"/api/webhooks/trigger/{token}",
	"/api/auth/federated/token",
}

// CSRFMiddleware rejects cross-origin state-changing requests to the cookie-backed
// API. It complements the SameSite=Lax session cookie with server-side origin
// verification (Sec-Fetch-Site, with an Origin/Host fallback) via the standard
// library's net/http.CrossOriginProtection.
//
// Header-credentialed requests (Bearer / X-API-Key / agent token) are not CSRF-able
// — a browser cannot attach those headers to a forged cross-origin request — so they
// are left untouched, as are non-browser clients that send no Origin header.
type CSRFMiddleware struct {
	cfg *config.Config
}

func NewCSRFMiddleware(cfg *config.Config) *CSRFMiddleware {
	return &CSRFMiddleware{cfg: cfg}
}

func (m *CSRFMiddleware) Add() echo.MiddlewareFunc {
	// Edge Agent mode: skip. The agent only receives server-to-server requests
	// through the edge tunnel from the manager — never from browsers. Mirrors CORSMiddleware.
	if m.cfg != nil && m.cfg.EdgeAgent {
		return func(next echo.HandlerFunc) echo.HandlerFunc {
			return func(c echo.Context) error { return next(c) }
		}
	}

	cop := http.NewCrossOriginProtection()
	for _, origin := range deriveAllowedOriginsInternal(m.cfg, nil) {
		// AddTrustedOrigin expects scheme://host[:port]; deriveAllowedOriginsInternal
		// already produces that form (shared with CORS). Skip anything malformed.
		if err := cop.AddTrustedOrigin(origin); err != nil {
			slog.Warn("CSRF: ignoring invalid trusted origin", "origin", origin, "error", err)
		}
	}
	for _, pattern := range publicCrossOriginBypassPaths {
		cop.AddInsecureBypassPattern(pattern)
	}

	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			req := c.Request()
			if hasHeaderCredentialInternal(req) {
				return next(c)
			}
			if err := cop.Check(req); err != nil {
				slog.WarnContext(req.Context(), "CSRF: cross-origin request blocked",
					"path", req.URL.Path,
					"method", req.Method,
					"origin", req.Header.Get("Origin"),
					"sec_fetch_site", req.Header.Get("Sec-Fetch-Site"),
				)
				return c.JSON(http.StatusForbidden, models.APIError{
					Code:    models.APIErrorCodeForbidden,
					Message: "Cross-origin request blocked",
				})
			}
			return next(c)
		}
	}
}

// hasHeaderCredentialInternal reports whether the request carries a credential in a
// header that a browser cannot forge on a cross-origin request, meaning it is not
// susceptible to CSRF and should bypass the cross-origin check.
func hasHeaderCredentialInternal(req *http.Request) bool {
	return strings.HasPrefix(req.Header.Get("Authorization"), "Bearer ") ||
		req.Header.Get(pkgutils.HeaderApiKey) != "" ||
		req.Header.Get(pkgutils.HeaderAgentToken) != ""
}
