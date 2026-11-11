package dashboard

import (
	"context"

	"github.com/danielgtaylor/huma/v2"

	"getarcane.dev/arcane/v3/internal/middleware"
	"getarcane.dev/arcane/v3/pkg/handlerutil"
	"getarcane.dev/arcane/v3/pkg/httpx"
	"getarcane.dev/types/v3"
)

type (
	DashboardHandler struct {
		dashboardService *DashboardService
	}

	GetSummaryInput struct {
		EnvironmentID string   `path:"id" doc:"Environment ID"`
		Sections      []string `query:"sections" doc:"Sections to load: containers, projects, images, vulnerabilities, risk, diskUsage, topMemory, version"`
	}
)

func (h *DashboardHandler) GetSummary(ctx context.Context, input *GetSummaryInput) (*handlerutil.Out[types.DashboardSummary], error) {
	if _, ok := httpx.EnvironmentScopeFromContext(ctx); ok {
		body, err := h.dashboardService.AggregateSummaries(ctx)
		if err != nil {
			return nil, huma.Error500InternalServerError("Failed to get dashboard summary: " + err.Error())
		}
		return &handlerutil.Out[types.DashboardSummary]{Body: body}, nil
	}

	permissions, _ := middleware.PermissionsFromContext(ctx)
	return &handlerutil.Out[types.DashboardSummary]{
		Body: types.ApiResponse[types.DashboardSummary]{
			Success: true,
			Data: h.dashboardService.GetSummary(ctx, SummaryRequest{
				Permissions:   permissions,
				EnvironmentID: input.EnvironmentID,
				Sections:      input.Sections,
			}),
		},
	}, nil
}
