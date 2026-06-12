package handlers

import (
	"context"
	"net/http"

	"github.com/danielgtaylor/huma/v2"
	humamw "github.com/getarcaneapp/arcane/backend/v2/api/middleware"
	"github.com/getarcaneapp/arcane/backend/v2/internal/services"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/authz"
	"github.com/getarcaneapp/arcane/types/v2/base"
	porttypes "github.com/getarcaneapp/arcane/types/v2/port"
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
		Middlewares: humamw.RequirePermission(api, authz.PermContainersList),
	}, h.ListPorts)
}

func (h *PortHandler) ListPorts(ctx context.Context, input *ListPortsInput) (*ListPortsOutput, error) {
	if h.portService == nil {
		return nil, huma.Error500InternalServerError("service not available")
	}

	params := buildPaginationParamsInternal(input.Start, input.Limit, input.Sort, input.Order, input.Search)

	items, paginationResp, err := h.portService.ListPortsPaginated(ctx, params)
	if err != nil {
		return nil, huma.Error500InternalServerError("failed to list ports")
	}

	return &ListPortsOutput{
		Body: PortPaginatedResponse{
			Success:    true,
			Data:       items,
			Pagination: toPaginationResponseInternal(paginationResp),
		},
	}, nil
}
