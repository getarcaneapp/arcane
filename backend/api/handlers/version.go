package handlers

import (
	"context"
	"strings"

	"github.com/danielgtaylor/huma/v2"
	"github.com/getarcaneapp/arcane/backend/v2/internal/services"
	"github.com/getarcaneapp/arcane/types/v2/version"
)

// versionHandler handles version information endpoints.
type versionHandler struct {
	versionService *services.VersionService
}

// ============================================================================
// Input/Output Types
// ============================================================================

type getVersionInput struct {
	Current string `query:"current" doc:"Current version to compare against"`
}

type getVersionOutput struct {
	Body version.Check
}

type getAppVersionInput struct{}

type getAppVersionOutput struct {
	Body version.Info
}

// ============================================================================
// Registration
// ============================================================================

// RegisterVersion registers version endpoints.
func RegisterVersion(api huma.API, versionService *services.VersionService) {
	h := &versionHandler{versionService: versionService}

	huma.Register(api, huma.Operation{
		OperationID: "getVersion",
		Method:      "GET",
		Path:        "/version",
		Summary:     "Get version information",
		Description: "Get application version information and check for updates",
		Tags:        []string{"Version"},
		Security:    []map[string][]string{},
	}, h.getVersionInternal)

	huma.Register(api, huma.Operation{
		OperationID: "getAppVersion",
		Method:      "GET",
		Path:        "/app-version",
		Summary:     "Get app version",
		Description: "Get the current application version",
		Tags:        []string{"Version"},
		Security:    []map[string][]string{},
	}, h.getAppVersionInternal)
}

// ============================================================================
// Handler Methods
// ============================================================================

// GetVersion returns version information with optional update check.
func (h *versionHandler) getVersionInternal(ctx context.Context, input *getVersionInput) (*getVersionOutput, error) {
	if h.versionService == nil {
		return nil, huma.Error500InternalServerError("service not available")
	}

	current := strings.TrimSpace(input.Current)
	check, _ := h.versionService.GetVersionInformation(ctx, current)

	return &getVersionOutput{
		Body: *check,
	}, nil
}

// GetAppVersion returns the current application version.
func (h *versionHandler) getAppVersionInternal(ctx context.Context, _ *getAppVersionInput) (*getAppVersionOutput, error) {
	if h.versionService == nil {
		return nil, huma.Error500InternalServerError("service not available")
	}

	info := h.versionService.GetAppVersionInfo(ctx)

	return &getAppVersionOutput{
		Body: *info,
	}, nil
}
