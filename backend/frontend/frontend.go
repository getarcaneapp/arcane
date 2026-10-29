//go:build !exclude_frontend

package frontend

import (
	"cmp"
	"embed"
	"fmt"
	"io"
	"io/fs"
	"mime"
	"net/http"
	"path"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/labstack/echo/v5"
	"github.com/labstack/echo/v5/middleware"
	kit "go.getarcane.app/kit/pkg"
)

//go:embed all:dist
var frontendFS embed.FS

const indexHtmlFileConstant = "index.html"

type precompressedVariant struct {
	encoding  string
	extension string
}

var precompressedVariants = []precompressedVariant{
	{encoding: "br", extension: ".br"},
	{encoding: "gzip", extension: ".gz"},
}

// RegisterFrontend serves embedded assets and the SPA fallback outside /api.
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
		raw, err := fs.ReadFile(distFS, indexHtmlFileConstant)
		if err != nil {
			return fmt.Errorf("failed to read %s: %w", indexHtmlFileConstant, err)
		}
		if indexHTML, err = rewriteIndexForBasePath(raw, basePath); err != nil {
			return err
		}
	}

	skipAPI := func(c *echo.Context) bool {
		return strings.HasPrefix(c.Request().URL.Path, "/api")
	}

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
			method := c.Request().Method
			if (method == http.MethodGet || method == http.MethodHead) && serveEncoded(c, distFS, p) {
				return nil
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

func serveEncoded(c *echo.Context, distFS fs.FS, p string) bool {
	contentType := mime.TypeByExtension(path.Ext(p))
	available := slices.DeleteFunc(slices.Clone(precompressedVariants), func(variant precompressedVariant) bool {
		_, err := fs.Stat(distFS, p+variant.extension)
		return err != nil
	})
	if contentType == "" || len(available) == 0 {
		return false
	}

	header := c.Response().Header()
	header.Add("Vary", "Accept-Encoding")

	accepted := acceptedEncodings(c.Request().Header.Get("Accept-Encoding"))
	idx := slices.IndexFunc(available, func(variant precompressedVariant) bool {
		return slices.Contains(accepted, variant.encoding)
	})
	if idx < 0 {
		return false
	}

	file, err := distFS.Open(p + available[idx].extension)
	if err != nil {
		return false
	}
	defer func() {
		_ = file.Close()
	}()

	content, ok := file.(io.ReadSeeker)
	if !ok {
		return false
	}

	header.Set("Content-Encoding", available[idx].encoding)
	header.Set("Content-Type", contentType)
	http.ServeContent(c.Response(), c.Request(), p, time.Time{}, content)
	return true
}

func acceptedEncodings(acceptEncoding string) []string {
	weights := map[string]float64{}
	for part := range strings.SplitSeq(acceptEncoding, ",") {
		name, params, _ := strings.Cut(part, ";")
		weight := 1.0
		for param := range strings.SplitSeq(params, ";") {
			if value, found := strings.CutPrefix(strings.TrimSpace(param), "q="); found {
				parsed, err := strconv.ParseFloat(strings.TrimSpace(value), 64)
				weight = kit.Ternary(err == nil, parsed, 0)
			}
		}
		weights[strings.ToLower(strings.TrimSpace(name))] = weight
	}

	return slices.DeleteFunc([]string{"br", "gzip"}, func(encoding string) bool {
		weight, found := weights[encoding]
		if !found {
			weight, found = weights["*"]
		}
		return !found || weight <= 0
	})
}
