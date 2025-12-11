package handlers

import (
	"context"
	"net/http"

	"github.com/danielgtaylor/huma/v2"
	"github.com/getarcaneapp/arcane/backend/internal/services"
)

// FontsHandler provides Huma-based font endpoints.
type FontsHandler struct {
	fontService *services.FontService
}

// --- Huma Input/Output Wrappers ---

type GetFontOutput struct {
	ContentType  string `header:"Content-Type"`
	CacheControl string `header:"Cache-Control"`
	Body         []byte
}

// RegisterFonts registers font routes using Huma.
func RegisterFonts(api huma.API, fontService *services.FontService) {
	h := &FontsHandler{
		fontService: fontService,
	}

	huma.Register(api, huma.Operation{
		OperationID: "get-sans-font",
		Method:      http.MethodGet,
		Path:        "/fonts/sans",
		Summary:     "Get sans-serif font",
		Description: "Get the application sans-serif font (Geist)",
		Tags:        []string{"Fonts"},
		Security:    []map[string][]string{}, // Public endpoint
	}, h.GetSansFont)

	huma.Register(api, huma.Operation{
		OperationID: "get-mono-font",
		Method:      http.MethodGet,
		Path:        "/fonts/mono",
		Summary:     "Get monospace font",
		Description: "Get the application monospace font (Geist Mono)",
		Tags:        []string{"Fonts"},
		Security:    []map[string][]string{}, // Public endpoint
	}, h.GetMonoFont)

	huma.Register(api, huma.Operation{
		OperationID: "get-serif-font",
		Method:      http.MethodGet,
		Path:        "/fonts/serif",
		Summary:     "Get serif font",
		Description: "Get the application serif font (Calistoga)",
		Tags:        []string{"Fonts"},
		Security:    []map[string][]string{}, // Public endpoint
	}, h.GetSerifFont)
}

// GetSansFont returns the sans-serif font.
func (h *FontsHandler) GetSansFont(ctx context.Context, input *struct{}) (*GetFontOutput, error) {
	if h.fontService == nil {
		return nil, huma.Error500InternalServerError("service not available")
	}

	data, mimeType, err := h.fontService.GetSansFont()
	if err != nil {
		return nil, huma.Error404NotFound("font not found")
	}

	return h.createFontResponse(data, mimeType), nil
}

// GetMonoFont returns the monospace font.
func (h *FontsHandler) GetMonoFont(ctx context.Context, input *struct{}) (*GetFontOutput, error) {
	if h.fontService == nil {
		return nil, huma.Error500InternalServerError("service not available")
	}

	data, mimeType, err := h.fontService.GetMonoFont()
	if err != nil {
		return nil, huma.Error404NotFound("font not found")
	}

	return h.createFontResponse(data, mimeType), nil
}

// GetSerifFont returns the serif font.
func (h *FontsHandler) GetSerifFont(ctx context.Context, input *struct{}) (*GetFontOutput, error) {
	if h.fontService == nil {
		return nil, huma.Error500InternalServerError("service not available")
	}

	data, mimeType, err := h.fontService.GetSerifFont()
	if err != nil {
		return nil, huma.Error404NotFound("font not found")
	}

	return h.createFontResponse(data, mimeType), nil
}

func (h *FontsHandler) createFontResponse(data []byte, mimeType string) *GetFontOutput {
	// Cache for 1 year, immutable
	cacheControl := "public, max-age=31536000, immutable"

	return &GetFontOutput{
		ContentType:  mimeType,
		CacheControl: cacheControl,
		Body:         data,
	}
}
