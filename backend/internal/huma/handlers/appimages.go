package handlers

import (
	"context"
	"net/http"

	"github.com/danielgtaylor/huma/v2"
	"github.com/getarcaneapp/arcane/backend/internal/common"
	"github.com/getarcaneapp/arcane/backend/internal/services"
)

// AppImagesHandler provides Huma-based application image endpoints.
type AppImagesHandler struct {
	appImagesService *services.ApplicationImagesService
}

// --- Huma Input/Output Wrappers ---

type GetLogoInput struct {
	Full bool `query:"full" default:"false" doc:"Return full logo instead of icon"`
}

type GetAppImageOutput struct {
	ContentType  string `header:"Content-Type"`
	CacheControl string `header:"Cache-Control"`
	Body         []byte
}

// RegisterAppImages registers application image routes using Huma.
func RegisterAppImages(api huma.API, appImagesService *services.ApplicationImagesService) {
	h := &AppImagesHandler{
		appImagesService: appImagesService,
	}

	huma.Register(api, huma.Operation{
		OperationID: "get-logo",
		Method:      http.MethodGet,
		Path:        "/app-images/logo",
		Summary:     "Get application logo",
		Description: "Get the application logo image",
		Tags:        []string{"Application Images"},
	}, h.GetLogo)

	huma.Register(api, huma.Operation{
		OperationID: "get-logo-email",
		Method:      http.MethodGet,
		Path:        "/app-images/logo-email",
		Summary:     "Get application logo for email",
		Description: "Get the application logo image in PNG format for emails",
		Tags:        []string{"Application Images"},
	}, h.GetLogoEmail)

	huma.Register(api, huma.Operation{
		OperationID: "get-favicon",
		Method:      http.MethodGet,
		Path:        "/app-images/favicon",
		Summary:     "Get application favicon",
		Description: "Get the application favicon image",
		Tags:        []string{"Application Images"},
	}, h.GetFavicon)

	huma.Register(api, huma.Operation{
		OperationID: "get-default-profile",
		Method:      http.MethodGet,
		Path:        "/app-images/profile",
		Summary:     "Get default profile image",
		Description: "Get the default user profile image",
		Tags:        []string{"Application Images"},
	}, h.GetDefaultProfile)
}

// GetLogo returns the application logo image.
func (h *AppImagesHandler) GetLogo(ctx context.Context, input *GetLogoInput) (*GetAppImageOutput, error) {
	if h.appImagesService == nil {
		return nil, huma.Error500InternalServerError("service not available")
	}

	name := "logo"
	if input.Full {
		name = "logo-full"
	}

	return h.getImage(name)
}

// GetLogoEmail returns the application logo image for emails (PNG).
func (h *AppImagesHandler) GetLogoEmail(ctx context.Context, input *struct{}) (*GetAppImageOutput, error) {
	if h.appImagesService == nil {
		return nil, huma.Error500InternalServerError("service not available")
	}

	return h.getImage("logo-email")
}

// GetFavicon returns the application favicon image.
func (h *AppImagesHandler) GetFavicon(ctx context.Context, input *struct{}) (*GetAppImageOutput, error) {
	if h.appImagesService == nil {
		return nil, huma.Error500InternalServerError("service not available")
	}

	return h.getImage("favicon")
}

// GetDefaultProfile returns the default user profile image.
func (h *AppImagesHandler) GetDefaultProfile(ctx context.Context, input *struct{}) (*GetAppImageOutput, error) {
	if h.appImagesService == nil {
		return nil, huma.Error500InternalServerError("service not available")
	}

	return h.getImage("profile")
}

func (h *AppImagesHandler) getImage(name string) (*GetAppImageOutput, error) {
	imageData, mimeType, err := h.appImagesService.GetImage(name)
	if err != nil {
		return nil, huma.Error500InternalServerError((&common.ImageRetrievalError{Err: err}).Error())
	}

	// Cache for 15 minutes, stale-while-revalidate for 24 hours
	cacheControl := "public, max-age=900, stale-while-revalidate=86400"

	return &GetAppImageOutput{
		ContentType:  mimeType,
		CacheControl: cacheControl,
		Body:         imageData,
	}, nil
}
