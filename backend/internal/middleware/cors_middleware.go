package middleware

import (
	"log/slog"
	"net/url"
	"strings"

	"github.com/getarcaneapp/arcane/backend/internal/config"
	"github.com/labstack/echo/v4"
	echomiddleware "github.com/labstack/echo/v4/middleware"
)

type CORSMiddleware struct {
	cfg           *config.Config
	customOrigins []string
}

func NewCORSMiddleware(cfg *config.Config) *CORSMiddleware {
	return &CORSMiddleware{cfg: cfg}
}

func (m *CORSMiddleware) Add() echo.MiddlewareFunc {
	// Edge Agent mode: skip CORS entirely. The agent only receives server-to-server
	// requests through the edge tunnel from the manager — never from browsers.
	if m.cfg != nil && m.cfg.EdgeAgent {
		return func(next echo.HandlerFunc) echo.HandlerFunc {
			return func(c echo.Context) error { return next(c) }
		}
	}

	return echomiddleware.CORSWithConfig(echomiddleware.CORSConfig{
		AllowOrigins:     deriveAllowedOriginsInternal(m.cfg, m.customOrigins),
		AllowCredentials: true,
		AllowMethods:     []string{"GET", "POST", "PUT", "DELETE", "OPTIONS", "PATCH", "HEAD"},
		AllowHeaders: []string{
			"Authorization",
			"Content-Type",
			"X-CSRF-Token",
			"X-Requested-With",
			"Accept",
			"Accept-Language",
			"Accept-Encoding",
			"User-Agent",
			"Cache-Control",
			"Origin",
			"Referer",
			"X-Arcane-Agent-Token",
			"X-API-Key",
		},
		ExposeHeaders: []string{
			"Content-Length",
			"Content-Type",
			"X-Total-Count",
			"X-Page",
			"X-Per-Page",
		},
		MaxAge: 300,
	})
}

func deriveAllowedOriginsInternal(cfg *config.Config, custom []string) []string {
	if len(custom) > 0 {
		return dedupeInternal(custom)
	}

	var origins []string
	if cfg != nil {
		appURL := cfg.GetAppURL()
		if appURL != "" {
			if !strings.HasPrefix(appURL, "http://") && !strings.HasPrefix(appURL, "https://") {
				appURL = "https://" + appURL
			}
			if u, err := url.Parse(appURL); err == nil {
				origins = append(origins, u.Scheme+"://"+u.Host)
			} else {
				slog.Warn("Failed to parse APP_URL for CORS origins", "url", appURL, "error", err)
			}
		}
	}

	if cfg == nil || cfg.Environment != "production" {
		origins = append(origins,
			"http://localhost:3000", "http://127.0.0.1:3000",
			"http://localhost:3552", "http://127.0.0.1:3552",
		)
	}

	origins = dedupeInternal(origins)

	if len(origins) == 0 {
		if cfg != nil && cfg.Environment == "production" {
			slog.Warn("CORS: No origins specified for production - defaulting to https://localhost")
			return []string{"https://localhost"}
		}
		return []string{"http://localhost:3000"}
	}

	return origins
}

func dedupeInternal(in []string) []string {
	seen := make(map[string]struct{}, len(in))
	out := make([]string, 0, len(in))
	for _, v := range in {
		if _, ok := seen[v]; ok {
			continue
		}
		seen[v] = struct{}{}
		out = append(out, v)
	}
	return out
}
