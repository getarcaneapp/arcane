package handlers

import (
	"context"
	"net/http"
	"path/filepath"
	"strings"

	"github.com/danielgtaylor/huma/v2"
	"github.com/getarcaneapp/arcane/backend/v2/internal/common"
	"github.com/getarcaneapp/arcane/backend/v2/internal/services"
)

// appImagesHandler provides Huma-based application image endpoints.
type appImagesHandler struct {
	appImagesService *services.ApplicationImagesService
}

// --- Huma Input/Output Wrappers ---

type getLogoInput struct {
	Full  bool   `query:"full" default:"false" doc:"Return full logo instead of icon"`
	Color string `query:"color" doc:"Optional accent color override for preview (e.g., 'oklch(0.65 0.2 150)')"`
}

type getPWAIconInput struct {
	Filename string `path:"filename" example:"icon-192x192.png" doc:"PWA icon filename"`
}

type getAppImageOutput struct {
	ContentType         string `header:"Content-Type"`
	CacheControl        string `header:"Cache-Control"`
	XContentTypeOptions string `header:"X-Content-Type-Options"`
	Body                []byte
}

var allowedPWAIconFilenames = map[string]struct{}{
	"icon-72x72.png":   {},
	"icon-96x96.png":   {},
	"icon-128x128.png": {},
	"icon-144x144.png": {},
	"icon-152x152.png": {},
	"icon-192x192.png": {},
	"icon-384x384.png": {},
	"icon-512x512.png": {},
}

// RegisterAppImages registers application image routes using Huma.
func RegisterAppImages(api huma.API, appImagesService *services.ApplicationImagesService) {
	h := &appImagesHandler{
		appImagesService: appImagesService,
	}

	huma.Register(api, huma.Operation{
		OperationID: "get-logo",
		Method:      http.MethodGet,
		Path:        "/app-images/logo",
		Summary:     "Get application logo",
		Description: "Get the application logo image",
		Tags:        []string{"Application Images"},
		Security:    []map[string][]string{},
	}, h.getLogoInternal)

	huma.Register(api, huma.Operation{
		OperationID: "get-logo-email",
		Method:      http.MethodGet,
		Path:        "/app-images/logo-email",
		Summary:     "Get application logo for email",
		Description: "Get the application logo image in PNG format for emails",
		Tags:        []string{"Application Images"},
		Security:    []map[string][]string{},
	}, h.getLogoEmailInternal)

	huma.Register(api, huma.Operation{
		OperationID: "get-favicon",
		Method:      http.MethodGet,
		Path:        "/app-images/favicon",
		Summary:     "Get application favicon",
		Description: "Get the application favicon image",
		Tags:        []string{"Application Images"},
		Security:    []map[string][]string{},
	}, h.getFaviconInternal)

	huma.Register(api, huma.Operation{
		OperationID: "get-default-profile",
		Method:      http.MethodGet,
		Path:        "/app-images/profile",
		Summary:     "Get default profile image",
		Description: "Get the default user profile image",
		Tags:        []string{"Application Images"},
		Security:    []map[string][]string{},
	}, h.getDefaultProfileInternal)

	huma.Register(api, huma.Operation{
		OperationID: "get-pwa-icon",
		Method:      http.MethodGet,
		Path:        "/app-images/pwa/{filename}",
		Summary:     "Get PWA icon",
		Description: "Get a Progressive Web App icon image",
		Tags:        []string{"Application Images"},
		Security:    []map[string][]string{},
	}, h.getPWAIconInternal)
}

// GetLogo returns the application logo image.
func (h *appImagesHandler) getLogoInternal(_ context.Context, input *getLogoInput) (*getAppImageOutput, error) {
	if h.appImagesService == nil {
		return nil, huma.Error500InternalServerError("service not available")
	}

	name := "logo"
	if input.Full {
		name = "logo-full"
	}

	return h.getImageWithColor(name, input.Color)
}

// GetLogoEmail returns the application logo image for emails (PNG).
func (h *appImagesHandler) getLogoEmailInternal(_ context.Context, _ *struct{}) (*getAppImageOutput, error) {
	if h.appImagesService == nil {
		return nil, huma.Error500InternalServerError("service not available")
	}

	return h.getImage("logo-email")
}

// GetFavicon returns the application favicon image.
func (h *appImagesHandler) getFaviconInternal(_ context.Context, _ *struct{}) (*getAppImageOutput, error) {
	if h.appImagesService == nil {
		return nil, huma.Error500InternalServerError("service not available")
	}

	return h.getImage("favicon")
}

// GetDefaultProfile returns the default user profile image.
func (h *appImagesHandler) getDefaultProfileInternal(_ context.Context, _ *struct{}) (*getAppImageOutput, error) {
	if h.appImagesService == nil {
		return nil, huma.Error500InternalServerError("service not available")
	}

	return h.getImage("profile")
}

// GetPWAIcon returns a PWA icon image.
func (h *appImagesHandler) getPWAIconInternal(_ context.Context, input *getPWAIconInput) (*getAppImageOutput, error) {
	if h.appImagesService == nil {
		return nil, huma.Error500InternalServerError("service not available")
	}

	return h.getImageByFilenameInternal(input.Filename)
}

func (h *appImagesHandler) getImage(name string) (*getAppImageOutput, error) {
	return h.getImageWithColor(name, "")
}

func (h *appImagesHandler) getImageByFilenameInternal(filename string) (*getAppImageOutput, error) {
	if _, ok := allowedPWAIconFilenames[filename]; !ok {
		return nil, huma.Error400BadRequest("invalid PWA icon filename")
	}

	name := strings.TrimSuffix(filename, filepath.Ext(filename))
	return h.getImage(name)
}

func (h *appImagesHandler) getImageWithColor(name string, colorOverride string) (*getAppImageOutput, error) {
	imageData, mimeType, err := h.appImagesService.GetImageWithColor(name, colorOverride)
	if err != nil {
		return nil, huma.Error500InternalServerError((&common.ImageRetrievalError{Err: err}).Error())
	}

	// Always disable logo caching so theme/logo updates are reflected immediately.
	// Keep cache for static app images that do not change at runtime.
	cacheControl := "public, max-age=900, stale-while-revalidate=86400"
	if name == "logo" || name == "logo-full" || colorOverride != "" {
		cacheControl = "no-cache, no-store, must-revalidate"
	}

	return &getAppImageOutput{
		ContentType:         mimeType,
		CacheControl:        cacheControl,
		XContentTypeOptions: "nosniff",
		Body:                imageData,
	}, nil
}
