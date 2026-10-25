//go:build !exclude_frontend

package frontend

import (
	"cmp"
	"embed"
	"fmt"
	"io/fs"
	"mime"
	"net/http"
	"path"
	"strings"

	"github.com/labstack/echo/v5"
	"github.com/labstack/echo/v5/middleware"
)

//go:embed all:dist
var frontendFS embed.FS

const indexHtmlFileConstant = "index.html"

// RegisterFrontend serves embedded assets and the SPA fallback outside /api. With a base path
// (see config.Config.BasePath), index.html is served rewritten for it; the router has already
// stripped the prefix from the request path.
func RegisterFrontend(e *echo.Echo, basePath string) error {
	distFS, err := fs.Sub(frontendFS, "dist")
	if err != nil {
		return fmt.Errorf("failed to create sub FS: %w", err)
	}

	if err := mime.AddExtensionType(".webmanifest", "application/manifest+json"); err != nil {
		return fmt.Errorf("failed to register web manifest MIME type: %w", err)
	}
	appFS, err := fs.Sub(distFS, "_app")
	if err != nil {
		return fmt.Errorf("failed to create app sub FS: %w", err)
	}

	var indexHTML []byte
	if basePath != "" {
		raw, readErr := fs.ReadFile(distFS, indexHtmlFileConstant)
		if readErr != nil {
			return fmt.Errorf("failed to read %s: %w", indexHtmlFileConstant, readErr)
		}
		if indexHTML, err = rewriteIndexForBasePath(raw, basePath); err != nil {
			return err
		}
	}

	skipAPI := func(c *echo.Context) bool {
		return strings.HasPrefix(c.Request().URL.Path, "/api")
	}

	e.Use(middleware.GzipWithConfig(middleware.GzipConfig{
		MinLength: 1024,
		Skipper: func(c *echo.Context) bool {
			if skipAPI(c) {
				return true
			}
			switch strings.ToLower(path.Ext(c.Request().URL.Path)) {
			case ".woff2", ".woff", ".png", ".jpg", ".jpeg", ".webp", ".avif", ".ico", ".gz":
				return true
			}
			return false
		},
	}))

	e.Use(func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c *echo.Context) error {
			if skipAPI(c) {
				return next(c)
			}

			p := cmp.Or(strings.TrimPrefix(c.Request().URL.Path, "/"), indexHtmlFileConstant)
			if _, statErr := fs.Stat(distFS, p); statErr != nil && !strings.HasPrefix(p, "_app/") {
				p = indexHtmlFileConstant
			}

			header := c.Response().Header()
			switch {
			case p == indexHtmlFileConstant || p == "service-worker.js" || p == "app.webmanifest" || p == "_app/version.json":
				header.Set("Cache-Control", "no-cache, no-store, must-revalidate")
				header.Set("Pragma", "no-cache")
				header.Set("Expires", "0")
			case strings.HasPrefix(p, "_app/immutable/"):
				header.Set("Cache-Control", "public, max-age=31536000, immutable")
			default:
				header.Set("Cache-Control", "public, max-age=86400")
			}
			if indexHTML != nil && p == indexHtmlFileConstant {
				return c.HTMLBlob(http.StatusOK, indexHTML)
			}
			return next(c)
		}
	})

	// Missing chunks must return 404 so SvelteKit can recover after a redeploy.
	e.StaticFS("/_app/", appFS)
	e.Use(middleware.StaticWithConfig(middleware.StaticConfig{
		Root:       ".",
		Filesystem: distFS,
		HTML5:      true,
		Skipper: func(c *echo.Context) bool {
			return skipAPI(c) || strings.HasPrefix(c.Request().URL.Path, "/_app/")
		},
	}))

	return nil
}
