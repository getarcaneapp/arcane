package handlers

import (
	"context"
	"net/http"
	"strings"

	"github.com/danielgtaylor/huma/v2"
	"github.com/getarcaneapp/arcane/backend/internal/services"
	"github.com/getarcaneapp/arcane/backend/pkg/pagination"
	"github.com/getarcaneapp/arcane/types/base"
	porttypes "github.com/getarcaneapp/arcane/types/port"
)

type PortHandler struct {
	portService *services.PortService
}

type PortPaginatedResponse struct {
	Success    bool                    `json:"success"`
	Data       []porttypes.PortMapping `json:"data"`
	Pagination base.PaginationResponse `json:"pagination"`
}

type ListPortsInput struct {
	EnvironmentID string `path:"id" doc:"Environment ID"`
	Search        string `query:"search" doc:"Search query"`
	Sort          string `query:"sort" doc:"Column to sort by"`
	Order         string `query:"order" default:"asc" doc:"Sort direction (asc or desc)"`
	Start         int    `query:"start" default:"0" doc:"Start index for pagination"`
	Limit         int    `query:"limit" default:"20" doc:"Number of items per page"`
}

type ListPortsOutput struct {
	Body PortPaginatedResponse
}

func RegisterPorts(api huma.API, portSvc *services.PortService) {
	h := &PortHandler{portService: portSvc}

	huma.Register(api, huma.Operation{
		OperationID: "list-ports",
		Method:      http.MethodGet,
		Path:        "/environments/{id}/ports",
		Summary:     "List port mappings",
		Tags:        []string{"Ports"},
		Security:    []map[string][]string{{"BearerAuth": {}}, {"ApiKeyAuth": {}}},
	}, h.ListPorts)
}

func (h *PortHandler) ListPorts(ctx context.Context, input *ListPortsInput) (*ListPortsOutput, error) {
	if h.portService == nil {
		return nil, huma.Error500InternalServerError("service not available")
	}

	params := pagination.QueryParams{
		SearchQuery: pagination.SearchQuery{
			Search: strings.TrimSpace(input.Search),
		},
		SortParams: pagination.SortParams{
			Sort:  strings.TrimSpace(input.Sort),
			Order: pagination.SortOrder(input.Order),
		},
		PaginationParams: pagination.PaginationParams{
			Start: input.Start,
			Limit: input.Limit,
		},
	}

	items, paginationResp, err := h.portService.ListPortsPaginated(ctx, params)
	if err != nil {
		return nil, huma.Error500InternalServerError("failed to list ports")
	}

	return &ListPortsOutput{
		Body: PortPaginatedResponse{
			Success: true,
			Data:    items,
			Pagination: base.PaginationResponse{
				TotalPages:      paginationResp.TotalPages,
				TotalItems:      paginationResp.TotalItems,
				CurrentPage:     paginationResp.CurrentPage,
				ItemsPerPage:    paginationResp.ItemsPerPage,
				GrandTotalItems: paginationResp.GrandTotalItems,
			},
		},
	}, nil
}
