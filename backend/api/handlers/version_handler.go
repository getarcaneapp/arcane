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

// GetVersionInput represents the input for the version check endpoint.
type GetVersionInput struct {
	Current string `query:"current" doc:"Current version to compare against"`
}

// GetVersionOutput represents the output for the version check endpoint.
type GetVersionOutput struct {
	Body version.Check
}

// GetAppVersionOutput represents the output for the app version endpoint.
type GetAppVersionOutput struct {
	Body version.Info
}

// GetAppVersionInput represents the input for the app version endpoint.
type GetAppVersionInput struct{}

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
