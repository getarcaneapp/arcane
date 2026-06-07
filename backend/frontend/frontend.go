//go:build !exclude_frontend

package frontend

import (
	"embed"
	"errors"
	"fmt"
	"io/fs"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/labstack/echo/v4"
)

//go:embed all:dist
var frontendFS embed.FS

const indexHtmlFileConstant = "index.html"

// RegisterFrontend mounts the embedded SPA on the Echo instance and configures
// a custom HTTP error handler that intercepts 404s to serve the SPA index
// (preserving SvelteKit client-side routing) while keeping /api/* 404s as JSON.
func RegisterFrontend(e *echo.Echo) error {
	distFS, err := fs.Sub(frontendFS, "dist")
	if err != nil {
		return fmt.Errorf("failed to create sub FS: %w", err)
	}

	cacheMaxAge := time.Hour * 24
	fileServer := NewFileServerWithCaching(http.FS(distFS), int(cacheMaxAge.Seconds()))

	defaultErrorHandler := e.HTTPErrorHandler
	e.HTTPErrorHandler = func(handlerErr error, c echo.Context) {
		var he *echo.HTTPError
		isNotFound := errors.As(handlerErr, &he) && he.Code == http.StatusNotFound
		if !isNotFound {
			defaultErrorHandler(handlerErr, c)
			return
		}

		req := c.Request()
		path := req.URL.Path

		if strings.HasPrefix(path, "/api/") {
			_ = c.JSON(http.StatusNotFound, map[string]any{
				"success": false,
				"error":   fmt.Sprintf("API endpoint not found: %s", path),
			})
			return
		}

		requestedPath := strings.TrimPrefix(path, "/")
		if requestedPath == "" {
			requestedPath = indexHtmlFileConstant
		}

		if _, statErr := fs.Stat(distFS, requestedPath); os.IsNotExist(statErr) {
			req.URL.Path = "/"
		}

		fileServer.ServeHTTP(c.Response().Writer, req)
	}

	return nil
}

type FileServerWithCaching struct {
	root                    http.FileSystem
	lastModified            time.Time
	cacheMaxAge             int
	lastModifiedHeaderValue string
	cacheControlHeaderValue string
}

func NewFileServerWithCaching(root http.FileSystem, maxAge int) *FileServerWithCaching {
	return &FileServerWithCaching{
		root:                    root,
		lastModified:            time.Now(),
		cacheMaxAge:             maxAge,
		lastModifiedHeaderValue: time.Now().UTC().Format(http.TimeFormat),
		cacheControlHeaderValue: fmt.Sprintf("public, max-age=%d", maxAge),
	}
}

func (f *FileServerWithCaching) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/")
	if path == "" {
		path = indexHtmlFileConstant
	}

	// Service worker needs correct MIME type and no caching for PWA updates
	if path == "service-worker.js" {
		w.Header().Set("Content-Type", "application/javascript; charset=utf-8")
		w.Header().Set("Cache-Control", "no-cache, no-store, must-revalidate")
		w.Header().Set("Pragma", "no-cache")
		w.Header().Set("Expires", "0")
		http.FileServer(f.root).ServeHTTP(w, r)
		return
	}

	// Web manifest needs correct MIME type and no caching for PWA updates
	if path == "app.webmanifest" {
		w.Header().Set("Content-Type", "application/manifest+json; charset=utf-8")
		w.Header().Set("Cache-Control", "no-cache, no-store, must-revalidate")
		w.Header().Set("Pragma", "no-cache")
		w.Header().Set("Expires", "0")
		http.FileServer(f.root).ServeHTTP(w, r)
		return
	}

	// Never cache index.html - it needs to be fresh to detect updates
	if path == indexHtmlFileConstant {
		w.Header().Set("Cache-Control", "no-cache, no-store, must-revalidate")
		w.Header().Set("Pragma", "no-cache")
		w.Header().Set("Expires", "0")
		http.FileServer(f.root).ServeHTTP(w, r)
		return
	}

	// For immutable assets (with content hashes), use long-term caching
	if strings.Contains(path, "/_app/immutable/") {
		w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
		http.FileServer(f.root).ServeHTTP(w, r)
		return
	}

	// For other static assets, use the configured cache duration
	if ifModifiedSince := r.Header.Get("If-Modified-Since"); ifModifiedSince != "" {
		ifModifiedSinceTime, err := time.Parse(http.TimeFormat, ifModifiedSince)
		if err == nil && f.lastModified.Before(ifModifiedSinceTime.Add(1*time.Second)) {
			w.WriteHeader(http.StatusNotModified)
			return
		}
	}

	w.Header().Set("Last-Modified", f.lastModifiedHeaderValue)
	w.Header().Set("Cache-Control", f.cacheControlHeaderValue)

	http.FileServer(f.root).ServeHTTP(w, r)
}
