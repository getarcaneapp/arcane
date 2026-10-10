package appimages

import (
	"context"
	"path/filepath"
	"strings"

	"github.com/danielgtaylor/huma/v2"
	"go.getarcane.app/kit/pkg"
)

// AppImagesHandler provides Huma-based application image endpoints.
type AppImagesHandler struct {
	appImagesService *ApplicationImagesService
}

type GetLogoInput struct {
	Full     bool   `query:"full" default:"false" doc:"Return full logo instead of icon"`
	Color    string `query:"color" doc:"Optional accent color override for preview (e.g., 'oklch(0.65 0.2 150)')"`
	Animated bool   `query:"animated" default:"false" doc:"Return trace-and-fill animated logo variant"`
	Loop     bool   `query:"loop" default:"false" doc:"Loop the animated icon indefinitely (loader mode; animated icon only)"`
}

type GetPWAIconInput struct {
	Filename string `path:"filename" example:"icon-192x192.png" doc:"PWA icon filename"`
}

type GetAppImageOutput struct {
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

// GetLogo returns the application logo image.
func (h *AppImagesHandler) GetLogo(ctx context.Context, input *GetLogoInput) (*GetAppImageOutput, error) {
	name := kit.Ternary(input.Full, "logo-full", "logo")
	if input.Animated {
		name += "-animated"
	}

	return h.getImageWithColor(name, input.Color, input.Loop)
}

// GetLogoEmail returns the application logo image for emails (PNG).
func (h *AppImagesHandler) GetLogoEmail(ctx context.Context, input *struct{}) (*GetAppImageOutput, error) {
	return h.getImageWithColor("logo-email", "", false)
}

// GetFavicon returns the application favicon image.
func (h *AppImagesHandler) GetFavicon(ctx context.Context, input *struct{}) (*GetAppImageOutput, error) {
	return h.getImageWithColor("favicon", "", false)
}

// GetDefaultProfile returns the default user profile image.
func (h *AppImagesHandler) GetDefaultProfile(ctx context.Context, input *struct{}) (*GetAppImageOutput, error) {
	return h.getImageWithColor("profile", "", false)
}

// GetPWAIcon returns a PWA icon image.
func (h *AppImagesHandler) GetPWAIcon(ctx context.Context, input *GetPWAIconInput) (*GetAppImageOutput, error) {
	if _, ok := allowedPWAIconFilenames[input.Filename]; !ok {
		return nil, huma.Error400BadRequest("invalid PWA icon filename")
	}

	return h.getImageWithColor(strings.TrimSuffix(input.Filename, filepath.Ext(input.Filename)), "", false)
}

func (h *AppImagesHandler) getImageWithColor(name, colorOverride string, loop bool) (*GetAppImageOutput, error) {
	imageData, mimeType, err := h.appImagesService.GetImageWithColor(name, colorOverride, loop)
	if err != nil {
		return nil, huma.Error500InternalServerError("Failed to retrieve image: " + err.Error())
	}

	// Disable branding caching so theme and build-channel artwork is reflected immediately.
	_, isPWAIcon := allowedPWAIconFilenames[name+".png"]
	isBranding := IsLogoVariant(name) || isPWAIcon || name == "logo-email" || name == "favicon"
	cacheControl := kit.Ternary(
		isBranding || colorOverride != "",
		"no-cache, no-store, must-revalidate",
		"public, max-age=900, stale-while-revalidate=86400",
	)

	return &GetAppImageOutput{
		ContentType:         mimeType,
		CacheControl:        cacheControl,
		XContentTypeOptions: "nosniff",
		Body:                imageData,
	}, nil
}
