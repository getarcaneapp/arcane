package appimages

import (
	"context"
	"errors"
	"log/slog"
	"mime/multipart"
	"path/filepath"
	"strings"

	"github.com/danielgtaylor/huma/v2"

	"kit.getarcane.app"

	"getarcane.dev/arcane/v3/internal/common"
	"getarcane.dev/arcane/v3/pkg/handlerutil"
	"getarcane.dev/types/v3"
)

type (
	AppImagesHandler struct {
		appImagesService *ApplicationImagesService
	}

	GetLogoInput struct {
		Color    string `query:"color" doc:"Optional accent color override for preview (e.g., 'oklch(0.65 0.2 150)')"`
		Full     bool   `query:"full" default:"false" doc:"Return full logo instead of icon"`
		Animated bool   `query:"animated" default:"false" doc:"Return trace-and-fill animated logo variant"`
		Loop     bool   `query:"loop" default:"false" doc:"Loop the animated icon indefinitely (loader mode; animated icon only)"`
	}

	UploadLogoInput struct {
		RawBody multipart.Form `contentType:"multipart/form-data"`
		Variant string         `path:"variant" enum:"mark,full" doc:"Logo slot to replace"`
	}

	DeleteLogoInput struct {
		Variant string `path:"variant" enum:"mark,full" doc:"Logo slot to reset to the built-in artwork"`
	}

	UploadImageInput struct {
		RawBody multipart.Form `contentType:"multipart/form-data"`
	}

	GetPWAIconInput struct {
		Filename string `path:"filename" example:"icon-192x192.png" doc:"PWA icon filename"`
	}

	GetAppImageOutput struct {
		ContentType           string `header:"Content-Type"`
		CacheControl          string `header:"Cache-Control"`
		XContentTypeOptions   string `header:"X-Content-Type-Options"`
		ContentSecurityPolicy string `header:"Content-Security-Policy"`
		Body                  []byte
	}
)

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

func (h *AppImagesHandler) GetLogo(ctx context.Context, input *GetLogoInput) (*GetAppImageOutput, error) {
	name := kit.Ternary(input.Full, "logo-full", "logo")
	if input.Animated {
		name += "-animated"
	}

	return h.getImageWithColor(ctx, name, input.Color, input.Loop)
}

func (h *AppImagesHandler) UploadLogo(ctx context.Context, input *UploadLogoInput) (*handlerutil.Out[types.MessageResponse], error) {
	return h.uploadImage(ctx, ImageSlot(input.Variant), input.RawBody)
}

func (h *AppImagesHandler) DeleteLogo(ctx context.Context, input *DeleteLogoInput) (*handlerutil.Out[types.MessageResponse], error) {
	return h.deleteImage(ctx, ImageSlot(input.Variant))
}

func (h *AppImagesHandler) UploadFavicon(ctx context.Context, input *UploadImageInput) (*handlerutil.Out[types.MessageResponse], error) {
	return h.uploadImage(ctx, ImageSlotFavicon, input.RawBody)
}

func (h *AppImagesHandler) DeleteFavicon(ctx context.Context, input *struct{}) (*handlerutil.Out[types.MessageResponse], error) {
	return h.deleteImage(ctx, ImageSlotFavicon)
}

func (h *AppImagesHandler) UploadPWAIcon(ctx context.Context, input *UploadImageInput) (*handlerutil.Out[types.MessageResponse], error) {
	return h.uploadImage(ctx, ImageSlotPWA, input.RawBody)
}

func (h *AppImagesHandler) DeletePWAIcon(ctx context.Context, input *struct{}) (*handlerutil.Out[types.MessageResponse], error) {
	return h.deleteImage(ctx, ImageSlotPWA)
}

func (h *AppImagesHandler) GetFavicon(ctx context.Context, input *struct{}) (*GetAppImageOutput, error) {
	return h.getImageWithColor(ctx, "favicon", "", false)
}

func (h *AppImagesHandler) GetDefaultProfile(ctx context.Context, input *struct{}) (*GetAppImageOutput, error) {
	return h.getImageWithColor(ctx, "profile", "", false)
}

func (h *AppImagesHandler) GetPWAIcon(ctx context.Context, input *GetPWAIconInput) (*GetAppImageOutput, error) {
	if _, ok := allowedPWAIconFilenames[input.Filename]; !ok {
		return nil, huma.Error400BadRequest("invalid PWA icon filename")
	}

	return h.getImageWithColor(ctx, strings.TrimSuffix(input.Filename, filepath.Ext(input.Filename)), "", false)
}

func (h *AppImagesHandler) uploadImage(ctx context.Context, slot ImageSlot, form multipart.Form) (*handlerutil.Out[types.MessageResponse], error) {
	files := form.File["file"]
	if len(files) == 0 {
		return nil, huma.Error400BadRequest("no file uploaded; include a 'file' field in the multipart form")
	}
	f, err := files[0].Open()
	if err != nil {
		return nil, huma.Error500InternalServerError("failed to read uploaded file: " + err.Error())
	}
	defer func() { _ = f.Close() }()

	if uploadErr := h.appImagesService.UploadImage(ctx, slot, f); uploadErr != nil {
		if errors.Is(uploadErr, common.ErrValidation) {
			return nil, handlerutil.APIError(uploadErr)
		}
		slog.ErrorContext(ctx, "Failed to save custom image", "slot", slot, "error", uploadErr)
		return nil, huma.Error500InternalServerError("failed to save image")
	}
	return handlerutil.MessageOutput("Image updated successfully", ""), nil
}

func (h *AppImagesHandler) deleteImage(ctx context.Context, slot ImageSlot) (*handlerutil.Out[types.MessageResponse], error) {
	if err := h.appImagesService.DeleteImage(ctx, slot); err != nil {
		if errors.Is(err, common.ErrValidation) {
			return nil, handlerutil.APIError(err)
		}
		slog.ErrorContext(ctx, "Failed to delete custom image", "slot", slot, "error", err)
		return nil, huma.Error500InternalServerError("failed to delete image")
	}
	return handlerutil.MessageOutput("Image reset successfully", ""), nil
}

func (h *AppImagesHandler) getImageWithColor(ctx context.Context, name, colorOverride string, loop bool) (*GetAppImageOutput, error) {
	imageData, mimeType, err := h.appImagesService.GetImageWithColor(ctx, name, colorOverride, loop)
	if err != nil {
		if errors.Is(err, common.ErrNotFound) {
			return nil, huma.Error404NotFound(err.Error())
		}
		return nil, huma.Error500InternalServerError("Failed to retrieve image: " + err.Error())
	}

	// Disable branding caching so theme and build-channel artwork is reflected immediately.
	_, isPWAIcon := allowedPWAIconFilenames[name+".png"]
	isBranding := IsLogoVariant(name) || isPWAIcon || name == "favicon"
	cacheControl := kit.Ternary(
		isBranding || colorOverride != "",
		"no-cache, no-store, must-revalidate",
		"public, max-age=900, stale-while-revalidate=86400",
	)

	return &GetAppImageOutput{
		ContentType:           mimeType,
		CacheControl:          cacheControl,
		XContentTypeOptions:   "nosniff",
		ContentSecurityPolicy: "default-src 'none'; style-src 'unsafe-inline'; sandbox",
		Body:                  imageData,
	}, nil
}
