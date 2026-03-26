package handlers

import (
	"context"
	"net/http"

	"github.com/danielgtaylor/huma/v2"
	"github.com/getarcaneapp/arcane/backend/internal/services"
	"github.com/getarcaneapp/arcane/types/base"
	dashboardtypes "github.com/getarcaneapp/arcane/types/dashboard"
)

type DashboardHandler struct {
	dashboardService *services.DashboardService
}

type GetDashboardInput struct {
	EnvironmentID string `path:"id" doc:"Environment ID"`
	DebugAllGood  bool   `query:"debugAllGood" default:"false" doc:"Debug mode: force an empty action item list"`
}

type GetDashboardOutput struct {
	Body base.ApiResponse[dashboardtypes.Snapshot]
}

type GetDashboardActionItemsInput struct {
	EnvironmentID string `path:"id" doc:"Environment ID"`
	DebugAllGood  bool   `query:"debugAllGood" default:"false" doc:"Debug mode: force an empty action item list"`
}

type GetDashboardActionItemsOutput struct {
	Body base.ApiResponse[dashboardtypes.ActionItems]
}

func RegisterDashboard(api huma.API, dashboardService *services.DashboardService) {
	h := &DashboardHandler{dashboardService: dashboardService}

	huma.Register(api, huma.Operation{
		OperationID: "get-dashboard",
		Method:      http.MethodGet,
		Path:        "/environments/{id}/dashboard",
		Summary:     "Get dashboard snapshot",
		Description: "Returns the dashboard first-paint snapshot in a single response",
		Tags:        []string{"Dashboard"},
		Security: []map[string][]string{
			{"BearerAuth": {}},
			{"ApiKeyAuth": {}},
		},
	}, h.GetDashboard)

	huma.Register(api, huma.Operation{
		OperationID: "get-dashboard-action-items",
		Method:      http.MethodGet,
		Path:        "/environments/{id}/dashboard/action-items",
		Summary:     "Get dashboard action items",
		Description: "Returns only dashboard action items that currently need attention",
		Tags:        []string{"Dashboard"},
		Security: []map[string][]string{
			{"BearerAuth": {}},
			{"ApiKeyAuth": {}},
		},
	}, h.GetActionItems)
}

func (h *DashboardHandler) GetDashboard(ctx context.Context, input *GetDashboardInput) (*GetDashboardOutput, error) {
	if h.dashboardService == nil {
		return nil, huma.Error500InternalServerError("service not available")
	}

	// EnvironmentID is consumed by env proxy/auth middleware for routing/validation.
	_ = input.EnvironmentID

	snapshot, err := h.dashboardService.GetSnapshot(ctx, services.DashboardActionItemsOptions{
		DebugAllGood: input.DebugAllGood,
	})
	if err != nil {
		return nil, huma.Error500InternalServerError(err.Error())
	}

	if snapshot == nil {
		return nil, huma.Error500InternalServerError("dashboard snapshot not available")
	}

	return &GetDashboardOutput{
		Body: base.ApiResponse[dashboardtypes.Snapshot]{
			Success: true,
			Data:    *snapshot,
		},
	}, nil
}

func (h *DashboardHandler) GetActionItems(ctx context.Context, input *GetDashboardActionItemsInput) (*GetDashboardActionItemsOutput, error) {
	if h.dashboardService == nil {
		return nil, huma.Error500InternalServerError("service not available")
	}

	// EnvironmentID is consumed by env proxy/auth middleware for routing/validation.
	_ = input.EnvironmentID

	actionItems, err := h.dashboardService.GetActionItems(ctx, services.DashboardActionItemsOptions{
		DebugAllGood: input.DebugAllGood,
	})
	if err != nil {
		return nil, huma.Error500InternalServerError(err.Error())
	}

	if actionItems == nil {
		actionItems = &dashboardtypes.ActionItems{Items: []dashboardtypes.ActionItem{}}
	} else if actionItems.Items == nil {
		actionItems.Items = []dashboardtypes.ActionItem{}
	}

	return &GetDashboardActionItemsOutput{
		Body: base.ApiResponse[dashboardtypes.ActionItems]{
			Success: true,
			Data:    *actionItems,
		},
	}, nil
}
