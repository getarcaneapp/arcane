package version

import (
	"context"
	"strings"

	"getarcane.dev/types/v3"
)

type (
	VersionHandler struct {
		versionService *VersionService
	}

	GetVersionInput struct {
		Current string `query:"current" doc:"Current version to compare against"`
	}

	GetVersionOutput struct {
		Body types.VersionCheck
	}

	GetAppVersionInput struct{}

	GetAppVersionOutput struct {
		Body types.VersionInfo
	}
)

func (h *VersionHandler) GetVersion(ctx context.Context, input *GetVersionInput) (*GetVersionOutput, error) {
	current := strings.TrimSpace(input.Current)
	check, _ := h.versionService.GetVersionInformation(ctx, current)

	return &GetVersionOutput{
		Body: *check,
	}, nil
}

func (h *VersionHandler) GetAppVersion(ctx context.Context, _ *GetAppVersionInput) (*GetAppVersionOutput, error) {
	info := h.versionService.GetAppVersionInfo(ctx)

	return &GetAppVersionOutput{
		Body: *info,
	}, nil
}
