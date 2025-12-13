package handlers

import (
	"context"
	"strings"

	"github.com/danielgtaylor/huma/v2"
	"github.com/getarcaneapp/arcane/backend/internal/services"
	"github.com/getarcaneapp/arcane/types/version"
)

// VersionHandler handles version information endpoints.
type VersionHandler struct {
	versionService *services.VersionService
}

// ============================================================================
// Input/Output Types
// ============================================================================

type GetVersionInput struct {
	Current string `query:"current" doc:"Current version to compare against"`
}

type GetVersionOutput struct {
	Body version.Check
}

type GetAppVersionInput struct{}

type GetAppVersionOutput struct {
	Body version.Info
}

// ============================================================================
// Registration
// ============================================================================

// RegisterVersion registers version endpoints.
func RegisterVersion(api huma.API, versionService *services.VersionService) {
	h := &VersionHandler{versionService: versionService}

	huma.Register(api, huma.Operation{
		OperationID: "getVersion",
		Method:      "GET",
		Path:        "/version",
		Summary:     "Get version information",
		Description: "Get application version information and check for updates",
		Tags:        []string{"Version"},
	}, h.GetVersion)

	huma.Register(api, huma.Operation{
		OperationID: "getAppVersion",
		Method:      "GET",
		Path:        "/app-version",
		Summary:     "Get app version",
		Description: "Get the current application version",
		Tags:        []string{"Version"},
	}, h.GetAppVersion)
}

// ============================================================================
// Handler Methods
// ============================================================================

// GetVersion returns version information with optional update check.
func (h *VersionHandler) GetVersion(ctx context.Context, input *GetVersionInput) (*GetVersionOutput, error) {
	if h.versionService == nil {
		return nil, huma.Error500InternalServerError("service not available")
	}

	current := strings.TrimSpace(input.Current)
	info, _ := h.versionService.GetVersionInformation(ctx, current)

	return &GetVersionOutput{
		Body: version.Check{
			CurrentVersion:  info.CurrentVersion,
			NewestVersion:   info.NewestVersion,
			UpdateAvailable: info.UpdateAvailable,
			ReleaseURL:      info.ReleaseURL,
		},
	}, nil
}

// GetAppVersion returns the current application version.
func (h *VersionHandler) GetAppVersion(ctx context.Context, _ *GetAppVersionInput) (*GetAppVersionOutput, error) {
	if h.versionService == nil {
		return nil, huma.Error500InternalServerError("service not available")
	}

	info := h.versionService.GetAppVersionInfo(ctx)

	return &GetAppVersionOutput{
		Body: *info,
	}, nil
}
